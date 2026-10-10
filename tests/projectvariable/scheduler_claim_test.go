//go:build integration

package projectvariable_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/agent"
	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/execution"
	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/outbox"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/LunaDeerTech/agenteam/internal/central/project"
	"github.com/LunaDeerTech/agenteam/internal/central/scheduler"
	"github.com/LunaDeerTech/agenteam/internal/central/work"
	wc "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
)

// This slice establishes a durable claim, not a Launch or a running Scheduler.
// Every Project/Sprint/Task/Agent fact is produced by the real public services.
func TestSchedulerClaim(t *testing.T) {
	t.Run("start-sprint-claim-and-replay", func(t *testing.T) {
		v, coordinator := newSchedulerClaimFixture(t)
		runSchedulerClaimCommit(t, v, coordinator)
	})
	t.Run("final-transaction-rollback", func(t *testing.T) {
		v, coordinator := newSchedulerClaimFixture(t)
		runSchedulerClaimRollback(t, v, coordinator)
	})
}

func newSchedulerClaimFixture(t *testing.T) (*taskTransitionFixture, *scheduler.Coordinator) {
	t.Helper()
	v := prepareSchedulerClaimTask(t)
	startSchedulerSprint(t, v)
	return v, newSchedulerClaimCoordinator(t, v, nil)
}

// An optional immutable policy selects the explicit opt-in constructor. The
// existing Claim fixture retains its unbound default and original lifecycle.
func newSchedulerClaimCoordinator(t *testing.T, v *taskTransitionFixture, policy *scheduler.LaunchRetryPolicy) *scheduler.Coordinator {
	t.Helper()
	catalog := event.NewCatalog()
	events, err := wc.RegisterSchedulerClaimEvents(catalog)
	if err != nil {
		t.Fatal("formal Scheduler claim events", err)
	}
	box, err := outbox.New(v.base.tracked, catalog, outbox.Authorizations{
		Producers: map[event.StableName]oc.ProducerAuthority{wc.WorkProducer: v.authority},
		Sessions:  v.base.accounts, System: v.base.accounts, Projects: v.base.projectAuthority,
		Audit: v.base.audit, Cursors: v.base.keys, Processes: fixtureProcess{id[oc.Process](t)},
	})
	if err != nil {
		t.Fatal("real Work claim Outbox", err)
	}
	currentAgent, err := agent.NewSchedulerCurrent(v.agent.providers.Agents, v.pending)
	if err != nil {
		t.Fatal("real Scheduler current Agent authority", err)
	}
	occupancy, err := execution.NewWorkOccupancy(v.base.tracked)
	if err != nil {
		t.Fatal("same-Store actual Execution occupancy", err)
	}
	claims, err := work.NewSchedulerClaim(v.base.tracked, work.SchedulerClaimDependencies{
		Authority: v.authority, Scheduler: v.pending, Agents: currentAgent,
		Pending: v.pending, Occupancy: occupancy, Events: box, ClaimEvents: events,
	})
	if err != nil {
		t.Fatal("real Work Scheduler claim service", err)
	}
	t.Cleanup(func() {
		claims.Stop()
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := claims.Drain(ctx); err != nil {
			t.Error("original Work claim calls did not join", err)
		}
	})
	observer, err := execution.NewDispatchObservation(v.base.tracked)
	if err != nil {
		t.Fatal("same-Store Dispatch observer", err)
	}
	deps := scheduler.CoordinatorDependencies{
		Projects: v.base.projectAuthority, Claims: claims, Executions: observer, Capacity: observer,
	}
	var coordinator *scheduler.Coordinator
	if policy == nil {
		coordinator, err = scheduler.NewCoordinator(v.pending, deps)
	} else {
		coordinator, err = scheduler.NewCoordinatorWithRetryPolicy(v.pending, deps, *policy)
	}
	if err != nil {
		t.Fatal("real Scheduler coordinator", err)
	}
	t.Cleanup(func() {
		coordinator.Stop()
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := coordinator.Drain(ctx); err != nil || !coordinator.Joined() {
			t.Error("original Scheduler calls did not join", err)
		}
	})
	return coordinator
}

