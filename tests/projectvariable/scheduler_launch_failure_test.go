//go:build integration

package projectvariable_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/outbox"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/LunaDeerTech/agenteam/internal/central/scheduler"
	"github.com/LunaDeerTech/agenteam/internal/central/work"
	wc "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
)

// The opaque nonempty policy is accepted as canonical input by the real
// Claim API and rejected by the real Task Launch v1 provider. It never claims
// that resource-constraint semantics, a permit or an Execution were created.
func TestSchedulerLaunchFinalFailure(t *testing.T) {
	t.Run("title-preserved-technical-blocker-and-replay", func(t *testing.T) {
		x := newSchedulerFailureFixture(t)
		before := x.currentTask(t)
		title := "Owner title survives final Launch failure"
		updated, err := x.v.tasks.UpdateTask(ctxFor(t), x.v.base.ownerBrowser.actor,
			meta(t, "final-failure-owner-title", &before.Version), x.v.base.project.ID, before.ID, wc.TaskFieldsUpdate{Title: &title})
		if err != nil || !updated.Changed || updated.Task.Version != before.Version+1 || updated.Task.Title != title {
			t.Fatal("actual Owner title update", err)
		}
		settled, err := x.finalizer.FinalizeLaunchFailure(ctxFor(t), x.request.ProjectID, x.dispatch)
		if err != nil {
			t.Fatal("actual final failure settlement", err)
		}
		x.requireSettled(t, settled, updated.Task)
		stable := x.snapshot(t)
		replay, err := x.finalizer.FinalizeLaunchFailure(ctxFor(t), x.request.ProjectID, x.dispatch)
		if err != nil || !reflect.DeepEqual(replay.Summary(), settled.Summary()) {
			t.Fatal("original final result replay", err)
		}
		observed, err := x.finalizer.Lookup(ctxFor(t), x.request.ProjectID, x.dispatch)
		if err != nil || !reflect.DeepEqual(observed.Summary(), settled.Summary()) || stable != x.snapshot(t) {
			t.Fatal("lookup/replay changed committed facts", err)
		}
		x.requireNoExecution(t)
	})
	t.Run("late-transaction-rollback-and-settlement", func(t *testing.T) {
		x := newSchedulerFailureFixture(t)
		before := x.currentTask(t)
		stable := x.snapshot(t)
		command, err := f.NewCommandIdentity("scheduler", []string{x.request.ProjectID.String()}, "finalize_launch_failure", x.request.Meta.IdempotencyKey)
		if err != nil {
			t.Fatal(err)
		}
		marker := errors.New("scheduler-final-failure-late-callback-rollback")
		var seen, returned bool
		var physical f.CommitResult
		store := x.v.base.tracked
		store.setAfter(func(ctx context.Context, tx f.Tx, cause f.TransactionCause) error {
			if cause.Kind() != f.CommandsCause || cause.Details().Primary.Canonical() != command.Canonical() {
				return nil
			}
			sql, err := store.InTx(tx)
			if err != nil {
				return err
			}
			if err = x.settledFacts(ctx, sql, before.Version); err != nil {
				return err
			}
			seen = true
			return marker // Original Work+Dispatch callback succeeded; real Store rolls back.
		})
		store.mu.Lock()
		store.afterResult = func(cause f.TransactionCause, result f.CommitResult) {
			if seen && cause.Kind() == f.CommandsCause && cause.Details().Primary.Canonical() == command.Canonical() {
				physical, returned = result, true
			}
		}
		store.mu.Unlock()
		clear := func() { store.setAfter(nil); store.mu.Lock(); store.afterResult = nil; store.mu.Unlock() }
		defer clear()
		out, err := x.finalizer.FinalizeLaunchFailure(ctxFor(t), x.request.ProjectID, x.dispatch)
		clear()
		if !seen || !returned || physical.State() != f.NotCommitted || !errors.Is(physical.Fault(), marker) || !errors.Is(err, marker) || out.Summary().Status == scheduler.Failed {
			t.Fatal("actual late rollback was lost", err)
		}
		if stable != x.snapshot(t) {
			t.Fatal("rollback leaked Task/blocker/history/event/Dispatch writes")
		}
		settled, err := x.finalizer.FinalizeLaunchFailure(ctxFor(t), x.request.ProjectID, x.dispatch)
		if err != nil {
			t.Fatal("same original Dispatch after known rollback", err)
		}
		x.requireSettled(t, settled, before)
		x.requireNoExecution(t)
	})
}

type schedulerFailureFixture struct {
	v         *taskTransitionFixture
	dispatch  scheduler.DispatchID
	request   ec.LaunchRequest
	rejected  scheduler.DispatchSummary
	launcher  *countedSchedulerExecution
	finalizer *scheduler.LaunchFailureFinalizer
}

