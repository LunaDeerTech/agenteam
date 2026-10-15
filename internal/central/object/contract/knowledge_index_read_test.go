package contract

import (
	"strings"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

func TestKnowledgeIndexReadGrantClosedShape(t *testing.T) {
	owner, _ := NewObjectOwner(Knowledge, testID, secondID)
	object, _ := f.ParseID[StoredObject](testID)
	actor := initializationService(t, id.KnowledgeRuntime, secondID, testID)
	base := OwnerAuthorizationDetails{Actor: actor, Owner: owner, Intent: id.Read, Existence: ExistingOwner, Version: 1, ReadObjectID: object}
	grant, err := NewOwnerAuthorization(base)
	if err != nil || !grant.MatchesObjectRead(actor, owner, object) {
		t.Fatal("exact indexing read rejected", err)
	}
	other, _ := f.ParseID[StoredObject](secondID)
	if grant.MatchesObjectRead(actor, owner, other) {
		t.Fatal("index grant widened to another object")
	}
	for name, edit := range map[string]func(*OwnerAuthorizationDetails){
		"other-service": func(d *OwnerAuthorizationDetails) {
			d.Actor = initializationService(t, id.ModelRuntime, secondID, testID)
		},
		"other-project": func(d *OwnerAuthorizationDetails) {
			d.Actor = initializationService(t, id.KnowledgeRuntime, testID, testID)
		},
		"non-job-cause": func(d *OwnerAuthorizationDetails) {
			registration, _ := id.RegisterService(id.KnowledgeRuntime)
			// This service already rejects non-UUID causes while constructing
			// its Actor; that rejected value must not become an Object grant.
			d.Actor, _ = registration.Actor("sha256:"+strings.Repeat("a", 64), owner.Scope())
		},
		"other-owner":     func(d *OwnerAuthorizationDetails) { d.Owner, _ = NewObjectOwner(MCPContent, testID, secondID) },
		"mutation":        func(d *OwnerAuthorizationDetails) { d.Intent = id.Mutate },
		"prospective":     func(d *OwnerAuthorizationDetails) { d.Existence = ProspectiveOwner; d.CreationCause = testID },
		"creation-cause":  func(d *OwnerAuthorizationDetails) { d.CreationCause = testID },
		"missing-target":  func(d *OwnerAuthorizationDetails) { d.ReadObjectID = ObjectID{} },
		"protected-lease": func(d *OwnerAuthorizationDetails) { d.ProtectedLease = &ObjectLease{} },
		"zero-version":    func(d *OwnerAuthorizationDetails) { d.Version = 0 },
	} {
		t.Run(name, func(t *testing.T) {
			d := base
			edit(&d)
			if _, err := NewOwnerAuthorization(d); err == nil {
				t.Fatal("invalid indexing read grant accepted")
			}
		})
	}
}
