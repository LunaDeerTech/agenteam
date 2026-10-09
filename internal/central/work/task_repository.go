package work

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"time"

	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

const taskCreate c.TaskCommandName = "work.task.create"
const taskUpdate c.TaskCommandName = "work.task.update"
const taskReorder c.TaskCommandName = "work.task.reorder"
const taskRequestCap = 512 << 10
const taskPlanCap = 4 << 20
const taskGroupCap = 4096
const taskProjectCap = 65536

func taskSQL(err error) error {
	var pg *pgconn.PgError
	if errors.As(err, &pg) {
		return internal(err)
	}
	return unavailable(err)
}
func taskAffected(tag pgconn.CommandTag, err error) error {
	if err != nil {
		return taskSQL(err)
	}
	if tag.RowsAffected() != 1 {
		return internal(nil)
	}
	return nil
}
func taskCounter(v int64) (int64, error) {
	if v <= 0 || v == math.MaxInt64 {
		return 0, field(f.InvalidState, "/version", "COUNTER_EXHAUSTED")
	}
	return v + 1, nil
}
func taskScheduleLock(p c.ProjectID, mode f.LockMode) f.LockRequest {
	key, _ := f.ProjectScheduleLock(p.String())
	return f.LockRequest{Key: key, Mode: mode}
}
func taskLock(id string, mode f.LockMode) f.LockRequest {
	key, _ := f.AggregateLock(f.TaskAggregate, id)
	return f.LockRequest{Key: key, Mode: mode}
}
func taskNormalize(raw []f.LockRequest) ([]f.LockRequest, error) {
	if len(raw) > 512 {
		return nil, fault(f.ResourceBusy)
	}
	return oc.NormalizeLocks(raw)
}

type taskGroup struct {
	Sprint   c.SprintID     `json:"sprint_id"`
	State    c.TaskState    `json:"state"`
	Priority c.TaskPriority `json:"priority"`
}

func groupForTask(t c.Task) taskGroup { return taskGroup{t.SprintID, t.State, t.Priority} }
func taskRankLock(p c.ProjectID, g taskGroup) f.LockRequest {
	key, _ := f.RankGroupLock("work.task:" + p.String() + ":" + g.Sprint.String() + ":" + string(g.State) + ":" + string(g.Priority))
	return f.LockRequest{Key: key, Mode: f.Exclusive}
}

const taskColumns = `id::text,project_id::text,milestone_id::text,sprint_id::text,title,description,type,priority,state,assignee_agent_id::text,plan,manual_rank,version,created_at,updated_at`

