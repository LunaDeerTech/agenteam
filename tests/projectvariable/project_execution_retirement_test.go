//go:build integration

package projectvariable_test

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/agent"
	ac "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/audit"
	au "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/execution"
	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/model"
	"github.com/LunaDeerTech/agenteam/internal/central/mount"
	objc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/outbox"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/LunaDeerTech/agenteam/internal/central/project"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	pv "github.com/LunaDeerTech/agenteam/internal/central/projectvariable"
	"github.com/LunaDeerTech/agenteam/internal/central/scheduler"
	"github.com/LunaDeerTech/agenteam/internal/central/secret"
	"github.com/LunaDeerTech/agenteam/internal/central/skill"
	sc "github.com/LunaDeerTech/agenteam/internal/central/skill/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/tool/registry"
	"github.com/LunaDeerTech/agenteam/internal/central/work"
	assembly "github.com/LunaDeerTech/agenteam/tests/testsupport/agentconfiguration"
	pgfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/postgres"
)

// Launch creates an Execution and never invokes it. Its original owned pair
// is bound to retirement before that first call. A real queued assignment is
// retired atomically, then its unchanged receipt/proof is consumed by Agent.
// Tool/Mount/PV Execution captures are empty in this Created-only shape; no
// positive lease retirement, default Registry or complete Delete is claimed.
func TestProjectExecutionRetirement(t *testing.T) {
	t.Run("queued-proof-upgrade-and-original-return", testExecutionRetirementReturn)
	t.Run("original-committed-unknown-readonly-confirmation", testExecutionRetirementUnknown)
	t.Run("registration-rollback-unknown-and-next-explicit-run", testExecutionRetirementRegistration)
}

type executionRetirementPGAssembly struct {
	base    *variableHTTPFixture
	fixture *executionRetirementPG
	upgrade bool
	work    *workCleanupPGAssembly
	runner  *runnerManagedPGAssembly
}

// Observe only the original Store results during this opt-in seed. Opaque
// port wrappers can hide an inner database error; this preserves its safe
// category before those wrappers, without logging identities, SQL or values.
func (*executionRetirementPGAssembly) diagnoseSeed(t *testing.T, store *hookStore) func() {
	t.Helper()
	type observation struct {
		stage  string
		result f.CommitResult
	}
	var mu sync.Mutex
	var observed []observation
	store.mu.Lock()
	original := store.afterResult
	store.afterResult = func(cause f.TransactionCause, result f.CommitResult) {
		if original != nil {
			original(cause, result)
		}
		stage := "execution-seed-other-store"
		d := cause.Details()
		if cause.Kind() == f.RecoveryCause {
			switch d.Owner {
			case "execution-call-register":
				stage = "execution-seed-service-register"
			case "execution-call-return":
				stage = "execution-seed-service-return"
			case "scheduler-call-register":
				stage = "execution-seed-scheduler-register"
			case "scheduler-call-return":
				stage = "execution-seed-scheduler-return"
			}
		} else if cause.Kind() == f.CommandsCause {
			switch d.Primary.Command() {
			case "launch":
				stage = "execution-seed-launch-store"
			case "launch_handoff":
				stage = "execution-seed-mark-launch"
			case "reject_launch":
				stage = "execution-seed-reject-launch"
			case "associate_launch":
				stage = "execution-seed-associate"
			}
		}
		mu.Lock()
		observed = append(observed, observation{stage, result})
		mu.Unlock()
	}
	store.mu.Unlock()
	return func() {
		store.mu.Lock()
		store.afterResult = original
		store.mu.Unlock()
		if !t.Failed() {
			return
		}
		mu.Lock()
		defer mu.Unlock()
		for n, value := range observed {
			t.Logf("EXECUTION_RETIREMENT_SEED_STORE index=%d stage=%s commit_state=%s", n, value.stage, value.result.State().Safe())
			if value.result.State() != f.Committed {
				agentRetirementDiagnostic(t, value.stage, value.result.Fault())
			}
		}
	}
}

func (a *executionRetirementPGAssembly) manifest(t *testing.T, original pc.RequiredManifest) pc.RequiredManifest {
	t.Helper()
	entries := original.Entries()
	entries = append(entries,
		pc.ParticipantRegistration{Name: pc.SchedulerParticipant, ContractVersion: 1, OwnerModule: "scheduler", ReferenceKinds: []pc.ReferenceKind{"scheduler-call"}},
		pc.ParticipantRegistration{Name: pc.ExecutionParticipant, ContractVersion: 1, OwnerModule: "execution", ReferenceKinds: []pc.ReferenceKind{"execution-call"}, CleanupAfter: []pc.ParticipantName{pc.SchedulerParticipant}},
	)
	for n := range entries {
		switch entries[n].Name {
		case pc.SkillsParticipant:
			entries[n].CleanupAfter = append(entries[n].CleanupAfter, pc.ExecutionParticipant)
		case pc.OutboxParticipant, pc.AuditParticipant:
			entries[n].CleanupAfter = append(entries[n].CleanupAfter, pc.SchedulerParticipant, pc.ExecutionParticipant)
		}
	}
	if a.work != nil {
		entries = a.work.manifest(entries)
	}
	if a.runner != nil {
		entries = a.runner.manifest(entries)
	}
	manifest, err := pc.NewRequiredManifest(entries)
	schedulerOwnedRequire(t, "retirement-complete-manifest", err)
	return manifest
}

type executionRetirementPG struct {
	*executionCallPG
	providers       assembly.Providers
	consumer        *execution.ProjectExecutionRetirement
	driver          *project.LifecycleCleanupDriver
	agents          *project.LifecycleCleanupDriver
	drivers         []*project.LifecycleCleanupDriver
	assignment      sc.AssignmentMutation
	receipt         string
	canonicalDigest string
	workBefore      string
	otherBefore     string
	started         bool
	settled         bool
}

