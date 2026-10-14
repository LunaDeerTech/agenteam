package model

import (
	"context"
	"encoding/json"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	wire "github.com/LunaDeerTech/agenteam/internal/central/model/adapter"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
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
	} {
		t.Run(name, func(t *testing.T) {
			r := runtimeToolRequest(t)
			change(&r)
			out, err := prepareRuntimeInput(context.Background(), r, wire.JSONResponse)
			if err == nil || len(out.Messages) > 0 {
				t.Fatal("invalid tools profile accepted")
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
