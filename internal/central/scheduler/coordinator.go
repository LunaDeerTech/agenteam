package scheduler

import (
	"context"
	"math"
	"sync"
	"time"

	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	wc "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
)

type ProjectSchedulerGate interface {
	RequireSchedulerProjectInTx(context.Context, f.Tx, i.ProjectID) (pc.SchedulerProject, error)
}
type CoordinatorDependencies struct {
	Projects   ProjectSchedulerGate
	Claims     wc.SchedulerTaskClaims
	Executions ec.DispatchObserver
	Capacity   ec.DispatchCapacityReader
}

// Coordinator provides individual durable claims; it does not invent a
// traversal, timer, dispatcher process or a Task source that is not installed.
// Authority -> Work -> Coordinator is an immutable, acyclic construction order.
type Coordinator struct {
	authority   *PendingAuthority
	deps        CoordinatorDependencies
	retryPolicy LaunchRetryPolicy
	mu          sync.Mutex
	stopped     bool
	calls       map[*claimCall]struct{}
	unknown     map[DispatchID]*claimCall
	drained     chan struct{}
}
type claimStage uint8

const (
	claimDiscovery claimStage = iota + 1
	claimApplying
)

type claimContextKey struct{}
type claimCall struct {
	owner       *Coordinator
	request     wc.TaskClaimRequest
	actor       i.Actor
	launch      ec.LaunchRequest
	retryPolicy LaunchRetryPolicy
	cancel      context.CancelCauseFunc
	mu          sync.Mutex
	live        bool
	stage       claimStage
	tx          f.Tx
	plan        wc.TaskClaimPlan
	locks       []f.LockRequest
	unknown     error
	resolving   bool
}

func NewCoordinator(a *PendingAuthority, deps CoordinatorDependencies) (*Coordinator, error) {
	if a == nil || nilPort(a.store) || nilPort(deps.Projects) || nilPort(deps.Claims) || nilPort(deps.Executions) || nilPort(deps.Capacity) {
		return nil, fault(f.DependencyUnbound)
	}
	return &Coordinator{authority: a, deps: deps, calls: make(map[*claimCall]struct{}), unknown: make(map[DispatchID]*claimCall), drained: make(chan struct{})}, nil
}

