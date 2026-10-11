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

	"github.com/LunaDeerTech/agenteam/internal/central/agent"
	"github.com/LunaDeerTech/agenteam/internal/central/execution"
	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/LunaDeerTech/agenteam/internal/central/scheduler"
	"github.com/LunaDeerTech/agenteam/internal/central/work"
	wc "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
)

// Relaunch starts from a real completed Model call. The original Task remains
// in_progress, and the original Claim/Launch/Execution lineage stays intact.
// All subsequent visits use the original providers and persisted lineage.
func TestSchedulerRelaunch(t *testing.T) {
	t.Run("cooldown-restart-and-new-execution", runSchedulerRelaunchCooldown)
	t.Run("relaunch-failure-atomic-block-and-replay", runSchedulerRelaunchFailure)
}

type schedulerRelaunchFixture struct {
	execution *schedulerExecutionFixture
	original  scheduler.Dispatch
	task      wc.Task
	workFacts string
}

func newSchedulerRelaunchFixture(t *testing.T) *schedulerRelaunchFixture {
	t.Helper()
	x := newSchedulerExecutionFixture(t, false)
	x.start(t)
	runner := x.runner(t, true)
	first, err := runner.RunTraversal(ctxFor(t))
	firstRoundRequire(t, err)
	if len(first.Visits) != 1 || first.Visits[0].Err != nil || first.Visits[0].Action != scheduler.ProjectVisitClaim {
		t.Fatal("initial real traversal did not claim and launch the original todo")
	}
	dispatch := first.Visits[0].Dispatch
	x.bindOriginal(t, dispatch)
	x.requireDelivery(t, first, dispatch)
	x.awaitTerminal(t, ec.Succeeded)
	stopSchedulerExecutionRunner(t, runner)
	x.requireTerminal(t, ec.Succeeded)
	c := x.round.capture
	task, err := c.v.taskReader.GetTask(ctxFor(t), c.v.base.ownerBrowser.actor, c.v.base.project.ID, c.v.task.ID)
	firstRoundRequire(t, err)
	if task.State != wc.TaskStateInProgress {
		t.Fatal("the real terminal Execution changed the independently owned Task")
	}
	// Keep the original executor and its real providers owned by the existing
	// fixture cleanup. Reconstructing Scheduler owners must not stop or replace
	// the execution graph, or depend on mutable capture-observation taps.
	out := &schedulerRelaunchFixture{execution: x, original: dispatch, task: task}
	out.workFacts = out.readWorkFacts(t)
	return out
}

type schedulerRelaunchOwner struct {
	runner *scheduler.ProjectRunner
	owner  *scheduler.RelaunchCoordinator
	work   *work.TaskRelaunchService
}

func (x *schedulerRelaunchFixture) newOwner(t *testing.T, count int64, policy ec.Policy) *schedulerRelaunchOwner {
	t.Helper()
	c := x.execution.round.capture
	v := c.v
	agents, err := agent.NewSchedulerCurrent(v.agent.providers.Agents, v.pending)
	firstRoundRequire(t, err)
	occupancy, err := execution.NewWorkOccupancy(v.base.tracked)
	firstRoundRequire(t, err)
	reader, err := work.NewSchedulerTaskReader(v.base.tracked, v.authority)
	firstRoundRequire(t, err)
	writer, err := work.NewTaskRelaunch(v.base.tracked, work.TaskRelaunchDependencies{
		Authority: v.authority, Scheduler: v.pending, Agents: agents, Pending: v.pending, Occupancy: occupancy,
	})
	firstRoundRequire(t, err)
	o := &schedulerRelaunchOwner{work: writer}
	t.Cleanup(func() { o.stop(t) })
	o.owner, err = scheduler.NewRelaunchCoordinator(c.claims, writer, reader, occupancy, count)
	firstRoundRequire(t, err)
	o.runner, err = scheduler.NewProjectRunnerWithExecutions(c.claims, x.execution.visitor,
		scheduler.ProjectRunnerOptions{ProjectID: v.base.project.ID, TickInterval: 20 * time.Millisecond,
			LaunchPolicy: policy, Tasks: reader, Relaunch: o.owner}, x.execution.executor, 1)
	firstRoundRequire(t, err)
	return o
}

