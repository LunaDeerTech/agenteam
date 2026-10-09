package contract

import (
	"encoding/json"
	"fmt"
	"log/slog"

	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

const (
	TaskBlockersChangedName     event.StableName = "work.task_blockers_changed"
	TaskBlockerSchemaVersion    uint32           = 1
	MaxTaskBlockersChangedBytes                  = 16 << 10
	MaxTaskBlockerEventBytes                     = 16 << 10
)

type TaskBlockerEventType string

const (
	TaskBlockerEventAdded    TaskBlockerEventType = "blocker_added"
	TaskBlockerEventResolved TaskBlockerEventType = "blocker_resolved"
)

func (v TaskBlockerEventType) Validate() error {
	if v != TaskBlockerEventAdded && v != TaskBlockerEventResolved {
		return invalid("/type", "INVALID_BLOCKER_EVENT_TYPE")
	}
	return nil
}
func (v TaskBlockerEventType) MarshalJSON() ([]byte, error) {
	return checkedLimit(string(v), v.Validate(), 64)
}
func (v *TaskBlockerEventType) UnmarshalJSON(raw []byte) error {
	if v == nil {
		return invalid("", "INVALID_ENCODING")
	}
	s, e := taskBlockerString(raw, 64)
	if e != nil {
		return e
	}
	n := TaskBlockerEventType(s)
	if e = n.Validate(); e != nil {
		return e
	}
	*v = n
	return nil
}

// The enclosing event selects exactly one typed payload; this union is not a wire DTO.
type TaskBlockerEventPayload struct {
	Added    *TaskBlockerAddedPayload
	Resolved *TaskBlockerResolvedPayload
}

func (v TaskBlockerEventPayload) ValidateFor(t TaskBlockerEventType) error {
	if t == TaskBlockerEventAdded && v.Added != nil && v.Resolved == nil {
		return v.Added.Validate()
	}
	if t == TaskBlockerEventResolved && v.Resolved != nil && v.Added == nil {
		return v.Resolved.Validate()
	}
	return invalid("/payload", "INVALID_BLOCKER_EVENT_PAYLOAD")
}
func (v TaskBlockerEventPayload) Clone() TaskBlockerEventPayload {
	v.Added = taskClonePtr(v.Added)
	if v.Resolved != nil {
		n := v.Resolved.Clone()
		v.Resolved = &n
	}
	return v
}

type TaskBlockerEvent struct {
	ID            TaskEventID             `json:"id"`
	ProjectID     ProjectID               `json:"project_id"`
	TaskID        TaskID                  `json:"task_id"`
	TaskVersion   f.Version               `json:"task_version"`
	Type          TaskBlockerEventType    `json:"type"`
	Actor         TaskEventActor          `json:"actor"`
	OperationID   TaskBlockerCommandID    `json:"operation_id"`
	CorrelationID TaskBlockerCommandID    `json:"correlation_id"`
	Payload       TaskBlockerEventPayload `json:"-"`
	CreatedAt     f.Instant               `json:"created_at"`
}

func (v TaskBlockerEvent) Validate() error {
	if v.ID.Validate() != nil || v.ProjectID.Validate() != nil || v.TaskID.Validate() != nil || v.TaskVersion < 2 || v.Type.Validate() != nil || v.Actor.Validate() != nil || v.OperationID.Validate() != nil || v.CorrelationID != v.OperationID || v.CreatedAt.Validate() != nil {
		return invalid("", "INVALID_BLOCKER_EVENT")
	}
	return v.Payload.ValidateFor(v.Type)
}
func (v TaskBlockerEvent) Clone() TaskBlockerEvent { v.Payload = v.Payload.Clone(); return v }
func (v TaskBlockerEvent) MarshalJSON() ([]byte, error) {
	if e := v.Validate(); e != nil {
		return nil, e
	}
	var p any = v.Payload.Added
	if v.Type == TaskBlockerEventResolved {
		p = v.Payload.Resolved
	}
	type wire TaskBlockerEvent
	return checkedLimit(struct {
		wire
		Payload any `json:"payload"`
	}{wire(v), p}, nil, MaxTaskBlockerEventBytes)
}
func (v *TaskBlockerEvent) UnmarshalJSON(raw []byte) error {
	if v == nil {
		return invalid("", "INVALID_ENCODING")
	}
	fields, e := taskBlockerFields(raw, MaxTaskBlockerEventBytes, []string{"id", "project_id", "task_id", "task_version", "type", "actor", "operation_id", "correlation_id", "payload", "created_at"}, nil)
	if e != nil {
		return e
	}
	var n TaskBlockerEvent
	for _, p := range []struct {
		k string
		v any
	}{{"id", &n.ID}, {"project_id", &n.ProjectID}, {"task_id", &n.TaskID}, {"task_version", &n.TaskVersion}, {"type", &n.Type}, {"operation_id", &n.OperationID}, {"correlation_id", &n.CorrelationID}, {"created_at", &n.CreatedAt}} {
		if json.Unmarshal(fields[p.k], p.v) != nil {
			return invalid("", "INVALID_ENCODING")
		}
	}
	if e = n.Actor.UnmarshalJSON(fields["actor"]); e != nil {
		return e
	}
	switch n.Type {
	case TaskBlockerEventAdded:
		n.Payload.Added = new(TaskBlockerAddedPayload)
		e = n.Payload.Added.UnmarshalJSON(fields["payload"])
	case TaskBlockerEventResolved:
		n.Payload.Resolved = new(TaskBlockerResolvedPayload)
		e = n.Payload.Resolved.UnmarshalJSON(fields["payload"])
	default:
		return invalid("/type", "INVALID_BLOCKER_EVENT_TYPE")
	}
	if e != nil {
		return e
	}
	if e = n.Validate(); e != nil {
		return e
	}
	*v = n
	return nil
}
func DecodeTaskBlockerEvent(raw []byte) (TaskBlockerEvent, error) {
	var v TaskBlockerEvent
	e := v.UnmarshalJSON(raw)
	return v, e
}

type TaskBlockerChange string

const (
	TaskBlockerAddedChange    TaskBlockerChange = "added"
	TaskBlockerResolvedChange TaskBlockerChange = "resolved"
)

func (v TaskBlockerChange) Validate() error {
	if v != TaskBlockerAddedChange && v != TaskBlockerResolvedChange {
		return invalid("/change", "INVALID_BLOCKER_CHANGE")
	}
	return nil
}
func (v TaskBlockerChange) MarshalJSON() ([]byte, error) {
	return checkedLimit(string(v), v.Validate(), 64)
}
func (v *TaskBlockerChange) UnmarshalJSON(raw []byte) error {
	if v == nil {
		return invalid("", "INVALID_ENCODING")
	}
	s, e := taskBlockerString(raw, 64)
	if e != nil {
		return e
	}
	n := TaskBlockerChange(s)
	if e = n.Validate(); e != nil {
		return e
	}
	*v = n
	return nil
}

type TaskBlockersChanged struct {
	OperationID TaskBlockerCommandID `json:"operation_id"`
	ActorUserID i.UserID             `json:"actor_user_id"`
	TaskEventID TaskEventID          `json:"task_event_id"`
	BlockerID   TaskBlockerID        `json:"blocker_id"`
	Change      TaskBlockerChange    `json:"change"`
}

func (v TaskBlockersChanged) Validate() error {
	if v.OperationID.Validate() != nil || v.ActorUserID.Validate() != nil || v.TaskEventID.Validate() != nil || v.BlockerID.Validate() != nil || v.Change.Validate() != nil {
		return invalid("", "INVALID_BLOCKERS_CHANGED")
	}
	return nil
}
func (v TaskBlockersChanged) Clone() TaskBlockersChanged { return v }
func (v TaskBlockersChanged) MarshalJSON() ([]byte, error) {
	type wire TaskBlockersChanged
	return checkedLimit(wire(v), v.Validate(), MaxTaskBlockersChangedBytes)
}
func (v *TaskBlockersChanged) UnmarshalJSON(raw []byte) error {
	if v == nil {
		return invalid("", "INVALID_ENCODING")
	}
	type wire TaskBlockersChanged
	n, e := decodeFieldsLimit[wire](raw, []string{"operation_id", "actor_user_id", "task_event_id", "blocker_id", "change"}, nil, nil, MaxTaskBlockersChangedBytes)
	if e != nil {
		return e
	}
	next := TaskBlockersChanged(n)
	if e = next.Validate(); e != nil {
		return e
	}
	*v = next
	return nil
}
func DecodeTaskBlockersChanged(raw []byte) (TaskBlockersChanged, error) {
	var v TaskBlockersChanged
	e := v.UnmarshalJSON(raw)
	return v, e
}

type TaskBlockerEvents struct {
	catalog *event.Catalog
	task    event.EventType[TaskBlockersChanged]
}

func RegisterTaskBlockerEvents(catalog *event.Catalog) (TaskBlockerEvents, error) {
	if !catalog.Valid() {
		return TaskBlockerEvents{}, invalid("", "INVALID_CATALOG")
	}
	t, e := event.DefineEvent(catalog, event.Definition[TaskBlockersChanged]{Schema: event.Schema{Producer: WorkProducer, EventType: TaskBlockersChangedName, AggregateType: TaskAggregate, Version: TaskBlockerSchemaVersion}, Codec: event.JSONCodec[TaskBlockersChanged]{}, Validate: TaskBlockersChanged.Validate})
	if e != nil {
		return TaskBlockerEvents{}, e
	}
	return TaskBlockerEvents{catalog, t}, nil
}
func (v TaskBlockerEvents) Valid() bool {
	return v.catalog.Valid() && v.task.Schema() == (event.Schema{Producer: WorkProducer, EventType: TaskBlockersChangedName, AggregateType: TaskAggregate, Version: TaskBlockerSchemaVersion})
}
func validTaskBlockerHeader(h event.Header) error {
	if h.Validate() != nil || h.EventType != TaskBlockersChangedName || h.AggregateType != TaskAggregate || h.SchemaVersion != TaskBlockerSchemaVersion || h.Scope.Kind != event.ProjectScope || h.AggregateVersion == nil || *h.AggregateVersion < 2 || h.AggregateSequence != nil {
		return invalid("", "INVALID_EVENT_HEADER")
	}
	return nil
}
func (v TaskBlockerEvents) NewTaskBlockersChanged(h event.Header, p TaskBlockersChanged) (event.Event, error) {
	if !v.Valid() {
		return event.Event{}, invalid("", "INVALID_CATALOG")
	}
	if e := validTaskBlockerHeader(h); e != nil {
		return event.Event{}, e
	}
	if e := p.Validate(); e != nil {
		return event.Event{}, e
	}
	return event.NewEvent(v.task, h, p)
}
func (v TaskBlockerEvents) Restore(h event.Header, raw []byte) (event.Event, error) {
	if !v.Valid() {
		return event.Event{}, invalid("", "INVALID_CATALOG")
	}
	if h.EventType != TaskBlockersChangedName || h.AggregateType != TaskAggregate || h.SchemaVersion != TaskBlockerSchemaVersion {
		return event.Event{}, f.NewFault(f.SchemaUnsupported, f.NotStarted)
	}
	p, e := DecodeTaskBlockersChanged(raw)
	if e != nil {
		return event.Event{}, e
	}
	return v.NewTaskBlockersChanged(h, p)
}
func (v TaskBlockerEvents) DecodeTaskBlockersChanged(e event.Event) (TaskBlockersChanged, error) {
	if !v.Valid() || !v.catalog.Owns(e) || e.Summary().Producer != WorkProducer {
		return TaskBlockersChanged{}, invalid("", "FOREIGN_EVENT")
	}
	p, err := event.DecodeEvent(v.task, e)
	if err != nil {
		return TaskBlockersChanged{}, err
	}
	if _, err = v.NewTaskBlockersChanged(e.Header(), p); err != nil {
		return TaskBlockersChanged{}, err
	}
	return p, nil
}
func (TaskBlockerEventType) Format(w fmt.State, r rune)    { blockerSafeFormat(w, r) }
func (TaskBlockerEventType) LogValue() slog.Value          { return blockerSafeLog() }
func (TaskBlockerEventPayload) Format(w fmt.State, r rune) { blockerSafeFormat(w, r) }
func (TaskBlockerEventPayload) LogValue() slog.Value       { return blockerSafeLog() }
func (TaskBlockerEvent) Format(w fmt.State, r rune)        { blockerSafeFormat(w, r) }
func (TaskBlockerEvent) LogValue() slog.Value              { return blockerSafeLog() }
func (TaskBlockerChange) Format(w fmt.State, r rune)       { blockerSafeFormat(w, r) }
func (TaskBlockerChange) LogValue() slog.Value             { return blockerSafeLog() }
func (TaskBlockersChanged) Format(w fmt.State, r rune)     { blockerSafeFormat(w, r) }
func (TaskBlockersChanged) LogValue() slog.Value           { return blockerSafeLog() }
func (TaskBlockerEvents) Format(w fmt.State, r rune)       { blockerSafeFormat(w, r) }
func (TaskBlockerEvents) LogValue() slog.Value             { return blockerSafeLog() }
