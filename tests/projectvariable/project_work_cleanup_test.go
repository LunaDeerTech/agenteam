//go:build integration

package projectvariable_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/LunaDeerTech/agenteam/internal/central/project"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/work"
	wc "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
	pgfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/postgres"
)

// Five services and three readers belong to the original WithReaders pair
// before the first Work call. Scheduler and Execution really stop and clean;
// other missing participants' stopping/cleaning handoff stays controlled.
// Launch creates an Execution but never invokes it. No nonempty relaunch or
// busy-compensation history, default App binding or final Delete is claimed.
func TestProjectWorkCleanup(t *testing.T) {
	t.Run("real-relations-upgrade-and-original-return", testWorkCleanupReturn)
	t.Run("provider-and-checkpoint-unknown-readonly-confirmations", testWorkCleanupUnknown)
	t.Run("registration-rollback-unknown-and-next-run", testWorkCleanupRegistration)
}

// Used only by the nil-default retirement assembly seam. The original
// Scheduler helper still supplies its real Authority, Store and event graph.
type workCleanupPGAssembly struct {
	pair     *work.ProjectCallOwnership
	blockers wc.TaskBlockerEvents
	sprints  wc.SprintLifecycleEvents
}

func (*workCleanupPGAssembly) manifest(entries []pc.ParticipantRegistration) []pc.ParticipantRegistration {
	entries = append(entries, pc.ParticipantRegistration{Name: pc.WorkParticipant, ContractVersion: 1, OwnerModule: "work", ReferenceKinds: []pc.ReferenceKind{"work-call"}, CleanupAfter: []pc.ParticipantName{pc.SchedulerParticipant, pc.ExecutionParticipant}})
	for n := range entries {
		if entries[n].Name == pc.OutboxParticipant || entries[n].Name == pc.AuditParticipant {
			entries[n].CleanupAfter = append(entries[n].CleanupAfter, pc.WorkParticipant)
		}
	}
	return entries
}

func (a *workCleanupPGAssembly) registerEvents(t *testing.T, catalog *event.Catalog) {
	t.Helper()
	var err error
	a.blockers, err = wc.RegisterTaskBlockerEvents(catalog)
	schedulerOwnedRequire(t, "work-cleanup-blocker-events", err)
	a.sprints, err = wc.RegisterSprintLifecycleEvents(catalog)
	schedulerOwnedRequire(t, "work-cleanup-sprint-events", err)
}

func (a *workCleanupPGAssembly) bind(t *testing.T, v *schedulerOwnedStopPG, deps work.ProjectCallDependencies) {
	t.Helper()
	pointer, err := project.NewSprintLifecycle(v.work.base.projectAuthority, deps.Structure.Authority)
	schedulerOwnedRequire(t, "work-cleanup-original-sprint-pointer", err)
	deps.Blockers = work.BlockerDependencies{Authority: deps.Structure.Authority, Structure: deps.Tasks.Structure, Events: deps.Structure.Events, BlockerEvents: a.blockers, Activity: deps.Structure.Activity}
	deps.Sprints = work.SprintLifecycleDependencies{Authority: deps.Structure.Authority, Projects: pointer, Events: deps.Structure.Events, SprintEvents: a.sprints, Activity: deps.Structure.Activity}
	gate, err := project.NewWorkStopAuthority(v.life, v.base.tracked)
	schedulerOwnedRequire(t, "work-cleanup-original-stop-gate", err)
	a.pair, err = work.NewProjectCallOwnershipWithReaders(v.base.tracked, deps, v.base.keys, v.skills.guard, v.skills.process, gate)
	schedulerOwnedRequire(t, "work-cleanup-before-first-business", err)
	t.Cleanup(func() { schedulerOwnedDrain(t, a.pair) })
	w := v.work
	w.structure, w.tasks, w.transitions = a.pair.Service(), a.pair.TaskService(), a.pair.TaskTransitionService()
	w.structureReader, w.taskReader = a.pair.Reader(), a.pair.TaskReader()
}

func (a *workCleanupPGAssembly) startSprint(t *testing.T, w *taskTransitionFixture) {
	t.Helper()
	before, err := a.pair.Reader().GetSprint(ctxFor(t), w.base.ownerBrowser.actor, w.base.project.ID, w.task.SprintID)
	schedulerOwnedRequire(t, "work-cleanup-original-sprint-read", err)
	if before.State != wc.Planned {
		t.Fatal("original Sprint must be planned")
	}
	receipt, err := a.pair.SprintLifecycleService().StartSprint(ctxFor(t), w.base.ownerBrowser.actor, meta(t, "work-cleanup-owned-sprint-start", &before.Version), w.base.project.ID, before.ID)
	schedulerOwnedRequire(t, "work-cleanup-original-sprint-start", err)
	if receipt.Validate() != nil {
		t.Fatal("original SprintStart receipt invalid")
	}
	w.base.project = receipt.Project
}

