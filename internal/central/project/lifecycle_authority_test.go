package project

import (
	"context"
	"testing"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

func lifecycleTestAuthority(t *testing.T) *LifecycleAuthority {
	t.Helper()
	a, err := NewLifecycleAuthority(&authorityStore{}, registryTestManifest(t, registryTestEntries()), nil)
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func lifecycleTestActor(t *testing.T) (identity.Actor, c.ProjectID, c.LifecycleCause) {
	t.Helper()
	project := testID[identity.Project](t)
	cause := c.LifecycleCause{OperationID: testID[c.Operation](t), Action: c.Archive, ProjectVersion: 3}
	registration, _ := identity.RegisterService(identity.ProjectLifecycle)
	scope, _ := identity.InProject(project)
	actor, err := registration.Actor(cause.OperationID.String(), scope)
	if err != nil {
		t.Fatal(err)
	}
	return actor, project, cause
}

func TestLifecycleAuthorityConstructionAndCompatibility(t *testing.T) {
	manifest := registryTestManifest(t, registryTestEntries())
	var typedNil *authorityStore
	for _, store := range []Store{nil, typedNil} {
		_, err := NewLifecycleAuthority(store, manifest, nil)
		hasCode(t, err, foundation.DependencyUnbound)
	}
	store := &authorityStore{}
	_, err := NewLifecycleAuthority(store, c.RequiredManifest{}, nil)
	if err == nil {
		t.Fatal("empty manifest bound")
	}
	_, err = NewLifecycleAuthority(store, manifest, manifest.Entries()[:1])
	hasCode(t, err, foundation.InvalidArgument)
	old := registryTestEntries()
	for i := range old {
		old[i].ContractVersion = 2
	}
	a, err := NewLifecycleAuthority(store, manifest, old)
	if err != nil {
		t.Fatal(err)
	}
	old[0].ReferenceKinds[0] = "changed"
	old[2].CleanupAfter[0] = c.AuditParticipant
	if a.declarations[lifecycleParticipantKey{c.ArtifactObjectParticipant, 2}].ReferenceKinds[1] != "object" || a.declarations[lifecycleParticipantKey{c.OutboxParticipant, 2}].CleanupAfter[0] != c.ArtifactObjectParticipant {
		t.Fatal("declarations alias caller slices")
	}
	sessions := sessionFunc(func(context.Context, foundation.Tx, identity.Actor) error {
		t.Fatal("constructor called Session")
		return nil
	})
	for _, facts := range []*LifecycleAuthority{nil, {}, lifecycleTestAuthority(t)} {
		_, err := NewAuthority(store, AuthorityDependencies{Sessions: sessions, Lifecycle: facts})
		hasCode(t, err, foundation.DependencyUnbound)
	}
	if _, err := NewAuthority(store, AuthorityDependencies{Sessions: sessions, Lifecycle: a}); err != nil {
		t.Fatal(err)
	}
	if _, err := NewAuthority(store, AuthorityDependencies{Sessions: sessions}); err != nil {
		t.Fatal("legacy nil lifecycle", err)
	}
	var zero LifecycleAuthority
	actor, _, cause := lifecycleTestActor(t)
	hasCode(t, zero.ValidateLifecycleInTx(context.Background(), foundation.NewTx(), actor, cause, c.OutboxParticipant, c.StopPhase), foundation.DependencyUnbound)
}
