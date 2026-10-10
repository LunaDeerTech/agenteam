package mount

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	ac "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	rc "github.com/LunaDeerTech/agenteam/internal/central/runner/contract"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// This is a controlled Store/authority protocol fixture. It exercises the real
// provider and SQL boundary; it is not a PostgreSQL migration, MountCreate,
// live Session or real Agent canonical-writer integration proof.
type controlRow struct {
	values []any
	err    error
}

func (r controlRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	if len(dest) != len(r.values) {
		panic("unexpected SQL projection")
	}
	for n, dst := range dest {
		v := reflect.ValueOf(dst).Elem()
		if r.values[n] == nil {
			v.SetZero()
		} else {
			v.Set(reflect.ValueOf(r.values[n]))
		}
	}
	return nil
}

type controlledStore struct {
	tx                      f.Tx
	project                 i.ProjectID
	head                    referenceState
	definitions             map[i.MountID]definition
	locks                   []f.LockRequest
	acquires, reads, writes int
	applied                 bool
	missingLocks            bool
	physical                *f.CommitResult
	readError               error
}

func (s *controlledStore) WithinTx(ctx context.Context, _ f.TransactionCause, fn func(context.Context, f.Tx) error) f.CommitResult {
	if s.physical != nil {
		return *s.physical
	}
	if err := fn(ctx, s.tx); err != nil {
		var fault *f.Fault
		if errors.As(err, &fault) {
			return f.NotCommittedResult(fault)
		}
		return f.NotCommittedResult(f.NewFault(f.InternalError, f.NotCommitted).WithCause(err))
	}
	return f.CommittedResult()
}
func (s *controlledStore) InTx(tx f.Tx) (postgres.SQLExecutor, error) {
	if tx != s.tx {
		return nil, fault(f.DependencyUnavailable)
	}
	return s, nil
}
func (s *controlledStore) AcquireAll(_ context.Context, _ f.Tx, locks []f.LockRequest) error {
	s.acquires++
	s.locks = slices.Clone(locks)
	return nil
}
func (s *controlledStore) RequireHeldLocks(_ context.Context, _ f.Tx, locks []f.LockRequest) error {
	if s.missingLocks || !reflect.DeepEqual(s.locks, locks) {
		return fault(f.Forbidden)
	}
	return nil
}
func (*controlledStore) Query(context.Context, string, ...any) (*postgres.Rows, error) {
	panic("unexpected unbounded query")
}
func (s *controlledStore) QueryRow(_ context.Context, sql string, args ...any) postgres.Row {
	s.reads++
	if s.readError != nil {
		return controlRow{err: s.readError}
	}
	switch {
	case strings.HasPrefix(sql, "SELECT project_id,owner_version FROM agenteam_mount.agent_mount_heads"):
		if !s.head.exists {
			return controlRow{err: pgx.ErrNoRows}
		}
		return controlRow{values: []any{s.project.String(), s.head.version}}
	case strings.HasPrefix(sql, "SELECT COALESCE(jsonb_agg(mount_id"):
		ids := make([]string, len(s.head.ids))
		for n, id := range s.head.ids {
			ids[n] = id.String()
		}
		raw, _ := json.Marshal(ids)
		return controlRow{values: []any{raw}}
	case strings.HasPrefix(sql, "SELECT id,project_id,agent_id,runner_id,name,description,workspace,lifecycle,version FROM agenteam_mount.mounts"):
		id, _ := f.ParseID[i.Mount](args[0].(string))
		d, ok := s.definitions[id]
		if !ok {
			return controlRow{err: pgx.ErrNoRows}
		}
		return controlRow{values: []any{d.ID.String(), d.Project.String(), d.Agent.String(), d.Runner.String(), d.Name, d.Description, d.Workspace, d.Lifecycle, d.Version}}
	default:
		panic("SQL outside owned Mount reads")
	}
}
func (s *controlledStore) Exec(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	if !s.applied {
		panic("write without original owner callback")
	}
	s.writes++
	switch {
	case strings.HasPrefix(sql, "INSERT INTO agenteam_mount.agent_mount_heads"):
		if s.head.exists {
			panic("duplicate initialization")
		}
		s.head.exists, s.head.version = true, f.Version(args[2].(int64))
		return pgconn.NewCommandTag("INSERT 0 1"), nil
	case strings.HasPrefix(sql, "UPDATE agenteam_mount.agent_mount_heads"):
		if !s.head.exists || s.head.version != f.Version(args[3].(int64)) {
			return pgconn.NewCommandTag("UPDATE 0"), nil
		}
		s.head.version = f.Version(args[2].(int64))
		return pgconn.NewCommandTag("UPDATE 1"), nil
	case strings.HasPrefix(sql, "DELETE FROM agenteam_mount.agent_mount_refs"):
		n := len(s.head.ids)
		s.head.ids = nil
		return pgconn.NewCommandTag(fmt.Sprintf("DELETE %d", n)), nil
	case strings.HasPrefix(sql, "INSERT INTO agenteam_mount.agent_mount_refs"):
		id, _ := f.ParseID[i.Mount](args[2].(string))
		s.head.ids = append(s.head.ids, id)
		return pgconn.NewCommandTag("INSERT 0 1"), nil
	default:
		panic("SQL outside owned Mount writes")
	}
}

