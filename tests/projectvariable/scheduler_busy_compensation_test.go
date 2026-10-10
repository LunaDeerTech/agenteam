//go:build integration

package projectvariable_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/agent"
	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/execution"
	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/outbox"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/LunaDeerTech/agenteam/internal/central/project"
	"github.com/LunaDeerTech/agenteam/internal/central/scheduler"
	"github.com/LunaDeerTech/agenteam/internal/central/work"
	wc "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
)

// Both Dispatches are claimed while the Agent is idle. Only the competitor is
// launched first; the target's AgentBusy is returned by the real Execution
// service. No Task, slot, busy result or compensation is seeded through SQL.
func TestSchedulerBusyCompensation(t *testing.T) {
	t.Run("rollback-restore-and-replay", func(t *testing.T) {
		x := newSchedulerBusyFixture(t)
		before := x.snapshot(t)
		command, err := f.NewCommandIdentity("scheduler", []string{x.v.base.project.ID.String()}, "compensate_agent_busy", x.request.Meta.IdempotencyKey)
		if err != nil {
			t.Fatal("original compensation command", err)
		}
		marker := errors.New("scheduler-busy-final-callback-rollback")
		var observed, returned bool
		var physical f.CommitResult
		store := x.v.base.tracked
		store.setAfter(func(ctx context.Context, tx f.Tx, cause f.TransactionCause) error {
			if cause.Kind() != f.CommandsCause || cause.Details().Primary.Canonical() != command.Canonical() {
				return nil
			}
			sql, err := store.InTx(tx)
			if err != nil {
				return err
			}
			if err = x.committedFacts(ctx, sql, true, x.claimed.Version); err != nil {
				return err
			}
			observed = true
			return marker // All domain writes happened; the original Store rolls back.
		})
		store.mu.Lock()
		store.afterResult = func(cause f.TransactionCause, result f.CommitResult) {
			if observed && cause.Kind() == f.CommandsCause && cause.Details().Primary.Canonical() == command.Canonical() {
				physical, returned = result, true
			}
		}
		store.mu.Unlock()
		clear := func() { store.setAfter(nil); store.mu.Lock(); store.afterResult = nil; store.mu.Unlock() }
		defer clear()
		result, err := x.compensator.CompensateAgentBusy(ctxFor(t), x.v.base.project.ID, x.target)
		clear()
		if !observed || !returned || physical.State() != f.NotCommitted || !errors.Is(physical.Fault(), marker) || !errors.Is(err, marker) || result.Summary().Status == scheduler.Skipped {
			t.Fatal("late compensation failure lost the actual rollback or published a result", err)
		}
		if x.snapshot(t) != before {
			t.Fatal("rollback changed Task, Work result, history, Outbox, Dispatch or slot")
		}
		done, err := x.compensator.CompensateAgentBusy(ctxFor(t), x.v.base.project.ID, x.target)
		if err != nil {
			t.Fatal("formal AgentBusy compensation", err)
		}
		x.requireCompleted(t, done, true, x.claimed.Version)
		restored := x.currentTask(t)
		want := x.claimed.Clone()
		want.State, want.Version, want.UpdatedAt = wc.TaskStateTodo, x.claimed.Version+1, restored.UpdatedAt
		want.ManualRank = restored.ManualRank // The writer recomputes rank from logical anchors.
		if !bytes.Equal(jsonBytes(t, restored), jsonBytes(t, want)) {
			t.Fatal("restoration changed fields outside state/version and recomputed placement")
		}
		if !reflect.DeepEqual(x.todoIDs(t), x.originalTodo) {
			t.Fatal("compensation did not restore the original logical todo position")
		}
		stable := x.snapshot(t)
		replay, err := x.compensator.CompensateAgentBusy(ctxFor(t), x.v.base.project.ID, x.target)
		if err != nil || !reflect.DeepEqual(replay.Summary(), done.Summary()) {
			t.Fatal("same compensation replay changed its original result", err)
		}
		found, err := x.compensator.Lookup(ctxFor(t), x.v.base.project.ID, x.target)
		if err != nil || !reflect.DeepEqual(found.Summary(), done.Summary()) || stable != x.snapshot(t) {
			t.Fatal("lookup or replay rewrote already committed compensation facts", err)
		}
		x.requireNoFurtherLaunch(t)
	})
	t.Run("preserve-user-update", func(t *testing.T) {
		x := newSchedulerBusyFixture(t)
		title := "Current Owner retained this title"
		updated, err := x.v.tasks.UpdateTask(ctxFor(t), x.v.base.ownerBrowser.actor,
			meta(t, "busy-owner-title", &x.claimed.Version), x.v.base.project.ID, x.claimed.ID, wc.TaskFieldsUpdate{Title: &title})
		if err != nil || updated.Validate() != nil || !updated.Changed || updated.Task.Version != x.claimed.Version+1 || updated.Task.Title != title || updated.Task.State != wc.TaskStateInProgress {
			t.Fatal("formal Owner title update did not produce the current Task", err)
		}
		// This snapshot includes the complete Task row (including current rank),
		// its history, order groups, query generation, Outbox and Human activity.
		before := x.v.databaseSnapshot(t)
		done, err := x.compensator.CompensateAgentBusy(ctxFor(t), x.v.base.project.ID, x.target)
		if err != nil {
			t.Fatal("formal compensation must preserve the newer user version", err)
		}
		x.requireCompleted(t, done, false, updated.Task.Version)
		if before != x.v.databaseSnapshot(t) || !bytes.Equal(jsonBytes(t, x.currentTask(t)), jsonBytes(t, updated.Task)) {
			t.Fatal("preserved compensation changed the user Task, rank, history or events")
		}
		if len(x.todoIDs(t)) != 0 {
			t.Fatal("preserved Task was silently restored to todo")
		}
		x.requireNoFurtherLaunch(t)
	})
}

