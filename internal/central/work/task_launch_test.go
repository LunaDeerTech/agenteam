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
	c "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
	"github.com/jackc/pgx/v5/pgconn"
)

type taskLaunchTestProof struct{}
type taskLaunchTestAuthority struct {
	intent  c.TaskLaunchIntent
	request ec.LaunchRequest
	calls   int
	check   func(context.Context) error
}

func (a *taskLaunchTestAuthority) RequireTaskLaunchInTx(ctx context.Context, _ f.Tx, actor i.Actor, request ec.LaunchRequest) (c.TaskLaunchIntent, error) {
	a.calls++
	if a.check != nil {
		if err := a.check(ctx); err != nil {
			return c.TaskLaunchIntent{}, err
		}
	}
	if ctx.Value(taskLaunchTestProof{}) != a || !sameTaskLaunchRequest(request, a.request) || actor.Details().CauseRef != a.intent.DispatchID {
		return c.TaskLaunchIntent{}, fault(f.Forbidden)
	}
	return a.intent, nil
}

type taskLaunchTestProject struct {
	pc.ProjectAuthority
	value pc.SchedulerProject
}

func (p *taskLaunchTestProject) RequireSchedulerProjectInTx(context.Context, f.Tx, i.ProjectID) (pc.SchedulerProject, error) {
	return p.value.Clone(), nil
}

type taskLaunchTestStore struct {
	taskTriggerTestStore
	t          *testing.T
	claim      schedulerClaimRecord
	task       c.Task
	sprint     c.Sprint
	milestone  c.Milestone
	unresolved int64
	writes     int
}

func (s *taskLaunchTestStore) InTx(tx f.Tx) (postgres.SQLExecutor, error) {
	if tx != s.tx {
		return nil, fault(f.Forbidden)
	}
	return s, nil
}
func (s *taskLaunchTestStore) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	s.writes++
	return pgconn.CommandTag{}, errors.New("unexpected source write")
}
func (s *taskLaunchTestStore) QueryRow(_ context.Context, query string, args ...any) postgres.Row {
	s.queries++
	if len(args) != 2 || args[0] != s.task.ProjectID.String() {
		s.t.Fatal("incorrect source project")
	}
	switch {
	case strings.Contains(query, "FROM agenteam_work.task_scheduler_claims"):
		if args[1] != s.claim.Request.DispatchID {
			s.t.Fatal("incorrect claim")
		}
		raw, err := canonical(s.claim)
		if err != nil {
			s.t.Fatal(err)
		}
		r := s.claim
		return taskTriggerRow{r.Request.TaskID.String(), r.Request.AgentID.String(), r.Request.RequestID.String(), int64(r.Before.Version), int64(r.After.Version), r.History.ID.String(), r.Header.EventID.String(), raw, r.After.UpdatedAt.Time()}
	case strings.Contains(query, "FROM agenteam_work.tasks "):
		v := s.task
		if args[1] != v.ID.String() {
			s.t.Fatal("incorrect Task")
		}
		var agent *string
		if v.AssigneeAgentID != nil {
			text := v.AssigneeAgentID.String()
			agent = &text
		}
		return taskTriggerRow{v.ID.String(), v.ProjectID.String(), v.MilestoneID.String(), v.SprintID.String(), v.Title, v.Description, v.Type, v.Priority, v.State, agent, v.Plan, v.ManualRank, v.Version, v.CreatedAt.Time(), v.UpdatedAt.Time()}
	case strings.Contains(query, "FROM agenteam_work.sprints "):
		v := s.sprint
		if args[1] != v.ID.String() {
			s.t.Fatal("incorrect Sprint")
		}
		by, err := json.Marshal(v.StartedBy)
		if err != nil {
			s.t.Fatal(err)
		}
		at := v.StartedAt.Time()
		return taskTriggerRow{v.ID.String(), v.ProjectID.String(), v.MilestoneID.String(), v.Title, v.Description, v.ManualRank, v.Version, v.CreatedAt.Time(), v.UpdatedAt.Time(), &at, by, nil, nil}
	case strings.Contains(query, "FROM agenteam_work.milestones "):
		v := s.milestone
		if args[1] != v.ID.String() {
			s.t.Fatal("incorrect Milestone")
		}
		return taskTriggerRow{v.ID.String(), v.ProjectID.String(), v.Title, v.Description, v.ManualRank, v.Version, v.CreatedAt.Time(), v.UpdatedAt.Time()}
	case strings.Contains(query, "FROM agenteam_work.task_blockers "):
		if args[1] != s.task.ID.String() {
			s.t.Fatal("incorrect blockers")
		}
		return taskTriggerRow{s.unresolved}
	default:
		s.t.Fatal("unexpected SQL")
	}
	return denialRow{}
}

