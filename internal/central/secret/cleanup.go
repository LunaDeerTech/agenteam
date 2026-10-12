package secret

import (
	"context"
	"errors"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

const CleanupBatchSize = 100

// CleanupProject participates in the D08 persisted deleting/stopped gate. A
// checkpoint binds the operation; each batch scans from the head. An original
// Unknown is retained by this Service until a later explicit, read-only call
// returns Committed. Live domain references return pending.
func (s *Service) CleanupProject(ctx context.Context, actor identity.Actor, cause sc.LifecycleCause, project identity.ProjectID, checkpoint *sc.CleanupCheckpoint) (sc.CleanupReport, error) {
	empty := sc.CleanupReport{}
	if ctx == nil || project.Validate() != nil || cause.Validate() != nil {
		return empty, invalid()
	}
	d, a := cause.Details(), actor.Details()
	if actor.Validate() != nil || a.Kind != identity.Service || a.ServiceName != identity.ProjectLifecycle || a.ProjectID != project.String() || a.CauseRef != d.OperationID.String() || !d.Deleting {
		return empty, failure(AuthorizationRejected, foundation.Forbidden, nil)
	}
	if checkpoint != nil && (checkpoint.ProjectID != project || checkpoint.OperationID != d.OperationID) {
		return empty, invalid()
	}
	if s == nil || s.data == nil || s.state() == nil || nilPort(s.state().store) {
		return empty, unavailable(nil)
	}
	state := s.state()
	port := state.auth.Projects
	if nilPort(port) {
		return empty, failure(AuthorizationUnbound, foundation.DependencyUnbound, nil)
	}
	transactionCause, err := foundation.NewRecoveryCause("secret-project-cleanup", d.OperationID.String(), "")
	if err != nil {
		return empty, invalid()
	}
	owner, err := state.borrowProjectCleanup(actor, d, project)
	if err != nil {
		return empty, err
	}
	defer state.releaseProjectCleanup(project, owner)
	confirm := owner.retained
	if err = ctx.Err(); err != nil {
		if confirm {
			return empty, errors.Join(projectCleanupUnknown(owner.original), unavailable(err))
		}
		return empty, unavailable(err)
	}
	validated := false
	report := sc.CleanupReport{Checkpoint: sc.CleanupCheckpoint{ProjectID: project, OperationID: d.OperationID}}
	result := state.store.WithinTx(ctx, transactionCause, func(ctx context.Context, tx foundation.Tx) error {
		key, _ := foundation.ProjectLock(project.String())
		if err := state.store.Acquire(ctx, tx, key, foundation.Exclusive); err != nil {
			return unavailable(err)
		}
		if err := port.CheckCleanupInTx(ctx, tx, actor, cause, project); err != nil {
			return authorization(err)
		}
		e, err := state.store.InTx(tx)
		if err != nil {
			return unavailable(err)
		}
		if err := ctx.Err(); err != nil {
			return unavailable(err)
		}
		var retained bool
		if err = e.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_secret.secret_references WHERE project_id=$1) OR EXISTS(SELECT 1 FROM agenteam_secret.secret_leases WHERE project_id=$1 AND NOT released)`, project.String()).Scan(&retained); err != nil {
			return unavailable(err)
		}
		if err = ctx.Err(); err != nil {
			return unavailable(err)
		}
		if retained {
			validated = true
			return nil
		}
		if confirm {
			report.Completed, err = projectCleanupEmpty(ctx, e, project)
			validated = err == nil
			return err
		}
		// Tombstones have no payload and do not protect a value. Their removal is
		// authorized only here or in a successful credential deletion transaction.
		if _, err = e.Exec(ctx, `DELETE FROM agenteam_secret.secret_leases WHERE project_id=$1 AND released`, project.String()); err != nil {
			return unavailable(err)
		}
		if _, err = e.Exec(ctx, `WITH removed AS (DELETE FROM agenteam_secret.secrets WHERE id IN (SELECT id FROM agenteam_secret.secrets WHERE scope='project' AND project_id=$1 ORDER BY id LIMIT 100) RETURNING current_payload_id) DELETE FROM agenteam_secret.secret_payloads WHERE payload_id IN (SELECT current_payload_id FROM removed)`, project.String()); err != nil {
			return unavailable(err)
		}
		if _, err = e.Exec(ctx, `WITH removed AS (DELETE FROM agenteam_secret.secret_command_receipts WHERE id IN (SELECT id FROM agenteam_secret.secret_command_receipts WHERE scope='project' AND project_id=$1 ORDER BY id LIMIT 100) RETURNING digest_payload_id) DELETE FROM agenteam_secret.secret_payloads WHERE payload_id IN (SELECT digest_payload_id FROM removed)`, project.String()); err != nil {
			return unavailable(err)
		}
		if err = cleanupProjectVariableReceipts(ctx, e, project); err != nil {
			return err
		}
		report.Completed, err = projectCleanupEmpty(ctx, e, project)
		validated = err == nil
		return err
	})
	if confirm {
		// Confirmation reports current cleanup facts, not the physical outcome of
		// the original attempt. Its original return must precede releasing it.
		if result.State() != foundation.Committed || !validated || ctx.Err() != nil {
			confirmationErr := projectCleanupResultError(result)
			if ctx.Err() != nil || !validated {
				confirmationErr = errors.Join(confirmationErr, unavailable(ctx.Err()))
			}
			return empty, errors.Join(projectCleanupUnknown(owner.original), confirmationErr)
		}
		owner.retained = false
	} else if result.State() == foundation.Unknown {
		owner.original, owner.retained = result, true
		return empty, projectCleanupUnknown(result)
	}
	if err = projectCleanupResultError(result); err != nil {
		return empty, err
	}
	if !validated || ctx.Err() != nil {
		return empty, unavailable(ctx.Err())
	}
	return report, nil
}
