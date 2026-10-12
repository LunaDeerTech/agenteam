//go:build integration

package projectvariable_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/agent"
	ac "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/object"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/project"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/skill"
	"github.com/LunaDeerTech/agenteam/internal/central/tool/builtin"
	"github.com/LunaDeerTech/agenteam/internal/central/tool/registry"
	toolruntime "github.com/LunaDeerTech/agenteam/internal/central/tool/runtime"
	assembly "github.com/LunaDeerTech/agenteam/tests/testsupport/agentconfiguration"
	"github.com/minio/minio-go/v7"
)

// A real Create publishes AddSkills; a real Human Install publishes the other
// package. Only accepted Delete/the remaining participants' stop prerequisites
// are controlled by the inherited fixture. Skills/Object stop and each cleanup
// call are real. Skills completion never completes agent-skills-variables or
// moves domains to Object, Outbox, Audit or a Project finalizer.
func TestProjectSkillsCleanup(t *testing.T) {
	t.Run("real-packages-and-original-return", testProjectSkillsCleanupReturn)
	t.Run("disabled-agent-retains-initialization", testProjectSkillsCleanupDisabledAgent)
}

type skillsCleanupPGTap struct {
	service *skill.Service
	calls   atomic.Int64
}

func (p *skillsCleanupPGTap) Cleanup(ctx context.Context, actor i.Actor, cause pc.LifecycleCause, scope pc.ScopeRef, checkpoint *pc.CleanupCheckpoint) (pc.CleanupReport, error) {
	p.calls.Add(1)
	return p.service.Cleanup(ctx, actor, cause, scope, checkpoint)
}

type skillsCleanupPGCall struct {
	done chan struct{}
	err  error
}

type skillsCleanupPGObject struct {
	id   string
	key  string
	size int64
}

type skillsCleanupPG struct {
	p2                *skillInstallationFixture
	projects          *project.Authority
	guard             *object.ProcessGuard
	process           oc.ProcessID
	installer         *toolruntime.InstallAuthority
	agents            *agent.Service
	tap               *skillsCleanupPGTap
	cause             pc.LifecycleCause
	drivers           []*project.LifecycleCleanupDriver
	calls             []*skillsCleanupPGCall
	barrier           *schedulerStopBarrier
	objects           []skillsCleanupPGObject
	foreign           string
	otherParticipants string
	closed            bool
}

func newSkillsCleanupPG(t *testing.T) *skillsCleanupPG {
	t.Helper()
	v := &skillsCleanupPG{}
	v.p2 = newAgentOwnershipInstallationFixture(t, func() bool { return v.p2 == nil || v.closed }, func(store *hookStore, projects *project.Authority, guard *object.ProcessGuard, process oc.ProcessID) (*skill.Authority, error) {
		v.projects, v.guard, v.process = projects, guard, process
		var err error
		v.installer, err = toolruntime.NewInstallAuthority(store, guard, process)
		if err != nil {
			return nil, err
		}
		t.Cleanup(func() {
			if v.p2 == nil {
				v.installer.Stop()
				ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
				defer cancel()
				if err := v.installer.Drain(ctx); err != nil || !v.installer.Joined() {
					t.Error("partial fixture install authority did not return", err)
				}
			}
		})
		return skill.NewAuthorityWithInstallExecution(store, projects, v.installer)
	})
	t.Cleanup(func() { v.close(t) })
	v.tap = &skillsCleanupPGTap{service: v.p2.service}
	actual, err := v.guard.CurrentProcess()
	outboxCleanupRequire(t, err)
	if actual != v.process {
		t.Fatal("original Skill/Object runtime process changed")
	}
	// The inherited cleanup stack drains the real service/runtime before this
	// callback, and still owns its Store. It cannot substitute a timeout for
	// the original guard's release or the actual process checkpoint.
	v.p2.base.afterOwners = func() {
		if _, err := v.guard.CurrentProcess(); err == nil {
			t.Error("original guard remained bound after actual runtime retirement")
		}
		var stopped bool
		err := v.p2.base.raw.QueryRow(ctxFor(t), `SELECT state='stopped' FROM agenteam_object.process_claims WHERE process_id=$1`, v.process.String()).Scan(&stopped)
		if err != nil || !stopped {
			t.Error("original process did not checkpoint actual runtime return", err)
		}
	}
	v.foreign = v.foreignSnapshot(t)
	return v
}

