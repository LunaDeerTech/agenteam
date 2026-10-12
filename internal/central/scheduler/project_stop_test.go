package scheduler

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

// Manager mechanics use the original private functions and controlled runner
// returns, not a substitute successful Project authority. The real concrete
// directory/StopDriver/authorization SQL composition belongs to integration.
func projectStopIdentity(t *testing.T, p i.ProjectID) (pc.LifecycleCause, pc.ScopeRef) {
	t.Helper()
	return pc.LifecycleCause{OperationID: dispatchTestID[pc.Operation](t, 941), Action: pc.Delete, ProjectVersion: 2}, pc.ScopeRef{Kind: pc.ProjectScope, ProjectID: p}
}
func projectStopEntry(t *testing.T, m *ProjectRunners, p i.ProjectID) *managedRunner {
	t.Helper()
	r, err := m.ops.create(p)
	if err != nil {
		t.Fatal(err)
	}
	v := &managedRunner{runner: r, seen: true}
	m.projects[p] = v
	return v
}
func projectStopAwait(t *testing.T, done <-chan struct{}) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("original managed call did not reach barrier")
	}
}

func TestSchedulerProjectStopDiscardsCapturedPageWithoutStoppingAnotherProject(t *testing.T) {
	a, b := dispatchTestID[i.Project](t, 931), dispatchTestID[i.Project](t, 932)
	entered, release, returned := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var once sync.Once
	free := func() { once.Do(func() { close(release) }) }
	calls := 0
	m, _, executions := newManagedTest(t, managedDirectory(func(context.Context, pc.SchedulerProjectPageRequest) (pc.SchedulerProjectPage, error) {
		calls++
		if calls == 1 {
			close(entered)
			<-release
			return pc.SchedulerProjectPage{ProjectIDs: []i.ProjectID{a}, Through: &b, Complete: true}, nil
		}
		return pc.SchedulerProjectPage{ProjectIDs: []i.ProjectID{b}, Through: &b, Complete: true}, nil
	}))
	m.options.MaxProjects = 2
	bOwner := projectStopEntry(t, m, b)
	go func() { defer close(returned); m.discover(context.Background()) }()
	t.Cleanup(func() { free(); projectStopAwait(t, returned) })
	projectStopAwait(t, entered)
	cause, scope := projectStopIdentity(t, a)
	if _, err := m.stopProject(context.Background(), cause, scope, false); err != nil {
		t.Fatal(err)
	}
	free()
	projectStopAwait(t, returned)
	m.mu.Lock()
	recreated := m.projects[a] != nil
	wrongStop := bOwner.stopping || m.projects[b] != bOwner
	position := m.position.Clone()
	cycle := m.cycleOpen
	m.mu.Unlock()
	if recreated || wrongStop || cycle || position.After != nil || position.Through != nil {
		t.Fatal("old page changed the new discovery generation")
	}
	m.discover(context.Background())
	if m.projects[a] != nil || m.projects[b] != bOwner || bOwner.stopping || executions.stops.Load() != 0 {
		t.Fatal("fresh other-project cycle changed owner isolation")
	}
}

