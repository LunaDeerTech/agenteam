package scheduler

import (
	"context"
	"sync"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	wc "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
)

type BusyCompensatorDependencies struct {
	Projects ProjectSchedulerGate
	Work     wc.SchedulerTaskBusyCompensations
}

// BusyCompensator closes one durably confirmed AgentBusy outcome. Work alone
// owns restoration, preservation, rank maintenance, history and Outbox. This
// owner checks Work's private applied proof before closing the same Dispatch
// in the same transaction. Neither a public error nor a generic known failure
// is a Busy receipt. Construction is immutable and performs no I/O.
type BusyCompensator struct {
	authority *PendingAuthority
	deps      BusyCompensatorDependencies
	mu        sync.Mutex
	stopped   bool
	calls     map[DispatchID]*busyCall
	drained   chan struct{}
}
type busyStage uint8

const (
	busyDiscovery busyStage = iota + 1
	busyApplying
)

type busyContextKey struct{}
type busyCall struct {
	owner   *BusyCompensator
	project i.ProjectID
	id      DispatchID
	actor   i.Actor
	// owner.mu protects lifecycle and the callback-local proof below.
	cancel  context.CancelCauseFunc
	running bool
	live    bool
	unknown error
	record  *dispatchRecord
	request wc.TaskBusyCompensationRequest
	stage   busyStage
	tx      f.Tx
	plan    wc.TaskBusyCompensationPlan
	locks   []f.LockRequest
}

