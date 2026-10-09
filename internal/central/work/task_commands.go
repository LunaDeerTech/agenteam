package work

import (
	"context"
	"errors"
	"time"

	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	c "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
	"github.com/jackc/pgx/v5/pgconn"
)

func (s *TaskService) CreateTask(ctx context.Context, a i.Actor, m f.CommandMeta, p c.ProjectID, r c.TaskCreate) (c.TaskMutation, error) {
	return s.executeTask(ctx, a, m, taskInput{Command: taskCreate, Project: p, Target: r.TaskID, Create: &r})
}
func (s *TaskService) UpdateTask(ctx context.Context, a i.Actor, m f.CommandMeta, p c.ProjectID, id c.TaskID, r c.TaskFieldsUpdate) (c.TaskMutation, error) {
	return s.executeTask(ctx, a, m, taskInput{Command: taskUpdate, Project: p, Target: id, Update: &r})
}
func (s *TaskService) ReorderTask(ctx context.Context, a i.Actor, m f.CommandMeta, p c.ProjectID, id c.TaskID, r c.TaskReorder) (c.TaskMutation, error) {
	return s.executeTask(ctx, a, m, taskInput{Command: taskReorder, Project: p, Target: id, Reorder: &r})
}

func (s *TaskService) executeTask(ctx context.Context, a i.Actor, m f.CommandMeta, in taskInput) (c.TaskMutation, error) {
	ctx, entry, done, err := s.begin(ctx)
	if err != nil {
		return c.TaskMutation{}, err
	}
	defer done()
	if err = readInput(ctx, a, in.Project); err != nil {
		return c.TaskMutation{}, err
	}
	if m.Validate() != nil {
		return c.TaskMutation{}, fault(f.InvalidArgument)
	}
	in.User, err = f.ParseID[i.User](a.Details().UserID)
	if err != nil {
		return c.TaskMutation{}, fault(f.Unauthenticated)
	}
	in.Expected = m.ExpectedVersion
	semantic, err := in.semantic(a, m.IdempotencyKey)
	if err != nil {
		return c.TaskMutation{}, err
	}
	raw, err := canonical(in)
	if err != nil {
		return c.TaskMutation{}, err
	}
	in, err = decodePrivate[taskInput](raw, taskRequestCap, taskInputFields)
	if err != nil {
		return c.TaskMutation{}, err
	}
	key := m.IdempotencyKey
	id, err := taskIdentity(in.Project, in.Command, key)
	if err != nil {
		return c.TaskMutation{}, err
	}
	q := c.TaskCommandLookupRequest{ProjectID: in.Project, Command: in.Command, IdempotencyKey: key, SemanticDigest: semantic}
	for round := 0; round < 3; round++ {
		var source taskGroup
		if in.Create != nil {
			source = taskGroup{in.Create.SprintID, c.TaskStateBacklog, in.Create.Priority}
		} else {
			var replay *c.TaskMutation
			discovery, err := taskNormalize([]f.LockRequest{commandLock(id), userLock(in.User.String(), f.Shared), projectLock(in.Project, f.Shared), taskScheduleLock(in.Project, f.Exclusive), taskLock(in.Target.String(), f.Shared)})
			if err != nil {
				return c.TaskMutation{}, err
			}
			result := s.state().store.WithinTx(ctx, commandCause(id), func(ctx context.Context, tx f.Tx) error {
				if err := s.state().store.AcquireAll(ctx, tx, discovery); err != nil {
					return portError(err)
				}
				x, _, r, e := s.taskCurrent(ctx, tx, a, q)
				if e != nil {
					return e
				}
				if r != nil {
					replay = r
					return nil
				}
				if _, e = s.state().deps.Authority.state().projects.RequireOwnerInTx(ctx, tx, a, in.Project, i.Mutate); e != nil {
					return portError(e)
				}
				t, e := loadTask(ctx, x, in.Project, in.Target)
				if e != nil {
					return e
				}
				if t.Version != *in.Expected {
					return field(f.TaskVersionConflict, "/expected_version", "STALE_VERSION")
				}
				source = groupForTask(t)
				return nil
			})
			if result.State() == f.Unknown {
				return s.confirmTaskUnknown(ctx, entry, a, q, result)
			}
			if err = taskTxError(ctx, result); err != nil {
				return c.TaskMutation{}, err
			}
			if replay != nil {
				return replay.Clone(), nil
			}
		}
		base, err := in.locks(key, source)
		if err != nil {
			return c.TaskMutation{}, err
		}
		var prepared *taskRecord
		var completed *c.TaskMutation
		replan := false
		result := s.state().store.WithinTx(ctx, commandCause(id), func(ctx context.Context, tx f.Tx) error {
			if err := s.state().store.AcquireAll(ctx, tx, base); err != nil {
				return portError(err)
			}
			x, record, replay, err := s.taskCurrent(ctx, tx, a, q)
			if err != nil {
				return err
			}
			if replay != nil {
				completed = replay
				return nil
			}
			if _, err = s.state().deps.Authority.state().projects.RequireOwnerInTx(ctx, tx, a, in.Project, i.Mutate); err != nil {
				return portError(err)
			}
			at, err := dbNow(ctx, x)
			if err != nil {
				return err
			}
			insert := record == nil
			if insert {
				rid, e := f.NewID[c.TaskCommand]()
				if e != nil {
					return unavailable(e)
				}
				record = &taskRecord{ID: rid, Project: in.Project, User: in.User, Command: in.Command, Key: key, Semantic: semantic, Input: in, Revision: 1, State: "planned", Created: at}
			} else {
				n, e := taskCounter(int64(record.Revision))
				if e != nil {
					return e
				}
				record.Revision = f.Version(n)
			}
			te, err := f.NewID[c.TaskEvent]()
			if err != nil {
				return unavailable(err)
			}
			ev, err := f.NewID[event.EventIdentity]()
			if err != nil {
				return unavailable(err)
			}
			evaluation, err := s.evaluateTask(ctx, tx, x, a, record, source, at, te, ev)
			if errors.Is(err, errReplan) {
				replan = true
			}
			if err != nil {
				return err
			}
			if !evaluation.After.Changed {
				if !insert {
					record.Revision--
				}
				if err = completeTaskCommand(ctx, x, record, evaluation.After, insert); err != nil {
					return err
				}
				if err = s.state().deps.Activity.TouchActivityInTx(ctx, tx, a); err != nil {
					return portError(err)
				}
				v := evaluation.After.Clone()
				completed = &v
				return nil
			}
			record.Plan = evaluation
			record.TaskEventID = &te
			record.EventID = &ev
			if err = storeTaskPlan(ctx, x, record, insert); err != nil {
				return err
			}
			prepared = record
			return nil
		})
		if result.State() == f.Unknown {
			return s.confirmTaskUnknown(ctx, entry, a, q, result)
		}
		if result.State() == f.NotCommitted && replan {
			if ctx.Err() != nil {
				return c.TaskMutation{}, canceled(ctx.Err())
			}
			continue
		}
		if err = taskTxError(ctx, result); err != nil {
			return c.TaskMutation{}, mapTaskConflict(err, in)
		}
		if completed != nil {
			return completed.Clone(), nil
		}
		if prepared == nil || prepared.Plan == nil {
			return c.TaskMutation{}, internal(nil)
		}
		ev, err := s.state().deps.TaskEvents.Restore(prepared.Plan.Header, prepared.Plan.Payload)
		if err != nil {
			return c.TaskMutation{}, internal(err)
		}
		appendPlan, err := s.state().deps.Events.PrepareAppend(ctx, a, ev)
		if err != nil {
			return c.TaskMutation{}, portError(err)
		}
		locks, err := taskNormalize(append(append([]f.LockRequest{}, base...), appendPlan.Locks()...))
		if err != nil {
			return c.TaskMutation{}, portError(err)
		}
		var out c.TaskMutation
		replan = false
		result = s.state().store.WithinTx(ctx, commandCause(id), func(ctx context.Context, tx f.Tx) error {
			if err := s.state().store.AcquireAll(ctx, tx, locks); err != nil {
				return portError(err)
			}
			x, record, replay, err := s.taskCurrent(ctx, tx, a, q)
			if err != nil {
				return err
			}
			if replay != nil {
				out = replay.Clone()
				return nil
			}
			if _, err = s.state().deps.Authority.state().projects.RequireOwnerInTx(ctx, tx, a, in.Project, i.Mutate); err != nil {
				return portError(err)
			}
			if record == nil || record.Plan == nil || record.ID != prepared.ID || record.Revision != prepared.Revision || record.EventID == nil || *record.EventID != *prepared.EventID || record.TaskEventID == nil || *record.TaskEventID != *prepared.TaskEventID {
				replan = true
				return errReplan
			}
			current, err := s.evaluateTask(ctx, tx, x, a, record, source, record.Plan.Header.OccurredAt, *record.TaskEventID, *record.EventID)
			if errors.Is(err, errReplan) {
				replan = true
			}
			if err != nil {
				return err
			}
			if !sameValue(current, prepared.Plan) {
				replan = true
				return errReplan
			}
			if err = applyTaskPlan(ctx, x, record); err != nil {
				return err
			}
			receipt, err := s.state().deps.Events.AppendEventInTx(ctx, tx, a, ev, appendPlan)
			if err != nil {
				return portError(err)
			}
			if receipt.EventID != *record.EventID || receipt.Sequence.Validate() != nil {
				return internal(nil)
			}
			if err = completeTaskCommand(ctx, x, record, current.After, false); err != nil {
				return err
			}
			if err = s.state().deps.Activity.TouchActivityInTx(ctx, tx, a); err != nil {
				return portError(err)
			}
			out = current.After.Clone()
			return nil
		})
		if result.State() == f.Unknown {
			return s.confirmTaskUnknown(ctx, entry, a, q, result)
		}
		if result.State() == f.NotCommitted && replan {
			if ctx.Err() != nil {
				return c.TaskMutation{}, canceled(ctx.Err())
			}
			continue
		}
		if err = taskTxError(ctx, result); err != nil {
			return c.TaskMutation{}, mapTaskConflict(err, in)
		}
		if out.Validate() != nil {
			return c.TaskMutation{}, internal(nil)
		}
		return out.Clone(), nil
	}
	return c.TaskMutation{}, fault(f.ResourceBusy)
}
func taskTxError(ctx context.Context, result f.CommitResult) error {
	if result.State() == f.NotCommitted && ctx.Err() != nil {
		return canceled(ctx.Err())
	}
	return txError(result)
}
func mapTaskConflict(err error, in taskInput) error {
	var pg *pgconn.PgError
	if in.Command == taskCreate && errors.As(err, &pg) && pg.Code == "23505" && pg.SchemaName == "agenteam_work" && pg.ConstraintName == "tasks_pkey" {
		v := f.NewFault(f.ResourceBusy, f.NotCommitted)
		v.FieldErrors = []f.FieldError{{Path: "/task_id", Code: "TARGET_OCCUPIED"}}
		return v
	}
	return err
}
func (s *TaskService) taskCurrent(ctx context.Context, tx f.Tx, a i.Actor, q c.TaskCommandLookupRequest) (postgres.SQLExecutor, *taskRecord, *c.TaskMutation, error) {
	x, err := s.state().store.InTx(tx)
	if err != nil {
		return nil, nil, nil, portError(err)
	}
	access, err := s.state().deps.Authority.state().projects.RequireOwnerInTx(ctx, tx, a, q.ProjectID, i.Read)
	if err != nil {
		return nil, nil, nil, portError(err)
	}
	if access.Project().ID != q.ProjectID {
		return nil, nil, nil, internal(nil)
	}
	id, err := taskIdentity(q.ProjectID, q.Command, q.IdempotencyKey)
	if err != nil {
		return nil, nil, nil, err
	}
	r, err := loadTaskCommand(ctx, x, id)
	if err != nil {
		return nil, nil, nil, err
	}
	if r == nil {
		return x, nil, nil, nil
	}
	if r.User.String() != a.Details().UserID {
		return nil, nil, nil, fault(f.NotFound)
	}
	if r.Semantic != q.SemanticDigest {
		return nil, nil, nil, fault(f.IdempotencyKeyReused)
	}
	if err = validateTaskRecord(r, a); err != nil {
		return nil, nil, nil, err
	}
	if r.State == "completed" {
		v := r.Receipt.Clone()
		return x, r, &v, nil
	}
	return x, r, nil, nil
}
func (s *TaskService) LookupTaskCommand(ctx context.Context, a i.Actor, q c.TaskCommandLookupRequest) (c.TaskCommandLookup, error) {
	ctx, _, done, err := s.begin(ctx)
	if err != nil {
		return c.TaskCommandLookup{}, err
	}
	defer done()
	if err = readInput(ctx, a, q.ProjectID); err != nil {
		return c.TaskCommandLookup{}, err
	}
	if err = q.Validate(); err != nil {
		return c.TaskCommandLookup{}, err
	}
	return s.lookupTask(ctx, a, q)
}
func (s *TaskService) lookupTask(ctx context.Context, a i.Actor, q c.TaskCommandLookupRequest) (c.TaskCommandLookup, error) {
	id, err := taskIdentity(q.ProjectID, q.Command, q.IdempotencyKey)
	if err != nil {
		return c.TaskCommandLookup{}, err
	}
	locks, err := taskNormalize([]f.LockRequest{commandLock(id), userLock(a.Details().UserID, f.Shared), projectLock(q.ProjectID, f.Shared)})
	if err != nil {
		return c.TaskCommandLookup{}, err
	}
	var out c.TaskCommandLookup
	result := s.state().store.WithinTx(ctx, commandCause(id), func(ctx context.Context, tx f.Tx) error {
		if err := s.state().store.AcquireAll(ctx, tx, locks); err != nil {
			return portError(err)
		}
		_, r, replay, err := s.taskCurrent(ctx, tx, a, q)
		if err != nil {
			return err
		}
		switch {
		case replay != nil:
			out = c.TaskCommandLookup{Status: c.LookupCommitted, Receipt: replay}
		case r != nil:
			out = c.TaskCommandLookup{Status: c.LookupInProgress}
		default:
			out = c.TaskCommandLookup{Status: c.LookupNotObserved}
		}
		return nil
	})
	if err = taskTxError(ctx, result); err != nil {
		return c.TaskCommandLookup{}, err
	}
	return out.Clone(), nil
}
func (s *TaskService) confirmTaskUnknown(ctx context.Context, entry *call, a i.Actor, q c.TaskCommandLookupRequest, original f.CommitResult) (c.TaskMutation, error) {
	confirm, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
	token := &confirmation{cancel: cancel}
	st := s.state()
	st.mu.Lock()
	entry.confirmations[token] = struct{}{}
	if st.stopped {
		cancel()
	}
	st.mu.Unlock()
	defer func() { cancel(); st.mu.Lock(); delete(entry.confirmations, token); st.mu.Unlock() }()
	out, err := s.lookupTask(confirm, a, q)
	if err != nil {
		return c.TaskMutation{}, txError(original)
	}
	if out.Status == c.LookupCommitted && out.Receipt != nil {
		return out.Receipt.Clone(), nil
	}
	if out.Status == c.LookupNotObserved {
		return c.TaskMutation{}, notCommittedAfterUnknown(original)
	}
	return c.TaskMutation{}, txError(original)
}

