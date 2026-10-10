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

type SprintLifecycleDependencies struct {
	Authority    *Authority
	Projects     pc.SprintLifecycleAuthority
	Events       oc.Appender
	SprintEvents c.SprintLifecycleEvents
	Activity     ActivityAuthority
}
type SprintLifecycleService struct {
	base     *Service
	projects pc.SprintLifecycleAuthority
	events   c.SprintLifecycleEvents
}

func NewSprintLifecycle(store Store, deps SprintLifecycleDependencies) (*SprintLifecycleService, error) {
	if nilPort(store) || deps.Authority.state() == nil || !sameStore(store, deps.Authority.state().store) || nilPort(deps.Projects) || nilPort(deps.Events) || nilPort(deps.Activity) || !deps.SprintEvents.Valid() {
		return nil, fault(f.DependencyUnbound)
	}
	// Share the existing owned-call/confirmation lifetime implementation; this
	// independent service has its own immutable dependencies and admission set.
	st := &serviceState{store: store, deps: Dependencies{Authority: deps.Authority, Events: deps.Events, Activity: deps.Activity}, calls: map[*call]struct{}{}, changed: make(chan struct{})}
	return &SprintLifecycleService{base: &Service{data: func() *serviceState { return st }}, projects: deps.Projects, events: deps.SprintEvents}, nil
}
func (s *SprintLifecycleService) bound() bool {
	return s != nil && s.base != nil && s.base.state() != nil && !nilPort(s.projects) && s.events.Valid()
}
func (s *SprintLifecycleService) Stop() {
	if s != nil && s.base != nil {
		s.base.Stop()
	}
}
func (s *SprintLifecycleService) Drain(ctx context.Context) error {
	if s == nil || s.base == nil {
		return nil
	}
	return s.base.Drain(ctx)
}
func (s *SprintLifecycleService) StartSprint(ctx context.Context, actor i.Actor, meta f.CommandMeta, project c.ProjectID, sprint c.SprintID) (c.SprintStartMutation, error) {
	if !s.bound() {
		return c.SprintStartMutation{}, fault(f.DependencyUnbound)
	}
	if err := readInput(ctx, actor, project); err != nil {
		return c.SprintStartMutation{}, err
	}
	semantic, err := c.StartSprintDigest(actor, meta, project, sprint)
	if err != nil {
		return c.SprintStartMutation{}, err
	}
	user, err := f.ParseID[i.User](actor.Details().UserID)
	if err != nil {
		return c.SprintStartMutation{}, fault(f.Unauthenticated)
	}
	in := sprintStartInput{project, sprint, user, *meta.ExpectedVersion}
	key := meta.IdempotencyKey
	ctx, entry, done, err := s.base.begin(ctx)
	if err != nil {
		return c.SprintStartMutation{}, err
	}
	defer done()
	identity, err := c.SprintStartIdentity(project, key)
	if err != nil {
		return c.SprintStartMutation{}, err
	}
	base, err := in.locks(key)
	if err != nil {
		return c.SprintStartMutation{}, err
	}
	q := c.SprintStartLookupRequest{ProjectID: project, Key: key, Semantic: semantic}
	st := s.base.state()
	for range 3 {
		var prepared *sprintStartRecord
		var completed *c.SprintStartMutation
		result := st.store.WithinTx(ctx, commandCause(identity), func(ctx context.Context, tx f.Tx) error {
			if err := st.store.AcquireAll(ctx, tx, base); err != nil {
				return portError(err)
			}
			x, ref, r, replay, err := s.current(ctx, tx, actor, q)
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
				id, e := f.NewID[c.SprintStartCommand]()
				if e != nil {
					return unavailable(e)
				}
				r = &sprintStartRecord{ID: id, Input: in, Key: key, Semantic: semantic, Revision: 1, State: "planned", Created: at}
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
			after, err := deriveSprintStart(in, before, ref, at, evID)
			if err != nil {
				return err
			}
			h, err := sprintStartHeader(after)
			if err != nil {
				return err
			}
			ev, err := s.events.NewSprintStarted(h, c.SprintStarted{CommandID: r.ID, ActorUserID: user, MilestoneID: before.MilestoneID, ProjectVersion: after.Project.Version})
			if err != nil {
				return internal(err)
			}
			r.Plan = sprintStartPlan{Before: before.Clone(), Project: ref.Clone(), After: after.Clone(), Header: ev.Header(), Payload: ev.PayloadBytes()}
			if err = validateSprintStartRecord(r, actor); err != nil {
				return err
			}
			if err = saveSprintStartPlan(ctx, x, r, insert); err != nil {
				return err
			}
			prepared = r
			return nil
		})
		if result.State() == f.Unknown {
			return s.confirm(ctx, entry, actor, q, result)
		}
		if err = txError(result); err != nil {
			return c.SprintStartMutation{}, err
		}
		if completed != nil {
			return completed.Clone(), nil
		}
		if prepared == nil {
			return c.SprintStartMutation{}, internal(nil)
		}
		ev, err := s.events.Restore(prepared.Plan.Header, prepared.Plan.Payload)
		if err != nil {
			return c.SprintStartMutation{}, internal(err)
		}
		appendPlan, err := st.deps.Events.PrepareAppend(ctx, actor, ev)
		if err != nil {
			return c.SprintStartMutation{}, portError(err)
		}
		locks, err := oc.NormalizeLocks(append(append([]f.LockRequest{}, base...), appendPlan.Locks()...))
		if err != nil {
			return c.SprintStartMutation{}, portError(err)
		}
		var output c.SprintStartMutation
		replan := false
		result = st.store.WithinTx(ctx, commandCause(identity), func(ctx context.Context, tx f.Tx) error {
			if err := st.store.AcquireAll(ctx, tx, locks); err != nil {
				return portError(err)
			}
			x, ref, r, replay, err := s.current(ctx, tx, actor, q)
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
			history, err := jsonSprintStartActor(r.Plan.After.Sprint)
			if err != nil {
				return err
			}
			v := r.Plan.After.Sprint
			if err = affected(x.Exec(ctx, `UPDATE agenteam_work.sprints SET started_at=$3,started_by=$4,version=$5,updated_at=$3 WHERE project_id=$1 AND id=$2 AND version=$6 AND started_at IS NULL AND completed_at IS NULL`, project.String(), sprint.String(), v.UpdatedAt.Time(), history, int64(v.Version), int64(in.Expected))); err != nil {
				return err
			}
			change, err := sprintStartChange(r)
			if err != nil {
				return internal(err)
			}
			witness := sprintStartWrite{authority: st.deps.Authority, tx: tx, actor: actor.Details(), id: r.ID, revision: r.Revision}
			writeCtx := context.WithValue(ctx, sprintStartWriteKey{}, witness)
			p, err := s.projects.ApplySprintStartInTx(writeCtx, tx, actor, change)
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
			if err = completeSprintStart(ctx, x, r); err != nil {
				return err
			}
			if err = st.deps.Activity.TouchActivityInTx(ctx, tx, actor); err != nil {
				return portError(err)
			}
			output = r.Plan.After.Clone()
			return nil
		})
		if result.State() == f.Unknown {
			return s.confirm(ctx, entry, actor, q, result)
		}
		if result.State() == f.NotCommitted && replan {
			continue
		}
		if err = txError(result); err != nil {
			return c.SprintStartMutation{}, err
		}
		if output.Validate() != nil {
			return c.SprintStartMutation{}, internal(nil)
		}
		return output.Clone(), nil
	}
	return c.SprintStartMutation{}, fault(f.ResourceBusy)
}
func (s *SprintLifecycleService) current(ctx context.Context, tx f.Tx, actor i.Actor, q c.SprintStartLookupRequest) (postgres.SQLExecutor, pc.ProjectRef, *sprintStartRecord, *c.SprintStartMutation, error) {
	st := s.base.state()
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
	r, err := loadSprintStart(ctx, x, q.ProjectID, q.Key)
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
		if err = validateSprintStartRecord(r, actor); err != nil {
			return nil, pc.ProjectRef{}, nil, nil, err
		}
		if r.State == "completed" {
			out := r.Receipt.Clone()
			return x, ref, r, &out, nil
		}
	}
	return x, ref, r, nil, nil
}
func (s *SprintLifecycleService) LookupStartSprint(ctx context.Context, actor i.Actor, q c.SprintStartLookupRequest) (c.SprintStartLookup, error) {
	if !s.bound() {
		return c.SprintStartLookup{}, fault(f.DependencyUnbound)
	}
	if err := readInput(ctx, actor, q.ProjectID); err != nil {
		return c.SprintStartLookup{}, err
	}
	if err := q.Validate(); err != nil {
		return c.SprintStartLookup{}, err
	}
	ctx, _, done, err := s.base.begin(ctx)
	if err != nil {
		return c.SprintStartLookup{}, err
	}
	defer done()
	return s.lookup(ctx, actor, q)
}
func (s *SprintLifecycleService) lookup(ctx context.Context, actor i.Actor, q c.SprintStartLookupRequest) (c.SprintStartLookup, error) {
	identity, err := c.SprintStartIdentity(q.ProjectID, q.Key)
	if err != nil {
		return c.SprintStartLookup{}, err
	}
	st := s.base.state()
	var out c.SprintStartLookup
	result := st.store.WithinTx(ctx, commandCause(identity), func(ctx context.Context, tx f.Tx) error {
		locks, err := oc.NormalizeLocks([]f.LockRequest{commandLock(identity), userLock(actor.Details().UserID, f.Shared), projectLock(q.ProjectID, f.Shared)})
		if err != nil {
			return portError(err)
		}
		if err = st.store.AcquireAll(ctx, tx, locks); err != nil {
			return portError(err)
		}
		_, _, record, replay, err := s.current(ctx, tx, actor, q)
		if err != nil {
			return err
		}
		switch {
		case replay != nil:
			out = c.SprintStartLookup{State: c.LookupCommitted, Result: replay}
		case record != nil:
			out.State = c.LookupInProgress
		default:
			out.State = c.LookupNotObserved
		}
		return nil
	})
	if result.State() == f.NotCommitted && ctx.Err() != nil {
		return c.SprintStartLookup{}, canceled(ctx.Err())
	}
	if err = txError(result); err != nil {
		return c.SprintStartLookup{}, err
	}
	return out, nil
}
func (s *SprintLifecycleService) confirm(ctx context.Context, entry *call, actor i.Actor, q c.SprintStartLookupRequest, original f.CommitResult) (c.SprintStartMutation, error) {
	run, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
	token := &confirmation{cancel: cancel}
	st := s.base.state()
	st.mu.Lock()
	entry.confirmations[token] = struct{}{}
	if st.stopped {
		cancel()
	}
	st.mu.Unlock()
	defer func() { cancel(); st.mu.Lock(); delete(entry.confirmations, token); st.mu.Unlock() }()
	value, err := s.lookup(run, actor, q)
	if err != nil {
		return c.SprintStartMutation{}, txError(original)
	}
	if value.State == c.LookupCommitted && value.Result != nil {
		return value.Result.Clone(), nil
	}
	if value.State == c.LookupNotObserved {
		return c.SprintStartMutation{}, notCommittedAfterUnknown(original)
	}
	return c.SprintStartMutation{}, txError(original)
}

var _ c.SprintLifecycleCommands = (*SprintLifecycleService)(nil)