func (v *skillsCleanupPG) install(t *testing.T) {
	t.Helper()
	pkg := skillInstallationPackage(t)
	want, err := pkg.Bytes()
	outboxCleanupRequire(t, err)
	request, err := skill.NewInstallRequest(ctxFor(t), id[pc.Skill](t), pkg)
	outboxCleanupRequire(t, err)
	command := meta(t, id[struct{}](t).String(), nil)
	receipt, err := v.p2.service.Install(ctxFor(t), v.p2.base.ownerBrowser.actor, command, v.p2.project.ID, request)
	outboxCleanupRequire(t, err)
	if receipt.Validate() != nil || receipt.SkillID != request.SkillID() || receipt.ProjectID != v.p2.project.ID {
		t.Fatal("original Human installation receipt changed")
	}
	observed, err := v.p2.service.LookupInstall(ctxFor(t), v.p2.base.ownerBrowser.actor, v.p2.project.ID, command.IdempotencyKey, request)
	outboxCleanupRequire(t, err)
	if observed != receipt {
		t.Fatal("original installation lookup did not confirm the exact request")
	}
	body, err := v.p2.service.OpenPackage(ctxFor(t), v.p2.base.ownerBrowser.actor, v.p2.project.ID, receipt.SkillID, 1)
	outboxCleanupRequire(t, err)
	got, readErr := io.ReadAll(body)
	closeErr := body.Close()
	if readErr != nil || closeErr != nil || !body.Joined() || !bytes.Equal(got, want) {
		t.Fatal("original installed package reader did not return exact bytes", readErr, closeErr)
	}
}

func (v *skillsCleanupPG) captureObjects(t *testing.T, count int) {
	t.Helper()
	rows, err := v.p2.base.raw.Query(ctxFor(t), `SELECT o.id::text,o.candidate_key,o.byte_size FROM agenteam_skill.revisions r JOIN agenteam_object.objects o ON o.id=r.object_id WHERE r.project_id=$1 ORDER BY o.id`, v.p2.project.ID.String())
	outboxCleanupRequire(t, err)
	defer rows.Close()
	for rows.Next() {
		var value skillsCleanupPGObject
		outboxCleanupRequire(t, rows.Scan(&value.id, &value.key, &value.size))
		if value.id == "" || value.key == "" || value.size <= 0 {
			t.Fatal("real published Skill lacked its original object")
		}
		v.objects = append(v.objects, value)
	}
	outboxCleanupRequire(t, rows.Err())
	if len(v.objects) != count {
		t.Fatal("wrong number of real Skill package objects", len(v.objects))
	}
	v.nativePresent(t)
}

func (v *skillsCleanupPG) nativePresent(t *testing.T) {
	t.Helper()
	for _, value := range v.objects {
		info, err := v.p2.s3.StatObject(ctxFor(t), v.p2.bucket, value.key, minio.StatObjectOptions{})
		if err != nil || info.Size != value.size {
			t.Fatal("original package was not present at its owned native key", err)
		}
	}
}

func (v *skillsCleanupPG) enterCleanup(t *testing.T) {
	t.Helper()
	_, v.cause, _ = v.p2.enterCleanup(t)
	var err error
	v.otherParticipants = v.otherSnapshot(t)
	var state string
	err = v.p2.base.raw.QueryRow(ctxFor(t), `SELECT state FROM agenteam_object.project_stops WHERE project_id=$1`, v.p2.project.ID.String()).Scan(&state)
	outboxCleanupRequire(t, err)
	if state != "stopped" {
		t.Fatal("actual Object project stop was not confirmed")
	}
}

