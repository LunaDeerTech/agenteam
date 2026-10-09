package knowledge

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	ev "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	kc "github.com/LunaDeerTech/agenteam/internal/central/knowledge/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	ob "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type contentRequest struct {
	Format   int                `json:"format"`
	Create   *kc.CreateRequest  `json:"create,omitempty"`
	Update   *kc.UpdateRequest  `json:"update,omitempty"`
	Expected *f.Version         `json:"expected_version,omitempty"`
	Source   *publicationSource `json:"source,omitempty"`
}

func (r contentRequest) validate() error {
	if r.Format != 1 {
		return internal(nil)
	}
	if r.Create != nil {
		if r.Create.Validate() != nil || r.Update != nil || r.Expected != nil || r.Source == nil {
			return internal(nil)
		}
	} else if r.Update == nil || r.Update.Validate() != nil || r.Expected == nil || r.Expected.Validate() != nil || r.Update.ReplaceSource != (r.Source != nil) {
		return internal(nil)
	}
	if r.Source != nil {
		return r.Source.validate()
	}
	return nil
}
func decodeContentRequest(raw []byte) (contentRequest, error) {
	var out contentRequest
	if len(raw) == 0 || len(raw) > 512<<10 {
		return out, internal(nil)
	}
	canonical, err := cursor.CanonicalJSON(raw)
	if err != nil {
		return out, internal(err)
	}
	d := json.NewDecoder(bytes.NewReader(canonical))
	d.DisallowUnknownFields()
	if err = d.Decode(&out); err != nil {
		return out, internal(err)
	}
	if d.Decode(new(any)) != io.EOF {
		return out, internal(nil)
	}
	return out, out.validate()
}

type contentInput struct {
	actor    id.Actor
	meta     f.CommandMeta
	project  id.ProjectID
	document kc.DocumentID
	name     kc.CommandName
	digest   f.Digest
	request  contentRequest
}

func createContentInput(actor id.Actor, meta f.CommandMeta, request kc.CreateRequest, source kc.SourceInput) (contentInput, error) {
	digest, err := kc.CreateDigest(actor, meta, request, source)
	if err != nil {
		return contentInput{}, portError(err)
	}
	descriptor, err := describePublicationSource(source)
	if err != nil {
		return contentInput{}, err
	}
	// Snapshot the public pointer-bearing request before any transaction/I/O.
	if request.ParentDocumentID != nil {
		value := *request.ParentDocumentID
		request.ParentDocumentID = &value
	}
	out := contentInput{actor: actor, meta: meta, project: request.ProjectID, document: request.DocumentID, name: kc.Create, digest: digest, request: contentRequest{Format: 1, Create: &request, Source: &descriptor}}
	return out, out.request.validate()
}
func updateContentInput(actor id.Actor, meta f.CommandMeta, project id.ProjectID, document kc.DocumentID, request kc.UpdateRequest, source *kc.SourceInput) (contentInput, error) {
	digest, err := kc.UpdateDigest(actor, meta, project, document, request, source)
	if err != nil {
		return contentInput{}, portError(err)
	}
	if request.Title != nil {
		value := *request.Title
		request.Title = &value
	}
	expected := *meta.ExpectedVersion
	meta.ExpectedVersion = &expected
	out := contentInput{actor: actor, meta: meta, project: project, document: document, name: kc.Update, digest: digest, request: contentRequest{Format: 1, Update: &request, Expected: &expected}}
	if source != nil {
		descriptor, err := describePublicationSource(*source)
		if err != nil {
			return contentInput{}, err
		}
		out.request.Source = &descriptor
	}
	return out, out.request.validate()
}

type contentIntent struct {
	record  *commandRecord
	request contentRequest
	header  ev.Header
	current *documentRow
}

func newContentHeader(project id.ProjectID, document kc.DocumentID, version f.Version, at f.Instant) (ev.Header, error) {
	key, err := f.NewID[ev.EventIdentity]()
	if err != nil {
		return ev.Header{}, unavailable(err)
	}
	p, err := f.ParseID[ev.Project](project.String())
	if err != nil {
		return ev.Header{}, internal(err)
	}
	d, err := f.ParseID[ev.Aggregate](document.String())
	if err != nil {
		return ev.Header{}, internal(err)
	}
	header := ev.Header{EventID: key, EventType: kc.ContentChangedEvent, SchemaVersion: 1, OccurredAt: at, Scope: ev.Scope{Kind: ev.ProjectScope, ProjectID: p}, AggregateType: kc.KnowledgeAggregate, AggregateID: d, AggregateVersion: &version}
	return header, portError(header.Validate())
}

