package contract

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

// TaskTransitionActor is the Human history projection, not an authenticated
// Actor or a grant. Agent/System history needs its own authority contract.
type TaskTransitionActor struct {
	UserID identity.UserID `json:"user_id"`
}

func (v TaskTransitionActor) Validate() error {
	if v.UserID.Validate() != nil {
		return invalid("/actor", "INVALID_TASK_TRANSITION_ACTOR")
	}
	return nil
}
func (v TaskTransitionActor) Clone() TaskTransitionActor { return v }
func (v TaskTransitionActor) MarshalJSON() ([]byte, error) {
	return checkedLimit(struct {
		Type   string          `json:"type"`
		UserID identity.UserID `json:"user_id"`
		Source string          `json:"source"`
	}{"human", v.UserID, "task_domain"}, v.Validate(), MaxTaskTransitionActorBytes)
}
func (v *TaskTransitionActor) UnmarshalJSON(raw []byte) error {
	if v == nil {
		return invalid("/actor", "INVALID_TASK_TRANSITION_ACTOR")
	}
	type wire struct {
		Type   string          `json:"type"`
		UserID identity.UserID `json:"user_id"`
		Source string          `json:"source"`
	}
	w, err := decodeFieldsLimit[wire](raw, []string{"type", "user_id", "source"}, nil, nil, MaxTaskTransitionActorBytes)
	if err != nil {
		return err
	}
	next := TaskTransitionActor{UserID: w.UserID}
	if w.Type != "human" || w.Source != "task_domain" {
		return invalid("/actor", "INVALID_TASK_TRANSITION_ACTOR")
	}
	if err := next.Validate(); err != nil {
		return err
	}
	*v = next
	return nil
}
func DecodeTaskTransitionActor(raw []byte) (TaskTransitionActor, error) {
	var v TaskTransitionActor
	err := v.UnmarshalJSON(raw)
	return v, err
}

type TaskTransitionEventType string

const (
	TaskTransitionStateChanged    TaskTransitionEventType = "state_changed"
	TaskTransitionAssigneeChanged TaskTransitionEventType = "assignee_changed"
	TaskTransitionBlockerAdded    TaskTransitionEventType = "blocker_added"
	TaskTransitionBlockerResolved TaskTransitionEventType = "blocker_resolved"
	TaskTransitionComment         TaskTransitionEventType = "comment"
)

func (v TaskTransitionEventType) Validate() error {
	switch v {
	case TaskTransitionStateChanged, TaskTransitionAssigneeChanged, TaskTransitionBlockerAdded, TaskTransitionBlockerResolved, TaskTransitionComment:
		return nil
	}
	return invalid("/type", "INVALID_TASK_TRANSITION_EVENT_TYPE")
}
func (v TaskTransitionEventType) MarshalJSON() ([]byte, error) {
	return checkedLimit(string(v), v.Validate(), MaxTaskTransitionNameBytes)
}
func (v *TaskTransitionEventType) UnmarshalJSON(raw []byte) error {
	if v == nil || strictRawLimit(raw, MaxTaskTransitionNameBytes) != nil {
		return invalid("/type", "INVALID_TASK_TRANSITION_EVENT_TYPE")
	}
	var s string
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) || json.Unmarshal(raw, &s) != nil {
		return invalid("/type", "INVALID_TASK_TRANSITION_EVENT_TYPE")
	}
	next := TaskTransitionEventType(s)
	if err := next.Validate(); err != nil {
		return err
	}
	*v = next
	return nil
}

type TaskStateChangedPayload struct {
	FromState TaskState `json:"from_state"`
	ToState   TaskState `json:"to_state"`
}

func (v TaskStateChangedPayload) Validate() error {
	if CheckTaskTransitionRule(TaskTransitionRuleInput{FromState: v.FromState, ToState: v.ToState, Role: TaskTransitionHumanOwner}) != nil {
		return invalid("/payload", "INVALID_TASK_STATE_CHANGED")
	}
	return nil
}
func (v TaskStateChangedPayload) Clone() TaskStateChangedPayload { return v }
func (v TaskStateChangedPayload) MarshalJSON() ([]byte, error) {
	return checkedLimit(struct {
		FromState  TaskState `json:"from_state"`
		ToState    TaskState `json:"to_state"`
		ReasonCode *string   `json:"reason_code"`
	}{v.FromState, v.ToState, nil}, v.Validate(), MaxTaskHistoryPayloadBytes)
}
func (v *TaskStateChangedPayload) UnmarshalJSON(raw []byte) error {
	if v == nil {
		return invalid("/payload", "INVALID_TASK_STATE_CHANGED")
	}
	type wire struct {
		FromState  TaskState       `json:"from_state"`
		ToState    TaskState       `json:"to_state"`
		ReasonCode json.RawMessage `json:"reason_code"`
	}
	w, err := decodeFieldsLimit[wire](raw, []string{"from_state", "to_state", "reason_code"}, nil, []string{"reason_code"}, MaxTaskHistoryPayloadBytes)
	if err != nil {
		return err
	}
	if !bytes.Equal(bytes.TrimSpace(w.ReasonCode), []byte("null")) {
		return invalid("/reason_code", "INVALID_REASON_CODE")
	}
	next := TaskStateChangedPayload{FromState: w.FromState, ToState: w.ToState}
	if err := next.Validate(); err != nil {
		return err
	}
	*v = next
	return nil
}
func DecodeTaskStateChangedPayload(raw []byte) (TaskStateChangedPayload, error) {
	var v TaskStateChangedPayload
	err := v.UnmarshalJSON(raw)
	return v, err
}

