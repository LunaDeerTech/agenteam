package contract

import (
	"context"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

const (
	TaskToolRead           = "read"
	TaskToolTransfer       = "transfer"
	TaskToolLookupTransfer = "lookup-transfer"
)

// AgentTaskAuthority validates the original live Tool operation, its captured
// policy and current Execution. An Actor or a persisted DTO is not a permit.
// Discovery returns the full locks needed for the same-transaction recheck.
type AgentTaskAuthority interface {
	DiscoverTaskTool(context.Context, i.Actor, ProjectID, string, string) ([]f.LockRequest, error)
	RequireTaskToolInTx(context.Context, f.Tx, i.Actor, ProjectID, string, string) error
}
