package scheduler

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"

	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	wc "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
	"github.com/jackc/pgx/v5"
)

type claimTestProject struct{ project pc.SchedulerProject }

func (p claimTestProject) RequireSchedulerProjectInTx(context.Context, f.Tx, i.ProjectID) (pc.SchedulerProject, error) {
	return p.project.Clone(), nil
}

type claimTestPlan struct{ locks []f.LockRequest }

func (p *claimTestPlan) RequiredLocks() []f.LockRequest {
	return append([]f.LockRequest(nil), p.locks...)
}

type claimTestWork struct {
	discover func(context.Context) (wc.TaskClaimPlan, error)
	called   int
}

func (w *claimTestWork) DiscoverTaskClaim(ctx context.Context, _ i.Actor, _ wc.TaskClaimRequest) (wc.TaskClaimPlan, error) {
	w.called++
	if w.discover != nil {
		return w.discover(ctx)
	}
	return &claimTestPlan{}, nil
}
func (*claimTestWork) ApplyTaskClaimInTx(context.Context, f.Tx, i.Actor, wc.TaskClaimRequest, wc.TaskClaimPlan) (wc.AppliedTaskClaim, error) {
	panic("controlled commit-outcome test must not claim successful Work SQL")
}
func (*claimTestWork) CheckTaskClaimAppliedInTx(context.Context, f.Tx, i.Actor, wc.TaskClaimRequest, wc.TaskClaimPlan, wc.AppliedTaskClaim) error {
	panic("controlled commit-outcome test must not mint applied proof")
}

type claimTestObserver struct {
	lookup func(ec.LaunchLookupKey, f.Digest, string) (ec.LaunchLookup, error)
}

func (o claimTestObserver) LookupLaunchInTx(_ context.Context, _ f.Tx, k ec.LaunchLookupKey, d f.Digest, id string) (ec.LaunchLookup, error) {
	if o.lookup == nil {
		return ec.LaunchLookup{}, fault(f.DependencyUnbound)
	}
	return o.lookup(k, d, id)
}
func (claimTestObserver) AgentSlotInTx(context.Context, f.Tx, i.ProjectID, i.AgentID) (ec.AgentSlotObservation, error) {
	return ec.AgentSlotObservation{}, nil
}

type claimTestStore struct {
	pendingTestStore
	within func(context.Context, f.TransactionCause, func(context.Context, f.Tx) error) f.CommitResult
	row    postgres.Row
}

func (s *claimTestStore) InTx(tx f.Tx) (postgres.SQLExecutor, error) {
	if tx != s.tx || !tx.Valid() {
		return nil, fault(f.InvalidArgument)
	}
	return s, nil
}
func (s *claimTestStore) WithinTx(ctx context.Context, c f.TransactionCause, fn func(context.Context, f.Tx) error) f.CommitResult {
	return s.within(ctx, c, fn)
}
func (s *claimTestStore) AcquireAll(context.Context, f.Tx, []f.LockRequest) error { return nil }
func (s *claimTestStore) RequireHeldLocks(context.Context, f.Tx, []f.LockRequest) error {
	return s.heldErr
}
func (s *claimTestStore) QueryRow(context.Context, string, ...any) postgres.Row {
	if s.row == nil {
		return dispatchTestRow{err: pgx.ErrNoRows}
	}
	return s.row
}
func testClaimSetup(t *testing.T) (*Coordinator, *claimTestStore, *claimTestWork, wc.TaskClaimRequest) {
	t.Helper()
	r := wc.TaskClaimRequest{ProjectID: dispatchTestID[i.Project](t, 100), TaskID: dispatchTestID[wc.Task](t, 201), AgentID: dispatchTestID[i.Agent](t, 101), DispatchID: dispatchTestID[DispatchIdentity](t, 1).String(), ExpectedTaskVersion: 1, CurrentSprintID: dispatchTestID[pc.Sprint](t, 102), Purpose: "task/work", RequestID: dispatchTestID[f.Request](t, 301)}
	store := &claimTestStore{pendingTestStore: pendingTestStore{tx: f.NewTx()}}
	a, _ := NewPendingAuthority(store)
	work := &claimTestWork{}
	s, err := NewCoordinator(a, CoordinatorDependencies{Projects: claimTestProject{pc.SchedulerProject{Project: pc.ProjectRef{ID: r.ProjectID, CurrentSprintID: &r.CurrentSprintID}, Config: pc.ProjectSchedulerConfig{Enabled: true}}}, Claims: work, Executions: claimTestObserver{}})
	if err != nil {
		t.Fatal(err)
	}
	return s, store, work, r
}
func emptyClaimPolicy() ec.Policy {
	return ec.Policy{SchemaVersion: 1, DeniedToolIDs: []i.ToolID{}, AllowedResourceConstraints: []json.RawMessage{}}
}

