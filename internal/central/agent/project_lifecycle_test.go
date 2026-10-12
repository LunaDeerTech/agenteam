package agent

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	c "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	rc "github.com/LunaDeerTech/agenteam/internal/central/runner/contract"
)

// These controlled Store/Project boundaries exercise the actual Agent owners
// and public local-stop provider. They do not establish a PG lifecycle grant,
// successful business SQL, or foreign-process/whole-project completion.
type agentStopBusinessKey struct{}
type agentStopStore struct {
	Store
	mu       sync.Mutex
	live     map[f.Tx][]f.LockRequest
	controls int
	state    f.CommitState
	after    func(context.Context)
	business func(context.Context, f.TransactionCause) f.CommitResult
}

func (s *agentStopStore) WithinTx(ctx context.Context, cause f.TransactionCause, fn func(context.Context, f.Tx) error) f.CommitResult {
	if ctx.Value(agentStopBusinessKey{}) != nil {
		return s.business(ctx, cause)
	}
	tx := f.NewTx()
	s.mu.Lock()
	s.controls++
	s.live[tx] = nil
	s.mu.Unlock()
	defer func() { s.mu.Lock(); delete(s.live, tx); s.mu.Unlock() }()
	if err := fn(ctx, tx); err != nil {
		var problem *f.Fault
		if !errors.As(err, &problem) {
			problem = f.NewFault(f.InternalError, f.NotCommitted).WithCause(err)
		}
		return f.NotCommittedResult(problem)
	}
	if s.after != nil {
		s.after(ctx)
	}
	switch s.state {
	case f.Unknown:
		attempt, _ := f.ParseID[f.TransactionAttempt]("01900000-0000-7000-8000-000000000099")
		return f.UnknownResult(attempt, cause)
	case f.NotCommitted:
		return f.NotCommittedResult(f.NewFault(f.DependencyUnavailable, f.NotCommitted))
	default:
		return f.CommittedResult()
	}
}
func (s *agentStopStore) InTx(tx f.Tx) (postgres.SQLExecutor, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.live[tx]; !ok || !tx.Valid() {
		return nil, fault(f.Forbidden)
	}
	return s, nil
}
func (s *agentStopStore) AcquireAll(ctx context.Context, tx f.Tx, locks []f.LockRequest) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.live[tx]; !ok || !tx.Valid() {
		return fault(f.Forbidden)
	}
	s.live[tx] = append([]f.LockRequest(nil), locks...)
	return ctx.Err()
}

type agentStopProjects struct {
	pc.ProjectAuthority
	store   *agentStopStore
	cause   pc.LifecycleCause
	project i.ProjectID
	err     error
}

func (p *agentStopProjects) ValidateLifecycleInTx(ctx context.Context, tx f.Tx, actor i.Actor, cause pc.LifecycleCause, participant pc.ParticipantName, phase pc.OperationPhase) error {
	p.store.mu.Lock()
	locks, live := p.store.live[tx]
	p.store.mu.Unlock()
	if !live || !tx.Valid() || cause != p.cause || actor.Details().ProjectID != p.project.String() ||
		participant != pc.SkillsParticipant || phase != pc.StopPhase || len(locks) != 1 ||
		locks[0].Mode != f.Shared || f.CompareLockKeys(locks[0].Key, projectLock(p.project, f.Shared).Key) != 0 {
		return fault(f.Forbidden)
	}
	if p.err != nil {
		return p.err
	}
	return ctx.Err()
}

type agentStopFixture struct {
	stopper   *ProjectCallStopper
	service   *Service
	directory *DirectoryReader
	store     *agentStopStore
	projects  *agentStopProjects
	actor     i.Actor
	cause     pc.LifecycleCause
	scope     pc.ScopeRef
}

