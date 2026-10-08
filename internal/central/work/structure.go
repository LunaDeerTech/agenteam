package work

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
	"github.com/jackc/pgx/v5/pgconn"
)

func (s *Service) CreateMilestone(ctx context.Context, a i.Actor, m f.CommandMeta, p c.ProjectID, r c.CreateMilestoneRequest) (c.StructureMutation, error) {
	return s.execute(ctx, a, m, commandInput{Command: c.MilestoneCreate, Project: p, Target: r.MilestoneID.String(), CreateMilestone: &r})
}
func (s *Service) UpdateMilestone(ctx context.Context, a i.Actor, m f.CommandMeta, p c.ProjectID, id c.MilestoneID, r c.UpdateFields) (c.StructureMutation, error) {
	return s.execute(ctx, a, m, commandInput{Command: c.MilestoneUpdate, Project: p, Target: id.String(), Update: &r})
}
func (s *Service) ReorderMilestone(ctx context.Context, a i.Actor, m f.CommandMeta, p c.ProjectID, id c.MilestoneID, r c.ReorderMilestoneRequest) (c.StructureMutation, error) {
	return s.execute(ctx, a, m, commandInput{Command: c.MilestoneReorder, Project: p, Target: id.String(), ReorderMilestone: &r})
}
func (s *Service) CreateSprint(ctx context.Context, a i.Actor, m f.CommandMeta, p c.ProjectID, r c.CreateSprintRequest) (c.StructureMutation, error) {
	return s.execute(ctx, a, m, commandInput{Command: c.SprintCreate, Project: p, Target: r.SprintID.String(), CreateSprint: &r})
}
func (s *Service) UpdateSprint(ctx context.Context, a i.Actor, m f.CommandMeta, p c.ProjectID, id c.SprintID, r c.UpdateFields) (c.StructureMutation, error) {
	return s.execute(ctx, a, m, commandInput{Command: c.SprintUpdate, Project: p, Target: id.String(), Update: &r})
}
func (s *Service) ReorderSprint(ctx context.Context, a i.Actor, m f.CommandMeta, p c.ProjectID, id c.SprintID, r c.ReorderSprintRequest) (c.StructureMutation, error) {
	return s.execute(ctx, a, m, commandInput{Command: c.SprintReorder, Project: p, Target: id.String(), ReorderSprint: &r})
}

// This sentinel never escapes as a successful result. The failed transaction
// must finish and release every lock before a new planning round starts.
var errReplan = errors.New("WORK_PLAN_STALE")

type evaluation struct {
	Result c.StructureMutation
	Plan   *structurePlan
	Ranks  rankResult
}

