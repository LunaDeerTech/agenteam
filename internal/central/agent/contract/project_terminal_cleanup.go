package contract

import (
	"context"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

// The fixed Agent terminal owner restores the last complete immutable anchor
// before retiring commands. Receipt confirmation never reads that deleted anchor.
type ProjectTerminalCleanupDomain interface {
	pc.ProjectTerminalCleanupDomain
	RestoreProjectTerminalAnchor(context.Context, pc.ProjectTerminalCleanupRequest, i.AgentID, f.Version, AgentCommandID, f.Digest, *f.Instant) (ProjectRetirementPlan, error)
	ProjectTerminalAnchorLocks(ProjectRetirementPlan) ([]f.LockRequest, error)
	RequireProjectTerminalAnchorRetiredInTx(context.Context, f.Tx, ProjectRetirementPlan) error
}
