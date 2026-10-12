//go:build integration

package projectvariable_test

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/agent"
	ac "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/execution"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/model"
	"github.com/LunaDeerTech/agenteam/internal/central/mount"
	"github.com/LunaDeerTech/agenteam/internal/central/outbox"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/LunaDeerTech/agenteam/internal/central/project"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/projectvariable"
	"github.com/LunaDeerTech/agenteam/internal/central/scheduler"
	"github.com/LunaDeerTech/agenteam/internal/central/skill"
	"github.com/LunaDeerTech/agenteam/internal/central/tool/builtin"
	"github.com/LunaDeerTech/agenteam/internal/central/tool/registry"
	toolruntime "github.com/LunaDeerTech/agenteam/internal/central/tool/runtime"
	"github.com/LunaDeerTech/agenteam/internal/central/work"
	wc "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
	assembly "github.com/LunaDeerTech/agenteam/tests/testsupport/agentconfiguration"
)

// Accepted Delete/the earlier domains remain the inherited controlled phase
// prerequisite. The fixed cleanup driver creates its real claim and private
// Agent scope. All Agent/Model/Task/Skill facts below come from formal services.
// A todo Task and its immutable history survive; no terminal Task or completed
// Execution is fabricated. Variables remain pending, so this is not a complete
// LifecycleRegistry, BeginDelete/default-App path, or Project finalizer.
func TestProjectAgentRetirement(t *testing.T) {
	t.Run("canonical-references-and-original-return", testProjectAgentRetirementReturn)
	t.Run("original-unknown-and-applied-anchor", testProjectAgentRetirementUnknown)
	t.Run("enabled-assignment-rolls-back-retirement", testProjectAgentRetirementEnabled)
}

// This transparent delegate observes the actual Model call's private context;
// it grants nothing and does not change a transaction, result, or return tail.
type agentRetirementModelTap struct {
	ac.ProjectRetirementReferences
	mu      sync.Mutex
	saved   context.Context
	change  ac.ProjectRetirementChange
	retired int
	checked int
}

func (p *agentRetirementModelTap) RetireProjectAgentReferencesInTx(ctx context.Context, tx f.Tx, change ac.ProjectRetirementChange) error {
	err := p.ProjectRetirementReferences.RetireProjectAgentReferencesInTx(ctx, tx, change)
	p.mu.Lock()
	p.saved, p.change = context.WithoutCancel(ctx), change.Clone()
	if err == nil {
		p.retired++
	}
	p.mu.Unlock()
	return err
}
func (p *agentRetirementModelTap) RequireProjectAgentReferencesRetiredInTx(ctx context.Context, tx f.Tx, change ac.ProjectRetirementChange) error {
	err := p.ProjectRetirementReferences.RequireProjectAgentReferencesRetiredInTx(ctx, tx, change)
	if err == nil {
		p.mu.Lock()
		p.checked++
		p.mu.Unlock()
	}
	return err
}

type agentRetirementPG struct {
	*skillsCleanupPG
	configuration *agentCreateFixture
	consumer      *agent.ProjectRetirement
	models        *agentRetirementModelTap
	target        i.AgentID
	before        ac.AgentConfig
	command       ac.AgentCommandID
	history       string
	task          *taskTransitionFixture
	taskHistory   string
}

