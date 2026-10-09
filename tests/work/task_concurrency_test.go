//go:build integration

package work_test

import (
	"context"
	"errors"
	"fmt"
	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	c "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	wc "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type taskReply struct {
	result wc.TaskMutation
	err    error
}

func callTaskAsync(t *testing.T, fn func(context.Context) (wc.TaskMutation, error)) <-chan taskReply {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	out := make(chan taskReply, 1)
	done := make(chan struct{})
	go func() { defer close(done); r, err := fn(ctx); out <- taskReply{r, err} }()
	t.Cleanup(func() { cancel(); await(t, done) })
	return out
}
func joinTaskReply(t *testing.T, ch <-chan taskReply) taskReply {
	t.Helper()
	select {
	case r := <-ch:
		return r
	case <-time.After(10 * time.Second):
		t.Fatal("Task call did not actually join")
	}
	return taskReply{}
}
func TestTaskPlanningCommitUnknown(t *testing.T) {
	base := newTaskFixture(t)
	a := base.human(t, "unknown-owner", "user")
	p, _, _ := base.create(t, a, "unknown")
	milestone := base.milestone(t, a, p.ID, "m")
	sprint := base.sprint(t, a, p.ID, milestone.ID, "s")
	target := base.task(t, a, p.ID, sprint.ID, "before")
	for _, phase := range []string{"planned", "completed"} {
		for _, commit := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/commit=%t", phase, commit), func(t *testing.T) {
				f, store, proxy := proxyTaskFixture(t, base, commit)
				current, err := base.taskReader.GetTask(ctxFor(t), a, p.ID, target.ID)
				if err != nil {
					t.Fatal(err)
				}
				text := fmt.Sprintf("%s-%t", phase, commit)
				m := meta(t, id[struct{}](t).String(), &current.Version)
				r := wc.TaskFieldsUpdate{Title: &text}
				semantic, err := wc.TaskUpdateDigest(a, m, p.ID, target.ID, r)
				if err != nil {
					t.Fatal(err)
				}
				lookup := wc.TaskCommandLookupRequest{ProjectID: p.ID, Command: wc.TaskCommandUpdate, IdempotencyKey: m.IdempotencyKey, SemanticDigest: semantic}
				command, err := wc.TaskIdentity(p.ID, wc.TaskCommandUpdate, m.IdempotencyKey)
				if err != nil {
					t.Fatal(err)
				}
				key, err := foundation.CommandLock(command)
				if err != nil {
					t.Fatal(err)
				}
				confirmationAttempt := observeLock(store, key, func() bool {
					select {
					case <-proxy.reached:
						return true
					default:
						return false
					}
				})
				lookupFixture, lookupStore := observedTaskFixture(t, base)
				lookupAttempt := observeLock(lookupStore, key, nil)
				var armed atomic.Bool
				var backend atomic.Int32
				store.setAfter(func(ctx context.Context, tx foundation.Tx, cause foundation.TransactionCause) error {
					if cause.Kind() != foundation.CommandsCause || cause.Details().Primary.Canonical() != command.Canonical() || armed.Load() {
						return nil
					}
					x, err := store.InTx(tx)
					if err != nil {
						return err
					}
					var state string
					if err = x.QueryRow(ctx, `SELECT coalesce((SELECT state FROM agenteam_work.task_commands WHERE project_id=$1 AND command_name=$2 AND idempotency_key=$3),'')`, p.ID.String(), string(wc.TaskCommandUpdate), string(m.IdempotencyKey)).Scan(&state); err != nil {
						return err
					}
					if state == phase && armed.CompareAndSwap(false, true) {
						var pid int32
						if err = x.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&pid); err != nil {
							return err
						}
						backend.Store(pid)
						proxy.targetPID.Store(pid)
					}
					return nil
				})
				var releaseOnce sync.Once
				release := func() { releaseOnce.Do(func() { close(proxy.release) }) }
				defer release()
				beforeEvents := base.eventCount(t)
				original := callTaskAsync(t, func(ctx context.Context) (wc.TaskMutation, error) {
					return f.tasks.UpdateTask(ctx, a, m, p.ID, target.ID, r)
				})
				awaitTaskStage(t, proxy.reached, original)
				if proxy.backendPID.Load() != backend.Load() || backend.Load() <= 0 {
					t.Fatal("COMMIT not bound to exact backend")
				}
				actualConfirmation := awaitLockAttempt(t, confirmationAttempt)
				waitExactLock(t, base.db, actualConfirmation, false, backend.Load())
				type lookupReply struct {
					result wc.TaskCommandLookup
					err    error
				}
				lookupDone := make(chan lookupReply, 1)
				lookupJoined := make(chan struct{})
				lookupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				go func() {
					defer close(lookupJoined)
					value, err := lookupFixture.tasks.LookupTaskCommand(lookupCtx, a, lookup)
					lookupDone <- lookupReply{value, err}
				}()
				t.Cleanup(func() { cancel(); await(t, lookupJoined) })
				actualLookup := awaitLockAttempt(t, lookupAttempt)
				if actualLookup.BackendPID == actualConfirmation.BackendPID || actualLookup.BackendPID == backend.Load() {
					t.Fatal("lookup did not use an independent caller Tx")
				}
				waitExactLock(t, base.db, actualLookup, false, backend.Load())
				cancel() // Only after this caller is proved waiting on the exact writer.
				await(t, lookupJoined)
				looked := <-lookupDone
				if !errors.Is(looked.err, context.Canceled) || looked.result.Status != "" || looked.result.Receipt != nil {
					t.Fatal("cancelled exact Lookup waiter published a result or lost cancellation", looked.result, looked.err)
				}
				got := joinTaskReply(t, original)
				requireCode(t, got.err, foundation.CommitUnknown)
				var unknown *foundation.Fault
				if !errors.As(got.err, &unknown) || unknown.CommitState != foundation.Unknown || unknown.RetryHint != "lookup" {
					t.Fatal("unknown proof rewritten")
				}
				if base.eventCount(t) != beforeEvents {
					t.Fatal("held writer result was prematurely visible")
				}
				release()
				await(t, proxy.completed)
				observed, err := base.tasks.LookupTaskCommand(ctxFor(t), a, lookup)
				if err != nil {
					t.Fatal(err)
				}
				want := wc.LookupInProgress
				if phase == "planned" && !commit {
					want = wc.LookupNotObserved
				}
				if phase == "completed" && commit {
					want = wc.LookupCommitted
				}
				if observed.Status != want {
					t.Fatal("wrong serialized late outcome", observed.Status, want)
				}
				fresh := base.renew(t, a)
				resolved, err := base.tasks.UpdateTask(ctxFor(t), fresh, m, p.ID, target.ID, r)
				if err != nil {
					t.Fatal("explicit original recovery", err)
				}
				if resolved.Task.Title != text || resolved.Task.Version != current.Version+1 || base.eventCount(t) != beforeEvents+1 {
					t.Fatal("late recovery duplicated or changed intent")
				}
				if observed.Receipt != nil {
					equalTaskMutation(t, *observed.Receipt, resolved)
				}
				stable := base.taskSnapshot(t, fresh)
				again, err := base.tasks.UpdateTask(ctxFor(t), fresh, m, p.ID, target.ID, r)
				if err != nil {
					t.Fatal(err)
				}
				equalTaskMutation(t, resolved, again)
				if base.eventCount(t) != beforeEvents+1 || base.taskSnapshot(t, fresh) != stable {
					t.Fatal("receipt recovery duplicated event or touched stored facts")
				}
				var commands int
				if err = base.raw.QueryRow(ctxFor(t), `SELECT count(*) FROM agenteam_work.task_commands WHERE project_id=$1 AND command_name=$2 AND idempotency_key=$3`, p.ID.String(), string(wc.TaskCommandUpdate), string(m.IdempotencyKey)).Scan(&commands); err != nil || commands != 1 {
					t.Fatal("original identity was replaced", err)
				}
				t.Log("actual COMMIT frame held; serialized lookup and real late outcome", phase, commit, "backend", backend.Load())
			})
		}
	}
	for _, failure := range []string{"confirmation-sql-error", "confirmation-revoked-session"} {
		t.Run(failure, func(t *testing.T) {
			f, store, proxy := proxyTaskFixture(t, base, true)
			actor := base.renew(t, a)
			recoveryActor := base.renew(t, a)
			current, err := base.taskReader.GetTask(ctxFor(t), a, p.ID, target.ID)
			if err != nil {
				t.Fatal(err)
			}
			text := failure
			metadata := meta(t, failure, &current.Version)
			identity, err := wc.TaskIdentity(p.ID, wc.TaskCommandUpdate, metadata.IdempotencyKey)
			if err != nil {
				t.Fatal(err)
			}
			var armed atomic.Bool
			var original foundation.CommitResult
			var observed atomic.Bool
			var once sync.Once
			release := func() { once.Do(func() { close(proxy.release) }) }
			defer release()
			store.setAfter(func(ctx context.Context, tx foundation.Tx, cause foundation.TransactionCause) error {
				if cause.Kind() != foundation.CommandsCause || cause.Details().Primary.Canonical() != identity.Canonical() || armed.Load() {
					return nil
				}
				x, err := store.InTx(tx)
				if err != nil {
					return err
				}
				var completed bool
				if err = x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_work.task_commands WHERE project_id=$1 AND command_name=$2 AND idempotency_key=$3 AND state='completed')`, p.ID.String(), string(wc.TaskCommandUpdate), string(metadata.IdempotencyKey)).Scan(&completed); err != nil {
					return err
				}
				if completed && armed.CompareAndSwap(false, true) {
					var pid int32
					if err = x.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&pid); err != nil {
						return err
					}
					proxy.targetPID.Store(pid)
				}
				return nil
			})
			store.mu.Lock()
			store.afterResult = func(cause foundation.TransactionCause, result foundation.CommitResult) {
				if cause.Kind() != foundation.CommandsCause || cause.Details().Primary.Canonical() != identity.Canonical() || result.State() != foundation.Unknown || !observed.CompareAndSwap(false, true) {
					return
				}
				original = result
				release()
				await(t, proxy.completed)
				if failure == "confirmation-revoked-session" {
					key, _ := foundation.UserLock(actor.Details().UserID)
					base.tx(t, []foundation.LockRequest{{Key: key, Mode: foundation.Exclusive}}, func(ctx context.Context, tx foundation.Tx, x postgres.SQLExecutor) error {
						if err := base.accounts.RequireCurrentSession(ctx, tx, actor); err != nil {
							return err
						}
						_, err := x.Exec(ctx, `UPDATE agenteam_account.sessions SET revoked_at=clock_timestamp(),revoked_reason='administrative' WHERE id=$1`, actor.Details().SessionID)
						return err
					})
				}
			}
			store.beforeLocks = func(ctx context.Context, tx foundation.Tx, _ []foundation.LockRequest) error {
				if failure != "confirmation-sql-error" || !observed.Load() {
					return nil
				}
				x, err := store.InTx(tx)
				if err != nil {
					return err
				}
				_, err = x.Exec(ctx, `SELECT 1/0`)
				return err
			}
			store.mu.Unlock()
			before := base.eventCount(t)
			result, err := f.tasks.UpdateTask(ctxFor(t), actor, metadata, p.ID, target.ID, wc.TaskFieldsUpdate{Title: &text})
			requireCode(t, err, foundation.CommitUnknown)
			var fault *foundation.Fault
			if !observed.Load() || original.State() != foundation.Unknown || original.AttemptID().Validate() != nil || !errors.As(err, &fault) || fault.CauseID != original.AttemptID().String() || fault.CommitState != foundation.Unknown || fault.RetryHint != "lookup" || result.Changed {
				t.Fatal("failed confirmation replaced original physical Unknown", err)
			}
			if base.eventCount(t) != before+1 {
				t.Fatal("actual late commit not observed once")
			}
			resolved, err := base.tasks.UpdateTask(ctxFor(t), recoveryActor, metadata, p.ID, target.ID, wc.TaskFieldsUpdate{Title: &text})
			if err != nil || resolved.Task.Title != text || base.eventCount(t) != before+1 {
				t.Fatal("explicit recovery after failed confirmation", err)
			}
		})
	}
	t.Run("confirmation-real-commit-unknown-keeps-writer-cause", func(t *testing.T) {
		f, store, writerProxy, confirmationProxy := doubleProxyTaskFixture(t, base)
		current, err := base.taskReader.GetTask(ctxFor(t), a, p.ID, target.ID)
		if err != nil {
			t.Fatal(err)
		}
		title := "confirmation also unknown"
		cm := meta(t, "confirmation-also-unknown", &current.Version)
		command := taskIdentity(t, p.ID, wc.TaskCommandUpdate, cm.IdempotencyKey)
		var writerArmed atomic.Bool
		var writerObserved atomic.Bool
		var writerResult, confirmationResult foundation.CommitResult
		var writerPID, confirmationPID int32
		var writerOnce, confirmationOnce sync.Once
		releaseWriter := func() { writerOnce.Do(func() { close(writerProxy.release) }) }
		releaseConfirmation := func() { confirmationOnce.Do(func() { close(confirmationProxy.release) }) }
		defer releaseWriter()
		defer releaseConfirmation()
		store.setAfter(func(ctx context.Context, tx foundation.Tx, cause foundation.TransactionCause) error {
			if cause.Kind() != foundation.CommandsCause || cause.Details().Primary.Canonical() != command.Canonical() {
				return nil
			}
			x, err := store.InTx(tx)
			if err != nil {
				return err
			}
			var completed bool
			if err = x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_work.task_commands WHERE project_id=$1 AND command_name=$2 AND idempotency_key=$3 AND state='completed')`, p.ID.String(), string(wc.TaskCommandUpdate), string(cm.IdempotencyKey)).Scan(&completed); err != nil {
				return err
			}
			if !completed {
				return nil
			}
			var pid int32
			if err = x.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&pid); err != nil {
				return err
			}
			if writerArmed.CompareAndSwap(false, true) {
				writerPID = pid
				writerProxy.targetPID.Store(pid)
			} else if writerObserved.Load() {
				confirmationPID = pid
				confirmationProxy.targetPID.Store(pid)
			}
			return nil
		})
		store.mu.Lock()
		store.afterResult = func(cause foundation.TransactionCause, result foundation.CommitResult) {
			if cause.Kind() != foundation.CommandsCause || cause.Details().Primary.Canonical() != command.Canonical() || result.State() != foundation.Unknown {
				return
			}
			if writerObserved.CompareAndSwap(false, true) {
				writerResult = result
				releaseWriter()
				await(t, writerProxy.completed)
			} else {
				confirmationResult = result
			}
		}
		store.mu.Unlock()
		beforeH, beforeE := base.taskCounts(t)
		out, err := f.tasks.UpdateTask(ctxFor(t), a, cm, p.ID, target.ID, wc.TaskFieldsUpdate{Title: &title})
		requireCode(t, err, foundation.CommitUnknown)
		await(t, writerProxy.reached)
		await(t, confirmationProxy.reached)
		var fault *foundation.Fault
		if writerPID <= 0 || confirmationPID <= 0 || writerPID == confirmationPID || writerProxy.backendPID.Load() != writerPID || confirmationProxy.backendPID.Load() != confirmationPID {
			t.Fatal("two Unknowns did not use independent exact backend PIDs")
		}
		if writerResult.State() != foundation.Unknown || confirmationResult.State() != foundation.Unknown || writerResult.AttemptID() == confirmationResult.AttemptID() || !errors.As(err, &fault) || fault.CauseID != writerResult.AttemptID().String() || fault.CommitState != foundation.Unknown || out.Changed {
			t.Fatal("confirmation Unknown replaced original writer proof", err)
		}
		releaseConfirmation()
		await(t, confirmationProxy.completed)
		resolved, err := base.tasks.UpdateTask(ctxFor(t), a, cm, p.ID, target.ID, wc.TaskFieldsUpdate{Title: &title})
		if err != nil || resolved.Task.Title != title || resolved.Task.Version != current.Version+1 {
			t.Fatal("explicit recovery after confirmation Unknown", err)
		}
		h, e := base.taskCounts(t)
		if h != beforeH+1 || e != beforeE+1 {
			t.Fatal("confirmation Unknown duplicated event", h, e)
		}
		t.Log("actual original and read-only confirmation COMMIT frames each held; original causal attempt retained", writerPID, confirmationPID)
	})

	t.Run("stop-joins-confirmation-not-proxy-writer", func(t *testing.T) {
		f, store, proxy := proxyTaskFixture(t, base, true)
		current, err := base.taskReader.GetTask(ctxFor(t), a, p.ID, target.ID)
		if err != nil {
			t.Fatal(err)
		}
		text := "stop"
		m := meta(t, "stop-confirmation", &current.Version)
		identity, err := wc.TaskIdentity(p.ID, wc.TaskCommandUpdate, m.IdempotencyKey)
		if err != nil {
			t.Fatal(err)
		}
		key, err := foundation.CommandLock(identity)
		if err != nil {
			t.Fatal(err)
		}
		confirmationAttempt := observeLock(store, key, func() bool {
			select {
			case <-proxy.reached:
				return true
			default:
				return false
			}
		})
		var armed atomic.Bool
		store.setAfter(func(ctx context.Context, tx foundation.Tx, cause foundation.TransactionCause) error {
			if cause.Kind() != foundation.CommandsCause || cause.Details().Primary.Canonical() != identity.Canonical() || armed.Load() {
				return nil
			}
			x, err := store.InTx(tx)
			if err != nil {
				return err
			}
			var completed bool
			if err = x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_work.task_commands WHERE project_id=$1 AND idempotency_key=$2 AND state='completed')`, p.ID.String(), string(m.IdempotencyKey)).Scan(&completed); err != nil {
				return err
			}
			if completed && armed.CompareAndSwap(false, true) {
				var pid int32
				if err = x.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&pid); err != nil {
					return err
				}
				proxy.targetPID.Store(pid)
			}
			return nil
		})
		var once sync.Once
		release := func() { once.Do(func() { close(proxy.release) }) }
		defer release()
		reply := callTaskAsync(t, func(ctx context.Context) (wc.TaskMutation, error) {
			return f.tasks.UpdateTask(ctx, a, m, p.ID, target.ID, wc.TaskFieldsUpdate{Title: &text})
		})
		awaitTaskStage(t, proxy.reached, reply)
		actualConfirmation := awaitLockAttempt(t, confirmationAttempt)
		waitExactLock(t, base.db, actualConfirmation, false, proxy.backendPID.Load())
		f.tasks.Stop()
		got := joinTaskReply(t, reply)
		requireCode(t, got.err, foundation.CommitUnknown)
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err = f.tasks.Drain(ctx); err != nil {
			t.Fatal("confirmation was not joined", err)
		}
		release()
		await(t, proxy.completed)
	})
	t.Run("cancel-before-final-and-after-known-commit", func(t *testing.T) {
		for _, afterCommit := range []bool{false, true} {
			t.Run(fmt.Sprint(afterCommit), func(t *testing.T) {
				raw := openStore(t, base.db.Config(t, nil))
				store := &hookStore{fixtureStore: raw}
				f := assembleTask(t, base.db, raw, store, false)
				current, err := base.taskReader.GetTask(ctxFor(t), a, p.ID, target.ID)
				if err != nil {
					t.Fatal(err)
				}
				text := fmt.Sprintf("cancel-%t", afterCommit)
				m := meta(t, id[struct{}](t).String(), &current.Version)
				command, err := wc.TaskIdentity(p.ID, wc.TaskCommandUpdate, m.IdempotencyKey)
				if err != nil {
					t.Fatal(err)
				}
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				var completed atomic.Bool
				store.setAfter(func(ctx context.Context, tx foundation.Tx, cause foundation.TransactionCause) error {
					if cause.Kind() != foundation.CommandsCause || cause.Details().Primary.Canonical() != command.Canonical() {
						return nil
					}
					x, err := store.InTx(tx)
					if err != nil {
						return err
					}
					var state string
					if err = x.QueryRow(ctx, `SELECT coalesce((SELECT state FROM agenteam_work.task_commands WHERE project_id=$1 AND command_name=$2 AND idempotency_key=$3),'')`, p.ID.String(), string(wc.TaskCommandUpdate), string(m.IdempotencyKey)).Scan(&state); err != nil {
						return err
					}
					if state == "completed" {
						completed.Store(true)
						if !afterCommit {
							cancel()
						}
					}
					return nil
				})
				store.mu.Lock()
				store.afterResult = func(cause foundation.TransactionCause, result foundation.CommitResult) {
					if afterCommit && completed.Load() && result.State() == foundation.Committed && cause.Kind() == foundation.CommandsCause && cause.Details().Primary.Canonical() == command.Canonical() {
						cancel()
					}
				}
				store.mu.Unlock()
				before := base.eventCount(t)
				result, err := f.tasks.UpdateTask(ctx, a, m, p.ID, target.ID, wc.TaskFieldsUpdate{Title: &text})
				if !completed.Load() {
					t.Fatal("final callback not reached")
				}
				if afterCommit {
					if err != nil || !result.Changed || base.eventCount(t) != before+1 {
						t.Fatal("known commit rewritten after delivery cancellation", err)
					}
				} else {
					if err == nil || !errors.Is(err, context.Canceled) || base.eventCount(t) != before {
						t.Fatal("pre-COMMIT cancellation did not preserve rollback/cause", err)
					}
				}
			})
		}
	})
}

func TestTaskPlanningConcurrencyAndRank(t *testing.T) {
	f := newTaskFixture(t)
	a := f.human(t, "task-rank", "user")
	p, _, _ := f.create(t, a, "rank")
	m := f.milestone(t, a, p.ID, "m")
	s := f.sprint(t, a, p.ID, m.ID, "s")
	one := f.task(t, a, p.ID, s.ID, "one")
	two := f.task(t, a, p.ID, s.ID, "two")
	three := f.task(t, a, p.ID, s.ID, "three")
	second := f.newTaskService(t, f.events, f.accounts)
	t.Run("same-key-real-final-lock-waiter", func(t *testing.T) {
		writer, store := observedTaskFixture(t, f)
		competitor, competitorStore := observedTaskFixture(t, f)
		title := "same-key"
		cm := meta(t, "same-key", &one.Version)
		command := taskIdentity(t, p.ID, wc.TaskCommandUpdate, cm.IdempotencyKey)
		key, _ := foundation.CommandLock(command)
		observed := observeLock(competitorStore, key, nil)
		reached, release := make(chan struct{}), make(chan struct{})
		var once sync.Once
		var pid atomic.Int32
		var armed atomic.Bool
		store.setAfter(func(ctx context.Context, tx foundation.Tx, cause foundation.TransactionCause) error {
			if cause.Kind() != foundation.CommandsCause || cause.Details().Primary.Canonical() != command.Canonical() || armed.Load() {
				return nil
			}
			x, err := store.InTx(tx)
			if err != nil {
				return err
			}
			var completed bool
			if err = x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_work.task_commands WHERE project_id=$1 AND command_name=$2 AND idempotency_key=$3 AND state='completed')`, p.ID.String(), string(wc.TaskCommandUpdate), string(cm.IdempotencyKey)).Scan(&completed); err != nil {
				return err
			}
			if completed && armed.CompareAndSwap(false, true) {
				var n int32
				if err = x.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&n); err != nil {
					return err
				}
				pid.Store(n)
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
		defer unlock()
		first := callTaskAsync(t, func(ctx context.Context) (wc.TaskMutation, error) {
			return writer.tasks.UpdateTask(ctx, a, cm, p.ID, one.ID, wc.TaskFieldsUpdate{Title: &title})
		})
		awaitTaskStage(t, reached, first)
		next := callTaskAsync(t, func(ctx context.Context) (wc.TaskMutation, error) {
			return competitor.tasks.UpdateTask(ctx, a, cm, p.ID, one.ID, wc.TaskFieldsUpdate{Title: &title})
		})
		attempt := awaitLockAttempt(t, observed)
		waitExactLock(t, f.db, attempt, false, pid.Load())
		unlock()
		x, y := joinTaskReply(t, first), joinTaskReply(t, next)
		if x.err != nil || y.err != nil {
			t.Fatal(x.err, y.err)
		}
		equalTaskMutation(t, x.result, y.result)
		one = x.result.Task
	})
	t.Run("two-services-old-version-one-winner", func(t *testing.T) {
		title1, title2 := "winner-a", "winner-b"
		start := make(chan struct{})
		r1 := callTaskAsync(t, func(ctx context.Context) (wc.TaskMutation, error) {
			<-start
			return f.tasks.UpdateTask(ctx, a, meta(t, "race-a", &two.Version), p.ID, two.ID, wc.TaskFieldsUpdate{Title: &title1})
		})
		r2 := callTaskAsync(t, func(ctx context.Context) (wc.TaskMutation, error) {
			<-start
			return second.UpdateTask(ctx, a, meta(t, "race-b", &two.Version), p.ID, two.ID, wc.TaskFieldsUpdate{Title: &title2})
		})
		close(start)
		x, y := joinTaskReply(t, r1), joinTaskReply(t, r2)
		if (x.err == nil) == (y.err == nil) {
			t.Fatal("old version must have exactly one winner", x.err, y.err)
		}
		if x.err != nil {
			requireCode(t, x.err, foundation.TaskVersionConflict)
			two = y.result.Task
		} else {
			requireCode(t, y.err, foundation.TaskVersionConflict)
			two = x.result.Task
		}
	})
	t.Run("dense-rank-spectators-and-priority", func(t *testing.T) {
		seedTaskDense(t, f, a, p.ID, s.ID, []wc.TaskID{one.ID, two.ID, three.ID})
		beforeOne, err := f.taskReader.GetTask(ctxFor(t), a, p.ID, one.ID)
		if err != nil {
			t.Fatal(err)
		}
		beforeTwo, err := f.taskReader.GetTask(ctxFor(t), a, p.ID, two.ID)
		if err != nil {
			t.Fatal(err)
		}
		g, q := f.generation(t, p.ID, s.ID, wc.TaskPriorityMedium)
		changed, err := second.ReorderTask(ctxFor(t), a, meta(t, "dense", &three.Version), p.ID, three.ID, wc.TaskReorder{BeforeID: &two.ID})
		if err != nil {
			t.Fatal(err)
		}
		afterG, afterQ := f.generation(t, p.ID, s.ID, wc.TaskPriorityMedium)
		if afterG != g+1 || afterQ != q+1 {
			t.Fatal("reorder generation", g, afterG, q, afterQ)
		}
		for _, before := range []wc.Task{beforeOne, beforeTwo} {
			after, err := f.taskReader.GetTask(ctxFor(t), a, p.ID, before.ID)
			if err != nil {
				t.Fatal(err)
			}
			after.ManualRank = before.ManualRank
			if string(jsonBytes(t, after)) != string(jsonBytes(t, before)) {
				t.Fatal("rebalance changed spectator business facts")
			}
		}
		three = changed.Task
		priority := wc.TaskPriorityHigh
		move, err := f.tasks.UpdateTask(ctxFor(t), a, meta(t, "priority", &three.Version), p.ID, three.ID, wc.TaskFieldsUpdate{Priority: &priority})
		if err != nil {
			t.Fatal(err)
		}
		src, query := f.generation(t, p.ID, s.ID, wc.TaskPriorityMedium)
		dst, _ := f.generation(t, p.ID, s.ID, wc.TaskPriorityHigh)
		if src != afterG+1 || dst != 2 || query != afterQ+1 {
			t.Fatal("priority source/target/query generation", src, dst, query)
		}
		three = move.Task
	})
	t.Run("content-replan-preserves-rank", func(t *testing.T) {
		four := f.task(t, a, p.ID, s.ID, "four")
		seedTaskDense(t, f, a, p.ID, s.ID, []wc.TaskID{one.ID, two.ID, four.ID})
		var fired atomic.Bool
		capture := &capturingAppender{Appender: f.events}
		capture.after = func(ctx context.Context, _ identity.Actor, _ event.Event, _ oc.AppendPlan) error {
			if fired.CompareAndSwap(false, true) {
				_, err := second.ReorderTask(ctx, a, meta(t, "interposed-rebalance", &four.Version), p.ID, four.ID, wc.TaskReorder{BeforeID: &two.ID})
				return err
			}
			return nil
		}
		writer := f.newTaskService(t, capture, f.accounts)
		title := "new large content"
		out, err := writer.UpdateTask(ctxFor(t), a, meta(t, "content", &one.Version), p.ID, one.ID, wc.TaskFieldsUpdate{Title: &title})
		if err != nil {
			t.Fatal(err)
		}
		got, err := f.taskReader.GetTask(ctxFor(t), a, p.ID, one.ID)
		if err != nil || got.ManualRank == "00000000000000000000000000000001" || got.ManualRank != out.Task.ManualRank || got.Title != title {
			t.Fatal("content update restored stale rank", err)
		}
		capture.mu.Lock()
		attempts := len(capture.captured)
		capture.mu.Unlock()
		if attempts != 2 {
			t.Fatal("expected one actual applicability replan", attempts)
		}
		one = out.Task
	})
	t.Run("four-object-low-high-density-and-priority-spectators", func(t *testing.T) {
		for _, edge := range []string{"low", "high"} {
			t.Run(edge, func(t *testing.T) {
				sprint := f.sprint(t, a, p.ID, m.ID, "four "+edge)
				items := make([]wc.Task, 4)
				for i := range items {
					items[i] = f.task(t, a, p.ID, sprint.ID, fmt.Sprintf("spectator %d", i))
				}
				seedTaskRanks(t, f, a, p.ID, sprint.ID, wc.TaskPriorityMedium, []wc.TaskID{items[0].ID, items[1].ID, items[2].ID, items[3].ID}, []string{"00000000000000000000000000000001", "00000000000000000000000000000002", "fffffffffffffffffffffffffffffffd", "fffffffffffffffffffffffffffffffe"})
				for i := range items {
					var err error
					items[i], err = f.taskReader.GetTask(ctxFor(t), a, p.ID, items[i].ID)
					if err != nil {
						t.Fatal(err)
					}
				}
				target, anchor := 3, 1
				want := []int{0, 3, 1, 2}
				if edge == "high" {
					target, anchor = 0, 3
					want = []int{1, 2, 0, 3}
				}
				g, q := f.generation(t, p.ID, sprint.ID, wc.TaskPriorityMedium)
				beforeH, beforeE := f.taskCounts(t)
				moved, err := second.ReorderTask(ctxFor(t), a, meta(t, "four-"+edge, &items[target].Version), p.ID, items[target].ID, wc.TaskReorder{BeforeID: &items[anchor].ID})
				if err != nil {
					t.Fatal(err)
				}
				afterG, afterQ := f.generation(t, p.ID, sprint.ID, wc.TaskPriorityMedium)
				if afterG != g+1 || afterQ != q+1 {
					t.Fatal("four-object generation increment", afterG, afterQ)
				}
				page, err := f.taskReader.ListTasks(ctxFor(t), a, p.ID, wc.TaskFilter{SprintID: &sprint.ID}, foundation.DefaultPageRequest())
				if err != nil || len(page.Items) != 4 {
					t.Fatal(err)
				}
				for i, item := range page.Items {
					if item.ID != items[want[i]].ID {
						t.Fatal("rebalance altered logical order")
					}
				}
				for i, before := range items {
					after, err := f.taskReader.GetTask(ctxFor(t), a, p.ID, before.ID)
					if err != nil {
						t.Fatal(err)
					}
					if i != target {
						after.ManualRank = before.ManualRank
						if string(jsonBytes(t, after)) != string(jsonBytes(t, before)) {
							t.Fatal("four-object spectator business fields changed", i)
						}
					}
				}
				h, e := f.taskCounts(t)
				if h != beforeH+1 || e != beforeE+1 || moved.Task.Version != items[target].Version+1 {
					t.Fatal("rebalance produced spectator history")
				}
				// Target group is dense at the upper endpoint: moving priority must
				// preserve every source/target spectator while rebalancing its tail.
				high := wc.TaskPriorityHigh
				dest := make([]wc.Task, 2)
				for i := range dest {
					out, err := second.CreateTask(ctxFor(t), a, meta(t, fmt.Sprintf("dest-%s-%d", edge, i), nil), p.ID, wc.TaskCreate{TaskID: id[wc.Task](t), SprintID: sprint.ID, Title: "high spectator", Type: wc.TaskTypeBug, Priority: high, Description: "spectator description", Plan: "spectator plan"})
					if err != nil {
						t.Fatal(err)
					}
					dest[i] = out.Task
				}
				seedTaskRanks(t, f, a, p.ID, sprint.ID, high, []wc.TaskID{dest[0].ID, dest[1].ID}, []string{"fffffffffffffffffffffffffffffffd", "fffffffffffffffffffffffffffffffe"})
				for i := range dest {
					dest[i], err = f.taskReader.GetTask(ctxFor(t), a, p.ID, dest[i].ID)
					if err != nil {
						t.Fatal(err)
					}
				}
				sourceBefore, err := f.taskReader.ListTasks(ctxFor(t), a, p.ID, wc.TaskFilter{SprintID: &sprint.ID, Priority: taskPriorityPtr(wc.TaskPriorityMedium)}, foundation.DefaultPageRequest())
				if err != nil {
					t.Fatal(err)
				}
				sourceGen, query := f.generation(t, p.ID, sprint.ID, wc.TaskPriorityMedium)
				destGen, _ := f.generation(t, p.ID, sprint.ID, high)
				changed, err := second.UpdateTask(ctxFor(t), a, meta(t, "four-priority-"+edge, &moved.Task.Version), p.ID, moved.Task.ID, wc.TaskFieldsUpdate{Priority: &high})
				if err != nil {
					t.Fatal(err)
				}
				sourceAfter, queryAfter := f.generation(t, p.ID, sprint.ID, wc.TaskPriorityMedium)
				destAfter, _ := f.generation(t, p.ID, sprint.ID, high)
				if sourceAfter != sourceGen+1 || destAfter != destGen+1 || queryAfter != query+1 {
					t.Fatal("priority migration counted rebalance twice")
				}
				for _, before := range append(sourceBefore.Items, dest...) {
					if before.ID == changed.Task.ID {
						continue
					}
					after, err := f.taskReader.GetTask(ctxFor(t), a, p.ID, before.ID)
					if err != nil {
						t.Fatal(err)
					}
					after.ManualRank = before.ManualRank
					if string(jsonBytes(t, after)) != string(jsonBytes(t, before)) {
						t.Fatal("priority destination spectator business fields changed")
					}
				}
				f.ageSession(t, a)
				var beforeActivity string
				if err = f.raw.QueryRow(ctxFor(t), `SELECT last_activity_at::text FROM agenteam_account.sessions WHERE id=$1`, a.Details().SessionID).Scan(&beforeActivity); err != nil {
					t.Fatal(err)
				}
				noopMeta := meta(t, "four-noop-"+edge, &changed.Task.Version)
				noop, err := second.UpdateTask(ctxFor(t), a, noopMeta, p.ID, changed.Task.ID, wc.TaskFieldsUpdate{Priority: &high})
				if err != nil || noop.Changed || string(jsonBytes(t, noop.Task)) != string(jsonBytes(t, changed.Task)) {
					t.Fatal("no-op canonical", err)
				}
				var afterActivity string
				if err = f.raw.QueryRow(ctxFor(t), `SELECT last_activity_at::text FROM agenteam_account.sessions WHERE id=$1`, a.Details().SessionID).Scan(&afterActivity); err != nil || afterActivity == beforeActivity {
					t.Fatal("new no-op did not Touch Activity", err)
				}
				noopGen, noopQuery := f.generation(t, p.ID, sprint.ID, high)
				if noopGen != destAfter || noopQuery != queryAfter {
					t.Fatal("no-op changed generation")
				}
				f.ageSession(t, a)
				stable := f.taskSnapshot(t, a)
				again, err := second.UpdateTask(ctxFor(t), a, noopMeta, p.ID, changed.Task.ID, wc.TaskFieldsUpdate{Priority: &high})
				if err != nil {
					t.Fatal(err)
				}
				equalTaskMutation(t, noop, again)
				if f.taskSnapshot(t, a) != stable {
					t.Fatal("historical no-op replay touched Activity/facts")
				}
			})
		}
	})

	t.Run("three-round-cap-does-not-swallow-forbidden", func(t *testing.T) {
		var rounds atomic.Int32
		capture := &capturingAppender{Appender: f.events}
		capture.after = func(ctx context.Context, _ identity.Actor, _ event.Event, _ oc.AppendPlan) error {
			n := rounds.Add(1)
			_, err := second.CreateTask(ctx, a, meta(t, fmt.Sprintf("invalidate-%d", n), nil), p.ID, wc.TaskCreate{TaskID: id[wc.Task](t), SprintID: s.ID, Title: "invalidate generation", Type: wc.TaskTypeTask, Priority: wc.TaskPriorityMedium})
			return err
		}
		writer := f.newTaskService(t, capture, f.accounts)
		_, err := writer.ReorderTask(ctxFor(t), a, meta(t, "three-rounds", &one.Version), p.ID, one.ID, wc.TaskReorder{})
		requireCode(t, err, foundation.ResourceBusy)
		if rounds.Load() != 3 {
			t.Fatal("shared replan cap", rounds.Load())
		}
		capture = &capturingAppender{Appender: f.events, before: func(context.Context, identity.Actor, event.Event) error {
			return foundation.NewFault(foundation.Forbidden, foundation.NotStarted)
		}}
		writer = f.newTaskService(t, capture, f.accounts)
		title := "forbidden prepare"
		_, err = writer.UpdateTask(ctxFor(t), a, meta(t, "prepare-forbidden", &one.Version), p.ID, one.ID, wc.TaskFieldsUpdate{Title: &title})
		requireCode(t, err, foundation.Forbidden)
	})
}

