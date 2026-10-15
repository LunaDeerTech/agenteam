package contract

import (
	"context"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

// Meeting controls borrow the source domain's original live transaction and
// private witness. A trigger or association alone never authorizes a control.
type MeetingControlAuthority interface {
	RequireMeetingControlBindingInTx(context.Context, f.Tx, any) error
	RequireMeetingSlotInTx(context.Context, f.Tx, i.ProjectID, i.AgentID, Trigger) error
	RequireMeetingCancellationInTx(context.Context, f.Tx, i.ProjectID, AssociatedMeeting) error
	RequireMeetingCancellationNotificationInTx(context.Context, f.Tx, i.ProjectID, AssociatedMeeting) error
}
type MeetingControl interface {
	ReadMeetingAgentSlotInTx(context.Context, f.Tx, i.ProjectID, i.AgentID, Trigger) (AgentSlotObservation, error)
	RequestMeetingCancelInTx(context.Context, f.Tx, i.ProjectID, AssociatedMeeting) (Summary, error)
	NotifyMeetingCancellation(context.Context, i.ProjectID, AssociatedMeeting) error
}

func MeetingAgentSlotLocks(project i.ProjectID, agent i.AgentID) ([]f.LockRequest, error) {
	if project.Validate() != nil || agent.Validate() != nil {
		return nil, invalid()
	}
	p, _ := f.ProjectLock(project.String())
	a, _ := f.AgentLock(agent.String())
	return []f.LockRequest{{Key: p, Mode: f.Shared}, {Key: a, Mode: f.Shared}}, nil
}
