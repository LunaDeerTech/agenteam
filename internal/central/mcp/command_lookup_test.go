package mcp

import (
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

func TestMCPOAuthHumanRefreshStoppedOriginalSessionOnly(t *testing.T) {
	source, _ := humanRefreshRecoveryFacts(t)
	user, _ := f.ParseID[id.User](source.caller.user)
	session, _ := f.ParseID[id.Session](source.caller.session)
	actor, _ := id.NewHuman(user, session)
	project, _ := f.ParseID[id.Project](source.project)
	scope, _ := id.InProject(project)
	if source.stoppedHumanRefreshCaller(actor, scope, source.caller.command) {
		t.Fatal("live refresh exposed stopped")
	}
	source.phase, source.retired, source.released = "unknown", true, true
	if !source.stoppedHumanRefreshCaller(actor, scope, source.caller.command) || source.phase != "unknown" {
		t.Fatal("original retired Unknown was changed or rejected")
	}
	otherActor, _ := id.NewHuman(user, contentTailID[id.Session](t, "61"))
	otherScope, _ := id.InProject(contentTailID[id.Project](t, "62"))
	for _, request := range []struct {
		actor   id.Actor
		scope   id.Scope
		command string
	}{
		{otherActor, scope, source.caller.command}, {actor, otherScope, source.caller.command},
		{actor, scope, digest([]byte("new-refresh"))}, {id.Actor{}, scope, source.caller.command},
	} {
		if source.stoppedHumanRefreshCaller(request.actor, request.scope, request.command) {
			t.Fatal("another session/project/command observed original stopped intent")
		}
	}
	for _, mutate := range []func(*oauthRefreshFact){
		func(v *oauthRefreshFact) { v.phase = "claimed" },
		func(v *oauthRefreshFact) { v.retired = false },
		func(v *oauthRefreshFact) { v.released = false },
		func(v *oauthRefreshFact) { v.process = "" },
	} {
		other := source
		mutate(&other)
		if other.stoppedHumanRefreshCaller(actor, scope, source.caller.command) {
			t.Fatal("incomplete original retirement exposed stopped")
		}
	}
}
