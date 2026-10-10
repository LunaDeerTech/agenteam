package scheduler

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	wc "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
	"github.com/jackc/pgx/v5"
)

// These controls exercise the actual Runner, Coordinator and PendingVisitor.
// Work snapshot ordering and persisted Dispatches are controlled inputs, not
// evidence of real Work SQL, a successful Claim or an Execution Launch.
type runnerTestStore struct {
	*visitTestStore
	lastPendingRows *dispatchTestRows
}

func (s *runnerTestStore) InTx(tx f.Tx) (postgres.SQLExecutor, error) {
	if _, err := s.visitTestStore.InTx(tx); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *runnerTestStore) QueryRow(ctx context.Context, query string, args ...any) postgres.Row {
	if query == traversalTaskPendingSQL {
		for _, row := range s.rows {
			if row.id == s.row.id && s.staged != nil {
				row = s.staged
			}
			if len(args) == 2 && args[0] == row.project.String() && args[1] == row.task && row.status == Pending {
				return dispatchTestRow{values: recordValues(s.t, row)}
			}
		}
		return dispatchTestRow{err: pgx.ErrNoRows}
	}
	if query == `SELECT `+dispatchColumns+` FROM agenteam_scheduler.dispatches WHERE project_id=$1 AND id=$2` {
		for _, row := range s.rows {
			if row.id == s.row.id && s.staged != nil {
				row = s.staged
			}
			if len(args) == 2 && args[0] == row.project.String() && args[1] == row.id.String() {
				return dispatchTestRow{values: recordValues(s.t, row)}
			}
		}
		return dispatchTestRow{err: pgx.ErrNoRows}
	}
	return s.visitTestStore.QueryRow(ctx, query, args...)
}

type runnerTestReader struct {
	store      *runnerTestStore
	project    *visitTestProject
	entries    []wc.SchedulerTaskIdentity
	facts      map[wc.TaskID]wc.SchedulerTaskFacts
	reads      []wc.TaskID
	readAt     []time.Time
	snapshots  int
	onSnapshot func(context.Context)
	onCurrent  func(context.Context, wc.TaskID)
}

func (r *runnerTestReader) SnapshotInTx(ctx context.Context, tx f.Tx, project i.ProjectID) (wc.SchedulerTaskSnapshot, error) {
	if _, err := r.store.InTx(tx); err != nil {
		return wc.SchedulerTaskSnapshot{}, err
	}
	if err := r.store.RequireHeldLocks(ctx, tx, pendingLocks(project)); err != nil {
		return wc.SchedulerTaskSnapshot{}, err
	}
	r.snapshots++
	if r.onSnapshot != nil {
		r.onSnapshot(ctx)
	}
	return (wc.SchedulerTaskSnapshot{ProjectID: project, CurrentSprintID: r.project.sprint, Entries: r.entries}).Clone(), ctx.Err()
}

func (r *runnerTestReader) CurrentTaskInTx(ctx context.Context, tx f.Tx, project i.ProjectID, task wc.TaskID) (wc.SchedulerTaskFacts, error) {
	if _, err := r.store.InTx(tx); err != nil {
		return wc.SchedulerTaskFacts{}, err
	}
	key, _ := f.AggregateLock(f.TaskAggregate, task.String())
	locks := append(pendingLocks(project), f.LockRequest{Key: key, Mode: f.Shared})
	if err := r.store.RequireHeldLocks(ctx, tx, locks); err != nil {
		return wc.SchedulerTaskFacts{}, err
	}
	r.reads, r.readAt = append(r.reads, task), append(r.readAt, time.Now())
	if r.onCurrent != nil {
		r.onCurrent(ctx, task)
	}
	v, ok := r.facts[task]
	if !ok {
		return wc.SchedulerTaskFacts{}, fault(f.TaskNotFound)
	}
	return v.Clone(), ctx.Err()
}

type runnerTestClaims struct{ requests []wc.TaskClaimRequest }

