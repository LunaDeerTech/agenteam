package scheduler

import (
	"context"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	wc "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
)

func validRelaunchOrigin(r *dispatchRecord) bool {
	if r == nil || r.guard != nil || r.relaunch == nil || r.relaunch.Validate() != nil {
		return false
	}
	v := r.relaunch.Request
	return v.ProjectID == r.project && v.TaskID.String() == r.task && v.AgentID == r.agent && v.CurrentSprintID.String() == r.sprint && v.DispatchID == r.id.String() && v.RequestID == r.launch.Meta.RequestID && v.Purpose == r.launch.Purpose
}
func validDispatchOrigin(r *dispatchRecord) bool {
	return r != nil && (r.guard != nil && r.relaunch == nil && r.guard.valid() || validRelaunchOrigin(r))
}
func sameRelaunchSource(a, b *wc.TaskRelaunchSource) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}
func (a *PendingAuthority) relaunchCall(ctx context.Context, actor i.Actor, r wc.TaskRelaunchRequest) (*relaunchCall, error) {
	if ctx == nil || r.Validate() != nil || actor.Validate() != nil {
		return nil, invalid()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	call, _ := ctx.Value(relaunchContextKey{}).(*relaunchCall)
	if a == nil || call == nil || call.owner == nil || call.owner.coordinator.authority != a || call.request != r || !call.actor.Equal(actor) {
		return nil, fault(f.Forbidden)
	}
	details := actor.Details()
	if details.Kind != i.Service || details.ServiceName != i.Scheduler || details.ProjectID != r.ProjectID.String() || details.CauseRef != r.DispatchID {
		return nil, fault(f.Forbidden)
	}
	call.mu.Lock()
	live := call.live
	call.mu.Unlock()
	if !live {
		return nil, fault(f.Forbidden)
	}
	return call, nil
}
func (a *PendingAuthority) RequireTaskRelaunchDiscoveryInTx(ctx context.Context, tx f.Tx, actor i.Actor, r wc.TaskRelaunchRequest) error {
	call, err := a.relaunchCall(ctx, actor, r)
	if err != nil {
		return err
	}
	call.mu.Lock()
	applying := call.applying
	call.mu.Unlock()
	if applying {
		return fault(f.Forbidden)
	}
	if _, err = a.inTx(ctx, tx, r.ProjectID); err != nil {
		return err
	}
	ak, _ := f.AgentLock(r.AgentID.String())
	tk, _ := f.AggregateLock(f.TaskAggregate, r.TaskID.String())
	locks := append(pendingLocks(r.ProjectID), f.LockRequest{Key: ak, Mode: f.Shared}, f.LockRequest{Key: tk, Mode: f.Shared})
	if err = a.store.RequireHeldLocks(ctx, tx, locks); err != nil {
		return portError(err)
	}
	_, err = call.owner.projectInTx(ctx, tx, r, true)
	return err
}
func (a *PendingAuthority) RequireTaskRelaunchInTx(ctx context.Context, tx f.Tx, actor i.Actor, r wc.TaskRelaunchRequest, plan wc.TaskRelaunchPlan) error {
	call, err := a.relaunchCall(ctx, actor, r)
	if err != nil {
		return err
	}
	call.mu.Lock()
	valid := call.applying && call.tx == tx && samePlan(call.plan, plan)
	locks := append([]f.LockRequest(nil), call.locks...)
	call.mu.Unlock()
	if !valid || len(locks) == 0 {
		return fault(f.Forbidden)
	}
	if _, err = a.inTx(ctx, tx, r.ProjectID); err != nil {
		return err
	}
	if err = a.store.RequireHeldLocks(ctx, tx, locks); err != nil {
		return portError(err)
	}
	return ctx.Err()
}
func (a *PendingAuthority) relaunchCurrentIntent(ctx context.Context, tx f.Tx, actor i.Actor, p i.ProjectID, agent i.AgentID) (pc.SchedulerIntent, error) {
	call, _ := ctx.Value(relaunchContextKey{}).(*relaunchCall)
	if call == nil || call.request.ProjectID != p || call.request.AgentID != agent {
		return pc.SchedulerIntent{}, fault(f.Forbidden)
	}
	call.mu.Lock()
	plan := call.plan
	call.mu.Unlock()
	if err := a.RequireTaskRelaunchInTx(ctx, tx, actor, call.request, plan); err != nil {
		return pc.SchedulerIntent{}, err
	}
	return pc.SchedulerIntent{ProjectID: p, AgentID: agent, DispatchID: call.request.DispatchID, SprintID: call.request.CurrentSprintID}, nil
}

var _ wc.SchedulerRelaunchAuthority = (*PendingAuthority)(nil)
