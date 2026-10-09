package work

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"

	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	c "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
)

type TaskReader struct{ data func() *taskReaderState }
type taskReaderState struct {
	store     Store
	authority *Authority
	structure *Reader
	cursors   cursor.Keyring
}

func NewTaskReader(store Store, authority *Authority, structure *Reader, keys cursor.Keyring) (*TaskReader, error) {
	if nilPort(store) || authority.state() == nil || structure.state() == nil ||
		!sameStore(store, authority.state().store) || !sameStore(store, structure.state().store) ||
		structure.state().authority != authority || keys.Validate() != nil {
		return nil, fault(f.DependencyUnbound)
	}
	st := &taskReaderState{store: store, authority: authority, structure: structure, cursors: keys}
	return &TaskReader{data: func() *taskReaderState { return st }}, nil
}

func (r *TaskReader) state() *taskReaderState {
	if r == nil || r.data == nil {
		return nil
	}
	return r.data()
}

func (r *TaskReader) read(ctx context.Context, actor i.Actor, project c.ProjectID, extra []f.LockRequest, fn func(context.Context, postgres.SQLExecutor) error) error {
	if err := readInput(ctx, actor, project); err != nil {
		return err
	}
	st := r.state()
	if st == nil {
		return fault(f.DependencyUnbound)
	}
	locks, err := oc.NormalizeLocks(append([]f.LockRequest{userLock(actor.Details().UserID, f.Shared), projectLock(project, f.Shared), taskScheduleLock(project, f.Shared)}, extra...))
	if err != nil {
		return portError(err)
	}
	cause, err := readCause("task.read")
	if err != nil {
		return err
	}
	result := st.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := st.store.AcquireAll(ctx, tx, locks); err != nil {
			return portError(err)
		}
		x, err := st.store.InTx(tx)
		if err != nil {
			return portError(err)
		}
		access, err := st.authority.state().projects.RequireOwnerInTx(ctx, tx, actor, project, i.Read)
		if err != nil {
			return portError(err)
		}
		if access.Project().ID != project {
			return internal(nil)
		}
		return fn(ctx, x)
	})
	if result.State() == f.NotCommitted && ctx.Err() != nil {
		return canceled(ctx.Err())
	}
	return txError(result)
}

func (r *TaskReader) GetTask(ctx context.Context, actor i.Actor, project c.ProjectID, id c.TaskID) (c.Task, error) {
	if err := readInput(ctx, actor, project); err != nil {
		return c.Task{}, err
	}
	if id.Validate() != nil {
		return c.Task{}, fault(f.InvalidArgument)
	}
	var out c.Task
	err := r.read(ctx, actor, project, []f.LockRequest{taskLock(id.String(), f.Shared)}, func(ctx context.Context, x postgres.SQLExecutor) error {
		v, err := loadTask(ctx, x, project, id)
		if err != nil {
			return err
		}
		if _, err = loadTaskQueryGeneration(ctx, x, project); err != nil {
			return err
		}
		out = v.Clone()
		return nil
	})
	if err != nil {
		return c.Task{}, err
	}
	return out, nil
}

const taskPageOrder = "sprint:asc,state_order:asc,priority_order:asc,manual_rank:asc,id:asc"
const taskStateOrderSQL = "CASE state WHEN 'backlog' THEN 0 WHEN 'todo' THEN 1 WHEN 'in_progress' THEN 2 WHEN 'in_review' THEN 3 WHEN 'blocked' THEN 4 WHEN 'done' THEN 5 WHEN 'cancelled' THEN 6 END"
const taskPriorityOrderSQL = "CASE priority WHEN 'critical' THEN 0 WHEN 'high' THEN 1 WHEN 'medium' THEN 2 WHEN 'low' THEN 3 END"

