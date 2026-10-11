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

	ac "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/LunaDeerTech/agenteam/internal/central/work"
	wc "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
)

// Both Tasks reach in_progress through the real Scheduler and complete a real
// Model call. Human review never obtains or substitutes an AgentRun authority.
func TestTaskHumanReview(t *testing.T) {
	t.Run("completed-work-review-and-done", func(t *testing.T) {
		x := newTaskReviewFixture(t)
		v := x.domain
		http := technicalResolutionHTTPFixture(t, v)
		before := x.current(t)
		request, command, lookup := x.intent(t, before, wc.TaskStateInReview, &x.reviewer, "Please review the completed work.", "human-submit-review")
		body, lookupBody := taskReviewHTTPIntent(t, before, request)
		stable := x.snapshot(t)
		http.requireProblem(t, http.request(t, v.base.otherBrowser, "POST", http.transferPath(), body, command.IdempotencyKey), 404, f.NotFound)
		if x.snapshot(t) != stable {
			t.Fatal("non-Owner review created intent or changed business facts")
		}
		generations := x.generations(t, before, request.TargetState)
		reply := http.request(t, v.base.ownerBrowser, "POST", http.transferPath(), body, command.IdempotencyKey)
		review, err := wc.DecodeTaskTransitionMutation(reply.body)
		if reply.status != 200 || err != nil {
			t.Fatal("current Owner HTTPS review transfer", err)
		}
		x.requireCommitted(t, before, request, command, lookup, generations, nil, review)
		x.requireHTTPTask(t, http, review.Task)
		stable = x.snapshot(t)
		http.requireLookup(t, http.request(t, v.base.ownerBrowser, "POST", http.lookupPath(), lookupBody, command.IdempotencyKey), review)
		replay := http.request(t, v.base.ownerBrowser, "POST", http.transferPath(), body, command.IdempotencyKey)
		if replay.status != 200 || !bytes.Equal(replay.body, reply.body) || x.snapshot(t) != stable {
			t.Fatal("review HTTP Lookup/replay rewrote its original facts")
		}

		before = review.Task
		request, command, lookup = x.intent(t, before, wc.TaskStateDone, nil, "Reviewed and accepted.", "human-accept-review")
		body, lookupBody = taskReviewHTTPIntent(t, before, request)
		generations = x.generations(t, before, request.TargetState)
		reply = http.request(t, v.base.ownerBrowser, "POST", http.transferPath(), body, command.IdempotencyKey)
		done, err := wc.DecodeTaskTransitionMutation(reply.body)
		if reply.status != 200 || err != nil {
			t.Fatal("current Owner HTTPS review acceptance", err)
		}
		x.requireCommitted(t, before, request, command, lookup, generations, nil, done)
		if done.Task.AssigneeAgentID == nil || *done.Task.AssigneeAgentID != x.reviewer {
			t.Fatal("omitted acceptance assignee did not retain the reviewer")
		}
		x.requireHTTPTask(t, http, done.Task)
		stable = x.snapshot(t)
		http.requireLookup(t, http.request(t, v.base.ownerBrowser, "POST", http.lookupPath(), lookupBody, command.IdempotencyKey), done)
		replay = http.request(t, v.base.ownerBrowser, "POST", http.transferPath(), body, command.IdempotencyKey)
		if replay.status != 200 || !bytes.Equal(replay.body, reply.body) || x.snapshot(t) != stable {
			t.Fatal("terminal Task prevented original receipt replay or replay mutated facts")
		}
		x.requireLineage(t)
	})

	t.Run("review-rework-late-transaction-rollback-and-replay", func(t *testing.T) {
		x := newTaskReviewFixture(t)
		v, actor := x.domain, x.domain.base.ownerBrowser.actor
		before := x.current(t)
		request, command, lookup := x.intent(t, before, wc.TaskStateInReview, &x.reviewer, "Review this completed work before accepting it.", "rework-submit-review")
		generations := x.generations(t, before, request.TargetState)
		review, err := v.transitions.TransferTask(ctxFor(t), actor, command, before.ProjectID, before.ID, request)
		firstRoundRequire(t, err)
		x.requireCommitted(t, before, request, command, lookup, generations, nil, review)

		// A real todo peer gives rework a nonempty destination group. Its logical
		// neighbor is asserted through ListTasks and the typed position, not a
		// numeric/manual-rank implementation assumption.
		created, err := v.tasks.CreateTask(ctxFor(t), actor, meta(t, "review-todo-peer", nil), before.ProjectID, wc.TaskCreate{
			TaskID: id[wc.Task](t), SprintID: before.SprintID, Title: "Existing todo peer", Type: wc.TaskTypeTask, Priority: before.Priority,
		})
		firstRoundRequire(t, err)
		peer, err := v.transitions.TransferTask(ctxFor(t), actor, meta(t, "review-todo-peer-assign", &created.Task.Version), before.ProjectID, created.Task.ID,
			wc.TaskTransfer{TargetState: wc.TaskStateTodo, AssigneeAgentID: &v.agentID, AddBlockers: []wc.TaskBlockerCreate{}, ResolveBlockerIDs: []wc.TaskBlockerID{}})
		firstRoundRequire(t, err)
		before = review.Task
		request, command, lookup = x.intent(t, before, wc.TaskStateTodo, &v.agentID, "Revise the work and return it for another review.", "human-review-rework")
		generations = x.generations(t, before, request.TargetState)
		prepared, err := v.transitions.PrepareTaskTransition(ctxFor(t), actor, command, before.ProjectID, before.ID, request)
		if err != nil || prepared.Prepared == nil || prepared.CompletedReceipt != nil {
			t.Fatal("original rework command did not produce its real prepared plan", err)
		}
		stable := x.snapshot(t) // The real planned intent is allowed to remain.
		identity, err := wc.TaskTransitionIdentity(before.ProjectID, command.IdempotencyKey)
		firstRoundRequire(t, err)
		cause, err := f.NewCommandsCause(identity)
		firstRoundRequire(t, err)
		marker := errors.New("human-review-rework-after-all-final-facts")
		sawAll := false
		physical := v.base.tracked.WithinTx(ctxFor(t), cause, func(ctx context.Context, tx f.Tx) error {
			if err := v.base.tracked.AcquireAll(ctx, tx, prepared.Prepared.RequiredLocks()); err != nil {
				return err
			}
			tentative, err := v.transitions.TransferTaskInTx(ctx, tx, actor, prepared.Prepared)
			if err != nil {
				return err
			}
			sql, err := v.base.tracked.InTx(tx)
			if err != nil {
				return err
			}
			stored, err := x.committedFacts(ctx, sql, before, request, command, lookup, generations, &peer.Task.ID)
			if err != nil {
				return err
			}
			if !reflect.DeepEqual(tentative, stored) {
				return errors.New("tentative rework receipt differs from same-transaction facts")
			}
			sawAll = true
			return marker // The original Store performs the physical rollback.
		})
		if !sawAll || physical.State() != f.NotCommitted || !errors.Is(physical.Fault(), marker) || x.snapshot(t) != stable || !reflect.DeepEqual(x.current(t), before) {
			t.Fatal("late rework transaction leaked Task, history, ranks, receipt, Outbox or activity", physical.Fault())
		}
		found, err := v.transitions.LookupTaskTransition(ctxFor(t), actor, lookup)
		if err != nil || found.Status != wc.LookupInProgress || found.Receipt != nil {
			t.Fatal("rolled-back rework was reported as committed", err)
		}
		committed, err := v.transitions.TransferTask(ctxFor(t), actor, command, before.ProjectID, before.ID, request)
		firstRoundRequire(t, err)
		x.requireCommitted(t, before, request, command, lookup, generations, &peer.Task.ID, committed)
		stable = x.snapshot(t)
		replay, err := v.transitions.TransferTask(ctxFor(t), actor, command, before.ProjectID, before.ID, request)
		if err != nil || !reflect.DeepEqual(replay, committed) {
			t.Fatal("original rework key did not replay its complete receipt", err)
		}
		found, err = v.transitions.LookupTaskTransition(ctxFor(t), actor, lookup)
		if err != nil || found.Status != wc.LookupCommitted || found.Receipt == nil || !reflect.DeepEqual(*found.Receipt, committed) || x.snapshot(t) != stable {
			t.Fatal("rework Lookup/replay changed the original intent or committed facts", err)
		}
		x.requireLineage(t)
	})
}

