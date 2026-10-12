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

// RunCommandResultBinding identifies one complete persisted command report and
// both streams. ResultDigest/Bytes bind the canonical {report,stdout,stderr}
// bundle, while TerminalDigest binds the original three separate stored refs.
// It contains neither output nor a dispatch grant. A completed invocation may
// be a known failure/cancellation; only the shared classifier decides success.
type RunCommandResultBinding struct {
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

func (v RunCommandResultBinding) Validate() error {
	if v.Call.Validate() != nil || v.Call.Binding != (BuiltinBinding{}) || v.Call.RunnerBinding != RunCommandRegistration().Binding ||
		v.OperationID.Validate() != nil || v.AttemptID.Validate() != nil || !v.RequestID.Valid() ||
		v.InputDigest.Validate() != nil || v.Fingerprint.Validate() != nil || v.TerminalDigest.Validate() != nil ||
		!ValidRunCommandResultClassification(v.Outcome, v.Code) || !v.Attempted || v.ResultDigest.Validate() != nil ||
		v.ResultBytes < 2 || v.ResultBytes > MaxRunCommandResultBundleBytes {
		return f.NewFault(f.InvalidArgument, f.NotStarted)
	}
	return nil
}

func (RunCommandResultBinding) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "tool_run_command_result_binding")
}
func (RunCommandResultBinding) LogValue() slog.Value {
	return slog.StringValue("tool_run_command_result_binding")
}
func (RunCommandResultBinding) MarshalJSON() ([]byte, error) {
	return []byte(`"tool_run_command_result_binding"`), nil
}

// A plan is owned by the actual Tool reader. RequiredLocks includes the full
// committed-call historical union and original Operation lock. Binding is discovery
// metadata only; copying or fabricating it cannot construct an accepted plan.
type RunCommandResultPlan interface {
	RequiredLocks() []f.LockRequest
	Binding() RunCommandResultBinding
}

// The independent historical reader takes no caller payload or current grants.
// Inside the original committed-call loan, under the same Store/Tx/full locks,
// it verifies Operation, retired Attempt/RID, terminal, report and both streams.
// Unknown, missing reports, partial bytes and mismatched classification fail.
// The synchronous callback receives the complete canonical bundle once. Its
// bytes are cleared before return; no nested Tx, late locks or external I/O is
// permitted. Publication waits for the original outer transaction's return.
type RunCommandResultAuthority interface {
	DiscoverRunCommandResult(context.Context, i.ExecutionID) (RunCommandResultPlan, error)
	WithRunCommandResultInTx(context.Context, f.Tx, RunCommandResultPlan, func(context.Context, RunCommandResultBinding, []byte) error) error
}
