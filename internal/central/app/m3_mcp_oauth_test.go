package app

import "testing"

func TestMCPOAuthRedirectUsesOnlyConfiguredOrigin(t *testing.T) {
	for _, origin := range []string{"https://central.example", "http://localhost:8080", "https://[2001:db8::1]:9443"} {
		got, err := mcpOAuthRedirect(origin)
		if err != nil || got != origin+"/mcp/oauth/callback" {
			t.Fatalf("fixed callback not derived from origin: %q %v", got, err)
		}
	}
	for _, origin := range []string{"", "//central.example", "ftp://central.example", "https://user:password@central.example", "https://central.example/override", "https://central.example/", "https://central.example?redirect_uri=https://other.example", "https://central.example?", "https://central.example#callback", "https:central.example"} {
		if got, err := mcpOAuthRedirect(origin); err == nil || got != "" {
			t.Fatalf("non-origin input changed the callback: %q", got)
		}
	}
}