// Preparation reuses the real P2/Agent/Object Runtime composition. The Task
// becomes todo only through the current Human Owner's public transition.
// No current Sprint, pending Dispatch or Execution is seeded in SQL.
// newDatabase uses the complete embedded *.sql source, including 44 and 45.
// The prefix40 upgrade subtest in TestTaskTransitionHuman is not called here.
func prepareSchedulerClaimTask(t *testing.T) *taskTransitionFixture {
	t.Helper()
	v := newTaskTransitionFixture(t)
	v.configureProject(t)
	request, commandMeta, _ := v.transferRequest(t)
	receipt, err := v.transitions.TransferTask(ctxFor(t), v.base.ownerBrowser.actor, commandMeta, v.base.project.ID, v.task.ID, request)
	if err != nil || receipt.Validate() != nil || receipt.Task.State != wc.TaskStateTodo || receipt.Task.Version != v.task.Version+1 || receipt.Task.AssigneeAgentID == nil || *receipt.Task.AssigneeAgentID != v.agentID {
		t.Fatal("real Owner assignment did not produce the intended todo Task", err)
	}
	v.task = receipt.Task
	return v
}

// Start uses the actual Work writer and Project pointer adapter. The original
// Human fixture's catalog and Service stay untouched.
func startSchedulerSprint(t *testing.T, v *taskTransitionFixture) {
	t.Helper()
	catalog := event.NewCatalog()
	events, err := wc.RegisterSprintLifecycleEvents(catalog)
	if err != nil {
		t.Fatal("formal Sprint lifecycle events", err)
	}
	box, err := outbox.New(v.base.tracked, catalog, outbox.Authorizations{
		Producers: map[event.StableName]oc.ProducerAuthority{wc.WorkProducer: v.authority},
		Sessions:  v.base.accounts, System: v.base.accounts, Projects: v.base.projectAuthority,
		Audit: v.base.audit, Cursors: v.base.keys, Processes: fixtureProcess{id[oc.Process](t)},
	})
	if err != nil {
		t.Fatal("real Sprint lifecycle Outbox", err)
	}
	pointer, err := project.NewSprintLifecycle(v.base.projectAuthority, v.authority)
	if err != nil {
		t.Fatal("same-Store Project Sprint pointer authority", err)
	}
	service, err := work.NewSprintLifecycle(v.base.tracked, work.SprintLifecycleDependencies{
		Authority: v.authority, Projects: pointer, Events: box, SprintEvents: events, Activity: v.base.accounts,
	})
	if err != nil {
		t.Fatal("real Sprint lifecycle service", err)
	}
	t.Cleanup(func() {
		service.Stop()
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := service.Drain(ctx); err != nil {
			t.Error("original Sprint lifecycle calls did not join", err)
		}
	})
	ctx, actor, p := ctxFor(t), v.base.ownerBrowser.actor, v.base.project.ID
	before, err := v.structureReader.GetSprint(ctx, actor, p, v.task.SprintID)
	if err != nil || before.State != wc.Planned {
		t.Fatal("actual Sprint must be planned before StartSprint", err)
	}
	receipt, err := service.StartSprint(ctx, actor, meta(t, "scheduler-start-sprint", &before.Version), p, before.ID)
	if err != nil || receipt.EventID.Validate() != nil || receipt.Sprint.State != wc.Current || receipt.Sprint.Version != before.Version+1 || receipt.Project.ID != p || receipt.Project.CurrentSprintID == nil || *receipt.Project.CurrentSprintID != before.ID {
		t.Fatal("formal StartSprint did not atomically establish the current Sprint", err)
	}
	current, err := v.structureReader.GetSprint(ctx, actor, p, before.ID)
	if err != nil || !bytes.Equal(jsonBytes(t, current), jsonBytes(t, receipt.Sprint)) {
		t.Fatal("current Sprint differs from the original StartSprint result", err)
	}
	projectState, err := v.base.projects.GetSchedulerConfig(ctx, actor, p)
	if err != nil || !bytes.Equal(jsonBytes(t, projectState.Project), jsonBytes(t, receipt.Project)) || !projectState.Config.Enabled || projectState.Config.MaxConcurrency == nil || *projectState.Config.MaxConcurrency != 2 {
		t.Fatal("StartSprint current Project/configuration differs from its committed facts", err)
	}
	v.base.project = projectState.Project
}