type controlledProject struct {
	pc.ProjectAuthority
	access pc.ProjectAccess
	reject bool
	calls  []i.AccessIntent
}

func (p *controlledProject) RequireOwnerInTx(_ context.Context, _ f.Tx, _ i.Actor, _ i.ProjectID, intent i.AccessIntent) (pc.ProjectAccess, error) {
	p.calls = append(p.calls, intent)
	if p.reject {
		return pc.ProjectAccess{}, nil
	}
	return p.access, nil
}

type controlledOwnerPlan struct{ locks []f.LockRequest }

func (p *controlledOwnerPlan) RequiredLocks() []f.LockRequest { return slices.Clone(p.locks) }

type witnessKey struct{}
type controlledOwner struct {
	store  *controlledStore
	plan   *controlledOwnerPlan
	change ac.MountConfigurationChange
	checks int
}

func (o *controlledOwner) DiscoverMountReferenceOwner(_ context.Context, r ac.MountConfigurationChange) (ac.MountReferenceOwnerPlan, error) {
	o.change = r.Clone()
	return o.plan, nil
}
func (o *controlledOwner) CheckMountReferenceOwnerAppliedInTx(ctx context.Context, tx f.Tx, r ac.MountConfigurationChange, p ac.MountReferenceOwnerPlan) error {
	o.checks++
	if ctx.Value(witnessKey{}) != o || tx != o.store.tx || p != o.plan || !sameChange(r, o.change) {
		return fault(f.Forbidden)
	}
	o.store.applied = true
	return nil
}
func testID[T any](t *testing.T, n int) f.ID[T] {
	t.Helper()
	v, err := f.ParseID[T](fmt.Sprintf("01900000-0000-7000-8000-%012d", n))
	if err != nil {
		t.Fatal(err)
	}
	return v
}
func fixture(t *testing.T) (*Configuration, *controlledStore, *controlledProject, *controlledOwner, ac.MountConfigurationChange) {
	t.Helper()
	u, sid := testID[i.User](t, 1), testID[i.Session](t, 2)
	actor, err := i.NewHuman(u, sid)
	if err != nil {
		t.Fatal(err)
	}
	p, a := testID[i.Project](t, 3), testID[i.Agent](t, 4)
	command, err := f.NewCommandIdentity("project", []string{p.String()}, "agent.create", f.IdempotencyKey("mount-create"))
	if err != nil {
		t.Fatal(err)
	}
	r := ac.MountConfigurationChange{Actor: actor, ProjectID: p, AgentID: a, Command: command, PlanRevision: 1, ResultOwnerVersion: 1, Before: []i.MountID{}, After: []i.MountID{}}
	if err = r.Validate(); err != nil {
		t.Fatal(err)
	}
	now, _ := f.NewInstant(time.Date(2026, 10, 10, 0, 0, 0, 0, time.UTC))
	access, err := pc.NewProjectAccess(actor, pc.ProjectRef{ID: p, OwnerUserID: u, Name: "mount-project", NormalizedName: "mount-project", Lifecycle: pc.Active, Version: 1, CreatedAt: now, UpdatedAt: now}, now)
	if err != nil {
		t.Fatal(err)
	}
	store := &controlledStore{tx: f.NewTx(), project: p, definitions: make(map[i.MountID]definition)}
	projects := &controlledProject{access: access}
	projectEX, _ := f.ProjectLock(p.String())
	owner := &controlledOwner{store: store, plan: &controlledOwnerPlan{locks: []f.LockRequest{{Key: projectEX, Mode: f.Exclusive}}}}
	c, err := NewConfiguration(store, projects, owner)
	if err != nil {
		t.Fatal(err)
	}
	return c, store, projects, owner, r
}
func requireCode(t *testing.T, err error, code f.Code) {
	t.Helper()
	var fault *f.Fault
	if !errors.As(err, &fault) || fault.Code != code {
		t.Fatalf("wanted %s, got %v", code, err)
	}
}

