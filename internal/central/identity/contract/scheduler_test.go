package contract

import (
	"strings"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
)

func TestSchedulerRegistrationIsProjectDispatchIdentityOnly(t *testing.T) {
	registration, err := RegisterService(Scheduler)
	if err != nil {
		t.Fatal(err)
	}
	project, _ := f.NewID[Project]()
	dispatch, _ := f.NewID[struct{}]()
	scope, _ := InProject(project)
	actor, err := registration.Actor(dispatch.String(), scope)
	d := actor.Details()
	if err != nil || d.Kind != Service || d.ServiceName != Scheduler || d.ProjectID != project.String() || d.CauseRef != dispatch.String() || d.UserID != "" || d.SessionID != "" || d.AgentID != "" || d.ExecutionID != "" {
		t.Fatal("Scheduler identity lost its exact scope or cause", err)
	}
	if (AccessGrant{}).Matches(actor, scope, Launch) {
		t.Fatal("registration granted launch authority")
	}
	if _, err = registration.Actor(dispatch.String(), SystemScope()); err == nil {
		t.Fatal("Scheduler accepted System scope")
	}
	agent, _ := f.NewID[Agent]()
	memory, _ := InAgentMemory(project, agent)
	if _, err = registration.Actor(dispatch.String(), memory); err == nil {
		t.Fatal("Scheduler accepted Agent memory scope")
	}
	for _, cause := range []string{"", "dispatch-caller-assertion", "sha256:" + strings.Repeat("a", 64), "01900000-0000-7000-8000-00000000000A"} {
		if _, err = registration.Actor(cause, scope); err == nil {
			t.Fatal("noncanonical Dispatch cause accepted")
		}
	}
	for _, name := range []ServiceName{"Scheduler", "scheduler-admin", "task-scheduler"} {
		if _, err = RegisterService(name); err == nil {
			t.Fatal("unregistered service accepted")
		}
	}
}
