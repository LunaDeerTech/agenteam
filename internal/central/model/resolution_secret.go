package model

import (
	"context"
	"sync"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

type resolutionCandidateKey struct{}
type resolutionApplyKey struct{}

// A candidate only permits discovery. This separate single-Apply witness is
// initially unverified and can only be validated by the actual Secret callback.
type resolutionApply struct {
	mu               sync.Mutex
	candidate        *resolutionCandidate
	tx               f.Tx
	active, verified bool
}

func resolutionHasApply(ctx context.Context) bool {
	return ctx != nil && ctx.Value(resolutionApplyKey{}) != nil
}
func (a *Authority) resolutionUsage(c *resolutionCandidate) (sc.UsageRequest, error) {
	empty := sc.UsageRequest{}
	if a.state() == nil || a.state().auth.Resolution == nil || c == nil || c.authority != a {
		return empty, fault(f.DependencyUnbound)
	}
	snapshot, e := c.draft.Snapshot.snapshot()
	if e != nil {
		return empty, e
	}
	if snapshot.CredentialRef == nil {
		return empty, fault(f.InvalidArgument)
	}
	lease, e := f.ParseID[sc.Lease](c.draft.LeaseID)
	if e != nil {
		return empty, unavailable(e)
	}
	actor, e := a.state().auth.Resolution.SecretService.Actor(c.request.LeaseOwner.Details().ID, snapshot.CredentialRef.Details().Scope)
	if e != nil || actor.Details().ServiceName != id.SecretService {
		return empty, fault(f.DependencyUnbound)
	}
	r := sc.UsageRequest{Actor: actor, Ref: *snapshot.CredentialRef, Purpose: sc.Model, LeaseOwner: c.request.LeaseOwner, LeaseID: lease, Action: sc.AcquireLeaseUsage}
	if e = r.Validate(); e != nil {
		return empty, portError(e)
	}
	return r, nil
}
func (s *Service) prepareResolutionSecret(ctx context.Context, c *resolutionCandidate) error {
	if c.draft.Snapshot.CredentialID == "" {
		return nil
	}
	r, e := c.authority.resolutionUsage(c)
	if e != nil {
		return e
	}
	c.usage = r
	plan, e := s.state().deps.Secret.DiscoverUsage(context.WithValue(ctx, resolutionCandidateKey{}, c), r)
	if e != nil {
		return portError(e)
	}
	b, e := sc.UsageBinding(r)
	if e != nil || plan.Validate() != nil || plan.Binding() != b {
		return fault(f.Forbidden)
	}
	c.secret = &plan
	c.locks, e = resolutionUnion(c.locks, plan.RequiredLocks())
	return e
}
func (a *Authority) discoverResolutionUsage(ctx context.Context, r sc.UsageRequest) (sc.UsageDependencies, error) {
	empty := sc.UsageDependencies{}
	if e := resolutionContextError(ctx); e != nil {
		return empty, e
	}
	c, ok := ctx.Value(resolutionCandidateKey{}).(*resolutionCandidate)
	if !ok || c == nil {
		return empty, fault(f.DependencyUnbound)
	}
	expected, e := a.resolutionUsage(c)
	if e != nil {
		return empty, e
	}
	b, e := sc.UsageBinding(r)
	want, err := sc.UsageBinding(expected)
	if e != nil || err != nil || b != want {
		return empty, fault(f.Forbidden)
	}
	return sc.NewUsageDependencies(a.state().usageIssuer, b, c.mapping(), c.locks)
}
func (a *Authority) resolutionApplyWitness(ctx context.Context, tx f.Tx, verified bool) (*resolutionApply, error) {
	if e := resolutionContextError(ctx); e != nil {
		return nil, e
	}
	w, ok := ctx.Value(resolutionApplyKey{}).(*resolutionApply)
	if !ok || w == nil {
		return nil, fault(f.DependencyUnbound)
	}
	w.mu.Lock()
	valid := w.active && (!verified || w.verified) && w.tx == tx && w.candidate != nil && w.candidate.authority == a
	w.mu.Unlock()
	if !valid {
		return nil, fault(f.Forbidden)
	}
	if a.state() == nil || a.state().auth.Resolution == nil {
		return nil, fault(f.DependencyUnbound)
	}
	if _, e := a.state().store.InTx(tx); e != nil {
		return nil, portError(e)
	}
	return w, nil
}
func (a *Authority) validateResolutionAcquireFact(ctx context.Context, tx f.Tx, c *resolutionCandidate) error {
	if e := a.validateResolutionConsumer(ctx, tx, c); e != nil {
		return e
	}
	x, e := a.state().store.InTx(tx)
	if e != nil {
		return portError(e)
	}
	p, e := loadResolutionPreparation(ctx, x, c.identity.Canonical())
	if e != nil {
		return e
	}
	if p == nil || p.Phase != "committed" || p.ID != c.row.ID || p.Semantic != resolutionSemantic(c.request) || p.Version != c.version || !resolutionEqual(p.Draft, c.draft) {
		return fault(f.Forbidden)
	}
	if e = loadCommittedResolution(ctx, x, p); e != nil {
		return e
	}
	return checkCanonicalResolution(ctx, x, c)
}
func (a *Authority) validateResolutionUsage(ctx context.Context, tx f.Tx, r sc.UsageRequest, d sc.UsageDependencies) error {
	w, e := a.resolutionApplyWitness(ctx, tx, false)
	if e != nil {
		return e
	}
	c := w.candidate
	expected, e := a.resolutionUsage(c)
	if e != nil {
		return e
	}
	binding, e := sc.UsageBinding(r)
	want, err := sc.UsageBinding(expected)
	if e != nil || err != nil || binding != want || !d.Matches(a.state().usageIssuer, binding, c.mapping()) {
		return fault(f.Forbidden)
	}
	if e = a.state().store.RequireHeldLocks(ctx, tx, d.RequiredLocks()); e != nil {
		return portError(e)
	}
	if e = a.validateResolutionAcquireFact(ctx, tx, c); e != nil {
		return e
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.active || w.tx != tx {
		return fault(f.Forbidden)
	}
	w.verified = true
	return nil
}
func (a *Authority) authorizeResolutionLease(ctx context.Context, tx f.Tx, actor id.Actor, ref sc.CredentialRef, owner sc.CredentialLeaseOwner, action sc.LeaseAction) (sc.UseGrant, error) {
	empty := sc.UseGrant{}
	w, e := a.resolutionApplyWitness(ctx, tx, true)
	if e != nil {
		return empty, e
	}
	r, e := a.resolutionUsage(w.candidate)
	if e != nil {
		return empty, e
	}
	if action != sc.AcquireLease || !actor.Equal(r.Actor) || !ref.Equal(r.Ref) || !owner.Equal(r.LeaseOwner) {
		return empty, fault(f.Forbidden)
	}
	if e = a.validateResolutionAcquireFact(ctx, tx, w.candidate); e != nil {
		return empty, e
	}
	return sc.UseGrant{Subject: actor, Consumer: sc.Model}, nil
}
func (s *Service) applyResolutionSecret(ctx context.Context, tx f.Tx, c *resolutionCandidate) error {
	if c.draft.Snapshot.CredentialID == "" {
		if c.secret != nil {
			return fault(f.Forbidden)
		}
		return nil
	}
	if c.secret == nil {
		return fault(f.Forbidden)
	}
	r, e := c.authority.resolutionUsage(c)
	if e != nil {
		return e
	}
	w := &resolutionApply{candidate: c, tx: tx, active: true}
	defer func() { w.mu.Lock(); w.active = false; w.verified = false; w.mu.Unlock() }()
	result, e := s.state().deps.Secret.ApplyUsageInTx(context.WithValue(ctx, resolutionApplyKey{}, w), tx, r, *c.secret)
	if e != nil {
		return portError(e)
	}
	if result.Action() != sc.AcquireLeaseUsage {
		return unavailable(nil)
	}
	lease, ok := result.Lease()
	if !ok || lease.LeaseID != r.LeaseID || !lease.CredentialRef.Equal(r.Ref) {
		return unavailable(nil)
	}
	return nil
}
