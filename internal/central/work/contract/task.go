package contract

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"slices"

	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

type TaskID = f.ID[Task]
type TaskEventID = f.ID[TaskEvent]
type TaskCommand struct{}
type TaskCommandID = f.ID[TaskCommand]

const (
	MaxTaskRequestBytes       = 512 << 10
	MaxTaskResultBytes        = 512 << 10
	MaxTaskFilterBytes        = 16 << 10
	MaxTaskLookupRequestBytes = 16 << 10
	MaxTaskPlanBytes          = 4 << 20
	MaxTaskGroupSize          = 4096
	MaxProjectTasks           = 65536
	MaxTaskPlanTextBytes      = 32768
)

type TaskType string

const (
	TaskTypeFeature TaskType = "feature"
	TaskTypeBug     TaskType = "bug"
	TaskTypeTask    TaskType = "task"
	TaskTypeSpike   TaskType = "spike"
	TaskTypeChore   TaskType = "chore"
)

func (v TaskType) Validate() error {
	switch v {
	case TaskTypeFeature, TaskTypeBug, TaskTypeTask, TaskTypeSpike, TaskTypeChore:
		return nil
	}
	return invalid("/type", "INVALID_TASK_TYPE")
}

type TaskPriority string

const (
	TaskPriorityLow      TaskPriority = "low"
	TaskPriorityMedium   TaskPriority = "medium"
	TaskPriorityHigh     TaskPriority = "high"
	TaskPriorityCritical TaskPriority = "critical"
)

func (v TaskPriority) Validate() error {
	if v.Order() < 0 {
		return invalid("/priority", "INVALID_TASK_PRIORITY")
	}
	return nil
}
func (v TaskPriority) Order() int {
	switch v {
	case TaskPriorityCritical:
		return 0
	case TaskPriorityHigh:
		return 1
	case TaskPriorityMedium:
		return 2
	case TaskPriorityLow:
		return 3
	}
	return -1
}

type TaskState string

const (
	TaskStateBacklog    TaskState = "backlog"
	TaskStateTodo       TaskState = "todo"
	TaskStateInProgress TaskState = "in_progress"
	TaskStateInReview   TaskState = "in_review"
	TaskStateBlocked    TaskState = "blocked"
	TaskStateDone       TaskState = "done"
	TaskStateCancelled  TaskState = "cancelled"
)

func (v TaskState) Order() int {
	switch v {
	case TaskStateBacklog:
		return 0
	case TaskStateTodo:
		return 1
	case TaskStateInProgress:
		return 2
	case TaskStateInReview:
		return 3
	case TaskStateBlocked:
		return 4
	case TaskStateDone:
		return 5
	case TaskStateCancelled:
		return 6
	}
	return -1
}
func (v TaskState) Validate() error {
	if v.Order() < 0 {
		return invalid("/state", "INVALID_TASK_STATE")
	}
	return nil
}
func (v TaskState) RequiresAssignee() bool {
	return v == TaskStateTodo || v == TaskStateInProgress || v == TaskStateInReview
}
func (v TaskState) Terminal() bool { return v == TaskStateDone || v == TaskStateCancelled }

type TaskCommandName string

const (
	TaskCommandCreate  TaskCommandName = "work.task.create"
	TaskCommandUpdate  TaskCommandName = "work.task.update"
	TaskCommandReorder TaskCommandName = "work.task.reorder"
)

func (v TaskCommandName) Validate() error {
	switch v {
	case TaskCommandCreate, TaskCommandUpdate, TaskCommandReorder:
		return nil
	}
	return invalid("/command", "UNKNOWN_COMMAND")
}
func (v TaskCommandName) IsCreate() bool { return v == TaskCommandCreate }

type Task struct {
	ID              TaskID       `json:"id"`
	ProjectID       ProjectID    `json:"project_id"`
	MilestoneID     MilestoneID  `json:"milestone_id"`
	SprintID        SprintID     `json:"sprint_id"`
	Title           string       `json:"title"`
	Description     string       `json:"description"`
	Type            TaskType     `json:"type"`
	Priority        TaskPriority `json:"priority"`
	State           TaskState    `json:"state"`
	AssigneeAgentID *i.AgentID   `json:"assignee_agent_id"`
	Plan            string       `json:"plan"`
	ManualRank      string       `json:"manual_rank"`
	Version         f.Version    `json:"version"`
	CreatedAt       f.Instant    `json:"created_at"`
	UpdatedAt       f.Instant    `json:"updated_at"`
}

