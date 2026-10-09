package contract

import (
	"fmt"
	"io"
	"log/slog"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

// TaskTransitionRole selects a pure rule, not an authorization capability or a
// wire value. Current authority and internal causes belong to the calling port.
type TaskTransitionRole uint8

const (
	TaskTransitionHumanOwner TaskTransitionRole = iota + 1
	TaskTransitionAgentRun
	TaskTransitionSystemBlock
	TaskTransitionSchedulerClaim
	TaskTransitionAgentBusyCompensation
	TaskTransitionSchedulerReconcile
)

// TaskTransitionRuleInput is controlled Go input, never a public decoder target.
// CurrentAssigneeAgentID must come from the current transaction's Task preimage;
// a captured execution identity or a prior grant cannot establish that fact.
// A nil assignee supplies no fact about the persisted Task.
type TaskTransitionRuleInput struct {
	FromState              TaskState
	ToState                TaskState
	Role                   TaskTransitionRole
	ActorAgentID           identity.AgentID
	CurrentAssigneeAgentID *identity.AgentID
}

// CheckTaskTransitionRule checks only input shape, the state edge and its role.
// Success does not establish current authority, a valid Task/Agent/Blocker,
// occupancy, a claim, or permission to commit. The caller must prove those facts
// through its real ports and preserve service-level replay/error ordering.
func CheckTaskTransitionRule(in TaskTransitionRuleInput) error {
	if in.FromState.Validate() != nil {
		return invalid("/from_state", "INVALID_TASK_TRANSITION_INPUT")
	}
	if in.ToState.Validate() != nil {
		return invalid("/to_state", "INVALID_TASK_TRANSITION_INPUT")
	}
	if in.Role < TaskTransitionHumanOwner || in.Role > TaskTransitionSchedulerReconcile {
		return invalid("/role", "INVALID_TASK_TRANSITION_INPUT")
	}
	if in.Role == TaskTransitionAgentRun && in.ActorAgentID.Validate() != nil ||
		in.Role != TaskTransitionAgentRun && in.ActorAgentID != (identity.AgentID{}) {
		return invalid("/actor_agent_id", "INVALID_TASK_TRANSITION_INPUT")
	}
	if in.CurrentAssigneeAgentID != nil && in.CurrentAssigneeAgentID.Validate() != nil {
		return invalid("/current_assignee_agent_id", "INVALID_TASK_TRANSITION_INPUT")
	}
	if in.FromState.Terminal() {
		return f.NewFault(f.TaskTerminalImmutable, f.NotStarted)
	}
	roles := taskTransitionRoles(in.FromState, in.ToState)
	if roles == 0 {
		return f.NewFault(f.InvalidState, f.NotStarted)
	}
	if roles&(1<<in.Role) == 0 {
		return f.NewFault(f.Forbidden, f.NotStarted)
	}
	if in.Role == TaskTransitionAgentRun && in.FromState == TaskStateInReview &&
		(in.ToState == TaskStateDone || in.ToState == TaskStateTodo || in.ToState == TaskStateBlocked) &&
		(in.CurrentAssigneeAgentID == nil || *in.CurrentAssigneeAgentID != in.ActorAgentID) {
		return f.NewFault(f.Forbidden, f.NotStarted)
	}
	return nil
}

func taskTransitionRoles(from, to TaskState) uint8 {
	const humanAgent = 1<<TaskTransitionHumanOwner | 1<<TaskTransitionAgentRun
	switch from {
	case TaskStateBacklog:
		if to == TaskStateTodo || to == TaskStateCancelled {
			return humanAgent
		}
	case TaskStateTodo:
		switch to {
		case TaskStateInProgress:
			return 1 << TaskTransitionSchedulerClaim
		case TaskStateBlocked:
			return humanAgent | 1<<TaskTransitionSystemBlock
		case TaskStateCancelled:
			return humanAgent
		}
	case TaskStateInProgress:
		switch to {
		case TaskStateTodo:
			return 1 << TaskTransitionAgentBusyCompensation
		case TaskStateInReview, TaskStateCancelled:
			return humanAgent
		case TaskStateBlocked:
			return humanAgent | 1<<TaskTransitionSystemBlock
		}
	case TaskStateInReview:
		switch to {
		case TaskStateDone, TaskStateTodo, TaskStateCancelled:
			return humanAgent
		case TaskStateBlocked:
			return humanAgent | 1<<TaskTransitionSystemBlock
		}
	case TaskStateBlocked:
		switch to {
		case TaskStateTodo:
			return humanAgent | 1<<TaskTransitionSchedulerReconcile
		case TaskStateCancelled:
			return humanAgent
		}
	}
	return 0
}

// TaskTransitionPosition describes a logical slot in any Task state group.
// Shape validation does not prove current neighbors, membership or generation.
// It is separate from the existing backlog-only TaskPosition schema.
type TaskTransitionPosition struct {
	SprintID        SprintID     `json:"sprint_id"`
	State           TaskState    `json:"state"`
	Priority        TaskPriority `json:"priority"`
	PreviousID      *TaskID      `json:"previous_id"`
	NextID          *TaskID      `json:"next_id"`
	OrderGeneration int64        `json:"-"`
}

func (v TaskTransitionPosition) Validate() error {
	if v.SprintID.Validate() != nil || v.State.Validate() != nil || v.Priority.Validate() != nil ||
		v.OrderGeneration < 1 || v.PreviousID != nil && v.PreviousID.Validate() != nil ||
		v.NextID != nil && v.NextID.Validate() != nil ||
		v.PreviousID != nil && v.NextID != nil && *v.PreviousID == *v.NextID {
		return invalid("/position", "INVALID_POSITION")
	}
	return nil
}

func (v TaskTransitionPosition) ValidateTarget(id TaskID) error {
	if err := v.Validate(); err != nil {
		return err
	}
	if id.Validate() != nil || v.PreviousID != nil && *v.PreviousID == id || v.NextID != nil && *v.NextID == id {
		return invalid("/position", "SELF_NEIGHBOR")
	}
	return nil
}

func (v TaskTransitionPosition) Clone() TaskTransitionPosition {
	v.PreviousID = taskClonePtr(v.PreviousID)
	v.NextID = taskClonePtr(v.NextID)
	return v
}

type taskTransitionPositionWire struct {
	SprintID        SprintID     `json:"sprint_id"`
	State           TaskState    `json:"state"`
	Priority        TaskPriority `json:"priority"`
	PreviousID      *TaskID      `json:"previous_id"`
	NextID          *TaskID      `json:"next_id"`
	OrderGeneration f.Version    `json:"order_generation"`
}

func (v TaskTransitionPosition) MarshalJSON() ([]byte, error) {
	return checkedLimit(taskTransitionPositionWire{
		SprintID: v.SprintID, State: v.State, Priority: v.Priority,
		PreviousID: v.PreviousID, NextID: v.NextID, OrderGeneration: f.Version(v.OrderGeneration),
	}, v.Validate(), MaxTaskHistoryPayloadBytes)
}

func (v *TaskTransitionPosition) UnmarshalJSON(raw []byte) error {
	if v == nil {
		return invalid("/position", "INVALID_POSITION")
	}
	w, err := decodeFieldsLimit[taskTransitionPositionWire](raw,
		[]string{"sprint_id", "state", "priority", "previous_id", "next_id", "order_generation"},
		nil, []string{"previous_id", "next_id"}, MaxTaskHistoryPayloadBytes)
	if err != nil {
		return err
	}
	next := TaskTransitionPosition{
		SprintID: w.SprintID, State: w.State, Priority: w.Priority,
		PreviousID: w.PreviousID, NextID: w.NextID, OrderGeneration: int64(w.OrderGeneration),
	}
	if err := next.Validate(); err != nil {
		return err
	}
	*v = next
	return nil
}

// DecodeTaskTransitionPosition bounds the complete raw argument, including
// outer whitespace that encoding/json would strip before UnmarshalJSON.
func DecodeTaskTransitionPosition(raw []byte) (TaskTransitionPosition, error) {
	var v TaskTransitionPosition
	err := v.UnmarshalJSON(raw)
	return v, err
}

func taskTransitionSafeFormat(w fmt.State)                 { _, _ = io.WriteString(w, "work_task_transition") }
func (TaskTransitionRuleInput) Format(w fmt.State, _ rune) { taskTransitionSafeFormat(w) }
func (TaskTransitionRuleInput) LogValue() slog.Value       { return slog.StringValue("work_task_transition") }
func (TaskTransitionPosition) Format(w fmt.State, _ rune)  { taskTransitionSafeFormat(w) }
func (TaskTransitionPosition) LogValue() slog.Value        { return slog.StringValue("work_task_transition") }
