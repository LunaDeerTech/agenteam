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
	"github.com/LunaDeerTech/agenteam/internal/central/execution"
	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/model"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/project"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/scheduler"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/work"
	wc "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
)

// Accepted Delete is the existing controlled prerequisite, not BeginDelete.
// The original bound-guard StopDriver issues the live claim/scope. Both Projects
// use ONE actual Executor/Store, real preparation providers and scoped Model.
// Unknown is an in-process acknowledgement mask AFTER real COMMIT; it does not
// model network loss. No foreign terminal, full Registry or Tool loop is claimed.
func TestExecutionProjectStop(t *testing.T) {
	t.Run("project-isolation-and-original-model-return", testExecutionStopIsolation)
	t.Run("original-preparation-unknown-retained", testExecutionStopUnknown)
}

type executionStopFixture struct {
	x         *schedulerExecutionFixture
	prepare   *execution.PreparationDriver
	b         *taskTransitionFixture
	driver    *project.LifecycleStopDriver
	stopper   *execution.ProjectStopper
	lifecycle *schedulerStopFixture // Only accepted/claim SQL helpers; never its idle manager.
	done      chan struct{}
	driverErr error
	cancel    context.CancelFunc
	barriers  []*schedulerStopBarrier
	closed    sync.Once
}

func newExecutionStopFixture(t *testing.T, withB bool) *executionStopFixture {
	t.Helper()
	s := &executionStopFixture{x: &schedulerExecutionFixture{round: newFirstRoundFixtureWithSource(t, true, true)}}
	x, c := s.x, s.x.round.capture
	v := c.v
	x.handoff, x.launcher = newSchedulerLaunchHandoff(t, v, true)
	var err error
	x.visitor, err = scheduler.NewPendingVisitorWithFailure(v.pending, x.handoff, newBusyCompensator(t, v), newSchedulerFailureFinalizer(t, v))
	firstRoundRequire(t, err)
	s.prepare, err = execution.NewPreparationDriver(v.base.tracked, c.authority, execution.PreparationDependencies{
		Agents: c.configuration, Projects: v.base.projectAuthority, Processes: captureProviderProcesses{v.agent.guard},
		Task: c.task, Skills: c.skills.provider, Tools: c.tools.provider, Models: c.models.provider, Environment: c.environment.provider, Mounts: c.mounts.provider,
	})
	firstRoundRequire(t, err)
	direct := x.round.directDriver(t)
	x.executor, err = execution.NewAssociatedExecutor(s.prepare, direct, execution.AssociatedExecutorOptions{MaxOwned: 2, RecoveryInterval: 20 * time.Millisecond})
	firstRoundRequire(t, err)
	if withB {
		s.b = s.secondProject(t)
	}
	// Registered last: release/actual returns precede inherited resource cleanup.
	t.Cleanup(func() { s.close(t) })
	return s
}

