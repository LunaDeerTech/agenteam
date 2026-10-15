package secret

import (
	"context"
	"errors"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

func (s *Service) DiscoverMCPRequestLeaseObservation(ctx context.Context, r sc.UsageRequest) (sc.UsageDependencies, error) {
	if ctx == nil || r.Validate() != nil || r.Purpose != sc.MCP || r.Action != sc.ReleaseLeaseUsage || r.LeaseOwner.Details().Kind != sc.MCPConnectionRequestOwner {
		return sc.UsageDependencies{}, invalid()
	}
	actor, scope := r.Actor.Details(), r.Ref.Details().Scope.Details()
	if scope.Kind != id.ProjectScope || actor.Kind != id.Service || actor.ServiceName != id.SecretService || actor.ProjectID != scope.ProjectID || actor.CauseRef != r.LeaseOwner.Details().ID {
		return sc.UsageDependencies{}, invalid()
	}
	if err := ctx.Err(); err != nil {
		return sc.UsageDependencies{}, err
	}
	if s == nil || s.data == nil || s.state() == nil {
		return sc.UsageDependencies{}, failure(AuthorizationUnbound, f.DependencyUnbound, nil)
	}
	planner, ok := s.state().auth.Usage.(sc.UsagePlanner)
	if !ok || nilPort(planner) {
		return sc.UsageDependencies{}, failure(AuthorizationUnbound, f.DependencyUnbound, nil)
	}
	// This is only a dependency plan. The caller's private recovery authority
	// must admit the exact original request; no Secret row is inspected here.
	// Absence can be established only by ObserveMCPRequestLeaseInTx after
	// validateUsage has rechecked that proof under the complete held locks.
	plan, err := planner.DiscoverUsage(ctx, r)
	if err != nil {
		return sc.UsageDependencies{}, plannedAuthorization(err)
	}
	return s.wrapUsagePlan(r, plan)
}

func (s *Service) ObserveMCPRequestLeaseInTx(ctx context.Context, tx f.Tx, r sc.UsageRequest, plan sc.UsageDependencies) (sc.MCPRequestLeaseState, error) {
	if s == nil || s.data == nil || s.state() == nil || nilPort(s.state().auth.Usage) {
		return sc.MCPRequestLeaseState{}, failure(AuthorizationUnbound, f.DependencyUnbound, nil)
	}
	if ctx == nil || !tx.Valid() || r.Validate() != nil || r.Purpose != sc.MCP || r.LeaseOwner.Details().Kind != sc.MCPConnectionRequestOwner || (r.Action != sc.AcquireLeaseUsage && r.Action != sc.ReleaseLeaseUsage) {
		return sc.MCPRequestLeaseState{}, invalid()
	}
	if err := ctx.Err(); err != nil {
		return sc.MCPRequestLeaseState{}, err
	}
	if err := s.validateUsage(ctx, tx, r, plan); err != nil {
		return sc.MCPRequestLeaseState{}, err
	}
	x, err := s.state().store.InTx(tx)
	if err != nil {
		return sc.MCPRequestLeaseState{}, unavailable(err)
	}
	lease, err := loadLease(ctx, x, r.LeaseID)
	if err != nil {
		var fault *f.Fault
		if errors.As(err, &fault) && fault.Code == f.NotFound {
			return sc.MCPRequestLeaseState{}, nil
		}
		return sc.MCPRequestLeaseState{}, err
	}
	if lease.consumer != sc.MCP || !lease.ref.Equal(r.Ref) || !lease.owner.Equal(r.LeaseOwner) {
		return sc.MCPRequestLeaseState{}, failure(AuthorizationRejected, f.Forbidden, nil)
	}
	return sc.MCPRequestLeaseState{Exists: true, Released: lease.released}, nil
}

var _ sc.MCPRequestLeaseObservation = (*Service)(nil)
var _ sc.MCPRequestLeaseObservationPlanner = (*Service)(nil)
