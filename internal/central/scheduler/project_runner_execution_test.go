package scheduler

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
)

// These controls reuse the actual Runner, page collector and association
// checks. Stored rows and the Execution observer/executor remain explicit
// test inputs; they do not prove a real Launch, runtime authority or DB commit.
type runnerExecutionControl struct {
	requests []ec.AssociatedDispatch
	status   map[i.ExecutionID]ec.Status
	failID   i.ExecutionID
	onCall   func(context.Context)
	stops    int
	drains   int
}

func (v *runnerExecutionControl) Advance(ctx context.Context, project i.ProjectID, input ec.AssociatedDispatch) (ec.ExecutionAdvance, error) {
	v.requests = append(v.requests, input)
	if v.onCall != nil {
		v.onCall(ctx)
	}
	if input.ExecutionID == v.failID {
		return ec.ExecutionAdvance{}, fault(f.DependencyUnavailable)
	}
	if err := ctx.Err(); err != nil {
		return ec.ExecutionAdvance{}, err
	}
	status := v.status[input.ExecutionID]
	if status == ec.Succeeded || status == ec.Failed || status == ec.Cancelled {
		return ec.ExecutionAdvance{ProjectID: project, ExecutionID: input.ExecutionID, Phase: ec.ExecutionTerminal, Status: status, Joined: true}, nil
	}
	return ec.ExecutionAdvance{ProjectID: project, ExecutionID: input.ExecutionID, Phase: ec.ExecutionAccepted, Status: status, Active: true}, nil
}

// Extra lifecycle methods detect accidental ownership of the borrowed port.
func (v *runnerExecutionControl) Stop() { v.stops++ }
func (v *runnerExecutionControl) Drain(context.Context) error {
	v.drains++
	return nil
}

type runnerExecutionObserver struct {
	ec.DispatchObserver
	fixture *runnerTestFixture
	status  map[i.ExecutionID]ec.Status
	reads   []DispatchID
}

func (v *runnerExecutionObserver) LookupLaunchInTx(ctx context.Context, tx f.Tx, key ec.LaunchLookupKey, digest f.Digest, dispatch string) (ec.LaunchLookup, error) {
	for _, row := range v.fixture.store.rows {
		if row.id.String() != dispatch {
			continue
		}
		if row.execution == nil || key != lookupKey(row) || digest != row.digest {
			return ec.LaunchLookup{}, fault(f.ConfirmationStale)
		}
		if err := v.fixture.store.RequireHeldLocks(ctx, tx, executionIntentLocks(row)); err != nil {
			return ec.LaunchLookup{}, err
		}
		v.reads = append(v.reads, row.id)
		status := v.status[*row.execution]
		value := ec.Summary{ID: *row.execution, ProjectID: row.project, AgentID: row.agent, Trigger: row.launch.Trigger, Purpose: row.launch.Purpose, Status: status, Version: 1, CreatedAt: row.createdAt}
		if status == ec.Succeeded || status == ec.Failed || status == ec.Cancelled {
			at := row.createdAt
			value.CompletedAt = &at
			value.Version = 4
		}
		return ec.LaunchLookup{Found: true, RequestDigest: digest, Execution: &value}, ctx.Err()
	}
	return ec.LaunchLookup{}, fault(f.ConfirmationStale)
}

type runnerExecutionFixture struct {
	base       *runnerTestFixture
	runner     *ProjectRunner
	executor   *runnerExecutionControl
	observer   *runnerExecutionObserver
	pages      []*dispatchTestRows
	after      []string
	through    []string
	pageError  error
	closeError error
}

type runnerExecutionStore struct {
	*runnerTestStore
}

func (s *runnerExecutionStore) InTx(tx f.Tx) (postgres.SQLExecutor, error) {
	if _, err := s.runnerTestStore.InTx(tx); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *runnerExecutionStore) QueryRow(ctx context.Context, query string, args ...any) postgres.Row {
	if query != traversalLaunchedHighWaterSQL {
		return s.runnerTestStore.QueryRow(ctx, query, args...)
	}
	if len(args) != 1 || args[0] != s.row.project.String() {
		return dispatchTestRow{err: errors.New("controlled high-water project mismatch")}
	}
	var highest *string
	for _, row := range s.rows {
		if row.status == Launched && (highest == nil || row.id.String() > *highest) {
			id := row.id.String()
			highest = &id
		}
	}
	return dispatchTestRow{values: []any{highest}}
}

