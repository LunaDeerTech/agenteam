package execution

import (
	"context"
	"slices"

	c "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
)

// WorkOccupancy reads Execution's canonical facts in the caller's transaction.
// Construction performs no I/O and creates no empty-success fallback.
type WorkOccupancy struct{ store Store }

func NewWorkOccupancy(store Store) (*WorkOccupancy, error) {
	if nilPort(store) {
		return nil, fault(f.DependencyUnbound)
	}
	return &WorkOccupancy{store: store}, nil
}

// The existing Project-leading index bounds the domain being examined. Select
// every active identity and at most one terminal representative per Task; the
// history result is existence, not a truncated execution history. Only scalar
// identity/state columns are materialized before loading each selected original
// row for strict scanExecution validation. One statement observes both sets.
const workOccupancySQL = `WITH matching AS MATERIALIZED (
 SELECT id::text AS execution_id,status,launch_request->'trigger'->>'task_id' AS task_id
 FROM agenteam_execution.executions
 WHERE project_id=$1 AND launch_request->'trigger'->>'kind'='task'
 AND launch_request->'trigger'->>'task_id'=ANY($2::text[])
), selected AS (
 SELECT execution_id FROM matching WHERE status IN ('created','preparing','running','waiting')
 UNION ALL
 SELECT min(execution_id) FROM matching WHERE status NOT IN ('created','preparing','running','waiting') GROUP BY task_id
)
SELECT ` + executionColumns + ` FROM agenteam_execution.executions
WHERE project_id=$1 AND id::text IN (SELECT execution_id FROM selected)
ORDER BY launch_request->'trigger'->>'task_id' COLLATE "C",id COLLATE "C"`

func occupancyLocks(project i.ProjectID) []f.LockRequest {
	key, _ := f.ProjectScheduleLock(project.String())
	return []f.LockRequest{projectLock(project), {Key: key, Mode: f.Exclusive}}
}

func (r *WorkOccupancy) ReadInTx(ctx context.Context, tx f.Tx, project i.ProjectID, taskIDs []string) (c.ExecutionOccupancy, error) {
	if ctx == nil || project.Validate() != nil || taskIDs == nil {
		return c.ExecutionOccupancy{}, invalid()
	}
	if err := ctx.Err(); err != nil {
		return c.ExecutionOccupancy{}, err
	}
	if len(taskIDs) > c.MaxWorkOccupancyTasks {
		return c.ExecutionOccupancy{}, fault(f.PayloadTooLarge)
	}
	ids := slices.Clone(taskIDs)
	for n, task := range ids {
		if _, err := f.ParseID[struct{}](task); err != nil || n > 0 && ids[n-1] >= task {
			return c.ExecutionOccupancy{}, invalid()
		}
		if err := ctx.Err(); err != nil {
			return c.ExecutionOccupancy{}, err
		}
	}
	if r == nil || nilPort(r.store) {
		return c.ExecutionOccupancy{}, fault(f.DependencyUnbound)
	}
	x, err := r.store.InTx(tx)
	if err != nil {
		return c.ExecutionOccupancy{}, portError(err)
	}
	if nilPort(x) {
		return c.ExecutionOccupancy{}, fault(f.DependencyUnbound)
	}
	if err = r.store.RequireHeldLocks(ctx, tx, occupancyLocks(project)); err != nil {
		return c.ExecutionOccupancy{}, portError(err)
	}
	if len(ids) == 0 {
		if err = ctx.Err(); err != nil {
			return c.ExecutionOccupancy{}, err
		}
		return c.ExecutionOccupancy{Active: []c.ActiveTaskExecution{}, HistoryTaskIDs: []string{}}, nil
	}
	rows, err := x.Query(ctx, workOccupancySQL, project.String(), ids)
	if err != nil {
		if ctx.Err() != nil {
			return c.ExecutionOccupancy{}, ctx.Err()
		}
		return c.ExecutionOccupancy{}, portError(err)
	}
	if rows == nil {
		return c.ExecutionOccupancy{}, unavailable(nil)
	}
	return collectWorkOccupancy(ctx, rows, project, ids)
}

// The private iterator seam allows controlled decoding/Close failures to be
// tested without exposing or manufacturing a postgres.Rows/transaction.
type workOccupancyRows interface {
	postgres.Row
	Next() bool
	Err() error
	Close()
}

func collectWorkOccupancy(ctx context.Context, rows workOccupancyRows, project i.ProjectID, ids []string) (out c.ExecutionOccupancy, err error) {
	defer func() {
		rows.Close()
		if err == nil {
			err = portError(rows.Err())
		}
		if ctx.Err() != nil {
			err = ctx.Err()
		}
		if err != nil {
			out = c.ExecutionOccupancy{}
		}
	}()
	out = c.ExecutionOccupancy{Active: []c.ActiveTaskExecution{}, HistoryTaskIDs: []string{}}
	history := make(map[string]bool, len(ids))
	seen := make(map[i.ExecutionID]bool)
	for rows.Next() {
		if err = ctx.Err(); err != nil {
			return c.ExecutionOccupancy{}, err
		}
		var record *executionRecord
		record, err = scanExecution(rows)
		if err != nil {
			return c.ExecutionOccupancy{}, err
		}
		if record == nil || record.summary.ProjectID != project || record.summary.Trigger.Kind != "task" || seen[record.summary.ID] {
			return c.ExecutionOccupancy{}, unavailable(nil)
		}
		s := record.summary
		if _, found := slices.BinarySearch(ids, s.Trigger.TaskID); !found {
			return c.ExecutionOccupancy{}, unavailable(nil)
		}
		seen[s.ID], history[s.Trigger.TaskID] = true, true
		if !s.Status.Terminal() {
			out.Active = append(out.Active, c.ActiveTaskExecution{TaskID: s.Trigger.TaskID, ExecutionID: s.ID, AgentID: s.AgentID, Status: s.Status})
		}
	}
	if err = rows.Err(); err != nil {
		return c.ExecutionOccupancy{}, portError(err)
	}
	slices.SortFunc(out.Active, func(a, b c.ActiveTaskExecution) int {
		if a.TaskID < b.TaskID {
			return -1
		}
		if a.TaskID > b.TaskID {
			return 1
		}
		if a.ExecutionID.String() < b.ExecutionID.String() {
			return -1
		}
		if a.ExecutionID == b.ExecutionID {
			return 0
		}
		return 1
	})
	for _, task := range ids {
		if history[task] {
			out.HistoryTaskIDs = append(out.HistoryTaskIDs, task)
		}
	}
	return out, ctx.Err()
}

var _ c.WorkOccupancyReader = (*WorkOccupancy)(nil)
