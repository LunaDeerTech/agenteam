package skill

import (
	"context"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	sc "github.com/LunaDeerTech/agenteam/internal/central/skill/contract"
)

func installationActor(actor id.Actor) (id.UserID, error) {
	if actor.Validate() != nil {
		return id.UserID{}, invalid()
	}
	if actor.Details().Kind == id.AgentRun {
		// The production Tool/Agent execution witness is not implemented here.
		return id.UserID{}, fault(f.DependencyUnbound)
	}
	if actor.Details().Kind != id.Human {
		return id.UserID{}, fault(f.Forbidden)
	}
	user, err := f.ParseID[id.User](actor.Details().UserID)
	if err != nil {
		return id.UserID{}, invalid()
	}
	return user, nil
}

// planInstallation persists an immutable command, not a successful Skill or
// permission to send an Object request. Its caller must own the complete
// physical-call lifetime and use the returned phase/current attempt unchanged.
// Every failure, including an unknown commit, returns no usable plan.
func (a *Authority) planInstallation(ctx context.Context, actor id.Actor, project id.ProjectID, key f.IdempotencyKey, request InstallRequest) (installationRow, error) {
	state := a.state()
	if state == nil {
		return installationRow{}, fault(f.DependencyUnbound)
	}
	if ctx == nil || project.Validate() != nil || key.Validate() != nil || request.Validate() != nil {
		return installationRow{}, invalid()
	}
	if err := ctx.Err(); err != nil {
		return installationRow{}, portError(err)
	}
	user, err := installationActor(actor)
	if err != nil {
		return installationRow{}, err
	}
	input := request.data()
	semantic, err := installSemantic(project, user, input)
	if err != nil {
		return installationRow{}, err
	}
	command, err := installIdentity(project, key)
	if err != nil {
		return installationRow{}, err
	}
	cause, err := f.NewCommandsCause(command)
	if err != nil {
		return installationRow{}, err
	}
	// Project EX freezes the same-domain name catalogue during creation. No
	// sequential lock extension is used after entering the transaction.
	locks, err := oc.NormalizeAccessLocks([]f.LockRequest{commandLock(command), userLock(user.String(), f.Exclusive), projectLock(project, f.Exclusive), skillLock(input.skill, f.Exclusive)})
	if err != nil {
		return installationRow{}, err
	}
	var row installationRow
	var callbackErr error
	result := state.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) (err error) {
		defer func() { callbackErr = err }()
		if err = state.store.AcquireAll(ctx, tx, locks); err != nil {
			return portError(err)
		}
		if err = state.store.RequireHeldLocks(ctx, tx, locks); err != nil {
			return portError(err)
		}
		x, err := state.store.InTx(tx)
		if err != nil {
			return portError(err)
		}
		if err = a.installationOwnerInTx(ctx, tx, actor, project, id.Read); err != nil {
			return err
		}
		current, err := loadInstallation(ctx, x, project, key)
		if err != nil {
			return err
		}
		if current != nil {
			if current.user != user {
				return fault(f.Forbidden)
			}
			if current.semantic != semantic || current.skill != input.skill {
				return fault(f.IdempotencyKeyReused)
			}
			if current.phase == installationFailed {
				return fault(f.InvalidState)
			}
			if current.phase != installationPublished {
				if err = a.installationOwnerInTx(ctx, tx, actor, project, id.Mutate); err != nil {
					return err
				}
			}
			row = *current
			return nil
		}
		if err = a.installationOwnerInTx(ctx, tx, actor, project, id.Mutate); err != nil {
			return err
		}
		if err = installationNameAvailable(ctx, x, project, input); err != nil {
			return err
		}
		installation, err := f.NewID[Installation]()
		if err != nil {
			return unavailable(err)
		}
		revision, err := f.NewID[sc.Revision]()
		if err != nil {
			return unavailable(err)
		}
		now, err := f.NewInstant(time.Now().UTC().Truncate(time.Microsecond))
		if err != nil {
			return unavailable(err)
		}
		row = installationRow{id: installation, project: project, user: user, key: key, skill: input.skill, revision: revision,
			semantic: semantic, pkg: freezeInstallation(input), phase: installationPlanned, version: 1, created: now, updated: now}
		return insertInstallation(ctx, x, row)
	})
	if result.State() == f.NotCommitted && callbackErr != nil {
		return installationRow{}, callbackErr
	}
	if err = commitError(result); err != nil {
		return installationRow{}, err
	}
	return row, nil
}

func (a *Authority) installationOwnerInTx(ctx context.Context, tx f.Tx, actor id.Actor, project id.ProjectID, intent id.AccessIntent) error {
	state := a.state()
	if state == nil {
		return fault(f.DependencyUnbound)
	}
	if _, err := installationActor(actor); err != nil {
		return err
	}
	if intent != id.Read && intent != id.Mutate {
		return fault(f.Forbidden)
	}
	grant, err := state.projects.RequireOwnerInTx(ctx, tx, actor, project, intent)
	if err != nil {
		return portError(err)
	}
	if !grant.Matches(actor, project) {
		return unavailable(nil)
	}
	return nil
}

func installationNameAvailable(ctx context.Context, x postgres.SQLExecutor, project id.ProjectID, input installInput) error {
	var conflict bool
	err := x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_skill.skills WHERE project_id=$1 AND (id=$2 OR normalized_name=$3)) OR EXISTS(SELECT 1 FROM agenteam_skill.installations WHERE skill_id=$2 OR (project_id=$1 AND normalized_name=$3 AND phase<>'failed'))`, project.String(), input.skill.String(), input.normalized).Scan(&conflict)
	if err != nil {
		return unavailable(err)
	}
	if conflict {
		return fault(f.VersionConflict)
	}
	return nil
}