func newAgentRetirementPG(t *testing.T, enabled, withTask bool) *agentRetirementPG {
	t.Helper()
	v := &agentRetirementPG{skillsCleanupPG: newSkillsCleanupPG(t)}
	store := v.p2.base.tracked
	source, err := toolruntime.NewInstallSource(v.installer, v.p2.service)
	outboxCleanupRequire(t, err)
	graph, err := assembly.New(store, assembly.Options{Accounts: v.p2.base.accounts, BuiltinSources: map[string]registry.BuiltinSource{builtin.SkillInstallStableKey: source}, InstallSourceKey: builtin.SkillInstallStableKey})
	outboxCleanupRequire(t, err)
	a := &agentCreateFixture{p2: v.p2, guard: v.guard, providers: graph.Providers()}
	v.configuration = a
	// Even a disabled Agent traverses real Registry discovery. No false flag or
	// synthetic registration replaces this original InstallAuthority binding.
	v.p2.base.tx(t, []f.LockRequest{registry.RegistryLock(f.Exclusive)}, func(ctx context.Context, tx f.Tx, _ postgres.SQLExecutor) error {
		if err := v.p2.service.RequireInstallBindingInTx(ctx, tx, v.installer, v.process); err != nil {
			return err
		}
		registration, err := source.DescribeInTx(ctx, tx)
		if err != nil {
			return err
		}
		if err = source.CheckBindingInTx(ctx, tx, registration); err != nil {
			return err
		}
		a.tool, err = a.providers.Registry.ReconcileBuiltinInTx(ctx, tx, builtin.SkillInstallStableKey)
		return err
	})
	processes := captureProviderProcesses{guard: v.guard}
	a.modelID = a.createModel(t, fixtureProcess{processes.CurrentProcess()})
	v.agents, err = graph.NewService(v.p2.base.keys, processes)
	outboxCleanupRequire(t, err)
	a.agents = v.agents
	disabled := false
	v.target = id[i.Agent](t)
	request, err := ac.NewAgentCreate(ac.AgentCreateFields{AgentID: v.target, Name: "retirement-agent", Description: "Original Project-owned Agent", Instructions: "No execution or provider request is started", ModelRef: a.modelID, ApprovalPolicy: ac.ApprovalAuto, ApprovalModelRef: &a.modelID, AllowedToolIDs: []i.ToolID{}, AllowedMountIDs: []i.MountID{}, AllowedSecretVariableIDs: []i.ProjectVariableID{}, AddSkillsEnabled: &enabled, InstallSkillEnabled: &disabled})
	outboxCleanupRequire(t, err)
	receipt, err := v.agents.CreateAgent(ctxFor(t), v.p2.base.ownerBrowser.actor, meta(t, "project-agent-retirement-create", nil), v.p2.project.ID, request)
	outboxCleanupRequire(t, err)
	if receipt.Validate() != nil || receipt.Fields().Agent.Fields().Core.ID != v.target {
		t.Fatal("original Agent creation did not return its exact configuration")
	}
	v.before = receipt.Fields().Agent.Clone()
	var raw string
	outboxCleanupRequire(t, v.p2.base.raw.QueryRow(ctxFor(t), `SELECT id::text FROM agenteam_agent.commands WHERE project_id=$1 AND target_id=$2 AND state='completed' AND command_name='agent.create'`, v.p2.project.ID.String(), v.target.String()).Scan(&raw))
	v.command, err = f.ParseID[ac.AgentCommand](raw)
	outboxCleanupRequire(t, err)
	workGate, err := work.NewProjectAgentRetirement(store, a.providers.Agents)
	outboxCleanupRequire(t, err)
	executionGate, err := execution.NewProjectAgentRetirement(store, a.providers.Agents)
	outboxCleanupRequire(t, err)
	models, err := model.NewProjectAgentRetirementReferences(store, a.providers.Agents)
	outboxCleanupRequire(t, err)
	v.models = &agentRetirementModelTap{ProjectRetirementReferences: models}
	tools, err := registry.NewProjectAgentRetirementReferences(store, a.providers.Agents)
	outboxCleanupRequire(t, err)
	mounts, err := mount.NewProjectAgentRetirementReferences(store, a.providers.Agents)
	outboxCleanupRequire(t, err)
	secrets, err := projectvariable.NewProjectAgentRetirementReferences(store, a.providers.Agents)
	outboxCleanupRequire(t, err)
	skills, err := skill.NewProjectAgentRetirementReferences(store, a.providers.Agents)
	outboxCleanupRequire(t, err)
	// The original concrete Project authority owns the Skills/driver lifecycle.
	// The ordinary Agent assembly retains its own same-Store read authority;
	// it is never substituted for the real private Project Delete issuer.
	v.consumer, err = agent.NewProjectRetirement(a.providers.Agents, v.projects, agent.ProjectRetirementDependencies{Work: workGate, Execution: executionGate, Models: v.models, Tools: tools, Mounts: mounts, Secrets: secrets, Skills: skills})
	outboxCleanupRequire(t, err)
	if withTask {
		v.task = agentRetirementTask(t, a, v.target, processes)
		v.taskHistory = v.task.databaseSnapshot(t)
	}
	v.agents.Stop()
	outboxCleanupRequire(t, v.agents.Drain(ctxFor(t)))
	if !v.agents.Joined() {
		t.Fatal("original Agent creation owner has not returned")
	}
	v.captureObjects(t, 1)
	v.requireReferences(t, true, enabled)
	v.history = v.historySnapshot(t)
	v.enterCleanup(t)
	store.mu.Lock()
	store.afterResult = func(cause f.TransactionCause, result f.CommitResult) {
		v.diagnoseResult(t, cause, result)
	}
	store.mu.Unlock()
	t.Cleanup(v.clearHooks)
	return v
}

