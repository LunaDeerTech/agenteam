//go:build integration

package projectvariable_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"

	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/scheduler"
	wc "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
	"github.com/jackc/pgx/v5/pgconn"
)

// Both attempts use actual PostgreSQL advisory-lock contention. A temporary
// receipt is never seeded, and exhaustion never substitutes a different
// permanent Task-provider rejection for the original lock-timeout proof.
func TestSchedulerBoundedRetry(t *testing.T) {
	t.Run("temporary-due-original-key-created", func(t *testing.T) {
		x := newSchedulerBoundedRetryFixture(t)
		taskBefore := x.v.databaseSnapshot(t)
		release := holdSchedulerRetryAgent(t, x.v, x.agentKey)
		first := x.realTimeout(t, false)
		release()
		x.requireTemporary(t, first, 1, true)
		x.requireNoExecution(t, 1)
		waitSchedulerRetryDue(t, first)
		launched, err := x.handoff.RetryDue(ctxFor(t), x.request.ProjectID, x.dispatch)
		if err != nil {
			t.Fatal("real due retry", err)
		}
		x.requireAssociated(t, launched)
		if taskBefore != x.v.databaseSnapshot(t) {
			t.Fatal("retry changed claimed Task/history/activity")
		}
		stable := x.snapshot(t)
		replay, err := x.handoff.LaunchOnce(ctxFor(t), x.request.ProjectID, x.dispatch)
		if err != nil || !reflect.DeepEqual(replay.Summary(), launched.Summary()) {
			t.Fatal("original Dispatch replay lost the associated result", err)
		}
		looked, err := x.handoff.Lookup(ctxFor(t), x.request.ProjectID, x.dispatch)
		if err != nil || !reflect.DeepEqual(looked.Summary(), launched.Summary()) || stable != x.snapshot(t) {
			t.Fatal("original-key lookup/replay changed committed retry facts", err)
		}
		x.requireAssociated(t, looked)
	})
	t.Run("temporary-exhaustion-technical-blocker", func(t *testing.T) {
		x := newSchedulerBoundedRetryFixture(t)
		taskBefore := x.v.databaseSnapshot(t)
		// The same holder stays live until BOTH original Launch calls return.
		release := holdSchedulerRetryAgent(t, x.v, x.agentKey)
		first := x.realTimeout(t, false)
		x.requireTemporary(t, first, 1, true)
		waitSchedulerRetryDue(t, first)
		exhausted := x.realTimeout(t, true)
		release()
		x.requireTemporary(t, exhausted, 2, false)
		x.requireNoExecution(t, 2)
		if taskBefore != x.v.databaseSnapshot(t) {
			t.Fatal("temporary attempts changed the Task before finalization")
		}
		x.rejected = exhausted.Summary()
		x.finalizer = newSchedulerFailureFinalizer(t, x.v)
		before := x.currentTask(t)
		settled, err := x.finalizer.FinalizeLaunchFailure(ctxFor(t), x.request.ProjectID, x.dispatch)
		if err != nil {
			t.Fatal("actual exhausted Launch finalization", err)
		}
		x.requireExhausted(t, settled, before)
		stable := x.snapshot(t)
		replay, err := x.finalizer.FinalizeLaunchFailure(ctxFor(t), x.request.ProjectID, x.dispatch)
		if err != nil || !reflect.DeepEqual(replay.Summary(), settled.Summary()) {
			t.Fatal("exhausted finalization replay", err)
		}
		looked, err := x.finalizer.Lookup(ctxFor(t), x.request.ProjectID, x.dispatch)
		if err != nil || !reflect.DeepEqual(looked.Summary(), settled.Summary()) || stable != x.snapshot(t) {
			t.Fatal("exhausted lookup/replay duplicated Work or Dispatch facts", err)
		}
		x.requireNoExecution(t, 2)
	})
}

type schedulerBoundedRetryFixture struct {
	*schedulerFailureFixture // Reuse only current reads and full snapshots.
	policy                   scheduler.LaunchRetryPolicy
	handoff                  *scheduler.LaunchHandoff
	agentKey                 f.LockKey
}

