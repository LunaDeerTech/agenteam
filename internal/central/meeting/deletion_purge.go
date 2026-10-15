package meeting

import (
	"context"
	"encoding/json"
	"math"

	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/meeting/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
)

type deletionPurge struct {
	original deletionRecord
	version  f.Version
	intents  []intentRecord
}

// Unknown launches retain their original identity. Neither a missing
// Contribution pointer nor an empty runtime map proves that launch retired.
func loadDeletionIntents(ctx context.Context, x postgres.SQLExecutor, m c.MeetingID) ([]intentRecord, error) {
	rows, err := x.Query(ctx, `SELECT request_data,request_id,idempotency_key FROM agenteam_meeting.launch_intents WHERE meeting_id=$1 ORDER BY id`, m.String())
	if err != nil {
		return nil, portError(err)
	}
	var requests []ec.LaunchRequest
	for rows.Next() {
		var raw []byte
		var request, key string
		if err = rows.Scan(&raw, &request, &key); err != nil {
			rows.Close()
			return nil, portError(err)
		}
		var value ec.LaunchRequest
		if json.Unmarshal(raw, &value) != nil {
			rows.Close()
			return nil, fault(f.InternalError)
		}
		value.Meta.RequestID, err = f.ParseID[f.Request](request)
		value.Meta.IdempotencyKey = f.IdempotencyKey(key)
		if err != nil || value.Validate() != nil || value.Trigger.MeetingID != m.String() || value.Lineage.ContributionGeneration == nil || value.Lineage.ContributionAttempt == nil {
			rows.Close()
			return nil, fault(f.InternalError)
		}
		requests = append(requests, value)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, portError(err)
	}
	out := make([]intentRecord, 0, len(requests))
	for _, request := range requests {
		intent, err := loadIntentForRequest(ctx, x, request)
		if err != nil {
			return nil, err
		}
		if intent.execution == nil && intent.unknown {
			return nil, unknownSummary()
		}
		out = append(out, intent)
	}
	return out, nil
}

func sameDeletionIntents(a, b []intentRecord) bool {
	if len(a) != len(b) {
		return false
	}
	for n := range a {
		if a[n].id != b[n].id || a[n].requestDigest != b[n].requestDigest || a[n].inputID != b[n].inputID || a[n].inputDigest != b[n].inputDigest || a[n].unknown != b[n].unknown || a[n].association() != b[n].association() {
			return false
		}
	}
	return true
}

func deletionRetirementLocks(plan deletionPurge) ([]f.LockRequest, error) {
	command, err := commandIdentity(plan.original.project, plan.original.key)
	if err != nil {
		return nil, err
	}
	locks := []f.LockRequest{projectLock(plan.original.project, f.Exclusive), meetingLock(plan.original.meeting, f.Exclusive), commandLock(command)}
	for _, intent := range plan.intents {
		if intent.execution == nil {
			continue
		}
		additional, err := ec.MeetingExecutionRetirementLocks(plan.original.project, intent.association())
		if err != nil {
			return nil, portError(err)
		}
		locks = append(locks, additional...)
	}
	return normalize(locks)
}

