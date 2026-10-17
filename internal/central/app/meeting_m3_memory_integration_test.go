//go:build integration && linux

package app

import (
	"context"
	"encoding/json"
	"net/http"
	"net/netip"
	"strings"
	"testing"
	"time"

	ac "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/config"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mt "github.com/LunaDeerTech/agenteam/internal/central/meeting/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/memory"
	mm "github.com/LunaDeerTech/agenteam/internal/central/memory/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	netfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/outbound"
)

func TestMeetingM3MemoryConfiguration(t *testing.T) {
	cfg := testConfig(t, "5s", memoryM3Settings())
	workAppCheck(t, "meeting-memory-complete-config", cfg.Validate())
	runtime, execution := cfg.ExecutionRuntime()
	retrieval, indexed := cfg.RetrievalRuntime()
	if !execution || !runtime.ToolLoop() || !indexed || retrieval.Backend() != "native_pg_fts" {
		t.Fatal("Meeting Memory fixture lost its original execution and retrieval graph")
	}
	_, err := meetingMemoryAgentIntent(guardID[id.Agent](t), guardID[mc.Model](t))
	workAppCheck(t, "meeting-memory-agent-configuration", err)
}

func meetingMemoryAgentIntent(agent id.AgentID, model mc.ModelID) (ac.AgentCreate, error) {
	disabled := false
	return ac.NewAgentCreate(ac.AgentCreateFields{AgentID: agent, Name: "meeting-memory-agent", Description: "Use the original Core memory tools in a meeting", Instructions: "Retain the requested lesson, recall it, reflect on it and publish a concise response.", ModelRef: model, ApprovalPolicy: ac.ApprovalAllow, AllowedToolIDs: []id.ToolID{}, AllowedMountIDs: []id.MountID{}, AllowedSecretVariableIDs: []id.ProjectVariableID{}, AddSkillsEnabled: &disabled, InstallSkillEnabled: &disabled, InjectAgentsMD: false})
}

