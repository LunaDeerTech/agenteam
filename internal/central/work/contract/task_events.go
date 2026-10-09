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
	TaskAggregate              event.StableName = "work.task"
	TaskChangedName            event.StableName = "work.task_changed"
	TaskSchemaVersion          uint32           = 1
	MaxTaskEventBytes                           = 16 << 10
	MaxTaskHistoryPayloadBytes                  = 8 << 10
	MaxTaskChangedBytes                         = 16 << 10
)

type TaskEventType string

const (
	TaskEventCreated       TaskEventType = "task_created"
	TaskEventFieldsUpdated TaskEventType = "fields_updated"
)

func (v TaskEventType) Validate() error {
	if v != TaskEventCreated && v != TaskEventFieldsUpdated {
		return invalid("/type", "INVALID_TASK_EVENT_TYPE")
	}
	return nil
}

type TaskChange string

const (
	TaskCreatedChange   TaskChange = "created"
	TaskUpdatedChange   TaskChange = "updated"
	TaskReorderedChange TaskChange = "reordered"
)

func (v TaskChange) Validate() error {
	if v != TaskCreatedChange && v != TaskUpdatedChange && v != TaskReorderedChange {
		return invalid("/change", "INVALID_TASK_CHANGE")
	}
	return nil
}

type TaskChangedField string

const (
	TaskDescriptionChanged TaskChangedField = "description"
	TaskRankChanged        TaskChangedField = "manual_rank"
	TaskPlanChanged        TaskChangedField = "plan"
	TaskPriorityChanged    TaskChangedField = "priority"
	TaskTitleChanged       TaskChangedField = "title"
	TaskTypeChanged        TaskChangedField = "type"
)

func (v TaskChangedField) Validate() error {
	switch v {
	case TaskDescriptionChanged, TaskRankChanged, TaskPlanChanged, TaskPriorityChanged, TaskTitleChanged, TaskTypeChanged:
		return nil
	}
	return invalid("/changed_fields", "INVALID_CHANGED_FIELD")
}
func validateTaskFields(v []TaskChangedField) error {
	if len(v) == 0 || len(v) > 6 {
		return invalid("/changed_fields", "INVALID_CHANGED_FIELDS")
	}
	for n, field := range v {
		if field.Validate() != nil || n > 0 && v[n-1] >= field {
			return invalid("/changed_fields", "INVALID_CHANGED_FIELDS")
		}
	}
	return nil
}

type TaskPosition struct {
	SprintID        SprintID     `json:"sprint_id"`
	State           TaskState    `json:"state"`
	Priority        TaskPriority `json:"priority"`
	PreviousID      *TaskID      `json:"previous_id"`
	NextID          *TaskID      `json:"next_id"`
	OrderGeneration int64        `json:"-"`
}

func (v TaskPosition) Validate() error {
	if v.SprintID.Validate() != nil || v.State != TaskStateBacklog || v.Priority.Validate() != nil || v.OrderGeneration < 1 || v.PreviousID != nil && v.PreviousID.Validate() != nil || v.NextID != nil && v.NextID.Validate() != nil || v.PreviousID != nil && v.NextID != nil && *v.PreviousID == *v.NextID {
		return invalid("/position", "INVALID_POSITION")
	}
	return nil
}
func (v TaskPosition) ValidateTarget(id TaskID) error {
	if err := v.Validate(); err != nil {
		return err
	}
	if id.Validate() != nil || v.PreviousID != nil && *v.PreviousID == id || v.NextID != nil && *v.NextID == id {
		return invalid("/position", "SELF_NEIGHBOR")
	}
	return nil
}
func (v TaskPosition) Clone() TaskPosition {
	v.PreviousID = taskClonePtr(v.PreviousID)
	v.NextID = taskClonePtr(v.NextID)
	return v
}
func cloneTaskPosition(v *TaskPosition) *TaskPosition {
	if v == nil {
		return nil
	}
	next := v.Clone()
	return &next
}
func (v TaskPosition) MarshalJSON() ([]byte, error) {
	return checkedLimit(struct {
		SprintID        SprintID     `json:"sprint_id"`
		State           TaskState    `json:"state"`
		Priority        TaskPriority `json:"priority"`
		PreviousID      *TaskID      `json:"previous_id"`
		NextID          *TaskID      `json:"next_id"`
		OrderGeneration f.Version    `json:"order_generation"`
	}{v.SprintID, v.State, v.Priority, v.PreviousID, v.NextID, f.Version(v.OrderGeneration)}, v.Validate(), MaxTaskHistoryPayloadBytes)
}
func (v *TaskPosition) UnmarshalJSON(raw []byte) error {
	type wire struct {
		SprintID        SprintID     `json:"sprint_id"`
		State           TaskState    `json:"state"`
		Priority        TaskPriority `json:"priority"`
		PreviousID      *TaskID      `json:"previous_id"`
		NextID          *TaskID      `json:"next_id"`
		OrderGeneration f.Version    `json:"order_generation"`
	}
	w, err := decodeFieldsLimit[wire](raw, []string{"sprint_id", "state", "priority", "previous_id", "next_id", "order_generation"}, nil, []string{"previous_id", "next_id"}, MaxTaskHistoryPayloadBytes)
	if err != nil {
		return err
	}
	next := TaskPosition{w.SprintID, w.State, w.Priority, w.PreviousID, w.NextID, int64(w.OrderGeneration)}
	if err = next.Validate(); err == nil {
		*v = next
	}
	return err
}

