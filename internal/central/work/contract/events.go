package contract

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"slices"

	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

const (
	WorkProducer         event.StableName = "work"
	MilestoneAggregate   event.StableName = "work.milestone"
	SprintAggregate      event.StableName = "work.sprint"
	MilestoneChangedName event.StableName = "work.milestone_changed"
	SprintChangedName    event.StableName = "work.sprint_changed"
	WorkSchemaVersion    uint32           = 1
)

type Change string

const (
	CreatedChange   Change = "created"
	UpdatedChange   Change = "updated"
	ReorderedChange Change = "reordered"
)

type ChangedField string

const (
	DescriptionChanged ChangedField = "description"
	RankChanged        ChangedField = "manual_rank"
	TitleChanged       ChangedField = "title"
)

type MilestonePosition struct {
	PreviousID      *MilestoneID `json:"previous_id"`
	NextID          *MilestoneID `json:"next_id"`
	OrderGeneration f.Version    `json:"order_generation"`
}
type SprintPosition struct {
	PreviousID      *SprintID `json:"previous_id"`
	NextID          *SprintID `json:"next_id"`
	OrderGeneration f.Version `json:"order_generation"`
}

func (p MilestonePosition) Validate() error {
	if p.OrderGeneration.Validate() != nil || p.PreviousID != nil && p.PreviousID.Validate() != nil || p.NextID != nil && p.NextID.Validate() != nil || p.PreviousID != nil && p.NextID != nil && *p.PreviousID == *p.NextID {
		return invalid("/position", "INVALID_POSITION")
	}
	return nil
}
func (p SprintPosition) Validate() error {
	if p.OrderGeneration.Validate() != nil || p.PreviousID != nil && p.PreviousID.Validate() != nil || p.NextID != nil && p.NextID.Validate() != nil || p.PreviousID != nil && p.NextID != nil && *p.PreviousID == *p.NextID {
		return invalid("/position", "INVALID_POSITION")
	}
	return nil
}

type MilestoneChanged struct {
	CommandID     CommandID          `json:"command_id"`
	ActorUserID   i.UserID           `json:"actor_user_id"`
	Change        Change             `json:"change"`
	ChangedFields []ChangedField     `json:"changed_fields"`
	Position      *MilestonePosition `json:"position"`
}
type SprintChanged struct {
	CommandID     CommandID       `json:"command_id"`
	ActorUserID   i.UserID        `json:"actor_user_id"`
	MilestoneID   MilestoneID     `json:"milestone_id"`
	Change        Change          `json:"change"`
	ChangedFields []ChangedField  `json:"changed_fields"`
	Position      *SprintPosition `json:"position"`
}

func validateChange(change Change, fields []ChangedField, position bool) error {
	switch change {
	case CreatedChange:
		if position && slices.Equal(fields, []ChangedField{DescriptionChanged, RankChanged, TitleChanged}) {
			return nil
		}
	case ReorderedChange:
		if position && slices.Equal(fields, []ChangedField{RankChanged}) {
			return nil
		}
	case UpdatedChange:
		if position || len(fields) < 1 || len(fields) > 2 {
			break
		}
		for n, v := range fields {
			if v != DescriptionChanged && v != TitleChanged || n > 0 && fields[n-1] >= v {
				return invalid("/changed_fields", "INVALID_EVENT")
			}
		}
		return nil
	}
	return invalid("", "INVALID_EVENT")
}
func (p MilestoneChanged) Validate() error {
	if p.CommandID.Validate() != nil || p.ActorUserID.Validate() != nil {
		return invalid("", "INVALID_EVENT")
	}
	if p.Position != nil {
		if err := p.Position.Validate(); err != nil {
			return err
		}
	}
	return validateChange(p.Change, p.ChangedFields, p.Position != nil)
}
func (p SprintChanged) Validate() error {
	if p.CommandID.Validate() != nil || p.ActorUserID.Validate() != nil || p.MilestoneID.Validate() != nil {
		return invalid("", "INVALID_EVENT")
	}
	if p.Position != nil {
		if err := p.Position.Validate(); err != nil {
			return err
		}
	}
	return validateChange(p.Change, p.ChangedFields, p.Position != nil)
}

