package scheduler

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	wc "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
	"github.com/jackc/pgx/v5"
)

const dispatchColumns = `id::text,project_id::text,sprint_id::text,task_id::text,agent_id::text,
 launch_request,launch_digest,idempotency_key,request_id::text,status,launch_outcome,version,
 claim_guard,claim_source_sprint_id::text,claim_source_state,claim_source_priority,execution_id::text,attempt_count,next_retry_at,created_at,updated_at,busy_attempt,skip_reason,skipped_at,final_attempt,failure_reason,failure_code,failure_occurred_at,failed_at,retry_policy,retry_policy_digest,temporary_attempt,temporary_reason,temporary_code,temporary_occurred_at`

func scanDispatch(row postgres.Row) (*dispatchRecord, error) {
	var id, p, s, t, a, key, requestID, status, outcome, digest string
	var launchRaw, guardRaw []byte
	var execution *string
	var sourceSprint, sourceState, sourcePriority *string
	var version, attempts int64
	var retry *time.Time
	var created, updated time.Time
	var busy *int64
	var reason *string
	var skipped *time.Time
	var finalAttempt *int64
	var failureReason, failureCode *string
	var occurred, failed *time.Time
	var policyRaw []byte
	var policyDigest *string
	var temporaryAttempt *int64
	var temporaryReason, temporaryCode *string
	var temporaryOccurred *time.Time
	if err := row.Scan(&id, &p, &s, &t, &a, &launchRaw, &digest, &key, &requestID, &status, &outcome, &version, &guardRaw, &sourceSprint, &sourceState, &sourcePriority, &execution, &attempts, &retry, &created, &updated, &busy, &reason, &skipped, &finalAttempt, &failureReason, &failureCode, &occurred, &failed, &policyRaw, &policyDigest, &temporaryAttempt, &temporaryReason, &temporaryCode, &temporaryOccurred); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, portError(err)
	}
	r := &dispatchRecord{sprint: s, task: t, status: Status(status), outcome: LaunchOutcome(outcome), version: f.Version(version), digest: f.Digest(digest), attempts: attempts}
	var err error
	if r.retryPolicy, err = decodeRetryPolicy(policyRaw, policyDigest); err != nil {
		return nil, err
	}
	if r.id, err = f.ParseID[DispatchIdentity](id); err != nil {
		return nil, unavailable(nil)
	}
	if r.project, err = f.ParseID[i.Project](p); err != nil {
		return nil, unavailable(nil)
	}
	if r.agent, err = f.ParseID[i.Agent](a); err != nil {
		return nil, unavailable(nil)
	}
	if !validID(s) || !validID(t) || r.version.Validate() != nil || r.digest.Validate() != nil || !r.status.Valid() || !r.outcome.Valid() || attempts < 0 {
		return nil, unavailable(nil)
	}
	if err = decodeExact(launchRaw, &r.launch, 262144); err != nil {
		return nil, err
	}
	if r.launch.Meta.RequestID, err = f.ParseID[f.Request](requestID); err != nil {
		return nil, unavailable(nil)
	}
	r.launch.Meta.IdempotencyKey = f.IdempotencyKey(key)
	actual, err := r.launch.Digest()
	if err != nil || actual != r.digest || r.launch.ProjectID != r.project || r.launch.AgentID != r.agent || r.launch.Trigger.Kind != "task" || r.launch.Trigger.TaskID != r.task || r.launch.Lineage.DispatchID != id || r.launch.Meta.IdempotencyKey != launchKey(r.id) {
		return nil, unavailable(nil)
	}
	if guardRaw != nil {
		g := new(ClaimGuard)
		if err = decodeExact(guardRaw, g, 4096); err != nil {
			return nil, err
		}
		if !g.valid() || g.TaskID != t || g.SourceSprintID != s || g.SourceAssigneeID != r.agent || r.launch.Purpose != "task/work" || sourceSprint == nil || sourceState == nil || sourcePriority == nil || *sourceSprint != g.SourceSprintID || *sourceState != g.SourceState || *sourcePriority != g.SourcePriority {
			return nil, unavailable(nil)
		}
		r.guard = g
	} else if sourceSprint != nil || sourceState != nil || sourcePriority != nil {
		return nil, unavailable(nil)
	}
	if execution != nil {
		e, err := f.ParseID[i.Execution](*execution)
		if err != nil {
			return nil, unavailable(nil)
		}
		r.execution = &e
	}
	if (r.status == Launched) != (r.execution != nil) || (r.status == Launched) != (r.outcome == Created) ||
		(r.status == Failed || r.status == Skipped) && r.outcome != KnownNotCreated ||
		r.status == Pending && r.outcome == Created ||
		(r.outcome == NotSent) != (attempts == 0) || r.status != Pending && retry != nil || r.outcome != KnownNotCreated && retry != nil {
		return nil, unavailable(nil)
	}
	if created.IsZero() || updated.Before(created) || created.Nanosecond()%1000 != 0 || updated.Nanosecond()%1000 != 0 {
		return nil, unavailable(nil)
	}
	if r.createdAt, err = f.NewInstant(created); err != nil {
		return nil, unavailable(nil)
	}
	if r.updatedAt, err = f.NewInstant(updated); err != nil {
		return nil, unavailable(nil)
	}
	if retry != nil {
		v, err := f.NewInstant(*retry)
		if err != nil || retry.Before(updated) || retry.Nanosecond()%1000 != 0 {
			return nil, unavailable(nil)
		}
		r.nextRetry = &v
	}
	// Historical rows have all three NULL fields. Never infer AgentBusy from
	// their generic known_not_created outcome. A new marker is attempt-bound.
	if busy == nil {
		if reason != nil || skipped != nil {
			return nil, unavailable(nil)
		}
	} else {
		if *busy <= 0 || *busy != attempts || r.outcome != KnownNotCreated || r.execution != nil || retry != nil {
			return nil, unavailable(nil)
		}
		r.busyAttempt = *busy
		switch r.status {
		case Pending:
			if reason != nil || skipped != nil {
				return nil, unavailable(nil)
			}
		case Skipped:
			if reason == nil || *reason != "agent_busy" || skipped == nil || !skipped.Equal(updated) {
				return nil, unavailable(nil)
			}
			v, err := f.NewInstant(*skipped)
			if err != nil || skipped.Nanosecond()%1000 != 0 {
				return nil, unavailable(nil)
			}
			r.skipReason, r.skippedAt = *reason, &v
		default:
			return nil, unavailable(nil)
		}
	}
	if temporaryAttempt == nil {
		if temporaryReason != nil || temporaryCode != nil || temporaryOccurred != nil {
			return nil, unavailable(nil)
		}
	} else {
		if temporaryReason == nil || temporaryCode == nil || temporaryOccurred == nil || temporaryOccurred.IsZero() || temporaryOccurred.Nanosecond()%1000 != 0 {
			return nil, unavailable(nil)
		}
		r.temporaryAttempt, r.temporaryReason, r.temporaryCode = *temporaryAttempt, ec.LaunchTemporaryReason(*temporaryReason), f.Code(*temporaryCode)
		at, err := f.NewInstant(*temporaryOccurred)
		if err != nil {
			return nil, unavailable(nil)
		}
		r.temporaryOccurredAt = &at
	}
	// Old generic known-not-created rows have no classification. A missing
	// marker never becomes final failure, even if a safe code looks familiar.
	if finalAttempt == nil {
		if failureReason != nil || failureCode != nil || occurred != nil || failed != nil {
			return nil, unavailable(nil)
		}
	} else {
		if failureReason == nil || failureCode == nil || occurred == nil || occurred.IsZero() || occurred.Nanosecond()%1000 != 0 || occurred.Before(created) || occurred.After(updated) || busy != nil || reason != nil || skipped != nil {
			return nil, unavailable(nil)
		}
		r.finalAttempt, r.failureReason, r.failureCode = *finalAttempt, wc.TaskLaunchFailureReason(*failureReason), f.Code(*failureCode)
		at, err := f.NewInstant(*occurred)
		if err != nil {
			return nil, unavailable(nil)
		}
		r.failureOccurredAt = &at
		if failed != nil {
			at, err := f.NewInstant(*failed)
			if err != nil || failed.Nanosecond()%1000 != 0 || !failed.Equal(updated) {
				return nil, unavailable(nil)
			}
			r.failedAt = &at
		}
		if !pendingFinalFailure(r) && !completedFinalFailure(r) {
			return nil, unavailable(nil)
		}
	}
	if err := validateRetryRecord(r); err != nil {
		return nil, err
	}
	return r, nil
}

