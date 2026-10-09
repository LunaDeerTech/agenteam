//go:build integration

package work_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/LunaDeerTech/agenteam/db/migrations"
	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	c "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	wc "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
	pgfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/postgres"
)

func TestTaskPlanningMigration(t *testing.T) {
	for _, upgrade := range []bool{false, true} {
		t.Run(fmt.Sprintf("populated=%t", upgrade), func(t *testing.T) {
			db := pgfixture.NewDatabase(t)
			if upgrade {
				migrate(t, db, migrationPrefix(t, "00021"))
			} else {
				migrate(t, db)
			}
			raw := openStore(t, db.Config(t, nil))
			f := assemble(t, db, raw, raw, true)
			a := f.human(t, "task-migration", "user")
			p, _, _ := f.create(t, a, "migration")
			m := f.milestone(t, a, p.ID, "old milestone")
			s := f.sprint(t, a, p.ID, m.ID, "old sprint")
			before := f.snapshot(t, a)
			migrate(t, db)
			migrate(t, db)
			if f.snapshot(t, a) != before {
				t.Fatal("00022 rewrote old Structure/Activity facts")
			}
			conn := db.Connect(t)
			var tables, foreign int
			if err := conn.QueryRow(ctxFor(t), `SELECT count(*) FROM pg_tables WHERE schemaname='agenteam_work' AND tablename IN ('tasks','task_order_groups','task_query_generations','task_commands','task_events','milestones','sprints','milestone_order_groups','sprint_order_groups','structure_commands')`).Scan(&tables); err != nil || tables != 10 {
				t.Fatal("five new plus five original named tables", tables, err)
			}
			if err := conn.QueryRow(ctxFor(t), `SELECT count(*) FROM pg_constraint c JOIN pg_class a ON a.oid=c.conrelid JOIN pg_namespace an ON an.oid=a.relnamespace JOIN pg_class b ON b.oid=c.confrelid JOIN pg_namespace bn ON bn.oid=b.relnamespace WHERE c.contype='f' AND an.nspname='agenteam_work' AND bn.nspname<>'agenteam_work'`).Scan(&foreign); err != nil || foreign != 0 {
				t.Fatal("cross-domain FK", foreign, err)
			}
			tf := assembleTask(t, db, raw, raw, false)
			task := tf.task(t, a, p.ID, s.ID, "constraint target")
			for _, tc := range []struct{ name, sql, state string }{
				{"composite-parent", `UPDATE agenteam_work.tasks SET milestone_id='01900000-0000-7000-8000-000000000001' WHERE id=$1`, "23503"},
				{"type", `UPDATE agenteam_work.tasks SET type='TASK' WHERE id=$1`, "23514"}, {"priority", `UPDATE agenteam_work.tasks SET priority='urgent' WHERE id=$1`, "23514"}, {"state", `UPDATE agenteam_work.tasks SET state='unknown' WHERE id=$1`, "23514"},
				{"assignee", `UPDATE agenteam_work.tasks SET state='todo' WHERE id=$1`, "23514"}, {"version", `UPDATE agenteam_work.tasks SET version=0 WHERE id=$1`, "23514"}, {"low-rank", `UPDATE agenteam_work.tasks SET manual_rank=repeat('0',32) WHERE id=$1`, "23514"}, {"high-rank", `UPDATE agenteam_work.tasks SET manual_rank=repeat('f',32) WHERE id=$1`, "23514"}, {"time", `UPDATE agenteam_work.tasks SET updated_at=created_at-interval '1 second' WHERE id=$1`, "23514"},
			} {
				t.Run(tc.name, func(t *testing.T) {
					_, err := conn.Exec(ctxFor(t), tc.sql, task.ID.String())
					requirePGState(t, err, tc.state)
				})
			}
			for _, state := range []string{"todo", "in_progress", "in_review", "blocked", "done", "cancelled", "backlog"} {
				_, err := conn.Exec(ctxFor(t), `UPDATE agenteam_work.tasks SET state=$2,assignee_agent_id=$3 WHERE id=$1`, task.ID.String(), state, id[identity.Agent](t).String())
				if err != nil {
					t.Fatal("legal state/assignee rejected", state, err)
				}
			}
			_, err := conn.Exec(ctxFor(t), `DELETE FROM agenteam_work.sprints WHERE id=$1`, s.ID.String())
			requirePGState(t, err, "23503")
		})
	}
	t.Run("DDL-failure-rollback", func(t *testing.T) {
		db := pgfixture.NewDatabase(t)
		migrate(t, db, migrationPrefix(t, "00021"))
		files := migrationFiles(t, "00021")
		raw, err := fs.ReadFile(migrations.SQL, "00022_task_planning.sql")
		if err != nil {
			t.Fatal(err)
		}
		files["00022_task_planning.sql"] = &fstest.MapFile{Data: append(append([]byte{}, raw...), []byte("\nSELECT 1/0;\n")...)}
		source, err := postgres.NewSource(files, nil)
		if err != nil {
			t.Fatal(err)
		}
		m, err := postgres.NewMigrator(db.Config(t, nil), source)
		if err != nil {
			t.Fatal(err)
		}
		result := m.Migrate(ctxFor(t))
		if result.Migrated || result.Fault == nil {
			t.Fatal("failed DDL accepted")
		}
		conn := db.Connect(t)
		var n int
		if err = conn.QueryRow(ctxFor(t), `SELECT count(*) FROM pg_tables WHERE schemaname='agenteam_work'`).Scan(&n); err != nil || n != 5 {
			t.Fatal("DDL partially escaped rollback", n, err)
		}
		if err = conn.QueryRow(ctxFor(t), `SELECT count(*) FROM agenteam_meta.goose_db_version WHERE version_id=22 AND is_applied`).Scan(&n); err != nil || n != 0 {
			t.Fatal("failed22 marked applied", n, err)
		}
	})
}

