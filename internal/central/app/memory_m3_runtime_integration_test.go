//go:build integration && linux

package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net/netip"
	"strings"
	"syscall"
	"testing"
	"time"

	ac "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/config"
	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/memory"
	mm "github.com/LunaDeerTech/agenteam/internal/central/memory/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/secret"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
	wc "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
	netfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/outbound"
)

const memoryM3Fact = "For schema migrations, create a reversible change and verify rollback before deployment."
const memoryM3Reflection = "Use a reversible migration and verify its rollback before deployment."

func memoryM3Settings() map[string]string {
	runtime := strings.Replace(executionSchedulerAppConfig, "direct-text-v1", "tool-loop-v1", 1)
	// Keep completed Task history from becoming an unrelated re-launch during
	// this bounded test. The original Scheduler still discovers every new Task.
	runtime = strings.Replace(runtime, `"relaunch_skip_count":0`, `"relaunch_skip_count":100000`, 1)
	return map[string]string{"PUBLIC_ORIGIN": "http://localhost:8080", "SHUTDOWN_TIMEOUT": "5s", "EXECUTION_RUNTIME": runtime, "RETRIEVAL_RUNTIME": `{"backend":"native_pg_fts","dimensions":2}`, "SCHEDULER_LAUNCH_RETRY_MAX_ATTEMPTS": "2", "SCHEDULER_LAUNCH_RETRY_INITIAL_BACKOFF": "100ms", "SCHEDULER_LAUNCH_RETRY_MAX_BACKOFF": "1s"}
}
func TestMemoryM3AppConfiguration(t *testing.T) {
	cfg := testConfig(t, "5s", memoryM3Settings())
	workAppCheck(t, "memory-complete-config", cfg.Validate())
	runtime, enabled := cfg.ExecutionRuntime()
	retrieval, indexed := cfg.RetrievalRuntime()
	_, retry := cfg.SchedulerLaunchRetryPolicy()
	if !enabled || !runtime.ToolLoop() || !indexed || retrieval.Dimensions() != 2 || !retry {
		t.Fatal("Memory fixture lost its complete deployed graph")
	}
}

// Only the HTTP model peer is deterministic. Original Scheduler/Coordinator,
// AgentRun, Unified Tool, Memory, Model, Secret, Usage and Audit are exercised.
// No direct Memory mutation, synthetic Execution, or successful Tool witness
// is used to create a memory. Owner deletion uses the real HTTP boundary.
func TestMemoryM3AppCrossExecution(t *testing.T) {
	memoryM3CrossExecution(t, false)
}

func TestMemoryM3AppEmbeddingProfileSwitch(t *testing.T) {
	memoryM3CrossExecution(t, true)
}

// The optional workflow shares this exact App/database and the configured
// embedding selector without replacing the original Memory authorities.
type memoryM3CrossExecutionOptions struct {
	RetrievalBackend        string
	BeforeRun               func(*testing.T, *dependencies, *executionSchedulerApp)
	AfterModelConfiguration func(*testing.T, *executionSchedulerApp, *netfixture.Descriptor, string)
	ExtraEmbeddingRequests  int
}

func memoryM3CrossExecution(t *testing.T, switchProfile bool) {
	t.Helper()
	memoryM3CrossExecutionConfigured(t, switchProfile, memoryM3CrossExecutionOptions{})
}

func memoryM3CrossExecutionWith(t *testing.T, options memoryM3CrossExecutionOptions) {
	t.Helper()
	memoryM3CrossExecutionConfigured(t, false, options)
}

