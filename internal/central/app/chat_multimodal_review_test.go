//go:build integration && linux

package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"testing"
	"time"

	ac "github.com/LunaDeerTech/agenteam/internal/central/artifact/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/config"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mt "github.com/LunaDeerTech/agenteam/internal/central/meeting/contract"
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
	response, err := r.app.request(r.app.ctx, method, path, body, key)
	workAppCheck(t, "multimodal-original-http", err)
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
