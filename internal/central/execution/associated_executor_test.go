package execution

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	c "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

// Only the private physical-call seam is controlled here. These tests exercise
// admission, lifetime and recovery scheduling, not canonical SQL association,
// preparing authority, Model permission or a committed terminal transition.
func associatedTestExecutor(ops associatedExecutorOps, max int, interval time.Duration) *AssociatedExecutor {
	return &AssociatedExecutor{state: &associatedExecutorState{
		options: AssociatedExecutorOptions{MaxOwned: max, RecoveryInterval: interval},
		ops:     ops, changed: make(chan struct{}), ready: make(chan struct{}),
		jobs: make(map[i.ExecutionID]*associatedCall),
	}}
}

func associatedTestIdentity(t *testing.T) (i.ProjectID, c.AssociatedDispatch) {
	t.Helper()
	dispatch := newTestID[struct{}](t).String()
	return newTestID[i.Project](t), c.AssociatedDispatch{
		ExecutionID: newTestID[i.Execution](t), AgentID: newTestID[i.Agent](t),
		DispatchID: dispatch, Key: f.IdempotencyKey("scheduler_dispatch:" + dispatch),
		Digest: f.Digest("sha256:" + strings.Repeat("a", 64)),
	}
}

func associatedTestReceive[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	timer := time.NewTimer(3 * time.Second)
	defer timer.Stop()
	select {
	case value := <-ch:
		return value
	case <-timer.C:
		t.Fatal("controlled original call did not return")
		var zero T
		return zero
	}
}

func associatedTestRun(t *testing.T, e *AssociatedExecutor, ctx context.Context) <-chan error {
	t.Helper()
	done := make(chan error, 1)
	go func() {
		defer close(done)
		done <- e.Run(ctx)
	}()
	t.Cleanup(func() {
		e.Stop()
		timer := time.NewTimer(3 * time.Second)
		defer timer.Stop()
		select {
		case <-done:
		case <-timer.C:
			t.Error("original Run goroutine remained live after controlled release")
		}
	})
	associatedTestReceive(t, e.Ready())
	return done
}

func associatedTestIdle(t *testing.T, e *AssociatedExecutor) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	for {
		e.state.mu.Lock()
		idle, changed := e.state.workers == 0, e.state.changed
		e.state.mu.Unlock()
		if idle {
			return
		}
		select {
		case <-changed:
		case <-ctx.Done():
			t.Fatal("executor reported before its original worker exited")
		}
	}
}

func TestAssociatedExecutorAdmissionOwnsExactTupleAndCapacity(t *testing.T) {
	project, dispatch := associatedTestIdentity(t)
	entered, release := make(chan struct{}, 1), make(chan struct{})
	var once sync.Once
	defer once.Do(func() { close(release) })
	var reads, prepares atomic.Int32
	rejected := fault(f.DependencyUnavailable)
	e := associatedTestExecutor(associatedExecutorOps{
		inspect: func(context.Context, i.ProjectID, c.AssociatedDispatch) (associatedFacts, error) {
			if reads.Add(1) == 1 {
				entered <- struct{}{}
				<-release
			}
			return associatedFacts{status: c.Created}, nil
		},
		prepare: func(context.Context, i.ExecutionID) error { prepares.Add(1); return rejected },
		owners:  func(*associatedCall) associatedOwners { return associatedOwners{} },
	}, 1, time.Second)
	if got, err := e.Advance(context.Background(), project, dispatch); err == nil || got.Active {
		t.Fatal("admitted without a Run lifetime")
	}
	if reads.Load() != 0 {
		t.Fatal("rejected admission touched a driver")
	}
	runCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := associatedTestRun(t, e, runCtx)
	defer e.Stop()
	first, err := e.Advance(context.Background(), project, dispatch)
	if err != nil || first.ExecutionID != dispatch.ExecutionID || !first.Active || first.Joined {
		t.Fatal("asynchronous admission failed", err)
	}
	associatedTestReceive(t, entered)
	for range 8 {
		got, err := e.Advance(context.Background(), project, dispatch)
		if err != nil || got != first {
			t.Fatal("exact duplicate did not observe the same live call", err)
		}
	}
	for _, change := range []func(*c.AssociatedDispatch){
		func(v *c.AssociatedDispatch) { v.AgentID = newTestID[i.Agent](t) },
		func(v *c.AssociatedDispatch) { v.Key = "different-key" },
		func(v *c.AssociatedDispatch) { v.Digest = f.Digest("sha256:" + strings.Repeat("b", 64)) },
		func(v *c.AssociatedDispatch) { v.DispatchID = newTestID[struct{}](t).String() },
	} {
		bad := dispatch
		change(&bad)
		_, err := e.Advance(context.Background(), project, bad)
		requireCode(t, err, f.ConfirmationStale)
	}
	_, err = e.Advance(context.Background(), newTestID[i.Project](t), dispatch)
	requireCode(t, err, f.ConfirmationStale)
	_, other := associatedTestIdentity(t)
	_, err = e.Advance(context.Background(), project, other)
	requireCode(t, err, f.ResourceBusy)
	once.Do(func() { close(release) })
	associatedTestIdle(t, e)
	// A known failed attempt with no durable exclusion must still use capacity;
	// a repeated visit must not silently create another preparation attempt.
	got, err := e.Advance(context.Background(), project, dispatch)
	if !errors.Is(err, rejected) || got.Active || got.Retained || !got.Joined {
		t.Fatal("known failure lost its exact returned result", err)
	}
	_, err = e.Advance(context.Background(), project, other)
	requireCode(t, err, f.ResourceBusy)
	if reads.Load() != 2 || prepares.Load() != 1 {
		t.Fatal("deduplication dispatched extra work")
	}
	e.Stop()
	if err = associatedTestReceive(t, done); err != nil || !e.Joined() {
		t.Fatal("returned nonretained owner did not join", err)
	}
}

