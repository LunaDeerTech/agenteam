package knowledge

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"io"
	"log/slog"
	"math"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	kc "github.com/LunaDeerTech/agenteam/internal/central/knowledge/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	ob "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/jackc/pgx/v5"
)

// The persisted input identifies the exact original source. Raw text and upload
// readers are never retained here. Recovery of an unreserved stream requires
// the caller to supply the same semantic input again; a descriptor is not bytes.
type publicationSource struct {
	WireVersion int              `json:"format"`
	Kind        kc.InputKind     `json:"kind"`
	Media       string           `json:"media,omitempty"`
	Length      *f.Progress      `json:"length,omitempty"`
	SHA         *f.Digest        `json:"sha256,omitempty"`
	Business    *publicationFile `json:"business,omitempty"`
}
type publicationFile struct {
	Kind      oc.SourceKind       `json:"kind"`
	Project   string              `json:"project,omitempty"`
	Artifact  string              `json:"artifact,omitempty"`
	File      string              `json:"file,omitempty"`
	Document  string              `json:"document,omitempty"`
	Execution string              `json:"execution,omitempty"`
	Payload   string              `json:"payload,omitempty"`
	Revision  f.Version           `json:"revision,omitempty"`
	Receipt   *publicationReceipt `json:"receipt,omitempty"`
}
type publicationReceipt struct {
	ID     oc.ReceiptID    `json:"id"`
	Upload oc.UploadID     `json:"upload"`
	Object oc.ObjectID     `json:"object"`
	Owner  oc.OwnerDetails `json:"owner"`
	Cause  string          `json:"cause"`
}

func (publicationSource) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "knowledge_publication_source")
}
func (publicationSource) LogValue() slog.Value {
	return slog.StringValue("knowledge_publication_source")
}

func describePublicationSource(source kc.SourceInput) (publicationSource, error) {
	d, err := source.Details()
	if err != nil {
		return publicationSource{}, portError(err)
	}
	out := publicationSource{WireVersion: 1, Kind: d.Kind, Media: d.MediaType}
	switch d.Kind {
	case kc.InputText:
		length := f.Progress(len(*d.Text))
		sum := sha256.Sum256([]byte(*d.Text))
		digest := f.Digest("sha256:" + hex.EncodeToString(sum[:]))
		out.Length, out.SHA = &length, &digest
	case kc.InputUpload:
		length, digest := d.Upload.Length, d.Upload.ExpectedSHA256
		out.Length, out.SHA = &length, &digest
	case kc.InputBusinessFile:
		b := d.BusinessFile.Details()
		out.Business = &publicationFile{Kind: b.Kind, Artifact: b.ArtifactID, File: b.FileID, Document: b.DocumentID, Execution: b.ExecutionID, Payload: b.PayloadID, Revision: b.Revision}
		if b.ProjectID.Validate() == nil {
			out.Business.Project = b.ProjectID.String()
		}
		if b.Kind == oc.UploadedObject {
			r := b.Receipt.Details()
			out.Business.Receipt = &publicationReceipt{r.ID, r.UploadID, r.ObjectID, r.Owner.Details(), r.CreationCause}
		}
	}
	return out, out.validate()
}

func publicationMedia(media string) (kc.SourceKind, error) {
	switch media {
	case "text/plain", "text/markdown":
		return kc.Text, nil
	case "application/pdf", "application/vnd.openxmlformats-officedocument.wordprocessingml.document":
		return kc.File, nil
	default:
		return "", fault(f.UnsupportedMediaType)
	}
}

func (p publicationSource) validate() error {
	if p.WireVersion != 1 {
		return internal(nil)
	}
	switch p.Kind {
	case kc.InputText, kc.InputUpload:
		kind, err := publicationMedia(p.Media)
		if err != nil || p.Kind == kc.InputText && kind != kc.Text || p.Business != nil || p.Length == nil || p.Length.Validate() != nil || int64(*p.Length) > oc.MaxObjectSize || p.SHA == nil || p.SHA.Validate() != nil {
			return internal(err)
		}
	case kc.InputBusinessFile:
		if p.Media != "" || p.Length != nil || p.SHA != nil || p.Business == nil {
			return internal(nil)
		}
		if _, err := p.Business.reference(); err != nil {
			return err
		}
	default:
		return internal(nil)
	}
	return nil
}

