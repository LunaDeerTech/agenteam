package contract

import (
	"context"
	"fmt"
	"io"
	"log/slog"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

// PendingTaskDispatch is a Scheduler-owned fact projection. Task, Sprint and
// Dispatch use canonical UUID strings here; this lower-level contract neither
// imports Work/Scheduler nor creates another owner of their identities.
type PendingTaskDispatch struct {
	TaskID     string
	DispatchID string
	AgentID    i.AgentID
	SprintID   string
}

// DispatchOccupancy reports all pending rows and existence of history in any
// of the four Dispatch states. Successful slices are nonnil and sorted by Task
// identity (Pending then by Dispatch identity). Unknown Launch is still pending.
type DispatchOccupancy struct {
	Pending        []PendingTaskDispatch
	HistoryTaskIDs []string
}

// PendingDispatchReader is the D01 caller-transaction fact port. The caller
// first authorizes its operation and reads the actual Task set from Work while
// holding its complete lock union. Implementations require their original
// Store/Tx, Project SH and Schedule EX, including for an empty input. taskIDs
// is a nonnil sorted unique canonical UUID list, bounded by MaxWorkOccupancyTasks.
// Neither a missing dependency nor a failed read can mean empty occupancy.
type PendingDispatchReader interface {
	ReadInTx(context.Context, f.Tx, i.ProjectID, []string) (DispatchOccupancy, error)
}

// PendingClaimGroup denotes a Work order group. State and Priority are closed
// Work scalar values, not authorization or a rank. A pending todo claim has
// already left this group; its persisted source group remains protected.
type PendingClaimGroup struct {
	SprintID string
	State    string
	Priority string
}

// PendingClaimGroupGuard protects the persisted logical source position.
// The caller supplies all source/destination/neighbor groups that its mutation
// can affect, derived under the same Schedule EX lock. This first implementation
// rejects such a mutation while a pending claim owns a position in that group;
// it never restores an old numerical rank or silently drops a claim mapping.
// Groups must be nonnil, sorted and unique by SprintID, State, Priority. The
// provider checks the same original transaction gates even for an empty list.
type PendingClaimGroupGuard interface {
	RequireNoPendingGroupsInTx(context.Context, f.Tx, i.ProjectID, []PendingClaimGroup) error
}

func (DispatchOccupancy) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "scheduler_dispatch_occupancy")
}
func (DispatchOccupancy) LogValue() slog.Value {
	return slog.StringValue("scheduler_dispatch_occupancy")
}
func (DispatchOccupancy) MarshalJSON() ([]byte, error) {
	return []byte(`"scheduler_dispatch_occupancy"`), nil
}
