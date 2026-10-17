//go:build integration

package app

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/config"
	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	kc "github.com/LunaDeerTech/agenteam/internal/central/knowledge/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/model"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	wc "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
	netfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/outbound"
)

// A single owned App checks configuration, actual management transport and
// Knowledge's real automatic index/tool consumers. Only provider replies and
// DNS are controlled; the test never creates a Model consumer witness.
func TestModelEmbeddingDimensionsIndependentReview(t *testing.T) {
	network, err := netfixture.Load()
	workAppCheck(t, "dimensions-owned-network", err)
	address, err := netip.ParseAddr(network.PrivateIP)
	workAppCheck(t, "dimensions-owned-peer-address", err)
	var transport *outboundRuntime
	v := newExecutionSchedulerAppWith(t, func(d *dependencies, v *executionSchedulerApp) {
		v.root.cfg = knowledgeM3RetentionConfiguration(t, guardEnvironment(t, v.root.db), network.CAFile)
		d.outboundConstruct = func(_ context.Context, _ config.Config, actual *outboundRuntime) error {
			actual.resolver, transport = appFixtureResolver{address}, actual
			return nil
		}
	})
	if transport == nil {
		t.Fatal("dimensions review requires the original outbound owner")
	}
	m1AllowOwnedPeer(t, v, transport, network)
	v.assembly.mu.Lock()
	documents, ok := v.assembly.knowledge.(*knowledgeCallAssembly)
	v.assembly.mu.Unlock()
	if !ok || documents.index == nil {
		t.Fatal("dimensions review requires the original automatic Knowledge index")
	}
	defer func() {
		if t.Failed() {
			knowledgeM3AutomaticDiagnostic(t, v)
		}
	}()
	r := &pickerReasoningReview{app: v, secret: "owned-dimensions-" + guardID[struct{}](t).String()}
	fixedWire := dimensionsReviewReply(t, "dimensions-fixed", 2)
	fixedCase, err := network.Create(v.ctx, netfixture.ScenarioConfig{Mode: "openai_embeddings_wire", Wire: &fixedWire})
	workAppCheck(t, "dimensions-fixed-peer", err)
	requestWire := dimensionsReviewReply(t, "dimensions-request", 3)
	hold := 0
	requestWire.HoldAfter = &hold
	requestCase, err := network.Create(v.ctx, netfixture.ScenarioConfig{Mode: "openai_embeddings_wire", Wire: &requestWire})
	workAppCheck(t, "dimensions-request-peer", err)
	// Release even on a failed assertion, before the App cleanup joins callers.
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		workAppCheck(t, "dimensions-held-peer-cleanup", network.Release(ctx, requestCase))
	}()
	wrongWire := dimensionsReviewReply(t, "dimensions-wrong", 2)
	wrongCase, err := network.Create(v.ctx, netfixture.ScenarioConfig{Mode: "openai_embeddings_wire", Wire: &wrongWire})
	workAppCheck(t, "dimensions-wrong-length-peer", err)
	documentID := guardID[kc.Document](t)
	tools := map[string]id.ToolID{}
	for _, key := range []string{"builtin:query-doc", "builtin:read-doc"} {
		var raw string
		workAppCheck(t, "dimensions-original-core-tool", v.admin.QueryRow(v.ctx, `SELECT tool_id FROM agenteam_tool.identities WHERE stable_key=$1`, key).Scan(&raw))
		tools[key], err = f.ParseID[id.Tool](raw)
		workAppCheck(t, "dimensions-original-tool-id", err)
	}
	chatCase, err := network.Create(v.ctx, netfixture.ScenarioConfig{Mode: "openai_chat_wire", WireSequence: []netfixture.WireScenario{
		knowledgeM3AutoToolReply(t, tools["builtin:query-doc"], map[string]any{"query": "Mercury orbital period", "limit": 5}, 1),
		knowledgeM3AutoToolReply(t, tools["builtin:read-doc"], map[string]any{"document_id": documentID, "document_version": "1"}, 2),
		knowledgeM3AutoReply(t, map[string]any{"role": "assistant", "content": "Mercury takes 88 Earth days. [document_id=" + documentID.String() + "; document_version=1]"}, "stop", 3),
	}})
	workAppCheck(t, "dimensions-original-query-peer", err)
	var credential struct {
		ID string `json:"credential_id"`
	}
	r.decode(t, "dimensions-owned-credential", r.call(t, http.MethodPost, "model-credentials", r.key(t), map[string]string{"value": r.secret}, http.StatusOK), &credential)
	if credential.ID == "" {
		t.Fatal("dimensions credential identity missing")
	}
	fixedProvider := r.provider(t, fixedCase, credential.ID)
	requestProvider := r.provider(t, requestCase, credential.ID)
	wrongProvider := r.provider(t, wrongCase, credential.ID)
	chatProvider := r.provider(t, chatCase, credential.ID)
	fixedModel := r.model(t, fixedProvider, dimensionsReviewInput("embedding", "dimensions-fixed", 2, "fixed"))
	requestModel := r.model(t, requestProvider, dimensionsReviewInput("embedding", "dimensions-request", 3, "request"))
	wrongModel := r.model(t, wrongProvider, dimensionsReviewInput("embedding", "dimensions-wrong", 3, "request"))
	summaryModel := r.model(t, chatProvider, dimensionsReviewInput("chat", "dimensions-unused-summary", 0, ""))
	agentInput := dimensionsReviewInput("chat", "knowledge-auto", 0, "")
	agentInput["capabilities"].(map[string]any)["tool_calls"] = true
	agentInput["capabilities"].(map[string]any)["structured_output_modes"] = []string{"text"}
	agentModel := r.model(t, chatProvider, agentInput)
	firstSelection := r.selectSummary(t, fixedModel, summaryModel, "default", "")
	for _, scenario := range []string{fixedCase, requestCase, wrongCase, chatCase} {
		state, err := network.State(v.ctx, scenario)
		workAppCheck(t, "dimensions-no-implicit-provider-call", err)
		if len(state.Requests) != 0 {
			t.Fatal("saving model metadata or selection contacted the provider")
		}
	}
	dimensionsReviewManagement(t, r, network, fixedCase, fixedProvider, fixedModel, true)
	dimensionsReviewManagement(t, r, network, wrongCase, wrongProvider, wrongModel, false)
	t.Log("MODEL_DIMENSIONS_REVIEW stage=management_fixed_and_wrong_length")

	const sourceText = "Mercury completes its orbit in 88 Earth days."
	source, err := kc.NewTextSource(kc.PlainText, sourceText)
	workAppCheck(t, "dimensions-canonical-document-source", err)
	doc, err := documents.pair.Service().CreateDocument(v.ctx, v.actor, executionSchedulerAppMeta(t, nil), kc.CreateRequest{ProjectID: v.a, DocumentID: documentID, Title: "Dimensions Mercury fact"}, source)
	workAppCheck(t, "dimensions-original-create-document", err)
	first := knowledgeM3AwaitAutomatic(t, v, doc, "", sourceText)
	firstDigest := dimensionsReviewProfile(t, v, first, fixedModel, firstSelection, 2, "fixed", "active")
	var strictProfile bool
	workAppCheck(t, "dimensions-profile-database-closed-shape", v.admin.QueryRow(v.ctx, `SELECT
 agenteam_model.valid_embedding_profile(profile_data,dimension) IS TRUE
 AND agenteam_model.valid_embedding_profile(profile_data-'dimensions',dimension) IS FALSE
 AND agenteam_model.valid_embedding_profile(jsonb_set(profile_data,'{dimensions_mode}','null'::jsonb),dimension) IS FALSE
 AND agenteam_model.valid_embedding_profile(profile_data,dimension+1) IS FALSE
 FROM agenteam_knowledge.index_profiles WHERE project_id=$1 AND id=$2`, v.a.String(), first).Scan(&strictProfile))
	if !strictProfile {
		t.Fatal("database profile validator admitted missing, null or inconsistent dimension metadata")
	}
	secondSelection := r.selectSummary(t, requestModel, summaryModel, "default", "")
	dimensionsReviewAwaitHeld(t, v, network, requestCase)
	// Two distinct configured models remain enabled. B's real indexing response
	// is held, so publication cannot replace A's serving snapshot. A real Tool
	// invocation must still query A's fixed 2D space under current authority.
	dimensionsReviewServing(t, v, first)
	modelID, err := f.ParseID[mc.Model](agentModel)
	workAppCheck(t, "dimensions-original-agent-model", err)
	execution := dimensionsReviewAgentQuery(t, v, modelID, doc)
	dimensionsReviewServing(t, v, first)
	var oldQuery int
	workAppCheck(t, "dimensions-frozen-query-snapshot", v.admin.QueryRow(v.ctx, `SELECT count(*)
 FROM agenteam_knowledge.model_operations o
 JOIN agenteam_knowledge.index_profiles p ON p.project_id=o.project_id AND p.id=o.profile_id
 JOIN agenteam_model.calls c ON c.id=o.call_id
 WHERE o.project_id=$1 AND o.kind='query' AND o.profile_id=$2 AND o.snapshot_id=p.snapshot_id
 AND p.dimension=2 AND o.state='retired' AND c.retired AND c.phase='succeeded'`, v.a.String(), first).Scan(&oldQuery))
	if oldQuery != 1 {
		t.Fatal("actual query did not consume the original serving snapshot exactly once")
	}
	t.Log("MODEL_DIMENSIONS_REVIEW stage=old_serving_query_after_selection_change")
	workAppCheck(t, "dimensions-release-new-index", network.Release(v.ctx, requestCase))
	second := knowledgeM3AwaitAutomatic(t, v, doc, first, sourceText)
	secondDigest := dimensionsReviewProfile(t, v, second, requestModel, secondSelection, 3, "request", "active")
	if first == second || firstDigest == secondDigest {
		t.Fatal("dimension/model change reused the old generation or profile digest")
	}
	if got := dimensionsReviewProfile(t, v, first, fixedModel, firstSelection, 2, "fixed", "retired"); got != firstDigest {
		t.Fatal("old profile metadata changed while publishing the new dimension")
	}
	dimensionsReviewManagement(t, r, network, requestCase, requestProvider, requestModel, true)
	// The shared source assertion includes all project operations. Run it only
	// after the deliberately held new index has published and retired.
	knowledgeM3CheckSources(t, v, execution, doc, sourceText)
	t.Log("MODEL_DIMENSIONS_REVIEW stage=new_request_dimension_published")

	v.requestStop()
	v.store.releaseDirectory()
	usageAppWait(t, v.root.done, "dimensions-original-app-return")
	workAppCheck(t, "dimensions-original-app-exit", v.root.err)
	if !documents.index.Joined() || !v.runtime.Joined() || !v.assembly.Joined() || !v.root.owned.producersJoined() {
		t.Fatal("dimension review returned before original owners joined")
	}
	taskExecutionGuardReleased(t, v.workAppFixture)
	for _, expected := range []struct {
		scenario, upstream, mode string
		dimensions, count        int
	}{{fixedCase, "dimensions-fixed", "fixed", 2, 3}, {requestCase, "dimensions-request", "request", 3, 2}, {wrongCase, "dimensions-wrong", "request", 3, 1}} {
		state, err := network.State(context.Background(), expected.scenario)
		workAppCheck(t, "dimensions-original-wire-state", err)
		if len(state.Requests) != expected.count || state.CompletedHandlers != expected.count || state.ActiveHandlers != 0 {
			t.Fatalf("dimension wire count=%d completed=%d active=%d want=%d", len(state.Requests), state.CompletedHandlers, state.ActiveHandlers, expected.count)
		}
		for _, request := range state.Requests {
			dimensionsReviewWire(t, request, expected.scenario, r.secret, expected.upstream, expected.dimensions, expected.mode)
			if !state.Closed[request.Connection] {
				t.Fatal("embedding connection remained live after original App join")
			}
		}
	}
	chatState, err := network.State(context.Background(), chatCase)
	workAppCheck(t, "dimensions-original-chat-state", err)
	if len(chatState.Requests) != 3 || chatState.CompletedHandlers != 3 || chatState.ActiveHandlers != 0 {
		t.Fatal("the original Knowledge query/read tool loop did not close its three exchanges")
	}
	var management, released, active int
	cleanup, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	workAppCheck(t, "dimensions-real-secret-retirement", v.admin.QueryRow(cleanup, `SELECT
 (SELECT count(*) FROM agenteam_model.management_operations WHERE credential_id=$1 AND result IS NOT NULL),
 (SELECT count(*) FROM agenteam_secret.secret_leases WHERE credential_id=$1 AND owner_kind='model_management_request' AND released),
 (SELECT count(*) FROM agenteam_secret.secret_leases WHERE credential_id=$1 AND NOT released)`, credential.ID).Scan(&management, &released, &active))
	if management != 3 || released != 3 || active != 0 {
		t.Fatalf("dimension calls did not release original credentials management=%d released=%d active=%d", management, released, active)
	}
	independentProxyNoSecret(t, []byte(v.root.logs.String()), r.secret)
	t.Log("MODEL_DIMENSIONS_REVIEW fixed_omitted=true request_integer=true mismatch_rejected=true replay_no_wire=true old_query_frozen=true new_space=true original_app_join=true")
}

