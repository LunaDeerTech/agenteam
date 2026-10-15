//go:build integration && linux

package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	kc "github.com/LunaDeerTech/agenteam/internal/central/knowledge/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/LunaDeerTech/agenteam/internal/central/retrieval/lexical"
	netfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/outbound"
	pgfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/postgres"
)

// Both domains run in the original App/database. Knowledge uses the second
// formally created project, so neither workflow alters the other's current
// Sprint. The original Memory helper retains its four Executions and teardown.
func TestKnowledgeMemoryM3AppHybrid(t *testing.T) {
	var original *executionSchedulerApp
	memoryM3CrossExecutionWith(t, memoryM3CrossExecutionOptions{
		RetrievalBackend: "pg_search",
		BeforeRun: func(t *testing.T, d *dependencies, v *executionSchedulerApp) {
			original = v
			knowledgeM3HybridBootstrap(t, d, v)
		},
		AfterModelConfiguration: knowledgeM3HybridKnowledge,
		ExtraEmbeddingRequests:  2,
	})
	knowledgeM3HybridDiagnostic(t, original, "knowledge", original.b, "Mercury orbital period")
	knowledgeM3HybridDiagnostic(t, original, "memory", original.a, "schema migration rollback")
	t.Log("M3_SHARED_HYBRID actual_same_database=true knowledge_and_memory=true both_actual_legs_nonempty=true original_model_tool_consumption=true")
}

func knowledgeM3HybridDiagnostic(t *testing.T, v *executionSchedulerApp, domain string, project id.ProjectID, query string) {
	t.Helper()
	digest := sha256.Sum256([]byte(query))
	decoder := json.NewDecoder(strings.NewReader(v.root.logs.String()))
	matched := 0
	for {
		var row map[string]any
		err := decoder.Decode(&row)
		if err == io.EOF {
			break
		}
		workAppCheck(t, "hybrid-typed-diagnostic-json", err)
		if row["event"] != "retrieval" || row["domain_kind"] != domain || row["project_id"] != project.String() || row["query_sha256"] != hex.EncodeToString(digest[:]) {
			continue
		}
		if row["retrieval_mode"] == "hybrid" && row["lexical_backend"] == "pg_search" && row["lexical_candidate_count"] == float64(1) && row["dense_candidate_count"] == float64(1) && row["fusion_candidate_count"] == float64(1) && row["result_count"] == float64(1) && row["error_code"] == "" && row["degradation_reason"] == "" {
			matched++
		}
	}
	if matched != 1 {
		t.Fatalf("actual %s hybrid positive-query diagnostics=%d expected=1", domain, matched)
	}
}

func knowledgeM3HybridBootstrap(t *testing.T, _ *dependencies, v *executionSchedulerApp) {
	t.Helper()
	if v.root.db.Fixture.Retrieval != pgfixture.PGSearchProfile {
		t.Fatal("hybrid workflow requires the explicitly pinned pg_search fixture")
	}
	workAppCheck(t, "hybrid-owned-fixture", v.root.db.Fixture.Verify())
	migrator, err := postgres.NewMigrator(v.root.cfg.Database())
	workAppCheck(t, "hybrid-original-migrator", err)
	state := migrator.Migrate(v.ctx)
	if !state.Migrated || state.Fault != nil {
		workAppCheck(t, "hybrid-original-migrations", state.Fault)
		t.Fatal("hybrid database migrations did not complete")
	}
	// Use the exact deployment bootstrap, after the real global migrations and
	// before the normal App startup/readiness. No migration result is replaced.
	raw, err := os.ReadFile("../../../deploy/retrieval/bootstrap-pg-search.sql")
	workAppCheck(t, "hybrid-bootstrap-source", err)
	if len(raw) == 0 || len(raw) > 16<<10 {
		t.Fatal("bounded deployment bootstrap required")
	}
	ctx, cancel := context.WithTimeout(v.ctx, 10*time.Second)
	defer cancel()
	conn, err := v.root.db.Fixture.Connect(ctx, v.root.db.Name)
	workAppCheck(t, "hybrid-bootstrap-connection", err)
	defer func() {
		closeCtx, stop := context.WithTimeout(context.Background(), time.Second)
		defer stop()
		workAppCheck(t, "hybrid-bootstrap-close", conn.Close(closeCtx))
	}()
	_, err = conn.Exec(ctx, string(raw))
	workAppCheck(t, "hybrid-explicit-bootstrap", err)
	var ready bool
	workAppCheck(t, "hybrid-bootstrap-readiness", conn.QueryRow(ctx, lexical.PGSearchReadinessSQL).Scan(&ready))
	if !ready {
		t.Fatal("exact same-database pg_search indexes are not ready")
	}
}