func newTaskLaunchControl(t *testing.T) (*TaskLaunchProvider, *taskLaunchTestStore, *taskLaunchTestProject, *taskLaunchTestAuthority, context.Context, i.Actor, ec.LaunchRequest) {
	t.Helper()
	claim, actor := claimControlRecord(t)
	v := claim.After.Clone()
	at := v.UpdatedAt
	s := &taskLaunchTestStore{taskTriggerTestStore: taskTriggerTestStore{tx: f.NewTx()}, t: t, claim: claim, task: v}
	s.sprint = c.Sprint{ID: v.SprintID, ProjectID: v.ProjectID, MilestoneID: v.MilestoneID, Title: "Sprint", Description: "source", ManualRank: v.ManualRank, Version: 2, CreatedAt: v.CreatedAt, UpdatedAt: at, StartedAt: &at, StartedBy: &c.ActorHistory{Kind: i.Human, UserID: pureID[i.User](t, 1).String()}, State: c.Current}
	s.milestone = c.Milestone{ID: v.MilestoneID, ProjectID: v.ProjectID, Title: "Milestone", Description: "source", ManualRank: v.ManualRank, Version: 1, CreatedAt: v.CreatedAt, UpdatedAt: at}
	sprint := v.SprintID
	projects := &taskLaunchTestProject{value: pc.SchedulerProject{Project: pc.ProjectRef{ID: v.ProjectID, OwnerUserID: pureID[i.User](t, 1), Name: "launch", NormalizedName: "launch", Lifecycle: pc.Active, Version: 3, CurrentSprintID: &sprint, CreatedAt: v.CreatedAt, UpdatedAt: at}, Config: pc.ProjectSchedulerConfig{Enabled: true}}}
	authority, err := NewAuthority(s, projects)
	if err != nil {
		t.Fatal(err)
	}
	r := ec.LaunchRequest{ProjectID: v.ProjectID, AgentID: *v.AssigneeAgentID, Trigger: ec.Trigger{Kind: "task", TaskID: v.ID.String()}, Purpose: "task/work", Policy: ec.Policy{SchemaVersion: 1, DeniedToolIDs: []i.ToolID{}, AllowedResourceConstraints: []json.RawMessage{}}, Lineage: ec.Lineage{DispatchID: claim.Request.DispatchID}, Meta: f.CommandMeta{RequestID: claim.Request.RequestID, IdempotencyKey: f.IdempotencyKey("scheduler_dispatch:" + claim.Request.DispatchID)}}
	intent := &taskLaunchTestAuthority{request: r.Clone(), intent: c.TaskLaunchIntent{ProjectID: v.ProjectID, TaskID: v.ID, AgentID: r.AgentID, SprintID: v.SprintID, DispatchID: r.Lineage.DispatchID, ClaimedVersion: v.Version}}
	p, err := NewTaskLaunchProvider(s, authority, intent)
	if err != nil {
		t.Fatal(err)
	}
	if r.Validate() != nil || s.sprint.Validate() != nil || s.milestone.Validate() != nil || projects.value.Project.Validate() != nil {
		t.Fatal("invalid control fixture")
	}
	return p, s, projects, intent, context.WithValue(context.Background(), taskLaunchTestProof{}, intent), actor, r
}

func TestTaskLaunchCurrentClaimAndFrozenSource(t *testing.T) {
	p, s, _, intent, ctx, actor, r := newTaskLaunchControl(t)
	// A legitimate change before discovery is not compared to the old claim's
	// full postimage. Once observed, it is frozen until final validation.
	s.task.Title = "new private Task title"
	s.task.Version++
	r.Policy.DeniedToolIDs = []i.ToolID{pureID[i.Tool](t, 96)}
	intent.request = r.Clone()
	plan, err := p.DiscoverLaunch(ctx, actor, r)
	if err != nil {
		t.Fatal(err)
	}
	if s.queries != 5 || s.acquires != 1 || intent.calls != 1 || len(plan.RequiredLocks()) != 5 {
		t.Fatal("discovery omitted real source/proof")
	}
	if strings.Contains(fmt.Sprint(plan), s.task.Title) {
		t.Fatal("plan leaked material")
	}
	locks := plan.RequiredLocks()
	locks[0].Mode = f.Shared
	if sameTaskLocks(locks, plan.RequiredLocks()) {
		t.Fatal("plan lock alias")
	}
	s.tx = f.NewTx() // final callback has a distinct original physical Tx.
	permit, err := p.ValidateLaunchInTx(ctx, s.tx, actor, r, plan)
	if err != nil || !permit.Matches(r) || s.acquires != 1 || s.writes != 0 || intent.calls != 2 {
		t.Fatal("same-Tx validation", err)
	}
	s.task.Title = "later private title"
	s.task.Version++
	permit, err = p.ValidateLaunchInTx(ctx, s.tx, actor, r, plan)
	pureCode(t, err, f.ConfirmationStale)
	if permit.Matches(r) {
		t.Fatal("changed source kept permit")
	}
	before := s.queries
	changed := r.Clone()
	changed.Meta.RequestID = pureID[f.Request](t, 97)
	_, err = p.ValidateLaunchInTx(ctx, s.tx, actor, changed, plan)
	pureCode(t, err, f.Forbidden)
	changed = r.Clone()
	changed.Policy.DeniedToolIDs = []i.ToolID{}
	_, err = p.ValidateLaunchInTx(ctx, s.tx, actor, changed, plan)
	pureCode(t, err, f.Forbidden)
	other := *p
	_, err = other.ValidateLaunchInTx(ctx, s.tx, actor, r, plan)
	pureCode(t, err, f.Forbidden)
	if s.queries != before {
		t.Fatal("changed request/issuer reached SQL")
	}
}

