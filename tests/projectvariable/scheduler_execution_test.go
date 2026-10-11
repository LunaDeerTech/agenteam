//go:build integration

package projectvariable_test

import (
	"bytes"
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/execution"
	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/scheduler"
	"github.com/LunaDeerTech/agenteam/internal/central/work"
	wc "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
)

// Both paths use the original Execution authority, real preparation providers,
// scoped Model calls, Usage, Secret and owned HTTPS fixture. The Scheduler alone
// creates the Claim/Launch. SQL below observes facts; it never seeds them.
func TestSchedulerExecution(t *testing.T) {
	t.Run("historical-association-terminal-and-dedup", func(t *testing.T) {
		x := newSchedulerExecutionFixture(t, false)
		legacy := x.runner(t, false)
		launched, err := legacy.RunTraversal(ctxFor(t))
		firstRoundRequire(t, err)
		if len(launched.Visits) != 1 || launched.Visits[0].Err != nil || len(launched.Executions) != 0 {
			t.Fatal("original claim/Launch-only runner did not produce exactly one undelivered association")
		}
		dispatch := launched.Visits[0].Dispatch
		x.bindOriginal(t, dispatch)
		counts, err := captureInputCounts(ctxFor(t), x.round.capture.v.base.raw, x.round.capture.created.ID.String())
		firstRoundRequire(t, err)
		if counts != ([11]int64{}) {
			t.Fatal("association-only setup captured or executed before the executor was started")
		}
		x.round.requireWire(t, 0, true)
		stopSchedulerExecutionRunner(t, legacy)

		x.start(t)
		runner := x.runner(t, true)
		page, err := runner.RunTraversal(ctxFor(t))
		firstRoundRequire(t, err)
		x.requireDelivery(t, page, dispatch)
		original := x.associated(t, dispatch)
		for n := 0; n < 2; n++ {
			observed, err := x.executor.Advance(ctxFor(t), x.round.capture.v.base.project.ID, original)
			firstRoundRequire(t, err)
			if observed.ExecutionID != original.ExecutionID || observed.ProjectID != x.round.capture.v.base.project.ID {
				t.Fatal("repeated exact association changed the original Execution identity")
			}
		}
		x.awaitTerminal(t, ec.Succeeded)
		stopSchedulerExecutionRunner(t, runner)
		x.stop(t)
		// The repeated observations must preserve one actual preparation,
		// Round, Invocation and HTTPS request, not just a terminal status.
		x.requireTerminal(t, ec.Succeeded)
	})
	t.Run("asynchronous-todo-and-cancel-join", func(t *testing.T) {
		x := newSchedulerExecutionFixture(t, true)
		v := x.round.capture.v
		next := createRunnerTodo(t, v, "executor-next-todo", wc.TaskPriorityLow)
		setRunnerLimit(t, v, 1)
		x.start(t)
		runner := x.runner(t, true)
		type result struct {
			value scheduler.ProjectRunResult
			err   error
		}
		done := make(chan result, 1)
		traversal, cancel := context.WithCancel(ctxFor(t))
		defer cancel()
		go func() {
			value, err := runner.RunTraversal(traversal)
			done <- result{value, err}
		}()
		x.round.requireWire(t, 1, false)
		var observed result
		select {
		case observed = <-done:
		case <-time.After(3 * time.Second):
			cancel()
			stopSchedulerExecutionRunner(t, runner)
			select {
			case <-done:
			case <-time.After(time.Second):
				t.Error("original traversal did not return after its actual Drain")
			}
			t.Fatal("Scheduler waited for the held Model response instead of returning after admission")
		}
		firstRoundRequire(t, observed.err)
		if len(observed.value.Visits) != 2 || observed.value.Visits[0].TaskID != v.task.ID || observed.value.Visits[0].Err != nil || observed.value.Visits[1].TaskID != next.ID || observed.value.Visits[1].Action != scheduler.ProjectVisitClaim {
			t.Fatal("asynchronous delivery prevented the original ordered traversal from visiting its next todo")
		}
		requireCode(t, observed.value.Visits[1].Err, f.ResourceBusy)
		dispatch := observed.value.Visits[0].Dispatch
		x.bindOriginal(t, dispatch)
		x.requireDelivery(t, observed.value, dispatch)
		current, err := v.taskReader.GetTask(ctxFor(t), v.base.ownerBrowser.actor, v.base.project.ID, next.ID)
		firstRoundRequire(t, err)
		if !reflect.DeepEqual(current, next) {
			t.Fatal("full capacity changed the next todo while the first actual Model call was still active")
		}
		cancel() // Admission's context is not the executor's lifetime.
		stopSchedulerExecutionRunner(t, runner)
		x.round.requireWire(t, 1, false)
		x.stop(t) // Stop/Drain must cancel and join the SAME Model/Loop call.
		x.requireTerminal(t, ec.Cancelled)
	})
}