func schedulerClaimRequest(t *testing.T, v *taskTransitionFixture) (wc.TaskClaimRequest, ec.Policy) {
	t.Helper()
	request := wc.TaskClaimRequest{
		ProjectID: v.base.project.ID, TaskID: v.task.ID, AgentID: v.agentID,
		DispatchID: id[scheduler.DispatchIdentity](t).String(), ExpectedTaskVersion: v.task.Version,
		CurrentSprintID: v.task.SprintID, Purpose: "task/work", RequestID: id[f.Request](t),
	}
	policy := ec.Policy{SchemaVersion: 1, DeniedToolIDs: []i.ToolID{}, AllowedResourceConstraints: []json.RawMessage{}}
	if request.Validate() != nil || policy.Validate() != nil {
		t.Fatal("invalid fixed claim input")
	}
	return request, policy
}

// Observe complete original rows rather than selected successful counters.
// UUID Work/Project/Outbox IDs and text Scheduler/Execution IDs keep their
// actual database types. Claim records are final-Tx facts and must roll back
// with the Task, its history, typed event and pending Dispatch.
func schedulerClaimSnapshot(ctx context.Context, x postgres.SQLExecutor, v *taskTransitionFixture) (string, error) {
	var snapshot string
	err := x.QueryRow(ctx, `SELECT jsonb_build_object(
 'project',(SELECT to_jsonb(p) FROM agenteam_project.projects p WHERE id=$1::text::uuid),
 'sprint',(SELECT to_jsonb(s) FROM agenteam_work.sprints s WHERE project_id=$1::text::uuid AND id=$2::text::uuid),
 'tasks',(SELECT coalesce(jsonb_agg(to_jsonb(t) ORDER BY id),'[]'::jsonb) FROM agenteam_work.tasks t WHERE project_id=$1::text::uuid),
 'history',(SELECT coalesce(jsonb_agg(to_jsonb(e) ORDER BY id),'[]'::jsonb) FROM agenteam_work.task_events e WHERE project_id=$1::text::uuid),
 'groups',(SELECT coalesce(jsonb_agg(to_jsonb(g) ORDER BY sprint_id,state,priority),'[]'::jsonb) FROM agenteam_work.task_order_groups g WHERE project_id=$1::text::uuid),
 'generation',(SELECT query_generation FROM agenteam_work.task_query_generations WHERE project_id=$1::text::uuid),
 'outbox',(SELECT coalesce(jsonb_agg(to_jsonb(e) ORDER BY id),'[]'::jsonb) FROM agenteam_outbox.events e WHERE project_id=$1::text::uuid),
 'claims',(SELECT coalesce(jsonb_agg(to_jsonb(c) ORDER BY id),'[]'::jsonb) FROM agenteam_work.task_scheduler_claims c WHERE project_id=$1::text::uuid),
 'dispatches',(SELECT coalesce(jsonb_agg(to_jsonb(d) ORDER BY id),'[]'::jsonb) FROM agenteam_scheduler.dispatches d WHERE project_id=$1::text),
 'executions',(SELECT coalesce(jsonb_agg(to_jsonb(e) ORDER BY id),'[]'::jsonb) FROM agenteam_execution.executions e WHERE project_id=$1::text),
 'activity',(SELECT last_activity_at FROM agenteam_account.sessions WHERE id=$3::uuid)
 )::text`, v.base.project.ID.String(), v.task.SprintID.String(), v.base.ownerBrowser.actor.Details().SessionID).Scan(&snapshot)
	return snapshot, err
}

