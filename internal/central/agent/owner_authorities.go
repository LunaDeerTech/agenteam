package agent

import (
	"context"
	"slices"

	c "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	vc "github.com/LunaDeerTech/agenteam/internal/central/projectvariable/contract"
)

// The concrete plans and issuer identity stay in Agent. Implementing the
// public RequiredLocks method cannot manufacture an accepted owner plan.
type creationPlan struct {
	issuer  *authorityState
	request c.SkillsInitializationRequest
	mapping f.Digest
}

func (p creationPlan) RequiredLocks() []f.LockRequest {
	return ownerLocks(p.request.Actor, p.request.ProjectID, p.request.AgentID, p.request.Command)
}
func matchesCreation(r *commandRecord, req c.SkillsInitializationRequest) bool {
	return r != nil && r.Name == c.CreateAgentCommand && r.Before == nil &&
		r.Revision == req.PlanRevision && r.AddSkillsEnabled != nil && *r.AddSkillsEnabled == req.AddSkillsEnabled &&
		r.After.Fields().Core.Version == 1
}
func sameCreation(a, b c.SkillsInitializationRequest) bool {
	return a.Actor.Equal(b.Actor) && a.ProjectID == b.ProjectID && a.AgentID == b.AgentID &&
		a.Command.Canonical() == b.Command.Canonical() && a.PlanRevision == b.PlanRevision && a.AddSkillsEnabled == b.AddSkillsEnabled
}

func (a *Authority) DiscoverNewAgentCreation(ctx context.Context, req c.SkillsInitializationRequest) (c.NewAgentCreationPlan, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}
	var mapping f.Digest
	err := a.discoverPlanned(ctx, req.Actor, req.ProjectID, req.AgentID, req.Command, func(r *commandRecord) error {
		if !matchesCreation(r, req) {
			return fault(f.VersionConflict)
		}
		var err error
		mapping, err = recordMapping(r)
		return err
	})
	if err != nil {
		return nil, err
	}
	return creationPlan{a.state, req.Clone(), mapping}, nil
}

func (a *Authority) CheckNewAgentCreationAppliedInTx(ctx context.Context, tx f.Tx, req c.SkillsInitializationRequest, plan c.NewAgentCreationPlan) error {
	if err := req.Validate(); err != nil {
		return err
	}
	if a == nil || a.state == nil {
		return fault(f.DependencyUnbound)
	}
	p, ok := plan.(creationPlan)
	if !ok || p.issuer != a.state || !sameCreation(p.request, req) {
		return fault(f.Forbidden)
	}
	r, err := a.checkApplied(ctx, tx, req.Actor, req.ProjectID, req.AgentID, req.Command, p.mapping)
	if err != nil {
		return err
	}
	if !matchesCreation(r, req) {
		return fault(f.Forbidden)
	}
	return nil
}

type toolOwnerPlan struct {
	issuer  *authorityState
	request c.ToolReferenceChange
	mapping f.Digest
}

