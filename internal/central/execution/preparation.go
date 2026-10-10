package execution

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"time"

	ac "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
)

type PreparationDependencies struct {
	Agents    ac.ExecutionConfiguration
	Projects  c.PreparationProjectGate
	Processes oc.ProcessAuthority
	Task      c.TriggerCaptureProvider
	Meeting   c.TriggerCaptureProvider
}

// PreparationDriver owns a synchronous attempt to prepare an existing
// Execution. It is not a Launch/Trigger registry or a background worker. The
// composition root must keep its real ProcessGuard held until Joined is true.
// CurrentProcess is an identity check, not a pin of that guard.
type PreparationDriver struct{ state *preparationState }
type preparationState struct {
	store         Store
	authority     *Authority
	agents        ac.ExecutionConfiguration
	projects      c.PreparationProjectGate
	processes     oc.ProcessAuthority
	process       oc.ProcessID
	task, meeting c.TriggerCaptureProvider
	mu            sync.Mutex
	stopped       bool
	calls         map[i.ExecutionID]*preparationCall
	returned      map[i.ExecutionID]preparationClaim
	changed       chan struct{}
}
type preparationCall struct {
	cancel              context.CancelFunc
	request             c.PreparationRequest
	claim               *preparationClaim
	returned, resolving bool
	unresolved          error
}

func NewPreparationDriver(store Store, authority *Authority, deps PreparationDependencies) (*PreparationDriver, error) {
	if nilPort(store) || !reflect.TypeOf(store).Comparable() || authority == nil || authority.state == nil || authority.state.store != store || nilPort(deps.Agents) || nilPort(deps.Projects) || nilPort(deps.Processes) {
		return nil, fault(f.DependencyUnbound)
	}
	process := deps.Processes.CurrentProcess()
	if process.Validate() != nil {
		return nil, fault(f.DependencyUnbound)
	}
	return &PreparationDriver{&preparationState{store: store, authority: authority, agents: deps.Agents, projects: deps.Projects, processes: deps.Processes, process: process, task: deps.Task, meeting: deps.Meeting, calls: map[i.ExecutionID]*preparationCall{}, returned: map[i.ExecutionID]preparationClaim{}, changed: make(chan struct{})}}, nil
}