// Only inspect the original returned failure. No SQL, arguments, identifiers,
// driver message or credentials are formatted, and no transaction is replayed.
func agentRetirementDiagnostic(t *testing.T, stage string, err error) {
	t.Helper()
	if err == nil {
		return
	}
	code, state, databaseCode, sqlState := "none", "none", "none", ""
	var fault *f.Fault
	if errors.As(err, &fault) {
		code, state = string(fault.Code.Safe()), string(fault.CommitState.Safe())
	}
	var databaseError *postgres.Error
	if errors.As(err, &databaseError) {
		databaseCode, sqlState = string(databaseError.Code()), databaseError.SQLState()
	}
	t.Logf("AGENT_RETIREMENT_FAILURE stage=%s code=%s commit_state=%s database_code=%s sqlstate=%q cause_deadline=%t cause_canceled=%t", stage, code, state, databaseCode, sqlState, errors.Is(err, context.DeadlineExceeded), errors.Is(err, context.Canceled))
}

func (v *agentRetirementPG) diagnoseResult(t *testing.T, cause f.TransactionCause, result f.CommitResult) {
	stage := ""
	if v.lifecycleTransaction(cause) {
		stage = "lifecycle-transaction"
	} else if cause.Kind() == f.RecoveryCause && cause.Details().Owner == "agent-project-retirement" {
		stage = "agent-read-transaction"
	}
	if stage != "" && result.State() != f.Committed {
		agentRetirementDiagnostic(t, stage, result.Fault())
	}
}

func (v *agentRetirementPG) newDriver(t *testing.T) *project.LifecycleCleanupDriver {
	t.Helper()
	driver, err := project.NewLifecycleCleanupDriverWithAgents(v.p2.base.tracked, v.projects, captureProviderProcesses{guard: v.guard}, v.consumer, v.tap)
	outboxCleanupRequire(t, err)
	v.drivers = append(v.drivers, driver)
	return driver
}

// Independent persisted projection; deliberately not the product's decoder.
type agentRetirementCheckpoint struct {
	Project    string    `json:"project_id"`
	Operation  string    `json:"operation_id"`
	Version    f.Version `json:"project_version"`
	AgentsDone bool      `json:"agents_done"`
	Applied    *struct {
		Agent   i.AgentID         `json:"agent_id"`
		Version f.Version         `json:"version"`
		Command ac.AgentCommandID `json:"command_id"`
		Digest  f.Digest          `json:"before_digest"`
	} `json:"agent_applied,omitempty"`
	Skills struct {
		Project    string    `json:"project_id"`
		Operation  string    `json:"operation_id"`
		Version    f.Version `json:"project_version"`
		State      string    `json:"skills_state"`
		Checkpoint *struct {
			Schema f.Version `json:"schema"`
			Bytes  []byte    `json:"bytes"`
		} `json:"skills_checkpoint,omitempty"`
	} `json:"skills_progress"`
}

func (v *agentRetirementPG) checkpoint(t *testing.T, present bool) agentRetirementCheckpoint {
	t.Helper()
	var lifecycle, state, stage, stop, cleanup, phase, process string
	var schema *int64
	var raw []byte
	err := v.p2.base.raw.QueryRow(ctxFor(t), `SELECT p.lifecycle,o.state,o.cleanup_stage,r.stop_state,r.cleanup_state,r.checkpoint_schema,r.checkpoint,w.phase,w.process_id::text FROM agenteam_project.projects p JOIN agenteam_project.lifecycle_operations o ON o.id=p.current_lifecycle_operation_id JOIN agenteam_project.lifecycle_participants r ON r.operation_id=o.id AND r.participant_name='agent-skills-variables' JOIN agenteam_project.work_claims w ON w.work_id=o.id AND w.work_kind='lifecycle' WHERE p.id=$1 AND o.id=$2`, v.p2.project.ID.String(), v.cause.OperationID.String()).Scan(&lifecycle, &state, &stage, &stop, &cleanup, &schema, &raw, &phase, &process)
	outboxCleanupRequire(t, err)
	if lifecycle != "deleting" || state != "cleaning" || stage != "domains" || stop != "stopped" || phase != "terminal" || process != v.process.String() {
		t.Fatal("Agent substep changed the original Project stage or exact claim owner")
	}
	var cp agentRetirementCheckpoint
	if !present {
		if schema != nil || raw != nil || cleanup != "required" {
			t.Fatal("refused retirement persisted an applied checkpoint")
		}
		return cp
	}
	if schema == nil || *schema != 3 || json.Unmarshal(raw, &cp) != nil || cleanup != "pending" || cp.Project != v.p2.project.ID.String() || cp.Operation != v.cause.OperationID.String() || cp.Version != v.cause.ProjectVersion || cp.Applied == nil {
		t.Fatal("original schema3 checkpoint lost its exact retirement identity")
	}
	digest, err := ac.ProjectRetirementBeforeDigest(v.before)
	outboxCleanupRequire(t, err)
	if cp.Applied.Agent != v.target || cp.Applied.Version != v.before.Fields().Core.Version || cp.Applied.Command != v.command || cp.Applied.Digest != digest || cp.Skills.Project != cp.Project || cp.Skills.Operation != cp.Operation || cp.Skills.Version != cp.Version || cp.Skills.State != "pending" && cp.Skills.State != "completed" {
		t.Fatal("checkpoint changed the immutable full Before anchor or child scope")
	}
	if cp.Skills.Checkpoint != nil && (cp.Skills.Checkpoint.Schema != 1 || len(cp.Skills.Checkpoint.Bytes) == 0) {
		t.Fatal("original Skills checkpoint was not preserved")
	}
	return cp
}