// Keep the formal Scheduler/Task journey while the new index is held. The
// caller checks this query's retirement before releasing that index; shared
// project-wide source/owner assertions follow publication.
func dimensionsReviewAgentQuery(t *testing.T, v *executionSchedulerApp, model mc.ModelID, source kc.DocumentRef) id.ExecutionID {
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
	return execution
}

func dimensionsReviewInput(kind, upstream string, dimensions int, mode string) map[string]any {
	input := pickerReasoningInput(kind, upstream, "Independent dimension review")
	caps := input["capabilities"].(map[string]any)
	caps["embedding"] = nil
	if kind == "embedding" {
		caps["embedding"] = map[string]any{"dimensions": strconv.Itoa(dimensions), "dimensions_mode": mode}
	}
	return input
}

func dimensionsReviewReply(t *testing.T, upstream string, dimension int) netfixture.WireScenario {
	t.Helper()
	vector := make([]float64, dimension)
	vector[0] = 1
	return netfixture.WireScenario{Status: http.StatusOK, Headers: map[string]string{"Content-Type": "application/json"}, Chunks: [][]byte{workAppJSON(t, map[string]any{
		"object": "list", "model": upstream, "data": []any{map[string]any{"object": "embedding", "index": 0, "embedding": vector}},
		"usage": map[string]any{"prompt_tokens": 10, "total_tokens": 10},
	})}}
}

