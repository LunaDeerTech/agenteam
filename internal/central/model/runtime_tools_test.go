package model

import (
	"context"
	"encoding/json"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	wire "github.com/LunaDeerTech/agenteam/internal/central/model/adapter"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
)

func runtimeToolRequest(t *testing.T) mc.ModelRequest {
	t.Helper()
	r := agentRuntimePureRequest(t)
	r.Model.Snapshot.Identity.AdapterRevision = wire.OpenAIChatToolsRevision
	r.Model.Snapshot.Capabilities.ToolCalls = true
	r.Model.Snapshot.Capabilities.Streaming = false
	r.Messages = []mc.Message{{Role: "system", Parts: []mc.MessagePart{{Text: &mc.TextPart{Text: "system"}}}}, {Role: "user", Parts: []mc.MessagePart{{Text: &mc.TextPart{Text: "read the file"}}}}}
	r.Tools = []mc.Tool{{Name: "read_file", InputSchema: json.RawMessage(`{"type":"object"}`)}}
	r.ToolChoice = mc.ToolChoice{Kind: "required"}
	r.Input.SchemaVersion = mc.ToolCallInputSchemaVersion
	var err error
	r.Input.Digest, err = mc.ToolCallInputDigest(context.Background(), r.Messages, r.Tools[0])
	if err != nil {
		t.Fatal(err)
	}
	return r
}
func TestRuntimeToolsAcceptsFirstAndFullSuccessorInput(t *testing.T) {
	r := runtimeToolRequest(t)
	r.Model.Snapshot.Capabilities.Streaming = true
	r.Model.Snapshot.Capabilities.StructuredOutputModes = []string{"text", "json_schema"}
	if _, err := prepareRuntimeInput(context.Background(), r, wire.JSONResponse); err != nil {
		t.Fatal(err)
	}
	r.Tools = append(r.Tools, mc.Tool{Name: "write_file", InputSchema: json.RawMessage(`{}`)})
	r.Input.SchemaVersion = mc.ToolInputSchemaVersion
	var err error
	r.Input.Digest, err = mc.ToolInputDigest(context.Background(), r.Messages, r.Tools, r.ToolChoice)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := prepareRuntimeInput(context.Background(), r, wire.JSONResponse); err != nil {
		t.Fatal(err)
	}
	r.ToolChoice = mc.ToolChoice{Kind: "auto"}
	for _, name := range []string{"read_file", "write_file", "read_file"} {
		r.Messages = append(r.Messages, mc.Message{Role: "assistant", Parts: []mc.MessagePart{{ToolCall: &mc.ToolCallPart{ID: "same-provider-id", Name: name, Arguments: json.RawMessage(`{}`)}}}}, mc.Message{Role: "tool", Parts: []mc.MessagePart{{ToolResult: &mc.ToolResultPart{CallID: "same-provider-id", Parts: []mc.ResultPart{{Text: &mc.TextPart{Text: "actual result"}}}}}}})
	}
	semantic, err := mc.NewSuccessorSemantic(context.Background(), mc.SuccessorSemanticParts{Actor: r.Actor, Model: r.Model, Messages: r.Messages, Tools: r.Tools, ToolChoice: r.ToolChoice, ResponseFormat: r.ResponseFormat, RetryClass: r.RetryClass})
	if err != nil {
		t.Fatal(err)
	}
	r.Input.SchemaVersion = mc.SuccessorInputSchemaVersion
	r.Input.Digest = semantic.Digest()
	accepted, err := prepareRuntimeInput(context.Background(), r, wire.JSONResponse)
	if err != nil || len(accepted.Messages) != 8 || len(accepted.Tools) != 2 {
		t.Fatal("complete multi-round history rejected", err)
	}
	r.Messages[2].Parts[0].ToolCall.Arguments[0] = '!'
	r.Tools[0].InputSchema[0] = '!'
	if accepted.Messages[2].Parts[0].ToolCall.Arguments[0] != '{' || accepted.Tools[0].InputSchema[0] != '{' {
		t.Fatal("caller retained mutable input")
	}
}
func TestRuntimeToolsRejectsChangedAndHistoricalProfiles(t *testing.T) {
	for name, change := range map[string]func(*mc.ModelRequest){
		"digest":        func(r *mc.ModelRequest) { r.Input.Digest = hash([]byte("foreign")) },
		"facts-only":    func(r *mc.ModelRequest) { r.Model.Snapshot.Identity.AdapterRevision = toolFactsCaptureRevision },
		"text-revision": func(r *mc.ModelRequest) { r.Model.Snapshot.Identity.AdapterRevision = wire.OpenAIChatTextRevision },
		"parallel":      func(r *mc.ModelRequest) { r.Model.Snapshot.Capabilities.ParallelToolCalls = true },
		"missing-lease": func(r *mc.ModelRequest) { r.Model.CredentialLease = nil },
		"schema":        func(r *mc.ModelRequest) { r.Input.SchemaVersion = 99 },
		"schema-response": func(r *mc.ModelRequest) {
			r.Model.Snapshot.Capabilities.StructuredOutputModes = []string{"text", "json_schema"}
			r.ResponseFormat = mc.ResponseFormat{Kind: "json_schema", Name: "result", Schema: json.RawMessage(`{"type":"object"}`)}
		},
		"no-text-response": func(r *mc.ModelRequest) {
			r.Model.Snapshot.Capabilities.StructuredOutputModes = []string{"json_schema"}
		},
	} {
		t.Run(name, func(t *testing.T) {
			r := runtimeToolRequest(t)
			change(&r)
			out, err := prepareRuntimeInput(context.Background(), r, wire.JSONResponse)
			if err == nil || len(out.Messages) > 0 {
				t.Fatal("invalid tools profile accepted")
			}
			if name == "schema-response" || name == "no-text-response" {
				requireCode(t, err, f.CapabilityUnsupported)
			}
		})
	}
	r := runtimeToolRequest(t)
	_, err := prepareRuntimeInput(context.Background(), r, wire.SSEResponse)
	requireCode(t, err, f.CapabilityUnsupported)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := prepareRuntimeInput(ctx, r, wire.JSONResponse); err == nil {
		t.Fatal("cancelled input accepted")
	}
}
func TestRuntimeToolsResponseProjectionAndExplicitCapture(t *testing.T) {
	value := wire.Result{ToolCalls: []mc.ToolCallPart{{ID: "tool-1", Name: "read_file", Arguments: json.RawMessage(`{"path":"file"}`)}}}
	message := runtimeResponseMessage(value)
	if message.Validate() != nil || len(message.Parts) != 1 || message.Parts[0].ToolCall == nil {
		t.Fatal("tool response dropped")
	}
	value.ToolCalls[0].Arguments[0] = '!'
	if message.Parts[0].ToolCall.Arguments[0] != '{' {
		t.Fatal("response retained alias")
	}
	old, _, owner, _ := modelCaptureFixture(t)
	capture, err := NewToolCallingExecutionCapture(old.models, owner)
	if err != nil || capture.profile != toolCallingResolutionCapture || old.profile != defaultResolutionCapture {
		t.Fatal("capture profile leaked", err)
	}
	r := resolutionTestRequest(t)
	draft := resolutionTestDraft(t, r)
	draft.Snapshot.Capabilities.ToolCalls = true
	draft.Snapshot.Capabilities.Streaming = true
	draft.Snapshot.Identity.AdapterRevision = wire.OpenAIChatToolsRevision
	if err := toolCallingResolutionCapture.requireDraft(r, draft); err != nil {
		t.Fatal(err)
	}
	requireCode(t, defaultResolutionCapture.requireDraft(r, draft), f.CapabilityUnsupported)
	requireCode(t, toolFactsResolutionCapture.requireDraft(r, draft), f.CapabilityUnsupported)
	draft.Snapshot.Identity.AdapterRevision = toolFactsCaptureRevision
	requireCode(t, toolCallingResolutionCapture.requireDraft(r, draft), f.CapabilityUnsupported)
}