func TestTaskLaunchRejectsUnprovenOrIneligibleSource(t *testing.T) {
	p, s, projects, intents, ctx, actor, r := newTaskLaunchControl(t)
	authority, _ := NewAuthority(s, projects)
	var typed *taskLaunchTestAuthority
	for _, missing := range []c.TaskLaunchAuthority{nil, typed} {
		_, err := NewTaskLaunchProvider(s, authority, missing)
		pureCode(t, err, f.DependencyUnbound)
	}
	if _, err := NewTaskLaunchProvider(&denialStore{}, authority, intents); err == nil {
		t.Fatal("foreign Store")
	}
	_, err := p.DiscoverLaunch(context.Background(), actor, r)
	pureCode(t, err, f.Forbidden)
	if s.queries != 0 {
		t.Fatal("Service name granted source read")
	}
	for _, mutate := range []func(*ec.LaunchRequest){func(r *ec.LaunchRequest) {
		r.Policy.AllowedResourceConstraints = []json.RawMessage{json.RawMessage(`{"unknown":true}`)}
	}, func(r *ec.LaunchRequest) { v := pureID[i.Execution](t, 99); r.Lineage.RetryOf = &v }, func(r *ec.LaunchRequest) { r.Purpose = "task/review" }} {
		changed := r.Clone()
		mutate(&changed)
		if plan, e := p.DiscoverLaunch(ctx, actor, changed); e == nil || plan != nil {
			t.Fatal("unsupported semantic accepted")
		}
	}
	if s.queries != 0 {
		t.Fatal("unsupported request read source")
	}
	for _, mode := range []string{"locks", "foreign-tx", "paused", "old-sprint", "wrong-agent", "old-version", "blocker", "claim-mismatch"} {
		p, s, projects, intents, ctx, actor, r = newTaskLaunchControl(t)
		plan, e := p.DiscoverLaunch(ctx, actor, r)
		if e != nil {
			t.Fatal(e)
		}
		tx := s.tx
		switch mode {
		case "locks":
			s.missing = true
		case "foreign-tx":
			tx = f.NewTx()
		case "paused":
			projects.value.Config.Enabled = false
		case "old-sprint":
			v := pureID[pc.Sprint](t, 98)
			projects.value.Project.CurrentSprintID = &v
		case "wrong-agent":
			v := pureID[i.Agent](t, 98)
			s.task.AssigneeAgentID = &v
		case "old-version":
			s.task.Version = intents.intent.ClaimedVersion - 1
		case "blocker":
			s.unresolved = 1
		case "claim-mismatch":
			intents.intent.ClaimedVersion++
		}
		permit, e := p.ValidateLaunchInTx(ctx, tx, actor, r, plan)
		if e == nil || permit.Matches(r) || s.writes != 0 {
			t.Fatal("ineligible source accepted", mode)
		}
	}
}

func TestTaskLaunchPhysicalUnknownAndOriginalCancellation(t *testing.T) {
	p, s, _, intent, ctx, actor, r := newTaskLaunchControl(t)
	cause, err := readCause("launch-control")
	if err != nil {
		t.Fatal(err)
	}
	original := f.UnknownResult(pureID[f.TransactionAttempt](t, 100), cause)
	s.physical = &original
	cancelled, cancel := context.WithCancel(ctx)
	s.cancelPhysical = cancel
	plan, err := p.DiscoverLaunch(cancelled, actor, r)
	pureCode(t, err, f.CommitUnknown)
	var problem *f.Fault
	if plan != nil || !errors.As(err, &problem) || problem.CommitState != f.Unknown || problem.CauseID != original.AttemptID().String() || s.queries != 0 {
		t.Fatal("physical Unknown lost")
	}
	s.physical = nil
	s.cancelPhysical = nil
	for _, sentinel := range []error{context.Canceled, context.DeadlineExceeded} {
		intent.check = func(context.Context) error { return fmt.Errorf("private launch proof: %w", sentinel) }
		plan, err = p.DiscoverLaunch(ctx, actor, r)
		if plan != nil || !errors.Is(err, sentinel) || strings.Contains(fmt.Sprint(err), "private launch") || s.queries != 0 {
			t.Fatal("unsafe original cancellation")
		}
	}
	current, cancel := context.WithCancel(ctx)
	returned := false
	intent.check = func(context.Context) error { cancel(); returned = true; return nil }
	plan, err = p.DiscoverLaunch(current, actor, r)
	if plan != nil || !returned || !errors.Is(err, context.Canceled) || s.queries != 0 {
		t.Fatal("late proof cancellation published source")
	}
}
