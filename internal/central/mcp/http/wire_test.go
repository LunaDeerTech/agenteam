package mcphttp

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/mcp"
	"github.com/LunaDeerTech/agenteam/internal/central/mcp/protocol"
)

func TestMCPHTTPClosedInputsRejectSecretFields(t *testing.T) {
	for _, body := range []string{
		`{"config_id":"01960aa5-0286-7000-8000-000000000001","expected_version":"0","token":"never-send-raw"}`,
		`{"config_id":"01960aa5-0286-7000-8000-000000000001","expected_version":"0","expected_version":"1"}`,
		`{"config_id":"01960aa5-0286-7000-8000-000000000001","expected_version":0}`,
	} {
		r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		var input connectInput
		if err := decode(httptest.NewRecorder(), r, &input); err == nil {
			t.Fatal("invalid control request entered the domain")
		}
	}
	if _, err := (configInput{Authentication: protocol.Authentication{Scheme: "oauth"}}).domain(); err != nil {
		t.Fatal("implemented OAuth rejected by Owner control plane")
	}
	if implementedAuthentication(protocol.Authentication{Scheme: "custom_header", HeaderName: "Authorization"}) {
		t.Fatal("reserved credential header accepted")
	}
	if !implementedAuthentication(protocol.Authentication{Scheme: "none"}) {
		t.Fatal("implemented no-auth configuration rejected")
	}
}

func TestMCPHTTPProjectionKeepsRuntimeAuthorityPrivate(t *testing.T) {
	config, _ := f.ParseID[mcp.ConfigIdentity]("01960aa5-0286-7000-8000-000000000001")
	connection, _ := f.ParseID[mcp.ConnectionIdentity]("01960aa5-0286-7000-8000-000000000002")
	project, _ := f.ParseID[id.Project]("01960aa5-0286-7000-8000-000000000003")
	value := mcp.CommandResult{Connection: &mcp.Connection{ID: connection, ProjectID: project, ConfigID: config, Version: 1, ConfigRevision: 1, State: "auth_required", ToolsState: "never", ResourcesState: "never", RuntimeBindingID: "never-publish-runtime-binding", LastErrorCode: "mcp_authentication_required"}}
	raw, err := encode(projectCommand(value))
	if err != nil {
		t.Fatal("initial failure projection did not encode")
	}
	if strings.Contains(string(raw), "never-publish") || strings.Contains(string(raw), "runtime_binding") {
		t.Fatal("internal invocation authority leaked in control response")
	}
	if !strings.Contains(string(raw), `"tools_revision":"0"`) || !strings.Contains(string(raw), `"last_error_code":"mcp_authentication_required"`) {
		t.Fatal("safe discovery failure state absent")
	}
}

func TestMCPHTTPProjectConnectionRoutesCannotBecomeSystemConnections(t *testing.T) {
	for _, path := range []string{"/api/v1/system/mcp/connections", "/api/v1/system/mcp/connections/connect", "/api/v1/projects/p/mcp/configs/c/refresh", "/api/v1/projects/p/mcp/connections/c/invoke"} {
		if HandlesPath(path) {
			t.Fatal("unsupported control route admitted")
		}
	}
	for _, path := range []string{"/api/v1/system/mcp/configs", "/api/v1/projects/p/mcp/connections/connect", "/api/v1/projects/p/mcp/connections/c/resources", "/api/v1/projects/p/mcp/commands/lookup"} {
		if !HandlesPath(path) {
			t.Fatal("implemented control route missing")
		}
	}
}
