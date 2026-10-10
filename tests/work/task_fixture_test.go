//go:build integration

package work_test

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/url"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/account"
	"github.com/LunaDeerTech/agenteam/internal/central/audit"
	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/outbox"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/LunaDeerTech/agenteam/internal/central/project"
	c "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/scheduler"
	"github.com/LunaDeerTech/agenteam/internal/central/work"
	wc "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
	pgfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/postgres"
)

type taskFixture struct {
	*fixture
	tasks      *work.TaskService
	taskReader *work.TaskReader
	taskEvents wc.TaskEvents
}

func newTaskFixture(t *testing.T) *taskFixture {
	t.Helper()
	db := newDatabase(t)
	raw := openStore(t, db.Config(t, nil))
	return assembleTask(t, db, raw, raw, true)
}
func assembleTask(t *testing.T, db *pgfixture.Database, raw *postgres.Store, store fixtureStore, createSkillSchema bool) *taskFixture {
	t.Helper()
	ak, ck := keys(t)
	accounts, err := account.NewAuthority(store, ak)
	if err != nil {
		t.Fatal(err)
	}
	if err = accounts.Initialize(ctxFor(t)); err != nil {
		t.Fatal(err)
	}
	pa, err := project.NewAuthority(store, project.AuthorityDependencies{Sessions: accounts, Routes: accounts})
	if err != nil {
		t.Fatal(err)
	}
	wa, err := work.NewAuthority(store, pa)
	if err != nil {
		t.Fatal(err)
	}
	aud, err := audit.New(store, ck, audit.Authorizations{Sessions: accounts, System: accounts, Accounts: accounts, Projects: pa})
	if err != nil {
		t.Fatal(err)
	}
	catalog := event.NewCatalog()
	pt, err := c.RegisterProjectEvents(catalog)
	if err != nil {
		t.Fatal(err)
	}
	wt, err := wc.RegisterWorkEvents(catalog)
	if err != nil {
		t.Fatal(err)
	}
	tt, err := wc.RegisterTaskEvents(catalog)
	if err != nil {
		t.Fatal(err)
	}
	process := fixtureProcess{id[oc.Process](t)}
	events, err := outbox.New(store, catalog, outbox.Authorizations{Producers: map[event.StableName]oc.ProducerAuthority{c.ProjectProducer: pa, wc.WorkProducer: wa}, Projects: pa, Sessions: accounts, System: accounts, Processes: process, Audit: aud, Cursors: ck})
	if err != nil {
		t.Fatal(err)
	}
	if createSkillSchema {
		if _, err = raw.Exec(ctxFor(t), `CREATE SCHEMA work_fixture; CREATE TABLE work_fixture.skills(creation_id uuid PRIMARY KEY,project_id uuid NOT NULL UNIQUE,init_key text NOT NULL,skill_id uuid NOT NULL UNIQUE,revision bigint NOT NULL,protected boolean NOT NULL,published boolean NOT NULL)`); err != nil {
			t.Fatal(err)
		}
	}
	skills := &skillFixture{store: store, authority: pa, issuer: c.NewInitializationPlanIssuer()}
	projects, err := project.New(store, project.Dependencies{Authority: pa, Activity: accounts, Audit: aud, Events: events, ProjectEvents: pt, Initializer: skills, Processes: process, Cursors: ck}, project.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		projects.Stop()
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := projects.Drain(ctx); err != nil {
			t.Error(err)
		}
	})
	f := &fixture{db: db, raw: raw, store: store, accounts: accounts, projectAuthority: pa, projects: projects, authority: wa, skills: skills, events: events, workEvents: wt, keys: ck}
	f.service = f.newService(t, events, accounts)
	f.reader, err = work.NewReader(store, wa, ck)
	if err != nil {
		t.Fatal(err)
	}
	tf := &taskFixture{fixture: f, taskEvents: tt}
	tf.tasks = tf.newTaskService(t, events, accounts)
	tf.taskReader, err = work.NewTaskReader(store, wa, f.reader, ck)
	if err != nil {
		t.Fatal(err)
	}
	return tf
}
func (f *taskFixture) newTaskService(t *testing.T, events oc.Appender, activity work.ActivityAuthority) *work.TaskService {
	t.Helper()
	pending, err := scheduler.NewPendingAuthority(f.store)
	if err != nil {
		t.Fatal("same-Store pending Dispatch authority assembly", err)
	}
	s, err := work.NewTask(f.store, work.TaskDependencies{Authority: f.authority, Structure: f.reader, Events: events, TaskEvents: f.taskEvents, Activity: activity, Pending: pending})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		s.Stop()
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := s.Drain(ctx); err != nil {
			t.Error("Task Drain did not join", err)
		}
	})
	return s
}
func observedTaskFixture(t *testing.T, base *taskFixture) (*taskFixture, *hookStore) {
	t.Helper()
	raw := openStore(t, base.db.Config(t, map[string]string{"LOCK_TIMEOUT": "5s"}))
	store := &hookStore{fixtureStore: raw}
	return assembleTask(t, base.db, raw, store, false), store
}
func proxyTaskFixture(t *testing.T, base *taskFixture, commit bool) (*taskFixture, *hookStore, *commitProxy) {
	t.Helper()
	proxy := newCommitProxy(t, net.JoinHostPort("127.0.0.1", base.db.Fixture.Port), commit)
	u, err := url.Parse(base.db.Fixture.URL(base.db.Name))
	if err != nil {
		t.Fatal(err)
	}
	u.Host = proxy.listener.Addr().String()
	raw := openStore(t, base.db.Config(t, map[string]string{"URL": u.String(), "TLS_MODE": "disable", "LOCK_TIMEOUT": "5s"}))
	store := &hookStore{fixtureStore: raw}
	return assembleTask(t, base.db, raw, store, false), store, proxy
}

