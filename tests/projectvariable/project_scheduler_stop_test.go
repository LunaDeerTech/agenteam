//go:build integration

package projectvariable_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/agent"
	"github.com/LunaDeerTech/agenteam/internal/central/audit"
	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/execution"
	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/outbox"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/LunaDeerTech/agenteam/internal/central/project"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/scheduler"
	"github.com/LunaDeerTech/agenteam/internal/central/tool/builtin"
	"github.com/LunaDeerTech/agenteam/internal/central/tool/registry"
	toolruntime "github.com/LunaDeerTech/agenteam/internal/central/tool/runtime"
	"github.com/LunaDeerTech/agenteam/internal/central/work"
	wc "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
	assembly "github.com/LunaDeerTech/agenteam/tests/testsupport/agentconfiguration"
	pgfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/postgres"
)

// Only the explicit owned factory is bound. The acceptance-only Registry is
// never executed. This slice stops Scheduler calls; it neither deletes their
// history nor proves the default App, Archive or a borrowed Execution owner.
func TestProjectSchedulerStop(t *testing.T) {
	t.Run("seven-entry-owners-upgrade-and-original-return", testProjectSchedulerStopReturn)
	t.Run("technical-unknown-readonly-confirmation", testProjectSchedulerStopTechnicalUnknown)
	t.Run("known-not-created-original-acknowledgement", testProjectSchedulerStopOutcome)
}

type schedulerOwnedStopPG struct {
	skills         *skillsCleanupPG
	base           *variableHTTPFixture
	work           *taskTransitionFixture
	life           *project.LifecycleAuthority
	projects       *project.Service
	deps           scheduler.ProjectCallDependencies
	pair           *scheduler.ProjectCallOwnership
	pairs          []*scheduler.ProjectCallOwnership
	launcher       *countedSchedulerExecution
	driver         *project.LifecycleStopDriver
	step           func(context.Context, i.Actor, pc.LifecycleCause, pc.ScopeRef) error
	cause          pc.LifecycleCause
	scope          pc.ScopeRef
	last           pc.StopReport
	calls          []*skillsCleanupPGCall
	barriers       []*schedulerStopBarrier
	claim          wc.TaskClaimRequest
	policy         ec.Policy
	dispatch       scheduler.Dispatch
	runners        []*scheduler.ProjectRunner
	managers       []*scheduler.ProjectRunners
	recoverOnClose bool
}

func schedulerOwnedRequire(t *testing.T, stage string, err error) {
	t.Helper()
	if err != nil {
		agentRetirementDiagnostic(t, "scheduler-owned-"+stage, err)
		t.Fatalf("original Scheduler chain failed at %s", stage)
	}
}

func schedulerOwnedDrain(t *testing.T, owner interface {
	Stop()
	Drain(context.Context) error
}) {
	t.Helper()
	owner.Stop()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	err := owner.Drain(ctx)
	cancel()
	if err != nil {
		t.Error("original Scheduler fixture dependency did not return", err)
		_ = owner.Drain(context.Background())
	}
}

