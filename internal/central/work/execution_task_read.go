package work

import (
	"context"
	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	wc "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
)

// ExecutionTaskReader is a fixed borrowed view of the installed Task reader.
// Its entire SQL lifetime belongs to the Execution read invocation; no standalone
// read or mutable permission is exposed by this adapter.
type ExecutionTaskReader struct{ original *TaskReader }

func NewExecutionTaskReader(original *TaskReader) (*ExecutionTaskReader, error) {
	if original.state() == nil || original.state().ownership == nil {
		return nil, fault(f.DependencyUnbound)
	}
	return &ExecutionTaskReader{original}, nil
}
func (r *ExecutionTaskReader) RequireStore(value any) error {
	store, ok := value.(Store)
	if r == nil || r.original.state() == nil || !ok || !sameStore(store, r.original.state().store) {
		return fault(f.DependencyUnbound)
	}
	return nil
}
func (r *ExecutionTaskReader) TaskReadLocks(actor i.Actor, project i.ProjectID, task string) ([]f.LockRequest, error) {
	if actor.Validate() != nil || actor.Details().Kind != i.Human || project.Validate() != nil {
		return nil, fault(f.Forbidden)
	}
	id, err := f.ParseID[wc.Task](task)
	if err != nil {
		return nil, fault(f.InvalidArgument)
	}
	return oc.NormalizeLocks([]f.LockRequest{userLock(actor.Details().UserID, f.Shared), projectLock(project, f.Shared), taskScheduleLock(project, f.Shared), taskLock(id.String(), f.Shared)})
}
func (r *ExecutionTaskReader) RequireTaskReadInTx(ctx context.Context, tx f.Tx, actor i.Actor, project i.ProjectID, task string) error {
	if r == nil || r.original.state() == nil {
		return fault(f.DependencyUnbound)
	}
	if err := readInput(ctx, actor, project); err != nil {
		return err
	}
	locks, err := r.TaskReadLocks(actor, project, task)
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
	id, _ := f.ParseID[wc.Task](task)
	if _, err = loadTask(ctx, x, project, id); err != nil {
		return err
	}
	return ctx.Err()
}

var _ ec.TaskReadScope = (*ExecutionTaskReader)(nil)
