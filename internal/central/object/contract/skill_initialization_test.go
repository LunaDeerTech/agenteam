package contract

import (
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"strings"
	"testing"
)

func initializationService(t *testing.T, name id.ServiceName, project, cause string) id.Actor {
	t.Helper()
	p, e := f.ParseID[id.Project](project)
	if e != nil {
		t.Fatal(e)
	}
	scope, _ := id.InProject(p)
	role, e := id.RegisterService(name)
	if e != nil {
		t.Fatal(e)
	}
	a, e := role.Actor(cause, scope)
	if e != nil {
		t.Fatal(e)
	}
	return a
}
func TestSkillInitializationOwnerGrantClosedShape(t *testing.T) {
	owner, _ := NewObjectOwner(SkillRevision, testID, secondID)
	actor := initializationService(t, id.ProjectInitialization, secondID, testID)
	for _, existence := range []OwnerExistence{ProspectiveOwner, ExistingOwner} {
		for _, intent := range []id.AccessIntent{id.Read, id.Mutate, id.Converge, id.Lifecycle} {
			grant, e := NewOwnerAuthorization(OwnerAuthorizationDetails{Actor: actor, Owner: owner, Intent: intent, Existence: existence, CreationCause: testID, Version: 1})
			if e != nil || !grant.Matches(actor, owner, intent) {
				t.Fatal("exact initialization", e)
			}
		}
	}
	base := OwnerAuthorizationDetails{Actor: actor, Owner: owner, Intent: id.Mutate, Existence: ExistingOwner, CreationCause: testID, Version: 1}
	for name, edit := range map[string]func(*OwnerAuthorizationDetails){
		"service": func(d *OwnerAuthorizationDetails) {
			d.Actor = initializationService(t, id.ObjectService, secondID, testID)
		},
		"project": func(d *OwnerAuthorizationDetails) {
			d.Actor = initializationService(t, id.ProjectInitialization, testID, testID)
		},
		"cause": func(d *OwnerAuthorizationDetails) { d.CreationCause = secondID },
		"digest_cause": func(d *OwnerAuthorizationDetails) {
			d.CreationCause = "sha256:" + strings.Repeat("a", 64)
			d.Actor = initializationService(t, id.ProjectInitialization, secondID, d.CreationCause)
		},
		"missing_cause": func(d *OwnerAuthorizationDetails) { d.CreationCause = "" },
		"owner":         func(d *OwnerAuthorizationDetails) { d.Owner, _ = NewObjectOwner(Artifact, testID, secondID) },
		"read_body": func(d *OwnerAuthorizationDetails) {
			d.Intent = id.Read
			d.ReadObjectID, _ = f.ParseID[StoredObject](testID)
		},
		"intent": func(d *OwnerAuthorizationDetails) { d.Intent = id.Launch },
	} {
		t.Run(name, func(t *testing.T) {
			d := base
			edit(&d)
			if _, e := NewOwnerAuthorization(d); e == nil {
				t.Fatal("foreign initialization grant accepted")
			}
		})
	}
	// All previously admitted ordinary convergence/lifecycle shapes remain.
	for _, name := range []id.ServiceName{id.ObjectService, id.SecretService, id.ProjectLifecycle} {
		for _, intent := range []id.AccessIntent{id.Read, id.Mutate, id.Converge, id.Lifecycle} {
			d := base
			d.Actor = initializationService(t, name, secondID, testID)
			d.Intent = intent
			_, e := NewOwnerAuthorization(d)
			want := intent == id.Converge || intent == id.Lifecycle
			if (e == nil) != want {
				t.Fatal("changed old Service matrix", name, intent, e)
			}
		}
	}
}
func TestSkillCleanupReleaseExactIdentity(t *testing.T) {
	_, _, object := accessValues(t)
	owner, _ := NewObjectOwner(SkillRevision, testID, secondID)
	op, _ := f.ParseID[CleanupOperation](secondID)
	upload, _ := f.ParseID[Upload](testID)
	cause, _ := NewObjectCleanupCause(CleanupDetails{Owner: owner, OperationID: op, Reason: ProjectDeleted})
	base := AccessRequestDetails{Operation: ReleaseForCleanupAccess, ObjectID: object, UploadID: upload, Cleanup: cause}
	request, e := NewCleanupReleaseAccess(base)
	if e != nil {
		t.Fatal(e)
	}
	for _, reason := range []CleanupReason{ReplacedObject, CancelledUpload, OwnerDeleted, AbandonedAttempt} {
		d := base
		d.Cleanup, _ = NewObjectCleanupCause(CleanupDetails{Owner: owner, OperationID: op, Reason: reason})
		if _, e := NewCleanupReleaseAccess(d); e == nil {
			t.Fatal("non-project Skill removal")
		}
	}
	for _, edit := range []func(*AccessRequestDetails){
		func(d *AccessRequestDetails) { d.ObjectID = ObjectID{} }, func(d *AccessRequestDetails) { d.UploadID = UploadID{} }, func(d *AccessRequestDetails) { d.Operation = ReleaseAccess }, func(d *AccessRequestDetails) { d.Owner = owner },
	} {
		d := base
		edit(&d)
		if _, e := NewCleanupReleaseAccess(d); e == nil {
			t.Fatal("missing or mixed field")
		}
	}
	for _, edit := range []func(*AccessRequestDetails){
		func(d *AccessRequestDetails) { d.UploadID, _ = f.ParseID[Upload](secondID) },
		func(d *AccessRequestDetails) { d.ObjectID, _ = f.ParseID[StoredObject](testID) },
		func(d *AccessRequestDetails) {
			v := cause.Details()
			v.OperationID, _ = f.ParseID[CleanupOperation](testID)
			d.Cleanup, _ = NewObjectCleanupCause(v)
		},
		func(d *AccessRequestDetails) {
			v := cause.Details()
			v.Owner, _ = NewObjectOwner(SkillRevision, secondID, secondID)
			d.Cleanup, _ = NewObjectCleanupCause(v)
		},
	} {
		d := base
		edit(&d)
		other, e := NewCleanupReleaseAccess(d)
		if e != nil || request.Equal(other) {
			t.Fatal("identity lost", e)
		}
	}
}