type taskReviewFixture struct {
	domain   *taskTransitionFixture
	reviewer i.AgentID
	lineage  string
}

func newTaskReviewFixture(t *testing.T) *taskReviewFixture {
	t.Helper()
	original := newSchedulerRelaunchFixture(t)
	original.execution.stop(t)
	v := original.execution.round.capture.v
	request, command, _ := v.agent.request(t)
	fields := request.Fields()
	fields.Name = "human-reviewer"
	request, err := ac.NewAgentCreate(fields)
	firstRoundRequire(t, err)
	receipt, err := v.agent.agents.CreateAgent(ctxFor(t), v.base.ownerBrowser.actor, command, v.base.project.ID, request)
	if err != nil || receipt.Validate() != nil || receipt.Fields().Agent.Fields().Core.ID != fields.AgentID || fields.AgentID == v.agentID {
		t.Fatal("distinct current reviewer was not created by the real Agent service", err)
	}
	x := &taskReviewFixture{domain: v, reviewer: fields.AgentID}
	x.lineage = x.readLineage(t)
	if !reflect.DeepEqual(x.current(t), original.task) || original.task.State != wc.TaskStateInProgress || original.task.AssigneeAgentID == nil || *original.task.AssigneeAgentID != v.agentID {
		t.Fatal("review setup changed the Task after its real completed Execution")
	}
	return x
}

