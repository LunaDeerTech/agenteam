package contract

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"slices"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

type TaskTimelineEventType string

const (
	TaskTimelineCreated         TaskTimelineEventType = "task_created"
	TaskTimelineFieldsUpdated   TaskTimelineEventType = "fields_updated"
	TaskTimelineBlockerAdded    TaskTimelineEventType = "blocker_added"
	TaskTimelineBlockerResolved TaskTimelineEventType = "blocker_resolved"
	TaskTimelineStateChanged    TaskTimelineEventType = "state_changed"
	TaskTimelineAssigneeChanged TaskTimelineEventType = "assignee_changed"
	TaskTimelineComment         TaskTimelineEventType = "comment"
)

var taskTimelineTypes = [...]TaskTimelineEventType{TaskTimelineCreated, TaskTimelineFieldsUpdated, TaskTimelineBlockerAdded, TaskTimelineBlockerResolved, TaskTimelineStateChanged, TaskTimelineAssigneeChanged, TaskTimelineComment}

func (v TaskTimelineEventType) Validate() error {
	if !slices.Contains(taskTimelineTypes[:], v) {
		return invalid("/type", "INVALID_TASK_TIMELINE_TYPE")
	}
	return nil
}
func (v TaskTimelineEventType) MarshalJSON() ([]byte, error) {
	return checkedLimit(string(v), v.Validate(), 64)
}
func (v *TaskTimelineEventType) UnmarshalJSON(raw []byte) error {
	if v == nil {
		return invalid("", "INVALID_ENCODING")
	}
	s, err := taskBlockerString(raw, 64)
	if err != nil {
		return err
	}
	n := TaskTimelineEventType(s)
	if err = n.Validate(); err == nil {
		*v = n
	}
	return err
}

type TaskTimelineOrder string

const (
	TaskTimelineAscending  TaskTimelineOrder = "asc"
	TaskTimelineDescending TaskTimelineOrder = "desc"
)

func (v TaskTimelineOrder) Validate() error {
	if v != TaskTimelineAscending && v != TaskTimelineDescending {
		return invalid("/order", "INVALID_TASK_TIMELINE_ORDER")
	}
	return nil
}
func (v TaskTimelineOrder) MarshalJSON() ([]byte, error) {
	return checkedLimit(string(v), v.Validate(), 64)
}
func (v *TaskTimelineOrder) UnmarshalJSON(raw []byte) error {
	if v == nil {
		return invalid("", "INVALID_ENCODING")
	}
	s, err := taskBlockerString(raw, 64)
	if err != nil {
		return err
	}
	n := TaskTimelineOrder(s)
	if err = n.Validate(); err == nil {
		*v = n
	}
	return err
}

type TaskTimelineFilter struct {
	Types []TaskTimelineEventType `json:"types,omitempty"`
	Order TaskTimelineOrder       `json:"order,omitempty"`
}

func (v TaskTimelineFilter) Validate() error {
	if v.Order != "" && v.Order.Validate() != nil {
		return invalid("/order", "INVALID_TASK_TIMELINE_ORDER")
	}
	if v.Types != nil && (len(v.Types) == 0 || len(v.Types) > len(taskTimelineTypes)) {
		return invalid("/types", "INVALID_TASK_TIMELINE_TYPES")
	}
	for n, kind := range v.Types {
		if kind.Validate() != nil || slices.Contains(v.Types[:n], kind) {
			return invalid("/types", "INVALID_TASK_TIMELINE_TYPES")
		}
	}
	return nil
}
func (v TaskTimelineFilter) Clone() TaskTimelineFilter {
	v.Types = slices.Clone(v.Types)
	return v
}
func (v TaskTimelineFilter) MarshalJSON() ([]byte, error) {
	if err := v.Validate(); err != nil {
		return nil, err
	}
	if v.Order == "" {
		v.Order = TaskTimelineDescending
	}
	types := make([]TaskTimelineEventType, 0, len(v.Types))
	for _, kind := range taskTimelineTypes {
		if slices.Contains(v.Types, kind) {
			types = append(types, kind)
		}
	}
	type wire TaskTimelineFilter
	return checkedLimit(wire{Types: types, Order: v.Order}, nil, MaxTaskEventBytes)
}
func (v *TaskTimelineFilter) UnmarshalJSON(raw []byte) error {
	if v == nil {
		return invalid("", "INVALID_ENCODING")
	}
	type wire TaskTimelineFilter
	w, err := decodeFieldsLimit[wire](raw, nil, []string{"types", "order"}, nil, MaxTaskEventBytes)
	if err != nil {
		return err
	}
	n := TaskTimelineFilter(w)
	if n.Order == "" {
		n.Order = TaskTimelineDescending
	}
	if err = n.Validate(); err == nil {
		*v = n.Clone()
	}
	return err
}
func DecodeTaskTimelineFilter(raw []byte) (TaskTimelineFilter, error) {
	var v TaskTimelineFilter
	err := v.UnmarshalJSON(raw)
	return v, err
}