func (p toolOwnerPlan) RequiredLocks() []f.LockRequest {
	return ownerLocks(p.request.Actor, p.request.ProjectID, p.request.AgentID, p.request.Command)
}
func sameToolChange(a, b c.ToolReferenceChange) bool {
	return a.Actor.Equal(b.Actor) && a.ProjectID == b.ProjectID && a.AgentID == b.AgentID && a.Command.Canonical() == b.Command.Canonical() &&
		a.PlanRevision == b.PlanRevision && sameValue(a.ExpectedOwnerVersion, b.ExpectedOwnerVersion) && a.ResultOwnerVersion == b.ResultOwnerVersion &&
		slices.Equal(a.Before, b.Before) && slices.Equal(a.After, b.After)
}
func matchesTools(r *commandRecord, req c.ToolReferenceChange) bool {
	if r == nil || r.Revision != req.PlanRevision || !sameValue(r.Expected, req.ExpectedOwnerVersion) || r.After.Fields().Core.Version != req.ResultOwnerVersion || !slices.Equal(r.After.Fields().AllowedToolIDs, req.After) {
		return false
	}
	if r.Before == nil {
		return len(req.Before) == 0 && r.Name == c.CreateAgentCommand
	}
	return r.Name == c.UpdateAgentCommand && slices.Equal(r.Before.Fields().AllowedToolIDs, req.Before)
}
func (a *Authority) DiscoverToolReferenceOwner(ctx context.Context, req c.ToolReferenceChange) (c.ToolReferenceOwnerPlan, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}
	var mapping f.Digest
	err := a.discoverPlanned(ctx, req.Actor, req.ProjectID, req.AgentID, req.Command, func(r *commandRecord) error {
		if !matchesTools(r, req) {
			return fault(f.VersionConflict)
		}
		var err error
		mapping, err = recordMapping(r)
		return err
	})
	if err != nil {
		return nil, err
	}
	return toolOwnerPlan{a.state, req.Clone(), mapping}, nil
}
func (a *Authority) CheckToolReferenceOwnerAppliedInTx(ctx context.Context, tx f.Tx, req c.ToolReferenceChange, plan c.ToolReferenceOwnerPlan) error {
	if err := req.Validate(); err != nil {
		return err
	}
	if a == nil || a.state == nil {
		return fault(f.DependencyUnbound)
	}
	p, ok := plan.(toolOwnerPlan)
	if !ok || p.issuer != a.state || !sameToolChange(p.request, req) {
		return fault(f.Forbidden)
	}
	r, err := a.checkApplied(ctx, tx, req.Actor, req.ProjectID, req.AgentID, req.Command, p.mapping)
	if err != nil {
		return err
	}
	if !matchesTools(r, req) {
		return fault(f.Forbidden)
	}
	return nil
}

func matchesSecrets(r *commandRecord, req vc.SecretReferenceChange) bool {
	if r == nil || !sameValue(r.Expected, req.ExpectedOwnerVersion) || r.After.Fields().Core.Version != req.ResultOwnerVersion || !slices.Equal(r.After.Fields().AllowedSecretVariableIDs, req.After) {
		return false
	}
	if r.Before == nil {
		return r.Name == c.CreateAgentCommand && req.Operation == vc.SecretReferenceCreate && len(req.Before) == 0
	}
	return r.Name == c.UpdateAgentCommand && req.Operation == vc.SecretReferenceUpdate && slices.Equal(r.Before.Fields().AllowedSecretVariableIDs, req.Before)
}

// The Secret port uses its existing typed plan. Its issuer belongs to this
// exact Authority, and Mapping includes the original command ID/revision and
// entire canonical pre/postimage, not just the Secret subset.
func (a *Authority) Discover(ctx context.Context, req vc.SecretReferenceChange) (vc.SecretReferenceOwnerPlan, error) {
	if err := req.Validate(); err != nil {
		return vc.SecretReferenceOwnerPlan{}, err
	}
	var mapping f.Digest
	err := a.discoverPlanned(ctx, req.Actor, req.ProjectID, req.AgentID, req.Command, func(r *commandRecord) error {
		if !matchesSecrets(r, req) {
			return fault(f.VersionConflict)
		}
		var err error
		mapping, err = recordMapping(r)
		return err
	})
	if err != nil {
		return vc.SecretReferenceOwnerPlan{}, err
	}
	return vc.NewSecretReferenceOwnerPlan(a.state.secretIssuer, req, mapping, ownerLocks(req.Actor, req.ProjectID, req.AgentID, req.Command))
}
func (a *Authority) CheckAppliedInTx(ctx context.Context, tx f.Tx, req vc.SecretReferenceChange, plan vc.SecretReferenceOwnerPlan) error {
	if err := req.Validate(); err != nil {
		return err
	}
	if a == nil || a.state == nil {
		return fault(f.DependencyUnbound)
	}
	binding, err := vc.SecretReferenceBinding(req)
	if err != nil {
		return err
	}
	mapping := plan.Details().Mapping
	if !plan.Matches(a.state.secretIssuer, binding, mapping) {
		return fault(f.Forbidden)
	}
	r, err := a.checkApplied(ctx, tx, req.Actor, req.ProjectID, req.AgentID, req.Command, mapping)
	if err != nil {
		return err
	}
	if !matchesSecrets(r, req) {
		return fault(f.Forbidden)
	}
	return nil
}

var _ c.NewAgentCreationAuthority = (*Authority)(nil)
var _ c.ToolReferenceOwnerAuthority = (*Authority)(nil)
var _ vc.SecretReferenceOwnerAuthority = (*Authority)(nil)