func seedTaskDense(t *testing.T, f *taskFixture, a identity.Actor, p c.ProjectID, s wc.SprintID, ids []wc.TaskID) {
	t.Helper()
	schedule, _ := foundation.ProjectScheduleLock(p.String())
	group, _ := foundation.RankGroupLock("work.task:" + p.String() + ":" + s.String() + ":backlog:medium")
	f.tx(t, fixtureLocks(a, p, foundation.LockRequest{Key: schedule, Mode: foundation.Exclusive}, foundation.LockRequest{Key: group, Mode: foundation.Exclusive}), func(ctx context.Context, tx foundation.Tx, x postgres.SQLExecutor) error {
		if _, err := f.projectAuthority.RequireOwnerInTx(ctx, tx, a, p, identity.Mutate); err != nil {
			return err
		}
		ranks := []string{"00000000000000000000000000000001", "00000000000000000000000000000002", "fffffffffffffffffffffffffffffffe"}
		for i, id := range ids {
			if _, err := x.Exec(ctx, `UPDATE agenteam_work.tasks SET manual_rank=$2 WHERE id=$1`, id.String(), ranks[i]); err != nil {
				return err
			}
		}
		if _, err := x.Exec(ctx, `UPDATE agenteam_work.task_order_groups SET order_generation=order_generation+1 WHERE project_id=$1 AND sprint_id=$2 AND state='backlog' AND priority='medium'`, p.String(), s.String()); err != nil {
			return err
		}
		_, err := x.Exec(ctx, `UPDATE agenteam_work.task_query_generations SET query_generation=query_generation+1 WHERE project_id=$1`, p.String())
		return err
	})
	t.Log("test-only dense physical rank input, no business version/time changes")
}