func (w *runnerTestClaims) DiscoverTaskClaim(_ context.Context, _ i.Actor, request wc.TaskClaimRequest) (wc.TaskClaimPlan, error) {
	w.requests = append(w.requests, request.Clone())
	// The pure traversal can observe a real public Claim attempt, but does
	// not manufacture an applied Work witness or a successful mutation.
	return nil, fault(f.DependencyUnbound)
}
func (*runnerTestClaims) ApplyTaskClaimInTx(context.Context, f.Tx, i.Actor, wc.TaskClaimRequest, wc.TaskClaimPlan) (wc.AppliedTaskClaim, error) {
	panic("controlled discovery must not apply Work")
}
func (*runnerTestClaims) CheckTaskClaimAppliedInTx(context.Context, f.Tx, i.Actor, wc.TaskClaimRequest, wc.TaskClaimPlan, wc.AppliedTaskClaim) error {
	panic("controlled discovery must not mint an applied witness")
}

type runnerTestFixture struct {
	runner      *ProjectRunner
	coordinator *Coordinator
	visitor     *PendingVisitor
	store       *runnerTestStore
	project     *visitTestProject
	reader      *runnerTestReader
	claims      *runnerTestClaims
	execution   *handoffTestExecution
	options     ProjectRunnerOptions
}

func newRunnerTest(t *testing.T, tick time.Duration) *runnerTestFixture {
	t.Helper()
	old, base, project, execution, work := newVisitTest(t)
	store := &runnerTestStore{visitTestStore: base}
	store.rows = nil
	a, err := NewPendingAuthority(store)
	if err != nil {
		t.Fatal(err)
	}
	old.busy.authority, work.authority, execution.authority = a, a, a
	handoff, err := NewLaunchHandoff(a, LaunchHandoffDependencies{Executions: execution, Observations: execution})
	if err != nil {
		t.Fatal(err)
	}
	visitor, err := NewPendingVisitor(a, handoff, old.busy)
	if err != nil {
		t.Fatal(err)
	}
	claims := &runnerTestClaims{}
	coordinator, err := NewCoordinator(a, CoordinatorDependencies{Projects: project, Claims: claims, Executions: execution, Capacity: claimTestCapacity(func(context.Context, f.Tx, i.ProjectID, []ec.AssociatedDispatch) (int64, error) {
		return 0, fault(f.DependencyUnbound)
	})})
	if err != nil {
		t.Fatal(err)
	}
	reader := &runnerTestReader{store: store, project: project, entries: []wc.SchedulerTaskIdentity{}, facts: make(map[wc.TaskID]wc.SchedulerTaskFacts)}
	options := ProjectRunnerOptions{ProjectID: store.row.project, TickInterval: tick, LaunchPolicy: emptyClaimPolicy(), Tasks: reader}
	runner, err := NewProjectRunner(coordinator, visitor, options)
	if err != nil {
		t.Fatal(err)
	}
	// Only the concrete postgres.Rows acquisition is substituted. The
	// production collector still owns Scan/EOF/Close, and capture keeps its
	// original Store transaction, Project gate and complete held lock set.
	runner.readPending = func(ctx context.Context, x postgres.SQLExecutor, p i.ProjectID) ([]traversalPending, error) {
		if x != store || p != options.ProjectID {
			return nil, fault(f.Forbidden)
		}
		rows := &dispatchTestRows{}
		for _, row := range store.rows {
			if row.status == Pending {
				rows.rows = append(rows.rows, dispatchTestRow{values: []any{row.id.String(), row.task}})
			}
		}
		store.lastPendingRows = rows
		return collectTraversalPending(ctx, rows)
	}
	v := &runnerTestFixture{runner, coordinator, visitor, store, project, reader, claims, execution, options}
	t.Cleanup(func() {
		runner.Stop()
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := runner.Drain(ctx); err != nil {
			t.Error("Runner did not join its actual call", err)
		}
	})
	return v
}