func TestAssociatedExecutorRunLifetimeStopsOnlyOwnedCalls(t *testing.T) {
	type contextMarker struct{}
	project, dispatch := associatedTestIdentity(t)
	entered, cancelled := make(chan context.Context, 1), make(chan struct{}, 1)
	release := make(chan struct{})
	var once sync.Once
	defer once.Do(func() { close(release) })
	e := associatedTestExecutor(associatedExecutorOps{
		inspect: func(context.Context, i.ProjectID, c.AssociatedDispatch) (associatedFacts, error) {
			return associatedFacts{status: c.Preparing, inputReady: true}, nil
		},
		start: func(ctx context.Context, _ i.ExecutionID) (c.DirectTextReceipt, error) {
			entered <- ctx
			<-ctx.Done()
			cancelled <- struct{}{}
			<-release
			return c.DirectTextReceipt{}, ctx.Err()
		},
		owners: func(*associatedCall) associatedOwners { return associatedOwners{} },
	}, 2, time.Second)
	// Shared drivers contain an unrelated controlled owner. Stopping this
	// executor may not call their global Stop/Drain or cancel that owner.
	foreignCtx, foreignCancel := context.WithCancel(context.Background())
	defer foreignCancel()
	foreignID := newTestID[i.Execution](t)
	e.state.preparation = &PreparationDriver{state: &preparationState{
		calls: map[i.ExecutionID]*preparationCall{foreignID: {cancel: foreignCancel}}, changed: make(chan struct{}),
	}}
	e.state.direct = &DirectTextDriver{state: &directTextState{
		calls: map[i.ExecutionID]*directTextCall{foreignID: {cancel: foreignCancel}}, changed: make(chan struct{}),
	}}
	runCtx, cancelRun := context.WithCancel(context.WithValue(context.Background(), contextMarker{}, "run-lifetime"))
	defer cancelRun()
	done := associatedTestRun(t, e, runCtx)
	defer e.Stop()
	admission, cancelAdmission := context.WithCancel(context.WithValue(context.Background(), contextMarker{}, "short-visit"))
	if _, err := e.Advance(admission, project, dispatch); err != nil {
		cancelAdmission()
		t.Fatal(err)
	}
	ownerCtx := associatedTestReceive(t, entered)
	cancelAdmission()
	if ownerCtx.Err() != nil || ownerCtx.Value(contextMarker{}) != "run-lifetime" {
		t.Fatal("short admission context replaced or cancelled the owner lifetime")
	}
	cancelRun()
	associatedTestReceive(t, cancelled)
	e.Stop()
	if e.Joined() || foreignCtx.Err() != nil || e.state.preparation.state.stopped || e.state.direct.state.stopped {
		t.Fatal("Stop claimed an early join or stopped a shared driver")
	}
	select {
	case <-done:
		t.Fatal("Run returned before its original call physically exited")
	default:
	}
	budget, cancelBudget := context.WithCancel(context.Background())
	cancelBudget()
	if err := e.Drain(budget); !errors.Is(err, context.Canceled) || e.Joined() {
		t.Fatal("expired cleanup budget discarded the active owner", err)
	}
	once.Do(func() { close(release) })
	if err := associatedTestReceive(t, done); !errors.Is(err, context.Canceled) || !e.Joined() {
		t.Fatal("Run did not wait for its own physical return", err)
	}
	if err := e.Drain(context.Background()); err != nil || foreignCtx.Err() != nil {
		t.Fatal("joined cleanup touched another owner", err)
	}
	// A worker still reading before Start cannot turn Stop into its first
	// Model dispatch, even when that original read returns ready input.
	reading, allowRead := make(chan struct{}, 1), make(chan struct{})
	var releaseRead sync.Once
	defer releaseRead.Do(func() { close(allowRead) })
	var lateStarts atomic.Int32
	beforeStart := associatedTestExecutor(associatedExecutorOps{
		inspect: func(context.Context, i.ProjectID, c.AssociatedDispatch) (associatedFacts, error) {
			reading <- struct{}{}
			<-allowRead
			return associatedFacts{status: c.Preparing, inputReady: true}, nil
		},
		start: func(context.Context, i.ExecutionID) (c.DirectTextReceipt, error) {
			lateStarts.Add(1)
			return c.DirectTextReceipt{}, fault(f.DependencyUnbound)
		},
		owners: func(*associatedCall) associatedOwners { return associatedOwners{} },
	}, 1, time.Second)
	beforeDone := associatedTestRun(t, beforeStart, context.Background())
	if _, err := beforeStart.Advance(context.Background(), project, dispatch); err != nil {
		t.Fatal(err)
	}
	associatedTestReceive(t, reading)
	beforeStart.Stop()
	if beforeStart.Joined() {
		t.Fatal("Stop forgot the original pre-start read")
	}
	releaseRead.Do(func() { close(allowRead) })
	if err := associatedTestReceive(t, beforeDone); err != nil || !beforeStart.Joined() || lateStarts.Load() != 0 {
		t.Fatal("shutdown dispatched a first Model call", err)
	}
}

