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
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/mcp"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
	mcpfixture "github.com/LunaDeerTech/agenteam/tests/mcp/fixture"
	netfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/outbound"
)

type mcpM3MediaCase struct {
	name, tool, uri, media string
	image                  bool
	body                   []byte
}

type mcpM3MediaObserver struct {
	mu                                           sync.Mutex
	media                                        mcpM3MediaCase
	readSource                                   bool
	sourceStage                                  string
	begins, unsupported, reads, closes, rejected int
	failure                                      error
}

type mcpM3MediaCaller struct {
	actual   mc.JSONCaller
	resolver oc.SourceResolver
	objects  oc.Objects
	observe  *mcpM3MediaObserver
}

type mcpM3MediaCall struct {
	mc.JSONCall
	observe *mcpM3MediaObserver
}

func (o *mcpM3MediaObserver) wrap(actual mc.JSONCaller, resolver oc.SourceResolver, objects oc.Objects) mc.JSONCaller {
	if actual == nil || resolver == nil || objects == nil {
		return nil
	}
	return &mcpM3MediaCaller{actual: actual, resolver: resolver, objects: objects, observe: o}
}

func (o *mcpM3MediaObserver) outcome(err error) {
	if !mcpM3FaultCode(err, f.CapabilityUnsupported) {
		return
	}
	o.mu.Lock()
	o.unsupported++
	o.mu.Unlock()
}

func (c *mcpM3MediaCaller) BeginChat(ctx context.Context, request mc.ModelRequest) (mc.JSONCall, error) {
	c.observe.mu.Lock()
	c.observe.begins++
	c.observe.mu.Unlock()
	if c.observe.readSource {
		if err := c.readOriginal(ctx, request); err != nil {
			c.observe.mu.Lock()
			c.observe.failure = err
			c.observe.mu.Unlock()
		}
	}
	// The observer neither manufactures an outcome nor replaces the original
	// request/context. Every admitted handle retains its real Result/Close/Join.
	call, err := c.actual.BeginChat(ctx, request)
	c.observe.outcome(err)
	if call != nil {
		return &mcpM3MediaCall{JSONCall: call, observe: c.observe}, err
	}
	return nil, err
}

func (c *mcpM3MediaCall) Result(ctx context.Context) (mc.ModelResponse, error) {
	response, err := c.JSONCall.Result(ctx)
	c.observe.outcome(err)
	return response, err
}

