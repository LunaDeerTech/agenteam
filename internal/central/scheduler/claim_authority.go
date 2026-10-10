package scheduler

import (
	"context"
	"reflect"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	wc "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
)

func (a *PendingAuthority) claimCall(ctx context.Context, actor i.Actor, r wc.TaskClaimRequest) (*claimCall, error) {
	if ctx == nil || r.Validate() != nil || actor.Validate() != nil {
		return nil, invalid()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	call, _ := ctx.Value(claimContextKey{}).(*claimCall)
	if a == nil || call == nil || call.owner == nil || call.owner.authority != a || !call.actor.Equal(actor) || call.request != r {
		return nil, fault(f.Forbidden)
	}
	d := actor.Details()
	if d.Kind != i.Service || d.ServiceName != i.Scheduler || d.ProjectID != r.ProjectID.String() || d.CauseRef != r.DispatchID {
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

// Discovery and final mutation have distinct proofs. Protected Work discovery
// checks the current Project in the caller's original Tx, but cannot be used
// to apply a claim or to impersonate a committed pending Dispatch.
func (a *PendingAuthority) RequireTaskClaimDiscoveryInTx(ctx context.Context, tx f.Tx, actor i.Actor, r wc.TaskClaimRequest) error {
	call, err := a.claimCall(ctx, actor, r)
	if err != nil {
		return err
	}
	call.mu.Lock()
	stage := call.stage
	call.mu.Unlock()
	if stage != claimDiscovery {
		return fault(f.Forbidden)
	}
	if _, err = a.inTx(ctx, tx, r.ProjectID); err != nil {
		return err
	}
	// Work discovery knows these scalar identities before reading any content.
	// Its own rank/group locks are planned only after this protected read.
	ak, _ := f.AgentLock(r.AgentID.String())
	tk, _ := f.AggregateLock(f.TaskAggregate, r.TaskID.String())
	locks := append(pendingLocks(r.ProjectID), f.LockRequest{Key: ak, Mode: f.Shared}, f.LockRequest{Key: tk, Mode: f.Shared})
	if err = a.store.RequireHeldLocks(ctx, tx, locks); err != nil {
		return portError(err)
	}
	_, err = call.owner.currentProject(ctx, tx, r, true)
	return err
}
func samePlan(a, b wc.TaskClaimPlan) bool {
	if nilPort(a) || nilPort(b) {
		return false
	}
	x, y := reflect.ValueOf(a), reflect.ValueOf(b)
	return x.Type() == y.Type() && x.Type().Comparable() && x.Interface() == y.Interface()
}
func (a *PendingAuthority) RequireTaskClaimInTx(ctx context.Context, tx f.Tx, actor i.Actor, r wc.TaskClaimRequest, plan wc.TaskClaimPlan) error {
	call, err := a.claimCall(ctx, actor, r)
	if err != nil {
		return err
	}
	call.mu.Lock()
	valid := call.stage == claimApplying && call.tx == tx && samePlan(call.plan, plan)
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
	// Project, capacity and the all-trigger Agent slot were checked under this
	// exact complete union before the coordinator entered claimApplying. Those
	// facts cannot change while the same Project/Schedule/Agent locks are held.
	return ctx.Err()
}

// This first-claim proof is deliberately distinct from Project's persisted
// Launch intent. Agent's real provider then reads its current initialized row
// and the same Project owner; neither owner asks for a not-yet-inserted row.
func (a *PendingAuthority) RequireSchedulerCurrentIntentInTx(ctx context.Context, tx f.Tx, actor i.Actor, p i.ProjectID, agent i.AgentID) (pc.SchedulerIntent, error) {
	if ctx == nil {
		return pc.SchedulerIntent{}, invalid()
	}
	call, _ := ctx.Value(claimContextKey{}).(*claimCall)
	if call == nil || call.request.ProjectID != p || call.request.AgentID != agent {
		return pc.SchedulerIntent{}, fault(f.Forbidden)
	}
	call.mu.Lock()
	plan := call.plan
	call.mu.Unlock()
	if err := a.RequireTaskClaimInTx(ctx, tx, actor, call.request, plan); err != nil {
		return pc.SchedulerIntent{}, err
	}
	return pc.SchedulerIntent{ProjectID: p, AgentID: agent, DispatchID: call.request.DispatchID, SprintID: call.request.CurrentSprintID}, nil
}

var _ wc.SchedulerClaimAuthority = (*PendingAuthority)(nil)
