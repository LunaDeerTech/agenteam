package execution

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

// Original technical repository and admitted public reader run over controlled
// live transactions. Rows pagination and real Project/Work grants belong to PG.
type taskReadStore struct {
	*executionCallStore
	row        []any
	queryErr   error
	query      string
	args       []any
	runtimeRow []any
}

func (s *taskReadStore) WithinTx(ctx context.Context, cause f.TransactionCause, fn func(context.Context, f.Tx) error) f.CommitResult {
	tx := f.NewTx()
	s.mu.Lock()
	s.calls++
	s.live[tx] = nil
	s.mu.Unlock()
	err := fn(ctx, tx)
	s.mu.Lock()
	delete(s.live, tx)
	after := s.after
	s.mu.Unlock()
	result := f.CommittedResult()
	if err != nil {
		var known *f.Fault
		if !errors.As(err, &known) {
			known = f.NewFault(f.DependencyUnavailable, f.NotCommitted).WithCause(err)
		}
		result = f.NotCommittedResult(known)
	}
	if after != nil {
		return after(ctx, cause, result)
	}
	return result
}
func (s *taskReadStore) InTx(tx f.Tx) (postgres.SQLExecutor, error) {
	s.mu.Lock()
	_, ok := s.live[tx]
	s.mu.Unlock()
	if !ok {
		return nil, fault(f.Forbidden)
	}
	return s, nil
}
func (s *taskReadStore) QueryRow(ctx context.Context, q string, args ...any) postgres.Row {
	if strings.Contains(q, "agenteam_execution.project_calls") {
		return s.executionCallStore.QueryRow(ctx, q, args...)
	}
	if strings.HasPrefix(q, "SELECT current.*,v.content") {
		s.query, s.args = q, args
		if s.queryErr != nil {
			return launchScan{err: s.queryErr}
		}
		if len(args) != 3 || args[0] != s.row[0] || args[1] != s.row[1] {
			return launchScan{}
		}
		return launchScan{values: s.runtimeRow}
	}
	if !strings.HasPrefix(q, taskExecutionSelect) {
		return launchScan{err: errors.New("unexpected read")}
	}
	s.query = q
	s.args = args
	if s.queryErr != nil {
		return launchScan{err: s.queryErr}
	}
	if len(args) != 3 || args[0] != s.row[0] || args[1] != s.row[1] {
		return launchScan{}
	}
	return launchScan{values: append(append([]any{}, s.row...), (*string)(nil), (*string)(nil), (*int64)(nil), (*time.Time)(nil), (*string)(nil), (*string)(nil), (*time.Time)(nil))}
}
func (s *taskReadStore) Query(_ context.Context, q string, args ...any) (*postgres.Rows, error) {
	s.query = q
	s.args = args
	return nil, s.queryErr
}

type taskReadScope struct {
	store *taskReadStore
	task  string
	deny  bool
}

func (s *taskReadScope) RequireStore(v any) error {
	if v != s.store {
		return fault(f.DependencyUnbound)
	}
	return nil
}
func (s *taskReadScope) TaskReadLocks(actor i.Actor, p i.ProjectID, task string) ([]f.LockRequest, error) {
	user, _ := f.UserLock(actor.Details().UserID)
	key, _ := f.AggregateLock(f.TaskAggregate, task)
	return []f.LockRequest{{Key: user, Mode: f.Shared}, projectLock(p), {Key: key, Mode: f.Shared}}, nil
}
func (s *taskReadScope) RequireTaskReadInTx(ctx context.Context, tx f.Tx, actor i.Actor, p i.ProjectID, task string) error {
	if s.deny || task != s.task {
		return fault(f.NotFound)
	}
	locks, _ := s.TaskReadLocks(actor, p, task)
	return s.store.RequireHeldLocks(ctx, tx, locks)
}

type taskReadFixture struct {
	owner *ProjectReadOwnership
	store *taskReadStore
	scope *taskReadScope
	actor i.Actor
	p     i.ProjectID
	task  string
	id    i.ExecutionID
}

