package contract

import (
	"context"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
)

// ApprovalOperation is an immutable Tool-owned operation identity. Its public
// shape is not authority: the owner must verify the original row in the same Tx.
type ApprovalOperation struct {
	Binding     ToolCallBinding
	OperationID OperationID
	Fingerprint f.Digest
}

// ApprovalToolName is derived only from the captured immutable backend tuple.
// Remote display text cannot impersonate the existing run-command operation.
func ApprovalToolName(b ToolCallBinding) string {
	if b.Validate() != nil {
		return ""
	}
	if b.MCPBinding.Validate() == nil {
		return "mcp:" + b.MCPBinding.ConfigID + ":" + b.MCPBinding.RemoteToolName
	}
	if b.RunnerBinding == RunCommandRegistration().Binding {
		return "run-command"
	}
	return ""
}

func (v ApprovalOperation) Validate() error {
	if v.Binding.Validate() != nil || v.OperationID.Validate() != nil || v.Fingerprint.Validate() != nil || (v.Binding.RunnerBinding != RunCommandRegistration().Binding && v.Binding.MCPBinding.Validate() != nil) {
		return operationInvalid()
	}
	return nil
}
func (v ApprovalOperation) RequiredLocks() []f.LockRequest {
	if v.Validate() != nil {
		return nil
	}
	e, _ := f.AggregateLock(f.ExecutionAggregate, v.Binding.ExecutionID.String())
	o, _ := f.AggregateLock(f.OperationAggregate, v.OperationID.String())
	return []f.LockRequest{{Key: e, Mode: f.Exclusive}, {Key: o, Mode: f.Exclusive}}
}

// Reserve performs current authorization and persists only created metadata. It
// never creates an Attempt, dispatches a Runner request or grants permission.
// Require verifies the exact immutable binding and fingerprint under the full
// held lock set; terminal/unknown operations cannot be approved or replayed.
type ApprovalOperations interface {
	ReserveApprovalOperation(context.Context, ToolCallBinding) (ApprovalOperation, error)
	RequireApprovalOperationInTx(context.Context, f.Tx, ApprovalOperation) error
	RequireUnattemptedApprovalOperationInTx(context.Context, f.Tx, ApprovalOperation) error
	RejectApprovalOperationInTx(context.Context, f.Tx, ApprovalOperation, string) error
	RequireRejectedOperationInTx(context.Context, f.Tx, ApprovalOperation, string) error
}
