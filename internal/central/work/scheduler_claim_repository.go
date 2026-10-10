package work

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"time"

	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	c "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
	"github.com/jackc/pgx/v5"
)

// This immutable Work record is inserted with the canonical mutation, not
// during discovery. The Scheduler inserts its pending row only after checking
// the private applied value. There is deliberately no cross-domain FK cycle.
type schedulerClaimRecord struct {
	Request         c.TaskClaimRequest     `json:"request"`
	Before          c.Task                 `json:"before"`
	After           c.Task                 `json:"after"`
	Groups          []taskGroupPlan        `json:"groups"`
	QueryGeneration int64                  `json:"query_generation"`
	Guard           c.TaskClaimGuard       `json:"guard"`
	History         c.SchedulerTaskEvent   `json:"history"`
	Event           c.SchedulerTaskClaimed `json:"event"`
	Header          event.Header           `json:"header"`
}

func planSchedulerClaim(ctx context.Context, x postgres.SQLExecutor, r c.TaskClaimRequest, before c.Task) (schedulerClaimRecord, error) {
	source := groupForTask(before)
	target := source
	target.State = c.TaskStateInProgress
	sourceRows, sourceGen, err := loadTaskRanks(ctx, x, r.ProjectID, source)
	if err != nil {
		return schedulerClaimRecord{}, err
	}
	targetRows, targetGen, err := loadTaskRanks(ctx, x, r.ProjectID, target)
	if err != nil {
		return schedulerClaimRecord{}, err
	}
	query, err := loadTaskQueryGeneration(ctx, x, r.ProjectID)
	if err != nil {
		return schedulerClaimRecord{}, err
	}
	history, err := f.NewID[c.TaskEvent]()
	if err != nil {
		return schedulerClaimRecord{}, internal(err)
	}
	eventID, err := f.NewID[event.EventIdentity]()
	if err != nil {
		return schedulerClaimRecord{}, internal(err)
	}
	at, _ := f.NewInstant(time.Now())
	return buildSchedulerClaimRecord(r, before, sourceRows, sourceGen, targetRows, targetGen, query, history, eventID, at)
}

func buildSchedulerClaimRecord(r c.TaskClaimRequest, before c.Task, sourceRows []rankItem, sourceGen int64, targetRows []rankItem, targetGen, query int64, historyID c.TaskEventID, eventID event.EventID, at f.Instant) (schedulerClaimRecord, error) {
	var zero schedulerClaimRecord
	if r.Validate() != nil || validateClaimTask(before, r) != nil || historyID.Validate() != nil || eventID.Validate() != nil || at.Validate() != nil || validateTaskRanks(sourceRows) != nil || validateTaskRanks(targetRows) != nil {
		return zero, internal(nil)
	}
	if len(targetRows) >= taskGroupCap {
		return zero, fault(f.ResourceBusy)
	}
	sourceNext, err := transitionNext(sourceGen)
	if err != nil {
		return zero, err
	}
	targetNext, err := transitionNext(targetGen)
	if err != nil {
		return zero, err
	}
	version, err := transitionNext(int64(before.Version))
	if err != nil {
		return zero, err
	}
	if _, err = transitionNext(query); err != nil {
		return zero, err
	}
	remaining := make([]rankItem, 0, len(sourceRows))
	found := -1
	for n, item := range sourceRows {
		if item.ID == r.TaskID.String() {
			found = n
			if item.Rank != before.ManualRank {
				return zero, internal(nil)
			}
		} else {
			remaining = append(remaining, item)
		}
	}
	if found < 0 {
		return zero, internal(nil)
	}
	for _, item := range targetRows {
		if item.ID == r.TaskID.String() {
			return zero, internal(nil)
		}
	}
	var previous, next string
	if found > 0 {
		previous = sourceRows[found-1].ID
	}
	if found+1 < len(sourceRows) {
		next = sourceRows[found+1].ID
	}
	source := groupForTask(before)
	target := source
	target.State = c.TaskStateInProgress
	sp, err := transitionPosition(source, sourceNext, previous, next)
	if err != nil {
		return zero, err
	}
	ranks, err := rankFor(targetRows, r.TaskID.String(), "", true)
	if err != nil {
		return zero, err
	}
	tp, err := transitionPosition(target, targetNext, ranks.Previous, ranks.Next)
	if err != nil {
		return zero, err
	}
	if at.Time().Before(before.UpdatedAt.Time()) {
		at = before.UpdatedAt
	}
	after := before.Clone()
	after.State = c.TaskStateInProgress
	after.Version = f.Version(version)
	after.ManualRank = ranks.Rank
	after.UpdatedAt = at
	claimID, err := f.ParseID[c.SchedulerClaim](r.DispatchID)
	if err != nil {
		return zero, internal(err)
	}
	actor := c.SchedulerTaskActor{CauseID: r.DispatchID}
	history := c.SchedulerTaskEvent{ID: historyID, ProjectID: r.ProjectID, TaskID: r.TaskID, TaskVersion: after.Version, Type: c.TaskTransitionStateChanged, Actor: actor, OperationID: claimID, CorrelationID: claimID, Payload: c.SchedulerStateChanged{FromState: c.TaskStateTodo, ToState: c.TaskStateInProgress, ReasonCode: "scheduler_claim"}, CreatedAt: at}
	header, err := taskHeader(eventID, after, at)
	if err != nil {
		return zero, err
	}
	header.EventType = c.TaskTransitionedName
	header.SchemaVersion = c.SchedulerClaimSchemaVersion
	payload := c.SchedulerTaskClaimed{ClaimID: claimID, Actor: actor, TaskEventID: historyID, MilestoneID: before.MilestoneID, SprintID: before.SprintID, AgentID: r.AgentID, SourcePosition: sp, TargetPosition: tp}
	guard := c.TaskClaimGuard{TaskID: r.TaskID, ClaimedVersion: after.Version, SourceState: before.State, SourceAssigneeID: r.AgentID, SourcePriority: before.Priority, SourceSprintID: before.SprintID, SourceOrderGeneration: sourceNext, PredecessorID: sp.PreviousID, SuccessorID: sp.NextID}
	if after.Validate() != nil || history.Validate() != nil || payload.Validate() != nil {
		return zero, internal(nil)
	}
	return schedulerClaimRecord{r.Clone(), before.Clone(), after, []taskGroupPlan{{source, slices.Clone(sourceRows), remaining, sourceGen}, {target, slices.Clone(targetRows), ranks.Items, targetGen}}, query, guard, history, payload, header}, nil
}

