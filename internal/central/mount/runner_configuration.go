package mount

import (
	"context"
	"slices"

	ac "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/mount/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	rc "github.com/LunaDeerTech/agenteam/internal/central/runner/contract"
)

// NewRunnerMountConfiguration adds explicit Agent configuration declarations
// to the existing provider. The Runner port authorizes Project configuration,
// not system administration, physical ensure or an Execution dispatch.
func NewRunnerMountConfiguration(store Store, projects pc.ProjectAuthority, owner ac.MountReferenceOwnerAuthority, runners rc.ConfigurationCandidates) (*Configuration, error) {
	if nilPort(runners) {
		return nil, fault(f.DependencyUnbound)
	}
	c, err := NewConfiguration(store, projects, owner)
	if err != nil {
		return nil, err
	}
	c.state.runners = runners
	return c, nil
}

func declarationRunnerIDs(declarations []ac.RunnerMountDeclaration) []rc.RunnerID {
	ids := make([]rc.RunnerID, 0, len(declarations))
	for _, d := range declarations {
		ids = append(ids, d.RunnerID)
	}
	slices.SortFunc(ids, func(a, b rc.RunnerID) int {
		if a.String() < b.String() {
			return -1
		}
		if a == b {
			return 0
		}
		return 1
	})
	return slices.Compact(ids)
}
func bindingRunnerIDs(bindings []ac.RunnerMountBinding) []rc.RunnerID {
	declarations := make([]ac.RunnerMountDeclaration, 0, len(bindings))
	for _, b := range bindings {
		declarations = append(declarations, ac.RunnerMountDeclaration{RunnerID: b.RunnerID, Workspace: b.Workspace})
	}
	return declarationRunnerIDs(declarations)
}
func (s *configurationState) runnerLocks(ids []rc.RunnerID) ([]f.LockRequest, error) {
	if nilPort(s.runners) {
		return nil, fault(f.DependencyUnbound)
	}
	locks, err := s.runners.ConfigurationCandidateLocks(slices.Clone(ids))
	if err != nil {
		return nil, portError(err)
	}
	if len(locks) > 512 {
		return nil, fault(f.InvalidArgument)
	}
	return oc.NormalizeLocks(locks)
}
func (s *configurationState) requireRunners(ctx context.Context, tx f.Tx, actor i.Actor, project i.ProjectID, ids []rc.RunnerID) error {
	if nilPort(s.runners) {
		return fault(f.DependencyUnbound)
	}
	facts, err := s.runners.RequireConfigurationCandidatesInTx(ctx, tx, actor, project, slices.Clone(ids))
	if err != nil {
		return portError(err)
	}
	if facts == nil || len(facts) != len(ids) {
		return fault(f.DependencyUnavailable)
	}
	for n, row := range facts {
		if row.Validate() != nil || row.ID != ids[n] {
			return fault(f.DependencyUnavailable)
		}
	}
	// Online/offline/incompatible is descriptive metadata, not authority to
	// retain or declare a durable Runner reference.
	return portError(ctx.Err())
}

func (c *Configuration) RunnerMountPlanningLocks(r ac.RunnerMountPlanRequest) ([]f.LockRequest, error) {
	if c == nil || c.state == nil {
		return nil, fault(f.DependencyUnbound)
	}
	// The caller has not loaded Before yet. Its complete preimage is checked
	// by PlanRunnerMountsInTx, after this entire lock union has been acquired.
	if r.Actor.Validate() != nil || r.Actor.Details().Kind != i.Human || r.ProjectID.Validate() != nil || r.AgentID.Validate() != nil || r.Command.Validate() != nil || r.Command.Namespace() != "project" || !slices.Equal(r.Command.OwnerIDs(), []string{r.ProjectID.String()}) || ac.ValidateRunnerMountDeclarations(r.Declarations) != nil {
		return nil, fault(f.InvalidArgument)
	}
	switch r.Command.Command() {
	case "agent.create":
		if r.ExpectedOwnerVersion != nil {
			return nil, fault(f.InvalidArgument)
		}
	case "agent.update":
		if r.ExpectedOwnerVersion == nil || r.ExpectedOwnerVersion.Validate() != nil {
			return nil, fault(f.InvalidArgument)
		}
	default:
		return nil, fault(f.InvalidArgument)
	}
	runners, err := c.state.runnerLocks(declarationRunnerIDs(r.Declarations))
	if err != nil {
		return nil, err
	}
	command, _ := f.CommandLock(r.Command)
	user, _ := f.UserLock(r.Actor.Details().UserID)
	project, _ := f.ProjectLock(r.ProjectID.String())
	agent, _ := f.AgentLock(r.AgentID.String())
	locks := append([]f.LockRequest{{Key: command, Mode: f.Exclusive}, {Key: user, Mode: f.Exclusive}, {Key: project, Mode: f.Shared}, {Key: agent, Mode: f.Exclusive}}, runners...)
	if len(locks) > 512 {
		return nil, fault(f.InvalidArgument)
	}
	return oc.NormalizeLocks(locks)
}

