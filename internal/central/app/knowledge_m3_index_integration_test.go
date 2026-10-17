//go:build integration && linux

package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/netip"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	ac "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/config"
	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	kc "github.com/LunaDeerTech/agenteam/internal/central/knowledge/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/outbound"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/retrieval/chunker"
	"github.com/LunaDeerTech/agenteam/internal/central/secret"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
	wc "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
	"github.com/LunaDeerTech/agenteam/tests/testsupport/accountenv"
	objectfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/objectstore"
	netfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/outbound"
	"github.com/jackc/pgx/v5/pgconn"
)

// Opt-in bounds this test to two public documents and their real embeddings.
// It uses the original App graph and preserves the deployment proxy/TLS path.
// Agent tool consumption has a separate assertion; successful indexing alone
// is deliberately not labeled the completed D13 acceptance.
func TestKnowledgeM3AppAuthorizedIndex(t *testing.T) {
	if os.Getenv("AGENTEAM_M3_KNOWLEDGE_LIVE") != "index" {
		t.Skip("explicit Knowledge live index opt-in required")
	}
	knowledgeM3AppAuthorized(t, false)
}

func TestKnowledgeM3AppAgentSources(t *testing.T) {
	if os.Getenv("AGENTEAM_M3_KNOWLEDGE_LIVE") != "agent" {
		t.Skip("explicit Knowledge live Agent opt-in required")
	}
	knowledgeM3AppAuthorized(t, true)
}

// This separate top verifies the current production retention graph against an
// owned TLS peer. It does not reuse the earlier external-model acceptance.
func TestKnowledgeM3AppServingRetention(t *testing.T) {
	network, err := netfixture.Load()
	workAppCheck(t, "knowledge-retention-network", err)
	address, err := netip.ParseAddr(network.PrivateIP)
	workAppCheck(t, "knowledge-retention-address", err)
	var transport *outboundRuntime
	v := newExecutionSchedulerAppWith(t, func(d *dependencies, v *executionSchedulerApp) {
		v.root.cfg = knowledgeM3RetentionConfiguration(t, guardEnvironment(t, v.root.db), network.CAFile)
		v.store.observeCommit = m1CommitObserver(t)
		d.outboundConstruct = func(_ context.Context, _ config.Config, actual *outboundRuntime) error {
			actual.resolver = appFixtureResolver{address}
			transport = actual
			return nil
		}
	})
	if transport == nil {
		t.Fatal("original controlled outbound owner missing")
	}
	m1AllowOwnedPeer(t, v, transport, network)
	v.assembly.mu.Lock()
	documents, ok := v.assembly.knowledge.(*knowledgeCallAssembly)
	v.assembly.mu.Unlock()
	if !ok || documents.index == nil {
		t.Fatal("original Knowledge index graph missing")
	}
	wire := netfixture.WireScenario{Status: 200, Headers: map[string]string{"Content-Type": "application/json"}, Chunks: [][]byte{workAppJSON(t, map[string]any{"object": "list", "model": "knowledge-retention", "data": []any{map[string]any{"object": "embedding", "index": 0, "embedding": []float64{1, 0}}}, "usage": map[string]any{"prompt_tokens": 10, "total_tokens": 10}})}}
	scenario, err := network.Create(v.ctx, netfixture.ScenarioConfig{Mode: "openai_embeddings_wire", Wire: &wire})
	workAppCheck(t, "knowledge-retention-peer", err)
	provider, credential := knowledgeM3RetentionModels(t, v, scenario)
	source, err := kc.NewTextSource(kc.PlainText, "Mercury completes its orbit in 88 Earth days.")
	workAppCheck(t, "knowledge-retention-source", err)
	doc, err := documents.pair.Service().CreateDocument(v.ctx, v.actor, executionSchedulerAppMeta(t, nil), kc.CreateRequest{ProjectID: v.a, DocumentID: guardID[kc.Document](t), Title: "Retained orbital fact"}, source)
	workAppCheck(t, "knowledge-retention-document", err)
	first, err := documents.index.Rebuild(v.ctx, v.actor, v.a)
	if err != nil {
		knowledgeM3IndexDiagnostic(t, v, err)
	}
	workAppCheck(t, "knowledge-first-retained-profile", err)
	if first.Documents != 1 || first.Chunks != 1 {
		t.Fatal("first generation was incomplete")
	}
	knowledgeM3RetentionFacts(t, v, first.ProfileID, "active", "retained", credential)
	// Replace the Provider's credential through the formal command. The old
	// snapshot must keep its independent reference after that Provider unpins it.
	replacement := knowledgeM3RetentionCredential(t, v, "owned-knowledge-replacement")
	var view struct {
		Version f.Version `json:"version"`
	}
	knowledgeM3SystemCall(t, v, "GET", "model-providers/"+provider.String(), nil, &view)
	var receipt mc.CommandReceipt
	knowledgeM3SystemCall(t, v, "PUT", "model-providers/"+provider.String(), map[string]any{"expected_version": view.Version, "input": map[string]any{"name": "Knowledge retained embedding", "protocol": mc.OpenAIEmbeddings, "base_url": "https://fixture.test:8443/case/" + scenario, "enabled": true, "credential_ref": replacement, "options": map[string]any{}}}, &receipt)
	knowledgeM3RetentionFacts(t, v, first.ProfileID, "active", "retained", credential)
	var oldReferences int
	workAppCheck(t, "knowledge-only-generation-reference", v.admin.QueryRow(v.ctx, `SELECT count(*) FROM agenteam_secret.secret_references WHERE credential_id=$1`, credential.String()).Scan(&oldReferences))
	if oldReferences != 1 {
		t.Fatalf("old credential references=%d, expected only retained generation", oldReferences)
	}
	second, err := documents.index.Rebuild(v.ctx, v.actor, v.a)
	if err != nil {
		knowledgeM3IndexDiagnostic(t, v, err)
	}
	workAppCheck(t, "knowledge-replacement-retained-profile", err)
	if second.ProfileID == first.ProfileID || second.Documents != 1 || second.Chunks != 1 {
		t.Fatal("replacement did not publish a complete fresh generation")
	}
	knowledgeM3RetentionFacts(t, v, first.ProfileID, "retired", "released", credential)
	knowledgeM3RetentionFacts(t, v, second.ProfileID, "active", "retained", replacement)
	var serving, calls, unretired, readers int
	workAppCheck(t, "knowledge-retention-runtime-facts", v.admin.QueryRow(v.ctx, `SELECT
 (SELECT count(*) FROM agenteam_knowledge.index_state WHERE project_id=$1 AND serving_profile_id=$2),
 (SELECT count(*) FROM agenteam_knowledge.model_operations o JOIN agenteam_model.calls c ON c.id=o.call_id JOIN agenteam_model.invocations i ON i.id=c.invocation_id WHERE o.project_id=$1 AND o.kind='index' AND c.retired AND c.phase='succeeded' AND i.dispatch='sent' AND i.final_status='succeeded'),
 (SELECT count(*) FROM agenteam_knowledge.model_operations WHERE project_id=$1 AND state<>'retired'),
 (SELECT count(*) FROM agenteam_object.object_leases WHERE object_id=$3 AND owner_kind='reader' AND state='active')`, v.a.String(), second.ProfileID, doc.ObjectID.String()).Scan(&serving, &calls, &unretired, &readers))
	if serving != 1 || calls != 2 || unretired != 0 || readers != 0 {
		t.Fatalf("retention runtime serving=%d calls=%d unretired=%d readers=%d", serving, calls, unretired, readers)
	}
	documents.index.StopAdmission()
	drain, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	err = documents.index.Drain(drain)
	cancel()
	workAppCheck(t, "knowledge-retention-original-drain", err)
	v.requestStop()
	v.store.releaseDirectory()
	usageAppWait(t, v.root.done, "knowledge-retention-app-return")
	workAppCheck(t, "knowledge-retention-app-exit", v.root.err)
	if !documents.index.Joined() || !v.assembly.Joined() || !v.root.owned.producersJoined() {
		t.Fatal("retention graph returned before original owners joined")
	}
	taskExecutionGuardReleased(t, v.workAppFixture)
	peer, err := network.State(context.Background(), scenario)
	workAppCheck(t, "knowledge-retention-peer-state", err)
	if len(peer.Requests) != 2 || peer.CompletedHandlers != 2 || peer.ActiveHandlers != 0 {
		t.Fatalf("controlled peer requests=%d completed=%d active=%d", len(peer.Requests), peer.CompletedHandlers, peer.ActiveHandlers)
	}
	t.Log("M3_KNOWLEDGE_RETENTION controlled_tls_peer=true original_model_runtime=true credential_replacement_pin=true atomic_old_release=true original_app_join=true")
}

