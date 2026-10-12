package execution

import (
	"context"

	ac "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	sc "github.com/LunaDeerTech/agenteam/internal/central/skill/contract"
)

// ProjectAgentIngressRetirement owns only the historical no-active routing
// decisions. It never retires an Execution, discovers an active slot, or opens
// a transaction. Skills supplies its complete original change/receipt set and
// calls this owner before deleting its history in the same Project transaction.
type ProjectAgentIngressRetirement struct {
	store Store
	owner ac.ProjectRetirementOwner
}

func NewProjectAgentIngressRetirement(store Store, owner ac.ProjectRetirementOwner) (*ProjectAgentIngressRetirement, error) {
	if nilPort(store) || nilPort(owner) {
		return nil, fault(f.DependencyUnbound)
	}
	return &ProjectAgentIngressRetirement{store: store, owner: owner}, nil
}

func (p *ProjectAgentIngressRetirement) original(ctx context.Context, tx f.Tx, change ac.ProjectRetirementChange) (postgres.SQLExecutor, error) {
	if p == nil || nilPort(p.store) || nilPort(p.owner) {
		return nil, fault(f.DependencyUnbound)
	}
	if ctx == nil || !tx.Valid() {
		return nil, invalid()
	}
	if err := change.Validate(); err != nil {
		return nil, portError(err)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	x, err := p.store.InTx(tx)
	if err != nil {
		return nil, portError(err)
	}
	if nilPort(x) {
		return nil, fault(f.DependencyUnbound)
	}
	if err = p.store.RequireHeldLocks(ctx, tx, change.Request.RequiredLocks()); err != nil {
		return nil, portError(err)
	}
	if err = p.owner.RequireProjectRetirementAppliedInTx(ctx, tx, change.Clone()); err != nil {
		return nil, portError(err)
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	return x, nil
}

func (p *ProjectAgentIngressRetirement) RetireProjectAgentIngressInTx(ctx context.Context, tx f.Tx, change ac.ProjectRetirementChange, entries []sc.ProjectAgentIngressRetirementEntry) error {
	change = change.Clone()
	x, err := p.original(ctx, tx, change)
	if err != nil {
		return err
	}
	// These public tuples are expectations, never an issuer. Reject duplicates
	// before comparing the complete owned set, including unexpected foreign-P
	// rows under the same globally unique Agent identity.
	expected := make([]sc.ProjectAgentIngressRetirementEntry, len(entries))
	for n, entry := range entries {
		if err = entry.Validate(); err != nil {
			return portError(err)
		}
		if entry.Change.ProjectID != change.Request.ProjectID || entry.Change.AgentID != change.Request.AgentID || entry.Change.AgentVersion > change.Request.ExpectedVersion {
			return invalid()
		}
		if n > 0 && entries[n-1].Change.ID.String() >= entry.Change.ID.String() {
			return invalid()
		}
		expected[n] = entry.Clone()
	}
	var execution bool
	err = x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_execution.executions WHERE agent_id=$1)`, change.Request.AgentID.String()).Scan(&execution)
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err != nil {
		return portError(err)
	}
	if execution {
		return fault(f.ResourceBusy)
	}
	var count int64
	err = x.QueryRow(ctx, `SELECT count(*) FROM agenteam_execution.skill_assignment_ingress WHERE agent_id=$1`, change.Request.AgentID.String()).Scan(&count)
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err != nil {
		return portError(err)
	}
	if count != int64(len(expected)) {
		return fault(f.ResourceBusy)
	}
	for _, entry := range expected {
		raw, marshalErr := directTextJSON(entry.Change)
		if marshalErr != nil || len(raw) > maxSkillAssignmentIngressBytes {
			return invalid()
		}
		actual, found, readErr := readSkillAssignmentIngress(ctx, x, entry.Change, raw, c.TriggerInputDigest(raw))
		if readErr != nil {
			return readErr
		}
		if !found || actual.ChangeID != entry.Receipt.ChangeID || actual.Queued || actual.ExecutionID != nil {
			return fault(f.ResourceBusy)
		}
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	if count == 0 {
		return nil
	}
	// Agent EX holds the original set stable across all reads and this delete.
	// Matching both set cardinality and every immutable entry prevents deleting
	// an extra, mismatched, queued or foreign Project decision.
	tag, err := x.Exec(ctx, `DELETE FROM agenteam_execution.skill_assignment_ingress WHERE agent_id=$1 AND project_id=$2 AND outcome='no_active_execution' AND execution_id IS NULL`, change.Request.AgentID.String(), change.Request.ProjectID.String())
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err != nil {
		return portError(err)
	}
	if tag.RowsAffected() != count {
		return fault(f.ResourceBusy)
	}
	return nil
}

func (p *ProjectAgentIngressRetirement) RequireProjectAgentIngressRetiredInTx(ctx context.Context, tx f.Tx, change ac.ProjectRetirementChange) error {
	x, err := p.original(ctx, tx, change.Clone())
	if err != nil {
		return err
	}
	var remaining bool
	err = x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_execution.executions WHERE agent_id=$1) OR EXISTS(SELECT 1 FROM agenteam_execution.skill_assignment_ingress WHERE agent_id=$1)`, change.Request.AgentID.String()).Scan(&remaining)
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err != nil {
		return portError(err)
	}
	if remaining {
		return fault(f.ResourceBusy)
	}
	return nil
}

var _ sc.ProjectAgentIngressRetirement = (*ProjectAgentIngressRetirement)(nil)