func TestToolCallingCaptureRejectsRevisionWithoutCapability(t *testing.T) {
	request := resolutionTestRequest(t)
	draft := resolutionTestDraft(t, request)
	draft.Snapshot.Identity.AdapterRevision = wire.OpenAIChatToolsRevision
	draft.Snapshot.Capabilities.ToolCalls = false
	requireCode(t, defaultResolutionCapture.requireDraft(request, draft), f.CapabilityUnsupported)
	requireCode(t, toolFactsResolutionCapture.requireDraft(request, draft), f.CapabilityUnsupported)
	requireCode(t, toolCallingResolutionCapture.requireDraft(request, draft), f.CapabilityUnsupported)
}

func TestRuntimeFirstRoundAutoRequiresExactBoundDigest(t *testing.T) {
	r := runtimeToolRequest(t)
	r.Input.SchemaVersion = mc.ToolInputSchemaVersion
	var err error
	r.Input.Digest, err = mc.ToolInputDigest(context.Background(), r.Messages, r.Tools, r.ToolChoice)
	if err != nil {
		t.Fatal(err)
	}
	r.ToolChoice = mc.ToolChoice{Kind: "auto"}
	if _, err := prepareRuntimeInput(context.Background(), r, wire.JSONResponse); err == nil {
		t.Fatal("changed choice accepted under required digest")
	}
	r.Input.Digest, err = mc.ToolInputDigest(context.Background(), r.Messages, r.Tools, r.ToolChoice)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := prepareRuntimeInput(context.Background(), r, wire.JSONResponse); err != nil {
		t.Fatal("bound schema4 auto rejected", err)
	}
	r.Input.SchemaVersion = mc.ToolCallInputSchemaVersion
	if _, err := prepareRuntimeInput(context.Background(), r, wire.JSONResponse); err == nil {
		t.Fatal("historical schema2 became auto")
	}
}

