//go:build integration

package work_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/LunaDeerTech/agenteam/db/migrations"
	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	c "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/work"
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
			f.lifecycle(t, a, p.ID, s, false)
			before := taskOldDomainSnapshot(t, raw)
			migrate(t, db)
			migrate(t, db)
			if taskOldDomainSnapshot(t, raw) != before {
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
			var empty int
			if err := conn.QueryRow(ctxFor(t), `SELECT (SELECT count(*) FROM agenteam_work.tasks)+(SELECT count(*) FROM agenteam_work.task_order_groups)+(SELECT count(*) FROM agenteam_work.task_query_generations)+(SELECT count(*) FROM agenteam_work.task_commands)+(SELECT count(*) FROM agenteam_work.task_events)`).Scan(&empty); err != nil || empty != 0 {
				t.Fatal("upgrade precreated Task facts", empty, err)
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
			taskMigrationConstraints(t, tf, a, task)
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
		store := openStore(t, db.Config(t, nil))
		old := assemble(t, db, store, store, true)
		actor := old.human(t, "ddl-rollback", "user")
		project, _, _ := old.create(t, actor, "ddl-rollback")
		milestone := old.milestone(t, actor, project.ID, "retained")
		sprint := old.sprint(t, actor, project.ID, milestone.ID, "retained")
		old.lifecycle(t, actor, project.ID, sprint, false)
		before := taskOldDomainSnapshot(t, store)
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
		if err = conn.QueryRow(ctxFor(t), `SELECT count(*) FROM pg_constraint WHERE conrelid='agenteam_work.sprints'::regclass AND conname='sprints_project_milestone_id_key'`).Scan(&n); err != nil || n != 0 {
			t.Fatal("failed22 kept composite unique", n, err)
		}
		if taskOldDomainSnapshot(t, store) != before {
			t.Fatal("failed22 changed old data or pointer")
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
	tasks[0] = updated.Task
	taskPagingMatrix(t, f, a, p.ID, tasks)
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

// Full rows include original planned commands, all timestamps, and the Project
// current Sprint pointer. This is deliberately not a count-only migration oracle.
func taskOldDomainSnapshot(t *testing.T, x postgres.SQLExecutor) string {
	t.Helper()
	var snapshot string
	if err := x.QueryRow(ctxFor(t), `SELECT jsonb_build_object(
 'milestones',(SELECT coalesce(jsonb_agg(to_jsonb(t) ORDER BY id),'[]') FROM agenteam_work.milestones t),
 'sprints',(SELECT coalesce(jsonb_agg(to_jsonb(t) ORDER BY id),'[]') FROM agenteam_work.sprints t),
 'milestone_groups',(SELECT coalesce(jsonb_agg(to_jsonb(t) ORDER BY project_id),'[]') FROM agenteam_work.milestone_order_groups t),
 'sprint_groups',(SELECT coalesce(jsonb_agg(to_jsonb(t) ORDER BY project_id,milestone_id),'[]') FROM agenteam_work.sprint_order_groups t),
 'commands',(SELECT coalesce(jsonb_agg(to_jsonb(t) ORDER BY id),'[]') FROM agenteam_work.structure_commands t),
 'projects',(SELECT coalesce(jsonb_agg(to_jsonb(t) ORDER BY id),'[]') FROM agenteam_project.projects t))::text`).Scan(&snapshot); err != nil {
		t.Fatal(err)
	}
	return snapshot
}
func taskMigrationConstraints(t *testing.T, f *taskFixture, a identity.Actor, task wc.Task) {
	t.Helper()
	conn := f.db.Connect(t)
	var command, history string
	if err := conn.QueryRow(ctxFor(t), `SELECT operation_id::text,id::text FROM agenteam_work.task_events WHERE task_id=$1`, task.ID.String()).Scan(&command, &history); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ name, sql, id, code string }{
		{"title-byte-cap", `UPDATE agenteam_work.tasks SET title=repeat('a',257) WHERE id=$1`, task.ID.String(), "23514"},
		{"description-byte-cap", `UPDATE agenteam_work.tasks SET description=repeat('a',32769) WHERE id=$1`, task.ID.String(), "23514"},
		{"plan-byte-cap", `UPDATE agenteam_work.tasks SET plan=repeat('a',32769) WHERE id=$1`, task.ID.String(), "23514"},
		{"rank-case", `UPDATE agenteam_work.tasks SET manual_rank=repeat('A',32) WHERE id=$1`, task.ID.String(), "23514"},
		{"group-parent", `UPDATE agenteam_work.task_order_groups SET milestone_id='01900000-0000-7000-8000-000000000099' WHERE project_id=$1`, task.ProjectID.String(), "23503"},
		{"group-state", `UPDATE agenteam_work.task_order_groups SET state='unknown' WHERE project_id=$1`, task.ProjectID.String(), "23514"},
		{"group-priority", `UPDATE agenteam_work.task_order_groups SET priority='unknown' WHERE project_id=$1`, task.ProjectID.String(), "23514"},
		{"group-generation", `UPDATE agenteam_work.task_order_groups SET order_generation=0 WHERE project_id=$1`, task.ProjectID.String(), "23514"},
		{"query-generation", `UPDATE agenteam_work.task_query_generations SET query_generation=0 WHERE project_id=$1`, task.ProjectID.String(), "23514"},
		{"command-name", `UPDATE agenteam_work.task_commands SET command_name='work.task.delete' WHERE id=$1`, command, "23514"},
		{"command-revision", `UPDATE agenteam_work.task_commands SET plan_revision=0 WHERE id=$1`, command, "23514"},
		{"command-phase", `UPDATE agenteam_work.task_commands SET state='planned' WHERE id=$1`, command, "23514"},
		{"command-receipt", `UPDATE agenteam_work.task_commands SET receipt='{}' WHERE id=$1`, command, "23514"},
		{"command-event-pair", `UPDATE agenteam_work.task_commands SET event_id=NULL WHERE id=$1`, command, "23514"},
		{"command-request-cap", `UPDATE agenteam_work.task_commands SET request=jsonb_build_object('x',repeat('x',524289)) WHERE id=$1`, command, "23514"},
		{"command-plan-cap", `UPDATE agenteam_work.task_commands SET plan=jsonb_build_object('x',repeat('x',4194305)) WHERE id=$1`, command, "23514"},
		{"history-version", `UPDATE agenteam_work.task_events SET task_version=0 WHERE id=$1`, history, "23514"},
		{"history-type", `UPDATE agenteam_work.task_events SET type='plan_updated' WHERE id=$1`, history, "23514"},
		{"history-human", `UPDATE agenteam_work.task_events SET actor=jsonb_set(actor,'{type}','"service"') WHERE id=$1`, history, "23514"},
		{"history-source", `UPDATE agenteam_work.task_events SET actor=jsonb_set(actor,'{source}','"transport"') WHERE id=$1`, history, "23514"},
		{"history-correlation", `UPDATE agenteam_work.task_events SET correlation_id='01900000-0000-7000-8000-000000000099' WHERE id=$1`, history, "23514"},
		{"history-task-fk", `UPDATE agenteam_work.task_events SET task_id='01900000-0000-7000-8000-000000000099' WHERE id=$1`, history, "23503"},
		{"history-operation-fk", `UPDATE agenteam_work.task_events SET operation_id='01900000-0000-7000-8000-000000000099',correlation_id='01900000-0000-7000-8000-000000000099' WHERE id=$1`, history, "23503"},
		{"history-payload-cap", `UPDATE agenteam_work.task_events SET payload=jsonb_build_object('x',repeat('x',8193)) WHERE id=$1`, history, "23514"},
	} {
		t.Run(tc.name, func(t *testing.T) { _, err := conn.Exec(ctxFor(t), tc.sql, tc.id); requirePGState(t, err, tc.code) })
	}
	other := f.task(t, a, task.ProjectID, task.SprintID, "rank uniqueness")
	_, err := conn.Exec(ctxFor(t), `UPDATE agenteam_work.tasks SET manual_rank=$2 WHERE id=$1`, other.ID.String(), task.ManualRank)
	requirePGState(t, err, "23505")
	for _, typ := range []string{"feature", "bug", "task", "spike", "chore"} {
		if _, err := conn.Exec(ctxFor(t), `UPDATE agenteam_work.tasks SET type=$2 WHERE id=$1`, task.ID.String(), typ); err != nil {
			t.Fatal("legal type", err)
		}
	}
	for _, priority := range []string{"low", "medium", "high", "critical"} {
		if _, err := conn.Exec(ctxFor(t), `UPDATE agenteam_work.tasks SET priority=$2 WHERE id=$1`, task.ID.String(), priority); err != nil {
			t.Fatal("legal priority", err)
		}
	}
}
func taskAllPages(t *testing.T, f *taskFixture, a identity.Actor, p wc.ProjectID, filter wc.TaskFilter, limit int) []wc.Task {
	t.Helper()
	out := []wc.Task{}
	request := foundation.PageRequest{Limit: limit}
	seen := map[wc.TaskID]bool{}
	for {
		page, err := f.taskReader.ListTasks(ctxFor(t), a, p, filter, request)
		if err != nil {
			t.Fatal(err)
		}
		for _, task := range page.Items {
			if seen[task.ID] {
				t.Fatal("duplicate Task across filter pages")
			}
			seen[task.ID] = true
			out = append(out, task)
		}
		if page.NextCursor == "" {
			return out
		}
		if page.NextCursor == request.Cursor {
			t.Fatal("cursor failed to advance")
		}
		request.Cursor = page.NextCursor
	}
}
func taskExpectedFilter(v wc.Task, filter wc.TaskFilter) bool {
	return (filter.State == nil || v.State == *filter.State) && (filter.Priority == nil || v.Priority == *filter.Priority) && (filter.Type == nil || v.Type == *filter.Type) && (filter.MilestoneID == nil || v.MilestoneID == *filter.MilestoneID) && (filter.SprintID == nil || v.SprintID == *filter.SprintID) && (!filter.AssigneeAgentID.Present || (v.AssigneeAgentID == nil && filter.AssigneeAgentID.AgentID == nil) || (v.AssigneeAgentID != nil && filter.AssigneeAgentID.AgentID != nil && *v.AssigneeAgentID == *filter.AssigneeAgentID.AgentID)) && (filter.Text == nil || strings.Contains(v.Title, *filter.Text) || strings.Contains(v.Description, *filter.Text) || strings.Contains(v.Plan, *filter.Text))
}
func taskPagingMatrix(t *testing.T, f *taskFixture, a identity.Actor, p wc.ProjectID, tasks []wc.Task) {
	t.Helper()
	agentA, agentB := id[identity.Agent](t), id[identity.Agent](t)
	states := []wc.TaskState{wc.TaskStateBacklog, wc.TaskStateTodo, wc.TaskStateInProgress, wc.TaskStateInReview, wc.TaskStateBlocked, wc.TaskStateDone, wc.TaskStateCancelled}
	priorities := []wc.TaskPriority{wc.TaskPriorityCritical, wc.TaskPriorityHigh, wc.TaskPriorityMedium, wc.TaskPriorityLow}
	schedule, _ := foundation.ProjectScheduleLock(p.String())
	f.tx(t, fixtureLocks(a, p, foundation.LockRequest{Key: schedule, Mode: foundation.Exclusive}), func(ctx context.Context, tx foundation.Tx, x postgres.SQLExecutor) error {
		if _, err := f.projectAuthority.RequireOwnerInTx(ctx, tx, a, p, identity.Mutate); err != nil {
			return err
		}
		for n := 0; n < 28; n++ {
			task := &tasks[n+1]
			task.State = states[n/4]
			task.Priority = priorities[n%4]
			task.AssigneeAgentID = nil
			if n%3 == 0 {
				task.AssigneeAgentID = &agentA
			} else if n%3 == 1 || task.State == wc.TaskStateTodo || task.State == wc.TaskStateInProgress || task.State == wc.TaskStateInReview {
				task.AssigneeAgentID = &agentB
			}
			var agent any
			if task.AssigneeAgentID != nil {
				agent = task.AssigneeAgentID.String()
			}
			if _, err := x.Exec(ctx, `UPDATE agenteam_work.tasks SET state=$2,priority=$3,assignee_agent_id=$4 WHERE id=$1`, task.ID.String(), string(task.State), string(task.Priority), agent); err != nil {
				return err
			}
		}
		if _, err := x.Exec(ctx, `INSERT INTO agenteam_work.task_order_groups(project_id,milestone_id,sprint_id,state,priority,order_generation) SELECT DISTINCT project_id,milestone_id,sprint_id,state,priority,1 FROM agenteam_work.tasks WHERE project_id=$1 ON CONFLICT DO NOTHING`, p.String()); err != nil {
			return err
		}
		_, err := x.Exec(ctx, `UPDATE agenteam_work.task_query_generations SET query_generation=query_generation+1 WHERE project_id=$1`, p.String())
		return err
	})
	t.Log("test-owned future state/assignee geometry proves persistent predicates only; no Agent assignment or state transition API")
	title, description, plan := "Case%_\\Title", "OnlyDescription", "OnlyPlan"
	updated, err := f.tasks.UpdateTask(ctxFor(t), a, meta(t, "literal-fields", &tasks[29].Version), p, tasks[29].ID, wc.TaskFieldsUpdate{Title: &title, Description: &description, Plan: &plan})
	if err != nil {
		t.Fatal(err)
	}
	tasks[29] = updated.Task
	title, description, plan = "Cross", "Field", "Edge"
	updated, err = f.tasks.UpdateTask(ctxFor(t), a, meta(t, "separate-fields", &tasks[30].Version), p, tasks[30].ID, wc.TaskFieldsUpdate{Title: &title, Description: &description, Plan: &plan})
	if err != nil {
		t.Fatal(err)
	}
	tasks[30] = updated.Task
	stateIndex := map[wc.TaskState]int{wc.TaskStateBacklog: 0, wc.TaskStateTodo: 1, wc.TaskStateInProgress: 2, wc.TaskStateInReview: 3, wc.TaskStateBlocked: 4, wc.TaskStateDone: 5, wc.TaskStateCancelled: 6}
	priorityIndex := map[wc.TaskPriority]int{wc.TaskPriorityCritical: 0, wc.TaskPriorityHigh: 1, wc.TaskPriorityMedium: 2, wc.TaskPriorityLow: 3}
	sort.Slice(tasks, func(a, b int) bool {
		x, y := tasks[a], tasks[b]
		if x.SprintID != y.SprintID {
			return x.SprintID.String() < y.SprintID.String()
		}
		if x.State != y.State {
			return stateIndex[x.State] < stateIndex[y.State]
		}
		if x.Priority != y.Priority {
			return priorityIndex[x.Priority] < priorityIndex[y.Priority]
		}
		if x.ManualRank != y.ManualRank {
			return x.ManualRank < y.ManualRank
		}
		return x.ID.String() < y.ID.String()
	})
	filters := []wc.TaskFilter{{}, {AssigneeAgentID: wc.TaskAssigneeFilter{Present: true}}, {AssigneeAgentID: wc.TaskAssigneeFilter{Present: true, AgentID: &agentA}}, {AssigneeAgentID: wc.TaskAssigneeFilter{Present: true, AgentID: &agentB}}}
	for _, state := range states {
		value := state
		filters = append(filters, wc.TaskFilter{State: &value})
	}
	for _, priority := range priorities {
		value := priority
		filters = append(filters, wc.TaskFilter{Priority: &value})
	}
	for _, text := range []string{"%", "_", "\\", "Case", "case", "OnlyDescription", "OnlyPlan", "CrossField", "FieldEdge"} {
		value := text
		filters = append(filters, wc.TaskFilter{Text: &value})
	}
	typ := wc.TaskTypeFeature
	filters = append(filters, wc.TaskFilter{Type: &typ}, wc.TaskFilter{MilestoneID: &tasks[0].MilestoneID}, wc.TaskFilter{SprintID: &tasks[0].SprintID}, wc.TaskFilter{State: &states[0], Priority: &priorities[0], AssigneeAgentID: wc.TaskAssigneeFilter{Present: true, AgentID: &agentA}})
	for n, filter := range filters {
		want := []wc.Task{}
		for _, v := range tasks {
			if taskExpectedFilter(v, filter) {
				want = append(want, v)
			}
		}
		got := taskAllPages(t, f, a, p, filter, 17)
		if !bytes.Equal(jsonBytes(t, got), jsonBytes(t, want)) {
			t.Fatalf("exact filter/order result %d got %d want %d", n, len(got), len(want))
		}
	}
	page, err := f.taskReader.ListTasks(ctxFor(t), a, p, wc.TaskFilter{}, foundation.PageRequest{Limit: 1})
	if err != nil || page.NextCursor == "" {
		t.Fatal("key test page", err)
	}
	encode := func(b byte) string { return base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{b}, 32)) }
	for _, retain := range []bool{true, false} {
		keys := fmt.Sprintf(`{"format":1,"current_kid":"next","keys":[{"kid":"next","key_b64":%q}`, encode(9))
		if retain {
			keys += fmt.Sprintf(`,{"kid":"c","key_b64":%q}`, encode(1))
		}
		keys += `]}`
		rotated, err := cursor.LoadKeyring(keys)
		if err != nil {
			t.Fatal(err)
		}
		reader, err := work.NewTaskReader(f.store, f.authority, f.reader, rotated)
		if err != nil {
			t.Fatal(err)
		}
		_, err = reader.ListTasks(ctxFor(t), a, p, wc.TaskFilter{}, foundation.PageRequest{Limit: 2, Cursor: page.NextCursor})
		if retain && err != nil {
			t.Fatal("retained old signing key", err)
		}
		if !retain {
			requireCode(t, err, foundation.CursorInvalid)
		}
	}
	for _, bad := range []string{"broken", page.NextCursor + "x"} {
		_, err = f.taskReader.ListTasks(ctxFor(t), a, p, wc.TaskFilter{}, foundation.PageRequest{Limit: 1, Cursor: bad})
		requireCode(t, err, foundation.CursorInvalid)
	}
	nextOwner := f.human(t, "task-cursor-new-owner", "user")
	f.transferOwner(t, a, nextOwner, p)
	_, err = f.taskReader.ListTasks(ctxFor(t), nextOwner, p, wc.TaskFilter{}, foundation.PageRequest{Limit: 1, Cursor: page.NextCursor})
	requireCode(t, err, foundation.CursorInvalid)
	f.transferOwner(t, nextOwner, a, p)
	taskCursorMutationMatrix(t, f, a, p, tasks)
}
func taskCursorMutationMatrix(t *testing.T, f *taskFixture, a identity.Actor, p wc.ProjectID, tasks []wc.Task) {
	t.Helper()
	var target wc.Task
	for _, v := range tasks {
		if v.State == wc.TaskStateBacklog && v.AssigneeAgentID == nil && v.Priority == wc.TaskPriorityMedium {
			target = v
			break
		}
	}
	if target.ID.Validate() != nil {
		t.Fatal("missing mutable target")
	}
	freshToken := func() string {
		page, err := f.taskReader.ListTasks(ctxFor(t), a, p, wc.TaskFilter{}, foundation.PageRequest{Limit: 1})
		if err != nil || page.NextCursor == "" {
			t.Fatal(err)
		}
		return page.NextCursor
	}
	for n, patch := range []wc.TaskFieldsUpdate{func() wc.TaskFieldsUpdate { v := "stale-title"; return wc.TaskFieldsUpdate{Title: &v} }(), func() wc.TaskFieldsUpdate { v := "stale-description"; return wc.TaskFieldsUpdate{Description: &v} }(), func() wc.TaskFieldsUpdate { v := "stale-plan"; return wc.TaskFieldsUpdate{Plan: &v} }(), func() wc.TaskFieldsUpdate { v := wc.TaskTypeBug; return wc.TaskFieldsUpdate{Type: &v} }(), func() wc.TaskFieldsUpdate { v := wc.TaskPriorityLow; return wc.TaskFieldsUpdate{Priority: &v} }()} {
		token := freshToken()
		out, err := f.tasks.UpdateTask(ctxFor(t), a, meta(t, fmt.Sprintf("stale-%d", n), &target.Version), p, target.ID, patch)
		if err != nil || !out.Changed {
			t.Fatal("stale mutation", err)
		}
		target = out.Task
		_, err = f.taskReader.ListTasks(ctxFor(t), a, p, wc.TaskFilter{}, foundation.PageRequest{Limit: 1, Cursor: token})
		requireCode(t, err, foundation.CursorStale)
	}
	token := freshToken()
	created, err := f.tasks.CreateTask(ctxFor(t), a, meta(t, "stale-create", nil), p, wc.TaskCreate{TaskID: id[wc.Task](t), SprintID: target.SprintID, Title: "new tail", Type: wc.TaskTypeTask, Priority: target.Priority})
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.taskReader.ListTasks(ctxFor(t), a, p, wc.TaskFilter{}, foundation.PageRequest{Limit: 1, Cursor: token})
	requireCode(t, err, foundation.CursorStale)
	token = freshToken()
	reordered, err := f.tasks.ReorderTask(ctxFor(t), a, meta(t, "stale-reorder", &created.Task.Version), p, created.Task.ID, wc.TaskReorder{BeforeID: &target.ID})
	if err != nil || !reordered.Changed {
		t.Fatal("reorder stale", err)
	}
	_, err = f.taskReader.ListTasks(ctxFor(t), a, p, wc.TaskFilter{}, foundation.PageRequest{Limit: 1, Cursor: token})
	requireCode(t, err, foundation.CursorStale)
	token = freshToken()
	metaNoop := meta(t, "cursor-noop", &target.Version)
	noop, err := f.tasks.UpdateTask(ctxFor(t), a, metaNoop, p, target.ID, wc.TaskFieldsUpdate{Title: &target.Title})
	if err != nil || noop.Changed {
		t.Fatal("noop", err)
	}
	if _, err = f.taskReader.ListTasks(ctxFor(t), a, p, wc.TaskFilter{}, foundation.PageRequest{Limit: 1, Cursor: token}); err != nil {
		t.Fatal("noop staled cursor", err)
	}
	if _, err = f.tasks.UpdateTask(ctxFor(t), a, metaNoop, p, target.ID, wc.TaskFieldsUpdate{Title: &target.Title}); err != nil {
		t.Fatal(err)
	}
	if _, err = f.taskReader.ListTasks(ctxFor(t), a, p, wc.TaskFilter{}, foundation.PageRequest{Limit: 1, Cursor: token}); err != nil {
		t.Fatal("replay staled cursor", err)
	}
	token = freshToken()
	agent := id[identity.Agent](t)
	f.seedTaskState(t, a, target, wc.TaskStateTodo, &agent)
	_, err = f.taskReader.ListTasks(ctxFor(t), a, p, wc.TaskFilter{}, foundation.PageRequest{Limit: 1, Cursor: token})
	requireCode(t, err, foundation.CursorStale)
	// Legal SQL text can still violate the stricter Unicode business codec.
	if _, err = f.raw.Exec(ctxFor(t), `UPDATE agenteam_work.tasks SET title=' ' WHERE id=$1`, target.ID.String()); err != nil {
		t.Fatal(err)
	}
	got, err := f.taskReader.GetTask(ctxFor(t), a, p, target.ID)
	requireCode(t, err, foundation.InternalError)
	if got.ID.Validate() == nil {
		t.Fatal("corrupt Get returned nonzero")
	}
	out, err := f.taskReader.ListTasks(ctxFor(t), a, p, wc.TaskFilter{SprintID: &target.SprintID}, foundation.PageRequest{Limit: 200})
	requireCode(t, err, foundation.InternalError)
	if len(out.Items) != 0 {
		t.Fatal("corrupt List leaked partial results")
	}
	if _, err = f.raw.Exec(ctxFor(t), `UPDATE agenteam_work.tasks SET title=$2 WHERE id=$1`, target.ID.String(), target.Title); err != nil {
		t.Fatal(err)
	}
	var generation int64
	if err = f.raw.QueryRow(ctxFor(t), `DELETE FROM agenteam_work.task_query_generations WHERE project_id=$1 RETURNING query_generation`, p.String()).Scan(&generation); err != nil {
		t.Fatal(err)
	}
	out, err = f.taskReader.ListTasks(ctxFor(t), a, p, wc.TaskFilter{}, foundation.PageRequest{Limit: 1})
	requireCode(t, err, foundation.InternalError)
	if len(out.Items) != 0 {
		t.Fatal("missing generation pretended empty")
	}
	if _, err = f.raw.Exec(ctxFor(t), `INSERT INTO agenteam_work.task_query_generations(project_id,query_generation) VALUES($1,$2)`, p.String(), generation); err != nil {
		t.Fatal(err)
	}
}