func knowledgeM3HybridKnowledge(t *testing.T, original *executionSchedulerApp, network *netfixture.Descriptor, embeddingScenario string) {
	t.Helper()
	// This is only a borrowed view for existing test helpers: no new App,
	// session, database, lifecycle owner or cleanup callback is constructed.
	v := &executionSchedulerApp{workAppFixture: &workAppFixture{
		root: original.root, store: original.workAppFixture.store,
		assembly: original.assembly, planning: original.planning,
		projects: original.projects, objects: original.objects,
		ctx: original.ctx, actor: original.actor, cookie: original.cookie,
		csrf: original.csrf, address: original.address, client: original.client,
		admin: original.admin, a: original.b, b: original.a,
		instance: original.instance,
	}, store: original.store, runtime: original.runtime}
	v.assembly.mu.Lock()
	documents, ok := v.assembly.knowledge.(*knowledgeCallAssembly)
	v.assembly.mu.Unlock()
	if !ok || documents.index == nil {
		t.Fatal("original Knowledge index missing in shared App")
	}
	const text = "Mercury's orbital period is 88 Earth days."
	source, err := kc.NewTextSource(kc.PlainText, text)
	workAppCheck(t, "hybrid-knowledge-source", err)
	doc, err := documents.pair.Service().CreateDocument(v.ctx, v.actor, executionSchedulerAppMeta(t, nil), kc.CreateRequest{ProjectID: v.a, DocumentID: guardID[kc.Document](t), Title: "Mercury orbital period"}, source)
	workAppCheck(t, "hybrid-knowledge-create", err)
	profile := knowledgeM3AwaitAutomatic(t, v, doc, "", text)
	var actualProfile bool
	workAppCheck(t, "hybrid-knowledge-serving", v.admin.QueryRow(v.ctx, `SELECT lexical_backend='pg_search' AND state='active' AND dimension=2 AND retention_state='retained' FROM agenteam_knowledge.index_profiles WHERE project_id=$1 AND id=$2`, v.a.String(), profile).Scan(&actualProfile))
	if !actualProfile {
		t.Fatal("Knowledge serving did not publish the actual hybrid profile")
	}
	tools := map[string]id.ToolID{}
	for _, key := range []string{"builtin:query-doc", "builtin:read-doc"} {
		var raw string
		workAppCheck(t, "hybrid-knowledge-tool", v.admin.QueryRow(v.ctx, `SELECT tool_id FROM agenteam_tool.identities WHERE stable_key=$1`, key).Scan(&raw))
		tools[key], err = f.ParseID[id.Tool](raw)
		workAppCheck(t, "hybrid-knowledge-tool-id", err)
	}
	sequence := []netfixture.WireScenario{
		knowledgeM3AutoToolReply(t, tools["builtin:query-doc"], map[string]any{"query": "Mercury orbital period", "limit": 5}, 1),
		knowledgeM3AutoToolReply(t, tools["builtin:read-doc"], map[string]any{"document_id": doc.ID, "document_version": doc.ContentVersion}, 2),
		knowledgeM3AutoReply(t, map[string]any{"role": "assistant", "content": "Mercury takes 88 Earth days. [document_id=" + doc.ID.String() + "; document_version=1]"}, "stop", 3),
	}
	chat, err := network.Create(v.ctx, netfixture.ScenarioConfig{Mode: "openai_chat_wire", WireSequence: sequence})
	workAppCheck(t, "hybrid-knowledge-peer", err)
	credential := knowledgeM3RetentionCredential(t, v, "owned-hybrid-knowledge-chat")
	var receipt mc.CommandReceipt
	knowledgeM3SystemCall(t, v, "POST", "model-providers", map[string]any{"input": map[string]any{"name": "Hybrid Knowledge Agent", "protocol": mc.OpenAIChat, "base_url": "https://fixture.test:8443/case/" + chat, "enabled": true, "credential_ref": credential, "options": map[string]any{}}}, &receipt)
	provider, err := f.ParseID[mc.Provider](receipt.ResourceID)
	workAppCheck(t, "hybrid-knowledge-provider", err)
	knowledgeM3SystemCall(t, v, "POST", "models", map[string]any{"provider_id": provider, "input": knowledgeM3ModelWire(knowledgeM3ModelInput(t, "agent", "knowledge-hybrid"))}, &receipt)
	model, err := f.ParseID[mc.Model](receipt.ResourceID)
	workAppCheck(t, "hybrid-knowledge-model", err)
	knowledgeM3AgentSources(t, v, model, doc, text)
	peer := &m1Workflow{app: v, network: network}
	peer.joinedPeer(t, chat, 3, 3)
	peer.joinedPeer(t, embeddingScenario, 2, 2)
	t.Log("M3_KNOWLEDGE_HYBRID shared_database=true distinct_project=true automatic_serving=true actual_agent_query_read=true source_citation=true")
}
