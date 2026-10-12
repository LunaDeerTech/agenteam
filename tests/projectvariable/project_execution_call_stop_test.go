//go:build integration

package projectvariable_test

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/agent"
	"github.com/LunaDeerTech/agenteam/internal/central/agentloop"
	"github.com/LunaDeerTech/agenteam/internal/central/audit"
	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/execution"
	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/model"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/outbox"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/LunaDeerTech/agenteam/internal/central/project"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	pv "github.com/LunaDeerTech/agenteam/internal/central/projectvariable"
	"github.com/LunaDeerTech/agenteam/internal/central/scheduler"
	"github.com/LunaDeerTech/agenteam/internal/central/secret"
	"github.com/LunaDeerTech/agenteam/internal/central/skill"
	"github.com/LunaDeerTech/agenteam/internal/central/tool/builtin"
	"github.com/LunaDeerTech/agenteam/internal/central/tool/registry"
	toolruntime "github.com/LunaDeerTech/agenteam/internal/central/tool/runtime"
	"github.com/LunaDeerTech/agenteam/internal/central/work"
	wc "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
	assembly "github.com/LunaDeerTech/agenteam/tests/testsupport/agentconfiguration"
)

// This explicitly owned factory exercises real Service and Associated calls,
// including Preparation's known missing-capture failure and original return.
// Direct's private-parent rejection is not a successful lane or Model call.
// No default Registry, Invoke, foreign death or Execution cleanup is claimed.
func TestProjectExecutionCallStop(t *testing.T) {
	t.Run("service-return-upgrade-and-instance-isolation", testExecutionCallStopReturn)
	t.Run("technical-unknown-readonly-confirmation", testExecutionCallStopUnknown)
	t.Run("associated-preparation-original-failure-return", testExecutionCallStopAssociated)
}

type executionCallPG struct {
	*schedulerOwnedStopPG
	deps              execution.ProjectCallDependencies
	owned             *execution.ProjectCallOwnership
	ownedPairs        []*execution.ProjectCallOwnership
	stopDriver        *project.LifecycleStopDriver
	stopStep          func(context.Context, i.Actor, pc.LifecycleCause, pc.ScopeRef) error
	stopReport        pc.StopReport
	request           ec.LaunchRequest
	created           ec.Summary
	association       ec.AssociatedDispatch
	models            *executionCallNoModel
	loop              *agentloop.DirectTextController
	lifetime          chan error
	lifetimeCancel    context.CancelFunc
	needsConfirmation bool
}

// Only a never-invoked guard: no response, permission or Model owner is made.
type executionCallNoModel struct{ calls atomic.Int64 }

func (m *executionCallNoModel) BeginChat(context.Context, mc.ModelRequest) (mc.JSONCall, error) {
	m.calls.Add(1)
	return nil, f.NewFault(f.DependencyUnbound, f.NotCommitted)
}

