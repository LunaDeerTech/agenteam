//go:build integration

package projectvariable_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	ac "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	wc "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
)

// This checks a Project-scoped Work predicate, not retirement or owner Join.
// All positive Agent/Task facts use the original public services. Only the two
// explicitly rolled-back terminal query inputs below use test-owned SQL.
func TestWorkAgentAssigneeReferences(t *testing.T) {
	t.Run("same-transaction-current-and-terminal-references", testWorkAssigneeCurrent)
	t.Run("authority-and-transaction-boundaries", testWorkAssigneeBoundaries)
	t.Run("caller-lock-release-before-original-return", testWorkAssigneeCallerReturn)
}

type workAssigneeFixture struct {
	*taskTransitionFixture
	unassigned i.AgentID
}

func newWorkAssigneeFixture(t *testing.T) *workAssigneeFixture {
	t.Helper()
	v := newTaskTransitionFixture(t)
	r, m, _ := v.transferRequest(t)
	assigned, err := v.transitions.TransferTask(ctxFor(t), v.base.ownerBrowser.actor, m, v.base.project.ID, v.task.ID, r)
	workAssigneeRequire(t, err)
	if assigned.Validate() != nil || assigned.Task.State != wc.TaskStateTodo || assigned.Task.AssigneeAgentID == nil || *assigned.Task.AssigneeAgentID != v.agentID {
		t.Fatal("formal backlog-to-todo assignment was not published")
	}
	v.task = assigned.Task
	raw, command, _ := v.agent.request(t)
	fields := raw.Fields()
	fields.Name = "unassigned-reference-agent"
	request, err := ac.NewAgentCreate(fields)
	workAssigneeRequire(t, err)
	created, err := v.agent.agents.CreateAgent(ctxFor(t), v.base.ownerBrowser.actor, command, v.base.project.ID, request)
	workAssigneeRequire(t, err)
	if created.Validate() != nil || created.Fields().Agent.Fields().Core.ID != fields.AgentID || fields.AgentID == v.agentID {
		t.Fatal("second Agent was not created through its real original service")
	}
	var tasks, claims, executions int
	workAssigneeRequire(t, v.base.raw.QueryRow(ctxFor(t), `SELECT
 (SELECT count(*) FROM agenteam_work.tasks WHERE project_id=$1::uuid),
 (SELECT count(*) FROM agenteam_work.task_scheduler_claims WHERE project_id=$1::uuid),
 (SELECT count(*) FROM agenteam_execution.executions WHERE project_id=$1::text)`, v.base.project.ID.String()).Scan(&tasks, &claims, &executions))
	if tasks != 1 || claims != 0 || executions != 0 {
		t.Fatal("fixture must contain the original assigned Task without Scheduler or Execution work")
	}
	return &workAssigneeFixture{taskTransitionFixture: v, unassigned: fields.AgentID}
}

func workAssigneeRequire(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal("original Work assignee fixture operation failed", err)
	}
}

func workAssigneeCode(err error, code f.Code) bool {
	var value *f.Fault
	return errors.As(err, &value) && value.Code == code
}

func workAssigneeLocks(t *testing.T, actor i.Actor, project i.ProjectID, agent i.AgentID) []f.LockRequest {
	t.Helper()
	user, err := f.UserLock(actor.Details().UserID)
	workAssigneeRequire(t, err)
	p, err := f.ProjectLock(project.String())
	workAssigneeRequire(t, err)
	schedule, err := f.ProjectScheduleLock(project.String())
	workAssigneeRequire(t, err)
	a, err := f.AgentLock(agent.String())
	workAssigneeRequire(t, err)
	locks, err := oc.NormalizeLocks([]f.LockRequest{{Key: user, Mode: f.Shared}, {Key: p, Mode: f.Shared}, {Key: schedule, Mode: f.Exclusive}, {Key: a, Mode: f.Exclusive}})
	workAssigneeRequire(t, err)
	return locks
}

func (v *workAssigneeFixture) tx(t *testing.T, locks []f.LockRequest, fn func(context.Context, f.Tx, postgres.SQLExecutor) error) f.CommitResult {
	t.Helper()
	return v.base.tracked.WithinTx(ctxFor(t), cause(t), func(ctx context.Context, tx f.Tx) error {
		if err := v.base.tracked.AcquireAll(ctx, tx, locks); err != nil {
			return err
		}
		x, err := v.base.tracked.InTx(tx)
		if err != nil {
			return err
		}
		return fn(ctx, tx, x)
	})
}