func (x *taskReviewFixture) current(t *testing.T) wc.Task {
	t.Helper()
	v := x.domain
	task, err := v.taskReader.GetTask(ctxFor(t), v.base.ownerBrowser.actor, v.base.project.ID, v.task.ID)
	firstRoundRequire(t, err)
	return task
}

func (x *taskReviewFixture) intent(t *testing.T, before wc.Task, target wc.TaskState, assignee *i.AgentID, comment, key string) (wc.TaskTransfer, f.CommandMeta, wc.TaskTransitionLookupRequest) {
	t.Helper()
	r := wc.TaskTransfer{TargetState: target, AssigneeAgentID: assignee, Comment: &comment, AddBlockers: []wc.TaskBlockerCreate{}, ResolveBlockerIDs: []wc.TaskBlockerID{}}
	firstRoundRequire(t, r.Validate())
	m := meta(t, key, &before.Version)
	digest, err := wc.TaskTransferDigest(x.domain.base.ownerBrowser.actor, m, before.ProjectID, before.ID, r)
	firstRoundRequire(t, err)
	return r, m, wc.TaskTransitionLookupRequest{ProjectID: before.ProjectID, Command: wc.TaskTransitionTransfer, IdempotencyKey: m.IdempotencyKey, SemanticDigest: digest}
}

func taskReviewHTTPIntent(t *testing.T, before wc.Task, request wc.TaskTransfer) (string, string) {
	t.Helper()
	return string(jsonBytes(t, map[string]any{"expected_version": before.Version, "request": request})),
		string(jsonBytes(t, map[string]any{"command": wc.TaskTransitionTransfer, "target_id": before.ID, "expected_version": before.Version, "request": request}))
}

func (x *taskReviewFixture) requireHTTPTask(t *testing.T, http *taskHumanHTTPFixture, want wc.Task) {
	t.Helper()
	reply := http.request(t, x.domain.base.ownerBrowser, "GET", http.taskPath(), "", "")
	var current wc.Task
	if reply.status != 200 || json.Unmarshal(reply.body, &current) != nil || !reflect.DeepEqual(current, want) {
		t.Fatal("HTTPS current Task differs from the exact committed review receipt")
	}
}

func (x *taskReviewFixture) generations(t *testing.T, before wc.Task, target wc.TaskState) [3]int64 {
	t.Helper()
	var values [3]int64
	err := x.domain.base.raw.QueryRow(ctxFor(t), `SELECT
 (SELECT query_generation FROM agenteam_work.task_query_generations WHERE project_id=$1::text::uuid),
 coalesce((SELECT order_generation FROM agenteam_work.task_order_groups WHERE project_id=$1::text::uuid AND sprint_id=$2::text::uuid AND state=$3 AND priority=$5),1),
 coalesce((SELECT order_generation FROM agenteam_work.task_order_groups WHERE project_id=$1::text::uuid AND sprint_id=$2::text::uuid AND state=$4 AND priority=$5),1)`,
		before.ProjectID.String(), before.SprintID.String(), string(before.State), string(target), string(before.Priority)).Scan(&values[0], &values[1], &values[2])
	firstRoundRequire(t, err)
	return values
}

