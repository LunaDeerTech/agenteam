package contract

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"strings"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
)

// MCPBinding freezes the selected Project connection and definition version.
// It contains no endpoint or credentials and conveys no permission to connect.
type MCPBinding struct {
	ProjectID        id.ProjectID `json:"project_id"`
	ConfigID         string       `json:"config_id"`
	ConnectionID     string       `json:"connection_id"`
	RuntimeBindingID string       `json:"runtime_binding_id"`
	ConfigRevision   f.Version    `json:"config_revision"`
	RemoteToolName   string       `json:"remote_tool_name"`
}

func validMCPName(name string) bool {
	if len(name) == 0 || len(name) > 128 {
		return false
	}
	for _, c := range name {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '-' || c == '.') {
			return false
		}
	}
	return true
}

func (b MCPBinding) Validate() error {
	if b.ProjectID.Validate() != nil || b.ConfigRevision.Validate() != nil || !validMCPName(b.RemoteToolName) {
		return invalidRegistry()
	}
	for _, value := range []string{b.ConfigID, b.ConnectionID, b.RuntimeBindingID} {
		if _, err := f.ParseID[struct{}](value); err != nil {
			return invalidRegistry()
		}
	}
	return nil
}

func ValidMCPKey(key string) bool {
	parts := strings.Split(key, ":")
	if len(parts) != 3 || parts[0] != "mcp" || !validMCPName(parts[2]) {
		return false
	}
	_, err := f.ParseID[struct{}](parts[1])
	return err == nil
}

type MCPRegistration struct {
	Definition     Definition
	Binding        MCPBinding
	DefaultEnabled bool
	Active         bool
}

func (r MCPRegistration) Validate(ctx context.Context) error {
	if ctx == nil || r.Binding.Validate() != nil || r.Definition.StableKey != "mcp:"+r.Binding.ConfigID+":"+r.Binding.RemoteToolName || r.Definition.Name != r.Binding.RemoteToolName {
		return invalidRegistry()
	}
	_, err := CanonicalDefinition(ctx, r.Definition)
	return err
}

// PresentNames includes invalid discovered definitions. Reconciliation keeps
// their last valid definition, while removing only genuinely absent names.
type MCPDiscovery struct {
	ProjectID        id.ProjectID
	ConfigID         string
	ConnectionID     string
	RuntimeBindingID string
	ConfigRevision   f.Version
	Tools            []MCPRegistration
	PresentNames     []string
}

type MCPInvocation struct {
	Call        ToolCallBinding
	OperationID OperationID
	AttemptID   AttemptID
	RequestID   string
}

func (v MCPInvocation) Validate() error {
	if v.Call.Validate() != nil || v.Call.MCPBinding.Validate() != nil || v.OperationID.Validate() != nil || v.AttemptID.Validate() != nil {
		return operationInvalid()
	}
	if _, err := f.ParseID[f.Request](v.RequestID); err != nil {
		return operationInvalid()
	}
	return nil
}

type MCPInvocationPlan interface{ RequiredLocks() []f.LockRequest }

type MCPInvocationAuthority interface {
	DiscoverMCPInvocation(context.Context, MCPInvocation) (MCPInvocationPlan, error)
	RequireMCPInvocationInTx(context.Context, f.Tx, MCPInvocation, MCPInvocationPlan) error
}

// The runtime owns Operation/Attempt and the dispatch policy. This backend
// performs one admitted call and reports uncertainty without retrying it.
type MCPBackend interface {
	RequireBindingInTx(context.Context, f.Tx, any) error
	Execute(context.Context, MCPInvocation, []byte) (MCPBackendResult, error)
}

type MCPBackendResult struct {
	Result          mc.ToolResultPart
	OutcomeKnown    bool
	MayHaveExecuted bool
	FailureCode     string
}

func (MCPInvocation) Format(w fmt.State, _ rune)   { _, _ = io.WriteString(w, "mcp_invocation") }
func (MCPInvocation) LogValue() slog.Value         { return slog.StringValue("mcp_invocation") }
func (MCPInvocation) MarshalJSON() ([]byte, error) { return []byte(`"mcp_invocation"`), nil }

// Registry mutations require the original MCP controller transaction, not a
// caller's public discovery DTO. The provider checks its private same-Tx facts.
type MCPRegistryAuthority interface {
	RequireBindingInTx(context.Context, f.Tx, any) error
	RequireMCPDiscoveryInTx(context.Context, f.Tx, MCPDiscovery) error
	RequireMCPRemovalInTx(context.Context, f.Tx, id.ProjectID, string) error
	RequireCurrentMCPBindingInTx(context.Context, f.Tx, MCPBinding) error
	DiscoverMCPRetention(context.Context, ExecutionToolCaptureRequest, []MCPBinding) (MCPRetentionPlan, error)
	RetainMCPRuntimeInTx(context.Context, f.Tx, id.ExecutionID, MCPBinding, MCPRetentionPlan) error
}

// Called back only while Registry lends an original Execution capture witness.
type MCPRetentionPlan interface{ RequiredLocks() []f.LockRequest }
type MCPCaptureAuthority interface {
	RequireMCPRetentionDiscoveryInTx(context.Context, f.Tx, ExecutionToolCaptureRequest, MCPBinding) error
	RequireMCPCaptureInTx(context.Context, f.Tx, id.ExecutionID, MCPBinding) error
}
