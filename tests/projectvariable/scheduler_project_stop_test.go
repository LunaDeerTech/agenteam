//go:build integration

package projectvariable_test

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/agent"
	"github.com/LunaDeerTech/agenteam/internal/central/execution"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/project"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/scheduler"
	"github.com/LunaDeerTech/agenteam/internal/central/work"
	wc "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
)

// A/B are formally initialized Projects. The real manager owns real runners
// and borrows an actual, idle AssociatedExecutor; no Claim/Launch/Model request
// is made. Only accepted Delete is a controlled SQL prerequisite. The original
// StopDriver and bound guard produce stopping and the exact running claim.
// Physical COMMIT and the original Store return are distinct barriers below.
func TestSchedulerProjectStop(t *testing.T) {
	t.Run("committed-stop-isolates-project-and-discards-stale-page", testSchedulerStopCommitted)
	t.Run("driver-scope-and-authorization-unknown", testSchedulerStopAuthority)
}

type schedulerStopBarrier struct {
	claimed atomic.Bool
	entered chan f.CommitResult
	release chan struct{}
	once    sync.Once
}

func newSchedulerStopBarrier() *schedulerStopBarrier {
	return &schedulerStopBarrier{entered: make(chan f.CommitResult, 1), release: make(chan struct{})}
}
func (b *schedulerStopBarrier) open() { b.once.Do(func() { close(b.release) }) }
func (b *schedulerStopBarrier) hold(result f.CommitResult) {
	if b.claimed.CompareAndSwap(false, true) {
		b.entered <- result
		<-b.release // The original call must really return, including on cancellation.
	}
}
func (b *schedulerStopBarrier) await(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithTimeout(ctxFor(t), 5*time.Second)
	defer cancel()
	select {
	case result := <-b.entered:
		if result.State() != f.Committed {
			t.Fatal("Scheduler stop barrier did not observe the original physical COMMIT")
		}
	case <-ctx.Done():
		t.Fatal("Scheduler stop original Store return barrier was not reached")
	}
}

type schedulerStopReads struct {
	wc.SchedulerTaskReader
	mu     sync.Mutex
	counts map[pc.ProjectID]int
}

func (r *schedulerStopReads) SnapshotInTx(ctx context.Context, tx f.Tx, p pc.ProjectID) (wc.SchedulerTaskSnapshot, error) {
	value, err := r.SchedulerTaskReader.SnapshotInTx(ctx, tx, p)
	if err == nil {
		r.mu.Lock()
		r.counts[p]++
		r.mu.Unlock()
	}
	return value, err
}
func (r *schedulerStopReads) count(p pc.ProjectID) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.counts[p]
}

type schedulerStopFixture struct {
	x              *schedulerExecutionFixture
	v              *taskTransitionFixture
	a, b           pc.ProjectRef
	directory      *project.SchedulerProjects
	manager        *scheduler.ProjectRunners
	stopper        *scheduler.ProjectStopper
	driver         *project.LifecycleStopDriver
	relaunch       *scheduler.RelaunchCoordinator
	writer         *work.TaskRelaunchService
	reads          *schedulerStopReads
	manifest       pc.RequiredManifest
	cause          pc.LifecycleCause
	before         string
	claimAttempt   string
	capture        *schedulerStopBarrier
	oldPage        *schedulerStopBarrier
	freshPage      *schedulerStopBarrier
	authorization  *schedulerStopBarrier
	continueStep   *schedulerStopBarrier
	captureArmed   atomic.Bool
	directoryArmed atomic.Bool
	directoryCalls atomic.Int64
	authorizations atomic.Int64
	maskUnknown    bool // Fixed before any owned call starts.
	masked         atomic.Bool
	unknownAttempt f.ID[f.TransactionAttempt]
	managerCancel  context.CancelFunc
	managerDone    chan struct{}
	managerErr     error
	driverDone     chan struct{}
	driverErr      error
	closed         sync.Once
}