// The original helpers supply all real dependencies. Only the new fixture
// opts into schema84 before the first owned Launch and uses this same Life
// for the initial Skill/Object graph, Scheduler, Execution and Agent cleanup.
func (a *executionRetirementPGAssembly) beforeLaunch(t *testing.T, v *executionCallPG, providers assembly.Providers, variables variableOwnershipDependencies) {
	t.Helper()
	r := a.fixture
	r.executionCallPG, r.providers = v, providers
	if a.upgrade {
		before := v.snapshot(t, true)
		for n := 0; n < 2; n++ {
			migrate(t, v.base.db, migrationPrefix(t, "00084"))
			if before != v.snapshot(t, true) {
				t.Fatal("migration84/repeat changed actual pre-Execution business facts")
			}
		}
	}
	var count int
	schedulerOwnedRequire(t, "retirement-before-first-owned-launch", v.base.raw.QueryRow(ctxFor(t), `SELECT count(*) FROM agenteam_execution.executions`).Scan(&count))
	if count != 0 {
		t.Fatal("retirement was bound after an Execution already existed")
	}
	owner := v.deps.Service.Authority
	tools, err := registry.NewProjectExecutionRetirementReferences(v.base.tracked, owner)
	schedulerOwnedRequire(t, "retirement-tool-owner", err)
	mounts, err := mount.NewProjectExecutionRetirementReferences(v.base.tracked, owner)
	schedulerOwnedRequire(t, "retirement-mount-owner", err)
	variableAuthority, err := pv.NewProjectExecutionRetirementAuthority(providers.Variables, owner)
	schedulerOwnedRequire(t, "retirement-variable-authority", err)
	leases, err := secret.NewProjectExecutionLeaseRetirement(variables.d04, variableAuthority)
	schedulerOwnedRequire(t, "retirement-secret-owner", err)
	variableRefs, err := pv.NewProjectExecutionRetirementReferences(variableAuthority, leases)
	schedulerOwnedRequire(t, "retirement-variable-owner", err)
	skills, err := skill.NewProjectExecutionRetirementReferences(v.base.tracked, owner)
	schedulerOwnedRequire(t, "retirement-skill-owner", err)
	gate, err := project.NewExecutionProjectRetirementAuthority(v.life, v.base.tracked)
	schedulerOwnedRequire(t, "retirement-project-authority", err)
	r.consumer, err = execution.NewProjectExecutionRetirement(v.owned, gate, tools, variableRefs, skills, mounts)
	schedulerOwnedRequire(t, "retirement-original-consumer", err)
	// The real Handoff already owns this transparent counter, but no Launch has
	// run. Its one Service is fixed here before any original business call.
	v.launcher.service = v.owned.Service()
}

func newExecutionRetirementPG(t *testing.T, upgrade bool) *executionRetirementPG {
	t.Helper()
	var base *variableHTTPFixture
	if upgrade {
		db := pgfixture.NewDatabase(t)
		migrate(t, db, migrationPrefix(t, "00083"))
		raw := openStore(t, db.Config(t, nil))
		base = assembleVariableHTTPFixture(t, db, raw, &hookStore{fixtureStore: raw})
	} else {
		base = newVariableHTTPFixture(t)
	}
	v := &executionRetirementPG{}
	option := &executionRetirementPGAssembly{base: base, fixture: v, upgrade: upgrade}
	v.executionCallPG = newExecutionCallPG(t, false, option)
	t.Cleanup(func() { v.closeRetirement(t) })
	v.assignQueued(t)
	if upgrade {
		v.immutableRetirementOrigin(t)
	}
	v.bindAgentCleanup(t)
	v.otherExecution(t)
	v.otherBefore = v.retirementOtherSnapshot(t)
	v.workBefore = v.retirementWorkSnapshot(t)
	v.accept(t)
	v.executionObserved(t, v.owned, false, pc.Stopped)
	v.observed(t, v.pair, false, pc.Stopped)
	v.enterRetirementCleanup(t)
	v.driver = v.newRetirementDriver(t)
	return v
}

func (v *executionRetirementPG) assignQueued(t *testing.T) {
	t.Helper()
	b, p, target := v.base, v.work.base.project.ID, v.work.agentID
	install, err := skill.NewInstallRequest(ctxFor(t), id[pc.Skill](t), skillInstallationPackage(t))
	schedulerOwnedRequire(t, "retirement-install-request", err)
	installed, err := v.skills.p2.service.Install(ctxFor(t), b.ownerBrowser.actor, meta(t, "execution-retirement-install", nil), p, install)
	schedulerOwnedRequire(t, "retirement-original-install", err)
	proof, err := skill.NewAssignmentAuditFacts(b.tracked)
	schedulerOwnedRequire(t, "retirement-assignment-facts", err)
	projects, err := project.NewAuthority(b.tracked, project.AuthorityDependencies{Sessions: b.accounts, Routes: b.accounts, Lifecycle: v.life, AuditFacts: map[au.Producer]au.ProjectFactAuthority{au.SkillProducer: proof}})
	schedulerOwnedRequire(t, "retirement-assignment-projects", err)
	owner, err := skill.NewAuthority(b.tracked, projects)
	schedulerOwnedRequire(t, "retirement-assignment-owner", err)
	bundle, err := skill.AddSkills(ctxFor(t))
	schedulerOwnedRequire(t, "retirement-assignment-bundle", err)
	service, err := skill.New(skill.Dependencies{Authority: owner, Objects: v.skills.p2.objects, Processes: v.skills.guard, ProcessID: v.skills.process, Bundle: bundle})
	schedulerOwnedRequire(t, "retirement-assignment-service", err)
	t.Cleanup(func() { schedulerOwnedDrain(t, service) })
	catalog := event.NewCatalog()
	types, err := sc.RegisterAssignmentEvents(catalog)
	schedulerOwnedRequire(t, "retirement-assignment-events", err)
	facts, err := skill.NewAssignmentAuthority(owner, types)
	schedulerOwnedRequire(t, "retirement-assignment-authority", err)
	ingress, err := execution.NewSkillAssignmentIngress(b.tracked, facts)
	schedulerOwnedRequire(t, "retirement-original-ingress", err)
	tools, err := registry.NewSkillAssignmentReferences(b.tracked, v.providers.Agents)
	schedulerOwnedRequire(t, "retirement-assignment-tool-refs", err)
	mounts, err := mount.NewSkillAssignmentReferences(b.tracked, v.providers.Agents)
	schedulerOwnedRequire(t, "retirement-assignment-mount-refs", err)
	models, err := model.NewSkillAssignmentReferences(b.tracked, v.providers.Agents)
	schedulerOwnedRequire(t, "retirement-assignment-model-refs", err)
	variables, err := pv.NewSkillAssignmentReferences(b.tracked, v.providers.Agents)
	schedulerOwnedRequire(t, "retirement-assignment-variable-refs", err)
	agents, err := agent.NewSkillAssignmentAuthority(v.providers.Agents, []ac.SkillAssignmentVersionReferences{tools, mounts, models, variables})
	schedulerOwnedRequire(t, "retirement-assignment-agent-refs", err)
	auditor, err := audit.New(b.tracked, b.keys, audit.Authorizations{Sessions: b.accounts, System: b.accounts, Accounts: b.accounts, Projects: projects})
	schedulerOwnedRequire(t, "retirement-assignment-audit", err)
	box, err := outbox.New(b.tracked, catalog, outbox.Authorizations{Producers: map[event.StableName]oc.ProducerAuthority{sc.AssignmentProducer: facts}, Sessions: b.accounts, System: b.accounts, Projects: projects, Audit: auditor, Cursors: b.keys, Processes: captureProviderProcesses{guard: v.skills.guard}})
	schedulerOwnedRequire(t, "retirement-assignment-outbox", err)
	commands, err := skill.NewAssignmentCommands(skill.AssignmentDependencies{Service: service, Agents: agents, CurrentAgents: v.providers.Agents, Runtime: ingress, Audit: auditor, Outbox: box, Events: types}, facts)
	schedulerOwnedRequire(t, "retirement-assignment-commands", err)
	version := f.Version(1)
	v.assignment, err = commands.AssignSkill(ctxFor(t), b.ownerBrowser.actor, meta(t, "execution-retirement-queued", &version), p, target, installed.SkillID, true)
	schedulerOwnedRequire(t, "retirement-formal-queued-assignment", err)
	if v.assignment.Validate() != nil || !v.assignment.Changed || v.assignment.AgentVersion != 2 || v.assignment.Runtime == nil || !v.assignment.Runtime.Queued || v.assignment.Runtime.ExecutionID == nil || *v.assignment.Runtime.ExecutionID != v.created.ID {
		t.Fatal("formal assignment did not queue against the original Created Execution")
	}
	var raw string
	schedulerOwnedRequire(t, "retirement-original-queued-receipt", b.raw.QueryRow(ctxFor(t), `SELECT encode(runtime_receipt,'hex') FROM agenteam_skill.assignment_commands WHERE id=$1::uuid AND project_id=$2::uuid AND agent_id=$3::uuid AND state='completed'`, v.assignment.ChangeID.String(), p.String(), target.String()).Scan(&raw))
	v.receipt = raw
	var canonical []byte
	schedulerOwnedRequire(t, "retirement-full-canonical-before", b.raw.QueryRow(ctxFor(t), `SELECT to_jsonb(e) FROM agenteam_execution.executions e WHERE id=$1::text`, v.created.ID.String()).Scan(&canonical))
	v.canonicalDigest = fmt.Sprintf("sha256:%x", sha256.Sum256(canonical))
	schedulerOwnedDrain(t, service)
	if !service.Joined() {
		t.Fatal("original assignment call did not return")
	}
	v.requireRetirementFacts(t, false)
}

