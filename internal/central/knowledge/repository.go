package knowledge

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"reflect"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	kc "github.com/LunaDeerTech/agenteam/internal/central/knowledge/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	ob "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/jackc/pgx/v5"
)

type Store interface {
	postgres.SQLExecutor
	WithinTx(context.Context, f.TransactionCause, func(context.Context, f.Tx) error) f.CommitResult
	InTx(f.Tx) (postgres.SQLExecutor, error)
	AcquireAll(context.Context, f.Tx, []f.LockRequest) error
	RequireHeldLocks(context.Context, f.Tx, []f.LockRequest) error
}

func nilPort(v any) bool {
	if v == nil {
		return true
	}
	x := reflect.ValueOf(v)
	switch x.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return x.IsNil()
	}
	return false
}
func fault(c f.Code) *f.Fault     { return f.NewFault(c, f.NotStarted) }
func unavailable(err error) error { return fault(f.DependencyUnavailable).WithCause(err) }
func internal(err error) error    { return fault(f.InternalError).WithCause(err) }
func portError(err error) error {
	if err == nil {
		return nil
	}
	var known *f.Fault
	if errors.As(err, &known) {
		return err
	}
	return unavailable(err)
}
func txError(r f.CommitResult) error {
	switch r.State() {
	case f.Committed:
		return nil
	case f.Unknown:
		err := f.NewFault(f.CommitUnknown, f.Unknown)
		err.RetryHint = "lookup"
		if r.AttemptID().Validate() == nil {
			err.CauseID = r.AttemptID().String()
		}
		return err.WithCause(commitFailure{result: r})
	default:
		if err := r.Fault(); err != nil {
			return err
		}
		return internal(nil)
	}
}

// The actual transaction outcome stays available for confirmation without
// exposing the command key or private cause through formatting or logging.
type commitFailure struct{ result f.CommitResult }

func (commitFailure) Error() string { return string(f.CommitUnknown) }
func (commitFailure) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "knowledge_commit_outcome")
}
func (commitFailure) LogValue() slog.Value { return slog.StringValue("knowledge_commit_outcome") }
func readInput(ctx context.Context, actor id.Actor, project id.ProjectID) error {
	if ctx == nil {
		return fault(f.InvalidArgument)
	}
	if err := ctx.Err(); err != nil {
		return unavailable(err)
	}
	if actor.Validate() != nil {
		return fault(f.Unauthenticated)
	}
	switch actor.Details().Kind {
	case id.Human:
	case id.AgentRun:
		return fault(f.DependencyUnbound)
	default:
		return fault(f.Forbidden)
	}
	if project.Validate() != nil {
		return fault(f.InvalidArgument)
	}
	return nil
}
func readCause() (f.TransactionCause, error) {
	v, err := f.NewID[struct{}]()
	if err != nil {
		return f.TransactionCause{}, unavailable(err)
	}
	cause, err := f.NewRecoveryCause("knowledge.read", v.String(), "")
	return cause, portError(err)
}
func scopeLocks(actor id.Actor, project id.ProjectID, write bool) ([]f.LockRequest, error) {
	u, err := f.UserLock(actor.Details().UserID)
	if err != nil {
		return nil, portError(err)
	}
	p, err := f.ProjectLock(project.String())
	if err != nil {
		return nil, portError(err)
	}
	t, err := f.KnowledgeTreeLock(project.String())
	if err != nil {
		return nil, portError(err)
	}
	mode := f.Shared
	if write {
		mode = f.Exclusive
	}
	return ob.NormalizeLocks([]f.LockRequest{{Key: u, Mode: mode}, {Key: p, Mode: f.Shared}, {Key: t, Mode: mode}})
}

type documentRow struct {
	head   kc.DocumentHead
	upload oc.UploadID
}

const documentColumns = `id::text,project_id::text,parent_document_id::text,title,content_version,
 source_kind,media_type,current_object_id::text,current_upload_id::text,status,indexing_status,
 creator_user_id::text,created_at,updated_at,deleted_at`

func scanDocument(row interface{ Scan(...any) error }) (*documentRow, error) {
	var key, project, status string
	var parent, title, kind, media, object, upload, indexing, creator *string
	var version int64
	var created, updated, deleted *time.Time
	if err := row.Scan(&key, &project, &parent, &title, &version, &kind, &media, &object, &upload, &status, &indexing, &creator, &created, &updated, &deleted); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fault(f.NotFound)
		}
		return nil, unavailable(err)
	}
	docID, err := f.ParseID[kc.Document](key)
	if err != nil {
		return nil, internal(err)
	}
	projectID, err := f.ParseID[id.Project](project)
	if err != nil {
		return nil, internal(err)
	}
	v := f.Version(version)
	if v.Validate() != nil {
		return nil, internal(nil)
	}
	if status == string(kc.Deleted) {
		if deleted == nil || parent != nil || title != nil || kind != nil || media != nil || object != nil || upload != nil || indexing != nil || creator != nil || created != nil || updated != nil {
			return nil, internal(nil)
		}
		at, err := f.NewInstant(*deleted)
		if err != nil {
			return nil, internal(err)
		}
		tombstone := kc.DocumentTombstone{ID: docID, ProjectID: projectID, ContentVersion: v, DeletedAt: at}
		if err = tombstone.Validate(); err != nil {
			return nil, internal(err)
		}
		return &documentRow{head: kc.DocumentHead{Deleted: &tombstone}}, nil
	}
	if status != string(kc.Active) || deleted != nil || title == nil || kind == nil || media == nil || object == nil || upload == nil || indexing == nil || creator == nil || created == nil || updated == nil {
		return nil, internal(nil)
	}
	objectID, err := f.ParseID[oc.StoredObject](*object)
	if err != nil {
		return nil, internal(err)
	}
	uploadID, err := f.ParseID[oc.Upload](*upload)
	if err != nil {
		return nil, internal(err)
	}
	userID, err := f.ParseID[id.User](*creator)
	if err != nil {
		return nil, internal(err)
	}
	by, err := kc.NewCreatorRef(kc.CreatorDetails{Kind: id.Human, UserID: userID})
	if err != nil {
		return nil, internal(err)
	}
	start, err := f.NewInstant(*created)
	if err != nil {
		return nil, internal(err)
	}
	end, err := f.NewInstant(*updated)
	if err != nil {
		return nil, internal(err)
	}
	d := kc.DocumentRef{ID: docID, ProjectID: projectID, Title: *title, ContentVersion: v, SourceKind: kc.SourceKind(*kind), MediaType: *media, ObjectID: objectID, Status: kc.Active, IndexingStatus: kc.IndexingStatus(*indexing), CreatedBy: by, CreatedAt: start, UpdatedAt: end}
	if parent != nil {
		p, err := f.ParseID[kc.Document](*parent)
		if err != nil {
			return nil, internal(err)
		}
		d.ParentDocumentID = &p
	}
	if err := d.Validate(); err != nil {
		return nil, internal(err)
	}
	return &documentRow{head: kc.DocumentHead{Active: &d}, upload: uploadID}, nil
}
func loadDocument(ctx context.Context, x postgres.SQLExecutor, project id.ProjectID, document kc.DocumentID) (*documentRow, error) {
	return scanDocument(x.QueryRow(ctx, `SELECT `+documentColumns+` FROM agenteam_knowledge.documents WHERE project_id=$1 AND id=$2`, project.String(), document.String()))
}
