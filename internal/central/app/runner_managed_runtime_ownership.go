package app

import (
	"github.com/LunaDeerTech/agenteam/internal/central/agent"
	"github.com/LunaDeerTech/agenteam/internal/central/execution"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/mount"
	"github.com/LunaDeerTech/agenteam/internal/central/project"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

type ownedRunnerManagedRuntime struct {
	calls   *mount.ProjectLifecycleOwnership
	cleanup pc.ProjectLifecycleParticipant
}

// The default graph binds this view only after its original Execution pair
// exists. Agent configuration keeps the same formal Runner directory and Mount
// provider. No System Runner, credential, directory or configuration is changed.
// A partial result transfers its control owner before the caller handles err.
func createOwnedRunnerManagedRuntime(store mount.Store, life *project.LifecycleAuthority, agents *agent.ProjectCallOwnership, executions *execution.ProjectCallOwnership, objects *objectAssembly) (*ownedRunnerManagedRuntime, error) {
	if runtimeInformationNil(store) || life == nil || agents == nil || executions == nil || objects == nil || objects.guard == nil || objects.process.Validate() != nil {
		return nil, f.NewFault(f.DependencyUnbound, f.NotStarted)
	}
	stop, err := project.NewRunnerManagedStopAuthority(life, store)
	if err != nil {
		return nil, err
	}
	cleanup, err := project.NewRunnerManagedProjectCleanupAuthority(life, store)
	if err != nil {
		return nil, err
	}
	agentCalls, err := agent.NewRunnerManagedProjectCalls(agents, store, objects.guard, objects.process, stop, cleanup)
	if err != nil {
		return nil, err
	}
	executionCalls, err := execution.NewRunnerManagedProjectCalls(executions, store, objects.guard, objects.process, stop, cleanup)
	if err != nil {
		return nil, err
	}
	calls, err := mount.NewProjectLifecycleOwnership(store, agentCalls, executionCalls, objects.guard, objects.process, stop)
	if err != nil {
		return nil, err
	}
	result := &ownedRunnerManagedRuntime{calls: calls}
	result.cleanup, err = mount.NewProjectCleanupParticipant(calls, cleanup)
	return result, err
}
