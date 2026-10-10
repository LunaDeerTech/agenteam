package agent

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	c "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	"github.com/jackc/pgx/v5"
)

type executionConfigurationOwner struct {
	check func(context.Context, f.Tx, c.ExecutionConfigurationRequest) error
}

func (o *executionConfigurationOwner) RequireExecutionConfigurationInTx(ctx context.Context, tx f.Tx, r c.ExecutionConfigurationRequest) error {
	return o.check(ctx, tx, r)
}

type executionConfigurationProjects struct {
	pc.ProjectAuthority
	check func(context.Context, f.Tx, i.Actor, i.ProjectID, i.AccessIntent) (pc.ProjectAccess, error)
}

func (p *executionConfigurationProjects) RequireOwnerInTx(ctx context.Context, tx f.Tx, actor i.Actor, project i.ProjectID, intent i.AccessIntent) (pc.ProjectAccess, error) {
	return p.check(ctx, tx, actor, project, intent)
}

// The controlled executor only admits the original canonical point read and
// returns no row. These controls prove authorization/order/ownership boundaries;
// actual initialized SQL and Execution private witnesses belong to integration.
type executionConfigurationStore struct {
	Store
	t              *testing.T
	ctx            context.Context
	tx             f.Tx
	request        c.ExecutionConfigurationRequest
	locks          []f.LockRequest
	live           bool
	lockErr        error
	providerPassed bool
	reads          int
	onRead         func()
	readErr        error
}

func (s *executionConfigurationStore) InTx(tx f.Tx) (postgres.SQLExecutor, error) {
	if tx != s.tx || !s.live {
		return nil, fault(f.Forbidden)
	}
	return s, nil
}

func (s *executionConfigurationStore) RequireHeldLocks(ctx context.Context, tx f.Tx, locks []f.LockRequest) error {
	if ctx != s.ctx || tx != s.tx || len(locks) != len(s.locks) {
		s.t.Fatal("original context, Tx or complete lock subset changed")
	}
	for n := range locks {
		if locks[n].Key.Canonical() != s.locks[n].Key.Canonical() || locks[n].Mode != s.locks[n].Mode {
			s.t.Fatal("lock identity, mode or order changed")
		}
	}
	return s.lockErr
}

type executionConfigurationRow struct{ err error }

func (r executionConfigurationRow) Scan(...any) error {
	if r.err != nil {
		return r.err
	}
	return pgx.ErrNoRows
}

func (s *executionConfigurationStore) QueryRow(ctx context.Context, sql string, args ...any) postgres.Row {
	if !s.providerPassed || ctx != s.ctx || !strings.Contains(sql, "FROM agenteam_agent.agents WHERE project_id=$1 AND id=$2") || len(args) != 2 || args[0] != s.request.ProjectID.String() || args[1] != s.request.AgentID.String() {
		s.t.Fatal("canonical read preceded original authorization or changed target")
	}
	s.reads++
	if s.onRead != nil {
		s.onRead()
	}
	return executionConfigurationRow{s.readErr}
}

func executionConfigurationFixture(t *testing.T) (c.ExecutionConfigurationRequest, i.Actor, pc.ProjectAccess) {
	t.Helper()
	in, at := planFixture(t)
	r := c.ExecutionConfigurationRequest{Actor: in.actor, ProjectID: in.project, AgentID: in.target, ExecutionID: commandID[i.Execution](t, "01900000-0000-7000-8000-000000000009"), Stage: c.ExecutionConfigurationLaunch}
	run, err := i.NewAgentRun(r.ProjectID, r.AgentID, r.ExecutionID)
	if err != nil {
		t.Fatal(err)
	}
	grant, err := pc.NewProjectAccess(r.Actor, pc.ProjectRef{ID: r.ProjectID, OwnerUserID: commandID[i.User](t, r.Actor.Details().UserID), Name: "Execution-Project", NormalizedName: "execution-project", Lifecycle: pc.Active, Version: 1, CreatedAt: at, UpdatedAt: at}, at)
	if err != nil {
		t.Fatal(err)
	}
	return r, run, grant
}

