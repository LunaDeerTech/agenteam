package app

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestProjectSchedulerRoutesPreserveOwnerEndpoints(t *testing.T) {
	current := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.Header().Set("X-Test-Route", "existing") })
	commands := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.Header().Set("X-Test-Route", "project-commands") })
	combined := projectUpdateRoutes(current, commands)
	project := updateRootID[struct{}]().String()
	for _, test := range []struct{ method, suffix, route string }{
		{"GET", "/" + project + "/scheduler", "project-commands"},
		{"HEAD", "/" + project + "/scheduler", "project-commands"},
		{"PATCH", "/" + project + "/scheduler", "project-commands"},
		{"DELETE", "/" + project + "/scheduler", "project-commands"},
		{"POST", "/" + project + "/commands/lookup", "project-commands"},
		{"PATCH", "/" + project, "project-commands"},
		{"GET", "/" + project, "existing"},
		{"HEAD", "/" + project, "existing"},
		{"GET", "", "existing"},
		{"GET", "/resolve", "existing"},
		{"GET", "/" + project + "/scheduler/extra", "existing"},
		{"PATCH", "/" + project + "/scheduler/", "existing"},
	} {
		t.Run(test.method+test.suffix, func(t *testing.T) {
			r := httptest.NewRequest(test.method, "/api/v1/projects"+test.suffix, nil)
			w := httptest.NewRecorder()
			combined.ServeHTTP(w, r)
			if w.Header().Get("X-Test-Route") != test.route {
				t.Fatal("route owner changed", w.Header())
			}
		})
	}
}