func TestTaskPlanningMembership(t *testing.T) {
	f := newTaskFixture(t)
	a := f.human(t, "task-membership", "user")
	p, _, _ := f.create(t, a, "membership")
	m := f.milestone(t, a, p.ID, "m")
	s := f.sprint(t, a, p.ID, m.ID, "s")
	schedule, _ := foundation.ProjectScheduleLock(p.ID.String())
	sprint, _ := foundation.AggregateLock(foundation.SprintAggregate, s.ID.String())
	user, _ := foundation.UserLock(a.Details().UserID)
	projectKey, _ := foundation.ProjectLock(p.ID.String())
	locks := []foundation.LockRequest{{Key: user, Mode: foundation.Shared}, {Key: projectKey, Mode: foundation.Shared}, {Key: schedule, Mode: foundation.Exclusive}, {Key: sprint, Mode: foundation.Shared}}
	var ended foundation.Tx
	f.tx(t, locks, func(ctx context.Context, tx foundation.Tx, _ postgres.SQLExecutor) error {
		ended = tx
		yes, err := f.taskReader.HasTasksInSprintInTx(ctx, tx, a, p.ID, s.ID)
		if err == nil && yes {
			return errors.New("empty sprint falsely occupied")
		}
		return err
	})
	if yes, err := f.taskReader.HasTasksInSprintInTx(ctxFor(t), ended, a, p.ID, s.ID); err == nil || yes {
		t.Fatal("ended Tx accepted")
	}
	failed := f.store.WithinTx(ctxFor(t), cause(t), func(ctx context.Context, tx foundation.Tx) error {
		yes, err := f.taskReader.HasTasksInSprintInTx(ctx, tx, a, p.ID, s.ID)
		if yes {
			t.Error("missing-lock returned true")
		}
		return err
	})
	if failed.State() != foundation.NotCommitted {
		t.Fatal("missing locks accepted")
	}
	foreign := openStore(t, f.db.Config(t, nil))
	failed = foreign.WithinTx(ctxFor(t), cause(t), func(ctx context.Context, tx foundation.Tx) error {
		_, err := f.taskReader.HasTasksInSprintInTx(ctx, tx, a, p.ID, s.ID)
		return err
	})
	if failed.State() != foundation.NotCommitted {
		t.Fatal("foreign Store accepted")
	}
	// Hold the membership caller's exact Schedule EX across both reads.
	competitor, store := observedTaskFixture(t, f)
	observed := observeLock(store, user, nil)
	reached, release, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var pid int32
	var result foundation.CommitResult
	var once sync.Once
	unlock := func() { once.Do(func() { close(release) }) }
	defer unlock()
	go func() {
		defer close(done)
		result = f.store.WithinTx(ctx, cause(t), func(ctx context.Context, tx foundation.Tx) error {
			if err := f.store.AcquireAll(ctx, tx, locks); err != nil {
				return err
			}
			x, err := f.store.InTx(tx)
			if err != nil {
				return err
			}
			if err = x.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&pid); err != nil {
				return err
			}
			yes, err := f.taskReader.HasTasksInSprintInTx(ctx, tx, a, p.ID, s.ID)
			if err != nil {
				return err
			}
			if yes {
				return errors.New("initial membership not empty")
			}
			close(reached)
			select {
			case <-release:
			case <-ctx.Done():
				return ctx.Err()
			}
			yes, err = f.taskReader.HasTasksInSprintInTx(ctx, tx, a, p.ID, s.ID)
			if err == nil && yes {
				return errors.New("membership phantom under Schedule EX")
			}
			return err
		})
	}()
	t.Cleanup(func() { cancel(); unlock(); await(t, done) })
	await(t, reached)
	request := wc.TaskCreate{TaskID: id[wc.Task](t), SprintID: s.ID, Title: "membership contender", Type: wc.TaskTypeTask, Priority: wc.TaskPriorityMedium}
	reply := callTaskAsync(t, func(ctx context.Context) (wc.TaskMutation, error) {
		return competitor.tasks.CreateTask(ctx, a, meta(t, "membership-create", nil), p.ID, request)
	})
	attempt := awaitLockAttempt(t, observed)
	waitExactLock(t, f.db, attempt, false, pid)
	unlock()
	await(t, done)
	if result.State() != foundation.Committed {
		t.Fatal("membership Tx", result.Fault())
	}
	created := joinTaskReply(t, reply)
	if created.err != nil {
		t.Fatal(created.err)
	}
	for _, state := range []wc.TaskState{wc.TaskStateBacklog, wc.TaskStateTodo, wc.TaskStateInProgress, wc.TaskStateInReview, wc.TaskStateBlocked, wc.TaskStateDone, wc.TaskStateCancelled} {
		agent := id[identity.Agent](t)
		f.seedTaskState(t, a, created.result.Task, state, &agent)
		f.tx(t, locks, func(ctx context.Context, tx foundation.Tx, _ postgres.SQLExecutor) error {
			yes, err := f.taskReader.HasTasksInSprintInTx(ctx, tx, a, p.ID, s.ID)
			if err == nil && !yes {
				return errors.New("non-backlog Task was ignored")
			}
			return err
		})
	}
	t.Run("missing-or-foreign-sprint-and-insufficient-schedule", func(t *testing.T) {
		foreignProject, _, _ := f.create(t, a, "membership-foreign")
		foreignMilestone := f.milestone(t, a, foreignProject.ID, "m")
		foreignSprint := f.sprint(t, a, foreignProject.ID, foreignMilestone.ID, "s")
		for _, target := range []wc.SprintID{id[c.Sprint](t), foreignSprint.ID} {
			sprintKey, _ := foundation.AggregateLock(foundation.SprintAggregate, target.String())
			requested := []foundation.LockRequest{{Key: user, Mode: foundation.Shared}, {Key: projectKey, Mode: foundation.Shared}, {Key: schedule, Mode: foundation.Exclusive}, {Key: sprintKey, Mode: foundation.Shared}}
			result := f.store.WithinTx(ctxFor(t), cause(t), func(ctx context.Context, tx foundation.Tx) error {
				if err := f.store.AcquireAll(ctx, tx, requested); err != nil {
					return err
				}
				yes, err := f.taskReader.HasTasksInSprintInTx(ctx, tx, a, p.ID, target)
				if yes {
					t.Error("invalid Sprint returned true")
				}
				return err
			})
			if result.State() != foundation.NotCommitted {
				t.Fatal("invalid Sprint accepted")
			}
			requireCode(t, result.Fault(), foundation.TaskSprintInvalid)
		}
		weak := append([]foundation.LockRequest(nil), locks...)
		for i := range weak {
			if foundation.CompareLockKeys(weak[i].Key, schedule) == 0 {
				weak[i].Mode = foundation.Shared
			}
		}
		result := f.store.WithinTx(ctxFor(t), cause(t), func(ctx context.Context, tx foundation.Tx) error {
			if err := f.store.AcquireAll(ctx, tx, weak); err != nil {
				return err
			}
			yes, err := f.taskReader.HasTasksInSprintInTx(ctx, tx, a, p.ID, s.ID)
			if yes {
				t.Error("Schedule SH returned true")
			}
			return err
		})
		if result.State() != foundation.NotCommitted {
			t.Fatal("Schedule SH satisfied required EX")
		}
	})
	t.Run("SQL-and-context-failure-never-mean-empty", func(t *testing.T) {
		for _, failure := range []string{"missing-table", "cancelled-context"} {
			result := f.store.WithinTx(ctxFor(t), cause(t), func(ctx context.Context, tx foundation.Tx) error {
				if err := f.store.AcquireAll(ctx, tx, locks); err != nil {
					return err
				}
				readCtx := ctx
				if failure == "missing-table" {
					x, err := f.store.InTx(tx)
					if err != nil {
						return err
					}
					if _, err = x.Exec(ctx, `ALTER TABLE agenteam_work.tasks RENAME TO tasks_fixture_hidden`); err != nil {
						return err
					}
				} else {
					var cancel context.CancelFunc
					readCtx, cancel = context.WithCancel(ctx)
					cancel()
				}
				yes, err := f.taskReader.HasTasksInSprintInTx(readCtx, tx, a, p.ID, s.ID)
				if yes || err == nil {
					t.Error("membership failure became legal empty/true", failure)
				}
				if failure == "cancelled-context" && !errors.Is(err, context.Canceled) {
					t.Error("membership cancellation cause lost", err)
				}
				return err
			})
			if result.State() != foundation.NotCommitted {
				t.Fatal("membership failure committed", failure, result.Fault())
			}
		}
		// The failed same-Tx DDL is really rolled back, not repaired out-of-band.
		f.tx(t, locks, func(ctx context.Context, tx foundation.Tx, _ postgres.SQLExecutor) error {
			yes, err := f.taskReader.HasTasksInSprintInTx(ctx, tx, a, p.ID, s.ID)
			if err == nil && !yes {
				return errors.New("table rollback lost canonical member")
			}
			return err
		})
	})

	t.Run("create-first-membership-real-shared-waiter", func(t *testing.T) {
		sprint := f.sprint(t, a, p.ID, m.ID, "inverse")
		writer, store := observedTaskFixture(t, f)
		reader, readerStore := observedTaskFixture(t, f)
		cm := meta(t, "inverse-create", nil)
		command := taskIdentity(t, p.ID, wc.TaskCommandCreate, cm.IdempotencyKey)
		reached, release := make(chan struct{}), make(chan struct{})
		var once sync.Once
		unlock := func() { once.Do(func() { close(release) }) }
		defer unlock()
		var backend atomic.Int32
		var armed atomic.Bool
		store.setAfter(func(ctx context.Context, tx foundation.Tx, cause foundation.TransactionCause) error {
			if cause.Kind() != foundation.CommandsCause || cause.Details().Primary.Canonical() != command.Canonical() || armed.Load() {
				return nil
			}
			x, err := store.InTx(tx)
			if err != nil {
				return err
			}
			var completed bool
			if err = x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_work.task_commands WHERE project_id=$1 AND command_name=$2 AND idempotency_key=$3 AND state='completed')`, p.ID.String(), string(wc.TaskCommandCreate), string(cm.IdempotencyKey)).Scan(&completed); err != nil {
				return err
			}
			if completed && armed.CompareAndSwap(false, true) {
				var pid int32
				if err = x.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&pid); err != nil {
					return err
				}
				backend.Store(pid)
				close(reached)
				select {
				case <-release:
				case <-ctx.Done():
					return ctx.Err()
				}
			}
			return nil
		})
		original := callTaskAsync(t, func(ctx context.Context) (wc.TaskMutation, error) {
			return writer.tasks.CreateTask(ctx, a, cm, p.ID, wc.TaskCreate{TaskID: id[wc.Task](t), SprintID: sprint.ID, Title: "inverse winner", Type: wc.TaskTypeTask, Priority: wc.TaskPriorityMedium})
		})
		awaitTaskStage(t, reached, original)
		observation := observeLock(readerStore, user, nil)
		sprintKey, _ := foundation.AggregateLock(foundation.SprintAggregate, sprint.ID.String())
		requested := []foundation.LockRequest{{Key: user, Mode: foundation.Shared}, {Key: projectKey, Mode: foundation.Shared}, {Key: schedule, Mode: foundation.Exclusive}, {Key: sprintKey, Mode: foundation.Shared}}
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		joined := make(chan struct{})
		var result foundation.CommitResult
		var yes bool
		readCause := cause(t)
		go func() {
			defer close(joined)
			result = reader.store.WithinTx(ctx, readCause, func(ctx context.Context, tx foundation.Tx) error {
				if err := reader.store.AcquireAll(ctx, tx, requested); err != nil {
					return err
				}
				var err error
				yes, err = reader.taskReader.HasTasksInSprintInTx(ctx, tx, a, p.ID, sprint.ID)
				return err
			})
		}()
		t.Cleanup(func() { cancel(); unlock(); await(t, joined) })
		var attempt lockAttempt
		select {
		case attempt = <-observation:
		case <-time.After(5 * time.Second):
			t.Fatal("membership caller did not reach actual AcquireAll")
		}
		waitTaskExactMode(t, f, attempt, foundation.Shared, backend.Load())
		unlock()
		created := joinTaskReply(t, original)
		await(t, joined)
		if created.err != nil || result.State() != foundation.Committed || !yes {
			t.Fatal("membership failed to see committed winning create", created.err, result.Fault(), yes)
		}
	})

	t.Log("membership false/true is only same-Tx occupancy, no DeleteSprint is invoked")
}

// A SharedLock waiter is observed from the membership caller's own Tx, with
// its entire official key and the exact known writer as blocker.
func waitTaskExactMode(t *testing.T, f *taskFixture, attempt lockAttempt, mode foundation.LockMode, blocker int32) {
	t.Helper()
	if attempt.BackendPID <= 0 || attempt.Request.Mode != mode || blocker <= 0 {
		t.Fatal("invalid exact waiter premise")
	}
	name := "ShareLock"
	if mode == foundation.Exclusive {
		name = "ExclusiveLock"
	}
	conn := f.db.Connect(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	key := uint64(attempt.Request.Key.AdvisoryKey())
	for {
		var found bool
		err := conn.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_locks l JOIN pg_stat_activity a ON a.pid=l.pid WHERE a.datname=$1 AND l.locktype='advisory' AND l.classid::bigint=$2 AND l.objid::bigint=$3 AND l.objsubid=1 AND l.pid=$4 AND l.mode=$5 AND NOT l.granted AND $6=ANY(pg_blocking_pids(l.pid)))`, f.db.Name, int64(key>>32), int64(key&0xffffffff), attempt.BackendPID, name, blocker).Scan(&found)
		if err != nil {
			t.Fatal(err)
		}
		if found {
			t.Log("exact caller waiter", attempt.BackendPID, attempt.Request.Key.AdvisoryKey(), name, "blocker", blocker)
			return
		}
		select {
		case <-ctx.Done():
			t.Fatal("exact shared waiter not observed")
		case <-time.After(5 * time.Millisecond):
		}
	}
}