type schedulerExecutionFixture struct {
	round    *firstRoundFixture
	executor *execution.AssociatedExecutor
	handoff  *scheduler.LaunchHandoff
	launcher *countedSchedulerExecution
	visitor  *scheduler.PendingVisitor
	runDone  chan error
	cancel   context.CancelFunc
	started  bool
	stopped  sync.Once
}

func newSchedulerExecutionFixture(t *testing.T, held bool) *schedulerExecutionFixture {
	t.Helper()
	x := &schedulerExecutionFixture{round: newFirstRoundFixtureWithSource(t, held, true)}
	c := x.round.capture
	v := c.v
	x.handoff, x.launcher = newSchedulerLaunchHandoff(t, v, true)
	var err error
	x.visitor, err = scheduler.NewPendingVisitorWithFailure(v.pending, x.handoff, newBusyCompensator(t, v), newSchedulerFailureFinalizer(t, v))
	firstRoundRequire(t, err)
	t.Cleanup(func() {
		x.visitor.Stop()
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := x.visitor.Drain(ctx); err != nil || !x.visitor.Joined() {
			t.Error("original pending visitor did not join", err)
		}
	})
	// Deliberately bypass only the old test observation taps, whose mutable
	// single-Execution fields are filled after Launch. Every dependency here
	// is the SAME real provider and Execution authority created by the graph.
	prepare, err := execution.NewPreparationDriver(v.base.tracked, c.authority, execution.PreparationDependencies{
		Agents: c.configuration, Projects: v.base.projectAuthority, Processes: captureProviderProcesses{v.agent.guard},
		Task: c.task, Skills: c.skills.provider, Tools: c.tools.provider, Models: c.models.provider,
		Environment: c.environment.provider, Mounts: c.mounts.provider,
	})
	firstRoundRequire(t, err)
	t.Cleanup(func() {
		prepare.Stop()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := prepare.Drain(ctx); err != nil || !prepare.Joined() {
			t.Error("original preparation calls did not join", err)
		}
	})
	direct := x.round.directDriver(t)
	x.executor, err = execution.NewAssociatedExecutor(prepare, direct, execution.AssociatedExecutorOptions{MaxOwned: 1, RecoveryInterval: 20 * time.Millisecond})
	firstRoundRequire(t, err)
	t.Cleanup(func() { x.stop(t) })
	return x
}

func (x *schedulerExecutionFixture) start(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithCancel(ctxFor(t))
	x.cancel, x.runDone, x.started = cancel, make(chan error, 1), true
	go func() { x.runDone <- x.executor.Run(ctx) }()
	select {
	case <-x.executor.Ready():
	case err := <-x.runDone:
		x.started = false
		t.Fatal("executor returned before installing its own lifetime", err)
	case <-ctx.Done():
		t.Fatal("executor did not become ready within the original test budget")
	}
}

