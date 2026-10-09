//go:build integration

package knowledge_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/knowledge"
	kc "github.com/LunaDeerTech/agenteam/internal/central/knowledge/contract"
)

// Hooks only hold an original successful transaction before its real COMMIT,
// or observe its requested locks. They never fabricate a lock/result/receipt.
type knowledgeConcurrentStore struct {
	knowledge.Store
	identity f.CommandIdentity
	after    func(context.Context, f.Tx) error
	before   func(context.Context, f.Tx, []f.LockRequest) error
}

func (s *knowledgeConcurrentStore) WithinTx(ctx context.Context, cause f.TransactionCause, callback func(context.Context, f.Tx) error) f.CommitResult {
	return s.Store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := callback(ctx, tx); err != nil {
			return err
		}
		if s.after != nil && cause.Kind() == f.CommandsCause && cause.Details().Primary.Canonical() == s.identity.Canonical() {
			return s.after(ctx, tx)
		}
		return nil
	})
}

func (s *knowledgeConcurrentStore) AcquireAll(ctx context.Context, tx f.Tx, locks []f.LockRequest) error {
	if s.before != nil {
		if err := s.before(ctx, tx, locks); err != nil {
			return err
		}
	}
	return s.Store.AcquireAll(ctx, tx, locks)
}

func concurrentService(t *testing.T, x *ownerTreeFixture, store knowledge.Store) *knowledge.Service {
	t.Helper()
	s, err := knowledge.New(store, x.deps)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		s.Stop()
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := s.Drain(ctx); err != nil {
			t.Error("concurrent service actual drain", err)
		}
	})
	return s
}

func concurrentIdentity(t *testing.T, p id.ProjectID, name kc.CommandName, meta f.CommandMeta) f.CommandIdentity {
	t.Helper()
	identity, err := kc.CommandIdentity(p, name, meta.IdempotencyKey)
	if err != nil {
		t.Fatal(err)
	}
	return identity
}

func holdConcurrentCommit(s *knowledgeConcurrentStore) (<-chan int32, func()) {
	reached, release := make(chan int32, 1), make(chan struct{})
	var held atomic.Bool
	var once sync.Once
	s.after = func(ctx context.Context, tx f.Tx) error {
		if !held.CompareAndSwap(false, true) {
			return nil
		}
		x, err := s.Store.InTx(tx)
		if err != nil {
			return err
		}
		var pid int32
		if err = x.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&pid); err != nil {
			return err
		}
		reached <- pid
		select {
		case <-release:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return reached, func() { once.Do(func() { close(release) }) }
}

func observeConcurrentLock(s *knowledgeConcurrentStore, key f.LockKey) <-chan int32 {
	reached := make(chan int32, 1)
	var observed atomic.Bool
	s.before = func(ctx context.Context, tx f.Tx, locks []f.LockRequest) error {
		for _, lock := range locks {
			if f.CompareLockKeys(lock.Key, key) != 0 || lock.Mode != f.Exclusive || !observed.CompareAndSwap(false, true) {
				continue
			}
			x, err := s.Store.InTx(tx)
			if err != nil {
				return err
			}
			var pid int32
			if err = x.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&pid); err != nil {
				return err
			}
			reached <- pid
		}
		return nil
	}
	return reached
}

type concurrentReply[T any] struct {
	value T
	err   error
}

func concurrentCall[T any](t *testing.T, run func(context.Context) (T, error)) <-chan concurrentReply[T] {
	t.Helper()
	ctx, cancel := context.WithCancel(knowledgeContext(t))
	result, joined := make(chan concurrentReply[T], 1), make(chan struct{})
	go func() {
		defer close(joined)
		value, err := run(ctx)
		result <- concurrentReply[T]{value, err}
	}()
	t.Cleanup(func() { cancel(); runtimeAwait(t, joined) })
	return result
}

func concurrentPID[T any](t *testing.T, reached <-chan int32, early <-chan concurrentReply[T]) int32 {
	t.Helper()
	select {
	case pid := <-reached:
		if pid <= 0 {
			t.Fatal("missing actual transaction PID")
		}
		return pid
	case result := <-early:
		t.Fatal("call returned before declared actual transaction boundary", result.err)
	case <-time.After(5 * time.Second):
		t.Fatal("actual transaction boundary not reached")
	}
	return 0
}

