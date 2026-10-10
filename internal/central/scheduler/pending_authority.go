package scheduler

import (
	"context"
	"slices"
	"strings"
	"sync"

	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
)

// PendingAuthority is constructed before Work and the coordinator. It owns
// current facts and a private proof issuer, not a mutable provider registry.
// Construction does not claim a running Scheduler or a bound Launch provider.
type PendingAuthority struct {
	store Store
	// In-process Project ownership only; this is not a database leader lease.
	runnerMu sync.Mutex
	runners  map[i.ProjectID]*projectRunCall
}

func NewPendingAuthority(store Store) (*PendingAuthority, error) {
	if nilPort(store) {
		return nil, fault(f.DependencyUnbound)
	}
	return &PendingAuthority{store: store}, nil
}

func pendingLocks(p i.ProjectID) []f.LockRequest {
	project, _ := f.ProjectLock(p.String())
	schedule, _ := f.ProjectScheduleLock(p.String())
	return []f.LockRequest{{Key: project, Mode: f.Shared}, {Key: schedule, Mode: f.Exclusive}}
}
func (a *PendingAuthority) inTx(ctx context.Context, tx f.Tx, p i.ProjectID) (postgres.SQLExecutor, error) {
	if ctx == nil || p.Validate() != nil {
		return nil, invalid()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if a == nil || nilPort(a.store) {
		return nil, fault(f.DependencyUnbound)
	}
	x, err := a.store.InTx(tx)
	if err != nil {
		return nil, portError(err)
	}
	if nilPort(x) {
		return nil, fault(f.DependencyUnbound)
	}
	if err = a.store.RequireHeldLocks(ctx, tx, pendingLocks(p)); err != nil {
		return nil, portError(err)
	}
	return x, ctx.Err()
}

const pendingReadSQL = `WITH selected AS (
 SELECT id FROM agenteam_scheduler.dispatches WHERE project_id=$1 AND task_id=ANY($2::text[]) AND status='pending'
 UNION ALL
 SELECT min(id::text)::agenteam_scheduler.safe_id FROM agenteam_scheduler.dispatches
 WHERE project_id=$1 AND task_id=ANY($2::text[]) AND status<>'pending' GROUP BY task_id
)
SELECT ` + dispatchColumns + ` FROM agenteam_scheduler.dispatches WHERE project_id=$1 AND id IN(SELECT id FROM selected)
ORDER BY task_id COLLATE "C",id COLLATE "C"`

func (a *PendingAuthority) ReadInTx(ctx context.Context, tx f.Tx, p i.ProjectID, taskIDs []string) (ec.DispatchOccupancy, error) {
	if ctx == nil || taskIDs == nil {
		return ec.DispatchOccupancy{}, invalid()
	}
	if len(taskIDs) > ec.MaxWorkOccupancyTasks {
		return ec.DispatchOccupancy{}, fault(f.PayloadTooLarge)
	}
	ids := slices.Clone(taskIDs)
	for n, id := range ids {
		if !validID(id) || n > 0 && ids[n-1] >= id {
			return ec.DispatchOccupancy{}, invalid()
		}
		if err := ctx.Err(); err != nil {
			return ec.DispatchOccupancy{}, err
		}
	}
	x, err := a.inTx(ctx, tx, p)
	if err != nil {
		return ec.DispatchOccupancy{}, err
	}
	if len(ids) == 0 {
		return ec.DispatchOccupancy{Pending: []ec.PendingTaskDispatch{}, HistoryTaskIDs: []string{}}, ctx.Err()
	}
	rows, err := x.Query(ctx, pendingReadSQL, p.String(), ids)
	if err != nil {
		return ec.DispatchOccupancy{}, portError(err)
	}
	if rows == nil {
		return ec.DispatchOccupancy{}, unavailable(nil)
	}
	return collectPending(ctx, rows, p, ids)
}

type dispatchRows interface {
	postgres.Row
	Next() bool
	Err() error
	Close()
}

func collectPending(ctx context.Context, rows dispatchRows, p i.ProjectID, ids []string) (out ec.DispatchOccupancy, err error) {
	defer func() {
		rows.Close()
		if err == nil {
			err = portError(rows.Err())
		}
		if ctx.Err() != nil {
			err = ctx.Err()
		}
		if err != nil {
			out = ec.DispatchOccupancy{}
		}
	}()
	out = ec.DispatchOccupancy{Pending: []ec.PendingTaskDispatch{}, HistoryTaskIDs: []string{}}
	history := make(map[string]bool, len(ids))
	seen := make(map[DispatchID]bool)
	pending := make(map[string]bool)
	for rows.Next() {
		if err = ctx.Err(); err != nil {
			return ec.DispatchOccupancy{}, err
		}
		r, e := scanDispatch(rows)
		if e != nil {
			return ec.DispatchOccupancy{}, e
		}
		if r == nil || r.project != p || seen[r.id] {
			return ec.DispatchOccupancy{}, unavailable(nil)
		}
		if _, ok := slices.BinarySearch(ids, r.task); !ok {
			return ec.DispatchOccupancy{}, unavailable(nil)
		}
		seen[r.id], history[r.task] = true, true
		if r.status == Pending {
			if pending[r.task] {
				return ec.DispatchOccupancy{}, unavailable(nil)
			}
			pending[r.task] = true
			out.Pending = append(out.Pending, ec.PendingTaskDispatch{TaskID: r.task, DispatchID: r.id.String(), AgentID: r.agent, SprintID: r.sprint})
		}
	}
	if err = rows.Err(); err != nil {
		return ec.DispatchOccupancy{}, portError(err)
	}
	slices.SortFunc(out.Pending, func(a, b ec.PendingTaskDispatch) int {
		if c := strings.Compare(a.TaskID, b.TaskID); c != 0 {
			return c
		}
		return strings.Compare(a.DispatchID, b.DispatchID)
	})
	for _, id := range ids {
		if history[id] {
			out.HistoryTaskIDs = append(out.HistoryTaskIDs, id)
		}
	}
	return out, ctx.Err()
}

func compareGroup(a, b ec.PendingClaimGroup) int {
	if n := strings.Compare(a.SprintID, b.SprintID); n != 0 {
		return n
	}
	if n := strings.Compare(a.State, b.State); n != 0 {
		return n
	}
	return strings.Compare(a.Priority, b.Priority)
}
func (a *PendingAuthority) RequireNoPendingGroupsInTx(ctx context.Context, tx f.Tx, p i.ProjectID, groups []ec.PendingClaimGroup) error {
	if ctx == nil || groups == nil {
		return invalid()
	}
	if len(groups) > ec.MaxWorkOccupancyTasks {
		return fault(f.PayloadTooLarge)
	}
	values := slices.Clone(groups)
	for n, g := range values {
		if !validID(g.SprintID) || !validState(g.State) || !validPriority(g.Priority) || n > 0 && compareGroup(values[n-1], g) >= 0 {
			return invalid()
		}
		if err := ctx.Err(); err != nil {
			return err
		}
	}
	x, err := a.inTx(ctx, tx, p)
	if err != nil {
		return err
	}
	// Every lookup uses the same Schedule EX snapshot and the dedicated partial
	// source-group index. The canonical owner protects even an empty live group.
	for _, g := range values {
		var exists bool
		err = x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_scheduler.dispatches
 WHERE project_id=$1 AND status='pending' AND claim_guard IS NOT NULL AND claim_source_sprint_id=$2 AND claim_source_state=$3 AND claim_source_priority=$4)`, p.String(), g.SprintID, g.State, g.Priority).Scan(&exists)
		if err != nil {
			return portError(err)
		}
		if err = ctx.Err(); err != nil {
			return err
		}
		if exists {
			return fault(f.ResourceBusy)
		}
	}
	return ctx.Err()
}

var _ ec.PendingDispatchReader = (*PendingAuthority)(nil)
var _ ec.PendingClaimGroupGuard = (*PendingAuthority)(nil)
