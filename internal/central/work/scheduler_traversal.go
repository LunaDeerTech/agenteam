package work

import (
	"context"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
)

type SchedulerTaskReader struct {
	store    Store
	projects schedulerClaimProject
}

// Construction binds the original Work Authority's same-Store Project reader.
// No Human session, Scheduler actor, timer or mutation authority is created.
func NewSchedulerTaskReader(store Store, authority *Authority) (*SchedulerTaskReader, error) {
	if nilPort(store) || authority.state() == nil || !sameStore(store, authority.state().store) {
		return nil, fault(f.DependencyUnbound)
	}
	projects, ok := authority.state().projects.(schedulerClaimProject)
	if !ok || nilPort(projects) {
		return nil, fault(f.DependencyUnbound)
	}
	return &SchedulerTaskReader{store: store, projects: projects}, nil
}

func (r *SchedulerTaskReader) scope(ctx context.Context, tx f.Tx, p c.ProjectID, task *c.TaskID) (postgres.SQLExecutor, pc.SchedulerProject, error) {
	if ctx == nil || !tx.Valid() || p.Validate() != nil || task != nil && task.Validate() != nil {
		return nil, pc.SchedulerProject{}, fault(f.InvalidArgument)
	}
	if err := ctx.Err(); err != nil {
		return nil, pc.SchedulerProject{}, err
	}
	if r == nil || nilPort(r.store) || nilPort(r.projects) {
		return nil, pc.SchedulerProject{}, fault(f.DependencyUnbound)
	}
	x, err := r.store.InTx(tx)
	if err != nil {
		return nil, pc.SchedulerProject{}, portError(err)
	}
	if nilPort(x) {
		return nil, pc.SchedulerProject{}, fault(f.DependencyUnbound)
	}
	locks := []f.LockRequest{projectLock(p, f.Shared), taskScheduleLock(p, f.Exclusive)}
	if task != nil {
		locks = append(locks, taskLock(task.String(), f.Shared))
	}
	locks, err = taskNormalize(locks)
	if err != nil {
		return nil, pc.SchedulerProject{}, err
	}
	if err = r.store.RequireHeldLocks(ctx, tx, locks); err != nil {
		return nil, pc.SchedulerProject{}, portError(err)
	}
	project, err := r.projects.RequireSchedulerProjectInTx(ctx, tx, p)
	if ctx.Err() != nil {
		return nil, pc.SchedulerProject{}, ctx.Err()
	}
	if err != nil {
		return nil, pc.SchedulerProject{}, portError(err)
	}
	if project.Project.Validate() != nil || project.Project.ID != p || project.Config.Validate() != nil {
		return nil, pc.SchedulerProject{}, internal(nil)
	}
	if project.Project.Lifecycle != pc.Active {
		return nil, pc.SchedulerProject{}, fault(f.InvalidState)
	}
	return x, project.Clone(), nil
}

// Schedule EX freezes the complete ordered identity set against all Task/rank
// writers. Project SH protects the Sprint/structure relationship. No per-Task
// lock list or multi-transaction pagination is needed to obtain this snapshot.
const schedulerTaskSnapshotSQL = `SELECT id::text,project_id::text,sprint_id::text,state,priority,manual_rank
 FROM agenteam_work.tasks WHERE project_id=$1 AND sprint_id=$2
 AND state IN ('todo','in_progress','in_review','blocked')
 ORDER BY (` + taskStateOrderSQL + `),(` + taskPriorityOrderSQL + `),manual_rank COLLATE "C",id
 LIMIT $3`

func (r *SchedulerTaskReader) SnapshotInTx(ctx context.Context, tx f.Tx, p c.ProjectID) (c.SchedulerTaskSnapshot, error) {
	x, project, err := r.scope(ctx, tx, p, nil)
	if err != nil {
		return c.SchedulerTaskSnapshot{}, err
	}
	if project.Project.CurrentSprintID == nil {
		return c.SchedulerTaskSnapshot{ProjectID: p, Entries: []c.SchedulerTaskIdentity{}}, nil
	}
	if err = checkPointer(ctx, x, project.Project); err != nil {
		if ctx.Err() != nil {
			return c.SchedulerTaskSnapshot{}, ctx.Err()
		}
		return c.SchedulerTaskSnapshot{}, err
	}
	sprint := *project.Project.CurrentSprintID
	rows, err := x.Query(ctx, schedulerTaskSnapshotSQL, p.String(), sprint.String(), c.MaxProjectTasks+1)
	if err != nil {
		if ctx.Err() != nil {
			return c.SchedulerTaskSnapshot{}, ctx.Err()
		}
		return c.SchedulerTaskSnapshot{}, taskSQL(err)
	}
	if rows == nil {
		return c.SchedulerTaskSnapshot{}, internal(nil)
	}
	return collectSchedulerTaskSnapshot(ctx, rows, p, sprint)
}

