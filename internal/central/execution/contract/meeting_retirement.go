package contract

import (
	"context"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	ob "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
)

// This borrowed-transaction requirement has no public retired flag. Only the
// provider bound to the original Execution owners can establish retirement.
type MeetingExecutionRetirementReader interface {
	RequireMeetingExecutionRetiredInTx(context.Context, f.Tx, i.ProjectID, AssociatedMeeting) error
}

// Meeting supplies its live deletion transaction and exact historical launch
// intent. The public association alone never authorizes a retirement read.
type MeetingExecutionRetirementAuthority interface {
	RequireBindingInTx(context.Context, f.Tx, any) error
	RequireMeetingDeletionInTx(context.Context, f.Tx, i.ProjectID, AssociatedMeeting) error
}

func MeetingExecutionRetirementLocks(project i.ProjectID, m AssociatedMeeting) ([]f.LockRequest, error) {
	locks, err := MeetingExecutionReadLocks(project, m)
	if err != nil {
		return nil, err
	}
	// Every original ProjectCall registration takes this project lock. The
	// deletion observation must exclude a new registered owner during its Tx.
	locks[0].Mode = f.Exclusive
	command, err := (LaunchLookupKey{ProjectID: project, AgentID: m.AgentID, IdempotencyKey: m.Key}).Command()
	if err != nil {
		return nil, err
	}
	key, err := f.CommandLock(command)
	if err != nil {
		return nil, err
	}
	return ob.NormalizeLocks(append(locks, f.LockRequest{Key: key, Mode: f.Exclusive}))
}
