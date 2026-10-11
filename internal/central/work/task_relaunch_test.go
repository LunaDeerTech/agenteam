package work

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	ac "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	c "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
	"github.com/jackc/pgx/v5/pgconn"
)

// These ports supply controlled current facts, not production Scheduler or
// Execution authority. The real cross-domain writer is exercised by the PG
// fixture; these controls isolate the Work provider's original caller-Tx gates.
type relaunchTestAgent struct {
	t     *testing.T
	tx    *f.Tx
	value ac.AgentRef
	err   error
	calls int
	after func()
}

func (p *relaunchTestAgent) RequireSchedulerCurrentInTx(_ context.Context, tx f.Tx, _ i.Actor, project i.ProjectID, agent i.AgentID) (ac.AgentRef, error) {
	p.calls++
	if tx != *p.tx || project != p.value.ProjectID || agent != p.value.AgentID {
		p.t.Fatal("Agent read escaped the original scope/transaction")
	}
	if p.after != nil {
		p.after()
	}
	return p.value, p.err
}

type relaunchTestExecutions struct {
	t     *testing.T
	tx    *f.Tx
	p     i.ProjectID
	task  string
	value ec.ExecutionOccupancy
	err   error
	calls int
}

func (p *relaunchTestExecutions) ReadInTx(_ context.Context, tx f.Tx, project i.ProjectID, tasks []string) (ec.ExecutionOccupancy, error) {
	p.calls++
	if tx != *p.tx || project != p.p || len(tasks) != 1 || tasks[0] != p.task {
		p.t.Fatal("Execution occupancy escaped the original Task/transaction")
	}
	return p.value, p.err
}

type relaunchTestPending struct {
	t     *testing.T
	tx    *f.Tx
	p     i.ProjectID
	task  string
	value ec.DispatchOccupancy
	err   error
	calls int
}

func (p *relaunchTestPending) ReadInTx(_ context.Context, tx f.Tx, project i.ProjectID, tasks []string) (ec.DispatchOccupancy, error) {
	p.calls++
	if tx != *p.tx || project != p.p || len(tasks) != 1 || tasks[0] != p.task {
		p.t.Fatal("pending read escaped the original Task/transaction")
	}
	return p.value, p.err
}

func relaunchControlRequest(t *testing.T) (c.TaskRelaunchRequest, i.Actor) {
	t.Helper()
	claim, _ := claimControlRecord(t)
	r := c.TaskRelaunchRequest{ProjectID: claim.After.ProjectID, TaskID: claim.After.ID, AgentID: *claim.After.AssigneeAgentID, CurrentSprintID: claim.After.SprintID, ExpectedTaskVersion: claim.After.Version, DispatchID: pureID[f.Request](t, 131).String(), RequestID: pureID[f.Request](t, 132), Purpose: "task/work"}
	registration, err := i.RegisterService(i.Scheduler)
	if err != nil {
		t.Fatal(err)
	}
	scope, err := i.InProject(r.ProjectID)
	if err != nil {
		t.Fatal(err)
	}
	actor, err := registration.Actor(r.DispatchID, scope)
	if err != nil || r.Validate() != nil {
		t.Fatal("invalid relaunch control", err)
	}
	return r, actor
}