// The envelope reserve covers the maximum cursor and JSON page framing. The
// reader budgets the entire items array and never truncates an event body.
const (
	MaxTaskTimelineResponseBytes = 4 << 20
	MaxTaskTimelineItemsBytes    = MaxTaskTimelineResponseBytes - (16 << 10)
)

// Exactly one branch is present. JSON is the original ten-field event wire.
// Storage operation columns, not the public event type, establish provenance.
type TaskTimelineEvent struct {
	Planning     *TaskEvent            `json:"-"`
	Blocker      *TaskBlockerEvent     `json:"-"`
	Transition   *TaskTransitionEvent  `json:"-"`
	Claim        *SchedulerTaskEvent   `json:"-"`
	Compensation *TaskBusyTaskEvent    `json:"-"`
	Failure      *TaskFailureTaskEvent `json:"-"`
}

// TaskTimelineEventHeader is a read projection, not an authorization or a new
// event wire format. Header only returns it after validating the whole event.
type TaskTimelineEventHeader struct {
	ID          TaskEventID
	ProjectID   ProjectID
	TaskID      TaskID
	TaskVersion f.Version
	Type        TaskTimelineEventType
	CreatedAt   f.Instant
}

func (v TaskTimelineEvent) Validate() error {
	count := 0
	var err error
	if v.Planning != nil {
		count++
		err = v.Planning.Validate()
	}
	if v.Blocker != nil {
		count++
		err = v.Blocker.Validate()
	}
	if v.Transition != nil {
		count++
		err = v.Transition.Validate()
	}
	if v.Claim != nil {
		count++
		err = v.Claim.Validate()
	}
	if v.Compensation != nil {
		count++
		err = v.Compensation.Validate()
	}
	if v.Failure != nil {
		count++
		err = v.Failure.Validate()
	}
	if count != 1 {
		return invalid("", "INVALID_TASK_TIMELINE_EVENT")
	}
	return err
}
func (v TaskTimelineEvent) Clone() TaskTimelineEvent {
	if v.Planning != nil {
		x := v.Planning.Clone()
		v.Planning = &x
	}
	if v.Blocker != nil {
		x := v.Blocker.Clone()
		v.Blocker = &x
	}
	if v.Transition != nil {
		x := v.Transition.Clone()
		v.Transition = &x
	}
	if v.Claim != nil {
		x := *v.Claim
		v.Claim = &x
	}
	if v.Compensation != nil {
		x := *v.Compensation
		v.Compensation = &x
	}
	if v.Failure != nil {
		x := v.Failure.Clone()
		v.Failure = &x
	}
	return v
}
func (v TaskTimelineEvent) Header() (TaskTimelineEventHeader, error) {
	if err := v.Validate(); err != nil {
		return TaskTimelineEventHeader{}, err
	}
	if e := v.Planning; e != nil {
		return TaskTimelineEventHeader{e.ID, e.ProjectID, e.TaskID, e.TaskVersion, TaskTimelineEventType(e.Type), e.CreatedAt}, nil
	}
	if e := v.Blocker; e != nil {
		return TaskTimelineEventHeader{e.ID, e.ProjectID, e.TaskID, e.TaskVersion, TaskTimelineEventType(e.Type), e.CreatedAt}, nil
	}
	if e := v.Transition; e != nil {
		return TaskTimelineEventHeader{e.ID, e.ProjectID, e.TaskID, e.TaskVersion, TaskTimelineEventType(e.Type), e.CreatedAt}, nil
	}
	if e := v.Claim; e != nil {
		return TaskTimelineEventHeader{e.ID, e.ProjectID, e.TaskID, e.TaskVersion, TaskTimelineEventType(e.Type), e.CreatedAt}, nil
	}
	if e := v.Compensation; e != nil {
		return TaskTimelineEventHeader{e.ID, e.ProjectID, e.TaskID, e.TaskVersion, TaskTimelineEventType(e.Type), e.CreatedAt}, nil
	}
	if e := v.Failure; e != nil {
		return TaskTimelineEventHeader{e.ID, e.ProjectID, e.TaskID, e.TaskVersion, TaskTimelineEventType(e.Type), e.CreatedAt}, nil
	}
	return TaskTimelineEventHeader{}, invalid("", "INVALID_TASK_TIMELINE_EVENT")
}
func (v TaskTimelineEvent) MarshalJSON() ([]byte, error) {
	if err := v.Validate(); err != nil {
		return nil, err
	}
	if v.Planning != nil {
		return v.Planning.MarshalJSON()
	}
	if v.Blocker != nil {
		return v.Blocker.MarshalJSON()
	}
	if v.Transition != nil {
		return v.Transition.MarshalJSON()
	}
	if v.Claim != nil {
		return v.Claim.MarshalJSON()
	}
	if v.Compensation != nil {
		return v.Compensation.MarshalJSON()
	}
	return v.Failure.MarshalJSON()
}
func (v *TaskTimelineEvent) UnmarshalJSON(raw []byte) error {
	if v == nil {
		return invalid("", "INVALID_ENCODING")
	}
	fields, err := taskBlockerFields(raw, MaxTaskTransitionCommentEventBytes, []string{"id", "project_id", "task_id", "task_version", "type", "actor", "operation_id", "correlation_id", "payload", "created_at"}, nil)
	if err != nil {
		return err
	}
	var kind TaskTimelineEventType
	var actor struct {
		Type string `json:"type"`
	}
	var payload struct {
		ReasonCode  string `json:"reason_code"`
		BlockerType string `json:"blocker_type"`
	}
	if json.Unmarshal(fields["type"], &kind) != nil || json.Unmarshal(fields["actor"], &actor) != nil || json.Unmarshal(fields["payload"], &payload) != nil {
		return invalid("", "INVALID_TASK_TIMELINE_EVENT")
	}
	var n TaskTimelineEvent
	if actor.Type == "system" {
		switch payload.ReasonCode {
		case "scheduler_claim":
			n.Claim = new(SchedulerTaskEvent)
			err = n.Claim.UnmarshalJSON(raw)
		case "scheduler_agent_busy_compensation":
			n.Compensation = new(TaskBusyTaskEvent)
			err = n.Compensation.UnmarshalJSON(raw)
		case TaskLaunchFailureHistoryReason:
			n.Failure = new(TaskFailureTaskEvent)
			err = n.Failure.UnmarshalJSON(raw)
		default:
			return invalid("", "INVALID_TASK_TIMELINE_EVENT")
		}
	} else {
		switch kind {
		case TaskTimelineCreated, TaskTimelineFieldsUpdated:
			n.Planning = new(TaskEvent)
			err = n.Planning.UnmarshalJSON(raw)
		case TaskTimelineBlockerAdded, TaskTimelineBlockerResolved:
			// Human blocker records from Transfer and the standalone blocker command
			// have identical public wires. Decode canonically as Blocker; only the
			// SQL operation column can establish the original command family.
			if kind == TaskTimelineBlockerResolved && payload.BlockerType == string(TaskBlockerTechnical) {
				n.Transition = new(TaskTransitionEvent)
				err = n.Transition.UnmarshalJSON(raw)
			} else {
				n.Blocker = new(TaskBlockerEvent)
				err = n.Blocker.UnmarshalJSON(raw)
			}
		case TaskTimelineStateChanged, TaskTimelineAssigneeChanged, TaskTimelineComment:
			n.Transition = new(TaskTransitionEvent)
			err = n.Transition.UnmarshalJSON(raw)
		default:
			return invalid("", "INVALID_TASK_TIMELINE_EVENT")
		}
	}
	if err != nil {
		return err
	}
	if err = n.Validate(); err != nil {
		return err
	}
	*v = n
	return nil
}
func DecodeTaskTimelineEvent(raw []byte) (TaskTimelineEvent, error) {
	var v TaskTimelineEvent
	err := v.UnmarshalJSON(raw)
	return v, err
}