func validateSchedulerClaimRecord(r *schedulerClaimRecord) error {
	if r == nil || len(r.Groups) != 2 {
		return internal(nil)
	}
	expected, err := buildSchedulerClaimRecord(r.Request, r.Before, r.Groups[0].Before, r.Groups[0].Generation, r.Groups[1].Before, r.Groups[1].Generation, r.QueryGeneration, r.History.ID, r.Header.EventID, r.Header.OccurredAt)
	if err != nil || !sameValue(expected, *r) {
		return internal(nil)
	}
	return nil
}
func (r *schedulerClaimRecord) UnmarshalJSON(raw []byte) error {
	fields, err := taskPrivateObject(raw, taskPlanCap, []string{"request", "before", "after", "groups", "query_generation", "guard", "history", "event", "header"}, nil)
	if r == nil || err != nil {
		return internal(nil)
	}
	if _, err = taskPrivateObject(fields["request"], taskRequestCap, []string{"ProjectID", "TaskID", "AgentID", "DispatchID", "ExpectedTaskVersion", "CurrentSprintID", "Purpose", "RequestID"}, nil); err != nil {
		return internal(nil)
	}
	type wire schedulerClaimRecord
	var next wire
	if err = json.Unmarshal(raw, &next); err != nil {
		return internal(err)
	}
	v := schedulerClaimRecord(next)
	if err = validateSchedulerClaimRecord(&v); err != nil {
		return err
	}
	original, e := cursorPayload(raw)
	encoded, e2 := canonical(v)
	if e != nil || e2 != nil || !slices.Equal(original, encoded) {
		return internal(nil)
	}
	*r = v
	return nil
}

func applySchedulerClaim(ctx context.Context, x postgres.SQLExecutor, r *schedulerClaimRecord) error {
	if err := validateSchedulerClaimRecord(r); err != nil {
		return err
	}
	raw, err := canonical(r)
	if err != nil || len(raw) > taskPlanCap {
		return internal(err)
	}
	if err = taskAffected(x.Exec(ctx, `INSERT INTO agenteam_work.task_scheduler_claims(id,project_id,task_id,agent_id,request_id,expected_version,claimed_version,task_event_id,event_id,record,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`, r.Request.DispatchID, r.Request.ProjectID.String(), r.Request.TaskID.String(), r.Request.AgentID.String(), r.Request.RequestID.String(), int64(r.Before.Version), int64(r.After.Version), r.History.ID.String(), r.Header.EventID.String(), raw, r.After.UpdatedAt.Time())); err != nil {
		return err
	}
	t := r.After
	if err = taskAffected(x.Exec(ctx, `UPDATE agenteam_work.tasks SET state='in_progress',manual_rank=$4,version=$5,updated_at=$6 WHERE project_id=$1 AND id=$2 AND version=$3 AND state='todo' AND assignee_agent_id=$7`, t.ProjectID.String(), t.ID.String(), int64(r.Before.Version), t.ManualRank, int64(t.Version), t.UpdatedAt.Time(), r.Request.AgentID.String())); err != nil {
		return err
	}
	for _, g := range r.Groups {
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
			if err = taskAffected(x.Exec(ctx, `UPDATE agenteam_work.tasks SET manual_rank=$6 WHERE project_id=$1 AND sprint_id=$2 AND state=$3 AND priority=$4 AND id=$5`, t.ProjectID.String(), g.Group.Sprint.String(), string(g.Group.State), string(g.Group.Priority), v.ID, v.Rank)); err != nil {
				return err
			}
		}
		next, err := transitionNext(g.Generation)
		if err != nil {
			return err
		}
		if err = taskAffected(x.Exec(ctx, `INSERT INTO agenteam_work.task_order_groups(project_id,milestone_id,sprint_id,state,priority,order_generation) VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT(project_id,sprint_id,state,priority) DO UPDATE SET order_generation=EXCLUDED.order_generation WHERE agenteam_work.task_order_groups.order_generation=$7`, t.ProjectID.String(), t.MilestoneID.String(), g.Group.Sprint.String(), string(g.Group.State), string(g.Group.Priority), next, g.Generation)); err != nil {
			return err
		}
	}
	next, err := transitionNext(r.QueryGeneration)
	if err != nil {
		return err
	}
	if err = taskAffected(x.Exec(ctx, `UPDATE agenteam_work.task_query_generations SET query_generation=$2 WHERE project_id=$1 AND query_generation=$3`, t.ProjectID.String(), next, r.QueryGeneration)); err != nil {
		return err
	}
	h := r.History
	actor, err := json.Marshal(h.Actor)
	if err != nil {
		return internal(err)
	}
	payload, err := json.Marshal(h.Payload)
	if err != nil {
		return internal(err)
	}
	return taskAffected(x.Exec(ctx, `INSERT INTO agenteam_work.task_events(id,project_id,task_id,task_version,type,actor,claim_operation_id,correlation_id,payload,created_at) VALUES($1,$2,$3,$4,'state_changed',$5,$6,$6,$7,$8)`, h.ID.String(), h.ProjectID.String(), h.TaskID.String(), int64(h.TaskVersion), actor, h.OperationID.String(), payload, h.CreatedAt.Time()))
}

