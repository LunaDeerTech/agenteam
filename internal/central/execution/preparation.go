package execution

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"time"

	ac "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/execution/prompt"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	mountc "github.com/LunaDeerTech/agenteam/internal/central/mount/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	pvc "github.com/LunaDeerTech/agenteam/internal/central/projectvariable/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/skill/contract"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
)

type PreparationDependencies struct {
	Agents      ac.ExecutionConfiguration
	Projects    c.PreparationProjectGate
	Processes   oc.ProcessAuthority
	Task        c.TriggerCaptureProvider
	Meeting     c.TriggerCaptureProvider
	Skills      sc.InitialBindingsProvider
	Tools       tc.ExecutionTools
	Models      mc.ExecutionModelCaptureProvider
	Environment pvc.ExecutionEnvironment
	Mounts      mountc.ExecutionMountCaptureProvider
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
	skills        sc.InitialBindingsProvider
	tools         tc.ExecutionTools
	models        mc.ExecutionModelCaptureProvider
	environment   pvc.ExecutionEnvironment
	mounts        mountc.ExecutionMountCaptureProvider
	mu            sync.Mutex
	stopped       bool
	calls         map[i.ExecutionID]*preparationCall
	returned      map[i.ExecutionID]preparationClaim
	changed       chan struct{}
}
type preparationCall struct {
	ctx                 context.Context
	cancel              context.CancelFunc
	request             c.PreparationRequest
	claim               *preparationClaim
	returned, resolving bool
	unresolved          error
	modelUnknown        error
	modelScope          *mc.ExecutionModelCaptureScope
	input               *preparationInputRecord
	replayed            bool
}

func NewPreparationDriver(store Store, authority *Authority, deps PreparationDependencies) (*PreparationDriver, error) {
	if nilPort(store) || !reflect.TypeOf(store).Comparable() || authority == nil || authority.state == nil || authority.state.store != store || nilPort(deps.Agents) || nilPort(deps.Projects) || nilPort(deps.Processes) {
		return nil, fault(f.DependencyUnbound)
	}
	if deps.Skills != nil && nilPort(deps.Skills) || deps.Tools != nil && nilPort(deps.Tools) || deps.Models != nil && nilPort(deps.Models) || deps.Environment != nil && nilPort(deps.Environment) || deps.Mounts != nil && nilPort(deps.Mounts) {
		return nil, fault(f.DependencyUnbound)
	}
	process := deps.Processes.CurrentProcess()
	if process.Validate() != nil {
		return nil, fault(f.DependencyUnbound)
	}
	return &PreparationDriver{&preparationState{store: store, authority: authority, agents: deps.Agents, projects: deps.Projects, processes: deps.Processes, process: process, task: deps.Task, meeting: deps.Meeting, skills: deps.Skills, tools: deps.Tools, models: deps.Models, environment: deps.Environment, mounts: deps.Mounts, calls: map[i.ExecutionID]*preparationCall{}, returned: map[i.ExecutionID]preparationClaim{}, changed: make(chan struct{})}}, nil
}

