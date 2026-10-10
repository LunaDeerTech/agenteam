package contract

import (
	"context"

	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

// TaskLaunchIntent is the Scheduler's callback-local projection of the original
// committed todo claim. It is data, never a bearer permit or a replacement for
// the Work-owned claim record and current Task facts.
type TaskLaunchIntent struct {
	ProjectID      ProjectID
	TaskID         TaskID
	AgentID        i.AgentID
	SprintID       SprintID
	DispatchID     string
	ClaimedVersion f.Version
}

func (v TaskLaunchIntent) Validate() error {
	if v.ProjectID.Validate() != nil || v.TaskID.Validate() != nil || v.AgentID.Validate() != nil || v.SprintID.Validate() != nil || v.ClaimedVersion.Validate() != nil || v.ClaimedVersion < 2 {
		return invalid("", "INVALID_TASK_LAUNCH_INTENT")
	}
	if _, err := f.ParseID[SchedulerClaim](v.DispatchID); err != nil {
		return invalid("", "INVALID_TASK_LAUNCH_INTENT")
	}
	return nil
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
