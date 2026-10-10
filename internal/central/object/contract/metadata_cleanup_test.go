package contract

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
)

func TestObjectMetadataPurgeClosedResult(t *testing.T) {
	operation, _ := foundation.ParseID[CleanupOperation](testID)
	object, _ := foundation.ParseID[StoredObject](secondID)
	for _, state := range []CleanupState{CleanupPending, CleanupCompleted} {
		r := ObjectMetadataPurgeResult{state, operation, object}
		if r.Validate() != nil || !r.MatchesOperation(operation, object) {
			t.Fatal("valid proposed progress rejected")
		}
	}
	for _, r := range []ObjectMetadataPurgeResult{
		{}, {CleanupFailed, operation, object}, {"unknown", operation, object},
		{State: CleanupCompleted, ObjectID: object}, {State: CleanupCompleted, OperationID: operation},
	} {
		if r.Validate() == nil || r.MatchesOperation(operation, object) {
			t.Fatal("invalid progress accepted")
		}
	}
}

func TestObjectMetadataPurgeAccessClosedScopeAndToken(t *testing.T) {
	actor, _, object := accessValues(t)
	operation, _ := foundation.ParseID[CleanupOperation](testID)
	var request AccessRequest
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
			cause, err := NewObjectCleanupCause(CleanupDetails{OperationID: operation, Owner: owner, Reason: reason})
			if err != nil {
				t.Fatal(err)
			}
			d := AccessRequestDetails{Operation: PurgeDeletedObjectMetadataAccess, ObjectID: object, Cleanup: cause}
			r, err := NewObjectCleanupAccess(d)
			want := kind == SkillRevision && reason == ProjectDeleted
			if (err == nil) != want {
				t.Fatalf("purge closure %s/%s: accepted=%v", kind, reason, err == nil)
			}
			if want {
				request = r
			}
			// The new operation never narrows the existing physical cleanup port.
			d.Operation = CleanupObjectAccess
			if _, err = NewObjectCleanupAccess(d); err != nil {
				t.Fatal("existing physical cleanup changed", err)
			}
		}
	}
	for name, mutate := range map[string]func(*AccessRequestDetails){
		"actor":     func(d *AccessRequestDetails) { d.Actor = actor },
		"key":       func(d *AccessRequestDetails) { d.Key = "not-purge-authority" },
		"upload":    func(d *AccessRequestDetails) { d.UploadID, _ = foundation.ParseID[Upload](testID) },
		"object":    func(d *AccessRequestDetails) { d.ObjectID = ObjectID{} },
		"cause":     func(d *AccessRequestDetails) { d.Cleanup = ObjectCleanupCause{} },
		"operation": func(d *AccessRequestDetails) { d.Operation = "future_purge" },
	} {
		d := request.Details()
		mutate(&d)
		if _, err := NewObjectCleanupAccess(d); err == nil {
			t.Fatal("foreign or missing field accepted", name)
		}
	}
	issuer := NewAccessIssuer()
	tx := foundation.NewTx()
	key, _ := foundation.AggregateLock(foundation.ObjectAggregate, object.String())
	locks := []foundation.LockRequest{{Key: key, Mode: foundation.Exclusive}}
	binding := foundation.Digest("sha256:" + strings.Repeat("a", 64))
	dependencies, _ := NewAccessDependencies(binding, locks)
	plan, err := NewAccessLockPlan(issuer, AccessPlanDetails{Request: request, DependencyRequest: request, Dependencies: dependencies, DomainBinding: binding, Objects: []ObjectID{object}, Locks: locks})
	if err != nil {
		t.Fatal(err)
	}
	token, err := NewLockedAccess(issuer, tx, []AccessLockPlan{plan}, nil)
	if err != nil || !token.Matches(issuer, tx, plan, request) {
		t.Fatal("exact opaque operation rejected", err)
	}
	for name, mutate := range map[string]func(*AccessRequestDetails){
		"physical cleanup": func(d *AccessRequestDetails) { d.Operation = CleanupObjectAccess },
		"other object":     func(d *AccessRequestDetails) { d.ObjectID, _ = foundation.ParseID[StoredObject](testID) },
		"other cause": func(d *AccessRequestDetails) {
			c := d.Cleanup.Details()
			c.OperationID, _ = foundation.ParseID[CleanupOperation](secondID)
			d.Cleanup, _ = NewObjectCleanupCause(c)
		},
	} {
		d := request.Details()
		mutate(&d)
		other, err := NewObjectCleanupAccess(d)
		if err != nil || token.Matches(issuer, tx, plan, other) {
			t.Fatal("opaque plan crossed operation identity", name, err)
		}
	}
	if token.Matches(NewAccessIssuer(), tx, plan, request) || token.Matches(issuer, foundation.NewTx(), plan, request) {
		t.Fatal("foreign issuer or Tx accepted")
	}
}

func TestObjectMetadataPurgeScalarIdentityIsNotProof(t *testing.T) {
	operation, _ := foundation.ParseID[CleanupOperation](testID)
	otherOperation, _ := foundation.ParseID[CleanupOperation](secondID)
	object, _ := foundation.ParseID[StoredObject](secondID)
	otherObject, _ := foundation.ParseID[StoredObject](testID)
	r := ObjectMetadataPurgeResult{CleanupCompleted, operation, object}
	if r.MatchesOperation(otherOperation, object) || r.MatchesOperation(operation, otherObject) || r.MatchesOperation(CleanupID{}, object) || r.MatchesOperation(operation, ObjectID{}) {
		t.Fatal("crossed operation or object")
	}
	// Anyone can reconstruct this data. Consumers need a real provider call
	// followed by the actual outer CommitResult, never a shape-based proof.
	raw, err := json.Marshal(r)
	var copy ObjectMetadataPurgeResult
	if err != nil || json.Unmarshal(raw, &copy) != nil || copy != r {
		t.Fatal("ordinary result did not round trip")
	}
	if ObjectMetadataPurgeBatchLimit != 32 {
		t.Fatal("transaction-wide row budget changed")
	}
}
