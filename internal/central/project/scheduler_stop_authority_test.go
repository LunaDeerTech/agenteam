package project

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

// This Store controls only original method returns. It cannot supply a full
// persisted stopping operation/manifest. Complete positive authorization is PG;
// even a controlled Committed result without that callback must grant nothing.
type schedulerStopTestStore struct {
	*retirementPrepStore
	transactions atomic.Int32
	within       func(context.Context, f.TransactionCause, func(context.Context, f.Tx) error) f.CommitResult
}

func (s *schedulerStopTestStore) WithinTx(ctx context.Context, cause f.TransactionCause, fn func(context.Context, f.Tx) error) f.CommitResult {
	s.transactions.Add(1)
	return s.within(ctx, cause, fn)
}
func (s *schedulerStopTestStore) AcquireAll(ctx context.Context, tx f.Tx, locks []f.LockRequest) error {
	s.record("acquire")
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if tx != s.tx || len(locks) != len(s.required) {
		return fault(f.Forbidden)
	}
	for n, l := range locks {
		if l.Key.Canonical() != s.required[n].Key.Canonical() || l.Mode != s.required[n].Mode {
			return fault(f.Forbidden)
		}
	}
	return nil
}
func schedulerStopTest(t *testing.T, step LifecycleLocalStopStep) (*LifecycleStopDriver, *SchedulerStopAuthority, *schedulerStopTestStore, *SchedulerProjects, c.AgentRetirementPreparationRequest) {
	t.Helper()
	r := retirementPrepRequest(t)
	p, err := f.ProjectLock(r.ProjectID.String())
	if err != nil {
		t.Fatal(err)
	}
	s := &schedulerStopTestStore{retirementPrepStore: &retirementPrepStore{tx: f.NewTx(), live: true, held: map[string]f.LockMode{p.Canonical(): f.Shared}, required: []f.LockRequest{{Key: p, Mode: f.Shared}}, readErr: errors.New("controlled lifecycle SQL")}}
	s.within = func(context.Context, f.TransactionCause, func(context.Context, f.Tx) error) f.CommitResult {
		return f.NotCommittedResult(fault(f.DependencyUnavailable))
	}
	pa, err := NewAuthority(s, AuthorityDependencies{Sessions: sessionFunc(func(context.Context, f.Tx, i.Actor) error {
		t.Error("internal stop requested Human authority")
		return fault(f.Forbidden)
	})})
	if err != nil {
		t.Fatal(err)
	}
	directory, err := NewSchedulerProjects(pa)
	if err != nil {
		t.Fatal(err)
	}
	if step == nil {
		step = func(context.Context, i.Actor, c.LifecycleCause, c.ScopeRef) error { return nil }
	}
	d := stopRoundTestDriver(t, s, step)
	a, err := NewSchedulerStopAuthority(d, directory, s)
	if err != nil {
		t.Fatal(err)
	}
	if s.transactions.Load() != 0 || len(s.seen()) != 0 {
		t.Fatal("construction performed I/O")
	}
	return d, a, s, directory, r
}

func TestSchedulerStopAuthorityRequiresOriginalOwnersAndIssuer(t *testing.T) {
	d, a, s, directory, r := schedulerStopTest(t, nil)
	_, _, foreign, foreignDirectory, _ := schedulerStopTest(t, nil)
	for _, tc := range []struct {
		name      string
		driver    *LifecycleStopDriver
		directory *SchedulerProjects
		store     Store
	}{
		{"missing-driver", nil, directory, s},
		{"unbound-driver", &LifecycleStopDriver{}, directory, s},
		{"missing-directory", d, nil, s},
		{"unbound-directory", d, &SchedulerProjects{}, s},
		{"foreign-directory", d, foreignDirectory, s},
		{"foreign-manager-store", d, directory, foreign},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := NewSchedulerStopAuthority(tc.driver, tc.directory, tc.store); err == nil {
				t.Fatal("mismatched concrete owner accepted")
			}
		})
	}
	called := false
	err := a.WithConfirmedSchedulerStop(context.Background(), r.Actor, r.Cause, c.ScopeRef{Kind: c.ProjectScope, ProjectID: r.ProjectID}, func(context.Context) error { called = true; return nil })
	if err == nil || called || s.transactions.Load() != 0 || len(s.seen()) != 0 {
		t.Fatal("public DTO manufactured an active driver scope", err)
	}
}

