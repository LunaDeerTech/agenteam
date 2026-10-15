package secret

import (
	"context"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

func (s *Service) ModelExecutionLeaseReleasedInTx(ctx context.Context, tx f.Tx, request sc.UsageRequest, plan sc.UsageDependencies) (bool, error) {
	return s.modelLeaseReleasedInTx(ctx, tx, request, plan, sc.ExecutionOwner)
}

func (s *Service) ModelCallLeaseReleasedInTx(ctx context.Context, tx f.Tx, request sc.UsageRequest, plan sc.UsageDependencies) (bool, error) {
	return s.modelLeaseReleasedInTx(ctx, tx, request, plan, sc.ModelCallOwner)
}

func (s *Service) modelLeaseReleasedInTx(ctx context.Context, tx f.Tx, request sc.UsageRequest, plan sc.UsageDependencies, owner sc.LeaseOwnerKind) (bool, error) {
	if s == nil || s.data == nil || s.state() == nil || modelUsageNilPort(s.state().store) || modelUsageNilPort(s.state().auth.Usage) {
		return false, failure(AuthorizationUnbound, f.DependencyUnbound, nil)
	}
	if ctx == nil || request.Validate() != nil || request.Purpose != sc.Model || request.Action != sc.ReleaseLeaseUsage || request.LeaseOwner.Details().Kind != owner {
		return false, invalid()
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if err := s.validateUsage(ctx, tx, request, plan); err != nil {
		return false, err
	}
	x, err := s.state().store.InTx(tx)
	if err != nil {
		return false, unavailable(err)
	}
	lease, err := loadLease(ctx, x, request.LeaseID)
	if err != nil {
		return false, err
	}
	if cancelled := ctx.Err(); cancelled != nil {
		return false, cancelled
	}
	if lease.consumer != sc.Model || !lease.ref.Equal(request.Ref) || !lease.owner.Equal(request.LeaseOwner) {
		return false, failure(AuthorizationRejected, f.Forbidden, nil)
	}
	return lease.released, nil
}

var _ sc.ModelLeaseRetirementObservation = (*Service)(nil)
var _ sc.ModelCallLeaseRetirementObservation = (*Service)(nil)
