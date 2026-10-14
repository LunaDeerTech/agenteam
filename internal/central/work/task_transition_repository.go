package work

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"slices"
	"time"

	agentc "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	c "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
	"github.com/jackc/pgx/v5"
)

type transitionInput struct {
	Cancellation *transitionCancellation `json:"execution_cancellation,omitempty"`
	Project      c.ProjectID             `json:"project_id"`
	Task         c.TaskID                `json:"task_id"`
	User         i.UserID                `json:"actor_user_id,omitempty"`
	Agent        i.AgentID               `json:"actor_agent_id,omitempty"`
	Execution    i.ExecutionID           `json:"actor_execution_id,omitempty"`
	Expected     f.Version               `json:"expected_version"`
	Request      c.TaskTransfer          `json:"request"`
}

func (in transitionInput) semantic(actor i.Actor, key f.IdempotencyKey) (f.Digest, error) {
	if !in.matches(actor) {
		return "", fault(f.Forbidden)
	}
	return in.transitionSemantic(actor, key)
}
func (in transitionInput) locks(key f.IdempotencyKey, before c.Task) ([]f.LockRequest, error) {
	id, err := c.TaskTransitionIdentity(in.Project, key)
	if err != nil {
		return nil, err
	}
	source := groupForTask(before)
	target := source
	target.State = in.Request.TargetState
	locks := []f.LockRequest{commandLock(id), projectLock(in.Project, f.Shared), taskScheduleLock(in.Project, f.Exclusive), taskRankLock(in.Project, source), taskRankLock(in.Project, target), sprintLock(before.SprintID.String(), f.Shared), taskLock(in.Task.String(), f.Exclusive)}
	if in.User.Validate() == nil {
		locks = append(locks, userLock(in.User.String(), f.Exclusive))
	} else {
		a, _ := f.AgentLock(in.Agent.String())
		e, _ := f.AggregateLock(f.ExecutionAggregate, in.Execution.String())
		locks = append(locks, f.LockRequest{Key: a, Mode: f.Shared}, f.LockRequest{Key: e, Mode: f.Shared})
	}
	for _, id := range []*i.AgentID{before.AssigneeAgentID, in.Request.AssigneeAgentID} {
		if id != nil {
			key, err := f.AgentLock(id.String())
			if err != nil {
				return nil, err
			}
			locks = append(locks, f.LockRequest{Key: key, Mode: f.Shared})
		}
	}
	return taskNormalize(locks)
}

type transitionPlan struct {
	CancellationBlocker *c.TaskBlocker                `json:"cancellation_blocker,omitempty"`
	CancelExecutions    []ec.CancellationScope        `json:"cancel_executions,omitempty"`
	CancelDispatches    []string                      `json:"cancel_dispatches,omitempty"`
	Resolutions         []transitionBlockerResolution `json:"blocker_resolutions,omitempty"`
	Before              c.Task                        `json:"before"`
	After               c.TaskTransitionMutation      `json:"after"`
	Placement           taskPlacement                 `json:"placement"`
	Agent               *agentc.AgentRef              `json:"agent"`
	Groups              []taskGroupPlan               `json:"groups"`
	QueryGeneration     int64                         `json:"query_generation"`
	History             []c.TaskTransitionEvent       `json:"history"`
	Source              c.TaskTransitionPosition      `json:"source"`
	Target              c.TaskTransitionPosition      `json:"target"`
	Header              event.Header                  `json:"header"`
	Payload             json.RawMessage               `json:"payload"`
}
type transitionRecord struct {
	ID        c.TaskTransitionCommandID
	Key       f.IdempotencyKey
	Semantic  f.Digest
	Input     transitionInput
	Revision  f.Version
	State     string
	Plan      transitionPlan
	Receipt   *c.TaskTransitionMutation
	Created   f.Instant
	Committed *f.Instant
}

func (transitionInput) Format(w fmt.State, _ rune)  { _, _ = io.WriteString(w, "work_task_transition") }
func (transitionInput) LogValue() slog.Value        { return slog.StringValue("work_task_transition") }
func (transitionPlan) Format(w fmt.State, _ rune)   { _, _ = io.WriteString(w, "work_task_transition") }
func (transitionPlan) LogValue() slog.Value         { return slog.StringValue("work_task_transition") }
func (transitionRecord) Format(w fmt.State, _ rune) { _, _ = io.WriteString(w, "work_task_transition") }
func (transitionRecord) LogValue() slog.Value       { return slog.StringValue("work_task_transition") }

