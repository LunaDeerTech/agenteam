package execution

import (
	"bytes"
	"context"

	c "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
)

// A historical call plan cannot be used by the current dispatch authority.
// Its locks protect the original facts; holding them does not renew a grant.
type committedRunCommandCallPlan struct{ call *toolCallPlan }

func (p *committedRunCommandCallPlan) RequiredLocks() []f.LockRequest {
	if p == nil || p.call == nil {
		return nil
	}
	return p.call.RequiredLocks()
}

func (p *committedRunCommandCallPlan) Binding() tc.ToolCallBinding {
	if p == nil || p.call == nil {
		return tc.ToolCallBinding{}
	}
	return p.call.binding
}

func (d *ToolCallDriver) DiscoverCommittedRunCommandCall(ctx context.Context, e i.ExecutionID) (tc.CommittedRunCommandCallPlan, error) {
	if d == nil || d.driver == nil || d.driver.state == nil || ctx == nil || e.Validate() != nil {
		return nil, invalid()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if d.driver.state.tools.profile() != tc.RunCommandRegistration().Binding {
		return nil, fault(f.Forbidden)
	}
	stored, err := loadDirectText(ctx, d.driver.state.store, e)
	if err != nil {
		return nil, err
	}
	if stored != nil && stored.toolPending != nil {
		defer clear(stored.toolPending.original)
		defer clear(stored.toolPending.canonical)
	}
	binding, err := toolBinding(stored)
	if err != nil {
		return nil, err
	}
	if binding.RunnerBinding != tc.RunCommandRegistration().Binding {
		return nil, fault(f.Forbidden)
	}
	plan, err := d.DiscoverToolCall(ctx, binding)
	if err != nil {
		return nil, err
	}
	return &committedRunCommandCallPlan{call: plan.(*toolCallPlan)}, ctx.Err()
}

// Only immutable committed Execution facts are lent here. Current Tool, Approval/Environment,
// Registry and Mount grants deliberately do not control recovery of a result
// that was already produced. This does not issue an Agent-current witness or
// permission to send another Runner request. The first consumption separately
// checks running/no-cancel and the current Project write gate in this same Tx.
func (d *ToolCallDriver) WithCommittedRunCommandCallInTx(ctx context.Context, tx f.Tx, plan tc.CommittedRunCommandCallPlan, consume func(context.Context, tc.ToolCallBinding, []byte) error) error {
	if d == nil || d.driver == nil || d.driver.state == nil || ctx == nil || consume == nil {
		return invalid()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	p, ok := plan.(*committedRunCommandCallPlan)
	if !ok || p == nil || p.call == nil || p.call.owner != d || p.call.binding.RunnerBinding != tc.RunCommandRegistration().Binding || d.driver.state.tools.profile() != tc.RunCommandRegistration().Binding {
		return fault(f.Forbidden)
	}
	s, b := d.driver.state, p.call.binding
	x, err := s.store.InTx(tx)
	if err != nil {
		return portError(err)
	}
	if err = s.store.RequireHeldLocks(ctx, tx, p.RequiredLocks()); err != nil {
		return portError(err)
	}
	row, err := loadExecution(ctx, x, b.ExecutionID)
	if err != nil {
		return err
	}
	stored, err := loadDirectText(ctx, x, b.ExecutionID)
	if err != nil {
		return err
	}
	if stored != nil && stored.toolPending != nil {
		defer clear(stored.toolPending.original)
		defer clear(stored.toolPending.canonical)
	}
	if row == nil || stored == nil || stored.toolPending == nil || row.summary.ID != b.ExecutionID || row.summary.ProjectID != b.ProjectID || row.summary.AgentID != b.AgentID || row.summary.SnapshotID == nil || *row.summary.SnapshotID != stored.snapshot.Fields().ID || row.summary.Version < stored.toolPending.version || stored.snapshot.Digest() != p.call.snapshot || stored.round.Digest() != p.call.round {
		return fault(f.ConfirmationStale)
	}
	expected, err := toolBinding(stored)
	if err != nil || !sameToolCallBinding(expected, b) {
		return fault(f.Forbidden)
	}
	input, err := loadPreparationInput(ctx, x, b.ExecutionID)
	if err != nil {
		return err
	}
	if input == nil || input.input.Digest() != stored.snapshot.Fields().Context.InputDigest() || !input.input.Fields().Request.Equal(c.PreparationRequest{ExecutionID: row.summary.ID, Launch: row.launch}) {
		return fault(f.ConfirmationStale)
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	borrowed := bytes.Clone(stored.toolPending.original)
	defer clear(borrowed)
	if err = consume(ctx, expected, borrowed); err != nil {
		return err
	}
	return ctx.Err()
}

var _ tc.CommittedRunCommandCallAuthority = (*ToolCallDriver)(nil)