// Two one-shot frame proxies independently observe the original writer and its
// subsequent read-only confirmation. Both are the accepted complete-frame
// proxy and both register every listener/handler for actual cleanup join.
func doubleProxyTaskFixture(t *testing.T, base *taskFixture) (*taskFixture, *hookStore, *commitProxy, *commitProxy) {
	t.Helper()
	writer := newCommitProxy(t, net.JoinHostPort("127.0.0.1", base.db.Fixture.Port), true)
	confirmation := newCommitProxy(t, writer.listener.Addr().String(), true)
	t.Log("owned complete-frame proxies", "writer", writer.listener.Addr().String(), "confirmation", confirmation.listener.Addr().String())
	u, err := url.Parse(base.db.Fixture.URL(base.db.Name))
	if err != nil {
		t.Fatal(err)
	}
	u.Host = confirmation.listener.Addr().String()
	raw := openStore(t, base.db.Config(t, map[string]string{"URL": u.String(), "TLS_MODE": "disable", "LOCK_TIMEOUT": "5s"}))
	store := &hookStore{fixtureStore: raw}
	return assembleTask(t, base.db, raw, store, false), store, writer, confirmation
}

func (f *taskFixture) task(t *testing.T, a identity.Actor, p c.ProjectID, s wc.SprintID, title string) wc.Task {
	t.Helper()
	r, err := f.tasks.CreateTask(ctxFor(t), a, meta(t, id[struct{}](t).String(), nil), p, wc.TaskCreate{TaskID: id[wc.Task](t), SprintID: s, Title: title, Type: wc.TaskTypeTask, Priority: wc.TaskPriorityMedium})
	if err != nil || !r.Changed || r.TaskEventID == nil || len(r.EventIDs) != 1 {
		t.Fatal("create Task", err)
	}
	return r.Task
}
func taskIdentity(t *testing.T, p c.ProjectID, name wc.TaskCommandName, key foundation.IdempotencyKey) foundation.CommandIdentity {
	t.Helper()
	v, err := foundation.NewCommandIdentity("project", []string{p.String()}, string(name), key)
	if err != nil {
		t.Fatal(err)
	}
	return v
}
func equalTaskMutation(t *testing.T, a, b wc.TaskMutation) {
	t.Helper()
	if string(jsonBytes(t, a)) != string(jsonBytes(t, b)) {
		t.Fatal("Task receipt changed")
	}
}
func (f *taskFixture) taskSnapshot(t *testing.T, a identity.Actor) string {
	t.Helper()
	var raw string
	err := f.raw.QueryRow(ctxFor(t), `SELECT jsonb_build_object(
 'tasks',(SELECT coalesce(jsonb_agg(to_jsonb(t) ORDER BY id),'[]'::jsonb) FROM agenteam_work.tasks t),
 'groups',(SELECT coalesce(jsonb_agg(to_jsonb(t) ORDER BY project_id,sprint_id,state,priority),'[]'::jsonb) FROM agenteam_work.task_order_groups t),
 'query',(SELECT coalesce(jsonb_agg(to_jsonb(t) ORDER BY project_id),'[]'::jsonb) FROM agenteam_work.task_query_generations t),
 'history',(SELECT coalesce(jsonb_agg(to_jsonb(t) ORDER BY id),'[]'::jsonb) FROM agenteam_work.task_events t),
 'receipts',(SELECT coalesce(jsonb_agg(to_jsonb(t) ORDER BY id),'[]'::jsonb) FROM agenteam_work.task_commands t WHERE state='completed'),
 'events',(SELECT coalesce(jsonb_agg(to_jsonb(t) ORDER BY id),'[]'::jsonb) FROM agenteam_outbox.events t WHERE event_type='work.task_changed'),
 'activity',(SELECT last_activity_at FROM agenteam_account.sessions WHERE id=$1))::text`, a.Details().SessionID).Scan(&raw)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
func (f *taskFixture) taskCounts(t *testing.T) (int, int) {
	t.Helper()
	var h, e int
	if err := f.raw.QueryRow(ctxFor(t), `SELECT (SELECT count(*) FROM agenteam_work.task_events),(SELECT count(*) FROM agenteam_outbox.events WHERE event_type='work.task_changed')`).Scan(&h, &e); err != nil {
		t.Fatal(err)
	}
	return h, e
}
func (f *taskFixture) generation(t *testing.T, p c.ProjectID, s wc.SprintID, priority wc.TaskPriority) (int64, int64) {
	t.Helper()
	var g, q int64
	if err := f.raw.QueryRow(ctxFor(t), `SELECT (SELECT order_generation FROM agenteam_work.task_order_groups WHERE project_id=$1 AND sprint_id=$2 AND state='backlog' AND priority=$3),(SELECT query_generation FROM agenteam_work.task_query_generations WHERE project_id=$1)`, p.String(), s.String(), string(priority)).Scan(&g, &q); err != nil {
		t.Fatal(err)
	}
	return g, q
}

// This seeds canonical archived Project input and a rolled-back malformed
// timestamp control; it does not execute the Project lifecycle protocol.
func taskSeedArchivedProject(t *testing.T, f *taskFixture, a identity.Actor, p c.ProjectID) {
	t.Helper()
	snapshot := func() string {
		t.Helper()
		var out string
		if err := f.raw.QueryRow(ctxFor(t), `SELECT to_jsonb(p)::text FROM agenteam_project.projects p WHERE id=$1`, p.String()).Scan(&out); err != nil {
			t.Fatal("archive fixture Project snapshot", foundation.NewFault(foundation.InternalError, foundation.NotStarted).WithCause(err))
		}
		return out
	}
	before := snapshot()
	result := f.store.WithinTx(ctxFor(t), cause(t), func(ctx context.Context, tx foundation.Tx) error {
		if err := f.store.AcquireAll(ctx, tx, fixtureLocks(a, p)); err != nil {
			return err
		}
		if _, err := f.projectAuthority.RequireOwnerInTx(ctx, tx, a, p, identity.Read); err != nil {
			return err
		}
		x, err := f.store.InTx(tx)
		if err != nil {
			return err
		}
		// Observe the old fixture expression without assuming that two clocks
		// always differ at PostgreSQL's stored microsecond precision.
		if _, err := x.Exec(ctx, `UPDATE agenteam_project.projects SET lifecycle='archived',archived_at=clock_timestamp(),updated_at=clock_timestamp() WHERE id=$1`, p.String()); err != nil {
			return err
		}
		var legacyAfter bool
		if err := x.QueryRow(ctx, `SELECT archived_at>updated_at FROM agenteam_project.projects WHERE id=$1`, p.String()).Scan(&legacyAfter); err != nil {
			return err
		}
		t.Logf("legacy archive seed observed archived_after_updated=%t; prior failed run timestamps were not captured", legacyAfter)
		// A deterministic malformed control proves the exact ProjectRef read
		// failure independently of the old expression's clock coincidence.
		if _, err := x.Exec(ctx, `WITH moment AS MATERIALIZED (SELECT clock_timestamp() AS value) UPDATE agenteam_project.projects SET lifecycle='archived',updated_at=moment.value,archived_at=moment.value+interval '1 microsecond' FROM moment WHERE id=$1`, p.String()); err != nil {
			return err
		}
		var malformed bool
		if err := x.QueryRow(ctx, `SELECT archived_at=updated_at+interval '1 microsecond' FROM agenteam_project.projects WHERE id=$1`, p.String()).Scan(&malformed); err != nil {
			return err
		}
		if !malformed {
			return errors.New("archive fixture malformed timestamp control missing")
		}
		_, err = f.projectAuthority.RequireOwnerInTx(ctx, tx, a, p, identity.Read)
		if err == nil {
			return errors.New("archive fixture malformed timestamp was accepted")
		}
		return err
	})
	if result.State() != foundation.NotCommitted {
		t.Fatal("malformed archive fixture did not roll back", result.State(), result.Fault())
	}
	requireCode(t, result.Fault(), foundation.DependencyUnavailable)
	if snapshot() != before {
		t.Fatal("malformed archive fixture changed persisted Project")
	}
	t.Log("malformed archive timestamp control: archived_after_updated=true, current Read DEPENDENCY_UNAVAILABLE, transaction not_committed, Project unchanged")
	f.tx(t, fixtureLocks(a, p), func(ctx context.Context, tx foundation.Tx, x postgres.SQLExecutor) error {
		if _, err := f.projectAuthority.RequireOwnerInTx(ctx, tx, a, p, identity.Read); err != nil {
			return err
		}
		if _, err := x.Exec(ctx, `WITH moment AS MATERIALIZED (SELECT clock_timestamp() AS value) UPDATE agenteam_project.projects SET lifecycle='archived',archived_at=moment.value,updated_at=moment.value FROM moment WHERE id=$1`, p.String()); err != nil {
			return err
		}
		access, err := f.projectAuthority.RequireOwnerInTx(ctx, tx, a, p, identity.Read)
		if err != nil {
			return err
		}
		ref := access.Project()
		if ref.Validate() != nil || ref.Lifecycle != c.Archived || ref.ArchivedAt == nil || !ref.ArchivedAt.Time().Equal(ref.UpdatedAt.Time()) {
			return errors.New("single-moment archive fixture is not canonical")
		}
		return nil
	})
	t.Log("test-only single-moment archived Project input verified by current Read; no lifecycle participant run")
}

// These are canonical future facts, not an implemented Agent or state transition.
func (f *taskFixture) seedTaskState(t *testing.T, a identity.Actor, task wc.Task, state wc.TaskState, agent *identity.AgentID) {
	t.Helper()
	schedule, _ := foundation.ProjectScheduleLock(task.ProjectID.String())
	target, _ := foundation.AggregateLock(foundation.TaskAggregate, task.ID.String())
	f.tx(t, fixtureLocks(a, task.ProjectID, foundation.LockRequest{Key: schedule, Mode: foundation.Exclusive}, foundation.LockRequest{Key: target, Mode: foundation.Exclusive}), func(ctx context.Context, tx foundation.Tx, x postgres.SQLExecutor) error {
		if _, err := f.projectAuthority.RequireOwnerInTx(ctx, tx, a, task.ProjectID, identity.Mutate); err != nil {
			return err
		}
		var assignee any
		if agent != nil {
			assignee = agent.String()
		}
		if _, err := x.Exec(ctx, `UPDATE agenteam_work.tasks SET state=$2,assignee_agent_id=$3 WHERE id=$1`, task.ID.String(), string(state), assignee); err != nil {
			return err
		}
		if _, err := x.Exec(ctx, `INSERT INTO agenteam_work.task_order_groups(project_id,milestone_id,sprint_id,state,priority,order_generation) VALUES($1,$2,$3,$4,$5,1) ON CONFLICT DO NOTHING`, task.ProjectID.String(), task.MilestoneID.String(), task.SprintID.String(), string(state), string(task.Priority)); err != nil {
			return err
		}
		_, err := x.Exec(ctx, `UPDATE agenteam_work.task_query_generations SET query_generation=query_generation+1 WHERE project_id=$1`, task.ProjectID.String())
		return err
	})
	t.Log("test-only future canonical Task state/assignee; no Agent binding or transition executed")
}
func taskPayload(t *testing.T, f *taskFixture, id wc.TaskEventID) map[string]json.RawMessage {
	t.Helper()
	var raw []byte
	if err := f.raw.QueryRow(ctxFor(t), `SELECT payload FROM agenteam_work.task_events WHERE id=$1`, id.String()).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var out map[string]json.RawMessage
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	return out
}
