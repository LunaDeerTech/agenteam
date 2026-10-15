package contract

import (
	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

// ContributionExecution is a current source-bound projection of Execution
// facts. It grants no permission to resume, approve or cancel an Execution.
type ContributionExecution struct {
	ID                i.ExecutionID  `json:"id"`
	ProjectID         i.ProjectID    `json:"project_id"`
	MeetingID         MeetingID      `json:"meeting_id"`
	TurnID            TurnID         `json:"turn_id"`
	ContributionID    ContributionID `json:"contribution_id"`
	ParticipantID     ParticipantID  `json:"participant_id"`
	AgentID           i.AgentID      `json:"agent_id"`
	Generation        f.Version      `json:"generation"`
	Attempt           f.Version      `json:"attempt"`
	Status            ec.Status      `json:"status"`
	Version           f.Version      `json:"version"`
	WaitingReason     *string        `json:"waiting_reason"`
	WaitingReference  *string        `json:"waiting_reference"`
	CancelRequestedAt *f.Instant     `json:"cancel_requested_at"`
	CreatedAt         f.Instant      `json:"created_at"`
	StartedAt         *f.Instant     `json:"started_at"`
	CompletedAt       *f.Instant     `json:"completed_at"`
}

func (v ContributionExecution) Validate() error {
	if v.ID.Validate() != nil || v.ProjectID.Validate() != nil || v.MeetingID.Validate() != nil || v.TurnID.Validate() != nil || v.ContributionID.Validate() != nil || v.ParticipantID.Validate() != nil || v.AgentID.Validate() != nil || v.Generation.Validate() != nil || v.Attempt.Validate() != nil || v.Version.Validate() != nil || !v.Status.Valid() || v.CreatedAt.Validate() != nil {
		return invalid()
	}
	for _, at := range []*f.Instant{v.StartedAt, v.CompletedAt, v.CancelRequestedAt} {
		if at != nil && (at.Validate() != nil || at.Time().Before(v.CreatedAt.Time())) {
			return invalid()
		}
	}
	if v.Status.Terminal() != (v.CompletedAt != nil) || v.CompletedAt != nil && v.StartedAt != nil && v.CompletedAt.Time().Before(v.StartedAt.Time()) {
		return invalid()
	}
	if (v.Status == ec.Running || v.Status == ec.Waiting || v.Status == ec.Succeeded) && v.StartedAt == nil {
		return invalid()
	}
	if v.Status == ec.Waiting {
		if v.WaitingReason == nil || (*v.WaitingReason != "approval" && *v.WaitingReason != "decision") || v.WaitingReference == nil {
			return invalid()
		}
		if _, err := f.ParseID[struct{}](*v.WaitingReference); err != nil {
			return invalid()
		}
	} else if v.WaitingReason != nil || v.WaitingReference != nil {
		return invalid()
	}
	return nil
}
