//go:build integration

package projectvariable_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/LunaDeerTech/agenteam/internal/central/project"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/scheduler"
	sc "github.com/LunaDeerTech/agenteam/internal/central/scheduler/contract"
	wc "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
)

// The same real Scheduler pair stops and cleans its own returned calls. Other
// manifest Stop facts and the transition to cleaning are explicit controlled
// prerequisites; no unavailable Registry port is invoked. Formal Launch only
// creates an Execution: nothing invokes or retires it. Nonempty relaunch_visits
// needs a terminal Execution and is deliberately outside this PostgreSQL slice.
func TestProjectSchedulerCleanup(t *testing.T) {
	t.Run("real-references-upgrade-and-original-returns", testProjectSchedulerCleanupReturn)
	t.Run("original-unknown-readonly-confirmation", testProjectSchedulerCleanupUnknown)
}

type schedulerCleanupPG struct {
	*schedulerOwnedStopPG
	participant pc.ProjectLifecycleParticipant
	cleanup     *project.LifecycleCleanupDriver
	drivers     []*project.LifecycleCleanupDriver
	retained    atomic.Bool
	outside     string
	others      string
	closedCalls int64
}

func newSchedulerCleanupPG(t *testing.T, upgrade bool) *schedulerCleanupPG {
	t.Helper()
	v := &schedulerCleanupPG{schedulerOwnedStopPG: newSchedulerOwnedStopPG(t, upgrade)}
	v.seedCleanupReferences(t)
	v.other(t)
	if upgrade {
		before := v.snapshot(t, true)
		v.rejectOrdinaryDelete(t)
		for n := 0; n < 2; n++ {
			migrate(t, v.base.db, migrationPrefix(t, "00082"))
			if before != v.snapshot(t, true) {
				t.Fatal("migration82/repeat changed original business or returned ownership facts")
			}
		}
	}
	v.rejectOrdinaryDelete(t)
	v.accept(t)
	v.observed(t, v.pair, false, pc.Stopped)
	if v.last.Details().State != pc.Stopped {
		t.Fatal("original Scheduler Stop did not return before cleanup")
	}
	// Only the persisted handoff is controlled. The provider must still prove
	// this original pair/instance, every actual returned call and every fresh
	// private Project cleanup round. Other cleanup states remain required.
	lock, _ := f.ProjectLock(v.scope.ProjectID.String())
	v.base.tx(t, []f.LockRequest{{Key: lock, Mode: f.Exclusive}}, func(ctx context.Context, _ f.Tx, x postgres.SQLExecutor) error {
		if _, err := x.Exec(ctx, `UPDATE agenteam_project.lifecycle_participants SET stop_state='stopped',version=version+1 WHERE operation_id=$1`, v.cause.OperationID.String()); err != nil {
			return err
		}
		tag, err := x.Exec(ctx, `UPDATE agenteam_project.lifecycle_operations SET state='cleaning',cleanup_stage='domains',version=version+1,updated_at=clock_timestamp() WHERE id=$1 AND project_id=$2 AND state='stopping'`, v.cause.OperationID.String(), v.scope.ProjectID.String())
		if err == nil && tag.RowsAffected() != 1 {
			return errors.New("original Scheduler stopping handoff changed")
		}
		return err
	})
	authority, err := project.NewSchedulerProjectCleanupAuthority(v.life, v.base.tracked)
	schedulerOwnedRequire(t, "cleanup-authority", err)
	v.participant, err = scheduler.NewProjectCleanupParticipant(v.pair, authority)
	schedulerOwnedRequire(t, "cleanup-original-pair", err)
	v.cleanup = v.newCleanupDriver(t)
	v.outside = v.preserved(t)
	v.others = v.otherParticipants(t)
	counts := v.counts(t)
	if counts[0] != 1 || counts[1] != 0 || counts[2] != 2 || counts[3] < 5 {
		t.Fatal("formal Scheduler references were not present", counts)
	}
	v.closedCalls = counts[3]
	t.Cleanup(func() { v.closeCleanup(t) })
	return v
}

