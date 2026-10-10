package scheduler

import (
	"context"
	"sync"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	wc "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
)

type LaunchFailureFinalizerDependencies struct {
	Projects ProjectSchedulerGate
	Work     wc.SchedulerTaskLaunchFailures
}

// LaunchFailureFinalizer settles one durably classified nonretryable rejection.
// Work owns current applicability, technical Blocker, Task, history and Outbox. This
// owner checks Work's private applied proof before closing the same Dispatch
// in the same transaction. Neither a public error nor a generic known failure
// is a final-failure receipt. Construction is immutable and performs no I/O.
type LaunchFailureFinalizer struct {
	authority *PendingAuthority
	deps      LaunchFailureFinalizerDependencies
	mu        sync.Mutex
	stopped   bool
	calls     map[DispatchID]*failureCall
	drained   chan struct{}
}
type failureStage uint8

const (
	failureDiscovery failureStage = iota + 1
	failureApplying
)

type failureContextKey struct{}
type failureCall struct {
	owner   *LaunchFailureFinalizer
	project i.ProjectID
	id      DispatchID
	actor   i.Actor
	// owner.mu protects lifecycle and the callback-local proof below.
	cancel  context.CancelCauseFunc
	running bool
	live    bool
	unknown error
	record  *dispatchRecord
	request wc.TaskLaunchFailureRequest
	stage   failureStage
	tx      f.Tx
	plan    wc.TaskLaunchFailurePlan
	locks   []f.LockRequest
}

