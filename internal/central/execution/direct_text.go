package execution

import (
	"context"
	"errors"
	"math"
	"reflect"
	"time"

	ac "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/agentloop"
	c "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	pvc "github.com/LunaDeerTech/agenteam/internal/central/projectvariable/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/skill/contract"
)

type DirectTextDependencies struct {
	Context     *ContextBuilder
	Loop        *agentloop.DirectTextController
	Processes   oc.ProcessAuthority
	Projects    c.PreparationProjectGate
	Agents      ac.ExecutionConfiguration
	Skills      sc.InitialRoundBindingsReader
	Events      oc.Appender
	Lifecycle   c.DirectTextEvents
	Models      mc.ExecutionModelRetirement
	Environment pvc.EnvironmentRetirement
}
type DirectTextDriver struct{ state *directTextState }
type directTextInvocation struct {
	session         *agentloop.DirectTextSession
	request         agentloop.DirectTextRequest
	skillPlan       sc.InitialRoundBindingsPlan
	startPlan       oc.AppendPlan
	modelPlan       mc.ExecutionModelRetirementPlan
	environmentPlan pvc.EnvironmentRetirementPlan
	modelRetirement mc.ExecutionModelRetirementRequest
	callError       error
	response        *mc.ModelResponse
}

func NewDirectTextDriver(store Store, a *Authority, deps DirectTextDependencies) (*DirectTextDriver, error) {
	if nilPort(store) || !reflect.TypeOf(store).Comparable() || a == nil || a.state == nil || a.state.store != store || deps.Context == nil || deps.Loop == nil || nilPort(deps.Processes) || nilPort(deps.Projects) || nilPort(deps.Agents) || nilPort(deps.Skills) || nilPort(deps.Events) || !deps.Lifecycle.Valid() || nilPort(deps.Models) || nilPort(deps.Environment) {
		return nil, fault(f.DependencyUnbound)
	}
	process := deps.Processes.CurrentProcess()
	if process.Validate() != nil {
		return nil, fault(f.DependencyUnbound)
	}
	return &DirectTextDriver{&directTextState{store: store, authority: a, processes: deps.Processes, process: process, deps: deps, calls: map[i.ExecutionID]*directTextCall{}, changed: make(chan struct{})}}, nil
}

// Start consumes the original complete preparation input. It accepts an actual
// Loop session without I/O, commits Snapshot/running/Started and RoundInput,
// then invokes that one session. Completion changes Execution only, never Task.
// Unknown keeps its exact run/session/candidates for ResolveUnknown.
func (d *DirectTextDriver) Start(ctx context.Context, execution i.ExecutionID) (receipt c.DirectTextReceipt, err error) {
	if d == nil || d.state == nil {
		return receipt, fault(f.DependencyUnbound)
	}
	if ctx == nil || execution.Validate() != nil {
		return receipt, invalid()
	}
	if err = ctx.Err(); err != nil {
		return receipt, err
	}
	s := d.state
	s.mu.Lock()
	if s.stopped {
		s.mu.Unlock()
		return receipt, fault(f.ShuttingDown)
	}
	if s.calls[execution] != nil {
		s.mu.Unlock()
		return receipt, fault(f.ResourceBusy)
	}
	runCtx, cancel := context.WithCancel(ctx)
	run := &directTextCall{owner: s, cancel: cancel, request: c.PreparationRequest{ExecutionID: execution}, phase: "loading", invocation: &directTextInvocation{}}
	run.ctx = context.WithValue(runCtx, directTextContextKey{}, run)
	s.calls[execution] = run
	s.mu.Unlock()
	defer func() { s.returned(run) }()
	row, stored, err := s.readInitial(run.ctx, run)
	if err != nil {
		return receipt, err
	}
	if stored != nil {
		if row.summary.Status.Terminal() && stored.terminal != nil {
			return directTextReceipt(row, stored), nil
		}
		return receipt, fault(f.ResourceBusy)
	}
	if err = s.prepare(run.ctx, run); err != nil {
		return receipt, err
	}
	if err = s.start(run.ctx, run); err != nil {
		if directTextUnknown(err) {
			s.mu.Lock()
			run.uncertainty = "start"
			s.mu.Unlock()
			s.retain(run, err)
		}
		return s.receipt(run), err
	}
	return s.callAndFinish(run.ctx, run, false)
}