func TestAssociatedExecutorUnknownRecoveryIsPacedAndUsesOriginalOwner(t *testing.T) {
	project, dispatch := associatedTestIdentity(t)
	originalCause := errors.New("controlled-original-attempt")
	unknown := f.NewFault(f.CommitUnknown, f.Unknown).WithCause(originalCause)
	var mu sync.Mutex
	owned, resolved := false, false
	var token any
	var prepares, recoveries, starts atomic.Int32
	entered, release := make(chan struct{}, 1), make(chan struct{})
	var once sync.Once
	defer once.Do(func() { close(release) })
	e := associatedTestExecutor(associatedExecutorOps{
		inspect: func(context.Context, i.ProjectID, c.AssociatedDispatch) (associatedFacts, error) {
			mu.Lock()
			defer mu.Unlock()
			if resolved {
				return associatedFacts{status: c.Preparing, inputReady: true}, nil
			}
			return associatedFacts{status: c.Created}, nil
		},
		prepare: func(ctx context.Context, _ i.ExecutionID) error {
			prepares.Add(1)
			mu.Lock()
			owned, token = true, ctx.Value(associatedContextKey{})
			mu.Unlock()
			return unknown
		},
		recoverPreparation: func(ctx context.Context, id i.ExecutionID) error {
			recoveries.Add(1)
			mu.Lock()
			same := token != nil && token == ctx.Value(associatedContextKey{}) && id == dispatch.ExecutionID
			mu.Unlock()
			if !same {
				return fault(f.Forbidden)
			}
			entered <- struct{}{}
			<-release
			mu.Lock()
			owned, resolved = false, true
			mu.Unlock()
			return nil
		},
		start: func(_ context.Context, id i.ExecutionID) (c.DirectTextReceipt, error) {
			starts.Add(1)
			return c.DirectTextReceipt{ExecutionID: id, Status: c.Succeeded, Version: 4}, nil
		},
		owners: func(*associatedCall) associatedOwners {
			mu.Lock()
			defer mu.Unlock()
			if owned {
				return associatedOwners{preparation: true, err: unknown}
			}
			return associatedOwners{}
		},
	}, 1, 200*time.Millisecond)
	runCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := associatedTestRun(t, e, runCtx)
	defer e.Stop()
	if _, err := e.Advance(context.Background(), project, dispatch); err != nil {
		t.Fatal(err)
	}
	associatedTestIdle(t, e)
	for range 8 {
		got, err := e.Advance(context.Background(), project, dispatch)
		if got.Active || !got.Retained || got.Joined || !errors.Is(err, unknown) || !errors.Is(err, originalCause) {
			t.Fatal("Unknown lost ownership/cause or retried before its interval", err)
		}
	}
	if recoveries.Load() != 0 || prepares.Load() != 1 || starts.Load() != 0 {
		t.Fatal("Unknown was redispatched")
	}
	// No background retry: even after the interval the explicit visit owns the
	// single recovery. Waiting here uses the real configured clock, not a loop.
	timer := time.NewTimer(250 * time.Millisecond)
	defer timer.Stop()
	<-timer.C
	if recoveries.Load() != 0 {
		t.Fatal("executor introduced a background retry loop")
	}
	if got, err := e.Advance(context.Background(), project, dispatch); !got.Active || !errors.Is(err, unknown) {
		t.Fatal("due recovery lost the retained result", err)
	}
	associatedTestReceive(t, entered)
	for range 8 {
		if got, err := e.Advance(context.Background(), project, dispatch); !got.Active || !errors.Is(err, originalCause) {
			t.Fatal("in-flight recovery was replaced", err)
		}
	}
	if recoveries.Load() != 1 {
		t.Fatal("concurrent visits repeated recovery")
	}
	once.Do(func() { close(release) })
	associatedTestIdle(t, e)
	if prepares.Load() != 1 || recoveries.Load() != 1 || starts.Load() != 1 {
		t.Fatal("recovery created another preparation or lost its one start")
	}
	e.Stop()
	if err := associatedTestReceive(t, done); err != nil || !e.Joined() {
		t.Fatal("recovered original owner did not join", err)
	}
}

