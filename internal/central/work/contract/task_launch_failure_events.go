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

const TaskLaunchFailureSchemaVersion uint32 = 4
const TaskLaunchFailureHistoryReason = "scheduler_launch_failed"

type TaskFailureBlockerAdded struct {
	BlockerID   TaskBlockerID   `json:"blocker_id"`
	BlockerType TaskBlockerType `json:"blocker_type"`
	ReasonCode  string          `json:"reason_code"`
}

func (v TaskFailureBlockerAdded) Validate() error {
	if v.BlockerID.Validate() != nil || v.BlockerType != TaskBlockerTechnical || v.ReasonCode != TaskLaunchFailureHistoryReason {
		return invalid("/payload", "INVALID_TASK_FAILURE_HISTORY")
	}
	return nil
}
func (v TaskFailureBlockerAdded) MarshalJSON() ([]byte, error) {
	type wire TaskFailureBlockerAdded
	return checkedLimit(wire(v), v.Validate(), MaxTaskHistoryPayloadBytes)
}
func (v *TaskFailureBlockerAdded) UnmarshalJSON(raw []byte) error {
	type wire TaskFailureBlockerAdded
	w, e := decodeFieldsLimit[wire](raw, []string{"blocker_id", "blocker_type", "reason_code"}, nil, nil, MaxTaskHistoryPayloadBytes)
	if v == nil || e != nil {
		return invalid("/payload", "INVALID_TASK_FAILURE_HISTORY")
	}
	n := TaskFailureBlockerAdded(w)
	if e = n.Validate(); e != nil {
		return e
	}
	*v = n
	return nil
}

type TaskFailureStateChanged struct {
	FromState  TaskState `json:"from_state"`
	ToState    TaskState `json:"to_state"`
	ReasonCode string    `json:"reason_code"`
}

func (v TaskFailureStateChanged) Validate() error {
	if v.FromState != TaskStateInProgress || v.ToState != TaskStateBlocked || v.ReasonCode != TaskLaunchFailureHistoryReason {
		return invalid("/payload", "INVALID_TASK_FAILURE_HISTORY")
	}
	return nil
}
func (v TaskFailureStateChanged) MarshalJSON() ([]byte, error) {
	type wire TaskFailureStateChanged
	return checkedLimit(wire(v), v.Validate(), MaxTaskHistoryPayloadBytes)
}
func (v *TaskFailureStateChanged) UnmarshalJSON(raw []byte) error {
	type wire TaskFailureStateChanged
	w, e := decodeFieldsLimit[wire](raw, []string{"from_state", "to_state", "reason_code"}, nil, nil, MaxTaskHistoryPayloadBytes)
	if v == nil || e != nil {
		return invalid("/payload", "INVALID_TASK_FAILURE_HISTORY")
	}
	n := TaskFailureStateChanged(w)
	if e = n.Validate(); e != nil {
		return e
	}
	*v = n
	return nil
}

// Separate tagged history: neither Human records nor claim/Busy records can
// decode a final technical failure by falling back after an actor error.
type TaskFailureTaskEvent struct {
	ID            TaskEventID              `json:"id"`
	ProjectID     ProjectID                `json:"project_id"`
	TaskID        TaskID                   `json:"task_id"`
	TaskVersion   f.Version                `json:"task_version"`
	Type          string                   `json:"type"`
	Actor         SchedulerTaskActor       `json:"actor"`
	OperationID   SchedulerClaimID         `json:"operation_id"`
	CorrelationID SchedulerClaimID         `json:"correlation_id"`
	Blocker       *TaskFailureBlockerAdded `json:"-"`
	State         *TaskFailureStateChanged `json:"-"`
	CreatedAt     f.Instant                `json:"created_at"`
}