func memoryM3CrossExecutionConfigured(t *testing.T, switchProfile bool, options memoryM3CrossExecutionOptions) {
	t.Helper()
	TestMemoryM3AppConfiguration(t)
	if options.RetrievalBackend != "" && options.RetrievalBackend != "pg_search" || options.ExtraEmbeddingRequests < 0 || options.ExtraEmbeddingRequests > 2 || options.AfterModelConfiguration == nil && options.ExtraEmbeddingRequests != 0 {
		t.Fatal("unsupported Memory shared workflow options")
	}
	settings := memoryM3Settings()
	if options.RetrievalBackend != "" {
		settings["RETRIEVAL_RUNTIME"] = string(workAppJSON(t, map[string]any{"backend": options.RetrievalBackend, "dimensions": 2}))
		workAppCheck(t, "memory-shared-config", testConfig(t, "5s", settings).Validate())
	}
	network, err := netfixture.Load()
	workAppCheck(t, "memory-owned-network", err)
	address, err := netip.ParseAddr(network.PrivateIP)
	workAppCheck(t, "memory-owned-peer-address", err)
	var transport *outboundRuntime
	v := newExecutionSchedulerAppWith(t, func(d *dependencies, v *executionSchedulerApp) {
		env := guardEnvironment(t, v.root.db)
		for key, value := range settings {
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
		if options.BeforeRun != nil {
			options.BeforeRun(t, d, v)
		}
	})
	stage := "app"
	defer func() {
		if t.Failed() {
			memoryM3Diagnose(t, v, stage)
		}
	}()
	if transport == nil || v.runtime.memory == nil || v.runtime.memories == nil {
		t.Fatal("Memory original App graph missing")
	}
	m1AllowOwnedPeer(t, v, transport, network)
	keys := []string{"builtin:retain", "builtin:recall", "builtin:reflect"}
	tools := map[string]id.ToolID{}
	for _, key := range keys {
		var raw string
		workAppCheck(t, "memory-tool-registration", v.admin.QueryRow(v.ctx, `SELECT tool_id FROM agenteam_tool.identities WHERE stable_key=$1`, key).Scan(&raw))
		tools[key], err = f.ParseID[id.Tool](raw)
		workAppCheck(t, "memory-tool-id", err)
	}
	agentSequence := []netfixture.WireScenario{
		memoryM3ToolReply(t, tools["builtin:retain"], map[string]any{"content": memoryM3Fact}, 1), memoryM3TextReply(t, "Retained the lesson.", 2),
		memoryM3ToolReply(t, tools["builtin:recall"], map[string]any{"query": "schema migration rollback"}, 3), memoryM3ToolReply(t, tools["builtin:reflect"], map[string]any{"query": "How should migrations be checked?"}, 4), memoryM3TextReply(t, "Read and reflected on the lesson.", 5),
		memoryM3ToolReply(t, tools["builtin:recall"], map[string]any{"query": "schema migration rollback"}, 6), memoryM3TextReply(t, "No memory in this namespace.", 7),
		memoryM3ToolReply(t, tools["builtin:recall"], map[string]any{"query": "schema migration rollback"}, 8), memoryM3TextReply(t, "The deleted memory is absent.", 9),
	}
	agentScenario, err := network.Create(v.ctx, netfixture.ScenarioConfig{Mode: "openai_chat_wire", WireSequence: agentSequence})
	workAppCheck(t, "memory-agent-peer", err)
	content := map[string]any{"type": "procedure", "text": memoryM3Fact, "tags": []string{"migration"}, "importance": "normal"}
	extraction := string(workAppJSON(t, map[string]any{"candidates": []any{content}}))
	consolidation := string(workAppJSON(t, map[string]any{"actions": []any{map[string]any{"action": "ADD", "target_memory_id": "", "expected_revision": "", "content": content, "reason": "Reusable migration procedure"}}}))
	memoryScenario, err := network.Create(v.ctx, netfixture.ScenarioConfig{Mode: "openai_chat_wire", WireSequence: []netfixture.WireScenario{memoryM3TextReply(t, extraction, 1), memoryM3TextReply(t, consolidation, 2), memoryM3TextReply(t, memoryM3Reflection, 3)}})
	workAppCheck(t, "memory-structured-peer", err)
	embeddingWire := netfixture.WireScenario{Status: 200, Headers: map[string]string{"Content-Type": "application/json"}, Chunks: [][]byte{workAppJSON(t, map[string]any{"object": "list", "model": "memory-embedding", "data": []any{map[string]any{"object": "embedding", "index": 0, "embedding": []float64{1, 0}}}, "usage": map[string]any{"prompt_tokens": 10, "total_tokens": 10}})}}
	embeddingScenario, err := network.Create(v.ctx, netfixture.ScenarioConfig{Mode: "openai_embeddings_wire", Wire: &embeddingWire})
	workAppCheck(t, "memory-embedding-peer", err)
	stage = "model-configuration"
	agentModel := memoryM3ConfigureModels(t, v, agentScenario, memoryScenario, embeddingScenario)
	if options.AfterModelConfiguration != nil {
		stage = "shared-knowledge-workflow"
		options.AfterModelConfiguration(t, v, network, embeddingScenario)
	}
	agents := make([]id.AgentID, 2)
	// Memory is a Core Tool captured by Execution. Agent configuration only
	// accepts optional Ordinary Tool references; Core IDs must not enter it.
	disabled := false
	for n := range agents {
		agents[n] = guardID[id.Agent](t)
		intent, err := ac.NewAgentCreate(ac.AgentCreateFields{AgentID: agents[n], Name: fmt.Sprintf("memory-agent-%d", n+1), Description: "Memory integration participant", Instructions: "Use the explicitly requested memory tool and report the result.", ModelRef: agentModel, ApprovalPolicy: ac.ApprovalAllow, AllowedToolIDs: []id.ToolID{}, AllowedMountIDs: []id.MountID{}, AllowedSecretVariableIDs: []id.ProjectVariableID{}, AddSkillsEnabled: &disabled, InstallSkillEnabled: &disabled, InjectAgentsMD: false})
		workAppCheck(t, "memory-agent-intent", err)
		_, err = v.assembly.agents.service.CreateAgent(v.ctx, v.actor, executionSchedulerAppMeta(t, nil), v.a, intent)
		workAppCheck(t, "memory-agent-create", err)
	}
	sprint := memoryM3CreateSprint(t, v)
	v.agent = agents[0]
	stage = "first-retain"
	firstTask := memoryM3CreateTask(t, v, sprint.ID, agents[0], "Explicitly retain the durable migration lesson.")
	_, err = v.planning.sprints.StartSprint(v.ctx, v.actor, executionSchedulerAppMeta(t, &sprint.Version), v.a, sprint.ID)
	workAppCheck(t, "memory-start-sprint", err)
	v.store.releaseDirectory()
	first := memoryM3AwaitExecution(t, v, firstTask.ID)
	namespace := mm.Namespace{ProjectID: v.a, AgentID: agents[0]}
	page, err := v.runtime.memories.List(v.ctx, v.actor, namespace, memory.ListQuery{})
	workAppCheck(t, "memory-owner-current-list", err)
	if len(page.Memories) != 1 || page.Memories[0].Current.Text != memoryM3Fact {
		t.Fatal("actual retain did not publish the canonical lesson")
	}
	record := page.Memories[0]
	if record.Current.Provenance.ExecutionID != first {
		t.Fatal("retained memory lost original Execution provenance")
	}
	var serving int
	workAppCheck(t, "memory-first-serving-reference", v.admin.QueryRow(v.ctx, `SELECT count(*) FROM agenteam_memory.namespaces n JOIN agenteam_memory.generations g ON g.generation_id=n.serving_generation_id JOIN agenteam_model.serving_snapshot_retentions r ON r.generation_id=g.generation_id AND r.snapshot_id=g.snapshot_id WHERE n.project_id=$1 AND n.agent_id=$2 AND g.status='ready' AND g.retention_state='retained' AND r.state='retained' AND r.consumer_kind='memory'`, v.a.String(), agents[0].String()).Scan(&serving))
	if serving != 1 {
		t.Fatal("canonical retain did not complete its first serving reference")
	}
	var previousGeneration, replacementScenario string
	var replacementModel mc.ModelID
	if switchProfile {
		stage = "embedding-profile-switch"
		workAppCheck(t, "memory-original-generation", v.admin.QueryRow(v.ctx, `SELECT serving_generation_id::text FROM agenteam_memory.namespaces WHERE project_id=$1 AND agent_id=$2`, v.a.String(), agents[0].String()).Scan(&previousGeneration))
		replacementWire := netfixture.WireScenario{Status: 200, Headers: map[string]string{"Content-Type": "application/json"}, Chunks: [][]byte{workAppJSON(t, map[string]any{"object": "list", "model": "memory-replacement", "data": []any{map[string]any{"object": "embedding", "index": 0, "embedding": []float64{0, 1}}}, "usage": map[string]any{"prompt_tokens": 10, "total_tokens": 10}})}}
		replacementScenario, err = network.Create(v.ctx, netfixture.ScenarioConfig{Mode: "openai_embeddings_wire", Wire: &replacementWire})
		workAppCheck(t, "memory-replacement-peer", err)
		replacementModel = memoryM3ReplaceEmbedding(t, v, replacementScenario)
		var current string
		workAppCheck(t, "memory-old-serving-before-recall", v.admin.QueryRow(v.ctx, `SELECT serving_generation_id::text FROM agenteam_memory.namespaces WHERE project_id=$1 AND agent_id=$2`, v.a.String(), agents[0].String()).Scan(&current))
		if current != previousGeneration {
			t.Fatal("configuration change replaced the old space before a complete Memory build")
		}
	}
	stage = "cross-execution-recall-reflect"
	secondTask := memoryM3CreateTask(t, v, sprint.ID, agents[0], "Recall the migration lesson and reflect on the checks.")
	second := memoryM3AwaitExecution(t, v, secondTask.ID)
	if first == second {
		t.Fatal("cross-execution test reused the original Execution")
	}
	page, err = v.runtime.memories.List(v.ctx, v.actor, namespace, memory.ListQuery{})
	workAppCheck(t, "memory-readonly-after-reflect", err)
	if len(page.Memories) != 1 || page.Memories[0].Current.Revision != record.Current.Revision {
		t.Fatal("recall or reflection changed canonical memory")
	}
	if switchProfile {
		memoryM3ProfileFacts(t, v, agents[0], previousGeneration, replacementModel)
	}
	stage = "foreign-agent-recall"
	thirdTask := memoryM3CreateTask(t, v, sprint.ID, agents[1], "Recall migration lessons from your own namespace.")
	third := memoryM3AwaitExecution(t, v, thirdTask.ID)
	stage = "owner-delete"
	path := "/api/v1/projects/" + v.a.String() + "/agents/" + agents[0].String() + "/memory/" + record.Current.MemoryID.String() + "/delete"
	response, err := v.request(v.ctx, "POST", path, workAppJSON(t, map[string]any{"expected_revision": record.Current.Revision}), guardID[struct{}](t).String())
	workAppCheck(t, "memory-owner-http-delete", err)
	if response.status != 200 {
		t.Fatalf("memory delete HTTP status=%d", response.status)
	}
	stage = "deleted-recall"
	fourthTask := memoryM3CreateTask(t, v, sprint.ID, agents[0], "Recall after the Owner deleted the memory.")
	fourth := memoryM3AwaitExecution(t, v, fourthTask.ID)
	expectedCalls := 6
	if switchProfile {
		expectedCalls++
	}
	memoryM3Facts(t, v, agents[0], record.Current.MemoryID, []id.ExecutionID{first, second, third, fourth}, expectedCalls)
	stage = "original-join"
	v.runtime.StopAdmission()
	drain, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	err = v.runtime.Drain(drain)
	cancel()
	workAppCheck(t, "memory-original-runtime-drain", err)
	v.requestStop()
	usageAppWait(t, v.root.done, "memory-original-app-return")
	workAppCheck(t, "memory-original-app-exit", v.root.err)
	if !v.runtime.Joined() || !v.runtime.memory.Joined() || !v.assembly.Joined() || !v.root.owned.producersJoined() {
		t.Fatal("Memory App returned before original owners joined")
	}
	peer := &m1Workflow{app: v, network: network}
	state := peer.joinedPeer(t, agentScenario, 9, 9)
	peer.joinedPeer(t, memoryScenario, 3, 3)
	if switchProfile {
		peer.joinedPeer(t, embeddingScenario, 1, 1)
		peer.joinedPeer(t, replacementScenario, 3, 3)
	} else {
		peer.joinedPeer(t, embeddingScenario, 3+options.ExtraEmbeddingRequests, 3+options.ExtraEmbeddingRequests)
	}
	for index, want := range map[int]string{1: `"added"`, 3: memoryM3Fact, 4: memoryM3Reflection, 6: `"memories":[]`, 8: `"memories":[]`} {
		text := memoryM3LastTool(t, state.Requests[index].Body)
		if !strings.Contains(text, want) {
			t.Fatalf("Agent did not receive actual tool response at round=%d", index+1)
		}
		if (index == 6 || index == 8) && strings.Contains(text, memoryM3Fact) {
			t.Fatal("foreign or deleted memory leaked into tool output")
		}
	}
	t.Log("MEMORY_MODEL_PEERS_JOINED expected_requests=true original_tool_output_consumed=true")
	if options.RetrievalBackend == "pg_search" {
		var recall mm.RecallResult
		workAppCheck(t, "memory-hybrid-recall-wire", json.Unmarshal([]byte(memoryM3LastTool(t, state.Requests[3].Body)), &recall))
		if recall.Mode != "hybrid" || recall.Degraded || len(recall.Memories) != 1 || recall.Memories[0].MemoryID != record.Current.MemoryID {
			t.Fatal("Memory original Tool did not deliver a complete hybrid recall")
		}
		t.Log("M3_MEMORY_HYBRID original_tool_result=true mode=hybrid degraded=false canonical_memory=true")
	}
	stage = "guard-exit-observation"
	memoryM3GuardReleased(t, v.workAppFixture)
	t.Log("M3_MEMORY_CROSS_EXECUTION actual_scheduler_agent_tool_model_audit=true retain_recall_reflect_owner_delete=true foreign_namespace_empty=true serving_reference=true model_peer_deterministic=true external_model=false original_app_wait=true")
	if switchProfile {
		t.Log("M3_MEMORY_PROFILE_SWITCH system_selector_changed=true next_execution_rebuilt=true old_reference_released=true canonical_unchanged=true query_uses_new_snapshot=true external_model=false")
	}
}

// The actual App Wait/Joined and original flock remain prerequisite gates.
// A closed client socket and the server backend's observed disappearance are
// distinct events, so observe the latter within a small part of the same budget.
func memoryM3GuardReleased(t *testing.T, v *workAppFixture) {
	t.Helper()
	workAppCheck(t, "memory-original-guard-release", syscall.Flock(int(v.claim.Fd()), syscall.LOCK_EX|syscall.LOCK_NB))
	workAppCheck(t, "memory-guard-observer-unlock", syscall.Flock(int(v.claim.Fd()), syscall.LOCK_UN))
	ctx, cancel := context.WithTimeout(v.ctx, 2*time.Second)
	defer cancel()
	tick := time.NewTicker(20 * time.Millisecond)
	defer tick.Stop()
	observations := 0
	var stopped bool
	var connections int
	for {
		err := v.admin.QueryRow(ctx, `SELECT coalesce((SELECT state='stopped' AND stopped_at IS NOT NULL FROM agenteam_object.process_claims WHERE process_id=$1::uuid),false),(SELECT count(*) FROM pg_stat_activity WHERE datname=current_database() AND application_name='agenteam')`, v.objects.process.String()).Scan(&stopped, &connections)
		if err != nil {
			t.Logf("MEMORY_EXIT_FACTS stopped=%t database_gone=%t connections=%d observations=%d observed=%t observation_failed=true", stopped, connections == 0, connections, observations, observations > 0)
			workAppCheck(t, "memory-original-exit-observation", err)
		}
		observations++
		if observations == 1 || stopped && connections == 0 {
			t.Logf("MEMORY_EXIT_FACTS stopped=%t database_gone=%t connections=%d observations=%d", stopped, connections == 0, connections, observations)
		}
		if stopped && connections == 0 {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatalf("Memory original exit facts unconfirmed: stopped=%t database_gone=%t connections=%d observations=%d", stopped, connections == 0, connections, observations)
		case <-tick.C:
		}
	}
}

func memoryM3TextReply(t *testing.T, text string, n int) netfixture.WireScenario {
	return memoryM3Reply(t, map[string]any{"role": "assistant", "content": text}, "stop", n)
}
func memoryM3ToolReply(t *testing.T, tool id.ToolID, args any, n int) netfixture.WireScenario {
	return memoryM3Reply(t, map[string]any{"role": "assistant", "content": nil, "tool_calls": []any{map[string]any{"id": fmt.Sprintf("memory_call_%d", n), "type": "function", "function": map[string]any{"name": "tool_" + strings.ReplaceAll(tool.String(), "-", ""), "arguments": string(workAppJSON(t, args))}}}}, "tool_calls", n)
}
func memoryM3Reply(t *testing.T, message any, finish string, n int) netfixture.WireScenario {
	return netfixture.WireScenario{Status: 200, Headers: map[string]string{"Content-Type": "application/json", "X-Request-Id": fmt.Sprintf("memory-round-%d", n)}, Chunks: [][]byte{workAppJSON(t, map[string]any{"id": "memory-fixture-response", "object": "chat.completion", "created": 1, "model": "memory-fixture", "choices": []any{map[string]any{"index": 0, "message": message, "finish_reason": finish}}, "usage": map[string]any{"prompt_tokens": 10, "completion_tokens": 10, "total_tokens": 20}})}}
}
func memoryM3LastTool(t *testing.T, body string) string {
	t.Helper()
	var request struct {
		Messages []struct {
			Role    string          `json:"role"`
			Content json.RawMessage `json:"content"`
		} `json:"messages"`
	}
	// OpenAI permits either a string or text content parts on a Tool message.
	workAppCheck(t, "memory-peer-request-decode", json.Unmarshal([]byte(body), &request))
	for n := len(request.Messages) - 1; n >= 0; n-- {
		if request.Messages[n].Role == "tool" {
			var text string
			if json.Unmarshal(request.Messages[n].Content, &text) == nil {
				return text
			}
			var parts []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			}
			workAppCheck(t, "memory-peer-tool-content", json.Unmarshal(request.Messages[n].Content, &parts))
			var joined strings.Builder
			if len(parts) == 0 {
				t.Fatal("original Tool message lost its text content")
			}
			for _, part := range parts {
				if part.Type != "text" {
					t.Fatal("original Tool message contained unexpected nontext part")
				}
				joined.WriteString(part.Text)
			}
			return joined.String()
		}
	}
	t.Fatal("original Model request has no Tool result")
	return ""
}