func (p publicationFile) reference() (oc.BusinessFileRef, error) {
	d := oc.BusinessFileDetails{Kind: p.Kind, ArtifactID: p.Artifact, FileID: p.File, DocumentID: p.Document, ExecutionID: p.Execution, PayloadID: p.Payload, Revision: p.Revision}
	var err error
	if p.Project != "" {
		d.ProjectID, err = f.ParseID[id.Project](p.Project)
		if err != nil {
			return oc.BusinessFileRef{}, internal(err)
		}
	}
	if p.Receipt != nil {
		r := p.Receipt
		owner, err := oc.NewObjectOwner(r.Owner.Kind, r.Owner.ID, r.Owner.ProjectID)
		if err != nil {
			return oc.BusinessFileRef{}, internal(err)
		}
		d.Receipt, err = oc.NewUploadReceipt(oc.ReceiptDetails{ID: r.ID, UploadID: r.Upload, ObjectID: r.Object, Owner: owner, CreationCause: r.Cause})
		if err != nil {
			return oc.BusinessFileRef{}, internal(err)
		}
	}
	ref, err := oc.NewBusinessFileRef(d)
	return ref, portError(err)
}

func (p publicationSource) sourceProject() (*id.ProjectID, error) {
	if err := p.validate(); err != nil {
		return nil, err
	}
	if p.Business == nil {
		return nil, nil
	}
	project := p.Business.Project
	if p.Business.Receipt != nil {
		project = p.Business.Receipt.Owner.ProjectID
	}
	if project == "" {
		return nil, nil
	}
	value, err := f.ParseID[id.Project](project)
	if err != nil {
		return nil, internal(err)
	}
	return &value, nil
}

func decodePublicationSource(raw []byte) (publicationSource, error) {
	var out publicationSource
	if len(raw) == 0 || len(raw) > 16<<10 {
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

type publicationAttempt struct{}
type publicationWork struct {
	command f.ID[command]
	project id.ProjectID
	source  *id.ProjectID
	process ob.ProcessID
	attempt f.ID[publicationAttempt]
	fence   int64
	phase   string
}

func loadPublicationWork(ctx context.Context, x postgres.SQLExecutor, commandID f.ID[command]) (*publicationWork, error) {
	var cmd, project, process, attempt, phase string
	var source *string
	var fence int64
	err := x.QueryRow(ctx, `SELECT command_id::text,project_id::text,source_project_id::text,
 process_id::text,attempt_id::text,fence,phase FROM agenteam_knowledge.work_claims WHERE command_id=$1`, commandID.String()).Scan(&cmd, &project, &source, &process, &attempt, &fence, &phase)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, unavailable(err)
	}
	out := &publicationWork{fence: fence, phase: phase}
	if out.command, err = f.ParseID[command](cmd); err != nil {
		return nil, internal(err)
	}
	if out.project, err = f.ParseID[id.Project](project); err != nil {
		return nil, internal(err)
	}
	if out.process, err = f.ParseID[ob.Process](process); err != nil {
		return nil, internal(err)
	}
	if out.attempt, err = f.ParseID[publicationAttempt](attempt); err != nil {
		return nil, internal(err)
	}
	if source != nil {
		value, err := f.ParseID[id.Project](*source)
		if err != nil {
			return nil, internal(err)
		}
		out.source = &value
	}
	if out.command != commandID || fence < 1 || phase != "active" && phase != "joined" {
		return nil, internal(nil)
	}
	return out, nil
}

func (w publicationWork) equal(other publicationWork) bool {
	return w.command == other.command && w.project == other.project && w.process == other.process && w.attempt == other.attempt && w.fence == other.fence && w.phase == other.phase && equalProjectPointer(w.source, other.source)
}
func (w publicationWork) clone() publicationWork {
	if w.source != nil {
		origin := *w.source
		w.source = &origin
	}
	return w
}
func equalProjectPointer(a, b *id.ProjectID) bool {
	return a == nil && b == nil || a != nil && b != nil && *a == *b
}

// claimContentPublication establishes the durable work identity before source
// resolution or preparation. Stop proof is obtained outside a transaction and
// the complete original row is checked again under the second transaction's
// full union. A nonzero work returned with Unknown still belongs to this call:
// the caller must record its actual no-I/O retirement, never begin preparation.
func (s *Service) claimContentPublication(ctx context.Context, input contentInput, intent contentIntent) (work publicationWork, replay *kc.DocumentRef, err error) {
	if intent.record == nil || input.request.Source == nil {
		return work, nil, internal(nil)
	}
	origin, err := input.request.Source.sourceProject()
	if err != nil {
		return work, nil, err
	}
	locks, err := publicationLocks(input.actor, intent.record, origin)
	if err != nil {
		return work, nil, err
	}
	identity, err := kc.CommandIdentity(input.project, input.name, input.meta.IdempotencyKey)
	if err != nil {
		return work, nil, portError(err)
	}
	cause, err := f.NewCommandsCause(identity)
	if err != nil {
		return work, nil, portError(err)
	}
	st := s.state()
	var previous *publicationWork
	read := func(ctx context.Context, tx f.Tx) (postgres.SQLExecutor, *commandRecord, error) {
		if err := st.store.AcquireAll(ctx, tx, locks); err != nil {
			return nil, nil, portError(err)
		}
		x, record, _, err := s.currentContentIntent(ctx, tx, input, intent)
		if err != nil {
			return nil, nil, err
		}
		if record.state == kc.Committed {
			replay = record.receipt.Document
		} else if origin != nil && *origin != input.project {
			if _, err = s.readScope(ctx, tx, input.actor, *origin); err != nil {
				return nil, nil, err
			}
		}
		return x, record, nil
	}
	result := st.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		x, _, err := read(ctx, tx)
		if err != nil || replay != nil {
			return err
		}
		previous, err = loadPublicationWork(ctx, x, intent.record.id)
		return err
	})
	if err = txError(result); err != nil || replay != nil {
		return work, replay, err
	}
	proof, err := s.stoppedPublication(ctx, previous)
	if err != nil {
		return work, nil, err
	}
	result = st.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		_, record, err := read(ctx, tx)
		if err != nil || replay != nil {
			return err
		}
		work, err = s.claimPublicationInTx(ctx, tx, input.actor, record, origin, previous, proof)
		return err
	})
	return work, replay, txError(result)
}