var workCleanupTables = [...]string{
	"task_events", "task_blockers", "task_launch_failures", "task_busy_compensations",
	"task_scheduler_claims", "task_scheduler_relaunches", "task_blocker_commands",
	"task_transition_commands", "task_commands", "sprint_start_commands", "structure_commands",
	"tasks", "task_order_groups", "task_query_generations", "sprints", "sprint_order_groups",
	"milestones", "milestone_order_groups", "project_calls",
}

type workCleanupPG struct {
	*executionRetirementPG
	workPair    *work.ProjectCallOwnership
	participant pc.ProjectLifecycleParticipant
	cleanup     *project.LifecycleCleanupDriver
	workDrivers []*project.LifecycleCleanupDriver
	other       wc.MilestoneID
	instance    string
	businessB   string
	outsideB    string
	protected   string
	startedWork bool
	finished    bool
}

func newWorkCleanupPG(t *testing.T, upgrade bool) *workCleanupPG {
	t.Helper()
	var base *variableHTTPFixture
	if upgrade {
		db := pgfixture.NewDatabase(t)
		migrate(t, db, migrationPrefix(t, "00084"))
		raw := openStore(t, db.Config(t, nil))
		base = assembleVariableHTTPFixture(t, db, raw, &hookStore{fixtureStore: raw})
	} else {
		base = newVariableHTTPFixture(t)
	}
	w := &workCleanupPGAssembly{}
	v := &workCleanupPG{executionRetirementPG: &executionRetirementPG{}}
	option := &executionRetirementPGAssembly{base: base, fixture: v.executionRetirementPG, work: w}
	v.executionCallPG = newExecutionCallPG(t, false, option)
	v.workPair = w.pair
	t.Cleanup(func() { v.closeWorkCleanup(t) })
	v.seedWorkRelations(t)
	v.rejectWorkLedgerMutation(t)
	if upgrade {
		before := v.workRows(t, v.work.base.project.ID, true)
		for n := 0; n < 2; n++ {
			migrate(t, base.db, migrationPrefix(t, "00085"))
			if before != v.workRows(t, v.work.base.project.ID, true) {
				t.Fatal("migration85/repeat changed original business or returned ownership")
			}
		}
		v.rejectWorkLedgerMutation(t)
	}
	v.accept(t)
	v.executionObserved(t, v.owned, false, pc.Stopped)
	v.observed(t, v.pair, false, pc.Stopped)
	v.stopWork(t)
	v.enterRetirementCleanup(t) // actual Skill/Object Stop and Scheduler Completed
	v.driver = v.newRetirementDriver(t)
	for n := 0; n < 2; n++ {
		v.round(t)
	}
	var prerequisites bool
	schedulerOwnedRequire(t, "work-cleanup-real-predecessors", base.raw.QueryRow(ctxFor(t), `SELECT
 (SELECT count(*)=2 FROM agenteam_project.lifecycle_participants WHERE operation_id=$1::text::uuid AND participant_name IN ('scheduler','execution') AND cleanup_state='completed')
 AND NOT EXISTS(SELECT 1 FROM agenteam_scheduler.dispatches WHERE project_id=$2::text)
 AND NOT EXISTS(SELECT 1 FROM agenteam_scheduler.task_runtimes WHERE project_id=$2::text)
 AND NOT EXISTS(SELECT 1 FROM agenteam_scheduler.project_calls WHERE project_id=$2::text::uuid)
 AND NOT EXISTS(SELECT 1 FROM agenteam_execution.executions WHERE project_id=$2::text)
 AND NOT EXISTS(SELECT 1 FROM agenteam_execution.project_calls WHERE project_id=$2::text::uuid)`, v.cause.OperationID.String(), v.scope.ProjectID.String()).Scan(&prerequisites))
	if !prerequisites {
		t.Fatal("Work cleanup requires the original Scheduler and Execution completion")
	}
	v.settled = true
	gate, err := project.NewWorkProjectCleanupAuthority(v.life, base.tracked)
	schedulerOwnedRequire(t, "work-cleanup-private-authority", err)
	v.participant, err = work.NewProjectCleanupParticipant(v.workPair, gate)
	schedulerOwnedRequire(t, "work-cleanup-original-with-readers", err)
	v.cleanup = v.newWorkDriver(t)
	v.businessB = v.workRows(t, base.project.ID, false)
	v.outsideB = v.retirementOtherSnapshot(t)
	v.protected = v.protectedRows(t)
	v.requireSeedCounts(t)
	return v
}