func TestAssociatedExecutorCurrentFactsDeferUnsafeTakeover(t *testing.T) {
	if got, err := NewAssociatedExecutor(nil, nil, AssociatedExecutorOptions{MaxOwned: 1, RecoveryInterval: time.Second}); got != nil || err == nil {
		t.Fatal("missing real drivers became an executor")
	}
	for _, facts := range []associatedFacts{
		{status: c.Preparing}, {status: c.Preparing, claimTerminal: true},
		{status: c.Running}, {status: c.Waiting}, {status: c.Succeeded},
	} {
		project, dispatch := associatedTestIdentity(t)
		var reads, effects atomic.Int32
		e := associatedTestExecutor(associatedExecutorOps{
			inspect: func(context.Context, i.ProjectID, c.AssociatedDispatch) (associatedFacts, error) {
				reads.Add(1)
				return facts, nil
			},
			prepare: func(context.Context, i.ExecutionID) error { effects.Add(1); return fault(f.DependencyUnbound) },
			start: func(context.Context, i.ExecutionID) (c.DirectTextReceipt, error) {
				effects.Add(1)
				return c.DirectTextReceipt{}, fault(f.DependencyUnbound)
			},
			owners: func(*associatedCall) associatedOwners { return associatedOwners{} },
		}, 1, time.Second)
		runCtx, cancel := context.WithCancel(context.Background())
		done := associatedTestRun(t, e, runCtx)
		for range 2 {
			if _, err := e.Advance(context.Background(), project, dispatch); err != nil {
				cancel()
				t.Fatal("read-only current observation failed", err)
			}
			associatedTestIdle(t, e)
		}
		e.Stop()
		err := associatedTestReceive(t, done)
		cancel()
		if err != nil || !e.Joined() || reads.Load() != 2 || effects.Load() != 0 {
			t.Fatal("existing state was recaptured, restarted or retained without an owner", facts.status, err)
		}
	}
}