func NewLaunchFailureFinalizer(a *PendingAuthority, deps LaunchFailureFinalizerDependencies) (*LaunchFailureFinalizer, error) {
	if a == nil || nilPort(a.store) || nilPort(deps.Projects) || nilPort(deps.Work) {
		return nil, fault(f.DependencyUnbound)
	}
	return &LaunchFailureFinalizer{authority: a, deps: deps, calls: make(map[DispatchID]*failureCall), drained: make(chan struct{})}, nil
}
func (s *LaunchFailureFinalizer) begin(ctx context.Context, p i.ProjectID, id DispatchID, lookup bool) (context.Context, *failureCall, error) {
	if ctx == nil || p.Validate() != nil || id.Validate() != nil {
		return nil, nil, invalid()
	}
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	if s == nil || s.authority == nil {
		return nil, nil, fault(f.DependencyUnbound)
	}
	actor, err := schedulerActor(p, id.String())
	if err != nil {
		return nil, nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	call := s.calls[id]
	if call != nil {
		if call.project != p || call.running {
			return nil, nil, fault(f.ResourceBusy)
		}
		if !lookup {
			return nil, nil, call.unknown
		}
	} else {
		if s.stopped {
			return nil, nil, fault(f.ShuttingDown)
		}
		call = &failureCall{owner: s, project: p, id: id, actor: actor}
		s.calls[id] = call
	}
	owned, cancel := context.WithCancelCause(ctx)
	call.cancel, call.running = cancel, true
	return context.WithValue(owned, failureContextKey{}, call), call, nil
}
func (s *LaunchFailureFinalizer) finish(call *failureCall, retain bool, err error) {
	s.mu.Lock()
	call.live, call.running = false, false
	call.tx, call.plan, call.locks = f.Tx{}, nil, nil
	if retain {
		if call.unknown == nil {
			call.unknown = err
		}
	} else {
		delete(s.calls, call.id)
	}
	cancel := call.cancel
	if s.stopped && len(s.calls) == 0 {
		select {
		case <-s.drained:
		default:
			close(s.drained)
		}
	}
	s.mu.Unlock()
	cancel(nil)
}
func (s *LaunchFailureFinalizer) Stop() {
	if s == nil {
		return
	}
	s.mu.Lock()
	if s.stopped {
		s.mu.Unlock()
		return
	}
	s.stopped = true
	cancels := make([]context.CancelCauseFunc, 0, len(s.calls))
	for _, c := range s.calls {
		if c.running {
			cancels = append(cancels, c.cancel)
		}
	}
	if len(s.calls) == 0 {
		close(s.drained)
	}
	s.mu.Unlock()
	for _, cancel := range cancels {
		cancel(fault(f.ShuttingDown))
	}
}
func (s *LaunchFailureFinalizer) Drain(ctx context.Context) error {
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
func (s *LaunchFailureFinalizer) Joined() bool {
	if s == nil {
		return true
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.stopped && len(s.calls) == 0
}

func failureCommand(p i.ProjectID, id DispatchID) f.CommandIdentity {
	c, _ := handoffCommand(p, id, "finalize_launch_failure")
	return c
}
func failureLocks(p i.ProjectID, id DispatchID, r *dispatchRecord) ([]f.LockRequest, error) {
	locks, err := handoffLocks(p, id, "finalize_launch_failure", nil)
	if err != nil || r == nil {
		return locks, err
	}
	ak, _ := f.AgentLock(r.agent.String())
	tk, _ := f.AggregateLock(f.TaskAggregate, r.task)
	return oc.NormalizeLocks(append(locks, f.LockRequest{Key: ak, Mode: f.Shared}, f.LockRequest{Key: tk, Mode: f.Exclusive}))
}
func (s *LaunchFailureFinalizer) enabled(ctx context.Context, tx f.Tx, p i.ProjectID) error {
	current, err := s.deps.Projects.RequireSchedulerProjectInTx(ctx, tx, p)
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err != nil {
		return portError(err)
	}
	if current.Project.ID != p || current.Config.Validate() != nil {
		return unavailable(nil)
	}
	if !current.Config.Enabled {
		return fault(f.InvalidState)
	}
	// This is settlement, not a new Launch. Work checks the original/current
	// Sprint and Task relationship; no free-slot or capacity check belongs here.
	return nil
}

func (s *LaunchFailureFinalizer) read(ctx context.Context, call *failureCall) (*dispatchRecord, error) {
	locks, err := handoffLocks(call.project, call.id, "lookup_launch_failure", nil)
	if err != nil {
		return nil, err
	}
	command, _ := handoffCommand(call.project, call.id, "lookup_launch_failure")
	cause, _ := f.NewCommandsCause(command)
	var out *dispatchRecord
	result := s.authority.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := s.authority.store.AcquireAll(ctx, tx, locks); err != nil {
			return portError(err)
		}
		x, err := s.authority.inTx(ctx, tx, call.project)
		if err != nil {
			return err
		}
		out, err = loadDispatch(ctx, x, call.project, call.id)
		if err != nil {
			return err
		}
		if out == nil {
			return fault(f.NotFound)
		}
		return ctx.Err()
	})
	if err = commitError(result); err != nil {
		return nil, err
	}
	if out == nil {
		return nil, unavailable(nil)
	}
	return out, nil
}

func (s *LaunchFailureFinalizer) FinalizeLaunchFailure(ctx context.Context, p i.ProjectID, id DispatchID) (out Dispatch, err error) {
	ctx, call, err := s.begin(ctx, p, id, false)
	if err != nil {
		return Dispatch{}, err
	}
	retain := false
	defer func() { s.finish(call, retain, err) }()
	r, err := s.read(ctx, call)
	if err != nil {
		return Dispatch{}, err
	}
	if completedFinalFailure(r) {
		return snapshot(r), nil
	}
	request, err := failureRequest(r)
	if err != nil {
		return Dispatch{}, err
	}
	s.mu.Lock()
	call.record, call.request, call.stage, call.live = r, request, failureDiscovery, true
	s.mu.Unlock()
	plan, err := s.deps.Work.DiscoverTaskLaunchFailure(ctx, call.actor, request)
	if err != nil {
		return Dispatch{}, portError(err)
	}
	if nilPort(plan) {
		return Dispatch{}, fault(f.DependencyUnbound)
	}
	locks, err := failureLocks(p, id, r)
	if err != nil {
		return Dispatch{}, err
	}
	locks, err = oc.NormalizeLocks(append(locks, plan.RequiredLocks()...))
	if err != nil {
		return Dispatch{}, portError(err)
	}
	cause, _ := f.NewCommandsCause(failureCommand(p, id))
	var settled *dispatchRecord
	result := s.authority.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := s.authority.store.AcquireAll(ctx, tx, locks); err != nil {
			return portError(err)
		}
		x, err := s.authority.inTx(ctx, tx, p)
		if err != nil {
			return err
		}
		current, err := loadDispatch(ctx, x, p, id)
		if err != nil {
			return err
		}
		if !sameDispatch(current, r) || !sameFinalFailure(current, r) {
			return fault(f.ConfirmationStale)
		}
		if completedFinalFailure(current) {
			settled = current
			return ctx.Err()
		}
		if current.version != r.version || !pendingFinalFailure(current) {
			return fault(f.ConfirmationStale)
		}
		if err = s.enabled(ctx, tx, p); err != nil {
			return err
		}
		s.mu.Lock()
		call.stage, call.tx, call.plan = failureApplying, tx, plan
		call.locks = append([]f.LockRequest(nil), locks...)
		s.mu.Unlock()
		defer func() {
			s.mu.Lock()
			call.stage, call.tx, call.plan, call.locks = failureDiscovery, f.Tx{}, nil, nil
			s.mu.Unlock()
		}()
		applied, err := s.deps.Work.ApplyTaskLaunchFailureInTx(ctx, tx, call.actor, request, plan)
		if err != nil {
			return portError(err)
		}
		if nilPort(applied) {
			return fault(f.DependencyUnbound)
		}
		if err = s.deps.Work.CheckTaskLaunchFailureAppliedInTx(ctx, tx, call.actor, request, plan, applied); err != nil {
			return portError(err)
		}
		// Changed=false is a genuine Work-verified preservation outcome. It
		// is not an excuse to skip a missing/paused provider or failed callback.
		settled, err = nextDispatch(current)
		if err != nil {
			return err
		}
		settled.status = Failed
		at := settled.updatedAt
		settled.failedAt = &at
		return updateDispatch(ctx, x, settled, current.version)
	})
	err = commitError(result)
	if result.State() == f.Unknown {
		retain = true
	}
	if err != nil {
		return Dispatch{}, err
	}
	if settled == nil {
		return Dispatch{}, unavailable(nil)
	}
	return snapshot(settled), nil
}

