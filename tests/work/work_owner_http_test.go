//go:build integration

package work_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/account"
	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
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
		// Discovery takes Schedule EX before the later User EX mutation Tx.
		// The pager holds Schedule SH while owning its actual Rows.
		schedule, _ := foundation.ProjectScheduleLock(p.ID.String())
		hook.beforeLocks = func(ctx context.Context, tx foundation.Tx, locks []foundation.LockRequest) error {
			for _, lock := range locks {
				if foundation.CompareLockKeys(lock.Key, schedule) == 0 && lock.Mode == foundation.Exclusive {
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
		var attempt lockAttempt
		select {
		case attempt = <-attempts:
			if attempt.BackendPID <= 0 || attempt.Request.Mode != foundation.Exclusive || foundation.CompareLockKeys(attempt.Request.Key, schedule) != 0 {
				t.Fatal("writer did not request its real Schedule EX")
			}
		case early := <-writes:
			var fault *foundation.Fault
			if errors.As(early.err, &fault) {
				t.Fatal("writer returned before Schedule observation", fault)
			}
			t.Fatal("writer returned before Schedule observation without a domain Fault")
		case <-time.After(5 * time.Second):
			t.Fatal("writer Schedule EX was not observed")
		}
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

// These HTTP tops use formal Account Bootstrap/Invitation/Redeem/Login. The
// recorder is solely a PG business boundary; native transport and root Join
// have their own actual-socket tests, never inferred from these responses.
func TestWorkOwnerHTTPAuthorityAndPersistence(t *testing.T) {
	v := newWorkOwnerHTTPFixture(t)
	var originals []workOwnerHTTPIntent
	var receipts [][]byte
	t.Run("owner-facts-and-current-identity", func(t *testing.T) {
		// Age only this legitimately issued Session to cross Account's existing
		// 60-second Activity throttle. This is not a fabricated login capability.
		user, _ := foundation.UserLock(v.ownerBrowser.actor.Details().UserID)
		v.tx(t, []foundation.LockRequest{{Key: user, Mode: foundation.Exclusive}}, func(ctx context.Context, tx foundation.Tx, x postgres.SQLExecutor) error {
			_, e := x.Exec(ctx, `UPDATE agenteam_account.sessions SET issued_at=clock_timestamp()-interval '2 minutes',last_activity_at=clock_timestamp()-interval '90 seconds' WHERE id=$1`, v.ownerBrowser.actor.Details().SessionID)
			return e
		})
		var activityBefore time.Time
		if e := v.raw.QueryRow(ctxFor(t), `SELECT last_activity_at FROM agenteam_account.sessions WHERE id=$1`, v.ownerBrowser.actor.Details().SessionID).Scan(&activityBefore); e != nil {
			t.Fatal(e)
		}
		for _, domain := range []string{"structure", "task", "blocker"} {
			i := v.intent(t, domain, v.project.ID)
			// Setup commands can also touch Activity; age immediately before the
			// actual HTTP command whose atomic persistence is under examination.
			v.tx(t, []foundation.LockRequest{{Key: user, Mode: foundation.Exclusive}}, func(ctx context.Context, tx foundation.Tx, x postgres.SQLExecutor) error {
				_, e := x.Exec(ctx, `UPDATE agenteam_account.sessions SET last_activity_at=$2 WHERE id=$1`, v.ownerBrowser.actor.Details().SessionID, activityBefore)
				return e
			})
			oldEvents := v.eventCount(t)
			r := v.send(t, v.ownerBrowser, i)
			receipt := workOwnerHTTPReceipt(t, i, r, false)
			workOwnerHTTPPersistentReceipt(t, v, i, receipt)
			if v.eventCount(t) != oldEvents+1 {
				t.Fatal("HTTP command did not append exactly one Work event")
			}
			var after time.Time
			if e := v.raw.QueryRow(ctxFor(t), `SELECT last_activity_at FROM agenteam_account.sessions WHERE id=$1`, v.ownerBrowser.actor.Details().SessionID).Scan(&after); e != nil || !after.After(activityBefore) {
				t.Fatal("HTTP commit omitted real Account Activity", e)
			}
			originals = append(originals, i)
			receipts = append(receipts, receipt)
			before := v.httpSnapshot(t)
			for _, browser := range []workOwnerHTTPBrowser{v.otherBrowser, v.adminBrowser, {}} {
				want := foundation.NotFound
				if browser.cookie == "" {
					want = foundation.Unauthenticated
				}
				workOwnerHTTPRequireProblem(t, v.send(t, browser, i), want)
				workOwnerHTTPRequireProblem(t, v.lookup(t, browser, i), want)
				workOwnerHTTPRequireProblem(t, v.request(t, browser, "GET", workOwnerHTTPReadPath(i), "", ""), want)
			}
			if v.httpSnapshot(t) != before {
				t.Fatal("denied caller changed Work facts")
			}
		}
	})
	t.Run("cross-project-parent", func(t *testing.T) {
		other, _, _ := v.create(t, v.ownerBrowser.actor, "http-other-parent")
		for _, i := range originals {
			q := i
			q.path = strings.Replace(i.path, i.project.String(), other.ID.String(), 1)
			q.project = other.ID
			q.key = foundation.IdempotencyKey(id[struct{}](t).String())
			want := foundation.NotFound
			if i.domain != "structure" {
				want = foundation.TaskNotFound
			}
			workOwnerHTTPRequireProblem(t, v.send(t, v.ownerBrowser, q), want)
			read := strings.Replace(workOwnerHTTPReadPath(i), i.project.String(), other.ID.String(), 1)
			workOwnerHTTPRequireProblem(t, v.request(t, v.ownerBrowser, "GET", read, "", ""), want)
		}
	})
	t.Run("revoked-and-expired-real-session", func(t *testing.T) {
		revoked := v.login(t, v.ownerBrowser.email)
		if e := v.core.Logout(ctxFor(t), account.LogoutRequest{Actor: revoked.actor, Key: foundation.IdempotencyKey(id[struct{}](t).String())}); e != nil {
			t.Fatal(e)
		}
		expired := v.login(t, v.ownerBrowser.email)
		user, _ := foundation.UserLock(expired.actor.Details().UserID)
		v.tx(t, []foundation.LockRequest{{Key: user, Mode: foundation.Exclusive}}, func(ctx context.Context, tx foundation.Tx, x postgres.SQLExecutor) error {
			_, e := x.Exec(ctx, `UPDATE agenteam_account.sessions SET issued_at=clock_timestamp()-interval '2 hours',last_activity_at=clock_timestamp()-interval '1 hour',absolute_expires_at=clock_timestamp()-interval '1 second' WHERE id=$1`, expired.actor.Details().SessionID)
			return e
		})
		for _, i := range originals {
			for _, sample := range []struct {
				b    workOwnerHTTPBrowser
				code foundation.Code
			}{{revoked, foundation.SessionRevoked}, {expired, foundation.Unauthenticated}} {
				workOwnerHTTPRequireProblem(t, v.send(t, sample.b, i), sample.code)
				workOwnerHTTPRequireProblem(t, v.lookup(t, sample.b, i), sample.code)
				workOwnerHTTPRequireProblem(t, v.request(t, sample.b, "GET", workOwnerHTTPReadPath(i), "", ""), sample.code)
			}
		}
	})
	for _, stage := range []string{"after-http-auth", "after-real-prepare"} {
		for _, change := range []string{"logout", "owner", "archive"} {
			t.Run(stage+"/"+change, func(t *testing.T) {
				p, _, _ := v.create(t, v.ownerBrowser.actor, "race-"+id[struct{}](t).String()[24:])
				i := v.intent(t, "blocker", p.ID)
				browser := v.login(t, v.ownerBrowser.email)
				gate := newBlockerInteropGate(t)
				defer gate.free()
				r := workOwnerHTTPRequest(ctxFor(t), browser, i.method, i.path, i.body, i.key)
				if stage == "after-http-auth" {
					r.Body = &workOwnerHTTPGatedBody{Reader: strings.NewReader(i.body), gate: gate, ctx: r.Context()}
				} else {
					v.bindAppender(t, &capturingAppender{Appender: v.blockerOutbox, after: func(ctx context.Context, _ identity.Actor, _ event.Event, _ oc.AppendPlan) error {
						return gate.wait(ctx)
					}})
				}
				done := workOwnerHTTPAsync(t, v, r)
				select {
				case <-gate.reached:
				case early := <-done:
					t.Fatalf("HTTP returned before %s status=%d", stage, early.status)
				case <-time.After(5 * time.Second):
					t.Fatal("real HTTP barrier not reached")
				}
				if stage == "after-real-prepare" && v.commandState(t, i) != "planned" {
					t.Fatal("Prepare barrier lacks durable planned command")
				}
				before := v.httpSnapshot(t)
				want := foundation.NotFound
				switch change {
				case "logout":
					want = foundation.SessionRevoked
					if e := v.core.Logout(ctxFor(t), account.LogoutRequest{Actor: browser.actor, Key: foundation.IdempotencyKey(id[struct{}](t).String())}); e != nil {
						t.Fatal(e)
					}
				case "owner":
					v.transferOwner(t, v.ownerBrowser.actor, v.otherBrowser.actor, p.ID)
				case "archive":
					want = foundation.ProjectNotActive
					op, e := v.projects.BeginArchive(ctxFor(t), v.ownerBrowser.actor, meta(t, id[struct{}](t).String(), &p.Version), p.ID)
					if e != nil || op.State != pc.OperationAccepted {
						t.Fatal("real archive acceptance", e)
					}
				}
				gate.free()
				workOwnerHTTPRequireProblem(t, workOwnerHTTPAwait(t, done), want)
				if v.httpSnapshot(t) != before {
					t.Fatal("changed current authority bypassed final Work gate")
				}
				if stage == "after-real-prepare" {
					v.restoreHandler(t)
				}
			})
		}
	}
	t.Run("archived-history-and-unfinished-command", func(t *testing.T) {
		// Only a canonical archived gate input is supplied: no absent participant
		// is represented as successfully stopped or cleaned by this fixture.
		var unfinished []workOwnerHTTPIntent
		for _, domain := range []string{"structure", "task", "blocker"} {
			i := v.intent(t, domain, v.project.ID)
			v.bindAppender(t, &capturingAppender{Appender: v.blockerOutbox, after: func(context.Context, identity.Actor, event.Event, oc.AppendPlan) error {
				return foundation.NewFault(foundation.DependencyUnavailable, foundation.NotStarted)
			}})
			workOwnerHTTPRequireProblem(t, v.send(t, v.ownerBrowser, i), foundation.DependencyUnavailable)
			if v.commandState(t, i) != "planned" {
				t.Fatal("planned intent missing")
			}
			unfinished = append(unfinished, i)
		}
		v.restoreHandler(t)
		v.seedLifecycle(t, v.ownerBrowser.actor, v.project.ID, pc.Archived)
		before := v.httpSnapshot(t)
		for n, i := range originals {
			workOwnerHTTPRequireOK(t, v.request(t, v.ownerBrowser, "GET", workOwnerHTTPReadPath(i), "", ""))
			if !bytes.Equal(workOwnerHTTPReceipt(t, i, v.lookup(t, v.ownerBrowser, i), true), receipts[n]) || !bytes.Equal(workOwnerHTTPReceipt(t, i, v.send(t, v.ownerBrowser, i), false), receipts[n]) {
				t.Fatal("archived completed history changed")
			}
			q := i
			q.key = foundation.IdempotencyKey(id[struct{}](t).String())
			workOwnerHTTPRequireProblem(t, v.send(t, v.ownerBrowser, q), foundation.ProjectNotActive)
		}
		for _, i := range unfinished {
			workOwnerHTTPRequireProblem(t, v.send(t, v.ownerBrowser, i), foundation.ProjectNotActive)
			workOwnerHTTPLookupState(t, i, v.lookup(t, v.ownerBrowser, i), wc.LookupInProgress)
		}
		if v.httpSnapshot(t) != before {
			t.Fatal("archived read/replay/new-write rejection changed Work facts")
		}
	})
	t.Run("pending-and-real-deleting-gates", func(t *testing.T) {
		v.skills.setMode("pending")
		pending := id[identity.Project](t)
		result, e := v.projects.CreateProject(ctxFor(t), v.ownerBrowser.actor, meta(t, id[struct{}](t).String(), nil), pc.CreateProjectRequest{ProjectID: pending, Name: "http-pending"})
		v.skills.setMode("")
		if e != nil || result.State == pc.CreationReady || result.Operation == nil {
			t.Fatal("real pending initialization", e)
		}
		deleting, _, _ := v.create(t, v.ownerBrowser.actor, "http-delete")
		var deletedIntents []workOwnerHTTPIntent
		for _, domain := range []string{"structure", "task", "blocker"} {
			i := v.intent(t, domain, deleting.ID)
			workOwnerHTTPRequireOK(t, v.send(t, v.ownerBrowser, i))
			deletedIntents = append(deletedIntents, i)
		}
		path := strings.Split(v.ownerBrowser.email, "@")[0] + "/" + deleting.NormalizedName
		deleted, e := v.projects.BeginDeleteProject(ctxFor(t), v.ownerBrowser.actor, meta(t, id[struct{}](t).String(), &deleting.Version), deleting.ID, pc.DeleteProjectRequest{NormalizedCurrentPath: path, Permanent: true})
		if e != nil || deleted.Operation == nil || deleted.Operation.State != pc.OperationAccepted {
			t.Fatal("real delete acceptance", e)
		}
		before := v.httpSnapshot(t)
		for _, project := range []pc.ProjectID{pending, deleting.ID} {
			for _, base := range deletedIntents {
				i := base
				i.project = project
				i.path = strings.Replace(base.path, deleting.ID.String(), project.String(), 1)
				i.lookupPath = strings.Replace(base.lookupPath, deleting.ID.String(), project.String(), 1)
				workOwnerHTTPRequireProblem(t, v.send(t, v.ownerBrowser, i), foundation.ProjectNotActive)
				workOwnerHTTPRequireProblem(t, v.lookup(t, v.ownerBrowser, i), foundation.ProjectNotActive)
				workOwnerHTTPRequireProblem(t, v.request(t, v.ownerBrowser, "GET", strings.Replace(workOwnerHTTPReadPath(base), deleting.ID.String(), project.String(), 1), "", ""), foundation.ProjectNotActive)
			}
		}
		if v.httpSnapshot(t) != before {
			t.Fatal("pending/deleting gate mutated Work")
		}
	})
}

func workOwnerHTTPReadPath(i workOwnerHTTPIntent) string {
	if i.domain == "structure" {
		return workOwnerHTTPPath(i.project, "/milestones/"+i.milestone.ID.String())
	}
	if i.domain == "task" {
		return workOwnerHTTPPath(i.project, "/tasks/"+i.task.ID.String())
	}
	return i.path
}
func workOwnerHTTPPersistentReceipt(t *testing.T, v *workOwnerHTTPFixture, i workOwnerHTTPIntent, receipt []byte) {
	t.Helper()
	var raw []byte
	if e := v.raw.QueryRow(ctxFor(t), `SELECT receipt FROM agenteam_work.`+i.table+` WHERE project_id=$1 AND command_name=$2 AND idempotency_key=$3 AND state='completed'`, i.project.String(), i.command, string(i.key)).Scan(&raw); e != nil {
		t.Fatal("durable HTTP receipt", e)
	}
	var actual, want any
	if json.Unmarshal(raw, &actual) != nil || json.Unmarshal(receipt, &want) != nil || !bytes.Equal(jsonBytes(t, actual), jsonBytes(t, want)) {
		t.Fatal("HTTP result is not its historical receipt")
	}
	var fields struct {
		EventIDs    []string `json:"event_ids"`
		EventID     string   `json:"event_id"`
		TaskEventID string   `json:"task_event_id"`
	}
	if json.Unmarshal(receipt, &fields) != nil {
		t.Fatal("receipt event projection")
	}
	if i.domain == "structure" {
		fields.EventIDs = []string{fields.EventID}
	}
	if len(fields.EventIDs) != 1 {
		t.Fatal("receipt has no unique Outbox event")
	}
	var count int
	if e := v.raw.QueryRow(ctxFor(t), `SELECT count(*) FROM agenteam_outbox.events WHERE id=$1 AND producer='work' AND project_id=$2`, fields.EventIDs[0], i.project.String()).Scan(&count); e != nil || count != 1 {
		t.Fatal("same-fact Work event missing", e)
	}
	if i.domain != "structure" {
		if e := v.raw.QueryRow(ctxFor(t), `SELECT count(*) FROM agenteam_work.task_events WHERE id=$1 AND task_id=$2 AND project_id=$3`, fields.TaskEventID, i.task.ID.String(), i.project.String()).Scan(&count); e != nil || count != 1 {
			t.Fatal("same-fact typed Task history missing", e)
		}
	}
}

func TestWorkOwnerHTTPIntentRecovery(t *testing.T) {
	v := newWorkOwnerHTTPFixture(t)
	for _, domain := range []string{"structure", "task", "blocker"} {
		t.Run(domain+"/lost-response-and-historical-original-intent", func(t *testing.T) {
			i := v.intent(t, domain, v.project.ID)
			workOwnerHTTPLookupState(t, i, v.lookup(t, v.ownerBrowser, i), wc.LookupNotObserved)
			beforeEvents := v.eventCount(t)
			v.loseResponse(t, i)
			if v.commandState(t, i) != "completed" || v.eventCount(t) != beforeEvents+1 {
				t.Fatal("lost HTTP response was not really committed once")
			}
			fresh := v.login(t, v.ownerBrowser.email)
			if fresh.actor.Details().UserID != v.ownerBrowser.actor.Details().UserID || fresh.actor.Details().SessionID == v.ownerBrowser.actor.Details().SessionID {
				t.Fatal("recovery did not use a new formal Session")
			}
			original := workOwnerHTTPReceipt(t, i, v.lookup(t, fresh, i), true)
			workOwnerHTTPPersistentReceipt(t, v, i, original)
			currentPath := i.path
			method := "PATCH"
			request := any(wc.UpdateFields{Title: workOwnerHTTPString("later milestone")})
			switch domain {
			case "task":
				request = wc.TaskFieldsUpdate{Title: workOwnerHTTPString("later task")}
			case "blocker":
				currentPath += "/resolve"
				method = "POST"
				request = wc.TaskBlockerResolve{BlockerID: i.blocker.BlockerID, ResolutionComment: workOwnerHTTPString("later resolution")}
			}
			body := string(jsonBytes(t, map[string]any{"expected_version": foundation.Version(2), "request": request}))
			workOwnerHTTPRequireOK(t, v.request(t, fresh, method, currentPath, body, foundation.IdempotencyKey(id[struct{}](t).String())))
			readPath := workOwnerHTTPReadPath(i)
			if domain == "blocker" {
				readPath += "?status=resolved"
			}
			current := v.request(t, fresh, "GET", readPath, "", "")
			workOwnerHTTPRequireOK(t, current)
			expectedCurrent := []byte("later milestone")
			if domain == "task" {
				expectedCurrent = []byte("later task")
			}
			if domain == "blocker" {
				expectedCurrent = []byte("later resolution")
			}
			if !bytes.Contains(current.body, expectedCurrent) {
				t.Fatal("current read never observed later content")
			}
			stable := v.httpSnapshot(t)
			if !bytes.Equal(original, workOwnerHTTPReceipt(t, i, v.lookup(t, fresh, i), true)) || !bytes.Equal(original, workOwnerHTTPReceipt(t, i, v.send(t, fresh, i), false)) {
				t.Fatal("original receipt replaced with current entity")
			}
			changed := i
			changed.body = strings.ReplaceAll(i.body, "private changed", "other semantics")
			changed.lookupBody = strings.ReplaceAll(i.lookupBody, "private changed", "other semantics")
			if changed.body == i.body {
				t.Fatal("changed-intent stimulus did not change original")
			}
			workOwnerHTTPRequireProblem(t, v.send(t, fresh, changed), foundation.IdempotencyKeyReused)
			workOwnerHTTPRequireProblem(t, v.lookup(t, fresh, changed), foundation.IdempotencyKeyReused)
			if v.httpSnapshot(t) != stable {
				t.Fatal("lookup/replay/conflict emitted a second fact")
			}
			logs := v.logs.text()
			for _, private := range []string{v.ownerBrowser.cookie, v.ownerBrowser.csrf, fresh.cookie, fresh.csrf, "private changed", "later resolution", string(i.key)} {
				if strings.Contains(logs, private) {
					t.Fatal("HTTP logs exposed original intent or identity material")
				}
			}
		})
		t.Run(domain+"/writer-in-flight-is-not-absence", func(t *testing.T) {
			i := v.intent(t, domain, v.project.ID)
			// Preauthenticate Lookup before the writer takes User EX; the body gate
			// then releases it directly into the real command-lock transaction.
			lookupGate := newBlockerInteropGate(t)
			defer lookupGate.free()
			lookupCtx, cancelLookup := context.WithCancel(ctxFor(t))
			defer cancelLookup()
			req := workOwnerHTTPRequest(lookupCtx, v.ownerBrowser, "POST", i.lookupPath, i.lookupBody, i.key)
			req.Body = &workOwnerHTTPGatedBody{Reader: strings.NewReader(i.lookupBody), gate: lookupGate, ctx: lookupCtx}
			looked := workOwnerHTTPAsync(t, v, req)
			await(t, lookupGate.reached)
			final := newBlockerInteropGate(t)
			defer final.free()
			var pid atomic.Int32
			var armed atomic.Bool
			v.tracked.setAfter(func(ctx context.Context, tx foundation.Tx, cause foundation.TransactionCause) error {
				if cause.Kind() != foundation.CommandsCause || cause.Details().Primary.Canonical() != i.identity.Canonical() || armed.Load() {
					return nil
				}
				x, e := v.tracked.InTx(tx)
				if e != nil {
					return e
				}
				var state string
				if e = x.QueryRow(ctx, `SELECT coalesce((SELECT state FROM agenteam_work.`+i.table+` WHERE project_id=$1 AND command_name=$2 AND idempotency_key=$3),'')`, i.project.String(), i.command, string(i.key)).Scan(&state); e != nil {
					return e
				}
				if state == "completed" && armed.CompareAndSwap(false, true) {
					var backend int32
					if e = x.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&backend); e != nil {
						return e
					}
					pid.Store(backend)
					return final.wait(ctx)
				}
				return nil
			})
			defer v.tracked.setAfter(nil)
			writer := workOwnerHTTPAsync(t, v, workOwnerHTTPRequest(ctxFor(t), v.ownerBrowser, i.method, i.path, i.body, i.key))
			select {
			case <-final.reached:
			case r := <-writer:
				t.Fatalf("writer escaped final barrier status=%d", r.status)
			case <-time.After(5 * time.Second):
				t.Fatal("writer did not reach real completed transaction")
			}
			key, _ := foundation.CommandLock(i.identity)
			observed := observeLock(v.tracked, key, nil)
			lookupGate.free()
			attempt := awaitLockAttempt(t, observed)
			waitExactLock(t, v.db, attempt, false, pid.Load())
			cancelLookup()
			r := workOwnerHTTPAwait(t, looked)
			if !r.aborted || len(r.body) != 0 {
				t.Fatal("cancelled in-flight HTTP Lookup published a result or false absence")
			}
			final.free()
			receipt := workOwnerHTTPReceipt(t, i, workOwnerHTTPAwait(t, writer), false)
			v.tracked.setAfter(nil)
			if !bytes.Equal(receipt, workOwnerHTTPReceipt(t, i, v.lookup(t, v.ownerBrowser, i), true)) {
				t.Fatal("joined writer receipt not subsequently discoverable")
			}
		})
	}
}

func TestWorkOwnerHTTPUnknown(t *testing.T) {
	for _, domain := range []string{"structure", "task", "blocker"} {
		for _, forwarded := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/final-commit-forwarded=%t", domain, forwarded), func(t *testing.T) { workOwnerHTTPUnknownCase(t, domain, "completed", forwarded) })
		}
	}
	// The HTTP not_observed arm needs a genuinely absent planned COMMIT too;
	// this does not fabricate Store terminals or infer rollback from a timeout.
	t.Run("structure/planning-commit-unforwarded", func(t *testing.T) { workOwnerHTTPUnknownCase(t, "structure", "planned", false) })
}

func workOwnerHTTPUnknownCase(t *testing.T, domain, phase string, forwarded bool) {
	t.Helper()
	v, proxy := newWorkOwnerHTTPProxyFixture(t, forwarded)
	i := v.intent(t, domain, v.project.ID)
	key, _ := foundation.CommandLock(i.identity)
	confirmationAttempts := observeLock(v.tracked, key, func() bool {
		select {
		case <-proxy.reached:
			return true
		default:
			return false
		}
	})
	var armed atomic.Bool
	var backend atomic.Int32
	originalResults := make(chan foundation.CommitResult, 1)
	v.tracked.setAfter(func(ctx context.Context, tx foundation.Tx, cause foundation.TransactionCause) error {
		if cause.Kind() != foundation.CommandsCause || cause.Details().Primary.Canonical() != i.identity.Canonical() || armed.Load() {
			return nil
		}
		x, e := v.tracked.InTx(tx)
		if e != nil {
			return e
		}
		var state string
		if e = x.QueryRow(ctx, `SELECT coalesce((SELECT state FROM agenteam_work.`+i.table+` WHERE project_id=$1 AND command_name=$2 AND idempotency_key=$3),'')`, i.project.String(), i.command, string(i.key)).Scan(&state); e != nil {
			return e
		}
		if state == phase && armed.CompareAndSwap(false, true) {
			var pid int32
			if e = x.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&pid); e != nil {
				return e
			}
			backend.Store(pid)
			proxy.targetPID.Store(pid)
		}
		return nil
	})
	v.tracked.mu.Lock()
	v.tracked.afterResult = func(cause foundation.TransactionCause, result foundation.CommitResult) {
		if cause.Kind() == foundation.CommandsCause && cause.Details().Primary.Canonical() == i.identity.Canonical() && result.State() == foundation.Unknown {
			select {
			case originalResults <- result:
			default:
			}
		}
	}
	v.tracked.mu.Unlock()
	var released sync.Once
	release := func() { released.Do(func() { close(proxy.release) }) }
	defer release()
	before := v.httpSnapshot(t)
	eventsBefore := v.eventCount(t)
	writer := workOwnerHTTPAsync(t, v, workOwnerHTTPRequest(ctxFor(t), v.ownerBrowser, i.method, i.path, i.body, i.key))
	select {
	case <-proxy.reached:
	case r := <-writer:
		t.Fatalf("HTTP returned before physical COMMIT proxy status=%d", r.status)
	case <-time.After(5 * time.Second):
		t.Fatal("physical COMMIT not captured")
	}
	if backend.Load() <= 0 || proxy.backendPID.Load() != backend.Load() {
		t.Fatal("proxy did not bind exact physical writer")
	}
	confirmation := awaitLockAttempt(t, confirmationAttempts)
	waitExactLock(t, v.db, confirmation, false, backend.Load())
	response := workOwnerHTTPAwait(t, writer)
	problem := workOwnerHTTPRequireProblem(t, response, foundation.CommitUnknown)
	if problem.CommitState != foundation.Unknown || problem.RetryHint != "lookup" {
		t.Fatal("HTTP rewrote original Unknown or recovery hint")
	}
	select {
	case original := <-originalResults:
		if original.AttemptID().Validate() != nil || original.Cause().Details().Primary.Canonical() != i.identity.Canonical() {
			t.Fatal("Unknown lost actual attempt/cause")
		}
	default:
		t.Fatal("no real Store Unknown terminal")
	}
	if v.httpSnapshot(t) != before {
		t.Fatal("held physical COMMIT exposed a durable Work fact")
	}
	release()
	await(t, proxy.completed)
	fresh := v.login(t, v.ownerBrowser.email)
	state := wc.LookupInProgress
	if forwarded {
		state = wc.LookupCommitted
	}
	if phase == "planned" && !forwarded {
		state = wc.LookupNotObserved
	}
	observed := v.lookup(t, fresh, i)
	workOwnerHTTPLookupState(t, i, observed, state)
	// Only this explicit caller action retries the exact saved original. Lookup
	// never sends the write and the handler contains no implicit second attempt.
	receipt := workOwnerHTTPReceipt(t, i, v.send(t, fresh, i), false)
	if state == wc.LookupCommitted && !bytes.Equal(receipt, workOwnerHTTPReceipt(t, i, observed, true)) {
		t.Fatal("late committed result replaced original receipt")
	}
	workOwnerHTTPPersistentReceipt(t, v, i, receipt)
	if v.eventCount(t) != eventsBefore+1 {
		t.Fatal("explicit Unknown recovery did not commit exactly one business event")
	}
	stable := v.httpSnapshot(t)
	if !bytes.Equal(receipt, workOwnerHTTPReceipt(t, i, v.lookup(t, fresh, i), true)) || !bytes.Equal(receipt, workOwnerHTTPReceipt(t, i, v.send(t, fresh, i), false)) || v.httpSnapshot(t) != stable {
		t.Fatal("Unknown recovery duplicated durable facts")
	}
	var commands int
	if e := v.raw.QueryRow(ctxFor(t), `SELECT count(*) FROM agenteam_work.`+i.table+` WHERE project_id=$1 AND command_name=$2 AND idempotency_key=$3`, i.project.String(), i.command, string(i.key)).Scan(&commands); e != nil || commands != 1 {
		t.Fatal("recovery replaced original operation identity", e)
	}
}
