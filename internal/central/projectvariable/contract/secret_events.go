package contract

import (
	"encoding/json"
	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	"slices"
)

const (
	SecretVariableProducer      event.StableName = "projectvariable"
	SecretVariableChangedName   event.StableName = "project.secret_variable_changed"
	SecretVariableAggregate     event.StableName = "project.variable"
	SecretVariableSchemaVersion uint32           = 1
)

type SecretVariableChanged struct {
	VariableID    VariableID  `json:"variable_id"`
	OperationID   OperationID `json:"operation_id"`
	Change        Change      `json:"change"`
	ChangedFields []string    `json:"changed_fields"`
}

func (v SecretVariableChanged) Validate() error {
	if v.VariableID.Validate() != nil || v.OperationID.Validate() != nil {
		return invalid("", "INVALID_EVENT")
	}
	return ValidateChangedFields(v.Change, v.ChangedFields)
}
func (v SecretVariableChanged) Clone() SecretVariableChanged {
	v.ChangedFields = slices.Clone(v.ChangedFields)
	return v
}
func (v SecretVariableChanged) MarshalJSON() ([]byte, error) {
	if err := v.Validate(); err != nil {
		return nil, err
	}
	type wire SecretVariableChanged
	return json.Marshal(wire(v))
}
func (v *SecretVariableChanged) UnmarshalJSON(raw []byte) error {
	if v == nil {
		return invalid("", "INVALID_ENCODING")
	}
	type wire SecretVariableChanged
	n, err := decodeSecret[wire](raw, []string{"variable_id", "operation_id", "change", "changed_fields"}, nil, nil, 8192)
	if err != nil {
		return err
	}
	next := SecretVariableChanged(n)
	if err = next.Validate(); err == nil {
		*v = next
	}
	return err
}

type SecretVariableEvents struct {
	catalog *event.Catalog
	changed event.EventType[SecretVariableChanged]
}

func RegisterSecretVariableEvents(catalog *event.Catalog) (SecretVariableEvents, error) {
	v, err := event.DefineEvent(catalog, event.Definition[SecretVariableChanged]{Schema: event.Schema{Producer: SecretVariableProducer, EventType: SecretVariableChangedName, AggregateType: SecretVariableAggregate, Version: SecretVariableSchemaVersion}, Codec: event.JSONCodec[SecretVariableChanged]{}, Validate: SecretVariableChanged.Validate})
	if err != nil {
		return SecretVariableEvents{}, err
	}
	return SecretVariableEvents{catalog, v}, nil
}
func (v SecretVariableEvents) Valid() bool {
	return v.catalog != nil && v.catalog.Valid() && v.changed.Schema().EventType == SecretVariableChangedName
}
func (v SecretVariableEvents) NewSecretVariableChanged(h event.Header, p SecretVariableChanged) (event.Event, error) {
	if !v.Valid() || h.Validate() != nil || h.Scope.Kind != event.ProjectScope || h.AggregateVersion == nil || h.AggregateSequence != nil || h.EventType != SecretVariableChangedName || h.AggregateType != SecretVariableAggregate || h.SchemaVersion != SecretVariableSchemaVersion || h.AggregateID.String() != p.VariableID.String() || p.Validate() != nil || p.Change == Created && *h.AggregateVersion != 1 || p.Change != Created && *h.AggregateVersion < 2 {
		return event.Event{}, invalid("", "INVALID_EVENT")
	}
	return event.NewEvent(v.changed, h, p)
}
func (v SecretVariableEvents) Restore(h event.Header, raw []byte) (event.Event, error) {
	var p SecretVariableChanged
	if err := p.UnmarshalJSON(raw); err != nil {
		return event.Event{}, err
	}
	return v.NewSecretVariableChanged(h, p)
}
func (v SecretVariableEvents) Decode(e event.Event) (SecretVariableChanged, error) {
	p, err := event.DecodeEvent(v.changed, e)
	if err != nil {
		return SecretVariableChanged{}, err
	}
	if _, err = v.NewSecretVariableChanged(e.Header(), p); err != nil {
		return SecretVariableChanged{}, err
	}
	return p, nil
}