func concurrentResult[T any](t *testing.T, result <-chan concurrentReply[T]) concurrentReply[T] {
	t.Helper()
	select {
	case got := <-result:
		return got
	case <-time.After(10 * time.Second):
		t.Fatal("actual concurrent caller did not return")
	}
	return concurrentReply[T]{}
}

func concurrentLockWait(t *testing.T, x *ownerTreeFixture, key f.LockKey, holder, waiter int32) {
	t.Helper()
	ctx, cancel := context.WithTimeout(knowledgeContext(t), 5*time.Second)
	defer cancel()
	tick := time.NewTicker(5 * time.Millisecond)
	defer tick.Stop()
	n := uint64(key.AdvisoryKey())
	for {
		var matched bool
		err := x.raw.QueryRow(ctx, `SELECT EXISTS(
 SELECT 1 FROM pg_locks w JOIN pg_locks h ON h.locktype=w.locktype AND h.database=w.database
 AND h.classid=w.classid AND h.objid=w.objid AND h.objsubid=w.objsubid
 JOIN pg_stat_activity a ON a.pid=w.pid
 WHERE a.datname=current_database() AND w.locktype='advisory' AND w.classid::bigint=$1
 AND w.objid::bigint=$2 AND w.objsubid=1 AND w.pid=$3 AND w.mode='ExclusiveLock' AND NOT w.granted
 AND h.pid=$4 AND h.mode='ExclusiveLock' AND h.granted AND $4::int=ANY(pg_blocking_pids(w.pid)))`, int64(n>>32), int64(n&0xffffffff), waiter, holder).Scan(&matched)
		if err != nil {
			t.Fatal("exact original lock wait observation", err)
		}
		if matched {
			return
		}
		select {
		case <-tick.C:
		case <-ctx.Done():
			t.Fatal("expected real key/mode/holder/waiter relationship was not observed")
		}
	}
}