func newExecutionCallPG(t *testing.T, upgrade bool) *executionCallPG {
	t.Helper()
	v := &executionCallPG{schedulerOwnedStopPG: newSchedulerOwnedStopPG(t, upgrade), models: &executionCallNoModel{}}
	if upgrade {
		migrate(t, v.base.db, migrationPrefix(t, "00082"))
	}
	v.seedExecution(t)
	if upgrade {
		before := v.executionSnapshot(t, false)
		// The actual legacy Launch/Handoff returned above. No legacy call is
		// treated as a newly owned row or an independently proved Stop.
		for n := 0; n < 2; n++ {
			migrate(t, v.base.db, migrationPrefix(t, "00083"))
			var count int
			schedulerOwnedRequire(t, "execution-migration-no-invented-owners", v.base.raw.QueryRow(ctxFor(t), `SELECT count(*) FROM agenteam_execution.project_calls`).Scan(&count))
			if count != 0 || before != v.executionSnapshot(t, false) {
				t.Fatal("migration83/repeat changed original facts or invented returned ownership")
			}
		}
	}
	entries := v.skills.p2.manifest.Entries()
	entries = append(entries, pc.ParticipantRegistration{Name: pc.ExecutionParticipant, ContractVersion: 1, OwnerModule: "execution", ReferenceKinds: []pc.ReferenceKind{"execution-call"}})
	for n := range entries {
		if entries[n].Name == pc.OutboxParticipant || entries[n].Name == pc.AuditParticipant {
			entries[n].CleanupAfter = append(entries[n].CleanupAfter, pc.ExecutionParticipant)
		}
	}
	manifest, err := pc.NewRequiredManifest(entries)
	schedulerOwnedRequire(t, "execution-manifest", err)
	v.life, err = project.NewLifecycleAuthority(v.base.tracked, manifest, nil)
	schedulerOwnedRequire(t, "execution-lifecycle", err)
	source, err := toolruntime.NewInstallSource(v.skills.installer, v.skills.p2.service)
	schedulerOwnedRequire(t, "execution-original-install-source", err)
	graph, err := assembly.New(v.base.tracked, assembly.Options{Accounts: v.base.accounts, Lifecycle: v.life, BuiltinSources: map[string]registry.BuiltinSource{builtin.SkillInstallStableKey: source}, InstallSourceKey: builtin.SkillInstallStableKey})
	schedulerOwnedRequire(t, "execution-real-authorities", err)
	providers := graph.Providers()
	access, err := project.NewSchedulerExecutionAccess(providers.Projects, v.work.pending)
	schedulerOwnedRequire(t, "execution-real-access", err)
	authority, err := execution.NewAuthority(v.base.tracked, providers.Projects, access)
	schedulerOwnedRequire(t, "execution-real-authority", err)
	configuration, err := agent.NewExecutionConfiguration(providers.Agents, authority)
	schedulerOwnedRequire(t, "execution-real-configuration", err)
	launch, err := work.NewTaskLaunchProvider(v.base.tracked, v.work.authority, v.work.pending)
	schedulerOwnedRequire(t, "execution-real-launch-provider", err)
	catalog := event.NewCatalog()
	projectEvents, err := pc.RegisterProjectEvents(catalog)
	schedulerOwnedRequire(t, "execution-project-events", err)
	directEvents, err := ec.RegisterDirectTextEvents(catalog)
	schedulerOwnedRequire(t, "execution-direct-events", err)
	directAuthority, err := execution.NewDirectTextEventAuthority(authority)
	schedulerOwnedRequire(t, "execution-direct-event-authority", err)
	auditing, err := audit.New(v.base.tracked, v.base.keys, audit.Authorizations{Sessions: v.base.accounts, System: v.base.accounts, Accounts: v.base.accounts, Projects: providers.AuditProjects})
	schedulerOwnedRequire(t, "execution-real-audit", err)
	processes := captureProviderProcesses{guard: v.skills.guard}
	box, err := outbox.New(v.base.tracked, catalog, outbox.Authorizations{Producers: map[event.StableName]oc.ProducerAuthority{pc.ProjectProducer: providers.Projects, ec.ExecutionProducer: directAuthority}, Sessions: v.base.accounts, System: v.base.accounts, Projects: providers.Projects, Audit: auditing, Cursors: v.base.keys, Processes: processes})
	schedulerOwnedRequire(t, "execution-real-events", err)
	var bindings []project.LifecycleParticipantBinding
	for _, entry := range manifest.Entries() {
		port := &unavailableParticipant{name: entry.Name}
		bindings = append(bindings, project.LifecycleParticipantBinding{Registration: entry, Participant: port})
		t.Cleanup(func() {
			if port.calls.Load() != 0 {
				t.Error("execution acceptance-only Registry invoked a missing port")
			}
		})
	}
	registration, err := project.NewLifecycleRegistry(manifest, bindings)
	schedulerOwnedRequire(t, "execution-acceptance-registry", err)
	// The inherited service's real setup calls have returned. Its cleanup
	// closure observes this field, so retire it before replacing the fixture
	// projection with the new same-Life acceptance service.
	schedulerOwnedDrain(t, v.projects)
	v.projects, err = project.New(v.base.tracked, project.Dependencies{Authority: providers.Projects, Activity: v.base.accounts, Audit: auditing, Events: box, ProjectEvents: projectEvents, Initializer: v.skills.p2.service, Processes: processes, Cursors: v.base.keys, LifecycleRegistry: registration}, project.DefaultConfig())
	schedulerOwnedRequire(t, "execution-original-project-service", err)
	t.Cleanup(func() { schedulerOwnedDrain(t, v.projects) })
	contextBuilder, err := execution.NewContextBuilder(work.TaskContextBuilder{})
	schedulerOwnedRequire(t, "execution-context-builder", err)
	v.loop, err = agentloop.NewDirectTextController(v.models)
	schedulerOwnedRequire(t, "execution-zero-call-loop", err)
	initial, err := skill.NewInitialRoundBindings(providers.SkillAuthority)
	schedulerOwnedRequire(t, "execution-real-round-bindings", err)
	modelRole, err := i.RegisterService(i.ModelRuntime)
	schedulerOwnedRequire(t, "execution-model-role", err)
	secretRole, err := i.RegisterService(i.SecretService)
	schedulerOwnedRequire(t, "execution-secret-role", err)
	outboundRole, err := i.RegisterService(i.OutboundService)
	schedulerOwnedRequire(t, "execution-outbound-role", err)
	runtimeAuthority, err := model.NewRuntimeAuthority(v.base.tracked, model.RuntimeAuthorizations{Consumers: authority, Process: v.skills.guard, ModelRuntime: modelRole, SecretService: secretRole, OutboundService: outboundRole})
	schedulerOwnedRequire(t, "execution-real-runtime-authority", err)
	variables, err := variableOwnershipAssemble(v.base.tracked, v.base.accounts, v.base.keys, manifest, v.skills.process, providers.Projects)
	schedulerOwnedRequire(t, "execution-real-variable-dependencies", err)
	t.Cleanup(variables.d04.StopMaintenance)
	modelRetirement, err := model.NewExecutionLeaseRetirement(runtimeAuthority, variables.d04)
	schedulerOwnedRequire(t, "execution-real-model-retirement", err)
	environmentAuthority, err := pv.NewEnvironmentRetirementAuthority(v.base.authority, authority)
	schedulerOwnedRequire(t, "execution-real-environment-authority", err)
	leaseRetirement, err := secret.NewProjectVariableLeaseRetirement(variables.d04, environmentAuthority)
	schedulerOwnedRequire(t, "execution-real-environment-leases", err)
	environment, err := pv.NewEnvironmentRetirement(environmentAuthority, leaseRetirement)
	schedulerOwnedRequire(t, "execution-real-environment-retirement", err)
	v.deps = execution.ProjectCallDependencies{
		Service: execution.Dependencies{Authority: authority, Agents: configuration, Task: launch},
		// No capture provider is installed. A real Preparation claim can fail
		// with DependencyUnbound and must still return its terminal checkpoint.
		Preparation: execution.PreparationDependencies{Agents: configuration, Projects: providers.Projects, Processes: processes},
		Direct:      execution.DirectTextDependencies{Context: contextBuilder, Loop: v.loop, Processes: processes, Projects: providers.Projects, Agents: configuration, Skills: initial, Events: box, Lifecycle: directEvents, Models: modelRetirement, Environment: environment},
		Associated:  execution.AssociatedExecutorOptions{MaxOwned: 2, RecoveryInterval: 20 * time.Millisecond},
	}
	v.owned = v.newExecutionPair(t)
	v.stopDriver, err = project.NewLifecycleStopDriver(v.base.tracked, v.life, processes, func(ctx context.Context, actor i.Actor, cause pc.LifecycleCause, scope pc.ScopeRef) error {
		if v.stopStep == nil || cause != v.cause || !scope.Equal(v.scope) {
			return errors.New("Execution original Stop scope changed")
		}
		return v.stopStep(ctx, actor, cause, scope)
	})
	schedulerOwnedRequire(t, "execution-original-stop-driver", err)
	t.Cleanup(func() { v.closeExecution(t) })
	return v
}

