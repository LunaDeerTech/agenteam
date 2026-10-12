package execution

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"

	ac "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	"github.com/jackc/pgx/v5/pgconn"
)

// These controls borrow a controlled live Tx and a controlled owner boundary.
// They do not manufacture a real Agent prepared witness, validate SQL text, or
// prove PostgreSQL's all-state predicate. The original Agent/Project issuer and
// real Task/Execution facts belong to the integration case.
type retirementGateStore struct {
	Store
	tx                         f.Tx
	live, nilExecutor, present bool
	ctx                        context.Context
	held                       map[string]f.LockMode
	args                       []any
	steps                      []string
	queries, forbidden         int
	scanErr                    error
	entered, release           chan struct{}
}

func (s *retirementGateStore) InTx(tx f.Tx) (postgres.SQLExecutor, error) {
	s.steps = append(s.steps, "borrow")
	if !s.live || tx != s.tx {
		return nil, fault(f.Forbidden)
	}
	if s.nilExecutor {
		return nil, nil
	}
	return s, nil
}
func (s *retirementGateStore) RequireHeldLocks(ctx context.Context, tx f.Tx, locks []f.LockRequest) error {
	s.steps = append(s.steps, "locks")
	if ctx != s.ctx || tx != s.tx || !s.live || len(locks) != 3 {
		return fault(f.Forbidden)
	}
	seen := map[string]bool{}
	for _, lock := range locks {
		key := lock.Key.Canonical()
		if seen[key] || lock.Mode != f.Exclusive || s.held[key] != f.Exclusive {
			return fault(f.Forbidden)
		}
		seen[key] = true
	}
	return ctx.Err()
}
func (s *retirementGateStore) WithinTx(context.Context, f.TransactionCause, func(context.Context, f.Tx) error) f.CommitResult {
	s.forbidden++
	return f.NotCommittedResult(fault(f.Forbidden))
}
func (s *retirementGateStore) AcquireAll(context.Context, f.Tx, []f.LockRequest) error {
	s.forbidden++
	return fault(f.Forbidden)
}
func (s *retirementGateStore) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	s.forbidden++
	return pgconn.CommandTag{}, fault(f.Forbidden)
}
func (s *retirementGateStore) Query(context.Context, string, ...any) (*postgres.Rows, error) {
	s.forbidden++
	return nil, fault(f.Forbidden)
}
func (s *retirementGateStore) QueryRow(ctx context.Context, _ string, args ...any) postgres.Row {
	s.steps = append(s.steps, "query")
	s.queries++
	if ctx != s.ctx || !s.live || !reflect.DeepEqual(args, s.args) {
		return retirementGateRow{err: fault(f.Forbidden)}
	}
	return retirementGateRow{store: s}
}

type retirementGateRow struct {
	store *retirementGateStore
	err   error
}

func (r retirementGateRow) Scan(out ...any) error {
	if r.err != nil {
		return r.err
	}
	s := r.store
	s.steps = append(s.steps, "scan")
	if s.entered != nil {
		close(s.entered)
		<-s.release
	}
	if s.scanErr != nil {
		return s.scanErr
	}
	if len(out) != 1 {
		return fault(f.InternalError)
	}
	value, ok := out[0].(*bool)
	if !ok {
		return fault(f.InternalError)
	}
	*value = s.present
	return nil
}

type retirementGateOwner struct {
	store  *retirementGateStore
	change ac.ProjectRetirementChange
	err    error
	cancel context.CancelFunc
}

