package project

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	audit "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	c "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type schedulerIntentFunc func(context.Context, f.Tx, i.Actor, i.ProjectID, i.AgentID, i.AccessIntent) (c.SchedulerIntent, error)

func (fn schedulerIntentFunc) RequireSchedulerIntentInTx(ctx context.Context, tx f.Tx, actor i.Actor, project i.ProjectID, agent i.AgentID, intent i.AccessIntent) (c.SchedulerIntent, error) {
	return fn(ctx, tx, actor, project, agent, intent)
}

func schedulerFactsFixture(t *testing.T) (*preparationProjectFixture, *c.ProjectSchedulerConfig) {
	x := newPreparationProjectFixture(t)
	config := &c.ProjectSchedulerConfig{}
	original := x.store.row
	x.store.row = func(query string, args ...any) postgres.Row {
		if strings.HasPrefix(query, "SELECT scheduler_enabled,") {
			if len(args) != 1 || args[0] != x.project.String() {
				t.Fatal("wrong config target")
			}
			return valuesRow(config.Enabled, config.MaxConcurrency)
		}
		return original(query, args...)
	}
	return x, config
}

func TestSchedulerProjectReadsCurrentConfiguration(t *testing.T) {
	x, config := schedulerFactsFixture(t)
	value, err := x.authority.RequireSchedulerProjectInTx(context.Background(), x.store.tx, x.project)
	if err != nil || value.Project.ID != x.project || value.Config.Enabled || value.Config.MaxConcurrency != nil {
		t.Fatal("paused/unlimited actual facts lost", err)
	}
	limit := int64(2)
	config.Enabled, config.MaxConcurrency = true, &limit
	value, err = x.authority.RequireSchedulerProjectInTx(context.Background(), x.store.tx, x.project)
	if err != nil || !value.Config.Enabled || value.Config.MaxConcurrency == nil || *value.Config.MaxConcurrency != 2 {
		t.Fatal("configuration was cached or guessed", err)
	}
	// A real driver must already hold the entire union. No Acquire call is
	// provided by this authorityStore; accidentally acquiring would panic.
	x.store.lockErr = errors.New("missing scheduler lock")
	_, err = x.authority.RequireSchedulerProjectInTx(context.Background(), x.store.tx, x.project)
	hasCode(t, err, f.DependencyUnavailable)
	x.store.lockErr = nil
	x.initialized = false
	_, err = x.authority.RequireSchedulerProjectInTx(context.Background(), x.store.tx, x.project)
	hasCode(t, err, f.ProjectNotActive)
	x.initialized, x.lifecycle = true, c.Archived
	_, err = x.authority.RequireSchedulerProjectInTx(context.Background(), x.store.tx, x.project)
	hasCode(t, err, f.ProjectNotActive)
}

func TestSchedulerDefaultsArePersistedFacts(t *testing.T) {
	x := newPreparationProjectFixture(t)
	limit, version := int64(4), f.Version(3)
	var maximum *int64 = &limit
	x.store.row = func(query string, args ...any) postgres.Row {
		if query != "SELECT default_project_scheduler_max_concurrency,version FROM agenteam_project.scheduler_defaults WHERE singleton" || len(args) != 0 {
			t.Fatal("unexpected defaults query")
		}
		return valuesRow(maximum, version)
	}
	value, err := x.authority.SchedulerDefaultsInTx(context.Background(), x.store.tx)
	if err != nil || value.Version != 3 || value.MaxConcurrency == nil || *value.MaxConcurrency != 4 {
		t.Fatal("actual defaults not read", err)
	}
	if len(x.store.locks) != 1 || x.store.locks[0].Key.Canonical() != schedulerDefaultsLock(f.Shared).Key.Canonical() || x.store.locks[0].Mode != f.Shared {
		t.Fatal("defaults read omitted original shared lock")
	}
	maximum, version = nil, 4
	value, err = x.authority.SchedulerDefaultsInTx(context.Background(), x.store.tx)
	if err != nil || value.Version != 4 || value.MaxConcurrency != nil {
		t.Fatal("unlimited confused with missing defaults", err)
	}
	_, err = x.authority.SchedulerDefaultsInTx(context.Background(), f.NewTx())
	hasCode(t, err, f.DependencyUnavailable)
	x.store.row = func(string, ...any) postgres.Row {
		return rowFunc(func(...any) error { return errors.New("private-default-row-canary") })
	}
	_, err = x.authority.SchedulerDefaultsInTx(context.Background(), x.store.tx)
	if err == nil || strings.Contains(fmt.Sprint(err), "private-default-row-canary") {
		t.Fatal("missing defaults became success or exposed material")
	}
}

