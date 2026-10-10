//go:build integration

package projectvariable_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/execution"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/outbox"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/scheduler"
	"github.com/LunaDeerTech/agenteam/internal/central/work"
	wc "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
)

// This first chain stops at a Human-assigned todo Task. There is no public
// StartSprint in this composition, so neither a claim nor a Launch is seeded.
func TestTaskTransitionHuman(t *testing.T) {
	t.Run("assignment-config-and-replay", func(t *testing.T) {
		v := newTaskTransitionFixture(t)
		v.configureProject(t)
		request, commandMeta, lookup := v.transferRequest(t)
		receipt, err := v.transitions.TransferTask(ctxFor(t), v.base.ownerBrowser.actor, commandMeta, v.base.project.ID, v.task.ID, request)
		if err != nil {
			t.Fatal("current Owner transfer", err)
		}
		stored, err := v.committedFacts(ctxFor(t), v.base.raw, commandMeta.IdempotencyKey)
		if err != nil || !bytes.Equal(jsonBytes(t, receipt), jsonBytes(t, stored)) {
			t.Fatal("original receipt differs from its atomic domain facts", err)
		}
		current, err := v.taskReader.GetTask(ctxFor(t), v.base.ownerBrowser.actor, v.base.project.ID, v.task.ID)
		if err != nil || !bytes.Equal(jsonBytes(t, current), jsonBytes(t, receipt.Task)) {
			t.Fatal("current Task differs from committed transition", err)
		}
		beforeReplay := v.databaseSnapshot(t)
		replay, err := v.transitions.TransferTask(ctxFor(t), v.base.ownerBrowser.actor, commandMeta, v.base.project.ID, v.task.ID, request)
		if err != nil || !bytes.Equal(jsonBytes(t, replay), jsonBytes(t, receipt)) {
			t.Fatal("same-key transition replay changed its complete original receipt", err)
		}
		found, err := v.transitions.LookupTaskTransition(ctxFor(t), v.base.ownerBrowser.actor, lookup)
		if err != nil || found.Status != wc.LookupCommitted || found.Receipt == nil || !bytes.Equal(jsonBytes(t, *found.Receipt), jsonBytes(t, receipt)) {
			t.Fatal("original transition lookup lost its committed receipt", err)
		}
		if afterReplay := v.databaseSnapshot(t); beforeReplay != afterReplay {
			t.Fatal("completed replay rewrote Task, order, history, Outbox or activity")
		}
	})
	t.Run("final-transaction-rollback", func(t *testing.T) {
		v := newTaskTransitionFixture(t)
		request, commandMeta, lookup := v.transferRequest(t)
		command, err := wc.TaskTransitionIdentity(v.base.project.ID, commandMeta.IdempotencyKey)
		if err != nil {
			t.Fatal(err)
		}
		before := v.databaseSnapshot(t)
		marker := errors.New("task-transition-final-callback-rollback")
		var observed, returned bool
		var physical f.CommitResult
		store := v.base.tracked
		store.setAfter(func(ctx context.Context, tx f.Tx, cause f.TransactionCause) error {
			d := cause.Details()
			if d.Kind != f.CommandsCause || d.Primary.Canonical() != command.Canonical() {
				return nil
			}
			x, err := store.InTx(tx)
			if err != nil {
				return err
			}
			var completed bool
			if err = x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_work.task_transition_commands WHERE project_id=$1 AND idempotency_key=$2 AND state='completed')`, v.base.project.ID.String(), string(commandMeta.IdempotencyKey)).Scan(&completed); err != nil {
				return err
			}
			if !completed {
				return nil // Original discovery and planning may commit normally.
			}
			if _, err = v.committedFacts(ctx, x, commandMeta.IdempotencyKey); err != nil {
				return err
			}
			observed = true
			return marker // Original Store executes ROLLBACK; no result replacement.
		})
		store.mu.Lock()
		store.afterResult = func(cause f.TransactionCause, result f.CommitResult) {
			d := cause.Details()
			if observed && d.Kind == f.CommandsCause && d.Primary.Canonical() == command.Canonical() {
				physical, returned = result, true
			}
		}
		store.mu.Unlock()
		clear := func() { store.setAfter(nil); store.mu.Lock(); store.afterResult = nil; store.mu.Unlock() }
		defer clear()
		receipt, err := v.transitions.TransferTask(ctxFor(t), v.base.ownerBrowser.actor, commandMeta, v.base.project.ID, v.task.ID, request)
		clear()
		if !observed || !returned || physical.State() != f.NotCommitted || !errors.Is(physical.Fault(), marker) || !errors.Is(err, marker) || receipt.Validate() == nil {
			t.Fatal("late rollback lost the original physical result or returned a receipt", err)
		}
		if after := v.databaseSnapshot(t); after != before {
			t.Fatal("rolled-back transfer changed Task, order, history, Outbox or activity")
		}
		var planned, completed int64
		if err := v.base.raw.QueryRow(ctxFor(t), `SELECT count(*) FILTER(WHERE state='planned' AND receipt IS NULL),count(*) FILTER(WHERE state='completed') FROM agenteam_work.task_transition_commands WHERE project_id=$1 AND idempotency_key=$2`, v.base.project.ID.String(), string(commandMeta.IdempotencyKey)).Scan(&planned, &completed); err != nil || planned != 1 || completed != 0 {
			t.Fatal("rollback did not preserve only the original plan", err)
		}
		found, err := v.transitions.LookupTaskTransition(ctxFor(t), v.base.ownerBrowser.actor, lookup)
		if err != nil || found.Status != wc.LookupInProgress || found.Receipt != nil {
			t.Fatal("rollback lookup fabricated a completed transition", err)
		}
	})
}

// This preparation uses the actual P2/InstallSource/Agent graph. The Work
// services share its original Store and current Owner. No Task state, Agent
// assignment, Project setting or Dispatch is seeded through SQL.
type taskTransitionFixture struct {
	agent            *agentCreateFixture
	base             *variableHTTPFixture
	agentID          i.AgentID
	authority        *work.Authority
	structure        *work.Service
	structureReader  *work.Reader
	tasks            *work.TaskService
	taskReader       *work.TaskReader
	pending          *scheduler.PendingAuthority
	box              oc.Appender
	transitionEvents wc.TaskTransitionEvents
	transitions      *work.TaskTransitionService
	task             wc.Task
}

func newTaskTransitionFixture(t *testing.T) *taskTransitionFixture {
	t.Helper()
	a := newAgentCreateFixture(t)
	request, commandMeta, _ := a.request(t)
	receipt, err := a.agents.CreateAgent(ctxFor(t), a.p2.base.ownerBrowser.actor, commandMeta, a.p2.project.ID, request)
	if err != nil || receipt.Validate() != nil {
		t.Fatal("real default-enabled Agent creation", err)
	}
	agentID := request.Fields().AgentID
	if receipt.Fields().Agent.Fields().Core.ID != agentID {
		t.Fatal("created Agent identity differs from the original request")
	}
	// This is only a fixture projection onto the actual P2 Project. The Store,
	// Account session and domain authorities retain their original identity.
	base := *a.p2.base
	base.project = a.p2.project
	base.projectAuthority = a.providers.Projects
	v := &taskTransitionFixture{agent: a, base: &base, agentID: agentID}
	v.authority, err = work.NewAuthority(base.tracked, base.projectAuthority)
	if err != nil {
		t.Fatal("same-Store Work authority", err)
	}
	catalog := event.NewCatalog()
	workEvents, err := wc.RegisterWorkEvents(catalog)
	if err != nil {
		t.Fatal("formal Work events", err)
	}
	taskEvents, err := wc.RegisterTaskEvents(catalog)
	if err != nil {
		t.Fatal("formal Task events", err)
	}
	v.transitionEvents, err = wc.RegisterTaskTransitionEvents(catalog)
	if err != nil {
		t.Fatal("formal Task transition events", err)
	}
	v.box, err = outbox.New(base.tracked, catalog, outbox.Authorizations{
		Producers: map[event.StableName]oc.ProducerAuthority{wc.WorkProducer: v.authority},
		Sessions:  base.accounts, System: base.accounts, Projects: base.projectAuthority,
		Audit: base.audit, Cursors: base.keys, Processes: fixtureProcess{id[oc.Process](t)},
	})
	if err != nil {
		t.Fatal("same-Store formal Work producer", err)
	}
	v.structure, err = work.New(base.tracked, work.Dependencies{Authority: v.authority, Events: v.box, WorkEvents: workEvents, Activity: base.accounts})
	if err != nil {
		t.Fatal("formal Work service", err)
	}
	t.Cleanup(func() {
		v.structure.Stop()
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := v.structure.Drain(ctx); err != nil {
			t.Error("original Work calls did not join", err)
		}
	})
	v.structureReader, err = work.NewReader(base.tracked, v.authority, base.keys)
	if err != nil {
		t.Fatal("same-Store Work reader", err)
	}
	v.pending, err = scheduler.NewPendingAuthority(base.tracked)
	if err != nil {
		t.Fatal("same-Store pending Dispatch authority", err)
	}
	v.tasks, err = work.NewTask(base.tracked, work.TaskDependencies{Structure: v.structureReader, Authority: v.authority, Events: v.box, TaskEvents: taskEvents, Activity: base.accounts, Pending: v.pending})
	if err != nil {
		t.Fatal("formal Task service", err)
	}
	t.Cleanup(func() {
		v.tasks.Stop()
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := v.tasks.Drain(ctx); err != nil {
			t.Error("original Task calls did not join", err)
		}
	})
	v.taskReader, err = work.NewTaskReader(base.tracked, v.authority, v.structureReader, base.keys)
	if err != nil {
		t.Fatal("same-Store Task reader", err)
	}
	milestone := wc.CreateMilestoneRequest{MilestoneID: id[wc.Milestone](t), Title: "Task transition source", Description: "Created by the current Owner"}
	if _, err := v.structure.CreateMilestone(ctxFor(t), base.ownerBrowser.actor, meta(t, "transition-milestone", nil), base.project.ID, milestone); err != nil {
		t.Fatal("formal milestone creation", err)
	}
	sprint := wc.CreateSprintRequest{SprintID: id[pc.Sprint](t), MilestoneID: milestone.MilestoneID, Title: "Task transition sprint", Description: "No scheduler state is seeded"}
	if _, err := v.structure.CreateSprint(ctxFor(t), base.ownerBrowser.actor, meta(t, "transition-sprint", nil), base.project.ID, sprint); err != nil {
		t.Fatal("formal sprint creation", err)
	}
	created, err := v.tasks.CreateTask(ctxFor(t), base.ownerBrowser.actor, meta(t, "transition-task", nil), base.project.ID, wc.TaskCreate{
		TaskID: id[wc.Task](t), SprintID: sprint.SprintID, Title: "Assign the real Agent", Description: "A current Owner changes the backlog task",
		Type: wc.TaskTypeTask, Priority: wc.TaskPriorityMedium, Plan: "Exercise the committed transition without launching an Execution",
	})
	if err != nil || !created.Changed || created.TaskEventID == nil || len(created.EventIDs) != 1 || created.Task.State != wc.TaskStateBacklog || created.Task.AssigneeAgentID != nil {
		t.Fatal("formal Task creation did not produce the unassigned backlog fact", err)
	}
	v.task = created.Task
	occupancy, err := execution.NewWorkOccupancy(base.tracked)
	if err != nil {
		t.Fatal("same-Store real Execution occupancy", err)
	}
	v.transitions, err = work.NewTaskTransition(base.tracked, work.TaskTransitionDependencies{
		Structure: v.structureReader, TaskEvents: v.transitionEvents, Authority: v.authority,
		Events: v.box, Activity: base.accounts, Agents: a.providers.Agents,
		Occupancy: occupancy, Pending: v.pending,
	})
	if err != nil {
		t.Fatal("same-Store actual Task transition composition", err)
	}
	t.Cleanup(func() {
		v.transitions.Stop()
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := v.transitions.Drain(ctx); err != nil {
			t.Error("original Task transition calls did not join", err)
		}
	})
	return v
}

func (v *taskTransitionFixture) transferRequest(t *testing.T) (wc.TaskTransfer, f.CommandMeta, wc.TaskTransitionLookupRequest) {
	t.Helper()
	request := wc.TaskTransfer{TargetState: wc.TaskStateTodo, AssigneeAgentID: &v.agentID, AddBlockers: []wc.TaskBlockerCreate{}, ResolveBlockerIDs: []wc.TaskBlockerID{}}
	commandMeta := meta(t, "human-assign-todo", &v.task.Version)
	digest, err := wc.TaskTransferDigest(v.base.ownerBrowser.actor, commandMeta, v.base.project.ID, v.task.ID, request)
	if err != nil {
		t.Fatal(err)
	}
	return request, commandMeta, wc.TaskTransitionLookupRequest{ProjectID: v.base.project.ID, Command: wc.TaskTransitionTransfer, IdempotencyKey: commandMeta.IdempotencyKey, SemanticDigest: digest}
}

func (v *taskTransitionFixture) committedFacts(ctx context.Context, x postgres.SQLExecutor, key f.IdempotencyKey) (wc.TaskTransitionMutation, error) {
	var operation string
	var raw []byte
	if err := x.QueryRow(ctx, `SELECT id::text,receipt FROM agenteam_work.task_transition_commands WHERE project_id=$1 AND task_id=$2 AND idempotency_key=$3 AND state='completed'`, v.base.project.ID.String(), v.task.ID.String(), string(key)).Scan(&operation, &raw); err != nil {
		return wc.TaskTransitionMutation{}, err
	}
	r, err := wc.DecodeTaskTransitionMutation(raw)
	if err != nil || r.Task.ID != v.task.ID || r.Task.ProjectID != v.base.project.ID || r.Task.Version != v.task.Version+1 || r.Task.State != wc.TaskStateTodo || r.Task.AssigneeAgentID == nil || *r.Task.AssigneeAgentID != v.agentID || len(r.TaskEventIDs) != 2 || len(r.EventIDs) != 1 {
		return wc.TaskTransitionMutation{}, errors.New("completed transition did not contain the exact assigned todo receipt")
	}
	var facts [6]int64
	err = x.QueryRow(ctx, `SELECT
 (SELECT count(*) FROM agenteam_work.task_events WHERE project_id=$1::text AND task_id=$2::text AND transition_operation_id=$3::text),
 (SELECT count(*) FROM agenteam_work.task_events WHERE project_id=$1::text AND id=$4::text AND transition_operation_id=$3::text AND correlation_id=$3::text AND task_version=$7 AND type='state_changed' AND payload->>'from_state'='backlog' AND payload->>'to_state'='todo' AND actor->>'user_id'=$8::text),
 (SELECT count(*) FROM agenteam_work.task_events WHERE project_id=$1::text AND id=$5::text AND transition_operation_id=$3::text AND correlation_id=$3::text AND task_version=$7 AND type='assignee_changed' AND payload->'from_agent_id'='null'::jsonb AND payload->>'to_agent_id'=$9::text AND actor->>'user_id'=$8::text),
 (SELECT count(*) FROM agenteam_outbox.events WHERE id=$6::text AND project_id=$1::text AND aggregate_id=$2::text AND aggregate_version=$7 AND producer='work' AND event_type='work.task_transitioned' AND schema_version=1 AND convert_from(payload,'UTF8')::jsonb->>'command_id'=$3::text),
 (SELECT count(*) FROM agenteam_scheduler.dispatches WHERE project_id=$1::text),
 (SELECT count(*) FROM agenteam_execution.executions WHERE project_id=$1::text)`,
		v.base.project.ID.String(), v.task.ID.String(), operation, r.TaskEventIDs[0].String(), r.TaskEventIDs[1].String(), r.EventIDs[0].String(), int64(r.Task.Version), v.base.ownerBrowser.actor.Details().UserID, v.agentID.String()).Scan(&facts[0], &facts[1], &facts[2], &facts[3], &facts[4], &facts[5])
	if err != nil {
		return wc.TaskTransitionMutation{}, err
	}
	if facts != ([6]int64{2, 1, 1, 1, 0, 0}) {
		return wc.TaskTransitionMutation{}, fmt.Errorf("atomic Task transition fact counts %v", facts)
	}
	return r, nil
}

func (v *taskTransitionFixture) databaseSnapshot(t *testing.T) string {
	t.Helper()
	var snapshot string
	err := v.base.raw.QueryRow(ctxFor(t), `SELECT jsonb_build_object(
 'task',(SELECT to_jsonb(t) FROM agenteam_work.tasks t WHERE project_id=$1::text AND id=$2::text),
 'history',(SELECT coalesce(jsonb_agg(to_jsonb(e) ORDER BY id),'[]'::jsonb) FROM agenteam_work.task_events e WHERE project_id=$1::text AND task_id=$2::text),
 'groups',(SELECT coalesce(jsonb_agg(to_jsonb(g) ORDER BY sprint_id,state,priority),'[]'::jsonb) FROM agenteam_work.task_order_groups g WHERE project_id=$1::text),
 'generation',(SELECT query_generation FROM agenteam_work.task_query_generations WHERE project_id=$1::text),
 'outbox',(SELECT coalesce(jsonb_agg(to_jsonb(e) ORDER BY id),'[]'::jsonb) FROM agenteam_outbox.events e WHERE project_id=$1::text AND aggregate_id=$2::text),
 'activity',(SELECT last_activity_at FROM agenteam_account.sessions WHERE id=$3::uuid)
 )::text`, v.base.project.ID.String(), v.task.ID.String(), v.base.ownerBrowser.actor.Details().SessionID).Scan(&snapshot)
	if err != nil {
		t.Fatal("original Task transaction snapshot", err)
	}
	return snapshot
}

func (v *taskTransitionFixture) configureProject(t *testing.T) {
	t.Helper()
	ctx := ctxFor(t)
	actor, projectID := v.base.ownerBrowser.actor, v.base.project.ID
	before, err := v.base.projects.GetSchedulerConfig(ctx, actor, projectID)
	if err != nil || before.Project.ID != projectID || before.Config.Enabled || before.Config.MaxConcurrency != nil {
		t.Fatal("new real Project did not retain the disabled unlimited default", err)
	}
	limit := int64(2)
	request := pc.UpdateProjectRequest{Scheduler: &pc.ProjectSchedulerConfig{Enabled: true, MaxConcurrency: &limit}}
	commandMeta := meta(t, "transition-project-scheduler", &before.Project.Version)
	updated, err := v.base.projects.UpdateProject(ctx, actor, commandMeta, projectID, request)
	if err != nil || updated.ID != projectID || updated.Version != before.Project.Version+1 {
		t.Fatal("current Owner did not commit the actual Project scheduler configuration", err)
	}
	current, err := v.base.projects.GetSchedulerConfig(ctx, actor, projectID)
	if err != nil || !bytes.Equal(jsonBytes(t, current.Project), jsonBytes(t, updated)) || !current.Config.Enabled || current.Config.MaxConcurrency == nil || *current.Config.MaxConcurrency != limit || current.Project.CurrentSprintID != nil {
		t.Fatal("current Project configuration differs from the committed replacement", err)
	}
	replay, err := v.base.projects.UpdateProject(ctx, actor, commandMeta, projectID, request)
	if err != nil || !bytes.Equal(jsonBytes(t, replay), jsonBytes(t, updated)) {
		t.Fatal("same-key Project configuration replay changed the original receipt", err)
	}
	after, err := v.base.projects.GetSchedulerConfig(ctx, actor, projectID)
	if err != nil || after.Project.Version != current.Project.Version || !after.Config.Enabled || after.Config.MaxConcurrency == nil || *after.Config.MaxConcurrency != limit || after.Project.CurrentSprintID != nil {
		t.Fatal("configuration replay rewrote state or activated the planned Sprint", err)
	}
	v.base.project = current.Project
}
