package work

import (
	"context"

	ac "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
)

// ProjectAgentRetirement confirms Work's boundary inside the Agent owner's
// original Project Delete transaction. It does not unassign or rewrite Tasks:
// their fields and immutable history survive as historical identities after
// the irreversible Project gate has closed future use. The Human unassigned
// predicate remains separate and continues to include every Task state.
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
		return fault(f.InvalidArgument)
	}
	if err := change.Validate(); err != nil {
		return portError(err)
	}
	if err := ctx.Err(); err != nil {
		return canceled(err)
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
		return canceled(err)
	}
	// Do not hide malformed cross-Project references, or filter terminal Tasks
	// out of this predicate. Own-Project references are retained, not deleted.
	var foreign bool
	err = x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_work.tasks WHERE assignee_agent_id=$1 AND project_id<>$2)`, change.Request.AgentID.String(), change.Request.ProjectID.String()).Scan(&foreign)
	if err != nil {
		return taskSQL(err)
	}
	if err = ctx.Err(); err != nil {
		return canceled(err)
	}
	if foreign {
		return fault(f.ResourceBusy)
	}
	return nil
}

var _ ac.ProjectRetirementGate = (*ProjectAgentRetirement)(nil)
