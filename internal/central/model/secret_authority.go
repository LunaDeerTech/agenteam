package model

import (
	"context"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

func (s *Service) prepareSecret(ctx context.Context, p *preparedCommand) error {
	old, newRef := "", ""
	if p.plan.BeforeProvider != nil {
		old = p.plan.BeforeProvider.Input.CredentialID
	}
	if p.plan.AfterProvider != nil {
		newRef = p.plan.AfterProvider.Input.CredentialID
	}
	if old == newRef {
		return nil
	}
	for _, v := range []struct {
		id     string
		retain bool
	}{{newRef, true}, {old, false}} {
		if v.id == "" {
			continue
		}
		cid, e := f.ParseID[sc.Credential](v.id)
		if e != nil {
			return unavailable(e)
		}
		ref, e := sc.NewCredentialRef(cid, id.SystemScope())
		if e != nil {
			return e
		}
		action := sc.ReleaseReferenceUsage
		if v.retain {
			action = sc.RetainReferenceUsage
		}
		r := sc.UsageRequest{Actor: p.actor, Ref: ref, Purpose: sc.Model, ReferenceOwner: p.plan.Resource, Action: action, Retain: v.retain}
		d, e := s.state().deps.Secret.DiscoverUsage(ctx, r)
		if e != nil {
			return portError(e)
		}
		binding, e := sc.UsageBinding(r)
		if e != nil || d.Validate() != nil || d.Binding() != binding {
			return unavailable(e)
		}
		p.usages = append(p.usages, r)
		p.usagePlans = append(p.usagePlans, d)
		p.locks = append(p.locks, d.RequiredLocks()...)
	}
	return nil
}
func referenceMatches(p *preparedCommand, r sc.UsageRequest) bool {
	if r.Validate() != nil || r.Purpose != sc.Model || !r.Ref.Details().Scope.Equal(id.SystemScope()) || r.ReferenceOwner != p.plan.Resource {
		return false
	}
	old, next := "", ""
	if p.plan.BeforeProvider != nil {
		old = p.plan.BeforeProvider.Input.CredentialID
	}
	if p.plan.AfterProvider != nil {
		next = p.plan.AfterProvider.Input.CredentialID
	}
	if old == next {
		return false
	}
	return r.Action == sc.RetainReferenceUsage && r.Retain && next != "" && r.Ref.Details().ID.String() == next || r.Action == sc.ReleaseReferenceUsage && !r.Retain && old != "" && r.Ref.Details().ID.String() == old
}
func (a *Authority) DiscoverUsage(ctx context.Context, r sc.UsageRequest) (sc.UsageDependencies, error) {
	if r.Validate() != nil {
		return sc.UsageDependencies{}, fault(f.InvalidArgument)
	}
	if r.Purpose != sc.Model || r.Action != sc.RetainReferenceUsage && r.Action != sc.ReleaseReferenceUsage {
		return sc.UsageDependencies{}, fault(f.DependencyUnbound)
	}
	p, e := a.contextPlan(ctx, r.Actor)
	if e != nil {
		return sc.UsageDependencies{}, e
	}
	if !referenceMatches(p, r) {
		return sc.UsageDependencies{}, fault(f.Forbidden)
	}
	binding, e := sc.UsageBinding(r)
	if e != nil {
		return sc.UsageDependencies{}, e
	}
	raw, e := encoded(p.plan)
	if e != nil {
		return sc.UsageDependencies{}, e
	}
	return sc.NewUsageDependencies(a.state().usageIssuer, binding, hash(raw), p.locks)
}
func (a *Authority) ValidateUsageInTx(ctx context.Context, tx f.Tx, r sc.UsageRequest, d sc.UsageDependencies) error {
	p, e := a.contextPlan(ctx, r.Actor)
	if e != nil {
		return e
	}
	if !referenceMatches(p, r) {
		return fault(f.Forbidden)
	}
	binding, e := sc.UsageBinding(r)
	if e != nil {
		return e
	}
	raw, e := encoded(p.plan)
	if e != nil {
		return e
	}
	if !d.Matches(a.state().usageIssuer, binding, hash(raw)) {
		return fault(f.Forbidden)
	}
	if e = a.state().store.RequireHeldLocks(ctx, tx, d.RequiredLocks()); e != nil {
		return portError(e)
	}
	return a.validatePreparedFact(ctx, tx, p)
}
func (a *Authority) CheckReferenceInTx(ctx context.Context, tx f.Tx, actor id.Actor, ref sc.CredentialRef, purpose sc.Purpose, owner string, retain bool) error {
	if purpose != sc.Model {
		return fault(f.DependencyUnbound)
	}
	action := sc.ReleaseReferenceUsage
	if retain {
		action = sc.RetainReferenceUsage
	}
	r := sc.UsageRequest{Actor: actor, Ref: ref, Purpose: purpose, ReferenceOwner: owner, Action: action, Retain: retain}
	if r.Validate() != nil {
		return fault(f.InvalidArgument)
	}
	// Secret's existing planned-authority adapter translates ResourceBusy into
	// SECRET_PREPARATION_REQUIRED. No legacy path may mutate Model references.
	return fault(f.ResourceBusy)
}
func (a *Authority) AuthorizeLeaseInTx(context.Context, f.Tx, id.Actor, sc.CredentialRef, sc.CredentialLeaseOwner, sc.LeaseAction) (sc.UseGrant, error) {
	return sc.UseGrant{}, fault(f.DependencyUnbound)
}

var _ sc.UsageAuthority = (*Authority)(nil)
var _ sc.UsagePlanner = (*Authority)(nil)