func NewBusyCompensator(a *PendingAuthority, deps BusyCompensatorDependencies) (*BusyCompensator, error) {
	if a == nil || nilPort(a.store) || nilPort(deps.Projects) || nilPort(deps.Work) {
		return nil, fault(f.DependencyUnbound)
	}
	return &BusyCompensator{authority: a, deps: deps, calls: make(map[DispatchID]*busyCall), drained: make(chan struct{})}, nil
}
func (s *BusyCompensator) begin(ctx context.Context, p i.ProjectID, id DispatchID, lookup bool) (context.Context, *busyCall, error) {
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
		call = &busyCall{owner: s, project: p, id: id, actor: actor}
		s.calls[id] = call
	}
	owned, cancel := context.WithCancelCause(ctx)
	call.cancel, call.running = cancel, true
	return context.WithValue(owned, busyContextKey{}, call), call, nil
}
func (s *BusyCompensator) finish(call *busyCall, retain bool, err error) {
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
func (s *BusyCompensator) Stop() {
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
func (s *BusyCompensator) Drain(ctx context.Context) error {
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
func (s *BusyCompensator) Joined() bool {
	if s == nil {
		return true
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.stopped && len(s.calls) == 0
}

func busyCommand(p i.ProjectID, id DispatchID) f.CommandIdentity {
	c, _ := handoffCommand(p, id, "compensate_agent_busy")
	return c
}
func busyLocks(p i.ProjectID, id DispatchID, r *dispatchRecord) ([]f.LockRequest, error) {
	locks, err := handoffLocks(p, id, "compensate_agent_busy", nil)
	if err != nil || r == nil {
		return locks, err
	}
	ak, _ := f.AgentLock(r.agent.String())
	tk, _ := f.AggregateLock(f.TaskAggregate, r.task)
	return oc.NormalizeLocks(append(locks, f.LockRequest{Key: ak, Mode: f.Shared}, f.LockRequest{Key: tk, Mode: f.Exclusive}))
}
func (s *BusyCompensator) enabled(ctx context.Context, tx f.Tx, p i.ProjectID) error {
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

func (s *BusyCompensator) read(ctx context.Context, call *busyCall) (*dispatchRecord, error) {
	locks, err := handoffLocks(call.project, call.id, "lookup_busy_compensation", nil)
	if err != nil {
		return nil, err
	}
	command, _ := handoffCommand(call.project, call.id, "lookup_busy_compensation")
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

func (s *BusyCompensator) CompensateAgentBusy(ctx context.Context, p i.ProjectID, id DispatchID) (out Dispatch, err error) {
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
	if completedBusy(r) {
		return snapshot(r), nil
	}
	if validRelaunchOrigin(r) {
		out, retain, err = s.skipBusyRelaunch(ctx, call, r)
		return out, err
	}
	request, err := busyRequest(r)
	if err != nil {
		return Dispatch{}, err
	}
	s.mu.Lock()
	call.record, call.request, call.stage, call.live = r, request, busyDiscovery, true
	s.mu.Unlock()
	plan, err := s.deps.Work.DiscoverTaskBusyCompensation(ctx, call.actor, request)
	if err != nil {
		return Dispatch{}, portError(err)
	}
	if nilPort(plan) {
		return Dispatch{}, fault(f.DependencyUnbound)
	}
	locks, err := busyLocks(p, id, r)
	if err != nil {
		return Dispatch{}, err
	}
	locks, err = oc.NormalizeLocks(append(locks, plan.RequiredLocks()...))
	if err != nil {
		return Dispatch{}, portError(err)
	}
	cause, _ := f.NewCommandsCause(busyCommand(p, id))
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
		if !sameDispatch(current, r) || current.busyAttempt != r.busyAttempt {
			return fault(f.ConfirmationStale)
		}
		if completedBusy(current) {
			settled = current
			return ctx.Err()
		}
		if current.version != r.version || !pendingBusy(current) {
			return fault(f.ConfirmationStale)
		}
		if err = s.enabled(ctx, tx, p); err != nil {
			return err
		}
		s.mu.Lock()
		call.stage, call.tx, call.plan = busyApplying, tx, plan
		call.locks = append([]f.LockRequest(nil), locks...)
		s.mu.Unlock()
		defer func() {
			s.mu.Lock()
			call.stage, call.tx, call.plan, call.locks = busyDiscovery, f.Tx{}, nil, nil
			s.mu.Unlock()
		}()
		applied, err := s.deps.Work.ApplyTaskBusyCompensationInTx(ctx, tx, call.actor, request, plan)
		if err != nil {
			return portError(err)
		}
		if nilPort(applied) {
			return fault(f.DependencyUnbound)
		}
		if err = s.deps.Work.CheckTaskBusyCompensationAppliedInTx(ctx, tx, call.actor, request, plan, applied); err != nil {
			return portError(err)
		}
		// Restored=false is a genuine Work-verified preservation outcome. It
		// is not an excuse to skip a missing/paused provider or failed callback.
		settled, err = nextDispatch(current)
		if err != nil {
			return err
		}
		settled.status, settled.skipReason = Skipped, "agent_busy"
		at := settled.updatedAt
		settled.skippedAt = &at
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

// Lookup only observes the original compensation outcome, including after
// Stop. It never invokes Work or Launch. An unobserved skipped result retains
// the original unknown physical attempt; a known rollback may be retried by
// the caller as the same Dispatch, with fresh discovery and current facts.
func (s *BusyCompensator) Lookup(ctx context.Context, p i.ProjectID, id DispatchID) (out Dispatch, err error) {
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
	if original != nil && (!sameDispatch(r, original) || r.busyAttempt != original.busyAttempt) {
		return Dispatch{}, fault(f.ConfirmationStale)
	}
	if completedBusy(r) {
		retain = false
		return snapshot(r), nil
	}
	if prior != nil {
		return Dispatch{}, prior
	}
	if !pendingBusy(r) {
		return Dispatch{}, fault(f.InvalidState)
	}
	return snapshot(r), nil
}

func pendingBusy(r *dispatchRecord) bool {
	return r != nil && r.status == Pending && r.outcome == KnownNotCreated && r.busyAttempt > 0 && r.busyAttempt == r.attempts && validDispatchOrigin(r) && r.execution == nil && r.nextRetry == nil && r.skipReason == "" && r.skippedAt == nil
}
func completedBusy(r *dispatchRecord) bool {
	return r != nil && r.status == Skipped && r.outcome == KnownNotCreated && r.busyAttempt > 0 && r.busyAttempt == r.attempts && r.skipReason == "agent_busy" && r.skippedAt != nil
}