func (v Task) Validate() error {
	if v.ID.Validate() != nil || v.ProjectID.Validate() != nil || v.MilestoneID.Validate() != nil || v.SprintID.Validate() != nil || v.Type.Validate() != nil || v.Priority.Validate() != nil || v.State.Validate() != nil || v.Version.Validate() != nil || ValidateRank(v.ManualRank) != nil || !validTimes(v.CreatedAt, v.UpdatedAt) || v.AssigneeAgentID != nil && v.AssigneeAgentID.Validate() != nil || v.State.RequiresAssignee() && v.AssigneeAgentID == nil {
		return invalid("", "INVALID_TASK")
	}
	if err := ValidateTitle(v.Title); err != nil {
		return err
	}
	if err := ValidateDescription(v.Description); err != nil {
		return err
	}
	return ValidateTaskPlan(v.Plan)
}
func ValidateTaskPlan(v string) error {
	if ValidateDescription(v) != nil {
		return invalid("/plan", "INVALID_PLAN")
	}
	return nil
}
func taskClonePtr[T any](v *T) *T {
	if v == nil {
		return nil
	}
	c := *v
	return &c
}
func (v Task) Clone() Task { v.AssigneeAgentID = taskClonePtr(v.AssigneeAgentID); return v }

type TaskCreate struct {
	TaskID          TaskID       `json:"task_id"`
	SprintID        SprintID     `json:"sprint_id"`
	Title           string       `json:"title"`
	Description     string       `json:"description"`
	Type            TaskType     `json:"type"`
	Priority        TaskPriority `json:"priority"`
	InitialState    TaskState    `json:"initial_state,omitempty"`
	AssigneeAgentID *i.AgentID   `json:"assignee_agent_id,omitempty"`
	Plan            string       `json:"plan"`
}

func (v TaskCreate) EffectiveState() TaskState {
	if v.InitialState == "" {
		return TaskStateBacklog
	}
	return v.InitialState
}
func (v TaskCreate) Validate() error {
	if v.TaskID.Validate() != nil || v.SprintID.Validate() != nil || v.Type.Validate() != nil || v.Priority.Validate() != nil || v.EffectiveState().Validate() != nil || v.AssigneeAgentID != nil && v.AssigneeAgentID.Validate() != nil {
		return invalid("", "INVALID_TASK_CREATE")
	}
	if err := ValidateTitle(v.Title); err != nil {
		return err
	}
	if err := ValidateDescription(v.Description); err != nil {
		return err
	}
	return ValidateTaskPlan(v.Plan)
}
func (v TaskCreate) Clone() TaskCreate { v.AssigneeAgentID = taskClonePtr(v.AssigneeAgentID); return v }

type TaskFieldsUpdate struct {
	Title       *string       `json:"title,omitempty"`
	Description *string       `json:"description,omitempty"`
	Type        *TaskType     `json:"type,omitempty"`
	Priority    *TaskPriority `json:"priority,omitempty"`
	Plan        *string       `json:"plan,omitempty"`
}

func (v TaskFieldsUpdate) Validate() error {
	if v.Title == nil && v.Description == nil && v.Type == nil && v.Priority == nil && v.Plan == nil {
		return invalid("", "EMPTY_UPDATE")
	}
	if v.Title != nil {
		if err := ValidateTitle(*v.Title); err != nil {
			return err
		}
	}
	if v.Description != nil {
		if err := ValidateDescription(*v.Description); err != nil {
			return err
		}
	}
	if v.Type != nil {
		if err := v.Type.Validate(); err != nil {
			return err
		}
	}
	if v.Priority != nil {
		if err := v.Priority.Validate(); err != nil {
			return err
		}
	}
	if v.Plan != nil {
		if err := ValidateTaskPlan(*v.Plan); err != nil {
			return err
		}
	}
	return nil
}
func (v TaskFieldsUpdate) Clone() TaskFieldsUpdate {
	v.Title = taskClonePtr(v.Title)
	v.Description = taskClonePtr(v.Description)
	v.Type = taskClonePtr(v.Type)
	v.Priority = taskClonePtr(v.Priority)
	v.Plan = taskClonePtr(v.Plan)
	return v
}