func (s *executionStopFixture) secondProject(t *testing.T) *taskTransitionFixture {
	t.Helper()
	original := s.x.round.capture.v
	ref := createManagerProject(t, original.agent.p2, "execution-stop-b")
	base := *original.base
	base.project = ref
	p2 := *original.agent.p2
	p2.base = &base
	p2.project = ref
	a := *original.agent
	a.p2 = &p2
	a.modelID = a.createModel(t, fixtureProcess{captureProviderProcesses{a.guard}.CurrentProcess()})
	request, meta, _ := a.request(t)
	receipt, err := a.agents.CreateAgent(ctxFor(t), base.ownerBrowser.actor, meta, ref.ID, request)
	firstRoundRequire(t, err)
	v := *original
	v.agent = &a
	v.base = &base
	v.agentID = request.Fields().AgentID
	inject := false
	change, err := ac.NewAgentUpdate(ac.AgentUpdateFields{InjectAgentsMD: &inject})
	firstRoundRequire(t, err)
	version := receipt.Fields().Agent.Fields().Core.Version
	_, err = a.agents.UpdateAgent(ctxFor(t), base.ownerBrowser.actor, executionStopMeta(t, &version), ref.ID, v.agentID, change)
	firstRoundRequire(t, err)
	milestone := wc.CreateMilestoneRequest{MilestoneID: id[wc.Milestone](t), Title: "Execution stop B", Description: "Same real Store"}
	_, err = v.structure.CreateMilestone(ctxFor(t), base.ownerBrowser.actor, executionStopMeta(t, nil), ref.ID, milestone)
	firstRoundRequire(t, err)
	sprint := wc.CreateSprintRequest{SprintID: id[pc.Sprint](t), MilestoneID: milestone.MilestoneID, Title: "Execution stop B", Description: "Formal current Sprint"}
	_, err = v.structure.CreateSprint(ctxFor(t), base.ownerBrowser.actor, executionStopMeta(t, nil), ref.ID, sprint)
	firstRoundRequire(t, err)
	created, err := v.tasks.CreateTask(ctxFor(t), base.ownerBrowser.actor, executionStopMeta(t, nil), ref.ID, wc.TaskCreate{TaskID: id[wc.Task](t), SprintID: sprint.SprintID, Title: "Execute B", Description: "Continue while A stops", Type: wc.TaskTypeTask, Priority: wc.TaskPriorityMedium, Plan: "Return the bounded fixture answer"})
	firstRoundRequire(t, err)
	v.task = created.Task
	v.configureProject(t)
	transfer, command, _ := v.transferRequest(t)
	moved, err := v.transitions.TransferTask(ctxFor(t), base.ownerBrowser.actor, command, ref.ID, v.task.ID, transfer)
	firstRoundRequire(t, err)
	v.task = moved.Task
	startSchedulerSprint(t, &v)
	models, _, _ := newCaptureModelResolverWithPorts(t, &v, s.x.round.capture.authority, captureModelPorts{
		projects: s.x.round.projects, auditProjects: s.x.round.auditProjects,
		usageFactory: func(authority *model.Authority) (sc.UsageAuthority, error) {
			return model.NewRuntimeSecretUsageRouter(authority, s.x.round.runtimeAuthority, base.accounts)
		},
	})
	current, err := models.GetProjectModel(ctxFor(t), base.ownerBrowser.actor, ref.ID, a.modelID)
	firstRoundRequire(t, err)
	provider, err := models.GetProjectProvider(ctxFor(t), base.ownerBrowser.actor, ref.ID, current.ProviderID)
	firstRoundRequire(t, err)
	input := provider.Input.Clone()
	input.BaseURL = "https://fixture.test:8443/case/" + s.x.round.scenario
	scope, err := i.InProject(ref.ID)
	firstRoundRequire(t, err)
	_, err = models.UpdateProvider(ctxFor(t), mc.UpdateProviderRequest{CommandMeta: mc.CommandMeta{Actor: base.ownerBrowser.actor, Scope: scope, Key: f.IdempotencyKey(id[struct{}](t).String())}, ID: provider.ID, ExpectedVersion: provider.Version, Input: input})
	firstRoundRequire(t, err)
	return &v
}
func executionStopMeta(t *testing.T, version *f.Version) f.CommandMeta {
	return meta(t, id[struct{}](t).String(), version)
}

func (s *executionStopFixture) launch(t *testing.T, v *taskTransitionFixture) ec.AssociatedDispatch {
	t.Helper()
	c := s.x.round.capture
	reader, err := work.NewSchedulerTaskReader(v.base.tracked, v.authority)
	firstRoundRequire(t, err)
	runner, err := scheduler.NewProjectRunnerWithExecutions(c.claims, s.x.visitor, scheduler.ProjectRunnerOptions{ProjectID: v.base.project.ID, TickInterval: 20 * time.Millisecond, LaunchPolicy: c.launchPolicy, Tasks: reader}, s.x.executor, 1)
	firstRoundRequire(t, err)
	defer stopSchedulerExecutionRunner(t, runner)
	page, err := runner.RunTraversal(ctxFor(t))
	firstRoundRequire(t, err)
	if len(page.Visits) != 1 || page.Visits[0].Err != nil || len(page.Executions) != 1 || page.Executions[0].Err != nil {
		t.Fatal("real Scheduler did not admit exactly its original association")
	}
	dispatch := page.Visits[0].Dispatch
	if v == c.v {
		s.x.bindOriginal(t, dispatch)
	}
	request, err := dispatch.LaunchRequest()
	firstRoundRequire(t, err)
	summary := dispatch.Summary()
	digest, err := request.Digest()
	firstRoundRequire(t, err)
	return ec.AssociatedDispatch{ExecutionID: page.Executions[0].ExecutionID, AgentID: v.agentID, Key: request.Meta.IdempotencyKey, Digest: digest, DispatchID: summary.ID.String()}
}

