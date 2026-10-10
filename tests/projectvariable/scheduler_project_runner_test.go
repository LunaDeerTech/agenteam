//go:build integration

package projectvariable_test

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"

	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/scheduler"
	"github.com/LunaDeerTech/agenteam/internal/central/work"
	wc "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
)

// The runner receives real Work facts and the original Claim/Launch owners.
// No test seeds a snapshot, a current Sprint, a Dispatch or an Execution.
func TestSchedulerProjectRunner(t *testing.T) {
	t.Run("ordered-todo-and-serial-launch", func(t *testing.T) {
		v := prepareSchedulerClaimTask(t)
		first := createRunnerTodo(t, v, "runner-high-first", wc.TaskPriorityHigh)
		second := createRunnerTodo(t, v, "runner-high-second", wc.TaskPriorityHigh)
		startSchedulerSprint(t, v)
		setRunnerLimit(t, v, 1)
		policy := projectRunnerRetryPolicy(t)
		coordinator := newSchedulerClaimCoordinator(t, v, &policy)
		x := newProjectRunnerFixture(t, v, coordinator)
		// The two High tasks entered the same group in this order; the old
		// Medium task was created first and must nevertheless be visited last.
		want := []wc.TaskID{first.ID, second.ID, v.task.ID}
		state, sprint := wc.TaskStateTodo, v.task.SprintID
		listed, err := v.taskReader.ListTasks(ctxFor(t), v.base.ownerBrowser.actor, v.base.project.ID,
			wc.TaskFilter{State: &state, SprintID: &sprint}, f.PageRequest{Limit: 10})
		if err != nil || listed.NextCursor != "" || len(listed.Items) != len(want) {
			t.Fatal("complete real todo order", err)
		}
		for n, item := range listed.Items {
			if item.ID != want[n] {
				t.Fatal("formal priority/rank order differs from the prepared sequence")
			}
		}
		result, err := x.runner.RunTraversal(ctxFor(t))
		if err != nil || len(result.Visits) != len(want) {
			t.Fatal("real traversal did not visit the complete fixed snapshot", err)
		}
		for n, visit := range result.Visits {
			if visit.TaskID != want[n] || visit.ExpectedState != wc.TaskStateTodo || visit.Action != scheduler.ProjectVisitClaim {
				t.Fatal("runner reordered or revisited its frozen identities")
			}
			if n > 0 {
				requireCode(t, visit.Err, f.ResourceBusy)
			}
		}
		if result.Visits[0].Err != nil {
			t.Fatal("first ordered todo could not claim and launch", result.Visits[0].Err)
		}
		launched := result.Visits[0].Dispatch
		request, err := launched.LaunchRequest()
		if err != nil || request.Trigger.TaskID != first.ID.String() {
			t.Fatal("runner did not preserve the first Task's actual Launch request", err)
		}
		firstFixture := *v
		firstFixture.task = first
		launch := &schedulerLaunchFixture{task: &firstFixture, dispatch: launched.Summary().ID, request: request, handoff: x.handoff, launcher: x.launcher}
		launch.requireAssociated(t, launched)
		for _, before := range []wc.Task{second, v.task} {
			current, readErr := v.taskReader.GetTask(ctxFor(t), v.base.ownerBrowser.actor, v.base.project.ID, before.ID)
			if readErr != nil || !reflect.DeepEqual(current, before) {
				t.Fatal("full capacity mutated a later todo Task", readErr)
			}
		}
		var claims, dispatches, executions int64
		err = v.base.raw.QueryRow(ctxFor(t), `SELECT
 (SELECT count(*) FROM agenteam_work.task_scheduler_claims WHERE project_id=$1::text::uuid),
 (SELECT count(*) FROM agenteam_scheduler.dispatches WHERE project_id=$1::text),
 (SELECT count(*) FROM agenteam_execution.executions WHERE project_id=$1::text)`, v.base.project.ID.String()).Scan(&claims, &dispatches, &executions)
		if err != nil || claims != 1 || dispatches != 1 || executions != 1 {
			t.Fatal("serial traversal overbooked actual Project capacity", err)
		}
	})
	t.Run("paused-pending-recovery-and-join", func(t *testing.T) {
		v := prepareSchedulerClaimTask(t)
		startSchedulerSprint(t, v)
		policy := projectRunnerRetryPolicy(t)
		coordinator := newSchedulerClaimCoordinator(t, v, &policy)
		claim, launchPolicy := schedulerClaimRequest(t, v)
		original, err := coordinator.ClaimTask(ctxFor(t), claim, launchPolicy)
		if err != nil {
			t.Fatal("real original pending Claim", err)
		}
		x := newProjectRunnerFixture(t, v, coordinator)
		request, err := original.LaunchRequest()
		if err != nil {
			t.Fatal(err)
		}
		launch := &schedulerLaunchFixture{task: v, dispatch: original.Summary().ID, request: request, handoff: x.handoff, launcher: x.launcher}
		rollbackRunnerAssociation(t, launch)
		setPendingVisitEnabled(t, v, false, "runner-pause")
		before := runnerSnapshot(t, v)
		launches, lookups, _ := x.launcher.observed()
		paused, err := x.runner.RunTraversal(ctxFor(t))
		if err != nil || len(paused.Visits) != 1 || paused.Visits[0].TaskID != v.task.ID || paused.Visits[0].Action != scheduler.ProjectVisitDeferred || paused.Visits[0].ExpectedState != "" || paused.Visits[0].Err != nil {
			t.Fatal("paused traversal omitted or duplicated the original pending Task", err)
		}
		afterLaunches, afterLookups, _ := x.launcher.observed()
		if before != runnerSnapshot(t, v) || afterLaunches != launches || afterLookups != lookups {
			t.Fatal("paused traversal sent, associated or changed original business facts")
		}
		setPendingVisitEnabled(t, v, true, "runner-resume")
		recovered, err := x.runner.RunTraversal(ctxFor(t))
		if err != nil || len(recovered.Visits) != 1 || recovered.Visits[0].TaskID != v.task.ID || recovered.Visits[0].Action != scheduler.ProjectVisitPending || recovered.Visits[0].Err != nil {
			t.Fatal("resumed pending traversal did not recover one original identity", err)
		}
		launch.requireAssociated(t, recovered.Visits[0].Dispatch)
		afterLaunches, afterLookups, _ = x.launcher.observed()
		if afterLaunches != launches || afterLookups != lookups+1 {
			t.Fatal("pending recovery resent Launch or bypassed original-key Lookup")
		}
		stopRunnerDuringRealAcquire(t, x)
	})
}