func newSchedulerBoundedRetryFixture(t *testing.T) *schedulerBoundedRetryFixture {
	t.Helper()
	config := schedulerRetryConfig(t, "2", "100ms", "1s")
	policy, configured := config.SchedulerLaunchRetryPolicy()
	if !configured || policy.Validate() != nil || policy.MaxAttempts() != 2 {
		t.Fatal("explicit bounded fixture policy")
	}
	v := prepareSchedulerClaimTask(t)
	startSchedulerSprint(t, v)
	claims := newSchedulerClaimCoordinator(t, v, &policy)
	claim, launchPolicy := schedulerClaimRequest(t, v)
	dispatch, err := claims.ClaimTask(ctxFor(t), claim, launchPolicy)
	if err != nil || dispatch.Summary().Status != scheduler.Pending || dispatch.Summary().AttemptCount != 0 {
		t.Fatal("real bound Claim", err)
	}
	requireStoredRetryPolicy(t, v, dispatch, &policy)
	request, err := dispatch.LaunchRequest()
	if err != nil || request.Validate() != nil || len(request.Policy.AllowedResourceConstraints) != 0 {
		t.Fatal("original supported Launch request", err)
	}
	handoff, counter := newSchedulerLaunchHandoff(t, v, true)
	agentKey, err := f.AgentLock(request.AgentID.String())
	if err != nil {
		t.Fatal(err)
	}
	return &schedulerBoundedRetryFixture{
		schedulerFailureFixture: &schedulerFailureFixture{v: v, dispatch: dispatch.Summary().ID, request: request, launcher: counter},
		policy:                  policy, handoff: handoff, agentKey: agentKey,
	}
}

// Observe the actual final AcquireAll and original Store return. Both hooks
// return the real implementation's values; neither injects a Fault nor alters
// the SQL operation, transaction outcome or synchronous caller's error.
func (x *schedulerBoundedRetryFixture) realTimeout(t *testing.T, retry bool) scheduler.Dispatch {
	t.Helper()
	command, err := x.request.Command()
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var finalAcquire bool
	var physical []f.CommitResult
	store := x.v.base.tracked
	store.mu.Lock()
	store.beforeLocks = func(_ context.Context, _ f.Tx, locks []f.LockRequest) error {
		for _, lock := range locks {
			if lock.Key.Canonical() == x.agentKey.Canonical() && lock.Mode == f.Exclusive {
				mu.Lock()
				finalAcquire = true
				mu.Unlock()
			}
		}
		return nil
	}
	store.afterResult = func(cause f.TransactionCause, result f.CommitResult) {
		mu.Lock()
		defer mu.Unlock()
		if finalAcquire && cause.Kind() == f.CommandsCause && cause.Details().Primary.Canonical() == command.Canonical() {
			physical = append(physical, result)
		}
	}
	store.mu.Unlock()
	clear := func() {
		store.mu.Lock()
		store.beforeLocks, store.afterResult = nil, nil
		store.mu.Unlock()
	}
	defer clear()
	var rejected scheduler.Dispatch
	var launchErr error
	if retry {
		rejected, launchErr = x.handoff.RetryDue(ctxFor(t), x.request.ProjectID, x.dispatch)
	} else {
		rejected, launchErr = x.handoff.LaunchOnce(ctxFor(t), x.request.ProjectID, x.dispatch)
	}
	clear()
	mu.Lock()
	seen, results := finalAcquire, append([]f.CommitResult(nil), physical...)
	mu.Unlock()
	var pgError *pgconn.PgError
	if !seen || len(results) != 1 || results[0].State() != f.NotCommitted || !errors.As(results[0].Fault(), &pgError) || pgError.Code != "55P03" {
		t.Fatal("original final AcquireAll did not physically roll back PostgreSQL 55P03")
	}
	reason, exact := ec.MatchLaunchTemporaryRejection(launchErr, x.request)
	if launchErr == nil || !exact || reason != ec.LaunchTemporaryLockTimeout || errors.Is(launchErr, context.Canceled) || errors.Is(launchErr, context.DeadlineExceeded) {
		t.Fatal("real synchronous rejection lost its request-bound temporary proof")
	}
	return rejected
}