// prepareContentIntent confirms only the durable original command/source/event
// identity. It never reads a source or makes a prospective document visible.
// External preparation additionally requires a confirmed work claim.
func (s *Service) prepareContentIntent(ctx context.Context, input contentInput) (contentIntent, error) {
	var out contentIntent
	if err := readInput(ctx, input.actor, input.project); err != nil {
		return out, err
	}
	if err := input.request.validate(); err != nil {
		return out, err
	}
	user, err := f.ParseID[id.User](input.actor.Details().UserID)
	if err != nil {
		return out, portError(err)
	}
	operation, err := f.NewID[command]()
	if err != nil {
		return out, unavailable(err)
	}
	candidate := &commandRecord{id: operation, project: input.project, document: input.document, user: user, name: input.name, key: input.meta.IdempotencyKey, digest: input.digest, state: kc.InProgress}
	var sourceProject *id.ProjectID
	if input.request.Source != nil {
		sourceProject, err = input.request.Source.sourceProject()
		if err != nil {
			return out, err
		}
	}
	locks, err := publicationLocks(input.actor, candidate, sourceProject)
	if err != nil {
		return out, err
	}
	identity, err := kc.CommandIdentity(input.project, input.name, input.meta.IdempotencyKey)
	if err != nil {
		return out, portError(err)
	}
	cause, err := f.NewCommandsCause(identity)
	if err != nil {
		return out, portError(err)
	}
	requestRaw, err := json.Marshal(input.request)
	if err != nil {
		return out, internal(err)
	}
	st := s.state()
	result := st.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := st.store.AcquireAll(ctx, tx, locks); err != nil {
			return portError(err)
		}
		x, err := s.readScope(ctx, tx, input.actor, input.project)
		if err != nil {
			return err
		}
		record, err := loadCommand(ctx, x, input.project, input.name, input.meta.IdempotencyKey)
		if err != nil {
			return err
		}
		if record != nil {
			if record.user != user || record.digest != input.digest || record.document != input.document {
				return fault(f.IdempotencyKeyReused)
			}
			if record.state == kc.Committed {
				out.record = record
				return nil
			}
		}
		access, err := st.deps.Projects.RequireOwnerInTx(ctx, tx, input.actor, input.project, id.Mutate)
		if err != nil {
			return portError(err)
		}
		if !access.Matches(input.actor, input.project) {
			return internal(nil)
		}
		if source := input.request.Source; source != nil && source.Business != nil && source.Business.Kind == oc.ExecutionFile {
			return fault(f.DependencyUnbound)
		}
		if sourceProject != nil && *sourceProject != input.project {
			if _, err = s.readScope(ctx, tx, input.actor, *sourceProject); err != nil {
				return err
			}
		}
		var current *documentRow
		version := f.Version(1)
		if input.name == kc.Create {
			if record == nil {
				var occupied string
				err = x.QueryRow(ctx, `SELECT project_id::text FROM (
 SELECT project_id FROM agenteam_knowledge.documents WHERE id=$1
 UNION ALL SELECT project_id FROM agenteam_knowledge.commands WHERE document_id=$1 AND command_name='create') q LIMIT 1`, input.document.String()).Scan(&occupied)
				if err == nil {
					if occupied != input.project.String() {
						return fault(f.NotFound)
					}
					return fault(f.ResourceBusy)
				}
				if !errors.Is(err, pgx.ErrNoRows) {
					return unavailable(err)
				}
			}
			if parent := input.request.Create.ParentDocumentID; parent != nil {
				row, err := loadDocument(ctx, x, input.project, *parent)
				if err != nil {
					return err
				}
				if row.head.Active == nil {
					return fault(f.NotFound)
				}
			}
		} else {
			current, err = loadDocument(ctx, x, input.project, input.document)
			if err != nil {
				return err
			}
			if current.head.Active == nil {
				return fault(f.NotFound)
			}
			if current.head.Active.ContentVersion != *input.request.Expected {
				return fault(f.VersionConflict)
			}
			if input.request.Source == nil && *input.request.Update.Title == current.head.Active.Title && record == nil {
				now, err := dbNow(ctx, x)
				if err != nil {
					return err
				}
				receipt := kc.MutationReceipt{Command: kc.Update, Document: current.head.Active, Changed: false}
				if err = insertCompletedCommand(ctx, x, input.actor, input.project, input.document, kc.Update, input.meta.IdempotencyKey, input.digest, receipt, now); err != nil {
					return err
				}
				out.record, err = loadCommand(ctx, x, input.project, kc.Update, input.meta.IdempotencyKey)
				return err
			}
			if current.head.Active.ContentVersion == f.Version(math.MaxInt64) {
				return fault(f.VersionConflict)
			}
			version = current.head.Active.ContentVersion + 1
		}
		if record != nil {
			var stored, header []byte
			if err = x.QueryRow(ctx, `SELECT request,plan FROM agenteam_knowledge.commands WHERE project_id=$1 AND id=$2`, record.project.String(), record.id.String()).Scan(&stored, &header); err != nil {
				return unavailable(err)
			}
			parsed, err := decodeContentRequest(stored)
			if err != nil {
				return err
			}
			canonical, err := cursor.CanonicalJSON(stored)
			if err != nil {
				return internal(err)
			}
			wanted, err := cursor.CanonicalJSON(requestRaw)
			if err != nil {
				return internal(err)
			}
			if !bytes.Equal(canonical, wanted) {
				return fault(f.IdempotencyKeyReused)
			}
			if out.header, err = ev.DecodeHeader(header); err != nil {
				return internal(err)
			}
			if out.header.EventType != kc.ContentChangedEvent || out.header.SchemaVersion != 1 || out.header.AggregateType != kc.KnowledgeAggregate || out.header.AggregateSequence != nil || out.header.AggregateID.String() != input.document.String() || out.header.Scope.Kind != ev.ProjectScope || out.header.Scope.ProjectID.String() != input.project.String() || out.header.AggregateVersion == nil || *out.header.AggregateVersion != version {
				return internal(nil)
			}
			out.record, out.request, out.current = record, parsed, current
			return nil
		}
		now, err := dbNow(ctx, x)
		if err != nil {
			return err
		}
		header, err := newContentHeader(input.project, input.document, version, now)
		if err != nil {
			return err
		}
		headerRaw, err := json.Marshal(header)
		if err != nil {
			return internal(err)
		}
		_, err = x.Exec(ctx, `INSERT INTO agenteam_knowledge.commands
 (id,project_id,document_id,actor_user_id,command_name,command_key,semantic_digest,request,plan,state,created_at)
 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,'planned',$10)`, operation.String(), input.project.String(), input.document.String(), user.String(), string(input.name), string(input.meta.IdempotencyKey), input.digest.String(), requestRaw, headerRaw, now.Time())
		if err != nil {
			return unavailable(err)
		}
		if input.request.Source != nil {
			raw, err := json.Marshal(input.request.Source)
			if err != nil {
				return internal(err)
			}
			var origin any
			if sourceProject != nil {
				origin = sourceProject.String()
			}
			_, err = x.Exec(ctx, `INSERT INTO agenteam_knowledge.publications(command_id,project_id,document_id,source,source_project_id,phase) VALUES($1,$2,$3,$4,$5,'planned')`, operation.String(), input.project.String(), input.document.String(), raw, origin)
			if err != nil {
				return unavailable(err)
			}
		}
		candidate.created = now
		out = contentIntent{record: candidate, request: input.request, header: header, current: current}
		return nil
	})
	if err = txError(result); err != nil {
		return contentIntent{}, err
	}
	return out, nil
}