func newSchedulerStopFixture(t *testing.T, unknown bool, step func(*schedulerStopFixture, context.Context, i.Actor, pc.LifecycleCause, pc.ScopeRef) error) *schedulerStopFixture {
	t.Helper()
	s := &schedulerStopFixture{x: newSchedulerExecutionFixture(t, true), maskUnknown: unknown,
		capture: newSchedulerStopBarrier(), oldPage: newSchedulerStopBarrier(), freshPage: newSchedulerStopBarrier(),
		authorization: newSchedulerStopBarrier(), continueStep: newSchedulerStopBarrier(), unknownAttempt: id[f.TransactionAttempt](t)}
	s.v = s.x.round.capture.v
	t.Cleanup(func() { s.close(t) })
	setPendingVisitEnabled(t, s.v, false, "project-stop-pause-fixture")
	archiveManagerBaseProject(t, s.v.agent.p2)
	s.a = createManagerProject(t, s.v.agent.p2, "project-stop-a")
	s.b = createManagerProject(t, s.v.agent.p2, "project-stop-b")
	s.before = runnerSnapshot(t, s.v)
	s.manifest = lifecycleManifest(t)
	s.cause = pc.LifecycleCause{OperationID: id[pc.Operation](t), Action: pc.Delete, ProjectVersion: s.a.Version + 1}
	var err error
	s.directory, err = project.NewSchedulerProjects(s.v.base.projectAuthority)
	schedulerStopRequire(t, err)
	reader, err := work.NewSchedulerTaskReader(s.v.base.tracked, s.v.authority)
	schedulerStopRequire(t, err)
	s.reads = &schedulerStopReads{SchedulerTaskReader: reader, counts: make(map[pc.ProjectID]int)}
	agents, err := agent.NewSchedulerCurrent(s.v.agent.providers.Agents, s.v.pending)
	schedulerStopRequire(t, err)
	occupancy, err := execution.NewWorkOccupancy(s.v.base.tracked)
	schedulerStopRequire(t, err)
	s.writer, err = work.NewTaskRelaunch(s.v.base.tracked, work.TaskRelaunchDependencies{
		Authority: s.v.authority, Scheduler: s.v.pending, Agents: agents, Pending: s.v.pending, Occupancy: occupancy,
	})
	schedulerStopRequire(t, err)
	s.relaunch, err = scheduler.NewRelaunchCoordinator(s.x.round.capture.claims, s.writer, reader, occupancy, 1)
	schedulerStopRequire(t, err)
	s.manager, err = scheduler.NewProjectRunners(scheduler.ProjectRunnersDependencies{
		Projects: s.directory, Coordinator: s.x.round.capture.claims, Visitor: s.x.visitor, Tasks: s.reads,
		Relaunch: s.relaunch, Executions: s.x.executor,
	}, scheduler.ProjectRunnersOptions{MaxProjects: 4, ProjectPageSize: 16, ExecutionPageSize: 1,
		DiscoveryInterval: 20 * time.Millisecond, TickInterval: 20 * time.Millisecond, RelaunchSkipCount: 1, LaunchPolicy: s.x.round.capture.launchPolicy})
	schedulerStopRequire(t, err)
	lifecycle, err := project.NewLifecycleAuthority(s.v.base.tracked, s.manifest, nil)
	schedulerStopRequire(t, err)
	s.driver, err = project.NewLifecycleStopDriver(s.v.base.tracked, lifecycle, captureProviderProcesses{guard: s.v.agent.guard},
		func(ctx context.Context, actor i.Actor, cause pc.LifecycleCause, scope pc.ScopeRef) error {
			return step(s, ctx, actor, cause, scope)
		})
	schedulerStopRequire(t, err)
	s.stopper, err = scheduler.NewProjectStopper(s.manager, s.driver)
	schedulerStopRequire(t, err)
	store := s.v.base.tracked
	store.mu.Lock()
	store.afterResult = func(cause f.TransactionCause, result f.CommitResult) {
		d := cause.Details()
		if d.Kind == f.CommandsCause && d.Primary.Namespace() == "scheduler" && d.Primary.Command() == "capture_traversal" {
			owners := d.Primary.OwnerIDs()
			if len(owners) == 1 && owners[0] == s.a.ID.String() && s.captureArmed.Load() {
				s.capture.hold(result)
			}
		}
		if d.Kind == f.RecoveryCause && d.Owner == "project.scheduler-projects" && s.directoryArmed.Load() {
			switch s.directoryCalls.Add(1) {
			case 1:
				s.oldPage.hold(result)
			case 2:
				s.freshPage.hold(result)
			}
		}
		if s.authorizationCause(cause) && s.authorizations.Add(1) == 1 {
			s.authorization.hold(result)
		}
	}
	store.transformResult = func(cause f.TransactionCause, result f.CommitResult) f.CommitResult {
		if s.maskUnknown && s.authorizationCause(cause) && result.State() == f.Committed && s.masked.CompareAndSwap(false, true) {
			return f.UnknownResult(s.unknownAttempt, cause)
		}
		return result
	}
	store.mu.Unlock()
	s.x.start(t)
	ctx, cancel := context.WithCancel(ctxFor(t))
	s.managerCancel, s.managerDone = cancel, make(chan struct{})
	go func() { defer close(s.managerDone); s.managerErr = s.manager.Run(ctx) }()
	s.await(t, func(o scheduler.ProjectRunnersObservation) bool {
		return schedulerStopRunning(o, s.a.ID) && schedulerStopRunning(o, s.b.ID) && s.reads.count(s.a.ID) > 0 && s.reads.count(s.b.ID) > 0
	})
	return s
}

