package project

import (
	"context"
	"errors"
	"sync"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

type artifactSourceStopStore struct{ *schedulerStopTestStore }

// Model the original Store's held-lock check, including EX satisfying SH. The
// canonical SQL is still the existing sentinel, never a fabricated manifest.
func (s *artifactSourceStopStore) RequireHeldLocks(ctx context.Context, tx f.Tx, locks []f.LockRequest) error {
	s.record("locks")
	if tx != s.tx || !s.live {
		return fault(f.Forbidden)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	for _, l := range locks {
		m, ok := s.held[l.Key.Canonical()]
		if !ok || m != l.Mode && m != f.Exclusive {
			return fault(f.Forbidden)
		}
	}
	return nil
}

func artifactSourceStopFixture(t *testing.T, step LifecycleLocalStopStep) (*LifecycleStopDriver, *ArtifactSourceStopAuthority, *artifactSourceStopStore, c.AgentRetirementPreparationRequest, oc.ProjectSourceStopRequest) {
	t.Helper()
	_, _, original, _, r := schedulerStopTest(t, nil)
	store := &artifactSourceStopStore{original}
	if step == nil {
		step = func(context.Context, i.Actor, c.LifecycleCause, c.ScopeRef) error { return nil }
	}
	d := stopRoundTestDriver(t, store, step)
	a, err := NewArtifactSourceStopAuthority(d.state.authority, store)
	if err != nil {
		t.Fatal(err)
	}
	op, err := f.ParseID[oc.ProjectStopOperation](r.Cause.OperationID.String())
	if err != nil {
		t.Fatal(err)
	}
	cause, err := oc.NewProjectStopCause(oc.ProjectStopCauseDetails{ProjectID: r.ProjectID, OperationID: op, Action: oc.ProjectStopAction(r.Cause.Action), ProjectVersion: r.Cause.ProjectVersion})
	if err != nil {
		t.Fatal(err)
	}
	command, err := f.NewCommandIdentity("artifact", []string{r.ProjectID.String()}, "create", f.IdempotencyKey(testID[struct{}](t).String()))
	if err != nil {
		t.Fatal(err)
	}
	lease, err := oc.NewSourceLease(oc.NewSourceIssuer(), testID[oc.Lease](t))
	if err != nil {
		t.Fatal(err)
	}
	req, err := oc.NewProjectSourceStopRequest(oc.ProjectSourceStopDetails{Actor: r.Actor, Cause: cause, Command: command, Lease: lease, SourceProjectID: r.ProjectID, TargetProjectID: r.ProjectID, ObjectID: testID[oc.StoredObject](t)})
	if err != nil {
		t.Fatal(err)
	}
	store.required, err = req.RequiredLocks()
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range store.required {
		store.held[l.Key.Canonical()] = l.Mode
	}
	return d, a, store, r, req
}

func TestArtifactSourceStopRequiresOriginalIssuer(t *testing.T) {
	d, a, s, r, req := artifactSourceStopFixture(t, nil)
	_, foreign, other, _, _ := artifactSourceStopFixture(t, nil)
	for _, mode := range []string{"nil-authority", "nil-store", "foreign-store", "public-request", "foreign-authority", "closed-scope"} {
		t.Run(mode, func(t *testing.T) {
			switch mode {
			case "nil-authority":
				if _, err := NewArtifactSourceStopAuthority(nil, s); err == nil {
					t.Fatal("nil authority bound")
				}
				return
			case "nil-store":
				if _, err := NewArtifactSourceStopAuthority(d.state.authority, nil); err == nil {
					t.Fatal("nil Store bound")
				}
				return
			case "foreign-store":
				if _, err := NewArtifactSourceStopAuthority(d.state.authority, other); err == nil {
					t.Fatal("foreign Store bound")
				}
				return
			}
			ctx := context.Background()
			current := a
			if mode != "public-request" {
				var scope *agentPreparationScope
				ctx, scope = retirementPrepScope(t, d, r)
				if mode == "closed-scope" {
					scope.close()
					ctx = context.WithoutCancel(ctx)
				}
				if mode == "foreign-authority" {
					current = foreign
				}
			}
			err := current.WithProjectSourceStop(ctx, req, func(context.Context) error { t.Error("unissued source callback"); return nil })
			if err == nil || s.transactions.Load() != 0 {
				t.Fatal("invalid issuer reached original Store", err)
			}
		})
	}
}

func TestArtifactSourceStopExactRequest(t *testing.T) {
	for _, mode := range []string{"same-project", "cross-project-locks", "unrelated-stop", "wrong-command-owner", "empty-handle", "different-issuer"} {
		t.Run(mode, func(t *testing.T) {
			_, _, _, _, req := artifactSourceStopFixture(t, nil)
			d := req.Details()
			wantValid := true
			switch mode {
			case "cross-project-locks":
				d.SourceProjectID = testID[i.Project](t)
			case "unrelated-stop":
				d.SourceProjectID = testID[i.Project](t)
				d.TargetProjectID = testID[i.Project](t)
				d.Command, _ = f.NewCommandIdentity("artifact", []string{d.TargetProjectID.String()}, "create", d.Command.Key())
				wantValid = false
			case "wrong-command-owner":
				d.Command, _ = f.NewCommandIdentity("artifact", []string{testID[i.Project](t).String()}, "create", d.Command.Key())
				wantValid = false
			case "empty-handle":
				d.Lease = oc.SourceLease{}
				wantValid = false
			case "different-issuer":
				d.Lease, _ = oc.NewSourceLease(oc.NewSourceIssuer(), d.Lease.ID())
			}
			changed, err := oc.NewProjectSourceStopRequest(d)
			if !wantValid {
				if err == nil {
					t.Fatal("invalid command/Project/handle shape accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			locks, err := changed.RequiredLocks()
			if err != nil {
				t.Fatal(err)
			}
			if mode == "cross-project-locks" {
				if len(locks) != 2 || f.CompareLockKeys(locks[0].Key, locks[1].Key) >= 0 {
					t.Fatal("dual Project locks not complete and ordered")
				}
			} else if len(locks) != 1 {
				t.Fatal("same Project lock duplicated")
			}
			for _, l := range locks {
				if l.Mode != f.Exclusive {
					t.Fatal("source convergence requires Project EX")
				}
			}
			if changed.Equal(req) != (mode == "same-project") {
				t.Fatal("request lost exact original lease issuer or Project")
			}
		})
	}
}

func TestArtifactSourceStopExactTransaction(t *testing.T) {
	for _, mode := range []string{"missing-loan", "released-loan", "changed-object", "changed-lease-issuer", "wrong-tx", "expired-tx", "nil-executor", "missing-lock", "missing-claim", "other-fence", "original-scan"} {
		t.Run(mode, func(t *testing.T) {
			d, a, s, r, req := artifactSourceStopFixture(t, nil)
			ctx, scope := retirementPrepScope(t, d, r)
			loan, err := scope.borrowArtifactSourceStop(ctx, req)
			if err != nil {
				t.Fatal(err)
			}
			var once sync.Once
			release := func() { once.Do(func() { scope.releaseArtifactSourceStop(loan) }) }
			t.Cleanup(release)
			if mode != "missing-loan" {
				ctx = context.WithValue(ctx, artifactSourceStopLoanKey{}, loan)
			}
			claim := scope.claim
			s.claim = &claim
			tx := s.tx
			switch mode {
			case "released-loan":
				release()
				ctx = context.WithoutCancel(ctx)
			case "changed-object":
				d := req.Details()
				d.ObjectID = testID[oc.StoredObject](t)
				req, err = oc.NewProjectSourceStopRequest(d)
			case "changed-lease-issuer":
				d := req.Details()
				d.Lease, err = oc.NewSourceLease(oc.NewSourceIssuer(), d.Lease.ID())
				if err == nil {
					req, err = oc.NewProjectSourceStopRequest(d)
				}
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
			}
			if err != nil {
				t.Fatal(err)
			}
			err = a.RequireProjectSourceStopInTx(ctx, tx, req)
			if err == nil {
				t.Fatal("invalid source loan/transaction accepted")
			}
			if mode == "original-scan" && !errors.Is(err, s.readErr) {
				t.Fatal("original canonical Scan error lost", err)
			}
			if s.transactions.Load() != 0 {
				t.Fatal("Require opened another transaction")
			}
		})
	}
}

func TestArtifactSourceStopUnconfirmedOriginalReturn(t *testing.T) {
	for _, mode := range []string{"not-committed", "unknown", "no-callback", "rejected-callback"} {
		t.Run(mode, func(t *testing.T) {
			d, a, s, r, req := artifactSourceStopFixture(t, nil)
			ctx, scope := retirementPrepScope(t, d, r)
			claim := scope.claim
			s.claim = &claim
			s.within = func(ctx context.Context, cause f.TransactionCause, fn func(context.Context, f.Tx) error) f.CommitResult {
				switch mode {
				case "not-committed":
					return f.NotCommittedResult(fault(f.ResourceBusy))
				case "unknown":
					return f.UnknownResult(testID[f.TransactionAttempt](t), cause)
				case "rejected-callback":
					if err := fn(ctx, s.tx); !errors.Is(err, s.readErr) {
						t.Error("did not reach original canonical failure", err)
					}
				}
				return f.CommittedResult()
			}
			err := a.WithProjectSourceStop(ctx, req, func(context.Context) error { t.Error("unconfirmed callback"); return nil })
			if err == nil || s.transactions.Load() != 1 {
				t.Fatal("unconfirmed Store result authorized source", err)
			}
		})
	}
}

func TestArtifactSourceStopFamiliesAndParentReturn(t *testing.T) {
	for _, family := range []string{"agent", "scheduler", "execution", "source"} {
		t.Run(family, func(t *testing.T) {
			d, _, s, r, req := artifactSourceStopFixture(t, nil)
			ctx, scope := retirementPrepScope(t, d, r)
			loan, err := scope.borrowArtifactSourceStop(ctx, req)
			if err != nil {
				t.Fatal(err)
			}
			defer scope.releaseArtifactSourceStop(loan)
			ctx = context.WithValue(ctx, artifactSourceStopLoanKey{}, loan)
			switch family {
			case "agent":
				v, err := scope.borrow(ctx, s.tx, r)
				if err == nil {
					scope.release(v)
					t.Fatal("source loan issued Agent read")
				}
			case "scheduler":
				v, err := scope.borrowSchedulerStop(ctx)
				if err == nil {
					scope.releaseSchedulerStop(v)
					t.Fatal("source loan issued Scheduler stop")
				}
			case "execution":
				v, err := scope.borrowExecutionStop(ctx)
				if err == nil {
					scope.releaseExecutionStop(v)
					t.Fatal("source loan issued Execution stop")
				}
			case "source":
				v, err := scope.borrowArtifactSourceStop(ctx, req)
				if err == nil {
					scope.releaseArtifactSourceStop(v)
					t.Fatal("source loan reissued itself")
				}
			}
		})
	}
	t.Run("original-authorization-return", func(t *testing.T) {
		d, a, s, r, req := artifactSourceStopFixture(t, nil)
		ctx, scope := retirementPrepScope(t, d, r)
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
			if a.WithProjectSourceStop(ctx, req, func(context.Context) error { t.Error("unvalidated late callback"); return nil }) == nil {
				t.Error("late unvalidated acknowledgement accepted")
			}
		}()
		t.Cleanup(func() { free(); retirementPrepAwait(t, returned); retirementPrepAwait(t, closed) })
		retirementPrepAwait(t, entered)
		go func() { defer close(closed); scope.close() }()
		retirementPrepAwait(t, scope.ctx.Done())
		select {
		case <-closed:
			t.Fatal("scope cancellation replaced source Store return")
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
	})
}
