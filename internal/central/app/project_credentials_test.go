package app

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestProjectCredentialsRootPureMissingDependencies(t *testing.T) {
	if h, e := projectCredentialsHandler(nil, nil, "http://localhost:8080"); h != nil || e == nil {
		t.Fatal("unbound services accepted")
	}
}
func TestProjectCredentialsRootPureRouteOwnership(t *testing.T) {
	const base = "/api/v1/projects/01900000-0000-7000-8000-000000000001"
	for _, tc := range []struct{ path, owner string }{
		{base + "/model-credentials", "credentials"}, {base + "/model-credentials/bad-id", "credentials"}, {base + "/model-credential-commands/lookup", "credentials"}, {"/api/v1/projects/bad-id/model-credentials", "credentials"},
		{base + "/model-credentials/", "old"}, {base + "/model-credentials/a/b", "old"}, {base + "/model-credential-commands/lookup/", "old"},
		{base + "/model-providers", "models"}, {base + "/models", "models"}, {base + "/available-chat-models", "models"},
		{"/api/v1/projects", "read"}, {base, "read"}, {base + "/model-usage", "usage"}, {"/api/v1/projects/resolve", "usage"},
		{"/api/v1/system/model-credentials", "old"}, {"/api/v1/system/model-selection/meeting-summary", "old"}, {"/api/v1/session", "old"}, {"/readyz", "old"},
	} {
		t.Run(tc.path, func(t *testing.T) {
			for _, method := range []string{"POST", "PUT", "DELETE", "GET", "HEAD", "PATCH"} {
				r := httptest.NewRequest(method, tc.path+"?cursor=opaque", strings.NewReader("owned"))
				url, body := r.URL, r.Body
				calls := 0
				child := func(owner string) http.Handler {
					return http.HandlerFunc(func(w http.ResponseWriter, got *http.Request) {
						calls++
						if owner != tc.owner || got != r || got.URL != url || got.Body != body {
							t.Error("dispatch altered ownership/request")
						}
						w.WriteHeader(204)
					})
				}
				h := projectCredentialsRoutes(projectModelsRoutes(projectReadRoutes(projectUsageRoutes(child("old"), child("usage")), child("read")), child("models")), child("credentials"))
				w := httptest.NewRecorder()
				h.ServeHTTP(w, r)
				if calls != 1 || w.Code != 204 {
					t.Fatal("lost or duplicate dispatch")
				}
			}
		})
	}
}