func newAgentStopFixture(t *testing.T, action pc.LifecycleAction) *agentStopFixture {
	t.Helper()
	in, _ := planFixture(t)
	cause := pc.LifecycleCause{OperationID: commandID[pc.Operation](t, "01900000-0000-7000-8000-000000000090"), Action: action, ProjectVersion: 7}
	scope := pc.ScopeRef{Kind: pc.ProjectScope, ProjectID: in.project}
	store := &agentStopStore{live: make(map[f.Tx][]f.LockRequest)}
	projects := &agentStopProjects{store: store, cause: cause, project: scope.ProjectID}
	authority, err := NewAuthority(store, projects)
	if err != nil {
		t.Fatal(err)
	}
	service := &Service{state: &serviceState{store: store, deps: Dependencies{Authority: authority}, calls: newCalls()}}
	directory, err := NewDirectoryReader(store, authority, directoryKeys(t))
	if err != nil {
		t.Fatal(err)
	}
	stopper, err := NewProjectCallStopper(service, directory)
	if err != nil {
		t.Fatal(err)
	}
	registration, err := i.RegisterService(i.ProjectLifecycle)
	if err != nil {
		t.Fatal(err)
	}
	actorScope, err := i.InProject(scope.ProjectID)
	if err != nil {
		t.Fatal(err)
	}
	actor, err := registration.Actor(cause.OperationID.String(), actorScope)
	if err != nil {
		t.Fatal(err)
	}
	x := &agentStopFixture{stopper, service, directory, store, projects, actor, cause, scope}
	t.Cleanup(func() {
		service.Stop()
		directory.Stop()
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := service.Drain(ctx); err != nil {
			t.Error("service cleanup did not join", err)
		}
		if err := directory.Drain(ctx); err != nil {
			t.Error("directory cleanup did not join", err)
		}
	})
	return x
}
func agentStopCall(t *testing.T, calls *callSet, project i.ProjectID, kind callKind) (context.Context, *ownedCall, func()) {
	t.Helper()
	ctx, entry, done, err := calls.beginProject(context.Background(), project, kind)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(done)
	return ctx, entry, done
}
func agentStopMarked(calls *callSet, entry *ownedCall) bool {
	calls.mu.Lock()
	defer calls.mu.Unlock()
	return entry.stopRequested
}
func agentStopPending(t *testing.T, x *agentStopFixture, want int) {
	t.Helper()
	r, err := x.stopper.InspectStop(context.Background(), x.actor, x.cause, x.scope)
	if err != nil || !r.Matches(x.cause, x.scope) || r.Details().PendingCalls != want || r.Details().LocalJoined != (want == 0) {
		t.Fatalf("pending: want %d, got %+v, error %v", want, r.Details(), err)
	}
}
func agentStopWait[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case v := <-ch:
		return v
	case <-time.After(5 * time.Second):
		t.Fatal("original call did not reach/return from its barrier")
		var zero T
		return zero
	}
}
func agentStopRelease(ch chan struct{}) func() {
	var once sync.Once
	return func() { once.Do(func() { close(ch) }) }
}

func TestAgentProjectStopBindsActualOwners(t *testing.T) {
	for _, mode := range []string{"same-owner", "nil-service", "zero-service", "nil-directory", "zero-directory", "foreign-store", "foreign-authority", "authority-store", "missing-project-authority"} {
		t.Run(mode, func(t *testing.T) {
			x := newAgentStopFixture(t, pc.Archive)
			a, b := x.service, x.directory
			switch mode {
			case "nil-service":
				a = nil
			case "zero-service":
				a = &Service{}
			case "nil-directory":
				b = nil
			case "zero-directory":
				b = &DirectoryReader{}
			case "foreign-store":
				b.state.store = &agentStopStore{}
			case "foreign-authority":
				b.state.authority, _ = NewAuthority(x.store, x.projects)
			case "authority-store":
				a.state.deps.Authority.state.store = &agentStopStore{}
			case "missing-project-authority":
				a.state.deps.Authority.state.projects = nil
			}
			stopper, err := NewProjectCallStopper(a, b)
			if mode == "same-owner" {
				if err != nil || stopper == nil {
					t.Fatal("same actual owner rejected", err)
				}
			} else if err == nil || stopper != nil {
				t.Fatal("unbound owner accepted")
			}
			if x.store.controls != 0 {
				t.Fatal("constructor performed I/O")
			}
		})
	}
}