func (x *taskReviewFixture) requireCommitted(t *testing.T, before wc.Task, request wc.TaskTransfer, command f.CommandMeta, lookup wc.TaskTransitionLookupRequest, generations [3]int64, previous *wc.TaskID, receipt wc.TaskTransitionMutation) {
	t.Helper()
	stored, err := x.committedFacts(ctxFor(t), x.domain.base.raw, before, request, command, lookup, generations, previous)
	if err != nil || !reflect.DeepEqual(stored, receipt) || !reflect.DeepEqual(x.current(t), receipt.Task) {
		t.Fatal("review receipt differs from current canonical/history/order/Outbox facts", err)
	}
	v := x.domain
	for _, state := range []wc.TaskState{before.State, request.TargetState} {
		page, err := v.taskReader.ListTasks(ctxFor(t), v.base.ownerBrowser.actor, before.ProjectID,
			wc.TaskFilter{SprintID: &before.SprintID, State: &state, Priority: &before.Priority}, f.PageRequest{Limit: 10})
		firstRoundRequire(t, err)
		var want []wc.TaskID
		if state == request.TargetState {
			if previous != nil {
				want = append(want, *previous)
			}
			want = append(want, before.ID)
		}
		if page.NextCursor != "" || len(page.Items) != len(want) {
			t.Fatal("review did not remove the source member and append once to its destination")
		}
		for n, task := range page.Items {
			if task.ID != want[n] {
				t.Fatal("review destination logical neighbor/order differs from the original plan")
			}
		}
	}
	reader, err := work.NewBlockerReader(v.base.tracked, v.authority, v.base.keys)
	firstRoundRequire(t, err)
	blockers, err := reader.ListTaskBlockersPage(ctxFor(t), v.base.ownerBrowser.actor, before.ProjectID, before.ID, wc.TaskBlockersUnresolved, f.PageRequest{Limit: 10})
	if err != nil || blockers.Items == nil || len(blockers.Items) != 0 || blockers.NextCursor != "" {
		t.Fatal("real current blocker read was not the empty fixture set", err)
	}
}