type TaskAssigneeChangedPayload struct {
	FromAgentID *identity.AgentID `json:"from_agent_id"`
	ToAgentID   identity.AgentID  `json:"to_agent_id"`
}

func (v TaskAssigneeChangedPayload) Validate() error {
	if v.ToAgentID.Validate() != nil || v.FromAgentID != nil && (v.FromAgentID.Validate() != nil || *v.FromAgentID == v.ToAgentID) {
		return invalid("/payload", "INVALID_TASK_ASSIGNEE_CHANGED")
	}
	return nil
}
func (v TaskAssigneeChangedPayload) Clone() TaskAssigneeChangedPayload {
	v.FromAgentID = taskClonePtr(v.FromAgentID)
	return v
}
func (v TaskAssigneeChangedPayload) MarshalJSON() ([]byte, error) {
	type wire TaskAssigneeChangedPayload
	return checkedLimit(wire(v), v.Validate(), MaxTaskHistoryPayloadBytes)
}
func (v *TaskAssigneeChangedPayload) UnmarshalJSON(raw []byte) error {
	if v == nil {
		return invalid("/payload", "INVALID_TASK_ASSIGNEE_CHANGED")
	}
	type wire TaskAssigneeChangedPayload
	w, err := decodeFieldsLimit[wire](raw, []string{"from_agent_id", "to_agent_id"}, nil, []string{"from_agent_id"}, MaxTaskHistoryPayloadBytes)
	if err != nil {
		return err
	}
	next := TaskAssigneeChangedPayload(w)
	if err := next.Validate(); err != nil {
		return err
	}
	*v = next
	return nil
}
func DecodeTaskAssigneeChangedPayload(raw []byte) (TaskAssigneeChangedPayload, error) {
	var v TaskAssigneeChangedPayload
	err := v.UnmarshalJSON(raw)
	return v, err
}

type TaskCommentPayload struct {
	Body string `json:"body"`
}

func (v TaskCommentPayload) Validate() error {
	return validateTaskTransitionComment(v.Body, "/body")
}
func (v TaskCommentPayload) Clone() TaskCommentPayload { return v }
func (v TaskCommentPayload) MarshalJSON() ([]byte, error) {
	type wire TaskCommentPayload
	return checkedLimit(wire(v), v.Validate(), MaxTaskCommentPayloadBytes)
}
func (v *TaskCommentPayload) UnmarshalJSON(raw []byte) error {
	if v == nil {
		return invalid("/body", "INVALID_COMMENT")
	}
	type wire TaskCommentPayload
	w, err := decodeFieldsLimit[wire](raw, []string{"body"}, nil, nil, MaxTaskCommentPayloadBytes)
	if err != nil {
		return err
	}
	next := TaskCommentPayload(w)
	if err := next.Validate(); err != nil {
		return err
	}
	*v = next
	return nil
}
func DecodeTaskCommentPayload(raw []byte) (TaskCommentPayload, error) {
	var v TaskCommentPayload
	err := v.UnmarshalJSON(raw)
	return v, err
}

// TaskTransitionFactPayload is selected by the enclosing history type. It has
// no standalone wire representation and proves no complete mutation or write.
type TaskTransitionFactPayload struct {
	StateChanged    *TaskStateChangedPayload
	AssigneeChanged *TaskAssigneeChangedPayload
	BlockerAdded    *TaskBlockerAddedPayload
	BlockerResolved *TaskBlockerResolvedPayload
	Comment         *TaskCommentPayload
}

