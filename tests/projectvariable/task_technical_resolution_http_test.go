//go:build integration

package projectvariable_test

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"
	"time"

	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/outbox"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/work"
	wc "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
	workhttp "github.com/LunaDeerTech/agenteam/internal/central/work/http"
)

func TestTaskTechnicalResolutionHTTP(t *testing.T) {
	t.Run("resolve-to-todo-lookup-replay", func(t *testing.T) {
		// Reuse the actual claim, original synchronous Launch rejection and
		// finalizer. No Task, technical Blocker or failed Dispatch is SQL-seeded.
		x := newSchedulerFailureFixture(t)
		beforeFailure := x.currentTask(t)
		settled, err := x.finalizer.FinalizeLaunchFailure(ctxFor(t), x.request.ProjectID, x.dispatch)
		if err != nil {
			t.Fatal("formal final failure did not settle", err)
		}
		x.requireSettled(t, settled, beforeFailure)
		v := technicalResolutionHTTPFixture(t, x.v)
		browser := x.v.base.ownerBrowser
		readTask := func() wc.Task {
			t.Helper()
			r := v.request(t, browser, "GET", v.taskPath(), "", "")
			var task wc.Task
			if r.status != 200 || json.Unmarshal(r.body, &task) != nil || task.Validate() != nil || task.ID != x.v.task.ID || task.ProjectID != x.request.ProjectID {
				t.Fatal("current HTTPS Task unavailable")
			}
			return task
		}
		readBlockers := func(status string) []wc.TaskBlocker {
			t.Helper()
			r := v.request(t, browser, "GET", v.taskPath()+"/blockers?status="+status, "", "")
			var page f.Page[wc.TaskBlocker]
			if r.status != 200 || json.Unmarshal(r.body, &page) != nil || page.Items == nil || page.NextCursor != "" {
				t.Fatal("complete HTTPS Blocker page unavailable")
			}
			for _, b := range page.Items {
				if b.Validate() != nil || b.ProjectID != x.request.ProjectID || b.TaskID != x.v.task.ID {
					t.Fatal("Blocker read lost current Task scope")
				}
			}
			return page.Items
		}
		before, blockers := readTask(), readBlockers("unresolved")
		if before.State != wc.TaskStateBlocked || before.AssigneeAgentID == nil || *before.AssigneeAgentID != x.request.AgentID || len(blockers) != 1 {
			t.Fatal("original failure is not the assigned blocked Task")
		}
		blocker := blockers[0]
		if blocker.Type != wc.TaskBlockerTechnical || blocker.Technical == nil || blocker.SchedulerCreatedBy == nil || blocker.Technical.ReferenceID != x.dispatch.String() || blocker.SchedulerCreatedBy.CauseID != x.dispatch.String() || blocker.ResolvedAt != nil {
			t.Fatal("GET did not expose the original Scheduler technical Blocker")
		}

		// Save the entire intent before the first send. Omitted assignee retains
		// the current Agent; the client supplies no metadata or authority.
		request := wc.TaskTransfer{TargetState: wc.TaskStateTodo, ResolveBlockerIDs: []wc.TaskBlockerID{blocker.ID}}
		key := f.IdempotencyKey("http-technical-resolve-to-todo")
		body := string(jsonBytes(t, map[string]any{"expected_version": before.Version, "request": request}))
		lookup := string(jsonBytes(t, map[string]any{"command": wc.TaskTransitionTransfer, "target_id": before.ID, "expected_version": before.Version, "request": request}))
		reply := v.request(t, browser, "POST", v.transferPath(), body, key)
		receipt, err := wc.DecodeTaskTransitionMutation(reply.body)
		if reply.status != 200 || err != nil || receipt.Task.Version != before.Version+1 || receipt.Task.State != wc.TaskStateTodo || receipt.Task.AssigneeAgentID == nil || *receipt.Task.AssigneeAgentID != *before.AssigneeAgentID || len(receipt.TaskEventIDs) != 2 || len(receipt.EventIDs) != 1 {
			t.Fatal("HTTPS did not atomically resolve and return the original assigned Task to todo", err)
		}
		want := before.Clone()
		want.State, want.Version, want.UpdatedAt, want.ManualRank = wc.TaskStateTodo, before.Version+1, receipt.Task.UpdatedAt, receipt.Task.ManualRank
		if !bytes.Equal(jsonBytes(t, receipt.Task), jsonBytes(t, want)) || !bytes.Equal(jsonBytes(t, readTask()), jsonBytes(t, receipt.Task)) || len(readBlockers("unresolved")) != 0 {
			t.Fatal("resolution changed unrelated Task fields or left an unresolved Blocker")
		}
		resolved := readBlockers("resolved")
		if len(resolved) != 1 || resolved[0].ID != blocker.ID || resolved[0].ResolvedAt == nil || resolved[0].ResolvedBy == nil || resolved[0].ResolvedBy.UserID.String() != browser.actor.Details().UserID || resolved[0].ResolutionComment != nil {
			t.Fatal("resolved projection lost the current Human or invented a Blocker comment")
		}
		wantBlocker := blocker.Clone()
		wantBlocker.ResolvedAt, wantBlocker.ResolvedBy = resolved[0].ResolvedAt, resolved[0].ResolvedBy
		if !bytes.Equal(jsonBytes(t, resolved[0]), jsonBytes(t, wantBlocker)) {
			t.Fatal("resolution changed original Scheduler creation identity or metadata")
		}
		requireTechnicalResolutionHTTPFacts(t, x, key, blocker.ID, receipt)
		snapshot := func() string {
			t.Helper()
			var commands string
			if err := x.v.base.raw.QueryRow(ctxFor(t), `SELECT coalesce(jsonb_agg(to_jsonb(c) ORDER BY id),'[]'::jsonb)::text FROM agenteam_work.task_transition_commands c WHERE project_id=$1::uuid`, x.request.ProjectID.String()).Scan(&commands); err != nil {
				t.Fatal("original transition command snapshot", err)
			}
			return x.snapshot(t) + "\n" + commands
		}
		stable := snapshot()
		v.requireLookup(t, v.request(t, browser, "POST", v.lookupPath(), lookup, key), receipt)
		replayed := v.request(t, browser, "POST", v.transferPath(), body, key)
		if replayed.status != 200 || !bytes.Equal(replayed.body, reply.body) || snapshot() != stable {
			t.Fatal("original-key Lookup/replay rewrote resolved facts, history, Dispatch or Activity")
		}
		x.requireNoExecution(t)
	})
}