type TaskEventActor struct {
	Type   i.ActorKind `json:"type"`
	UserID i.UserID    `json:"user_id"`
	Source string      `json:"source"`
}

func (v TaskEventActor) Validate() error {
	if v.Type != i.Human || v.UserID.Validate() != nil || v.Source != "task_domain" {
		return invalid("/actor", "INVALID_TASK_EVENT_ACTOR")
	}
	return nil
}
func (v TaskEventActor) Clone() TaskEventActor { return v }

type TaskCreatedPayload struct {
	InitialState TaskState    `json:"initial_state"`
	MilestoneID  MilestoneID  `json:"milestone_id"`
	SprintID     SprintID     `json:"sprint_id"`
	Type         TaskType     `json:"type"`
	Priority     TaskPriority `json:"priority"`
}

func (v TaskCreatedPayload) Validate() error {
	if v.InitialState != TaskStateBacklog || v.MilestoneID.Validate() != nil || v.SprintID.Validate() != nil || v.Type.Validate() != nil || v.Priority.Validate() != nil {
		return invalid("/payload", "INVALID_TASK_CREATED")
	}
	return nil
}
func (v TaskCreatedPayload) Clone() TaskCreatedPayload { return v }

type TaskTypeChange struct {
	From TaskType `json:"from"`
	To   TaskType `json:"to"`
}

func (v TaskTypeChange) Validate() error {
	if v.From.Validate() != nil || v.To.Validate() != nil || v.From == v.To {
		return invalid("/type_change", "INVALID_TYPE_CHANGE")
	}
	return nil
}
func (v TaskTypeChange) Clone() TaskTypeChange { return v }

type TaskPriorityChange struct {
	From TaskPriority `json:"from"`
	To   TaskPriority `json:"to"`
}

func (v TaskPriorityChange) Validate() error {
	if v.From.Validate() != nil || v.To.Validate() != nil || v.From == v.To {
		return invalid("/priority_change", "INVALID_PRIORITY_CHANGE")
	}
	return nil
}
func (v TaskPriorityChange) Clone() TaskPriorityChange { return v }

type TaskFieldsUpdatedPayload struct {
	ChangedFields  []TaskChangedField  `json:"changed_fields"`
	TypeChange     *TaskTypeChange     `json:"type_change"`
	PriorityChange *TaskPriorityChange `json:"priority_change"`
	Position       *TaskPosition       `json:"position"`
}