func (v *agentRetirementPG) requireReferences(t *testing.T, present, enabled bool) {
	t.Helper()
	var canonical, models, tools, mounts, skills, assignments, extras int
	err := v.p2.base.raw.QueryRow(ctxFor(t), `SELECT
 (SELECT count(*) FROM agenteam_agent.agents WHERE id::text=$1),
 (SELECT count(*) FROM agenteam_model.references WHERE owner_kind='agent' AND owner_id::text=$1),
 (SELECT count(*) FROM agenteam_tool.agent_configurations WHERE agent_id::text=$1),
 (SELECT count(*) FROM agenteam_mount.agent_mount_heads WHERE agent_id::text=$1),
 (SELECT count(*) FROM agenteam_skill.agent_assignment_heads WHERE agent_id::text=$1),
 (SELECT count(*) FROM agenteam_skill.agent_assignments WHERE agent_id::text=$1),
 (SELECT count(*) FROM agenteam_agent.tool_allowlist WHERE agent_id::text=$1)+(SELECT count(*) FROM agenteam_agent.mount_allowlist WHERE agent_id::text=$1)+(SELECT count(*) FROM agenteam_agent.secret_allowlist WHERE agent_id::text=$1)+(SELECT count(*) FROM agenteam_tool.agent_references WHERE agent_id::text=$1)+(SELECT count(*) FROM agenteam_mount.agent_mount_refs WHERE agent_id::text=$1)+(SELECT count(*) FROM agenteam_projectvariable.secret_references WHERE owner_kind='agent' AND owner_id::text=$1)+(SELECT count(*) FROM agenteam_execution.executions WHERE agent_id::text=$1)`, v.target.String()).Scan(&canonical, &models, &tools, &mounts, &skills, &assignments, &extras)
	outboxCleanupRequire(t, err)
	want, grants := 0, 0
	if present {
		want = 1
		if enabled {
			grants = 1
		}
	}
	if canonical != want || models != 2*want || tools != want || mounts != want || skills != want || assignments != grants || extras != 0 {
		t.Fatal("canonical Agent and all five actual reference owners disagree", canonical, models, tools, mounts, skills, assignments, extras)
	}
}

func (v *agentRetirementPG) referenceSnapshot(t *testing.T) string {
	t.Helper()
	var value string
	err := v.p2.base.raw.QueryRow(ctxFor(t), `SELECT jsonb_build_array(
 (SELECT jsonb_agg(to_jsonb(r) ORDER BY role) FROM agenteam_model.references r WHERE owner_kind='agent' AND owner_id::text=$1),
 (SELECT jsonb_agg(to_jsonb(h) ORDER BY agent_id) FROM agenteam_tool.agent_configurations h WHERE agent_id::text=$1),
 (SELECT jsonb_agg(to_jsonb(r) ORDER BY tool_id) FROM agenteam_tool.agent_references r WHERE agent_id::text=$1),
 (SELECT jsonb_agg(to_jsonb(h) ORDER BY agent_id) FROM agenteam_mount.agent_mount_heads h WHERE agent_id::text=$1),
 (SELECT jsonb_agg(to_jsonb(r) ORDER BY mount_id) FROM agenteam_mount.agent_mount_refs r WHERE agent_id::text=$1),
 (SELECT jsonb_agg(to_jsonb(r) ORDER BY variable_id) FROM agenteam_projectvariable.secret_references r WHERE owner_kind='agent' AND owner_id::text=$1)
 )::text`, v.target.String()).Scan(&value)
	outboxCleanupRequire(t, err)
	return value
}

