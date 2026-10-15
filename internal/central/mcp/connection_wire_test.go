package mcp

import (
	"encoding/json"
	"strings"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

func TestMCPInitialConnectionAndFailedDiscoveryCanBePersisted(t *testing.T) {
	config, _ := f.ParseID[ConfigIdentity]("01960aa5-0286-7000-8000-000000000001")
	raw, err := connectionSemantic(ConnectInput{ConfigID: config}, 0)
	if err != nil || !strings.Contains(string(raw), `"Version":"0"`) {
		t.Fatal("initial connection must support expected version zero")
	}
	connection, _ := f.ParseID[ConnectionIdentity]("01960aa5-0286-7000-8000-000000000002")
	project, _ := f.ParseID[id.Project]("01960aa5-0286-7000-8000-000000000003")
	out := CommandResult{Connection: &Connection{ID: connection, ProjectID: project, ConfigID: config, Version: 1, ConfigRevision: 1, State: "auth_required", ToolsState: "never", ResourcesState: "never", LastErrorCode: "mcp_authentication_required"}}
	raw, err = json.Marshal(out)
	if err != nil || !strings.Contains(string(raw), `"tools_revision":"0"`) || !strings.Contains(string(raw), `"resources_revision":"0"`) {
		t.Fatal("failed initial discovery must retain a queryable command result")
	}
	var observed CommandResult
	if json.Unmarshal(raw, &observed) != nil || observed.Connection == nil || *observed.Connection != *out.Connection {
		t.Fatal("original failed discovery command did not round trip")
	}
}
