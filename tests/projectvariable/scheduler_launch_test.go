//go:build integration

package projectvariable_test

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/agent"
	"github.com/LunaDeerTech/agenteam/internal/central/execution"
	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/LunaDeerTech/agenteam/internal/central/project"
	"github.com/LunaDeerTech/agenteam/internal/central/scheduler"
	"github.com/LunaDeerTech/agenteam/internal/central/work"
)

// The new scenarios reuse the completed real Claim fixture. Launch must stop
// at a durable created Execution and its Dispatch association: no test writes
// a Task, Execution, pending row, Snapshot or running state to manufacture it.
func TestSchedulerLaunch(t *testing.T) {
	t.Run("created-association-and-replay", func(t *testing.T) {
		x := newSchedulerLaunchFixture(t)
		before := x.task.databaseSnapshot(t)
		launched, err := x.handoff.LaunchOnce(ctxFor(t), x.request.ProjectID, x.dispatch)
		if err != nil {
			t.Fatal("real Scheduler Launch", err)
		}
		x.requireAssociated(t, launched)
		stable, err := schedulerClaimSnapshot(ctxFor(t), x.task.base.raw, x.task)
		if err != nil {
			t.Fatal("committed Launch snapshot", err)
		}
		replay, err := x.handoff.LaunchOnce(ctxFor(t), x.request.ProjectID, x.dispatch)
		if err != nil || !reflect.DeepEqual(replay.Summary(), launched.Summary()) {
			t.Fatal("same Dispatch changed its committed Launch result", err)
		}
		x.requireAssociated(t, replay)
		after, err := schedulerClaimSnapshot(ctxFor(t), x.task.base.raw, x.task)
		if err != nil || stable != after || before != x.task.databaseSnapshot(t) {
			t.Fatal("Launch or replay rewrote Task/history/activity or created duplicate facts", err)
		}
	})
	t.Run("association-failure-lookup-recovery", func(t *testing.T) {
		x := newSchedulerLaunchFixture(t)
		before := x.task.databaseSnapshot(t)
		command, err := f.NewCommandIdentity("scheduler", []string{x.request.ProjectID.String()}, "associate_launch", x.request.Meta.IdempotencyKey)
		if err != nil {
			t.Fatal("association command", err)
		}
		marker := errors.New("scheduler-launch-association-callback-rollback")
		var observed, returned bool
		var physical f.CommitResult
		store := x.task.base.tracked
		store.setAfter(func(ctx context.Context, tx f.Tx, cause f.TransactionCause) error {
			if cause.Kind() != f.CommandsCause || cause.Details().Primary.Canonical() != command.Canonical() {
				return nil
			}
			_, _, created := x.launcher.observed()
			if created.ID.Validate() != nil {
				return errors.New("association preceded the real Execution Launch return")
			}
			sql, err := store.InTx(tx)
			if err != nil {
				return err
			}
			if err = schedulerLaunchFacts(ctx, sql, x.request, created.ID, true); err != nil {
				return err
			}
			observed = true
			return marker // Original association callback succeeded; actual Store rolls back.
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
		result, err := x.handoff.LaunchOnce(ctxFor(t), x.request.ProjectID, x.dispatch)
		clear()
		if !observed || !returned || physical.State() != f.NotCommitted || !errors.Is(physical.Fault(), marker) || err == nil || result.Summary().Status == scheduler.Launched {
			t.Fatal("association failure lost the real rollback or reported an uncommitted association", err)
		}
		launches, lookups, created := x.launcher.observed()
		if launches != 1 || created.ID.Validate() != nil || created.Status != ec.Created {
			t.Fatal("original Execution did not actually commit before association failed")
		}
		if err := schedulerLaunchFacts(ctxFor(t), x.task.base.raw, x.request, created.ID, false); err != nil {
			t.Fatal("association rollback did not preserve the committed Execution and original pending intent", err)
		}
		requireSchedulerLaunchObservation(t, x.task, x.request, created.ID, false)
		// This is association recovery, not a fake Execution CommitUnknown.
		// The original canonical key/digest is looked up, never resent as Launch.
		recovered, err := x.handoff.Lookup(ctxFor(t), x.request.ProjectID, x.dispatch)
		if err != nil {
			t.Fatal("original-key Launch lookup recovery", err)
		}
		x.requireAssociated(t, recovered)
		afterLaunches, afterLookups, same := x.launcher.observed()
		if afterLaunches != 1 || afterLookups <= lookups || same.ID != created.ID || before != x.task.databaseSnapshot(t) {
			t.Fatal("association recovery resent Launch, changed identity or mutated the Task")
		}
	})
}

// This transparent delegate counts actual synchronous calls. It neither
// fabricates permits/results nor changes arguments, contexts, errors or tails.
type countedSchedulerExecution struct {
	service           *execution.Service
	mu                sync.Mutex
	launches, lookups int
	created           ec.Summary
}

func (c *countedSchedulerExecution) Launch(ctx context.Context, actor i.Actor, request ec.LaunchRequest) (ec.LaunchResult, error) {
	c.mu.Lock()
	c.launches++
	c.mu.Unlock()
	result, err := c.service.Launch(ctx, actor, request)
	if err == nil {
		c.mu.Lock()
		c.created = result.Execution.Clone()
		c.mu.Unlock()
	}
	return result, err
}
func (c *countedSchedulerExecution) LookupLaunch(ctx context.Context, actor i.Actor, key ec.LaunchLookupKey, digest f.Digest) (ec.LaunchLookup, error) {
	c.mu.Lock()
	c.lookups++
	c.mu.Unlock()
	return c.service.LookupLaunch(ctx, actor, key, digest)
}
func (c *countedSchedulerExecution) observed() (int, int, ec.Summary) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.launches, c.lookups, c.created.Clone()
}

type schedulerLaunchFixture struct {
	task     *taskTransitionFixture
	dispatch scheduler.DispatchID
	request  ec.LaunchRequest
	handoff  *scheduler.LaunchHandoff
	launcher *countedSchedulerExecution
}

func newSchedulerLaunchFixture(t *testing.T) *schedulerLaunchFixture {
	t.Helper()
	v, claims := newSchedulerClaimFixture(t)
	claim, policy := schedulerClaimRequest(t, v)
	dispatch, err := claims.ClaimTask(ctxFor(t), claim, policy)
	if err != nil || dispatch.Summary().Status != scheduler.Pending {
		t.Fatal("formal Claim did not establish a pending Launch intent", err)
	}
	request, err := dispatch.LaunchRequest()
	if err != nil || request.Validate() != nil {
		t.Fatal("original Dispatch Launch request", err)
	}
	access, err := project.NewSchedulerExecutionAccess(v.base.projectAuthority, v.pending)
	if err != nil {
		t.Fatal("real Scheduler Project access", err)
	}
	authority, err := execution.NewAuthority(v.base.tracked, v.base.projectAuthority, access)
	if err != nil {
		t.Fatal("same-Store Execution authority", err)
	}
	configuration, err := agent.NewExecutionConfiguration(v.agent.providers.Agents, authority)
	if err != nil {
		t.Fatal("real initialized Agent configuration", err)
	}
	provider, err := work.NewTaskLaunchProvider(v.base.tracked, v.authority, v.pending)
	if err != nil {
		t.Fatal("real Task Launch provider", err)
	}
	service, err := execution.New(v.base.tracked, execution.Dependencies{Authority: authority, Agents: configuration, Task: provider})
	if err != nil {
		t.Fatal("real Execution Launch service", err)
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
		t.Fatal("real canonical Execution observer", err)
	}
	counter := &countedSchedulerExecution{service: service}
	handoff, err := scheduler.NewLaunchHandoff(v.pending, scheduler.LaunchHandoffDependencies{Executions: counter, Observations: observer})
	if err != nil {
		t.Fatal("real Scheduler Launch handoff", err)
	}
	t.Cleanup(func() {
		handoff.Stop()
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := handoff.Drain(ctx); err != nil || !handoff.Joined() {
			t.Error("original Scheduler Launch calls did not join", err)
		}
	})
	return &schedulerLaunchFixture{v, dispatch.Summary().ID, request, handoff, counter}
}

func (x *schedulerLaunchFixture) requireAssociated(t *testing.T, dispatch scheduler.Dispatch) {
	t.Helper()
	launches, _, created := x.launcher.observed()
	summary := dispatch.Summary()
	if launches != 1 || summary.ID != x.dispatch || summary.ProjectID != x.request.ProjectID || summary.AgentID != x.request.AgentID || summary.TaskID != x.request.Trigger.TaskID || summary.Status != scheduler.Launched || summary.LaunchOutcome != scheduler.Created || summary.AttemptCount != 1 || summary.ExecutionID == nil || *summary.ExecutionID != created.ID || created.ID.Validate() != nil {
		t.Fatal("Scheduler association does not match its single actual Execution Launch")
	}
	request, err := dispatch.LaunchRequest()
	if err != nil || !reflect.DeepEqual(request, x.request) {
		t.Fatal("association changed the original complete Launch request", err)
	}
	if err = schedulerLaunchFacts(ctxFor(t), x.task.base.raw, x.request, created.ID, true); err != nil {
		t.Fatal("committed Launch association facts", err)
	}
	requireSchedulerLaunchObservation(t, x.task, x.request, created.ID, true)
}

// This same-transaction observation counts the pending reservation OR its
// canonical linked Execution, never both. Even after an association failure,
// the already committed Execution must occupy the original global Agent slot.
func requireSchedulerLaunchObservation(t *testing.T, v *taskTransitionFixture, request ec.LaunchRequest, executionID i.ExecutionID, associated bool) {
	t.Helper()
	observer, err := execution.NewDispatchObservation(v.base.tracked)
	if err != nil {
		t.Fatal("real Execution observation", err)
	}
	digest, err := request.Digest()
	if err != nil {
		t.Fatal("original Launch digest", err)
	}
	command, err := request.Command()
	if err != nil {
		t.Fatal("original Launch command", err)
	}
	user, _ := f.UserLock(v.base.ownerBrowser.actor.Details().UserID)
	project, _ := f.ProjectLock(request.ProjectID.String())
	schedule, _ := f.ProjectScheduleLock(request.ProjectID.String())
	agent, _ := f.AgentLock(request.AgentID.String())
	key, _ := f.CommandLock(command)
	v.base.tx(t, []f.LockRequest{
		{Key: user, Mode: f.Shared}, {Key: project, Mode: f.Shared},
		{Key: schedule, Mode: f.Exclusive}, {Key: agent, Mode: f.Shared}, {Key: key, Mode: f.Exclusive},
	}, func(ctx context.Context, tx f.Tx, _ postgres.SQLExecutor) error {
		grant, err := v.base.projectAuthority.RequireOwnerInTx(ctx, tx, v.base.ownerBrowser.actor, request.ProjectID, i.Read)
		if err != nil {
			return err
		}
		if !grant.Matches(v.base.ownerBrowser.actor, request.ProjectID) {
			return errors.New("original Owner observation is not authorized")
		}
		found, err := observer.LookupLaunchInTx(ctx, tx, ec.LaunchLookupKey{ProjectID: request.ProjectID, AgentID: request.AgentID, IdempotencyKey: request.Meta.IdempotencyKey}, digest, request.Lineage.DispatchID)
		if err != nil {
			return err
		}
		if !found.Found || found.RequestDigest != digest || found.Execution == nil || found.Execution.ID != executionID || found.Execution.ProjectID != request.ProjectID || found.Execution.AgentID != request.AgentID || found.Execution.Trigger != request.Trigger || found.Execution.Purpose != request.Purpose || found.Execution.Status != ec.Created || found.Execution.Version != 1 || found.Execution.SnapshotID != nil || found.Execution.StartedAt != nil || found.Execution.CompletedAt != nil || found.Execution.CancelRequestedAt != nil {
			return errors.New("original Launch lookup lost the created identity or started preparation")
		}
		slot, err := observer.AgentSlotInTx(ctx, tx, request.ProjectID, request.AgentID)
		if err != nil {
			return err
		}
		if !slot.Occupied || slot.ExecutionID == nil || *slot.ExecutionID != executionID || slot.Status != ec.Created || slot.CancelRequestedAt != nil {
			return errors.New("created Execution does not own the original Agent slot")
		}
		pending, err := v.pending.ReadInTx(ctx, tx, request.ProjectID, []string{request.Trigger.TaskID})
		if err != nil {
			return err
		}
		if pending.Pending == nil || len(pending.HistoryTaskIDs) != 1 || pending.HistoryTaskIDs[0] != request.Trigger.TaskID {
			return errors.New("Dispatch history disappeared during Launch")
		}
		links := []ec.AssociatedDispatch{}
		if associated {
			if len(pending.Pending) != 0 {
				return errors.New("associated Dispatch still reserves a pending slot")
			}
			links = append(links, ec.AssociatedDispatch{ExecutionID: executionID, AgentID: request.AgentID, Key: request.Meta.IdempotencyKey, Digest: digest, DispatchID: request.Lineage.DispatchID})
		} else if len(pending.Pending) != 1 || pending.Pending[0].DispatchID != request.Lineage.DispatchID || pending.Pending[0].AgentID != request.AgentID {
			return errors.New("unassociated Launch lost its original pending reservation")
		}
		used, err := observer.CountAssociatedInTx(ctx, tx, request.ProjectID, links)
		if err != nil {
			return err
		}
		if int64(len(pending.Pending))+used != 1 {
			return errors.New("Launch association duplicated or released Scheduler capacity")
		}
		return nil
	})
}

// All SQL below is observation. The callback form is also used immediately
// after the real association writer, before the injected error rolls it back.
func schedulerLaunchFacts(ctx context.Context, x postgres.SQLExecutor, request ec.LaunchRequest, executionID i.ExecutionID, associated bool) error {
	raw, err := json.Marshal(request)
	if err != nil {
		return err
	}
	digest, err := request.Digest()
	if err != nil {
		return err
	}
	initiator, err := json.Marshal(i.ActorDetails{Kind: i.Service, ServiceName: i.Scheduler, ProjectID: request.ProjectID.String(), CauseRef: request.Lineage.DispatchID})
	if err != nil {
		return err
	}
	status, outcome := "pending", "unknown"
	if associated {
		status, outcome = "launched", "created"
	}
	var dispatches, executions, slot, originalExecution, originalDispatch int64
	err = x.QueryRow(ctx, `SELECT
 (SELECT count(*) FROM agenteam_scheduler.dispatches WHERE project_id=$1::text),
 (SELECT count(*) FROM agenteam_execution.executions WHERE project_id=$1::text),
 (SELECT count(*) FROM agenteam_execution.executions WHERE agent_id=$2::text AND status IN ('created','preparing','running','waiting')),
 (SELECT count(*) FROM agenteam_execution.executions WHERE project_id=$1::text AND agent_id=$2::text AND id=$3::text
  AND status='created' AND version=1 AND idempotency_key=$4 AND request_id=$5::text AND request_digest=$6
  AND launch_request=$7::jsonb AND trigger_reference_digest ~ '^sha256:[0-9a-f]{64}$'
  AND initiator=$13::jsonb
  AND snapshot_id IS NULL AND started_at IS NULL AND completed_at IS NULL AND cancel_requested_at IS NULL),
 (SELECT count(*) FROM agenteam_scheduler.dispatches WHERE project_id=$1::text AND agent_id=$2::text AND id=$8::text
  AND idempotency_key=$4 AND request_id=$5::text AND launch_digest=$6 AND convert_from(launch_request,'UTF8')::jsonb=$7::jsonb
  AND task_id=$9::text AND status=$10 AND launch_outcome=$11 AND attempt_count=1 AND next_retry_at IS NULL
  AND (($12::boolean AND execution_id=$3::text) OR (NOT $12::boolean AND execution_id IS NULL)))`,
		request.ProjectID.String(), request.AgentID.String(), executionID.String(), request.Meta.IdempotencyKey.String(), request.Meta.RequestID.String(), string(digest), raw, request.Lineage.DispatchID, request.Trigger.TaskID, status, outcome, associated, initiator).Scan(&dispatches, &executions, &slot, &originalExecution, &originalDispatch)
	if err != nil {
		return err
	}
	if dispatches != 1 || executions != 1 || slot != 1 || originalExecution != 1 || originalDispatch != 1 {
		return errors.New("Launch changed the original identity, full input, slot or Dispatch association")
	}
	return nil
}