func (v *agentRetirementPG) historySnapshot(t *testing.T) string {
	t.Helper()
	var value string
	err := v.p2.base.raw.QueryRow(ctxFor(t), `SELECT jsonb_build_array(
 (SELECT jsonb_agg(to_jsonb(c) ORDER BY id) FROM agenteam_agent.commands c WHERE target_id::text=$1),
 (SELECT jsonb_agg(to_jsonb(a) ORDER BY id) FROM agenteam_audit.audit_records a WHERE resource_id::text=$1),
 (SELECT jsonb_agg(to_jsonb(e) ORDER BY id) FROM agenteam_outbox.events e WHERE aggregate_id::text=$1),
 (SELECT jsonb_agg(to_jsonb(m) ORDER BY id) FROM agenteam_model.models m),
 (SELECT jsonb_agg(to_jsonb(p) ORDER BY id) FROM agenteam_model.providers p)
 )::text`, v.target.String()).Scan(&value)
	outboxCleanupRequire(t, err)
	return value
}

func (v *agentRetirementPG) unchanged(t *testing.T) {
	t.Helper()
	if v.historySnapshot(t) != v.history || v.task != nil && v.task.databaseSnapshot(t) != v.taskHistory {
		t.Fatal("retirement changed completed Agent commands, Model resources or Task/history")
	}
	v.isolated(t)
}

func (v *agentRetirementPG) lifecycleTransaction(cause f.TransactionCause) bool {
	d := cause.Details()
	return cause.Kind() == f.JobCause && d.JobType == "project-lifecycle" && d.JobID == v.cause.OperationID.String()
}

// Select the actual transaction by its original lifecycle cause and the
// already-written exact applied checkpoint, never merely by a missing Agent.
func (v *agentRetirementPG) observeApplied(t *testing.T) *atomic.Bool {
	t.Helper()
	store := v.p2.base.tracked
	observed := &atomic.Bool{}
	store.setAfter(func(ctx context.Context, tx f.Tx, cause f.TransactionCause) error {
		if !v.lifecycleTransaction(cause) || observed.Load() {
			return nil
		}
		x, err := store.InTx(tx)
		if err != nil {
			return err
		}
		var applied bool
		err = x.QueryRow(ctx, `SELECT NOT EXISTS(SELECT 1 FROM agenteam_agent.agents WHERE id::text=$1) AND EXISTS(SELECT 1 FROM agenteam_project.lifecycle_participants WHERE operation_id::text=$2 AND participant_name='agent-skills-variables' AND checkpoint_schema=3 AND convert_from(checkpoint,'UTF8')::jsonb->'agent_applied'->>'agent_id'=$1)`, v.target.String(), v.cause.OperationID.String()).Scan(&applied)
		if err == nil && applied {
			observed.Store(true)
		}
		return err
	})
	return observed
}

func (v *agentRetirementPG) clearHooks() {
	store := v.p2.base.tracked
	store.setAfter(nil)
	store.mu.Lock()
	store.afterResult, store.transformResult = nil, nil
	store.mu.Unlock()
}

func (v *agentRetirementPG) expiredModelWitness(t *testing.T) {
	t.Helper()
	v.models.mu.Lock()
	saved, change := v.models.saved, v.models.change.Clone()
	v.models.mu.Unlock()
	if saved == nil || change.Validate() != nil {
		t.Fatal("original applied Model call was not observed")
	}
	locks, err := v.models.ProjectRetirementLocks(change)
	outboxCleanupRequire(t, err)
	// WithoutCancel cannot revive the closed private witness, even with the
	// original full public change, live same-Store Tx and all required locks.
	v.p2.base.tx(t, locks, func(_ context.Context, tx f.Tx, _ postgres.SQLExecutor) error {
		if err := v.models.ProjectRetirementReferences.RequireProjectAgentReferencesRetiredInTx(saved, tx, change); !outboxCleanupFault(err, f.Forbidden) {
			return errors.New("saved applied context authorized a later transaction")
		}
		return nil
	})
}

