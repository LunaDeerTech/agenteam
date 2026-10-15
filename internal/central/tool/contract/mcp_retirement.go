package contract

import (
	"context"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

// MCPExecutionRetirementRequest names the terminal candidate of the original
// Execution owner. The tuple is not authority: implementations require the
// caller's live retirement witness before discovering or releasing leases.
type MCPExecutionRetirementRequest struct {
	ProjectID       id.ProjectID
	AgentID         id.AgentID
	ExecutionID     id.ExecutionID
	TerminalVersion f.Version
}

func (r MCPExecutionRetirementRequest) Validate() error {
	if r.ProjectID.Validate() != nil || r.AgentID.Validate() != nil || r.ExecutionID.Validate() != nil || r.TerminalVersion.Validate() != nil {
		return f.NewFault(f.InvalidArgument, f.NotStarted)
	}
	return nil
}

type MCPExecutionRetirementPlan interface{ RequiredLocks() []f.LockRequest }

type MCPExecutionRetirement interface {
	DiscoverMCPExecutionRetirement(context.Context, MCPExecutionRetirementRequest) (MCPExecutionRetirementPlan, error)
	ReleaseMCPExecutionInTx(context.Context, f.Tx, MCPExecutionRetirementRequest, MCPExecutionRetirementPlan) error
	MCPExecutionRetiredInTx(context.Context, f.Tx, MCPExecutionRetirementRequest, MCPExecutionRetirementPlan) (bool, error)
}

// The same original owner proves both release and its read-only confirmation.
// Commit-unknown recovery must observe the original release, never repeat it.
type MCPExecutionRetirementAuthority interface {
	CheckMCPRetirementPlan(context.Context, MCPExecutionRetirementRequest) error
	CheckMCPRetirementInTx(context.Context, f.Tx, MCPExecutionRetirementRequest) error
}