func (v TaskTransitionFactPayload) ValidateFor(kind TaskTransitionEventType) error {
	if err := kind.Validate(); err != nil {
		return err
	}
	count := 0
	for _, present := range []bool{v.StateChanged != nil, v.AssigneeChanged != nil, v.BlockerAdded != nil, v.BlockerResolved != nil, v.Comment != nil} {
		if present {
			count++
		}
	}
	if count != 1 {
		return invalid("/payload", "INVALID_TASK_TRANSITION_PAYLOAD")
	}
	switch kind {
	case TaskTransitionStateChanged:
		if v.StateChanged != nil {
			return v.StateChanged.Validate()
		}
	case TaskTransitionAssigneeChanged:
		if v.AssigneeChanged != nil {
			return v.AssigneeChanged.Validate()
		}
	case TaskTransitionBlockerAdded:
		if v.BlockerAdded != nil {
			return v.BlockerAdded.Validate()
		}
	case TaskTransitionBlockerResolved:
		if v.BlockerResolved != nil && v.BlockerResolved.ResolutionComment == nil {
			return v.BlockerResolved.Validate()
		}
	case TaskTransitionComment:
		if v.Comment != nil {
			return v.Comment.Validate()
		}
	}
	return invalid("/payload", "INVALID_TASK_TRANSITION_PAYLOAD")
}
func (v TaskTransitionFactPayload) Clone() TaskTransitionFactPayload {
	v.StateChanged = taskClonePtr(v.StateChanged)
	if v.AssigneeChanged != nil {
		copy := v.AssigneeChanged.Clone()
		v.AssigneeChanged = &copy
	}
	v.BlockerAdded = taskClonePtr(v.BlockerAdded)
	if v.BlockerResolved != nil {
		copy := v.BlockerResolved.Clone()
		v.BlockerResolved = &copy
	}
	v.Comment = taskClonePtr(v.Comment)
	return v
}

type TaskTransitionEvent struct {
	ID            TaskEventID               `json:"id"`
	ProjectID     ProjectID                 `json:"project_id"`
	TaskID        TaskID                    `json:"task_id"`
	TaskVersion   f.Version                 `json:"task_version"`
	Type          TaskTransitionEventType   `json:"type"`
	Actor         TaskTransitionActor       `json:"actor"`
	OperationID   TaskTransitionCommandID   `json:"operation_id"`
	CorrelationID TaskTransitionCommandID   `json:"correlation_id"`
	Payload       TaskTransitionFactPayload `json:"-"`
	CreatedAt     f.Instant                 `json:"created_at"`
}

func (v TaskTransitionEvent) Validate() error {
	if v.ID.Validate() != nil || v.ProjectID.Validate() != nil || v.TaskID.Validate() != nil || v.TaskVersion.Validate() != nil || v.TaskVersion < 2 || v.OperationID.Validate() != nil || v.CorrelationID != v.OperationID || v.CreatedAt.Validate() != nil {
		return invalid("", "INVALID_TASK_TRANSITION_EVENT")
	}
	if err := v.Type.Validate(); err != nil {
		return err
	}
	if err := v.Actor.Validate(); err != nil {
		return err
	}
	return v.Payload.ValidateFor(v.Type)
}
func (v TaskTransitionEvent) Clone() TaskTransitionEvent {
	v.Payload = v.Payload.Clone()
	return v
}

// RawMessage appears only in this private wire projection. Public successful
// values always carry one concrete typed branch; no unknown payload is retained.
type taskTransitionHistoryWire struct {
	ID            TaskEventID             `json:"id"`
	ProjectID     ProjectID               `json:"project_id"`
	TaskID        TaskID                  `json:"task_id"`
	TaskVersion   f.Version               `json:"task_version"`
	Type          TaskTransitionEventType `json:"type"`
	Actor         json.RawMessage         `json:"actor"`
	OperationID   TaskTransitionCommandID `json:"operation_id"`
	CorrelationID TaskTransitionCommandID `json:"correlation_id"`
	Payload       json.RawMessage         `json:"payload"`
	CreatedAt     f.Instant               `json:"created_at"`
}

