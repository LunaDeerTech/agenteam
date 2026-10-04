package contract

import (
	"context"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

// DownloadCleanup removes this Project's terminal download bindings in the
// caller's final lifecycle Tx. The caller first confirms its business/source
// work and physical object cleanup. A FinishProjectAccess plan with no objects
// supplies the already-held Project EX and current cleanup authority; no locks
// are acquired or transactions opened by this method. Missing capability is an
// unbound dependency, never permission to skip permanent deletion.
type DownloadCleanup interface {
	PurgeProjectDownloadsInTx(context.Context, foundation.Tx, identity.Actor, ProjectCleanupCause, AccessLockPlan, LockedAccess) error
}
