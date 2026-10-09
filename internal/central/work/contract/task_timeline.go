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
)

var taskTimelineTypes = [...]TaskTimelineEventType{TaskTimelineCreated, TaskTimelineFieldsUpdated, TaskTimelineBlockerAdded, TaskTimelineBlockerResolved}

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

// Exactly one branch is present. JSON is the original event wire, without an
// extra wrapper or an enlarged definition of either existing event family.
type TaskTimelineEvent struct {
	Planning *TaskEvent        `json:"-"`
	Blocker  *TaskBlockerEvent `json:"-"`
}

func (v TaskTimelineEvent) Validate() error {
	if v.Planning != nil && v.Blocker == nil {
		return v.Planning.Validate()
	}
	if v.Blocker != nil && v.Planning == nil {
		return v.Blocker.Validate()
	}
	return invalid("", "INVALID_TASK_TIMELINE_EVENT")
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
	return v
}
func (v TaskTimelineEvent) MarshalJSON() ([]byte, error) {
	if err := v.Validate(); err != nil {
		return nil, err
	}
	if v.Planning != nil {
		return v.Planning.MarshalJSON()
	}
	return v.Blocker.MarshalJSON()
}
func (v *TaskTimelineEvent) UnmarshalJSON(raw []byte) error {
	if v == nil {
		return invalid("", "INVALID_ENCODING")
	}
	fields, err := taskBlockerFields(raw, MaxTaskEventBytes, []string{"id", "project_id", "task_id", "task_version", "type", "actor", "operation_id", "correlation_id", "payload", "created_at"}, nil)
	if err != nil {
		return err
	}
	var kind TaskTimelineEventType
	if err = json.Unmarshal(fields["type"], &kind); err != nil {
		return err
	}
	var n TaskTimelineEvent
	switch kind {
	case TaskTimelineCreated, TaskTimelineFieldsUpdated:
		n.Planning = new(TaskEvent)
		err = n.Planning.UnmarshalJSON(raw)
	case TaskTimelineBlockerAdded, TaskTimelineBlockerResolved:
		n.Blocker = new(TaskBlockerEvent)
		err = n.Blocker.UnmarshalJSON(raw)
	}
	if err == nil {
		*v = n
	}
	return err
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
