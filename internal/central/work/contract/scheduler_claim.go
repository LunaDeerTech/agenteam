package contract

import (
	"context"
	"fmt"
	"io"
	"log/slog"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

// TaskClaimRequest identifies the original Scheduler operation. It is data,
// never a grant: both discovery and application need the Scheduler owner's
// private call proof in the same live transaction.
type TaskClaimRequest struct {
	ProjectID           ProjectID
	TaskID              TaskID
	AgentID             i.AgentID
	DispatchID          string
	ExpectedTaskVersion f.Version
	CurrentSprintID     SprintID
	Purpose             string
	RequestID           f.ID[f.Request]
}

func (v TaskClaimRequest) Validate() error {
	if v.ProjectID.Validate() != nil || v.TaskID.Validate() != nil || v.AgentID.Validate() != nil || v.ExpectedTaskVersion.Validate() != nil || v.CurrentSprintID.Validate() != nil || v.RequestID.Validate() != nil || v.Purpose != "task/work" {
		return invalid("", "INVALID_TASK_CLAIM")
	}
	if _, err := f.ParseID[f.Request](v.DispatchID); err != nil {
		return invalid("", "INVALID_TASK_CLAIM")
	}
	return nil
}
func (v TaskClaimRequest) Clone() TaskClaimRequest  { return v }
func (TaskClaimRequest) Format(w fmt.State, _ rune) { _, _ = io.WriteString(w, "task_claim") }
func (TaskClaimRequest) LogValue() slog.Value       { return slog.StringValue("task_claim") }

// TaskClaimGuard is the durable logical position to restore, not a physical
// rank or an authorization proof. ClaimedVersion is the post-write version.
type TaskClaimGuard struct {
	TaskID                TaskID
	ClaimedVersion        f.Version
	SourceState           TaskState
	SourceAssigneeID      i.AgentID
	SourcePriority        TaskPriority
	SourceSprintID        SprintID
	SourceOrderGeneration int64
	PredecessorID         *TaskID
	SuccessorID           *TaskID
}

func (v TaskClaimGuard) Clone() TaskClaimGuard {
	v.PredecessorID = taskClonePtr(v.PredecessorID)
	v.SuccessorID = taskClonePtr(v.SuccessorID)
	return v
}

// Implementations return private concrete plans and reject any other issuer,
// instance, original request or Tx. Public interface satisfaction is not proof.
type TaskClaimPlan interface{ RequiredLocks() []f.LockRequest }
type AppliedTaskClaim interface{ Guard() TaskClaimGuard }

type SchedulerTaskClaims interface {
	DiscoverTaskClaim(context.Context, i.Actor, TaskClaimRequest) (TaskClaimPlan, error)
	ApplyTaskClaimInTx(context.Context, f.Tx, i.Actor, TaskClaimRequest, TaskClaimPlan) (AppliedTaskClaim, error)
	CheckTaskClaimAppliedInTx(context.Context, f.Tx, i.Actor, TaskClaimRequest, TaskClaimPlan, AppliedTaskClaim) error
}

// SchedulerClaimAuthority is implemented by the real durable Scheduler owner.
// The first proof authorizes only protected discovery under the original
// Project/Schedule/Agent/Task lock set. The final proof also binds the original
// full plan, current configuration, capacity and free Agent slot. Neither
// requires a pending row or a Work applied proof which does not exist yet.
type SchedulerClaimAuthority interface {
	RequireTaskClaimDiscoveryInTx(context.Context, f.Tx, i.Actor, TaskClaimRequest) error
	RequireTaskClaimInTx(context.Context, f.Tx, i.Actor, TaskClaimRequest, TaskClaimPlan) error
}
