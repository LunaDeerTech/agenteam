package scheduler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

type managedDirectory func(context.Context, pc.SchedulerProjectPageRequest) (pc.SchedulerProjectPage, error)

func (f managedDirectory) ListSchedulerProjects(ctx context.Context, r pc.SchedulerProjectPageRequest) (pc.SchedulerProjectPage, error) {
	return f(ctx, r)
}

type managedExecutions struct {
	ready chan struct{}
	stops atomic.Int32
}

func (e *managedExecutions) Ready() <-chan struct{} { return e.ready }
func (e *managedExecutions) Stop()                  { e.stops.Add(1) }
func (*managedExecutions) Advance(context.Context, i.ProjectID, ec.AssociatedDispatch) (ec.ExecutionAdvance, error) {
	return ec.ExecutionAdvance{}, errors.New("controlled executor must not receive a fabricated association")
}

func newManagedTest(t *testing.T, projects pc.SchedulerProjects) (*ProjectRunners, *runnerTestFixture, *managedExecutions) {
	t.Helper()
	v := newRunnerTest(t, time.Millisecond)
	relaunch, err := NewRelaunchCoordinator(v.coordinator, &relaunchTestWork{}, v.reader, &relaunchTestOccupancy{}, 2)
	if err != nil {
		t.Fatal(err)
	}
	executions := &managedExecutions{ready: make(chan struct{})}
	m, err := NewProjectRunners(ProjectRunnersDependencies{Projects: projects, Coordinator: v.coordinator, Visitor: v.visitor, Tasks: v.reader, Relaunch: relaunch, Executions: executions}, ProjectRunnersOptions{
		MaxProjects: 1, ProjectPageSize: 1, ExecutionPageSize: 1, DiscoveryInterval: time.Hour, TickInterval: time.Millisecond, RelaunchSkipCount: 2, LaunchPolicy: emptyClaimPolicy(),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { m.Stop(); relaunch.Stop() })
	return m, v, executions
}

// Directory pages and call exits below are explicit controls. Constructors
// remain the real Scheduler constructors; these tests do not claim Project
// discovery SQL, Work authorization, successful Claim or a real Execution.
func TestSchedulerProjectRunnersBoundedCyclesAndCapacity(t *testing.T) {
	a, b, c := dispatchTestID[i.Project](t, 901), dispatchTestID[i.Project](t, 902), dispatchTestID[i.Project](t, 903)
	var requests []pc.SchedulerProjectPageRequest
	readFailure := errors.New("controlled discovery failure")
	step := 0
	directory := managedDirectory(func(_ context.Context, request pc.SchedulerProjectPageRequest) (pc.SchedulerProjectPage, error) {
		requests = append(requests, request.Clone())
		step++
		switch step {
		case 1:
			return pc.SchedulerProjectPage{ProjectIDs: []i.ProjectID{a}, Through: &c}, nil
		case 2:
			return pc.SchedulerProjectPage{}, readFailure
		case 3:
			return pc.SchedulerProjectPage{ProjectIDs: []i.ProjectID{b}, Through: &c}, nil
		case 4:
			return pc.SchedulerProjectPage{ProjectIDs: []i.ProjectID{c}, Through: &c, Complete: true}, nil
		case 5:
			return pc.SchedulerProjectPage{ProjectIDs: []i.ProjectID{b}, Through: &c}, nil
		case 6:
			return pc.SchedulerProjectPage{ProjectIDs: []i.ProjectID{c}, Through: &c, Complete: true}, nil
		default:
			return pc.SchedulerProjectPage{ProjectIDs: []i.ProjectID{b}, Through: &b, Complete: true}, nil
		}
	})
	m, _, executions := newManagedTest(t, directory)
	for _, change := range []func(*ProjectRunnersOptions){
		func(o *ProjectRunnersOptions) { o.MaxProjects = 0 },
		func(o *ProjectRunnersOptions) { o.ProjectPageSize = pc.MaxSchedulerProjectPageSize + 1 },
		func(o *ProjectRunnersOptions) { o.ExecutionPageSize = 0 },
		func(o *ProjectRunnersOptions) { o.DiscoveryInterval = 0 },
		func(o *ProjectRunnersOptions) { o.TickInterval = 0 },
		func(o *ProjectRunnersOptions) { o.RelaunchSkipCount = -1 },
	} {
		o := m.options
		change(&o)
		if _, err := NewProjectRunners(m.deps, o); !runnerHasCode(err, f.InvalidArgument) {
			t.Fatal("implicit or invalid capacity/timing was accepted", err)
		}
	}
	o := m.options
	o.RelaunchSkipCount++
	if _, err := NewProjectRunners(m.deps, o); !runnerHasCode(err, f.DependencyUnbound) {
		t.Fatal("manager changed the original relaunch policy", err)
	}

	m.discover(context.Background())
	first := m.projects[a]
	if first == nil || first.runner.options.ProjectID != a || first.runner.coordinator != m.deps.Coordinator || first.runner.visitor != m.deps.Visitor || first.runner.executions != executions {
		t.Fatal("factory replaced original domain owners")
	}
	position := m.position.Clone()
	m.discover(context.Background())
	if !reflect.DeepEqual(position, m.position) || !errors.Is(m.Observation().DiscoveryError, readFailure) {
		t.Fatal("failed page advanced its cursor")
	}
	m.discover(context.Background())
	m.discover(context.Background())
	if len(m.projects) != 1 || m.projects[a] != first || m.Observation().Deferred != 2 || m.position.After != nil || m.position.Through != nil {
		t.Fatal("capacity hid later pages, replaced a runner, or lost EOF wrap")
	}
	if !reflect.DeepEqual(requests[1], requests[2]) || requests[3].After == nil || *requests[3].After != b || requests[3].Through == nil || *requests[3].Through != c {
		t.Fatal("fixed cycle identity changed")
	}
	m.discover(context.Background())
	if first.stopping || first.runner.Joined() {
		t.Fatal("a partial cycle's absence stopped a Project")
	}
	m.discover(context.Background())
	if !first.runner.Joined() || len(m.projects) != 0 {
		t.Fatal("complete absence did not retire the actual original runner")
	}
	m.discover(context.Background())
	observation := m.Observation()
	if len(observation.Projects) != 1 || observation.Projects[0].ProjectID != b || executions.stops.Load() != 0 {
		t.Fatal("capacity did not admit a later cycle or stopped the borrowed executor")
	}
	observation.Projects[0].ProjectID = a
	if m.Observation().Projects[0].ProjectID != b {
		t.Fatal("observation aliases live ownership")
	}
	encoded, err := json.Marshal(observation)
	if err != nil || string(encoded) != `"scheduler_project_runners"` || fmt.Sprint(observation) != "scheduler_project_runners" {
		t.Fatal("default observation exposed runtime details")
	}
}

func waitManaged(t *testing.T, m *ProjectRunners, ready func(ProjectRunnersObservation) bool) {
	t.Helper()
	deadline := time.NewTimer(2 * time.Second)
	defer deadline.Stop()
	for {
		m.mu.Lock()
		changed := m.changed
		m.mu.Unlock()
		if ready(m.Observation()) {
			return
		}
		select {
		case <-changed:
		case <-deadline.C:
			t.Fatal("managed lifetime did not reach controlled checkpoint")
		}
	}
}

func TestSchedulerProjectRunnersWaitForActualCallsAndBorrowExecutor(t *testing.T) {
	p := dispatchTestID[i.Project](t, 904)
	var scans atomic.Int32
	m, _, executions := newManagedTest(t, managedDirectory(func(context.Context, pc.SchedulerProjectPageRequest) (pc.SchedulerProjectPage, error) {
		scans.Add(1)
		return pc.SchedulerProjectPage{ProjectIDs: []i.ProjectID{p}, Through: &p, Complete: true}, nil
	}))
	entered, cancelled, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	m.ops.run = func(ctx context.Context, _ *ProjectRunner) error {
		close(entered)
		<-ctx.Done()
		close(cancelled)
		<-release
		return ctx.Err()
	}
	returned := make(chan error, 1)
	go func() { returned <- m.Run(context.Background()) }()
	t.Cleanup(func() { m.Stop(); releaseOnce.Do(func() { close(release) }) })
	waitManaged(t, m, func(v ProjectRunnersObservation) bool { return v.Started })
	if scans.Load() != 0 {
		t.Fatal("discovery preceded the borrowed executor lifetime")
	}
	close(executions.ready)
	awaitRunnerSignal(t, entered)
	if err := m.Run(context.Background()); !runnerHasCode(err, f.InvalidState) {
		t.Fatal("a second manager lifetime was admitted", err)
	}
	m.Stop()
	awaitRunnerSignal(t, cancelled)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := m.Drain(ctx); !errors.Is(err, context.Canceled) || m.Joined() {
		t.Fatal("cancellation was mistaken for the original runner return", err)
	}
	select {
	case <-returned:
		t.Fatal("Run abandoned its physical callback")
	default:
	}
	releaseOnce.Do(func() { close(release) })
	select {
	case <-returned:
	case <-time.After(2 * time.Second):
		t.Fatal("manager failed to join its actual runner")
	}
	if err := m.Drain(context.Background()); err != nil || !m.Joined() || executions.stops.Load() != 0 {
		t.Fatal("manager claimed borrowed ownership or lost actual join", err)
	}

	// The discovery callback itself is also an owned call, even with no runners.
	readEntered, readRelease := make(chan struct{}), make(chan struct{})
	m2, _, executor2 := newManagedTest(t, managedDirectory(func(ctx context.Context, _ pc.SchedulerProjectPageRequest) (pc.SchedulerProjectPage, error) {
		close(readEntered)
		<-readRelease
		return pc.SchedulerProjectPage{}, ctx.Err()
	}))
	close(executor2.ready)
	readDone := make(chan error, 1)
	go func() { readDone <- m2.Run(context.Background()) }()
	var readOnce sync.Once
	t.Cleanup(func() { m2.Stop(); readOnce.Do(func() { close(readRelease) }) })
	awaitRunnerSignal(t, readEntered)
	m2.Stop()
	if err := m2.Drain(ctx); !errors.Is(err, context.Canceled) || m2.Joined() {
		t.Fatal("Drain abandoned a cancelled directory callback", err)
	}
	readOnce.Do(func() { close(readRelease) })
	select {
	case <-readDone:
	case <-time.After(2 * time.Second):
		t.Fatal("directory callback was not joined")
	}
	if !m2.Joined() {
		t.Fatal("returned discovery still looked active")
	}
}

func TestSchedulerProjectRunnersKeepOriginalUnknownBeforeRestart(t *testing.T) {
	m, v, executions := newManagedTest(t, managedDirectory(func(context.Context, pc.SchedulerProjectPageRequest) (pc.SchedulerProjectPage, error) {
		return pc.SchedulerProjectPage{ProjectIDs: []i.ProjectID{}, Complete: true}, nil
	}))
	v.store.row.outcome, v.store.row.attempts, v.store.row.busyAttempt = NotSent, 0, 0
	v.store.rows = []*dispatchRecord{v.store.row}
	v.store.failName, v.store.failMode = "launch_handoff", "unknown-after"
	prior := v.runner.Run(context.Background())
	original, ok := UnknownAttempt(prior)
	if !ok || v.execution.launches != 0 {
		t.Fatal("controlled marker did not retain its original physical Unknown", prior)
	}
	owned := &managedRunner{runner: v.runner, active: true, seen: true, err: prior}
	m.projects[v.options.ProjectID] = owned
	starts := 0
	afterRecovery := errors.New("controlled traversal return after exact recovery")
	m.ops.run = func(context.Context, *ProjectRunner) error { starts++; return afterRecovery }
	v.project.enabled = false
	m.runOne(context.Background(), owned, prior)
	if starts != 0 || v.execution.lookups != 0 || !m.Observation().Projects[0].Retained {
		t.Fatal("paused recovery associated or launched new work")
	}
	v.project.enabled = true
	owned.active = true
	m.runOne(context.Background(), owned, owned.err)
	actual, retained := UnknownAttempt(owned.err)
	if !retained || actual.AttemptID() != original.AttemptID() || starts != 0 || v.execution.lookups != 1 || v.execution.launches != 0 || owned.next.Before(time.Now()) {
		t.Fatal("recovery replaced the original key, owner, or pacing", owned.err)
	}

	// Controlled canonical observation, not evidence of an actual SQL commit or
	// Execution creation. The real Handoff.Lookup/association path consumes it.
	v.execution.created = visitCreated(t, v.store.row)
	owned.active = true
	m.runOne(context.Background(), owned, owned.err)
	if !errors.Is(owned.err, afterRecovery) || uncertainProjectVisit(owned.err) || starts != 1 || v.execution.lookups != 2 || v.execution.launches != 0 {
		t.Fatal("confirmed original owner did not precede the same runner restart", owned.err)
	}
	v.visitor.handoff.mu.Lock()
	remaining := len(v.visitor.handoff.calls)
	v.visitor.handoff.mu.Unlock()
	if remaining != 0 || executions.stops.Load() != 0 {
		t.Fatal("recovery lost borrowed owner retirement")
	}
	m.Stop()
	if err := m.Drain(context.Background()); err != nil || !m.Joined() {
		t.Fatal("resolved manager failed to join", err)
	}
}
