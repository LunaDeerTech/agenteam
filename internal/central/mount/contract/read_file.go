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

// ReadFileIntent is an identity and argument description, never authority. The
// current Operation owner must bind it to its original running Execution,
// committed model call/arguments and applicable permission/approval policy.
type ReadFileIntent struct {
	Actor       i.Actor
	ProjectID   i.ProjectID
	AgentID     i.AgentID
	ExecutionID i.ExecutionID
	OperationID tc.OperationID
	MountID     i.MountID
	Arguments   p.ReadFileArguments
}

func (r ReadFileIntent) Validate() error {
	if r.Actor.Validate() != nil || r.ProjectID.Validate() != nil || r.AgentID.Validate() != nil || r.ExecutionID.Validate() != nil || r.OperationID.Validate() != nil || r.MountID.Validate() != nil || r.Arguments.Validate() != nil {
		return f.NewFault(f.InvalidArgument, f.NotStarted)
	}
	d := r.Actor.Details()
	if d.Kind != i.AgentRun || d.ProjectID != r.ProjectID.String() || d.AgentID != r.AgentID.String() || d.ExecutionID != r.ExecutionID.String() {
		return f.NewFault(f.InvalidArgument, f.NotStarted)
	}
	return nil
}

func (ReadFileIntent) Format(w fmt.State, _ rune)   { _, _ = io.WriteString(w, "read_file_intent") }
func (ReadFileIntent) MarshalJSON() ([]byte, error) { return []byte(`"read_file_intent"`), nil }
func (ReadFileIntent) LogValue() slog.Value         { return slog.StringValue("read_file_intent") }

// ReadFileAuthority is a trusted composition port, not a public command API.
// It contributes the entire original Operation/Execution/Trigger lock plan and
// owns the original live same-Store witness for the callback duration. Require
// must reject cancelled/terminal executions, wrong Operation/arguments and any
// missing grant. The callback may use the real current Agent reader; it must
// execute synchronously exactly once and may not recurse into this resolver.
// This slice has no production ToolCall producer; a strict D15 §7 substitute
// in integration evidence must remain explicitly identified as such.
type ReadFileAuthority interface {
	ReadFileLocks(ReadFileIntent) ([]f.LockRequest, error)
	WithReadFileInTx(context.Context, f.Tx, ReadFileIntent, func(context.Context) error) error
}
