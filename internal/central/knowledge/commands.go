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
