package contract

import (
	"context"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
)

// Input observation can reserve a zero-attempt approval operation. Dispatch
// additionally requires the original live Execution callback and its current
// Agent policy or exact durable one-operation approval in the same transaction.
type MCPDispatchAuthority interface {
	RequireMCPAdmissionInTx(context.Context, f.Tx, ToolCallBinding, ToolCallPlan) error
}
