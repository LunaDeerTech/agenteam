package app

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestProjectModelsRootPureMissingDependencies(t *testing.T) {
	if h, e := projectModelsHandler(nil, nil, "http://localhost:8080"); e == nil || h != nil {
		t.Fatal("unbound Account/Model accepted")
	}
}
func TestProjectModelsRootPureRouteOwnership(t *testing.T) {
	const p = "01900000-0000-7000-8000-000000000001"
	base := "/api/v1/projects/" + p
	for _, tc := range []struct{ path, owner string }{
		{base + "/model-providers", "models"}, {base + "/model-providers/bad-id", "models"}, {base + "/models", "models"}, {base + "/models/bad-id", "models"}, {base + "/available-chat-models", "models"}, {"/api/v1/projects/bad-id/models", "models"},
		{base + "/models/", "old"}, {base + "/models/a/b", "old"}, {base + "/available-chat-models/extra", "old"}, {base + "/models-other", "old"}, {base + "/archive", "old"},
		{"/api/v1/projects", "read"}, {base, "read"}, {"/api/v1/projects/resolve", "usage"}, {base + "/model-usage", "usage"}, {base + "/model-usage/summary", "usage"},
		{"/api/v1/session", "old"}, {"/api/v1/system/model-selection/meeting-summary", "old"}, {"/api/v1/system/models", "old"}, {"/readyz", "old"},
	} {
		t.Run(tc.path, func(t *testing.T) {
			r := httptest.NewRequest("GET", tc.path+"?cursor=sensitive", strings.NewReader("owned"))
			url, body := r.URL, r.Body
			calls := 0
			child := func(owner string) http.Handler {
				return http.HandlerFunc(func(w http.ResponseWriter, got *http.Request) {
					calls++
					if owner != tc.owner || got != r || got.URL != url || got.Body != body {
						t.Error("dispatch altered ownership or request")
					}
					w.WriteHeader(204)
				})
			}
			h := projectModelsRoutes(projectReadRoutes(projectUsageRoutes(child("old"), child("usage")), child("read")), child("models"))
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if calls != 1 || w.Code != 204 {
				t.Fatal("lost/double dispatch")
			}
		})
	}
}
