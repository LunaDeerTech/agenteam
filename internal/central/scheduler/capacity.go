package scheduler

import (
	"context"
	"math"

	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
)

func readCapacityRecords(ctx context.Context, x postgres.SQLExecutor, p i.ProjectID) (out []*dispatchRecord, err error) {
	rows, err := x.Query(ctx, `SELECT `+dispatchColumns+` FROM agenteam_scheduler.dispatches WHERE project_id=$1 AND status IN ('pending','launched') ORDER BY id COLLATE "C"`, p.String())
	if err != nil {
		return nil, portError(err)
	}
	if rows == nil {
		return nil, unavailable(nil)
	}
	defer func() {
		rows.Close()
		if err == nil {
			err = portError(rows.Err())
		}
		if ctx.Err() != nil {
			err = ctx.Err()
		}
		if err != nil {
			out = nil
		}
	}()
	out = []*dispatchRecord{}
	for rows.Next() {
		if err = ctx.Err(); err != nil {
			return nil, err
		}
		r, e := scanDispatch(rows)
		if e != nil {
			return nil, e
		}
		if r == nil || r.project != p || r.status != Pending && r.status != Launched || len(out) > 0 && out[len(out)-1].id.String() >= r.id.String() {
			return nil, unavailable(nil)
		}
		out = append(out, r)
	}
	return out, portError(rows.Err())
}
func sameCapacityRecords(a, b []*dispatchRecord) bool {
	if len(a) != len(b) {
		return false
	}
	for n, x := range a {
		y := b[n]
		if x.id != y.id || x.version != y.version || x.status != y.status || x.digest != y.digest {
			return false
		}
	}
	return true
}
func capacityLocks(rows []*dispatchRecord) []f.LockRequest {
	var out []f.LockRequest
	for _, r := range rows {
		if r.status != Launched {
			continue
		}
		ak, _ := f.AgentLock(r.agent.String())
		cmd, _ := r.launch.Command()
		ck, _ := f.CommandLock(cmd)
		out = append(out, f.LockRequest{Key: ak, Mode: f.Shared}, f.LockRequest{Key: ck, Mode: f.Exclusive})
	}
	return out
}
func (s *Coordinator) capacityInTx(ctx context.Context, tx f.Tx, rows []*dispatchRecord) (int64, error) {
	var count int64
	for _, r := range rows {
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		if r.status == Pending {
			count++
		} else {
			found, err := s.deps.Executions.LookupLaunchInTx(ctx, tx, lookupKey(r), r.digest, r.id.String())
			if err != nil {
				return 0, portError(err)
			}
			if !found.Found || found.Execution == nil || r.execution == nil || found.Execution.ID != *r.execution || found.RequestDigest != r.digest || found.Execution.ProjectID != r.project || found.Execution.AgentID != r.agent || !found.Execution.Status.Valid() {
				return 0, unavailable(nil)
			}
			switch found.Execution.Status {
			case ec.Created, ec.Preparing, ec.Running:
				count++
			}
		}
		if count == math.MaxInt64 {
			return 0, fault(f.InvalidState)
		}
	}
	return count, nil
}
