package work

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	c "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
	"github.com/jackc/pgx/v5"
)

type blockerScope struct {
	x       postgres.SQLExecutor
	project c.ProjectID
	task    c.TaskID
}

func (a *Authority) blockerScope(ctx context.Context, tx f.Tx, actor i.Actor, p c.ProjectID, t c.TaskID, write bool) (*blockerScope, error) {
	st := a.state()
	if st == nil {
		return nil, fault(f.DependencyUnbound)
	}
	x, e := st.store.InTx(tx)
	if e != nil {
		return nil, portError(e)
	}
	mode := f.Shared
	intent := i.Read
	if write {
		mode = f.Exclusive
		intent = i.Mutate
	}
	if e = st.store.RequireHeldLocks(ctx, tx, []f.LockRequest{userLock(actor.Details().UserID, f.Shared), projectLock(p, f.Shared), taskScheduleLock(p, mode), taskLock(t.String(), mode)}); e != nil {
		return nil, portError(e)
	}
	access, e := st.projects.RequireOwnerInTx(ctx, tx, actor, p, intent)
	if e != nil {
		return nil, portError(e)
	}
	if access.Project().ID != p {
		return nil, internal(nil)
	}
	return &blockerScope{x, p, t}, nil
}

type blockerRow struct {
	Value              c.TaskBlocker
	CreatedOperation   c.TaskBlockerCommandID
	ResolvedOperation  *c.TaskBlockerCommandID
	FailureOperation   *c.SchedulerClaimID
	ResolvedTransition *c.TaskTransitionCommandID
}

const blockerColumns = `id::text,project_id::text,task_id::text,type,description,metadata,created_at,created_by,(CASE WHEN type='technical' AND created_operation_id IS NULL THEN failure_operation_id WHEN type<>'technical' AND failure_operation_id IS NULL THEN created_operation_id END)::text,resolved_at,resolved_by,resolution_comment,resolved_operation_id::text,resolved_transition_operation_id::text`