func workAssigneeCommitted(t *testing.T, result f.CommitResult) {
	t.Helper()
	if result.State() != f.Committed {
		t.Fatal("original read caller transaction did not commit", result.State(), result.Fault())
	}
}

func (v *workAssigneeFixture) current(ctx context.Context, tx f.Tx, project i.ProjectID, agent i.AgentID) error {
	actor := v.base.ownerBrowser.actor
	ref, err := v.agent.providers.Agents.RequireCurrentInTx(ctx, tx, actor, project, agent)
	if err != nil {
		return err
	}
	if ref.Validate() != nil || ref.ProjectID != project || ref.AgentID != agent {
		return errors.New("original Agent current provider returned a different target")
	}
	return v.authority.RequireAgentUnassignedInTx(ctx, tx, actor, project, agent)
}

func (v *workAssigneeFixture) snapshot(t *testing.T) string {
	t.Helper()
	var value string
	workAssigneeRequire(t, v.base.raw.QueryRow(ctxFor(t), `SELECT jsonb_build_object(
 'tasks',(SELECT coalesce(jsonb_agg(to_jsonb(t) ORDER BY id),'[]'::jsonb) FROM agenteam_work.tasks t WHERE project_id=$1::uuid),
 'history',(SELECT coalesce(jsonb_agg(to_jsonb(h) ORDER BY id),'[]'::jsonb) FROM agenteam_work.task_events h WHERE project_id=$1::uuid),
 'commands',(SELECT coalesce(jsonb_agg(to_jsonb(c) ORDER BY id),'[]'::jsonb) FROM agenteam_work.task_commands c WHERE project_id=$1::uuid),
 'transitions',(SELECT coalesce(jsonb_agg(to_jsonb(c) ORDER BY id),'[]'::jsonb) FROM agenteam_work.task_transition_commands c WHERE project_id=$1::uuid),
 'groups',(SELECT coalesce(jsonb_agg(to_jsonb(g) ORDER BY sprint_id,state,priority),'[]'::jsonb) FROM agenteam_work.task_order_groups g WHERE project_id=$1::uuid),
 'generation',(SELECT to_jsonb(g) FROM agenteam_work.task_query_generations g WHERE project_id=$1::uuid),
 'claims',(SELECT coalesce(jsonb_agg(to_jsonb(c) ORDER BY id),'[]'::jsonb) FROM agenteam_work.task_scheduler_claims c WHERE project_id=$1::uuid),
 'dispatches',(SELECT coalesce(jsonb_agg(to_jsonb(d) ORDER BY id),'[]'::jsonb) FROM agenteam_scheduler.dispatches d WHERE project_id=$1::text),
 'executions',(SELECT coalesce(jsonb_agg(to_jsonb(e) ORDER BY id),'[]'::jsonb) FROM agenteam_execution.executions e WHERE project_id=$1::text),
 'agents',(SELECT coalesce(jsonb_agg(to_jsonb(a) ORDER BY id),'[]'::jsonb) FROM agenteam_agent.agents a WHERE project_id=$1::uuid),
 'agent_commands',(SELECT coalesce(jsonb_agg(to_jsonb(c) ORDER BY id),'[]'::jsonb) FROM agenteam_agent.commands c WHERE project_id=$1::uuid),
 'events',(SELECT coalesce(jsonb_agg(to_jsonb(e) ORDER BY id),'[]'::jsonb) FROM agenteam_outbox.events e WHERE project_id=$1::uuid),
 'activity',(SELECT last_activity_at FROM agenteam_account.sessions WHERE id=$2::uuid)
 )::text`, v.base.project.ID.String(), v.base.ownerBrowser.actor.Details().SessionID).Scan(&value))
	return value
}

