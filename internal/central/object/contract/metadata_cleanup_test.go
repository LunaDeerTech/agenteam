package contract

import (
	"encoding/json"
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