func (v *runnerTestFixture) addTask(t *testing.T, n int, state wc.TaskState, priority wc.TaskPriority) wc.TaskID {
	t.Helper()
	id := dispatchTestID[wc.Task](t, n)
	v.reader.entries = append(v.reader.entries, wc.SchedulerTaskIdentity{TaskID: id, State: state})
	v.reader.facts[id] = wc.SchedulerTaskFacts{ProjectID: v.options.ProjectID, TaskID: id, MilestoneID: dispatchTestID[wc.Milestone](t, 701), SprintID: *v.project.sprint, State: state, Priority: priority, Version: 7, AssigneeAgentID: &v.store.row.agent}
	return id
}

func TestSchedulerProjectRunnerConsumesFixedSnapshotOrder(t *testing.T) {
	const tick = 2 * time.Millisecond
	v := newRunnerTest(t, tick)
	// The reader owns priority/manual-rank ordering. In particular, the two
	// equal-priority identities deliberately do not follow UUID order.
	want := []wc.TaskID{
		v.addTask(t, 509, wc.TaskStateTodo, wc.TaskPriorityCritical),
		v.addTask(t, 507, wc.TaskStateTodo, wc.TaskPriorityHigh),
		v.addTask(t, 501, wc.TaskStateTodo, wc.TaskPriorityHigh),
		v.addTask(t, 601, wc.TaskStateInProgress, wc.TaskPriorityLow),
		v.addTask(t, 602, wc.TaskStateInReview, wc.TaskPriorityHigh),
		v.addTask(t, 603, wc.TaskStateBlocked, wc.TaskPriorityCritical),
	}
	out, err := v.runner.RunTraversal(context.Background())
	if err != nil || out.ProjectID != v.options.ProjectID || out.CurrentSprintID == nil || *out.CurrentSprintID != *v.project.sprint || len(out.Visits) != len(want) || !reflect.DeepEqual(v.reader.reads, want) || v.reader.snapshots != 1 {
		t.Fatal("fixed reader order was lost or a Task was visited twice", err)
	}
	if len(v.claims.requests) != 3 || v.execution.launches != 0 {
		t.Fatal("unsupported groups or refused claims dispatched")
	}
	for n, visit := range out.Visits {
		if visit.TaskID != want[n] || visit.ExpectedState != v.reader.entries[n].State {
			t.Fatal("snapshot identity/group changed")
		}
		if n < 3 {
			request := v.claims.requests[n]
			if visit.Action != ProjectVisitClaim || !runnerHasCode(visit.Err, f.DependencyUnbound) || request.TaskID != want[n] || request.ExpectedTaskVersion != 7 || request.AgentID != v.store.row.agent || request.CurrentSprintID != *v.project.sprint || request.Validate() != nil {
				t.Fatal("Claim did not receive the current typed facts")
			}
		} else if visit.Action != ProjectVisitDeferred || visit.Err != nil {
			t.Fatal("relaunch/review/blocked gained an unbound action")
		}
	}
	assertRunnerTicks(t, v.reader.readAt, time.Now(), tick)
}

