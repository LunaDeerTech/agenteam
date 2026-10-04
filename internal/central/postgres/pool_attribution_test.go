package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgconn/ctxwatch"
)

func attributionOperation(t *testing.T, s *storeState, target *poolCancelTarget, caller context.Context) *operation {
	t.Helper()
	ctx, cancel := context.WithCancel(caller)
	op := &operation{owner: s, caller: caller, ctx: ctx, cancel: cancel, target: target}
	s.mu.Lock()
	s.operations[op] = struct{}{}
	target.retainLocked()
	s.mu.Unlock()
	t.Cleanup(op.release)
	return op
}

func attributionClosedError(t *testing.T, target *poolCancelTarget, ctx context.Context) error {
	t.Helper()
	_, err := target.pg.Exec(ctx, "SELECT 1").ReadAll()
	if !errors.Is(err, pgconn.ErrConnClosed) {
		t.Fatal("fixture did not obtain the driver's exact closed-connection error")
	}
	return err
}

func TestPoolCancellationAttributionRequiresMatchingOwnerDiscard(t *testing.T) {
	for _, mode := range []string{"caller", "item", "deadline"} {
		t.Run(mode, func(t *testing.T) {
			s := cancellationTestState()
			target, count, reached := cancellationTestTarget(t, s, "ack", nil)
			caller, cancelCaller := context.WithCancelCause(context.Background())
			defer cancelCaller(nil)
			op := attributionOperation(t, s, target, caller)
			item, cancelItem := context.WithCancelCause(op.ctx)
			defer cancelItem(nil)
			var watched, source context.Context = item, item
			reason := errors.New("private cancellation cause")
			want := error(context.Canceled)
			if mode == "caller" {
				watched, source = op.ctx, caller
			}
			if mode == "deadline" {
				deadline, cancel := context.WithTimeout(item, 20*time.Millisecond)
				defer cancel()
				watched, source, want = deadline, deadline, context.DeadlineExceeded
			}
			call := op.sqlContext(watched, source)
			watcher := ctxwatch.NewContextWatcher(&poolCancelWatcher{target: target})
			watcher.Watch(call)
			if mode == "caller" {
				cancelCaller(reason)
			} else if mode == "item" {
				cancelItem(reason)
			}
			cancellationAwait(t, reached)
			// CancelRequest and data close alone are not the SQL owner's discard.
			if got := call.sqlError(pgconn.ErrConnClosed); got != pgconn.ErrConnClosed {
				t.Fatal("attributed cancellation before the matching owner Unwatch")
			}
			watcher.Unwatch()
			raw := attributionClosedError(t, target, call)
			got := call.sqlError(raw)
			if !errors.Is(got, pgconn.ErrConnClosed) || !errors.Is(got, want) || count.Load() != 1 {
				t.Fatal("matching cancellation lost its original driver or context cause")
			}
			if mode != "deadline" && !errors.Is(got, reason) {
				t.Fatal("explicit cancellation cause lost")
			}
			if mode != "caller" && caller.Err() != nil {
				t.Fatal("item cancellation changed the operation's caller")
			}
			call.finish() // initialization/previous SQL scope has ended
			s.mu.Lock()
			if target.sql != nil {
				t.Error("completed SQL scope retained the active target binding")
			}
			s.mu.Unlock()
			other := op.sqlContext(watched, source)
			if other.sqlError(raw) != raw {
				t.Fatal("another SQL call inherited cancellation provenance")
			}
			call.finish() // a deferred old initializer must not erase user SQL
			s.mu.Lock()
			if target.sql != other {
				t.Error("old initialization scope erased a later SQL owner")
			}
			s.mu.Unlock()
			other.finish()
			hard := &pgconn.PgError{Code: "XX000", Message: "independent hard error"}
			if call.sqlError(hard) != hard {
				t.Fatal("proved cancellation hid an unrelated hard error")
			}
		})
	}
}

