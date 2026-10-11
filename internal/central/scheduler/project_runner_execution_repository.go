package scheduler

import (
	"context"

	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
)

const traversalLaunchedHighWaterSQL = `SELECT max(id::text COLLATE "C") FROM agenteam_scheduler.dispatches
 WHERE project_id=$1 AND status='launched'`

const traversalLaunchedSQL = `SELECT ` + dispatchColumns + ` FROM agenteam_scheduler.dispatches
 WHERE project_id=$1 AND status='launched' AND id::text COLLATE "C">$2::text COLLATE "C"
 AND id::text COLLATE "C"<=$3::text COLLATE "C"
 ORDER BY id COLLATE "C" LIMIT $4`

func validAssociatedDispatch(r *dispatchRecord, project i.ProjectID) bool {
	return r != nil && r.project == project && r.id.Validate() == nil && r.status == Launched && r.outcome == Created && r.execution != nil && r.execution.Validate() == nil && r.agent.Validate() == nil && r.launch.Validate() == nil && r.digest.Validate() == nil && r.launch.ProjectID == project && r.launch.AgentID == r.agent && r.launch.Trigger.Kind == "task" && r.launch.Trigger.TaskID == r.task && r.launch.Lineage.DispatchID == r.id.String()
}

func loadTraversalLaunched(ctx context.Context, x postgres.SQLExecutor, project i.ProjectID, after, through string, limit int) ([]*dispatchRecord, error) {
	rows, err := x.Query(ctx, traversalLaunchedSQL, project.String(), after, through, limit)
	if err != nil {
		return nil, portError(err)
	}
	if rows == nil {
		return nil, unavailable(nil)
	}
	return collectTraversalLaunched(ctx, rows, project, after, through, limit)
}

func collectTraversalLaunched(ctx context.Context, rows dispatchRows, project i.ProjectID, after, through string, limit int) (out []*dispatchRecord, err error) {
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
	if limit < 1 || limit > MaxExecutionHandoffPage+1 || project.Validate() != nil || after != "" && !validID(after) || !validID(through) || after > through {
		return nil, invalid()
	}
	out = make([]*dispatchRecord, 0, limit)
	previous := after
	seen := make(map[i.ExecutionID]bool, limit)
	for rows.Next() {
		if err = ctx.Err(); err != nil {
			return nil, err
		}
		if len(out) == limit {
			return nil, unavailable(nil)
		}
		r, e := scanDispatch(rows)
		if e != nil {
			return nil, e
		}
		if !validAssociatedDispatch(r, project) || r.id.String() <= previous || r.id.String() > through || seen[*r.execution] {
			return nil, unavailable(nil)
		}
		previous = r.id.String()
		seen[*r.execution] = true
		out = append(out, r)
	}
	return out, portError(rows.Err())
}

func (s *ProjectRunner) captureExecutionPage(ctx context.Context, after string, through *DispatchID) ([]*dispatchRecord, *DispatchID, error) {
	if after != "" && (!validID(after) || through == nil) || through != nil && (through.Validate() != nil || after > through.String()) {
		return nil, nil, invalid()
	}
	cause, locks, err := s.traversalLocks("capture_launched", nil)
	if err != nil {
		return nil, nil, err
	}
	a := s.coordinator.authority
	var rows []*dispatchRecord
	result := a.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := a.store.AcquireAll(ctx, tx, locks); err != nil {
			return portError(err)
		}
		x, err := a.inTx(ctx, tx, s.options.ProjectID)
		if err != nil {
			return err
		}
		// Current Project lifecycle remains real; scheduler_enabled only
		// controls new scheduling, not an already-created Execution.
		if _, err = s.readProject(ctx, tx); err != nil {
			return err
		}
		if through == nil {
			var highWater *string
			if err = x.QueryRow(ctx, traversalLaunchedHighWaterSQL, s.options.ProjectID.String()).Scan(&highWater); err != nil {
				return portError(err)
			}
			if highWater == nil {
				rows = []*dispatchRecord{}
				return ctx.Err()
			}
			id, e := f.ParseID[DispatchIdentity](*highWater)
			if e != nil {
				return unavailable(e)
			}
			through = &id
		}
		rows, err = s.readLaunched(ctx, x, s.options.ProjectID, after, through.String(), s.executionPageSize+1)
		if err != nil {
			return err
		}
		if rows == nil || len(rows) > s.executionPageSize+1 {
			return unavailable(nil)
		}
		previous := after
		seen := make(map[i.ExecutionID]bool, len(rows))
		for _, r := range rows {
			if !validAssociatedDispatch(r, s.options.ProjectID) || r.id.String() <= previous || r.id.String() > through.String() || seen[*r.execution] {
				return unavailable(nil)
			}
			previous = r.id.String()
			seen[*r.execution] = true
		}
		return ctx.Err()
	})
	if err = commitError(result); err != nil {
		return nil, nil, err
	}
	if err = ctx.Err(); err != nil {
		return nil, nil, err
	}
	return rows, through, nil
}

func (s *ProjectRunner) observeAssociatedExecution(ctx context.Context, expected *dispatchRecord) (*dispatchRecord, ec.Summary, error) {
	var zero ec.Summary
	if !validAssociatedDispatch(expected, s.options.ProjectID) {
		return nil, zero, fault(f.ConfirmationStale)
	}
	cause, locks, err := s.traversalLocks("observe_launched", nil)
	if err != nil {
		return nil, zero, err
	}
	locks, err = oc.NormalizeLocks(append(locks, executionIntentLocks(expected)...))
	if err != nil {
		return nil, zero, portError(err)
	}
	a := s.coordinator.authority
	var actual *dispatchRecord
	var summary ec.Summary
	result := a.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := a.store.AcquireAll(ctx, tx, locks); err != nil {
			return portError(err)
		}
		x, err := a.inTx(ctx, tx, s.options.ProjectID)
		if err != nil {
			return err
		}
		if _, err = s.readProject(ctx, tx); err != nil {
			return err
		}
		actual, err = loadDispatch(ctx, x, s.options.ProjectID, expected.id)
		if err != nil {
			return err
		}
		if !validAssociatedDispatch(actual, s.options.ProjectID) || !sameDispatch(actual, expected) || actual.version != expected.version || actual.attempts != expected.attempts || *actual.execution != *expected.execution {
			return fault(f.ConfirmationStale)
		}
		key := ec.LaunchLookupKey{ProjectID: actual.project, AgentID: actual.agent, IdempotencyKey: actual.launch.Meta.IdempotencyKey}
		observed, err := s.coordinator.deps.Executions.LookupLaunchInTx(ctx, tx, key, actual.digest, actual.id.String())
		if err != nil {
			return portError(err)
		}
		if !observed.Found || observed.RequestDigest != actual.digest || observed.Execution == nil {
			return fault(f.ConfirmationStale)
		}
		v := observed.Execution
		if v.ID != *actual.execution || v.ProjectID != actual.project || v.AgentID != actual.agent || v.Trigger.Kind != "task" || v.Trigger.TaskID != actual.task || v.Purpose != actual.launch.Purpose || !v.Status.Valid() || v.Version.Validate() != nil {
			return fault(f.ConfirmationStale)
		}
		summary = v.Clone()
		return ctx.Err()
	})
	if err = commitError(result); err != nil {
		return nil, zero, err
	}
	if err = ctx.Err(); err != nil {
		return nil, zero, err
	}
	return actual, summary, nil
}