func newSchedulerOwnedStopPG(t *testing.T, upgrade bool, retirement ...*executionRetirementPGAssembly) *schedulerOwnedStopPG {
	t.Helper()
	if len(retirement) > 1 || len(retirement) == 1 && (retirement[0] == nil || upgrade) {
		t.Fatal("invalid optional retirement assembly")
	}
	var base *variableHTTPFixture
	if len(retirement) == 1 {
		base = retirement[0].base
		base.executionRetirementAssembly = retirement[0]
	} else if upgrade {
		db := pgfixture.NewDatabase(t)
		migrate(t, db, migrationPrefix(t, "00080"))
		raw := openStore(t, db.Config(t, nil))
		base = assembleVariableHTTPFixture(t, db, raw, &hookStore{fixtureStore: raw})
	} else {
		base = newVariableHTTPFixture(t)
	}
	v := &schedulerOwnedStopPG{base: base}
	v.skills = newSkillsCleanupPG(t, base)
	agentID := v.skills.disabledAgent(t)
	entries := v.skills.p2.manifest.Entries()
	if len(retirement) == 0 {
		entries = append(entries, pc.ParticipantRegistration{Name: pc.SchedulerParticipant, ContractVersion: 1, OwnerModule: "scheduler", ReferenceKinds: []pc.ReferenceKind{"scheduler-call"}})
		for n := range entries {
			if entries[n].Name == pc.OutboxParticipant || entries[n].Name == pc.AuditParticipant {
				entries[n].CleanupAfter = append(entries[n].CleanupAfter, pc.SchedulerParticipant)
			}
		}
	}
	manifest, err := pc.NewRequiredManifest(entries)
	schedulerOwnedRequire(t, "manifest", err)
	if len(retirement) == 1 {
		v.life = v.skills.p2.lifecycle
	} else {
		v.life, err = project.NewLifecycleAuthority(base.tracked, manifest, nil)
	}
	schedulerOwnedRequire(t, "lifecycle", err)
	source, err := toolruntime.NewInstallSource(v.skills.installer, v.skills.p2.service)
	schedulerOwnedRequire(t, "original-install-source", err)
	graph, err := assembly.New(base.tracked, assembly.Options{Accounts: base.accounts, Lifecycle: v.life, BuiltinSources: map[string]registry.BuiltinSource{builtin.SkillInstallStableKey: source}, InstallSourceKey: builtin.SkillInstallStableKey})
	schedulerOwnedRequire(t, "real-authorities", err)
	providers := graph.Providers()
	wa, err := work.NewAuthority(base.tracked, providers.Projects)
	schedulerOwnedRequire(t, "work-authority", err)
	reader, err := work.NewReader(base.tracked, wa, base.keys)
	schedulerOwnedRequire(t, "work-reader", err)
	pending, err := scheduler.NewPendingAuthority(base.tracked)
	schedulerOwnedRequire(t, "pending-authority", err)
	occupancy, err := execution.NewWorkOccupancy(base.tracked)
	schedulerOwnedRequire(t, "actual-occupancy", err)
	current, err := agent.NewSchedulerCurrent(providers.Agents, pending)
	schedulerOwnedRequire(t, "current-agent", err)
	observations, err := execution.NewDispatchObservation(base.tracked)
	schedulerOwnedRequire(t, "actual-dispatch-observer", err)
	catalog := event.NewCatalog()
	structures, err := wc.RegisterWorkEvents(catalog)
	schedulerOwnedRequire(t, "structure-events", err)
	tasks, err := wc.RegisterTaskEvents(catalog)
	schedulerOwnedRequire(t, "task-events", err)
	transitions, err := wc.RegisterTaskTransitionEvents(catalog)
	schedulerOwnedRequire(t, "transition-events", err)
	claims, err := wc.RegisterSchedulerClaimEvents(catalog)
	schedulerOwnedRequire(t, "claim-events", err)
	busyEvents, err := wc.RegisterTaskBusyCompensationEvents(catalog)
	schedulerOwnedRequire(t, "busy-events", err)
	failureEvents, err := wc.RegisterTaskLaunchFailureEvents(catalog)
	schedulerOwnedRequire(t, "failure-events", err)
	projectEvents, err := pc.RegisterProjectEvents(catalog)
	schedulerOwnedRequire(t, "project-events", err)
	auditing, err := audit.New(base.tracked, base.keys, audit.Authorizations{Sessions: base.accounts, System: base.accounts, Accounts: base.accounts, Projects: providers.AuditProjects})
	schedulerOwnedRequire(t, "actual-audit", err)
	processes := captureProviderProcesses{guard: v.skills.guard}
	box, err := outbox.New(base.tracked, catalog, outbox.Authorizations{Producers: map[event.StableName]oc.ProducerAuthority{wc.WorkProducer: wa, pc.ProjectProducer: providers.Projects}, Sessions: base.accounts, System: base.accounts, Projects: providers.Projects, Audit: auditing, Cursors: base.keys, Processes: processes})
	schedulerOwnedRequire(t, "actual-outbox", err)
	var bindings []project.LifecycleParticipantBinding
	for _, entry := range manifest.Entries() {
		p := &unavailableParticipant{name: entry.Name}
		bindings = append(bindings, project.LifecycleParticipantBinding{Registration: entry, Participant: p})
		t.Cleanup(func() {
			if p.calls.Load() != 0 {
				t.Error("acceptance-only Registry invoked an unavailable participant")
			}
		})
	}
	registry, err := project.NewLifecycleRegistry(manifest, bindings)
	schedulerOwnedRequire(t, "acceptance-registry", err)
	v.projects, err = project.New(base.tracked, project.Dependencies{Authority: providers.Projects, Activity: base.accounts, Audit: auditing, Events: box, ProjectEvents: projectEvents, Initializer: v.skills.p2.service, Processes: processes, Cursors: base.keys, LifecycleRegistry: registry}, project.DefaultConfig())
	schedulerOwnedRequire(t, "project-service", err)
	t.Cleanup(func() { schedulerOwnedDrain(t, v.projects) })
	projection := *base
	projection.project, projection.projectAuthority, projection.projects, projection.audit = v.skills.p2.project, providers.Projects, v.projects, auditing
	w := &taskTransitionFixture{base: &projection, agentID: agentID, authority: wa, structureReader: reader, pending: pending, box: box, transitionEvents: transitions}
	v.work = w
	w.structure, err = work.New(base.tracked, work.Dependencies{Authority: wa, Events: box, WorkEvents: structures, Activity: base.accounts})
	schedulerOwnedRequire(t, "work-service", err)
	t.Cleanup(func() { schedulerOwnedDrain(t, w.structure) })
	w.tasks, err = work.NewTask(base.tracked, work.TaskDependencies{Authority: wa, Structure: reader, Events: box, TaskEvents: tasks, Activity: base.accounts, Pending: pending})
	schedulerOwnedRequire(t, "task-service", err)
	t.Cleanup(func() { schedulerOwnedDrain(t, w.tasks) })
	w.taskReader, err = work.NewTaskReader(base.tracked, wa, reader, base.keys)
	schedulerOwnedRequire(t, "task-reader", err)
	w.transitions, err = work.NewTaskTransition(base.tracked, work.TaskTransitionDependencies{Authority: wa, Structure: reader, Events: box, TaskEvents: transitions, Activity: base.accounts, Agents: providers.Agents, Occupancy: occupancy, Pending: pending})
	schedulerOwnedRequire(t, "transition-service", err)
	t.Cleanup(func() { schedulerOwnedDrain(t, w.transitions) })
	claimWriter, err := work.NewSchedulerClaim(base.tracked, work.SchedulerClaimDependencies{Authority: wa, Scheduler: pending, Agents: current, Pending: pending, Occupancy: occupancy, Events: box, ClaimEvents: claims})
	schedulerOwnedRequire(t, "claim-writer", err)
	t.Cleanup(func() { schedulerOwnedDrain(t, claimWriter) })
	busyWriter, err := work.NewTaskBusyCompensation(base.tracked, work.TaskBusyCompensationDependencies{Authority: wa, Scheduler: pending, Pending: pending, Events: box, CompensationEvents: busyEvents})
	schedulerOwnedRequire(t, "busy-writer", err)
	t.Cleanup(func() { schedulerOwnedDrain(t, busyWriter) })
	failureWriter, err := work.NewTaskLaunchFailure(base.tracked, work.TaskLaunchFailureDependencies{Authority: wa, Scheduler: pending, Pending: pending, Events: box, FailureEvents: failureEvents})
	schedulerOwnedRequire(t, "failure-writer", err)
	t.Cleanup(func() { schedulerOwnedDrain(t, failureWriter) })
	relaunchWriter, err := work.NewTaskRelaunch(base.tracked, work.TaskRelaunchDependencies{Authority: wa, Scheduler: pending, Agents: current, Pending: pending, Occupancy: occupancy})
	schedulerOwnedRequire(t, "relaunch-writer", err)
	t.Cleanup(func() { schedulerOwnedDrain(t, relaunchWriter) })
	taskReader, err := work.NewSchedulerTaskReader(base.tracked, wa)
	schedulerOwnedRequire(t, "scheduler-task-reader", err)
	access, err := project.NewSchedulerExecutionAccess(providers.Projects, pending)
	schedulerOwnedRequire(t, "execution-access", err)
	executionAuthority, err := execution.NewAuthority(base.tracked, providers.Projects, access)
	schedulerOwnedRequire(t, "execution-authority", err)
	configuration, err := agent.NewExecutionConfiguration(providers.Agents, executionAuthority)
	schedulerOwnedRequire(t, "execution-agent", err)
	taskLaunch, err := work.NewTaskLaunchProvider(base.tracked, wa, pending)
	schedulerOwnedRequire(t, "task-launch-provider", err)
	executions, err := execution.New(base.tracked, execution.Dependencies{Authority: executionAuthority, Agents: configuration, Task: taskLaunch})
	schedulerOwnedRequire(t, "execution-launch-service", err)
	t.Cleanup(func() { schedulerOwnedDrain(t, executions) })
	v.launcher = &countedSchedulerExecution{service: executions}
	v.deps = scheduler.ProjectCallDependencies{
		Coordinator:   scheduler.CoordinatorDependencies{Projects: providers.Projects, Claims: claimWriter, Executions: observations, Capacity: observations},
		Handoff:       scheduler.LaunchHandoffDependencies{Executions: v.launcher, Observations: observations},
		Busy:          scheduler.BusyCompensatorDependencies{Projects: providers.Projects, Work: busyWriter},
		Failure:       scheduler.LaunchFailureFinalizerDependencies{Projects: providers.Projects, Work: failureWriter},
		RelaunchTasks: relaunchWriter, Tasks: taskReader, Occupancy: occupancy, RelaunchSkipCount: 1,
	}
	v.seedTask(t)
	v.claim, v.policy = schedulerClaimRequest(t, w)
	// The real v1 Task provider rejects this preserved opaque constraint.
	// It is not a fabricated Execution permit or a resource implementation.
	v.policy.AllowedResourceConstraints = []json.RawMessage{json.RawMessage(`{}`)}
	if upgrade {
		v.upgrade(t)
	}
	v.pair = v.newPair(t)
	v.dispatch, err = v.pair.Coordinator().ClaimTask(ctxFor(t), v.claim, v.policy)
	schedulerOwnedRequire(t, "owned-claim", err)
	if v.dispatch.Summary().Status != scheduler.Pending {
		t.Fatal("formal Claim did not produce Pending")
	}
	v.driver, err = project.NewLifecycleStopDriver(base.tracked, v.life, processes, func(ctx context.Context, actor i.Actor, cause pc.LifecycleCause, scope pc.ScopeRef) error {
		if v.step == nil || cause != v.cause || !scope.Equal(v.scope) {
			return errors.New("Scheduler original Stop scope changed")
		}
		return v.step(ctx, actor, cause, scope)
	})
	schedulerOwnedRequire(t, "stop-driver", err)
	t.Cleanup(func() { v.close(t) })
	return v
}

