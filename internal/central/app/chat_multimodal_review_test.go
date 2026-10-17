//go:build integration && linux

package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	ac "github.com/LunaDeerTech/agenteam/internal/central/artifact/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/config"
	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mt "github.com/LunaDeerTech/agenteam/internal/central/meeting/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/model"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	wc "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
	netfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/outbound"
)

// The independent chain uses the original App, uploaded Artifact objects and
// Task/Meeting consumers. A controlled provider observes the final wire only;
// no test observer resolves or reads media on the production consumer's behalf.
func TestChatMultimodalIndependentReview(t *testing.T) {
	network, err := netfixture.Load()
	workAppCheck(t, "multimodal-owned-network", err)
	address, err := netip.ParseAddr(network.PrivateIP)
	workAppCheck(t, "multimodal-owned-peer-address", err)
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
			actual.resolver, transport = appFixtureResolver{address}, actual
			return nil
		}
	})
	if transport == nil || v.runtime.meetings == nil {
		t.Fatal("multimodal review requires the original Meeting and Model graph")
	}
	m1AllowOwnedPeer(t, v, transport, network)
	w := &m1Workflow{app: v, network: network}
	r := &chatMultimodalReview{app: v, secret: "owned-multimodal-" + guardID[struct{}](t).String()}
	defer func() {
		if t.Failed() {
			memoryM3Diagnose(t, v, "multimodal-review")
		}
	}()
	taskID := guardID[wc.Task](t)
	var readTaskID string
	workAppCheck(t, "multimodal-original-read-task-tool", v.admin.QueryRow(v.ctx, `SELECT tool_id FROM agenteam_tool.identities WHERE stable_key='builtin:read-task'`).Scan(&readTaskID))
	readTask, err := f.ParseID[id.Tool](readTaskID)
	workAppCheck(t, "multimodal-read-task-tool-id", err)
	// Task's initial round requires a tool call. Use the original read-only
	// Core tool, then complete its real successor; Meeting begins with auto.
	agentScenario, err := network.Create(v.ctx, netfixture.ScenarioConfig{Mode: "openai_chat_wire", WireSequence: []netfixture.WireScenario{
		memoryM3ToolReply(t, readTask, map[string]any{"task_id": taskID.String()}, 1),
		memoryM3TextReply(t, "Task media confirmed.", 2),
		memoryM3TextReply(t, "Meeting media confirmed.", 3),
	}})
	workAppCheck(t, "multimodal-agent-peer", err)
	summary := string(workAppJSON(t, map[string]string{"title": "Media attachments confirmed", "goals": "Inspect the supplied documents", "decisions": "Keep original attachment identities", "unresolved": "", "facts": "One participant"}))
	summaryWire := memoryM3TextReply(t, summary, 1)
	summaryScenario, err := network.Create(v.ctx, netfixture.ScenarioConfig{Mode: "openai_chat_wire", Wire: &summaryWire})
	workAppCheck(t, "multimodal-summary-peer", err)
	// Reuse only configuration HTTP helpers; no prior test or consumer runs.
	catalog := &pickerReasoningReview{app: v, secret: r.secret}
	var credential struct {
		ID string `json:"credential_id"`
	}
	workAppCheck(t, "multimodal-owned-credential", json.Unmarshal(r.call(t, http.MethodPost, "/api/v1/system/model-credentials", guardID[struct{}](t).String(), map[string]string{"value": r.secret}, http.StatusOK), &credential))
	if credential.ID == "" {
		t.Fatal("multimodal credential has no identity")
	}
	agentProvider := catalog.provider(t, agentScenario, credential.ID)
	summaryProvider := catalog.provider(t, summaryScenario, credential.ID)
	agentInput := pickerReasoningInput("chat", "multimodal-agent", "Independent media consumer")
	agentCaps := agentInput["capabilities"].(map[string]any)
	agentCaps["tool_calls"] = true
	agentCaps["input_modalities"] = []string{"text", "image", "file"}
	agentCaps["structured_output_modes"] = []string{"text"}
	agentModel := catalog.model(t, agentProvider, agentInput)
	summaryInput := pickerReasoningInput("chat", "multimodal-summary", "Independent summary media consumer")
	summaryInput["capabilities"].(map[string]any)["input_modalities"] = []string{"text", "image", "file"}
	summaryModel := catalog.model(t, summaryProvider, summaryInput)
	// The shared platform contract requires an embedding choice. This chain
	// invokes no Memory tool or retrieval operation and must send no embedding.
	embedding := catalog.model(t, agentProvider, pickerReasoningInput("embedding", "multimodal-unused-embedding", ""))
	catalog.selectSummary(t, embedding, summaryModel, "default", "")
	modelID, err := f.ParseID[mc.Model](agentModel)
	workAppCheck(t, "multimodal-agent-model-id", err)
	agents := meetingM3CreateAgents(t, v, modelID)

	const docxMarker = "SYNTHETIC-DOCX-BODY-735104"
	picture := r.upload(t, v.a, "task-colors.png", "image/png", chatMultimodalPNG(t))
	pdf := r.upload(t, v.a, "meeting-paper.pdf", "application/pdf", knowledgeM3PDF("SYNTHETIC-PDF-BODY-419206"))
	docx := r.upload(t, v.a, "meeting-notes.docx", "application/vnd.openxmlformats-officedocument.wordprocessingml.document", knowledgeM3DOCX(t, docxMarker))
	sprint := memoryM3CreateSprint(t, v)
	var created wc.TaskMutation
	v.httpSeed(t, "/tasks", map[string]any{"request": map[string]any{
		"task_id": taskID, "sprint_id": sprint.ID, "title": "Inspect the original picture", "type": "task", "priority": "medium",
		"plan": "Identify the two colors in the supplied picture.", "attachments": []string{picture.ref.ArtifactID.String()},
	}}, &created)
	_, err = v.planning.transitions.TransferTask(v.ctx, v.actor, executionSchedulerAppMeta(t, &created.Task.Version), v.a, taskID, wc.TaskTransfer{TargetState: wc.TaskStateTodo, AssigneeAgentID: &agents[0], AddBlockers: []wc.TaskBlockerCreate{}, ResolveBlockerIDs: []wc.TaskBlockerID{}})
	workAppCheck(t, "multimodal-assign-original-task", err)
	_, err = v.planning.sprints.StartSprint(v.ctx, v.actor, executionSchedulerAppMeta(t, &sprint.Version), v.a, sprint.ID)
	workAppCheck(t, "multimodal-start-original-sprint", err)
	v.store.releaseDirectory()
	memoryM3AwaitExecution(t, v, taskID)
	t.Log("CHAT_MULTIMODAL_REVIEW stage=task_execution_completed")

	var started mt.Receipt
	meetingMemoryHTTP(t, v, http.MethodPost, "/meetings", map[string]any{
		"agents": agents[:1], "mode": "sequential", "content_nodes": []mt.InlineNode{
			{Type: "text", Text: "Read both attached documents and give a concise response."},
			{Type: "file", Reference: &mt.Reference{Kind: "file", ID: pdf.ref.ArtifactID.String(), Label: pdf.name}},
			{Type: "file", Reference: &mt.Reference{Kind: "file", ID: docx.ref.ArtifactID.String(), Label: docx.name}},
		},
	}, &started)
	if started.MeetingID.Validate() != nil || started.TurnID == nil {
		t.Fatal("multimodal Meeting lost its original receipt")
	}
	detail := meetingM3AwaitCompleted(t, v, started, nil)
	if detail.Session.Title == nil || *detail.Session.Title != "Media attachments confirmed" || detail.Session.Summary == nil || detail.Session.Summary.PendingUpdate || detail.Session.Summary.Decisions != "Keep original attachment identities" {
		t.Fatal("multimodal Meeting did not publish its original summary")
	}
	t.Log("CHAT_MULTIMODAL_REVIEW stage=meeting_and_summary_completed")
	v.runtime.StopAdmission()
	drain, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	err = v.runtime.Drain(drain)
	cancel()
	workAppCheck(t, "multimodal-original-runtime-drain", err)
	v.requestStop()
	usageAppWait(t, v.root.done, "multimodal-original-app-return")
	workAppCheck(t, "multimodal-original-app-exit", v.root.err)
	if !v.runtime.Joined() || !v.assembly.Joined() || !v.root.owned.producersJoined() {
		t.Fatal("multimodal App returned before its original owners joined")
	}
	memoryM3GuardReleased(t, v.workAppFixture)
	var calls, retired, released, readers int
	workAppCheck(t, "multimodal-original-call-retirement", v.admin.QueryRow(v.ctx, `SELECT
 (SELECT count(*) FROM agenteam_model.calls WHERE project_id=$1::uuid),
 (SELECT count(*) FROM agenteam_model.calls WHERE project_id=$1::uuid AND retired),
 (SELECT count(*) FROM agenteam_model.calls c JOIN agenteam_secret.secret_leases l ON l.id=c.lease_id WHERE c.project_id=$1::uuid AND l.released),
 (SELECT count(*) FROM agenteam_object.object_leases l JOIN agenteam_object.objects o ON o.id=l.object_id WHERE o.project_id=$1::uuid AND l.state='active' AND l.owner_kind IN ('reader','source','writer'))`, v.a.String()).Scan(&calls, &retired, &released, &readers))
	if calls != 4 || retired != 4 || released != 4 || readers != 0 {
		t.Fatalf("multimodal original owners not retired calls=%d retired=%d secret_released=%d active_io_leases=%d", calls, retired, released, readers)
	}
	agentState := w.joinedPeer(t, agentScenario, 3, 3)
	summaryState := w.joinedPeer(t, summaryScenario, 1, 1)
	chatMultimodalPNGWire(t, chatMultimodalWire(t, agentState.Requests[0], r.secret), picture)
	chatMultimodalPNGWire(t, chatMultimodalWire(t, agentState.Requests[1], r.secret), picture)
	chatMultimodalDocumentWire(t, chatMultimodalWire(t, agentState.Requests[2], r.secret), pdf, docx, docxMarker)
	chatMultimodalDocumentWire(t, chatMultimodalWire(t, summaryState.Requests[0], r.secret), pdf, docx, docxMarker)
	independentProxyNoSecret(t, []byte(v.root.logs.String()), r.secret, v.cookie, v.csrf)
	t.Log("CHAT_MULTIMODAL_REVIEW task_png_and_successor=true meeting_pdf_and_parsed_docx=true summary_media=true exact_object_bytes=true original_app_joined=true external_model=false")
}

