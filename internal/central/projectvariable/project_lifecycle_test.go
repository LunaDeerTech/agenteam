package projectvariable

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/projectvariable/contract"
)

// The SQL/Project ports here are explicit controls of the service's ownership
// decisions. Only the integration fixture can establish a real lifecycle gate.
type localStopStore struct {
	Store
	tx      f.Tx
	locks   []f.LockRequest
	result  f.CommitState
	after   func(context.Context)
	entered int
}

func (s *localStopStore) InTx(tx f.Tx) (postgres.SQLExecutor, error) {
	if tx != s.tx || !tx.Valid() {
		return nil, fault(f.Forbidden)
	}
	return s, nil
}
func (s *localStopStore) AcquireAll(ctx context.Context, tx f.Tx, locks []f.LockRequest) error {
	if _, err := s.InTx(tx); err != nil {
		return err
	}
	s.locks = append([]f.LockRequest(nil), locks...)
	return ctx.Err()
}
func (s *localStopStore) WithinTx(ctx context.Context, cause f.TransactionCause, fn func(context.Context, f.Tx) error) f.CommitResult {
	s.entered++
	s.tx = f.NewTx()
	defer func() { s.tx = f.Tx{}; s.locks = nil }()
	if err := fn(ctx, s.tx); err != nil {
		var problem *f.Fault
		if !errors.As(err, &problem) {
			problem = fault(f.InternalError)
		}
		return f.NotCommittedResult(problem)
	}
	if s.after != nil {
		s.after(ctx)
	}
	switch s.result {
	case f.Unknown:
		return f.UnknownResult(testID[f.TransactionAttempt](99), cause)
	case f.NotCommitted:
		return f.NotCommittedResult(f.NewFault(f.DependencyUnavailable, f.NotCommitted))
	default:
		return f.CommittedResult()
	}
}

type localStopProjects struct {
	pc.ProjectAuthority
	store   *localStopStore
	cause   pc.LifecycleCause
	project i.ProjectID
	err     error
}

func (p *localStopProjects) ValidateLifecycleInTx(ctx context.Context, tx f.Tx, actor i.Actor, cause pc.LifecycleCause, participant pc.ParticipantName, phase pc.OperationPhase) error {
	if _, err := p.store.InTx(tx); err != nil {
		return err
	}
	if cause != p.cause || actor.Details().ProjectID != p.project.String() || participant != pc.SkillsParticipant || phase != pc.StopPhase || len(p.store.locks) != 1 || p.store.locks[0] != projectLock(p.project, f.Shared) {
		return fault(f.Forbidden)
	}
	if p.err != nil {
		return p.err
	}
	return ctx.Err()
}
func localStopFixture(t *testing.T, action pc.LifecycleAction) (*ProjectCallStopper, *Service, *SecretService, *localStopStore, *localStopProjects, i.Actor, pc.LifecycleCause, pc.ScopeRef) {
	t.Helper()
	cause := pc.LifecycleCause{OperationID: testID[pc.Operation](90), Action: action, ProjectVersion: 7}
	scope := pc.ScopeRef{Kind: pc.ProjectScope, ProjectID: testID[i.Project](2)}
	store := &localStopStore{}
	project := &localStopProjects{store: store, cause: cause, project: scope.ProjectID}
	authority, err := NewAuthority(store)
	if err != nil {
		t.Fatal(err)
	}
	a := &serviceState{store: store, deps: Dependencies{Authority: authority, Projects: project}, calls: map[*call]struct{}{}, changed: make(chan struct{})}
	b := &secretServiceState{store: store, deps: SecretDependencies{Authority: authority, Projects: project}, calls: map[*call]struct{}{}, changed: make(chan struct{})}
	ordinary := &Service{data: func() *serviceState { return a }}
	secrets := &SecretService{data: func() *secretServiceState { return b }}
	stopper, err := NewProjectCallStopper(ordinary, secrets)
	if err != nil {
		t.Fatal(err)
	}
	registration, _ := i.RegisterService(i.ProjectLifecycle)
	actorScope, _ := i.InProject(scope.ProjectID)
	actor, err := registration.Actor(cause.OperationID.String(), actorScope)
	if err != nil {
		t.Fatal(err)
	}
	return stopper, ordinary, secrets, store, project, actor, cause, scope
}