func (v *workCleanupPG) seedWorkRelations(t *testing.T) {
	t.Helper()
	a, p := v.base.ownerBrowser.actor, v.work.base.project.ID
	created, err := v.workPair.TaskService().CreateTask(ctxFor(t), a, meta(t, "work-cleanup-blocked-task", nil), p, wc.TaskCreate{TaskID: id[wc.Task](t), SprintID: v.work.task.SprintID, Title: "Actual Work relationship", Type: wc.TaskTypeTask, Priority: wc.TaskPriorityMedium, Plan: "Remain unlaunched"})
	schedulerOwnedRequire(t, "work-cleanup-formal-related-task", err)
	blocked, err := v.workPair.BlockerService().AddTaskBlocker(ctxFor(t), a, meta(t, "work-cleanup-human-blocker", &created.Task.Version), p, created.Task.ID, wc.TaskBlockerCreate{BlockerID: id[wc.TaskBlockerIdentity](t), Type: wc.TaskBlockerWaitingForHuman, Description: "Real owner must return", Metadata: wc.TaskBlockerMetadata{WaitingForHuman: &wc.TaskBlockerWaitingForHumanMetadata{}}})
	schedulerOwnedRequire(t, "work-cleanup-formal-blocker", err)
	if blocked.Validate() != nil {
		t.Fatal("formal blocker receipt invalid")
	}
	page, err := v.workPair.BlockerReader().ListTaskBlockersPage(ctxFor(t), a, p, created.Task.ID, wc.TaskBlockersAll, f.PageRequest{Limit: 10})
	schedulerOwnedRequire(t, "work-cleanup-original-related-read", err)
	if len(page.Items) != 1 || page.Items[0].TaskID != created.Task.ID {
		t.Fatal("original nonempty blocker relationship missing")
	}
	v.other = id[wc.Milestone](t)
	_, err = v.workPair.Service().CreateMilestone(ctxFor(t), a, meta(t, "work-cleanup-project-b", nil), v.base.project.ID, wc.CreateMilestoneRequest{MilestoneID: v.other, Title: "Independent B Work"})
	schedulerOwnedRequire(t, "work-cleanup-formal-project-b", err)
	var lanes, active, instances int
	schedulerOwnedRequire(t, "work-cleanup-original-five-lanes", v.base.raw.QueryRow(ctxFor(t), `SELECT count(DISTINCT lane),count(*) FILTER(WHERE phase<>'business_closed'),count(DISTINCT instance_id),min(instance_id::text) FROM agenteam_work.project_calls WHERE project_id=$1::uuid`, p.String()).Scan(&lanes, &active, &instances, &v.instance))
	if lanes != 5 || active != 0 || instances != 1 {
		t.Fatal("all original Work services/readers must share one returned instance", lanes, active, instances)
	}
}

func (v *workCleanupPG) stopWork(t *testing.T) {
	t.Helper()
	v.step = func(ctx context.Context, actor i.Actor, cause pc.LifecycleCause, scope pc.ScopeRef) error {
		report, err := v.workPair.RequestProjectStop(ctx, actor, cause, scope)
		if err == nil && (!report.Matches(pc.WorkParticipant, cause, scope) || report.Details().State != pc.Stopped) {
			return errors.New("original Work Stop did not finish its actual owners")
		}
		return err
	}
	call := v.start(func(ctx context.Context) error {
		return v.schedulerOwnedStopPG.driver.Run(ctx, v.scope.ProjectID, v.cause.OperationID)
	})
	outboxCleanupWait(t, call.done, "original Work Stop before cleanup")
	schedulerOwnedRequire(t, "work-cleanup-real-stop-return", call.err)
}

func (v *workCleanupPG) newWorkDriver(t *testing.T) *project.LifecycleCleanupDriver {
	t.Helper()
	d, err := project.NewLifecycleCleanupDriverForWork(v.base.tracked, v.life, captureProviderProcesses{guard: v.skills.guard}, v.participant)
	schedulerOwnedRequire(t, "work-cleanup-fixed-driver", err)
	v.workDrivers = append(v.workDrivers, d)
	return d
}

func (v *workCleanupPG) workRows(t *testing.T, p i.ProjectID, ledger bool) string {
	t.Helper()
	var parts []string
	for _, table := range workCleanupTables {
		if table == "project_calls" && !ledger {
			continue
		}
		parts = append(parts, `(SELECT jsonb_agg(to_jsonb(x) ORDER BY to_jsonb(x)::text) FROM agenteam_work.`+table+` x WHERE project_id=$1::uuid)`)
	}
	var raw string
	schedulerOwnedRequire(t, "work-cleanup-full-work-snapshot", v.base.raw.QueryRow(ctxFor(t), "SELECT jsonb_build_array("+strings.Join(parts, ",")+")::text", p.String()).Scan(&raw))
	return raw
}

func (v *workCleanupPG) counts(t *testing.T) [19]int64 {
	t.Helper()
	var counts [19]int64
	parts, dest := make([]string, len(counts)), make([]any, len(counts))
	for n, table := range workCleanupTables {
		parts[n] = `(SELECT count(*) FROM agenteam_work.` + table + ` WHERE project_id=$1::uuid)`
		dest[n] = &counts[n]
	}
	schedulerOwnedRequire(t, "work-cleanup-19-counts", v.base.raw.QueryRow(ctxFor(t), "SELECT "+strings.Join(parts, ","), v.scope.ProjectID.String()).Scan(dest...))
	return counts
}

func (v *workCleanupPG) requireSeedCounts(t *testing.T) {
	t.Helper()
	counts := v.counts(t)
	for n, count := range counts {
		if n == 3 || n == 5 { // no real busy-compensation or terminal relaunch seed
			if count != 0 {
				t.Fatal("unrequested Work history was created", workCleanupTables[n])
			}
			continue
		}
		if count < 1 {
			t.Fatal("formal Work seed lacks expected nonempty history", workCleanupTables[n])
		}
	}
	t.Logf("PROJECT_WORK_CLEANUP_SEED counts=%v empty_busy_compensation_and_relaunch=true", counts)
}