func requireDeletionOwnersSettled(ctx context.Context, x postgres.SQLExecutor, m c.MeetingID) error {
	var busy bool
	err := x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_meeting.contributions WHERE meeting_id=$1 AND kind='agent' AND status IN('pending','waiting_for_agent','running')) OR EXISTS(SELECT 1 FROM agenteam_meeting.decision_requests WHERE meeting_id=$1 AND status='pending') OR EXISTS(SELECT 1 FROM agenteam_meeting.summary_generations WHERE meeting_id=$1 AND (status NOT IN('published','cancelled') OR (retained_at IS NOT NULL AND released_at IS NULL))) OR EXISTS(SELECT 1 FROM agenteam_meeting.summary_model_calls WHERE meeting_id=$1 AND (returned_at IS NULL OR retired_at IS NULL OR terminal_version IS DISTINCT FROM 1))`, m.String()).Scan(&busy)
	if err != nil {
		return portError(err)
	}
	if busy {
		return fault(f.ResourceBusy)
	}
	return nil
}

func (r *Runtime) advanceDeletion(ctx context.Context, p i.ProjectID, m c.MeetingID) (AdvanceResult, error) {
	out := AdvanceResult{MeetingID: m, Phase: "deleting"}
	if r.deps.DeletionAuthority == nil || r.deps.DeletionAuthority.original != r.deps.Authority || nilPort(r.deps.DeletionProjects) || nilPort(r.deps.Retirement) || nilPort(r.deps.Audit) || r.deps.LifecycleEvents == nil || !sameStore(r.store, r.deps.LifecycleEvents.authority.store) {
		return out, fault(f.DependencyUnbound)
	}
	// Discovery supplies only the immutable key for the complete lock union.
	// Every cleanup transaction re-reads it through the private Project gate.
	original, err := loadDeletion(ctx, r.store, p, m)
	if err != nil {
		return out, err
	}
	if original.phase != c.DeletionCancelling {
		return out, fault(f.ConfirmationStale)
	}
	owner := &deletionRuntimeContext{runtime: r, original: original, active: true}
	defer func() { owner.active = false }()
	ctx = context.WithValue(ctx, deletionRuntimeKey{}, owner)
	cleanup, err := r.planDeletionCleanup(ctx, p, m)
	if err != nil {
		return out, err
	}
	if len(cleanup.intents) != 0 || len(cleanup.summaries) != 0 {
		return out, r.cancelDeletionOwners(ctx, p, m, cleanup)
	}
	var plan deletionPurge
	err = r.transaction(ctx, p, m, "discover-purge", func(ctx context.Context, _ f.Tx, x postgres.SQLExecutor) error {
		var err error
		plan.original, err = loadDeletion(ctx, x, p, m)
		if err != nil {
			return err
		}
		if plan.original.phase != c.DeletionCancelling {
			return fault(f.ConfirmationStale)
		}
		session, err := loadSession(ctx, x, p, m)
		if err != nil {
			return err
		}
		if !session.Deleting || session.Version < plan.original.accepted || session.Version == f.Version(math.MaxInt64) {
			return fault(f.ConfirmationStale)
		}
		plan.version = session.Version
		if err := requireDeletionOwnersSettled(ctx, x, m); err != nil {
			return err
		}
		plan.intents, err = loadDeletionIntents(ctx, x, m)
		return err
	})
	if err != nil {
		return out, err
	}
	locks, err := deletionRetirementLocks(plan)
	if err != nil {
		return out, err
	}
	scope, err := i.InProject(p)
	if err != nil {
		return out, portError(err)
	}
	actor, err := r.deps.Actor.Actor(plan.original.id.String(), scope)
	if err != nil {
		return out, portError(err)
	}
	completed := plan.original
	completed.phase = c.DeletionCompleted
	eventPlan, err := r.deps.LifecycleEvents.prepare(ctx, actor, p, m, plan.version+1, c.DeletedName, "deleted", now(), locks)
	if err != nil {
		return out, err
	}
	cause, err := readCause("meeting.runtime.purge")
	if err != nil {
		return out, err
	}
	err = commitError(r.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := r.store.AcquireAll(ctx, tx, eventPlan.locks); err != nil {
			return portError(err)
		}
		if err := r.requireRuntimeProjectInTx(ctx, tx, p, m); err != nil {
			return err
		}
		if err := r.deps.DeletionAuthority.requireCurrentInTx(ctx, tx, r.store); err != nil {
			return err
		}
		x, err := r.store.InTx(tx)
		if err != nil {
			return portError(err)
		}
		current, err := loadDeletion(ctx, x, p, m)
		if err != nil {
			return err
		}
		if current != plan.original {
			return fault(f.ConfirmationStale)
		}
		session, err := loadSession(ctx, x, p, m)
		if err != nil {
			return err
		}
		if !session.Deleting || session.Version != plan.version {
			return fault(f.ConfirmationStale)
		}
		if err := requireDeletionOwnersSettled(ctx, x, m); err != nil {
			return err
		}
		intents, err := loadDeletionIntents(ctx, x, m)
		if err != nil {
			return err
		}
		if !sameDeletionIntents(plan.intents, intents) {
			return fault(f.ConfirmationStale)
		}
		for _, intent := range intents {
			if intent.execution == nil {
				continue
			}
			w := &deletionWitness{authority: r.deps.DeletionAuthority, tx: tx, original: current, intent: intent, active: true}
			err = r.deps.Retirement.RequireMeetingExecutionRetiredInTx(context.WithValue(ctx, deletionWitnessKey{}, w), tx, p, intent.association())
			w.active = false
			if err != nil {
				return portError(err)
			}
		}
		deleted, err := purgeMeetingRows(ctx, x, m)
		if err != nil {
			return err
		}
		tag, err := x.Exec(ctx, `UPDATE agenteam_meeting.deletion_requests SET phase='deleted',deleted_at=$2,deleted_records=$3,error_code='' WHERE id=$1 AND phase='cancelling'`, current.id.String(), now(), deleted)
		if err != nil {
			return portError(err)
		}
		if tag.RowsAffected() != 1 {
			return fault(f.ConfirmationStale)
		}
		// The closure exists only after every original owner passed in this
		// transaction. Outbox cannot manufacture it from a technical Actor.
		active := true
		defer func() { active = false }()
		eventPlan.sourceCheck = func(ctx context.Context, borrowed f.Tx) error {
			if !active || borrowed != tx || actor.Details().CauseRef != current.id.String() {
				return fault(f.Forbidden)
			}
			if err := r.deps.DeletionAuthority.requireCurrentInTx(ctx, borrowed, r.store); err != nil {
				return err
			}
			return requireDeletionPostimage(ctx, x, completed, deleted)
		}
		if err := appendDeletionAudit(ctx, tx, r.deps.Audit, r.deps.DeletionAuthority, actor, completed, deleted, true); err != nil {
			return err
		}
		return r.deps.LifecycleEvents.appendInTx(ctx, tx, eventPlan)
	}))
	if err == nil {
		out.Phase = "deleted"
		invalidateMeeting(r.deps.Realtime, p, m)
	}
	return out, err
}

// The fixed statement list is deliberately limited to this domain. Original
// Execution, Tool, Approval, Audit and referenced resources remain independent.
var meetingPurgeStatements = []string{
	`DELETE FROM agenteam_meeting.message_references WHERE message_id IN(SELECT id FROM agenteam_meeting.messages WHERE meeting_id=$1)`,
	`DELETE FROM agenteam_meeting.summary_model_calls WHERE meeting_id=$1`,
	`DELETE FROM agenteam_meeting.summary_generations WHERE meeting_id=$1`,
	`DELETE FROM agenteam_meeting.timeline_items WHERE meeting_id=$1`,
	`DELETE FROM agenteam_meeting.decision_requests WHERE meeting_id=$1`,
	`DELETE FROM agenteam_meeting.approval_references WHERE meeting_id=$1`,
	`DELETE FROM agenteam_meeting.contribution_executions WHERE meeting_id=$1`,
	`DELETE FROM agenteam_meeting.launch_intents WHERE meeting_id=$1`,
	`DELETE FROM agenteam_meeting.contributions WHERE meeting_id=$1`,
	`DELETE FROM agenteam_meeting.turns WHERE meeting_id=$1`,
	`DELETE FROM agenteam_meeting.messages WHERE meeting_id=$1`,
	`DELETE FROM agenteam_meeting.inputs WHERE meeting_id=$1`,
	`DELETE FROM agenteam_meeting.meeting_references WHERE meeting_id=$1`,
	`DELETE FROM agenteam_meeting.proposal_sources WHERE meeting_id=$1`,
	`DELETE FROM agenteam_meeting.participants WHERE meeting_id=$1`,
	`DELETE FROM agenteam_meeting.sessions WHERE id=$1 AND deleting`,
}

func purgeMeetingRows(ctx context.Context, x postgres.SQLExecutor, m c.MeetingID) (int64, error) {
	var total int64
	for n, statement := range meetingPurgeStatements {
		tag, err := x.Exec(ctx, statement, m.String())
		if err != nil {
			return 0, portError(err)
		}
		count := tag.RowsAffected()
		if count < 0 || total > math.MaxInt64-count || n == len(meetingPurgeStatements)-1 && count != 1 {
			return 0, fault(f.InvalidState)
		}
		total += count
	}
	return total, nil
}

func requireDeletionPostimage(ctx context.Context, x postgres.SQLExecutor, original deletionRecord, deleted int64) error {
	if original.phase != c.DeletionCompleted || deleted < 1 {
		return fault(f.Forbidden)
	}
	var exact bool
	err := x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_meeting.deletion_requests WHERE id=$1 AND project_id=$2 AND meeting_id=$3 AND actor_id=$4 AND accepted_version=$5 AND phase='deleted' AND deleted_at IS NOT NULL AND deleted_records=$6) AND NOT EXISTS(SELECT 1 FROM agenteam_meeting.sessions WHERE id=$3)`, original.id.String(), original.project.String(), original.meeting.String(), original.actor.String(), int64(original.accepted), deleted).Scan(&exact)
	if err != nil {
		return portError(err)
	}
	if !exact {
		return fault(f.Forbidden)
	}
	return ctx.Err()
}
