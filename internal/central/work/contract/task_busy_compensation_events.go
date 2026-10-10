package contract

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"

	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

// Busy compensation has its own closed payload and history codec. Neither
// Human schema 1 nor Scheduler claim schema 2 accepts this transition.
const TaskBusyCompensationSchemaVersion uint32 = 3

type TaskBusyStateChanged struct {
	FromState  TaskState `json:"from_state"`
	ToState    TaskState `json:"to_state"`
	ReasonCode string    `json:"reason_code"`
}

func (v TaskBusyStateChanged) Validate() error {
	if v.FromState != TaskStateInProgress || v.ToState != TaskStateTodo || v.ReasonCode != "scheduler_agent_busy_compensation" {
		return invalid("/payload", "INVALID_TASK_BUSY_TRANSITION")
	}
	return nil
}
func (v TaskBusyStateChanged) MarshalJSON() ([]byte, error) {
	type wire TaskBusyStateChanged
	return checkedLimit(wire(v), v.Validate(), MaxTaskHistoryPayloadBytes)
}
func (v *TaskBusyStateChanged) UnmarshalJSON(raw []byte) error {
	type wire TaskBusyStateChanged
	w, err := decodeFieldsLimit[wire](raw, []string{"from_state", "to_state", "reason_code"}, nil, nil, MaxTaskHistoryPayloadBytes)
	if v == nil || err != nil {
		return invalid("/payload", "INVALID_TASK_BUSY_TRANSITION")
	}
	next := TaskBusyStateChanged(w)
	if err = next.Validate(); err != nil {
		return err
	}
	*v = next
	return nil
}

type TaskBusyTaskEvent struct {
	ID            TaskEventID             `json:"id"`
	ProjectID     ProjectID               `json:"project_id"`
	TaskID        TaskID                  `json:"task_id"`
	TaskVersion   f.Version               `json:"task_version"`
	Type          TaskTransitionEventType `json:"type"`
	Actor         SchedulerTaskActor      `json:"actor"`
	OperationID   SchedulerClaimID        `json:"operation_id"`
	CorrelationID SchedulerClaimID        `json:"correlation_id"`
	Payload       TaskBusyStateChanged    `json:"payload"`
	CreatedAt     f.Instant               `json:"created_at"`
}

func (v TaskBusyTaskEvent) Validate() error {
	if v.ID.Validate() != nil || v.ProjectID.Validate() != nil || v.TaskID.Validate() != nil || v.TaskVersion.Validate() != nil || v.TaskVersion < 2 || v.Type != TaskTransitionStateChanged || v.Actor.Validate() != nil || v.OperationID.Validate() != nil || v.CorrelationID != v.OperationID || v.Actor.CauseID != v.OperationID.String() || v.Payload.Validate() != nil || v.CreatedAt.Validate() != nil {
		return invalid("", "INVALID_TASK_BUSY_HISTORY")
	}
	return nil
}
func (v TaskBusyTaskEvent) MarshalJSON() ([]byte, error) {
	type wire TaskBusyTaskEvent
	return checkedLimit(wire(v), v.Validate(), 16<<10)
}
func (v *TaskBusyTaskEvent) UnmarshalJSON(raw []byte) error {
	type wire TaskBusyTaskEvent
	w, err := decodeFieldsLimit[wire](raw, []string{"id", "project_id", "task_id", "task_version", "type", "actor", "operation_id", "correlation_id", "payload", "created_at"}, nil, nil, 16<<10)
	if v == nil || err != nil {
		return invalid("", "INVALID_TASK_BUSY_HISTORY")
	}
	next := TaskBusyTaskEvent(w)
	if err = next.Validate(); err != nil {
		return err
	}
	*v = next
	return nil
}

type TaskBusyCompensated struct {
	ClaimID        SchedulerClaimID       `json:"claim_id"`
	Actor          SchedulerTaskActor     `json:"actor"`
	TaskEventID    TaskEventID            `json:"task_event_id"`
	MilestoneID    MilestoneID            `json:"milestone_id"`
	SprintID       SprintID               `json:"sprint_id"`
	AgentID        i.AgentID              `json:"agent_id"`
	SourcePosition TaskTransitionPosition `json:"source_position"`
	TargetPosition TaskTransitionPosition `json:"target_position"`
}

