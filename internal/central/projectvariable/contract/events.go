package contract

import (
	"encoding/json"
	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	"slices"
)

const (
	VariableProducer      event.StableName = "projectvariable"
	VariableChangedName   event.StableName = "project.variable_changed"
	VariableAggregate     event.StableName = "project.variable"
	VariableSchemaVersion uint32           = 1
)

type Change string

const (
	Created Change = "created"
	Updated Change = "updated"
	Deleted Change = "deleted"
)

type VariableChanged struct {
	VariableID    VariableID  `json:"variable_id"`
	OperationID   OperationID `json:"operation_id"`
	Change        Change      `json:"change"`
	ChangedFields []string    `json:"changed_fields"`
}

func ValidateChangedFields(change Change, fields []string) error {
	switch change {
	case Created:
		if slices.Equal(fields, []string{"created"}) {
			return nil
		}
	case Deleted:
		if slices.Equal(fields, []string{"deleted"}) {
			return nil
		}
	case Updated:
		if len(fields) < 1 || len(fields) > 3 {
			break
		}
		for n, field := range fields {
			if field != "name" && field != "description" && field != "value" || n > 0 && fields[n-1] >= field {
				return invalid("/changed_fields", "INVALID_CHANGE")
			}
		}
		return nil
	}
	return invalid("/changed_fields", "INVALID_CHANGE")
}
func (v VariableChanged) Validate() error {
	if v.VariableID.Validate() != nil || v.OperationID.Validate() != nil {
		return invalid("", "INVALID_EVENT")
	}
	return ValidateChangedFields(v.Change, v.ChangedFields)
}
func (v VariableChanged) Clone() VariableChanged {
	v.ChangedFields = slices.Clone(v.ChangedFields)
	return v
}
func (v VariableChanged) MarshalJSON() ([]byte, error) {
	if err := v.Validate(); err != nil {
		return nil, err
	}
	type wire VariableChanged
	return json.Marshal(wire(v))
}
func (v *VariableChanged) UnmarshalJSON(raw []byte) error {
	if v == nil {
		return invalid("", "INVALID_ENCODING")
	}
	type wire VariableChanged
	n, err := decode[wire](raw, []string{"variable_id", "operation_id", "change", "changed_fields"}, nil, nil, 8192)
	if err != nil {
		return err
	}
	next := VariableChanged(n)
	if err = next.Validate(); err == nil {
		*v = next
	}
	return err
}

type VariableEvents struct {
	catalog *event.Catalog
	changed event.EventType[VariableChanged]
}

func RegisterVariableEvents(catalog *event.Catalog) (VariableEvents, error) {
	v, err := event.DefineEvent(catalog, event.Definition[VariableChanged]{Schema: event.Schema{Producer: VariableProducer, EventType: VariableChangedName, AggregateType: VariableAggregate, Version: VariableSchemaVersion}, Codec: event.JSONCodec[VariableChanged]{}, Validate: VariableChanged.Validate})
	if err != nil {
		return VariableEvents{}, err
	}
	return VariableEvents{catalog, v}, nil
}
func (v VariableEvents) Valid() bool {
	return v.catalog != nil && v.catalog.Valid() && v.changed.Schema().EventType == VariableChangedName
}
func (v VariableEvents) NewVariableChanged(h event.Header, p VariableChanged) (event.Event, error) {
	if !v.Valid() || h.Validate() != nil || h.Scope.Kind != event.ProjectScope || h.AggregateVersion == nil || h.AggregateSequence != nil || h.EventType != VariableChangedName || h.AggregateType != VariableAggregate || h.SchemaVersion != VariableSchemaVersion || h.AggregateID.String() != p.VariableID.String() || p.Validate() != nil || p.Change == Created && *h.AggregateVersion != 1 || p.Change != Created && *h.AggregateVersion < 2 {
		return event.Event{}, invalid("", "INVALID_EVENT")
	}
	return event.NewEvent(v.changed, h, p)
}
func (v VariableEvents) Restore(h event.Header, raw []byte) (event.Event, error) {
	var p VariableChanged
	if err := p.UnmarshalJSON(raw); err != nil {
		return event.Event{}, err
	}
	return v.NewVariableChanged(h, p)
}
func (v VariableEvents) Decode(e event.Event) (VariableChanged, error) {
	p, err := event.DecodeEvent(v.changed, e)
	if err != nil {
		return VariableChanged{}, err
	}
	if _, err = v.NewVariableChanged(e.Header(), p); err != nil {
		return VariableChanged{}, err
	}
	return p, nil
}
