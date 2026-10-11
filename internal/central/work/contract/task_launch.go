package contract

import (
	"context"

	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

// TaskLaunchIntent is the Scheduler's callback-local projection of the original
// committed dispatch origin. It is data, never a bearer permit or a replacement
// for Work's immutable origin and current Task facts. The zero Origin is kept
// only for the legacy todo-claim projection; it cannot identify a relaunch.
type TaskLaunchIntent struct {
	ProjectID      ProjectID
	TaskID         TaskID
	AgentID        i.AgentID
	SprintID       SprintID
	DispatchID     string
	ClaimedVersion f.Version
	Origin         TaskDispatchOriginKind `json:",omitempty"`
	Relaunch       *TaskRelaunchSource    `json:",omitempty"`
}

func (v TaskLaunchIntent) Validate() error {
	if v.ProjectID.Validate() != nil || v.TaskID.Validate() != nil || v.AgentID.Validate() != nil || v.SprintID.Validate() != nil {
		return invalid("", "INVALID_TASK_LAUNCH_INTENT")
	}
	if _, err := f.ParseID[SchedulerClaim](v.DispatchID); err != nil {
		return invalid("", "INVALID_TASK_LAUNCH_INTENT")
	}
	switch v.Origin {
	case "", TaskDispatchTodoClaim:
		if v.Relaunch != nil || v.ClaimedVersion.Validate() != nil || v.ClaimedVersion < 2 {
			return invalid("", "INVALID_TASK_LAUNCH_INTENT")
		}
	case TaskDispatchRelaunch:
		if v.ClaimedVersion != 0 || v.Relaunch == nil || v.Relaunch.Validate() != nil {
			return invalid("", "INVALID_TASK_LAUNCH_INTENT")
		}
		r := v.Relaunch.Request
		if r.ProjectID != v.ProjectID || r.TaskID != v.TaskID || r.AgentID != v.AgentID || r.CurrentSprintID != v.SprintID || r.DispatchID != v.DispatchID {
			return invalid("", "INVALID_TASK_LAUNCH_INTENT")
		}
	default:
		return invalid("", "INVALID_TASK_LAUNCH_INTENT")
	}
	return nil
}

func (v TaskLaunchIntent) Clone() TaskLaunchIntent {
	v.Relaunch = taskClonePtr(v.Relaunch)
	return v
}

// TaskLaunchAuthority is supplied by the real Scheduler owner. Before every
// protected Work discovery/final read it must prove the live private launch
// handoff, its known-committed send attempt, the original same-Store Tx and full
// held union, and the canonical pending Dispatch's complete request (including
// Meta RequestID/key, policy and lineage). A row or a Service name is not proof.
// This port must not call Work, Execution or the Project gate recursively.
// Historical Lookup uses Project's separate Read intent, not this launch gate.
type TaskLaunchAuthority interface {
	RequireTaskLaunchInTx(context.Context, f.Tx, i.Actor, ec.LaunchRequest) (TaskLaunchIntent, error)
}