func TestMemoryM3ToolContent(t *testing.T) {
	for _, content := range []any{`{"memories":[]}`, []any{map[string]any{"type": "text", "text": `{"memories":[]}`}}} {
		raw := workAppJSON(t, map[string]any{"messages": []any{
			map[string]any{"role": "assistant", "content": nil},
			map[string]any{"role": "tool", "content": content},
		}})
		if memoryM3LastTool(t, string(raw)) != `{"memories":[]}` {
			t.Fatal("original native Tool content changed during assertion")
		}
	}
}

func memoryM3ConfigureModels(t *testing.T, v *executionSchedulerApp, agentScenario, memoryScenario, embeddingScenario string) mc.ModelID {
	t.Helper()
	secrets, ok := v.root.owned.secret().(*secret.Service)
	if !ok {
		t.Fatal("original Secret absent")
	}
	material, err := sc.NewSecretMaterial([]byte("owned-memory-fixture-credential"))
	workAppCheck(t, "memory-secret-material", err)
	identity, err := f.NewCommandIdentity("secret", []string{v.actor.Details().UserID}, "create", f.IdempotencyKey(guardID[struct{}](t).String()))
	workAppCheck(t, "memory-secret-command", err)
	credential, err := secrets.ExecuteWrite(v.ctx, sc.WriteRequest{Actor: v.actor, Scope: id.SystemScope(), Identity: identity, Kind: sc.Create, Purpose: sc.Model, Value: material})
	material.Destroy()
	workAppCheck(t, "memory-secret-create", err)
	models := make([]mc.ModelID, 3)
	for n, scenario := range []string{agentScenario, memoryScenario, embeddingScenario} {
		protocol, kind, output := mc.OpenAIChat, mc.ChatModel, "text"
		if n == 2 {
			protocol, kind, output = mc.OpenAIEmbeddings, mc.EmbeddingModel, "vector"
		}
		var receipt mc.CommandReceipt
		meetingM3SystemCall(t, v, "POST", "model-providers", map[string]any{"input": map[string]any{"name": fmt.Sprintf("Memory fixture provider %d", n), "protocol": protocol, "base_url": "https://fixture.test:8443/case/" + scenario, "enabled": true, "credential_ref": credential.Metadata.CredentialRef.Details().ID, "options": map[string]any{}}}, &receipt)
		provider, err := f.ParseID[mc.Provider](receipt.ResourceID)
		workAppCheck(t, "memory-provider-id", err)
		capabilities := map[string]any{"tool_calls": n == 0, "parallel_tool_calls": false, "streaming": n != 2, "reasoning": false, "input_modalities": []string{"text"}, "output_modalities": []string{output}, "reasoning_efforts": []string{}, "structured_output_modes": []string{}, "context_length": nil, "max_output": nil}
		if n == 1 {
			capabilities["structured_output_modes"] = []string{"text", "json_schema"}
		}
		meetingM3SystemCall(t, v, "POST", "models", map[string]any{"provider_id": provider, "input": map[string]any{"name": fmt.Sprintf("Memory fixture model %d", n), "provider_model_id": "memory-fixture", "type": kind, "enabled": true, "parameters": map[string]any{}, "request_overwrite": map[string]any{}, "header_overwrite": map[string]string{}, "capabilities": capabilities}}, &receipt)
		models[n], err = f.ParseID[mc.Model](receipt.ResourceID)
		workAppCheck(t, "memory-model-id", err)
	}
	var selection struct {
		ID      string    `json:"id"`
		Version f.Version `json:"version"`
	}
	meetingM3SystemCall(t, v, "GET", "model-selection", nil, &selection)
	var receipt mc.CommandReceipt
	meetingM3SystemCall(t, v, "PUT", "model-selection", map[string]any{"id": selection.ID, "expected_version": selection.Version, "embedding": models[2], "memory": models[1], "reranker": nil, "image": nil}, &receipt)
	return models[0]
}
func memoryM3CreateSprint(t *testing.T, v *executionSchedulerApp) wc.Sprint {
	t.Helper()
	var mutation wc.StructureMutation
	milestone := wc.CreateMilestoneRequest{MilestoneID: guardID[wc.Milestone](t), Title: "Memory lifecycle"}
	v.httpSeed(t, "/milestones", map[string]any{"request": milestone}, &mutation)
	sprint := wc.CreateSprintRequest{SprintID: guardID[pc.Sprint](t), MilestoneID: milestone.MilestoneID, Title: "Memory explicit tools"}
	v.httpSeed(t, "/sprints", map[string]any{"request": sprint}, &mutation)
	before, err := v.projects.service.GetSchedulerConfig(v.ctx, v.actor, v.a)
	workAppCheck(t, "memory-scheduler-config", err)
	limit := int64(1)
	_, err = v.projects.service.UpdateProject(v.ctx, v.actor, executionSchedulerAppMeta(t, &before.Project.Version), v.a, pc.UpdateProjectRequest{Scheduler: &pc.ProjectSchedulerConfig{Enabled: true, MaxConcurrency: &limit}})
	workAppCheck(t, "memory-enable-scheduler", err)
	return *mutation.Sprint
}
func memoryM3CreateTask(t *testing.T, v *executionSchedulerApp, sprint pc.SprintID, agent id.AgentID, plan string) wc.Task {
	t.Helper()
	var mutation wc.TaskMutation
	request := wc.TaskCreate{TaskID: guardID[wc.Task](t), SprintID: sprint, Title: plan, Type: wc.TaskTypeTask, Priority: wc.TaskPriorityMedium, Plan: plan}
	v.httpSeed(t, "/tasks", map[string]any{"request": request}, &mutation)
	moved, err := v.planning.transitions.TransferTask(v.ctx, v.actor, executionSchedulerAppMeta(t, &mutation.Task.Version), v.a, request.TaskID, wc.TaskTransfer{TargetState: wc.TaskStateTodo, AssigneeAgentID: &agent, AddBlockers: []wc.TaskBlockerCreate{}, ResolveBlockerIDs: []wc.TaskBlockerID{}})
	workAppCheck(t, "memory-assign-task", err)
	return moved.Task
}
func memoryM3AwaitExecution(t *testing.T, v *executionSchedulerApp, task wc.TaskID) id.ExecutionID {
	t.Helper()
	ctx, cancel := context.WithTimeout(v.ctx, 30*time.Second)
	defer cancel()
	tick := time.NewTicker(25 * time.Millisecond)
	defer tick.Stop()
	for {
		var count int
		var value, status string
		err := v.admin.QueryRow(ctx, `SELECT count(*),COALESCE(min(id),''),COALESCE(min(status),'') FROM agenteam_execution.executions WHERE project_id=$1 AND launch_request->'trigger'->>'task_id'=$2`, v.a.String(), task.String()).Scan(&count, &value, &status)
		workAppCheck(t, "memory-observe-original-execution", err)
		if count > 1 {
			t.Fatal("Memory task unexpectedly relaunched")
		}
		if count == 1 && ec.Status(status).Terminal() {
			if status != string(ec.Succeeded) {
				t.Fatalf("Memory Execution terminal=%s", status)
			}
			execution, err := f.ParseID[id.Execution](value)
			workAppCheck(t, "memory-execution-id", err)
			return execution
		}
		select {
		case <-ctx.Done():
			t.Fatalf("Memory Execution not terminal count=%d status=%s", count, status)
		case <-tick.C:
		}
	}
}
func memoryM3Facts(t *testing.T, v *executionSchedulerApp, agent id.AgentID, memoryID mm.ID, executions []id.ExecutionID, expectedCalls int) {
	t.Helper()
	var calls, retired, operations, retentions, canonical, deleted, auditRetain, auditDelete int
	err := v.admin.QueryRow(v.ctx, `SELECT
 (SELECT count(*) FROM agenteam_memory.model_calls WHERE project_id::text=$1::text AND agent_id::text=$2::text),
 (SELECT count(*) FROM agenteam_memory.model_calls m JOIN agenteam_model.calls c ON c.id=m.call_id JOIN agenteam_model.invocations i ON i.id=c.invocation_id JOIN agenteam_secret.secret_leases l ON l.id=c.lease_id WHERE m.project_id::text=$1::text AND m.agent_id::text=$2::text AND m.retired AND c.retired AND l.released AND c.phase='succeeded' AND i.final_status='succeeded' AND i.dispatch='sent' AND m.invocation_id=c.invocation_id AND m.attempt_index=1 AND m.attempt_process_id=m.process_id AND m.attempt_fence=1 AND NOT EXISTS(SELECT 1 FROM agenteam_model.usage_project_calls u WHERE u.call_id=c.id AND u.phase<>'business_closed')),
 (SELECT count(*) FROM agenteam_memory.operations WHERE project_id::text=$1::text AND agent_id::text=$2::text AND phase='committed'),
 (SELECT count(*) FROM agenteam_memory.generations g JOIN agenteam_model.serving_snapshot_retentions r ON r.generation_id=g.generation_id AND r.snapshot_id=g.snapshot_id WHERE g.project_id::text=$1::text AND g.agent_id::text=$2::text AND g.status='ready' AND g.owner_version=1 AND g.retention_state='retained' AND r.state='retained' AND r.consumer_kind='memory'),
 (SELECT count(*) FROM agenteam_memory.memories WHERE project_id::text=$1::text AND agent_id::text=$2::text),
 (SELECT count(*) FROM agenteam_memory.memories WHERE project_id::text=$1::text AND agent_id::text=$2::text AND memory_id::text=$3::text AND status='deleted'),
 (SELECT count(*) FROM agenteam_audit.audit_records WHERE project_id::text=$1::text AND action='memory.retain'),
 (SELECT count(*) FROM agenteam_audit.audit_records WHERE project_id::text=$1::text AND action='memory.delete')`, v.a.String(), agent.String(), memoryID.String()).Scan(&calls, &retired, &operations, &retentions, &canonical, &deleted, &auditRetain, &auditDelete)
	workAppCheck(t, "memory-durable-model-reference-audit", err)
	if calls != expectedCalls || retired != expectedCalls || operations != 1 || retentions != 1 || canonical != 1 || deleted != 1 || auditRetain != 1 || auditDelete != 1 {
		t.Fatalf("Memory durable counts calls=%d retired=%d operations=%d retentions=%d canonical=%d deleted=%d retain_audit=%d delete_audit=%d", calls, retired, operations, retentions, canonical, deleted, auditRetain, auditDelete)
	}
	ids := make([]string, len(executions))
	for n, e := range executions {
		ids[n] = e.String()
	}
	var count int
	workAppCheck(t, "memory-original-tool-results", v.admin.QueryRow(v.ctx, `SELECT count(*) FROM agenteam_execution.loop_tool_results WHERE execution_id::text=ANY($1)`, ids).Scan(&count))
	if count != 5 {
		t.Fatalf("Memory original execution tool results=%d", count)
	}
}

