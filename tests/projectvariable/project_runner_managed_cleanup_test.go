//go:build integration

package projectvariable_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/LunaDeerTech/agenteam/internal/central/agent"
	ac "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/audit"
	"github.com/LunaDeerTech/agenteam/internal/central/execution"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/model"
	"github.com/LunaDeerTech/agenteam/internal/central/mount"
	mtc "github.com/LunaDeerTech/agenteam/internal/central/mount/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/LunaDeerTech/agenteam/internal/central/project"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	pv "github.com/LunaDeerTech/agenteam/internal/central/projectvariable"
	rc "github.com/LunaDeerTech/agenteam/internal/central/runner/contract"
	runnerservice "github.com/LunaDeerTech/agenteam/internal/central/runner/service"
	"github.com/LunaDeerTech/agenteam/internal/central/skill"
	"github.com/LunaDeerTech/agenteam/internal/central/tool/registry"
	"github.com/LunaDeerTech/agenteam/internal/central/work"
	assembly "github.com/LunaDeerTech/agenteam/tests/testsupport/agentconfiguration"
	pgfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/postgres"
)

// This is an explicit real-owner assembly, not default App deletion. The
// original Agent pair creates nonempty Runner configuration; Scheduler and
// Execution actually stop and clean before Mount metadata retirement. Other
// missing Stop participants retain the fixture's controlled phase handoff.
// An offline Runner is only configured: no directory, connection or Invoke.
func TestProjectRunnerManagedCleanup(t *testing.T) {
	t.Run("nonempty-configuration-and-original-return", func(t *testing.T) { testRunnerManagedCleanup(t, false) })
	t.Run("original-unknown-readonly-confirmation", func(t *testing.T) { testRunnerManagedCleanup(t, true) })
}

type runnerManagedPGAssembly struct {
	agents        *agent.ProjectCallOwnership
	providers     assembly.Providers
	runner        rc.Snapshot
	root          string
	createdConfig ac.AgentConfig
}

func (*runnerManagedPGAssembly) manifest(entries []pc.ParticipantRegistration) []pc.ParticipantRegistration {
	entries = append(entries, mtc.ProjectLifecycleRegistration())
	for n := range entries {
		switch entries[n].Name {
		case pc.SkillsParticipant, pc.OutboxParticipant, pc.AuditParticipant:
			entries[n].CleanupAfter = append(entries[n].CleanupAfter, pc.RunnerManagedParticipant)
		}
	}
	return entries
}
func (a *runnerManagedPGAssembly) bindAgent(t *testing.T, v *skillsCleanupPG, graph *assembly.Assembly) (*agent.Service, error) {
	t.Helper()
	a.providers = graph.Providers()
	var err error
	a.agents, err = graph.NewProjectCallOwnership(v.p2.base.keys, captureProviderProcesses{guard: v.guard}, v.guard, v.process)
	if err != nil {
		return nil, err
	}
	t.Cleanup(func() { schedulerOwnedDrain(t, a.agents) })
	return a.agents.Service(), nil
}
func (a *runnerManagedPGAssembly) request(t *testing.T, v *skillsCleanupPG, original ac.AgentCreate) ac.AgentCreate {
	t.Helper()
	b := v.p2.base
	authority, err := runnerservice.NewAuthority(b.tracked, b.accounts)
	schedulerOwnedRequire(t, "runner-system-authority", err)
	auditing, err := audit.New(b.tracked, b.keys, audit.Authorizations{Sessions: b.accounts, System: b.accounts, Accounts: b.accounts, Runners: authority})
	schedulerOwnedRequire(t, "runner-system-audit", err)
	service, err := runnerservice.New(authority, auditing)
	schedulerOwnedRequire(t, "runner-system-service", err)
	t.Cleanup(func() { schedulerOwnedDrain(t, service) })
	a.root = "/runner-managed-project-fixture/" + id[struct{}](t).String()
	if _, err = os.Stat(a.root); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("configured Runner root must not exist", err)
	}
	intent, err := rc.NewCreate(rc.CreateRequest{RunnerID: id[rc.Runner](t), Name: "retirement-runner", Description: "Offline logical configuration only", Tags: []string{}, RootPath: a.root})
	schedulerOwnedRequire(t, "runner-formal-create-request", err)
	result, err := service.Execute(ctxFor(t), b.adminBrowser.actor, "runner-managed-system-create", intent)
	schedulerOwnedRequire(t, "runner-formal-create-return", err)
	if result.Receipt.Validate() != nil || result.Receipt.Runner.Status != rc.Offline || result.Material == nil || !result.Material.Token.Valid() {
		t.Fatal("formal Runner creation omitted its original offline receipt/material")
	}
	a.runner = result.Receipt.Runner
	declarations := []ac.RunnerMountDeclaration{{RunnerID: a.runner.ID, Workspace: "owned.workspace"}}
	fields := original.Fields()
	fields.AllowedMountIDs = nil
	fields.RunnerMounts = &declarations
	request, err := ac.NewAgentCreate(fields)
	schedulerOwnedRequire(t, "runner-formal-agent-request", err)
	return request
}
func (a *runnerManagedPGAssembly) created(t *testing.T, receipt ac.AgentMutation) {
	t.Helper()
	fields := receipt.Fields()
	if fields.RunnerMounts == nil || len(*fields.RunnerMounts) != 1 || len(fields.Agent.Fields().AllowedMountIDs) != 1 || (*fields.RunnerMounts)[0].RunnerID != a.runner.ID {
		t.Fatal("original Agent did not publish nonempty Runner configuration")
	}
	a.createdConfig = fields.Agent.Clone()
}