func scanBlocker(row interface{ Scan(...any) error }) (*blockerRow, error) {
	var out blockerRow
	var id, p, t, created string
	var resolved, transition *string
	var at time.Time
	var done *time.Time
	var metadata, actor, resolver []byte
	v := &out.Value
	if e := row.Scan(&id, &p, &t, &v.Type, &v.Description, &metadata, &at, &actor, &created, &done, &resolver, &v.ResolutionComment, &resolved, &transition); e != nil {
		if errors.Is(e, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, taskSQL(e)
	}
	var e error
	if v.ID, e = f.ParseID[c.TaskBlockerIdentity](id); e != nil {
		return nil, internal(e)
	}
	if v.ProjectID, e = f.ParseID[i.Project](p); e != nil {
		return nil, internal(e)
	}
	if v.TaskID, e = f.ParseID[c.Task](t); e != nil {
		return nil, internal(e)
	}
	if v.CreatedAt, e = f.NewInstant(at); e != nil {
		return nil, internal(e)
	}
	if v.Type == c.TaskBlockerTechnical {
		id, err := f.ParseID[c.SchedulerClaim](created)
		if err != nil {
			return nil, internal(err)
		}
		out.FailureOperation = &id
	} else if out.CreatedOperation, e = f.ParseID[c.TaskBlockerCommand](created); e != nil {
		return nil, internal(e)
	}
	if resolved != nil {
		n, e := f.ParseID[c.TaskBlockerCommand](*resolved)
		if e != nil {
			return nil, internal(e)
		}
		out.ResolvedOperation = &n
	}
	if transition != nil {
		id, err := f.ParseID[c.TaskTransitionCommand](*transition)
		if err != nil {
			return nil, internal(err)
		}
		out.ResolvedTransition = &id
	}
	if v.Type == c.TaskBlockerTechnical {
		v.SchedulerCreatedBy = new(c.SchedulerTaskActor)
		e = v.SchedulerCreatedBy.UnmarshalJSON(actor)
		if e == nil && v.SchedulerCreatedBy.CauseID != created {
			return nil, internal(nil)
		}
	} else {
		e = v.CreatedBy.UnmarshalJSON(actor)
	}
	if e != nil {
		return nil, internal(e)
	}
	if done != nil {
		n, e := f.NewInstant(*done)
		if e != nil {
			return nil, internal(e)
		}
		v.ResolvedAt = &n
	}
	if resolver != nil {
		v.ResolvedBy = new(c.TaskEventActor)
		if e = v.ResolvedBy.UnmarshalJSON(resolver); e != nil {
			return nil, internal(e)
		}
	}
	if e = decodeBlockerMetadata(v, metadata); e != nil {
		return nil, e
	}
	if v.Validate() != nil || (out.ResolvedOperation == nil && out.ResolvedTransition == nil) != (v.ResolvedAt == nil) || out.ResolvedOperation != nil && out.ResolvedTransition != nil || v.Type == c.TaskBlockerTechnical && out.ResolvedOperation != nil {
		return nil, internal(nil)
	}
	raw, e := json.Marshal(v)
	if e != nil || len(raw) > c.MaxTaskBlockerRecordBytes {
		return nil, internal(e)
	}
	return &out, nil
}
func decodeBlockerMetadata(v *c.TaskBlocker, raw []byte) error {
	var e error
	switch v.Type {
	case c.TaskBlockerTechnical:
		v.Technical = new(c.TaskBlockerTechnicalMetadata)
		e = v.Technical.UnmarshalJSON(raw)
	case c.TaskBlockerRelyOn:
		v.Metadata.RelyOn = new(c.TaskBlockerRelyOnMetadata)
		e = v.Metadata.RelyOn.UnmarshalJSON(raw)
	case c.TaskBlockerWaitingForHuman:
		v.Metadata.WaitingForHuman = new(c.TaskBlockerWaitingForHumanMetadata)
		e = v.Metadata.WaitingForHuman.UnmarshalJSON(raw)
	default:
		return internal(nil)
	}
	if e != nil {
		return internal(e)
	}
	return nil
}
func loadBlocker(ctx context.Context, s *blockerScope, id c.TaskBlockerID) (*blockerRow, error) {
	return scanBlocker(s.x.QueryRow(ctx, `SELECT `+blockerColumns+` FROM agenteam_work.task_blockers WHERE project_id=$1 AND task_id=$2 AND id=$3`, s.project.String(), s.task.String(), id.String()))
}

func (s *BlockerService) ListTaskBlockers(ctx context.Context, a i.Actor, p c.ProjectID, t c.TaskID, status c.TaskBlockerStatus) ([]c.TaskBlocker, error) {
	ctx, _, done, e := s.begin(ctx)
	if e != nil {
		return nil, e
	}
	defer done()
	if e = readInput(ctx, a, p); e != nil {
		return nil, e
	}
	if t.Validate() != nil || status.Validate() != nil {
		return nil, fault(f.InvalidArgument)
	}
	cause, e := readCause("task-blockers.list")
	if e != nil {
		return nil, e
	}
	locks, e := taskNormalize([]f.LockRequest{userLock(a.Details().UserID, f.Shared), projectLock(p, f.Shared), taskScheduleLock(p, f.Shared), taskLock(t.String(), f.Shared)})
	if e != nil {
		return nil, e
	}
	out := []c.TaskBlocker{}
	result := s.state().store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if e := s.state().store.AcquireAll(ctx, tx, locks); e != nil {
			return portError(e)
		}
		scope, e := s.state().deps.Authority.blockerScope(ctx, tx, a, p, t, false)
		if e != nil {
			return e
		}
		if _, e = loadTask(ctx, scope.x, p, t); e != nil {
			return e
		}
		var count int64
		if e = scope.x.QueryRow(ctx, `SELECT count(*) FROM agenteam_work.task_blockers WHERE project_id=$1 AND task_id=$2`, p.String(), t.String()).Scan(&count); e != nil {
			return taskSQL(e)
		}
		if count > blockerHistoryCap {
			return fault(f.ResourceBusy)
		}
		query := `SELECT ` + blockerColumns + ` FROM agenteam_work.task_blockers WHERE project_id=$1 AND task_id=$2`
		if status == c.TaskBlockersUnresolved {
			query += ` AND resolved_at IS NULL`
		} else if status == c.TaskBlockersResolved {
			query += ` AND resolved_at IS NOT NULL`
		}
		query += ` ORDER BY created_at,id`
		rows, e := scope.x.Query(ctx, query, p.String(), t.String())
		if e != nil {
			return taskSQL(e)
		}
		defer rows.Close()
		for rows.Next() {
			if len(out) >= blockerHistoryCap {
				return fault(f.ResourceBusy)
			}
			v, e := scanBlocker(rows)
			if e != nil {
				return e
			}
			if v == nil {
				return internal(nil)
			}
			out = append(out, v.Value.Clone())
		}
		if err := rows.Err(); err != nil {
			return taskSQL(err)
		}
		return nil
	})
	if e = taskTxError(ctx, result); e != nil {
		return nil, e
	}
	return out, nil
}

