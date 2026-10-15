package approval

import (
	"bytes"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
	"testing"
)

func TestApprovalMCPIdentityUsesCapturedTupleAndPreservesRunCommand(t *testing.T) {
	command := testBinding(t)
	if tc.ApprovalToolName(command) != "run-command" {
		t.Fatal("existing command identity changed")
	}
	original, err := bindingBytes(command)
	if err != nil {
		t.Fatal(err)
	}
	restored, err := decodeBinding(original)
	if err != nil || !sameBinding(restored, command) {
		t.Fatal("existing command failed roundtrip", err)
	}
	if bytes.Contains(original, []byte("mcp_binding")) {
		t.Fatal("MCP field changed old command wire")
	}
	mcp := command
	mcp.RunnerBinding = tc.RunnerBinding{}
	mcp.MCPBinding = tc.MCPBinding{ProjectID: mcp.ProjectID, ConfigID: testID[struct{}](t).String(), ConnectionID: testID[struct{}](t).String(), RuntimeBindingID: testID[struct{}](t).String(), ConfigRevision: 2, RemoteToolName: "run-command"}
	if tc.ApprovalToolName(mcp) != "mcp:"+mcp.MCPBinding.ConfigID+":run-command" {
		t.Fatal("remote name impersonated command")
	}
	raw, err := bindingBytes(mcp)
	if err != nil {
		t.Fatal(err)
	}
	got, err := decodeBinding(raw)
	if err != nil || !sameBinding(got, mcp) {
		t.Fatal("captured MCP tuple lost", err)
	}
	changed := mcp
	changed.MCPBinding.ConfigRevision++
	if sameBinding(changed, mcp) {
		t.Fatal("new connection revision reused approval")
	}
	changed = mcp
	changed.MCPBinding.RuntimeBindingID = testID[struct{}](t).String()
	if sameBinding(changed, mcp) {
		t.Fatal("foreign runtime shadow reused approval")
	}
}