func (v *skillsCleanupPG) driver(t *testing.T) *project.LifecycleCleanupDriver {
	t.Helper()
	driver, err := project.NewLifecycleCleanupDriverForSkills(v.p2.base.tracked, v.projects, captureProviderProcesses{guard: v.guard}, v.tap)
	outboxCleanupRequire(t, err)
	v.drivers = append(v.drivers, driver)
	return driver
}

func (v *skillsCleanupPG) run(t *testing.T, driver *project.LifecycleCleanupDriver) *skillsCleanupPGCall {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	call := &skillsCleanupPGCall{done: make(chan struct{})}
	v.calls = append(v.calls, call)
	go func() {
		defer close(call.done)
		defer cancel()
		call.err = driver.Run(ctx, v.p2.project.ID, v.cause.OperationID)
	}()
	return call
}

// Read persisted state independently of the new driver's checkpoint decoder.
func (v *skillsCleanupPG) state(t *testing.T, wantClaim string) string {
	t.Helper()
	var lifecycle, state, stage, cleanup, stop, claim, process string
	var schema *int64
	var raw []byte
	err := v.p2.base.raw.QueryRow(ctxFor(t), `SELECT p.lifecycle,o.state,o.cleanup_stage,r.stop_state,r.cleanup_state,r.checkpoint_schema,r.checkpoint,w.phase,w.process_id::text FROM agenteam_project.projects p JOIN agenteam_project.lifecycle_operations o ON o.id=p.current_lifecycle_operation_id JOIN agenteam_project.lifecycle_participants r ON r.operation_id=o.id AND r.participant_name='agent-skills-variables' JOIN agenteam_project.work_claims w ON w.work_id=o.id AND w.work_kind='lifecycle' WHERE p.id=$1 AND o.id=$2`, v.p2.project.ID.String(), v.cause.OperationID.String()).Scan(&lifecycle, &state, &stage, &stop, &cleanup, &schema, &raw, &claim, &process)
	outboxCleanupRequire(t, err)
	if lifecycle != "deleting" || state != "cleaning" || stage != "domains" || stop != "stopped" || claim != wantClaim || process != v.process.String() || cleanup != "required" && cleanup != "pending" {
		t.Fatal("Skills substep changed the parent lifecycle or original owner", lifecycle, state, stage, stop, cleanup, claim)
	}
	if schema == nil {
		if raw != nil || cleanup != "required" {
			t.Fatal("missing child checkpoint was published as parent progress")
		}
		return ""
	}
	var cp struct {
		Project    string    `json:"project_id"`
		Operation  string    `json:"operation_id"`
		Version    f.Version `json:"project_version"`
		State      string    `json:"skills_state"`
		Checkpoint *struct {
			Schema f.Version `json:"schema"`
			Bytes  []byte    `json:"bytes"`
		} `json:"skills_checkpoint,omitempty"`
	}
	if *schema != 2 || json.Unmarshal(raw, &cp) != nil || cp.Project != v.p2.project.ID.String() || cp.Operation != v.cause.OperationID.String() || cp.Version != v.cause.ProjectVersion || cp.State != "pending" && cp.State != "completed" || cleanup != "pending" {
		t.Fatal("persisted Skills subcheckpoint lost its exact original identity")
	}
	if cp.Checkpoint != nil && (cp.Checkpoint.Schema != 1 || len(cp.Checkpoint.Bytes) == 0) {
		t.Fatal("parent did not preserve the original provider checkpoint")
	}
	return cp.State
}

