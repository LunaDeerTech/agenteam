package contract

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"unicode/utf8"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
)

// These identities belong to Tool. Execution, Model call and payload identities
// remain owned by their original domains; a valid projection is not authority.
type Operation struct{}
type Attempt struct{}
type OperationID = f.ID[Operation]
type AttemptID = f.ID[Attempt]

// ToolInputReference identifies the original protected arguments payload. The
// Execution owner proves the reference against its committed input binding.
// Tool never stores the text_files body in its metadata tables.
type ToolInputReference struct {
	PayloadID string
	SHA256    f.Digest
	Bytes     f.Progress
}

// ToolCallBinding is the consumer-owned projection of the D01 trusted context
// and one already committed model call. UUID strings below project upstream
// identities; they do not introduce Tool-owned Snapshot/Round/Payload types.
type ToolCallBinding struct {
	Actor              id.Actor
	ProjectID          id.ProjectID
	AgentID            id.AgentID
	ExecutionID        id.ExecutionID
	RoundID            string
	SnapshotID         string
	InputBindingID     string
	LogicalCallID      mc.CallID
	InvocationID       mc.InvocationID
	CallID             string
	ModelVisibleName   string
	Spec               SpecRef
	Binding            BuiltinBinding
	Input              ToolInputReference
	CanonicalArguments f.Digest
}

func operationInvalid() error { return f.NewFault(f.InvalidArgument, f.NotStarted) }

func (v ToolInputReference) Validate() error {
	if _, err := f.ParseID[struct{}](v.PayloadID); err != nil || v.SHA256.Validate() != nil || v.Bytes < 1 || v.Bytes > 1<<20 {
		return operationInvalid()
	}
	return nil
}

func (v ToolCallBinding) Validate() error {
	if v.Actor.Validate() != nil || v.ProjectID.Validate() != nil || v.AgentID.Validate() != nil || v.ExecutionID.Validate() != nil || v.LogicalCallID.Validate() != nil || v.InvocationID.Validate() != nil || v.Spec.Validate() != nil || v.Input.Validate() != nil || v.CanonicalArguments.Validate() != nil {
		return operationInvalid()
	}
	a := v.Actor.Details()
	if a.Kind != id.AgentRun || a.ProjectID != v.ProjectID.String() || a.AgentID != v.AgentID.String() || a.ExecutionID != v.ExecutionID.String() {
		return f.NewFault(f.Forbidden, f.NotStarted)
	}
	for _, value := range []string{v.RoundID, v.SnapshotID, v.InputBindingID} {
		if _, err := f.ParseID[struct{}](value); err != nil {
			return operationInvalid()
		}
	}
	if len(v.CallID) == 0 || len(v.CallID) > 256 || !utf8.ValidString(v.CallID) || strings.ContainsRune(v.CallID, 0) || v.ModelVisibleName != "tool_"+strings.ReplaceAll(v.Spec.ToolID.String(), "-", "") {
		return operationInvalid()
	}
	if v.Binding.HandlerID != "skill.install" || v.Binding.ContractRevision != 1 {
		return f.NewFault(f.CapabilityUnsupported, f.NotStarted)
	}
	return nil
}

// ToolCallPlan must be issued by the same actual ExecutionAuthority. A foreign
// plan or a request differing in any field is rejected, including empty scopes.
type ToolCallPlan interface {
	RequiredLocks() []f.LockRequest
}

// OperationExecutionAuthority is implemented by the actual Execution owner.
// Discover performs no dispatch. Require uses this exact Store's live caller Tx
// and the full pre-collected lock union; it cannot Begin/Commit or add locks.
//
// It proves current Project/Agent/Execution, immutable Snapshot name/spec/binding,
// the original committed Model call and protected input reference, and current
// Capability intersected with Execution Policy. A constructed Actor, config DTO,
// Registry row or caller-supplied digest cannot replace those canonical facts.
// Missing canonical model/round/payload writers remain DependencyUnbound.
type OperationExecutionAuthority interface {
	DiscoverToolCall(context.Context, ToolCallBinding) (ToolCallPlan, error)
	RequireToolCallInTx(context.Context, f.Tx, ToolCallBinding, ToolCallPlan) error
}

func (ToolCallBinding) Format(w fmt.State, _ rune)   { _, _ = io.WriteString(w, "tool_call_binding") }
func (ToolCallBinding) LogValue() slog.Value         { return slog.StringValue("tool_call_binding") }
func (ToolCallBinding) MarshalJSON() ([]byte, error) { return []byte(`"tool_call_binding"`), nil }
func (ToolInputReference) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "tool_input_reference")
}
func (ToolInputReference) LogValue() slog.Value         { return slog.StringValue("tool_input_reference") }
func (ToolInputReference) MarshalJSON() ([]byte, error) { return []byte(`"tool_input_reference"`), nil }
