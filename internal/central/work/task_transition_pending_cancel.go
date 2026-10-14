package work

import (
	"context"
	"slices"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
)

func (s *TaskTransitionService) BindPendingCancellation(owner c.TaskPendingCanceller) error {
	st := s.state()
	if st == nil || nilPort(owner) {
		return fault(f.DependencyUnbound)
	}
	if err := owner.RequireStore(st.store); err != nil {
		return portError(err)
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	if !nilPort(st.deps.PendingCancellation) || len(st.calls) != 0 || st.stopped {
		return fault(f.InvalidState)
	}
	st.deps.PendingCancellation = owner
	return nil
}

type pendingTaskCancelKey struct{}
type pendingTaskCancelWitness struct {
	owner  *taskTransitionServiceState
	tx     f.Tx
	actor  i.ActorDetails
	record *transitionRecord
}

func pendingCancellationScope(r *transitionRecord) c.TaskPendingCancellationScope {
	return c.TaskPendingCancellationScope{ProjectID: r.Input.Project, TaskID: r.Input.Task.String(), CommandID: r.ID, DispatchIDs: slices.Clone(r.Plan.CancelDispatches), OccurredAt: r.Plan.After.Task.UpdatedAt}
}
func (s *TaskTransitionService) RequirePendingTaskCancellationInTx(ctx context.Context, tx f.Tx, actor i.Actor, scope c.TaskPendingCancellationScope) error {
	if ctx == nil || s.state() == nil {
		return fault(f.Forbidden)
	}
	w, _ := ctx.Value(pendingTaskCancelKey{}).(*pendingTaskCancelWitness)
	if w == nil || w.owner != s.state() || w.tx != tx || w.actor != actor.Details() || w.record == nil || w.record.Plan.After.Task.State != c.TaskStateCancelled || !sameValue(pendingCancellationScope(w.record), scope) {
		return fault(f.Forbidden)
	}
	if err := s.requireTransitionLifetime(ctx, tx, actor, scope.ProjectID); err != nil {
		return err
	}
	if _, err := s.state().deps.Authority.requireTransitionAccess(ctx, tx, actor, scope.ProjectID, i.Mutate); err != nil {
		return err
	}
	x, err := s.state().store.InTx(tx)
	if err != nil {
		return portError(err)
	}
	return verifyTransitionPostimage(ctx, x, w.record)
}
func (s *TaskTransitionService) cancelPendingInTx(ctx context.Context, tx f.Tx, actor i.Actor, r *transitionRecord) error {
	if len(r.Plan.CancelDispatches) == 0 {
		return nil
	}
	if nilPort(s.state().deps.PendingCancellation) {
		return fault(f.DependencyUnbound)
	}
	inner := context.WithValue(ctx, pendingTaskCancelKey{}, &pendingTaskCancelWitness{s.state(), tx, actor.Details(), r})
	return portError(s.state().deps.PendingCancellation.CancelTaskDispatchesInTx(inner, tx, actor, pendingCancellationScope(r)))
}
