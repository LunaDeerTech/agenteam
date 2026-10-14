package scheduler

import (
	"context"
	"slices"

	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	wc "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
)

// TaskCancellation preserves original launch identities. Only Work's current
// same-transaction cancelled postimage can request cancellation; Unknown rows
// retain their original outcome and remain reserved until canonical Lookup.
type TaskCancellation struct {
	pending *PendingAuthority
	work    wc.TaskPendingCancellationAuthority
}

func NewTaskCancellation(pending *PendingAuthority, work wc.TaskPendingCancellationAuthority) (*TaskCancellation, error) {
	if pending == nil || nilPort(pending.store) || nilPort(work) {
		return nil, fault(f.DependencyUnbound)
	}
	if err := work.RequireStore(pending.store); err != nil {
		return nil, portError(err)
	}
	return &TaskCancellation{pending: pending, work: work}, nil
}
func (s *TaskCancellation) RequireStore(store any) error {
	if s == nil || s.pending == nil || nilPort(s.work) {
		return fault(f.DependencyUnbound)
	}
	return portError(s.work.RequireStore(store))
}
func (s *TaskCancellation) CancellationLocks(project i.ProjectID, ids []string) ([]f.LockRequest, error) {
	if s == nil || s.pending == nil || project.Validate() != nil || ids == nil || len(ids) > 1 {
		return nil, invalid()
	}
	locks := pendingLocks(project)
	for _, raw := range ids {
		if !validID(raw) {
			return nil, invalid()
		}
		key, err := f.AggregateLock(f.DispatchAggregate, raw)
		if err != nil {
			return nil, err
		}
		locks = append(locks, f.LockRequest{Key: key, Mode: f.Exclusive})
	}
	return locks, nil
}
func (s *TaskCancellation) RequireNoOtherPendingGroupsInTx(ctx context.Context, tx f.Tx, project i.ProjectID, task string, groups []ec.PendingClaimGroup) error {
	if !validID(task) {
		return invalid()
	}
	return s.pending.requireNoPendingGroupsExceptTaskInTx(ctx, tx, project, groups, task)
}
func (s *TaskCancellation) CancelTaskDispatchesInTx(ctx context.Context, tx f.Tx, actor i.Actor, scope wc.TaskPendingCancellationScope) error {
	if s == nil || s.pending == nil || nilPort(s.work) || scope.Validate() != nil {
		return fault(f.Forbidden)
	}
	if err := s.work.RequirePendingTaskCancellationInTx(ctx, tx, actor, scope); err != nil {
		return portError(err)
	}
	locks, err := s.CancellationLocks(scope.ProjectID, scope.DispatchIDs)
	if err != nil {
		return err
	}
	if err = s.pending.store.RequireHeldLocks(ctx, tx, locks); err != nil {
		return portError(err)
	}
	x, err := s.pending.inTx(ctx, tx, scope.ProjectID)
	if err != nil {
		return err
	}
	occupancy, err := s.pending.ReadInTx(ctx, tx, scope.ProjectID, []string{scope.TaskID})
	if err != nil {
		return err
	}
	current := make([]string, 0, len(occupancy.Pending))
	for _, v := range occupancy.Pending {
		current = append(current, v.DispatchID)
	}
	slices.Sort(current)
	if !slices.Equal(current, scope.DispatchIDs) {
		return fault(f.ConfirmationStale)
	}
	for _, raw := range scope.DispatchIDs {
		id, _ := f.ParseID[DispatchIdentity](raw)
		r, err := loadDispatch(ctx, x, scope.ProjectID, id)
		if err != nil {
			return err
		}
		if r == nil || r.task != scope.TaskID || r.status != Pending {
			return fault(f.ConfirmationStale)
		}
		tag, err := x.Exec(ctx, `INSERT INTO agenteam_scheduler.task_cancellation_requests(project_id,dispatch_id,task_id,task_transition_id,requested_at) VALUES($1,$2,$3,$4,$5)`, scope.ProjectID.String(), raw, scope.TaskID, scope.CommandID.String(), scope.OccurredAt.Time())
		if err != nil {
			return portError(err)
		}
		if tag.RowsAffected() != 1 {
			return unavailable(nil)
		}
		r.cancelRequested = true
		if _, err = settleTaskCancellation(ctx, x, r); err != nil {
			return err
		}
	}
	return ctx.Err()
}
func settleTaskCancellation(ctx context.Context, x postgres.SQLExecutor, r *dispatchRecord) (*dispatchRecord, error) {
	if r.status != Pending || r.outcome == Unknown {
		return r, nil
	}
	if r.outcome != NotSent && r.outcome != KnownNotCreated {
		return nil, unavailable(nil)
	}
	next, err := nextDispatch(r)
	if err != nil {
		return nil, err
	}
	next.status = Cancelled
	// Preserve every accepted outcome and diagnostic byte. No next attempt is
	// minted and the existing request/key/claim remains available for history.
	if err = updateDispatch(ctx, x, next, r.version); err != nil {
		return nil, err
	}
	return next, nil
}

// The visitor uses this current fact before compensation/retry. It shares the
// same Schedule EX used by markAttempt, so a pre-cancel scan is never a permit.
func (s *PendingVisitor) settleCancellation(ctx context.Context, r *dispatchRecord) (*dispatchRecord, bool, error) {
	locks, err := handoffLocks(r.project, r.id, "task_cancellation", nil)
	if err != nil {
		return nil, false, err
	}
	command, err := handoffCommand(r.project, r.id, "task_cancellation")
	if err != nil {
		return nil, false, err
	}
	cause, _ := f.NewCommandsCause(command)
	var out *dispatchRecord
	var requested bool
	result := s.authority.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := s.authority.store.AcquireAll(ctx, tx, locks); err != nil {
			return portError(err)
		}
		x, err := s.authority.inTx(ctx, tx, r.project)
		if err != nil {
			return err
		}
		out, err = loadDispatch(ctx, x, r.project, r.id)
		if err != nil {
			return err
		}
		if !sameDispatch(out, r) {
			return fault(f.ConfirmationStale)
		}
		requested = out.cancelRequested
		if !requested {
			return nil
		}
		out, err = settleTaskCancellation(ctx, x, out)
		return err
	})
	if err = commitError(result); err != nil {
		return nil, false, err
	}
	return out, requested, nil
}

var _ wc.TaskPendingCanceller = (*TaskCancellation)(nil)
