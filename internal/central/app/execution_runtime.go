package app

import (
	"context"

	"github.com/LunaDeerTech/agenteam/internal/central/account"
	"github.com/LunaDeerTech/agenteam/internal/central/agent"
	"github.com/LunaDeerTech/agenteam/internal/central/agentloop"
	"github.com/LunaDeerTech/agenteam/internal/central/config"
	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/execution"
	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	gc "github.com/LunaDeerTech/agenteam/internal/central/governance/approval/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/model"
	"github.com/LunaDeerTech/agenteam/internal/central/model/adapter"
	"github.com/LunaDeerTech/agenteam/internal/central/mount"
	"github.com/LunaDeerTech/agenteam/internal/central/outbox"
	pv "github.com/LunaDeerTech/agenteam/internal/central/projectvariable"
	runnerservice "github.com/LunaDeerTech/agenteam/internal/central/runner/service"
	"github.com/LunaDeerTech/agenteam/internal/central/scheduler"
	"github.com/LunaDeerTech/agenteam/internal/central/secret"
	"github.com/LunaDeerTech/agenteam/internal/central/skill"
	"github.com/LunaDeerTech/agenteam/internal/central/tool/registry"
	"github.com/LunaDeerTech/agenteam/internal/central/work"
	wc "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
)

type executionRuntimeEventSet struct {
	lifecycle ec.DirectTextEvents
	claim     wc.SchedulerClaimEvents
	busy      wc.TaskBusyCompensationEvents
	failure   wc.TaskLaunchFailureEvents
}

func defineExecutionRuntimeEvents(catalog *event.Catalog) (executionRuntimeEventSet, error) {
	var out executionRuntimeEventSet
	var err error
	if out.lifecycle, err = ec.RegisterDirectTextEvents(catalog); err != nil {
		return out, err
	}
	if out.claim, err = wc.RegisterSchedulerClaimEvents(catalog); err != nil {
		return out, err
	}
	if out.busy, err = wc.RegisterTaskBusyCompensationEvents(catalog); err != nil {
		return out, err
	}
	if out.failure, err = wc.RegisterTaskLaunchFailureEvents(catalog); err != nil {
		return out, err
	}
	return out, nil
}

