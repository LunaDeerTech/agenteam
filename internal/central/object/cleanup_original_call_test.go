package object

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
)

type cleanupOriginalValueKey struct{}
type cleanupCallbackOnlyKey struct{}

// The planner accepts only the original outer call's witness. It deliberately
// rejects a callback-only transaction value in a subsequent transaction.
type cleanupOriginalPlanner struct {
	oc.AccessPlanner
	witness  *int
	discover func(context.Context, oc.AccessRequest) (oc.AccessDependencies, error)
	checked  int
}

func (p *cleanupOriginalPlanner) check(ctx context.Context) error {
	if ctx.Err() != nil || ctx.Value(cleanupOriginalValueKey{}) != p.witness || ctx.Value(cleanupCallbackOnlyKey{}) != nil {
		return failure(f.Forbidden, nil)
	}
	p.checked++
	return nil
}
func (p *cleanupOriginalPlanner) Discover(ctx context.Context, req oc.AccessRequest) (oc.AccessDependencies, error) {
	if err := p.check(ctx); err != nil {
		return oc.AccessDependencies{}, err
	}
	if p.discover != nil {
		return p.discover(ctx, req)
	}
	return p.AccessPlanner.Discover(ctx, req)
}
func (p *cleanupOriginalPlanner) ValidateInTx(ctx context.Context, tx f.Tx, req oc.AccessRequest, deps oc.AccessDependencies) error {
	if err := p.check(ctx); err != nil {
		return err
	}
	return p.AccessPlanner.ValidateInTx(ctx, tx, req, deps)
}

func cleanupOriginalRegistered(s *Service) int {
	r := s.state()
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.cleanupRequests)
}

func TestObjectCleanupOriginalCallMaintenanceReturn(t *testing.T) {
	for _, mode := range []string{"finish-writer", "release-reader"} {
		t.Run(mode, func(t *testing.T) {
			v := newKnowledgeSourcePure(t)
			r := v.service.state()
			witness := new(int)
			original, cancel := context.WithCancel(context.WithValue(context.Background(), cleanupOriginalValueKey{}, witness))
			cancel()
			planner := &cleanupOriginalPlanner{AccessPlanner: v.sourceStopFixture, witness: witness}
			r.auth.Planner = planner
			entered, release, returned := make(chan struct{}), make(chan struct{}), make(chan struct{})
			var once sync.Once
			open := func() { once.Do(func() { close(release) }) }
			var run context.Context
			var result error
			stop := errors.New("original writer planner reached")
			calls := v.store.calls.Load()
			if mode == "finish-writer" {
				r.closedAttempts = map[oc.AttemptID]bool{}
				planner.discover = func(ctx context.Context, req oc.AccessRequest) (oc.AccessDependencies, error) {
					if req.Details().Operation != oc.FinishWriterAccess {
						return oc.AccessDependencies{}, failure(f.Forbidden, nil)
					}
					run = ctx
					close(entered)
					<-release
					return oc.AccessDependencies{}, stop
				}
			} else {
				v.store.after = func(ctx context.Context, _ f.TransactionCause, result f.CommitResult) f.CommitResult {
					run = ctx
					close(entered)
					<-release
					return result
				}
			}
			t.Cleanup(func() {
				open()
				joinWait(t, returned)
				v.store.after = nil
				r.auth.Planner = v.sourceStopFixture
			})
			attempt := attemptRow{id: auditID[oc.Attempt](t), object: v.h.source.Details().Meta.ID}
			go func() {
				defer close(returned)
				if mode == "finish-writer" {
					result = v.service.finishWriter(original, attempt, true, true, nil)
				} else {
					result = v.service.releaseInternalWithValues(original, v.h.id, v.h.source.Details().Meta.ID)
				}
			}()
			joinWait(t, entered)
			if run.Err() != nil || run.Value(cleanupOriginalValueKey{}) != witness || cleanupOriginalRegistered(v.service) != 1 {
				t.Fatal("cancelled caller lost its witness or retired the held cleanup")
			}
			select {
			case <-returned:
				t.Fatal("original method returned before its dependency")
			default:
			}
			if mode == "release-reader" {
				if v.releases != 1 || v.store.calls.Load() != calls+1 || planner.checked != 2 {
					t.Fatal("actual release did not validate and write on the original Store")
				}
				v.store.mu.Lock()
				live := len(v.store.active)
				v.store.mu.Unlock()
				if live != 0 {
					t.Fatal("return barrier was inside a live callback")
				}
			}
			open()
			joinWait(t, returned)
			if cleanupOriginalRegistered(v.service) != 0 || run.Err() == nil {
				t.Fatal("actual method return did not close its cleanup registration")
			}
			if mode == "finish-writer" {
				if !errors.Is(result, stop) || planner.checked != 1 || v.store.calls.Load() != calls {
					t.Fatal("writer did not reach the exact planner boundary", result)
				}
			} else if result != nil {
				t.Fatal("original reader release failed", result)
			}
		})
	}
}