func (x *schedulerExecutionFixture) stop(t *testing.T) {
	t.Helper()
	x.stopped.Do(func() {
		x.executor.Stop()
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if x.cancel != nil {
			defer x.cancel()
		}
		if err := x.executor.Drain(ctx); err != nil || !x.executor.Joined() {
			t.Error("original asynchronous Execution owner did not join", err)
		}
		if x.started {
			select {
			case err := <-x.runDone:
				if err != nil && !errors.Is(err, context.Canceled) {
					t.Error("original executor Run returned an unresolved owner", err)
				}
			case <-ctx.Done():
				t.Error("original executor Run did not actually return after Stop/Drain")
			}
		}
	})
}

func (x *schedulerExecutionFixture) runner(t *testing.T, execute bool) *scheduler.ProjectRunner {
	t.Helper()
	c := x.round.capture
	reader, err := work.NewSchedulerTaskReader(c.v.base.tracked, c.v.authority)
	firstRoundRequire(t, err)
	options := scheduler.ProjectRunnerOptions{ProjectID: c.v.base.project.ID, TickInterval: 20 * time.Millisecond, LaunchPolicy: c.launchPolicy, Tasks: reader}
	var runner *scheduler.ProjectRunner
	if execute {
		runner, err = scheduler.NewProjectRunnerWithExecutions(c.claims, x.visitor, options, x.executor, 1)
	} else {
		runner, err = scheduler.NewProjectRunner(c.claims, x.visitor, options)
	}
	firstRoundRequire(t, err)
	t.Cleanup(func() { stopSchedulerExecutionRunner(t, runner) })
	return runner
}

func stopSchedulerExecutionRunner(t *testing.T, runner *scheduler.ProjectRunner) {
	t.Helper()
	runner.Stop()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := runner.Drain(ctx); err != nil || !runner.Joined() {
		t.Error("original Scheduler traversal did not join", err)
	}
}

func (x *schedulerExecutionFixture) bindOriginal(t *testing.T, dispatch scheduler.Dispatch) {
	t.Helper()
	launches, _, created := x.launcher.observed()
	s := dispatch.Summary()
	request, err := dispatch.LaunchRequest()
	firstRoundRequire(t, err)
	if launches != 1 || created.Status != ec.Created || s.Status != scheduler.Launched || s.ExecutionID == nil || *s.ExecutionID != created.ID || request.Trigger.TaskID != x.round.capture.v.task.ID.String() {
		t.Fatal("runner did not preserve its one real Launch and committed association")
	}
	// These fields are used only by post-call assertions. Async preparation
	// uses raw real providers and never reads or writes this fixture record.
	x.round.capture.created, x.round.capture.request = created, request
}

func (x *schedulerExecutionFixture) associated(t *testing.T, dispatch scheduler.Dispatch) ec.AssociatedDispatch {
	t.Helper()
	s := dispatch.Summary()
	request, err := dispatch.LaunchRequest()
	firstRoundRequire(t, err)
	digest, err := request.Digest()
	firstRoundRequire(t, err)
	if s.ExecutionID == nil {
		t.Fatal("unassociated Dispatch cannot be delivered")
	}
	return ec.AssociatedDispatch{ExecutionID: *s.ExecutionID, AgentID: s.AgentID, Key: request.Meta.IdempotencyKey, Digest: digest, DispatchID: s.ID.String()}
}

func (x *schedulerExecutionFixture) requireDelivery(t *testing.T, result scheduler.ProjectRunResult, dispatch scheduler.Dispatch) {
	t.Helper()
	if len(result.Executions) != 1 {
		t.Fatal("bounded traversal did not deliver exactly one reliable association")
	}
	v := result.Executions[0]
	s := dispatch.Summary()
	if v.Err != nil || s.ExecutionID == nil || v.ExecutionID != *s.ExecutionID || v.DispatchID != s.ID || v.Execution.ID != *s.ExecutionID || v.Advance.ExecutionID != *s.ExecutionID || v.Advance.ProjectID != s.ProjectID {
		t.Fatal("delivery lost canonical original Execution identity or admission", v.Err)
	}
}

