package app

import (
	"github.com/LunaDeerTech/agenteam/internal/central/execution"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/project"
	"github.com/LunaDeerTech/agenteam/internal/central/scheduler"
)

type ownedSchedulerRuntime struct {
	pair    *scheduler.ProjectCallOwnership
	manager *scheduler.ProjectRunners
}

// The original PendingAuthority already belongs to the installed Work graph.
// Six fixed owners and each manager-created ProjectRunner share one call state;
// the manager still owns its separate Run and actual discovery/worker returns.
func createOwnedSchedulerRuntime(store scheduler.Store, pending *scheduler.PendingAuthority, deps scheduler.ProjectCallDependencies,
	life *project.LifecycleAuthority, projects *project.Authority, objects *objectAssembly,
	executor *execution.AssociatedExecutor, options scheduler.ProjectRunnersOptions) (*ownedSchedulerRuntime, error) {
	if runtimeInformationNil(store) || pending == nil || projects == nil || objects == nil || objects.guard == nil || objects.process.Validate() != nil || executor == nil || deps.Coordinator.Projects != projects {
		return nil, f.NewFault(f.DependencyUnbound, f.NotStarted)
	}
	current, err := objects.guard.CurrentProcess()
	if err != nil {
		return nil, err
	}
	if current != objects.process {
		return nil, f.NewFault(f.DependencyUnbound, f.NotStarted)
	}
	authority, err := project.NewSchedulerProjectStopAuthority(life, store)
	if err != nil {
		return nil, err
	}
	discovery, err := project.NewSchedulerProjects(projects)
	if err != nil {
		return nil, err
	}
	pair, err := scheduler.NewProjectCallOwnership(pending, deps, objects.guard, objects.process, authority)
	if err != nil {
		return nil, err
	}
	out := &ownedSchedulerRuntime{pair: pair}
	out.manager, err = scheduler.NewProjectRunners(scheduler.ProjectRunnersDependencies{
		Projects: discovery, Coordinator: pair.Coordinator(), Visitor: pair.Visitor(),
		Tasks: deps.Tasks, Relaunch: pair.Relaunch(), Executions: executor,
	}, options)
	// Preserve a created pair on failure so the root takes its original Stop /
	// Drain ownership before handling the error. No background budget is added.
	return out, err
}