// NewCoordinatorWithRetryPolicy fixes explicit deployment settings for newly
// created Dispatches. Existing receipts retain their stored policy, including
// its absence. The original constructor remains unbound to retry settings.
// Neither constructor enables sending a known-rejected Launch again.
func NewCoordinatorWithRetryPolicy(a *PendingAuthority, deps CoordinatorDependencies, policy LaunchRetryPolicy) (*Coordinator, error) {
	if err := policy.Validate(); err != nil {
		return nil, err
	}
	s, err := NewCoordinator(a, deps)
	if err != nil {
		return nil, err
	}
	s.retryPolicy = policy
	return s, nil
}
func schedulerActor(p i.ProjectID, dispatch string) (i.Actor, error) {
	r, err := i.RegisterService(i.Scheduler)
	if err != nil {
		return i.Actor{}, err
	}
	scope, err := i.InProject(p)
	if err != nil {
		return i.Actor{}, err
	}
	return r.Actor(dispatch, scope)
}
func (s *Coordinator) admit(ctx context.Context, r wc.TaskClaimRequest, policy ec.Policy) (context.Context, *claimCall, error) {
	if ctx == nil || r.Validate() != nil || policy.Validate() != nil {
		return nil, nil, invalid()
	}
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	if s == nil || s.authority == nil {
		return nil, nil, fault(f.DependencyUnbound)
	}
	actor, err := schedulerActor(r.ProjectID, r.DispatchID)
	if err != nil {
		return nil, nil, err
	}
	dispatch, _ := f.ParseID[DispatchIdentity](r.DispatchID)
	launch := ec.LaunchRequest{ProjectID: r.ProjectID, AgentID: r.AgentID, Trigger: ec.Trigger{Kind: "task", TaskID: r.TaskID.String()}, Purpose: r.Purpose, Policy: policy.Clone(), Lineage: ec.Lineage{DispatchID: r.DispatchID}, Meta: f.CommandMeta{RequestID: r.RequestID, IdempotencyKey: launchKey(dispatch)}}
	if launch.Validate() != nil {
		return nil, nil, invalid()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopped {
		return nil, nil, fault(f.ShuttingDown)
	}
	if prior := s.unknown[dispatch]; prior != nil {
		return nil, nil, prior.unknown
	}
	for call := range s.calls {
		if call.request.DispatchID == r.DispatchID {
			return nil, nil, fault(f.ResourceBusy)
		}
	}
	owned, cancel := context.WithCancelCause(ctx)
	call := &claimCall{owner: s, request: r.Clone(), actor: actor, launch: launch, retryPolicy: s.retryPolicy, cancel: cancel, live: true, stage: claimDiscovery}
	s.calls[call] = struct{}{}
	return context.WithValue(owned, claimContextKey{}, call), call, nil
}
func (s *Coordinator) release(call *claimCall) {
	call.mu.Lock()
	call.live = false
	call.tx = f.Tx{}
	call.mu.Unlock()
	call.cancel(nil)
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.calls, call)
	if s.stopped && len(s.calls) == 0 {
		select {
		case <-s.drained:
		default:
			close(s.drained)
		}
	}
}
func (s *Coordinator) Stop() {
	if s == nil {
		return
	}
	s.mu.Lock()
	if s.stopped {
		s.mu.Unlock()
		return
	}
	s.stopped = true
	calls := make([]*claimCall, 0, len(s.calls))
	for c := range s.calls {
		calls = append(calls, c)
	}
	if len(calls) == 0 {
		close(s.drained)
	}
	s.mu.Unlock()
	for _, c := range calls {
		c.cancel(fault(f.ShuttingDown))
	}
}
func (s *Coordinator) Drain(ctx context.Context) error {
	if ctx == nil {
		return invalid()
	}
	if s == nil {
		return nil
	}
	select {
	case <-s.drained:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
func (s *Coordinator) Joined() bool {
	if s == nil {
		return true
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.stopped && len(s.calls) == 0
}

// ClaimTask preserves the caller's one original Dispatch identity. The caller
// obtains candidate IDs from Work; this method revalidates every current fact.
// CommitUnknown returns no Dispatch and retains the original call ownership.
func (s *Coordinator) ClaimTask(ctx context.Context, r wc.TaskClaimRequest, policy ec.Policy) (out Dispatch, err error) {
	ctx, call, err := s.admit(ctx, r, policy)
	if err != nil {
		return Dispatch{}, err
	}
	retained := false
	defer func() {
		if !retained {
			s.release(call)
		}
	}()
	dispatch, _ := f.ParseID[DispatchIdentity](r.DispatchID)
	command, _ := claimCommand(r.ProjectID, dispatch)
	cause, _ := f.NewCommandsCause(command)
	base, err := claimLocks(r, call.launch)
	if err != nil {
		return Dispatch{}, err
	}
	var replay *dispatchRecord
	// Observe only the original claim for replay. Work and canonical mutation
	// happen in the later final transaction; no permit survives this callback.
	result := s.authority.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := s.authority.store.AcquireAll(ctx, tx, base); err != nil {
			return portError(err)
		}
		if _, err := s.currentProject(ctx, tx, r, false); err != nil {
			return err
		}
		x, err := s.authority.inTx(ctx, tx, r.ProjectID)
		if err != nil {
			return err
		}
		replay, err = loadDispatch(ctx, x, r.ProjectID, dispatch)
		if err != nil {
			return err
		}
		if replay != nil {
			return sameClaim(replay, r, call.launch)
		}
		return nil
	})
	if err = commitError(result); err != nil {
		return Dispatch{}, err
	}
	if replay != nil {
		return snapshot(replay), nil
	}
	plan, err := s.deps.Claims.DiscoverTaskClaim(ctx, call.actor, r)
	if err != nil {
		return Dispatch{}, portError(err)
	}
	if nilPort(plan) {
		return Dispatch{}, fault(f.DependencyUnbound)
	}
	locks := append(base, plan.RequiredLocks()...)
	locks, err = oc.NormalizeLocks(locks)
	if err != nil {
		return Dispatch{}, invalid()
	}
	var committed *dispatchRecord
	result = s.authority.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := s.authority.store.AcquireAll(ctx, tx, locks); err != nil {
			return portError(err)
		}
		project, err := s.currentProject(ctx, tx, r, true)
		if err != nil {
			return err
		}
		x, err := s.authority.inTx(ctx, tx, r.ProjectID)
		if err != nil {
			return err
		}
		old, err := loadDispatch(ctx, x, r.ProjectID, dispatch)
		if err != nil {
			return err
		}
		if old != nil {
			if err = sameClaim(old, r, call.launch); err != nil {
				return err
			}
			// This transaction observed another claimant's existing receipt.
			// If its commit is Unknown, resolution must match that observed
			// binding, not the deployment settings used for a new insertion.
			call.retryPolicy = old.retryPolicy
			committed = old
			return nil
		}
		current, err := readCapacityRecords(ctx, x, r.ProjectID)
		if err != nil {
			return err
		}
		for _, d := range current {
			if d.task == r.TaskID.String() && d.status == Pending {
				return fault(f.ResourceBusy)
			}
		}
		used, err := s.capacityInTx(ctx, tx, r.ProjectID, current)
		if err != nil {
			return err
		}
		if project.Config.MaxConcurrency != nil && used >= *project.Config.MaxConcurrency {
			return fault(f.ResourceBusy)
		}
		slot, err := s.deps.Executions.AgentSlotInTx(ctx, tx, r.ProjectID, r.AgentID)
		if err != nil {
			return portError(err)
		}
		if slot.Occupied {
			if slot.ExecutionID == nil || slot.ExecutionID.Validate() != nil || !slot.Status.Valid() || slot.Status.Terminal() {
				return unavailable(nil)
			}
			return fault(f.AgentBusy)
		}
		if slot.ExecutionID != nil || slot.Status != "" || slot.CancelRequestedAt != nil {
			return unavailable(nil)
		}
		call.mu.Lock()
		call.stage = claimApplying
		call.tx = tx
		call.plan = plan
		call.locks = append([]f.LockRequest(nil), locks...)
		call.mu.Unlock()
		defer func() {
			call.mu.Lock()
			call.tx = f.Tx{}
			call.stage = claimDiscovery
			call.plan = nil
			call.locks = nil
			call.mu.Unlock()
		}()
		applied, err := s.deps.Claims.ApplyTaskClaimInTx(ctx, tx, call.actor, r, plan)
		if err != nil {
			return portError(err)
		}
		if nilPort(applied) {
			return fault(f.DependencyUnbound)
		}
		if err = s.deps.Claims.CheckTaskClaimAppliedInTx(ctx, tx, call.actor, r, plan, applied); err != nil {
			return portError(err)
		}
		guard, err := storedGuard(r, applied.Guard())
		if err != nil {
			return err
		}
		now, _ := f.NewInstant(time.Now())
		digest, _ := call.launch.Digest()
		committed = &dispatchRecord{id: dispatch, project: r.ProjectID, sprint: r.CurrentSprintID.String(), task: r.TaskID.String(), agent: r.AgentID, launch: call.launch.Clone(), digest: digest, retryPolicy: call.retryPolicy, status: Pending, outcome: NotSent, version: 1, guard: guard, createdAt: now, updatedAt: now}
		return insertDispatch(ctx, x, committed)
	})
	err = commitError(result)
	if result.State() == f.Unknown {
		call.mu.Lock()
		call.live = false
		call.unknown = err
		call.mu.Unlock()
		s.mu.Lock()
		s.unknown[dispatch] = call
		s.mu.Unlock()
		retained = true
		return Dispatch{}, err
	}
	if err != nil {
		return Dispatch{}, err
	}
	if committed == nil {
		return Dispatch{}, unavailable(nil)
	}
	return snapshot(committed), nil
}

