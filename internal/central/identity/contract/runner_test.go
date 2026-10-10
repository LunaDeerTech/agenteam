package contract

import (
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
)

func TestRunnerIdentityRegistrationIsSystemOnly(t *testing.T) {
	registration, e := RegisterService(RunnerIdentity)
	if e != nil {
		t.Fatal(e)
	}
	event, e := f.NewID[struct{}]()
	if e != nil {
		t.Fatal(e)
	}
	actor, e := registration.Actor(event.String(), SystemScope())
	if e != nil || actor.Details().Kind != Service || actor.Details().ServiceName != RunnerIdentity || actor.Details().CauseRef != event.String() {
		t.Fatal("system identity", e)
	}
	project, e := f.NewID[Project]()
	if e != nil {
		t.Fatal(e)
	}
	scope, _ := InProject(project)
	if _, e = registration.Actor(event.String(), scope); e == nil {
		t.Fatal("Runner service acquired project identity")
	}
	for _, name := range []ServiceName{"Runner-identity", "runner_identity", "runner-identity-extra"} {
		if _, e = RegisterService(name); e == nil {
			t.Fatal("unregistered service accepted")
		}
	}
}
