package scheduler

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	wc "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

var relaunchAdmissionReached = errors.New("controlled relaunch admission reached")

// Physical commit outcomes and SQL rows are explicit controls. The actual
// visitor, lock checks, repository encoding/scanning and immutable receipt
// recovery are exercised; this is not evidence that PostgreSQL committed.
type relaunchTestStore struct {
	*handoffTestStore
	runtime, stagedRuntime                      []any
	receipts, stagedReceipts                    map[string][]any
	pending                                     bool
	runtimeWrites, receiptWrites, capacityReads int
	failSkip                                    int
}

func (s *relaunchTestStore) InTx(tx f.Tx) (postgres.SQLExecutor, error) {
	if _, err := s.handoffTestStore.InTx(tx); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *relaunchTestStore) WithinTx(ctx context.Context, cause f.TransactionCause, fn func(context.Context, f.Tx) error) f.CommitResult {
	mode := ""
	if cause.Details().Primary.Command() == s.failName {
		if s.failSkip > 0 {
			s.failSkip--
		} else {
			mode, s.failName = s.failMode, ""
		}
	}
	if mode == "unknown-before" {
		return f.UnknownResult(s.attempt, cause)
	}
	if err := ctx.Err(); err != nil {
		return f.NotCommittedResult(f.NewFault(f.InternalError, f.NotCommitted).WithCause(err))
	}
	s.tx, s.locks, s.staged = f.NewTx(), nil, s.row
	s.stagedRuntime = append([]any(nil), s.runtime...)
	s.stagedReceipts = make(map[string][]any, len(s.receipts))
	for key, row := range s.receipts {
		s.stagedReceipts[key] = append([]any(nil), row...)
	}
	defer func() { s.tx, s.staged, s.locks = f.Tx{}, nil, nil; s.stagedRuntime, s.stagedReceipts = nil, nil }()
	if err := fn(ctx, s.tx); err != nil {
		var known *f.Fault
		if errors.As(err, &known) {
			return f.NotCommittedResult(known)
		}
		return f.NotCommittedResult(f.NewFault(f.InternalError, f.NotCommitted).WithCause(err))
	}
	if mode == "rollback-after" {
		return f.NotCommittedResult(fault(f.DependencyUnavailable))
	}
	s.row, s.runtime, s.receipts = s.staged, s.stagedRuntime, s.stagedReceipts
	if mode == "unknown-after" {
		return f.UnknownResult(s.attempt, cause)
	}
	return f.CommittedResult()
}

func (s *relaunchTestStore) QueryRow(ctx context.Context, query string, args ...any) postgres.Row {
	switch query {
	case relaunchRuntimeSQL:
		if len(args) != 2 || args[0] != s.row.project.String() || args[1] != s.row.task {
			return dispatchTestRow{err: errors.New("changed runtime scope")}
		}
		if s.stagedRuntime == nil {
			return dispatchTestRow{err: pgx.ErrNoRows}
		}
		return dispatchTestRow{values: s.stagedRuntime}
	case relaunchVisitSQL:
		if len(args) != 3 || args[0] != s.row.project.String() || args[1] != s.row.task {
			return dispatchTestRow{err: errors.New("changed original visit scope")}
		}
		row, ok := s.stagedReceipts[args[2].(string)]
		if !ok {
			return dispatchTestRow{err: pgx.ErrNoRows}
		}
		return dispatchTestRow{values: row}
	case `SELECT ` + dispatchColumns + ` FROM agenteam_scheduler.dispatches WHERE project_id=$1 AND id=$2`:
		if len(args) == 2 && args[0] == s.row.project.String() && args[1] == s.row.id.String() {
			return dispatchTestRow{values: recordValues(s.t, s.staged)}
		}
		return dispatchTestRow{err: pgx.ErrNoRows}
	}
	if strings.HasPrefix(query, "SELECT EXISTS") && strings.Contains(query, "agenteam_scheduler.dispatches") {
		return dispatchTestRow{values: []any{s.pending}}
	}
	return dispatchTestRow{err: errors.New("unexpected relaunch query")}
}

func (s *relaunchTestStore) Query(_ context.Context, query string, _ ...any) (*postgres.Rows, error) {
	if strings.Contains(query, "status IN ('pending','launched')") {
		s.capacityReads++
		return nil, relaunchAdmissionReached
	}
	return nil, errors.New("unexpected relaunch row query")
}

