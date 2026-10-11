package object

import (
	"context"
	"errors"
	"net"
	"net/http"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/jackc/pgx/v5"
)

// Formal Store boundary controls only; real locks and canonical rows are PG tests.
type joinControlStore struct {
	Store
	mu      sync.Mutex
	active  map[f.Tx]bool
	calls   atomic.Int32
	acquire func(context.Context, f.Tx, []f.LockRequest) error
	after   func(context.Context, f.TransactionCause, f.CommitResult) f.CommitResult
	before  bool
}

func (s *joinControlStore) InTx(tx f.Tx) (postgres.SQLExecutor, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.active[tx] {
		return nil, errors.New("original transaction ended")
	}
	return s, nil
}
func (s *joinControlStore) WithinTx(ctx context.Context, cause f.TransactionCause, cb func(context.Context, f.Tx) error) f.CommitResult {
	s.calls.Add(1)
	result := f.NotCommittedResult(f.NewFault(f.Forbidden, f.NotCommitted))
	if !s.before {
		tx := f.NewTx()
		s.mu.Lock()
		s.active[tx] = true
		s.mu.Unlock()
		err := cb(ctx, tx)
		s.mu.Lock()
		delete(s.active, tx)
		s.mu.Unlock()
		if err == nil {
			result = f.CommittedResult()
		} else {
			var fault *f.Fault
			if !errors.As(err, &fault) {
				fault = f.NewFault(f.DependencyUnavailable, f.NotCommitted)
			}
			result = f.NotCommittedResult(fault)
		}
	}
	if s.after != nil {
		return s.after(ctx, cause, result)
	}
	return result
}
func (s *joinControlStore) AcquireAll(ctx context.Context, tx f.Tx, locks []f.LockRequest) error {
	if s.acquire != nil {
		return s.acquire(ctx, tx, locks)
	}
	return nil
}
func (*joinControlStore) QueryRow(context.Context, string, ...any) postgres.Row {
	return auditTestRow(func(...any) error { return pgx.ErrNoRows })
}
func joinControl(t *testing.T) (*Service, *serviceState, *joinControlStore, *projectWorkHandle) {
	t.Helper()
	store := &joinControlStore{active: map[f.Tx]bool{}}
	state := &serviceState{store: store, accessIssuer: oc.NewAccessIssuer(), projectWork: map[string]*projectWorkHandle{}, projectWriters: map[*projectWorkWriter]bool{}, projectJoins: map[string]*projectWorkHandle{}, operations: map[*operation]bool{}, cleanupRequests: map[*cleanupRequest]bool{}, changed: make(chan struct{})}
	service := &Service{data: func() *serviceState { return state }}
	raw := auditID[struct{}](t).String()
	handle := &projectWorkHandle{work: projectWork{id: raw, resource: raw, kind: "preparation", project: auditID[identity.Project](t), process: auditID[oc.Process](t)}, ended: true, registered: true}
	state.projectWork[raw] = handle
	return service, state, store, handle
}
func joinControlPlan(t *testing.T, r *serviceState) (oc.AccessLockPlan, []f.LockRequest) {
	t.Helper()
	user := auditID[identity.User](t)
	actor, _ := identity.NewHuman(user, auditID[identity.Session](t))
	owner, _ := oc.NewObjectOwner(oc.Avatar, user.String(), "")
	request := ownerRequest(actor, owner, oc.PrepareAccess, oc.AccessRequestDetails{})
	key, _ := f.UserLock(user.String())
	locks := []f.LockRequest{{Key: key, Mode: f.Shared}}
	deps, _ := oc.NewAccessDependencies(newDigest(make([]byte, 32)), locks)
	plan, err := oc.NewAccessLockPlan(r.accessIssuer, oc.AccessPlanDetails{Request: request, DependencyRequest: request, Dependencies: deps, DomainBinding: newDigest(make([]byte, 32)), Locks: locks})
	if err != nil {
		t.Fatal(err)
	}
	return plan, locks
}
func joinWait(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(time.Second):
		t.Fatal("explicit ownership barrier not reached")
	}
}

