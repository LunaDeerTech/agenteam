//go:build integration && linux

package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net/netip"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	ac "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/config"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/secret"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
	skc "github.com/LunaDeerTech/agenteam/internal/central/skill/contract"
	netfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/outbound"
)

const skillsM3Text = "---\nname: dynamic-checklist\ndescription: A checklist installed during this execution\n---\nBefore deployment, verify the reversible migration and its rollback.\n"

func skillsM3Settings() map[string]string {
	runtime := strings.Replace(executionSchedulerAppConfig, "direct-text-v1", "tool-loop-v1", 1)
	runtime = strings.Replace(runtime, `"relaunch_skip_count":0`, `"relaunch_skip_count":100000`, 1)
	return map[string]string{"PUBLIC_ORIGIN": "http://localhost:8080", "SHUTDOWN_TIMEOUT": "5s", "EXECUTION_RUNTIME": runtime, "SCHEDULER_LAUNCH_RETRY_MAX_ATTEMPTS": "2", "SCHEDULER_LAUNCH_RETRY_INITIAL_BACKOFF": "100ms", "SCHEDULER_LAUNCH_RETRY_MAX_BACKOFF": "1s"}
}

func TestSkillsM3AppConfiguration(t *testing.T) {
	cfg := testConfig(t, "5s", skillsM3Settings())
	workAppCheck(t, "skills-complete-config", cfg.Validate())
	runtime, enabled := cfg.ExecutionRuntime()
	if !enabled || !runtime.ToolLoop() {
		t.Fatal("Skills fixture lost its deployed Tool loop")
	}
}

