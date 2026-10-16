package outbound

import (
	"context"
	"encoding/json"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

// The proxy singleton owns System-purpose Secret references. Its use authority
// accepts only a private, per-operation witness issued by this module, never a
// caller-supplied owner ID or a generic Secret service actor on its own.
const proxySettingsID = "01900000-0000-7000-8000-000000000001"

type proxySecretStore interface {
	Store
	RequireHeldLocks(context.Context, foundation.Tx, []foundation.LockRequest) error
}
type ProxySecretAuthority struct {
	store    proxySecretStore
	fallback sc.UsageAuthority
	issuer   sc.PlanIssuer
}
type proxyUseKey struct{}
type proxyUse struct {
	authority      *ProxySecretAuthority
	version        foundation.Version
	previous, next string
	actor          identity.Actor
	owner          sc.CredentialLeaseOwner
	ref            sc.CredentialRef
	process        string
	lease          sc.LeaseID
}

func NewProxySecretAuthority(store Store, fallback sc.UsageAuthority) (*ProxySecretAuthority, error) {
	checked, ok := store.(proxySecretStore)
	if !ok || nilPort(fallback) {
		return nil, invalid()
	}
	if _, ok = fallback.(sc.UsagePlanner); !ok {
		return nil, invalid()
	}
	return &ProxySecretAuthority{checked, fallback, sc.NewPlanIssuer()}, nil
}
func (a *ProxySecretAuthority) witness(ctx context.Context) *proxyUse {
	if ctx == nil {
		return nil
	}
	w, _ := ctx.Value(proxyUseKey{}).(*proxyUse)
	if w == nil || w.authority != a {
		return nil
	}
	return w
}
func (a *ProxySecretAuthority) CheckReferenceInTx(ctx context.Context, tx foundation.Tx, actor identity.Actor, ref sc.CredentialRef, purpose sc.Purpose, owner string, retain bool) error {
	if owner == proxySettingsID {
		return foundation.NewFault(foundation.ResourceBusy, foundation.NotStarted)
	}
	return a.fallback.CheckReferenceInTx(ctx, tx, actor, ref, purpose, owner, retain)
}
func (a *ProxySecretAuthority) AuthorizeLeaseInTx(ctx context.Context, tx foundation.Tx, actor identity.Actor, ref sc.CredentialRef, owner sc.CredentialLeaseOwner, action sc.LeaseAction) (sc.UseGrant, error) {
	if owner.Details().Kind != sc.OutboundProxyOwner {
		return a.fallback.AuthorizeLeaseInTx(ctx, tx, actor, ref, owner, action)
	}
	w := a.witness(ctx)
	if w == nil || !w.ref.Equal(ref) || !w.owner.Equal(owner) || actor.Validate() != nil || actor.Details() != w.actor.Details() || actor.Details().Kind != identity.Service || actor.Details().ServiceName != identity.SecretService || actor.Details().CauseRef != owner.Details().ID {
		return sc.UseGrant{}, foundation.NewFault(foundation.Forbidden, foundation.NotStarted)
	}
	if err := a.validateOperation(ctx, tx, w); err != nil {
		return sc.UseGrant{}, err
	}
	if action != sc.ReleaseLease {
		x, e := a.store.InTx(tx)
		if e != nil {
			return sc.UseGrant{}, e
		}
		p, e := readProxy(ctx, x)
		if e != nil {
			return sc.UseGrant{}, e
		}
		if p.settings.Version != w.version || p.settings.Mode != "proxy" || p.ref != ref.Details().ID.String() {
			return sc.UseGrant{}, foundation.NewFault(foundation.Forbidden, foundation.NotStarted)
		}
	}
	return sc.UseGrant{Subject: actor, Consumer: sc.System}, nil
}
func (a *ProxySecretAuthority) matches(ctx context.Context, r sc.UsageRequest) (*proxyUse, error) {
	w := a.witness(ctx)
	if w == nil || r.Validate() != nil || r.Purpose != sc.System || !r.Ref.Details().Scope.Equal(identity.SystemScope()) || r.Actor.Details() != w.actor.Details() {
		return nil, foundation.NewFault(foundation.Forbidden, foundation.NotStarted)
	}
	switch r.Action {
	case sc.RetainReferenceUsage:
		if r.ReferenceOwner != proxySettingsID || r.Ref.Details().ID.String() != w.next || w.next == w.previous {
			return nil, invalid()
		}
	case sc.ReleaseReferenceUsage:
		if r.ReferenceOwner != proxySettingsID || r.Ref.Details().ID.String() != w.previous || w.next == w.previous {
			return nil, invalid()
		}
	case sc.AcquireLeaseUsage, sc.ReadLeaseUsage, sc.ReleaseLeaseUsage:
		if !r.Ref.Equal(w.ref) || !r.LeaseOwner.Equal(w.owner) {
			return nil, invalid()
		}
	default:
		return nil, invalid()
	}
	return w, nil
}
func proxyMapping(w *proxyUse) foundation.Digest {
	b, _ := json.Marshal(struct {
		Version                    foundation.Version
		Previous, Next, Ref, Owner string
	}{w.version, w.previous, w.next, w.ref.Details().ID.String(), w.owner.Details().ID})
	return foundation.Digest(digestBytes(b))
}
func (a *ProxySecretAuthority) DiscoverUsage(ctx context.Context, r sc.UsageRequest) (sc.UsageDependencies, error) {
	if r.ReferenceOwner != proxySettingsID && r.LeaseOwner.Details().Kind != sc.OutboundProxyOwner {
		return a.fallback.(sc.UsagePlanner).DiscoverUsage(ctx, r)
	}
	w, e := a.matches(ctx, r)
	if e != nil {
		return sc.UsageDependencies{}, e
	}
	b, e := sc.UsageBinding(r)
	if e != nil {
		return sc.UsageDependencies{}, e
	}
	locks := []foundation.LockRequest{{Key: proxyLock(), Mode: foundation.Shared}}
	if r.LeaseID.Validate() == nil {
		mode := foundation.Exclusive
		if r.Action == sc.ReadLeaseUsage {
			mode = foundation.Shared
		}
		locks = append(locks, foundation.LockRequest{Key: proxyOperationLock(w.owner.Details().ID), Mode: mode})
	}
	return sc.NewUsageDependencies(a.issuer, b, proxyMapping(w), locks)
}
func (a *ProxySecretAuthority) ValidateUsageInTx(ctx context.Context, tx foundation.Tx, r sc.UsageRequest, p sc.UsageDependencies) error {
	if r.ReferenceOwner != proxySettingsID && r.LeaseOwner.Details().Kind != sc.OutboundProxyOwner {
		return a.fallback.(sc.UsagePlanner).ValidateUsageInTx(ctx, tx, r, p)
	}
	w, e := a.matches(ctx, r)
	if e != nil {
		return e
	}
	b, e := sc.UsageBinding(r)
	if e != nil {
		return e
	}
	if !p.Matches(a.issuer, b, proxyMapping(w)) {
		return foundation.NewFault(foundation.Forbidden, foundation.NotStarted)
	}
	if e = a.store.RequireHeldLocks(ctx, tx, p.RequiredLocks()); e != nil {
		return e
	}
	if r.Action == sc.AcquireLeaseUsage || r.Action == sc.ReadLeaseUsage || r.Action == sc.ReleaseLeaseUsage {
		if err := a.validateOperation(ctx, tx, w); err != nil {
			return err
		}
	}
	if r.Action == sc.ReleaseLeaseUsage {
		return nil
	}
	x, e := a.store.InTx(tx)
	if e != nil {
		return e
	}
	record, e := readProxy(ctx, x)
	if e != nil {
		return e
	}
	if record.settings.Version != w.version {
		return foundation.NewFault(foundation.VersionConflict, foundation.NotStarted)
	}
	if r.Action == sc.RetainReferenceUsage || r.Action == sc.ReleaseReferenceUsage {
		if record.ref != w.next {
			return foundation.NewFault(foundation.Forbidden, foundation.NotStarted)
		}
		return nil
	}
	if record.settings.Mode != "proxy" || record.ref != r.Ref.Details().ID.String() {
		return foundation.NewFault(foundation.Forbidden, foundation.NotStarted)
	}
	return nil
}
func (s *PolicyService) borrowProxyCredential(ctx context.Context, record proxyRecord, use func([]byte) error) (err error) {
	ctx, cancelBorrow := context.WithTimeout(ctx, 2*time.Second)
	defer cancelBorrow()
	deps := s.state().proxyDependencies
	if deps == nil || nilPort(deps.Processes) || record.ref == "" || use == nil {
		return unavailable(nil)
	}
	process, e := deps.Processes.CurrentProxyProcess()
	if e != nil {
		return e
	}
	id, e := foundation.NewID[struct{}]()
	if e != nil {
		return unavailable(e)
	}
	registration, e := identity.RegisterService(identity.SecretService)
	if e != nil {
		return e
	}
	actor, e := registration.Actor(id.String(), identity.SystemScope())
	if e != nil {
		return e
	}
	credential, e := foundation.ParseID[sc.Credential](record.ref)
	if e != nil {
		return unavailable(e)
	}
	ref, e := sc.NewCredentialRef(credential, identity.SystemScope())
	if e != nil {
		return e
	}
	owner, e := sc.NewCredentialLeaseOwner(sc.OutboundProxyOwner, id.String())
	if e != nil {
		return e
	}
	leaseID, e := foundation.NewID[sc.Lease]()
	if e != nil {
		return unavailable(e)
	}
	ctx = context.WithValue(ctx, proxyUseKey{}, &proxyUse{authority: deps.Authority, version: record.settings.Version, actor: actor, owner: owner, ref: ref, process: process, lease: leaseID})
	req := sc.UsageRequest{Actor: actor, Ref: ref, Purpose: sc.System, LeaseOwner: owner, LeaseID: leaseID, Action: sc.AcquireLeaseUsage}
	plan, e := deps.Secrets.DiscoverUsage(ctx, req)
	if e != nil {
		return e
	}
	cause, e := foundation.NewRecoveryCause("outbound-proxy-credential", id.String(), "")
	if e != nil {
		return e
	}
	apply := func(c context.Context, r sc.UsageRequest, p sc.UsageDependencies) error {
		result := s.state().store.WithinTx(c, cause, func(c context.Context, tx foundation.Tx) error {
			if e := s.state().store.AcquireAll(c, tx, p.RequiredLocks()); e != nil {
				return e
			}
			x, e := s.state().store.InTx(tx)
			if e != nil {
				return e
			}
			if _, e = x.Exec(c, `INSERT INTO agenteam_outbound.proxy_credential_operations(operation_id,process_id,lease_id,credential_id,proxy_version) VALUES($1,$2,$3,$4,$5)`, id.String(), process, leaseID.String(), record.ref, int64(record.settings.Version)); e != nil {
				return e
			}
			_, e = deps.Secrets.ApplyUsageInTx(c, tx, r, p)
			return e
		})
		return resultError(result)
	}
	// Cleanup is attempted even if the acquire COMMIT acknowledgement is lost.
	// Release is idempotent and has its own bounded context, independent of the
	// cancelled network operation. A cleanup failure closes admission for repair.
	defer func() {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		e := s.releaseProxyOperation(cleanup, proxyCredentialOperation{id.String(), process, leaseID.String(), record.ref, record.settings.Version})
		if e != nil {
			s.unavailable()
			if err == nil {
				err = unavailable(e)
			}
		}
	}()
	if e = apply(ctx, req, plan); e != nil {
		return e
	}
	material, e := deps.Secrets.ReadCredentialForRequest(ctx, actor, leaseID)
	if e != nil {
		return e
	}
	defer material.Destroy()
	return material.Use(use)
}