// Lookup only observes the original settlement outcome, including after
// Stop. It never invokes Work or Launch. An unobserved failed result retains
// the original unknown physical attempt; a known rollback may be retried by
// the caller as the same Dispatch, with fresh discovery and current facts.
func (s *LaunchFailureFinalizer) Lookup(ctx context.Context, p i.ProjectID, id DispatchID) (out Dispatch, err error) {
	ctx, call, err := s.begin(ctx, p, id, true)
	if err != nil {
		return Dispatch{}, err
	}
	s.mu.Lock()
	prior, original := call.unknown, call.record
	s.mu.Unlock()
	retain := prior != nil
	defer func() { s.finish(call, retain, err) }()
	r, err := s.read(ctx, call)
	if err != nil {
		return Dispatch{}, err
	}
	if original != nil && (!sameDispatch(r, original) || !sameFinalFailure(r, original)) {
		return Dispatch{}, fault(f.ConfirmationStale)
	}
	if completedFinalFailure(r) {
		retain = false
		return snapshot(r), nil
	}
	if prior != nil {
		return Dispatch{}, prior
	}
	if !pendingFinalFailure(r) {
		return Dispatch{}, fault(f.InvalidState)
	}
	return snapshot(r), nil
}

// These predicates describe a durable source-specific receipt. They do not
// grant Work access; the authority also requires the private live call/Tx.
func finalFailureMarker(r *dispatchRecord) bool {
	return r != nil && r.finalAttempt > 0 && r.finalAttempt == r.attempts &&
		(r.failureReason == wc.TaskLaunchFailureUnsupportedResourceConstraints && r.failureCode == f.DependencyUnbound && len(r.launch.Policy.AllowedResourceConstraints) > 0 || exhaustedTemporary(r)) &&
		r.failureOccurredAt != nil && r.failureOccurredAt.Validate() == nil &&
		!r.failureOccurredAt.Time().Before(r.createdAt.Time()) && !r.failureOccurredAt.Time().After(r.updatedAt.Time()) &&
		r.outcome == KnownNotCreated && r.guard != nil && r.guard.valid() && r.execution == nil && r.nextRetry == nil &&
		r.busyAttempt == 0 && r.skipReason == "" && r.skippedAt == nil &&
		r.launch.Purpose == "task/work" &&
		r.launch.Lineage.RetryOf == nil && r.launch.Lineage.RegenerateOf == nil && r.launch.Lineage.ContributionGeneration == nil && r.launch.Lineage.ContributionAttempt == nil
}
func pendingFinalFailure(r *dispatchRecord) bool {
	return finalFailureMarker(r) && r.status == Pending && r.failedAt == nil
}
func completedFinalFailure(r *dispatchRecord) bool {
	return finalFailureMarker(r) && r.status == Failed && r.failedAt != nil && r.failedAt.Validate() == nil && r.failedAt.Time().Equal(r.updatedAt.Time())
}
func sameFinalFailure(a, b *dispatchRecord) bool {
	return a != nil && b != nil && a.finalAttempt == b.finalAttempt && a.failureReason == b.failureReason && a.failureCode == b.failureCode && a.failureOccurredAt != nil && b.failureOccurredAt != nil && a.failureOccurredAt.Time().Equal(b.failureOccurredAt.Time()) &&
		(a.failureReason != wc.TaskLaunchFailureRetryExhausted || exhaustedTemporary(a) && exhaustedTemporary(b) && a.retryPolicy == b.retryPolicy)
}
