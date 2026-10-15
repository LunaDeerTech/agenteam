package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
)

func mcpCodecBinding(t *testing.T) tc.ToolCallBinding {
	t.Helper()
	_, _, _, call, _ := operationFixture(t)
	call.Binding = tc.BuiltinBinding{}
	call.MCPBinding = tc.MCPBinding{ProjectID: call.ProjectID, ConfigID: newOperationID[struct{}](t).String(), ConnectionID: newOperationID[struct{}](t).String(), RuntimeBindingID: newOperationID[struct{}](t).String(), ConfigRevision: 1, RemoteToolName: "echo"}
	if err := call.Validate(); err != nil {
		t.Fatal(err)
	}
	return call
}

func TestMCPResultPersistsExactMediaAndKnownFailure(t *testing.T) {
	call := mcpCodecBinding(t)
	file := newOperationID[struct{}](t).String()
	ref, err := oc.NewBusinessFileRef(oc.BusinessFileDetails{Kind: oc.MCPFile, ProjectID: call.ProjectID, FileID: file})
	if err != nil {
		t.Fatal(err)
	}
	result := tc.MCPBackendResult{OutcomeKnown: true, MayHaveExecuted: true, Result: mc.ToolResultPart{CallID: call.CallID, Parts: []mc.ResultPart{{Text: &mc.TextPart{Text: "result"}}, {Image: &mc.MediaPart{Ref: ref, MediaType: "image/png"}}, {File: &mc.MediaPart{Ref: ref, MediaType: "application/pdf"}}}}}
	raw, err := encodeMCPResult(call, result)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(raw, []byte(file)) || bytes.Contains(raw, []byte("business_file_ref")) {
		t.Fatal("canonical media identity was lost")
	}
	decoded, err := decodeMCPResult(call, raw)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Result.Parts[1].Image.Ref.Details().FileID != file || decoded.Result.Parts[2].File.Ref.Details().FileID != file || decoded.Result.Parts[1].Image.Ref.Details().ProjectID != call.ProjectID {
		t.Fatal("media projection changed")
	}
	e := &BuiltinExecutor{}
	outcome, code, err := e.backendTerminal(&builtinCall{binding: call}, raw)
	if err != nil || outcome != "success" || code != "" {
		t.Fatalf("terminal: %s %s %v", outcome, code, err)
	}
	part, err := e.resultPart(call, builtinTerminal{Outcome: outcome, Code: code}, raw)
	if err != nil || len(part.Parts) != 3 || part.Parts[1].Image == nil {
		t.Fatal("result loan lost media", err)
	}
	result.Result.IsError, result.FailureCode = true, "mcp_invalid_response"
	raw, err = encodeMCPResult(call, result)
	if err != nil {
		t.Fatal(err)
	}
	outcome, code, err = e.backendTerminal(&builtinCall{binding: call}, raw)
	if err != nil || outcome != "failure" || code != string(f.DependencyUnavailable) {
		t.Fatal("known failure classified incorrectly", err)
	}
}

func TestMCPResultRejectsForeignAndNonCanonicalReferences(t *testing.T) {
	call := mcpCodecBinding(t)
	result := tc.MCPBackendResult{OutcomeKnown: true, Result: mc.ToolResultPart{CallID: call.CallID, Parts: []mc.ResultPart{{Text: &mc.TextPart{Text: "ok"}}}}}
	raw, err := encodeMCPResult(call, result)
	if err != nil {
		t.Fatal(err)
	}
	for _, broken := range [][]byte{append(bytes.Clone(raw), '\n'), append(bytes.Clone(raw), []byte(`{}`)...), bytes.Replace(raw, []byte(`"format":1`), []byte(`"format":1,"format":1`), 1), bytes.Replace(raw, []byte(`"format":1`), []byte(`"format":1,"unknown":true`), 1)} {
		if _, err := decodeMCPResult(call, broken); err == nil {
			t.Fatal("noncanonical result accepted")
		}
	}
	foreign, _ := oc.NewBusinessFileRef(oc.BusinessFileDetails{Kind: oc.MCPFile, ProjectID: newOperationID[id.Project](t), FileID: newOperationID[struct{}](t).String()})
	result.Result.Parts = []mc.ResultPart{{File: &mc.MediaPart{Ref: foreign, MediaType: "text/plain"}}}
	if _, err := encodeMCPResult(call, result); err == nil {
		t.Fatal("foreign project file accepted")
	}
	artifact, _ := oc.NewBusinessFileRef(oc.BusinessFileDetails{Kind: oc.ArtifactFile, ProjectID: call.ProjectID, ArtifactID: newOperationID[struct{}](t).String(), FileID: newOperationID[struct{}](t).String()})
	result.Result.Parts[0].File.Ref = artifact
	if _, err := encodeMCPResult(call, result); err == nil {
		t.Fatal("unrelated business source accepted")
	}
}

