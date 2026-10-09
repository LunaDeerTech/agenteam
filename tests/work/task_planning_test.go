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
	"math"
	"sort"
	"strings"
	"sync/atomic"
	"testing"
	"testing/fstest"
	"time"

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
	"github.com/jackc/pgx/v5/pgconn"
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
	taskAuthorityMatrix(t, f, a, other)
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
	store fixtureStore
	seen  bool
}

func (a *taskFailAfterHistory) AppendEventInTx(ctx context.Context, tx foundation.Tx, actor identity.Actor, e event.Event, p oc.AppendPlan) (oc.AppendReceipt, error) {
	x, err := a.store.InTx(tx)
	if err != nil {
		return oc.AppendReceipt{}, err
	}
	var payload wc.TaskChanged
	if err = json.Unmarshal(e.PayloadBytes(), &payload); err != nil {
		return oc.AppendReceipt{}, err
	}
	var found bool
	err = x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_work.task_events h JOIN agenteam_work.tasks t ON (t.project_id,t.id)=(h.project_id,h.task_id) WHERE h.id=$1 AND h.operation_id=$2 AND h.correlation_id=h.operation_id AND h.task_version=$3 AND t.version=h.task_version AND h.created_at=$4 AND t.updated_at=h.created_at)`, payload.TaskEventID.String(), payload.CommandID.String(), int64(*e.Header().AggregateVersion), e.Header().OccurredAt.Time()).Scan(&found)
	if err != nil {
		return oc.AppendReceipt{}, err
	}
	if !found {
		return oc.AppendReceipt{}, foundation.NewFault(foundation.InternalError, foundation.NotStarted)
	}
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
			history := &taskFailAfterHistory{Appender: f.events, store: f.store}
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
	taskAtomicityMatrix(t, f, a, p.ID, s.ID, first.ID, captured)
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
func taskPagingOriginalRankCollision(t *testing.T, f *taskFixture, a identity.Actor, p wc.ProjectID, first, moved wc.Task) {
	t.Helper()
	original, err := f.taskReader.GetTask(ctxFor(t), a, p, first.ID)
	if err != nil {
		t.Fatal("read original geometry target", err)
	}
	before, err := f.taskReader.GetTask(ctxFor(t), a, p, moved.ID)
	if err != nil {
		t.Fatal("read original geometry source", err)
	}
	if original.SprintID != before.SprintID || original.ManualRank != before.ManualRank {
		t.Fatal("paging geometry collision precondition changed")
	}
	schedule, _ := foundation.ProjectScheduleLock(p.String())
	// SQLExecutor forbids savepoints and poisons failed transactions. Use a
	// separate real transaction so the expected SQL error is fully rolled back.
	result := f.store.WithinTx(ctxFor(t), cause(t), func(ctx context.Context, tx foundation.Tx) error {
		if err := f.store.AcquireAll(ctx, tx, fixtureLocks(a, p, foundation.LockRequest{Key: schedule, Mode: foundation.Exclusive})); err != nil {
			return err
		}
		if _, err := f.projectAuthority.RequireOwnerInTx(ctx, tx, a, p, identity.Mutate); err != nil {
			return err
		}
		x, err := f.store.InTx(tx)
		if err != nil {
			return err
		}
		if _, err := x.Exec(ctx, `UPDATE agenteam_work.tasks SET state='backlog',priority='critical' WHERE id=$1`, before.ID.String()); err != nil {
			return err
		}
		if _, err := x.Exec(ctx, `SET CONSTRAINTS ALL IMMEDIATE`); err != nil {
			return err
		}
		return errors.New("original paging geometry unexpectedly satisfied rank uniqueness")
	})
	var pg *pgconn.PgError
	if errors.As(result.Fault(), &pg) {
		t.Logf("original paging geometry: SQLSTATE=%s constraint=%s", pg.Code, pg.ConstraintName)
	}
	if result.State() != foundation.NotCommitted || pg == nil || pg.Code != "23505" || pg.ConstraintName != "tasks_group_rank_key" {
		t.Fatal("original geometry did not prove the expected rolled-back rank collision", result.State(), result.Fault())
	}
	after, err := f.taskReader.GetTask(ctxFor(t), a, p, before.ID)
	if err != nil || !bytes.Equal(jsonBytes(t, before), jsonBytes(t, after)) {
		t.Fatal("original geometry probe did not preserve source canonical", err)
	}
}
func taskPagingMatrix(t *testing.T, f *taskFixture, a identity.Actor, p wc.ProjectID, tasks []wc.Task) {
	t.Helper()
	taskPagingOriginalRankCollision(t, f, a, p, tasks[0], tasks[1])
	agentA, agentB := id[identity.Agent](t), id[identity.Agent](t)
	states := []wc.TaskState{wc.TaskStateBacklog, wc.TaskStateTodo, wc.TaskStateInProgress, wc.TaskStateInReview, wc.TaskStateBlocked, wc.TaskStateDone, wc.TaskStateCancelled}
	priorities := []wc.TaskPriority{wc.TaskPriorityCritical, wc.TaskPriorityHigh, wc.TaskPriorityMedium, wc.TaskPriorityLow}
	schedule, _ := foundation.ProjectScheduleLock(p.String())
	f.tx(t, fixtureLocks(a, p, foundation.LockRequest{Key: schedule, Mode: foundation.Exclusive}), func(ctx context.Context, tx foundation.Tx, x postgres.SQLExecutor) error {
		if _, err := f.projectAuthority.RequireOwnerInTx(ctx, tx, a, p, identity.Mutate); err != nil {
			return err
		}
		diagnose := func(stage string, err error) error {
			if err != nil {
				var pg *pgconn.PgError
				if errors.As(err, &pg) {
					t.Logf("paging geometry %s: SQLSTATE=%s constraint=%s", stage, pg.Code, pg.ConstraintName)
				} else {
					t.Logf("paging geometry %s failed without PostgreSQL error metadata", stage)
				}
			}
			return err
		}
		// Read current ranks instead of assuming every earlier create receipt
		// still carries the stored rank after possible rank maintenance.
		rows, err := x.Query(ctx, `SELECT id::text,manual_rank FROM agenteam_work.tasks WHERE project_id=$1`, p.String())
		if err != nil {
			return diagnose("read ranks", err)
		}
		ranks := make(map[string]string, len(tasks))
		for rows.Next() {
			var taskID, rank string
			if err := rows.Scan(&taskID, &rank); err != nil {
				rows.Close()
				return diagnose("scan rank", err)
			}
			ranks[taskID] = rank
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return diagnose("read ranks complete", err)
		}
		if len(ranks) != len(tasks) {
			return errors.New("paging geometry rank membership mismatch")
		}
		for n := range tasks {
			rank, ok := ranks[tasks[n].ID.String()]
			if !ok {
				return errors.New("paging geometry missing stored rank")
			}
			tasks[n].ManualRank = rank
		}
		for n := 0; n < 28; n++ {
			task := &tasks[n+1]
			task.State = states[n/4]
			task.Priority = priorities[n%4]
			// The real create ranks are all at least the first midpoint. These
			// distinct positive low ranks keep every seeded target group legal.
			task.ManualRank = fmt.Sprintf("%032x", n+1)
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
			if _, err := x.Exec(ctx, `UPDATE agenteam_work.tasks SET state=$2,priority=$3,assignee_agent_id=$4,manual_rank=$5 WHERE id=$1`, task.ID.String(), string(task.State), string(task.Priority), agent, task.ManualRank); err != nil {
				return diagnose("seed member", err)
			}
		}
		if _, err := x.Exec(ctx, `INSERT INTO agenteam_work.task_order_groups(project_id,milestone_id,sprint_id,state,priority,order_generation) SELECT DISTINCT project_id,milestone_id,sprint_id,state,priority,1 FROM agenteam_work.tasks WHERE project_id=$1 ON CONFLICT DO NOTHING`, p.String()); err != nil {
			return diagnose("seed groups", err)
		}
		if _, err := x.Exec(ctx, `UPDATE agenteam_work.task_query_generations SET query_generation=query_generation+1 WHERE project_id=$1`, p.String()); err != nil {
			return diagnose("advance query generation", err)
		}
		_, err = x.Exec(ctx, `SET CONSTRAINTS ALL IMMEDIATE`)
		return diagnose("validate seeded ranks", err)
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

func taskFaultReason(t *testing.T, err error, code foundation.Code, reason string) {
	t.Helper()
	requireCode(t, err, code)
	var fault *foundation.Fault
	if !errors.As(err, &fault) || len(fault.FieldErrors) != 1 || fault.FieldErrors[0].Code != reason {
		t.Fatal("missing exact safe reason", reason, err)
	}
}
func taskAuthorityMatrix(t *testing.T, f *taskFixture, a, other identity.Actor) {
	t.Helper()
	t.Run("command-scope-writer-presence", func(t *testing.T) {
		p, _, _ := f.create(t, a, "task-key-scope")
		m := f.milestone(t, a, p.ID, "m")
		s := f.sprint(t, a, p.ID, m.ID, "s")
		request := wc.TaskCreate{TaskID: id[wc.Task](t), SprintID: s.ID, Title: "original", Type: wc.TaskTypeFeature, Priority: wc.TaskPriorityLow}
		metadata := meta(t, "same-key", nil)
		created, err := f.tasks.CreateTask(ctxFor(t), a, metadata, p.ID, request)
		if err != nil {
			t.Fatal(err)
		}
		title := "updated"
		um := meta(t, "same-key", &created.Task.Version)
		updated, err := f.tasks.UpdateTask(ctxFor(t), a, um, p.ID, created.Task.ID, wc.TaskFieldsUpdate{Title: &title})
		if err != nil {
			t.Fatal("same key different command", err)
		}
		_, err = f.tasks.ReorderTask(ctxFor(t), a, meta(t, "same-key", &updated.Task.Version), p.ID, created.Task.ID, wc.TaskReorder{})
		if err != nil {
			t.Fatal("third command key namespace", err)
		}
		var commands int
		if err = f.raw.QueryRow(ctxFor(t), `SELECT count(DISTINCT command_name) FROM agenteam_work.task_commands WHERE project_id=$1 AND idempotency_key='same-key'`, p.ID.String()).Scan(&commands); err != nil || commands != 3 {
			t.Fatal("command namespace conflated", commands, err)
		}
		empty := ""
		_, err = f.tasks.UpdateTask(ctxFor(t), a, um, p.ID, created.Task.ID, wc.TaskFieldsUpdate{Title: &title, Description: &empty})
		requireCode(t, err, foundation.IdempotencyKeyReused)
		differentVersion := updated.Task.Version
		changedMeta := um
		changedMeta.ExpectedVersion = &differentVersion
		_, err = f.tasks.UpdateTask(ctxFor(t), a, changedMeta, p.ID, created.Task.ID, wc.TaskFieldsUpdate{Title: &title})
		requireCode(t, err, foundation.IdempotencyKeyReused)
		p2, _, _ := f.create(t, a, "task-key-project")
		m2 := f.milestone(t, a, p2.ID, "m")
		s2 := f.sprint(t, a, p2.ID, m2.ID, "s")
		r2 := request
		r2.TaskID = id[wc.Task](t)
		r2.SprintID = s2.ID
		if _, err = f.tasks.CreateTask(ctxFor(t), a, metadata, p2.ID, r2); err != nil {
			t.Fatal("same key other Project", err)
		}
		r2.TaskID = request.TaskID
		_, err = f.tasks.CreateTask(ctxFor(t), a, meta(t, "occupied", nil), p2.ID, r2)
		taskFaultReason(t, err, foundation.ResourceBusy, "TARGET_OCCUPIED")
		r2.TaskID = id[wc.Task](t)
		r2.SprintID = s.ID
		_, err = f.tasks.CreateTask(ctxFor(t), a, meta(t, "foreign-sprint", nil), p2.ID, r2)
		requireCode(t, err, foundation.TaskSprintInvalid)
		r2.SprintID = id[c.Sprint](t)
		_, err = f.tasks.CreateTask(ctxFor(t), a, meta(t, "missing-sprint", nil), p2.ID, r2)
		requireCode(t, err, foundation.TaskSprintInvalid)
		_, err = f.taskReader.GetTask(ctxFor(t), a, p2.ID, request.TaskID)
		requireCode(t, err, foundation.TaskNotFound)
		digest, err := wc.TaskCreateDigest(a, metadata, p.ID, request)
		if err != nil {
			t.Fatal(err)
		}
		lookup := wc.TaskCommandLookupRequest{ProjectID: p.ID, Command: wc.TaskCommandCreate, IdempotencyKey: metadata.IdempotencyKey, SemanticDigest: digest}
		f.transferOwner(t, a, other, p.ID)
		_, err = f.tasks.CreateTask(ctxFor(t), other, metadata, p.ID, request)
		requireCode(t, err, foundation.NotFound)
		_, err = f.tasks.LookupTaskCommand(ctxFor(t), other, lookup)
		requireCode(t, err, foundation.NotFound)
		f.transferOwner(t, other, a, p.ID)
		f.seedTaskState(t, a, updated.Task, wc.TaskStateDone, nil)
		wrong := foundation.Version(1)
		_, err = f.tasks.UpdateTask(ctxFor(t), a, meta(t, "version-before-terminal", &wrong), p.ID, created.Task.ID, wc.TaskFieldsUpdate{Title: &title})
		requireCode(t, err, foundation.TaskVersionConflict)
		replay, err := f.tasks.CreateTask(ctxFor(t), a, metadata, p.ID, request)
		if err != nil {
			t.Fatal("history before future state", err)
		}
		equalTaskMutation(t, created, replay)
	})
	t.Run("current-and-completed-sprint", func(t *testing.T) {
		p, _, _ := f.create(t, a, "task-current")
		m := f.milestone(t, a, p.ID, "m")
		s := f.sprint(t, a, p.ID, m.ID, "s")
		f.lifecycle(t, a, p.ID, s, false)
		req := wc.TaskCreate{TaskID: id[wc.Task](t), SprintID: s.ID, Title: "current", Type: wc.TaskTypeChore, Priority: wc.TaskPriorityLow}
		metadata := meta(t, "current", nil)
		created, err := f.tasks.CreateTask(ctxFor(t), a, metadata, p.ID, req)
		if err != nil {
			t.Fatal("current Sprint create", err)
		}
		title := "current update"
		updated, err := f.tasks.UpdateTask(ctxFor(t), a, meta(t, "current-update", &created.Task.Version), p.ID, created.Task.ID, wc.TaskFieldsUpdate{Title: &title})
		if err != nil {
			t.Fatal("current Sprint update", err)
		}
		if _, err = f.tasks.ReorderTask(ctxFor(t), a, meta(t, "current-reorder", &updated.Task.Version), p.ID, updated.Task.ID, wc.TaskReorder{}); err != nil {
			t.Fatal(err)
		}
		f.lifecycle(t, a, p.ID, s, true)
		replay, err := f.tasks.CreateTask(ctxFor(t), a, metadata, p.ID, req)
		if err != nil {
			t.Fatal(err)
		}
		equalTaskMutation(t, created, replay)
		req.TaskID = id[wc.Task](t)
		req.InitialState = wc.TaskStateTodo
		_, err = f.tasks.CreateTask(ctxFor(t), a, meta(t, "completed-before-assignee", nil), p.ID, req)
		requireCode(t, err, foundation.TaskSprintInvalid)
	})
	for _, state := range []c.Lifecycle{c.Archiving, c.Archived, c.Deleting} {
		t.Run("history-planned-"+string(state), func(t *testing.T) {
			p, _, _ := f.create(t, a, "task-"+string(state))
			m := f.milestone(t, a, p.ID, "m")
			s := f.sprint(t, a, p.ID, m.ID, "s")
			req := wc.TaskCreate{TaskID: id[wc.Task](t), SprintID: s.ID, Title: "history", Type: wc.TaskTypeTask, Priority: wc.TaskPriorityHigh}
			metadata := meta(t, "completed", nil)
			completed, err := f.tasks.CreateTask(ctxFor(t), a, metadata, p.ID, req)
			if err != nil {
				t.Fatal(err)
			}
			pending := req
			pending.TaskID = id[wc.Task](t)
			pm := meta(t, "planned", nil)
			app := &capturingAppender{Appender: f.events, before: func(context.Context, identity.Actor, event.Event) error {
				return foundation.NewFault(foundation.DependencyUnavailable, foundation.NotStarted)
			}}
			writer := f.newTaskService(t, app, f.accounts)
			_, err = writer.CreateTask(ctxFor(t), a, pm, p.ID, pending)
			requireCode(t, err, foundation.DependencyUnavailable)
			pd, err := wc.TaskCreateDigest(a, pm, p.ID, pending)
			if err != nil {
				t.Fatal(err)
			}
			q := wc.TaskCommandLookupRequest{ProjectID: p.ID, Command: wc.TaskCommandCreate, IdempotencyKey: pm.IdempotencyKey, SemanticDigest: pd}
			observed, err := f.tasks.LookupTaskCommand(ctxFor(t), a, q)
			if err != nil || observed.Status != wc.LookupInProgress {
				t.Fatal("planned premise", err)
			}
			if state == c.Archived {
				f.seedLifecycle(t, a, p.ID, state)
			} else {
				f.seedProjectTransition(t, a, p.ID, state)
			}
			replay, err := f.tasks.CreateTask(ctxFor(t), a, metadata, p.ID, req)
			if state == c.Deleting {
				requireCode(t, err, foundation.ProjectNotActive)
			} else {
				if err != nil {
					t.Fatal("history Read boundary", err)
				}
				equalTaskMutation(t, completed, replay)
			}
			_, err = f.tasks.CreateTask(ctxFor(t), a, pm, p.ID, pending)
			requireCode(t, err, foundation.ProjectNotActive)
			observed, err = f.tasks.LookupTaskCommand(ctxFor(t), a, q)
			if state == c.Deleting {
				requireCode(t, err, foundation.ProjectNotActive)
			} else if err != nil || observed.Status != wc.LookupInProgress {
				t.Fatal("planned readonly Lookup", err)
			}
			_, err = f.taskReader.GetTask(ctxFor(t), a, p.ID, completed.Task.ID)
			if state == c.Deleting {
				requireCode(t, err, foundation.ProjectNotActive)
			} else if err != nil {
				t.Fatal("archived current Get", err)
			}
		})
	}
	t.Run("uninitialized", func(t *testing.T) {
		f.skills.setMode("pending")
		defer f.skills.setMode("")
		req := c.CreateProjectRequest{ProjectID: id[identity.Project](t), Name: "task-uninitialized"}
		result, err := f.projects.CreateProject(ctxFor(t), a, meta(t, "task-pending-project", nil), req)
		if err != nil || result.State == c.CreationReady {
			t.Fatal("uninitialized fixture", err)
		}
		_, err = f.tasks.CreateTask(ctxFor(t), a, meta(t, "uninitialized-task", nil), req.ProjectID, wc.TaskCreate{TaskID: id[wc.Task](t), SprintID: id[c.Sprint](t), Title: "uninitialized", Type: wc.TaskTypeBug, Priority: wc.TaskPriorityLow})
		requireCode(t, err, foundation.ProjectNotActive)
	})

	t.Run("future-state-capabilities-and-anchors", func(t *testing.T) {
		p, _, _ := f.create(t, a, "task-capabilities")
		m := f.milestone(t, a, p.ID, "m")
		s := f.sprint(t, a, p.ID, m.ID, "s")
		request := wc.TaskCreate{TaskID: id[wc.Task](t), SprintID: s.ID, Title: "original", Type: wc.TaskTypeTask, Priority: wc.TaskPriorityMedium}
		metadata := meta(t, "capability-original", nil)
		created, err := f.tasks.CreateTask(ctxFor(t), a, metadata, p.ID, request)
		if err != nil {
			t.Fatal(err)
		}
		title := "must reject"
		agent := id[identity.Agent](t)
		for _, state := range []wc.TaskState{wc.TaskStateTodo, wc.TaskStateInProgress, wc.TaskStateInReview, wc.TaskStateBlocked, wc.TaskStateBacklog, wc.TaskStateDone, wc.TaskStateCancelled} {
			assigned := &agent
			if state == wc.TaskStateDone || state == wc.TaskStateCancelled {
				assigned = nil
			}
			f.seedTaskState(t, a, created.Task, state, assigned)
			code := foundation.DependencyUnbound
			if state == wc.TaskStateDone || state == wc.TaskStateCancelled {
				code = foundation.TaskTerminalImmutable
			}
			_, err = f.tasks.UpdateTask(ctxFor(t), a, meta(t, "state-update-"+string(state), &created.Task.Version), p.ID, created.Task.ID, wc.TaskFieldsUpdate{Title: &title})
			requireCode(t, err, code)
			_, err = f.tasks.ReorderTask(ctxFor(t), a, meta(t, "state-reorder-"+string(state), &created.Task.Version), p.ID, created.Task.ID, wc.TaskReorder{})
			requireCode(t, err, code)
			replay, err := f.tasks.CreateTask(ctxFor(t), a, metadata, p.ID, request)
			if err != nil {
				t.Fatal("history preceded capability", state, err)
			}
			equalTaskMutation(t, created, replay)
		}
		f.seedTaskState(t, a, created.Task, wc.TaskStateBacklog, nil)
		missing := id[wc.Task](t)
		_, err = f.tasks.UpdateTask(ctxFor(t), a, meta(t, "new-key-missing", &created.Task.Version), p.ID, missing, wc.TaskFieldsUpdate{Title: &title})
		requireCode(t, err, foundation.TaskNotFound)
		low := request
		low.TaskID = id[wc.Task](t)
		low.Priority = wc.TaskPriorityLow
		anchor, err := f.tasks.CreateTask(ctxFor(t), a, meta(t, "foreign-group-anchor", nil), p.ID, low)
		if err != nil {
			t.Fatal(err)
		}
		_, err = f.tasks.ReorderTask(ctxFor(t), a, meta(t, "cross-group-anchor", &created.Task.Version), p.ID, created.Task.ID, wc.TaskReorder{BeforeID: &anchor.Task.ID})
		requireCode(t, err, foundation.TaskNotFound)
		_, err = f.tasks.ReorderTask(ctxFor(t), a, meta(t, "self-anchor", &created.Task.Version), p.ID, created.Task.ID, wc.TaskReorder{BeforeID: &created.Task.ID})
		requireCode(t, err, foundation.InvalidArgument)
		for _, state := range []wc.TaskState{wc.TaskStateInProgress, wc.TaskStateInReview, wc.TaskStateBlocked, wc.TaskStateCancelled} {
			bad := request
			bad.TaskID = id[wc.Task](t)
			bad.InitialState = state
			_, err = f.tasks.CreateTask(ctxFor(t), a, meta(t, "invalid-initial-"+string(state), nil), p.ID, bad)
			requireCode(t, err, foundation.TaskStateInvalid)
		}
	})
	t.Run("capacity-and-counter-boundaries", func(t *testing.T) { taskCapacityAndCounters(t, f, a) })
}
func taskCapacityAndCounters(t *testing.T, f *taskFixture, a identity.Actor) {
	t.Helper()
	p, _, _ := f.create(t, a, "task-capacity")
	m := f.milestone(t, a, p.ID, "m")
	s := f.sprint(t, a, p.ID, m.ID, "full")
	empty := f.sprint(t, a, p.ID, m.ID, "empty")
	req := wc.TaskCreate{TaskID: id[wc.Task](t), SprintID: s.ID, Title: "capacity original", Type: wc.TaskTypeTask, Priority: wc.TaskPriorityCritical}
	metadata := meta(t, "capacity-original", nil)
	original, err := f.tasks.CreateTask(ctxFor(t), a, metadata, p.ID, req)
	if err != nil {
		t.Fatal(err)
	}
	medium := f.task(t, a, p.ID, s.ID, "medium original")
	schedule, _ := foundation.ProjectScheduleLock(p.ID.String())
	agent := id[identity.Agent](t)
	seed := func(from, to int) {
		f.tx(t, fixtureLocks(a, p.ID, foundation.LockRequest{Key: schedule, Mode: foundation.Exclusive}), func(ctx context.Context, tx foundation.Tx, x postgres.SQLExecutor) error {
			if _, err := f.projectAuthority.RequireOwnerInTx(ctx, tx, a, p.ID, identity.Mutate); err != nil {
				return err
			}
			_, err := x.Exec(ctx, `INSERT INTO agenteam_work.tasks(id,project_id,milestone_id,sprint_id,title,description,type,priority,state,assignee_agent_id,plan,manual_rank,version,created_at,updated_at)
 SELECT ('01900000-0000-7000-8001-'||lpad(to_hex(n),12,'0'))::uuid,$1,$2,$3,'capacity','','task',(ARRAY['critical','high','medium','low'])[((n-1)/4096)%4+1],(ARRAY['backlog','todo','in_progress','in_review'])[(n-1)/16384+1],CASE WHEN n>16384 THEN $6::uuid ELSE NULL END,'',lpad(to_hex((n-1)%4096+1),32,'0'),1,clock_timestamp(),clock_timestamp() FROM generate_series($4::int,$5::int) n WHERE n<>1 AND n<>8193`, p.ID.String(), m.ID.String(), s.ID.String(), from, to, agent.String())
			if err != nil {
				return err
			}
			if _, err = x.Exec(ctx, `INSERT INTO agenteam_work.task_order_groups(project_id,milestone_id,sprint_id,state,priority,order_generation) SELECT DISTINCT project_id,milestone_id,sprint_id,state,priority,1 FROM agenteam_work.tasks WHERE project_id=$1 ON CONFLICT(project_id,sprint_id,state,priority) DO UPDATE SET order_generation=agenteam_work.task_order_groups.order_generation+1`, p.ID.String()); err != nil {
				return err
			}
			_, err = x.Exec(ctx, `UPDATE agenteam_work.task_query_generations SET query_generation=query_generation+1 WHERE project_id=$1`, p.ID.String())
			return err
		})
	}
	seed(2, 4096)
	var count int
	if err = f.raw.QueryRow(ctxFor(t), `SELECT count(*) FROM agenteam_work.tasks WHERE project_id=$1 AND state='backlog' AND priority='critical'`, p.ID.String()).Scan(&count); err != nil || count != 4096 {
		t.Fatal("exact full group fixture", count, err)
	}
	replay, err := f.tasks.CreateTask(ctxFor(t), a, metadata, p.ID, req)
	if err != nil {
		t.Fatal(err)
	}
	equalTaskMutation(t, original, replay)
	over := req
	over.TaskID = id[wc.Task](t)
	_, err = f.tasks.CreateTask(ctxFor(t), a, meta(t, "group-overflow", nil), p.ID, over)
	taskFaultReason(t, err, foundation.ResourceBusy, "GROUP_LIMIT")
	priority := wc.TaskPriorityCritical
	_, err = f.tasks.UpdateTask(ctxFor(t), a, meta(t, "priority-overflow", &medium.Version), p.ID, medium.ID, wc.TaskFieldsUpdate{Priority: &priority})
	taskFaultReason(t, err, foundation.ResourceBusy, "GROUP_LIMIT")
	seed(4097, 65536)
	if err = f.raw.QueryRow(ctxFor(t), `SELECT count(*) FROM agenteam_work.tasks WHERE project_id=$1`, p.ID.String()).Scan(&count); err != nil || count != 65536 {
		t.Fatal("exact full Project fixture", count, err)
	}
	var wrongGroups int
	if err = f.raw.QueryRow(ctxFor(t), `SELECT count(*) FROM (SELECT count(*) AS n FROM agenteam_work.tasks WHERE project_id=$1 GROUP BY sprint_id,state,priority HAVING count(*)<>4096) g`, p.ID.String()).Scan(&wrongGroups); err != nil || wrongGroups != 0 {
		t.Fatal("capacity fixture group limits", err)
	}
	replay, err = f.tasks.CreateTask(ctxFor(t), a, metadata, p.ID, req)
	if err != nil {
		t.Fatal("Project capacity displaced history", err)
	}
	equalTaskMutation(t, original, replay)
	over.TaskID = id[wc.Task](t)
	over.SprintID = empty.ID
	_, err = f.tasks.CreateTask(ctxFor(t), a, meta(t, "project-overflow", nil), p.ID, over)
	taskFaultReason(t, err, foundation.ResourceBusy, "PROJECT_TASK_LIMIT")
	t.Log("test-owned 65536 canonical Tasks, 16 exact groups and real group/query rows; seeded Agent IDs prove no assignment capability")
	p2, _, _ := f.create(t, a, "task-counters")
	m2 := f.milestone(t, a, p2.ID, "m")
	s2 := f.sprint(t, a, p2.ID, m2.ID, "s")
	target := f.task(t, a, p2.ID, s2.ID, "counter")
	other := f.task(t, a, p2.ID, s2.ID, "other")
	title := "changed"
	for _, kind := range []string{"version", "group", "query"} {
		t.Run(kind, func(t *testing.T) {
			var sql, restore string
			var key string
			expected := target.Version
			switch kind {
			case "version":
				sql = `UPDATE agenteam_work.tasks SET version=$2 WHERE id=$1`
				restore = sql
				key = target.ID.String()
				expected = foundation.Version(math.MaxInt64)
			case "group":
				sql = `UPDATE agenteam_work.task_order_groups SET order_generation=$2 WHERE project_id=$1`
				restore = sql
				key = p2.ID.String()
			case "query":
				sql = `UPDATE agenteam_work.task_query_generations SET query_generation=$2 WHERE project_id=$1`
				restore = sql
				key = p2.ID.String()
			}
			var old int64
			switch kind {
			case "version":
				old = int64(target.Version)
			case "group":
				old, _ = f.generation(t, p2.ID, s2.ID, wc.TaskPriorityMedium)
			case "query":
				_, old = f.generation(t, p2.ID, s2.ID, wc.TaskPriorityMedium)
			}
			if _, err = f.raw.Exec(ctxFor(t), sql, key, int64(math.MaxInt64)); err != nil {
				t.Fatal(err)
			}
			if kind == "group" {
				_, err = f.tasks.ReorderTask(ctxFor(t), a, meta(t, "counter-group", &other.Version), p2.ID, other.ID, wc.TaskReorder{BeforeID: &target.ID})
			} else {
				_, err = f.tasks.UpdateTask(ctxFor(t), a, meta(t, "counter-"+kind, &expected), p2.ID, target.ID, wc.TaskFieldsUpdate{Title: &title})
			}
			taskFaultReason(t, err, foundation.InvalidState, "COUNTER_EXHAUSTED")
			if _, err = f.raw.Exec(ctxFor(t), restore, key, old); err != nil {
				t.Fatal(err)
			}
		})
	}
}

// A transparent final-transaction hook: every assertion still invokes the real
// Outbox producer after the persisted Task and TaskEvent have been written.
type taskProbeAppender struct {
	oc.Appender
	check func(context.Context, foundation.Tx, event.Event, oc.AppendPlan) error
	seen  bool
}

func (a *taskProbeAppender) AppendEventInTx(ctx context.Context, tx foundation.Tx, actor identity.Actor, e event.Event, p oc.AppendPlan) (oc.AppendReceipt, error) {
	a.seen = true
	if err := a.check(ctx, tx, e, p); err != nil {
		return oc.AppendReceipt{}, err
	}
	return a.Appender.AppendEventInTx(ctx, tx, actor, e, p)
}
func taskAtomicityMatrix(t *testing.T, f *taskFixture, a identity.Actor, p wc.ProjectID, s wc.SprintID, target wc.TaskID, saved capturedEvent) {
	t.Helper()
	var auditBefore int
	if err := f.raw.QueryRow(ctxFor(t), `SELECT count(*) FROM agenteam_audit.audit_records`).Scan(&auditBefore); err != nil {
		t.Fatal(err)
	}
	t.Run("created-and-priority-payloads", func(t *testing.T) {
		var raw []byte
		var historyID string
		if err := f.raw.QueryRow(ctxFor(t), `SELECT id::text,payload FROM agenteam_work.task_events WHERE task_id=$1 AND type='task_created'`, target.String()).Scan(&historyID, &raw); err != nil {
			t.Fatal(err)
		}
		var created wc.TaskCreatedPayload
		if err := json.Unmarshal(raw, &created); err != nil || created.InitialState != wc.TaskStateBacklog || created.Type != wc.TaskTypeTask || created.Priority != wc.TaskPriorityMedium || created.SprintID != s {
			t.Fatal("real creation history", err)
		}
		var headerRaw, payloadRaw []byte
		if err := f.raw.QueryRow(ctxFor(t), `SELECT header,payload FROM agenteam_outbox.events WHERE aggregate_id=$1 AND event_type='work.task_changed' AND aggregate_version=1`, target.String()).Scan(&headerRaw, &payloadRaw); err != nil {
			t.Fatal(err)
		}
		var header event.Header
		if err := json.Unmarshal(headerRaw, &header); err != nil {
			t.Fatal(err)
		}
		ev, err := f.taskEvents.Restore(header, payloadRaw)
		if err != nil {
			t.Fatal(err)
		}
		body, err := f.taskEvents.DecodeTaskChanged(ev)
		if err != nil || body.Change != wc.TaskCreatedChange || body.TaskEventID.String() != historyID || body.Position == nil || body.Position.NextID != nil {
			t.Fatal("created typed payload", err)
		}
		current, err := f.taskReader.GetTask(ctxFor(t), a, p, target)
		if err != nil {
			t.Fatal(err)
		}
		priority, typ, plan := wc.TaskPriorityHigh, wc.TaskTypeBug, "new plan"
		capture := &capturingAppender{Appender: f.events}
		writer := f.newTaskService(t, capture, f.accounts)
		updated, err := writer.UpdateTask(ctxFor(t), a, meta(t, "priority-and-type", &current.Version), p, target, wc.TaskFieldsUpdate{Priority: &priority, Type: &typ, Plan: &plan})
		if err != nil {
			t.Fatal(err)
		}
		if err = f.raw.QueryRow(ctxFor(t), `SELECT payload FROM agenteam_work.task_events WHERE id=$1`, updated.TaskEventID.String()).Scan(&raw); err != nil {
			t.Fatal(err)
		}
		var fields wc.TaskFieldsUpdatedPayload
		if err = json.Unmarshal(raw, &fields); err != nil || fields.TypeChange == nil || fields.TypeChange.From != wc.TaskTypeTask || fields.TypeChange.To != typ || fields.PriorityChange == nil || fields.PriorityChange.From != wc.TaskPriorityMedium || fields.PriorityChange.To != priority || fields.Position == nil || fields.Position.Priority != priority || fields.Position.NextID != nil || !bytes.Equal(jsonBytes(t, fields.ChangedFields), []byte(`["plan","priority","type"]`)) {
			t.Fatal("priority/type safe history", err)
		}
		last := capture.last(t)
		body, err = f.taskEvents.DecodeTaskChanged(last.Event)
		if err != nil || body.Change != wc.TaskUpdatedChange || body.TaskEventID != *updated.TaskEventID || body.Position == nil || *last.Event.Header().AggregateVersion != updated.Task.Version || last.Event.Header().OccurredAt != updated.Task.UpdatedAt {
			t.Fatal("typed update Header/history alignment", err)
		}
		// A fresh no-op touches Activity exactly once; historical replay is read-only.
		f.ageSession(t, a)
		beforeG, beforeQ := f.generation(t, p, s, priority)
		var activityBefore, activityAfter time.Time
		if err = f.raw.QueryRow(ctxFor(t), `SELECT last_activity_at FROM agenteam_account.sessions WHERE id=$1`, a.Details().SessionID).Scan(&activityBefore); err != nil {
			t.Fatal(err)
		}
		metadata := meta(t, "real-noop-activity", &updated.Task.Version)
		noop, err := writer.UpdateTask(ctxFor(t), a, metadata, p, target, wc.TaskFieldsUpdate{Plan: &plan})
		if err != nil || noop.Changed {
			t.Fatal(err)
		}
		if err = f.raw.QueryRow(ctxFor(t), `SELECT last_activity_at FROM agenteam_account.sessions WHERE id=$1`, a.Details().SessionID).Scan(&activityAfter); err != nil || !activityAfter.After(activityBefore) {
			t.Fatal("new noop omitted Activity", err)
		}
		afterG, afterQ := f.generation(t, p, s, priority)
		if beforeG != afterG || beforeQ != afterQ {
			t.Fatal("noop changed generations")
		}
		before := f.taskSnapshot(t, a)
		replay, err := writer.UpdateTask(ctxFor(t), a, metadata, p, target, wc.TaskFieldsUpdate{Plan: &plan})
		if err != nil {
			t.Fatal(err)
		}
		equalTaskMutation(t, noop, replay)
		if before != f.taskSnapshot(t, a) {
			t.Fatal("historical noop touched facts")
		}
	})
	t.Run("durable-history-and-postimage-are-mandatory", func(t *testing.T) {
		for _, kind := range []string{"missing-history", "actor", "operation", "version", "time", "payload", "canonical", "query", "revision", "writer"} {
			t.Run(kind, func(t *testing.T) {
				current, err := f.taskReader.GetTask(ctxFor(t), a, p, target)
				if err != nil {
					t.Fatal(err)
				}
				before := f.taskSnapshot(t, a)
				app := &taskProbeAppender{Appender: f.events}
				app.check = func(ctx context.Context, tx foundation.Tx, e event.Event, _ oc.AppendPlan) error {
					x, err := f.store.InTx(tx)
					if err != nil {
						return err
					}
					var payload wc.TaskChanged
					if err = json.Unmarshal(e.PayloadBytes(), &payload); err != nil {
						return err
					}
					var sql string
					var args []any
					switch kind {
					case "missing-history":
						sql = `DELETE FROM agenteam_work.task_events WHERE id=$1`
						args = []any{payload.TaskEventID.String()}
					case "actor":
						sql = `UPDATE agenteam_work.task_events SET actor=jsonb_set(actor,'{user_id}',to_jsonb($2::text)) WHERE id=$1`
						args = []any{payload.TaskEventID.String(), id[identity.User](t).String()}
					case "operation":
						sql = `UPDATE agenteam_work.task_events SET operation_id=(SELECT operation_id FROM agenteam_work.task_events WHERE id<>$1 ORDER BY id LIMIT 1),correlation_id=(SELECT operation_id FROM agenteam_work.task_events WHERE id<>$1 ORDER BY id LIMIT 1) WHERE id=$1`
						args = []any{payload.TaskEventID.String()}
					case "version":
						sql = `UPDATE agenteam_work.task_events SET task_version=task_version+1 WHERE id=$1`
						args = []any{payload.TaskEventID.String()}
					case "time":
						sql = `UPDATE agenteam_work.task_events SET created_at=created_at+interval '1 microsecond' WHERE id=$1`
						args = []any{payload.TaskEventID.String()}
					case "payload":
						sql = `UPDATE agenteam_work.task_events SET payload=jsonb_set(payload,'{changed_fields}','["description"]'::jsonb) WHERE id=$1`
						args = []any{payload.TaskEventID.String()}
					case "canonical":
						sql = `UPDATE agenteam_work.tasks SET plan='unexpected canonical value' WHERE id=$1`
						args = []any{target.String()}
					case "query":
						sql = `UPDATE agenteam_work.task_query_generations SET query_generation=query_generation+1 WHERE project_id=$1`
						args = []any{p.String()}
					case "revision":
						sql = `UPDATE agenteam_work.task_commands SET plan_revision=plan_revision+1 WHERE id=$1`
						args = []any{payload.CommandID.String()}
					case "writer":
						sql = `UPDATE agenteam_work.task_commands SET actor_user_id=$2 WHERE id=$1`
						args = []any{payload.CommandID.String(), id[identity.User](t).String()}
					}
					_, err = x.Exec(ctx, sql, args...)
					return err
				}
				writer := f.newTaskService(t, app, f.accounts)
				title := "denied " + kind
				result, err := writer.UpdateTask(ctxFor(t), a, meta(t, "tamper-"+kind, &current.Version), p, target, wc.TaskFieldsUpdate{Title: &title})
				if err == nil || !app.seen || result.Task.ID.Validate() == nil {
					t.Fatal("real producer admitted tampered final fact", kind, err)
				}
				if before != f.taskSnapshot(t, a) {
					t.Fatal("tampered final fact escaped rollback", kind)
				}
			})
		}
	})

	t.Run("both-priority-groups-and-spectator-rank", func(t *testing.T) {
		for _, kind := range []string{"source-generation", "target-generation", "spectator-rank"} {
			t.Run(kind, func(t *testing.T) {
				current, err := f.taskReader.GetTask(ctxFor(t), a, p, target)
				if err != nil {
					t.Fatal(err)
				}
				before := f.taskSnapshot(t, a)
				priority := wc.TaskPriorityMedium
				if current.Priority != wc.TaskPriorityHigh {
					t.Fatal("priority source premise")
				}
				app := &taskProbeAppender{Appender: f.events}
				app.check = func(ctx context.Context, tx foundation.Tx, _ event.Event, _ oc.AppendPlan) error {
					x, err := f.store.InTx(tx)
					if err != nil {
						return err
					}
					if kind == "spectator-rank" {
						tag, err := x.Exec(ctx, `UPDATE agenteam_work.tasks SET manual_rank='00000000000000000000000000000001' WHERE project_id=$1 AND sprint_id=$2 AND state='backlog' AND priority='medium' AND id<>$3`, p.String(), s.String(), target.String())
						if err != nil {
							return err
						}
						if tag.RowsAffected() < 1 {
							return foundation.NewFault(foundation.InternalError, foundation.NotStarted)
						}
						return nil
					}
					group := current.Priority
					if kind == "target-generation" {
						group = priority
					}
					_, err = x.Exec(ctx, `UPDATE agenteam_work.task_order_groups SET order_generation=order_generation+1 WHERE project_id=$1 AND sprint_id=$2 AND state='backlog' AND priority=$3`, p.String(), s.String(), string(group))
					return err
				}
				writer := f.newTaskService(t, app, f.accounts)
				_, err = writer.UpdateTask(ctxFor(t), a, meta(t, "group-proof-"+kind, &current.Version), p, target, wc.TaskFieldsUpdate{Priority: &priority})
				if err == nil || !app.seen {
					t.Fatal("unmatched group postimage accepted", kind, err)
				}
				if before != f.taskSnapshot(t, a) {
					t.Fatal("group postimage tampering escaped rollback", kind)
				}
			})
		}
	})
	t.Run("typed-forgeries-and-header", func(t *testing.T) {
		payload, err := f.taskEvents.DecodeTaskChanged(saved.Event)
		if err != nil {
			t.Fatal(err)
		}
		for _, kind := range []string{"event-id", "command-id", "history-id", "actor", "time", "version", "aggregate", "catalog"} {
			header := saved.Event.Header()
			body := payload.Clone()
			typed := f.taskEvents
			switch kind {
			case "event-id":
				header.EventID = id[event.EventIdentity](t)
			case "command-id":
				body.CommandID = id[wc.TaskCommand](t)
			case "history-id":
				body.TaskEventID = id[wc.TaskEvent](t)
			case "actor":
				body.ActorUserID = id[identity.User](t)
			case "time":
				header.OccurredAt, _ = foundation.NewInstant(header.OccurredAt.Time().Add(time.Microsecond))
			case "version":
				version := *header.AggregateVersion + 1
				header.AggregateVersion = &version
			case "aggregate":
				header.AggregateID = id[event.Aggregate](t)
			case "catalog":
				typed, err = wc.RegisterTaskEvents(event.NewCatalog())
				if err != nil {
					t.Fatal(err)
				}
			}
			forged, err := typed.NewTaskChanged(header, body)
			if err != nil {
				t.Fatal("valid typed forgery premise", kind, err)
			}
			if _, err = f.events.PrepareAppend(ctxFor(t), a, forged); err == nil {
				t.Fatal("typed forgery acquired real append plan", kind)
			}
		}
	})
	t.Run("issuer-actor-locks-and-transaction", func(t *testing.T) {
		newAuthority, err := work.NewAuthority(f.store, f.projectAuthority)
		if err != nil {
			t.Fatal(err)
		}
		renewed := f.renew(t, a)
		foreignDeps, err := oc.NewDependencies(oc.NewPlanIssuer(), saved.Plan.Details().Producer.Binding(), saved.Plan.Details().Producer.Locks(), saved.Plan.Details().Producer.Opaque())
		if err != nil {
			t.Fatal(err)
		}
		for _, kind := range []string{"issuer", "actor", "fake-dependencies", "missing-user", "missing-project", "missing-schedule", "shared-task", "foreign-store", "ended-tx", "historical-current", "completed-new"} {
			t.Run(kind, func(t *testing.T) {
				authority := f.authority
				actor := a
				deps := saved.Plan.Details().Producer
				stage := oc.CurrentAccess
				if kind == "issuer" {
					authority = newAuthority
				}
				if kind == "actor" {
					actor = renewed
				}
				if kind == "fake-dependencies" {
					deps = foreignDeps
				}
				if kind == "completed-new" {
					stage = oc.NewFact
				}
				locks := saved.Plan.Locks()
				var remove foundation.LockKey
				switch kind {
				case "missing-user":
					remove, _ = foundation.UserLock(a.Details().UserID)
				case "missing-project":
					remove, _ = foundation.ProjectLock(p.String())
				case "missing-schedule":
					remove, _ = foundation.ProjectScheduleLock(p.String())
				case "shared-task":
					remove, _ = foundation.AggregateLock(foundation.TaskAggregate, saved.Event.Header().AggregateID.String())
				}
				if remove.Validate() == nil {
					filtered := []foundation.LockRequest{}
					for _, l := range locks {
						if foundation.CompareLockKeys(l.Key, remove) == 0 {
							if kind == "shared-task" {
								l.Mode = foundation.Shared
							} else {
								continue
							}
						}
						filtered = append(filtered, l)
					}
					locks = filtered
				}
				var ended foundation.Tx
				store := f.raw
				if kind == "foreign-store" {
					store = openStore(t, f.db.Config(t, nil))
				}
				result := store.WithinTx(ctxFor(t), cause(t), func(ctx context.Context, tx foundation.Tx) error {
					ended = tx
					if err := store.AcquireAll(ctx, tx, locks); err != nil {
						return err
					}
					if kind == "ended-tx" {
						return nil
					}
					return authority.ValidateAppendInTx(ctx, tx, actor, saved.Event.Summary(), deps, stage)
				})
				if kind == "historical-current" || kind == "ended-tx" {
					if result.State() != foundation.Committed {
						t.Fatal("expected real read/terminal", kind, result.Fault())
					}
					if kind == "ended-tx" {
						if err := authority.ValidateAppendInTx(ctxFor(t), ended, actor, saved.Event.Summary(), deps, stage); err == nil {
							t.Fatal("ended transaction accepted")
						}
					}
				} else if result.State() != foundation.NotCommitted {
					t.Fatal("producer admitted unbound input", kind, result.Fault())
				}
			})
		}
	})
	t.Run("obsolete-real-plan-revision", func(t *testing.T) {
		current, err := f.taskReader.GetTask(ctxFor(t), a, p, target)
		if err != nil {
			t.Fatal(err)
		}
		capture := &capturingAppender{Appender: f.events}
		injected := false
		capture.after = func(ctx context.Context, _ identity.Actor, _ event.Event, _ oc.AppendPlan) error {
			if injected {
				return nil
			}
			injected = true
			_, err := f.tasks.CreateTask(ctx, a, meta(t, "revision-interloper", nil), p, wc.TaskCreate{TaskID: id[wc.Task](t), SprintID: s, Title: "changes real query generation", Type: wc.TaskTypeTask, Priority: wc.TaskPriorityLow})
			return err
		}
		writer := f.newTaskService(t, capture, f.accounts)
		title := "after real revision"
		metadata := meta(t, "real-revision", &current.Version)
		if _, err = writer.UpdateTask(ctxFor(t), a, metadata, p, target, wc.TaskFieldsUpdate{Title: &title}); err != nil {
			t.Fatal(err)
		}
		capture.mu.Lock()
		events := append([]capturedEvent(nil), capture.captured...)
		capture.mu.Unlock()
		if len(events) < 2 || events[0].Event.Header().EventID == events[len(events)-1].Event.Header().EventID {
			t.Fatal("real replan did not replace event identity")
		}
		var revision int64
		if err = f.raw.QueryRow(ctxFor(t), `SELECT plan_revision FROM agenteam_work.task_commands WHERE project_id=$1 AND command_name='work.task.update' AND idempotency_key=$2`, p.String(), string(metadata.IdempotencyKey)).Scan(&revision); err != nil || revision < 2 {
			t.Fatal("real revision not persisted", revision, err)
		}
		obsolete := events[0]
		if _, err = f.events.PrepareAppend(ctxFor(t), a, obsolete.Event); err == nil {
			t.Fatal("old event discovered after real replan")
		}
		result := f.store.WithinTx(ctxFor(t), cause(t), func(ctx context.Context, tx foundation.Tx) error {
			if err := f.store.AcquireAll(ctx, tx, obsolete.Plan.Locks()); err != nil {
				return err
			}
			return f.authority.ValidateAppendInTx(ctx, tx, a, obsolete.Event.Summary(), obsolete.Plan.Details().Producer, oc.CurrentAccess)
		})
		if result.State() != foundation.NotCommitted {
			t.Fatal("old opaque/revision remained valid")
		}
	})
	t.Run("completed-outbox-event-cannot-be-recreated", func(t *testing.T) {
		f.tx(t, saved.Plan.Locks(), func(ctx context.Context, _ foundation.Tx, x postgres.SQLExecutor) error {
			_, err := x.Exec(ctx, `DELETE FROM agenteam_outbox.events WHERE id=$1`, saved.Event.Header().EventID.String())
			return err
		})
		plan, err := f.events.PrepareAppend(ctxFor(t), a, saved.Event)
		if err != nil {
			t.Fatal(err)
		}
		result := f.store.WithinTx(ctxFor(t), cause(t), func(ctx context.Context, tx foundation.Tx) error {
			if err := f.store.AcquireAll(ctx, tx, plan.Locks()); err != nil {
				return err
			}
			_, err := f.events.AppendEventInTx(ctx, tx, a, saved.Event, plan)
			return err
		})
		if result.State() != foundation.NotCommitted {
			t.Fatal("completed event resurrected")
		}
		var count int
		if err = f.raw.QueryRow(ctxFor(t), `SELECT count(*) FROM agenteam_outbox.events WHERE id=$1`, saved.Event.Header().EventID.String()).Scan(&count); err != nil || count != 0 {
			t.Fatal("missing event rebuilt", count, err)
		}
	})
	var auditAfter, handlers, deliveries int
	if err := f.raw.QueryRow(ctxFor(t), `SELECT (SELECT count(*) FROM agenteam_audit.audit_records),(SELECT count(*) FROM agenteam_outbox.handlers),(SELECT count(*) FROM agenteam_outbox.deliveries d JOIN agenteam_outbox.events e ON e.id=d.event_id WHERE e.event_type='work.task_changed')`).Scan(&auditAfter, &handlers, &deliveries); err != nil || auditAfter != auditBefore || handlers != 0 || deliveries != 0 {
		t.Fatal("Task introduced Audit or delivery handler", err)
	}
	t.Run("current-read-before-first-work-sql", func(t *testing.T) { taskObserveCurrentRead(t, f, a, p, s) })
}

// Observation only: SQL is never rewritten, suppressed, or simulated. The same
// concrete Store transaction still reaches real Account/Project authority.
type taskSQLObserver struct {
	fixtureStore
	armed   atomic.Bool
	workSQL atomic.Int64
}

func (s *taskSQLObserver) InTx(tx foundation.Tx) (postgres.SQLExecutor, error) {
	x, err := s.fixtureStore.InTx(tx)
	if err != nil {
		return nil, err
	}
	return taskObservedExecutor{x, s}, nil
}

type taskObservedExecutor struct {
	postgres.SQLExecutor
	observer *taskSQLObserver
}

func (x taskObservedExecutor) observe(sql string) {
	if x.observer.armed.Load() && strings.Contains(sql, "agenteam_work.") {
		x.observer.workSQL.Add(1)
	}
}
func (x taskObservedExecutor) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	x.observe(sql)
	return x.SQLExecutor.Exec(ctx, sql, args...)
}
func (x taskObservedExecutor) Query(ctx context.Context, sql string, args ...any) (*postgres.Rows, error) {
	x.observe(sql)
	return x.SQLExecutor.Query(ctx, sql, args...)
}
func (x taskObservedExecutor) QueryRow(ctx context.Context, sql string, args ...any) postgres.Row {
	x.observe(sql)
	return x.SQLExecutor.QueryRow(ctx, sql, args...)
}
func taskObserveCurrentRead(t *testing.T, base *taskFixture, a identity.Actor, p wc.ProjectID, s wc.SprintID) {
	t.Helper()
	raw := openStore(t, base.db.Config(t, nil))
	store := &taskSQLObserver{fixtureStore: raw}
	f := assembleTask(t, base.db, raw, store, false)
	capture := &capturingAppender{Appender: f.events}
	writer := f.newTaskService(t, capture, f.accounts)
	if _, err := writer.CreateTask(ctxFor(t), a, meta(t, "sql-order-proof", nil), p, wc.TaskCreate{TaskID: id[wc.Task](t), SprintID: s, Title: "proof", Type: wc.TaskTypeTask, Priority: wc.TaskPriorityLow}); err != nil {
		t.Fatal(err)
	}
	saved := capture.last(t)
	user, _ := foundation.UserLock(a.Details().UserID)
	f.tx(t, []foundation.LockRequest{{Key: user, Mode: foundation.Exclusive}}, func(ctx context.Context, tx foundation.Tx, x postgres.SQLExecutor) error {
		if err := f.accounts.RequireCurrentSession(ctx, tx, a); err != nil {
			return err
		}
		_, err := x.Exec(ctx, `UPDATE agenteam_account.sessions SET revoked_at=clock_timestamp(),revoked_reason='administrative' WHERE id=$1`, a.Details().SessionID)
		return err
	})
	store.armed.Store(true)
	result := store.WithinTx(ctxFor(t), cause(t), func(ctx context.Context, tx foundation.Tx) error {
		if err := store.AcquireAll(ctx, tx, saved.Plan.Locks()); err != nil {
			return err
		}
		return f.authority.ValidateAppendInTx(ctx, tx, a, saved.Event.Summary(), saved.Plan.Details().Producer, oc.CurrentAccess)
	})
	store.armed.Store(false)
	if result.State() != foundation.NotCommitted {
		t.Fatal("revoked producer committed")
	}
	requireCode(t, result.Fault(), foundation.SessionRevoked)
	if store.workSQL.Load() != 0 {
		t.Fatal("Work SQL preceded current Read denial", store.workSQL.Load())
	}
}
