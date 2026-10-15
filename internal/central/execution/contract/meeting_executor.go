package contract

import (
	"context"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

// AssociatedMeeting names a committed launch association owned by Meeting.
// Every field is checked against Execution's original launch before work starts.
type AssociatedMeeting struct {
	ExecutionID i.ExecutionID
	AgentID     i.AgentID
	Key         f.IdempotencyKey
	Digest      f.Digest
	Trigger     Trigger
	Generation  f.Version
	Attempt     f.Version
}

func (v AssociatedMeeting) Validate() error {
	if v.ExecutionID.Validate() != nil || v.AgentID.Validate() != nil || v.Key.Validate() != nil || v.Digest.Validate() != nil || v.Trigger.Validate("meeting/response") != nil || v.Generation.Validate() != nil || v.Attempt.Validate() != nil {
		return invalid()
	}
	return nil
}

type MeetingAssociatedExecutor interface {
	AdvanceMeeting(context.Context, i.ProjectID, AssociatedMeeting) (ExecutionAdvance, error)
}

// Meeting owns the immutable launch intent. Project independently applies its
// current gate; this callback alone never grants launch or historical access.
type MeetingIntentAuthority interface {
	RequireMeetingIntentInTx(context.Context, f.Tx, i.Actor, i.ProjectID, i.AgentID, i.AccessIntent) error
}