func schedulerStopRequire(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal("real Scheduler project-stop prerequisite failed", err)
	}
}
func (s *schedulerStopFixture) authorizationCause(cause f.TransactionCause) bool {
	d := cause.Details()
	return d.Kind == f.RecoveryCause && d.Owner == "scheduler-project-stop" && d.RecoveryRunID == s.cause.OperationID.String() && d.CheckpointRef == s.a.ID.String()
}
func schedulerStopRunning(o scheduler.ProjectRunnersObservation, p pc.ProjectID) bool {
	for _, row := range o.Projects {
		if row.ProjectID == p {
			return row.Running && !row.Stopping && !row.Retained && row.Err == nil
		}
	}
	return false
}
func schedulerStopAbsent(o scheduler.ProjectRunnersObservation, p pc.ProjectID) bool {
	for _, row := range o.Projects {
		if row.ProjectID == p {
			return false
		}
	}
	return true
}
func (s *schedulerStopFixture) await(t *testing.T, accept func(scheduler.ProjectRunnersObservation) bool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(ctxFor(t), 5*time.Second)
	defer cancel()
	tick := time.NewTicker(5 * time.Millisecond)
	defer tick.Stop()
	for {
		o := s.manager.Observation()
		if o.DiscoveryError != nil || o.Stopped {
			t.Fatal("real Scheduler manager stopped or lost its directory")
		}
		if accept(o) {
			return
		}
		select {
		case <-tick.C:
		case <-ctx.Done():
			t.Fatal("real Scheduler project-stop state was not reached")
		}
	}
}

