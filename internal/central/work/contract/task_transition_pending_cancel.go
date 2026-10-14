package contract

import (
	"context"

	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

// Scope is an immutable request shape, not permission. Work separately proves
// its original command and cancelled postimage in the caller's transaction.
type TaskPendingCancellationScope struct {
	ProjectID   ProjectID
	TaskID      string
	CommandID   TaskTransitionCommandID
	DispatchIDs []string
	OccurredAt  f.Instant
}

func (s TaskPendingCancellationScope) Validate() error {
	if s.ProjectID.Validate() != nil || s.CommandID.Validate() != nil || s.OccurredAt.Validate() != nil || s.DispatchIDs == nil || len(s.DispatchIDs) > 1 {
		return f.NewFault(f.InvalidArgument, f.NotStarted)
	}
	if _, err := f.ParseID[Task](s.TaskID); err != nil {
		return f.NewFault(f.InvalidArgument, f.NotStarted)
	}
	for _, id := range s.DispatchIDs {
		if _, err := f.ParseID[struct{}](id); err != nil {
			return f.NewFault(f.InvalidArgument, f.NotStarted)
		}
	}
	return nil
}

type TaskPendingCancellationAuthority interface {
	RequireStore(any) error
	RequirePendingTaskCancellationInTx(context.Context, f.Tx, i.Actor, TaskPendingCancellationScope) error
}
type TaskPendingCanceller interface {
	RequireStore(any) error
	CancellationLocks(ProjectID, []string) ([]f.LockRequest, error)
	RequireNoOtherPendingGroupsInTx(context.Context, f.Tx, ProjectID, string, []ec.PendingClaimGroup) error
	CancelTaskDispatchesInTx(context.Context, f.Tx, i.Actor, TaskPendingCancellationScope) error
}
