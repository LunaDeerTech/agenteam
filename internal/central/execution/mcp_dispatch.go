package execution

import (
	"context"
	ac "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
	"sync/atomic"
)

type mcpCurrentKey struct{}
type mcpCurrentWitness struct {
	driver   *ToolCallDriver
	plan     *toolCallPlan
	tx       f.Tx
	approval ac.ApprovalPolicy
	active   atomic.Bool
}

func (d *ToolCallDriver) mcpWitness(ctx context.Context, tx f.Tx, b tc.ToolCallBinding, p *toolCallPlan) (*mcpCurrentWitness, error) {
	if d == nil || d.driver == nil || ctx == nil || b.Validate() != nil || b.MCPBinding.Validate() != nil {
		return nil, invalid()
	}
	w, _ := ctx.Value(mcpCurrentKey{}).(*mcpCurrentWitness)
	if w == nil || !w.active.Load() || w.driver != d || w.tx != tx || w.plan != p || p == nil || p.owner != d || !sameToolCallBinding(p.binding, b) {
		return nil, fault(f.Forbidden)
	}
	if _, err := d.driver.state.store.InTx(tx); err != nil {
		return nil, portError(err)
	}
	if err := d.driver.state.store.RequireHeldLocks(ctx, tx, p.RequiredLocks()); err != nil {
		return nil, portError(err)
	}
	return w, ctx.Err()
}

func (d *ToolCallDriver) RequireMCPAdmissionInTx(ctx context.Context, tx f.Tx, b tc.ToolCallBinding, plan tc.ToolCallPlan) error {
	p, ok := plan.(*toolCallPlan)
	if !ok {
		return fault(f.Forbidden)
	}
	w, err := d.mcpWitness(ctx, tx, b, p)
	if err != nil {
		return err
	}
	if w.approval != ac.ApprovalAllow {
		s := d.driver.state
		if s.multi == nil || nilPort(s.multi.approvals) {
			return fault(f.CapabilityUnsupported)
		}
		if err = s.multi.approvals.RequireApprovedInTx(ctx, tx, b); err != nil {
			return portError(err)
		}
	}
	after, err := d.mcpWitness(ctx, tx, b, p)
	if err != nil {
		return err
	}
	if after != w {
		return fault(f.Forbidden)
	}
	return ctx.Err()
}

var _ tc.MCPDispatchAuthority = (*ToolCallDriver)(nil)
