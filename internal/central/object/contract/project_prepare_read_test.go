package contract

import (
	"testing"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

func TestPrepareReadPlanClosedShapeAndCompleteBinding(t *testing.T) {
	actor, owner, object := accessValues(t)
	d := AccessRequestDetails{Operation: PrepareReadAccess, Actor: actor, Owner: owner, Intent: identity.Read}
	first, err := NewOwnerAccess(d)
	if err != nil || first.Validate() != nil {
		t.Fatal("owner read planning rejected", err)
	}
	equal, err := NewOwnerAccess(d)
	if err != nil || !first.Equal(equal) {
		t.Fatal("equivalent read plans differ", err)
	}
	changed := d
	changed.Owner, _ = NewObjectOwner(Knowledge, owner.Details().ID, owner.Details().ProjectID)
	other, err := NewOwnerAccess(changed)
	if err != nil || first.Equal(other) {
		t.Fatal("owner identity lost", err)
	}
	user, _ := foundation.ParseID[identity.User](secondID)
	session, _ := foundation.ParseID[identity.Session](testID)
	changed = d
	changed.Actor, _ = identity.NewHuman(user, session)
	other, err = NewOwnerAccess(changed)
	if err != nil || first.Equal(other) {
		t.Fatal("actor identity lost", err)
	}
	changed = d
	changed.Operation = PrepareAccess
	changed.Intent = identity.Mutate
	other, err = NewOwnerAccess(changed)
	if err != nil || first.Equal(other) {
		t.Fatal("read preparation aliases write preparation", err)
	}
	req, _ := foundation.ParseID[foundation.Request](testID)
	for name, change := range map[string]func(*AccessRequestDetails){
		"mutation_intent": func(v *AccessRequestDetails) { v.Intent = identity.Mutate },
		"missing_actor":   func(v *AccessRequestDetails) { v.Actor = identity.Actor{} },
		"missing_owner":   func(v *AccessRequestDetails) { v.Owner = ObjectOwner{} },
		"object":          func(v *AccessRequestDetails) { v.ObjectID = object },
		"command": func(v *AccessRequestDetails) {
			v.Command = &foundation.CommandMeta{IdempotencyKey: "not-a-lookup", RequestID: req}
		},
		"key":         func(v *AccessRequestDetails) { v.Key = "not-a-lookup" },
		"range":       func(v *AccessRequestDetails) { v.Range = &ByteRange{Offset: 0, Length: 1} },
		"lease":       func(v *AccessRequestDetails) { v.LeaseID, _ = foundation.ParseID[Lease](secondID) },
		"objects":     func(v *AccessRequestDetails) { v.Objects = []ObjectID{object} },
		"other_union": func(v *AccessRequestDetails) { v.Kind = ObjectReadAccess },
	} {
		t.Run(name, func(t *testing.T) {
			candidate := d
			change(&candidate)
			if _, err := NewOwnerAccess(candidate); err == nil {
				t.Fatal("foreign/missing field accepted")
			}
		})
	}
	// The new operation does not loosen any old constructor's input contract.
	if _, err = NewOwnerAccess(AccessRequestDetails{Operation: PrepareAccess, Actor: actor, Owner: owner, Intent: identity.Read}); err == nil {
		t.Fatal("old write prepare accepted read")
	}
	if _, err = NewOwnerAccess(AccessRequestDetails{Operation: LookupAccess, Actor: actor, Owner: owner, Intent: identity.Read}); err == nil {
		t.Fatal("old lookup lost required key")
	}
	if _, err = NewObjectReadAccess(d); err == nil {
		t.Fatal("owner plan accepted by object-read constructor")
	}
}