// publicationReader validates text incrementally without retaining the body.
// Read and Close have separate serialization so cancellation can interrupt a
// blocked provider Read. Close returns the original result on every call; a
// provider's ignored Close failure cannot become a false join in our runtime.
type publicationReader struct {
	source    io.ReadCloser
	text      bool
	readMu    sync.Mutex
	hash      hash.Hash
	length    int64
	limit     int64
	pending   [utf8.UTFMax]byte
	used      int
	terminal  error
	closeOnce sync.Once
	closeErr  error
}

func newPublicationReader(source io.ReadCloser, text bool, length int64) *publicationReader {
	return &publicationReader{source: source, text: text, limit: length, hash: sha256.New()}
}

func (r *publicationReader) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "knowledge_publication_reader")
}
func (r *publicationReader) LogValue() slog.Value {
	return slog.StringValue("knowledge_publication_reader")
}

func (r *publicationReader) Read(p []byte) (int, error) {
	r.readMu.Lock()
	defer r.readMu.Unlock()
	if len(p) == 0 {
		return 0, nil
	}
	if r.terminal != nil {
		return 0, r.terminal
	}
	n, err := r.source.Read(p)
	if n < 0 || n > len(p) {
		r.terminal = internal(nil)
		return 0, r.terminal
	}
	r.length += int64(n)
	_, _ = r.hash.Write(p[:n])
	if r.length > r.limit || r.text && !r.acceptUTF8(p[:n]) {
		err = fault(f.InvalidArgument)
	}
	if err == io.EOF && (r.length != r.limit || r.used != 0) {
		err = fault(f.InvalidArgument)
	}
	if err != nil {
		r.terminal = err
	}
	return n, err
}

func (r *publicationReader) acceptUTF8(p []byte) bool {
	for len(p) != 0 {
		if r.used != 0 {
			r.pending[r.used] = p[0]
			r.used++
			p = p[1:]
			if !utf8.FullRune(r.pending[:r.used]) {
				continue
			}
			value, size := utf8.DecodeRune(r.pending[:r.used])
			if value == utf8.RuneError && size == 1 {
				return false
			}
			r.used = 0
			continue
		}
		if !utf8.FullRune(p) {
			r.used = copy(r.pending[:], p)
			break
		}
		value, size := utf8.DecodeRune(p)
		if value == utf8.RuneError && size == 1 {
			return false
		}
		p = p[size:]
	}
	return true
}

func (r *publicationReader) Close() error {
	r.closeOnce.Do(func() { r.closeErr = r.source.Close() })
	return r.closeErr
}

func (r *publicationReader) measured(payload oc.PreparedPayload, descriptor publicationSource) bool {
	r.readMu.Lock()
	defer r.readMu.Unlock()
	if r.terminal != io.EOF || payload.Validate() != nil || descriptor.Length == nil || descriptor.SHA == nil {
		return false
	}
	d := payload.Details()
	return d.MediaType == descriptor.Media && d.Length == r.length && d.Length == int64(*descriptor.Length) &&
		d.SHA256 == *descriptor.SHA && d.SHA256.String() == "sha256:"+hex.EncodeToString(r.hash.Sum(nil))
}

