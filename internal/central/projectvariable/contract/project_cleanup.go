package contract

import (
	"context"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

// Project owns this check. It accepts only the same Store's live transaction,
// held Project EX and the exact active Variables substep of its cleanup driver.
// Public actor/cause and deleting rows do not issue cleanup permission.
type ProjectCleanupAuthority interface {
	RequireProjectVariablesCleanupInTx(context.Context, f.Tx, i.Actor, pc.LifecycleCause, i.ProjectID) error
}

// This is only D10 ordinary/Secret Variable metadata, consumed after Agent and
// Skills. The actual fixed ordinary/Secret pair owns the call and its original
// transaction return. It does not clean D04, closed call history or complete the
// combined lifecycle participant. Nil checkpoint is the bounded head-scan
// protocol; Pending/Completed describe this child only, never parent progress.
type ProjectCleanupDomain interface {
	Cleanup(context.Context, i.Actor, pc.LifecycleCause, pc.ScopeRef, *pc.CleanupCheckpoint) (pc.CleanupReport, error)
}