func technicalResolutionHTTPFixture(t *testing.T, d *taskTransitionFixture) *taskHumanHTTPFixture {
	t.Helper()
	// Only the required Blocker HTTP binding is added to the existing real
	// Work graph. The original transition/claim/failure services remain owned
	// by their existing fixtures and are never replaced by controlled ports.
	catalog := event.NewCatalog()
	events, err := wc.RegisterTaskBlockerEvents(catalog)
	if err != nil {
		t.Fatal(err)
	}
	box, err := outbox.New(d.base.tracked, catalog, outbox.Authorizations{
		Producers: map[event.StableName]oc.ProducerAuthority{wc.WorkProducer: d.authority},
		Sessions:  d.base.accounts, System: d.base.accounts, Projects: d.base.projectAuthority,
		Audit: d.base.audit, Cursors: d.base.keys, Processes: fixtureProcess{id[oc.Process](t)},
	})
	if err != nil {
		t.Fatal(err)
	}
	blockers, err := work.NewBlocker(d.base.tracked, work.BlockerDependencies{
		Authority: d.authority, Structure: d.structureReader, Events: box, BlockerEvents: events, Activity: d.base.accounts,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		blockers.Stop()
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := blockers.Drain(ctx); err != nil {
			t.Error("original Blocker service did not join", err)
		}
	})
	reader, err := work.NewBlockerReader(d.base.tracked, d.authority, d.base.keys)
	if err != nil {
		t.Fatal(err)
	}
	return serveTaskHumanHTTPFixture(t, d, workhttp.Bindings{
		Structure: d.structure, StructureReader: d.structureReader, Tasks: d.tasks, TaskReader: d.taskReader,
		Blockers: blockers, BlockerReader: reader, Transitions: d.transitions,
	})
}