type WorkEvents struct {
	catalog   *event.Catalog
	milestone event.EventType[MilestoneChanged]
	sprint    event.EventType[SprintChanged]
}

func RegisterWorkEvents(catalog *event.Catalog) (WorkEvents, error) {
	if !catalog.Valid() {
		return WorkEvents{}, invalid("", "INVALID_CATALOG")
	}
	for _, s := range catalog.Schemas() {
		if s.Version == WorkSchemaVersion && (s.EventType == MilestoneChangedName || s.EventType == SprintChangedName) {
			return WorkEvents{}, invalid("", "DUPLICATE_SCHEMA")
		}
	}
	m, err := event.DefineEvent(catalog, event.Definition[MilestoneChanged]{Schema: event.Schema{Producer: WorkProducer, EventType: MilestoneChangedName, AggregateType: MilestoneAggregate, Version: WorkSchemaVersion}, Codec: event.JSONCodec[MilestoneChanged]{}, Validate: MilestoneChanged.Validate})
	if err != nil {
		return WorkEvents{}, err
	}
	s, err := event.DefineEvent(catalog, event.Definition[SprintChanged]{Schema: event.Schema{Producer: WorkProducer, EventType: SprintChangedName, AggregateType: SprintAggregate, Version: WorkSchemaVersion}, Codec: event.JSONCodec[SprintChanged]{}, Validate: SprintChanged.Validate})
	if err != nil {
		return WorkEvents{}, err
	}
	return WorkEvents{catalog, m, s}, nil
}
func (w WorkEvents) Valid() bool { return w.catalog != nil && w.catalog.Valid() }
func validWorkHeader(h event.Header, name, aggregate event.StableName, change Change) error {
	if h.Validate() != nil || h.SchemaVersion != WorkSchemaVersion || h.EventType != name || h.AggregateType != aggregate || h.Scope.Kind != event.ProjectScope || h.AggregateVersion == nil || h.AggregateSequence != nil {
		return invalid("", "INVALID_EVENT_HEADER")
	}
	if change == CreatedChange && *h.AggregateVersion != 1 || change != CreatedChange && *h.AggregateVersion <= 1 {
		return invalid("/aggregate_version", "INVALID_VERSION")
	}
	return nil
}
func (w WorkEvents) NewMilestoneChanged(h event.Header, p MilestoneChanged) (event.Event, error) {
	if !w.Valid() {
		return event.Event{}, invalid("", "INVALID_CATALOG")
	}
	if err := validWorkHeader(h, MilestoneChangedName, MilestoneAggregate, p.Change); err != nil {
		return event.Event{}, err
	}
	if p.Position != nil && (p.Position.PreviousID != nil && p.Position.PreviousID.String() == h.AggregateID.String() || p.Position.NextID != nil && p.Position.NextID.String() == h.AggregateID.String()) {
		return event.Event{}, invalid("/position", "SELF_NEIGHBOR")
	}
	return event.NewEvent(w.milestone, h, p)
}
func (w WorkEvents) NewSprintChanged(h event.Header, p SprintChanged) (event.Event, error) {
	if !w.Valid() {
		return event.Event{}, invalid("", "INVALID_CATALOG")
	}
	if err := validWorkHeader(h, SprintChangedName, SprintAggregate, p.Change); err != nil {
		return event.Event{}, err
	}
	if p.Position != nil && (p.Position.PreviousID != nil && p.Position.PreviousID.String() == h.AggregateID.String() || p.Position.NextID != nil && p.Position.NextID.String() == h.AggregateID.String()) {
		return event.Event{}, invalid("/position", "SELF_NEIGHBOR")
	}
	return event.NewEvent(w.sprint, h, p)
}
func (w WorkEvents) Restore(h event.Header, raw []byte) (event.Event, error) {
	if !w.Valid() {
		return event.Event{}, invalid("", "INVALID_CATALOG")
	}
	switch h.EventType {
	case MilestoneChangedName:
		var p MilestoneChanged
		if err := json.Unmarshal(raw, &p); err != nil {
			return event.Event{}, err
		}
		return w.NewMilestoneChanged(h, p)
	case SprintChangedName:
		var p SprintChanged
		if err := json.Unmarshal(raw, &p); err != nil {
			return event.Event{}, err
		}
		return w.NewSprintChanged(h, p)
	default:
		return event.Event{}, f.NewFault(f.SchemaUnsupported, f.NotStarted)
	}
}
func (w WorkEvents) DecodeMilestoneChanged(e event.Event) (MilestoneChanged, error) {
	if !w.Valid() || !w.catalog.Owns(e) || e.Summary().Producer != WorkProducer {
		return MilestoneChanged{}, invalid("", "FOREIGN_EVENT")
	}
	p, err := event.DecodeEvent(w.milestone, e)
	if err != nil {
		return p, err
	}
	_, err = w.NewMilestoneChanged(e.Header(), p)
	return p, err
}
func (w WorkEvents) DecodeSprintChanged(e event.Event) (SprintChanged, error) {
	if !w.Valid() || !w.catalog.Owns(e) || e.Summary().Producer != WorkProducer {
		return SprintChanged{}, invalid("", "FOREIGN_EVENT")
	}
	p, err := event.DecodeEvent(w.sprint, e)
	if err != nil {
		return p, err
	}
	_, err = w.NewSprintChanged(e.Header(), p)
	return p, err
}