type runnerManagedPG struct {
	*executionRetirementPG
	assembly       *runnerManagedPGAssembly
	ownership      *mount.ProjectLifecycleOwnership
	participant    pc.ProjectLifecycleParticipant
	cleanup        *project.LifecycleCleanupDriver
	cleanupDrivers []*project.LifecycleCleanupDriver
	before         ac.AgentConfig
	originalMounts string
	protected      string
	cleanupStarted bool
	cleanupDone    bool
}

func newRunnerManagedPG(t *testing.T, upgrade bool) *runnerManagedPG {
	t.Helper()
	var base *variableHTTPFixture
	if upgrade {
		db := pgfixture.NewDatabase(t)
		migrate(t, db, migrationPrefix(t, "00087"))
		raw := openStore(t, db.Config(t, nil))
		base = assembleVariableHTTPFixture(t, db, raw, &hookStore{fixtureStore: raw})
	} else {
		base = newVariableHTTPFixture(t)
	}
	a := &runnerManagedPGAssembly{}
	v := &runnerManagedPG{executionRetirementPG: &executionRetirementPG{}, assembly: a}
	option := &executionRetirementPGAssembly{base: base, fixture: v.executionRetirementPG, runner: a}
	v.executionCallPG = newExecutionCallPG(t, false, option)
	t.Cleanup(func() { v.closeRunnerManaged(t) })
	v.assignQueued(t)
	var err error
	v.before, err = a.agents.Service().GetAgent(ctxFor(t), base.ownerBrowser.actor, v.work.base.project.ID, v.work.agentID)
	schedulerOwnedRequire(t, "runner-original-current-configuration", err)
	if len(v.before.Fields().AllowedMountIDs) != 1 || v.before.Fields().Core.Version != 2 {
		t.Fatal("queued assignment lost the nonempty original Mount configuration")
	}
	var originalCalls bool
	schedulerOwnedRequire(t, "runner-first-agent-owned-calls", base.raw.QueryRow(ctxFor(t), `SELECT count(*) FILTER (WHERE lane='service' AND class='mutation' AND phase='joined')=1 AND count(*) FILTER (WHERE lane='service' AND class='read' AND phase='joined')>=1 AND count(DISTINCT instance_id)=1 AND bool_and(process_id=$2::uuid AND phase='joined') FROM agenteam_agent.project_calls WHERE project_id=$1::uuid`, v.work.base.project.ID.String(), v.skills.process.String()).Scan(&originalCalls))
	if !originalCalls || a.createdConfig.Fields().Core.ID != v.before.Fields().Core.ID {
		t.Fatal("first Agent configuration did not belong to the original returned pair")
	}
	v.originalMounts = v.mountSnapshot(t, false)
	if upgrade {
		for n := 0; n < 2; n++ {
			migrate(t, base.db, migrationPrefix(t, "00088"))
			if v.originalMounts != v.mountSnapshot(t, false) {
				t.Fatal("migration88/repeat changed actual configuration")
			}
		}
	}
	stop, err := project.NewRunnerManagedStopAuthority(v.life, base.tracked)
	schedulerOwnedRequire(t, "runner-original-stop-gate", err)
	cleanup, err := project.NewRunnerManagedProjectCleanupAuthority(v.life, base.tracked)
	schedulerOwnedRequire(t, "runner-original-cleanup-gate", err)
	agents, err := agent.NewRunnerManagedProjectCalls(a.agents, base.tracked, v.skills.guard, v.skills.process, stop, cleanup)
	schedulerOwnedRequire(t, "runner-original-agent-adapter", err)
	executions, err := execution.NewRunnerManagedProjectCalls(v.owned, base.tracked, v.skills.guard, v.skills.process, stop, cleanup)
	schedulerOwnedRequire(t, "runner-original-execution-adapter", err)
	v.ownership, err = mount.NewProjectLifecycleOwnership(base.tracked, agents, executions, v.skills.guard, v.skills.process, stop)
	schedulerOwnedRequire(t, "runner-original-owner", err)
	v.participant, err = mount.NewProjectCleanupParticipant(v.ownership, cleanup)
	schedulerOwnedRequire(t, "runner-original-cleaner", err)
	v.bindManagedAgentCleanup(t)
	v.otherExecution(t)
	v.otherBefore = v.retirementOtherSnapshot(t)
	v.workBefore = v.retirementWorkSnapshot(t)
	v.protected = v.protectedSnapshot(t)
	v.accept(t)
	v.executionObserved(t, v.owned, false, pc.Stopped)
	v.observed(t, v.pair, false, pc.Stopped)
	v.stopRunnerManaged(t)
	v.enterRetirementCleanup(t) // Actual Scheduler cleanup; only missing Stop facts are controlled.
	v.driver = v.newRetirementDriver(t)
	for n := 0; n < 4; n++ {
		v.round(t)
		if v.completed(t, pc.ExecutionParticipant) {
			v.settled = true
			break
		}
	}
	if !v.settled {
		t.Fatal("original Execution predecessor did not actually complete")
	}
	v.requireRetirementFacts(t, true)
	v.cleanup = v.newManagedDriver(t)
	v.requireMountFacts(t, false)
	return v
}
func (v *runnerManagedPG) bindManagedAgentCleanup(t *testing.T) {
	t.Helper()
	s, owner := v.base.tracked, v.assembly.providers.Agents
	workGate, err := work.NewProjectAgentRetirement(s, owner)
	schedulerOwnedRequire(t, "runner-agent-work", err)
	executionGate, err := execution.NewProjectAgentRetirement(s, owner)
	schedulerOwnedRequire(t, "runner-agent-execution", err)
	models, err := model.NewProjectAgentRetirementReferences(s, owner)
	schedulerOwnedRequire(t, "runner-agent-models", err)
	tools, err := registry.NewProjectAgentRetirementReferences(s, owner)
	schedulerOwnedRequire(t, "runner-agent-tools", err)
	mounts, err := mount.NewProjectAgentRetirementReferencesWithRunnerManaged(s, owner, v.ownership)
	schedulerOwnedRequire(t, "runner-agent-original-mount-proof-consumer", err)
	variables, err := pv.NewProjectAgentRetirementReferences(s, owner)
	schedulerOwnedRequire(t, "runner-agent-variables", err)
	ingress, err := execution.NewProjectAgentIngressRetirement(s, owner)
	schedulerOwnedRequire(t, "runner-agent-ingress", err)
	skills, err := skill.NewProjectAgentRetirementReferencesWithIngress(s, owner, ingress)
	schedulerOwnedRequire(t, "runner-agent-skills", err)
	consumer, err := agent.NewProjectRetirement(owner, v.skills.projects, agent.ProjectRetirementDependencies{Work: workGate, Execution: executionGate, Models: models, Tools: tools, Mounts: mounts, Secrets: variables, Skills: skills})
	schedulerOwnedRequire(t, "runner-original-agent-consumer", err)
	v.agents, err = project.NewLifecycleCleanupDriverWithAgents(s, v.skills.projects, captureProviderProcesses{guard: v.skills.guard}, consumer, agentRetirementSkillsDiagnostic{SkillsCleanupDomain: v.skills.tap, t: t})
	schedulerOwnedRequire(t, "runner-agent-fixed-driver", err)
	v.drivers = append(v.drivers, v.agents)
}
func (v *runnerManagedPG) stopRunnerManaged(t *testing.T) {
	t.Helper()
	var report pc.StopReport
	v.stopStep = func(ctx context.Context, actor i.Actor, cause pc.LifecycleCause, scope pc.ScopeRef) error {
		var err error
		report, err = v.participant.RequestStop(ctx, actor, cause, scope)
		if err == nil && !report.Matches(pc.RunnerManagedParticipant, cause, scope) {
			return errors.New("Runner Stop changed its original identity")
		}
		return err
	}
	call := v.start(func(ctx context.Context) error { return v.stopDriver.Run(ctx, v.scope.ProjectID, v.cause.OperationID) })
	outboxCleanupWait(t, call.done, "original Runner Managed Stop")
	schedulerOwnedRequire(t, "runner-original-stop-return", call.err)
	if report.Details().State != pc.Stopped {
		t.Fatal("original Agent/Execution calls have not returned")
	}
}
func (v *runnerManagedPG) newManagedDriver(t *testing.T) *project.LifecycleCleanupDriver {
	t.Helper()
	driver, err := project.NewLifecycleCleanupDriverForRunnerManaged(v.base.tracked, v.life, captureProviderProcesses{guard: v.skills.guard}, v.participant)
	schedulerOwnedRequire(t, "runner-original-fixed-cleanup", err)
	v.cleanupDrivers = append(v.cleanupDrivers, driver)
	return driver
}
func (v *runnerManagedPG) completed(t *testing.T, name pc.ParticipantName) bool {
	t.Helper()
	var complete bool
	schedulerOwnedRequire(t, "runner-participant-completion", v.base.raw.QueryRow(ctxFor(t), `SELECT cleanup_state='completed' FROM agenteam_project.lifecycle_participants WHERE operation_id=$1::uuid AND participant_name=$2`, v.cause.OperationID.String(), string(name)).Scan(&complete))
	return complete
}
func (v *runnerManagedPG) runManaged(driver *project.LifecycleCleanupDriver) *skillsCleanupPGCall {
	v.cleanupStarted = true
	return v.start(func(ctx context.Context) error { return driver.Run(ctx, v.scope.ProjectID, v.cause.OperationID) })
}
func (v *runnerManagedPG) mountSnapshot(t *testing.T, proof bool) string {
	t.Helper()
	// Migration35/60 Mount IDs and the new proof are UUID. Execution history
	// uses TEXT and is covered by the original explicit-ID snapshot separately.
	query := `SELECT jsonb_build_object('head',(SELECT to_jsonb(x) FROM agenteam_mount.agent_mount_heads x WHERE project_id=$1::uuid AND agent_id=$2::uuid),'references',COALESCE((SELECT jsonb_agg(to_jsonb(x) ORDER BY mount_id) FROM agenteam_mount.agent_mount_refs x WHERE project_id=$1::uuid AND agent_id=$2::uuid),'[]'::jsonb),'workspaces',COALESCE((SELECT jsonb_agg(to_jsonb(x) ORDER BY workspace_id) FROM agenteam_mount.runner_workspaces x WHERE project_id=$1::uuid AND agent_id=$2::uuid),'[]'::jsonb),'definitions',COALESCE((SELECT jsonb_agg(to_jsonb(x) ORDER BY id) FROM agenteam_mount.mounts x WHERE project_id=$1::uuid AND agent_id=$2::uuid),'[]'::jsonb)`
	if proof {
		query += `,'proof',(SELECT to_jsonb(x) FROM agenteam_mount.project_agent_retirements x WHERE project_id=$1::uuid AND agent_id=$2::uuid)`
	}
	query += `)::text`
	var raw string
	schedulerOwnedRequire(t, "runner-complete-mount-snapshot", v.base.raw.QueryRow(ctxFor(t), query, v.work.base.project.ID.String(), v.work.agentID.String()).Scan(&raw))
	return raw
}
func (v *runnerManagedPG) protectedSnapshot(t *testing.T) string {
	t.Helper()
	var parts []string
	// The entire System Runner registration, enrollment credential and any
	// protocol history stay outside Project metadata retirement. Never log them.
	for _, table := range []string{"runners", "commands", "enrollment_tokens", "identity_events", "challenges", "connections"} {
		parts = append(parts, fmt.Sprintf(`(SELECT jsonb_agg(to_jsonb(x) ORDER BY to_jsonb(x)::text) FROM agenteam_runner.%s x)`, table))
	}
	for _, table := range []string{"mounts", "runner_workspaces", "agent_mount_heads", "agent_mount_refs"} {
		parts = append(parts, fmt.Sprintf(`(SELECT jsonb_agg(to_jsonb(x) ORDER BY to_jsonb(x)::text) FROM agenteam_mount.%s x WHERE project_id=$1::uuid)`, table))
	}
	parts = append(parts, `(SELECT to_jsonb(x) FROM agenteam_project.projects x WHERE id=$1::uuid)`)
	var raw string
	schedulerOwnedRequire(t, "runner-protected-system-and-B", v.base.raw.QueryRow(ctxFor(t), "SELECT jsonb_build_array("+strings.Join(parts, ",")+")::text", v.base.project.ID.String()).Scan(&raw))
	return raw
}
func (v *runnerManagedPG) requireProtected(t *testing.T) {
	t.Helper()
	if v.protected != v.protectedSnapshot(t) {
		t.Fatal("Project cleanup changed System Runner/credentials or B")
	}
	if _, err := os.Stat(v.assembly.root); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("logical metadata retirement touched the configured physical root", err)
	}
	page, err := v.assembly.agents.Service().ListAvailableRunners(ctxFor(t), v.base.ownerBrowser.actor, v.base.project.ID, f.PageRequest{Limit: 50})
	schedulerOwnedRequire(t, "runner-B-original-owned-read", err)
	if len(page.Items) != 1 || page.Items[0].ID != v.assembly.runner.ID {
		t.Fatal("B lost its real System Runner candidate")
	}
	if v.protected != v.protectedSnapshot(t) {
		t.Fatal("B candidate read changed protected facts")
	}
}
func (v *runnerManagedPG) requireMountFacts(t *testing.T, retired bool) {
	t.Helper()
	var counts [5]int
	schedulerOwnedRequire(t, "runner-original-mount-counts", v.base.raw.QueryRow(ctxFor(t), `SELECT
 (SELECT count(*) FROM agenteam_mount.agent_mount_heads WHERE project_id=$1::uuid AND agent_id=$2::uuid),
 (SELECT count(*) FROM agenteam_mount.agent_mount_refs WHERE project_id=$1::uuid AND agent_id=$2::uuid),
 (SELECT count(*) FROM agenteam_mount.runner_workspaces WHERE project_id=$1::uuid AND agent_id=$2::uuid),
 (SELECT count(*) FROM agenteam_mount.mounts WHERE project_id=$1::uuid AND agent_id=$2::uuid),
 (SELECT count(*) FROM agenteam_mount.project_agent_retirements WHERE project_id=$1::uuid AND agent_id=$2::uuid)`, v.scope.ProjectID.String(), v.work.agentID.String()).Scan(&counts[0], &counts[1], &counts[2], &counts[3], &counts[4]))
	want := [5]int{1, 1, 1, 1, 0}
	if retired {
		want = [5]int{1, 0, 0, 0, 1}
	}
	if counts != want {
		t.Fatal("exact nonempty Mount retirement facts differ", counts, want)
	}
	if !retired {
		return
	}
	before, err := json.Marshal(v.before)
	schedulerOwnedRequire(t, "runner-original-config-json", err)
	digest, err := ac.ProjectRetirementBeforeDigest(v.before)
	schedulerOwnedRequire(t, "runner-original-config-digest", err)
	var exact bool
	schedulerOwnedRequire(t, "runner-exact-immutable-proof", v.base.raw.QueryRow(ctxFor(t), `SELECT EXISTS(SELECT 1 FROM agenteam_mount.project_agent_retirements x WHERE project_id=$1::uuid AND agent_id=$2::uuid AND operation_id=$3::uuid AND project_version=$4 AND owner_version=$5 AND before_config=$6::jsonb AND before_digest=$7 AND mount_snapshot=$8::jsonb AND process_id=$9::uuid)`, v.scope.ProjectID.String(), v.work.agentID.String(), v.cause.OperationID.String(), int64(v.cause.ProjectVersion), int64(v.before.Fields().Core.Version), before, string(digest), v.originalMounts, v.skills.process.String()).Scan(&exact))
	if !exact {
		t.Fatal("Mount proof lost original nonempty configuration or complete metadata preimage")
	}
	var canonical bool
	schedulerOwnedRequire(t, "runner-canonical-before-agent-applied", v.base.raw.QueryRow(ctxFor(t), `SELECT EXISTS(SELECT 1 FROM agenteam_agent.agents a WHERE a.id=$1::uuid AND a.project_id=$2::uuid AND a.version=$3) AND (SELECT count(*) FROM agenteam_agent.mount_allowlist WHERE project_id=$2::uuid AND agent_id=$1::uuid)=1`, v.work.agentID.String(), v.scope.ProjectID.String(), int64(v.before.Fields().Core.Version)).Scan(&canonical))
	if !canonical {
		t.Fatal("Mount retirement rewrote the Agent's nonempty canonical configuration")
	}
}
func (v *runnerManagedPG) allSnapshot(t *testing.T) string {
	return v.mountSnapshot(t, true) + v.retirementSnapshot(t) + v.retirementWorkSnapshot(t) + v.protectedSnapshot(t)
}
func (v *runnerManagedPG) managedCause(cause f.TransactionCause) bool {
	d := cause.Details()
	return d.Kind == f.RecoveryCause && d.Owner == "runner-managed-project-cleanup" && d.CheckpointRef == v.scope.ProjectID.String()
}
func (v *runnerManagedPG) busyManaged(t *testing.T, original *skillsCleanupPGCall) {
	t.Helper()
	if original != nil {
		select {
		case <-original.done:
			t.Fatal("original cleanup returned before its Store")
		default:
		}
	}
	call := v.runManaged(v.newManagedDriver(t))
	outboxCleanupWait(t, call.done, "second Runner cleanup driver")
	if !schedulerOwnedCode(call.err, f.ResourceBusy) {
		agentRetirementDiagnostic(t, "runner-second-driver", call.err)
		t.Fatal("second driver crossed the original cleanup round")
	}
	v.requireProtected(t)
}
func (v *runnerManagedPG) holdManaged(t *testing.T, mask bool) (*skillsCleanupPGCall, *schedulerStopBarrier, *f.CommitResult) {
	t.Helper()
	s, barrier := v.base.tracked, v.barrier()
	var selected, masked atomic.Bool
	var original f.CommitResult
	attempt := id[f.TransactionAttempt](t)
	s.setAfter(func(ctx context.Context, tx f.Tx, cause f.TransactionCause) error {
		if !v.managedCause(cause) {
			return nil
		}
		x, err := s.InTx(tx)
		if err != nil {
			return err
		}
		var exact bool
		err = x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_mount.project_agent_retirements p WHERE p.project_id=$1::uuid AND p.agent_id=$2::uuid AND p.operation_id=$3::uuid AND p.xmin::text=pg_current_xact_id()::text AND NOT EXISTS(SELECT 1 FROM agenteam_mount.mounts WHERE project_id=p.project_id) AND NOT EXISTS(SELECT 1 FROM agenteam_mount.runner_workspaces WHERE project_id=p.project_id) AND NOT EXISTS(SELECT 1 FROM agenteam_mount.agent_mount_refs WHERE project_id=p.project_id))`, v.scope.ProjectID.String(), v.work.agentID.String(), v.cause.OperationID.String()).Scan(&exact)
		if err == nil && exact {
			selected.Store(true)
		}
		return err
	})
	s.mu.Lock()
	s.afterResult = func(cause f.TransactionCause, result f.CommitResult) {
		if v.managedCause(cause) && selected.Load() && result.State() == f.Committed {
			barrier.hold(result)
		}
	}
	if mask {
		s.transformResult = func(cause f.TransactionCause, result f.CommitResult) f.CommitResult {
			if v.managedCause(cause) && selected.Load() && result.State() == f.Committed && masked.CompareAndSwap(false, true) {
				original = f.UnknownResult(attempt, cause)
				return original
			}
			return result
		}
	}
	s.mu.Unlock()
	return v.runManaged(v.cleanup), barrier, &original
}
func (v *runnerManagedPG) confirmManaged(t *testing.T, original f.CommitResult) {
	t.Helper()
	v.clear()
	stable := v.allSnapshot(t)
	s, barrier := v.base.tracked, v.barrier()
	var baselines sync.Map
	var seen atomic.Int64
	relevant := func(c f.TransactionCause) bool { return v.managedCause(c) || v.retirementCause(c) }
	s.mu.Lock()
	s.before = func(ctx context.Context, tx f.Tx, cause f.TransactionCause) error {
		if !relevant(cause) {
			return nil
		}
		if v.managedCause(cause) && !schedulerOwnedSameCause(cause, original.Cause()) {
			return errors.New("Runner confirmation changed original cause")
		}
		x, err := s.InTx(tx)
		if err != nil {
			return err
		}
		var writes int64
		if err = x.QueryRow(ctx, schedulerCleanupWritesSQL).Scan(&writes); err != nil {
			return err
		}
		baselines.Store(tx, writes)
		return nil
	}
	s.after = func(ctx context.Context, tx f.Tx, cause f.TransactionCause) error {
		if !relevant(cause) {
			return nil
		}
		before, ok := baselines.LoadAndDelete(tx)
		if !ok {
			return errors.New("Runner confirmation lost original Tx")
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
			return errors.New("Runner confirmation wrote in its original Tx")
		}
		if v.managedCause(cause) {
			seen.Add(1)
		}
		return nil
	}
	s.afterResult = func(cause f.TransactionCause, result f.CommitResult) {
		if v.managedCause(cause) && seen.Load() == 1 && result.State() == f.Committed {
			barrier.hold(result)
		}
	}
	s.mu.Unlock()
	call := v.runManaged(v.cleanup)
	v.await(t, call, barrier)
	if seen.Load() != 1 || stable != v.allSnapshot(t) {
		t.Fatal("readonly confirmation changed the complete preimage")
	}
	v.busyManaged(t, call)
	barrier.open()
	outboxCleanupWait(t, call.done, "original Runner confirmation Store return")
	schedulerOwnedRequire(t, "runner-original-confirmation-return", call.err)
	v.clear()
	if stable != v.allSnapshot(t) || v.completed(t, pc.RunnerManagedParticipant) {
		t.Fatal("confirmation advanced before the next explicit Run")
	}
}
func testRunnerManagedCleanup(t *testing.T, unknown bool) {
	v := newRunnerManagedPG(t, !unknown)
	call, barrier, original := v.holdManaged(t, unknown)
	v.await(t, call, barrier)
	v.requireMountFacts(t, true)
	if v.completed(t, pc.RunnerManagedParticipant) {
		t.Fatal("Project checkpoint preceded original provider return")
	}
	v.busyManaged(t, call)
	barrier.open()
	outboxCleanupWait(t, call.done, "original Runner metadata Store return")
	v.clear()
	if unknown {
		actual, ok := mount.UnknownAttempt(call.err)
		if !ok || original.State() != f.Unknown || actual.AttemptID() != original.AttemptID() || !schedulerOwnedSameCause(actual.Cause(), original.Cause()) {
			agentRetirementDiagnostic(t, "runner-first-unknown", call.err)
			t.Fatal("Runner lost its original Unknown attempt/cause")
		}
		v.confirmManaged(t, *original)
	} else {
		schedulerOwnedRequire(t, "runner-known-original-return", call.err)
	}
	for n := 0; n < 4; n++ {
		call = v.runManaged(v.cleanup)
		outboxCleanupWait(t, call.done, "next explicit Runner cleanup round")
		schedulerOwnedRequire(t, "runner-next-explicit-round", call.err)
		if v.completed(t, pc.RunnerManagedParticipant) {
			v.cleanupDone = true
			break
		}
	}
	if !v.cleanupDone {
		t.Fatal("Runner metadata cleanup did not complete within original bounded rounds")
	}
	v.requireMountFacts(t, true)
	if !unknown {
		v.rejectProofRewrite(t)
	}
	v.requireProtected(t)
	v.finishRetirement(t) // Original Agent Applied consumes Mount and queued-Skill proofs.
	var remaining int
	schedulerOwnedRequire(t, "runner-proof-consumed-by-agent", v.base.raw.QueryRow(ctxFor(t), `SELECT (SELECT count(*) FROM agenteam_mount.agent_mount_heads WHERE project_id=$1::uuid)+(SELECT count(*) FROM agenteam_mount.project_agent_retirements WHERE project_id=$1::uuid)`, v.scope.ProjectID.String()).Scan(&remaining))
	if remaining != 0 {
		t.Fatal("Agent Applied left its original Mount head/proof")
	}
	v.requireProtected(t)
	v.noInvoke(t)
	t.Logf("PROJECT_RUNNER_MANAGED original_owned_agent=true nonempty_configuration=true actual_execution_completed=true immutable_proof_then_agent_applied=true original_store_return=true unknown=%t readonly_confirmation=%t system_runner_and_credentials_preserved=true B_usable=true physical_root_absent=true controlled_missing_stop=true no_default_registry=true no_invoke=true", unknown, unknown)
}
func (v *runnerManagedPG) rejectProofRewrite(t *testing.T) {
	t.Helper()
	before := v.allSnapshot(t)
	cause, err := f.NewRecoveryCause("runner-proof-rewrite-control", id[struct{}](t).String(), v.scope.ProjectID.String())
	schedulerOwnedRequire(t, "runner-proof-control-cause", err)
	key, err := f.ProjectLock(v.scope.ProjectID.String())
	schedulerOwnedRequire(t, "runner-proof-control-lock", err)
	s := v.base.tracked
	result := s.WithinTx(ctxFor(t), cause, func(ctx context.Context, tx f.Tx) error {
		if err := s.AcquireAll(ctx, tx, []f.LockRequest{{Key: key, Mode: f.Exclusive}}); err != nil {
			return err
		}
		x, err := s.InTx(tx)
		if err != nil {
			return err
		}
		_, err = x.Exec(ctx, `UPDATE agenteam_mount.project_agent_retirements SET owner_version=owner_version WHERE project_id=$1::uuid AND agent_id=$2::uuid`, v.scope.ProjectID.String(), v.work.agentID.String())
		return err
	})
	var database *postgres.Error
	if result.State() != f.NotCommitted || !errors.As(result.Fault(), &database) || database.SQLState() != "23514" || before != v.allSnapshot(t) {
		t.Fatal("ordinary SQL rewrote the immutable Runner retirement proof")
	}
}

func (v *runnerManagedPG) closeRunnerManaged(t *testing.T) {
	t.Helper()
	for _, barrier := range v.barriers {
		barrier.open()
	}
	for _, call := range v.calls {
		outboxCleanupWait(t, call.done, "Runner original call before shutdown")
	}
	v.clear()
	if v.cleanupStarted && !v.cleanupDone && v.cleanup != nil {
		for n := 0; n < 5; n++ {
			call := v.runManaged(v.cleanup)
			outboxCleanupWait(t, call.done, "Runner retained round before global Stop")
			if call.err != nil {
				t.Error("Runner cleanup failed to settle its original round", call.err)
				break
			}
			if v.completed(t, pc.RunnerManagedParticipant) {
				v.cleanupDone = true
				break
			}
		}
	}
	for _, driver := range v.cleanupDrivers {
		schedulerOwnedDrain(t, driver)
	}
	if v.ownership != nil {
		schedulerOwnedDrain(t, v.ownership)
	}
	v.closeRetirement(t)
}
