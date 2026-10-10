package work

import (
	"context"
	"math"
	"slices"

	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	c "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
)

func transitionNext(v int64) (int64, error) {
	if v < 1 {
		return 0, internal(nil)
	}
	if v == math.MaxInt64 {
		return 0, fault(f.ResourceBusy)
	}
	return v + 1, nil
}
func pendingGroups(groups ...taskGroup) []ec.PendingClaimGroup {
	out := make([]ec.PendingClaimGroup, 0, len(groups))
	for _, g := range groups {
		out = append(out, ec.PendingClaimGroup{SprintID: g.Sprint.String(), State: string(g.State), Priority: string(g.Priority)})
	}
	slices.SortFunc(out, func(a, b ec.PendingClaimGroup) int {
		if a.SprintID < b.SprintID {
			return -1
		}
		if a.SprintID > b.SprintID {
			return 1
		}
		if a.State < b.State {
			return -1
		}
		if a.State > b.State {
			return 1
		}
		if a.Priority < b.Priority {
			return -1
		}
		if a.Priority > b.Priority {
			return 1
		}
		return 0
	})
	return slices.Compact(out)
}
func transitionPosition(g taskGroup, gen int64, previous, next string) (c.TaskTransitionPosition, error) {
	p := c.TaskTransitionPosition{SprintID: g.Sprint, State: g.State, Priority: g.Priority, OrderGeneration: gen}
	if previous != "" {
		v, e := f.ParseID[c.Task](previous)
		if e != nil {
			return p, internal(e)
		}
		p.PreviousID = &v
	}
	if next != "" {
		v, e := f.ParseID[c.Task](next)
		if e != nil {
			return p, internal(e)
		}
		p.NextID = &v
	}
	return p, nil
}

