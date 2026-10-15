package mcphttp

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/mcp"
)

type oauthCommandProbe struct {
	begin, complete int
	command         mcp.Command
	actor           id.Actor
	key             f.IdempotencyKey
	config          mcp.ConfigID
}

func (p *oauthCommandProbe) BeginOAuth(_ context.Context, c mcp.Command, config mcp.ConfigID) (mcp.CommandResult, error) {
	p.begin++
	p.command, p.config = c, config
	return mcp.CommandResult{OAuth: &mcp.OAuthAuthorization{FlowID: "owned-flow", AuthorizationURL: "https://issuer.fixture/authorize?state=original"}}, nil
}
func (p *oauthCommandProbe) CompleteOAuth(_ context.Context, a id.Actor, k f.IdempotencyKey, _ mcp.OAuthCallback) (mcp.CommandResult, error) {
	p.complete++
	p.actor, p.key = a, k
	return mcp.CommandResult{}, nil
}
func oauthProbe(t *testing.T) (*oauthHTTP, *oauthCommandProbe) {
	t.Helper()
	h, _ := credentialProbe(t)
	p := &oauthCommandProbe{}
	return &oauthHTTP{service: p, boundary: h.boundary}, p
}
func oauthCallbackBody() string {
	return `{"state":"` + base64.RawURLEncoding.EncodeToString(make([]byte, 32)) + `","code":"owned-code","issuer":"https://issuer.fixture"}`
}
func TestMCPOAuthHTTPOriginalSessionAndCommand(t *testing.T) {
	h, p := oauthProbe(t)
	path := "/api/v1/projects/" + credentialProject + "/mcp/oauth/begin"
	raw, err := h.execute(httptest.NewRecorder(), credentialRequest(http.MethodPost, path, `{"config_id":"`+credentialID+`","expected_version":"0"}`))
	if err != nil || p.begin != 1 || p.command.Scope.Details().ProjectID != credentialProject || p.command.Actor.Details().Kind != id.Human || p.config.String() != credentialID || !strings.Contains(string(raw), `"authorization_url"`) {
		t.Fatal("begin lost the original Owner command or authorization redirect")
	}
	raw, err = h.execute(httptest.NewRecorder(), credentialRequest(http.MethodPost, "/api/v1/mcp/oauth/complete", oauthCallbackBody()))
	if err != nil || p.complete != 1 || !p.actor.Equal(p.command.Actor) || p.key != "mcp-credential:original" || strings.Contains(string(raw), "owned-code") {
		t.Fatal("callback lost original session/key or exposed code")
	}
}
func TestMCPOAuthHTTPRejectsBrowserAuthorityAndQuery(t *testing.T) {
	for _, v := range []struct{ name, method, path, body string }{
		{"query", http.MethodPost, "/api/v1/mcp/oauth/complete?code=owned-code", oauthCallbackBody()},
		{"get", http.MethodGet, "/api/v1/mcp/oauth/complete", oauthCallbackBody()},
		{"project", http.MethodPost, "/api/v1/mcp/oauth/complete", strings.TrimSuffix(oauthCallbackBody(), "}") + `,"project_id":"` + credentialProject + `"}`},
		{"redirect", http.MethodPost, "/api/v1/mcp/oauth/complete", strings.TrimSuffix(oauthCallbackBody(), "}") + `,"redirect_uri":"https://other.fixture"}`},
		{"duplicate", http.MethodPost, "/api/v1/mcp/oauth/complete", strings.TrimSuffix(oauthCallbackBody(), "}") + `,"code":"another"}`},
		{"system", http.MethodPost, "/api/v1/system/mcp/oauth/begin", `{"config_id":"` + credentialID + `","expected_version":"0"}`},
	} {
		t.Run(v.name, func(t *testing.T) {
			h, p := oauthProbe(t)
			if _, err := h.execute(httptest.NewRecorder(), credentialRequest(v.method, v.path, v.body)); err == nil || p.begin+p.complete != 0 {
				t.Fatal("invalid callback reached domain")
			}
		})
	}
	h, p := oauthProbe(t)
	h.boundary = credentialBoundaryProbe{denied: fault(f.Forbidden)}
	if _, err := h.execute(httptest.NewRecorder(), credentialRequest(http.MethodPost, "/api/v1/mcp/oauth/complete", oauthCallbackBody())); err == nil || p.complete != 0 {
		t.Fatal("browser boundary denial reached OAuth")
	}
}