func skillsM3ToolText(body string) (string, error) {
	var request struct {
		Messages []struct {
			Role    string          `json:"role"`
			Content json.RawMessage `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal([]byte(body), &request); err != nil {
		return "", err
	}
	for n := len(request.Messages) - 1; n >= 0; n-- {
		if request.Messages[n].Role != "tool" {
			continue
		}
		var parts []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		}
		if json.Unmarshal(request.Messages[n].Content, &parts) != nil || len(parts) != 1 || parts[0].Type != "text" {
			return "", fmt.Errorf("Skills peer expected one text Tool content part")
		}
		return parts[0].Text, nil
	}
	return "", fmt.Errorf("Skills peer request has no Tool result")
}

func skillsM3LastTool(t *testing.T, body string) string {
	t.Helper()
	text, err := skillsM3ToolText(body)
	workAppCheck(t, "skills-peer-tool-text", err)
	return text
}

func TestSkillsM3WireToolText(t *testing.T) {
	const body = `{"messages":[{"role":"assistant","content":null},{"role":"tool","content":[{"type":"text","text":"original package text"}]},{"role":"user","content":"skills_added"}]}`
	text, err := skillsM3ToolText(body)
	if err != nil || text != "original package text" {
		t.Fatal("original Tool text array was not read", err)
	}
	for _, content := range []string{`"different string profile"`, `null`, `[]`, `[{"type":"image_url","text":"not text"}]`} {
		if _, err := skillsM3ToolText(`{"messages":[{"role":"tool","content":` + content + `}]}`); err == nil {
			t.Fatal("different Tool content shape accepted")
		}
	}
}

// Installation uses the original Tool executor and Skill ledger. The returned
// random Skill ID is obtained from the next real Model request. Owner HTTP
// assignment happens only after that request has captured its immutable round.
// No Tool success, Execution, assignment, or Object body is seeded by this test.
func TestSkillsM3AppDynamicInstallRead(t *testing.T) {
	TestSkillsM3AppConfiguration(t)
	network, err := netfixture.Load()
	workAppCheck(t, "skills-owned-network", err)
	address, err := netip.ParseAddr(network.PrivateIP)
	workAppCheck(t, "skills-owned-peer-address", err)
	var transport *outboundRuntime
	v := newExecutionSchedulerAppWith(t, func(d *dependencies, v *executionSchedulerApp) {
		env := guardEnvironment(t, v.root.db)
		for key, value := range skillsM3Settings() {
			env = append(env, "AGENTEAM_CENTRAL_"+key+"="+value)
		}
		env = append(env, "AGENTEAM_CENTRAL_OUTBOUND_CA_FILE="+network.CAFile)
		v.root.cfg = guardConfiguration(t, env)
		v.store.observeCommit = skillsM3CommitObserver(t)
		d.outboundConstruct = func(_ context.Context, _ config.Config, actual *outboundRuntime) error {
			actual.resolver = appFixtureResolver{address}
			transport = actual
			return nil
		}
	})
	stage := "app"
	defer func() {
		if t.Failed() {
			skillsM3Diagnose(t, v, stage)
		}
	}()
	if transport == nil {
		t.Fatal("original outbound Runtime missing")
	}
	m1AllowOwnedPeer(t, v, transport, network)
	tools := map[string]id.ToolID{}
	for _, name := range []string{"install-skill", "list-skills", "read-skill"} {
		var raw string
		workAppCheck(t, "skills-tool-registration", v.admin.QueryRow(v.ctx, `SELECT tool_id FROM agenteam_tool.identities WHERE stable_key=$1`, "builtin:"+name).Scan(&raw))
		tools[name], err = f.ParseID[id.Tool](raw)
		workAppCheck(t, "skills-tool-id", err)
	}
	install := map[string]any{"mode": "create", "source": map[string]any{"kind": "text_files", "files": []map[string]string{{"path": "SKILL.md", "utf8_text": skillsM3Text}}}}
	hold := 0
	second := skillsM3ToolReply(t, tools["list-skills"], map[string]any{}, 2)
	second.HoldAfter = &hold
	scenario, err := network.Create(v.ctx, netfixture.ScenarioConfig{Mode: "openai_chat_wire", WireSequence: []netfixture.WireScenario{
		skillsM3ToolReply(t, tools["install-skill"], install, 1), second,
		skillsM3ToolReply(t, tools["list-skills"], map[string]any{}, 3),
		// Replaced through the nonce-owned future-only control after installation.
		skillsM3TextReply(t, "Future read response has not been configured.", 4),
		skillsM3TextReply(t, "Verified the newly installed checklist.", 5),
	}})
	workAppCheck(t, "skills-agent-peer", err)
	defer func() { _ = network.Release(context.Background(), scenario) }()
	model := skillsM3ConfigureModel(t, v, "https://fixture.test:8443/case/"+scenario)
	agent := guardID[id.Agent](t)
	disabled, enabled := false, true
	allowed := []id.ToolID{tools["list-skills"], tools["read-skill"]}
	slices.SortFunc(allowed, func(a, b id.ToolID) int { return strings.Compare(a.String(), b.String()) })
	intent, err := ac.NewAgentCreate(ac.AgentCreateFields{AgentID: agent, Name: "dynamic-skills-agent", Description: "Dynamic Skills participant", Instructions: "Install the requested package, use only explicitly assigned skills, and read the checklist before answering.", ModelRef: model, ApprovalPolicy: ac.ApprovalAllow, AllowedToolIDs: allowed, AllowedMountIDs: []id.MountID{}, AllowedSecretVariableIDs: []id.ProjectVariableID{}, AddSkillsEnabled: &disabled, InstallSkillEnabled: &enabled, InjectAgentsMD: false})
	workAppCheck(t, "skills-agent-intent", err)
	_, err = v.assembly.agents.service.CreateAgent(v.ctx, v.actor, executionSchedulerAppMeta(t, nil), v.a, intent)
	workAppCheck(t, "skills-agent-create", err)
	v.agent = agent
	sprint := memoryM3CreateSprint(t, v)
	task := memoryM3CreateTask(t, v, sprint.ID, agent, "Install the checklist, wait for its explicit assignment, then list and read the assigned skill.")
	_, err = v.planning.sprints.StartSprint(v.ctx, v.actor, executionSchedulerAppMeta(t, &sprint.Version), v.a, sprint.ID)
	workAppCheck(t, "skills-start-sprint", err)
	v.store.releaseDirectory()
	stage = "original-install"
	state := skillsM3AwaitPeer(t, v, network, scenario, 2)
	var receipt struct {
		SkillID  skc.SkillID `json:"skill_id"`
		Revision f.Revision  `json:"revision"`
		Version  f.Version   `json:"version"`
	}
	workAppCheck(t, "skills-original-install-result", json.Unmarshal([]byte(skillsM3LastTool(t, state.Requests[1].Body)), &receipt))
	if receipt.SkillID.Validate() != nil || receipt.Revision != 1 || receipt.Version != 1 {
		t.Fatal("original installation did not return a canonical Skill identity")
	}
	var assigned int
	workAppCheck(t, "skills-install-not-autoassigned", v.admin.QueryRow(v.ctx, `SELECT count(*) FROM agenteam_skill.agent_assignments WHERE project_id=$1 AND agent_id=$2 AND skill_id=$3`, v.a.String(), agent.String(), receipt.SkillID.String()).Scan(&assigned))
	if assigned != 0 {
		t.Fatal("installation implicitly assigned the Skill")
	}
	stage = "owner-assignment"
	path := "/api/v1/projects/" + v.a.String() + "/agents/" + agent.String() + "/skills/assign"
	response, err := v.request(v.ctx, "POST", path, workAppJSON(t, map[string]any{"expected_version": f.Version(1), "request": map[string]any{"skill_id": receipt.SkillID, "enabled": true}}), guardID[struct{}](t).String())
	workAppCheck(t, "skills-owner-http-assign", err)
	if response.status != 200 {
		t.Fatalf("Skills assignment HTTP status=%d", response.status)
	}
	var assignment skc.AssignmentMutation
	workAppCheck(t, "skills-assignment-receipt", json.Unmarshal(response.body, &assignment))
	if assignment.Validate() != nil || !assignment.Changed || assignment.AgentVersion != 2 || assignment.AssignmentSequence != 2 || assignment.Runtime == nil || !assignment.Runtime.Queued {
		t.Fatal("original assignment was not queued for the active Execution")
	}
	workAppCheck(t, "skills-future-real-id", network.UpdateFutureWire(v.ctx, scenario, 4, skillsM3ToolReply(t, tools["read-skill"], map[string]any{"skill_id": receipt.SkillID, "path": "SKILL.md"}, 4)))
	workAppCheck(t, "skills-release-original-request", network.Release(v.ctx, scenario))
	stage = "next-round-read"
	execution := memoryM3AwaitExecution(t, v, task.ID)
	if assignment.Runtime.ExecutionID == nil || *assignment.Runtime.ExecutionID != execution {
		t.Fatal("assignment routed to a different Execution")
	}
	stage = "original-join"
	v.runtime.StopAdmission()
	drain, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	err = v.runtime.Drain(drain)
	cancel()
	workAppCheck(t, "skills-original-runtime-drain", err)
	v.requestStop()
	usageAppWait(t, v.root.done, "skills-original-app-return")
	workAppCheck(t, "skills-original-app-exit", v.root.err)
	if !v.runtime.Joined() || !v.assembly.Joined() || !v.root.owned.producersJoined() {
		t.Fatal("Skills App returned before original owners joined")
	}
	taskExecutionGuardReleased(t, v.workAppFixture)
	peer := &m1Workflow{app: v, network: network}
	state = peer.joinedPeer(t, scenario, 5, 5)
	var before, after struct {
		Skills []struct {
			SkillID skc.SkillID `json:"skill_id"`
		} `json:"skills"`
	}
	workAppCheck(t, "skills-old-round-list", json.Unmarshal([]byte(skillsM3LastTool(t, state.Requests[2].Body)), &before))
	workAppCheck(t, "skills-new-round-list", json.Unmarshal([]byte(skillsM3LastTool(t, state.Requests[3].Body)), &after))
	if len(before.Skills) != 0 || len(after.Skills) != 1 || after.Skills[0].SkillID != receipt.SkillID {
		t.Fatal("dynamic assignment changed a captured round or failed to enter its successor")
	}
	var read struct {
		SkillID skc.SkillID `json:"skill_id"`
		Text    string      `json:"text"`
		Error   *struct {
			Code f.Code `json:"code"`
		} `json:"error"`
	}
	workAppCheck(t, "skills-model-received-read", json.Unmarshal([]byte(skillsM3LastTool(t, state.Requests[4].Body)), &read))
	if read.Error != nil {
		t.Fatalf("read-skill returned original Tool error code=%s", read.Error.Code.Safe())
	}
	if read.SkillID != receipt.SkillID || read.Text != skillsM3Text {
		t.Fatal("Model did not receive the actual canonical package text")
	}
	var inputs, controls, pending int
	workAppCheck(t, "skills-durable-next-round-inputs", v.admin.QueryRow(v.ctx, `SELECT count(*),count(control_sequence) FROM agenteam_execution.loop_skill_inputs WHERE execution_id=$1`, execution.String()).Scan(&inputs, &controls))
	workAppCheck(t, "skills-ingress-consumed", v.admin.QueryRow(v.ctx, `SELECT count(*) FROM agenteam_execution.skill_assignment_ingress i WHERE i.execution_id=$1 AND i.outcome='queued' AND NOT EXISTS(SELECT 1 FROM agenteam_execution.loop_skill_inputs l, jsonb_array_elements(convert_from(l.content,'UTF8')::jsonb->'dispositions') d WHERE l.execution_id=i.execution_id AND d->>'change_id'=i.change_id AND d->>'state'='applied')`, execution.String()).Scan(&pending))
	if inputs != 4 || controls != 1 || pending != 0 {
		t.Fatalf("Skill round facts inputs=%d controls=%d queued=%d", inputs, controls, pending)
	}
	var readers, liveReaders, liveWork int
	workAppCheck(t, "skills-actual-object-reader-joined", v.admin.QueryRow(v.ctx, `SELECT count(*),count(*) FILTER(WHERE l.state<>'released') FROM agenteam_object.object_leases l JOIN agenteam_skill.installations i ON i.object_id=l.object_id WHERE i.skill_id=$1 AND l.owner_kind='reader'`, receipt.SkillID.String()).Scan(&readers, &liveReaders))
	workAppCheck(t, "skills-domain-work-joined", v.admin.QueryRow(v.ctx, `SELECT count(*) FROM agenteam_skill.work WHERE skill_id=$1 AND phase<>'joined'`, receipt.SkillID.String()).Scan(&liveWork))
	if readers == 0 || liveReaders != 0 || liveWork != 0 {
		t.Fatal("canonical package reader or original Skill work did not join")
	}
	t.Log("M3_SKILLS_DYNAMIC actual_scheduler_agent_tool_install=true explicit_owner_assignment=true captured_round_unchanged=true successor_list_and_object_read=true model_consumed_canonical_text=true original_app_wait=true external_model=false agent_assign=false runner_prepare=false")
}

func skillsM3AwaitPeer(t *testing.T, v *executionSchedulerApp, network *netfixture.Descriptor, scenario string, requests int) netfixture.State {
	t.Helper()
	ctx, cancel := context.WithTimeout(v.ctx, 30*time.Second)
	defer cancel()
	tick := time.NewTicker(25 * time.Millisecond)
	defer tick.Stop()
	for {
		state, err := network.State(ctx, scenario)
		workAppCheck(t, "skills-observe-original-peer", err)
		if len(state.Requests) >= requests {
			return state
		}
		var terminal bool
		workAppCheck(t, "skills-peer-execution-state", v.admin.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_execution.executions WHERE project_id=$1 AND status IN ('failed','cancelled'))`, v.a.String()).Scan(&terminal))
		if terminal {
			t.Fatalf("Skills original Execution terminated with peer requests=%d want=%d", len(state.Requests), requests)
		}
		select {
		case <-ctx.Done():
			t.Fatalf("Skills peer requests=%d want=%d", len(state.Requests), requests)
		case <-tick.C:
		}
	}
}