func TestTaskRelaunchOriginIsDistinctFromTodoClaim(t *testing.T) {
	r, _ := relaunchControlRequest(t)
	claim, _ := claimControlRecord(t)
	legacy := c.TaskLaunchIntent{ProjectID: claim.Request.ProjectID, TaskID: claim.Request.TaskID, AgentID: claim.Request.AgentID, SprintID: claim.Request.CurrentSprintID, DispatchID: claim.Request.DispatchID, ClaimedVersion: claim.After.Version}
	if legacy.Validate() != nil {
		t.Fatal("legacy claim rejected")
	}
	raw, err := json.Marshal(legacy)
	if err != nil || strings.Contains(string(raw), "Origin") || strings.Contains(string(raw), "Relaunch") {
		t.Fatal("legacy persisted claim projection changed", err)
	}
	source := c.TaskRelaunchSource{Request: r, MilestoneID: claim.After.MilestoneID, ReferenceDigest: digest([]byte("controlled frozen Work source"))}
	intent := c.TaskLaunchIntent{ProjectID: r.ProjectID, TaskID: r.TaskID, AgentID: r.AgentID, SprintID: r.CurrentSprintID, DispatchID: r.DispatchID, Origin: c.TaskDispatchRelaunch, Relaunch: &source}
	if source.Validate() != nil || intent.Validate() != nil {
		t.Fatal("typed relaunch origin rejected")
	}
	copy := intent.Clone()
	copy.Relaunch.Request.DispatchID = legacy.DispatchID
	if copy.Validate() == nil || intent.Relaunch.Request.DispatchID != r.DispatchID {
		t.Fatal("origin alias or changed Dispatch accepted")
	}
	for _, mutate := range []func(*c.TaskLaunchIntent){
		func(v *c.TaskLaunchIntent) { v.Origin = "" },
		func(v *c.TaskLaunchIntent) { v.Origin = c.TaskDispatchTodoClaim },
		func(v *c.TaskLaunchIntent) { v.ClaimedVersion = claim.After.Version },
		func(v *c.TaskLaunchIntent) { v.Relaunch = nil },
		func(v *c.TaskLaunchIntent) { v.Relaunch.Request.Purpose = "task/review" },
		func(v *c.TaskLaunchIntent) { v.Relaunch.Request.AgentID = pureID[i.Agent](t, 133) },
	} {
		bad := intent.Clone()
		mutate(&bad)
		if bad.Validate() == nil {
			t.Fatal("mixed or unsupported origin accepted")
		}
	}
	legacy.Origin = c.TaskDispatchRelaunch
	if legacy.Validate() == nil {
		t.Fatal("claim shape became a relaunch grant")
	}
}

type relaunchTestProofKey struct{}
type relaunchTestAuthority struct {
	t       *testing.T
	tx      *f.Tx
	request c.TaskRelaunchRequest
	plan    c.TaskRelaunchPlan
	calls   int
	deny    bool
}

func (a *relaunchTestAuthority) check(ctx context.Context, tx f.Tx, actor i.Actor, r c.TaskRelaunchRequest) error {
	a.calls++
	if tx != *a.tx {
		a.t.Fatal("Scheduler proof received another transaction")
	}
	if a.deny || ctx.Value(relaunchTestProofKey{}) != a || r != a.request || actor.Details().CauseRef != r.DispatchID {
		return fault(f.Forbidden)
	}
	return nil
}
func (a *relaunchTestAuthority) RequireTaskRelaunchDiscoveryInTx(ctx context.Context, tx f.Tx, actor i.Actor, r c.TaskRelaunchRequest) error {
	return a.check(ctx, tx, actor, r)
}
func (a *relaunchTestAuthority) RequireTaskRelaunchInTx(ctx context.Context, tx f.Tx, actor i.Actor, r c.TaskRelaunchRequest, plan c.TaskRelaunchPlan) error {
	if err := a.check(ctx, tx, actor, r); err != nil {
		return err
	}
	if plan == nil || plan != a.plan {
		return fault(f.Forbidden)
	}
	return nil
}

type relaunchTestStore struct {
	*taskLaunchTestStore
	origin      *taskRelaunchRecord
	insertError error
	hideOrigin  bool
	afterRead   func()
	readResult  *f.CommitResult
	returned    bool
}