func TestObjectCleanupOriginalCallForceAndDrain(t *testing.T) {
	t.Run("force-retains-registration", func(t *testing.T) {
		s := cleanupBudgetService()
		// This fixture owns no native backend; Force still performs its real
		// cancellation and Drain path around the held cleanup registration.
		s.state().backendsClosed = true
		s.state().workerStop = make(chan struct{})
		witness := new(int)
		original, cancel := context.WithDeadline(context.WithValue(context.Background(), cleanupOriginalValueKey{}, witness), time.Now().Add(-time.Second))
		defer cancel()
		before := time.Now()
		run, finish := s.cleanupContextWithValues(original)
		defer finish()
		deadline, bounded := run.Deadline()
		if run.Err() != nil || run.Value(cleanupOriginalValueKey{}) != witness || !bounded || !deadline.After(before) || deadline.After(time.Now().Add(2*time.Second)) {
			t.Fatal("cleanup did not separate original values from the existing two-second budget")
		}
		force, stop := context.WithCancel(context.Background())
		stop()
		if err := s.Force(force); !errors.Is(err, context.Canceled) {
			t.Fatal("held cleanup did not keep Force pending", err)
		}
		requireCleanupCancelled(t, run)
		if cleanupOriginalRegistered(s) != 1 {
			t.Fatal("Force cancellation impersonated actual cleanup return")
		}
		finish()
		finish()
		if cleanupOriginalRegistered(s) != 0 {
			t.Fatal("actual cleanup return retained its registration")
		}
		late, done := s.cleanupContextWithValues(original)
		defer done()
		requireCleanupCancelled(t, late)
	})
	t.Run("drained-refuses-new-cleanup", func(t *testing.T) {
		s := cleanupBudgetService()
		s.state().drained = true
		run, done := s.cleanupContextWithValues(context.WithValue(context.Background(), cleanupOriginalValueKey{}, new(int)))
		defer done()
		if run.Err() == nil || cleanupOriginalRegistered(s) != 0 {
			t.Fatal("preserving values reopened a fully drained service")
		}
	})
}

type cleanupOriginalStore struct{ *sourceStopStore }

func (s *cleanupOriginalStore) WithinTx(ctx context.Context, cause f.TransactionCause, fn func(context.Context, f.Tx) error) f.CommitResult {
	return s.sourceStopStore.WithinTx(ctx, cause, func(callback context.Context, tx f.Tx) error {
		if cause.Kind() == f.CommandsCause {
			callback = context.WithValue(callback, cleanupCallbackOnlyKey{}, tx)
		}
		return fn(callback, tx)
	})
}