type schedulerBusyFixture struct {
	v            *taskTransitionFixture
	target       scheduler.DispatchID
	competitor   scheduler.DispatchID
	request      ec.LaunchRequest
	busy         scheduler.DispatchSummary
	claimed      wc.Task
	originalTodo []wc.TaskID
	launcher     *countedSchedulerExecution
	compensator  *scheduler.BusyCompensator
}

func newSchedulerBusyFixture(t *testing.T) *schedulerBusyFixture {
	t.Helper()
	v, claims := newSchedulerClaimFixture(t)
	x := &schedulerBusyFixture{v: v}
	x.originalTodo = x.todoIDs(t)
	if len(x.originalTodo) != 1 || x.originalTodo[0] != v.task.ID {
		t.Fatal("the target's original source group is not the expected singleton")
	}
	// All Human Task writes precede both claims. Different source priorities
	// keep each original pending group's position guard independent.
	b, err := v.tasks.CreateTask(ctxFor(t), v.base.ownerBrowser.actor, meta(t, "busy-competitor-create", nil), v.base.project.ID, wc.TaskCreate{
		TaskID: id[wc.Task](t), SprintID: v.task.SprintID, Title: "Occupy the same Agent slot",
		Description: "A second real task competes after both claims", Type: wc.TaskTypeTask,
		Priority: wc.TaskPriorityHigh, Plan: "Create the competing Execution through the original Launch service",
	})
	if err != nil || b.Validate() != nil || b.Task.State != wc.TaskStateBacklog || b.Task.AssigneeAgentID != nil {
		t.Fatal("formal competing Task creation", err)
	}
	assigned, err := v.transitions.TransferTask(ctxFor(t), v.base.ownerBrowser.actor, meta(t, "busy-competitor-assign", &b.Task.Version), v.base.project.ID, b.Task.ID,
		wc.TaskTransfer{TargetState: wc.TaskStateTodo, AssigneeAgentID: &v.agentID, AddBlockers: []wc.TaskBlockerCreate{}, ResolveBlockerIDs: []wc.TaskBlockerID{}})
	if err != nil || assigned.Validate() != nil || assigned.Task.State != wc.TaskStateTodo || assigned.Task.Priority == v.task.Priority || assigned.Task.AssigneeAgentID == nil || *assigned.Task.AssigneeAgentID != v.agentID {
		t.Fatal("formal competing Task assignment", err)
	}
	request, policy := schedulerClaimRequest(t, v)
	a, err := claims.ClaimTask(ctxFor(t), request, policy)
	if err != nil || a.Summary().Status != scheduler.Pending {
		t.Fatal("target's real claim", err)
	}
	x.target = a.Summary().ID
	x.request, err = a.LaunchRequest()
	if err != nil {
		t.Fatal("target's original Launch request", err)
	}
	other := *v
	other.task = assigned.Task
	bRequest, bPolicy := schedulerClaimRequest(t, &other)
	bDispatch, err := claims.ClaimTask(ctxFor(t), bRequest, bPolicy)
	if err != nil || bDispatch.Summary().Status != scheduler.Pending {
		t.Fatal("competitor's real claim before the Agent is occupied", err)
	}
	x.competitor = bDispatch.Summary().ID
	handoff, counter := newBusyLaunchHandoff(t, v)
	x.launcher = counter
	launched, err := handoff.LaunchOnce(ctxFor(t), v.base.project.ID, x.competitor)
	_, _, created := counter.observed()
	if err != nil || launched.Summary().Status != scheduler.Launched || launched.Summary().ExecutionID == nil || *launched.Summary().ExecutionID != created.ID || created.Status != ec.Created {
		t.Fatal("competing Execution did not actually occupy the Agent slot", err)
	}
	busy, err := handoff.LaunchOnce(ctxFor(t), v.base.project.ID, x.target)
	requireCode(t, err, f.AgentBusy)
	x.busy = busy.Summary()
	if x.busy.ID != x.target || x.busy.Status != scheduler.Pending || x.busy.LaunchOutcome != scheduler.KnownNotCreated || x.busy.AttemptCount != 1 || x.busy.ExecutionID != nil {
		t.Fatal("real AgentBusy was not durably retained as known-not-created pending")
	}
	x.claimed = x.currentTask(t)
	if x.claimed.State != wc.TaskStateInProgress || x.claimed.Version != request.ExpectedTaskVersion+1 {
		t.Fatal("busy Launch changed the claimed Task")
	}
	x.requireNoFurtherLaunch(t)
	x.compensator = newBusyCompensator(t, v)
	return x
}

