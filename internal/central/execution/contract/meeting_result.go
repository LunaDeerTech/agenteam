package contract

import (
	"context"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

type MeetingExecutionResult struct {
	Summary          Summary
	FinalText        *string
	WaitingReason    *string
	WaitingReference *string
}
type MeetingResultReader interface {
	ReadMeetingExecutionInTx(context.Context, f.Tx, i.ProjectID, AssociatedMeeting) (MeetingExecutionResult, error)
}
type MeetingResultAuthority interface {
	RequireBindingInTx(context.Context, f.Tx, any) error
	RequireMeetingResultInTx(context.Context, f.Tx, i.ProjectID, AssociatedMeeting) error
}

func MeetingExecutionReadLocks(project i.ProjectID, m AssociatedMeeting) ([]f.LockRequest, error) {
	if project.Validate() != nil || m.Validate() != nil {
		return nil, invalid()
	}
	p, _ := f.ProjectLock(project.String())
	a, _ := f.AgentLock(m.AgentID.String())
	e, _ := f.AggregateLock(f.ExecutionAggregate, m.ExecutionID.String())
	return []f.LockRequest{{Key: p, Mode: f.Shared}, {Key: a, Mode: f.Shared}, {Key: e, Mode: f.Exclusive}}, nil
}
