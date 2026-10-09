//go:build integration

package work_test

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/work"
	wc "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
)

// This first top exercises the public pager through real PG. Its accepted
// library fixture uses test-owned Account facts and a persistent Skills receipt;
// it does not prove HTTP login, production Skills, or the default app root.
func TestWorkOwnerBlockerPage(t *testing.T) {
	f := newBlockerFixture(t)
	a := f.human(t, "page-owner", "user")
	other := f.human(t, "page-other", "user")
	admin := f.human(t, "page-admin", "admin")
	p, _, _ := f.create(t, a, "blocker-pages")
	m := f.milestone(t, a, p.ID, "m")
	s := f.sprint(t, a, p.ID, m.ID, "s")
	target := f.task(t, a, p.ID, s.ID, "target")
	related := f.task(t, a, p.ID, s.ID, "related")
	reader, err := work.NewBlockerReader(f.store, f.authority, f.keys)
	if err != nil {
		t.Fatal(err)
	}
	read := func(actor identity.Actor, status wc.TaskBlockerStatus, limit int, token string) foundation.Page[wc.TaskBlocker] {
		t.Helper()
		page, e := reader.ListTaskBlockersPage(ctxFor(t), actor, p.ID, target.ID, status, foundation.PageRequest{Limit: limit, Cursor: token})
		if e != nil || page.Items == nil || len(page.Items) > limit {
			t.Fatal("authorized bounded page", e)
		}
		return page
	}
	assertFailure := func(page foundation.Page[wc.TaskBlocker], e error, code foundation.Code) {
		t.Helper()
		requireCode(t, e, code)
		if page.Items != nil || page.NextCursor != "" {
			t.Fatal("failure exposed a partial page")
		}
	}
	add := func(key string) wc.TaskBlocker {
		t.Helper()
		r := blockerWaiting(t, "page "+key)
		if key == "0" {
			r = blockerDependency(t, related.ID)
		}
		result, e := f.blockers.AddTaskBlocker(ctxFor(t), a, meta(t, "page-add-"+key, &target.Version), p.ID, target.ID, r)
		if e != nil {
			t.Fatal(e)
		}
		target = result.Task
		return result.Blocker
	}
	if page := read(a, wc.TaskBlockersAll, 1, ""); len(page.Items) != 0 || page.NextCursor != "" {
		t.Fatal("empty page")
	}
	values := make([]wc.TaskBlocker, 6)
	for n := range values {
		values[n] = add(fmt.Sprint(n))
	}
	resolved, err := f.blockers.ResolveTaskBlocker(ctxFor(t), a, meta(t, "page-resolve-initial", &target.Version), p.ID, target.ID, wc.TaskBlockerResolve{BlockerID: values[0].ID})
	if err != nil {
		t.Fatal(err)
	}
	target = resolved.Task
	values[0] = resolved.Blocker

	t.Run("real-pages-status-identity-and-version", func(t *testing.T) {
		before := f.blockerSnapshot(t, a)
		first := read(a, wc.TaskBlockersAll, 1, "")
		if len(first.Items) != 1 || first.Items[0].ID != values[0].ID || first.NextCursor == "" {
			t.Fatal("first/lookahead position")
		}
		middle := read(a, wc.TaskBlockersAll, 2, first.NextCursor)
		last := read(a, wc.TaskBlockersAll, 200, middle.NextCursor)
		got := append(append(first.Items, middle.Items...), last.Items...)
		if len(got) != len(values) || middle.NextCursor == "" || last.NextCursor != "" {
			t.Fatal("page boundaries skipped or repeated rows")
		}
		for n := range values {
			if got[n].ID != values[n].ID {
				t.Fatal("lookahead used as continuation")
			}
		}
		if f.blockerSnapshot(t, a) != before {
			t.Fatal("read changed canonical/history/receipt/Activity")
		}
		if page := read(a, wc.TaskBlockersResolved, 200, ""); len(page.Items) != 1 || page.Items[0].ResolvedAt == nil {
			t.Fatal("resolved filter")
		}
		if page := read(a, wc.TaskBlockersUnresolved, 200, ""); len(page.Items) != 5 {
			t.Fatal("unresolved filter")
		}
		page, e := reader.ListTaskBlockersPage(ctxFor(t), a, p.ID, target.ID, wc.TaskBlockersUnresolved, foundation.PageRequest{Limit: 2, Cursor: first.NextCursor})
		assertFailure(page, e, foundation.CursorInvalid)
		page, e = reader.ListTaskBlockersPage(ctxFor(t), a, p.ID, related.ID, wc.TaskBlockersAll, foundation.PageRequest{Limit: 2, Cursor: first.NextCursor})
		assertFailure(page, e, foundation.CursorInvalid)
		fresh := f.renew(t, a)
		if got := read(fresh, wc.TaskBlockersAll, 2, first.NextCursor); got.Items[0].ID != values[1].ID {
			t.Fatal("same User new Session cannot continue")
		}
		for _, denied := range []identity.Actor{other, admin} {
			page, e = reader.ListTaskBlockersPage(ctxFor(t), denied, p.ID, target.ID, wc.TaskBlockersAll, foundation.PageRequest{Limit: 2, Cursor: "bad-signature"})
			assertFailure(page, e, foundation.NotFound)
		}
		for n, mutate := range []func(){
			func() { add("version") },
			func() {
				v, e := f.blockers.ResolveTaskBlocker(ctxFor(t), a, meta(t, "page-resolve-version", &target.Version), p.ID, target.ID, wc.TaskBlockerResolve{BlockerID: values[1].ID})
				if e != nil {
					t.Fatal(e)
				}
				target = v.Task
			},
			func() {
				title := "changed"
				v, e := f.tasks.UpdateTask(ctxFor(t), a, meta(t, "page-task-version", &target.Version), p.ID, target.ID, wc.TaskFieldsUpdate{Title: &title})
				if e != nil {
					t.Fatal(e)
				}
				target = v.Task
			},
			func() {
				v, e := f.tasks.ReorderTask(ctxFor(t), a, meta(t, "page-reorder-version", &target.Version), p.ID, target.ID, wc.TaskReorder{})
				if e != nil {
					t.Fatal(e)
				}
				target = v.Task
			},
		} {
			current := read(a, wc.TaskBlockersAll, 1, "")
			version := target.Version
			mutate()
			if target.Version != version+1 {
				t.Fatalf("mutation %d did not create a real Task version", n)
			}
			page, e = reader.ListTaskBlockersPage(ctxFor(t), a, p.ID, target.ID, wc.TaskBlockersAll, foundation.PageRequest{Limit: 2, Cursor: current.NextCursor})
			assertFailure(page, e, foundation.CursorStale)
		}
		current := read(a, wc.TaskBlockersAll, 1, "")
		same := target.Title
		v, e := f.tasks.UpdateTask(ctxFor(t), a, meta(t, "page-noop", &target.Version), p.ID, target.ID, wc.TaskFieldsUpdate{Title: &same})
		if e != nil || v.Task.Version != target.Version {
			t.Fatal("no-op baseline", e)
		}
		read(a, wc.TaskBlockersAll, 2, current.NextCursor)
	})

	t.Run("actual-rows-cancellation-releases-writer", func(t *testing.T) {
		borrowed := make(chan int32, 1)
		store := &workOwnerPageStore{fixtureStore: f.store, afterQuery: func(ctx context.Context, rows *postgres.Rows, pid int32) {
			borrowed <- pid
			<-ctx.Done() // real Rows already exist and stay owned by this call
		}}
		authority, e := work.NewAuthority(store, f.projectAuthority)
		if e != nil {
			t.Fatal(e)
		}
		owned, e := work.NewBlockerReader(store, authority, f.keys)
		if e != nil {
			t.Fatal(e)
		}
		ctx, cancel := context.WithCancel(ctxFor(t))
		defer cancel()
		type reply struct {
			page foundation.Page[wc.TaskBlocker]
			err  error
		}
		done := make(chan reply, 1)
		joined := make(chan struct{})
		t.Cleanup(func() { cancel(); await(t, joined) })
		go func() {
			defer close(joined)
			p, e := owned.ListTaskBlockersPage(ctx, a, p.ID, target.ID, wc.TaskBlockersAll, foundation.PageRequest{Limit: 2})
			done <- reply{p, e}
		}()
		var readerPID int32
		select {
		case readerPID = <-borrowed:
		case <-time.After(5 * time.Second):
			t.Fatal("real Rows were not acquired")
		}
		writer, hook := observedBlockerFixture(t, f)
		attempts := make(chan lockAttempt, 1)
		var once sync.Once
		user, _ := foundation.UserLock(a.Details().UserID)
		hook.beforeLocks = func(ctx context.Context, tx foundation.Tx, locks []foundation.LockRequest) error {
			for _, lock := range locks {
				if foundation.CompareLockKeys(lock.Key, user) == 0 && lock.Mode == foundation.Exclusive {
					x, e := hook.fixtureStore.InTx(tx)
					if e != nil {
						return e
					}
					var pid int32
					if e = x.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&pid); e != nil {
						return e
					}
					once.Do(func() { attempts <- lockAttempt{BackendPID: pid, Request: lock} })
				}
			}
			return nil
		}
		request := blockerWaiting(t, "writer after pager Rows")
		command := meta(t, "page-after-rows", &target.Version)
		writes := callBlockerAsync(t, func(ctx context.Context) (wc.TaskBlockerMutation, error) {
			return writer.blockers.AddTaskBlocker(ctx, a, command, p.ID, target.ID, request)
		})
		attempt := awaitBlockerLockAttempt(t, attempts, foundation.Exclusive)
		waitExactLock(t, f.db, attempt, false, readerPID)
		cancel()
		select {
		case result := <-done:
			if !errors.Is(result.err, context.Canceled) || result.page.Items != nil || result.page.NextCursor != "" {
				t.Fatal("cancelled Rows returned partial success", result.err)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("pager Rows/Tx did not actually join")
		}
		result := joinBlockerReply(t, writes)
		if result.err != nil {
			t.Fatal("writer still held after pager join", result.err)
		}
		target = result.result.Task
	})

	t.Run("tie-keyset-and-off-page-corruption", func(t *testing.T) {
		// Only this isolated query fixture changes timestamps, under full locks
		// with the exact immutable trigger disabled and restored in one Tx.
		// FK/CHECK remain enabled. This is not a producer/history/replay proof.
		at := values[0].CreatedAt.Time()
		workOwnerPageFixtureEdit(t, f, a, p.ID, target.ID, func(ctx context.Context, x postgres.SQLExecutor) error {
			_, e := x.Exec(ctx, `UPDATE agenteam_work.task_blockers SET created_at=$3 WHERE project_id=$1 AND task_id=$2`, p.ID.String(), target.ID.String(), at)
			return e
		})
		expected := read(a, wc.TaskBlockersAll, 200, "").Items
		slices.SortFunc(expected, func(a, b wc.TaskBlocker) int { return strings.Compare(a.ID.String(), b.ID.String()) })
		var found []wc.TaskBlocker
		for token, limit := "", 1; ; limit = 3 {
			page := read(a, wc.TaskBlockersAll, limit, token)
			found = append(found, page.Items...)
			if page.NextCursor == "" {
				break
			}
			token = page.NextCursor
			if len(found) > len(expected) {
				t.Fatal("keyset did not advance")
			}
		}
		if len(found) != len(expected) {
			t.Fatal("same timestamp rows omitted")
		}
		for n := range expected {
			if found[n].ID != expected[n].ID || !found[n].CreatedAt.Time().Equal(at) {
				t.Fatal("tie-break order")
			}
		}
		var bad wc.TaskBlocker
		for _, v := range expected {
			if v.ResolvedAt == nil {
				bad = v
			}
		}
		workOwnerPageFixtureEdit(t, f, a, p.ID, target.ID, func(ctx context.Context, x postgres.SQLExecutor) error {
			_, e := x.Exec(ctx, `UPDATE agenteam_work.task_blockers SET created_at='10000-01-01 00:00:00+00'::timestamptz WHERE id=$1`, bad.ID.String())
			return e
		})
		// This valid PostgreSQL timestamp is outside the public Instant range.
		// A pager that eagerly decodes all retained rows would fail this page.
		read(a, wc.TaskBlockersAll, 1, "")
		page, e := reader.ListTaskBlockersPage(ctxFor(t), a, p.ID, target.ID, wc.TaskBlockersAll, foundation.PageRequest{Limit: 200})
		assertFailure(page, e, foundation.InternalError)
		workOwnerPageFixtureEdit(t, f, a, p.ID, target.ID, func(ctx context.Context, x postgres.SQLExecutor) error {
			_, e := x.Exec(ctx, `UPDATE agenteam_work.task_blockers SET created_at=$2 WHERE id=$1`, bad.ID.String(), at)
			return e
		})
		t.Log("timestamp tie/corruption are explicit query fixtures, not production mutable Blockers")
	})

	t.Run("current-authority-before-cursor-and-lifecycle", func(t *testing.T) {
		current := read(a, wc.TaskBlockersAll, 1, "")
		fresh := f.renew(t, a)
		user, _ := foundation.UserLock(a.Details().UserID)
		f.tx(t, []foundation.LockRequest{{Key: user, Mode: foundation.Exclusive}}, func(ctx context.Context, _ foundation.Tx, x postgres.SQLExecutor) error {
			_, e := x.Exec(ctx, `UPDATE agenteam_account.sessions SET revoked_at=clock_timestamp(),revoked_reason='administrative' WHERE id=$1`, a.Details().SessionID)
			return e
		})
		page, e := reader.ListTaskBlockersPage(ctxFor(t), a, p.ID, target.ID, wc.TaskBlockersAll, foundation.PageRequest{Limit: 2, Cursor: "bad-signature"})
		assertFailure(page, e, foundation.SessionRevoked)
		a = fresh
		read(a, wc.TaskBlockersAll, 2, current.NextCursor)
		f.transferOwner(t, a, other, p.ID)
		page, e = reader.ListTaskBlockersPage(ctxFor(t), a, p.ID, target.ID, wc.TaskBlockersAll, foundation.PageRequest{Limit: 2, Cursor: current.NextCursor})
		assertFailure(page, e, foundation.NotFound)
		page, e = reader.ListTaskBlockersPage(ctxFor(t), other, p.ID, target.ID, wc.TaskBlockersAll, foundation.PageRequest{Limit: 2, Cursor: current.NextCursor})
		assertFailure(page, e, foundation.CursorInvalid)
		f.transferOwner(t, other, a, p.ID)
		taskSeedArchivedProject(t, f.taskFixture, a, p.ID)
		read(a, wc.TaskBlockersAll, 2, current.NextCursor)
		f.skills.setMode("pending")
		pendingID := id[identity.Project](t)
		pending, e := f.projects.CreateProject(ctxFor(t), a, meta(t, "page-pending", nil), pc.CreateProjectRequest{ProjectID: pendingID, Name: "page-pending"})
		f.skills.setMode("")
		if e != nil || pending.State == pc.CreationReady {
			t.Fatal("pending initialization fixture", e)
		}
		page, e = reader.ListTaskBlockersPage(ctxFor(t), a, pendingID, target.ID, wc.TaskBlockersAll, foundation.PageRequest{Limit: 2, Cursor: "bad-signature"})
		assertFailure(page, e, foundation.ProjectNotActive)
		deleting, _, _ := f.create(t, a, "page-deleting")
		f.seedProjectTransition(t, a, deleting.ID, pc.Deleting)
		page, e = reader.ListTaskBlockersPage(ctxFor(t), a, deleting.ID, target.ID, wc.TaskBlockersAll, foundation.PageRequest{Limit: 2, Cursor: "bad-signature"})
		assertFailure(page, e, foundation.ProjectNotActive)
	})
}

