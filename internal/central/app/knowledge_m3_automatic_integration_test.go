//go:build integration && linux

package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/config"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	kc "github.com/LunaDeerTech/agenteam/internal/central/knowledge/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	netfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/outbound"
)

// No Rebuild/RunPending/Advance is called by this test. The actual App Run owns
// initial indexing, a canonical update, and the selector's replacement space.
func TestKnowledgeM3AppAutomaticIndex(t *testing.T) {
	network, err := netfixture.Load()
	workAppCheck(t, "knowledge-auto-network", err)
	address, err := netip.ParseAddr(network.PrivateIP)
	workAppCheck(t, "knowledge-auto-address", err)
	var transport *outboundRuntime
	trace := &knowledgeM3CaptureTrace{}
	v := newExecutionSchedulerAppWith(t, func(d *dependencies, v *executionSchedulerApp) {
		v.root.cfg = knowledgeM3RetentionConfiguration(t, guardEnvironment(t, v.root.db), network.CAFile)
		v.store.observeCommit = trace.observe
		d.outboundConstruct = func(_ context.Context, _ config.Config, actual *outboundRuntime) error {
			actual.resolver = appFixtureResolver{address}
			transport = actual
			return nil
		}
	})
	if transport == nil {
		t.Fatal("original outbound missing")
	}
	m1AllowOwnedPeer(t, v, transport, network)
	v.assembly.mu.Lock()
	documents, ok := v.assembly.knowledge.(*knowledgeCallAssembly)
	v.assembly.mu.Unlock()
	if !ok || documents.index == nil {
		t.Fatal("original index missing")
	}
	defer func() {
		if t.Failed() {
			trace.diagnose(t)
			knowledgeM3IndexDiagnostic(t, v, nil)
			knowledgeM3AutomaticDiagnostic(t, v)
		}
	}()
	wire := netfixture.WireScenario{Status: 200, Headers: map[string]string{"Content-Type": "application/json"}, Chunks: [][]byte{workAppJSON(t, map[string]any{"object": "list", "model": "knowledge-retention", "data": []any{map[string]any{"object": "embedding", "index": 0, "embedding": []float64{1, 0}}}, "usage": map[string]any{"prompt_tokens": 10, "total_tokens": 10}})}}
	embeddingCase, err := network.Create(v.ctx, netfixture.ScenarioConfig{Mode: "openai_embeddings_wire", Wire: &wire})
	workAppCheck(t, "knowledge-auto-embedding-peer", err)
	provider, credential := knowledgeM3RetentionModels(t, v, embeddingCase)
	source, err := kc.NewTextSource(kc.PlainText, "Mercury completes its orbit in 88 Earth days.")
	workAppCheck(t, "knowledge-auto-source", err)
	doc, err := documents.pair.Service().CreateDocument(v.ctx, v.actor, executionSchedulerAppMeta(t, nil), kc.CreateRequest{ProjectID: v.a, DocumentID: guardID[kc.Document](t), Title: "Automatic Mercury fact"}, source)
	workAppCheck(t, "knowledge-auto-create", err)
	first := knowledgeM3AwaitAutomatic(t, v, doc, "", "Mercury completes its orbit in 88 Earth days.")
	knowledgeM3RetentionFacts(t, v, first, "active", "retained", credential)
	const updatedText = "Mercury's orbital period is 88 Earth days. This is the revised current source."
	updated, err := kc.NewTextSource(kc.PlainText, updatedText)
	workAppCheck(t, "knowledge-auto-update-source", err)
	doc, err = documents.pair.Service().UpdateDocument(v.ctx, v.actor, executionSchedulerAppMeta(t, &doc.ContentVersion), v.a, doc.ID, kc.UpdateRequest{ReplaceSource: true}, &updated)
	workAppCheck(t, "knowledge-auto-update", err)
	if doc.ContentVersion != 2 {
		t.Fatal("canonical version did not advance")
	}
	if same := knowledgeM3AwaitAutomatic(t, v, doc, "", updatedText); same != first {
		t.Fatal("document update unnecessarily replaced embedding profile")
	}
	input := knowledgeM3ModelInput(t, "embedding", "knowledge-retention")
	input.Name = "Knowledge automatic replacement"
	var receipt mc.CommandReceipt
	knowledgeM3SystemCall(t, v, "POST", "models", map[string]any{"provider_id": provider, "input": knowledgeM3ModelWire(input)}, &receipt)
	replacement, err := f.ParseID[mc.Model](receipt.ResourceID)
	workAppCheck(t, "knowledge-auto-replacement-model", err)
	var selection knowledgeM3AutomaticSelection
	knowledgeM3SystemCall(t, v, "GET", "model-selection", nil, &selection)
	if selection.Configured == nil || selection.Configured.Summary.Validate() != nil {
		t.Fatal("configured platform selector missing summary model")
	}
	knowledgeM3SystemCall(t, v, "PUT", "model-selection", selection.replaceEmbedding(replacement), &receipt)
	second := knowledgeM3AwaitAutomatic(t, v, doc, first, updatedText)
	knowledgeM3RetentionFacts(t, v, first, "retired", "released", credential)
	knowledgeM3RetentionFacts(t, v, second, "active", "retained", credential)
	var correctProfile bool
	workAppCheck(t, "knowledge-auto-formal-profile", v.admin.QueryRow(v.ctx, `SELECT profile_data->>'model_id'=$3 AND profile_data->>'selection_version'=$4 AND profile_digest IS NOT NULL AND source_digest IS NOT NULL FROM agenteam_knowledge.index_profiles WHERE project_id=$1 AND id=$2`, v.a.String(), second, replacement.String(), fmt.Sprint(int64(selection.Version+1))).Scan(&correctProfile))
	if !correctProfile {
		t.Fatal("automatic generation used stale selector facts")
	}
	// Controlled HTTP only chooses the reply; the real Scheduler and ToolLoop
	// produce all invocation witnesses, results and subsequent Model inputs.
	tools := map[string]id.ToolID{}
	for _, key := range []string{"builtin:query-doc", "builtin:read-doc"} {
		var raw string
		workAppCheck(t, "knowledge-auto-tool-registration", v.admin.QueryRow(v.ctx, `SELECT tool_id FROM agenteam_tool.identities WHERE stable_key=$1`, key).Scan(&raw))
		tools[key], err = f.ParseID[id.Tool](raw)
		workAppCheck(t, "knowledge-auto-tool-id", err)
	}
	sequence := []netfixture.WireScenario{
		knowledgeM3AutoToolReply(t, tools["builtin:query-doc"], map[string]any{"query": "Mercury orbital period", "limit": 5}, 1),
		knowledgeM3AutoToolReply(t, tools["builtin:read-doc"], map[string]any{"document_id": doc.ID, "document_version": doc.ContentVersion}, 2),
		knowledgeM3AutoReply(t, map[string]any{"role": "assistant", "content": "Mercury takes 88 Earth days. [document_id=" + doc.ID.String() + "; document_version=2]"}, "stop", 3),
	}
	chatCase, err := network.Create(v.ctx, netfixture.ScenarioConfig{Mode: "openai_chat_wire", WireSequence: sequence})
	workAppCheck(t, "knowledge-auto-chat-peer", err)
	chatCredential := knowledgeM3RetentionCredential(t, v, "owned-knowledge-auto-chat")
	knowledgeM3SystemCall(t, v, "POST", "model-providers", map[string]any{"input": map[string]any{"name": "Knowledge automatic Agent", "protocol": mc.OpenAIChat, "base_url": "https://fixture.test:8443/case/" + chatCase, "enabled": true, "credential_ref": chatCredential, "options": map[string]any{}}}, &receipt)
	chatProvider, err := f.ParseID[mc.Provider](receipt.ResourceID)
	workAppCheck(t, "knowledge-auto-chat-provider", err)
	knowledgeM3SystemCall(t, v, "POST", "models", map[string]any{"provider_id": chatProvider, "input": knowledgeM3ModelWire(knowledgeM3ModelInput(t, "agent", "knowledge-auto"))}, &receipt)
	agentModel, err := f.ParseID[mc.Model](receipt.ResourceID)
	workAppCheck(t, "knowledge-auto-chat-model", err)
	knowledgeM3AgentSources(t, v, agentModel, doc, updatedText)
	var calls int
	workAppCheck(t, "knowledge-auto-index-model-count", v.admin.QueryRow(v.ctx, `SELECT count(*) FROM agenteam_knowledge.model_operations o JOIN agenteam_model.calls c ON c.id=o.call_id JOIN agenteam_model.invocations i ON i.id=c.invocation_id WHERE o.project_id=$1 AND o.kind='index' AND c.retired AND c.phase='succeeded' AND i.dispatch='sent' AND i.final_status='succeeded'`, v.a.String()).Scan(&calls))
	if calls != 3 {
		t.Fatalf("automatic index calls=%d expected=3", calls)
	}
	v.requestStop()
	v.store.releaseDirectory()
	usageAppWait(t, v.root.done, "knowledge-auto-app-return")
	workAppCheck(t, "knowledge-auto-app-exit", v.root.err)
	if !documents.index.Joined() || !v.assembly.Joined() || !v.root.owned.producersJoined() {
		t.Fatal("automatic index original owner did not join")
	}
	taskExecutionGuardReleased(t, v.workAppFixture)
	for scenario, want := range map[string]int{embeddingCase: 4, chatCase: 3} {
		peer, err := network.State(context.Background(), scenario)
		workAppCheck(t, "knowledge-auto-peer-state", err)
		if len(peer.Requests) != want || peer.CompletedHandlers != want || peer.ActiveHandlers != 0 {
			t.Fatalf("automatic peer requests=%d completed=%d active=%d expected=%d", len(peer.Requests), peer.CompletedHandlers, peer.ActiveHandlers, want)
		}
	}
	t.Log("M3_KNOWLEDGE_AUTOMATIC first_document=true canonical_update=true selector_rebuild=true actual_agent_query_read=true original_app_run_join=true controlled_model=true")
}