func requireTechnicalResolutionHTTPFacts(t *testing.T, x *schedulerFailureFixture, key f.IdempotencyKey, blocker wc.TaskBlockerID, receipt wc.TaskTransitionMutation) {
	t.Helper()
	var command string
	var raw []byte
	var history []string
	if err := x.v.base.raw.QueryRow(ctxFor(t), `SELECT c.id::text,c.receipt,
 (SELECT array_agg(h.id::text ORDER BY h.id) FROM agenteam_work.task_events h WHERE h.project_id=c.project_id AND h.task_id=c.task_id AND h.transition_operation_id=c.id)
 FROM agenteam_work.task_transition_commands c WHERE c.project_id=$1::uuid AND c.task_id=$2::uuid AND c.idempotency_key=$3 AND c.state='completed'`, x.request.ProjectID.String(), x.v.task.ID.String(), string(key)).Scan(&command, &raw, &history); err != nil {
		t.Fatal("completed original transition command missing", err)
	}
	stored, err := wc.DecodeTaskTransitionMutation(raw)
	if err != nil || !bytes.Equal(jsonBytes(t, stored), jsonBytes(t, receipt)) || len(history) != 2 || history[0] != receipt.TaskEventIDs[0].String() || history[1] != receipt.TaskEventIDs[1].String() {
		t.Fatal("HTTP receipt differs from the original stored receipt", err)
	}
	var counts [6]int64
	err = x.v.base.raw.QueryRow(ctxFor(t), `SELECT
 (SELECT count(*) FROM agenteam_work.task_events WHERE project_id=$1::text::uuid AND task_id=$2::text::uuid AND transition_operation_id=$3::text::uuid),
 (SELECT count(*) FROM agenteam_work.task_events WHERE project_id=$1::text::uuid AND task_id=$2::text::uuid AND transition_operation_id=$3::text::uuid AND correlation_id=$3::text::uuid AND task_version=$4 AND type='blocker_resolved' AND payload->>'blocker_id'=$5::text AND actor=jsonb_build_object('type','human','user_id',$6::text,'source','task_domain')),
 (SELECT count(*) FROM agenteam_work.task_events WHERE project_id=$1::text::uuid AND task_id=$2::text::uuid AND transition_operation_id=$3::text::uuid AND correlation_id=$3::text::uuid AND task_version=$4 AND type='state_changed' AND payload->>'from_state'='blocked' AND payload->>'to_state'='todo' AND actor=jsonb_build_object('type','human','user_id',$6::text,'source','task_domain')),
 (SELECT count(*) FROM agenteam_outbox.events WHERE id=$7::text::uuid AND project_id=$1::text::uuid AND aggregate_id=$2::text::uuid AND aggregate_version=$4 AND producer='work' AND event_type='work.task_transitioned' AND schema_version=1 AND convert_from(payload,'UTF8')::jsonb->>'command_id'=$3::text),
 (SELECT count(*) FROM agenteam_scheduler.dispatches WHERE project_id=$1::text AND id=$8::text AND status='failed' AND launch_outcome='known_not_created' AND attempt_count=1 AND execution_id IS NULL),
 (SELECT count(*) FROM agenteam_execution.executions WHERE project_id=$1::text)`, x.request.ProjectID.String(), x.v.task.ID.String(), command, int64(receipt.Task.Version), blocker.String(), x.v.base.ownerBrowser.actor.Details().UserID, receipt.EventIDs[0].String(), x.dispatch.String()).Scan(&counts[0], &counts[1], &counts[2], &counts[3], &counts[4], &counts[5])
	if err != nil || counts != ([6]int64{2, 1, 1, 1, 1, 0}) {
		t.Fatal("original resolution/state history, Outbox or Dispatch facts differ", err)
	}
}
