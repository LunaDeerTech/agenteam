package contract

import (
	"context"

	ac "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
)

// Each entry is the complete original change and independently persisted
// routing receipt, decoded by Skills before removing its command history.
// These public values are expectations, never an authorization witness.
type ProjectAgentIngressRetirementEntry struct {
	Change  RuntimeAssignmentChange
	Receipt AssignmentIngressReceipt
}

func (e ProjectAgentIngressRetirementEntry) Validate() error {
	if e.Change.Validate() != nil || e.Receipt.Validate() != nil ||
		e.Change.ID != e.Receipt.ChangeID || e.Receipt.Queued || e.Receipt.ExecutionID != nil {
		return invalid()
	}
	return nil
}

func (e ProjectAgentIngressRetirementEntry) Clone() ProjectAgentIngressRetirementEntry {
	e.Receipt = e.Receipt.Clone()
	return e
}

// Execution owns these methods. Both require its own same-Store live Tx,
// Project/Schedule/Agent EX and the Agent consumer's original private Applied
// witness. No historical Execution of any state is permitted. Retirement
// compares the complete Agent-wide ingress set with entries sorted by Change
// ID, including the empty set, and removes only exact no-active decisions.
// Skills invokes it before deleting its completed command/change history in
// the same caller transaction. Any failure rolls back both domains. Recovery
// requires the exact Project applied checkpoint before accepting empty rows.
type ProjectAgentIngressRetirement interface {
	RetireProjectAgentIngressInTx(context.Context, f.Tx, ac.ProjectRetirementChange, []ProjectAgentIngressRetirementEntry) error
	RequireProjectAgentIngressRetiredInTx(context.Context, f.Tx, ac.ProjectRetirementChange) error
}