func (v *schedulerOwnedStopPG) seedTask(t *testing.T) {
	t.Helper()
	w, a, p := v.work, v.base.ownerBrowser.actor, v.work.base.project.ID
	milestone := wc.CreateMilestoneRequest{MilestoneID: id[wc.Milestone](t), Title: "Original Scheduler task"}
	_, err := w.structure.CreateMilestone(ctxFor(t), a, meta(t, "scheduler-owned-milestone", nil), p, milestone)
	schedulerOwnedRequire(t, "create-milestone", err)
	sprint := wc.CreateSprintRequest{SprintID: id[pc.Sprint](t), MilestoneID: milestone.MilestoneID, Title: "Original Scheduler sprint"}
	_, err = w.structure.CreateSprint(ctxFor(t), a, meta(t, "scheduler-owned-sprint", nil), p, sprint)
	schedulerOwnedRequire(t, "create-sprint", err)
	created, err := w.tasks.CreateTask(ctxFor(t), a, meta(t, "scheduler-owned-task", nil), p, wc.TaskCreate{TaskID: id[wc.Task](t), SprintID: sprint.SprintID, Title: "Real task without an Execution", Type: wc.TaskTypeTask, Priority: wc.TaskPriorityMedium, Plan: "Verify original call ownership"})
	schedulerOwnedRequire(t, "create-task", err)
	w.task = created.Task
	w.configureProject(t)
	moved, err := w.transitions.TransferTask(ctxFor(t), a, meta(t, "scheduler-owned-transfer", &w.task.Version), p, w.task.ID, wc.TaskTransfer{TargetState: wc.TaskStateTodo, AssigneeAgentID: &w.agentID, AddBlockers: []wc.TaskBlockerCreate{}, ResolveBlockerIDs: []wc.TaskBlockerID{}})
	schedulerOwnedRequire(t, "transfer-task", err)
	if moved.Validate() != nil || moved.Task.State != wc.TaskStateTodo {
		t.Fatal("formal Task transfer receipt invalid")
	}
	w.task = moved.Task
	startSchedulerSprint(t, w)
}

func (v *schedulerOwnedStopPG) newPair(t *testing.T) *scheduler.ProjectCallOwnership {
	t.Helper()
	a, err := project.NewSchedulerProjectStopAuthority(v.life, v.base.tracked)
	schedulerOwnedRequire(t, "stop-authority", err)
	p, err := scheduler.NewProjectCallOwnership(v.work.pending, v.deps, v.skills.guard, v.skills.process, a)
	schedulerOwnedRequire(t, "owned-factory", err)
	v.pairs = append(v.pairs, p)
	return p
}

func (v *schedulerOwnedStopPG) upgrade(t *testing.T) {
	t.Helper()
	legacy, err := scheduler.NewCoordinator(v.work.pending, v.deps.Coordinator)
	schedulerOwnedRequire(t, "legacy80-coordinator", err)
	t.Cleanup(func() { schedulerOwnedDrain(t, legacy) })
	original, err := legacy.ClaimTask(ctxFor(t), v.claim, v.policy)
	schedulerOwnedRequire(t, "legacy80-claim-return", err)
	if original.Summary().Status != scheduler.Pending {
		t.Fatal("original legacy Claim did not commit")
	}
	schedulerOwnedDrain(t, legacy)
	if !legacy.Joined() {
		t.Fatal("legacy original calls did not return before upgrade")
	}
	before := v.snapshot(t, false)
	for n := 0; n < 2; n++ {
		migrate(t, v.base.db, migrationPrefix(t, "00081"))
		var count int
		schedulerOwnedRequire(t, "migration81-no-manufactured-ownership", v.base.raw.QueryRow(ctxFor(t), `SELECT count(*) FROM agenteam_scheduler.project_calls`).Scan(&count))
		if count != 0 || before != v.snapshot(t, false) {
			t.Fatal("migration81/repeat changed business history or invented legacy ownership")
		}
	}
}

