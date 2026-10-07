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

func TestProjectModelConfigurationHTTPComposition(t *testing.T) {
	const base = "/api/v1/projects/01900000-0000-7000-8000-000000000001"
	for _, path := range []string{base + "/model-providers", base + "/model-providers/bad-id", base + "/models", base + "/models/bad-id", base + "/available-chat-models", base + "/model-commands/lookup"} {
		for _, method := range []string{"GET", "HEAD", "POST", "PUT", "DELETE", "PATCH", "OPTIONS"} {
			t.Run(method+path, func(t *testing.T) {
				want := "write"
				if strings.HasSuffix(path, "/available-chat-models") || ((method == "GET" || method == "HEAD") && !strings.HasSuffix(path, "/lookup")) {
					want = "read"
				}
				r := httptest.NewRequest(method, path+"?private=1", strings.NewReader("original"))
				body, url := r.Body, r.URL
				calls := 0
				child := func(owner string) http.Handler {
					return http.HandlerFunc(func(w http.ResponseWriter, got *http.Request) {
						calls++
						if owner != want || got != r || got.Body != body || got.URL != url {
							t.Error("composition changed ownership/request")
						}
						w.WriteHeader(204)
					})
				}
				w := httptest.NewRecorder()
				projectModelMethods(child("read"), child("write")).ServeHTTP(w, r)
				if w.Code != 204 || calls != 1 {
					t.Fatal("double/lost dispatch")
				}
			})
		}
	}
	for _, path := range []string{base + "/model-commands/lookup/", base + "/model-commands/lookup/extra", base + "/model-commands/other"} {
		calls := 0
		old := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++; w.WriteHeader(204) })
		unexpected := http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Error("unowned route claimed") })
		projectModelsRoutes(old, unexpected).ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("POST", path, nil))
		if calls != 1 {
			t.Fatal("fallback lost")
		}
	}
}