func (s *TaskTransitionService) evaluateTransition(ctx context.Context, tx f.Tx, x postgres.SQLExecutor, actor i.Actor, r *transitionRecord, discovered c.Task, at f.Instant, ids []c.TaskEventID, eventID event.EventID) (transitionPlan, error) {
	var zero transitionPlan
	in := r.Input
	before, err := loadTask(ctx, x, in.Project, in.Task)
	if err != nil {
		return zero, err
	}
	if before.Version != in.Expected {
		return zero, field(f.TaskVersionConflict, "/expected_version", "STALE_VERSION")
	}
	if !sameValue(before, discovered) {
		return zero, errReplan
	}
	if before.State.Terminal() {
		return zero, fault(f.TaskTerminalImmutable)
	}
	if err = c.CheckTaskTransitionRule(c.TaskTransitionRuleInput{FromState: before.State, ToState: in.Request.TargetState, Role: c.TaskTransitionHumanOwner, CurrentAssigneeAgentID: before.AssigneeAgentID}); err != nil {
		return zero, err
	}
	// This first writer owns precisely this public edge. Other formally valid
	// edges retain their own unbound dependencies instead of borrowing it.
	if before.State != c.TaskStateBacklog || in.Request.TargetState != c.TaskStateTodo {
		return zero, fault(f.DependencyUnbound)
	}
	if in.Request.AssigneeAgentID == nil {
		return zero, fault(f.TaskAssigneeRequired)
	}
	if len(in.Request.AddBlockers) != 0 || len(in.Request.ResolveBlockerIDs) != 0 {
		return zero, fault(f.DependencyUnbound)
	}
	placement, err := s.state().deps.Structure.ReadPlacementInTx(ctx, tx, actor, in.Project, before.SprintID)
	if err != nil {
		return zero, err
	}
	if placement.Milestone.ID != before.MilestoneID {
		return zero, internal(nil)
	}
	if placement.Sprint.State == c.Completed {
		return zero, field(f.TaskSprintInvalid, "/sprint_id", "COMPLETED_SPRINT")
	}
	agent, err := s.state().deps.Agents.RequireCurrentInTx(ctx, tx, actor, in.Project, *in.Request.AssigneeAgentID)
	if err != nil {
		return zero, portError(err)
	}
	if agent.Validate() != nil || agent.ProjectID != in.Project || agent.AgentID != *in.Request.AssigneeAgentID {
		return zero, internal(nil)
	}
	if before.AssigneeAgentID != nil && *before.AssigneeAgentID != agent.AgentID {
		old, e := s.state().deps.Agents.RequireCurrentInTx(ctx, tx, actor, in.Project, *before.AssigneeAgentID)
		if e != nil {
			return zero, portError(e)
		}
		if old.Validate() != nil || old.ProjectID != in.Project || old.AgentID != *before.AssigneeAgentID {
			return zero, internal(nil)
		}
	}
	occupancy, err := s.state().deps.Occupancy.ReadInTx(ctx, tx, in.Project, []string{in.Task.String()})
	if err != nil {
		return zero, portError(err)
	}
	if occupancy.Active == nil || occupancy.HistoryTaskIDs == nil {
		return zero, internal(nil)
	}
	if len(occupancy.Active) != 0 {
		return zero, fault(f.ResourceBusy)
	}
	pending, err := s.state().deps.Pending.ReadInTx(ctx, tx, in.Project, []string{in.Task.String()})
	if err != nil {
		return zero, portError(err)
	}
	if pending.Pending == nil || pending.HistoryTaskIDs == nil {
		return zero, internal(nil)
	}
	if len(pending.Pending) != 0 {
		return zero, fault(f.ResourceBusy)
	}
	var blockers int64
	if err = x.QueryRow(ctx, `SELECT count(*) FROM agenteam_work.task_blockers WHERE project_id=$1 AND task_id=$2 AND resolved_at IS NULL`, in.Project.String(), in.Task.String()).Scan(&blockers); err != nil {
		return zero, taskSQL(err)
	}
	if blockers < 0 {
		return zero, internal(nil)
	}
	if blockers > 0 {
		return zero, field(f.InvalidState, "/task_id", "UNRESOLVED_BLOCKERS")
	}
	source := groupForTask(before)
	target := source
	target.State = in.Request.TargetState
	if err = s.state().deps.Pending.RequireNoPendingGroupsInTx(ctx, tx, in.Project, pendingGroups(source, target)); err != nil {
		return zero, portError(err)
	}
	oldRows, oldGen, err := loadTaskRanks(ctx, x, in.Project, source)
	if err != nil {
		return zero, err
	}
	rows, gen, err := loadTaskRanks(ctx, x, in.Project, target)
	if err != nil {
		return zero, err
	}
	if len(rows) >= taskGroupCap {
		return zero, fault(f.ResourceBusy)
	}
	oldNext, err := transitionNext(oldGen)
	if err != nil {
		return zero, err
	}
	next, err := transitionNext(gen)
	if err != nil {
		return zero, err
	}
	version, err := transitionNext(int64(before.Version))
	if err != nil {
		return zero, err
	}
	query, err := loadTaskQueryGeneration(ctx, x, in.Project)
	if err != nil {
		return zero, err
	}
	if _, err = transitionNext(query); err != nil {
		return zero, err
	}
	sourceAfter := make([]rankItem, 0, len(oldRows))
	found := -1
	for n, v := range oldRows {
		if v.ID == in.Task.String() {
			found = n
			if v.Rank != before.ManualRank {
				return zero, internal(nil)
			}
		} else {
			sourceAfter = append(sourceAfter, v)
		}
	}
	if found < 0 {
		return zero, internal(nil)
	}
	var prev, following string
	if found > 0 {
		prev = oldRows[found-1].ID
	}
	if found+1 < len(oldRows) {
		following = oldRows[found+1].ID
	}
	sp, err := transitionPosition(source, oldNext, prev, following)
	if err != nil {
		return zero, err
	}
	ranks, err := rankFor(rows, in.Task.String(), "", true)
	if err != nil {
		return zero, err
	}
	tp, err := transitionPosition(target, next, ranks.Previous, ranks.Next)
	if err != nil {
		return zero, err
	}
	after := before.Clone()
	after.State = in.Request.TargetState
	newAgent := agent.AgentID
	after.AssigneeAgentID = &newAgent
	after.ManualRank = ranks.Rank
	after.Version = f.Version(version)
	if at.Time().Before(before.UpdatedAt.Time()) {
		at = before.UpdatedAt
	}
	after.UpdatedAt = at
	count := 1
	changed := before.AssigneeAgentID == nil || *before.AssigneeAgentID != newAgent
	if changed {
		count++
	}
	if in.Request.Comment != nil {
		count++
	}
	if len(ids) != count {
		return zero, internal(nil)
	}
	history := make([]c.TaskTransitionEvent, 0, count)
	add := func(kind c.TaskTransitionEventType, payload c.TaskTransitionFactPayload) {
		history = append(history, c.TaskTransitionEvent{ID: ids[len(history)], ProjectID: in.Project, TaskID: in.Task, TaskVersion: after.Version, Type: kind, Actor: c.TaskTransitionActor{UserID: in.User}, OperationID: r.ID, CorrelationID: r.ID, Payload: payload, CreatedAt: at})
	}
	add(c.TaskTransitionStateChanged, c.TaskTransitionFactPayload{StateChanged: &c.TaskStateChangedPayload{FromState: before.State, ToState: after.State}})
	if changed {
		add(c.TaskTransitionAssigneeChanged, c.TaskTransitionFactPayload{AssigneeChanged: &c.TaskAssigneeChangedPayload{FromAgentID: before.AssigneeAgentID, ToAgentID: newAgent}})
	}
	if in.Request.Comment != nil {
		add(c.TaskTransitionComment, c.TaskTransitionFactPayload{Comment: &c.TaskCommentPayload{Body: *in.Request.Comment}})
	}
	header, err := taskHeader(eventID, after, at)
	if err != nil {
		return zero, err
	}
	header.EventType = c.TaskTransitionedName
	header.SchemaVersion = c.TaskTransitionSchemaVersion
	out, ev, err := s.state().deps.TaskEvents.NewTaskTransitionData(header, before, after, in.Request, history, sp, tp)
	if err != nil {
		return zero, internal(err)
	}
	if err = ctx.Err(); err != nil {
		return zero, canceled(err)
	}
	return transitionPlan{Before: before, After: out, Placement: taskPlacement{placement.Milestone.ID, placement.Sprint.ID, placement.Sprint.State}, Agent: agent, Groups: []taskGroupPlan{{source, oldRows, sourceAfter, oldGen}, {target, rows, ranks.Items, gen}}, QueryGeneration: query, History: history, Source: sp, Target: tp, Header: ev.Header(), Payload: ev.PayloadBytes()}, nil
}
