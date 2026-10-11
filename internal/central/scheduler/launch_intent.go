package scheduler

import (
	"context"

	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	wc "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
)

func executionIntentLocks(r *dispatchRecord) []f.LockRequest {
	command, _ := r.launch.Command()
	ck, _ := f.CommandLock(command)
	ak, _ := f.AgentLock(r.agent.String())
	return append(pendingLocks(r.project), f.LockRequest{Key: ck, Mode: f.Exclusive}, f.LockRequest{Key: ak, Mode: f.Shared})
}

// The only issuer is this package's synchronous handoff after a known marker
// commit (send) or original durable unknown observation (read). The provider
// inspects its own canonical row in the caller's Tx; it never opens a second
// Tx, acquires locks, or calls Project/Agent/Work/Execution recursively.
func (a *PendingAuthority) launchIntent(ctx context.Context, tx f.Tx, actor i.Actor, p i.ProjectID, agent i.AgentID, send bool) (*dispatchRecord, error) {
	if ctx == nil || p.Validate() != nil || agent.Validate() != nil {
		return nil, invalid()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	call, _ := ctx.Value(launchContextKey{}).(*launchCall)
	if a == nil || call == nil || call.owner == nil || call.owner.authority != a || !call.actor.Equal(actor) || call.project != p {
		return nil, fault(f.Forbidden)
	}
	call.owner.mu.Lock()
	live := call.running && call.live && (!send || call.send)
	expected := call.record
	call.owner.mu.Unlock()
	if !live || expected == nil || expected.project != p || expected.agent != agent {
		return nil, fault(f.Forbidden)
	}
	x, err := a.inTx(ctx, tx, p)
	if err != nil {
		return nil, err
	}
	if err = a.store.RequireHeldLocks(ctx, tx, executionIntentLocks(expected)); err != nil {
		return nil, portError(err)
	}
	r, err := loadDispatch(ctx, x, p, call.id)
	if err != nil {
		return nil, err
	}
	if !sameDispatch(r, expected) || r.attempts != expected.attempts {
		return nil, fault(f.Forbidden)
	}
	if send {
		if r.status != Pending || r.outcome != Unknown || r.version != expected.version {
			return nil, fault(f.ConfirmationStale)
		}
	} else if !(r.status == Pending && r.outcome == Unknown && r.version == expected.version || r.status == Launched) {
		return nil, fault(f.ConfirmationStale)
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	call.owner.mu.Lock()
	live = call.running && call.live && call.record == expected && (!send || call.send)
	call.owner.mu.Unlock()
	if !live {
		return nil, fault(f.Forbidden)
	}
	return r, nil
}

func (a *PendingAuthority) RequireSchedulerIntentInTx(ctx context.Context, tx f.Tx, actor i.Actor, p i.ProjectID, agent i.AgentID, intent i.AccessIntent) (pc.SchedulerIntent, error) {
	if intent != i.Read && intent != i.Launch {
		return pc.SchedulerIntent{}, fault(f.Forbidden)
	}
	r, err := a.launchIntent(ctx, tx, actor, p, agent, intent == i.Launch)
	if err != nil {
		return pc.SchedulerIntent{}, err
	}
	sprint, err := f.ParseID[pc.Sprint](r.sprint)
	if err != nil {
		return pc.SchedulerIntent{}, unavailable(nil)
	}
	return pc.SchedulerIntent{ProjectID: p, AgentID: agent, DispatchID: r.id.String(), SprintID: sprint}, nil
}

func (a *PendingAuthority) RequireTaskLaunchInTx(ctx context.Context, tx f.Tx, actor i.Actor, request ec.LaunchRequest) (wc.TaskLaunchIntent, error) {
	if request.Validate() != nil {
		return wc.TaskLaunchIntent{}, invalid()
	}
	r, err := a.launchIntent(ctx, tx, actor, request.ProjectID, request.AgentID, true)
	if err != nil {
		return wc.TaskLaunchIntent{}, err
	}
	digest, _ := request.Digest()
	if digest != r.digest || request.Meta.RequestID != r.launch.Meta.RequestID || request.Meta.IdempotencyKey != r.launch.Meta.IdempotencyKey || request.Meta.ExpectedVersion != nil || !validDispatchOrigin(r) || r.launch.Purpose != "task/work" {
		return wc.TaskLaunchIntent{}, fault(f.Forbidden)
	}
	taskKey, _ := f.AggregateLock(f.TaskAggregate, r.task)
	if err = a.store.RequireHeldLocks(ctx, tx, []f.LockRequest{{Key: taskKey, Mode: f.Shared}}); err != nil {
		return wc.TaskLaunchIntent{}, portError(err)
	}
	task, e1 := f.ParseID[wc.Task](r.task)
	sprint, e2 := f.ParseID[pc.Sprint](r.sprint)
	if e1 != nil || e2 != nil {
		return wc.TaskLaunchIntent{}, unavailable(nil)
	}
	out := wc.TaskLaunchIntent{ProjectID: r.project, TaskID: task, AgentID: r.agent, SprintID: sprint, DispatchID: r.id.String()}
	if r.relaunch != nil {
		source := r.relaunch.Clone()
		out.Origin = wc.TaskDispatchRelaunch
		out.Relaunch = &source
	} else {
		out.ClaimedVersion = r.guard.ClaimedVersion
	}
	if out.Validate() != nil {
		return wc.TaskLaunchIntent{}, unavailable(nil)
	}
	return out, ctx.Err()
}

var _ pc.SchedulerIntentAuthority = (*PendingAuthority)(nil)
var _ wc.TaskLaunchAuthority = (*PendingAuthority)(nil)