func (s *Service) execute(ctx context.Context, actor i.Actor, meta f.CommandMeta, in commandInput) (c.StructureMutation, error) {
	if err := readInput(ctx, actor, in.Project); err != nil {
		return c.StructureMutation{}, err
	}
	if meta.Validate() != nil {
		return c.StructureMutation{}, fault(f.InvalidArgument)
	}
	var err error
	in.User, err = f.ParseID[i.User](actor.Details().UserID)
	if err != nil {
		return c.StructureMutation{}, fault(f.Unauthenticated)
	}
	in.Expected = meta.ExpectedVersion
	semantic, err := in.semantic(actor, meta.IdempotencyKey)
	if err != nil {
		return c.StructureMutation{}, err
	}
	// Freeze caller-owned optional pointers before any transaction or callback.
	raw, err := canonical(in)
	if err != nil {
		return c.StructureMutation{}, err
	}
	in, err = decodePrivate[commandInput](raw, c.MaxRequestBytes, []string{"command", "project_id", "target_id", "actor_user_id", "expected_version", "create_milestone", "create_sprint", "update", "reorder_milestone", "reorder_sprint"})
	if err != nil {
		return c.StructureMutation{}, err
	}
	ctx, entry, done, err := s.begin(ctx)
	if err != nil {
		return c.StructureMutation{}, err
	}
	defer done()
	key := meta.IdempotencyKey
	identity, err := in.identity(key)
	if err != nil {
		return c.StructureMutation{}, err
	}
	base, err := in.locks(key)
	if err != nil {
		return c.StructureMutation{}, err
	}
	lookup := c.CommandLookupRequest{ProjectID: in.Project, Command: in.Command, Key: key, Semantic: semantic}
	for round := 0; round < 3; round++ {
		var prepared *commandRecord
		var completed *c.StructureMutation
		result := s.state().store.WithinTx(ctx, commandCause(identity), func(ctx context.Context, tx f.Tx) error {
			if err := s.state().store.AcquireAll(ctx, tx, base); err != nil {
				return portError(err)
			}
			x, ref, record, replay, err := s.current(ctx, tx, actor, lookup)
			if err != nil {
				return err
			}
			if replay != nil {
				completed = replay
				return nil
			}
			if _, err = s.state().deps.Authority.state().projects.RequireOwnerInTx(ctx, tx, actor, in.Project, i.Mutate); err != nil {
				return portError(err)
			}
			at, err := dbNow(ctx, x)
			if err != nil {
				return err
			}
			insert := record == nil
			if insert {
				id, e := f.NewID[c.StructureCommand]()
				if e != nil {
					return unavailable(e)
				}
				record = &commandRecord{ID: id, Project: in.Project, User: in.User, Command: in.Command, Key: key, Semantic: semantic, Input: in, Revision: 1, State: "planned", Created: at}
			} else {
				record.Revision, err = nextVersion(record.Revision)
				if err != nil {
					return err
				}
			}
			eventID, err := f.NewID[event.EventIdentity]()
			if err != nil {
				return unavailable(err)
			}
			e, err := s.evaluate(ctx, x, ref, record, at, eventID)
			if err != nil {
				return err
			}
			if !e.Result.Changed {
				// A formerly planned attempt may now be a legitimate no-op. Keep
				// its existing revision for the conditional completed transition.
				if !insert {
					record.Revision--
				}
				if err = completeCommand(ctx, x, record, e.Result, insert); err != nil {
					return err
				}
				if err = s.state().deps.Activity.TouchActivityInTx(ctx, tx, actor); err != nil {
					return portError(err)
				}
				v := e.Result.Clone()
				completed = &v
				return nil
			}
			record.Plan = e.Plan
			record.EventID = &eventID
			if err = storePlan(ctx, x, record, insert); err != nil {
				return err
			}
			prepared = record
			return nil
		})
		if result.State() == f.Unknown {
			return s.confirmUnknown(ctx, entry, actor, lookup, result)
		}
		if err = txError(result); err != nil {
			return c.StructureMutation{}, mapTargetConflict(err, in)
		}
		if completed != nil {
			return completed.Clone(), nil
		}
		if prepared == nil || prepared.Plan == nil {
			return c.StructureMutation{}, internal(nil)
		}
		ev, err := s.state().deps.WorkEvents.Restore(prepared.Plan.Header, prepared.Plan.Payload)
		if err != nil {
			return c.StructureMutation{}, internal(err)
		}
		appendPlan, err := s.state().deps.Events.PrepareAppend(ctx, actor, ev)
		if err != nil {
			return c.StructureMutation{}, portError(err)
		}
		locks, err := oc.NormalizeLocks(append(append([]f.LockRequest{}, base...), appendPlan.Locks()...))
		if err != nil {
			return c.StructureMutation{}, portError(err)
		}
		var output c.StructureMutation
		replan := false
		result = s.state().store.WithinTx(ctx, commandCause(identity), func(ctx context.Context, tx f.Tx) error {
			if err := s.state().store.AcquireAll(ctx, tx, locks); err != nil {
				return portError(err)
			}
			x, ref, record, replay, err := s.current(ctx, tx, actor, lookup)
			if err != nil {
				return err
			}
			if replay != nil {
				output = replay.Clone()
				return nil
			}
			if _, err = s.state().deps.Authority.state().projects.RequireOwnerInTx(ctx, tx, actor, in.Project, i.Mutate); err != nil {
				return portError(err)
			}
			if record == nil || record.Plan == nil || record.Revision != prepared.Revision || record.ID != prepared.ID || record.EventID == nil || *record.EventID != *prepared.EventID {
				replan = true
				return errReplan
			}
			e, err := s.evaluate(ctx, x, ref, record, record.Plan.Header.OccurredAt, *record.EventID)
			if err != nil {
				return err
			}
			if !e.Result.Changed || !sameValue(e.Plan, prepared.Plan) {
				replan = true
				return errReplan
			}
			if err = applyEvaluation(ctx, x, record, e); err != nil {
				return err
			}
			receipt, err := s.state().deps.Events.AppendEventInTx(ctx, tx, actor, ev, appendPlan)
			if err != nil {
				return portError(err)
			}
			if receipt.EventID != *record.EventID || receipt.Sequence.Validate() != nil {
				return internal(nil)
			}
			if err = completeCommand(ctx, x, record, e.Result, false); err != nil {
				return err
			}
			if err = s.state().deps.Activity.TouchActivityInTx(ctx, tx, actor); err != nil {
				return portError(err)
			}
			output = e.Result.Clone()
			return nil
		})
		if result.State() == f.Unknown {
			return s.confirmUnknown(ctx, entry, actor, lookup, result)
		}
		if result.State() == f.NotCommitted && replan {
			continue
		}
		if err = txError(result); err != nil {
			return c.StructureMutation{}, mapTargetConflict(err, in)
		}
		if output.Validate() != nil {
			return c.StructureMutation{}, internal(nil)
		}
		return output.Clone(), nil
	}
	return c.StructureMutation{}, fault(f.ResourceBusy)
}

