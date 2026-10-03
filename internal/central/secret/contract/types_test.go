package contract

import (
	"encoding/json"
	"testing"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

func contractID[K any](t *testing.T) foundation.ID[K] {
	t.Helper()
	id, err := foundation.NewID[K]()
	if err != nil {
		t.Fatal(err)
	}
	return id
}
func TestCredentialIdentityAndGrantBoundaries(t *testing.T) {
	id := contractID[Credential](t)
	project := contractID[identity.Project](t)
	scope, _ := identity.InProject(project)
	ref, err := NewCredentialRef(id, scope)
	if err != nil || !ref.Equal(ref) {
		t.Fatal(err)
	}
	system, _ := NewCredentialRef(id, identity.SystemScope())
	if ref.Equal(system) {
		t.Fatal("scopes interchangeable")
	}
	if _, err = NewCredentialRef(id, identity.Scope{}); err == nil {
		t.Fatal("zero scope")
	}
	owner, err := NewCredentialLeaseOwner(ModelCallOwner, contractID[struct{}](t).String())
	if err != nil || !owner.Equal(owner) {
		t.Fatal(err)
	}
	if _, err = NewCredentialLeaseOwner("unknown", owner.Details().ID); err == nil {
		t.Fatal("unknown owner kind")
	}
	reg, _ := identity.RegisterService(identity.SecretService)
	actor, _ := reg.Actor(owner.Details().ID, scope)
	grant := UseGrant{Subject: actor, Consumer: Model}
	if grant.Validate(scope) != nil || grant.Validate(identity.SystemScope()) == nil || grant.Validate(identity.Scope{}) == nil {
		t.Fatal("scope grant validation")
	}
	grant.RequestID = "arbitrary request"
	if grant.Validate(scope) == nil {
		t.Fatal("free-form request identity")
	}
	for _, value := range []any{&ref, &owner} {
		if json.Unmarshal([]byte(`{}`), value) == nil {
			t.Fatal("external private identity injection")
		}
	}
	cause, err := NewLifecycleCause(contractID[LifecycleOperation](t), 1, true)
	if err != nil || cause.Validate() != nil || !cause.Details().Deleting {
		t.Fatal(err)
	}
	if _, err = NewLifecycleCause(contractID[LifecycleOperation](t), 0, true); err == nil {
		t.Fatal("zero lifecycle version")
	}
}
