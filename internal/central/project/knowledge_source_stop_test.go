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

func knowledgeSourceStopFixture(t *testing.T) (*LifecycleStopDriver, *KnowledgeProjectSourceStopAuthority, *artifactSourceStopStore, c.AgentRetirementPreparationRequest, oc.KnowledgeProjectSourceStopRequest) {
	t.Helper()
	d, _, s, r, original := artifactSourceStopFixture(t, nil)
	a, err := NewKnowledgeProjectSourceStopAuthority(d.state.authority, s)
	if err != nil {
		t.Fatal(err)
	}
	old := original.Details()
	command, err := f.NewCommandIdentity("knowledge", []string{r.ProjectID.String()}, "create", old.Command.Key())
	if err != nil {
		t.Fatal(err)
	}
	request, err := oc.NewKnowledgeProjectSourceStopRequest(oc.KnowledgeProjectSourceStopDetails{Actor: r.Actor, Cause: old.Cause, Command: command, Lease: old.Lease, SourceProjectID: testID[i.Project](t), TargetProjectID: r.ProjectID, ObjectID: old.ObjectID})
	if err != nil {
		t.Fatal(err)
	}
	s.required, err = request.RequiredLocks()
	if err != nil {
		t.Fatal(err)
	}
	s.held = map[string]f.LockMode{}
	for _, lock := range s.required {
		s.held[lock.Key.Canonical()] = lock.Mode
	}
	return d, a, s, r, request
}

func TestKnowledgeSourceStopIndependentOriginalFamily(t *testing.T) {
	for _, mode := range []string{"public", "no-knowledge-parent", "artifact-parent", "wrong-authority", "released-parent"} {
		t.Run(mode, func(t *testing.T) {
			d, a, s, r, request := knowledgeSourceStopFixture(t)
			ctx, scope := retirementPrepScope(t, d, r)
			parent, err := scope.borrowKnowledgeStop(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer scope.releaseKnowledgeStop(parent)
			ctx = context.WithValue(ctx, knowledgeStopLoanKey{}, parent)
			switch mode {
			case "public":
				ctx = context.Background()
			case "no-knowledge-parent":
				ctx = context.WithValue(ctx, knowledgeStopLoanKey{}, (*knowledgeStopLoan)(nil))
			case "artifact-parent":
				ctx = context.WithValue(ctx, artifactSourceStopLoanKey{}, &artifactSourceStopLoan{scope: scope})
			case "wrong-authority":
				_, a, _, _, _ = knowledgeSourceStopFixture(t)
			case "released-parent":
				scope.releaseKnowledgeStop(parent)
			}
			if err := a.WithKnowledgeProjectSourceStop(ctx, request, func(context.Context) error { t.Error("foreign family invoked Object"); return nil }); err == nil || s.transactions.Load() != 0 {
				t.Fatal("unissued source stop reached Store", err)
			}
		})
	}
}

func TestKnowledgeSourceStopExactDualProjectAndTuple(t *testing.T) {
	for _, mode := range []string{"dual-project", "missing-source-lock", "shared-source-lock", "changed-lease", "changed-fence", "wrong-tx"} {
		t.Run(mode, func(t *testing.T) {
			d, a, s, r, request := knowledgeSourceStopFixture(t)
			ctx, scope := retirementPrepScope(t, d, r)
			parent, err := scope.borrowKnowledgeStop(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer scope.releaseKnowledgeStop(parent)
			ctx = context.WithValue(ctx, knowledgeStopLoanKey{}, parent)
			child, err := scope.borrowKnowledgeProjectSourceStop(ctx, request)
			if err != nil {
				t.Fatal(err)
			}
			defer scope.releaseKnowledgeProjectSourceStop(child)
			ctx = context.WithValue(ctx, knowledgeSourceStopLoanKey{}, child)
			claim, tx := scope.claim, s.tx
			s.claim = &claim
			source := projectLock(request.Details().SourceProjectID, f.Exclusive).Key.Canonical()
			switch mode {
			case "missing-source-lock":
				delete(s.held, source)
			case "shared-source-lock":
				s.held[source] = f.Shared
			case "changed-lease":
				details := request.Details()
				details.Lease, err = oc.NewSourceLease(oc.NewSourceIssuer(), details.Lease.ID())
				if err != nil {
					t.Fatal(err)
				}
				request, err = oc.NewKnowledgeProjectSourceStopRequest(details)
				if err != nil {
					t.Fatal(err)
				}
			case "changed-fence":
				claim.fence++
			case "wrong-tx":
				tx = f.NewTx()
			}
			err = a.RequireKnowledgeProjectSourceStopInTx(ctx, tx, request)
			if err == nil || errors.Is(err, s.readErr) != (mode == "dual-project") || s.transactions.Load() != 0 {
				t.Fatal("lost original source/target/issuer gate", err)
			}
		})
	}
}

func TestKnowledgeSourceStopParentWaitsForActualReturn(t *testing.T) {
	d, a, s, r, request := knowledgeSourceStopFixture(t)
	ctx, scope := retirementPrepScope(t, d, r)
	parent, err := scope.borrowKnowledgeStop(ctx)
	if err != nil {
		t.Fatal(err)
	}
	ctx = context.WithValue(ctx, knowledgeStopLoanKey{}, parent)
	entered, release, returned, parentDone, closed := make(chan struct{}), make(chan struct{}), make(chan struct{}), make(chan struct{}), make(chan struct{})
	var once sync.Once
	free := func() { once.Do(func() { close(release) }) }
	s.within = func(context.Context, f.TransactionCause, func(context.Context, f.Tx) error) f.CommitResult {
		close(entered)
		<-release
		return f.CommittedResult()
	}
	go func() {
		defer close(returned)
		if err := a.WithKnowledgeProjectSourceStop(ctx, request, func(context.Context) error { t.Error("unvalidated source callback"); return nil }); err == nil {
			t.Error("unvalidated source accepted")
		}
	}()
	t.Cleanup(func() {
		free()
		retirementPrepAwait(t, returned)
		retirementPrepAwait(t, parentDone)
		retirementPrepAwait(t, closed)
	})
	retirementPrepAwait(t, entered)
	go func() { defer close(parentDone); scope.releaseKnowledgeStop(parent) }()
	go func() { defer close(closed); scope.close() }()
	retirementPrepAwait(t, scope.ctx.Done())
	select {
	case <-parentDone:
		t.Fatal("Knowledge parent released before original child Store return")
	case <-closed:
		t.Fatal("Project driver released before child Store return")
	case <-returned:
		t.Fatal("cancellation fabricated Store return")
	default:
	}
	free()
	retirementPrepAwait(t, returned)
	retirementPrepAwait(t, parentDone)
	retirementPrepAwait(t, closed)
}