// The administrative inline path has its own strict HTTP input and receipt.
// It shares the real media encoder/transport; it creates no Project upload.
func TestChatMultimodalInlineIndependentReview(t *testing.T) {
	r, w := chatMultimodalReviewApp(t, nil)
	v := r.app
	const marker = "INLINE-DOCX-BODY-913027"
	picture := chatMultimodalFile{name: "inline.png", media: "image/png", body: chatMultimodalPNG(t)}
	docx := chatMultimodalFile{name: "inline.docx", media: "application/vnd.openxmlformats-officedocument.wordprocessingml.document", body: knowledgeM3DOCX(t, marker)}
	// The 8192-byte boundary splits a three-byte rune. The safe projection
	// must keep valid UTF-8, while the complete response is still inspected.
	wantAnswer := strings.Repeat("a", 8191)
	wire := memoryM3TextReply(t, wantAnswer+"€tail", 1)
	scenario, err := w.network.Create(v.ctx, netfixture.ScenarioConfig{Mode: "openai_chat_wire", Wire: &wire})
	workAppCheck(t, "multimodal-inline-peer", err)
	catalog := &pickerReasoningReview{app: v, secret: r.secret}
	var credential struct {
		ID string `json:"credential_id"`
	}
	workAppCheck(t, "multimodal-inline-credential", json.Unmarshal(r.call(t, http.MethodPost, "/api/v1/system/model-credentials", guardID[struct{}](t).String(), map[string]string{"value": r.secret}, http.StatusOK), &credential))
	provider := catalog.provider(t, scenario, credential.ID)
	input := pickerReasoningInput("chat", "inline-multimodal", "Inline media boundary")
	input["capabilities"].(map[string]any)["input_modalities"] = []string{"text", "image", "file"}
	modelID := catalog.model(t, provider, input)
	path := "/api/v1/system/models/" + modelID + "/test"
	attachment := func(file chatMultimodalFile) map[string]any {
		return map[string]any{"name": file.name, "media_type": file.media, "data_base64": base64.StdEncoding.EncodeToString(file.body)}
	}
	request := func(text string, files []any) map[string]any {
		return map[string]any{"expected_version": "1", "provider_version": "1", "input": map[string]any{"text": text, "attachments": files}}
	}
	key := guardID[struct{}](t).String()
	intent := request("Read both supplied attachments.", []any{attachment(picture), attachment(docx)})
	raw := r.call(t, http.MethodPost, path, key, intent, http.StatusOK)
	var result model.ManagementResult
	workAppCheck(t, "multimodal-inline-result", json.Unmarshal(raw, &result))
	if result.OperationID == "" || result.Outcome != "succeeded" || result.Reason != nil || result.ModelID != modelID || result.ProviderID != provider || result.ModelVersion != 1 || result.ProviderVersion != 1 || result.Items != nil || result.AnswerText == nil || *result.AnswerText != wantAnswer || !utf8.ValidString(*result.AnswerText) || !result.AnswerTruncated || result.DurationMS < 0 || result.DurationMS > 120000 || result.CreatedAt.Validate() != nil {
		t.Fatal("inline media lost its bounded safe answer or original receipt")
	}
	if !bytes.Equal(raw, r.call(t, http.MethodPost, path, key, intent, http.StatusOK)) {
		t.Fatal("inline same-key replay changed its persisted receipt")
	}
	r.call(t, http.MethodPost, path, key, request("Changed input.", []any{attachment(picture), attachment(docx)}), http.StatusConflict)
	// Reject forged object selectors and case aliases before managed I/O.
	forged := attachment(picture)
	forged["object_id"] = guardID[struct{}](t).String()
	r.call(t, http.MethodPost, path, guardID[struct{}](t).String(), request("Reject forged source.", []any{forged}), http.StatusBadRequest)
	aliased := attachment(picture)
	aliased["Name"] = aliased["name"]
	delete(aliased, "name")
	r.call(t, http.MethodPost, path, guardID[struct{}](t).String(), request("Reject case alias.", []any{aliased}), http.StatusBadRequest)
	oversized := chatMultimodalFile{name: "too-large.txt", media: "text/plain", body: bytes.Repeat([]byte("x"), (1<<20)+1)}
	r.call(t, http.MethodPost, path, guardID[struct{}](t).String(), request("Reject before provider.", []any{attachment(oversized)}), http.StatusRequestEntityTooLarge)
	unsupported := chatMultimodalFile{name: "unsupported.bin", media: "application/octet-stream", body: []byte("owned unsupported input")}
	r.call(t, http.MethodPost, path, guardID[struct{}](t).String(), request("Reject unsupported format.", []any{attachment(unsupported)}), http.StatusUnprocessableEntity)
	var completed, released, active int
	workAppCheck(t, "multimodal-inline-owned-leases", v.admin.QueryRow(v.ctx, `SELECT
 (SELECT count(*) FROM agenteam_model.management_operations WHERE credential_id=$1 AND result IS NOT NULL),
 (SELECT count(*) FROM agenteam_secret.secret_leases WHERE credential_id=$1 AND owner_kind='model_management_request' AND released),
 (SELECT count(*) FROM agenteam_secret.secret_leases WHERE credential_id=$1 AND NOT released)`, credential.ID).Scan(&completed, &released, &active))
	if completed != 1 || released != 1 || active != 0 {
		t.Fatal("inline replay or rejected input acquired another provider lease", completed, released, active)
	}
	// Current authority also applies to a stored successful media receipt.
	_, err = v.admin.Exec(v.ctx, `UPDATE agenteam_account.users SET role='user' WHERE id=$1`, v.actor.Details().UserID)
	workAppCheck(t, "multimodal-inline-revoke-owned-admin", err)
	r.call(t, http.MethodPost, path, key, intent, http.StatusForbidden)
	chatMultimodalReviewStop(t, v)
	state := w.joinedPeer(t, scenario, 1, 1)
	parts := chatMultimodalWire(t, state.Requests[0], r.secret)
	chatMultimodalPNGWire(t, parts, picture)
	textMatches, files := 0, 0
	for _, part := range parts {
		if part.Type == "file" {
			files++
		}
		if part.Type == "text" && strings.Contains(part.Text, marker) {
			textMatches++
		}
	}
	if files != 0 || textMatches != 1 || strings.Contains(state.Requests[0].Body, base64.StdEncoding.EncodeToString(docx.body)) {
		t.Fatal("inline DOCX did not use the shared extracted-text path")
	}
	independentProxyNoSecret(t, []byte(v.root.logs.String()), r.secret, v.cookie, v.csrf)
	t.Log("CHAT_MULTIMODAL_INLINE_REVIEW http_input=true shared_media_wire=true bounded_utf8_answer=true same_key_single_send=true strict_and_size_rejected=true current_replay_authority=true original_app_joined=true")
}