type TaskReorder struct {
	BeforeID *TaskID `json:"before_id,omitempty"`
}

func (v TaskReorder) Validate() error {
	if v.BeforeID != nil && v.BeforeID.Validate() != nil {
		return invalid("/before_id", "INVALID_ID")
	}
	return nil
}
func (v TaskReorder) Clone() TaskReorder { v.BeforeID = taskClonePtr(v.BeforeID); return v }

type TaskAssigneeFilter struct {
	Present bool
	AgentID *i.AgentID
}

func (v TaskAssigneeFilter) Validate() error {
	if !v.Present && v.AgentID != nil || v.AgentID != nil && v.AgentID.Validate() != nil {
		return invalid("/assignee_agent_id", "INVALID_FILTER")
	}
	return nil
}
func (v TaskAssigneeFilter) Clone() TaskAssigneeFilter { v.AgentID = taskClonePtr(v.AgentID); return v }

type TaskFilter struct {
	State           *TaskState         `json:"state,omitempty"`
	Priority        *TaskPriority      `json:"priority,omitempty"`
	Type            *TaskType          `json:"type,omitempty"`
	AssigneeAgentID TaskAssigneeFilter `json:"-"`
	MilestoneID     *MilestoneID       `json:"milestone_id,omitempty"`
	SprintID        *SprintID          `json:"sprint_id,omitempty"`
	Text            *string            `json:"text,omitempty"`
}

func (v TaskFilter) Validate() error {
	if v.State != nil && v.State.Validate() != nil || v.Priority != nil && v.Priority.Validate() != nil || v.Type != nil && v.Type.Validate() != nil || v.AssigneeAgentID.Validate() != nil || v.MilestoneID != nil && v.MilestoneID.Validate() != nil || v.SprintID != nil && v.SprintID.Validate() != nil || v.Text != nil && ValidateTitle(*v.Text) != nil {
		return invalid("", "INVALID_FILTER")
	}
	return nil
}
func (v TaskFilter) Clone() TaskFilter {
	v.State = taskClonePtr(v.State)
	v.Priority = taskClonePtr(v.Priority)
	v.Type = taskClonePtr(v.Type)
	v.AssigneeAgentID = v.AssigneeAgentID.Clone()
	v.MilestoneID = taskClonePtr(v.MilestoneID)
	v.SprintID = taskClonePtr(v.SprintID)
	v.Text = taskClonePtr(v.Text)
	return v
}
func (v TaskFilter) MarshalJSON() ([]byte, error) {
	if err := v.Validate(); err != nil {
		return nil, err
	}
	fields := map[string]any{}
	if v.State != nil {
		fields["state"] = v.State
	}
	if v.Priority != nil {
		fields["priority"] = v.Priority
	}
	if v.Type != nil {
		fields["type"] = v.Type
	}
	if v.AssigneeAgentID.Present {
		fields["assignee_agent_id"] = v.AssigneeAgentID.AgentID
	}
	if v.MilestoneID != nil {
		fields["milestone_id"] = v.MilestoneID
	}
	if v.SprintID != nil {
		fields["sprint_id"] = v.SprintID
	}
	if v.Text != nil {
		fields["text"] = v.Text
	}
	return checkedLimit(fields, nil, MaxTaskFilterBytes)
}
func (v *TaskFilter) UnmarshalJSON(raw []byte) error {
	type wire struct {
		State       *TaskState      `json:"state"`
		Priority    *TaskPriority   `json:"priority"`
		Type        *TaskType       `json:"type"`
		Assignee    json.RawMessage `json:"assignee_agent_id"`
		MilestoneID *MilestoneID    `json:"milestone_id"`
		SprintID    *SprintID       `json:"sprint_id"`
		Text        *string         `json:"text"`
	}
	w, err := decodeFieldsLimit[wire](raw, nil, []string{"state", "priority", "type", "assignee_agent_id", "milestone_id", "sprint_id", "text"}, []string{"assignee_agent_id"}, MaxTaskFilterBytes)
	if err != nil {
		return err
	}
	next := TaskFilter{State: w.State, Priority: w.Priority, Type: w.Type, MilestoneID: w.MilestoneID, SprintID: w.SprintID, Text: w.Text}
	if len(w.Assignee) > 0 {
		next.AssigneeAgentID.Present = true
		if !bytes.Equal(bytes.TrimSpace(w.Assignee), []byte("null")) {
			var id i.AgentID
			if json.Unmarshal(w.Assignee, &id) != nil {
				return invalid("/assignee_agent_id", "INVALID_ID")
			}
			next.AssigneeAgentID.AgentID = &id
		}
	}
	if err = next.Validate(); err == nil {
		*v = next
	}
	return err
}

