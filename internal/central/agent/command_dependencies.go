package agent

import (
	"context"
	"slices"

	c "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	vc "github.com/LunaDeerTech/agenteam/internal/central/projectvariable/contract"
)

type commandDependencies struct {
	toolRequest   c.ToolConfigurationRequest
	tools         c.ToolConfigurationPlan
	modelChange   mc.AgentReferenceChange
	models        mc.AgentReferencePlan
	toolChange    c.ToolReferenceChange
	toolRefs      c.ToolReferencePlan
	mountChange   c.MountConfigurationChange
	mounts        c.MountConfigurationPlan
	secretRequest vc.SecretDirectoryRequest
	secrets       vc.SecretDirectoryPlan
	secretChange  vc.SecretReferenceChange
	secretRefs    vc.SecretReferencePlan
	skillRequest  c.SkillsInitializationRequest
	skills        c.SkillsInitializationPlan
	event         event.Event
	append        oc.AppendPlan
	locks         []f.LockRequest
}

func toolRequest(in commandInput, r *commandRecord) c.ToolConfigurationRequest {
	ids := r.After.Fields().AllowedToolIDs
	if in.create != nil {
		ids = in.create.Fields().AllowedToolIDs
	}
	return c.ToolConfigurationRequest{Actor: in.actor, ProjectID: r.Project, AgentID: r.Target, Command: r.identity(), RequestedToolIDs: ids, InstallSkillEnabled: r.InstallSkillEnabled}.Clone()
}

func (s *Service) discoverDependencies(ctx context.Context, in commandInput, r *commandRecord, tools c.ToolConfigurationPlan) (*commandDependencies, error) {
	if r == nil || r.validate() != nil || nilPort(tools) {
		return nil, unavailable(nil)
	}
	if !slices.Equal(tools.ResolvedToolIDs(), r.After.Fields().AllowedToolIDs) {
		return nil, fault(f.VersionConflict)
	}
	a := r.After.Fields()
	b := c.AgentConfigFields{AllowedToolIDs: []i.ToolID{}, AllowedMountIDs: []i.MountID{}, AllowedSecretVariableIDs: []i.ProjectVariableID{}}
	if r.Before != nil {
		b = r.Before.Fields()
	}
	d := &commandDependencies{toolRequest: toolRequest(in, r), tools: tools}
	d.modelChange = mc.AgentReferenceChange{Actor: in.actor, ProjectID: r.Project, AgentID: r.Target, Command: r.identity(), PlanRevision: r.Revision, ExpectedOwnerVersion: r.Expected, ResultOwnerVersion: a.Core.Version, After: configModels(r.After)}
	if r.Before != nil {
		before := configModels(*r.Before)
		d.modelChange.Before = &before
	}
	d.toolChange = c.ToolReferenceChange{Actor: in.actor, ProjectID: r.Project, AgentID: r.Target, Command: r.identity(), PlanRevision: r.Revision, ExpectedOwnerVersion: r.Expected, ResultOwnerVersion: a.Core.Version, Before: b.AllowedToolIDs, After: a.AllowedToolIDs}
	d.mountChange = c.MountConfigurationChange{Actor: in.actor, ProjectID: r.Project, AgentID: r.Target, Command: r.identity(), PlanRevision: r.Revision, ExpectedOwnerVersion: r.Expected, ResultOwnerVersion: a.Core.Version, Before: b.AllowedMountIDs, After: a.AllowedMountIDs}
	d.secretRequest = vc.SecretDirectoryRequest{Actor: in.actor, ProjectID: r.Project, Command: r.identity(), IDs: a.AllowedSecretVariableIDs}
	op := vc.SecretReferenceCreate
	if r.Before != nil {
		op = vc.SecretReferenceUpdate
	}
	d.secretChange = vc.SecretReferenceChange{Actor: in.actor, ProjectID: r.Project, AgentID: r.Target, Command: r.identity(), Operation: op, ExpectedOwnerVersion: r.Expected, ResultOwnerVersion: a.Core.Version, Before: b.AllowedSecretVariableIDs, After: a.AllowedSecretVariableIDs}
	var err error
	d.models, err = s.state.deps.Models.DiscoverAgentReferences(ctx, d.modelChange.Clone())
	if err != nil {
		return nil, portError(err)
	}
	if d.models.Validate() != nil {
		return nil, unavailable(nil)
	}
	d.toolRefs, err = s.state.deps.ToolReferences.DiscoverToolReferences(ctx, d.toolChange.Clone())
	if err != nil {
		return nil, portError(err)
	}
	if nilPort(d.toolRefs) {
		return nil, unavailable(nil)
	}
	d.mounts, err = s.state.deps.Mounts.DiscoverMountConfiguration(ctx, d.mountChange.Clone())
	if err != nil {
		return nil, portError(err)
	}
	if nilPort(d.mounts) {
		return nil, unavailable(nil)
	}
	d.secrets, err = s.state.deps.Secrets.DiscoverSecretVariables(ctx, d.secretRequest.Clone())
	if err != nil {
		return nil, portError(err)
	}
	if d.secrets.Validate() != nil {
		return nil, unavailable(nil)
	}
	d.secretRefs, err = s.state.deps.SecretReferences.DiscoverSecretReferences(ctx, d.secretChange.Clone())
	if err != nil {
		return nil, portError(err)
	}
	if d.secretRefs.Validate() != nil {
		return nil, unavailable(nil)
	}
	sets := [][]f.LockRequest{in.locks(), tools.RequiredLocks(), d.models.RequiredLocks(), d.toolRefs.RequiredLocks(), d.mounts.RequiredLocks(), d.secrets.RequiredLocks(), d.secretRefs.RequiredLocks()}
	if in.create != nil {
		d.skillRequest = c.SkillsInitializationRequest{Actor: in.actor, ProjectID: r.Project, AgentID: r.Target, Command: r.identity(), PlanRevision: r.Revision, AddSkillsEnabled: *r.AddSkillsEnabled}
		d.skills, err = s.state.deps.Skills.DiscoverNewAgentInitialization(ctx, d.skillRequest)
		if err != nil {
			return nil, portError(err)
		}
		if nilPort(d.skills) {
			return nil, unavailable(nil)
		}
		sets = append(sets, d.skills.RequiredLocks())
	}
	if len(r.ChangedFields) > 0 {
		d.event, err = eventForCommand(s.state.deps.EventAuthority.events, r)
		if err != nil {
			return nil, err
		}
		d.append, err = s.state.deps.Events.PrepareAppend(ctx, in.actor, d.event)
		if err != nil {
			return nil, portError(err)
		}
		plannedEvent := d.append.Details().Event
		if plannedEvent.Validate() != nil || !sameValue(d.event.Summary(), plannedEvent.Summary()) {
			return nil, unavailable(nil)
		}
		sets = append(sets, d.append.Locks())
	}
	// Count the raw union, including duplicates, before normalization. Registry
	// rank-one and every resource gate are acquired together in the final Tx.
	var locks []f.LockRequest
	for _, set := range sets {
		if len(set) > 512-len(locks) {
			return nil, fault(f.ResourceBusy)
		}
		locks = append(locks, set...)
	}
	d.locks, err = oc.NormalizeLocks(locks)
	if err != nil {
		return nil, portError(err)
	}
	return d, nil
}