func newRunnerExecutionControl(t *testing.T, pageSize int, tick time.Duration) *runnerExecutionFixture {
	t.Helper()
	base := newRunnerTest(t, tick)
	store := &runnerExecutionStore{runnerTestStore: base.store}
	// Wrap only SQL row acquisition; the existing Store still owns the actual
	// controlled transaction identity, held-lock checks and physical outcome.
	base.coordinator.authority.store = store
	executor := &runnerExecutionControl{}
	observer := &runnerExecutionObserver{DispatchObserver: base.execution, fixture: base, status: make(map[i.ExecutionID]ec.Status)}
	executor.status = observer.status
	base.coordinator.deps.Executions = observer
	runner, err := NewProjectRunnerWithExecutions(base.coordinator, base.visitor, base.options, executor, pageSize)
	if err != nil {
		t.Fatal(err)
	}
	runner.readPending = func(ctx context.Context, x postgres.SQLExecutor, project i.ProjectID) ([]traversalPending, error) {
		if x != store {
			t.Fatal("pending read left the original Store wrapper")
		}
		return base.runner.readPending(ctx, base.store, project)
	}
	v := &runnerExecutionFixture{base: base, runner: runner, executor: executor, observer: observer}
	runner.readLaunched = func(ctx context.Context, x postgres.SQLExecutor, project i.ProjectID, after, through string, limit int) ([]*dispatchRecord, error) {
		if x != store || project != base.options.ProjectID || limit != pageSize+1 {
			t.Fatal("page lost the original bounded caller transaction")
		}
		v.after = append(v.after, after)
		v.through = append(v.through, through)
		if v.pageError != nil {
			return nil, v.pageError
		}
		rows := &dispatchTestRows{closeErr: v.closeError}
		input := slices.Clone(base.store.rows)
		slices.SortFunc(input, func(a, b *dispatchRecord) int { return strings.Compare(a.id.String(), b.id.String()) })
		for _, row := range input {
			if row.status == Launched && row.id.String() > after && row.id.String() <= through {
				rows.rows = append(rows.rows, dispatchTestRow{values: recordValues(t, row)})
				if len(rows.rows) == limit {
					break
				}
			}
		}
		v.pages = append(v.pages, rows)
		return collectTraversalLaunched(ctx, rows, project, after, through, limit)
	}
	t.Cleanup(func() {
		runner.Stop()
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := runner.Drain(ctx); err != nil {
			t.Error("execution-aware Runner did not return", err)
		}
	})
	return v
}

func (v *runnerExecutionFixture) add(t *testing.T, n int, status ec.Status) *dispatchRecord {
	t.Helper()
	row := dispatchTestRecord(t, n, Launched)
	v.base.store.rows = append(v.base.store.rows, row)
	if len(v.base.store.rows) == 1 {
		v.base.store.row = row
	}
	v.observer.status[*row.execution] = status
	return row
}

func TestSchedulerProjectRunnerExecutionPagesReachAndWrapHistory(t *testing.T) {
	v := newRunnerExecutionControl(t, 2, time.Millisecond)
	first := v.add(t, 1, ec.Succeeded)
	second := v.add(t, 2, ec.Cancelled)
	later := v.add(t, 3, ec.Created)
	var appended *dispatchRecord
	for n := 0; n < 4; n++ {
		want := []*dispatchRecord{first, second}
		if n == 1 {
			want = []*dispatchRecord{later}
		} else if n == 3 {
			want = []*dispatchRecord{later, appended}
		}
		out, err := v.runner.RunTraversal(context.Background())
		if err != nil || len(out.Executions) != len(want) || len(out.Visits) != 0 {
			t.Fatal("bounded association page lost a historical entry", err)
		}
		for index, row := range want {
			visit := out.Executions[index]
			if visit.DispatchID != row.id || visit.ExecutionID != *row.execution || visit.Execution.Status != v.observer.status[*row.execution] || visit.Advance.ExecutionID != *row.execution || visit.Err != nil {
				t.Fatal("association observation changed identity or status")
			}
		}
		if n == 1 || n == 3 {
			if v.runner.executionAfter != nil || v.runner.executionThrough != nil {
				t.Fatal("observed page end did not wrap")
			}
		} else if v.runner.executionAfter == nil || *v.runner.executionAfter != second.id {
			t.Fatal("terminal prefix did not advance its bounded cursor")
		}
		if n == 0 {
			// A newer association must not move the current cycle's boundary.
			// It becomes visible only after the original end has wrapped.
			appended = v.add(t, 4, ec.Created)
		}
	}
	if !slices.Equal(v.after, []string{"", second.id.String(), "", second.id.String()}) || !slices.Equal(v.through, []string{later.id.String(), later.id.String(), appended.id.String(), appended.id.String()}) || len(v.executor.requests) != 7 || v.executor.requests[2].ExecutionID != *later.execution || v.executor.requests[6].ExecutionID != *appended.execution {
		t.Fatal("terminal history permanently hid the later created execution")
	}
	for _, page := range v.pages {
		if !page.closed {
			t.Fatal("page published before its actual rows closed")
		}
	}
	// Both page delivery and a newly linked Dispatch use this same production
	// helper and per-traversal set. The second identical delivery does not
	// reobserve or readmit the Execution; it is not another Launch permission.
	out := ProjectRunResult{ProjectID: v.base.options.ProjectID}
	seen := make(map[i.ExecutionID]DispatchID)
	reads, advances := len(v.observer.reads), len(v.executor.requests)
	for n := 0; n < 2; n++ {
		if err := v.runner.advanceExecution(context.Background(), snapshot(later), &out, seen); err != nil {
			t.Fatal(err)
		}
	}
	if len(out.Executions) != 1 || len(v.observer.reads) != reads+1 || len(v.executor.requests) != advances+1 || seen[*later.execution] != later.id {
		t.Fatal("same original Execution was delivered twice in one traversal")
	}
}