func TestAgentExecutionConfigurationStagesAndLocks(t *testing.T) {
	r, run, _ := executionConfigurationFixture(t)
	registration, err := i.RegisterService(i.ProjectLifecycle)
	if err != nil {
		t.Fatal(err)
	}
	scope, err := i.InProject(r.ProjectID)
	if err != nil {
		t.Fatal(err)
	}
	service, err := registration.Actor(r.ExecutionID.String(), scope)
	if err != nil {
		t.Fatal(err)
	}
	user, _ := f.UserLock(r.Actor.Details().UserID)
	project, _ := f.ProjectLock(r.ProjectID.String())
	agent, _ := f.AgentLock(r.AgentID.String())
	execution, _ := f.AggregateLock(f.ExecutionAggregate, r.ExecutionID.String())
	want := []f.LockRequest{{Key: project, Mode: f.Shared}, {Key: agent, Mode: f.Shared}, {Key: execution, Mode: f.Exclusive}}
	for _, stage := range []c.ExecutionConfigurationStage{c.ExecutionConfigurationLaunch, c.ExecutionConfigurationCapture, c.ExecutionConfigurationCurrent} {
		for _, actor := range []i.Actor{r.Actor, service, run} {
			probe := r
			probe.Stage, probe.Actor = stage, actor
			allowed := stage == c.ExecutionConfigurationCapture || stage == c.ExecutionConfigurationLaunch && actor.Details().Kind != i.AgentRun || stage == c.ExecutionConfigurationCurrent && actor.Details().Kind == i.AgentRun
			if (probe.Validate() == nil) != allowed {
				t.Fatal("stage accepted the wrong Actor kind", stage, actor.Details().Kind)
			}
			if !allowed {
				if probe.RequiredLocks() != nil {
					t.Fatal("invalid request returned a lock plan")
				}
				continue
			}
			expected := append([]f.LockRequest(nil), want...)
			if actor.Details().Kind == i.Human {
				expected = append([]f.LockRequest{{Key: user, Mode: f.Shared}}, expected...)
			}
			locks := probe.RequiredLocks()
			if len(locks) != len(expected) {
				t.Fatal("lock count")
			}
			for n := range expected {
				if locks[n].Key.Canonical() != expected[n].Key.Canonical() || locks[n].Mode != expected[n].Mode {
					t.Fatal("missing current identity or aggregate lock")
				}
			}
			locks[0].Mode = f.Exclusive
			if probe.RequiredLocks()[0].Mode != f.Shared {
				t.Fatal("caller mutated future request locks")
			}
		}
	}
	r.Actor, r.Stage = run, c.ExecutionConfigurationCapture
	for _, field := range []string{"project", "agent", "execution", "stage"} {
		bad := r
		switch field {
		case "project":
			bad.ProjectID = commandID[i.Project](t, "01900000-0000-7000-8000-000000000099")
		case "agent":
			bad.AgentID = commandID[i.Agent](t, "01900000-0000-7000-8000-000000000099")
		case "execution":
			bad.ExecutionID = commandID[i.Execution](t, "01900000-0000-7000-8000-000000000099")
		default:
			bad.Stage = "running"
		}
		if bad.Validate() == nil {
			t.Fatal("cross-scope or open stage accepted", field)
		}
	}
	if fmt.Sprintf("%+v", r) != "execution_configuration_request" || r.LogValue().String() != "execution_configuration_request" {
		t.Fatal("request exposed its trusted identity in diagnostics")
	}
}