func loadDispatch(ctx context.Context, x postgres.SQLExecutor, project i.ProjectID, id DispatchID) (*dispatchRecord, error) {
	return scanDispatch(x.QueryRow(ctx, `SELECT `+dispatchColumns+` FROM agenteam_scheduler.dispatches WHERE project_id=$1 AND id=$2`, project.String(), id.String()))
}

// Only the coordinator invokes this after the original Work applied-proof is
// checked. A row/DTO is not an alternative proof and never mints an intent.
func insertDispatch(ctx context.Context, x postgres.SQLExecutor, r *dispatchRecord) error {
	launch, err := encodeLaunch(r.launch)
	if err != nil {
		return err
	}
	policyRaw, policyDigest, err := encodeRetryPolicy(r.retryPolicy)
	if err != nil {
		return err
	}
	var guard []byte
	var sourceSprint, sourceState, sourcePriority any
	if r.guard != nil {
		guard, err = json.Marshal(r.guard)
		if err != nil {
			return invalid()
		}
	}
	if r.guard != nil {
		sourceSprint, sourceState, sourcePriority = r.guard.SourceSprintID, r.guard.SourceState, r.guard.SourcePriority
	}
	tag, err := x.Exec(ctx, `INSERT INTO agenteam_scheduler.dispatches
 (id,project_id,sprint_id,task_id,agent_id,launch_request,launch_digest,idempotency_key,request_id,status,launch_outcome,version,claim_guard,claim_source_sprint_id,claim_source_state,claim_source_priority,attempt_count,created_at,updated_at,retry_policy,retry_policy_digest)
 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,'pending','not_sent',1,$10,$11,$12,$13,0,$14,$14,$15,$16)`,
		r.id.String(), r.project.String(), r.sprint, r.task, r.agent.String(), launch, string(r.digest), string(r.launch.Meta.IdempotencyKey), r.launch.Meta.RequestID.String(), guard, sourceSprint, sourceState, sourcePriority, r.createdAt.Time(), policyRaw, policyDigest)
	if err != nil {
		return portError(err)
	}
	if tag.RowsAffected() != 1 {
		return unavailable(nil)
	}
	return ctx.Err()
}