func requireSchedulerPending(t *testing.T, v *taskTransitionFixture, request wc.TaskClaimRequest, present bool) {
	t.Helper()
	user, _ := f.UserLock(v.base.ownerBrowser.actor.Details().UserID)
	project, _ := f.ProjectLock(v.base.project.ID.String())
	schedule, _ := f.ProjectScheduleLock(v.base.project.ID.String())
	v.base.tx(t, []f.LockRequest{{Key: user, Mode: f.Shared}, {Key: project, Mode: f.Shared}, {Key: schedule, Mode: f.Exclusive}}, func(ctx context.Context, tx f.Tx, _ postgres.SQLExecutor) error {
		if _, err := v.base.projectAuthority.RequireOwnerInTx(ctx, tx, v.base.ownerBrowser.actor, v.base.project.ID, i.Read); err != nil {
			return err
		}
		observed, err := v.pending.ReadInTx(ctx, tx, v.base.project.ID, []string{v.task.ID.String()})
		if err != nil {
			return err
		}
		if observed.Pending == nil || observed.HistoryTaskIDs == nil {
			return errors.New("pending read returned incomplete collections")
		}
		if !present {
			if len(observed.Pending) != 0 || len(observed.HistoryTaskIDs) != 0 {
				return errors.New("rolled-back claim left pending or historical Dispatch facts")
			}
			return nil
		}
		if len(observed.Pending) != 1 || len(observed.HistoryTaskIDs) != 1 || observed.HistoryTaskIDs[0] != v.task.ID.String() {
			return errors.New("committed claim did not expose exactly one pending Dispatch")
		}
		pending := observed.Pending[0]
		if pending.TaskID != request.TaskID.String() || pending.DispatchID != request.DispatchID || pending.AgentID != request.AgentID || pending.SprintID != request.CurrentSprintID.String() {
			return errors.New("pending reader changed the original claim identity")
		}
		return nil
	})
}