func (v *executionRetirementPG) bindAgentCleanup(t *testing.T) {
	t.Helper()
	s, owner := v.base.tracked, v.providers.Agents
	workGate, err := work.NewProjectAgentRetirement(s, owner)
	schedulerOwnedRequire(t, "retirement-agent-work-gate", err)
	executionGate, err := execution.NewProjectAgentRetirement(s, owner)
	schedulerOwnedRequire(t, "retirement-agent-execution-gate", err)
	models, err := model.NewProjectAgentRetirementReferences(s, owner)
	schedulerOwnedRequire(t, "retirement-agent-models", err)
	tools, err := registry.NewProjectAgentRetirementReferences(s, owner)
	schedulerOwnedRequire(t, "retirement-agent-tools", err)
	mounts, err := mount.NewProjectAgentRetirementReferences(s, owner)
	schedulerOwnedRequire(t, "retirement-agent-mounts", err)
	variables, err := pv.NewProjectAgentRetirementReferences(s, owner)
	schedulerOwnedRequire(t, "retirement-agent-variables", err)
	ingress, err := execution.NewProjectAgentIngressRetirement(s, owner)
	schedulerOwnedRequire(t, "retirement-agent-no-active-ingress", err)
	skills, err := skill.NewProjectAgentRetirementReferencesWithIngress(s, owner, ingress)
	schedulerOwnedRequire(t, "retirement-agent-original-skill-proofs", err)
	consumer, err := agent.NewProjectRetirement(owner, v.skills.projects, agent.ProjectRetirementDependencies{Work: workGate, Execution: executionGate, Models: models, Tools: tools, Mounts: mounts, Secrets: variables, Skills: skills})
	schedulerOwnedRequire(t, "retirement-agent-consumer", err)
	v.agents, err = project.NewLifecycleCleanupDriverWithAgents(s, v.skills.projects, captureProviderProcesses{guard: v.skills.guard}, consumer, agentRetirementSkillsDiagnostic{SkillsCleanupDomain: v.skills.tap, t: t})
	schedulerOwnedRequire(t, "retirement-agent-original-driver", err)
	v.drivers = append(v.drivers, v.agents)
}

func (v *executionRetirementPG) enterRetirementCleanup(t *testing.T) {
	t.Helper()
	role, err := i.RegisterService(i.ProjectLifecycle)
	schedulerOwnedRequire(t, "retirement-stop-role", err)
	projectScope, err := i.InProject(v.scope.ProjectID)
	schedulerOwnedRequire(t, "retirement-stop-scope", err)
	actor, err := role.Actor(v.cause.OperationID.String(), projectScope)
	schedulerOwnedRequire(t, "retirement-stop-actor", err)
	report, err := v.skills.p2.service.RequestStop(ctxFor(t), actor, v.cause, v.scope)
	if err == nil && report.Details().State != pc.Stopped {
		report, err = v.skills.p2.service.InspectStop(ctxFor(t), actor, v.cause, v.scope)
	}
	schedulerOwnedRequire(t, "retirement-original-skill-stop", err)
	if report.Details().State != pc.Stopped {
		t.Fatal("original Skill owner is still pending")
	}
	op, err := f.ParseID[objc.ProjectStopOperation](v.cause.OperationID.String())
	schedulerOwnedRequire(t, "retirement-object-operation", err)
	cause, err := objc.NewProjectStopCause(objc.ProjectStopCauseDetails{ProjectID: v.scope.ProjectID, OperationID: op, Action: objc.ProjectStopDelete, ProjectVersion: v.cause.ProjectVersion})
	schedulerOwnedRequire(t, "retirement-object-stop-cause", err)
	native, err := v.skills.p2.objects.RequestProjectStop(ctxFor(t), actor, cause)
	for n := 0; err == nil && native.Details().State != objc.ProjectStopped && n < 16; n++ {
		native, err = v.skills.p2.objects.InspectProjectStop(ctxFor(t), actor, cause)
	}
	schedulerOwnedRequire(t, "retirement-original-object-stop", err)
	if !native.Matches(objc.ObjectStopComponent, cause) || native.Details().State != objc.ProjectStopped {
		t.Fatal("original Object owner is still pending")
	}
	lock, _ := f.ProjectLock(v.scope.ProjectID.String())
	// Other still-unbound participant Stop facts and the phase handoff are
	// controlled prerequisites. No unavailable Registry port is invoked.
	v.base.tx(t, []f.LockRequest{{Key: lock, Mode: f.Exclusive}}, func(ctx context.Context, _ f.Tx, x postgres.SQLExecutor) error {
		if _, err := x.Exec(ctx, `UPDATE agenteam_project.lifecycle_participants SET stop_state='stopped',version=version+1 WHERE operation_id=$1::uuid`, v.cause.OperationID.String()); err != nil {
			return err
		}
		tag, err := x.Exec(ctx, `UPDATE agenteam_project.lifecycle_operations SET state='cleaning',cleanup_stage='domains',version=version+1,updated_at=clock_timestamp() WHERE id=$1::uuid AND project_id=$2::uuid AND state='stopping'`, v.cause.OperationID.String(), v.scope.ProjectID.String())
		if err == nil && tag.RowsAffected() != 1 {
			return errors.New("original stopping handoff changed")
		}
		return err
	})
	gate, err := project.NewSchedulerProjectCleanupAuthority(v.life, v.base.tracked)
	schedulerOwnedRequire(t, "retirement-scheduler-cleanup-gate", err)
	participant, err := scheduler.NewProjectCleanupParticipant(v.pair, gate)
	schedulerOwnedRequire(t, "retirement-original-scheduler-cleaner", err)
	driver, err := project.NewLifecycleCleanupDriverForScheduler(v.base.tracked, v.life, captureProviderProcesses{guard: v.skills.guard}, participant)
	schedulerOwnedRequire(t, "retirement-original-scheduler-driver", err)
	v.drivers = append(v.drivers, driver)
	for n := 0; n < 4; n++ {
		call := v.start(func(ctx context.Context) error { return driver.Run(ctx, v.scope.ProjectID, v.cause.OperationID) })
		outboxCleanupWait(t, call.done, "original Scheduler cleanup before Execution retirement")
		schedulerOwnedRequire(t, "retirement-scheduler-cleanup-return", call.err)
		var complete bool
		schedulerOwnedRequire(t, "retirement-scheduler-completed", v.base.raw.QueryRow(ctxFor(t), `SELECT cleanup_state='completed' FROM agenteam_project.lifecycle_participants WHERE operation_id=$1::uuid AND participant_name='scheduler'`, v.cause.OperationID.String()).Scan(&complete))
		if complete {
			return
		}
	}
	t.Fatal("original Scheduler batches did not complete before Execution retirement")
}