func (v *skillsCleanupPG) otherSnapshot(t *testing.T) string {
	t.Helper()
	var value string
	err := v.p2.base.raw.QueryRow(ctxFor(t), `SELECT COALESCE(jsonb_agg(to_jsonb(p) ORDER BY participant_name),'[]')::text FROM agenteam_project.lifecycle_participants p WHERE operation_id=$1 AND participant_name<>'agent-skills-variables'`, v.cause.OperationID.String()).Scan(&value)
	outboxCleanupRequire(t, err)
	return value
}

func (v *skillsCleanupPG) foreignSnapshot(t *testing.T) string {
	t.Helper()
	var value string
	err := v.p2.base.raw.QueryRow(ctxFor(t), `SELECT jsonb_build_array((SELECT to_jsonb(p) FROM agenteam_project.projects p WHERE id=$1),(SELECT jsonb_agg(to_jsonb(a) ORDER BY id) FROM agenteam_audit.audit_records a WHERE project_id=$1),(SELECT jsonb_agg(to_jsonb(e) ORDER BY id) FROM agenteam_outbox.events e WHERE project_id=$1))::text`, v.p2.base.project.ID.String()).Scan(&value)
	outboxCleanupRequire(t, err)
	return value
}

func (v *skillsCleanupPG) isolated(t *testing.T) {
	t.Helper()
	if v.foreignSnapshot(t) != v.foreign || v.otherSnapshot(t) != v.otherParticipants {
		t.Fatal("Skills substep changed another Project or another participant")
	}
	actual, err := v.guard.CurrentProcess()
	if err != nil || actual != v.process {
		t.Fatal("original ProcessGuard retired before its real cleanup owners", err)
	}
}

func (v *skillsCleanupPG) empty(t *testing.T) {
	t.Helper()
	var local int
	err := v.p2.base.raw.QueryRow(ctxFor(t), `SELECT (SELECT count(*) FROM agenteam_skill.initializations WHERE project_id=$1)+(SELECT count(*) FROM agenteam_skill.object_attempts WHERE project_id=$1)+(SELECT count(*) FROM agenteam_skill.installations WHERE project_id=$1)+(SELECT count(*) FROM agenteam_skill.installation_attempts WHERE project_id=$1)+(SELECT count(*) FROM agenteam_skill.skills WHERE project_id=$1)+(SELECT count(*) FROM agenteam_skill.revisions WHERE project_id=$1)+(SELECT count(*) FROM agenteam_skill.cleanup WHERE project_id=$1)+(SELECT count(*) FROM agenteam_skill.work WHERE project_id=$1)+(SELECT count(*) FROM agenteam_skill.agent_assignment_heads WHERE project_id=$1)+(SELECT count(*) FROM agenteam_skill.agent_assignments WHERE project_id=$1)+(SELECT count(*) FROM agenteam_skill.execution_binding_heads WHERE project_id=$1)+(SELECT count(*) FROM agenteam_skill.execution_bindings WHERE project_id=$1)`, v.p2.project.ID.String()).Scan(&local)
	if err != nil || local != 0 {
		t.Fatal("Skills child completed before its actual local parents were removed", err, local)
	}
	for _, value := range v.objects {
		var anchors, deletes int
		err = v.p2.base.raw.QueryRow(ctxFor(t), `SELECT (SELECT count(*) FROM agenteam_object.objects WHERE id=$1)+(SELECT count(*) FROM agenteam_object.object_references WHERE object_id=$1)+(SELECT count(*) FROM agenteam_object.object_leases WHERE object_id=$1)+(SELECT count(*) FROM agenteam_object.uploads WHERE object_id=$1)+(SELECT count(*) FROM agenteam_object.upload_attempts WHERE object_id=$1)+(SELECT count(*) FROM agenteam_object.cleanup_operations WHERE object_id=$1)+(SELECT count(*) FROM agenteam_object.project_work WHERE object_id=$1)+(SELECT count(*) FROM agenteam_object.object_transfers WHERE object_id=$1),(SELECT count(*) FROM agenteam_audit.audit_records WHERE project_id=$2 AND resource_id=$1 AND action='object.delete')`, value.id, v.p2.project.ID.String()).Scan(&anchors, &deletes)
		if err != nil || anchors != 0 || deletes != 1 {
			t.Fatal("actual Object anchors or original deletion Audit changed", err, anchors, deletes)
		}
		if _, err = v.p2.s3.StatObject(ctxFor(t), v.p2.bucket, value.key, minio.StatObjectOptions{}); minio.ToErrorResponse(err).Code != "NoSuchKey" {
			t.Fatal("original native Skill package was not physically removed")
		}
	}
}