func testWorkAssigneeCurrent(t *testing.T) {
	v := newWorkAssigneeFixture(t)
	actor, project := v.base.ownerBrowser.actor, v.base.project.ID
	before := v.snapshot(t)
	for _, target := range []i.AgentID{v.agentID, v.unassigned} {
		result := v.tx(t, workAssigneeLocks(t, actor, project, target), func(ctx context.Context, tx f.Tx, _ postgres.SQLExecutor) error {
			err := v.current(ctx, tx, project, target)
			if target == v.agentID {
				if !workAssigneeCode(err, f.ResourceBusy) {
					return errors.New("current nonempty assignee was not rejected")
				}
				return nil
			}
			return err
		})
		workAssigneeCommitted(t, result)
	}
	// Production cancellation is still unbound; reaching done normally needs
	// the complete Execution chain. These are only query inputs in one owned
	// transaction. Preserve the assignee, never commit or synthesize history.
	rollback := errors.New("work-assignee-terminal-query-rollback")
	for _, state := range []wc.TaskState{wc.TaskStateDone, wc.TaskStateCancelled} {
		locks := workAssigneeLocks(t, actor, project, v.agentID)
		task, err := f.AggregateLock(f.TaskAggregate, v.task.ID.String())
		workAssigneeRequire(t, err)
		sprint, err := f.AggregateLock(f.SprintAggregate, v.task.SprintID.String())
		workAssigneeRequire(t, err)
		locks = append(locks, f.LockRequest{Key: task, Mode: f.Exclusive}, f.LockRequest{Key: sprint, Mode: f.Shared})
		for _, group := range []wc.TaskState{wc.TaskStateTodo, state} {
			key, err := f.RankGroupLock("work.task:" + project.String() + ":" + v.task.SprintID.String() + ":" + string(group) + ":" + string(v.task.Priority))
			workAssigneeRequire(t, err)
			locks = append(locks, f.LockRequest{Key: key, Mode: f.Exclusive})
		}
		locks, err = oc.NormalizeLocks(locks)
		workAssigneeRequire(t, err)
		checked := false
		result := v.tx(t, locks, func(ctx context.Context, tx f.Tx, x postgres.SQLExecutor) error {
			tag, err := x.Exec(ctx, `UPDATE agenteam_work.tasks SET state=$5 WHERE project_id=$1 AND id=$2 AND version=$3 AND assignee_agent_id=$4 AND state='todo'`, project.String(), v.task.ID.String(), int64(v.task.Version), v.agentID.String(), string(state))
			if err != nil {
				return err
			}
			if tag.RowsAffected() != 1 {
				return errors.New("controlled terminal input did not select the original Task")
			}
			var currentState, assignee string
			if err = x.QueryRow(ctx, `SELECT state,assignee_agent_id::text FROM agenteam_work.tasks WHERE project_id=$1 AND id=$2`, project.String(), v.task.ID.String()).Scan(&currentState, &assignee); err != nil {
				return err
			}
			if currentState != string(state) || assignee != v.agentID.String() || !workAssigneeCode(v.current(ctx, tx, project, v.agentID), f.ResourceBusy) {
				return errors.New("terminal current assignee was omitted from the real Work predicate")
			}
			checked = true
			return rollback
		})
		if !checked || result.State() != f.NotCommitted || !errors.Is(result.Fault(), rollback) || v.snapshot(t) != before {
			t.Fatal("controlled terminal query did not return through original rollback with facts restored")
		}
	}
	if v.snapshot(t) != before {
		t.Fatal("read predicate changed original Task, Agent, history or claim facts")
	}
	t.Log("work_agent_assignee_references real_agents=2 formal_todo=true assigned_busy=true unassigned_current=true terminal_query_fixture=done,cancelled original_rollback=true history_unchanged=true lifecycle_terminal_claim=false")
}

