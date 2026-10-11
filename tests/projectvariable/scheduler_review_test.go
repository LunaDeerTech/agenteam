//go:build integration

package projectvariable_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	ac "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/LunaDeerTech/agenteam/internal/central/scheduler"
	"github.com/LunaDeerTech/agenteam/internal/central/work"
	wc "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
)

func TestSchedulerReviewDispatch(t *testing.T) {
	t.Run("phase-isolated-review-execution", runSchedulerReviewExecution)
	t.Run("review-failure-atomic-block-and-replay", runSchedulerReviewFailure)
}

type schedulerReviewFixture struct {
	parent *schedulerRelaunchFixture
	human  *taskReviewFixture
}

func newSchedulerReviewFixture(t *testing.T) *schedulerReviewFixture {
	t.Helper()
	x := newSchedulerRelaunchFixture(t) // A real work Execution has succeeded; its executor stays owned.
	workOwner := x.newOwner(t, 2, x.execution.round.capture.launchPolicy)
	visited, err := workOwner.runner.RunTraversal(ctxFor(t))
	requireSchedulerReview(t, err)
	x.requireCooldownVisit(t, visited, 1)
	workOwner.stop(t)

	v := x.execution.round.capture.v
	request, command, _ := v.agent.request(t)
	fields := request.Fields()
	fields.Name = "automatic-reviewer"
	fields.InjectAgentsMD = false
	fields.AllowedSecretVariableIDs = []i.ProjectVariableID{x.execution.round.capture.secret.Fields().ID}
	request, err = ac.NewAgentCreate(fields)
	requireSchedulerReview(t, err)
	created, err := v.agent.agents.CreateAgent(ctxFor(t), v.base.ownerBrowser.actor, command, v.base.project.ID, request)
	requireSchedulerReview(t, err)
	if created.Validate() != nil || created.Fields().Agent.Fields().Core.ID != fields.AgentID || fields.AgentID == v.agentID {
		t.Fatal("formal Agent creation did not produce a distinct current reviewer")
	}
	human := &taskReviewFixture{domain: v, reviewer: fields.AgentID}
	before := human.current(t)
	transfer, meta, lookup := human.intent(t, before, wc.TaskStateInReview, &human.reviewer, "Review the completed work through the configured reviewer.", "automatic-review-handoff")
	generations := human.generations(t, before, transfer.TargetState)
	review, err := v.transitions.TransferTask(ctxFor(t), v.base.ownerBrowser.actor, meta, before.ProjectID, before.ID, transfer)
	requireSchedulerReview(t, err)
	human.requireCommitted(t, before, transfer, meta, lookup, generations, nil, review)
	if review.Task.State != wc.TaskStateInReview || review.Task.AssigneeAgentID == nil || *review.Task.AssigneeAgentID != human.reviewer {
		t.Fatal("Human handoff did not persist the current reviewer")
	}
	// This is only an assertion baseline. All original services, authorities,
	// actors, launch policy and the running executor remain the same instances.
	x.task = review.Task
	x.workFacts = x.readWorkFacts(t)
	out := &schedulerReviewFixture{parent: x, human: human}
	out.requireRuntime(t, nil, "task/work", 1)
	return out
}

func requireSchedulerReview(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		failSchedulerRelaunch(t, err) // Existing bounded code/SQLSTATE/constraint diagnostics; no private payload.
	}
}

func (x *schedulerReviewFixture) visit(t *testing.T, result scheduler.ProjectRunResult) scheduler.ProjectTaskVisit {
	t.Helper()
	if len(result.Visits) != 1 || result.Visits[0].TaskID != x.parent.task.ID || result.Visits[0].ExpectedState != wc.TaskStateInReview || result.Visits[0].Action != scheduler.ProjectVisitRelaunch {
		t.Fatal("Runner did not visit the actual in_review phase")
	}
	visit := result.Visits[0]
	if visit.RelaunchRequest == nil || visit.RelaunchRequest.Purpose != "task/review" || visit.RelaunchRequest.AgentID != x.human.reviewer || visit.RelaunchRequest.ExpectedTaskVersion != x.parent.task.Version {
		t.Fatal("review traversal lost the original current Task, purpose or reviewer")
	}
	return visit
}

