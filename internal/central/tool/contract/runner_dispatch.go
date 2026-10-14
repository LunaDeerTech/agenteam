package contract

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"unicode/utf8"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
)

// ToolCallIdentity selects one persisted call. It is not an execution grant;
// the original producer must still issue and revalidate its private plan.
type ToolCallIdentity struct {
	ExecutionID i.ExecutionID
	RoundID     string
	CallID      string
}

func (v ToolCallIdentity) Validate() error {
	if v.ExecutionID.Validate() != nil || len(v.CallID) == 0 || len(v.CallID) > 256 || !utf8.ValidString(v.CallID) || strings.ContainsRune(v.CallID, 0) {
		return operationInvalid()
	}
	if _, err := f.ParseID[struct{}](v.RoundID); err != nil {
		return operationInvalid()
	}
	return nil
}

func (v ToolCallIdentity) Matches(b ToolCallBinding) bool {
	return v.Validate() == nil && v.ExecutionID == b.ExecutionID && v.RoundID == b.RoundID && v.CallID == b.CallID
}

func (v ToolCallBinding) Identity() ToolCallIdentity {
	return ToolCallIdentity{ExecutionID: v.ExecutionID, RoundID: v.RoundID, CallID: v.CallID}
}

func (ToolCallIdentity) Format(w fmt.State, _ rune)   { _, _ = io.WriteString(w, "tool_call_identity") }
func (ToolCallIdentity) MarshalJSON() ([]byte, error) { return []byte(`"tool_call_identity"`), nil }
func (ToolCallIdentity) LogValue() slog.Value         { return slog.StringValue("tool_call_identity") }

// These extensions preserve the original single-call API while allowing exact
// multi-round discovery. Missing extensions fail closed; callers must never
// retry a first historical call as a substitute for the selected call.
type ReadFileCallDiscovery interface {
	DiscoverReadFileCallFor(context.Context, ToolCallIdentity) (ReadFileCallPlan, error)
}
type WriteFileCallDiscovery interface {
	DiscoverWriteFileCallFor(context.Context, ToolCallIdentity) (WriteFileCallPlan, error)
}
type RunCommandCallDiscovery interface {
	DiscoverRunCommandCallFor(context.Context, ToolCallIdentity) (RunCommandCallPlan, error)
}
type CommittedWriteFileCallDiscovery interface {
	DiscoverCommittedWriteFileCallFor(context.Context, ToolCallIdentity) (CommittedWriteFileCallPlan, error)
}
type CommittedRunCommandCallDiscovery interface {
	DiscoverCommittedRunCommandCallFor(context.Context, ToolCallIdentity) (CommittedRunCommandCallPlan, error)
}

// RunnerOutcome exposes durable operation metadata, never protected bytes.
type RunnerOutcome struct {
	OperationID OperationID
	AttemptID   AttemptID
	Phase       string
	Outcome     string
	Attempted   bool
}

type RunnerDispatcher interface {
	Execute(context.Context, ToolCallBinding) (RunnerOutcome, error)
	Lookup(context.Context, ToolCallBinding) (RunnerOutcome, bool, error)
	Stop()
	Drain(context.Context) error
	Joined() bool
}

// RunnerCallLifecycle is a trusted host control for a call issued through the
// unified dispatcher. CancelCall only signals that exact local owner. JoinCall
// waits for its original invocation to return and its retained accounting to
// retire; an uncertain commit remains an error. Neither method dispatches,
// retries, reads protected results, or stops another Execution. A nil result
// with no local owner proves local retirement only, not database outcome.
type RunnerCallLifecycle interface {
	CancelCall(context.Context, ToolCallBinding) error
	JoinCall(context.Context, ToolCallBinding) error
}

// RunnerResultBinding preserves the existing durable result identity across
// the three registered Runner tools. Each tool retains its original validator.
type RunnerResultBinding ReadFileResultBinding

func (v RunnerResultBinding) Validate() error {
	switch v.Call.RunnerBinding.OperationName {
	case "read-file":
		return ReadFileResultBinding(v).Validate()
	case "write-file":
		return WriteFileResultBinding(v).Validate()
	case "run-command":
		return RunCommandResultBinding(v).Validate()
	default:
		return operationInvalid()
	}
}

func (RunnerResultBinding) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "runner_tool_result_binding")
}
func (RunnerResultBinding) MarshalJSON() ([]byte, error) {
	return []byte(`"runner_tool_result_binding"`), nil
}
func (RunnerResultBinding) LogValue() slog.Value {
	return slog.StringValue("runner_tool_result_binding")
}

type RunnerResultPlan interface {
	RequiredLocks() []f.LockRequest
	Binding() RunnerResultBinding
}

// The original readers prove all protected arguments, durable bytes and result
// identities in the caller's same-Store transaction. The callback receives the
// complete model-visible canonical result, synchronously under the same locks.
// It may persist a copy in that transaction but may not start external I/O,
// acquire more locks, or use this historical receipt to dispatch another call.
type RunnerResultAuthority interface {
	DiscoverRunnerResult(context.Context, ToolCallBinding) (RunnerResultPlan, error)
	WithRunnerResultInTx(context.Context, f.Tx, RunnerResultPlan, func(context.Context, RunnerResultBinding, mc.ToolResultPart) error) error
}