func TestMountEmptyConfigurationRequiresOwnerAndPersistsHead(t *testing.T) {
	c, s, p, o, r := fixture(t)
	ctx := context.Background()
	plan, err := c.DiscoverMountConfiguration(ctx, r)
	if err != nil {
		t.Fatal(err)
	}
	if s.writes != 0 || s.acquires != 1 || len(plan.RequiredLocks()) != 4 {
		t.Fatal("discovery wrote or lost full union")
	}
	if err = c.RequireMountConfigurationInTx(ctx, s.tx, r, plan); err != nil {
		t.Fatal(err)
	}
	requireCode(t, c.ApplyMountConfigurationInTx(ctx, s.tx, r, plan), f.Forbidden)
	if s.writes != 0 {
		t.Fatal("empty without witness wrote")
	}
	ctx = context.WithValue(ctx, witnessKey{}, o)
	if err = c.ApplyMountConfigurationInTx(ctx, s.tx, r, plan); err != nil {
		t.Fatal(err)
	}
	if !s.head.exists || s.head.version != 1 || len(s.head.ids) != 0 || s.writes != 2 || s.acquires != 1 || o.checks != 2 {
		t.Fatal("empty initialization bypassed real head/callback or acquired final locks")
	}
	if !slices.Contains(p.calls, i.Mutate) {
		t.Fatal("missing current mutation gate")
	}
	// A later Agent-only change must still advance this domain's empty head.
	r.Command, err = f.NewCommandIdentity("project", []string{r.ProjectID.String()}, "agent.update", f.IdempotencyKey("mount-update"))
	if err != nil {
		t.Fatal(err)
	}
	v := f.Version(1)
	r.ExpectedOwnerVersion = &v
	r.ResultOwnerVersion = 2
	s.applied = false
	plan, err = c.DiscoverMountConfiguration(ctx, r)
	if err != nil {
		t.Fatal(err)
	}
	if err = c.ApplyMountConfigurationInTx(ctx, s.tx, r, plan); err != nil {
		t.Fatal(err)
	}
	if s.head.version != 2 || s.writes != 4 {
		t.Fatal("unchanged set retained old owner version")
	}
}

func TestMountConfigurationRejectsForeignPlansAndCurrentRevocation(t *testing.T) {
	c, s, p, o, r := fixture(t)
	if _, err := NewConfiguration(s, p, nil); err == nil {
		t.Fatal("empty unbound producer accepted")
	}
	ctx := context.WithValue(context.Background(), witnessKey{}, o)
	plan, err := c.DiscoverMountConfiguration(ctx, r)
	if err != nil {
		t.Fatal(err)
	}
	other, _ := NewConfiguration(s, p, o)
	requireCode(t, other.ApplyMountConfigurationInTx(ctx, s.tx, r, plan), f.InvalidArgument)
	changed := r.Clone()
	changed.PlanRevision++
	requireCode(t, c.RequireMountConfigurationInTx(ctx, s.tx, changed, plan), f.InvalidArgument)
	requireCode(t, c.RequireMountConfigurationInTx(ctx, f.NewTx(), r, plan), f.DependencyUnavailable)
	s.missingLocks = true
	requireCode(t, c.ApplyMountConfigurationInTx(ctx, s.tx, r, plan), f.Forbidden)
	s.missingLocks = false
	p.reject = true
	requireCode(t, c.ApplyMountConfigurationInTx(ctx, s.tx, r, plan), f.Forbidden)
	p.reject = false
	s.head = referenceState{exists: true, version: 1}
	requireCode(t, c.ApplyMountConfigurationInTx(ctx, s.tx, r, plan), f.VersionConflict)
	if s.writes != 0 || o.checks != 0 || s.acquires != 1 {
		t.Fatal("rejection reached owner/write or acquired new locks")
	}
}

