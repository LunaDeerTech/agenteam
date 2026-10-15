package contract

import (
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
)

func TestDomainRuntimeIdentityRequiresProjectAndOriginalOperation(t *testing.T) {
	project, _ := f.NewID[Project]()
	operation, _ := f.NewID[struct{}]()
	scope, _ := InProject(project)
	for _, name := range []ServiceName{KnowledgeRuntime, MemoryRuntime, MeetingRuntime} {
		t.Run(string(name), func(t *testing.T) {
			r, err := RegisterService(name)
			if err != nil {
				t.Fatal(err)
			}
			a, err := r.Actor(operation.String(), scope)
			if err != nil || a.Details().ServiceName != name || a.Details().ProjectID != project.String() || a.Details().CauseRef != operation.String() {
				t.Fatal("operation identity missing", err)
			}
			if _, err = r.Actor(operation.String(), SystemScope()); err == nil {
				t.Fatal("system scope accepted")
			}
			if _, err = r.Actor("sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", scope); err == nil {
				t.Fatal("digest substituted for original operation")
			}
			if (AccessGrant{}).Matches(a, scope, Read) {
				t.Fatal("registration granted access")
			}
		})
	}
}
