package work

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

type taskTriggerTestStore struct {
	denialStore
	tx                f.Tx
	missing           bool
	required          []f.LockRequest
	queries, acquires int
	physical          *f.CommitResult
	cancelPhysical    func()
}

func (s *taskTriggerTestStore) InTx(tx f.Tx) (postgres.SQLExecutor, error) {
	if tx != s.tx {
		return nil, fault(f.Forbidden)
	}
	return s, nil
}
func (s *taskTriggerTestStore) RequireHeldLocks(_ context.Context, tx f.Tx, locks []f.LockRequest) error {
	if tx != s.tx || s.missing || len(locks) != len(s.required) {
		return fault(f.Forbidden)
	}
	for n, lock := range locks {
		if lock.Key.Canonical() != s.required[n].Key.Canonical() || lock.Mode != s.required[n].Mode {
			return fault(f.Forbidden)
		}
	}
	return nil
}
func (s *taskTriggerTestStore) AcquireAll(_ context.Context, tx f.Tx, locks []f.LockRequest) error {
	s.acquires++
	if tx != s.tx {
		return fault(f.Forbidden)
	}
	s.required = append([]f.LockRequest{}, locks...)
	return nil
}
func (s *taskTriggerTestStore) WithinTx(ctx context.Context, _ f.TransactionCause, fn func(context.Context, f.Tx) error) f.CommitResult {
	if s.physical != nil {
		if s.cancelPhysical != nil {
			s.cancelPhysical()
		}
		return *s.physical
	}
	if err := fn(ctx, s.tx); err != nil {
		var problem *f.Fault
		if errors.As(err, &problem) {
			return f.NotCommittedResult(problem)
		}
		return f.NotCommittedResult(f.NewFault(f.DependencyUnavailable, f.NotCommitted).WithCause(err))
	}
	return f.CommittedResult()
}
func (s *taskTriggerTestStore) QueryRow(context.Context, string, ...any) postgres.Row {
	s.queries++
	return denialRow{}
}
func (s *taskTriggerTestStore) Query(context.Context, string, ...any) (*postgres.Rows, error) {
	s.queries++
	return nil, errors.New("private SQL canary")
}

type taskTriggerTestProject struct {
	pc.ProjectAuthority
	calls int
	check func(context.Context, i.Actor, i.ProjectID) (pc.ProjectAccess, error)
}

func (p *taskTriggerTestProject) RequireOwnerInTx(ctx context.Context, _ f.Tx, actor i.Actor, project i.ProjectID, _ i.AccessIntent) (pc.ProjectAccess, error) {
	p.calls++
	return p.check(ctx, actor, project)
}

type taskTriggerTestCapture struct {
	calls int
	check func(context.Context, f.Tx, i.ExecutionID, ec.LaunchRequest) (pc.ProjectRef, error)
}