// The same real Launch graph as the preceding slice, with a transparent
// counter only. Execution owns all synchronous provider calls and their tails.
func newBusyLaunchHandoff(t *testing.T, v *taskTransitionFixture) (*scheduler.LaunchHandoff, *countedSchedulerExecution) {
	t.Helper()
	access, err := project.NewSchedulerExecutionAccess(v.base.projectAuthority, v.pending)
	if err != nil {
		t.Fatal(err)
	}
	authority, err := execution.NewAuthority(v.base.tracked, v.base.projectAuthority, access)
	if err != nil {
		t.Fatal(err)
	}
	configuration, err := agent.NewExecutionConfiguration(v.agent.providers.Agents, authority)
	if err != nil {
		t.Fatal(err)
	}
	provider, err := work.NewTaskLaunchProvider(v.base.tracked, v.authority, v.pending)
	if err != nil {
		t.Fatal(err)
	}
	service, err := execution.New(v.base.tracked, execution.Dependencies{Authority: authority, Agents: configuration, Task: provider})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		service.Stop()
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := service.Drain(ctx); err != nil || !service.Joined() {
			t.Error("original Execution calls did not join", err)
		}
	})
	observer, err := execution.NewDispatchObservation(v.base.tracked)
	if err != nil {
		t.Fatal(err)
	}
	counter := &countedSchedulerExecution{service: service}
	handoff, err := scheduler.NewLaunchHandoff(v.pending, scheduler.LaunchHandoffDependencies{Executions: counter, Observations: observer})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		handoff.Stop()
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := handoff.Drain(ctx); err != nil || !handoff.Joined() {
			t.Error("original handoff calls did not join", err)
		}
	})
	return handoff, counter
}

