//go:build integration

package work_test

import (
	"context"
	"encoding/json"
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/LunaDeerTech/agenteam/db/migrations"
	"github.com/LunaDeerTech/agenteam/internal/central/audit"
	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/outbox"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/LunaDeerTech/agenteam/internal/central/work"
	wc "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
	pgfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/postgres"
)

// Blocker tests reuse the accepted Task/Project fixture boundary. The new
// producer has its own real Catalog and Outbox; no existing fixture is edited.
type blockerFixture struct {
	*taskFixture
	blockers      *work.BlockerService
	blockerEvents wc.TaskBlockerEvents
	blockerOutbox *outbox.Service
}

func newBlockerFixture(t *testing.T) *blockerFixture {
	t.Helper()
	return attachBlockers(t, newTaskFixture(t))
}

func attachBlockers(t *testing.T, base *taskFixture) *blockerFixture {
	t.Helper()
	catalog := event.NewCatalog()
	bt, err := wc.RegisterTaskBlockerEvents(catalog)
	if err != nil {
		t.Fatal(err)
	}
	aud, err := audit.New(base.store, base.keys, audit.Authorizations{Sessions: base.accounts, System: base.accounts, Accounts: base.accounts, Projects: base.projectAuthority})
	if err != nil {
		t.Fatal(err)
	}
	box, err := outbox.New(base.store, catalog, outbox.Authorizations{Producers: map[event.StableName]oc.ProducerAuthority{wc.WorkProducer: base.authority}, Projects: base.projectAuthority, Sessions: base.accounts, System: base.accounts, Processes: fixtureProcess{id[oc.Process](t)}, Audit: aud, Cursors: base.keys})
	if err != nil {
		t.Fatal(err)
	}
	f := &blockerFixture{taskFixture: base, blockerEvents: bt, blockerOutbox: box}
	f.blockers = f.newBlockerService(t, box, base.accounts)
	return f
}

func (f *blockerFixture) newBlockerService(t *testing.T, box oc.Appender, activity work.ActivityAuthority) *work.BlockerService {
	t.Helper()
	s, err := work.NewBlocker(f.store, work.BlockerDependencies{Authority: f.authority, Structure: f.reader, Events: box, BlockerEvents: f.blockerEvents, Activity: activity})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		s.Stop()
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := s.Drain(ctx); err != nil {
			t.Error("Blocker Drain did not join", err)
		}
	})
	return s
}

func blockerWaiting(t *testing.T, description string) wc.TaskBlockerCreate {
	t.Helper()
	return wc.TaskBlockerCreate{BlockerID: id[wc.TaskBlockerIdentity](t), Type: wc.TaskBlockerWaitingForHuman, Description: description, Metadata: wc.TaskBlockerMetadata{WaitingForHuman: &wc.TaskBlockerWaitingForHumanMetadata{}}}
}

func blockerDependency(t *testing.T, related wc.TaskID) wc.TaskBlockerCreate {
	t.Helper()
	return wc.TaskBlockerCreate{BlockerID: id[wc.TaskBlockerIdentity](t), Type: wc.TaskBlockerRelyOn, Description: "explicit dependency", Metadata: wc.TaskBlockerMetadata{RelyOn: &wc.TaskBlockerRelyOnMetadata{RelatedTaskID: related}}}
}

func equalBlockerMutation(t *testing.T, a, b wc.TaskBlockerMutation) {
	t.Helper()
	if string(jsonBytes(t, a)) != string(jsonBytes(t, b)) {
		t.Fatal("Blocker immutable receipt changed")
	}
}