func taskListBinding(project c.ProjectID, user string, filter c.TaskFilter) (cursor.Binding, error) {
	// Every optional predicate is expanded, including both assignee presence
	// and its nullable value. An omitted filter and unassigned are distinct.
	raw, err := json.Marshal(struct {
		Format  int         `json:"format"`
		Kind    string      `json:"kind"`
		Project c.ProjectID `json:"project_id"`
		User    string      `json:"owner_user_id"`
		Filters any         `json:"filters"`
	}{1, "work.tasks", project, user, struct {
		State    *c.TaskState    `json:"state"`
		Priority *c.TaskPriority `json:"priority"`
		Type     *c.TaskType     `json:"type"`
		Assignee struct {
			Present bool       `json:"present"`
			AgentID *i.AgentID `json:"agent_id"`
		} `json:"assignee_agent_id"`
		Milestone *c.MilestoneID `json:"milestone_id"`
		Sprint    *c.SprintID    `json:"sprint_id"`
		Text      *string        `json:"text"`
	}{filter.State, filter.Priority, filter.Type, struct {
		Present bool       `json:"present"`
		AgentID *i.AgentID `json:"agent_id"`
	}{filter.AssigneeAgentID.Present, filter.AssigneeAgentID.AgentID}, filter.MilestoneID, filter.SprintID, filter.Text}})
	if err != nil {
		return cursor.Binding{}, internal(err)
	}
	d, err := cursor.Digest(raw)
	if err != nil {
		return cursor.Binding{}, internal(err)
	}
	scope, err := i.InProject(project)
	if err != nil {
		return cursor.Binding{}, portError(err)
	}
	return cursor.Binding{Scope: scope, QueryDigest: d, Order: taskPageOrder}, nil
}

type taskPagePosition struct {
	sprint          string
	state, priority int64
	rank, id        string
}

func taskPageAfter(keys cursor.Keyring, token string, binding cursor.Binding, generation int64) (*taskPagePosition, error) {
	if token == "" {
		return nil, nil
	}
	p, err := keys.Verify(token, binding)
	if err != nil {
		return nil, err
	}
	if len(p.Scalars) != 5 || p.Scalars[0].Kind() != "uuid" || p.Scalars[1].Kind() != "integer" || p.Scalars[2].Kind() != "integer" || p.Scalars[3].Kind() != "text" || p.Scalars[4].Kind() != "uuid" || c.ValidateRank(p.Scalars[3].Value()) != nil || p.OrderGeneration == nil || *p.OrderGeneration < 1 {
		return nil, fault(f.CursorInvalid)
	}
	state, e1 := strconv.ParseInt(p.Scalars[1].Value(), 10, 64)
	priority, e2 := strconv.ParseInt(p.Scalars[2].Value(), 10, 64)
	if e1 != nil || e2 != nil || state < 0 || state > 6 || priority < 0 || priority > 3 {
		return nil, fault(f.CursorInvalid)
	}
	if *p.OrderGeneration != generation {
		return nil, fault(f.CursorStale)
	}
	return &taskPagePosition{p.Scalars[0].Value(), state, priority, p.Scalars[3].Value(), p.Scalars[4].Value()}, nil
}
func taskPageToken(keys cursor.Keyring, binding cursor.Binding, generation int64, v c.Task) (string, error) {
	sprint, err := cursor.UUID(v.SprintID.String())
	if err != nil {
		return "", err
	}
	rank, err := cursor.Text(v.ManualRank)
	if err != nil {
		return "", err
	}
	id, err := cursor.UUID(v.ID.String())
	if err != nil {
		return "", err
	}
	return keys.Sign(binding, cursor.Position{Scalars: []cursor.Scalar{sprint, cursor.Integer(int64(v.State.Order())), cursor.Integer(int64(v.Priority.Order())), rank, id}, OrderGeneration: &generation})
}