func (v *schedulerCleanupPG) seedCleanupReferences(t *testing.T) {
	t.Helper()
	p := v.work.base.project.ID
	request, err := v.dispatch.LaunchRequest()
	schedulerOwnedRequire(t, "cleanup-original-rejected-request", err)
	rejected, err := v.pair.Handoff().LaunchOnce(ctxFor(t), p, v.dispatch.Summary().ID)
	reason, exact := wc.MatchTaskLaunchFailure(err, request)
	if !exact || reason != wc.TaskLaunchFailureUnsupportedResourceConstraints || rejected.Summary().LaunchOutcome != scheduler.KnownNotCreated {
		agentRetirementDiagnostic(t, "scheduler-cleanup-formal-rejection", err)
		t.Fatal("original unsupported Task policy did not produce its formal rejection")
	}
	failed, err := v.pair.Failure().FinalizeLaunchFailure(ctxFor(t), p, v.dispatch.Summary().ID)
	schedulerOwnedRequire(t, "cleanup-original-failure-finalization", err)
	if failed.Summary().Status != scheduler.Failed {
		t.Fatal("original failed Dispatch did not actually settle")
	}
	w := v.work
	created, err := w.tasks.CreateTask(ctxFor(t), v.base.ownerBrowser.actor, meta(t, "scheduler-cleanup-created-task", nil), p, wc.TaskCreate{TaskID: id[wc.Task](t), SprintID: w.task.SprintID, Title: "Created Execution remains after Scheduler cleanup", Type: wc.TaskTypeTask, Priority: wc.TaskPriorityMedium, Plan: "Create identity without invoking Execution"})
	schedulerOwnedRequire(t, "cleanup-formal-task-create", err)
	moved, err := w.transitions.TransferTask(ctxFor(t), v.base.ownerBrowser.actor, meta(t, "scheduler-cleanup-transfer", &created.Task.Version), p, created.Task.ID, wc.TaskTransfer{TargetState: wc.TaskStateTodo, AssigneeAgentID: &w.agentID, AddBlockers: []wc.TaskBlockerCreate{}, ResolveBlockerIDs: []wc.TaskBlockerID{}})
	schedulerOwnedRequire(t, "cleanup-formal-task-transfer", err)
	if moved.Validate() != nil || moved.Task.State != wc.TaskStateTodo {
		t.Fatal("formal second Task did not reach todo")
	}
	w.task = moved.Task
	claim, policy := schedulerClaimRequest(t, w)
	dispatch, err := v.pair.Coordinator().ClaimTask(ctxFor(t), claim, policy)
	schedulerOwnedRequire(t, "cleanup-formal-second-claim", err)
	launched, err := v.pair.Handoff().LaunchOnce(ctxFor(t), p, dispatch.Summary().ID)
	schedulerOwnedRequire(t, "cleanup-formal-launch-created", err)
	launches, lookups, execution := v.launcher.observed()
	if launches != 2 || lookups != 0 || execution.ID.Validate() != nil || execution.Status != ec.Created || launched.Summary().Status != scheduler.Launched || launched.Summary().ExecutionID == nil || *launched.Summary().ExecutionID != execution.ID {
		t.Fatal("formal Launch did not retain the exact Created Execution association")
	}
	var exactReferences bool
	schedulerOwnedRequire(t, "cleanup-real-execution-foreign-keys", v.base.raw.QueryRow(ctxFor(t), `SELECT EXISTS(SELECT 1 FROM agenteam_scheduler.task_runtimes r JOIN agenteam_scheduler.dispatches d ON d.project_id=r.project_id AND d.id=r.latest_dispatch_id JOIN agenteam_execution.executions e ON e.id=r.latest_execution_id AND e.id=d.execution_id WHERE r.project_id=$1::text AND d.id=$2::text AND e.id=$3::text AND d.status='launched' AND e.status='created')`, p.String(), launched.Summary().ID.String(), execution.ID.String()).Scan(&exactReferences))
	if !exactReferences {
		t.Fatal("real runtime/Dispatch/Execution references absent")
	}
}