func (s *executionStopFixture) setupDriver(t *testing.T, step func(context.Context, i.Actor, pc.LifecycleCause, pc.ScopeRef) error) {
	t.Helper()
	v := s.x.round.capture.v
	current, err := v.base.projects.GetProject(ctxFor(t), v.base.ownerBrowser.actor, v.base.project.ID)
	firstRoundRequire(t, err)
	s.lifecycle = &schedulerStopFixture{v: v, a: current, manifest: lifecycleManifest(t), cause: pc.LifecycleCause{OperationID: id[pc.Operation](t), Action: pc.Delete, ProjectVersion: current.Version + 1}}
	authority, err := project.NewLifecycleAuthority(v.base.tracked, s.lifecycle.manifest, nil)
	firstRoundRequire(t, err)
	s.driver, err = project.NewLifecycleStopDriver(v.base.tracked, authority, captureProviderProcesses{v.agent.guard}, step)
	firstRoundRequire(t, err)
	s.stopper, err = execution.NewProjectStopper(s.x.executor, s.driver)
	firstRoundRequire(t, err)
}
func (s *executionStopFixture) startDriver(t *testing.T) {
	s.lifecycle.acceptDelete(t)
	ctx, cancel := context.WithCancel(ctxFor(t))
	s.cancel = cancel
	s.done = make(chan struct{})
	go func() {
		defer close(s.done)
		s.driverErr = s.driver.Run(ctx, s.lifecycle.a.ID, s.lifecycle.cause.OperationID)
	}()
}
func (s *executionStopFixture) barrier() *schedulerStopBarrier {
	b := newSchedulerStopBarrier()
	s.barriers = append(s.barriers, b)
	return b
}
func (s *executionStopFixture) authorization(cause f.TransactionCause) bool {
	d := cause.Details()
	return d.Kind == f.RecoveryCause && d.Owner == "execution-project-stop" && d.RecoveryRunID == s.lifecycle.cause.OperationID.String() && d.CheckpointRef == s.lifecycle.a.ID.String()
}

func executionStopReport(r execution.ProjectStopReport, cause pc.LifecycleCause, scope pc.ScopeRef, joined bool) error {
	d := r.Details()
	if !r.Valid() || !r.Matches(cause, scope) || d.LocalJoined != joined || d.PendingAdmissions != 0 || d.Retained || joined && d.PendingJobs != 0 || !joined && d.PendingJobs != 1 {
		return errors.New("local Execution report differs from original owner return state")
	}
	return nil
}
func executionStopWait(t *testing.T, fn func() bool, label string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(ctxFor(t), 5*time.Second)
	defer cancel()
	tick := time.NewTicker(5 * time.Millisecond)
	defer tick.Stop()
	for !fn() {
		select {
		case <-tick.C:
		case <-ctx.Done():
			t.Fatal("Execution stop readiness not reached: " + label)
		}
	}
}
func (s *executionStopFixture) wire(t *testing.T, requests, active int) {
	t.Helper()
	executionStopWait(t, func() bool {
		state, err := s.x.round.network.State(ctxFor(t), s.x.round.scenario)
		firstRoundRequire(t, err)
		return len(state.Requests) == requests && state.ActiveHandlers == active
	}, "original scoped wire")
}
func (s *executionStopFixture) terminal(t *testing.T, p i.ProjectID, e i.ExecutionID, status ec.Status) {
	t.Helper()
	executionStopWait(t, func() bool {
		var actual string
		err := s.x.round.capture.v.base.raw.QueryRow(ctxFor(t), `SELECT status FROM agenteam_execution.executions WHERE id=$1 AND project_id=$2`, e.String(), p.String()).Scan(&actual)
		firstRoundRequire(t, err)
		return actual == string(status)
	}, "original terminal checkpoint")
	var counts [6]int64
	err := s.x.round.capture.v.base.raw.QueryRow(ctxFor(t), `SELECT
 (SELECT count(*) FROM agenteam_execution.preparation_inputs WHERE execution_id=$1),
 (SELECT count(*) FROM agenteam_execution.preparation_attempts WHERE execution_id=$1 AND phase='terminal'),
 (SELECT count(*) FROM agenteam_execution.rounds r JOIN agenteam_execution.executions e ON e.id=r.execution_id WHERE e.id=$1 AND r.terminal_status=e.status AND r.terminal_version=e.version AND r.finished_at=e.completed_at),
 (SELECT count(*) FROM agenteam_model.invocations WHERE execution_id=$1 AND final_status=$2),
 (SELECT count(*) FROM agenteam_model.calls c JOIN agenteam_model.invocations v ON v.call_id=c.id WHERE v.execution_id=$1 AND c.phase=$2 AND c.retired),
 (SELECT count(*) FROM agenteam_model.calls c JOIN agenteam_model.invocations v ON v.call_id=c.id JOIN agenteam_secret.secret_leases l ON l.id=c.lease_id WHERE v.execution_id=$1 AND l.released)`, e.String(), string(status)).Scan(&counts[0], &counts[1], &counts[2], &counts[3], &counts[4], &counts[5])
	firstRoundRequire(t, err)
	if counts != ([6]int64{1, 1, 1, 1, 1, 1}) {
		t.Fatal("original preparation/round/Invocation terminal facts differ", counts)
	}
}