func (v *agentRetirementPG) finishSkills(t *testing.T, driver *project.LifecycleCleanupDriver) {
	t.Helper()
	call := v.run(t, driver)
	outboxCleanupWait(t, call.done, "exact applied-anchor confirmation and empty Agent set")
	outboxCleanupRequire(t, call.err)
	cp := v.checkpoint(t, true)
	if !cp.AgentsDone || cp.Skills.State != "pending" || v.tap.calls.Load() != 0 {
		t.Fatal("Agent confirmation skipped into Skills or completed its parent")
	}
	v.models.mu.Lock()
	checked, retired := v.models.checked, v.models.retired
	v.models.mu.Unlock()
	if checked != 1 || retired != 1 {
		t.Fatal("recovery did not confirm the exact original owners or resent retirement")
	}
	// Each explicit next round consumes actual Pending. The inherited one
	// package pipeline has separately returned physical/history/metadata rounds.
	for round := 0; round < 24 && cp.Skills.State == "pending"; round++ {
		call = v.run(t, driver)
		outboxCleanupWait(t, call.done, "original Skills cleanup round after Agent retirement")
		outboxCleanupRequire(t, call.err)
		cp = v.checkpoint(t, true)
		if !cp.AgentsDone {
			t.Fatal("Skills progress lost the confirmed Agent checkpoint")
		}
		v.unchanged(t)
	}
	if cp.Skills.State != "completed" || v.tap.calls.Load() == 0 {
		t.Fatal("real Skills provider did not complete its child cleanup")
	}
	v.empty(t)
	v.requireReferences(t, false, false)
	v.unchanged(t)
	count := v.tap.calls.Load()
	call = v.run(t, driver)
	outboxCleanupWait(t, call.done, "completed children parent-pending replay")
	outboxCleanupRequire(t, call.err)
	if v.checkpoint(t, true).Skills.State != "completed" || v.tap.calls.Load() != count {
		t.Fatal("completed child replay re-entered Skills or completed the whole parent")
	}
}

func testProjectAgentRetirementReturn(t *testing.T) {
	v := newAgentRetirementPG(t, false, true)
	driver := v.newDriver(t)
	observed := v.observeApplied(t)
	v.barrier = newSchedulerStopBarrier()
	store := v.p2.base.tracked
	store.mu.Lock()
	store.afterResult = func(cause f.TransactionCause, result f.CommitResult) {
		v.diagnoseResult(t, cause, result)
		if v.lifecycleTransaction(cause) && observed.Load() {
			v.barrier.hold(result)
		}
	}
	store.mu.Unlock()
	defer v.clearHooks()
	call := v.run(t, driver)
	wait, end := context.WithTimeout(ctxFor(t), 5*time.Second)
	defer end()
	select {
	case result := <-v.barrier.entered:
		if result.State() != f.Committed {
			t.Fatal("Agent retirement barrier did not observe the original physical COMMIT")
		}
	case <-call.done:
		agentRetirementDiagnostic(t, "original-run-before-barrier", call.err)
		t.Fatal("original retirement returned before its required COMMIT barrier", call.err)
	case <-wait.Done():
		t.Fatal("Agent retirement original Store return barrier was not reached")
	}
	v.requireReferences(t, false, false)
	cp := v.checkpoint(t, true)
	if cp.AgentsDone || cp.Skills.State != "pending" || v.tap.calls.Load() != 0 {
		t.Fatal("physical Agent commit advanced another substep before its original return")
	}
	v.unchanged(t)
	driver.Stop()
	drainCtx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	err := driver.Drain(drainCtx)
	cancel()
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("cleanup owner retired before its original Store return", err)
	}
	select {
	case <-call.done:
		t.Fatal("original Store call returned through the held boundary")
	default:
	}
	v.barrier.open()
	outboxCleanupWait(t, call.done, "original atomic retirement return after cancellation")
	if call.err == nil {
		t.Fatal("cancelled original round was reported as a successful returned round")
	}
	outboxCleanupRequire(t, driver.Drain(ctxFor(t)))
	v.clearHooks()
	v.expiredModelWitness(t)
	v.finishSkills(t, v.newDriver(t))
	v.close(t)
	t.Log("PROJECT_AGENT_RETIREMENT_RETURN real_disabled_agent=true two_model_roles=true original_task_history=true atomic_checkpoint=true original_return_held=true exact_anchor_confirmed=true skills_consumed=true parent_pending_domains=true")
}