func (v *schedulerCleanupPG) newCleanupDriver(t *testing.T) *project.LifecycleCleanupDriver {
	t.Helper()
	driver, err := project.NewLifecycleCleanupDriverForScheduler(v.base.tracked, v.life, captureProviderProcesses{guard: v.skills.guard}, v.participant)
	schedulerOwnedRequire(t, "fixed-cleanup-driver", err)
	v.drivers = append(v.drivers, driver)
	return driver
}

func (v *schedulerCleanupPG) runCleanup(driver *project.LifecycleCleanupDriver) *skillsCleanupPGCall {
	return v.start(func(ctx context.Context) error { return driver.Run(ctx, v.scope.ProjectID, v.cause.OperationID) })
}

func (v *schedulerCleanupPG) cleanupRound(t *testing.T) {
	t.Helper()
	c := v.runCleanup(v.cleanup)
	outboxCleanupWait(t, c.done, "original Scheduler cleanup round")
	schedulerOwnedRequire(t, "cleanup-round", c.err)
}

func (v *schedulerCleanupPG) counts(t *testing.T) [4]int64 {
	t.Helper()
	var counts [4]int64
	schedulerOwnedRequire(t, "cleanup-domain-counts", v.base.raw.QueryRow(ctxFor(t), `SELECT (SELECT count(*) FROM agenteam_scheduler.task_runtimes WHERE project_id=$1::text),(SELECT count(*) FROM agenteam_scheduler.relaunch_visits WHERE project_id=$1::text),(SELECT count(*) FROM agenteam_scheduler.dispatches WHERE project_id=$1::text),(SELECT count(*) FROM agenteam_scheduler.project_calls WHERE project_id=$1::text::uuid)`, v.scope.ProjectID.String()).Scan(&counts[0], &counts[1], &counts[2], &counts[3]))
	return counts
}

func (v *schedulerCleanupPG) requireCounts(t *testing.T, want [4]int64) {
	t.Helper()
	if got := v.counts(t); got != want {
		t.Fatal("original Scheduler batch differs", got, want)
	}
	if v.outside != v.preserved(t) || v.others != v.otherParticipants(t) {
		t.Fatal("Scheduler cleanup changed Work, Execution or another participant")
	}
}

func (v *schedulerCleanupPG) preserved(t *testing.T) string {
	t.Helper()
	var parts []string
	for _, table := range []string{"milestones", "sprints", "milestone_order_groups", "sprint_order_groups", "structure_commands", "tasks", "task_order_groups", "task_query_generations", "task_commands", "task_events", "task_blockers", "task_blocker_commands", "task_transition_commands", "sprint_start_commands", "task_scheduler_claims", "task_scheduler_relaunches", "task_busy_compensations", "task_launch_failures", "project_calls"} {
		parts = append(parts, fmt.Sprintf("(SELECT jsonb_agg(to_jsonb(x) ORDER BY to_jsonb(x)::text) FROM agenteam_work.%s x WHERE project_id=$1::text::uuid)", table))
	}
	parts = append(parts, `(SELECT jsonb_agg(to_jsonb(x) ORDER BY to_jsonb(x)::text) FROM agenteam_execution.executions x WHERE project_id=$1::text)`, `(SELECT jsonb_agg(to_jsonb(x) ORDER BY to_jsonb(x)::text) FROM agenteam_outbox.events x WHERE project_id=$1::text::uuid AND producer='work')`)
	var raw string
	schedulerOwnedRequire(t, "cleanup-preserved-domain-snapshot", v.base.raw.QueryRow(ctxFor(t), "SELECT jsonb_build_array("+strings.Join(parts, ",")+")::text", v.work.base.project.ID.String()).Scan(&raw))
	return raw
}

func (v *schedulerCleanupPG) otherParticipants(t *testing.T) string {
	t.Helper()
	var raw string
	schedulerOwnedRequire(t, "cleanup-other-participant-snapshot", v.base.raw.QueryRow(ctxFor(t), `SELECT jsonb_agg(to_jsonb(x) ORDER BY participant_name)::text FROM agenteam_project.lifecycle_participants x WHERE operation_id=$1 AND participant_name<>'scheduler'`, v.cause.OperationID.String()).Scan(&raw))
	return raw
}