type TaskMutation struct {
	Task        Task            `json:"task"`
	Changed     bool            `json:"changed"`
	TaskEventID *TaskEventID    `json:"task_event_id"`
	EventIDs    []event.EventID `json:"event_ids"`
}

func (v TaskMutation) Validate() error {
	if err := v.Task.Validate(); err != nil {
		return err
	}
	if v.EventIDs == nil {
		return invalid("/event_ids", "INVALID_RESULT")
	}
	if v.Changed {
		if v.TaskEventID == nil || v.TaskEventID.Validate() != nil || len(v.EventIDs) != 1 || v.EventIDs[0].Validate() != nil {
			return invalid("", "INVALID_RESULT")
		}
	} else if v.TaskEventID != nil || len(v.EventIDs) != 0 {
		return invalid("", "INVALID_RESULT")
	}
	return nil
}
func (v TaskMutation) Clone() TaskMutation {
	v.Task = v.Task.Clone()
	v.TaskEventID = taskClonePtr(v.TaskEventID)
	v.EventIDs = slices.Clone(v.EventIDs)
	return v
}

type TaskCommandLookupRequest struct {
	ProjectID      ProjectID        `json:"project_id"`
	Command        TaskCommandName  `json:"command"`
	IdempotencyKey f.IdempotencyKey `json:"idempotency_key"`
	SemanticDigest f.Digest         `json:"semantic_digest"`
}

func (v TaskCommandLookupRequest) Validate() error {
	if v.ProjectID.Validate() != nil || v.Command.Validate() != nil || v.IdempotencyKey.Validate() != nil || v.SemanticDigest.Validate() != nil {
		return invalid("", "INVALID_LOOKUP")
	}
	return nil
}
func (v TaskCommandLookupRequest) Clone() TaskCommandLookupRequest { return v }

type TaskCommandLookup struct {
	Status  LookupState   `json:"status"`
	Receipt *TaskMutation `json:"receipt"`
}

func (v TaskCommandLookup) Validate() error {
	switch v.Status {
	case LookupCommitted:
		if v.Receipt != nil {
			return v.Receipt.Validate()
		}
	case LookupInProgress, LookupNotObserved:
		if v.Receipt == nil {
			return nil
		}
	}
	return invalid("", "INVALID_LOOKUP_RESULT")
}
func (v TaskCommandLookup) Clone() TaskCommandLookup {
	if v.Receipt != nil {
		c := v.Receipt.Clone()
		v.Receipt = &c
	}
	return v
}

type TaskCommands interface {
	CreateTask(context.Context, i.Actor, f.CommandMeta, ProjectID, TaskCreate) (TaskMutation, error)
	UpdateTask(context.Context, i.Actor, f.CommandMeta, ProjectID, TaskID, TaskFieldsUpdate) (TaskMutation, error)
	ReorderTask(context.Context, i.Actor, f.CommandMeta, ProjectID, TaskID, TaskReorder) (TaskMutation, error)
	LookupTaskCommand(context.Context, i.Actor, TaskCommandLookupRequest) (TaskCommandLookup, error)
}
type TaskReader interface {
	GetTask(context.Context, i.Actor, ProjectID, TaskID) (Task, error)
	ListTasks(context.Context, i.Actor, ProjectID, TaskFilter, f.PageRequest) (f.Page[Task], error)
	HasTasksInSprintInTx(context.Context, f.Tx, i.Actor, ProjectID, SprintID) (bool, error)
}

