package scheduler

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/scheduler/contract"
	wc "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
)

// These controls reach the real fixed factory and public entrances but reject
// the original registration transaction before its callback. They do not claim
// successful SQL, Project authority, a Launch or Execution association.
type schedulerEntryStore struct {
	pendingTestStore
	records      []projectCallRecord
	causes       []f.TransactionCause
	beforeReturn func(context.Context)
}

func (s *schedulerEntryStore) WithinTx(ctx context.Context, cause f.TransactionCause, _ func(context.Context, f.Tx) error) f.CommitResult {
	s.causes = append(s.causes, cause)
	if witness, ok := ctx.Value(projectCallContextKey{}).(projectCallWitness); ok && witness.entry != nil {
		s.records = append(s.records, witness.entry.record)
	}
	if s.beforeReturn != nil {
		s.beforeReturn(ctx)
	}
	return f.NotCommittedResult(f.NewFault(f.Forbidden, f.NotCommitted))
}

type schedulerEntryProof struct{}

func (schedulerEntryProof) RequireCurrentProcessInTx(context.Context, f.Tx, oc.ProcessID) error {
	return fault(f.Forbidden)
}
func (schedulerEntryProof) ConfirmStopped(context.Context, oc.ProcessID) error {
	return fault(f.Forbidden)
}

type schedulerEntryStopAuthority struct{ sc.ProjectStopAuthority }

func newSchedulerEntryPair(t *testing.T) (*ProjectCallOwnership, *schedulerEntryStore) {
	t.Helper()
	store := &schedulerEntryStore{}
	a, err := NewPendingAuthority(store)
	if err != nil {
		t.Fatal(err)
	}
	projects := &claimTestProject{}
	execution := &handoffTestExecution{}
	deps := ProjectCallDependencies{
		Coordinator: CoordinatorDependencies{Projects: projects, Claims: &claimTestWork{}, Executions: execution, Capacity: claimTestCapacity(func(context.Context, f.Tx, i.ProjectID, []ec.AssociatedDispatch) (int64, error) {
			return 0, fault(f.Forbidden)
		})},
		Handoff:       LaunchHandoffDependencies{Executions: execution, Observations: execution},
		Busy:          BusyCompensatorDependencies{Projects: projects, Work: &busyTestWork{}},
		Failure:       LaunchFailureFinalizerDependencies{Projects: projects, Work: &failureTestWork{}},
		RelaunchTasks: &relaunchTestWork{}, Tasks: &runnerTestReader{}, Occupancy: &relaunchTestOccupancy{}, RelaunchSkipCount: 2,
	}
	pair, err := newProjectCallOwnership(a, deps, schedulerEntryProof{}, dispatchTestID[oc.Process](t, 881), &schedulerEntryStopAuthority{})
	if err != nil {
		t.Fatal(err)
	}
	return pair, store
}

type schedulerEntryInvocation struct {
	name   string
	lane   projectCallLane
	method string
	run    func(context.Context) error
}

