package model

import (
	"context"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

func stoppedRuntimeSnapshot(ctx context.Context, x postgres.SQLExecutor, b runtimeBinding) (mc.ConfigSnapshot, error) {
	identity, err := resolutionUnitIdentity(b.Consumer, b.Consumer.Purpose, b.Owner)
	if err != nil {
		return mc.ConfigSnapshot{}, err
	}
	p, err := loadResolutionPreparation(ctx, x, identity.Canonical())
	if err != nil {
		return mc.ConfigSnapshot{}, err
	}
	if p == nil || p.ID != b.PreparationID || p.Phase != "committed" || !p.Request.Consumer.Equal(b.Consumer) || p.Request.Owner != b.Owner || p.Draft.LeaseID != b.LeaseID.String() || p.SnapshotID != b.SnapshotID {
		return mc.ConfigSnapshot{}, fault(f.Forbidden)
	}
	if err = loadCommittedResolution(ctx, x, p); err != nil {
		return mc.ConfigSnapshot{}, err
	}
	raw, err := encoded(p.Draft.Snapshot)
	if err != nil || hash(raw) != b.SnapshotDigest {
		return mc.ConfigSnapshot{}, fault(f.Forbidden)
	}
	return p.Draft.Snapshot.snapshot()
}

func (w *stoppedRuntimeCall) prepareSecret(ctx context.Context) error {
	if w.before.binding.Owner.Kind != sc.ModelCallOwner {
		return fault(f.Forbidden)
	}
	snapshot, err := stoppedRuntimeSnapshot(ctx, w.runtime.store, w.before.binding)
	if err != nil {
		return err
	}
	if snapshot.CredentialRef == nil {
		return fault(f.InvalidState)
	}
	owner, err := sc.NewCredentialLeaseOwner(sc.ModelCallOwner, w.before.binding.Owner.ID)
	if err != nil {
		return runtimePortError(err)
	}
	actor, err := w.runtime.authority.state().auth.SecretService.Actor(owner.Details().ID, snapshot.CredentialRef.Details().Scope)
	if err != nil {
		return runtimePortError(err)
	}
	w.secret = sc.UsageRequest{Actor: actor, Ref: *snapshot.CredentialRef, Purpose: sc.Model, LeaseOwner: owner, LeaseID: w.before.binding.LeaseID, Action: sc.ReleaseLeaseUsage}
	w.secretPlan, err = w.runtime.deps.SecretUsage.DiscoverUsage(ctx, w.secret)
	if err != nil {
		return runtimePortError(err)
	}
	b, err := sc.UsageBinding(w.secret)
	if err != nil || w.secretPlan.Validate() != nil || !w.secretPlan.ProviderPlan().Matches(w.runtime.authority.state().secretIssuer, b, w.mapping) {
		return fault(f.Forbidden)
	}
	w.locks, err = resolutionUnion(w.locks, w.secretPlan.RequiredLocks())
	return err
}

func (a *RuntimeAuthority) stoppedSecret(ctx context.Context, r sc.UsageRequest) (*stoppedRuntimeCall, error) {
	w, err := a.stoppedCall(ctx)
	if err != nil {
		return nil, err
	}
	b, err := sc.UsageBinding(r)
	want, e := sc.UsageBinding(w.secret)
	if err != nil || e != nil || b != want || r.Action != sc.ReleaseLeaseUsage || r.Purpose != sc.Model || r.LeaseOwner.Details().Kind != sc.ModelCallOwner {
		return nil, fault(f.Forbidden)
	}
	return w, nil
}

func (a *RuntimeAuthority) discoverStoppedSecret(ctx context.Context, r sc.UsageRequest) (sc.UsageDependencies, error) {
	w, err := a.stoppedSecret(ctx, r)
	if err != nil {
		return sc.UsageDependencies{}, err
	}
	b, _ := sc.UsageBinding(r)
	return sc.NewUsageDependencies(a.state().secretIssuer, b, w.mapping, w.locks)
}

func (a *RuntimeAuthority) validateStoppedSecret(ctx context.Context, tx f.Tx, r sc.UsageRequest, p sc.UsageDependencies) error {
	w, err := a.stoppedSecret(ctx, r)
	if err != nil {
		return err
	}
	b, _ := sc.UsageBinding(r)
	if !p.Matches(a.state().secretIssuer, b, w.mapping) {
		return fault(f.Forbidden)
	}
	if _, err = w.validateInTx(ctx, tx); err != nil {
		return err
	}
	x, err := a.state().store.InTx(tx)
	if err != nil {
		return runtimePortError(err)
	}
	snapshot, err := stoppedRuntimeSnapshot(ctx, x, w.before.binding)
	if err != nil {
		return err
	}
	if snapshot.CredentialRef == nil || !snapshot.CredentialRef.Equal(r.Ref) {
		return fault(f.Forbidden)
	}
	w.mu.Lock()
	w.validatedSecret = tx
	w.mu.Unlock()
	return nil
}

func (a *RuntimeAuthority) authorizeStoppedSecret(ctx context.Context, tx f.Tx, actor id.Actor, ref sc.CredentialRef, owner sc.CredentialLeaseOwner, action sc.LeaseAction) (sc.UseGrant, error) {
	w, err := a.stoppedCall(ctx)
	if err != nil {
		return sc.UseGrant{}, err
	}
	w.mu.Lock()
	valid := w.active && w.staged && w.tx == tx && w.validatedSecret == tx
	w.mu.Unlock()
	if !valid || action != sc.ReleaseLease || !actor.Equal(w.secret.Actor) || !ref.Equal(w.secret.Ref) || !owner.Equal(w.secret.LeaseOwner) {
		return sc.UseGrant{}, fault(f.Forbidden)
	}
	grant := sc.UseGrant{Subject: actor, Consumer: sc.Model}
	if ref.Details().Scope.Details().Kind == id.ProjectScope {
		grant.OperationID = w.before.binding.Consumer.OperationID
	}
	return grant, nil
}