func (v TaskFieldsUpdatedPayload) Validate() error {
	if err := validateTaskFields(v.ChangedFields); err != nil {
		return err
	}
	if v.TypeChange != nil && v.TypeChange.Validate() != nil || v.PriorityChange != nil && v.PriorityChange.Validate() != nil || v.Position != nil && v.Position.Validate() != nil {
		return invalid("/payload", "INVALID_FIELDS_UPDATED")
	}
	if slices.Contains(v.ChangedFields, TaskTypeChanged) != (v.TypeChange != nil) || slices.Contains(v.ChangedFields, TaskPriorityChanged) != (v.PriorityChange != nil) {
		return invalid("/payload", "INVALID_FIELDS_UPDATED")
	}
	if slices.Contains(v.ChangedFields, TaskRankChanged) {
		if len(v.ChangedFields) != 1 || v.TypeChange != nil || v.PriorityChange != nil || v.Position == nil {
			return invalid("/payload", "INVALID_FIELDS_UPDATED")
		}
		return nil
	}
	if (v.PriorityChange != nil) != (v.Position != nil) || v.Position != nil && (v.Position.Priority != v.PriorityChange.To || v.Position.NextID != nil) {
		return invalid("/position", "INVALID_POSITION")
	}
	return nil
}
func (v TaskFieldsUpdatedPayload) Clone() TaskFieldsUpdatedPayload {
	v.ChangedFields = slices.Clone(v.ChangedFields)
	v.TypeChange = taskClonePtr(v.TypeChange)
	v.PriorityChange = taskClonePtr(v.PriorityChange)
	v.Position = cloneTaskPosition(v.Position)
	return v
}

type TaskEvent struct {
	ID            TaskEventID     `json:"id"`
	ProjectID     ProjectID       `json:"project_id"`
	TaskID        TaskID          `json:"task_id"`
	TaskVersion   f.Version       `json:"task_version"`
	Type          TaskEventType   `json:"type"`
	Actor         TaskEventActor  `json:"actor"`
	OperationID   TaskCommandID   `json:"operation_id"`
	CorrelationID TaskCommandID   `json:"correlation_id"`
	Payload       json.RawMessage `json:"payload"`
	CreatedAt     f.Instant       `json:"created_at"`
}

func (v TaskEvent) Validate() error {
	if v.ID.Validate() != nil || v.ProjectID.Validate() != nil || v.TaskID.Validate() != nil || v.TaskVersion.Validate() != nil || v.Type.Validate() != nil || v.Actor.Validate() != nil || v.OperationID.Validate() != nil || v.CorrelationID != v.OperationID || v.CreatedAt.Validate() != nil {
		return invalid("", "INVALID_TASK_EVENT")
	}
	switch v.Type {
	case TaskEventCreated:
		if v.TaskVersion != 1 {
			return invalid("/task_version", "INVALID_VERSION")
		}
		var p TaskCreatedPayload
		return p.UnmarshalJSON(v.Payload)
	case TaskEventFieldsUpdated:
		if v.TaskVersion <= 1 {
			return invalid("/task_version", "INVALID_VERSION")
		}
		var p TaskFieldsUpdatedPayload
		if err := p.UnmarshalJSON(v.Payload); err != nil {
			return err
		}
		if p.Position != nil {
			return p.Position.ValidateTarget(v.TaskID)
		}
		return nil
	}
	return invalid("", "INVALID_TASK_EVENT")
}
func (v TaskEvent) Clone() TaskEvent { v.Payload = slices.Clone(v.Payload); return v }

type TaskChanged struct {
	CommandID     TaskCommandID      `json:"command_id"`
	ActorUserID   i.UserID           `json:"actor_user_id"`
	TaskEventID   TaskEventID        `json:"task_event_id"`
	MilestoneID   MilestoneID        `json:"milestone_id"`
	SprintID      SprintID           `json:"sprint_id"`
	Change        TaskChange         `json:"change"`
	ChangedFields []TaskChangedField `json:"changed_fields"`
	Position      *TaskPosition      `json:"position"`
}