func (v TaskTransitionEvent) MarshalJSON() ([]byte, error) {
	if err := v.Validate(); err != nil {
		return nil, err
	}
	actor, err := v.Actor.MarshalJSON()
	if err != nil {
		return nil, err
	}
	var payload []byte
	switch v.Type {
	case TaskTransitionStateChanged:
		payload, err = v.Payload.StateChanged.MarshalJSON()
	case TaskTransitionAssigneeChanged:
		payload, err = v.Payload.AssigneeChanged.MarshalJSON()
	case TaskTransitionBlockerAdded:
		payload, err = v.Payload.BlockerAdded.MarshalJSON()
	case TaskTransitionBlockerResolved:
		payload, err = v.Payload.BlockerResolved.MarshalJSON()
	case TaskTransitionComment:
		payload, err = v.Payload.Comment.MarshalJSON()
	}
	if err != nil {
		return nil, err
	}
	limit := MaxTaskTransitionEventBytes
	if v.Type == TaskTransitionComment {
		limit = MaxTaskTransitionCommentEventBytes
	}
	return checkedLimit(taskTransitionHistoryWire{
		ID: v.ID, ProjectID: v.ProjectID, TaskID: v.TaskID, TaskVersion: v.TaskVersion,
		Type: v.Type, Actor: actor, OperationID: v.OperationID, CorrelationID: v.CorrelationID,
		Payload: payload, CreatedAt: v.CreatedAt,
	}, nil, limit)
}
func (v *TaskTransitionEvent) UnmarshalJSON(raw []byte) error {
	if v == nil {
		return invalid("", "INVALID_TASK_TRANSITION_EVENT")
	}
	w, err := decodeFieldsLimit[taskTransitionHistoryWire](raw,
		[]string{"id", "project_id", "task_id", "task_version", "type", "actor", "operation_id", "correlation_id", "payload", "created_at"},
		nil, nil, MaxTaskTransitionCommentEventBytes)
	if err != nil {
		return err
	}
	if w.Type != TaskTransitionComment && len(raw) > MaxTaskTransitionEventBytes {
		return invalid("", "INVALID_ENCODING")
	}
	next := TaskTransitionEvent{
		ID: w.ID, ProjectID: w.ProjectID, TaskID: w.TaskID, TaskVersion: w.TaskVersion,
		Type: w.Type, OperationID: w.OperationID, CorrelationID: w.CorrelationID, CreatedAt: w.CreatedAt,
	}
	// Preserve the original nested token ranges, including internal whitespace.
	if err = next.Actor.UnmarshalJSON(w.Actor); err != nil {
		return err
	}
	switch w.Type {
	case TaskTransitionStateChanged:
		next.Payload.StateChanged = new(TaskStateChangedPayload)
		err = next.Payload.StateChanged.UnmarshalJSON(w.Payload)
	case TaskTransitionAssigneeChanged:
		next.Payload.AssigneeChanged = new(TaskAssigneeChangedPayload)
		err = next.Payload.AssigneeChanged.UnmarshalJSON(w.Payload)
	case TaskTransitionBlockerAdded:
		next.Payload.BlockerAdded = new(TaskBlockerAddedPayload)
		err = next.Payload.BlockerAdded.UnmarshalJSON(w.Payload)
	case TaskTransitionBlockerResolved:
		next.Payload.BlockerResolved = new(TaskBlockerResolvedPayload)
		err = next.Payload.BlockerResolved.UnmarshalJSON(w.Payload)
	case TaskTransitionComment:
		next.Payload.Comment = new(TaskCommentPayload)
		err = next.Payload.Comment.UnmarshalJSON(w.Payload)
	}
	if err != nil {
		return err
	}
	if err = next.Validate(); err != nil {
		return err
	}
	*v = next
	return nil
}
func DecodeTaskTransitionEvent(raw []byte) (TaskTransitionEvent, error) {
	var v TaskTransitionEvent
	err := v.UnmarshalJSON(raw)
	return v, err
}

func (TaskTransitionActor) Format(w fmt.State, _ rune)        { taskTransitionSafeFormat(w) }
func (TaskTransitionActor) LogValue() slog.Value              { return slog.StringValue("work_task_transition") }
func (TaskTransitionEventType) Format(w fmt.State, _ rune)    { taskTransitionSafeFormat(w) }
func (TaskTransitionEventType) LogValue() slog.Value          { return slog.StringValue("work_task_transition") }
func (TaskStateChangedPayload) Format(w fmt.State, _ rune)    { taskTransitionSafeFormat(w) }
func (TaskStateChangedPayload) LogValue() slog.Value          { return slog.StringValue("work_task_transition") }
func (TaskAssigneeChangedPayload) Format(w fmt.State, _ rune) { taskTransitionSafeFormat(w) }
func (TaskAssigneeChangedPayload) LogValue() slog.Value {
	return slog.StringValue("work_task_transition")
}
func (TaskCommentPayload) Format(w fmt.State, _ rune)        { taskTransitionSafeFormat(w) }
func (TaskCommentPayload) LogValue() slog.Value              { return slog.StringValue("work_task_transition") }
func (TaskTransitionFactPayload) Format(w fmt.State, _ rune) { taskTransitionSafeFormat(w) }
func (TaskTransitionFactPayload) LogValue() slog.Value {
	return slog.StringValue("work_task_transition")
}
func (TaskTransitionEvent) Format(w fmt.State, _ rune) { taskTransitionSafeFormat(w) }
func (TaskTransitionEvent) LogValue() slog.Value       { return slog.StringValue("work_task_transition") }