func (v TaskBusyCompensated) Validate() error {
	if v.ClaimID.Validate() != nil || v.Actor.Validate() != nil || v.Actor.CauseID != v.ClaimID.String() || v.TaskEventID.Validate() != nil || v.MilestoneID.Validate() != nil || v.SprintID.Validate() != nil || v.AgentID.Validate() != nil || v.SourcePosition.Validate() != nil || v.TargetPosition.Validate() != nil || v.SourcePosition.SprintID != v.SprintID || v.TargetPosition.SprintID != v.SprintID || v.SourcePosition.State != TaskStateInProgress || v.TargetPosition.State != TaskStateTodo || v.SourcePosition.Priority != v.TargetPosition.Priority {
		return invalid("", "INVALID_TASK_BUSY_COMPENSATED")
	}
	return nil
}
func (v TaskBusyCompensated) Clone() TaskBusyCompensated {
	v.SourcePosition = v.SourcePosition.Clone()
	v.TargetPosition = v.TargetPosition.Clone()
	return v
}
func (v TaskBusyCompensated) MarshalJSON() ([]byte, error) {
	type wire TaskBusyCompensated
	return checkedLimit(wire(v), v.Validate(), 16<<10)
}
func (v *TaskBusyCompensated) UnmarshalJSON(raw []byte) error {
	type wire TaskBusyCompensated
	w, err := decodeFieldsLimit[wire](raw, []string{"claim_id", "actor", "task_event_id", "milestone_id", "sprint_id", "agent_id", "source_position", "target_position"}, nil, nil, 16<<10)
	if v == nil || err != nil {
		return invalid("", "INVALID_TASK_BUSY_COMPENSATED")
	}
	next := TaskBusyCompensated(w)
	if err = next.Validate(); err != nil {
		return err
	}
	*v = next
	return nil
}

type TaskBusyCompensationEvents struct {
	catalog *event.Catalog
	claimed event.EventType[TaskBusyCompensated]
}

func RegisterTaskBusyCompensationEvents(catalog *event.Catalog) (TaskBusyCompensationEvents, error) {
	if !catalog.Valid() {
		return TaskBusyCompensationEvents{}, invalid("", "INVALID_CATALOG")
	}
	t, err := event.DefineEvent(catalog, event.Definition[TaskBusyCompensated]{Schema: event.Schema{Producer: WorkProducer, EventType: TaskTransitionedName, AggregateType: TaskAggregate, Version: TaskBusyCompensationSchemaVersion}, Codec: event.JSONCodec[TaskBusyCompensated]{}, Validate: TaskBusyCompensated.Validate})
	if err != nil {
		return TaskBusyCompensationEvents{}, err
	}
	return TaskBusyCompensationEvents{catalog, t}, nil
}
func (v TaskBusyCompensationEvents) Valid() bool {
	return v.catalog.Valid() && v.claimed.Schema() == (event.Schema{Producer: WorkProducer, EventType: TaskTransitionedName, AggregateType: TaskAggregate, Version: TaskBusyCompensationSchemaVersion})
}
func (v TaskBusyCompensationEvents) NewTaskCompensated(h event.Header, p TaskBusyCompensated) (event.Event, error) {
	if !v.Valid() || h.Validate() != nil || h.EventType != TaskTransitionedName || h.AggregateType != TaskAggregate || h.SchemaVersion != TaskBusyCompensationSchemaVersion || h.Scope.Kind != event.ProjectScope || h.AggregateVersion == nil || *h.AggregateVersion < 2 || h.AggregateSequence != nil || p.Validate() != nil {
		return event.Event{}, invalid("", "INVALID_TASK_BUSY_COMPENSATED")
	}
	t, err := f.ParseID[Task](h.AggregateID.String())
	if err != nil || p.SourcePosition.ValidateTarget(t) != nil || p.TargetPosition.ValidateTarget(t) != nil {
		return event.Event{}, invalid("", "INVALID_TASK_BUSY_COMPENSATED")
	}
	return event.NewEvent(v.claimed, h, p.Clone())
}
func (v TaskBusyCompensationEvents) Restore(h event.Header, raw []byte) (event.Event, error) {
	var p TaskBusyCompensated
	if err := json.Unmarshal(raw, &p); err != nil {
		return event.Event{}, err
	}
	return v.NewTaskCompensated(h, p)
}
func (TaskBusyTaskEvent) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "work_task_busy_compensation")
}
func (TaskBusyTaskEvent) LogValue() slog.Value {
	return slog.StringValue("work_task_busy_compensation")
}