func TestAgentProjectStopRequiresCommittedExactGate(t *testing.T) {
	for _, mode := range []string{"unknown", "rollback", "denied", "version", "wrong-actor", "wrong-project", "wrong-operation", "caller-cancel"} {
		t.Run(mode, func(t *testing.T) {
			x := newAgentStopFixture(t, pc.Archive)
			ctx, entry, _ := agentStopCall(t, x.service.state.calls, x.scope.ProjectID, mutationCall)
			caller, cancel := context.WithCancel(context.Background())
			defer cancel()
			actor, cause, scope := x.actor, x.cause, x.scope
			switch mode {
			case "unknown":
				x.store.state = f.Unknown
			case "rollback":
				x.store.state = f.NotCommitted
			case "denied":
				x.projects.err = fault(f.Forbidden)
			case "version":
				cause.ProjectVersion++
			case "wrong-actor":
				in, _ := planFixture(t)
				actor = in.actor
			case "wrong-project":
				scope.ProjectID = commandID[i.Project](t, "01900000-0000-7000-8000-000000000091")
			case "wrong-operation":
				cause.OperationID = commandID[pc.Operation](t, "01900000-0000-7000-8000-000000000092")
			case "caller-cancel":
				x.store.after = func(context.Context) { cancel() }
			}
			r, err := x.stopper.RequestStop(caller, actor, cause, scope)
			if err == nil || r.Valid() || ctx.Err() != nil || agentStopMarked(x.service.state.calls, entry) {
				t.Fatal("unconfirmed/mismatched gate marked or canceled original call")
			}
			if mode == "unknown" {
				requireCode(t, err, f.CommitUnknown)
			}
			if (mode == "wrong-actor" || mode == "wrong-project" || mode == "wrong-operation") && x.store.controls != 0 {
				t.Fatal("invalid service identity reached Store")
			}
		})
	}
}

func TestAgentProjectStopWaitsForCommittedReturn(t *testing.T) {
	x := newAgentStopFixture(t, pc.Archive)
	ctx, entry, _ := agentStopCall(t, x.service.state.calls, x.scope.ProjectID, mutationCall)
	entered, release := make(chan struct{}), make(chan struct{})
	unblock := agentStopRelease(release)
	x.store.after = func(context.Context) { close(entered); <-release }
	type result struct {
		report ProjectCallStopReport
		err    error
	}
	finished := make(chan result, 1)
	go func() {
		r, err := x.stopper.RequestStop(context.Background(), x.actor, x.cause, x.scope)
		finished <- result{r, err}
	}()
	returned := false
	t.Cleanup(func() {
		unblock()
		if !returned {
			agentStopWait(t, finished)
		}
	})
	agentStopWait(t, entered)
	if ctx.Err() != nil || agentStopMarked(x.service.state.calls, entry) {
		t.Fatal("callback completion acted as actual Committed return")
	}
	select {
	case <-finished:
		returned = true
		t.Fatal("provider overtook original Store return")
	default:
	}
	unblock()
	out := agentStopWait(t, finished)
	returned = true
	if out.err != nil || !out.report.Valid() || out.report.Details().PendingCalls != 1 || out.report.Details().LocalJoined || ctx.Err() == nil || !agentStopMarked(x.service.state.calls, entry) {
		t.Fatal("actual commit did not request cancellation without false join", out.err)
	}
}