// Run reads the stored original Launch, commits created -> preparing and a
// fenced attempt, then atomically captures all installed providers and their
// references/leases with one immutable input. A missing provider rolls the
// entire capture back. The supported profile requires AGENTS.md injection off
// and a real empty Mount head. This does not seal a Snapshot, enter running or
// publish Started. A committed input is reused without recapturing sources.
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
	run := &preparationCall{ctx: runCtx, cancel: cancel}
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
	captured, err := s.capturedInput(runCtx, run.request)
	if err != nil || captured {
		return err
	}
	err = s.start(runCtx, run.request, run)
	if run.replayed {
		// The original transaction only observed a prior immutable input.
		// No preparation writer or attempt was started by this replay.
		return err
	}
	if err != nil {
		if _, unknown := UnknownAttempt(err); unknown {
			run.unresolved = err
		}
		return err
	}
	started = true
	err = s.capture(runCtx, run.request, *run.claim)
	if preparationUnknown(err) {
		run.unresolved = err
		return err
	}
	// The source/Agent call and its entire transaction actually returned. This
	// bounded checkpoint belongs to the same Run and remains counted in Drain.
	checkpoint, end := context.WithTimeout(context.WithoutCancel(runCtx), 3*time.Second)
	finishErr := s.finish(checkpoint, run.request, *run.claim, false, run.input)
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
	err := s.observeModelDiscovery(ctx, run)
	if err == nil {
		err = s.finish(ctx, run.request, *run.claim, true, run.input)
	}
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
	// Every provider call, including discovery, belongs to this original Run.
	// A provider panic cannot publish a plan or skip attempt retirement.
	defer func() {
		if recover() != nil {
			err = unavailable(nil)
		}
		if err != nil && !preparationUnknown(err) {
			s.mu.Lock()
			if run := s.calls[request.ExecutionID]; run != nil && run.claim != nil && *run.claim == claim {
				run.input = nil
			}
			s.mu.Unlock()
		}
	}()
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
	skillRequest := sc.SkillCaptureRequest{ProjectID: request.Launch.ProjectID, AgentID: request.Launch.AgentID, ExecutionID: request.ExecutionID}
	var skillPlan sc.InitialBindingsPlan
	if !nilPort(s.skills) {
		err = s.discoverResource(ctx, request, claim, "skill", func(discoveryCtx context.Context) error {
			var discoverErr error
			skillPlan, discoverErr = s.skills.DiscoverInitialBindings(discoveryCtx, skillRequest)
			return discoverErr
		})
		if err != nil {
			return portError(err)
		}
		if nilPort(skillPlan) {
			return fault(f.DependencyUnavailable)
		}
	}
	toolRequest := tc.ExecutionToolCaptureRequest{ProjectID: request.Launch.ProjectID, AgentID: request.Launch.AgentID, ExecutionID: request.ExecutionID}
	var toolPlan tc.ExecutionToolPlan
	if !nilPort(s.tools) {
		err = s.discoverResource(ctx, request, claim, "tool", func(discoveryCtx context.Context) error {
			var discoverErr error
			toolPlan, discoverErr = s.tools.DiscoverExecutionTools(discoveryCtx, toolRequest)
			return discoverErr
		})
		if err != nil {
			return portError(err)
		}
		if nilPort(toolPlan) {
			return fault(f.DependencyUnavailable)
		}
	}
	modelRequest := mc.ExecutionModelCaptureRequest{ProjectID: request.Launch.ProjectID, AgentID: request.Launch.AgentID, ExecutionID: request.ExecutionID}
	var modelPlan mc.ExecutionModelCapturePlan
	if !nilPort(s.models) {
		err = s.discoverResource(ctx, request, claim, "model", func(discoveryCtx context.Context) error {
			var discoverErr error
			modelPlan, discoverErr = s.models.DiscoverExecutionModel(discoveryCtx, modelRequest)
			return discoverErr
		})
		if err != nil {
			if preparationUnknown(err) {
				s.mu.Lock()
				if run := s.calls[request.ExecutionID]; run != nil && run.claim != nil && *run.claim == claim {
					run.modelUnknown = err
				}
				s.mu.Unlock()
				return err
			}
			return portError(err)
		}
		if nilPort(modelPlan) {
			return fault(f.DependencyUnavailable)
		}
	}
	environmentRequest := pvc.EnvironmentCaptureRequest{ProjectID: request.Launch.ProjectID, AgentID: request.Launch.AgentID, ExecutionID: request.ExecutionID}
	var environmentPlan pvc.EnvironmentCapturePlan
	if !nilPort(s.environment) {
		err = s.discoverResource(ctx, request, claim, "environment", func(discoveryCtx context.Context) error {
			var discoverErr error
			environmentPlan, discoverErr = s.environment.DiscoverExecutionEnvironment(discoveryCtx, environmentRequest)
			return discoverErr
		})
		if err != nil {
			return portError(err)
		}
		if nilPort(environmentPlan) {
			return fault(f.DependencyUnavailable)
		}
	}
	mountRequest := mountc.ExecutionMountCaptureRequest{ProjectID: request.Launch.ProjectID, AgentID: request.Launch.AgentID, ExecutionID: request.ExecutionID}
	var mountPlan mountc.ExecutionMountCapturePlan
	if !nilPort(s.mounts) {
		err = s.discoverResource(ctx, request, claim, "mount", func(discoveryCtx context.Context) error {
			var discoverErr error
			mountPlan, discoverErr = s.mounts.DiscoverExecutionMounts(discoveryCtx, mountRequest)
			return discoverErr
		})
		if err != nil {
			return portError(err)
		}
		if nilPort(mountPlan) {
			return fault(f.DependencyUnavailable)
		}
	}
	locks, err := preparationLocks(request)
	if err != nil {
		return err
	}
	locks = append(locks, plan.RequiredLocks()...)
	if skillPlan != nil {
		locks = append(locks, skillPlan.RequiredLocks()...)
	}
	if toolPlan != nil {
		locks = append(locks, toolPlan.RequiredLocks()...)
	}
	if modelPlan != nil {
		locks = append(locks, modelPlan.RequiredLocks()...)
	}
	if environmentPlan != nil {
		locks = append(locks, environmentPlan.RequiredLocks()...)
	}
	if mountPlan != nil {
		locks = append(locks, mountPlan.RequiredLocks()...)
	}
	locks, err = oc.NormalizeLocks(locks)
	if err != nil {
		return portError(err)
	}
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
		if fields.Core.InjectAgentsMD {
			// No AGENTS.md filesystem capture provider is installed. Never
			// represent a configured injection as an empty captured document.
			return fault(f.DependencyUnbound)
		}
		proof.agent, proof.agentCaptured = config.Clone(), true
		proof.resourcesOpen.Store(true)
		defer proof.resourcesOpen.Store(false)
		if nilPort(s.skills) {
			return fault(f.DependencyUnbound)
		}
		bindings, err := s.skills.ResolveInitialBindingsInTx(captureCtx, tx, skillRequest, skillPlan)
		if err != nil {
			return portError(err)
		}
		if err = ctx.Err(); err != nil {
			return err
		}
		if bindings.Validate() != nil || bindings.Request != skillRequest {
			return fault(f.InvalidState)
		}
		if nilPort(s.tools) {
			return fault(f.DependencyUnbound)
		}
		tools, err := s.tools.ResolveExecutionToolsInTx(captureCtx, tx, toolRequest, toolPlan)
		if err != nil {
			return portError(err)
		}
		if err = ctx.Err(); err != nil {
			return err
		}
		if tools == nil {
			return fault(f.InvalidState)
		}
		seen := make(map[i.ToolID]bool, len(tools))
		for _, tool := range tools {
			if tool.Validate() != nil || seen[tool.ToolID] {
				return fault(f.InvalidState)
			}
			seen[tool.ToolID] = true
		}
		if nilPort(s.models) {
			return fault(f.DependencyUnbound)
		}
		resolved, err := s.models.ResolveExecutionModelInTx(captureCtx, tx, modelRequest, modelPlan)
		if err != nil {
			return portError(err)
		}
		if err = ctx.Err(); err != nil {
			return err
		}
		if resolved.Validate() != nil || resolved.Consumer.Kind != mc.AgentConsumer || resolved.Consumer.Purpose != mc.AgentGeneration || resolved.Consumer.ProjectID != modelRequest.ProjectID || resolved.Consumer.AgentID == nil || *resolved.Consumer.AgentID != modelRequest.AgentID || resolved.Consumer.ExecutionID == nil || *resolved.Consumer.ExecutionID != modelRequest.ExecutionID || resolved.Snapshot.Identity.ModelID != fields.Core.ModelRef {
			return fault(f.InvalidState)
		}
		if nilPort(s.environment) {
			return fault(f.DependencyUnbound)
		}
		environment, err := s.environment.ResolveExecutionEnvironmentInTx(captureCtx, tx, environmentRequest, environmentPlan)
		if err != nil {
			return portError(err)
		}
		if err = ctx.Err(); err != nil {
			return err
		}
		if environment.Validate() != nil || environment.Fields().Request != environmentRequest || environment.Fields().AgentVersion != fields.Core.Version {
			return fault(f.InvalidState)
		}
		if nilPort(s.mounts) {
			return fault(f.DependencyUnbound)
		}
		mounts, err := s.mounts.CaptureExecutionMountsInTx(captureCtx, tx, mountRequest, mountPlan)
		if err != nil {
			return portError(err)
		}
		if err = ctx.Err(); err != nil {
			return err
		}
		if mounts.Validate() != nil || mounts.Request != mountRequest || mounts.AgentVersion != fields.Core.Version || len(fields.AllowedMountIDs) != 0 {
			return fault(f.InvalidState)
		}
		x, err := s.store.InTx(tx)
		if err != nil {
			return portError(err)
		}
		at, err := captureTime(ctx, x)
		if err != nil {
			return err
		}
		binding, err := preparationResourceBinding(request, claim, project)
		if err != nil {
			return err
		}
		complete, err := c.NewPreparationInput(c.PreparationInputFields{Request: request.Clone(), AttemptBinding: binding, CapturedAt: at, Project: clonePreparationProject(project), Agent: config.Clone(), Trigger: input, Model: resolved.Clone(), Tools: tools, Skills: bindings, Environment: environment.Clone(), Mounts: mounts.Clone(), PlatformPrompt: prompt.Current()})
		if err != nil {
			return portError(err)
		}
		return proof.saveInput(captureCtx, complete)
	})
	return commitError(result)
}