// One Task supplies both lifecycle boundaries: editing its current attachment
// list cannot rewrite an existing successor, and cancellation after the next
// original source lease is released must prevent the successor's application
// write. EOF may release the lease before Read/Close returns; final join is
// asserted separately after the held callback is released.
func TestChatMultimodalLifecycleIndependentReview(t *testing.T) {
	gate := &chatMultimodalReadGate{seen: make(map[string]bool), hits: make(chan chatMultimodalReadHit, 2), errors: make(chan error, 1), stop: make(chan struct{})}
	defer close(gate.stop)
	r, w := chatMultimodalReviewApp(t, gate.observe)
	v := r.app
	defer func() {
		if t.Failed() {
			memoryM3Diagnose(t, v, "multimodal-lifecycle")
		}
	}()
	taskID := guardID[wc.Task](t)
	var toolID string
	workAppCheck(t, "multimodal-lifecycle-original-tool", v.admin.QueryRow(v.ctx, `SELECT tool_id FROM agenteam_tool.identities WHERE stable_key='builtin:read-task'`).Scan(&toolID))
	tool, err := f.ParseID[id.Tool](toolID)
	workAppCheck(t, "multimodal-lifecycle-tool-id", err)
	scenario, err := w.network.Create(v.ctx, netfixture.ScenarioConfig{Mode: "openai_chat_wire", WireSequence: []netfixture.WireScenario{
		memoryM3ToolReply(t, tool, map[string]any{"task_id": taskID.String()}, 1),
		memoryM3TextReply(t, "This cancelled successor must never be sent.", 2),
	}})
	workAppCheck(t, "multimodal-lifecycle-peer", err)
	catalog := &pickerReasoningReview{app: v, secret: r.secret}
	var credential struct {
		ID string `json:"credential_id"`
	}
	workAppCheck(t, "multimodal-lifecycle-credential", json.Unmarshal(r.call(t, http.MethodPost, "/api/v1/system/model-credentials", guardID[struct{}](t).String(), map[string]string{"value": r.secret}, http.StatusOK), &credential))
	provider := catalog.provider(t, scenario, credential.ID)
	input := pickerReasoningInput("chat", "lifecycle-multimodal", "Current source permission")
	caps := input["capabilities"].(map[string]any)
	caps["tool_calls"] = true
	caps["input_modalities"] = []string{"text", "image", "file"}
	modelID := catalog.model(t, provider, input)
	embedding := catalog.model(t, provider, pickerReasoningInput("embedding", "lifecycle-unused-embedding", ""))
	catalog.selectSummary(t, embedding, modelID, "default", "")
	modelIdentity, err := f.ParseID[mc.Model](modelID)
	workAppCheck(t, "multimodal-lifecycle-model-id", err)
	agents := meetingM3CreateAgents(t, v, modelIdentity)
	picture := r.upload(t, v.a, "lifecycle.png", "image/png", chatMultimodalPNG(t))
	foreign := r.upload(t, v.b, "other-project.png", "image/png", chatMultimodalPNG(t))
	sprint := memoryM3CreateSprint(t, v)
	base := "/api/v1/projects/" + v.a.String()
	foreignTask := guardID[wc.Task](t)
	r.call(t, http.MethodPost, base+"/tasks", guardID[struct{}](t).String(), map[string]any{"request": map[string]any{
		"task_id": foreignTask, "sprint_id": sprint.ID, "title": "Reject cross-project image", "type": "task", "priority": "medium", "plan": "Do not read another Project's attachment.", "attachments": []string{foreign.ref.ArtifactID.String()},
	}}, http.StatusNotFound)
	r.call(t, http.MethodPost, base+"/meetings", guardID[struct{}](t).String(), map[string]any{
		"agents": agents[:1], "mode": "sequential", "content_nodes": []mt.InlineNode{
			{Type: "text", Text: "Do not read another Project's attachment."},
			{Type: "file", Reference: &mt.Reference{Kind: "file", ID: foreign.ref.ArtifactID.String(), Label: foreign.name}},
		},
	}, http.StatusNotFound)
	var rejectedTasks, meetings int
	workAppCheck(t, "multimodal-cross-project-no-mutation", v.admin.QueryRow(v.ctx, `SELECT
 (SELECT count(*) FROM agenteam_work.tasks WHERE id=$1),
 (SELECT count(*) FROM agenteam_meeting.sessions WHERE project_id=$2)`, foreignTask.String(), v.a.String()).Scan(&rejectedTasks, &meetings))
	if rejectedTasks != 0 || meetings != 0 {
		t.Fatal("cross-project media refusal persisted a business mutation")
	}
	t.Log("CHAT_MULTIMODAL_LIFECYCLE_REVIEW stage=cross_project_refused")
	var created wc.TaskMutation
	createBody := workAppJSON(t, map[string]any{"request": map[string]any{
		"task_id": taskID, "sprint_id": sprint.ID, "title": "Frozen original image", "type": "task", "priority": "medium", "plan": "Read the original image.", "attachments": []string{picture.ref.ArtifactID.String()},
	}})
	createStarted := time.Now()
	createResponse, createErr := v.request(v.ctx, http.MethodPost, base+"/tasks", createBody, guardID[struct{}](t).String())
	clear(createBody)
	if createErr != nil {
		createElapsed := time.Since(createStarted)
		// Keep the original HTTP/App budgets and avoid logging URL, headers or
		// payloads. The shared seed helper otherwise hides every local error.
		kind := "unclassified"
		switch createErr.Error() {
		case "original HTTP body did not return":
			kind = "response_body"
		case "HTTP request identity missing":
			kind = "response_identity"
		}
		var networkError net.Error
		timedOut := errors.As(createErr, &networkError) && networkError.Timeout()
		diagnosticCtx, diagnosticCancel := context.WithTimeout(context.Background(), 2*time.Second)
		var persisted int
		diagnosticErr := v.store.QueryRow(diagnosticCtx, `SELECT count(*) FROM agenteam_work.tasks WHERE id=$1`, taskID.String()).Scan(&persisted)
		diagnosticCancel()
		t.Fatalf("multimodal Task create local failure type=%T kind=%s elapsed_ms=%d timeout=%t deadline=%t cancelled=%t app_context_done=%t task_count=%d task_count_known=%t", createErr, kind, createElapsed.Milliseconds(), timedOut, errors.Is(createErr, context.DeadlineExceeded), errors.Is(createErr, context.Canceled), v.ctx.Err() != nil, persisted, diagnosticErr == nil)
	}
	if createResponse.status != http.StatusOK {
		var problem struct {
			Code f.Code `json:"code"`
		}
		_ = json.Unmarshal(createResponse.body, &problem)
		t.Fatalf("multimodal Task create status=%d code=%s", createResponse.status, problem.Code.Safe())
	}
	workAppCheck(t, "multimodal-lifecycle-create-receipt", json.Unmarshal(createResponse.body, &created))
	_, err = v.planning.transitions.TransferTask(v.ctx, v.actor, executionSchedulerAppMeta(t, &created.Task.Version), v.a, taskID, wc.TaskTransfer{TargetState: wc.TaskStateTodo, AssigneeAgentID: &agents[0], AddBlockers: []wc.TaskBlockerCreate{}, ResolveBlockerIDs: []wc.TaskBlockerID{}})
	workAppCheck(t, "multimodal-lifecycle-assign", err)
	var objectID string
	workAppCheck(t, "multimodal-original-object-identity", v.admin.QueryRow(v.ctx, `SELECT object_id FROM agenteam_artifact.artifacts WHERE project_id=$1 AND id=$2`, v.a.String(), picture.ref.ArtifactID.String()).Scan(&objectID))
	gate.mu.Lock()
	gate.app, gate.object = v, objectID
	gate.mu.Unlock()
	_, err = v.planning.sprints.StartSprint(v.ctx, v.actor, executionSchedulerAppMeta(t, &sprint.Version), v.a, sprint.ID)
	workAppCheck(t, "multimodal-lifecycle-start", err)
	v.store.releaseDirectory()
	first := gate.await(t, v.ctx)
	state, err := w.network.State(v.ctx, scenario)
	workAppCheck(t, "multimodal-before-first-application-write", err)
	if len(state.Requests) != 0 {
		t.Fatal("source-close barrier did not precede the first application request")
	}
	path := base + "/tasks/" + taskID.String()
	var current wc.Task
	workAppCheck(t, "multimodal-lifecycle-current-task", json.Unmarshal(r.call(t, http.MethodGet, path, "", nil, http.StatusOK), &current))
	var updated wc.TaskMutation
	workAppCheck(t, "multimodal-omitted-attachments-receipt", json.Unmarshal(r.call(t, http.MethodPatch, path, guardID[struct{}](t).String(), map[string]any{"expected_version": current.Version, "request": map[string]any{"title": "Edited current title"}}, http.StatusOK), &updated))
	if len(updated.Task.Attachments) != 1 || updated.Task.Attachments[0] != picture.ref.ArtifactID {
		t.Fatal("omitted Task attachments did not retain the original selection")
	}
	workAppCheck(t, "multimodal-cleared-attachments-receipt", json.Unmarshal(r.call(t, http.MethodPatch, path, guardID[struct{}](t).String(), map[string]any{"expected_version": updated.Task.Version, "request": map[string]any{"attachments": []string{}}}, http.StatusOK), &updated))
	if updated.Task.Attachments == nil || len(updated.Task.Attachments) != 0 {
		t.Fatal("explicit empty Task attachments did not clear the current selection")
	}
	t.Log("CHAT_MULTIMODAL_LIFECYCLE_REVIEW stage=current_task_edited_before_first_write")
	close(first.resume)
	second := gate.await(t, v.ctx)
	if second.lease == first.lease {
		t.Fatal("successor reused the previous source lease")
	}
	// The second original read still targets the frozen Object even though the
	// current Task is empty. Its released lease is observed, never read for it.
	state, err = w.network.State(v.ctx, scenario)
	workAppCheck(t, "multimodal-successor-before-application-write", err)
	if len(state.Requests) != 1 {
		t.Fatal("source-close barrier did not precede the successor application request")
	}
	chatMultimodalPNGWire(t, chatMultimodalWire(t, state.Requests[0], r.secret), picture)
	var executionID string
	workAppCheck(t, "multimodal-lifecycle-original-execution", v.admin.QueryRow(v.ctx, `SELECT id FROM agenteam_execution.executions WHERE project_id=$1 AND launch_request->'trigger'->>'task_id'=$2`, v.a.String(), taskID.String()).Scan(&executionID))
	execution, err := f.ParseID[id.Execution](executionID)
	workAppCheck(t, "multimodal-lifecycle-execution-id", err)
	workAppCheck(t, "multimodal-current-cleared-task", json.Unmarshal(r.call(t, http.MethodGet, path, "", nil, http.StatusOK), &current))
	if len(current.Attachments) != 0 {
		t.Fatal("Task readback restored the frozen attachment to current state")
	}
	var detail struct {
		Execution ec.TaskExecution `json:"execution"`
	}
	executionPath := taskExecutionReadPath(v.a, taskID.String(), execution)
	workAppCheck(t, "multimodal-current-execution-version", json.Unmarshal(r.call(t, http.MethodGet, executionPath, "", nil, http.StatusOK), &detail))
	var cancelled ec.CancelResult
	workAppCheck(t, "multimodal-cancel-after-read", json.Unmarshal(r.call(t, http.MethodPost, executionPath+"/cancel", guardID[struct{}](t).String(), map[string]any{"expected_version": detail.Execution.Version, "expected_task_version": current.Version}, http.StatusOK), &cancelled))
	if cancelled.Replayed || cancelled.Receipt.Execution.ID != execution || cancelled.Receipt.Execution.CancelRequestedAt == nil || cancelled.Receipt.Execution.Version != detail.Execution.Version+1 || cancelled.Receipt.Task.Version != current.Version+1 {
		t.Fatal("read-after-cancel boundary lost its original cancellation receipt")
	}
	close(second.resume)
	chatMultimodalAwaitCancelled(t, v, execution)
	t.Log("CHAT_MULTIMODAL_LIFECYCLE_REVIEW stage=read_successor_cancelled")
	chatMultimodalReviewStop(t, v)
	w.joinedPeer(t, scenario, 1, 1)
	var readers, sourceReleases, unretired, unreleased int
	workAppCheck(t, "multimodal-lifecycle-original-joins", v.admin.QueryRow(v.ctx, `SELECT
 (SELECT count(*) FROM agenteam_object.object_leases l JOIN agenteam_object.objects o ON o.id=l.object_id WHERE o.project_id=$1 AND l.state='active' AND l.owner_kind IN ('reader','source','writer')),
 (SELECT count(*) FROM agenteam_object.object_leases WHERE object_id=$2 AND owner_kind='source' AND state='released'),
 (SELECT count(*) FROM agenteam_model.calls WHERE project_id=$1 AND NOT retired),
 (SELECT count(*) FROM agenteam_secret.secret_leases WHERE credential_id=$3 AND NOT released)`, v.a.String(), objectID, credential.ID).Scan(&readers, &sourceReleases, &unretired, &unreleased))
	if readers != 0 || sourceReleases != 2 || unretired != 0 || unreleased != 0 {
		t.Fatal("cancelled successor retained its original readers or calls", readers, sourceReleases, unretired, unreleased)
	}
	independentProxyNoSecret(t, []byte(v.root.logs.String()), r.secret, v.cookie, v.csrf)
	t.Log("CHAT_MULTIMODAL_LIFECYCLE_REVIEW cross_project_refused=true current_attachment_clear=true frozen_successor_source=true cancel_after_actual_read=true successor_http_requests=0 original_reader_and_app_joined=true")
}