// The work pair must remain intact while the review pair advances. A shared
// countdown can authorize only the phase named by its persisted purpose.
func (x *schedulerReviewFixture) requireRuntime(t *testing.T, review *scheduler.Dispatch, purpose string, remaining int64) {
	t.Helper()
	v, old := x.human.domain, x.parent.original.Summary()
	var workD, workE string
	var reviewD, reviewE, cooldownE, cooldownPurpose *string
	var count int64
	err := v.base.raw.QueryRow(ctxFor(t), `SELECT latest_dispatch_id,latest_execution_id,
 latest_review_dispatch_id,latest_review_execution_id,cooldown_execution_id,cooldown_purpose,relaunch_skip_remaining
 FROM agenteam_scheduler.task_runtimes WHERE project_id=$1 AND task_id=$2`, v.base.project.ID.String(), x.parent.task.ID.String()).
		Scan(&workD, &workE, &reviewD, &reviewE, &cooldownE, &cooldownPurpose, &count)
	requireSchedulerReview(t, err)
	if workD != old.ID.String() || workE != old.ExecutionID.String() || count != remaining {
		t.Fatal("review rewrote the work phase pointer or lost the durable visit count")
	}
	if review == nil {
		if reviewD != nil || reviewE != nil {
			t.Fatal("Human handoff invented a Scheduler review association")
		}
	} else if reviewD == nil || reviewE == nil || *reviewD != review.Summary().ID.String() || *reviewE != review.Summary().ExecutionID.String() {
		t.Fatal("review association did not persist its independent latest pair")
	}
	if purpose == "" {
		if cooldownE != nil || cooldownPurpose != nil || remaining != 0 {
			t.Fatal("reliable review association retained a previous cooldown")
		}
		return
	}
	wantExecution := workE
	if purpose == "task/review" && reviewE != nil {
		wantExecution = *reviewE
	}
	if cooldownE == nil || *cooldownE != wantExecution || cooldownPurpose == nil || *cooldownPurpose != purpose {
		t.Fatal("cooldown refers to a different phase or terminal Execution")
	}
}