func (v *schedulerOwnedStopPG) snapshot(t *testing.T, ledger bool) string {
	t.Helper()
	// Scheduler/Execution business safe_id is TEXT; Work/Outbox and the new
	// ownership_id in migration81 are UUID. Cast the parameter, not columns.
	var parts []string
	for _, table := range []string{"dispatches", "task_runtimes", "relaunch_visits"} {
		parts = append(parts, fmt.Sprintf("(SELECT jsonb_agg(to_jsonb(x) ORDER BY to_jsonb(x)::text) FROM agenteam_scheduler.%s x WHERE project_id=$1::text)", table))
	}
	for _, table := range []string{"tasks", "task_events", "task_commands", "task_scheduler_claims", "task_scheduler_relaunches", "task_busy_compensations", "task_launch_failures", "task_order_groups", "task_query_generations", "task_blockers", "task_blocker_commands", "task_transition_commands"} {
		parts = append(parts, fmt.Sprintf("(SELECT jsonb_agg(to_jsonb(x) ORDER BY to_jsonb(x)::text) FROM agenteam_work.%s x WHERE project_id=$1::text::uuid)", table))
	}
	parts = append(parts, `(SELECT jsonb_agg(to_jsonb(x) ORDER BY to_jsonb(x)::text) FROM agenteam_execution.executions x WHERE project_id=$1::text)`, `(SELECT jsonb_agg(to_jsonb(x) ORDER BY to_jsonb(x)::text) FROM agenteam_outbox.events x WHERE project_id=$1::text::uuid AND producer='work')`)
	if ledger {
		parts = append(parts, `(SELECT jsonb_agg(to_jsonb(x) ORDER BY to_jsonb(x)::text) FROM agenteam_scheduler.project_calls x WHERE project_id=$1::text::uuid)`)
	}
	var snapshot string
	schedulerOwnedRequire(t, "business-snapshot", v.base.raw.QueryRow(ctxFor(t), "SELECT jsonb_build_array("+strings.Join(parts, ",")+")::text", v.work.base.project.ID.String()).Scan(&snapshot))
	return snapshot
}

func (v *schedulerOwnedStopPG) accept(t *testing.T) {
	t.Helper()
	p := v.work.base.project
	var path string
	v.base.tx(t, ownerLocks(v.base.ownerBrowser.actor, p.ID), func(ctx context.Context, tx f.Tx, _ postgres.SQLExecutor) error {
		r, err := v.base.accounts.CurrentUserRouteInTx(ctx, tx, v.base.ownerBrowser.actor)
		if err != nil {
			return err
		}
		path, err = pc.NormalizeProjectPath(r.Username, p.Name)
		return err
	})
	r, err := v.projects.BeginDeleteProject(ctxFor(t), v.base.ownerBrowser.actor, meta(t, "scheduler-owned-delete", &p.Version), p.ID, pc.DeleteProjectRequest{NormalizedCurrentPath: path, Permanent: true})
	schedulerOwnedRequire(t, "formal-delete-acceptance", err)
	if r.Operation == nil {
		t.Fatal("formal Delete operation absent")
	}
	v.cause = pc.LifecycleCause{OperationID: r.Operation.ID, Action: pc.Delete, ProjectVersion: r.Operation.ProjectVersion}
	v.scope = pc.ScopeRef{Kind: pc.ProjectScope, ProjectID: p.ID}
}

func (v *schedulerOwnedStopPG) start(fn func(context.Context) error) *skillsCleanupPGCall {
	c := &skillsCleanupPGCall{done: make(chan struct{})}
	v.calls = append(v.calls, c)
	go func() {
		defer close(c.done)
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		c.err = fn(ctx)
	}()
	return c
}

func (v *schedulerOwnedStopPG) stop(t *testing.T, pair *scheduler.ProjectCallOwnership, inspect bool) *skillsCleanupPGCall {
	t.Helper()
	v.step = func(ctx context.Context, actor i.Actor, cause pc.LifecycleCause, scope pc.ScopeRef) error {
		var r pc.StopReport
		var err error
		if inspect {
			r, err = pair.InspectProjectStop(ctx, actor, cause, scope)
		} else {
			r, err = pair.RequestProjectStop(ctx, actor, cause, scope)
		}
		if err != nil {
			return err
		}
		if !r.Matches(pc.SchedulerParticipant, cause, scope) {
			return errors.New("Scheduler Stop report changed the original identity")
		}
		v.last = r
		return nil
	}
	return v.start(func(ctx context.Context) error { return v.driver.Run(ctx, v.work.base.project.ID, v.cause.OperationID) })
}

func (v *schedulerOwnedStopPG) observed(t *testing.T, pair *scheduler.ProjectCallOwnership, inspect bool, want pc.StopState) {
	t.Helper()
	c := v.stop(t, pair, inspect)
	outboxCleanupWait(t, c.done, "original Scheduler Stop return")
	schedulerOwnedRequire(t, "stop-return", c.err)
	if v.last.Details().State != want {
		t.Fatal("actual Scheduler Stop report differs", v.last.Details().State, want)
	}
}

func (v *schedulerOwnedStopPG) barrier() *schedulerStopBarrier {
	b := newSchedulerStopBarrier()
	v.barriers = append(v.barriers, b)
	return b
}

func (v *schedulerOwnedStopPG) await(t *testing.T, c *skillsCleanupPGCall, b *schedulerStopBarrier) {
	t.Helper()
	select {
	case result := <-b.entered:
		if result.State() != f.Committed {
			t.Fatal("selected original Store did not commit")
		}
	case <-c.done:
		schedulerOwnedRequire(t, "before-original-return", c.err)
		t.Fatal("selected original Store return was not held")
	case <-time.After(5 * time.Second):
		t.Fatal("original Scheduler return barrier not reached")
	}
}