func (x *schedulerBoundedRetryFixture) requireTemporary(t *testing.T, got scheduler.Dispatch, attempt int64, due bool) {
	t.Helper()
	s := got.Summary()
	state, retry := got.RetryState()
	if !retry || s.ID != x.dispatch || s.Status != scheduler.Pending || s.LaunchOutcome != scheduler.KnownNotCreated || s.ExecutionID != nil || s.AttemptCount != attempt || state.Attempt != attempt || state.Reason != ec.LaunchTemporaryLockTimeout || state.Code != f.InternalError {
		t.Fatal("original current-attempt temporary checkpoint is incomplete")
	}
	request, err := got.LaunchRequest()
	if err != nil || !reflect.DeepEqual(request, x.request) {
		t.Fatal("retry checkpoint changed the original complete Launch request", err)
	}
	requireStoredRetryPolicy(t, x.v, got, &x.policy)
	if due {
		delay, allowed, err := x.policy.NextDelay(attempt)
		if err != nil || !allowed || state.NextRetryAt == nil || s.FailureReason != "" || s.FailureOccurredAt != nil {
			t.Fatal("retryable attempt has no bound deadline or has premature finality", err)
		}
		minimum := state.OccurredAt.Time().Add(delay)
		if state.NextRetryAt.Time().Before(minimum) || !state.NextRetryAt.Time().Before(minimum.Add(time.Microsecond)) {
			t.Fatal("stored due is not original checkpoint plus bound delay rounded up to microseconds")
		}
	} else if state.NextRetryAt != nil || s.FailureReason != wc.TaskLaunchFailureRetryExhausted || s.FailureCode != f.InternalError || s.FailureOccurredAt == nil || !s.FailureOccurredAt.Time().Equal(state.OccurredAt.Time()) {
		t.Fatal("max-attempt rejection did not establish its exact exhausted marker")
	}
	var matched bool
	var next *time.Time
	if state.NextRetryAt != nil {
		value := state.NextRetryAt.Time()
		next = &value
	}
	err = x.v.base.raw.QueryRow(ctxFor(t), `SELECT temporary_attempt=$3 AND temporary_reason=$4 AND temporary_code=$5
 AND temporary_occurred_at=$6::timestamptz AND next_retry_at IS NOT DISTINCT FROM $7::timestamptz
 AND attempt_count=$3 AND launch_outcome='known_not_created' AND status='pending'
 FROM agenteam_scheduler.dispatches WHERE project_id=$1::text AND id=$2::text`,
		x.request.ProjectID.String(), x.dispatch.String(), attempt, string(state.Reason), string(state.Code), state.OccurredAt.Time(), next).Scan(&matched)
	if err != nil || !matched {
		t.Fatal("public retry receipt does not match the actual stored attempt and deadline", err)
	}
}

func waitSchedulerRetryDue(t *testing.T, dispatch scheduler.Dispatch) {
	t.Helper()
	state, ok := dispatch.RetryState()
	if !ok || state.NextRetryAt == nil {
		t.Fatal("no actual persisted retry deadline")
	}
	wait := time.Until(state.NextRetryAt.Time())
	if wait > 2*time.Second {
		t.Fatal("fixture retry deadline exceeds its explicit bounded policy")
	}
	timer := time.NewTimer(max(wait, 0))
	defer timer.Stop()
	ctx := ctxFor(t)
	select {
	case <-timer.C:
	case <-ctx.Done():
		t.Fatal("original test context ended before stored retry deadline")
	}
}

func (x *schedulerBoundedRetryFixture) requireNoExecution(t *testing.T, attempts int) {
	t.Helper()
	launches, _, created := x.launcher.observed()
	var executions, slots int64
	err := x.v.base.raw.QueryRow(ctxFor(t), `SELECT
 (SELECT count(*) FROM agenteam_execution.executions WHERE project_id=$1::text),
 (SELECT count(*) FROM agenteam_execution.executions WHERE agent_id=$2::text AND status IN ('created','preparing','running','waiting'))`,
		x.request.ProjectID.String(), x.request.AgentID.String()).Scan(&executions, &slots)
	if err != nil || executions != 0 || slots != 0 || launches != attempts || created.ID.Validate() == nil {
		t.Fatal("temporary/exhausted attempts created an Execution or exceeded the original call count", err)
	}
}