// The callback receives the original final Tx executor during fault injection;
// the committed path uses the ordinary Store. Both read the actual durable
// Task plus fixed Dispatch input, never a test-created applied-proof value.
func schedulerDispatchFacts(ctx context.Context, x postgres.SQLExecutor, v *taskTransitionFixture, request wc.TaskClaimRequest, policy ec.Policy, sourceGeneration int64) error {
	var launchRaw, guardRaw []byte
	var digest, key, requestID string
	var pendingCount, taskCount, executionCount int64
	err := x.QueryRow(ctx, `SELECT launch_request,claim_guard,launch_digest,idempotency_key,request_id::text,
 (SELECT count(*) FROM agenteam_scheduler.dispatches WHERE project_id=$1::text AND status='pending'),
 (SELECT count(*) FROM agenteam_work.tasks WHERE project_id=$1::text::uuid AND id=$3::text::uuid AND state='in_progress' AND version=$4 AND assignee_agent_id=$5::text::uuid),
 (SELECT count(*) FROM agenteam_execution.executions WHERE project_id=$1::text)
 FROM agenteam_scheduler.dispatches WHERE project_id=$1::text AND id=$2::text AND task_id=$3::text AND agent_id=$5::text
 AND sprint_id=$6::text AND status='pending' AND launch_outcome='not_sent' AND version=1 AND attempt_count=0 AND execution_id IS NULL AND next_retry_at IS NULL
 AND claim_source_sprint_id=$6::text AND claim_source_state='todo' AND claim_source_priority=$7`,
		request.ProjectID.String(), request.DispatchID, request.TaskID.String(), int64(request.ExpectedTaskVersion+1), request.AgentID.String(), request.CurrentSprintID.String(), string(v.task.Priority)).Scan(&launchRaw, &guardRaw, &digest, &key, &requestID, &pendingCount, &taskCount, &executionCount)
	if err != nil {
		return err
	}
	if pendingCount != 1 || taskCount != 1 || executionCount != 0 {
		return errors.New("claim did not atomically create one in-progress Task and one unlaunched pending Dispatch")
	}
	expected := ec.LaunchRequest{
		ProjectID: request.ProjectID, AgentID: request.AgentID,
		Trigger: ec.Trigger{Kind: "task", TaskID: request.TaskID.String()}, Purpose: "task/work",
		Policy: policy.Clone(), Lineage: ec.Lineage{DispatchID: request.DispatchID},
		Meta: f.CommandMeta{RequestID: request.RequestID, IdempotencyKey: f.IdempotencyKey("scheduler_dispatch:" + request.DispatchID)},
	}
	expectedRaw, err := json.Marshal(expected)
	if err != nil {
		return err
	}
	expectedDigest, err := expected.Digest()
	if err != nil || !bytes.Equal(launchRaw, expectedRaw) || digest != string(expectedDigest) || key != string(expected.Meta.IdempotencyKey) || requestID != request.RequestID.String() {
		return errors.New("persisted Launch input changed the original claim identity, policy or lineage")
	}
	var guard scheduler.ClaimGuard
	if err := json.Unmarshal(guardRaw, &guard); err != nil {
		return err
	}
	if guard.TaskID != request.TaskID.String() || guard.ClaimedVersion != request.ExpectedTaskVersion+1 || guard.SourceState != "todo" || guard.SourceAssigneeID != request.AgentID || guard.SourcePriority != string(v.task.Priority) || guard.SourceSprintID != request.CurrentSprintID.String() || guard.SourceOrderGeneration != f.Version(sourceGeneration+1) || guard.PredecessorID != "" || guard.SuccessorID != "" {
		return errors.New("pending claim lost the actual singleton todo source position")
	}
	var claims, history, related int64
	err = x.QueryRow(ctx, `SELECT
 (SELECT count(*) FROM agenteam_work.task_scheduler_claims WHERE project_id=$1::text::uuid),
 (SELECT count(*) FROM agenteam_work.task_events WHERE project_id=$1::text::uuid AND claim_operation_id=$2::text::uuid),
 (SELECT count(*) FROM agenteam_work.task_scheduler_claims c
 JOIN agenteam_work.task_events h ON h.project_id=c.project_id AND h.id=c.task_event_id
 JOIN agenteam_outbox.events o ON o.project_id=c.project_id AND o.id=c.event_id
 WHERE c.project_id=$1::text::uuid AND c.id=$2::text::uuid AND c.task_id=$3::text::uuid AND c.agent_id=$4::text::uuid
 AND c.request_id=$5::text::uuid AND c.expected_version=$6 AND c.claimed_version=$7
 AND h.task_id=c.task_id AND h.task_version=c.claimed_version AND h.type='state_changed'
 AND h.claim_operation_id=c.id AND h.correlation_id=c.id AND h.operation_id IS NULL AND h.blocker_operation_id IS NULL AND h.transition_operation_id IS NULL
 AND h.actor=jsonb_build_object('type','system','service_name','scheduler','cause_id',$2::text,'source','scheduler')
 AND h.payload=jsonb_build_object('from_state','todo','to_state','in_progress','reason_code','scheduler_claim')
 AND o.producer='work' AND o.event_type='work.task_transitioned' AND o.schema_version=2 AND o.aggregate_type='work.task'
 AND o.aggregate_id=c.task_id AND o.aggregate_version=c.claimed_version
 AND convert_from(o.payload,'UTF8')::jsonb->>'claim_id'=$2::text
 AND convert_from(o.payload,'UTF8')::jsonb->>'task_event_id'=h.id::text
 AND convert_from(o.payload,'UTF8')::jsonb->'actor'=h.actor
 AND convert_from(o.payload,'UTF8')::jsonb->>'agent_id'=$4::text
 AND convert_from(o.payload,'UTF8')::jsonb->>'sprint_id'=$8::text
 AND convert_from(o.payload,'UTF8')::jsonb->>'milestone_id'=$9::text
 AND convert_from(o.payload,'UTF8')::jsonb->'source_position'->>'state'='todo'
 AND convert_from(o.payload,'UTF8')::jsonb->'target_position'->>'state'='in_progress')`,
		request.ProjectID.String(), request.DispatchID, request.TaskID.String(), request.AgentID.String(), request.RequestID.String(), int64(request.ExpectedTaskVersion), int64(request.ExpectedTaskVersion+1), request.CurrentSprintID.String(), v.task.MilestoneID.String()).Scan(&claims, &history, &related)
	if err != nil {
		return err
	}
	if claims != 1 || history != 1 || related != 1 {
		return errors.New("original Work claim, Scheduler history and typed Outbox are not atomically related")
	}
	return nil
}

func schedulerSourceGeneration(t *testing.T, v *taskTransitionFixture) int64 {
	t.Helper()
	var generation int64
	if err := v.base.raw.QueryRow(ctxFor(t), `SELECT order_generation FROM agenteam_work.task_order_groups
 WHERE project_id=$1 AND sprint_id=$2 AND state='todo' AND priority=$3`, v.base.project.ID.String(), v.task.SprintID.String(), string(v.task.Priority)).Scan(&generation); err != nil || generation < 1 {
		t.Fatal("real todo source group generation", err)
	}
	return generation
}