func (s *Coordinator) currentProject(ctx context.Context, tx f.Tx, r wc.TaskClaimRequest, enabled bool) (pc.SchedulerProject, error) {
	p, err := s.deps.Projects.RequireSchedulerProjectInTx(ctx, tx, r.ProjectID)
	if err != nil {
		return pc.SchedulerProject{}, portError(err)
	}
	if p.Project.ID != r.ProjectID || p.Config.Validate() != nil {
		return pc.SchedulerProject{}, unavailable(nil)
	}
	if enabled && (!p.Config.Enabled || p.Project.CurrentSprintID == nil || p.Project.CurrentSprintID.String() != r.CurrentSprintID.String()) {
		return pc.SchedulerProject{}, fault(f.InvalidState)
	}
	return p, ctx.Err()
}
func claimLocks(r wc.TaskClaimRequest, launch ec.LaunchRequest) ([]f.LockRequest, error) {
	dispatch, _ := f.ParseID[DispatchIdentity](r.DispatchID)
	command, _ := claimCommand(r.ProjectID, dispatch)
	ck, _ := f.CommandLock(command)
	lc, _ := launch.Command()
	lk, _ := f.CommandLock(lc)
	ak, _ := f.AgentLock(r.AgentID.String())
	tk, _ := f.AggregateLock(f.TaskAggregate, r.TaskID.String())
	dk, _ := f.AggregateLock(f.DispatchAggregate, r.DispatchID)
	return oc.NormalizeLocks(append(pendingLocks(r.ProjectID), f.LockRequest{Key: ck, Mode: f.Exclusive}, f.LockRequest{Key: lk, Mode: f.Exclusive}, f.LockRequest{Key: ak, Mode: f.Exclusive}, f.LockRequest{Key: tk, Mode: f.Exclusive}, f.LockRequest{Key: dk, Mode: f.Exclusive}))
}
func storedGuard(r wc.TaskClaimRequest, g wc.TaskClaimGuard) (*ClaimGuard, error) {
	if r.ExpectedTaskVersion == f.Version(math.MaxInt64) {
		return nil, fault(f.InvalidState)
	}
	if g.TaskID != r.TaskID || g.ClaimedVersion != r.ExpectedTaskVersion+1 || g.SourceAssigneeID != r.AgentID || g.SourceSprintID != r.CurrentSprintID {
		return nil, unavailable(nil)
	}
	out := &ClaimGuard{TaskID: g.TaskID.String(), ClaimedVersion: g.ClaimedVersion, SourceState: string(g.SourceState), SourceAssigneeID: g.SourceAssigneeID, SourcePriority: string(g.SourcePriority), SourceSprintID: g.SourceSprintID.String(), SourceOrderGeneration: f.Version(g.SourceOrderGeneration)}
	if g.PredecessorID != nil {
		out.PredecessorID = g.PredecessorID.String()
	}
	if g.SuccessorID != nil {
		out.SuccessorID = g.SuccessorID.String()
	}
	if !out.valid() {
		return nil, unavailable(nil)
	}
	return out, nil
}
func sameClaim(r *dispatchRecord, request wc.TaskClaimRequest, launch ec.LaunchRequest) error {
	digest, err := launch.Digest()
	if err != nil || r.digest != digest || r.launch.Meta.RequestID != request.RequestID || r.sprint != request.CurrentSprintID.String() || r.guard == nil || r.guard.ClaimedVersion != request.ExpectedTaskVersion+1 {
		return fault(f.IdempotencyKeyReused)
	}
	return nil
}