func TaskIdentity(project ProjectID, name TaskCommandName, key f.IdempotencyKey) (f.CommandIdentity, error) {
	if project.Validate() != nil || name.Validate() != nil || key.Validate() != nil {
		return f.CommandIdentity{}, invalid("", "INVALID_COMMAND")
	}
	return f.NewCommandIdentity("project", []string{project.String()}, string(name), key)
}
func taskCommandDigest(a i.Actor, m f.CommandMeta, p ProjectID, n TaskCommandName, target TaskID, r any) (f.Digest, error) {
	if err := ValidateActor(a); err != nil {
		return "", err
	}
	if m.Validate() != nil || p.Validate() != nil || n.Validate() != nil || target.Validate() != nil || n.IsCreate() != (m.ExpectedVersion == nil) {
		return "", invalid("", "INVALID_COMMAND")
	}
	raw, err := json.Marshal(r)
	if err != nil {
		return "", err
	}
	var request map[string]json.RawMessage
	if json.Unmarshal(raw, &request) != nil {
		return "", invalid("", "INVALID_COMMAND")
	}
	if n == TaskCommandCreate {
		if _, ok := request["initial_state"]; !ok {
			request["initial_state"] = json.RawMessage(`"backlog"`)
		}
		if _, ok := request["assignee_agent_id"]; !ok {
			request["assignee_agent_id"] = json.RawMessage("null")
		}
	}
	if n == TaskCommandReorder {
		if _, ok := request["before_id"]; !ok {
			request["before_id"] = json.RawMessage("null")
		}
	}
	raw, err = json.Marshal(struct {
		Format   string                     `json:"format"`
		Command  TaskCommandName            `json:"command"`
		Project  ProjectID                  `json:"project_id"`
		Target   TaskID                     `json:"target_id"`
		User     string                     `json:"actor_user_id"`
		Expected *f.Version                 `json:"expected_version"`
		Request  map[string]json.RawMessage `json:"request"`
	}{"work-task-planning-command-v1", n, p, target, a.Details().UserID, m.ExpectedVersion, request})
	if err != nil {
		return "", invalid("", "INVALID_COMMAND")
	}
	raw, err = cursor.CanonicalJSON(raw)
	if err != nil {
		return "", invalid("", "INVALID_COMMAND")
	}
	sum := sha256.Sum256(raw)
	return f.Digest("sha256:" + hex.EncodeToString(sum[:])), nil
}
func TaskCreateDigest(a i.Actor, m f.CommandMeta, p ProjectID, r TaskCreate) (f.Digest, error) {
	return taskCommandDigest(a, m, p, TaskCommandCreate, r.TaskID, r)
}
func TaskUpdateDigest(a i.Actor, m f.CommandMeta, p ProjectID, id TaskID, r TaskFieldsUpdate) (f.Digest, error) {
	return taskCommandDigest(a, m, p, TaskCommandUpdate, id, r)
}
func TaskReorderDigest(a i.Actor, m f.CommandMeta, p ProjectID, id TaskID, r TaskReorder) (f.Digest, error) {
	if err := ValidateActor(a); err != nil {
		return "", err
	}
	if r.BeforeID != nil && *r.BeforeID == id {
		return "", invalid("/before_id", "SELF_ANCHOR")
	}
	return taskCommandDigest(a, m, p, TaskCommandReorder, id, r)
}
func taskSafeFormat(w fmt.State, _ rune) { _, _ = io.WriteString(w, "work_task") }
func taskSafeLog() slog.Value            { return slog.StringValue("work_task") }

func (v TaskType) MarshalJSON() ([]byte, error) {
	return checkedLimit(string(v), v.Validate(), MaxTaskFilterBytes)
}
func (v *TaskType) UnmarshalJSON(raw []byte) error {
	if strictRawLimit(raw, MaxTaskFilterBytes) != nil {
		return invalid("", "INVALID_ENCODING")
	}
	var s string
	if json.Unmarshal(raw, &s) != nil {
		return invalid("", "INVALID_ENCODING")
	}
	next := TaskType(s)
	if err := next.Validate(); err != nil {
		return err
	}
	*v = next
	return nil
}