func (v TaskChanged) Validate() error {
	if v.CommandID.Validate() != nil || v.ActorUserID.Validate() != nil || v.TaskEventID.Validate() != nil || v.MilestoneID.Validate() != nil || v.SprintID.Validate() != nil || v.Change.Validate() != nil || validateTaskFields(v.ChangedFields) != nil {
		return invalid("", "INVALID_TASK_CHANGED")
	}
	if v.Position != nil && (v.Position.Validate() != nil || v.Position.SprintID != v.SprintID) {
		return invalid("/position", "INVALID_POSITION")
	}
	switch v.Change {
	case TaskCreatedChange:
		if v.Position != nil && v.Position.NextID == nil && slices.Equal(v.ChangedFields, []TaskChangedField{TaskDescriptionChanged, TaskRankChanged, TaskPlanChanged, TaskPriorityChanged, TaskTitleChanged, TaskTypeChanged}) {
			return nil
		}
	case TaskUpdatedChange:
		if !slices.Contains(v.ChangedFields, TaskRankChanged) && slices.Contains(v.ChangedFields, TaskPriorityChanged) == (v.Position != nil) && (v.Position == nil || v.Position.NextID == nil) {
			return nil
		}
	case TaskReorderedChange:
		if v.Position != nil && slices.Equal(v.ChangedFields, []TaskChangedField{TaskRankChanged}) {
			return nil
		}
	}
	return invalid("", "INVALID_TASK_CHANGED")
}
func (v TaskChanged) Clone() TaskChanged {
	v.ChangedFields = slices.Clone(v.ChangedFields)
	v.Position = cloneTaskPosition(v.Position)
	return v
}

type TaskEvents struct {
	catalog *event.Catalog
	task    event.EventType[TaskChanged]
}

func RegisterTaskEvents(catalog *event.Catalog) (TaskEvents, error) {
	if !catalog.Valid() {
		return TaskEvents{}, invalid("", "INVALID_CATALOG")
	}
	for _, schema := range catalog.Schemas() {
		if schema.EventType == TaskChangedName && schema.Version == TaskSchemaVersion {
			return TaskEvents{}, invalid("", "DUPLICATE_SCHEMA")
		}
	}
	t, err := event.DefineEvent(catalog, event.Definition[TaskChanged]{Schema: event.Schema{Producer: WorkProducer, EventType: TaskChangedName, AggregateType: TaskAggregate, Version: TaskSchemaVersion}, Codec: event.JSONCodec[TaskChanged]{}, Validate: TaskChanged.Validate})
	if err != nil {
		return TaskEvents{}, err
	}
	return TaskEvents{catalog, t}, nil
}
func (v TaskEvents) Valid() bool { return v.catalog != nil && v.catalog.Valid() }
func (v TaskEvents) NewTaskChanged(h event.Header, p TaskChanged) (event.Event, error) {
	if !v.Valid() {
		return event.Event{}, invalid("", "INVALID_CATALOG")
	}
	if err := validWorkHeader(h, TaskChangedName, TaskAggregate, Change(p.Change)); err != nil {
		return event.Event{}, err
	}
	if p.Position != nil {
		target, err := f.ParseID[Task](h.AggregateID.String())
		if err != nil {
			return event.Event{}, invalid("", "INVALID_EVENT_HEADER")
		}
		if err = p.Position.ValidateTarget(target); err != nil {
			return event.Event{}, err
		}
	}
	return event.NewEvent(v.task, h, p.Clone())
}
func (v TaskEvents) Restore(h event.Header, raw []byte) (event.Event, error) {
	if !v.Valid() {
		return event.Event{}, invalid("", "INVALID_CATALOG")
	}
	if h.EventType != TaskChangedName || h.AggregateType != TaskAggregate || h.SchemaVersion != TaskSchemaVersion {
		return event.Event{}, f.NewFault(f.SchemaUnsupported, f.NotStarted)
	}
	var p TaskChanged
	if err := p.UnmarshalJSON(raw); err != nil {
		return event.Event{}, err
	}
	return v.NewTaskChanged(h, p)
}
func (v TaskEvents) DecodeTaskChanged(e event.Event) (TaskChanged, error) {
	if !v.Valid() || !v.catalog.Owns(e) || e.Summary().Producer != WorkProducer {
		return TaskChanged{}, invalid("", "FOREIGN_EVENT")
	}
	p, err := event.DecodeEvent(v.task, e)
	if err != nil {
		return TaskChanged{}, err
	}
	if _, err = v.NewTaskChanged(e.Header(), p); err != nil {
		return TaskChanged{}, err
	}
	return p.Clone(), nil
}