func TestMCPUnknownTerminalAndInvalidOutcomeCombinations(t *testing.T) {
	call := mcpCodecBinding(t)
	result := tc.MCPBackendResult{OutcomeKnown: false, MayHaveExecuted: true, FailureCode: "mcp_transport_error", Result: mc.ToolResultPart{CallID: call.CallID, IsError: true, Parts: []mc.ResultPart{{Text: &mc.TextPart{Text: "mcp_transport_error"}}}}}
	raw, err := encodeMCPResult(call, result)
	if err != nil {
		t.Fatal(err)
	}
	e := &BuiltinExecutor{}
	outcome, code, err := e.backendTerminal(&builtinCall{binding: call}, raw)
	if err != nil || outcome != "unknown" || code != string(f.DependencyUnavailable) {
		t.Fatal("uncertain effect classified as success or known failure")
	}
	if _, err := e.resultPart(call, builtinTerminal{Outcome: "success"}, raw); err == nil {
		t.Fatal("mismatched terminal accepted")
	}
	result.MayHaveExecuted = false
	if _, err := encodeMCPResult(call, result); err == nil {
		t.Fatal("impossible unknown result accepted")
	}
	result.MayHaveExecuted, result.Result.IsError = true, false
	if _, err := encodeMCPResult(call, result); err == nil {
		t.Fatal("unknown success accepted")
	}
	result.OutcomeKnown, result.Result.IsError, result.FailureCode = true, true, "remote-secret-error"
	if _, err := encodeMCPResult(call, result); err == nil {
		t.Fatal("unbounded failure code accepted")
	}
	result.FailureCode = ""
	result.Result.Parts[0].Text.Text = string(bytes.Repeat([]byte("x"), 1<<20))
	if _, err := encodeMCPResult(call, result); err == nil {
		t.Fatal("oversized terminal accepted")
	}
}

func TestMCPInvocationDescriptorNeverAuthorizesOrLooksUpNetwork(t *testing.T) {
	call := mcpCodecBinding(t)
	v := tc.MCPInvocation{Call: call, OperationID: newOperationID[tc.Operation](t), AttemptID: newOperationID[tc.Attempt](t), RequestID: newOperationID[f.Request](t).String()}
	if v.Validate() != nil {
		t.Fatal("fixture invocation invalid")
	}
	a := &MCPAuthority{}
	if _, err := a.DiscoverMCPInvocation(context.Background(), v); err == nil {
		t.Fatal("unbound authority accepted descriptor")
	}
	if err := a.RequireMCPInvocationInTx(context.Background(), f.Tx{}, v, &mcpInvocationPlan{owner: a, invocation: v}); err == nil {
		t.Fatal("forged plan accepted")
	}
	b := &mcpEngineBackend{}
	if body, found, err := b.Lookup(context.Background(), BuiltinInvocation{}, nil); body != nil || found || err != nil {
		t.Fatal("lookup attempted remote work")
	}
	if err := a.ValidateMCPOutput(context.Background(), v, json.RawMessage(`{}`)); err == nil {
		t.Fatal("output schema bypassed dispatch authority")
	}
}