func dimensionsReviewManagement(t *testing.T, r *pickerReasoningReview, network *netfixture.Descriptor, scenario, provider, modelID string, success bool) {
	t.Helper()
	before, err := network.State(r.app.ctx, scenario)
	workAppCheck(t, "dimensions-management-before", err)
	body := map[string]string{"expected_version": "1", "provider_version": "1"}
	key, path := r.key(t), "models/"+modelID+"/test"
	raw := r.call(t, http.MethodPost, path, key, body, http.StatusOK)
	var result model.ManagementResult
	r.decode(t, "dimensions-management-receipt", raw, &result)
	if result.OperationID == "" || result.ProviderID != provider || result.ModelID != modelID || result.ModelVersion.String() != "1" || result.ProviderVersion.String() != "1" || result.Items != nil || result.AnswerText != nil || result.AnswerTruncated || result.DurationMS < 0 || result.DurationMS > 20000 {
		t.Fatal("embedding management returned an invalid safe receipt")
	}
	if success {
		if result.Outcome != "succeeded" || result.Reason != nil {
			t.Fatal("declared embedding dimension did not succeed")
		}
	} else if result.Outcome != "failed" || result.Reason == nil || *result.Reason != "invalid_response" {
		t.Fatal("wrong embedding length did not produce a safe invalid_response")
	}
	if !bytes.Equal(raw, r.call(t, http.MethodPost, path, key, body, http.StatusOK)) {
		t.Fatal("same-key embedding test changed the original receipt")
	}
	after, err := network.State(r.app.ctx, scenario)
	workAppCheck(t, "dimensions-management-after", err)
	if len(after.Requests) != len(before.Requests)+1 {
		t.Fatal("same-key embedding test did not perform exactly one provider request")
	}
}