func knowledgeM3RetentionConfiguration(t *testing.T, base []string, ca string, origins ...string) config.Config {
	t.Helper()
	origin := "http://localhost:8080"
	if len(origins) > 1 {
		t.Fatal("one exact public origin required")
	}
	if len(origins) == 1 {
		origin = origins[0]
	}
	env := append(slices.Clone(base), "AGENTEAM_CENTRAL_PUBLIC_ORIGIN="+origin, "AGENTEAM_CENTRAL_SHUTDOWN_TIMEOUT=5s", "AGENTEAM_CENTRAL_EXECUTION_RUNTIME="+strings.Replace(executionSchedulerAppConfig, "direct-text-v1", "tool-loop-v1", 1), `AGENTEAM_CENTRAL_RETRIEVAL_RUNTIME={"backend":"native_pg_fts","dimensions":2}`, "AGENTEAM_CENTRAL_SCHEDULER_LAUNCH_RETRY_MAX_ATTEMPTS=2", "AGENTEAM_CENTRAL_SCHEDULER_LAUNCH_RETRY_INITIAL_BACKOFF=100ms", "AGENTEAM_CENTRAL_SCHEDULER_LAUNCH_RETRY_MAX_BACKOFF=1s")
	if ca != "" {
		env = append(env, "AGENTEAM_CENTRAL_OUTBOUND_CA_FILE="+ca)
	}
	cfg := guardConfiguration(t, env)
	workAppCheck(t, "knowledge-retention-configuration", cfg.Validate())
	return cfg
}

func knowledgeM3RetentionCredential(t *testing.T, v *executionSchedulerApp, value string) sc.CredentialID {
	t.Helper()
	secrets, ok := v.root.owned.secret().(*secret.Service)
	if !ok {
		t.Fatal("original Secret missing")
	}
	material, err := sc.NewSecretMaterial([]byte(value))
	workAppCheck(t, "knowledge-retention-material", err)
	identity, err := f.NewCommandIdentity("secret", []string{v.actor.Details().UserID}, "create", f.IdempotencyKey(guardID[struct{}](t).String()))
	workAppCheck(t, "knowledge-retention-secret-command", err)
	result, err := secrets.ExecuteWrite(v.ctx, sc.WriteRequest{Actor: v.actor, Scope: id.SystemScope(), Identity: identity, Kind: sc.Create, Purpose: sc.Model, Value: material})
	material.Destroy()
	workAppCheck(t, "knowledge-retention-secret-write", err)
	return result.Metadata.CredentialRef.Details().ID
}

func knowledgeM3RetentionModels(t *testing.T, v *executionSchedulerApp, scenario string) (mc.ProviderID, sc.CredentialID) {
	t.Helper()
	credential := knowledgeM3RetentionCredential(t, v, "owned-knowledge-retention")
	var embedding, memory mc.ModelID
	var embeddingProvider mc.ProviderID
	for _, profile := range []string{"embedding", "summary"} {
		protocol := mc.OpenAIEmbeddings
		ref := credential
		if profile == "summary" {
			protocol = mc.OpenAIChat
			ref = knowledgeM3RetentionCredential(t, v, "owned-knowledge-structured")
		}
		var receipt mc.CommandReceipt
		knowledgeM3SystemCall(t, v, "POST", "model-providers", map[string]any{"input": map[string]any{"name": "Knowledge retained " + profile, "protocol": protocol, "base_url": "https://fixture.test:8443/case/" + scenario, "enabled": true, "credential_ref": ref, "options": map[string]any{}}}, &receipt)
		provider, err := f.ParseID[mc.Provider](receipt.ResourceID)
		workAppCheck(t, "knowledge-retention-provider", err)
		input := knowledgeM3ModelInput(t, profile, "knowledge-retention")
		knowledgeM3SystemCall(t, v, "POST", "models", map[string]any{"provider_id": provider, "input": knowledgeM3ModelWire(input)}, &receipt)
		model, err := f.ParseID[mc.Model](receipt.ResourceID)
		workAppCheck(t, "knowledge-retention-model", err)
		if profile == "embedding" {
			embedding, embeddingProvider = model, provider
		} else {
			memory = model
		}
	}
	var selection struct {
		ID      string    `json:"id"`
		Version f.Version `json:"version"`
	}
	knowledgeM3SystemCall(t, v, "GET", "model-selection", nil, &selection)
	var receipt mc.CommandReceipt
	knowledgeM3SystemCall(t, v, "PUT", "model-selection", map[string]any{"id": selection.ID, "expected_version": selection.Version, "embedding": embedding, "summary": memory, "reranker": nil, "image": nil}, &receipt)
	return embeddingProvider, credential
}

func knowledgeM3RetentionFacts(t *testing.T, v *executionSchedulerApp, profile, state, retention string, credential sc.CredentialID) {
	t.Helper()
	var records, references int
	workAppCheck(t, "knowledge-serving-retention-facts", v.admin.QueryRow(v.ctx, `SELECT
 (SELECT count(*) FROM agenteam_knowledge.index_profiles p JOIN agenteam_model.serving_snapshot_retentions r ON r.project_id=p.project_id AND r.generation_id=p.id AND r.snapshot_id=p.snapshot_id WHERE p.project_id=$1 AND p.id=$2 AND p.state=$3 AND p.retention_state=$4 AND r.consumer_kind='knowledge' AND r.state=$4 AND r.credential_id=$5 AND r.owner_version=p.version AND p.source_operation_id IS NOT NULL AND ($4<>'released' OR r.released_version=p.version)),
 (SELECT count(*) FROM agenteam_model.serving_snapshot_retentions r JOIN agenteam_secret.secret_references s ON s.owner_id=r.reference_id AND s.credential_id=r.credential_id AND s.consumer='model' WHERE r.project_id=$1 AND r.generation_id=$2 AND r.credential_id=$5)`, v.a.String(), profile, state, retention, credential.String()).Scan(&records, &references))
	want := 0
	if retention == "retained" {
		want = 1
	}
	if records != 1 || references != want {
		t.Fatalf("generation retention records=%d references=%d expected=%d", records, references, want)
	}
}