func (r *TaskReader) ListTasks(ctx context.Context, actor i.Actor, project c.ProjectID, filter c.TaskFilter, page f.PageRequest) (f.Page[c.Task], error) {
	if err := readInput(ctx, actor, project); err != nil {
		return f.Page[c.Task]{}, err
	}
	if err := filter.Validate(); err != nil {
		return f.Page[c.Task]{}, err
	}
	if page.Validate() != nil || len(page.Cursor) > cursor.MaxTokenBytes {
		return f.Page[c.Task]{}, fault(f.InvalidArgument)
	}
	filter = filter.Clone()
	out := f.Page[c.Task]{Items: []c.Task{}}
	err := r.read(ctx, actor, project, nil, func(ctx context.Context, x postgres.SQLExecutor) error {
		generation, err := loadTaskQueryGeneration(ctx, x, project)
		if err != nil {
			return err
		}
		binding, err := taskListBinding(project, actor.Details().UserID, filter)
		if err != nil {
			return err
		}
		after, err := taskPageAfter(r.state().cursors, page.Cursor, binding, generation)
		if err != nil {
			return err
		}
		args := []any{project.String()}
		where := []string{"project_id=$1"}
		bind := func(value any) string { args = append(args, value); return "$" + strconv.Itoa(len(args)) }
		if filter.State != nil {
			where = append(where, "state="+bind(string(*filter.State)))
		}
		if filter.Priority != nil {
			where = append(where, "priority="+bind(string(*filter.Priority)))
		}
		if filter.Type != nil {
			where = append(where, "type="+bind(string(*filter.Type)))
		}
		if filter.AssigneeAgentID.Present {
			if filter.AssigneeAgentID.AgentID == nil {
				where = append(where, "assignee_agent_id IS NULL")
			} else {
				where = append(where, "assignee_agent_id="+bind(filter.AssigneeAgentID.AgentID.String())+"::uuid")
			}
		}
		if filter.MilestoneID != nil {
			where = append(where, "milestone_id="+bind(filter.MilestoneID.String())+"::uuid")
		}
		if filter.SprintID != nil {
			where = append(where, "sprint_id="+bind(filter.SprintID.String())+"::uuid")
		}
		if filter.Text != nil {
			p := bind(*filter.Text)
			where = append(where, "(strpos(title,"+p+")>0 OR strpos(description,"+p+")>0 OR strpos(plan,"+p+")>0)")
		}
		if after != nil {
			where = append(where, "(sprint_id,("+taskStateOrderSQL+"),("+taskPriorityOrderSQL+"),manual_rank COLLATE \"C\",id)>("+bind(after.sprint)+"::uuid,"+bind(after.state)+"::integer,"+bind(after.priority)+"::integer,"+bind(after.rank)+"::text COLLATE \"C\","+bind(after.id)+"::uuid)")
		}
		query := "SELECT " + taskColumns + " FROM agenteam_work.tasks WHERE " + strings.Join(where, " AND ") + " ORDER BY sprint_id,(" + taskStateOrderSQL + "),(" + taskPriorityOrderSQL + "),manual_rank COLLATE \"C\",id LIMIT " + bind(page.Limit+1)
		rows, err := x.Query(ctx, query, args...)
		if err != nil {
			return unavailable(err)
		}
		defer rows.Close()
		for rows.Next() {
			v, e := scanTask(rows)
			if e != nil {
				return e
			}
			if v.ProjectID != project {
				return internal(nil)
			}
			out.Items = append(out.Items, v.Clone())
		}
		if err = rows.Err(); err != nil {
			return unavailable(err)
		}
		rows.Close()
		if len(out.Items) > page.Limit {
			last := out.Items[page.Limit-1]
			out.Items = out.Items[:page.Limit]
			out.NextCursor, err = taskPageToken(r.state().cursors, binding, generation, last)
		}
		return err
	})
	if err != nil {
		return f.Page[c.Task]{}, err
	}
	return out, nil
}

func (r *TaskReader) HasTasksInSprintInTx(ctx context.Context, tx f.Tx, actor i.Actor, project c.ProjectID, sprint c.SprintID) (bool, error) {
	if err := readInput(ctx, actor, project); err != nil {
		return false, err
	}
	if sprint.Validate() != nil {
		return false, fault(f.InvalidArgument)
	}
	st := r.state()
	if st == nil {
		return false, fault(f.DependencyUnbound)
	}
	x, err := st.store.InTx(tx)
	if err != nil {
		return false, portError(err)
	}
	locks := []f.LockRequest{userLock(actor.Details().UserID, f.Shared), projectLock(project, f.Shared), taskScheduleLock(project, f.Exclusive), sprintLock(sprint.String(), f.Shared)}
	if err = st.store.RequireHeldLocks(ctx, tx, locks); err != nil {
		return false, portError(err)
	}
	access, err := st.authority.state().projects.RequireOwnerInTx(ctx, tx, actor, project, i.Read)
	if err != nil {
		return false, portError(err)
	}
	if access.Project().ID != project {
		return false, internal(nil)
	}
	placement, err := st.structure.ReadPlacementInTx(ctx, tx, actor, project, sprint)
	if err != nil {
		if isNotFound(err) {
			return false, field(f.TaskSprintInvalid, "/sprint_id", "INVALID_SPRINT")
		}
		return false, err
	}
	if placement.Sprint.ID != sprint || placement.Sprint.ProjectID != project || placement.Milestone.ProjectID != project || placement.Sprint.MilestoneID != placement.Milestone.ID {
		return false, internal(nil)
	}
	var exists bool
	if err = x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_work.tasks WHERE project_id=$1 AND sprint_id=$2)`, project.String(), sprint.String()).Scan(&exists); err != nil {
		return false, unavailable(err)
	}
	return exists, nil
}

var _ c.TaskReader = (*TaskReader)(nil)