// currentContentIntent checks the same original intent under the caller's
// complete union. Completed receipts precede new-work gates and source checks.
func (s *Service) currentContentIntent(ctx context.Context, tx f.Tx, input contentInput, intent contentIntent) (postgres.SQLExecutor, *commandRecord, *documentRow, error) {
	x, err := s.readScope(ctx, tx, input.actor, input.project)
	if err != nil {
		return nil, nil, nil, err
	}
	record, err := loadCommand(ctx, x, input.project, input.name, input.meta.IdempotencyKey)
	if err != nil {
		return nil, nil, nil, err
	}
	if record == nil || intent.record == nil || record.id != intent.record.id {
		return nil, nil, nil, fault(f.ResourceBusy)
	}
	if record.user.String() != input.actor.Details().UserID || record.digest != input.digest || record.document != input.document {
		return nil, nil, nil, fault(f.IdempotencyKeyReused)
	}
	if record.state == kc.Committed {
		return x, record, nil, nil
	}
	grant, err := s.state().deps.Projects.RequireOwnerInTx(ctx, tx, input.actor, input.project, id.Mutate)
	if err != nil {
		return nil, nil, nil, portError(err)
	}
	if !grant.Matches(input.actor, input.project) {
		return nil, nil, nil, internal(nil)
	}
	var request, header []byte
	if err = x.QueryRow(ctx, `SELECT request,plan FROM agenteam_knowledge.commands WHERE project_id=$1 AND id=$2`, input.project.String(), record.id.String()).Scan(&request, &header); err != nil {
		return nil, nil, nil, unavailable(err)
	}
	wanted, err := json.Marshal(input.request)
	if err != nil {
		return nil, nil, nil, internal(err)
	}
	actualRequest, err := cursor.CanonicalJSON(request)
	if err != nil {
		return nil, nil, nil, internal(err)
	}
	wantedRequest, err := cursor.CanonicalJSON(wanted)
	if err != nil || !bytes.Equal(actualRequest, wantedRequest) {
		return nil, nil, nil, fault(f.ResourceBusy)
	}
	actualHeader, err := ev.DecodeHeader(header)
	if err != nil {
		return nil, nil, nil, internal(err)
	}
	a, err := json.Marshal(actualHeader)
	if err != nil {
		return nil, nil, nil, internal(err)
	}
	b, err := json.Marshal(intent.header)
	if err != nil || !bytes.Equal(a, b) {
		return nil, nil, nil, fault(f.ResourceBusy)
	}
	if input.name == kc.Create {
		if parent := input.request.Create.ParentDocumentID; parent != nil {
			current, err := loadDocument(ctx, x, input.project, *parent)
			if err != nil {
				return nil, nil, nil, err
			}
			if current.head.Active == nil {
				return nil, nil, nil, fault(f.NotFound)
			}
		}
		return x, record, nil, nil
	}
	current, err := loadDocument(ctx, x, input.project, input.document)
	if err != nil {
		return nil, nil, nil, err
	}
	if current.head.Active == nil {
		return nil, nil, nil, fault(f.NotFound)
	}
	if input.request.Expected == nil || current.head.Active.ContentVersion != *input.request.Expected {
		return nil, nil, nil, fault(f.VersionConflict)
	}
	return x, record, current, nil
}

func titleContentResult(input contentInput, intent contentIntent, current kc.DocumentRef, now f.Instant) (kc.DocumentRef, error) {
	if input.name != kc.Update || input.request.Update == nil || input.request.Update.Title == nil || input.request.Expected == nil || current.Validate() != nil || current.Status != kc.Active || current.ProjectID != input.project || current.ID != input.document || now.Validate() != nil {
		return kc.DocumentRef{}, internal(nil)
	}
	if current.ContentVersion != *input.request.Expected || current.ContentVersion == f.Version(math.MaxInt64) {
		return kc.DocumentRef{}, fault(f.VersionConflict)
	}
	if intent.header.AggregateVersion == nil || *intent.header.AggregateVersion != current.ContentVersion+1 || intent.header.AggregateID.String() != current.ID.String() || intent.header.Scope.ProjectID.String() != current.ProjectID.String() || *input.request.Update.Title == current.Title {
		return kc.DocumentRef{}, fault(f.ResourceBusy)
	}
	// Copy the current row, not the pre-discovery parent. Move deliberately
	// preserves content_version and may have committed since preparation.
	out := current
	if current.ParentDocumentID != nil {
		parent := *current.ParentDocumentID
		out.ParentDocumentID = &parent
	}
	out.Title = *input.request.Update.Title
	out.ContentVersion++
	out.IndexingStatus = kc.IndexPending
	if !now.Time().Before(out.UpdatedAt.Time()) {
		out.UpdatedAt = now
	}
	return out, portError(out.Validate())
}

func completeContentCommand(ctx context.Context, x postgres.SQLExecutor, record *commandRecord, document kc.DocumentRef, changed bool, at f.Instant) error {
	receipt := kc.MutationReceipt{Command: record.name, Document: &document, Changed: changed}
	if err := receiptForCommand(receipt, record); err != nil {
		return err
	}
	raw, err := json.Marshal(receipt)
	if err != nil {
		return internal(err)
	}
	tag, err := x.Exec(ctx, `UPDATE agenteam_knowledge.commands SET state='completed',receipt=$3::jsonb,request=NULL,plan=NULL,committed_at=$4
 WHERE project_id=$1 AND id=$2 AND state='planned'`, record.project.String(), record.id.String(), raw, at.Time())
	if err != nil {
		return unavailable(err)
	}
	if tag.RowsAffected() != 1 {
		return fault(f.ResourceBusy)
	}
	return nil
}

// contentPublicationResult uses the final current tree position. The measured
// object identity comes from the confirmed reservation, never request metadata.
func contentPublicationResult(input contentInput, intent contentIntent, current *documentRow, measured publicationMeasurement, attempt oc.UploadAttempt, now f.Instant) (kc.DocumentRef, []kc.ContentChange, error) {
	if intent.record == nil || input.request.Source == nil || measured.validate() != nil || attempt.Validate() != nil || now.Validate() != nil || intent.header.AggregateVersion == nil || intent.header.AggregateID.String() != input.document.String() || intent.header.Scope.ProjectID.String() != input.project.String() {
		return kc.DocumentRef{}, nil, internal(nil)
	}
	kind, err := publicationMedia(measured.Media)
	if err != nil {
		return kc.DocumentRef{}, nil, err
	}
	var out kc.DocumentRef
	changes := []kc.ContentChange{kc.SourceChanged}
	if input.name == kc.Create {
		if current != nil || input.request.Create == nil || *intent.header.AggregateVersion != 1 {
			return out, nil, internal(nil)
		}
		user, err := f.ParseID[id.User](input.actor.Details().UserID)
		if err != nil {
			return out, nil, internal(err)
		}
		creator, err := kc.NewCreatorRef(kc.CreatorDetails{Kind: id.Human, UserID: user})
		if err != nil {
			return out, nil, internal(err)
		}
		if now.Time().Before(intent.record.created.Time()) {
			now = intent.record.created
		}
		out = kc.DocumentRef{ID: input.document, ProjectID: input.project, Title: input.request.Create.Title, ContentVersion: 1, CreatedBy: creator, CreatedAt: now, UpdatedAt: now}
		if p := input.request.Create.ParentDocumentID; p != nil {
			copied := *p
			out.ParentDocumentID = &copied
		}
		changes = []kc.ContentChange{kc.ContentCreated}
	} else {
		if input.name != kc.Update || current == nil || current.head.Active == nil || input.request.Update == nil || input.request.Expected == nil {
			return out, nil, internal(nil)
		}
		out = *current.head.Active
		if out.Validate() != nil || out.ID != input.document || out.ProjectID != input.project || out.ContentVersion != *input.request.Expected || out.ContentVersion == f.Version(math.MaxInt64) || *intent.header.AggregateVersion != out.ContentVersion+1 {
			return kc.DocumentRef{}, nil, fault(f.VersionConflict)
		}
		if out.ObjectID == attempt.Details().ObjectID {
			return kc.DocumentRef{}, nil, internal(nil)
		}
		if p := out.ParentDocumentID; p != nil {
			copied := *p
			out.ParentDocumentID = &copied
		}
		if title := input.request.Update.Title; title != nil && *title != out.Title {
			out.Title = *title
			changes = []kc.ContentChange{kc.TitleChanged, kc.SourceChanged}
		}
		out.ContentVersion++
		if !now.Time().Before(out.UpdatedAt.Time()) {
			out.UpdatedAt = now
		}
	}
	out.SourceKind, out.MediaType, out.ObjectID = kind, measured.Media, attempt.Details().ObjectID
	out.Status, out.IndexingStatus = kc.Active, kc.IndexPending
	return out, changes, portError(out.Validate())
}

