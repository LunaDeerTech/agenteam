package contract

import (
	"context"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
)

// ReferenceCleanup closes an Avatar's durable publication gate in the owning
// domain's transaction. The matching CleanupReleaseAccess plan contains the
// original UploadID; a caller-supplied cause is not cleanup authorization.
type ReferenceCleanup interface {
	ReleaseForCleanupInTx(context.Context, foundation.Tx, ObjectCleanupCause, ObjectID, AccessLockPlan, LockedAccess) error
}