// prepareDirectPublication consumes only text/upload input after the caller's
// confirmed work claim. Business files use the separate leased source path;
// they are never silently reduced to a public ObjectID or unleased reader.
func (s *Service) prepareDirectPublication(ctx context.Context, input contentInput, intent contentIntent, source kc.SourceInput, work publicationWork, retirement *publicationRetirement) (oc.PreparedPayload, error) {
	if ctx == nil || retirement == nil || s.state() == nil || retirement.service != s || !retirement.work.equal(work) || intent.record == nil || work.command != intent.record.id || intent.record.document != input.document || intent.record.project != input.project || intent.record.digest != input.digest || intent.record.key != input.meta.IdempotencyKey || intent.record.user.String() != input.actor.Details().UserID || work.command.Validate() != nil || work.project != input.project || work.process != s.state().deps.Processes.CurrentProcess() || work.phase != "active" || input.request.Source == nil {
		return oc.PreparedPayload{}, internal(nil)
	}
	retirement.mu.Lock()
	joined := retirement.joined
	retirement.mu.Unlock()
	if joined {
		return oc.PreparedPayload{}, fault(f.ResourceBusy)
	}
	if err := ctx.Err(); err != nil {
		return oc.PreparedPayload{}, unavailable(err)
	}
	actual, err := describePublicationSource(source)
	if err != nil {
		return oc.PreparedPayload{}, err
	}
	expected := *input.request.Source
	origin, err := expected.sourceProject()
	if err != nil {
		return oc.PreparedPayload{}, err
	}
	if !equalProjectPointer(origin, work.source) {
		return oc.PreparedPayload{}, fault(f.ResourceBusy)
	}
	a, err := json.Marshal(actual)
	if err != nil {
		return oc.PreparedPayload{}, internal(err)
	}
	b, err := json.Marshal(expected)
	if err != nil || !bytes.Equal(a, b) {
		return oc.PreparedPayload{}, fault(f.IdempotencyKeyReused)
	}
	if actual.Kind == kc.InputBusinessFile {
		return oc.PreparedPayload{}, fault(f.DependencyUnbound)
	}
	owner, err := oc.NewObjectOwner(oc.Knowledge, input.document.String(), input.project.String())
	if err != nil {
		return oc.PreparedPayload{}, portError(err)
	}
	details, err := source.Details()
	if err != nil {
		return oc.PreparedPayload{}, portError(err)
	}
	var body io.ReadCloser
	if actual.Kind == kc.InputText {
		body = io.NopCloser(strings.NewReader(*details.Text))
	} else {
		body, err = source.TakeUploadBody()
		if err != nil {
			return oc.PreparedPayload{}, portError(err)
		}
	}
	kind, err := publicationMedia(actual.Media)
	if err != nil {
		return oc.PreparedPayload{}, err
	}
	reader := newPublicationReader(body, kind == kc.Text, int64(*actual.Length))
	if err = retirement.own(reader.Close); err != nil {
		return oc.PreparedPayload{}, err
	}
	prepared, prepareErr := s.state().deps.Uploads.PreparePayload(ctx, input.actor, owner, actual.Media, int64(*actual.Length), actual.SHA, reader)
	// Even a misbehaving provider returning a handle with an error leaves an
	// owned preparation to retire. Do not let later checks discard that handle.
	if prepared.Validate() == nil {
		if err = retirement.own(func() error { return s.state().deps.Uploads.DiscardPrepared(prepared) }); err != nil {
			return oc.PreparedPayload{}, err
		}
	}
	closeErr := reader.Close()
	if prepareErr != nil {
		return oc.PreparedPayload{}, portError(prepareErr)
	}
	if closeErr != nil {
		return oc.PreparedPayload{}, portError(closeErr)
	}
	if err = ctx.Err(); err != nil {
		return oc.PreparedPayload{}, unavailable(err)
	}
	if !reader.measured(prepared, actual) {
		return oc.PreparedPayload{}, internal(nil)
	}
	return prepared, nil
}

type publicationMeasurement struct {
	Media  string     `json:"media"`
	Length f.Progress `json:"length"`
	SHA    f.Digest   `json:"sha256"`
}

