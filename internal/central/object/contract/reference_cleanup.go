package contract

import (
	"context"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
)

// ReferenceCleanup closes the exact durable publication gate in the owning
// domain's transaction: Avatar permits replacement/cancelled upload; Knowledge
// requires a Project and also permits owner deletion. Other owners are rejected.
// The matching CleanupReleaseAccess plan contains the original UploadID; a
// caller-supplied cause is not cleanup authorization.
type ReferenceCleanup interface {
	ReleaseForCleanupInTx(context.Context, foundation.Tx, ObjectCleanupCause, ObjectID, AccessLockPlan, LockedAccess) error
}