func (v *workCleanupPG) protectedRows(t *testing.T) string {
	t.Helper()
	parts := []string{
		`(SELECT to_jsonb(x) FROM agenteam_project.projects x WHERE id=$1::text::uuid)`,
		`(SELECT jsonb_agg(to_jsonb(x) ORDER BY id) FROM agenteam_agent.agents x WHERE project_id=$1::text::uuid)`,
		`(SELECT jsonb_agg(to_jsonb(x) ORDER BY id) FROM agenteam_outbox.events x WHERE project_id=$1::text::uuid)`,
		`(SELECT jsonb_agg(to_jsonb(x) ORDER BY participant_name) FROM agenteam_project.lifecycle_participants x WHERE operation_id=$2::text::uuid AND participant_name<>'work')`,
	}
	for _, table := range []string{"assignment_commands", "agent_assignment_heads", "assignment_execution_retirement_proofs"} {
		parts = append(parts, `(SELECT jsonb_agg(to_jsonb(x) ORDER BY to_jsonb(x)::text) FROM agenteam_skill.`+table+` x WHERE project_id=$1::text::uuid)`)
	}
	for _, table := range []string{"agenteam_execution.executions", "agenteam_scheduler.dispatches", "agenteam_scheduler.task_runtimes", "agenteam_scheduler.relaunch_visits"} {
		parts = append(parts, `(SELECT jsonb_agg(to_jsonb(x) ORDER BY to_jsonb(x)::text) FROM `+table+` x WHERE project_id=$1::text)`)
	}
	var raw string
	schedulerOwnedRequire(t, "work-cleanup-external-facts", v.base.raw.QueryRow(ctxFor(t), "SELECT jsonb_build_array("+strings.Join(parts, ",")+")::text", v.scope.ProjectID.String(), v.cause.OperationID.String()).Scan(&raw))
	return raw
}

func (v *workCleanupPG) fullSnapshot(t *testing.T) string {
	t.Helper()
	return v.workRows(t, v.scope.ProjectID, true) + v.workRows(t, v.base.project.ID, true) + v.protectedRows(t) + v.retirementSnapshot(t)
}

func (v *workCleanupPG) preserved(t *testing.T) {
	t.Helper()
	if v.businessB != v.workRows(t, v.base.project.ID, false) || v.outsideB != v.retirementOtherSnapshot(t) || v.protected != v.protectedRows(t) {
		t.Fatal("Work cleanup changed Project B or another domain")
	}
	if v.models.calls.Load() != 0 {
		t.Fatal("Work cleanup invoked an external Model")
	}
}

func (v *workCleanupPG) otherWork(t *testing.T) {
	t.Helper()
	v.preserved(t)
	row, err := v.workPair.Reader().GetMilestone(ctxFor(t), v.base.ownerBrowser.actor, v.base.project.ID, v.other)
	schedulerOwnedRequire(t, "work-cleanup-b-original-reader", err)
	if row.Validate() != nil || row.ID != v.other || row.ProjectID != v.base.project.ID {
		t.Fatal("A cleanup stopped B's original reader")
	}
	v.preserved(t)
}

func (v *workCleanupPG) rejectWorkLedgerMutation(t *testing.T) {
	t.Helper()
	p := v.work.base.project.ID
	before := v.workRows(t, p, true)
	lock, _ := f.ProjectLock(p.String())
	for _, sql := range []string{`DELETE FROM agenteam_work.project_calls WHERE project_id=$1::uuid`, `UPDATE agenteam_work.project_calls SET lane='structure' WHERE project_id=$1::uuid AND lane='task'`} {
		cause, _ := f.NewRecoveryCause("work-cleanup-ordinary-guard", id[struct{}](t).String(), p.String())
		r := v.base.tracked.WithinTx(ctxFor(t), cause, func(ctx context.Context, tx f.Tx) error {
			if err := v.base.tracked.AcquireAll(ctx, tx, []f.LockRequest{{Key: lock, Mode: f.Exclusive}}); err != nil {
				return err
			}
			x, err := v.base.tracked.InTx(tx)
			if err != nil {
				return err
			}
			_, err = x.Exec(ctx, sql, p.String())
			return err
		})
		var pg *postgres.Error
		if r.State() != f.NotCommitted || !errors.As(r.Fault(), &pg) || pg.SQLState() != "23514" || before != v.workRows(t, p, true) {
			t.Fatal("ordinary Work history mutation bypassed its immutable guard")
		}
	}
}

func (v *workCleanupPG) runWork(d *project.LifecycleCleanupDriver) *skillsCleanupPGCall {
	v.startedWork = true
	return v.start(func(ctx context.Context) error { return d.Run(ctx, v.scope.ProjectID, v.cause.OperationID) })
}

func (v *workCleanupPG) workRound(t *testing.T) {
	t.Helper()
	call := v.runWork(v.cleanup)
	outboxCleanupWait(t, call.done, "original Work cleanup round")
	schedulerOwnedRequire(t, "work-cleanup-original-round", call.err)
}