func TestObjectRuntimeJoinOwnerOutcomeBoundary(t *testing.T) {
	t.Run("first-writer-union-and-partial-acquire", func(t *testing.T) {
		s, r, store, h := joinControl(t)
		delete(r.projectWork, h.work.id)
		h.ended = false
		h.registered = false
		plan, planned := joinControlPlan(t, r)
		extra := []f.LockRequest{workLock(h.work.id)}
		w1, err := s.captureProjectWriter(f.NewTx(), h, plan, extra)
		if err != nil {
			t.Fatal(err)
		}
		w2, err := s.captureProjectWriter(f.NewTx(), h, plan, extra)
		if err != nil {
			t.Fatal(err)
		}
		s.returnedProjectWriter(w1, f.CommittedResult())
		cause, _ := f.NewRecoveryCause("object", h.work.id, "")
		unknown := f.UnknownResult(auditID[f.TransactionAttempt](t), cause)
		s.returnedProjectWriter(w2, unknown)
		if len(r.projectWriters) != 1 || !r.projectWriters[w2] || !hasCode(commitError(unknown), f.CommitUnknown) {
			t.Fatal("one result erased another original writer")
		}
		h.ended = true
		store.acquire = func(_ context.Context, _ f.Tx, locks []f.LockRequest) error {
			found := map[string]bool{}
			for _, lock := range locks {
				if lock.Mode != f.Exclusive {
					t.Error("confirmation did not normalize complete union")
				}
				found[lock.Key.Canonical()] = true
			}
			for _, lock := range append(planned, extra...) {
				if !found[lock.Key.Canonical()] {
					t.Error("original union lost before partial failure")
				}
			}
			return errors.New("controlled partial Acquire failure")
		}
		if err = s.joinProjectWork(context.Background(), h); err == nil || len(r.projectWriters) != 1 || len(r.projectJoins) != 1 {
			t.Fatal("failed confirmation retired original owner", err)
		}
		store.acquire = nil
		changed := r.changed
		if err = s.joinProjectWork(context.Background(), h); err != nil || !r.workJoinedLocked() {
			t.Fatal("confirmed absence did not retire owners", err)
		}
		select {
		case <-changed:
		default:
			t.Fatal("retirement did not notify")
		}
	})
	t.Run("before-callback-safe-return", func(t *testing.T) {
		s, r, store, h := joinControl(t)
		delete(r.projectWork, h.work.id)
		store.before = true
		if err := s.joinProjectWork(context.Background(), h); !hasCode(err, f.Forbidden) {
			t.Fatal("original result changed", err)
		}
		if !r.workJoinedLocked() {
			t.Fatal("no callback leaked confirmation owner")
		}
	})
	t.Run("unknown-confirmation-retains-recovery-handle", func(t *testing.T) {
		s, r, store, h := joinControl(t)
		delete(r.projectWork, h.work.id)
		attempt := auditID[f.TransactionAttempt](t)
		store.after = func(_ context.Context, cause f.TransactionCause, result f.CommitResult) f.CommitResult {
			if result.State() != f.Committed {
				t.Error("controlled callback did not confirm absence")
			}
			return f.UnknownResult(attempt, cause)
		}
		err := s.joinProjectWork(context.Background(), h)
		var fault *f.Fault
		if !errors.As(err, &fault) || fault.Code != f.CommitUnknown || fault.CommitState != f.Unknown || r.projectJoins[h.work.id] != h || r.workJoinedLocked() {
			t.Fatal("unknown confirmation lost original identity or owner", err)
		}
		store.after = nil
		if err = s.joinProjectWork(context.Background(), h); err != nil || !r.workJoinedLocked() {
			t.Fatal("retained recovery handle could not finish", err)
		}
	})
}