func publicationMatches(p publicationReservation, input contentInput, measured publicationMeasurement, attempt oc.UploadAttempt) bool {
	return p.phase == "uploaded" && input.request.Source != nil && publicationSourceEqual(p.source, *input.request.Source) && p.measurement != nil && *p.measurement == measured && p.attempt.Validate() == nil && p.attempt.Details() == attempt.Details()
}

func replacementCleanup(record *commandRecord, old *documentRow) (*cleanupRecord, error) {
	if old == nil {
		return nil, nil
	}
	if record == nil || record.name != kc.Update || old.head.Active == nil || old.head.Active.ProjectID != record.project || old.head.Active.ID != record.document || old.upload.Validate() != nil {
		return nil, internal(nil)
	}
	// One content command can replace only this one canonical object. Reuse its
	// typed UUID as the fixed cleanup identity instead of minting per retry.
	operation, err := f.ParseID[oc.CleanupOperation](record.id.String())
	if err != nil {
		return nil, internal(err)
	}
	return &cleanupRecord{id: operation, project: record.project, document: record.document, command: record.id, object: old.head.Active.ObjectID, upload: old.upload, reason: oc.ReplacedObject, phase: "reference"}, nil
}

func insertReplacementCleanup(ctx context.Context, x postgres.SQLExecutor, record *cleanupRecord) error {
	_, err := x.Exec(ctx, `INSERT INTO agenteam_knowledge.object_cleanup(id,project_id,document_id,command_id,object_id,upload_id,reason,phase)
 VALUES($1,$2,$3,$4,$5,$6,'replaced_object','reference') ON CONFLICT(id) DO NOTHING`, record.id.String(), record.project.String(), record.document.String(), record.command.String(), record.object.String(), record.upload.String())
	if err != nil {
		return unavailable(err)
	}
	actual, err := loadCleanup(ctx, x, record.id)
	if err != nil {
		return err
	}
	if actual == nil || *actual != *record {
		return fault(f.ResourceBusy)
	}
	return nil
}

func storePublishedDocument(ctx context.Context, x postgres.SQLExecutor, input contentInput, document kc.DocumentRef, attempt oc.UploadAttempt) error {
	var tag pgconn.CommandTag
	var err error
	if input.name == kc.Create {
		var parent any
		if document.ParentDocumentID != nil {
			parent = document.ParentDocumentID.String()
		}
		tag, err = x.Exec(ctx, `INSERT INTO agenteam_knowledge.documents(id,project_id,parent_document_id,title,content_version,source_kind,media_type,current_object_id,current_upload_id,status,indexing_status,creator_user_id,created_at,updated_at)
 VALUES($1,$2,$3,$4,1,$5,$6,$7,$8,'active','pending',$9,$10,$10) ON CONFLICT(id) DO NOTHING`, document.ID.String(), document.ProjectID.String(), parent, document.Title, string(document.SourceKind), document.MediaType, document.ObjectID.String(), attempt.Details().UploadID.String(), document.CreatedBy.Details().UserID.String(), document.CreatedAt.Time())
	} else {
		tag, err = x.Exec(ctx, `UPDATE agenteam_knowledge.documents SET title=$3,content_version=$4,source_kind=$5,media_type=$6,current_object_id=$7,current_upload_id=$8,indexing_status='pending',updated_at=$9
 WHERE project_id=$1 AND id=$2 AND status='active' AND content_version=$10`, document.ProjectID.String(), document.ID.String(), document.Title, int64(document.ContentVersion), string(document.SourceKind), document.MediaType, document.ObjectID.String(), attempt.Details().UploadID.String(), document.UpdatedAt.Time(), int64(*input.request.Expected))
	}
	if err != nil {
		return unavailable(err)
	}
	if tag.RowsAffected() != 1 {
		return fault(f.VersionConflict)
	}
	return nil
}