func workCleanupNext(counts [19]int64) string {
	for n, count := range counts {
		if count != 0 {
			return workCleanupTables[n]
		}
	}
	return "completed"
}

func workCleanupBatchAfter(before [19]int64) [19]int64 {
	after := before
	for n, count := range before {
		if count == 0 {
			continue
		}
		limit := int64(100)
		switch workCleanupTables[n] {
		case "task_events":
			limit = 24
		case "tasks":
			limit = 16
		case "milestones", "sprints":
			limit = 32
		case "task_launch_failures", "task_busy_compensations", "task_scheduler_claims", "task_scheduler_relaunches", "task_blocker_commands", "task_transition_commands", "task_commands", "sprint_start_commands", "structure_commands":
			limit = 1
		}
		after[n] -= min(limit, count)
		break
	}
	return after
}

func (v *workCleanupPG) progress(t *testing.T, state, claim, stage string) {
	t.Helper()
	var operation, parentStage, actual, phase string
	var schema int64
	var raw []byte
	schedulerOwnedRequire(t, "work-cleanup-original-progress", v.base.raw.QueryRow(ctxFor(t), `SELECT o.state,o.cleanup_stage,p.cleanup_state,w.phase,coalesce(p.checkpoint_schema,0),p.checkpoint FROM agenteam_project.lifecycle_operations o JOIN agenteam_project.lifecycle_participants p ON p.operation_id=o.id AND p.participant_name='work' JOIN agenteam_project.work_claims w ON w.work_kind='lifecycle' AND w.work_id=o.id WHERE o.id=$1::uuid AND o.project_id=$2::uuid`, v.cause.OperationID.String(), v.scope.ProjectID.String()).Scan(&operation, &parentStage, &actual, &phase, &schema, &raw))
	if operation != "cleaning" || parentStage != "domains" || actual != state || phase != claim {
		t.Fatal("Work cleanup changed original Project progress", operation, parentStage, actual, phase)
	}
	if stage == "" {
		if schema != 0 || len(raw) != 0 {
			t.Fatal("unapplied Work cleanup wrote a checkpoint")
		}
		return
	}
	var cp wc.ProjectCleanupProgress
	if schema != 1 || json.Unmarshal(raw, &cp) != nil || cp.Validate() != nil || cp.Stage != stage || cp.ProjectID != v.scope.ProjectID || cp.OperationID != v.cause.OperationID || cp.ProjectVersion != v.cause.ProjectVersion || cp.ProcessID != v.skills.process || cp.InstanceID != v.instance {
		t.Fatal("Work checkpoint lost original identity or stage")
	}
}

func (v *workCleanupPG) advanceBatch(t *testing.T) {
	t.Helper()
	before := v.counts(t)
	want := workCleanupBatchAfter(before)
	v.workRound(t)
	if got := v.counts(t); got != want {
		t.Fatal("original bounded Work batch changed a different stage", before, got, want)
	}
	state := "pending"
	if want == [19]int64{} {
		state = "completed"
		v.finished = true // Only after this original Run actually returned.
	}
	v.progress(t, state, "terminal", workCleanupNext(want))
	v.preserved(t)
}

func (v *workCleanupPG) finishWork(t *testing.T) {
	t.Helper()
	// This is one explicit call per finite, already-created batch. The bound
	// derives from original row counts, not elapsed time or a retry budget.
	var remaining int64
	for _, count := range v.counts(t) {
		remaining += count
	}
	for n := int64(0); !v.finished && n <= remaining; n++ {
		v.advanceBatch(t)
	}
	if !v.finished || v.counts(t) != [19]int64{} {
		t.Fatal("Work completed with retained original rows")
	}
	stable := v.fullSnapshot(t)
	v.workRound(t)
	if stable != v.fullSnapshot(t) {
		t.Fatal("Completed Work replay changed original facts")
	}
	v.otherWork(t)
}

func (v *workCleanupPG) busyWork(t *testing.T, original *skillsCleanupPGCall) {
	t.Helper()
	if original != nil {
		select {
		case <-original.done:
			t.Fatal("original Work Run returned before its Store")
		default:
		}
	}
	stable := v.fullSnapshot(t)
	call := v.runWork(v.newWorkDriver(t))
	outboxCleanupWait(t, call.done, "same-Life second Work cleanup driver")
	if !schedulerOwnedCode(call.err, f.ResourceBusy) || stable != v.fullSnapshot(t) {
		agentRetirementDiagnostic(t, "work-cleanup-second-driver", call.err)
		t.Fatal("another driver crossed the original Work return/retained round")
	}
}

func (v *workCleanupPG) workProviderCause(cause f.TransactionCause) bool {
	d := cause.Details()
	return cause.Kind() == f.RecoveryCause && d.Owner == "work-project-cleanup" && d.CheckpointRef == v.scope.ProjectID.String()
}

func workCleanupSameCause(a, b f.TransactionCause) bool {
	if a.Kind() == f.JobCause && b.Kind() == f.JobCause {
		x, y := a.Details(), b.Details()
		return x.JobType == y.JobType && x.JobID == y.JobID && x.JobAttemptID == y.JobAttemptID
	}
	return schedulerOwnedSameCause(a, b)
}

