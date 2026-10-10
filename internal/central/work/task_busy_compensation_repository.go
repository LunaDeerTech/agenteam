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

// Both restored and preserved are immutable Work facts. The Scheduler changes
// the exact pending Dispatch to skipped only after checking the private
// applied value, in the same transaction. No cross-domain FK cycle is needed.
type taskBusyRecord struct {
	Request         c.TaskBusyCompensationRequest `json:"request"`
	Claim           schedulerClaimRecord          `json:"claim"`
	Before          c.Task                        `json:"before"`
	After           c.Task                        `json:"after"`
	Sprint          c.Sprint                      `json:"sprint"`
	CurrentSprintID *c.SprintID                   `json:"current_sprint_id"`
	Restored        bool                          `json:"restored"`
	Groups          []taskGroupPlan               `json:"groups"`
	QueryGeneration int64                         `json:"query_generation"`
	History         *c.TaskBusyTaskEvent          `json:"history"`
	Event           *c.TaskBusyCompensated        `json:"event"`
	Header          *event.Header                 `json:"header"`
	CreatedAt       f.Instant                     `json:"created_at"`
}

func busyCanRestore(claim *schedulerClaimRecord, before c.Task, sprint c.Sprint, current *c.SprintID) bool {
	g := claim.Guard
	return before.Version == g.ClaimedVersion && before.State == c.TaskStateInProgress && before.AssigneeAgentID != nil && *before.AssigneeAgentID == g.SourceAssigneeID && before.Priority == g.SourcePriority && before.SprintID == g.SourceSprintID && before.MilestoneID == claim.Before.MilestoneID && current != nil && *current == g.SourceSprintID && sprint.ID == g.SourceSprintID && sprint.State == c.Current
}

