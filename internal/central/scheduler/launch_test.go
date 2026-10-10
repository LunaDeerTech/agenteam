package scheduler

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/jackc/pgx/v5/pgconn"
)

// This Store is an explicit physical-outcome control for the handoff. It
// exercises the production scanner/UPDATE and callback ordering, not real PG,
// Work authorization, slot insertion or a successful Execution service.
type handoffTestStore struct {
	pendingTestStore
	t                  *testing.T
	row, staged        *dispatchRecord
	locks              []f.LockRequest
	failName, failMode string
	attempt            f.ID[f.TransactionAttempt]
}

func (s *handoffTestStore) InTx(tx f.Tx) (postgres.SQLExecutor, error) {
	if tx != s.tx || !tx.Valid() {
		return nil, fault(f.InvalidArgument)
	}
	return s, nil
}
func (s *handoffTestStore) WithinTx(ctx context.Context, cause f.TransactionCause, fn func(context.Context, f.Tx) error) f.CommitResult {
	name := cause.Details().Primary.Command()
	mode := ""
	if name == s.failName {
		mode, s.failName = s.failMode, ""
	}
	if mode == "unknown-before" {
		return f.UnknownResult(s.attempt, cause)
	}
	if mode == "rollback-before" {
		return f.NotCommittedResult(fault(f.DependencyUnavailable))
	}
	if err := ctx.Err(); err != nil {
		return f.NotCommittedResult(f.NewFault(f.InternalError, f.NotCommitted).WithCause(err))
	}
	s.tx, s.locks, s.staged = f.NewTx(), nil, s.row
	defer func() { s.tx, s.staged, s.locks = f.Tx{}, nil, nil }()
	if err := fn(ctx, s.tx); err != nil {
		var known *f.Fault
		if errors.As(err, &known) {
			return f.NotCommittedResult(known)
		}
		return f.NotCommittedResult(f.NewFault(f.InternalError, f.NotCommitted).WithCause(err))
	}
	if mode == "rollback-after" {
		return f.NotCommittedResult(fault(f.DependencyUnavailable))
	}
	s.row = s.staged
	if mode == "unknown-after" {
		return f.UnknownResult(s.attempt, cause)
	}
	return f.CommittedResult()
}
func (s *handoffTestStore) AcquireAll(ctx context.Context, tx f.Tx, locks []f.LockRequest) error {
	if _, err := s.InTx(tx); err != nil {
		return err
	}
	s.locks = append([]f.LockRequest(nil), locks...)
	return ctx.Err()
}
func (s *handoffTestStore) RequireHeldLocks(ctx context.Context, tx f.Tx, required []f.LockRequest) error {
	if _, err := s.InTx(tx); err != nil {
		return err
	}
	for _, want := range required {
		found := false
		for _, held := range s.locks {
			if held.Key.Canonical() == want.Key.Canonical() && (held.Mode == f.Exclusive || held.Mode == want.Mode) {
				found = true
			}
		}
		if !found {
			return fault(f.Forbidden)
		}
	}
	return ctx.Err()
}
func (s *handoffTestStore) QueryRow(context.Context, string, ...any) postgres.Row {
	return dispatchTestRow{values: recordValues(s.t, s.staged)}
}
func (s *handoffTestStore) Exec(ctx context.Context, query string, args ...any) (pgconn.CommandTag, error) {
	if !strings.HasPrefix(query, "UPDATE agenteam_scheduler.dispatches SET") || len(args) != 13 || s.staged.version != f.Version(args[9].(int64)) {
		return pgconn.CommandTag{}, errors.New("unexpected controlled update")
	}
	r := *s.staged
	r.status, r.outcome = Status(args[2].(string)), LaunchOutcome(args[3].(string))
	r.attempts, r.version = args[5].(int64), f.Version(args[7].(int64))
	r.updatedAt, _ = f.NewInstant(args[8].(time.Time))
	if args[10] != nil {
		r.busyAttempt = args[10].(int64)
	}
	if args[11] != nil {
		r.skipReason = args[11].(string)
	}
	if args[12] != nil {
		v, _ := f.NewInstant(args[12].(time.Time))
		r.skippedAt = &v
	}
	if args[4] != nil {
		v, err := f.ParseID[i.Execution](args[4].(string))
		if err != nil {
			return pgconn.CommandTag{}, err
		}
		r.execution = &v
	}
	s.staged = &r
	return pgconn.NewCommandTag("UPDATE 1"), ctx.Err()
}