func (v *executionRetirementPG) newRetirementDriver(t *testing.T) *project.LifecycleCleanupDriver {
	t.Helper()
	d, err := project.NewLifecycleCleanupDriverForExecutions(v.base.tracked, v.life, captureProviderProcesses{guard: v.skills.guard}, v.consumer)
	schedulerOwnedRequire(t, "retirement-fixed-driver", err)
	v.drivers = append(v.drivers, d)
	return d
}

func (v *executionRetirementPG) requireRetirementFacts(t *testing.T, retired bool) {
	t.Helper()
	var counts [8]int
	schedulerOwnedRequire(t, "retirement-exact-facts", v.base.raw.QueryRow(ctxFor(t), `SELECT
 (SELECT count(*) FROM agenteam_execution.executions WHERE id=$1::text),
 (SELECT count(*) FROM agenteam_skill.assignment_pending_refs WHERE execution_id=$1::text::uuid),
 (SELECT count(*) FROM agenteam_execution.skill_assignment_ingress WHERE execution_id=$1::text),
 (SELECT count(*) FROM agenteam_skill.assignment_execution_retirement_proofs WHERE execution_id=$1::text),
 (SELECT count(*) FROM agenteam_tool.execution_configurations WHERE execution_id=$1::text),
 (SELECT count(*) FROM agenteam_projectvariable.execution_environments WHERE execution_id=$1::text),
 (SELECT count(*) FROM agenteam_secret.project_variable_execution_leases WHERE execution_id=$1::text),
 (SELECT count(*) FROM agenteam_mount.execution_captures WHERE execution_id=$1::text)`, v.created.ID.String()).Scan(&counts[0], &counts[1], &counts[2], &counts[3], &counts[4], &counts[5], &counts[6], &counts[7]))
	want := [8]int{1, 1, 1, 0, 0, 0, 0, 0}
	if retired {
		want = [8]int{0, 0, 0, 1, 0, 0, 0, 0}
	}
	if counts != want {
		t.Fatal("original Execution/queued reference shape differs", counts)
	}
	var receipt string
	schedulerOwnedRequire(t, "retirement-keeps-queued-receipt", v.base.raw.QueryRow(ctxFor(t), `SELECT encode(runtime_receipt,'hex') FROM agenteam_skill.assignment_commands WHERE id=$1::uuid AND state='completed'`, v.assignment.ChangeID.String()).Scan(&receipt))
	if receipt != v.receipt {
		t.Fatal("retirement rewrote the immutable queued receipt")
	}
	if retired {
		var exact bool
		schedulerOwnedRequire(t, "retirement-original-applied-proof", v.base.raw.QueryRow(ctxFor(t), `SELECT p.project_id=$2::text::uuid AND p.agent_id=$3::text::uuid AND p.execution_id=$4::text AND p.execution_version=1 AND p.canonical_digest=$5 AND p.operation_id=$6::text::uuid AND p.project_version=$7 AND p.claim_process=$8::text::uuid AND p.proof_digest='sha256:'||encode(sha256(p.proof_data),'hex') AND convert_from(p.proof_data,'UTF8')::jsonb->'runtime_receipt'=convert_from(c.runtime_receipt,'UTF8')::jsonb FROM agenteam_skill.assignment_execution_retirement_proofs p JOIN agenteam_skill.assignment_commands c ON c.id=p.change_id WHERE p.change_id=$1::uuid`, v.assignment.ChangeID.String(), v.scope.ProjectID.String(), v.work.agentID.String(), v.created.ID.String(), v.canonicalDigest, v.cause.OperationID.String(), int64(v.cause.ProjectVersion), v.skills.process.String()).Scan(&exact))
		if !exact {
			t.Fatal("Skill Applied proof lost its original complete binding")
		}
	}
}

type executionRetirementCheckpointPG struct {
	Stage   string                                `json:"stage"`
	Binding *ec.ProjectExecutionRetirementBinding `json:"binding"`
	Commit  ec.ProjectExecutionRetirementCommit   `json:"commit"`
	Owner   ec.ProjectExecutionRetirementIdentity `json:"owner"`
}