func testExecutionStopIsolation(t *testing.T) {
	s := newExecutionStopFixture(t, true)
	v := s.x.round.capture.v
	auth, modelReturn, resume := s.barrier(), s.barrier(), s.barrier()
	pending, unknownReturned := make(chan error, 1), make(chan error, 1)
	// CommittedResult exposes no physical AttemptID. This private ID names
	// only the acknowledgement mask returned by this wrapper.
	authAttempt := id[f.TransactionAttempt](t)
	var authMasked atomic.Bool
	var saved context.Context
	var actor i.Actor
	var cause pc.LifecycleCause
	var scope pc.ScopeRef
	s.setupDriver(t, func(ctx context.Context, a i.Actor, c pc.LifecycleCause, p pc.ScopeRef) error {
		saved, actor, cause, scope = ctx, a, c, p
		r, err := s.stopper.RequestStop(context.Background(), a, c, p)
		if r.Valid() || !executionStopFault(err, f.Forbidden) {
			return errors.New("public DTO forged the original issuer")
		}
		r, err = s.stopper.RequestStop(ctx, a, c, p)
		if r.Valid() || !executionStopUnknownFault(err, authAttempt) {
			err = errors.New("authorization Unknown lost its original acknowledgement identity")
		} else {
			err = nil
		}
		unknownReturned <- err
		if err != nil {
			return err
		}
		select {
		case <-resume.release:
		case <-ctx.Done():
			return ctx.Err()
		}
		r, err = s.stopper.RequestStop(ctx, a, c, p)
		if err == nil {
			err = executionStopReport(r, c, p, false)
		}
		pending <- err
		if err != nil {
			return err
		}
		r, err = s.stopper.DrainProject(ctx, a, c, p)
		if err != nil {
			return err
		}
		return executionStopReport(r, c, p, true)
	})
	var armed atomic.Bool
	store := v.base.tracked
	store.mu.Lock()
	store.afterResult = func(c f.TransactionCause, r f.CommitResult) {
		if s.authorization(c) {
			auth.hold(r)
		}
		d := c.Details()
		owners := d.Primary.OwnerIDs()
		if armed.Load() && r.State() == f.Committed && d.Kind == f.CommandsCause && d.Primary.Namespace() == "model.usage" && d.Primary.Command() == "call-ledger" && len(owners) == 1 && owners[0] == v.base.project.ID.String() {
			// Observe the actual terminal row independently; this is after COMMIT,
			// but the original synchronous Model call has not received its return.
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			var terminal bool
			err := v.base.raw.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_model.invocations WHERE project_id=$1 AND final_status='cancelled')`, v.base.project.ID.String()).Scan(&terminal)
			if err == nil && terminal {
				modelReturn.hold(r)
			}
		}
	}
	store.transformResult = func(c f.TransactionCause, r f.CommitResult) f.CommitResult {
		if s.authorization(c) && r.State() == f.Committed && authMasked.CompareAndSwap(false, true) {
			return f.UnknownResult(authAttempt, c)
		}
		return r
	}
	store.mu.Unlock()
	s.x.start(t)
	a := s.launch(t, v)
	b := s.launch(t, s.b)
	s.wire(t, 2, 2)
	armed.Store(true)
	s.startDriver(t)
	auth.await(t)
	s.lifecycle.requireClaim(t, "running")
	s.wire(t, 2, 2)
	select {
	case <-pending:
		t.Fatal("stop escaped held authorization return")
	default:
	}
	auth.open()
	schedulerStopReceive(t, s.done, unknownReturned, "original authorization Unknown")
	// Unlike a held callback that ignores cancellation, the real HTTPS handlers
	// and canonical call phase directly detect any premature cancellation.
	s.wire(t, 2, 2)
	var running int64
	err := v.base.raw.QueryRow(ctxFor(t), `SELECT count(*) FROM agenteam_execution.executions WHERE id IN ($1,$2) AND status='running'`, a.ExecutionID.String(), b.ExecutionID.String()).Scan(&running)
	firstRoundRequire(t, err)
	if running != 2 || !authMasked.Load() {
		t.Fatal("authorization Unknown stopped an original execution")
	}
	resume.open()
	schedulerStopReceive(t, s.done, pending, "original Execution RequestStop")
	modelReturn.await(t)
	s.wire(t, 2, 1)
	select {
	case <-s.done:
		t.Fatal("stop round joined before original Model return")
	default:
	}
	// B completes using its original call while A still owns its held return.
	firstRoundRequire(t, s.x.round.network.Release(ctxFor(t), s.x.round.scenario))
	s.terminal(t, s.b.base.project.ID, b.ExecutionID, ec.Succeeded)
	select {
	case <-s.done:
		t.Fatal("B completion falsely joined A")
	default:
	}
	modelReturn.open()
	schedulerStopWait(t, s.done, "original Execution stop round")
	firstRoundRequire(t, s.driverErr)
	s.lifecycle.requireClaim(t, "terminal")
	s.terminal(t, v.base.project.ID, a.ExecutionID, ec.Cancelled)
	r, err := s.stopper.RequestStop(context.WithoutCancel(saved), actor, cause, scope)
	if r.Valid() || !executionStopFault(err, f.InvalidState) {
		t.Fatal("expired original callback did not fail with its closed-scope code")
	}
	// Original tuple is historical evidence; the deleting Project cannot
	// readmit it after its original job was removed.
	_, err = s.x.executor.Advance(ctxFor(t), v.base.project.ID, a)
	if !executionStopFault(err, f.ProjectNotActive) {
		t.Fatal("stopped Project did not reject readmission at its current gate")
	}
	s.x.round.requireWire(t, 2, true)
	s.x.round.reader.requireDestroyed(t, 2)
	s.close(t)
	t.Log("EXECUTION_PROJECT_STOP_ISOLATED same_executor=true original_model_return=true project_b_completed=true original_checkpoint=true")
}

func testExecutionStopUnknown(t *testing.T) {
	s := newExecutionStopFixture(t, false)
	v := s.x.round.capture.v
	capture, auth, recoverNow := s.barrier(), s.barrier(), s.barrier()
	pending := make(chan error, 1)
	captureAttempt := id[f.TransactionAttempt](t)
	var captureMasked atomic.Bool
	var captureCause f.TransactionCause
	s.setupDriver(t, func(ctx context.Context, a i.Actor, c pc.LifecycleCause, p pc.ScopeRef) error {
		r, err := s.stopper.RequestStop(ctx, a, c, p)
		if err == nil {
			err = executionStopReport(r, c, p, false)
		}
		pending <- err
		if err != nil {
			return err
		}
		select {
		case <-recoverNow.release:
		case <-ctx.Done():
			return ctx.Err()
		}
		r, err = s.stopper.DrainProject(ctx, a, c, p)
		if err != nil {
			return err
		}
		return executionStopReport(r, c, p, true)
	})
	store := v.base.tracked
	store.mu.Lock()
	store.afterResult = func(c f.TransactionCause, r f.CommitResult) {
		if s.authorization(c) {
			auth.hold(r)
		}
		d := c.Details()
		if d.Kind == f.JobCause && d.JobType == "execution-preparation" && r.State() == f.Committed && !capture.claimed.Load() {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			var exists bool
			err := v.base.raw.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_execution.preparation_inputs WHERE execution_id=$1 AND project_id=$2)`, d.JobID, v.base.project.ID.String()).Scan(&exists)
			if err == nil && exists {
				captureCause = c
				capture.hold(r)
			}
		}
	}
	store.transformResult = func(c f.TransactionCause, r f.CommitResult) f.CommitResult {
		if r.State() != f.Committed {
			return r
		}
		if capture.claimed.Load() && executionStopSameJob(c, captureCause) && captureMasked.CompareAndSwap(false, true) {
			return f.UnknownResult(captureAttempt, c)
		}
		return r
	}
	store.mu.Unlock()
	s.x.start(t)
	a := s.launch(t, v)
	capture.await(t)
	before := s.preparationSnapshot(t, a.ExecutionID)
	stable := s.inputClaimSnapshot(t, a.ExecutionID)
	s.wire(t, 0, 0)
	s.startDriver(t)
	auth.await(t)
	s.lifecycle.requireClaim(t, "running")
	auth.open()
	schedulerStopReceive(t, s.done, pending, "actual committed stop before preparation acknowledgement")
	capture.open()
	executionStopWait(t, func() bool {
		value, err := s.x.executor.Advance(ctxFor(t), v.base.project.ID, a)
		if value.Active {
			return false
		}
		if !value.Retained || value.Joined || !executionStopUnknownFault(err, captureAttempt) {
			t.Fatal("original preparation Unknown was not retained")
		}
		return true
	}, "original preparation returned Unknown")
	if !captureMasked.Load() || s.preparationSnapshot(t, a.ExecutionID) != before {
		t.Fatal("local ackmask rewrote immutable input or original claim")
	}
	s.wire(t, 0, 0)
	// Advance after stop is observation only, including after normal recovery
	// pacing becomes due. Explicit Drain alone owns the old ResolveUnknown.
	timer := time.NewTimer(25 * time.Millisecond)
	defer timer.Stop()
	<-timer.C
	value, err := s.x.executor.Advance(ctxFor(t), v.base.project.ID, a)
	if value.Active || !value.Retained || !executionStopUnknownFault(err, captureAttempt) {
		t.Fatal("stopped observation restarted the retained owner")
	}
	recoverNow.open()
	schedulerStopWait(t, s.done, "original preparation recovery and stop return")
	firstRoundRequire(t, s.driverErr)
	s.lifecycle.requireClaim(t, "terminal")
	var phase, status string
	var rounds, invocations, attempts int64
	err = v.base.raw.QueryRow(ctxFor(t), `SELECT e.status,a.phase,(SELECT count(*) FROM agenteam_execution.rounds WHERE execution_id=e.id),(SELECT count(*) FROM agenteam_model.invocations WHERE execution_id=e.id),(SELECT count(*) FROM agenteam_execution.preparation_attempts n WHERE n.execution_id=e.id) FROM agenteam_execution.executions e JOIN agenteam_execution.preparation_attempts a ON a.execution_id=e.id WHERE e.id=$1`, a.ExecutionID.String()).Scan(&status, &phase, &rounds, &invocations, &attempts)
	firstRoundRequire(t, err)
	if status != "preparing" || phase != "terminal" || rounds != 0 || invocations != 0 || attempts != 1 || s.inputClaimSnapshot(t, a.ExecutionID) != stable {
		t.Fatal("recovery entered a first Model or invented Execution terminal")
	}
	_, err = s.x.executor.Advance(ctxFor(t), v.base.project.ID, a)
	if !executionStopFault(err, f.ProjectNotActive) {
		t.Fatal("stopped retained association did not reject fresh admission at its current gate")
	}
	s.x.round.requireWire(t, 0, true)
	s.x.round.reader.requireDestroyed(t, 0)
	s.close(t)
	t.Log("EXECUTION_PROJECT_STOP_UNKNOWN local_ackmask=true original_claim_retained=true explicit_original_recovery=true no_first_model=true")
}
func executionStopSameJob(a, b f.TransactionCause) bool {
	x, y := a.Details(), b.Details()
	return x.Kind == f.JobCause && y.Kind == f.JobCause && x.JobType == y.JobType && x.JobID == y.JobID && x.JobAttemptID == y.JobAttemptID
}

