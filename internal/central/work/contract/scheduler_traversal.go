package contract

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"slices"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

// SchedulerTaskIdentity records only membership and order for one traversal.
// It is not a current Task version or permission to claim/launch.
type SchedulerTaskIdentity struct {
	TaskID TaskID
	State  TaskState
}

// Entries are complete, unique and ordered by todo, in_progress, in_review,
// blocked; within each group by priority, manual rank and Task ID. A nil
// CurrentSprintID has an empty, nonnil Entries slice. Historical pending
// Dispatch identities are enumerated separately by their Scheduler owner.
type SchedulerTaskSnapshot struct {
	ProjectID       ProjectID
	CurrentSprintID *SprintID
	Entries         []SchedulerTaskIdentity
}

func (v SchedulerTaskSnapshot) Clone() SchedulerTaskSnapshot {
	v.CurrentSprintID = taskClonePtr(v.CurrentSprintID)
	v.Entries = slices.Clone(v.Entries)
	return v
}

// Current facts intentionally omit Task content and do not validate Agent
// capability, Execution occupancy or pending claims. Existing Claim/Launch
// owners must perform those current checks again before any mutation.
type SchedulerTaskFacts struct {
	ProjectID             ProjectID
	TaskID                TaskID
	MilestoneID           MilestoneID
	SprintID              SprintID
	State                 TaskState
	Priority              TaskPriority
	Version               f.Version
	AssigneeAgentID       *i.AgentID
	HasUnresolvedBlockers bool
}

func (v SchedulerTaskFacts) Clone() SchedulerTaskFacts {
	v.AssigneeAgentID = taskClonePtr(v.AssigneeAgentID)
	return v
}

// This trusted read port accepts only the original same-Store caller Tx with
// Project SH and Schedule EX already held. CurrentTaskInTx additionally needs
// Task SH. The provider rechecks the real Project lifecycle/configuration; it
// neither fabricates a Human/Service actor nor grants scheduling authority.
// It performs no Begin/Acquire/write. Missing Tasks return TASK_NOT_FOUND.
// Paused and nil-current-Sprint Projects remain readable for reconciliation.
type SchedulerTaskReader interface {
	SnapshotInTx(context.Context, f.Tx, ProjectID) (SchedulerTaskSnapshot, error)
	CurrentTaskInTx(context.Context, f.Tx, ProjectID, TaskID) (SchedulerTaskFacts, error)
}

func (SchedulerTaskSnapshot) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "scheduler_task_snapshot")
}
func (SchedulerTaskSnapshot) LogValue() slog.Value {
	return slog.StringValue("scheduler_task_snapshot")
}
func (SchedulerTaskFacts) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "scheduler_task_facts")
}
func (SchedulerTaskFacts) LogValue() slog.Value { return slog.StringValue("scheduler_task_facts") }