type projectRunnerFixture struct {
	v        *taskTransitionFixture
	runner   *scheduler.ProjectRunner
	handoff  *scheduler.LaunchHandoff
	launcher *countedSchedulerExecution
}

func projectRunnerRetryPolicy(t *testing.T) scheduler.LaunchRetryPolicy {
	t.Helper()
	config := schedulerRetryConfig(t, "2", "100ms", "1s")
	policy, ok := config.SchedulerLaunchRetryPolicy()
	if !ok || policy.Validate() != nil {
		t.Fatal("explicit original retry policy")
	}
	return policy
}

func newProjectRunnerFixture(t *testing.T, v *taskTransitionFixture, coordinator *scheduler.Coordinator) *projectRunnerFixture {
	t.Helper()
	handoff, counter := newSchedulerLaunchHandoff(t, v, true)
	busy, failure := newBusyCompensator(t, v), newSchedulerFailureFinalizer(t, v)
	visitor, err := scheduler.NewPendingVisitorWithFailure(v.pending, handoff, busy, failure)
	if err != nil {
		t.Fatal("real complete pending visitor", err)
	}
	t.Cleanup(func() {
		visitor.Stop()
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := visitor.Drain(ctx); err != nil || !visitor.Joined() {
			t.Error("original pending visitor did not join", err)
		}
	})
	reader, err := work.NewSchedulerTaskReader(v.base.tracked, v.authority)
	if err != nil {
		t.Fatal("same-Store actual Work traversal reader", err)
	}
	_, launchPolicy := schedulerClaimRequest(t, v)
	runner, err := scheduler.NewProjectRunner(coordinator, visitor, scheduler.ProjectRunnerOptions{
		ProjectID: v.base.project.ID, TickInterval: 20 * time.Millisecond, LaunchPolicy: launchPolicy, Tasks: reader,
	})
	if err != nil {
		t.Fatal("same-owner Project runner", err)
	}
	t.Cleanup(func() {
		runner.Stop()
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := runner.Drain(ctx); err != nil || !runner.Joined() {
			t.Error("original Project runner did not join", err)
		}
	})
	return &projectRunnerFixture{v: v, runner: runner, handoff: handoff, launcher: counter}
}

