package contract

import (
	"context"
	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
)

// MCPAccessAuditProof verifies a denied request of the original live MCP
// process call. It cannot allow outbound traffic or authorize a new request.
type MCPAccessAuditProof interface {
	RequireMCPAccessAuditInTx(context.Context, f.Tx, ac.Entry, ac.AppendKey) error
}
