package model

import (
	"context"
	"encoding/json"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

// Only Management can issue this in-memory witness. HTTP IDs and a Secret
// service actor alone confer no right to read a System model credential.
type ManagementSecretAuthority struct {
	store     Store
	authority *Authority
	fallback  sc.UsageAuthority
	issuer    sc.PlanIssuer
}
type managementUseKey struct{}
type managementUse struct {
	authority      *ManagementSecretAuthority
	op             managementOperation
	actor, subject id.Actor
	ref            sc.CredentialRef
	owner          sc.CredentialLeaseOwner
	lease          sc.LeaseID
}

func NewManagementSecretAuthority(store Store, a *Authority, fallback sc.UsageAuthority) (*ManagementSecretAuthority, error) {
	if a.state() == nil || !sameStore(store, a.state().store) || nilPort(fallback) {
		return nil, fault(f.DependencyUnbound)
	}
	if _, ok := fallback.(sc.UsagePlanner); !ok {
		return nil, fault(f.DependencyUnbound)
	}
	return &ManagementSecretAuthority{store, a, fallback, sc.NewPlanIssuer()}, nil
}
func (a *ManagementSecretAuthority) use(ctx context.Context, r sc.UsageRequest) (*managementUse, error) {
	w, _ := ctx.Value(managementUseKey{}).(*managementUse)
	if w == nil || w.authority != a || r.Validate() != nil || r.Purpose != sc.Model || !r.Ref.Equal(w.ref) || !r.LeaseOwner.Equal(w.owner) || r.LeaseID != w.lease || r.Actor.Details() != w.actor.Details() {
		return nil, fault(f.Forbidden)
	}
	if r.Action != sc.AcquireLeaseUsage && r.Action != sc.ReadLeaseUsage && r.Action != sc.ReleaseLeaseUsage {
		return nil, fault(f.Forbidden)
	}
	return w, nil
}
func (a *ManagementSecretAuthority) CheckReferenceInTx(ctx context.Context, tx f.Tx, actor id.Actor, ref sc.CredentialRef, p sc.Purpose, owner string, retain bool) error {
	return a.fallback.CheckReferenceInTx(ctx, tx, actor, ref, p, owner, retain)
}
func managementMapping(w *managementUse) f.Digest { b, _ := json.Marshal(w.op); return hash(b) }
func (a *ManagementSecretAuthority) DiscoverUsage(ctx context.Context, r sc.UsageRequest) (sc.UsageDependencies, error) {
	if r.LeaseOwner.Details().Kind != sc.ModelManagementRequestOwner {
		return a.fallback.(sc.UsagePlanner).DiscoverUsage(ctx, r)
	}
	w, e := a.use(ctx, r)
	if e != nil {
		return sc.UsageDependencies{}, e
	}
	b, e := sc.UsageBinding(r)
	if e != nil {
		return sc.UsageDependencies{}, e
	}
	locks := []f.LockRequest{managementLock(w.op.ID, f.Exclusive)}
	if r.Action != sc.ReleaseLeaseUsage {
		locks = append(locks, managementCurrentLocks(w.op)...)
	}
	return sc.NewUsageDependencies(a.issuer, b, managementMapping(w), locks)
}
func (a *ManagementSecretAuthority) ValidateUsageInTx(ctx context.Context, tx f.Tx, r sc.UsageRequest, p sc.UsageDependencies) error {
	if r.LeaseOwner.Details().Kind != sc.ModelManagementRequestOwner {
		return a.fallback.(sc.UsagePlanner).ValidateUsageInTx(ctx, tx, r, p)
	}
	w, e := a.use(ctx, r)
	if e != nil {
		return e
	}
	b, e := sc.UsageBinding(r)
	if e != nil {
		return e
	}
	if !p.Matches(a.issuer, b, managementMapping(w)) {
		return fault(f.Forbidden)
	}
	if e = a.store.RequireHeldLocks(ctx, tx, p.RequiredLocks()); e != nil {
		return e
	}
	return a.validate(ctx, tx, w, r.Action != sc.ReleaseLeaseUsage)
}
func (a *ManagementSecretAuthority) validate(ctx context.Context, tx f.Tx, w *managementUse, current bool) error {
	x, e := a.store.InTx(tx)
	if e != nil {
		return e
	}
	o, e := readManagement(ctx, x, "id", w.op.ID)
	if e != nil {
		return e
	}
	if o == nil || o.Process != w.op.Process || o.Lease != w.op.Lease || o.Credential != w.op.Credential || o.User != w.op.User || o.Session != w.op.Session || o.ProviderVersion != w.op.ProviderVersion || o.ModelVersion != w.op.ModelVersion || o.CredentialVersion != w.op.CredentialVersion {
		return fault(f.Forbidden)
	}
	if current {
		if len(o.Result) != 0 {
			return fault(f.InvalidState)
		}
		if e = a.authority.current(ctx, tx, w.subject, id.Mutate); e != nil {
			return e
		}
		_, _, e = managementCurrent(ctx, x, *o)
		return e
	}
	return nil
}
func (a *ManagementSecretAuthority) AuthorizeLeaseInTx(ctx context.Context, tx f.Tx, actor id.Actor, ref sc.CredentialRef, owner sc.CredentialLeaseOwner, action sc.LeaseAction) (sc.UseGrant, error) {
	if owner.Details().Kind != sc.ModelManagementRequestOwner {
		return a.fallback.AuthorizeLeaseInTx(ctx, tx, actor, ref, owner, action)
	}
	w, _ := ctx.Value(managementUseKey{}).(*managementUse)
	if w == nil || w.authority != a || actor.Details() != w.actor.Details() || actor.Details().ServiceName != id.SecretService || actor.Details().CauseRef != owner.Details().ID || !ref.Equal(w.ref) || !owner.Equal(w.owner) {
		return sc.UseGrant{}, fault(f.Forbidden)
	}
	if e := a.validate(ctx, tx, w, action != sc.ReleaseLease); e != nil {
		return sc.UseGrant{}, e
	}
	return sc.UseGrant{Subject: w.subject, Consumer: sc.Model}, nil
}
func (a *ManagementSecretAuthority) context(ctx context.Context, o managementOperation) (context.Context, sc.UsageRequest, error) {
	registration, e := id.RegisterService(id.SecretService)
	if e != nil {
		return ctx, sc.UsageRequest{}, e
	}
	actor, e := registration.Actor(o.ID, id.SystemScope())
	if e != nil {
		return ctx, sc.UsageRequest{}, e
	}
	user, e := f.ParseID[id.User](o.User)
	if e != nil {
		return ctx, sc.UsageRequest{}, e
	}
	session, e := f.ParseID[id.Session](o.Session)
	if e != nil {
		return ctx, sc.UsageRequest{}, e
	}
	subject, e := id.NewHuman(user, session)
	if e != nil {
		return ctx, sc.UsageRequest{}, e
	}
	cid, e := f.ParseID[sc.Credential](o.Credential)
	if e != nil {
		return ctx, sc.UsageRequest{}, e
	}
	ref, e := sc.NewCredentialRef(cid, id.SystemScope())
	if e != nil {
		return ctx, sc.UsageRequest{}, e
	}
	owner, e := sc.NewCredentialLeaseOwner(sc.ModelManagementRequestOwner, o.ID)
	if e != nil {
		return ctx, sc.UsageRequest{}, e
	}
	lease, e := f.ParseID[sc.Lease](o.Lease)
	if e != nil {
		return ctx, sc.UsageRequest{}, e
	}
	w := &managementUse{a, o, actor, subject, ref, owner, lease}
	r := sc.UsageRequest{Actor: actor, Ref: ref, Purpose: sc.Model, LeaseOwner: owner, LeaseID: lease, Action: sc.AcquireLeaseUsage}
	return context.WithValue(ctx, managementUseKey{}, w), r, nil
}
