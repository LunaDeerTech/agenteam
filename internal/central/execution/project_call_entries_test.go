package execution

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	ac "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/agentloop"
	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	outbox "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	pvc "github.com/LunaDeerTech/agenteam/internal/central/projectvariable/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/skill/contract"
)

// These controls use the actual fixed factory and public entry methods. The
// original registration Store refuses before invoking its callback, so none
// of these tests claims successful SQL, Project permission or a Model session.
type executionEntryStore struct {
	Store
	mu      sync.Mutex
	records []projectCallRecord
	methods []string
	causes  []f.TransactionCause
	hold    func(context.Context, projectCallRecord)
}

func (s *executionEntryStore) WithinTx(ctx context.Context, cause f.TransactionCause, _ func(context.Context, f.Tx) error) f.CommitResult {
	w, ok := ctx.Value(projectCallContextKey{}).(projectCallWitness)
	var r projectCallRecord
	var method string
	if ok && w.entry != nil {
		r, method = w.entry.record, w.entry.identity.Command()
	}
	s.mu.Lock()
	s.records, s.methods, s.causes = append(s.records, r), append(s.methods, method), append(s.causes, cause)
	hold := s.hold
	s.mu.Unlock()
	if hold != nil {
		hold(ctx, r)
	}
	return f.NotCommittedResult(f.NewFault(f.Forbidden, f.NotCommitted))
}

func (s *executionEntryStore) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.records)
}

type executionEntryProjects struct {
	pc.ProjectAuthority
	ec.PreparationProjectGate
}
type executionEntryStopAuthority struct{ ec.ProjectStopAuthority }
type executionEntryProof struct{}

func (executionEntryProof) RequireCurrentProcessInTx(context.Context, f.Tx, oc.ProcessID) error {
	return fault(f.Forbidden)
}
func (executionEntryProof) ConfirmStopped(context.Context, oc.ProcessID) error {
	return fault(f.Forbidden)
}

func newExecutionEntryPair(t *testing.T) (*ProjectCallOwnership, *executionEntryStore) {
	t.Helper()
	store := &executionEntryStore{}
	projects := &executionEntryProjects{}
	authority, err := NewAuthority(store, projects, nil)
	if err != nil {
		t.Fatal(err)
	}
	process := newTestID[oc.Process](t)
	processes := &preparationProcesses{process: outbox.ProcessID(process.String())}
	agents := &struct{ ac.ExecutionConfiguration }{}
	catalog := event.NewCatalog()
	lifecycle, err := ec.RegisterDirectTextEvents(catalog)
	if err != nil {
		t.Fatal(err)
	}
	if err = catalog.Seal(); err != nil {
		t.Fatal(err)
	}
	contextBuilder, err := NewContextBuilder(&struct{ ec.TriggerContextBuilder }{})
	if err != nil {
		t.Fatal(err)
	}
	deps := ProjectCallDependencies{
		Service:     Dependencies{Authority: authority, Agents: agents},
		Preparation: PreparationDependencies{Agents: agents, Projects: projects, Processes: processes},
		Direct: DirectTextDependencies{Context: contextBuilder, Loop: &agentloop.DirectTextController{}, Processes: processes,
			Projects: projects, Agents: agents, Skills: &struct{ sc.InitialRoundBindingsReader }{}, Events: &struct{ outbox.Appender }{},
			Lifecycle: lifecycle, Models: &struct{ mc.ExecutionModelRetirement }{}, Environment: &struct{ pvc.EnvironmentRetirement }{}},
		Associated: AssociatedExecutorOptions{MaxOwned: 4, RecoveryInterval: time.Hour},
	}
	pair, err := newProjectCallOwnership(store, deps, executionEntryProof{}, process, &executionEntryStopAuthority{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		pair.Stop()
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := pair.Drain(ctx); err != nil || !pair.Joined() {
			t.Error("original entry ownership did not join", err)
		}
	})
	return pair, store
}

func executionEntryRequest(t *testing.T) (i.Actor, ec.LaunchRequest) {
	t.Helper()
	actor, err := i.NewHuman(newTestID[i.User](t), newTestID[i.Session](t))
	if err != nil {
		t.Fatal(err)
	}
	r := ec.LaunchRequest{ProjectID: newTestID[i.Project](t), AgentID: newTestID[i.Agent](t),
		Trigger: ec.Trigger{Kind: "task", TaskID: newTestID[struct{}](t).String()}, Purpose: "task/work",
		Policy: ec.Policy{SchemaVersion: 1, DeniedToolIDs: []i.ToolID{}, AllowedResourceConstraints: []json.RawMessage{}},
		Meta:   f.CommandMeta{RequestID: newTestID[f.Request](t), IdempotencyKey: "execution-entry"}}
	if err := r.Validate(); err != nil {
		t.Fatal(err)
	}
	return actor, r
}