func newTaskReadFixture(t *testing.T) *taskReadFixture {
	t.Helper()
	launch := newLaunchControl(t)
	created, err := launch.service.Launch(context.Background(), launch.actor, launch.request)
	if err != nil {
		t.Fatal(err)
	}
	base := &executionCallStore{live: map[f.Tx][]f.LockRequest{}, rows: map[string]projectCallRecord{}}
	store := &taskReadStore{executionCallStore: base, row: launch.store.rows[created.Execution.ID.String()]}
	authority, err := NewAuthority(store, &executionCallProjects{store: base}, nil)
	if err != nil {
		t.Fatal(err)
	}
	keys, err := cursor.LoadKeyring(`{"format":1,"current_kid":"test","keys":[{"kid":"test","key_b64":"AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="}]}`)
	if err != nil {
		t.Fatal(err)
	}
	scope := &taskReadScope{store: store, task: launch.request.Trigger.TaskID}
	owner, err := newProjectReadOwnership(store, TaskReadDependencies{authority, scope, keys}, &executionCallProof{store: base}, newTestID[oc.Process](t), &executionCallAuthority{base})
	if err != nil {
		t.Fatal(err)
	}
	v := &taskReadFixture{owner, store, scope, launch.actor, launch.request.ProjectID, scope.task, created.Execution.ID}
	t.Cleanup(func() {
		store.setAfter(nil)
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := owner.Drain(ctx); err != nil || !owner.Joined() {
			t.Error("unreturned read owner", err)
		}
	})
	return v
}
func (v *taskReadFixture) get(ctx context.Context) (ec.TaskExecution, error) {
	return v.owner.TaskReader().Get(ctx, v.actor, v.p, v.task, v.id)
}
func TestTaskExecutionReaderOriginalReturns(t *testing.T) {
	for _, phase := range []string{"execution-call-register", "execution.task-read", "execution-call-return"} {
		t.Run(phase, func(t *testing.T) {
			v := newTaskReadFixture(t)
			entered, release := make(chan struct{}), make(chan struct{})
			v.store.setAfter(func(ctx context.Context, cause f.TransactionCause, result f.CommitResult) f.CommitResult {
				if cause.Details().Owner == phase {
					close(entered)
					<-release
				}
				return result
			})
			done := make(chan error, 1)
			go func() { _, err := v.get(context.Background()); done <- err }()
			select {
			case <-entered:
			case <-time.After(time.Second):
				t.Fatal("original Store not reached")
			}
			v.owner.Stop()
			if v.owner.Joined() {
				t.Fatal("held Store advertised joined")
			}
			deadline, cancel := context.WithCancel(context.Background())
			cancel()
			if v.owner.Drain(deadline) == nil {
				t.Fatal("held original Store drained")
			}
			close(release)
			select {
			case <-done:
			case <-time.After(time.Second):
				t.Fatal("actual method did not return")
			}
			v.store.setAfter(nil)
		})
	}
}
func TestTaskExecutionReaderScopeAndQuery(t *testing.T) {
	t.Run("get", func(t *testing.T) {
		v := newTaskReadFixture(t)
		out, err := v.get(context.Background())
		if err != nil || out.ID != v.id || out.TaskID != v.task || out.Status != ec.Created || out.Reason != nil {
			t.Fatal(out, err)
		}
		if !strings.Contains(v.store.query, "e.project_id=$2") || !strings.Contains(v.store.query, "task_id'=$3") {
			t.Fatal("unscoped query")
		}
		_, _, writes := v.store.counts()
		if writes != 2 {
			t.Fatal("expected technical register/close only", writes)
		}
	})
	t.Run("task-denied", func(t *testing.T) {
		v := newTaskReadFixture(t)
		v.scope.deny = true
		if _, err := v.get(context.Background()); err == nil {
			t.Fatal("missing Task accepted")
		}
		if v.store.query != "" {
			t.Fatal("business query before Task gate")
		}
	})
	t.Run("list-bound", func(t *testing.T) {
		v := newTaskReadFixture(t)
		sentinel := errors.New("Rows controlled rejection")
		v.store.queryErr = sentinel
		if _, err := v.owner.TaskReader().List(context.Background(), v.actor, v.p, v.task, f.PageRequest{Limit: 100}); err == nil {
			t.Fatal("lost query failure")
		}
		if len(v.store.args) != 3 || v.store.args[2] != 101 || !strings.HasSuffix(v.store.query, "ORDER BY e.created_at DESC,e.id DESC LIMIT $3") {
			t.Fatal("not bounded keyset read")
		}
	})
	t.Run("current-project", func(t *testing.T) {
		v := newTaskReadFixture(t)
		other := newTestID[i.Project](t)
		out, err := v.owner.TaskReader().Get(context.Background(), v.actor, other, v.task, v.id)
		if err == nil || out.ID.Validate() == nil {
			t.Fatal("foreign execution disclosed")
		}
	})
}
func TestTaskExecutionReaderUnknown(t *testing.T) {
	for _, phase := range []string{"execution-call-register", "execution.task-read", "execution-call-return"} {
		t.Run(phase, func(t *testing.T) {
			v := newTaskReadFixture(t)
			var original f.CommitResult
			masked := false
			v.store.setAfter(func(_ context.Context, cause f.TransactionCause, result f.CommitResult) f.CommitResult {
				if !masked && cause.Details().Owner == phase {
					masked = true
					original = f.UnknownResult(newTestID[f.TransactionAttempt](t), cause)
					return original
				}
				return result
			})
			out, err := v.get(context.Background())
			raw, ok := UnknownAttempt(err)
			if !ok || raw.AttemptID() != original.AttemptID() || !reflect.DeepEqual(raw.Cause().Details(), original.Cause().Details()) || out.ID.Validate() == nil {
				t.Fatal("original Unknown or empty output lost", err)
			}
			v.owner.state.mu.Lock()
			var entry *projectCallEntry
			for _, e := range v.owner.state.owners {
				entry = e
			}
			v.owner.state.mu.Unlock()
			if entry == nil || !entry.returned {
				t.Fatal("original uncertainty retired")
			}
			v.store.setAfter(nil)
			_, _, before := v.store.counts()
			_, _ = v.owner.state.settle(context.Background(), entry, nil, true)
			_, _, after := v.store.counts()
			if after != before {
				t.Fatal("Unknown confirmation wrote")
			}
			// Explicit next shutdown turn closes only the original read ledger.
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			if err := v.owner.Drain(ctx); err != nil || !v.owner.Joined() {
				t.Fatal(err)
			}
		})
	}
}
func TestTaskExecutionReaderCursorAndConstructor(t *testing.T) {
	v := newTaskReadFixture(t)
	reader := v.owner.TaskReader()
	binding, _ := taskExecutionBinding(v.p, v.task, v.actor.Details().UserID)
	at, _ := f.ParseInstant("2026-10-13T01:02:03.000001Z")
	a, _ := cursor.Instant(at)
	id, _ := cursor.UUID(v.id.String())
	token, err := reader.deps.Cursors.Sign(binding, cursor.Position{Scalars: []cursor.Scalar{a, id}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = reader.taskPosition(binding, token); err != nil {
		t.Fatal(err)
	}
	for _, b := range []cursor.Binding{func() cursor.Binding {
		x, _ := taskExecutionBinding(newTestID[i.Project](t), v.task, v.actor.Details().UserID)
		return x
	}(), func() cursor.Binding {
		x, _ := taskExecutionBinding(v.p, newTestID[struct{}](t).String(), v.actor.Details().UserID)
		return x
	}(), func() cursor.Binding {
		x, _ := taskExecutionBinding(v.p, v.task, newTestID[i.User](t).String())
		return x
	}()} {
		if _, err = reader.taskPosition(b, token); err == nil {
			t.Fatal("foreign cursor accepted")
		}
	}
	if _, err := NewProjectReadOwnership(v.store, reader.deps, nil, v.owner.state.process, v.owner.state.authority); err == nil {
		t.Fatal("unbound guard accepted")
	}
	foreign := reader.deps
	foreign.Tasks = &taskReadScope{store: &taskReadStore{}}
	if _, err := newTaskExecutionReader(v.owner.state, foreign); err == nil {
		t.Fatal("foreign Store bound")
	}
	if v.owner.state.service != nil || v.owner.state.associated != nil || v.owner.state.direct != nil || v.owner.state.preparation != nil || v.owner.state.retirementOrigins {
		t.Fatal("read-only graph owns drivers")
	}
}
func TestTaskExecutionReaderProjectStop(t *testing.T) {
	v := newTaskReadFixture(t)
	if _, err := v.get(context.Background()); err != nil {
		t.Fatal(err)
	}
	svc, _ := i.RegisterService(i.ProjectLifecycle)
	scope, _ := i.InProject(v.p)
	cause := pc.LifecycleCause{OperationID: newTestID[pc.Operation](t), Action: pc.Delete, ProjectVersion: 2}
	actor, _ := svc.Actor(cause.OperationID.String(), scope)
	report, err := v.owner.RequestProjectStop(context.Background(), actor, cause, pc.ScopeRef{Kind: pc.ProjectScope, ProjectID: v.p})
	requireExecutionStopState(t, report, err, pc.Stopped)
}

func TestTaskExecutionReaderSharesFullPair(t *testing.T) {
	seed, store := newExecutionEntryPair(t)
	keys, err := cursor.LoadKeyring(`{"format":1,"current_kid":"test","keys":[{"kid":"test","key_b64":"AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="}]}`)
	if err != nil {
		t.Fatal(err)
	}
	scope := &entryTaskScope{store: store}
	deps := ProjectCallDependencies{Service: seed.Service().deps, Preparation: PreparationDependencies{Agents: seed.Preparation().state.agents, Projects: seed.Preparation().state.projects, Processes: seed.Preparation().state.processes}, Direct: seed.Direct().state.deps, Associated: AssociatedExecutorOptions{MaxOwned: 4, RecoveryInterval: time.Hour}}
	deps.Read = &TaskReadDependencies{Authority: deps.Service.Authority, Tasks: scope, Cursors: keys}
	pair, err := newProjectCallOwnership(store, deps, executionEntryProof{}, seed.state.process, executionEntryStopAuthority{})
	if err != nil {
		t.Fatal(err)
	}
	if pair.TaskReader() == nil || pair.TaskReader().state != pair.state || pair.Service().projectCalls != pair.TaskReader().state || pair.Associated().state.projectCalls != pair.TaskReader().state {
		t.Fatal("second reader instance")
	}
	pair.Stop()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := pair.Drain(ctx); err != nil || !pair.Joined() {
		t.Fatal(err)
	}
}

type entryTaskScope struct{ store Store }

func (s *entryTaskScope) RequireStore(value any) error {
	if !sameProjectCallPort(value, s.store) {
		return fault(f.DependencyUnbound)
	}
	return nil
}
func (*entryTaskScope) TaskReadLocks(i.Actor, i.ProjectID, string) ([]f.LockRequest, error) {
	return nil, fault(f.Forbidden)
}
func (*entryTaskScope) RequireTaskReadInTx(context.Context, f.Tx, i.Actor, i.ProjectID, string) error {
	return fault(f.Forbidden)
}
