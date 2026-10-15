package contract

import (
	"bytes"
	"testing"
)

func TestM3MetadataRejectsUnboundedAndDuplicateFields(t *testing.T) {
	const key = "01900000-0000-7000-8000-000000000001"
	fixtures := []struct {
		action   Action
		metadata Metadata
	}{}
	memory, e := MemoryMetadata(MemoryRetain, MemoryMetadataFields{AgentID: key, OperationID: key, Gate: "mask", Added: 2})
	if e != nil {
		t.Fatal(e)
	}
	mcp, e := MCPMetadata(MCPDiscoveryRefresh, MCPMetadataFields{ConfigID: key, ConnectionID: key, Version: 1, ToolCount: 2})
	if e != nil {
		t.Fatal(e)
	}
	fixtures = append(fixtures, struct {
		action   Action
		metadata Metadata
	}{MemoryRetain, memory}, struct {
		action   Action
		metadata Metadata
	}{MCPDiscoveryRefresh, mcp})
	for _, fixture := range fixtures {
		t.Run(string(fixture.action), func(t *testing.T) {
			raw := fixture.metadata.JSON()
			if _, e := DecodeMetadata(fixture.action, raw); e != nil {
				t.Fatal(e)
			}
			for _, suffix := range []string{`,"endpoint":"https://private"}`, `,"tool_count":null}`, `,"agent_id":"` + key + `"}`} {
				bad := append(bytes.Clone(raw[:len(raw)-1]), []byte(suffix)...)
				if _, e := DecodeMetadata(fixture.action, bad); e == nil {
					t.Fatal("accepted unknown, null or duplicate metadata")
				}
			}
		})
	}
	if _, e := MemoryMetadata(MemoryRetain, MemoryMetadataFields{AgentID: key, OperationID: key, Gate: "reject"}); e == nil {
		t.Fatal("rejected input became successful retain audit")
	}
	if _, e := MCPMetadata(MCPDiscoveryRefresh, MCPMetadataFields{ConfigID: key, ConnectionID: key, Version: 1, ErrorCode: "provider response contains secret"}); e == nil {
		t.Fatal("accepted provider text as error code")
	}
}
