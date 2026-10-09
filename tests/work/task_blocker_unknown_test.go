//go:build integration

package work_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	wc "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
)

func TestTaskBlockerUnknown(t *testing.T) {
	base := newBlockerFixture(t)
	a := base.human(t, "blocker-unknown", "user")
	p, _, _ := base.create(t, a, "blocker-unknown")
	m := base.milestone(t, a, p.ID, "m")
	s := base.sprint(t, a, p.ID, m.ID, "s")
	target := base.task(t, a, p.ID, s.ID, "target")
	for _, phase := range []string{"planned", "completed"} {
		for _, commit := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/commit=%t", phase, commit), func(t *testing.T) {
				tf, store, proxy := proxyTaskFixture(t, base.taskFixture, commit)
				f := attachBlockers(t, tf)
				current, err := base.taskReader.GetTask(ctxFor(t), a, p.ID, target.ID)
				if err != nil {
					t.Fatal(err)
				}
				request := blockerWaiting(t, fmt.Sprintf("%s-%t", phase, commit))
				cm := meta(t, id[struct{}](t).String(), &current.Version)
				digest, err := wc.TaskBlockerAddDigest(a, cm, p.ID, target.ID, request)
				if err != nil {
					t.Fatal(err)
				}
				query := wc.TaskBlockerCommandLookupRequest{ProjectID: p.ID, Command: wc.TaskBlockerCommandAdd, IdempotencyKey: cm.IdempotencyKey, SemanticDigest: digest}
				command, err := wc.TaskBlockerCommandIdentity(p.ID, wc.TaskBlockerCommandAdd, cm.IdempotencyKey)
				if err != nil {
					t.Fatal(err)
				}
				key, _ := foundation.CommandLock(command)
				confirmationAttempts := observeLock(store, key, func() bool {
					select {
					case <-proxy.reached:
						return true
					default:
						return false
					}
				})
				lookupFixture, lookupStore := observedBlockerFixture(t, base)
				lookupAttempts := observeLock(lookupStore, key, nil)
				var armed atomic.Bool
				var backend atomic.Int32
				originalResults := make(chan foundation.CommitResult, 1)
				store.setAfter(func(ctx context.Context, tx foundation.Tx, cause foundation.TransactionCause) error {
					if cause.Kind() != foundation.CommandsCause || cause.Details().Primary.Canonical() != command.Canonical() || armed.Load() {
						return nil
					}
					x, err := store.InTx(tx)
					if err != nil {
						return err
					}
					var state string
					if err = x.QueryRow(ctx, `SELECT coalesce((SELECT state FROM agenteam_work.task_blocker_commands WHERE project_id=$1 AND command_name=$2 AND idempotency_key=$3),'')`, p.ID.String(), string(wc.TaskBlockerCommandAdd), string(cm.IdempotencyKey)).Scan(&state); err != nil {
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
				store.mu.Lock()
				store.afterResult = func(cause foundation.TransactionCause, result foundation.CommitResult) {
					if cause.Kind() == foundation.CommandsCause && cause.Details().Primary.Canonical() == command.Canonical() && result.State() == foundation.Unknown {
						select {
						case originalResults <- result:
						default:
						}
					}
				}
				store.mu.Unlock()
				var once sync.Once
				release := func() { once.Do(func() { close(proxy.release) }) }
				defer release()
				before := base.blockerSnapshot(t, a)
				writer := callBlockerAsync(t, func(ctx context.Context) (wc.TaskBlockerMutation, error) {
					return f.blockers.AddTaskBlocker(ctx, a, cm, p.ID, target.ID, request)
				})
				awaitBlockerStage(t, proxy.reached, writer)
				if backend.Load() <= 0 || proxy.backendPID.Load() != backend.Load() {
					t.Fatal("COMMIT proxy did not bind exact physical writer")
				}
				confirmation := awaitLockAttempt(t, confirmationAttempts)
				waitExactLock(t, base.db, confirmation, false, backend.Load())
				type lookupReply struct {
					value wc.TaskBlockerCommandLookup
					err   error
				}
				lookupDone := make(chan lookupReply, 1)
				joined := make(chan struct{})
				lookupContext, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				go func() {
					defer close(joined)
					value, err := lookupFixture.blockers.LookupTaskBlockerCommand(lookupContext, a, query)
					lookupDone <- lookupReply{value, err}
				}()
				t.Cleanup(func() { cancel(); await(t, joined) })
				lookupAttempt := awaitLockAttempt(t, lookupAttempts)
				if lookupAttempt.BackendPID == backend.Load() || lookupAttempt.BackendPID == confirmation.BackendPID {
					t.Fatal("Lookup did not use independent physical Tx")
				}
				waitExactLock(t, base.db, lookupAttempt, false, backend.Load())
				cancel()
				await(t, joined)
				looked := <-lookupDone
				if !errors.Is(looked.err, context.Canceled) || looked.value.Status != "" || looked.value.Receipt != nil {
					t.Fatal("cancelled Lookup reported a false absence/result", looked.err)
				}
				got := joinBlockerReply(t, writer)
				requireCode(t, got.err, foundation.CommitUnknown)
				var fault *foundation.Fault
				if !errors.As(got.err, &fault) || fault.CommitState != foundation.Unknown || fault.RetryHint != "lookup" {
					t.Fatal("Unknown outcome rewritten")
				}
				select {
				case original := <-originalResults:
					if original.AttemptID().Validate() != nil || fault.CauseID != original.AttemptID().String() || original.Cause().Details().Primary.Canonical() != command.Canonical() {
						t.Fatal("original physical attempt/cause lost")
					}
				default:
					t.Fatal("writer Unknown was not actually observed")
				}
				if base.blockerSnapshot(t, a) != before {
					t.Fatal("held COMMIT exposed a durable mutation")
				}
				release()
				await(t, proxy.completed)
				observed, err := base.blockers.LookupTaskBlockerCommand(ctxFor(t), a, query)
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
				recovered, err := base.blockers.AddTaskBlocker(ctxFor(t), fresh, cm, p.ID, target.ID, request)
				if err != nil || recovered.Blocker.ID != request.BlockerID || recovered.Task.Version != current.Version+1 {
					t.Fatal("original-key recovery", err)
				}
				if observed.Receipt != nil {
					equalBlockerMutation(t, *observed.Receipt, recovered)
				}
				stable := base.blockerSnapshot(t, fresh)
				replay, err := base.blockers.AddTaskBlocker(ctxFor(t), fresh, cm, p.ID, target.ID, request)
				if err != nil {
					t.Fatal(err)
				}
				equalBlockerMutation(t, recovered, replay)
				if base.blockerSnapshot(t, fresh) != stable {
					t.Fatal("explicit recovery duplicated facts/Activity")
				}
				var count int
				if err = base.raw.QueryRow(ctxFor(t), `SELECT count(*) FROM agenteam_work.task_blocker_commands WHERE project_id=$1 AND command_name=$2 AND idempotency_key=$3`, p.ID.String(), string(wc.TaskBlockerCommandAdd), string(cm.IdempotencyKey)).Scan(&count); err != nil || count != 1 {
					t.Fatal("recovery replaced original operation identity", err)
				}
			})
		}
	}
	t.Run("stopped-active-lock-waiter-actually-joins", func(t *testing.T) {
		f, store := observedBlockerFixture(t, base)
		current, err := base.taskReader.GetTask(ctxFor(t), a, p.ID, target.ID)
		if err != nil {
			t.Fatal(err)
		}
		schedule, _ := foundation.ProjectScheduleLock(p.ID.String())
		attempts := observeLock(store, schedule, nil)
		release, pid := holdLocks(t, base.fixture, []foundation.LockRequest{{Key: schedule, Mode: foundation.Exclusive}})
		defer release()
		request := blockerWaiting(t, "stopped")
		cm := meta(t, "stopped", &current.Version)
		writer := callBlockerAsync(t, func(ctx context.Context) (wc.TaskBlockerMutation, error) {
			return f.blockers.AddTaskBlocker(ctx, a, cm, p.ID, target.ID, request)
		})
		waitTaskExactMode(t, base.taskFixture, awaitLockAttempt(t, attempts), foundation.Exclusive, pid)
		f.blockers.Stop()
		got := joinBlockerReply(t, writer)
		if !errors.Is(got.err, context.Canceled) {
			t.Fatal("Stop lost active caller cancellation", got.err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err = f.blockers.Drain(ctx); err != nil {
			t.Fatal("Drain did not join actual call/Tx", err)
		}
		_, err = f.blockers.AddTaskBlocker(ctxFor(t), a, cm, p.ID, target.ID, request)
		requireCode(t, err, foundation.ShuttingDown)
		release()
		digest, err := wc.TaskBlockerAddDigest(a, cm, p.ID, target.ID, request)
		if err != nil {
			t.Fatal(err)
		}
		lookup, err := base.blockers.LookupTaskBlockerCommand(ctxFor(t), a, wc.TaskBlockerCommandLookupRequest{ProjectID: p.ID, Command: wc.TaskBlockerCommandAdd, IdempotencyKey: cm.IdempotencyKey, SemanticDigest: digest})
		if err != nil || lookup.Status != wc.LookupNotObserved {
			t.Fatal("stopped admission created command", err)
		}
	})
}