func TestSchedulerServiceAccessRequiresOriginalIntent(t *testing.T) {
	x, config := schedulerFactsFixture(t)
	reg, err := i.RegisterService(i.Scheduler)
	if err != nil {
		t.Fatal(err)
	}
	scope, _ := i.InProject(x.project)
	dispatch, agent := testID[struct{}](t).String(), testID[i.Agent](t)
	actor, err := reg.Actor(dispatch, scope)
	if err != nil {
		t.Fatal(err)
	}
	accepted, calls, badMapping := false, 0, false
	access, err := NewSchedulerExecutionAccess(x.authority, schedulerIntentFunc(func(ctx context.Context, tx f.Tx, actual i.Actor, project i.ProjectID, target i.AgentID, intent i.AccessIntent) (c.SchedulerIntent, error) {
		calls++
		if tx != x.store.tx || !actual.Equal(actor) || project != x.project || target != agent {
			t.Fatal("original intent inputs changed")
		}
		if !accepted {
			return c.SchedulerIntent{}, fault(f.Forbidden)
		}
		v := c.SchedulerIntent{ProjectID: project, AgentID: target, DispatchID: dispatch, SprintID: x.sprint}
		if badMapping {
			v.DispatchID = testID[struct{}](t).String()
		}
		return v, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	run := func(intent i.AccessIntent) error {
		return access.RequireExecutionProjectInTx(context.Background(), x.store.tx, actor, x.project, agent, intent)
	}
	hasCode(t, run(i.Launch), f.Forbidden)
	if len(x.order) != 2 || x.order[0] != "executor" || x.order[1] != "locks" {
		t.Fatal("name-only actor reached protected Project read")
	}
	accepted = true
	hasCode(t, run(i.Launch), f.InvalidState) // persisted paused
	if err = run(i.Read); err != nil {
		t.Fatal("paused original Lookup was denied", err)
	}
	config.Enabled = true
	if err = run(i.Launch); err != nil {
		t.Fatal("current intent/config rejected", err)
	}
	badMapping = true
	hasCode(t, run(i.Launch), f.Forbidden)
	badMapping = false
	x.lifecycle = c.Archived
	if err = run(i.Read); err != nil {
		t.Fatal("historical original Lookup requires active Project", err)
	}
	hasCode(t, run(i.Launch), f.ProjectNotActive)
	before := calls
	hasCode(t, access.RequireExecutionProjectInTx(context.Background(), x.store.tx, testActor(t), x.project, agent, i.Launch), f.Forbidden)
	if calls != before {
		t.Fatal("wrong Actor reached Scheduler provider")
	}
	if _, err = NewSchedulerExecutionAccess(x.authority, nil); err == nil {
		t.Fatal("missing private intent authority accepted")
	}
}

func TestSchedulerConfigCancellationWaitsForOriginalRead(t *testing.T) {
	x, _ := schedulerFactsFixture(t)
	original := x.store.row
	entered, release := make(chan struct{}), make(chan struct{})
	x.store.row = func(q string, args ...any) postgres.Row {
		if strings.HasPrefix(q, "SELECT scheduler_enabled,") {
			return rowFunc(func(...any) error {
				close(entered)
				<-release
				return fmt.Errorf("private-scheduler-canary: %w", context.Canceled)
			})
		}
		return original(q, args...)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	released, joined := false, false
	defer func() {
		cancel()
		if !released {
			close(release)
		}
		if !joined {
			<-done
		}
	}()
	go func() { _, err := x.authority.RequireSchedulerProjectInTx(ctx, x.store.tx, x.project); done <- err }()
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("read not entered")
	}
	cancel()
	select {
	case <-done:
		joined = true
		t.Fatal("returned before original SQL tail")
	default:
	}
	close(release)
	released = true
	err := <-done
	joined = true
	if err != context.Canceled || strings.Contains(fmt.Sprint(err), "canary") {
		t.Fatal("cancel material leaked or lost sentinel", err)
	}
}

// SQL and external appenders are explicitly controlled here. The service,
// command digest, durable plan parsing, current Owner checks and all write
// ordering are the actual implementation; real PG atomicity has a separate test.
type schedulerAuditFunc func(context.Context, f.Tx, audit.Entry, audit.AppendKey) (audit.AppendReceipt, error)

func (fn schedulerAuditFunc) AppendInTx(ctx context.Context, tx f.Tx, e audit.Entry, k audit.AppendKey) (audit.AppendReceipt, error) {
	return fn(ctx, tx, e, k)
}

type schedulerActivityFunc func(context.Context, f.Tx, i.Actor) error

func (fn schedulerActivityFunc) TouchActivityInTx(ctx context.Context, tx f.Tx, a i.Actor) error {
	return fn(ctx, tx, a)
}

type schedulerEventAppender struct {
	preparedOnlyAppender
	append func(context.Context, f.Tx, i.Actor, event.Event, oc.AppendPlan) (oc.AppendReceipt, error)
}

func (a schedulerEventAppender) AppendEventInTx(ctx context.Context, tx f.Tx, actor i.Actor, e event.Event, plan oc.AppendPlan) (oc.AppendReceipt, error) {
	return a.append(ctx, tx, actor, e, plan)
}

func TestSchedulerOwnerUpdateReplayAndCurrentGate(t *testing.T) {
	ctx := context.Background()
	order := []string{}
	store := &roundTestStore{authorityStore: &authorityStore{tx: f.NewTx(), order: &order}}
	service, actor := roundTestService(t, store)
	project := testProject(t)
	project.OwnerUserID, _ = f.ParseID[i.User](actor.Details().UserID)
	creation := testID[c.Creation](t)
	config := c.ProjectSchedulerConfig{}
	type savedCommand struct {
		id, user, key, semantic, state string
		plan, result                   []byte
		eventID                        *string
	}
	commands := map[string]*savedCommand{}
	updates, audits, events, activities := 0, 0, 0, 0
	sessionError := error(nil)
	authority, err := NewAuthority(store, AuthorityDependencies{Sessions: sessionFunc(func(context.Context, f.Tx, i.Actor) error { return sessionError })})
	if err != nil {
		t.Fatal(err)
	}
	service.state().deps.Authority = authority
	registered, err := c.RegisterProjectEvents(event.NewCatalog())
	if err != nil {
		t.Fatal(err)
	}
	service.state().deps.ProjectEvents = registered
	store.row = func(q string, args ...any) postgres.Row {
		switch {
		case q == "SELECT clock_timestamp()":
			return valuesRow(project.UpdatedAt.Time().Add(time.Second))
		case strings.HasPrefix(q, "SELECT scheduler_enabled,"):
			return valuesRow(config.Enabled, config.MaxConcurrency)
		case strings.Contains(q, "FROM agenteam_project.projects"):
			return valuesRow(project.ID.String(), project.OwnerUserID.String(), project.Name, project.NormalizedName, project.Description, string(project.Lifecycle), int64(project.Version), nil, project.CreatedAt.Time(), project.UpdatedAt.Time(), nil, creation.String(), true, nil)
		case strings.Contains(q, "FROM agenteam_project.commands"):
			r := commands[args[2].(string)]
			if r == nil {
				return rowFunc(func(...any) error { return pgx.ErrNoRows })
			}
			return valuesRow(r.id, project.ID.String(), r.user, "update", r.key, r.semantic, r.state, r.result, r.plan, r.eventID)
		default:
			t.Fatalf("unexpected Project query: %s", q)
			return nil
		}
	}
	store.exec = func(q string, args ...any) (pgconn.CommandTag, error) {
		switch {
		case strings.HasPrefix(q, "INSERT INTO agenteam_project.commands"):
			r := &savedCommand{id: args[0].(string), user: args[2].(string), key: args[3].(string), semantic: args[4].(string)}
			if strings.Contains(q, "'planned'") {
				r.state = "planned"
				r.plan = append([]byte(nil), args[5].([]byte)...)
				v := args[6].(string)
				r.eventID = &v
			} else {
				r.state = "completed"
				r.result = append([]byte(nil), args[5].([]byte)...)
			}
			commands[r.key] = r
		case strings.HasPrefix(q, "UPDATE agenteam_project.projects SET name="):
			if int64(project.Version) != args[6].(int64) {
				return pgconn.NewCommandTag("UPDATE 0"), nil
			}
			project.Name, project.NormalizedName, project.Description = args[1].(string), args[2].(string), args[3].(string)
			project.Version = f.Version(args[4].(int64))
			project.UpdatedAt = instant(args[5].(time.Time))
		case strings.HasPrefix(q, "UPDATE agenteam_project.projects SET scheduler_enabled="):
			if int64(project.Version) != args[3].(int64) {
				t.Fatal("config and Project versions diverged")
			}
			config = c.ProjectSchedulerConfig{Enabled: args[1].(bool), MaxConcurrency: args[2].(*int64)}.Clone()
			updates++
		case strings.HasPrefix(q, "UPDATE agenteam_project.commands SET state='completed'"):
			for _, r := range commands {
				if r.id == args[0].(string) {
					r.state = "completed"
					r.result = append([]byte(nil), args[1].([]byte)...)
				}
			}
		default:
			t.Fatalf("unexpected Project mutation: %s", q)
		}
		return pgconn.NewCommandTag("UPDATE 1"), nil
	}
	assertPostimage := func(tx f.Tx) {
		t.Helper()
		if tx != store.tx || !config.Enabled || config.MaxConcurrency == nil || *config.MaxConcurrency != 2 || project.Version != 2 {
			t.Fatal("external append before real config postimage")
		}
	}
	service.state().deps.Audit = schedulerAuditFunc(func(_ context.Context, tx f.Tx, e audit.Entry, _ audit.AppendKey) (audit.AppendReceipt, error) {
		assertPostimage(tx)
		audits++
		raw := string(e.Fields().Metadata.JSON())
		if !strings.Contains(raw, `"changed_fields":["scheduler_enabled","scheduler_max_concurrency"]`) {
			t.Fatal("wrong audit fields")
		}
		return audit.AppendReceipt{}, nil
	})
	service.state().deps.Events = schedulerEventAppender{append: func(_ context.Context, tx f.Tx, _ i.Actor, e event.Event, _ oc.AppendPlan) (oc.AppendReceipt, error) {
		assertPostimage(tx)
		events++
		return oc.AppendReceipt{}, nil
	}}
	service.state().deps.Activity = schedulerActivityFunc(func(_ context.Context, tx f.Tx, _ i.Actor) error { assertPostimage(tx); activities++; return nil })
	expected, limit := f.Version(1), int64(2)
	meta := f.CommandMeta{RequestID: testID[f.Request](t), IdempotencyKey: "enable-scheduler", ExpectedVersion: &expected}
	request := c.UpdateProjectRequest{Scheduler: &c.ProjectSchedulerConfig{Enabled: true, MaxConcurrency: &limit}}
	got, err := service.UpdateProject(ctx, actor, meta, project.ID, request)
	if err != nil || got.Version != 2 || updates != 1 || audits != 1 || events != 1 || activities != 1 {
		t.Fatal("actual Update failed", err, updates, audits, events, activities)
	}
	for _, plan := range store.acquirePlans {
		found := false
		for _, l := range plan {
			if l.Key.Canonical() == schedulerLock(project.ID).Key.Canonical() && l.Mode == f.Exclusive {
				found = true
			}
		}
		if !found {
			t.Fatal("phase omitted schedule writer lock")
		}
	}
	got, err = service.UpdateProject(ctx, actor, meta, project.ID, request)
	if err != nil || got.Version != 2 || updates != 1 || audits != 1 || events != 1 || activities != 1 {
		t.Fatal("replay repeated mutation", err)
	}
	current, err := service.GetSchedulerConfig(ctx, actor, project.ID)
	if err != nil || current.Project.Version != 2 || !current.Config.Enabled || *current.Config.MaxConcurrency != 2 {
		t.Fatal("current read disagrees", err)
	}
	// A new intent with the same actual configuration is a durable no-op.
	expected = 2
	meta.IdempotencyKey = "scheduler-noop"
	got, err = service.UpdateProject(ctx, actor, meta, project.ID, request)
	if err != nil || got.Version != 2 || updates != 1 || audits != 1 || events != 1 || activities != 2 {
		t.Fatal("no-op changed facts", err)
	}
	raw, _ := json.Marshal(got)
	if strings.Contains(string(raw), "scheduler_") {
		t.Fatal("old Project receipt shape changed")
	}
	meta.IdempotencyKey = "enable-scheduler"
	expected = 1
	different := request.Scheduler.Clone()
	different.Enabled = false
	_, err = service.UpdateProject(ctx, actor, meta, project.ID, c.UpdateProjectRequest{Scheduler: &different})
	hasCode(t, err, f.IdempotencyKeyReused)
	_, err = service.GetSchedulerConfig(ctx, testActor(t), project.ID)
	hasCode(t, err, f.NotFound)
	sessionError = fault(f.SessionRevoked)
	_, err = service.UpdateProject(ctx, actor, meta, project.ID, request)
	hasCode(t, err, f.SessionRevoked)
	_, err = service.GetSchedulerConfig(ctx, actor, project.ID)
	hasCode(t, err, f.SessionRevoked)
	if updates != 1 || audits != 1 || events != 1 || activities != 2 {
		t.Fatal("denied owner altered facts")
	}
}
