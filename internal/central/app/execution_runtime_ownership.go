package app

import (
	"github.com/LunaDeerTech/agenteam/internal/central/execution"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/mount"
	"github.com/LunaDeerTech/agenteam/internal/central/project"
	pv "github.com/LunaDeerTech/agenteam/internal/central/projectvariable"
	"github.com/LunaDeerTech/agenteam/internal/central/secret"
	"github.com/LunaDeerTech/agenteam/internal/central/skill"
	"github.com/LunaDeerTech/agenteam/internal/central/tool/registry"
)

type ownedExecutionRuntime struct {
	pair       *execution.ProjectCallOwnership
	retirement *execution.ProjectExecutionRetirement
}

// Construction follows the original Object initialization: the ProcessGuard
// must already be bound. No service is published until the complete retirement
// graph enables provenance on this same pair, before its first Launch.
//
// A nonnil result always transfers its pair to the caller, including when the
// final retirement binding fails. The installed runtime must record that owner
// before handling err, so its original shutdown also owns this partial graph.
func createOwnedExecutionRuntime(store execution.Store, deps execution.ProjectCallDependencies, life *project.LifecycleAuthority, variables *pv.Authority, secrets *secret.Service, objects *objectAssembly) (*ownedExecutionRuntime, error) {
	if runtimeInformationNil(store) || life == nil || deps.Service.Authority == nil || variables == nil || secrets == nil || objects == nil || objects.guard == nil || objects.process.Validate() != nil {
		return nil, f.NewFault(f.DependencyUnbound, f.NotStarted)
	}
	stop, err := project.NewExecutionProjectStopAuthority(life, store)
	if err != nil {
		return nil, err
	}
	cleanup, err := project.NewExecutionProjectRetirementAuthority(life, store)
	if err != nil {
		return nil, err
	}
	owner := deps.Service.Authority
	tools, err := registry.NewProjectExecutionRetirementReferences(store, owner)
	if err != nil {
		return nil, err
	}
	mounts, err := mount.NewProjectExecutionRetirementReferences(store, owner)
	if err != nil {
		return nil, err
	}
	variableAuthority, err := pv.NewProjectExecutionRetirementAuthority(variables, owner)
	if err != nil {
		return nil, err
	}
	leases, err := secret.NewProjectExecutionLeaseRetirement(secrets, variableAuthority)
	if err != nil {
		return nil, err
	}
	variableRefs, err := pv.NewProjectExecutionRetirementReferences(variableAuthority, leases)
	if err != nil {
		return nil, err
	}
	skills, err := skill.NewProjectExecutionRetirementReferences(store, owner)
	if err != nil {
		return nil, err
	}
	pair, err := execution.NewProjectCallOwnership(store, deps, objects.guard, objects.process, stop)
	if err != nil {
		return nil, err
	}
	result := &ownedExecutionRuntime{pair: pair}
	result.retirement, err = execution.NewProjectExecutionRetirement(pair, cleanup, tools, variableRefs, skills, mounts)
	return result, err
}
