package project

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

// Reuse the original controlled Store, not a fabricated successful lifecycle
// manifest. It can expose the first SQL boundary and hold an actual method
// return; full committed authorization and the executor callback are real PG.
func executionStopTest(t *testing.T, step LifecycleLocalStopStep) (*LifecycleStopDriver, *ExecutionStopAuthority, *schedulerStopTestStore, c.AgentRetirementPreparationRequest) {
	t.Helper()
	d, _, store, _, request := schedulerStopTest(t, step)
	authority, err := NewExecutionStopAuthority(d, store)
	if err != nil {
		t.Fatal(err)
	}
	return d, authority, store, request
}

func TestExecutionStopAuthorityRequiresOriginalStoreAndIssuer(t *testing.T) {
	d, authority, store, request := executionStopTest(t, nil)
	_, _, foreign, _ := executionStopTest(t, nil)
	for _, entry := range []struct {
		name   string
		driver *LifecycleStopDriver
		store  Store
	}{
		{"missing-driver", nil, store},
		{"unbound-driver", &LifecycleStopDriver{}, store},
		{"missing-store", d, nil},
		{"typed-nil-store", d, (*schedulerStopTestStore)(nil)},
		{"foreign-store", d, foreign},
	} {
		t.Run(entry.name, func(t *testing.T) {
			if _, err := NewExecutionStopAuthority(entry.driver, entry.store); err == nil {
				t.Fatal("unbound or foreign executor Store accepted")
			}
		})
	}
	err := authority.WithConfirmedExecutionStop(context.Background(), request.Actor, request.Cause, c.ScopeRef{Kind: c.ProjectScope, ProjectID: request.ProjectID}, func(context.Context) error {
		t.Error("public identity manufactured an Execution stop issuer")
		return nil
	})
	hasCode(t, err, f.Forbidden)
	if store.transactions.Load() != 0 || len(store.seen()) != 0 {
		t.Fatal("unissued control performed I/O")
	}
	for _, name := range []string{"foreign-driver", "archive", "other-project", "other-version"} {
		t.Run(name, func(t *testing.T) {
			d, authority, store, request := executionStopTest(t, nil)
			ctx, _ := retirementPrepScope(t, d, request)
			target := c.ScopeRef{Kind: c.ProjectScope, ProjectID: request.ProjectID}
			switch name {
			case "foreign-driver":
				_, authority, _, _ = executionStopTest(t, nil)
			case "archive":
				request.Cause.Action = c.Archive
			case "other-project":
				target.ProjectID = testID[i.Project](t)
			case "other-version":
				request.Cause.ProjectVersion++
			}
			err := authority.WithConfirmedExecutionStop(ctx, request.Actor, request.Cause, target, func(context.Context) error {
				t.Error("mismatched original issuer reached executor")
				return nil
			})
			if err == nil || store.transactions.Load() != 0 {
				t.Fatal("invalid issuer began authorization", err)
			}
		})
	}
}