func memoryM3ReplaceEmbedding(t *testing.T, v *executionSchedulerApp, scenario string) mc.ModelID {
	t.Helper()
	secrets, ok := v.root.owned.secret().(*secret.Service)
	if !ok {
		t.Fatal("original Secret absent")
	}
	material, err := sc.NewSecretMaterial([]byte("owned-replacement-fixture-credential"))
	workAppCheck(t, "memory-replacement-material", err)
	identity, err := f.NewCommandIdentity("secret", []string{v.actor.Details().UserID}, "create", f.IdempotencyKey(guardID[struct{}](t).String()))
	workAppCheck(t, "memory-replacement-secret-command", err)
	credential, err := secrets.ExecuteWrite(v.ctx, sc.WriteRequest{Actor: v.actor, Scope: id.SystemScope(), Identity: identity, Kind: sc.Create, Purpose: sc.Model, Value: material})
	material.Destroy()
	workAppCheck(t, "memory-replacement-secret", err)
	var receipt mc.CommandReceipt
	meetingM3SystemCall(t, v, "POST", "model-providers", map[string]any{"input": map[string]any{"name": "Memory replacement provider", "protocol": mc.OpenAIEmbeddings, "base_url": "https://fixture.test:8443/case/" + scenario, "enabled": true, "credential_ref": credential.Metadata.CredentialRef.Details().ID, "options": map[string]any{}}}, &receipt)
	provider, err := f.ParseID[mc.Provider](receipt.ResourceID)
	workAppCheck(t, "memory-replacement-provider", err)
	capabilities := map[string]any{"tool_calls": false, "parallel_tool_calls": false, "streaming": false, "reasoning": false, "input_modalities": []string{"text"}, "output_modalities": []string{"vector"}, "reasoning_efforts": []string{}, "structured_output_modes": []string{}, "context_length": nil, "max_output": nil}
	meetingM3SystemCall(t, v, "POST", "models", map[string]any{"provider_id": provider, "input": map[string]any{"name": "Memory replacement model", "provider_model_id": "memory-replacement", "type": mc.EmbeddingModel, "enabled": true, "parameters": map[string]any{}, "request_overwrite": map[string]any{}, "header_overwrite": map[string]string{}, "capabilities": capabilities}}, &receipt)
	model, err := f.ParseID[mc.Model](receipt.ResourceID)
	workAppCheck(t, "memory-replacement-model", err)
	var selection struct {
		ID         string    `json:"id"`
		Version    f.Version `json:"version"`
		Configured *struct {
			Memory   mc.ModelID  `json:"memory"`
			Reranker *mc.ModelID `json:"reranker"`
			Image    *mc.ModelID `json:"image"`
		} `json:"configured"`
	}
	meetingM3SystemCall(t, v, "GET", "model-selection", nil, &selection)
	if selection.Configured == nil {
		t.Fatal("first generation lost configured selectors")
	}
	meetingM3SystemCall(t, v, "PUT", "model-selection", map[string]any{"id": selection.ID, "expected_version": selection.Version, "embedding": model, "memory": selection.Configured.Memory, "reranker": selection.Configured.Reranker, "image": selection.Configured.Image}, &receipt)
	return model
}