func (v *schedulerCleanupPG) otherProject(t *testing.T) string {
	t.Helper()
	var raw string
	schedulerOwnedRequire(t, "cleanup-other-project-snapshot", v.base.raw.QueryRow(ctxFor(t), `SELECT jsonb_build_array((SELECT to_jsonb(x) FROM agenteam_project.projects x WHERE id=$1::text::uuid),(SELECT jsonb_agg(to_jsonb(x) ORDER BY to_jsonb(x)::text) FROM agenteam_scheduler.project_calls x WHERE project_id=$1::text::uuid),(SELECT jsonb_agg(to_jsonb(x) ORDER BY to_jsonb(x)::text) FROM agenteam_scheduler.dispatches x WHERE project_id=$1::text),(SELECT jsonb_agg(to_jsonb(x) ORDER BY to_jsonb(x)::text) FROM agenteam_scheduler.task_runtimes x WHERE project_id=$1::text),(SELECT jsonb_agg(to_jsonb(x) ORDER BY to_jsonb(x)::text) FROM agenteam_scheduler.relaunch_visits x WHERE project_id=$1::text))::text`, v.base.project.ID.String()).Scan(&raw))
	return raw
}

func (v *schedulerCleanupPG) progress(t *testing.T, cleanup, claim, stage string) {
	t.Helper()
	var operation, actualStage, actualCleanup, actualClaim, process string
	var schema int64
	var raw []byte
	schedulerOwnedRequire(t, "cleanup-original-project-progress", v.base.raw.QueryRow(ctxFor(t), `SELECT o.state,o.cleanup_stage,r.cleanup_state,w.phase,w.process_id::text,coalesce(r.checkpoint_schema,0),r.checkpoint FROM agenteam_project.lifecycle_operations o JOIN agenteam_project.lifecycle_participants r ON r.operation_id=o.id AND r.participant_name='scheduler' JOIN agenteam_project.work_claims w ON w.work_kind='lifecycle' AND w.work_id=o.id WHERE o.id=$1 AND o.project_id=$2`, v.cause.OperationID.String(), v.scope.ProjectID.String()).Scan(&operation, &actualStage, &actualCleanup, &actualClaim, &process, &schema, &raw))
	if operation != "cleaning" || actualStage != "domains" || actualCleanup != cleanup || actualClaim != claim || process != v.skills.process.String() {
		t.Fatal("original Scheduler cleanup progress differs", operation, actualStage, actualCleanup, actualClaim)
	}
	if stage == "" {
		if schema != 0 || len(raw) != 0 {
			t.Fatal("provider has not returned but Project checkpoint advanced")
		}
		return
	}
	cp, err := pc.NewCleanupCheckpoint(pc.SchedulerParticipant, v.cause, v.scope, f.Version(schema), raw)
	schedulerOwnedRequire(t, "cleanup-persisted-checkpoint", err)
	p, err := sc.DecodeProjectCleanupCheckpoint(&cp, v.cause, v.scope)
	schedulerOwnedRequire(t, "cleanup-persisted-progress", err)
	if p.Stage != stage || p.ProcessID != v.skills.process || p.InstanceID == "" {
		t.Fatal("original Scheduler checkpoint lost stage/process/instance", p.Stage)
	}
}

func (v *schedulerCleanupPG) rejectOrdinaryDelete(t *testing.T) {
	t.Helper()
	before := v.snapshot(t, true)
	cause, err := f.NewRecoveryCause("scheduler-cleanup-pg-ordinary-delete", id[struct{}](t).String(), v.work.base.project.ID.String())
	schedulerOwnedRequire(t, "cleanup-delete-guard-cause", err)
	result := v.base.tracked.WithinTx(ctxFor(t), cause, func(ctx context.Context, tx f.Tx) error {
		x, err := v.base.tracked.InTx(tx)
		if err != nil {
			return err
		}
		_, err = x.Exec(ctx, `DELETE FROM agenteam_scheduler.project_calls WHERE project_id=$1::uuid AND phase='business_closed'`, v.work.base.project.ID.String())
		return err
	})
	var db *postgres.Error
	if result.State() != f.NotCommitted || !errors.As(result.Fault(), &db) || db.SQLState() != "23514" || before != v.snapshot(t, true) {
		t.Fatal("ordinary active-Project call deletion bypassed immutable ownership", result.State())
	}
}