func testWorkAssigneeBoundaries(t *testing.T) {
	v := newWorkAssigneeFixture(t)
	actor, project := v.base.ownerBrowser.actor, v.base.project.ID
	before := v.snapshot(t)
	missing := id[i.Agent](t)
	otherProject := v.agent.p2.base.project.ID
	if otherProject == project {
		t.Fatal("original fixture must retain its distinct initialized parent Project")
	}
	// Missing and other-Project targets can yield only a Work-scoped nil. The
	// same real Agent provider remains responsible for canonical existence.
	for _, target := range []struct {
		project i.ProjectID
		agent   i.AgentID
	}{{project, missing}, {otherProject, v.agentID}} {
		result := v.tx(t, workAssigneeLocks(t, actor, target.project, target.agent), func(ctx context.Context, tx f.Tx, _ postgres.SQLExecutor) error {
			if err := v.authority.RequireAgentUnassignedInTx(ctx, tx, actor, target.project, target.agent); err != nil {
				return err
			}
			_, err := v.agent.providers.Agents.RequireCurrentInTx(ctx, tx, actor, target.project, target.agent)
			if !workAssigneeCode(err, f.NotFound) {
				return errors.New("Work-scoped nil bypassed the real Agent existence boundary")
			}
			return nil
		})
		workAssigneeCommitted(t, result)
	}
	other := v.base.otherBrowser.actor
	result := v.tx(t, workAssigneeLocks(t, other, project, v.unassigned), func(ctx context.Context, tx f.Tx, _ postgres.SQLExecutor) error {
		return v.authority.RequireAgentUnassignedInTx(ctx, tx, other, project, v.unassigned)
	})
	if result.State() != f.NotCommitted || !workAssigneeCode(result.Fault(), f.NotFound) {
		t.Fatal("empty predicate bypassed the actual current Owner gate")
	}
	full := workAssigneeLocks(t, actor, project, v.unassigned)
	schedule, err := f.ProjectScheduleLock(project.String())
	workAssigneeRequire(t, err)
	agent, err := f.AgentLock(v.unassigned.String())
	workAssigneeRequire(t, err)
	for _, weakAgent := range []bool{false, true} {
		var locks []f.LockRequest
		for _, lock := range full {
			if !weakAgent && lock.Key.Canonical() == schedule.Canonical() {
				continue
			}
			if weakAgent && lock.Key.Canonical() == agent.Canonical() {
				lock.Mode = f.Shared
			}
			locks = append(locks, lock)
		}
		var rejected error
		result = v.tx(t, locks, func(ctx context.Context, tx f.Tx, _ postgres.SQLExecutor) error {
			rejected = v.authority.RequireAgentUnassignedInTx(ctx, tx, actor, project, v.unassigned)
			return rejected
		})
		if result.State() != f.NotCommitted || !workAssigneeCode(rejected, f.DependencyUnavailable) || postgres.CodeOf(rejected) != postgres.LockNotHeld {
			t.Fatal("actual missing or weak transaction lock was not rejected")
		}
	}
	// Same DSN is not same Store. Keep this real second adapter and its original
	// callback/rollback alive until the wrong-Store check has actually returned.
	foreign := openStore(t, v.base.db.Config(t, nil))
	var rejected error
	result = foreign.WithinTx(ctxFor(t), cause(t), func(ctx context.Context, tx f.Tx) error {
		if err := foreign.AcquireAll(ctx, tx, full); err != nil {
			return err
		}
		rejected = v.authority.RequireAgentUnassignedInTx(ctx, tx, actor, project, v.unassigned)
		return rejected
	})
	if result.State() != f.NotCommitted || !workAssigneeCode(rejected, f.DependencyUnavailable) || postgres.CodeOf(rejected) != postgres.InvalidTransaction {
		t.Fatal("foreign live Store transaction was accepted by Work")
	}
	var expired f.Tx
	workAssigneeCommitted(t, v.tx(t, full, func(_ context.Context, tx f.Tx, _ postgres.SQLExecutor) error { expired = tx; return nil }))
	rejected = v.authority.RequireAgentUnassignedInTx(ctxFor(t), expired, actor, project, v.unassigned)
	// The real adapter removes returned callback tokens from its live table.
	if !workAssigneeCode(rejected, f.DependencyUnavailable) || postgres.CodeOf(rejected) != postgres.InvalidTransaction {
		t.Fatal("returned original transaction loan remained usable")
	}
	if v.snapshot(t) != before {
		t.Fatal("scope or transaction rejection changed original business facts")
	}
	t.Log("work_agent_assignee_references same_store_required=true expired_loan_rejected=true schedule_missing_rejected=true agent_shared_rejected=true actual_owner_denied=true work_nil_not_agent_existence=true")
}

type workAssigneeBackend struct {
	PID     int32
	Started time.Time
}

type workAssigneeCall struct {
	done    chan struct{}
	result  f.CommitResult
	backend workAssigneeBackend
}

func workAssigneeReadBackend(ctx context.Context, x postgres.SQLExecutor) (workAssigneeBackend, error) {
	var backend workAssigneeBackend
	err := x.QueryRow(ctx, `SELECT pg_backend_pid(),xact_start FROM pg_stat_activity WHERE pid=pg_backend_pid()`).Scan(&backend.PID, &backend.Started)
	if err == nil && (backend.PID <= 0 || backend.Started.IsZero()) {
		err = errors.New("original caller backend identity unavailable")
	}
	return backend, err
}

