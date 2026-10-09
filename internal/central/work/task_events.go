package work

import (
	"context"
	"encoding/json"
	"slices"
	"time"

	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	c "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
)

const taskPurpose = "work.task-planning.append-v1"

type taskOpaque struct {
	Kind        string          `json:"kind"`
	CommandID   c.TaskCommandID `json:"command_id"`
	Revision    f.Version       `json:"plan_revision"`
	TaskEventID c.TaskEventID   `json:"task_event_id"`
}

func taskEventTriple(s event.Summary) bool {
	return s.Header.EventType == c.TaskChangedName && s.Header.AggregateType == c.TaskAggregate && s.Header.SchemaVersion == c.TaskSchemaVersion
}
func taskEventProject(s event.Summary) (c.ProjectID, error) {
	h := s.Header
	if !taskEventTriple(s) || s.Producer != c.WorkProducer || h.Validate() != nil || s.PayloadDigest.Validate() != nil || h.Scope.Kind != event.ProjectScope || h.AggregateVersion == nil || h.AggregateSequence != nil {
		return c.ProjectID{}, fault(f.Forbidden)
	}
	p, e := f.ParseID[i.Project](h.Scope.ProjectID.String())
	if e != nil {
		return c.ProjectID{}, fault(f.Forbidden)
	}
	return p, nil
}

