package model

import (
	"context"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

type SecretUsageRouter struct {
	model    *Authority
	fallback sc.UsageAuthority
}

func NewSecretUsageRouter(model *Authority, fallback sc.UsageAuthority) (*SecretUsageRouter, error) {
	if model.state() == nil || nilPort(fallback) {
		return nil, fault(f.DependencyUnbound)
	}
	return &SecretUsageRouter{model: model, fallback: fallback}, nil
}
func (r *SecretUsageRouter) CheckReferenceInTx(ctx context.Context, tx f.Tx, actor id.Actor, ref sc.CredentialRef, purpose sc.Purpose, owner string, retain bool) error {
	if r == nil || r.model.state() == nil || nilPort(r.fallback) {
		return fault(f.DependencyUnbound)
	}
	if purpose == sc.Model {
		return r.model.CheckReferenceInTx(ctx, tx, actor, ref, purpose, owner, retain)
	}
	return r.fallback.CheckReferenceInTx(ctx, tx, actor, ref, purpose, owner, retain)
}
func (r *SecretUsageRouter) AuthorizeLeaseInTx(ctx context.Context, tx f.Tx, actor id.Actor, ref sc.CredentialRef, owner sc.CredentialLeaseOwner, action sc.LeaseAction) (sc.UseGrant, error) {
	if r == nil || nilPort(r.fallback) {
		return sc.UseGrant{}, fault(f.DependencyUnbound)
	}
	// No Purpose is present. Preserve the existing authority's decision and
	// never guess Model versus MCP from an execution owner.
	return r.fallback.AuthorizeLeaseInTx(ctx, tx, actor, ref, owner, action)
}
func (r *SecretUsageRouter) planner(request sc.UsageRequest) (sc.UsagePlanner, error) {
	if r == nil || r.model.state() == nil || nilPort(r.fallback) {
		return nil, fault(f.DependencyUnbound)
	}
	if request.Purpose == sc.Model {
		if request.Action == sc.RetainReferenceUsage || request.Action == sc.ReleaseReferenceUsage {
			return r.model, nil
		}
		return nil, fault(f.DependencyUnbound)
	}
	p, ok := r.fallback.(sc.UsagePlanner)
	if !ok || nilPort(p) {
		return nil, fault(f.DependencyUnbound)
	}
	return p, nil
}
func (r *SecretUsageRouter) DiscoverUsage(ctx context.Context, request sc.UsageRequest) (sc.UsageDependencies, error) {
	p, e := r.planner(request)
	if e != nil {
		return sc.UsageDependencies{}, e
	}
	return p.DiscoverUsage(ctx, request)
}
func (r *SecretUsageRouter) ValidateUsageInTx(ctx context.Context, tx f.Tx, request sc.UsageRequest, d sc.UsageDependencies) error {
	p, e := r.planner(request)
	if e != nil {
		return e
	}
	return p.ValidateUsageInTx(ctx, tx, request, d)
}

var _ sc.UsageAuthority = (*SecretUsageRouter)(nil)
var _ sc.UsagePlanner = (*SecretUsageRouter)(nil)