func TestSchedulerProjectRunnerExecutionFailuresPreserveCursor(t *testing.T) {
	v := newRunnerExecutionControl(t, 2, time.Millisecond)
	first := v.add(t, 1, ec.Created)
	second := v.add(t, 2, ec.Created)
	third := v.add(t, 3, ec.Created)
	v.executor.failID = *second.execution
	out, err := v.runner.RunTraversal(context.Background())
	if err == nil || len(out.Executions) != 2 || out.Executions[1].Err == nil || v.runner.executionAfter == nil || *v.runner.executionAfter != first.id || len(v.executor.requests) != 2 {
		t.Fatal("failed admission consumed its cursor or skipped to later history", err)
	}
	v.executor.failID = i.ExecutionID{}
	out, err = v.runner.RunTraversal(context.Background())
	if err != nil || len(out.Executions) != 2 || out.Executions[0].DispatchID != second.id || out.Executions[1].DispatchID != third.id || len(v.executor.requests) != 4 || v.executor.requests[2].ExecutionID != *second.execution || v.runner.executionAfter != nil {
		t.Fatal("next explicit traversal did not resume at the failed association", err)
	}
	for _, closeFailure := range []bool{false, true} {
		cause := errors.New("controlled association read failure")
		v.pageError, v.closeError = nil, nil
		if closeFailure {
			v.closeError = cause
		} else {
			v.pageError = cause
		}
		before := len(v.executor.requests)
		out, err = v.runner.RunTraversal(context.Background())
		if err == nil || len(out.Executions) != 0 || v.runner.executionAfter != nil || v.runner.executionThrough != nil || len(v.executor.requests) != before {
			t.Fatal("failed page read published partial work or moved its cursor", err)
		}
		if closeFailure && !v.pages[len(v.pages)-1].closed {
			t.Fatal("failed page left its original rows open")
		}
	}
}

func TestSchedulerProjectRunnerExecutionPauseAndBorrowedLifetime(t *testing.T) {
	v := newRunnerExecutionControl(t, 1, time.Millisecond)
	row := v.add(t, 1, ec.Created)
	v.base.project.enabled, v.base.project.sprint = false, nil
	out, err := v.runner.RunTraversal(context.Background())
	if err != nil || len(out.Executions) != 1 || out.Executions[0].ExecutionID != *row.execution || len(v.executor.requests) != 1 || len(v.base.claims.requests) != 0 || v.base.execution.launches != 0 || len(v.base.reader.reads) != 0 {
		t.Fatal("pause or missing Current Sprint hid an existing association or started new scheduling", err)
	}
	for _, size := range []int{0, MaxExecutionHandoffPage + 1} {
		if runner, err := NewProjectRunnerWithExecutions(v.base.coordinator, v.base.visitor, v.base.options, v.executor, size); err == nil || runner != nil {
			t.Fatal("unbounded association page accepted")
		}
	}
	var missing *runnerExecutionControl
	if runner, err := NewProjectRunnerWithExecutions(v.base.coordinator, v.base.visitor, v.base.options, missing, 1); err == nil || runner != nil {
		t.Fatal("missing execution consumer accepted")
	}
	entered := make(chan struct{})
	v.executor.onCall = func(ctx context.Context) {
		close(entered)
		<-ctx.Done()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	returned := make(chan error, 1)
	go func() {
		_, err := v.runner.RunTraversal(ctx)
		returned <- err
	}()
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal("original Advance did not enter")
	}
	v.runner.Stop()
	select {
	case err := <-returned:
		if !errors.Is(err, context.Canceled) {
			t.Fatal("Runner lost the original admission cancellation", err)
		}
	case <-ctx.Done():
		t.Fatal("Runner did not join its original Advance")
	}
	if err = v.runner.Drain(ctx); err != nil || !v.runner.Joined() || v.executor.stops != 0 || v.executor.drains != 0 {
		t.Fatal("Runner took ownership of the shared executor lifetime", err)
	}
}