func testProjectAgentRetirementUnknown(t *testing.T) {
	v := newAgentRetirementPG(t, false, false)
	driver := v.newDriver(t)
	observed := v.observeApplied(t)
	store := v.p2.base.tracked
	attempt := id[f.TransactionAttempt](t)
	var masked atomic.Bool
	var originalCause f.TransactionCause
	store.mu.Lock()
	store.transformResult = func(cause f.TransactionCause, result f.CommitResult) f.CommitResult {
		if v.lifecycleTransaction(cause) && observed.Load() && result.State() == f.Committed && masked.CompareAndSwap(false, true) {
			originalCause = cause
			// Local test acknowledgement mask of an actually returned COMMIT.
			// This attempt identifies the mask, not a physical/network attempt.
			return f.UnknownResult(attempt, cause)
		}
		return result
	}
	store.mu.Unlock()
	defer v.clearHooks()
	call := v.run(t, driver)
	outboxCleanupWait(t, call.done, "original locally masked atomic retirement return")
	result, ok := project.UnknownAttempt(call.err)
	if !masked.Load() || !ok || !outboxCleanupFault(call.err, f.CommitUnknown) || result.AttemptID() != attempt || !reflect.DeepEqual(result.Cause().Details(), originalCause.Details()) {
		agentRetirementDiagnostic(t, "original-run-unknown", call.err)
		t.Fatal("original acknowledgement Unknown lost its cause/attempt", call.err)
	}
	v.requireReferences(t, false, false)
	if v.checkpoint(t, true).AgentsDone || v.tap.calls.Load() != 0 {
		t.Fatal("Unknown was treated as rollback or completed Agent enumeration")
	}
	v.unchanged(t)
	v.clearHooks()
	v.expiredModelWitness(t)
	// Explicit next Run, only after the original return, consumes the complete
	// persisted anchor and immutable command. Missing canonical rows alone do
	// not authorize any reference owner's confirmation.
	v.finishSkills(t, driver)
	v.close(t)
	t.Log("PROJECT_AGENT_RETIREMENT_UNKNOWN local_committed_ackmask=true original_attempt_cause=true applied_anchor_preserved=true original_return_then_confirmation=true no_resend=true skills_consumed=true parent_pending_domains=true")
}

func testProjectAgentRetirementEnabled(t *testing.T) {
	v := newAgentRetirementPG(t, true, false)
	before, references := v.retained(t), v.referenceSnapshot(t)
	call := v.run(t, v.newDriver(t))
	outboxCleanupWait(t, call.done, "real enabled Skill assignment retirement refusal")
	if !outboxCleanupFault(call.err, f.ResourceBusy) {
		agentRetirementDiagnostic(t, "original-run-enabled", call.err)
		t.Fatal("enabled immutable Skill grant was accepted for retirement", call.err)
	}
	v.checkpoint(t, false)
	v.requireReferences(t, true, true)
	if v.retained(t) != before || v.referenceSnapshot(t) != references || v.tap.calls.Load() != 0 {
		t.Fatal("late Skill refusal committed partial canonical/reference removal")
	}
	v.nativePresent(t)
	v.unchanged(t)
	v.close(t)
	t.Log("PROJECT_AGENT_RETIREMENT_BUSY real_enabled_assignment=true original_error=true canonical_and_all_references_rollback=true no_skills_cleanup=true parent_pending_domains=true")
}