func (v *transitionInput) UnmarshalJSON(raw []byte) error {
	var shape map[string]json.RawMessage
	if json.Unmarshal(raw, &shape) != nil {
		return internal(nil)
	}
	fields := []string{"project_id", "task_id", "expected_version", "request"}
	for _, key := range []string{"actor_user_id", "actor_agent_id", "actor_execution_id", "execution_cancellation"} {
		if _, ok := shape[key]; ok {
			fields = append(fields, key)
		}
	}
	if _, err := taskPrivateObject(raw, taskRequestCap, fields, nil); err != nil {
		return err
	}
	type wire transitionInput
	var next wire
	if err := json.Unmarshal(raw, &next); err != nil {
		return internal(err)
	}
	if next.Project.Validate() != nil || next.Task.Validate() != nil || transitionInput(next).historyActor().Validate() != nil || next.Expected.Validate() != nil || next.Request.Validate() != nil {
		return internal(nil)
	}
	if next.Cancellation != nil && next.Cancellation.validate(transitionInput(next)) != nil {
		return internal(nil)
	}
	*v = transitionInput(next)
	return nil
}
func (v *transitionPlan) UnmarshalJSON(raw []byte) error {
	var shape map[string]json.RawMessage
	if len(raw) > taskPlanCap || json.Unmarshal(raw, &shape) != nil {
		return internal(nil)
	}
	fields := []string{"before", "after", "placement", "agent", "groups", "query_generation", "history", "source", "target", "header", "payload"}
	for _, key := range []string{"cancellation_blocker", "cancel_executions", "cancel_dispatches"} {
		if _, ok := shape[key]; ok {
			fields = append(fields, key)
		}
	}
	if _, ok := shape["blocker_resolutions"]; ok {
		fields = append(fields, "blocker_resolutions")
	}
	if _, err := taskPrivateObject(raw, taskPlanCap, fields, []string{"agent"}); err != nil {
		return err
	}
	type wire transitionPlan
	var next wire
	if err := json.Unmarshal(raw, &next); err != nil {
		return internal(err)
	}
	if next.Before.Validate() != nil || next.After.Validate() != nil || next.Source.Validate() != nil || next.Target.Validate() != nil || next.Header.Validate() != nil || len(next.Groups) != 2 || len(next.History) < 1 || len(next.History) > 35 || next.QueryGeneration < 1 {
		return internal(nil)
	}
	if next.Agent == nil {
		retainedDone := next.Before.State == c.TaskStateInReview && next.After.Task.State == c.TaskStateDone
		retainedCancel := transitionCancelEdge(next.Before, c.TaskTransfer{TargetState: next.After.Task.State})
		retainedBlock := transitionBlockEdge(next.Before, c.TaskTransfer{TargetState: next.After.Task.State})
		if (!retainedDone && !retainedCancel && !retainedBlock) || !sameValue(next.Before.AssigneeAgentID, next.After.Task.AssigneeAgentID) {
			return internal(nil)
		}
	} else if next.Agent.Validate() != nil {
		return internal(nil)
	}
	if _, ok := shape["blocker_resolutions"]; ok && (len(next.Resolutions) == 0 || len(next.Resolutions) > 16) {
		return internal(nil)
	}
	*v = transitionPlan(next)
	return nil
}

const transitionColumns = `id::text,project_id::text,COALESCE(actor_user_id::text,'00000000-0000-0000-0000-000000000000'),COALESCE(actor_agent_id::text,'00000000-0000-0000-0000-000000000000'),COALESCE(actor_execution_id::text,'00000000-0000-0000-0000-000000000000'),task_id::text,idempotency_key,semantic_digest,expected_version,request,plan_revision,state,plan,task_event_ids,event_id::text,receipt,created_at,committed_at`