func createRunnerTodo(t *testing.T, v *taskTransitionFixture, key string, priority wc.TaskPriority) wc.Task {
	t.Helper()
	created, err := v.tasks.CreateTask(ctxFor(t), v.base.ownerBrowser.actor, meta(t, key+"-create", nil), v.base.project.ID, wc.TaskCreate{
		TaskID: id[wc.Task](t), SprintID: v.task.SprintID, Title: key, Type: wc.TaskTypeTask, Priority: priority,
		Description: "A real ordered runner input", Plan: "Use the original Claim and Launch services",
	})
	if err != nil || created.Validate() != nil {
		t.Fatal("real ordered Task creation", err)
	}
	assigned, err := v.transitions.TransferTask(ctxFor(t), v.base.ownerBrowser.actor, meta(t, key+"-assign", &created.Task.Version), v.base.project.ID, created.Task.ID,
		wc.TaskTransfer{TargetState: wc.TaskStateTodo, AssigneeAgentID: &v.agentID, AddBlockers: []wc.TaskBlockerCreate{}, ResolveBlockerIDs: []wc.TaskBlockerID{}})
	if err != nil || assigned.Validate() != nil || assigned.Task.State != wc.TaskStateTodo {
		t.Fatal("real ordered Task assignment", err)
	}
	return assigned.Task
}

func setRunnerLimit(t *testing.T, v *taskTransitionFixture, limit int64) {
	t.Helper()
	current, err := v.base.projects.GetSchedulerConfig(ctxFor(t), v.base.ownerBrowser.actor, v.base.project.ID)
	if err != nil {
		t.Fatal(err)
	}
	config := current.Config.Clone()
	config.MaxConcurrency = &limit
	updated, err := v.base.projects.UpdateProject(ctxFor(t), v.base.ownerBrowser.actor, meta(t, "runner-limit", &current.Project.Version), v.base.project.ID, pc.UpdateProjectRequest{Scheduler: &config})
	if err != nil {
		t.Fatal("formal Project concurrency configuration", err)
	}
	v.base.project = updated
}

func runnerSnapshot(t *testing.T, v *taskTransitionFixture) string {
	t.Helper()
	snapshot, err := schedulerClaimSnapshot(ctxFor(t), v.base.raw, v)
	if err != nil {
		t.Fatal("complete original runner business snapshot", err)
	}
	return snapshot
}