func (s *relaunchTestStore) Exec(ctx context.Context, query string, args ...any) (pgconn.CommandTag, error) {
	switch {
	case strings.HasPrefix(query, "UPDATE agenteam_scheduler.task_runtimes SET"):
		if len(args) != 10 || s.stagedRuntime == nil || args[9] != s.stagedRuntime[5] {
			return pgconn.CommandTag{}, errors.New("runtime CAS lost original version")
		}
		s.stagedRuntime = relaunchRuntimeValues(args[2:9])
		s.runtimeWrites++
	case strings.HasPrefix(query, "INSERT INTO agenteam_scheduler.task_runtimes("):
		if len(args) != 8 || s.stagedRuntime != nil {
			return pgconn.CommandTag{}, errors.New("unexpected runtime insertion")
		}
		values := []any{args[2], args[3], args[4], args[5], args[6], int64(1), args[7]}
		s.stagedRuntime = relaunchRuntimeValues(values)
		s.runtimeWrites++
	case strings.HasPrefix(query, "INSERT INTO agenteam_scheduler.relaunch_visits("):
		if len(args) != 8 || args[1] != s.row.project.String() || args[2] != s.row.task {
			return pgconn.CommandTag{}, errors.New("changed visit insert scope")
		}
		key := args[0].(string)
		if _, exists := s.stagedReceipts[key]; exists {
			return pgconn.CommandTag{}, errors.New("duplicate visit insertion")
		}
		raw := append([]byte(nil), args[3].([]byte)...)
		s.stagedReceipts[key] = []any{raw, args[4], args[5], args[6], args[7]}
		s.receiptWrites++
	default:
		return pgconn.CommandTag{}, errors.New("controlled relaunch must not write Task or Dispatch")
	}
	return pgconn.NewCommandTag("UPDATE 1"), ctx.Err()
}

func relaunchRuntimeValues(values []any) []any {
	out := append([]any(nil), values...)
	for _, n := range []int{2, 3} {
		if out[n] != nil {
			text := out[n].(string)
			out[n] = &text
		}
	}
	return out
}

type relaunchTestCurrent struct {
	store *handoffTestStore
	facts wc.SchedulerTaskFacts
}

func (*relaunchTestCurrent) SnapshotInTx(context.Context, f.Tx, i.ProjectID) (wc.SchedulerTaskSnapshot, error) {
	panic("single relaunch must not rediscover a traversal snapshot")
}
func (r *relaunchTestCurrent) CurrentTaskInTx(ctx context.Context, tx f.Tx, p i.ProjectID, task wc.TaskID) (wc.SchedulerTaskFacts, error) {
	key, _ := f.AggregateLock(f.TaskAggregate, task.String())
	locks := append(pendingLocks(p), f.LockRequest{Key: key, Mode: f.Shared})
	if err := r.store.RequireHeldLocks(ctx, tx, locks); err != nil {
		return wc.SchedulerTaskFacts{}, err
	}
	if r.facts.ProjectID != p || r.facts.TaskID != task {
		return wc.SchedulerTaskFacts{}, errors.New("changed current Task scope")
	}
	return r.facts.Clone(), ctx.Err()
}

type relaunchTestProject struct {
	store   *handoffTestStore
	project pc.SchedulerProject
}

func (p *relaunchTestProject) RequireSchedulerProjectInTx(ctx context.Context, tx f.Tx, id i.ProjectID) (pc.SchedulerProject, error) {
	if err := p.store.RequireHeldLocks(ctx, tx, pendingLocks(id)); err != nil {
		return pc.SchedulerProject{}, err
	}
	if p.project.Project.ID != id {
		return pc.SchedulerProject{}, errors.New("changed Project scope")
	}
	return p.project.Clone(), ctx.Err()
}

// Work is a controlled, read-only candidate source. Capacity acquisition is
// deliberately stopped before Record; even an accidental later Record returns
// an error rather than manufacturing a Work origin or an applied witness.
type relaunchTestWork struct {
	locks               []f.LockRequest
	discovered, records int
}

func (w *relaunchTestWork) DiscoverTaskRelaunch(ctx context.Context, _ i.Actor, request wc.TaskRelaunchRequest) (wc.TaskRelaunchPlan, error) {
	if err := request.Validate(); err != nil {
		return nil, err
	}
	w.discovered++
	return &claimTestPlan{locks: append([]f.LockRequest(nil), w.locks...)}, ctx.Err()
}

