package contract

import (
	"context"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

// PreparedTaskTransition is an opaque instance-bound original command plan.
// RequiredLocks returns a copy; callers acquire the complete union once before
// TransferTaskInTx. Implementing this interface does not create a valid plan.
type PreparedTaskTransition interface{ RequiredLocks() []f.LockRequest }
type TaskTransitionPreparation struct {
	Prepared         PreparedTaskTransition
	CompletedReceipt *TaskTransitionMutation
}

// Exactly one preparation arm is set. The InTx method never begins, commits,
// acquires more locks or substitutes a new request; its result is tentative
// until the caller's actual transaction commits.
type TaskTransitionComposer interface {
	PrepareTaskTransition(context.Context, i.Actor, f.CommandMeta, ProjectID, TaskID, TaskTransfer) (TaskTransitionPreparation, error)
	TransferTaskInTx(context.Context, f.Tx, i.Actor, PreparedTaskTransition) (TaskTransitionMutation, error)
}