func (s *relaunchTestStore) InTx(tx f.Tx) (postgres.SQLExecutor, error) {
	if tx != s.tx {
		return nil, fault(f.Forbidden)
	}
	return s, nil
}
func (s *relaunchTestStore) WithinTx(ctx context.Context, cause f.TransactionCause, fn func(context.Context, f.Tx) error) f.CommitResult {
	result := s.taskTriggerTestStore.WithinTx(ctx, cause, fn)
	s.returned = true
	if s.afterRead != nil {
		s.afterRead()
	}
	if s.readResult != nil {
		return *s.readResult
	}
	return result
}
func (s *relaunchTestStore) Exec(_ context.Context, query string, args ...any) (pgconn.CommandTag, error) {
	s.writes++
	if !strings.HasPrefix(query, "INSERT INTO agenteam_work.task_scheduler_relaunches(") || len(args) != 11 {
		s.t.Fatal("relaunch wrote outside its immutable origin")
	}
	if s.insertError != nil {
		return pgconn.CommandTag{}, s.insertError
	}
	raw, ok := args[9].([]byte)
	if !ok {
		s.t.Fatal("origin is not canonical bytes")
	}
	var record taskRelaunchRecord
	if err := json.Unmarshal(raw, &record); err != nil {
		s.t.Fatal("invalid stored origin", err)
	}
	if args[0] != record.Request.DispatchID || args[1] != record.Request.ProjectID.String() || args[2] != record.Task.ID.String() || args[3] != record.Request.AgentID.String() || args[4] != record.Sprint.ID.String() || args[5] != record.Milestone.ID.String() || args[6] != record.Request.RequestID.String() || args[7] != int64(record.Task.Version) || args[8] != string(record.source().ReferenceDigest) {
		s.t.Fatal("origin SQL scalar projection differs from its record")
	}
	at, ok := args[10].(time.Time)
	if !ok || !at.Equal(record.CreatedAt.Time()) {
		s.t.Fatal("origin time differs")
	}
	s.origin = &record
	return pgconn.NewCommandTag("INSERT 0 1"), nil
}
func (s *relaunchTestStore) QueryRow(ctx context.Context, query string, args ...any) postgres.Row {
	if strings.Contains(query, "FROM agenteam_work.task_scheduler_relaunches") {
		s.queries++
		if len(args) != 2 || args[0] != s.task.ProjectID.String() {
			s.t.Fatal("origin read escaped Project")
		}
		if s.origin == nil || s.hideOrigin {
			return busyAbsentRow{}
		}
		r := s.origin
		if args[1] != r.Request.DispatchID {
			s.t.Fatal("origin read changed Dispatch")
		}
		raw, err := canonical(r)
		if err != nil {
			s.t.Fatal(err)
		}
		return taskTriggerRow{r.Task.ID.String(), r.Request.AgentID.String(), r.Sprint.ID.String(), r.Milestone.ID.String(), r.Request.RequestID.String(), int64(r.Task.Version), r.Request.Purpose, string(r.source().ReferenceDigest), raw, r.CreatedAt.Time()}
	}
	return s.taskLaunchTestStore.QueryRow(ctx, query, args...)
}

type relaunchTestFixture struct {
	service  *TaskRelaunchService
	store    *relaunchTestStore
	projects *taskLaunchTestProject
	intents  *relaunchTestAuthority
	agents   *relaunchTestAgent
	active   *relaunchTestExecutions
	pending  *relaunchTestPending
	ctx      context.Context
	actor    i.Actor
	request  c.TaskRelaunchRequest
}

