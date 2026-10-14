package contract

import (
	"context"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
)

// WorkArtifactBuiltinBinding is the closed set of ordinary Builtin backends
// composed for the Task collaboration path. It checks metadata only; dispatch
// still requires the original Execution's private plan and protected input.
func WorkArtifactBuiltinBinding(v BuiltinBinding) bool {
	if v.ContractRevision != 1 {
		return false
	}
	switch v.HandlerID {
	case "task.read", "task.transfer", "artifact.list", "artifact.create", "artifact.read":
		return true
	default:
		return false
	}
}

// BuiltinInputAuthority lends the exact committed call's protected arguments
// while the original Execution current-authority witness is alive. No caller
// may replace these bytes with model arguments received by another route.
// The callback runs synchronously in the caller's same-Store transaction after
// its complete lock union has been acquired; it cannot start external I/O.
type BuiltinInputAuthority interface {
	OperationExecutionAuthority
	WithBuiltinToolCallInTx(context.Context, f.Tx, ToolCallBinding, ToolCallPlan, func(context.Context, ToolCallBinding, []byte) error) error
}

// CommittedBuiltinInputAuthority lends only an already accepted call's frozen
// input for canonical result/receipt lookup. It grants no current execution or
// domain mutation authority, including after cancellation or terminalization.
// Runtime must bind it to the original persisted Operation and Attempt and
// restrict the private domain context to lookup before using this port.
type CommittedBuiltinInputAuthority interface {
	WithCommittedBuiltinToolCallInTx(context.Context, f.Tx, ToolCallBinding, ToolCallPlan, func(context.Context, ToolCallBinding, []byte) error) error
}

func validateBuiltinResult(v RunnerResultBinding) error {
	if v.Call.Validate() != nil || v.Call.RunnerBinding != (RunnerBinding{}) || !WorkArtifactBuiltinBinding(v.Call.Binding) ||
		v.OperationID.Validate() != nil || v.AttemptID.Validate() != nil || !v.RequestID.Valid() ||
		v.InputDigest.Validate() != nil || v.Fingerprint.Validate() != nil || v.TerminalDigest.Validate() != nil ||
		!v.Attempted || v.ResultDigest.Validate() != nil || v.ResultBytes < 2 || v.ResultBytes > 1<<20 {
		return operationInvalid()
	}
	if v.Outcome == "success" && v.Code == "" || v.Outcome == "failure" && f.Code(v.Code).Known() {
		return nil
	}
	return operationInvalid()
}