func dimensionsReviewAwaitHeld(t *testing.T, v *executionSchedulerApp, network *netfixture.Descriptor, scenario string) {
	t.Helper()
	tick := time.NewTicker(25 * time.Millisecond)
	defer tick.Stop()
	for {
		state, err := network.State(v.ctx, scenario)
		workAppCheck(t, "dimensions-new-index-peer-progress", err)
		if len(state.Requests) == 1 && state.ActiveHandlers == 1 && state.CompletedHandlers == 0 {
			return
		}
		if len(state.Requests) > 1 || state.CompletedHandlers != 0 {
			t.Fatal("new index did not remain at its first controlled response")
		}
		select {
		case <-v.root.done:
			t.Fatal("original App exited before the new dimension reached its peer")
		case <-v.ctx.Done():
			t.Fatal("new dimension did not reach the peer within the original App budget")
		case <-tick.C:
		}
	}
}

func dimensionsReviewServing(t *testing.T, v *executionSchedulerApp, want string) {
	t.Helper()
	var serving string
	workAppCheck(t, "dimensions-original-serving-profile", v.admin.QueryRow(v.ctx, `SELECT serving_profile_id::text FROM agenteam_knowledge.index_state WHERE project_id=$1`, v.a.String()).Scan(&serving))
	if serving != want {
		t.Fatal("unpublished target replaced the original serving dimension")
	}
}