func TestKnowledgeB02Concurrency(t *testing.T) {
	t.Run("same_key_two_sessions", func(t *testing.T) {
		x := newPublicationFixture(t)
		first := x.human(t)
		user, err := f.ParseID[id.User](first.Details().UserID)
		if err != nil {
			t.Fatal(err)
		}
		second := x.session(t, user)
		p, meta := x.project(t, first, true), treeMeta(t)
		request := kc.CreateRequest{ProjectID: p, DocumentID: treeID[kc.Document](t), Title: "one publication"}
		identity := concurrentIdentity(t, p, kc.Create, meta)
		lock, err := f.CommandLock(identity)
		if err != nil {
			t.Fatal(err)
		}
		leaderStore := &knowledgeConcurrentStore{Store: x.raw, identity: identity}
		followerStore := &knowledgeConcurrentStore{Store: x.raw}
		held, release := holdConcurrentCommit(leaderStore)
		defer release()
		attempt := observeConcurrentLock(followerStore, lock)
		leaderService, followerService := concurrentService(t, x, leaderStore), concurrentService(t, x, followerStore)
		input1, input2 := publicationText(t, "same bytes"), publicationText(t, "same bytes")
		leader := concurrentCall(t, func(ctx context.Context) (kc.DocumentRef, error) {
			return leaderService.CreateDocument(ctx, first, meta, request, input1)
		})
		holder := concurrentPID(t, held, leader)
		follower := concurrentCall(t, func(ctx context.Context) (kc.DocumentRef, error) {
			return followerService.CreateDocument(ctx, second, meta, request, input2)
		})
		waiter := concurrentPID(t, attempt, follower)
		concurrentLockWait(t, x, lock, holder, waiter)
		release()
		one, two := concurrentResult(t, leader), concurrentResult(t, follower)
		succeeded := 0
		for _, result := range []concurrentReply[kc.DocumentRef]{one, two} {
			if result.err == nil {
				succeeded++
				if result.value.ID != request.DocumentID || result.value.ContentVersion != 1 {
					t.Fatal("same-key returned another publication")
				}
			} else {
				treeCode(t, result.err, f.ResourceBusy)
			}
		}
		if succeeded == 0 {
			t.Fatal("serialized same-key callers produced no completed publication")
		}
		for _, actor := range []id.Actor{first, second} {
			replay, err := x.service.CreateDocument(knowledgeContext(t), actor, meta, request, publicationText(t, "same bytes"))
			if err != nil || replay.ID != request.DocumentID || replay.ContentVersion != 1 {
				t.Fatal("stable-user same-key retry did not return one receipt", err)
			}
			publicationRead(t, x, actor, replay, []byte("same bytes"))
		}
		if commands, events := x.count(t, p); commands != 1 || events != 1 {
			t.Fatal("same-key competition duplicated durable command/event")
		}
		if audits, events := publicationCount(t, x, p); audits != 1 || events != 1 {
			t.Fatal("same-key competition duplicated Object Audit/Outbox")
		}
	})
	t.Run("global_document_id_cross_project", func(t *testing.T) {
		x := newPublicationFixture(t)
		first, second := x.human(t), x.human(t)
		p, other := x.project(t, first, true), x.project(t, second, true)
		meta, otherMeta := treeMeta(t), treeMeta(t)
		document := treeID[kc.Document](t)
		request := kc.CreateRequest{ProjectID: p, DocumentID: document, Title: "winner"}
		otherRequest := kc.CreateRequest{ProjectID: other, DocumentID: document, Title: "foreign claimant"}
		lock, err := f.RecordLock(f.ReferenceRecordLock, "knowledge:document:"+document.String())
		if err != nil {
			t.Fatal(err)
		}
		leaderStore := &knowledgeConcurrentStore{Store: x.raw, identity: concurrentIdentity(t, p, kc.Create, meta)}
		followerStore := &knowledgeConcurrentStore{Store: x.raw}
		held, release := holdConcurrentCommit(leaderStore)
		defer release()
		attempt := observeConcurrentLock(followerStore, lock)
		leaderService, followerService := concurrentService(t, x, leaderStore), concurrentService(t, x, followerStore)
		input1, input2 := publicationText(t, "owner bytes"), publicationText(t, "other bytes")
		leader := concurrentCall(t, func(ctx context.Context) (kc.DocumentRef, error) {
			return leaderService.CreateDocument(ctx, first, meta, request, input1)
		})
		holder := concurrentPID(t, held, leader)
		follower := concurrentCall(t, func(ctx context.Context) (kc.DocumentRef, error) {
			return followerService.CreateDocument(ctx, second, otherMeta, otherRequest, input2)
		})
		waiter := concurrentPID(t, attempt, follower)
		concurrentLockWait(t, x, lock, holder, waiter)
		release()
		one, two := concurrentResult(t, leader), concurrentResult(t, follower)
		if one.err != nil || one.value.ProjectID != p || one.value.ID != document {
			t.Fatal("original global-ID publication failed", one.err)
		}
		treeCode(t, two.err, f.NotFound)
		publicationRead(t, x, first, one.value, []byte("owner bytes"))
		digest, err := kc.CreateDigest(second, otherMeta, otherRequest, publicationText(t, "other bytes"))
		if err != nil {
			t.Fatal(err)
		}
		lookup, err := x.service.LookupCommand(knowledgeContext(t), second, kc.LookupRequest{ProjectID: other, Command: kc.Create, Key: otherMeta.IdempotencyKey, SemanticDigest: digest})
		if err != nil || lookup.State != kc.NotObserved {
			t.Fatal("foreign global-ID rejection left command facts", err)
		}
		runtimeNoCanonical(t, x, other)
		if audits, events := publicationCount(t, x, other); audits != 0 || events != 0 {
			t.Fatal("foreign global-ID rejection leaked publication side effects")
		}
		_, err = x.service.GetDocument(knowledgeContext(t), second, other, document)
		treeCode(t, err, f.NotFound)
	})
	t.Run("opposing_moves_recheck_current_ancestry", func(t *testing.T) {
		x := newOwnerTreeFixture(t)
		actor := x.human(t)
		p := x.project(t, actor, true)
		a, b := x.document(t, actor, p, nil, "a"), x.document(t, actor, p, nil, "b")
		meta, otherMeta := treeMeta(t), treeMeta(t)
		identity := concurrentIdentity(t, p, kc.Move, meta)
		leaderStore := &knowledgeConcurrentStore{Store: x.raw, identity: identity}
		followerStore := &knowledgeConcurrentStore{Store: x.raw}
		held, release := holdConcurrentCommit(leaderStore)
		defer release()
		// Both mutations share the current Owner, so User EX precedes tree EX
		// in the real union. Observe that actual first conflicting key, without
		// replacing the union with an artificial tree-only lock.
		lock, err := f.UserLock(actor.Details().UserID)
		if err != nil {
			t.Fatal(err)
		}
		attempt := observeConcurrentLock(followerStore, lock)
		leaderService, followerService := concurrentService(t, x, leaderStore), concurrentService(t, x, followerStore)
		request, otherRequest := kc.MoveRequest{TargetParentID: &b.ID}, kc.MoveRequest{TargetParentID: &a.ID}
		mutation, err := kc.NewMoveMutation(actor, meta, p, a.ID, request)
		if err != nil {
			t.Fatal(err)
		}
		otherMutation, err := kc.NewMoveMutation(actor, otherMeta, p, b.ID, otherRequest)
		if err != nil {
			t.Fatal(err)
		}
		plan, err := leaderService.DiscoverMutation(knowledgeContext(t), mutation)
		if err != nil {
			t.Fatal(err)
		}
		otherPlan, err := followerService.DiscoverMutation(knowledgeContext(t), otherMutation)
		if err != nil {
			t.Fatal(err)
		}
		move := func(ctx context.Context, store *knowledgeConcurrentStore, service *knowledge.Service, m f.CommandMeta, document kc.DocumentID, request kc.MoveRequest, plan kc.MutationPlan) (kc.MoveResult, error) {
			identity, err := kc.CommandIdentity(p, kc.Move, m.IdempotencyKey)
			if err != nil {
				return kc.MoveResult{}, err
			}
			cause, err := f.NewCommandsCause(identity)
			if err != nil {
				return kc.MoveResult{}, err
			}
			var out kc.MoveResult
			result := store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
				locked, err := service.AcquireMutationInTx(ctx, tx, plan, nil)
				if err != nil {
					return err
				}
				out, err = service.MoveDocumentInTx(ctx, tx, actor, m, p, document, request, locked)
				return err
			})
			if result.State() != f.Committed {
				if result.Fault() == nil {
					return kc.MoveResult{}, errors.New("original caller transaction had no committed outcome")
				}
				return kc.MoveResult{}, result.Fault()
			}
			return out, nil
		}
		leader := concurrentCall(t, func(ctx context.Context) (kc.MoveResult, error) {
			return move(ctx, leaderStore, leaderService, meta, a.ID, request, plan)
		})
		holder := concurrentPID(t, held, leader)
		follower := concurrentCall(t, func(ctx context.Context) (kc.MoveResult, error) {
			return move(ctx, followerStore, followerService, otherMeta, b.ID, otherRequest, otherPlan)
		})
		waiter := concurrentPID(t, attempt, follower)
		concurrentLockWait(t, x, lock, holder, waiter)
		release()
		one, two := concurrentResult(t, leader), concurrentResult(t, follower)
		if one.err != nil || !one.value.Changed || one.value.Document.ParentDocumentID == nil || *one.value.Document.ParentDocumentID != b.ID {
			t.Fatal("first actual Move did not commit", one.err)
		}
		treeCode(t, two.err, f.InvalidArgument)
		ancestors, err := x.service.ReadAncestors(knowledgeContext(t), actor, p, a.ID)
		if err != nil || len(ancestors) != 1 || ancestors[0].ID != b.ID {
			t.Fatal("opposing Move created a cycle or lost committed parent", err)
		}
		if commands, events := x.count(t, p); commands != 1 || events != 0 {
			t.Fatal("rejected cycle left a receipt or content event")
		}
	})
}