func (f *blockerFixture) blockerSnapshot(t *testing.T, a identity.Actor) string {
	t.Helper()
	var raw string
	err := f.raw.QueryRow(ctxFor(t), `SELECT jsonb_build_object(
 'tasks',(SELECT coalesce(jsonb_agg(to_jsonb(t) ORDER BY id),'[]'::jsonb) FROM agenteam_work.tasks t),
 'blockers',(SELECT coalesce(jsonb_agg(to_jsonb(t) ORDER BY id),'[]'::jsonb) FROM agenteam_work.task_blockers t),
 'groups',(SELECT coalesce(jsonb_agg(to_jsonb(t) ORDER BY project_id,sprint_id,state,priority),'[]'::jsonb) FROM agenteam_work.task_order_groups t),
 'query',(SELECT coalesce(jsonb_agg(to_jsonb(t) ORDER BY project_id),'[]'::jsonb) FROM agenteam_work.task_query_generations t),
 'history',(SELECT coalesce(jsonb_agg(to_jsonb(t) ORDER BY id),'[]'::jsonb) FROM agenteam_work.task_events t),
 'receipts',(SELECT coalesce(jsonb_agg(to_jsonb(t) ORDER BY id),'[]'::jsonb) FROM agenteam_work.task_blocker_commands t WHERE state='completed'),
 'events',(SELECT coalesce(jsonb_agg(to_jsonb(t) ORDER BY id),'[]'::jsonb) FROM agenteam_outbox.events t),
 'activity',(SELECT last_activity_at FROM agenteam_account.sessions WHERE id=$1))::text`, a.Details().SessionID).Scan(&raw)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func blockerOldSnapshot(t *testing.T, store postgres.SQLExecutor) string {
	t.Helper()
	var raw string
	err := store.QueryRow(ctxFor(t), `SELECT jsonb_build_object(
 'tasks',(SELECT coalesce(jsonb_agg(to_jsonb(t) ORDER BY id),'[]'::jsonb) FROM agenteam_work.tasks t),
 'history',(SELECT coalesce(jsonb_agg(to_jsonb(t)-'blocker_operation_id' ORDER BY id),'[]'::jsonb) FROM agenteam_work.task_events t),
 'commands',(SELECT coalesce(jsonb_agg(to_jsonb(t) ORDER BY id),'[]'::jsonb) FROM agenteam_work.task_commands t),
 'groups',(SELECT coalesce(jsonb_agg(to_jsonb(t) ORDER BY project_id,sprint_id,state,priority),'[]'::jsonb) FROM agenteam_work.task_order_groups t),
 'query',(SELECT coalesce(jsonb_agg(to_jsonb(t) ORDER BY project_id),'[]'::jsonb) FROM agenteam_work.task_query_generations t),
 'outbox',(SELECT coalesce(jsonb_agg(to_jsonb(t) ORDER BY id),'[]'::jsonb) FROM agenteam_outbox.events t))::text`).Scan(&raw)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestTaskBlockerPersistence(t *testing.T) {
	t.Run("retained-history-and-unresolved-capacity", testTaskBlockerCapacity)
	t.Run("project-unresolved-capacity", testTaskBlockerProjectCapacity)
	t.Run("populated-22-upgrade-and-repeat", func(t *testing.T) {
		db := pgfixture.NewDatabase(t)
		migrate(t, db, migrationPrefix(t, "00022"))
		raw := openStore(t, db.Config(t, nil))
		base := assembleTask(t, db, raw, raw, true)
		a := base.human(t, "blocker-upgrade", "user")
		p, _, _ := base.create(t, a, "blocker-upgrade")
		m := base.milestone(t, a, p.ID, "retained")
		s := base.sprint(t, a, p.ID, m.ID, "retained")
		target := base.task(t, a, p.ID, s.ID, "old Task")
		before := blockerOldSnapshot(t, raw)
		migrate(t, db)
		migrate(t, db)
		if blockerOldSnapshot(t, raw) != before {
			t.Fatal("00023 rewrote old business facts")
		}
		f := attachBlockers(t, base)
		created, err := f.blockers.AddTaskBlocker(ctxFor(t), a, meta(t, "first-blocker", &target.Version), p.ID, target.ID, blockerWaiting(t, "after upgrade"))
		if err != nil || created.Task.Version != target.Version+1 {
			t.Fatal("upgraded Task cannot create Blocker", err)
		}
		title := "old writer remains usable"
		updated, err := base.tasks.UpdateTask(ctxFor(t), a, meta(t, "old-writer", &created.Task.Version), p.ID, target.ID, wc.TaskFieldsUpdate{Title: &title})
		if err != nil || updated.Task.Title != title {
			t.Fatal("00023 broke old planning writer", err)
		}
		var history []byte
		if err = raw.QueryRow(ctxFor(t), `SELECT jsonb_build_object('id',id,'project_id',project_id,'task_id',task_id,'task_version',task_version::text,'type',type,'actor',actor,'operation_id',operation_id,'correlation_id',correlation_id,'payload',payload,'created_at',to_char(created_at AT TIME ZONE 'UTC','YYYY-MM-DD"T"HH24:MI:SS.US"Z"')) FROM agenteam_work.task_events WHERE id=$1`, updated.TaskEventID.String()).Scan(&history); err != nil {
			t.Fatal(err)
		}
		var legacy wc.TaskEvent
		if err = json.Unmarshal(history, &legacy); err != nil {
			t.Fatal("legacy history no longer decodes", err)
		}
		var tables, crossFK int
		if err = raw.QueryRow(ctxFor(t), `SELECT count(*) FROM pg_tables WHERE schemaname='agenteam_work' AND tablename IN ('task_blockers','task_blocker_commands')`).Scan(&tables); err != nil || tables != 2 {
			t.Fatal("new canonical tables missing", err)
		}
		if err = raw.QueryRow(ctxFor(t), `SELECT count(*) FROM pg_constraint c JOIN pg_class a ON a.oid=c.conrelid JOIN pg_namespace an ON an.oid=a.relnamespace JOIN pg_class b ON b.oid=c.confrelid JOIN pg_namespace bn ON bn.oid=b.relnamespace WHERE c.contype='f' AND an.nspname='agenteam_work' AND bn.nspname<>'agenteam_work'`).Scan(&crossFK); err != nil || crossFK != 0 {
			t.Fatal("cross-domain foreign key", err)
		}
	})
	t.Run("migration-DDL-failure-is-atomic", func(t *testing.T) {
		db := pgfixture.NewDatabase(t)
		migrate(t, db, migrationPrefix(t, "00022"))
		raw := openStore(t, db.Config(t, nil))
		base := assembleTask(t, db, raw, raw, true)
		a := base.human(t, "blocker-ddl", "user")
		p, _, _ := base.create(t, a, "blocker-ddl")
		m := base.milestone(t, a, p.ID, "m")
		s := base.sprint(t, a, p.ID, m.ID, "s")
		base.task(t, a, p.ID, s.ID, "retained")
		before := blockerOldSnapshot(t, raw)
		files := migrationFiles(t, "00022")
		sql, err := fs.ReadFile(migrations.SQL, "00023_task_blockers.sql")
		if err != nil {
			t.Fatal(err)
		}
		files["00023_task_blockers.sql"] = &fstest.MapFile{Data: append(append([]byte{}, sql...), []byte("\nSELECT 1/0;\n")...)}
		source, err := postgres.NewSource(files, nil)
		if err != nil {
			t.Fatal(err)
		}
		migrator, err := postgres.NewMigrator(db.Config(t, nil), source)
		if err != nil {
			t.Fatal(err)
		}
		result := migrator.Migrate(ctxFor(t))
		if result.Migrated || result.Fault == nil {
			t.Fatal("injected DDL failure accepted")
		}
		var leaked int
		if err = raw.QueryRow(ctxFor(t), `SELECT (SELECT count(*) FROM pg_tables WHERE schemaname='agenteam_work' AND tablename IN ('task_blockers','task_blocker_commands'))+(SELECT count(*) FROM information_schema.columns WHERE table_schema='agenteam_work' AND table_name='task_events' AND column_name='blocker_operation_id')+(SELECT count(*) FROM agenteam_meta.goose_db_version WHERE version_id=23 AND is_applied)`).Scan(&leaked); err != nil || leaked != 0 {
			t.Fatal("partial 00023 escaped rollback", err)
		}
		if blockerOldSnapshot(t, raw) != before {
			t.Fatal("failed migration changed existing facts")
		}
	})
	t.Run("fresh-workflow-history-rebuild-and-replay", func(t *testing.T) {
		f := newBlockerFixture(t)
		a := f.human(t, "blocker-owner", "user")
		p, _, _ := f.create(t, a, "blocker-workflow")
		m := f.milestone(t, a, p.ID, "m")
		s := f.sprint(t, a, p.ID, m.ID, "s")
		original := f.task(t, a, p.ID, s.ID, "source")
		related := f.task(t, a, p.ID, s.ID, "related")
		g, q := f.generation(t, p.ID, s.ID, original.Priority)
		page, err := f.taskReader.ListTasks(ctxFor(t), a, p.ID, wc.TaskFilter{}, foundation.PageRequest{Limit: 1})
		if err != nil || page.NextCursor == "" {
			t.Fatal("cursor setup", err)
		}
		f.ageSession(t, a)
		var activityBefore, activityAfter time.Time
		if err = f.raw.QueryRow(ctxFor(t), `SELECT last_activity_at FROM agenteam_account.sessions WHERE id=$1`, a.Details().SessionID).Scan(&activityBefore); err != nil {
			t.Fatal(err)
		}
		request := blockerWaiting(t, strings.Repeat("<", wc.MaxTaskBlockerDescriptionBytes))
		cm := meta(t, "wait", &original.Version)
		first, err := f.blockers.AddTaskBlocker(ctxFor(t), a, cm, p.ID, original.ID, request)
		if err != nil {
			t.Fatal(err)
		}
		if first.Blocker.ID != request.BlockerID || first.Blocker.Description != request.Description || first.Blocker.ResolvedAt != nil || first.Task.Version != original.Version+1 {
			t.Fatal("first canonical Blocker result")
		}
		same := first.Task.Clone()
		same.Version, same.UpdatedAt = original.Version, original.UpdatedAt
		if string(jsonBytes(t, same)) != string(jsonBytes(t, original)) {
			t.Fatal("Blocker changed Task business fields/rank")
		}
		afterG, afterQ := f.generation(t, p.ID, s.ID, original.Priority)
		if afterG != g || afterQ != q+1 {
			t.Fatal("wrong query/order generation")
		}
		_, err = f.taskReader.ListTasks(ctxFor(t), a, p.ID, wc.TaskFilter{}, foundation.PageRequest{Limit: 1, Cursor: page.NextCursor})
		requireCode(t, err, foundation.CursorStale)
		if err = f.raw.QueryRow(ctxFor(t), `SELECT last_activity_at FROM agenteam_account.sessions WHERE id=$1`, a.Details().SessionID).Scan(&activityAfter); err != nil || !activityAfter.After(activityBefore) {
			t.Fatal("real Activity missing", err)
		}
		secondRequest := blockerDependency(t, related.ID)
		second, err := f.blockers.AddTaskBlocker(ctxFor(t), a, meta(t, "dependency", &first.Task.Version), p.ID, original.ID, secondRequest)
		if err != nil {
			t.Fatal(err)
		}
		comment := strings.Repeat("&", wc.MaxTaskBlockerResolutionCommentBytes)
		resolved, err := f.blockers.ResolveTaskBlocker(ctxFor(t), a, meta(t, "resolve", &second.Task.Version), p.ID, original.ID, wc.TaskBlockerResolve{BlockerID: first.Blocker.ID, ResolutionComment: &comment})
		if err != nil || resolved.Blocker.ResolvedAt == nil || resolved.Blocker.ResolvedBy == nil || resolved.Blocker.ResolutionComment == nil || *resolved.Blocker.ResolutionComment != comment {
			t.Fatal("resolve canonical facts", err)
		}
		for _, tc := range []struct {
			status wc.TaskBlockerStatus
			count  int
		}{{wc.TaskBlockersUnresolved, 1}, {wc.TaskBlockersResolved, 1}, {wc.TaskBlockersAll, 2}} {
			rows, err := f.blockers.ListTaskBlockers(ctxFor(t), a, p.ID, original.ID, tc.status)
			if err != nil || rows == nil || len(rows) != tc.count {
				t.Fatal("filtered Blocker read", tc.status, err)
			}
			for n := 1; n < len(rows); n++ {
				if rows[n].CreatedAt.Time().Before(rows[n-1].CreatedAt.Time()) || rows[n].CreatedAt == rows[n-1].CreatedAt && rows[n].ID.String() <= rows[n-1].ID.String() {
					t.Fatal("unstable Blocker order")
				}
			}
		}
		for _, mutation := range []wc.TaskBlockerMutation{first, second, resolved} {
			f.assertBlockerFacts(t, a, mutation)
		}
		rebuilt := attachBlockers(t, f.taskFixture)
		semantic, err := wc.TaskBlockerAddDigest(a, cm, p.ID, original.ID, request)
		if err != nil {
			t.Fatal(err)
		}
		query := wc.TaskBlockerCommandLookupRequest{ProjectID: p.ID, Command: wc.TaskBlockerCommandAdd, IdempotencyKey: cm.IdempotencyKey, SemanticDigest: semantic}
		before := f.blockerSnapshot(t, a)
		lookup, err := rebuilt.blockers.LookupTaskBlockerCommand(ctxFor(t), a, query)
		if err != nil || lookup.Status != wc.LookupCommitted || lookup.Receipt == nil {
			t.Fatal("rebuilt command lookup", err)
		}
		equalBlockerMutation(t, first, *lookup.Receipt)
		replay, err := rebuilt.blockers.AddTaskBlocker(ctxFor(t), a, cm, p.ID, original.ID, request)
		if err != nil {
			t.Fatal("historical replay revalidated current version", err)
		}
		equalBlockerMutation(t, first, replay)
		if f.blockerSnapshot(t, a) != before {
			t.Fatal("read/replay mutated canonical facts or Activity")
		}
		_, err = f.blockers.ResolveTaskBlocker(ctxFor(t), a, meta(t, "second-resolve", &resolved.Task.Version), p.ID, original.ID, wc.TaskBlockerResolve{BlockerID: first.Blocker.ID})
		requireCode(t, err, foundation.BlockerAlreadyResolved)
		if f.blockerSnapshot(t, a) != before {
			t.Fatal("duplicate new resolve partially wrote")
		}
	})
}

func testTaskBlockerProjectCapacity(t *testing.T) {
	f := newBlockerFixture(t)
	a := f.human(t, "blocker-project-cap", "user")
	p, _, _ := f.create(t, a, "blocker-project-cap")
	m := f.milestone(t, a, p.ID, "m")
	s := f.sprint(t, a, p.ID, m.ID, "s")
	origin := f.task(t, a, p.ID, s.ID, "real origin")
	target := f.task(t, a, p.ID, s.ID, "real boundary target")
	first, err := f.blockers.AddTaskBlocker(ctxFor(t), a, meta(t, "project-cap-origin", &origin.Version), p.ID, origin.ID, blockerWaiting(t, "pressure origin"))
	if err != nil {
		t.Fatal(err)
	}
	// Each synthetic Task has at most 256 unresolved rows; the real target
	// remains empty. This isolates the Project bound from the per-Task bound.
	if _, err = f.raw.Exec(ctxFor(t), `INSERT INTO agenteam_work.tasks(id,project_id,milestone_id,sprint_id,title,description,type,priority,state,assignee_agent_id,plan,manual_rank,version,created_at,updated_at)
 SELECT ('01903000-0000-7000-8000-'||lpad(g::text,12,'0'))::uuid,project_id,milestone_id,sprint_id,'capacity fixture',description,type,priority,state,assignee_agent_id,plan,lpad(to_hex(g),32,'0'),version,created_at,updated_at FROM agenteam_work.tasks CROSS JOIN generate_series(1,1024) g WHERE id=$1`, origin.ID.String()); err != nil {
		t.Fatal("bounded Project Task pressure fixture", err)
	}
	if _, err = f.raw.Exec(ctxFor(t), `INSERT INTO agenteam_work.task_blockers(id,project_id,task_id,type,description,metadata,created_at,created_by,created_operation_id)
 SELECT ('01904000-0000-7000-8000-'||lpad(g::text,12,'0'))::uuid,project_id,('01903000-0000-7000-8000-'||lpad(((g-1)/256+1)::text,12,'0'))::uuid,type,description,metadata,created_at,created_by,created_operation_id FROM agenteam_work.task_blockers CROSS JOIN generate_series(1,262143) g WHERE id=$1`, first.Blocker.ID.String()); err != nil {
		t.Fatal("bounded Project Blocker pressure fixture", err)
	}
	var count, maximum int64
	if err = f.raw.QueryRow(ctxFor(t), `SELECT count(*),(SELECT max(n) FROM (SELECT count(*) n FROM agenteam_work.task_blockers WHERE project_id=$1 AND resolved_at IS NULL GROUP BY task_id) q) FROM agenteam_work.task_blockers WHERE project_id=$1 AND resolved_at IS NULL`, p.ID.String()).Scan(&count, &maximum); err != nil || count != 262144 || maximum > 256 {
		t.Fatal("Project capacity fixture did not isolate the intended bound", err)
	}
	_, err = f.blockers.AddTaskBlocker(ctxFor(t), a, meta(t, "project-overflow", &target.Version), p.ID, target.ID, blockerWaiting(t, "overflow"))
	requireCode(t, err, foundation.ResourceBusy)
	rows, err := f.blockers.ListTaskBlockers(ctxFor(t), a, p.ID, target.ID, wc.TaskBlockersAll)
	if err != nil || len(rows) != 0 {
		t.Fatal("Project overflow partially created target blocker", err)
	}
	_, err = f.blockers.ResolveTaskBlocker(ctxFor(t), a, meta(t, "project-free-one", &first.Task.Version), p.ID, origin.ID, wc.TaskBlockerResolve{BlockerID: first.Blocker.ID})
	if err != nil {
		t.Fatal("resolve at Project bound rejected", err)
	}
	_, err = f.blockers.AddTaskBlocker(ctxFor(t), a, meta(t, "project-reuse-slot", &target.Version), p.ID, target.ID, blockerWaiting(t, "fits"))
	if err != nil {
		t.Fatal("Project unresolved slot was not reusable", err)
	}
}

// Pressure rows are explicit test-owned canonical fixtures after a real
// authorized creation. They exercise counters/stream bounds, not per-row
// command authorization or a fabricated successful bulk-creation endpoint.
func TestTaskBlockerHistoryCapacityRegression(t *testing.T) {
	testTaskBlockerCapacity(t)
}

func testTaskBlockerCapacity(t *testing.T) {
	f := newBlockerFixture(t)
	a := f.human(t, "blocker-capacity", "user")
	p, _, _ := f.create(t, a, "blocker-capacity")
	m := f.milestone(t, a, p.ID, "m")
	s := f.sprint(t, a, p.ID, m.ID, "s")
	target := f.task(t, a, p.ID, s.ID, "retained")
	first, err := f.blockers.AddTaskBlocker(ctxFor(t), a, meta(t, "capacity-origin", &target.Version), p.ID, target.ID, blockerWaiting(t, "fixture origin"))
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := f.blockers.ResolveTaskBlocker(ctxFor(t), a, meta(t, "capacity-resolved", &first.Task.Version), p.ID, target.ID, wc.TaskBlockerResolve{BlockerID: first.Blocker.ID})
	if err != nil {
		t.Fatal(err)
	}
	active, err := f.blockers.AddTaskBlocker(ctxFor(t), a, meta(t, "capacity-live", &resolved.Task.Version), p.ID, target.ID, blockerWaiting(t, "live"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.raw.Exec(ctxFor(t), `INSERT INTO agenteam_work.task_blockers(id,project_id,task_id,type,description,metadata,created_at,created_by,created_operation_id,resolved_at,resolved_by,resolution_comment,resolved_operation_id)
 SELECT ('01901000-0000-7000-8000-'||lpad(g::text,12,'0'))::uuid,project_id,task_id,type,description,metadata,created_at,created_by,created_operation_id,resolved_at,resolved_by,resolution_comment,resolved_operation_id FROM agenteam_work.task_blockers CROSS JOIN generate_series(1,4094) g WHERE id=$1`, first.Blocker.ID.String()); err != nil {
		t.Fatal("history pressure fixture", err)
	}
	_, err = f.blockers.AddTaskBlocker(ctxFor(t), a, meta(t, "history-overflow", &active.Task.Version), p.ID, target.ID, blockerWaiting(t, "overflow"))
	taskFaultReason(t, err, foundation.ResourceBusy, "BLOCKER_HISTORY_LIMIT")
	last, err := f.blockers.ResolveTaskBlocker(ctxFor(t), a, meta(t, "resolve-at-history-cap", &active.Task.Version), p.ID, target.ID, wc.TaskBlockerResolve{BlockerID: active.Blocker.ID})
	if err != nil {
		t.Fatal("history cap blocked legitimate resolve", err)
	}
	rows, err := f.blockers.ListTaskBlockers(ctxFor(t), a, p.ID, target.ID, wc.TaskBlockersAll)
	if err != nil || len(rows) != 4096 || last.Task.Version != active.Task.Version+1 {
		t.Fatal("retained history bound", err)
	}
	target = f.task(t, a, p.ID, s.ID, "unresolved")
	active, err = f.blockers.AddTaskBlocker(ctxFor(t), a, meta(t, "unresolved-origin", &target.Version), p.ID, target.ID, blockerWaiting(t, "unresolved"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.raw.Exec(ctxFor(t), `INSERT INTO agenteam_work.task_blockers(id,project_id,task_id,type,description,metadata,created_at,created_by,created_operation_id)
 SELECT ('01902000-0000-7000-8000-'||lpad(g::text,12,'0'))::uuid,project_id,task_id,type,description,metadata,created_at,created_by,created_operation_id FROM agenteam_work.task_blockers CROSS JOIN generate_series(1,255) g WHERE id=$1`, active.Blocker.ID.String()); err != nil {
		t.Fatal("unresolved pressure fixture", err)
	}
	_, err = f.blockers.AddTaskBlocker(ctxFor(t), a, meta(t, "unresolved-overflow", &active.Task.Version), p.ID, target.ID, blockerWaiting(t, "overflow"))
	requireCode(t, err, foundation.ResourceBusy)
	freed, err := f.blockers.ResolveTaskBlocker(ctxFor(t), a, meta(t, "free-one", &active.Task.Version), p.ID, target.ID, wc.TaskBlockerResolve{BlockerID: active.Blocker.ID})
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.blockers.AddTaskBlocker(ctxFor(t), a, meta(t, "reuse-free-capacity", &freed.Task.Version), p.ID, target.ID, blockerWaiting(t, "fits"))
	if err != nil {
		t.Fatal("freed unresolved slot was not reusable", err)
	}
}

func (f *blockerFixture) assertBlockerFacts(t *testing.T, actor identity.Actor, result wc.TaskBlockerMutation) {
	t.Helper()
	var operation, correlation, kind, user string
	var taskVersion int64
	var created, taskAt time.Time
	var payload, headerRaw, eventPayload []byte
	var legacyOperation *string
	err := f.raw.QueryRow(ctxFor(t), `SELECT blocker_operation_id::text,operation_id::text,correlation_id::text,type,actor->>'user_id',task_version,created_at,payload FROM agenteam_work.task_events WHERE id=$1`, result.TaskEventID.String()).Scan(&operation, &legacyOperation, &correlation, &kind, &user, &taskVersion, &created, &payload)
	if err != nil || legacyOperation != nil || operation != correlation || user != actor.Details().UserID || taskVersion != int64(result.Task.Version) || !created.Equal(result.Task.UpdatedAt.Time()) {
		t.Fatal("real TaskEvent alignment", err)
	}
	if err = f.raw.QueryRow(ctxFor(t), `SELECT updated_at FROM agenteam_work.tasks WHERE id=$1`, result.Task.ID.String()).Scan(&taskAt); err != nil || taskAt.Before(created) {
		t.Fatal("Task time precedes historical fact", err)
	}
	if kind == "blocker_added" {
		var v wc.TaskBlockerAddedPayload
		if err = json.Unmarshal(payload, &v); err != nil || v.BlockerID != result.Blocker.ID || v.BlockerType != result.Blocker.Type {
			t.Fatal("added history", err)
		}
	} else {
		var v wc.TaskBlockerResolvedPayload
		if kind != "blocker_resolved" {
			t.Fatal("unexpected history type")
		}
		if err = json.Unmarshal(payload, &v); err != nil || v.BlockerID != result.Blocker.ID || v.ResolutionComment == nil || result.Blocker.ResolutionComment == nil || *v.ResolutionComment != *result.Blocker.ResolutionComment {
			t.Fatal("resolved history", err)
		}
	}
	if len(result.EventIDs) != 1 {
		t.Fatal("one mutation must have one Outbox event")
	}
	if err = f.raw.QueryRow(ctxFor(t), `SELECT header,payload FROM agenteam_outbox.events WHERE id=$1`, result.EventIDs[0].String()).Scan(&headerRaw, &eventPayload); err != nil {
		t.Fatal(err)
	}
	var header event.Header
	if err = json.Unmarshal(headerRaw, &header); err != nil {
		t.Fatal(err)
	}
	ev, err := f.blockerEvents.Restore(header, eventPayload)
	if err != nil {
		t.Fatal(err)
	}
	v, err := f.blockerEvents.DecodeTaskBlockersChanged(ev)
	if err != nil || v.OperationID.String() != operation || v.ActorUserID.String() != actor.Details().UserID || v.TaskEventID != result.TaskEventID || v.BlockerID != result.Blocker.ID || header.AggregateVersion == nil || *header.AggregateVersion != result.Task.Version || header.OccurredAt != result.Task.UpdatedAt {
		t.Fatal("typed Outbox fact mismatch", err)
	}
	var finalized, prepared time.Time
	if err = f.raw.QueryRow(ctxFor(t), `SELECT created_at,committed_at FROM agenteam_work.task_blocker_commands WHERE id=$1 AND state='completed'`, operation).Scan(&prepared, &finalized); err != nil || finalized.Before(prepared) || finalized.Before(created) {
		t.Fatal("command completion time mismatch", err)
	}
}