func (p *taskTriggerTestCapture) RequireTriggerCaptureInTx(ctx context.Context, tx f.Tx, execution i.ExecutionID, request ec.LaunchRequest) (pc.ProjectRef, error) {
	p.calls++
	return p.check(ctx, tx, execution, request)
}
func taskTriggerTestProvider(t *testing.T, store *taskTriggerTestStore, project *taskTriggerTestProject, capture ec.TriggerCaptureAuthority) *TaskTrigger {
	t.Helper()
	authority, err := NewAuthority(store, project)
	if err != nil {
		t.Fatal(err)
	}
	provider, err := NewTaskTrigger(store, authority, capture)
	if err != nil {
		t.Fatal(err)
	}
	return provider
}
func taskTriggerTestProjectRef(t *testing.T) pc.ProjectRef {
	t.Helper()
	w := taskTriggerFixtureInput(t)
	return pc.ProjectRef{ID: w.Task.ProjectID, OwnerUserID: pureID[i.User](t, 1), Name: "task-source", NormalizedName: "task-source", Lifecycle: pc.Active, Version: 1, CreatedAt: w.Task.CreatedAt, UpdatedAt: w.Task.UpdatedAt}
}
func TestTaskTriggerCapturePlansAndAuthorityBoundary(t *testing.T) {
	execution, request := taskTriggerFixtureRequest(t)
	store := &taskTriggerTestStore{tx: f.NewTx()}
	project := &taskTriggerTestProject{check: func(context.Context, i.Actor, i.ProjectID) (pc.ProjectAccess, error) {
		t.Fatal("capture borrowed Human Owner")
		return pc.ProjectAccess{}, nil
	}}
	capture := &taskTriggerTestCapture{check: func(context.Context, f.Tx, i.ExecutionID, ec.LaunchRequest) (pc.ProjectRef, error) {
		return pc.ProjectRef{}, fault(f.Forbidden)
	}}
	provider := taskTriggerTestProvider(t, store, project, capture)
	plan, err := provider.DiscoverCapture(context.Background(), execution, request)
	if err != nil || len(plan.RequiredLocks()) != 5 || store.queries != 0 || capture.calls != 0 || store.acquires != 0 {
		t.Fatal("lock-only discovery", err)
	}
	locks := plan.RequiredLocks()
	locks[0].Mode = f.Exclusive
	if plan.RequiredLocks()[0].Mode != f.Shared {
		t.Fatal("lock alias")
	}
	store.required = plan.RequiredLocks()
	changed := request.Clone()
	changed.Meta.IdempotencyKey = "different-key"
	value, err := provider.CaptureInputInTx(context.Background(), store.tx, execution, changed, plan)
	pureCode(t, err, f.Forbidden)
	if value.Validate() == nil || capture.calls != 0 || store.queries != 0 {
		t.Fatal("changed request reached source")
	}
	second := taskTriggerTestProvider(t, store, project, capture)
	_, err = second.CaptureInputInTx(context.Background(), store.tx, execution, request, plan)
	pureCode(t, err, f.Forbidden)
	for _, mode := range []string{"missing-locks", "foreign-tx", "denied-proof", "cancelled-after-proof"} {
		plan, err = provider.DiscoverCapture(context.Background(), execution, request)
		if err != nil {
			t.Fatal(err)
		}
		store.required = plan.RequiredLocks()
		store.missing = mode == "missing-locks"
		tx := store.tx
		if mode == "foreign-tx" {
			tx = f.NewTx()
		}
		ctx, cancel := context.WithCancel(context.Background())
		capture.check = func(context.Context, f.Tx, i.ExecutionID, ec.LaunchRequest) (pc.ProjectRef, error) {
			if mode == "cancelled-after-proof" {
				cancel()
				return taskTriggerTestProjectRef(t), nil
			}
			return pc.ProjectRef{}, fault(f.Forbidden)
		}
		value, err = provider.CaptureInputInTx(ctx, tx, execution, request, plan)
		cancel()
		if err == nil || value.Validate() == nil || store.queries != 0 || store.acquires != 0 {
			t.Fatal("proof/locks must precede any source read", mode)
		}
		if mode == "cancelled-after-proof" && !errors.Is(err, context.Canceled) {
			t.Fatal("cancellation lost")
		}
	}
	noCapture := taskTriggerTestProvider(t, store, project, nil)
	_, err = noCapture.DiscoverCapture(context.Background(), execution, request)
	pureCode(t, err, f.DependencyUnbound)
	request.Policy.AllowedResourceConstraints = []json.RawMessage{json.RawMessage(`{"unknown":true}`)}
	_, err = provider.DiscoverCapture(context.Background(), execution, request)
	pureCode(t, err, f.DependencyUnbound)
	if store.queries != 0 {
		t.Fatal("unsupported policy read material")
	}
	// Wrapped dependency cancellation exposes only the sentinel, after the
	// synchronous callback actually returned; no raw dependency text escapes.
	_, request = taskTriggerFixtureRequest(t)
	store.missing = false
	for _, sentinel := range []error{context.Canceled, context.DeadlineExceeded} {
		plan, _ = provider.DiscoverCapture(context.Background(), execution, request)
		store.required = plan.RequiredLocks()
		capture.check = func(context.Context, f.Tx, i.ExecutionID, ec.LaunchRequest) (pc.ProjectRef, error) {
			return pc.ProjectRef{}, fmt.Errorf("private source canary: %w", sentinel)
		}
		_, err = provider.CaptureInputInTx(context.Background(), store.tx, execution, request, plan)
		if err != sentinel || strings.Contains(fmt.Sprint(err), "private") || store.queries != 0 {
			t.Fatal("unsafe cancelled dependency")
		}
	}
}
func TestTaskTriggerCurrentOwnerAndPhysicalOutcome(t *testing.T) {
	w := taskTriggerFixtureInput(t)
	store := &taskTriggerTestStore{tx: f.NewTx()}
	actor := pureActor(t, 2)
	project := &taskTriggerTestProject{check: func(context.Context, i.Actor, i.ProjectID) (pc.ProjectAccess, error) {
		return pc.ProjectAccess{}, fault(f.SessionRevoked)
	}}
	provider := taskTriggerTestProvider(t, store, project, nil)
	value, err := provider.ReadTaskInput(context.Background(), actor, w.Task.ProjectID, w.Task.ID, "task/work")
	pureCode(t, err, f.SessionRevoked)
	if value.Validate() == nil || store.queries != 0 || project.calls != 1 {
		t.Fatal("revoked Owner material")
	}
	project.check = func(context.Context, i.Actor, i.ProjectID) (pc.ProjectAccess, error) { return pc.ProjectAccess{}, nil }
	_, err = provider.ReadTaskInput(context.Background(), actor, w.Task.ProjectID, w.Task.ID, "task/work")
	pureCode(t, err, f.Forbidden)
	if store.queries != 0 {
		t.Fatal("zero grant material")
	}
	// Physical Unknown is not downgraded by cancellation after original return.
	cause, err := readCause("trigger-test")
	if err != nil {
		t.Fatal(err)
	}
	result := f.UnknownResult(pureID[f.TransactionAttempt](t, 94), cause)
	unknownCtx, unknownCancel := context.WithCancel(context.Background())
	store.cancelPhysical = unknownCancel
	store.physical = &result
	_, err = provider.ReadTaskInput(unknownCtx, actor, w.Task.ProjectID, w.Task.ID, "task/work")
	pureCode(t, err, f.CommitUnknown)
	var problem *f.Fault
	if !errors.As(err, &problem) || problem.CommitState != f.Unknown || problem.CauseID != result.AttemptID().String() || unknownCtx.Err() != context.Canceled {
		t.Fatal("Unknown outcome was lost or leaked")
	}
	store.physical = nil
	store.cancelPhysical = nil
	ctx, cancel := context.WithCancel(context.Background())
	project.check = func(context.Context, i.Actor, i.ProjectID) (pc.ProjectAccess, error) {
		ref := taskTriggerTestProjectRef(t)
		grant, err := pc.NewProjectAccess(actor, ref, ref.UpdatedAt)
		cancel()
		return grant, err
	}
	_, err = provider.ReadTaskInput(ctx, actor, w.Task.ProjectID, w.Task.ID, "task/work")
	if !errors.Is(err, context.Canceled) || store.queries != 0 {
		t.Fatal("late cancellation published input")
	}
}
