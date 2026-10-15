package contract

import (
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"testing"
)

func TestMCPRuntimeIdentityRequiresProjectAndOriginalUUID(t *testing.T) {
	registration, err := RegisterService(MCPRuntime)
	if err != nil {
		t.Fatal(err)
	}
	project, _ := f.NewID[Project]()
	scope, _ := InProject(project)
	request, _ := f.NewID[struct{}]()
	actor, err := registration.Actor(request.String(), scope)
	if err != nil || actor.Details().ServiceName != MCPRuntime || actor.Details().ProjectID != project.String() {
		t.Fatal("closed identity rejected", err)
	}
	if _, err = registration.Actor(request.String(), SystemScope()); err == nil {
		t.Fatal("system runtime identity granted")
	}
	if _, err = registration.Actor("sha256:0000000000000000000000000000000000000000000000000000000000000000", scope); err == nil {
		t.Fatal("command digest replaced original request")
	}
}
