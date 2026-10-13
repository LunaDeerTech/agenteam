package project

import (
	"context"
	"errors"
	"sync"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	c "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

// This original Store supplies a controlled claim, then stops at its canonical
// SQL sentinel. It never asserts successful persisted lifecycle authorization.
func runnerManagedStopTest(t *testing.T) (*LifecycleStopDriver, *RunnerManagedStopAuthority, *artifactSourceStopStore, c.AgentRetirementPreparationRequest) {
	t.Helper()
	d, _, s, request, _ := artifactSourceStopFixture(t, nil)
	a, err := NewRunnerManagedStopAuthority(d.state.authority, s)
	if err != nil {
		t.Fatal(err)
	}
	s.required = []f.LockRequest{projectLock(request.ProjectID, f.Exclusive), schedulerLock(request.ProjectID)}
	s.held = map[string]f.LockMode{s.required[0].Key.Canonical(): f.Exclusive, s.required[1].Key.Canonical(): f.Exclusive}
	return d, a, s, request
}

func TestRunnerManagedStopDeletePrivateIssuer(t *testing.T) {
	for _, mode := range []string{"public", "archive", "foreign-authority", "closed"} {
		t.Run(mode, func(t *testing.T) {
			d, a, s, r := runnerManagedStopTest(t)
			if mode == "archive" {
				r.Cause.Action = c.Archive
			}
			ctx, scope := retirementPrepScope(t, d, r)
			switch mode {
			case "public":
				ctx = context.Background()
			case "foreign-authority":
				_, a, _, _ = runnerManagedStopTest(t)
			case "closed":
				scope.close()
				ctx = context.WithoutCancel(ctx)
			}
			err := a.WithProjectStop(ctx, r.Actor, r.Cause, c.ScopeRef{Kind: c.ProjectScope, ProjectID: r.ProjectID}, func(context.Context) error {
				t.Error("unissued RunnerManaged stop callback")
				return nil
			})
			if err == nil || s.transactions.Load() != 0 {
				t.Fatal("invalid issuer reached Store", err)
			}
		})
	}
}

func TestRunnerManagedStopExactClaimAndProjectLock(t *testing.T) {
	for _, mode := range []string{"full-union", "missing-schedule", "shared-project", "wrong-tx", "released-loan", "changed-fence"} {
		t.Run(mode, func(t *testing.T) {
			d, a, s, r := runnerManagedStopTest(t)
			ctx, scope := retirementPrepScope(t, d, r)
			loan, err := scope.borrowRunnerManagedStop(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer scope.releaseRunnerManagedStop(loan)
			ctx = context.WithValue(ctx, runnerManagedStopLoanKey{}, loan)
			claim, tx := scope.claim, s.tx
			s.claim = &claim
			switch mode {
			case "missing-schedule":
				delete(s.held, schedulerLock(r.ProjectID).Key.Canonical())
			case "shared-project":
				s.held[projectLock(r.ProjectID, f.Exclusive).Key.Canonical()] = f.Shared
			case "wrong-tx":
				tx = f.NewTx()
			case "released-loan":
				scope.releaseRunnerManagedStop(loan)
				ctx = context.WithoutCancel(ctx)
			case "changed-fence":
				claim.fence++
			}
			err = a.RequireProjectStopInTx(ctx, tx, r.Actor, r.Cause, c.ScopeRef{Kind: c.ProjectScope, ProjectID: r.ProjectID})
			if err == nil || errors.Is(err, s.readErr) != (mode == "full-union") || s.transactions.Load() != 0 {
				t.Fatal("lost exact gate or mandatory Schedule lock", err)
			}
		})
	}
}

func TestRunnerManagedStopUnconfirmedStoreCannotInvoke(t *testing.T) {
	for _, mode := range []string{"unknown", "no-callback", "rejected-callback"} {
		t.Run(mode, func(t *testing.T) {
			d, a, s, r := runnerManagedStopTest(t)
			ctx, scope := retirementPrepScope(t, d, r)
			s.claim = &scope.claim
			var original f.CommitResult
			s.within = func(ctx context.Context, cause f.TransactionCause, fn func(context.Context, f.Tx) error) f.CommitResult {
				if mode == "unknown" {
					original = f.UnknownResult(testID[f.TransactionAttempt](t), cause)
					return original
				}
				if mode == "rejected-callback" && !errors.Is(fn(ctx, s.tx), s.readErr) {
					t.Error("authorization did not reach original canonical read")
				}
				return f.CommittedResult()
			}
			err := a.WithProjectStop(ctx, r.Actor, r.Cause, c.ScopeRef{Kind: c.ProjectScope, ProjectID: r.ProjectID}, func(context.Context) error {
				t.Error("unconfirmed authorization invoked RunnerManaged")
				return nil
			})
			if err == nil || s.transactions.Load() != 1 {
				t.Fatal("unconfirmed Store accepted", err)
			}
			if mode == "unknown" {
				requireFinalizeUnknown(t, err, original)
			}
		})
	}
}

func TestRunnerManagedStopLoanFamilyIsolation(t *testing.T) {
	d, _, s, r, source := artifactSourceStopFixture(t, nil)
	ctx, scope := retirementPrepScope(t, d, r)
	borrowers := map[string]func(context.Context) (func(), error){
		"agent": func(ctx context.Context) (func(), error) {
			v, err := scope.borrow(ctx, s.tx, r)
			return func() { scope.release(v) }, err
		},
		"scheduler": func(ctx context.Context) (func(), error) {
			v, err := scope.borrowSchedulerStop(ctx)
			return func() { scope.releaseSchedulerStop(v) }, err
		},
		"execution": func(ctx context.Context) (func(), error) {
			v, err := scope.borrowExecutionStop(ctx)
			return func() { scope.releaseExecutionStop(v) }, err
		},
		"source": func(ctx context.Context) (func(), error) {
			v, err := scope.borrowArtifactSourceStop(ctx, source)
			return func() { scope.releaseArtifactSourceStop(v) }, err
		},
		"work": func(ctx context.Context) (func(), error) {
			v, err := scope.borrowWorkStop(ctx)
			return func() { scope.releaseWorkStop(v) }, err
		},
		"knowledge": func(ctx context.Context) (func(), error) {
			v, e := scope.borrowKnowledgeStop(ctx)
			return func() { scope.releaseKnowledgeStop(v) }, e
		},
		"usage": func(ctx context.Context) (func(), error) {
			v, e := scope.borrowUsageStop(ctx)
			return func() { scope.releaseUsageStop(v) }, e
		},
		"runnerManaged": func(ctx context.Context) (func(), error) {
			v, err := scope.borrowRunnerManagedStop(ctx)
			return func() { scope.releaseRunnerManagedStop(v) }, err
		},
	}
	loan, err := scope.borrowRunnerManagedStop(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer scope.releaseRunnerManagedStop(loan)
	borrowed := context.WithValue(ctx, runnerManagedStopLoanKey{}, loan)
	for name, borrow := range borrowers {
		t.Run(name, func(t *testing.T) {
			if end, err := borrow(context.WithoutCancel(borrowed)); err == nil {
				end()
				t.Fatal("RunnerManaged loan reissued another family")
			}
		})
	}
	// An existing Work loan must also be unable to enter the new RunnerManaged family.
	work, err := scope.borrowWorkStop(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer scope.releaseWorkStop(work)
	if nested, err := scope.borrowRunnerManagedStop(context.WithValue(ctx, workStopLoanKey{}, work)); err == nil {
		scope.releaseRunnerManagedStop(nested)
		t.Fatal("Work loan issued RunnerManaged")
	}
}

func TestRunnerManagedStopParentWaitsForOriginalStoreReturn(t *testing.T) {
	d, a, s, r := runnerManagedStopTest(t)
	ctx, scope := retirementPrepScope(t, d, r)
	entered, release, returned, closed := make(chan struct{}), make(chan struct{}), make(chan struct{}), make(chan struct{})
	var once sync.Once
	free := func() { once.Do(func() { close(release) }) }
	closing := false
	s.within = func(context.Context, f.TransactionCause, func(context.Context, f.Tx) error) f.CommitResult {
		close(entered)
		<-release
		return f.CommittedResult()
	}
	go func() {
		defer close(returned)
		if err := a.WithProjectStop(ctx, r.Actor, r.Cause, c.ScopeRef{Kind: c.ProjectScope, ProjectID: r.ProjectID}, func(context.Context) error {
			t.Error("late unvalidated Store return issued callback")
			return nil
		}); err == nil {
			t.Error("unvalidated Store accepted")
		}
	}()
	t.Cleanup(func() {
		free()
		retirementPrepAwait(t, returned)
		if closing {
			retirementPrepAwait(t, closed)
		}
	})
	retirementPrepAwait(t, entered)
	closing = true
	go func() { defer close(closed); scope.close() }()
	retirementPrepAwait(t, scope.ctx.Done())
	select {
	case <-closed:
		t.Fatal("parent retired while original RunnerManaged Store was held")
	case <-returned:
		t.Fatal("cancellation replaced original Store return")
	default:
	}
	free()
	retirementPrepAwait(t, returned)
	retirementPrepAwait(t, closed)
}