func testProjectSkillsCleanupReturn(t *testing.T) {
	v := newSkillsCleanupPG(t)
	v.install(t)
	v.captureObjects(t, 2)
	v.enterCleanup(t)
	driver := v.driver(t)
	v.barrier = newSchedulerStopBarrier()
	var purged atomic.Bool
	store := v.p2.base.tracked
	store.setAfter(func(ctx context.Context, tx f.Tx, cause f.TransactionCause) error {
		if cause.Details().Owner != "skill-cleanup" || cause.Details().RecoveryRunID != v.cause.OperationID.String() {
			return nil
		}
		x, err := store.InTx(tx)
		if err != nil {
			return err
		}
		var gone bool
		err = x.QueryRow(ctx, `SELECT NOT EXISTS(SELECT 1 FROM agenteam_skill.initializations WHERE project_id=$1)`, v.p2.project.ID.String()).Scan(&gone)
		if err == nil && gone {
			purged.Store(true)
		}
		return err
	})
	store.mu.Lock()
	store.afterResult = func(cause f.TransactionCause, result f.CommitResult) {
		if cause.Details().Owner == "skill-cleanup" && purged.Load() && result.State() == f.Committed {
			v.barrier.hold(result)
		}
	}
	store.mu.Unlock()
	var held *skillsCleanupPGCall
	// The existing two-object fixture needs distinct physical, history and
	// metadata rounds. Pending is consumed explicitly, never treated as success
	// for the whole parent or repaired with test SQL.
	for round := 0; round < 24 && held == nil; round++ {
		call := v.run(t, driver)
		select {
		case result := <-v.barrier.entered:
			if result.State() != f.Committed {
				t.Fatal("original final purge did not physically commit")
			}
			held = call
		case <-call.done:
			outboxCleanupRequire(t, call.err)
			if v.state(t, "terminal") != "pending" {
				t.Fatal("child completed before its held final return")
			}
			v.isolated(t)
		case <-time.After(5 * time.Second):
			t.Fatal("original bounded Skills cleanup round did not return")
		}
	}
	if held == nil {
		t.Fatal("actual Skills final metadata return barrier was not reached")
	}
	v.empty(t)
	if v.state(t, "running") != "pending" {
		t.Fatal("physical purge advanced Project before original return")
	}
	v.isolated(t)
	driver.Stop()
	drainCtx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	err := driver.Drain(drainCtx)
	cancel()
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("Drain bypassed actual Skills return", err)
	}
	select {
	case <-held.done:
		t.Fatal("held original cleanup had already returned")
	default:
	}
	v.barrier.open()
	outboxCleanupWait(t, held.done, "original final Skills cleanup return")
	if held.err == nil {
		t.Fatal("cancelled original cleanup published progress")
	}
	outboxCleanupRequire(t, driver.Drain(ctxFor(t)))
	store.setAfter(nil)
	store.mu.Lock()
	store.afterResult = nil
	store.mu.Unlock()
	if v.state(t, "terminal") != "pending" {
		t.Fatal("cancelled final return completed the child")
	}
	next := v.driver(t)
	call := v.run(t, next)
	outboxCleanupWait(t, call.done, "original Skills completion confirmation")
	outboxCleanupRequire(t, call.err)
	if v.state(t, "terminal") != "completed" {
		t.Fatal("real empty Skills child was not checkpointed")
	}
	v.empty(t)
	v.isolated(t)
	count := v.tap.calls.Load()
	call = v.run(t, next)
	outboxCleanupWait(t, call.done, "Skills child-only replay")
	outboxCleanupRequire(t, call.err)
	if v.tap.calls.Load() != count || v.state(t, "terminal") != "completed" {
		t.Fatal("child replay re-entered cleanup or completed the parent")
	}
	v.isolated(t)
	v.close(t)
	t.Log("SKILLS_DOMAIN_CLEANUP actual_create_install=true original_purge_return=true packages_and_metadata_removed=true parent_pending_domains=true other_participants_unchanged=true")
}

