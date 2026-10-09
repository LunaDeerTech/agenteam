//go:build integration

package knowledge_test

import (
	"bytes"
	"context"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/knowledge"
	kc "github.com/LunaDeerTech/agenteam/internal/central/knowledge/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
)

// This hook observes exactly the original final command transaction. It never
// changes the original callback, SQL results, commit result, or lock union.
type treeCommandTxStore struct {
	knowledge.Store
	identity      f.CommandIdentity
	before, after func(context.Context, f.Tx) error
	attempt       func(context.Context, f.Tx) error
	result        chan f.CommitResult
	once, active  atomic.Bool
}

func (s *treeCommandTxStore) WithinTx(ctx context.Context, cause f.TransactionCause, fn func(context.Context, f.Tx) error) f.CommitResult {
	match := cause.Kind() == f.CommandsCause && cause.Details().Primary.Canonical() == s.identity.Canonical() && s.once.CompareAndSwap(false, true)
	r := s.Store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if match {
			s.active.Store(true)
			defer s.active.Store(false)
			if s.before != nil {
				if err := s.before(ctx, tx); err != nil {
					return err
				}
			}
		}
		if err := fn(ctx, tx); err != nil {
			return err
		}
		if match && s.after != nil {
			return s.after(ctx, tx)
		}
		return nil
	})
	if match && s.result != nil {
		s.result <- r
	}
	return r
}
func (s *treeCommandTxStore) AcquireAll(ctx context.Context, tx f.Tx, locks []f.LockRequest) error {
	if s.active.Load() && s.attempt != nil {
		if err := s.attempt(ctx, tx); err != nil {
			return err
		}
	}
	return s.Store.AcquireAll(ctx, tx, locks)
}
func treeCommandGate() (<-chan struct{}, func()) {
	gate := make(chan struct{})
	var once sync.Once
	return gate, func() { once.Do(func() { close(gate) }) }
}
func treeCommandWait(ctx context.Context, gate <-chan struct{}) error {
	select {
	case <-gate:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
func treeCommandPID(ctx context.Context, store knowledge.Store, tx f.Tx) (int32, error) {
	q, err := store.InTx(tx)
	if err != nil {
		return 0, err
	}
	var pid int32
	err = q.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&pid)
	return pid, err
}
func treeCommandPause(store knowledge.Store, reached chan<- int32, gate <-chan struct{}) func(context.Context, f.Tx) error {
	return func(ctx context.Context, tx f.Tx) error {
		pid, err := treeCommandPID(ctx, store, tx)
		if err != nil {
			return err
		}
		reached <- pid
		if gate != nil {
			return treeCommandWait(ctx, gate)
		}
		return nil
	}
}
func treeCommandReceivePID(t *testing.T, reached <-chan int32) int32 {
	t.Helper()
	select {
	case pid := <-reached:
		if pid > 0 {
			return pid
		}
	case <-time.After(3 * time.Second):
	}
	t.Fatal("original transaction PID not observed")
	return 0
}
func treeCommandRun(t *testing.T, v *treeCommandHTTPFixture, request *http.Request) (<-chan treeCommandHTTPResponse, <-chan struct{}) {
	t.Helper()
	out, done := make(chan treeCommandHTTPResponse, 1), make(chan struct{})
	ctx, cancel := context.WithCancel(request.Context())
	go func() { defer close(done); out <- v.serve(request.WithContext(ctx)) }()
	t.Cleanup(func() { cancel(); runtimeAwait(t, done) })
	return out, done
}
func treeCommandReply(t *testing.T, out <-chan treeCommandHTTPResponse, done <-chan struct{}) treeCommandHTTPResponse {
	t.Helper()
	select {
	case r := <-out:
		runtimeAwait(t, done)
		return r
	case <-time.After(5 * time.Second):
		t.Fatal("actual HTTP call did not return")
	}
	return treeCommandHTTPResponse{}
}
func treeCommandLockWait(t *testing.T, raw *postgres.Store, key f.LockKey, holder, waiter int32, holderMode, waiterMode string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(knowledgeContext(t), time.Second)
	defer cancel()
	tick := time.NewTicker(5 * time.Millisecond)
	defer tick.Stop()
	n := uint64(key.AdvisoryKey())
	for {
		var found bool
		err := raw.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_locks w JOIN pg_locks h ON h.locktype=w.locktype AND h.database=w.database AND h.classid=w.classid AND h.objid=w.objid AND h.objsubid=w.objsubid JOIN pg_stat_activity a ON a.pid=w.pid WHERE a.datname=current_database() AND w.locktype='advisory' AND w.classid::bigint=$1 AND w.objid::bigint=$2 AND w.objsubid=1 AND w.pid=$3 AND w.mode=$5 AND NOT w.granted AND h.pid=$4 AND h.mode=$6 AND h.granted AND $4::int=ANY(pg_blocking_pids(w.pid)))`, int64(n>>32), int64(n&0xffffffff), waiter, holder, waiterMode, holderMode).Scan(&found)
		if err != nil {
			t.Fatal("exact lock wait observation", err)
		}
		if found {
			return
		}
		select {
		case <-tick.C:
		case <-ctx.Done():
			t.Fatal("exact original lock/blocker relation missing")
		}
	}
}
func treeCommandOwnerWriter(t *testing.T, v *treeCommandHTTPFixture, attempt, held chan<- int32, gate <-chan struct{}) (<-chan f.CommitResult, <-chan struct{}) {
	t.Helper()
	ctx, cancel := context.WithCancel(knowledgeContext(t))
	out, done := make(chan f.CommitResult, 1), make(chan struct{})
	cause, err := f.NewRecoveryCause("knowledge.commandhttp.fixture", knowledgeID(t), "")
	if err != nil {
		t.Fatal(err)
	}
	user, err := f.UserLock(v.ownerBrowser.actor.Details().UserID)
	if err != nil {
		t.Fatal(err)
	}
	project, err := f.ProjectLock(v.project.String())
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		defer close(done)
		out <- v.raw.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
			pid, err := treeCommandPID(ctx, v.raw, tx)
			if err != nil {
				return err
			}
			if attempt != nil {
				attempt <- pid
			}
			if err = v.raw.AcquireAll(ctx, tx, []f.LockRequest{{Key: user, Mode: f.Exclusive}, {Key: project, Mode: f.Exclusive}}); err != nil {
				return err
			}
			if err = treeCommandChangeOwner(ctx, v, tx); err != nil {
				return err
			}
			if held != nil {
				held <- pid
			}
			if gate != nil {
				return treeCommandWait(ctx, gate)
			}
			return nil
		})
	}()
	t.Cleanup(func() { cancel(); runtimeAwait(t, done) })
	return out, done
}
func treeCommandCommit(t *testing.T, out <-chan f.CommitResult, done <-chan struct{}) {
	t.Helper()
	select {
	case r := <-out:
		runtimeAwait(t, done)
		if r.State() != f.Committed {
			t.Fatal("upstream original transaction not committed", r.Fault())
		}
	case <-time.After(5 * time.Second):
		t.Fatal("original transaction did not return")
	}
}

func TestKnowledgeTreeCommandHTTPTransactions(t *testing.T) {
	for _, commandFirst := range []bool{true, false} {
		name := "owner_writer_first"
		if commandFirst {
			name = "command_first"
		}
		t.Run(name, func(t *testing.T) {
			v := newTreeCommandHTTPFixture(t)
			d, target := v.makeDocument(t, nil, "moving"), v.makeDocument(t, nil, "target")
			key := treeMeta(t).IdempotencyKey
			body := treeCommandJSON(t, map[string]any{"expected_parent_id": nil, "target_parent_id": target.ID})
			identity, err := kc.CommandIdentity(v.project, kc.Move, key)
			if err != nil {
				t.Fatal(err)
			}
			before := v.facts(t)
			hook := &treeCommandTxStore{Store: v.raw, identity: identity, result: make(chan f.CommitResult, 1)}
			gate, release := treeCommandGate()
			defer release()
			seen, attempt := make(chan int32, 1), make(chan int32, 1)
			if commandFirst {
				hook.after = treeCommandPause(v.raw, seen, gate)
			} else {
				hook.before = treeCommandPause(v.raw, seen, gate)
			}
			hook.attempt = treeCommandPause(v.raw, attempt, nil)
			v.bind(t, concurrentService(t, v.ownerTreeFixture, hook))
			out, done := treeCommandRun(t, v, treeCommandRequest(v, d, key, body))
			pid := treeCommandReceivePID(t, seen)
			user, err := f.UserLock(v.ownerBrowser.actor.Details().UserID)
			if err != nil {
				t.Fatal(err)
			}
			writerSeen := make(chan int32, 1)
			if commandFirst {
				writer, writerDone := treeCommandOwnerWriter(t, v, writerSeen, nil, nil)
				treeCommandLockWait(t, v.raw, user, pid, treeCommandReceivePID(t, writerSeen), "ExclusiveLock", "ExclusiveLock")
				release()
				treeCommandSuccess(t, treeCommandReply(t, out, done))
				treeCommandCommit(t, writer, writerDone)
			} else {
				writerGate, writerRelease := treeCommandGate()
				defer writerRelease()
				writer, writerDone := treeCommandOwnerWriter(t, v, nil, writerSeen, writerGate)
				writerPID := treeCommandReceivePID(t, writerSeen)
				release()
				if treeCommandReceivePID(t, attempt) != pid {
					t.Fatal("command transaction changed")
				}
				treeCommandLockWait(t, v.raw, user, writerPID, pid, "ExclusiveLock", "ExclusiveLock")
				writerRelease()
				treeCommandCommit(t, writer, writerDone)
				treeCommandProblem(t, treeCommandReply(t, out, done), f.NotFound)
			}
			result := <-hook.result
			if (result.State() == f.Committed) != commandFirst {
				t.Fatal("original command result does not match proven lock order")
			}
			after := v.facts(t)
			wantCommands := before.Commands
			if commandFirst {
				wantCommands++
			}
			if after.Commands != wantCommands || after.Events != before.Events || after.Audit != before.Audit || after.Outbox != before.Outbox {
				t.Fatal("ordered move effects")
			}
			// Original HTTP identity cannot reuse even the confirmed receipt after
			// the actual upstream Owner change has committed.
			treeCommandProblem(t, v.post(t, d.ID, "move", body, key), f.NotFound)
		})
	}
	t.Run("cancelled_original_transaction_returns_before_http_tail", func(t *testing.T) {
		v := newTreeCommandHTTPFixture(t)
		d, target := v.makeDocument(t, nil, "cancel"), v.makeDocument(t, nil, "target")
		key := treeMeta(t).IdempotencyKey
		body := treeCommandJSON(t, map[string]any{"expected_parent_id": nil, "target_parent_id": target.ID})
		identity, err := kc.CommandIdentity(v.project, kc.Move, key)
		if err != nil {
			t.Fatal(err)
		}
		hook := &treeCommandTxStore{Store: v.raw, identity: identity, result: make(chan f.CommitResult, 1)}
		gate, release := treeCommandGate()
		defer release()
		seen := make(chan int32, 1)
		hook.after = treeCommandPause(v.raw, seen, gate)
		v.bind(t, concurrentService(t, v.ownerTreeFixture, hook))
		before := v.facts(t)
		ctx, cancel := context.WithCancel(knowledgeContext(t))
		defer cancel()
		out, done := treeCommandRun(t, v, treeCommandRequest(v, d, key, body).WithContext(ctx))
		treeCommandReceivePID(t, seen)
		cancel()
		response := treeCommandReply(t, out, done)
		result := <-hook.result
		if !response.aborted || len(response.body) != 0 || result.State() != f.NotCommitted || v.facts(t) != before {
			t.Fatal("cancel did not roll back and join original transaction")
		}
		v.bind(t, v.service)
		treeCommandLookupState(t, v.lookup(t, d.ID, "move", body, key), "not_observed")
		treeCommandSuccess(t, v.post(t, d.ID, "move", body, key))
	})
	t.Run("same_key_two_real_sessions_one_completed_receipt", func(t *testing.T) {
		v := newTreeCommandHTTPFixture(t)
		second := *v
		second.ownerBrowser = v.login(t, v.ownerBrowser.email)
		d, target := v.makeDocument(t, nil, "same key"), v.makeDocument(t, nil, "target")
		key := treeMeta(t).IdempotencyKey
		body := treeCommandJSON(t, map[string]any{"expected_parent_id": nil, "target_parent_id": target.ID})
		identity, err := kc.CommandIdentity(v.project, kc.Move, key)
		if err != nil {
			t.Fatal(err)
		}
		commandLock, err := f.CommandLock(identity)
		if err != nil {
			t.Fatal(err)
		}
		leaderStart, startLeader := treeCommandGate()
		defer startLeader()
		followerStart, startFollower := treeCommandGate()
		defer startFollower()
		leaderEnd, finishLeader := treeCommandGate()
		defer finishLeader()
		leaderBefore, followerBefore := make(chan int32, 1), make(chan int32, 1)
		leaderHeld, followerAttempt := make(chan int32, 1), make(chan int32, 1)
		leader := &treeCommandTxStore{Store: v.raw, identity: identity, before: treeCommandPause(v.raw, leaderBefore, leaderStart), after: treeCommandPause(v.raw, leaderHeld, leaderEnd)}
		follower := &treeCommandTxStore{Store: v.raw, identity: identity, before: treeCommandPause(v.raw, followerBefore, followerStart), attempt: treeCommandPause(v.raw, followerAttempt, nil)}
		v.bind(t, concurrentService(t, v.ownerTreeFixture, leader))
		second.bind(t, concurrentService(t, v.ownerTreeFixture, follower))
		before := v.facts(t)
		one, doneOne := treeCommandRun(t, v, treeCommandRequest(v, d, key, body))
		leaderPID := treeCommandReceivePID(t, leaderBefore)
		two, doneTwo := treeCommandRun(t, &second, treeCommandRequest(&second, d, key, body))
		followerPID := treeCommandReceivePID(t, followerBefore)
		startLeader()
		if treeCommandReceivePID(t, leaderHeld) != leaderPID {
			t.Fatal("leader changed original transaction")
		}
		startFollower()
		if treeCommandReceivePID(t, followerAttempt) != followerPID {
			t.Fatal("follower changed original transaction")
		}
		treeCommandLockWait(t, v.raw, commandLock, leaderPID, followerPID, "ExclusiveLock", "ExclusiveLock")
		finishLeader()
		r1, r2 := treeCommandReply(t, one, doneOne), treeCommandReply(t, two, doneTwo)
		treeCommandSuccess(t, r1)
		treeCommandSuccess(t, r2)
		if !bytes.Equal(r1.body, r2.body) {
			t.Fatal("same User original command produced different receipts")
		}
		after := v.facts(t)
		if after.Commands != before.Commands+1 || after.Events != before.Events || after.Audit != before.Audit || after.Outbox != before.Outbox {
			t.Fatal("same-key competing calls duplicated effects")
		}
		treeCommandLookupState(t, second.lookup(t, d.ID, "move", body, key), "committed")
	})
}