func (x *schedulerBoundedRetryFixture) requireAssociated(t *testing.T, got scheduler.Dispatch) {
	t.Helper()
	launches, _, created := x.launcher.observed()
	s := got.Summary()
	if launches != 2 || created.ID.Validate() != nil || created.Status != ec.Created || s.ID != x.dispatch || s.Status != scheduler.Launched || s.LaunchOutcome != scheduler.Created || s.AttemptCount != 2 || s.ExecutionID == nil || *s.ExecutionID != created.ID || s.FailureReason != "" {
		t.Fatal("retry did not associate exactly its second original Launch as created")
	}
	request, err := got.LaunchRequest()
	if err != nil || !reflect.DeepEqual(request, x.request) {
		t.Fatal("created retry changed original key/request/policy/lineage", err)
	}
	requireStoredRetryPolicy(t, x.v, got, &x.policy)
	raw, err := json.Marshal(x.request)
	if err != nil {
		t.Fatal(err)
	}
	digest, err := x.request.Digest()
	if err != nil {
		t.Fatal(err)
	}
	var executions, association int64
	err = x.v.base.raw.QueryRow(ctxFor(t), `SELECT
 (SELECT count(*) FROM agenteam_execution.executions WHERE project_id=$1::text),
 (SELECT count(*) FROM agenteam_scheduler.dispatches d JOIN agenteam_execution.executions e
 ON e.project_id=d.project_id AND e.id=d.execution_id AND e.agent_id=d.agent_id
 WHERE d.project_id=$1::text AND d.id=$2::text AND d.attempt_count=2 AND d.status='launched'
 AND d.launch_outcome='created' AND d.next_retry_at IS NULL AND d.final_attempt IS NULL
 AND d.launch_digest=$3 AND e.request_digest=d.launch_digest AND e.status='created'
 AND d.request_id=$4::text AND e.request_id=d.request_id AND d.idempotency_key=$5 AND e.idempotency_key=d.idempotency_key
 AND e.launch_request=$6::jsonb AND convert_from(d.launch_request,'UTF8')::jsonb=e.launch_request)`,
		x.request.ProjectID.String(), x.dispatch.String(), string(digest), x.request.Meta.RequestID.String(), x.request.Meta.IdempotencyKey.String(), raw).Scan(&executions, &association)
	if err != nil || executions != 1 || association != 1 {
		t.Fatal("retry duplicated Execution or lost persisted original Launch identity", err)
	}
	requireSchedulerLaunchObservation(t, x.v, x.request, created.ID, true)
}

