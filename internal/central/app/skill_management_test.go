package app

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSkillManagementRootRoutesKeepReadCompatibility(t *testing.T) {
	for _, tc := range []struct{ method, suffix, want string }{
		{"GET", "/skills", "existing"}, {"HEAD", "/skills", "existing"},
		{"GET", "/skills/01900000-0000-7000-8000-000000000001", "existing"},
		{"POST", "/skills", "management"}, {"GET", "/skills/catalog", "management"},
		{"HEAD", "/skills/catalog", "management"}, {"POST", "/skills/commands/lookup", "management"},
		{"GET", "/skills/commands/lookup", "management"}, {"DELETE", "/skills", "existing"},
		{"POST", "/skills/commands/lookup/extra", "existing"}, {"POST", "/skills/", "existing"},
	} {
		t.Run(tc.method+tc.suffix, func(t *testing.T) {
			r := httptest.NewRequest(tc.method, "/api/v1/projects/01900000-0000-7000-8000-000000000002"+tc.suffix, nil)
			seen := ""
			capture := func(name string) http.Handler {
				return http.HandlerFunc(func(_ http.ResponseWriter, actual *http.Request) {
					if actual != r {
						t.Fatal("root replaced original request")
					}
					seen = name
				})
			}
			skillManagementRoutes(capture("existing"), capture("management")).ServeHTTP(httptest.NewRecorder(), r)
			if seen != tc.want {
				t.Fatal("wrong exact route", seen)
			}
		})
	}
}
