package execution

import (
	"bytes"
	"context"

	c "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
)

// WithCommittedBuiltinToolCallInTx observes immutable input for the Runtime's
// original accepted-operation lookup. It lends no current-configuration
// witness and cannot authorize a new Task/Artifact mutation or dispatch.
func (d *ToolCallDriver) WithCommittedBuiltinToolCallInTx(ctx context.Context, tx f.Tx, b tc.ToolCallBinding, plan tc.ToolCallPlan, consume func(context.Context, tc.ToolCallBinding, []byte) error) error {
	if d == nil || d.driver == nil || ctx == nil || consume == nil || b.Validate() != nil || b.RunnerBinding != (tc.RunnerBinding{}) || !tc.WorkArtifactBuiltinBinding(b.Binding) {
		return invalid()
	}
	p, ok := plan.(*toolCallPlan)
	if !ok || p == nil || p.owner != d || !sameToolCallBinding(p.binding, b) {
		return fault(f.Forbidden)
	}
	s := d.driver.state
	if s.multi == nil {
		return fault(f.Forbidden)
	}
	x, err := s.store.InTx(tx)
	if err != nil {
		return portError(err)
	}
	if err = s.store.RequireHeldLocks(ctx, tx, p.RequiredLocks()); err != nil {
		return portError(err)
	}
	if err = s.multi.guard.RequireCurrentProcessInTx(ctx, tx, s.multiProcess()); err != nil {
		return portError(err)
	}
	row, err := loadExecution(ctx, x, b.ExecutionID)
	if err != nil {
		return err
	}
	stored, err := loadDirectTextRound(ctx, x, b.ExecutionID, b.RoundID)
	if err != nil {
		return err
	}
	if stored != nil && stored.toolPending != nil {
		defer clear(stored.toolPending.original)
		defer clear(stored.toolPending.canonical)
	}
	if row == nil || stored == nil || stored.toolPending == nil || row.summary.ProjectID != b.ProjectID || row.summary.AgentID != b.AgentID || row.summary.SnapshotID == nil || *row.summary.SnapshotID != stored.snapshot.Fields().ID || row.summary.Version < stored.toolPending.version || stored.snapshot.Digest() != p.snapshot || stored.round.Digest() != p.round {
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
	raw := bytes.Clone(stored.toolPending.original)
	defer clear(raw)
	if err = consume(ctx, expected, raw); err != nil {
		return err
	}
	return ctx.Err()
}

var _ tc.CommittedBuiltinInputAuthority = (*ToolCallDriver)(nil)