// These statements supply accepted Delete only, with the existing complete
// manifest. They neither fabricate a stopping claim nor issue stop authority.
func (s *schedulerStopFixture) acceptDelete(t *testing.T) {
	t.Helper()
	raw, err := json.Marshal(s.manifest.Entries())
	schedulerStopRequire(t, err)
	digest, err := s.manifest.Digest()
	schedulerStopRequire(t, err)
	cause, err := f.NewRecoveryCause("scheduler-stop.fixture", s.cause.OperationID.String(), "")
	schedulerStopRequire(t, err)
	store := s.v.base.tracked
	result := store.WithinTx(ctxFor(t), cause, func(ctx context.Context, tx f.Tx) error {
		key, _ := f.ProjectLock(s.a.ID.String())
		if err := store.AcquireAll(ctx, tx, []f.LockRequest{{Key: key, Mode: f.Exclusive}}); err != nil {
			return err
		}
		x, err := store.InTx(tx)
		if err != nil {
			return err
		}
		_, err = x.Exec(ctx, `INSERT INTO agenteam_project.lifecycle_operations(id,project_id,owner_user_id,action,project_version,state,version,required_manifest,manifest_digest,created_at,updated_at) VALUES($1,$2,$3,'delete',$4,'accepted',1,$5::jsonb,$6,clock_timestamp(),clock_timestamp())`, s.cause.OperationID.String(), s.a.ID.String(), s.a.OwnerUserID.String(), int64(s.cause.ProjectVersion), raw, digest.String())
		if err != nil {
			return err
		}
		for _, entry := range s.manifest.Entries() {
			if _, err = x.Exec(ctx, `INSERT INTO agenteam_project.lifecycle_participants(operation_id,participant_name,contract_version,stop_state,cleanup_state,version) VALUES($1,$2,$3,'required','required',1)`, s.cause.OperationID.String(), string(entry.Name), int64(entry.ContractVersion)); err != nil {
				return err
			}
		}
		tag, err := x.Exec(ctx, `UPDATE agenteam_project.projects SET lifecycle='deleting',version=$2,current_lifecycle_operation_id=$3,updated_at=clock_timestamp() WHERE id=$1 AND version=$4 AND lifecycle='active' AND current_lifecycle_operation_id IS NULL`, s.a.ID.String(), int64(s.cause.ProjectVersion), s.cause.OperationID.String(), int64(s.a.Version))
		if err != nil || tag.RowsAffected() != 1 {
			return errors.New("accepted Delete prerequisite lost its original Project")
		}
		return nil
	})
	if result.State() != f.Committed {
		t.Fatal("accepted Delete prerequisite did not actually commit")
	}
}

func (s *schedulerStopFixture) holdOriginalCalls(t *testing.T) {
	t.Helper()
	s.captureArmed.Store(true)
	s.capture.await(t)
	s.directoryArmed.Store(true)
	s.oldPage.await(t)
	// Both original transactions have physically committed. In particular no
	// A Project SH is being held while accepted Delete obtains its real EX.
	s.acceptDelete(t)
}
func (s *schedulerStopFixture) startDriver(t *testing.T) {
	t.Helper()
	s.driverDone = make(chan struct{})
	ctx := ctxFor(t)
	go func() { defer close(s.driverDone); s.driverErr = s.driver.Run(ctx, s.a.ID, s.cause.OperationID) }()
}
func (s *schedulerStopFixture) requireClaim(t *testing.T, phase string) {
	t.Helper()
	process, err := s.v.agent.guard.CurrentProcess()
	schedulerStopRequire(t, err)
	var attempt string
	err = s.v.base.raw.QueryRow(ctxFor(t), `SELECT c.attempt_id::text FROM agenteam_project.work_claims c JOIN agenteam_project.lifecycle_operations o ON o.id=c.work_id JOIN agenteam_project.projects p ON p.id=o.project_id WHERE c.work_kind='lifecycle' AND c.work_id=$1 AND c.project_id=$2 AND c.process_id=$3 AND c.phase=$4 AND c.fence=1 AND o.state='stopping' AND o.action='delete' AND o.project_version=$5 AND p.lifecycle='deleting' AND p.version=$5 AND p.current_lifecycle_operation_id=o.id`, s.cause.OperationID.String(), s.a.ID.String(), process.String(), phase, int64(s.cause.ProjectVersion)).Scan(&attempt)
	schedulerStopRequire(t, err)
	_, err = f.ParseID[struct{}](attempt)
	schedulerStopRequire(t, err)
	if s.claimAttempt == "" && phase == "running" {
		s.claimAttempt = attempt
	}
	if s.claimAttempt != attempt {
		t.Fatal("original real-guard StopDriver claim changed its attempt")
	}
}
func schedulerStopReport(report scheduler.ProjectStopReport, cause pc.LifecycleCause, scope pc.ScopeRef, joined bool) error {
	d := report.Details()
	wantPending := 1
	if joined {
		wantPending = 0
	}
	if !report.Valid() || !report.Matches(cause, scope) || d.ProjectID != scope.ProjectID || d.OperationID != cause.OperationID || d.Action != pc.Delete || d.ProjectVersion != cause.ProjectVersion || d.LocalJoined != joined || d.PendingRunners != wantPending || d.Retained {
		return errors.New("Scheduler local report lost its exact original Project or return state")
	}
	return nil
}

