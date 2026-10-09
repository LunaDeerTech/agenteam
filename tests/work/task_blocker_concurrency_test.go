//go:build integration

package work_test

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	wc "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
)

type blockerReply struct {
	result wc.TaskBlockerMutation
	err    error
}

func callBlockerAsync(t *testing.T, fn func(context.Context) (wc.TaskBlockerMutation, error)) <-chan blockerReply {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	out := make(chan blockerReply, 1)
	done := make(chan struct{})
	go func() { defer close(done); v, err := fn(ctx); out <- blockerReply{v, err} }()
	t.Cleanup(func() { cancel(); await(t, done) })
	return out
}

func joinBlockerReply(t *testing.T, ch <-chan blockerReply) blockerReply {
	t.Helper()
	select {
	case v := <-ch:
		return v
	case <-time.After(10 * time.Second):
		t.Fatal("Blocker call did not join")
	}
	return blockerReply{}
}

func awaitBlockerStage(t *testing.T, reached <-chan struct{}, reply <-chan blockerReply) {
	t.Helper()
	select {
	case <-reached:
	case result := <-reply:
		t.Fatal("Blocker returned before observed stage", result.err)
	case <-time.After(10 * time.Second):
		t.Fatal("Blocker stage was not reached")
	}
}

func awaitBlockerLockAttempt(t *testing.T, observed <-chan lockAttempt, mode foundation.LockMode) lockAttempt {
	t.Helper()
	select {
	case attempt := <-observed:
		if attempt.BackendPID <= 0 || attempt.Request.Mode != mode {
			t.Fatal("caller did not request the expected real lock mode", attempt, mode)
		}
		return attempt
	case <-time.After(5 * time.Second):
		t.Fatal("actual Blocker caller Tx did not reach AcquireAll")
	}
	return lockAttempt{}
}

func observedBlockerFixture(t *testing.T, base *blockerFixture) (*blockerFixture, *hookStore) {
	t.Helper()
	tf, store := observedTaskFixture(t, base.taskFixture)
	return attachBlockers(t, tf), store
}

