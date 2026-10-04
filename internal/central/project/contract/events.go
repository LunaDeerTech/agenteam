package contract

import (
	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

const (
	ProjectProducer           event.StableName = "project"
	ProjectAggregate          event.StableName = "project"
	CreatedEventName          event.StableName = "project.created"
	UpdatedEventName          event.StableName = "project.updated"
	LifecycleChangedEventName event.StableName = "project.lifecycle_changed"
	ProjectEventSchemaVersion uint32           = 1
)

type CreatedPayload struct {
	OwnerUserID identity.UserID `json:"owner_user_id"`
	CreationID  CreationID      `json:"creation_id"`
}

func (p CreatedPayload) Validate() error {
	if p.OwnerUserID.Validate() != nil || p.CreationID.Validate() != nil {
		return invalid("", "INVALID_EVENT")
	}
	return nil
}
func (p CreatedPayload) MarshalJSON() ([]byte, error) {
	type wire CreatedPayload
	return checkedJSON(wire(p), p.Validate())
}
func (p *CreatedPayload) UnmarshalJSON(raw []byte) error {
	type wire CreatedPayload
	v, err := decodeFields[wire](raw, []string{"owner_user_id", "creation_id"}, nil, nil)
	if err != nil {
		return err
	}
	value := CreatedPayload(v)
	if err = value.Validate(); err == nil {
		*p = value
	}
	return err
}

type ChangedField string

const (
	NameChanged        ChangedField = "name"
	DescriptionChanged ChangedField = "description"
)

func (f ChangedField) Validate() error              { return oneOf(f, NameChanged, DescriptionChanged) }
func (f ChangedField) MarshalJSON() ([]byte, error) { return enumJSON(f, f.Validate()) }
func (f *ChangedField) UnmarshalJSON(raw []byte) error {
	v, err := decodeEnum(raw, ChangedField.Validate)
	if err == nil {
		*f = v
	}
	return err
}

type UpdatedPayload struct {
	ChangedFields []ChangedField `json:"changed_fields"`
}

func (p UpdatedPayload) Validate() error {
	if len(p.ChangedFields) < 1 || len(p.ChangedFields) > 2 {
		return invalid("/changed_fields", "INVALID_EVENT")
	}
	for i, field := range p.ChangedFields {
		if field.Validate() != nil || i > 0 && field == p.ChangedFields[i-1] {
			return invalid("/changed_fields", "INVALID_EVENT")
		}
	}
	return nil
}
func (p UpdatedPayload) MarshalJSON() ([]byte, error) {
	type wire UpdatedPayload
	return checkedJSON(wire(p), p.Validate())
}
func (p *UpdatedPayload) UnmarshalJSON(raw []byte) error {
	type wire UpdatedPayload
	v, err := decodeFields[wire](raw, []string{"changed_fields"}, nil, nil)
	if err != nil {
		return err
	}
	value := UpdatedPayload(v)
	if err = value.Validate(); err == nil {
		*p = value
	}
	return err
}

type LifecycleChangedPayload struct {
	OperationID *OperationID    `json:"operation_id,omitempty"`
	From        Lifecycle       `json:"from"`
	To          Lifecycle       `json:"to"`
	Action      LifecycleAction `json:"action"`
}

func (p LifecycleChangedPayload) Validate() error {
	return ValidateLifecycleTransition(p.From, p.To, p.Action, p.OperationID)
}
func (p LifecycleChangedPayload) MarshalJSON() ([]byte, error) {
	type wire LifecycleChangedPayload
	return checkedJSON(wire(p), p.Validate())
}
func (p *LifecycleChangedPayload) UnmarshalJSON(raw []byte) error {
	type wire LifecycleChangedPayload
	v, err := decodeFields[wire](raw, []string{"from", "to", "action"}, []string{"operation_id"}, nil)
	if err != nil {
		return err
	}
	value := LifecycleChangedPayload(v)
	if err = value.Validate(); err == nil {
		*p = value
	}
	return err
}

// ProjectEvents keeps the neutral event handles private so normal producers
// also check the Project-specific header/scope invariants. Registration occurs
// before Catalog.Seal. Discard the composition catalog if registration fails.
type ProjectEvents struct {
	catalog   *event.Catalog
	created   event.EventType[CreatedPayload]
	updated   event.EventType[UpdatedPayload]
	lifecycle event.EventType[LifecycleChangedPayload]
}

func RegisterProjectEvents(catalog *event.Catalog) (ProjectEvents, error) {
	if !catalog.Valid() {
		return ProjectEvents{}, invalid("", "INVALID_CATALOG")
	}
	for _, schema := range catalog.Schemas() {
		if schema.Version == ProjectEventSchemaVersion && (schema.EventType == CreatedEventName || schema.EventType == UpdatedEventName || schema.EventType == LifecycleChangedEventName) {
			return ProjectEvents{}, invalid("", "DUPLICATE_SCHEMA")
		}
	}
	created, err := event.DefineEvent(catalog, event.Definition[CreatedPayload]{Schema: event.Schema{Producer: ProjectProducer, EventType: CreatedEventName, AggregateType: ProjectAggregate, Version: ProjectEventSchemaVersion}, Codec: event.JSONCodec[CreatedPayload]{}, Validate: CreatedPayload.Validate})
	if err != nil {
		return ProjectEvents{}, err
	}
	updated, err := event.DefineEvent(catalog, event.Definition[UpdatedPayload]{Schema: event.Schema{Producer: ProjectProducer, EventType: UpdatedEventName, AggregateType: ProjectAggregate, Version: ProjectEventSchemaVersion}, Codec: event.JSONCodec[UpdatedPayload]{}, Validate: UpdatedPayload.Validate})
	if err != nil {
		return ProjectEvents{}, err
	}
	lifecycle, err := event.DefineEvent(catalog, event.Definition[LifecycleChangedPayload]{Schema: event.Schema{Producer: ProjectProducer, EventType: LifecycleChangedEventName, AggregateType: ProjectAggregate, Version: ProjectEventSchemaVersion}, Codec: event.JSONCodec[LifecycleChangedPayload]{}, Validate: LifecycleChangedPayload.Validate})
	if err != nil {
		return ProjectEvents{}, err
	}
	return ProjectEvents{catalog, created, updated, lifecycle}, nil
}
func validateProjectHeader(header event.Header) error {
	if header.Validate() != nil || header.SchemaVersion != ProjectEventSchemaVersion || header.AggregateType != ProjectAggregate || header.Scope.Kind != event.ProjectScope || header.Scope.ProjectID.String() != header.AggregateID.String() || header.AggregateVersion == nil || header.AggregateSequence != nil {
		return invalid("", "INVALID_PROJECT_EVENT_HEADER")
	}
	if header.EventType != CreatedEventName && header.EventType != UpdatedEventName && header.EventType != LifecycleChangedEventName {
		return fault(foundation.SchemaUnsupported)
	}
	if header.EventType == CreatedEventName && *header.AggregateVersion != 1 {
		return invalid("/aggregate_version", "INVALID_VERSION")
	}
	if header.EventType != CreatedEventName && *header.AggregateVersion <= 1 {
		return invalid("/aggregate_version", "INVALID_VERSION")
	}
	return nil
}
func (p ProjectEvents) validateEvent(e event.Event) error {
	if p.catalog == nil || !p.catalog.Owns(e) || e.Summary().Producer != ProjectProducer {
		return invalid("", "FOREIGN_EVENT")
	}
	return validateProjectHeader(e.Header())
}
func (p ProjectEvents) NewCreated(header event.Header, payload CreatedPayload) (event.Event, error) {
	if err := validateProjectHeader(header); err != nil {
		return event.Event{}, err
	}
	return event.NewEvent(p.created, header, payload)
}
func (p ProjectEvents) NewUpdated(header event.Header, payload UpdatedPayload) (event.Event, error) {
	if err := validateProjectHeader(header); err != nil {
		return event.Event{}, err
	}
	return event.NewEvent(p.updated, header, payload)
}
func (p ProjectEvents) NewLifecycleChanged(header event.Header, payload LifecycleChangedPayload) (event.Event, error) {
	if err := validateProjectHeader(header); err != nil {
		return event.Event{}, err
	}
	return event.NewEvent(p.lifecycle, header, payload)
}
func (p ProjectEvents) DecodeCreated(e event.Event) (CreatedPayload, error) {
	if err := p.validateEvent(e); err != nil {
		return CreatedPayload{}, err
	}
	return event.DecodeEvent(p.created, e)
}
func (p ProjectEvents) DecodeUpdated(e event.Event) (UpdatedPayload, error) {
	if err := p.validateEvent(e); err != nil {
		return UpdatedPayload{}, err
	}
	return event.DecodeEvent(p.updated, e)
}
func (p ProjectEvents) DecodeLifecycleChanged(e event.Event) (LifecycleChangedPayload, error) {
	if err := p.validateEvent(e); err != nil {
		return LifecycleChangedPayload{}, err
	}
	return event.DecodeEvent(p.lifecycle, e)
}
func (p ProjectEvents) Restore(header event.Header, raw []byte) (event.Event, error) {
	if err := validateProjectHeader(header); err != nil {
		return event.Event{}, err
	}
	if p.catalog == nil {
		return event.Event{}, invalid("", "INVALID_CATALOG")
	}
	return p.catalog.Restore(ProjectProducer, header, raw)
}