type handoffTestExecution struct {
	store             *handoffTestStore
	authority         *PendingAuthority
	launches, lookups int
	created           *ec.Summary
	onLaunch          func(context.Context) error
	onLookup          func(context.Context) error
	lastContext       context.Context
	lastActor         i.Actor
	lastRequest       ec.LaunchRequest
}

func (e *handoffTestExecution) proof(ctx context.Context, actor i.Actor, send bool) error {
	r := e.store.row
	command, _ := r.launch.Command()
	cause, _ := f.NewCommandsCause(command)
	return commitError(e.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		locks := executionIntentLocks(r)
		task, _ := f.AggregateLock(f.TaskAggregate, r.task)
		locks = append(locks, f.LockRequest{Key: task, Mode: f.Shared})
		if err := e.store.AcquireAll(ctx, tx, locks); err != nil {
			return err
		}
		if _, err := e.authority.RequireSchedulerIntentInTx(ctx, tx, actor, r.project, r.agent, i.Read); err != nil {
			return err
		}
		if send {
			_, err := e.authority.RequireTaskLaunchInTx(ctx, tx, actor, r.launch.Clone())
			return err
		}
		if _, err := e.authority.RequireTaskLaunchInTx(ctx, tx, actor, r.launch.Clone()); err == nil {
			return errors.New("recovery minted send permission")
		}
		return nil
	}))
}
func (e *handoffTestExecution) Launch(ctx context.Context, actor i.Actor, request ec.LaunchRequest) (ec.LaunchResult, error) {
	e.launches++
	e.lastContext, e.lastActor, e.lastRequest = ctx, actor, request.Clone()
	if e.store.row.outcome != Unknown || e.store.row.attempts != 1 {
		return ec.LaunchResult{}, errors.New("send preceded committed marker")
	}
	if err := e.proof(ctx, actor, true); err != nil {
		return ec.LaunchResult{}, err
	}
	if e.onLaunch != nil {
		if err := e.onLaunch(ctx); err != nil {
			return ec.LaunchResult{}, err
		}
	}
	r := e.store.row
	e.created = &ec.Summary{ID: dispatchTestID[i.Execution](e.store.t, 900), ProjectID: r.project, AgentID: r.agent, Trigger: r.launch.Trigger, Purpose: r.launch.Purpose, Status: ec.Created, Version: 1, CreatedAt: r.createdAt}
	return ec.LaunchResult{Execution: e.created.Clone()}, nil
}
func (e *handoffTestExecution) LookupLaunch(ctx context.Context, actor i.Actor, key ec.LaunchLookupKey, digest f.Digest) (ec.LaunchLookup, error) {
	e.lookups++
	r := e.store.row
	if key != lookupKey(r) || digest != r.digest {
		return ec.LaunchLookup{}, errors.New("changed original key or digest")
	}
	if err := e.proof(ctx, actor, false); err != nil {
		return ec.LaunchLookup{}, err
	}
	if e.onLookup != nil {
		if err := e.onLookup(ctx); err != nil {
			return ec.LaunchLookup{}, err
		}
	}
	return e.lookup(r), nil
}
func (e *handoffTestExecution) lookup(r *dispatchRecord) ec.LaunchLookup {
	if e.created == nil {
		return ec.LaunchLookup{}
	}
	value := e.created.Clone()
	return ec.LaunchLookup{Found: true, RequestDigest: r.digest, Execution: &value}
}
func (e *handoffTestExecution) LookupLaunchInTx(ctx context.Context, tx f.Tx, key ec.LaunchLookupKey, digest f.Digest, dispatch string) (ec.LaunchLookup, error) {
	r := e.store.staged
	if err := e.store.RequireHeldLocks(ctx, tx, executionIntentLocks(r)); err != nil {
		return ec.LaunchLookup{}, err
	}
	if key != lookupKey(r) || digest != r.digest || dispatch != r.id.String() {
		return ec.LaunchLookup{}, errors.New("association lost original tuple")
	}
	return e.lookup(r), nil
}
func (*handoffTestExecution) AgentSlotInTx(context.Context, f.Tx, i.ProjectID, i.AgentID) (ec.AgentSlotObservation, error) {
	panic("handoff does not approximate Execution's current slot")
}
func newHandoffTest(t *testing.T) (*LaunchHandoff, *handoffTestStore, *handoffTestExecution) {
	t.Helper()
	r := dispatchTestRecord(t, 1, Pending)
	r.outcome, r.attempts = NotSent, 0
	r.guard = &ClaimGuard{TaskID: r.task, ClaimedVersion: 2, SourceState: "todo", SourceAssigneeID: r.agent, SourcePriority: "high", SourceSprintID: r.sprint, SourceOrderGeneration: 2}
	store := &handoffTestStore{t: t, row: r, attempt: dispatchTestID[f.TransactionAttempt](t, 800)}
	a, _ := NewPendingAuthority(store)
	execution := &handoffTestExecution{store: store, authority: a}
	s, err := NewLaunchHandoff(a, LaunchHandoffDependencies{Executions: execution, Observations: execution})
	if err != nil {
		t.Fatal(err)
	}
	return s, store, execution
}

