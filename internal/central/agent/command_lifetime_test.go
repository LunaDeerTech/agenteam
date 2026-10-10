package agent

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/jackc/pgx/v5/pgconn"
)

// This is a controlled physical-outcome seam. It never invokes the SQL
// callback and is not evidence of a successful Agent mutation or authorization.
type confirmationStore struct {
	original       f.CommitResult
	cancelOriginal context.CancelFunc
	entered        chan context.Context
	release        chan struct{}
	mu             sync.Mutex
	calls          int
}

func (s *confirmationStore) WithinTx(ctx context.Context, _ f.TransactionCause, _ func(context.Context, f.Tx) error) f.CommitResult {
	s.mu.Lock()
	s.calls++
	n := s.calls
	s.mu.Unlock()
	if n == 1 {
		s.cancelOriginal()
		return s.original
	}
	if n != 2 {
		panic("unexpected extra attempt")
	}
	s.entered <- ctx
	<-s.release
	return f.NotCommittedResult(f.NewFault(f.DependencyUnavailable, f.NotCommitted).WithCause(ctx.Err()))
}
func (*confirmationStore) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	panic("unexpected SQL")
}
func (*confirmationStore) Query(context.Context, string, ...any) (*postgres.Rows, error) {
	panic("unexpected SQL")
}
func (*confirmationStore) QueryRow(context.Context, string, ...any) postgres.Row {
	panic("unexpected SQL")
}
func (*confirmationStore) InTx(f.Tx) (postgres.SQLExecutor, error) { panic("unexpected SQL") }
func (*confirmationStore) AcquireAll(context.Context, f.Tx, []f.LockRequest) error {
	panic("unexpected SQL")
}
func (*confirmationStore) RequireHeldLocks(context.Context, f.Tx, []f.LockRequest) error {
	panic("unexpected SQL")
}

func TestAgentUnknownConfirmationOwnedUntilActualReturn(t *testing.T) {
	in, _ := planFixture(t)
	cause, err := f.NewCommandsCause(in.identity)
	if err != nil {
		t.Fatal(err)
	}
	attempt := commandID[f.TransactionAttempt](t, "01900000-0000-7000-8000-000000000009")
	original := f.UnknownResult(attempt, cause)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	store := &confirmationStore{original: original, cancelOriginal: cancel, entered: make(chan context.Context, 1), release: make(chan struct{})}
	s := &Service{state: &serviceState{store: store, calls: newCalls()}}
	finished := make(chan error, 1)
	go func() { _, err := s.execute(ctx, in); finished <- err }()
	var once sync.Once
	release := func() { once.Do(func() { close(store.release) }) }
	returned := false
	t.Cleanup(func() {
		s.Stop()
		release()
		if !returned {
			select {
			case <-finished:
			case <-time.After(time.Second):
				t.Error("original call cleanup did not join")
			}
		}
	})
	var confirm context.Context
	select {
	case confirm = <-store.entered:
	case <-time.After(time.Second):
		t.Fatal("confirmation did not enter")
	}
	if ctx.Err() == nil || confirm.Err() != nil {
		t.Fatal("independent confirmation inherited delivery cancellation")
	}
	deadline, ok := confirm.Deadline()
	if !ok || time.Until(deadline) > 3*time.Second {
		t.Fatal("confirmation lost its bounded budget")
	}
	s.Stop()
	if confirm.Err() == nil || s.Joined() {
		t.Fatal("Stop did not cancel or falsely joined held confirmation")
	}
	short, stop := context.WithCancel(context.Background())
	stop()
	if err = s.Drain(short); !errors.Is(err, context.Canceled) || s.Joined() {
		t.Fatal("failed Drain retired held physical call")
	}
	release()
	select {
	case err = <-finished:
		returned = true
	case <-time.After(time.Second):
		t.Fatal("original execute did not return")
	}
	retained, ok := UnknownAttempt(err)
	if !ok || retained.AttemptID() != attempt || retained.Cause().Details().Primary.Canonical() != in.identity.Canonical() {
		t.Fatal("confirmation replaced original Unknown provenance")
	}
	if err = s.Drain(context.Background()); err != nil || !s.Joined() {
		t.Fatal("actual return did not retire original call", err)
	}
	store.mu.Lock()
	calls := store.calls
	store.mu.Unlock()
	if calls != 2 {
		t.Fatal("Unknown repeated writer or confirmation")
	}
}