func (v *executionRetirementPG) progress(t *testing.T, stage, state, claim string) executionRetirementCheckpointPG {
	t.Helper()
	var actualStage, actualState, actualClaim, operation string
	var schema int64
	var raw []byte
	schedulerOwnedRequire(t, "retirement-original-progress", v.base.raw.QueryRow(ctxFor(t), `SELECT o.state,o.cleanup_stage,r.cleanup_state,w.phase,coalesce(r.checkpoint_schema,0),r.checkpoint FROM agenteam_project.lifecycle_operations o JOIN agenteam_project.lifecycle_participants r ON r.operation_id=o.id AND r.participant_name='execution' JOIN agenteam_project.work_claims w ON w.work_kind='lifecycle' AND w.work_id=o.id WHERE o.id=$1::uuid AND o.project_id=$2::uuid`, v.cause.OperationID.String(), v.scope.ProjectID.String()).Scan(&operation, &actualStage, &actualState, &actualClaim, &schema, &raw))
	if operation != "cleaning" || actualStage != "domains" || actualState != state || actualClaim != claim {
		t.Fatal("Execution retirement advanced or changed original Project progress")
	}
	var cp executionRetirementCheckpointPG
	if stage == "" {
		if schema != 0 || len(raw) != 0 {
			t.Fatal("unapplied retirement wrote a checkpoint")
		}
		return cp
	}
	if schema != 1 || json.Unmarshal(raw, &cp) != nil || cp.Stage != stage || cp.Owner != v.consumer.ProjectExecutionRetirementIdentity() || cp.Commit.ProjectID != v.scope.ProjectID || cp.Commit.OperationID != v.cause.OperationID || cp.Commit.ProjectVersion != v.cause.ProjectVersion || cp.Commit.Validate() != nil {
		t.Fatal("original retirement checkpoint identity changed")
	}
	if stage == "applied" && (cp.Binding == nil || cp.Binding.ExecutionID != v.created.ID || cp.Binding.AgentID != v.work.agentID || cp.Binding.ProjectID != v.scope.ProjectID || cp.Binding.Version != 1 || cp.Binding.CanonicalDigest.String() != v.canonicalDigest) {
		t.Fatal("checkpoint lost the complete original Before")
	}
	if stage == "empty" && cp.Binding != nil {
		t.Fatal("empty checkpoint retained a fabricated Execution")
	}
	return cp
}

func (v *executionRetirementPG) retirementWorkSnapshot(t *testing.T) string {
	t.Helper()
	var parts []string
	for _, table := range []string{"milestones", "sprints", "milestone_order_groups", "sprint_order_groups", "structure_commands", "tasks", "task_order_groups", "task_query_generations", "task_commands", "task_events", "task_transition_commands", "task_blockers", "task_blocker_commands", "sprint_start_commands", "task_scheduler_claims", "task_scheduler_relaunches", "task_busy_compensations", "task_launch_failures"} {
		parts = append(parts, fmt.Sprintf("(SELECT jsonb_agg(to_jsonb(x) ORDER BY to_jsonb(x)::text) FROM agenteam_work.%s x WHERE project_id=$1::uuid)", table))
	}
	parts = append(parts, `(SELECT jsonb_agg(to_jsonb(x) ORDER BY id) FROM agenteam_outbox.events x WHERE project_id=$1::uuid AND producer='work')`)
	var raw string
	schedulerOwnedRequire(t, "retirement-preserved-work", v.base.raw.QueryRow(ctxFor(t), "SELECT jsonb_build_array("+strings.Join(parts, ",")+")::text", v.work.base.project.ID.String()).Scan(&raw))
	return raw
}

func (v *executionRetirementPG) retirementOtherSnapshot(t *testing.T) string {
	t.Helper()
	var raw string
	schedulerOwnedRequire(t, "retirement-other-project", v.base.raw.QueryRow(ctxFor(t), `SELECT jsonb_build_array((SELECT to_jsonb(p) FROM agenteam_project.projects p WHERE id=$1::text::uuid),(SELECT jsonb_agg(to_jsonb(c) ORDER BY id) FROM agenteam_execution.project_calls c WHERE project_id=$1::text::uuid),(SELECT jsonb_agg(to_jsonb(e) ORDER BY id) FROM agenteam_execution.executions e WHERE project_id=$1::text))::text`, v.base.project.ID.String()).Scan(&raw))
	return raw
}

func (v *executionRetirementPG) otherUsable(t *testing.T) {
	t.Helper()
	if v.otherBefore != v.retirementOtherSnapshot(t) {
		t.Fatal("A retirement changed B")
	}
	v.otherExecution(t)
	v.otherBefore = v.retirementOtherSnapshot(t)
}

func (v *executionRetirementPG) runRetirement(driver *project.LifecycleCleanupDriver) *skillsCleanupPGCall {
	v.started = true
	return v.start(func(ctx context.Context) error { return driver.Run(ctx, v.scope.ProjectID, v.cause.OperationID) })
}

func (v *executionRetirementPG) round(t *testing.T) {
	t.Helper()
	call := v.runRetirement(v.driver)
	outboxCleanupWait(t, call.done, "original Execution retirement round")
	schedulerOwnedRequire(t, "retirement-round-return", call.err)
}

// Fixed identifiers only. Include the complete target row sets even after the
// canonical Execution has gone; joining through it would hide orphaned facts.
func (v *executionRetirementPG) retirementSnapshot(t *testing.T) string {
	t.Helper()
	parts := []string{`(SELECT to_jsonb(x) FROM agenteam_execution.executions x WHERE id=$1::text)`}
	for _, table := range []string{"transcript_entries", "rounds", "snapshots", "preparation_inputs", "preparation_claims", "preparation_attempts", "skill_assignment_ingress", "tool_call_arguments", "tool_calls", "tool_result_consumptions", "round_inputs", "skill_assignment_dispositions", "control_inputs", "round_model_requests", "round_model_claims"} {
		parts = append(parts, fmt.Sprintf(`(SELECT jsonb_agg(to_jsonb(x) ORDER BY to_jsonb(x)::text) FROM agenteam_execution.%s x WHERE execution_id=$1::text)`, table))
	}
	for _, table := range []string{"agenteam_tool.execution_configurations", "agenteam_tool.execution_references", "agenteam_projectvariable.execution_environments", "agenteam_projectvariable.execution_secret_references", "agenteam_secret.project_variable_execution_leases", "agenteam_mount.execution_captures", "agenteam_mount.execution_references", "agenteam_mount.execution_uses", "agenteam_mount.execution_use_retirements", "agenteam_skill.assignment_execution_retirement_proofs"} {
		parts = append(parts, fmt.Sprintf(`(SELECT jsonb_agg(to_jsonb(x) ORDER BY to_jsonb(x)::text) FROM %s x WHERE execution_id=$1::text)`, table))
	}
	for _, table := range []string{"execution_binding_heads", "execution_bindings", "assignment_pending_refs"} {
		parts = append(parts, fmt.Sprintf(`(SELECT jsonb_agg(to_jsonb(x) ORDER BY to_jsonb(x)::text) FROM agenteam_skill.%s x WHERE execution_id=$1::text::uuid)`, table))
	}
	parts = append(parts,
		`(SELECT jsonb_agg(to_jsonb(x) ORDER BY id) FROM agenteam_execution.project_calls x WHERE project_id=$2::text::uuid)`,
		`(SELECT jsonb_agg(to_jsonb(x) ORDER BY id) FROM agenteam_skill.assignment_commands x WHERE project_id=$2::text::uuid)`,
		`(SELECT jsonb_agg(to_jsonb(x) ORDER BY to_jsonb(x)::text) FROM agenteam_skill.agent_assignment_heads x WHERE project_id=$2::text::uuid)`,
		`(SELECT to_jsonb(x) FROM agenteam_project.lifecycle_operations x WHERE id=$3::text::uuid)`,
		`(SELECT jsonb_agg(to_jsonb(x) ORDER BY participant_name) FROM agenteam_project.lifecycle_participants x WHERE operation_id=$3::text::uuid)`,
		`(SELECT to_jsonb(x) FROM agenteam_project.work_claims x WHERE work_kind='lifecycle' AND work_id=$3::text::uuid)`,
	)
	var raw string
	schedulerOwnedRequire(t, "retirement-complete-snapshot", v.base.raw.QueryRow(ctxFor(t), "SELECT jsonb_build_array("+strings.Join(parts, ",")+")::text", v.created.ID.String(), v.scope.ProjectID.String(), v.cause.OperationID.String()).Scan(&raw))
	return raw
}