// Read authorization deliberately precedes receipt identity and every new-write
// check. Completed history never needs the target to remain at its old version.
func (s *Service) current(ctx context.Context, tx f.Tx, actor i.Actor, q c.CommandLookupRequest) (postgres.SQLExecutor, pc.ProjectRef, *commandRecord, *c.StructureMutation, error) {
	x, err := s.state().store.InTx(tx)
	if err != nil {
		return nil, pc.ProjectRef{}, nil, nil, portError(err)
	}
	access, err := s.state().deps.Authority.state().projects.RequireOwnerInTx(ctx, tx, actor, q.ProjectID, i.Read)
	if err != nil {
		return nil, pc.ProjectRef{}, nil, nil, portError(err)
	}
	ref := access.Project()
	if ref.ID != q.ProjectID {
		return nil, pc.ProjectRef{}, nil, nil, internal(nil)
	}
	id, err := c.Identity(q.ProjectID, q.Command, q.Key)
	if err != nil {
		return nil, pc.ProjectRef{}, nil, nil, err
	}
	record, err := loadCommand(ctx, x, id)
	if err != nil {
		return nil, pc.ProjectRef{}, nil, nil, err
	}
	if record == nil {
		return x, ref, nil, nil, nil
	}
	if record.User.String() != actor.Details().UserID {
		return nil, pc.ProjectRef{}, nil, nil, fault(f.NotFound)
	}
	if record.Semantic != q.Semantic {
		return nil, pc.ProjectRef{}, nil, nil, fault(f.IdempotencyKeyReused)
	}
	if err = validateRecord(record, actor); err != nil {
		return nil, pc.ProjectRef{}, nil, nil, err
	}
	if record.State == "completed" {
		v := record.Receipt.Clone()
		return x, ref, record, &v, nil
	}
	return x, ref, record, nil, nil
}

func (s *Service) LookupCommand(ctx context.Context, actor i.Actor, q c.CommandLookupRequest) (c.CommandLookup, error) {
	if err := readInput(ctx, actor, q.ProjectID); err != nil {
		return c.CommandLookup{}, err
	}
	if err := q.Validate(); err != nil {
		return c.CommandLookup{}, err
	}
	ctx, _, done, err := s.begin(ctx)
	if err != nil {
		return c.CommandLookup{}, err
	}
	defer done()
	return s.lookup(ctx, actor, q)
}
func (s *Service) lookup(ctx context.Context, actor i.Actor, q c.CommandLookupRequest) (c.CommandLookup, error) {
	id, err := c.Identity(q.ProjectID, q.Command, q.Key)
	if err != nil {
		return c.CommandLookup{}, err
	}
	locks := []f.LockRequest{commandLock(id), userLock(actor.Details().UserID, f.Shared), projectLock(q.ProjectID, f.Shared)}
	var output c.CommandLookup
	result := s.state().store.WithinTx(ctx, commandCause(id), func(ctx context.Context, tx f.Tx) error {
		if err := s.state().store.AcquireAll(ctx, tx, locks); err != nil {
			return portError(err)
		}
		_, _, record, replay, err := s.current(ctx, tx, actor, q)
		if err != nil {
			return err
		}
		switch {
		case replay != nil:
			output = c.CommandLookup{State: c.LookupCommitted, Result: replay}
		case record != nil:
			output = c.CommandLookup{State: c.LookupInProgress}
		default:
			output = c.CommandLookup{State: c.LookupNotObserved}
		}
		return nil
	})
	// Caller cancellation only describes a proven rollback, never a committed
	// or unknown transaction outcome.
	if result.State() == f.NotCommitted {
		if err := ctx.Err(); err != nil {
			return c.CommandLookup{}, canceled(err)
		}
	}
	if err = txError(result); err != nil {
		return c.CommandLookup{}, err
	}
	return output, nil
}
func (s *Service) confirmUnknown(ctx context.Context, entry *call, actor i.Actor, q c.CommandLookupRequest, original f.CommitResult) (c.StructureMutation, error) {
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
	value, err := s.lookup(confirm, actor, q)
	if err != nil {
		return c.StructureMutation{}, txError(original)
	}
	if value.State == c.LookupCommitted && value.Result != nil {
		return value.Result.Clone(), nil
	}
	if value.State == c.LookupNotObserved {
		return c.StructureMutation{}, notCommittedAfterUnknown(original)
	}
	return c.StructureMutation{}, txError(original)
}
func mapTargetConflict(err error, in commandInput) error {
	var pg *pgconn.PgError
	if in.Command.IsCreate() && errors.As(err, &pg) && pg.Code == "23505" && pg.SchemaName == "agenteam_work" && (pg.ConstraintName == "milestones_pkey" && in.Command == c.MilestoneCreate || pg.ConstraintName == "sprints_pkey" && in.Command == c.SprintCreate) {
		value := f.NewFault(f.ResourceBusy, f.NotCommitted)
		value.FieldErrors = []f.FieldError{{Path: targetPath(in), Code: "TARGET_OCCUPIED"}}
		return value
	}
	return err
}
func targetPath(in commandInput) string {
	if in.Command.IsSprint() {
		return "/sprint_id"
	}
	return "/milestone_id"
}