func TestObjectCleanupOriginalCallSourceAcquisition(t *testing.T) {
	v := newKnowledgeSourcePure(t)
	r := v.service.state()
	old := v.h
	// Reuse the issued source tuple with a separate original handle; never reset
	// a returned handle or seed its acquisition outcome/originalValues.
	h := &sourceLeaseState{owner: old.owner, id: auditID[oc.Lease](t), actor: old.actor, source: old.source, process: old.process}
	w := old.work.work
	w.id, w.resource = h.id.String(), h.id.String()
	h.work = &projectWorkHandle{source: h, work: w, registered: true}
	r.projectWork[w.id] = h.work
	v.reads.data().leases[h.id] = h
	v.h = h
	lease, err := oc.NewSourceLease(v.reads.data().issuer, h.id)
	if err != nil {
		t.Fatal(err)
	}
	r.store = &cleanupOriginalStore{sourceStopStore: v.store}
	witness := new(int)
	original, cancel := context.WithCancel(context.WithValue(context.Background(), cleanupOriginalValueKey{}, witness))
	defer cancel()
	planner := &cleanupOriginalPlanner{AccessPlanner: v.sourceStopFixture, witness: witness}
	r.auth.Planner = planner
	d := v.request.Details()
	locks, err := v.request.RequiredLocks()
	if err != nil {
		t.Fatal(err)
	}
	cause, err := f.NewCommandsCause(d.Command)
	if err != nil {
		t.Fatal(err)
	}
	entered, release, returned := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var once sync.Once
	open := func() { once.Do(func() { close(release) }) }
	v.store.after = func(_ context.Context, _ f.TransactionCause, result f.CommitResult) f.CommitResult {
		close(entered)
		<-release
		return result
	}
	var acquisition f.CommitResult
	t.Cleanup(func() {
		open()
		joinWait(t, returned)
		v.store.after = nil
		r.auth.Planner = v.sourceStopFixture
		if err := v.reads.CancelSourceLease(context.Background(), lease); err != nil {
			t.Error("new original source cleanup", err)
		}
		if err := v.reads.ForgetProjectSource(lease); err != nil {
			t.Error("new original source retirement", err)
		}
		v.h, v.leaseState, v.joined = old, "active", false
		r.store = v.store
	})
	go func() {
		defer close(returned)
		acquisition = v.reads.WithinKnowledgeProjectSourceAcquisition(original, cause, func(ctx context.Context, tx f.Tx) error {
			if ctx.Value(cleanupCallbackOnlyKey{}) != tx || ctx.Value(cleanupOriginalValueKey{}) != witness {
				return failure(f.Forbidden, nil)
			}
			h.origin, h.work.origin = tx, tx
			if err := v.store.AcquireAll(ctx, tx, locks); err != nil {
				return err
			}
			return v.reads.BindKnowledgeProjectSourceInTx(ctx, tx, lease, d.TargetProjectID, d.Command)
		})
	}()
	joinWait(t, entered)
	h.mu.Lock()
	early := h.acquireReturned || h.originalValues != nil
	h.mu.Unlock()
	if early {
		t.Fatal("callback completion published original context before Store return")
	}
	if _, err := v.store.InTx(h.origin); err == nil {
		t.Fatal("original acquisition return barrier still has a live callback")
	}
	if err := v.reads.CancelSourceLease(context.Background(), lease); !hasCode(err, f.ResourceBusy) {
		t.Fatal("unreturned acquisition admitted cleanup", err)
	}
	cancel()
	open()
	joinWait(t, returned)
	if acquisition.State() != f.Committed || !h.acquireReturned || h.originalValues == nil || h.originalValues.Value(cleanupOriginalValueKey{}) != witness || h.originalValues.Value(cleanupCallbackOnlyKey{}) != nil {
		t.Fatal("actual acquisition return failed to publish only the outer values", acquisition.Fault())
	}
	// Hold the actual lease release and then the actual work join separately.
	// Both use fresh transactions while retaining the same outer witness.
	steps := make(chan int, 2)
	releases := [2]chan struct{}{make(chan struct{}), make(chan struct{})}
	var opens [2]sync.Once
	openStep := func(n int) { opens[n].Do(func() { close(releases[n]) }) }
	step := 0
	v.store.after = func(ctx context.Context, _ f.TransactionCause, result f.CommitResult) f.CommitResult {
		if ctx.Value(cleanupOriginalValueKey{}) != witness || ctx.Value(cleanupCallbackOnlyKey{}) != nil || ctx.Err() != nil {
			t.Error("new Store lost original values or borrowed the acquisition callback")
		}
		n := step
		step++
		steps <- n
		if n < len(releases) {
			<-releases[n]
		}
		return result
	}
	closed := make(chan struct{})
	var closeErr error
	t.Cleanup(func() { openStep(0); openStep(1); joinWait(t, closed) })
	go func() {
		defer close(closed)
		closeErr = v.reads.CancelSourceLease(context.Background(), lease)
	}()
	for want := range 2 {
		select {
		case got := <-steps:
			if got != want {
				t.Fatal("unexpected release/join Store call", got, want)
			}
		case <-time.After(time.Second):
			t.Fatal("original source cleanup did not reach release/join Store")
		}
		h.mu.Lock()
		pending := h.busy && !h.released && !h.joined
		h.mu.Unlock()
		if !pending || cleanupOriginalRegistered(v.service) != 1 || v.releases != 1 || v.joins != want {
			t.Fatal("callback or cancellation retired the original release/join")
		}
		select {
		case <-closed:
			t.Fatal("CancelSourceLease returned before its original Store")
		default:
		}
		openStep(want)
	}
	joinWait(t, closed)
	if closeErr != nil || cleanupOriginalRegistered(v.service) != 0 || planner.checked != 2 || !h.released || !h.work.joinRetired {
		t.Fatal("original source cleanup did not join after both Store returns", closeErr)
	}
}