// Owner HTTP creates one Meeting and one Agent contribution. Its original
// Execution invokes the three existing Core Memory tools through the ordinary
// Builtin/Tool owner. The controlled peers supply only model responses; no
// Task, Memory mutation, synthetic Tool witness, or alternate executor is used.
func TestMeetingM3AppMemoryTools(t *testing.T) {
	TestMeetingM3MemoryConfiguration(t)
	network, err := netfixture.Load()
	workAppCheck(t, "meeting-memory-owned-network", err)
	address, err := netip.ParseAddr(network.PrivateIP)
	workAppCheck(t, "meeting-memory-owned-address", err)
	var transport *outboundRuntime
	v := newExecutionSchedulerAppWith(t, func(d *dependencies, v *executionSchedulerApp) {
		env := guardEnvironment(t, v.root.db)
		for key, value := range memoryM3Settings() {
			env = append(env, "AGENTEAM_CENTRAL_"+key+"="+value)
		}
		env = append(env, "AGENTEAM_CENTRAL_OUTBOUND_CA_FILE="+network.CAFile)
		v.root.cfg = guardConfiguration(t, env)
		v.store.observeCommit = m1CommitObserver(t)
		d.outboundConstruct = func(_ context.Context, _ config.Config, actual *outboundRuntime) error {
			actual.resolver = appFixtureResolver{address}
			transport = actual
			return nil
		}
	})
	if transport == nil || v.runtime.memory == nil || v.runtime.memories == nil || v.runtime.meetings == nil || v.runtime.meetingRuntime == nil {
		t.Fatal("Meeting and Memory original App graph missing")
	}
	meetingM3VerifyOwnedDatabase(t, v)
	m1AllowOwnedPeer(t, v, transport, network)
	w := &m1Workflow{app: v, network: network}
	stage := "registered-core-tools"
	defer func() {
		if t.Failed() {
			memoryM3Diagnose(t, v, stage)
		}
	}()
	toolIDs := make(map[string]id.ToolID)
	for _, key := range []string{"builtin:retain", "builtin:recall", "builtin:reflect"} {
		var raw string
		workAppCheck(t, "meeting-memory-tool-registration", v.admin.QueryRow(v.ctx, `SELECT tool_id FROM agenteam_tool.identities WHERE stable_key=$1`, key).Scan(&raw))
		toolIDs[key], err = f.ParseID[id.Tool](raw)
		workAppCheck(t, "meeting-memory-tool-id", err)
	}
	const final = "I retained the migration lesson, recalled it, and reflected on its rollback checks."
	agentScenario, err := network.Create(v.ctx, netfixture.ScenarioConfig{Mode: "openai_chat_wire", WireSequence: []netfixture.WireScenario{
		memoryM3ToolReply(t, toolIDs["builtin:retain"], map[string]any{"content": memoryM3Fact}, 1),
		memoryM3ToolReply(t, toolIDs["builtin:recall"], map[string]any{"query": "schema migration rollback"}, 2),
		memoryM3ToolReply(t, toolIDs["builtin:reflect"], map[string]any{"query": "How should migrations be checked?"}, 3),
		memoryM3TextReply(t, final, 4),
	}})
	workAppCheck(t, "meeting-memory-agent-peer", err)
	content := map[string]any{"type": "procedure", "text": memoryM3Fact, "tags": []string{"migration"}, "importance": "normal"}
	extraction := string(workAppJSON(t, map[string]any{"candidates": []any{content}}))
	consolidation := string(workAppJSON(t, map[string]any{"actions": []any{map[string]any{"action": "ADD", "target_memory_id": "", "expected_revision": "", "content": content, "reason": "Reusable migration procedure"}}}))
	summaryText := string(workAppJSON(t, map[string]string{"title": "Migration memory", "goals": "Remember migration checks", "decisions": "Use the retained procedure", "unresolved": "", "facts": "The same participant retained, recalled and reflected on a lesson"}))
	memoryScenario, err := network.Create(v.ctx, netfixture.ScenarioConfig{Mode: "openai_chat_wire", WireSequence: []netfixture.WireScenario{
		memoryM3TextReply(t, extraction, 1), memoryM3TextReply(t, consolidation, 2), memoryM3TextReply(t, memoryM3Reflection, 3), meetingM3Reply(t, 4, summaryText),
	}})
	workAppCheck(t, "meeting-memory-structured-peer", err)
	embeddingWire := netfixture.WireScenario{Status: http.StatusOK, Headers: map[string]string{"Content-Type": "application/json"}, Chunks: [][]byte{workAppJSON(t, map[string]any{
		"object": "list", "model": "memory-embedding", "data": []any{map[string]any{"object": "embedding", "index": 0, "embedding": []float64{1, 0}}}, "usage": map[string]any{"prompt_tokens": 10, "total_tokens": 10},
	})}}
	embeddingScenario, err := network.Create(v.ctx, netfixture.ScenarioConfig{Mode: "openai_embeddings_wire", Wire: &embeddingWire})
	workAppCheck(t, "meeting-memory-embedding-peer", err)
	stage = "owner-model-configuration"
	agentModel := memoryM3ConfigureModels(t, v, agentScenario, memoryScenario, embeddingScenario)
	// Memory and Meeting now consume the same Summary model and controlled peer.
	agent := guardID[id.Agent](t)
	intent, err := meetingMemoryAgentIntent(agent, agentModel)
	workAppCheck(t, "meeting-memory-agent-intent", err)
	_, err = v.assembly.agents.service.CreateAgent(v.ctx, v.actor, executionSchedulerAppMeta(t, nil), v.a, intent)
	workAppCheck(t, "meeting-memory-agent-create", err)
	v.store.releaseDirectory()
	stage = "owner-start"
	var receipt mt.Receipt
	meetingMemoryHTTP(t, v, http.MethodPost, "/meetings", map[string]any{"agents": []id.AgentID{agent}, "mode": "sequential", "content_nodes": []mt.InlineNode{{Type: "text", Text: "Retain a migration lesson, recall it, and reflect on its rollback checks."}}}, &receipt)
	if receipt.MeetingID.Validate() != nil || receipt.TurnID == nil {
		t.Fatal("Meeting Memory Owner Start lost its original identity")
	}
	stage = "same-execution-tools-and-summary"
	detail := meetingM3AwaitCompleted(t, v, receipt, nil)
	meetingMemoryHTTP(t, v, http.MethodGet, "/meetings/"+receipt.MeetingID.String(), nil, &detail)
	if len(detail.Turns) != 1 || len(detail.Turns[0].Contributions) != 2 || detail.Session.Summary == nil || detail.Session.Summary.Version != 1 || detail.Session.Summary.PendingUpdate {
		t.Fatal("Meeting Memory did not publish one complete contribution and Summary")
	}
	contribution := detail.Turns[0].Contributions[1]
	if contribution.ExecutionID == nil || contribution.Status != "completed" || contribution.Message == nil || len(contribution.Message.Content) != 1 || contribution.Message.Content[0].Text != final {
		t.Fatal("Memory tools did not return to the original public contribution")
	}
	original := *contribution.ExecutionID
	page, err := v.runtime.memories.List(v.ctx, v.actor, mm.Namespace{ProjectID: v.a, AgentID: agent}, memory.ListQuery{})
	workAppCheck(t, "meeting-memory-owner-list", err)
	if len(page.Memories) != 1 || page.Memories[0].Current.Text != memoryM3Fact || page.Memories[0].Current.Revision != 1 || page.Memories[0].Current.Provenance.ExecutionID != original {
		t.Fatal("Meeting Memory lost its original namespace, revision or Execution provenance")
	}
	stage = "original-source-durable-facts"
	var executions, tasks, results, operations, memoryCalls, retiredMemoryCalls, retainAudit, models, retiredModels, releasedModels int
	var source bool
	err = v.admin.QueryRow(v.ctx, `SELECT
 (SELECT count(*) FROM agenteam_execution.executions WHERE project_id::text=$1::text),
 (SELECT count(*) FROM agenteam_work.tasks WHERE project_id::text=$1::text),
 (SELECT count(*) FROM agenteam_execution.loop_tool_results WHERE execution_id::text=$3::text),
 (SELECT count(*) FROM agenteam_memory.operations WHERE project_id::text=$1::text AND agent_id::text=$4::text AND execution_id::text=$3::text AND phase='committed'),
 (SELECT count(*) FROM agenteam_memory.model_calls WHERE project_id::text=$1::text AND agent_id::text=$4::text AND execution_id::text=$3::text),
 (SELECT count(*) FROM agenteam_memory.model_calls WHERE project_id::text=$1::text AND agent_id::text=$4::text AND execution_id::text=$3::text AND retired),
 (SELECT count(*) FROM agenteam_audit.audit_records WHERE project_id::text=$1::text AND action='memory.retain'),
 (SELECT count(*) FROM agenteam_model.calls WHERE project_id::text=$1::text),
 (SELECT count(*) FROM agenteam_model.calls WHERE project_id::text=$1::text AND retired),
 (SELECT count(*) FROM agenteam_model.calls c JOIN agenteam_secret.secret_leases l ON l.id=c.lease_id WHERE c.project_id::text=$1::text AND l.released),
 EXISTS(SELECT 1 FROM agenteam_execution.executions WHERE id::text=$3::text AND project_id::text=$1::text AND agent_id::text=$4::text AND status='succeeded' AND launch_request->>'purpose'='meeting/response' AND launch_request#>>'{trigger,kind}'='meeting' AND launch_request#>>'{trigger,meeting_id}'=$2::text AND launch_request#>>'{trigger,turn_id}'=$5::text AND launch_request#>>'{trigger,contribution_id}'=$6::text AND launch_request#>>'{trigger,participant_id}'=$7::text AND NOT (launch_request->'trigger' ? 'task_id'))`, v.a.String(), receipt.MeetingID.String(), original.String(), agent.String(), receipt.TurnID.String(), contribution.ID.String(), contribution.ParticipantID.String()).Scan(&executions, &tasks, &results, &operations, &memoryCalls, &retiredMemoryCalls, &retainAudit, &models, &retiredModels, &releasedModels, &source)
	workAppCheck(t, "meeting-memory-original-durable-source", err)
	if executions != 1 || tasks != 0 || results != 3 || operations != 1 || memoryCalls != 6 || retiredMemoryCalls != 6 || retainAudit != 1 || models != 11 || retiredModels != 11 || releasedModels != 11 || !source {
		t.Fatalf("Meeting Memory facts executions=%d tasks=%d results=%d operations=%d memory_calls=%d memory_retired=%d retain_audit=%d models=%d retired=%d released=%d source=%t", executions, tasks, results, operations, memoryCalls, retiredMemoryCalls, retainAudit, models, retiredModels, releasedModels, source)
	}
	stage = "original-join"
	v.runtime.StopAdmission()
	drain, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	err = v.runtime.Drain(drain)
	cancel()
	workAppCheck(t, "meeting-memory-original-drain", err)
	v.requestStop()
	usageAppWait(t, v.root.done, "meeting-memory-original-app-return")
	workAppCheck(t, "meeting-memory-original-app-exit", v.root.err)
	if !v.runtime.Joined() || !v.runtime.memory.Joined() || !v.runtime.meetingRuntime.Joined() || !v.runtime.executionCalls.Joined() || !v.assembly.Joined() || !v.root.owned.producersJoined() {
		t.Fatal("Meeting Memory App returned before all original owners joined")
	}
	state := w.joinedPeer(t, agentScenario, 4, 4)
	w.joinedPeer(t, memoryScenario, 4, 4)
	w.joinedPeer(t, embeddingScenario, 3, 3)
	for index, want := range map[int]string{1: `"added"`, 2: memoryM3Fact, 3: memoryM3Reflection} {
		if !strings.Contains(memoryM3LastTool(t, state.Requests[index].Body), want) {
			t.Fatalf("Meeting Agent did not receive its original Memory result at round=%d", index+1)
		}
	}
	meetingDeletionGuardReleased(t, v.workAppFixture)
	t.Log("M3_MEETING_MEMORY actual_owner_http=true meeting_source_same_execution=true retain_recall_reflect=true canonical_provenance=true no_task=true summary_published=true original_app_joined=true controlled_model_peers=true external_model=false")
}

func meetingMemoryHTTP(t *testing.T, v *executionSchedulerApp, method, path string, body, out any) {
	t.Helper()
	var raw []byte
	if body != nil {
		raw = workAppJSON(t, body)
	}
	response, err := v.request(v.ctx, method, "/api/v1/projects/"+v.a.String()+path, raw, guardID[struct{}](t).String())
	clear(raw)
	workAppCheck(t, "meeting-memory-owner-http", err)
	defer clear(response.body)
	if response.status != http.StatusOK && response.status != http.StatusCreated {
		var problem struct {
			Code f.Code `json:"code"`
		}
		_ = json.Unmarshal(response.body, &problem)
		t.Fatalf("Meeting Memory HTTP status=%d code=%s", response.status, problem.Code.Safe())
	}
	workAppCheck(t, "meeting-memory-owner-response", json.Unmarshal(response.body, out))
}