func TestProjectVariableStopSelectionAndActualReturn(t *testing.T) {
	for _, action := range []pc.LifecycleAction{pc.Archive, pc.Delete} {
		t.Run(string(action), func(t *testing.T) {
			stopper, a, b, _, _, actor, cause, scope := localStopFixture(t, action)
			var runs []context.Context
			var returns []func()
			for _, begin := range []func(context.Context, i.ProjectID, callKind) (context.Context, *call, func(), error){a.beginProject, b.beginProject} {
				for _, input := range []struct {
					project i.ProjectID
					kind    callKind
				}{{scope.ProjectID, mutationCall}, {scope.ProjectID, readCall}, {testID[i.Project](3), mutationCall}} {
					ctx, _, done, err := begin(context.Background(), input.project, input.kind)
					if err != nil {
						t.Fatal(err)
					}
					t.Cleanup(done)
					runs = append(runs, ctx)
					returns = append(returns, done)
				}
			}
			want := 2
			if action == pc.Delete {
				want = 4
			}
			r, err := stopper.InspectStop(context.Background(), actor, cause, scope)
			if err != nil || r.Details().PendingCalls != want {
				t.Fatal("inspection", err)
			}
			for _, ctx := range runs {
				if ctx.Err() != nil {
					t.Fatal("inspection canceled call")
				}
			}
			r, err = stopper.RequestStop(context.Background(), actor, cause, scope)
			if err != nil || !r.Matches(cause, scope) || r.Details().PendingCalls != want || r.Details().LocalJoined {
				t.Fatal("stop mistook cancellation for return", err)
			}
			for index, ctx := range runs {
				wantCancel := index%3 == 0 || action == pc.Delete && index%3 == 1
				if (ctx.Err() == context.Canceled) != wantCancel {
					t.Fatalf("call %d scope selection", index)
				}
				if wantCancel {
					returns[index]()
				}
			}
			r, err = stopper.InspectStop(context.Background(), actor, cause, scope)
			if err != nil || !r.Details().LocalJoined || r.Details().PendingCalls != 0 {
				t.Fatal("actual return not observed", err)
			}
		})
	}
}

func TestProjectVariableStopRequiresConfirmedExactGate(t *testing.T) {
	for _, mode := range []string{"unknown", "rollback", "denied", "version", "wrong-actor", "caller-cancel"} {
		t.Run(mode, func(t *testing.T) {
			stopper, a, _, store, project, actor, cause, scope := localStopFixture(t, pc.Archive)
			ctx, entry, done, err := a.beginProject(context.Background(), scope.ProjectID, mutationCall)
			if err != nil {
				t.Fatal(err)
			}
			defer done()
			caller, cancel := context.WithCancel(context.Background())
			defer cancel()
			switch mode {
			case "unknown":
				store.result = f.Unknown
			case "rollback":
				store.result = f.NotCommitted
			case "denied":
				project.err = fault(f.Forbidden)
			case "version":
				cause.ProjectVersion++
			case "wrong-actor":
				actor, _ = i.NewHuman(testID[i.User](7), testID[i.Session](8))
			case "caller-cancel":
				store.after = func(context.Context) { cancel() }
			}
			r, err := stopper.RequestStop(caller, actor, cause, scope)
			if err == nil || r.Valid() || ctx.Err() != nil || entry.stopRequested {
				t.Fatal("unconfirmed stop affected original call")
			}
			if mode == "unknown" {
				code(t, err, f.CommitUnknown)
			}
			if mode == "wrong-actor" && store.entered != 0 {
				t.Fatal("wrong actor entered Tx")
			}
		})
	}
}

func TestProjectVariableStopDoesNotReplaceCapturedCall(t *testing.T) {
	stopper, a, _, store, _, actor, cause, scope := localStopFixture(t, pc.Archive)
	_, _, oldDone, err := a.beginProject(context.Background(), scope.ProjectID, mutationCall)
	if err != nil {
		t.Fatal(err)
	}
	defer oldDone()
	var fresh context.Context
	store.after = func(context.Context) {
		oldDone()
		var done func()
		fresh, _, done, err = a.beginProject(context.Background(), scope.ProjectID, mutationCall)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(done)
	}
	r, err := stopper.RequestStop(context.Background(), actor, cause, scope)
	if err != nil || fresh.Err() != nil || r.Details().PendingCalls != 1 || r.Details().LocalJoined {
		t.Fatal("replacement canceled or overlooked", err)
	}
}

func TestProjectVariableStopControlBelongsToBothServiceDrains(t *testing.T) {
	stopper, a, b, store, _, actor, cause, scope := localStopFixture(t, pc.Delete)
	store.after = func(ctx context.Context) {
		a.Stop()
		b.Stop()
		if ctx.Err() != context.Canceled {
			t.Fatal("control context not owned")
		}
		bounded, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
		defer cancel()
		if a.Drain(bounded) != context.DeadlineExceeded || b.Drain(bounded) != context.DeadlineExceeded {
			t.Fatal("Drain overtook control Tx")
		}
	}
	r, err := stopper.RequestStop(context.Background(), actor, cause, scope)
	if err == nil || r.Valid() {
		t.Fatal("canceled control issued report")
	}
	if a.Drain(context.Background()) != nil || b.Drain(context.Background()) != nil {
		t.Fatal("returned control still registered")
	}
}

