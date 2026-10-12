package contract

import (
	"fmt"
	"io"
	"log/slog"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	rc "github.com/LunaDeerTech/agenteam/internal/central/runner/contract"
)

// WriteFileTargetRequest names current metadata inside the original Execution
// owner's live Agent Current transaction. It is not Operation authority and
// deliberately carries neither an Operation identity nor protected arguments.
type WriteFileTargetRequest struct {
	Actor       i.Actor
	ProjectID   i.ProjectID
	AgentID     i.AgentID
	ExecutionID i.ExecutionID
	MountID     i.MountID
}

func (r WriteFileTargetRequest) Validate() error {
	if r.Actor.Validate() != nil || r.ProjectID.Validate() != nil || r.AgentID.Validate() != nil || r.ExecutionID.Validate() != nil || r.MountID.Validate() != nil {
		return f.NewFault(f.InvalidArgument, f.NotStarted)
	}
	d := r.Actor.Details()
	if d.Kind != i.AgentRun || d.ProjectID != r.ProjectID.String() || d.AgentID != r.AgentID.String() || d.ExecutionID != r.ExecutionID.String() {
		return f.NewFault(f.InvalidArgument, f.NotStarted)
	}
	return nil
}

func (WriteFileTargetRequest) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "write_file_target_request")
}
func (WriteFileTargetRequest) MarshalJSON() ([]byte, error) {
	return []byte(`"write_file_target_request"`), nil
}
func (WriteFileTargetRequest) LogValue() slog.Value {
	return slog.StringValue("write_file_target_request")
}

// WriteFileTarget is only a safe metadata projection. The workspace name is a
// logical segment, never a Runner root or system path. Retaining this value
// grants neither a ticket nor permission to dispatch a request.
type WriteFileTarget struct {
	MountID       i.MountID   `json:"mount_id"`
	WorkspaceID   WorkspaceID `json:"workspace_id"`
	RunnerID      rc.RunnerID `json:"runner_id"`
	WorkspaceName string      `json:"workspace_name"`
	AgentVersion  f.Version   `json:"agent_version"`
	MountVersion  f.Version   `json:"mount_version"`
}

func (v WriteFileTarget) Validate() error {
	if v.MountID.Validate() != nil || v.WorkspaceID.Validate() != nil || v.RunnerID.Validate() != nil || !ValidWorkspaceName(v.WorkspaceName) || v.AgentVersion.Validate() != nil || v.MountVersion.Validate() != nil {
		return f.NewFault(f.InvalidArgument, f.NotStarted)
	}
	return nil
}

func (WriteFileTarget) Format(w fmt.State, _ rune) { _, _ = io.WriteString(w, "write_file_target") }
func (WriteFileTarget) LogValue() slog.Value       { return slog.StringValue("write_file_target") }
