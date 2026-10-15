package secret

import (
	"context"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

func (s *Service) ModelServingSnapshotReferenceInTx(ctx context.Context, tx f.Tx, request sc.UsageRequest, plan sc.UsageDependencies) (bool, error) {
	if s == nil || s.data == nil || s.state() == nil || nilPort(s.state().store) || nilPort(s.state().auth.Usage) {
		return false, failure(AuthorizationUnbound, f.DependencyUnbound, nil)
	}
	if ctx == nil || request.Validate() != nil || request.Purpose != sc.Model || request.Action != sc.RetainReferenceUsage && request.Action != sc.ReleaseReferenceUsage {
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
	ref := request.Ref.Details()
	var present bool
	err = x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_secret.secret_references WHERE credential_id=$1 AND scope=$2 AND project_id IS NOT DISTINCT FROM $3::uuid AND consumer='model' AND owner_id=$4)`, ref.ID.String(), string(ref.Scope.Details().Kind), null(ref.Scope.Details().ProjectID), request.ReferenceOwner).Scan(&present)
	if err != nil {
		return false, unavailable(err)
	}
	if err = ctx.Err(); err != nil {
		return false, err
	}
	return present, nil
}

var _ sc.ModelServingSnapshotReferenceObservation = (*Service)(nil)
