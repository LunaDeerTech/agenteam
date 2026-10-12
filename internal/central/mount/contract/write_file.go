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

// WriteFileIntent is an identity and argument description, never authority. The
// current Operation owner must bind it to its original running Execution,
// committed model call/arguments and applicable permission/approval policy.
type WriteFileIntent struct {
	Actor       i.Actor
	ProjectID   i.ProjectID
	AgentID     i.AgentID
	ExecutionID i.ExecutionID
	OperationID tc.OperationID
	MountID     i.MountID
	Arguments   p.WriteFileArguments
}

func (r WriteFileIntent) Validate() error {
	if r.Actor.Validate() != nil || r.ProjectID.Validate() != nil || r.AgentID.Validate() != nil || r.ExecutionID.Validate() != nil || r.OperationID.Validate() != nil || r.MountID.Validate() != nil || r.Arguments.Validate() != nil {
		return f.NewFault(f.InvalidArgument, f.NotStarted)
	}
	d := r.Actor.Details()
	if d.Kind != i.AgentRun || d.ProjectID != r.ProjectID.String() || d.AgentID != r.AgentID.String() || d.ExecutionID != r.ExecutionID.String() {
		return f.NewFault(f.InvalidArgument, f.NotStarted)
	}
	return nil
}

func (WriteFileIntent) Format(w fmt.State, _ rune)   { _, _ = io.WriteString(w, "write_file_intent") }
func (WriteFileIntent) MarshalJSON() ([]byte, error) { return []byte(`"write_file_intent"`), nil }
func (WriteFileIntent) LogValue() slog.Value         { return slog.StringValue("write_file_intent") }

// WriteFileAuthority is a trusted composition port, not a public command API.
// It contributes the entire original Operation/Execution/Trigger lock plan and
// owns the original live same-Store witness for the callback duration. Require
// must reject cancelled/terminal executions, wrong Operation/arguments and any
// missing grant. The callback may use the real current Agent reader; it must
// execute synchronously exactly once and may not recurse into this resolver.
// Empty RequestID is metadata resolution for the original created Operation.
// A nonempty RequestID is dispatch Claim: the owner must require its private
// known-committed-and-returned Attempt admission and verify the exact original
// active Attempt/backend RequestID in this transaction. A caller-supplied DTO
// or a prepared transport call is never proof of that admission.
type WriteFileAuthority interface {
	WriteFileLocks(WriteFileIntent) ([]f.LockRequest, error)
	WithWriteFileInTx(context.Context, f.Tx, WriteFileIntent, p.ID, func(context.Context) error) error
}