func newRelaunchControl(t *testing.T) *relaunchTestFixture {
	t.Helper()
	_, old, projects, _, _, _, _ := newTaskLaunchControl(t)
	r, actor := relaunchControlRequest(t)
	store := &relaunchTestStore{taskLaunchTestStore: old}
	authority, err := NewAuthority(store, projects)
	if err != nil {
		t.Fatal(err)
	}
	intents := &relaunchTestAuthority{t: t, tx: &store.tx, request: r}
	agents := &relaunchTestAgent{t: t, tx: &store.tx, value: ac.AgentRef{ProjectID: r.ProjectID, AgentID: r.AgentID, ConfigVersion: 2}}
	active := &relaunchTestExecutions{t: t, tx: &store.tx, p: r.ProjectID, task: r.TaskID.String(), value: ec.ExecutionOccupancy{Active: []ec.ActiveTaskExecution{}, HistoryTaskIDs: []string{r.TaskID.String()}}}
	pending := &relaunchTestPending{t: t, tx: &store.tx, p: r.ProjectID, task: r.TaskID.String(), value: ec.DispatchOccupancy{Pending: []ec.PendingTaskDispatch{}, HistoryTaskIDs: []string{r.TaskID.String()}}}
	service, err := NewTaskRelaunch(store, TaskRelaunchDependencies{Authority: authority, Scheduler: intents, Agents: agents, Occupancy: active, Pending: pending})
	if err != nil {
		t.Fatal(err)
	}
	return &relaunchTestFixture{service, store, projects, intents, agents, active, pending, context.WithValue(context.Background(), relaunchTestProofKey{}, intents), actor, r}
}

func (v *relaunchTestFixture) discover(t *testing.T) c.TaskRelaunchPlan {
	t.Helper()
	plan, err := v.service.DiscoverTaskRelaunch(v.ctx, v.actor, v.request)
	if err != nil || plan == nil {
		t.Fatal("discover controlled origin", err)
	}
	v.intents.plan = plan
	return plan
}