// Reuse the existing formal Task composition on this same Store/P/Agent. No
// Scheduler, Execution, task state or history row is manufactured by SQL.
func agentRetirementTask(t *testing.T, a *agentCreateFixture, agentID i.AgentID, processes oc.ProcessAuthority) *taskTransitionFixture {
	t.Helper()
	var err error
	// This is only a fixture projection onto the actual P2 Project. The Store,
	// Account session and domain authorities retain their original identity.
	base := *a.p2.base
	base.project = a.p2.project
	base.projectAuthority = a.providers.Projects
	v := &taskTransitionFixture{agent: a, base: &base, agentID: agentID}
	v.authority, err = work.NewAuthority(base.tracked, base.projectAuthority)
	if err != nil {
		t.Fatal("same-Store Work authority", err)
	}
	catalog := event.NewCatalog()
	workEvents, err := wc.RegisterWorkEvents(catalog)
	if err != nil {
		t.Fatal("formal Work events", err)
	}
	taskEvents, err := wc.RegisterTaskEvents(catalog)
	if err != nil {
		t.Fatal("formal Task events", err)
	}
	v.transitionEvents, err = wc.RegisterTaskTransitionEvents(catalog)
	if err != nil {
		t.Fatal("formal Task transition events", err)
	}
	v.box, err = outbox.New(base.tracked, catalog, outbox.Authorizations{
		Producers: map[event.StableName]oc.ProducerAuthority{wc.WorkProducer: v.authority},
		Sessions:  base.accounts, System: base.accounts, Projects: base.projectAuthority,
		Audit: base.audit, Cursors: base.keys, Processes: processes,
	})
	if err != nil {
		t.Fatal("same-Store formal Work producer", err)
	}
	v.structure, err = work.New(base.tracked, work.Dependencies{Authority: v.authority, Events: v.box, WorkEvents: workEvents, Activity: base.accounts})
	if err != nil {
		t.Fatal("formal Work service", err)
	}
	t.Cleanup(func() {
		v.structure.Stop()
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := v.structure.Drain(ctx); err != nil {
			t.Error("original Work calls did not join", err)
			_ = v.structure.Drain(context.Background())
		}
	})
	v.structureReader, err = work.NewReader(base.tracked, v.authority, base.keys)
	if err != nil {
		t.Fatal("same-Store Work reader", err)
	}
	v.pending, err = scheduler.NewPendingAuthority(base.tracked)
	if err != nil {
		t.Fatal("same-Store pending Dispatch authority", err)
	}
	v.tasks, err = work.NewTask(base.tracked, work.TaskDependencies{Structure: v.structureReader, Authority: v.authority, Events: v.box, TaskEvents: taskEvents, Activity: base.accounts, Pending: v.pending})
	if err != nil {
		t.Fatal("formal Task service", err)
	}
	t.Cleanup(func() {
		v.tasks.Stop()
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := v.tasks.Drain(ctx); err != nil {
			t.Error("original Task calls did not join", err)
			_ = v.tasks.Drain(context.Background())
		}
	})
	v.taskReader, err = work.NewTaskReader(base.tracked, v.authority, v.structureReader, base.keys)
	if err != nil {
		t.Fatal("same-Store Task reader", err)
	}
	milestone := wc.CreateMilestoneRequest{MilestoneID: id[wc.Milestone](t), Title: "Task transition source", Description: "Created by the current Owner"}
	if _, err := v.structure.CreateMilestone(ctxFor(t), base.ownerBrowser.actor, meta(t, "transition-milestone", nil), base.project.ID, milestone); err != nil {
		t.Fatal("formal milestone creation", err)
	}
	sprint := wc.CreateSprintRequest{SprintID: id[pc.Sprint](t), MilestoneID: milestone.MilestoneID, Title: "Task transition sprint", Description: "No scheduler state is seeded"}
	if _, err := v.structure.CreateSprint(ctxFor(t), base.ownerBrowser.actor, meta(t, "transition-sprint", nil), base.project.ID, sprint); err != nil {
		t.Fatal("formal sprint creation", err)
	}
	created, err := v.tasks.CreateTask(ctxFor(t), base.ownerBrowser.actor, meta(t, "transition-task", nil), base.project.ID, wc.TaskCreate{
		TaskID: id[wc.Task](t), SprintID: sprint.SprintID, Title: "Assign the real Agent", Description: "A current Owner changes the backlog task",
		Type: wc.TaskTypeTask, Priority: wc.TaskPriorityMedium, Plan: "Exercise the committed transition without launching an Execution",
	})
	if err != nil || !created.Changed || created.TaskEventID == nil || len(created.EventIDs) != 1 || created.Task.State != wc.TaskStateBacklog || created.Task.AssigneeAgentID != nil {
		t.Fatal("formal Task creation did not produce the unassigned backlog fact", err)
	}
	v.task = created.Task
	occupancy, err := execution.NewWorkOccupancy(base.tracked)
	if err != nil {
		t.Fatal("same-Store real Execution occupancy", err)
	}
	v.transitions, err = work.NewTaskTransition(base.tracked, work.TaskTransitionDependencies{
		Structure: v.structureReader, TaskEvents: v.transitionEvents, Authority: v.authority,
		Events: v.box, Activity: base.accounts, Agents: a.providers.Agents,
		Occupancy: occupancy, Pending: v.pending,
	})
	if err != nil {
		t.Fatal("same-Store actual Task transition composition", err)
	}
	t.Cleanup(func() {
		v.transitions.Stop()
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := v.transitions.Drain(ctx); err != nil {
			t.Error("original Task transition calls did not join", err)
			_ = v.transitions.Drain(context.Background())
		}
	})
	request, command, _ := v.transferRequest(t)
	assigned, err := v.transitions.TransferTask(ctxFor(t), base.ownerBrowser.actor, command, base.project.ID, v.task.ID, request)
	outboxCleanupRequire(t, err)
	if assigned.Validate() != nil || assigned.Task.State != wc.TaskStateTodo || assigned.Task.AssigneeAgentID == nil || *assigned.Task.AssigneeAgentID != agentID {
		t.Fatal("formal Task assignment did not preserve the original Agent identity")
	}
	v.task = assigned.Task
	// No live Work command is carried into the controlled Delete prerequisite.
	// These are the original services' actual returned drains, not an inferred
	// result from the Project phase or absence of Execution rows.
	v.transitions.Stop()
	outboxCleanupRequire(t, v.transitions.Drain(ctxFor(t)))
	v.tasks.Stop()
	outboxCleanupRequire(t, v.tasks.Drain(ctxFor(t)))
	v.structure.Stop()
	outboxCleanupRequire(t, v.structure.Drain(ctxFor(t)))
	return v
}