func (v *executionRetirementPG) retirementCause(cause f.TransactionCause) bool {
	d := cause.Details()
	return cause.Kind() == f.JobCause && d.JobType == "project-lifecycle" && d.JobID == v.cause.OperationID.String()
}

func (v *executionRetirementPG) immutableRetirementOrigin(t *testing.T) {
	t.Helper()
	before := v.executionSnapshot(t, true)
	var origins int
	schedulerOwnedRequire(t, "retirement-original-launch-result", v.base.raw.QueryRow(ctxFor(t), `SELECT count(*) FROM agenteam_execution.project_calls WHERE project_id=$1::uuid AND original_result IS NOT NULL AND phase='business_closed'`, v.work.base.project.ID.String()).Scan(&origins))
	if origins != 1 {
		t.Fatal("first owned Launch did not preserve its original result")
	}
	lock, _ := f.ProjectLock(v.work.base.project.ID.String())
	for _, statement := range []string{
		`UPDATE agenteam_execution.project_calls SET original_result=NULL WHERE project_id=$1::uuid AND original_result IS NOT NULL`,
		`DELETE FROM agenteam_execution.project_calls WHERE project_id=$1::uuid`,
	} {
		cause, _ := f.NewRecoveryCause("execution-retirement-pg-immutable", id[struct{}](t).String(), v.work.base.project.ID.String())
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
			t.Fatal("ordinary origin mutation bypassed immutable history")
		}
	}
}

func (v *executionRetirementPG) busyRetirement(t *testing.T, original *skillsCleanupPGCall) {
	t.Helper()
	if original != nil {
		select {
		case <-original.done:
			t.Fatal("original retirement returned before its Store")
		default:
		}
	}
	driver := v.newRetirementDriver(t)
	call := v.runRetirement(driver)
	outboxCleanupWait(t, call.done, "same-Life second Execution retirement driver")
	if !schedulerOwnedCode(call.err, f.ResourceBusy) {
		agentRetirementDiagnostic(t, "retirement-second-driver", call.err)
		t.Fatal("another driver crossed the original retained retirement round")
	}
	v.otherUsable(t)
}

