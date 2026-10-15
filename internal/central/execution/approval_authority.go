package execution

import (
	"context"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	gc "github.com/LunaDeerTech/agenteam/internal/central/governance/approval/contract"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
)

func (d *ToolCallDriver) ApprovalLocks(ctx context.Context, b tc.ToolCallBinding) ([]f.LockRequest, error) {
	if d == nil || d.driver == nil || d.driver.state.multi == nil || (b.RunnerBinding != tc.RunCommandRegistration().Binding && b.MCPBinding.Validate() != nil) {
		return nil, fault(f.Forbidden)
	}
	p, err := d.DiscoverToolCall(ctx, b)
	if err != nil {
		return nil, err
	}
	return p.RequiredLocks(), nil
}

func (d *ToolCallDriver) RequireApprovalExecutionInTx(ctx context.Context, tx f.Tx, b tc.ToolCallBinding, reference string) error {
	if d == nil || d.driver == nil || d.driver.state.multi == nil || (b.RunnerBinding != tc.RunCommandRegistration().Binding && b.MCPBinding.Validate() != nil) {
		return fault(f.Forbidden)
	}
	x, err := d.driver.state.store.InTx(tx)
	if err != nil {
		return portError(err)
	}
	p, err := d.discoverToolCall(ctx, x, b)
	if err != nil {
		return err
	}
	if reference != "" {
		if _, err = f.ParseID[gc.RequestIdentity](reference); err != nil {
			return invalid()
		}
	}
	return d.withToolCallStateInTx(ctx, tx, b, p, reference, nil)
}

var _ gc.ExecutionAuthority = (*ToolCallDriver)(nil)
