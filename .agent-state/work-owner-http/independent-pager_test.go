//go:build integration

package work_test

// This independently exercises the accepted public pagination contract. The
// Account rows and Skills receipt are the accepted old Work fixture boundary;
// this probe does not claim Login, HTTP, or production lifecycle coverage.
import (
	"context"
	"errors"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/audit"
	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/outbox"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/work"
	wc "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
)

type independentPageReply struct {
	page f.Page[wc.TaskBlocker]
	err  error
}

type independentPagePending struct {
	reply <-chan independentPageReply
	done  <-chan struct{}
}

func independentPager(t *testing.T, base *taskFixture) wc.TaskBlockerPageReader {
	t.Helper()
	r, err := work.NewBlockerReader(base.store, base.authority, base.keys)
	if err != nil {
		t.Fatal("public pager construction", err)
	}
	return r
}

func independentPageWriter(t *testing.T, base *taskFixture) *work.BlockerService {
	t.Helper()
	catalog := event.NewCatalog()
	typed, err := wc.RegisterTaskBlockerEvents(catalog)
	if err != nil {
		t.Fatal(err)
	}
	auditor, err := audit.New(base.store, base.keys, audit.Authorizations{Sessions: base.accounts, System: base.accounts, Accounts: base.accounts, Projects: base.projectAuthority})
	if err != nil {
		t.Fatal(err)
	}
	journal, err := outbox.New(base.store, catalog, outbox.Authorizations{Producers: map[event.StableName]oc.ProducerAuthority{wc.WorkProducer: base.authority}, Projects: base.projectAuthority, Sessions: base.accounts, System: base.accounts, Processes: fixtureProcess{id[oc.Process](t)}, Audit: auditor, Cursors: base.keys})
	if err != nil {
		t.Fatal(err)
	}
	service, err := work.NewBlocker(base.store, work.BlockerDependencies{Authority: base.authority, Structure: base.reader, Events: journal, BlockerEvents: typed, Activity: base.accounts})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		service.Stop()
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := service.Drain(ctx); err != nil {
			t.Error("independent pager writer actual Drain", err)
		}
	})
	return service
}

func independentPageSeed(t *testing.T, writer *work.BlockerService, actor i.Actor, task wc.Task, count int) (wc.Task, []wc.TaskBlocker) {
	t.Helper()
	var expected []wc.TaskBlocker
	for n := 0; n < count; n++ {
		request := wc.TaskBlockerCreate{BlockerID: id[wc.TaskBlockerIdentity](t), Type: wc.TaskBlockerWaitingForHuman, Description: strings.Repeat("<\t", 512), Metadata: wc.TaskBlockerMetadata{WaitingForHuman: &wc.TaskBlockerWaitingForHumanMetadata{}}}
		result, err := writer.AddTaskBlocker(ctxFor(t), actor, meta(t, id[struct{}](t).String(), &task.Version), task.ProjectID, task.ID, request)
		if err != nil || result.Validate() != nil {
			t.Fatal("real Blocker seed", err)
		}
		task = result.Task.Clone()
		expected = append(expected, result.Blocker.Clone())
	}
	sort.Slice(expected, func(a, b int) bool {
		if expected[a].CreatedAt.Time().Equal(expected[b].CreatedAt.Time()) {
			return expected[a].ID.String() < expected[b].ID.String()
		}
		return expected[a].CreatedAt.Time().Before(expected[b].CreatedAt.Time())
	})
	return task, expected
}

func independentPageEqual(t *testing.T, got, expected []wc.TaskBlocker) {
	t.Helper()
	if got == nil || len(got) != len(expected) {
		t.Fatal("page must have the expected nonnull item count", len(got), len(expected))
	}
	for n := range expected {
		if string(jsonBytes(t, got[n])) != string(jsonBytes(t, expected[n])) {
			t.Fatal("page disagrees with immutable command receipt oracle", n)
		}
	}
}