func TestTaskRelaunchRecordsOnlyOriginAndRechecksCurrentFacts(t *testing.T) {
	v := newRelaunchControl(t)
	before := v.store.task.Clone()
	plan := v.discover(t)
	if v.store.writes != 0 || v.store.queries != 4 || v.intents.calls != 1 {
		t.Fatal("discovery did not read only protected current Work facts")
	}
	locks := plan.RequiredLocks()
	locks[0].Mode = f.Shared
	if sameTaskLocks(locks, plan.RequiredLocks()) {
		t.Fatal("caller changed the original lock plan")
	}
	v.store.tx = f.NewTx()
	v.store.required = plan.RequiredLocks()
	applied, err := v.service.RecordTaskRelaunchInTx(v.ctx, v.store.tx, v.actor, v.request, plan)
	if err != nil || applied == nil || applied.Source().Validate() != nil || applied.Source().Request != v.request || applied.Source().MilestoneID != before.MilestoneID || v.store.writes != 1 || !sameValue(before, v.store.task) {
		t.Fatal("origin write changed Task or omitted source", err)
	}
	if err = v.service.CheckTaskRelaunchAppliedInTx(v.ctx, v.store.tx, v.actor, v.request, plan, applied); err != nil || v.store.writes != 1 || v.agents.calls != 2 || v.active.calls != 2 || v.pending.calls != 2 || v.intents.calls != 3 {
		t.Fatal("same-Tx postimage/current authority not checked", err)
	}
	if v.store.acquires != 1 {
		t.Fatal("final participant acquired locks or began its own transaction")
	}
	// Launch consumes the recorded relaunch arm through the unchanged public
	// TriggerProvider API. The old todo claim has a different Dispatch ID.
	source := applied.Source()
	launch := ec.LaunchRequest{ProjectID: v.request.ProjectID, AgentID: v.request.AgentID, Trigger: ec.Trigger{Kind: "task", TaskID: v.request.TaskID.String()}, Purpose: "task/work", Policy: ec.Policy{SchemaVersion: 1, DeniedToolIDs: []i.ToolID{}, AllowedResourceConstraints: []json.RawMessage{}}, Lineage: ec.Lineage{DispatchID: v.request.DispatchID}, Meta: f.CommandMeta{RequestID: v.request.RequestID, IdempotencyKey: f.IdempotencyKey("scheduler_dispatch:" + v.request.DispatchID)}}
	launchAuthority := &taskLaunchTestAuthority{request: launch, intent: c.TaskLaunchIntent{ProjectID: v.request.ProjectID, TaskID: v.request.TaskID, AgentID: v.request.AgentID, SprintID: v.request.CurrentSprintID, DispatchID: v.request.DispatchID, Origin: c.TaskDispatchRelaunch, Relaunch: &source}}
	launcher, err := NewTaskLaunchProvider(v.store, v.service.deps.Authority, launchAuthority)
	if err != nil {
		t.Fatal(err)
	}
	launchCtx := context.WithValue(v.ctx, taskLaunchTestProof{}, launchAuthority)
	launchPlan, err := launcher.DiscoverLaunch(launchCtx, v.actor, launch)
	if err != nil {
		t.Fatal("recorded relaunch did not supply Launch source", err)
	}
	permit, err := launcher.ValidateLaunchInTx(launchCtx, v.store.tx, v.actor, launch, launchPlan)
	if err != nil || !permit.Matches(launch) || v.store.writes != 1 || !sameValue(before, v.store.task) {
		t.Fatal("relaunch Launch source invented claim/Task mutation", err)
	}
	v.store.hideOrigin = true
	permit, err = launcher.ValidateLaunchInTx(launchCtx, v.store.tx, v.actor, launch, launchPlan)
	if err == nil || permit.Matches(launch) {
		t.Fatal("public relaunch projection replaced Work origin")
	}
	for _, mode := range []string{"paused", "sprint", "version", "same-version-edit", "state", "assignee", "blocker", "active-waiting", "pending-unknown", "missing-facts", "foreign-history", "denied-agent"} {
		v = newRelaunchControl(t)
		plan = v.discover(t)
		switch mode {
		case "paused":
			v.projects.value.Config.Enabled = false
		case "sprint":
			v.projects.value.Project.CurrentSprintID = nil
		case "version":
			v.store.task.Version++
		case "same-version-edit":
			v.store.task.Title = "changed after discovery"
		case "state":
			v.store.task.State = c.TaskStateTodo
		case "assignee":
			other := pureID[i.Agent](t, 135)
			v.store.task.AssigneeAgentID = &other
		case "blocker":
			v.store.unresolved = 1
		case "active-waiting":
			v.active.value.Active = []ec.ActiveTaskExecution{{TaskID: v.request.TaskID.String(), ExecutionID: pureID[i.Execution](t, 136), AgentID: v.request.AgentID, Status: ec.Waiting}}
		case "pending-unknown":
			v.pending.value.Pending = []ec.PendingTaskDispatch{{TaskID: v.request.TaskID.String(), DispatchID: pureID[f.Request](t, 137).String(), AgentID: v.request.AgentID, SprintID: v.request.CurrentSprintID.String()}}
		case "missing-facts":
			v.active.value.Active = nil
		case "foreign-history":
			v.pending.value.HistoryTaskIDs = []string{pureID[c.Task](t, 138).String()}
		case "denied-agent":
			v.agents.err = fault(f.Forbidden)
		}
		applied, err = v.service.RecordTaskRelaunchInTx(v.ctx, v.store.tx, v.actor, v.request, plan)
		if err == nil || applied != nil || v.store.writes != 0 {
			t.Fatal("ineligible current facts wrote origin", mode, err)
		}
	}
}

type forgedRelaunchPlan struct{}

func (forgedRelaunchPlan) RequiredLocks() []f.LockRequest { return nil }

type forgedRelaunchApplied struct{ source c.TaskRelaunchSource }

func (v forgedRelaunchApplied) Source() c.TaskRelaunchSource { return v.source }

