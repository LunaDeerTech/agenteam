package contract

import (
	"bytes"
	"encoding/json"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"io"
)

const (
	SkillAssign   Action   = "skill.assign"
	SkillRemove   Action   = "skill.remove"
	SkillProducer Producer = "skill"
)

func SkillAssignmentAction(a Action) bool { return a == SkillAssign || a == SkillRemove }

type SkillAssignmentMetadataFields struct {
	AgentID      string    `json:"agent_id"`
	AssignmentID string    `json:"assignment_id"`
	SkillID      string    `json:"skill_id"`
	ChangeID     string    `json:"change_id"`
	Version      f.Version `json:"version"`
	Sequence     f.Version `json:"assignment_sequence"`
	Enabled      bool      `json:"enabled"`
}

func SkillAssignmentMetadata(action Action, v SkillAssignmentMetadataFields) (Metadata, error) {
	if !SkillAssignmentAction(action) || !validID(v.AgentID) || !validID(v.AssignmentID) || !validID(v.SkillID) || !validID(v.ChangeID) || v.Version < 2 || v.Sequence < 2 || action == SkillRemove && v.Enabled {
		return Metadata{}, invalid("metadata")
	}
	raw, err := json.Marshal(v)
	if err != nil {
		return Metadata{}, invalid("metadata")
	}
	d := metadataData{action: action, raw: string(raw)}
	return Metadata{data: func() metadataData { return d }}, nil
}
func (m Metadata) SkillAssignmentFields() (SkillAssignmentMetadataFields, error) {
	var v SkillAssignmentMetadataFields
	if m.data == nil || !SkillAssignmentAction(m.data().action) || json.Unmarshal([]byte(m.data().raw), &v) != nil {
		return v, invalid("metadata")
	}
	return v, nil
}
func decodeSkillAssignmentMetadata(a Action, raw []byte) (Metadata, error) {
	d := json.NewDecoder(bytes.NewReader(raw))
	first, e := d.Token()
	if e != nil || first != json.Delim('{') {
		return Metadata{}, invalid("metadata")
	}
	seen := map[string]bool{}
	for d.More() {
		token, e := d.Token()
		name, ok := token.(string)
		if e != nil || !ok || seen[name] {
			return Metadata{}, invalid("metadata")
		}
		switch name {
		case "agent_id", "assignment_id", "skill_id", "change_id", "version", "assignment_sequence", "enabled":
		default:
			return Metadata{}, invalid("metadata")
		}
		seen[name] = true
		var v json.RawMessage
		if d.Decode(&v) != nil || bytes.Equal(bytes.TrimSpace(v), []byte("null")) {
			return Metadata{}, invalid("metadata")
		}
	}
	last, e := d.Token()
	if e != nil || last != json.Delim('}') || len(seen) != 7 || d.Decode(new(any)) != io.EOF {
		return Metadata{}, invalid("metadata")
	}
	var v SkillAssignmentMetadataFields
	if json.Unmarshal(raw, &v) != nil {
		return Metadata{}, invalid("metadata")
	}
	return SkillAssignmentMetadata(a, v)
}
func validateSkillAssignmentEntry(v EntryFields) error {
	m, e := v.Metadata.SkillAssignmentFields()
	if e != nil {
		return e
	}
	actor := v.Actor.Details()
	if v.Scope.Details().Kind != i.ProjectScope || v.Outcome != Success || v.Resource.Details().Kind != AgentResource || v.Resource.Details().ID != m.AgentID {
		return invalid("entry")
	}
	switch actor.Kind {
	case i.Human:
		if v.Associations != (Associations{}) {
			return invalid("entry")
		}
	case i.AgentRun:
		a := v.Associations
		if actor.ProjectID != v.Scope.Details().ProjectID || a.ExecutionID != actor.ExecutionID || !validID(a.ToolID) || !validID(a.ToolCallID) || !validID(a.OperationID) || a != (Associations{ExecutionID: actor.ExecutionID, ToolID: a.ToolID, ToolCallID: a.ToolCallID, OperationID: a.OperationID}) {
			return invalid("entry")
		}
	default:
		return invalid("entry")
	}
	return nil
}