func (v *schedulerCleanupPG) busyWhileHeld(t *testing.T, c *skillsCleanupPGCall) {
	t.Helper()
	select {
	case <-c.done:
		t.Fatal("original cleanup returned while its Store return was held")
	default:
	}
	other := v.newCleanupDriver(t)
	call := v.runCleanup(other)
	outboxCleanupWait(t, call.done, "second cleanup driver original rejection")
	if !schedulerOwnedCode(call.err, f.ResourceBusy) {
		agentRetirementDiagnostic(t, "scheduler-cleanup-second-driver", call.err)
		t.Fatal("another same-Life driver crossed the original return")
	}
	v.other(t)
}

func schedulerCleanupCause(v *schedulerCleanupPG, cause f.TransactionCause) bool {
	d := cause.Details()
	return cause.Kind() == f.RecoveryCause && d.Owner == "scheduler-project-cleanup" && d.CheckpointRef == v.scope.ProjectID.String()
}

// Select the actual runtime DELETE by its before/after facts, not merely the
// shared cause: this provider's preflight and mutation deliberately share it.
func (v *schedulerCleanupPG) selectRuntimeDelete(t *testing.T, after func(f.TransactionCause, f.CommitResult), transform func(f.TransactionCause, f.CommitResult) f.CommitResult) {
	t.Helper()
	s := v.base.tracked
	var baseline sync.Map
	var selected atomic.Bool
	s.mu.Lock()
	s.before = func(ctx context.Context, tx f.Tx, cause f.TransactionCause) error {
		if !schedulerCleanupCause(v, cause) {
			return nil
		}
		x, err := s.InTx(tx)
		if err != nil {
			return err
		}
		var count int64
		err = x.QueryRow(ctx, `SELECT count(*) FROM agenteam_scheduler.task_runtimes WHERE project_id=$1::text`, v.scope.ProjectID.String()).Scan(&count)
		baseline.Store(tx, count)
		return err
	}
	s.after = func(ctx context.Context, tx f.Tx, cause f.TransactionCause) error {
		if !schedulerCleanupCause(v, cause) {
			return nil
		}
		old, ok := baseline.LoadAndDelete(tx)
		if !ok {
			return errors.New("runtime batch lost original transaction baseline")
		}
		x, err := s.InTx(tx)
		if err != nil {
			return err
		}
		var count int64
		if err = x.QueryRow(ctx, `SELECT count(*) FROM agenteam_scheduler.task_runtimes WHERE project_id=$1::text`, v.scope.ProjectID.String()).Scan(&count); err != nil {
			return err
		}
		if old.(int64) == 1 && count == 0 {
			selected.Store(true)
		} else if count != old.(int64) {
			return errors.New("unexpected runtime batch shape")
		}
		return nil
	}
	s.afterResult = func(cause f.TransactionCause, result f.CommitResult) {
		if selected.Load() && schedulerCleanupCause(v, cause) && after != nil {
			after(cause, result)
		}
	}
	s.transformResult = func(cause f.TransactionCause, result f.CommitResult) f.CommitResult {
		if selected.Load() && schedulerCleanupCause(v, cause) && transform != nil {
			return transform(cause, result)
		}
		return result
	}
	s.mu.Unlock()
}

