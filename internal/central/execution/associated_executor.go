package execution

import (
	"context"
	"errors"
	"sync"
	"time"

	c "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

type AssociatedExecutorOptions struct {
	// MaxOwned includes active calls, retained Unknown and failures for which
	// there is no durable fact preventing an accidental second preparation.
	MaxOwned         int
	RecoveryInterval time.Duration
}

// AssociatedExecutor borrows two fixed drivers. It neither stops those shared
// drivers nor changes their authority. Only its own child contexts are cancelled.
// Run establishes the lifetime; Advance never inherits a traversal's lifetime.
type AssociatedExecutor struct{ state *associatedExecutorState }

type associatedExecutorState struct {
	mu               sync.Mutex
	drainMu          sync.Mutex
	options          AssociatedExecutorOptions
	preparation      *PreparationDriver
	direct           *DirectTextDriver
	ops              associatedExecutorOps
	started, stopped bool
	root             context.Context
	cancel           context.CancelFunc
	changed          chan struct{}
	ready            chan struct{}
	workers          int
	jobs             map[i.ExecutionID]*associatedCall
}

// All production functions below are fixed by the constructor. This private
// seam lets local tests control physical returns without fabricating a public
// preparation/Model permission. Real SQL and both driver proofs remain required
// in the production graph.
type associatedExecutorOps struct {
	inspect            func(context.Context, i.ProjectID, c.AssociatedDispatch) (associatedFacts, error)
	prepare            func(context.Context, i.ExecutionID) error
	recoverPreparation func(context.Context, i.ExecutionID) error
	start              func(context.Context, i.ExecutionID) (c.DirectTextReceipt, error)
	recoverStart       func(context.Context, i.ExecutionID) (c.DirectTextReceipt, error)
	owners             func(*associatedCall) associatedOwners
}

type associatedFacts struct {
	status        c.Status
	inputReady    bool
	claimTerminal bool
}
type associatedOwners struct {
	preparation, direct bool
	foreign             bool
	uncertainty         string
	err                 error
}
type associatedContextKey struct{}
type associatedCall struct {
	project  i.ProjectID
	dispatch c.AssociatedDispatch
	ctx      context.Context
	cancel   context.CancelFunc
	phase    c.ExecutionAdvancePhase
	status   c.Status
	active   bool
	retained bool
	owners   associatedOwners
	err      error
	next     time.Time
}

func NewAssociatedExecutor(preparation *PreparationDriver, direct *DirectTextDriver, options AssociatedExecutorOptions) (*AssociatedExecutor, error) {
	if preparation == nil || preparation.state == nil || direct == nil || direct.state == nil {
		return nil, fault(f.DependencyUnbound)
	}
	p, d := preparation.state, direct.state
	if nilPort(p.store) || nilPort(d.store) || p.store != d.store || p.authority == nil || d.authority == nil || p.authority.state == nil || d.authority.state == nil || p.authority.state != d.authority.state || p.process.Validate() != nil || p.process != d.process || nilPort(p.processes) || nilPort(d.processes) || p.processes.CurrentProcess() != p.process || d.processes.CurrentProcess() != d.process {
		return nil, fault(f.DependencyUnbound)
	}
	if options.MaxOwned < 1 || options.RecoveryInterval <= 0 {
		return nil, invalid()
	}
	p.mu.Lock()
	preparationStopped := p.stopped
	p.mu.Unlock()
	d.mu.Lock()
	directStopped := d.stopped
	d.mu.Unlock()
	if preparationStopped || directStopped {
		return nil, fault(f.ShuttingDown)
	}
	s := &associatedExecutorState{options: options, preparation: preparation, direct: direct, changed: make(chan struct{}), ready: make(chan struct{}), jobs: make(map[i.ExecutionID]*associatedCall)}
	s.ops = associatedExecutorOps{inspect: s.inspect, prepare: preparation.Run, recoverPreparation: preparation.ResolveUnknown, start: direct.Start, recoverStart: direct.ResolveUnknown, owners: s.ownedCalls}
	return &AssociatedExecutor{state: s}, nil
}

// Run is a one-shot service lifetime, not a polling loop. It waits for Stop or
// its root context and then for every original worker to physically return.
// Unresolved ownership is returned intact and keeps Joined false; Drain may
// subsequently converge those same owners under an explicit caller budget.
func (e *AssociatedExecutor) Run(ctx context.Context) error {
	if ctx == nil {
		return invalid()
	}
	if e == nil || e.state == nil {
		return fault(f.DependencyUnbound)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	s := e.state
	s.mu.Lock()
	if s.stopped || s.started {
		s.mu.Unlock()
		return fault(f.InvalidState)
	}
	s.root, s.cancel = context.WithCancel(ctx)
	s.started = true
	close(s.ready)
	s.notifyLocked()
	root := s.root
	s.mu.Unlock()
	<-root.Done()
	e.Stop()
	for {
		s.mu.Lock()
		if s.workers == 0 {
			err := s.retainedLocked()
			s.mu.Unlock()
			if err != nil {
				return err
			}
			return ctx.Err()
		}
		changed := s.changed
		s.mu.Unlock()
		<-changed
	}
}

// Ready closes only when Run has installed the root lifetime. It performs no
// work and grants no authority; callers can select it alongside Run's return
// instead of probing a business association to detect service startup.
func (e *AssociatedExecutor) Ready() <-chan struct{} {
	if e == nil || e.state == nil {
		return nil
	}
	return e.state.ready
}

func (e *AssociatedExecutor) Advance(ctx context.Context, project i.ProjectID, dispatch c.AssociatedDispatch) (c.ExecutionAdvance, error) {
	if ctx == nil || validateAssociated(project, dispatch) != nil {
		return c.ExecutionAdvance{}, invalid()
	}
	if err := ctx.Err(); err != nil {
		return c.ExecutionAdvance{}, err
	}
	if e == nil || e.state == nil {
		return c.ExecutionAdvance{}, fault(f.DependencyUnbound)
	}
	s := e.state
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.started {
		return c.ExecutionAdvance{}, fault(f.InvalidState)
	}
	if job := s.jobs[dispatch.ExecutionID]; job != nil {
		if job.project != project || job.dispatch != dispatch {
			return c.ExecutionAdvance{}, fault(f.ConfirmationStale)
		}
		// Stop forbids further asynchronous work. Explicit Drain owns any
		// remaining convergence; it cannot dispatch a first Model call.
		if job.retained && !job.active && !s.stopped && !time.Now().Before(job.next) {
			job.active = true
			job.next = time.Now().Add(s.options.RecoveryInterval)
			s.workers++
			go s.execute(job, true, job.ctx)
		}
		return s.observationLocked(job), job.err
	}
	if s.stopped || s.root.Err() != nil {
		return c.ExecutionAdvance{}, fault(f.ShuttingDown)
	}
	if len(s.jobs) >= s.options.MaxOwned {
		return c.ExecutionAdvance{}, fault(f.ResourceBusy)
	}
	job := &associatedCall{project: project, dispatch: dispatch, phase: c.ExecutionAccepted, active: true}
	job.ctx, job.cancel = context.WithCancel(context.WithValue(s.root, associatedContextKey{}, job))
	s.jobs[dispatch.ExecutionID] = job
	s.workers++
	go s.execute(job, false, job.ctx)
	return s.observationLocked(job), nil
}

func (s *associatedExecutorState) observationLocked(job *associatedCall) c.ExecutionAdvance {
	return c.ExecutionAdvance{ProjectID: job.project, ExecutionID: job.dispatch.ExecutionID, Phase: job.phase, Status: job.status, Active: job.active, Retained: job.retained, Joined: !job.active && !job.retained}
}
func (s *associatedExecutorState) notifyLocked() {
	close(s.changed)
	s.changed = make(chan struct{})
}
func (s *associatedExecutorState) retainedLocked() error {
	for _, job := range s.jobs {
		if job.retained {
			if job.err != nil {
				return job.err
			}
			return fault(f.ResourceBusy)
		}
	}
	return nil
}

func (e *AssociatedExecutor) Stop() {
	if e == nil || e.state == nil {
		return
	}
	s := e.state
	s.mu.Lock()
	s.stopped = true
	if s.cancel != nil {
		s.cancel()
	}
	for _, job := range s.jobs {
		job.cancel()
	}
	s.notifyLocked()
	s.mu.Unlock()
}

func (e *AssociatedExecutor) Joined() bool {
	if e == nil || e.state == nil {
		return false
	}
	s := e.state
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.stopped && s.workers == 0 && s.retainedLocked() == nil
}

// Drain first waits for the original calls. It may then make at most one paced
// recovery attempt per retained owner under this caller's explicit context.
// A start-commit Unknown cannot be advanced here: that branch can issue the
// first Model call and is therefore retained after Stop. No shared driver is
// stopped/drained, and an unresolved owner is never forgotten to report success.
func (e *AssociatedExecutor) Drain(ctx context.Context) error {
	if ctx == nil {
		return invalid()
	}
	if e == nil || e.state == nil {
		return fault(f.DependencyUnbound)
	}
	e.Stop()
	s := e.state
	s.drainMu.Lock()
	defer s.drainMu.Unlock()
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		s.mu.Lock()
		if s.workers == 0 {
			s.mu.Unlock()
			break
		}
		changed := s.changed
		s.mu.Unlock()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-changed:
		}
	}
	s.mu.Lock()
	var retry []*associatedCall
	for _, job := range s.jobs {
		if job.retained && !time.Now().Before(job.next) && !(job.owners.direct && job.owners.uncertainty == "start") {
			retry = append(retry, job)
		}
	}
	s.mu.Unlock()
	for _, job := range retry {
		if err := ctx.Err(); err != nil {
			return err
		}
		s.mu.Lock()
		job.active = true
		job.next = time.Now().Add(s.options.RecoveryInterval)
		s.workers++
		s.mu.Unlock()
		// The recovery keeps the original token but has this explicit cleanup
		// budget. It is synchronous: Drain cannot outlive its physical return.
		s.execute(job, true, context.WithValue(ctx, associatedContextKey{}, job))
	}
	s.mu.Lock()
	err := s.retainedLocked()
	if err == nil {
		clear(s.jobs)
	}
	s.mu.Unlock()
	if err != nil {
		return err
	}
	return ctx.Err()
}