func (v TaskEventType) MarshalJSON() ([]byte, error) {
	return checkedLimit(string(v), v.Validate(), MaxTaskHistoryPayloadBytes)
}
func (v *TaskEventType) UnmarshalJSON(raw []byte) error {
	if strictRawLimit(raw, MaxTaskHistoryPayloadBytes) != nil {
		return invalid("", "INVALID_ENCODING")
	}
	var s string
	if json.Unmarshal(raw, &s) != nil {
		return invalid("", "INVALID_ENCODING")
	}
	next := TaskEventType(s)
	if err := next.Validate(); err != nil {
		return err
	}
	*v = next
	return nil
}

func (v TaskChange) MarshalJSON() ([]byte, error) {
	return checkedLimit(string(v), v.Validate(), MaxTaskHistoryPayloadBytes)
}
func (v *TaskChange) UnmarshalJSON(raw []byte) error {
	if strictRawLimit(raw, MaxTaskHistoryPayloadBytes) != nil {
		return invalid("", "INVALID_ENCODING")
	}
	var s string
	if json.Unmarshal(raw, &s) != nil {
		return invalid("", "INVALID_ENCODING")
	}
	next := TaskChange(s)
	if err := next.Validate(); err != nil {
		return err
	}
	*v = next
	return nil
}

func (v TaskChangedField) MarshalJSON() ([]byte, error) {
	return checkedLimit(string(v), v.Validate(), MaxTaskHistoryPayloadBytes)
}
func (v *TaskChangedField) UnmarshalJSON(raw []byte) error {
	if strictRawLimit(raw, MaxTaskHistoryPayloadBytes) != nil {
		return invalid("", "INVALID_ENCODING")
	}
	var s string
	if json.Unmarshal(raw, &s) != nil {
		return invalid("", "INVALID_ENCODING")
	}
	next := TaskChangedField(s)
	if err := next.Validate(); err != nil {
		return err
	}
	*v = next
	return nil
}

func (v TaskEventActor) MarshalJSON() ([]byte, error) {
	type wire TaskEventActor
	return checkedLimit(wire(v), v.Validate(), MaxTaskHistoryPayloadBytes)
}
func (v *TaskEventActor) UnmarshalJSON(raw []byte) error {
	type wire TaskEventActor
	w, err := decodeFieldsLimit[wire](raw, []string{"type", "user_id", "source"}, nil, nil, MaxTaskHistoryPayloadBytes)
	if err != nil {
		return err
	}
	next := TaskEventActor(w)
	if err = next.Validate(); err == nil {
		*v = next
	}
	return err
}

func (v TaskCreatedPayload) MarshalJSON() ([]byte, error) {
	type wire TaskCreatedPayload
	return checkedLimit(wire(v), v.Validate(), MaxTaskHistoryPayloadBytes)
}
func (v *TaskCreatedPayload) UnmarshalJSON(raw []byte) error {
	type wire TaskCreatedPayload
	w, err := decodeFieldsLimit[wire](raw, []string{"initial_state", "milestone_id", "sprint_id", "type", "priority"}, nil, nil, MaxTaskHistoryPayloadBytes)
	if err != nil {
		return err
	}
	next := TaskCreatedPayload(w)
	if err = next.Validate(); err == nil {
		*v = next
	}
	return err
}

func (v TaskTypeChange) MarshalJSON() ([]byte, error) {
	type wire TaskTypeChange
	return checkedLimit(wire(v), v.Validate(), MaxTaskHistoryPayloadBytes)
}
func (v *TaskTypeChange) UnmarshalJSON(raw []byte) error {
	type wire TaskTypeChange
	w, err := decodeFieldsLimit[wire](raw, []string{"from", "to"}, nil, nil, MaxTaskHistoryPayloadBytes)
	if err != nil {
		return err
	}
	next := TaskTypeChange(w)
	if err = next.Validate(); err == nil {
		*v = next
	}
	return err
}

func (v TaskPriorityChange) MarshalJSON() ([]byte, error) {
	type wire TaskPriorityChange
	return checkedLimit(wire(v), v.Validate(), MaxTaskHistoryPayloadBytes)
}
func (v *TaskPriorityChange) UnmarshalJSON(raw []byte) error {
	type wire TaskPriorityChange
	w, err := decodeFieldsLimit[wire](raw, []string{"from", "to"}, nil, nil, MaxTaskHistoryPayloadBytes)
	if err != nil {
		return err
	}
	next := TaskPriorityChange(w)
	if err = next.Validate(); err == nil {
		*v = next
	}
	return err
}