func (w *relaunchTestWork) RecordTaskRelaunchInTx(context.Context, f.Tx, i.Actor, wc.TaskRelaunchRequest, wc.TaskRelaunchPlan) (wc.AppliedTaskRelaunch, error) {
	w.records++
	return nil, relaunchAdmissionReached
}

func (*relaunchTestWork) CheckTaskRelaunchAppliedInTx(context.Context, f.Tx, i.Actor, wc.TaskRelaunchRequest, wc.TaskRelaunchPlan, wc.AppliedTaskRelaunch) error {
	panic("controlled Work rejection must not mint an applied relaunch witness")
}

type relaunchTestOccupancy struct {
	store  *handoffTestStore
	task   string
	active bool
	reads  int
}

func (r *relaunchTestOccupancy) ReadInTx(ctx context.Context, tx f.Tx, project i.ProjectID, tasks []string) (ec.ExecutionOccupancy, error) {
	if _, err := r.store.InTx(tx); err != nil {
		return ec.ExecutionOccupancy{}, err
	}
	if err := r.store.RequireHeldLocks(ctx, tx, pendingLocks(project)); err != nil {
		return ec.ExecutionOccupancy{}, err
	}
	if project != r.store.row.project || len(tasks) != 1 || tasks[0] != r.task {
		return ec.ExecutionOccupancy{}, errors.New("changed occupancy scope")
	}
	r.reads++
	out := ec.ExecutionOccupancy{Active: []ec.ActiveTaskExecution{}, HistoryTaskIDs: []string{r.task}}
	if r.active {
		out.Active = append(out.Active, ec.ActiveTaskExecution{TaskID: r.task, ExecutionID: *r.store.row.execution, AgentID: r.store.row.agent, Status: ec.Waiting})
	}
	return out, ctx.Err()
}

type relaunchTestObserver struct {
	store *handoffTestStore
	value ec.Summary
	slots int
}

func (o *relaunchTestObserver) LookupLaunchInTx(ctx context.Context, tx f.Tx, key ec.LaunchLookupKey, digest f.Digest, dispatch string) (ec.LaunchLookup, error) {
	r := o.store.staged
	if err := o.store.RequireHeldLocks(ctx, tx, executionIntentLocks(r)); err != nil {
		return ec.LaunchLookup{}, err
	}
	if key != lookupKey(r) || digest != r.digest || dispatch != r.id.String() {
		return ec.LaunchLookup{}, errors.New("latest observation changed the original association")
	}
	v := o.value.Clone()
	return ec.LaunchLookup{Found: true, RequestDigest: r.digest, Execution: &v}, ctx.Err()
}

func (o *relaunchTestObserver) AgentSlotInTx(ctx context.Context, tx f.Tx, project i.ProjectID, agent i.AgentID) (ec.AgentSlotObservation, error) {
	if _, err := o.store.InTx(tx); err != nil {
		return ec.AgentSlotObservation{}, err
	}
	if project != o.store.row.project || agent != o.store.row.agent {
		return ec.AgentSlotObservation{}, errors.New("changed slot scope")
	}
	o.slots++
	return ec.AgentSlotObservation{}, ctx.Err()
}

func relaunchRequest(t *testing.T, r *dispatchRecord, n int) wc.TaskRelaunchRequest {
	t.Helper()
	task, err := f.ParseID[wc.Task](r.task)
	if err != nil {
		t.Fatal(err)
	}
	sprint, err := f.ParseID[pc.Sprint](r.sprint)
	if err != nil {
		t.Fatal(err)
	}
	return wc.TaskRelaunchRequest{ProjectID: r.project, TaskID: task, AgentID: r.agent, CurrentSprintID: sprint, ExpectedTaskVersion: 2, DispatchID: dispatchTestID[DispatchIdentity](t, n).String(), RequestID: dispatchTestID[f.Request](t, n+1000), Purpose: "task/work"}
}

type relaunchTestFixture struct {
	owner       *RelaunchCoordinator
	coordinator *Coordinator
	store       *relaunchTestStore
	work        *relaunchTestWork
	current     *relaunchTestCurrent
	project     *relaunchTestProject
	occupancy   *relaunchTestOccupancy
	request     wc.TaskRelaunchRequest
}