func testWorkAssigneeCallerReturn(t *testing.T) {
	v := newWorkAssigneeFixture(t)
	before := v.snapshot(t)
	actor, project, store := v.base.ownerBrowser.actor, v.base.project.ID, v.base.tracked
	locksA := workAssigneeLocks(t, actor, project, v.agentID)
	locksB := workAssigneeLocks(t, actor, project, v.unassigned)
	schedule, err := f.ProjectScheduleLock(project.String())
	workAssigneeRequire(t, err)
	causeA, causeB := cause(t), cause(t)
	a := &workAssigneeCall{done: make(chan struct{})}
	b := &workAssigneeCall{done: make(chan struct{})}
	held, bReady, releaseCommit, releaseReturn := make(chan struct{}), make(chan struct{}), make(chan struct{}), make(chan struct{})
	physical := make(chan f.CommitResult, 1)
	var commitOnce, returnOnce sync.Once
	allowCommit := func() { commitOnce.Do(func() { close(releaseCommit) }) }
	allowReturn := func() { returnOnce.Do(func() { close(releaseReturn) }) }
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	var started []*workAssigneeCall
	var bEntered atomic.Bool
	isA := func(c f.TransactionCause) bool {
		return c.Kind() == f.RecoveryCause && c.Details().Owner == causeA.Details().Owner && c.Details().RecoveryRunID == causeA.Details().RecoveryRunID
	}
	store.mu.Lock()
	clean := store.after == nil && store.afterResult == nil && store.transformResult == nil && store.beforeLocks == nil
	store.mu.Unlock()
	if !clean {
		cancel()
		t.Fatal("original fixture observation hooks were already owned")
	}
	// Cleanup runs before every inherited owner/Store/database cleanup. Failure
	// retains the graph after a safe diagnostic; it does not pretend that a
	// local timeout joined a call or refresh the original whole-run deadline.
	t.Cleanup(func() {
		allowCommit()
		allowReturn()
		cancel()
		joined, end := context.WithTimeout(context.Background(), 3*time.Second)
		defer end()
		for _, call := range started {
			select {
			case <-call.done:
			case <-joined.Done():
				t.Error("work assignee caller cleanup still owns its actual return; retaining fixture until original return or whole-run deadline")
				<-call.done
			}
		}
		store.mu.Lock()
		store.after, store.afterResult = nil, nil
		store.mu.Unlock()
	})
	store.mu.Lock()
	store.after = func(callCtx context.Context, _ f.Tx, c f.TransactionCause) error {
		if !isA(c) {
			return nil
		}
		close(held) // Original business callback returned; its real Tx is live.
		select {
		case <-releaseCommit:
			return nil
		case <-callCtx.Done():
			return callCtx.Err()
		}
	}
	store.afterResult = func(c f.TransactionCause, result f.CommitResult) {
		if isA(c) {
			physical <- result // Actual delegate return, without ack masking.
			<-releaseReturn
		}
	}
	store.mu.Unlock()
	started = append(started, a)
	go func() {
		defer close(a.done)
		a.result = store.WithinTx(ctx, causeA, func(ctx context.Context, tx f.Tx) error {
			if err := store.AcquireAll(ctx, tx, locksA); err != nil {
				return err
			}
			x, err := store.InTx(tx)
			if err != nil {
				return err
			}
			a.backend, err = workAssigneeReadBackend(ctx, x)
			if err != nil {
				return err
			}
			if !workAssigneeCode(v.current(ctx, tx, project, v.agentID), f.ResourceBusy) {
				return errors.New("readonly caller A lost the actual assignee predicate")
			}
			return nil
		})
	}()
	workAssigneeReady(t, ctx, held, a.done, "caller A original transaction barrier")
	started = append(started, b)
	go func() {
		defer close(b.done)
		b.result = store.WithinTx(ctx, causeB, func(ctx context.Context, tx f.Tx) error {
			x, err := store.InTx(tx)
			if err != nil {
				return err
			}
			b.backend, err = workAssigneeReadBackend(ctx, x)
			if err != nil {
				return err
			}
			close(bReady)
			// This caller acquires; the new Work port only RequireHeldLocks.
			if err = store.AcquireAll(ctx, tx, locksB); err != nil {
				return err
			}
			bEntered.Store(true)
			return v.current(ctx, tx, project, v.unassigned)
		})
	}()
	workAssigneeReady(t, ctx, bReady, b.done, "caller B original backend")
	workAssigneeWaitLock(t, ctx, v.base.raw, schedule, a.backend, b.backend, a.done, b.done)
	if bEntered.Load() {
		t.Fatal("caller B entered the port before acquiring the blocked Schedule lock")
	}
	select {
	case <-physical:
		t.Fatal("caller A physically returned before its original transaction barrier was released")
	default:
	}
	allowCommit()
	wait, finish := context.WithTimeout(ctx, 3*time.Second)
	defer finish()
	select {
	case actual := <-physical:
		workAssigneeCommitted(t, actual)
	case <-wait.Done():
		t.Fatal("caller A physical COMMIT did not actually return")
	}
	workAssigneeReady(t, ctx, b.done, nil, "caller B original Store return")
	workAssigneeCommitted(t, b.result)
	if !bEntered.Load() {
		t.Fatal("caller B returned without its real same-transaction check")
	}
	select {
	case <-a.done:
		t.Fatal("caller A wrapper returned while its original return barrier remained held")
	default:
	}
	// Both callbacks are real reads. B's success while A is still held proves
	// neither A's actual return nor any Scheduler/Execution/retirement Join.
	allowReturn()
	workAssigneeReady(t, ctx, a.done, nil, "caller A original Store return")
	workAssigneeCommitted(t, a.result)
	if v.snapshot(t) != before {
		t.Fatal("readonly caller competition changed original business facts")
	}
	t.Log("work_agent_assignee_references readonly_callers=2 exact_schedule_wait=true port_entered_after_acquire=true physical_commit_before_original_return=true peer_returned_while_original_held=true both_original_returns=true task_writer_or_join_claim=false")
}

