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

	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	wc "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
)

// B asserts atomic canonical facts across real rollback, old planning, and
// complete PostgreSQL COMMIT frame loss. Proxy mechanics are from the accepted
// old fixture; expected Blocker outcomes come from the new service contract.
func TestIndependentTaskBlockerRuntimeB(t *testing.T) {
	base := newTaskFixture(t)
	e := ibAttach(t, base)
	a := base.human(t, "ib-b-owner", "user")
	p, _, _ := base.create(t, a, "ib-b")
	m := base.milestone(t, a, p.ID, "m")
	s := base.sprint(t, a, p.ID, m.ID, "s")
	for _, point := range []string{"outbox", "activity"} {
		t.Run("rollback_"+point, func(t *testing.T) {
			target := base.task(t, a, p.ID, s.ID, "rollback-"+point)
			req := ibWait(t)
			cmd := meta(t, "rollback-"+point, &target.Version)
			before := ibSnapshot(t, base, a)
			g, q := base.generation(t, p.ID, s.ID, target.Priority)
			svc := e.newService(t, &capturingAppender{Appender: e.events, failAppend: true}, base.accounts)
			if point == "activity" {
				svc = e.newService(t, e.events, failActivity{base.accounts})
			}
			out, err := svc.AddTaskBlocker(ctxFor(t), a, cmd, p.ID, target.ID, req)
			requireCode(t, err, f.DependencyUnavailable)
			if out.Task.ID.Validate() == nil || ibSnapshot(t, base, a) != before {
				t.Fatal("failed final Tx published facts or receipt")
			}
			observed, err := e.service.LookupTaskBlockerCommand(ctxFor(t), a, ibLookup(t, a, cmd, p.ID, target.ID, req))
			if err != nil || observed.Status != wc.LookupInProgress || observed.Receipt != nil {
				t.Fatal("rolled-back final lost prepared command", err)
			}
			out, err = e.service.AddTaskBlocker(ctxFor(t), a, cmd, p.ID, target.ID, req)
			if err != nil {
				t.Fatal(err)
			}
			ibFact(t, e, a, out)
			ag, aq := base.generation(t, p.ID, s.ID, target.Priority)
			if out.Task.ManualRank != target.ManualRank || ag != g || aq != q+1 {
				t.Fatal("Blocker changed ordering or query generation incorrectly")
			}
		})
	}
	t.Run("old_planning_wins_version_race", func(t *testing.T) {
		target := base.task(t, a, p.ID, s.ID, "planning-wins")
		cmd := meta(t, "planning-wins-blocker", &target.Version)
		req := ibWait(t)
		hit, release, free := ibGate(t)
		defer free()
		cap := &capturingAppender{Appender: e.events, after: func(ctx context.Context, _ i.Actor, _ event.Event, _ oc.AppendPlan) error {
			return ibPause(ctx, hit, release)
		}}
		svc := e.newService(t, cap, base.accounts)
		pending := ibAsync(t, func(ctx context.Context) (wc.TaskBlockerMutation, error) {
			return svc.AddTaskBlocker(ctx, a, cmd, p.ID, target.ID, req)
		})
		ibStage(t, hit, pending)
		title := "canonical planning winner"
		winner, err := base.tasks.UpdateTask(ctxFor(t), a, meta(t, "planning-title-winner", &target.Version), p.ID, target.ID, wc.TaskFieldsUpdate{Title: &title})
		if err != nil {
			t.Fatal(err)
		}
		before := ibSnapshot(t, base, a)
		free()
		loser := ibJoin(t, pending)
		requireCode(t, loser.err, f.TaskVersionConflict)
		if ibSnapshot(t, base, a) != before || winner.Task.Version != target.Version+1 {
			t.Fatal("stale Blocker overwrote planning")
		}
	})
	t.Run("blocker_wins_old_reorder", func(t *testing.T) {
		anchor := base.task(t, a, p.ID, s.ID, "reorder-anchor")
		target := base.task(t, a, p.ID, s.ID, "reorder-loser")
		hit, release, free := ibGate(t)
		defer free()
		cap := &capturingAppender{Appender: base.events, after: func(ctx context.Context, _ i.Actor, _ event.Event, _ oc.AppendPlan) error {
			return ibPause(ctx, hit, release)
		}}
		planning := base.newTaskService(t, cap, base.accounts)
		pending := callTaskAsync(t, func(ctx context.Context) (wc.TaskMutation, error) {
			return planning.ReorderTask(ctx, a, meta(t, "reorder-old-version", &target.Version), p.ID, target.ID, wc.TaskReorder{BeforeID: &anchor.ID})
		})
		awaitTaskStage(t, hit, pending)
		g, q := base.generation(t, p.ID, s.ID, target.Priority)
		winner, err := e.service.AddTaskBlocker(ctxFor(t), a, meta(t, "blocker-before-reorder", &target.Version), p.ID, target.ID, ibWait(t))
		if err != nil {
			t.Fatal(err)
		}
		before := ibSnapshot(t, base, a)
		free()
		loser := joinTaskReply(t, pending)
		requireCode(t, loser.err, f.TaskVersionConflict)
		if ibSnapshot(t, base, a) != before {
			t.Fatal("old reorder overwrote Blocker")
		}
		ag, aq := base.generation(t, p.ID, s.ID, target.Priority)
		if ag != g || aq != q+1 || winner.Task.ManualRank != target.ManualRank {
			t.Fatal("Blocker winner changed rank/order")
		}
		ibFact(t, e, a, winner)
	})
	t.Run("sibling_rebalance_requires_fresh_rank", func(t *testing.T) {
		// A separate real group lets the accepted fixture establish adjacent ranks.
		rs := base.sprint(t, a, p.ID, m.ID, "dense")
		one := base.task(t, a, p.ID, rs.ID, "dense-one")
		two := base.task(t, a, p.ID, rs.ID, "dense-two")
		three := base.task(t, a, p.ID, rs.ID, "dense-three")
		seedTaskDense(t, base, a, p.ID, rs.ID, []wc.TaskID{one.ID, two.ID, three.ID})
		one, err := base.taskReader.GetTask(ctxFor(t), a, p.ID, one.ID)
		if err != nil {
			t.Fatal(err)
		}
		var once atomic.Bool
		var spectator wc.Task
		cap := &capturingAppender{Appender: e.events, after: func(ctx context.Context, _ i.Actor, _ event.Event, _ oc.AppendPlan) error {
			if !once.CompareAndSwap(false, true) {
				return nil
			}
			_, err := base.tasks.ReorderTask(ctx, a, meta(t, "independent-rebalance", &three.Version), p.ID, three.ID, wc.TaskReorder{BeforeID: &two.ID})
			if err != nil {
				return err
			}
			spectator, err = base.taskReader.GetTask(ctx, a, p.ID, one.ID)
			return err
		}}
		svc := e.newService(t, cap, base.accounts)
		g, q := base.generation(t, p.ID, rs.ID, one.Priority)
		out, err := svc.AddTaskBlocker(ctxFor(t), a, meta(t, "rebalance-blocker", &one.Version), p.ID, one.ID, ibWait(t))
		if err != nil {
			t.Fatal(err)
		}
		if spectator.ManualRank == one.ManualRank || spectator.Version != one.Version || out.Task.ManualRank != spectator.ManualRank || out.Task.Version != one.Version+1 {
			t.Fatal("Blocker restored stale spectator rank or refreshed client version")
		}
		cap.mu.Lock()
		attempts := len(cap.captured)
		cap.mu.Unlock()
		if attempts != 2 {
			t.Fatal("rank invalidation did not cause exactly one applicability replan", attempts)
		}
		ag, aq := base.generation(t, p.ID, rs.ID, one.Priority)
		if ag != g+1 || aq != q+2 {
			t.Fatal("replan duplicated query/order generations")
		}
		ibFact(t, e, a, out)
	})
	for _, phase := range []string{"planned", "completed"} {
		for _, commit := range []bool{false, true} {
			t.Run(fmt.Sprintf("original_commit_unknown_%s_%t", phase, commit), func(t *testing.T) {
				target := base.task(t, a, p.ID, s.ID, "unknown")
				proxied, hook, proxy := proxyTaskFixture(t, base, commit)
				pe := ibAttach(t, proxied)
				req := ibWait(t)
				cmd := meta(t, id[struct{}](t).String(), &target.Version)
				lookup := ibLookup(t, a, cmd, p.ID, target.ID, req)
				identity, err := wc.TaskBlockerCommandIdentity(p.ID, wc.TaskBlockerCommandAdd, cmd.IdempotencyKey)
				if err != nil {
					t.Fatal(err)
				}
				key, err := f.CommandLock(identity)
				if err != nil {
					t.Fatal(err)
				}
				confirmation := observeLock(hook, key, func() bool {
					select {
					case <-proxy.reached:
						return true
					default:
						return false
					}
				})
				lookupFixture, lookupHook := observedTaskFixture(t, base)
				le := ibAttach(t, lookupFixture)
				lookupAttempt := observeLock(lookupHook, key, nil)
				var armed atomic.Bool
				var backend atomic.Int32
				hook.setAfter(func(ctx context.Context, tx f.Tx, cause f.TransactionCause) error {
					if cause.Kind() != f.CommandsCause || cause.Details().Primary.Canonical() != identity.Canonical() || armed.Load() {
						return nil
					}
					x, err := hook.InTx(tx)
					if err != nil {
						return err
					}
					var state string
					if err = x.QueryRow(ctx, `SELECT coalesce((SELECT state FROM agenteam_work.task_blocker_commands WHERE project_id=$1 AND command_name=$2 AND idempotency_key=$3),'')`, p.ID.String(), string(wc.TaskBlockerCommandAdd), string(cmd.IdempotencyKey)).Scan(&state); err != nil {
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
				physical := make(chan f.CommitResult, 1)
				var captured atomic.Bool
				hook.mu.Lock()
				hook.afterResult = func(cause f.TransactionCause, result f.CommitResult) {
					if cause.Kind() == f.CommandsCause && cause.Details().Primary.Canonical() == identity.Canonical() && result.State() == f.Unknown && captured.CompareAndSwap(false, true) {
						physical <- result
					}
				}
				hook.mu.Unlock()
				var once sync.Once
				release := func() { once.Do(func() { close(proxy.release) }) }
				defer release()
				t.Cleanup(release)
				before := ibSnapshot(t, base, a)
				pending := ibAsync(t, func(ctx context.Context) (wc.TaskBlockerMutation, error) {
					return pe.service.AddTaskBlocker(ctx, a, cmd, p.ID, target.ID, req)
				})
				ibStage(t, proxy.reached, pending)
				if backend.Load() <= 0 || backend.Load() != proxy.backendPID.Load() {
					t.Fatal("original COMMIT not bound to exact PID")
				}
				actual := awaitLockAttempt(t, confirmation)
				waitExactLock(t, base.db, actual, false, backend.Load())
				type lookupReply struct {
					out wc.TaskBlockerCommandLookup
					err error
				}
				looked := make(chan lookupReply, 1)
				joined := make(chan struct{})
				lctx, cancel := context.WithCancel(context.Background())
				go func() {
					defer close(joined)
					out, err := le.service.LookupTaskBlockerCommand(lctx, a, lookup)
					looked <- lookupReply{out, err}
				}()
				t.Cleanup(func() { cancel(); await(t, joined) })
				waiting := awaitLockAttempt(t, lookupAttempt)
				if waiting.BackendPID == actual.BackendPID || waiting.BackendPID == backend.Load() {
					t.Fatal("Lookup did not get independent caller transaction")
				}
				waitExactLock(t, base.db, waiting, false, backend.Load())
				cancel()
				await(t, joined)
				cancelled := <-looked
				if !errors.Is(cancelled.err, context.Canceled) || cancelled.out.Status != "" || cancelled.out.Receipt != nil {
					t.Fatal("cancelled Lookup published result", cancelled.err)
				}
				// Stop also cancels an already detached confirmation and Drain joins it.
				if phase == "planned" && !commit {
					pe.service.Stop()
					ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
					err := pe.service.Drain(ctx)
					cancel()
					if err != nil {
						t.Fatal("Stop did not join independent confirmation", err)
					}
				}
				original := ibJoin(t, pending)
				requireCode(t, original.err, f.CommitUnknown)
				var proof f.CommitResult
				select {
				case proof = <-physical:
				default:
					t.Fatal("original physical Unknown was not captured")
				}
				var fault *f.Fault
				if !errors.As(original.err, &fault) || fault.CommitState != f.Unknown || fault.RetryHint != "lookup" || fault.CauseID != proof.AttemptID().String() || errors.Unwrap(original.err) == nil || original.value.Task.ID.Validate() == nil {
					t.Fatal("original writer proof or zero result lost", original.err)
				}
				if ibSnapshot(t, base, a) != before {
					t.Fatal("unreleased COMMIT became prematurely visible")
				}
				release()
				await(t, proxy.completed)
				out, err := e.service.LookupTaskBlockerCommand(ctxFor(t), a, lookup)
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
				if out.Status != want {
					t.Fatal("late original COMMIT outcome", out.Status, want)
				}
				fresh := base.renew(t, a)
				recovered, err := e.service.AddTaskBlocker(ctxFor(t), fresh, cmd, p.ID, target.ID, req)
				if err != nil {
					t.Fatal("explicit same-key recovery", err)
				}
				if recovered.Task.Version != target.Version+1 || recovered.Task.ManualRank != target.ManualRank {
					t.Fatal("recovery changed original intent")
				}
				if out.Receipt != nil {
					ibEqual(t, *out.Receipt, recovered)
				}
				ibFact(t, e, fresh, recovered)
				stable := ibSnapshot(t, base, fresh)
				again, err := e.service.AddTaskBlocker(ctxFor(t), fresh, cmd, p.ID, target.ID, req)
				if err != nil {
					t.Fatal(err)
				}
				ibEqual(t, recovered, again)
				if ibSnapshot(t, base, fresh) != stable {
					t.Fatal("committed replay repeated business facts or Activity")
				}
			})
		}
	}
}