func (o *retirementGateOwner) RequireProjectRetirementPreparedInTx(ctx context.Context, tx f.Tx, change ac.ProjectRetirementChange) error {
	o.store.steps = append(o.store.steps, "prepared")
	a, b := change.Request, o.change.Request
	if ctx != o.store.ctx || tx != o.store.tx || !a.Actor.Equal(b.Actor) || a.ProjectID != b.ProjectID || a.AgentID != b.AgentID || a.Cause != b.Cause || a.ExpectedVersion != b.ExpectedVersion || !reflect.DeepEqual(change.Before.Fields(), o.change.Before.Fields()) {
		return fault(f.Forbidden)
	}
	if o.cancel != nil {
		o.cancel()
	}
	return o.err
}
func (o *retirementGateOwner) RequireProjectRetirementAppliedInTx(context.Context, f.Tx, ac.ProjectRetirementChange) error {
	o.store.forbidden++
	return fault(f.Forbidden)
}
func retirementGateID[K any](t *testing.T) f.ID[K] {
	t.Helper()
	value, err := f.NewID[K]()
	if err != nil {
		t.Fatal(err)
	}
	return value
}
func newRetirementGate(t *testing.T) (*ProjectAgentRetirement, *retirementGateStore, *retirementGateOwner, ac.ProjectRetirementChange) {
	t.Helper()
	project, agent, operation := retirementGateID[i.Project](t), retirementGateID[i.Agent](t), retirementGateID[pc.Operation](t)
	registration, err := i.RegisterService(i.ProjectLifecycle)
	if err != nil {
		t.Fatal(err)
	}
	scope, err := i.InProject(project)
	if err != nil {
		t.Fatal(err)
	}
	actor, err := registration.Actor(operation.String(), scope)
	if err != nil {
		t.Fatal(err)
	}
	at, err := f.ParseInstant("2026-10-12T12:00:00.000000Z")
	if err != nil {
		t.Fatal(err)
	}
	before, err := ac.NewAgentConfig(ac.AgentConfigFields{
		Core:           ac.AgentCore{ID: agent, ProjectID: project, Name: "retirement-agent", NormalizedName: "retirement-agent", ModelRef: retirementGateID[mc.Model](t), ApprovalPolicy: ac.ApprovalDefault, Lifecycle: ac.AgentActive, Version: 1, CreatedAt: at, UpdatedAt: at},
		AllowedToolIDs: []i.ToolID{}, AllowedMountIDs: []i.MountID{}, AllowedSecretVariableIDs: []i.ProjectVariableID{},
	})
	if err != nil {
		t.Fatal(err)
	}
	change := ac.ProjectRetirementChange{Request: ac.ProjectRetirementRequest{Actor: actor, ProjectID: project, AgentID: agent, Cause: pc.LifecycleCause{OperationID: operation, Action: pc.Delete, ProjectVersion: 2}, ExpectedVersion: 1}, Before: before}
	if err = change.Validate(); err != nil {
		t.Fatal(err)
	}
	// Independently declare the required full EX set; do not derive expected
	// permissions from the gate or the shared request's RequiredLocks method.
	p, _ := f.ProjectLock(project.String())
	schedule, _ := f.ProjectScheduleLock(project.String())
	a, _ := f.AgentLock(agent.String())
	store := &retirementGateStore{tx: f.NewTx(), live: true, ctx: context.Background(), held: map[string]f.LockMode{p.Canonical(): f.Exclusive, schedule.Canonical(): f.Exclusive, a.Canonical(): f.Exclusive}, args: []any{agent.String()}}
	owner := &retirementGateOwner{store: store, change: change}
	gate, err := NewProjectAgentRetirement(store, owner)
	if err != nil {
		t.Fatal(err)
	}
	if len(store.steps) != 0 || store.forbidden != 0 {
		t.Fatal("construction performed I/O")
	}
	return gate, store, owner, change
}
func retirementGateCode(t *testing.T, err error, code f.Code) {
	t.Helper()
	var known *f.Fault
	if !errors.As(err, &known) || known.Code != code {
		t.Fatal("wrong safe rejection", err, code)
	}
}
func TestExecutionProjectAgentRetirementRejectsUnboundAndInvalidInput(t *testing.T) {
	gate, store, owner, change := newRetirementGate(t)
	var nilStore *retirementGateStore
	var nilOwner *retirementGateOwner
	for _, pair := range []struct {
		store Store
		owner ac.ProjectRetirementOwner
	}{{nil, owner}, {nilStore, owner}, {store, nil}, {store, nilOwner}} {
		if _, err := NewProjectAgentRetirement(pair.store, pair.owner); err == nil {
			t.Fatal("unbound constructor accepted")
		}
	}
	var nilGate *ProjectAgentRetirement
	retirementGateCode(t, nilGate.RequireProjectAgentRetirementInTx(store.ctx, store.tx, change), f.DependencyUnbound)
	retirementGateCode(t, (&ProjectAgentRetirement{}).RequireProjectAgentRetirementInTx(store.ctx, store.tx, change), f.DependencyUnbound)
	for _, name := range []string{"nil-context", "invalid-tx", "empty-before", "version-mismatch", "archive", "cancelled"} {
		t.Run(name, func(t *testing.T) {
			input, ctx, tx := change.Clone(), store.ctx, store.tx
			want := f.InvalidArgument
			switch name {
			case "nil-context":
				ctx = nil
			case "invalid-tx":
				tx = f.Tx{}
			case "empty-before":
				input.Before = ac.AgentConfig{}
			case "version-mismatch":
				input.Request.ExpectedVersion++
			case "archive":
				input.Request.Cause.Action = pc.Archive
				want = f.Forbidden
			case "cancelled":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			err := gate.RequireProjectAgentRetirementInTx(ctx, tx, input)
			if name == "cancelled" {
				if !errors.Is(err, context.Canceled) {
					t.Fatal("lost caller cancellation", err)
				}
			} else {
				retirementGateCode(t, err, want)
			}
			if len(store.steps) != 0 || store.forbidden != 0 {
				t.Fatal("invalid input reached a borrowed operation")
			}
		})
	}
}
func TestExecutionProjectAgentRetirementRequiresOriginalTransactionAndLocks(t *testing.T) {
	for _, name := range []string{"foreign-tx", "closed-tx", "nil-executor", "project-sh", "schedule-sh", "agent-sh"} {
		t.Run(name, func(t *testing.T) {
			gate, store, _, change := newRetirementGate(t)
			tx := store.tx
			want := f.Forbidden
			switch name {
			case "foreign-tx":
				tx = f.NewTx()
			case "closed-tx":
				store.live = false
			case "nil-executor":
				store.nilExecutor = true
				want = f.DependencyUnbound
			default:
				var key f.LockKey
				switch name {
				case "project-sh":
					key, _ = f.ProjectLock(change.Request.ProjectID.String())
				case "schedule-sh":
					key, _ = f.ProjectScheduleLock(change.Request.ProjectID.String())
				case "agent-sh":
					key, _ = f.AgentLock(change.Request.AgentID.String())
				}
				store.held[key.Canonical()] = f.Shared
			}
			retirementGateCode(t, gate.RequireProjectAgentRetirementInTx(store.ctx, tx, change), want)
			for _, step := range store.steps {
				if step == "prepared" {
					t.Fatal("issuer called before transaction/lock rejection")
				}
			}
			if store.queries != 0 || store.forbidden != 0 {
				t.Fatal("gate rejection reached SQL or a new owner")
			}
		})
	}
}
func TestExecutionProjectAgentRetirementConsumesOwnerBeforeOwnFacts(t *testing.T) {
	for _, name := range []string{"owner-rejected", "owner-cancelled", "no-execution-history", "any-execution-history", "scan-failed"} {
		t.Run(name, func(t *testing.T) {
			gate, store, owner, change := newRetirementGate(t)
			sentinel := errors.New("controlled original scan failure")
			switch name {
			case "owner-rejected":
				owner.err = fault(f.Forbidden)
			case "owner-cancelled":
				var cancel context.CancelFunc
				store.ctx, cancel = context.WithCancel(store.ctx)
				defer cancel()
				owner.cancel = cancel
			case "any-execution-history":
				store.present = true
			case "scan-failed":
				store.scanErr = sentinel
			}
			before := change.Before.Fields()
			err := gate.RequireProjectAgentRetirementInTx(store.ctx, store.tx, change)
			wantSteps := []string{"borrow", "locks", "prepared", "query", "scan"}
			switch name {
			case "owner-rejected":
				retirementGateCode(t, err, f.Forbidden)
				wantSteps = wantSteps[:3]
			case "owner-cancelled":
				if !errors.Is(err, context.Canceled) {
					t.Fatal("lost issuer cancellation", err)
				}
				wantSteps = wantSteps[:3]
			case "no-execution-history":
				if err != nil {
					t.Fatal(err)
				}
			case "any-execution-history":
				retirementGateCode(t, err, f.ResourceBusy)
			case "scan-failed":
				retirementGateCode(t, err, f.DependencyUnavailable)
			}
			if !reflect.DeepEqual(store.steps, wantSteps) || store.forbidden != 0 || !reflect.DeepEqual(change.Before.Fields(), before) {
				t.Fatal("wrong original transaction/owner order or mutated caller input", store.steps)
			}
		})
	}
}
func TestExecutionProjectAgentRetirementWaitsForOriginalScan(t *testing.T) {
	gate, store, _, change := newRetirementGate(t)
	ctx, cancel := context.WithCancel(store.ctx)
	defer cancel()
	store.ctx, store.entered, store.release = ctx, make(chan struct{}), make(chan struct{})
	done := make(chan struct{})
	var err error
	var once sync.Once
	release := func() { once.Do(func() { close(store.release) }) }
	go func() { defer close(done); err = gate.RequireProjectAgentRetirementInTx(ctx, store.tx, change) }()
	t.Cleanup(func() {
		release()
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Error("original gate did not return after release")
		}
	})
	select {
	case <-store.entered:
	case <-time.After(time.Second):
		t.Fatal("original Scan barrier not reached")
	}
	cancel()
	select {
	case <-done:
		t.Fatal("caller cancellation substituted for original Scan return")
	default:
	}
	release()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("original Scan did not return")
	}
	if !errors.Is(err, context.Canceled) || store.forbidden != 0 || store.queries != 1 {
		t.Fatal("late original Scan returned successful permission", err)
	}
}