func TestTaskPlanningPersistenceAndPaging(t *testing.T) {
	f := newTaskFixture(t)
	a := f.human(t, "task-pages", "user")
	p, _, _ := f.create(t, a, "pages")
	m := f.milestone(t, a, p.ID, "m")
	s := f.sprint(t, a, p.ID, m.ID, "s")
	s2 := f.sprint(t, a, p.ID, m.ID, "s2")
	req := wc.TaskCreate{TaskID: id[wc.Task](t), SprintID: s.ID, Title: "duplicate %_\\ literal", Type: wc.TaskTypeFeature, Priority: wc.TaskPriorityCritical, Description: strings.Repeat("<>&\"\\\t\n\r", 4096), Plan: strings.Repeat("<>&\"\\\t\n\r", 4096)}
	cm := meta(t, "maximum", nil)
	original, err := f.tasks.CreateTask(ctxFor(t), a, cm, p.ID, req)
	if err != nil {
		t.Fatal("both maximum escaped texts", err)
	}
	digest, err := wc.TaskCreateDigest(a, cm, p.ID, req)
	if err != nil {
		t.Fatal(err)
	}
	lookup, err := f.tasks.LookupTaskCommand(ctxFor(t), a, wc.TaskCommandLookupRequest{ProjectID: p.ID, Command: wc.TaskCommandCreate, IdempotencyKey: cm.IdempotencyKey, SemanticDigest: digest})
	if err != nil || lookup.Receipt == nil {
		t.Fatal("maximum receipt Lookup", err)
	}
	equalTaskMutation(t, original, *lookup.Receipt)
	var decoded wc.TaskCommandLookup
	if err = json.Unmarshal(jsonBytes(t, lookup), &decoded); err != nil {
		t.Fatal("maximum lookup wire", err)
	}
	var tasks []wc.Task
	tasks = append(tasks, original.Task)
	for n := 1; n < 205; n++ {
		parent := s.ID
		if n%2 == 0 {
			parent = s2.ID
		}
		tasks = append(tasks, f.task(t, a, p.ID, parent, "duplicate"))
	}
	rebuilt, _ := observedTaskFixture(t, f)
	got, err := rebuilt.taskReader.GetTask(ctxFor(t), a, p.ID, original.Task.ID)
	if err != nil || string(jsonBytes(t, got)) != string(jsonBytes(t, original.Task)) {
		t.Fatal("rebuilt service did not preserve canonical", err)
	}
	seen := map[string]bool{}
	page := foundation.DefaultPageRequest()
	firstToken := ""
	var previous wc.Task
	for {
		out, err := rebuilt.taskReader.ListTasks(ctxFor(t), a, p.ID, wc.TaskFilter{}, page)
		if err != nil {
			t.Fatal(err)
		}
		if firstToken == "" {
			if len(out.Items) != 50 {
				t.Fatal("default page size", len(out.Items))
			}
			firstToken = out.NextCursor
		}
		for _, item := range out.Items {
			if seen[item.ID.String()] {
				t.Fatal("duplicate page member")
			}
			seen[item.ID.String()] = true
			if previous.ID.String() != "" && previous.SprintID.String() > item.SprintID.String() {
				t.Fatal("cross Sprint total order")
			}
			previous = item
		}
		if out.NextCursor == "" {
			break
		}
		page = foundation.PageRequest{Cursor: out.NextCursor, Limit: 7}
	}
	if len(seen) != 205 {
		t.Fatal("page truncated", len(seen))
	}
	renewed := f.renew(t, a)
	if _, err = f.taskReader.ListTasks(ctxFor(t), renewed, p.ID, wc.TaskFilter{}, foundation.PageRequest{Cursor: firstToken, Limit: 1}); err != nil {
		t.Fatal("Session/limit incorrectly bound cursor", err)
	}
	state := wc.TaskStateBacklog
	priority := wc.TaskPriorityCritical
	typ := wc.TaskTypeFeature
	text := "%_\\"
	filters := []wc.TaskFilter{{State: &state}, {Priority: &priority}, {Type: &typ}, {AssigneeAgentID: wc.TaskAssigneeFilter{Present: true}}, {MilestoneID: &m.ID}, {SprintID: &s.ID}, {Text: &text}, {State: &state, Priority: &priority, Type: &typ, MilestoneID: &m.ID, SprintID: &s.ID, Text: &text, AssigneeAgentID: wc.TaskAssigneeFilter{Present: true}}}
	for n, filter := range filters {
		out, err := f.taskReader.ListTasks(ctxFor(t), a, p.ID, filter, foundation.PageRequest{Limit: 200})
		if err != nil || len(out.Items) == 0 {
			t.Fatal("filter", n, err)
		}
		if n == 1 || n == 2 || n == 6 || n == 7 {
			if len(out.Items) != 1 || out.Items[0].ID != original.Task.ID {
				t.Fatal("literal/combined predicate", n)
			}
		}
		_, err = f.taskReader.ListTasks(ctxFor(t), a, p.ID, filter, foundation.PageRequest{Cursor: firstToken, Limit: 1})
		requireCode(t, err, foundation.CursorInvalid)
	}
	emptySprint := id[c.Sprint](t)
	out, err := f.taskReader.ListTasks(ctxFor(t), a, p.ID, wc.TaskFilter{SprintID: &emptySprint}, foundation.DefaultPageRequest())
	if err != nil || len(out.Items) != 0 {
		t.Fatal("nonmatching valid filter", err)
	}
	title := "changed"
	updated, err := f.tasks.UpdateTask(ctxFor(t), a, meta(t, "update", &original.Task.Version), p.ID, original.Task.ID, wc.TaskFieldsUpdate{Title: &title})
	if err != nil || updated.Task.Version != 2 {
		t.Fatal("update", err)
	}
	_, err = f.taskReader.ListTasks(ctxFor(t), a, p.ID, wc.TaskFilter{}, foundation.PageRequest{Cursor: firstToken, Limit: 1})
	requireCode(t, err, foundation.CursorStale)
	// Future canonical values are seeded only to prove reader predicates.
	agent := id[identity.Agent](t)
	f.seedTaskState(t, a, tasks[1], wc.TaskStateInProgress, &agent)
	out, err = f.taskReader.ListTasks(ctxFor(t), a, p.ID, wc.TaskFilter{AssigneeAgentID: wc.TaskAssigneeFilter{Present: true, AgentID: &agent}}, foundation.DefaultPageRequest())
	if err != nil || len(out.Items) != 1 || out.Items[0].State != wc.TaskStateInProgress || out.Items[0].AssigneeAgentID == nil {
		t.Fatal("persistent assigned state lost", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	out, err = f.taskReader.ListTasks(ctx, a, p.ID, wc.TaskFilter{}, foundation.DefaultPageRequest())
	if !errors.Is(err, context.Canceled) || len(out.Items) != 0 {
		t.Fatal("cancelled read pretended empty success", err)
	}
}

func TestTaskPlanningAuthorityAndReplay(t *testing.T) {
	f := newTaskFixture(t)
	a := f.human(t, "task-authority", "user")
	other := f.human(t, "task-other", "user")
	admin := f.human(t, "task-admin", "admin")
	p, _, _ := f.create(t, a, "authority")
	m := f.milestone(t, a, p.ID, "m")
	s := f.sprint(t, a, p.ID, m.ID, "s")
	target := f.task(t, a, p.ID, s.ID, "before")
	title := "after"
	request := wc.TaskFieldsUpdate{Title: &title}
	cm := meta(t, "original", &target.Version)
	original, err := f.tasks.UpdateTask(ctxFor(t), a, cm, p.ID, target.ID, request)
	if err != nil {
		t.Fatal(err)
	}
	for _, actor := range []identity.Actor{other, admin} {
		_, err = f.tasks.UpdateTask(ctxFor(t), actor, cm, p.ID, target.ID, request)
		requireCode(t, err, foundation.NotFound)
	}
	_, err = f.tasks.UpdateTask(ctxFor(t), identity.Actor{}, cm, p.ID, target.ID, request)
	requireCode(t, err, foundation.Unauthenticated)
	agent, err := identity.NewAgentRun(p.ID, id[identity.Agent](t), id[identity.Execution](t))
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.tasks.UpdateTask(ctxFor(t), agent, cm, p.ID, target.ID, request)
	requireCode(t, err, foundation.DependencyUnbound)
	registration, err := identity.RegisterService(identity.ProjectLifecycle)
	if err != nil {
		t.Fatal(err)
	}
	scope, err := identity.InProject(p.ID)
	if err != nil {
		t.Fatal(err)
	}
	service, err := registration.Actor(id[struct{}](t).String(), scope)
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.tasks.UpdateTask(ctxFor(t), service, cm, p.ID, target.ID, request)
	requireCode(t, err, foundation.Forbidden)
	altered := "different"
	_, err = f.tasks.UpdateTask(ctxFor(t), a, cm, p.ID, target.ID, wc.TaskFieldsUpdate{Title: &altered})
	requireCode(t, err, foundation.IdempotencyKeyReused)
	absent := id[wc.Task](t)
	_, err = f.tasks.UpdateTask(ctxFor(t), a, cm, p.ID, absent, request)
	requireCode(t, err, foundation.IdempotencyKeyReused)
	for _, tc := range []struct {
		state    wc.TaskState
		assigned bool
		code     foundation.Code
	}{{wc.TaskStateTodo, false, foundation.TaskAssigneeRequired}, {wc.TaskStateTodo, true, foundation.DependencyUnbound}, {wc.TaskStateBacklog, true, foundation.DependencyUnbound}, {wc.TaskStateDone, false, foundation.TaskStateInvalid}} {
		r := wc.TaskCreate{TaskID: id[wc.Task](t), SprintID: s.ID, Title: "unsupported", Type: wc.TaskTypeTask, Priority: wc.TaskPriorityLow, InitialState: tc.state}
		if tc.assigned {
			v := id[identity.Agent](t)
			r.AssigneeAgentID = &v
		}
		_, err = f.tasks.CreateTask(ctxFor(t), a, meta(t, id[struct{}](t).String(), nil), p.ID, r)
		requireCode(t, err, tc.code)
	}
	f.seedTaskState(t, a, original.Task, wc.TaskStateDone, nil)
	replay, err := f.tasks.UpdateTask(ctxFor(t), a, cm, p.ID, target.ID, request)
	if err != nil {
		t.Fatal("history preceded terminal/version", err)
	}
	equalTaskMutation(t, original, replay)
	_, err = f.tasks.UpdateTask(ctxFor(t), a, meta(t, "terminal", &original.Task.Version), p.ID, target.ID, request)
	requireCode(t, err, foundation.TaskTerminalImmutable)
	fresh := f.renew(t, a)
	user, _ := foundation.UserLock(a.Details().UserID)
	f.tx(t, []foundation.LockRequest{{Key: user, Mode: foundation.Exclusive}}, func(ctx context.Context, _ foundation.Tx, x postgres.SQLExecutor) error {
		_, err := x.Exec(ctx, `UPDATE agenteam_account.sessions SET revoked_at=clock_timestamp(),revoked_reason='administrative' WHERE id=$1`, a.Details().SessionID)
		return err
	})
	_, err = f.tasks.UpdateTask(ctxFor(t), a, cm, p.ID, target.ID, request)
	requireCode(t, err, foundation.SessionRevoked)
	_, err = f.taskReader.ListTasks(ctxFor(t), a, p.ID, wc.TaskFilter{}, foundation.PageRequest{Limit: 1, Cursor: "bad"})
	requireCode(t, err, foundation.SessionRevoked)
	a = fresh
	f.lifecycle(t, a, p.ID, s, true)
	replay, err = f.tasks.UpdateTask(ctxFor(t), a, cm, p.ID, target.ID, request)
	if err != nil {
		t.Fatal(err)
	}
	equalTaskMutation(t, original, replay)
	_, err = f.tasks.UpdateTask(ctxFor(t), a, meta(t, "completed", &original.Task.Version), p.ID, target.ID, request)
	requireCode(t, err, foundation.TaskSprintInvalid)
	f.seedLifecycle(t, a, p.ID, c.Archived)
	replay, err = f.tasks.UpdateTask(ctxFor(t), a, cm, p.ID, target.ID, request)
	if err != nil {
		t.Fatal("archived completed history", err)
	}
	equalTaskMutation(t, original, replay)
	_, err = f.tasks.UpdateTask(ctxFor(t), a, meta(t, "archived-new", &original.Task.Version), p.ID, target.ID, request)
	requireCode(t, err, foundation.ProjectNotActive)
}

type taskFailAfterHistory struct {
	oc.Appender
	seen bool
}

func (a *taskFailAfterHistory) AppendEventInTx(ctx context.Context, tx foundation.Tx, actor identity.Actor, e event.Event, p oc.AppendPlan) (oc.AppendReceipt, error) {
	a.seen = true
	return oc.AppendReceipt{}, foundation.NewFault(foundation.DependencyUnavailable, foundation.NotStarted)
}

func TestTaskPlanningAtomicityAndEvents(t *testing.T) {
	f := newTaskFixture(t)
	a := f.human(t, "task-atomic", "user")
	p, _, _ := f.create(t, a, "atomic")
	m := f.milestone(t, a, p.ID, "m")
	s := f.sprint(t, a, p.ID, m.ID, "s")
	first := f.task(t, a, p.ID, s.ID, "first")
	second := f.task(t, a, p.ID, s.ID, "second")
	for _, point := range []string{"after-history", "after-outbox", "after-activity"} {
		t.Run(point, func(t *testing.T) {
			f.ageSession(t, a)
			before := f.taskSnapshot(t, a)
			var app oc.Appender = f.events
			activity := f.accounts
			var writer interface {
				UpdateTask(context.Context, identity.Actor, foundation.CommandMeta, wc.ProjectID, wc.TaskID, wc.TaskFieldsUpdate) (wc.TaskMutation, error)
			}
			history := &taskFailAfterHistory{Appender: f.events}
			if point == "after-history" {
				app = history
			}
			if point == "after-outbox" {
				app = &capturingAppender{Appender: f.events, failAppend: true}
			}
			if point == "after-activity" {
				writer = f.newTaskService(t, app, failActivity{activity})
			} else {
				writer = f.newTaskService(t, app, activity)
			}
			title := point
			_, err := writer.UpdateTask(ctxFor(t), a, meta(t, point, &first.Version), p.ID, first.ID, wc.TaskFieldsUpdate{Title: &title})
			requireCode(t, err, foundation.DependencyUnavailable)
			if point == "after-history" && !history.seen {
				t.Fatal("history boundary not reached")
			}
			if before != f.taskSnapshot(t, a) {
				t.Fatal("partial canonical/group/query/history/outbox/completed/touch escaped rollback", point)
			}
		})
	}
	title := "updated"
	capture := &capturingAppender{Appender: f.events}
	writer := f.newTaskService(t, capture, f.accounts)
	changed, err := writer.UpdateTask(ctxFor(t), a, meta(t, "real-update", &first.Version), p.ID, first.ID, wc.TaskFieldsUpdate{Title: &title})
	if err != nil || changed.TaskEventID == nil {
		t.Fatal(err)
	}
	payload := taskPayload(t, f, *changed.TaskEventID)
	if string(payload["changed_fields"]) != `["title"]` || string(payload["position"]) != "null" {
		t.Fatal("unsafe or wrong updated history", payload)
	}
	reordered, err := writer.ReorderTask(ctxFor(t), a, meta(t, "real-reorder", &second.Version), p.ID, second.ID, wc.TaskReorder{BeforeID: &first.ID})
	if err != nil || reordered.TaskEventID == nil {
		t.Fatal(err)
	}
	payload = taskPayload(t, f, *reordered.TaskEventID)
	if string(payload["changed_fields"]) != `["manual_rank"]` || string(payload["position"]) == "null" {
		t.Fatal("reorder history absent")
	}
	h, e := f.taskCounts(t)
	noop, err := writer.UpdateTask(ctxFor(t), a, meta(t, "noop", &changed.Task.Version), p.ID, first.ID, wc.TaskFieldsUpdate{Title: &title})
	if err != nil || noop.Changed || noop.TaskEventID != nil || len(noop.EventIDs) != 0 {
		t.Fatal("noop receipt", err)
	}
	hh, ee := f.taskCounts(t)
	if h != hh || e != ee {
		t.Fatal("noop emitted history")
	}
	captured := capture.last(t)
	failed := f.store.WithinTx(ctxFor(t), cause(t), func(ctx context.Context, tx foundation.Tx) error {
		return f.authority.ValidateAppendInTx(ctx, tx, a, captured.Event.Summary(), captured.Plan.Details().Producer, oc.NewFact)
	})
	if failed.State() != foundation.NotCommitted {
		t.Fatal("producer without locks accepted")
	}
	failed = f.store.WithinTx(ctxFor(t), cause(t), func(ctx context.Context, tx foundation.Tx) error {
		if err := f.store.AcquireAll(ctx, tx, captured.Plan.Locks()); err != nil {
			return err
		}
		return f.authority.ValidateAppendInTx(ctx, tx, a, captured.Event.Summary(), captured.Plan.Details().Producer, oc.NewFact)
	})
	if failed.State() != foundation.NotCommitted {
		t.Fatal("completed receipt accepted as new fact")
	}
}
