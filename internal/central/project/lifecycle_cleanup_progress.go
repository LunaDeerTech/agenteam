package project

import (
	"context"
	"math"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	c "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

func cleanupOutboxRow(ctx context.Context, x postgres.SQLExecutor, operation c.OperationID) (version, attempts int64, err error) {
	var checkpoint bool
	err = x.QueryRow(ctx, `SELECT version,attempt_count,(checkpoint IS NOT NULL OR checkpoint_schema IS NOT NULL) FROM agenteam_project.lifecycle_participants WHERE operation_id=$1 AND participant_name='outbox'`, operation.String()).Scan(&version, &attempts, &checkpoint)
	if err != nil {
		return 0, 0, unavailable(err)
	}
	if version < 1 || attempts < 0 || checkpoint {
		return 0, 0, unavailable(nil)
	}
	return version, attempts, nil
}

func cleanupRoundReport(round lifecycleCleanupRound, report c.CleanupReport) error {
	scope := c.ScopeRef{Kind: c.ProjectScope, ProjectID: round.claim.project}
	if !report.Matches(c.OutboxParticipant, round.cause, scope) {
		return unavailable(nil)
	}
	d := report.Details()
	// This fixed D06 adapter has its own persistent scan and only aggregate
	// counts. No external cursor or invented resource references can advance it.
	if d.Checkpoint != nil || len(d.RemainingRefs) != 0 || d.State != c.CleanupCompleted && d.State != c.CleanupPending {
		return unavailable(nil)
	}
	return nil
}

func (st *lifecycleCleanupState) finish(ctx context.Context, round lifecycleCleanupRound, report c.CleanupReport, callErr error) error {
	if callErr == nil {
		if err := cleanupRoundReport(round, report); err != nil {
			return err
		}
	}
	validated := false
	result := st.store.WithinTx(ctx, round.claim.cause(), func(ctx context.Context, tx f.Tx) error {
		if err := st.store.AcquireAll(ctx, tx, []f.LockRequest{projectLock(round.claim.project, f.Exclusive)}); err != nil {
			return unavailable(err)
		}
		x, err := st.authority.lifecycleExecutor(ctx, tx, round.claim.project)
		if err != nil {
			return err
		}
		actual, err := loadLifecycleStopClaim(ctx, x, round.claim.operation)
		if err != nil {
			return err
		}
		if actual == nil || *actual != round.claim {
			return fault(f.ResourceBusy)
		}
		cause, r, replay, err := st.current(ctx, x, round.claim.project, round.claim.operation)
		if err != nil {
			return err
		}
		if replay || cause != round.cause || r.operation.Version != round.operationVersion {
			return fault(f.InvalidState)
		}
		rowVersion, _, err := cleanupOutboxRow(ctx, x, round.claim.operation)
		if err != nil {
			return err
		}
		if rowVersion != round.rowVersion {
			return fault(f.ResourceBusy)
		}
		if callErr == nil {
			if rowVersion == math.MaxInt64 {
				return fault(f.InvalidState)
			}
			version, err := nextVersion(r.operation.Version)
			if err != nil {
				return err
			}
			now, err := dbNow(ctx, x)
			if err != nil {
				return err
			}
			details := report.Details()
			tag, err := x.Exec(ctx, `UPDATE agenteam_project.lifecycle_participants SET cleanup_state=$3,safe_reason=NULLIF($4,''),safe_pending_refs='[]'::jsonb,next_attempt_at=NULL,version=version+1 WHERE operation_id=$1 AND participant_name='outbox' AND version=$2 AND cleanup_state IN ('required','pending') AND checkpoint IS NULL AND checkpoint_schema IS NULL`, round.claim.operation.String(), rowVersion, string(details.State), string(details.SafeReason))
			if err = affected(tag, err); err != nil {
				return err
			}
			stage := "outbox"
			if details.State == c.CleanupCompleted {
				stage = "audit"
			}
			tag, err = x.Exec(ctx, `UPDATE agenteam_project.lifecycle_operations SET cleanup_stage=$3,version=$4,updated_at=$5 WHERE id=$1 AND project_id=$2 AND state='cleaning' AND cleanup_stage='outbox' AND version=$6`, round.claim.operation.String(), round.claim.project.String(), stage, int64(version), now.Time(), int64(round.operationVersion))
			if err = affected(tag, err); err != nil {
				return err
			}
		}
		// Errors (including a completed report returned with an error) retire
		// only this original worker, preserving the prior canonical progress.
		tag, err := x.Exec(ctx, `UPDATE agenteam_project.work_claims SET phase='terminal' WHERE work_kind='lifecycle' AND work_id=$1 AND project_id=$2 AND process_id=$3 AND attempt_id=$4 AND fence=$5 AND phase='running'`, round.claim.operation.String(), round.claim.project.String(), round.claim.process.String(), round.claim.attempt, round.claim.fence)
		if err = affected(tag, err); err != nil {
			return err
		}
		validated = true
		return nil
	})
	if err := commitError(result); err != nil {
		return err
	}
	if !validated {
		return unavailable(nil)
	}
	return portError(ctx.Err())
}