func (v *schedulerOwnedStopPG) clear() {
	s := v.base.tracked
	s.mu.Lock()
	s.enter, s.before, s.after, s.afterResult, s.transformResult = nil, nil, nil, nil, nil
	s.mu.Unlock()
}

func (v *schedulerOwnedStopPG) other(t *testing.T) {
	t.Helper()
	_, err := v.pair.Visitor().VisitNext(ctxFor(t), v.base.project.ID, nil)
	schedulerOwnedRequire(t, "other-project-original-visitor", err)
	var closed int
	schedulerOwnedRequire(t, "other-project-ledger", v.base.raw.QueryRow(ctxFor(t), `SELECT count(*) FROM agenteam_scheduler.project_calls WHERE project_id=$1::uuid AND lane='visitor' AND phase='business_closed'`, v.base.project.ID.String()).Scan(&closed))
	if closed == 0 {
		t.Fatal("other Project did not complete an actual owned entry")
	}
}

func (v *schedulerOwnedStopPG) noExecution(t *testing.T, launches int) {
	t.Helper()
	got, lookups, created := v.launcher.observed()
	var count int
	schedulerOwnedRequire(t, "no-execution", v.base.raw.QueryRow(ctxFor(t), `SELECT count(*) FROM agenteam_execution.executions WHERE project_id=$1::text`, v.work.base.project.ID.String()).Scan(&count))
	if got != launches || lookups != 0 || count != 0 || created.ID.Validate() == nil {
		t.Fatal("Stop created, looked up or retried an Execution", got, lookups, count)
	}
}

func (v *schedulerOwnedStopPG) close(t *testing.T) {
	t.Helper()
	for _, b := range v.barriers {
		b.open()
	}
	for _, manager := range v.managers {
		manager.Stop()
	}
	for _, runner := range v.runners {
		runner.Stop()
	}
	for _, c := range v.calls {
		outboxCleanupWait(t, c.done, "original Scheduler calls before shutdown")
	}
	v.clear()
	for _, manager := range v.managers {
		schedulerOwnedDrain(t, manager)
	}
	for _, runner := range v.runners {
		schedulerOwnedDrain(t, runner)
	}
	// A failed assertion after a local ACK mask must still release the original
	// business owner before global Stop. At most two retained entries were
	// created: confirmation and closure each, then observation. These are the
	// same public Stop calls and original scope, never a private reset.
	if v.recoverOnClose {
		if v.cause.Validate() != nil {
			v.accept(t)
		}
		for n := 0; n < 5; n++ {
			call := v.stop(t, v.pair, false)
			outboxCleanupWait(t, call.done, "original retained Scheduler owners during cleanup")
			if call.err != nil {
				t.Error("original Scheduler recovery during cleanup failed", call.err)
				break
			}
			if v.last.Details().State == pc.Stopped {
				break
			}
		}
	}
	for _, pair := range v.pairs {
		schedulerOwnedDrain(t, pair)
		if !pair.Joined() {
			t.Error("original owned Scheduler pair did not join")
		}
	}
	schedulerOwnedDrain(t, v.driver)
}

func schedulerOwnedCode(err error, code f.Code) bool {
	var problem *f.Fault
	return errors.As(err, &problem) && problem.Code == code
}

func (v *schedulerOwnedStopPG) pause(t *testing.T) {
	t.Helper()
	w := v.work
	before, err := v.projects.GetSchedulerConfig(ctxFor(t), v.base.ownerBrowser.actor, w.base.project.ID)
	schedulerOwnedRequire(t, "read-before-pause", err)
	config := before.Config
	config.Enabled = false
	w.base.project, err = v.projects.UpdateProject(ctxFor(t), v.base.ownerBrowser.actor, meta(t, "scheduler-owned-pause", &before.Project.Version), before.Project.ID, pc.UpdateProjectRequest{Scheduler: &config})
	schedulerOwnedRequire(t, "formal-pause", err)
}

func (v *schedulerOwnedStopPG) runner(t *testing.T, p i.ProjectID) *scheduler.ProjectRunner {
	t.Helper()
	r, err := scheduler.NewProjectRunner(v.pair.Coordinator(), v.pair.Visitor(), scheduler.ProjectRunnerOptions{ProjectID: p, TickInterval: 20 * time.Millisecond, LaunchPolicy: v.policy, Tasks: v.deps.Tasks, Relaunch: v.pair.Relaunch()})
	schedulerOwnedRequire(t, "original-runner", err)
	v.runners = append(v.runners, r)
	return r
}

func (v *schedulerOwnedStopPG) entryFacts(t *testing.T) {
	t.Helper()
	p, dispatch := v.work.base.project.ID, v.dispatch.Summary().ID
	v.pause(t)
	_, err := v.pair.Visitor().VisitNext(ctxFor(t), p, nil)
	schedulerOwnedRequire(t, "paused-visitor-return", err)
	observed, err := v.pair.Handoff().Lookup(ctxFor(t), p, dispatch)
	schedulerOwnedRequire(t, "not-sent-handoff-read", err)
	if observed.Summary().LaunchOutcome != scheduler.NotSent {
		t.Fatal("original Handoff lookup changed the unsent Dispatch")
	}
	_, err = v.pair.Busy().Lookup(ctxFor(t), p, dispatch)
	if !schedulerOwnedCode(err, f.InvalidState) {
		schedulerOwnedRequire(t, "busy-original-rejection", err)
		t.Fatal("unsent Dispatch unexpectedly supplied a Busy receipt")
	}
	_, err = v.pair.Failure().Lookup(ctxFor(t), p, dispatch)
	if !schedulerOwnedCode(err, f.InvalidState) {
		schedulerOwnedRequire(t, "failure-original-rejection", err)
		t.Fatal("unsent Dispatch unexpectedly supplied a failure receipt")
	}
	r := wc.TaskRelaunchRequest{ProjectID: p, TaskID: v.work.task.ID, AgentID: v.work.agentID, CurrentSprintID: v.work.task.SprintID, ExpectedTaskVersion: v.work.task.Version, DispatchID: id[scheduler.DispatchIdentity](t).String(), RequestID: id[f.Request](t), Purpose: "task/work"}
	_, err = v.pair.Relaunch().ResolveRelaunch(ctxFor(t), r)
	if !schedulerOwnedCode(err, f.NotFound) {
		schedulerOwnedRequire(t, "relaunch-original-rejection", err)
		t.Fatal("missing original Relaunch was accepted")
	}
	_, err = v.runner(t, p).RunTraversal(ctxFor(t))
	schedulerOwnedRequire(t, "paused-runner-return", err)
	var lanes, active, instances int
	schedulerOwnedRequire(t, "seven-original-entry-rows", v.base.raw.QueryRow(ctxFor(t), `SELECT count(DISTINCT lane),count(*) FILTER(WHERE phase<>'business_closed'),count(DISTINCT instance_id) FROM agenteam_scheduler.project_calls WHERE project_id=$1::uuid`, p.String()).Scan(&lanes, &active, &instances))
	if lanes != 7 || active != 0 || instances != 1 {
		t.Fatal("seven validated original entry wrappers did not actually return", lanes, active, instances)
	}
	// Busy/Failure/Relaunch above are registered, returned rejections, not
	// successful compensation/finalization/relaunch business receipts.
	v.noExecution(t, 0)
}