func TestMountExistingDefinitionCurrentSetAndSafeWorkspace(t *testing.T) {
	c, s, _, o, r := fixture(t)
	id := testID[i.Mount](t, 5)
	d := definition{ID: id, Project: r.ProjectID, Agent: r.AgentID, Runner: testID[rc.Runner](t, 6), Name: "source", Workspace: "source", Lifecycle: "active", Version: 1}
	s.definitions[id] = d
	r.After = []i.MountID{id}
	ctx := context.WithValue(context.Background(), witnessKey{}, o)
	plan, err := c.DiscoverMountConfiguration(ctx, r)
	if err != nil {
		t.Fatal(err)
	}
	changed := d
	changed.Workspace = "next"
	changed.Version++
	s.definitions[id] = changed
	requireCode(t, c.ApplyMountConfigurationInTx(ctx, s.tx, r, plan), f.VersionConflict)
	for _, state := range []string{"disabled", "removed"} {
		changed = d
		changed.Lifecycle = state
		s.definitions[id] = changed
		requireCode(t, c.RequireMountConfigurationInTx(ctx, s.tx, r, plan), f.NotFound)
	}
	changed = d
	changed.Project = testID[i.Project](t, 9)
	s.definitions[id] = changed
	requireCode(t, c.RequireMountConfigurationInTx(ctx, s.tx, r, plan), f.NotFound)
	s.definitions[id] = d
	// There is deliberately no Runner status port or online field in these
	// facts. This validates an existing definition; it does not create its Runner.
	if err = c.ApplyMountConfigurationInTx(ctx, s.tx, r, plan); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(s.head.ids, []i.MountID{id}) || s.writes != 3 {
		t.Fatal("valid logical reference not persisted")
	}
	for _, invalid := range []string{"", ".", "..", "../source", "foo/bar", `C:\source`, "nul", "COM1.txt", "repo.", "repo\x00", "répo"} {
		if safeWorkspace(invalid) {
			t.Fatal("unsafe workspace accepted")
		}
	}
	for _, valid := range []string{"source", "repo-1", "mac_debug", "repo.v2"} {
		if !safeWorkspace(valid) {
			t.Fatal("safe logical segment rejected")
		}
	}
}

func TestMountDiscoveryPreservesPhysicalOutcomeAndCancellation(t *testing.T) {
	c, s, _, _, r := fixture(t)
	cause, _ := f.NewCommandsCause(r.Command)
	original := f.UnknownResult(testID[f.TransactionAttempt](t, 10), cause)
	s.physical = &original
	_, err := c.DiscoverMountConfiguration(context.Background(), r)
	requireCode(t, err, f.CommitUnknown)
	retained, ok := UnknownAttempt(err)
	if !ok || retained.AttemptID() != original.AttemptID() || retained.Cause().Details().Primary.Canonical() != r.Command.Canonical() {
		t.Fatal("Unknown physical provenance lost")
	}
	s.physical = nil
	s.readError = errors.New("private SQL diagnostic")
	_, err = c.DiscoverMountConfiguration(context.Background(), r)
	requireCode(t, err, f.DependencyUnavailable)
	if strings.Contains(fmt.Sprintf("%+v", err), "private SQL diagnostic") || s.writes != 0 {
		t.Fatal("storage failure leaked or wrote")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = c.DiscoverMountConfiguration(ctx, r)
	if !errors.Is(err, context.Canceled) {
		t.Fatal("original cancellation lost")
	}
}
