package execution

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	ac "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// These controls use the real Launch/Lookup/repository/Authority, with explicit
// controlled SQL and upstream owners. They are not PostgreSQL constraints or
// proof of real Task, Meeting, Scheduler, Agent initialization or Snapshot.
type launchControl struct {
	t                                  *testing.T
	store                              *launchStore
	authority                          *Authority
	service                            *Service
	request                            c.LaunchRequest
	actor                              i.Actor
	config                             ac.AgentConfig
	revoked, stale, wrongAgent         bool
	discoveries, validations, captures int
}
type launchStore struct {
	Store
	t       *testing.T
	tx      f.Tx
	live    bool
	locks   []f.LockRequest
	rows    map[string][]any
	writes  int
	unknown bool
	readErr error
}
type launchScan struct {
	values []any
	err    error
}

func (r launchScan) Scan(out ...any) error {
	if r.err != nil {
		return r.err
	}
	if r.values == nil {
		return pgx.ErrNoRows
	}
	if len(out) != len(r.values) {
		panic("controlled SQL shape")
	}
	for n, value := range r.values {
		reflect.ValueOf(out[n]).Elem().Set(reflect.ValueOf(value))
	}
	return nil
}
func (s *launchStore) InTx(tx f.Tx) (postgres.SQLExecutor, error) {
	if !s.live || tx != s.tx {
		return nil, fault(f.Forbidden)
	}
	return s, nil
}
func (s *launchStore) AcquireAll(ctx context.Context, tx f.Tx, locks []f.LockRequest) error {
	if _, err := s.InTx(tx); err != nil {
		return err
	}
	if s.locks != nil {
		s.t.Fatal("late AcquireAll")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	s.locks = append([]f.LockRequest(nil), locks...)
	return nil
}
func (s *launchStore) RequireHeldLocks(ctx context.Context, tx f.Tx, locks []f.LockRequest) error {
	if _, err := s.InTx(tx); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	for _, wanted := range locks {
		found := false
		for _, held := range s.locks {
			if held.Key.Canonical() == wanted.Key.Canonical() && (held.Mode == f.Exclusive || wanted.Mode == f.Shared) {
				found = true
			}
		}
		if !found {
			return fault(f.Forbidden)
		}
	}
	return nil
}
func (s *launchStore) WithinTx(ctx context.Context, cause f.TransactionCause, fn func(context.Context, f.Tx) error) f.CommitResult {
	if s.live {
		s.t.Fatal("nested transaction")
	}
	before := make(map[string][]any, len(s.rows))
	for id, row := range s.rows {
		before[id] = append([]any(nil), row...)
	}
	priorWrites := s.writes
	s.tx, s.live, s.locks = f.NewTx(), true, nil
	defer func() { s.live = false; s.locks = nil }()
	if err := fn(ctx, s.tx); err != nil {
		s.rows, s.writes = before, priorWrites
		var known *f.Fault
		if !errors.As(err, &known) {
			known = f.NewFault(f.DependencyUnavailable, f.NotCommitted)
		}
		return f.NotCommittedResult(known)
	}
	if s.unknown && s.writes > priorWrites {
		s.unknown = false
		return f.UnknownResult(newTestID[f.TransactionAttempt](s.t), cause)
	}
	return f.CommittedResult()
}
func (s *launchStore) QueryRow(ctx context.Context, query string, args ...any) postgres.Row {
	if !s.live || ctx.Err() != nil {
		return launchScan{err: errors.New("no original live call")}
	}
	if s.readErr != nil {
		return launchScan{err: s.readErr}
	}
	if !strings.Contains(query, "agenteam_execution.executions") {
		s.t.Fatal("cross-domain query")
	}
	if strings.HasPrefix(query, "SELECT EXISTS") {
		busy := false
		for _, row := range s.rows {
			if row[2] == args[0] && !c.Status(row[3].(string)).Terminal() {
				busy = true
			}
		}
		return launchScan{values: []any{busy}}
	}
	if strings.HasSuffix(query, "WHERE id=$1") {
		return launchScan{values: s.rows[args[0].(string)]}
	}
	if !strings.HasSuffix(query, "WHERE project_id=$1 AND agent_id=$2 AND idempotency_key=$3") {
		s.t.Fatal("unexpected execution read")
	}
	for _, row := range s.rows {
		if row[1] == args[0] && row[2] == args[1] && row[11] == args[2] {
			return launchScan{values: row}
		}
	}
	return launchScan{}
}
func (s *launchStore) Exec(ctx context.Context, query string, args ...any) (pgconn.CommandTag, error) {
	if !s.live || ctx.Err() != nil || !strings.Contains(query, "INSERT INTO agenteam_execution.executions") || len(args) != 9 {
		s.t.Fatal("unexpected execution write")
	}
	at := time.Date(2026, 10, 10, 0, 0, 0, 0, time.UTC)
	row := []any{args[0], args[1], args[2], "created", int64(1), (*time.Time)(nil), (*string)(nil), at, (*time.Time)(nil), (*time.Time)(nil), args[3], args[4], args[5], args[6]}
	s.rows[args[0].(string)] = row
	s.writes++
	return pgconn.NewCommandTag("INSERT 0 1"), nil
}

type launchProjects struct {
	pc.ProjectAuthority
	fixture *launchControl
}

func (p launchProjects) RequireOwnerInTx(ctx context.Context, tx f.Tx, actor i.Actor, project i.ProjectID, intent i.AccessIntent) (pc.ProjectAccess, error) {
	x := p.fixture
	if x.revoked || !actor.Equal(x.actor) || project != x.request.ProjectID {
		return pc.ProjectAccess{}, fault(f.Forbidden)
	}
	if err := x.store.RequireHeldLocks(ctx, tx, scopeLocks(actor, project, x.request.AgentID, f.Shared)); err != nil {
		return pc.ProjectAccess{}, err
	}
	at, _ := f.NewInstant(time.Date(2026, 10, 10, 0, 0, 0, 0, time.UTC))
	user, _ := f.ParseID[i.User](actor.Details().UserID)
	return pc.NewProjectAccess(actor, pc.ProjectRef{ID: project, OwnerUserID: user, Name: "Execution-Test", NormalizedName: "execution-test", Lifecycle: pc.Active, Version: 1, CreatedAt: at, UpdatedAt: at}, at)
}

type controlledLaunchPlan struct {
	owner   *launchControl
	binding f.Digest
	locks   []f.LockRequest
}

func (p *controlledLaunchPlan) RequiredLocks() []f.LockRequest {
	return append([]f.LockRequest(nil), p.locks...)
}
func (x *launchControl) DiscoverLaunch(_ context.Context, actor i.Actor, request c.LaunchRequest) (c.LaunchPlan, error) {
	x.discoveries++
	if !actor.Equal(x.actor) {
		return nil, fault(f.Forbidden)
	}
	digest, _ := request.Digest()
	key, _ := f.AggregateLock(f.TaskAggregate, request.Trigger.TaskID)
	return &controlledLaunchPlan{x, digest, []f.LockRequest{{Key: key, Mode: f.Exclusive}}}, nil
}
func (x *launchControl) ValidateLaunchInTx(ctx context.Context, tx f.Tx, actor i.Actor, request c.LaunchRequest, plan c.LaunchPlan) (c.LaunchPermit, error) {
	x.validations++
	p, ok := plan.(*controlledLaunchPlan)
	digest, _ := request.Digest()
	if !ok || p.owner != x || p.binding != digest || !actor.Equal(x.actor) {
		return c.LaunchPermit{}, fault(f.Forbidden)
	}
	if err := x.store.RequireHeldLocks(ctx, tx, p.locks); err != nil {
		return c.LaunchPermit{}, err
	}
	if x.stale {
		return c.LaunchPermit{}, fault(f.ConfirmationStale)
	}
	return c.LaunchPermit{ProviderType: request.Trigger.Kind, ProjectID: request.ProjectID, AgentID: request.AgentID, RequestDigest: digest, ReferenceDigest: digest}, nil
}
func (x *launchControl) ReadExecutionConfigurationInTx(ctx context.Context, tx f.Tx, request ac.ExecutionConfigurationRequest) (ac.AgentConfig, error) {
	x.captures++
	if err := x.authority.RequireExecutionConfigurationInTx(ctx, tx, request); err != nil {
		return ac.AgentConfig{}, err
	}
	if x.wrongAgent {
		return ac.AgentConfig{}, fault(f.NotFound)
	}
	return x.config, nil
}
func newTestID[K any](t *testing.T) f.ID[K] {
	t.Helper()
	value, err := f.NewID[K]()
	if err != nil {
		t.Fatal(err)
	}
	return value
}
func newLaunchControl(t *testing.T) *launchControl {
	t.Helper()
	x := &launchControl{t: t}
	x.actor, _ = i.NewHuman(newTestID[i.User](t), newTestID[i.Session](t))
	x.request = c.LaunchRequest{ProjectID: newTestID[i.Project](t), AgentID: newTestID[i.Agent](t), Trigger: c.Trigger{Kind: "task", TaskID: newTestID[struct{}](t).String()}, Purpose: "task/work", Policy: c.Policy{SchemaVersion: 1, DeniedToolIDs: []i.ToolID{}, AllowedResourceConstraints: []json.RawMessage{}}, Meta: f.CommandMeta{RequestID: newTestID[f.Request](t), IdempotencyKey: "launch-original"}}
	at, _ := f.NewInstant(time.Date(2026, 10, 10, 0, 0, 0, 0, time.UTC))
	var err error
	x.config, err = ac.NewAgentConfig(ac.AgentConfigFields{Core: ac.AgentCore{ID: x.request.AgentID, ProjectID: x.request.ProjectID, Name: "Execution-Agent", NormalizedName: "execution-agent", ModelRef: newTestID[mc.Model](t), ApprovalPolicy: ac.ApprovalDefault, Lifecycle: ac.AgentActive, Version: 1, CreatedAt: at, UpdatedAt: at}, AllowedToolIDs: []i.ToolID{}, AllowedMountIDs: []i.MountID{}, AllowedSecretVariableIDs: []i.ProjectVariableID{}})
	if err != nil {
		t.Fatal(err)
	}
	x.store = &launchStore{t: t, rows: map[string][]any{}}
	x.authority, err = NewAuthority(x.store, launchProjects{fixture: x}, nil)
	if err != nil {
		t.Fatal(err)
	}
	x.service, err = New(x.store, Dependencies{Authority: x.authority, Agents: x, Task: x})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		x.service.Stop()
		if err := x.service.Drain(context.Background()); err != nil || !x.service.Joined() {
			t.Error("actual call ownership not joined")
		}
	})
	return x
}
func requireCode(t *testing.T, err error, code f.Code) {
	t.Helper()
	var fault *f.Fault
	if !errors.As(err, &fault) || fault.Code != code {
		t.Fatalf("expected %s, got %v", code, err)
	}
}

