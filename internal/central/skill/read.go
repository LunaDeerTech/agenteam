package skill

import (
	"context"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	sc "github.com/LunaDeerTech/agenteam/internal/central/skill/contract"
)

// Every directory call checks today's Owner/Session/Project. The fixed builtin
// directory is bounded by one row; a missing initialization is not a ready,
// empty Skill catalogue and is never manufactured into a protected revision.
func (s *Service) ListSkills(ctx context.Context, actor id.Actor, project id.ProjectID) ([]sc.Metadata, error) {
	call, e := s.begin(ctx, false)
	if e != nil {
		return nil, e
	}
	defer s.end(call)
	var out sc.Metadata
	e = s.ownerReadTx(call.ctx, actor, project, func(ctx context.Context, _ f.Tx, x postgres.SQLExecutor, row initializationRow) error {
		var err error
		out, _, err = loadPublished(ctx, x, row)
		return err
	})
	if e != nil {
		return nil, e
	}
	return []sc.Metadata{out}, nil
}
func (s *Service) GetSkill(ctx context.Context, actor id.Actor, project id.ProjectID, skill sc.SkillID) (sc.Metadata, error) {
	if skill.Validate() != nil {
		return sc.Metadata{}, invalid()
	}
	call, e := s.begin(ctx, false)
	if e != nil {
		return sc.Metadata{}, e
	}
	defer s.end(call)
	var out sc.Metadata
	e = s.ownerReadTx(call.ctx, actor, project, func(ctx context.Context, _ f.Tx, x postgres.SQLExecutor, row initializationRow) error {
		if row.skill != skill {
			installed, err := loadInstallationSkill(ctx, x, project, skill)
			if err != nil {
				return err
			}
			if installed == nil || installed.phase != installationPublished {
				return fault(f.NotFound)
			}
			out, _, err = loadInstalled(ctx, x, *installed)
			return err
		}
		var err error
		out, _, err = loadPublished(ctx, x, row)
		return err
	}, skill)
	if e != nil {
		return sc.Metadata{}, e
	}
	return out, nil
}
func (s *Service) ownerReadTx(ctx context.Context, actor id.Actor, project id.ProjectID, work func(context.Context, f.Tx, postgres.SQLExecutor, initializationRow) error, targets ...sc.SkillID) error {
	state := s.state()
	if state == nil {
		return fault(f.DependencyUnbound)
	}
	if actor.Validate() != nil || project.Validate() != nil {
		return invalid()
	}
	if actor.Details().Kind == id.AgentRun {
		return fault(f.DependencyUnbound)
	}
	if actor.Details().Kind != id.Human {
		return fault(f.Forbidden)
	}
	store := state.authority.state().store
	locks := []f.LockRequest{userLock(actor.Details().UserID, f.Shared), projectLock(project, f.Shared)}
	// Candidate parent facts are not returned before current authorization.
	discovered, _ := loadInitialization(ctx, store, project)
	if discovered != nil && discovered.request.ProjectID == project {
		locks = append(locks, skillLock(discovered.skill, f.Shared))
	}
	for _, target := range targets {
		if target.Validate() != nil {
			return invalid()
		}
		locks = append(locks, skillLock(target, f.Shared))
	}
	locks, e := oc.NormalizeAccessLocks(locks)
	if e != nil {
		return e
	}
	attempt, e := f.NewID[f.TransactionAttempt]()
	if e != nil {
		return unavailable(e)
	}
	cause, e := f.NewJobCause("skill-read", project.String(), attempt.String())
	if e != nil {
		return e
	}
	var callbackErr error
	result := store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) (err error) {
		defer func() { callbackErr = err }()
		if e := store.AcquireAll(ctx, tx, locks); e != nil {
			return portError(e)
		}
		x, e := store.InTx(tx)
		if e != nil {
			return portError(e)
		}
		grant, e := state.authority.state().projects.RequireOwnerInTx(ctx, tx, actor, project, id.Read)
		if e != nil {
			return portError(e)
		}
		if !grant.Matches(actor, project) {
			return unavailable(nil)
		}
		row, e := loadInitialization(ctx, x, project)
		if e != nil {
			return e
		}
		if row == nil || row.phase != initializationPublished {
			return fault(f.InvalidState)
		}
		if row.request.ProjectID != project {
			return unavailable(nil)
		}
		if e = store.RequireHeldLocks(ctx, tx, []f.LockRequest{skillLock(row.skill, f.Shared)}); e != nil {
			return portError(e)
		}
		return work(ctx, tx, x, *row)
	})
	if result.State() == f.NotCommitted && callbackErr != nil {
		return callbackErr
	}
	return commitError(result)
}
