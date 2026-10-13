package work

import (
	"context"
	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
)

// Completion is explicitly bound separately from the original Start capability.
// A missing or partially bound provider never means an empty Sprint.
type SprintCompletionDependencies struct {
	Projects  pc.SprintCompletionAuthority
	Occupancy ec.WorkOccupancyReader
	Pending   ec.PendingDispatchReader
	Events    c.SprintCompletionEvents
}

func (d SprintCompletionDependencies) valid() bool {
	return !nilPort(d.Projects) && !nilPort(d.Occupancy) && !nilPort(d.Pending) && d.Events.Valid()
}
func (s *SprintLifecycleService) completionBound() bool {
	return s.bound() && s.completion != nil && s.completion.valid()
}

// Project EX and Schedule EX exclude every canonical Task/member mutation and
// Scheduler claim. Completion reads every member, changes no Task, and requires
// no per-Task lock or paginated read. The same gates remain held through the
// occupancy reads, Sprint write and exact Project pointer change.
func (s *SprintLifecycleService) checkCompletionMembers(ctx context.Context, tx f.Tx, x postgres.SQLExecutor, project c.ProjectID, sprint c.SprintID) error {
	if err := s.base.state().store.RequireHeldLocks(ctx, tx, []f.LockRequest{projectLock(project, f.Exclusive), taskScheduleLock(project, f.Exclusive), sprintLock(sprint.String(), f.Exclusive)}); err != nil {
		return portError(err)
	}
	ids, err := sprintCompletionMembers(ctx, x, project, sprint)
	if err != nil {
		return err
	}
	return s.checkCompletionOccupancy(ctx, tx, project, ids)
}

func (s *SprintLifecycleService) checkCompletionOccupancy(ctx context.Context, tx f.Tx, project c.ProjectID, ids []string) error {
	occupied, err := s.completion.Occupancy.ReadInTx(ctx, tx, project, ids)
	if err != nil {
		return portError(err)
	}
	if occupied.Active == nil || occupied.HistoryTaskIDs == nil {
		return internal(nil)
	}
	if len(occupied.Active) != 0 {
		return field(f.ResourceBusy, "/sprint_id", "ACTIVE_EXECUTIONS")
	}
	pending, err := s.completion.Pending.ReadInTx(ctx, tx, project, ids)
	if err != nil {
		return portError(err)
	}
	if pending.Pending == nil || pending.HistoryTaskIDs == nil {
		return internal(nil)
	}
	if len(pending.Pending) != 0 {
		return field(f.ResourceBusy, "/sprint_id", "PENDING_DISPATCHES")
	}
	return ctx.Err()
}

func sprintCompletionMembers(ctx context.Context, x postgres.SQLExecutor, project c.ProjectID, sprint c.SprintID) (out []string, err error) {
	rows, err := x.Query(ctx, `SELECT id::text,state FROM agenteam_work.tasks WHERE project_id=$1 AND sprint_id=$2 ORDER BY id::text COLLATE "C"`, project.String(), sprint.String())
	if err != nil {
		return nil, portError(err)
	}
	if rows == nil {
		return nil, unavailable(nil)
	}
	return collectSprintCompletionMembers(ctx, rows)
}

type sprintCompletionRows interface {
	postgres.Row
	Next() bool
	Err() error
	Close()
}

func collectSprintCompletionMembers(ctx context.Context, rows sprintCompletionRows) (out []string, err error) {
	defer func() {
		rows.Close()
		if err == nil {
			err = portError(rows.Err())
		}
		if ctx.Err() != nil {
			err = canceled(ctx.Err())
		}
		if err != nil {
			out = nil
		}
	}()
	out = []string{}
	unfinished := false
	for rows.Next() {
		if err = ctx.Err(); err != nil {
			return nil, canceled(err)
		}
		var id string
		var state c.TaskState
		if err = rows.Scan(&id, &state); err != nil {
			return nil, portError(err)
		}
		value, e := f.ParseID[c.Task](id)
		if e != nil || value.String() != id || state.Validate() != nil || len(out) > 0 && out[len(out)-1] >= id {
			return nil, internal(nil)
		}
		if len(out) >= taskProjectCap {
			return nil, fault(f.PayloadTooLarge)
		}
		out = append(out, id)
		unfinished = unfinished || !state.Terminal()
	}
	if err = rows.Err(); err != nil {
		return nil, portError(err)
	}
	if unfinished {
		return nil, field(f.InvalidState, "/sprint_id", "UNFINISHED_TASKS")
	}
	return out, nil
}
