package scheduler

import (
	"fmt"
	"io"
	"log/slog"
	"sync"

	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	wc "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
)

// RelaunchVisit records one original visit. A skipped cooldown is a durable
// mutation; Remaining=0 after that mutation does not authorize this visit to
// send. A nonempty Dispatch is consumed by the original PendingVisitor.
// Neither observation is a Task state change or an Execution success receipt.
type RelaunchVisit struct {
	Dispatch        Dispatch
	CooldownSkipped bool
	Remaining       int64
}

func (RelaunchVisit) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "scheduler_relaunch_visit")
}
func (RelaunchVisit) LogValue() slog.Value         { return slog.StringValue("scheduler_relaunch_visit") }
func (RelaunchVisit) MarshalJSON() ([]byte, error) { return []byte(`"scheduler_relaunch_visit"`), nil }

// RelaunchCoordinator owns work-phase visits and their original uncertain
// transactions. It borrows the existing Coordinator's immutable Project,
// Execution and retry-policy dependencies. It never turns a relaunch into a
// todo claim and never owns the shared Execution executor's lifetime.
type RelaunchCoordinator struct {
	coordinator *Coordinator
	tasks       wc.SchedulerTaskRelaunches
	current     wc.SchedulerTaskReader
	occupancy   ec.WorkOccupancyReader
	skipCount   int64
	mu          sync.Mutex
	stopped     bool
	calls       map[*relaunchCall]struct{}
	unknown     map[DispatchID]*relaunchCall
	drained     chan struct{}
}

// NewRelaunchCoordinator requires an explicit skip count, including an explicit
// zero. It performs no I/O and does not alter any existing constructor/config.
// The Work provider is built with the same PendingAuthority before this owner.
func NewRelaunchCoordinator(coordinator *Coordinator, tasks wc.SchedulerTaskRelaunches, current wc.SchedulerTaskReader, occupancy ec.WorkOccupancyReader, skipCount int64) (*RelaunchCoordinator, error) {
	if skipCount < 0 {
		return nil, invalid()
	}
	if coordinator == nil || coordinator.authority == nil || nilPort(tasks) || nilPort(current) || nilPort(occupancy) {
		return nil, fault(f.DependencyUnbound)
	}
	return &RelaunchCoordinator{coordinator: coordinator, tasks: tasks, current: current, occupancy: occupancy, skipCount: skipCount, calls: make(map[*relaunchCall]struct{}), unknown: make(map[DispatchID]*relaunchCall), drained: make(chan struct{})}, nil
}