func TestSchedulerProjectStopWaitsForTargetActualReturnOnly(t *testing.T) {
	a, b := dispatchTestID[i.Project](t, 933), dispatchTestID[i.Project](t, 934)
	m, _, executions := newManagedTest(t, managedDirectory(func(context.Context, pc.SchedulerProjectPageRequest) (pc.SchedulerProjectPage, error) {
		return pc.SchedulerProjectPage{ProjectIDs: []i.ProjectID{}, Complete: true}, nil
	}))
	m.options.MaxProjects = 2
	aOwner, bOwner := projectStopEntry(t, m, a), projectStopEntry(t, m, b)
	aEntered, bEntered := make(chan struct{}), make(chan struct{})
	aCancelled, bCancelled := make(chan struct{}), make(chan struct{})
	aRelease, bRelease := make(chan struct{}), make(chan struct{})
	aReturned, bReturned := make(chan struct{}), make(chan struct{})
	var aOnce, bOnce sync.Once
	freeA := func() { aOnce.Do(func() { close(aRelease) }) }
	freeB := func() { bOnce.Do(func() { close(bRelease) }) }
	m.ops.run = func(ctx context.Context, r *ProjectRunner) error {
		if r == aOwner.runner {
			defer close(aReturned)
			close(aEntered)
			<-ctx.Done()
			close(aCancelled)
			<-aRelease
			return ctx.Err()
		}
		if r != bOwner.runner {
			t.Error("replacement runner created")
			return errors.New("unexpected runner")
		}
		defer close(bReturned)
		close(bEntered)
		<-ctx.Done()
		close(bCancelled)
		<-bRelease
		return ctx.Err()
	}
	m.startDue(context.Background())
	t.Cleanup(func() {
		m.Stop()
		freeA()
		freeB()
		projectStopAwait(t, aReturned)
		projectStopAwait(t, bReturned)
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := m.waitReturned(ctx, false); err != nil {
			t.Error(err)
		}
	})
	projectStopAwait(t, aEntered)
	projectStopAwait(t, bEntered)
	cause, scope := projectStopIdentity(t, a)
	report, err := m.stopProject(context.Background(), cause, scope, false)
	if err != nil {
		t.Fatal(err)
	}
	projectStopAwait(t, aCancelled)
	if report.Details().LocalJoined || report.Details().PendingRunners != 1 {
		t.Fatal("cancellation was reported as original return")
	}
	select {
	case <-bCancelled:
		t.Fatal("stopping A cancelled B")
	default:
	}
	select {
	case <-aReturned:
		t.Fatal("A ignored its actual return barrier")
	default:
	}
	controlCtx, controlDone, err := m.beginProjectStop(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(controlDone)
	drainReturned := make(chan struct{})
	var drain ProjectStopReport
	var drainErr error
	go func() { defer close(drainReturned); drain, drainErr = m.stopProject(controlCtx, cause, scope, true) }()
	t.Cleanup(func() { freeA(); projectStopAwait(t, drainReturned) })
	waitManaged(t, m, func(ProjectRunnersObservation) bool {
		m.mu.Lock()
		defer m.mu.Unlock()
		return m.generation == 2
	})
	select {
	case <-drainReturned:
		t.Fatal("target drain returned while the original runner was held")
	default:
	}
	freeA()
	projectStopAwait(t, aReturned)
	projectStopAwait(t, drainReturned)
	if drainErr != nil || !drain.Details().LocalJoined || drain.Details().PendingRunners != 0 {
		t.Fatal("original A return did not complete local handoff", drainErr)
	}
	// The target drain returned while its own manager control is still owned.
	// Using global Drain/waitReturned here would wait on itself forever.
	m.mu.Lock()
	controls := len(m.stopControls)
	m.mu.Unlock()
	if controls != 1 || !drain.Matches(cause, scope) {
		t.Fatal("target drain released/replaced its outer control")
	}
	m.mu.Lock()
	removed := m.projects[a] == nil
	stillB := m.projects[b] == bOwner && bOwner.active && !bOwner.stopping
	m.mu.Unlock()
	if !removed || !stillB || executions.stops.Load() != 0 {
		t.Fatal("handoff retired the wrong owner or stopped shared execution")
	}
}

func projectStopUnknown(t *testing.T, p i.ProjectID) error {
	t.Helper()
	cause, err := f.NewRecoveryCause("scheduler-project-stop-test", p.String())
	if err != nil {
		t.Fatal(err)
	}
	attempt := dispatchTestID[f.TransactionAttempt](t, 942)
	return &ProjectRunError{result: ProjectRunResult{ProjectID: p}, cause: commitError(f.UnknownResult(attempt, cause))}
}

func TestSchedulerProjectStopRetainsOriginalUnknownOnBothReturnOrders(t *testing.T) {
	for _, name := range []string{"unknown-before-stop", "unknown-after-stop"} {
		t.Run(name, func(t *testing.T) {
			p := dispatchTestID[i.Project](t, 935)
			m, _, _ := newManagedTest(t, managedDirectory(func(context.Context, pc.SchedulerProjectPageRequest) (pc.SchedulerProjectPage, error) {
				return pc.SchedulerProjectPage{ProjectIDs: []i.ProjectID{}, Complete: true}, nil
			}))
			v := projectStopEntry(t, m, p)
			original := projectStopUnknown(t, p)
			var recoveries atomic.Int32
			m.ops.recover = func(context.Context, *ProjectRunner, error) error { recoveries.Add(1); return nil }
			entered, release, returned := make(chan struct{}), make(chan struct{}), make(chan struct{})
			var once sync.Once
			free := func() { once.Do(func() { close(release) }) }
			if name == "unknown-before-stop" {
				v.err = original
			} else {
				m.ops.run = func(ctx context.Context, _ *ProjectRunner) error {
					defer close(returned)
					close(entered)
					<-ctx.Done()
					<-release
					return original
				}
				m.startDue(context.Background())
				t.Cleanup(func() {
					m.Stop()
					free()
					projectStopAwait(t, returned)
					ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
					defer cancel()
					if err := m.waitReturned(ctx, false); err != nil {
						t.Error(err)
					}
				})
				projectStopAwait(t, entered)
			}
			cause, scope := projectStopIdentity(t, p)
			_, _ = m.stopProject(context.Background(), cause, scope, false)
			if name == "unknown-after-stop" {
				free()
				projectStopAwait(t, returned)
				waitManaged(t, m, func(o ProjectRunnersObservation) bool { return len(o.Projects) == 1 && !o.Projects[0].Running })
			}
			retained, err := m.stopProject(context.Background(), cause, scope, false)
			if err != nil || !retained.Valid() || !retained.Details().Retained || retained.Details().LocalJoined || !retained.Matches(cause, scope) {
				t.Fatal("original Unknown missing from local report", err)
			}
			report, err := m.stopProject(context.Background(), cause, scope, true)
			if !errors.Is(err, original) || report.Details().LocalJoined {
				t.Fatal("Unknown was treated as a joined runner", err)
			}
			m.startDue(context.Background())
			m.mu.Lock()
			kept := m.projects[p] == v && errors.Is(v.err, original) && !v.active
			m.mu.Unlock()
			if !kept || recoveries.Load() != 0 || m.Joined() {
				t.Fatal("stop lost original Unknown identity/slot or auto-recovered")
			}
		})
	}
}

func TestSchedulerProjectStopCancelsRecoveryAndRejectsDelayedRun(t *testing.T) {
	for _, name := range []string{"scheduled-before-stop", "recovery-running-at-stop"} {
		t.Run(name, func(t *testing.T) {
			p := dispatchTestID[i.Project](t, 936)
			m, _, _ := newManagedTest(t, managedDirectory(func(context.Context, pc.SchedulerProjectPageRequest) (pc.SchedulerProjectPage, error) {
				return pc.SchedulerProjectPage{ProjectIDs: []i.ProjectID{}, Complete: true}, nil
			}))
			v := projectStopEntry(t, m, p)
			original := projectStopUnknown(t, p)
			v.err = original
			var recoveries, runs atomic.Int32
			entered, cancelled, release, returned := make(chan struct{}), make(chan struct{}), make(chan struct{}), make(chan struct{})
			var once sync.Once
			free := func() { once.Do(func() { close(release) }) }
			m.ops.recover = func(ctx context.Context, got *ProjectRunner, prior error) error {
				defer close(returned)
				recoveries.Add(1)
				if got != v.runner || prior != original {
					t.Error("replaced original recovery owner")
				}
				close(entered)
				<-ctx.Done()
				close(cancelled)
				<-release
				// Even a late nil from the original borrowed recovery cannot
				// authorize a new run after this Project was stopped.
				return nil
			}
			m.ops.run = func(context.Context, *ProjectRunner) error { runs.Add(1); return nil }
			cause, scope := projectStopIdentity(t, p)
			if name == "scheduled-before-stop" {
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				v.active, v.cancel = true, cancel
				if _, err := m.stopProject(context.Background(), cause, scope, false); err != nil {
					t.Fatal(err)
				}
				m.runOne(ctx, v, original)
			} else {
				m.startDue(context.Background())
				t.Cleanup(func() {
					m.Stop()
					free()
					projectStopAwait(t, returned)
					ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
					defer cancel()
					if err := m.waitReturned(ctx, false); err != nil {
						t.Error(err)
					}
				})
				projectStopAwait(t, entered)
				if _, err := m.stopProject(context.Background(), cause, scope, false); err != nil {
					t.Fatal(err)
				}
				projectStopAwait(t, cancelled)
				select {
				case <-returned:
					t.Fatal("cancellation impersonated original recovery return")
				default:
				}
				free()
				projectStopAwait(t, returned)
				waitManaged(t, m, func(o ProjectRunnersObservation) bool { return len(o.Projects) == 1 && !o.Projects[0].Running })
			}
			m.mu.Lock()
			kept := m.projects[p] == v && !v.active && v.stopping && errors.Is(v.err, original)
			m.mu.Unlock()
			want := int32(0)
			if name == "recovery-running-at-stop" {
				want = 1
			}
			if !kept || recoveries.Load() != want || runs.Load() != 0 {
				t.Fatal("late run erased original Unknown or advanced new work")
			}
		})
	}
}

func TestSchedulerProjectStopControlsKeepGlobalOwnerUntilReturn(t *testing.T) {
	m, _, executions := newManagedTest(t, managedDirectory(func(context.Context, pc.SchedulerProjectPageRequest) (pc.SchedulerProjectPage, error) {
		return pc.SchedulerProjectPage{ProjectIDs: []i.ProjectID{}, Complete: true}, nil
	}))
	run, done, err := m.beginProjectStop(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(done)
	if _, extra, err := m.beginProjectStop(context.Background()); err == nil {
		extra()
		t.Fatal("control admission exceeded original MaxProjects")
	}
	m.Stop()
	projectStopAwait(t, run.Done())
	if m.Joined() {
		t.Fatal("Stop cancelled a control and fabricated its return")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := m.Drain(ctx); !errors.Is(err, context.Canceled) || m.Joined() {
		t.Fatal("expired Drain discarded original control", err)
	}
	if _, extra, err := m.beginProjectStop(context.Background()); err == nil {
		extra()
		t.Fatal("stopped manager readmitted a control")
	}
	done()
	if err := m.Drain(context.Background()); err != nil || !m.Joined() || executions.stops.Load() != 0 {
		t.Fatal("actual control return did not finish local owner", err)
	}
}