func newSchedulerFailureFixture(t *testing.T) *schedulerFailureFixture {
	t.Helper()
	v, claims := newSchedulerClaimFixture(t)
	claim, policy := schedulerClaimRequest(t, v)
	policy.AllowedResourceConstraints = []json.RawMessage{json.RawMessage(`{}`)}
	claimed, err := claims.ClaimTask(ctxFor(t), claim, policy)
	if err != nil || claimed.Summary().Status != scheduler.Pending {
		t.Fatal("formal claim with preserved opaque policy", err)
	}
	request, err := claimed.LaunchRequest()
	if err != nil || request.Validate() != nil || len(request.Policy.AllowedResourceConstraints) != 1 {
		t.Fatal("original saved policy", err)
	}
	handoff, counter := newBusyLaunchHandoff(t, v)
	rejected, launchErr := handoff.LaunchOnce(ctxFor(t), request.ProjectID, claimed.Summary().ID)
	reason, exact := wc.MatchTaskLaunchFailure(launchErr, request)
	if !exact || reason != wc.TaskLaunchFailureUnsupportedResourceConstraints || rejected.Summary().Status != scheduler.Pending || rejected.Summary().LaunchOutcome != scheduler.KnownNotCreated || rejected.Summary().FailureReason != reason || rejected.Summary().FailureCode != f.DependencyUnbound || rejected.Summary().AttemptCount != 1 || rejected.Summary().FailureOccurredAt == nil {
		t.Fatal("real Task provider did not return and persist its exact rejection", launchErr)
	}
	x := &schedulerFailureFixture{v: v, dispatch: claimed.Summary().ID, request: request, rejected: rejected.Summary(), launcher: counter}
	x.finalizer = newSchedulerFailureFinalizer(t, v)
	x.requireNoExecution(t)
	return x
}

func (x *schedulerFailureFixture) currentTask(t *testing.T) wc.Task {
	t.Helper()
	v, err := x.v.taskReader.GetTask(ctxFor(t), x.v.base.ownerBrowser.actor, x.request.ProjectID, x.v.task.ID)
	if err != nil || v.Validate() != nil {
		t.Fatal("actual current Task", err)
	}
	return v
}

func (x *schedulerFailureFixture) requireSettled(t *testing.T, got scheduler.Dispatch, before wc.Task) {
	t.Helper()
	s := got.Summary()
	if s.Status != scheduler.Failed || s.ID != x.dispatch || s.Version != x.rejected.Version+1 || s.AttemptCount != 1 || s.FailedAt == nil || s.FailureReason != x.rejected.FailureReason || s.FailureOccurredAt == nil || !s.FailureOccurredAt.Time().Equal(x.rejected.FailureOccurredAt.Time()) {
		t.Fatal("final Dispatch identity/version/attempt changed")
	}
	current := x.currentTask(t)
	want := before.Clone()
	want.State, want.Version, want.UpdatedAt, want.ManualRank = wc.TaskStateBlocked, before.Version+1, current.UpdatedAt, current.ManualRank
	if !bytes.Equal(jsonBytes(t, want), jsonBytes(t, current)) {
		t.Fatal("final failure overwrote current title/plan/other Task fields")
	}
	if err := x.settledFacts(ctxFor(t), x.v.base.raw, before.Version); err != nil {
		t.Fatal("same-Tx final failure facts", err)
	}
}

func (x *schedulerFailureFixture) requireNoExecution(t *testing.T) {
	t.Helper()
	launches, lookups, created := x.launcher.observed()
	var executions int64
	err := x.v.base.raw.QueryRow(ctxFor(t), `SELECT count(*) FROM agenteam_execution.executions WHERE project_id=$1::text`, x.request.ProjectID.String()).Scan(&executions)
	if err != nil || executions != 0 || launches != 1 || lookups != 0 || created.ID.Validate() == nil {
		t.Fatal("typed rejection created/retried Execution", err)
	}
}