func (v TaskFailureTaskEvent) Validate() error {
	if v.ID.Validate() != nil || v.ProjectID.Validate() != nil || v.TaskID.Validate() != nil || v.TaskVersion < 2 || v.TaskVersion.Validate() != nil || v.Actor.Validate() != nil || v.OperationID.Validate() != nil || v.CorrelationID != v.OperationID || v.Actor.CauseID != v.OperationID.String() || v.CreatedAt.Validate() != nil {
		return invalid("", "INVALID_TASK_FAILURE_HISTORY")
	}
	switch v.Type {
	case "blocker_added":
		if v.Blocker == nil || v.State != nil || v.Blocker.Validate() != nil {
			return invalid("", "INVALID_TASK_FAILURE_HISTORY")
		}
	case "state_changed":
		if v.State == nil || v.Blocker != nil || v.State.Validate() != nil {
			return invalid("", "INVALID_TASK_FAILURE_HISTORY")
		}
	default:
		return invalid("", "INVALID_TASK_FAILURE_HISTORY")
	}
	return nil
}
func (v TaskFailureTaskEvent) MarshalJSON() ([]byte, error) {
	if e := v.Validate(); e != nil {
		return nil, e
	}
	type wire TaskFailureTaskEvent
	var payload any = v.Blocker
	if v.Type == "state_changed" {
		payload = v.State
	}
	return checkedLimit(struct {
		wire
		Payload any `json:"payload"`
	}{wire(v), payload}, nil, 16<<10)
}
func (v *TaskFailureTaskEvent) UnmarshalJSON(raw []byte) error {
	fields, e := taskBlockerFields(raw, 16<<10, []string{"id", "project_id", "task_id", "task_version", "type", "actor", "operation_id", "correlation_id", "payload", "created_at"}, nil)
	if v == nil || e != nil {
		return invalid("", "INVALID_TASK_FAILURE_HISTORY")
	}
	type wire TaskFailureTaskEvent
	var w wire
	if json.Unmarshal(raw, &w) != nil {
		return invalid("", "INVALID_TASK_FAILURE_HISTORY")
	}
	n := TaskFailureTaskEvent(w)
	switch n.Type {
	case "blocker_added":
		n.Blocker = new(TaskFailureBlockerAdded)
		e = json.Unmarshal(fields["payload"], n.Blocker)
	case "state_changed":
		n.State = new(TaskFailureStateChanged)
		e = json.Unmarshal(fields["payload"], n.State)
	default:
		return invalid("", "INVALID_TASK_FAILURE_HISTORY")
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
func (v TaskFailureTaskEvent) Clone() TaskFailureTaskEvent {
	v.Blocker = taskClonePtr(v.Blocker)
	v.State = taskClonePtr(v.State)
	return v
}
func (TaskFailureTaskEvent) Format(w fmt.State, r rune) { blockerSafeFormat(w, r) }
func (TaskFailureTaskEvent) LogValue() slog.Value       { return blockerSafeLog() }

type TaskLaunchFailed struct {
	ClaimID        SchedulerClaimID        `json:"claim_id"`
	Actor          SchedulerTaskActor      `json:"actor"`
	BlockerID      TaskBlockerID           `json:"blocker_id"`
	TaskEventIDs   []TaskEventID           `json:"task_event_ids"`
	MilestoneID    MilestoneID             `json:"milestone_id"`
	SprintID       SprintID                `json:"sprint_id"`
	AgentID        i.AgentID               `json:"agent_id"`
	FromState      TaskState               `json:"from_state"`
	ToState        TaskState               `json:"to_state"`
	Reason         TaskLaunchFailureReason `json:"reason"`
	SourcePosition *TaskTransitionPosition `json:"source_position"`
	TargetPosition *TaskTransitionPosition `json:"target_position"`
}

func (v TaskLaunchFailed) Validate() error {
	if v.ClaimID.Validate() != nil || v.Actor.Validate() != nil || v.Actor.CauseID != v.ClaimID.String() || v.BlockerID.Validate() != nil || v.MilestoneID.Validate() != nil || v.SprintID.Validate() != nil || v.AgentID.Validate() != nil || v.Reason.Validate() != nil || v.ToState != TaskStateBlocked {
		return invalid("", "INVALID_TASK_LAUNCH_FAILED")
	}
	if v.FromState == TaskStateBlocked {
		if len(v.TaskEventIDs) != 1 || v.SourcePosition != nil || v.TargetPosition != nil {
			return invalid("", "INVALID_TASK_LAUNCH_FAILED")
		}
	} else if v.FromState == TaskStateInProgress {
		if len(v.TaskEventIDs) != 2 || v.SourcePosition == nil || v.TargetPosition == nil || v.SourcePosition.Validate() != nil || v.TargetPosition.Validate() != nil || v.SourcePosition.State != v.FromState || v.TargetPosition.State != v.ToState || v.SourcePosition.SprintID != v.SprintID || v.TargetPosition.SprintID != v.SprintID || v.SourcePosition.Priority != v.TargetPosition.Priority {
			return invalid("", "INVALID_TASK_LAUNCH_FAILED")
		}
	} else {
		return invalid("", "INVALID_TASK_LAUNCH_FAILED")
	}
	for n, id := range v.TaskEventIDs {
		if id.Validate() != nil || (n > 0 && v.TaskEventIDs[n-1].String() >= id.String()) {
			return invalid("", "INVALID_TASK_LAUNCH_FAILED")
		}
	}
	return nil
}
func (v TaskLaunchFailed) Clone() TaskLaunchFailed {
	v.TaskEventIDs = slices.Clone(v.TaskEventIDs)
	if v.SourcePosition != nil {
		x := v.SourcePosition.Clone()
		v.SourcePosition = &x
	}
	if v.TargetPosition != nil {
		x := v.TargetPosition.Clone()
		v.TargetPosition = &x
	}
	return v
}
func (v TaskLaunchFailed) MarshalJSON() ([]byte, error) {
	type wire TaskLaunchFailed
	return checkedLimit(wire(v), v.Validate(), 16<<10)
}
func (v *TaskLaunchFailed) UnmarshalJSON(raw []byte) error {
	type wire TaskLaunchFailed
	w, e := decodeFieldsLimit[wire](raw, []string{"claim_id", "actor", "blocker_id", "task_event_ids", "milestone_id", "sprint_id", "agent_id", "from_state", "to_state", "reason", "source_position", "target_position"}, nil, []string{"source_position", "target_position"}, 16<<10)
	if v == nil || e != nil {
		return invalid("", "INVALID_TASK_LAUNCH_FAILED")
	}
	n := TaskLaunchFailed(w)
	if e = n.Validate(); e != nil {
		return e
	}
	*v = n
	return nil
}

type TaskLaunchFailureEvents struct {
	catalog *event.Catalog
	failed  event.EventType[TaskLaunchFailed]
}

func RegisterTaskLaunchFailureEvents(catalog *event.Catalog) (TaskLaunchFailureEvents, error) {
	if !catalog.Valid() {
		return TaskLaunchFailureEvents{}, invalid("", "INVALID_CATALOG")
	}
	t, e := event.DefineEvent(catalog, event.Definition[TaskLaunchFailed]{Schema: event.Schema{Producer: WorkProducer, EventType: TaskTransitionedName, AggregateType: TaskAggregate, Version: TaskLaunchFailureSchemaVersion}, Codec: event.JSONCodec[TaskLaunchFailed]{}, Validate: TaskLaunchFailed.Validate})
	if e != nil {
		return TaskLaunchFailureEvents{}, e
	}
	return TaskLaunchFailureEvents{catalog, t}, nil
}
func (v TaskLaunchFailureEvents) Valid() bool {
	return v.catalog.Valid() && v.failed.Schema() == (event.Schema{Producer: WorkProducer, EventType: TaskTransitionedName, AggregateType: TaskAggregate, Version: TaskLaunchFailureSchemaVersion})
}
func (v TaskLaunchFailureEvents) NewTaskLaunchFailed(h event.Header, p TaskLaunchFailed) (event.Event, error) {
	if !v.Valid() || h.Validate() != nil || h.EventType != TaskTransitionedName || h.AggregateType != TaskAggregate || h.SchemaVersion != TaskLaunchFailureSchemaVersion || h.Scope.Kind != event.ProjectScope || h.AggregateVersion == nil || *h.AggregateVersion < 2 || h.AggregateSequence != nil || p.Validate() != nil {
		return event.Event{}, invalid("", "INVALID_TASK_LAUNCH_FAILED")
	}
	id, e := f.ParseID[Task](h.AggregateID.String())
	if e != nil {
		return event.Event{}, invalid("", "INVALID_TASK_LAUNCH_FAILED")
	}
	if p.SourcePosition != nil && (p.SourcePosition.ValidateTarget(id) != nil || p.TargetPosition.ValidateTarget(id) != nil) {
		return event.Event{}, invalid("", "INVALID_TASK_LAUNCH_FAILED")
	}
	return event.NewEvent(v.failed, h, p.Clone())
}
func (v TaskLaunchFailureEvents) Restore(h event.Header, raw []byte) (event.Event, error) {
	var p TaskLaunchFailed
	if e := json.Unmarshal(raw, &p); e != nil {
		return event.Event{}, e
	}
	return v.NewTaskLaunchFailed(h, p)
}
func (TaskLaunchFailed) Format(w fmt.State, r rune) { blockerSafeFormat(w, r) }
func (TaskLaunchFailed) LogValue() slog.Value       { return blockerSafeLog() }