func (v TaskFieldsUpdatedPayload) MarshalJSON() ([]byte, error) {
	type wire TaskFieldsUpdatedPayload
	return checkedLimit(wire(v), v.Validate(), MaxTaskHistoryPayloadBytes)
}
func (v *TaskFieldsUpdatedPayload) UnmarshalJSON(raw []byte) error {
	type wire TaskFieldsUpdatedPayload
	w, err := decodeFieldsLimit[wire](raw, []string{"changed_fields", "type_change", "priority_change", "position"}, nil, []string{"type_change", "priority_change", "position"}, MaxTaskHistoryPayloadBytes)
	if err != nil {
		return err
	}
	next := TaskFieldsUpdatedPayload(w)
	if err = next.Validate(); err == nil {
		*v = next
	}
	return err
}

func (v TaskEvent) MarshalJSON() ([]byte, error) {
	type wire TaskEvent
	return checkedLimit(wire(v), v.Validate(), MaxTaskEventBytes)
}
func (v *TaskEvent) UnmarshalJSON(raw []byte) error {
	type wire TaskEvent
	w, err := decodeFieldsLimit[wire](raw, []string{"id", "project_id", "task_id", "task_version", "type", "actor", "operation_id", "correlation_id", "payload", "created_at"}, nil, nil, MaxTaskEventBytes)
	if err != nil {
		return err
	}
	next := TaskEvent(w)
	if err = next.Validate(); err == nil {
		*v = next
	}
	return err
}

func (v TaskChanged) MarshalJSON() ([]byte, error) {
	type wire TaskChanged
	return checkedLimit(wire(v), v.Validate(), MaxTaskChangedBytes)
}
func (v *TaskChanged) UnmarshalJSON(raw []byte) error {
	type wire TaskChanged
	w, err := decodeFieldsLimit[wire](raw, []string{"command_id", "actor_user_id", "task_event_id", "milestone_id", "sprint_id", "change", "changed_fields", "position"}, nil, []string{"position"}, MaxTaskChangedBytes)
	if err != nil {
		return err
	}
	next := TaskChanged(w)
	if err = next.Validate(); err == nil {
		*v = next
	}
	return err
}
func (v TaskPosition) Format(w fmt.State, r rune)             { taskSafeFormat(w, r) }
func (v TaskPosition) LogValue() slog.Value                   { return taskSafeLog() }
func (v TaskEventActor) Format(w fmt.State, r rune)           { taskSafeFormat(w, r) }
func (v TaskEventActor) LogValue() slog.Value                 { return taskSafeLog() }
func (v TaskCreatedPayload) Format(w fmt.State, r rune)       { taskSafeFormat(w, r) }
func (v TaskCreatedPayload) LogValue() slog.Value             { return taskSafeLog() }
func (v TaskTypeChange) Format(w fmt.State, r rune)           { taskSafeFormat(w, r) }
func (v TaskTypeChange) LogValue() slog.Value                 { return taskSafeLog() }
func (v TaskPriorityChange) Format(w fmt.State, r rune)       { taskSafeFormat(w, r) }
func (v TaskPriorityChange) LogValue() slog.Value             { return taskSafeLog() }
func (v TaskFieldsUpdatedPayload) Format(w fmt.State, r rune) { taskSafeFormat(w, r) }
func (v TaskFieldsUpdatedPayload) LogValue() slog.Value       { return taskSafeLog() }
func (v TaskEvent) Format(w fmt.State, r rune)                { taskSafeFormat(w, r) }
func (v TaskEvent) LogValue() slog.Value                      { return taskSafeLog() }
func (v TaskChanged) Format(w fmt.State, r rune)              { taskSafeFormat(w, r) }
func (v TaskChanged) LogValue() slog.Value                    { return taskSafeLog() }
func (v TaskEvents) Format(w fmt.State, r rune)               { taskSafeFormat(w, r) }
func (v TaskEvents) LogValue() slog.Value                     { return taskSafeLog() }