func testSchedulerStopCommitted(t *testing.T) {
	pending := make(chan error, 1)
	s := newSchedulerStopFixture(t, false, func(s *schedulerStopFixture, ctx context.Context, actor i.Actor, cause pc.LifecycleCause, scope pc.ScopeRef) error {
		report, err := s.stopper.RequestStop(ctx, actor, cause, scope)
		if err == nil {
			err = schedulerStopReport(report, cause, scope, false)
		}
		pending <- err
		if err != nil {
			return err
		}
		report, err = s.stopper.DrainProject(ctx, actor, cause, scope)
		if err != nil {
			return err
		}
		return schedulerStopReport(report, cause, scope, true)
	})
	s.holdOriginalCalls(t)
	s.startDriver(t)
	s.authorization.await(t)
	s.requireClaim(t, "running")
	s.requireUnstopped(t)
	select {
	case <-pending:
		t.Fatal("RequestStop returned before its original authorization Store returned")
	default:
	}
	s.authorization.open()
	schedulerStopReceive(t, s.driverDone, pending, "original RequestStop return")
	s.requirePending(t)
	s.capture.open()
	s.finishAndCheck(t)
	t.Log("SCHEDULER_PROJECT_STOP_ISOLATED original_return=true stale_page_discarded=true borrowed_owners_unchanged=true")
}

func testSchedulerStopAuthority(t *testing.T) {
	unknownReturned := make(chan error, 1)
	var savedCtx context.Context
	var savedActor i.Actor
	var savedCause pc.LifecycleCause
	var savedScope pc.ScopeRef
	s := newSchedulerStopFixture(t, true, func(s *schedulerStopFixture, ctx context.Context, actor i.Actor, cause pc.LifecycleCause, scope pc.ScopeRef) error {
		savedCtx, savedActor, savedCause, savedScope = ctx, actor, cause, scope
		// Even the actual actor/cause DTO and actual stopping rows do not carry
		// the private original-driver issuer into a caller-created context.
		report, err := s.stopper.RequestStop(context.Background(), actor, cause, scope)
		var fault *f.Fault
		if report.Valid() || !errors.As(err, &fault) || fault.Code != f.Forbidden || s.authorizations.Load() != 0 {
			return errors.New("caller-built issuer reached Scheduler stop authority")
		}
		report, err = s.stopper.RequestStop(ctx, actor, cause, scope)
		fault = nil
		if report.Valid() || !errors.As(err, &fault) || fault.Code != f.CommitUnknown || fault.CommitState != f.Unknown || fault.CauseID != s.unknownAttempt.String() || !s.masked.Load() {
			err = errors.New("original authorization Unknown lost its result or issued a report")
		} else {
			err = nil
		}
		unknownReturned <- err
		if err != nil {
			return err
		}
		select {
		case <-s.continueStep.release:
		case <-ctx.Done():
			return ctx.Err()
		}
		// The preceding read-only authorization has actually returned Unknown.
		// This is an independent current authorization from the still-live
		// original step, not lookup, inferred rollback or replay of that result.
		report, err = s.stopper.DrainProject(ctx, actor, cause, scope)
		if err != nil {
			return err
		}
		return schedulerStopReport(report, cause, scope, true)
	})
	s.holdOriginalCalls(t)
	s.startDriver(t)
	s.authorization.await(t)
	s.requireClaim(t, "running")
	s.requireUnstopped(t)
	select {
	case <-unknownReturned:
		t.Fatal("authorization ackmask escaped the original Store return barrier")
	default:
	}
	s.authorization.open()
	schedulerStopReceive(t, s.driverDone, unknownReturned, "original authorization Unknown return")
	s.requireUnstopped(t)
	if s.authorizations.Load() != 1 || !s.masked.Load() {
		t.Fatal("Unknown authorization retried itself or skipped its original return")
	}
	s.continueStep.open()
	s.requirePending(t)
	s.capture.open()
	schedulerStopWait(t, s.driverDone, "original authorized stop round")
	schedulerStopRequire(t, s.driverErr)
	before := s.authorizations.Load()
	report, err := s.stopper.RequestStop(context.WithoutCancel(savedCtx), savedActor, savedCause, savedScope)
	var fault *f.Fault
	if report.Valid() || !errors.As(err, &fault) || fault.Code != f.InvalidState || s.authorizations.Load() != before {
		t.Fatal("saved closed step context revived its original private issuer")
	}
	s.finishAndCheck(t)
	t.Log("SCHEDULER_PROJECT_STOP_AUTHORITY actual_ack_unknown_zero_stop=true original_driver_scope_only=true")
}