func (c *Configuration) PlanRunnerMountsInTx(ctx context.Context, tx f.Tx, r ac.RunnerMountPlanRequest) ([]ac.RunnerMountBinding, error) {
	if err := r.Validate(); err != nil {
		return nil, err
	}
	r = r.Clone()
	locks, err := c.RunnerMountPlanningLocks(r)
	if err != nil {
		return nil, err
	}
	x, err := c.state.executor(ctx, tx, locks)
	if err != nil {
		return nil, err
	}
	change := ac.MountConfigurationChange{Actor: r.Actor, ProjectID: r.ProjectID, AgentID: r.AgentID, ExpectedOwnerVersion: r.ExpectedOwnerVersion, Before: []i.MountID{}}
	if r.Before != nil {
		change.Before = r.Before.Fields().AllowedMountIDs
	}
	if err = c.state.currentOwner(ctx, tx, change, true); err != nil {
		return nil, err
	}
	before, err := loadReferences(ctx, x, r.ProjectID, r.AgentID)
	if err != nil {
		return nil, err
	}
	if err = checkBefore(change, before); err != nil {
		return nil, err
	}
	if err = c.state.requireRunners(ctx, tx, r.Actor, r.ProjectID, declarationRunnerIDs(r.Declarations)); err != nil {
		return nil, err
	}
	out := make([]ac.RunnerMountBinding, 0, len(r.Declarations))
	for _, declaration := range r.Declarations {
		d, err := definitionForTuple(ctx, x, r.ProjectID, r.AgentID, declaration.RunnerID, declaration.Workspace)
		if err != nil {
			return nil, err
		}
		binding := ac.RunnerMountBinding{RunnerID: declaration.RunnerID, Workspace: declaration.Workspace}
		if d != nil {
			binding.MountID = d.ID
			mapping, err := loadWorkspaceMapping(ctx, x, d.ID)
			if err != nil {
				return nil, err
			}
			if mapping != nil {
				if !mappingMatches(mapping, *d) {
					return nil, fault(f.InvalidState)
				}
				binding = mapping.binding
			}
		} else {
			binding.MountID, err = f.NewID[i.Mount]()
			if err != nil {
				return nil, portError(err)
			}
		}
		if binding.WorkspaceID.Validate() != nil {
			binding.WorkspaceID, err = f.NewID[mc.Workspace]()
			if err != nil {
				return nil, portError(err)
			}
		}
		if _, err = loadBindingState(ctx, x, r.ProjectID, r.AgentID, binding); err != nil {
			return nil, err
		}
		out = append(out, binding)
	}
	if _, err = ac.RunnerMountIDs(out); err != nil {
		return nil, err
	}
	if err = ctx.Err(); err != nil {
		return nil, portError(err)
	}
	// No Mount writes occurred. The Agent owner must persist this exact result
	// in its original planned command before it can become a final write plan.
	return out, nil
}

func loadRunnerBindingStates(ctx context.Context, x postgres.SQLExecutor, r ac.MountConfigurationChange) ([]runnerBindingState, error) {
	states := make([]runnerBindingState, 0, len(*r.RunnerMounts))
	for _, binding := range *r.RunnerMounts {
		s, err := loadBindingState(ctx, x, r.ProjectID, r.AgentID, binding)
		if err != nil {
			return nil, err
		}
		states = append(states, s)
	}
	return states, nil
}

// The Agent caller passes its canonical version and complete sorted allowlist
// while holding the same live Tx/Agent lock. Every reverse fact must agree;
// missing mappings, legacy ambiguity and partial sets fail as a whole.
func (c *Configuration) RunnerMountBindingsInTx(ctx context.Context, tx f.Tx, actor i.Actor, p i.ProjectID, a i.AgentID, version f.Version, ids []i.MountID) ([]ac.RunnerMountConfigurationItem, error) {
	if c == nil || c.state == nil {
		return nil, fault(f.DependencyUnbound)
	}
	if actor.Validate() != nil || actor.Details().Kind != i.Human || p.Validate() != nil || a.Validate() != nil || version.Validate() != nil || ids == nil || len(ids) > ac.MaxCapabilityReferences {
		return nil, fault(f.InvalidArgument)
	}
	ids = slices.Clone(ids)
	for n, id := range ids {
		if id.Validate() != nil || n > 0 && ids[n-1].String() >= id.String() {
			return nil, fault(f.InvalidArgument)
		}
	}
	user, _ := f.UserLock(actor.Details().UserID)
	project, _ := f.ProjectLock(p.String())
	agent, _ := f.AgentLock(a.String())
	locks, err := oc.NormalizeLocks([]f.LockRequest{{Key: user, Mode: f.Shared}, {Key: project, Mode: f.Shared}, {Key: agent, Mode: f.Shared}})
	if err != nil {
		return nil, portError(err)
	}
	x, err := c.state.executor(ctx, tx, locks)
	if err != nil {
		return nil, err
	}
	if err = c.state.currentOwner(ctx, tx, ac.MountConfigurationChange{Actor: actor, ProjectID: p}, false); err != nil {
		return nil, err
	}
	head, err := loadReferences(ctx, x, p, a)
	if err != nil {
		return nil, err
	}
	if !head.exists || head.version != version || !slices.Equal(head.ids, ids) {
		return nil, fault(f.VersionConflict)
	}
	out := make([]ac.RunnerMountConfigurationItem, 0, len(ids))
	for _, id := range ids {
		d, err := loadDefinition(ctx, x, p, a, id)
		if err != nil {
			return nil, err
		}
		unique, err := definitionForTuple(ctx, x, p, a, d.Runner, d.Workspace)
		if err != nil {
			return nil, err
		}
		if unique == nil || unique.ID != id {
			return nil, fault(f.InvalidState)
		}
		mapping, err := loadWorkspaceMapping(ctx, x, id)
		if err != nil {
			return nil, err
		}
		if !mappingMatches(mapping, d) {
			return nil, fault(f.DependencyUnbound)
		}
		out = append(out, ac.RunnerMountConfigurationItem{MountID: id, RunnerID: d.Runner, Workspace: d.Workspace, Name: d.Name})
	}
	if err = ctx.Err(); err != nil {
		return nil, portError(err)
	}
	return out, nil
}

var _ ac.RunnerMountPlanner = (*Configuration)(nil)
var _ ac.RunnerMountBindingsReader = (*Configuration)(nil)