func (s *directTextState) readInitial(ctx context.Context, run *directTextCall) (row *executionRecord, stored *directTextRecord, err error) {
	// Read the canonical immutable launch solely to plan the complete union.
	row, err = loadExecution(ctx, s.store, run.request.ExecutionID)
	if err != nil {
		return
	}
	if row == nil {
		err = fault(f.NotFound)
		return
	}
	run.request = c.PreparationRequest{ExecutionID: row.summary.ID, Launch: row.launch.Clone()}
	locks, e := preparationLocks(run.request)
	if e != nil {
		err = e
		return
	}
	cause, _ := f.NewRecoveryCause("execution.direct-text.read", run.request.ExecutionID.String(), run.request.Launch.Meta.RequestID.String())
	result := s.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if e := s.store.AcquireAll(ctx, tx, locks); e != nil {
			return portError(e)
		}
		x, e := s.store.InTx(tx)
		if e != nil {
			return portError(e)
		}
		if s.processes.CurrentProcess() != s.process {
			return fault(f.InvalidState)
		}
		row, e = loadExecution(ctx, x, run.request.ExecutionID)
		if e != nil {
			return e
		}
		if row == nil || !((c.PreparationRequest{ExecutionID: row.summary.ID, Launch: row.launch}).Equal(run.request)) {
			return fault(f.ConfirmationStale)
		}
		stored, e = loadDirectText(ctx, x, run.request.ExecutionID)
		if e != nil {
			return e
		}
		if stored != nil {
			return ctx.Err()
		}
		if row.summary.Status != c.Preparing || row.summary.CancelRequestedAt != nil {
			return fault(f.InvalidState)
		}
		input, e := loadPreparationInput(ctx, x, run.request.ExecutionID)
		if e != nil {
			return e
		}
		if input == nil || !input.input.Fields().Request.Equal(run.request) || input.claim.process != s.process {
			return fault(f.DependencyUnbound)
		}
		claim, e := loadPreparationClaim(ctx, x, run.request.ExecutionID)
		if e != nil {
			return e
		}
		if claim == nil || claim.phase != "terminal" || claim.attempt != input.claim.attempt || claim.process != input.claim.process || claim.fence != input.claim.fence {
			return fault(f.ResourceBusy)
		}
		if _, e = s.deps.Projects.RequirePreparingProjectInTx(ctx, tx, run.request.Launch.ProjectID); e != nil {
			return portError(e)
		}
		run.input = input
		run.summary = row.summary.Clone()
		return ctx.Err()
	})
	// No input/output writer or Loop admission has occurred in this read Tx.
	err = commitError(result)
	return
}