func newBusyCompensator(t *testing.T, v *taskTransitionFixture) *scheduler.BusyCompensator {
	t.Helper()
	catalog := event.NewCatalog()
	events, err := wc.RegisterTaskBusyCompensationEvents(catalog)
	if err != nil {
		t.Fatal(err)
	}
	box, err := outbox.New(v.base.tracked, catalog, outbox.Authorizations{
		Producers: map[event.StableName]oc.ProducerAuthority{wc.WorkProducer: v.authority},
		Sessions:  v.base.accounts, System: v.base.accounts, Projects: v.base.projectAuthority,
		Audit: v.base.audit, Cursors: v.base.keys, Processes: fixtureProcess{id[oc.Process](t)},
	})
	if err != nil {
		t.Fatal(err)
	}
	writer, err := work.NewTaskBusyCompensation(v.base.tracked, work.TaskBusyCompensationDependencies{
		Authority: v.authority, Scheduler: v.pending, Pending: v.pending, Events: box, CompensationEvents: events,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		writer.Stop()
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := writer.Drain(ctx); err != nil || !writer.Joined() {
			t.Error("original Work compensation calls did not join", err)
		}
	})
	service, err := scheduler.NewBusyCompensator(v.pending, scheduler.BusyCompensatorDependencies{Projects: v.base.projectAuthority, Work: writer})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		service.Stop()
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := service.Drain(ctx); err != nil || !service.Joined() {
			t.Error("original Scheduler compensation calls did not join", err)
		}
	})
	return service
}

func (x *schedulerBusyFixture) currentTask(t *testing.T) wc.Task {
	t.Helper()
	task, err := x.v.taskReader.GetTask(ctxFor(t), x.v.base.ownerBrowser.actor, x.v.base.project.ID, x.v.task.ID)
	if err != nil || task.Validate() != nil {
		t.Fatal("current Task read", err)
	}
	return task
}

func (x *schedulerBusyFixture) todoIDs(t *testing.T) []wc.TaskID {
	t.Helper()
	state, priority, sprint := wc.TaskStateTodo, x.v.task.Priority, x.v.task.SprintID
	page, err := x.v.taskReader.ListTasks(ctxFor(t), x.v.base.ownerBrowser.actor, x.v.base.project.ID,
		wc.TaskFilter{State: &state, Priority: &priority, SprintID: &sprint}, f.PageRequest{Limit: 10})
	if err != nil || page.NextCursor != "" {
		t.Fatal("complete bounded source-group observation", err)
	}
	ids := make([]wc.TaskID, 0, len(page.Items))
	for _, task := range page.Items {
		ids = append(ids, task.ID)
	}
	return ids
}

func (x *schedulerBusyFixture) snapshot(t *testing.T) string {
	t.Helper()
	base, err := schedulerClaimSnapshot(ctxFor(t), x.v.base.raw, x.v)
	if err != nil {
		t.Fatal("complete claim and Launch snapshot", err)
	}
	var compensation string
	if err = x.v.base.raw.QueryRow(ctxFor(t), `SELECT coalesce(jsonb_agg(to_jsonb(c) ORDER BY id),'[]'::jsonb)::text FROM agenteam_work.task_busy_compensations c WHERE project_id=$1::text::uuid`, x.v.base.project.ID.String()).Scan(&compensation); err != nil {
		t.Fatal("complete Work compensation snapshot", err)
	}
	return base + "\n" + compensation
}

func (x *schedulerBusyFixture) requireNoFurtherLaunch(t *testing.T) {
	t.Helper()
	launches, _, created := x.launcher.observed()
	if launches != 2 || created.ID.Validate() != nil || created.Status != ec.Created {
		t.Fatal("compensation retried Launch or lost the actual competing Execution")
	}
	var executions, slots, target int64
	err := x.v.base.raw.QueryRow(ctxFor(t), `SELECT
 (SELECT count(*) FROM agenteam_execution.executions WHERE project_id=$1::text),
	 (SELECT count(*) FROM agenteam_execution.executions WHERE agent_id=$2::text AND status IN ('created','preparing','running','waiting')),
 (SELECT count(*) FROM agenteam_execution.executions WHERE project_id=$1::text AND idempotency_key=$3::text)`,
		x.v.base.project.ID.String(), x.v.agentID.String(), x.request.Meta.IdempotencyKey.String()).Scan(&executions, &slots, &target)
	if err != nil || executions != 1 || slots != 1 || target != 0 {
		t.Fatal("busy target created an Execution or disturbed the competitor", err)
	}
}