func workOwnerPageFixtureEdit(t *testing.T, f *blockerFixture, a identity.Actor, p pc.ProjectID, task wc.TaskID, edit func(context.Context, postgres.SQLExecutor) error) {
	t.Helper()
	schedule, _ := foundation.ProjectScheduleLock(p.String())
	target, _ := foundation.AggregateLock(foundation.TaskAggregate, task.String())
	f.tx(t, fixtureLocks(a, p, foundation.LockRequest{Key: schedule, Mode: foundation.Exclusive}, foundation.LockRequest{Key: target, Mode: foundation.Exclusive}), func(ctx context.Context, tx foundation.Tx, x postgres.SQLExecutor) error {
		if _, e := f.projectAuthority.RequireOwnerInTx(ctx, tx, a, p, identity.Mutate); e != nil {
			return e
		}
		if _, e := x.Exec(ctx, `ALTER TABLE agenteam_work.task_blockers DISABLE TRIGGER task_blockers_immutable`); e != nil {
			return e
		}
		if e := edit(ctx, x); e != nil {
			return e
		}
		_, e := x.Exec(ctx, `ALTER TABLE agenteam_work.task_blockers ENABLE TRIGGER task_blockers_immutable`)
		return e
	})
}

type workOwnerPageStore struct {
	fixtureStore
	afterQuery func(context.Context, *postgres.Rows, int32)
}

func (s *workOwnerPageStore) InTx(tx foundation.Tx) (postgres.SQLExecutor, error) {
	x, e := s.fixtureStore.InTx(tx)
	if e != nil {
		return nil, e
	}
	return workOwnerPageExecutor{x, s.afterQuery}, nil
}

type workOwnerPageExecutor struct {
	postgres.SQLExecutor
	afterQuery func(context.Context, *postgres.Rows, int32)
}

func (x workOwnerPageExecutor) Query(ctx context.Context, query string, args ...any) (*postgres.Rows, error) {
	var pid int32
	if e := x.SQLExecutor.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&pid); e != nil {
		return nil, e
	}
	rows, e := x.SQLExecutor.Query(ctx, query, args...)
	if e == nil && strings.Contains(query, "FROM agenteam_work.task_blockers WHERE") {
		x.afterQuery(ctx, rows, pid)
	}
	return rows, e
}