func newRelaunchTest(t *testing.T, skip int64) *relaunchTestFixture {
	t.Helper()
	_, base, _ := newHandoffTest(t)
	r := base.row
	eid := dispatchTestID[i.Execution](t, 700)
	r.execution, r.status, r.outcome, r.attempts, r.version = &eid, Launched, Created, 1, 3
	request := relaunchRequest(t, r, 800)
	store := &relaunchTestStore{handoffTestStore: base, receipts: map[string][]any{}, runtime: []any{r.id.String(), eid.String(), nil, nil, int64(0), int64(1), r.updatedAt.Time()}}
	authority, err := NewPendingAuthority(store)
	if err != nil {
		t.Fatal(err)
	}
	project := &relaunchTestProject{store: base, project: pc.SchedulerProject{Project: pc.ProjectRef{ID: r.project, OwnerUserID: dispatchTestID[i.User](t, 105), Name: "relaunch-control", NormalizedName: "relaunch-control", Lifecycle: pc.Active, Version: 1, CreatedAt: r.createdAt, UpdatedAt: r.updatedAt, CurrentSprintID: &request.CurrentSprintID}, Config: pc.ProjectSchedulerConfig{Enabled: true}}}
	if err := project.project.Project.Validate(); err != nil {
		t.Fatal("invalid controlled Project", err)
	}
	observer := &relaunchTestObserver{store: base, value: ec.Summary{ID: eid, ProjectID: r.project, AgentID: r.agent, Trigger: r.launch.Trigger, Purpose: r.launch.Purpose, Status: ec.Succeeded, Version: 3, CreatedAt: r.createdAt, StartedAt: &r.createdAt, CompletedAt: &r.updatedAt}}
	coordinator, err := NewCoordinator(authority, CoordinatorDependencies{Projects: project, Claims: &claimTestWork{}, Executions: observer, Capacity: claimTestCapacity(func(context.Context, f.Tx, i.ProjectID, []ec.AssociatedDispatch) (int64, error) {
		return 0, errors.New("capacity rows were not supplied")
	})})
	if err != nil {
		t.Fatal(err)
	}
	taskKey, _ := f.AggregateLock(f.TaskAggregate, request.TaskID.String())
	work := &relaunchTestWork{locks: append(pendingLocks(r.project), f.LockRequest{Key: taskKey, Mode: f.Shared})}
	current := &relaunchTestCurrent{store: base, facts: wc.SchedulerTaskFacts{ProjectID: r.project, TaskID: request.TaskID, MilestoneID: dispatchTestID[wc.Milestone](t, 701), SprintID: request.CurrentSprintID, State: wc.TaskStateInProgress, Priority: wc.TaskPriorityHigh, Version: request.ExpectedTaskVersion, AssigneeAgentID: &r.agent}}
	occupancy := &relaunchTestOccupancy{store: base, task: r.task}
	owner, err := NewRelaunchCoordinator(coordinator, work, current, occupancy, skip)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { owner.Stop() })
	return &relaunchTestFixture{owner, coordinator, store, work, current, project, occupancy, request}
}