// finishContentPublication prepares the fixed event outside the final Tx, then
// atomically installs canonical metadata, publishes D05, retires the old gate,
// appends the event and commits the receipt/activity. A failed member rolls the
// entire transaction back; physical commit Unknown keeps its original cause.
func (s *Service) finishContentPublication(ctx context.Context, input contentInput, intent contentIntent, work publicationWork, prepared oc.PreparedPayload, attempt oc.UploadAttempt) (kc.DocumentRef, error) {
	if intent.record == nil || input.request.Source == nil || work.command != intent.record.id || attempt.Validate() != nil {
		return kc.DocumentRef{}, internal(nil)
	}
	if input.request.Source.Kind == kc.InputBusinessFile {
		return kc.DocumentRef{}, fault(f.DependencyUnbound)
	}
	measured, err := measurementOf(prepared)
	if err != nil {
		return kc.DocumentRef{}, err
	}
	st := s.state()
	locks, err := publicationLocks(input.actor, intent.record, work.source)
	if err != nil {
		return kc.DocumentRef{}, err
	}
	identity, err := kc.CommandIdentity(input.project, input.name, input.meta.IdempotencyKey)
	if err != nil {
		return kc.DocumentRef{}, portError(err)
	}
	cause, err := f.NewCommandsCause(identity)
	if err != nil {
		return kc.DocumentRef{}, portError(err)
	}
	var event ev.Event
	var replay *kc.DocumentRef
	var cleanup *cleanupRecord
	result := st.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := st.store.AcquireAll(ctx, tx, locks); err != nil {
			return portError(err)
		}
		x, record, current, err := s.currentContentIntent(ctx, tx, input, intent)
		if err != nil {
			return err
		}
		if record.state == kc.Committed {
			replay = record.receipt.Document
			return nil
		}
		if err = requirePublicationWork(ctx, x, work); err != nil {
			return err
		}
		publication, err := loadPublicationReservation(ctx, x, record)
		if err != nil {
			return err
		}
		if !publicationMatches(publication, input, measured, attempt) {
			return fault(f.ResourceBusy)
		}
		document, changes, err := contentPublicationResult(input, intent, current, measured, attempt, intent.header.OccurredAt)
		if err != nil {
			return err
		}
		cleanup, err = replacementCleanup(record, current)
		if err != nil {
			return err
		}
		event, err = st.deps.Events.ContentChanged(intent.header, kc.ContentChangedPayload{DocumentID: input.document, ContentVersion: document.ContentVersion, Changes: changes, ObjectID: document.ObjectID})
		if err != nil {
			return portError(err)
		}
		header, err := event.HeaderJSON()
		if err != nil {
			return internal(err)
		}
		if _, err = x.Exec(ctx, `INSERT INTO agenteam_knowledge.command_events(id,project_id,command_id,document_id,header,payload,event_type)
 VALUES($1,$2,$3,$4,$5::jsonb,$6::jsonb,$7) ON CONFLICT(id) DO NOTHING`, intent.header.EventID.String(), input.project.String(), record.id.String(), input.document.String(), header, event.PayloadBytes(), string(kc.ContentChangedEvent)); err != nil {
			return unavailable(err)
		}
		stored, err := loadCommandEvent(ctx, x, intent.header.EventID)
		if err != nil {
			return err
		}
		if stored.command.id != record.id {
			return fault(f.ResourceBusy)
		}
		_, _, _, err = eventBinding(input.actor, event.Summary(), stored)
		return err
	})
	if err = txError(result); err != nil {
		return kc.DocumentRef{}, err
	}
	if replay != nil {
		return *replay, nil
	}
	owner, err := oc.NewObjectOwner(oc.Knowledge, input.document.String(), input.project.String())
	if err != nil {
		return kc.DocumentRef{}, internal(err)
	}
	request, err := oc.NewOwnerAccess(oc.AccessRequestDetails{Operation: oc.PublishAccess, Actor: input.actor, Owner: owner, Intent: id.Mutate, Attempt: attempt})
	if err != nil {
		return kc.DocumentRef{}, internal(err)
	}
	publishPlan, err := st.deps.Uploads.DiscoverAccess(ctx, request)
	if err != nil {
		return kc.DocumentRef{}, portError(err)
	}
	plans := []oc.AccessLockPlan{publishPlan}
	var cleanupCause oc.ObjectCleanupCause
	if cleanup != nil {
		cleanupCause, err = cleanup.cause()
		if err != nil {
			return kc.DocumentRef{}, err
		}
		request, err := oc.NewCleanupReleaseAccess(oc.AccessRequestDetails{Operation: oc.ReleaseForCleanupAccess, Cleanup: cleanupCause, ObjectID: cleanup.object, UploadID: cleanup.upload})
		if err != nil {
			return kc.DocumentRef{}, internal(err)
		}
		plan, err := st.deps.Objects.DiscoverAccess(ctx, request)
		if err != nil {
			return kc.DocumentRef{}, portError(err)
		}
		plans = append(plans, plan)
	}
	eventPlan, err := st.deps.Outbox.PrepareAppend(ctx, input.actor, event)
	if err != nil {
		return kc.DocumentRef{}, portError(err)
	}
	locks, err = ob.NormalizeLocks(append(locks, eventPlan.Locks()...))
	if err != nil {
		return kc.DocumentRef{}, portError(err)
	}
	var out kc.DocumentRef
	result = st.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		locked, err := st.deps.Uploads.AcquireAccessPlansInTx(ctx, tx, plans, locks)
		if err != nil {
			return portError(err)
		}
		x, record, current, err := s.currentContentIntent(ctx, tx, input, intent)
		if err != nil {
			return err
		}
		if record.state == kc.Committed {
			out = *record.receipt.Document
			return nil
		}
		if err = requirePublicationWork(ctx, x, work); err != nil {
			return err
		}
		publication, err := loadPublicationReservation(ctx, x, record)
		if err != nil {
			return err
		}
		if !publicationMatches(publication, input, measured, attempt) {
			return fault(f.ResourceBusy)
		}
		now, err := dbNow(ctx, x)
		if err != nil {
			return err
		}
		var changes []kc.ContentChange
		if now.Time().Before(record.created.Time()) {
			now = record.created
		}
		out, changes, err = contentPublicationResult(input, intent, current, measured, attempt, now)
		if err != nil {
			return err
		}
		actualEvent, err := st.deps.Events.ContentChanged(intent.header, kc.ContentChangedPayload{DocumentID: input.document, ContentVersion: out.ContentVersion, Changes: changes, ObjectID: out.ObjectID})
		if err != nil {
			return portError(err)
		}
		if actualEvent.Summary().PayloadDigest != event.Summary().PayloadDigest {
			return fault(f.ResourceBusy)
		}
		actualCleanup, err := replacementCleanup(record, current)
		if err != nil {
			return err
		}
		if (actualCleanup == nil) != (cleanup == nil) || actualCleanup != nil && *actualCleanup != *cleanup {
			return fault(f.VersionConflict)
		}
		if err = storePublishedDocument(ctx, x, input, out, attempt); err != nil {
			return err
		}
		put, err := st.deps.Uploads.PublishVerifiedInTx(ctx, tx, input.actor, owner, attempt, publishPlan, locked)
		if err != nil {
			return portError(err)
		}
		if put.Meta.Validate() != nil || put.Meta.ID != out.ObjectID || put.Meta.Scope.Details().Kind != id.ProjectScope || put.Meta.Scope.Details().ProjectID != out.ProjectID.String() || put.Meta.State != oc.Available || put.Meta.MediaType != measured.Media || put.Meta.ByteSize != measured.Length || put.Meta.SHA256 != measured.SHA {
			return internal(nil)
		}
		if cleanup != nil {
			if err = insertReplacementCleanup(ctx, x, cleanup); err != nil {
				return err
			}
			if err = st.deps.ReferenceCleanup.ReleaseForCleanupInTx(ctx, tx, cleanupCause, cleanup.object, plans[1], locked); err != nil {
				return portError(err)
			}
			tag, err := x.Exec(ctx, `UPDATE agenteam_knowledge.object_cleanup SET phase='object' WHERE id=$1 AND phase='reference'`, cleanup.id.String())
			if err != nil {
				return unavailable(err)
			}
			if tag.RowsAffected() != 1 {
				return internal(nil)
			}
		}
		receipt, err := st.deps.Outbox.AppendEventInTx(ctx, tx, input.actor, event, eventPlan)
		if err != nil {
			return portError(err)
		}
		if receipt.EventID != intent.header.EventID || receipt.Sequence.Validate() != nil {
			return internal(nil)
		}
		raw, err := json.Marshal(put.Meta)
		if err != nil {
			return internal(err)
		}
		tag, err := x.Exec(ctx, `UPDATE agenteam_knowledge.publications SET object_meta=$2::jsonb,phase='published',source=NULL,source_lease_id=NULL WHERE command_id=$1 AND phase='uploaded'`, record.id.String(), raw)
		if err != nil {
			return unavailable(err)
		}
		if tag.RowsAffected() != 1 {
			return fault(f.ResourceBusy)
		}
		if err = completeContentCommand(ctx, x, record, out, true, now); err != nil {
			return err
		}
		return portError(st.deps.Activity.TouchActivityInTx(ctx, tx, input.actor))
	})
	if err = txError(result); err != nil {
		return kc.DocumentRef{}, err
	}
	return out, nil
}

