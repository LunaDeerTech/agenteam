package contract

import (
	"testing"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

func TestKnowledgeCleanupReleaseClosedOwnerAndReasonMatrix(t *testing.T) {
	_, _, object := accessValues(t)
	op, _ := foundation.ParseID[CleanupOperation](secondID)
	upload, _ := foundation.ParseID[Upload](testID)
	for _, kind := range []OwnerKind{Avatar, Knowledge, Artifact, SkillRevision, MCPContent, ExecutionPayload, MeetingFile} {
		project := secondID
		if kind == Avatar {
			project = ""
		}
		owner, err := NewObjectOwner(kind, testID, project)
		if err != nil {
			t.Fatal(err)
		}
		for _, reason := range []CleanupReason{ReplacedObject, CancelledUpload, OwnerDeleted, ProjectDeleted, AbandonedAttempt} {
			cause, err := NewObjectCleanupCause(CleanupDetails{Owner: owner, OperationID: op, Reason: reason})
			if err != nil {
				t.Fatal(err)
			}
			_, err = NewCleanupReleaseAccess(AccessRequestDetails{Operation: ReleaseForCleanupAccess, ObjectID: object, UploadID: upload, Cleanup: cause})
			want := (kind == Avatar || kind == Knowledge) && (reason == ReplacedObject || reason == CancelledUpload) || kind == Knowledge && reason == OwnerDeleted
			if (err == nil) != want {
				t.Fatalf("closed cleanup matrix %s/%s: accepted=%v want=%v", kind, reason, err == nil, want)
			}
		}
	}
	for _, project := range []string{"", "not-a-project"} {
		if _, err := NewObjectOwner(Knowledge, testID, project); err == nil {
			t.Fatal("Knowledge owner without valid Project accepted")
		}
	}
}

func TestKnowledgeCleanupReleaseBindsProjectOwnerUploadObjectAndCause(t *testing.T) {
	actor, _, object := accessValues(t)
	op, _ := foundation.ParseID[CleanupOperation](secondID)
	upload, _ := foundation.ParseID[Upload](testID)
	owner, _ := NewObjectOwner(Knowledge, testID, secondID)
	cause, _ := NewObjectCleanupCause(CleanupDetails{Owner: owner, OperationID: op, Reason: OwnerDeleted})
	base := AccessRequestDetails{Operation: ReleaseForCleanupAccess, ObjectID: object, UploadID: upload, Cleanup: cause}
	request, err := NewCleanupReleaseAccess(base)
	if err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*AccessRequestDetails){
		"project": func(d *AccessRequestDetails) {
			v := cause.Details()
			v.Owner, _ = NewObjectOwner(Knowledge, testID, testID)
			d.Cleanup, _ = NewObjectCleanupCause(v)
		},
		"owner": func(d *AccessRequestDetails) {
			v := cause.Details()
			v.Owner, _ = NewObjectOwner(Knowledge, secondID, secondID)
			d.Cleanup, _ = NewObjectCleanupCause(v)
		},
		"operation id": func(d *AccessRequestDetails) {
			v := cause.Details()
			v.OperationID, _ = foundation.ParseID[CleanupOperation](testID)
			d.Cleanup, _ = NewObjectCleanupCause(v)
		},
		"reason": func(d *AccessRequestDetails) {
			v := cause.Details()
			v.Reason = ReplacedObject
			d.Cleanup, _ = NewObjectCleanupCause(v)
		},
		"upload": func(d *AccessRequestDetails) { d.UploadID, _ = foundation.ParseID[Upload](secondID) },
		"object": func(d *AccessRequestDetails) { d.ObjectID, _ = foundation.ParseID[StoredObject](testID) },
	} {
		changed := base
		mutate(&changed)
		other, err := NewCleanupReleaseAccess(changed)
		if err != nil || request.Equal(other) {
			t.Fatal("different cleanup identity collapsed", name, err)
		}
	}
	for name, mutate := range map[string]func(*AccessRequestDetails){
		"actor":     func(d *AccessRequestDetails) { d.Actor = actor },
		"owner":     func(d *AccessRequestDetails) { d.Owner = owner },
		"intent":    func(d *AccessRequestDetails) { d.Intent = identity.Converge },
		"key":       func(d *AccessRequestDetails) { d.Key = "ordinary-key" },
		"operation": func(d *AccessRequestDetails) { d.Operation = ReleaseAccess },
		"no upload": func(d *AccessRequestDetails) { d.UploadID = UploadID{} },
		"no object": func(d *AccessRequestDetails) { d.ObjectID = ObjectID{} },
		"no cause":  func(d *AccessRequestDetails) { d.Cleanup = ObjectCleanupCause{} },
	} {
		changed := base
		mutate(&changed)
		if _, err := NewCleanupReleaseAccess(changed); err == nil {
			t.Fatal("ordinary/missing field accepted", name)
		}
	}
}
