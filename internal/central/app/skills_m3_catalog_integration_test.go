//go:build integration && linux

package app

import (
	"context"
	"encoding/json"
	"net/netip"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	ac "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/config"
	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	skc "github.com/LunaDeerTech/agenteam/internal/central/skill/contract"
	wc "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
	netfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/outbound"
)

type skillsM3CatalogWorkflow struct {
	v       *executionSchedulerApp
	network *netfixture.Descriptor
	tools   map[string]id.ToolID
}

func newSkillsM3CatalogWorkflow(t *testing.T, observers ...func(f.TransactionCause, f.CommitResult)) *skillsM3CatalogWorkflow {
	t.Helper()
	TestSkillsM3AssignmentConfiguration(t)
	network, err := netfixture.Load()
	skillsAssignmentCheck(t, "catalog-network", err)
	address, err := netip.ParseAddr(network.PrivateIP)
	skillsAssignmentCheck(t, "catalog-address", err)
	var transport *outboundRuntime
	v := newExecutionSchedulerAppWith(t, func(d *dependencies, v *executionSchedulerApp) {
		env := guardEnvironment(t, v.root.db)
		for k, value := range skillsM3Settings() {
			env = append(env, "AGENTEAM_CENTRAL_"+k+"="+value)
		}
		env = append(env, "AGENTEAM_CENTRAL_OUTBOUND_CA_FILE="+network.CAFile)
		v.root.cfg = guardConfiguration(t, env)
		original := skillsAssignmentCommitObserver(t)
		v.store.observeCommit = func(cause f.TransactionCause, result f.CommitResult) {
			original(cause, result)
			for _, observe := range observers {
				observe(cause, result)
			}
		}
		d.outboundConstruct = func(_ context.Context, _ config.Config, actual *outboundRuntime) error {
			actual.resolver = appFixtureResolver{address}
			transport = actual
			return nil
		}
	})
	if transport == nil {
		t.Fatal("catalog fixture lost original outbound")
	}
	m1AllowOwnedPeer(t, v, transport, network)
	w := &skillsM3CatalogWorkflow{v: v, network: network, tools: map[string]id.ToolID{}}
	for _, name := range []string{"install-skill", "list-skills", "read-skill", "assign-skill"} {
		var raw string
		skillsAssignmentCheck(t, "catalog-tool", v.admin.QueryRow(v.ctx, `SELECT tool_id FROM agenteam_tool.identities WHERE stable_key=$1`, "builtin:"+name).Scan(&raw))
		w.tools[name], err = f.ParseID[id.Tool](raw)
		skillsAssignmentCheck(t, "catalog-tool-id", err)
	}
	return w
}
func (w *skillsM3CatalogWorkflow) agent(t *testing.T, p id.ProjectID, model mc.ModelID, name string, install bool) id.AgentID {
	t.Helper()
	v := w.v
	a := guardID[id.Agent](t)
	disabled := false
	allowed := []id.ToolID{w.tools["list-skills"], w.tools["read-skill"], w.tools["assign-skill"]}
	slices.SortFunc(allowed, func(a, b id.ToolID) int { return strings.Compare(a.String(), b.String()) })
	intent, err := ac.NewAgentCreate(ac.AgentCreateFields{AgentID: a, Name: name, Description: "Skill metadata and assignment participant", Instructions: "Inspect metadata, use only assigned skills, and report permission failures without retrying.", ModelRef: model, ApprovalPolicy: ac.ApprovalAllow, AllowedToolIDs: allowed, AllowedMountIDs: []id.MountID{}, AllowedSecretVariableIDs: []id.ProjectVariableID{}, AddSkillsEnabled: &disabled, InstallSkillEnabled: &install, InjectAgentsMD: false})
	skillsAssignmentCheck(t, "catalog-agent-intent", err)
	_, err = v.assembly.agents.service.CreateAgent(v.ctx, v.actor, executionSchedulerAppMeta(t, nil), p, intent)
	skillsAssignmentCheck(t, "catalog-agent", err)
	return a
}
func (w *skillsM3CatalogWorkflow) stop(t *testing.T) {
	t.Helper()
	v := w.v
	v.runtime.StopAdmission()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	err := v.runtime.Drain(ctx)
	cancel()
	skillsAssignmentCheck(t, "catalog-runtime-drain", err)
	v.requestStop()
	usageAppWait(t, v.root.done, "catalog-app-return")
	skillsAssignmentCheck(t, "catalog-app-exit", v.root.err)
	if !v.runtime.Joined() || !v.assembly.Joined() || !v.root.owned.producersJoined() {
		t.Fatal("catalog original owners not joined")
	}
	skillsAssignmentGuardReleased(t, v.workAppFixture)
}
func skillsM3CatalogToolError(t *testing.T, raw string) f.Code {
	t.Helper()
	var out struct {
		Error *struct {
			Code f.Code `json:"code"`
		} `json:"error"`
	}
	skillsAssignmentCheck(t, "catalog-tool-error-shape", json.Unmarshal([]byte(raw), &out))
	if out.Error == nil || !out.Error.Code.Known() {
		t.Fatal("expected explicit permission or scope failure")
	}
	return out.Error.Code
}
func TestSkillsM3AppCatalogOtherTarget(t *testing.T) {
	w := newSkillsM3CatalogWorkflow(t)
	v := w.v
	stage := "provision"
	defer func() {
		if t.Failed() {
			skillsM3Diagnose(t, v, stage)
		}
	}()
	install := map[string]any{"mode": "create", "source": map[string]any{"kind": "text_files", "files": []map[string]string{{"path": "SKILL.md", "utf8_text": skillsM3Text}}}}
	hold := 0
	catalog := skillsM3ToolReply(t, w.tools["assign-skill"], map[string]any{"action": "catalog"}, 2)
	catalog.HoldAfter = &hold
	scenario, err := w.network.Create(v.ctx, netfixture.ScenarioConfig{Mode: "openai_chat_wire", WireSequence: []netfixture.WireScenario{
		skillsM3ToolReply(t, w.tools["install-skill"], install, 1), catalog,
		skillsM3TextReply(t, "Unconfigured target assignment.", 3), skillsM3TextReply(t, "Unconfigured unauthorized read.", 4),
		skillsM3TextReply(t, "Unconfigured cross-project attempt.", 5), skillsM3TextReply(t, "Unconfigured target metadata.", 6),
		skillsM3TextReply(t, "Assigned to the other agent without reading its skill.", 7),
		skillsM3ToolReply(t, w.tools["list-skills"], map[string]any{}, 8), skillsM3TextReply(t, "Unconfigured target read.", 9), skillsM3TextReply(t, "Read my assigned checklist.", 10),
	}})
	skillsAssignmentCheck(t, "catalog-scenario", err)
	defer func() { _ = w.network.Release(context.Background(), scenario) }()
	model := skillsM3ConfigureModel(t, v, "https://fixture.test:8443/case/"+scenario)
	caller := w.agent(t, v.a, model, "catalog-caller", true)
	target := w.agent(t, v.a, model, "catalog-target", false)
	foreign := w.agent(t, v.b, model, "catalog-foreign", false)
	v.agent = caller
	sprint := memoryM3CreateSprint(t, v)
	task := memoryM3CreateTask(t, v, sprint.ID, caller, "Install a checklist, inspect assignable metadata, assign it only to the other agent, and respect body and project permissions.")
	_, err = v.planning.sprints.StartSprint(v.ctx, v.actor, executionSchedulerAppMeta(t, &sprint.Version), v.a, sprint.ID)
	skillsAssignmentCheck(t, "catalog-sprint", err)
	v.store.releaseDirectory()
	stage = "installed-unassigned"
	state := skillsAssignmentAwaitPeer(t, v, w.network, scenario, 2)
	var installed struct {
		SkillID skc.SkillID `json:"skill_id"`
	}
	skillsAssignmentCheck(t, "catalog-install-receipt", json.Unmarshal([]byte(skillsM3LastTool(t, state.Requests[1].Body)), &installed))
	if installed.SkillID.Validate() != nil {
		t.Fatal("actual installed identity missing")
	}
	var count int
	skillsAssignmentCheck(t, "catalog-not-autoassigned", v.admin.QueryRow(v.ctx, `SELECT count(*) FROM agenteam_skill.agent_assignments WHERE skill_id=$1`, installed.SkillID.String()).Scan(&count))
	if count != 0 {
		t.Fatal("install assigned automatically")
	}
	replies := map[int]netfixture.WireScenario{
		3: skillsM3ToolReply(t, w.tools["assign-skill"], map[string]any{"action": "assign", "agent_id": target, "expected_version": f.Version(1), "skill_id": installed.SkillID, "enabled": true}, 3),
		4: skillsM3ToolReply(t, w.tools["read-skill"], map[string]any{"skill_id": installed.SkillID, "path": "SKILL.md"}, 4),
		5: skillsM3ToolReply(t, w.tools["assign-skill"], map[string]any{"action": "assign", "agent_id": foreign, "expected_version": f.Version(1), "skill_id": installed.SkillID, "enabled": true}, 5),
		6: skillsM3ToolReply(t, w.tools["assign-skill"], map[string]any{"action": "catalog", "agent_id": target}, 6),
		9: skillsM3ToolReply(t, w.tools["read-skill"], map[string]any{"skill_id": installed.SkillID, "path": "SKILL.md"}, 9),
	}
	for ordinal, reply := range replies {
		skillsAssignmentCheck(t, "catalog-future-original-response", w.network.UpdateFutureWire(v.ctx, scenario, ordinal, reply))
	}
	skillsAssignmentCheck(t, "catalog-release", w.network.Release(v.ctx, scenario))
	stage = "caller-metadata-other-assignment"
	callerExecution := memoryM3AwaitExecution(t, v, task.ID)
	state = skillsAssignmentAwaitPeer(t, v, w.network, scenario, 7)
	var directory struct {
		Skills []struct {
			Metadata   skc.Metadata              `json:"metadata"`
			Assignment *skc.AgentSkillAssignment `json:"assignment"`
		} `json:"skills"`
		Agents []struct {
			ID      id.AgentID `json:"agent_id"`
			Version f.Version  `json:"expected_version"`
		} `json:"agents"`
	}
	skillsAssignmentCheck(t, "catalog-directory", json.Unmarshal([]byte(skillsM3LastTool(t, state.Requests[2].Body)), &directory))
	found := false
	for _, s := range directory.Skills {
		if s.Metadata.ID == installed.SkillID && s.Metadata.ProjectID == v.a && s.Assignment == nil {
			found = true
		}
	}
	callerSeen, targetSeen := false, false
	for _, a := range directory.Agents {
		if a.ID == foreign {
			t.Fatal("foreign agent leaked into project directory")
		}
		if a.ID == caller && a.Version == 1 {
			callerSeen = true
		}
		if a.ID == target && a.Version == 1 {
			targetSeen = true
		}
	}
	if !found || !callerSeen || !targetSeen || strings.Contains(skillsM3LastTool(t, state.Requests[2].Body), skillsM3Text) {
		t.Fatal("metadata directory omitted permitted facts or leaked body")
	}
	var assigned skc.AssignmentMutation
	skillsAssignmentCheck(t, "catalog-other-receipt", json.Unmarshal([]byte(skillsM3LastTool(t, state.Requests[3].Body)), &assigned))
	if assigned.Validate() != nil || !assigned.Changed || assigned.AgentVersion != 2 || assigned.Assignment == nil || assigned.Assignment.AgentID != target {
		t.Fatal("other-target canonical receipt missing")
	}
	if skillsM3CatalogToolError(t, skillsM3LastTool(t, state.Requests[4].Body)) != f.Forbidden {
		t.Fatal("metadata granted caller body")
	}
	_ = skillsM3CatalogToolError(t, skillsM3LastTool(t, state.Requests[5].Body))
	directory.Skills = nil
	directory.Agents = nil
	skillsAssignmentCheck(t, "catalog-current-target", json.Unmarshal([]byte(skillsM3LastTool(t, state.Requests[6].Body)), &directory))
	if len(directory.Agents) != 1 || directory.Agents[0].ID != target || directory.Agents[0].Version != 2 {
		t.Fatal("target version not discoverable")
	}
	found = false
	for _, s := range directory.Skills {
		if s.Metadata.ID == installed.SkillID && s.Assignment != nil && s.Assignment.ID == assigned.Assignment.ID {
			found = true
		}
	}
	if !found {
		t.Fatal("target assignment identity not discoverable")
	}
	stage = "target-execution-read"
	targetTask := memoryM3CreateTask(t, v, sprint.ID, target, "List and read your newly assigned checklist.")
	targetExecution := memoryM3AwaitExecution(t, v, targetTask.ID)
	if targetExecution == callerExecution {
		t.Fatal("target reused caller execution")
	}
	w.stop(t)
	state = (&m1Workflow{app: v, network: w.network}).joinedPeer(t, scenario, 10, 10)
	var read struct {
		SkillID skc.SkillID `json:"skill_id"`
		Text    string      `json:"text"`
	}
	skillsAssignmentCheck(t, "catalog-target-model-body", json.Unmarshal([]byte(skillsM3LastTool(t, state.Requests[9].Body)), &read))
	if read.SkillID != installed.SkillID || read.Text != skillsM3Text {
		t.Fatal("actual target did not receive canonical body")
	}
	// Skill IDs and Audit resource IDs are UUIDs; Tool safe_id is text.
	// Keep parameter types local to each owning schema.
	var targetAssignments, callerAssignments, foreignAssignments, commands, audits int
	skillsAssignmentCheck(t, "catalog-isolation-facts", v.admin.QueryRow(v.ctx, `SELECT
 (SELECT count(*) FROM agenteam_skill.agent_assignments WHERE skill_id=$1 AND agent_id=$2 AND enabled AND removed_at IS NULL),
 (SELECT count(*) FROM agenteam_skill.agent_assignments WHERE skill_id=$1 AND agent_id=$3),
 (SELECT count(*) FROM agenteam_skill.agent_assignments WHERE agent_id=$4 AND skill_id=$1),
 (SELECT count(*) FROM agenteam_skill.assignment_commands WHERE project_id=$5 AND actor_kind='agent_run' AND state='completed'),
 (SELECT count(*) FROM agenteam_audit.audit_records WHERE project_id=$5 AND producer='skill' AND action='skill.assign' AND resource_id=$2::uuid)`, installed.SkillID.String(), target.String(), caller.String(), foreign.String(), v.a.String()).Scan(&targetAssignments, &callerAssignments, &foreignAssignments, &commands, &audits))
	if targetAssignments != 1 || callerAssignments != 0 || foreignAssignments != 0 || commands != 1 || audits != 1 {
		t.Fatal("catalog or denied calls changed assignment facts")
	}
	for _, check := range []struct {
		agent   id.AgentID
		project id.ProjectID
		version int64
	}{{target, v.a, 2}, {caller, v.a, 1}, {foreign, v.b, 1}} {
		var canonical, tool, mount, model int64
		skillsAssignmentCheck(t, "catalog-exact-target-reference-versions", v.admin.QueryRow(v.ctx, `SELECT a.version,t.config_version,m.owner_version,r.owner_version FROM agenteam_agent.agents a JOIN agenteam_tool.agent_configurations t ON t.agent_id=a.id::text JOIN agenteam_mount.agent_mount_heads m ON m.agent_id=a.id JOIN agenteam_model.references r ON r.owner_kind='agent' AND r.owner_id=a.id AND r.role='agent_model' WHERE a.id=$1::uuid AND a.project_id=$2::uuid`, check.agent.String(), check.project.String()).Scan(&canonical, &tool, &mount, &model))
		if canonical != check.version || tool != check.version || mount != check.version || model != check.version {
			t.Fatal("assignment advanced a foreign or incomplete reference owner")
		}
	}

	t.Log("SKILLS_CATALOG_OTHER_TARGET same_project_metadata=true metadata_not_body=true other_target_assigned=true cross_project_rejected=true target_new_execution_read=true no_autoassign=true original_app_joined=true external_model=false")
}

