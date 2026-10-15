package contract

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"unicode/utf8"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
)

// MCPContentReadRequest describes an existing canonical Tool result. It grants
// no read: the Execution owner must find this exact source in its original,
// currently running Model input, and MCP must recheck its own materialization.
type MCPContentReadRequest struct {
	Actor             i.Actor
	ProjectID         i.ProjectID
	ExecutionID       i.ExecutionID
	Reference         oc.BusinessFileRef
	MaterializationID string
	ConfigID          string
	ConnectionID      string
	RuntimeBindingID  string
	ConfigRevision    f.Version
	OperationID       tc.OperationID
	AttemptID         tc.AttemptID
	RequestID         string
	URI               string
}

func (r MCPContentReadRequest) Validate() error {
	if r.Actor.Validate() != nil || r.ProjectID.Validate() != nil || r.ExecutionID.Validate() != nil || r.Reference.Validate() != nil || r.ConfigRevision.Validate() != nil || r.OperationID.Validate() != nil || r.AttemptID.Validate() != nil || !utf8.ValidString(r.URI) || len(r.URI) > 8192 {
		return invalid()
	}
	a, source := r.Actor.Details(), r.Reference.Details()
	if a.Kind != i.AgentRun || a.ProjectID != r.ProjectID.String() || a.ExecutionID != r.ExecutionID.String() || source.Kind != oc.MCPFile || source.ProjectID != r.ProjectID {
		return invalid()
	}
	for _, raw := range []string{r.MaterializationID, r.ConfigID, r.ConnectionID, r.RuntimeBindingID, r.RequestID} {
		if _, err := f.ParseID[struct{}](raw); err != nil {
			return invalid()
		}
	}
	return nil
}

// All three methods require the same original private Execution/Model call.
// A committed projection, Actor or recomputed AccessDependencies is no grant.
type MCPContentReadAuthority interface {
	DiscoverMCPContentRead(context.Context, MCPContentReadRequest) (oc.AccessDependencies, error)
	ValidateMCPContentReadInTx(context.Context, f.Tx, MCPContentReadRequest, oc.AccessDependencies) error
	RequireMCPContentReadInTx(context.Context, f.Tx, MCPContentReadRequest) error
}

func (MCPContentReadRequest) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "mcp_content_source")
}
func (MCPContentReadRequest) MarshalJSON() ([]byte, error) {
	return []byte(`"mcp_content_source"`), nil
}
func (*MCPContentReadRequest) UnmarshalJSON([]byte) error { return invalid() }
func (MCPContentReadRequest) LogValue() slog.Value        { return slog.StringValue("mcp_content_source") }

// Tail authority can only retire the original Object reader. It never grants
// a new body read, and remains valid during the registered Model call's Close.
type MCPContentReadTailAuthority interface {
	MCPContentReadActor(context.Context) (i.Actor, error)
	DiscoverMCPContentReadTail(context.Context, MCPContentReadRequest, oc.AccessRequest) (oc.AccessDependencies, error)
	ValidateMCPContentReadTailInTx(context.Context, f.Tx, MCPContentReadRequest, oc.AccessRequest, oc.AccessDependencies) error
}