func skillsM3Diagnose(t *testing.T, v *executionSchedulerApp, stage string) {
	t.Helper()
	t.Logf("M3 Skills stopped stage=%s", stage)
	if v == nil || v.admin == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	var safe []byte
	err := v.admin.QueryRow(ctx, `SELECT jsonb_build_object(
 'executions',(SELECT COALESCE(jsonb_agg(jsonb_build_object('status',status,'version',version)),'[]'::jsonb) FROM agenteam_execution.executions WHERE project_id::text=$1::text),
 'rounds',(SELECT COALESCE(jsonb_agg(jsonb_build_object('ordinal',r.round_ordinal,'status',r.terminal_status,'reason',r.terminal_reason,'tool_pending',r.tool_pending)),'[]'::jsonb) FROM agenteam_execution.rounds r JOIN agenteam_execution.executions e ON e.id=r.execution_id WHERE e.project_id::text=$1::text),
 'model',(SELECT COALESCE(jsonb_agg(jsonb_build_object('status',final_status,'dispatch',dispatch,'code',error_data->>'code')),'[]'::jsonb) FROM agenteam_model.invocations WHERE project_id::text=$1::text),
 'installations',(SELECT COALESCE(jsonb_agg(jsonb_build_object('phase',phase,'version',version,'reason',safe_reason)),'[]'::jsonb) FROM agenteam_skill.installations WHERE project_id::text=$1::text),
 'work',(SELECT COALESCE(jsonb_agg(jsonb_build_object('kind',kind,'phase',phase)),'[]'::jsonb) FROM agenteam_skill.work WHERE project_id::text=$1::text),
 'object_readers',(SELECT jsonb_build_object('count',count(*),'active',count(*) FILTER(WHERE l.state<>'released')) FROM agenteam_object.object_leases l JOIN agenteam_skill.installations i ON i.object_id=l.object_id WHERE i.project_id::text=$1::text AND l.owner_kind='reader'),
 'package_metadata',(SELECT COALESCE(jsonb_agg(jsonb_build_object('version_matches',r.object_version=o.version,'created_matches',r.object_created_at=o.created_at,'available',o.state='available')),'[]'::jsonb) FROM agenteam_skill.revisions r JOIN agenteam_object.objects o ON o.id=r.object_id WHERE r.project_id::text=$1::text),
 'tool',(SELECT COALESCE(jsonb_agg(jsonb_build_object('phase',phase,'outcome',outcome,'category',convert_from(terminal_data,'UTF8')::jsonb->>'Category','code',convert_from(terminal_data,'UTF8')::jsonb->>'Code')),'[]'::jsonb) FROM agenteam_tool.operations WHERE project_id::text=$1::text))`, v.a.String()).Scan(&safe)
	if err == nil {
		t.Logf("M3 Skills safe durable facts=%s", safe)
	} else {
		t.Logf("M3 Skills diagnostic unavailable type=%T", err)
	}
}

