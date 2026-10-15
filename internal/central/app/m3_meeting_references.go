package app

import (
	"context"

	"github.com/LunaDeerTech/agenteam/internal/central/artifact"
	ac "github.com/LunaDeerTech/agenteam/internal/central/artifact/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/knowledge"
	kc "github.com/LunaDeerTech/agenteam/internal/central/knowledge/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/meeting"
	mc "github.com/LunaDeerTech/agenteam/internal/central/meeting/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/work"
	wc "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
)

// These fixed original domain adapters lend identity checks to Meeting's
// existing transaction. They neither open content nor create another owner.
type meetingReferences struct {
	tasks     *work.OwnerTaskReferences
	documents *knowledge.OwnerDocumentReferences
	files     *artifact.OwnerFileReferences
}

func newMeetingReferences(db database, tasks *work.TaskReader, documents *knowledge.Service, files *artifact.Service) (*meetingReferences, error) {
	t, err := work.NewOwnerTaskReferences(tasks)
	if err != nil {
		return nil, err
	}
	d, err := knowledge.NewOwnerDocumentReferences(documents)
	if err != nil {
		return nil, err
	}
	a, err := artifact.NewOwnerFileReferences(files)
	if err != nil {
		return nil, err
	}
	for _, original := range []interface{ RequireStore(any) error }{t, d, a} {
		if err = original.RequireStore(db); err != nil {
			return nil, err
		}
	}
	return &meetingReferences{tasks: t, documents: d, files: a}, nil
}

func (r *meetingReferences) input(ctx context.Context, actor i.Actor, project i.ProjectID, refs []mc.Reference) error {
	if r == nil || r.tasks == nil || r.documents == nil || r.files == nil {
		return f.NewFault(f.DependencyUnbound, f.NotStarted)
	}
	if ctx == nil || project.Validate() != nil || len(refs) > mc.MaxReferences {
		return f.NewFault(f.InvalidArgument, f.NotStarted)
	}
	if actor.Validate() != nil || actor.Details().Kind != i.Human {
		return f.NewFault(f.Forbidden, f.NotStarted)
	}
	for _, ref := range refs {
		if _, err := mc.CanonicalReference(ref); err != nil {
			return err
		}
	}
	return ctx.Err()
}

func (r *meetingReferences) ReferenceLocks(ctx context.Context, actor i.Actor, project i.ProjectID, refs []mc.Reference) ([]f.LockRequest, error) {
	if err := r.input(ctx, actor, project, refs); err != nil {
		return nil, err
	}
	var locks []f.LockRequest
	for _, ref := range refs {
		var next []f.LockRequest
		var err error
		switch ref.Kind {
		case "task":
			key, _ := f.ParseID[wc.Task](ref.ID)
			next, err = r.tasks.TaskReferenceLocks(actor, project, key)
		case "knowledge":
			key, _ := f.ParseID[kc.Document](ref.ID)
			next, err = r.documents.DocumentReferenceLocks(actor, project, key)
		case "file":
			key, _ := f.ParseID[ac.ArtifactEntity](ref.ID)
			next, err = r.files.FileReferenceLocks(ctx, actor, project, key)
		case "link":
			// A canonical URL is an identity, not a request to fetch it.
		default:
			return nil, f.NewFault(f.InvalidArgument, f.NotStarted)
		}
		if err != nil {
			return nil, err
		}
		locks = append(locks, next...)
	}
	return oc.NormalizeLocks(locks)
}

func (r *meetingReferences) RequireReferencesInTx(ctx context.Context, tx f.Tx, actor i.Actor, project i.ProjectID, refs []mc.Reference) error {
	if err := r.input(ctx, actor, project, refs); err != nil {
		return err
	}
	if !tx.Valid() {
		return f.NewFault(f.InvalidArgument, f.NotStarted)
	}
	for _, ref := range refs {
		var err error
		switch ref.Kind {
		case "task":
			key, _ := f.ParseID[wc.Task](ref.ID)
			err = r.tasks.RequireTaskReferenceInTx(ctx, tx, actor, project, key)
		case "knowledge":
			key, _ := f.ParseID[kc.Document](ref.ID)
			err = r.documents.RequireDocumentReferenceInTx(ctx, tx, actor, project, key)
		case "file":
			key, _ := f.ParseID[ac.ArtifactEntity](ref.ID)
			err = r.files.RequireFileReferenceInTx(ctx, tx, actor, project, key)
		case "link":
			// Meeting separately holds and validates current Project ownership.
		default:
			return f.NewFault(f.InvalidArgument, f.NotStarted)
		}
		if err != nil {
			return err
		}
	}
	return ctx.Err()
}

var _ meeting.ReferenceAuthority = (*meetingReferences)(nil)