func TestObjectRuntimeJoinConcurrentOwnership(t *testing.T) {
	t.Run("force-backend-close-remains-owned", func(t *testing.T) {
		s, r, _, h := joinControl(t)
		delete(r.projectWork, h.work.id)
		r.workerStop = make(chan struct{})
		spool, err := OpenSpool(filepath.Join(t.TempDir(), "spool"), h.work.process)
		if err != nil {
			t.Fatal(err)
		}
		r.spool = spool
		guard, err := OpenProcessGuard(spool, h.work.process)
		if err != nil {
			t.Fatal(err)
		}
		guard.state().service = s
		entered, release, returned := make(chan struct{}), make(chan struct{}), make(chan struct{})
		var once sync.Once
		backend := &backendState{transport: &http.Transport{}, stop: func() {}, connections: map[*storageConn]bool{}}
		conn := &storageConn{Conn: &joinCloseBarrier{entered: entered, release: release}, state: backend}
		backend.connections[conn] = true
		r.backend = &Backend{data: func() *backendState { return backend }}
		t.Cleanup(func() {
			once.Do(func() { close(release) })
			joinWait(t, returned)
			_ = guard.finish(context.Background())
		})
		var original error
		go func() { defer close(returned); original = s.Force(context.Background()) }()
		joinWait(t, entered)
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
		defer cancel()
		if err = s.Drain(ctx); err == nil {
			t.Error("concurrent Drain completed before original Force Close returned")
		}
		if err = guard.finish(context.Background()); err == nil {
			t.Error("guard released while original Force Close remained active")
		}
		once.Do(func() { close(release) })
		joinWait(t, returned)
		if original != nil {
			t.Fatal("released original Force did not finish", original)
		}
		if err = guard.finish(context.Background()); err != nil {
			t.Fatal("actual Force return did not permit guard exit", err)
		}
	})
	t.Run("actual-confirmation-return-and-cancelled-waiter", func(t *testing.T) {
		s, r, store, h := joinControl(t)
		entered, release, returned := make(chan struct{}), make(chan struct{}), make(chan struct{})
		var once sync.Once
		t.Cleanup(func() { once.Do(func() { close(release) }); joinWait(t, returned) })
		store.after = func(_ context.Context, _ f.TransactionCause, result f.CommitResult) f.CommitResult {
			close(entered)
			<-release
			return result
		}
		var err error
		go func() { defer close(returned); err = s.joinProjectWork(context.Background(), h) }()
		joinWait(t, entered)
		if e := s.joinProjectWork(context.Background(), h); !hasCode(e, f.ResourceBusy) {
			t.Fatal("second join entered original confirmation", e)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
		defer cancel()
		if e := s.Drain(ctx); e == nil {
			t.Fatal("cancelled Drain retired original confirmation")
		}
		r.mu.Lock()
		owned := r.projectJoins[h.work.id] == h && h.joining && !r.workJoinedLocked()
		r.mu.Unlock()
		if !owned || store.calls.Load() != 1 {
			t.Fatal("waiter stole or duplicated actual owner")
		}
		select {
		case <-returned:
			t.Fatal("Store return barrier not respected")
		default:
		}
		once.Do(func() { close(release) })
		joinWait(t, returned)
		if err != nil {
			t.Fatal(err)
		}
		r.mu.Lock()
		joined := r.workJoinedLocked()
		r.mu.Unlock()
		if !joined {
			t.Fatal("actual return failed to retire")
		}
		if err = s.joinProjectWork(context.Background(), h); err != nil || store.calls.Load() != 1 {
			t.Fatal("repeated join repeated original transaction", err)
		}
	})
	t.Run("guard-final-owner-has-context-aware-waiters", func(t *testing.T) {
		s, r, store, h := joinControl(t)
		delete(r.projectWork, h.work.id)
		r.drained = true
		r.drainClosed = true
		store.before = true
		entered, release, returned := make(chan struct{}), make(chan struct{}), make(chan struct{})
		var once sync.Once
		t.Cleanup(func() { once.Do(func() { close(release) }); joinWait(t, returned) })
		store.after = func(_ context.Context, _ f.TransactionCause, result f.CommitResult) f.CommitResult {
			close(entered)
			<-release
			return result
		}
		state := &processState{service: s, bound: true, process: h.work.process}
		guard := &ProcessGuard{data: func() *processState { return state }}
		var original error
		go func() { defer close(returned); original = guard.finish(context.Background()) }()
		joinWait(t, entered)
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
		defer cancel()
		waiter := make(chan error, 1)
		go func() { waiter <- guard.finish(ctx) }()
		select {
		case err := <-waiter:
			if err == nil {
				t.Fatal("short waiter reported guard closed")
			}
		case <-time.After(time.Second):
			t.Fatal("guard mutex blocked waiter's own context")
		}
		state.mu.Lock()
		owned := state.finishDone != nil && !state.closed
		state.mu.Unlock()
		if !owned || store.calls.Load() != 1 {
			t.Fatal("waiting guard stole actual stop owner")
		}
		once.Do(func() { close(release) })
		joinWait(t, returned)
		if !hasCode(original, f.Forbidden) {
			t.Fatal("original guard transaction error changed", original)
		}
	})
}

// Only the first local Close is held. A second Close returning says nothing
// about that original call; there is no socket, proxy or remote peer.
type joinCloseBarrier struct {
	net.Conn
	entered chan struct{}
	release <-chan struct{}
	calls   atomic.Int32
}

func (b *joinCloseBarrier) Close() error {
	if b.calls.Add(1) == 1 {
		close(b.entered)
		<-b.release
	}
	return nil
}
