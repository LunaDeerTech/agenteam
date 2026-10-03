package secret

import (
	"context"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

const CleanupBatchSize = 100

// CleanupProject participates in the D08 persisted deleting/stopped gate. A
// checkpoint binds the operation; each batch scans from the head so retries or
// unknown commits cannot skip rows. Live domain references return pending.
func (s *Service) CleanupProject(ctx context.Context, actor identity.Actor, cause sc.LifecycleCause, project identity.ProjectID, checkpoint *sc.CleanupCheckpoint) (sc.CleanupReport, error) {
	empty := sc.CleanupReport{}
	if project.Validate() != nil || cause.Validate() != nil {
		return empty, invalid()
	}
	d, a := cause.Details(), actor.Details()
	if actor.Validate() != nil || a.Kind != identity.Service || a.ServiceName != identity.ProjectLifecycle || a.ProjectID != project.String() || a.CauseRef != d.OperationID.String() || !d.Deleting {
		return empty, failure(AuthorizationRejected, foundation.Forbidden, nil)
	}
	if checkpoint != nil && (checkpoint.ProjectID != project || checkpoint.OperationID != d.OperationID) {
		return empty, invalid()
	}
	port := s.state().auth.Projects
	if nilPort(port) {
		return empty, failure(AuthorizationUnbound, foundation.DependencyUnbound, nil)
	}
	transactionCause, err := foundation.NewRecoveryCause("secret-project-cleanup", d.OperationID.String(), "")
	if err != nil {
		return empty, invalid()
	}
	report := sc.CleanupReport{Checkpoint: sc.CleanupCheckpoint{ProjectID: project, OperationID: d.OperationID}}
	result := s.state().store.WithinTx(ctx, transactionCause, func(ctx context.Context, tx foundation.Tx) error {
		key, _ := foundation.ProjectLock(project.String())
		if err := s.state().store.Acquire(ctx, tx, key, foundation.Exclusive); err != nil {
			return unavailable(err)
		}
		if err := port.CheckCleanupInTx(ctx, tx, actor, cause, project); err != nil {
			return authorization(err)
		}
		e, err := s.state().store.InTx(tx)
		if err != nil {
			return unavailable(err)
		}
		var retained bool
		if err = e.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_secret.secret_references WHERE project_id=$1) OR EXISTS(SELECT 1 FROM agenteam_secret.secret_leases WHERE project_id=$1 AND NOT released)`, project.String()).Scan(&retained); err != nil {
			return unavailable(err)
		}
		if retained {
			return nil
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
		if err = e.QueryRow(ctx, `SELECT NOT EXISTS(SELECT 1 FROM agenteam_secret.secret_payloads WHERE scope='project' AND project_id=$1) AND NOT EXISTS(SELECT 1 FROM agenteam_secret.secrets WHERE scope='project' AND project_id=$1) AND NOT EXISTS(SELECT 1 FROM agenteam_secret.secret_command_receipts WHERE scope='project' AND project_id=$1)`, project.String()).Scan(&report.Completed); err != nil {
			return unavailable(err)
		}
		return nil
	})
	if err = commitError(result); err != nil {
		return empty, err
	}
	return report, nil
}
