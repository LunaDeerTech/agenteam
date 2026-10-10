package contract

import (
	"context"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

// AgentSlotObservation is an observation, not a launch or busy-compensation
// permit. A cancellation request does not release an occupied slot. Waiting
// still occupies the Agent even though it does not consume Scheduler capacity.
// A successful free observation has Occupied=false and all other fields zero.
type AgentSlotObservation struct {
	Occupied          bool
	ExecutionID       *i.ExecutionID
	Status            Status
	CancelRequestedAt *f.Instant
}

// DispatchObserver supplies canonical Execution facts to a trusted domain
// caller that has already checked its own current authority in the original
// transaction. Neither method grants an Actor permission or calls back into
// Scheduler, Task or Project. Both require the same Store's live caller Tx and
// preheld Project SH, ProjectSchedule EX and Agent SH; Lookup additionally
// requires the original Launch Command EX. They never Begin, Acquire or write.
//
// Lookup binds the full request digest and exact stored lineage.DispatchID to
// the original key. Not observed is not evidence that an unknown writer cannot
// still commit, and never authorizes another Launch. A caller associating a
// Dispatch must check the returned Execution ID against any persisted linkage.
// Historical results do not require the Task to remain currently launchable.
type DispatchObserver interface {
	LookupLaunchInTx(context.Context, f.Tx, LaunchLookupKey, f.Digest, string) (LaunchLookup, error)
	AgentSlotInTx(context.Context, f.Tx, i.ProjectID, i.AgentID) (AgentSlotObservation, error)
}