func (m publicationMeasurement) validate() error {
	if _, err := publicationMedia(m.Media); err != nil {
		return internal(err)
	}
	if m.Length.Validate() != nil || int64(m.Length) > oc.MaxObjectSize || m.SHA.Validate() != nil {
		return internal(nil)
	}
	return nil
}
func measurementOf(p oc.PreparedPayload) (publicationMeasurement, error) {
	if p.Validate() != nil {
		return publicationMeasurement{}, internal(nil)
	}
	d := p.Details()
	m := publicationMeasurement{d.MediaType, f.Progress(d.Length), d.SHA256}
	return m, m.validate()
}
func decodePublicationMeasurement(raw []byte) (publicationMeasurement, error) {
	var out publicationMeasurement
	if len(raw) == 0 || len(raw) > 4096 {
		return out, internal(nil)
	}
	canonical, err := cursor.CanonicalJSON(raw)
	if err != nil {
		return out, internal(err)
	}
	var fields map[string]json.RawMessage
	if err = json.Unmarshal(canonical, &fields); err != nil || len(fields) != 3 || fields["media"] == nil || fields["length"] == nil || fields["sha256"] == nil {
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

type publicationReservation struct {
	phase       string
	source      publicationSource
	measurement *publicationMeasurement
	attempt     oc.UploadAttempt
}

func loadPublicationReservation(ctx context.Context, x postgres.SQLExecutor, record *commandRecord) (publicationReservation, error) {
	var out publicationReservation
	var source, measurement []byte
	var project, document string
	var object, upload, attempt *string
	err := x.QueryRow(ctx, `SELECT project_id::text,document_id::text,source,object_meta,object_id::text,upload_id::text,attempt_id::text,phase
 FROM agenteam_knowledge.publications WHERE command_id=$1`, record.id.String()).Scan(&project, &document, &source, &measurement, &object, &upload, &attempt, &out.phase)
	if err != nil {
		return out, unavailable(err)
	}
	if project != record.project.String() || document != record.document.String() {
		return out, internal(nil)
	}
	if out.source, err = decodePublicationSource(source); err != nil {
		return out, err
	}
	switch out.phase {
	case "planned":
		if len(measurement) != 0 || object != nil || upload != nil || attempt != nil {
			return out, internal(nil)
		}
	case "reserved", "uploaded":
		if object == nil || upload == nil || attempt == nil {
			return out, internal(nil)
		}
		var d oc.AttemptDetails
		if d.ObjectID, err = f.ParseID[oc.StoredObject](*object); err != nil {
			return out, internal(err)
		}
		if d.UploadID, err = f.ParseID[oc.Upload](*upload); err != nil {
			return out, internal(err)
		}
		if d.ID, err = f.ParseID[oc.Attempt](*attempt); err != nil {
			return out, internal(err)
		}
		if out.attempt, err = oc.NewUploadAttempt(d); err != nil {
			return out, internal(err)
		}
		m, err := decodePublicationMeasurement(measurement)
		if err != nil {
			return out, err
		}
		out.measurement = &m
	default:
		return out, fault(f.ResourceBusy)
	}
	return out, nil
}
func publicationSourceEqual(a, b publicationSource) bool {
	x, err := json.Marshal(a)
	if err != nil {
		return false
	}
	y, err := json.Marshal(b)
	return err == nil && bytes.Equal(x, y)
}

// reserveContentPublication composes D05's reservation in the original domain
// transaction. Its private D05 key is the durable Knowledge command UUID. The
// caller may send only after this function returns a confirmed success.
func (s *Service) reserveContentPublication(ctx context.Context, input contentInput, intent contentIntent, work publicationWork, prepared oc.PreparedPayload) (attempt oc.UploadAttempt, replay *kc.DocumentRef, err error) {
	if intent.record == nil || work.command != intent.record.id || input.request.Source == nil {
		return attempt, nil, internal(nil)
	}
	measured, err := measurementOf(prepared)
	if err != nil {
		return attempt, nil, err
	}
	owner, err := oc.NewObjectOwner(oc.Knowledge, input.document.String(), input.project.String())
	if err != nil {
		return attempt, nil, portError(err)
	}
	meta := f.CommandMeta{RequestID: input.meta.RequestID, IdempotencyKey: f.IdempotencyKey(intent.record.id.String())}
	request, err := oc.NewOwnerAccess(oc.AccessRequestDetails{Operation: oc.ReserveAccess, Actor: input.actor, Owner: owner, Intent: id.Mutate, Command: &meta, Prepared: prepared})
	if err != nil {
		return attempt, nil, portError(err)
	}
	st := s.state()
	plan, err := st.deps.Uploads.DiscoverAccess(ctx, request)
	if err != nil {
		return attempt, nil, portError(err)
	}
	locks, err := publicationLocks(input.actor, intent.record, work.source)
	if err != nil {
		return attempt, nil, err
	}
	identity, err := kc.CommandIdentity(input.project, input.name, input.meta.IdempotencyKey)
	if err != nil {
		return attempt, nil, portError(err)
	}
	cause, err := f.NewCommandsCause(identity)
	if err != nil {
		return attempt, nil, portError(err)
	}
	result := st.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		locked, err := st.deps.Uploads.AcquireAccessPlansInTx(ctx, tx, []oc.AccessLockPlan{plan}, locks)
		if err != nil {
			return portError(err)
		}
		x, record, _, err := s.currentContentIntent(ctx, tx, input, intent)
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
		if !publicationSourceEqual(publication.source, *input.request.Source) || publication.measurement != nil && *publication.measurement != measured {
			return fault(f.IdempotencyKeyReused)
		}
		attempt, err = st.deps.Uploads.ReserveUploadInTx(ctx, tx, input.actor, owner, meta, prepared, plan, locked)
		if err != nil {
			return portError(err)
		}
		if attempt.Validate() != nil {
			return internal(nil)
		}
		d := attempt.Details()
		if len(plan.Details().Objects) != 1 || plan.Details().Objects[0] != d.ObjectID {
			return internal(nil)
		}
		raw, err := json.Marshal(measured)
		if err != nil {
			return internal(err)
		}
		tag, err := x.Exec(ctx, `UPDATE agenteam_knowledge.publications SET object_id=$2,upload_id=$3,attempt_id=$4,object_meta=$5::jsonb,phase='reserved'
 WHERE command_id=$1 AND phase IN ('planned','reserved','uploaded')`, record.id.String(), d.ObjectID.String(), d.UploadID.String(), d.ID.String(), raw)
		if err != nil {
			return unavailable(err)
		}
		if tag.RowsAffected() != 1 {
			return fault(f.ResourceBusy)
		}
		return nil
	})
	return attempt, replay, txError(result)
}

