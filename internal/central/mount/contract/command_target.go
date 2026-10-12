package contract

import (
	"fmt"
	"io"
	"log/slog"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	rc "github.com/LunaDeerTech/agenteam/internal/central/runner/contract"
)

// CommandTargetRequest names current metadata inside the original Execution
// owner's live Agent Current transaction. It is not Operation authority and
// deliberately carries neither an Operation identity nor protected arguments.
type CommandTargetRequest struct {
	Actor       i.Actor
	ProjectID   i.ProjectID
	AgentID     i.AgentID
	ExecutionID i.ExecutionID
	MountID     i.MountID
}

func (r CommandTargetRequest) Validate() error {
	if r.Actor.Validate() != nil || r.ProjectID.Validate() != nil || r.AgentID.Validate() != nil || r.ExecutionID.Validate() != nil || r.MountID.Validate() != nil {
		return f.NewFault(f.InvalidArgument, f.NotStarted)
	}
	d := r.Actor.Details()
	if d.Kind != i.AgentRun || d.ProjectID != r.ProjectID.String() || d.AgentID != r.AgentID.String() || d.ExecutionID != r.ExecutionID.String() {
		return f.NewFault(f.InvalidArgument, f.NotStarted)
	}
	return nil
}

func (CommandTargetRequest) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "command_target_request")
}
func (CommandTargetRequest) MarshalJSON() ([]byte, error) {
	return []byte(`"command_target_request"`), nil
}
func (CommandTargetRequest) LogValue() slog.Value {
	return slog.StringValue("command_target_request")
}

// CommandTarget is only a safe metadata projection. The workspace name is a
// logical segment, never a Runner root or system path. Retaining this value
// grants neither a ticket nor permission to dispatch a request.
type CommandTarget struct {
	MountID       i.MountID   `json:"mount_id"`
	WorkspaceID   WorkspaceID `json:"workspace_id"`
	RunnerID      rc.RunnerID `json:"runner_id"`
	WorkspaceName string      `json:"workspace_name"`
	AgentVersion  f.Version   `json:"agent_version"`
	MountVersion  f.Version   `json:"mount_version"`
}

func (v CommandTarget) Validate() error {
	if v.MountID.Validate() != nil || v.WorkspaceID.Validate() != nil || v.RunnerID.Validate() != nil || !ValidWorkspaceName(v.WorkspaceName) || v.AgentVersion.Validate() != nil || v.MountVersion.Validate() != nil {
		return f.NewFault(f.InvalidArgument, f.NotStarted)
	}
	return nil
}

func (CommandTarget) Format(w fmt.State, _ rune) { _, _ = io.WriteString(w, "command_target") }
func (CommandTarget) LogValue() slog.Value       { return slog.StringValue("command_target") }