func (v *executionCallPG) seedExecution(t *testing.T) {
	t.Helper()
	p, w := v.work.base.project.ID, v.work
	rejectedRequest, err := v.dispatch.LaunchRequest()
	schedulerOwnedRequire(t, "execution-seed-rejected-request", err)
	rejected, err := v.pair.Handoff().LaunchOnce(ctxFor(t), p, v.dispatch.Summary().ID)
	reason, exact := wc.MatchTaskLaunchFailure(err, rejectedRequest)
	if !exact || reason != wc.TaskLaunchFailureUnsupportedResourceConstraints || rejected.Summary().LaunchOutcome != scheduler.KnownNotCreated {
		t.Fatal("formal unsupported seed rejection absent", err)
	}
	_, err = v.pair.Failure().FinalizeLaunchFailure(ctxFor(t), p, v.dispatch.Summary().ID)
	schedulerOwnedRequire(t, "execution-seed-formal-failure", err)
	created, err := w.tasks.CreateTask(ctxFor(t), v.base.ownerBrowser.actor, meta(t, "execution-owner-task", nil), p, wc.TaskCreate{TaskID: id[wc.Task](t), SprintID: w.task.SprintID, Title: "Created Execution owner", Type: wc.TaskTypeTask, Priority: wc.TaskPriorityMedium, Plan: "No model invocation"})
	schedulerOwnedRequire(t, "execution-seed-task", err)
	moved, err := w.transitions.TransferTask(ctxFor(t), v.base.ownerBrowser.actor, meta(t, "execution-owner-transfer", &created.Task.Version), p, created.Task.ID, wc.TaskTransfer{TargetState: wc.TaskStateTodo, AssigneeAgentID: &w.agentID, AddBlockers: []wc.TaskBlockerCreate{}, ResolveBlockerIDs: []wc.TaskBlockerID{}})
	schedulerOwnedRequire(t, "execution-seed-transfer", err)
	w.task = moved.Task
	claim, policy := schedulerClaimRequest(t, w)
	dispatch, err := v.pair.Coordinator().ClaimTask(ctxFor(t), claim, policy)
	schedulerOwnedRequire(t, "execution-seed-claim", err)
	v.request, err = dispatch.LaunchRequest()
	schedulerOwnedRequire(t, "execution-seed-original-request", err)
	launched, err := v.pair.Handoff().LaunchOnce(ctxFor(t), p, dispatch.Summary().ID)
	schedulerOwnedRequire(t, "execution-seed-real-launch", err)
	launches, lookups, actual := v.launcher.observed()
	if launches != 2 || lookups != 0 || actual.Status != ec.Created || launched.Summary().ExecutionID == nil || *launched.Summary().ExecutionID != actual.ID {
		t.Fatal("real Created Execution association missing")
	}
	v.created = actual
	digest, err := v.request.Digest()
	schedulerOwnedRequire(t, "execution-seed-digest", err)
	v.association = ec.AssociatedDispatch{ExecutionID: actual.ID, AgentID: w.agentID, Key: v.request.Meta.IdempotencyKey, Digest: digest, DispatchID: launched.Summary().ID.String()}
}

