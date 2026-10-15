package runtime

import (
	"context"
	"strings"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
	p "github.com/LunaDeerTech/agenteam/internal/runnerprotocol"
)

type resourceOversizedResult struct{}

func (resourceOversizedResult) RequireBindingInTx(context.Context, f.Tx, any) error { return nil }
func (resourceOversizedResult) Execute(_ context.Context, v tc.MCPResourceInvocation, _ []byte) (tc.MCPBackendResult, error) {
	return tc.MCPBackendResult{OutcomeKnown: false, MayHaveExecuted: true, FailureCode: "mcp_timeout", Result: mc.ToolResultPart{CallID: v.Call.CallID, IsError: true, Parts: []mc.ResultPart{{Text: &mc.TextPart{Text: strings.Repeat("x", 1<<20)}}}}}, nil
}
func TestMCPResourceCodecFailureRetainsUnknownOutcome(t *testing.T) {
	call := mcpCodecBinding(t)
	call.MCPBinding = tc.MCPBinding{}
	call.Binding = tc.BuiltinBinding{HandlerID: "mcp.read-resource", ContractRevision: 1}
	in := BuiltinInvocation{Call: call, OperationID: newOperationID[tc.Operation](t), AttemptID: newOperationID[tc.Attempt](t), RequestID: p.ID(newOperationID[f.Request](t).String())}
	b := &MCPResourceBuiltin{registration: tc.BuiltinRegistration{Binding: call.Binding}, backend: resourceOversizedResult{}}
	raw, err := b.Execute(context.Background(), in, nil)
	if err != nil {
		t.Fatal(err)
	}
	result, err := decodeMCPResult(call, raw)
	if err != nil || result.OutcomeKnown || !result.MayHaveExecuted || !result.Result.IsError || result.FailureCode != "mcp_invalid_response" {
		t.Fatal("local codec failure rewrote an unknown resource outcome")
	}
}

func TestMCPResourceResultProfileIsClosedToFixedHandlers(t *testing.T) {
	call := mcpCodecBinding(t)
	call.MCPBinding = tc.MCPBinding{}
	call.Binding = tc.BuiltinBinding{HandlerID: "mcp.read-resource", ContractRevision: 1}
	in := BuiltinInvocation{Call: call, OperationID: newOperationID[tc.Operation](t), AttemptID: newOperationID[tc.Attempt](t), RequestID: p.ID(newOperationID[f.Request](t).String())}
	result := tc.MCPBackendResult{OutcomeKnown: true, Result: mc.ToolResultPart{CallID: call.CallID, Parts: []mc.ResultPart{{Text: &mc.TextPart{Text: "content"}}}}}
	raw, err := EncodeMCPResourceResult(in, result)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = decodeMCPResult(call, raw); err != nil {
		t.Fatal(err)
	}
	in.Call.Binding.HandlerID = "artifact.read"
	if _, err = EncodeMCPResourceResult(in, result); err == nil {
		t.Fatal("ordinary builtin accepted MCP result profile")
	}
	if _, err = decodeMCPResult(in.Call, raw); err == nil {
		t.Fatal("stored envelope accepted by foreign profile")
	}
}
func TestMCPResourceInvocationRequiresOriginalBuiltinLoan(t *testing.T) {
	call := mcpCodecBinding(t)
	call.MCPBinding = tc.MCPBinding{}
	call.Binding = tc.BuiltinBinding{HandlerID: "mcp.list-resources", ContractRevision: 1}
	v := tc.MCPResourceInvocation{Call: call, OperationID: newOperationID[tc.Operation](t), AttemptID: newOperationID[tc.Attempt](t), RequestID: newOperationID[f.Request](t).String()}
	if v.Validate() != nil {
		t.Fatal("invalid fixture")
	}
	a := &BuiltinAuthority{}
	if _, err := a.DiscoverMCPResourceInvocation(context.Background(), v); err == nil {
		t.Fatal("public descriptor authorized resource access")
	}
	if err := a.RequireMCPResourceInvocationInTx(context.Background(), f.Tx{}, v, &resourceInvocationPlan{owner: a, invocation: v}); err == nil {
		t.Fatal("forged resource plan accepted")
	}
	if raw, found, err := (&MCPResourceBuiltin{}).Lookup(context.Background(), BuiltinInvocation{}, nil); raw != nil || found || err != nil {
		t.Fatal("resource lookup attempted work")
	}
}
