package app

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/LunaDeerTech/agenteam/internal/central/skill/contract"
)

func TestSkillAssignmentRootRequiresBindingsAndPreservesRoutes(t *testing.T) {
	if got, err := createSkillAssignments(nil, nil, nil, nil, contract.AssignmentEvents{}, nil, nil, nil); err == nil || got != nil {
		t.Fatal("missing same-Store assignment graph accepted")
	}
	if got, err := createSkillAssignmentOwner(nil, nil, nil); err == nil || got != nil {
		t.Fatal("missing original Agent owner accepted")
	}
	base := "/api/v1/projects/01900000-0000-7000-8000-000000000001/agents/01900000-0000-7000-8000-000000000002"
	for _, tc := range []struct{ suffix, want string }{
		{"/skills", "assignment"}, {"/skills/assign", "assignment"}, {"/skills/remove", "assignment"}, {"/skills/commands/lookup", "assignment"},
		{"/configuration", "existing"}, {"/runner-mounts", "existing"}, {"/skills/", "existing"}, {"/skills/commands/lookup/extra", "existing"},
	} {
		t.Run(tc.suffix, func(t *testing.T) {
			seen := ""
			r := httptest.NewRequest("POST", base+tc.suffix, nil)
			capture := func(name string) http.Handler {
				return http.HandlerFunc(func(_ http.ResponseWriter, got *http.Request) {
					if got != r {
						t.Fatal("root replaced original request")
					}
					seen = name
				})
			}
			skillAssignmentRoutes(capture("existing"), capture("assignment")).ServeHTTP(httptest.NewRecorder(), r)
			if seen != tc.want {
				t.Fatal("root route ownership changed", seen)
			}
		})
	}
}