func (c *mcpM3MediaCaller) readOriginal(ctx context.Context, request mc.ModelRequest) error {
	var parts []*mc.MediaPart
	for _, message := range request.Messages {
		for _, part := range message.Parts {
			if part.ToolResult == nil {
				continue
			}
			for _, result := range part.ToolResult.Parts {
				if result.File != nil {
					parts = append(parts, result.File)
				}
				if result.Image != nil {
					parts = append(parts, result.Image)
				}
			}
		}
	}
	if len(parts) == 0 {
		return nil
	}
	if len(parts) != 1 || parts[0].Ref.Validate() != nil || parts[0].Ref.Details().Kind != oc.MCPFile || parts[0].MediaType != c.observe.media.media {
		return errors.New("original media input mismatch")
	}
	ref := parts[0].Ref
	c.sourceStage("bare-model-context")
	if _, err := c.resolver.Resolve(context.Background(), request.Actor, ref); err == nil || !mcpM3FaultCode(err, f.Forbidden) {
		return errors.New("source without original model context was not denied")
	}
	changed := ref.Details()
	foreign, err := f.NewID[id.Project]()
	if err != nil {
		return err
	}
	changed.ProjectID = foreign
	otherProject, err := oc.NewBusinessFileRef(changed)
	if err != nil {
		return err
	}
	changed = ref.Details()
	file, err := f.NewID[f.Request]()
	if err != nil {
		return err
	}
	changed.FileID = file.String()
	otherFile, err := oc.NewBusinessFileRef(changed)
	if err != nil {
		return err
	}
	for _, substituted := range []oc.BusinessFileRef{otherProject, otherFile} {
		c.sourceStage("substituted-reference")
		if _, err := c.resolver.Resolve(ctx, request.Actor, substituted); err == nil || !mcpM3FaultCode(err, f.NotFound) && !mcpM3FaultCode(err, f.Forbidden) {
			return errors.New("substituted source was not denied")
		}
	}
	readContext, cancel := context.WithCancel(ctx)
	defer cancel()
	c.sourceStage("original-resolve")
	source, err := c.resolver.Resolve(readContext, request.Actor, ref)
	if err != nil {
		return err
	}
	d := source.Details()
	if source.Validate() != nil || d.Reference.Details().FileID != ref.Details().FileID || d.Reference.Details().ProjectID != ref.Details().ProjectID || d.Owner.Details().Kind != oc.MCPContent || d.Meta.MediaType != c.observe.media.media {
		return errors.New("canonical source metadata mismatch")
	}
	c.sourceStage("original-read")
	reader, err := c.objects.ReadObject(readContext, request.Actor, d.Owner, d.Meta.ID, nil)
	if err != nil {
		return err
	}
	if reader == nil {
		return errors.New("original source reader absent")
	}
	defer reader.Close()
	body, err := io.ReadAll(io.LimitReader(reader, int64(len(c.observe.media.body)+1)))
	defer clear(body)
	if err != nil || !bytes.Equal(body, c.observe.media.body) || reader.Meta().ID != d.Meta.ID || reader.Range() != nil {
		return errors.New("original source body mismatch")
	}
	cancel()
	c.sourceStage("cancelled-new-read")
	// Cancellation cannot authorize another read. It also cannot erase the
	// reader's original cleanup obligation while its registered owner is alive.
	if _, err := c.resolver.Resolve(readContext, request.Actor, ref); err == nil {
		return errors.New("cancelled source admitted a new read")
	}
	c.sourceStage("original-close")
	if err = reader.Close(); err != nil && !errors.Is(err, context.Canceled) {
		return err
	}
	c.observe.mu.Lock()
	c.observe.reads++
	c.observe.closes++
	c.observe.rejected += 4
	c.observe.mu.Unlock()
	return nil
}

func (c *mcpM3MediaCaller) sourceStage(stage string) {
	c.observe.mu.Lock()
	c.observe.sourceStage = stage
	c.observe.mu.Unlock()
}

func (o *mcpM3MediaObserver) check(t *testing.T) {
	t.Helper()
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.failure != nil {
		mcpM3SafeCause(t, o.failure)
		t.Fatalf("mcp-stage=media-source source_stage=%s original test consumer failed", o.sourceStage)
	}
	wantReads, wantRejects := 0, 0
	if o.readSource {
		wantReads, wantRejects = 1, 4
	}
	if o.begins != 2 || o.unsupported != 1 || o.reads != wantReads || o.closes != wantReads || o.rejected != wantRejects {
		t.Fatalf("mcp-stage=media-original-model begins=%d unsupported=%d reads=%d closes=%d denied=%d", o.begins, o.unsupported, o.reads, o.closes, o.rejected)
	}
}

func mcpM3MediaCases(t *testing.T) []mcpM3MediaCase {
	t.Helper()
	png, err := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+ip1sAAAAASUVORK5CYII=")
	workAppCheck(t, "mcp-media-png-input", err)
	wav := []byte{'R', 'I', 'F', 'F', 38, 0, 0, 0, 'W', 'A', 'V', 'E', 'f', 'm', 't', ' ', 16, 0, 0, 0, 1, 0, 1, 0, 0x40, 0x1f, 0, 0, 0x80, 0x3e, 0, 0, 2, 0, 16, 0, 'd', 'a', 't', 'a', 2, 0, 0, 0, 0, 0}
	return []mcpM3MediaCase{
		{name: "binary-resource", tool: "echo", uri: "repo://owned/binary", media: "application/octet-stream", body: []byte{0, 1, 2, 3, 128, 129, 254, 255}},
		{name: "image-resource", tool: "echo", uri: "repo://owned/image", media: "image/png", image: true, body: png},
		{name: "image-tool", tool: "owned_image", media: "image/png", image: true, body: png},
		{name: "audio-tool", tool: "owned_audio", media: "audio/wav", body: wav},
	}
}

// A text-only Model must reject the actual canonical media input. Neither a
// fixture-supplied final answer nor an inline base64 projection can satisfy
// this test. Positive private Model source access is a separate acceptance gate.
func TestMCPM3MediaCanonicalUnsupported(t *testing.T) {
	mcpM3RunMedia(t, false)
}