func TestSchedulerClaimPrivateIssuerAndOriginalTransaction(t *testing.T) {
	s, store, _, r := testClaimSetup(t)
	ctx, call, err := s.admit(context.Background(), r, emptyClaimPolicy())
	if err != nil {
		t.Fatal(err)
	}
	defer s.release(call)
	a := s.authority
	if err = a.RequireTaskClaimDiscoveryInTx(ctx, store.tx, call.actor, r); err != nil {
		t.Fatal("protected discovery", err)
	}
	if err = a.RequireTaskClaimDiscoveryInTx(context.Background(), store.tx, call.actor, r); err == nil {
		t.Fatal("actor DTO minted proof")
	}
	plan := &claimTestPlan{}
	locks, _ := claimLocks(r, call.launch)
	call.mu.Lock()
	call.stage = claimApplying
	call.tx = store.tx
	call.plan = plan
	call.locks = locks
	call.mu.Unlock()
	if err = a.RequireTaskClaimInTx(ctx, store.tx, call.actor, r, plan); err != nil {
		t.Fatal(err)
	}
	if _, err = a.RequireSchedulerCurrentIntentInTx(ctx, store.tx, call.actor, r.ProjectID, r.AgentID); err != nil {
		t.Fatal("Agent preclaim proof should not require a pending row", err)
	}
	for _, mode := range []string{"foreign-tx", "foreign-plan", "changed-request", "missing-held", "retired"} {
		t.Run(mode, func(t *testing.T) {
			tx := store.tx
			request := r
			var p wc.TaskClaimPlan = plan
			switch mode {
			case "foreign-tx":
				tx = f.NewTx()
			case "foreign-plan":
				p = &claimTestPlan{}
			case "changed-request":
				request.ExpectedTaskVersion++
			case "missing-held":
				store.heldErr = fault(f.Forbidden)
				defer func() { store.heldErr = nil }()
			case "retired":
				call.mu.Lock()
				call.live = false
				call.mu.Unlock()
			}
			if a.RequireTaskClaimInTx(ctx, tx, call.actor, request, p) == nil {
				t.Fatal("invalid original proof accepted")
			}
		})
	}
}

func TestSchedulerClaimGuardBindsAppliedVersionAndPosition(t *testing.T) {
	_, _, _, r := testClaimSetup(t)
	guard := wc.TaskClaimGuard{TaskID: r.TaskID, ClaimedVersion: 2, SourceState: wc.TaskStateTodo, SourceAssigneeID: r.AgentID, SourcePriority: wc.TaskPriorityHigh, SourceSprintID: r.CurrentSprintID, SourceOrderGeneration: 7}
	g, err := storedGuard(r, guard)
	if err != nil || g.SourceOrderGeneration != 7 {
		t.Fatal(err)
	}
	for _, mode := range []string{"version", "agent", "sprint", "state", "generation", "self-neighbor"} {
		t.Run(mode, func(t *testing.T) {
			bad := guard.Clone()
			switch mode {
			case "version":
				bad.ClaimedVersion++
			case "agent":
				bad.SourceAssigneeID = dispatchTestID[i.Agent](t, 999)
			case "sprint":
				bad.SourceSprintID = dispatchTestID[pc.Sprint](t, 999)
			case "state":
				bad.SourceState = wc.TaskStateBacklog
			case "generation":
				bad.SourceOrderGeneration = 0
			case "self-neighbor":
				bad.PredecessorID = &r.TaskID
			}
			if _, err := storedGuard(r, bad); err == nil {
				t.Fatal("changed claim guard accepted")
			}
		})
	}
}