func workAssigneeReady(t *testing.T, parent context.Context, ready, returned <-chan struct{}, stage string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(parent, 3*time.Second)
	defer cancel()
	select {
	case <-ready:
	case <-returned:
		t.Fatal(stage + " ended before readiness")
	case <-ctx.Done():
		t.Fatal(stage + " readiness exceeded the original budget")
	}
}

func workAssigneeWaitLock(t *testing.T, parent context.Context, store *postgres.Store, key f.LockKey, a, b workAssigneeBackend, aDone, bDone <-chan struct{}) {
	t.Helper()
	if a.PID == b.PID {
		t.Fatal("two live original transactions reused one backend")
	}
	ctx, cancel := context.WithTimeout(parent, 3*time.Second)
	defer cancel()
	tick := time.NewTicker(5 * time.Millisecond)
	defer tick.Stop()
	bits := uint64(key.AdvisoryKey())
	for {
		var waiting bool
		err := store.QueryRow(ctx, `SELECT EXISTS(
 SELECT 1 FROM pg_locks w JOIN pg_locks h ON h.locktype=w.locktype AND h.database=w.database AND h.classid=w.classid AND h.objid=w.objid AND h.objsubid=w.objsubid
 JOIN pg_stat_activity waiter ON waiter.pid=w.pid JOIN pg_stat_activity holder ON holder.pid=h.pid
 WHERE w.locktype='advisory' AND w.classid::bigint=$1 AND w.objid::bigint=$2 AND w.objsubid=1
 AND w.pid=$3 AND NOT w.granted AND w.mode='ExclusiveLock' AND h.pid=$4 AND h.granted AND h.mode='ExclusiveLock'
 AND waiter.datname=current_database() AND holder.datname=current_database()
 AND waiter.application_name='agenteam' AND holder.application_name='agenteam'
 AND waiter.xact_start=$5 AND holder.xact_start=$6 AND waiter.state='active'
 AND waiter.query='SELECT pg_advisory_xact_lock($1)' AND $4::int=ANY(pg_blocking_pids(w.pid)))`, int64(bits>>32), int64(bits&0xffffffff), b.PID, a.PID, b.Started, a.Started).Scan(&waiting)
		workAssigneeRequire(t, err)
		if waiting {
			return
		}
		select {
		case <-tick.C:
		case <-aDone:
			t.Fatal("caller A returned before exact Schedule lock observation")
		case <-bDone:
			t.Fatal("caller B returned before exact Schedule lock observation")
		case <-ctx.Done():
			t.Fatal("exact original Schedule lock waiter was not observed")
		}
	}
}