// This read runs inside the original caller transaction for the rollback case.
// No fixture writer manufactures a Task state, history entry or completed key.
func (x *taskReviewFixture) committedFacts(ctx context.Context, sql postgres.SQLExecutor, before wc.Task, request wc.TaskTransfer, command f.CommandMeta, lookup wc.TaskTransitionLookupRequest, generations [3]int64, previous *wc.TaskID) (wc.TaskTransitionMutation, error) {
	var operation, user, digest string
	var expected int64
	var raw, payload, originalRequest []byte
	err := sql.QueryRow(ctx, `SELECT c.id::text,c.actor_user_id::text,c.expected_version,c.semantic_digest,c.request->'request',c.receipt,e.payload
 FROM agenteam_work.task_transition_commands c JOIN agenteam_outbox.events e ON e.id=c.event_id
 WHERE c.project_id=$1::text::uuid AND c.task_id=$2::text::uuid AND c.idempotency_key=$3 AND c.state='completed'`,
		before.ProjectID.String(), before.ID.String(), string(command.IdempotencyKey)).Scan(&operation, &user, &expected, &digest, &originalRequest, &raw, &payload)
	if err != nil {
		return wc.TaskTransitionMutation{}, err
	}
	storedRequest, err := wc.DecodeTaskTransfer(originalRequest)
	if err != nil || !reflect.DeepEqual(storedRequest, request) || user != x.domain.base.ownerBrowser.actor.Details().UserID || expected != int64(before.Version) || digest != lookup.SemanticDigest.String() {
		return wc.TaskTransitionMutation{}, errors.New("review receipt changed original Human intent")
	}
	r, err := wc.DecodeTaskTransitionMutation(raw)
	if err != nil {
		return r, err
	}
	agent := before.AssigneeAgentID
	if request.AssigneeAgentID != nil {
		agent = request.AssigneeAgentID
	}
	want := before.Clone()
	want.State, want.AssigneeAgentID, want.Version, want.UpdatedAt, want.ManualRank = request.TargetState, agent, before.Version+1, r.Task.UpdatedAt, r.Task.ManualRank
	changed := !reflect.DeepEqual(before.AssigneeAgentID, agent)
	historyCount := 2
	if changed {
		historyCount++
	}
	if !reflect.DeepEqual(r.Task, want) || len(r.TaskEventIDs) != historyCount || len(r.EventIDs) != 1 || r.Task.UpdatedAt.Time().Before(before.UpdatedAt.Time()) {
		return r, errors.New("review changed unrelated Task fields or history cardinality")
	}
	e, err := wc.DecodeTaskTransitioned(payload)
	if err != nil || e.CommandID.String() != operation || e.Actor.UserID.String() != user || e.FromState != before.State || e.ToState != request.TargetState || e.MilestoneID != before.MilestoneID || e.SprintID != before.SprintID || !reflect.DeepEqual(e.TaskEventIDs, r.TaskEventIDs) || e.SourcePosition.PreviousID != nil || e.SourcePosition.NextID != nil || !reflect.DeepEqual(e.TargetPosition.PreviousID, previous) || e.TargetPosition.NextID != nil || e.SourcePosition.Priority != before.Priority || e.TargetPosition.Priority != before.Priority || e.SourcePosition.OrderGeneration != generations[1]+1 || e.TargetPosition.OrderGeneration != generations[2]+1 {
		return r, errors.New("review typed event changed current Human or original logical positions")
	}
	if changed && (e.AssigneeChange == nil || !reflect.DeepEqual(e.AssigneeChange.FromAgentID, before.AssigneeAgentID) || e.AssigneeChange.ToAgentID != *agent) || !changed && e.AssigneeChange != nil {
		return r, errors.New("review typed event invented or lost the assignee handoff")
	}
	// Validate the exact ordered per-command history payloads using their
	// declared receipt IDs, plus one shared actor/version/correlation/time.
	var got [10]int64
	assigneeEvent := r.TaskEventIDs[0].String() // state ID cannot match an assignee row in the unchanged arm.
	wantAssignee := int64(0)
	if changed {
		assigneeEvent, wantAssignee = r.TaskEventIDs[1].String(), 1
	}
	err = sql.QueryRow(ctx, `SELECT
 (SELECT count(*) FROM agenteam_work.tasks WHERE project_id=$1::text::uuid AND id=$2::text::uuid AND state=$6 AND version=$4 AND assignee_agent_id=$8::text::uuid AND updated_at=$12),
 (SELECT count(*) FROM agenteam_work.task_events WHERE project_id=$1::text::uuid AND task_id=$2::text::uuid AND transition_operation_id=$3::text::uuid),
 (SELECT count(*) FROM agenteam_work.task_events WHERE project_id=$1::text::uuid AND task_id=$2::text::uuid AND transition_operation_id=$3::text::uuid AND correlation_id=$3::text::uuid AND task_version=$4 AND created_at=$12 AND actor=jsonb_build_object('type','human','user_id',$7::text,'source','task_domain')),
 (SELECT count(*) FROM agenteam_work.task_events WHERE project_id=$1::text::uuid AND id=$9::text::uuid AND transition_operation_id=$3::text::uuid AND type='state_changed' AND payload=jsonb_build_object('from_state',$5::text,'to_state',$6::text,'reason_code',NULL)),
 (SELECT count(*) FROM agenteam_work.task_events WHERE project_id=$1::text::uuid AND id=$10::text::uuid AND transition_operation_id=$3::text::uuid AND type='assignee_changed' AND payload=jsonb_build_object('from_agent_id',$14::text,'to_agent_id',$8::text)),
 (SELECT count(*) FROM agenteam_work.task_events WHERE project_id=$1::text::uuid AND id=$11::text::uuid AND transition_operation_id=$3::text::uuid AND type='comment' AND payload=jsonb_build_object('body',$13::text)),
 (SELECT count(*) FROM agenteam_outbox.events WHERE project_id=$1::text::uuid AND aggregate_id=$2::text::uuid AND id=$15::text::uuid AND aggregate_version=$4 AND producer='work' AND event_type='work.task_transitioned' AND schema_version=1),
 (SELECT count(*) FROM agenteam_work.task_blockers WHERE project_id=$1::text::uuid AND task_id=$2::text::uuid AND resolved_at IS NULL),
 (SELECT query_generation FROM agenteam_work.task_query_generations WHERE project_id=$1::text::uuid),
 (SELECT count(*) FROM agenteam_work.task_transition_commands WHERE project_id=$1::text::uuid AND id=$3::text::uuid AND committed_at=$12 AND event_id=$15::text::uuid)`,
		before.ProjectID.String(), before.ID.String(), operation, int64(r.Task.Version), string(before.State), string(request.TargetState), user, agent.String(),
		r.TaskEventIDs[0].String(), assigneeEvent, r.TaskEventIDs[len(r.TaskEventIDs)-1].String(), r.Task.UpdatedAt.Time(), *request.Comment, before.AssigneeAgentID.String(), r.EventIDs[0].String()).Scan(
		&got[0], &got[1], &got[2], &got[3], &got[4], &got[5], &got[6], &got[7], &got[8], &got[9])
	wantCounts := [10]int64{1, int64(historyCount), int64(historyCount), 1, wantAssignee, 1, 1, 0, generations[0] + 1, 1}
	if err != nil {
		return r, err
	}
	if got != wantCounts {
		return r, fmt.Errorf("review same-transaction fact counts %v", got)
	}
	var source, target int64
	err = sql.QueryRow(ctx, `SELECT
 (SELECT order_generation FROM agenteam_work.task_order_groups WHERE project_id=$1::text::uuid AND sprint_id=$2::text::uuid AND state=$3 AND priority=$5),
 (SELECT order_generation FROM agenteam_work.task_order_groups WHERE project_id=$1::text::uuid AND sprint_id=$2::text::uuid AND state=$4 AND priority=$5)`,
		before.ProjectID.String(), before.SprintID.String(), string(before.State), string(request.TargetState), string(before.Priority)).Scan(&source, &target)
	if err != nil || source != generations[1]+1 || target != generations[2]+1 {
		return r, errors.New("review did not increment exactly the two original rank groups")
	}
	return r, nil
}