func dimensionsReviewProfile(t *testing.T, v *executionSchedulerApp, profile, modelID, selection string, dimension int, mode, state string) string {
	t.Helper()
	var digest string
	var exact bool
	workAppCheck(t, "dimensions-durable-profile-snapshot", v.admin.QueryRow(v.ctx, `SELECT p.profile_digest,
 p.state=$3 AND p.dimension=$4 AND p.profile_data->>'model_id'=$5 AND p.profile_data->>'selection_version'=$6
 AND p.profile_data->>'dimensions'=$4::text AND p.profile_data->>'dimensions_mode'=$7
 AND s.snapshot_data#>>'{Identity,model_id}'=$5 AND s.snapshot_data->>'SelectionVersion'=$6
 AND s.snapshot_data#>>'{Capabilities,embedding,dimensions}'=$4::text
 AND s.snapshot_data#>>'{Capabilities,embedding,dimensions_mode}'=$7
 AND EXISTS(SELECT 1 FROM agenteam_knowledge.index_generations g JOIN agenteam_knowledge.index_chunks c ON c.generation_id=g.id
 WHERE g.project_id=p.project_id AND g.profile_id=p.id AND g.dimension=$4 AND c.dimension=$4 AND vector_dims(c.embedding)=$4)
 FROM agenteam_knowledge.index_profiles p JOIN agenteam_model.snapshots s ON s.id=p.snapshot_id
 WHERE p.project_id=$1 AND p.id=$2`, v.a.String(), profile, state, dimension, modelID, selection, mode).Scan(&digest, &exact))
	if !exact || digest == "" {
		t.Fatal("durable profile, snapshot and vectors disagree on the configured dimension")
	}
	return digest
}

func dimensionsReviewWire(t *testing.T, request netfixture.Request, scenario, secret, upstream string, dimension int, mode string) {
	t.Helper()
	if request.Method != http.MethodPost || request.Path != "/case/"+scenario+"/embeddings" || request.Query != "" || request.Headers.Get("Authorization") != "Bearer "+secret || request.Headers.Get("Proxy-Authorization") != "" {
		t.Fatal("embedding request lost its fixed route or credential separation")
	}
	var body map[string]json.RawMessage
	workAppCheck(t, "dimensions-wire-json", json.Unmarshal([]byte(request.Body), &body))
	for key := range body {
		if key != "model" && key != "input" && key != "encoding_format" && key != "dimensions" {
			t.Fatal("embedding request emitted an undeclared field")
		}
	}
	var gotModel, encoding string
	workAppCheck(t, "dimensions-wire-model", json.Unmarshal(body["model"], &gotModel))
	workAppCheck(t, "dimensions-wire-encoding", json.Unmarshal(body["encoding_format"], &encoding))
	if gotModel != upstream || encoding != "float" || len(body["input"]) == 0 {
		t.Fatal("embedding request lost its actual input or fixed float encoding")
	}
	value, present := body["dimensions"]
	if mode == "fixed" {
		if present {
			t.Fatal("fixed embedding emitted a dimensions request parameter")
		}
	} else if !present || string(value) != strconv.Itoa(dimension) {
		t.Fatal("request embedding did not emit the configured integer dimension")
	}
}