func schedulerStopReceive(t *testing.T, done <-chan struct{}, result <-chan error, phase string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(ctxFor(t), 5*time.Second)
	defer cancel()
	select {
	case err := <-result:
		schedulerStopRequire(t, err)
	case <-done:
		// The result may have been published immediately before Run returned.
		select {
		case err := <-result:
			schedulerStopRequire(t, err)
		default:
			t.Fatal("original stop round returned before its required phase:", phase)
		}
	case <-ctx.Done():
		t.Fatal("original Scheduler control return was not reached:", phase)
	}
}

func (s *schedulerStopFixture) requireUnstopped(t *testing.T) {
	t.Helper()
	count := s.reads.count(s.b.ID)
	s.await(t, func(o scheduler.ProjectRunnersObservation) bool {
		return schedulerStopRunning(o, s.a.ID) && schedulerStopRunning(o, s.b.ID) && s.reads.count(s.b.ID) > count
	})
}
func (s *schedulerStopFixture) requirePending(t *testing.T) {
	t.Helper()
	count := s.reads.count(s.b.ID)
	s.await(t, func(o scheduler.ProjectRunnersObservation) bool {
		if !schedulerStopRunning(o, s.b.ID) || s.reads.count(s.b.ID) <= count {
			return false
		}
		for _, row := range o.Projects {
			if row.ProjectID == s.a.ID {
				return row.Running && row.Stopping && !row.Retained
			}
		}
		return false
	})
	select {
	case <-s.driverDone:
		t.Fatal("local DrainProject passed the held original A runner call")
	default:
	}
}

func (s *schedulerStopFixture) finishAndCheck(t *testing.T) {
	t.Helper()
	schedulerStopWait(t, s.driverDone, "original StopDriver Run")
	schedulerStopRequire(t, s.driverErr)
	s.requireClaim(t, "terminal")
	// The stale page must not be rescued by waiting for the next fresh page to
	// erase a briefly resurrected A. Hold that next page before publication.
	s.oldPage.open()
	s.freshPage.await(t)
	o := s.manager.Observation()
	if !schedulerStopAbsent(o, s.a.ID) || !schedulerStopRunning(o, s.b.ID) {
		t.Fatal("stale directory page recreated A or stopped B before fresh publication")
	}
	s.freshPage.open()
	count := s.reads.count(s.b.ID)
	s.await(t, func(o scheduler.ProjectRunnersObservation) bool {
		return schedulerStopAbsent(o, s.a.ID) && schedulerStopRunning(o, s.b.ID) && s.reads.count(s.b.ID) > count && s.directoryCalls.Load() >= 3
	})
	page, err := s.directory.ListSchedulerProjects(ctxFor(t), pc.SchedulerProjectPageRequest{Limit: 16})
	schedulerStopRequire(t, err)
	var foundB bool
	for _, p := range page.ProjectIDs {
		if p == s.a.ID {
			t.Fatal("fresh real directory included deleting A")
		}
		foundB = foundB || p == s.b.ID
	}
	if !page.Complete || !foundB || runnerSnapshot(t, s.v) != s.before {
		t.Fatal("local stopping changed borrowed scheduler business facts or active B")
	}
	var claims, dispatches, executions, required int64
	err = s.v.base.raw.QueryRow(ctxFor(t), `SELECT (SELECT count(*) FROM agenteam_work.task_scheduler_claims), (SELECT count(*) FROM agenteam_scheduler.dispatches), (SELECT count(*) FROM agenteam_execution.executions), (SELECT count(*) FROM agenteam_project.lifecycle_participants WHERE operation_id=$1 AND stop_state='required' AND cleanup_state='required')`, s.cause.OperationID.String()).Scan(&claims, &dispatches, &executions, &required)
	schedulerStopRequire(t, err)
	if claims != 0 || dispatches != 0 || executions != 0 || required != int64(len(s.manifest.Entries())) {
		t.Fatal("local report was promoted to business execution or full participant completion")
	}
	s.x.round.requireWire(t, 0, true)
	if s.x.executor.Joined() || s.x.round.capture.claims.Joined() || s.x.visitor.Joined() || s.relaunch.Joined() {
		t.Fatal("stopping A retired a borrowed owner")
	}
	s.close(t)
}