// sendContentPublication runs no I/O inside a transaction. The exact returned
// physical attempt is checked before storing the uploaded checkpoint; neither
// a nil error for a different attempt nor an unknown checkpoint is completion.
func (s *Service) sendContentPublication(ctx context.Context, input contentInput, intent contentIntent, work publicationWork, prepared oc.PreparedPayload, attempt oc.UploadAttempt) error {
	if intent.record == nil || work.command != intent.record.id || attempt.Validate() != nil {
		return internal(nil)
	}
	measured, err := measurementOf(prepared)
	if err != nil {
		return err
	}
	owner, err := oc.NewObjectOwner(oc.Knowledge, input.document.String(), input.project.String())
	if err != nil {
		return portError(err)
	}
	st := s.state()
	actual, err := st.deps.Uploads.UploadPrepared(ctx, input.actor, owner, prepared, attempt)
	if err != nil {
		return portError(err)
	}
	if actual.Validate() != nil || actual.Details() != attempt.Details() {
		return internal(nil)
	}
	locks, err := publicationLocks(input.actor, intent.record, work.source)
	if err != nil {
		return err
	}
	identity, err := kc.CommandIdentity(input.project, input.name, input.meta.IdempotencyKey)
	if err != nil {
		return portError(err)
	}
	cause, err := f.NewCommandsCause(identity)
	if err != nil {
		return portError(err)
	}
	return txError(st.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := st.store.AcquireAll(ctx, tx, locks); err != nil {
			return portError(err)
		}
		x, record, _, err := s.currentContentIntent(ctx, tx, input, intent)
		if err != nil {
			return err
		}
		if record.state == kc.Committed {
			return nil
		}
		if err = requirePublicationWork(ctx, x, work); err != nil {
			return err
		}
		publication, err := loadPublicationReservation(ctx, x, record)
		if err != nil {
			return err
		}
		if publication.attempt.Validate() != nil || publication.attempt.Details() != attempt.Details() || publication.measurement == nil || *publication.measurement != measured {
			return fault(f.ResourceBusy)
		}
		tag, err := x.Exec(ctx, `UPDATE agenteam_knowledge.publications SET phase='uploaded' WHERE command_id=$1 AND phase IN ('reserved','uploaded')`, record.id.String())
		if err != nil {
			return unavailable(err)
		}
		if tag.RowsAffected() != 1 {
			return fault(f.ResourceBusy)
		}
		return nil
	}))
}

// Only a successful real ProcessAuthority call can construct a stopped proof.
// It is bound to the original persisted attempt/fence and rechecked in the
// caller's transaction; clock age and a missing in-memory call are not proof.
type publicationStopProof struct {
	original    publicationWork
	joinedLocal bool
}

func (s *Service) stoppedPublication(ctx context.Context, old *publicationWork) (*publicationStopProof, error) {
	if old == nil || old.phase == "joined" {
		return nil, nil
	}
	if old.phase != "active" {
		return nil, fault(f.ResourceBusy)
	}
	if old.process == s.state().deps.Processes.CurrentProcess() {
		st := s.state()
		st.mu.Lock()
		joined, ok := st.joinedPublications[old.command]
		st.mu.Unlock()
		if !ok || !joined.equal(*old) {
			return nil, fault(f.ResourceBusy)
		}
		return &publicationStopProof{original: old.clone(), joinedLocal: true}, nil
	}
	if err := s.state().deps.Processes.ConfirmStopped(ctx, old.process); err != nil {
		return nil, portError(err)
	}
	return &publicationStopProof{original: old.clone()}, nil
}