func TestAgentProjectStopSelectionAndActualReturn(t *testing.T) {
	for _, action := range []pc.LifecycleAction{pc.Archive, pc.Delete} {
		t.Run(string(action), func(t *testing.T) {
			x := newAgentStopFixture(t, action)
			foreign := commandID[i.Project](t, "01900000-0000-7000-8000-000000000093")
			type run struct {
				ctx      context.Context
				done     func()
				selected bool
			}
			var runs []run
			for _, input := range []struct {
				calls    *callSet
				project  i.ProjectID
				kind     callKind
				selected bool
			}{
				{x.service.state.calls, x.scope.ProjectID, mutationCall, true},
				{x.service.state.calls, x.scope.ProjectID, readCall, action == pc.Delete},
				{x.directory.state.calls, x.scope.ProjectID, readCall, action == pc.Delete},
				{x.service.state.calls, foreign, mutationCall, false},
				{x.directory.state.calls, foreign, readCall, false},
				{x.service.state.calls, x.scope.ProjectID, controlCall, false},
				{x.directory.state.calls, x.scope.ProjectID, controlCall, false},
			} {
				ctx, _, done := agentStopCall(t, input.calls, input.project, input.kind)
				runs = append(runs, run{ctx, done, input.selected})
			}
			want := 1
			if action == pc.Delete {
				want = 3
			}
			agentStopPending(t, x, want)
			for _, r := range runs {
				if r.ctx.Err() != nil {
					t.Fatal("Inspect canceled a call")
				}
			}
			r, err := x.stopper.RequestStop(context.Background(), x.actor, x.cause, x.scope)
			if err != nil || !r.Matches(x.cause, x.scope) || r.Details().PendingCalls != want || r.Details().LocalJoined {
				t.Fatal("selection or cancellation/return boundary", err)
			}
			for _, call := range runs {
				if (call.ctx.Err() != nil) != call.selected {
					t.Fatal("stop crossed project/class")
				}
				if call.selected {
					call.done()
				}
			}
			agentStopPending(t, x, 0)
			var forged ProjectCallStopReport
			if forged.Valid() || json.Unmarshal([]byte(`{}`), &forged) == nil || forged.Valid() {
				t.Fatal("public JSON manufactured local stop proof")
			}
		})
	}
}

func TestAgentProjectStopKeepsCapturedIdentity(t *testing.T) {
	x := newAgentStopFixture(t, pc.Archive)
	_, _, oldDone := agentStopCall(t, x.service.state.calls, x.scope.ProjectID, mutationCall)
	var fresh context.Context
	var freshEntry *ownedCall
	x.store.after = func(context.Context) {
		oldDone()
		fresh, freshEntry, _ = agentStopCall(t, x.service.state.calls, x.scope.ProjectID, mutationCall)
	}
	r, err := x.stopper.RequestStop(context.Background(), x.actor, x.cause, x.scope)
	if err != nil || !r.Valid() || r.Details().PendingCalls != 1 || r.Details().LocalJoined || fresh.Err() != nil || agentStopMarked(x.service.state.calls, freshEntry) {
		t.Fatal("fresh call was retrospectively canceled or omitted from pending", err)
	}
	x.store.after = nil
	agentStopPending(t, x, 1)
}

func TestAgentProjectStopControlWaitsForBothOwners(t *testing.T) {
	for _, mode := range []string{"held-control", "directory-already-stopped"} {
		t.Run(mode, func(t *testing.T) {
			x := newAgentStopFixture(t, pc.Delete)
			if mode == "directory-already-stopped" {
				x.directory.Stop()
				r, err := x.stopper.RequestStop(context.Background(), x.actor, x.cause, x.scope)
				if err == nil || r.Valid() || x.store.controls != 0 {
					t.Fatal("partial control admission reached Store")
				}
				x.service.Stop()
				if !x.service.Joined() || !x.directory.Joined() {
					t.Fatal("partial admission leaked first owner")
				}
				return
			}
			entered, release := make(chan context.Context, 1), make(chan struct{})
			unblock := agentStopRelease(release)
			x.store.after = func(ctx context.Context) { entered <- ctx; <-release }
			finished := make(chan error, 1)
			go func() {
				r, err := x.stopper.InspectStop(context.Background(), x.actor, x.cause, x.scope)
				if r.Valid() {
					err = errors.New("canceled control published a report")
				}
				finished <- err
			}()
			returned := false
			t.Cleanup(func() {
				unblock()
				if !returned {
					agentStopWait(t, finished)
				}
			})
			ctx := agentStopWait(t, entered)
			x.service.Stop()
			x.directory.Stop()
			if ctx.Err() == nil || x.service.Joined() || x.directory.Joined() {
				t.Fatal("control lost actual owner registration")
			}
			canceled, cancel := context.WithCancel(context.Background())
			cancel()
			if !errors.Is(x.service.Drain(canceled), context.Canceled) || !errors.Is(x.directory.Drain(canceled), context.Canceled) {
				t.Fatal("Drain overtook original control Store return")
			}
			unblock()
			err := agentStopWait(t, finished)
			returned = true
			if err == nil || !x.service.Joined() || !x.directory.Joined() {
				t.Fatal("actual control return not retained through both owners", err)
			}
		})
	}
}