func independentPageFailure(t *testing.T, result f.Page[wc.TaskBlocker], err error, code f.Code) {
	t.Helper()
	requireCode(t, err, code)
	if len(result.Items) != 0 || result.NextCursor != "" {
		t.Fatal("failed page exposed a partial candidate")
	}
}

func independentPageStart(t *testing.T, fn func(context.Context) (f.Page[wc.TaskBlocker], error)) (independentPagePending, context.CancelFunc) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	out, done := make(chan independentPageReply, 1), make(chan struct{})
	go func() {
		defer close(done)
		page, err := fn(ctx)
		out <- independentPageReply{page, err}
	}()
	t.Cleanup(func() { cancel(); await(t, done) })
	return independentPagePending{out, done}, cancel
}

func independentPageJoin(t *testing.T, pending independentPagePending) independentPageReply {
	t.Helper()
	select {
	case reply := <-pending.reply:
		await(t, pending.done)
		return reply
	case <-time.After(8 * time.Second):
		t.Fatal("public page call did not actually return")
	}
	return independentPageReply{}
}

// The old helper assumes EX. A reader's SH observation must prove its actual
// caller PID/key/mode instead of being weakened to a generic blocked query.
func independentPageWaitShared(t *testing.T, base *taskFixture, observations <-chan lockAttempt, blocker int32) lockAttempt {
	t.Helper()
	var attempt lockAttempt
	select {
	case attempt = <-observations:
	case <-time.After(5 * time.Second):
		t.Fatal("reader did not reach its real lock union")
	}
	if attempt.BackendPID <= 0 || attempt.Request.Mode != f.Shared {
		t.Fatal("expected the reader's real SharedLock")
	}
	key := uint64(attempt.Request.Key.AdvisoryKey())
	conn := base.db.Connect(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	tick := time.NewTicker(5 * time.Millisecond)
	defer tick.Stop()
	for {
		var waiting bool
		err := conn.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_locks l JOIN pg_stat_activity a ON a.pid=l.pid WHERE a.datname=$1 AND l.pid=$2 AND l.locktype='advisory' AND l.classid::bigint=$3 AND l.objid::bigint=$4 AND l.objsubid=1 AND l.mode='ShareLock' AND NOT l.granted AND $5::int=ANY(pg_blocking_pids(l.pid)))`, base.db.Name, attempt.BackendPID, int64(key>>32), int64(key&0xffffffff), blocker).Scan(&waiting)
		if err != nil {
			t.Fatal("shared caller observation", err)
		}
		if waiting {
			t.Log("exact independent reader wait", "pid", attempt.BackendPID, "key", attempt.Request.Key.AdvisoryKey(), "mode", "ShareLock", "granted", false, "blocker", blocker)
			return attempt
		}
		select {
		case <-ctx.Done():
			t.Fatal("reader never held the expected exact wait")
		case <-tick.C:
		}
	}
}

func independentPageNoLocks(t *testing.T, base *taskFixture, attempt lockAttempt) {
	t.Helper()
	var released bool
	if err := base.raw.QueryRow(ctxFor(t), `SELECT NOT EXISTS(SELECT 1 FROM pg_locks WHERE pid=$1 AND locktype='advisory')`, attempt.BackendPID).Scan(&released); err != nil || !released {
		t.Fatal("returned reader retained an actual advisory lock/wait", err)
	}
}

func independentPageSnapshot(t *testing.T, base *taskFixture, actor i.Actor) string {
	t.Helper()
	var blockerFacts string
	if err := base.raw.QueryRow(ctxFor(t), `SELECT jsonb_build_object(
 'blockers',(SELECT coalesce(jsonb_agg(to_jsonb(b) ORDER BY id),'[]'::jsonb) FROM agenteam_work.task_blockers b),
 'commands',(SELECT coalesce(jsonb_agg(to_jsonb(c) ORDER BY id),'[]'::jsonb) FROM agenteam_work.task_blocker_commands c),
 'events',(SELECT coalesce(jsonb_agg(to_jsonb(e) ORDER BY id),'[]'::jsonb) FROM agenteam_outbox.events e WHERE event_type='work.task_blockers_changed'))::text`).Scan(&blockerFacts); err != nil {
		t.Fatal("independent read-only blocker snapshot", err)
	}
	return base.taskSnapshot(t, actor) + blockerFacts
}

func TestIndependentWorkOwnerBlockerPagination(t *testing.T) {
	base := newTaskFixture(t)
	actor := base.human(t, "ind-page-owner", "user")
	other := base.human(t, "ind-page-admin", "admin")
	project, _, _ := base.create(t, actor, "independent-pager")
	milestone := base.milestone(t, actor, project.ID, "milestone")
	sprint := base.sprint(t, actor, project.ID, milestone.ID, "sprint")
	writer := independentPageWriter(t, base)
	reader := independentPager(t, base)

	t.Run("cursor_identity_version_and_readonly", func(t *testing.T) {
		task, expected := independentPageSeed(t, writer, actor, base.task(t, actor, project.ID, sprint.ID, "paged"), 5)
		sibling := base.task(t, actor, project.ID, sprint.ID, "other task")
		first, err := reader.ListTaskBlockersPage(ctxFor(t), actor, project.ID, task.ID, wc.TaskBlockersUnresolved, f.PageRequest{Limit: 2})
		if err != nil || first.NextCursor == "" {
			t.Fatal("first page", err)
		}
		independentPageEqual(t, first.Items, expected[:2])
		// A different Task advances the Project query generation. It must not
		// invalidate this Task's Blocker cursor, whose generation is its version.
		title := "other task changed"
		if _, err = base.tasks.UpdateTask(ctxFor(t), actor, meta(t, "ind-page-other-change", &sibling.Version), project.ID, sibling.ID, wc.TaskFieldsUpdate{Title: &title}); err != nil {
			t.Fatal(err)
		}
		freshSession := base.renew(t, actor)
		before := independentPageSnapshot(t, base, freshSession)
		last, err := reader.ListTaskBlockersPage(ctxFor(t), freshSession, project.ID, task.ID, wc.TaskBlockersUnresolved, f.PageRequest{Limit: 3, Cursor: first.NextCursor})
		if err != nil || last.NextCursor != "" {
			t.Fatal("same User / changed limit / unchanged target version continuation", err)
		}
		independentPageEqual(t, last.Items, expected[2:])
		for _, bad := range []struct {
			target wc.TaskID
			status wc.TaskBlockerStatus
		}{{sibling.ID, wc.TaskBlockersUnresolved}, {task.ID, wc.TaskBlockersAll}} {
			page, err := reader.ListTaskBlockersPage(ctxFor(t), freshSession, project.ID, bad.target, bad.status, f.PageRequest{Limit: 2, Cursor: first.NextCursor})
			independentPageFailure(t, page, err, f.CursorInvalid)
		}
		page, err := reader.ListTaskBlockersPage(ctxFor(t), other, project.ID, task.ID, wc.TaskBlockersUnresolved, f.PageRequest{Limit: 2, Cursor: "not-a-token"})
		independentPageFailure(t, page, err, f.NotFound)
		if independentPageSnapshot(t, base, freshSession) != before {
			t.Fatal("page or rejected page changed canonical/Activity facts")
		}
		title = "target changed"
		if _, err = base.tasks.UpdateTask(ctxFor(t), actor, meta(t, "ind-page-target-change", &task.Version), project.ID, task.ID, wc.TaskFieldsUpdate{Title: &title}); err != nil {
			t.Fatal(err)
		}
		page, err = reader.ListTaskBlockersPage(ctxFor(t), actor, project.ID, task.ID, wc.TaskBlockersUnresolved, f.PageRequest{Limit: 2, Cursor: first.NextCursor})
		independentPageFailure(t, page, err, f.CursorStale)
	})

	t.Run("page_snapshot_serializes_real_resolve", func(t *testing.T) {
		task, expected := independentPageSeed(t, writer, actor, base.task(t, actor, project.ID, sprint.ID, "locked page"), 3)
		first, err := reader.ListTaskBlockersPage(ctxFor(t), actor, project.ID, task.ID, wc.TaskBlockersUnresolved, f.PageRequest{Limit: 1})
		if err != nil || first.NextCursor == "" {
			t.Fatal(err)
		}
		readBase, readHook := observedTaskFixture(t, base)
		readService := independentPager(t, readBase)
		held, release := make(chan int32, 1), make(chan struct{})
		var once sync.Once
		free := func() { once.Do(func() { close(release) }) }
		t.Cleanup(free)
		defer free()
		readHook.setAfter(func(ctx context.Context, tx f.Tx, _ f.TransactionCause) error {
			x, err := readHook.InTx(tx)
			if err != nil {
				return err
			}
			var pid int32
			if err = x.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&pid); err != nil {
				return err
			}
			held <- pid
			select {
			case <-release:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		})
		pending, _ := independentPageStart(t, func(ctx context.Context) (f.Page[wc.TaskBlocker], error) {
			return readService.ListTaskBlockersPage(ctx, actor, project.ID, task.ID, wc.TaskBlockersUnresolved, f.PageRequest{Limit: 1, Cursor: first.NextCursor})
		})
		var readerPID int32
		select {
		case readerPID = <-held:
		case <-time.After(5 * time.Second):
			t.Fatal("reader did not hold its completed same-Tx page")
		}
		writeBase, writeHook := observedTaskFixture(t, base)
		writeService := independentPageWriter(t, writeBase)
		key, err := f.ProjectScheduleLock(project.ID.String())
		if err != nil {
			t.Fatal(err)
		}
		observed := observeLock(writeHook, key, nil)
		resolveMeta := meta(t, "ind-page-resolve-waiter", &task.Version)
		changed, _ := independentPageStart(t, func(ctx context.Context) (f.Page[wc.TaskBlocker], error) {
			result, err := writeService.ResolveTaskBlocker(ctx, actor, resolveMeta, project.ID, task.ID, wc.TaskBlockerResolve{BlockerID: expected[1].ID})
			if err == nil && (result.Task.Version != task.Version+1 || result.Blocker.ID != expected[1].ID || result.Blocker.ResolvedAt == nil) {
				return f.Page[wc.TaskBlocker]{}, errors.New("actual resolve postimage mismatch")
			}
			return f.Page[wc.TaskBlocker]{Items: []wc.TaskBlocker{result.Blocker}}, err
		})
		attempt := awaitLockAttempt(t, observed)
		waitExactLock(t, base.db, attempt, false, readerPID)
		free()
		old := independentPageJoin(t, pending)
		if old.err != nil || old.page.NextCursor == "" {
			t.Fatal("held page did not complete unchanged", old.err)
		}
		independentPageEqual(t, old.page.Items, expected[1:2])
		if result := independentPageJoin(t, changed); result.err != nil {
			t.Fatal("serialized resolve", result.err)
		}
		page, err := reader.ListTaskBlockersPage(ctxFor(t), actor, project.ID, task.ID, wc.TaskBlockersUnresolved, f.PageRequest{Limit: 1, Cursor: old.page.NextCursor})
		independentPageFailure(t, page, err, f.CursorStale)
		page, err = reader.ListTaskBlockersPage(ctxFor(t), actor, project.ID, task.ID, wc.TaskBlockersResolved, f.PageRequest{Limit: 1})
		if err != nil || len(page.Items) != 1 || page.Items[0].ID != expected[1].ID || page.Items[0].ResolvedAt == nil || page.NextCursor != "" {
			t.Fatal("resolved filter current page", err)
		}
	})

	t.Run("session_revocation_and_canceled_lock_wait", func(t *testing.T) {
		owner := base.human(t, "ind-page-revoke", "user")
		p, _, _ := base.create(t, owner, "ind-page-revocation")
		m := base.milestone(t, owner, p.ID, "m")
		s := base.sprint(t, owner, p.ID, m.ID, "s")
		task, _ := independentPageSeed(t, writer, owner, base.task(t, owner, p.ID, s.ID, "revoked reader"), 2)
		first, err := reader.ListTaskBlockersPage(ctxFor(t), owner, p.ID, task.ID, wc.TaskBlockersUnresolved, f.PageRequest{Limit: 1})
		if err != nil || first.NextCursor == "" {
			t.Fatal(err)
		}
		key, err := f.UserLock(owner.Details().UserID)
		if err != nil {
			t.Fatal(err)
		}
		held, release := make(chan int32, 1), make(chan struct{})
		var once sync.Once
		free := func() { once.Do(func() { close(release) }) }
		t.Cleanup(free)
		defer free()
		revokeCause := cause(t)
		revocation, _ := independentPageStart(t, func(ctx context.Context) (f.Page[wc.TaskBlocker], error) {
			result := base.store.WithinTx(ctx, revokeCause, func(ctx context.Context, tx f.Tx) error {
				if err := base.store.AcquireAll(ctx, tx, []f.LockRequest{{Key: key, Mode: f.Exclusive}}); err != nil {
					return err
				}
				if err := base.accounts.RequireCurrentSession(ctx, tx, owner); err != nil {
					return err
				}
				x, err := base.store.InTx(tx)
				if err != nil {
					return err
				}
				var pid int32
				if err = x.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&pid); err != nil {
					return err
				}
				// Explicit negative Account input; this is not a Logout API test.
				if _, err = x.Exec(ctx, `UPDATE agenteam_account.sessions SET revoked_at=clock_timestamp(),revoked_reason='administrative' WHERE id=$1`, owner.Details().SessionID); err != nil {
					return err
				}
				held <- pid
				select {
				case <-release:
					return nil
				case <-ctx.Done():
					return ctx.Err()
				}
			})
			if result.State() != f.Committed {
				return f.Page[wc.TaskBlocker]{}, errors.New("negative revocation fixture did not commit")
			}
			return f.Page[wc.TaskBlocker]{}, nil
		})
		var holder int32
		select {
		case holder = <-held:
		case <-time.After(5 * time.Second):
			t.Fatal("revocation fixture did not hold User EX")
		}
		for _, cancelWait := range []bool{true, false} {
			readBase, hook := observedTaskFixture(t, base)
			service := independentPager(t, readBase)
			observed := observeLock(hook, key, nil)
			pending, cancel := independentPageStart(t, func(ctx context.Context) (f.Page[wc.TaskBlocker], error) {
				return service.ListTaskBlockersPage(ctx, owner, p.ID, task.ID, wc.TaskBlockersUnresolved, f.PageRequest{Limit: 1, Cursor: first.NextCursor})
			})
			attempt := independentPageWaitShared(t, base, observed, holder)
			if cancelWait {
				cancel()
				result := independentPageJoin(t, pending)
				if !errors.Is(result.err, context.Canceled) || len(result.page.Items) != 0 || result.page.NextCursor != "" {
					t.Fatal("canceled actual lock wait published data")
				}
				independentPageNoLocks(t, base, attempt)
				continue
			}
			free()
			result := independentPageJoin(t, pending)
			independentPageFailure(t, result.page, result.err, f.SessionRevoked)
			independentPageNoLocks(t, base, attempt)
		}
		if result := independentPageJoin(t, revocation); result.err != nil {
			t.Fatal(result.err)
		}
	})
}
