package contract

import (
	"context"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

// MCPResourceConnection is a captured resource capability. It deliberately has
// no remote tool name and is never projected into a remote Tool binding.
type MCPResourceConnection struct {
	ProjectID                                id.ProjectID
	ConfigID, ConnectionID, RuntimeBindingID string
	ConfigRevision                           f.Version
}

func (b MCPResourceConnection) Validate() error {
	if b.ProjectID.Validate() != nil || b.ConfigRevision.Validate() != nil {
		return invalidRegistry()
	}
	for _, v := range []string{b.ConfigID, b.ConnectionID, b.RuntimeBindingID} {
		if _, err := f.ParseID[struct{}](v); err != nil {
			return invalidRegistry()
		}
	}
	return nil
}

type MCPResourceInvocation struct {
	Call        ToolCallBinding
	OperationID OperationID
	AttemptID   AttemptID
	RequestID   string
}

func (v MCPResourceInvocation) Validate() error {
	if v.Call.Validate() != nil || v.OperationID.Validate() != nil || v.AttemptID.Validate() != nil || v.Call.Binding.ContractRevision != 1 {
		return operationInvalid()
	}
	if v.Call.Binding.HandlerID != "mcp.list-resources" && v.Call.Binding.HandlerID != "mcp.read-resource" {
		return operationInvalid()
	}
	if _, err := f.ParseID[f.Request](v.RequestID); err != nil {
		return operationInvalid()
	}
	return nil
}

type MCPResourceInvocationPlan interface {
	RequiredLocks() []f.LockRequest
	RuntimeConnections() []MCPResourceConnection
}
type MCPResourceInvocationAuthority interface {
	DiscoverMCPResourceInvocation(context.Context, MCPResourceInvocation) (MCPResourceInvocationPlan, error)
	RequireMCPResourceInvocationInTx(context.Context, f.Tx, MCPResourceInvocation, MCPResourceInvocationPlan) error
}

type MCPResourceCapturePlan interface {
	RequiredLocks() []f.LockRequest
	RuntimeConnections() []MCPResourceConnection
}

// Discovery shares the original remote-Tool retention plan when present, so
// overlapping connections acquire a credential lease only once per Execution.
// Neither method can authorize a public request DTO without Registry's witness.
type MCPResourceCaptureProvider interface {
	DiscoverMCPResources(context.Context, ExecutionToolCaptureRequest, MCPRetentionPlan) (MCPResourceCapturePlan, error)
	RetainMCPResourcesInTx(context.Context, f.Tx, ExecutionToolCaptureRequest, MCPResourceCapturePlan) error
}
type MCPResourceCaptureAuthority interface {
	RequireMCPResourceDiscoveryInTx(context.Context, f.Tx, ExecutionToolCaptureRequest) error
	RequireMCPResourceCaptureInTx(context.Context, f.Tx, ExecutionToolCaptureRequest, []MCPResourceConnection) error
}
