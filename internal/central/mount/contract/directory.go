package contract

import (
	"context"
	"fmt"
	"io"
	"log/slog"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
	p "github.com/LunaDeerTech/agenteam/internal/runnerprotocol"
)

// DirectoryIntent is an identity and argument description, never authority. The
// current Operation owner must bind it to its original running Execution,
// committed model call/arguments and applicable permission/approval policy.
type DirectoryIntent struct {
	Actor       i.Actor
	ProjectID   i.ProjectID
	AgentID     i.AgentID
	ExecutionID i.ExecutionID
	OperationID tc.OperationID
	MountID     i.MountID
	Operation   string
	Arguments   p.DirectoryArguments
}

func (r DirectoryIntent) Validate() error {
	if r.Actor.Validate() != nil || r.ProjectID.Validate() != nil || r.AgentID.Validate() != nil || r.ExecutionID.Validate() != nil || r.OperationID.Validate() != nil || r.MountID.Validate() != nil || r.Arguments.Validate(r.Operation) != nil {
		return f.NewFault(f.InvalidArgument, f.NotStarted)
	}
	d := r.Actor.Details()
	if d.Kind != i.AgentRun || d.ProjectID != r.ProjectID.String() || d.AgentID != r.AgentID.String() || d.ExecutionID != r.ExecutionID.String() {
		return f.NewFault(f.InvalidArgument, f.NotStarted)
	}
	return nil
}

func (DirectoryIntent) Format(w fmt.State, _ rune)   { _, _ = io.WriteString(w, "directory_intent") }
func (DirectoryIntent) MarshalJSON() ([]byte, error) { return []byte(`"directory_intent"`), nil }
func (DirectoryIntent) LogValue() slog.Value         { return slog.StringValue("directory_intent") }

// DirectoryAuthority is a trusted composition port, not a public command API.
// It contributes the entire original Operation/Execution/Trigger lock plan and
// owns the original live same-Store witness for the callback duration. Require
// must reject cancelled/terminal executions, wrong Operation/arguments and any
// missing grant. The callback may use the real current Agent reader; it must
// execute synchronously exactly once and may not recurse into this resolver.
// This slice has no production ToolCall producer; a strict D15 §7 substitute
// in integration evidence must remain explicitly identified as such.
type DirectoryAuthority interface {
	DirectoryLocks(DirectoryIntent) ([]f.LockRequest, error)
	WithDirectoryInTx(context.Context, f.Tx, DirectoryIntent, func(context.Context) error) error
}