// The production Model source, not the forwarding source observer below,
// must read the persisted MCP object and send its bytes on the successor wire.
func TestMCPM3MediaCanonicalSupported(t *testing.T) {
	mcpM3ConfigurationPreflight(t)
	peer := mcpfixture.New(t)
	network, err := netfixture.Load()
	workAppCheck(t, "mcp-media-supported-model", err)
	for _, media := range mcpM3MediaCases(t) {
		if media.name != "image-resource" {
			continue
		}
		t.Run(media.name, func(t *testing.T) {
			v := newMCPM3App(t, peer, network)
			tool, connection := mcpM3MediaConnection(t, v, peer, media.tool)
			var selected string
			mcpM3QueryCheck(t, "mcp-media-supported-core-tool", v.admin.QueryRow(v.ctx, `SELECT tool_id FROM agenteam_tool.identities WHERE stable_key='builtin:read-mcp-resource'`).Scan(&selected))
			toolID, err := f.ParseID[id.Tool](selected)
			workAppCheck(t, "mcp-media-supported-core-tool-id", err)
			arguments := workAppJSON(t, map[string]string{"connection_id": connection, "uri": media.uri})
			sequence := make([]netfixture.WireScenario, 0, 2)
			for n := range 2 {
				message := map[string]any{"role": "assistant", "content": mcpM3FinalText}
				finish := "stop"
				if n == 0 {
					message["content"] = nil
					message["tool_calls"] = []any{map[string]any{"id": "owned_media_call", "type": "function", "function": map[string]any{"name": "tool_" + strings.ReplaceAll(toolID.String(), "-", ""), "arguments": string(arguments)}}}
					finish = "tool_calls"
				}
				response := workAppJSON(t, map[string]any{"id": "owned-media-model", "object": "chat.completion", "created": 1, "model": "mcp-agent", "choices": []any{map[string]any{"index": 0, "message": message, "finish_reason": finish}}, "usage": map[string]int{"prompt_tokens": 10, "completion_tokens": 10, "total_tokens": 20}})
				sequence = append(sequence, netfixture.WireScenario{Status: 200, Headers: map[string]string{"Content-Type": "application/json"}, Chunks: [][]byte{response}})
			}
			scenario, err := network.Create(v.ctx, netfixture.ScenarioConfig{Mode: "openai_chat_wire", WireSequence: sequence})
			workAppCheck(t, "mcp-media-supported-sequence", err)
			task := mcpM3ScheduleTask(t, v, "https://fixture.test:8443/case/"+scenario, []id.ToolID{tool})
			mcpM3EnableMediaModel(t, v)
			v.store.releaseDirectory()
			execution := mcpM3AwaitAgent(t, v, task, peer, network, scenario)
			v.runtime.StopAdmission()
			drain, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			workAppCheck(t, "mcp-media-supported-runtime-drain", v.runtime.Drain(drain))
			cancel()
			v.requestStop()
			usageAppWait(t, v.directory.returned, "mcp-media-supported-directory")
			usageAppWait(t, v.root.done, "mcp-media-supported-app")
			workAppCheck(t, "mcp-media-supported-app-return", v.root.err)
			if !v.runtime.Joined() || !v.assembly.Joined() || !v.root.owned.producersJoined() {
				t.Fatal("mcp-stage=media-supported-join original owners not joined")
			}
			mcpM3CheckMediaCanonicalResult(t, v, execution, media, true, true)
			mcpM3CheckSupportedMediaWire(t, v, network, scenario, execution, media)
			t.Log("MCP_MEDIA supported=true actual_sdk=true canonical_object=true production_model_source=true successor_wire_bytes=true original_owners_joined=true")
		})
	}
}