// finishTitleContent is the no-upload content path. Its fixed event is staged
// before Outbox discovery; final metadata/event/receipt/activity share one Tx.
// The enclosing command call owns admission and remains registered throughout.
func (s *Service) finishTitleContent(ctx context.Context, input contentInput, intent contentIntent) (kc.DocumentRef, error) {
	return s.finishTitleOrReuse(ctx, input, intent, nil)
}

func (s *Service) finishTitleOrReuse(ctx context.Context, input contentInput, intent contentIntent, reuse *contentReuse) (kc.DocumentRef, error) {
	if (input.request.Source != nil) != (reuse != nil) || input.name != kc.Update || intent.record == nil {
		return kc.DocumentRef{}, internal(nil)
	}
	st := s.state()
	var sourceProject *id.ProjectID
	if reuse != nil {
		sourceProject = reuse.work.source
	}
	locks, err := publicationLocks(input.actor, intent.record, sourceProject)
	if err != nil {
		return kc.DocumentRef{}, err
	}
	identity, err := kc.CommandIdentity(input.project, input.name, input.meta.IdempotencyKey)
	if err != nil {
		return kc.DocumentRef{}, portError(err)
	}
	cause, err := f.NewCommandsCause(identity)
	if err != nil {
		return kc.DocumentRef{}, portError(err)
	}
	var event ev.Event
	var replay *kc.DocumentRef
	result := st.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := st.store.AcquireAll(ctx, tx, locks); err != nil {
			return portError(err)
		}
		x, record, current, err := s.currentContentIntent(ctx, tx, input, intent)
		if err != nil {
			return err
		}
		if record.state == kc.Committed {
			replay = record.receipt.Document
			return nil
		}
		if reuse != nil {
			if err = validateContentReuse(ctx, x, input, record, current, reuse); err != nil {
				return err
			}
		}
		document, err := titleContentResult(input, intent, *current.head.Active, intent.header.OccurredAt)
		if err != nil {
			return err
		}
		event, err = st.deps.Events.ContentChanged(intent.header, kc.ContentChangedPayload{DocumentID: input.document, ContentVersion: document.ContentVersion, Changes: []kc.ContentChange{kc.TitleChanged}, ObjectID: document.ObjectID})
		if err != nil {
			return portError(err)
		}
		header, err := event.HeaderJSON()
		if err != nil {
			return internal(err)
		}
		if _, err = x.Exec(ctx, `INSERT INTO agenteam_knowledge.command_events(id,project_id,command_id,document_id,header,payload,event_type)
 VALUES($1,$2,$3,$4,$5::jsonb,$6::jsonb,$7) ON CONFLICT(id) DO NOTHING`, intent.header.EventID.String(), input.project.String(), record.id.String(), input.document.String(), header, event.PayloadBytes(), string(kc.ContentChangedEvent)); err != nil {
			return unavailable(err)
		}
		// A retry accepts only the same original fixed event, not an arbitrary
		// row whose event ID happens to collide.
		stored, err := loadCommandEvent(ctx, x, intent.header.EventID)
		if err != nil {
			return err
		}
		if stored.command.id != record.id {
			return fault(f.ResourceBusy)
		}
		_, _, _, err = eventBinding(input.actor, event.Summary(), stored)
		return err
	})
	if err := txError(result); err != nil {
		return kc.DocumentRef{}, err
	}
	if replay != nil {
		return *replay, nil
	}
	plan, err := st.deps.Outbox.PrepareAppend(ctx, input.actor, event)
	if err != nil {
		return kc.DocumentRef{}, portError(err)
	}
	locks, err = ob.NormalizeLocks(append(locks, plan.Locks()...))
	if err != nil {
		return kc.DocumentRef{}, portError(err)
	}
	var out kc.DocumentRef
	result = st.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := st.store.AcquireAll(ctx, tx, locks); err != nil {
			return portError(err)
		}
		x, record, current, err := s.currentContentIntent(ctx, tx, input, intent)
		if err != nil {
			return err
		}
		if record.state == kc.Committed {
			out = *record.receipt.Document
			return nil
		}
		if reuse != nil {
			if err = validateContentReuse(ctx, x, input, record, current, reuse); err != nil {
				return err
			}
		}
		now, err := dbNow(ctx, x)
		if err != nil {
			return err
		}
		out, err = titleContentResult(input, intent, *current.head.Active, now)
		if err != nil {
			return err
		}
		tag, err := x.Exec(ctx, `UPDATE agenteam_knowledge.documents SET title=$3,content_version=$4,indexing_status='pending',updated_at=$5
 WHERE project_id=$1 AND id=$2 AND status='active' AND content_version=$6`, input.project.String(), input.document.String(), out.Title, int64(out.ContentVersion), out.UpdatedAt.Time(), int64(*input.request.Expected))
		if err != nil {
			return unavailable(err)
		}
		if tag.RowsAffected() != 1 {
			return fault(f.VersionConflict)
		}
		receipt, err := st.deps.Outbox.AppendEventInTx(ctx, tx, input.actor, event, plan)
		if err != nil {
			return portError(err)
		}
		if receipt.EventID != intent.header.EventID || receipt.Sequence.Validate() != nil {
			return internal(nil)
		}
		if reuse != nil {
			if err = cancelUnusedPublication(ctx, x, record); err != nil {
				return err
			}
		}
		if err = completeContentCommand(ctx, x, record, out, true, now); err != nil {
			return err
		}
		return portError(st.deps.Activity.TouchActivityInTx(ctx, tx, input.actor))
	})
	if err := txError(result); err != nil {
		return kc.DocumentRef{}, err
	}
	return out, nil
}

// contentReuse is minted from the measured prepared bytes and a real current
// Object.Stat result. It is only a comparison witness; the final Tx still
// checks current Owner, exact content version/pointer and original work.
type contentReuse struct {
	work     publicationWork
	digest   f.Digest
	object   oc.ObjectMeta
	upload   oc.UploadID
	version  f.Version
	title    string
	measured publicationMeasurement
}

