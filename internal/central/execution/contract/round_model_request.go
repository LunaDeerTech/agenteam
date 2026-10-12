package contract

import (
	"context"

	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

// RoundModelRequests persists one request for an already committed successor
// input. It neither starts a Model call nor creates a runtime authority. A
// candidate codec value alone proves neither persistence nor permission.
type RoundModelRequests interface {
	Prepare(context.Context, i.ExecutionID, RoundID) (NextRoundModelRequest, error)
	Lookup(context.Context, i.ExecutionID, RoundID) (NextRoundModelRequest, bool, error)
	Stop()
	Drain(context.Context) error
	Joined() bool
}