// A controlled borrowed endpoint is deliberately unreachable: without an
// associated Execution the real Runner must never invoke Advance. Actual
// Advance return custody belongs to its separate controlled owner tests.
type schedulerOwnedNoExecution struct {
	ready    chan struct{}
	advances atomic.Int64
}

func (x *schedulerOwnedNoExecution) Ready() <-chan struct{} { return x.ready }
func (x *schedulerOwnedNoExecution) Advance(context.Context, i.ProjectID, ec.AssociatedDispatch) (ec.ExecutionAdvance, error) {
	x.advances.Add(1)
	return ec.ExecutionAdvance{}, errors.New("unexpected Advance without a real associated Execution")
}

func testProjectSchedulerStopReturn(t *testing.T) {
	v := newSchedulerOwnedStopPG(t, true)
	v.entryFacts(t)
	b := v.barrier()
	command, err := f.NewCommandIdentity("scheduler", []string{v.work.base.project.ID.String()}, "capture_traversal", "project_traversal")
	schedulerOwnedRequire(t, "capture-identity", err)
	var originalContext context.Context
	var mu sync.Mutex
	s := v.base.tracked
	s.mu.Lock()
	s.enter = func(ctx context.Context, c f.TransactionCause) {
		if c.Kind() == f.CommandsCause && c.Details().Primary.Canonical() == command.Canonical() {
			mu.Lock()
			originalContext = ctx
			mu.Unlock()
		}
	}
	s.afterResult = func(c f.TransactionCause, r f.CommitResult) {
		if c.Kind() == f.CommandsCause && c.Details().Primary.Canonical() == command.Canonical() && r.State() == f.Committed {
			b.hold(r)
		}
	}
	s.mu.Unlock()
	directory, err := project.NewSchedulerProjects(v.work.base.projectAuthority)
	schedulerOwnedRequire(t, "original-directory", err)
	borrowed := &schedulerOwnedNoExecution{ready: make(chan struct{})}
	close(borrowed.ready)
	manager, err := scheduler.NewProjectRunners(scheduler.ProjectRunnersDependencies{Projects: directory, Coordinator: v.pair.Coordinator(), Visitor: v.pair.Visitor(), Tasks: v.deps.Tasks, Relaunch: v.pair.Relaunch(), Executions: borrowed}, scheduler.ProjectRunnersOptions{MaxProjects: 4, ProjectPageSize: 16, ExecutionPageSize: 1, DiscoveryInterval: 20 * time.Millisecond, TickInterval: 20 * time.Millisecond, RelaunchSkipCount: 1, LaunchPolicy: v.policy})
	schedulerOwnedRequire(t, "original-manager", err)
	v.managers = append(v.managers, manager)
	call := v.start(manager.Run)
	v.await(t, call, b)
	mu.Lock()
	live := originalContext != nil && originalContext.Err() == nil
	mu.Unlock()
	if !live {
		t.Fatal("original runner entry context was not live before Delete")
	}
	v.accept(t)
	v.observed(t, v.pair, false, pc.StopPending)
	active := false
	for _, p := range manager.Observation().Projects {
		if p.ProjectID == v.work.base.project.ID {
			active = p.Running
		}
	}
	mu.Lock()
	canceled := originalContext != nil && errors.Is(originalContext.Err(), context.Canceled)
	mu.Unlock()
	if !active || !canceled {
		t.Fatal("Stop lost the held original manager/runner or failed to cancel its actual context")
	}
	v.other(t)
	b.open()
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(5 * time.Millisecond)
	defer tick.Stop()
	for {
		active = false
		for _, p := range manager.Observation().Projects {
			if p.ProjectID == v.work.base.project.ID {
				active = p.Running
			}
		}
		// runOne publishes Running=false just before its outer ownership
		// returned hook. Only the real Project Stop report closes that tail.
		observation := v.stop(t, v.pair, false)
		select {
		case <-observation.done:
			schedulerOwnedRequire(t, "manager-tail-stop-return", observation.err)
		case <-deadline.C:
			t.Fatal("original A manager/ownership tail did not return")
		}
		state := v.last.Details().State
		if state == pc.Stopped && !active {
			break
		}
		if state != pc.StopPending && state != pc.Stopped {
			t.Fatal("invalid original Scheduler Stop report")
		}
		select {
		case <-deadline.C:
			t.Fatal("original A manager call did not return")
		case <-tick.C:
		}
	}
	v.clear()
	stable := v.snapshot(t, true)
	v.observed(t, v.newPair(t), false, pc.StopPending)
	if stable != v.snapshot(t, true) {
		t.Fatal("same-process different instance rewrote original A facts")
	}
	v.other(t)
	v.noExecution(t, 0)
	if borrowed.advances.Load() != 0 {
		t.Fatal("manager invented a borrowed Execution association")
	}
	manager.Stop()
	outboxCleanupWait(t, call.done, "original whole manager return")
	t.Log("PROJECT_SCHEDULER_STOP_RETURN seven_registered_entry_lanes=true rejected_entries_not_business_success=true migration80_81_repeat=true original_legacy_return=true manager_and_runner_store_return=true pending_then_stopped=true other_project_live=true different_instance_pending=true advance_not_reached=true no_default_registry=true")
}