type chatMultimodalReadHit struct {
	lease  string
	resume chan struct{}
}

type chatMultimodalReadGate struct {
	mu     sync.Mutex
	app    *executionSchedulerApp
	object string
	seen   map[string]bool
	hits   chan chatMultimodalReadHit
	errors chan error
	stop   chan struct{}
}

func (g *chatMultimodalReadGate) observe(cause f.TransactionCause, result f.CommitResult) {
	if cause.Details().Owner != "object" || result.State() != f.Committed {
		return
	}
	g.mu.Lock()
	v, objectID := g.app, g.object
	g.mu.Unlock()
	if v == nil || objectID == "" {
		return
	}
	// Ordinary SourceReads cleanup uses a random cleanup cause. Match the
	// exact owned Object and actual released source-lease identities instead.
	ctx, cancel := context.WithTimeout(v.ctx, 2*time.Second)
	var leases []string
	err := v.store.QueryRow(ctx, `SELECT COALESCE(array_agg(id::text ORDER BY id),ARRAY[]::text[]) FROM agenteam_object.object_leases WHERE object_id=$1 AND owner_kind='source' AND state='released'`, objectID).Scan(&leases)
	cancel()
	if err != nil {
		select {
		case g.errors <- err:
		default:
		}
		return
	}
	for _, lease := range leases {
		g.mu.Lock()
		fresh := !g.seen[lease]
		g.seen[lease] = true
		g.mu.Unlock()
		if !fresh {
			continue
		}
		hit := chatMultimodalReadHit{lease: lease, resume: make(chan struct{})}
		select {
		case g.hits <- hit:
		case <-g.stop:
			return
		case <-v.ctx.Done():
			return
		}
		// The real Store transaction has returned, but the surrounding reader
		// may still be handling EOF. This observer never opens a media reader.
		select {
		case <-hit.resume:
		case <-g.stop:
		case <-v.ctx.Done():
		}
	}
}