func publicationLocks(actor id.Actor, record *commandRecord, source *id.ProjectID) ([]f.LockRequest, error) {
	if record == nil || record.id.Validate() != nil || record.document.Validate() != nil || record.name != kc.Create && record.name != kc.Update || record.user.String() != actor.Details().UserID {
		return nil, internal(nil)
	}
	locks, err := scopeLocks(actor, record.project, true)
	if err != nil {
		return nil, err
	}
	commandIdentity, err := kc.CommandIdentity(record.project, record.name, record.key)
	if err != nil {
		return nil, portError(err)
	}
	key, err := f.CommandLock(commandIdentity)
	if err != nil {
		return nil, portError(err)
	}
	locks = append(locks, f.LockRequest{Key: key, Mode: f.Exclusive})
	if record.name == kc.Create {
		global, err := f.RecordLock(f.ReferenceRecordLock, "knowledge:document:"+record.document.String())
		if err != nil {
			return nil, portError(err)
		}
		locks = append(locks, f.LockRequest{Key: global, Mode: f.Exclusive})
	}
	if source != nil {
		origin, err := scopeLocks(actor, *source, false)
		if err != nil {
			return nil, err
		}
		locks = append(locks, origin...)
	}
	return ob.NormalizeLocks(locks)
}

func nextPublicationWork(record *commandRecord, source *id.ProjectID, process ob.ProcessID, previous *publicationWork, proof *publicationStopProof) (publicationWork, error) {
	if record == nil || record.id.Validate() != nil || record.project.Validate() != nil || record.state != kc.InProgress || process.Validate() != nil || source != nil && source.Validate() != nil {
		return publicationWork{}, internal(nil)
	}
	fence := int64(1)
	if previous != nil {
		if previous.command != record.id || previous.project != record.project || !equalProjectPointer(previous.source, source) || previous.fence < 1 || previous.fence == math.MaxInt64 {
			return publicationWork{}, internal(nil)
		}
		if previous.phase == "active" {
			if proof == nil || !proof.original.equal(*previous) || proof.joinedLocal != (previous.process == process) {
				return publicationWork{}, fault(f.ResourceBusy)
			}
		} else if previous.phase != "joined" {
			return publicationWork{}, internal(nil)
		}
		fence = previous.fence + 1
	} else if proof != nil {
		return publicationWork{}, internal(nil)
	}
	attempt, err := f.NewID[publicationAttempt]()
	if err != nil {
		return publicationWork{}, unavailable(err)
	}
	return publicationWork{command: record.id, project: record.project, source: source, process: process, attempt: attempt, fence: fence, phase: "active"}, nil
}

// The caller acquires the complete target/source/Command union once and checks
// current authorization before calling. No resolver, spool or source I/O may
// begin until this same transaction's actual commit has been confirmed.
func (s *Service) claimPublicationInTx(ctx context.Context, tx f.Tx, actor id.Actor, record *commandRecord, source *id.ProjectID, expected *publicationWork, proof *publicationStopProof) (publicationWork, error) {
	locks, err := publicationLocks(actor, record, source)
	if err != nil {
		return publicationWork{}, err
	}
	if err = s.state().store.RequireHeldLocks(ctx, tx, locks); err != nil {
		return publicationWork{}, unavailable(err)
	}
	x, err := s.state().store.InTx(tx)
	if err != nil {
		return publicationWork{}, unavailable(err)
	}
	current, err := loadPublicationWork(ctx, x, record.id)
	if err != nil {
		return publicationWork{}, err
	}
	if (current == nil) != (expected == nil) || current != nil && !current.equal(*expected) {
		return publicationWork{}, fault(f.ResourceBusy)
	}
	next, err := nextPublicationWork(record, source, s.state().deps.Processes.CurrentProcess(), current, proof)
	if err != nil {
		return publicationWork{}, err
	}
	var origin any
	if source != nil {
		origin = source.String()
	}
	_, err = x.Exec(ctx, `INSERT INTO agenteam_knowledge.work_claims
 (command_id,project_id,source_project_id,process_id,attempt_id,fence,phase)
 VALUES($1,$2,$3,$4,$5,$6,'active') ON CONFLICT(command_id) DO UPDATE
 SET process_id=EXCLUDED.process_id,attempt_id=EXCLUDED.attempt_id,fence=EXCLUDED.fence,phase='active'`, next.command.String(), next.project.String(), origin, next.process.String(), next.attempt.String(), next.fence)
	return next, portError(err)
}

// Every reserve/final transaction rechecks this exact persisted fence after
// acquiring the original union. A late worker cannot publish after takeover.
func requirePublicationWork(ctx context.Context, x postgres.SQLExecutor, work publicationWork) error {
	current, err := loadPublicationWork(ctx, x, work.command)
	if err != nil {
		return err
	}
	if current == nil || work.phase != "active" || !current.equal(work) {
		return fault(f.ResourceBusy)
	}
	return nil
}