func mcpM3EnableMediaModel(t *testing.T, v *executionSchedulerApp) {
	t.Helper()
	// The directory barrier still prevents Execution capture. Only locate the
	// model here; read and mutate its configuration through the public API.
	var model string
	mcpM3QueryCheck(t, "mcp-media-model-locate", v.admin.QueryRow(v.ctx, `SELECT m.id::text FROM agenteam_model.models m JOIN agenteam_model.providers p ON p.id=m.provider_id WHERE p.scope='project' AND p.project_id=$1 AND m.provider_model_id='mcp-agent'`, v.a.String()).Scan(&model))
	path := "/api/v1/projects/" + v.a.String() + "/models/" + model
	var before struct {
		ID      mc.ModelID      `json:"id"`
		Input   json.RawMessage `json:"input"`
		Version f.Version       `json:"version"`
	}
	mcpM3HTTP(t, v, "media-model-get", http.MethodGet, path, nil, "", http.StatusOK, &before)
	var modelInput mc.ModelInput
	workAppCheck(t, "mcp-media-model-input", json.Unmarshal(before.Input, &modelInput))
	if before.ID.String() != model || modelInput.Validate() != nil || before.Version.Validate() != nil {
		t.Fatal("mcp-stage=media-model-get invalid current model")
	}
	// Preserve the public GET's complete DTO. The internal Capabilities type
	// omits nil context_length/max_output, while PUT requires explicit nulls.
	var input, capabilities map[string]json.RawMessage
	workAppCheck(t, "mcp-media-model-public-input", json.Unmarshal(before.Input, &input))
	workAppCheck(t, "mcp-media-model-public-capabilities", json.Unmarshal(input["capabilities"], &capabilities))
	capabilities["input_modalities"] = workAppJSON(t, []string{"text", "image", "file"})
	input["capabilities"] = workAppJSON(t, capabilities)
	var receipt mc.CommandReceipt
	mcpM3HTTP(t, v, "media-model-update", http.MethodPut, path, map[string]any{"expected_version": before.Version, "input": input}, guardID[struct{}](t).String(), http.StatusOK, &receipt)
	var after struct {
		Input   mc.ModelInput `json:"input"`
		Version f.Version     `json:"version"`
	}
	mcpM3HTTP(t, v, "media-model-confirm", http.MethodGet, path, nil, "", http.StatusOK, &after)
	if after.Version != before.Version+1 || after.Input.Validate() != nil || strings.Join(after.Input.Capabilities.InputModalities, ",") != "text,image,file" {
		t.Fatal("mcp-stage=media-model-confirm supported configuration absent")
	}
}

func mcpM3CheckSupportedMediaWire(t *testing.T, v *executionSchedulerApp, network *netfixture.Descriptor, scenario string, execution id.ExecutionID, media mcpM3MediaCase) {
	t.Helper()
	state, err := network.State(v.ctx, scenario)
	workAppCheck(t, "mcp-media-supported-wire", err)
	if len(state.Requests) != 2 || state.ActiveHandlers != 0 || state.CompletedHandlers != 2 {
		t.Fatalf("mcp-stage=media-supported-wire requests=%d active=%d completed=%d", len(state.Requests), state.ActiveHandlers, state.CompletedHandlers)
	}
	for socket := range state.Connections {
		if !state.Closed[socket] {
			t.Fatal("mcp-stage=media-supported-wire socket unjoined")
		}
	}
	var wire struct {
		Messages []struct {
			Role       string          `json:"role"`
			ToolCallID string          `json:"tool_call_id"`
			Content    json.RawMessage `json:"content"`
		} `json:"messages"`
	}
	workAppCheck(t, "mcp-media-supported-wire-decode", json.Unmarshal([]byte(state.Requests[1].Body), &wire))
	toolIndex, images := -1, 0
	for n, message := range wire.Messages {
		if message.Role == "tool" {
			if toolIndex >= 0 || message.ToolCallID != "owned_media_call" {
				t.Fatal("mcp-stage=media-supported-wire original tool identity changed")
			}
			toolIndex = n
		}
		var parts []struct {
			Type     string `json:"type"`
			Text     string `json:"text"`
			ImageURL *struct {
				URL string `json:"url"`
			} `json:"image_url"`
		}
		if len(message.Content) == 0 || message.Content[0] != '[' {
			continue
		}
		workAppCheck(t, "mcp-media-supported-wire-parts", json.Unmarshal(message.Content, &parts))
		for p, part := range parts {
			if part.ImageURL == nil {
				continue
			}
			if message.Role != "user" || n != toolIndex+1 || toolIndex < 0 || p != 1 || len(parts) != 2 || parts[0].Type != "text" || parts[0].Text != "untrusted tool attachment (tool_call_id: owned_media_call)" || part.Type != "image_url" || !strings.HasPrefix(part.ImageURL.URL, "data:image/png;base64,") {
				t.Fatal("mcp-stage=media-supported-wire attachment provenance or order changed")
			}
			body, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(part.ImageURL.URL, "data:image/png;base64,"))
			matches := err == nil && bytes.Equal(body, media.body)
			clear(body)
			if !matches {
				t.Fatal("mcp-stage=media-supported-wire original PNG bytes absent")
			}
			images++
		}
	}
	if toolIndex < 0 || images != 1 {
		t.Fatal("mcp-stage=media-supported-wire expected one original media result")
	}
	var calls, distinct int
	var succeeded bool
	mcpM3QueryCheck(t, "mcp-media-supported-model-facts", v.admin.QueryRow(v.ctx, `SELECT count(*),count(DISTINCT call_id),COALESCE(bool_and(final_status='succeeded' AND dispatch='sent'),false) FROM agenteam_model.invocations WHERE execution_id=$1::uuid AND project_id=$2::uuid`, execution.String(), v.a.String()).Scan(&calls, &distinct, &succeeded))
	if calls != 2 || distinct != 2 || !succeeded {
		t.Fatalf("mcp-stage=media-supported-model-facts calls=%d distinct=%d succeeded=%t", calls, distinct, succeeded)
	}
}