func knowledgeM3AppAuthorized(t *testing.T, agentSources bool) {
	t.Helper()
	var transport *outboundRuntime
	v := newExecutionSchedulerAppWith(t, func(d *dependencies, v *executionSchedulerApp) {
		if agentSources {
			trace := &knowledgeM3CaptureTrace{}
			v.ctx = context.WithValue(v.ctx, knowledgeM3CaptureTraceKey{}, trace)
			v.store.observeCommit = trace.observe
		}
		v.root.cfg = knowledgeM3Configuration(t, guardEnvironment(t, v.root.db), []string{os.Getenv("DEV_EMBEDDING_BASE_URL"), os.Getenv("DEV_CHAT_BASE_URL")}, agentSources)
		d.outboundConstruct = func(_ context.Context, _ config.Config, actual *outboundRuntime) error {
			// No resolver replacement, allow rule, CA bypass, or bare HTTP client.
			if actual.resolver != nil {
				return f.NewFault(f.Forbidden, f.NotStarted)
			}
			transport = actual
			return nil
		}
	})
	if transport == nil || v.root.owned.outbound() != transport {
		t.Fatal("original outbound owner missing")
	}
	v.assembly.mu.Lock()
	documents, ok := v.assembly.knowledge.(*knowledgeCallAssembly)
	v.assembly.mu.Unlock()
	if !ok || documents == nil || documents.pair == nil || documents.index == nil || v.runtime.modelRuntime == nil {
		t.Fatal("original M3 Knowledge/Model graph missing")
	}
	chat := knowledgeM3ConfigureModels(t, v, agentSources)
	const first = "Mercury is the smallest planet in the Solar System. Its orbital period is 88 Earth days."
	const second = "# Observatory\n\nThe telescope maintenance window starts at 14:00 UTC on Friday.\n\n## Calibration\n\nUse the Vega reference star for the optical calibration."
	source, err := kc.NewTextSource(kc.PlainText, first)
	workAppCheck(t, "knowledge-source", err)
	one, err := documents.pair.Service().CreateDocument(v.ctx, v.actor, executionSchedulerAppMeta(t, nil), kc.CreateRequest{ProjectID: v.a, DocumentID: guardID[kc.Document](t), Title: "Mercury fact"}, source)
	workAppCheck(t, "knowledge-actual-create", err)
	source, err = kc.NewTextSource(kc.Markdown, second)
	workAppCheck(t, "knowledge-markdown-source", err)
	two, err := documents.pair.Service().CreateDocument(v.ctx, v.actor, executionSchedulerAppMeta(t, nil), kc.CreateRequest{ProjectID: v.a, DocumentID: guardID[kc.Document](t), Title: "Observatory guide"}, source)
	workAppCheck(t, "knowledge-actual-create-markdown", err)
	result, err := documents.index.Rebuild(v.ctx, v.actor, v.a)
	if err != nil {
		knowledgeM3IndexDiagnostic(t, v, err)
	}
	workAppCheck(t, "knowledge-real-rebuild", err)
	if result.ProfileID == "" || result.Documents != 2 || result.Chunks < 2 {
		t.Fatal("complete first profile was not published")
	}
	var active, ready, jobs, chunks, dimensions, operations, unretired, leases int
	err = v.admin.QueryRow(v.ctx, `SELECT
 (SELECT count(*) FROM agenteam_knowledge.index_state st JOIN agenteam_knowledge.index_profiles p ON p.project_id=st.project_id AND p.id=st.serving_profile_id WHERE st.project_id=$1 AND p.id=$2 AND p.state='active' AND p.snapshot_id IS NOT NULL AND p.dimension=4096),
 (SELECT count(*) FROM agenteam_knowledge.index_generations WHERE project_id=$1 AND profile_id=$2 AND state='ready' AND lexical_complete AND dense_complete),
 (SELECT count(*) FROM agenteam_knowledge.index_jobs WHERE project_id=$1 AND profile_id=$2 AND state='succeeded'),
 (SELECT count(*) FROM agenteam_knowledge.index_chunks c JOIN agenteam_knowledge.index_generations g ON g.project_id=c.project_id AND g.id=c.generation_id WHERE c.project_id=$1 AND g.profile_id=$2),
 (SELECT count(*) FROM agenteam_knowledge.index_chunks c JOIN agenteam_knowledge.index_generations g ON g.project_id=c.project_id AND g.id=c.generation_id WHERE c.project_id=$1 AND g.profile_id=$2 AND vector_dims(c.embedding)=4096),
 (SELECT count(*) FROM agenteam_knowledge.model_operations WHERE project_id=$1 AND profile_id=$2),
 (SELECT count(*) FROM agenteam_knowledge.model_operations WHERE project_id=$1 AND profile_id=$2 AND state<>'retired'),
 (SELECT count(*) FROM agenteam_object.object_leases WHERE object_id IN($3,$4) AND owner_kind='reader' AND state='active')`, v.a.String(), result.ProfileID, one.ObjectID.String(), two.ObjectID.String()).Scan(&active, &ready, &jobs, &chunks, &dimensions, &operations, &unretired, &leases)
	workAppCheck(t, "knowledge-real-index-facts", err)
	if active != 1 || ready != 2 || jobs != 2 || chunks != result.Chunks || dimensions != chunks || operations != 3 || unretired != 0 || leases != 0 {
		t.Fatal("generation, original model retirement or Object Close facts differ")
	}
	var sameSnapshot, matchingSource bool
	err = v.admin.QueryRow(v.ctx, `SELECT
 (SELECT count(DISTINCT snapshot_id)=1 FROM agenteam_knowledge.model_operations WHERE project_id=$1 AND profile_id=$2),
 (SELECT count(*)=2 AND bool_and(g.document_version=d.content_version AND d.current_object_id=j.object_id) FROM agenteam_knowledge.index_generations g JOIN agenteam_knowledge.documents d ON d.project_id=g.project_id AND d.id=g.document_id JOIN agenteam_knowledge.index_jobs j ON j.project_id=g.project_id AND j.id=g.job_id WHERE g.project_id=$1 AND g.profile_id=$2)`, v.a.String(), result.ProfileID).Scan(&sameSnapshot, &matchingSource)
	workAppCheck(t, "knowledge-immutable-profile-source", err)
	if !sameSnapshot || !matchingSource {
		t.Fatal("index source or embedding profile drifted")
	}
	var actualCalls int
	var runtimeJoined bool
	err = v.admin.QueryRow(v.ctx, `SELECT count(*),COALESCE(bool_and(c.phase='succeeded' AND c.retired AND l.released AND i.final_status='succeeded' AND i.dispatch='sent' AND i.consumer_kind='knowledge' AND i.purpose='knowledge_embedding' AND i.operation_id=m.id AND i.input_data#>>'{value,digest}'=m.input_digest AND i.identity_data#>>'{value,adapter_revision}'='openai-embeddings-float-v2' AND (SELECT count(*) FROM agenteam_model.runtime_attempts WHERE call_id=c.id)=1 AND NOT EXISTS(SELECT 1 FROM agenteam_model.usage_project_calls u WHERE u.call_id=c.id AND u.phase<>'business_closed')),false) FROM agenteam_knowledge.model_operations m JOIN agenteam_model.calls c ON c.id=m.call_id AND c.project_id=m.project_id JOIN agenteam_model.invocations i ON i.id=c.invocation_id JOIN agenteam_secret.secret_leases l ON l.id=c.lease_id WHERE m.project_id=$1 AND m.profile_id=$2 AND m.kind='index'`, v.a.String(), result.ProfileID).Scan(&actualCalls, &runtimeJoined)
	workAppCheck(t, "knowledge-original-model-secret-usage", err)
	if actualCalls != 2 || !runtimeJoined {
		t.Fatal("original v2 Model/Secret/Usage call facts did not close")
	}
	// Original private domain authorization rejects naked or foreign calls;
	// this assertion never constructs a synthetic successful Tool witness.
	if _, err = documents.index.QueryDocuments(v.ctx, kc.Invocation{}, kc.QueryRequest{Query: "Mercury orbital period", Limit: 5}); err == nil {
		t.Fatal("naked invocation searched knowledge")
	}
	if agentSources {
		knowledgeM3AgentSources(t, v, chat, one, first)
	}
	documents.index.StopAdmission()
	drain, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	err = documents.index.Drain(drain)
	cancel()
	workAppCheck(t, "knowledge-index-original-drain", err)
	v.requestStop()
	v.store.releaseDirectory()
	usageAppWait(t, v.root.done, "knowledge-original-app-return")
	workAppCheck(t, "knowledge-original-app-exit", v.root.err)
	if !v.assembly.Joined() || !v.root.owned.producersJoined() {
		t.Fatal("App returned before original Knowledge/Model owners joined")
	}
	taskExecutionGuardReleased(t, v.workAppFixture)
	t.Logf("M3_KNOWLEDGE_INDEX actual_object_read_close=true actual_model_runtime=true expected_dimensions=4096 immutable_snapshot=true atomic_serving=true original_app_join=true agent_tool_consumption=%t", agentSources)
}