// GET /model-selection exposes configuration under its nullable configured
// member. The PUT body is flat and preserves the other actual selectors.
type knowledgeM3AutomaticSelection struct {
	ID         string    `json:"id"`
	Version    f.Version `json:"version"`
	Configured *struct {
		Embedding mc.ModelChoice  `json:"embedding"`
		Summary   mc.ModelChoice  `json:"summary"`
		Reranker  *mc.ModelChoice `json:"reranker"`
		Image     *mc.ModelChoice `json:"image"`
	} `json:"configured"`
}

func (s knowledgeM3AutomaticSelection) replaceEmbedding(model mc.ModelID) map[string]any {
	return map[string]any{"id": s.ID, "expected_version": s.Version, "embedding": mc.DefaultModelChoice(model), "summary": s.Configured.Summary, "reranker": s.Configured.Reranker, "image": s.Configured.Image}
}

func TestKnowledgeM3AutomaticSelectionWire(t *testing.T) {
	id, old, memory, next := guardID[struct{}](t).String(), guardID[mc.Model](t), guardID[mc.Model](t), guardID[mc.Model](t)
	// The actual GET response schema in model/http_dto.go is nested, while
	// contract.PlatformSelection is the flat command-domain representation.
	raw := workAppJSON(t, map[string]any{"id": id, "version": f.Version(3), "configured": map[string]any{"embedding": mc.DefaultModelChoice(old), "summary": mc.DefaultModelChoice(memory), "reranker": nil, "image": nil}})
	var selection knowledgeM3AutomaticSelection
	workAppCheck(t, "knowledge-selector-wire", json.Unmarshal(raw, &selection))
	if selection.Configured == nil || selection.Configured.Summary.ModelID != memory {
		t.Fatal("actual nested selector configuration was lost")
	}
	var body struct {
		ID       string          `json:"id"`
		Version  f.Version       `json:"expected_version"`
		Summary  mc.ModelChoice  `json:"summary"`
		Embed    mc.ModelChoice  `json:"embedding"`
		Reranker *mc.ModelChoice `json:"reranker"`
		Image    *mc.ModelChoice `json:"image"`
	}
	workAppCheck(t, "knowledge-selector-update-wire", json.Unmarshal(workAppJSON(t, selection.replaceEmbedding(next)), &body))
	if body.ID != id || body.Version != 3 || body.Summary.ModelID != memory || body.Embed.ModelID != next || body.Reranker != nil || body.Image != nil {
		t.Fatal("selector update lost current version or unchanged selectors")
	}
}

