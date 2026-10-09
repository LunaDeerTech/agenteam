package contract

import (
	"context"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
)

// ObjectMetadataPurgeBatchLimit bounds deleted rows across ALL tables in one
// caller-owned transaction, including its final Object/Upload anchors.
const ObjectMetadataPurgeBatchLimit = 32

// PurgeDeletedObjectMetadataAccess is reserved for ObjectCleanupAccess. Its
// AccessRequest/Service integration is required separately; this declaration
// does not make an existing request or plan valid for the new operation.
const PurgeDeletedObjectMetadataAccess AccessOperation = "purge_deleted_object_metadata"

// DeletedObjectMetadataPurger is an independently bound optional capability.
// Its first provider supports only SkillRevision with ProjectDeleted. Every
// call revalidates the exact cause/object, private issuer plan, current owner
// authority, same Store/live Tx and complete lock union. An absent anchor is
// not successful replay. Missing capability remains DependencyUnbound.
//
// The provider establishes physical completion and actual original work join
// from its own native facts, never public result fields or cancellation. It
// performs no I/O, cancellation, lock acquisition, nested Tx or goroutine here.
// Pending preserves Object/Upload/current-attempt anchors. Completed removes
// them in this Tx, immediately before the caller removes its own anchors in
// the SAME Tx. Only that outer CommitResult establishes successful completion.
type DeletedObjectMetadataPurger interface {
	PurgeDeletedObjectMetadataInTx(context.Context, foundation.Tx, ObjectCleanupCause, ObjectID, AccessLockPlan, LockedAccess) (ObjectMetadataPurgeResult, error)
}

// ObjectMetadataPurgeResult is ordinary non-authorizing data describing
// proposed progress within a still-live Tx. It is no physical/join/commit proof.
// Errors require the caller to roll back the entire transaction.
type ObjectMetadataPurgeResult struct {
	State       CleanupState `json:"state"`
	OperationID CleanupID    `json:"operation_id"`
	ObjectID    ObjectID     `json:"object_id"`
}

func (r ObjectMetadataPurgeResult) Validate() error {
	if r.State != CleanupPending && r.State != CleanupCompleted || r.OperationID.Validate() != nil || r.ObjectID.Validate() != nil {
		return bad()
	}
	return nil
}

// MatchesOperation checks only response shape and scalar identities, not
// authority, complete owner/cause equivalence or transaction outcome.
func (r ObjectMetadataPurgeResult) MatchesOperation(operation CleanupID, object ObjectID) bool {
	return r.Validate() == nil && operation.Validate() == nil && object.Validate() == nil && r.OperationID == operation && r.ObjectID == object
}