func executionStopFault(err error, code f.Code) bool {
	var fault *f.Fault
	return errors.As(err, &fault) && fault.Code == code
}
func executionStopUnknownFault(err error, attempt f.ID[f.TransactionAttempt]) bool {
	var fault *f.Fault
	return errors.As(err, &fault) && fault.Code == f.CommitUnknown && fault.CommitState == f.Unknown && fault.CauseID == attempt.String()
}
func (s *executionStopFixture) preparationSnapshot(t *testing.T, e i.ExecutionID) string {
	t.Helper()
	var result string
	err := s.x.round.capture.v.base.raw.QueryRow(ctxFor(t), `SELECT jsonb_build_object('input',to_jsonb(p),'claim',to_jsonb(c),'attempt',to_jsonb(a))::text FROM agenteam_execution.preparation_inputs p JOIN agenteam_execution.preparation_claims c ON c.execution_id=p.execution_id JOIN agenteam_execution.preparation_attempts a ON a.execution_id=c.execution_id AND a.attempt_id=c.attempt_id WHERE p.execution_id=$1`, e.String()).Scan(&result)
	firstRoundRequire(t, err)
	return result
}
func (s *executionStopFixture) inputClaimSnapshot(t *testing.T, e i.ExecutionID) string {
	t.Helper()
	var result string
	err := s.x.round.capture.v.base.raw.QueryRow(ctxFor(t), `SELECT jsonb_build_object('input',to_jsonb(p),'claim',to_jsonb(c))::text FROM agenteam_execution.preparation_inputs p JOIN agenteam_execution.preparation_claims c ON c.execution_id=p.execution_id WHERE p.execution_id=$1`, e.String()).Scan(&result)
	firstRoundRequire(t, err)
	return result
}