func (x *schedulerBoundedRetryFixture) requireExhausted(t *testing.T, got scheduler.Dispatch, before wc.Task) {
	t.Helper()
	s := got.Summary()
	if s.ID != x.dispatch || s.Status != scheduler.Failed || s.Version != x.rejected.Version+1 || s.AttemptCount != 2 || s.FailedAt == nil || s.FailureReason != wc.TaskLaunchFailureRetryExhausted || s.FailureCode != f.InternalError || s.FailureOccurredAt == nil || !s.FailureOccurredAt.Time().Equal(x.rejected.FailureOccurredAt.Time()) {
		t.Fatal("exhausted finalization lost its original second-attempt facts")
	}
	current := x.currentTask(t)
	want := before.Clone()
	want.State, want.Version, want.UpdatedAt, want.ManualRank = wc.TaskStateBlocked, before.Version+1, current.UpdatedAt, current.ManualRank
	if !bytes.Equal(jsonBytes(t, want), jsonBytes(t, current)) {
		t.Fatal("exhausted finalization changed current Task fields beyond its transition")
	}
	var facts, blockers, histories, workResults, allBlockers int64
	err := x.v.base.raw.QueryRow(ctxFor(t), `SELECT
 (SELECT count(*) FROM agenteam_work.task_launch_failures c
 JOIN agenteam_scheduler.dispatches d ON d.project_id=c.project_id::text AND d.id=c.id::text
 JOIN agenteam_work.tasks t ON t.project_id=c.project_id AND t.id=c.task_id
 JOIN agenteam_outbox.events e ON e.id=c.event_id
 WHERE c.project_id=$1::text::uuid AND c.id=$2::text::uuid AND c.task_id=$3::text::uuid AND c.agent_id=$4::text::uuid
 AND c.request_id=$5::text::uuid AND c.dispatch_version=$6 AND c.launch_attempt=2 AND c.changed
 AND c.before_version=$7 AND c.after_version=$7+1 AND t.version=c.after_version AND t.state='blocked'
 AND c.reason='launch_retry_exhausted_v1' AND c.reason=d.failure_reason AND c.failure_occurred_at=d.failure_occurred_at
 AND d.status='failed' AND d.version=c.dispatch_version+1 AND d.attempt_count=2 AND d.final_attempt=2
 AND d.failure_code='INTERNAL_ERROR' AND d.failed_at=d.updated_at AND d.failed_at>=d.failure_occurred_at
 AND d.next_retry_at IS NULL AND d.execution_id IS NULL AND d.busy_attempt IS NULL
 AND e.project_id=c.project_id AND e.aggregate_id=c.task_id AND e.aggregate_version=c.after_version
 AND e.producer='work' AND e.event_type='work.task_transitioned' AND e.schema_version=4),
 (SELECT count(*) FROM agenteam_work.task_blockers b JOIN agenteam_work.task_launch_failures c
 ON b.project_id=c.project_id AND b.id=c.blocker_id AND b.failure_operation_id=c.id
 WHERE c.project_id=$1::text::uuid AND c.id=$2::text::uuid AND b.task_id=c.task_id AND b.type='technical'
 AND b.created_operation_id IS NULL AND b.resolved_at IS NULL
 AND b.created_by=jsonb_build_object('type','system','service_name','scheduler','source','scheduler','cause_id',$2::text)
 AND b.metadata=jsonb_build_object('code','scheduler_launch_failed','source','scheduler_dispatch','reference_id',$2::text)),
 (SELECT count(*) FROM agenteam_work.task_events WHERE project_id=$1::text::uuid AND failure_operation_id=$2::text::uuid
 AND task_id=$3::text::uuid AND task_version=$7+1 AND correlation_id=failure_operation_id
 AND type IN ('blocker_added','state_changed')),
 (SELECT count(*) FROM agenteam_work.task_launch_failures WHERE project_id=$1::text::uuid),
 (SELECT count(*) FROM agenteam_work.task_blockers WHERE project_id=$1::text::uuid)`,
		x.request.ProjectID.String(), x.dispatch.String(), x.v.task.ID.String(), x.request.AgentID.String(), x.request.Meta.RequestID.String(), int64(x.rejected.Version), int64(before.Version)).Scan(&facts, &blockers, &histories, &workResults, &allBlockers)
	if err != nil || facts != 1 || blockers != 1 || histories != 2 || workResults != 1 || allBlockers != 1 {
		t.Fatal("exhausted attempt did not publish one atomic Work/blocker/history/Outbox/Dispatch result", err)
	}
	var raw []byte
	var blocker string
	var history []string
	err = x.v.base.raw.QueryRow(ctxFor(t), `SELECT e.payload,c.blocker_id::text,
 (SELECT array_agg(h.id::text ORDER BY h.id) FROM agenteam_work.task_events h WHERE h.project_id=c.project_id AND h.failure_operation_id=c.id)
 FROM agenteam_work.task_launch_failures c JOIN agenteam_outbox.events e ON e.id=c.event_id
 WHERE c.project_id=$1::text::uuid AND c.id=$2::text::uuid`, x.request.ProjectID.String(), x.dispatch.String()).Scan(&raw, &blocker, &history)
	var payload wc.TaskLaunchFailed
	if err != nil || json.Unmarshal(raw, &payload) != nil || payload.Validate() != nil {
		t.Fatal("invalid typed exhausted failure event", err)
	}
	ids := make([]string, len(payload.TaskEventIDs))
	for n, id := range payload.TaskEventIDs {
		ids[n] = id.String()
	}
	if payload.ClaimID.String() != x.dispatch.String() || payload.Actor.CauseID != x.dispatch.String() || payload.BlockerID.String() != blocker || !reflect.DeepEqual(ids, history) || payload.MilestoneID != before.MilestoneID || payload.SprintID != before.SprintID || payload.AgentID != x.request.AgentID || payload.Reason != wc.TaskLaunchFailureRetryExhausted || payload.FromState != wc.TaskStateInProgress || payload.ToState != wc.TaskStateBlocked {
		t.Fatal("typed exhausted event lost original attempt and current Task relationships")
	}
}