func (x *schedulerExecutionFixture) awaitTerminal(t *testing.T, want ec.Status) {
	t.Helper()
	ctx, cancel := context.WithTimeout(ctxFor(t), 10*time.Second)
	defer cancel()
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	c := x.round.capture
	for {
		var state string
		err := c.v.base.raw.QueryRow(ctx, `SELECT status FROM agenteam_execution.executions
 WHERE id=$1 AND project_id=$2 AND agent_id=$3`, c.created.ID.String(), c.created.ProjectID.String(), c.created.AgentID.String()).Scan(&state)
		firstRoundRequire(t, err)
		if state == string(want) {
			return
		}
		select {
		case <-tick.C:
		case <-ctx.Done():
			t.Fatal("original admitted Execution did not reach its observed terminal phase")
		}
	}
}

func (x *schedulerExecutionFixture) requireTerminal(t *testing.T, status ec.Status) {
	t.Helper()
	c := x.round.capture
	var snapshotRaw, roundRaw, inputRaw []byte
	var actual, inputDigest string
	var version, through, attempts, claims, dispatches, executions int64
	err := c.v.base.raw.QueryRow(ctxFor(t), `SELECT e.status,e.version,r.transcript_through,s.content,r.content,p.input,p.input_digest,
 (SELECT count(*) FROM agenteam_execution.preparation_attempts a WHERE a.execution_id=e.id),
 (SELECT count(*) FROM agenteam_work.task_scheduler_claims w WHERE w.project_id=e.project_id::uuid),
 (SELECT count(*) FROM agenteam_scheduler.dispatches d WHERE d.project_id=e.project_id),
 (SELECT count(*) FROM agenteam_execution.executions n WHERE n.project_id=e.project_id)
 FROM agenteam_execution.executions e
 JOIN agenteam_execution.preparation_inputs p ON(p.execution_id,p.project_id,p.agent_id)=(e.id,e.project_id,e.agent_id)
 JOIN agenteam_execution.snapshots s ON(s.execution_id,s.id)=(e.id,e.snapshot_id)
 JOIN agenteam_execution.rounds r ON(r.execution_id,r.snapshot_id,r.start_id)=(e.id,s.id,s.start_id)
 WHERE e.id=$1 AND e.project_id=$2 AND e.agent_id=$3 AND e.status=$4
 AND r.terminal_status=e.status AND r.terminal_version=e.version AND r.finished_at=e.completed_at`,
		c.created.ID.String(), c.created.ProjectID.String(), c.created.AgentID.String(), string(status)).
		Scan(&actual, &version, &through, &snapshotRaw, &roundRaw, &inputRaw, &inputDigest, &attempts, &claims, &dispatches, &executions)
	firstRoundRequire(t, err)
	snapshot, err := ec.DecodeDirectTextSnapshot(snapshotRaw)
	firstRoundRequire(t, err)
	round, err := ec.DecodeDirectTextRound(roundRaw)
	firstRoundRequire(t, err)
	input, err := ec.DecodePreparationInput(inputRaw)
	firstRoundRequire(t, err)
	if actual != string(status) || attempts != 1 || claims != 1 || dispatches != 1 || executions != 1 || inputDigest != string(input.Digest()) || !bytes.Equal(snapshot.Fields().Context.Input().CanonicalBytes(), input.CanonicalBytes()) || !input.Fields().Request.Equal(ec.PreparationRequest{ExecutionID: c.created.ID, Launch: c.request}) {
		t.Fatal("asynchronous execution duplicated work or changed the original committed input/association")
	}
	sv, rv := snapshot.Fields(), round.Fields()
	// This is an observation decoded from the real terminal rows, not a
	// fabricated Start return or permission to execute/retire anything.
	observed := ec.DirectTextReceipt{ExecutionID: c.created.ID, StartID: sv.StartID, SnapshotID: sv.ID, RoundID: rv.ID, CallID: rv.CallID,
		Status: status, Version: f.Version(version), TranscriptThrough: f.Sequence(through)}
	x.round.requireTerminal(t, sv.Context, observed, status)
}
