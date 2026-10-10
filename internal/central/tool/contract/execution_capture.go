package contract

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"slices"
	"strings"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

// ExecutionToolCaptureRequest is an identity projection, never an execution
// grant. The actual Execution owner proves its original live preparing call.
type ExecutionToolCaptureRequest struct {
	ProjectID   id.ProjectID
	AgentID     id.AgentID
	ExecutionID id.ExecutionID
}

func (r ExecutionToolCaptureRequest) Validate() error {
	if r.ProjectID.Validate() != nil || r.AgentID.Validate() != nil || r.ExecutionID.Validate() != nil {
		return operationInvalid()
	}
	return nil
}

// RequiredLocks is the known Execution-side discovery subset. Registry adds
// its own lock and discovered ToolSpec locks to the caller's final full union.
func (r ExecutionToolCaptureRequest) RequiredLocks() []f.LockRequest {
	if r.Validate() != nil {
		return nil
	}
	p, _ := f.ProjectLock(r.ProjectID.String())
	s, _ := f.ProjectScheduleLock(r.ProjectID.String())
	a, _ := f.AgentLock(r.AgentID.String())
	e, _ := f.AggregateLock(f.ExecutionAggregate, r.ExecutionID.String())
	return []f.LockRequest{{Key: p, Mode: f.Shared}, {Key: s, Mode: f.Exclusive}, {Key: a, Mode: f.Shared}, {Key: e, Mode: f.Exclusive}}
}

// ToolSnapshotSelection is projected by the trusted Execution owner. Allowed
// IDs are the actual Agent capability set; denied IDs and constraints come from
// the original persisted launch policy. Core tools are handled by Registry,
// not manufactured as ordinary Agent references. Nonempty scope constraints
// remain unsupported by this first Builtin metadata provider and fail closed.
type ToolSnapshotSelection struct {
	AllowedToolIDs   []id.ToolID
	DeniedToolIDs    []id.ToolID
	ScopeConstraints []json.RawMessage
}

func validCaptureIDs(values []id.ToolID) bool {
	if values == nil || len(values) > 128 {
		return false
	}
	for n, v := range values {
		if v.Validate() != nil || n > 0 && values[n-1].String() >= v.String() {
			return false
		}
	}
	return true
}
func (s ToolSnapshotSelection) Validate() error {
	if !validCaptureIDs(s.AllowedToolIDs) || !validCaptureIDs(s.DeniedToolIDs) || s.ScopeConstraints == nil {
		return operationInvalid()
	}
	if len(s.ScopeConstraints) != 0 {
		return f.NewFault(f.CapabilityUnsupported, f.NotStarted)
	}
	return nil
}
func (s ToolSnapshotSelection) Clone() ToolSnapshotSelection {
	s.AllowedToolIDs = slices.Clone(s.AllowedToolIDs)
	s.DeniedToolIDs = slices.Clone(s.DeniedToolIDs)
	s.ScopeConstraints = slices.Clone(s.ScopeConstraints)
	for n := range s.ScopeConstraints {
		s.ScopeConstraints[n] = slices.Clone(s.ScopeConstraints[n])
	}
	return s
}

// Discovery reads no Agent canonical table. Tool freezes its own real Agent
// reference head/version after this callback has proved the original preparing
// claim, fence, process and current Project in the same Store/short Tx.
type ExecutionToolDiscoveryFacts struct {
	Project          pc.ProjectRef
	AttemptBinding   f.Digest
	DeniedToolIDs    []id.ToolID
	ScopeConstraints []json.RawMessage
}

func (v ExecutionToolDiscoveryFacts) Validate(r ExecutionToolCaptureRequest) error {
	if r.Validate() != nil || v.Project.Validate() != nil || v.Project.ID != r.ProjectID || v.Project.Lifecycle != pc.Active || v.AttemptBinding.Validate() != nil {
		return operationInvalid()
	}
	return (ToolSnapshotSelection{AllowedToolIDs: []id.ToolID{}, DeniedToolIDs: v.DeniedToolIDs, ScopeConstraints: v.ScopeConstraints}).Validate()
}
func (v ExecutionToolDiscoveryFacts) Clone() ExecutionToolDiscoveryFacts {
	v.Project = v.Project.Clone()
	s := (ToolSnapshotSelection{DeniedToolIDs: v.DeniedToolIDs, ScopeConstraints: v.ScopeConstraints}).Clone()
	v.DeniedToolIDs, v.ScopeConstraints = s.DeniedToolIDs, s.ScopeConstraints
	return v
}

