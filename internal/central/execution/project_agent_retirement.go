package execution

import (
	"context"

	ac "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
)

// ProjectAgentRetirement supports only an Agent with no Execution history.
// An empty active slot, terminal status, or local executor report is not an
// absence proof. Any original Execution identity keeps retirement blocked.
type ProjectAgentRetirement struct {
	store Store
	owner ac.ProjectRetirementOwner
}

func NewProjectAgentRetirement(store Store, owner ac.ProjectRetirementOwner) (*ProjectAgentRetirement, error) {
	if nilPort(store) || nilPort(owner) {
		return nil, fault(f.DependencyUnbound)
	}
	return &ProjectAgentRetirement{store: store, owner: owner}, nil
}

func (p *ProjectAgentRetirement) RequireProjectAgentRetirementInTx(ctx context.Context, tx f.Tx, change ac.ProjectRetirementChange) error {
	if p == nil || nilPort(p.store) || nilPort(p.owner) {
		return fault(f.DependencyUnbound)
	}
	if ctx == nil || !tx.Valid() {
		return invalid()
	}
	if err := change.Validate(); err != nil {
		return portError(err)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	change = change.Clone()
	x, err := p.store.InTx(tx)
	if err != nil {
		return portError(err)
	}
	if nilPort(x) {
		return fault(f.DependencyUnbound)
	}
	if err = p.store.RequireHeldLocks(ctx, tx, change.Request.RequiredLocks()); err != nil {
		return portError(err)
	}
	if err = p.owner.RequireProjectRetirementPreparedInTx(ctx, tx, change); err != nil {
		return portError(err)
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	// Agent IDs are global identities. Do not restrict to active states or hide
	// an inconsistent row under another Project by filtering on ProjectID.
	var present bool
	err = x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_execution.executions WHERE agent_id=$1)`, change.Request.AgentID.String()).Scan(&present)
	if err != nil {
		return portError(err)
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	if present {
		return fault(f.ResourceBusy)
	}
	return nil
}

var _ ac.ProjectRetirementGate = (*ProjectAgentRetirement)(nil)
