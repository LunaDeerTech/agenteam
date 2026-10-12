package registry

import (
	"context"
	"errors"
	"fmt"
	"reflect"
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

// Scripted rows and a controlled Applied owner test this adapter only. They do
// not issue a real Project claim, bind a PostgreSQL Tx or prove SQL predicates.
type retirementRefWitness struct{}
type retirementRefOwner struct {
	tx     f.Tx
	change ac.ProjectRetirementChange
	live   bool
	calls  int
}

func (*retirementRefOwner) RequireProjectRetirementPreparedInTx(context.Context, f.Tx, ac.ProjectRetirementChange) error {
	return f.NewFault(f.Forbidden, f.NotStarted)
}
func (o *retirementRefOwner) RequireProjectRetirementAppliedInTx(ctx context.Context, tx f.Tx, c ac.ProjectRetirementChange) error {
	o.calls++
	if !o.live || tx != o.tx || ctx.Value(retirementRefWitness{}) != o || !c.Request.Actor.Equal(o.change.Request.Actor) ||
		c.Request.ProjectID != o.change.Request.ProjectID || c.Request.AgentID != o.change.Request.AgentID ||
		c.Request.Cause != o.change.Request.Cause || c.Request.ExpectedVersion != o.change.Request.ExpectedVersion ||
		!reflect.DeepEqual(c.Before.Fields(), o.change.Before.Fields()) {
		return f.NewFault(f.Forbidden, f.NotStarted)
	}
	return nil
}

type retirementRefRow struct {
	values, args []any
	err          error
}
type retirementRefScan func(...any) error

func (fn retirementRefScan) Scan(out ...any) error { return fn(out...) }

type retirementRefStore struct {
	Store                  // No self-owned transaction or lock acquisition is part of this port.
	t                      *testing.T
	tx                     f.Tx
	owner                  *retirementRefOwner
	live                   bool
	held                   map[string]f.LockMode
	rows                   []retirementRefRow
	reads, queries, writes int
	writeArgs              []any
	writeCount             int64
	writeErr               error
	entered, release       chan struct{}
}

func (s *retirementRefStore) InTx(tx f.Tx) (postgres.SQLExecutor, error) {
	if !s.live || tx != s.tx {
		return nil, f.NewFault(f.Forbidden, f.NotStarted)
	}
	return s, nil
}
func (s *retirementRefStore) RequireHeldLocks(_ context.Context, tx f.Tx, locks []f.LockRequest) error {
	if !s.live || tx != s.tx {
		return f.NewFault(f.Forbidden, f.NotStarted)
	}
	for _, l := range locks {
		mode, ok := s.held[l.Key.Canonical()]
		if !ok || (mode != f.Exclusive && mode != l.Mode) {
			return f.NewFault(f.Forbidden, f.NotStarted)
		}
	}
	return nil
}
func (s *retirementRefStore) authorized(ctx context.Context) bool {
	return s.owner.calls > 0 && s.owner.live && ctx.Value(retirementRefWitness{}) == s.owner
}
func (s *retirementRefStore) QueryRow(ctx context.Context, _ string, args ...any) postgres.Row {
	n := s.reads
	s.reads++
	if !s.authorized(ctx) || n >= len(s.rows) || !reflect.DeepEqual(args, s.rows[n].args) {
		s.t.Error("unexpected SQL before Applied or outside original tuple")
		return retirementRefScan(func(...any) error { return f.NewFault(f.InternalError, f.NotStarted) })
	}
	row := s.rows[n]
	return retirementRefScan(func(out ...any) error {
		if n == 0 && s.entered != nil {
			close(s.entered)
			<-s.release
		}
		if row.err != nil {
			return row.err
		}
		if len(out) != len(row.values) {
			return fmt.Errorf("controlled row shape")
		}
		for k, v := range row.values {
			dst := reflect.ValueOf(out[k]).Elem()
			value := reflect.ValueOf(v)
			if !value.IsValid() || !value.Type().AssignableTo(dst.Type()) {
				return fmt.Errorf("controlled row type")
			}
			dst.Set(value)
		}
		return nil
	})
}
func (s *retirementRefStore) Query(ctx context.Context, _ string, args ...any) (*postgres.Rows, error) {
	s.queries++
	if !s.authorized(ctx) || !reflect.DeepEqual(args, []any{s.owner.change.Request.AgentID.String()}) {
		s.t.Error("reader escaped Applied tuple")
	}
	// postgres.Rows is deliberately not fabricated; its positive path is PG.
	return nil, f.NewFault(f.DependencyUnavailable, f.NotStarted)
}
func (s *retirementRefStore) Exec(ctx context.Context, _ string, args ...any) (pgconn.CommandTag, error) {
	s.writes++
	if !s.authorized(ctx) || !reflect.DeepEqual(args, s.writeArgs) {
		s.t.Error("write escaped exact original tuple")
		return pgconn.CommandTag{}, f.NewFault(f.InternalError, f.NotStarted)
	}
	if s.writeErr != nil {
		return pgconn.CommandTag{}, s.writeErr
	}
	return pgconn.NewCommandTag(fmt.Sprintf("DELETE %d", s.writeCount)), nil
}
func retirementRefID[K any](t *testing.T, n int) f.ID[K] {
	t.Helper()
	id, err := f.ParseID[K](fmt.Sprintf("01900000-0000-7000-8000-%012x", n))
	if err != nil {
		t.Fatal(err)
	}
	return id
}
func retirementRefFixture(t *testing.T) (*ProjectAgentRetirementReferences, *retirementRefStore, context.Context, ac.ProjectRetirementChange) {
	t.Helper()
	p, a := retirementRefID[i.Project](t, 1), retirementRefID[i.Agent](t, 2)
	at, err := f.NewInstant(time.Unix(100, 0))
	if err != nil {
		t.Fatal(err)
	}
	fields := ac.AgentConfigFields{Core: ac.AgentCore{ID: a, ProjectID: p, Name: "retirement-agent", NormalizedName: "retirement-agent", ModelRef: retirementRefID[mc.Model](t, 3), ApprovalPolicy: ac.ApprovalDefault, Lifecycle: ac.AgentActive, Version: 7, CreatedAt: at, UpdatedAt: at}, AllowedToolIDs: []i.ToolID{}, AllowedMountIDs: []i.MountID{}, AllowedSecretVariableIDs: []i.ProjectVariableID{}}
	fields.AllowedToolIDs = []i.ToolID{retirementRefID[i.Tool](t, 4)}

	before, err := ac.NewAgentConfig(fields)
	if err != nil {
		t.Fatal(err)
	}
	op := retirementRefID[pc.Operation](t, 10)
	registration, err := i.RegisterService(i.ProjectLifecycle)
	if err != nil {
		t.Fatal(err)
	}
	scope, err := i.InProject(p)
	if err != nil {
		t.Fatal(err)
	}
	actor, err := registration.Actor(op.String(), scope)
	if err != nil {
		t.Fatal(err)
	}
	c := ac.ProjectRetirementChange{Request: ac.ProjectRetirementRequest{Actor: actor, ProjectID: p, AgentID: a, Cause: pc.LifecycleCause{OperationID: op, Action: pc.Delete, ProjectVersion: 9}, ExpectedVersion: 7}, Before: before}
	if err = c.Validate(); err != nil {
		t.Fatal(err)
	}
	o := &retirementRefOwner{tx: f.NewTx(), change: c.Clone(), live: true}
	s := &retirementRefStore{t: t, tx: o.tx, owner: o, live: true, held: map[string]f.LockMode{}, writeCount: 1}
	pk, _ := f.ProjectLock(p.String())
	sk, _ := f.ProjectScheduleLock(p.String())
	ak, _ := f.AgentLock(a.String())
	for _, k := range []f.LockKey{pk, sk, ak} {
		s.held[k.Canonical()] = f.Exclusive
	}
	s.held[RegistryLock(f.Exclusive).Key.Canonical()] = f.Exclusive
	s.held[toolLock(fields.AllowedToolIDs[0], f.Shared).Key.Canonical()] = f.Shared
	adapter, err := NewProjectAgentRetirementReferences(s, o)
	if err != nil {
		t.Fatal(err)
	}
	locks, err := adapter.ProjectRetirementLocks(c)
	if err != nil || len(locks) != len(s.held) || adapter.ProjectRetirementReferenceKind() != ac.ProjectRetirementToolReferences || o.calls != 0 || s.reads != 0 {
		t.Fatal("constructor/lock collection must be pure and complete", err)
	}
	for _, l := range locks {
		if s.held[l.Key.Canonical()] != l.Mode {
			t.Fatal("incorrect lock identity or mode")
		}
	}
	return adapter, s, context.WithValue(context.Background(), retirementRefWitness{}, o), c
}
func retirementRefCode(err error, code f.Code) bool {
	var fault *f.Fault
	return errors.As(err, &fault) && fault.Code == code
}

func TestProjectAgentRetirementToolRequiresAppliedOwner(t *testing.T) {
	for _, mode := range []string{"foreign-tx", "expired-tx", "project-sh", "schedule-sh", "agent-sh", "expired-owner", "public-dto", "cancelled", "nil-context"} {
		t.Run(mode, func(t *testing.T) {
			a, s, ctx, c := retirementRefFixture(t)
			tx := s.tx
			switch mode {
			case "foreign-tx":
				tx = f.NewTx()
			case "expired-tx":
				s.live = false
			case "project-sh":
				k, _ := f.ProjectLock(c.Request.ProjectID.String())
				s.held[k.Canonical()] = f.Shared
			case "schedule-sh":
				k, _ := f.ProjectScheduleLock(c.Request.ProjectID.String())
				s.held[k.Canonical()] = f.Shared
			case "agent-sh":
				k, _ := f.AgentLock(c.Request.AgentID.String())
				s.held[k.Canonical()] = f.Shared
			case "expired-owner":
				s.owner.live = false
				ctx = context.WithoutCancel(ctx)
			case "public-dto":
				ctx = context.Background()
			case "cancelled":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			case "nil-context":
				ctx = nil
			}
			if a.RetireProjectAgentReferencesInTx(ctx, tx, c) == nil || a.RequireProjectAgentReferencesRetiredInTx(ctx, tx, c) == nil || s.reads+s.queries+s.writes != 0 {
				t.Fatal("invalid original owner reached SQL")
			}
		})
	}
	var owner *retirementRefOwner
	var store *retirementRefStore
	if _, err := NewProjectAgentRetirementReferences(store, &retirementRefOwner{}); err == nil {
		t.Fatal("typed nil Store")
	}
	if _, err := NewProjectAgentRetirementReferences(&retirementRefStore{}, owner); err == nil {
		t.Fatal("typed nil owner")
	}
}
func TestProjectAgentRetirementToolConfirmationAndOriginalReturn(t *testing.T) {
	t.Run("remaining", func(t *testing.T) {
		a, s, ctx, c := retirementRefFixture(t)
		s.rows = []retirementRefRow{{args: []any{c.Request.AgentID.String()}, values: []any{true}}}
		if err := a.RequireProjectAgentReferencesRetiredInTx(ctx, s.tx, c); !retirementRefCode(err, f.ResourceBusy) || s.writes != 0 {
			t.Fatal("present private facts confirmed absent", err)
		}
	})
	t.Run("empty", func(t *testing.T) {
		a, s, ctx, c := retirementRefFixture(t)
		s.rows = []retirementRefRow{{args: []any{c.Request.AgentID.String()}, values: []any{false}}}
		if err := a.RequireProjectAgentReferencesRetiredInTx(ctx, s.tx, c); err != nil || s.writes != 0 {
			t.Fatal("scoped empty confirmation", err)
		}
	})
	t.Run("cancelled-scan-actual-return", func(t *testing.T) {
		a, s, ctx, c := retirementRefFixture(t)
		s.rows = []retirementRefRow{{args: []any{c.Request.AgentID.String()}, values: []any{false}}}
		s.entered, s.release = make(chan struct{}), make(chan struct{})
		ctx, cancel := context.WithCancel(ctx)
		done := make(chan error, 1)
		joined, released := false, false
		t.Cleanup(func() {
			cancel()
			if !released {
				close(s.release)
			}
			if !joined {
				<-done
			}
		})
		go func() { done <- a.RequireProjectAgentReferencesRetiredInTx(ctx, s.tx, c) }()
		select {
		case <-s.entered:
		case <-time.After(time.Second):
			t.Fatal("original Scan not entered")
		}
		cancel()
		select {
		case <-done:
			joined = true
			t.Fatal("cancel replaced original return")
		default:
		}
		close(s.release)
		released = true
		select {
		case err := <-done:
			joined = true
			if !errors.Is(err, context.Canceled) || s.writes != 0 {
				t.Fatal("cancelled original read published completion", err)
			}
		case <-time.After(time.Second):
			t.Fatal("original Scan did not return")
		}
	})
}

func TestProjectAgentRetirementToolOriginalReader(t *testing.T) {
	for _, mode := range []string{"foreign-head", "original-reader-error", "original-scan-error", "registry-sh", "tool-lock-absent"} {
		t.Run(mode, func(t *testing.T) {
			a, s, ctx, c := retirementRefFixture(t)
			sentinel := f.NewFault(f.DependencyUnavailable, f.NotStarted)
			project := c.Request.ProjectID.String()
			if mode == "foreign-head" {
				project = retirementRefID[i.Project](t, 90).String()
			}
			s.rows = []retirementRefRow{{args: []any{c.Request.AgentID.String()}, values: []any{project, c.Request.ExpectedVersion}}}
			if mode == "original-scan-error" {
				s.rows[0].err = sentinel
			}
			if mode == "registry-sh" {
				s.held[RegistryLock(f.Exclusive).Key.Canonical()] = f.Shared
			}
			if mode == "tool-lock-absent" {
				delete(s.held, toolLock(c.Before.Fields().AllowedToolIDs[0], f.Shared).Key.Canonical())
			}
			err := a.RetireProjectAgentReferencesInTx(ctx, s.tx, c)
			if err == nil || s.writes != 0 {
				t.Fatal("incomplete original reader admitted deletion", err)
			}
			if mode == "original-scan-error" && !errors.Is(err, sentinel) {
				t.Fatal("original Scan error lost", err)
			}
			if mode == "original-reader-error" && (s.reads != 1 || s.queries != 1 || !retirementRefCode(err, f.DependencyUnavailable)) {
				t.Fatal("original Rows reader not owned", err)
			}
			if (mode == "registry-sh" || mode == "tool-lock-absent") && s.reads+s.queries != 0 {
				t.Fatal("missing union lock reached private reader")
			}
		})
	}
}