func TestAgentProjectStopKeepsUnknownConfirmationOwned(t *testing.T) {
	for _, order := range []string{"stop-before-confirmation", "confirmation-before-stop"} {
		t.Run(order, func(t *testing.T) {
			x := newAgentStopFixture(t, pc.Archive)
			in, _ := planFixture(t)
			cause, err := f.NewCommandsCause(in.identity)
			if err != nil {
				t.Fatal(err)
			}
			attempt := commandID[f.TransactionAttempt](t, "01900000-0000-7000-8000-000000000094")
			original := f.UnknownResult(attempt, cause)
			firstEntered, firstRelease := make(chan struct{}), make(chan struct{})
			confirmEntered, confirmRelease := make(chan context.Context, 1), make(chan struct{})
			firstDone, confirmDone := agentStopRelease(firstRelease), agentStopRelease(confirmRelease)
			caller, cancel := context.WithCancel(context.WithValue(context.Background(), agentStopBusinessKey{}, true))
			defer cancel()
			var transactions atomic.Int32
			x.store.business = func(ctx context.Context, _ f.TransactionCause) f.CommitResult {
				switch transactions.Add(1) {
				case 1:
					close(firstEntered)
					<-firstRelease
					cancel()
					return original
				case 2:
					confirmEntered <- ctx
					<-confirmRelease
					return f.NotCommittedResult(f.NewFault(f.DependencyUnavailable, f.NotCommitted).WithCause(ctx.Err()))
				default:
					return f.NotCommittedResult(f.NewFault(f.InternalError, f.NotCommitted))
				}
			}
			finished := make(chan error, 1)
			go func() {
				_, err := x.service.CreateAgent(caller, in.actor, in.meta, in.project, *in.create)
				finished <- err
			}()
			returned := false
			t.Cleanup(func() {
				x.service.Stop()
				firstDone()
				confirmDone()
				if !returned {
					agentStopWait(t, finished)
				}
			})
			agentStopWait(t, firstEntered)
			agentStopPending(t, x, 1)
			var confirmation context.Context
			if order == "confirmation-before-stop" {
				firstDone()
				confirmation = agentStopWait(t, confirmEntered)
				deadline, ok := confirmation.Deadline()
				if caller.Err() == nil || confirmation.Err() != nil || !ok || time.Until(deadline) > 3*time.Second {
					t.Fatal("confirmation lost independent bounded context")
				}
				agentStopPending(t, x, 1) // One original mutation, not a detached second call.
			}
			r, err := x.stopper.RequestStop(context.Background(), x.actor, x.cause, x.scope)
			if err != nil || r.Details().PendingCalls != 1 || r.Details().LocalJoined {
				t.Fatal("stop lost held mutation", err)
			}
			if confirmation != nil && confirmation.Err() == nil {
				t.Fatal("already admitted confirmation escaped stop")
			}
			agentStopPending(t, x, 1)
			// Do not globally Stop yet: that would mask a confirmation which
			// escaped the original Project stop by registering after its snapshot.
			canceled, stop := context.WithCancel(context.Background())
			stop()
			if !errors.Is(x.service.Drain(canceled), context.Canceled) || x.service.Joined() {
				t.Fatal("drain overtook original mutation/confirmation")
			}
			firstDone()
			confirmDone()
			err = agentStopWait(t, finished)
			returned = true
			retained, ok := UnknownAttempt(err)
			if !ok || retained.AttemptID() != attempt || retained.Cause().Details().Primary.Canonical() != in.identity.Canonical() {
				t.Fatal("lifecycle stop replaced original Unknown", err)
			}
			want := int32(1)
			if order == "confirmation-before-stop" {
				want = 2
			}
			agentStopPending(t, x, 0)
			x.service.Stop()
			if transactions.Load() != want || !x.service.Joined() {
				t.Fatal("confirmation escaped parent or original mutation did not join")
			}
		})
	}
}