func (v *executionCallPG) newExecutionPair(t *testing.T) *execution.ProjectCallOwnership {
	t.Helper()
	authority, err := project.NewExecutionProjectStopAuthority(v.life, v.base.tracked)
	schedulerOwnedRequire(t, "execution-project-stop-authority", err)
	pair, err := execution.NewProjectCallOwnership(v.base.tracked, v.deps, v.skills.guard, v.skills.process, authority)
	schedulerOwnedRequire(t, "execution-owned-factory", err)
	v.ownedPairs = append(v.ownedPairs, pair)
	return pair
}

func (v *executionCallPG) lookup(ctx context.Context, pair *execution.ProjectCallOwnership, p i.ProjectID, key f.IdempotencyKey) (ec.LaunchLookup, error) {
	return pair.Service().LookupLaunch(ctx, v.base.ownerBrowser.actor, ec.LaunchLookupKey{ProjectID: p, AgentID: v.work.agentID, IdempotencyKey: key}, v.association.Digest)
}

func (v *executionCallPG) otherExecution(t *testing.T) {
	t.Helper()
	// Human owner Lookup authorizes this real Project before observing no
	// launch. No Agent membership or nonexistent Execution is manufactured.
	got, err := v.lookup(ctxFor(t), v.owned, v.base.project.ID, "execution-other-project")
	schedulerOwnedRequire(t, "execution-other-project-original-read", err)
	if got.Found {
		t.Fatal("B lookup invented an Execution")
	}
	var count int
	schedulerOwnedRequire(t, "execution-other-project-returned-ledger", v.base.raw.QueryRow(ctxFor(t), `SELECT count(*) FROM agenteam_execution.project_calls WHERE project_id=$1::uuid AND lane='service' AND phase='business_closed'`, v.base.project.ID.String()).Scan(&count))
	if count == 0 {
		t.Fatal("B original Execution read was not registered and returned")
	}
}

func (v *executionCallPG) stopExecution(t *testing.T, pair *execution.ProjectCallOwnership, inspect bool) *skillsCleanupPGCall {
	t.Helper()
	v.stopStep = func(ctx context.Context, actor i.Actor, cause pc.LifecycleCause, scope pc.ScopeRef) error {
		var report pc.StopReport
		var err error
		if inspect {
			report, err = pair.InspectProjectStop(ctx, actor, cause, scope)
		} else {
			report, err = pair.RequestProjectStop(ctx, actor, cause, scope)
		}
		if err != nil {
			return err
		}
		if !report.Matches(pc.ExecutionParticipant, cause, scope) {
			return errors.New("Execution Stop report lost original identity")
		}
		v.stopReport = report
		return nil
	}
	return v.start(func(ctx context.Context) error {
		return v.stopDriver.Run(ctx, v.work.base.project.ID, v.cause.OperationID)
	})
}

func (v *executionCallPG) executionObserved(t *testing.T, pair *execution.ProjectCallOwnership, inspect bool, want pc.StopState) {
	t.Helper()
	call := v.stopExecution(t, pair, inspect)
	outboxCleanupWait(t, call.done, "original Execution Project Stop")
	schedulerOwnedRequire(t, "execution-project-stop-return", call.err)
	if v.stopReport.Details().State != want {
		t.Fatal("Execution Project Stop differs", v.stopReport.Details().State, want)
	}
}

