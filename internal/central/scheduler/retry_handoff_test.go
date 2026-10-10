package scheduler

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	"github.com/jackc/pgx/v5/pgconn"
)

// These are controlled Store/Execution boundary tests, not evidence that a
// PostgreSQL transaction issued Execution's private temporary-rejection proof.
// The real lock-timeout/checkpoint path belongs to the joint PG fixture.
func TestSchedulerRetryDeadlineAndUnprovenRejection(t *testing.T) {
	observed, err := f.ParseInstant("2026-10-10T12:00:00.000001Z")
	if err != nil {
		t.Fatal(err)
	}
	for _, input := range []struct {
		delay time.Duration
		want  time.Duration
	}{{time.Nanosecond, time.Microsecond}, {999 * time.Nanosecond, time.Microsecond}, {time.Microsecond, time.Microsecond}, {1001 * time.Nanosecond, 2 * time.Microsecond}, {time.Second, time.Second}} {
		got, err := retryDeadline(observed, input.delay)
		if err != nil || !got.Time().Equal(observed.Time().Add(input.want)) || got.Time().Before(observed.Time().Add(input.delay)) {
			t.Fatal("retry deadline was rounded early or changed an exact microsecond", err)
		}
	}
	last, err := f.ParseInstant("9999-12-31T23:59:59.999999Z")
	if err != nil {
		t.Fatal(err)
	}
	for _, input := range []struct {
		at    f.Instant
		delay time.Duration
	}{{f.Instant{}, time.Second}, {observed, 0}, {observed, -time.Nanosecond}, {last, time.Nanosecond}} {
		if deadline, err := retryDeadline(input.at, input.delay); err == nil || deadline != (f.Instant{}) {
			t.Fatal("invalid or overflowing retry deadline returned a partial value")
		}
	}
	for _, kind := range []string{"plain-fault", "hint", "driver-code", "wrapped-driver", "unknown", "cancelled", "deadline", "agent-busy"} {
		t.Run(kind, func(t *testing.T) {
			h, store, execution := newHandoffTest(t)
			store.row.retryPolicy = retryBindingPolicy(t, 3)
			returned := false
			execution.onLaunch = func(context.Context) error {
				returned = true
				raw := &pgconn.PgError{Code: "55P03", Message: "private-retry-proof-canary"}
				plain := f.NewFault(f.InternalError, f.NotCommitted)
				switch kind {
				case "hint":
					plain.RetryHint = "retry"
				case "driver-code":
					return raw
				case "wrapped-driver":
					return plain.WithCause(raw)
				case "unknown":
					return f.NewFault(f.CommitUnknown, f.Unknown).WithCause(raw)
				case "cancelled":
					return errors.Join(plain, context.Canceled)
				case "deadline":
					return errors.Join(plain, context.DeadlineExceeded)
				case "agent-busy":
					return f.NewFault(f.AgentBusy, f.NotStarted)
				}
				return plain
			}
			_, err := h.LaunchOnce(context.Background(), store.row.project, store.row.id)
			if err == nil || !returned || execution.launches != 1 || execution.lookups != 0 || store.row.attempts != 1 || store.row.nextRetry != nil {
				t.Fatal("unproven rejection scheduled another attempt or lost the original return", err)
			}
			if _, present := snapshot(store.row).RetryState(); present || pendingFinalFailure(store.row) {
				t.Fatal("a code, hint, cancellation or AgentBusy minted a temporary/final receipt")
			}
			if strings.Contains(err.Error(), "private-retry-proof-canary") {
				t.Fatal("original driver material escaped safe wrapping")
			}
		})
	}
}

type retryHandoffStore struct {
	*handoffTestStore
	writes    int
	rejectCAS bool
}

func (s *retryHandoffStore) InTx(tx f.Tx) (postgres.SQLExecutor, error) {
	if _, err := s.handoffTestStore.InTx(tx); err != nil {
		return nil, err
	}
	return s, nil
}
func (s *retryHandoffStore) Exec(ctx context.Context, query string, args ...any) (pgconn.CommandTag, error) {
	s.writes++
	if s.rejectCAS {
		return pgconn.NewCommandTag("UPDATE 0"), nil
	}
	return s.handoffTestStore.Exec(ctx, query, args...)
}

type retryHandoffProject struct {
	store   *retryHandoffStore
	enabled bool
	sprint  *pc.SprintID
	reads   int
}

