package contract

import (
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

// UpdateParticipantsRequest replaces the active Agent roster in explicit order.
// User identity and already-created Turn membership remain immutable.
type UpdateParticipantsRequest struct {
	ProjectID       i.ProjectID   `json:"project_id"`
	MeetingID       MeetingID     `json:"meeting_id"`
	ExpectedVersion f.Version     `json:"expected_version"`
	Agents          []i.AgentID   `json:"agents"`
	Meta            f.CommandMeta `json:"-"`
}

func (r UpdateParticipantsRequest) Validate() error {
	if r.ProjectID.Validate() != nil || r.MeetingID.Validate() != nil || r.ExpectedVersion.Validate() != nil || r.Meta.Validate() != nil || r.Meta.ExpectedVersion != nil || r.Agents == nil || len(r.Agents) > MaxParticipants {
		return invalid()
	}
	seen := map[i.AgentID]bool{}
	for _, agent := range r.Agents {
		if agent.Validate() != nil || seen[agent] {
			return invalid()
		}
		seen[agent] = true
	}
	return nil
}