type schedulerOwnedPGTag struct{}
type schedulerOwnedUnknown struct {
	original f.CommitResult
	id       string
}

func schedulerOwnedSameCause(a, b f.TransactionCause) bool {
	x, y := a.Details(), b.Details()
	if a.Kind() != b.Kind() {
		return false
	}
	if a.Kind() == f.CommandsCause {
		return x.Primary.Canonical() == y.Primary.Canonical()
	}
	return x.Owner == y.Owner && x.RecoveryRunID == y.RecoveryRunID && x.CheckpointRef == y.CheckpointRef
}

func (v *schedulerOwnedStopPG) maskTechnical(t *testing.T, owner string) schedulerOwnedUnknown {
	t.Helper()
	v.clear()
	s := v.base.tracked
	var selected string
	var mu sync.Mutex
	var original f.CommitResult
	var once atomic.Bool
	s.mu.Lock()
	s.enter = func(ctx context.Context, cause f.TransactionCause) {
		d := cause.Details()
		if ctx.Value(schedulerOwnedPGTag{}) == owner && cause.Kind() == f.RecoveryCause && d.Owner == owner && d.CheckpointRef == v.work.base.project.ID.String() {
			mu.Lock()
			selected = d.RecoveryRunID
			mu.Unlock()
		}
	}
	s.transformResult = func(cause f.TransactionCause, result f.CommitResult) f.CommitResult {
		mu.Lock()
		exact := selected != "" && cause.Kind() == f.RecoveryCause && cause.Details().Owner == owner && cause.Details().RecoveryRunID == selected
		mu.Unlock()
		if exact && result.State() == f.Committed && once.CompareAndSwap(false, true) {
			original = f.UnknownResult(id[f.TransactionAttempt](t), cause)
			return original
		}
		return result
	}
	s.mu.Unlock()
	c := v.start(func(ctx context.Context) error {
		ctx = context.WithValue(ctx, schedulerOwnedPGTag{}, owner)
		if owner == "scheduler-call-register" {
			_, err := v.pair.Visitor().VisitNext(ctx, v.work.base.project.ID, nil)
			return err
		}
		_, err := v.pair.Handoff().Lookup(ctx, v.work.base.project.ID, v.dispatch.Summary().ID)
		return err
	})
	outboxCleanupWait(t, c.done, "original Scheduler technical acknowledgement")
	v.clear()
	v.recoverOnClose = once.Load()
	r, ok := scheduler.UnknownAttempt(c.err)
	if !once.Load() || !ok || r.AttemptID() != original.AttemptID() || !schedulerOwnedSameCause(r.Cause(), original.Cause()) {
		agentRetirementDiagnostic(t, "scheduler-technical-unknown", c.err)
		t.Fatal("original technical Unknown attempt/cause lost")
	}
	return schedulerOwnedUnknown{original: original, id: selected}
}

const schedulerOwnedWritesSQL = `SELECT COALESCE(sum(n_tup_ins+n_tup_upd+n_tup_del),0)::bigint FROM pg_stat_xact_user_tables WHERE schemaname IN ('agenteam_scheduler','agenteam_work','agenteam_execution','agenteam_outbox')`

// The exact original callback's own before/after counters avoid PostgreSQL's
// pending per-backend statistics from earlier transactions. No COMMIT or
// connection change occurs between these two observations.
func (v *schedulerOwnedStopPG) confirm(t *testing.T, original f.CommitResult) {
	t.Helper()
	v.clear()
	before := v.snapshot(t, true)
	s := v.base.tracked
	b := v.barrier()
	var baseline sync.Map
	var seen atomic.Int64
	exact := func(c f.TransactionCause) bool { return schedulerOwnedSameCause(c, original.Cause()) }
	s.mu.Lock()
	s.before = func(ctx context.Context, tx f.Tx, c f.TransactionCause) error {
		if !exact(c) {
			return nil
		}
		x, err := s.InTx(tx)
		if err != nil {
			return err
		}
		var count int64
		if err = x.QueryRow(ctx, schedulerOwnedWritesSQL).Scan(&count); err != nil {
			return err
		}
		baseline.Store(tx, count)
		return nil
	}
	s.after = func(ctx context.Context, tx f.Tx, c f.TransactionCause) error {
		if !exact(c) {
			return nil
		}
		before, ok := baseline.LoadAndDelete(tx)
		if !ok {
			return errors.New("original Scheduler confirmation lost its Tx baseline")
		}
		x, err := s.InTx(tx)
		if err != nil {
			return err
		}
		var count int64
		if err = x.QueryRow(ctx, schedulerOwnedWritesSQL).Scan(&count); err != nil {
			return err
		}
		if count-before.(int64) != 0 {
			return errors.New("original Scheduler confirmation wrote domain facts")
		}
		seen.Add(1)
		return nil
	}
	s.afterResult = func(c f.TransactionCause, r f.CommitResult) {
		if exact(c) && seen.Load() == 1 && r.State() == f.Committed {
			b.hold(r)
		}
	}
	s.mu.Unlock()
	c := v.stop(t, v.pair, false)
	v.await(t, c, b)
	select {
	case <-c.done:
		t.Fatal("original confirmation returned through its held Store")
	default:
	}
	if seen.Load() != 1 || before != v.snapshot(t, true) {
		t.Fatal("read-only confirmation changed original metadata")
	}
	v.other(t)
	b.open()
	outboxCleanupWait(t, c.done, "original read-only Scheduler confirmation")
	schedulerOwnedRequire(t, "original-confirmation-return", c.err)
	if seen.Load() != 1 || before != v.snapshot(t, true) || v.last.Details().State != pc.StopPending {
		t.Fatal("confirmation mutated facts or skipped its explicit Pending boundary")
	}
	v.clear()
}