func schedulerEntryInvocations(t *testing.T, pair *ProjectCallOwnership) []schedulerEntryInvocation {
	t.Helper()
	p := dispatchTestID[i.Project](t, 100)
	id := dispatchTestID[DispatchIdentity](t, 1)
	r := wc.TaskClaimRequest{ProjectID: p, TaskID: dispatchTestID[wc.Task](t, 201), AgentID: dispatchTestID[i.Agent](t, 101), DispatchID: id.String(), ExpectedTaskVersion: 1, CurrentSprintID: dispatchTestID[pc.Sprint](t, 102), Purpose: "task/work", RequestID: dispatchTestID[f.Request](t, 301)}
	rr := wc.TaskRelaunchRequest{ProjectID: p, TaskID: r.TaskID, AgentID: r.AgentID, DispatchID: id.String(), ExpectedTaskVersion: 2, CurrentSprintID: r.CurrentSprintID, Purpose: r.Purpose, RequestID: r.RequestID}
	if r.Validate() != nil || rr.Validate() != nil || emptyClaimPolicy().Validate() != nil {
		t.Fatal("invalid entry input")
	}
	tasks := &runnerTestReader{}
	options := ProjectRunnerOptions{ProjectID: p, TickInterval: time.Millisecond, LaunchPolicy: emptyClaimPolicy(), Tasks: tasks, Relaunch: pair.Relaunch()}
	runner, err := NewProjectRunner(pair.Coordinator(), pair.Visitor(), options)
	if err != nil {
		t.Fatal(err)
	}
	executions := &managedExecutions{ready: make(chan struct{})}
	manager, err := NewProjectRunners(ProjectRunnersDependencies{Projects: managedDirectory(func(context.Context, pc.SchedulerProjectPageRequest) (pc.SchedulerProjectPage, error) {
		return pc.SchedulerProjectPage{}, fault(f.Forbidden)
	}), Coordinator: pair.Coordinator(), Visitor: pair.Visitor(), Tasks: tasks, Relaunch: pair.Relaunch(), Executions: executions}, ProjectRunnersOptions{MaxProjects: 1, ProjectPageSize: 1, ExecutionPageSize: 1, DiscoveryInterval: time.Hour, TickInterval: time.Millisecond, RelaunchSkipCount: 2, LaunchPolicy: emptyClaimPolicy()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { manager.Stop(); runner.Stop() })
	return []schedulerEntryInvocation{
		{"claim", projectLaneCoordinator, "claim", func(ctx context.Context) error {
			_, e := pair.Coordinator().ClaimTask(ctx, r, emptyClaimPolicy())
			return e
		}},
		{"resolve-claim", projectLaneCoordinator, "resolve_claim", func(ctx context.Context) error { _, e := pair.Coordinator().ResolveClaim(ctx, r); return e }},
		{"visit-next", projectLaneVisitor, "visit_next", func(ctx context.Context) error { _, e := pair.Visitor().VisitNext(ctx, p, nil); return e }},
		{"visit", projectLaneVisitor, "visit", func(ctx context.Context) error { _, e := pair.Visitor().Visit(ctx, p, id); return e }},
		{"launch", projectLaneHandoff, "launch_once", func(ctx context.Context) error { _, e := pair.Handoff().LaunchOnce(ctx, p, id); return e }},
		{"lookup-launch", projectLaneHandoff, "lookup_launch", func(ctx context.Context) error { _, e := pair.Handoff().Lookup(ctx, p, id); return e }},
		{"retry", projectLaneHandoff, "retry_due", func(ctx context.Context) error { _, e := pair.Handoff().RetryDue(ctx, p, id); return e }},
		{"busy", projectLaneBusy, "compensate_agent_busy", func(ctx context.Context) error { _, e := pair.Busy().CompensateAgentBusy(ctx, p, id); return e }},
		{"lookup-busy", projectLaneBusy, "lookup_busy_compensation", func(ctx context.Context) error { _, e := pair.Busy().Lookup(ctx, p, id); return e }},
		{"failure", projectLaneFailure, "finalize_launch_failure", func(ctx context.Context) error { _, e := pair.Failure().FinalizeLaunchFailure(ctx, p, id); return e }},
		{"lookup-failure", projectLaneFailure, "lookup_launch_failure", func(ctx context.Context) error { _, e := pair.Failure().Lookup(ctx, p, id); return e }},
		{"relaunch", projectLaneRelaunch, "relaunch", func(ctx context.Context) error {
			_, e := pair.Relaunch().VisitRelaunch(ctx, rr, emptyClaimPolicy())
			return e
		}},
		{"resolve-relaunch", projectLaneRelaunch, "resolve_relaunch", func(ctx context.Context) error { _, e := pair.Relaunch().ResolveRelaunch(ctx, rr); return e }},
		{"runner", projectLaneRunner, "project_run", runner.Run},
		{"traversal", projectLaneRunner, "project_traversal", func(ctx context.Context) error { _, e := runner.RunTraversal(ctx); return e }},
		{"manager", projectLaneRunner, "managed_project_run", func(ctx context.Context) error {
			call := &managedRunner{runner: runner, active: true}
			manager.mu.Lock()
			manager.projects[p] = call
			manager.mu.Unlock()
			manager.runOne(ctx, call, nil)
			manager.mu.Lock()
			defer manager.mu.Unlock()
			if call.active {
				t.Error("original manager tail did not return")
			}
			return call.err
		}},
	}
}

func TestSchedulerProjectEntriesRegisterBeforeBusiness(t *testing.T) {
	for index := range 16 {
		pair, store := newSchedulerEntryPair(t)
		invocation := schedulerEntryInvocations(t, pair)[index]
		t.Run(invocation.name, func(t *testing.T) {
			err := invocation.run(context.Background())
			if !runnerHasCode(err, f.Forbidden) {
				t.Fatal("did not reach registration refusal", err)
			}
			if len(store.causes) != 1 || len(store.records) != 1 {
				t.Fatal("entry skipped or repeated registration")
			}
			details := store.causes[0].Details()
			if details.Kind != f.RecoveryCause || details.Owner != "scheduler-call-register" {
				t.Fatal("business ran before registration")
			}
			r := store.records[0]
			if r.Project != dispatchTestID[i.Project](t, 100) || r.Lane != invocation.lane {
				t.Fatal("wrong registered Project/lane")
			}
			// Identity is the canonical value captured by the original registry, not
			// the business command's safe logging projection.
			if !stringsContainsEntryMethod(r.Identity, invocation.method) {
				t.Fatal("entrance identity was collapsed")
			}
			pair.Stop()
			if !pair.Joined() {
				t.Fatal("known registration refusal retained an actual returned call")
			}
		})
	}
}

func stringsContainsEntryMethod(identity, method string) bool {
	// Decode only the stable identity projection; this does not mint authority.
	var value struct {
		Command string `json:"command"`
	}
	return json.Unmarshal([]byte(identity), &value) == nil && value.Command == method
}

func TestSchedulerProjectEntryWaitsForOriginalRegistration(t *testing.T) {
	pair, store := newSchedulerEntryPair(t)
	invocation := schedulerEntryInvocations(t, pair)[4]
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	store.beforeReturn = func(context.Context) { close(entered); <-release }
	done := make(chan error, 1)
	collected := false
	t.Cleanup(func() {
		unblock()
		if !collected {
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				t.Error("original registration did not return")
			}
		}
		pair.Stop()
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { done <- invocation.run(ctx) }()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("registration did not start")
	}
	cancel()
	pair.Stop()
	if pair.Joined() || pair.Handoff().Joined() {
		t.Fatal("cancellation replaced original registration return")
	}
	select {
	case <-done:
		collected = true
		t.Fatal("entry returned before original Store")
	default:
	}
	cancelled, closeCancelled := context.WithCancel(context.Background())
	closeCancelled()
	if !errors.Is(pair.Handoff().Drain(cancelled), context.Canceled) {
		t.Fatal("lane drain skipped registered original call")
	}
	unblock()
	select {
	case err := <-done:
		collected = true
		if !runnerHasCode(err, f.Forbidden) {
			t.Fatal("changed original registration error", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("entry did not actually return")
	}
	if !pair.Joined() || !pair.Handoff().Joined() {
		t.Fatal("returned registration remained active")
	}
}

func TestSchedulerProjectEntriesRejectMixedOwners(t *testing.T) {
	for _, name := range []string{"runner-visitor", "runner-relaunch", "manager-visitor", "manager-relaunch"} {
		t.Run(name, func(t *testing.T) {
			pair, store := newSchedulerEntryPair(t)
			t.Cleanup(pair.Stop)
			coordinator, visitor, relaunch := pair.Coordinator(), pair.Visitor(), pair.Relaunch()
			tasks := &runnerTestReader{}
			var err error
			if name == "runner-visitor" || name == "manager-visitor" {
				// Same PendingAuthority and original downstream ports; only the
				// whole-call ownership binding differs.
				visitor, err = NewPendingVisitorWithFailure(coordinator.authority, pair.Handoff(), pair.Busy(), pair.Failure())
			} else {
				relaunch, err = NewRelaunchCoordinator(coordinator, &relaunchTestWork{}, tasks, &relaunchTestOccupancy{}, 2)
			}
			if err != nil {
				t.Fatal(err)
			}
			if name == "runner-visitor" || name == "runner-relaunch" {
				_, err = NewProjectRunner(coordinator, visitor, ProjectRunnerOptions{ProjectID: dispatchTestID[i.Project](t, 100), TickInterval: time.Millisecond, LaunchPolicy: emptyClaimPolicy(), Tasks: tasks, Relaunch: relaunch})
			} else {
				_, err = NewProjectRunners(ProjectRunnersDependencies{Projects: managedDirectory(func(context.Context, pc.SchedulerProjectPageRequest) (pc.SchedulerProjectPage, error) {
					return pc.SchedulerProjectPage{}, fault(f.Forbidden)
				}), Coordinator: coordinator, Visitor: visitor, Tasks: tasks, Relaunch: relaunch, Executions: &managedExecutions{ready: make(chan struct{})}}, ProjectRunnersOptions{MaxProjects: 1, ProjectPageSize: 1, ExecutionPageSize: 1, DiscoveryInterval: time.Hour, TickInterval: time.Millisecond, RelaunchSkipCount: 2, LaunchPolicy: emptyClaimPolicy()})
			}
			if !runnerHasCode(err, f.DependencyUnbound) || len(store.causes) != 0 {
				t.Fatal("mixed original ownership was accepted", err)
			}
		})
	}
}

func TestSchedulerProjectEntryInvalidInputPrecedesRegistration(t *testing.T) {
	pair, store := newSchedulerEntryPair(t)
	defer pair.Stop()
	p, id := dispatchTestID[i.Project](t, 100), dispatchTestID[DispatchIdentity](t, 1)
	if _, err := pair.Handoff().LaunchOnce(nil, p, id); !runnerHasCode(err, f.InvalidArgument) {
		t.Fatal(err)
	}
	if _, err := pair.Visitor().Visit(context.Background(), i.ProjectID{}, id); !runnerHasCode(err, f.InvalidArgument) {
		t.Fatal(err)
	}
	if _, err := pair.Coordinator().ClaimTask(context.Background(), wc.TaskClaimRequest{}, emptyClaimPolicy()); !runnerHasCode(err, f.InvalidArgument) {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := pair.Handoff().Lookup(ctx, p, id); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if len(store.causes) != 0 {
		t.Fatal("invalid or cancelled input registered technical work")
	}
}
