// Package contract contains the single Governance approval action and waiting ports.
package contract

import (
	"context"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
)

type RequestIdentity struct{}
type RequestID = f.ID[RequestIdentity]

const (
	Pending  = "pending"
	Approved = "approved"
	Rejected = "rejected"
)

type Waiting struct {
	RequestID   string
	Status      string
	OperationID tc.OperationID
	Fingerprint f.Digest
}
type Gate interface {
	Ensure(context.Context, tc.ToolCallBinding) (Waiting, error)
	// Resolve observes only the original undispatched Execution waiting state.
	// Pending/approved prove created and zero Attempts; rejected proves its
	// zero-Attempt denial. Running result observation uses the Tool lookup port.
	Resolve(context.Context, tc.ToolCallBinding, string) (Waiting, error)
	RequireApprovedInTx(context.Context, f.Tx, tc.ToolCallBinding) error
}

// Execution owns status, cancellation and the exact waiting reference. An empty
// reference is creation while running; a nonempty reference is decision while
// waiting. Both reject cancellation/terminal state, under the execution lock.
// Locks includes the complete current authorization dependency set.
type ExecutionAuthority interface {
	ApprovalLocks(context.Context, tc.ToolCallBinding) ([]f.LockRequest, error)
	RequireApprovalExecutionInTx(context.Context, f.Tx, tc.ToolCallBinding, string) error
}
type Request struct {
	ToolName    string         `json:"tool_name"`
	Version     f.Version      `json:"version"`
	ID          RequestID      `json:"id"`
	ProjectID   i.ProjectID    `json:"project_id"`
	AgentID     i.AgentID      `json:"agent_id"`
	ExecutionID i.ExecutionID  `json:"execution_id"`
	ToolID      i.ToolID       `json:"tool_id"`
	ToolCallID  string         `json:"tool_call_id"`
	OperationID tc.OperationID `json:"operation_id"`
	Fingerprint f.Digest       `json:"operation_fingerprint"`
	Status      string         `json:"status"`
	CreatedAt   f.Instant      `json:"created_at"`
	ResolvedAt  *f.Instant     `json:"resolved_at"`
	ResolvedBy  *i.UserID      `json:"resolved_by"`
}

func (v Request) Waiting() Waiting {
	return Waiting{v.ID.String(), v.Status, v.OperationID, v.Fingerprint}
}
func (v Request) Validate() error {
	if (v.ToolName != "run-command" && !tc.ValidMCPKey(v.ToolName)) || v.Version < 1 || v.Version > 2 || v.ID.Validate() != nil || v.ProjectID.Validate() != nil || v.AgentID.Validate() != nil || v.ExecutionID.Validate() != nil || v.ToolID.Validate() != nil || v.OperationID.Validate() != nil || v.Fingerprint.Validate() != nil || v.CreatedAt.Validate() != nil || v.ToolCallID == "" {
		return f.NewFault(f.InvalidArgument, f.NotStarted)
	}
	if v.Status == Pending && v.Version == 1 && v.ResolvedAt == nil && v.ResolvedBy == nil {
		return nil
	}
	if (v.Status == Approved || v.Status == Rejected) && v.Version == 2 && v.ResolvedAt != nil && v.ResolvedAt.Validate() == nil && !v.ResolvedAt.Time().Before(v.CreatedAt.Time()) && v.ResolvedBy != nil && v.ResolvedBy.Validate() == nil {
		return nil
	}
	return f.NewFault(f.InvalidArgument, f.NotStarted)
}

type CommandIdentity struct{}
type CommandID = f.ID[CommandIdentity]
type Decision struct {
	Decision        string    `json:"decision"`
	ExpectedVersion f.Version `json:"expected_version"`
}

func (d Decision) Validate() error {
	if (d.Decision != "approve" && d.Decision != "reject") || d.ExpectedVersion.Validate() != nil {
		return f.NewFault(f.InvalidArgument, f.NotStarted)
	}
	return nil
}

type Receipt struct {
	CommandID CommandID `json:"command_id"`
	Approval  Request   `json:"approval"`
}
type Lookup struct {
	Status  string   `json:"status"`
	Receipt *Receipt `json:"receipt"`
}