func (s *Service) evaluate(ctx context.Context, x postgres.SQLExecutor, ref pc.ProjectRef, record *commandRecord, at f.Instant, eventID event.EventID) (evaluation, error) {
	in := record.Input
	if err := checkPointer(ctx, x, ref); err != nil {
		return evaluation{}, err
	}
	out := c.StructureMutation{Command: in.Command}
	plan := &structurePlan{}
	var oldVersion f.Version
	if in.Command.IsCreate() {
		if in.CreateSprint != nil {
			if _, err := loadMilestone(ctx, x, in.Project, in.CreateSprint.MilestoneID); err != nil {
				return evaluation{}, err
			}
		}
		var occupied bool
		query := `SELECT EXISTS(SELECT 1 FROM agenteam_work.milestones WHERE id=$1)`
		if in.Command.IsSprint() {
			query = `SELECT EXISTS(SELECT 1 FROM agenteam_work.sprints WHERE id=$1)`
		}
		if err := x.QueryRow(ctx, query, in.Target).Scan(&occupied); err != nil {
			return evaluation{}, unavailable(err)
		}
		if occupied {
			return evaluation{}, field(f.ResourceBusy, targetPath(in), "TARGET_OCCUPIED")
		}
		if in.CreateMilestone != nil {
			r := in.CreateMilestone
			out.Milestone = &c.Milestone{ID: r.MilestoneID, ProjectID: in.Project, Title: r.Title, Description: r.Description, Version: 1, CreatedAt: at, UpdatedAt: at}
		} else {
			r := in.CreateSprint
			out.Sprint = &c.Sprint{ID: r.SprintID, ProjectID: in.Project, MilestoneID: r.MilestoneID, Title: r.Title, Description: r.Description, Version: 1, CreatedAt: at, UpdatedAt: at, State: c.Planned}
		}
	} else if in.Command.IsSprint() {
		id, err := typedID[pc.Sprint](in.Target)
		if err != nil {
			return evaluation{}, err
		}
		v, err := loadSprint(ctx, x, in.Project, id, ref.CurrentSprintID)
		if err != nil {
			return evaluation{}, err
		}
		if in.ReorderSprint != nil && v.MilestoneID != in.ReorderSprint.MilestoneID {
			return evaluation{}, fault(f.NotFound)
		}
		if _, err = loadMilestone(ctx, x, in.Project, v.MilestoneID); err != nil {
			if isNotFound(err) {
				return evaluation{}, internal(err)
			}
			return evaluation{}, err
		}
		oldVersion = v.Version
		before := v.Clone()
		plan.BeforeSprint = &before
		copy := v.Clone()
		out.Sprint = &copy
	} else {
		id, err := typedID[c.Milestone](in.Target)
		if err != nil {
			return evaluation{}, err
		}
		v, err := loadMilestone(ctx, x, in.Project, id)
		if err != nil {
			return evaluation{}, err
		}
		oldVersion = v.Version
		before := v
		plan.BeforeMilestone = &before
		copy := v
		out.Milestone = &copy
	}
	if !in.Command.IsCreate() {
		if in.Expected == nil || oldVersion != *in.Expected {
			return evaluation{}, field(f.VersionConflict, "/expected_version", "STALE_VERSION")
		}
		if out.Sprint != nil && out.Sprint.State == c.Completed {
			return evaluation{}, field(f.InvalidState, "/sprint_id", "COMPLETED_SPRINT_IMMUTABLE")
		}
		oldAt := f.Instant{}
		if out.Sprint != nil {
			oldAt = out.Sprint.UpdatedAt
		} else {
			oldAt = out.Milestone.UpdatedAt
		}
		if at.Time().Before(oldAt.Time()) {
			at = oldAt
		}
	}
	changed := []c.ChangedField{}
	rank := rankResult{}
	if in.Update != nil {
		var title, description *string
		if out.Sprint != nil {
			title = &out.Sprint.Title
			description = &out.Sprint.Description
		} else {
			title = &out.Milestone.Title
			description = &out.Milestone.Description
		}
		if in.Update.Description != nil && *in.Update.Description != *description {
			*description = *in.Update.Description
			changed = append(changed, c.DescriptionChanged)
		}
		if in.Update.Title != nil && *in.Update.Title != *title {
			*title = *in.Update.Title
			changed = append(changed, c.TitleChanged)
		}
	} else {
		generation, err := loadGeneration(ctx, x, in.Project, in.parent())
		if err != nil {
			return evaluation{}, err
		}
		rows, err := loadRanks(ctx, x, in.Project, in.parent())
		if err != nil {
			return evaluation{}, err
		}
		if in.Command.IsCreate() && len(rows) >= c.MaxGroupSize {
			path := "/project_id"
			if in.Command.IsSprint() {
				path = "/milestone_id"
			}
			return evaluation{}, field(f.ResourceBusy, path, "GROUP_LIMIT")
		}
		rank, err = rankFor(rows, in.Target, in.before(), in.Command.IsCreate())
		if err != nil {
			return evaluation{}, err
		}
		if rank.Changed {
			plan.GroupBefore = &generation
			changed = []c.ChangedField{c.RankChanged}
			if in.Command.IsCreate() {
				changed = []c.ChangedField{c.DescriptionChanged, c.RankChanged, c.TitleChanged}
			}
			if out.Sprint != nil {
				out.Sprint.ManualRank = rank.Rank
			} else {
				out.Milestone.ManualRank = rank.Rank
			}
		}
	}
	if len(changed) == 0 {
		if err := out.Validate(); err != nil {
			return evaluation{}, internal(err)
		}
		return evaluation{Result: out}, nil
	}
	if !in.Command.IsCreate() {
		next, err := nextVersion(oldVersion)
		if err != nil {
			return evaluation{}, err
		}
		if out.Sprint != nil {
			out.Sprint.Version = next
			out.Sprint.UpdatedAt = at
		} else {
			out.Milestone.Version = next
			out.Milestone.UpdatedAt = at
		}
	}
	out.Changed = true
	out.EventID = &eventID
	if err := out.Validate(); err != nil {
		return evaluation{}, internal(err)
	}
	plan.After = out.Clone()
	change := c.UpdatedChange
	if in.Command.IsCreate() {
		change = c.CreatedChange
	} else if in.ordered() {
		change = c.ReorderedChange
	}
	var generation f.Version
	if plan.GroupBefore != nil {
		var err error
		generation, err = nextVersion(*plan.GroupBefore)
		if err != nil {
			return evaluation{}, err
		}
	}
	header, err := structureHeader(eventID, in, out, at)
	if err != nil {
		return evaluation{}, err
	}
	var ev event.Event
	if out.Milestone != nil {
		p := c.MilestoneChanged{CommandID: record.ID, ActorUserID: record.User, Change: change, ChangedFields: changed}
		if plan.GroupBefore != nil {
			p.Position = &c.MilestonePosition{OrderGeneration: generation}
			if rank.Previous != "" {
				id, e := typedID[c.Milestone](rank.Previous)
				if e != nil {
					return evaluation{}, e
				}
				p.Position.PreviousID = &id
			}
			if rank.Next != "" {
				id, e := typedID[c.Milestone](rank.Next)
				if e != nil {
					return evaluation{}, e
				}
				p.Position.NextID = &id
			}
		}
		ev, err = s.state().deps.WorkEvents.NewMilestoneChanged(header, p)
	} else {
		p := c.SprintChanged{CommandID: record.ID, ActorUserID: record.User, MilestoneID: out.Sprint.MilestoneID, Change: change, ChangedFields: changed}
		if plan.GroupBefore != nil {
			p.Position = &c.SprintPosition{OrderGeneration: generation}
			if rank.Previous != "" {
				id, e := typedID[pc.Sprint](rank.Previous)
				if e != nil {
					return evaluation{}, e
				}
				p.Position.PreviousID = &id
			}
			if rank.Next != "" {
				id, e := typedID[pc.Sprint](rank.Next)
				if e != nil {
					return evaluation{}, e
				}
				p.Position.NextID = &id
			}
		}
		ev, err = s.state().deps.WorkEvents.NewSprintChanged(header, p)
	}
	if err != nil {
		return evaluation{}, internal(err)
	}
	plan.Header = ev.Header()
	plan.Payload = ev.PayloadBytes()
	return evaluation{Result: out, Plan: plan, Ranks: rank}, nil
}
func structureHeader(id event.EventID, in commandInput, out c.StructureMutation, at f.Instant) (event.Header, error) {
	project, err := typedID[event.Project](in.Project.String())
	if err != nil {
		return event.Header{}, err
	}
	target, err := typedID[event.Aggregate](in.Target)
	if err != nil {
		return event.Header{}, err
	}
	name, aggregate, version := c.MilestoneChangedName, c.MilestoneAggregate, f.Version(0)
	if out.Sprint != nil {
		name = c.SprintChangedName
		aggregate = c.SprintAggregate
		version = out.Sprint.Version
	} else {
		version = out.Milestone.Version
	}
	return event.Header{EventID: id, EventType: name, SchemaVersion: c.WorkSchemaVersion, OccurredAt: at, Scope: event.Scope{Kind: event.ProjectScope, ProjectID: project}, AggregateType: aggregate, AggregateID: target, AggregateVersion: &version}, nil
}

