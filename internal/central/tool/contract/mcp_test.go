package contract

import (
	"context"
	"testing"

	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

func TestMCPBindingKeepsProjectAndBackendExclusive(t *testing.T) {
	b := MCPBinding{ProjectID: runnerContractID[id.Project](t, 21), ConfigID: runnerContractID[struct{}](t, 22).String(), ConnectionID: runnerContractID[struct{}](t, 23).String(), RuntimeBindingID: runnerContractID[struct{}](t, 24).String(), ConfigRevision: 1, RemoteToolName: "Search.Documents"}
	if b.Validate() != nil || !validBindingChoice(BuiltinBinding{}, RunnerBinding{}, b) {
		t.Fatal("valid MCP binding rejected")
	}
	if validBindingChoice(BuiltinBinding{HandlerID: "memory.recall", ContractRevision: 1}, RunnerBinding{}, b) || validBindingChoice(BuiltinBinding{}, ReadFileRegistration().Binding, b) {
		t.Fatal("mixed backend accepted")
	}
	r := MCPRegistration{Definition: Definition{StableKey: "mcp:" + b.ConfigID + ":" + b.RemoteToolName, Name: b.RemoteToolName, InputSchema: []byte(`{"type":"object"}`)}, Binding: b, Active: true}
	if err := r.Validate(context.Background()); err != nil {
		t.Fatal(err)
	}
	r.Definition.StableKey = "mcp:" + b.ConnectionID + ":" + b.RemoteToolName
	if r.Validate(context.Background()) == nil {
		t.Fatal("connection substituted for stable config identity")
	}
	b.RemoteToolName = "https://remote.example/tool"
	if b.Validate() == nil {
		t.Fatal("URL accepted as remote tool name")
	}
}