func (g *chatMultimodalReadGate) await(t *testing.T, parent context.Context) chatMultimodalReadHit {
	t.Helper()
	ctx, cancel := context.WithTimeout(parent, 30*time.Second)
	defer cancel()
	select {
	case hit := <-g.hits:
		return hit
	case err := <-g.errors:
		t.Fatalf("original source-release observation failed: %v", err)
	case <-ctx.Done():
		t.Fatal("original source reader did not reach its bounded release barrier")
	}
	return chatMultimodalReadHit{}
}

func chatMultimodalAwaitCancelled(t *testing.T, v *executionSchedulerApp, execution id.ExecutionID) {
	t.Helper()
	ctx, cancel := context.WithTimeout(v.ctx, 30*time.Second)
	defer cancel()
	tick := time.NewTicker(25 * time.Millisecond)
	defer tick.Stop()
	for {
		var status string
		workAppCheck(t, "multimodal-original-cancellation-terminal", v.admin.QueryRow(ctx, `SELECT status FROM agenteam_execution.executions WHERE id=$1`, execution.String()).Scan(&status))
		if ec.Status(status).Terminal() {
			if status != string(ec.Cancelled) {
				t.Fatal("original media Execution finished with an unexpected terminal state", status)
			}
			return
		}
		select {
		case <-ctx.Done():
			t.Fatal("original media cancellation did not join within its budget")
		case <-tick.C:
		}
	}
}