// Hold the actual completed transaction before COMMIT, retaining its real
// locks. This neither substitutes a CommitResult nor predicts a waiter.
func holdBlockerFinal(t *testing.T, store *hookStore, project wc.ProjectID, command wc.TaskBlockerCommandName, key foundation.IdempotencyKey) (<-chan struct{}, *atomic.Int32, func()) {
	t.Helper()
	identity, err := wc.TaskBlockerCommandIdentity(project, command, key)
	if err != nil {
		t.Fatal(err)
	}
	reached, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	var armed atomic.Bool
	pid := new(atomic.Int32)
	store.setAfter(func(ctx context.Context, tx foundation.Tx, cause foundation.TransactionCause) error {
		if cause.Kind() != foundation.CommandsCause || cause.Details().Primary.Canonical() != identity.Canonical() || armed.Load() {
			return nil
		}
		x, err := store.InTx(tx)
		if err != nil {
			return err
		}
		var completed bool
		if err = x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_work.task_blocker_commands WHERE project_id=$1 AND command_name=$2 AND idempotency_key=$3 AND state='completed')`, project.String(), string(command), string(key)).Scan(&completed); err != nil {
			return err
		}
		if completed && armed.CompareAndSwap(false, true) {
			var backend int32
			if err = x.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&backend); err != nil {
				return err
			}
			pid.Store(backend)
			close(reached)
			select {
			case <-release:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		return nil
	})
	unlock := func() { once.Do(func() { close(release) }) }
	t.Cleanup(unlock)
	return reached, pid, unlock
}

func TestTaskBlockerConcurrency(t *testing.T) {
	f := newBlockerFixture(t)
	a := f.human(t, "blocker-concurrency", "user")
	p, _, _ := f.create(t, a, "blocker-concurrency")
	m := f.milestone(t, a, p.ID, "m")
	s := f.sprint(t, a, p.ID, m.ID, "s")
	t.Run("whole-graph-and-resolved-edge", func(t *testing.T) {
		one := f.task(t, a, p.ID, s.ID, "one")
		two := f.task(t, a, p.ID, s.ID, "two")
		three := f.task(t, a, p.ID, s.ID, "three")
		first, err := f.blockers.AddTaskBlocker(ctxFor(t), a, meta(t, "chain-a", &one.Version), p.ID, one.ID, blockerDependency(t, two.ID))
		if err != nil {
			t.Fatal(err)
		}
		second, err := f.blockers.AddTaskBlocker(ctxFor(t), a, meta(t, "chain-b", &two.Version), p.ID, two.ID, blockerDependency(t, three.ID))
		if err != nil {
			t.Fatal(err)
		}
		before := f.blockerSnapshot(t, a)
		_, err = f.blockers.AddTaskBlocker(ctxFor(t), a, meta(t, "chain-cycle", &three.Version), p.ID, three.ID, blockerDependency(t, one.ID))
		requireCode(t, err, foundation.TaskDependencyCycle)
		if f.blockerSnapshot(t, a) != before {
			t.Fatal("cycle rejection changed facts")
		}
		_, err = f.blockers.ResolveTaskBlocker(ctxFor(t), a, meta(t, "break-chain", &second.Task.Version), p.ID, two.ID, wc.TaskBlockerResolve{BlockerID: second.Blocker.ID})
		if err != nil {
			t.Fatal(err)
		}
		_, err = f.blockers.AddTaskBlocker(ctxFor(t), a, meta(t, "resolved-edge-excluded", &three.Version), p.ID, three.ID, blockerDependency(t, one.ID))
		if err != nil {
			t.Fatal("resolved history still participates in graph", err)
		}
		// done/cancelled are explicitly test-owned future state inputs. The
		// direct Human service must not invent automatic dependency recovery.
		for _, state := range []wc.TaskState{wc.TaskStateDone, wc.TaskStateCancelled} {
			f.seedTaskState(t, a, two, state, nil)
			rows, err := f.blockers.ListTaskBlockers(ctxFor(t), a, p.ID, one.ID, wc.TaskBlockersUnresolved)
			if err != nil || len(rows) != 1 || rows[0].ID != first.Blocker.ID {
				t.Fatal("read automatically resolved dependency", state, err)
			}
		}
	})
	t.Run("opposite-edges-real-waiter-one-cycle", func(t *testing.T) {
		one := f.task(t, a, p.ID, s.ID, "opposite-one")
		two := f.task(t, a, p.ID, s.ID, "opposite-two")
		writer, store := observedBlockerFixture(t, f)
		competitor, competitorStore := observedBlockerFixture(t, f)
		cm := meta(t, "opposite-a", &one.Version)
		reached, pid, release := holdBlockerFinal(t, store, p.ID, wc.TaskBlockerCommandAdd, cm.IdempotencyKey)
		user, _ := foundation.UserLock(a.Details().UserID)
		waiter := observeLock(competitorStore, user, nil)
		first := callBlockerAsync(t, func(ctx context.Context) (wc.TaskBlockerMutation, error) {
			return writer.blockers.AddTaskBlocker(ctx, a, cm, p.ID, one.ID, blockerDependency(t, two.ID))
		})
		awaitBlockerStage(t, reached, first)
		second := callBlockerAsync(t, func(ctx context.Context) (wc.TaskBlockerMutation, error) {
			return competitor.blockers.AddTaskBlocker(ctx, a, meta(t, "opposite-b", &two.Version), p.ID, two.ID, blockerDependency(t, one.ID))
		})
		attempt := awaitBlockerLockAttempt(t, waiter, foundation.Shared)
		waitTaskExactMode(t, f.taskFixture, attempt, foundation.Shared, pid.Load())
		release()
		x, y := joinBlockerReply(t, first), joinBlockerReply(t, second)
		if x.err != nil {
			t.Fatal(x.err)
		}
		requireCode(t, y.err, foundation.TaskDependencyCycle)
		rows, err := f.blockers.ListTaskBlockers(ctxFor(t), a, p.ID, two.ID, wc.TaskBlockersAll)
		if err != nil || len(rows) != 0 {
			t.Fatal("losing edge persisted", err)
		}
	})
	t.Run("different-keys-same-task-version", func(t *testing.T) {
		target := f.task(t, a, p.ID, s.ID, "same-version")
		writer, store := observedBlockerFixture(t, f)
		competitor, competitorStore := observedBlockerFixture(t, f)
		ready, proceed := make(chan struct{}), make(chan struct{})
		var once sync.Once
		unpark := func() { once.Do(func() { close(proceed) }) }
		defer unpark()
		app := &capturingAppender{Appender: competitor.blockerOutbox, after: func(ctx context.Context, _ identity.Actor, _ event.Event, _ oc.AppendPlan) error {
			close(ready)
			select {
			case <-proceed:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		}}
		competitor.blockers = competitor.newBlockerService(t, app, competitor.accounts)
		loserRequest := blockerWaiting(t, "same version loser")
		second := callBlockerAsync(t, func(ctx context.Context) (wc.TaskBlockerMutation, error) {
			return competitor.blockers.AddTaskBlocker(ctx, a, meta(t, "same-version-second", &target.Version), p.ID, target.ID, loserRequest)
		})
		awaitBlockerStage(t, ready, second)
		var beforeGeneration int64
		if err := f.raw.QueryRow(ctxFor(t), `SELECT query_generation FROM agenteam_work.task_query_generations WHERE project_id=$1`, p.ID.String()).Scan(&beforeGeneration); err != nil {
			t.Fatal(err)
		}
		cm := meta(t, "same-version-first", &target.Version)
		reached, pid, release := holdBlockerFinal(t, store, p.ID, wc.TaskBlockerCommandAdd, cm.IdempotencyKey)
		first := callBlockerAsync(t, func(ctx context.Context) (wc.TaskBlockerMutation, error) {
			return writer.blockers.AddTaskBlocker(ctx, a, cm, p.ID, target.ID, blockerWaiting(t, "same version winner"))
		})
		awaitBlockerStage(t, reached, first)
		user, _ := foundation.UserLock(a.Details().UserID)
		waiter := observeLock(competitorStore, user, nil)
		unpark()
		attempt := awaitBlockerLockAttempt(t, waiter, foundation.Exclusive)
		waitExactLock(t, f.db, attempt, false, pid.Load())
		release()
		x, y := joinBlockerReply(t, first), joinBlockerReply(t, second)
		if x.err != nil || x.result.Task.Version != target.Version+1 || x.result.Task.ManualRank != target.ManualRank {
			t.Fatal("first real final did not commit one unchanged-rank version", x.err)
		}
		requireCode(t, y.err, foundation.TaskVersionConflict)
		rows, err := f.blockers.ListTaskBlockers(ctxFor(t), a, p.ID, target.ID, wc.TaskBlockersAll)
		if err != nil || len(rows) != 1 || rows[0].ID != x.result.Blocker.ID || rows[0].ID == loserRequest.BlockerID {
			t.Fatal("same-version loser persisted a Blocker", err)
		}
		var generation, history, outbox, completed int64
		if err = f.raw.QueryRow(ctxFor(t), `SELECT
 (SELECT query_generation FROM agenteam_work.task_query_generations WHERE project_id=$1),
 (SELECT count(*) FROM agenteam_work.task_events WHERE task_id=$2 AND blocker_operation_id IS NOT NULL),
 (SELECT count(*) FROM agenteam_outbox.events WHERE aggregate_id=$2 AND event_type='work.task_blockers_changed'),
 (SELECT count(*) FROM agenteam_work.task_blocker_commands WHERE project_id=$1 AND idempotency_key IN ('same-version-first','same-version-second') AND state='completed')`, p.ID.String(), target.ID.String()).Scan(&generation, &history, &outbox, &completed); err != nil || generation != beforeGeneration+1 || history != 1 || outbox != 1 || completed != 1 {
			t.Fatal("same-version race did not commit exactly one fact set", generation, history, outbox, completed, err)
		}
	})
	t.Run("same-key-exact-command-waiter", func(t *testing.T) {
		target := f.task(t, a, p.ID, s.ID, "same-key")
		writer, store := observedBlockerFixture(t, f)
		competitor, competitorStore := observedBlockerFixture(t, f)
		cm := meta(t, "same-blocker-command", &target.Version)
		request := blockerWaiting(t, "once")
		command, err := wc.TaskBlockerCommandIdentity(p.ID, wc.TaskBlockerCommandAdd, cm.IdempotencyKey)
		if err != nil {
			t.Fatal(err)
		}
		key, _ := foundation.CommandLock(command)
		waiter := observeLock(competitorStore, key, nil)
		reached, pid, release := holdBlockerFinal(t, store, p.ID, wc.TaskBlockerCommandAdd, cm.IdempotencyKey)
		first := callBlockerAsync(t, func(ctx context.Context) (wc.TaskBlockerMutation, error) {
			return writer.blockers.AddTaskBlocker(ctx, a, cm, p.ID, target.ID, request)
		})
		awaitBlockerStage(t, reached, first)
		second := callBlockerAsync(t, func(ctx context.Context) (wc.TaskBlockerMutation, error) {
			return competitor.blockers.AddTaskBlocker(ctx, a, cm, p.ID, target.ID, request)
		})
		waitExactLock(t, f.db, awaitLockAttempt(t, waiter), false, pid.Load())
		release()
		x, y := joinBlockerReply(t, first), joinBlockerReply(t, second)
		if x.err != nil || y.err != nil {
			t.Fatal(x.err, y.err)
		}
		equalBlockerMutation(t, x.result, y.result)
		rows, err := f.blockers.ListTaskBlockers(ctxFor(t), a, p.ID, target.ID, wc.TaskBlockersAll)
		if err != nil || len(rows) != 1 {
			t.Fatal("same key duplicated Blocker", err)
		}
	})
	t.Run("planning-update-loses-old-version", func(t *testing.T) {
		target := f.task(t, a, p.ID, s.ID, "planning-target")
		writer, store := observedBlockerFixture(t, f)
		planner, plannerStore := observedTaskFixture(t, f.taskFixture)
		cm := meta(t, "blocker-before-planning", &target.Version)
		reached, pid, release := holdBlockerFinal(t, store, p.ID, wc.TaskBlockerCommandAdd, cm.IdempotencyKey)
		user, _ := foundation.UserLock(a.Details().UserID)
		waiter := observeLock(plannerStore, user, nil)
		first := callBlockerAsync(t, func(ctx context.Context) (wc.TaskBlockerMutation, error) {
			return writer.blockers.AddTaskBlocker(ctx, a, cm, p.ID, target.ID, blockerWaiting(t, "blocker first"))
		})
		awaitBlockerStage(t, reached, first)
		title := "must not overwrite"
		second := callTaskAsync(t, func(ctx context.Context) (wc.TaskMutation, error) {
			return planner.tasks.UpdateTask(ctx, a, meta(t, "planning-old-version", &target.Version), p.ID, target.ID, wc.TaskFieldsUpdate{Title: &title})
		})
		waitTaskExactMode(t, f.taskFixture, awaitBlockerLockAttempt(t, waiter, foundation.Shared), foundation.Shared, pid.Load())
		release()
		x, y := joinBlockerReply(t, first), joinTaskReply(t, second)
		if x.err != nil {
			t.Fatal(x.err)
		}
		requireCode(t, y.err, foundation.TaskVersionConflict)
		current, err := f.taskReader.GetTask(ctxFor(t), a, p.ID, target.ID)
		if err != nil || current.Title != target.Title || current.Version != target.Version+1 {
			t.Fatal("planning/version combination", err)
		}
		_, err = planner.tasks.UpdateTask(ctxFor(t), a, meta(t, "planning-new-version", &current.Version), p.ID, target.ID, wc.TaskFieldsUpdate{Title: &title})
		if err != nil {
			t.Fatal("fresh planning update after Blocker", err)
		}
	})
	t.Run("prepared-blocker-rechecks-interposed-planning", func(t *testing.T) {
		target := f.task(t, a, p.ID, s.ID, "interposed-target")
		var fired atomic.Bool
		title := "planning wins preparation gap"
		capture := &capturingAppender{Appender: f.blockerOutbox}
		capture.after = func(ctx context.Context, _ identity.Actor, _ event.Event, _ oc.AppendPlan) error {
			if fired.CompareAndSwap(false, true) {
				_, err := f.tasks.UpdateTask(ctx, a, meta(t, "interposed-planning", &target.Version), p.ID, target.ID, wc.TaskFieldsUpdate{Title: &title})
				return err
			}
			return nil
		}
		writer := f.newBlockerService(t, capture, f.accounts)
		_, err := writer.AddTaskBlocker(ctxFor(t), a, meta(t, "interposed-blocker", &target.Version), p.ID, target.ID, blockerWaiting(t, "stale"))
		requireCode(t, err, foundation.TaskVersionConflict)
		rows, err := f.blockers.ListTaskBlockers(ctxFor(t), a, p.ID, target.ID, wc.TaskBlockersAll)
		if err != nil || len(rows) != 0 {
			t.Fatal("stale prepared Blocker persisted", err)
		}
	})
	t.Run("schedule-gate-is-real", func(t *testing.T) {
		target := f.task(t, a, p.ID, s.ID, "schedule-protected")
		writer, store := observedBlockerFixture(t, f)
		schedule, _ := foundation.ProjectScheduleLock(p.ID.String())
		attempts := observeLock(store, schedule, nil)
		unlock, pid := holdLocks(t, f.fixture, []foundation.LockRequest{{Key: schedule, Mode: foundation.Exclusive}})
		defer unlock()
		result := callBlockerAsync(t, func(ctx context.Context) (wc.TaskBlockerMutation, error) {
			return writer.blockers.AddTaskBlocker(ctx, a, meta(t, "schedule-gate", &target.Version), p.ID, target.ID, blockerWaiting(t, "gate"))
		})
		waitTaskExactMode(t, f.taskFixture, awaitLockAttempt(t, attempts), foundation.Exclusive, pid)
		unlock()
		if got := joinBlockerReply(t, result); got.err != nil {
			t.Fatal(got.err)
		}
	})
}
