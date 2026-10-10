package agent

import (
	"context"

	c "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
)

// ModelOwnerAuthority projects only the two Model roles. Its private issuer
// is not a second Agent writer and cannot create a canonical witness.
type ModelOwnerAuthority struct {
	owner  *Authority
	issuer mc.PlanIssuer
}

func NewModelOwnerAuthority(owner *Authority) (*ModelOwnerAuthority, error) {
	if owner == nil || owner.state == nil {
		return nil, fault(f.DependencyUnbound)
	}
	return &ModelOwnerAuthority{owner, mc.NewPlanIssuer()}, nil
}
func configModels(v c.AgentConfig) mc.AgentModelSelection {
	core := v.Fields().Core
	return mc.AgentModelSelection{ModelID: core.ModelRef, ReasoningEffort: core.ReasoningEffort, ApprovalModelID: core.ApprovalModelRef}
}
func matchesModels(r *commandRecord, req mc.AgentReferenceChange) bool {
	if r == nil || r.Revision != req.PlanRevision || !sameValue(r.Expected, req.ExpectedOwnerVersion) || r.After.Fields().Core.Version != req.ResultOwnerVersion || !configModels(r.After).Equal(req.After) {
		return false
	}
	if r.Before == nil {
		return r.Name == c.CreateAgentCommand && req.Before == nil
	}
	return r.Name == c.UpdateAgentCommand && req.Before != nil && configModels(*r.Before).Equal(*req.Before)
}
func (a *ModelOwnerAuthority) DiscoverAgentReferenceOwner(ctx context.Context, req mc.AgentReferenceChange) (mc.AgentReferenceOwnerPlan, error) {
	if err := req.Validate(); err != nil {
		return mc.AgentReferenceOwnerPlan{}, err
	}
	if a == nil || a.owner == nil || a.owner.state == nil {
		return mc.AgentReferenceOwnerPlan{}, fault(f.DependencyUnbound)
	}
	var mapping f.Digest
	err := a.owner.discoverPlanned(ctx, req.Actor, req.ProjectID, req.AgentID, req.Command, func(r *commandRecord) error {
		if !matchesModels(r, req) {
			return fault(f.VersionConflict)
		}
		var err error
		mapping, err = recordMapping(r)
		return err
	})
	if err != nil {
		return mc.AgentReferenceOwnerPlan{}, err
	}
	return mc.NewAgentReferenceOwnerPlan(a.issuer, req, mapping, ownerLocks(req.Actor, req.ProjectID, req.AgentID, req.Command))
}
func (a *ModelOwnerAuthority) CheckAgentReferenceOwnerAppliedInTx(ctx context.Context, tx f.Tx, req mc.AgentReferenceChange, plan mc.AgentReferenceOwnerPlan) error {
	if err := req.Validate(); err != nil {
		return err
	}
	if a == nil || a.owner == nil || a.owner.state == nil {
		return fault(f.DependencyUnbound)
	}
	binding, err := mc.AgentReferenceBinding(req)
	if err != nil {
		return err
	}
	mapping := plan.Details().Mapping
	if !plan.Matches(a.issuer, binding, mapping) {
		return fault(f.Forbidden)
	}
	r, err := a.owner.checkApplied(ctx, tx, req.Actor, req.ProjectID, req.AgentID, req.Command, mapping)
	if err != nil {
		return err
	}
	if !matchesModels(r, req) {
		return fault(f.Forbidden)
	}
	return nil
}

var _ mc.AgentReferenceOwnerAuthority = (*ModelOwnerAuthority)(nil)