type associatedOutcome struct {
	phase  c.ExecutionAdvancePhase
	status c.Status
	forget bool
	err    error
}

func (s *associatedExecutorState) execute(job *associatedCall, recoverOriginal bool, ctx context.Context) {
	outcome := associatedOutcome{phase: c.ExecutionDeferred}
	defer func() {
		if recover() != nil {
			outcome.err = unavailable(nil)
			outcome.forget = false
		}
		owners := s.ops.owners(job)
		s.mu.Lock()
		job.active = false
		job.owners = owners
		job.retained = owners.preparation || owners.direct
		job.phase, job.status, job.err = outcome.phase, outcome.status, outcome.err
		if job.retained {
			job.phase = c.ExecutionRetained
			if owners.err != nil {
				job.err = owners.err
			}
			if job.err == nil {
				job.err = fault(f.ResourceBusy)
			}
			job.next = time.Now().Add(s.options.RecoveryInterval)
		} else if outcome.forget {
			delete(s.jobs, job.dispatch.ExecutionID)
			job.cancel()
		}
		s.workers--
		s.notifyLocked()
		s.mu.Unlock()
	}()
	outcome = s.advanceOwned(ctx, job, recoverOriginal)
}

func (s *associatedExecutorState) advanceOwned(ctx context.Context, job *associatedCall, recoverOriginal bool) associatedOutcome {
	out := associatedOutcome{phase: c.ExecutionDeferred}
	recoveredDirect := false
	if err := ctx.Err(); err != nil {
		out.err = err
		return out
	}
	if recoverOriginal {
		owners := s.ops.owners(job)
		switch {
		case owners.preparation:
			out.err = s.ops.recoverPreparation(ctx, job.dispatch.ExecutionID)
		case owners.direct:
			recoveredDirect = true
			_, out.err = s.ops.recoverStart(ctx, job.dispatch.ExecutionID)
		default:
			out.err = fault(f.InvalidState)
			return out
		}
		if owned := s.ops.owners(job); owned.preparation || owned.direct {
			return out
		}
		// A nil preparation recovery only retires the old attempt. The
		// canonical input below, not that nil, decides whether Start is possible.
	}
	facts, err := s.ops.inspect(ctx, job.project, job.dispatch)
	if err != nil {
		out.err = preserveAssociatedError(out.err, err)
		return out
	}
	out.status = facts.status
	if facts.status.Terminal() {
		out.phase, out.forget = c.ExecutionTerminal, true
		return out
	}
	if out.err != nil {
		return out
	}
	if recoveredDirect {
		// ResolveUnknown owns all continuation of that original Start. Its
		// return must never become permission for a second Start/Model call.
		out.err = fault(f.InvalidState)
		return out
	}
	s.mu.Lock()
	stopped := s.stopped
	s.mu.Unlock()
	if stopped {
		// Explicit cleanup can retire a preparation owner but must not start
		// the first Model call after shutdown.
		out.forget = true
		return out
	}
	if owned := s.ops.owners(job); owned.foreign || owned.preparation || owned.direct {
		out.err = fault(f.ResourceBusy)
		return out
	}
	if facts.status == c.Created && !recoverOriginal {
		s.setPhase(job, c.ExecutionPreparing, facts.status)
		err = s.ops.prepare(ctx, job.dispatch.ExecutionID)
		if owned := s.ops.owners(job); owned.preparation || owned.direct {
			out.err = err
			return out
		}
		// Re-read even after an error: known failed capture can leave a
		// terminal attempt with no input, which safely excludes recapture.
		facts, inspectErr := s.ops.inspect(ctx, job.project, job.dispatch)
		if inspectErr != nil {
			out.err = preserveAssociatedError(err, inspectErr)
			return out
		}
		out.status = facts.status
		if facts.status.Terminal() {
			out.phase, out.forget, out.err = c.ExecutionTerminal, true, err
			return out
		}
		if facts.status == c.Preparing && !facts.inputReady && facts.claimTerminal {
			out.forget, out.err = true, err
			return out
		}
		if err != nil {
			out.err = err
			return out
		}
		return s.startPrepared(ctx, job, facts)
	}
	return s.startPrepared(ctx, job, facts)
}

