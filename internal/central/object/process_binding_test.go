package object

import (
	"context"
	"errors"
	"testing"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
)

// These controls exercise the original Store port and guard state. They do
// not claim a real flock, database transaction, or foreign death proof.
type processBindingStore struct {
	Store
	inTx func(f.Tx) (postgres.SQLExecutor, error)
}

func (s processBindingStore) InTx(tx f.Tx) (postgres.SQLExecutor, error) { return s.inTx(tx) }

func bindingGuard(t *testing.T, store Store) (*ProcessGuard, *processState, oc.ProcessID) {
	t.Helper()
	id := auditID[oc.Process](t)
	s := &serviceState{store: store, process: id}
	r := &processState{bound: true, process: id, service: &Service{data: func() *serviceState { return s }}}
	return &ProcessGuard{data: func() *processState { return r }}, r, id
}

func TestProcessGuardBindingRequiresOriginalLiveStore(t *testing.T) {
	tx := f.NewTx()
	backendErr := errors.New("original store unavailable")
	calls := 0
	live := true
	store := processBindingStore{inTx: func(got f.Tx) (postgres.SQLExecutor, error) {
		calls++
		if got != tx || !live {
			return nil, backendErr
		}
		return &auditTestStore{tx: tx}, nil
	}}
	// The Store value contains a function and is deliberately not comparable.
	g, r, id := bindingGuard(t, store)
	if err := g.RequireCurrentProcessInTx(context.Background(), tx, id); err != nil || calls != 1 {
		t.Fatal("original live transaction was not checked", err, calls)
	}
	for _, item := range []struct {
		name string
		tx   f.Tx
		live bool
	}{{"foreign-transaction", f.NewTx(), true}, {"expired-transaction", tx, false}} {
		t.Run(item.name, func(t *testing.T) {
			live = item.live
			before := calls
			if err := g.RequireCurrentProcessInTx(context.Background(), item.tx, id); !errors.Is(err, backendErr) || calls != before+1 {
				t.Fatal("original Store rejection was lost", err, calls)
			}
		})
	}
	live = true
	for _, item := range []struct {
		name   string
		change func()
	}{
		{"unbound", func() { r.bound = false }},
		{"closed", func() { r.closed = true }},
		{"finishing", func() { r.finishDone = make(chan struct{}) }},
		{"wrong-process", func() { r.process = auditID[oc.Process](t) }},
	} {
		t.Run(item.name, func(t *testing.T) {
			r.bound, r.closed, r.finishDone, r.process = true, false, nil, id
			item.change()
			before := calls
			if err := g.RequireCurrentProcessInTx(context.Background(), tx, id); err == nil || calls != before {
				t.Fatal("invalid guard reached Store", err, calls)
			}
		})
	}
	r.bound, r.closed, r.finishDone, r.process = true, false, nil, id
	for _, missing := range []*ProcessGuard{nil, {}, {data: func() *processState { return nil }}} {
		if err := missing.RequireCurrentProcessInTx(context.Background(), tx, id); !hasCode(err, f.DependencyUnbound) {
			t.Fatal("missing guard accepted", err)
		}
	}
	before := calls
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := g.RequireCurrentProcessInTx(ctx, tx, id); !errors.Is(err, context.Canceled) {
		t.Fatal("initial cancellation lost", err)
	}
	if g.RequireCurrentProcessInTx(nil, tx, id) == nil || g.RequireCurrentProcessInTx(context.Background(), f.Tx{}, id) == nil || g.RequireCurrentProcessInTx(context.Background(), tx, oc.ProcessID{}) == nil || calls != before {
		t.Fatal("invalid input reached Store")
	}
}

func TestProcessGuardBindingWaitsAndRechecksState(t *testing.T) {
	for _, mode := range []string{"unchanged", "closed-during-store", "cancelled-during-store"} {
		t.Run(mode, func(t *testing.T) {
			entered, release := make(chan struct{}), make(chan struct{})
			tx := f.NewTx()
			var state *processState
			store := processBindingStore{inTx: func(got f.Tx) (postgres.SQLExecutor, error) {
				if got != tx || !state.mu.TryLock() {
					return nil, errors.New("Store called with wrong transaction or held guard mutex")
				}
				state.mu.Unlock()
				close(entered)
				<-release
				return &auditTestStore{tx: tx}, nil
			}}
			g, r, id := bindingGuard(t, store)
			state = r
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan error, 1)
			go func() { done <- g.RequireCurrentProcessInTx(ctx, tx, id) }()
			select {
			case <-entered:
			case err := <-done:
				close(release)
				t.Fatal("original Store was not entered without mutex", err)
			case <-time.After(time.Second):
				close(release)
				t.Fatal("original Store did not enter")
			}
			if mode == "closed-during-store" {
				state.mu.Lock()
				state.closed = true
				state.mu.Unlock()
			}
			if mode == "cancelled-during-store" {
				cancel()
			}
			select {
			case err := <-done:
				close(release)
				t.Fatal("returned before original Store", err)
			default:
			}
			close(release)
			select {
			case err := <-done:
				if mode == "unchanged" && err != nil || mode != "unchanged" && err == nil {
					t.Fatal("post-return guard/context check incorrect", err)
				}
				if mode == "cancelled-during-store" && !errors.Is(err, context.Canceled) {
					t.Fatal("post-return cancellation lost", err)
				}
			case <-time.After(time.Second):
				t.Fatal("did not join original Store return")
			}
		})
	}
}