// Run reads the stored original Launch, commits created -> preparing and a
// fenced attempt, then crosses the real Project/Trigger/Agent capture ports.
// The remaining complete Model/Tool/Skill/ref/lease capture is not installed in
// this slice: its absence returns DependencyUnbound from the original capture
// transaction, rolling back every source reference and retaining zero input.
// No partial preparation, Snapshot, running state or Started event is written.
func (d *PreparationDriver) Run(ctx context.Context, execution i.ExecutionID) (err error) {
	if ctx == nil || execution.Validate() != nil {
		return invalid()
	}
	if d == nil || d.state == nil {
		return fault(f.DependencyUnbound)
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	s := d.state
	s.mu.Lock()
	if s.stopped {
		s.mu.Unlock()
		return fault(f.ShuttingDown)
	}
	if _, exists := s.calls[execution]; exists {
		s.mu.Unlock()
		return fault(f.ResourceBusy)
	}
	runCtx, cancel := context.WithCancel(ctx)
	run := &preparationCall{cancel: cancel}
	s.calls[execution] = run
	s.mu.Unlock()
	started, terminal := false, false
	defer func() {
		cancel()
		s.mu.Lock()
		run.returned = true
		if run.unresolved == nil {
			if started && !terminal && run.claim != nil {
				s.returned[execution] = *run.claim
			}
			if terminal {
				delete(s.returned, execution)
			}
			delete(s.calls, execution)
		}
		close(s.changed)
		s.changed = make(chan struct{})
		s.mu.Unlock()
	}()
	row, err := loadExecution(runCtx, s.store, execution)
	if err != nil {
		return err
	}
	if row == nil {
		return fault(f.NotFound)
	}
	run.request = c.PreparationRequest{ExecutionID: execution, Launch: row.launch.Clone()}
	if err = preparingRecord(row, run.request, true); err != nil {
		return err
	}
	if err = s.start(runCtx, run.request, run); err != nil {
		if _, unknown := UnknownAttempt(err); unknown {
			run.unresolved = err
		}
		return err
	}
	started = true
	err = s.capture(runCtx, run.request, *run.claim)
	if _, unknown := UnknownAttempt(err); unknown {
		run.unresolved = err
		return err
	}
	// The source/Agent call and its entire transaction actually returned. This
	// bounded checkpoint belongs to the same Run and remains counted in Drain.
	checkpoint, end := context.WithTimeout(context.WithoutCancel(runCtx), 3*time.Second)
	finishErr := s.finish(checkpoint, run.request, *run.claim, false)
	end()
	if _, unknown := UnknownAttempt(finishErr); unknown {
		run.unresolved = finishErr
	}
	terminal = finishErr == nil
	if finishErr != nil {
		return errors.Join(finishErr, portError(err))
	}
	return portError(err)
}

// ResolveUnknown is a convergence operation for this driver's original
// returned call. It never starts a new claim or calls a capture provider. A
// not-observed attempt remains unknown: it is not proof that the old COMMIT
// cannot arrive. A known original attempt can be marked terminal only after
// its original physical call returned, under the same complete lock union.
// Successful resolution retires ownership only, not preparation or Execution.
func (d *PreparationDriver) ResolveUnknown(ctx context.Context, execution i.ExecutionID) error {
	if ctx == nil || execution.Validate() != nil {
		return invalid()
	}
	if d == nil || d.state == nil {
		return fault(f.DependencyUnbound)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	s := d.state
	s.mu.Lock()
	run := s.calls[execution]
	if run == nil || !run.returned || run.resolving || run.unresolved == nil || run.claim == nil {
		s.mu.Unlock()
		return fault(f.InvalidState)
	}
	run.resolving = true
	s.mu.Unlock()
	err := s.finish(ctx, run.request, *run.claim, true)
	s.mu.Lock()
	run.resolving = false
	if err == nil {
		delete(s.calls, execution)
		delete(s.returned, execution)
	} else {
		var known *f.Fault
		if errors.As(err, &known) && known.Code == f.CommitUnknown {
			err = run.unresolved
		}
	}
	close(s.changed)
	s.changed = make(chan struct{})
	s.mu.Unlock()
	return err
}
func (d *PreparationDriver) Stop() {
	if d == nil || d.state == nil {
		return
	}
	s := d.state
	s.mu.Lock()
	s.stopped = true
	for _, run := range s.calls {
		run.cancel()
	}
	close(s.changed)
	s.changed = make(chan struct{})
	s.mu.Unlock()
}
func (d *PreparationDriver) Drain(ctx context.Context) error {
	if ctx == nil {
		return invalid()
	}
	if d == nil || d.state == nil {
		return fault(f.DependencyUnbound)
	}
	s := d.state
	for {
		s.mu.Lock()
		done := s.stopped && len(s.calls) == 0
		changed := s.changed
		s.mu.Unlock()
		if done {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-changed:
		}
	}
}
func (d *PreparationDriver) Joined() bool {
	if d == nil || d.state == nil {
		return false
	}
	s := d.state
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.stopped && len(s.calls) == 0
}

func (s *preparationState) capture(ctx context.Context, request c.PreparationRequest, claim preparationClaim) (err error) {
	if err = ctx.Err(); err != nil {
		return err
	}
	provider := s.task
	if request.Launch.Trigger.Kind == "meeting" {
		provider = s.meeting
	}
	if nilPort(provider) {
		return fault(f.DependencyUnbound)
	}
	plan, err := provider.DiscoverCapture(ctx, request.ExecutionID, request.Launch.Clone())
	if err != nil {
		return portError(err)
	}
	if nilPort(plan) {
		return fault(f.DependencyUnavailable)
	}
	locks, err := preparationLocks(request)
	if err != nil {
		return err
	}
	locks, err = oc.NormalizeLocks(append(locks, plan.RequiredLocks()...))
	if err != nil {
		return portError(err)
	}
	// No panic from a provider becomes permission or a successful capture.
	defer func() {
		if recover() != nil {
			err = unavailable(nil)
		}
	}()
	result := s.store.WithinTx(ctx, claim.cause(), func(ctx context.Context, tx f.Tx) error {
		if err := s.store.AcquireAll(ctx, tx, locks); err != nil {
			return portError(err)
		}
		proof := &preparationWitness{owner: s.authority.state, driver: s, tx: tx, request: request.Clone(), claim: claim, locks: append([]f.LockRequest(nil), locks...)}
		if err := proof.require(ctx, tx, request); err != nil {
			return err
		}
		project, err := s.projects.RequirePreparingProjectInTx(ctx, tx, request.Launch.ProjectID)
		if err != nil {
			return portError(err)
		}
		if project.Validate() != nil || project.ID != request.Launch.ProjectID || project.Lifecycle != "active" {
			return fault(f.InvalidState)
		}
		proof.project = clonePreparationProject(project)
		proof.projectChecked = true
		captureCtx := context.WithValue(ctx, preparationWitnessKey{}, proof)
		input, err := provider.CaptureInputInTx(captureCtx, tx, request.ExecutionID, request.Launch.Clone(), plan)
		if err != nil {
			return portError(err)
		}
		if err = ctx.Err(); err != nil {
			return err
		}
		if input.Validate() != nil || input.Ref().ProviderType != request.Launch.Trigger.Kind {
			return fault(f.InvalidState)
		}
		proof.input = input
		proof.sourceCaptured = true
		actor, err := i.NewAgentRun(request.Launch.ProjectID, request.Launch.AgentID, request.ExecutionID)
		if err != nil {
			return err
		}
		configRequest := ac.ExecutionConfigurationRequest{Actor: actor, ProjectID: request.Launch.ProjectID, AgentID: request.Launch.AgentID, ExecutionID: request.ExecutionID, Stage: ac.ExecutionConfigurationCapture}
		config, err := s.agents.ReadExecutionConfigurationInTx(captureCtx, tx, configRequest)
		if err != nil {
			return portError(err)
		}
		if err = ctx.Err(); err != nil {
			return err
		}
		fields := config.Fields()
		if config.Validate() != nil || fields.Core.ID != request.Launch.AgentID || fields.Core.ProjectID != request.Launch.ProjectID || fields.Core.Lifecycle != ac.AgentActive {
			return fault(f.InvalidState)
		}
		// The complete D01 atomic metadata/ref/lease capture ports are not yet
		// implemented. Do not serialize these two partial values or let this
		// transaction commit provider references on their own.
		return fault(f.DependencyUnbound)
	})
	return commitError(result)
}
