package project

import (
	"context"
	"errors"
	"sync"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

// The shared Store exposes original claim reads and the first canonical SQL
// error. It does not manufacture a successful manifest or Work stop report.
func workStopTest(t *testing.T) (*LifecycleStopDriver, *WorkStopAuthority, *artifactSourceStopStore, c.AgentRetirementPreparationRequest) {
	t.Helper()
	d, _, store, request, _ := artifactSourceStopFixture(t, nil)
	a, err := NewWorkStopAuthority(d.state.authority, store)
	if err != nil {
		t.Fatal(err)
	}
	store.required = []f.LockRequest{projectLock(request.ProjectID, f.Exclusive)}
	return d, a, store, request
}

func TestWorkStopRequiresOriginalIssuer(t *testing.T) {
	for _, mode := range []string{"nil-authority", "nil-store", "foreign-store", "public-request", "foreign-authority", "closed-scope", "other-project", "other-version"} {
		t.Run(mode, func(t *testing.T) {
			d, a, s, request := workStopTest(t)
			_, foreign, other, _ := workStopTest(t)
			switch mode {
			case "nil-authority":
				if _, err := NewWorkStopAuthority(nil, s); err == nil {
					t.Fatal("unbound authority accepted")
				}
				return
			case "nil-store":
				if _, err := NewWorkStopAuthority(d.state.authority, nil); err == nil {
					t.Fatal("missing Store accepted")
				}
				return
			case "foreign-store":
				if _, err := NewWorkStopAuthority(d.state.authority, other); err == nil {
					t.Fatal("foreign Store accepted")
				}
				return
			}
			ctx := context.Background()
			if mode != "public-request" {
				var scope *agentPreparationScope
				ctx, scope = retirementPrepScope(t, d, request)
				if mode == "closed-scope" {
					scope.close()
					ctx = context.WithoutCancel(ctx)
				}
			}
			target := c.ScopeRef{Kind: c.ProjectScope, ProjectID: request.ProjectID}
			switch mode {
			case "foreign-authority":
				a = foreign
			case "other-project":
				target.ProjectID = testID[i.Project](t)
			case "other-version":
				request.Cause.ProjectVersion++
			}
			err := a.WithProjectStop(ctx, request.Actor, request.Cause, target, func(context.Context) error {
				t.Error("unissued Work callback")
				return nil
			})
			if err == nil || s.transactions.Load() != 0 {
				t.Fatal("invalid issuer reached authorization Store", err)
			}
		})
	}
}

func TestWorkStopRequiresExactTransaction(t *testing.T) {
	for _, mode := range []string{"missing-loan", "released-loan", "wrong-tx", "expired-tx", "nil-executor", "missing-lock", "missing-claim", "other-fence", "terminal-claim", "archive-original-scan", "delete-original-scan"} {
		t.Run(mode, func(t *testing.T) {
			d, a, s, request := workStopTest(t)
			if mode == "archive-original-scan" {
				request.Cause.Action = c.Archive
			}
			ctx, scope := retirementPrepScope(t, d, request)
			loan, err := scope.borrowWorkStop(ctx)
			if err != nil {
				t.Fatal(err)
			}
			var once sync.Once
			release := func() { once.Do(func() { scope.releaseWorkStop(loan) }) }
			t.Cleanup(release)
			if mode != "missing-loan" {
				ctx = context.WithValue(ctx, workStopLoanKey{}, loan)
			}
			claim := scope.claim
			s.claim = &claim
			tx := s.tx
			switch mode {
			case "released-loan":
				release()
				ctx = context.WithoutCancel(ctx)
			case "wrong-tx":
				tx = f.NewTx()
			case "expired-tx":
				s.live = false
			case "nil-executor":
				s.nilExecutor = true
			case "missing-lock":
				clear(s.held)
			case "missing-claim":
				s.claimMissing = true
			case "other-fence":
				claim.fence++
			case "terminal-claim":
				claim.phase = "terminal"
			}
			err = a.RequireProjectStopInTx(ctx, tx, request.Actor, request.Cause, c.ScopeRef{Kind: c.ProjectScope, ProjectID: request.ProjectID})
			if err == nil {
				t.Fatal("invalid transaction or unverified canonical facts accepted")
			}
			if mode == "archive-original-scan" || mode == "delete-original-scan" {
				if !errors.Is(err, s.readErr) {
					t.Fatal("valid stop action did not preserve original canonical Scan", err)
				}
			}
			if s.transactions.Load() != 0 {
				t.Fatal("Require opened another transaction")
			}
		})
	}
}

func TestWorkStopUnconfirmedStoreReturn(t *testing.T) {
	for _, mode := range []string{"not-committed", "unknown", "no-callback", "rejected-callback"} {
		t.Run(mode, func(t *testing.T) {
			d, a, s, request := workStopTest(t)
			ctx, scope := retirementPrepScope(t, d, request)
			claim := scope.claim
			s.claim = &claim
			var original f.CommitResult
			s.within = func(ctx context.Context, cause f.TransactionCause, fn func(context.Context, f.Tx) error) f.CommitResult {
				switch mode {
				case "not-committed":
					return f.NotCommittedResult(fault(f.ResourceBusy))
				case "unknown":
					original = f.UnknownResult(testID[f.TransactionAttempt](t), cause)
					return original
				case "rejected-callback":
					if err := fn(ctx, s.tx); !errors.Is(err, s.readErr) {
						t.Error("did not reach original canonical Scan", err)
					}
				}
				return f.CommittedResult()
			}
			err := a.WithProjectStop(ctx, request.Actor, request.Cause, c.ScopeRef{Kind: c.ProjectScope, ProjectID: request.ProjectID}, func(context.Context) error {
				t.Error("unconfirmed authorization cancelled Work")
				return nil
			})
			if err == nil || s.transactions.Load() != 1 {
				t.Fatal("unconfirmed Store result accepted", err)
			}
			if mode == "unknown" {
				got, ok := UnknownAttempt(err)
				actual, expected := got.Cause().Details(), original.Cause().Details()
				if !ok || got.AttemptID() != original.AttemptID() || actual.Kind != expected.Kind || actual.Owner != expected.Owner || actual.RecoveryRunID != expected.RecoveryRunID || actual.CheckpointRef != expected.CheckpointRef {
					t.Fatal("original unknown provenance changed")
				}
			}
		})
	}
}

func TestWorkStopLoanFamilies(t *testing.T) {
	for _, family := range []string{"agent", "scheduler", "execution", "source", "work"} {
		t.Run(family, func(t *testing.T) {
			d, _, s, request, source := artifactSourceStopFixture(t, nil)
			ctx, scope := retirementPrepScope(t, d, request)
			loan, err := scope.borrowWorkStop(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer scope.releaseWorkStop(loan)
			borrowed := context.WithValue(ctx, workStopLoanKey{}, loan)
			switch family {
			case "agent":
				v, err := scope.borrow(borrowed, s.tx, request)
				if err == nil {
					scope.release(v)
					t.Fatal("Work loan issued Agent read")
				}
				v, err = scope.borrow(ctx, s.tx, request)
				if err != nil {
					t.Fatal(err)
				}
				defer scope.release(v)
				ctx = context.WithValue(ctx, agentPreparationLoanKey{}, v)
			case "scheduler":
				v, err := scope.borrowSchedulerStop(borrowed)
				if err == nil {
					scope.releaseSchedulerStop(v)
					t.Fatal("Work issued Scheduler")
				}
				v, err = scope.borrowSchedulerStop(ctx)
				if err != nil {
					t.Fatal(err)
				}
				defer scope.releaseSchedulerStop(v)
				ctx = context.WithValue(ctx, schedulerStopLoanKey{}, v)
			case "execution":
				v, err := scope.borrowExecutionStop(borrowed)
				if err == nil {
					scope.releaseExecutionStop(v)
					t.Fatal("Work issued Execution")
				}
				v, err = scope.borrowExecutionStop(ctx)
				if err != nil {
					t.Fatal(err)
				}
				defer scope.releaseExecutionStop(v)
				ctx = context.WithValue(ctx, executionStopLoanKey{}, v)
			case "source":
				v, err := scope.borrowArtifactSourceStop(borrowed, source)
				if err == nil {
					scope.releaseArtifactSourceStop(v)
					t.Fatal("Work issued source stop")
				}
				v, err = scope.borrowArtifactSourceStop(ctx, source)
				if err != nil {
					t.Fatal(err)
				}
				defer scope.releaseArtifactSourceStop(v)
				ctx = context.WithValue(ctx, artifactSourceStopLoanKey{}, v)
			case "work":
				ctx = borrowed
			}
			if v, err := scope.borrowWorkStop(context.WithoutCancel(ctx)); err == nil {
				scope.releaseWorkStop(v)
				t.Fatal("independent control reissued Work")
			}
		})
	}
}

func TestWorkStopParentWaitsForOriginalReturn(t *testing.T) {
	d, a, s, request := workStopTest(t)
	ctx, scope := retirementPrepScope(t, d, request)
	entered, release, returned, closed := make(chan struct{}), make(chan struct{}), make(chan struct{}), make(chan struct{})
	var once sync.Once
	free := func() { once.Do(func() { close(release) }) }
	s.within = func(context.Context, f.TransactionCause, func(context.Context, f.Tx) error) f.CommitResult {
		close(entered)
		<-release
		return f.CommittedResult()
	}
	go func() {
		defer close(returned)
		if a.WithProjectStop(ctx, request.Actor, request.Cause, c.ScopeRef{Kind: c.ProjectScope, ProjectID: request.ProjectID}, func(context.Context) error {
			t.Error("unvalidated late callback")
			return nil
		}) == nil {
			t.Error("late acknowledgement manufactured authority")
		}
	}()
	t.Cleanup(func() { free(); retirementPrepAwait(t, returned); retirementPrepAwait(t, closed) })
	retirementPrepAwait(t, entered)
	go func() { defer close(closed); scope.close() }()
	retirementPrepAwait(t, scope.ctx.Done())
	select {
	case <-closed:
		t.Fatal("parent ignored original Work authorization return")
	default:
	}
	select {
	case <-returned:
		t.Fatal("authorization Store detached")
	default:
	}
	free()
	retirementPrepAwait(t, returned)
	retirementPrepAwait(t, closed)
	if _, err := scope.borrowWorkStop(context.WithoutCancel(ctx)); err == nil {
		t.Fatal("closed scope issued new Work loan")
	}
}