type schedulerSnapshotRows interface {
	postgres.Row
	Next() bool
	Err() error
	Close()
}

func collectSchedulerTaskSnapshot(ctx context.Context, rows schedulerSnapshotRows, p c.ProjectID, sprint c.SprintID) (out c.SchedulerTaskSnapshot, err error) {
	defer func() {
		rows.Close()
		if err == nil && rows.Err() != nil {
			err = taskSQL(rows.Err())
		}
		if ctx.Err() != nil {
			err = ctx.Err()
		}
		if err != nil {
			out = c.SchedulerTaskSnapshot{}
		}
	}()
	out = c.SchedulerTaskSnapshot{ProjectID: p, CurrentSprintID: &sprint, Entries: []c.SchedulerTaskIdentity{}}
	seen := make(map[c.TaskID]bool)
	var priorState, priorPriority int
	var priorRank, priorID string
	for rows.Next() {
		if err = ctx.Err(); err != nil {
			return out, err
		}
		if len(out.Entries) >= c.MaxProjectTasks {
			return out, fault(f.ResourceBusy)
		}
		var id, projectID, sprintID, rank string
		var state c.TaskState
		var priority c.TaskPriority
		if err = rows.Scan(&id, &projectID, &sprintID, &state, &priority, &rank); err != nil {
			return out, taskSQL(err)
		}
		task, parseErr := f.ParseID[c.Task](id)
		if parseErr != nil || projectID != p.String() || sprintID != sprint.String() ||
			(state != c.TaskStateTodo && state != c.TaskStateInProgress && state != c.TaskStateInReview && state != c.TaskStateBlocked) || priority.Validate() != nil || c.ValidateRank(rank) != nil || seen[task] {
			return out, internal(nil)
		}
		st, pr := state.Order(), priority.Order()
		if len(out.Entries) > 0 && (st < priorState || st == priorState && (pr < priorPriority || pr == priorPriority && (rank < priorRank || rank == priorRank && id <= priorID))) {
			return out, internal(nil)
		}
		seen[task] = true
		out.Entries = append(out.Entries, c.SchedulerTaskIdentity{TaskID: task, State: state})
		priorState, priorPriority, priorRank, priorID = st, pr, rank, id
	}
	return out, nil
}

func (r *SchedulerTaskReader) CurrentTaskInTx(ctx context.Context, tx f.Tx, p c.ProjectID, id c.TaskID) (c.SchedulerTaskFacts, error) {
	x, _, err := r.scope(ctx, tx, p, &id)
	if err != nil {
		return c.SchedulerTaskFacts{}, err
	}
	task, err := loadTask(ctx, x, p, id)
	if ctx.Err() != nil {
		return c.SchedulerTaskFacts{}, ctx.Err()
	}
	if err != nil {
		return c.SchedulerTaskFacts{}, err
	}
	if task.ProjectID != p || task.ID != id {
		return c.SchedulerTaskFacts{}, internal(nil)
	}
	var blockers int64
	err = x.QueryRow(ctx, `SELECT count(*) FROM agenteam_work.task_blockers WHERE project_id=$1 AND task_id=$2 AND resolved_at IS NULL`, p.String(), id.String()).Scan(&blockers)
	if ctx.Err() != nil {
		return c.SchedulerTaskFacts{}, ctx.Err()
	}
	if err != nil {
		return c.SchedulerTaskFacts{}, taskSQL(err)
	}
	if blockers < 0 {
		return c.SchedulerTaskFacts{}, internal(nil)
	}
	return (c.SchedulerTaskFacts{ProjectID: p, TaskID: id, MilestoneID: task.MilestoneID, SprintID: task.SprintID, State: task.State, Priority: task.Priority, Version: task.Version, AssigneeAgentID: task.AssigneeAgentID, HasUnresolvedBlockers: blockers > 0}).Clone(), nil
}

var _ c.SchedulerTaskReader = (*SchedulerTaskReader)(nil)