func TestExecutionProjectEntriesRegisterBeforeServiceBusiness(t *testing.T) {
	for _, method := range []string{"launch", "lookup_launch"} {
		t.Run(method, func(t *testing.T) {
			pair, store := newExecutionEntryPair(t)
			actor, request := executionEntryRequest(t)
			var err error
			if method == "launch" {
				_, err = pair.Service().Launch(context.Background(), actor, request)
			} else {
				digest, digestErr := request.Digest()
				if digestErr != nil {
					t.Fatal(digestErr)
				}
				_, err = pair.Service().LookupLaunch(context.Background(), actor, ec.LaunchLookupKey{ProjectID: request.ProjectID, AgentID: request.AgentID, IdempotencyKey: request.Meta.IdempotencyKey}, digest)
			}
			requireCode(t, err, f.Forbidden)
			if store.count() != 1 || store.methods[0] != method || store.records[0].Project != request.ProjectID || store.records[0].Lane != projectLaneService || !store.records[0].valid() {
				t.Fatal("public entry did not reach exactly its original registration")
			}
			pair.state.mu.Lock()
			remaining := len(pair.state.owners)
			pair.state.mu.Unlock()
			if remaining != 0 {
				t.Fatal("known registration rejection left an original owner")
			}
		})
	}
}

func TestExecutionProjectEntriesRequireOriginalAssociatedScope(t *testing.T) {
	for _, method := range []string{"prepare", "resolve_preparation", "start", "resolve_direct"} {
		t.Run(method, func(t *testing.T) {
			pair, store := newExecutionEntryPair(t)
			execution := newTestID[i.Execution](t)
			var err error
			switch method {
			case "prepare":
				err = pair.Preparation().Run(context.Background(), execution)
			case "resolve_preparation":
				err = pair.Preparation().ResolveUnknown(context.Background(), execution)
			case "start":
				_, err = pair.Direct().Start(context.Background(), execution)
			case "resolve_direct":
				_, err = pair.Direct().ResolveUnknown(context.Background(), execution)
			}
			requireCode(t, err, f.DependencyUnbound)
			if store.count() != 0 {
				t.Fatal("unowned execution identity reached SQL")
			}
		})
	}
}

func TestExecutionProjectEntriesServiceStopRejectsFreshCalls(t *testing.T) {
	for _, method := range []string{"launch", "lookup_launch"} {
		t.Run(method, func(t *testing.T) {
			pair, store := newExecutionEntryPair(t)
			actor, request := executionEntryRequest(t)
			// Stop just this original Service. A global pair.Stop would mask
			// the contract of the retained public Service view.
			pair.Service().Stop()
			var err error
			if method == "launch" {
				_, err = pair.Service().Launch(context.Background(), actor, request)
			} else {
				digest, digestErr := request.Digest()
				if digestErr != nil {
					t.Fatal(digestErr)
				}
				_, err = pair.Service().LookupLaunch(context.Background(), actor, ec.LaunchLookupKey{ProjectID: request.ProjectID, AgentID: request.AgentID, IdempotencyKey: request.Meta.IdempotencyKey}, digest)
			}
			requireCode(t, err, f.ShuttingDown)
			if store.count() != 0 || !pair.Service().Joined() {
				t.Fatal("fresh stopped Service call registered or retained ownership")
			}
		})
	}
}

func TestExecutionProjectEntriesServiceWaitsForOriginalReturn(t *testing.T) {
	pair, store := newExecutionEntryPair(t)
	actor, request := executionEntryRequest(t)
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	defer once.Do(func() { close(release) })
	store.hold = func(context.Context, projectCallRecord) { close(entered); <-release }
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := pair.Service().Launch(ctx, actor, request); done <- err }()
	associatedTestReceive(t, entered)
	cancel()
	pair.Stop()
	if pair.Joined() || pair.Service().Joined() {
		t.Fatal("caller cancellation or Stop replaced original registration return")
	}
	select {
	case <-done:
		t.Fatal("public entry escaped its original Store")
	default:
	}
	once.Do(func() { close(release) })
	requireCode(t, associatedTestReceive(t, done), f.Forbidden)
	if !pair.Service().Joined() {
		t.Fatal("known rejection did not retire after original return")
	}
}

func TestExecutionProjectEntriesAssociatedOwnsAsyncRegistrationTail(t *testing.T) {
	pair, store := newExecutionEntryPair(t)
	project, dispatch := associatedTestIdentity(t)
	otherProject, otherDispatch := associatedTestIdentity(t)
	entered, release, otherReturned := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var once sync.Once
	defer once.Do(func() { close(release) })
	store.hold = func(_ context.Context, r projectCallRecord) {
		if r.Project == project {
			close(entered)
			<-release
		} else if r.Project == otherProject {
			close(otherReturned)
		}
	}
	runCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runDone := associatedTestRun(t, pair.Associated(), runCtx)
	if store.count() != 0 {
		t.Fatal("global lifetime invented a Project call")
	}
	if _, err := pair.Associated().Advance(context.Background(), project, dispatch); err != nil {
		t.Fatal("local Advance admission waited for registration", err)
	}
	associatedTestReceive(t, entered)
	if _, err := pair.Associated().Advance(context.Background(), otherProject, otherDispatch); err != nil {
		t.Fatal("Project A registration blocked B", err)
	}
	associatedTestReceive(t, otherReturned)
	cancel()
	pair.Stop()
	if pair.Joined() || pair.Associated().Joined() {
		t.Fatal("Advance return retired its still-active worker")
	}
	select {
	case <-runDone:
		t.Fatal("Run escaped original asynchronous worker return")
	default:
	}
	once.Do(func() { close(release) })
	_ = associatedTestReceive(t, runDone)
	if !pair.Associated().Joined() || store.count() != 2 {
		t.Fatal("original worker tails did not retire exactly two registrations")
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	for n, r := range store.records {
		if r.Lane != projectLaneAssociated || store.methods[n] != "advance" || !r.valid() {
			t.Fatal("asynchronous registration lost its original tuple")
		}
	}
}