// Construction has no admitted domain calls. Each created owner is registered
// immediately, so a later constructor failure cannot orphan an earlier owner.
// The caller installs b in accountAssembly BEFORE invoking this function.
func constructExecutionRuntime(b *executionRuntimeAssembly, cfg config.Config, db database,
	accounts *account.Authority, projects *projectUsageAssembly, skills *skill.Authority,
	workAuthority *work.Authority, a *executionRuntimeAuthorities, objects *objectAssembly,
	models *model.Service, secrets *secret.Service, outbound *outboundRuntime,
	journal *outbox.Service, events executionRuntimeEventSet, registries ...*registry.Registry) error {
	if b == nil {
		return f.NewFault(f.DependencyUnbound, f.NotStarted)
	}
	defer b.constructionDone()
	options, enabled := cfg.ExecutionRuntime()
	policy, bound := cfg.SchedulerLaunchRetryPolicy()
	store, ok := db.(execution.Store)
	if !enabled || options.Validate() != nil || !bound || !ok || runtimeInformationNil(store) || b == nil || a == nil || projects == nil || projects.reader == nil || projects.lifecycle == nil || objects == nil || objects.guard == nil || models == nil || secrets == nil || outbound == nil || journal == nil {
		return f.NewFault(f.DependencyUnbound, f.NotStarted)
	}
	providers, err := createAgentRuntimeProviders(db, projects.projects, accounts, skills, a, registries...)
	if err != nil {
		return err
	}
	budget := adapter.NewBudget()
	b.add(executionRuntimeWork{stop: budget.StopAdmission, drain: budget.Drain, joined: budget.Joined})
	wire, err := adapter.NewOpenAIChat(adapter.Transport{Policy: outbound.policy, Trust: cfg.OutboundTrust(), Resolver: outbound.resolver, ProxyRoutes: outbound.routes}, budget)
	if err != nil {
		return err
	}
	runtime, err := model.NewRuntimeWithAgentRetry(store, a.model, model.RuntimeDependencies{
		Usage: projects.reader, SecretReader: secrets, SecretUsage: secrets, Adapter: wire,
	}, options.AgentRetryTiming())
	if err != nil {
		return err
	}
	b.add(executionRuntimeWork{stop: runtime.StopAdmission, drain: runtime.Drain, joined: runtime.Joined})
	b.initialize = runtime.Initialize
	loop, err := agentloop.NewDirectTextController(runtime)
	if err != nil {
		return err
	}
	b.add(runtimeWork(loop))
	currentAgents, err := agent.NewSchedulerCurrent(a.agents, a.pending)
	if err != nil {
		return err
	}
	occupancy, err := execution.NewWorkOccupancy(store)
	if err != nil {
		return err
	}
	claims, err := work.NewSchedulerClaim(store, work.SchedulerClaimDependencies{
		Authority: workAuthority, Scheduler: a.pending, Agents: currentAgents,
		Pending: a.pending, Occupancy: occupancy, Events: journal, ClaimEvents: events.claim,
	})
	if err != nil {
		return err
	}
	b.add(runtimeWork(claims))
	busyWriter, err := work.NewTaskBusyCompensation(store, work.TaskBusyCompensationDependencies{
		Authority: workAuthority, Scheduler: a.pending, Pending: a.pending,
		Events: journal, CompensationEvents: events.busy,
	})
	if err != nil {
		return err
	}
	b.add(runtimeWork(busyWriter))
	failureWriter, err := work.NewTaskLaunchFailure(store, work.TaskLaunchFailureDependencies{
		Authority: workAuthority, Scheduler: a.pending, Pending: a.pending,
		Events: journal, FailureEvents: events.failure,
	})
	if err != nil {
		return err
	}
	b.add(runtimeWork(failureWriter))
	relaunchWriter, err := work.NewTaskRelaunch(store, work.TaskRelaunchDependencies{
		Authority: workAuthority, Scheduler: a.pending, Agents: currentAgents, Pending: a.pending, Occupancy: occupancy,
	})
	if err != nil {
		return err
	}
	b.add(runtimeWork(relaunchWriter))
	tasks, err := work.NewSchedulerTaskReader(store, workAuthority)
	if err != nil {
		return err
	}
	launchSource, err := work.NewTaskLaunchProvider(store, workAuthority, a.pending)
	if err != nil {
		return err
	}
	observations, err := execution.NewDispatchObservation(store)
	if err != nil {
		return err
	}
	taskCapture, err := work.NewTaskTrigger(store, workAuthority, a.execution)
	if err != nil {
		return err
	}
	capture := model.NewExecutionCapture
	if options.ToolLoop() {
		capture = model.NewToolCallingExecutionCapture
	}
	modelCapture, err := capture(models, a.execution)
	if err != nil {
		return err
	}
	environmentAuthority, err := pv.NewEnvironmentAuthority(projects.variables, a.execution)
	if err != nil {
		return err
	}
	leases, err := secret.NewProjectVariableLeases(secrets, environmentAuthority)
	if err != nil {
		return err
	}
	environment, err := pv.NewExecutionEnvironment(environmentAuthority, leases)
	if err != nil {
		return err
	}
	runnerMetadata, err := runnerservice.NewExecutionMetadataDirectory(store, a.execution)
	if err != nil {
		return err
	}
	mounts, err := mount.NewExecutionCapture(store, a.execution, runnerMetadata)
	if err != nil {
		return err
	}
	processes := outboxProcessAuthority{process: objects.process, guard: objects.guard}
	contextBuilder, err := execution.NewContextBuilder(work.TaskContextBuilder{})
	if err != nil {
		return err
	}
	modelRetirement, err := model.NewExecutionLeaseRetirement(a.model, secrets)
	if err != nil {
		return err
	}
	environmentRetirementAuthority, err := pv.NewEnvironmentRetirementAuthority(projects.variables, a.execution)
	if err != nil {
		return err
	}
	leaseRetirement, err := secret.NewProjectVariableLeaseRetirement(secrets, environmentRetirementAuthority)
	if err != nil {
		return err
	}
	environmentRetirement, err := pv.NewEnvironmentRetirement(environmentRetirementAuthority, leaseRetirement)
	if err != nil {
		return err
	}
	mountUses, err := mount.NewExecutionUse(store, a.execution)
	if err != nil {
		return err
	}
	callDependencies := execution.ProjectCallDependencies{
		Read:    b.taskReadDependencies,
		Service: execution.Dependencies{Authority: a.execution, Agents: providers.configuration, Task: launchSource},
		Preparation: execution.PreparationDependencies{
			Agents: providers.configuration, Projects: projects.projects, Processes: processes,
			Task: taskCapture, Skills: providers.skills, Tools: providers.tools, Models: modelCapture,
			Environment: environment, Mounts: mounts,
		},
		Direct: execution.DirectTextDependencies{
			Context: contextBuilder, Loop: loop, Processes: processes, Projects: projects.projects,
			Agents: providers.configuration, Skills: providers.roundSkills, Events: journal, Lifecycle: events.lifecycle,
			Models: modelRetirement, Environment: environmentRetirement, Mounts: mountUses,
		},
		Associated: options.AssociatedExecutor(),
	}
	if options.ToolLoop() {
		if len(registries) != 1 || registries[0] == nil || b.runnerDevice == nil {
			return f.NewFault(f.DependencyUnbound, f.NotStarted)
		}
		currentEnvironment, err := pv.NewCurrentProcessEnvironment(store, a.execution)
		if err != nil {
			return err
		}
		callDependencies.MultiRound = &execution.MultiRoundDependencies{
			Models: runtime, Tools: registries[0], Guard: objects.guard,
			CurrentEnvironment: currentEnvironment,
			BindTools:          b.runnerToolFactory(store, objects, providers.configuration, registries[0]),
		}
		if b.approvalAuthority != nil {
			callDependencies.MultiRound.BindApproval = func(owner *execution.ToolCallDriver) (gc.Gate, error) { return b.bindApproval(store, owner) }
		}
	}
	owned, err := createOwnedExecutionRuntime(store, callDependencies, projects.lifecycle, projects.variables, secrets, objects)
	// A final retirement-binding failure may still return its original pair.
	// Acquire that owner before handling the error, using the same root budget.
	if owned != nil && owned.pair != nil {
		b.add(runtimeWork(owned.pair))
		b.executionCalls, b.executionRetirement = owned.pair, owned.retirement
	}
	if err != nil {
		return err
	}
	if owned == nil || owned.pair == nil || owned.retirement == nil {
		return f.NewFault(f.DependencyUnbound, f.NotStarted)
	}
	managed, err := createOwnedRunnerManagedRuntime(store, projects.lifecycle, b.agentCalls, owned.pair, objects)
	if managed != nil && managed.calls != nil {
		b.add(runtimeWork(managed.calls))
		b.runnerManagedCalls, b.runnerManagedCleanup = managed.calls, managed.cleanup
	}
	if err != nil {
		return err
	}
	if managed == nil || managed.calls == nil || managed.cleanup == nil {
		return f.NewFault(f.DependencyUnbound, f.NotStarted)
	}
	scheduled, err := createOwnedSchedulerRuntime(store, a.pending, scheduler.ProjectCallDependencies{
		Coordinator:   scheduler.CoordinatorDependencies{Projects: projects.projects, Claims: claims, Executions: observations, Capacity: observations},
		Handoff:       scheduler.LaunchHandoffDependencies{Executions: owned.pair.Service(), Observations: observations},
		Busy:          scheduler.BusyCompensatorDependencies{Projects: projects.projects, Work: busyWriter},
		Failure:       scheduler.LaunchFailureFinalizerDependencies{Projects: projects.projects, Work: failureWriter},
		RelaunchTasks: relaunchWriter, Tasks: tasks, Occupancy: occupancy,
		RelaunchSkipCount: options.ProjectRunners().RelaunchSkipCount, RetryPolicy: &policy,
	}, projects.lifecycle, projects.projects, objects, owned.pair.Associated(), options.ProjectRunners())
	if scheduled != nil && scheduled.pair != nil {
		b.add(runtimeWork(scheduled.pair))
		b.schedulerCalls = scheduled.pair
	}
	if scheduled != nil && scheduled.manager != nil {
		b.add(runtimeWork(scheduled.manager))
	}
	if err != nil {
		return err
	}
	if scheduled == nil || scheduled.pair == nil || scheduled.manager == nil {
		return f.NewFault(f.DependencyUnbound, f.NotStarted)
	}
	b.executor, b.manager = owned.pair.Associated(), scheduled.manager
	return nil
}

type runtimeDomainOwner interface {
	Stop()
	Drain(context.Context) error
	Joined() bool
}

func runtimeWork(owner runtimeDomainOwner) executionRuntimeWork {
	return executionRuntimeWork{stop: owner.Stop, drain: owner.Drain, joined: owner.Joined}
}
