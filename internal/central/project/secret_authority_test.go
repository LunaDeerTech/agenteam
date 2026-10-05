package project

import (
	"context"
	"testing"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

func TestSecretAuthorityConstructionAndUnboundCapabilities(t *testing.T) {
	for _, authority := range []*Authority{nil, {}} {
		_, err := NewSecretAuthority(authority)
		hasCode(t, err, foundation.DependencyUnbound)
	}
	for _, delegate := range []*SecretAuthority{nil, {}} {
		_, err := delegate.AuthorizeProject(context.Background(), foundation.Tx{}, identity.Actor{}, identity.ProjectID{}, identity.Read)
		hasCode(t, err, foundation.DependencyUnbound)
		hasCode(t, delegate.CheckMutationInTx(context.Background(), foundation.Tx{}, identity.Actor{}, sc.CredentialRef{}), foundation.DependencyUnbound)
		hasCode(t, delegate.CheckCleanupInTx(context.Background(), foundation.Tx{}, identity.Actor{}, sc.LifecycleCause{}, identity.ProjectID{}), foundation.DependencyUnbound)
	}
	f := newAuditGateFixture(t, nil)
	a, err := NewSecretAuthority(f.a)
	if err != nil {
		t.Fatal("Owner capability must not require Lifecycle or AuditFacts", err)
	}
	*f.a = Authority{}
	grant, err := a.AuthorizeProject(context.Background(), f.store.tx, f.actor, f.project, identity.Mutate)
	scope, _ := identity.InProject(f.project)
	if err != nil || !grant.Matches(f.actor, scope, identity.Mutate) {
		t.Fatal("delegate did not capture Authority", err)
	}
	hasCode(t, a.CheckCleanupInTx(context.Background(), f.store.tx, f.actor, sc.LifecycleCause{}, f.project), foundation.DependencyUnbound)
}

func TestSecretAuthorityMutationUsesCurrentOwnerMutateGate(t *testing.T) {
	f := newAuditGateFixture(t, nil)
	a, err := NewSecretAuthority(f.a)
	if err != nil {
		t.Fatal(err)
	}
	scope, _ := identity.InProject(f.project)
	ref, _ := sc.NewCredentialRef(testID[sc.Credential](t), scope)
	if err = a.CheckMutationInTx(context.Background(), f.store.tx, f.actor, ref); err != nil {
		t.Fatal(err)
	}
	// Ref ownership/canonical existence belongs to Secret, including create
	// before INSERT and delete after DELETE. Project only checks its own gate.
	f.lifecycle = c.Archived
	hasCode(t, a.CheckMutationInTx(context.Background(), f.store.tx, f.actor, ref), foundation.ProjectNotActive)
	f.lifecycle = c.Active
	f.sessionErr = fault(foundation.SessionRevoked)
	hasCode(t, a.CheckMutationInTx(context.Background(), f.store.tx, f.actor, ref), foundation.SessionRevoked)
	f.sessionErr = nil
	hasCode(t, a.CheckMutationInTx(context.Background(), foundation.Tx{}, f.actor, ref), foundation.InvalidArgument)
	hasCode(t, a.CheckMutationInTx(context.Background(), f.store.tx, f.actor, sc.CredentialRef{}), foundation.InvalidArgument)
	system, _ := sc.NewCredentialRef(testID[sc.Credential](t), identity.SystemScope())
	hasCode(t, a.CheckMutationInTx(context.Background(), f.store.tx, f.actor, system), foundation.InvalidArgument)
	reg, _ := identity.RegisterService(identity.SecretService)
	service, _ := reg.Actor(testID[struct{}](t).String(), scope)
	hasCode(t, a.CheckMutationInTx(context.Background(), f.store.tx, service, ref), foundation.Forbidden)
	run, _ := identity.NewAgentRun(f.project, testID[identity.Agent](t), testID[identity.Execution](t))
	hasCode(t, a.CheckMutationInTx(context.Background(), f.store.tx, run, ref), foundation.DependencyUnbound)
}