func memoryM3ProfileFacts(t *testing.T, v *executionSchedulerApp, agent id.AgentID, previous string, model mc.ModelID) {
	t.Helper()
	var ready, released, spaces int
	workAppCheck(t, "memory-switched-generation-facts", v.admin.QueryRow(v.ctx, `SELECT
 (SELECT count(*) FROM agenteam_memory.namespaces n JOIN agenteam_memory.generations g ON g.generation_id=n.serving_generation_id JOIN agenteam_model.serving_snapshot_retentions r ON r.generation_id=g.generation_id AND r.snapshot_id=g.snapshot_id WHERE n.project_id=$1 AND n.agent_id=$2 AND g.generation_id<>$3 AND g.status='ready' AND g.schema_version=2 AND g.profile_data->>'model_id'=$4 AND g.profile_digest IS NOT NULL AND g.retention_state='retained' AND r.state='retained'),
 (SELECT count(*) FROM agenteam_memory.generations g JOIN agenteam_model.serving_snapshot_retentions r ON r.generation_id=g.generation_id AND r.snapshot_id=g.snapshot_id WHERE g.project_id=$1 AND g.agent_id=$2 AND g.generation_id=$3 AND g.status='retired' AND g.owner_version=2 AND g.retention_state='released' AND r.state='released' AND r.released_version=2),
 (SELECT count(*) FROM agenteam_memory.projections p JOIN agenteam_memory.generations g USING(generation_id) WHERE p.project_id=$1 AND p.agent_id=$2 AND ((g.generation_id=$3 AND p.embedding::text='[1,0]') OR (g.generation_id<>$3 AND g.status='ready' AND p.embedding::text='[0,1]')))`, v.a.String(), agent.String(), previous, model.String()).Scan(&ready, &released, &spaces))
	if ready != 1 || released != 1 || spaces != 2 {
		t.Fatalf("profile switch ready=%d old_released=%d complete_spaces=%d", ready, released, spaces)
	}
}
func memoryM3Diagnose(t *testing.T, v *executionSchedulerApp, stage string) {
	t.Helper()
	t.Logf("M3 Memory stopped stage=%s", stage)
	if v == nil || v.admin == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	var safe []byte
	err := v.admin.QueryRow(ctx, `SELECT jsonb_build_object(
 'executions',(SELECT COALESCE(jsonb_agg(jsonb_build_object('status',status,'version',version)),'[]'::jsonb) FROM agenteam_execution.executions WHERE project_id::text=$1::text),
 'model',(SELECT COALESCE(jsonb_agg(jsonb_build_object('purpose',purpose,'status',final_status,'dispatch',dispatch,'code',error_data->>'code')),'[]'::jsonb) FROM agenteam_model.invocations WHERE project_id::text=$1::text),
 'memory_calls',(SELECT COALESCE(jsonb_agg(jsonb_build_object('purpose',purpose,'snapshot',snapshot_id IS NOT NULL,'attempt',invocation_id IS NOT NULL,'retired',retired)),'[]'::jsonb) FROM agenteam_memory.model_calls WHERE project_id::text=$1::text),
 'generations',(SELECT COALESCE(jsonb_agg(jsonb_build_object('status',status,'version',owner_version,'retention',retention_state)),'[]'::jsonb) FROM agenteam_memory.generations WHERE project_id::text=$1::text),
 'operations',(SELECT COALESCE(jsonb_agg(jsonb_build_object('phase',phase)),'[]'::jsonb) FROM agenteam_memory.operations WHERE project_id::text=$1::text),
 'tool',(SELECT COALESCE(jsonb_agg(jsonb_build_object('phase',phase,'outcome',outcome)),'[]'::jsonb) FROM agenteam_tool.operations WHERE project_id::text=$1::text))`, v.a.String()).Scan(&safe)
	if err == nil {
		t.Logf("M3 Memory safe durable facts=%s", safe)
	} else {
		t.Logf("M3 Memory diagnostic unavailable type=%T", err)
	}
}