func TestAgentExecutionConfigurationOriginalAuthorityAndTransaction(t *testing.T) {
	r, run, grant := executionConfigurationFixture(t)
	ctx := context.WithValue(context.Background(), struct{}{}, "original-private-caller-context")
	s := &executionConfigurationStore{t: t, ctx: ctx, tx: f.NewTx(), request: r, locks: r.RequiredLocks(), live: true}
	ownerCalls, executionCalls := 0, 0
	projects := &executionConfigurationProjects{check: func(got context.Context, tx f.Tx, actor i.Actor, project i.ProjectID, intent i.AccessIntent) (pc.ProjectAccess, error) {
		ownerCalls++
		if got != ctx || tx != s.tx || !actor.Equal(r.Actor) || project != r.ProjectID || intent != i.Read {
			t.Fatal("current Human Owner check changed")
		}
		return grant, nil
	}}
	original := f.NewFault(f.DependencyUnavailable, f.NotCommitted)
	var providerErr error = original
	owner := &executionConfigurationOwner{check: func(got context.Context, tx f.Tx, gotRequest c.ExecutionConfigurationRequest) error {
		executionCalls++
		if got != ctx || tx != s.tx || !gotRequest.Actor.Equal(s.request.Actor) || gotRequest.ProjectID != s.request.ProjectID || gotRequest.AgentID != s.request.AgentID || gotRequest.ExecutionID != s.request.ExecutionID || gotRequest.Stage != s.request.Stage {
			t.Fatal("Execution callback lost the original context/Tx/stage/identity")
		}
		s.providerPassed = providerErr == nil
		return providerErr
	}}
	a, err := NewAuthority(s, projects)
	if err != nil {
		t.Fatal(err)
	}
	var typedNil *executionConfigurationOwner
	for _, o := range []c.ExecutionIdentityAuthority{nil, typedNil} {
		_, err = NewExecutionConfiguration(a, o)
		requireCode(t, err, f.DependencyUnbound)
	}
	adapter, err := NewExecutionConfiguration(a, owner)
	if err != nil || ownerCalls != 0 || executionCalls != 0 || s.reads != 0 {
		t.Fatal("constructor performed I/O", err)
	}
	_, err = adapter.ReadExecutionConfigurationInTx(ctx, f.NewTx(), r)
	requireCode(t, err, f.Forbidden)
	s.live = false
	_, err = adapter.ReadExecutionConfigurationInTx(ctx, s.tx, r)
	requireCode(t, err, f.Forbidden)
	s.live = true
	s.lockErr = fault(f.Forbidden)
	_, err = adapter.ReadExecutionConfigurationInTx(ctx, s.tx, r)
	requireCode(t, err, f.Forbidden)
	s.lockErr = nil
	if ownerCalls != 0 || executionCalls != 0 || s.reads != 0 {
		t.Fatal("foreign/ended Tx or missing locks reached authorization")
	}
	savedGrant := grant
	grant = pc.ProjectAccess{}
	_, err = adapter.ReadExecutionConfigurationInTx(ctx, s.tx, r)
	requireCode(t, err, f.Forbidden)
	if executionCalls != 0 || s.reads != 0 {
		t.Fatal("unmatched Owner grant authorized Execution")
	}
	grant = savedGrant
	_, err = adapter.ReadExecutionConfigurationInTx(ctx, s.tx, r)
	if err != original || executionCalls != 1 || s.reads != 0 {
		t.Fatal("original typed refusal/state replaced or read started", err)
	}
	providerErr = nil
	_, err = adapter.ReadExecutionConfigurationInTx(ctx, s.tx, r)
	requireCode(t, err, f.NotFound)
	if executionCalls != 2 || s.reads != 1 {
		t.Fatal("authorized Human did not reach original canonical read")
	}
	// Public AgentRun construction is still insufficient in both stages. A
	// successful controlled callback only permits reaching the no-row read;
	// it does not prove real preparing/running state or return a config.
	wantOwnerCalls := ownerCalls
	for _, stage := range []c.ExecutionConfigurationStage{c.ExecutionConfigurationCapture, c.ExecutionConfigurationCurrent} {
		s.request.Actor, s.request.Stage = run, stage
		s.locks = s.request.RequiredLocks()
		providerErr = fault(f.Forbidden)
		before := s.reads
		_, err = adapter.ReadExecutionConfigurationInTx(ctx, s.tx, s.request)
		requireCode(t, err, f.Forbidden)
		if s.reads != before {
			t.Fatal("public AgentRun bypassed actual owner callback")
		}
		providerErr = nil
		_, err = adapter.ReadExecutionConfigurationInTx(ctx, s.tx, s.request)
		requireCode(t, err, f.NotFound)
		if s.reads != before+1 || ownerCalls != wantOwnerCalls {
			t.Fatal("AgentRun capture/current substituted a Human Session")
		}
	}
	// Cancellation during the original repository read must survive its safe
	// storage-error projection, after the actual read has returned.
	canceled, cancel := context.WithCancel(ctx)
	defer cancel()
	ctx, s.ctx, s.onRead = canceled, canceled, cancel
	s.readErr = context.Canceled
	_, err = adapter.ReadExecutionConfigurationInTx(ctx, s.tx, s.request)
	if !errors.Is(err, context.Canceled) {
		t.Fatal("original SQL-time cancellation was hidden", err)
	}
}

func TestAgentExecutionConfigurationCancellationWaitsForOriginalCallback(t *testing.T) {
	r, run, _ := executionConfigurationFixture(t)
	r.Actor, r.Stage = run, c.ExecutionConfigurationCapture
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s := &executionConfigurationStore{t: t, ctx: ctx, tx: f.NewTx(), request: r, locks: r.RequiredLocks(), live: true}
	entered, release, returned := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	owner := &executionConfigurationOwner{check: func(got context.Context, tx f.Tx, request c.ExecutionConfigurationRequest) error {
		if got != ctx || tx != s.tx || !request.Actor.Equal(run) || request.Stage != c.ExecutionConfigurationCapture {
			return fault(f.Forbidden)
		}
		close(entered)
		<-release
		return nil
	}}
	a, err := NewAuthority(s, &executionConfigurationProjects{})
	if err != nil {
		t.Fatal(err)
	}
	adapter, err := NewExecutionConfiguration(a, owner)
	if err != nil {
		t.Fatal(err)
	}
	released, joined := false, false
	t.Cleanup(func() {
		cancel()
		if !released {
			close(release)
		}
		if !joined {
			select {
			case <-returned:
			case <-time.After(time.Second):
				t.Error("original controlled callback did not join")
			}
		}
	})
	go func() {
		_, err := adapter.ReadExecutionConfigurationInTx(ctx, s.tx, r)
		returned <- err
	}()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("original callback was not entered")
	}
	cancel()
	select {
	case <-returned:
		joined = true
		t.Fatal("cancellation returned before the original callback")
	default:
	}
	close(release)
	released = true
	select {
	case err = <-returned:
		joined = true
	case <-time.After(time.Second):
		t.Fatal("released original callback did not join")
	}
	if !errors.Is(err, context.Canceled) || s.reads != 0 {
		t.Fatal("late callback started canonical read or lost cancellation", err)
	}
	if _, err = adapter.ReadExecutionConfigurationInTx(ctx, s.tx, r); !errors.Is(err, context.Canceled) {
		t.Fatal("already cancelled context began another callback", err)
	}
}