func TestTaskRelaunchRejectsForeignProofAndPreservesOriginalOutcome(t *testing.T) {
	v := newRelaunchControl(t)
	var typed *relaunchTestAuthority
	for _, missing := range []c.SchedulerRelaunchAuthority{nil, typed} {
		deps := v.service.deps
		deps.Scheduler = missing
		_, err := NewTaskRelaunch(v.store, deps)
		pureCode(t, err, f.DependencyUnbound)
	}
	_, err := NewTaskRelaunch(&denialStore{}, v.service.deps)
	pureCode(t, err, f.DependencyUnbound)
	plan, err := v.service.DiscoverTaskRelaunch(context.Background(), v.actor, v.request)
	if err == nil || plan != nil || v.store.queries != 0 {
		t.Fatal("Service name replaced original private intent")
	}
	plan = v.discover(t)
	queries := v.store.queries
	for _, mode := range []string{"forged", "request", "issuer", "transaction", "locks", "proof"} {
		request, service, tx, candidate := v.request, v.service, v.store.tx, plan
		switch mode {
		case "forged":
			candidate = forgedRelaunchPlan{}
		case "request":
			request.RequestID = pureID[f.Request](t, 139)
		case "issuer":
			service, err = NewTaskRelaunch(v.store, v.service.deps)
			if err != nil {
				t.Fatal(err)
			}
		case "transaction":
			tx = f.NewTx()
		case "locks":
			v.store.missing = true
		case "proof":
			v.intents.deny = true
		}
		applied, e := service.RecordTaskRelaunchInTx(v.ctx, tx, v.actor, request, candidate)
		if e == nil || applied != nil || v.store.queries != queries || v.store.writes != 0 {
			t.Fatal("foreign proof reached Work SQL", mode, e)
		}
		v.store.missing, v.intents.deny = false, false
	}
	err = v.service.CheckTaskRelaunchAppliedInTx(v.ctx, v.store.tx, v.actor, v.request, plan, forgedRelaunchApplied{})
	pureCode(t, err, f.Forbidden)
	for _, mode := range []string{"insert-error", "missing-postimage", "cancel-current"} {
		v = newRelaunchControl(t)
		plan = v.discover(t)
		ctx, cancel := context.WithCancel(v.ctx)
		switch mode {
		case "insert-error":
			v.store.insertError = errors.New("private SQL material")
		case "missing-postimage":
			v.store.hideOrigin = true
		case "cancel-current":
			v.agents.after = cancel
		}
		applied, e := v.service.RecordTaskRelaunchInTx(ctx, v.store.tx, v.actor, v.request, plan)
		cancel()
		if e == nil || applied != nil {
			t.Fatal("failed tentative write issued applied witness", mode)
		}
		if mode == "cancel-current" && (!errors.Is(e, context.Canceled) || v.store.writes != 0 || v.active.calls != 0) {
			t.Fatal("cancelled authority continued into later dependencies")
		}
	}
	// The original callback actually completes before a cancelled physical
	// result returns. Unknown keeps its original attempt/cause and no plan.
	for _, unknown := range []bool{false, true} {
		v = newRelaunchControl(t)
		ctx, cancel := context.WithCancel(v.ctx)
		v.store.afterRead = cancel
		result := f.CommittedResult()
		attempt := pureID[f.TransactionAttempt](t, 145)
		cause, e := readCause("controlled-relaunch")
		if e != nil {
			t.Fatal(e)
		}
		if unknown {
			result = f.UnknownResult(attempt, cause)
		}
		v.store.readResult = &result
		plan, e = v.service.DiscoverTaskRelaunch(ctx, v.actor, v.request)
		cancel()
		if plan != nil || !v.store.returned || v.store.queries != 4 || v.store.writes != 0 {
			t.Fatal("cancelled/unknown original callback produced a plan")
		}
		if unknown {
			var actual *f.Fault
			if !errors.As(e, &actual) || actual.CommitState != f.Unknown || actual.CauseID != attempt.String() {
				t.Fatal("original Unknown identity lost", e)
			}
		} else if !errors.Is(e, context.Canceled) {
			t.Fatal("original cancellation lost", e)
		}
	}
}
