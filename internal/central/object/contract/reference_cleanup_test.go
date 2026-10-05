package contract

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

func TestAvatarCleanupReleaseClosedAndExactBinding(t *testing.T) {
	actor, _, object := accessValues(t)
	owner, _ := NewObjectOwner(Avatar, testID, "")
	op, _ := foundation.ParseID[CleanupOperation](secondID)
	upload, _ := foundation.ParseID[Upload](testID)
	cause, e := NewObjectCleanupCause(CleanupDetails{Owner: owner, OperationID: op, Reason: ReplacedObject})
	if e != nil {
		t.Fatal(e)
	}
	d := AccessRequestDetails{Operation: ReleaseForCleanupAccess, ObjectID: object, UploadID: upload, Cleanup: cause}
	r, e := NewCleanupReleaseAccess(d)
	if e != nil || r.Validate() != nil {
		t.Fatal("exact cleanup", e)
	}
	for name, mutate := range map[string]func(*AccessRequestDetails){
		"missing upload": func(d *AccessRequestDetails) { d.UploadID = UploadID{} },
		"actor":          func(d *AccessRequestDetails) { d.Actor = actor },
		"ordinary owner": func(d *AccessRequestDetails) { d.Owner = owner },
		"intent":         func(d *AccessRequestDetails) { d.Intent = identity.Converge },
		"key":            func(d *AccessRequestDetails) { d.Key = "not-a-cleanup-field" },
		"operation":      func(d *AccessRequestDetails) { d.Operation = ReleaseAccess },
		"object":         func(d *AccessRequestDetails) { d.ObjectID = ObjectID{} },
		"foreign owner kind": func(d *AccessRequestDetails) {
			o, _ := NewObjectOwner(Artifact, testID, secondID)
			d.Cleanup, _ = NewObjectCleanupCause(CleanupDetails{Owner: o, OperationID: op, Reason: ReplacedObject})
		},
	} {
		bad := d
		mutate(&bad)
		if _, err := NewCleanupReleaseAccess(bad); err == nil {
			t.Fatal("mixed/invalid cleanup accepted", name)
		}
	}
	for name, mutate := range map[string]func(*AccessRequestDetails){
		"upload": func(d *AccessRequestDetails) { d.UploadID, _ = foundation.ParseID[Upload](secondID) },
		"object": func(d *AccessRequestDetails) { d.ObjectID, _ = foundation.ParseID[StoredObject](testID) },
		"reason": func(d *AccessRequestDetails) {
			d.Cleanup, _ = NewObjectCleanupCause(CleanupDetails{Owner: owner, OperationID: op, Reason: CancelledUpload})
		},
		"cause": func(d *AccessRequestDetails) {
			other, _ := foundation.ParseID[CleanupOperation](testID)
			d.Cleanup, _ = NewObjectCleanupCause(CleanupDetails{Owner: owner, OperationID: other, Reason: ReplacedObject})
		},
	} {
		changed := d
		mutate(&changed)
		other, err := NewCleanupReleaseAccess(changed)
		if err != nil || r.Equal(other) {
			t.Fatal("cleanup identity collapsed", name, err)
		}
	}
	ordinary := AccessRequestDetails{Operation: ReleaseAccess, Actor: actor, Owner: owner, Intent: identity.Converge, ObjectID: object, UploadID: upload}
	if _, e = NewOwnerAccess(ordinary); e == nil {
		t.Fatal("new field entered old union variant")
	}
	for _, v := range []any{r, struct{ hidden AccessRequest }{r}} {
		raw, _ := json.Marshal(v)
		for _, out := range []string{string(raw), fmt.Sprintf("%#v", v)} {
			if strings.Contains(out, testID) || strings.Contains(out, secondID) {
				t.Fatal("cleanup lookup identity leaked")
			}
		}
	}
	if json.Unmarshal([]byte(`{"held":true}`), &r) == nil {
		t.Fatal("wire constructed cleanup authority")
	}
}
