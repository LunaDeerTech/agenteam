package scheduler

import (
	"context"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	wc "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
)

func failureRequest(r *dispatchRecord) (wc.TaskLaunchFailureRequest, error) {
	if !pendingFinalFailure(r) || !r.guard.valid() || r.launch.Purpose != "task/work" {
		return wc.TaskLaunchFailureRequest{}, fault(f.InvalidState)
	}
	task, e1 := f.ParseID[wc.Task](r.task)
	sprint, e2 := f.ParseID[pc.Sprint](r.sprint)
	if e1 != nil || e2 != nil {
		return wc.TaskLaunchFailureRequest{}, unavailable(nil)
	}
	request := wc.TaskLaunchFailureRequest{
		Claim:           wc.TaskClaimRequest{ProjectID: r.project, TaskID: task, AgentID: r.agent, DispatchID: r.id.String(), ExpectedTaskVersion: r.guard.ClaimedVersion - 1, CurrentSprintID: sprint, Purpose: r.launch.Purpose, RequestID: r.launch.Meta.RequestID},
		DispatchVersion: r.version, LaunchAttempt: r.finalAttempt,
	}
	if request.Validate() != nil {
		return wc.TaskLaunchFailureRequest{}, unavailable(nil)
	}
	return request, nil
}
func failureGuard(r *dispatchRecord) (wc.TaskClaimGuard, error) {
	request, err := failureRequest(r)
	if err != nil {
		return wc.TaskClaimGuard{}, err
	}
	g := r.guard
	out := wc.TaskClaimGuard{TaskID: request.Claim.TaskID, ClaimedVersion: g.ClaimedVersion, SourceState: wc.TaskState(g.SourceState), SourceAssigneeID: g.SourceAssigneeID, SourcePriority: wc.TaskPriority(g.SourcePriority), SourceSprintID: request.Claim.CurrentSprintID, SourceOrderGeneration: int64(g.SourceOrderGeneration)}
	if g.PredecessorID != "" {
		id, err := f.ParseID[wc.Task](g.PredecessorID)
		if err != nil {
			return wc.TaskClaimGuard{}, unavailable(nil)
		}
		out.PredecessorID = &id
	}
	if g.SuccessorID != "" {
		id, err := f.ParseID[wc.Task](g.SuccessorID)
		if err != nil {
			return wc.TaskClaimGuard{}, unavailable(nil)
		}
		out.SuccessorID = &id
	}
	return out, nil
}

func (a *PendingAuthority) requireFailure(ctx context.Context, tx f.Tx, actor i.Actor, request wc.TaskLaunchFailureRequest, plan wc.TaskLaunchFailurePlan, applying bool) (wc.TaskLaunchFailureFacts, error) {
	if ctx == nil || request.Validate() != nil {
		return wc.TaskLaunchFailureFacts{}, invalid()
	}
	if err := ctx.Err(); err != nil {
		return wc.TaskLaunchFailureFacts{}, err
	}
	call, _ := ctx.Value(failureContextKey{}).(*failureCall)
	if a == nil || call == nil || call.owner == nil || call.owner.authority != a || !call.actor.Equal(actor) {
		return wc.TaskLaunchFailureFacts{}, fault(f.Forbidden)
	}
	s := call.owner
	s.mu.Lock()
	live := call.running && call.live && call.request == request
	if applying {
		live = live && call.stage == failureApplying && call.tx == tx && samePlan(call.plan, plan)
	} else {
		live = live && call.stage == failureDiscovery
	}
	locks := append([]f.LockRequest(nil), call.locks...)
	expected := call.record
	s.mu.Unlock()
	if !live || expected == nil || applying && len(locks) == 0 {
		return wc.TaskLaunchFailureFacts{}, fault(f.Forbidden)
	}
	p := request.Claim.ProjectID
	x, err := a.inTx(ctx, tx, p)
	if err != nil {
		return wc.TaskLaunchFailureFacts{}, err
	}
	if !applying {
		ak, _ := f.AgentLock(request.Claim.AgentID.String())
		tk, _ := f.AggregateLock(f.TaskAggregate, request.Claim.TaskID.String())
		locks = append(pendingLocks(p), f.LockRequest{Key: ak, Mode: f.Shared}, f.LockRequest{Key: tk, Mode: f.Shared})
	}
	if err = a.store.RequireHeldLocks(ctx, tx, locks); err != nil {
		return wc.TaskLaunchFailureFacts{}, portError(err)
	}
	r, err := loadDispatch(ctx, x, p, call.id)
	if err != nil {
		return wc.TaskLaunchFailureFacts{}, err
	}
	if !sameDispatch(r, expected) || !sameFinalFailure(r, expected) {
		return wc.TaskLaunchFailureFacts{}, fault(f.ConfirmationStale)
	}
	actual, err := failureRequest(r)
	if err != nil || actual != request {
		return wc.TaskLaunchFailureFacts{}, fault(f.ConfirmationStale)
	}
	if err = s.enabled(ctx, tx, p); err != nil {
		return wc.TaskLaunchFailureFacts{}, err
	}
	// Exempt only this exact canonical Dispatch, only during this private
	// compensation. Public rank guards keep their original no-exemption API.
	// The source group comes from the durable guard, not caller-supplied SQL.
	var other bool
	err = x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_scheduler.dispatches
 WHERE project_id=$1 AND status='pending' AND claim_guard IS NOT NULL
 AND claim_source_sprint_id=$2 AND claim_source_state=$3 AND claim_source_priority=$4 AND id<>$5)`, p.String(), r.guard.SourceSprintID, r.guard.SourceState, r.guard.SourcePriority, r.id.String()).Scan(&other)
	if err != nil {
		return wc.TaskLaunchFailureFacts{}, portError(err)
	}
	if other {
		return wc.TaskLaunchFailureFacts{}, fault(f.ResourceBusy)
	}
	if err = ctx.Err(); err != nil {
		return wc.TaskLaunchFailureFacts{}, err
	}
	s.mu.Lock()
	live = call.running && call.live && call.record == expected && call.request == request
	if applying {
		live = live && call.stage == failureApplying && call.tx == tx && samePlan(call.plan, plan)
	} else {
		live = live && call.stage == failureDiscovery
	}
	s.mu.Unlock()
	if !live {
		return wc.TaskLaunchFailureFacts{}, fault(f.Forbidden)
	}
	guard, err := failureGuard(r)
	if err != nil {
		return wc.TaskLaunchFailureFacts{}, err
	}
	return wc.TaskLaunchFailureFacts{Guard: guard, Reason: r.failureReason, OccurredAt: *r.failureOccurredAt}, nil
}

func (a *PendingAuthority) RequireTaskLaunchFailureDiscoveryInTx(ctx context.Context, tx f.Tx, actor i.Actor, request wc.TaskLaunchFailureRequest) (wc.TaskLaunchFailureFacts, error) {
	return a.requireFailure(ctx, tx, actor, request, nil, false)
}
func (a *PendingAuthority) RequireTaskLaunchFailureInTx(ctx context.Context, tx f.Tx, actor i.Actor, request wc.TaskLaunchFailureRequest, plan wc.TaskLaunchFailurePlan) (wc.TaskLaunchFailureFacts, error) {
	return a.requireFailure(ctx, tx, actor, request, plan, true)
}

var _ wc.SchedulerTaskLaunchFailureAuthority = (*PendingAuthority)(nil)