func TestPoolCancellationAttributionRejectsOtherCloseSources(t *testing.T) {
	for _, mode := range []string{"no_watch_later_cancel", "already_closed", "cleanup", "cleanup_then_caller", "force", "work_already_selected", "other_target", "unregistered", "untyped_context"} {
		t.Run(mode, func(t *testing.T) {
			s := cancellationTestState()
			target, _, reached := cancellationTestTarget(t, s, "ack", nil)
			caller, cancelCaller := context.WithCancel(context.Background())
			defer cancelCaller()
			op := attributionOperation(t, s, target, caller)
			call := op.sqlContext(op.ctx, caller)
			if mode == "no_watch_later_cancel" || mode == "already_closed" {
				_ = target.data.Close()
				target.discard() // independent local driver close, before cancellation
			}
			if mode == "force" {
				s.mu.Lock()
				s.force = &poolForcePhase{total: newPoolCancelBudget(time.Now().Add(time.Second)), request: newPoolCancelBudget(time.Now().Add(time.Second))}
				s.mu.Unlock()
				defer s.force.total.stop(context.Canceled)
				defer s.force.request.stop(context.Canceled)
			}
			if mode == "work_already_selected" {
				s.mu.Lock()
				target.workLocked()
				s.mu.Unlock()
			}
			if mode == "other_target" {
				// The actual watcher must reject a call bound to another physical
				// target even when its logical caller and Store are the same.
				call.target = &poolCancelTarget{owner: s}
			}
			if mode == "unregistered" {
				s.mu.Lock()
				delete(s.operations, op)
				s.mu.Unlock()
			}
			watcher := ctxwatch.NewContextWatcher(&poolCancelWatcher{target: target})
			if mode != "no_watch_later_cancel" {
				var ctx context.Context = call
				if mode == "untyped_context" {
					ctx = op.ctx
				}
				watcher.Watch(ctx)
			}
			if mode == "cleanup" || mode == "cleanup_then_caller" {
				op.cancelSQL() // internal Rows/lifecycle cancellation
				if mode == "cleanup_then_caller" {
					cancelCaller()
				}
			} else {
				cancelCaller()
			}
			if mode != "no_watch_later_cancel" {
				cancellationAwait(t, reached)
				watcher.Unwatch()
			}
			raw := attributionClosedError(t, target, call)
			if got := call.sqlError(raw); got != raw || errors.Is(got, context.Canceled) || errors.Is(got, context.DeadlineExceeded) {
				t.Fatal("independent/Force/cleanup close was misattributed to caller cancellation")
			}
		})
	}
}

func TestPoolCancellationBusinessCloseSurvivesCompletedScope(t *testing.T) {
	s := cancellationTestState()
	target, count, reached := cancellationTestTarget(t, s, "ack", nil)
	op := attributionOperation(t, s, target, context.Background())
	item, cancel := context.WithCancelCause(op.ctx)
	defer cancel(nil)
	cause := errors.New("private business cancellation")
	first := op.businessSQLContext(item, item)
	watcher := ctxwatch.NewContextWatcher(&poolCancelWatcher{target: target})
	watcher.Watch(first)
	cancel(cause)
	cancellationAwait(t, reached)
	watcher.Unwatch()
	raw := attributionClosedError(t, target, first)
	// A successful raw SQL returns through this same scope cleanup. The local
	// closing fact must outlive it without retaining its active SQL permission.
	first.cancelInternal(func() {})
	first.finish()
	next := op.businessSQLContext(item, item)
	got := next.sqlError(raw)
	if !errors.Is(got, pgconn.ErrConnClosed) || !errors.Is(got, context.Canceled) || !errors.Is(got, cause) || count.Load() != 1 {
		t.Fatal("same business caller lost the proved local closing cause")
	}
	if first.sqlError(raw) != raw {
		t.Fatal("completed internal scope regained an error permission")
	}
	hard := &pgconn.PgError{Code: "XX000", Message: "independent error"}
	if next.sqlError(hard) != hard {
		t.Fatal("terminal proof hid an unrelated driver error")
	}
	next.finish()
	op.release()
	s.mu.Lock()
	defer s.mu.Unlock()
	if target.closedBy != nil || len(s.operations)+len(s.targets)+len(s.works) != 0 {
		t.Fatal("retired ownership retained a terminal proof")
	}
}