func TestRuntimeToolMediaReturnsCapabilityBeforeAdmission(t *testing.T) {
	for _, image := range []bool{false, true} {
		name := "file"
		if image {
			name = "image"
		}
		t.Run(name, func(t *testing.T) {
			r := runtimeToolRequest(t)
			r.ToolChoice = mc.ToolChoice{Kind: "auto"}
			ref, err := oc.NewBusinessFileRef(oc.BusinessFileDetails{Kind: oc.MCPFile, ProjectID: r.Model.Consumer.ProjectID, FileID: mustID[struct{}](t).String()})
			if err != nil {
				t.Fatal(err)
			}
			media := &mc.MediaPart{Ref: ref, MediaType: "application/octet-stream"}
			result := mc.ResultPart{File: media}
			if image {
				media.MediaType = "image/png"
				result = mc.ResultPart{Image: media}
			}
			r.Messages = append(r.Messages, mc.Message{Role: "assistant", Parts: []mc.MessagePart{{ToolCall: &mc.ToolCallPart{ID: "original-call", Name: r.Tools[0].Name, Arguments: json.RawMessage(`{}`)}}}}, mc.Message{Role: "tool", Parts: []mc.MessagePart{{ToolResult: &mc.ToolResultPart{CallID: "original-call", Parts: []mc.ResultPart{{Text: &mc.TextPart{Text: "canonical summary"}}, result}}}}})
			semantic, err := mc.NewSuccessorSemantic(context.Background(), mc.SuccessorSemanticParts{Actor: r.Actor, Model: r.Model, Messages: r.Messages, Tools: r.Tools, ToolChoice: r.ToolChoice, ResponseFormat: r.ResponseFormat, RetryClass: r.RetryClass})
			if err != nil {
				t.Fatal(err)
			}
			r.Input.SchemaVersion = mc.SuccessorInputSchemaVersion
			r.Input.Digest = semantic.Digest()
			out, err := prepareRuntimeInput(context.Background(), r, wire.JSONResponse)
			requireCode(t, err, f.CapabilityUnsupported)
			if len(out.Messages) != 0 {
				t.Fatal("unsupported input returned an admitted copy")
			}
			visits := 0
			consumer := runtimeConsumerFunc(func(context.Context, mc.ConsumerRequest) (mc.ConsumerDependencies, error) {
				visits++
				t.Fatal("unsupported media reached consumer/wire admission")
				return mc.ConsumerDependencies{}, nil
			})
			service := runtimePureService(runtimePureAuthority(t, &noIOStore{}, consumer))
			timing, err := mc.NewAgentRetryTiming(agentRuntimeTiming())
			if err != nil {
				t.Fatal(err)
			}
			service.state().agentTiming = &timing
			handle, err := service.BeginChat(context.Background(), r)
			requireCode(t, err, f.CapabilityUnsupported)
			if handle != nil || visits != 0 {
				t.Fatal("unsupported media admitted an owner")
			}
			kept := r.Messages[3].Parts[0].ToolResult.Parts[1]
			actual := kept.File
			if image {
				actual = kept.Image
			}
			if actual == nil || actual.Ref.Details().FileID != ref.Details().FileID || actual.Ref.Details().ArtifactID != ref.Details().ArtifactID || len(r.Messages[3].Parts[0].ToolResult.Parts) != 2 {
				t.Fatal("canonical media/ref changed")
			}
			service.StopAdmission()
			if err := service.Drain(context.Background()); err != nil || !service.Joined() {
				t.Fatal("pre-dispatch rejection retained an owner", err)
			}
		})
	}
}