func chatMultimodalReviewApp(t *testing.T, observer func(f.TransactionCause, f.CommitResult)) (*chatMultimodalReview, *m1Workflow) {
	t.Helper()
	network, err := netfixture.Load()
	workAppCheck(t, "multimodal-boundary-owned-network", err)
	address, err := netip.ParseAddr(network.PrivateIP)
	workAppCheck(t, "multimodal-boundary-owned-address", err)
	var transport *outboundRuntime
	v := newExecutionSchedulerAppWith(t, func(d *dependencies, v *executionSchedulerApp) {
		env := guardEnvironment(t, v.root.db)
		for key, value := range memoryM3Settings() {
			env = append(env, "AGENTEAM_CENTRAL_"+key+"="+value)
		}
		env = append(env, "AGENTEAM_CENTRAL_OUTBOUND_CA_FILE="+network.CAFile)
		v.root.cfg = guardConfiguration(t, env)
		diagnose := m1CommitObserver(t)
		v.store.observeCommit = func(cause f.TransactionCause, result f.CommitResult) {
			diagnose(cause, result)
			if observer != nil {
				observer(cause, result)
			}
		}
		d.outboundConstruct = func(_ context.Context, _ config.Config, actual *outboundRuntime) error {
			actual.resolver, transport = appFixtureResolver{address}, actual
			return nil
		}
	})
	if transport == nil {
		t.Fatal("multimodal boundary requires original outbound assembly")
	}
	m1AllowOwnedPeer(t, v, transport, network)
	return &chatMultimodalReview{app: v, secret: "owned-multimodal-" + guardID[struct{}](t).String()}, &m1Workflow{app: v, network: network}
}

