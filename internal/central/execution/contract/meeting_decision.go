package contract

import (
	"context"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
	p "github.com/LunaDeerTech/agenteam/internal/runnerprotocol"
)

// MeetingDecisionCall identifies one original live Builtin operation. These
// fields are facts, not permission: Tool and Meeting both require their private
// invocation witnesses before lending the original Execution transaction.
type MeetingDecisionCall struct {
	Call        tc.ToolCallBinding
	OperationID tc.OperationID
	AttemptID   tc.AttemptID
	RequestID   p.ID
}

func (v MeetingDecisionCall) Validate() error {
	if v.Call.Validate() != nil || v.Call.Binding.HandlerID != "meeting.request-decision" || v.Call.Binding.ContractRevision != 1 || v.OperationID.Validate() != nil || v.AttemptID.Validate() != nil || !v.RequestID.Valid() {
		return invalid()
	}
	return nil
}

type MeetingDecisionResolution struct {
	DecisionID string
	Version    f.Version
}

// MeetingDecisionAuthority is fixed at construction. Call checks require the
// Meeting domain's private, same-transaction witness. Result methods only read
// canonical facts; they cannot create a Tool result or mark it consumed.
type MeetingDecisionAuthority interface {
	RequireMeetingDecisionBindingInTx(context.Context, f.Tx, any) error
	RequireMeetingDecisionCallInTx(context.Context, f.Tx, MeetingDecisionCall, Trigger, string, string) error
	DiscoverMeetingDecisionResult(context.Context, tc.RunnerResultBinding) ([]f.LockRequest, error)
	ReadMeetingDecisionResultInTx(context.Context, f.Tx, tc.RunnerResultBinding, Trigger, mc.ToolResultPart) (MeetingDecisionResolution, error)
}

// MeetingDecisionExecution is implemented by the original ToolCallDriver.
// BuiltinAuthority lends its private plan and persisted Operation/Attempt;
// neither method opens a transaction, acquires locks, or performs external I/O.
type MeetingDecisionExecution interface {
	WithMeetingDecisionToolCallInTx(context.Context, f.Tx, MeetingDecisionCall, tc.ToolCallPlan, string, bool, func(context.Context, []byte) error) error
	SetMeetingDecisionWaitingInTx(context.Context, f.Tx, MeetingDecisionCall, tc.ToolCallPlan, string, bool) error
}

// MeetingDecisionToolAuthority is implemented by the original Builtin owner.
// Discovery includes that owner's complete lock union; domain locks must be
// added before entering the transaction. Lookup only lends committed input.
type MeetingDecisionToolAuthority interface {
	RequireMeetingDecisionToolBindingInTx(context.Context, f.Tx, any) error
	DiscoverMeetingDecisionTool(context.Context, MeetingDecisionCall, bool) ([]f.LockRequest, error)
	WithMeetingDecisionToolInTx(context.Context, f.Tx, MeetingDecisionCall, string, bool, func(context.Context, []byte) error) error
	SetMeetingDecisionToolWaitingInTx(context.Context, f.Tx, MeetingDecisionCall, string, bool) error
	WithCommittedMeetingDecisionToolInTx(context.Context, f.Tx, MeetingDecisionCall, func(context.Context, []byte) error) error
}
