package work

import (
	"context"
	"time"

	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
)

func (s *SprintLifecycleService) CompleteSprint(ctx context.Context, actor i.Actor, meta f.CommandMeta, project c.ProjectID, sprint c.SprintID) (c.SprintCompleteMutation, error) {
	if !s.completionBound() {
		return c.SprintCompleteMutation{}, fault(f.DependencyUnbound)
	}
	if err := readInput(ctx, actor, project); err != nil {
		return c.SprintCompleteMutation{}, err
	}
	semantic, err := c.CompleteSprintDigest(actor, meta, project, sprint)
	if err != nil {
		return c.SprintCompleteMutation{}, err
	}
	user, err := f.ParseID[i.User](actor.Details().UserID)
	if err != nil {
		return c.SprintCompleteMutation{}, fault(f.Unauthenticated)
	}
	in := sprintCompleteInput{project, sprint, user, *meta.ExpectedVersion}
	key := meta.IdempotencyKey
	ctx, entry, done, err := s.base.begin(ctx)
	if err != nil {
		return c.SprintCompleteMutation{}, err
	}
	defer done()
	identity, err := c.SprintCompleteIdentity(project, key)
	if err != nil {
		return c.SprintCompleteMutation{}, err
	}
	base, err := in.locks(key)
	if err != nil {
		return c.SprintCompleteMutation{}, err
	}
	q := c.SprintCompleteLookupRequest{ProjectID: project, Key: key, Semantic: semantic}
	st := s.base.state()
	entry.project, entry.class = project, projectCallMutation
	if st.ownership != nil {
		ctx, err = st.ownership.admit(ctx, entry, actor, projectCallSprint)
		if err != nil {
			return c.SprintCompleteMutation{}, err
		}
	}
	for range 3 {
		var prepared *sprintCompleteRecord
		var completed *c.SprintCompleteMutation
		result := st.store.WithinTx(ctx, commandCause(identity), func(ctx context.Context, tx f.Tx) error {
			if err := st.store.AcquireAll(ctx, tx, base); err != nil {
				return portError(err)
			}
			x, ref, r, replay, err := s.currentCompletion(ctx, tx, actor, q)
			if err != nil {
				return err
			}
			if replay != nil {
				completed = replay
				return nil
			}
			if _, err = st.deps.Authority.state().projects.RequireOwnerInTx(ctx, tx, actor, project, i.Mutate); err != nil {
				return portError(err)
			}
			before, err := loadSprint(ctx, x, project, sprint, ref.CurrentSprintID)
			if err != nil {
				return err
			}
			if err = s.checkCompletionMembers(ctx, tx, x, project, sprint); err != nil {
				return err
			}
			at, err := dbNow(ctx, x)
			if err != nil {
				return err
			}
			if at.Time().Before(before.UpdatedAt.Time()) {
				at = before.UpdatedAt
			}
			if at.Time().Before(ref.UpdatedAt.Time()) {
				at = ref.UpdatedAt
			}
			insert := r == nil
			if insert {
				id, e := f.NewID[c.SprintCompleteCommand]()
				if e != nil {
					return unavailable(e)
				}
				r = &sprintCompleteRecord{ID: id, Input: in, Key: key, Semantic: semantic, Revision: 1, State: "planned", Created: at}
			} else {
				r.Revision, err = nextVersion(r.Revision)
				if err != nil {
					return err
				}
				if at.Time().Before(r.Created.Time()) {
					at = r.Created
				}
			}
			evID, err := f.NewID[event.EventIdentity]()
			if err != nil {
				return unavailable(err)
			}
			after, err := deriveSprintComplete(in, before, ref, at, evID)
			if err != nil {
				return err
			}
			h, err := sprintCompleteHeader(after)
			if err != nil {
				return err
			}
			ev, err := s.completion.Events.NewSprintCompleted(h, c.SprintCompleted{CommandID: r.ID, ActorUserID: user, MilestoneID: before.MilestoneID, ProjectVersion: after.Project.Version})
			if err != nil {
				return internal(err)
			}
			r.Plan = sprintCompletePlan{Before: before.Clone(), Project: ref.Clone(), After: after.Clone(), Header: ev.Header(), Payload: ev.PayloadBytes()}
			if err = validateSprintCompleteRecord(r, actor); err != nil {
				return err
			}
			if err = saveSprintCompletePlan(ctx, x, r, insert); err != nil {
				return err
			}
			prepared = r
			return nil
		})
		if result.State() == f.Unknown {
			return s.confirmCompletion(ctx, entry, actor, q, result)
		}
		if err = txError(result); err != nil {
			return c.SprintCompleteMutation{}, err
		}
		if completed != nil {
			return completed.Clone(), nil
		}
		if prepared == nil {
			return c.SprintCompleteMutation{}, internal(nil)
		}
		ev, err := s.completion.Events.Restore(prepared.Plan.Header, prepared.Plan.Payload)
		if err != nil {
			return c.SprintCompleteMutation{}, internal(err)
		}
		appendPlan, err := st.deps.Events.PrepareAppend(ctx, actor, ev)
		if err != nil {
			return c.SprintCompleteMutation{}, portError(err)
		}
		locks, err := oc.NormalizeLocks(append(append([]f.LockRequest{}, base...), appendPlan.Locks()...))
		if err != nil {
			return c.SprintCompleteMutation{}, portError(err)
		}
		var output c.SprintCompleteMutation
		replan := false
		result = st.store.WithinTx(ctx, commandCause(identity), func(ctx context.Context, tx f.Tx) error {
			if err := st.store.AcquireAll(ctx, tx, locks); err != nil {
				return portError(err)
			}
			x, ref, r, replay, err := s.currentCompletion(ctx, tx, actor, q)
			if err != nil {
				return err
			}
			if replay != nil {
				output = replay.Clone()
				return nil
			}
			if _, err = st.deps.Authority.state().projects.RequireOwnerInTx(ctx, tx, actor, project, i.Mutate); err != nil {
				return portError(err)
			}
			if r == nil || r.ID != prepared.ID || r.Revision != prepared.Revision || !sameValue(r.Plan, prepared.Plan) {
				replan = true
				return errReplan
			}
			before, err := loadSprint(ctx, x, project, sprint, ref.CurrentSprintID)
			if err != nil {
				return err
			}
			if !sameValue(before, r.Plan.Before) || !sameValue(ref, r.Plan.Project) {
				replan = true
				return errReplan
			}
			if err = s.checkCompletionMembers(ctx, tx, x, project, sprint); err != nil {
				return err
			}
			history, err := jsonSprintCompleteActor(r.Plan.After.Sprint)
			if err != nil {
				return err
			}
			v := r.Plan.After.Sprint
			if err = affected(x.Exec(ctx, `UPDATE agenteam_work.sprints SET completed_at=$3,completed_by=$4,version=$5,updated_at=$3 WHERE project_id=$1 AND id=$2 AND version=$6 AND started_at IS NOT NULL AND completed_at IS NULL`, project.String(), sprint.String(), v.UpdatedAt.Time(), history, int64(v.Version), int64(in.Expected))); err != nil {
				return err
			}
			change, err := sprintCompleteChange(r)
			if err != nil {
				return internal(err)
			}
			witness := sprintCompleteWrite{authority: st.deps.Authority, tx: tx, actor: actor.Details(), id: r.ID, revision: r.Revision}
			writeCtx := context.WithValue(ctx, sprintCompleteWriteKey{}, witness)
			p, err := s.completion.Projects.ApplySprintCompleteInTx(writeCtx, tx, actor, change)
			if err != nil {
				return portError(err)
			}
			if !sameValue(p, r.Plan.After.Project) {
				return internal(nil)
			}
			receipt, err := st.deps.Events.AppendEventInTx(ctx, tx, actor, ev, appendPlan)
			if err != nil {
				return portError(err)
			}
			if receipt.EventID != r.Plan.After.EventID || receipt.Sequence.Validate() != nil {
				return internal(nil)
			}
			if err = completeSprintComplete(ctx, x, r); err != nil {
				return err
			}
			if err = st.deps.Activity.TouchActivityInTx(ctx, tx, actor); err != nil {
				return portError(err)
			}
			output = r.Plan.After.Clone()
			return nil
		})
		if result.State() == f.Unknown {
			return s.confirmCompletion(ctx, entry, actor, q, result)
		}
		if result.State() == f.NotCommitted && replan {
			continue
		}
		if err = txError(result); err != nil {
			return c.SprintCompleteMutation{}, err
		}
		if output.Validate() != nil {
			return c.SprintCompleteMutation{}, internal(nil)
		}
		return output.Clone(), nil
	}
	return c.SprintCompleteMutation{}, fault(f.ResourceBusy)
}
func (s *SprintLifecycleService) currentCompletion(ctx context.Context, tx f.Tx, actor i.Actor, q c.SprintCompleteLookupRequest) (postgres.SQLExecutor, pc.ProjectRef, *sprintCompleteRecord, *c.SprintCompleteMutation, error) {
	st := s.base.state()
	if st.ownership != nil {
		if err := st.ownership.requireRunning(ctx, tx, q.ProjectID, projectCallSprint); err != nil {
			return nil, pc.ProjectRef{}, nil, nil, err
		}
	}
	x, err := st.store.InTx(tx)
	if err != nil {
		return nil, pc.ProjectRef{}, nil, nil, portError(err)
	}
	access, err := st.deps.Authority.state().projects.RequireOwnerInTx(ctx, tx, actor, q.ProjectID, i.Read)
	if err != nil {
		return nil, pc.ProjectRef{}, nil, nil, portError(err)
	}
	ref := access.Project()
	if ref.ID != q.ProjectID {
		return nil, pc.ProjectRef{}, nil, nil, internal(nil)
	}
	r, err := loadSprintComplete(ctx, x, q.ProjectID, q.Key)
	if err != nil {
		return nil, pc.ProjectRef{}, nil, nil, err
	}
	if r != nil {
		if r.Input.User.String() != actor.Details().UserID {
			return nil, pc.ProjectRef{}, nil, nil, fault(f.NotFound)
		}
		if r.Semantic != q.Semantic {
			return nil, pc.ProjectRef{}, nil, nil, fault(f.IdempotencyKeyReused)
		}
		if err = validateSprintCompleteRecord(r, actor); err != nil {
			return nil, pc.ProjectRef{}, nil, nil, err
		}
		if r.State == "completed" {
			out := r.Receipt.Clone()
			return x, ref, r, &out, nil
		}
	}
	return x, ref, r, nil, nil
}
func (s *SprintLifecycleService) LookupCompleteSprint(ctx context.Context, actor i.Actor, q c.SprintCompleteLookupRequest) (c.SprintCompleteLookup, error) {
	if !s.completionBound() {
		return c.SprintCompleteLookup{}, fault(f.DependencyUnbound)
	}
	if err := readInput(ctx, actor, q.ProjectID); err != nil {
		return c.SprintCompleteLookup{}, err
	}
	if err := q.Validate(); err != nil {
		return c.SprintCompleteLookup{}, err
	}
	ctx, entry, done, err := s.base.begin(ctx)
	if err != nil {
		return c.SprintCompleteLookup{}, err
	}
	defer done()
	entry.project, entry.class = q.ProjectID, projectCallRead
	if owned := s.base.state().ownership; owned != nil {
		ctx, err = owned.admit(ctx, entry, actor, projectCallSprint)
		if err != nil {
			return c.SprintCompleteLookup{}, err
		}
	}
	return s.lookupCompletion(ctx, actor, q)
}
func (s *SprintLifecycleService) lookupCompletion(ctx context.Context, actor i.Actor, q c.SprintCompleteLookupRequest) (c.SprintCompleteLookup, error) {
	identity, err := c.SprintCompleteIdentity(q.ProjectID, q.Key)
	if err != nil {
		return c.SprintCompleteLookup{}, err
	}
	st := s.base.state()
	var out c.SprintCompleteLookup
	result := st.store.WithinTx(ctx, commandCause(identity), func(ctx context.Context, tx f.Tx) error {
		locks, err := oc.NormalizeLocks([]f.LockRequest{commandLock(identity), userLock(actor.Details().UserID, f.Shared), projectLock(q.ProjectID, f.Shared)})
		if err != nil {
			return portError(err)
		}
		if err = st.store.AcquireAll(ctx, tx, locks); err != nil {
			return portError(err)
		}
		_, _, record, replay, err := s.currentCompletion(ctx, tx, actor, q)
		if err != nil {
			return err
		}
		switch {
		case replay != nil:
			out = c.SprintCompleteLookup{State: c.LookupCommitted, Result: replay}
		case record != nil:
			out.State = c.LookupInProgress
		default:
			out.State = c.LookupNotObserved
		}
		return nil
	})
	if result.State() == f.NotCommitted && ctx.Err() != nil {
		return c.SprintCompleteLookup{}, canceled(ctx.Err())
	}
	if err = txError(result); err != nil {
		return c.SprintCompleteLookup{}, err
	}
	return out, nil
}
func (s *SprintLifecycleService) confirmCompletion(ctx context.Context, entry *call, actor i.Actor, q c.SprintCompleteLookupRequest, original f.CommitResult) (c.SprintCompleteMutation, error) {
	run, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
	token := &confirmation{cancel: cancel}
	st := s.base.state()
	st.mu.Lock()
	entry.confirmations[token] = struct{}{}
	if st.stopped {
		cancel()
	}
	st.mu.Unlock()
	if st.ownership != nil {
		st.ownership.confirmation(entry, token, true)
	}
	defer func() {
		cancel()
		if st.ownership != nil {
			st.ownership.confirmation(entry, token, false)
		}
		st.mu.Lock()
		delete(entry.confirmations, token)
		st.mu.Unlock()
	}()
	value, err := s.lookupCompletion(run, actor, q)
	if err != nil {
		return c.SprintCompleteMutation{}, txError(original)
	}
	if value.State == c.LookupCommitted && value.Result != nil {
		return value.Result.Clone(), nil
	}
	if value.State == c.LookupNotObserved {
		return c.SprintCompleteMutation{}, notCommittedAfterUnknown(original)
	}
	return c.SprintCompleteMutation{}, txError(original)
}

var _ c.SprintCompletionCommands = (*SprintLifecycleService)(nil)