// This forwarding test consumer borrows the real registered Model context. It
// validates the SourceResolver/Object permission boundary, not a supported
// production Model media profile. Every call still reaches the original Model.
func TestMCPM3ModelSourceReadClose(t *testing.T) {
	mcpM3RunMedia(t, true)
}

func mcpM3RunMedia(t *testing.T, readSource bool) {
	t.Helper()
	mcpM3ConfigurationPreflight(t)
	peer := mcpfixture.New(t)
	network, err := netfixture.Load()
	workAppCheck(t, "mcp-media-owned-model", err)
	for _, media := range mcpM3MediaCases(t) {
		t.Run(media.name, func(t *testing.T) {
			observer := &mcpM3MediaObserver{readSource: readSource, media: media}
			v := newMCPM3App(t, peer, network, func(d *dependencies) {
				d.mcpModel = observer.wrap
			})
			tool, connection := mcpM3MediaConnection(t, v, peer, media.tool)
			selected := tool
			arguments := map[string]string{}
			if media.uri != "" {
				var raw string
				mcpM3QueryCheck(t, "mcp-media-core-tool", v.admin.QueryRow(v.ctx, `SELECT tool_id FROM agenteam_tool.identities WHERE stable_key='builtin:read-mcp-resource'`).Scan(&raw))
				selected, err = f.ParseID[id.Tool](raw)
				workAppCheck(t, "mcp-media-core-tool-id", err)
				arguments = map[string]string{"connection_id": connection, "uri": media.uri}
			}
			response := workAppJSON(t, map[string]any{"id": "owned-media-model", "object": "chat.completion", "created": 1, "model": "mcp-agent", "choices": []any{map[string]any{"index": 0, "message": map[string]any{"role": "assistant", "content": nil, "tool_calls": []any{map[string]any{"id": "owned_media_call", "type": "function", "function": map[string]any{"name": "tool_" + strings.ReplaceAll(selected.String(), "-", ""), "arguments": string(workAppJSON(t, arguments))}}}}, "finish_reason": "tool_calls"}}, "usage": map[string]int{"prompt_tokens": 10, "completion_tokens": 10, "total_tokens": 20}})
			scenario, err := network.Create(v.ctx, netfixture.ScenarioConfig{Mode: "openai_chat_wire", Wire: &netfixture.WireScenario{Status: 200, Headers: map[string]string{"Content-Type": "application/json"}, Chunks: [][]byte{response}}})
			workAppCheck(t, "mcp-media-model-sequence", err)
			task := mcpM3ScheduleTask(t, v, "https://fixture.test:8443/case/"+scenario, []id.ToolID{tool})
			v.store.releaseDirectory()
			execution := mcpM3AwaitMediaRejection(t, v, task.ID.String(), observer)
			mcpM3CheckMediaCanonical(t, v, execution, media, readSource)
			observer.check(t)
			state, err := network.State(v.ctx, scenario)
			workAppCheck(t, "mcp-media-actual-wire", err)
			if len(state.Requests) != 1 || state.ActiveHandlers != 0 || state.CompletedHandlers != 1 {
				t.Fatalf("mcp-stage=media-capability requests=%d active=%d completed=%d", len(state.Requests), state.ActiveHandlers, state.CompletedHandlers)
			}
			for socket := range state.Connections {
				if !state.Closed[socket] {
					t.Fatal("mcp-stage=media-model-socket unjoined")
				}
			}
			v.runtime.StopAdmission()
			drain, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			workAppCheck(t, "mcp-media-runtime-drain", v.runtime.Drain(drain))
			cancel()
			v.requestStop()
			usageAppWait(t, v.directory.returned, "mcp-media-original-directory")
			usageAppWait(t, v.root.done, "mcp-media-original-app")
			workAppCheck(t, "mcp-media-original-app-return", v.root.err)
			if !v.runtime.Joined() || !v.assembly.Joined() || !v.root.owned.producersJoined() {
				t.Fatal("mcp-stage=media-join original owners not joined")
			}
			t.Logf("MCP_MEDIA kind=%s actual_sdk=true canonical_ref=true successor_persisted=true capability_unsupported=true model_wire1=true source_test_consumer=%t original_owners_joined=true supported_media_consumer=false", media.name, readSource)
		})
	}
}

