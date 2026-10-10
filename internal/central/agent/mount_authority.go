package agent

import (
	"context"
	"slices"

	c "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
)

type mountOwnerPlan struct {
	issuer  *authorityState
	request c.MountConfigurationChange
	mapping f.Digest
}

func (p mountOwnerPlan) RequiredLocks() []f.LockRequest {
	return ownerLocks(p.request.Actor, p.request.ProjectID, p.request.AgentID, p.request.Command)
}
func sameMountChange(a, b c.MountConfigurationChange) bool {
	return a.Actor.Equal(b.Actor) && a.ProjectID == b.ProjectID && a.AgentID == b.AgentID && a.Command.Canonical() == b.Command.Canonical() && a.PlanRevision == b.PlanRevision && sameValue(a.ExpectedOwnerVersion, b.ExpectedOwnerVersion) && a.ResultOwnerVersion == b.ResultOwnerVersion && slices.Equal(a.Before, b.Before) && slices.Equal(a.After, b.After)
}
func matchesMounts(r *commandRecord, req c.MountConfigurationChange) bool {
	if r == nil || r.Revision != req.PlanRevision || !sameValue(r.Expected, req.ExpectedOwnerVersion) || r.After.Fields().Core.Version != req.ResultOwnerVersion || !slices.Equal(r.After.Fields().AllowedMountIDs, req.After) {
		return false
	}
	if r.Before == nil {
		return r.Name == c.CreateAgentCommand && len(req.Before) == 0
	}
	return r.Name == c.UpdateAgentCommand && slices.Equal(r.Before.Fields().AllowedMountIDs, req.Before)
}
func (a *Authority) DiscoverMountReferenceOwner(ctx context.Context, req c.MountConfigurationChange) (c.MountReferenceOwnerPlan, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}
	var mapping f.Digest
	err := a.discoverPlanned(ctx, req.Actor, req.ProjectID, req.AgentID, req.Command, func(r *commandRecord) error {
		if !matchesMounts(r, req) {
			return fault(f.VersionConflict)
		}
		var err error
		mapping, err = recordMapping(r)
		return err
	})
	if err != nil {
		return nil, err
	}
	return mountOwnerPlan{a.state, req.Clone(), mapping}, nil
}
func (a *Authority) CheckMountReferenceOwnerAppliedInTx(ctx context.Context, tx f.Tx, req c.MountConfigurationChange, plan c.MountReferenceOwnerPlan) error {
	if err := req.Validate(); err != nil {
		return err
	}
	if a == nil || a.state == nil {
		return fault(f.DependencyUnbound)
	}
	p, ok := plan.(mountOwnerPlan)
	if !ok || p.issuer != a.state || !sameMountChange(p.request, req) {
		return fault(f.Forbidden)
	}
	r, err := a.checkApplied(ctx, tx, req.Actor, req.ProjectID, req.AgentID, req.Command, p.mapping)
	if err != nil {
		return err
	}
	if !matchesMounts(r, req) {
		return fault(f.Forbidden)
	}
	return nil
}

var _ c.MountReferenceOwnerAuthority = (*Authority)(nil)