// The counts and every streamed fact are read under one Schedule EX lock.
// Only compact node/edge identities are retained, never complete descriptions.
func validateBlockerGraph(ctx context.Context, s *blockerScope, add *c.TaskBlockerCreate) error {
	var nodes, unresolved, history, target, maxPerTask int64
	if e := s.x.QueryRow(ctx, `SELECT (SELECT count(*) FROM agenteam_work.tasks WHERE project_id=$1),(SELECT count(*) FROM agenteam_work.task_blockers WHERE project_id=$1 AND resolved_at IS NULL),(SELECT count(*) FROM agenteam_work.task_blockers WHERE project_id=$1 AND task_id=$2),(SELECT count(*) FROM agenteam_work.task_blockers WHERE project_id=$1 AND task_id=$2 AND resolved_at IS NULL),(SELECT coalesce(max(n),0) FROM (SELECT count(*) n FROM agenteam_work.task_blockers WHERE project_id=$1 AND resolved_at IS NULL GROUP BY task_id) counts)`, s.project.String(), s.task.String()).Scan(&nodes, &unresolved, &history, &target, &maxPerTask); e != nil {
		return taskSQL(e)
	}
	if nodes > taskProjectCap || unresolved > blockerProjectCap || maxPerTask > blockerTaskCap || history > blockerHistoryCap {
		return fault(f.ResourceBusy)
	}
	if add != nil && history >= blockerHistoryCap {
		return field(f.ResourceBusy, "/blocker_id", "BLOCKER_HISTORY_LIMIT")
	}
	if add != nil && (unresolved >= blockerProjectCap || target >= blockerTaskCap) {
		return fault(f.ResourceBusy)
	}
	live := make(map[c.TaskID]struct{}, int(nodes))
	rows, e := s.x.Query(ctx, `SELECT id::text FROM agenteam_work.tasks WHERE project_id=$1`, s.project.String())
	if e != nil {
		return taskSQL(e)
	}
	for rows.Next() {
		if len(live) >= taskProjectCap {
			rows.Close()
			return fault(f.ResourceBusy)
		}
		var text string
		if e = rows.Scan(&text); e != nil {
			rows.Close()
			return taskSQL(e)
		}
		id, e := f.ParseID[c.Task](text)
		if e != nil {
			rows.Close()
			return internal(e)
		}
		live[id] = struct{}{}
	}
	rows.Close()
	if e = rows.Err(); e != nil {
		return taskSQL(e)
	}
	if int64(len(live)) != nodes {
		return internal(nil)
	}
	edges := make(map[c.TaskID][]c.TaskID)
	perTask := map[c.TaskID]int{}
	seen := int64(0)
	rows, e = s.x.Query(ctx, `SELECT id::text,task_id::text,type,metadata FROM agenteam_work.task_blockers WHERE project_id=$1 AND resolved_at IS NULL`, s.project.String())
	if e != nil {
		return taskSQL(e)
	}
	defer rows.Close()
	for rows.Next() {
		seen++
		if seen > blockerProjectCap {
			return fault(f.ResourceBusy)
		}
		var id, task string
		var v c.TaskBlocker
		var raw []byte
		if e = rows.Scan(&id, &task, &v.Type, &raw); e != nil {
			return taskSQL(e)
		}
		if _, e = f.ParseID[c.TaskBlockerIdentity](id); e != nil {
			return internal(e)
		}
		from, e := f.ParseID[c.Task](task)
		if e != nil {
			return internal(e)
		}
		if _, ok := live[from]; !ok {
			return internal(nil)
		}
		perTask[from]++
		if perTask[from] > blockerTaskCap {
			return fault(f.ResourceBusy)
		}
		if e = decodeBlockerMetadata(&v, raw); e != nil {
			return e
		}
		if v.Type == c.TaskBlockerRelyOn {
			to := v.Metadata.RelyOn.RelatedTaskID
			if _, ok := live[to]; !ok || from == to {
				return internal(nil)
			}
			edges[from] = append(edges[from], to)
		}
	}
	rows.Close()
	if e = rows.Err(); e != nil {
		return taskSQL(e)
	}
	if seen != unresolved {
		return internal(nil)
	}
	if add != nil && add.Type == c.TaskBlockerRelyOn {
		if blockerReachable(edges, add.Metadata.RelyOn.RelatedTaskID, s.task) {
			return fault(f.TaskDependencyCycle)
		}
	}
	return nil
}
func blockerReachable(edges map[c.TaskID][]c.TaskID, start, target c.TaskID) bool {
	stack := []c.TaskID{start}
	seen := map[c.TaskID]bool{}
	for len(stack) > 0 {
		v := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if v == target {
			return true
		}
		if seen[v] {
			continue
		}
		seen[v] = true
		for _, to := range edges[v] {
			if !seen[to] {
				stack = append(stack, to)
			}
		}
	}
	return false
}