func runSchedulerReviewExecution(t *testing.T) {
	x := newSchedulerReviewFixture(t)
	p := x.parent
	owner := p.newOwner(t, 2, p.execution.round.capture.launchPolicy)
	result, err := owner.runner.RunTraversal(ctxFor(t))
	requireRelaunchAdmission(t, result, err)
	visit := x.visit(t, result)
	requireSchedulerReview(t, visit.Err)
	s := visit.Dispatch.Summary()
	request, err := visit.Dispatch.LaunchRequest()
	requireSchedulerReview(t, err)
	old, err := p.original.LaunchRequest()
	requireSchedulerReview(t, err)
	if visit.Relaunch.CooldownSkipped || s.Status != scheduler.Launched || s.ExecutionID == nil || *s.ExecutionID == *p.original.Summary().ExecutionID || s.ID == p.original.Summary().ID ||
		request.Purpose != "task/review" || request.AgentID != x.human.reviewer || request.Trigger != old.Trigger || request.Meta.IdempotencyKey == old.Meta.IdempotencyKey || request.Meta.RequestID == old.Meta.RequestID {
		t.Fatal("first review inherited the work cooldown or repeated the original claim/launch")
	}
	owner.stop(t)
	x.requireOrigin(t, visit.Dispatch)
	x.requireRuntime(t, &visit.Dispatch, "", 0)
	x.awaitReview(t, visit.Dispatch)
	p.requireTaskUnchanged(t)
	x.requireReviewTerminal(t, visit.Dispatch)

	// Count two true Task visits, across separately constructed owners. The
	// historical Execution page itself must not consume this phase's count.
	first := p.newOwner(t, 2, request.Policy)
	result, err = first.runner.RunTraversal(ctxFor(t))
	requireSchedulerReview(t, err)
	x.requireReviewSkip(t, result, visit.Dispatch, 1)
	first.stop(t)
	second := p.newOwner(t, 2, request.Policy)
	result, err = second.runner.RunTraversal(ctxFor(t))
	requireSchedulerReview(t, err)
	x.requireReviewSkip(t, result, visit.Dispatch, 0)
	second.stop(t)
	p.execution.stop(t)

	v := x.human.domain
	before := x.human.current(t)
	transfer, command, lookup := x.human.intent(t, before, wc.TaskStateDone, nil, "Reviewed and accepted after the real review execution.", "automatic-review-accept")
	generations := x.human.generations(t, before, transfer.TargetState)
	done, err := v.transitions.TransferTask(ctxFor(t), v.base.ownerBrowser.actor, command, before.ProjectID, before.ID, transfer)
	requireSchedulerReview(t, err)
	x.human.requireCommitted(t, before, transfer, command, lookup, generations, nil, done)
	if done.Task.AssigneeAgentID == nil || *done.Task.AssigneeAgentID != x.human.reviewer {
		t.Fatal("Human done exit lost the reviewer")
	}
	stable := x.human.snapshot(t)
	found, err := v.transitions.LookupTaskTransition(ctxFor(t), v.base.ownerBrowser.actor, lookup)
	requireSchedulerReview(t, err)
	if found.Status != wc.LookupCommitted || found.Receipt == nil || !reflect.DeepEqual(*found.Receipt, done) || stable != x.human.snapshot(t) {
		t.Fatal("Human done lookup lost its original receipt or rewrote facts")
	}
	p.execution.round.requireWire(t, 2, true)
	p.execution.round.reader.requireDestroyed(t, 2)
}

func (x *schedulerReviewFixture) requireReviewSkip(t *testing.T, result scheduler.ProjectRunResult, dispatch scheduler.Dispatch, remaining int64) {
	t.Helper()
	visit := x.visit(t, result)
	requireSchedulerReview(t, visit.Err)
	if !visit.Relaunch.CooldownSkipped || visit.Relaunch.Remaining != remaining || visit.Dispatch.Summary().ID.Validate() == nil {
		t.Fatal("review cooldown visit created a new Dispatch or skipped its durable count")
	}
	x.requireRuntime(t, &dispatch, "task/review", remaining)
	x.parent.requireTaskUnchanged(t)
	var counts [4]int64
	v := x.human.domain
	err := v.base.raw.QueryRow(ctxFor(t), `SELECT
 (SELECT count(*) FROM agenteam_scheduler.relaunch_visits WHERE project_id=$1 AND task_id=$2 AND id=$3 AND outcome='cooldown_skipped' AND remaining=$4),
 (SELECT count(*) FROM agenteam_scheduler.dispatches WHERE project_id=$1),
 (SELECT count(*) FROM agenteam_execution.executions WHERE project_id=$1),
 (SELECT count(*) FROM agenteam_work.task_scheduler_claims WHERE project_id=$1::text::uuid)`,
		v.base.project.ID.String(), x.parent.task.ID.String(), visit.RelaunchRequest.DispatchID, remaining).Scan(&counts[0], &counts[1], &counts[2], &counts[3])
	requireSchedulerReview(t, err)
	if counts != ([4]int64{1, 2, 2, 1}) {
		t.Fatal("review visit repeated a claim/Execution or lost its immutable receipt")
	}
}

