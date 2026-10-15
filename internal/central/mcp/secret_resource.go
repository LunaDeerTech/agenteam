package mcp

import (
	"context"
	"sync"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

type resourceSecretKey struct{}
type resourceSecretUse struct {
	mu         sync.Mutex
	backend    *ResourceBackend
	invocation ResourceInvocation
	plan       ResourceInvocationPlan
	snapshot   runtimeSnapshot
	request    sc.UsageRequest
	locks      []f.LockRequest
	mapping    f.Digest
	active     bool
	validated  f.Tx
}

func (s *Service) readResourceCredential(ctx context.Context, b *ResourceBackend, v ResourceInvocation, plan ResourceInvocationPlan, snapshot runtimeSnapshot) (sc.SecretMaterial, error) {
	if nilPort(s.options.SecretReader) {
		return sc.SecretMaterial{}, fail(f.DependencyUnbound)
	}
	credential, err := f.ParseID[sc.Credential](snapshot.credential)
	if err != nil {
		return sc.SecretMaterial{}, fail(f.InvalidState)
	}
	lease, err := f.ParseID[sc.Lease](snapshot.lease)
	if err != nil {
		return sc.SecretMaterial{}, fail(f.InvalidState)
	}
	scope, _ := id.InProject(v.Call.ProjectID)
	ref, err := sc.NewCredentialRef(credential, scope)
	if err != nil {
		return sc.SecretMaterial{}, err
	}
	owner, _ := sc.NewCredentialLeaseOwner(sc.ExecutionOwner, v.Call.ExecutionID.String())
	registration, _ := id.RegisterService(id.SecretService)
	actor, err := registration.Actor(v.Call.ExecutionID.String(), scope)
	if err != nil {
		return sc.SecretMaterial{}, err
	}
	r := sc.UsageRequest{Actor: actor, Ref: ref, Purpose: sc.MCP, LeaseOwner: owner, LeaseID: lease, Action: sc.ReadLeaseUsage, RequestID: v.RequestID}
	key, _ := f.AggregateLock(f.ExecutionAggregate, v.Call.ExecutionID.String())
	locks, err := mergeLocks(plan.RequiredLocks(), []f.LockRequest{registryLock(f.Shared), {Key: key, Mode: f.Shared}})
	if err != nil {
		return sc.SecretMaterial{}, err
	}
	binding, err := sc.UsageBinding(r)
	if err != nil {
		return sc.SecretMaterial{}, err
	}
	raw, err := encode(struct {
		Binding                                          f.Digest
		Operation, Attempt, Request, Execution, Snapshot string
		Runtime                                          RuntimeConnectionBinding
	}{binding, v.OperationID.String(), v.AttemptID.String(), v.RequestID, v.Call.ExecutionID.String(), v.Call.SnapshotID, snapshot.binding})
	if err != nil {
		return sc.SecretMaterial{}, err
	}
	w := &resourceSecretUse{backend: b, invocation: v, plan: plan, snapshot: snapshot, request: r, locks: locks, mapping: f.Digest(digest(raw)), active: true}
	defer func() { w.mu.Lock(); w.active = false; w.mu.Unlock() }()
	if snapshot.auth.Scheme == "oauth" {
		return s.resourceOAuthAccess(ctx, w)
	}
	material, err := s.options.SecretReader.ReadMCPCredentialForUsage(context.WithValue(ctx, resourceSecretKey{}, w), r)
	if err != nil {
		material.Destroy()
		return sc.SecretMaterial{}, safeError(err)
	}
	return material, nil
}
func (s *Service) resourceSecretUse(ctx context.Context, r sc.UsageRequest) (*resourceSecretUse, error) {
	if ctx == nil || r.Validate() != nil || r.Purpose != sc.MCP {
		return nil, fail(f.Forbidden)
	}
	w, ok := ctx.Value(resourceSecretKey{}).(*resourceSecretUse)
	if !ok || w == nil || w.backend == nil || w.backend.service() != s {
		return nil, fail(f.Forbidden)
	}
	w.mu.Lock()
	active := w.active
	w.mu.Unlock()
	binding, err := sc.UsageBinding(r)
	want, e := sc.UsageBinding(w.request)
	if !active || err != nil || e != nil || binding != want {
		return nil, fail(f.Forbidden)
	}
	return w, nil
}
func (s *Service) discoverResourceSecret(ctx context.Context, r sc.UsageRequest) (sc.UsageDependencies, error) {
	w, err := s.resourceSecretUse(ctx, r)
	if err != nil {
		return sc.UsageDependencies{}, err
	}
	binding, _ := sc.UsageBinding(r)
	return sc.NewUsageDependencies(s.secretIssuer, binding, w.mapping, w.locks)
}
func (s *Service) validateResourceSecret(ctx context.Context, tx f.Tx, r sc.UsageRequest, plan sc.UsageDependencies) error {
	w, err := s.resourceSecretUse(ctx, r)
	if err != nil {
		return err
	}
	binding, _ := sc.UsageBinding(r)
	if !plan.Matches(s.secretIssuer, binding, w.mapping) {
		return fail(f.Forbidden)
	}
	if err = s.store.RequireHeldLocks(ctx, tx, w.locks); err != nil {
		return safeError(err)
	}
	if err = w.backend.authority.RequireMCPResourceInvocationInTx(ctx, tx, w.invocation, w.plan); err != nil {
		return safeError(err)
	}
	x, err := s.store.InTx(tx)
	if err != nil {
		return safeError(err)
	}
	snapshot, err := loadConnectionSnapshot(ctx, x, w.invocation.Call.ExecutionID, w.snapshot.binding)
	if err != nil {
		return err
	}
	if snapshot != w.snapshot {
		return fail(f.Forbidden)
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.active {
		return fail(f.Forbidden)
	}
	w.validated = tx
	return ctx.Err()
}
func (s *Service) authorizeResourceSecret(ctx context.Context, tx f.Tx, actor id.Actor, ref sc.CredentialRef, owner sc.CredentialLeaseOwner, action sc.LeaseAction) (sc.UseGrant, error) {
	w, ok := ctx.Value(resourceSecretKey{}).(*resourceSecretUse)
	if !ok || w == nil {
		return sc.UseGrant{}, fail(f.Forbidden)
	}
	if _, err := s.resourceSecretUse(ctx, w.request); err != nil {
		return sc.UseGrant{}, err
	}
	w.mu.Lock()
	valid := tx.Valid() && w.active && w.validated == tx
	w.mu.Unlock()
	if !valid || action != sc.ReadLease || !actor.Equal(w.request.Actor) || !ref.Equal(w.request.Ref) || !owner.Equal(w.request.LeaseOwner) {
		return sc.UseGrant{}, fail(f.Forbidden)
	}
	return sc.UseGrant{Subject: w.invocation.Call.Actor, Consumer: sc.MCP, OperationID: w.invocation.OperationID.String(), ToolID: w.invocation.Call.Spec.ToolID.String()}, nil
}