type TaskTimelineReader interface {
	ListTaskEvents(context.Context, i.Actor, ProjectID, TaskID, TaskTimelineFilter, f.PageRequest) (f.Page[TaskTimelineEvent], error)
}

func taskTimelineFormat(w fmt.State, _ rune)               { _, _ = io.WriteString(w, "work_task_timeline") }
func taskTimelineLog() slog.Value                          { return slog.StringValue("work_task_timeline") }
func (v TaskTimelineEventType) Format(w fmt.State, r rune) { taskTimelineFormat(w, r) }
func (v TaskTimelineEventType) LogValue() slog.Value       { return taskTimelineLog() }
func (v TaskTimelineOrder) Format(w fmt.State, r rune)     { taskTimelineFormat(w, r) }
func (v TaskTimelineOrder) LogValue() slog.Value           { return taskTimelineLog() }
func (v TaskTimelineFilter) Format(w fmt.State, r rune)    { taskTimelineFormat(w, r) }
func (v TaskTimelineFilter) LogValue() slog.Value          { return taskTimelineLog() }
func (v TaskTimelineEvent) Format(w fmt.State, r rune)     { taskTimelineFormat(w, r) }
func (v TaskTimelineEvent) LogValue() slog.Value           { return taskTimelineLog() }

func (v TaskTimelineEventHeader) Format(w fmt.State, r rune) { taskTimelineFormat(w, r) }
func (v TaskTimelineEventHeader) LogValue() slog.Value       { return taskTimelineLog() }