func (x *schedulerReviewFixture) awaitReview(t *testing.T, dispatch scheduler.Dispatch) {
	t.Helper()
	v, s := x.human.domain, dispatch.Summary()
	ctx, cancel := context.WithTimeout(ctxFor(t), 10*time.Second)
	defer cancel()
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	// This existing runner has no Relaunch option. It only redelivers reliable
	// associations after a bounded executor admission refusal, never creates E3.
	runner := x.parent.execution.runner(t, true)
	defer stopSchedulerExecutionRunner(t, runner)
	for {
		var status string
		err := v.base.raw.QueryRow(ctx, `SELECT status FROM agenteam_execution.executions WHERE id=$1 AND project_id=$2 AND agent_id=$3`, s.ExecutionID.String(), v.base.project.ID.String(), x.human.reviewer.String()).Scan(&status)
		requireSchedulerReview(t, err)
		if status == string(ec.Succeeded) {
			return
		}
		if status == string(ec.Failed) || status == string(ec.Cancelled) {
			t.Fatal("real review Execution terminated without a successful review Model response")
		}
		result, err := runner.RunTraversal(ctx)
		requireRelaunchAdmission(t, result, err)
		select {
		case <-tick.C:
		case <-ctx.Done():
			t.Fatal("real review Execution did not terminate within the original bounded wait")
		}
	}
}

func (x *schedulerReviewFixture) requireReviewTerminal(t *testing.T, dispatch scheduler.Dispatch) {
	t.Helper()
	v, s := x.human.domain, dispatch.Summary()
	var inputRaw, snapshotRaw, roundRaw []byte
	var inputDigest, snapshotDigest, roundDigest string
	var counts [8]int64
	err := v.base.raw.QueryRow(ctxFor(t), `SELECT p.input,p.input_digest,s.content,s.digest,r.content,r.digest,
 (SELECT count(*) FROM agenteam_work.task_scheduler_claims WHERE project_id=$1::text::uuid),
 (SELECT count(*) FROM agenteam_scheduler.dispatches WHERE project_id=$1 AND status='launched'),
 (SELECT count(*) FROM agenteam_execution.executions WHERE project_id=$1 AND status='succeeded'),
 (SELECT count(*) FROM agenteam_execution.preparation_inputs WHERE project_id=$1),
 (SELECT count(*) FROM agenteam_model.calls c JOIN agenteam_secret.secret_leases l ON l.id=c.lease_id WHERE c.project_id=$1::text::uuid AND c.phase='succeeded' AND c.retired AND l.owner_kind='execution' AND l.released),
 (SELECT count(*) FROM agenteam_secret.project_variable_execution_leases WHERE project_id=$1 AND released AND released_at IS NOT NULL),
 (SELECT count(*) FROM agenteam_execution.executions WHERE project_id=$1 AND status IN('created','preparing','running','waiting')),
 (SELECT count(*) FROM agenteam_model.calls c JOIN agenteam_model.invocations i ON i.id=c.invocation_id
  WHERE c.id=r.call_id::uuid AND c.request_data#>>'{Initiator,AgentID}'=$3 AND c.request_data#>>'{Initiator,ExecutionID}'=$2
  AND c.phase='succeeded' AND c.retired AND i.final_status='succeeded' AND i.usage_source='provider' AND i.input_tokens=3 AND i.output_tokens=2 AND i.total_tokens=5)
 FROM agenteam_execution.executions e JOIN agenteam_execution.preparation_inputs p ON(p.execution_id,p.project_id,p.agent_id)=(e.id,e.project_id,e.agent_id)
 JOIN agenteam_execution.snapshots s ON(s.execution_id,s.id)=(e.id,e.snapshot_id)
 JOIN agenteam_execution.rounds r ON(r.execution_id,r.snapshot_id,r.start_id)=(e.id,s.id,s.start_id)
 WHERE e.id=$2 AND e.project_id=$1 AND e.agent_id=$3 AND e.launch_request->>'purpose'='task/review' AND e.status='succeeded'
 AND r.terminal_status=e.status AND r.terminal_version=e.version AND r.finished_at=e.completed_at AND r.transcript_through=2`,
		v.base.project.ID.String(), s.ExecutionID.String(), x.human.reviewer.String()).Scan(&inputRaw, &inputDigest, &snapshotRaw, &snapshotDigest, &roundRaw, &roundDigest,
		&counts[0], &counts[1], &counts[2], &counts[3], &counts[4], &counts[5], &counts[6], &counts[7])
	requireSchedulerReview(t, err)
	input, err := ec.DecodePreparationInput(inputRaw)
	requireSchedulerReview(t, err)
	snapshot, err := ec.DecodeDirectTextSnapshot(snapshotRaw)
	requireSchedulerReview(t, err)
	round, err := ec.DecodeDirectTextRound(roundRaw)
	requireSchedulerReview(t, err)
	captured := snapshot.Fields().Context
	task, err := work.DecodeTaskContext(captured.Trigger())
	requireSchedulerReview(t, err)
	request, err := dispatch.LaunchRequest()
	requireSchedulerReview(t, err)
	if counts != ([8]int64{1, 2, 2, 2, 2, 2, 0, 1}) || inputDigest != input.Digest().String() || snapshotDigest != snapshot.Digest().String() || roundDigest != round.Digest().String() ||
		!bytes.Equal(input.CanonicalBytes(), captured.Input().CanonicalBytes()) || !input.Fields().Request.Equal(ec.PreparationRequest{ExecutionID: *s.ExecutionID, Launch: request}) ||
		task.Purpose() != "task/review" || !reflect.DeepEqual(task.Task(), x.parent.task) || captured.Trigger().Instructions().Revision() != work.TaskReviewPromptRevision ||
		input.Fields().Agent.Fields().Core.ID != x.human.reviewer || round.Fields().ContextDigest != captured.Digest() || round.Fields().ExecutionID != *s.ExecutionID {
		t.Fatal("review lost its fixed Task/reviewer/prompt or repeated the original work claim")
	}
	messages := round.Fields().Messages
	if len(messages) != 2 || messages[0].Parts[0].Text == nil || !strings.Contains(messages[0].Parts[0].Text.Text, captured.Trigger().Instructions().Content()) {
		t.Fatal("sealed review Round did not contain its versioned TaskReviewPrompt")
	}
	wire := x.parent.execution.round.requireWire(t, 2, true)
	var sent struct {
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
	}
	requireSchedulerReview(t, json.Unmarshal([]byte(wire.Requests[1].Body), &sent))
	if len(sent.Messages) != 2 || sent.Messages[0].Role != "system" || sent.Messages[0].Content != messages[0].Parts[0].Text.Text || sent.Messages[1].Role != "user" || sent.Messages[1].Content != messages[1].Parts[0].Text.Text {
		t.Fatal("actual HTTPS review Model input differed from the sealed review Round")
	}
	x.requireRuntime(t, &dispatch, "", 0)
}