func (v *skillsCleanupPG) disabledAgent(t *testing.T) i.AgentID {
	t.Helper()
	source, err := toolruntime.NewInstallSource(v.installer, v.p2.service)
	outboxCleanupRequire(t, err)
	graph, err := assembly.New(v.p2.base.tracked, assembly.Options{Accounts: v.p2.base.accounts, BuiltinSources: map[string]registry.BuiltinSource{builtin.SkillInstallStableKey: source}, InstallSourceKey: builtin.SkillInstallStableKey})
	outboxCleanupRequire(t, err)
	processes := captureProviderProcesses{guard: v.guard}
	base := &agentCreateFixture{p2: v.p2, guard: v.guard, providers: graph.Providers()}
	base.modelID = base.createModel(t, fixtureProcess{processes.CurrentProcess()})
	v.agents, err = graph.NewService(v.p2.base.keys, processes)
	outboxCleanupRequire(t, err)
	disabled := false
	target := id[i.Agent](t)
	request, err := ac.NewAgentCreate(ac.AgentCreateFields{AgentID: target, Name: "retained-agent", Description: "Disabled Skill assignment remains owned", Instructions: "No execution is started", ModelRef: base.modelID, ApprovalPolicy: ac.ApprovalDefault, AllowedToolIDs: []i.ToolID{}, AllowedMountIDs: []i.MountID{}, AllowedSecretVariableIDs: []i.ProjectVariableID{}, AddSkillsEnabled: &disabled, InstallSkillEnabled: &disabled})
	outboxCleanupRequire(t, err)
	receipt, err := v.agents.CreateAgent(ctxFor(t), v.p2.base.ownerBrowser.actor, meta(t, id[struct{}](t).String(), nil), v.p2.project.ID, request)
	outboxCleanupRequire(t, err)
	if receipt.Validate() != nil || receipt.Fields().Agent.Fields().Core.ID != target {
		t.Fatal("real disabled Agent was not committed")
	}
	var heads, assignments int
	err = v.p2.base.raw.QueryRow(ctxFor(t), `SELECT (SELECT count(*) FROM agenteam_skill.agent_assignment_heads WHERE project_id=$1 AND agent_id=$2 AND NOT add_skills_enabled AND initial_assignment_id IS NULL),(SELECT count(*) FROM agenteam_skill.agent_assignments WHERE project_id=$1 AND agent_id=$2)`, v.p2.project.ID.String(), target.String()).Scan(&heads, &assignments)
	if err != nil || heads != 1 || assignments != 0 {
		t.Fatal("disabled Agent did not keep its actual initialization head", err, heads, assignments)
	}
	v.agents.Stop()
	outboxCleanupRequire(t, v.agents.Drain(ctxFor(t)))
	if !v.agents.Joined() {
		t.Fatal("original Agent call owner did not return")
	}
	return target
}