func applyEvaluation(ctx context.Context, x postgres.SQLExecutor, record *commandRecord, e evaluation) error {
	in := record.Input
	if e.Ranks.Rebalanced {
		for _, row := range e.Ranks.Items {
			if row.ID == in.Target {
				continue
			}
			query := `UPDATE agenteam_work.milestones SET manual_rank=$3 WHERE project_id=$1 AND id=$2`
			args := []any{in.Project.String(), row.ID, row.Rank}
			if in.Command.IsSprint() {
				query = `UPDATE agenteam_work.sprints SET manual_rank=$4 WHERE project_id=$1 AND milestone_id=$2 AND id=$3`
				args = []any{in.Project.String(), in.parent(), row.ID, row.Rank}
			}
			if err := affected(x.Exec(ctx, query, args...)); err != nil {
				return err
			}
		}
	}
	if e.Result.Milestone != nil {
		v := e.Result.Milestone
		if in.Command.IsCreate() {
			if err := affected(x.Exec(ctx, `INSERT INTO agenteam_work.milestones(id,project_id,title,description,manual_rank,version,created_at,updated_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, v.ID.String(), v.ProjectID.String(), v.Title, v.Description, v.ManualRank, int64(v.Version), v.CreatedAt.Time(), v.UpdatedAt.Time())); err != nil {
				return err
			}
			if err := affected(x.Exec(ctx, `INSERT INTO agenteam_work.sprint_order_groups(project_id,milestone_id,order_generation) VALUES($1,$2,1)`, v.ProjectID.String(), v.ID.String())); err != nil {
				return err
			}
		} else if in.Update != nil {
			if err := affected(x.Exec(ctx, `UPDATE agenteam_work.milestones SET title=$3,description=$4,version=$5,updated_at=$6 WHERE project_id=$1 AND id=$2 AND version=$7`, v.ProjectID.String(), v.ID.String(), v.Title, v.Description, int64(v.Version), v.UpdatedAt.Time(), int64(*in.Expected))); err != nil {
				return err
			}
		} else {
			if err := affected(x.Exec(ctx, `UPDATE agenteam_work.milestones SET manual_rank=$3,version=$4,updated_at=$5 WHERE project_id=$1 AND id=$2 AND version=$6`, v.ProjectID.String(), v.ID.String(), v.ManualRank, int64(v.Version), v.UpdatedAt.Time(), int64(*in.Expected))); err != nil {
				return err
			}
		}
	} else {
		v := e.Result.Sprint
		if in.Command.IsCreate() {
			if err := affected(x.Exec(ctx, `INSERT INTO agenteam_work.sprints(id,project_id,milestone_id,title,description,manual_rank,version,created_at,updated_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)`, v.ID.String(), v.ProjectID.String(), v.MilestoneID.String(), v.Title, v.Description, v.ManualRank, int64(v.Version), v.CreatedAt.Time(), v.UpdatedAt.Time())); err != nil {
				return err
			}
		} else if in.Update != nil {
			if err := affected(x.Exec(ctx, `UPDATE agenteam_work.sprints SET title=$3,description=$4,version=$5,updated_at=$6 WHERE project_id=$1 AND id=$2 AND version=$7`, v.ProjectID.String(), v.ID.String(), v.Title, v.Description, int64(v.Version), v.UpdatedAt.Time(), int64(*in.Expected))); err != nil {
				return err
			}
		} else {
			if err := affected(x.Exec(ctx, `UPDATE agenteam_work.sprints SET manual_rank=$3,version=$4,updated_at=$5 WHERE project_id=$1 AND id=$2 AND version=$6`, v.ProjectID.String(), v.ID.String(), v.ManualRank, int64(v.Version), v.UpdatedAt.Time(), int64(*in.Expected))); err != nil {
				return err
			}
		}
	}
	if e.Plan.GroupBefore != nil {
		before := *e.Plan.GroupBefore
		after, err := nextVersion(before)
		if err != nil {
			return err
		}
		if in.Command.IsSprint() {
			return affected(x.Exec(ctx, `UPDATE agenteam_work.sprint_order_groups SET order_generation=$3 WHERE project_id=$1 AND milestone_id=$2 AND order_generation=$4`, in.Project.String(), in.parent(), int64(after), int64(before)))
		}
		return affected(x.Exec(ctx, `INSERT INTO agenteam_work.milestone_order_groups(project_id,order_generation) VALUES($1,$2) ON CONFLICT(project_id) DO UPDATE SET order_generation=EXCLUDED.order_generation WHERE agenteam_work.milestone_order_groups.order_generation=$3`, in.Project.String(), int64(after), int64(before)))
	}
	return nil
}

// This check is also used for historic completed records. It proves that the
// typed immutable result belongs to the original semantic input, without
// consulting a mutable target or treating a digest as current authorization.
func validateRecord(record *commandRecord, actor i.Actor) error {
	d, err := record.Input.semantic(actor, record.Key)
	if err != nil || d != record.Semantic {
		return internal(err)
	}
	if record.Receipt != nil {
		if err = validateResultInput(record.Input, *record.Receipt); err != nil {
			return err
		}
	}
	if record.Plan == nil {
		if record.State != "completed" || record.Receipt == nil || record.Receipt.Changed {
			return internal(nil)
		}
		return nil
	}
	p := record.Plan
	if record.EventID == nil || p.Header.EventID != *record.EventID || p.After.EventID == nil || *p.After.EventID != *record.EventID || !p.After.Changed || validateResultInput(record.Input, p.After) != nil {
		return internal(nil)
	}
	if record.Receipt != nil && !sameValue(*record.Receipt, p.After) {
		return internal(nil)
	}
	return validatePlan(record)
}
func validateResultInput(in commandInput, result c.StructureMutation) error {
	if result.Validate() != nil || result.Command != in.Command {
		return internal(nil)
	}
	if result.Milestone != nil {
		if result.Milestone.ID.String() != in.Target || result.Milestone.ProjectID != in.Project {
			return internal(nil)
		}
	} else if result.Sprint.ID.String() != in.Target || result.Sprint.ProjectID != in.Project || in.parent() != "" && result.Sprint.MilestoneID.String() != in.parent() {
		return internal(nil)
	}
	if !result.Changed {
		if in.Expected == nil {
			return internal(nil)
		}
		v := f.Version(0)
		title, description := "", ""
		if result.Milestone != nil {
			v = result.Milestone.Version
			title = result.Milestone.Title
			description = result.Milestone.Description
		} else {
			v = result.Sprint.Version
			title = result.Sprint.Title
			description = result.Sprint.Description
		}
		if v != *in.Expected {
			return internal(nil)
		}
		if in.Update != nil && (in.Update.Title != nil && *in.Update.Title != title || in.Update.Description != nil && *in.Update.Description != description) {
			return internal(nil)
		}
	}
	return nil
}

// validatePlan reconstructs every changed field and stable canonical field from
// the original typed request and recorded preimage. Positions are independently
// checked against real rows at NewFact, not accepted from this JSON alone.
func validatePlan(record *commandRecord) error {
	in, p := record.Input, record.Plan
	h := p.Header
	if h.Validate() != nil || h.Scope.Kind != event.ProjectScope || h.Scope.ProjectID.String() != in.Project.String() || h.AggregateID.String() != in.Target || h.AggregateSequence != nil || h.AggregateVersion == nil || h.SchemaVersion != c.WorkSchemaVersion {
		return internal(nil)
	}
	fields := []c.ChangedField{}
	change := c.UpdatedChange
	if in.Command.IsCreate() {
		change = c.CreatedChange
		fields = []c.ChangedField{c.DescriptionChanged, c.RankChanged, c.TitleChanged}
		if p.BeforeMilestone != nil || p.BeforeSprint != nil || in.Expected != nil {
			return internal(nil)
		}
	} else if in.Update == nil {
		change = c.ReorderedChange
		fields = []c.ChangedField{c.RankChanged}
	}
	if in.Command.IsSprint() {
		if h.EventType != c.SprintChangedName || h.AggregateType != c.SprintAggregate || p.After.Sprint == nil || p.BeforeMilestone != nil {
			return internal(nil)
		}
		after := p.After.Sprint.Clone()
		var expected c.Sprint
		if in.Command.IsCreate() {
			r := in.CreateSprint
			expected = c.Sprint{ID: r.SprintID, ProjectID: in.Project, MilestoneID: r.MilestoneID, Title: r.Title, Description: r.Description, ManualRank: after.ManualRank, Version: 1, CreatedAt: h.OccurredAt, UpdatedAt: h.OccurredAt, State: c.Planned}
		} else {
			if p.BeforeSprint == nil || in.Expected == nil || p.BeforeSprint.Version != *in.Expected || p.BeforeSprint.State == c.Completed {
				return internal(nil)
			}
			expected = p.BeforeSprint.Clone()
			if in.Update != nil {
				if in.Update.Description != nil && *in.Update.Description != expected.Description {
					expected.Description = *in.Update.Description
					fields = append(fields, c.DescriptionChanged)
				}
				if in.Update.Title != nil && *in.Update.Title != expected.Title {
					expected.Title = *in.Update.Title
					fields = append(fields, c.TitleChanged)
				}
			} else {
				expected.ManualRank = after.ManualRank
			}
			var err error
			expected.Version, err = nextVersion(expected.Version)
			if err != nil {
				return internal(err)
			}
			if h.OccurredAt.Time().Before(expected.UpdatedAt.Time()) {
				return internal(nil)
			}
			expected.UpdatedAt = h.OccurredAt
		}
		if !sameValue(expected, after) || *h.AggregateVersion != after.Version {
			return internal(nil)
		}
		var payload c.SprintChanged
		if json.Unmarshal(p.Payload, &payload) != nil || payload.CommandID != record.ID || payload.ActorUserID != record.User || payload.MilestoneID != after.MilestoneID || payload.Change != change || !sameValue(payload.ChangedFields, fields) {
			return internal(nil)
		}
		if payload.Position != nil && (payload.Position.PreviousID != nil && *payload.Position.PreviousID == after.ID || payload.Position.NextID != nil && *payload.Position.NextID == after.ID) {
			return internal(nil)
		}
		if err := validatePlanGeneration(in, p.GroupBefore, payload.Position != nil, func() f.Version {
			if payload.Position != nil {
				return payload.Position.OrderGeneration
			}
			return 0
		}()); err != nil {
			return err
		}
	} else {
		if h.EventType != c.MilestoneChangedName || h.AggregateType != c.MilestoneAggregate || p.After.Milestone == nil || p.BeforeSprint != nil {
			return internal(nil)
		}
		after := *p.After.Milestone
		var expected c.Milestone
		if in.Command.IsCreate() {
			r := in.CreateMilestone
			expected = c.Milestone{ID: r.MilestoneID, ProjectID: in.Project, Title: r.Title, Description: r.Description, ManualRank: after.ManualRank, Version: 1, CreatedAt: h.OccurredAt, UpdatedAt: h.OccurredAt}
		} else {
			if p.BeforeMilestone == nil || in.Expected == nil || p.BeforeMilestone.Version != *in.Expected {
				return internal(nil)
			}
			expected = *p.BeforeMilestone
			if in.Update != nil {
				if in.Update.Description != nil && *in.Update.Description != expected.Description {
					expected.Description = *in.Update.Description
					fields = append(fields, c.DescriptionChanged)
				}
				if in.Update.Title != nil && *in.Update.Title != expected.Title {
					expected.Title = *in.Update.Title
					fields = append(fields, c.TitleChanged)
				}
			} else {
				expected.ManualRank = after.ManualRank
			}
			var err error
			expected.Version, err = nextVersion(expected.Version)
			if err != nil {
				return internal(err)
			}
			if h.OccurredAt.Time().Before(expected.UpdatedAt.Time()) {
				return internal(nil)
			}
			expected.UpdatedAt = h.OccurredAt
		}
		if !sameValue(expected, after) || *h.AggregateVersion != after.Version {
			return internal(nil)
		}
		var payload c.MilestoneChanged
		if json.Unmarshal(p.Payload, &payload) != nil || payload.CommandID != record.ID || payload.ActorUserID != record.User || payload.Change != change || !sameValue(payload.ChangedFields, fields) {
			return internal(nil)
		}
		if payload.Position != nil && (payload.Position.PreviousID != nil && *payload.Position.PreviousID == after.ID || payload.Position.NextID != nil && *payload.Position.NextID == after.ID) {
			return internal(nil)
		}
		if err := validatePlanGeneration(in, p.GroupBefore, payload.Position != nil, func() f.Version {
			if payload.Position != nil {
				return payload.Position.OrderGeneration
			}
			return 0
		}()); err != nil {
			return err
		}
	}
	return nil
}
func validatePlanGeneration(in commandInput, before *f.Version, position bool, after f.Version) error {
	if !in.ordered() {
		if before != nil || position {
			return internal(nil)
		}
		return nil
	}
	if before == nil || !position {
		return internal(nil)
	}
	next, err := nextVersion(*before)
	if err != nil || next != after {
		return internal(err)
	}
	return nil
}