// The preflight and mutation share a Recovery cause. Select only a real
// decrease inside that original callback, not merely a matching cause.
func (v *workCleanupPG) holdWorkDelete(t *testing.T, mask bool) (*skillsCleanupPGCall, *schedulerStopBarrier, *f.CommitResult) {
	t.Helper()
	s, barrier := v.base.tracked, v.barrier()
	var baselines sync.Map
	var selected atomic.Bool
	var wrapped atomic.Bool
	var original f.CommitResult
	attempt := id[f.TransactionAttempt](t)
	s.mu.Lock()
	s.before = func(ctx context.Context, tx f.Tx, cause f.TransactionCause) error {
		if !v.workProviderCause(cause) {
			return nil
		}
		x, err := s.InTx(tx)
		if err != nil {
			return err
		}
		var n int64
		err = x.QueryRow(ctx, `SELECT count(*) FROM agenteam_work.task_events WHERE project_id=$1::uuid`, v.scope.ProjectID.String()).Scan(&n)
		baselines.Store(tx, n)
		return err
	}
	s.after = func(ctx context.Context, tx f.Tx, cause f.TransactionCause) error {
		if !v.workProviderCause(cause) {
			return nil
		}
		before, ok := baselines.LoadAndDelete(tx)
		if !ok {
			return errors.New("Work mutation selector lost its original callback")
		}
		x, err := s.InTx(tx)
		if err != nil {
			return err
		}
		var after int64
		if err = x.QueryRow(ctx, `SELECT count(*) FROM agenteam_work.task_events WHERE project_id=$1::uuid`, v.scope.ProjectID.String()).Scan(&after); err != nil {
			return err
		}
		if before.(int64) > after {
			selected.Store(true)
		}
		return nil
	}
	s.afterResult = func(cause f.TransactionCause, result f.CommitResult) {
		if v.workProviderCause(cause) && selected.Load() {
			barrier.hold(result)
		}
	}
	if mask {
		s.transformResult = func(cause f.TransactionCause, result f.CommitResult) f.CommitResult {
			if v.workProviderCause(cause) && selected.Load() && result.State() == f.Committed && wrapped.CompareAndSwap(false, true) {
				original = f.UnknownResult(attempt, cause)
				return original
			}
			return result
		}
	}
	s.mu.Unlock()
	return v.runWork(v.cleanup), barrier, &original
}

