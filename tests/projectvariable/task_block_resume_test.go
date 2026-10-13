//go:build integration

package projectvariable_test

import (
	"bytes"
	"encoding/json"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	wc "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
)

// The original HTTP fixture creates the Project, Agent, planned Sprint and
// backlog Task through their real owners. Scheduler stays formally disabled;
// real occupancy/pending adapters remain bound. No SQL creates workflow state.
func TestTaskBlockResumeHTTP(t *testing.T) {
	t.Run("assigned-todo-last-blocker-and-historical-receipts", func(t *testing.T) {
		v := assembleTaskHumanHTTPFixture(t, true)
		d, browser := v.domain, v.domain.base.ownerBrowser
		config, err := d.base.projects.GetSchedulerConfig(ctxFor(t), browser.actor, d.base.project.ID)
		if err != nil || config.Config.Enabled || config.Project.CurrentSprintID != nil {
			t.Fatal("fixture must retain the actual disabled Scheduler", err)
		}
		sprint, err := d.structureReader.GetSprint(ctxFor(t), browser.actor, d.base.project.ID, d.task.SprintID)
		if err != nil || sprint.State != wc.Planned {
			t.Fatal("fixture must use the original planned Sprint", err)
		}
		readyBody, _, readyKey := v.intent(t)
		ready := v.requireTransition(t, v.request(t, browser, "POST", v.transferPath(), readyBody, readyKey))
		if _, err = d.committedFacts(ctxFor(t), d.base.raw, readyKey); err != nil {
			t.Fatal("formal Ready facts missing", err)
		}
		snapshot := func() string {
			t.Helper()
			var ledger string
			if err := d.base.raw.QueryRow(ctxFor(t), `SELECT jsonb_build_object(
 'transitions',(SELECT coalesce(jsonb_agg(to_jsonb(c) ORDER BY id),'[]'::jsonb) FROM agenteam_work.task_transition_commands c WHERE project_id=$1::uuid),
 'blocker_commands',(SELECT coalesce(jsonb_agg(to_jsonb(c) ORDER BY id),'[]'::jsonb) FROM agenteam_work.task_blocker_commands c WHERE project_id=$1::uuid),
 'blockers',(SELECT coalesce(jsonb_agg(to_jsonb(b) ORDER BY id),'[]'::jsonb) FROM agenteam_work.task_blockers b WHERE project_id=$1::uuid)
 )::text`, d.base.project.ID.String()).Scan(&ledger); err != nil {
				t.Fatal(err)
			}
			return d.databaseSnapshot(t) + "\n" + ledger
		}
		intent := func(before wc.Task, request wc.TaskTransfer) (string, string) {
			t.Helper()
			return string(jsonBytes(t, map[string]any{"expected_version": before.Version, "request": request})),
				string(jsonBytes(t, map[string]any{"command": wc.TaskTransitionTransfer, "target_id": before.ID, "expected_version": before.Version, "request": request}))
		}
		blockRequest := wc.TaskTransfer{TargetState: wc.TaskStateBlocked}
		emptyBody, _ := intent(ready.Task, blockRequest)
		stable := snapshot()
		v.requireProblem(t, v.request(t, browser, "POST", v.transferPath(), emptyBody, "block-without-cause"), 409, f.TaskBlockedWithoutBlocker)
		if snapshot() != stable {
			t.Fatal("rejected cause-free block changed canonical facts or a command")
		}
		add := wc.TaskBlockerCreate{BlockerID: id[wc.TaskBlockerIdentity](t), Type: wc.TaskBlockerWaitingForHuman, Description: "Owner must confirm the task inputs", Metadata: wc.TaskBlockerMetadata{WaitingForHuman: &wc.TaskBlockerWaitingForHumanMetadata{}}}
		addedReply := v.request(t, browser, "POST", v.taskPath()+"/blockers", string(jsonBytes(t, map[string]any{"expected_version": ready.Task.Version, "request": add})), "block-resume-add")
		var added wc.TaskBlockerMutation
		if addedReply.status != 200 || json.Unmarshal(addedReply.body, &added) != nil || added.Validate() != nil || added.Blocker.ID != add.BlockerID || added.Blocker.ResolvedAt != nil {
			t.Fatal("formal Human Add did not commit")
		}
		wantAdded := ready.Task.Clone()
		wantAdded.Version, wantAdded.UpdatedAt = ready.Task.Version+1, added.Task.UpdatedAt
		if !bytes.Equal(jsonBytes(t, wantAdded), jsonBytes(t, added.Task)) {
			t.Fatal("Add changed workflow before the explicit transition")
		}
		checkTransfer := func(before wc.Task, request wc.TaskTransfer, reply variableHTTPResponse, key f.IdempotencyKey, historyCount int64) wc.TaskTransitionMutation {
			t.Helper()
			receipt, err := wc.DecodeTaskTransitionMutation(reply.body)
			if reply.status != 200 || err != nil || len(receipt.TaskEventIDs) != int(historyCount) || len(receipt.EventIDs) != 1 {
				t.Fatal("original Transfer did not return a complete receipt", reply.status, err)
			}
			want := before.Clone()
			want.State, want.Version, want.UpdatedAt, want.ManualRank = request.TargetState, before.Version+1, receipt.Task.UpdatedAt, receipt.Task.ManualRank
			if !bytes.Equal(jsonBytes(t, want), jsonBytes(t, receipt.Task)) {
				t.Fatal("Transfer changed assignment or unrelated Task content")
			}
			current := v.request(t, browser, "GET", v.taskPath(), "", "")
			var actual wc.Task
			if current.status != 200 || json.Unmarshal(current.body, &actual) != nil || !bytes.Equal(jsonBytes(t, actual), jsonBytes(t, receipt.Task)) {
				t.Fatal("receipt differs from the real Task postimage")
			}
			var operation string
			var raw []byte
			if err = d.base.raw.QueryRow(ctxFor(t), `SELECT id::text,receipt FROM agenteam_work.task_transition_commands WHERE project_id=$1::uuid AND task_id=$2::uuid AND idempotency_key=$3 AND state='completed'`, before.ProjectID.String(), before.ID.String(), string(key)).Scan(&operation, &raw); err != nil {
				t.Fatal(err)
			}
			stored, err := wc.DecodeTaskTransitionMutation(raw)
			if err != nil || !bytes.Equal(jsonBytes(t, stored), reply.body) {
				t.Fatal("completed ledger differs from original HTTP receipt", err)
			}
			var facts [5]int64
			err = d.base.raw.QueryRow(ctxFor(t), `SELECT
 (SELECT count(*) FROM agenteam_work.task_events WHERE project_id=$1::text::uuid AND task_id=$2::text::uuid AND transition_operation_id=$3::text::uuid),
 (SELECT count(*) FROM agenteam_work.task_events WHERE project_id=$1::text::uuid AND id=$4::text::uuid AND transition_operation_id=$3::text::uuid AND correlation_id=$3::text::uuid AND task_version=$6 AND type='state_changed' AND payload->>'from_state'=$7::text AND payload->>'to_state'=$8::text AND actor->>'user_id'=$9::text),
 (SELECT count(*) FROM agenteam_outbox.events WHERE project_id=$1::text::uuid AND aggregate_id=$2::text::uuid AND id=$5::text::uuid AND aggregate_version=$6 AND producer='work' AND event_type='work.task_transitioned' AND schema_version=1 AND convert_from(payload,'UTF8')::jsonb->>'command_id'=$3::text),
 (SELECT count(*) FROM agenteam_scheduler.dispatches WHERE project_id=$1::text),
 (SELECT count(*) FROM agenteam_execution.executions WHERE project_id=$1::text)`, before.ProjectID.String(), before.ID.String(), operation, receipt.TaskEventIDs[0].String(), receipt.EventIDs[0].String(), int64(receipt.Task.Version), string(before.State), string(request.TargetState), browser.actor.Details().UserID).Scan(&facts[0], &facts[1], &facts[2], &facts[3], &facts[4])
			if err != nil || facts != ([5]int64{historyCount, 1, 1, 0, 0}) {
				t.Fatal("actual transition history/Outbox/occupancy differs", facts, err)
			}
			return receipt
		}
		blockBody, blockLookup := intent(added.Task, blockRequest)
		blockKey := f.IdempotencyKey("mark-task-blocked")
		blockReply := v.request(t, browser, "POST", v.transferPath(), blockBody, blockKey)
		blocked := checkTransfer(added.Task, blockRequest, blockReply, blockKey, 1)
		readBlocker := func() wc.TaskBlocker {
			t.Helper()
			reply := v.request(t, browser, "GET", v.taskPath()+"/blockers?status=all", "", "")
			var page f.Page[wc.TaskBlocker]
			if reply.status != 200 || json.Unmarshal(reply.body, &page) != nil || len(page.Items) != 1 || page.NextCursor != "" || page.Items[0].ID != add.BlockerID {
				t.Fatal("original complete Blocker read failed")
			}
			return page.Items[0]
		}
		if !bytes.Equal(jsonBytes(t, readBlocker()), jsonBytes(t, added.Blocker)) {
			t.Fatal("marking blocked changed the existing cause")
		}
		stable = snapshot()
		lastBody := string(jsonBytes(t, map[string]any{"expected_version": blocked.Task.Version, "request": wc.TaskBlockerResolve{BlockerID: add.BlockerID}}))
		v.requireProblem(t, v.request(t, browser, "POST", v.taskPath()+"/blockers/resolve", lastBody, "last-cause-standalone"), 409, f.LastBlockerNeedsTransfer)
		if snapshot() != stable {
			t.Fatal("last-cause refusal changed Task, cause, order, ledger or Activity")
		}
		resumeRequest := wc.TaskTransfer{TargetState: wc.TaskStateTodo, ResolveBlockerIDs: []wc.TaskBlockerID{add.BlockerID}}
		resumeBody, resumeLookup := intent(blocked.Task, resumeRequest)
		resumeKey := f.IdempotencyKey("resume-task-todo")
		resumeReply := v.request(t, browser, "POST", v.transferPath(), resumeBody, resumeKey)
		resumed := checkTransfer(blocked.Task, resumeRequest, resumeReply, resumeKey, 2)
		resolved := readBlocker()
		if resolved.ResolvedAt == nil || resolved.ResolvedBy == nil || resolved.ResolvedBy.UserID != added.Blocker.CreatedBy.UserID || resolved.ResolutionComment != nil || *resolved.ResolvedAt != resumed.Task.UpdatedAt {
			t.Fatal("atomic resume lost the original Human resolution")
		}
		preserved := resolved.Clone()
		preserved.ResolvedAt, preserved.ResolvedBy = nil, nil
		if !bytes.Equal(jsonBytes(t, preserved), jsonBytes(t, added.Blocker)) {
			t.Fatal("resume rewrote original Blocker creation provenance")
		}
		var resolvedFacts int64
		if err = d.base.raw.QueryRow(ctxFor(t), `SELECT count(*) FROM agenteam_work.task_blockers b JOIN agenteam_work.task_transition_commands c ON c.id=b.resolved_transition_operation_id JOIN agenteam_work.task_events e ON e.transition_operation_id=c.id AND e.id=$4::uuid WHERE b.project_id=$1::uuid AND b.task_id=$2::uuid AND b.id=$3::uuid AND b.resolved_operation_id IS NULL AND c.idempotency_key=$5 AND e.type='blocker_resolved' AND e.payload->>'blocker_id'=b.id::text AND e.payload->>'blocker_type'='waiting_for_human' AND e.task_version=$6`, resumed.Task.ProjectID.String(), resumed.Task.ID.String(), add.BlockerID.String(), resumed.TaskEventIDs[1].String(), string(resumeKey), int64(resumed.Task.Version)).Scan(&resolvedFacts); err != nil || resolvedFacts != 1 {
			t.Fatal("same transition did not persist the actual resolution and history", err)
		}
		stable = snapshot()
		for _, original := range []struct {
			body, lookup string
			key          f.IdempotencyKey
			reply        variableHTTPResponse
			receipt      wc.TaskTransitionMutation
		}{{blockBody, blockLookup, blockKey, blockReply, blocked}, {resumeBody, resumeLookup, resumeKey, resumeReply, resumed}} {
			v.requireLookup(t, v.request(t, browser, "POST", v.lookupPath(), original.lookup, original.key), original.receipt)
			replay := v.request(t, browser, "POST", v.transferPath(), original.body, original.key)
			if replay.status != 200 || !bytes.Equal(replay.body, original.reply.body) || snapshot() != stable {
				t.Fatal("historical block/resume replay changed original facts")
			}
		}
	})
}
