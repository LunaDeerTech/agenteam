package execution

import (
	"context"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
)

// This private wrapper carries discovery identity, never protected arguments
// or a live witness. A caller-provided binding cannot construct an authority.
type readFileCallPlan struct {
	call *toolCallPlan
}

func (p *readFileCallPlan) RequiredLocks() []f.LockRequest {
	if p == nil || p.call == nil {
		return nil
	}
	return p.call.RequiredLocks()
}

func (p *readFileCallPlan) Binding() tc.ToolCallBinding {
	if p == nil || p.call == nil {
		return tc.ToolCallBinding{}
	}
	return p.call.binding
}

// DiscoverReadFileCall derives the call from Execution's committed facts. It
// does not read caller-supplied arguments or authorize an Operation/dispatch.
func (d *ToolCallDriver) DiscoverReadFileCall(ctx context.Context, e i.ExecutionID) (tc.ReadFileCallPlan, error) {
	return d.discoverReadFileCall(ctx, e, nil)
}
func (d *ToolCallDriver) DiscoverReadFileCallFor(ctx context.Context, id tc.ToolCallIdentity) (tc.ReadFileCallPlan, error) {
	if id.Validate() != nil {
		return nil, invalid()
	}
	return d.discoverReadFileCall(ctx, id.ExecutionID, &id)
}
func (d *ToolCallDriver) discoverReadFileCall(ctx context.Context, e i.ExecutionID, id *tc.ToolCallIdentity) (tc.ReadFileCallPlan, error) {
	if d == nil || d.driver == nil || ctx == nil || e.Validate() != nil {
		return nil, invalid()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	round := ""
	if id != nil {
		round = id.RoundID
	}
	stored, err := loadDirectTextRound(ctx, d.driver.state.store, e, round)
	if err != nil {
		return nil, err
	}
	binding, err := toolBinding(stored)
	if err != nil {
		return nil, err
	}
	if id != nil && !id.Matches(binding) {
		return nil, fault(f.Forbidden)
	}
	if binding.RunnerBinding != tc.ReadFileRegistration().Binding {
		return nil, fault(f.Forbidden)
	}
	plan, err := d.DiscoverToolCall(ctx, binding)
	if err != nil {
		return nil, err
	}
	return &readFileCallPlan{call: plan.(*toolCallPlan)}, nil
}

// WithReadFileCallInTx lends the verified original arguments exactly once in
// the caller's already-locked transaction. The current-Agent witness and byte
// loan both expire before return, including on callback failure/cancellation.
func (d *ToolCallDriver) WithReadFileCallInTx(ctx context.Context, tx f.Tx, plan tc.ReadFileCallPlan, consume func(context.Context, tc.ToolCallBinding, []byte) error) error {
	if d == nil || d.driver == nil || ctx == nil || consume == nil {
		return invalid()
	}
	p, ok := plan.(*readFileCallPlan)
	if !ok || p == nil || p.call == nil || p.call.owner != d || p.call.binding.RunnerBinding != tc.ReadFileRegistration().Binding {
		return fault(f.Forbidden)
	}
	return d.withToolCallInTx(ctx, tx, p.call.binding, p.call, consume)
}

var _ tc.ReadFileExecutionAuthority = (*ToolCallDriver)(nil)