func (x *schedulerReviewFixture) requireOrigin(t *testing.T, dispatch scheduler.Dispatch) wc.TaskRelaunchSource {
	t.Helper()
	v := x.human.domain
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
 AND r.purpose='task/review' AND d.claim_guard IS NULL AND d.claim_source_sprint_id IS NULL
 AND d.claim_source_state IS NULL AND d.claim_source_priority IS NULL
 AND r.source_digest=convert_from(d.relaunch_source,'UTF8')::jsonb->>'ReferenceDigest'
 AND r.record->'request'=convert_from(d.relaunch_source,'UTF8')::jsonb->'Request'
 AND v.outcome='dispatch_created' AND v.remaining=0
 AND (SELECT count(*) FROM agenteam_work.task_scheduler_relaunches WHERE project_id=r.project_id)=1`,
		v.base.project.ID.String(), s.ID.String(), x.parent.task.ID.String(), int64(x.parent.task.Version)).Scan(&sourceRaw, &taskRaw, &count)
	requireSchedulerReview(t, err)
	var source wc.TaskRelaunchSource
	var before wc.Task
	requireSchedulerReview(t, json.Unmarshal(sourceRaw, &source))
	requireSchedulerReview(t, json.Unmarshal(taskRaw, &before))
	requireSchedulerReview(t, source.Validate())
	if count != 1 || source.Request.DispatchID != s.ID.String() || source.Request.ExpectedTaskVersion != x.parent.task.Version ||
		source.Request.TaskID != x.parent.task.ID || source.Request.Purpose != "task/review" || source.Request.AgentID != x.human.reviewer || source.MilestoneID != x.parent.task.MilestoneID || !reflect.DeepEqual(before, x.parent.task) {
		t.Fatal("new Dispatch lost its immutable Work relaunch origin or invented a todo claim")
	}
	return source
}

type schedulerReviewFailureFixture struct {
	parent  *schedulerReviewFixture
	failure *schedulerFailureFixture
	source  wc.TaskRelaunchSource
}

func runSchedulerReviewFailure(t *testing.T) {
	x := newSchedulerReviewFixture(t)
	policy := x.parent.execution.round.capture.launchPolicy.Clone()
	// Canonical nonempty constraints have a real, request-bound permanent
	// rejection in the Task provider. A generic error code is not this proof.
	policy.AllowedResourceConstraints = []json.RawMessage{json.RawMessage(`{}`)}
	owner := x.parent.newOwner(t, 0, policy)
	result, err := owner.runner.RunTraversal(ctxFor(t))
	requireSchedulerReview(t, err)
	visit := x.visit(t, result)
	if visit.Dispatch.Summary().ID.Validate() != nil {
		failSchedulerRelaunch(t, visit.Err)
	}
	request, err := visit.Dispatch.LaunchRequest()
	requireSchedulerReview(t, err)
	requireSchedulerReview(t, request.Validate())
	s := visit.Dispatch.Summary()
	reason, exact := wc.MatchTaskLaunchFailure(visit.Err, request)
	if !exact || reason != wc.TaskLaunchFailureUnsupportedResourceConstraints || len(request.Policy.AllowedResourceConstraints) != 1 ||
		s.Status != scheduler.Pending || s.LaunchOutcome != scheduler.KnownNotCreated || s.ExecutionID != nil ||
		s.AttemptCount != 1 || s.FailureReason != reason || s.FailureCode != f.DependencyUnbound || s.FailureOccurredAt == nil {
		t.Fatal("real relaunch rejection did not retain the original private permanent proof")
	}
	owner.stop(t)
	x.parent.execution.stop(t)
	x.parent.requireTaskUnchanged(t)
	y := &schedulerReviewFailureFixture{parent: x, source: x.requireOrigin(t, visit.Dispatch),
		failure: &schedulerFailureFixture{v: x.parent.execution.round.capture.v, dispatch: s.ID, request: request, rejected: s}}
	y.failure.finalizer = newSchedulerFailureFinalizer(t, y.failure.v)
	stable, origins := y.snapshot(t), y.originFacts(t)
	command, err := f.NewCommandIdentity("scheduler", []string{request.ProjectID.String()}, "finalize_launch_failure", request.Meta.IdempotencyKey)
	requireSchedulerReview(t, err)
	marker := errors.New("review-final-failure-late-rollback")
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
	if err != nil && !errors.Is(err, marker) {
		requireSchedulerReview(t, err)
	}
	if !seen || !returned || physical.State() != f.NotCommitted || !errors.Is(physical.Fault(), marker) || !errors.Is(err, marker) || out.Summary().Status == scheduler.Failed {
		t.Fatal("original relaunch final transaction did not physically roll back at the late marker", err)
	}
	if stable != y.snapshot(t) {
		t.Fatal("relaunch rollback leaked Task/blocker/history/event/Dispatch or origin writes")
	}
	x.parent.requireTaskUnchanged(t)
	settled, err := y.failure.finalizer.FinalizeLaunchFailure(ctxFor(t), request.ProjectID, s.ID)
	requireSchedulerReview(t, err)
	if settled.Summary().Status != scheduler.Failed || settled.Summary().ID != s.ID || settled.Summary().Version != s.Version+1 || settled.Summary().AttemptCount != 1 {
		t.Fatal("same original relaunch did not settle after known rollback")
	}
	current := y.failure.currentTask(t)
	want := x.parent.task.Clone()
	want.State, want.Version, want.UpdatedAt, want.ManualRank = wc.TaskStateBlocked, x.parent.task.Version+1, current.UpdatedAt, current.ManualRank
	if !reflect.DeepEqual(want, current) || origins != y.originFacts(t) {
		t.Fatal("relaunch settlement rewrote original Work fields, origins, or latest Execution")
	}
	requireSchedulerReview(t, y.settledFacts(ctxFor(t), y.failure.v.base.raw))
	stable = y.snapshot(t)
	replay, err := y.failure.finalizer.FinalizeLaunchFailure(ctxFor(t), request.ProjectID, s.ID)
	requireSchedulerReview(t, err)
	observed, err := y.failure.finalizer.Lookup(ctxFor(t), request.ProjectID, s.ID)
	requireSchedulerReview(t, err)
	if !reflect.DeepEqual(replay.Summary(), settled.Summary()) || !reflect.DeepEqual(observed.Summary(), settled.Summary()) || stable != y.snapshot(t) {
		t.Fatal("original relaunch final result lookup/replay repeated a mutation")
	}
	x.parent.execution.round.requireWire(t, 1, true)
	x.parent.execution.round.reader.requireDestroyed(t, 1)
}

func (x *schedulerReviewFailureFixture) baseline() *schedulerRelaunchFailureFixture {
	return &schedulerRelaunchFailureFixture{parent: x.parent.parent, failure: x.failure, source: x.source}
}
func (x *schedulerReviewFailureFixture) snapshot(t *testing.T) string {
	return x.baseline().snapshot(t)
}
func (x *schedulerReviewFailureFixture) originFacts(t *testing.T) string {
	return x.baseline().originFacts(t)
}

// Read the actual completed writers inside the caller Tx before injecting rollback.
func (x *schedulerReviewFailureFixture) settledFacts(ctx context.Context, sql postgres.SQLExecutor) error {
	y := x.failure
	before := x.parent.parent.task.Version
	var counts [10]int64
	err := sql.QueryRow(ctx, `SELECT
 (SELECT count(*) FROM agenteam_work.task_launch_failures c
  JOIN agenteam_work.tasks t ON t.project_id=c.project_id AND t.id=c.task_id
  JOIN agenteam_work.task_scheduler_relaunches o ON(o.project_id,o.id)=(c.project_id,c.relaunch_operation_id)
  JOIN agenteam_scheduler.dispatches d ON d.project_id=c.project_id::text AND d.id=c.id::text
  WHERE c.project_id=$1::text::uuid AND c.id=$2::text::uuid AND c.task_id=$3::text::uuid
   AND c.agent_id=$4::text::uuid AND c.request_id=$5::text::uuid AND c.dispatch_version=$6 AND c.launch_attempt=1
   AND c.claim_operation_id IS NULL AND c.relaunch_operation_id=c.id
   AND o.purpose='task/review' AND o.task_id=c.task_id AND o.agent_id=c.agent_id AND o.request_id=c.request_id AND o.task_version=$7
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
   AND h.payload=jsonb_build_object('from_state','in_review','to_state','blocked','reason_code','scheduler_launch_failed')),
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
		payload.MilestoneID != y.v.task.MilestoneID || payload.SprintID != y.v.task.SprintID || payload.AgentID != y.request.AgentID || payload.Reason != wc.TaskLaunchFailureUnsupportedResourceConstraints || payload.FromState != wc.TaskStateInReview || payload.ToState != wc.TaskStateBlocked {
		return errors.New("typed final event lost actual current Task and history identities")
	}
	return nil
}