func seedTaskRanks(t *testing.T, f *taskFixture, a identity.Actor, p c.ProjectID, s wc.SprintID, priority wc.TaskPriority, ids []wc.TaskID, ranks []string) {
	t.Helper()
	if len(ids) != len(ranks) {
		t.Fatal("rank fixture length")
	}
	schedule, _ := foundation.ProjectScheduleLock(p.String())
	group, _ := foundation.RankGroupLock("work.task:" + p.String() + ":" + s.String() + ":backlog:" + string(priority))
	f.tx(t, fixtureLocks(a, p, foundation.LockRequest{Key: schedule, Mode: foundation.Exclusive}, foundation.LockRequest{Key: group, Mode: foundation.Exclusive}), func(ctx context.Context, tx foundation.Tx, x postgres.SQLExecutor) error {
		if _, err := f.projectAuthority.RequireOwnerInTx(ctx, tx, a, p, identity.Mutate); err != nil {
			return err
		}
		for i, id := range ids {
			if _, err := x.Exec(ctx, `UPDATE agenteam_work.tasks SET manual_rank=$2 WHERE id=$1`, id.String(), ranks[i]); err != nil {
				return err
			}
		}
		if _, err := x.Exec(ctx, `UPDATE agenteam_work.task_order_groups SET order_generation=order_generation+1 WHERE project_id=$1 AND sprint_id=$2 AND state='backlog' AND priority=$3`, p.String(), s.String(), string(priority)); err != nil {
			return err
		}
		_, err := x.Exec(ctx, `UPDATE agenteam_work.task_query_generations SET query_generation=query_generation+1 WHERE project_id=$1`, p.String())
		return err
	})
	t.Log("test-owned dense ranks preserved all business columns", len(ids), priority)
}

func taskPriorityPtr(priority wc.TaskPriority) *wc.TaskPriority { return &priority }

// Discovery deliberately has no persistent command. Observation hooks must
// leave it alone and a writer returning before the requested stage is an
// immediate diagnostic failure, never an eight-second generic timeout.
func awaitTaskStage(t *testing.T, reached <-chan struct{}, reply <-chan taskReply) {
	t.Helper()
	select {
	case <-reached:
		return
	case got := <-reply:
		t.Fatalf("Task writer returned before observed stage: changed=%t err=%v", got.result.Changed, got.err)
	case <-time.After(8 * time.Second):
		t.Fatal("owned Task stage handshake did not arrive")
	}
}