func skillsM3CommitObserver(t *testing.T) func(f.TransactionCause, f.CommitResult) {
	original := m1CommitObserver(t)
	var count atomic.Int32
	return func(cause f.TransactionCause, result f.CommitResult) {
		original(cause, result)
		owner := cause.Details().Owner
		if result.State() != f.NotCommitted || owner != "object" && owner != "skill.runtime-read" || count.Add(1) > 8 {
			return
		}
		if fault := result.Fault(); fault != nil {
			t.Logf("M3 Skills original read transaction rejected owner=%s code=%s outcome=%s", owner, fault.Code.Safe(), fault.CommitState.Safe())
		}
	}
}

func skillsM3ToolReply(t *testing.T, tool id.ToolID, args any, ordinal int) netfixture.WireScenario {
	return skillsM3Reply(t, map[string]any{"role": "assistant", "content": nil, "tool_calls": []any{map[string]any{"id": fmt.Sprintf("skills_call_%d", ordinal), "type": "function", "function": map[string]any{"name": "tool_" + strings.ReplaceAll(tool.String(), "-", ""), "arguments": string(workAppJSON(t, args))}}}}, "tool_calls", ordinal)
}
func skillsM3TextReply(t *testing.T, text string, ordinal int) netfixture.WireScenario {
	return skillsM3Reply(t, map[string]any{"role": "assistant", "content": text}, "stop", ordinal)
}
func skillsM3Reply(t *testing.T, message any, finish string, ordinal int) netfixture.WireScenario {
	return netfixture.WireScenario{Status: 200, Headers: map[string]string{"Content-Type": "application/json", "X-Request-Id": fmt.Sprintf("skills-round-%d", ordinal)}, Chunks: [][]byte{workAppJSON(t, map[string]any{"id": "skills-fixture-response", "object": "chat.completion", "created": 1, "model": "skills-fixture", "choices": []any{map[string]any{"index": 0, "message": message, "finish_reason": finish}}, "usage": map[string]any{"prompt_tokens": 10, "completion_tokens": 10, "total_tokens": 20}})}}
}

