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
	"github.com/LunaDeerTech/agenteam/internal/central/model"
	"github.com/LunaDeerTech/agenteam/internal/central/model/adapter"
	"github.com/LunaDeerTech/agenteam/internal/central/mount"
	"github.com/LunaDeerTech/agenteam/internal/central/outbox"
	"github.com/LunaDeerTech/agenteam/internal/central/project"
	pv "github.com/LunaDeerTech/agenteam/internal/central/projectvariable"
	"github.com/LunaDeerTech/agenteam/internal/central/scheduler"
	"github.com/LunaDeerTech/agenteam/internal/central/secret"
	"github.com/LunaDeerTech/agenteam/internal/central/skill"
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
	journal *outbox.Service, events executionRuntimeEventSet) error {
	if b == nil {
		return f.NewFault(f.DependencyUnbound, f.NotStarted)
	}
	defer b.constructionDone()
	options, enabled := cfg.ExecutionRuntime()
	policy, bound := cfg.SchedulerLaunchRetryPolicy()
	store, ok := db.(execution.Store)
	if !enabled || options.Validate() != nil || !bound || !ok || runtimeInformationNil(store) || b == nil || a == nil || projects == nil || projects.reader == nil || objects == nil || objects.guard == nil || models == nil || secrets == nil || outbound == nil || journal == nil {
		return f.NewFault(f.DependencyUnbound, f.NotStarted)
	}
	providers, err := createAgentRuntimeProviders(db, projects.projects, accounts, skills, a)
	if err != nil {
		return err
	}
	budget := adapter.NewBudget()
	b.add(executionRuntimeWork{stop: budget.StopAdmission, drain: budget.Drain, joined: budget.Joined})
	wire, err := adapter.NewOpenAIChat(adapter.Transport{Policy: outbound.policy, Trust: cfg.OutboundTrust()}, budget)
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
	launch, err := execution.New(store, execution.Dependencies{Authority: a.execution, Agents: providers.configuration, Task: launchSource})
	if err != nil {
		return err
	}
	b.add(runtimeWork(launch))
	observations, err := execution.NewDispatchObservation(store)
	if err != nil {
		return err
	}
	handoff, err := scheduler.NewLaunchHandoffWithRetry(a.pending, scheduler.LaunchHandoffDependencies{Executions: launch, Observations: observations}, projects.projects)
	if err != nil {
		return err
	}
	b.add(runtimeWork(handoff))
	busy, err := scheduler.NewBusyCompensator(a.pending, scheduler.BusyCompensatorDependencies{Projects: projects.projects, Work: busyWriter})
	if err != nil {
		return err
	}
	b.add(runtimeWork(busy))
	finalizer, err := scheduler.NewLaunchFailureFinalizer(a.pending, scheduler.LaunchFailureFinalizerDependencies{Projects: projects.projects, Work: failureWriter})
	if err != nil {
		return err
	}
	b.add(runtimeWork(finalizer))
	visitor, err := scheduler.NewPendingVisitorWithFailure(a.pending, handoff, busy, finalizer)
	if err != nil {
		return err
	}
	b.add(runtimeWork(visitor))
	coordinator, err := scheduler.NewCoordinatorWithRetryPolicy(a.pending, scheduler.CoordinatorDependencies{
		Projects: projects.projects, Claims: claims, Executions: observations, Capacity: observations,
	}, policy)
	if err != nil {
		return err
	}
	b.add(runtimeWork(coordinator))
	relaunch, err := scheduler.NewRelaunchCoordinator(coordinator, relaunchWriter, tasks, occupancy, options.ProjectRunners().RelaunchSkipCount)
	if err != nil {
		return err
	}
	b.add(runtimeWork(relaunch))
	taskCapture, err := work.NewTaskTrigger(store, workAuthority, a.execution)
	if err != nil {
		return err
	}
	modelCapture, err := model.NewExecutionCapture(models, a.execution)
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
	mounts, err := mount.NewExecutionCapture(store, a.execution)
	if err != nil {
		return err
	}
	processes := outboxProcessAuthority{process: objects.process, guard: objects.guard}
	preparation, err := execution.NewPreparationDriver(store, a.execution, execution.PreparationDependencies{
		Agents: providers.configuration, Projects: projects.projects, Processes: processes,
		Task: taskCapture, Skills: providers.skills, Tools: providers.tools, Models: modelCapture,
		Environment: environment, Mounts: mounts,
	})
	if err != nil {
		return err
	}
	b.add(runtimeWork(preparation))
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
	direct, err := execution.NewDirectTextDriver(store, a.execution, execution.DirectTextDependencies{
		Context: contextBuilder, Loop: loop, Processes: processes, Projects: projects.projects,
		Agents: providers.configuration, Skills: providers.roundSkills, Events: journal, Lifecycle: events.lifecycle,
		Models: modelRetirement, Environment: environmentRetirement,
	})
	if err != nil {
		return err
	}
	b.add(runtimeWork(direct))
	executor, err := execution.NewAssociatedExecutor(preparation, direct, options.AssociatedExecutor())
	if err != nil {
		return err
	}
	b.add(runtimeWork(executor))
	discovery, err := project.NewSchedulerProjects(projects.projects)
	if err != nil {
		return err
	}
	manager, err := scheduler.NewProjectRunners(scheduler.ProjectRunnersDependencies{
		Projects: discovery, Coordinator: coordinator, Visitor: visitor, Tasks: tasks, Relaunch: relaunch, Executions: executor,
	}, options.ProjectRunners())
	if err != nil {
		return err
	}
	b.add(runtimeWork(manager))
	b.executor, b.manager = executor, manager
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
