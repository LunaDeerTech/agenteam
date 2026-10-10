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

// The original Schedule EX freezes pending rows and all Task Execution status
// writers for this transaction. History does not add per-Agent/Command locks:
// every persisted association is checked by its real owner in bounded batches.
func (s *Coordinator) capacityInTx(ctx context.Context, tx f.Tx, p i.ProjectID, rows []*dispatchRecord) (int64, error) {
	var count int64
	batch := make([]ec.AssociatedDispatch, 0, ec.MaxDispatchCapacityBatch)
	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		n, err := s.deps.Capacity.CountAssociatedInTx(ctx, tx, p, batch)
		if err != nil {
			return portError(err)
		}
		if n < 0 || n > int64(len(batch)) || count > math.MaxInt64-n {
			return unavailable(nil)
		}
		count += n
		batch = batch[:0]
		return ctx.Err()
	}
	for _, r := range rows {
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		if r == nil || r.project != p {
			return 0, unavailable(nil)
		}
		if r.status == Pending {
			if count == math.MaxInt64 {
				return 0, fault(f.InvalidState)
			}
			count++
		} else if r.status == Launched {
			if r.execution == nil {
				return 0, unavailable(nil)
			}
			batch = append(batch, ec.AssociatedDispatch{ExecutionID: *r.execution, AgentID: r.agent, Key: r.launch.Meta.IdempotencyKey, Digest: r.digest, DispatchID: r.id.String()})
			if len(batch) == ec.MaxDispatchCapacityBatch {
				if err := flush(); err != nil {
					return 0, err
				}
			}
		} else {
			return 0, unavailable(nil)
		}
	}
	if err := flush(); err != nil {
		return 0, err
	}
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	return count, nil
}