func TestExecutionStopAuthorityRequiresCurrentConfirmedTransaction(t *testing.T) {
	for _, name := range []string{"not-committed", "unknown", "committed-without-callback", "rejected-callback", "expired-tx", "missing-lock", "missing-claim", "other-process", "other-attempt", "other-fence", "terminal-claim"} {
		t.Run(name, func(t *testing.T) {
			d, authority, store, request := executionStopTest(t, nil)
			ctx, scope := retirementPrepScope(t, d, request)
			claim := scope.claim
			store.claim = &claim
			switch name {
			case "expired-tx":
				store.live = false
			case "missing-lock":
				clear(store.held)
			case "missing-claim":
				store.claimMissing = true
			case "other-process":
				claim.process = testID[oc.Process](t)
			case "other-attempt":
				claim.attempt = testID[struct{}](t).String()
			case "other-fence":
				claim.fence++
			case "terminal-claim":
				claim.phase = "terminal"
			}
			var callbackErr error
			store.within = func(ctx context.Context, cause f.TransactionCause, fn func(context.Context, f.Tx) error) f.CommitResult {
				switch name {
				case "not-committed":
					return f.NotCommittedResult(fault(f.DependencyUnavailable))
				case "unknown":
					return f.UnknownResult(testID[f.TransactionAttempt](t), cause)
				case "committed-without-callback":
				default:
					callbackErr = fn(ctx, store.tx)
				}
				// Deliberately untrusted acknowledgement: without a successful
				// canonical callback even Committed must not call the executor.
				return f.CommittedResult()
			}
			called := false
			err := authority.WithConfirmedExecutionStop(ctx, request.Actor, request.Cause, c.ScopeRef{Kind: c.ProjectScope, ProjectID: request.ProjectID}, func(context.Context) error {
				called = true
				return nil
			})
			if err == nil || called || store.transactions.Load() != 1 {
				t.Fatal("unconfirmed or stale authorization stopped work", err)
			}
			if name == "unknown" {
				hasCode(t, err, f.CommitUnknown)
			}
			if name == "rejected-callback" && !errors.Is(callbackErr, store.readErr) {
				t.Fatal("controlled callback missed its original canonical SQL boundary", callbackErr)
			}
			if name != "not-committed" && name != "unknown" && name != "committed-without-callback" && callbackErr == nil {
				t.Fatal("invalid transaction or current claim accepted")
			}
		})
	}
}

func TestExecutionStopAuthorityLoanFamiliesCannotReissue(t *testing.T) {
	for _, family := range []string{"agent", "scheduler", "execution"} {
		for _, lifetime := range []string{"active", "returned"} {
			t.Run(family+"-"+lifetime, func(t *testing.T) {
				d, authority, store, request := executionStopTest(t, nil)
				ctx, scope := retirementPrepScope(t, d, request)
				var release func()
				switch family {
				case "agent":
					loan, err := scope.borrow(ctx, store.tx, request)
					if err != nil {
						t.Fatal(err)
					}
					ctx = context.WithValue(ctx, agentPreparationLoanKey{}, loan)
					release = func() { scope.release(loan) }
				case "scheduler":
					loan, err := scope.borrowSchedulerStop(ctx)
					if err != nil {
						t.Fatal(err)
					}
					ctx = context.WithValue(ctx, schedulerStopLoanKey{}, loan)
					release = func() { scope.releaseSchedulerStop(loan) }
				case "execution":
					loan, err := scope.borrowExecutionStop(ctx)
					if err != nil {
						t.Fatal(err)
					}
					ctx = context.WithValue(ctx, executionStopLoanKey{}, loan)
					release = func() { scope.releaseExecutionStop(loan) }
				}
				var once sync.Once
				done := func() { once.Do(release) }
				t.Cleanup(done)
				if lifetime == "returned" {
					done()
				}
				ctx = context.WithoutCancel(ctx)
				err := authority.WithConfirmedExecutionStop(ctx, request.Actor, request.Cause, c.ScopeRef{Kind: c.ProjectScope, ProjectID: request.ProjectID}, func(context.Context) error {
					t.Error("loan context reissued an independent stop")
					return nil
				})
				hasCode(t, err, f.Forbidden)
				if family == "execution" {
					if loan, err := scope.borrowSchedulerStop(ctx); err == nil {
						scope.releaseSchedulerStop(loan)
						t.Fatal("Execution loan granted Scheduler authority")
					}
					if loan, err := scope.borrow(ctx, store.tx, request); err == nil {
						scope.release(loan)
						t.Fatal("Execution loan granted Agent read authority")
					}
				}
				if store.transactions.Load() != 0 {
					t.Fatal("loan provenance reached authorization SQL")
				}
			})
		}
	}
}