func newSchedulerFailureFinalizer(t *testing.T, v *taskTransitionFixture) *scheduler.LaunchFailureFinalizer {
	t.Helper()
	catalog := event.NewCatalog()
	events, err := wc.RegisterTaskLaunchFailureEvents(catalog)
	if err != nil {
		t.Fatal(err)
	}
	box, err := outbox.New(v.base.tracked, catalog, outbox.Authorizations{
		Producers: map[event.StableName]oc.ProducerAuthority{wc.WorkProducer: v.authority},
		Sessions:  v.base.accounts, System: v.base.accounts, Projects: v.base.projectAuthority,
		Audit: v.base.audit, Cursors: v.base.keys, Processes: fixtureProcess{id[oc.Process](t)},
	})
	if err != nil {
		t.Fatal(err)
	}
	writer, err := work.NewTaskLaunchFailure(v.base.tracked, work.TaskLaunchFailureDependencies{
		Authority: v.authority, Scheduler: v.pending, Pending: v.pending, Events: box, FailureEvents: events,
	})
	if err != nil {
		t.Fatal("real Work final failure provider", err)
	}
	t.Cleanup(func() {
		writer.Stop()
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := writer.Drain(ctx); err != nil || !writer.Joined() {
			t.Error("original Work final failure calls did not join", err)
		}
	})
	finalizer, err := scheduler.NewLaunchFailureFinalizer(v.pending, scheduler.LaunchFailureFinalizerDependencies{Projects: v.base.projectAuthority, Work: writer})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		finalizer.Stop()
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := finalizer.Drain(ctx); err != nil || !finalizer.Joined() {
			t.Error("original Scheduler final failure calls did not join", err)
		}
	})
	return finalizer
}

func (x *schedulerFailureFixture) snapshot(t *testing.T) string {
	t.Helper()
	base, err := schedulerClaimSnapshot(ctxFor(t), x.v.base.raw, x.v)
	if err != nil {
		t.Fatal("complete original claim snapshot", err)
	}
	var owned string
	err = x.v.base.raw.QueryRow(ctxFor(t), `SELECT jsonb_build_object(
 'failures',(SELECT coalesce(jsonb_agg(to_jsonb(f) ORDER BY id),'[]'::jsonb) FROM agenteam_work.task_launch_failures f WHERE project_id=$1::text::uuid),
 'blockers',(SELECT coalesce(jsonb_agg(to_jsonb(b) ORDER BY id),'[]'::jsonb) FROM agenteam_work.task_blockers b WHERE project_id=$1::text::uuid)
 )::text`, x.request.ProjectID.String()).Scan(&owned)
	if err != nil {
		t.Fatal("complete final failure and blocker snapshot", err)
	}
	return base + "\n" + owned
}