func (x *taskReviewFixture) snapshot(t *testing.T) string {
	t.Helper()
	v := x.domain
	var extra string
	err := v.base.raw.QueryRow(ctxFor(t), `SELECT jsonb_build_object(
 'commands',(SELECT coalesce(jsonb_agg(to_jsonb(c) ORDER BY id),'[]'::jsonb) FROM agenteam_work.task_transition_commands c WHERE project_id=$1::text::uuid),
 'blockers',(SELECT coalesce(jsonb_agg(to_jsonb(b) ORDER BY id),'[]'::jsonb) FROM agenteam_work.task_blockers b WHERE project_id=$1::text::uuid)
 )::text`, v.base.project.ID.String()).Scan(&extra)
	firstRoundRequire(t, err)
	return v.databaseSnapshot(t) + "\n" + extra + "\n" + x.readLineage(t)
}

func (x *taskReviewFixture) readLineage(t *testing.T) string {
	t.Helper()
	var value string
	err := x.domain.base.raw.QueryRow(ctxFor(t), `SELECT jsonb_build_object(
 'executions',(SELECT coalesce(jsonb_agg(to_jsonb(e) ORDER BY id),'[]'::jsonb) FROM agenteam_execution.executions e WHERE project_id=$1::text),
 'dispatches',(SELECT coalesce(jsonb_agg(to_jsonb(d) ORDER BY id),'[]'::jsonb) FROM agenteam_scheduler.dispatches d WHERE project_id=$1::text),
 'claims',(SELECT coalesce(jsonb_agg(to_jsonb(c) ORDER BY id),'[]'::jsonb) FROM agenteam_work.task_scheduler_claims c WHERE project_id=$1::text::uuid)
 )::text`, x.domain.base.project.ID.String()).Scan(&value)
	firstRoundRequire(t, err)
	return value
}

func (x *taskReviewFixture) requireLineage(t *testing.T) {
	t.Helper()
	if x.readLineage(t) != x.lineage {
		t.Fatal("Human review changed or restarted the original completed work Execution/Dispatch")
	}
}