func TestSkillsM3AppCatalogPermissionRevoked(t *testing.T) {
	var revoked, refused atomic.Bool
	w := newSkillsM3CatalogWorkflow(t, func(_ f.TransactionCause, r f.CommitResult) {
		if revoked.Load() && r.State() == f.NotCommitted && r.Fault() != nil && r.Fault().Code == f.Forbidden {
			refused.Store(true)
		}
	})
	v := w.v
	stage := "provision"
	defer func() {
		if t.Failed() {
			skillsM3Diagnose(t, v, stage)
		}
	}()
	hold := 0
	reply := skillsM3ToolReply(t, w.tools["assign-skill"], map[string]any{"action": "catalog"}, 1)
	reply.HoldAfter = &hold
	scenario, err := w.network.Create(v.ctx, netfixture.ScenarioConfig{Mode: "openai_chat_wire", WireSequence: []netfixture.WireScenario{reply, skillsM3TextReply(t, "Catalog permission was revoked.", 2)}})
	skillsAssignmentCheck(t, "catalog-revoke-scenario", err)
	defer func() { _ = w.network.Release(context.Background(), scenario) }()
	model := skillsM3ConfigureModel(t, v, "https://fixture.test:8443/case/"+scenario)
	caller := w.agent(t, v.a, model, "catalog-revoked", false)
	v.agent = caller
	sprint := memoryM3CreateSprint(t, v)
	task := memoryM3CreateTask(t, v, sprint.ID, caller, "Inspect assignable project metadata.")
	_, err = v.planning.sprints.StartSprint(v.ctx, v.actor, executionSchedulerAppMeta(t, &sprint.Version), v.a, sprint.ID)
	skillsAssignmentCheck(t, "catalog-revoke-sprint", err)
	v.store.releaseDirectory()
	stage = "captured-before-revoke"
	captured := skillsAssignmentAwaitPeer(t, v, w.network, scenario, 1)
	var request struct {
		Tools []struct {
			Function struct {
				Name string `json:"name"`
			} `json:"function"`
		} `json:"tools"`
	}
	skillsAssignmentCheck(t, "catalog-captured-tool-schema", json.Unmarshal([]byte(captured.Requests[0].Body), &request))
	capturedAssign := false
	for _, tool := range request.Tools {
		if tool.Function.Name == "tool_"+strings.ReplaceAll(w.tools["assign-skill"].String(), "-", "") {
			capturedAssign = true
		}
	}
	if !capturedAssign {
		t.Fatal("original Model did not capture allowed assignment tool")
	}
	none := []id.ToolID{}
	update, err := ac.NewAgentUpdate(ac.AgentUpdateFields{AllowedToolIDs: &none})
	skillsAssignmentCheck(t, "catalog-revoke-intent", err)
	version := f.Version(1)
	_, err = v.assembly.agents.service.UpdateAgent(v.ctx, v.actor, executionSchedulerAppMeta(t, &version), v.a, caller, update)
	skillsAssignmentCheck(t, "catalog-revoke-current-tool", err)
	revoked.Store(true)
	skillsAssignmentCheck(t, "catalog-revoke-release", w.network.Release(v.ctx, scenario))
	stage = "current-permission-rejection"
	skillsM3CatalogAwaitTerminal(t, v, task.ID)
	w.stop(t)
	state, err := w.network.State(v.ctx, scenario)
	skillsAssignmentCheck(t, "catalog-revoke-peer", err)
	if len(state.Requests) < 1 || len(state.Requests) > 2 {
		t.Fatal("revoked catalog repeated model")
	}
	(&m1Workflow{app: v, network: w.network}).joinedPeer(t, scenario, len(state.Requests), len(state.Requests))
	// This shared project parameter crosses Tool text and Skill UUID domains.
	var successful, commands int
	skillsAssignmentCheck(t, "catalog-revoked-no-result", v.admin.QueryRow(v.ctx, `SELECT
 (SELECT count(*) FROM agenteam_tool.operations WHERE project_id=$1::text AND operation_kind='skill.assign' AND phase='succeeded'),
 (SELECT count(*) FROM agenteam_skill.assignment_commands WHERE project_id=$1::uuid AND actor_kind='agent_run')`, v.a.String()).Scan(&successful, &commands))
	if successful != 0 || commands != 0 {
		t.Fatal("revoked permission returned metadata or mutated assignments")
	}
	// A returned Tool error is consumed by the successor; a pre-Tool current
	// policy refusal terminates the original Execution without exposing metadata.
	if len(state.Requests) == 2 {
		if skillsM3CatalogToolError(t, skillsM3LastTool(t, state.Requests[1].Body)) != f.Forbidden {
			t.Fatal("revoked catalog failed for unrelated reason")
		}
		refused.Store(true)
	}
	if !refused.Load() {
		t.Fatal("no original current permission refusal observed")
	}
	t.Log("SKILLS_CATALOG_REVOKED original_model_captured=true owner_revoked_allowlist=true no_catalog_success=true no_assignment=true original_app_joined=true")
}
func skillsM3CatalogAwaitTerminal(t *testing.T, v *executionSchedulerApp, task wc.TaskID) {
	t.Helper()
	ctx, cancel := context.WithTimeout(v.ctx, 30*time.Second)
	defer cancel()
	tick := time.NewTicker(25 * time.Millisecond)
	defer tick.Stop()
	for {
		var count int
		var status string
		skillsAssignmentCheck(t, "catalog-terminal", v.admin.QueryRow(ctx, `SELECT count(*),coalesce(min(status),'') FROM agenteam_execution.executions WHERE project_id=$1 AND launch_request->'trigger'->>'task_id'=$2`, v.a.String(), task.String()).Scan(&count, &status))
		if count > 1 {
			t.Fatal("revocation relaunched original task")
		}
		if count == 1 && ec.Status(status).Terminal() {
			if status != string(ec.Failed) && status != string(ec.Succeeded) {
				t.Fatalf("unexpected revocation terminal=%s", status)
			}
			return
		}
		select {
		case <-ctx.Done():
			t.Fatalf("catalog revocation not terminal status=%s", status)
		case <-tick.C:
		}
	}
}
