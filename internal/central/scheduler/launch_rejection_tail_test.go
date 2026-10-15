package scheduler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	wc "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
)

// The existing physical Store control is wrapped only at the returned rejection
// Tx. This is not a PG commit proof; the original owners, private witnesses,
// complete lock verification and actual synchronous return are exercised.
type rejectionTailStore struct {
	Store
	beforeReturn func(context.Context, f.Tx) error
	rejects      int
}

func (s *rejectionTailStore) WithinTx(ctx context.Context, cause f.TransactionCause, fn func(context.Context, f.Tx) error) f.CommitResult {
	if cause.Details().Primary.Command() != "reject_launch" {
		return s.Store.WithinTx(ctx, cause, fn)
	}
	s.rejects++
	return s.Store.WithinTx(ctx, cause, func(inner context.Context, tx f.Tx) error {
		if err := fn(inner, tx); err != nil {
			return err
		}
		if s.beforeReturn != nil {
			return s.beforeReturn(inner, tx)
		}
		return nil
	})
}

func TestSchedulerReturnedRejectionTailKeepsOriginalOwner(t *testing.T) {
	for _, timing := range []string{"cancel-before-return", "stop-during-checkpoint"} {
		t.Run(timing, func(t *testing.T) {
			_, store, execution := newHandoffTest(t)
			calls := newSchedulerCalls(t)
			controlled := &rejectionTailStore{Store: store}
			calls.store.Store = controlled
			s := calls.pair.Handoff()
			execution.authority = s.authority
			s.deps = LaunchHandoffDependencies{Executions: execution, Observations: execution}
			entered, release := make(chan struct{}), make(chan struct{})
			checkpoint, returnTx := make(chan struct{}), make(chan struct{})
			var onceLaunch, onceTx sync.Once
			releaseLaunch := func() { onceLaunch.Do(func() { close(release) }) }
			releaseTx := func() { onceTx.Do(func() { close(returnTx) }) }
			defer releaseLaunch()
			defer releaseTx()
			execution.onLaunch = func(ctx context.Context) error {
				close(entered)
				<-release
				return f.NewFault(f.InternalError, f.NotCommitted).WithCause(context.Canceled)
			}
			controlled.beforeReturn = func(ctx context.Context, tx f.Tx) error {
				deadline, bounded := ctx.Deadline()
				if !bounded || time.Until(deadline) > 3*time.Second || ctx.Err() != nil {
					return errors.New("returned checkpoint did not retain its bounded live context")
				}
				if _, ok := ctx.Value(projectCallContextKey{}).(projectCallWitness); !ok {
					return errors.New("original ProjectCall witness lost")
				}
				locks, err := handoffLocks(store.row.project, store.row.id, "reject_launch", store.row)
				if err != nil {
					return err
				}
				if err = s.authority.store.RequireHeldLocks(ctx, tx, locks); err != nil {
					return err
				}
				close(checkpoint)
				<-returnTx
				return ctx.Err()
			}
			done := make(chan error, 1)
			joined := false
			t.Cleanup(func() {
				releaseLaunch()
				releaseTx()
				if !joined {
					<-done
				}
			})
			go func() { _, err := s.LaunchOnce(context.Background(), store.row.project, store.row.id); done <- err }()
			select {
			case <-entered:
			case <-time.After(time.Second):
				t.Fatal("original Launch did not enter")
			}
			if timing == "cancel-before-return" {
				calls.pair.Stop()
				if s.Joined() || calls.pair.Joined() || controlled.rejects != 0 {
					t.Fatal("cancellation replaced the held original Launch return")
				}
			}
			releaseLaunch()
			select {
			case <-checkpoint:
			case err := <-done:
				joined = true
				t.Fatalf("returned rejection did not reach its original Tx: %T", err)
			case <-time.After(time.Second):
				t.Fatal("returned rejection did not reach checkpoint")
			}
			calls.pair.Stop()
			if s.Joined() || calls.pair.Joined() {
				t.Fatal("checkpoint cancellation replaced its actual Store return")
			}
			releaseTx()
			err := <-done
			joined = true
			if !errors.Is(err, context.Canceled) || uncertainProjectVisit(err) {
				t.Fatal("proven original rejection became uncertain")
			}
			if store.row.outcome != KnownNotCreated || store.row.attempts != 1 || store.row.version != 3 || store.row.busyAttempt != 0 || store.row.finalAttempt != 0 || store.row.temporaryAttempt != 0 || execution.launches != 1 || execution.lookups != 0 || controlled.rejects != 1 {
				t.Fatal("returned checkpoint changed the original rejection/dispatch tuple")
			}
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			if err := calls.pair.Drain(ctx); err != nil || !calls.pair.Joined() {
				t.Fatal("original call owners did not actually join")
			}
		})
	}
}

