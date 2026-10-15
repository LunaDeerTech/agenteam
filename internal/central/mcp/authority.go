package mcp

import (
	"context"
	"sync/atomic"

	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
)

// Authority breaks only the static composition cycle. Service.New binds its
// actual same-Store service once; there is no exported rebinding operation.
// All grants still require that Service's original private command/capture/
// invocation witness, and every unbound call fails closed.
type Authority struct {
	store Store
	bound atomic.Pointer[Service]
}

func NewAuthority(store Store) (*Authority, error) {
	if nilPort(store) {
		return nil, fail(f.DependencyUnbound)
	}
	return &Authority{store: store}, nil
}
func (a *Authority) bind(service *Service) error {
	if a == nil || service == nil || !sameStore(a.store, service.store) || nilPort(service.options.Registry) {
		return fail(f.DependencyUnbound)
	}
	if !a.bound.CompareAndSwap(nil, service) {
		return fail(f.InvalidState)
	}
	return nil
}
func (a *Authority) service() (*Service, error) {
	if a == nil {
		return nil, fail(f.DependencyUnbound)
	}
	s := a.bound.Load()
	if s == nil {
		return nil, fail(f.DependencyUnbound)
	}
	return s, nil
}
func (a *Authority) RequireBindingInTx(ctx context.Context, tx f.Tx, store any) error {
	s, err := a.service()
	if err != nil {
		return err
	}
	return s.RequireBindingInTx(ctx, tx, store)
}
func (a *Authority) RequireMCPDiscoveryInTx(ctx context.Context, tx f.Tx, d tc.MCPDiscovery) error {
	s, err := a.service()
	if err != nil {
		return err
	}
	return s.RequireMCPDiscoveryInTx(ctx, tx, d)
}
func (a *Authority) RequireMCPRemovalInTx(ctx context.Context, tx f.Tx, project id.ProjectID, connection string) error {
	s, err := a.service()
	if err != nil {
		return err
	}
	return s.RequireMCPRemovalInTx(ctx, tx, project, connection)
}
func (a *Authority) RequireCurrentMCPBindingInTx(ctx context.Context, tx f.Tx, b tc.MCPBinding) error {
	s, err := a.service()
	if err != nil {
		return err
	}
	return s.RequireCurrentMCPBindingInTx(ctx, tx, b)
}
func (a *Authority) DiscoverMCPRetention(ctx context.Context, r tc.ExecutionToolCaptureRequest, b []tc.MCPBinding) (tc.MCPRetentionPlan, error) {
	s, err := a.service()
	if err != nil {
		return nil, err
	}
	return s.DiscoverMCPRetention(ctx, r, b)
}
func (a *Authority) RetainMCPRuntimeInTx(ctx context.Context, tx f.Tx, execution id.ExecutionID, b tc.MCPBinding, p tc.MCPRetentionPlan) error {
	s, err := a.service()
	if err != nil {
		return err
	}
	return s.RetainMCPRuntimeInTx(ctx, tx, execution, b, p)
}
func (a *Authority) DiscoverMCPResources(ctx context.Context, r tc.ExecutionToolCaptureRequest, p tc.MCPRetentionPlan) (tc.MCPResourceCapturePlan, error) {
	s, err := a.service()
	if err != nil {
		return nil, err
	}
	return s.DiscoverMCPResources(ctx, r, p)
}
func (a *Authority) RetainMCPResourcesInTx(ctx context.Context, tx f.Tx, r tc.ExecutionToolCaptureRequest, p tc.MCPResourceCapturePlan) error {
	s, err := a.service()
	if err != nil {
		return err
	}
	return s.RetainMCPResourcesInTx(ctx, tx, r, p)
}
func (a *Authority) RequireMCPAuditInTx(ctx context.Context, tx f.Tx, e ac.Entry, k ac.AppendKey) error {
	s, err := a.service()
	if err != nil {
		return err
	}
	return s.RequireMCPAuditInTx(ctx, tx, e, k)
}
func (a *Authority) CheckProjectAuditInTx(ctx context.Context, tx f.Tx, e ac.Entry, k ac.AppendKey) error {
	s, err := a.service()
	if err != nil {
		return err
	}
	return s.CheckProjectAuditInTx(ctx, tx, e, k)
}
func (a *Authority) DiscoverUsage(ctx context.Context, r sc.UsageRequest) (sc.UsageDependencies, error) {
	s, err := a.service()
	if err != nil {
		return sc.UsageDependencies{}, err
	}
	return s.DiscoverUsage(ctx, r)
}
func (a *Authority) ValidateUsageInTx(ctx context.Context, tx f.Tx, r sc.UsageRequest, p sc.UsageDependencies) error {
	s, err := a.service()
	if err != nil {
		return err
	}
	return s.ValidateUsageInTx(ctx, tx, r, p)
}
func (a *Authority) AuthorizeLeaseInTx(ctx context.Context, tx f.Tx, actor id.Actor, ref sc.CredentialRef, owner sc.CredentialLeaseOwner, action sc.LeaseAction) (sc.UseGrant, error) {
	s, err := a.service()
	if err != nil {
		return sc.UseGrant{}, err
	}
	return s.AuthorizeLeaseInTx(ctx, tx, actor, ref, owner, action)
}
func (a *Authority) CheckReferenceInTx(ctx context.Context, tx f.Tx, actor id.Actor, ref sc.CredentialRef, purpose sc.Purpose, reference string, retain bool) error {
	s, err := a.service()
	if err != nil {
		return err
	}
	return s.CheckReferenceInTx(ctx, tx, actor, ref, purpose, reference, retain)
}

var _ tc.MCPRegistryAuthority = (*Authority)(nil)
var _ tc.MCPResourceCaptureProvider = (*Authority)(nil)
var _ ac.MCPAuthority = (*Authority)(nil)
var _ ac.ProjectFactAuthority = (*Authority)(nil)
var _ sc.UsagePlanner = (*Authority)(nil)
var _ sc.UsageAuthority = (*Authority)(nil)
