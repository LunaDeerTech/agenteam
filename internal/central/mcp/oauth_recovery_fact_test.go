package mcp

import (
	"context"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

func oauthRecoveryTestFact(t *testing.T) oauthRecoveryFact {
	t.Helper()
	newID := func() string {
		value, err := f.NewID[f.Request]()
		if err != nil {
			t.Fatal(err)
		}
		return value.String()
	}
	return oauthRecoveryFact{request: newID(), operation: "exchange", process: newID(), project: newID(), config: newID(),
		connection: newID(), flow: newID(), command: digest([]byte("original OAuth command")), user: newID(), session: newID(),
		credential: newID(), lease: newID(), revision: 3, version: 2, credentialVersion: 1, phase: "claimed"}
}

func TestOAuthRecoveryFactNeverReplacesOriginalCaller(t *testing.T) {
	v := oauthRecoveryTestFact(t)
	if v.validate() != nil || !v.sameOrigin(v) {
		t.Fatal("valid original request rejected")
	}
	for _, change := range []func(*oauthRecoveryFact){
		func(p *oauthRecoveryFact) { p.request = p.flow },
		func(p *oauthRecoveryFact) { p.process = p.project },
		func(p *oauthRecoveryFact) { p.project = p.config },
		func(p *oauthRecoveryFact) { p.connection = p.flow },
		func(p *oauthRecoveryFact) { p.flow = p.credential },
		func(p *oauthRecoveryFact) { p.revision++ },
		func(p *oauthRecoveryFact) { p.version++ },
		func(p *oauthRecoveryFact) { p.credentialVersion++ },
		func(p *oauthRecoveryFact) { p.lease = p.credential },
		func(p *oauthRecoveryFact) { p.session = p.user },
		func(p *oauthRecoveryFact) { p.command = digest([]byte("another command")) },
	} {
		copy := v
		change(&copy)
		if v.sameOrigin(copy) {
			t.Fatal("original immutable tuple replaced")
		}
	}
	for _, change := range []func(*oauthRecoveryFact){
		func(p *oauthRecoveryFact) { p.credential = "" },
		func(p *oauthRecoveryFact) { p.resultVersion = 1 },
		func(p *oauthRecoveryFact) { p.lease = "" },
		func(p *oauthRecoveryFact) { p.released = true },
		func(p *oauthRecoveryFact) { p.operation = "refresh" },
		func(p *oauthRecoveryFact) { p.phase = "invented" },
	} {
		copy := v
		change(&copy)
		if copy.validate() == nil {
			t.Fatal("malformed fact accepted")
		}
	}
	// Neither a parsed fact nor a registered technical Actor creates an active
	// recovery call or a stopped Guard proof.
	s := &Service{}
	ctx := context.WithValue(context.Background(), oauthRecoveryKey{}, &oauthRecoveryUse{service: s, original: v})
	if _, err := s.oauthRecoveryUse(ctx); err == nil {
		t.Fatal("row data admitted a recovery owner")
	}
}

func TestOAuthRecoveryDeleteRebuildKeepsOriginalBinding(t *testing.T) {
	v := oauthRecoveryTestFact(t)
	v.operation, v.lease, v.retired, v.released = "retire", "", true, true
	project, _ := f.ParseID[id.Project](v.project)
	scope, _ := id.InProject(project)
	registration, _ := id.RegisterService(id.SecretService)
	actor, _ := registration.Actor(v.request, scope)
	key, _ := f.ParseIdempotencyKey(v.request)
	command, _ := f.NewCommandIdentity("secret.mcp", []string{v.project, v.connection, v.flow}, "delete", key)
	credential, _ := f.ParseID[sc.Credential](v.credential)
	ref, _ := sc.NewCredentialRef(credential, scope)
	original, err := sc.NewMCPOAuthWriteRequest(sc.MCPOAuthWriteFields{Actor: actor, Identity: command, ProjectID: project,
		ConnectionID: v.connection, ConfigRevision: v.revision, Kind: sc.Delete, Ref: ref, ExpectedVersion: v.credentialVersion,
		OwnerKind: sc.MCPOAuthFlow, OwnerID: v.flow})
	if err != nil {
		t.Fatal(err)
	}
	binding, _ := sc.MCPOAuthWriteBinding(original)
	v.writeBinding = string(binding)
	rebuilt, err := oauthRecoveryDeleteRequest(v)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := sc.MCPOAuthWriteBinding(rebuilt)
	if got != binding || rebuilt.Fields().Identity.Canonical() != command.Canonical() {
		t.Fatal("cold Delete changed original identity")
	}
	v.credentialVersion++
	if _, err := oauthRecoveryDeleteRequest(v); err == nil {
		t.Fatal("changed original version accepted")
	}
}