// Final facts are available only after the actual Trigger input and Agent
// capture returned inside this exact caller Tx. The provider compares them
// with discovery and its reference head before its first reference write.
type ExecutionToolCaptureFacts struct {
	Project        pc.ProjectRef
	AttemptBinding f.Digest
	AgentVersion   f.Version
	Selection      ToolSnapshotSelection
}

func (v ExecutionToolCaptureFacts) Validate(r ExecutionToolCaptureRequest) error {
	if r.Validate() != nil || v.Project.Validate() != nil || v.Project.ID != r.ProjectID || v.Project.Lifecycle != pc.Active || v.AttemptBinding.Validate() != nil || v.AgentVersion.Validate() != nil {
		return operationInvalid()
	}
	return v.Selection.Validate()
}
func (v ExecutionToolCaptureFacts) Clone() ExecutionToolCaptureFacts {
	v.Project = v.Project.Clone()
	v.Selection = v.Selection.Clone()
	return v
}

// Both ports are supplied by the real Execution Authority, not by a caller's
// DTO or context flag. Discovery and final witnesses are not interchangeable.
// Final checks the same original live call/claim/fence, private same-Tx witness,
// current process and complete held lock union; it performs no recursive Tool
// call, external I/O, transaction start, or lock acquisition.
type ExecutionToolCaptureAuthority interface {
	RequireToolDiscoveryInTx(context.Context, f.Tx, ExecutionToolCaptureRequest) (ExecutionToolDiscoveryFacts, error)
	RequireToolCaptureInTx(context.Context, f.Tx, ExecutionToolCaptureRequest) (ExecutionToolCaptureFacts, error)
}

type ExecutionToolPlan interface{ RequiredLocks() []f.LockRequest }
type ExecutionTools interface {
	DiscoverExecutionTools(context.Context, ExecutionToolCaptureRequest) (ExecutionToolPlan, error)
	ResolveExecutionToolsInTx(context.Context, f.Tx, ExecutionToolCaptureRequest, ExecutionToolPlan) ([]ExecutionTool, error)
}

// This slice supports actual registered Builtins only. Runner/MCP bindings and
// leases are not represented by a placeholder Builtin or a successful empty
// result. Capturing this metadata grants no runtime invocation permission.
type BuiltinExecutionBinding struct {
	Binding          BuiltinBinding
	ScopeResolverID  ScopeResolverID
	RiskClassifierID RiskClassifierID
	Class            ToolClass
}
type ExecutionTool struct {
	ToolID           id.ToolID
	SpecRevision     f.Version
	ModelVisibleName string
	BindingSnapshot  BuiltinExecutionBinding
}

func (v ExecutionTool) Validate() error {
	b := v.BindingSnapshot
	if (SpecRef{ToolID: v.ToolID, SpecRevision: v.SpecRevision}).Validate() != nil || v.ModelVisibleName != "tool_"+strings.ReplaceAll(v.ToolID.String(), "-", "") || !stablePart(b.Binding.HandlerID) || b.Binding.ContractRevision.Validate() != nil || !stablePart(string(b.ScopeResolverID)) || !stablePart(string(b.RiskClassifierID)) || (b.Class != OrdinaryTool && b.Class != CoreTool) {
		return operationInvalid()
	}
	return nil
}

func (ExecutionToolCaptureRequest) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "execution_tool_capture_request")
}
func (ExecutionToolCaptureRequest) LogValue() slog.Value {
	return slog.StringValue("execution_tool_capture_request")
}
func (ExecutionTool) Format(w fmt.State, _ rune) { _, _ = io.WriteString(w, "execution_tool_metadata") }
func (ExecutionTool) LogValue() slog.Value       { return slog.StringValue("execution_tool_metadata") }