func (o *schedulerRelaunchOwner) stop(t *testing.T) {
	t.Helper()
	if o.runner != nil {
		stopSchedulerExecutionRunner(t, o.runner)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if o.owner != nil {
		o.owner.Stop()
		if err := o.owner.Drain(ctx); err != nil || !o.owner.Joined() {
			t.Error("original relaunch owner did not join", err)
		}
	}
	if o.work != nil {
		o.work.Stop()
		if err := o.work.Drain(ctx); err != nil || !o.work.Joined() {
			t.Error("original Work relaunch calls did not join", err)
		}
	}
}

func (x *schedulerRelaunchFixture) requireTaskUnchanged(t *testing.T) {
	t.Helper()
	v := x.execution.round.capture.v
	current, err := v.taskReader.GetTask(ctxFor(t), v.base.ownerBrowser.actor, v.base.project.ID, x.task.ID)
	firstRoundRequire(t, err)
	if !reflect.DeepEqual(current, x.task) {
		t.Fatal("relaunch admission rewrote the original in_progress Task")
	}
	if x.readWorkFacts(t) != x.workFacts {
		t.Fatal("relaunch rewrote Task ordering/history, Work events or the original todo claim")
	}
}

func (x *schedulerRelaunchFixture) readWorkFacts(t *testing.T) string {
	t.Helper()
	v := x.execution.round.capture.v
	var raw string
	err := v.base.raw.QueryRow(ctxFor(t), `SELECT jsonb_build_object(
 'task',(SELECT to_jsonb(t) FROM agenteam_work.tasks t WHERE project_id=$1::text::uuid AND id=$2::text::uuid),
 'history',(SELECT coalesce(jsonb_agg(to_jsonb(h) ORDER BY id),'[]'::jsonb) FROM agenteam_work.task_events h WHERE project_id=$1::text::uuid AND task_id=$2::text::uuid),
 'groups',(SELECT coalesce(jsonb_agg(to_jsonb(g) ORDER BY sprint_id,state,priority),'[]'::jsonb) FROM agenteam_work.task_order_groups g WHERE project_id=$1::text::uuid),
 'generation',(SELECT query_generation FROM agenteam_work.task_query_generations WHERE project_id=$1::text::uuid),
 'events',(SELECT coalesce(jsonb_agg(to_jsonb(e) ORDER BY id),'[]'::jsonb) FROM agenteam_outbox.events e WHERE project_id=$1::text::uuid AND producer='work'),
 'claim',(SELECT to_jsonb(c) FROM agenteam_work.task_scheduler_claims c WHERE project_id=$1::text::uuid AND id=$3::text::uuid)
 )::text`, v.base.project.ID.String(), x.task.ID.String(), x.original.Summary().ID.String()).Scan(&raw)
	firstRoundRequire(t, err)
	return raw
}

func runSchedulerRelaunchCooldown(t *testing.T) {
	x := newSchedulerRelaunchFixture(t)
	policy := x.execution.round.capture.launchPolicy
	first := x.newOwner(t, 2, policy)
	visit, err := first.runner.RunTraversal(ctxFor(t))
	firstRoundRequire(t, err)
	x.requireCooldownVisit(t, visit, 1)
	first.stop(t)

	// This is a newly constructed owner, not a copied in-memory countdown.
	second := x.newOwner(t, 2, policy)
	visit, err = second.runner.RunTraversal(ctxFor(t))
	requireRelaunchAdmission(t, visit, err)
	x.requireCooldownVisit(t, visit, 0)
	visit, err = second.runner.RunTraversal(ctxFor(t))
	firstRoundRequire(t, err)
	item := x.relaunchVisit(t, visit)
	firstRoundRequire(t, item.Err)
	old, next := x.original.Summary(), item.Dispatch.Summary()
	request, err := item.Dispatch.LaunchRequest()
	firstRoundRequire(t, err)
	before, err := x.original.LaunchRequest()
	firstRoundRequire(t, err)
	if item.Relaunch.CooldownSkipped || item.RelaunchRequest == nil || item.RelaunchRequest.ExpectedTaskVersion != x.task.Version ||
		next.ID == old.ID || next.ExecutionID == nil || old.ExecutionID == nil || *next.ExecutionID == *old.ExecutionID || next.Status != scheduler.Launched ||
		request.Meta.IdempotencyKey == before.Meta.IdempotencyKey || request.Meta.RequestID == before.Meta.RequestID || request.Trigger != before.Trigger || request.Purpose != "task/work" {
		t.Fatal("zero cooldown did not produce a distinct canonical relaunch for the original work phase")
	}
	second.stop(t)
	x.requireOrigin(t, item.Dispatch)
	x.awaitExecution(t, *next.ExecutionID)
	x.execution.stop(t)
	x.requireTaskUnchanged(t)
	x.requireRelaunchedTerminal(t, item.Dispatch)
}

func (x *schedulerRelaunchFixture) relaunchVisit(t *testing.T, result scheduler.ProjectRunResult) scheduler.ProjectTaskVisit {
	t.Helper()
	if len(result.Visits) != 1 || result.Visits[0].TaskID != x.task.ID || result.Visits[0].ExpectedState != wc.TaskStateInProgress || result.Visits[0].Action != scheduler.ProjectVisitRelaunch {
		t.Fatal("the actual Runner did not visit the original in_progress work phase")
	}
	return result.Visits[0]
}

func (x *schedulerRelaunchFixture) requireCooldownVisit(t *testing.T, result scheduler.ProjectRunResult, remaining int64) {
	t.Helper()
	visit := x.relaunchVisit(t, result)
	firstRoundRequire(t, visit.Err)
	if !visit.Relaunch.CooldownSkipped || visit.Relaunch.Remaining != remaining || visit.Dispatch.Summary().ID.Validate() == nil {
		t.Fatal("cooldown skipped visit created a Dispatch or lost its persisted remaining count")
	}
	x.requireTaskUnchanged(t)
	x.execution.round.requireWire(t, 1, true)
	v := x.execution.round.capture.v
	var count int64
	if visit.RelaunchRequest == nil {
		t.Fatal("cooldown visit lost its original request")
	}
	err := v.base.raw.QueryRow(ctxFor(t), `SELECT count(*) FROM agenteam_scheduler.task_runtimes r
 JOIN agenteam_scheduler.relaunch_visits v ON(v.project_id,v.task_id)=(r.project_id,r.task_id)
 WHERE r.project_id=$1 AND r.task_id=$2 AND r.latest_dispatch_id=$3 AND r.latest_execution_id=$4
 AND r.cooldown_execution_id=$4 AND r.cooldown_purpose='task/work' AND r.relaunch_skip_remaining=$5
 AND v.id=$6 AND v.outcome='cooldown_skipped' AND v.remaining=$5
 AND NOT EXISTS(SELECT 1 FROM agenteam_scheduler.dispatches d WHERE d.project_id=r.project_id AND d.id=v.id)`,
		v.base.project.ID.String(), x.task.ID.String(), x.original.Summary().ID.String(), x.original.Summary().ExecutionID.String(), remaining, visit.RelaunchRequest.DispatchID).Scan(&count)
	firstRoundRequire(t, err)
	if count != 1 {
		t.Fatal("cooldown projection did not match the original persisted terminal/visit")
	}
}

func requireRelaunchAdmission(t *testing.T, result scheduler.ProjectRunResult, err error) {
	t.Helper()
	if err == nil {
		return
	}
	var fault *f.Fault
	if !errors.As(err, &fault) || fault.Code != f.ResourceBusy || fault.CommitState == f.Unknown {
		t.Fatal("unexpected original Runner admission failure", err)
	}
	for _, visit := range result.Executions {
		if errors.Is(visit.Err, err) {
			return // Reliable association survives this bounded executor capacity refusal.
		}
	}
	t.Fatal("capacity error was not an observed executor admission")
}

func (x *schedulerRelaunchFixture) awaitExecution(t *testing.T, executionID i.ExecutionID) {
	t.Helper()
	c := x.execution.round.capture
	ctx, cancel := context.WithTimeout(ctxFor(t), 10*time.Second)
	defer cancel()
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	// This original whole-Project reader has no Relaunch option: it can redeliver
	// the new association after a transient MaxOwned refusal, never create E3.
	runner := x.execution.runner(t, true)
	defer stopSchedulerExecutionRunner(t, runner)
	for {
		var status string
		err := c.v.base.raw.QueryRow(ctx, `SELECT status FROM agenteam_execution.executions
 WHERE id=$1 AND project_id=$2 AND agent_id=$3`, executionID.String(), c.v.base.project.ID.String(), c.v.agentID.String()).Scan(&status)
		firstRoundRequire(t, err)
		if status == string(ec.Succeeded) {
			return
		}
		result, err := runner.RunTraversal(ctx)
		requireRelaunchAdmission(t, result, err)
		select {
		case <-tick.C:
		case <-ctx.Done():
			t.Fatal("the new reliable relaunch did not reach its actual terminal state")
		}
	}
}

func (x *schedulerRelaunchFixture) requireRelaunchedTerminal(t *testing.T, dispatch scheduler.Dispatch) {
	t.Helper()
	v := x.execution.round.capture.v
	s := dispatch.Summary()
	request, err := dispatch.LaunchRequest()
	firstRoundRequire(t, err)
	var raw []byte
	var digest string
	var counts [7]int64
	err = v.base.raw.QueryRow(ctxFor(t), `SELECT p.input,p.input_digest,
 (SELECT count(*) FROM agenteam_work.task_scheduler_claims c WHERE c.project_id=$1::text::uuid),
 (SELECT count(*) FROM agenteam_scheduler.dispatches d WHERE d.project_id=$1 AND d.status='launched'),
 (SELECT count(*) FROM agenteam_execution.executions n WHERE n.project_id=$1 AND n.status='succeeded'),
 (SELECT count(*) FROM agenteam_execution.preparation_inputs n WHERE n.project_id=$1),
 (SELECT count(*) FROM agenteam_model.calls c JOIN agenteam_secret.secret_leases l ON l.id=c.lease_id
  WHERE c.project_id=$1::text::uuid AND c.retired AND c.phase='succeeded' AND l.owner_kind='execution' AND l.released),
 (SELECT count(*) FROM agenteam_secret.project_variable_execution_leases l WHERE l.project_id=$1 AND l.released AND l.released_at IS NOT NULL),
 (SELECT count(*) FROM agenteam_execution.executions n WHERE n.project_id=$1 AND n.status IN('created','preparing','running','waiting'))
 FROM agenteam_execution.executions e JOIN agenteam_execution.preparation_inputs p ON(p.execution_id,p.project_id,p.agent_id)=(e.id,e.project_id,e.agent_id)
 JOIN agenteam_execution.snapshots s ON(s.execution_id,s.id)=(e.id,e.snapshot_id)
 JOIN agenteam_execution.rounds r ON(r.execution_id,r.snapshot_id,r.start_id)=(e.id,s.id,s.start_id)
 JOIN agenteam_scheduler.task_runtimes rt ON(rt.project_id,rt.task_id,rt.latest_execution_id)=(e.project_id,$3,e.id)
 WHERE e.id=$2 AND e.project_id=$1 AND e.status='succeeded' AND r.terminal_status=e.status
 AND rt.latest_dispatch_id=$4 AND rt.cooldown_execution_id IS NULL AND rt.relaunch_skip_remaining=0
 AND r.terminal_version=e.version AND r.finished_at=e.completed_at AND r.transcript_through=2`,
		v.base.project.ID.String(), s.ExecutionID.String(), x.task.ID.String(), s.ID.String()).Scan(&raw, &digest, &counts[0], &counts[1], &counts[2], &counts[3], &counts[4], &counts[5], &counts[6])
	firstRoundRequire(t, err)
	input, err := ec.DecodePreparationInput(raw)
	firstRoundRequire(t, err)
	task, err := work.DecodeCapturedTaskInput(input.Fields().Trigger, input.Fields().Request)
	firstRoundRequire(t, err)
	if counts != ([7]int64{1, 2, 2, 2, 2, 2, 0}) || digest != string(input.Digest()) ||
		!input.Fields().Request.Equal(ec.PreparationRequest{ExecutionID: *s.ExecutionID, Launch: request}) || !bytes.Equal(jsonBytes(t, task.Task()), jsonBytes(t, x.task)) {
		t.Fatal("relaunch repeated todo Claim or lost the new canonical input, original Task and terminal lease facts")
	}
	x.execution.round.requireWire(t, 2, true)
	x.execution.round.reader.requireDestroyed(t, 2)
}

func (x *schedulerRelaunchFixture) requireOrigin(t *testing.T, dispatch scheduler.Dispatch) wc.TaskRelaunchSource {
	t.Helper()
	v := x.execution.round.capture.v
	s := dispatch.Summary()
	var sourceRaw, taskRaw []byte
	var count int64
	err := v.base.raw.QueryRow(ctxFor(t), `SELECT d.relaunch_source,r.record->'task',
 (SELECT count(*) FROM agenteam_work.task_scheduler_claims WHERE project_id=r.project_id)
 FROM agenteam_work.task_scheduler_relaunches r
 JOIN agenteam_scheduler.dispatches d ON(d.project_id,d.id)=(r.project_id::text,r.id::text)
 JOIN agenteam_scheduler.relaunch_visits v ON(v.project_id,v.task_id,v.id)=(d.project_id,d.task_id,d.id)
 WHERE r.project_id=$1::text::uuid AND r.id=$2::text::uuid AND r.task_id=$3::text::uuid
 AND r.task_version=$4 AND r.agent_id=d.agent_id::uuid AND r.sprint_id=d.sprint_id::uuid AND r.request_id=d.request_id::uuid
 AND r.purpose='task/work' AND d.claim_guard IS NULL AND d.claim_source_sprint_id IS NULL
 AND d.claim_source_state IS NULL AND d.claim_source_priority IS NULL
 AND r.source_digest=convert_from(d.relaunch_source,'UTF8')::jsonb->>'ReferenceDigest'
 AND r.record->'request'=convert_from(d.relaunch_source,'UTF8')::jsonb->'Request'
 AND v.outcome='dispatch_created' AND v.remaining=0
 AND (SELECT count(*) FROM agenteam_work.task_scheduler_relaunches WHERE project_id=r.project_id)=1`,
		v.base.project.ID.String(), s.ID.String(), x.task.ID.String(), int64(x.task.Version)).Scan(&sourceRaw, &taskRaw, &count)
	firstRoundRequire(t, err)
	var source wc.TaskRelaunchSource
	var before wc.Task
	firstRoundRequire(t, json.Unmarshal(sourceRaw, &source))
	firstRoundRequire(t, json.Unmarshal(taskRaw, &before))
	firstRoundRequire(t, source.Validate())
	if count != 1 || source.Request.DispatchID != s.ID.String() || source.Request.ExpectedTaskVersion != x.task.Version ||
		source.Request.TaskID != x.task.ID || source.Request.AgentID != v.agentID || source.MilestoneID != x.task.MilestoneID || !reflect.DeepEqual(before, x.task) {
		t.Fatal("new Dispatch lost its immutable Work relaunch origin or invented a todo claim")
	}
	return source
}

type schedulerRelaunchFailureFixture struct {
	parent  *schedulerRelaunchFixture
	failure *schedulerFailureFixture
	source  wc.TaskRelaunchSource
}

func runSchedulerRelaunchFailure(t *testing.T) {
	x := newSchedulerRelaunchFixture(t)
	policy := x.execution.round.capture.launchPolicy.Clone()
	// Canonical nonempty constraints have a real, request-bound permanent
	// rejection in the Task provider. A generic error code is not this proof.
	policy.AllowedResourceConstraints = []json.RawMessage{json.RawMessage(`{}`)}
	owner := x.newOwner(t, 0, policy)
	result, err := owner.runner.RunTraversal(ctxFor(t))
	firstRoundRequire(t, err)
	visit := x.relaunchVisit(t, result)
	request, err := visit.Dispatch.LaunchRequest()
	firstRoundRequire(t, err)
	firstRoundRequire(t, request.Validate())
	s := visit.Dispatch.Summary()
	reason, exact := wc.MatchTaskLaunchFailure(visit.Err, request)
	if !exact || reason != wc.TaskLaunchFailureUnsupportedResourceConstraints || len(request.Policy.AllowedResourceConstraints) != 1 ||
		s.Status != scheduler.Pending || s.LaunchOutcome != scheduler.KnownNotCreated || s.ExecutionID != nil ||
		s.AttemptCount != 1 || s.FailureReason != reason || s.FailureCode != f.DependencyUnbound || s.FailureOccurredAt == nil {
		t.Fatal("real relaunch rejection did not retain the original private permanent proof")
	}
	owner.stop(t)
	x.execution.stop(t)
	x.requireTaskUnchanged(t)
	y := &schedulerRelaunchFailureFixture{parent: x, source: x.requireOrigin(t, visit.Dispatch),
		failure: &schedulerFailureFixture{v: x.execution.round.capture.v, dispatch: s.ID, request: request, rejected: s}}
	y.failure.finalizer = newSchedulerFailureFinalizer(t, y.failure.v)
	stable, origins := y.snapshot(t), y.originFacts(t)
	command, err := f.NewCommandIdentity("scheduler", []string{request.ProjectID.String()}, "finalize_launch_failure", request.Meta.IdempotencyKey)
	firstRoundRequire(t, err)
	marker := errors.New("relaunch-final-failure-late-rollback")
	store := y.failure.v.base.tracked
	var seen, returned bool
	var physical f.CommitResult
	store.setAfter(func(ctx context.Context, tx f.Tx, cause f.TransactionCause) error {
		if cause.Kind() != f.CommandsCause || cause.Details().Primary.Canonical() != command.Canonical() {
			return nil
		}
		sql, err := store.InTx(tx)
		if err != nil {
			return err
		}
		if err = y.settledFacts(ctx, sql); err != nil {
			return err
		}
		seen = true
		return marker
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
	out, err := y.failure.finalizer.FinalizeLaunchFailure(ctxFor(t), request.ProjectID, s.ID)
	clear()
	if !seen || !returned || physical.State() != f.NotCommitted || !errors.Is(physical.Fault(), marker) || !errors.Is(err, marker) || out.Summary().Status == scheduler.Failed {
		t.Fatal("original relaunch final transaction did not physically roll back at the late marker", err)
	}
	if stable != y.snapshot(t) {
		t.Fatal("relaunch rollback leaked Task/blocker/history/event/Dispatch or origin writes")
	}
	x.requireTaskUnchanged(t)
	settled, err := y.failure.finalizer.FinalizeLaunchFailure(ctxFor(t), request.ProjectID, s.ID)
	firstRoundRequire(t, err)
	if settled.Summary().Status != scheduler.Failed || settled.Summary().ID != s.ID || settled.Summary().Version != s.Version+1 || settled.Summary().AttemptCount != 1 {
		t.Fatal("same original relaunch did not settle after known rollback")
	}
	current := y.failure.currentTask(t)
	want := x.task.Clone()
	want.State, want.Version, want.UpdatedAt, want.ManualRank = wc.TaskStateBlocked, x.task.Version+1, current.UpdatedAt, current.ManualRank
	if !reflect.DeepEqual(want, current) || origins != y.originFacts(t) {
		t.Fatal("relaunch settlement rewrote original Work fields, origins, or latest Execution")
	}
	firstRoundRequire(t, y.settledFacts(ctxFor(t), y.failure.v.base.raw))
	stable = y.snapshot(t)
	replay, err := y.failure.finalizer.FinalizeLaunchFailure(ctxFor(t), request.ProjectID, s.ID)
	firstRoundRequire(t, err)
	observed, err := y.failure.finalizer.Lookup(ctxFor(t), request.ProjectID, s.ID)
	firstRoundRequire(t, err)
	if !reflect.DeepEqual(replay.Summary(), settled.Summary()) || !reflect.DeepEqual(observed.Summary(), settled.Summary()) || stable != y.snapshot(t) {
		t.Fatal("original relaunch final result lookup/replay repeated a mutation")
	}
	x.execution.round.requireWire(t, 1, true)
	x.execution.round.reader.requireDestroyed(t, 1)
}

func (x *schedulerRelaunchFailureFixture) originFacts(t *testing.T) string {
	t.Helper()
	var raw string
	err := x.failure.v.base.raw.QueryRow(ctxFor(t), `SELECT jsonb_build_object(
 'claims',(SELECT coalesce(jsonb_agg(to_jsonb(c) ORDER BY id),'[]'::jsonb) FROM agenteam_work.task_scheduler_claims c WHERE project_id=$1::text::uuid),
 'relaunches',(SELECT coalesce(jsonb_agg(to_jsonb(r) ORDER BY id),'[]'::jsonb) FROM agenteam_work.task_scheduler_relaunches r WHERE project_id=$1::text::uuid),
 'runtimes',(SELECT coalesce(jsonb_agg(to_jsonb(r) ORDER BY task_id),'[]'::jsonb) FROM agenteam_scheduler.task_runtimes r WHERE project_id=$1),
 'visits',(SELECT coalesce(jsonb_agg(to_jsonb(v) ORDER BY id),'[]'::jsonb) FROM agenteam_scheduler.relaunch_visits v WHERE project_id=$1)
 )::text`, x.failure.request.ProjectID.String()).Scan(&raw)
	firstRoundRequire(t, err)
	return raw
}

func (x *schedulerRelaunchFailureFixture) snapshot(t *testing.T) string {
	t.Helper()
	return x.failure.snapshot(t) + "\n" + x.originFacts(t)
}

// Read the actual completed writers inside the caller Tx before injecting rollback.
func (x *schedulerRelaunchFailureFixture) settledFacts(ctx context.Context, sql postgres.SQLExecutor) error {
	y := x.failure
	before := x.parent.task.Version
	var counts [10]int64
	err := sql.QueryRow(ctx, `SELECT
 (SELECT count(*) FROM agenteam_work.task_launch_failures c
  JOIN agenteam_work.tasks t ON t.project_id=c.project_id AND t.id=c.task_id
  JOIN agenteam_work.task_scheduler_relaunches o ON(o.project_id,o.id)=(c.project_id,c.relaunch_operation_id)
  JOIN agenteam_scheduler.dispatches d ON d.project_id=c.project_id::text AND d.id=c.id::text
  WHERE c.project_id=$1::text::uuid AND c.id=$2::text::uuid AND c.task_id=$3::text::uuid
   AND c.agent_id=$4::text::uuid AND c.request_id=$5::text::uuid AND c.dispatch_version=$6 AND c.launch_attempt=1
   AND c.claim_operation_id IS NULL AND c.relaunch_operation_id=c.id
   AND o.task_id=c.task_id AND o.agent_id=c.agent_id AND o.request_id=c.request_id AND o.task_version=$7
   AND d.claim_guard IS NULL AND d.relaunch_source IS NOT NULL
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
   AND e.producer='work' AND e.event_type='work.task_transitioned' AND e.schema_version=5),
 (SELECT count(*) FROM agenteam_work.task_launch_failures WHERE project_id=$1::text::uuid),
 (SELECT count(*) FROM agenteam_execution.executions WHERE project_id=$1::text),
 (SELECT count(*) FROM agenteam_scheduler.dispatches WHERE project_id=$1::text AND status='pending'),
 (SELECT count(*) FROM agenteam_work.task_blockers WHERE project_id=$1::text::uuid)`,
		y.request.ProjectID.String(), y.dispatch.String(), y.v.task.ID.String(), y.request.AgentID.String(), y.request.Meta.RequestID.String(), int64(y.rejected.Version), int64(before), int64(before+1), y.v.task.SprintID.String()).Scan(
		&counts[0], &counts[1], &counts[2], &counts[3], &counts[4], &counts[5], &counts[6], &counts[7], &counts[8], &counts[9])
	if err != nil {
		return err
	}
	if counts != ([10]int64{1, 1, 1, 1, 2, 1, 1, 1, 0, 1}) {
		return fmt.Errorf("atomic final failure counts %v", counts)
	}
	var raw []byte
	var blocker string
	var history []string
	err = sql.QueryRow(ctx, `SELECT e.payload,c.blocker_id::text,
 (SELECT array_agg(h.id::text ORDER BY h.id) FROM agenteam_work.task_events h WHERE h.project_id=c.project_id AND h.failure_operation_id=c.id)
 FROM agenteam_work.task_launch_failures c JOIN agenteam_outbox.events e ON e.id=c.event_id
 WHERE c.project_id=$1::text::uuid AND c.id=$2::text::uuid`, y.request.ProjectID.String(), y.dispatch.String()).Scan(&raw, &blocker, &history)
	if err != nil {
		return err
	}
	var payload wc.TaskRelaunchFailed
	if json.Unmarshal(raw, &payload) != nil || payload.Validate() != nil {
		return errors.New("invalid typed final failure event")
	}
	ids := make([]string, len(payload.TaskEventIDs))
	for n, id := range payload.TaskEventIDs {
		ids[n] = id.String()
	}
	if payload.DispatchID.String() != y.dispatch.String() || payload.Origin != wc.TaskDispatchRelaunch || payload.Source != x.source || payload.Actor.CauseID != y.dispatch.String() || payload.BlockerID.String() != blocker || !reflect.DeepEqual(ids, history) ||
		payload.MilestoneID != y.v.task.MilestoneID || payload.SprintID != y.v.task.SprintID || payload.AgentID != y.request.AgentID || payload.Reason != wc.TaskLaunchFailureUnsupportedResourceConstraints || payload.FromState != wc.TaskStateInProgress || payload.ToState != wc.TaskStateBlocked {
		return errors.New("typed final event lost actual current Task and history identities")
	}
	return nil
}