// A failed join retains the original graph until the enclosing Go hard deadline;
// it is not bounded local-join success and cannot enter the next subtest.
func (s *executionStopFixture) close(t *testing.T) {
	t.Helper()
	s.closed.Do(func() {
		for _, b := range s.barriers {
			b.open()
		}
		if s.cancel != nil {
			s.cancel()
		}
		if s.done != nil {
			select {
			case <-s.done:
			case <-time.After(10 * time.Second):
				t.Error("original stop round has not returned; retaining its resource graph")
				<-s.done
			}
		}
		if s.driver != nil {
			s.driver.Stop()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			err := s.driver.Drain(ctx)
			cancel()
			// The driver exposes Drain, not Joined. Its original Run return
			// was awaited above; Drain also waits for its actual owned calls.
			if err != nil {
				t.Error("original driver Drain failed; retaining graph", err)
				select {}
			}
		}
		s.x.executor.Stop()
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		err := s.x.executor.Drain(ctx)
		cancel()
		if err != nil || !s.x.executor.Joined() {
			t.Error("original executor is not joined; retaining graph", err)
			select {}
		}
		if s.x.started {
			select {
			case err := <-s.x.runDone:
				if err != nil && !errors.Is(err, context.Canceled) {
					t.Error("original executor Run error", err)
				}
				s.x.started = false
			case <-time.After(time.Second):
				t.Error("original executor Run has not returned; retaining graph")
				<-s.x.runDone
			}
		}
		if s.x.cancel != nil {
			s.x.cancel()
		}
		s.prepare.Stop()
		ctx, cancel = context.WithTimeout(context.Background(), 5*time.Second)
		err = s.prepare.Drain(ctx)
		cancel()
		if err != nil || !s.prepare.Joined() {
			t.Error("original preparation did not join; retaining graph", err)
			select {}
		}
		s.x.visitor.Stop()
		ctx, cancel = context.WithTimeout(context.Background(), 3*time.Second)
		err = s.x.visitor.Drain(ctx)
		cancel()
		if err != nil || !s.x.visitor.Joined() {
			t.Error("original visitor did not join; retaining graph", err)
			select {}
		}
		store := s.x.round.capture.v.base.tracked
		store.mu.Lock()
		store.afterResult = nil
		store.transformResult = nil
		store.mu.Unlock()
	})
}