func mcpM3MediaConnection(t *testing.T, v *executionSchedulerApp, peer *mcpfixture.Provider, name string) (id.ToolID, string) {
	t.Helper()
	material, err := peer.Credential()
	workAppCheck(t, "mcp-media-secret", err)
	credential := mcpM3Secret(t, v, sc.MCP, material)
	base := "/api/v1/projects/" + v.a.String() + "/mcp"
	var created, connected mcp.CommandResult
	mcpM3HTTP(t, v, "media-config", http.MethodPost, base+"/configs", map[string]any{"expected_version": "0", "input": map[string]any{"name": "Canonical media", "description": "owned SDK media", "endpoint": peer.Endpoint() + "/protected", "authentication": map[string]string{"scheme": "bearer"}}}, guardID[struct{}](t).String(), http.StatusOK, &created)
	if created.Config == nil {
		t.Fatal("mcp-stage=media-config receipt absent")
	}
	mcpM3HTTP(t, v, "media-connect", http.MethodPost, base+"/connections/connect", map[string]any{"config_id": created.Config.ID, "credential_id": credential, "expected_version": "0"}, guardID[struct{}](t).String(), http.StatusOK, &connected)
	if connected.Connection == nil || connected.Connection.State != "connected" || connected.Connection.ToolsState != "ready" || connected.Connection.ResourcesState != "ready" {
		t.Fatal("mcp-stage=media-connect discovery incomplete")
	}
	connection := connected.Connection.ID.String()
	var directory struct {
		Items []struct {
			ToolID     id.ToolID `json:"tool_id"`
			RemoteName string    `json:"remote_name"`
			Active     bool      `json:"active"`
		} `json:"items"`
	}
	mcpM3HTTP(t, v, "media-directory", http.MethodGet, base+"/connections/"+connection+"/tools", nil, "", http.StatusOK, &directory)
	for _, item := range directory.Items {
		if item.Active && item.RemoteName == name && item.ToolID.Validate() == nil {
			return item.ToolID, connection
		}
	}
	t.Fatal("mcp-stage=media-directory actual provider tool absent")
	return id.ToolID{}, ""
}

func mcpM3AwaitMediaRejection(t *testing.T, v *executionSchedulerApp, task string, observer *mcpM3MediaObserver) id.ExecutionID {
	t.Helper()
	ctx, cancel := context.WithTimeout(v.ctx, 40*time.Second)
	defer cancel()
	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()
	var count int
	var raw, status string
	for {
		err := v.admin.QueryRow(ctx, `SELECT count(*),COALESCE(min(id),''),COALESCE(min(status),'') FROM agenteam_execution.executions WHERE project_id=$1 AND launch_request->'trigger'->>'task_id'=$2`, v.a.String(), task).Scan(&count, &raw, &status)
		mcpM3QueryCheck(t, "mcp-media-execution-observe", err)
		if count > 1 || count == 1 && ec.Status(status).Terminal() && status != string(ec.Failed) {
			t.Fatalf("mcp-stage=media-execution count=%d status=%s", count, status)
		}
		if count == 1 && status == string(ec.Failed) {
			observer.check(t)
			execution, err := f.ParseID[id.Execution](raw)
			workAppCheck(t, "mcp-media-execution-id", err)
			return execution
		}
		select {
		case <-ctx.Done():
			for _, project := range v.runtime.manager.Observation().Projects {
				if project.ProjectID == v.a {
					mcpM3SafeCause(t, project.Err)
				}
			}
			t.Fatalf("mcp-stage=media-terminal original budget ended count=%d status=%s", count, status)
		case <-ticker.C:
		}
	}
}

