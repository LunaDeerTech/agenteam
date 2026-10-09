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

func (s *BlockerService) AddTaskBlocker(ctx context.Context, a i.Actor, m f.CommandMeta, p c.ProjectID, t c.TaskID, r c.TaskBlockerCreate) (c.TaskBlockerMutation, error) {
	return s.executeBlocker(ctx, a, m, blockerInput{Command: c.TaskBlockerCommandAdd, Project: p, Target: t, Add: &r})
}
func (s *BlockerService) ResolveTaskBlocker(ctx context.Context, a i.Actor, m f.CommandMeta, p c.ProjectID, t c.TaskID, r c.TaskBlockerResolve) (c.TaskBlockerMutation, error) {
	return s.executeBlocker(ctx, a, m, blockerInput{Command: c.TaskBlockerCommandResolve, Project: p, Target: t, Resolve: &r})
}
func (s *BlockerService) executeBlocker(ctx context.Context, a i.Actor, m f.CommandMeta, in blockerInput) (c.TaskBlockerMutation, error) {
	ctx, entry, done, err := s.begin(ctx)
	if err != nil {
		return c.TaskBlockerMutation{}, err
	}
	defer done()
	if err = readInput(ctx, a, in.Project); err != nil {
		return c.TaskBlockerMutation{}, err
	}
	if m.Validate() != nil || m.ExpectedVersion == nil {
		return c.TaskBlockerMutation{}, fault(f.InvalidArgument)
	}
	in.Expected = *m.ExpectedVersion
	in.User, err = f.ParseID[i.User](a.Details().UserID)
	if err != nil {
		return c.TaskBlockerMutation{}, fault(f.Unauthenticated)
	}
	semantic, err := in.semantic(a, m.IdempotencyKey)
	if err != nil {
		return c.TaskBlockerMutation{}, err
	}
	raw, err := canonical(in)
	if err != nil {
		return c.TaskBlockerMutation{}, err
	}
	if err = in.UnmarshalJSON(raw); err != nil {
		return c.TaskBlockerMutation{}, err
	}
	id, err := c.TaskBlockerCommandIdentity(in.Project, in.Command, m.IdempotencyKey)
	if err != nil {
		return c.TaskBlockerMutation{}, err
	}
	q := c.TaskBlockerCommandLookupRequest{ProjectID: in.Project, Command: in.Command, IdempotencyKey: m.IdempotencyKey, SemanticDigest: semantic}
	st := s.state()
	for round := 0; round < 3; round++ {
		var source c.SprintID
		var replay *c.TaskBlockerMutation
		discovery, err := taskNormalize([]f.LockRequest{commandLock(id), userLock(in.User.String(), f.Shared), projectLock(in.Project, f.Shared), taskScheduleLock(in.Project, f.Exclusive), taskLock(in.Target.String(), f.Shared)})
		if err != nil {
			return c.TaskBlockerMutation{}, err
		}
		result := st.store.WithinTx(ctx, commandCause(id), func(ctx context.Context, tx f.Tx) error {
			if err := st.store.AcquireAll(ctx, tx, discovery); err != nil {
				return portError(err)
			}
			x, _, r, e := s.blockerCurrent(ctx, tx, a, q)
			if e != nil {
				return e
			}
			if r != nil {
				replay = r
				return nil
			}
			if _, e = st.deps.Authority.state().projects.RequireOwnerInTx(ctx, tx, a, in.Project, i.Mutate); e != nil {
				return portError(e)
			}
			t, e := loadTask(ctx, x, in.Project, in.Target)
			if e != nil {
				return e
			}
			if e = blockerTaskGate(t, in.Expected); e != nil {
				return e
			}
			source = t.SprintID
			return nil
		})
		if result.State() == f.Unknown {
			return s.confirmBlockerUnknown(ctx, entry, a, q, result)
		}
		if err = taskTxError(ctx, result); err != nil {
			return c.TaskBlockerMutation{}, err
		}
		if replay != nil {
			return replay.Clone(), nil
		}
		base, err := in.locks(m.IdempotencyKey, source)
		if err != nil {
			return c.TaskBlockerMutation{}, err
		}
		var prepared *blockerRecord
		replan := false
		result = st.store.WithinTx(ctx, commandCause(id), func(ctx context.Context, tx f.Tx) error {
			if e := st.store.AcquireAll(ctx, tx, base); e != nil {
				return portError(e)
			}
			x, r, out, e := s.blockerCurrent(ctx, tx, a, q)
			if e != nil {
				return e
			}
			if out != nil {
				replay = out
				return nil
			}
			scope, e := st.deps.Authority.blockerScope(ctx, tx, a, in.Project, in.Target, true)
			if e != nil {
				return e
			}
			at, e := dbNow(ctx, x)
			if e != nil {
				return e
			}
			insert := r == nil
			if insert {
				rid, e := f.NewID[c.TaskBlockerCommand]()
				if e != nil {
					return unavailable(e)
				}
				r = &blockerRecord{ID: rid, Project: in.Project, User: in.User, Command: in.Command, Key: m.IdempotencyKey, Semantic: semantic, Input: in, Revision: 1, State: "planned", Created: at}
			} else {
				n, e := taskCounter(int64(r.Revision))
				if e != nil {
					return e
				}
				r.Revision = f.Version(n)
			}
			te, e := f.NewID[c.TaskEvent]()
			if e != nil {
				return unavailable(e)
			}
			ev, e := f.NewID[event.EventIdentity]()
			if e != nil {
				return unavailable(e)
			}
			plan, e := s.evaluateBlocker(ctx, tx, scope, a, r, source, at, te, ev)
			if errors.Is(e, errReplan) {
				replan = true
			}
			if e != nil {
				return e
			}
			r.Plan = plan
			r.TaskEventID = te
			r.EventID = ev
			if e = validateBlockerRecord(r, a); e != nil {
				return e
			}
			if e = storeBlockerPlan(ctx, x, r, insert); e != nil {
				return e
			}
			prepared = r
			return nil
		})
		if result.State() == f.Unknown {
			return s.confirmBlockerUnknown(ctx, entry, a, q, result)
		}
		if result.State() == f.NotCommitted && replan {
			if ctx.Err() != nil {
				return c.TaskBlockerMutation{}, canceled(ctx.Err())
			}
			continue
		}
		if err = taskTxError(ctx, result); err != nil {
			return c.TaskBlockerMutation{}, mapBlockerConflict(err, in)
		}
		if replay != nil {
			return replay.Clone(), nil
		}
		if prepared == nil || prepared.Plan == nil {
			return c.TaskBlockerMutation{}, internal(nil)
		}
		ev, err := st.deps.BlockerEvents.NewTaskBlockersChanged(prepared.Plan.Header, prepared.Plan.Payload)
		if err != nil {
			return c.TaskBlockerMutation{}, internal(err)
		}
		appendPlan, err := st.deps.Events.PrepareAppend(ctx, a, ev)
		if err != nil {
			return c.TaskBlockerMutation{}, portError(err)
		}
		locks, err := taskNormalize(append(append([]f.LockRequest{}, base...), appendPlan.Locks()...))
		if err != nil {
			return c.TaskBlockerMutation{}, err
		}
		var out c.TaskBlockerMutation
		replan = false
		result = st.store.WithinTx(ctx, commandCause(id), func(ctx context.Context, tx f.Tx) error {
			if e := st.store.AcquireAll(ctx, tx, locks); e != nil {
				return portError(e)
			}
			x, r, replay, e := s.blockerCurrent(ctx, tx, a, q)
			if e != nil {
				return e
			}
			if replay != nil {
				out = replay.Clone()
				return nil
			}
			scope, e := st.deps.Authority.blockerScope(ctx, tx, a, in.Project, in.Target, true)
			if e != nil {
				return e
			}
			if r == nil || r.Plan == nil || r.ID != prepared.ID || r.Revision != prepared.Revision || r.EventID != prepared.EventID || r.TaskEventID != prepared.TaskEventID {
				replan = true
				return errReplan
			}
			current, e := s.evaluateBlocker(ctx, tx, scope, a, r, source, r.Plan.Header.OccurredAt, r.TaskEventID, r.EventID)
			if errors.Is(e, errReplan) {
				replan = true
			}
			if e != nil {
				return e
			}
			if !sameValue(current, prepared.Plan) {
				replan = true
				return errReplan
			}
			if e = applyBlockerPlan(ctx, scope, r); e != nil {
				return e
			}
			receipt, e := st.deps.Events.AppendEventInTx(ctx, tx, a, ev, appendPlan)
			if e != nil {
				return portError(e)
			}
			if receipt.EventID != r.EventID || receipt.Sequence.Validate() != nil {
				return internal(nil)
			}
			if e = completeBlockerCommand(ctx, x, r, current.After); e != nil {
				return e
			}
			if e = st.deps.Activity.TouchActivityInTx(ctx, tx, a); e != nil {
				return portError(e)
			}
			out = current.After.Clone()
			return nil
		})
		if result.State() == f.Unknown {
			return s.confirmBlockerUnknown(ctx, entry, a, q, result)
		}
		if result.State() == f.NotCommitted && replan {
			if ctx.Err() != nil {
				return c.TaskBlockerMutation{}, canceled(ctx.Err())
			}
			continue
		}
		if err = taskTxError(ctx, result); err != nil {
			return c.TaskBlockerMutation{}, mapBlockerConflict(err, in)
		}
		if out.Validate() != nil {
			return c.TaskBlockerMutation{}, internal(nil)
		}
		return out.Clone(), nil
	}
	return c.TaskBlockerMutation{}, fault(f.ResourceBusy)
}
func mapBlockerConflict(err error, in blockerInput) error {
	var pg *pgconn.PgError
	if in.Add != nil && errors.As(err, &pg) && pg.Code == "23505" && pg.SchemaName == "agenteam_work" && pg.ConstraintName == "task_blockers_pkey" {
		v := f.NewFault(f.ResourceBusy, f.NotCommitted)
		v.FieldErrors = []f.FieldError{{Path: "/blocker_id", Code: "TARGET_OCCUPIED"}}
		return v
	}
	return err
}
func blockerTaskGate(t c.Task, expected f.Version) error {
	if t.Version != expected {
		return field(f.TaskVersionConflict, "/expected_version", "STALE_VERSION")
	}
	if t.State.Terminal() {
		return fault(f.TaskTerminalImmutable)
	}
	if t.State != c.TaskStateBacklog || t.AssigneeAgentID != nil {
		return fault(f.DependencyUnbound)
	}
	return nil
}