func (v *executionCallPG) executionSnapshot(t *testing.T, ledger bool) string {
	t.Helper()
	parts := []string{}
	// These business tables have TEXT IDs; migration83 ownership uses UUID.
	for _, table := range []string{"executions", "preparation_attempts", "preparation_claims", "preparation_inputs", "snapshots", "rounds"} {
		if table == "executions" {
			parts = append(parts, `(SELECT jsonb_agg(to_jsonb(x) ORDER BY to_jsonb(x)::text) FROM agenteam_execution.executions x WHERE project_id=$1::text)`)
		} else {
			parts = append(parts, fmt.Sprintf(`(SELECT jsonb_agg(to_jsonb(x) ORDER BY to_jsonb(x)::text) FROM agenteam_execution.%s x WHERE execution_id IN (SELECT id FROM agenteam_execution.executions WHERE project_id=$1::text))`, table))
		}
	}
	if ledger {
		parts = append(parts, `(SELECT jsonb_agg(to_jsonb(x) ORDER BY to_jsonb(x)::text) FROM agenteam_execution.project_calls x WHERE project_id=$1::text::uuid)`)
	}
	var snapshot string
	schedulerOwnedRequire(t, "execution-business-snapshot", v.base.raw.QueryRow(ctxFor(t), "SELECT jsonb_build_array("+strings.Join(parts, ",")+")::text", v.work.base.project.ID.String()).Scan(&snapshot))
	return v.snapshot(t, true) + snapshot
}

func (v *executionCallPG) noInvoke(t *testing.T) {
	t.Helper()
	var counts [3]int
	schedulerOwnedRequire(t, "execution-no-invoke-facts", v.base.raw.QueryRow(ctxFor(t), `SELECT (SELECT count(*) FROM agenteam_execution.rounds WHERE execution_id=$1::text),(SELECT count(*) FROM agenteam_model.invocations WHERE execution_id=$1::text::uuid),(SELECT count(*) FROM agenteam_execution.project_calls WHERE project_id=$2::uuid AND lane='direct')`, v.created.ID.String(), v.work.base.project.ID.String()).Scan(&counts[0], &counts[1], &counts[2]))
	if counts != [3]int{} || v.models.calls.Load() != 0 {
		t.Fatal("this PG entered Direct or Model", counts, v.models.calls.Load())
	}
}

func (v *executionCallPG) closeExecution(t *testing.T) {
	for _, barrier := range v.barriers {
		barrier.open()
	}
	for _, call := range v.calls {
		outboxCleanupWait(t, call.done, "original Execution fixture call")
	}
	v.clear()
	if v.needsConfirmation {
		if v.cause.Validate() != nil {
			v.accept(t)
		}
		// Only this fixture's exact technical Unknown; bounded explicit owner
		// steps, not a new call or a global Stop used to erase uncertainty.
		for n := 0; n < 5; n++ {
			call := v.stopExecution(t, v.owned, false)
			outboxCleanupWait(t, call.done, "original Execution technical cleanup confirmation")
			if call.err != nil {
				t.Error("original technical confirmation failed", call.err)
				break
			}
			if v.stopReport.Details().State == pc.Stopped {
				v.needsConfirmation = false
				break
			}
		}
	}
	for _, pair := range v.ownedPairs {
		pair.Stop()
		schedulerOwnedDrain(t, pair)
		if !pair.Joined() {
			t.Error("original Execution pair did not join")
		}
	}
	if v.lifetime != nil {
		select {
		case err := <-v.lifetime:
			if err != nil && !errors.Is(err, context.Canceled) {
				t.Error("Associated original Run failed", err)
			}
		case <-time.After(5 * time.Second):
			t.Error("Associated original Run did not return")
			<-v.lifetime
		}
	}
	if v.lifetimeCancel != nil {
		v.lifetimeCancel()
	}
	v.stopDriver.Stop()
	schedulerOwnedDrain(t, v.stopDriver)
	v.loop.Stop()
	schedulerOwnedDrain(t, v.loop)
	if v.models.calls.Load() != 0 {
		t.Error("unexpected Model call in non-Invoke fixture")
	}
}

func (v *executionCallPG) immutableLedger(t *testing.T) {
	t.Helper()
	before := v.executionSnapshot(t, true)
	lock, _ := f.ProjectLock(v.work.base.project.ID.String())
	for _, statement := range []string{
		`UPDATE agenteam_execution.project_calls SET lane='direct' WHERE project_id=$1::uuid AND phase='running'`,
		`DELETE FROM agenteam_execution.project_calls WHERE project_id=$1::uuid`,
	} {
		cause, _ := f.NewRecoveryCause("execution-pg-immutable", id[struct{}](t).String(), v.work.base.project.ID.String())
		result := v.base.tracked.WithinTx(ctxFor(t), cause, func(ctx context.Context, tx f.Tx) error {
			if err := v.base.tracked.AcquireAll(ctx, tx, []f.LockRequest{{Key: lock, Mode: f.Exclusive}}); err != nil {
				return err
			}
			x, err := v.base.tracked.InTx(tx)
			if err != nil {
				return err
			}
			_, err = x.Exec(ctx, statement, v.work.base.project.ID.String())
			return err
		})
		var problem *postgres.Error
		if result.State() != f.NotCommitted || !errors.As(result.Fault(), &problem) || problem.SQLState() != "23514" || before != v.executionSnapshot(t, true) {
			t.Fatal("ordinary Execution ownership mutation was not rejected and rolled back")
		}
	}
}