func TestSchedulerStopAuthorityUnconfirmedTransactionCannotStop(t *testing.T) {
	for _, name := range []string{"not-committed", "unknown", "committed-without-callback", "callback-rejected-but-committed", "expired-tx", "missing-project-lock", "stale-claim"} {
		t.Run(name, func(t *testing.T) {
			d, a, s, _, r := schedulerStopTest(t, nil)
			ctx, scope := retirementPrepScope(t, d, r)
			if name == "expired-tx" {
				s.live = false
			}
			if name == "missing-project-lock" {
				clear(s.held)
			}
			if name == "stale-claim" {
				actual := scope.claim
				actual.fence++
				s.claim = &actual
			}
			var callbackErr error
			s.within = func(ctx context.Context, cause f.TransactionCause, fn func(context.Context, f.Tx) error) f.CommitResult {
				switch name {
				case "not-committed":
					return f.NotCommittedResult(fault(f.InvalidState))
				case "unknown":
					return f.UnknownResult(testID[f.TransactionAttempt](t), cause)
				case "callback-rejected-but-committed", "expired-tx", "missing-project-lock", "stale-claim":
					callbackErr = fn(ctx, s.tx)
				}
				return f.CommittedResult()
			}
			called := false
			err := a.WithConfirmedSchedulerStop(ctx, r.Actor, r.Cause, c.ScopeRef{Kind: c.ProjectScope, ProjectID: r.ProjectID}, func(context.Context) error { called = true; return nil })
			if err == nil || called || s.transactions.Load() != 1 {
				t.Fatal("unconfirmed authorization reached manager", err)
			}
			if name == "unknown" {
				hasCode(t, err, f.CommitUnknown)
			}
			if name == "callback-rejected-but-committed" && !errors.Is(callbackErr, s.readErr) {
				t.Fatal("test failed before its original SQL boundary", callbackErr)
			}
			if (name == "expired-tx" || name == "missing-project-lock" || name == "stale-claim") && callbackErr == nil {
				t.Fatal("transaction/lock/claim rejection was bypassed")
			}
		})
	}
}

func TestSchedulerStopAuthorityLoanContextsCannotReissue(t *testing.T) {
	for _, name := range []string{"active-agent-loan", "released-agent-loan", "active-control-loan", "released-control-loan", "foreign-driver", "archive"} {
		t.Run(name, func(t *testing.T) {
			d, authority, store, _, request := schedulerStopTest(t, nil)
			ctx, scope := retirementPrepScope(t, d, request)
			switch name {
			case "active-agent-loan", "released-agent-loan":
				loan, err := scope.borrow(ctx, store.tx, request)
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { scope.release(loan) })
				ctx = context.WithValue(ctx, agentPreparationLoanKey{}, loan)
				if name == "released-agent-loan" {
					scope.release(loan)
				}
			case "active-control-loan", "released-control-loan":
				loan, err := scope.borrowSchedulerStop(ctx)
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { scope.releaseSchedulerStop(loan) })
				ctx = context.WithValue(ctx, schedulerStopLoanKey{}, loan)
				if name == "released-control-loan" {
					scope.releaseSchedulerStop(loan)
				}
				if child, err := scope.borrow(ctx, store.tx, request); err == nil {
					scope.release(child)
					t.Fatal("control context reissued an Agent read")
				}
			case "foreign-driver":
				_, authority, _, _, _ = schedulerStopTest(t, nil)
			case "archive":
				request.Cause.Action = c.Archive
			}
			// Removing cancellation must not strip the private loan's closed or
			// nested provenance and revive an otherwise still-active step.
			err := authority.WithConfirmedSchedulerStop(context.WithoutCancel(ctx), request.Actor, request.Cause, c.ScopeRef{Kind: c.ProjectScope, ProjectID: request.ProjectID}, func(context.Context) error { t.Error("reissued control reached manager"); return nil })
			if err == nil || store.transactions.Load() != 0 {
				t.Fatal("invalid provenance reached a new transaction", err)
			}
		})
	}
}