func loadSchedulerClaim(ctx context.Context, x postgres.SQLExecutor, project c.ProjectID, id string) (*schedulerClaimRecord, error) {
	var raw []byte
	var task, agent, request, history, eventID string
	var before, after int64
	var at time.Time
	err := x.QueryRow(ctx, `SELECT task_id::text,agent_id::text,request_id::text,expected_version,claimed_version,task_event_id::text,event_id::text,record,created_at FROM agenteam_work.task_scheduler_claims WHERE project_id=$1 AND id=$2`, project.String(), id).Scan(&task, &agent, &request, &before, &after, &history, &eventID, &raw, &at)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, taskSQL(err)
	}
	var r schedulerClaimRecord
	if json.Unmarshal(raw, &r) != nil || r.Request.ProjectID != project || r.Request.DispatchID != id || r.Request.TaskID.String() != task || r.Request.AgentID.String() != agent || r.Request.RequestID.String() != request || int64(r.Before.Version) != before || int64(r.After.Version) != after || r.History.ID.String() != history || r.Header.EventID.String() != eventID || !r.After.UpdatedAt.Time().Equal(at) {
		return nil, internal(nil)
	}
	return &r, nil
}

func verifySchedulerClaimPostimage(ctx context.Context, x postgres.SQLExecutor, r *schedulerClaimRecord) error {
	actual, err := loadSchedulerClaim(ctx, x, r.Request.ProjectID, r.Request.DispatchID)
	if err != nil {
		return err
	}
	if actual == nil || !sameValue(*actual, *r) {
		return fault(f.Forbidden)
	}
	t, err := loadTask(ctx, x, r.Request.ProjectID, r.Request.TaskID)
	if err != nil {
		return err
	}
	if !sameValue(t, r.After) {
		return fault(f.Forbidden)
	}
	query, err := loadTaskQueryGeneration(ctx, x, r.Request.ProjectID)
	if err != nil {
		return err
	}
	if query != r.QueryGeneration+1 {
		return fault(f.Forbidden)
	}
	for _, g := range r.Groups {
		rows, gen, e := loadTaskRanks(ctx, x, r.Request.ProjectID, g.Group)
		if e != nil {
			return e
		}
		if gen != g.Generation+1 || !sameValue(rows, g.After) {
			return fault(f.Forbidden)
		}
	}
	rows, err := x.Query(ctx, `SELECT id::text,project_id::text,task_id::text,task_version,type,actor,operation_id::text,blocker_operation_id::text,transition_operation_id::text,claim_operation_id::text,correlation_id::text,payload,created_at FROM agenteam_work.task_events WHERE project_id=$1 AND claim_operation_id=$2 ORDER BY id`, r.Request.ProjectID.String(), r.Request.DispatchID)
	if err != nil {
		return taskSQL(err)
	}
	if nilPort(rows) {
		return internal(nil)
	}
	defer rows.Close()
	if !rows.Next() {
		if err = rows.Err(); err != nil {
			return taskSQL(err)
		}
		return fault(f.Forbidden)
	}
	raw, err := scanTaskTriggerEvent(rows)
	if err != nil {
		return err
	}
	var history c.SchedulerTaskEvent
	if json.Unmarshal(raw, &history) != nil || !sameValue(history, r.History) {
		return fault(f.Forbidden)
	}
	if rows.Next() {
		return fault(f.Forbidden)
	}
	if err = rows.Err(); err != nil {
		return taskSQL(err)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return taskSQL(err)
	}
	return ctx.Err()
}