func testProjectSchedulerCleanupReturn(t *testing.T) {
	v := newSchedulerCleanupPG(t, true)
	b := v.barrier()
	v.selectRuntimeDelete(t, func(_ f.TransactionCause, result f.CommitResult) { b.hold(result) }, nil)
	call := v.runCleanup(v.cleanup)
	v.await(t, call, b)
	v.requireCounts(t, [4]int64{0, 0, 2, v.closedCalls})
	v.progress(t, "required", "running", "")
	v.busyWhileHeld(t, call)
	other := v.otherProject(t)
	b.open()
	outboxCleanupWait(t, call.done, "original runtime cleanup Store return")
	schedulerOwnedRequire(t, "cleanup-runtime-original-return", call.err)
	v.clear()
	v.progress(t, "pending", "terminal", "dispatches")
	v.cleanupRound(t)
	v.requireCounts(t, [4]int64{0, 0, 0, v.closedCalls})
	v.progress(t, "pending", "terminal", "calls")
	// The provider has returned; this distinct barrier holds the Project's
	// final checkpoint COMMIT acknowledgement, after all A rows are gone.
	finish := v.barrier()
	var completed atomic.Bool
	s := v.base.tracked
	s.setAfter(func(ctx context.Context, tx f.Tx, cause f.TransactionCause) error {
		if cause.Kind() != f.JobCause || cause.Details().JobType != "project-lifecycle" || cause.Details().JobID != v.cause.OperationID.String() {
			return nil
		}
		x, err := s.InTx(tx)
		if err != nil {
			return err
		}
		var exact bool
		if err = x.QueryRow(ctx, `SELECT cleanup_state='completed' FROM agenteam_project.lifecycle_participants WHERE operation_id=$1 AND participant_name='scheduler'`, v.cause.OperationID.String()).Scan(&exact); err != nil {
			return err
		}
		if exact {
			completed.Store(true)
		}
		return nil
	})
	s.mu.Lock()
	s.afterResult = func(cause f.TransactionCause, result f.CommitResult) {
		if cause.Kind() == f.JobCause && cause.Details().JobType == "project-lifecycle" && cause.Details().JobID == v.cause.OperationID.String() && completed.Load() {
			finish.hold(result)
		}
	}
	s.mu.Unlock()
	call = v.runCleanup(v.cleanup)
	v.await(t, call, finish)
	v.requireCounts(t, [4]int64{})
	v.progress(t, "completed", "terminal", "completed")
	if other != v.otherProject(t) {
		t.Fatal("A cleanup changed another Project's returned Scheduler facts")
	}
	v.busyWhileHeld(t, call)
	other = v.otherProject(t)
	finish.open()
	outboxCleanupWait(t, call.done, "original completed Scheduler checkpoint return")
	schedulerOwnedRequire(t, "cleanup-final-checkpoint-return", call.err)
	v.clear()
	v.cleanupRound(t)
	v.requireCounts(t, [4]int64{})
	if other != v.otherProject(t) {
		t.Fatal("completed replay touched another Project")
	}
	t.Log("PROJECT_SCHEDULER_CLEANUP_RETURN migration81_to82_repeat=true ordinary_delete_rejected=true real_created_execution_fk=true runtime_dispatch_closed_calls_removed=true original_provider_and_checkpoint_returns=true same_life_second_driver_busy=true work_execution_other_project_preserved=true parent_domains=true relaunch_nonempty_not_covered=true controlled_other_stop_prerequisites=true no_invoke_no_default_registry=true")
}