// Only bounded lifecycle labels and presence bits are exposed. A profile
// capture resolves and pins without dispatching an embedding, so Model call
// counts alone cannot locate a failure before the first document job.
func knowledgeM3AutomaticDiagnostic(t *testing.T, v *executionSchedulerApp) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	rows, err := v.admin.Query(ctx, `SELECT 'profile',state,retention_state,snapshot_id IS NOT NULL,source_operation_id IS NOT NULL FROM agenteam_knowledge.index_profiles WHERE project_id=$1
 UNION ALL SELECT 'operation',kind,state,snapshot_id IS NOT NULL,false FROM agenteam_knowledge.model_operations WHERE project_id=$1
 UNION ALL SELECT 'job',state,COALESCE(error_code,''),process_id IS NOT NULL,false FROM agenteam_knowledge.index_jobs WHERE project_id=$1 LIMIT 24`, v.a.String())
	if err != nil {
		t.Logf("M3_KNOWLEDGE_AUTOMATIC_FACTS unavailable=%T", err)
		return
	}
	defer rows.Close()
	for rows.Next() {
		var kind, state, detail string
		var snapshotOrProcess, source bool
		if err := rows.Scan(&kind, &state, &detail, &snapshotOrProcess, &source); err != nil {
			t.Logf("M3_KNOWLEDGE_AUTOMATIC_FACTS scan_unavailable=%T", err)
			return
		}
		t.Logf("M3_KNOWLEDGE_AUTOMATIC_FACT kind=%s state=%s detail=%s snapshot_or_process=%t source=%t", knowledgeM3TechnicalIdentifier(kind), knowledgeM3TechnicalIdentifier(state), knowledgeM3TechnicalIdentifier(detail), snapshotOrProcess, source)
	}
	if err := rows.Err(); err != nil {
		t.Logf("M3_KNOWLEDGE_AUTOMATIC_FACTS read_unavailable=%T", err)
	}
}