func (s *Service) inspectContentReuse(ctx context.Context, input contentInput, intent contentIntent, work publicationWork, prepared oc.PreparedPayload) (*contentReuse, *kc.DocumentRef, error) {
	if input.name != kc.Update {
		return nil, nil, nil
	}
	measured, err := measurementOf(prepared)
	if err != nil {
		return nil, nil, err
	}
	locks, err := publicationLocks(input.actor, intent.record, work.source)
	if err != nil {
		return nil, nil, err
	}
	identity, err := kc.CommandIdentity(input.project, input.name, input.meta.IdempotencyKey)
	if err != nil {
		return nil, nil, portError(err)
	}
	cause, err := f.NewCommandsCause(identity)
	if err != nil {
		return nil, nil, portError(err)
	}
	st := s.state()
	var current *documentRow
	var publication publicationReservation
	var replay *kc.DocumentRef
	result := st.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := st.store.AcquireAll(ctx, tx, locks); err != nil {
			return portError(err)
		}
		x, record, row, err := s.currentContentIntent(ctx, tx, input, intent)
		if err != nil {
			return err
		}
		if record.state == kc.Committed {
			replay = record.receipt.Document
			return nil
		}
		if err = requirePublicationWork(ctx, x, work); err != nil {
			return err
		}
		publication, err = loadPublicationReservation(ctx, x, record)
		if err != nil {
			return err
		}
		current = row
		return nil
	})
	if err = txError(result); err != nil {
		return nil, nil, err
	}
	if replay != nil {
		return nil, replay, nil
	}
	if current == nil || current.head.Active == nil {
		return nil, nil, internal(nil)
	}
	owner, err := oc.NewObjectOwner(oc.Knowledge, input.document.String(), input.project.String())
	if err != nil {
		return nil, nil, internal(err)
	}
	actual, err := st.deps.Objects.StatObject(ctx, input.actor, owner, current.head.Active.ObjectID)
	if err != nil {
		return nil, nil, portError(err)
	}
	if actual.Validate() != nil || actual.State != oc.Available || actual.ID != current.head.Active.ObjectID || actual.Scope.Details().Kind != id.ProjectScope || actual.Scope.Details().ProjectID != input.project.String() || actual.MediaType != current.head.Active.MediaType {
		return nil, nil, internal(nil)
	}
	if actual.MediaType != measured.Media || actual.ByteSize != measured.Length || actual.SHA256 != measured.SHA {
		return nil, nil, nil
	}
	// The unchanged decision precedes reservation on every retry of this exact
	// input. An already reserved equal replacement is inconsistent, not a
	// reason to abandon a live D05 upload without retiring its durable gate.
	if publication.phase != "planned" {
		return nil, nil, fault(f.ResourceBusy)
	}
	return &contentReuse{work: work, digest: input.digest, object: actual, upload: current.upload, version: current.head.Active.ContentVersion, title: current.head.Active.Title, measured: measured}, nil, nil
}

func validateContentReuse(ctx context.Context, x postgres.SQLExecutor, input contentInput, record *commandRecord, current *documentRow, reuse *contentReuse) error {
	if reuse == nil || current == nil || current.head.Active == nil || input.request.Source == nil || reuse.digest != input.digest || reuse.work.command != record.id || reuse.object.Validate() != nil || reuse.object.State != oc.Available || reuse.object.Scope.Details().Kind != id.ProjectScope || reuse.object.Scope.Details().ProjectID != input.project.String() || reuse.measured.validate() != nil || reuse.object.MediaType != reuse.measured.Media || reuse.object.ByteSize != reuse.measured.Length || reuse.object.SHA256 != reuse.measured.SHA || current.head.Active.ObjectID != reuse.object.ID || current.head.Active.MediaType != reuse.object.MediaType || current.upload != reuse.upload || current.head.Active.ContentVersion != reuse.version || current.head.Active.Title != reuse.title {
		return fault(f.VersionConflict)
	}
	if err := requirePublicationWork(ctx, x, reuse.work); err != nil {
		return err
	}
	publication, err := loadPublicationReservation(ctx, x, record)
	if err != nil {
		return err
	}
	if publication.phase != "planned" || !publicationSourceEqual(publication.source, *input.request.Source) {
		return fault(f.ResourceBusy)
	}
	return nil
}

func cancelUnusedPublication(ctx context.Context, x postgres.SQLExecutor, record *commandRecord) error {
	tag, err := x.Exec(ctx, `UPDATE agenteam_knowledge.publications SET phase='cancelled',source=NULL,source_lease_id=NULL
 WHERE command_id=$1 AND phase='planned' AND object_id IS NULL AND source_lease_id IS NULL`, record.id.String())
	if err != nil {
		return unavailable(err)
	}
	if tag.RowsAffected() != 1 {
		return fault(f.ResourceBusy)
	}
	return nil
}

func (s *Service) finishContentReuse(ctx context.Context, input contentInput, intent contentIntent, reuse *contentReuse) (kc.DocumentRef, error) {
	if reuse == nil {
		return kc.DocumentRef{}, internal(nil)
	}
	// An actual title change still uses the ordinary fixed event/final Tx. A
	// source-equal no-op below neither changes the version nor touches Activity.
	if title := input.request.Update.Title; title != nil && *title != reuse.title {
		return s.finishTitleOrReuse(ctx, input, intent, reuse)
	}
	locks, err := publicationLocks(input.actor, intent.record, reuse.work.source)
	if err != nil {
		return kc.DocumentRef{}, err
	}
	identity, err := kc.CommandIdentity(input.project, input.name, input.meta.IdempotencyKey)
	if err != nil {
		return kc.DocumentRef{}, portError(err)
	}
	cause, err := f.NewCommandsCause(identity)
	if err != nil {
		return kc.DocumentRef{}, portError(err)
	}
	st := s.state()
	var out kc.DocumentRef
	result := st.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := st.store.AcquireAll(ctx, tx, locks); err != nil {
			return portError(err)
		}
		x, record, current, err := s.currentContentIntent(ctx, tx, input, intent)
		if err != nil {
			return err
		}
		if record.state == kc.Committed {
			out = *record.receipt.Document
			return nil
		}
		if err = validateContentReuse(ctx, x, input, record, current, reuse); err != nil {
			return err
		}
		out = *current.head.Active
		now, err := dbNow(ctx, x)
		if err != nil {
			return err
		}
		if now.Time().Before(record.created.Time()) {
			now = record.created
		}
		if err = cancelUnusedPublication(ctx, x, record); err != nil {
			return err
		}
		return completeContentCommand(ctx, x, record, out, false, now)
	})
	if err = txError(result); err != nil {
		return kc.DocumentRef{}, err
	}
	return out, nil
}