// Every Project round uses the same Job cause for its read and registration
// transactions. Select the mutation by its newly created proof's current xid,
// exact claim and cp, never by cause alone or by an absent canonical row.
func (v *executionRetirementPG) holdRetirement(t *testing.T, mask bool) (*skillsCleanupPGCall, *schedulerStopBarrier, *f.CommitResult) {
	t.Helper()
	s, barrier := v.base.tracked, v.barrier()
	var selected, wrapped atomic.Bool
	var original f.CommitResult
	attempt := id[f.TransactionAttempt](t)
	s.setAfter(func(ctx context.Context, tx f.Tx, cause f.TransactionCause) error {
		if !v.retirementCause(cause) {
			return nil
		}
		x, err := s.InTx(tx)
		if err != nil {
			return err
		}
		var exact bool
		err = x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_skill.assignment_execution_retirement_proofs p JOIN agenteam_project.work_claims w ON w.work_kind='lifecycle' AND w.work_id=p.operation_id JOIN agenteam_project.lifecycle_participants r ON r.operation_id=p.operation_id AND r.participant_name='execution' WHERE p.change_id=$1::uuid AND p.execution_id=$2::text AND p.transaction_id=pg_current_xact_id()::text AND p.claim_process=w.process_id AND p.claim_attempt=w.attempt_id AND p.fence=w.fence AND w.phase='terminal' AND p.claim_attempt::text=$3 AND r.cleanup_state='pending' AND r.checkpoint_schema=1 AND convert_from(r.checkpoint,'UTF8')::jsonb->>'stage'='applied' AND convert_from(r.checkpoint,'UTF8')::jsonb->'commit'->>'transaction_id'=p.transaction_id AND convert_from(r.checkpoint,'UTF8')::jsonb->'commit'->>'attempt_id'=p.claim_attempt::text AND NOT EXISTS(SELECT 1 FROM agenteam_execution.executions WHERE id=p.execution_id))`, v.assignment.ChangeID.String(), v.created.ID.String(), cause.Details().JobAttemptID).Scan(&exact)
		if err == nil && exact {
			selected.Store(true)
		}
		return err
	})
	s.mu.Lock()
	s.afterResult = func(cause f.TransactionCause, result f.CommitResult) {
		if v.retirementCause(cause) && selected.Load() && result.State() == f.Committed {
			barrier.hold(result)
		}
	}
	if mask {
		s.transformResult = func(cause f.TransactionCause, result f.CommitResult) f.CommitResult {
			if v.retirementCause(cause) && selected.Load() && result.State() == f.Committed && wrapped.CompareAndSwap(false, true) {
				original = f.UnknownResult(attempt, cause)
				return original
			}
			return result
		}
	}
	s.mu.Unlock()
	return v.runRetirement(v.driver), barrier, &original
}

func (v *executionRetirementPG) unknownRetirement(t *testing.T, err error, original f.CommitResult) {
	t.Helper()
	actual, ok := project.UnknownAttempt(err)
	if !ok || original.State() != f.Unknown || actual.AttemptID() != original.AttemptID() || !schedulerOwnedSameCause(actual.Cause(), original.Cause()) || !v.retirementCause(actual.Cause()) {
		agentRetirementDiagnostic(t, "retirement-original-unknown", err)
		t.Fatal("retirement lost the original acknowledgement attempt/cause")
	}
}

func (v *executionRetirementPG) confirmRetirement(t *testing.T, original f.TransactionCause) {
	t.Helper()
	v.clear()
	stable := v.retirementSnapshot(t)
	s, barrier := v.base.tracked, v.barrier()
	var baseline sync.Map
	var seen atomic.Int64
	s.mu.Lock()
	s.before = func(ctx context.Context, tx f.Tx, cause f.TransactionCause) error {
		if !v.retirementCause(cause) {
			return nil
		}
		if !schedulerOwnedSameCause(cause, original) {
			return errors.New("retirement confirmation replaced the original cause")
		}
		x, err := s.InTx(tx)
		if err != nil {
			return err
		}
		var writes int64
		if err = x.QueryRow(ctx, schedulerCleanupWritesSQL).Scan(&writes); err != nil {
			return err
		}
		baseline.Store(tx, writes)
		return nil
	}
	s.after = func(ctx context.Context, tx f.Tx, cause f.TransactionCause) error {
		if !v.retirementCause(cause) {
			return nil
		}
		before, ok := baseline.LoadAndDelete(tx)
		if !ok {
			return errors.New("retirement confirmation lost its original Tx baseline")
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
			return errors.New("retirement confirmation performed a write")
		}
		seen.Add(1)
		return nil
	}
	s.afterResult = func(cause f.TransactionCause, result f.CommitResult) {
		if schedulerOwnedSameCause(cause, original) && seen.Load() == 1 && result.State() == f.Committed {
			barrier.hold(result)
		}
	}
	s.mu.Unlock()
	call := v.runRetirement(v.driver)
	v.await(t, call, barrier)
	if seen.Load() != 1 || stable != v.retirementSnapshot(t) {
		t.Fatal("original confirmation changed complete retirement facts")
	}
	v.busyRetirement(t, call)
	barrier.open()
	outboxCleanupWait(t, call.done, "original Execution retirement read-only confirmation")
	schedulerOwnedRequire(t, "retirement-confirmation-return", call.err)
	v.clear()
	if seen.Load() != 1 || stable != v.retirementSnapshot(t) {
		t.Fatal("confirmation wrote or advanced before the next explicit Run")
	}
}

func (v *executionRetirementPG) finishRetirement(t *testing.T) {
	t.Helper()
	v.requireRetirementFacts(t, true)
	v.round(t)
	v.progress(t, "empty", "completed", "terminal")
	v.settled = true // Original empty-tail Run has actually returned above.
	var calls int
	schedulerOwnedRequire(t, "retirement-original-call-ledger-empty", v.base.raw.QueryRow(ctxFor(t), `SELECT count(*) FROM agenteam_execution.project_calls WHERE project_id=$1::uuid`, v.scope.ProjectID.String()).Scan(&calls))
	if calls != 0 {
		t.Fatal("Execution completed with its original call history retained")
	}
	// The queued receipt stays intact through the complete Execution tail.
	v.requireRetirementFacts(t, true)
	call := v.start(func(ctx context.Context) error { return v.agents.Run(ctx, v.scope.ProjectID, v.cause.OperationID) })
	outboxCleanupWait(t, call.done, "original Agent Applied and Skill proof consumption")
	schedulerOwnedRequire(t, "retirement-agent-applied-return", call.err)
	var remaining [3]int
	schedulerOwnedRequire(t, "retirement-proof-consumed-with-agent", v.base.raw.QueryRow(ctxFor(t), `SELECT (SELECT count(*) FROM agenteam_agent.agents WHERE id=$1::uuid),(SELECT count(*) FROM agenteam_skill.assignment_commands WHERE agent_id=$1::uuid),(SELECT count(*) FROM agenteam_skill.assignment_execution_retirement_proofs WHERE agent_id=$1::uuid)`, v.work.agentID.String()).Scan(&remaining[0], &remaining[1], &remaining[2]))
	if remaining != [3]int{} {
		t.Fatal("Agent Applied did not consume the original queued command/proof", remaining)
	}
	var done bool
	// Same bounded child cleanup scope as the already-tested Agent/Skills
	// fixture: one explicit Run per provider batch, no implicit retry loop.
	for round := 0; round < 24; round++ {
		call = v.start(func(ctx context.Context) error { return v.agents.Run(ctx, v.scope.ProjectID, v.cause.OperationID) })
		outboxCleanupWait(t, call.done, "original Agent confirmation and Skills child cleanup")
		schedulerOwnedRequire(t, "retirement-skills-child-return", call.err)
		var schema int64
		var raw []byte
		schedulerOwnedRequire(t, "retirement-skills-child-checkpoint", v.base.raw.QueryRow(ctxFor(t), `SELECT checkpoint_schema,checkpoint FROM agenteam_project.lifecycle_participants WHERE operation_id=$1::uuid AND participant_name='agent-skills-variables' AND cleanup_state='pending'`, v.cause.OperationID.String()).Scan(&schema, &raw))
		var cp struct {
			AgentsDone bool `json:"agents_done"`
			Skills     struct {
				State string `json:"skills_state"`
			} `json:"skills_progress"`
		}
		if schema != 4 || json.Unmarshal(raw, &cp) != nil || !cp.AgentsDone {
			t.Fatal("Agent confirmation lost its exact full Before anchor")
		}
		if cp.Skills.State == "completed" {
			done = true
			break
		}
		if cp.Skills.State != "pending" {
			t.Fatal("unexpected real Skills child state")
		}
	}
	if !done || v.skills.tap.calls.Load() == 0 {
		t.Fatal("original Skills provider did not complete")
	}
	v.progress(t, "empty", "completed", "terminal")
	if v.workBefore != v.retirementWorkSnapshot(t) {
		t.Fatal("Execution/Agent retirement changed retained Work history")
	}
	v.otherUsable(t)
	v.noInvoke(t)
}

func testExecutionRetirementReturn(t *testing.T) {
	v := newExecutionRetirementPG(t, true)
	call, barrier, _ := v.holdRetirement(t, false)
	v.await(t, call, barrier)
	v.requireRetirementFacts(t, true)
	v.progress(t, "applied", "pending", "terminal")
	v.busyRetirement(t, call)
	barrier.open()
	outboxCleanupWait(t, call.done, "original atomic Execution/refs/proof/checkpoint Store return")
	schedulerOwnedRequire(t, "retirement-atomic-return", call.err)
	v.clear()
	// A failed next registration must preserve the already accepted anchor.
	// This is a real callback rollback, followed by a successful explicit Run.
	stable := v.retirementSnapshot(t)
	sentinel := errors.New("controlled next retirement registration rollback")
	var rejected atomic.Bool
	s := v.base.tracked
	s.setAfter(func(ctx context.Context, tx f.Tx, cause f.TransactionCause) error {
		if !v.retirementCause(cause) {
			return nil
		}
		x, err := s.InTx(tx)
		if err != nil {
			return err
		}
		var exact bool
		err = x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_project.work_claims w JOIN agenteam_project.lifecycle_participants r ON r.operation_id=w.work_id AND r.participant_name='execution' WHERE w.work_kind='lifecycle' AND w.work_id=$1::uuid AND w.phase='running' AND w.attempt_id::text=$2 AND r.attempt_count=2 AND convert_from(r.checkpoint,'UTF8')::jsonb->>'stage'='applied')`, v.cause.OperationID.String(), cause.Details().JobAttemptID).Scan(&exact)
		if err == nil && exact {
			rejected.Store(true)
			return sentinel
		}
		return err
	})
	call = v.runRetirement(v.driver)
	outboxCleanupWait(t, call.done, "original next registration known rollback")
	v.clear()
	if !rejected.Load() || !errors.Is(call.err, sentinel) || stable != v.retirementSnapshot(t) {
		t.Fatal("known registration rollback lost the accepted original checkpoint")
	}
	v.finishRetirement(t)
	t.Log("PROJECT_EXECUTION_RETIREMENT_RETURN migration83_84_repeat=true original_launch_provenance=true formal_queued_receipt_unchanged=true same_tx_execution_refs_proof_checkpoint=true original_store_return=true second_driver_busy=true accepted_anchor_survives_known_registration_rollback=true agent_applied_consumes_proof=true work_other_project_preserved=true no_invoke_no_full_delete=true")
}