func knowledgeM3AwaitAutomatic(t *testing.T, v *executionSchedulerApp, doc kc.DocumentRef, previous, text string) string {
	t.Helper()
	tick := time.NewTicker(50 * time.Millisecond)
	defer tick.Stop()
	for {
		var profile string
		var ready, failed bool
		err := v.admin.QueryRow(v.ctx, `SELECT COALESCE((SELECT st.serving_profile_id::text FROM agenteam_knowledge.index_state st WHERE st.project_id=$1),''),
 EXISTS(SELECT 1 FROM agenteam_knowledge.index_state st JOIN agenteam_knowledge.index_profiles p ON p.project_id=st.project_id AND p.id=st.serving_profile_id
 JOIN agenteam_knowledge.index_generations g ON g.project_id=p.project_id AND g.profile_id=p.id
 JOIN agenteam_knowledge.index_chunks c ON c.project_id=g.project_id AND c.generation_id=g.id
 JOIN agenteam_knowledge.documents d ON d.project_id=g.project_id AND d.id=g.document_id
 WHERE p.project_id=$1 AND p.state='active' AND p.retention_state='retained' AND g.state='ready' AND g.lexical_complete AND g.dense_complete AND g.document_id=$2 AND g.document_version=$3 AND d.content_version=$3 AND d.indexing_status='ready' AND c.raw_text=$4),
 EXISTS(SELECT 1 FROM agenteam_knowledge.index_profiles WHERE project_id=$1 AND state='failed') OR EXISTS(SELECT 1 FROM agenteam_knowledge.index_jobs WHERE project_id=$1 AND document_id=$2 AND document_version=$3 AND state='failed')`, v.a.String(), doc.ID.String(), int64(doc.ContentVersion), text).Scan(&profile, &ready, &failed)
		workAppCheck(t, "knowledge-auto-ready-state", err)
		if ready && profile != "" && profile != previous {
			return profile
		}
		if !ready && profile != "" && profile != previous {
			var chunks, minimum, maximum int
			workAppCheck(t, "knowledge-auto-ready-content", v.admin.QueryRow(v.ctx, `SELECT count(*),COALESCE(min(octet_length(c.raw_text)),0),COALESCE(max(octet_length(c.raw_text)),0)
 FROM agenteam_knowledge.index_generations g JOIN agenteam_knowledge.index_chunks c ON c.generation_id=g.id
 JOIN agenteam_knowledge.documents d ON d.project_id=g.project_id AND d.id=g.document_id
 WHERE g.project_id=$1 AND g.document_id=$2 AND g.document_version=$3 AND g.profile_id=$4 AND g.state='ready' AND g.lexical_complete AND g.dense_complete AND d.content_version=g.document_version AND d.indexing_status='ready'`, v.a.String(), doc.ID.String(), int64(doc.ContentVersion), profile).Scan(&chunks, &minimum, &maximum))
			if chunks > 0 {
				t.Fatalf("current generation ready but exact source differs: chunks=%d expected_bytes=%d min_bytes=%d max_bytes=%d", chunks, len(text), minimum, maximum)
			}
		}
		if failed {
			t.Fatal("automatic index entered terminal failure")
		}
		select {
		case <-v.root.done:
			workAppCheck(t, "knowledge-auto-early-app-exit", v.root.err)
			t.Fatal("App returned before automatic index")
		case <-v.ctx.Done():
			t.Fatal("automatic index exceeded original App budget")
		case <-tick.C:
		}
	}
}
func knowledgeM3AutoToolReply(t *testing.T, tool id.ToolID, args any, n int) netfixture.WireScenario {
	return knowledgeM3AutoReply(t, map[string]any{"role": "assistant", "content": nil, "tool_calls": []any{map[string]any{"id": fmt.Sprintf("knowledge_call_%d", n), "type": "function", "function": map[string]any{"name": "tool_" + strings.ReplaceAll(tool.String(), "-", ""), "arguments": string(workAppJSON(t, args))}}}}, "tool_calls", n)
}
func knowledgeM3AutoReply(t *testing.T, message any, finish string, n int) netfixture.WireScenario {
	return netfixture.WireScenario{Status: 200, Headers: map[string]string{"Content-Type": "application/json"}, Chunks: [][]byte{workAppJSON(t, map[string]any{"id": fmt.Sprintf("knowledge-response-%d", n), "object": "chat.completion", "created": 1, "model": "knowledge-auto", "choices": []any{map[string]any{"index": 0, "message": message, "finish_reason": finish}}, "usage": map[string]any{"prompt_tokens": 10, "completion_tokens": 10, "total_tokens": 20}})}}
}
