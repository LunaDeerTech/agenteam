package contract

import (
	"context"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

// MCPRuntimeProof is implemented by the fixed MCP domain authority. Project
// proves its current gate; MCP proves its original process/claim in the same
// transaction. Neither a service registration nor this tuple is authority.
type MCPRuntimeProof interface {
	RequireMCPRuntimeInTx(context.Context, f.Tx, id.Actor, id.ProjectID) error
}

type MCPRuntimeProjectAuthority interface {
	RequireMCPRuntimeProjectInTx(context.Context, f.Tx, id.Actor, id.ProjectID) error
}
