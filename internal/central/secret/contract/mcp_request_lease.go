package contract

import (
	"context"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
)

// MCPRequestLeaseObservation reads one original acquire/release plan. It grants
// no lease mutation and cannot observe an Execution or another purpose's lease.
type MCPRequestLeaseObservation interface {
	ObserveMCPRequestLeaseInTx(context.Context, f.Tx, UsageRequest, UsageDependencies) (MCPRequestLeaseState, error)
}

// MCPRequestLeaseObservationPlanner prepares the original Project MCP request's
// release observation even when acquisition never reached Secret. A plan is
// not an absence fact or a release grant: the actual transactional observer
// still validates the private owner, complete locks and original lease tuple.
type MCPRequestLeaseObservationPlanner interface {
	DiscoverMCPRequestLeaseObservation(context.Context, UsageRequest) (UsageDependencies, error)
}

type MCPRequestLeaseState struct{ Exists, Released bool }
