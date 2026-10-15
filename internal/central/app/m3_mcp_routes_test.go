package app

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestMCPRoutesIncludeOnlyOriginalCredentialResources(t *testing.T) {
	const project = "01900000-0000-7000-8000-000000000001"
	const credential = "01900000-0000-7000-8000-000000000002"
	for _, tc := range []struct {
		path   string
		remote bool
	}{
		{"/api/v1/projects/" + project + "/mcp/credentials", true},
		{"/api/v1/projects/" + project + "/mcp/credentials/" + credential, true},
		{"/api/v1/projects/" + project + "/mcp/credential-commands/lookup", true},
		{"/api/v1/projects/" + project + "/mcp/credentials/" + credential + "/body", false},
		{"/api/v1/projects/" + project + "/credentials", false},
		{"/api/v1/system/mcp/credentials", false},
		{"/api/v1/projects/" + project + "/mcp/oauth/begin", true},
		{"/api/v1/mcp/oauth/complete", true},
		{"/api/v1/system/mcp/oauth/begin", false},
		{"/api/v1/projects/" + project + "/mcp/oauth/complete", false},
	} {
		t.Run(tc.path, func(t *testing.T) {
			calls, previous := 0, 0
			router := mcpRoutes(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { previous++ }), http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls++ }))
			router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, tc.path, nil))
			if (calls == 1) != tc.remote || calls+previous != 1 {
				t.Fatal("credential route escaped the fixed MCP handler", calls, previous)
			}
		})
	}
}