// Planning never writes canonical rows. Its compact group vectors contain only
// identifiers and physical ranks, keeping maximum-size task text bounded.
func (s *TaskService) evaluateTask(ctx context.Context, tx f.Tx, x postgres.SQLExecutor, a i.Actor, r *taskRecord, source taskGroup, at f.Instant, te c.TaskEventID, ev event.EventID) (*taskPlan, error) {
	in := r.Input
	p := &taskPlan{Groups: []taskGroupPlan{}}
	var t c.Task
	if in.Create != nil {
		var occupied bool
		if err := x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_work.tasks WHERE id=$1)`, in.Target.String()).Scan(&occupied); err != nil {
			return nil, taskSQL(err)
		}
		if occupied {
			return nil, field(f.ResourceBusy, "/task_id", "TARGET_OCCUPIED")
		}
		v := in.Create
		t = c.Task{ID: in.Target, ProjectID: in.Project, SprintID: v.SprintID, Title: v.Title, Description: v.Description, Type: v.Type, Priority: v.Priority, State: c.TaskStateBacklog, Plan: v.Plan, Version: 1, CreatedAt: at, UpdatedAt: at}
	} else {
		v, err := loadTask(ctx, x, in.Project, in.Target)
		if err != nil {
			return nil, err
		}
		if v.Version != *in.Expected {
			return nil, field(f.TaskVersionConflict, "/expected_version", "STALE_VERSION")
		}
		if groupForTask(v) != source {
			return nil, errReplan
		}
		copy := v.Clone()
		p.Before = &copy
		t = v.Clone()
	}
	placement, err := s.state().deps.Structure.ReadPlacementInTx(ctx, tx, a, in.Project, t.SprintID)
	if err != nil {
		if isNotFound(err) {
			return nil, field(f.TaskSprintInvalid, "/sprint_id", "INVALID_SPRINT")
		}
		return nil, err
	}
	if p.Before != nil && t.MilestoneID != placement.Milestone.ID {
		return nil, internal(nil)
	}
	t.MilestoneID = placement.Milestone.ID
	p.Placement = taskPlacement{placement.Milestone.ID, placement.Sprint.ID, placement.Sprint.State}
	if placement.Sprint.State == c.Completed {
		return nil, field(f.TaskSprintInvalid, "/sprint_id", "COMPLETED_SPRINT")
	}
	if p.Before != nil {
		if t.State.Terminal() {
			return nil, fault(f.TaskTerminalImmutable)
		}
		if t.State != c.TaskStateBacklog || t.AssigneeAgentID != nil {
			return nil, fault(f.DependencyUnbound)
		}
		if at.Time().Before(t.UpdatedAt.Time()) {
			at = t.UpdatedAt
		}
	}
	if in.Create != nil {
		switch in.Create.EffectiveState() {
		case c.TaskStateBacklog:
		case c.TaskStateTodo:
			if in.Create.AssigneeAgentID == nil {
				return nil, fault(f.TaskAssigneeRequired)
			}
			return nil, fault(f.DependencyUnbound)
		default:
			return nil, fault(f.TaskStateInvalid)
		}
		if in.Create.AssigneeAgentID != nil {
			return nil, fault(f.DependencyUnbound)
		}
	}
	fields := []c.TaskChangedField{}
	var position *c.TaskPosition
	var typeChange *c.TaskTypeChange
	var priorityChange *c.TaskPriorityChange
	if in.Update != nil {
		u := in.Update
		if u.Description != nil && *u.Description != t.Description {
			t.Description = *u.Description
			fields = append(fields, c.TaskDescriptionChanged)
		}
		if u.Plan != nil && *u.Plan != t.Plan {
			t.Plan = *u.Plan
			fields = append(fields, c.TaskPlanChanged)
		}
		if u.Priority != nil && *u.Priority != t.Priority {
			priorityChange = &c.TaskPriorityChange{From: t.Priority, To: *u.Priority}
			t.Priority = *u.Priority
			fields = append(fields, c.TaskPriorityChanged)
		}
		if u.Title != nil && *u.Title != t.Title {
			t.Title = *u.Title
			fields = append(fields, c.TaskTitleChanged)
		}
		if u.Type != nil && *u.Type != t.Type {
			typeChange = &c.TaskTypeChange{From: t.Type, To: *u.Type}
			t.Type = *u.Type
			fields = append(fields, c.TaskTypeChanged)
		}
	}
	if in.Create != nil || in.Reorder != nil || priorityChange != nil {
		target := groupForTask(t)
		rows, generation, err := loadTaskRanks(ctx, x, in.Project, target)
		if err != nil {
			return nil, err
		}
		adding := in.Create != nil || priorityChange != nil
		if adding && len(rows) >= taskGroupCap {
			return nil, field(f.ResourceBusy, "/sprint_id", "GROUP_LIMIT")
		}
		if in.Create != nil {
			var count int64
			if err = x.QueryRow(ctx, `SELECT count(*) FROM agenteam_work.tasks WHERE project_id=$1`, in.Project.String()).Scan(&count); err != nil {
				return nil, taskSQL(err)
			}
			if count > taskProjectCap {
				return nil, internal(nil)
			}
			if count == taskProjectCap {
				return nil, field(f.ResourceBusy, "/project_id", "PROJECT_TASK_LIMIT")
			}
		}
		before := ""
		if in.Reorder != nil && in.Reorder.BeforeID != nil {
			before = in.Reorder.BeforeID.String()
		}
		ranks, err := rankFor(rows, in.Target.String(), before, adding)
		if err != nil {
			if isNotFound(err) {
				return nil, fault(f.TaskNotFound)
			}
			return nil, err
		}
		if ranks.Changed {
			next, err := taskCounter(generation)
			if err != nil {
				return nil, err
			}
			t.ManualRank = ranks.Rank
			p.Groups = append(p.Groups, taskGroupPlan{target, rows, ranks.Items, generation})
			position = &c.TaskPosition{SprintID: t.SprintID, State: t.State, Priority: t.Priority, OrderGeneration: next}
			if ranks.Previous != "" {
				v, e := f.ParseID[c.Task](ranks.Previous)
				if e != nil {
					return nil, internal(e)
				}
				position.PreviousID = &v
			}
			if ranks.Next != "" {
				v, e := f.ParseID[c.Task](ranks.Next)
				if e != nil {
					return nil, internal(e)
				}
				position.NextID = &v
			}
			if in.Reorder != nil {
				fields = []c.TaskChangedField{c.TaskRankChanged}
			}
			if in.Create != nil {
				fields = []c.TaskChangedField{c.TaskDescriptionChanged, c.TaskRankChanged, c.TaskPlanChanged, c.TaskPriorityChanged, c.TaskTitleChanged, c.TaskTypeChanged}
			}
		}
		if priorityChange != nil {
			oldRows, oldGen, e := loadTaskRanks(ctx, x, in.Project, source)
			if e != nil {
				return nil, e
			}
			if _, e = taskCounter(oldGen); e != nil {
				return nil, e
			}
			after := make([]rankItem, 0, len(oldRows))
			found := false
			for _, v := range oldRows {
				if v.ID == in.Target.String() {
					found = true
					continue
				}
				after = append(after, v)
			}
			if !found {
				return nil, internal(nil)
			}
			p.Groups = append(p.Groups, taskGroupPlan{source, oldRows, after, oldGen})
		}
	}
	out := c.TaskMutation{Task: t, EventIDs: []event.EventID{}}
	if len(fields) == 0 {
		p.After = out
		return p, nil
	}
	if in.Create == nil {
		n, err := taskCounter(int64(t.Version))
		if err != nil {
			return nil, err
		}
		t.Version = f.Version(n)
		t.UpdatedAt = at
	}
	p.QueryGeneration, err = loadTaskQueryGeneration(ctx, x, in.Project)
	if err != nil {
		return nil, err
	}
	if _, err = taskCounter(p.QueryGeneration); err != nil {
		return nil, err
	}
	out = c.TaskMutation{Task: t, Changed: true, TaskEventID: &te, EventIDs: []event.EventID{ev}}
	if err = out.Validate(); err != nil {
		return nil, internal(err)
	}
	p.After = out
	history := c.TaskEvent{ID: te, ProjectID: in.Project, TaskID: in.Target, TaskVersion: t.Version, Actor: c.TaskEventActor{Type: i.Human, UserID: in.User, Source: "task_domain"}, OperationID: r.ID, CorrelationID: r.ID, CreatedAt: at}
	change := c.TaskUpdatedChange
	if in.Create != nil {
		history.Type = c.TaskEventCreated
		history.Payload, err = canonical(c.TaskCreatedPayload{InitialState: c.TaskStateBacklog, MilestoneID: t.MilestoneID, SprintID: t.SprintID, Type: t.Type, Priority: t.Priority})
		change = c.TaskCreatedChange
	} else {
		history.Type = c.TaskEventFieldsUpdated
		history.Payload, err = canonical(c.TaskFieldsUpdatedPayload{ChangedFields: fields, TypeChange: typeChange, PriorityChange: priorityChange, Position: position})
		if in.Reorder != nil {
			change = c.TaskReorderedChange
		}
	}
	if err != nil {
		return nil, err
	}
	p.TaskEvent, err = canonical(history)
	if err != nil {
		return nil, internal(err)
	}
	header, err := taskHeader(ev, t, at)
	if err != nil {
		return nil, err
	}
	typed, err := s.state().deps.TaskEvents.NewTaskChanged(header, c.TaskChanged{CommandID: r.ID, ActorUserID: in.User, TaskEventID: te, MilestoneID: t.MilestoneID, SprintID: t.SprintID, Change: change, ChangedFields: fields, Position: position})
	if err != nil {
		return nil, internal(err)
	}
	p.Header = typed.Header()
	p.Payload = typed.PayloadBytes()
	return p, nil
}
func taskHeader(id event.EventID, t c.Task, at f.Instant) (event.Header, error) {
	p, err := f.ParseID[event.Project](t.ProjectID.String())
	if err != nil {
		return event.Header{}, internal(err)
	}
	target, err := f.ParseID[event.Aggregate](t.ID.String())
	if err != nil {
		return event.Header{}, internal(err)
	}
	v := t.Version
	return event.Header{EventID: id, EventType: c.TaskChangedName, SchemaVersion: c.TaskSchemaVersion, OccurredAt: at, Scope: event.Scope{Kind: event.ProjectScope, ProjectID: p}, AggregateType: c.TaskAggregate, AggregateID: target, AggregateVersion: &v}, nil
}