func (v TaskPriority) MarshalJSON() ([]byte, error) {
	return checkedLimit(string(v), v.Validate(), MaxTaskFilterBytes)
}
func (v *TaskPriority) UnmarshalJSON(raw []byte) error {
	if strictRawLimit(raw, MaxTaskFilterBytes) != nil {
		return invalid("", "INVALID_ENCODING")
	}
	var s string
	if json.Unmarshal(raw, &s) != nil {
		return invalid("", "INVALID_ENCODING")
	}
	next := TaskPriority(s)
	if err := next.Validate(); err != nil {
		return err
	}
	*v = next
	return nil
}

func (v TaskState) MarshalJSON() ([]byte, error) {
	return checkedLimit(string(v), v.Validate(), MaxTaskFilterBytes)
}
func (v *TaskState) UnmarshalJSON(raw []byte) error {
	if strictRawLimit(raw, MaxTaskFilterBytes) != nil {
		return invalid("", "INVALID_ENCODING")
	}
	var s string
	if json.Unmarshal(raw, &s) != nil {
		return invalid("", "INVALID_ENCODING")
	}
	next := TaskState(s)
	if err := next.Validate(); err != nil {
		return err
	}
	*v = next
	return nil
}

func (v TaskCommandName) MarshalJSON() ([]byte, error) {
	return checkedLimit(string(v), v.Validate(), MaxTaskFilterBytes)
}
func (v *TaskCommandName) UnmarshalJSON(raw []byte) error {
	if strictRawLimit(raw, MaxTaskFilterBytes) != nil {
		return invalid("", "INVALID_ENCODING")
	}
	var s string
	if json.Unmarshal(raw, &s) != nil {
		return invalid("", "INVALID_ENCODING")
	}
	next := TaskCommandName(s)
	if err := next.Validate(); err != nil {
		return err
	}
	*v = next
	return nil
}

func (v Task) MarshalJSON() ([]byte, error) {
	type wire Task
	return checkedLimit(wire(v), v.Validate(), MaxTaskResultBytes)
}
func (v *Task) UnmarshalJSON(raw []byte) error {
	type wire Task
	w, err := decodeFieldsLimit[wire](raw, []string{"id", "project_id", "milestone_id", "sprint_id", "title", "description", "type", "priority", "state", "assignee_agent_id", "plan", "manual_rank", "version", "created_at", "updated_at"}, nil, []string{"assignee_agent_id"}, MaxTaskResultBytes)
	if err != nil {
		return err
	}
	next := Task(w)
	if err = next.Validate(); err == nil {
		*v = next
	}
	return err
}

func (v TaskCreate) MarshalJSON() ([]byte, error) {
	type wire TaskCreate
	return checkedLimit(wire(v), v.Validate(), MaxTaskRequestBytes)
}
func (v *TaskCreate) UnmarshalJSON(raw []byte) error {
	type wire TaskCreate
	w, err := decodeFieldsLimit[wire](raw, []string{"task_id", "sprint_id", "title", "type", "priority"}, []string{"description", "plan", "initial_state", "assignee_agent_id"}, nil, MaxTaskRequestBytes)
	if err != nil {
		return err
	}
	next := TaskCreate(w)
	if err = next.Validate(); err == nil {
		*v = next
	}
	return err
}

func (v TaskFieldsUpdate) MarshalJSON() ([]byte, error) {
	type wire TaskFieldsUpdate
	return checkedLimit(wire(v), v.Validate(), MaxTaskRequestBytes)
}
func (v *TaskFieldsUpdate) UnmarshalJSON(raw []byte) error {
	type wire TaskFieldsUpdate
	w, err := decodeFieldsLimit[wire](raw, nil, []string{"title", "description", "type", "priority", "plan"}, nil, MaxTaskRequestBytes)
	if err != nil {
		return err
	}
	next := TaskFieldsUpdate(w)
	if err = next.Validate(); err == nil {
		*v = next
	}
	return err
}