func TestSchedulerRelaunchCooldownPersistsVisits(t *testing.T) {
	v := newRelaunchTest(t, 2)
	first, err := v.owner.VisitRelaunch(context.Background(), v.request, emptyClaimPolicy())
	if err != nil || !first.CooldownSkipped || first.Remaining != 1 || first.Dispatch.data != nil || v.store.runtime[4] != int64(1) || v.store.runtimeWrites != 1 || v.store.receiptWrites != 1 || v.work.records != 0 {
		t.Fatal("first terminal observation must consume one visit without dispatch", err)
	}
	// Replaying the same completed visit is observation, not another traversal.
	replay, err := v.owner.VisitRelaunch(context.Background(), v.request, emptyClaimPolicy())
	if err != nil || !replay.CooldownSkipped || replay.Remaining != 1 || v.store.runtimeWrites != 1 || v.store.receiptWrites != 1 {
		t.Fatal("original visit replay consumed cooldown again", err)
	}
	v.owner.Stop()
	if !v.owner.Joined() {
		t.Fatal("known visit retained an active owner")
	}
	// A different owner gets the same committed Store, never an in-memory copy
	// of the old coordinator's counter or original retained call.
	next, err := NewRelaunchCoordinator(v.coordinator, v.work, v.current, v.occupancy, 2)
	if err != nil {
		t.Fatal(err)
	}
	defer next.Stop()
	secondRequest := relaunchRequest(t, v.store.row, 801)
	second, err := next.VisitRelaunch(context.Background(), secondRequest, emptyClaimPolicy())
	if err != nil || !second.CooldownSkipped || second.Remaining != 0 || second.Dispatch.data != nil || v.store.runtime[4] != int64(0) || v.store.receiptWrites != 2 || v.work.records != 0 {
		t.Fatal("visit which decrements to zero must still skip", err)
	}
	_, err = next.VisitRelaunch(context.Background(), relaunchRequest(t, v.store.row, 802), emptyClaimPolicy())
	if !errors.Is(err, relaunchAdmissionReached) || v.store.capacityReads != 1 || v.store.receiptWrites != 2 || v.store.runtime[4] != int64(0) {
		t.Fatal("visit beginning at zero did not proceed to current capacity", err)
	}
	zero := newRelaunchTest(t, 0)
	_, err = zero.owner.VisitRelaunch(context.Background(), zero.request, emptyClaimPolicy())
	if !errors.Is(err, relaunchAdmissionReached) || zero.store.capacityReads != 1 || zero.store.receiptWrites != 0 || zero.store.runtime[4] != int64(0) {
		t.Fatal("explicit zero invented a cooldown visit", err)
	}
	for _, blocked := range []string{"paused", "pending", "active", "stale-task"} {
		fixture := newRelaunchTest(t, 2)
		switch blocked {
		case "paused":
			fixture.project.project.Config.Enabled = false
		case "pending":
			fixture.store.pending = true
		case "active":
			fixture.occupancy.active = true
		case "stale-task":
			fixture.current.facts.Version++
		}
		out, err := fixture.owner.VisitRelaunch(context.Background(), fixture.request, emptyClaimPolicy())
		if err == nil || out.CooldownSkipped || out.Dispatch.data != nil || fixture.store.runtimeWrites != 0 || fixture.store.receiptWrites != 0 || fixture.store.capacityReads != 0 || fixture.work.records != 0 {
			t.Fatalf("%s consumed a visit or manufactured eligibility: %v", blocked, err)
		}
	}
	if _, err := NewRelaunchCoordinator(v.coordinator, v.work, v.current, v.occupancy, -1); err == nil {
		t.Fatal("negative skip count accepted")
	}
	// Legacy bootstrap uses Work's monotone claim version, not a timestamp or
	// UUID ordering guess. The newer claim intentionally has the smaller ID.
	newer := *v.store.row
	newer.guard = &ClaimGuard{TaskID: newer.task, ClaimedVersion: 9, SourceState: "todo", SourceAssigneeID: newer.agent, SourcePriority: "high", SourceSprintID: newer.sprint, SourceOrderGeneration: 2}
	older := dispatchTestRecord(t, 2, Launched)
	older.task, older.launch.Trigger.TaskID = newer.task, newer.task
	older.digest, err = older.launch.Digest()
	if err != nil {
		t.Fatal(err)
	}
	older.guard = &ClaimGuard{TaskID: newer.task, ClaimedVersion: 2, SourceState: "todo", SourceAssigneeID: older.agent, SourcePriority: "high", SourceSprintID: older.sprint, SourceOrderGeneration: 2}
	older.createdAt, err = f.NewInstant(newer.createdAt.Time().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	older.updatedAt = older.createdAt
	rows := &dispatchTestRows{rows: []dispatchTestRow{{values: recordValues(t, &newer)}, {values: recordValues(t, older)}}}
	latest, err := collectRelaunchHistory(context.Background(), rows, newer.project, v.request.TaskID)
	if err != nil || !rows.closed || latest == nil || latest.id != newer.id {
		t.Fatal("legacy bootstrap inferred launch order from clock or UUID", err)
	}
}

func TestSchedulerRelaunchUnknownKeepsOriginalVisit(t *testing.T) {
	for _, mode := range []string{"unknown-before", "unknown-after"} {
		v := newRelaunchTest(t, 2)
		// The first Tx is discovery. Only the final mutation's physical outcome
		// is faulted; no callback is replayed by the Store or by recovery.
		v.store.failName, v.store.failMode, v.store.failSkip = "relaunch", mode, 1
		out, original := v.owner.VisitRelaunch(context.Background(), v.request, emptyClaimPolicy())
		attempt, ok := UnknownAttempt(original)
		if original == nil || !ok || attempt.AttemptID() != v.store.attempt || out.CooldownSkipped || out.Dispatch.data != nil {
			t.Fatalf("%s lost the original physical mutation outcome: %v", mode, original)
		}
		_, repeated := v.owner.VisitRelaunch(context.Background(), v.request, emptyClaimPolicy())
		if repeated != original {
			t.Fatal("retained original visit was sent again", repeated)
		}
		_, fresh := v.owner.VisitRelaunch(context.Background(), relaunchRequest(t, v.store.row, 801), emptyClaimPolicy())
		if fresh == nil {
			t.Fatal("new visit bypassed an unresolved mutation for the same Task")
		}
		if mode == "unknown-before" {
			_, err := v.owner.ResolveRelaunch(context.Background(), v.request)
			if err != original || v.store.runtimeWrites != 0 || v.store.receiptWrites != 0 || v.store.runtime[4] != int64(0) || v.work.records != 0 {
				t.Fatal("absence of the original receipt authorized a repeated decrement", err)
			}
			v.owner.Stop()
			cancelled, cancel := context.WithCancel(context.Background())
			cancel()
			if v.owner.Joined() || !errors.Is(v.owner.Drain(cancelled), context.Canceled) {
				t.Fatal("stop fabricated a resolved mutation")
			}
			continue
		}
		if v.store.runtimeWrites != 1 || v.store.receiptWrites != 1 || v.store.runtime[4] != int64(1) {
			t.Fatal("controlled committed mutation did not persist one decrement")
		}
		// A valid but different immutable policy is not the original physical
		// receipt, even when request identity and remaining value still match.
		key := v.request.DispatchID
		good := append([]any(nil), v.store.receipts[key]...)
		changedPolicy := emptyClaimPolicy()
		changedPolicy.DeniedToolIDs = append(changedPolicy.DeniedToolIDs, dispatchTestID[i.Tool](t, 999))
		changedRaw, err := encodeRelaunchIntent(v.request, changedPolicy, LaunchRetryPolicy{})
		if err != nil {
			t.Fatal(err)
		}
		v.store.receipts[key][0], v.store.receipts[key][1] = changedRaw, string(relaunchDigest(changedRaw))
		if _, err = v.owner.ResolveRelaunch(context.Background(), v.request); err != original {
			t.Fatal("changed original receipt retired the owner", err)
		}
		v.store.receipts[key] = good
		v.project.project.Config.Enabled = false
		v.occupancy.active = true
		resolved, err := v.owner.ResolveRelaunch(context.Background(), v.request)
		if err != nil || !resolved.CooldownSkipped || resolved.Remaining != 1 || v.store.runtimeWrites != 1 || v.store.receiptWrites != 1 || v.work.records != 0 {
			t.Fatal("original read-only recovery changed the confirmed visit", err)
		}
		v.owner.Stop()
		if !v.owner.Joined() {
			t.Fatal("exact observed receipt failed to release its original owner")
		}
	}
}

func TestSchedulerRelaunchBusyKeepsTaskAndCooldown(t *testing.T) {
	owner, store, work := newBusyTest(t)
	r := store.row
	request := relaunchRequest(t, r, 1)
	request.RequestID = r.launch.Meta.RequestID
	r.guard = nil
	r.relaunch = &wc.TaskRelaunchSource{Request: request, MilestoneID: dispatchTestID[wc.Milestone](t, 701), ReferenceDigest: f.Digest("sha256:" + strings.Repeat("1", 64))}
	if err := r.relaunch.Validate(); err != nil {
		t.Fatal(err)
	}
	before := r.relaunch.Clone()
	out, err := owner.CompensateAgentBusy(context.Background(), r.project, r.id)
	if err != nil || out.Summary().Status != Skipped || out.Summary().SkipReason != "agent_busy" || work.discover != 0 || work.apply != 0 || work.checked != 0 || store.associationRuntime != nil {
		t.Fatal("relaunch Busy attempted Work claim restoration", err)
	}
	if store.row.relaunch == nil || *store.row.relaunch != before || store.row.guard != nil || store.row.attempts != 1 {
		t.Fatal("Busy changed the immutable relaunch source")
	}
	replay, err := owner.CompensateAgentBusy(context.Background(), r.project, r.id)
	if err != nil || replay.Summary().Version != out.Summary().Version || work.discover != 0 || work.apply != 0 || work.checked != 0 || store.associationRuntime != nil {
		t.Fatal("Busy replay repeated settlement", err)
	}
	// The shared Store supports association runtime writes. The explicit nil
	// checks above prove that neither Busy settlement nor its replay created or
	// reset cooldown; Task/rank writes remain outside this controlled Store.
	owner.Stop()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := owner.Drain(ctx); err != nil || !owner.Joined() {
		t.Fatal("known Busy settlement retained a call", err)
	}
}
