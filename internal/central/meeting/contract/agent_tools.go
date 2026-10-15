package contract

import (
	"context"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
	p "github.com/LunaDeerTech/agenteam/internal/runnerprotocol"
)

// AgentToolCall identifies the original invocation. Its fields are not a grant.
type AgentToolCall struct {
	Call        tc.ToolCallBinding
	OperationID tc.OperationID
	AttemptID   tc.AttemptID
	RequestID   p.ID
}

func AgentHandler(v tc.BuiltinBinding) bool {
	if v.ContractRevision != 1 {
		return false
	}
	switch v.HandlerID {
	case "meeting.list", "meeting.request", "meeting.add-agent", "meeting.remove-agent", "meeting.update", "meeting.comment":
		return true
	}
	return false
}
func (v AgentToolCall) Validate() error {
	if v.Call.Validate() != nil || !AgentHandler(v.Call.Binding) || v.OperationID.Validate() != nil || v.AttemptID.Validate() != nil || !v.RequestID.Valid() {
		return invalid()
	}
	return nil
}

// AgentSource is returned only by the original Execution owner under its
// private ToolCallPlan. Origin is provenance and never reference authority.
type AgentSource struct {
	ExecutionID i.ExecutionID   `json:"execution_id"`
	Origin      *ProposalOrigin `json:"origin,omitempty"`
}

func (v AgentSource) Validate() error {
	if v.ExecutionID.Validate() != nil || v.Origin != nil && v.Origin.Validate() != nil {
		return invalid()
	}
	return nil
}

type AgentSourceReader interface {
	ReadMeetingToolSourceInTx(context.Context, f.Tx, tc.ToolCallBinding, tc.ToolCallPlan, bool) (AgentSource, error)
}

// AgentInvocationAuthority must bind the live private operation/attempt,
// current process, actor/P/Agent/E/handler and exact protected input. Discovery
// is only lock planning; the input loan rechecks after the full lock union.
type AgentInvocationAuthority interface {
	RequireMeetingToolBindingInTx(context.Context, f.Tx, any) error
	DiscoverMeetingTool(context.Context, AgentToolCall, bool) ([]f.LockRequest, error)
	WithMeetingToolInputInTx(context.Context, f.Tx, AgentToolCall, bool, func(context.Context, []byte, AgentSource) error) error
}
type AgentParticipant struct {
	ID          i.AgentID `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
}
type AgentParticipantAuthority interface {
	ReadMeetingParticipantInTx(context.Context, f.Tx, AgentToolCall, i.AgentID) (AgentParticipant, error)
}

type AgentInput struct {
	MeetingID       *MeetingID  `json:"meeting_id,omitempty"`
	ExpectedVersion *f.Version  `json:"expected_version,omitempty"`
	Status          string      `json:"status,omitempty"`
	Limit           int         `json:"limit,omitempty"`
	ProposalContent string      `json:"proposal_content,omitempty"`
	Agents          []i.AgentID `json:"agents,omitempty"`
	AgentID         *i.AgentID  `json:"agent_id,omitempty"`
}

func (v AgentInput) Validate(handler string) error {
	if v.MeetingID != nil && v.MeetingID.Validate() != nil || v.ExpectedVersion != nil && v.ExpectedVersion.Validate() != nil || v.AgentID != nil && v.AgentID.Validate() != nil {
		return invalid()
	}
	switch handler {
	case "meeting.list":
		if v.Status != "" && v.Status != "active" && v.Status != "proposed" && v.Status != "archive" || v.Limit < 0 || v.Limit > 100 || v.MeetingID != nil || v.ExpectedVersion != nil || v.ProposalContent != "" || len(v.Agents) > 0 || v.AgentID != nil {
			return invalid()
		}
	case "meeting.request":
		if v.MeetingID != nil || v.ExpectedVersion != nil || v.Status != "" || v.Limit != 0 || v.AgentID != nil || !ValidText(v.ProposalContent, 32768) || len(v.ProposalContent) == 0 || len(v.Agents) > MaxParticipants {
			return invalid()
		}
		seen := map[i.AgentID]bool{}
		for _, id := range v.Agents {
			if id.Validate() != nil || seen[id] {
				return invalid()
			}
			seen[id] = true
		}
	case "meeting.add-agent", "meeting.remove-agent":
		if v.MeetingID == nil || v.ExpectedVersion == nil || v.AgentID == nil || v.Status != "" || v.Limit != 0 || v.ProposalContent != "" || len(v.Agents) > 0 {
			return invalid()
		}
	default:
		return invalid()
	}
	return nil
}

type AgentResult struct {
	Receipt  *Receipt  `json:"receipt,omitempty"`
	Meetings []Session `json:"meetings,omitzero"`
}
