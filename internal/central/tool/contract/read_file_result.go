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

// ReadFileResultBinding is a safe identity projection of one already durable
// successful result. It contains no file path, file content or dispatch grant.
// Outcome is the original terminal value "success", not the Operation phase.
type ReadFileResultBinding struct {
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

func (v ReadFileResultBinding) Validate() error {
	if v.Call.Validate() != nil || v.Call.Binding != (BuiltinBinding{}) || v.Call.RunnerBinding != ReadFileRegistration().Binding ||
		v.OperationID.Validate() != nil || v.AttemptID.Validate() != nil || !v.RequestID.Valid() ||
		v.InputDigest.Validate() != nil || v.Fingerprint.Validate() != nil || v.TerminalDigest.Validate() != nil ||
		v.Outcome != "success" || v.Code != "" || !v.Attempted || v.ResultDigest.Validate() != nil ||
		v.ResultBytes < 2 || v.ResultBytes > p.MaxReadFileResultBytes {
		return f.NewFault(f.InvalidArgument, f.NotStarted)
	}
	return nil
}

func (ReadFileResultBinding) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "tool_read_file_result_binding")
}
func (ReadFileResultBinding) LogValue() slog.Value {
	return slog.StringValue("tool_read_file_result_binding")
}
func (ReadFileResultBinding) MarshalJSON() ([]byte, error) {
	return []byte(`"tool_read_file_result_binding"`), nil
}

// A plan is owned by the actual Tool reader. RequiredLocks includes the full
// current Execution union and original Operation lock. Binding is discovery
// metadata only; copying or fabricating it cannot construct an accepted plan.
type ReadFileResultPlan interface {
	RequiredLocks() []f.LockRequest
	Binding() ReadFileResultBinding
}

// The result reader accepts no caller payload. With uses only the caller's
// same-Store transaction and already-held complete lock set. It rechecks the
// original protected arguments, current Execution permission, Operation,
// retired Attempt, terminal metadata and complete result reference/bytes.
//
// consume receives the canonical runnerprotocol read-file result JSON exactly
// once, synchronously while the current witness remains live. The slice is
// cleared before return. The consumer may persist a validated projection in
// this transaction, but must not retain the borrowed slice, acquire locks,
// begin another transaction, or invoke Model/Runner I/O. Only durable success
// is supported: incomplete, failed, cancelled and Unknown results are rejected.
type ReadFileResultAuthority interface {
	DiscoverReadFileResult(context.Context, i.ExecutionID) (ReadFileResultPlan, error)
	WithReadFileResultInTx(context.Context, f.Tx, ReadFileResultPlan, func(context.Context, ReadFileResultBinding, []byte) error) error
}