type attributionUncomparableContext struct {
	context.Context
	values []byte
}

// Its type is comparable but its dynamic interface field need not be. A
// reflect.Type.Comparable check alone is insufficient to prevent a panic.
type attributionInterfaceContext struct {
	context.Context
	value any
}

func TestPoolCancellationBusinessCloseDoesNotMigrate(t *testing.T) {
	for _, mode := range []string{
		"different_caller", "same_done_wrapper", "different_operation", "different_target", "unregistered",
		"force_after", "internal_next", "finished_next", "init_source", "init_target",
		"internal_origin", "already_closed", "different_watched_cause", "slice_context", "interface_context",
	} {
		t.Run(mode, func(t *testing.T) {
			s := cancellationTestState()
			target, _, reached := cancellationTestTarget(t, s, "ack", nil)
			op := attributionOperation(t, s, target, context.Background())
			item, cancel := context.WithCancelCause(op.ctx)
			defer cancel(nil)
			cause := errors.New("private original cause")
			var source context.Context = item
			if mode == "slice_context" {
				source = attributionUncomparableContext{Context: item, values: []byte{1}}
			}
			if mode == "interface_context" {
				source = attributionInterfaceContext{Context: item, value: []byte{1}}
			}
			watched := source
			cancelWatched := func() {}
			if mode == "different_watched_cause" {
				other, otherCancel := context.WithCancelCause(op.ctx)
				defer otherCancel(nil)
				watched = other
				cancelWatched = func() { otherCancel(errors.New("unrelated watched cause")) }
			}
			first := op.businessSQLContext(watched, source)
			if mode == "init_source" {
				first = op.sqlContext(watched, source)
			}
			if mode == "internal_origin" {
				first.cancelInternal(func() {})
			}
			if mode == "already_closed" {
				_ = target.data.Close()
				target.discard()
			}
			watcher := ctxwatch.NewContextWatcher(&poolCancelWatcher{target: target})
			watcher.Watch(first)
			cancel(cause)
			cancelWatched()
			cancellationAwait(t, reached)
			watcher.Unwatch()
			raw := attributionClosedError(t, target, first)
			first.cancelInternal(func() {})
			first.finish()
			consumer := op
			if mode == "different_operation" {
				consumer = attributionOperation(t, s, target, context.Background())
			}
			var nextSource context.Context = source
			if mode == "different_caller" {
				other, otherCancel := context.WithCancelCause(op.ctx)
				defer otherCancel(nil)
				otherCancel(cause) // same reason/cause does not establish identity
				nextSource = other
			}
			if mode == "same_done_wrapper" {
				nextSource = attributionInterfaceContext{Context: source, value: "another caller"}
			}
			next := consumer.businessSQLContext(nextSource, nextSource)
			if mode == "init_target" {
				next = consumer.sqlContext(nextSource, nextSource)
			}
			if mode == "different_target" {
				// Copying a local proof into a different target cannot grant it
				// the original authenticated physical connection's identity.
				next.target = &poolCancelTarget{owner: s, closedBy: target.closedBy, sql: next}
			}
			if mode == "unregistered" {
				s.mu.Lock()
				delete(s.operations, consumer)
				s.mu.Unlock()
			}
			if mode == "force_after" {
				s.mu.Lock()
				s.force = &poolForcePhase{total: newPoolCancelBudget(time.Now().Add(time.Second)), request: newPoolCancelBudget(time.Now().Add(time.Second))}
				s.mu.Unlock()
				defer s.force.total.stop(context.Canceled)
				defer s.force.request.stop(context.Canceled)
			}
			if mode == "internal_next" {
				next.cancelInternal(func() {})
			}
			if mode == "finished_next" {
				next.finish()
			}
			if got := next.sqlError(raw); got != raw || errors.Is(got, context.Canceled) || errors.Is(got, context.DeadlineExceeded) {
				t.Fatal("terminal close proof migrated to an unrelated SQL/cancellation source")
			}
			next.target = target
			next.finish()
		})
	}
}