// Validate the immutable intent, preimage, rank calculation and safe history.
// This intentionally does not consult today's mutable task for historical replay.
func validateTaskRecord(r *taskRecord, a i.Actor) error {
	if r == nil || r.User.String() != a.Details().UserID {
		return internal(nil)
	}
	semantic, err := r.Input.semantic(a, r.Key)
	if err != nil || semantic != r.Semantic {
		return internal(err)
	}
	if r.Receipt != nil {
		out := r.Receipt
		if out.Validate() != nil || out.Task.ID != r.Input.Target || out.Task.ProjectID != r.Project {
			return internal(nil)
		}
	}
	if r.Plan == nil {
		if r.State != "completed" || r.Receipt == nil || r.Receipt.Changed || r.Input.Create != nil || r.Input.Expected == nil || r.Receipt.Task.Version != *r.Input.Expected {
			return internal(nil)
		}
		t := r.Receipt.Task
		if t.State != c.TaskStateBacklog || t.AssigneeAgentID != nil {
			return internal(nil)
		}
		if u := r.Input.Update; u != nil && (u.Title != nil && *u.Title != t.Title || u.Description != nil && *u.Description != t.Description || u.Type != nil && *u.Type != t.Type || u.Priority != nil && *u.Priority != t.Priority || u.Plan != nil && *u.Plan != t.Plan) {
			return internal(nil)
		}
		return nil
	}
	p := r.Plan
	out := p.After
	t := out.Task
	in := r.Input
	if r.EventID == nil || r.TaskEventID == nil || !out.Changed || out.Validate() != nil || out.TaskEventID == nil || *out.TaskEventID != *r.TaskEventID || out.EventIDs[0] != *r.EventID || t.ID != in.Target || t.ProjectID != r.Project || p.Placement.Milestone != t.MilestoneID || p.Placement.Sprint != t.SprintID || (p.Placement.State != c.Planned && p.Placement.State != c.Current) || p.QueryGeneration < 1 {
		return internal(nil)
	}
	if _, err = taskCounter(p.QueryGeneration); err != nil {
		return internal(err)
	}
	if r.Receipt != nil && !sameValue(*r.Receipt, out) {
		return internal(nil)
	}
	h, err := taskHeader(*r.EventID, t, t.UpdatedAt)
	if err != nil || !sameValue(h, p.Header) {
		return internal(err)
	}
	var expected c.Task
	fields := []c.TaskChangedField{}
	var typeChange *c.TaskTypeChange
	var priorityChange *c.TaskPriorityChange
	change := c.TaskUpdatedChange
	if in.Create != nil {
		v := in.Create
		if p.Before != nil || v.EffectiveState() != c.TaskStateBacklog || v.AssigneeAgentID != nil || t.Version != 1 || t.SprintID != v.SprintID || !sameValue(t.CreatedAt, t.UpdatedAt) {
			return internal(nil)
		}
		expected = c.Task{ID: in.Target, ProjectID: in.Project, MilestoneID: p.Placement.Milestone, SprintID: v.SprintID, Title: v.Title, Description: v.Description, Type: v.Type, Priority: v.Priority, State: c.TaskStateBacklog, Plan: v.Plan, ManualRank: t.ManualRank, Version: 1, CreatedAt: t.CreatedAt, UpdatedAt: t.UpdatedAt}
		fields = []c.TaskChangedField{c.TaskDescriptionChanged, c.TaskRankChanged, c.TaskPlanChanged, c.TaskPriorityChanged, c.TaskTitleChanged, c.TaskTypeChanged}
		change = c.TaskCreatedChange
	} else {
		if p.Before == nil || p.Before.Validate() != nil || in.Expected == nil || p.Before.Version != *in.Expected || p.Before.State != c.TaskStateBacklog || p.Before.AssigneeAgentID != nil || p.Before.ID != in.Target || p.Before.ProjectID != in.Project || p.Before.SprintID != t.SprintID || p.Before.MilestoneID != t.MilestoneID || t.UpdatedAt.Time().Before(p.Before.UpdatedAt.Time()) {
			return internal(nil)
		}
		expected = p.Before.Clone()
		n, e := taskCounter(int64(expected.Version))
		if e != nil {
			return internal(e)
		}
		expected.Version = f.Version(n)
		expected.UpdatedAt = t.UpdatedAt
		if u := in.Update; u != nil {
			if u.Description != nil && *u.Description != expected.Description {
				expected.Description = *u.Description
				fields = append(fields, c.TaskDescriptionChanged)
			}
			if u.Plan != nil && *u.Plan != expected.Plan {
				expected.Plan = *u.Plan
				fields = append(fields, c.TaskPlanChanged)
			}
			if u.Priority != nil && *u.Priority != expected.Priority {
				priorityChange = &c.TaskPriorityChange{From: expected.Priority, To: *u.Priority}
				expected.Priority = *u.Priority
				expected.ManualRank = t.ManualRank
				fields = append(fields, c.TaskPriorityChanged)
			}
			if u.Title != nil && *u.Title != expected.Title {
				expected.Title = *u.Title
				fields = append(fields, c.TaskTitleChanged)
			}
			if u.Type != nil && *u.Type != expected.Type {
				typeChange = &c.TaskTypeChange{From: expected.Type, To: *u.Type}
				expected.Type = *u.Type
				fields = append(fields, c.TaskTypeChanged)
			}
		} else {
			expected.ManualRank = t.ManualRank
			fields = []c.TaskChangedField{c.TaskRankChanged}
			change = c.TaskReorderedChange
		}
	}
	if len(fields) == 0 || !sameValue(expected, t) {
		return internal(nil)
	}
	var position *c.TaskPosition
	ordered := in.Create != nil || in.Reorder != nil || priorityChange != nil
	count := 0
	if ordered {
		count = 1
	}
	if priorityChange != nil {
		count = 2
	}
	if len(p.Groups) != count {
		return internal(nil)
	}
	if ordered {
		g := p.Groups[0]
		if g.Group != groupForTask(t) || len(g.Before) > taskGroupCap || len(g.After) > taskGroupCap {
			return internal(nil)
		}
		before := ""
		if in.Reorder != nil && in.Reorder.BeforeID != nil {
			before = in.Reorder.BeforeID.String()
		}
		ranks, e := rankFor(g.Before, in.Target.String(), before, in.Create != nil || priorityChange != nil)
		if e != nil || !ranks.Changed || !sameValue(ranks.Items, g.After) || ranks.Rank != t.ManualRank {
			return internal(e)
		}
		next, e := taskCounter(g.Generation)
		if e != nil {
			return internal(e)
		}
		position = &c.TaskPosition{SprintID: t.SprintID, State: t.State, Priority: t.Priority, OrderGeneration: next}
		if ranks.Previous != "" {
			v, e := f.ParseID[c.Task](ranks.Previous)
			if e != nil {
				return internal(e)
			}
			position.PreviousID = &v
		}
		if ranks.Next != "" {
			v, e := f.ParseID[c.Task](ranks.Next)
			if e != nil {
				return internal(e)
			}
			position.NextID = &v
		}
		if priorityChange != nil {
			g = p.Groups[1]
			if g.Group != groupForTask(*p.Before) || len(g.Before) > taskGroupCap || len(g.After) > taskGroupCap {
				return internal(nil)
			}
			if _, e = taskCounter(g.Generation); e != nil {
				return internal(e)
			}
			remaining := []rankItem{}
			found := false
			for n, v := range g.Before {
				if c.ValidateRank(v.Rank) != nil || n > 0 && g.Before[n-1].Rank >= v.Rank {
					return internal(nil)
				}
				if v.ID == in.Target.String() {
					if found || v.Rank != p.Before.ManualRank {
						return internal(nil)
					}
					found = true
					continue
				}
				if _, e = f.ParseID[c.Task](v.ID); e != nil {
					return internal(e)
				}
				remaining = append(remaining, v)
			}
			if !found || !sameValue(remaining, g.After) {
				return internal(nil)
			}
		}
	}
	wanted := c.TaskChanged{CommandID: r.ID, ActorUserID: r.User, TaskEventID: *r.TaskEventID, MilestoneID: t.MilestoneID, SprintID: t.SprintID, Change: change, ChangedFields: fields, Position: position}
	var payload c.TaskChanged
	if err = json.Unmarshal(p.Payload, &payload); err != nil || !sameValue(wanted, payload) {
		return internal(err)
	}
	history := c.TaskEvent{ID: *r.TaskEventID, ProjectID: r.Project, TaskID: in.Target, TaskVersion: t.Version, Actor: c.TaskEventActor{Type: i.Human, UserID: r.User, Source: "task_domain"}, OperationID: r.ID, CorrelationID: r.ID, CreatedAt: t.UpdatedAt}
	if in.Create != nil {
		history.Type = c.TaskEventCreated
		history.Payload, err = canonical(c.TaskCreatedPayload{InitialState: c.TaskStateBacklog, MilestoneID: t.MilestoneID, SprintID: t.SprintID, Type: t.Type, Priority: t.Priority})
	} else {
		history.Type = c.TaskEventFieldsUpdated
		history.Payload, err = canonical(c.TaskFieldsUpdatedPayload{ChangedFields: fields, TypeChange: typeChange, PriorityChange: priorityChange, Position: position})
	}
	if err != nil {
		return internal(err)
	}
	var recorded c.TaskEvent
	if err = json.Unmarshal(p.TaskEvent, &recorded); err != nil || !sameValue(history, recorded) {
		return internal(err)
	}
	return nil
}
func taskEventBinding(r *taskRecord, a i.Actor, summary event.Summary) (f.Digest, []f.LockRequest, []byte, error) {
	if c.ValidateActor(a) != nil || r == nil || r.Plan == nil || r.EventID == nil || r.TaskEventID == nil || r.User.String() != a.Details().UserID {
		return "", nil, nil, fault(f.Forbidden)
	}
	if _, err := taskEventProject(summary); err != nil {
		return "", nil, nil, err
	}
	if err := validateTaskRecord(r, a); err != nil {
		return "", nil, nil, err
	}
	raw, err := cursorPayload(r.Plan.Payload)
	if err != nil {
		return "", nil, nil, err
	}
	if !sameValue(r.Plan.Header, summary.Header) || digest(raw) != summary.PayloadDigest || *r.EventID != summary.Header.EventID {
		return "", nil, nil, fault(f.Forbidden)
	}
	source := groupForTask(r.Plan.After.Task)
	if r.Plan.Before != nil {
		source = groupForTask(*r.Plan.Before)
	}
	locks, err := r.Input.locks(r.Key, source)
	if err != nil {
		return "", nil, nil, err
	}
	identity, err := taskIdentity(r.Project, r.Command, r.Key)
	if err != nil {
		return "", nil, nil, err
	}
	type lock struct {
		Key  string     `json:"key"`
		Mode f.LockMode `json:"mode"`
	}
	projected := make([]lock, len(locks))
	for n, v := range locks {
		projected[n] = lock{v.Key.Canonical(), v.Mode}
	}
	bytes, err := canonical(struct {
		Purpose     string          `json:"purpose"`
		Actor       i.ActorDetails  `json:"actor"`
		Summary     event.Summary   `json:"summary"`
		Identity    string          `json:"identity"`
		CommandID   c.TaskCommandID `json:"command_id"`
		Revision    f.Version       `json:"plan_revision"`
		TaskEventID c.TaskEventID   `json:"task_event_id"`
		Stages      [2]oc.Stage     `json:"stages"`
		Locks       []lock          `json:"locks"`
	}{taskPurpose, a.Details(), summary, identity.Canonical(), r.ID, r.Revision, *r.TaskEventID, [2]oc.Stage{oc.CurrentAccess, oc.NewFact}, projected})
	if err != nil {
		return "", nil, nil, err
	}
	opaque, err := canonical(taskOpaque{"task_planning", r.ID, r.Revision, *r.TaskEventID})
	if err != nil {
		return "", nil, nil, err
	}
	return digest(bytes), locks, opaque, nil
}
func (a *Authority) discoverTaskAppend(ctx context.Context, actor i.Actor, summary event.Summary) (oc.Dependencies, error) {
	st := a.state()
	if st == nil {
		return oc.Dependencies{}, fault(f.DependencyUnbound)
	}
	project, err := taskEventProject(summary)
	if err != nil {
		return oc.Dependencies{}, err
	}
	if err = readInput(ctx, actor, project); err != nil {
		return oc.Dependencies{}, err
	}
	cause, err := readCause("task-append")
	if err != nil {
		return oc.Dependencies{}, err
	}
	var out oc.Dependencies
	result := st.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		locks, err := taskNormalize([]f.LockRequest{userLock(actor.Details().UserID, f.Shared), projectLock(project, f.Shared)})
		if err != nil {
			return err
		}
		if err = st.store.AcquireAll(ctx, tx, locks); err != nil {
			return portError(err)
		}
		x, err := st.store.InTx(tx)
		if err != nil {
			return portError(err)
		}
		access, err := st.projects.RequireOwnerInTx(ctx, tx, actor, project, i.Read)
		if err != nil {
			return portError(err)
		}
		if access.Project().ID != project {
			return internal(nil)
		}
		r, err := loadTaskEventCommand(ctx, x, summary.Header.EventID)
		if err != nil {
			return err
		}
		binding, locks, opaque, err := taskEventBinding(r, actor, summary)
		if err != nil {
			return err
		}
		out, err = oc.NewDependencies(st.issuer, binding, locks, opaque)
		return portError(err)
	})
	if err = taskTxError(ctx, result); err != nil {
		return oc.Dependencies{}, err
	}
	return out, nil
}
func (a *Authority) validateTaskAppendInTx(ctx context.Context, tx f.Tx, actor i.Actor, summary event.Summary, deps oc.Dependencies, stage oc.Stage) error {
	st := a.state()
	if st == nil {
		return fault(f.DependencyUnbound)
	}
	x, err := st.store.InTx(tx)
	if err != nil {
		return portError(err)
	}
	if !stage.Valid() || c.ValidateActor(actor) != nil || deps.Validate() != nil || !deps.Matches(st.issuer, deps.Binding()) {
		return fault(f.Forbidden)
	}
	project, err := taskEventProject(summary)
	if err != nil {
		return err
	}
	opaque, err := decodePrivate[taskOpaque](deps.Opaque(), 16384, []string{"kind", "command_id", "plan_revision", "task_event_id"})
	if err != nil || opaque.Kind != "task_planning" || opaque.CommandID.Validate() != nil || opaque.TaskEventID.Validate() != nil || opaque.Revision.Validate() != nil {
		return fault(f.Forbidden)
	}
	if err = st.store.RequireHeldLocks(ctx, tx, deps.Locks()); err != nil {
		return portError(err)
	}
	if err = st.store.RequireHeldLocks(ctx, tx, []f.LockRequest{userLock(actor.Details().UserID, f.Shared), projectLock(project, f.Shared), taskScheduleLock(project, f.Exclusive), taskLock(summary.Header.AggregateID.String(), f.Exclusive)}); err != nil {
		return portError(err)
	}
	access, err := st.projects.RequireOwnerInTx(ctx, tx, actor, project, i.Read)
	if err != nil {
		return portError(err)
	}
	if access.Project().ID != project {
		return internal(nil)
	}
	r, err := loadTaskEventCommand(ctx, x, summary.Header.EventID)
	if err != nil {
		return err
	}
	binding, locks, expectedOpaque, err := taskEventBinding(r, actor, summary)
	if err != nil {
		return err
	}
	if !deps.Matches(st.issuer, binding) || !sameTaskLocks(deps.Locks(), locks) || !slices.Equal(deps.Opaque(), expectedOpaque) || r.ID != opaque.CommandID || r.Revision != opaque.Revision || *r.TaskEventID != opaque.TaskEventID || r.Project != project {
		return fault(f.Forbidden)
	}
	if err = st.store.RequireHeldLocks(ctx, tx, locks); err != nil {
		return portError(err)
	}
	if stage == oc.CurrentAccess {
		return nil
	}
	if r.State != "planned" {
		return fault(f.Forbidden)
	}
	access, err = st.projects.RequireOwnerInTx(ctx, tx, actor, project, i.Mutate)
	if err != nil {
		return portError(err)
	}
	if access.Project().ID != project {
		return internal(nil)
	}
	if err = checkPointer(ctx, x, access.Project()); err != nil {
		return err
	}
	sprint, err := loadSprint(ctx, x, project, r.Plan.Placement.Sprint, access.Project().CurrentSprintID)
	if err != nil {
		return err
	}
	if sprint.State != r.Plan.Placement.State || sprint.MilestoneID != r.Plan.Placement.Milestone {
		return fault(f.Forbidden)
	}
	if _, err = loadMilestone(ctx, x, project, sprint.MilestoneID); err != nil {
		return err
	}
	return verifyTaskPostimage(ctx, x, r)
}
func sameTaskLocks(a, b []f.LockRequest) bool {
	return slices.EqualFunc(a, b, func(x, y f.LockRequest) bool { return f.CompareLockKeys(x.Key, y.Key) == 0 && x.Mode == y.Mode })
}
func verifyTaskPostimage(ctx context.Context, x postgres.SQLExecutor, r *taskRecord) error {
	p := r.Plan
	current, err := loadTask(ctx, x, r.Project, r.Input.Target)
	if err != nil {
		return err
	}
	if !sameValue(current, p.After.Task) {
		return fault(f.Forbidden)
	}
	gen, err := loadTaskQueryGeneration(ctx, x, r.Project)
	if err != nil {
		return err
	}
	next, err := taskCounter(p.QueryGeneration)
	if err != nil || gen != next {
		return fault(f.Forbidden)
	}
	for _, g := range p.Groups {
		ranks, gen, err := loadTaskRanks(ctx, x, r.Project, g.Group)
		if err != nil {
			return err
		}
		next, err := taskCounter(g.Generation)
		if err != nil || gen != next || !sameValue(ranks, g.After) {
			return fault(f.Forbidden)
		}
	}
	var id, project, task, operation, correlation string
	var version f.Version
	var kind c.TaskEventType
	var actor, payload []byte
	var created time.Time
	err = x.QueryRow(ctx, `SELECT id::text,project_id::text,task_id::text,task_version,type,actor,operation_id::text,correlation_id::text,payload,created_at FROM agenteam_work.task_events WHERE id=$1`, r.TaskEventID.String()).Scan(&id, &project, &task, &version, &kind, &actor, &operation, &correlation, &payload, &created)
	if err != nil {
		return taskSQL(err)
	}
	var expected c.TaskEvent
	if err = json.Unmarshal(p.TaskEvent, &expected); err != nil {
		return internal(err)
	}
	var actualActor c.TaskEventActor
	if err = json.Unmarshal(actor, &actualActor); err != nil {
		return internal(err)
	}
	var actualPayload any
	if err = json.Unmarshal(payload, &actualPayload); err != nil {
		return internal(err)
	}
	var expectedPayload any
	if err = json.Unmarshal(expected.Payload, &expectedPayload); err != nil {
		return internal(err)
	}
	if id != expected.ID.String() || project != expected.ProjectID.String() || task != expected.TaskID.String() || version != expected.TaskVersion || kind != expected.Type || !sameValue(actualActor, expected.Actor) || operation != expected.OperationID.String() || correlation != expected.CorrelationID.String() || !sameValue(actualPayload, expectedPayload) || !created.Equal(expected.CreatedAt.Time()) {
		return fault(f.Forbidden)
	}
	return nil
}