func TestSchedulerProjectRunnerPrioritizesHistoricalPending(t *testing.T) {
	for _, noCurrent := range []bool{false, true} {
		name := "old-sprint-terminal-and-deduplicated"
		if noCurrent {
			name = "no-current-pure-control"
		}
		t.Run(name, func(t *testing.T) {
			v := newRunnerTest(t, time.Millisecond)
			var pendingTasks []wc.TaskID
			for n := 1; n <= 3; n++ {
				r := dispatchTestRecord(t, n, Pending)
				r.outcome = KnownNotCreated // Legacy refusal: not a retry/finalize proof.
				r.sprint = dispatchTestID[pc.Sprint](t, 800+n).String()
				v.store.rows = append(v.store.rows, r)
				task, _ := f.ParseID[wc.Task](r.task)
				pendingTasks = append(pendingTasks, task)
				v.reader.facts[task] = wc.SchedulerTaskFacts{ProjectID: r.project, TaskID: task, State: []wc.TaskState{wc.TaskStateDone, wc.TaskStateCancelled, wc.TaskStateBacklog}[n-1]}
			}
			v.store.row = v.store.rows[0]
			var fresh wc.TaskID
			if noCurrent {
				v.project.sprint = nil
			} else {
				v.reader.entries = append(v.reader.entries, wc.SchedulerTaskIdentity{TaskID: pendingTasks[0], State: wc.TaskStateTodo})
				fresh = v.addTask(t, 900, wc.TaskStateInReview, wc.TaskPriorityHigh)
			}
			out, err := v.runner.RunTraversal(context.Background())
			wantCount := 3
			if !noCurrent {
				wantCount++
			}
			if err != nil || len(out.Visits) != wantCount || len(v.claims.requests) != 0 || v.execution.launches != 0 || v.execution.lookups != 0 || v.store.lastPendingRows == nil || !v.store.lastPendingRows.closed {
				t.Fatal("historical pending was hidden, repeated or granted unsupported work", err)
			}
			for n, task := range pendingTasks {
				visit := out.Visits[n]
				if visit.TaskID != task || visit.ExpectedState != "" || visit.Action != ProjectVisitPending || visit.Err != nil {
					t.Fatal("pending prefix lost durable order or depended on the current Work group")
				}
			}
			if noCurrent {
				if out.CurrentSprintID != nil || len(v.reader.reads) != 0 {
					t.Fatal("nil Current Sprint caused new Task scheduling")
				}
			} else if !reflect.DeepEqual(v.reader.reads, []wc.TaskID{fresh}) || out.Visits[3].TaskID != fresh {
				t.Fatal("snapshot duplicate was revisited after its pending prefix")
			}
		})
	}
}

func TestSchedulerProjectRunnerRechecksScopeAndPacesSkips(t *testing.T) {
	const tick = 3 * time.Millisecond
	for _, change := range []string{"changed-state", "missing-task", "initially-paused", "pause", "sprint"} {
		t.Run(change, func(t *testing.T) {
			v := newRunnerTest(t, tick)
			first := v.addTask(t, 501, wc.TaskStateTodo, wc.TaskPriorityHigh)
			second := v.addTask(t, 502, wc.TaskStateInReview, wc.TaskPriorityLow)
			if change == "changed-state" {
				facts := v.reader.facts[first]
				facts.State = wc.TaskStateDone
				v.reader.facts[first] = facts
			} else if change == "missing-task" {
				delete(v.reader.facts, first)
			} else if change == "initially-paused" {
				v.project.enabled = false
			} else {
				// Mutate the controlled current Project after capture, before
				// the first entry is read. No stale snapshot is a permission.
				v.reader.onSnapshot = func(context.Context) {
					v.project.onRead = func(int) {
						if change == "pause" {
							v.project.enabled = false
						} else {
							other := dispatchTestID[pc.Sprint](t, 999)
							v.project.sprint = &other
						}
					}
				}
			}
			start := time.Now()
			out, err := v.runner.RunTraversal(context.Background())
			if err != nil || len(v.claims.requests) != 0 || v.execution.launches != 0 {
				t.Fatal("a changed or absent current fact enabled scheduling", err)
			}
			if change == "changed-state" || change == "missing-task" {
				if len(out.Visits) != 2 || out.Visits[0].TaskID != first || out.Visits[1].TaskID != second || !reflect.DeepEqual(v.reader.reads, []wc.TaskID{first, second}) {
					t.Fatal("ordinary skip starved the remaining snapshot")
				}
				assertRunnerTicks(t, v.reader.readAt, time.Now(), tick)
			} else if change == "initially-paused" {
				if len(out.Visits) != 2 || len(v.reader.reads) != 0 || time.Since(start) < 2*tick || out.Visits[0].Action != ProjectVisitDeferred || out.Visits[1].Action != ProjectVisitDeferred {
					t.Fatal("paused snapshot mutated, disappeared or skipped normal pacing")
				}
			} else if len(out.Visits) != 1 || len(v.reader.reads) != 0 || time.Since(start) < tick {
				t.Fatal("pause/Sprint change continued the stale traversal or omitted its tick")
			}
		})
	}
	// A continuous empty runner must yield a full tick before rebuilding.
	v := newRunnerTest(t, tick)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var captures []time.Time
	v.reader.onSnapshot = func(context.Context) {
		captures = append(captures, time.Now())
		if len(captures) == 3 {
			cancel()
		}
	}
	if err := v.runner.Run(ctx); !errors.Is(err, context.Canceled) || len(captures) != 3 || len(v.reader.reads) != 0 {
		t.Fatal("continuous empty traversal did not rebuild and stop", err)
	}
	for n := 1; n < len(captures); n++ {
		if captures[n].Sub(captures[n-1]) < tick {
			t.Fatal("empty traversal spun without a full tick")
		}
	}
}