func (s *associatedExecutorState) startPrepared(ctx context.Context, job *associatedCall, facts associatedFacts) associatedOutcome {
	out := associatedOutcome{phase: c.ExecutionDeferred, status: facts.status}
	if facts.status != c.Preparing || !facts.inputReady {
		// Existing empty preparation, running/waiting without our original
		// owner, and a recovered claim without input never create new work.
		out.forget = facts.status != c.Created
		return out
	}
	s.mu.Lock()
	stopped := s.stopped
	s.mu.Unlock()
	if stopped || ctx.Err() != nil {
		out.err = ctx.Err()
		return out
	}
	s.setPhase(job, c.ExecutionStarting, facts.status)
	receipt, err := s.ops.start(ctx, job.dispatch.ExecutionID)
	out.err = err
	if owned := s.ops.owners(job); owned.preparation || owned.direct {
		return out
	}
	// A completed driver receipt is authoritative only after its actual
	// original call returned and no retained owner remains.
	if receipt.ExecutionID == job.dispatch.ExecutionID && receipt.Status.Terminal() && receipt.Version.Validate() == nil {
		out.status, out.phase, out.forget = receipt.Status, c.ExecutionTerminal, true
	}
	return out
}

func (s *associatedExecutorState) setPhase(job *associatedCall, phase c.ExecutionAdvancePhase, status c.Status) {
	s.mu.Lock()
	job.phase, job.status = phase, status
	s.notifyLocked()
	s.mu.Unlock()
}

func preserveAssociatedError(original, next error) error {
	if original == nil {
		return portError(next)
	}
	if next == nil || errors.Is(next, original) {
		return original
	}
	return errors.Join(original, portError(next))
}

var _ c.AssociatedExecutor = (*AssociatedExecutor)(nil)
