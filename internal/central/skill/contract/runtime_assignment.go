package contract

import (
	"bytes"
	"encoding/json"
	"io"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

// The target is independent of the calling Agent. Management metadata never
// supplies a body-read binding or changes the current round's SkillsInput.
type AssignmentToolArguments struct {
	Action            string        `json:"action"`
	AgentID           i.AgentID     `json:"agent_id"`
	ExpectedVersion   f.Version     `json:"expected_version"`
	SkillID           *SkillID      `json:"skill_id,omitempty"`
	AssignmentID      *AssignmentID `json:"assignment_id,omitempty"`
	Enabled           *bool         `json:"enabled,omitempty"`
	AfterSkillID      *SkillID      `json:"after_skill_id,omitempty"`
	AfterAgentID      *i.AgentID    `json:"after_agent_id,omitempty"`
	AfterAgentCreated *f.Instant    `json:"after_agent_created_at,omitempty"`
	Limit             int           `json:"limit,omitempty"`
}

func DecodeAssignmentToolArguments(raw []byte) (AssignmentToolArguments, error) {
	var a AssignmentToolArguments
	if len(raw) < 2 || len(raw) > 8192 {
		return a, invalid()
	}
	// Reject duplicate fields independently of JSON's last-value semantics.
	d := json.NewDecoder(bytes.NewReader(raw))
	first, e := d.Token()
	if e != nil || first != json.Delim('{') {
		return a, invalid()
	}
	seen := map[string]bool{}
	for d.More() {
		v, e := d.Token()
		k, ok := v.(string)
		if e != nil || !ok || seen[k] {
			return a, invalid()
		}
		seen[k] = true
		var value json.RawMessage
		if d.Decode(&value) != nil || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return a, invalid()
		}
	}
	if last, e := d.Token(); e != nil || last != json.Delim('}') || d.Decode(new(any)) != io.EOF {
		return a, invalid()
	}
	d = json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(&a) != nil {
		return a, invalid()
	}
	if a.Action == "catalog" {
		if seen["expected_version"] || a.SkillID != nil || a.AssignmentID != nil || a.Enabled != nil ||
			seen["agent_id"] && a.AgentID.Validate() != nil || a.AfterSkillID != nil && a.AfterSkillID.Validate() != nil || a.AfterAgentID != nil && a.AfterAgentID.Validate() != nil || a.AfterAgentCreated != nil && a.AfterAgentCreated.Validate() != nil ||
			(a.AfterAgentID == nil) != (a.AfterAgentCreated == nil) || seen["agent_id"] && a.AfterAgentID != nil || a.Limit < 0 || a.Limit > 50 {
			return a, invalid()
		}
		if a.Limit == 0 {
			a.Limit = 20
		}
		return a, nil
	}
	if a.AgentID.Validate() != nil || a.ExpectedVersion.Validate() != nil || a.AfterSkillID != nil || a.AfterAgentID != nil || a.AfterAgentCreated != nil || seen["limit"] {
		return a, invalid()
	}
	switch a.Action {
	case "assign":
		if a.SkillID == nil || a.SkillID.Validate() != nil || a.Enabled == nil || a.AssignmentID != nil {
			return a, invalid()
		}
	case "remove":
		if a.AssignmentID == nil || a.AssignmentID.Validate() != nil || a.SkillID != nil || a.Enabled != nil {
			return a, invalid()
		}
	default:
		return a, invalid()
	}
	return a, nil
}
