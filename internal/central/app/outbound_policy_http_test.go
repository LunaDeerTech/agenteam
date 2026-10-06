package app

import (
	"bytes"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/LunaDeerTech/agenteam/internal/central/httpapi"
)

func TestOutboundPolicyRootRoutePreservationAndSingleMiddleware(t *testing.T) {
	const root = "/api/v1/system/outbound-policy"
	for _, tc := range []struct{ path, raw, owner string }{
		{root, "", "policy"}, {root + "/", "", "policy"}, {root + "/child", "", "policy"}, {root + "//child", "", "policy"}, {root + "/../session", "", "policy"},
		{root + "/child", root + "%2fchild", "policy"}, {root + "-other", "", "account"}, {root + "x", "", "account"}, {"/api/v1/session", "", "account"},
		{"/api/v1/system/model-providers", "", "model"}, {"/api/v1/system/models/child", "", "model"}, {"/api/v1/system/model-selection", "", "model"},
		{"/api/v1/system/model-commands/lookup", "", "model"}, {"/api/v1/system/model-credentials/id", "", "model"}, {"/api/v1/system/model-credential-commands/lookup", "", "model"},
	} {
		t.Run(tc.path+tc.raw, func(t *testing.T) {
			r := httptest.NewRequest("PATCH", "http://localhost:8080"+tc.path+"?private=query", strings.NewReader("private-body"))
			r.URL.RawPath = tc.raw
			r.Header.Set("Origin", "http://localhost:8080")
			r.Header.Set("Cookie", "private-cookie")
			r.Header.Set("Idempotency-Key", "private-command")
			url, body, uri := r.URL, r.Body, r.RequestURI
			calls := 0
			child := func(owner string) http.Handler {
				return http.HandlerFunc(func(w http.ResponseWriter, got *http.Request) {
					calls++
					if owner != tc.owner || got.URL != url || got.Body != body || got.RequestURI != uri || got.Method != "PATCH" || got.URL.RawPath != tc.raw || got.Host != "localhost:8080" || got.Header.Get("Cookie") != "private-cookie" || got.Header.Get("Idempotency-Key") != "private-command" || got.Header.Get("Origin") != "http://localhost:8080" {
						t.Fatal("request or ownership changed")
					}
					b, e := io.ReadAll(got.Body)
					if e != nil || string(b) != "private-body" || httpapi.RequestID(got.Context()).Validate() != nil {
						t.Fatal("body consumed or missing trace")
					}
					w.WriteHeader(204)
				})
			}
			var logs bytes.Buffer
			logger := slog.New(slog.NewJSONHandler(&logs, nil))
			w := httptest.NewRecorder()
			httpapi.Handler(logger, systemOutboundPolicyRoutes(systemModelRoutes(child("account"), child("model")), child("policy"))).ServeHTTP(w, r)
			if calls != 1 || w.Code != 204 || w.Header().Get("Location") != "" || len(w.Header().Values("X-Request-ID")) != 1 || strings.Count(logs.String(), `"event":"http_request"`) != 1 {
				t.Fatal("duplicate dispatch/middleware or redirect")
			}
			for _, secret := range []string{"private=query", "private-body", "private-cookie", "private-command"} {
				if strings.Contains(logs.String(), secret) {
					t.Fatal("ordinary log exposed material")
				}
			}
		})
	}
}