func schedulerStopWait(t *testing.T, done <-chan struct{}, phase string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	select {
	case <-done:
	case <-ctx.Done():
		t.Error("Scheduler stop retains original graph until actual return:", phase)
		<-done // Failure retention uses the original global Go/supervisor limit.
	}
}
func (s *schedulerStopFixture) close(t *testing.T) {
	t.Helper()
	s.closed.Do(func() {
		for _, barrier := range []*schedulerStopBarrier{s.capture, s.oldPage, s.freshPage, s.authorization, s.continueStep} {
			barrier.open()
		}
		if s.driver != nil {
			s.driver.Stop()
		}
		if s.manager != nil {
			s.manager.Stop()
		}
		if s.managerCancel != nil {
			s.managerCancel()
		}
		if s.driverDone != nil {
			schedulerStopWait(t, s.driverDone, "StopDriver cleanup")
		}
		if s.managerDone != nil {
			schedulerStopWait(t, s.managerDone, "manager cleanup")
			if s.managerErr != nil && !errors.Is(s.managerErr, context.Canceled) {
				t.Error("original manager returned an unexpected retained error")
			}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if s.driver != nil {
			if err := s.driver.Drain(ctx); err != nil {
				t.Error("original driver remains owned before guard cleanup")
				_ = s.driver.Drain(context.Background())
			}
		}
		if s.manager != nil {
			if err := s.manager.Drain(ctx); err != nil || !s.manager.Joined() {
				t.Error("original manager remains retained; no clean join is claimed")
				select {} // Never delete this fixture's database/guard under an owner.
			}
		}
		if s.relaunch != nil {
			s.relaunch.Stop()
			if err := s.relaunch.Drain(ctx); err != nil || !s.relaunch.Joined() {
				t.Error("original relaunch owner remains retained")
				select {}
			}
		}
		if s.writer != nil {
			s.writer.Stop()
			if err := s.writer.Drain(ctx); err != nil || !s.writer.Joined() {
				t.Error("original Work relaunch provider remains retained")
				select {}
			}
		}
		if s.x != nil && s.x.started {
			s.x.executor.Stop()
			s.x.cancel()
			if err := s.x.executor.Drain(ctx); err != nil || !s.x.executor.Joined() {
				t.Error("original idle executor remains owned before guard cleanup")
				select {}
			}
			select {
			case err := <-s.x.runDone:
				if err != nil && !errors.Is(err, context.Canceled) {
					t.Error("original idle executor returned an unexpected error")
				}
			case <-ctx.Done():
				t.Error("original executor Run has not returned; retaining fixture")
				<-s.x.runDone
			}
			s.x.started = false
		}
		if s.v != nil {
			store := s.v.base.tracked
			store.mu.Lock()
			store.afterResult, store.transformResult = nil, nil
			store.mu.Unlock()
		}
	})
}
