package contract

import (
	"bytes"
	"encoding/json"
	"io"
	"slices"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

const (
	AgentCreate   Action   = "agent.create"
	AgentUpdate   Action   = "agent.update"
	AgentProducer Producer = "agent"
)

func AgentAction(action Action) bool { return action == AgentCreate || action == AgentUpdate }

// This lower-layer closed projection contains only field names, never values,
// prompts, capability references, command keys or the canonical Agent config.
var agentChangedFields = []string{"allowed_mount_ids", "allowed_secret_variable_ids", "allowed_tool_ids", "approval_model_ref", "approval_policy", "description", "display_name", "inject_agents_md", "instructions", "model_ref", "name", "reasoning_effort", "tag_color"}

type AgentMetadataFields struct {
	AgentID       string    `json:"agent_id"`
	Version       f.Version `json:"version"`
	CommandID     string    `json:"command_id"`
	ChangedFields []string  `json:"changed_fields"`
}

func AgentMetadata(action Action, value AgentMetadataFields) (Metadata, error) {
	if !AgentAction(action) || !validID(value.AgentID) || !validID(value.CommandID) || value.Version.Validate() != nil || len(value.ChangedFields) == 0 || len(value.ChangedFields) > len(agentChangedFields) {
		return Metadata{}, invalid("metadata")
	}
	for n, field := range value.ChangedFields {
		if !slices.Contains(agentChangedFields, field) || n > 0 && value.ChangedFields[n-1] >= field {
			return Metadata{}, invalid("metadata")
		}
	}
	if action == AgentCreate && (value.Version != 1 || !slices.Equal(value.ChangedFields, agentChangedFields)) || action == AgentUpdate && value.Version <= 1 {
		return Metadata{}, invalid("metadata")
	}
	value.ChangedFields = slices.Clone(value.ChangedFields)
	raw, err := json.Marshal(value)
	if err != nil || len(raw) > 4096 {
		return Metadata{}, invalid("metadata")
	}
	d := metadataData{action: action, raw: string(raw)}
	return Metadata{data: func() metadataData { return d }}, nil
}
func (m Metadata) AgentFields() (AgentMetadataFields, error) {
	var value AgentMetadataFields
	if m.data == nil || !AgentAction(m.data().action) || json.Unmarshal([]byte(m.data().raw), &value) != nil {
		return AgentMetadataFields{}, invalid("metadata")
	}
	return value, nil
}
func decodeAgentMetadata(action Action, raw []byte) (Metadata, error) {
	d := json.NewDecoder(bytes.NewReader(raw))
	first, err := d.Token()
	if err != nil || first != json.Delim('{') {
		return Metadata{}, invalid("metadata")
	}
	seen := make(map[string]bool, 4)
	for d.More() {
		key, err := d.Token()
		name, ok := key.(string)
		if err != nil || !ok || seen[name] {
			return Metadata{}, invalid("metadata")
		}
		switch name {
		case "agent_id", "version", "command_id", "changed_fields":
		default:
			return Metadata{}, invalid("metadata")
		}
		seen[name] = true
		var value json.RawMessage
		if d.Decode(&value) != nil || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return Metadata{}, invalid("metadata")
		}
	}
	last, err := d.Token()
	if err != nil || last != json.Delim('}') || len(seen) != 4 || d.Decode(new(any)) != io.EOF {
		return Metadata{}, invalid("metadata")
	}
	var value AgentMetadataFields
	if json.Unmarshal(raw, &value) != nil {
		return Metadata{}, invalid("metadata")
	}
	return AgentMetadata(action, value)
}
func validateAgentEntry(value EntryFields) error {
	m, err := value.Metadata.AgentFields()
	if err != nil {
		return err
	}
	if value.Actor.Details().Kind != id.Human || value.Scope.Details().Kind != id.ProjectScope || value.Outcome != Success || value.Resource.Details().Kind != AgentResource || value.Resource.Details().ID != m.AgentID || value.Associations != (Associations{}) {
		return invalid("entry")
	}
	return nil
}
