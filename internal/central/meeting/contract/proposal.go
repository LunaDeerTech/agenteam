package contract

import (
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

type ProposalOrigin struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
}

func (v ProposalOrigin) Validate() error {
	if v.Kind != "task" && v.Kind != "meeting" {
		return invalid()
	}
	if _, err := f.ParseID[struct{}](v.ID); err != nil {
		return invalid()
	}
	return nil
}

type ProposalView struct {
	Session                Session         `json:"session"`
	SourceExecutionID      i.ExecutionID   `json:"source_execution_id"`
	Origin                 *ProposalOrigin `json:"origin,omitempty"`
	CreatedByParticipantID ParticipantID   `json:"created_by_participant_id"`
}
type ReviewProposalRequest struct {
	ProjectID       i.ProjectID   `json:"project_id"`
	MeetingID       MeetingID     `json:"meeting_id"`
	ExpectedVersion f.Version     `json:"expected_version"`
	Approve         bool          `json:"approve"`
	Meta            f.CommandMeta `json:"-"`
}

func (v ReviewProposalRequest) Validate() error {
	if v.ProjectID.Validate() != nil || v.MeetingID.Validate() != nil || v.ExpectedVersion.Validate() != nil || v.Meta.Validate() != nil || v.Meta.ExpectedVersion != nil {
		return invalid()
	}
	return nil
}