func TestSchedulerLaunchMarkerMustCommitBeforeHandoff(t *testing.T) {
	for _, mode := range []string{"rollback-before", "unknown-before", "unknown-after"} {
		t.Run(mode, func(t *testing.T) {
			s, store, e := newHandoffTest(t)
			store.failName, store.failMode = "launch_handoff", mode
			out, err := s.LaunchOnce(context.Background(), store.row.project, store.row.id)
			if err == nil || out.data != nil || e.launches != 0 {
				t.Fatal("uncertain/rejected marker dispatched", err)
			}
			if strings.HasPrefix(mode, "unknown") {
				original, ok := UnknownAttempt(err)
				if !ok || original.AttemptID() != store.attempt {
					t.Fatal("original marker attempt lost")
				}
				_, observed := s.Lookup(context.Background(), store.row.project, store.row.id)
				if observed != err || e.launches != 0 {
					t.Fatal("not observed caused resend or cleared original error")
				}
				s.Stop()
				if s.Joined() {
					t.Fatal("unknown marker retired without observation")
				}
			}
		})
	}
}

func TestSchedulerLaunchAssociationAndOriginalKeyRecovery(t *testing.T) {
	for _, mode := range []string{"normal", "rollback-after", "unknown-after"} {
		t.Run(mode, func(t *testing.T) {
			s, store, e := newHandoffTest(t)
			if mode != "normal" {
				store.failName, store.failMode = "associate_launch", mode
			}
			out, err := s.LaunchOnce(context.Background(), store.row.project, store.row.id)
			if e.launches != 1 || e.created == nil {
				t.Fatal("missing controlled original call")
			}
			if mode == "normal" {
				if err != nil || out.Summary().Status != Launched {
					t.Fatal("association", err)
				}
			} else {
				if err == nil || out.data != nil {
					t.Fatal("uncertain association published result")
				}
				out, err = s.Lookup(context.Background(), store.row.project, store.row.id)
				if err != nil || out.Summary().Status != Launched || *out.Summary().ExecutionID != e.created.ID {
					t.Fatal("original-key recovery", err)
				}
			}
			if _, err = s.LaunchOnce(context.Background(), store.row.project, store.row.id); err != nil || e.launches != 1 || store.row.attempts != 1 {
				t.Fatal("replayed dispatch sent again", err)
			}
			s.Stop()
			if !s.Joined() {
				t.Fatal("known result retained completed call")
			}
		})
	}
}

func TestSchedulerLaunchKnownRejectionAndUncertainTransport(t *testing.T) {
	for _, mode := range []string{"busy", "forbidden", "opaque-error", "context-cancel"} {
		t.Run(mode, func(t *testing.T) {
			s, store, e := newHandoffTest(t)
			e.onLaunch = func(context.Context) error {
				switch mode {
				case "busy":
					return f.NewFault(f.AgentBusy, f.NotCommitted)
				case "forbidden":
					return f.NewFault(f.Forbidden, f.NotCommitted)
				case "context-cancel":
					return context.Canceled
				default:
					return errors.New("private transport canary")
				}
			}
			out, err := s.LaunchOnce(context.Background(), store.row.project, store.row.id)
			if err == nil || strings.Contains(err.Error(), "canary") || e.launches != 1 || store.row.status != Pending {
				t.Fatal("rejection classification", err)
			}
			if mode == "busy" {
				if store.row.outcome != KnownNotCreated || out.Summary().LaunchOutcome != KnownNotCreated || store.row.busyAttempt != store.row.attempts {
					t.Fatal("known busy was falsified as unknown/skipped")
				}
			} else if mode == "forbidden" {
				if store.row.outcome != KnownNotCreated || out.Summary().LaunchOutcome != KnownNotCreated || store.row.busyAttempt != 0 {
					t.Fatal("non-Busy rejection manufactured a compensation receipt")
				}
			} else if store.row.outcome != Unknown || out.data != nil || store.row.busyAttempt != 0 {
				t.Fatal("opaque error used as negative creation proof")
			}
			_, _ = s.LaunchOnce(context.Background(), store.row.project, store.row.id)
			if e.launches != 1 {
				t.Fatal("unsupported retry resent")
			}
		})
	}
}

