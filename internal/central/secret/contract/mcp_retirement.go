package contract

import (
	"context"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
)

// MCPLeaseRetirementObservation only reads the original Execution-owned MCP
// lease under its ReleaseLeaseUsage plan and current private domain authority.
type MCPLeaseRetirementObservation interface {
	MCPExecutionLeaseReleasedInTx(context.Context, f.Tx, UsageRequest, UsageDependencies) (bool, error)
}