func (v MilestonePosition) MarshalJSON() ([]byte, error) {
	type wire MilestonePosition
	return checked(wire(v), v.Validate())
}
func (v *MilestonePosition) UnmarshalJSON(raw []byte) error {
	type wire MilestonePosition
	w, err := decodeFields[wire](raw, []string{"previous_id", "next_id", "order_generation"}, nil, []string{"previous_id", "next_id"})
	if err != nil {
		return err
	}
	next := MilestonePosition(w)
	if err = next.Validate(); err == nil {
		*v = next
	}
	return err
}
func (v SprintPosition) MarshalJSON() ([]byte, error) {
	type wire SprintPosition
	return checked(wire(v), v.Validate())
}
func (v *SprintPosition) UnmarshalJSON(raw []byte) error {
	type wire SprintPosition
	w, err := decodeFields[wire](raw, []string{"previous_id", "next_id", "order_generation"}, nil, []string{"previous_id", "next_id"})
	if err != nil {
		return err
	}
	next := SprintPosition(w)
	if err = next.Validate(); err == nil {
		*v = next
	}
	return err
}
func (v MilestoneChanged) MarshalJSON() ([]byte, error) {
	type wire MilestoneChanged
	return checked(wire(v), v.Validate())
}
func (v *MilestoneChanged) UnmarshalJSON(raw []byte) error {
	type wire MilestoneChanged
	w, err := decodeFields[wire](raw, []string{"command_id", "actor_user_id", "change", "changed_fields", "position"}, nil, []string{"position"})
	if err != nil {
		return err
	}
	next := MilestoneChanged(w)
	if err = next.Validate(); err == nil {
		*v = next
	}
	return err
}
func (v SprintChanged) MarshalJSON() ([]byte, error) {
	type wire SprintChanged
	return checked(wire(v), v.Validate())
}
func (v *SprintChanged) UnmarshalJSON(raw []byte) error {
	type wire SprintChanged
	w, err := decodeFields[wire](raw, []string{"command_id", "actor_user_id", "milestone_id", "change", "changed_fields", "position"}, nil, []string{"position"})
	if err != nil {
		return err
	}
	next := SprintChanged(w)
	if err = next.Validate(); err == nil {
		*v = next
	}
	return err
}
func (v MilestoneChanged) Format(w fmt.State, r rune) { safeFormat(w, r) }
func (v MilestoneChanged) LogValue() slog.Value       { return safeLog() }
func (v SprintChanged) Format(w fmt.State, r rune)    { safeFormat(w, r) }
func (v SprintChanged) LogValue() slog.Value          { return safeLog() }
