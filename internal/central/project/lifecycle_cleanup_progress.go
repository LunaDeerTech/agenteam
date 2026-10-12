package project

import (
	"context"
	"math"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	c "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

func cleanupParticipantRow(ctx context.Context, x postgres.SQLExecutor, round lifecycleCleanupRound) (version, attempts int64, checkpoint *c.CleanupCheckpoint, err error) {
	var schema *int64
	var raw []byte
	err = x.QueryRow(ctx, `SELECT version,attempt_count,checkpoint_schema,checkpoint FROM agenteam_project.lifecycle_participants WHERE operation_id=$1 AND participant_name=$2`, round.claim.operation.String(), string(round.participant)).Scan(&version, &attempts, &schema, &raw)
	if err != nil {
		return 0, 0, nil, unavailable(err)
	}
	if version < 1 || attempts < 0 || (schema == nil) != (raw == nil) {
		return 0, 0, nil, unavailable(nil)
	}
	if schema != nil {
		value, err := c.NewCleanupCheckpoint(round.participant, round.cause, c.ScopeRef{Kind: c.ProjectScope, ProjectID: round.claim.project}, f.Version(*schema), raw)
		if err != nil {
			return 0, 0, nil, unavailable(err)
		}
		checkpoint = &value
	}
	if err := validateCleanupCheckpoint(round.participant, checkpoint, round.cause, round.claim.project); err != nil {
		return 0, 0, nil, err
	}
	return version, attempts, checkpoint, nil
}

func cleanupRoundReport(round lifecycleCleanupRound, report c.CleanupReport) error {
	scope := c.ScopeRef{Kind: c.ProjectScope, ProjectID: round.claim.project}
	if !report.Matches(round.participant, round.cause, scope) {
		return unavailable(nil)
	}
	d := report.Details()
	// Fixed ports expose progress without resource refs. Audit preserves its
	// diagnostic checkpoint; Skills always wraps its child progress as pending.
	if len(d.RemainingRefs) != 0 || d.State != c.CleanupCompleted && d.State != c.CleanupPending || (round.participant == c.AuditParticipant || round.participant == c.SecretParticipant || round.participant == c.ArtifactObjectParticipant) && d.Checkpoint == nil {
		return unavailable(nil)
	}
	if round.participant == c.SchedulerParticipant {
		if err := schedulerCleanupReport(round, report); err != nil {
			return err
		}
	}
	if round.participant == c.SkillsParticipant && round.terminalStep {
		if d.State != c.CleanupCompleted || d.SafeReason != "" || d.Checkpoint == nil || d.Checkpoint.Schema() != 6 {
			return unavailable(nil)
		}
	} else if round.participant == c.SkillsParticipant && (d.State != c.CleanupPending || d.Checkpoint == nil || d.SafeReason != c.ReasonWorkPending) {
		return unavailable(nil)
	}
	return validateCleanupCheckpoint(round.participant, d.Checkpoint, round.cause, round.claim.project)
}

func (st *lifecycleCleanupState) finish(ctx context.Context, round lifecycleCleanupRound, report c.CleanupReport, callErr error) error {
	if callErr == nil {
		if err := cleanupRoundReport(round, report); err != nil {
			return err
		}
	}
	validated := false
	result := st.store.WithinTx(ctx, round.claim.cause(), func(ctx context.Context, tx f.Tx) error {
		locks := []f.LockRequest{projectLock(round.claim.project, f.Exclusive)}
		if round.participant == c.SchedulerParticipant {
			locks = append(locks, schedulerLock(round.claim.project))
		}
		if err := st.store.AcquireAll(ctx, tx, locks); err != nil {
			return unavailable(err)
		}
		if err := st.finishInTx(ctx, tx, round, report, callErr); err != nil {
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

// finishInTx is also used by Agent retirement: canonical deletion, all owned
// reference removals and this exact progress checkpoint share one caller Tx.
func (st *lifecycleCleanupState) finishInTx(ctx context.Context, tx f.Tx, round lifecycleCleanupRound, report c.CleanupReport, callErr error) error {
	if callErr == nil {
		if err := cleanupRoundReport(round, report); err != nil {
			return err
		}
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
	cause, r, participant, replay, err := st.current(ctx, x, round.claim.project, round.claim.operation)
	if err != nil {
		return err
	}
	if replay || participant != round.participant || cause != round.cause || r.operation.Version != round.operationVersion {
		return fault(f.InvalidState)
	}
	rowVersion, _, _, err := cleanupParticipantRow(ctx, x, round)
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
		var schema any
		var checkpoint []byte
		if details.Checkpoint != nil {
			schema, checkpoint = int64(details.Checkpoint.Schema()), details.Checkpoint.Bytes()
		}
		tag, err := x.Exec(ctx, `UPDATE agenteam_project.lifecycle_participants SET cleanup_state=$3,safe_reason=NULLIF($4,''),safe_pending_refs='[]'::jsonb,next_attempt_at=NULL,checkpoint_schema=$6,checkpoint=$7,version=version+1 WHERE operation_id=$1 AND participant_name=$5 AND version=$2 AND cleanup_state IN ('required','pending')`, round.claim.operation.String(), rowVersion, string(details.State), string(details.SafeReason), string(round.participant), schema, checkpoint)
		if err = affected(tag, err); err != nil {
			return err
		}
		stage, next, err := cleanupStages(round.participant)
		if err != nil {
			return err
		}
		previous := stage
		if details.State == c.CleanupCompleted {
			stage = next
		}
		if round.participant == c.ArtifactObjectParticipant && details.State == c.CleanupCompleted {
			complete, e := artifactCleanupCompletesDomains(r)
			if e != nil {
				return e
			}
			if complete {
				stage = "outbox"
			}
		}
		tag, err = x.Exec(ctx, `UPDATE agenteam_project.lifecycle_operations SET cleanup_stage=$3,version=$4,updated_at=$5 WHERE id=$1 AND project_id=$2 AND state='cleaning' AND cleanup_stage=$7 AND version=$6`, round.claim.operation.String(), round.claim.project.String(), stage, int64(version), now.Time(), int64(round.operationVersion), previous)
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
	return nil
}