func skillsM3ConfigureModel(t *testing.T, v *executionSchedulerApp, endpoint string) mc.ModelID {
	t.Helper()
	secrets, ok := v.root.owned.secret().(*secret.Service)
	if !ok {
		t.Fatal("original Secret Service absent")
	}
	material, err := sc.NewSecretMaterial([]byte("owned-skills-fixture-credential"))
	workAppCheck(t, "skills-secret-material", err)
	identity, err := f.NewCommandIdentity("secret", []string{v.actor.Details().UserID}, "create", f.IdempotencyKey(guardID[struct{}](t).String()))
	workAppCheck(t, "skills-secret-command", err)
	credential, err := secrets.ExecuteWrite(v.ctx, sc.WriteRequest{Actor: v.actor, Scope: id.SystemScope(), Identity: identity, Kind: sc.Create, Purpose: sc.Model, Value: material})
	material.Destroy()
	workAppCheck(t, "skills-secret-create", err)
	var receipt mc.CommandReceipt
	meetingM3SystemCall(t, v, "POST", "model-providers", map[string]any{"input": map[string]any{"name": "Skills fixture Provider", "protocol": mc.OpenAIChat, "base_url": endpoint, "enabled": true, "credential_ref": credential.Metadata.CredentialRef.Details().ID, "options": map[string]any{}}}, &receipt)
	provider, err := f.ParseID[mc.Provider](receipt.ResourceID)
	workAppCheck(t, "skills-provider-id", err)
	meetingM3SystemCall(t, v, "POST", "models", map[string]any{"provider_id": provider, "input": map[string]any{"name": "Skills fixture Model", "provider_model_id": "skills-fixture", "type": mc.ChatModel, "enabled": true, "parameters": map[string]any{}, "request_overwrite": map[string]any{}, "header_overwrite": map[string]string{}, "capabilities": map[string]any{"tool_calls": true, "parallel_tool_calls": false, "streaming": true, "reasoning": false, "input_modalities": []string{"text"}, "output_modalities": []string{"text"}, "reasoning_efforts": []string{}, "structured_output_modes": []string{}, "context_length": nil, "max_output": mc.TokenCount(2048)}}}, &receipt)
	model, err := f.ParseID[mc.Model](receipt.ResourceID)
	workAppCheck(t, "skills-model-id", err)
	return model
}