type agentStopRunnerDirectory struct{ c.RunnerCandidateDirectory }

func (agentStopRunnerDirectory) ConfigurationCandidateLocks([]rc.RunnerID) ([]f.LockRequest, error) {
	return nil, nil
}

type agentStopMounts struct {
	c.MountConfiguration
	c.RunnerMountBindingsReader
}

func TestAgentProjectStopTracksPublicReadEntries(t *testing.T) {
	for _, name := range []string{"configuration", "command-lookup", "available-runners", "runner-mounts", "directory-get", "directory-list"} {
		t.Run(name, func(t *testing.T) {
			x := newAgentStopFixture(t, pc.Delete)
			in, _ := planFixture(t)
			x.service.state.deps.RunnerCandidates = agentStopRunnerDirectory{}
			x.service.state.deps.RunnerCursorKeys = directoryKeys(t)
			x.service.state.deps.Mounts = agentStopMounts{}
			entered, release := make(chan context.Context, 1), make(chan struct{})
			unblock := agentStopRelease(release)
			x.store.business = func(ctx context.Context, _ f.TransactionCause) f.CommitResult {
				entered <- ctx
				<-release
				return f.NotCommittedResult(f.NewFault(f.DependencyUnavailable, f.NotCommitted).WithCause(ctx.Err()))
			}
			ctx := context.WithValue(context.Background(), agentStopBusinessKey{}, true)
			finished := make(chan error, 1)
			go func() {
				var err error
				switch name {
				case "configuration":
					_, err = x.service.GetAgent(ctx, in.actor, in.project, in.target)
				case "command-lookup":
					_, err = x.service.LookupAgentCommand(ctx, in.actor, in.project, in.name, in.meta.IdempotencyKey, in.semantic)
				case "available-runners":
					_, err = x.service.ListAvailableRunners(ctx, in.actor, in.project, f.PageRequest{Limit: 1})
				case "runner-mounts":
					_, err = x.service.GetRunnerMounts(ctx, in.actor, in.project, in.target)
				case "directory-get":
					_, err = x.directory.GetAgent(ctx, in.actor, in.project, in.target)
				case "directory-list":
					_, err = x.directory.ListAgents(ctx, in.actor, in.project, f.PageRequest{Limit: 1})
				}
				finished <- err
			}()
			returned := false
			t.Cleanup(func() {
				unblock()
				if !returned {
					agentStopWait(t, finished)
				}
			})
			original := agentStopWait(t, entered)
			agentStopPending(t, x, 1)
			if original.Err() != nil {
				t.Fatal("Inspect canceled public read")
			}
			r, err := x.stopper.RequestStop(context.Background(), x.actor, x.cause, x.scope)
			if err != nil || r.Details().PendingCalls != 1 || r.Details().LocalJoined || original.Err() == nil {
				t.Fatal("public read missing from precise stop", err)
			}
			agentStopPending(t, x, 1)
			unblock()
			err = agentStopWait(t, finished)
			returned = true
			if err == nil {
				t.Fatal("controlled canceled read returned success")
			}
			agentStopPending(t, x, 0)
		})
	}
}