func (v *skillsCleanupPG) retained(t *testing.T) string {
	t.Helper()
	var value string
	err := v.p2.base.raw.QueryRow(ctxFor(t), `SELECT jsonb_build_array((SELECT jsonb_agg(to_jsonb(h) ORDER BY agent_id) FROM agenteam_skill.agent_assignment_heads h WHERE project_id=$1),(SELECT jsonb_agg(to_jsonb(a) ORDER BY id) FROM agenteam_skill.agent_assignments a WHERE project_id=$1),(SELECT jsonb_agg(to_jsonb(s) ORDER BY id) FROM agenteam_skill.skills s WHERE project_id=$1),(SELECT jsonb_agg(to_jsonb(r) ORDER BY id) FROM agenteam_skill.revisions r WHERE project_id=$1),(SELECT jsonb_agg(to_jsonb(n) ORDER BY project_id) FROM agenteam_skill.initializations n WHERE project_id=$1),(SELECT jsonb_agg(to_jsonb(c) ORDER BY id) FROM agenteam_skill.cleanup c WHERE project_id=$1),(SELECT jsonb_agg(to_jsonb(a) ORDER BY id) FROM agenteam_agent.agents a WHERE project_id=$1),(SELECT jsonb_agg(to_jsonb(a) ORDER BY id) FROM agenteam_audit.audit_records a WHERE project_id=$1),(SELECT jsonb_agg(to_jsonb(o) ORDER BY o.id) FROM agenteam_object.objects o JOIN agenteam_skill.revisions r ON r.object_id=o.id WHERE r.project_id=$1),(SELECT jsonb_agg(to_jsonb(x) ORDER BY x.object_id,x.owner_kind,x.owner_id) FROM agenteam_object.object_references x JOIN agenteam_skill.revisions r ON r.object_id=x.object_id WHERE r.project_id=$1))::text`, v.p2.project.ID.String()).Scan(&value)
	outboxCleanupRequire(t, err)
	return value
}

func testProjectSkillsCleanupDisabledAgent(t *testing.T) {
	v := newSkillsCleanupPG(t)
	v.disabledAgent(t)
	v.captureObjects(t, 1)
	v.enterCleanup(t)
	before := v.retained(t)
	call := v.run(t, v.driver(t))
	outboxCleanupWait(t, call.done, "disabled Agent Skills cleanup refusal")
	outboxCleanupRequire(t, call.err)
	if v.state(t, "terminal") != "pending" || v.retained(t) != before {
		t.Fatal("disabled Agent head was treated as retired or its Skill was released")
	}
	v.nativePresent(t)
	v.isolated(t)
	v.close(t)
	t.Log("SKILLS_DOMAIN_RETAINED actual_disabled_agent=true assignment_rows_zero=true initialization_head_retained=true no_release=true parent_pending_domains=true")
}

func (v *skillsCleanupPG) close(t *testing.T) {
	t.Helper()
	if v.closed {
		return
	}
	if v.barrier != nil {
		v.barrier.open()
	}
	for _, driver := range v.drivers {
		driver.Stop()
	}
	for _, call := range v.calls {
		select {
		case <-call.done:
		case <-time.After(3 * time.Second):
			t.Error("cleanup retains original Skills driver and guard until actual return")
			<-call.done
		}
	}
	for _, driver := range v.drivers {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		err := driver.Drain(ctx)
		cancel()
		if err != nil {
			t.Error("original cleanup driver Drain", err)
			_ = driver.Drain(context.Background())
		}
	}
	if v.agents != nil {
		v.agents.Stop()
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		err := v.agents.Drain(ctx)
		cancel()
		if err != nil || !v.agents.Joined() {
			t.Error("original Agent return before Skill runtime", err)
			_ = v.agents.Drain(context.Background())
		}
	}
	if v.installer != nil {
		v.installer.Stop()
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		err := v.installer.Drain(ctx)
		cancel()
		if err != nil || !v.installer.Joined() {
			t.Error("original install authority return before Skill runtime", err)
			_ = v.installer.Drain(context.Background())
		}
	}
	if v.p2 != nil {
		v.p2.base.tracked.setAfter(nil)
		v.p2.base.tracked.mu.Lock()
		v.p2.base.tracked.afterResult = nil
		v.p2.base.tracked.mu.Unlock()
	}
	v.closed = true
}
