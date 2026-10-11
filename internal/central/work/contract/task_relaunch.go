package contract

import (
	"context"
	"fmt"
	"io"
	"log/slog"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

type TaskDispatchOriginKind string

const (
	TaskDispatchTodoClaim TaskDispatchOriginKind = "todo_claim"
	TaskDispatchRelaunch  TaskDispatchOriginKind = "relaunch"
)

// TaskRelaunchRequest describes a new dispatch for an existing work or review
// phase. Purpose binds in_progress/task/work or in_review/task/review; AgentID
// is the current assignee (the reviewer in the review phase).
// It is not a todo claim and never authorizes a Task state or rank mutation.
// The Scheduler's original private discovery/applying call is required before
// protected reads; public fields, a Service actor or historical rows are not a grant.
type TaskRelaunchRequest struct {
	ProjectID           ProjectID
	TaskID              TaskID
	AgentID             i.AgentID
	CurrentSprintID     SprintID
	ExpectedTaskVersion f.Version
	DispatchID          string
	RequestID           f.ID[f.Request]
	Purpose             string
}

func (v TaskRelaunchRequest) Validate() error {
	if v.ProjectID.Validate() != nil || v.TaskID.Validate() != nil || v.AgentID.Validate() != nil || v.CurrentSprintID.Validate() != nil || v.ExpectedTaskVersion.Validate() != nil || v.RequestID.Validate() != nil || (v.Purpose != "task/work" && v.Purpose != "task/review") {
		return invalid("", "INVALID_TASK_RELAUNCH")
	}
	if _, err := f.ParseID[f.Request](v.DispatchID); err != nil {
		return invalid("", "INVALID_TASK_RELAUNCH")
	}
	return nil
}

func (v TaskRelaunchRequest) Clone() TaskRelaunchRequest { return v }
func (TaskRelaunchRequest) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "task_relaunch")
}
func (TaskRelaunchRequest) LogValue() slog.Value { return slog.StringValue("task_relaunch") }

// TaskRelaunchSource identifies Work's immutable origin, recorded in the same
// transaction as the new pending Dispatch. ReferenceDigest binds its exact
// Task/Sprint/Milestone preimage; it is neither a bearer permit nor a claim guard.
type TaskRelaunchSource struct {
	Request         TaskRelaunchRequest
	MilestoneID     MilestoneID
	ReferenceDigest f.Digest
}

func (v TaskRelaunchSource) Validate() error {
	if v.Request.Validate() != nil || v.MilestoneID.Validate() != nil || v.ReferenceDigest.Validate() != nil {
		return invalid("", "INVALID_TASK_RELAUNCH_SOURCE")
	}
	return nil
}
func (v TaskRelaunchSource) Clone() TaskRelaunchSource { return v }
func (TaskRelaunchSource) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "task_relaunch_source")
}
func (TaskRelaunchSource) LogValue() slog.Value { return slog.StringValue("task_relaunch_source") }

type TaskRelaunchPlan interface{ RequiredLocks() []f.LockRequest }
type AppliedTaskRelaunch interface{ Source() TaskRelaunchSource }

// Record writes only the immutable Work origin. It does not change Task,
// version, rank, history or Outbox. The private applied value is checked in the
// same live caller Tx before Scheduler inserts the matching pending Dispatch.
type SchedulerTaskRelaunches interface {
	DiscoverTaskRelaunch(context.Context, i.Actor, TaskRelaunchRequest) (TaskRelaunchPlan, error)
	RecordTaskRelaunchInTx(context.Context, f.Tx, i.Actor, TaskRelaunchRequest, TaskRelaunchPlan) (AppliedTaskRelaunch, error)
	CheckTaskRelaunchAppliedInTx(context.Context, f.Tx, i.Actor, TaskRelaunchRequest, TaskRelaunchPlan, AppliedTaskRelaunch) error
}

// These callbacks prove the original same-Store live Scheduler call and held
// lock set. Final authorization also binds the opaque plan and the actual
// enabled/current-Sprint, no-pending/no-active, free Agent slot, project capacity
// and persisted cooldown admission. No pending row or Work origin is required
// before Record. They must not recursively call Work or Agent.
type SchedulerRelaunchAuthority interface {
	RequireTaskRelaunchDiscoveryInTx(context.Context, f.Tx, i.Actor, TaskRelaunchRequest) error
	RequireTaskRelaunchInTx(context.Context, f.Tx, i.Actor, TaskRelaunchRequest, TaskRelaunchPlan) error
}
