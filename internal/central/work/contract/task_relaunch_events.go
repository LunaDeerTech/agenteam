package contract

import (
	"fmt"
	"log/slog"
	"slices"

	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

const TaskRelaunchFailureSchemaVersion uint32 = 5

// Schema 4 remains the historical todo-claim event. A relaunch has its own
// explicit source and dispatch_id; it never serializes a fictitious claim_id.
type TaskRelaunchFailed struct {
	DispatchID     f.ID[f.Request]         `json:"dispatch_id"`
	Origin         TaskDispatchOriginKind  `json:"origin"`
	Source         TaskRelaunchSource      `json:"source"`
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

func (v TaskRelaunchFailed) Validate() error {
	if v.DispatchID.Validate() != nil || v.Origin != TaskDispatchRelaunch || v.Source.Validate() != nil || v.Source.Request.DispatchID != v.DispatchID.String() || v.Actor.Validate() != nil || v.Actor.CauseID != v.DispatchID.String() || v.BlockerID.Validate() != nil || v.MilestoneID != v.Source.MilestoneID || v.SprintID != v.Source.Request.CurrentSprintID || v.AgentID != v.Source.Request.AgentID || v.Reason.Validate() != nil || v.ToState != TaskStateBlocked {
		return invalid("", "INVALID_TASK_RELAUNCH_FAILED")
	}
	if v.FromState == TaskStateBlocked {
		if len(v.TaskEventIDs) != 1 || v.SourcePosition != nil || v.TargetPosition != nil {
			return invalid("", "INVALID_TASK_RELAUNCH_FAILED")
		}
	} else if v.FromState == TaskStateInProgress {
		if len(v.TaskEventIDs) != 2 || v.SourcePosition == nil || v.TargetPosition == nil || v.SourcePosition.Validate() != nil || v.TargetPosition.Validate() != nil || v.SourcePosition.State != v.FromState || v.TargetPosition.State != v.ToState || v.SourcePosition.SprintID != v.SprintID || v.TargetPosition.SprintID != v.SprintID || v.SourcePosition.Priority != v.TargetPosition.Priority {
			return invalid("", "INVALID_TASK_RELAUNCH_FAILED")
		}
	} else {
		return invalid("", "INVALID_TASK_RELAUNCH_FAILED")
	}
	for n, id := range v.TaskEventIDs {
		if id.Validate() != nil || n > 0 && v.TaskEventIDs[n-1].String() >= id.String() {
			return invalid("", "INVALID_TASK_RELAUNCH_FAILED")
		}
	}
	return nil
}
func (v TaskRelaunchFailed) Clone() TaskRelaunchFailed {
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
func (v TaskRelaunchFailed) MarshalJSON() ([]byte, error) {
	type wire TaskRelaunchFailed
	return checkedLimit(wire(v), v.Validate(), 16<<10)
}
func (v *TaskRelaunchFailed) UnmarshalJSON(raw []byte) error {
	if v == nil {
		return invalid("", "INVALID_TASK_RELAUNCH_FAILED")
	}
	type wire TaskRelaunchFailed
	w, err := decodeFieldsLimit[wire](raw, []string{"dispatch_id", "origin", "source", "actor", "blocker_id", "task_event_ids", "milestone_id", "sprint_id", "agent_id", "from_state", "to_state", "reason", "source_position", "target_position"}, nil, []string{"source_position", "target_position"}, 16<<10)
	if err != nil {
		return err
	}
	n := TaskRelaunchFailed(w)
	if err = n.Validate(); err != nil {
		return err
	}
	*v = n
	return nil
}
func (TaskRelaunchFailed) Format(w fmt.State, r rune) { blockerSafeFormat(w, r) }
func (TaskRelaunchFailed) LogValue() slog.Value       { return blockerSafeLog() }

func (v TaskLaunchFailureEvents) NewTaskRelaunchFailed(h event.Header, p TaskRelaunchFailed) (event.Event, error) {
	project, err := f.ParseID[event.Project](p.Source.Request.ProjectID.String())
	if err != nil || !v.Valid() || h.Validate() != nil || h.EventType != TaskTransitionedName || h.AggregateType != TaskAggregate || h.SchemaVersion != TaskRelaunchFailureSchemaVersion || h.Scope.Kind != event.ProjectScope || h.Scope.ProjectID != project || h.AggregateID.String() != p.Source.Request.TaskID.String() || h.AggregateVersion == nil || *h.AggregateVersion < 2 || h.AggregateSequence != nil || p.Validate() != nil {
		return event.Event{}, invalid("", "INVALID_TASK_RELAUNCH_FAILED")
	}
	if p.SourcePosition != nil && (p.SourcePosition.ValidateTarget(p.Source.Request.TaskID) != nil || p.TargetPosition.ValidateTarget(p.Source.Request.TaskID) != nil) {
		return event.Event{}, invalid("", "INVALID_TASK_RELAUNCH_FAILED")
	}
	return event.NewEvent(v.relaunched, h, p.Clone())
}
