package contract

import (
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

type ControlRequest struct {
	ProjectID       i.ProjectID     `json:"project_id"`
	MeetingID       MeetingID       `json:"meeting_id"`
	Action          string          `json:"action"`
	TurnID          *TurnID         `json:"turn_id,omitempty"`
	ContributionID  *ContributionID `json:"contribution_id,omitempty"`
	ExpectedVersion f.Version       `json:"expected_version"`
	Meta            f.CommandMeta   `json:"-"`
}

func (r ControlRequest) Validate() error {
	if r.ProjectID.Validate() != nil || r.MeetingID.Validate() != nil || r.ExpectedVersion.Validate() != nil || r.Meta.Validate() != nil || r.Meta.ExpectedVersion != nil {
		return invalid()
	}
	if r.TurnID != nil && r.TurnID.Validate() != nil || r.ContributionID != nil && r.ContributionID.Validate() != nil {
		return invalid()
	}
	switch r.Action {
	case "archive", "unarchive":
		if r.TurnID != nil || r.ContributionID != nil {
			return invalid()
		}
	case "cancel-turn", "retry-summary":
		if r.TurnID == nil || r.ContributionID != nil {
			return invalid()
		}
	case "skip-contribution", "cancel-contribution", "retry-contribution", "regenerate-contribution":
		if r.TurnID == nil || r.ContributionID == nil {
			return invalid()
		}
	default:
		return invalid()
	}
	return nil
}