func (s *directTextState) prepare(ctx context.Context, run *directTextCall) error {
	built, err := s.deps.Context.BuildContext(ctx, run.input.input)
	if err != nil {
		return portError(err)
	}
	start, err := f.NewID[c.DirectTextStart]()
	if err != nil {
		return unavailable(err)
	}
	snapshot, err := f.NewID[c.Snapshot]()
	if err != nil {
		return unavailable(err)
	}
	round, err := f.NewID[c.Round]()
	if err != nil {
		return unavailable(err)
	}
	binding, err := f.NewID[c.InputBinding]()
	if err != nil {
		return unavailable(err)
	}
	call, err := f.NewID[mc.Call]()
	if err != nil {
		return unavailable(err)
	}
	at, err := f.NewInstant(time.Now())
	if err != nil {
		return err
	}
	if at.Time().Before(run.input.input.Fields().CapturedAt.Time()) {
		return fault(f.InvalidState)
	}
	actor, err := i.NewAgentRun(run.request.Launch.ProjectID, run.request.Launch.AgentID, run.request.ExecutionID)
	if err != nil {
		return err
	}
	request, err := agentloop.BuildDirectTextRequest(ctx, built, actor, round.String(), call)
	if err != nil {
		return portError(err)
	}
	run.snapshot, err = c.NewDirectTextSnapshot(c.DirectTextSnapshotFields{ID: snapshot, StartID: start, ProcessID: s.process, Context: built, CreatedAt: at})
	if err != nil {
		return err
	}
	mr := request.Request()
	run.round, err = c.NewDirectTextRound(c.DirectTextRoundFields{ID: round, InputBindingID: binding, ExecutionID: run.request.ExecutionID, SnapshotID: snapshot, StartID: start, CallID: call, ContextDigest: built.Digest(), Input: mr.Input, Messages: mr.Messages, CreatedAt: at})
	if err != nil {
		return err
	}
	run.runtimeLocks, err = directTextRuntimeLocks(run.request)
	if err != nil {
		return err
	}
	run.invocation.request = request
	run.invocation.skillPlan, err = s.deps.Skills.DiscoverInitialRoundBindings(ctx, sc.InitialRoundBindingsRequest{Initial: run.input.input.Fields().Skills, CaptureAttemptBinding: run.input.input.Fields().AttemptBinding})
	if err != nil {
		return portError(err)
	}
	if nilPort(run.invocation.skillPlan) {
		return fault(f.DependencyUnavailable)
	}
	if run.summary.Version == f.Version(math.MaxInt64) {
		return fault(f.InvalidState)
	}
	s.mu.Lock()
	run.phase = "planning"
	s.mu.Unlock()
	run.event, err = s.lifecycleEvent(run, c.Running, "started", run.summary.Version+1, 1, at)
	if err != nil {
		return err
	}
	run.invocation.startPlan, err = s.deps.Events.PrepareAppend(ctx, actor, run.event)
	if err != nil {
		return portError(err)
	}
	locks, err := preparationLocks(run.request)
	if err != nil {
		return err
	}
	locks = append(locks, run.invocation.skillPlan.RequiredLocks()...)
	locks = append(locks, run.invocation.startPlan.Locks()...)
	run.locks, err = oc.NormalizeLocks(locks)
	if err != nil {
		return err
	}
	run.invocation.session, err = s.deps.Loop.Accept(request)
	if err != nil {
		return portError(err)
	}
	return ctx.Err()
}

func (s *directTextState) start(ctx context.Context, run *directTextCall) error {
	cause, _ := f.NewJobCause("execution-direct-text-start", run.request.ExecutionID.String(), run.snapshot.Fields().StartID.String())
	result := s.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := s.store.AcquireAll(ctx, tx, run.locks); err != nil {
			return portError(err)
		}
		x, err := s.store.InTx(tx)
		if err != nil {
			return portError(err)
		}
		if s.processes.CurrentProcess() != s.process {
			return fault(f.InvalidState)
		}
		row, err := loadExecution(ctx, x, run.request.ExecutionID)
		if err != nil {
			return err
		}
		input, err := loadPreparationInput(ctx, x, run.request.ExecutionID)
		if err != nil {
			return err
		}
		if row == nil || row.summary.Status != c.Preparing || row.summary.Version != run.summary.Version || row.summary.CancelRequestedAt != nil || !((c.PreparationRequest{ExecutionID: row.summary.ID, Launch: row.launch}).Equal(run.request)) || !samePreparationInput(input, run.input) {
			return fault(f.ConfirmationStale)
		}
		claim, err := loadPreparationClaim(ctx, x, run.request.ExecutionID)
		if err != nil {
			return err
		}
		if claim == nil || claim.phase != "terminal" || claim.attempt != input.claim.attempt || claim.process != input.claim.process || claim.fence != input.claim.fence {
			return fault(f.ConfirmationStale)
		}
		if _, err = s.deps.Projects.RequirePreparingProjectInTx(ctx, tx, run.request.Launch.ProjectID); err != nil {
			return portError(err)
		}
		initial := run.input.input.Fields().Skills
		actual, err := s.deps.Skills.ReadInitialRoundBindingsInTx(ctx, tx, sc.InitialRoundBindingsRequest{Initial: initial, CaptureAttemptBinding: run.input.input.Fields().AttemptBinding}, run.invocation.skillPlan)
		if err != nil {
			return portError(err)
		}
		if !reflect.DeepEqual(initial, actual) {
			return fault(f.ConfirmationStale)
		}
		s.mu.Lock()
		run.phase = "starting"
		run.tx = tx
		s.mu.Unlock()
		defer func() { s.mu.Lock(); run.tx = f.Tx{}; s.mu.Unlock() }()
		if err = insertDirectText(ctx, x, run, run.event.Header().EventID); err != nil {
			return err
		}
		actor, _ := i.NewAgentRun(run.request.Launch.ProjectID, run.request.Launch.AgentID, run.request.ExecutionID)
		if _, err = s.deps.Agents.ReadExecutionConfigurationInTx(ctx, tx, ac.ExecutionConfigurationRequest{Actor: actor, ProjectID: run.request.Launch.ProjectID, AgentID: run.request.Launch.AgentID, ExecutionID: run.request.ExecutionID, Stage: ac.ExecutionConfigurationCurrent}); err != nil {
			return portError(err)
		}
		if _, err = s.deps.Events.AppendEventInTx(ctx, tx, actor, run.event, run.invocation.startPlan); err != nil {
			return portError(err)
		}
		return ctx.Err()
	})
	if err := commitError(result); err != nil {
		return err
	}
	s.mu.Lock()
	run.started = true
	run.phase = "calling"
	run.summary.Status = c.Running
	run.summary.Version++
	sid := run.snapshot.Fields().ID
	at := run.snapshot.Fields().CreatedAt
	run.summary.SnapshotID = &sid
	run.summary.StartedAt = &at
	s.mu.Unlock()
	return nil
}