func testExecutionCallStopReturn(t *testing.T) {
	v := newExecutionCallPG(t, true)
	replay, err := v.owned.Service().Launch(ctxFor(t), v.base.ownerBrowser.actor, v.request)
	schedulerOwnedRequire(t, "execution-owned-launch-replay", err)
	if !replay.Replayed || replay.Execution.ID != v.created.ID {
		t.Fatal("owned Service lost the original Created receipt")
	}
	v.otherExecution(t)
	barrier := v.barrier()
	command, err := (ec.LaunchLookupKey{ProjectID: v.work.base.project.ID, AgentID: v.work.agentID, IdempotencyKey: v.request.Meta.IdempotencyKey}).Command()
	schedulerOwnedRequire(t, "execution-lookup-identity", err)
	var mu sync.Mutex
	var originalContext context.Context
	exact := func(c f.TransactionCause) bool {
		return c.Kind() == f.CommandsCause && c.Details().Primary.Canonical() == command.Canonical()
	}
	s := v.base.tracked
	s.mu.Lock()
	s.enter = func(ctx context.Context, c f.TransactionCause) {
		if exact(c) {
			mu.Lock()
			originalContext = ctx
			mu.Unlock()
		}
	}
	s.afterResult = func(c f.TransactionCause, r f.CommitResult) {
		if exact(c) && r.State() == f.Committed {
			barrier.hold(r)
		}
	}
	s.mu.Unlock()
	var lookup ec.LaunchLookup
	call := v.start(func(ctx context.Context) error {
		var err error
		lookup, err = v.lookup(ctx, v.owned, v.work.base.project.ID, v.request.Meta.IdempotencyKey)
		return err
	})
	v.await(t, call, barrier)
	mu.Lock()
	live := originalContext != nil && originalContext.Err() == nil
	mu.Unlock()
	if !live {
		t.Fatal("original owned Service entry context was not live")
	}
	v.immutableLedger(t)
	v.accept(t)
	v.executionObserved(t, v.owned, false, pc.StopPending)
	mu.Lock()
	canceled := originalContext != nil && errors.Is(originalContext.Err(), context.Canceled)
	mu.Unlock()
	select {
	case <-call.done:
		t.Fatal("original Service returned through held Store")
	default:
	}
	if !canceled {
		t.Fatal("Project Stop did not cancel its original Service context")
	}
	v.otherExecution(t)
	barrier.open()
	outboxCleanupWait(t, call.done, "original Execution Lookup return")
	schedulerOwnedRequire(t, "execution-lookup-original-return", call.err)
	if !lookup.Found || lookup.Execution == nil || lookup.Execution.ID != v.created.ID {
		t.Fatal("real committed Lookup receipt was lost")
	}
	v.clear()
	v.executionObserved(t, v.owned, false, pc.Stopped)
	stable := v.executionSnapshot(t, true)
	v.executionObserved(t, v.newExecutionPair(t), false, pc.StopPending)
	if stable != v.executionSnapshot(t, true) {
		t.Fatal("different instance changed original ownership or business facts")
	}
	v.otherExecution(t)
	v.noInvoke(t)
	t.Log("PROJECT_EXECUTION_CALL_STOP_RETURN migration82_83_repeat=true legacy_original_return=true owned_service_launch_replay=true original_store_return=true pending_until_actual_return=true different_instance_pending=true other_project_live=true immutable_identity=true no_direct_or_model_invoke=true no_default_registry=true")
}

type executionCallPGTag struct{}

func (v *executionCallPG) maskExecutionTechnical(t *testing.T, owner string) schedulerOwnedUnknown {
	t.Helper()
	v.clear()
	var mu sync.Mutex
	selected := ""
	var original f.CommitResult
	var once atomic.Bool
	s := v.base.tracked
	s.mu.Lock()
	s.enter = func(ctx context.Context, c f.TransactionCause) {
		d := c.Details()
		if ctx.Value(executionCallPGTag{}) == owner && c.Kind() == f.RecoveryCause && d.Owner == owner && d.CheckpointRef == v.work.base.project.ID.String() {
			mu.Lock()
			selected = d.RecoveryRunID
			mu.Unlock()
		}
	}
	s.transformResult = func(c f.TransactionCause, r f.CommitResult) f.CommitResult {
		mu.Lock()
		exact := selected != "" && c.Kind() == f.RecoveryCause && c.Details().Owner == owner && c.Details().RecoveryRunID == selected
		mu.Unlock()
		if exact && r.State() == f.Committed && once.CompareAndSwap(false, true) {
			original = f.UnknownResult(id[f.TransactionAttempt](t), c)
			return original
		}
		return r
	}
	s.mu.Unlock()
	call := v.start(func(ctx context.Context) error {
		_, err := v.lookup(context.WithValue(ctx, executionCallPGTag{}, owner), v.owned, v.work.base.project.ID, f.IdempotencyKey(owner))
		return err
	})
	outboxCleanupWait(t, call.done, "original Execution technical acknowledgement")
	v.clear()
	v.needsConfirmation = once.Load() || v.needsConfirmation
	r, ok := execution.UnknownAttempt(call.err)
	if !once.Load() || !ok || r.AttemptID() != original.AttemptID() || !schedulerOwnedSameCause(r.Cause(), original.Cause()) {
		agentRetirementDiagnostic(t, "execution-technical-unknown", call.err)
		t.Fatal("original Execution technical Unknown lost its cause or attempt")
	}
	return schedulerOwnedUnknown{original: original, id: selected}
}

