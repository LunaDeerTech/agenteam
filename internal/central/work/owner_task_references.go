package work

import (
	"context"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
)

// OwnerTaskReferences lends only current Task identity validation to another
// domain's existing transaction. The caller owns the SQL lifetime and complete
// lock union. This adapter does not start a Work read session or expose content,
// scheduling, or Execution authority.
type OwnerTaskReferences struct{ original *TaskReader }

func NewOwnerTaskReferences(original *TaskReader) (*OwnerTaskReferences, error) {
	if original.state() == nil || original.state().ownership == nil {
		return nil, fault(f.DependencyUnbound)
	}
	return &OwnerTaskReferences{original: original}, nil
}

func (r *OwnerTaskReferences) RequireStore(value any) error {
	store, ok := value.(Store)
	if r == nil || r.original.state() == nil || !ok || !sameStore(store, r.original.state().store) {
		return fault(f.DependencyUnbound)
	}
	return nil
}

func (r *OwnerTaskReferences) TaskReferenceLocks(actor i.Actor, project i.ProjectID, task c.TaskID) ([]f.LockRequest, error) {
	if r == nil || r.original.state() == nil {
		return nil, fault(f.DependencyUnbound)
	}
	if actor.Validate() != nil || actor.Details().Kind != i.Human {
		return nil, fault(f.Forbidden)
	}
	if project.Validate() != nil || task.Validate() != nil {
		return nil, fault(f.InvalidArgument)
	}
	return oc.NormalizeLocks([]f.LockRequest{userLock(actor.Details().UserID, f.Shared), projectLock(project, f.Shared), taskScheduleLock(project, f.Shared), taskLock(task.String(), f.Shared)})
}

func (r *OwnerTaskReferences) RequireTaskReferenceInTx(ctx context.Context, tx f.Tx, actor i.Actor, project i.ProjectID, task c.TaskID) error {
	if err := readInput(ctx, actor, project); err != nil {
		return err
	}
	locks, err := r.TaskReferenceLocks(actor, project, task)
	if err != nil {
		return err
	}
	st := r.original.state()
	if err = st.store.RequireHeldLocks(ctx, tx, locks); err != nil {
		return portError(err)
	}
	x, err := st.store.InTx(tx)
	if err != nil {
		return portError(err)
	}
	grant, err := st.authority.state().projects.RequireOwnerInTx(ctx, tx, actor, project, i.Read)
	if err != nil {
		return portError(err)
	}
	if !grant.Matches(actor, project) {
		return fault(f.Forbidden)
	}
	var exists bool
	if err = x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_work.tasks WHERE project_id=$1 AND id=$2)`, project.String(), task.String()).Scan(&exists); err != nil {
		return portError(err)
	}
	if !exists {
		return fault(f.NotFound)
	}
	return ctx.Err()
}