func testExecutionRetirementUnknown(t *testing.T) {
	v := newExecutionRetirementPG(t, false)
	call, barrier, original := v.holdRetirement(t, true)
	v.await(t, call, barrier)
	v.requireRetirementFacts(t, true)
	v.busyRetirement(t, call)
	barrier.open()
	outboxCleanupWait(t, call.done, "original committed retirement acknowledgement mask")
	v.clear()
	v.unknownRetirement(t, call.err, *original)
	v.progress(t, "applied", "pending", "terminal")
	v.confirmRetirement(t, original.Cause())
	v.requireRetirementFacts(t, true)
	v.finishRetirement(t)
	t.Log("PROJECT_EXECUTION_RETIREMENT_UNKNOWN local_committed_ack_mask=true original_attempt_cause=true same_tx_zero_writes=true full_snapshot_unchanged=true confirmation_actual_return=true next_explicit_empty_tail=true queued_proof_then_agent_consumption=true no_network_lost_ack=true")
}

func testExecutionRetirementRegistration(t *testing.T) {
	v := newExecutionRetirementPG(t, false)
	stable := v.retirementSnapshot(t)
	s, barrier := v.base.tracked, v.barrier()
	var selected, wrapped atomic.Bool
	var original f.CommitResult
	sentinel := errors.New("controlled Execution retirement registration rollback")
	attempt := id[f.TransactionAttempt](t)
	s.setAfter(func(ctx context.Context, tx f.Tx, cause f.TransactionCause) error {
		if !v.retirementCause(cause) {
			return nil
		}
		x, err := s.InTx(tx)
		if err != nil {
			return err
		}
		var exact bool
		err = x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_project.work_claims w JOIN agenteam_project.lifecycle_participants r ON r.operation_id=w.work_id AND r.participant_name='execution' WHERE w.work_kind='lifecycle' AND w.work_id=$1::uuid AND w.project_id=$2::uuid AND w.phase='running' AND w.attempt_id::text=$3 AND r.attempt_count=1 AND r.checkpoint IS NULL AND r.cleanup_state='required')`, v.cause.OperationID.String(), v.scope.ProjectID.String(), cause.Details().JobAttemptID).Scan(&exact)
		if err != nil {
			return err
		}
		if exact {
			selected.Store(true)
			return sentinel
		}
		return nil
	})
	s.mu.Lock()
	s.afterResult = func(cause f.TransactionCause, result f.CommitResult) {
		if v.retirementCause(cause) && selected.Load() && result.State() == f.NotCommitted && errors.Is(result.Fault(), sentinel) {
			barrier.hold(result)
		}
	}
	s.transformResult = func(cause f.TransactionCause, result f.CommitResult) f.CommitResult {
		if v.retirementCause(cause) && selected.Load() && result.State() == f.NotCommitted && errors.Is(result.Fault(), sentinel) && wrapped.CompareAndSwap(false, true) {
			original = f.UnknownResult(attempt, cause)
			return original
		}
		return result
	}
	s.mu.Unlock()
	call := v.runRetirement(v.driver)
	select {
	case result := <-barrier.entered:
		if result.State() != f.NotCommitted || !errors.Is(result.Fault(), sentinel) {
			t.Fatal("original registration did not actually roll back")
		}
	case <-call.done:
		schedulerOwnedRequire(t, "retirement-before-registration-return", call.err)
		t.Fatal("original registration rollback return not held")
	case <-time.After(5 * time.Second):
		t.Fatal("original registration return barrier not reached")
	}
	if stable != v.retirementSnapshot(t) {
		t.Fatal("registration rollback changed original facts")
	}
	v.busyRetirement(t, call)
	barrier.open()
	outboxCleanupWait(t, call.done, "original rolled-back retirement registration acknowledgement")
	v.clear()
	v.unknownRetirement(t, call.err, original)
	v.progress(t, "", "required", "terminal")
	v.confirmRetirement(t, original.Cause())
	v.requireRetirementFacts(t, false)
	v.busyRetirement(t, nil)
	v.round(t)
	v.progress(t, "applied", "pending", "terminal")
	v.finishRetirement(t)
	t.Log("PROJECT_EXECUTION_RETIREMENT_REGISTRATION actual_registration_rollback=true local_ack_unknown=true original_cause_attempt=true original_snapshot_preserved=true confirmation_same_tx_zero_writes=true held_confirmation_second_driver_busy=true next_explicit_run_applies=true proof_consumed_by_agent=true no_default_registry=true")
}

func (v *executionRetirementPG) closeRetirement(t *testing.T) {
	t.Helper()
	for _, b := range v.barriers {
		b.open()
	}
	for _, call := range v.calls {
		outboxCleanupWait(t, call.done, "original retirement call before owner shutdown")
	}
	v.clear()
	// Even an accepted Pending checkpoint belongs to the original driver.
	// Resolve its one possible local mask and finish the empty tail before any
	// global Stop; never erase an owner or infer return from closed SQL rows.
	if v.started && !v.settled && v.driver != nil {
		for n := 0; n < 4; n++ {
			call := v.runRetirement(v.driver)
			outboxCleanupWait(t, call.done, "original retirement recovery before shutdown")
			if call.err != nil {
				t.Error("original retirement cleanup failed", call.err)
				break
			}
			var complete bool
			err := v.base.raw.QueryRow(ctxFor(t), `SELECT cleanup_state='completed' FROM agenteam_project.lifecycle_participants WHERE operation_id=$1::uuid AND participant_name='execution'`, v.cause.OperationID.String()).Scan(&complete)
			if err != nil {
				t.Error("original retirement cleanup progress unavailable", err)
				break
			}
			if complete {
				v.settled = true
				break
			}
		}
	}
	for _, driver := range v.drivers {
		schedulerOwnedDrain(t, driver)
	}
}