type command struct{}
type commandRecord struct {
	id        f.ID[command]
	project   id.ProjectID
	document  kc.DocumentID
	user      id.UserID
	name      kc.CommandName
	key       f.IdempotencyKey
	digest    f.Digest
	state     kc.LookupState
	receipt   *kc.MutationReceipt
	created   f.Instant
	committed *f.Instant
}

func scanCommand(row interface{ Scan(...any) error }) (*commandRecord, error) {
	var key, project, document, user, name, commandKey, digest, state string
	var raw []byte
	var created time.Time
	var committed *time.Time
	if err := row.Scan(&key, &project, &document, &user, &name, &commandKey, &digest, &state, &raw, &created, &committed); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, unavailable(err)
	}
	out := &commandRecord{name: kc.CommandName(name), key: f.IdempotencyKey(commandKey), digest: f.Digest(digest)}
	var err error
	if out.id, err = f.ParseID[command](key); err != nil {
		return nil, internal(err)
	}
	if out.project, err = f.ParseID[id.Project](project); err != nil {
		return nil, internal(err)
	}
	if out.document, err = f.ParseID[kc.Document](document); err != nil {
		return nil, internal(err)
	}
	if out.user, err = f.ParseID[id.User](user); err != nil {
		return nil, internal(err)
	}
	if out.name.Validate() != nil || out.key.Validate() != nil || out.digest.Validate() != nil {
		return nil, internal(nil)
	}
	if out.created, err = f.NewInstant(created); err != nil {
		return nil, internal(err)
	}
	switch state {
	case "planned":
		if raw != nil || committed != nil {
			return nil, internal(nil)
		}
		out.state = kc.InProgress
	case "completed":
		if len(raw) == 0 || len(raw) > 4<<20 || committed == nil || committed.Before(created) {
			return nil, internal(nil)
		}
		var receipt kc.MutationReceipt
		if err = json.Unmarshal(raw, &receipt); err != nil {
			return nil, internal(err)
		}
		if err = receiptForCommand(receipt, out); err != nil {
			return nil, err
		}
		at, err := f.NewInstant(*committed)
		if err != nil {
			return nil, internal(err)
		}
		out.state = kc.Committed
		out.receipt = &receipt
		out.committed = &at
	default:
		return nil, internal(nil)
	}
	return out, nil
}

func receiptForCommand(receipt kc.MutationReceipt, record *commandRecord) error {
	if receipt.Validate() != nil || receipt.Command != record.name {
		return internal(nil)
	}
	if receipt.Command == kc.DeleteSubtree {
		if *receipt.RootID != record.document {
			return internal(nil)
		}
	} else if receipt.Document.ID != record.document || receipt.Document.ProjectID != record.project {
		return internal(nil)
	}
	return nil
}

func loadCommand(ctx context.Context, x postgres.SQLExecutor, project id.ProjectID, name kc.CommandName, key f.IdempotencyKey) (*commandRecord, error) {
	return scanCommand(x.QueryRow(ctx, `SELECT id::text,project_id::text,document_id::text,actor_user_id::text,
 command_name,command_key,semantic_digest,state,receipt,created_at,committed_at
 FROM agenteam_knowledge.commands WHERE project_id=$1 AND command_name=$2 AND command_key=$3`, project.String(), string(name), string(key)))
}

func insertCompletedCommand(ctx context.Context, x postgres.SQLExecutor, actor id.Actor, project id.ProjectID, document kc.DocumentID, name kc.CommandName, key f.IdempotencyKey, semantic f.Digest, receipt kc.MutationReceipt, now f.Instant) error {
	if receipt.Validate() != nil || receipt.Command != name || semantic.Validate() != nil || now.Validate() != nil {
		return internal(nil)
	}
	raw, err := json.Marshal(receipt)
	if err != nil {
		return internal(err)
	}
	if len(raw) > 4<<20 {
		return fault(f.ResourceBusy)
	}
	operation, err := f.NewID[command]()
	if err != nil {
		return unavailable(err)
	}
	_, err = x.Exec(ctx, `INSERT INTO agenteam_knowledge.commands
 (id,project_id,document_id,actor_user_id,command_name,command_key,semantic_digest,state,receipt,created_at,committed_at)
 VALUES($1,$2,$3,$4,$5,$6,$7,'completed',$8,$9,$9)`, operation.String(), project.String(), document.String(), actor.Details().UserID, string(name), string(key), semantic.String(), raw, now.Time())
	return portError(err)
}

func (s *Service) LookupCommand(ctx context.Context, actor id.Actor, request kc.LookupRequest) (kc.CommandLookup, error) {
	if err := readInput(ctx, actor, request.ProjectID); err != nil {
		return kc.CommandLookup{}, err
	}
	if request.Validate() != nil {
		return kc.CommandLookup{}, fault(f.InvalidArgument)
	}
	ctx, done, err := s.begin(ctx)
	if err != nil {
		return kc.CommandLookup{}, err
	}
	defer done()
	identity, err := kc.CommandIdentity(request.ProjectID, request.Command, request.Key)
	if err != nil {
		return kc.CommandLookup{}, portError(err)
	}
	cause, err := f.NewCommandsCause(identity)
	if err != nil {
		return kc.CommandLookup{}, portError(err)
	}
	locks, err := scopeLocks(actor, request.ProjectID, false)
	if err != nil {
		return kc.CommandLookup{}, err
	}
	commandKey, err := f.CommandLock(identity)
	if err != nil {
		return kc.CommandLookup{}, portError(err)
	}
	locks, err = ob.NormalizeLocks(append(locks, f.LockRequest{Key: commandKey, Mode: f.Exclusive}))
	if err != nil {
		return kc.CommandLookup{}, portError(err)
	}
	out := kc.CommandLookup{State: kc.NotObserved}
	result := s.state().store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := s.state().store.AcquireAll(ctx, tx, locks); err != nil {
			return portError(err)
		}
		x, err := s.readScope(ctx, tx, actor, request.ProjectID)
		if err != nil {
			return err
		}
		row, err := loadCommand(ctx, x, request.ProjectID, request.Command, request.Key)
		if err != nil {
			return err
		}
		if row == nil {
			return nil
		}
		if row.project != request.ProjectID || row.name != request.Command || row.key != request.Key {
			return internal(nil)
		}
		if row.user.String() != actor.Details().UserID || row.digest != request.SemanticDigest {
			return fault(f.IdempotencyKeyReused)
		}
		out = kc.CommandLookup{State: row.state, Receipt: row.receipt}
		return out.Validate()
	})
	if err = txError(result); err != nil {
		return kc.CommandLookup{}, err
	}
	return out, nil
}