func (v *executionCallPG) confirmExecution(t *testing.T, original f.CommitResult) {
	t.Helper()
	v.clear()
	before := v.executionSnapshot(t, true)
	s := v.base.tracked
	barrier := v.barrier()
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
			return errors.New("Execution confirmation lost its original Tx baseline")
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
			return errors.New("original Execution confirmation wrote domain facts")
		}
		seen.Add(1)
		return nil
	}
	s.afterResult = func(c f.TransactionCause, r f.CommitResult) {
		if exact(c) && seen.Load() == 1 && r.State() == f.Committed {
			barrier.hold(r)
		}
	}
	s.mu.Unlock()
	call := v.stopExecution(t, v.owned, false)
	v.await(t, call, barrier)
	select {
	case <-call.done:
		t.Fatal("Execution confirmation returned before original Store return")
	default:
	}
	if seen.Load() != 1 || before != v.executionSnapshot(t, true) {
		t.Fatal("Execution readonly confirmation changed original metadata")
	}
	v.otherExecution(t)
	barrier.open()
	outboxCleanupWait(t, call.done, "original Execution confirmation return")
	schedulerOwnedRequire(t, "execution-original-readonly-confirmation", call.err)
	if seen.Load() != 1 || before != v.executionSnapshot(t, true) || v.stopReport.Details().State != pc.StopPending {
		t.Fatal("Execution confirmation skipped readonly/returned Pending boundary")
	}
	v.clear()
}

func testExecutionCallStopUnknown(t *testing.T) {
	v := newExecutionCallPG(t, false)
	registration := v.maskExecutionTechnical(t, "execution-call-register")
	returned := v.maskExecutionTechnical(t, "execution-call-return")
	v.accept(t)
	// Owner selection is by actual UUID ordering, not creation time: IDs made
	// within the same millisecond have a random suffix.
	unknowns := []schedulerOwnedUnknown{registration, returned}
	sort.Slice(unknowns, func(a, b int) bool { return unknowns[a].id < unknowns[b].id })
	for _, unknown := range unknowns {
		v.confirmExecution(t, unknown.original)
		if unknown.original.Cause().Details().Owner == "execution-call-register" {
			v.executionObserved(t, v.owned, false, pc.StopPending)
			var closed bool
			schedulerOwnedRequire(t, "execution-explicit-post-confirmation-close", v.base.raw.QueryRow(ctxFor(t), `SELECT phase='business_closed' FROM agenteam_execution.project_calls WHERE id=$1::uuid`, unknown.id).Scan(&closed))
			if !closed {
				t.Fatal("explicit call did not close the original registration")
			}
		}
	}
	v.executionObserved(t, v.owned, false, pc.Stopped)
	v.needsConfirmation = false
	v.otherExecution(t)
	v.noInvoke(t)
	t.Log("PROJECT_EXECUTION_CALL_STOP_UNKNOWN real_register_and_return_commits=true local_ack_unknown=true original_attempt_and_cause=true same_tx_zero_writes=true original_confirmation_return=true explicit_following_close=true other_project_live=true no_network_lost_ack=true no_default_registry=true")
}

