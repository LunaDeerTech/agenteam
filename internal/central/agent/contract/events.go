package contract

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"slices"

	ec "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

const (
	AgentProducer           ec.StableName = "agent"
	AgentAggregate          ec.StableName = "agent.config"
	AgentConfigChangedEvent ec.StableName = "agent.config_changed"
	AgentEventSchemaVersion uint32        = 1
)

type ConfigOperation string

const (
	ConfigCreated ConfigOperation = "created"
	ConfigUpdated ConfigOperation = "updated"
)

var editableFields = []string{"allowed_mount_ids", "allowed_secret_variable_ids", "allowed_tool_ids", "approval_model_ref", "approval_policy", "description", "display_name", "inject_agents_md", "instructions", "model_ref", "name", "reasoning_effort", "tag_color"}

func EditableFields() []string { return slices.Clone(editableFields) }

type ConfigChangedPayload struct {
	CommandID     AgentCommandID  `json:"command_id"`
	ActorUserID   i.UserID        `json:"actor_user_id"`
	Operation     ConfigOperation `json:"operation"`
	ChangedFields []string        `json:"changed_fields"`
}

func (v ConfigChangedPayload) Validate() error {
	if v.CommandID.Validate() != nil || v.ActorUserID.Validate() != nil || v.Operation != ConfigCreated && v.Operation != ConfigUpdated || len(v.ChangedFields) == 0 || len(v.ChangedFields) > len(editableFields) {
		return invalid("", "INVALID_AGENT_EVENT")
	}
	for n, name := range v.ChangedFields {
		if !slices.Contains(editableFields, name) || n > 0 && v.ChangedFields[n-1] >= name {
			return invalid("", "INVALID_AGENT_EVENT")
		}
	}
	if v.Operation == ConfigCreated && !slices.Equal(v.ChangedFields, editableFields) {
		return invalid("", "INVALID_AGENT_EVENT")
	}
	return nil
}
func (v ConfigChangedPayload) Clone() ConfigChangedPayload {
	v.ChangedFields = slices.Clone(v.ChangedFields)
	return v
}
func (ConfigChangedPayload) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "agent_config_changed")
}
func (ConfigChangedPayload) LogValue() slog.Value { return slog.StringValue("agent_config_changed") }
func (v ConfigChangedPayload) MarshalJSON() ([]byte, error) {
	type wire ConfigChangedPayload
	return marshalChecked(wire(v), v.Validate(), 16<<10)
}
func (v *ConfigChangedPayload) UnmarshalJSON(raw []byte) error {
	if v == nil {
		return invalid("", "INVALID_ENCODING")
	}
	_, err := decodeFields(raw, 16<<10, []string{"command_id", "actor_user_id", "operation", "changed_fields"}, nil, nil)
	if err != nil {
		return err
	}
	type wire ConfigChangedPayload
	var d wire
	if json.Unmarshal(raw, &d) != nil {
		return invalid("", "INVALID_ENCODING")
	}
	next := ConfigChangedPayload(d)
	if err := next.Validate(); err != nil {
		return err
	}
	*v = next.Clone()
	return nil
}

type AgentEvents struct {
	catalog *ec.Catalog
	changed ec.EventType[ConfigChangedPayload]
}

func RegisterAgentEvents(catalog *ec.Catalog) (AgentEvents, error) {
	if !catalog.Valid() {
		return AgentEvents{}, invalid("", "INVALID_CATALOG")
	}
	for _, s := range catalog.Schemas() {
		if s.EventType == AgentConfigChangedEvent && s.Version == AgentEventSchemaVersion {
			return AgentEvents{}, invalid("", "DUPLICATE_SCHEMA")
		}
	}
	t, err := ec.DefineEvent(catalog, ec.Definition[ConfigChangedPayload]{Schema: ec.Schema{Producer: AgentProducer, EventType: AgentConfigChangedEvent, AggregateType: AgentAggregate, Version: AgentEventSchemaVersion}, Codec: ec.JSONCodec[ConfigChangedPayload]{}, Validate: ConfigChangedPayload.Validate})
	if err != nil {
		return AgentEvents{}, err
	}
	return AgentEvents{catalog, t}, nil
}
func (v AgentEvents) Valid() bool { return v.catalog != nil && v.catalog.Valid() }
func ValidateAgentEventHeader(h ec.Header) error {
	if h.Validate() != nil || h.SchemaVersion != AgentEventSchemaVersion || h.EventType != AgentConfigChangedEvent || h.AggregateType != AgentAggregate || h.Scope.Kind != ec.ProjectScope || h.AggregateVersion == nil || h.AggregateSequence != nil {
		return invalid("", "INVALID_AGENT_EVENT_HEADER")
	}
	return nil
}
func (v AgentEvents) NewConfigChanged(h ec.Header, p ConfigChangedPayload) (ec.Event, error) {
	if !v.Valid() {
		return ec.Event{}, invalid("", "INVALID_CATALOG")
	}
	if err := ValidateAgentEventHeader(h); err != nil {
		return ec.Event{}, err
	}
	if p.Validate() != nil || (p.Operation == ConfigCreated) != (*h.AggregateVersion == 1) {
		return ec.Event{}, invalid("", "INVALID_AGENT_EVENT")
	}
	return ec.NewEvent(v.changed, h, p)
}
func (v AgentEvents) DecodeConfigChanged(e ec.Event) (ConfigChangedPayload, error) {
	if !v.Valid() || !v.catalog.Owns(e) || e.Summary().Producer != AgentProducer || ValidateAgentEventHeader(e.Header()) != nil {
		return ConfigChangedPayload{}, invalid("", "FOREIGN_EVENT")
	}
	p, err := ec.DecodeEvent(v.changed, e)
	if err != nil {
		return ConfigChangedPayload{}, err
	}
	if (p.Operation == ConfigCreated) != (*e.Header().AggregateVersion == 1) {
		return ConfigChangedPayload{}, invalid("", "INVALID_AGENT_EVENT")
	}
	return p.Clone(), nil
}