func directTextUnknown(err error) bool {
	var known *f.Fault
	return errors.As(err, &known) && known.CommitState == f.Unknown
}
func (s *directTextState) retain(run *directTextCall, err error) {
	s.mu.Lock()
	if run.unresolved == nil {
		run.unresolved = err
	}
	s.mu.Unlock()
}
func (s *directTextState) receipt(run *directTextCall) c.DirectTextReceipt {
	if run.snapshot.Validate() != nil || run.round.Validate() != nil {
		return c.DirectTextReceipt{ExecutionID: run.request.ExecutionID}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	sf, rf := run.snapshot.Fields(), run.round.Fields()
	through := f.Sequence(1)
	if run.terminal != nil && run.summary.Status.Terminal() {
		through = run.terminal.through
	}
	return c.DirectTextReceipt{ExecutionID: run.request.ExecutionID, StartID: sf.StartID, SnapshotID: sf.ID, RoundID: rf.ID, CallID: rf.CallID, Status: run.summary.Status, Version: run.summary.Version, TranscriptThrough: through}
}
func (s *directTextState) returned(run *directTextCall) {
	// Accepted-but-not-started sessions own no Model call. Retire them only
	// after a known failure; an unknown startup must retain its acceptance.
	s.mu.Lock()
	retained := run.unresolved != nil
	s.mu.Unlock()
	if !retained && run.invocation.session != nil && !run.invocation.session.Joined() {
		run.invocation.session.Stop()
		wait, cancel := context.WithTimeout(context.WithoutCancel(run.ctx), 3*time.Second)
		err := run.invocation.session.Drain(wait)
		cancel()
		if err != nil || !run.invocation.session.Joined() {
			if err == nil {
				err = fault(f.ResourceBusy)
			}
			s.retain(run, err)
		}
	}
	s.mu.Lock()
	run.returned = true
	run.resolving = false
	if run.unresolved == nil {
		delete(s.calls, run.request.ExecutionID)
		run.cancel()
	}
	close(s.changed)
	s.changed = make(chan struct{})
	s.mu.Unlock()
}

func (d *DirectTextDriver) Stop() {
	if d == nil || d.state == nil {
		return
	}
	s := d.state
	s.mu.Lock()
	s.stopped = true
	for _, run := range s.calls {
		run.cancel()
	}
	s.mu.Unlock()
}
func (d *DirectTextDriver) Joined() bool {
	if d == nil || d.state == nil {
		return false
	}
	s := d.state
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.stopped && len(s.calls) == 0
}
func (d *DirectTextDriver) Drain(ctx context.Context) error {
	if ctx == nil {
		return invalid()
	}
	if d == nil || d.state == nil {
		return fault(f.DependencyUnbound)
	}
	d.Stop()
	s := d.state
	for {
		s.mu.Lock()
		if len(s.calls) == 0 {
			s.mu.Unlock()
			return ctx.Err()
		}
		changed := s.changed
		var retained error
		active := false
		for _, run := range s.calls {
			if !run.returned || run.resolving {
				active = true
			} else if run.unresolved != nil {
				retained = run.unresolved
			}
		}
		s.mu.Unlock()
		if !active && retained != nil {
			return retained
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-changed:
		}
	}
}