func testExecutionCallStopAssociated(t *testing.T) {
	v := newExecutionCallPG(t, false)
	before := v.executionSnapshot(t, true)
	var sqlCalls atomic.Int64
	s := v.base.tracked
	s.mu.Lock()
	s.enter = func(context.Context, f.TransactionCause) { sqlCalls.Add(1) }
	s.mu.Unlock()
	prepareErr := v.owned.Preparation().Run(ctxFor(t), v.created.ID)
	_, directErr := v.owned.Direct().Start(ctxFor(t), v.created.ID)
	v.clear()
	if !schedulerOwnedCode(prepareErr, f.DependencyUnbound) || !schedulerOwnedCode(directErr, f.DependencyUnbound) || sqlCalls.Load() != 0 || before != v.executionSnapshot(t, true) {
		t.Fatal("naked E-only driver gained a private parent or SQL admission", prepareErr, directErr)
	}
	ctx, cancel := context.WithCancel(context.Background())
	v.lifetimeCancel = cancel
	v.lifetime = make(chan error, 1)
	go func() { v.lifetime <- v.owned.Associated().Run(ctx) }()
	select {
	case <-v.owned.Associated().Ready():
	case err := <-v.lifetime:
		v.lifetime = nil
		t.Fatal("Associated lifetime ended before ready", err)
	case <-time.After(5 * time.Second):
		t.Fatal("Associated lifetime did not become ready")
	}
	barrier := v.barrier()
	var terminal atomic.Bool
	var selected f.TransactionCause
	var mu sync.Mutex
	s.mu.Lock()
	s.after = func(ctx context.Context, tx f.Tx, c f.TransactionCause) error {
		d := c.Details()
		if c.Kind() != f.JobCause || d.JobType != "execution-preparation" || d.JobID != v.created.ID.String() {
			return nil
		}
		x, err := s.InTx(tx)
		if err != nil {
			return err
		}
		var exact bool
		if err = x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_execution.preparation_attempts WHERE execution_id=$1::text AND attempt_id=$2::text AND phase='terminal')`, v.created.ID.String(), d.JobAttemptID).Scan(&exact); err != nil {
			return err
		}
		if exact {
			mu.Lock()
			selected = c
			mu.Unlock()
			terminal.Store(true)
		}
		return nil
	}
	s.afterResult = func(c f.TransactionCause, r f.CommitResult) {
		mu.Lock()
		exact := terminal.Load() && c.Kind() == f.JobCause && c.Details().JobID == selected.Details().JobID && c.Details().JobAttemptID == selected.Details().JobAttemptID
		mu.Unlock()
		if exact && r.State() == f.Committed {
			barrier.hold(r)
		}
	}
	s.mu.Unlock()
	advance, err := v.owned.Associated().Advance(ctxFor(t), v.work.base.project.ID, v.association)
	schedulerOwnedRequire(t, "execution-real-associated-admission", err)
	if advance.ExecutionID != v.created.ID {
		t.Fatal("Associated admitted a different original Execution")
	}
	select {
	case result := <-barrier.entered:
		if result.State() != f.Committed {
			t.Fatal("original Preparation finish did not commit")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("original Preparation finish return was not reached")
	}
	var counts [4]int
	schedulerOwnedRequire(t, "execution-associated-real-preparation-facts", v.base.raw.QueryRow(ctxFor(t), `SELECT (SELECT count(*) FROM agenteam_execution.preparation_attempts WHERE execution_id=$1::text AND phase='terminal'),(SELECT count(*) FROM agenteam_execution.preparation_inputs WHERE execution_id=$1::text),(SELECT count(*) FROM agenteam_execution.project_calls WHERE project_id=$2::uuid AND lane='associated' AND phase='running'),(SELECT count(*) FROM agenteam_execution.project_calls WHERE project_id=$2::uuid AND lane='preparation' AND phase='running')`, v.created.ID.String(), v.work.base.project.ID.String()).Scan(&counts[0], &counts[1], &counts[2], &counts[3]))
	if counts != [4]int{1, 0, 1, 1} {
		t.Fatal("actual Associated/Preparation return owners differ", counts)
	}
	v.accept(t)
	v.executionObserved(t, v.owned, false, pc.StopPending)
	v.otherExecution(t)
	barrier.open()
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(5 * time.Millisecond)
	defer tick.Stop()
	for {
		call := v.stopExecution(t, v.owned, false)
		select {
		case <-call.done:
			schedulerOwnedRequire(t, "execution-original-worker-tail", call.err)
		case <-deadline.C:
			t.Fatal("original worker ownership tail did not return")
		}
		if v.stopReport.Details().State == pc.Stopped {
			break
		}
		if v.stopReport.Details().State != pc.StopPending {
			t.Fatal("original worker tail yielded an invalid Stop report")
		}
		select {
		case <-tick.C:
		case <-deadline.C:
			t.Fatal("original worker ownership tail did not return")
		}
	}
	v.clear()
	var closed int
	schedulerOwnedRequire(t, "execution-two-returned-worker-lanes", v.base.raw.QueryRow(ctxFor(t), `SELECT count(*) FROM agenteam_execution.project_calls WHERE project_id=$1::uuid AND lane IN ('associated','preparation') AND phase='business_closed'`, v.work.base.project.ID.String()).Scan(&closed))
	if closed != 2 {
		t.Fatal("actual associated and preparation call closures missing", closed)
	}
	v.noInvoke(t)
	v.otherExecution(t)
	t.Log("PROJECT_EXECUTION_CALL_STOP_ASSOCIATED actual_association=true preparation_registered=true known_missing_capture_not_success=true terminal_attempt_original_return=true parent_worker_tail=true naked_e_before_sql_rejected=true other_project_live=true direct_worker_not_reached=true model_calls_zero=true no_default_registry=true")
}
