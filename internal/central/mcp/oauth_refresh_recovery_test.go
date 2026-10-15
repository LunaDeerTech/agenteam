package mcp

import (
	"context"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
)

func refreshRecoveryFact(t *testing.T) oauthRefreshFact {
	t.Helper()
	return oauthRefreshFact{request: contentTailID[f.Request](t, "40").String(), process: contentTailID[oc.Process](t, "41").String(), project: contentTailID[struct{}](t, "42").String(), config: contentTailID[struct{}](t, "43").String(), connection: contentTailID[struct{}](t, "44").String(), credential: contentTailID[struct{}](t, "45").String(), revision: 2, connectionVersion: 3, credentialVersion: 4, caller: oauthRefreshCaller{startup: contentTailID[f.Request](t, "46").String()}, phase: "claimed"}
}

func TestMCPOAuthRefreshRecoveryRequiresPrivateStoppedCall(t *testing.T) {
	fact := refreshRecoveryFact(t)
	owner := &processOwner{process: contentTailID[oc.Process](t, "47"), calls: map[*processCall]struct{}{}}
	s := &Service{owner: owner}
	w := &oauthRefreshRecovery{service: s, call: &processCall{owner: owner}, original: fact}
	if fact.validate() != nil {
		t.Fatal("fixture original caller")
	}
	if w.require(context.Background(), f.NewTx()) == nil {
		t.Fatal("persisted row alone proved process stopped")
	}
	w.stopped = true
	if w.require(context.Background(), f.NewTx()) == nil {
		t.Fatal("unregistered private call reached current guard")
	}
	owner.calls[w.call] = struct{}{}
	w.original.process = owner.process.String()
	if w.require(context.Background(), f.NewTx()) == nil {
		t.Fatal("current process became a stopped origin")
	}
	w.original = fact
	w.call = &processCall{owner: &processOwner{}}
	if w.require(context.Background(), f.NewTx()) == nil {
		t.Fatal("another process owner's call was borrowed")
	}
}

func TestMCPOAuthRefreshRecoveryKeepsExactOriginalCaller(t *testing.T) {
	original := refreshRecoveryFact(t)
	otherID := contentTailID[f.Request](t, "48").String()
	for name, change := range map[string]func(*oauthRefreshFact){
		"request":            func(v *oauthRefreshFact) { v.request = otherID },
		"process":            func(v *oauthRefreshFact) { v.process = otherID },
		"project":            func(v *oauthRefreshFact) { v.project = otherID },
		"config":             func(v *oauthRefreshFact) { v.config = otherID },
		"connection":         func(v *oauthRefreshFact) { v.connection = otherID },
		"credential":         func(v *oauthRefreshFact) { v.credential = otherID },
		"revision":           func(v *oauthRefreshFact) { v.revision++ },
		"connection-version": func(v *oauthRefreshFact) { v.connectionVersion++ },
		"credential-version": func(v *oauthRefreshFact) { v.credentialVersion++ },
		"startup":            func(v *oauthRefreshFact) { v.caller.startup = otherID },
	} {
		t.Run(name, func(t *testing.T) {
			changed := original
			change(&changed)
			if original.sameOrigin(changed) {
				t.Fatal("substituted original tuple accepted")
			}
		})
	}
	for _, caller := range []oauthRefreshCaller{{}, {startup: original.caller.startup, execution: otherID}, {user: otherID, session: otherID}, {execution: otherID, operation: otherID, attempt: otherID, request: otherID}} {
		changed := original
		changed.caller = caller
		if changed.validate() == nil {
			t.Fatal("incomplete or mixed caller accepted")
		}
	}
	// Terminal state may become visible after the real old transaction returns.
	// It is preserved, while the immutable origin still has to match exactly.
	complete := original
	complete.phase = "complete"
	complete.retired = true
	complete.released = true
	if !original.sameOrigin(complete) || complete.validate() != nil {
		t.Fatal("original committed postimage was replaced")
	}
}