func scanTransition(row interface{ Scan(...any) error }) (*transitionRecord, error) {
	var r transitionRecord
	var id, p, u, a, ex, t, ev string
	var expected f.Version
	var request, plan, ids, receipt []byte
	var created time.Time
	var committed *time.Time
	if err := row.Scan(&id, &p, &u, &a, &ex, &t, &r.Key, &r.Semantic, &expected, &request, &r.Revision, &r.State, &plan, &ids, &ev, &receipt, &created, &committed); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, taskSQL(err)
	}
	var err error
	if r.ID, err = f.ParseID[c.TaskTransitionCommand](id); err != nil {
		return nil, internal(err)
	}
	if r.Created, err = f.NewInstant(created); err != nil {
		return nil, internal(err)
	}
	if committed != nil {
		v, e := f.NewInstant(*committed)
		if e != nil {
			return nil, internal(e)
		}
		r.Committed = &v
	}
	if len(request) > taskRequestCap || json.Unmarshal(request, &r.Input) != nil || len(plan) > taskPlanCap || json.Unmarshal(plan, &r.Plan) != nil {
		return nil, internal(nil)
	}
	var history []c.TaskEventID
	if json.Unmarshal(ids, &history) != nil || !slices.Equal(history, r.Plan.After.TaskEventIDs) || r.Plan.After.EventIDs[0].String() != ev {
		return nil, internal(nil)
	}
	if receipt != nil {
		v, e := c.DecodeTaskTransitionMutation(receipt)
		if e != nil {
			return nil, internal(e)
		}
		r.Receipt = &v
	}
	if r.Input.Project.String() != p || r.Input.User.String() != u || r.Input.Agent.String() != a || r.Input.Execution.String() != ex || r.Input.Task.String() != t || r.Input.Expected != expected || r.Key.Validate() != nil || r.Semantic.Validate() != nil || r.Revision.Validate() != nil {
		return nil, internal(nil)
	}
	if r.State == "planned" {
		if r.Receipt != nil || r.Committed != nil {
			return nil, internal(nil)
		}
	} else if r.State != "completed" || r.Receipt == nil || r.Committed == nil || !r.Committed.Time().Equal(r.Plan.After.Task.UpdatedAt.Time()) || !sameValue(*r.Receipt, r.Plan.After) {
		return nil, internal(nil)
	}
	return &r, nil
}
func loadTransition(ctx context.Context, x postgres.SQLExecutor, id f.CommandIdentity) (*transitionRecord, error) {
	return scanTransition(x.QueryRow(ctx, `SELECT `+transitionColumns+` FROM agenteam_work.task_transition_commands WHERE project_id=$1 AND command_name='work.task.transfer' AND idempotency_key=$2`, id.OwnerIDs()[0], string(id.Key())))
}
func loadTransitionEvent(ctx context.Context, x postgres.SQLExecutor, id event.EventID) (*transitionRecord, error) {
	return scanTransition(x.QueryRow(ctx, `SELECT `+transitionColumns+` FROM agenteam_work.task_transition_commands WHERE event_id=$1`, id.String()))
}
func storeTransitionPlan(ctx context.Context, x postgres.SQLExecutor, r *transitionRecord, insert bool) error {
	request, err := canonical(r.Input)
	if err != nil || len(request) > taskRequestCap {
		return internal(err)
	}
	plan, err := canonical(r.Plan)
	if err != nil || len(plan) > taskPlanCap {
		return internal(err)
	}
	ids, err := canonical(r.Plan.After.TaskEventIDs)
	if err != nil {
		return internal(err)
	}
	if insert {
		return taskAffected(x.Exec(ctx, `INSERT INTO agenteam_work.task_transition_commands(id,project_id,actor_user_id,actor_agent_id,actor_execution_id,task_id,command_name,idempotency_key,semantic_digest,expected_version,request,plan_revision,state,plan,task_event_ids,event_id,created_at) VALUES($1,$2,$3,$14,$15,$4,'work.task.transfer',$5,$6,$7,$8,$9,'planned',$10,$11,$12,$13)`, r.ID.String(), r.Input.Project.String(), transitionNullableID(r.Input.User.String()), r.Input.Task.String(), string(r.Key), string(r.Semantic), int64(r.Input.Expected), request, int64(r.Revision), plan, ids, r.Plan.Header.EventID.String(), r.Created.Time(), transitionNullableID(r.Input.Agent.String()), transitionNullableID(r.Input.Execution.String())))
	}
	return taskAffected(x.Exec(ctx, `UPDATE agenteam_work.task_transition_commands SET plan_revision=$2,plan=$3,task_event_ids=$4,event_id=$5 WHERE id=$1 AND state='planned' AND plan_revision=$6`, r.ID.String(), int64(r.Revision), plan, ids, r.Plan.Header.EventID.String(), int64(r.Revision-1)))
}
func completeTransition(ctx context.Context, x postgres.SQLExecutor, r *transitionRecord) error {
	raw, err := canonical(r.Plan.After)
	if err != nil || len(raw) > c.MaxTaskTransitionResultBytes {
		return internal(err)
	}
	return taskAffected(x.Exec(ctx, `UPDATE agenteam_work.task_transition_commands SET state='completed',receipt=$2,committed_at=$3 WHERE id=$1 AND state='planned' AND plan_revision=$4`, r.ID.String(), raw, r.Plan.After.Task.UpdatedAt.Time(), int64(r.Revision)))
}