func mcpM3FaultCode(err error, code f.Code) bool {
	queue := []error{err}
	for n := 0; len(queue) > 0 && n < 32; n++ {
		current := queue[0]
		queue = queue[1:]
		if current == nil {
			continue
		}
		if fault, ok := current.(*f.Fault); ok && fault.Code == code {
			return true
		}
		if joined, ok := current.(interface{ Unwrap() []error }); ok {
			parts := joined.Unwrap()
			queue = append(queue, parts[:min(len(parts), 32-len(queue))]...)
		} else if next := errors.Unwrap(current); next != nil {
			queue = append(queue, next)
		}
	}
	return false
}

func mcpM3CheckMediaCanonical(t *testing.T, v *executionSchedulerApp, execution id.ExecutionID, media mcpM3MediaCase, readSource bool) {
	t.Helper()
	mcpM3CheckMediaCanonicalResult(t, v, execution, media, readSource, false)
}

func mcpM3CheckMediaCanonicalResult(t *testing.T, v *executionSchedulerApp, execution id.ExecutionID, media mcpM3MediaCase, readSource, supported bool) {
	t.Helper()
	var raw, transcript []byte
	var reason, status string
	mcpM3QueryCheck(t, "mcp-media-successor", v.admin.QueryRow(v.ctx, `SELECT content,terminal_status,terminal_reason FROM agenteam_execution.rounds WHERE execution_id=$1 AND round_ordinal=2`, execution.String()).Scan(&raw, &status, &reason))
	wantStatus, wantReason := "failed", "model_failed"
	if supported {
		wantStatus, wantReason = "succeeded", "completed"
	}
	if status != wantStatus || reason != wantReason {
		t.Fatal("mcp-stage=media-terminal unexpected original model outcome")
	}
	round, err := ec.DecodeLoopRound(raw)
	workAppCheck(t, "mcp-media-successor-codec", err)
	mcpM3QueryCheck(t, "mcp-media-transcript", v.admin.QueryRow(v.ctx, `SELECT message FROM agenteam_execution.transcript_entries WHERE execution_id=$1 AND kind='tool_result'`, execution.String()).Scan(&transcript))
	message, err := mc.DecodeCanonicalMessage(transcript)
	workAppCheck(t, "mcp-media-transcript-codec", err)
	if message.Role != "tool" || len(message.Parts) != 1 || message.Parts[0].ToolResult == nil {
		t.Fatal("mcp-stage=media-transcript expected original Tool result")
	}
	result := message.Parts[0].ToolResult
	if result.IsError || result.CallID != "owned_media_call" || len(result.Parts) != 1 || result.Parts[0].Text != nil {
		t.Fatal("mcp-stage=media-result expected canonical media only")
	}
	part := result.Parts[0].File
	if media.image {
		part = result.Parts[0].Image
	}
	if part == nil || part.MediaType != media.media || part.Ref.Validate() != nil || part.Ref.Details().Kind != oc.MCPFile || part.Ref.Details().ProjectID != v.a {
		t.Fatal("mcp-stage=media-ref canonical identity or MIME lost")
	}
	encoded, err := mc.EncodeCanonicalMessage(message)
	workAppCheck(t, "mcp-media-ref-encoding", err)
	matches := 0
	for _, item := range round.Fields().Round.Messages {
		if item.Role == "tool" {
			actual, err := mc.EncodeCanonicalMessage(item)
			workAppCheck(t, "mcp-media-round-encoding", err)
			if bytes.Equal(actual, encoded) {
				matches++
			}
		}
	}
	if matches != 1 {
		t.Fatal("mcp-stage=media-successor canonical ref not retained exactly once")
	}
	var object, mime, digest string
	var size int64
	mcpM3QueryCheck(t, "mcp-media-object", v.admin.QueryRow(v.ctx, `SELECT c.object_id,o.media_type,o.byte_size,encode(o.sha256,'hex') FROM agenteam_mcp.materialized_contents c JOIN agenteam_mcp.materializations m ON m.id=c.materialization_id JOIN agenteam_object.objects o ON o.id=c.object_id::uuid WHERE m.execution_id=$1 AND m.project_id=$2 AND m.state='ready' AND c.content_id=$3 AND o.state='available'`, execution.String(), v.a.String(), part.Ref.Details().FileID).Scan(&object, &mime, &size, &digest))
	if mime != media.media || size != int64(len(media.body)) || digest != fmt.Sprintf("%x", sha256.Sum256(media.body)) {
		t.Fatal("mcp-stage=media-object canonical bytes metadata mismatch")
	}
	var operations, references, writers, readers, active, runtimeLeases, secretLeases int
	var success, retired, released bool
	mcpM3QueryCheck(t, "mcp-media-operation", v.admin.QueryRow(v.ctx, `SELECT count(*),COALESCE(bool_and(phase='succeeded' AND outcome='success'),false) FROM agenteam_tool.operations WHERE execution_id=$1`, execution.String()).Scan(&operations, &success))
	mcpM3QueryCheck(t, "mcp-media-object-reference", v.admin.QueryRow(v.ctx, `SELECT count(*) FROM agenteam_object.object_references WHERE object_id=$1 AND owner_kind='mcp_content' AND kind='canonical'`, object).Scan(&references))
	mcpM3QueryCheck(t, "mcp-media-object-leases", v.admin.QueryRow(v.ctx, `SELECT count(*) FILTER(WHERE owner_kind='writer' AND state='released'),count(*) FILTER(WHERE owner_kind='reader' AND state='released'),count(*) FILTER(WHERE state='active') FROM agenteam_object.object_leases WHERE object_id=$1`, object).Scan(&writers, &readers, &active))
	mcpM3QueryCheck(t, "mcp-media-runtime-leases", v.admin.QueryRow(v.ctx, `SELECT count(*),COALESCE(bool_and(retired),false) FROM agenteam_mcp.runtime_leases WHERE execution_id=$1`, execution.String()).Scan(&runtimeLeases, &retired))
	mcpM3QueryCheck(t, "mcp-media-secret-leases", v.admin.QueryRow(v.ctx, `SELECT count(*),COALESCE(bool_and(released),false) FROM agenteam_secret.secret_leases WHERE owner_kind='execution' AND owner_id=$1::uuid AND consumer='mcp'`, execution.String()).Scan(&secretLeases, &released))
	wantReaders := 0
	if readSource {
		wantReaders = 1
	}
	if operations != 1 || !success || references != 1 || writers != 1 || readers != wantReaders || active != 0 || runtimeLeases != 1 || !retired || secretLeases != 1 || !released {
		t.Fatalf("mcp-stage=media-durable-facts operations=%d success=%t references=%d writers=%d readers=%d active=%d runtimes=%d retired=%t secrets=%d released=%t", operations, success, references, writers, readers, active, runtimeLeases, retired, secretLeases, released)
	}
	if supported {
		var sources int
		mcpM3QueryCheck(t, "mcp-media-source-release", v.admin.QueryRow(v.ctx, `SELECT count(*) FROM agenteam_object.object_leases WHERE object_id=$1 AND owner_kind='source' AND state='released'`, object).Scan(&sources))
		if sources != 1 {
			t.Fatalf("mcp-stage=media-source-release released=%d", sources)
		}
	}
}

// Admin fact checks expose only their fixed stage and a validated SQLSTATE;
// SQL text, bound arguments, driver messages and returned content stay private.
func mcpM3QueryCheck(t *testing.T, stage string, err error) {
	t.Helper()
	if err != nil {
		var sql interface{ SQLState() string }
		if errors.As(err, &sql) && m2RecoverySQLState(sql.SQLState()) {
			t.Logf("MCP fact query stage=%s sqlstate=%s", stage, sql.SQLState())
		}
		mcpM3SafeCause(t, err)
	}
	workAppCheck(t, stage, err)
}