func (s *Service) requireDependencies(ctx context.Context, tx f.Tx, d *commandDependencies) error {
	if err := s.state.deps.Tools.RequireConfigurationToolsInTx(ctx, tx, d.toolRequest.Clone(), d.tools); err != nil {
		return portError(err)
	}
	if err := s.state.deps.Mounts.RequireMountConfigurationInTx(ctx, tx, d.mountChange.Clone(), d.mounts); err != nil {
		return portError(err)
	}
	facts, err := s.state.deps.Secrets.RequireSecretVariablesInTx(ctx, tx, d.secretRequest.Clone(), d.secrets)
	if err != nil {
		return portError(err)
	}
	if facts.Validate() != nil {
		return unavailable(nil)
	}
	entries := facts.Entries()
	if len(entries) != len(d.secretRequest.IDs) {
		return unavailable(nil)
	}
	for n, entry := range entries {
		if entry.ID != d.secretRequest.IDs[n] {
			return unavailable(nil)
		}
		switch entry.Status {
		case vc.SecretDirectoryValid:
			if entry.Variable == nil || entry.Variable.Validate() != nil || entry.Variable.Fields().ID != entry.ID || entry.Variable.Fields().ProjectID != d.secretRequest.ProjectID {
				return unavailable(nil)
			}
		case vc.SecretDirectoryRemoved, vc.SecretDirectoryNotInScope:
			return fault(f.NotFound)
		default:
			return unavailable(nil)
		}
	}
	return nil
}

func (s *Service) applyDependencies(ctx context.Context, tx f.Tx, in commandInput, d *commandDependencies) error {
	// ctx is the original private canonical-writer context, never rebuilt from
	// public facts. Every provider owns its tables and rechecks its own plan.
	if err := s.state.deps.Models.ApplyAgentReferencesInTx(ctx, tx, d.modelChange.Clone(), d.models); err != nil {
		return portError(err)
	}
	if err := s.state.deps.ToolReferences.ApplyToolReferencesInTx(ctx, tx, d.toolChange.Clone(), d.toolRefs); err != nil {
		return portError(err)
	}
	if err := s.state.deps.SecretReferences.ApplySecretReferencesInTx(ctx, tx, d.secretChange.Clone(), d.secretRefs); err != nil {
		return portError(err)
	}
	if err := s.state.deps.Mounts.ApplyMountConfigurationInTx(ctx, tx, d.mountChange.Clone(), d.mounts); err != nil {
		return portError(err)
	}
	if in.create != nil {
		if err := s.state.deps.Skills.InitializeNewAgentInTx(ctx, tx, d.skillRequest, d.skills); err != nil {
			return portError(err)
		}
	}
	return nil
}