func assertRunnerTicks(t *testing.T, starts []time.Time, returned time.Time, tick time.Duration) {
	t.Helper()
	for n := 1; n < len(starts); n++ {
		if starts[n].Sub(starts[n-1]) < tick {
			t.Fatal("next Task started before the preceding full tick")
		}
	}
	if len(starts) > 0 && returned.Sub(starts[len(starts)-1]) < tick {
		t.Fatal("last Task did not receive its full tick")
	}
}

func TestSchedulerProjectRunnerSerialAdmissionAndActualJoin(t *testing.T) {
	t.Run("held-reader", func(t *testing.T) {
		v := newRunnerTest(t, time.Hour)
		entered, cancelled, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
		var releaseOnce sync.Once
		v.reader.onSnapshot = func(ctx context.Context) {
			close(entered)
			<-ctx.Done()
			close(cancelled)
			<-release // Cancellation is not physical return of the borrowed read.
		}
		result := make(chan error, 1)
		joined := make(chan struct{})
		go func() {
			defer close(joined)
			result <- v.runner.Run(context.Background())
		}()
		t.Cleanup(func() {
			v.runner.Stop()
			releaseOnce.Do(func() { close(release) })
			awaitRunnerSignal(t, joined)
		})
		awaitRunnerSignal(t, entered)
		options := v.options
		options.TickInterval = time.Millisecond
		other, err := NewProjectRunner(v.coordinator, v.visitor, options)
		if err != nil {
			t.Fatal(err)
		}
		defer other.Stop()
		if _, err := other.RunTraversal(context.Background()); !runnerHasCode(err, f.ResourceBusy) {
			t.Fatal("another Runner overlapped the same Authority/Project", err)
		}
		if err := v.runner.Run(context.Background()); !runnerHasCode(err, f.ResourceBusy) {
			t.Fatal("continuous Run lost whole-call admission", err)
		}
		v.runner.Stop()
		awaitRunnerSignal(t, cancelled)
		cancelledContext, cancel := context.WithCancel(context.Background())
		cancel()
		if err := v.runner.Drain(cancelledContext); !errors.Is(err, context.Canceled) || v.runner.Joined() {
			t.Fatal("Stop claimed a held original read had returned", err)
		}
		select {
		case <-joined:
			t.Fatal("Run returned before its borrowed read")
		default:
		}
		releaseOnce.Do(func() { close(release) })
		awaitRunnerSignal(t, joined)
		if err := <-result; !errors.Is(err, context.Canceled) {
			t.Fatal("original cancellation was lost", err)
		}
		ctx, finish := context.WithTimeout(context.Background(), time.Second)
		defer finish()
		if err := v.runner.Drain(ctx); err != nil || !v.runner.Joined() || v.visitor.Joined() || v.visitor.handoff.Joined() {
			t.Fatal("Runner either failed to join or retired a borrowed owner", err)
		}
		// Admission is released after physical return, not permanently owned
		// by the stopped Runner. No SQL/Work success is implied by this read.
		other.readPending = v.runner.readPending
		v.reader.onSnapshot = nil
		if _, err := other.RunTraversal(context.Background()); err != nil {
			t.Fatal("Project registration survived the original joined call", err)
		}
	})
	t.Run("long-tick-cancel", func(t *testing.T) {
		v := newRunnerTest(t, time.Hour)
		v.addTask(t, 501, wc.TaskStateInReview, wc.TaskPriorityLow)
		read := make(chan struct{})
		v.reader.onCurrent = func(context.Context, wc.TaskID) { close(read) }
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		result := make(chan error, 1)
		joined := make(chan struct{})
		go func() {
			defer close(joined)
			_, err := v.runner.RunTraversal(ctx)
			result <- err
		}()
		t.Cleanup(func() { cancel(); awaitRunnerSignal(t, joined) })
		awaitRunnerSignal(t, read)
		// There must be no normal return during this deliberately long tick.
		probe := time.NewTimer(5 * time.Millisecond)
		defer probe.Stop()
		select {
		case <-joined:
			t.Fatal("Traversal skipped its final tick")
		case <-probe.C:
		}
		cancel()
		awaitRunnerSignal(t, joined)
		if err := <-result; !errors.Is(err, context.Canceled) || len(v.reader.reads) != 1 || v.execution.launches != 0 {
			t.Fatal("long tick ignored cancellation or produced new work", err)
		}
	})
	t.Run("unknown-keeps-original-owner", func(t *testing.T) {
		v := newRunnerTest(t, time.Millisecond)
		v.store.row.outcome, v.store.row.attempts, v.store.row.busyAttempt = NotSent, 0, 0
		v.store.rows = []*dispatchRecord{v.store.row}
		v.store.failName, v.store.failMode = "launch_handoff", "unknown-after"
		v.addTask(t, 700, wc.TaskStateInReview, wc.TaskPriorityLow)
		err := v.runner.Run(context.Background())
		var reported *ProjectRunError
		original, ok := UnknownAttempt(err)
		if !errors.As(err, &reported) || !ok || original.AttemptID() != v.store.attempt || v.execution.launches != 0 || len(v.reader.reads) != 0 {
			t.Fatal("uncertain marker sent or silently continued the traversal", err)
		}
		observation := reported.Observation()
		if len(observation.Visits) != 1 || observation.Visits[0].DispatchID == nil || *observation.Visits[0].DispatchID != v.store.row.id || observation.Visits[0].Action != ProjectVisitPending {
			t.Fatal("original unknown recovery identity was lost")
		}
		if strings.Contains(err.Error(), v.store.row.id.String()) || strings.Contains(err.Error(), string(v.store.row.launch.Meta.IdempotencyKey)) {
			t.Fatal("default Run error exposed the original recovery identity")
		}
		*observation.Visits[0].DispatchID = dispatchTestID[DispatchIdentity](t, 999)
		if again := reported.Observation(); again.Visits[0].DispatchID == nil || *again.Visits[0].DispatchID != v.store.row.id {
			t.Fatal("caller mutation changed the retained observation")
		}
		_, next := v.runner.RunTraversal(context.Background())
		nextAttempt, known := UnknownAttempt(next)
		if !known || nextAttempt.AttemptID() != original.AttemptID() || v.execution.launches != 0 || v.execution.lookups != 1 || len(v.reader.reads) != 0 {
			t.Fatal("new traversal replaced original-key lookup with a send", next)
		}
		v.runner.Stop()
		if !v.runner.Joined() {
			t.Fatal("returned traversal retained its registration")
		}
		v.visitor.handoff.Stop()
		if v.visitor.handoff.Joined() {
			t.Fatal("Runner stop retired the original unresolved handoff")
		}
		v.execution.created = visitCreated(t, v.store.row)
		out, resolved := v.visitor.handoff.Lookup(context.Background(), v.options.ProjectID, v.store.row.id)
		if resolved != nil || out.Summary().Status != Launched || !v.visitor.handoff.Joined() || v.execution.launches != 0 || v.execution.lookups != 2 {
			t.Fatal("original owner failed to recover without sending", resolved)
		}
	})
}

func runnerHasCode(err error, code f.Code) bool {
	var known *f.Fault
	return errors.As(err, &known) && known.Code == code
}

func awaitRunnerSignal(t *testing.T, done <-chan struct{}) {
	t.Helper()
	timer := time.NewTimer(2 * time.Second)
	defer timer.Stop()
	select {
	case <-done:
	case <-timer.C:
		t.Fatal("controlled original call did not reach its bounded checkpoint")
	}
}