func TestExecutionLaunchOriginalIdentityReplayAndSlot(t *testing.T) {
	x := newLaunchControl(t)
	first, err := x.service.Launch(context.Background(), x.actor, x.request)
	if err != nil || first.Replayed || first.Execution.Status != c.Created || first.Execution.Version != 1 || x.store.writes != 1 || x.validations != 1 || x.captures != 1 {
		t.Fatal("original launch did not publish exactly one created identity", err)
	}
	x.stale = true // An old successful key is observed before new source checks.
	replay, err := x.service.Launch(context.Background(), x.actor, x.request)
	if err != nil || !replay.Replayed || replay.Execution.ID != first.Execution.ID || x.discoveries != 1 || x.store.writes != 1 {
		t.Fatal("replay attempted a new launch", err)
	}
	x.stale = false
	next := x.request.Clone()
	next.Meta.IdempotencyKey = "other-intent"
	_, err = x.service.Launch(context.Background(), x.actor, next)
	requireCode(t, err, f.AgentBusy)
	if x.store.writes != 1 {
		t.Fatal("busy created another identity")
	}
	changed := x.request.Clone()
	changed.Purpose = "task/review"
	_, err = x.service.Launch(context.Background(), x.actor, changed)
	requireCode(t, err, f.IdempotencyKeyReused)
	x.revoked = true
	_, err = x.service.Launch(context.Background(), x.actor, x.request)
	requireCode(t, err, f.Forbidden)
}
func TestExecutionLaunchRejectsMissingAndStaleOwners(t *testing.T) {
	for _, name := range []string{"trigger-unbound", "source-stale", "agent-rejected", "owner-revoked"} {
		t.Run(name, func(t *testing.T) {
			x := newLaunchControl(t)
			code := f.DependencyUnbound
			switch name {
			case "trigger-unbound":
				x.service.deps.Task = nil
			case "source-stale":
				x.stale = true
				code = f.ConfirmationStale
			case "agent-rejected":
				x.wrongAgent = true
				code = f.NotFound
			case "owner-revoked":
				x.revoked = true
				code = f.Forbidden
			}
			_, err := x.service.Launch(context.Background(), x.actor, x.request)
			requireCode(t, err, code)
			if x.store.writes != 0 || len(x.store.rows) != 0 {
				t.Fatal("rejection retained launch or slot")
			}
		})
	}
}
func TestExecutionUnknownLookupDoesNotRepeatLaunch(t *testing.T) {
	x := newLaunchControl(t)
	x.store.unknown = true
	value, err := x.service.Launch(context.Background(), x.actor, x.request)
	requireCode(t, err, f.CommitUnknown)
	if value.Execution.ID.Validate() == nil {
		t.Fatal("Unknown exposed committed success")
	}
	attempt, ok := UnknownAttempt(err)
	if !ok || attempt.AttemptID().Validate() != nil || x.validations != 1 || x.store.writes != 1 {
		t.Fatal("original unknown attempt lost or repeated")
	}
	digest, _ := x.request.Digest()
	key := c.LaunchLookupKey{ProjectID: x.request.ProjectID, AgentID: x.request.AgentID, IdempotencyKey: x.request.Meta.IdempotencyKey}
	lookup, err := x.service.LookupLaunch(context.Background(), x.actor, key, digest)
	if err != nil || !lookup.Found || lookup.Execution == nil || lookup.Execution.Status != c.Created || x.store.writes != 1 || x.validations != 1 {
		t.Fatal("Lookup wrote or missed actual observed identity", err)
	}
	x.store.readErr = errors.New("controlled private storage cause")
	_, err = x.service.LookupLaunch(context.Background(), x.actor, key, digest)
	requireCode(t, err, f.DependencyUnavailable)
	if strings.Contains(err.Error(), "private storage") {
		t.Fatal("private storage cause escaped")
	}
}
func TestExecutionLaunchWitnessCannotBeConstructedOrReused(t *testing.T) {
	x := newLaunchControl(t)
	r := ac.ExecutionConfigurationRequest{Actor: x.actor, ProjectID: x.request.ProjectID, AgentID: x.request.AgentID, ExecutionID: newTestID[i.Execution](t), Stage: ac.ExecutionConfigurationLaunch}
	x.store.tx, x.store.live, x.store.locks = f.NewTx(), true, r.RequiredLocks()
	err := x.authority.RequireExecutionConfigurationInTx(context.Background(), x.store.tx, r)
	requireCode(t, err, f.Forbidden)
	// Even an original Tx handle from another store does not identify this one.
	err = x.authority.RequireExecutionConfigurationInTx(context.Background(), f.NewTx(), r)
	requireCode(t, err, f.Forbidden)
	for _, stage := range []ac.ExecutionConfigurationStage{ac.ExecutionConfigurationCapture, ac.ExecutionConfigurationCurrent} {
		r.Stage = stage
		r.Actor, _ = i.NewAgentRun(r.ProjectID, r.AgentID, r.ExecutionID)
		x.store.locks = r.RequiredLocks()
		err = x.authority.RequireExecutionConfigurationInTx(context.Background(), x.store.tx, r)
		requireCode(t, err, f.DependencyUnbound)
	}
	x.store.live = false
}