// The source group remains protected by the pending claim. A missing anchor
// or changed generation is corruption/staleness, not a license to append at
// an arbitrary position. Numeric ranks are read from the current group only.
func busyRestoreBefore(rows []rankItem, generation int64, g c.TaskClaimGuard) (string, error) {
	if validateTaskRanks(rows) != nil || generation != g.SourceOrderGeneration {
		return "", fault(f.ConfirmationStale)
	}
	previous, next := -1, len(rows)
	for n, row := range rows {
		if row.ID == g.TaskID.String() {
			return "", internal(nil)
		}
		if g.PredecessorID != nil && row.ID == g.PredecessorID.String() {
			previous = n
		}
		if g.SuccessorID != nil && row.ID == g.SuccessorID.String() {
			next = n
		}
	}
	if (g.PredecessorID != nil && previous < 0) || (g.SuccessorID != nil && next == len(rows)) || next != previous+1 {
		return "", fault(f.ConfirmationStale)
	}
	if next == len(rows) {
		return "", nil
	}
	return rows[next].ID, nil
}
func buildTaskBusyRecord(r c.TaskBusyCompensationRequest, claim schedulerClaimRecord, before c.Task, sprint c.Sprint, current *c.SprintID, groups []taskGroupPlan, query int64, historyID c.TaskEventID, eventID event.EventID, at f.Instant) (taskBusyRecord, error) {
	var zero taskBusyRecord
	if r.Validate() != nil || validateSchedulerClaimRecord(&claim) != nil || claim.Request != r.Claim || before.Validate() != nil || before.ProjectID != r.Claim.ProjectID || before.ID != r.Claim.TaskID || before.Version < claim.Guard.ClaimedVersion || sprint.Validate() != nil || sprint.ProjectID != before.ProjectID || sprint.ID != claim.Guard.SourceSprintID || at.Validate() != nil {
		return zero, internal(nil)
	}
	if current != nil && current.Validate() != nil {
		return zero, internal(nil)
	}
	// Rank-only maintenance may change the physical rank without changing
	// this Task's business version. No other same-version rewrite is valid.
	if before.Version == claim.Guard.ClaimedVersion {
		expected := claim.After.Clone()
		expected.ManualRank = before.ManualRank
		if !sameValue(expected, before) {
			return zero, internal(nil)
		}
	}
	if at.Time().Before(before.UpdatedAt.Time()) {
		at = before.UpdatedAt
	}
	rec := taskBusyRecord{Request: r.Clone(), Claim: claim, Before: before.Clone(), After: before.Clone(), Sprint: sprint.Clone(), CurrentSprintID: current, Groups: []taskGroupPlan{}, CreatedAt: at}
	if current != nil {
		value := *current
		rec.CurrentSprintID = &value
	}
	if !busyCanRestore(&claim, before, sprint, current) {
		if len(groups) != 0 || query != 0 || historyID.Validate() == nil || eventID.Validate() == nil {
			return zero, internal(nil)
		}
		return rec, nil
	}
	if len(groups) != 2 || historyID.Validate() != nil || eventID.Validate() != nil {
		return zero, internal(nil)
	}
	source := groupForTask(before)
	target := source
	target.State = c.TaskStateTodo
	if groups[0].Group != source || groups[1].Group != target || validateTaskRanks(groups[0].Before) != nil {
		return zero, internal(nil)
	}
	anchor, err := busyRestoreBefore(groups[1].Before, groups[1].Generation, claim.Guard)
	if err != nil {
		return zero, err
	}
	sourceNext, err := transitionNext(groups[0].Generation)
	if err != nil {
		return zero, err
	}
	targetNext, err := transitionNext(groups[1].Generation)
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
	remaining := make([]rankItem, 0, len(groups[0].Before))
	found := -1
	for n, item := range groups[0].Before {
		if item.ID == before.ID.String() {
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
	var previous, next string
	if found > 0 {
		previous = groups[0].Before[found-1].ID
	}
	if found+1 < len(groups[0].Before) {
		next = groups[0].Before[found+1].ID
	}
	sp, err := transitionPosition(source, sourceNext, previous, next)
	if err != nil {
		return zero, err
	}
	ranks, err := rankFor(groups[1].Before, before.ID.String(), anchor, true)
	if err != nil {
		return zero, err
	}
	tp, err := transitionPosition(target, targetNext, ranks.Previous, ranks.Next)
	if err != nil {
		return zero, err
	}
	after := before.Clone()
	after.State = c.TaskStateTodo
	after.Version = f.Version(version)
	after.ManualRank = ranks.Rank
	after.UpdatedAt = at
	id, err := f.ParseID[c.SchedulerClaim](r.Claim.DispatchID)
	if err != nil {
		return zero, internal(err)
	}
	actor := c.SchedulerTaskActor{CauseID: r.Claim.DispatchID}
	h := c.TaskBusyTaskEvent{ID: historyID, ProjectID: before.ProjectID, TaskID: before.ID, TaskVersion: after.Version, Type: c.TaskTransitionStateChanged, Actor: actor, OperationID: id, CorrelationID: id, Payload: c.TaskBusyStateChanged{FromState: c.TaskStateInProgress, ToState: c.TaskStateTodo, ReasonCode: "scheduler_agent_busy_compensation"}, CreatedAt: at}
	header, err := taskHeader(eventID, after, at)
	if err != nil {
		return zero, err
	}
	header.EventType = c.TaskTransitionedName
	header.SchemaVersion = c.TaskBusyCompensationSchemaVersion
	payload := c.TaskBusyCompensated{ClaimID: id, Actor: actor, TaskEventID: historyID, MilestoneID: before.MilestoneID, SprintID: before.SprintID, AgentID: r.Claim.AgentID, SourcePosition: sp, TargetPosition: tp}
	if after.Validate() != nil || h.Validate() != nil || payload.Validate() != nil {
		return zero, internal(nil)
	}
	rec.After = after
	rec.Restored = true
	rec.Groups = []taskGroupPlan{{source, slices.Clone(groups[0].Before), remaining, groups[0].Generation}, {target, slices.Clone(groups[1].Before), ranks.Items, groups[1].Generation}}
	rec.QueryGeneration = query
	rec.History = &h
	rec.Header = &header
	rec.Event = &payload
	return rec, nil
}
func validateTaskBusyRecord(r *taskBusyRecord) error {
	if r == nil {
		return internal(nil)
	}
	var h c.TaskEventID
	var e event.EventID
	if r.History != nil {
		h = r.History.ID
	}
	if r.Header != nil {
		e = r.Header.EventID
	}
	expected, err := buildTaskBusyRecord(r.Request, r.Claim, r.Before, r.Sprint, r.CurrentSprintID, r.Groups, r.QueryGeneration, h, e, r.CreatedAt)
	if err != nil || !sameValue(expected, *r) {
		return internal(nil)
	}
	return nil
}
func (r *taskBusyRecord) UnmarshalJSON(raw []byte) error {
	if r == nil {
		return internal(nil)
	}
	fields, err := taskPrivateObject(raw, taskPlanCap, []string{"request", "claim", "before", "after", "sprint", "current_sprint_id", "restored", "groups", "query_generation", "history", "event", "header", "created_at"}, []string{"current_sprint_id", "history", "event", "header"})
	if err != nil {
		return internal(nil)
	}
	if _, err = taskPrivateObject(fields["request"], taskRequestCap, []string{"Claim", "DispatchVersion", "LaunchAttempt"}, nil); err != nil {
		return internal(nil)
	}
	type wire taskBusyRecord
	var next wire
	if err = json.Unmarshal(raw, &next); err != nil {
		return internal(err)
	}
	v := taskBusyRecord(next)
	if err = validateTaskBusyRecord(&v); err != nil {
		return err
	}
	canonicalRaw, e := cursorPayload(raw)
	encoded, e2 := canonical(v)
	if e != nil || e2 != nil || !slices.Equal(canonicalRaw, encoded) {
		return internal(nil)
	}
	*r = v
	return nil
}
func planTaskBusyRecord(ctx context.Context, x postgres.SQLExecutor, r c.TaskBusyCompensationRequest, claim schedulerClaimRecord, before c.Task, sprint c.Sprint, current *c.SprintID) (taskBusyRecord, error) {
	groups := []taskGroupPlan{}
	var query int64
	var history c.TaskEventID
	var ev event.EventID
	if busyCanRestore(&claim, before, sprint, current) {
		source := groupForTask(before)
		target := source
		target.State = c.TaskStateTodo
		for _, g := range []taskGroup{source, target} {
			rows, gen, err := loadTaskRanks(ctx, x, r.Claim.ProjectID, g)
			if err != nil {
				return taskBusyRecord{}, err
			}
			groups = append(groups, taskGroupPlan{Group: g, Before: rows, Generation: gen})
		}
		var err error
		query, err = loadTaskQueryGeneration(ctx, x, r.Claim.ProjectID)
		if err != nil {
			return taskBusyRecord{}, err
		}
		history, err = f.NewID[c.TaskEvent]()
		if err != nil {
			return taskBusyRecord{}, internal(err)
		}
		ev, err = f.NewID[event.EventIdentity]()
		if err != nil {
			return taskBusyRecord{}, internal(err)
		}
	}
	at, err := f.NewInstant(time.Now())
	if err != nil {
		return taskBusyRecord{}, internal(err)
	}
	return buildTaskBusyRecord(r, claim, before, sprint, current, groups, query, history, ev, at)
}
func applyTaskBusyRecord(ctx context.Context, x postgres.SQLExecutor, r *taskBusyRecord) error {
	if err := validateTaskBusyRecord(r); err != nil {
		return err
	}
	raw, err := canonical(r)
	if err != nil || len(raw) > taskPlanCap {
		return internal(err)
	}
	var hid, eid any
	if r.Restored {
		hid = r.History.ID.String()
		eid = r.Header.EventID.String()
	}
	if err = taskAffected(x.Exec(ctx, `INSERT INTO agenteam_work.task_busy_compensations(id,project_id,task_id,agent_id,request_id,dispatch_version,launch_attempt,restored,before_version,after_version,task_event_id,event_id,record,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)`, r.Request.Claim.DispatchID, r.Before.ProjectID.String(), r.Before.ID.String(), r.Request.Claim.AgentID.String(), r.Request.Claim.RequestID.String(), int64(r.Request.DispatchVersion), r.Request.LaunchAttempt, r.Restored, int64(r.Before.Version), int64(r.After.Version), hid, eid, raw, r.CreatedAt.Time())); err != nil {
		return err
	}
	if !r.Restored {
		return nil
	}
	t := r.After
	if err = taskAffected(x.Exec(ctx, `UPDATE agenteam_work.tasks SET state='todo',manual_rank=$4,version=$5,updated_at=$6 WHERE project_id=$1 AND id=$2 AND version=$3 AND state='in_progress' AND assignee_agent_id=$7 AND sprint_id=$8 AND priority=$9`, t.ProjectID.String(), t.ID.String(), int64(r.Before.Version), t.ManualRank, int64(t.Version), t.UpdatedAt.Time(), r.Request.Claim.AgentID.String(), t.SprintID.String(), string(t.Priority))); err != nil {
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
	return taskAffected(x.Exec(ctx, `INSERT INTO agenteam_work.task_events(id,project_id,task_id,task_version,type,actor,compensation_operation_id,correlation_id,payload,created_at) VALUES($1,$2,$3,$4,'state_changed',$5,$6,$6,$7,$8)`, h.ID.String(), h.ProjectID.String(), h.TaskID.String(), int64(h.TaskVersion), actor, h.OperationID.String(), payload, h.CreatedAt.Time()))
}
func loadTaskBusyRecord(ctx context.Context, x postgres.SQLExecutor, p c.ProjectID, id string) (*taskBusyRecord, error) {
	var raw []byte
	var task, agent, request string
	var dv, attempt, before, after int64
	var restored bool
	var history, eventID *string
	var at time.Time
	err := x.QueryRow(ctx, `SELECT task_id::text,agent_id::text,request_id::text,dispatch_version,launch_attempt,restored,before_version,after_version,task_event_id::text,event_id::text,record,created_at FROM agenteam_work.task_busy_compensations WHERE project_id=$1 AND id=$2`, p.String(), id).Scan(&task, &agent, &request, &dv, &attempt, &restored, &before, &after, &history, &eventID, &raw, &at)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, taskSQL(err)
	}
	var r taskBusyRecord
	if json.Unmarshal(raw, &r) != nil || r.Request.Claim.ProjectID != p || r.Request.Claim.DispatchID != id || r.Before.ID.String() != task || r.Request.Claim.AgentID.String() != agent || r.Request.Claim.RequestID.String() != request || int64(r.Request.DispatchVersion) != dv || r.Request.LaunchAttempt != attempt || r.Restored != restored || int64(r.Before.Version) != before || int64(r.After.Version) != after || !r.CreatedAt.Time().Equal(at) {
		return nil, internal(nil)
	}
	if restored {
		if history == nil || eventID == nil || r.History.ID.String() != *history || r.Header.EventID.String() != *eventID {
			return nil, internal(nil)
		}
	} else if history != nil || eventID != nil {
		return nil, internal(nil)
	}
	return &r, nil
}
func verifyTaskBusyPostimage(ctx context.Context, x postgres.SQLExecutor, r *taskBusyRecord) error {
	actual, err := loadTaskBusyRecord(ctx, x, r.Before.ProjectID, r.Request.Claim.DispatchID)
	if err != nil {
		return err
	}
	if actual == nil || !sameValue(actual, r) {
		return fault(f.Forbidden)
	}
	claim, err := loadSchedulerClaim(ctx, x, r.Before.ProjectID, r.Request.Claim.DispatchID)
	if err != nil {
		return err
	}
	if claim == nil || !sameValue(*claim, r.Claim) {
		return fault(f.Forbidden)
	}
	t, err := loadTask(ctx, x, r.Before.ProjectID, r.Before.ID)
	if err != nil {
		return err
	}
	if !sameValue(t, r.After) {
		return fault(f.Forbidden)
	}
	if r.Restored {
		q, err := loadTaskQueryGeneration(ctx, x, r.Before.ProjectID)
		if err != nil {
			return err
		}
		if q != r.QueryGeneration+1 {
			return fault(f.Forbidden)
		}
		for _, g := range r.Groups {
			rows, gen, err := loadTaskRanks(ctx, x, r.Before.ProjectID, g.Group)
			if err != nil {
				return err
			}
			if gen != g.Generation+1 || !sameValue(rows, g.After) {
				return fault(f.Forbidden)
			}
		}
	}
	rows, err := x.Query(ctx, `SELECT id::text,project_id::text,task_id::text,task_version,type,actor,operation_id::text,blocker_operation_id::text,transition_operation_id::text,claim_operation_id::text,compensation_operation_id::text,correlation_id::text,payload,created_at FROM agenteam_work.task_events WHERE project_id=$1 AND compensation_operation_id=$2 ORDER BY id`, r.Before.ProjectID.String(), r.Request.Claim.DispatchID)
	if err != nil {
		return taskSQL(err)
	}
	if nilPort(rows) {
		return internal(nil)
	}
	defer rows.Close()
	if r.Restored {
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
		var history c.TaskBusyTaskEvent
		if json.Unmarshal(raw, &history) != nil || !sameValue(history, *r.History) {
			return fault(f.Forbidden)
		}
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