func applyTransition(ctx context.Context, x postgres.SQLExecutor, r *transitionRecord) error {
	if err := applyCancellationBlocker(ctx, x, r); err != nil {
		return err
	}
	if err := applyTransitionResolutions(ctx, x, r); err != nil {
		return err
	}
	t := r.Plan.After.Task
	var assignee any
	if t.AssigneeAgentID != nil {
		assignee = t.AssigneeAgentID.String()
	} else if t.State != c.TaskStateCancelled || r.Plan.Before.AssigneeAgentID != nil {
		return internal(nil)
	}
	if err := taskAffected(x.Exec(ctx, `UPDATE agenteam_work.tasks SET state=$4,assignee_agent_id=$5,manual_rank=$6,version=$7,updated_at=$8 WHERE project_id=$1 AND id=$2 AND version=$3`, t.ProjectID.String(), t.ID.String(), int64(r.Input.Expected), string(t.State), assignee, t.ManualRank, int64(t.Version), t.UpdatedAt.Time())); err != nil {
		return err
	}
	for _, g := range r.Plan.Groups {
		old := map[string]string{}
		for _, v := range g.Before {
			old[v.ID] = v.Rank
		}
		for _, v := range g.After {
			if v.ID == t.ID.String() || old[v.ID] == v.Rank {
				continue
			}
			if _, ok := old[v.ID]; !ok {
				return internal(nil)
			}
			if err := taskAffected(x.Exec(ctx, `UPDATE agenteam_work.tasks SET manual_rank=$6 WHERE project_id=$1 AND sprint_id=$2 AND state=$3 AND priority=$4 AND id=$5`, t.ProjectID.String(), g.Group.Sprint.String(), string(g.Group.State), string(g.Group.Priority), v.ID, v.Rank)); err != nil {
				return err
			}
		}
		next, err := taskCounter(g.Generation)
		if err != nil {
			return err
		}
		if err = taskAffected(x.Exec(ctx, `INSERT INTO agenteam_work.task_order_groups(project_id,milestone_id,sprint_id,state,priority,order_generation) VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT(project_id,sprint_id,state,priority) DO UPDATE SET order_generation=EXCLUDED.order_generation WHERE agenteam_work.task_order_groups.order_generation=$7`, t.ProjectID.String(), t.MilestoneID.String(), g.Group.Sprint.String(), string(g.Group.State), string(g.Group.Priority), next, g.Generation)); err != nil {
			return err
		}
	}
	next, err := taskCounter(r.Plan.QueryGeneration)
	if err != nil {
		return err
	}
	if err = taskAffected(x.Exec(ctx, `UPDATE agenteam_work.task_query_generations SET query_generation=$2 WHERE project_id=$1 AND query_generation=$3`, t.ProjectID.String(), next, r.Plan.QueryGeneration)); err != nil {
		return err
	}
	for _, h := range r.Plan.History {
		actor, e := json.Marshal(h.Actor)
		if e != nil {
			return internal(e)
		}
		raw, e := json.Marshal(h)
		if e != nil {
			return internal(e)
		}
		var fields map[string]json.RawMessage
		if json.Unmarshal(raw, &fields) != nil {
			return internal(nil)
		}
		if e = taskAffected(x.Exec(ctx, `INSERT INTO agenteam_work.task_events(id,project_id,task_id,task_version,type,actor,transition_operation_id,correlation_id,payload,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$7,$8,$9)`, h.ID.String(), h.ProjectID.String(), h.TaskID.String(), int64(h.TaskVersion), string(h.Type), actor, h.OperationID.String(), []byte(fields["payload"]), h.CreatedAt.Time())); e != nil {
			return e
		}
	}
	return nil
}
