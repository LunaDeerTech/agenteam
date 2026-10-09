package knowledge

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"

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
func equalProjectPointer(a, b *id.ProjectID) bool {
	return a == nil && b == nil || a != nil && b != nil && *a == *b
}

// Only a successful real ProcessAuthority call can construct a stopped proof.
// It is bound to the original persisted attempt/fence and rechecked in the
// caller's transaction; clock age and a missing in-memory call are not proof.
type publicationStopProof struct{ original publicationWork }

func (s *Service) stoppedPublication(ctx context.Context, old *publicationWork) (*publicationStopProof, error) {
	if old == nil || old.phase == "joined" {
		return nil, nil
	}
	if old.phase != "active" || old.process == s.state().deps.Processes.CurrentProcess() {
		return nil, fault(f.ResourceBusy)
	}
	if err := s.state().deps.Processes.ConfirmStopped(ctx, old.process); err != nil {
		return nil, portError(err)
	}
	return &publicationStopProof{original: *old}, nil
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
			if previous.process == process || proof == nil || !proof.original.equal(*previous) {
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