func knowledgeM3IndexDiagnostic(t *testing.T, v *executionSchedulerApp, original error) {
	t.Helper()
	var storeFailure *postgres.Error
	if errors.As(original, &storeFailure) {
		t.Logf("M3_KNOWLEDGE_INDEX_STORE code=%s sqlstate=%s", storeFailure.Code(), storeFailure.SQLState())
	}
	var database *pgconn.PgError
	if errors.As(original, &database) {
		// SQLSTATE and source position expose no bound arguments, source text,
		// credentials, provider body, or raw database diagnostic message.
		t.Logf("M3_KNOWLEDGE_INDEX_SQL sqlstate=%s position=%d", database.Code, database.Position)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	var profiles, jobs, operations, calls int
	if err := v.admin.QueryRow(ctx, `SELECT (SELECT count(*) FROM agenteam_knowledge.index_profiles WHERE project_id=$1),(SELECT count(*) FROM agenteam_knowledge.index_jobs WHERE project_id=$1),(SELECT count(*) FROM agenteam_knowledge.model_operations WHERE project_id=$1),(SELECT count(*) FROM agenteam_model.calls WHERE project_id=$1)`, v.a.String()).Scan(&profiles, &jobs, &operations, &calls); err == nil {
		t.Logf("M3_KNOWLEDGE_INDEX_PROGRESS profiles=%d jobs=%d operations=%d model_calls=%d", profiles, jobs, operations, calls)
	}
}

// This preflight shares the complete override construction with the live top.
// Config-only infrastructure values are never used to run an App or model call.
func TestKnowledgeM3AppConfiguration(t *testing.T) {
	base := append(objectfixture.ConfigOnlyEnvironment(), accountenv.New(t).Environ()...)
	base = append(base,
		"AGENTEAM_CENTRAL_DATABASE_URL=postgresql://config_user:config-password@127.0.0.1:1/config_only",
		"AGENTEAM_CENTRAL_DATABASE_TLS_MODE=disable",
		`AGENTEAM_CENTRAL_SECRET_KEYRING={"format":1,"current_version":"1","keys":[{"version":"1","key_b64":"ICEiIyQlJicoKSorLC0uLzAxMjM0NTY3ODk6Ozw9Pj8="}]}`,
		`AGENTEAM_CENTRAL_CURSOR_KEYRING={"format":1,"current_kid":"test","keys":[{"kid":"test","key_b64":"AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8="}]}`,
		`AGENTEAM_CENTRAL_KNOWLEDGE_CONFIRMATION_KEYRING={"format":1,"current_kid":"knowledge","keys":[{"kid":"knowledge","key_b64":"gIGCg4SFhoeIiYqLjI2Oj5CRkpOUlZaXmJmam5ydnp8="}]}`)
	retained := knowledgeM3RetentionConfiguration(t, base, "")
	if settings, ok := retained.RetrievalRuntime(); !ok || settings.Dimensions() != 2 {
		t.Fatal("controlled retention configuration missing explicit dimensions")
	}
	pdf := knowledgeM3RetentionConfiguration(t, knowledgeM3PDFEnvironment(base), "")
	if parser, enabled := pdf.KnowledgeParser(); !enabled || parser.Validate() != nil {
		t.Fatal("explicit PDF configuration missing shape-only parser binding")
	}
	_ = knowledgeM3AgentRequest(t, guardID[id.Agent](t), guardID[mc.Model](t))
	// Validate the exact inputs passed by live setup, including the distinct
	// structured selector and the currently supported ToolLoop profile.
	embedding := knowledgeM3ModelInput(t, "embedding", "fixture-embedding")
	memory := knowledgeM3ModelInput(t, "summary", "fixture-chat")
	agent := knowledgeM3ModelInput(t, "agent", "fixture-chat")
	if embedding.Type != mc.EmbeddingModel || memory.Type != mc.ChatModel || memory.Capabilities.ToolCalls || !slices.Equal(memory.Capabilities.StructuredOutputModes, []string{"text", "json_schema"}) || !agent.Capabilities.ToolCalls || agent.Capabilities.ParallelToolCalls || agent.Capabilities.Reasoning || !slices.Equal(agent.Capabilities.StructuredOutputModes, []string{"text"}) || !slices.Equal(agent.Capabilities.InputModalities, []string{"text"}) || !slices.Equal(agent.Capabilities.OutputModalities, []string{"text"}) {
		t.Fatal("Knowledge platform structured and Agent ToolLoop profiles overlap")
	}
	for _, input := range []mc.ModelInput{embedding, memory, agent} {
		raw, err := json.Marshal(knowledgeM3ModelWire(input))
		workAppCheck(t, "knowledge-model-wire", err)
		var encoded struct {
			Capabilities map[string]json.RawMessage `json:"capabilities"`
		}
		workAppCheck(t, "knowledge-model-wire-counts", json.Unmarshal(raw, &encoded))
		if _, ok := encoded.Capabilities["context_length"]; !ok {
			t.Fatal("Knowledge Model HTTP context_length was omitted")
		}
		if _, ok := encoded.Capabilities["max_output"]; !ok {
			t.Fatal("Knowledge Model HTTP max_output was omitted")
		}
	}
	for _, agentSources := range []bool{false, true} {
		cfg := knowledgeM3Configuration(t, base, []string{"https://model.example/v1", "https://model.example/v1"}, agentSources)
		runtime, enabled := cfg.ExecutionRuntime()
		retrieval, indexed := cfg.RetrievalRuntime()
		_, retry := cfg.SchedulerLaunchRetryPolicy()
		if !enabled || !runtime.ToolLoop() || !indexed || retrieval.Dimensions() != 4096 || !retry {
			t.Fatal("complete Knowledge App configuration or origin deduplication differs")
		}
	}
}

func knowledgeM3Configuration(t *testing.T, base []string, endpoints []string, agentSources bool) config.Config {
	t.Helper()
	var origins []string
	for _, endpoint := range endpoints {
		target, err := outbound.ParseTarget(endpoint)
		workAppCheck(t, "knowledge-live-origin", err)
		if target.Origin().Scheme() != "https" {
			t.Fatal("Knowledge live origin must retain TLS")
		}
		if origin := target.Origin().String(); !slices.Contains(origins, origin) {
			origins = append(origins, origin)
		}
	}
	runtimeConfig := strings.Replace(executionSchedulerAppConfig, "direct-text-v1", "tool-loop-v1", 1)
	if agentSources {
		// Keep the real Scheduler from re-launching a completed work task
		// during this source-result observation, as in the M1 workflow fixture.
		runtimeConfig = strings.Replace(runtimeConfig, `"relaunch_skip_count":0`, `"relaunch_skip_count":240`, 1)
	}
	env := append(slices.Clone(base), "AGENTEAM_CENTRAL_PUBLIC_ORIGIN=http://localhost:8080", "AGENTEAM_CENTRAL_SHUTDOWN_TIMEOUT=5s", "AGENTEAM_CENTRAL_EXECUTION_RUNTIME="+runtimeConfig, `AGENTEAM_CENTRAL_RETRIEVAL_RUNTIME={"backend":"native_pg_fts","dimensions":4096}`, "AGENTEAM_CENTRAL_SCHEDULER_LAUNCH_RETRY_MAX_ATTEMPTS=2", "AGENTEAM_CENTRAL_SCHEDULER_LAUNCH_RETRY_INITIAL_BACKOFF=100ms", "AGENTEAM_CENTRAL_SCHEDULER_LAUNCH_RETRY_MAX_BACKOFF=1s")
	if ca, present := os.LookupEnv("AGENTEAM_CENTRAL_OUTBOUND_CA_FILE"); present {
		env = append(env, "AGENTEAM_CENTRAL_OUTBOUND_CA_FILE="+ca)
	}
	cfg := guardConfiguration(t, env)
	workAppCheck(t, "knowledge-complete-configuration", cfg.Validate())
	return cfg
}

func knowledgeM3SystemCall(t *testing.T, v *executionSchedulerApp, method, path string, body any, out any) {
	t.Helper()
	var raw []byte
	if body != nil {
		raw = workAppJSON(t, body)
	}
	response, err := v.request(v.ctx, method, "/api/v1/system/"+path, raw, guardID[struct{}](t).String())
	clear(raw)
	workAppCheck(t, "knowledge-model-http", err)
	if response.status != http.StatusOK && response.status != http.StatusCreated {
		var problem struct {
			Code f.Code `json:"code"`
		}
		_ = json.Unmarshal(response.body, &problem)
		t.Fatalf("Knowledge model setup route=%s status=%d code=%s", path, response.status, problem.Code.Safe())
	}
	if out != nil {
		workAppCheck(t, "knowledge-model-response", json.Unmarshal(response.body, out))
	}
	clear(response.body)
}
func knowledgeM3ConfigureModels(t *testing.T, v *executionSchedulerApp, agentSources bool) mc.ModelID {
	t.Helper()
	secrets, ok := v.root.owned.secret().(*secret.Service)
	if !ok {
		t.Fatal("original Secret unavailable")
	}
	var embedding, memory, agentModel mc.ModelID
	for _, kind := range []string{"EMBEDDING", "CHAT"} {
		base, key, name := os.Getenv("DEV_"+kind+"_BASE_URL"), os.Getenv("DEV_"+kind+"_API_KEY"), os.Getenv("DEV_"+kind+"_MODEL")
		if base == "" || key == "" || name == "" {
			t.Fatal("authorized model parameters missing")
		}
		material, err := sc.NewSecretMaterial([]byte(key))
		workAppCheck(t, "knowledge-secret-material", err)
		identity, err := f.NewCommandIdentity("secret", []string{v.actor.Details().UserID}, "create", f.IdempotencyKey(guardID[struct{}](t).String()))
		workAppCheck(t, "knowledge-secret-command", err)
		credential, err := secrets.ExecuteWrite(v.ctx, sc.WriteRequest{Actor: v.actor, Scope: id.SystemScope(), Identity: identity, Kind: sc.Create, Purpose: sc.Model, Value: material})
		material.Destroy()
		workAppCheck(t, "knowledge-real-secret", err)
		protocol, profile := mc.OpenAIChat, "summary"
		if kind == "EMBEDDING" {
			protocol, profile = mc.OpenAIEmbeddings, "embedding"
		}
		var receipt mc.CommandReceipt
		knowledgeM3SystemCall(t, v, "POST", "model-providers", map[string]any{"input": map[string]any{"name": "Knowledge M3 " + strings.ToLower(kind), "protocol": protocol, "base_url": base, "enabled": true, "credential_ref": credential.Metadata.CredentialRef.Details().ID, "options": map[string]any{}}}, &receipt)
		provider, err := f.ParseID[mc.Provider](receipt.ResourceID)
		workAppCheck(t, "knowledge-model-provider-id", err)
		input := knowledgeM3ModelInput(t, profile, name)
		knowledgeM3SystemCall(t, v, "POST", "models", map[string]any{"provider_id": provider, "input": knowledgeM3ModelWire(input)}, &receipt)
		model, err := f.ParseID[mc.Model](receipt.ResourceID)
		workAppCheck(t, "knowledge-model-id", err)
		if kind == "EMBEDDING" {
			embedding = model
		} else {
			memory = model
			if agentSources {
				// One Provider/credential, two formal immutable capability profiles.
				// The platform structured selector is never reused by ToolLoop.
				input = knowledgeM3ModelInput(t, "agent", name)
				knowledgeM3SystemCall(t, v, "POST", "models", map[string]any{"provider_id": provider, "input": knowledgeM3ModelWire(input)}, &receipt)
				agentModel, err = f.ParseID[mc.Model](receipt.ResourceID)
				workAppCheck(t, "knowledge-agent-model-id", err)
			}
		}
	}
	var selection struct {
		ID      string    `json:"id"`
		Version f.Version `json:"version"`
	}
	knowledgeM3SystemCall(t, v, "GET", "model-selection", nil, &selection)
	var receipt mc.CommandReceipt
	knowledgeM3SystemCall(t, v, "PUT", "model-selection", map[string]any{"id": selection.ID, "expected_version": selection.Version, "embedding": embedding, "summary": memory, "reranker": nil, "image": nil}, &receipt)
	if agentSources {
		if agentModel.Validate() != nil || agentModel == memory {
			t.Fatal("Knowledge Agent and structured platform Model profiles were not distinct")
		}
		return agentModel
	}
	return memory
}

func knowledgeM3ModelInput(t *testing.T, profile, upstream string) mc.ModelInput {
	t.Helper()
	input := mc.ModelInput{Name: "Knowledge M3 " + profile, ProviderModelID: upstream, Type: mc.ChatModel, Enabled: true, Parameters: json.RawMessage(`{}`), RequestOverwrite: json.RawMessage(`{}`), HeaderOverwrite: map[string]string{}, Capabilities: mc.Capabilities{InputModalities: []string{"text"}, OutputModalities: []string{"text"}, ReasoningEfforts: []string{}, StructuredOutputModes: []string{}}}
	switch profile {
	case "embedding":
		input.Type = mc.EmbeddingModel
		input.Capabilities.OutputModalities = []string{"vector"}
	case "summary":
		limit := mc.TokenCount(128)
		input.Capabilities.Streaming = true
		input.Capabilities.MaxOutput = &limit
		input.Capabilities.StructuredOutputModes = []string{"text", "json_schema"}
	case "agent":
		limit := mc.TokenCount(512)
		input.Capabilities.Streaming = true
		input.Capabilities.ToolCalls = true
		input.Capabilities.MaxOutput = &limit
		input.Capabilities.StructuredOutputModes = []string{"text"}
	default:
		t.Fatal("unknown Knowledge fixture model profile")
	}
	workAppCheck(t, "knowledge-formal-model-input", input.Validate())
	return input
}

// The HTTP configuration DTO requires explicit nullable counts; the domain
// capabilities omit nil counts when serialized for other uses.
func knowledgeM3ModelWire(input mc.ModelInput) any {
	type capabilities struct {
		mc.Capabilities
		ContextLength *mc.TokenCount `json:"context_length"`
		MaxOutput     *mc.TokenCount `json:"max_output"`
	}
	return struct {
		mc.ModelInput
		Capabilities capabilities `json:"capabilities"`
	}{input, capabilities{input.Capabilities, input.Capabilities.ContextLength, input.Capabilities.MaxOutput}}
}

func knowledgeM3AgentRequest(t *testing.T, agentID id.AgentID, model mc.ModelID) ac.AgentCreate {
	t.Helper()
	disabled := false
	const instructions = "Answer the assigned knowledge question using exactly two sequential tools. First use query-doc to search Mercury orbital period, limit 5. Then use read-doc with the document_id and exact document_version from the Mercury fact search hit. Do not answer from memory or from the search snippet alone. Do not invoke any other tools. After reading the source, answer briefly in English and include a citation exactly as [document_id=<returned id>; document_version=<returned version>]."
	agent, err := ac.NewAgentCreate(ac.AgentCreateFields{AgentID: agentID, Name: "knowledge-source-reader", Description: "Reads original project knowledge", Instructions: instructions, ModelRef: model, ApprovalPolicy: ac.ApprovalAllow, AllowedToolIDs: []id.ToolID{}, AllowedMountIDs: []id.MountID{}, AllowedSecretVariableIDs: []id.ProjectVariableID{}, AddSkillsEnabled: &disabled, InstallSkillEnabled: &disabled})
	workAppCheck(t, "knowledge-agent-request", err)
	return agent
}

// The original Scheduler discovers this formally created Task. The harness
// never calls ClaimTask, LaunchOnce, Advance, or constructs a Tool witness.
func knowledgeM3AgentSources(t *testing.T, v *executionSchedulerApp, model mc.ModelID, source kc.DocumentRef, text string, location ...knowledgeM3SourceLocation) {
	t.Helper()
	v.agent = guardID[id.Agent](t)
	agent := knowledgeM3AgentRequest(t, v.agent, model)
	_, err := v.assembly.agents.service.CreateAgent(v.ctx, v.actor, executionSchedulerAppMeta(t, nil), v.a, agent)
	workAppCheck(t, "knowledge-formal-agent", err)
	milestone := wc.CreateMilestoneRequest{MilestoneID: guardID[wc.Milestone](t), Title: "Knowledge source task"}
	var structure wc.StructureMutation
	v.httpSeed(t, "/milestones", map[string]any{"request": milestone}, &structure)
	if structure.Validate() != nil || structure.Milestone == nil || structure.Milestone.ID != milestone.MilestoneID {
		t.Fatal("Knowledge milestone receipt differs")
	}
	sprint := wc.CreateSprintRequest{SprintID: guardID[pc.Sprint](t), MilestoneID: milestone.MilestoneID, Title: "Knowledge source sprint"}
	v.httpSeed(t, "/sprints", map[string]any{"request": sprint}, &structure)
	if structure.Validate() != nil || structure.Sprint == nil || structure.Sprint.ID != sprint.SprintID {
		t.Fatal("Knowledge Sprint receipt differs")
	}
	sprintVersion := structure.Sprint.Version
	create := wc.TaskCreate{TaskID: guardID[wc.Task](t), SprintID: sprint.SprintID, Title: "Read Mercury's orbital period", Type: wc.TaskTypeTask, Priority: wc.TaskPriorityMedium, Plan: "Find Mercury's orbital period in this project's knowledge. Search with query-doc, read the current cited source with read-doc, then answer with its document ID and exact version. Use exactly these two tools sequentially. No filesystem work is needed."}
	var created wc.TaskMutation
	v.httpSeed(t, "/tasks", map[string]any{"request": create}, &created)
	if created.Validate() != nil || created.Task.ID != create.TaskID {
		t.Fatal("Knowledge Task receipt differs")
	}
	before, err := v.projects.service.GetSchedulerConfig(v.ctx, v.actor, v.a)
	workAppCheck(t, "knowledge-scheduler-config", err)
	limit := int64(1)
	_, err = v.projects.service.UpdateProject(v.ctx, v.actor, executionSchedulerAppMeta(t, &before.Project.Version), v.a, pc.UpdateProjectRequest{Scheduler: &pc.ProjectSchedulerConfig{Enabled: true, MaxConcurrency: &limit}})
	workAppCheck(t, "knowledge-enable-scheduler", err)
	moved, err := v.planning.transitions.TransferTask(v.ctx, v.actor, executionSchedulerAppMeta(t, &created.Task.Version), v.a, created.Task.ID, wc.TaskTransfer{TargetState: wc.TaskStateTodo, AssigneeAgentID: &v.agent, AddBlockers: []wc.TaskBlockerCreate{}, ResolveBlockerIDs: []wc.TaskBlockerID{}})
	workAppCheck(t, "knowledge-ready-task", err)
	_, err = v.planning.sprints.StartSprint(v.ctx, v.actor, executionSchedulerAppMeta(t, &sprintVersion), v.a, sprint.SprintID)
	workAppCheck(t, "knowledge-start-sprint", err)
	v.store.releaseDirectory()
	execution := knowledgeM3AwaitAgent(t, v, moved.Task)
	defer func() {
		if t.Failed() {
			knowledgeM3AgentDiagnostic(t, v, moved.Task.ID)
		}
	}()
	knowledgeM3CheckSources(t, v, execution, source, text, location...)
	response := taskExecutionGet(t, v.workAppFixture, v.client, v.cookie, "GET", taskExecutionReadPath(v.a, moved.Task.ID.String(), execution)+"/runtime-view", http.StatusOK)
	var view ec.TaskRuntimeView
	workAppCheck(t, "knowledge-agent-public-view", json.Unmarshal(response.body, &view))
	workAppCheck(t, "knowledge-agent-public-contract", view.ValidateProjection())
	if view.Execution.ID != execution || view.Execution.AgentID != v.agent || view.Execution.Status != ec.Succeeded || view.Availability != ec.RuntimeAvailable || view.Snapshot == nil || len(view.Snapshot.Items) != 1 {
		t.Fatal("Knowledge Agent final projection absent")
	}
	answer := view.Snapshot.Items[0].Data.Content
	if !strings.Contains(answer, "88") || !strings.Contains(answer, source.ID.String()) || !strings.Contains(answer, "document_version="+strconv.FormatInt(int64(source.ContentVersion), 10)) {
		t.Fatal("Knowledge Agent answer omitted the read fact or exact source citation")
	}
	current, err := v.planning.taskReader.GetTask(v.ctx, v.actor, v.a, moved.Task.ID)
	workAppCheck(t, "knowledge-scheduler-task-state", err)
	if current.State != wc.TaskStateInProgress || current.Version != moved.Task.Version+1 {
		t.Fatal("Knowledge Task did not retain original Scheduler claim")
	}
	t.Log("M3_KNOWLEDGE_AGENT scheduler_claim=true actual_model_tools=true query_then_versioned_read=true original_tool_results_consumed=true source_citation=true")
}

func knowledgeM3AwaitAgent(t *testing.T, v *executionSchedulerApp, task wc.Task) id.ExecutionID {
	t.Helper()
	tick := time.NewTicker(50 * time.Millisecond)
	defer tick.Stop()
	var count int
	var execution, status string
	for {
		var preparationFailed bool
		err := v.admin.QueryRow(v.ctx, `WITH selected AS (
 SELECT id,status FROM agenteam_execution.executions WHERE project_id=$1 AND launch_request->'trigger'->>'task_id'=$2
 ) SELECT count(*),COALESCE(min(id),''),COALESCE(min(status),''),EXISTS(
 SELECT 1 FROM selected e JOIN agenteam_execution.preparation_claims c ON c.execution_id=e.id
 JOIN agenteam_execution.preparation_attempts a ON (a.execution_id,a.attempt_id,a.process_id,a.fence)=(c.execution_id,c.attempt_id,c.process_id,c.fence)
 WHERE e.status='preparing' AND a.phase='terminal' AND NOT EXISTS(SELECT 1 FROM agenteam_execution.preparation_inputs p WHERE p.execution_id=e.id)) FROM selected`, v.a.String(), task.ID.String()).Scan(&count, &execution, &status, &preparationFailed)
		if err != nil || count > 1 || count == 1 && ec.Status(status).Terminal() && status != string(ec.Succeeded) {
			knowledgeM3AgentDiagnostic(t, v, task.ID)
			t.Fatalf("Knowledge Agent did not complete: execution_count=%d status=%s read_failed=%t", count, status, err != nil)
		}
		if preparationFailed {
			knowledgeM3AgentDiagnostic(t, v, task.ID)
			t.Fatal("Knowledge Agent original preparation returned without captured input")
		}
		if count == 1 && status == string(ec.Succeeded) {
			key, err := f.ParseID[id.Execution](execution)
			workAppCheck(t, "knowledge-original-execution", err)
			return key
		}
		for _, project := range v.runtime.manager.Observation().Projects {
			if project.ProjectID == v.a && !project.Running && !project.Retained && !project.Stopping && project.Err != nil {
				knowledgeM3AgentDiagnostic(t, v, task.ID)
				t.Fatal("Knowledge Agent project runner stopped before completion")
			}
		}
		select {
		case <-v.ctx.Done():
			knowledgeM3AgentDiagnostic(t, v, task.ID)
			t.Fatal("Knowledge Agent exceeded the original App observation budget")
		case <-tick.C:
		}
	}
}

// Parsed file text may retain separators beyond the actual chunk range. Both
// the full read and the distinct exact query range are checked independently.
type knowledgeM3SourceLocation struct {
	Snippet string
	Page    int
}

func knowledgeM3CheckSources(t *testing.T, v *executionSchedulerApp, execution id.ExecutionID, source kc.DocumentRef, text string, locations ...knowledgeM3SourceLocation) {
	t.Helper()
	location := knowledgeM3SourceLocation{Snippet: text}
	if len(locations) == 1 {
		location = locations[0]
	} else if len(locations) != 0 {
		t.Fatal("ambiguous source location")
	}
	rows, err := v.admin.Query(v.ctx, `SELECT o.operation_kind,b.body_data,a.original,r.round_ordinal,next.content,o.call_id,r.id
 FROM agenteam_tool.operations o JOIN agenteam_tool.builtin_results b ON b.operation_id=o.operation_id AND b.attempt_id=o.active_attempt_id
 JOIN agenteam_tool.attempts attempt ON attempt.attempt_id=b.attempt_id AND attempt.retired
 JOIN agenteam_execution.tool_calls c ON c.execution_id=o.execution_id AND c.invocation_id=o.invocation_id AND c.call_id=o.call_id
 JOIN agenteam_execution.tool_call_arguments a ON a.execution_id=c.execution_id AND a.id=c.payload_id
 JOIN agenteam_execution.rounds r ON r.execution_id=c.execution_id AND r.id=c.round_id
 JOIN agenteam_execution.loop_tool_results result ON result.execution_id=r.execution_id AND result.round_id=r.id AND result.operation_id=o.operation_id
 JOIN agenteam_execution.rounds next ON next.execution_id=r.execution_id AND next.predecessor_round_id=r.id
 JOIN agenteam_model.invocations invocation ON invocation.call_id::text=next.call_id AND invocation.execution_id::text=next.execution_id
 AND invocation.input_data#>>'{value,round_id}'=next.id AND invocation.input_data#>>'{value,digest}'=next.input_digest
 AND invocation.final_status='succeeded' AND invocation.dispatch='sent'
 WHERE o.execution_id=$1 AND o.phase='succeeded' AND o.outcome='success' ORDER BY r.round_ordinal`, execution.String())
	workAppCheck(t, "knowledge-original-tool-successors", err)
	defer rows.Close()
	count := 0
	for rows.Next() {
		var kind, call, predecessor string
		var body, args, next []byte
		var ordinal int
		workAppCheck(t, "knowledge-tool-source-row", rows.Scan(&kind, &body, &args, &ordinal, &next, &call, &predecessor))
		count++
		if ordinal != count {
			t.Fatal("Knowledge source tool order changed")
		}
		switch count {
		case 1:
			var out kc.QueryResult
			workAppCheck(t, "knowledge-query-result", json.Unmarshal(body, &out))
			found := false
			for _, hit := range out.Hits {
				if hit.DocumentID == source.ID && hit.DocumentVersion == source.ContentVersion && hit.Title == source.Title && hit.MediaType == source.MediaType && hit.Snippet == location.Snippet {
					var locator chunker.Locator
					workAppCheck(t, "knowledge-query-source-locator", json.Unmarshal(hit.Locator, &locator))
					found = locator.Ordinal == 1 && locator.Page == location.Page && locator.ByteStart == 0 && locator.ByteEnd == len(location.Snippet) && locator.ElementStart > 0 && locator.ElementEnd >= locator.ElementStart
				}
			}
			if kind != "knowledge.query" || !found || out.Degraded {
				t.Fatal("Knowledge query omitted the current source locator or degraded")
			}
		case 2:
			var in struct {
				Document kc.DocumentID `json:"document_id"`
				Version  f.Version     `json:"document_version"`
			}
			var out kc.DocumentContent
			workAppCheck(t, "knowledge-read-original-arguments", json.Unmarshal(args, &in))
			workAppCheck(t, "knowledge-read-original-result", json.Unmarshal(body, &out))
			workAppCheck(t, "knowledge-read-content-contract", out.Validate())
			if kind != "knowledge.read" || in.Document != source.ID || in.Version != source.ContentVersion || out.Document.ID != source.ID || out.Document.ContentVersion != source.ContentVersion || out.Text == nil || out.Text.Text != text || out.Text.Truncated {
				t.Fatal("Knowledge read did not bind the exact returned current source")
			}
		default:
			t.Fatal("Knowledge Agent performed extra source operations")
		}
		decoded, err := ec.DecodeLoopRound(next)
		workAppCheck(t, "knowledge-successor-loop-round", err)
		fields := decoded.Fields()
		if fields.Round.ExecutionID != execution || fields.Predecessor == nil || fields.Predecessor.String() != predecessor || int(fields.Ordinal) != ordinal+1 {
			t.Fatal("Knowledge successor lost the exact original ToolLoop predecessor")
		}
		consumed := false
		for _, message := range fields.Round.Messages {
			for _, part := range message.Parts {
				if result := part.ToolResult; result != nil && result.CallID == call && !result.IsError && len(result.Parts) == 1 && result.Parts[0].Text != nil && bytes.Equal([]byte(result.Parts[0].Text.Text), body) {
					consumed = true
				}
			}
		}
		if !consumed {
			t.Fatal("next actual Model request did not consume the original tool result")
		}
	}
	workAppCheck(t, "knowledge-tool-source-rows", rows.Err())
	if count != 2 {
		t.Fatalf("Knowledge Agent did not complete query and versioned read: joined_successful_sources=%d", count)
	}
	var launched, queries, unretired int
	workAppCheck(t, "knowledge-agent-owner-facts", v.admin.QueryRow(v.ctx, `SELECT
 (SELECT count(*) FROM agenteam_scheduler.dispatches WHERE execution_id=$1 AND status='launched'),
 (SELECT count(*) FROM agenteam_knowledge.model_operations WHERE project_id=$2::uuid AND kind='query'),
 (SELECT count(*) FROM agenteam_knowledge.model_operations WHERE project_id=$2::uuid AND state<>'retired')`, execution.String(), v.a.String()).Scan(&launched, &queries, &unretired))
	if launched != 1 || queries != 1 || unretired != 0 {
		t.Fatal("Knowledge query embedding or original Scheduler owner did not close")
	}
}

func knowledgeM3AgentDiagnostic(t *testing.T, v *executionSchedulerApp, task wc.TaskID) {
	t.Helper()
	if trace, ok := v.ctx.Value(knowledgeM3CaptureTraceKey{}).(*knowledgeM3CaptureTrace); ok {
		trace.diagnose(t)
	}
	manager := v.runtime.manager.Observation()
	t.Logf("M3_KNOWLEDGE_AGENT_MANAGER started=%t stopped=%t projects=%d", manager.Started, manager.Stopped, len(manager.Projects))
	logFault := func(stage string, err error) {
		var fault *f.Fault
		if errors.As(err, &fault) {
			t.Logf("M3_KNOWLEDGE_AGENT_MANAGER stage=%s code=%s outcome=%s", stage, fault.Code.Safe(), fault.CommitState.Safe())
		} else if err != nil {
			t.Logf("M3_KNOWLEDGE_AGENT_MANAGER stage=%s error_type=%T", stage, err)
		}
	}
	logFault("discovery", manager.DiscoveryError)
	for _, project := range manager.Projects {
		if project.ProjectID == v.a {
			t.Logf("M3_KNOWLEDGE_AGENT_MANAGER target_running=%t retained=%t stopping=%t", project.Running, project.Retained, project.Stopping)
			logFault("target-project", project.Err)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	var facts []byte
	err := v.admin.QueryRow(ctx, `WITH selected AS (SELECT id,status,version,cancel_requested_at FROM agenteam_execution.executions WHERE project_id=$1 AND launch_request->'trigger'->>'task_id'=$2)
 SELECT jsonb_build_object(
 'dispatch',(SELECT COALESCE(jsonb_agg(jsonb_build_object('status',status,'outcome',launch_outcome,'attempts',attempt_count,'reason',failure_reason)),'[]'::jsonb) FROM agenteam_scheduler.dispatches WHERE project_id=$1 AND task_id=$2),
 'execution',(SELECT COALESCE(jsonb_agg(jsonb_build_object('status',status,'version',version,'cancel_requested',cancel_requested_at IS NOT NULL)),'[]'::jsonb) FROM selected),
 'preparation',(SELECT COALESCE(jsonb_agg(jsonb_build_object('phase',phase,'fence',fence)),'[]'::jsonb) FROM agenteam_execution.preparation_attempts WHERE execution_id IN(SELECT id FROM selected)),
 'preparation_claims',(SELECT count(*) FROM agenteam_execution.preparation_claims WHERE execution_id IN(SELECT id FROM selected)),
 'preparation_inputs',(SELECT count(*) FROM agenteam_execution.preparation_inputs WHERE execution_id IN(SELECT id FROM selected)),
 'model_resolution',(SELECT COALESCE(jsonb_agg(jsonb_build_object('phase',phase,'plan_version',plan_version)),'[]'::jsonb) FROM agenteam_model.resolution_preparations WHERE owner_kind='execution' AND owner_id::text IN(SELECT id FROM selected)),
 'model_snapshots',(SELECT count(*) FROM agenteam_model.snapshots WHERE id IN(SELECT snapshot_id FROM agenteam_model.resolution_preparations WHERE owner_kind='execution' AND owner_id::text IN(SELECT id FROM selected))),
 'snapshots',(SELECT count(*) FROM agenteam_execution.snapshots WHERE execution_id IN(SELECT id FROM selected)),
 'project_calls',(SELECT COALESCE(jsonb_agg(jsonb_build_object('lane',lane,'phase',phase,'count',total)),'[]'::jsonb) FROM (SELECT lane,phase,count(*) AS total FROM agenteam_execution.project_calls WHERE project_id::text=$1 GROUP BY lane,phase) calls),
 'rounds',(SELECT COALESCE(jsonb_agg(jsonb_build_object('ordinal',round_ordinal,'tool_pending',tool_pending,'status',terminal_status,'reason',terminal_reason)),'[]'::jsonb) FROM agenteam_execution.rounds WHERE execution_id IN(SELECT id FROM selected)),
 'model',(SELECT COALESCE(jsonb_agg(jsonb_build_object('dispatch',dispatch,'status',final_status,'category',error_data->>'category','code',error_data->>'code')),'[]'::jsonb) FROM agenteam_model.invocations WHERE execution_id::text IN(SELECT id FROM selected)),
 'tool',(SELECT COALESCE(jsonb_agg(jsonb_build_object('kind',operation_kind,'phase',phase,'outcome',outcome)),'[]'::jsonb) FROM agenteam_tool.operations WHERE execution_id IN(SELECT id FROM selected)),
 'tool_calls',(SELECT count(*) FROM agenteam_execution.tool_calls WHERE execution_id IN(SELECT id FROM selected)),
 'builtin_results',(SELECT count(*) FROM agenteam_tool.builtin_results WHERE execution_id IN(SELECT id FROM selected)),
 'loop_tool_results',(SELECT count(*) FROM agenteam_execution.loop_tool_results WHERE execution_id IN(SELECT id FROM selected)),
 'round_model_requests',(SELECT count(*) FROM agenteam_execution.round_model_requests WHERE execution_id IN(SELECT id FROM selected)))`, v.a.String(), task.String()).Scan(&facts)
	if err == nil {
		t.Logf("M3_KNOWLEDGE_AGENT_SAFE_FACTS %s", facts)
	} else {
		t.Logf("M3_KNOWLEDGE_AGENT_SAFE_FACTS unavailable=%T", err)
	}
}

// Observe only the original returned transaction outcome. The bounded trace
// excludes SQL, bind values, request/cause IDs, prompts and provider contents.
// Discovery owners have their own transactions before Execution's capture Tx.
type knowledgeM3CaptureTraceKey struct{}
type knowledgeM3CaptureEvent struct {
	owner, job, command, state, code, outcome, sqlstate, table, constraint string
	canceled, deadline                                                     bool
}
type knowledgeM3CaptureTrace struct {
	mu     sync.Mutex
	events []knowledgeM3CaptureEvent
}

func knowledgeM3TechnicalIdentifier(value string) string {
	if len(value) > 128 {
		return "invalid_identifier"
	}
	for _, r := range value {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '.' || r == '_' || r == '-' {
			continue
		}
		return "invalid_identifier"
	}
	return value
}
func (o *knowledgeM3CaptureTrace) observe(cause f.TransactionCause, result f.CommitResult) {
	d := cause.Details()
	e := knowledgeM3CaptureEvent{owner: knowledgeM3TechnicalIdentifier(d.Owner), job: knowledgeM3TechnicalIdentifier(d.JobType), command: knowledgeM3TechnicalIdentifier(d.Primary.Command()), state: string(result.State())}
	if fault := result.Fault(); fault != nil {
		e.code, e.outcome = string(fault.Code.Safe()), string(fault.CommitState.Safe())
		e.canceled, e.deadline = errors.Is(fault, context.Canceled), errors.Is(fault, context.DeadlineExceeded)
		var db *postgres.Error
		if errors.As(fault, &db) {
			e.sqlstate = knowledgeM3TechnicalIdentifier(db.SQLState())
		}
		var pg *pgconn.PgError
		if errors.As(fault, &pg) {
			e.sqlstate, e.table, e.constraint = knowledgeM3TechnicalIdentifier(pg.Code), knowledgeM3TechnicalIdentifier(pg.TableName), knowledgeM3TechnicalIdentifier(pg.ConstraintName)
		}
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	if len(o.events) == 64 {
		copy(o.events, o.events[1:])
		o.events = o.events[:63]
	}
	o.events = append(o.events, e)
}
func (o *knowledgeM3CaptureTrace) diagnose(t *testing.T) {
	o.mu.Lock()
	events := slices.Clone(o.events)
	o.mu.Unlock()
	for n, e := range events {
		t.Logf("M3_KNOWLEDGE_CAPTURE_TX seq=%d owner=%s job=%s command=%s state=%s code=%s outcome=%s sqlstate=%s table=%s constraint=%s canceled=%t deadline=%t", n, e.owner, e.job, e.command, e.state, e.code, e.outcome, e.sqlstate, e.table, e.constraint, e.canceled, e.deadline)
	}
}
