package contract

import (
	"context"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

// TaskReadScope borrows the original Work owner and caller's live transaction.
// It returns no permission token and may not open another Store transaction.
type TaskReadScope interface {
	RequireStore(any) error
	TaskReadLocks(i.Actor, i.ProjectID, string) ([]f.LockRequest, error)
	RequireTaskReadInTx(context.Context, f.Tx, i.Actor, i.ProjectID, string) error
}

// TaskExecution contains only canonical identity/state and a closed safe reason.
// Runtime content, prompts, credentials and private error text are not projected.
type TaskExecution struct {
	ID                i.ExecutionID `json:"id"`
	ProjectID         i.ProjectID   `json:"project_id"`
	TaskID            string        `json:"task_id"`
	AgentID           i.AgentID     `json:"agent_id"`
	Purpose           string        `json:"purpose"`
	Status            Status        `json:"status"`
	Version           f.Version     `json:"version"`
	CreatedAt         f.Instant     `json:"created_at"`
	StartedAt         *f.Instant    `json:"started_at"`
	CompletedAt       *f.Instant    `json:"completed_at"`
	CancelRequestedAt *f.Instant    `json:"cancel_requested_at"`
	SnapshotID        *SnapshotID   `json:"snapshot_id"`
	Reason            *string       `json:"reason"`
}

type TaskExecutionPage struct {
	Items      []TaskExecution `json:"items"`
	NextCursor *string         `json:"next_cursor"`
}