func (s *BlockerService) blockerCurrent(ctx context.Context, tx f.Tx, a i.Actor, q c.TaskBlockerCommandLookupRequest) (postgres.SQLExecutor, *blockerRecord, *c.TaskBlockerMutation, error) {
	x, e := s.state().store.InTx(tx)
	if e != nil {
		return nil, nil, nil, portError(e)
	}
	access, e := s.state().deps.Authority.state().projects.RequireOwnerInTx(ctx, tx, a, q.ProjectID, i.Read)
	if e != nil {
		return nil, nil, nil, portError(e)
	}
	if access.Project().ID != q.ProjectID {
		return nil, nil, nil, internal(nil)
	}
	id, e := c.TaskBlockerCommandIdentity(q.ProjectID, q.Command, q.IdempotencyKey)
	if e != nil {
		return nil, nil, nil, e
	}
	r, e := loadBlockerCommand(ctx, x, id, a.Details().UserID)
	if e != nil {
		return nil, nil, nil, e
	}
	if r == nil {
		return x, nil, nil, nil
	}
	if r.Semantic != q.SemanticDigest {
		return nil, nil, nil, fault(f.IdempotencyKeyReused)
	}
	if e = validateBlockerRecord(r, a); e != nil {
		return nil, nil, nil, e
	}
	if r.State == "completed" {
		v := r.Receipt.Clone()
		return x, r, &v, nil
	}
	return x, r, nil, nil
}
func (s *BlockerService) LookupTaskBlockerCommand(ctx context.Context, a i.Actor, q c.TaskBlockerCommandLookupRequest) (c.TaskBlockerCommandLookup, error) {
	ctx, _, done, e := s.begin(ctx)
	if e != nil {
		return c.TaskBlockerCommandLookup{}, e
	}
	defer done()
	if e = readInput(ctx, a, q.ProjectID); e != nil {
		return c.TaskBlockerCommandLookup{}, e
	}
	if e = q.Validate(); e != nil {
		return c.TaskBlockerCommandLookup{}, e
	}
	return s.lookupBlocker(ctx, a, q)
}
func (s *BlockerService) lookupBlocker(ctx context.Context, a i.Actor, q c.TaskBlockerCommandLookupRequest) (c.TaskBlockerCommandLookup, error) {
	id, e := c.TaskBlockerCommandIdentity(q.ProjectID, q.Command, q.IdempotencyKey)
	if e != nil {
		return c.TaskBlockerCommandLookup{}, e
	}
	locks, e := taskNormalize([]f.LockRequest{commandLock(id), userLock(a.Details().UserID, f.Shared), projectLock(q.ProjectID, f.Shared)})
	if e != nil {
		return c.TaskBlockerCommandLookup{}, e
	}
	var out c.TaskBlockerCommandLookup
	result := s.state().store.WithinTx(ctx, commandCause(id), func(ctx context.Context, tx f.Tx) error {
		if e := s.state().store.AcquireAll(ctx, tx, locks); e != nil {
			return portError(e)
		}
		_, r, replay, e := s.blockerCurrent(ctx, tx, a, q)
		if e != nil {
			return e
		}
		switch {
		case replay != nil:
			out = c.TaskBlockerCommandLookup{Status: c.LookupCommitted, Receipt: replay}
		case r != nil:
			out = c.TaskBlockerCommandLookup{Status: c.LookupInProgress}
		default:
			out = c.TaskBlockerCommandLookup{Status: c.LookupNotObserved}
		}
		return nil
	})
	if e = taskTxError(ctx, result); e != nil {
		return c.TaskBlockerCommandLookup{}, e
	}
	return out.Clone(), nil
}
func (s *BlockerService) confirmBlockerUnknown(ctx context.Context, entry *call, a i.Actor, q c.TaskBlockerCommandLookupRequest, original f.CommitResult) (c.TaskBlockerMutation, error) {
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
	out, e := s.lookupBlocker(confirm, a, q)
	if e != nil {
		return c.TaskBlockerMutation{}, txError(original)
	}
	if out.Status == c.LookupCommitted && out.Receipt != nil {
		return out.Receipt.Clone(), nil
	}
	if out.Status == c.LookupNotObserved {
		return c.TaskBlockerMutation{}, notCommittedAfterUnknown(original)
	}
	return c.TaskBlockerMutation{}, txError(original)
}
func (s *BlockerService) evaluateBlocker(ctx context.Context, tx f.Tx, scope *blockerScope, a i.Actor, r *blockerRecord, source c.SprintID, at f.Instant, te c.TaskEventID, ev event.EventID) (*blockerPlan, error) {
	in := r.Input
	t, e := loadTask(ctx, scope.x, in.Project, in.Target)
	if e != nil {
		return nil, e
	}
	if e = blockerTaskGate(t, in.Expected); e != nil {
		return nil, e
	}
	if t.SprintID != source {
		return nil, errReplan
	}
	placement, e := s.state().deps.Structure.ReadPlacementInTx(ctx, tx, a, in.Project, t.SprintID)
	if e != nil {
		if isNotFound(e) {
			return nil, field(f.TaskSprintInvalid, "/sprint_id", "INVALID_SPRINT")
		}
		return nil, e
	}
	if t.MilestoneID != placement.Milestone.ID {
		return nil, internal(nil)
	}
	if placement.Sprint.State != c.Planned && placement.Sprint.State != c.Current {
		return nil, field(f.TaskSprintInvalid, "/sprint_id", "COMPLETED_SPRINT")
	}
	plan := &blockerPlan{Before: t.Clone(), Placement: taskPlacement{placement.Milestone.ID, placement.Sprint.ID, placement.Sprint.State}}
	if at.Time().Before(t.UpdatedAt.Time()) {
		at = t.UpdatedAt
	}
	if at.Time().Before(r.Created.Time()) {
		at = r.Created
	}
	actor := c.TaskEventActor{Type: i.Human, UserID: in.User, Source: "task_domain"}
	var b c.TaskBlocker
	if in.Add != nil {
		var occupied bool
		if e = scope.x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_work.task_blockers WHERE id=$1)`, in.Add.BlockerID.String()).Scan(&occupied); e != nil {
			return nil, taskSQL(e)
		}
		if occupied {
			return nil, field(f.ResourceBusy, "/blocker_id", "TARGET_OCCUPIED")
		}
		if in.Add.Type == c.TaskBlockerRelyOn {
			related := in.Add.Metadata.RelyOn.RelatedTaskID
			if related == in.Target {
				return nil, field(f.InvalidArgument, "/metadata/related_task_id", "SELF_REFERENCE")
			}
			if _, e = loadTask(ctx, scope.x, in.Project, related); e != nil {
				var value *f.Fault
				if errors.As(e, &value) && value.Code == f.TaskNotFound {
					return nil, field(f.NotFound, "/metadata/related_task_id", "INVALID_DEPENDENCY_TARGET")
				}
				return nil, e
			}
		}
		b = c.TaskBlocker{ID: in.Add.BlockerID, ProjectID: in.Project, TaskID: in.Target, Type: in.Add.Type, Description: in.Add.Description, Metadata: in.Add.Metadata.Clone(), CreatedAt: at, CreatedBy: actor}
	} else {
		row, e := loadBlocker(ctx, scope, in.Resolve.BlockerID)
		if e != nil {
			return nil, e
		}
		if row == nil {
			return nil, fault(f.BlockerNotFound)
		}
		if row.Value.ResolvedAt != nil {
			return nil, fault(f.BlockerAlreadyResolved)
		}
		b = row.Value.Clone()
		before := b.Clone()
		plan.BlockerBefore = &before
		if at.Time().Before(b.CreatedAt.Time()) {
			at = b.CreatedAt
		}
		b.ResolvedAt = &at
		b.ResolvedBy = &actor
		b.ResolutionComment = in.Resolve.Clone().ResolutionComment
	}
	if e = validateBlockerGraph(ctx, scope, in.Add); e != nil {
		return nil, e
	}
	gen, e := loadTaskQueryGeneration(ctx, scope.x, in.Project)
	if e != nil {
		return nil, e
	}
	if _, e = taskCounter(gen); e != nil {
		return nil, e
	}
	plan.QueryGeneration = gen
	version, e := taskCounter(int64(t.Version))
	if e != nil {
		return nil, e
	}
	t.Version = f.Version(version)
	t.UpdatedAt = at
	plan.After = c.TaskBlockerMutation{Task: t, Blocker: b, TaskEventID: te, EventIDs: []event.EventID{ev}}
	history := c.TaskBlockerEvent{ID: te, ProjectID: in.Project, TaskID: in.Target, TaskVersion: t.Version, Actor: actor, OperationID: r.ID, CorrelationID: r.ID, CreatedAt: at}
	change := c.TaskBlockerAddedChange
	if in.Add != nil {
		history.Type = c.TaskBlockerEventAdded
		history.Payload.Added = &c.TaskBlockerAddedPayload{BlockerID: b.ID, BlockerType: b.Type}
	} else {
		change = c.TaskBlockerResolvedChange
		history.Type = c.TaskBlockerEventResolved
		history.Payload.Resolved = &c.TaskBlockerResolvedPayload{BlockerID: b.ID, BlockerType: b.Type, ResolutionComment: b.ResolutionComment}
	}
	plan.TaskEvent = history
	plan.Payload = c.TaskBlockersChanged{OperationID: r.ID, ActorUserID: in.User, TaskEventID: te, BlockerID: b.ID, Change: change}
	plan.Header, e = blockerHeader(ev, t, at)
	if e != nil {
		return nil, e
	}
	return plan, nil
}
func blockerHeader(id event.EventID, t c.Task, at f.Instant) (event.Header, error) {
	h, e := taskHeader(id, t, at)
	if e != nil {
		return event.Header{}, e
	}
	h.EventType = c.TaskBlockersChangedName
	h.SchemaVersion = c.TaskBlockerSchemaVersion
	return h, nil
}