func (x *schedulerBusyFixture) requireCompleted(t *testing.T, d scheduler.Dispatch, restored bool, before f.Version) {
	t.Helper()
	s := d.Summary()
	if s.ID != x.target || s.Status != scheduler.Skipped || s.LaunchOutcome != scheduler.KnownNotCreated || s.ExecutionID != nil || s.AttemptCount != x.busy.AttemptCount || s.Version != x.busy.Version+1 {
		t.Fatal("compensation did not atomically settle only the original busy Dispatch")
	}
	if err := x.committedFacts(ctxFor(t), x.v.base.raw, restored, before); err != nil {
		t.Fatal("atomic compensation relations", err)
	}
}

// Pure SELECTs verify facts produced by the actual services. UUID Work and
// Outbox columns are distinct from text Scheduler/Execution identity domains.
func (x *schedulerBusyFixture) committedFacts(ctx context.Context, sql postgres.SQLExecutor, restored bool, before f.Version) error {
	after, state, events := before, string(wc.TaskStateInProgress), int64(0)
	if restored {
		after++
		state = string(wc.TaskStateTodo)
		events = 1
	}
	var facts [8]int64
	err := sql.QueryRow(ctx, `SELECT
 (SELECT count(*) FROM agenteam_work.task_busy_compensations c
   JOIN agenteam_work.tasks t ON t.project_id=c.project_id AND t.id=c.task_id
   JOIN agenteam_scheduler.dispatches d ON d.project_id=c.project_id::text AND d.id=c.id::text
  WHERE c.project_id=$1::text::uuid AND c.id=$2::text::uuid AND c.task_id=$3::text::uuid
    AND c.agent_id=$4::text::uuid AND c.request_id=$5::text::uuid AND c.dispatch_version=$6 AND c.launch_attempt=1
    AND c.restored=$7 AND c.before_version=$8 AND c.after_version=$9 AND t.version=$9 AND t.state=$10
    AND t.assignee_agent_id=c.agent_id AND t.sprint_id=$11::text::uuid AND t.priority='medium'
    AND d.status='skipped' AND d.launch_outcome='known_not_created' AND d.busy_attempt=1
    AND d.skip_reason='agent_busy' AND d.skipped_at=d.updated_at AND d.execution_id IS NULL
    AND (($7 AND c.task_event_id IS NOT NULL AND c.event_id IS NOT NULL) OR (NOT $7 AND c.task_event_id IS NULL AND c.event_id IS NULL))),
 (SELECT count(*) FROM agenteam_work.task_events WHERE project_id=$1::text::uuid AND task_id=$3::text::uuid AND compensation_operation_id=$2::text::uuid),
 (SELECT count(*) FROM agenteam_work.task_events h JOIN agenteam_work.task_busy_compensations c ON c.project_id=h.project_id AND c.task_event_id=h.id
  WHERE c.project_id=$1::text::uuid AND c.id=$2::text::uuid AND h.compensation_operation_id=c.id AND h.correlation_id=c.id AND h.task_version=$9
    AND h.type='state_changed' AND h.actor=jsonb_build_object('type','system','service_name','scheduler','source','scheduler','cause_id',$2::text)
    AND h.payload=jsonb_build_object('from_state','in_progress','to_state','todo','reason_code','scheduler_agent_busy_compensation')),
 (SELECT count(*) FROM agenteam_outbox.events e JOIN agenteam_work.task_busy_compensations c ON e.id=c.event_id
  WHERE c.project_id=$1::text::uuid AND c.id=$2::text::uuid AND e.project_id=c.project_id AND e.aggregate_id=c.task_id AND e.aggregate_version=$9
    AND e.producer='work' AND e.event_type='work.task_transitioned' AND e.schema_version=3
    AND convert_from(e.payload,'UTF8')::jsonb->>'claim_id'=$2::text AND convert_from(e.payload,'UTF8')::jsonb->>'task_event_id'=c.task_event_id::text
    AND convert_from(e.payload,'UTF8')::jsonb->>'agent_id'=$4::text AND convert_from(e.payload,'UTF8')::jsonb->>'sprint_id'=$11::text),
 (SELECT count(*) FROM agenteam_work.task_busy_compensations WHERE project_id=$1::text::uuid),
 (SELECT count(*) FROM agenteam_scheduler.dispatches WHERE project_id=$1::text AND id=$12::text AND status='launched' AND execution_id=$13::text),
 (SELECT count(*) FROM agenteam_execution.executions WHERE project_id=$1::text AND id=$13::text AND agent_id=$4::text AND status='created'),
 (SELECT count(*) FROM agenteam_scheduler.dispatches WHERE project_id=$1::text AND status='pending')`,
		x.v.base.project.ID.String(), x.target.String(), x.v.task.ID.String(), x.v.agentID.String(), x.request.Meta.RequestID.String(), int64(x.busy.Version), restored, int64(before), int64(after), state, x.v.task.SprintID.String(), x.competitor.String(), x.createdID()).Scan(&facts[0], &facts[1], &facts[2], &facts[3], &facts[4], &facts[5], &facts[6], &facts[7])
	if err != nil {
		return err
	}
	if facts != ([8]int64{1, events, events, events, 1, 1, 1, 0}) {
		return fmt.Errorf("atomic compensation counts %v", facts)
	}
	if restored {
		var payload, rawGuard []byte
		var sourceGeneration, targetGeneration int64
		err = sql.QueryRow(ctx, `SELECT e.payload,d.claim_guard,
 (SELECT order_generation FROM agenteam_work.task_order_groups WHERE project_id=$1::text::uuid AND sprint_id=$3::text::uuid AND state='in_progress' AND priority='medium'),
 (SELECT order_generation FROM agenteam_work.task_order_groups WHERE project_id=$1::text::uuid AND sprint_id=$3::text::uuid AND state='todo' AND priority='medium')
 FROM agenteam_work.task_busy_compensations c JOIN agenteam_outbox.events e ON e.id=c.event_id
 JOIN agenteam_scheduler.dispatches d ON d.project_id=c.project_id::text AND d.id=c.id::text
 WHERE c.project_id=$1::text::uuid AND c.id=$2::text::uuid`, x.v.base.project.ID.String(), x.target.String(), x.v.task.SprintID.String()).Scan(&payload, &rawGuard, &sourceGeneration, &targetGeneration)
		if err != nil {
			return err
		}
		var event wc.TaskBusyCompensated
		var guard scheduler.ClaimGuard
		if json.Unmarshal(payload, &event) != nil || event.Validate() != nil || json.Unmarshal(rawGuard, &guard) != nil {
			return errors.New("compensation did not persist the strict typed event and original logical guard")
		}
		if event.ClaimID.String() != x.target.String() || event.Actor.CauseID != x.target.String() || event.MilestoneID != x.claimed.MilestoneID || event.SprintID != x.claimed.SprintID || event.AgentID != x.v.agentID ||
			event.SourcePosition.Priority != wc.TaskPriorityMedium || event.TargetPosition.Priority != wc.TaskPriorityMedium || event.SourcePosition.PreviousID != nil || event.SourcePosition.NextID != nil || event.TargetPosition.PreviousID != nil || event.TargetPosition.NextID != nil ||
			event.SourcePosition.OrderGeneration != sourceGeneration || event.TargetPosition.OrderGeneration != targetGeneration || targetGeneration != int64(guard.SourceOrderGeneration)+1 || guard.PredecessorID != "" || guard.SuccessorID != "" {
			return errors.New("typed compensation event lost the source guard or new logical group positions")
		}
	}
	return nil
}

func (x *schedulerBusyFixture) createdID() string {
	_, _, created := x.launcher.observed()
	return created.ID.String()
}
