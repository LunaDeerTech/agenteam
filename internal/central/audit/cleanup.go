package audit

import (
	"context"
	contract "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

const CleanupBatchSize = 500

// Cleanup always starts from remaining rows. A checkpoint identifies the
// lifecycle operation; its diagnostic LastID is never a skip/delete boundary.
func (s *Service) CleanupProject(ctx context.Context, actor identity.Actor, cause contract.LifecycleCause, project identity.ProjectID, checkpoint *contract.CleanupCheckpoint) (contract.CleanupReport, error) {
	empty := contract.CleanupReport{}
	if project.Validate() != nil || cause.Validate() != nil || actor.Validate() != nil {
		return empty, invalid("cleanup_input")
	}
	a, c := actor.Details(), cause.Details()
	if c.Action != contract.Delete || a.Kind != identity.Service || a.ServiceName != identity.ProjectLifecycle || a.ProjectID != project.String() || a.CauseRef != c.OperationID.String() {
		return empty, failure(foundation.Forbidden, "cleanup_cause_rejected", nil)
	}
	if checkpoint != nil && (checkpoint.ProjectID != project || checkpoint.OperationID != c.OperationID || checkpoint.LastID != nil && checkpoint.LastID.Validate() != nil) {
		return empty, invalid("cleanup_checkpoint")
	}
	if nilPort(s.auth.Projects) {
		return empty, failure(foundation.DependencyUnbound, "project_gate_unbound", nil)
	}
	transactionCause, err := foundation.NewRecoveryCause("audit-cleanup", c.OperationID.String(), project.String())
	if err != nil {
		return empty, invalid("cleanup_cause")
	}
	key, _ := foundation.ProjectLock(project.String())
	report := contract.CleanupReport{State: contract.CleanupPending, Checkpoint: contract.CleanupCheckpoint{ProjectID: project, OperationID: c.OperationID}}
	run := func(remove bool) foundation.CommitResult {
		return s.store.WithinTx(ctx, transactionCause, func(ctx context.Context, tx foundation.Tx) error {
			if err := s.store.Acquire(ctx, tx, key, foundation.Exclusive); err != nil {
				return unavailable(err)
			}
			if err := s.auth.Projects.CheckCleanupInTx(ctx, tx, actor, cause, project); err != nil {
				return portError(err)
			}
			e, err := s.store.InTx(tx)
			if err != nil {
				return unavailable(err)
			}
			if remove {
				rows, err := e.Query(ctx, `DELETE FROM agenteam_audit.audit_records WHERE id IN (SELECT id FROM agenteam_audit.audit_records WHERE scope='project' AND project_id=$1 ORDER BY id LIMIT 500) RETURNING id::text`, project.String())
				if err != nil {
					return unavailable(err)
				}
				for rows.Next() {
					var raw string
					if err = rows.Scan(&raw); err != nil {
						rows.Close()
						return unavailable(err)
					}
					id, err := foundation.ParseID[contract.Record](raw)
					if err != nil {
						rows.Close()
						return unavailable(err)
					}
					if report.Checkpoint.LastID == nil || id.String() > report.Checkpoint.LastID.String() {
						report.Checkpoint.LastID = &id
					}
				}
				rows.Close()
				if err = rows.Err(); err != nil {
					return unavailable(err)
				}
			}
			var remaining bool
			if err := e.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_audit.audit_records WHERE scope='project' AND project_id=$1)`, project.String()).Scan(&remaining); err != nil {
				return unavailable(err)
			}
			report.State = contract.CleanupPending
			if !remaining {
				report.State = contract.CleanupCompleted
			}
			return nil
		})
	}
	result := run(true)
	switch result.State() {
	case foundation.Committed:
		return report, nil
	case foundation.Unknown:
		// This second lock also waits for an ambiguous earlier backend to release
		// the Project gate, so absence is observed only after that attempt ends.
		report.Checkpoint.LastID = nil
		verified := run(false)
		if verified.State() == foundation.Committed {
			return report, nil
		}
		return empty, failure(foundation.CommitUnknown, "cleanup_unknown", verified.Fault())
	default:
		return empty, portError(result.Fault())
	}
}
