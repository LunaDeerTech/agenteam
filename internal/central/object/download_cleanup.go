package object

import (
	"context"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
)

// PurgeProjectDownloadsInTx owns deletion of download-domain records. It can
// only follow real physical cleanup, under the exact lifecycle gate. Late
// checkpoints take Project SH before their grant lock and only UPDATE an
// existing binding, so they cannot recreate data after this Tx commits.
func (s *Service) PurgeProjectDownloadsInTx(ctx context.Context, tx foundation.Tx, actor identity.Actor, cause oc.ProjectCleanupCause, plan oc.AccessLockPlan, locked oc.LockedAccess) error {
	request, err := oc.NewProjectCleanupAccess(oc.AccessRequestDetails{Operation: oc.FinishProjectAccess, Actor: actor, ProjectCleanup: cause})
	if err != nil {
		return err
	}
	if err = s.ValidateAccessPlanInTx(ctx, tx, request, plan, locked); err != nil {
		return err
	}
	if nilPort(s.state().auth.Cleanup) {
		return failure(foundation.DependencyUnbound, nil)
	}
	if err = s.state().auth.Cleanup.CheckProjectCleanupInTx(ctx, tx, actor, cause); err != nil {
		return portError(err)
	}
	e, err := executor(s, tx)
	if err != nil {
		return err
	}
	project := cause.Details().ProjectID.String()
	var remaining bool
	if err = e.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_object.objects WHERE project_id=$1)`, project).Scan(&remaining); err != nil {
		return unavailable(err)
	}
	if remaining {
		return failure(foundation.ResourceBusy, nil)
	}
	// The FK cascade removes attempts including incomplete terminal Audit facts;
	// permanent Project deletion, not elapsed time, authorizes their removal.
	_, err = e.Exec(ctx, `DELETE FROM agenteam_download.grants WHERE project_id=$1`, project)
	return unavailableIf(err)
}
