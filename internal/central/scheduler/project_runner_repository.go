package scheduler

import (
	"context"
	"slices"
	"strings"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	wc "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
)

type projectTaskEntry struct {
	task  wc.TaskID
	state wc.TaskState
}
type projectTraversal struct {
	sprint  *wc.SprintID
	enabled bool
	entries []projectTaskEntry
}
type traversalPending struct {
	dispatch DispatchID
	task     wc.TaskID
}

const traversalPendingSQL = `SELECT id,task_id FROM agenteam_scheduler.dispatches
 WHERE project_id=$1 AND status='pending' ORDER BY id COLLATE "C" LIMIT 65537`
const traversalTaskPendingSQL = `SELECT ` + dispatchColumns + ` FROM agenteam_scheduler.dispatches
 WHERE project_id=$1 AND task_id=$2 AND status='pending'`

func (s *ProjectRunner) traversalLocks(name string, task *wc.TaskID) (f.TransactionCause, []f.LockRequest, error) {
	cmd, err := f.NewCommandIdentity("scheduler", []string{s.options.ProjectID.String()}, name, "project_traversal")
	if err != nil {
		return f.TransactionCause{}, nil, err
	}
	key, err := f.CommandLock(cmd)
	if err != nil {
		return f.TransactionCause{}, nil, err
	}
	locks := append(pendingLocks(s.options.ProjectID), f.LockRequest{Key: key, Mode: f.Exclusive})
	if task != nil {
		key, e := f.AggregateLock(f.TaskAggregate, task.String())
		if e != nil {
			return f.TransactionCause{}, nil, e
		}
		locks = append(locks, f.LockRequest{Key: key, Mode: f.Shared})
	}
	cause, err := f.NewCommandsCause(cmd)
	return cause, locks, err
}
func (s *ProjectRunner) readProject(ctx context.Context, tx f.Tx) (pc.SchedulerProject, error) {
	v, err := s.coordinator.deps.Projects.RequireSchedulerProjectInTx(ctx, tx, s.options.ProjectID)
	if ctx.Err() != nil {
		return pc.SchedulerProject{}, ctx.Err()
	}
	if err != nil {
		return pc.SchedulerProject{}, portError(err)
	}
	if v.Project.ID != s.options.ProjectID || v.Config.Validate() != nil || v.Project.CurrentSprintID != nil && v.Project.CurrentSprintID.Validate() != nil {
		return pc.SchedulerProject{}, unavailable(nil)
	}
	return v.Clone(), nil
}
func (s *ProjectRunner) captureTraversal(ctx context.Context) (projectTraversal, error) {
	cause, locks, err := s.traversalLocks("capture_traversal", nil)
	if err != nil {
		return projectTraversal{}, err
	}
	a := s.coordinator.authority
	var current pc.SchedulerProject
	var tasks wc.SchedulerTaskSnapshot
	var pending []traversalPending
	result := a.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := a.store.AcquireAll(ctx, tx, locks); err != nil {
			return portError(err)
		}
		x, err := a.inTx(ctx, tx, s.options.ProjectID)
		if err != nil {
			return err
		}
		current, err = s.readProject(ctx, tx)
		if err != nil {
			return err
		}
		pending, err = s.readPending(ctx, x, s.options.ProjectID)
		if err != nil {
			return err
		}
		if len(pending) > wc.MaxProjectTasks {
			return fault(f.PayloadTooLarge)
		}
		previousPending := ""
		pendingTasks := make(map[wc.TaskID]bool, len(pending))
		for _, entry := range pending {
			if entry.dispatch.Validate() != nil || entry.task.Validate() != nil || entry.dispatch.String() <= previousPending || pendingTasks[entry.task] {
				return unavailable(nil)
			}
			previousPending = entry.dispatch.String()
			pendingTasks[entry.task] = true
		}
		tasks, err = s.options.Tasks.SnapshotInTx(ctx, tx, s.options.ProjectID)
		if err != nil {
			return portError(err)
		}
		tasks = tasks.Clone()
		if tasks.ProjectID != s.options.ProjectID || !sameTraversalSprint(tasks.CurrentSprintID, current.Project.CurrentSprintID) || tasks.Entries == nil || len(tasks.Entries) > wc.MaxProjectTasks || tasks.CurrentSprintID == nil && len(tasks.Entries) != 0 {
			return unavailable(nil)
		}
		seen := make(map[wc.TaskID]bool, len(tasks.Entries))
		previous := 0
		for _, entry := range tasks.Entries {
			order := entry.State.Order()
			if entry.TaskID.Validate() != nil || order < 1 || order > 4 || order < previous || seen[entry.TaskID] {
				return unavailable(nil)
			}
			seen[entry.TaskID] = true
			previous = order
			if err := ctx.Err(); err != nil {
				return err
			}
		}
		return ctx.Err()
	})
	if err = commitError(result); err != nil {
		return projectTraversal{}, err
	}
	if err = ctx.Err(); err != nil {
		return projectTraversal{}, err
	}
	// Retained claim attempts may have no visible row. Include those original
	// identities as recovery visits without treating absence as non-commit.
	s.coordinator.mu.Lock()
	for id, call := range s.coordinator.unknown {
		if call.request.ProjectID == s.options.ProjectID {
			if len(pending) == 2*wc.MaxProjectTasks {
				s.coordinator.mu.Unlock()
				return projectTraversal{}, fault(f.PayloadTooLarge)
			}
			pending = append(pending, traversalPending{dispatch: id, task: call.request.TaskID})
		}
	}
	s.coordinator.mu.Unlock()
	if relaunch := s.options.Relaunch; relaunch != nil {
		relaunch.mu.Lock()
		for id, call := range relaunch.unknown {
			if call.request.ProjectID == s.options.ProjectID {
				pending = append(pending, traversalPending{dispatch: id, task: call.request.TaskID})
			}
		}
		relaunch.mu.Unlock()
	}
	if len(pending) > 2*wc.MaxProjectTasks {
		return projectTraversal{}, fault(f.PayloadTooLarge)
	}
	slices.SortFunc(pending, func(a, b traversalPending) int { return strings.Compare(a.dispatch.String(), b.dispatch.String()) })
	out := projectTraversal{sprint: cloneSprintID(tasks.CurrentSprintID), enabled: current.Config.Enabled, entries: make([]projectTaskEntry, 0, len(pending)+len(tasks.Entries))}
	seen := make(map[wc.TaskID]DispatchID, len(pending)+len(tasks.Entries))
	for _, entry := range pending {
		if prior, ok := seen[entry.task]; ok {
			if prior != entry.dispatch {
				return projectTraversal{}, fault(f.ResourceBusy)
			}
			continue
		}
		seen[entry.task] = entry.dispatch
		out.entries = append(out.entries, projectTaskEntry{task: entry.task})
	}
	for _, entry := range tasks.Entries {
		if _, ok := seen[entry.TaskID]; ok {
			continue
		}
		seen[entry.TaskID] = DispatchID{}
		out.entries = append(out.entries, projectTaskEntry{task: entry.TaskID, state: entry.State})
	}
	return out, ctx.Err()
}
func sameTraversalSprint(a, b *wc.SprintID) bool {
	return a == nil && b == nil || a != nil && b != nil && *a == *b
}
func loadTraversalPending(ctx context.Context, x postgres.SQLExecutor, p i.ProjectID) ([]traversalPending, error) {
	rows, err := x.Query(ctx, traversalPendingSQL, p.String())
	if err != nil {
		return nil, portError(err)
	}
	if rows == nil {
		return nil, unavailable(nil)
	}
	return collectTraversalPending(ctx, rows)
}
func collectTraversalPending(ctx context.Context, rows dispatchRows) (out []traversalPending, err error) {
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
	out = make([]traversalPending, 0)
	seen := make(map[wc.TaskID]bool)
	previous := ""
	for rows.Next() {
		if err = ctx.Err(); err != nil {
			return nil, err
		}
		if len(out) == wc.MaxProjectTasks {
			return nil, fault(f.PayloadTooLarge)
		}
		var rawID, rawTask string
		if err = rows.Scan(&rawID, &rawTask); err != nil {
			return nil, portError(err)
		}
		id, e := f.ParseID[DispatchIdentity](rawID)
		if e != nil {
			return nil, unavailable(nil)
		}
		task, e := f.ParseID[wc.Task](rawTask)
		if e != nil || seen[task] || rawID <= previous {
			return nil, unavailable(nil)
		}
		previous = rawID
		seen[task] = true
		out = append(out, traversalPending{dispatch: id, task: task})
	}
	return out, portError(rows.Err())
}
func (s *ProjectRunner) readTraversalTask(ctx context.Context, task wc.TaskID, needFacts bool, sprint *wc.SprintID) (pc.SchedulerProject, *dispatchRecord, *wc.SchedulerTaskFacts, error) {
	cause, locks, err := s.traversalLocks("visit_traversal", &task)
	if err != nil {
		return pc.SchedulerProject{}, nil, nil, err
	}
	a := s.coordinator.authority
	var current pc.SchedulerProject
	var pending *dispatchRecord
	var facts *wc.SchedulerTaskFacts
	result := a.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := a.store.AcquireAll(ctx, tx, locks); err != nil {
			return portError(err)
		}
		x, err := a.inTx(ctx, tx, s.options.ProjectID)
		if err != nil {
			return err
		}
		current, err = s.readProject(ctx, tx)
		if err != nil {
			return err
		}
		pending, err = scanDispatch(x.QueryRow(ctx, traversalTaskPendingSQL, s.options.ProjectID.String(), task.String()))
		if err != nil {
			return err
		}
		if pending != nil && (pending.project != s.options.ProjectID || pending.task != task.String() || pending.status != Pending) {
			return unavailable(nil)
		}
		if !needFacts || !current.Config.Enabled || pending != nil || !sameTraversalSprint(sprint, current.Project.CurrentSprintID) {
			return ctx.Err()
		}
		v, err := s.options.Tasks.CurrentTaskInTx(ctx, tx, s.options.ProjectID, task)
		if isMissingTraversalTask(err) {
			return ctx.Err()
		}
		if err != nil {
			return portError(err)
		}
		v = v.Clone()
		if v.ProjectID != s.options.ProjectID || v.TaskID != task || v.SprintID.Validate() != nil || v.MilestoneID.Validate() != nil || v.State.Validate() != nil || v.Priority.Validate() != nil || v.Version.Validate() != nil || v.AssigneeAgentID != nil && v.AssigneeAgentID.Validate() != nil {
			return unavailable(nil)
		}
		facts = &v
		return ctx.Err()
	})
	if err = commitError(result); err != nil {
		return pc.SchedulerProject{}, nil, nil, err
	}
	if err = ctx.Err(); err != nil {
		return pc.SchedulerProject{}, nil, nil, err
	}
	return current, pending, facts, nil
}