func scanTask(row interface{ Scan(...any) error }) (c.Task, error) {
	var t c.Task
	var id, p, m, s string
	var agent *string
	var created, updated time.Time
	if err := row.Scan(&id, &p, &m, &s, &t.Title, &t.Description, &t.Type, &t.Priority, &t.State, &agent, &t.Plan, &t.ManualRank, &t.Version, &created, &updated); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return c.Task{}, fault(f.TaskNotFound)
		}
		return c.Task{}, taskSQL(err)
	}
	var err error
	if t.ID, err = f.ParseID[c.Task](id); err != nil {
		return c.Task{}, internal(err)
	}
	if t.ProjectID, err = typedID[i.Project](p); err != nil {
		return c.Task{}, err
	}
	if t.MilestoneID, err = typedID[c.Milestone](m); err != nil {
		return c.Task{}, err
	}
	if t.SprintID, err = f.ParseID[pc.Sprint](s); err != nil {
		return c.Task{}, internal(err)
	}
	if agent != nil {
		v, e := f.ParseID[i.Agent](*agent)
		if e != nil {
			return c.Task{}, internal(e)
		}
		t.AssigneeAgentID = &v
	}
	if t.CreatedAt, err = f.NewInstant(created); err != nil {
		return c.Task{}, internal(err)
	}
	if t.UpdatedAt, err = f.NewInstant(updated); err != nil {
		return c.Task{}, internal(err)
	}
	if err = t.Validate(); err != nil {
		return c.Task{}, internal(err)
	}
	return t, nil
}
func loadTask(ctx context.Context, x postgres.SQLExecutor, p c.ProjectID, id c.TaskID) (c.Task, error) {
	return scanTask(x.QueryRow(ctx, `SELECT `+taskColumns+` FROM agenteam_work.tasks WHERE project_id=$1 AND id=$2`, p.String(), id.String()))
}
func loadTaskQueryGeneration(ctx context.Context, x postgres.SQLExecutor, p c.ProjectID) (int64, error) {
	var n int64
	err := x.QueryRow(ctx, `SELECT query_generation FROM agenteam_work.task_query_generations WHERE project_id=$1`, p.String()).Scan(&n)
	if errors.Is(err, pgx.ErrNoRows) {
		var exists bool
		if e := x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_work.tasks WHERE project_id=$1)`, p.String()).Scan(&exists); e != nil {
			return 0, taskSQL(e)
		}
		if exists {
			return 0, internal(nil)
		}
		return 1, nil
	}
	if err != nil {
		return 0, taskSQL(err)
	}
	if n < 1 {
		return 0, internal(nil)
	}
	return n, nil
}
func loadTaskRanks(ctx context.Context, x postgres.SQLExecutor, p c.ProjectID, g taskGroup) ([]rankItem, int64, error) {
	rows, err := x.Query(ctx, `SELECT id::text,manual_rank FROM agenteam_work.tasks WHERE project_id=$1 AND sprint_id=$2 AND state=$3 AND priority=$4 ORDER BY manual_rank COLLATE "C",id LIMIT 4097`, p.String(), g.Sprint.String(), string(g.State), string(g.Priority))
	if err != nil {
		return nil, 0, taskSQL(err)
	}
	out := []rankItem{}
	for rows.Next() {
		var v rankItem
		if err = rows.Scan(&v.ID, &v.Rank); err != nil {
			rows.Close()
			return nil, 0, taskSQL(err)
		}
		if _, e := f.ParseID[c.Task](v.ID); e != nil || c.ValidateRank(v.Rank) != nil || len(out) > 0 && out[len(out)-1].Rank >= v.Rank {
			rows.Close()
			return nil, 0, internal(nil)
		}
		out = append(out, v)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, 0, taskSQL(err)
	}
	if len(out) > taskGroupCap {
		return nil, 0, internal(nil)
	}
	var n int64
	err = x.QueryRow(ctx, `SELECT order_generation FROM agenteam_work.task_order_groups WHERE project_id=$1 AND sprint_id=$2 AND state=$3 AND priority=$4`, p.String(), g.Sprint.String(), string(g.State), string(g.Priority)).Scan(&n)
	if errors.Is(err, pgx.ErrNoRows) {
		if len(out) > 0 {
			return nil, 0, internal(nil)
		}
		return out, 1, nil
	}
	if err != nil {
		return nil, 0, taskSQL(err)
	}
	if n <= 0 {
		return nil, 0, internal(nil)
	}
	return out, n, nil
}

type taskInput struct {
	Command  c.TaskCommandName   `json:"command"`
	Project  c.ProjectID         `json:"project_id"`
	Target   c.TaskID            `json:"target_id"`
	User     i.UserID            `json:"actor_user_id"`
	Expected *f.Version          `json:"expected_version"`
	Create   *c.TaskCreate       `json:"create"`
	Update   *c.TaskFieldsUpdate `json:"update"`
	Reorder  *c.TaskReorder      `json:"reorder"`
}

var taskInputFields = []string{"command", "project_id", "target_id", "actor_user_id", "expected_version", "create", "update", "reorder"}

func taskIdentity(p c.ProjectID, name c.TaskCommandName, key f.IdempotencyKey) (f.CommandIdentity, error) {
	return f.NewCommandIdentity("project", []string{p.String()}, string(name), key)
}
func (in taskInput) semantic(a i.Actor, key f.IdempotencyKey) (f.Digest, error) {
	if in.User.String() != a.Details().UserID || in.Target.Validate() != nil || in.Command.Validate() != nil {
		return "", fault(f.InvalidArgument)
	}
	rid, e := f.ParseID[f.Request](in.Target.String())
	if e != nil {
		return "", fault(f.InvalidArgument)
	}
	m := f.CommandMeta{RequestID: rid, IdempotencyKey: key, ExpectedVersion: in.Expected}
	switch in.Command {
	case taskCreate:
		if in.Create != nil && in.Update == nil && in.Reorder == nil && in.Create.TaskID == in.Target {
			return c.TaskCreateDigest(a, m, in.Project, *in.Create)
		}
	case taskUpdate:
		if in.Create == nil && in.Update != nil && in.Reorder == nil {
			return c.TaskUpdateDigest(a, m, in.Project, in.Target, *in.Update)
		}
	case taskReorder:
		if in.Create == nil && in.Update == nil && in.Reorder != nil {
			return c.TaskReorderDigest(a, m, in.Project, in.Target, *in.Reorder)
		}
	}
	return "", fault(f.InvalidArgument)
}
func (in taskInput) locks(key f.IdempotencyKey, source taskGroup) ([]f.LockRequest, error) {
	id, err := taskIdentity(in.Project, in.Command, key)
	if err != nil {
		return nil, err
	}
	locks := []f.LockRequest{commandLock(id), userLock(in.User.String(), f.Exclusive), projectLock(in.Project, f.Shared), taskScheduleLock(in.Project, f.Exclusive), taskRankLock(in.Project, source), sprintLock(source.Sprint.String(), f.Shared), taskLock(in.Target.String(), f.Exclusive)}
	if in.Update != nil && in.Update.Priority != nil && *in.Update.Priority != source.Priority {
		to := source
		to.Priority = *in.Update.Priority
		locks = append(locks, taskRankLock(in.Project, to))
	}
	return taskNormalize(locks)
}

type taskPlacement struct {
	Milestone c.MilestoneID `json:"milestone_id"`
	Sprint    c.SprintID    `json:"sprint_id"`
	State     c.SprintState `json:"state"`
}
type taskGroupPlan struct {
	Group      taskGroup  `json:"group"`
	Before     []rankItem `json:"before"`
	After      []rankItem `json:"after"`
	Generation int64      `json:"generation"`
}
type taskPlan struct {
	Before          *c.Task         `json:"before"`
	After           c.TaskMutation  `json:"after"`
	Placement       taskPlacement   `json:"placement"`
	Groups          []taskGroupPlan `json:"groups"`
	QueryGeneration int64           `json:"query_generation"`
	TaskEvent       json.RawMessage `json:"task_event"`
	Header          event.Header    `json:"header"`
	Payload         json.RawMessage `json:"payload"`
}

var taskPlanFields = []string{"before", "after", "placement", "groups", "query_generation", "task_event", "header", "payload"}

type taskRecord struct {
	ID          c.TaskCommandID
	Project     c.ProjectID
	User        i.UserID
	Command     c.TaskCommandName
	Key         f.IdempotencyKey
	Semantic    f.Digest
	Input       taskInput
	Revision    f.Version
	State       string
	Plan        *taskPlan
	TaskEventID *c.TaskEventID
	EventID     *event.EventID
	Receipt     *c.TaskMutation
	Created     f.Instant
	Committed   *f.Instant
}

func (taskInput) Format(w fmt.State, _ rune)  { _, _ = io.WriteString(w, "work_task") }
func (taskInput) LogValue() slog.Value        { return slog.StringValue("work_task") }
func (taskPlan) Format(w fmt.State, _ rune)   { _, _ = io.WriteString(w, "work_task") }
func (taskPlan) LogValue() slog.Value         { return slog.StringValue("work_task") }
func (taskRecord) Format(w fmt.State, _ rune) { _, _ = io.WriteString(w, "work_task") }
func (taskRecord) LogValue() slog.Value       { return slog.StringValue("work_task") }

const taskCommandColumns = `id::text,project_id::text,actor_user_id::text,command_name,idempotency_key,semantic_digest,request,plan_revision,state,plan,task_event_id::text,event_id::text,receipt,created_at,committed_at`

func scanTaskCommand(row interface{ Scan(...any) error }) (*taskRecord, error) {
	var v taskRecord
	var id, p, u string
	var req, plan, receipt []byte
	var te, ev *string
	var at time.Time
	var done *time.Time
	if err := row.Scan(&id, &p, &u, &v.Command, &v.Key, &v.Semantic, &req, &v.Revision, &v.State, &plan, &te, &ev, &receipt, &at, &done); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, taskSQL(err)
	}
	var err error
	if v.ID, err = f.ParseID[c.TaskCommand](id); err != nil {
		return nil, internal(err)
	}
	if v.Project, err = typedID[i.Project](p); err != nil {
		return nil, err
	}
	if v.User, err = typedID[i.User](u); err != nil {
		return nil, err
	}
	if v.Created, err = f.NewInstant(at); err != nil {
		return nil, internal(err)
	}
	if done != nil {
		n, e := f.NewInstant(*done)
		if e != nil {
			return nil, internal(e)
		}
		v.Committed = &n
	}
	if v.Input, err = decodePrivate[taskInput](req, taskRequestCap, taskInputFields); err != nil {
		return nil, err
	}
	if plan != nil {
		p, e := decodePrivate[taskPlan](plan, taskPlanCap, taskPlanFields)
		if e != nil {
			return nil, e
		}
		var shape map[string]json.RawMessage
		if json.Unmarshal(plan, &shape) != nil {
			return nil, internal(nil)
		}
		if p.Header, e = event.DecodeHeader(shape["header"]); e != nil {
			return nil, internal(e)
		}
		v.Plan = &p
	}
	if te != nil {
		id, e := f.ParseID[c.TaskEvent](*te)
		if e != nil {
			return nil, internal(e)
		}
		v.TaskEventID = &id
	}
	if ev != nil {
		id, e := f.ParseID[event.EventIdentity](*ev)
		if e != nil {
			return nil, internal(e)
		}
		v.EventID = &id
	}
	if receipt != nil {
		if len(receipt) > taskRequestCap {
			return nil, internal(nil)
		}
		var r c.TaskMutation
		if err = json.Unmarshal(receipt, &r); err != nil {
			return nil, internal(err)
		}
		v.Receipt = &r
	}
	if v.Command.Validate() != nil || v.Key.Validate() != nil || v.Semantic.Validate() != nil || v.Revision.Validate() != nil || v.Input.Command != v.Command || v.Input.Project != v.Project || v.Input.User != v.User || (v.Plan == nil) != (v.EventID == nil) || (v.Plan == nil) != (v.TaskEventID == nil) {
		return nil, internal(nil)
	}
	switch v.State {
	case "planned":
		if v.Plan == nil || v.Receipt != nil || v.Committed != nil {
			return nil, internal(nil)
		}
	case "completed":
		if v.Receipt == nil || v.Committed == nil || v.Committed.Time().Before(v.Created.Time()) || v.Receipt.Changed != (v.Plan != nil) {
			return nil, internal(nil)
		}
	default:
		return nil, internal(nil)
	}
	return &v, nil
}
func loadTaskCommand(ctx context.Context, x postgres.SQLExecutor, id f.CommandIdentity) (*taskRecord, error) {
	return scanTaskCommand(x.QueryRow(ctx, `SELECT `+taskCommandColumns+` FROM agenteam_work.task_commands WHERE project_id=$1 AND command_name=$2 AND idempotency_key=$3`, id.OwnerIDs()[0], id.Command(), string(id.Key())))
}
func loadTaskEventCommand(ctx context.Context, x postgres.SQLExecutor, id event.EventID) (*taskRecord, error) {
	return scanTaskCommand(x.QueryRow(ctx, `SELECT `+taskCommandColumns+` FROM agenteam_work.task_commands WHERE event_id=$1`, id.String()))
}
func storeTaskPlan(ctx context.Context, x postgres.SQLExecutor, r *taskRecord, insert bool) error {
	input, err := canonical(r.Input)
	if err != nil || len(input) > taskRequestCap {
		return internal(err)
	}
	plan, err := canonical(r.Plan)
	if err != nil || len(plan) > taskPlanCap {
		return internal(err)
	}
	if insert {
		return taskAffected(x.Exec(ctx, `INSERT INTO agenteam_work.task_commands(id,project_id,actor_user_id,command_name,idempotency_key,semantic_digest,request,plan_revision,state,plan,task_event_id,event_id,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,'planned',$9,$10,$11,$12)`, r.ID.String(), r.Project.String(), r.User.String(), string(r.Command), string(r.Key), string(r.Semantic), input, int64(r.Revision), plan, r.TaskEventID.String(), r.EventID.String(), r.Created.Time()))
	}
	return taskAffected(x.Exec(ctx, `UPDATE agenteam_work.task_commands SET plan_revision=$2,plan=$3,task_event_id=$4,event_id=$5 WHERE id=$1 AND state='planned' AND plan_revision=$6`, r.ID.String(), int64(r.Revision), plan, r.TaskEventID.String(), r.EventID.String(), int64(r.Revision-1)))
}
func completeTaskCommand(ctx context.Context, x postgres.SQLExecutor, r *taskRecord, out c.TaskMutation, insert bool) error {
	receipt, err := canonical(out)
	if err != nil || len(receipt) > taskRequestCap {
		return internal(err)
	}
	now, err := dbNow(ctx, x)
	if err != nil {
		return err
	}
	if insert {
		input, e := canonical(r.Input)
		if e != nil || len(input) > taskRequestCap {
			return internal(e)
		}
		return taskAffected(x.Exec(ctx, `INSERT INTO agenteam_work.task_commands(id,project_id,actor_user_id,command_name,idempotency_key,semantic_digest,request,plan_revision,state,receipt,created_at,committed_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,'completed',$9,$10,$11)`, r.ID.String(), r.Project.String(), r.User.String(), string(r.Command), string(r.Key), string(r.Semantic), input, int64(r.Revision), receipt, r.Created.Time(), now.Time()))
	}
	if out.Changed {
		return taskAffected(x.Exec(ctx, `UPDATE agenteam_work.task_commands SET state='completed',receipt=$2,committed_at=$3 WHERE id=$1 AND state='planned' AND plan_revision=$4`, r.ID.String(), receipt, now.Time(), int64(r.Revision)))
	}
	return taskAffected(x.Exec(ctx, `UPDATE agenteam_work.task_commands SET state='completed',receipt=$2,committed_at=$3,plan=NULL,task_event_id=NULL,event_id=NULL WHERE id=$1 AND state='planned' AND plan_revision=$4`, r.ID.String(), receipt, now.Time(), int64(r.Revision)))
}

func applyTaskPlan(ctx context.Context, x postgres.SQLExecutor, r *taskRecord) error {
	p := r.Plan
	t := p.After.Task
	in := r.Input
	if in.Create != nil {
		if err := taskAffected(x.Exec(ctx, `INSERT INTO agenteam_work.tasks(id,project_id,milestone_id,sprint_id,title,description,type,priority,state,assignee_agent_id,plan,manual_rank,version,created_at,updated_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,NULL,$10,$11,$12,$13,$14)`, t.ID.String(), t.ProjectID.String(), t.MilestoneID.String(), t.SprintID.String(), t.Title, t.Description, string(t.Type), string(t.Priority), string(t.State), t.Plan, t.ManualRank, int64(t.Version), t.CreatedAt.Time(), t.UpdatedAt.Time())); err != nil {
			return err
		}
	} else {
		query := `UPDATE agenteam_work.tasks SET version=$4,updated_at=$5`
		args := []any{t.ProjectID.String(), t.ID.String(), int64(*in.Expected), int64(t.Version), t.UpdatedAt.Time()}
		add := func(column string, v any) { args = append(args, v); query += fmt.Sprintf(",%s=$%d", column, len(args)) }
		if in.Update != nil {
			u := in.Update
			if u.Title != nil {
				add("title", t.Title)
			}
			if u.Description != nil {
				add("description", t.Description)
			}
			if u.Type != nil {
				add("type", string(t.Type))
			}
			if u.Priority != nil {
				add("priority", string(t.Priority))
			}
			if u.Plan != nil {
				add("plan", t.Plan)
			}
		}
		if in.Reorder != nil || in.Update != nil && in.Update.Priority != nil && *in.Update.Priority != p.Before.Priority {
			add("manual_rank", t.ManualRank)
		}
		query += ` WHERE project_id=$1 AND id=$2 AND version=$3`
		if err := taskAffected(x.Exec(ctx, query, args...)); err != nil {
			return err
		}
	}
	for _, g := range p.Groups {
		before := map[string]string{}
		for _, v := range g.Before {
			before[v.ID] = v.Rank
		}
		for _, v := range g.After {
			if v.ID == in.Target.String() || before[v.ID] == v.Rank {
				continue
			}
			if _, exists := before[v.ID]; !exists {
				return internal(nil)
			}
			if err := taskAffected(x.Exec(ctx, `UPDATE agenteam_work.tasks SET manual_rank=$6 WHERE project_id=$1 AND sprint_id=$2 AND state=$3 AND priority=$4 AND id=$5`, r.Project.String(), g.Group.Sprint.String(), string(g.Group.State), string(g.Group.Priority), v.ID, v.Rank)); err != nil {
				return err
			}
		}
		next, err := taskCounter(g.Generation)
		if err != nil {
			return err
		}
		if err = taskAffected(x.Exec(ctx, `INSERT INTO agenteam_work.task_order_groups(project_id,milestone_id,sprint_id,state,priority,order_generation) VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT(project_id,sprint_id,state,priority) DO UPDATE SET order_generation=EXCLUDED.order_generation WHERE agenteam_work.task_order_groups.order_generation=$7 AND agenteam_work.task_order_groups.milestone_id=EXCLUDED.milestone_id`, r.Project.String(), t.MilestoneID.String(), g.Group.Sprint.String(), string(g.Group.State), string(g.Group.Priority), next, g.Generation)); err != nil {
			return err
		}
	}
	next, err := taskCounter(p.QueryGeneration)
	if err != nil {
		return err
	}
	if err = taskAffected(x.Exec(ctx, `INSERT INTO agenteam_work.task_query_generations(project_id,query_generation) VALUES($1,$2) ON CONFLICT(project_id) DO UPDATE SET query_generation=EXCLUDED.query_generation WHERE agenteam_work.task_query_generations.query_generation=$3`, r.Project.String(), next, p.QueryGeneration)); err != nil {
		return err
	}
	var history c.TaskEvent
	if err = json.Unmarshal(p.TaskEvent, &history); err != nil {
		return internal(err)
	}
	actor, err := canonical(history.Actor)
	if err != nil {
		return err
	}
	return taskAffected(x.Exec(ctx, `INSERT INTO agenteam_work.task_events(id,project_id,task_id,task_version,type,actor,operation_id,correlation_id,payload,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, history.ID.String(), history.ProjectID.String(), history.TaskID.String(), int64(history.TaskVersion), string(history.Type), actor, history.OperationID.String(), history.CorrelationID.String(), []byte(history.Payload), history.CreatedAt.Time()))
}