// Each durable update is guarded by the original version and SQL's immutable
// identity/transition trigger. It never replaces fixed launch input or guard.
func updateDispatch(ctx context.Context, x postgres.SQLExecutor, r *dispatchRecord, previous f.Version) error {
	var execution any
	if r.execution != nil {
		execution = r.execution.String()
	}
	var retry any
	if r.nextRetry != nil {
		retry = r.nextRetry.Time()
	}
	var busy, reason, skipped any
	if r.busyAttempt > 0 {
		busy = r.busyAttempt
	}
	if r.skipReason != "" {
		reason = r.skipReason
	}
	if r.skippedAt != nil {
		skipped = r.skippedAt.Time()
	}
	var finalAttempt, failureReason, failureCode, occurred, failed any
	if r.finalAttempt > 0 {
		finalAttempt = r.finalAttempt
	}
	if r.failureReason != "" {
		failureReason = string(r.failureReason)
	}
	if r.failureCode != "" {
		failureCode = string(r.failureCode)
	}
	if r.failureOccurredAt != nil {
		occurred = r.failureOccurredAt.Time()
	}
	if r.failedAt != nil {
		failed = r.failedAt.Time()
	}
	var temporaryAttempt, temporaryReason, temporaryCode, temporaryOccurred any
	if r.temporaryAttempt > 0 {
		temporaryAttempt, temporaryReason, temporaryCode = r.temporaryAttempt, string(r.temporaryReason), string(r.temporaryCode)
	}
	if r.temporaryOccurredAt != nil {
		temporaryOccurred = r.temporaryOccurredAt.Time()
	}
	tag, err := x.Exec(ctx, `UPDATE agenteam_scheduler.dispatches SET status=$3,launch_outcome=$4,execution_id=$5,attempt_count=$6,next_retry_at=$7,version=$8,updated_at=$9,busy_attempt=$11,skip_reason=$12,skipped_at=$13,final_attempt=$14,failure_reason=$15,failure_code=$16,failure_occurred_at=$17,failed_at=$18,temporary_attempt=$19,temporary_reason=$20,temporary_code=$21,temporary_occurred_at=$22
 WHERE project_id=$1 AND id=$2 AND version=$10`, r.project.String(), r.id.String(), string(r.status), string(r.outcome), execution, r.attempts, retry, int64(r.version), r.updatedAt.Time(), int64(previous), busy, reason, skipped, finalAttempt, failureReason, failureCode, occurred, failed, temporaryAttempt, temporaryReason, temporaryCode, temporaryOccurred)
	if err != nil {
		return portError(err)
	}
	if tag.RowsAffected() != 1 {
		return fault(f.VersionConflict)
	}
	return ctx.Err()
}

func lookupKey(r *dispatchRecord) ec.LaunchLookupKey {
	return ec.LaunchLookupKey{ProjectID: r.project, AgentID: r.agent, IdempotencyKey: r.launch.Meta.IdempotencyKey}
}