func (v TaskReorder) MarshalJSON() ([]byte, error) {
	type wire TaskReorder
	return checkedLimit(wire(v), v.Validate(), MaxTaskRequestBytes)
}
func (v *TaskReorder) UnmarshalJSON(raw []byte) error {
	type wire TaskReorder
	w, err := decodeFieldsLimit[wire](raw, nil, []string{"before_id"}, nil, MaxTaskRequestBytes)
	if err != nil {
		return err
	}
	next := TaskReorder(w)
	if err = next.Validate(); err == nil {
		*v = next
	}
	return err
}

func (v TaskMutation) MarshalJSON() ([]byte, error) {
	type wire TaskMutation
	return checkedLimit(wire(v), v.Validate(), MaxTaskResultBytes)
}
func (v *TaskMutation) UnmarshalJSON(raw []byte) error {
	type wire TaskMutation
	w, err := decodeFieldsLimit[wire](raw, []string{"task", "changed", "task_event_id", "event_ids"}, nil, []string{"task_event_id"}, MaxTaskResultBytes)
	if err != nil {
		return err
	}
	next := TaskMutation(w)
	if err = next.Validate(); err == nil {
		*v = next
	}
	return err
}

func (v TaskCommandLookupRequest) MarshalJSON() ([]byte, error) {
	type wire TaskCommandLookupRequest
	return checkedLimit(wire(v), v.Validate(), MaxTaskLookupRequestBytes)
}
func (v *TaskCommandLookupRequest) UnmarshalJSON(raw []byte) error {
	type wire TaskCommandLookupRequest
	w, err := decodeFieldsLimit[wire](raw, []string{"project_id", "command", "idempotency_key", "semantic_digest"}, nil, nil, MaxTaskLookupRequestBytes)
	if err != nil {
		return err
	}
	next := TaskCommandLookupRequest(w)
	if err = next.Validate(); err == nil {
		*v = next
	}
	return err
}

func (v TaskCommandLookup) MarshalJSON() ([]byte, error) {
	type wire TaskCommandLookup
	return checkedLimit(wire(v), v.Validate(), MaxTaskResultBytes)
}
func (v *TaskCommandLookup) UnmarshalJSON(raw []byte) error {
	type wire TaskCommandLookup
	w, err := decodeFieldsLimit[wire](raw, []string{"status", "receipt"}, nil, []string{"receipt"}, MaxTaskResultBytes)
	if err != nil {
		return err
	}
	next := TaskCommandLookup(w)
	if err = next.Validate(); err == nil {
		*v = next
	}
	return err
}
func (v Task) Format(w fmt.State, r rune)                     { taskSafeFormat(w, r) }
func (v Task) LogValue() slog.Value                           { return taskSafeLog() }
func (v TaskCreate) Format(w fmt.State, r rune)               { taskSafeFormat(w, r) }
func (v TaskCreate) LogValue() slog.Value                     { return taskSafeLog() }
func (v TaskFieldsUpdate) Format(w fmt.State, r rune)         { taskSafeFormat(w, r) }
func (v TaskFieldsUpdate) LogValue() slog.Value               { return taskSafeLog() }
func (v TaskReorder) Format(w fmt.State, r rune)              { taskSafeFormat(w, r) }
func (v TaskReorder) LogValue() slog.Value                    { return taskSafeLog() }
func (v TaskAssigneeFilter) Format(w fmt.State, r rune)       { taskSafeFormat(w, r) }
func (v TaskAssigneeFilter) LogValue() slog.Value             { return taskSafeLog() }
func (v TaskFilter) Format(w fmt.State, r rune)               { taskSafeFormat(w, r) }
func (v TaskFilter) LogValue() slog.Value                     { return taskSafeLog() }
func (v TaskMutation) Format(w fmt.State, r rune)             { taskSafeFormat(w, r) }
func (v TaskMutation) LogValue() slog.Value                   { return taskSafeLog() }
func (v TaskCommandLookupRequest) Format(w fmt.State, r rune) { taskSafeFormat(w, r) }
func (v TaskCommandLookupRequest) LogValue() slog.Value       { return taskSafeLog() }
func (v TaskCommandLookup) Format(w fmt.State, r rune)        { taskSafeFormat(w, r) }
func (v TaskCommandLookup) LogValue() slog.Value              { return taskSafeLog() }