func chatMultimodalReviewStop(t *testing.T, v *executionSchedulerApp) {
	t.Helper()
	v.runtime.StopAdmission()
	// The inline-only case never launches a Project scheduler. Release the
	// constructor's original discovery hold after admission has stopped.
	v.store.releaseDirectory()
	v.store.releaseLookup()
	drain, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	err := v.runtime.Drain(drain)
	cancel()
	workAppCheck(t, "multimodal-boundary-drain", err)
	v.requestStop()
	usageAppWait(t, v.root.done, "multimodal-boundary-original-return")
	workAppCheck(t, "multimodal-boundary-original-exit", v.root.err)
	if !v.runtime.Joined() || !v.assembly.Joined() || !v.root.owned.producersJoined() {
		t.Fatal("multimodal boundary owners have not joined")
	}
	memoryM3GuardReleased(t, v.workAppFixture)
}

type chatMultimodalReview struct {
	app    *executionSchedulerApp
	secret string
}

type chatMultimodalFile struct {
	ref         ac.ArtifactRef
	name, media string
	body        []byte
}

func chatMultimodalPNG(t *testing.T) []byte {
	t.Helper()
	canvas := image.NewNRGBA(image.Rect(0, 0, 12, 12))
	for y := 0; y < 12; y++ {
		for x := 0; x < 12; x++ {
			value := color.NRGBA{R: 220, G: 30, B: 40, A: 255}
			if x >= 6 {
				value = color.NRGBA{R: 20, G: 70, B: 230, A: 255}
			}
			canvas.SetNRGBA(x, y, value)
		}
	}
	var out bytes.Buffer
	workAppCheck(t, "multimodal-synthetic-png", png.Encode(&out, canvas))
	return out.Bytes()
}

func (r *chatMultimodalReview) upload(t *testing.T, project id.ProjectID, name, media string, content []byte) chatMultimodalFile {
	t.Helper()
	// This wrapper supplies only the existing HTTP helper's client/session
	// fields. It does not create another App or substitute an Object service.
	httpFixture := &humanArtifactApp{workAppFixture: r.app.workAppFixture}
	meta := humanArtifactMeta(t)
	digest := fmt.Sprintf("sha256:%x", sha256.Sum256(content))
	input := artifactUserUploadJSON{Name: name, MediaType: media, ByteSize: strconv.Itoa(len(content)), SHA256: digest}
	body := workAppJSON(t, input)
	defer clear(body)
	artifactUserUploadState(t, artifactUserUploadHTTP(t, httpFixture, project, meta, "begin", body, nil, nil, http.StatusOK), ac.UserUploadPending)
	artifactUserUploadState(t, artifactUserUploadHTTP(t, httpFixture, project, meta, "payload", content, nil, func(request *http.Request) {
		request.Header.Set("Content-Type", media)
	}, http.StatusOK), ac.UserUploadUploaded)
	completed := artifactUserUploadHTTP(t, httpFixture, project, meta, "complete", body, nil, nil, http.StatusOK)
	defer clear(completed.body)
	var result struct {
		Artifact struct {
			Ref    ac.ArtifactRef `json:"artifact_ref"`
			Name   string         `json:"name"`
			Kind   string         `json:"kind"`
			Object struct {
				MediaType string     `json:"media_type"`
				ByteSize  f.Progress `json:"byte_size"`
				SHA256    f.Digest   `json:"sha256"`
			} `json:"object"`
		} `json:"artifact"`
	}
	workAppCheck(t, "multimodal-upload-complete", json.Unmarshal(completed.body, &result))
	a := result.Artifact
	if a.Ref.Validate() != nil || a.Ref.ProjectID != project || a.Name != name || a.Kind != string(ac.UserUpload) || a.Object.MediaType != media || a.Object.ByteSize != f.Progress(len(content)) || a.Object.SHA256.String() != digest {
		t.Fatal("multimodal upload lost its exact canonical reference or byte metadata")
	}
	independentProxyNoSecret(t, completed.body, r.secret)
	return chatMultimodalFile{ref: a.Ref, name: name, media: media, body: bytes.Clone(content)}
}