// These observations use the Work participant's own current claim and row
// version, including xmin, to distinguish a Project write from its read phase.
func (v *workCleanupPG) holdWorkProject(t *testing.T, registration, rollback, mask bool) (*skillsCleanupPGCall, *schedulerStopBarrier, *f.CommitResult) {
	t.Helper()
	s, barrier := v.base.tracked, v.barrier()
	var selected, wrapped atomic.Bool
	var original f.CommitResult
	attempt := id[f.TransactionAttempt](t)
	sentinel := errors.New("controlled original Work registration rollback")
	s.setAfter(func(ctx context.Context, tx f.Tx, cause f.TransactionCause) error {
		if !v.retirementCause(cause) {
			return nil
		}
		x, err := s.InTx(tx)
		if err != nil {
			return err
		}
		var exact bool
		phase := "terminal"
		if registration {
			phase = "running"
		}
		err = x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_project.work_claims w JOIN agenteam_project.lifecycle_participants p ON p.operation_id=w.work_id AND p.participant_name='work' WHERE w.work_kind='lifecycle' AND w.work_id=$1::uuid AND w.project_id=$2::uuid AND w.attempt_id::text=$3 AND w.phase=$4 AND p.attempt_count>0 AND p.xmin::text=pg_current_xact_id()::text AND w.xmin::text=pg_current_xact_id()::text)`, v.cause.OperationID.String(), v.scope.ProjectID.String(), cause.Details().JobAttemptID, phase).Scan(&exact)
		if err != nil {
			return err
		}
		if exact {
			selected.Store(true)
			if rollback {
				return sentinel
			}
		}
		return nil
	})
	s.mu.Lock()
	s.afterResult = func(cause f.TransactionCause, result f.CommitResult) {
		if v.retirementCause(cause) && selected.Load() && (!rollback || errors.Is(result.Fault(), sentinel)) {
			barrier.hold(result)
		}
	}
	if mask {
		s.transformResult = func(cause f.TransactionCause, result f.CommitResult) f.CommitResult {
			want := f.Committed
			if rollback {
				want = f.NotCommitted
			}
			if v.retirementCause(cause) && selected.Load() && result.State() == want && (!rollback || errors.Is(result.Fault(), sentinel)) && wrapped.CompareAndSwap(false, true) {
				original = f.UnknownResult(attempt, cause)
				return original
			}
			return result
		}
	}
	s.mu.Unlock()
	return v.runWork(v.cleanup), barrier, &original
}

func (v *workCleanupPG) requireWorkUnknown(t *testing.T, err error, original f.CommitResult, provider bool) {
	t.Helper()
	var problem *f.Fault
	if !errors.As(err, &problem) || problem.Code != f.CommitUnknown || problem.CommitState != f.Unknown || problem.CauseID != original.AttemptID().String() || original.State() != f.Unknown || original.AttemptID().Validate() != nil {
		agentRetirementDiagnostic(t, "work-cleanup-original-unknown", err)
		t.Fatal("Work Unknown lost its original attempt")
	}
	if !provider {
		actual, ok := project.UnknownAttempt(err)
		if !ok || actual.AttemptID() != original.AttemptID() || !workCleanupSameCause(actual.Cause(), original.Cause()) {
			t.Fatal("Project Unknown changed its original Work cause")
		}
	}
}

func (v *workCleanupPG) confirmWork(t *testing.T, original f.CommitResult, provider bool) {
	t.Helper()
	stable := v.fullSnapshot(t)
	s, barrier := v.base.tracked, v.barrier()
	var baseline sync.Map
	var seen atomic.Int64
	match := func(c f.TransactionCause) bool {
		return workCleanupSameCause(c, original.Cause()) || provider && v.retirementCause(c)
	}
	s.mu.Lock()
	s.before = func(ctx context.Context, tx f.Tx, cause f.TransactionCause) error {
		if !match(cause) {
			return nil
		}
		x, err := s.InTx(tx)
		if err != nil {
			return err
		}
		var writes int64
		err = x.QueryRow(ctx, schedulerCleanupWritesSQL).Scan(&writes)
		baseline.Store(tx, writes)
		return err
	}
	s.after = func(ctx context.Context, tx f.Tx, cause f.TransactionCause) error {
		if !match(cause) {
			return nil
		}
		before, ok := baseline.LoadAndDelete(tx)
		if !ok {
			return errors.New("Work confirmation lost its original transaction baseline")
		}
		x, err := s.InTx(tx)
		if err != nil {
			return err
		}
		var writes int64
		if err = x.QueryRow(ctx, schedulerCleanupWritesSQL).Scan(&writes); err != nil {
			return err
		}
		if writes != before.(int64) {
			return errors.New("Work confirmation wrote inside its original transaction")
		}
		seen.Add(1)
		return nil
	}
	s.afterResult = func(cause f.TransactionCause, result f.CommitResult) {
		if workCleanupSameCause(cause, original.Cause()) {
			barrier.hold(result)
		}
	}
	s.mu.Unlock()
	call := v.runWork(v.cleanup)
	v.await(t, call, barrier)
	if stable != v.fullSnapshot(t) {
		t.Fatal("Work confirmation changed original complete facts before returning")
	}
	v.busyWork(t, call)
	barrier.open()
	outboxCleanupWait(t, call.done, "original Work read-only confirmation return")
	schedulerOwnedRequire(t, "work-cleanup-readonly-return", call.err)
	v.clear()
	want := int64(1)
	if provider {
		want = 2 // original Project round read, then the original provider read
	}
	if seen.Load() != want || stable != v.fullSnapshot(t) {
		t.Fatal("confirmation performed a new batch or lost an original read", seen.Load(), want)
	}
	v.preserved(t)
}

func testWorkCleanupReturn(t *testing.T) {
	v := newWorkCleanupPG(t, true)
	before := v.counts(t)
	call, barrier, _ := v.holdWorkDelete(t, false)
	v.await(t, call, barrier)
	if v.counts(t) != workCleanupBatchAfter(before) {
		t.Fatal("held original Work mutation did not remove exactly its first batch")
	}
	v.progress(t, "required", "running", "")
	v.busyWork(t, call)
	v.otherWork(t)
	barrier.open()
	outboxCleanupWait(t, call.done, "original Work batch Store return")
	schedulerOwnedRequire(t, "work-cleanup-held-provider-return", call.err)
	v.clear()
	v.progress(t, "pending", "terminal", workCleanupNext(v.counts(t)))
	// Keep the original final Project checkpoint return distinct from the
	// provider's physical deletion. All earlier batches return explicitly.
	var bound int64
	for _, count := range v.counts(t) {
		bound += count
	}
	for n := int64(0); n < bound && workCleanupNext(v.counts(t)) != "project_calls"; n++ {
		v.advanceBatch(t)
	}
	if workCleanupNext(v.counts(t)) != "project_calls" {
		t.Fatal("original closed ledger was not the final Work batch")
	}
	call, barrier, _ = v.holdWorkProject(t, false, false, false)
	v.await(t, call, barrier)
	if v.counts(t) != [19]int64{} {
		t.Fatal("final Work checkpoint preceded complete original history deletion")
	}
	v.progress(t, "completed", "terminal", "completed")
	v.busyWork(t, call)
	v.otherWork(t)
	barrier.open()
	outboxCleanupWait(t, call.done, "original completed Work checkpoint Store return")
	schedulerOwnedRequire(t, "work-cleanup-final-checkpoint-return", call.err)
	v.clear()
	v.finished = true
	v.finishWork(t)
	t.Log("PROJECT_WORK_CLEANUP_RETURN original_with_readers_before_seed=true real_five_lane_history_and_relations=true migration84_85_repeat=true ordinary_ledger_mutation_rejected=true scheduler_execution_actual_completed=true all_19_tables_empty=true original_provider_and_project_returns=true same_life_busy=true other_project_usable=true parent_domains=true empty_relaunch_compensation_boundary=true no_invoke_no_default_registry=true")
}

func testWorkCleanupUnknown(t *testing.T) {
	v := newWorkCleanupPG(t, false)
	before := v.counts(t)
	call, barrier, original := v.holdWorkDelete(t, true)
	v.await(t, call, barrier)
	v.busyWork(t, call)
	barrier.open()
	outboxCleanupWait(t, call.done, "original Work provider acknowledgement mask")
	v.clear()
	v.requireWorkUnknown(t, call.err, *original, true)
	if v.counts(t) != workCleanupBatchAfter(before) {
		t.Fatal("provider Unknown lost its physically applied original batch")
	}
	v.progress(t, "required", "running", "")
	v.confirmWork(t, *original, true)
	v.progress(t, "required", "running", "")
	// This next explicit Run consumes the already-returned conservative report;
	// it must checkpoint it without performing the provider's next batch.
	stable := v.workRows(t, v.scope.ProjectID, true)
	call, barrier, original = v.holdWorkProject(t, false, false, true)
	v.await(t, call, barrier)
	if stable != v.workRows(t, v.scope.ProjectID, true) {
		t.Fatal("checkpoint re-entered Work instead of consuming the original report")
	}
	v.busyWork(t, call)
	barrier.open()
	outboxCleanupWait(t, call.done, "original Work Project checkpoint acknowledgement mask")
	v.clear()
	v.requireWorkUnknown(t, call.err, *original, false)
	v.progress(t, "pending", "terminal", "task_events")
	v.confirmWork(t, *original, false)
	v.progress(t, "pending", "terminal", "task_events")
	v.finishWork(t)
	t.Log("PROJECT_WORK_CLEANUP_UNKNOWN local_provider_and_checkpoint_ack_masks=true first_attempt_cause_preserved=true original_retained_round=true both_confirmations_same_tx_zero_writes=true complete_snapshot_unchanged=true confirmation_actual_return=true next_explicit_run_only=true all_19_tables_empty=true other_project_preserved=true no_network_lost_ack=true")
}

func testWorkCleanupRegistration(t *testing.T) {
	v := newWorkCleanupPG(t, false)
	stable := v.fullSnapshot(t)
	call, barrier, original := v.holdWorkProject(t, true, true, true)
	select {
	case result := <-barrier.entered:
		if result.State() != f.NotCommitted {
			t.Fatal("original Work registration did not roll back")
		}
	case <-call.done:
		schedulerOwnedRequire(t, "work-cleanup-before-registration-barrier", call.err)
		t.Fatal("original Work registration return not held")
	case <-time.After(5 * time.Second):
		t.Fatal("original Work registration return barrier not reached")
	}
	if stable != v.fullSnapshot(t) {
		t.Fatal("registration rollback changed original facts")
	}
	v.busyWork(t, call)
	barrier.open()
	outboxCleanupWait(t, call.done, "original Work registration rollback acknowledgement")
	v.clear()
	v.requireWorkUnknown(t, call.err, *original, false)
	v.progress(t, "required", "terminal", "")
	v.confirmWork(t, *original, false)
	if stable != v.fullSnapshot(t) {
		t.Fatal("registration confirmation wrote or advanced Work")
	}
	v.busyWork(t, nil)
	v.finishWork(t)
	t.Log("PROJECT_WORK_CLEANUP_REGISTRATION real_registration_rollback=true local_ack_unknown=true original_attempt_cause=true same_tx_zero_write_confirmation=true full_snapshot_unchanged=true held_confirmation_same_life_busy=true next_run_registers_and_cleans=true all_19_tables_empty=true parent_domains=true no_default_registry=true")
}

func (v *workCleanupPG) closeWorkCleanup(t *testing.T) {
	t.Helper()
	for _, barrier := range v.barriers {
		barrier.open()
	}
	for _, call := range v.calls {
		outboxCleanupWait(t, call.done, "original Work cleanup calls before shutdown")
	}
	v.clear()
	if v.startedWork && !v.finished && v.cleanup != nil {
		var remaining int64 = 4 // at most two confirmations plus their checkpoints
		for _, count := range v.counts(t) {
			remaining += count
		}
		for n := int64(0); n <= remaining; n++ {
			call := v.runWork(v.cleanup)
			outboxCleanupWait(t, call.done, "original Work recovery before global Stop")
			if call.err != nil {
				t.Error("original Work cleanup did not settle", call.err)
				break
			}
			var completed bool
			err := v.base.raw.QueryRow(ctxFor(t), `SELECT cleanup_state='completed' FROM agenteam_project.lifecycle_participants WHERE operation_id=$1::uuid AND participant_name='work'`, v.cause.OperationID.String()).Scan(&completed)
			if err != nil {
				t.Error("original Work cleanup progress unavailable", err)
				break
			}
			if completed {
				v.finished = true
				break
			}
		}
	}
	for _, driver := range v.workDrivers {
		schedulerOwnedDrain(t, driver)
	}
	v.closeRetirement(t)
}