func TestSchedulerClaimUnknownKeepsOriginalOwnership(t *testing.T) {
	s, store, work, r := testClaimSetup(t)
	var phase int
	attempt := dispatchTestID[f.TransactionAttempt](t, 777)
	dispatch, _ := f.ParseID[DispatchIdentity](r.DispatchID)
	cmd, _ := claimCommand(r.ProjectID, dispatch)
	cause, _ := f.NewCommandsCause(cmd)
	// The first two commit results are physical-boundary controls, not fake
	// successful SQL/Work. Only the recovery callback uses controlled row data.
	store.within = func(ctx context.Context, _ f.TransactionCause, fn func(context.Context, f.Tx) error) f.CommitResult {
		phase++
		if phase == 1 {
			return f.CommittedResult()
		}
		if phase == 2 {
			return f.UnknownResult(attempt, cause)
		}
		if err := fn(ctx, store.tx); err != nil {
			return f.NotCommittedResult(f.NewFault(f.DependencyUnavailable, f.NotCommitted).WithCause(err))
		}
		return f.CommittedResult()
	}
	out, original := s.ClaimTask(context.Background(), r, emptyClaimPolicy())
	unknown, ok := UnknownAttempt(original)
	if !ok || unknown.AttemptID() != attempt || out.data != nil || work.called != 1 {
		t.Fatal("unknown was lost or replayed")
	}
	s.Stop()
	if s.Joined() {
		t.Fatal("unknown call retired on method return")
	}
	if _, err := s.ResolveClaim(context.Background(), r); err != original || s.Joined() || work.called != 1 {
		t.Fatal("not observed became negative commit proof")
	}
	row := dispatchTestRecord(t, 1, Pending)
	row.outcome = NotSent
	row.attempts = 0
	row.guard = &ClaimGuard{TaskID: r.TaskID.String(), ClaimedVersion: 2, SourceState: "todo", SourceAssigneeID: r.AgentID, SourcePriority: "high", SourceSprintID: r.CurrentSprintID.String(), SourceOrderGeneration: 1}
	store.row = dispatchTestRow{values: recordValues(t, row)}
	out, err := s.ResolveClaim(context.Background(), r)
	if err != nil || out.Summary().ID != dispatch || !s.Joined() || work.called != 1 {
		t.Fatal("known original commit not joined", err)
	}
	if err = s.Drain(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestSchedulerClaimStopWaitsOriginalDiscoveryReturn(t *testing.T) {
	s, store, work, r := testClaimSetup(t)
	store.within = func(context.Context, f.TransactionCause, func(context.Context, f.Tx) error) f.CommitResult {
		return f.CommittedResult()
	}
	entered, released, cancelled := make(chan struct{}), make(chan struct{}), make(chan struct{})
	work.discover = func(ctx context.Context) (wc.TaskClaimPlan, error) {
		close(entered)
		<-ctx.Done()
		close(cancelled)
		<-released
		return nil, ctx.Err()
	}
	var wg sync.WaitGroup
	wg.Add(1)
	var err error
	go func() { defer wg.Done(); _, err = s.ClaimTask(context.Background(), r, emptyClaimPolicy()) }()
	<-entered
	s.Stop()
	<-cancelled
	if s.Joined() {
		t.Fatal("cancel was treated as returned")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if !errors.Is(s.Drain(ctx), context.Canceled) {
		t.Fatal("held call prematurely drained")
	}
	close(released)
	wg.Wait()
	if !errors.Is(err, context.Canceled) || !s.Joined() {
		t.Fatal("original return not joined", err)
	}
}

func TestSchedulerCapacityKeepsUnknownAndExcludesWaiting(t *testing.T) {
	s, store, _, _ := testClaimSetup(t)
	pending := dispatchTestRecord(t, 1, Pending)
	launched := dispatchTestRecord(t, 2, Launched)
	for _, status := range []ec.Status{ec.Created, ec.Preparing, ec.Running, ec.Waiting, ec.Succeeded} {
		t.Run(string(status), func(t *testing.T) {
			s.deps.Executions = claimTestObserver{lookup: func(_ ec.LaunchLookupKey, d f.Digest, _ string) (ec.LaunchLookup, error) {
				return ec.LaunchLookup{Found: true, RequestDigest: d, Execution: &ec.Summary{ID: *launched.execution, ProjectID: launched.project, AgentID: launched.agent, Status: status}}, nil
			}}
			used, err := s.capacityInTx(context.Background(), store.tx, []*dispatchRecord{pending, launched})
			want := int64(2)
			if status == ec.Waiting || status == ec.Succeeded {
				want = 1
			}
			if err != nil || used != want {
				t.Fatal("quota/slot semantics mixed", used, err)
			}
		})
	}
	s.deps.Executions = claimTestObserver{lookup: func(ec.LaunchLookupKey, f.Digest, string) (ec.LaunchLookup, error) { return ec.LaunchLookup{}, nil }}
	if n, err := s.capacityInTx(context.Background(), store.tx, []*dispatchRecord{pending, launched}); err == nil || n != 0 {
		t.Fatal("missing associated fact returned partial quota")
	}
}