func (r *chatMultimodalReview) call(t *testing.T, method, path, key string, input any, want int) []byte {
	t.Helper()
	var body []byte
	if input != nil {
		body = workAppJSON(t, input)
	}
	defer clear(body)
	started := time.Now()
	response, err := r.app.request(r.app.ctx, method, path, body, key)
	if err != nil {
		elapsed := time.Since(started)
		kind := "unclassified"
		switch err.Error() {
		case "original HTTP body did not return":
			kind = "response_body"
		case "HTTP request identity missing":
			kind = "response_identity"
		}
		var networkError net.Error
		timedOut := errors.As(err, &networkError) && networkError.Timeout()
		diagnosticCtx, diagnosticCancel := context.WithTimeout(context.Background(), 2*time.Second)
		var waiting, blocked int
		diagnosticErr := r.app.store.QueryRow(diagnosticCtx, `SELECT
 count(*) FILTER (WHERE wait_event_type='Lock'),
 count(*) FILTER (WHERE cardinality(pg_blocking_pids(pid))>0)
 FROM pg_stat_activity WHERE datname=current_database()`).Scan(&waiting, &blocked)
		diagnosticCancel()
		t.Fatalf("multimodal HTTP local failure method=%s type=%T kind=%s elapsed_ms=%d timeout=%t deadline=%t cancelled=%t app_context_done=%t db_lock_waiters=%d db_blocked=%d db_observation_known=%t", method, err, kind, elapsed.Milliseconds(), timedOut, errors.Is(err, context.DeadlineExceeded), errors.Is(err, context.Canceled), r.app.ctx.Err() != nil, waiting, blocked, diagnosticErr == nil)
	}
	independentProxyNoSecret(t, response.body, r.secret)
	if response.status != want {
		var problem struct {
			Code f.Code `json:"code"`
		}
		_ = json.Unmarshal(response.body, &problem)
		t.Fatalf("multimodal HTTP method=%s status=%d want=%d code=%s", method, response.status, want, problem.Code.Safe())
	}
	return response.body
}

type chatMultimodalWirePart struct {
	Type     string `json:"type"`
	Text     string `json:"text"`
	ImageURL *struct {
		URL string `json:"url"`
	} `json:"image_url"`
	File *struct {
		Name string `json:"filename"`
		Data string `json:"file_data"`
		ID   string `json:"file_id"`
	} `json:"file"`
}

func chatMultimodalWire(t *testing.T, request netfixture.Request, credential string) []chatMultimodalWirePart {
	t.Helper()
	if request.Method != http.MethodPost || !strings.HasSuffix(request.Path, "/chat/completions") || request.Query != "" || request.Headers.Get("Authorization") != "Bearer "+credential || request.Headers.Get("Proxy-Authorization") != "" {
		t.Fatal("multimodal call lost the fixed Chat endpoint or credential separation")
	}
	independentProxyNoSecret(t, []byte(request.Body), credential)
	var body struct {
		Messages []struct {
			Role    string          `json:"role"`
			Content json.RawMessage `json:"content"`
		} `json:"messages"`
	}
	workAppCheck(t, "multimodal-provider-body", json.Unmarshal([]byte(request.Body), &body))
	var parts []chatMultimodalWirePart
	for _, message := range body.Messages {
		var text string
		if json.Unmarshal(message.Content, &text) == nil {
			parts = append(parts, chatMultimodalWirePart{Type: "text", Text: text})
			continue
		}
		var content []chatMultimodalWirePart
		workAppCheck(t, "multimodal-provider-content", json.Unmarshal(message.Content, &content))
		for _, part := range content {
			switch part.Type {
			case "text":
				if part.ImageURL != nil || part.File != nil {
					t.Fatal("text part acquired a media payload")
				}
			case "image_url":
				if part.ImageURL == nil || part.File != nil || part.Text != "" {
					t.Fatal("image part has an invalid native shape")
				}
			case "file":
				if part.File == nil || part.ImageURL != nil || part.Text != "" || part.File.ID != "" {
					t.Fatal("file part has an invalid native shape or remote file identity")
				}
			default:
				t.Fatal("unexpected provider media part type")
			}
		}
		parts = append(parts, content...)
	}
	return parts
}

func chatMultimodalData(t *testing.T, value string, file chatMultimodalFile) {
	t.Helper()
	prefix := "data:" + file.media + ";base64,"
	if !strings.HasPrefix(value, prefix) {
		t.Fatal("provider media was not the exact inline MIME type")
	}
	raw, err := base64.StdEncoding.Strict().DecodeString(strings.TrimPrefix(value, prefix))
	defer clear(raw)
	workAppCheck(t, "multimodal-provider-base64", err)
	if !bytes.Equal(raw, file.body) {
		t.Fatal("provider media bytes differ from the original uploaded object")
	}
}

func chatMultimodalPNGWire(t *testing.T, parts []chatMultimodalWirePart, picture chatMultimodalFile) {
	t.Helper()
	images := 0
	for _, part := range parts {
		if part.File != nil {
			t.Fatal("Task picture was encoded as a PDF file")
		}
		if part.ImageURL != nil {
			images++
			chatMultimodalData(t, part.ImageURL.URL, picture)
		}
	}
	if images != 1 {
		t.Fatal("Task did not send its single original picture", images)
	}
}

func chatMultimodalDocumentWire(t *testing.T, parts []chatMultimodalWirePart, pdf, docx chatMultimodalFile, marker string) {
	t.Helper()
	files, texts := 0, 0
	for _, part := range parts {
		if part.ImageURL != nil {
			t.Fatal("Meeting documents were reinterpreted as images")
		}
		if part.File != nil {
			files++
			if part.File.Name != pdf.name {
				t.Fatal("Meeting sent a non-PDF file through the native PDF path")
			}
			chatMultimodalData(t, part.File.Data, pdf)
		}
		if part.Type == "text" && strings.Contains(part.Text, marker) {
			texts++
		}
		if strings.Contains(part.Text, base64.StdEncoding.EncodeToString(docx.body)) {
			t.Fatal("DOCX archive was forwarded as text instead of extracted content")
		}
	}
	if files != 1 || texts != 1 {
		t.Fatalf("Meeting media paths differ PDF=%d parsed_DOCX=%d", files, texts)
	}
}
