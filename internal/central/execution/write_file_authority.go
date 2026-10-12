package execution

import (
	"context"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
)

// NewWriteFileToolCallDriver explicitly captures one write-file Model call.
// It shares the original persistence/lifecycle, but never enables the read or
// no-tools constructor to run writes. No ToolOperation or wire is started here.
func NewWriteFileToolCallDriver(store Store, a *Authority, deps ToolCallDependencies) (*ToolCallDriver, error) {
	return newProfileToolCallDriver(store, a, deps, tc.WriteFileRegistration().Binding)
}

func (d *toolCallDependencies) profile() tc.RunnerBinding {
	// The zero private profile retains the original read-only meaning. Public
	// constructors always select an explicit profile and verify their Loop.
	if d == nil || d.binding == (tc.RunnerBinding{}) {
		return tc.ReadFileRegistration().Binding
	}
	return d.binding
}

// Select only from the original captured binding. Parsing a write here needs
// its Mount identity alone, so decoded file bytes are cleared before return.
func toolCallMountID(ctx context.Context, binding tc.RunnerBinding, raw []byte) (i.MountID, error) {
	switch binding {
	case tc.ReadFileRegistration().Binding:
		args, err := tc.DecodeReadFileToolArguments(ctx, raw)
		return args.MountID, err
	case tc.WriteFileRegistration().Binding:
		args, err := tc.DecodeWriteFileToolArguments(ctx, raw)
		clear(args.Arguments.Data)
		return args.MountID, err
	case tc.RunCommandRegistration().Binding:
		args, err := tc.DecodeRunCommandToolArguments(ctx, raw)
		return args.MountID, err
	default:
		return i.MountID{}, fault(f.CapabilityUnsupported)
	}
}

type writeFileCallPlan struct{ call *toolCallPlan }

func (p *writeFileCallPlan) RequiredLocks() []f.LockRequest {
	if p == nil || p.call == nil {
		return nil
	}
	return p.call.RequiredLocks()
}
func (p *writeFileCallPlan) Binding() tc.ToolCallBinding {
	if p == nil || p.call == nil {
		return tc.ToolCallBinding{}
	}
	return p.call.binding
}

func (d *ToolCallDriver) DiscoverWriteFileCall(ctx context.Context, e i.ExecutionID) (tc.WriteFileCallPlan, error) {
	if d == nil || d.driver == nil || ctx == nil || e.Validate() != nil {
		return nil, invalid()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if d.driver.state.tools.profile() != tc.WriteFileRegistration().Binding {
		return nil, fault(f.Forbidden)
	}
	stored, err := loadDirectText(ctx, d.driver.state.store, e)
	if err != nil {
		return nil, err
	}
	binding, err := toolBinding(stored)
	if err != nil {
		return nil, err
	}
	if binding.RunnerBinding != tc.WriteFileRegistration().Binding {
		return nil, fault(f.Forbidden)
	}
	plan, err := d.DiscoverToolCall(ctx, binding)
	if err != nil {
		return nil, err
	}
	return &writeFileCallPlan{call: plan.(*toolCallPlan)}, nil
}

func (d *ToolCallDriver) WithWriteFileCallInTx(ctx context.Context, tx f.Tx, plan tc.WriteFileCallPlan, consume func(context.Context, tc.ToolCallBinding, []byte) error) error {
	if d == nil || d.driver == nil || ctx == nil || consume == nil {
		return invalid()
	}
	p, ok := plan.(*writeFileCallPlan)
	if !ok || p == nil || p.call == nil || p.call.owner != d || p.call.binding.RunnerBinding != tc.WriteFileRegistration().Binding {
		return fault(f.Forbidden)
	}
	return d.withToolCallInTx(ctx, tx, p.call.binding, p.call, consume)
}

var _ tc.WriteFileExecutionAuthority = (*ToolCallDriver)(nil)