func TestSchedulerLaunchIntentRequiresLiveOriginalCall(t *testing.T) {
	s, store, e := newHandoffTest(t)
	r := store.row
	ctx, call, err := s.begin(context.Background(), r.project, r.id, false)
	if err != nil {
		t.Fatal(err)
	}
	r, sent, err := s.markSending(ctx, call)
	if err != nil || !sent {
		t.Fatal(err)
	}
	s.activate(call, r, true)
	if err = e.proof(ctx, call.actor, true); err != nil {
		t.Fatal("original proof", err)
	}
	for _, mode := range []string{"no-private-context", "foreign-store", "foreign-tx", "missing-held", "request-id", "policy", "retired"} {
		t.Run(mode, func(t *testing.T) {
			checkCtx := ctx
			authority := s.authority
			request := r.launch.Clone()
			command, _ := r.launch.Command()
			cause, _ := f.NewCommandsCause(command)
			result := store.WithinTx(ctx, cause, func(inner context.Context, tx f.Tx) error {
				locks := executionIntentLocks(r)
				task, _ := f.AggregateLock(f.TaskAggregate, r.task)
				locks = append(locks, f.LockRequest{Key: task, Mode: f.Shared})
				if err := store.AcquireAll(inner, tx, locks); err != nil {
					return err
				}
				switch mode {
				case "no-private-context":
					checkCtx = context.Background()
				case "foreign-store":
					authority, _ = NewPendingAuthority(&pendingTestStore{})
				case "foreign-tx":
					tx = f.NewTx()
				case "missing-held":
					store.locks = nil
				case "request-id":
					request.Meta.RequestID = dispatchTestID[f.Request](t, 999)
				case "policy":
					request.Policy.DeniedToolIDs = []i.ToolID{dispatchTestID[i.Tool](t, 999)}
				case "retired":
					s.deactivate(call)
				}
				if _, err := authority.RequireTaskLaunchInTx(checkCtx, tx, call.actor, request); err == nil {
					return errors.New("changed or retired private proof accepted")
				}
				return nil
			})
			if result.State() != f.Committed {
				t.Fatal("invalid proof test failed", result.Fault())
			}
		})
	}
	s.finish(call, false, nil)
}

func TestSchedulerLaunchStopWaitsOriginalReturn(t *testing.T) {
	s, store, e := newHandoffTest(t)
	entered, cancelled, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
	e.onLaunch = func(ctx context.Context) error {
		close(entered)
		<-ctx.Done()
		close(cancelled)
		<-release
		return nil
	}
	done := make(chan error, 1)
	p, id := store.row.project, store.row.id
	go func() { _, err := s.LaunchOnce(context.Background(), p, id); done <- err }()
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("original call not entered")
	}
	s.Stop()
	<-cancelled
	if s.Joined() {
		t.Fatal("cancel replaced actual call return")
	}
	short, cancel := context.WithCancel(context.Background())
	cancel()
	if s.Drain(short) == nil {
		t.Fatal("Drain ignored held original call")
	}
	close(release)
	if err := <-done; err == nil || e.created == nil || s.Joined() {
		t.Fatal("unknown association incorrectly retired", err)
	}
	// The original call has actually returned, but its created result cannot
	// be checkpointed with the cancelled context. Read-only recovery remains
	// available after Stop and cannot reuse the old send permission.
	out, err := s.Lookup(context.Background(), p, id)
	if err != nil || out.Summary().Status != Launched || e.launches != 1 || e.lookups != 1 || !s.Joined() {
		t.Fatal("post-return recovery did not retire original ownership", err)
	}
}