func TestSchedulerReturnedRejectionKeepsUncertainOutcome(t *testing.T) {
	for _, mode := range []string{"bare-cancel", "wrapped-unknown", "joined-unknown", "checkpoint-unknown-before", "checkpoint-unknown-after"} {
		t.Run(mode, func(t *testing.T) {
			s, store, execution := newHandoffTest(t)
			execution.onLaunch = func(context.Context) error {
				s.Stop()
				known := f.NewFault(f.InternalError, f.NotCommitted).WithCause(context.Canceled)
				switch mode {
				case "bare-cancel":
					return context.Canceled
				case "wrapped-unknown":
					return f.NewFault(f.InternalError, f.NotCommitted).WithCause(errors.Join(known, f.NewFault(f.CommitUnknown, f.Unknown).WithCause(errors.New("private-rejection-canary"))))
				case "joined-unknown":
					return errors.Join(known, f.NewFault(f.CommitUnknown, f.Unknown).WithCause(errors.New("private-rejection-canary")))
				default:
					store.failName, store.failMode = "reject_launch", mode[len("checkpoint-"):]
					return known
				}
			}
			_, original := s.LaunchOnce(context.Background(), store.row.project, store.row.id)
			if original == nil || !uncertainProjectVisit(original) || s.Joined() || execution.launches != 1 {
				t.Fatal("uncertain original was discarded")
			}
			raw, marshalErr := json.Marshal(original)
			if marshalErr != nil || strings.Contains(original.Error()+fmt.Sprintf("%#v", original)+string(raw), "private-rejection-canary") {
				t.Fatal("uncertain error exposed private diagnostic cause")
			}
			if mode == "checkpoint-unknown-before" || mode == "checkpoint-unknown-after" {
				attempt, ok := UnknownAttempt(original)
				if !ok || attempt.AttemptID() != store.attempt {
					t.Fatal("cancellation lost original checkpoint Unknown attempt")
				}
			}
			version := store.row.version
			out, err := s.Lookup(context.Background(), store.row.project, store.row.id)
			if mode == "checkpoint-unknown-after" {
				if err != nil || out.Summary().LaunchOutcome != KnownNotCreated || !s.Joined() || execution.lookups != 0 {
					t.Fatal("read-only committed rejection observation did not join")
				}
			} else if err != original || out.data != nil || s.Joined() || store.row.outcome != Unknown || execution.lookups != 1 {
				t.Fatal("not-observed replaced original unknown with a negative proof")
			}
			if store.row.version != version || execution.launches != 1 {
				t.Fatal("original uncertainty observation rewrote or resent the dispatch")
			}
		})
	}
}

func TestSchedulerReturnedRejectionRejectsChangedOwnerAndTuple(t *testing.T) {
	for _, mode := range []string{"no-private-context", "foreign-owner", "live-send", "changed-record", "changed-attempt", "finished-owner"} {
		t.Run(mode, func(t *testing.T) {
			s, store, _ := newHandoffTest(t)
			ctx, call, err := s.begin(context.Background(), store.row.project, store.row.id, false)
			if err != nil {
				t.Fatal(err)
			}
			expected, sent, err := s.markAttempt(ctx, call, false)
			if err != nil || !sent {
				t.Fatal("marker did not commit")
			}
			s.activate(call, expected, true)
			s.deactivate(call)
			checkCtx := ctx
			switch mode {
			case "no-private-context":
				checkCtx = context.Background()
			case "foreign-owner":
				call.owner = &LaunchHandoff{}
			case "live-send":
				s.activate(call, expected, true)
			case "changed-record":
				copied := *expected
				expected = &copied
			case "changed-attempt":
				changed := *store.row
				changed.attempts++
				store.row = &changed
			case "finished-owner":
				s.finish(call, false, nil)
			}
			version := store.row.version
			if _, err = s.recordRejected(checkCtx, call, expected, false, wc.TaskLaunchFailureReason(""), false); err == nil || store.row.version != version || store.row.outcome != Unknown {
				t.Fatal("changed owner/record authorized returned rejection")
			}
			if mode != "finished-owner" {
				s.finish(call, false, nil)
			}
		})
	}
}