func TestProjectVariableStopConstructionAndReportBoundary(t *testing.T) {
	_, a, b, _, _, _, cause, scope := localStopFixture(t, pc.Archive)
	if _, err := NewProjectCallStopper(nil, b); err == nil {
		t.Fatal("nil ordinary")
	}
	if _, err := NewProjectCallStopper(a, &SecretService{}); err == nil {
		t.Fatal("zero Secret")
	}
	original := b.state().store
	b.state().store = &localStopStore{}
	if _, err := NewProjectCallStopper(a, b); err == nil {
		t.Fatal("foreign Store")
	}
	b.state().store = original
	b.state().deps.Authority, _ = NewAuthority(original)
	if _, err := NewProjectCallStopper(a, b); err == nil {
		t.Fatal("second facts authority")
	}
	b.state().deps.Authority = a.state().deps.Authority
	b.state().deps.Projects = &localStopProjects{}
	if _, err := NewProjectCallStopper(a, b); err == nil {
		t.Fatal("second Project authority")
	}
	var zero ProjectCallStopReport
	if zero.Valid() || zero.Matches(cause, scope) || zero.Details().LocalJoined {
		t.Fatal("zero report grants join")
	}
}

type localConfirmationStore struct {
	Store
	entered chan context.Context
	release chan struct{}
}

func (s *localConfirmationStore) WithinTx(ctx context.Context, _ f.TransactionCause, _ func(context.Context, f.Tx) error) f.CommitResult {
	s.entered <- ctx
	<-s.release
	return f.NotCommittedResult(f.NewFault(f.DependencyUnavailable, f.NotCommitted))
}

func TestProjectVariableStopConfirmationRegistrationOrders(t *testing.T) {
	for _, secret := range []bool{false, true} {
		for _, stopFirst := range []bool{false, true} {
			name := "ordinary"
			if secret {
				name = "secret"
			}
			if stopFirst {
				name += "/stop-first"
			} else {
				name += "/confirmation-first"
			}
			t.Run(name, func(t *testing.T) {
				store := &localConfirmationStore{entered: make(chan context.Context, 1), release: make(chan struct{})}
				stopper, ordinary, secrets, _, _, _, _, scope := localStopFixture(t, pc.Archive)
				_ = stopper // Public RequestStop selection is covered above; this isolates the registration race.
				ordinary.state().store = store
				secrets.state().store = store
				begin := ordinary.beginProject
				mu, calls := &ordinary.state().mu, ordinary.state().calls
				if secret {
					begin = secrets.beginProject
					mu, calls = &secrets.state().mu, secrets.state().calls
				}
				run, entry, done, err := begin(context.Background(), scope.ProjectID, mutationCall)
				if err != nil {
					t.Fatal(err)
				}
				cancelOriginal := func() { mu.Lock(); defer mu.Unlock(); finishProjectStop(calls, []*call{entry}, true) }
				if stopFirst {
					cancelOriginal()
				}
				returned := make(chan error, 1)
				var original f.CommitResult
				var confirm func() error
				if secret {
					_, _, d04, request, saved := secretConfirmationFixture(t)
					original = saved
					confirm = func() error {
						_, err := secrets.confirmSecretUnknown(run, entry, request, d04.prepared, original)
						return err
					}
				} else {
					r, actor := runtimeRecord(t)
					q, err := c.NewVariableCommandLookupRequest(c.VariableCommandLookupFields{ProjectID: r.Project, Command: r.Command, IdempotencyKey: r.Key, SemanticDigest: r.Semantic})
					if err != nil {
						t.Fatal(err)
					}
					identity, _ := c.VariableCommandIdentity(r.Project, r.Command, r.Key)
					original = f.UnknownResult(testID[f.TransactionAttempt](99), commandCause(identity))
					confirm = func() error { _, err := ordinary.confirmUnknown(run, entry, actor, q, original); return err }
				}
				go func() { err := confirm(); done(); returned <- err }()
				var releaseOnce sync.Once
				release := func() { releaseOnce.Do(func() { close(store.release) }) }
				joined := false
				defer func() {
					release()
					if !joined {
						<-returned
					}
				}()
				var confirmation context.Context
				select {
				case confirmation = <-store.entered:
				case <-time.After(time.Second):
					t.Fatal("confirmation did not reach original Tx port")
				}
				if !stopFirst && confirmation.Err() != nil {
					t.Fatal("unselected confirmation canceled")
				}
				if !stopFirst {
					cancelOriginal()
				}
				if run.Err() != context.Canceled || confirmation.Err() != context.Canceled {
					t.Fatal("late or registered confirmation escaped cancellation")
				}
				mu.Lock()
				_, stillOwned := calls[entry]
				registered := len(entry.confirmations)
				mu.Unlock()
				if !stillOwned || registered != 1 {
					t.Fatal("held original confirmation retired early")
				}
				// Replace the cleanup with an explicit join so the original Unknown
				// and removal of the exact original call/token are both checked.
				release()
				err = <-returned
				joined = true
				requireOriginalSecretUnknown(t, err, original)
				mu.Lock()
				remaining, tokens := len(calls), len(entry.confirmations)
				mu.Unlock()
				if remaining != 0 || tokens != 0 {
					t.Fatal("original call or confirmation not retired")
				}
			})
		}
	}
}