func testProjectSchedulerCleanupUnknown(t *testing.T) {
	v := newSchedulerCleanupPG(t, false)
	var original f.CommitResult
	var masked atomic.Bool
	v.selectRuntimeDelete(t, nil, func(cause f.TransactionCause, result f.CommitResult) f.CommitResult {
		if result.State() == f.Committed && masked.CompareAndSwap(false, true) {
			original = f.UnknownResult(id[f.TransactionAttempt](t), cause)
			v.retained.Store(true)
			return original
		}
		return result
	})
	call := v.runCleanup(v.cleanup)
	outboxCleanupWait(t, call.done, "original cleanup acknowledgement Unknown")
	v.clear()
	actual, ok := scheduler.UnknownAttempt(call.err)
	if !masked.Load() || !ok || actual.AttemptID() != original.AttemptID() || !schedulerOwnedSameCause(actual.Cause(), original.Cause()) {
		agentRetirementDiagnostic(t, "scheduler-cleanup-original-unknown", call.err)
		t.Fatal("Scheduler cleanup lost the original Unknown attempt/cause")
	}
	v.requireCounts(t, [4]int64{0, 0, 2, v.closedCalls})
	v.progress(t, "required", "running", "")
	stable := v.snapshot(t, true)
	other := v.otherProject(t)
	b := v.barrier()
	var baseline sync.Map
	var seen atomic.Int64
	s := v.base.tracked
	s.mu.Lock()
	s.before = func(ctx context.Context, tx f.Tx, cause f.TransactionCause) error {
		if !schedulerOwnedSameCause(cause, original.Cause()) {
			return nil
		}
		x, err := s.InTx(tx)
		if err != nil {
			return err
		}
		var count int64
		err = x.QueryRow(ctx, schedulerCleanupWritesSQL).Scan(&count)
		baseline.Store(tx, count)
		return err
	}
	s.after = func(ctx context.Context, tx f.Tx, cause f.TransactionCause) error {
		if !schedulerOwnedSameCause(cause, original.Cause()) {
			return nil
		}
		before, ok := baseline.LoadAndDelete(tx)
		if !ok {
			return errors.New("cleanup confirmation lacked its original transaction baseline")
		}
		x, err := s.InTx(tx)
		if err != nil {
			return err
		}
		var count int64
		if err = x.QueryRow(ctx, schedulerCleanupWritesSQL).Scan(&count); err != nil {
			return err
		}
		if count != before.(int64) {
			return errors.New("cleanup confirmation wrote database facts")
		}
		seen.Add(1)
		return nil
	}
	s.afterResult = func(cause f.TransactionCause, result f.CommitResult) {
		if schedulerOwnedSameCause(cause, original.Cause()) && seen.Load() == 1 {
			b.hold(result)
		}
	}
	s.mu.Unlock()
	call = v.runCleanup(v.cleanup)
	v.await(t, call, b)
	v.progress(t, "required", "running", "")
	if stable != v.snapshot(t, true) || other != v.otherProject(t) {
		t.Fatal("original read-only confirmation changed complete domain facts")
	}
	v.busyWhileHeld(t, call)
	other = v.otherProject(t)
	b.open()
	outboxCleanupWait(t, call.done, "original cleanup confirmation Store return")
	schedulerOwnedRequire(t, "cleanup-readonly-confirmation-return", call.err)
	v.retained.Store(false)
	v.clear()
	if seen.Load() != 1 || stable != v.snapshot(t, true) {
		t.Fatal("confirmation deleted the next batch or lost the original callback")
	}
	v.progress(t, "pending", "terminal", "runtimes")
	v.requireCounts(t, [4]int64{0, 0, 2, v.closedCalls})
	v.cleanupRound(t)
	v.requireCounts(t, [4]int64{0, 0, 0, v.closedCalls})
	v.progress(t, "pending", "terminal", "calls")
	v.cleanupRound(t)
	v.requireCounts(t, [4]int64{})
	v.progress(t, "completed", "terminal", "completed")
	if other != v.otherProject(t) {
		t.Fatal("A recovery changed B's original returned facts")
	}
	t.Log("PROJECT_SCHEDULER_CLEANUP_UNKNOWN actual_runtime_delete_committed=true local_ack_unknown=true original_attempt_cause=true original_project_round_retained=true same_tx_zero_writes=true original_confirmation_return=true next_explicit_batch=true work_execution_other_project_preserved=true parent_domains=true relaunch_nonempty_not_covered=true no_network_lost_ack=true")
}

// These are before/after readings in the very same original transaction. The
// absolute backend count can include earlier, unflushed transaction statistics.
const schedulerCleanupWritesSQL = `SELECT COALESCE(sum(n_tup_ins+n_tup_upd+n_tup_del),0)::bigint FROM pg_stat_xact_user_tables`

func (v *schedulerCleanupPG) closeCleanup(t *testing.T) {
	t.Helper()
	for _, b := range v.barriers {
		b.open()
	}
	for _, call := range v.calls {
		outboxCleanupWait(t, call.done, "original cleanup calls before owner shutdown")
	}
	v.clear()
	// Only the one test-local retained acknowledgement can exist. Confirm it
	// through the same original driver before global Stop; never reset an owner.
	if v.retained.Load() {
		call := v.runCleanup(v.cleanup)
		outboxCleanupWait(t, call.done, "original retained cleanup confirmation during shutdown")
		if call.err != nil {
			t.Error("retained Scheduler cleanup confirmation failed", call.err)
		}
	}
	for _, driver := range v.drivers {
		schedulerOwnedDrain(t, driver)
	}
}