func TestSchedulerStopAuthorityCloseWaitsForBothOriginalLoanKinds(t *testing.T) {
	for _, name := range []string{"control-returns-first", "agent-returns-first"} {
		t.Run(name, func(t *testing.T) {
			d, _, store, _, request := schedulerStopTest(t, nil)
			ctx, scope := retirementPrepScope(t, d, request)
			read, err := scope.borrow(ctx, store.tx, request)
			if err != nil {
				t.Fatal(err)
			}
			control, err := scope.borrowSchedulerStop(ctx)
			if err != nil {
				scope.release(read)
				t.Fatal(err)
			}
			closed := make(chan struct{})
			var readOnce, controlOnce sync.Once
			readDone := func() { readOnce.Do(func() { scope.release(read) }) }
			controlDone := func() { controlOnce.Do(func() { scope.releaseSchedulerStop(control) }) }
			go func() { defer close(closed); scope.close() }()
			t.Cleanup(func() { readDone(); controlDone(); retirementPrepAwait(t, closed) })
			retirementPrepAwait(t, scope.ctx.Done())
			if loan, err := scope.borrowSchedulerStop(context.WithoutCancel(ctx)); err == nil {
				scope.releaseSchedulerStop(loan)
				t.Fatal("closed scope reissued a control")
			}
			if name == "control-returns-first" {
				controlDone()
			} else {
				readDone()
			}
			select {
			case <-closed:
				t.Fatal("one returned loan hid the other original owner")
			default:
			}
			// Releasing either kind returns synchronously, independently of the
			// other kind. Only the invoke-owned close waits for the aggregate.
			readDone()
			controlDone()
			retirementPrepAwait(t, closed)
		})
	}
}

func TestSchedulerStopAuthorityInvokeWaitsForOriginalControlReturn(t *testing.T) {
	for _, name := range []string{"step-return", "caller-cancel"} {
		t.Run(name, func(t *testing.T) {
			entered, release, controlDone, invokeDone := make(chan struct{}), make(chan struct{}), make(chan struct{}), make(chan struct{})
			stepRelease := make(chan struct{})
			var releaseOnce, stepOnce sync.Once
			free := func() { releaseOnce.Do(func() { close(release) }) }
			freeStep := func() { stepOnce.Do(func() { close(stepRelease) }) }
			var authority *SchedulerStopAuthority
			var originalScope *agentPreparationScope
			var controlErr, invokeErr error
			var called atomic.Int32
			stepErr := errors.New("original stop step")
			d, actual, store, _, request := schedulerStopTest(t, func(ctx context.Context, actor i.Actor, cause c.LifecycleCause, scope c.ScopeRef) error {
				originalScope = ctx.Value(agentPreparationScopeKey{}).(*agentPreparationScope)
				go func() {
					defer close(controlDone)
					controlErr = authority.WithConfirmedSchedulerStop(ctx, actor, cause, scope, func(context.Context) error { called.Add(1); return nil })
				}()
				<-stepRelease
				return stepErr
			})
			authority = actual
			store.within = func(ctx context.Context, _ f.TransactionCause, _ func(context.Context, f.Tx) error) f.CommitResult {
				if !originalScope.mu.TryLock() {
					t.Error("scope mutex held across original Store")
				} else {
					originalScope.mu.Unlock()
				}
				close(entered)
				<-release
				// Deliberately late and unvalidated: this is a return/lifetime
				// control, not evidence of a successful authorization SQL commit.
				return f.CommittedResult()
			}
			ctx, cancel := context.WithCancel(context.Background())
			claim := retirementPrepClaim(t, d, request)
			go func() {
				defer close(invokeDone)
				invokeErr = d.state.invoke(agentPreparationClaimContext(ctx, d.state, claim, request.Cause), request.ProjectID, request.Cause.OperationID, request.Cause)
			}()
			t.Cleanup(func() {
				cancel()
				freeStep()
				free()
				retirementPrepAwait(t, controlDone)
				retirementPrepAwait(t, invokeDone)
			})
			retirementPrepAwait(t, entered)
			if name == "caller-cancel" {
				cancel()
			}
			freeStep()
			retirementPrepAwait(t, originalScope.ctx.Done())
			select {
			case <-controlDone:
				t.Fatal("control returned before original Store")
			default:
			}
			select {
			case <-invokeDone:
				t.Fatal("invoke lost its original control loan")
			default:
			}
			if called.Load() != 0 {
				t.Fatal("manager called before original confirmation")
			}
			free()
			retirementPrepAwait(t, controlDone)
			retirementPrepAwait(t, invokeDone)
			if controlErr == nil || !errors.Is(invokeErr, stepErr) || called.Load() != 0 {
				t.Fatal("late result replaced original step or authorized a stop", controlErr, invokeErr)
			}
			originalScope.mu.Lock()
			remaining := len(originalScope.schedulerStops)
			originalScope.mu.Unlock()
			if remaining != 0 || store.transactions.Load() != 1 {
				t.Fatal("original control not retired exactly once")
			}
		})
	}
}