// This observer is also called inside the original final callback before the
// test's injected error. Every queried row must come from the actual writers;
// the observer never inserts, updates or synthesizes a successful outcome.
func (x *schedulerFailureFixture) settledFacts(ctx context.Context, sql postgres.SQLExecutor, before f.Version) error {
	var counts [10]int64
	err := sql.QueryRow(ctx, `SELECT
 (SELECT count(*) FROM agenteam_work.task_launch_failures c
  JOIN agenteam_work.tasks t ON t.project_id=c.project_id AND t.id=c.task_id
  JOIN agenteam_scheduler.dispatches d ON d.project_id=c.project_id::text AND d.id=c.id::text
  WHERE c.project_id=$1::text::uuid AND c.id=$2::text::uuid AND c.task_id=$3::text::uuid
   AND c.agent_id=$4::text::uuid AND c.request_id=$5::text::uuid AND c.dispatch_version=$6 AND c.launch_attempt=1
   AND c.changed AND c.before_version=$7 AND c.after_version=$8 AND t.version=$8 AND t.state='blocked'
   AND t.assignee_agent_id=c.agent_id AND t.sprint_id=$9::text::uuid
   AND c.reason='unsupported_resource_constraints_v1' AND c.reason=d.failure_reason AND c.failure_occurred_at=d.failure_occurred_at
   AND c.blocker_id IS NOT NULL AND c.event_id IS NOT NULL
   AND d.status='failed' AND d.launch_outcome='known_not_created' AND d.version=$6+1 AND d.attempt_count=1 AND d.final_attempt=1
   AND d.failure_code='DEPENDENCY_UNBOUND' AND d.failed_at=d.updated_at AND d.failed_at>=d.failure_occurred_at
   AND d.execution_id IS NULL AND d.next_retry_at IS NULL AND d.busy_attempt IS NULL AND d.skip_reason IS NULL),
 (SELECT count(*) FROM agenteam_work.task_blockers b JOIN agenteam_work.task_launch_failures c
   ON b.project_id=c.project_id AND b.id=c.blocker_id AND b.failure_operation_id=c.id
  WHERE c.project_id=$1::text::uuid AND c.id=$2::text::uuid AND b.task_id=c.task_id AND b.type='technical'
   AND b.created_operation_id IS NULL AND b.resolved_at IS NULL AND b.description<>''
   AND b.created_by=jsonb_build_object('type','system','service_name','scheduler','source','scheduler','cause_id',$2::text)
   AND b.metadata=jsonb_build_object('code','scheduler_launch_failed','source','scheduler_dispatch','reference_id',$2::text)),
 (SELECT count(*) FROM agenteam_work.task_events h JOIN agenteam_work.task_launch_failures c
   ON h.project_id=c.project_id AND h.failure_operation_id=c.id AND h.task_id=c.task_id
  WHERE c.project_id=$1::text::uuid AND c.id=$2::text::uuid AND h.type='blocker_added' AND h.task_version=$8
   AND h.correlation_id=c.id AND h.actor=jsonb_build_object('type','system','service_name','scheduler','source','scheduler','cause_id',$2::text)
   AND h.payload=jsonb_build_object('blocker_id',c.blocker_id::text,'blocker_type','technical','reason_code','scheduler_launch_failed')),
 (SELECT count(*) FROM agenteam_work.task_events h WHERE h.project_id=$1::text::uuid AND h.task_id=$3::text::uuid
   AND h.failure_operation_id=$2::text::uuid AND h.correlation_id=h.failure_operation_id AND h.type='state_changed' AND h.task_version=$8
   AND h.actor=jsonb_build_object('type','system','service_name','scheduler','source','scheduler','cause_id',$2::text)
   AND h.payload=jsonb_build_object('from_state','in_progress','to_state','blocked','reason_code','scheduler_launch_failed')),
 (SELECT count(*) FROM agenteam_work.task_events WHERE project_id=$1::text::uuid AND failure_operation_id=$2::text::uuid),
 (SELECT count(*) FROM agenteam_outbox.events e JOIN agenteam_work.task_launch_failures c ON e.id=c.event_id
  WHERE c.project_id=$1::text::uuid AND c.id=$2::text::uuid AND e.project_id=c.project_id AND e.aggregate_id=c.task_id AND e.aggregate_version=$8
   AND e.producer='work' AND e.event_type='work.task_transitioned' AND e.schema_version=4),
 (SELECT count(*) FROM agenteam_work.task_launch_failures WHERE project_id=$1::text::uuid),
 (SELECT count(*) FROM agenteam_execution.executions WHERE project_id=$1::text),
 (SELECT count(*) FROM agenteam_scheduler.dispatches WHERE project_id=$1::text AND status='pending'),
 (SELECT count(*) FROM agenteam_work.task_blockers WHERE project_id=$1::text::uuid)`,
		x.request.ProjectID.String(), x.dispatch.String(), x.v.task.ID.String(), x.request.AgentID.String(), x.request.Meta.RequestID.String(), int64(x.rejected.Version), int64(before), int64(before+1), x.v.task.SprintID.String()).Scan(
		&counts[0], &counts[1], &counts[2], &counts[3], &counts[4], &counts[5], &counts[6], &counts[7], &counts[8], &counts[9])
	if err != nil {
		return err
	}
	if counts != ([10]int64{1, 1, 1, 1, 2, 1, 1, 0, 0, 1}) {
		return fmt.Errorf("atomic final failure counts %v", counts)
	}
	var raw []byte
	var blocker string
	var history []string
	err = sql.QueryRow(ctx, `SELECT e.payload,c.blocker_id::text,
 (SELECT array_agg(h.id::text ORDER BY h.id) FROM agenteam_work.task_events h WHERE h.project_id=c.project_id AND h.failure_operation_id=c.id)
 FROM agenteam_work.task_launch_failures c JOIN agenteam_outbox.events e ON e.id=c.event_id
 WHERE c.project_id=$1::text::uuid AND c.id=$2::text::uuid`, x.request.ProjectID.String(), x.dispatch.String()).Scan(&raw, &blocker, &history)
	if err != nil {
		return err
	}
	var payload wc.TaskLaunchFailed
	if json.Unmarshal(raw, &payload) != nil || payload.Validate() != nil {
		return errors.New("invalid typed final failure event")
	}
	ids := make([]string, len(payload.TaskEventIDs))
	for n, id := range payload.TaskEventIDs {
		ids[n] = id.String()
	}
	if payload.ClaimID.String() != x.dispatch.String() || payload.Actor.CauseID != x.dispatch.String() || payload.BlockerID.String() != blocker || !reflect.DeepEqual(ids, history) ||
		payload.MilestoneID != x.v.task.MilestoneID || payload.SprintID != x.v.task.SprintID || payload.AgentID != x.request.AgentID || payload.Reason != wc.TaskLaunchFailureUnsupportedResourceConstraints || payload.FromState != wc.TaskStateInProgress || payload.ToState != wc.TaskStateBlocked {
		return errors.New("typed final event lost actual current Task and history identities")
	}
	return nil
}
