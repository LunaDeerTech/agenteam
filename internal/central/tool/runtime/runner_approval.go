package runtime

import (
	"context"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
)

func (e *RunnerExecutor) approvalCall(ctx context.Context, b tc.ToolCallBinding) (context.Context, error) {
	ctx, err := exactRunnerBinding(ctx, b)
	if err != nil {
		return nil, err
	}
	if e == nil || e.command == nil || b.RunnerBinding.OperationName != "run-command" {
		return nil, fail(f.DependencyUnbound)
	}
	return ctx, nil
}
func (e *RunnerExecutor) ReserveApprovalOperation(ctx context.Context, b tc.ToolCallBinding) (tc.ApprovalOperation, error) {
	ctx, err := e.approvalCall(ctx, b)
	if err != nil {
		return tc.ApprovalOperation{}, err
	}
	return e.command.ReserveApprovalOperation(ctx, b)
}
func (e *RunnerExecutor) RequireApprovalOperationInTx(ctx context.Context, tx f.Tx, op tc.ApprovalOperation) error {
	ctx, err := e.approvalCall(ctx, op.Binding)
	if err != nil {
		return err
	}
	return e.command.RequireApprovalOperationInTx(ctx, tx, op)
}
func (e *RunnerExecutor) RequireUnattemptedApprovalOperationInTx(ctx context.Context, tx f.Tx, op tc.ApprovalOperation) error {
	ctx, err := e.approvalCall(ctx, op.Binding)
	if err != nil {
		return err
	}
	return e.command.RequireUnattemptedApprovalOperationInTx(ctx, tx, op)
}
func (e *RunnerExecutor) RejectApprovalOperationInTx(ctx context.Context, tx f.Tx, op tc.ApprovalOperation, requestID string) error {
	ctx, err := e.approvalCall(ctx, op.Binding)
	if err != nil {
		return err
	}
	return e.command.RejectApprovalOperationInTx(ctx, tx, op, requestID)
}
func (e *RunnerExecutor) RequireRejectedOperationInTx(ctx context.Context, tx f.Tx, op tc.ApprovalOperation, requestID string) error {
	ctx, err := e.approvalCall(ctx, op.Binding)
	if err != nil {
		return err
	}
	return e.command.RequireRejectedOperationInTx(ctx, tx, op, requestID)
}
func (e *CompositeExecutor) ReserveApprovalOperation(ctx context.Context, b tc.ToolCallBinding) (tc.ApprovalOperation, error) {
	if e == nil {
		return tc.ApprovalOperation{}, fail(f.DependencyUnbound)
	}
	return e.runners.ReserveApprovalOperation(ctx, b)
}
func (e *CompositeExecutor) RequireApprovalOperationInTx(ctx context.Context, tx f.Tx, op tc.ApprovalOperation) error {
	if e == nil {
		return fail(f.DependencyUnbound)
	}
	return e.runners.RequireApprovalOperationInTx(ctx, tx, op)
}
func (e *CompositeExecutor) RequireUnattemptedApprovalOperationInTx(ctx context.Context, tx f.Tx, op tc.ApprovalOperation) error {
	if e == nil {
		return fail(f.DependencyUnbound)
	}
	return e.runners.RequireUnattemptedApprovalOperationInTx(ctx, tx, op)
}
func (e *CompositeExecutor) RejectApprovalOperationInTx(ctx context.Context, tx f.Tx, op tc.ApprovalOperation, requestID string) error {
	if e == nil {
		return fail(f.DependencyUnbound)
	}
	return e.runners.RejectApprovalOperationInTx(ctx, tx, op, requestID)
}
func (e *CompositeExecutor) RequireRejectedOperationInTx(ctx context.Context, tx f.Tx, op tc.ApprovalOperation, requestID string) error {
	if e == nil {
		return fail(f.DependencyUnbound)
	}
	return e.runners.RequireRejectedOperationInTx(ctx, tx, op, requestID)
}

var _ tc.ApprovalOperations = (*RunnerExecutor)(nil)
var _ tc.ApprovalOperations = (*CompositeExecutor)(nil)
