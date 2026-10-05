package contract

import (
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"testing"
)

func TestProjectInitializationRegisteredWithoutOwnerGrant(t *testing.T) {
	r, e := RegisterService(ProjectInitialization)
	if e != nil {
		t.Fatal(e)
	}
	id, _ := foundation.NewID[Project]()
	scope, _ := InProject(id)
	cause, _ := foundation.NewID[struct{}]()
	a, e := r.Actor(cause.String(), scope)
	if e != nil || a.Details().Kind != Service || a.Details().ServiceName != ProjectInitialization || a.Details().ProjectID != id.String() || a.Details().UserID != "" || a.Details().SessionID != "" {
		t.Fatal("role shape", e)
	}
	if _, e = RegisterService("project-initialization-extra"); e == nil {
		t.Fatal("role set opened")
	}
	if _, e = r.Actor("arbitrary-text", scope); e == nil {
		t.Fatal("untyped cause accepted")
	}
}
