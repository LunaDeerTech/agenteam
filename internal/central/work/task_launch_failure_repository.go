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

type taskFailureRecord struct {
	Request         c.TaskLaunchFailureRequest `json:"request"`
	Facts           c.TaskLaunchFailureFacts   `json:"facts"`
	Claim           schedulerClaimRecord       `json:"claim,omitzero"`
	Relaunch        *taskRelaunchRecord        `json:"relaunch,omitempty"`
	RelaunchEvent   *c.TaskRelaunchFailed      `json:"relaunch_event,omitempty"`
	Before          c.Task                     `json:"before"`
	After           c.Task                     `json:"after"`
	Sprint          c.Sprint                   `json:"sprint"`
	CurrentSprintID *c.SprintID                `json:"current_sprint_id"`
	Changed         bool                       `json:"changed"`
	Groups          []taskGroupPlan            `json:"groups"`
	QueryGeneration int64                      `json:"query_generation"`
	Blocker         *c.TaskBlocker             `json:"blocker"`
	History         []c.TaskFailureTaskEvent   `json:"history"`
	Event           *c.TaskLaunchFailed        `json:"event"`
	Header          *event.Header              `json:"header"`
	CreatedAt       f.Instant                  `json:"created_at"`
}

// A later title/plan/version is retained. Applicability is the original Task,
// assignee and source relation, not equality with the claim's old postimage.
func failureCanBlock(claim *schedulerClaimRecord, before c.Task, sprint c.Sprint, relaunch ...*taskRelaunchRecord) bool {
	base := claim.Before
	phase := c.TaskStateInProgress // A historical todo claim is always work.
	if len(relaunch) > 0 && relaunch[0] != nil {
		base = relaunch[0].Task
		phase = taskRelaunchState(relaunch[0].Request.Purpose)
	}
	return phase != "" && (before.State == phase || before.State == c.TaskStateBlocked) && before.AssigneeAgentID != nil && base.AssigneeAgentID != nil && *before.AssigneeAgentID == *base.AssigneeAgentID && before.SprintID == base.SprintID && before.MilestoneID == base.MilestoneID && sprint.ID == base.SprintID && sprint.State != c.Completed
}
func buildTaskFailureRecord(r c.TaskLaunchFailureRequest, facts c.TaskLaunchFailureFacts, claim schedulerClaimRecord, before c.Task, sprint c.Sprint, current *c.SprintID, groups []taskGroupPlan, query int64, blockerID c.TaskBlockerID, historyIDs []c.TaskEventID, eventID event.EventID, at f.Instant, relaunch ...*taskRelaunchRecord) (taskFailureRecord, error) {
	var zero taskFailureRecord
	var origin *taskRelaunchRecord
	if len(relaunch) > 1 {
		return zero, internal(nil)
	}
	if len(relaunch) == 1 {
		origin = relaunch[0]
	}
	baseline, originErr := taskFailureBaseline(r, facts, claim, origin)
	if originErr != nil || before.Validate() != nil || before.ProjectID != r.ProjectID() || before.ID != r.TaskID() || before.Version < baseline.Version || sprint.Validate() != nil || sprint.ProjectID != before.ProjectID || sprint.ID != baseline.SprintID || at.Validate() != nil {
		return zero, internal(originErr)
	}
	if current != nil && current.Validate() != nil {
		return zero, internal(nil)
	}
	if before.Version == baseline.Version {
		expected := baseline.Clone()
		expected.ManualRank = before.ManualRank
		if !sameValue(expected, before) {
			return zero, internal(nil)
		}
	}
	if at.Time().Before(before.UpdatedAt.Time()) {
		at = before.UpdatedAt
	}
	if at.Time().Before(facts.OccurredAt.Time()) {
		at = facts.OccurredAt
	}
	rec := taskFailureRecord{Request: r.Clone(), Facts: facts.Clone(), Claim: claim, Relaunch: cloneTaskRelaunchRecord(origin), Before: before.Clone(), After: before.Clone(), Sprint: sprint.Clone(), Groups: []taskGroupPlan{}, History: []c.TaskFailureTaskEvent{}, CreatedAt: at}
	if current != nil {
		x := *current
		rec.CurrentSprintID = &x
	}
	if !failureCanBlock(&claim, before, sprint, origin) {
		if len(groups) != 0 || query != 0 || blockerID.Validate() == nil || len(historyIDs) != 0 || eventID.Validate() == nil {
			return zero, internal(nil)
		}
		return rec, nil
	}
	n := 1
	if before.State != c.TaskStateBlocked {
		n = 2
	}
	if len(historyIDs) != n || blockerID.Validate() != nil || eventID.Validate() != nil || historyIDs == nil {
		return zero, internal(nil)
	}
	for k, id := range historyIDs {
		if id.Validate() != nil || (k > 0 && historyIDs[k-1].String() >= id.String()) {
			return zero, internal(nil)
		}
	}
	version, e := transitionNext(int64(before.Version))
	if e != nil {
		return zero, e
	}
	if _, e = transitionNext(query); e != nil {
		return zero, e
	}
	after := before.Clone()
	after.State = c.TaskStateBlocked
	after.Version = f.Version(version)
	after.UpdatedAt = at
	var sourcePosition, targetPosition *c.TaskTransitionPosition
	if n == 1 {
		if len(groups) != 0 {
			return zero, internal(nil)
		}
	} else {
		if len(groups) != 2 {
			return zero, internal(nil)
		}
		source := groupForTask(before)
		target := source
		target.State = c.TaskStateBlocked
		if groups[0].Group != source || groups[1].Group != target || validateTaskRanks(groups[0].Before) != nil || validateTaskRanks(groups[1].Before) != nil || len(groups[1].Before) >= taskGroupCap {
			return zero, internal(nil)
		}
		srcNext, e := transitionNext(groups[0].Generation)
		if e != nil {
			return zero, e
		}
		dstNext, e := transitionNext(groups[1].Generation)
		if e != nil {
			return zero, e
		}
		remaining := make([]rankItem, 0, len(groups[0].Before))
		found := -1
		for k, item := range groups[0].Before {
			if item.ID == before.ID.String() {
				found = k
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
		var prev, next string
		if found > 0 {
			prev = groups[0].Before[found-1].ID
		}
		if found+1 < len(groups[0].Before) {
			next = groups[0].Before[found+1].ID
		}
		sp, e := transitionPosition(source, srcNext, prev, next)
		if e != nil {
			return zero, e
		}
		ranks, e := rankFor(groups[1].Before, before.ID.String(), "", true)
		if e != nil {
			return zero, e
		}
		tp, e := transitionPosition(target, dstNext, ranks.Previous, ranks.Next)
		if e != nil {
			return zero, e
		}
		sourcePosition, targetPosition = &sp, &tp
		after.ManualRank = ranks.Rank
		rec.Groups = []taskGroupPlan{{source, slices.Clone(groups[0].Before), remaining, groups[0].Generation}, {target, slices.Clone(groups[1].Before), ranks.Items, groups[1].Generation}}
	}
	id, e := f.ParseID[c.SchedulerClaim](r.DispatchID())
	if e != nil {
		return zero, internal(e)
	}
	actor := c.SchedulerTaskActor{CauseID: r.DispatchID()}
	blocker := c.TaskBlocker{ID: blockerID, ProjectID: before.ProjectID, TaskID: before.ID, Type: c.TaskBlockerTechnical, Description: "Scheduler launch failed.", CreatedAt: at, SchedulerCreatedBy: &actor, Technical: &c.TaskBlockerTechnicalMetadata{Code: "scheduler_launch_failed", Source: "scheduler_dispatch", ReferenceID: r.DispatchID()}}
	history := []c.TaskFailureTaskEvent{{ID: historyIDs[0], ProjectID: before.ProjectID, TaskID: before.ID, TaskVersion: after.Version, Type: "blocker_added", Actor: actor, OperationID: id, CorrelationID: id, Blocker: &c.TaskFailureBlockerAdded{BlockerID: blockerID, BlockerType: c.TaskBlockerTechnical, ReasonCode: c.TaskLaunchFailureHistoryReason}, CreatedAt: at}}
	if n == 2 {
		history = append(history, c.TaskFailureTaskEvent{ID: historyIDs[1], ProjectID: before.ProjectID, TaskID: before.ID, TaskVersion: after.Version, Type: "state_changed", Actor: actor, OperationID: id, CorrelationID: id, State: &c.TaskFailureStateChanged{FromState: before.State, ToState: after.State, ReasonCode: c.TaskLaunchFailureHistoryReason}, CreatedAt: at})
	}
	header, e := taskHeader(eventID, after, at)
	if e != nil {
		return zero, e
	}
	header.EventType = c.TaskTransitionedName
	header.SchemaVersion = c.TaskLaunchFailureSchemaVersion
	var payload *c.TaskLaunchFailed
	var relaunchEvent *c.TaskRelaunchFailed
	if origin == nil {
		v := c.TaskLaunchFailed{ClaimID: id, Actor: actor, BlockerID: blockerID, TaskEventIDs: slices.Clone(historyIDs), MilestoneID: before.MilestoneID, SprintID: before.SprintID, AgentID: r.AgentID(), FromState: before.State, ToState: after.State, Reason: facts.Reason, SourcePosition: sourcePosition, TargetPosition: targetPosition}
		if v.Validate() != nil {
			return zero, internal(nil)
		}
		payload = &v
	} else {
		dispatch, e := f.ParseID[f.Request](r.DispatchID())
		if e != nil {
			return zero, internal(e)
		}
		v := c.TaskRelaunchFailed{DispatchID: dispatch, Origin: c.TaskDispatchRelaunch, Source: origin.source(), Actor: actor, BlockerID: blockerID, TaskEventIDs: slices.Clone(historyIDs), MilestoneID: before.MilestoneID, SprintID: before.SprintID, AgentID: r.AgentID(), FromState: before.State, ToState: after.State, Reason: facts.Reason, SourcePosition: sourcePosition, TargetPosition: targetPosition}
		if v.Validate() != nil {
			return zero, internal(nil)
		}
		relaunchEvent = &v
		header.SchemaVersion = c.TaskRelaunchFailureSchemaVersion
	}
	if after.Validate() != nil || blocker.Validate() != nil {
		return zero, internal(nil)
	}
	for _, h := range history {
		if h.Validate() != nil {
			return zero, internal(nil)
		}
	}
	rec.After = after
	rec.Changed = true
	rec.QueryGeneration = query
	rec.Blocker = &blocker
	rec.History = history
	rec.Header = &header
	rec.Event = payload
	rec.RelaunchEvent = relaunchEvent
	return rec, nil
}
func validateTaskFailureRecord(r *taskFailureRecord) error {
	if r == nil {
		return internal(nil)
	}
	var b c.TaskBlockerID
	var ev event.EventID
	ids := []c.TaskEventID{}
	if r.Blocker != nil {
		b = r.Blocker.ID
	}
	if r.Header != nil {
		ev = r.Header.EventID
	}
	for _, h := range r.History {
		ids = append(ids, h.ID)
	}
	expected, e := buildTaskFailureRecord(r.Request, r.Facts, r.Claim, r.Before, r.Sprint, r.CurrentSprintID, r.Groups, r.QueryGeneration, b, ids, ev, r.CreatedAt, r.Relaunch)
	if e != nil || !sameValue(expected, *r) {
		return internal(nil)
	}
	return nil
}
func (r *taskFailureRecord) UnmarshalJSON(raw []byte) error {
	if r == nil {
		return internal(nil)
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil {
		return internal(nil)
	}
	_, isRelaunch := fields["relaunch"]
	keys := []string{"request", "facts", "claim", "before", "after", "sprint", "current_sprint_id", "changed", "groups", "query_generation", "blocker", "history", "event", "header", "created_at"}
	if isRelaunch {
		keys[2] = "relaunch"
		if _, ok := fields["relaunch_event"]; ok {
			keys = append(keys, "relaunch_event")
		}
	}
	if _, e := taskPrivateObject(raw, taskPlanCap, keys, []string{"current_sprint_id", "blocker", "event", "header"}); e != nil {
		return internal(nil)
	}
	var e error
	type wire taskFailureRecord
	var w wire
	if json.Unmarshal(raw, &w) != nil {
		return internal(nil)
	}
	n := taskFailureRecord(w)
	if e = validateTaskFailureRecord(&n); e != nil {
		return e
	}
	actual, e := cursorPayload(raw)
	expected, e2 := canonical(n)
	if e != nil || e2 != nil || !slices.Equal(actual, expected) {
		return internal(nil)
	}
	*r = n
	return nil
}
func requireTaskFailureCapacity(ctx context.Context, x postgres.SQLExecutor, t c.Task, n int) error {
	var total, unresolved, history int64
	if e := x.QueryRow(ctx, `SELECT (SELECT count(*) FROM agenteam_work.task_blockers WHERE project_id=$1 AND resolved_at IS NULL),(SELECT count(*) FROM agenteam_work.task_blockers WHERE project_id=$1 AND task_id=$2 AND resolved_at IS NULL),(SELECT count(*) FROM agenteam_work.task_blockers WHERE project_id=$1 AND task_id=$2)`, t.ProjectID.String(), t.ID.String()).Scan(&total, &unresolved, &history); e != nil {
		return taskSQL(e)
	}
	if total < 0 || unresolved < 0 || history < 0 || unresolved > total || unresolved > history || n < 1 || n > 2 {
		return internal(nil)
	}
	if total >= blockerProjectCap || unresolved >= blockerTaskCap || history >= blockerHistoryCap {
		return fault(f.ResourceBusy)
	}
	return ctx.Err()
}
func planTaskFailureRecord(ctx context.Context, x postgres.SQLExecutor, r c.TaskLaunchFailureRequest, facts c.TaskLaunchFailureFacts, claim schedulerClaimRecord, before c.Task, sprint c.Sprint, current *c.SprintID, relaunch ...*taskRelaunchRecord) (taskFailureRecord, error) {
	var origin *taskRelaunchRecord
	if len(relaunch) > 1 {
		return taskFailureRecord{}, internal(nil)
	}
	if len(relaunch) == 1 {
		origin = relaunch[0]
	}
	groups := []taskGroupPlan{}
	var query int64
	var b c.TaskBlockerID
	ids := []c.TaskEventID{}
	var ev event.EventID
	if failureCanBlock(&claim, before, sprint, origin) {
		n := 1
		if before.State != c.TaskStateBlocked {
			n = 2
			source := groupForTask(before)
			target := source
			target.State = c.TaskStateBlocked
			for _, g := range []taskGroup{source, target} {
				rows, gen, e := loadTaskRanks(ctx, x, r.ProjectID(), g)
				if e != nil {
					return taskFailureRecord{}, e
				}
				groups = append(groups, taskGroupPlan{Group: g, Before: rows, Generation: gen})
			}
		}
		if e := requireTaskFailureCapacity(ctx, x, before, n); e != nil {
			return taskFailureRecord{}, e
		}
		var e error
		query, e = loadTaskQueryGeneration(ctx, x, r.ProjectID())
		if e != nil {
			return taskFailureRecord{}, e
		}
		b, e = f.NewID[c.TaskBlockerIdentity]()
		if e != nil {
			return taskFailureRecord{}, internal(e)
		}
		for k := 0; k < n; k++ {
			id, e := f.NewID[c.TaskEvent]()
			if e != nil {
				return taskFailureRecord{}, internal(e)
			}
			ids = append(ids, id)
		}
		slices.SortFunc(ids, func(a, b c.TaskEventID) int {
			if a.String() < b.String() {
				return -1
			}
			if a == b {
				return 0
			}
			return 1
		})
		ev, e = f.NewID[event.EventIdentity]()
		if e != nil {
			return taskFailureRecord{}, internal(e)
		}
	}
	at, e := f.NewInstant(time.Now())
	if e != nil {
		return taskFailureRecord{}, internal(e)
	}
	return buildTaskFailureRecord(r, facts, claim, before, sprint, current, groups, query, b, ids, ev, at, origin)
}

func applyTaskFailureRecord(ctx context.Context, x postgres.SQLExecutor, r *taskFailureRecord) error {
	if e := validateTaskFailureRecord(r); e != nil {
		return e
	}
	raw, err := canonical(r)
	if err != nil || len(raw) > taskPlanCap {
		return internal(err)
	}
	var bid, eid any
	if r.Changed {
		bid = r.Blocker.ID.String()
		eid = r.Header.EventID.String()
	}
	query := `INSERT INTO agenteam_work.task_launch_failures(id,project_id,task_id,agent_id,request_id,dispatch_version,launch_attempt,reason,failure_occurred_at,changed,before_version,after_version,blocker_id,event_id,record,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16)`
	args := []any{r.Request.DispatchID(), r.Before.ProjectID.String(), r.Before.ID.String(), r.Request.AgentID().String(), r.Request.RequestID().String(), int64(r.Request.DispatchVersion), r.Request.LaunchAttempt, string(r.Facts.Reason), r.Facts.OccurredAt.Time(), r.Changed, int64(r.Before.Version), int64(r.After.Version), bid, eid, raw, r.CreatedAt.Time()}
	if r.Relaunch != nil {
		query = `INSERT INTO agenteam_work.task_launch_failures(id,project_id,task_id,agent_id,request_id,dispatch_version,launch_attempt,reason,failure_occurred_at,changed,before_version,after_version,blocker_id,event_id,record,created_at,relaunch_operation_id) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17)`
		args = append(args, r.Request.DispatchID())
	}
	if err = taskAffected(x.Exec(ctx, query, args...)); err != nil {
		return err
	}
	if !r.Changed {
		return nil
	}
	t := r.After
	if err = taskAffected(x.Exec(ctx, `UPDATE agenteam_work.tasks SET state='blocked',manual_rank=$4,version=$5,updated_at=$6 WHERE project_id=$1 AND id=$2 AND version=$3 AND state=$7 AND assignee_agent_id=$8 AND sprint_id=$9 AND priority=$10`, t.ProjectID.String(), t.ID.String(), int64(r.Before.Version), t.ManualRank, int64(t.Version), t.UpdatedAt.Time(), string(r.Before.State), r.Request.AgentID().String(), t.SprintID.String(), string(t.Priority))); err != nil {
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
	b := r.Blocker
	actor, err := json.Marshal(b.SchedulerCreatedBy)
	if err != nil {
		return internal(err)
	}
	metadata, err := json.Marshal(b.Technical)
	if err != nil {
		return internal(err)
	}
	if err = taskAffected(x.Exec(ctx, `INSERT INTO agenteam_work.task_blockers(id,project_id,task_id,type,description,metadata,created_at,created_by,failure_operation_id) VALUES($1,$2,$3,'technical',$4,$5,$6,$7,$8)`, b.ID.String(), b.ProjectID.String(), b.TaskID.String(), b.Description, metadata, b.CreatedAt.Time(), actor, r.Request.DispatchID())); err != nil {
		return err
	}
	for _, h := range r.History {
		actor, e := json.Marshal(h.Actor)
		if e != nil {
			return internal(e)
		}
		var payload any = h.Blocker
		if h.Type == "state_changed" {
			payload = h.State
		}
		raw, e := json.Marshal(payload)
		if e != nil {
			return internal(e)
		}
		if e = taskAffected(x.Exec(ctx, `INSERT INTO agenteam_work.task_events(id,project_id,task_id,task_version,type,actor,failure_operation_id,correlation_id,payload,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$7,$8,$9)`, h.ID.String(), h.ProjectID.String(), h.TaskID.String(), int64(h.TaskVersion), h.Type, actor, h.OperationID.String(), raw, h.CreatedAt.Time())); e != nil {
			return e
		}
	}
	return ctx.Err()
}
func loadTaskFailureRecord(ctx context.Context, x postgres.SQLExecutor, p c.ProjectID, id string) (*taskFailureRecord, error) {
	var raw []byte
	var task, agent, request, reason string
	var dv, attempt, before, after int64
	var changed bool
	var blocker, eventID *string
	var at, occurred time.Time
	err := x.QueryRow(ctx, `SELECT task_id::text,agent_id::text,request_id::text,dispatch_version,launch_attempt,reason,failure_occurred_at,changed,before_version,after_version,blocker_id::text,event_id::text,record,created_at FROM agenteam_work.task_launch_failures WHERE project_id=$1 AND id=$2`, p.String(), id).Scan(&task, &agent, &request, &dv, &attempt, &reason, &occurred, &changed, &before, &after, &blocker, &eventID, &raw, &at)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, taskSQL(err)
	}
	var r taskFailureRecord
	if json.Unmarshal(raw, &r) != nil || r.Request.ProjectID() != p || r.Request.DispatchID() != id || r.Before.ID.String() != task || r.Request.AgentID().String() != agent || r.Request.RequestID().String() != request || int64(r.Request.DispatchVersion) != dv || r.Request.LaunchAttempt != attempt || string(r.Facts.Reason) != reason || !r.Facts.OccurredAt.Time().Equal(occurred) || r.Changed != changed || int64(r.Before.Version) != before || int64(r.After.Version) != after || !r.CreatedAt.Time().Equal(at) {
		return nil, internal(nil)
	}
	if changed {
		if blocker == nil || eventID == nil || r.Blocker.ID.String() != *blocker || r.Header.EventID.String() != *eventID {
			return nil, internal(nil)
		}
	} else if blocker != nil || eventID != nil {
		return nil, internal(nil)
	}
	return &r, nil
}
func verifyTaskFailurePostimage(ctx context.Context, x postgres.SQLExecutor, r *taskFailureRecord) error {
	actual, err := loadTaskFailureRecord(ctx, x, r.Before.ProjectID, r.Request.DispatchID())
	if err != nil {
		return err
	}
	if actual == nil || !sameValue(actual, r) {
		return fault(f.Forbidden)
	}
	if err = verifyTaskFailureOrigin(ctx, x, r); err != nil {
		return err
	}
	t, err := loadTask(ctx, x, r.Before.ProjectID, r.Before.ID)
	if err != nil {
		return err
	}
	if !sameValue(t, r.After) {
		return fault(f.Forbidden)
	}
	if r.Changed {
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
	rows, err := x.Query(ctx, `SELECT `+blockerColumns+` FROM agenteam_work.task_blockers WHERE project_id=$1 AND failure_operation_id=$2 ORDER BY id`, r.Before.ProjectID.String(), r.Request.DispatchID())
	if err != nil {
		return taskSQL(err)
	}
	if nilPort(rows) {
		return internal(nil)
	}
	if r.Changed {
		if !rows.Next() {
			err = rows.Err()
			rows.Close()
			if err != nil {
				return taskSQL(err)
			}
			return fault(f.Forbidden)
		}
		b, e := scanBlocker(rows)
		if e != nil {
			rows.Close()
			return e
		}
		if b == nil || b.FailureOperation == nil || b.FailureOperation.String() != r.Request.DispatchID() || !sameValue(b.Value, *r.Blocker) {
			rows.Close()
			return fault(f.Forbidden)
		}
	}
	if rows.Next() {
		rows.Close()
		return fault(f.Forbidden)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return taskSQL(err)
	}
	if err = rows.Err(); err != nil {
		return taskSQL(err)
	}
	history, err := x.Query(ctx, `SELECT id::text,project_id::text,task_id::text,task_version,type,actor,operation_id::text,blocker_operation_id::text,transition_operation_id::text,claim_operation_id::text,compensation_operation_id::text,failure_operation_id::text,correlation_id::text,payload,created_at FROM agenteam_work.task_events WHERE project_id=$1 AND failure_operation_id=$2 ORDER BY id`, r.Before.ProjectID.String(), r.Request.DispatchID())
	if err != nil {
		return taskSQL(err)
	}
	if nilPort(history) {
		return internal(nil)
	}
	defer history.Close()
	for _, expected := range r.History {
		if !history.Next() {
			if err = history.Err(); err != nil {
				return taskSQL(err)
			}
			return fault(f.Forbidden)
		}
		raw, e := scanTaskTriggerEventWithFailure(history)
		if e != nil {
			return e
		}
		var h c.TaskFailureTaskEvent
		if json.Unmarshal(raw, &h) != nil || !sameValue(h, expected) {
			return fault(f.Forbidden)
		}
	}
	if history.Next() {
		return fault(f.Forbidden)
	}
	if err = history.Err(); err != nil {
		return taskSQL(err)
	}
	history.Close()
	if err = history.Err(); err != nil {
		return taskSQL(err)
	}
	return ctx.Err()
}
