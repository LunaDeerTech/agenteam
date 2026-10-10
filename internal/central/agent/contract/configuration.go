package contract

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"slices"

	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

const MaxCapabilityReferences = 128
const MaxTotalCapabilityReferences = 256

// AgentConfig is the canonical configuration, not an execution snapshot or a
// capability grant. Assignment facts continue to belong to Skills.
type AgentConfigFields struct {
	Core                     AgentCore
	AllowedToolIDs           []i.ToolID
	AllowedMountIDs          []i.MountID
	AllowedSecretVariableIDs []i.ProjectVariableID
}

type AgentConfig struct{ data func() AgentConfigFields }

func cloneConfig(v AgentConfigFields) AgentConfigFields {
	v.Core = v.Core.Clone()
	v.AllowedToolIDs = slices.Clone(v.AllowedToolIDs)
	v.AllowedMountIDs = slices.Clone(v.AllowedMountIDs)
	v.AllowedSecretVariableIDs = slices.Clone(v.AllowedSecretVariableIDs)
	return v
}

type canonicalID interface {
	Validate() error
	String() string
}

func validReferences[T canonicalID](values []T) bool {
	if values == nil || len(values) > MaxCapabilityReferences {
		return false
	}
	for n, value := range values {
		if value.Validate() != nil || n > 0 && values[n-1].String() >= value.String() {
			return false
		}
	}
	return true
}

func validateReferences(tools []i.ToolID, mounts []i.MountID, variables []i.ProjectVariableID) error {
	if !validReferences(tools) || !validReferences(mounts) || !validReferences(variables) || len(tools)+len(mounts)+len(variables) > MaxTotalCapabilityReferences {
		return invalid("", "INVALID_CAPABILITY_REFERENCES")
	}
	return nil
}

func NewAgentConfig(v AgentConfigFields) (AgentConfig, error) {
	if err := v.Core.Validate(); err != nil {
		return AgentConfig{}, err
	}
	if err := validateReferences(v.AllowedToolIDs, v.AllowedMountIDs, v.AllowedSecretVariableIDs); err != nil {
		return AgentConfig{}, err
	}
	v = cloneConfig(v)
	return AgentConfig{func() AgentConfigFields { return cloneConfig(v) }}, nil
}

func (v AgentConfig) Validate() error {
	if v.data == nil {
		return invalid("", "INVALID_AGENT_CONFIG")
	}
	return nil
}
func (v AgentConfig) Fields() AgentConfigFields {
	if v.data == nil {
		return AgentConfigFields{}
	}
	return v.data()
}
func (v AgentConfig) Clone() AgentConfig { return v }

var coreKeys = []string{"id", "project_id", "name", "normalized_name", "display_name", "tag_color", "description", "instructions", "inject_agents_md", "model_ref", "reasoning_effort", "approval_policy", "approval_model_ref", "lifecycle", "version", "created_at", "updated_at"}
var configReferenceKeys = []string{"allowed_tool_ids", "allowed_mount_ids", "allowed_secret_variable_ids"}
var coreNullable = []string{"display_name", "tag_color", "reasoning_effort", "approval_model_ref"}

func (v AgentConfig) MarshalJSON() ([]byte, error) {
	if err := v.Validate(); err != nil {
		return nil, err
	}
	d := v.Fields()
	raw, err := json.Marshal(d.Core)
	if err != nil {
		return nil, err
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil {
		return nil, invalid("", "INVALID_ENCODING")
	}
	fields["allowed_tool_ids"], err = json.Marshal(d.AllowedToolIDs)
	if err != nil {
		return nil, err
	}
	fields["allowed_mount_ids"], err = json.Marshal(d.AllowedMountIDs)
	if err != nil {
		return nil, err
	}
	fields["allowed_secret_variable_ids"], err = json.Marshal(d.AllowedSecretVariableIDs)
	return marshalChecked(fields, err, MaxAgentCoreBytes)
}

func (v *AgentConfig) UnmarshalJSON(raw []byte) error {
	if v == nil {
		return invalid("", "INVALID_ENCODING")
	}
	keys := append(slices.Clone(coreKeys), configReferenceKeys...)
	fields, err := decodeFields(raw, MaxAgentCoreBytes, keys, nil, coreNullable)
	if err != nil {
		return err
	}
	var d AgentConfigFields
	if json.Unmarshal(fields[configReferenceKeys[0]], &d.AllowedToolIDs) != nil || json.Unmarshal(fields[configReferenceKeys[1]], &d.AllowedMountIDs) != nil || json.Unmarshal(fields[configReferenceKeys[2]], &d.AllowedSecretVariableIDs) != nil {
		return invalid("", "INVALID_CAPABILITY_REFERENCES")
	}
	for _, key := range configReferenceKeys {
		delete(fields, key)
	}
	core, err := json.Marshal(fields)
	if err != nil || json.Unmarshal(core, &d.Core) != nil {
		return invalid("", "INVALID_AGENT_CORE")
	}
	next, err := NewAgentConfig(d)
	if err == nil {
		*v = next
	}
	return err
}

func DecodeAgentConfig(raw []byte) (AgentConfig, error) {
	var value AgentConfig
	err := value.UnmarshalJSON(raw)
	return value, err
}

// decodeFields also serves the presence-sensitive command codecs. It checks
// the complete original raw, spelling, duplicates and null before decoding.
func decodeFields(raw []byte, limit int, required, optional, nullable []string) (map[string]json.RawMessage, error) {
	if !validRaw(raw, limit) {
		return nil, invalid("", "INVALID_ENCODING")
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	first, err := d.Token()
	if err != nil || first != json.Delim('{') {
		return nil, invalid("", "INVALID_ENCODING")
	}
	out := make(map[string]json.RawMessage, len(required)+len(optional))
	for d.More() {
		token, err := d.Token()
		key, ok := token.(string)
		if err != nil || !ok || out[key] != nil || !slices.Contains(required, key) && !slices.Contains(optional, key) {
			return nil, invalid("", "INVALID_ENCODING")
		}
		var value json.RawMessage
		if d.Decode(&value) != nil {
			return nil, invalid("", "INVALID_ENCODING")
		}
		if bytes.Equal(bytes.TrimSpace(value), []byte("null")) && !slices.Contains(nullable, key) {
			return nil, invalid("/"+key, "NULL_NOT_ALLOWED")
		}
		out[key] = value
	}
	end, err := d.Token()
	if err != nil || end != json.Delim('}') || d.Decode(new(json.RawMessage)) != io.EOF {
		return nil, invalid("", "INVALID_ENCODING")
	}
	for _, key := range required {
		if out[key] == nil {
			return nil, invalid("/"+key, "REQUIRED")
		}
	}
	return out, nil
}

func (AgentConfig) Format(w fmt.State, _ rune) { _, _ = io.WriteString(w, "agent_config") }
func (AgentConfig) LogValue() slog.Value       { return slog.StringValue("agent_config") }