func (v *schedulerOwnedStopPG) phase(t *testing.T, call string) string {
	t.Helper()
	var phase, process, instance string
	schedulerOwnedRequire(t, "original-ownership-row", v.base.raw.QueryRow(ctxFor(t), `SELECT phase,process_id::text,instance_id::text FROM agenteam_scheduler.project_calls WHERE id=$1::uuid AND project_id=$2::uuid`, call, v.work.base.project.ID.String()).Scan(&phase, &process, &instance))
	if process != v.skills.process.String() || instance == "" {
		t.Fatal("original process/instance tuple changed")
	}
	return phase
}

func testProjectSchedulerStopTechnicalUnknown(t *testing.T) {
	v := newSchedulerOwnedStopPG(t, false)
	v.pause(t)
	business := v.snapshot(t, false)
	registered := v.maskTechnical(t, "scheduler-call-register")
	returned := v.maskTechnical(t, "scheduler-call-return")
	if registered.id == returned.id || v.phase(t, registered.id) != "running" || v.phase(t, returned.id) != "business_closed" {
		t.Fatal("actual original technical outcomes differ")
	}
	v.accept(t)
	// Selection follows actual IDs, not an assumed UUIDv7 generation order.
	if returned.id < registered.id {
		v.confirm(t, returned.original)
	}
	v.confirm(t, registered.original)
	if v.phase(t, registered.id) != "running" {
		t.Fatal("registration confirmation wrote the close in the same round")
	}
	v.observed(t, v.pair, false, pc.StopPending)
	if v.phase(t, registered.id) != "business_closed" {
		t.Fatal("next explicit Request did not close the original registration")
	}
	if registered.id < returned.id {
		v.confirm(t, returned.original)
	}
	v.observed(t, v.pair, false, pc.Stopped)
	v.recoverOnClose = false
	if business != v.snapshot(t, false) {
		t.Fatal("technical ownership recovery changed Scheduler business facts")
	}
	v.other(t)
	v.noExecution(t, 0)
	t.Log("PROJECT_SCHEDULER_STOP_TECHNICAL_UNKNOWN original_register_and_close_commit=true local_ack_masks=true exact_attempt_cause=true same_tx_zero_writes=true confirmation_original_return=true next_explicit_close=true other_project_live=true no_network_lost_ack=true")
}

func testProjectSchedulerStopOutcome(t *testing.T) {
	v := newSchedulerOwnedStopPG(t, false)
	p, dispatch := v.work.base.project.ID, v.dispatch.Summary().ID
	request, err := v.dispatch.LaunchRequest()
	schedulerOwnedRequire(t, "original-launch-request", err)
	command, err := f.NewCommandIdentity("scheduler", []string{p.String()}, "reject_launch", request.Meta.IdempotencyKey)
	schedulerOwnedRequire(t, "original-rejection-identity", err)
	b := v.barrier()
	s := v.base.tracked
	var selected atomic.Bool
	var masked atomic.Bool
	var original f.CommitResult
	s.mu.Lock()
	s.after = func(ctx context.Context, tx f.Tx, cause f.TransactionCause) error {
		if cause.Kind() != f.CommandsCause || cause.Details().Primary.Canonical() != command.Canonical() {
			return nil
		}
		x, err := s.InTx(tx)
		if err != nil {
			return err
		}
		var exact bool
		if err = x.QueryRow(ctx, `SELECT launch_outcome='known_not_created' AND execution_id IS NULL AND attempt_count=1 AND final_attempt=1 AND failure_reason=$3 FROM agenteam_scheduler.dispatches WHERE project_id=$1::text AND id=$2::text`, p.String(), dispatch.String(), string(wc.TaskLaunchFailureUnsupportedResourceConstraints)).Scan(&exact); err != nil {
			return err
		}
		if !exact {
			return errors.New("original typed rejection did not persist its exact receipt")
		}
		selected.Store(true)
		return nil
	}
	s.afterResult = func(c f.TransactionCause, r f.CommitResult) {
		if selected.Load() && c.Kind() == f.CommandsCause && c.Details().Primary.Canonical() == command.Canonical() && r.State() == f.Committed {
			b.hold(r)
		}
	}
	s.transformResult = func(c f.TransactionCause, r f.CommitResult) f.CommitResult {
		if selected.Load() && c.Kind() == f.CommandsCause && c.Details().Primary.Canonical() == command.Canonical() && r.State() == f.Committed && masked.CompareAndSwap(false, true) {
			original = f.UnknownResult(id[f.TransactionAttempt](t), c)
			return original
		}
		return r
	}
	s.mu.Unlock()
	call := v.start(func(ctx context.Context) error { _, err := v.pair.Handoff().LaunchOnce(ctx, p, dispatch); return err })
	v.recoverOnClose = true
	v.await(t, call, b)
	v.accept(t)
	v.observed(t, v.pair, false, pc.StopPending)
	v.other(t)
	v.noExecution(t, 1)
	b.open()
	outboxCleanupWait(t, call.done, "original rejected Launch acknowledgement")
	v.clear()
	actual, ok := scheduler.UnknownAttempt(call.err)
	if !selected.Load() || !masked.Load() || !ok || actual.AttemptID() != original.AttemptID() || !schedulerOwnedSameCause(actual.Cause(), original.Cause()) {
		agentRetirementDiagnostic(t, "scheduler-rejection-unknown", call.err)
		t.Fatal("original Handoff Unknown lost its actual attempt/cause")
	}
	stable := v.snapshot(t, false)
	v.confirm(t, original)
	v.noExecution(t, 1)
	v.observed(t, v.pair, false, pc.StopPending) // Only the original technical close.
	v.observed(t, v.pair, false, pc.Stopped)
	v.recoverOnClose = false
	if stable != v.snapshot(t, false) {
		t.Fatal("Stop changed the original Dispatch, Work receipt or failure history")
	}
	v.noExecution(t, 1)
	v.other(t)
	t.Log("PROJECT_SCHEDULER_STOP_OUTCOME real_typed_known_not_created=true original_rejection_commit=true local_ack_unknown=true original_attempt_cause=true positive_receipt_confirmation=true same_tx_zero_writes=true confirmation_original_return=true no_lookup_association_compensation_or_relaunch=true execution_rows_zero=true no_network_lost_ack=true")
}
