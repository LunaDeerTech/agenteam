package contract

import (
	"context"
	"fmt"
	"io"
	"log/slog"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	p "github.com/LunaDeerTech/agenteam/internal/runnerprotocol"
)

// WriteFileResultBinding is a safe identity projection of one already durable
// known-publication receipt. It contains no path, write data or dispatch grant.
// Outcome is the original terminal value "success", not the Operation phase.
// The original payload preserves durable=false and any late completion_code;
// those facts do not turn known publication into an Unknown result.
type WriteFileResultBinding struct {
	Call           ToolCallBinding
	OperationID    OperationID
	AttemptID      AttemptID
	RequestID      p.ID
	InputDigest    f.Digest
	Fingerprint    f.Digest
	TerminalDigest f.Digest
	Outcome        string
	Code           string
	Attempted      bool
	ResultDigest   f.Digest
	ResultBytes    f.Progress
}

func (v WriteFileResultBinding) Validate() error {
	if v.Call.Validate() != nil || v.Call.Binding != (BuiltinBinding{}) || v.Call.RunnerBinding != WriteFileRegistration().Binding ||
		v.OperationID.Validate() != nil || v.AttemptID.Validate() != nil || !v.RequestID.Valid() ||
		v.InputDigest.Validate() != nil || v.Fingerprint.Validate() != nil || v.TerminalDigest.Validate() != nil ||
		v.Outcome != "success" || v.Code != "" || !v.Attempted || v.ResultDigest.Validate() != nil ||
		v.ResultBytes < 2 || v.ResultBytes > 32768 {
		return f.NewFault(f.InvalidArgument, f.NotStarted)
	}
	return nil
}

func (WriteFileResultBinding) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "tool_write_file_result_binding")
}
func (WriteFileResultBinding) LogValue() slog.Value {
	return slog.StringValue("tool_write_file_result_binding")
}
func (WriteFileResultBinding) MarshalJSON() ([]byte, error) {
	return []byte(`"tool_write_file_result_binding"`), nil
}

// A plan is owned by the actual Tool reader. RequiredLocks includes the full
// committed-call historical union and original Operation lock. Binding is discovery
// metadata only; copying or fabricating it cannot construct an accepted plan.
type WriteFileResultPlan interface {
	RequiredLocks() []f.LockRequest
	Binding() WriteFileResultBinding
}

// The reader accepts no caller payload. With uses only the caller's same-Store
// Tx and already-held complete locks. Inside the independent committed-call
// loan it rechecks original protected args, Operation, retired Attempt/RID,
// terminal bytes and the exact durable receipt reference and payload. Current
// dispatch grants, an executor active map and a ProcessGuard are not consulted.
//
// consume receives the exact canonical runnerprotocol write-file receipt JSON
// once, synchronously. That slice and decoded private write bytes are cleared
// before return. The consumer may persist a validated projection in this Tx,
// but must not retain the slice, add locks/transactions or invoke Model/Runner.
// Only known committed publication is supported; failed, cancelled, missing or
// Unknown receipts are rejected. The outer owner must await the actual original
// transaction return before publishing; this borrowed result is not a grant.
type WriteFileResultAuthority interface {
	DiscoverWriteFileResult(context.Context, i.ExecutionID) (WriteFileResultPlan, error)
	WithWriteFileResultInTx(context.Context, f.Tx, WriteFileResultPlan, func(context.Context, WriteFileResultBinding, []byte) error) error
}