func TestExecutionStopAuthorityScopeWaitsForAllOriginalLoans(t *testing.T) {
	for _, last := range []string{"agent", "scheduler", "execution"} {
		t.Run(last+"-last", func(t *testing.T) {
			d, _, store, request := executionStopTest(t, nil)
			ctx, scope := retirementPrepScope(t, d, request)
			read, err := scope.borrow(ctx, store.tx, request)
			if err != nil {
				t.Fatal(err)
			}
			var readOnce sync.Once
			readDone := func() { readOnce.Do(func() { scope.release(read) }) }
			t.Cleanup(readDone)
			scheduler, err := scope.borrowSchedulerStop(ctx)
			if err != nil {
				t.Fatal(err)
			}
			var schedulerOnce sync.Once
			schedulerDone := func() { schedulerOnce.Do(func() { scope.releaseSchedulerStop(scheduler) }) }
			t.Cleanup(schedulerDone)
			execution, err := scope.borrowExecutionStop(ctx)
			if err != nil {
				t.Fatal(err)
			}
			var executionOnce sync.Once
			executionDone := func() { executionOnce.Do(func() { scope.releaseExecutionStop(execution) }) }
			t.Cleanup(executionDone)
			closed := make(chan struct{})
			go func() { defer close(closed); scope.close() }()
			t.Cleanup(func() { readDone(); schedulerDone(); executionDone(); retirementPrepAwait(t, closed) })
			retirementPrepAwait(t, scope.ctx.Done())
			if loan, err := scope.borrowExecutionStop(context.WithoutCancel(ctx)); err == nil {
				scope.releaseExecutionStop(loan)
				t.Fatal("closed scope issued another Execution loan")
			}
			for name, release := range map[string]func(){"agent": readDone, "scheduler": schedulerDone, "execution": executionDone} {
				if name != last {
					release()
				}
			}
			select {
			case <-closed:
				t.Fatal("scope exited before the last original loan")
			default:
			}
			readDone()
			schedulerDone()
			executionDone()
			retirementPrepAwait(t, closed)
		})
	}
}

func TestExecutionStopAuthorityInvokeWaitsForOriginalStoreReturn(t *testing.T) {
	for _, name := range []string{"step-return", "caller-cancel"} {
		t.Run(name, func(t *testing.T) {
			entered, release, stepRelease := make(chan struct{}), make(chan struct{}), make(chan struct{})
			controlDone, invokeDone := make(chan struct{}), make(chan struct{})
			var once, stepOnce sync.Once
			free := func() { once.Do(func() { close(release) }) }
			freeStep := func() { stepOnce.Do(func() { close(stepRelease) }) }
			var authority *ExecutionStopAuthority
			var scope *agentPreparationScope
			var controlErr, invokeErr error
			var called atomic.Int32
			stepErr := errors.New("original Execution stop step")
			d, original, store, request := executionStopTest(t, func(ctx context.Context, actor i.Actor, cause c.LifecycleCause, target c.ScopeRef) error {
				scope = ctx.Value(agentPreparationScopeKey{}).(*agentPreparationScope)
				go func() {
					defer close(controlDone)
					controlErr = authority.WithConfirmedExecutionStop(ctx, actor, cause, target, func(context.Context) error { called.Add(1); return nil })
				}()
				<-stepRelease
				return stepErr
			})
			authority = original
			store.within = func(context.Context, f.TransactionCause, func(context.Context, f.Tx) error) f.CommitResult {
				close(entered)
				<-release
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
			retirementPrepAwait(t, scope.ctx.Done())
			select {
			case <-invokeDone:
				t.Fatal("step return or cancellation substituted for original control return")
			default:
			}
			select {
			case <-controlDone:
				t.Fatal("control detached its original Store return")
			default:
			}
			free()
			retirementPrepAwait(t, controlDone)
			retirementPrepAwait(t, invokeDone)
			if controlErr == nil || !errors.Is(invokeErr, stepErr) || called.Load() != 0 {
				t.Fatal("late unvalidated result granted a stop or replaced the original step error", controlErr, invokeErr)
			}
		})
	}
}