func runSchedulerClaimCommit(t *testing.T, v *taskTransitionFixture, coordinator *scheduler.Coordinator) {
	t.Helper()
	request, policy := schedulerClaimRequest(t, v)
	generation := schedulerSourceGeneration(t, v)
	claimed, err := coordinator.ClaimTask(ctxFor(t), request, policy)
	if err != nil {
		t.Fatal("real Scheduler claim", err)
	}
	summary := claimed.Summary()
	if summary.ID.String() != request.DispatchID || summary.ProjectID != request.ProjectID || summary.TaskID != request.TaskID.String() || summary.SprintID != request.CurrentSprintID.String() || summary.AgentID != request.AgentID || summary.Status != scheduler.Pending || summary.LaunchOutcome != scheduler.NotSent || summary.Version != 1 || summary.AttemptCount != 0 || summary.ExecutionID != nil {
		t.Fatal("claim returned an inconsistent pending Dispatch")
	}
	if err := schedulerDispatchFacts(ctxFor(t), v.base.raw, v, request, policy, generation); err != nil {
		t.Fatal("committed Scheduler/Work facts", err)
	}
	current, err := v.taskReader.GetTask(ctxFor(t), v.base.ownerBrowser.actor, request.ProjectID, request.TaskID)
	if err != nil || current.State != wc.TaskStateInProgress || current.Version != request.ExpectedTaskVersion+1 || current.AssigneeAgentID == nil || *current.AssigneeAgentID != request.AgentID {
		t.Fatal("current Task did not expose the committed claim", err)
	}
	requireSchedulerPending(t, v, request, true)
	before, err := schedulerClaimSnapshot(ctxFor(t), v.base.raw, v)
	if err != nil {
		t.Fatal("committed claim snapshot", err)
	}
	replay, err := coordinator.ClaimTask(ctxFor(t), request, policy)
	if err != nil || replay.Summary() != summary {
		t.Fatal("same-request claim replay changed the original Dispatch", err)
	}
	originalLaunch, originalErr := claimed.LaunchRequest()
	replayedLaunch, replayErr := replay.LaunchRequest()
	if originalErr != nil || replayErr != nil || !bytes.Equal(jsonBytes(t, originalLaunch), jsonBytes(t, replayedLaunch)) || originalLaunch.Meta != replayedLaunch.Meta {
		t.Fatal("same-request claim replay changed the original Launch input")
	}
	after, err := schedulerClaimSnapshot(ctxFor(t), v.base.raw, v)
	if err != nil || before != after {
		t.Fatal("claim replay changed Task, history, order, Dispatch, Outbox or activity", err)
	}
}

func runSchedulerClaimRollback(t *testing.T, v *taskTransitionFixture, coordinator *scheduler.Coordinator) {
	t.Helper()
	request, policy := schedulerClaimRequest(t, v)
	generation := schedulerSourceGeneration(t, v)
	command, err := f.NewCommandIdentity("scheduler", []string{request.ProjectID.String()}, "claim", f.IdempotencyKey("scheduler_claim:"+request.DispatchID))
	if err != nil {
		t.Fatal(err)
	}
	before, err := schedulerClaimSnapshot(ctxFor(t), v.base.raw, v)
	if err != nil {
		t.Fatal("original claim snapshot", err)
	}
	marker := errors.New("scheduler-claim-final-callback-rollback")
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
		var inserted bool
		if err := x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_scheduler.dispatches WHERE project_id=$1 AND id=$2)`, request.ProjectID.String(), request.DispatchID).Scan(&inserted); err != nil {
			return err
		}
		if !inserted {
			return nil // Original discovery has no pending Dispatch yet.
		}
		if err := schedulerDispatchFacts(ctx, x, v, request, policy, generation); err != nil {
			return err
		}
		observed = true
		return marker // Original callback has finished; the real Store rolls back.
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
	claimed, err := coordinator.ClaimTask(ctxFor(t), request, policy)
	clear()
	if !observed || !returned || physical.State() != f.NotCommitted || !errors.Is(physical.Fault(), marker) || !errors.Is(err, marker) || claimed.Summary().ID.Validate() == nil {
		t.Fatal("late claim rollback lost the original physical result or returned a Dispatch", err)
	}
	after, err := schedulerClaimSnapshot(ctxFor(t), v.base.raw, v)
	if err != nil || before != after {
		t.Fatal("rolled-back claim changed Task, history, order, Dispatch, Outbox or activity", err)
	}
	requireSchedulerPending(t, v, request, false)
}