func rollbackRunnerAssociation(t *testing.T, x *schedulerLaunchFixture) {
	t.Helper()
	command, err := f.NewCommandIdentity("scheduler", []string{x.request.ProjectID.String()}, "associate_launch", x.request.Meta.IdempotencyKey)
	if err != nil {
		t.Fatal(err)
	}
	marker := errors.New("project-runner-original-association-rollback")
	store := x.task.base.tracked
	var observed, returned bool
	var physical f.CommitResult
	store.setAfter(func(ctx context.Context, tx f.Tx, cause f.TransactionCause) error {
		if cause.Kind() != f.CommandsCause || cause.Details().Primary.Canonical() != command.Canonical() {
			return nil
		}
		_, _, created := x.launcher.observed()
		if created.ID.Validate() != nil || created.Status != ec.Created {
			return errors.New("association preceded the original successful Execution return")
		}
		sql, err := store.InTx(tx)
		if err != nil {
			return err
		}
		if err := schedulerLaunchFacts(ctx, sql, x.request, created.ID, true); err != nil {
			return err
		}
		observed = true
		return marker
	})
	store.mu.Lock()
	store.afterResult = func(cause f.TransactionCause, result f.CommitResult) {
		if observed && cause.Kind() == f.CommandsCause && cause.Details().Primary.Canonical() == command.Canonical() {
			physical, returned = result, true
		}
	}
	store.mu.Unlock()
	clear := func() { store.setAfter(nil); store.mu.Lock(); store.afterResult = nil; store.mu.Unlock() }
	defer clear()
	_, err = x.handoff.LaunchOnce(ctxFor(t), x.request.ProjectID, x.dispatch)
	clear()
	if !observed || !returned || physical.State() != f.NotCommitted || !errors.Is(physical.Fault(), marker) || !errors.Is(err, marker) {
		t.Fatal("original association failure did not physically roll back", err)
	}
	launches, _, created := x.launcher.observed()
	if launches != 1 || created.ID.Validate() != nil {
		t.Fatal("original Execution did not actually commit exactly once")
	}
	if err := schedulerLaunchFacts(ctxFor(t), x.task.base.raw, x.request, created.ID, false); err != nil {
		t.Fatal("original pending/created facts did not survive association rollback", err)
	}
}

func stopRunnerDuringRealAcquire(t *testing.T, x *projectRunnerFixture) {
	t.Helper()
	before := runnerSnapshot(t, x.v)
	key, err := f.ProjectLock(x.v.base.project.ID.String())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	ready, release, holderDone := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var holderResult f.CommitResult
	transactionCause := cause(t)
	go func() {
		holderResult = x.v.base.raw.WithinTx(ctx, transactionCause, func(ctx context.Context, tx f.Tx) error {
			if err := x.v.base.raw.AcquireAll(ctx, tx, []f.LockRequest{{Key: key, Mode: f.Exclusive}}); err != nil {
				return err
			}
			close(ready)
			select {
			case <-release:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		})
		close(holderDone)
	}()
	var releaseOnce sync.Once
	joinHolder := func() {
		releaseOnce.Do(func() {
			close(release)
			<-holderDone
			cancel()
			if holderResult.State() != f.Committed {
				t.Error("original Project lock holder did not return committed")
			}
		})
	}
	t.Cleanup(joinHolder)
	defer joinHolder()
	select {
	case <-ready:
	case <-holderDone:
		t.Fatal("real Project EX holder failed")
	case <-ctx.Done():
		t.Fatal("real Project EX barrier expired")
	}
	entered := make(chan struct{}, 1)
	store := x.v.base.tracked
	store.mu.Lock()
	store.beforeLocks = func(_ context.Context, _ f.Tx, locks []f.LockRequest) error {
		for _, lock := range locks {
			if lock.Key.Canonical() == key.Canonical() {
				select {
				case entered <- struct{}{}:
				default:
				}
			}
		}
		return nil // The original Store owns acquisition and cancellation.
	}
	store.mu.Unlock()
	clear := func() { store.mu.Lock(); store.beforeLocks = nil; store.mu.Unlock() }
	defer clear()
	runDone := make(chan error, 1)
	go func() { runDone <- x.runner.Run(ctx) }()
	select {
	case <-entered:
	case err := <-runDone:
		t.Fatal("runner returned before entering its actual transaction", err)
	case <-ctx.Done():
		t.Fatal("runner never reached the original Store call")
	}
	x.runner.Stop()
	drainCtx, drainCancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer drainCancel()
	if err := x.runner.Drain(drainCtx); err != nil || !x.runner.Joined() {
		t.Fatal("runner did not join the actual cancelled transaction", err)
	}
	select {
	case err := <-runDone:
		if err == nil {
			t.Fatal("continuous runner hid cancellation of its active call")
		}
	case <-drainCtx.Done():
		t.Fatal("Drain preceded the original Run return")
	}
	clear()
	joinHolder()
	if before != runnerSnapshot(t, x.v) {
		t.Fatal("cancelled real Acquire leaked runner business writes")
	}
}