func (p *retryHandoffProject) RequireSchedulerProjectInTx(ctx context.Context, tx f.Tx, project i.ProjectID) (pc.SchedulerProject, error) {
	if _, err := p.store.InTx(tx); err != nil {
		return pc.SchedulerProject{}, err
	}
	if err := p.store.RequireHeldLocks(ctx, tx, pendingLocks(project)); err != nil {
		return pc.SchedulerProject{}, err
	}
	p.reads++
	return pc.SchedulerProject{Project: pc.ProjectRef{ID: project, CurrentSprintID: p.sprint}, Config: pc.ProjectSchedulerConfig{Enabled: p.enabled}}, ctx.Err()
}

// Seed only the controlled Store's persisted input, explicitly bypassing no
// production proof API. This tests recovery of a stored receipt, not its minting.
func newRetryHandoffControl(t *testing.T) (*LaunchHandoff, *retryHandoffStore, *retryHandoffProject, *handoffTestExecution) {
	t.Helper()
	_, original, execution := newHandoffTest(t)
	store := &retryHandoffStore{handoffTestStore: original}
	authority, err := NewPendingAuthority(store)
	if err != nil {
		t.Fatal(err)
	}
	execution.authority = authority
	r := store.row
	r.retryPolicy, err = NewLaunchRetryPolicy(3, time.Second, 3*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	r.outcome, r.attempts, r.version = KnownNotCreated, 1, 3
	observed, err := f.NewInstant(time.Now().UTC().Truncate(time.Microsecond).Add(-time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	r.createdAt, err = f.NewInstant(observed.Time().Add(-time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	r.updatedAt = observed
	r.temporaryAttempt, r.temporaryReason, r.temporaryCode = 1, ec.LaunchTemporaryLockTimeout, f.InternalError
	r.temporaryOccurredAt = &observed
	deadline, err := retryDeadline(observed, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	r.nextRetry = &deadline
	sprint, err := f.ParseID[pc.Sprint](r.sprint)
	if err != nil {
		t.Fatal(err)
	}
	project := &retryHandoffProject{store: store, enabled: true, sprint: &sprint}
	h, err := NewLaunchHandoffWithRetry(authority, LaunchHandoffDependencies{Executions: execution, Observations: execution}, project)
	if err != nil {
		t.Fatal(err)
	}
	return h, store, project, execution
}

func TestSchedulerRetryDueRequiresCurrentEligibilityAndCommit(t *testing.T) {
	for _, mode := range []string{"due", "not-due", "paused", "other-sprint", "no-project", "cas-miss", "rollback-before", "rollback-after", "unknown-before", "unknown-after"} {
		t.Run(mode, func(t *testing.T) {
			h, store, project, execution := newRetryHandoffControl(t)
			switch mode {
			case "not-due":
				observed, err := f.NewInstant(time.Now().UTC().Truncate(time.Microsecond).Add(time.Hour))
				if err != nil {
					t.Fatal(err)
				}
				delay, remaining, err := store.row.retryPolicy.NextDelay(store.row.attempts)
				if err != nil || !remaining {
					t.Fatal("not-due control has no remaining policy allowance", err)
				}
				deadline, err := retryDeadline(observed, delay)
				if err != nil {
					t.Fatal(err)
				}
				store.row.updatedAt, store.row.temporaryOccurredAt, store.row.nextRetry = observed, &observed, &deadline
				if row, err := scanDispatch(dispatchTestRow{values: recordValues(t, store.row)}); err != nil || row == nil {
					t.Fatal("not-due control is not a valid persisted receipt", err)
				}
			case "paused":
				project.enabled = false
			case "other-sprint":
				other := dispatchTestID[pc.Sprint](t, 999)
				project.sprint = &other
			case "no-project":
				var err error
				h, err = NewLaunchHandoff(h.authority, LaunchHandoffDependencies{Executions: execution, Observations: execution})
				if err != nil {
					t.Fatal(err)
				}
			case "cas-miss":
				store.rejectCAS = true
			case "rollback-before", "rollback-after", "unknown-before", "unknown-after":
				store.failName, store.failMode = "retry_launch_handoff", mode
			}
			before := snapshot(store.row)
			original := store.row.launch.Clone()
			execution.onLaunch = func(context.Context) error {
				state, ok := snapshot(store.row).RetryState()
				if store.row.outcome != Unknown || store.row.attempts != 2 || store.row.nextRetry != nil || !ok || state.Attempt != 1 || state.NextRetryAt != nil || !reflect.DeepEqual(execution.lastRequest, original) {
					return errors.New("retry changed original input, skipped commit, or reused old temporary receipt as current")
				}
				return nil
			}
			out, err := h.RetryDue(context.Background(), store.row.project, store.row.id)
			if mode == "due" {
				if err != nil || out.Summary().Status != Launched || out.Summary().AttemptCount != 2 || execution.launches != 1 || execution.lookups != 0 || project.reads == 0 || store.writes != 2 || !reflect.DeepEqual(execution.lastRequest, original) {
					t.Fatal("eligible committed retry did not perform one original-key handoff and association", err)
				}
				if _, err = h.RetryDue(context.Background(), store.row.project, store.row.id); execution.launches != 1 || store.row.attempts != 2 {
					t.Fatal("completed retry was sent a second time", err)
				}
				return
			}
			if err == nil || execution.launches != 0 || execution.lookups != 0 {
				t.Fatal("ineligible or uncommitted marker dispatched", err)
			}
			if mode == "unknown-after" {
				if store.row.outcome != Unknown || store.row.attempts != 2 || store.row.nextRetry != nil || store.row.temporaryAttempt != 1 {
					t.Fatal("physical Unknown lost persisted marker or diagnostic receipt")
				}
			} else if !reflect.DeepEqual(snapshot(store.row).data(), before.data()) {
				t.Fatal("rejected gate or rollback changed the original Dispatch")
			}
			if mode == "not-due" || mode == "paused" || mode == "other-sprint" {
				var issue *f.Fault
				if !errors.As(err, &issue) || issue.Code != f.InvalidState || out.data == nil || store.writes != 0 {
					t.Fatal("deferred retry lost receipt or performed a write", err)
				}
			}
			if mode == "no-project" {
				var issue *f.Fault
				if !errors.As(err, &issue) || issue.Code != f.DependencyUnbound || project.reads != 0 || store.writes != 0 {
					t.Fatal("legacy constructor acquired retry authority")
				}
			}
			if strings.HasPrefix(mode, "unknown") {
				physical, ok := UnknownAttempt(err)
				if !ok || physical.AttemptID() != store.attempt || out.data != nil {
					t.Fatal("marker Unknown lost original physical attempt")
				}
				if _, again := h.RetryDue(context.Background(), store.row.project, store.row.id); again != err || execution.launches != 0 {
					t.Fatal("retained Unknown acquired another send owner")
				}
			}
		})
	}
}

func TestSchedulerRetryUnknownAndStopKeepOriginalOwner(t *testing.T) {
	for _, mode := range []string{"launch-unknown", "association-unknown", "stop-held-launch"} {
		t.Run(mode, func(t *testing.T) {
			h, store, _, execution := newRetryHandoffControl(t)
			p, id := store.row.project, store.row.id
			var original error
			if mode == "stop-held-launch" {
				entered, cancelled, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
				finished, result := make(chan struct{}), make(chan error, 1)
				var releaseOnce sync.Once
				execution.onLaunch = func(ctx context.Context) error {
					close(entered)
					<-ctx.Done()
					close(cancelled)
					<-release
					return nil
				}
				t.Cleanup(func() {
					h.Stop()
					releaseOnce.Do(func() { close(release) })
					select {
					case <-finished:
					case <-time.After(2 * time.Second):
						t.Error("controlled original retry did not return during cleanup")
					}
				})
				go func() { _, err := h.RetryDue(context.Background(), p, id); result <- err; close(finished) }()
				select {
				case <-entered:
				case <-time.After(2 * time.Second):
					t.Fatal("retry did not enter the original synchronous Launch")
				}
				h.Stop()
				select {
				case <-cancelled:
				case <-time.After(2 * time.Second):
					t.Fatal("Stop did not cancel the original Launch context")
				}
				short, cancel := context.WithCancel(context.Background())
				cancel()
				if h.Joined() || !errors.Is(h.Drain(short), context.Canceled) {
					t.Fatal("cancellation replaced original physical return")
				}
				releaseOnce.Do(func() { close(release) })
				select {
				case original = <-result:
				case <-time.After(2 * time.Second):
					t.Fatal("released original Launch did not join")
				}
			} else {
				if mode == "launch-unknown" {
					execution.onLaunch = func(context.Context) error { return f.NewFault(f.CommitUnknown, f.Unknown) }
				} else {
					store.failName, store.failMode = "associate_launch", "unknown-before"
				}
				_, original = h.RetryDue(context.Background(), p, id)
			}
			if original == nil || execution.launches != 1 || store.row.outcome != Unknown || store.row.attempts != 2 || store.row.nextRetry != nil || store.row.temporaryAttempt != 1 {
				t.Fatal("uncertain retry reused previous rejection or lost its original attempt", original)
			}
			if mode != "stop-held-launch" {
				if _, again := h.RetryDue(context.Background(), p, id); again != original || execution.launches != 1 {
					t.Fatal("uncertain retry resent before original-key observation")
				}
			}
			h.Stop()
			if h.Joined() {
				t.Fatal("Stop discarded unresolved original ownership")
			}
			if mode == "launch-unknown" {
				if _, err := h.Lookup(context.Background(), p, id); err == nil || h.Joined() || execution.launches != 1 || execution.lookups != 1 {
					t.Fatal("not observed was treated as permission to resend/retire", err)
				}
				// Controlled canonical observation, not a second Launch or a
				// claim that a real Execution transaction committed in this test.
				execution.created = visitCreated(t, store.row)
			}
			out, err := h.Lookup(context.Background(), p, id)
			if err != nil || out.Summary().Status != Launched || execution.launches != 1 || !h.Joined() {
				t.Fatal("original-key observation did not retire this handoff after its physical return", err)
			}
		})
	}
}

func TestSchedulerRetryAttemptAndProjectionBoundaries(t *testing.T) {
	for _, mode := range []string{"legacy-null", "old-attempt", "unknown-at-limit", "at-limit", "over-limit", "cancelled"} {
		t.Run(mode, func(t *testing.T) {
			h, store, _, execution := newRetryHandoffControl(t)
			ctx := context.Background()
			switch mode {
			case "legacy-null":
				store.row.retryPolicy = LaunchRetryPolicy{}
				store.row.temporaryAttempt, store.row.temporaryReason, store.row.temporaryCode = 0, "", ""
				store.row.temporaryOccurredAt, store.row.nextRetry = nil, nil
			case "old-attempt":
				store.row.attempts, store.row.nextRetry = 2, nil
			case "unknown-at-limit":
				store.row.attempts, store.row.outcome, store.row.nextRetry = 3, Unknown, nil
				store.row.temporaryAttempt = 2
				if row, err := scanDispatch(dispatchTestRow{values: recordValues(t, store.row)}); err != nil || row == nil {
					t.Fatal("unknown-at-limit control is not a valid previous-attempt receipt", err)
				}
			case "at-limit", "over-limit":
				store.row.attempts, store.row.temporaryAttempt = 3, 3
				if mode == "over-limit" {
					store.row.attempts, store.row.temporaryAttempt = 4, 4
				}
				// An impossible due row cannot authorize a send even if the
				// stored count claims a current rejection. Real exhaustion is
				// produced by the PG test's final typed rejection checkpoint.
			case "cancelled":
				cancelled, cancel := context.WithCancel(ctx)
				cancel()
				ctx = cancelled
			}
			before := snapshot(store.row)
			_, err := h.RetryDue(ctx, store.row.project, store.row.id)
			if err == nil || execution.launches != 0 || execution.lookups != 0 || store.writes != 0 || !reflect.DeepEqual(snapshot(store.row).data(), before.data()) || pendingFinalFailure(store.row) {
				t.Fatal("legacy/stale/exhausted/unknown/cancelled state caused a send or terminal write", err)
			}
			if mode == "cancelled" && !errors.Is(err, context.Canceled) {
				t.Fatal("original cancellation was lost")
			}
		})
	}
	_, store, _, _ := newRetryHandoffControl(t)
	receipt := snapshot(store.row)
	state, ok := receipt.RetryState()
	if !ok || state.Attempt != 1 || state.Reason != ec.LaunchTemporaryLockTimeout || state.Code != f.InternalError || state.NextRetryAt == nil || state.OccurredAt != *store.row.temporaryOccurredAt {
		t.Fatal("typed diagnostic projection lost original fields")
	}
	want := *state.NextRetryAt
	*state.NextRetryAt = state.OccurredAt
	*store.row.nextRetry = state.OccurredAt
	*store.row.temporaryOccurredAt = want
	again, ok := receipt.RetryState()
	if !ok || again.NextRetryAt == nil || *again.NextRetryAt != want || again.OccurredAt != state.OccurredAt {
		t.Fatal("RetryState retained mutable receipt/Store timestamps")
	}
}