// A publicationRetirement is held by one admitted call until every owned source,
// lease and spool close has actually succeeded. Callers invoke join only after
// all synchronous preparation/send/final callbacks have returned. Failure keeps
// the call registered; the process cannot report Drain complete on cancellation.
type publicationRetirement struct {
	mu        sync.Mutex
	service   *Service
	work      publicationWork
	resources []func() error
	done      func()
	joined    bool
}

func (s *Service) publicationRetirement(work publicationWork, done func()) (*publicationRetirement, error) {
	if s.state() == nil || work.command.Validate() != nil || work.project.Validate() != nil || work.process != s.state().deps.Processes.CurrentProcess() || work.attempt.Validate() != nil || work.fence < 1 || work.phase != "active" || done == nil {
		return nil, internal(nil)
	}
	return &publicationRetirement{service: s, work: work.clone(), done: done}, nil
}

func (r *publicationRetirement) own(close func() error) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.joined || close == nil {
		return internal(nil)
	}
	r.resources = append(r.resources, close)
	return nil
}

// No transaction is opened here: resource retirement cannot be made to depend
// on the request's now-cancelled SQL context. Keep exact in-process evidence for
// a subsequent caller to confirm under the original command lock. A process
// restart instead uses the real ProcessAuthority stop proof.
func (r *publicationRetirement) join() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.joined {
		return nil
	}
	var failure error
	for n := len(r.resources) - 1; n >= 0; n-- {
		if r.resources[n] == nil {
			continue
		}
		if err := r.resources[n](); err != nil {
			if failure == nil {
				failure = portError(err)
			}
		} else {
			r.resources[n] = nil
		}
	}
	if failure != nil {
		return failure
	}
	st := r.service.state()
	st.mu.Lock()
	if st.joinedPublications == nil {
		st.joinedPublications = make(map[f.ID[command]]publicationWork)
	}
	// Preserve the newest locally joined attempt. An old finalizer cannot
	// overwrite a newer fence that has already completed independently.
	old, exists := st.joinedPublications[r.work.command]
	if !exists || old.fence < r.work.fence || old.equal(r.work) {
		st.joinedPublications[r.work.command] = r.work
	}
	st.mu.Unlock()
	r.joined = true
	r.resources = nil
	r.done()
	return nil
}

// checkpointPublicationJoin is an optional durable projection of an already
// established local join, not the source of that proof. The caller supplies its
// own still-live budget. Failed/Unknown SQL leaves the exact local proof intact.
func (s *Service) checkpointPublicationJoin(ctx context.Context, actor id.Actor, record *commandRecord, work publicationWork) error {
	st := s.state()
	if st == nil {
		return fault(f.DependencyUnbound)
	}
	st.mu.Lock()
	joined, ok := st.joinedPublications[work.command]
	st.mu.Unlock()
	if !ok || !joined.equal(work) || record == nil || record.id != work.command || record.project != work.project {
		return fault(f.ResourceBusy)
	}
	locks, err := publicationLocks(actor, record, work.source)
	if err != nil {
		return err
	}
	identity, err := kc.CommandIdentity(record.project, record.name, record.key)
	if err != nil {
		return portError(err)
	}
	cause, err := f.NewCommandsCause(identity)
	if err != nil {
		return portError(err)
	}
	result := st.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := st.store.AcquireAll(ctx, tx, locks); err != nil {
			return portError(err)
		}
		x, err := st.store.InTx(tx)
		if err != nil {
			return portError(err)
		}
		original, err := loadCommand(ctx, x, record.project, record.name, record.key)
		if err != nil {
			return err
		}
		if original == nil || original.id != work.command || original.user != record.user || original.document != record.document || original.digest != record.digest {
			return fault(f.ResourceBusy)
		}
		current, err := loadPublicationWork(ctx, x, work.command)
		if err != nil {
			return err
		}
		if current == nil {
			return fault(f.ResourceBusy)
		}
		already := *current
		already.phase = "active"
		if !already.equal(work) {
			return fault(f.ResourceBusy)
		}
		if current.phase == "joined" {
			return nil
		}
		// This closes only the exact persisted work; it grants no document
		// access and performs no user mutation after Session revocation.
		tag, err := x.Exec(ctx, `UPDATE agenteam_knowledge.work_claims SET phase='joined'
 WHERE command_id=$1 AND process_id=$2 AND attempt_id=$3 AND fence=$4 AND phase='active'`, work.command.String(), work.process.String(), work.attempt.String(), work.fence)
		if err != nil {
			return unavailable(err)
		}
		if tag.RowsAffected() != 1 {
			return fault(f.ResourceBusy)
		}
		return nil
	})
	if err := txError(result); err != nil {
		return err
	}
	st.mu.Lock()
	if current, ok := st.joinedPublications[work.command]; ok && current.equal(work) {
		delete(st.joinedPublications, work.command)
	}
	st.mu.Unlock()
	return nil
}
