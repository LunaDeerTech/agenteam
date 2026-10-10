package project

import (
	"context"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	c "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

func schedulerDefaultsLock(mode f.LockMode) f.LockRequest {
	key, _ := f.SystemConfigLock("project-scheduler-defaults")
	return f.LockRequest{Key: key, Mode: mode}
}
func schedulerLock(project c.ProjectID) f.LockRequest {
	key, _ := f.ProjectScheduleLock(project.String())
	return f.LockRequest{Key: key, Mode: f.Exclusive}
}
func loadSchedulerConfig(ctx context.Context, x postgres.SQLExecutor, project c.ProjectID) (c.ProjectSchedulerConfig, error) {
	var value c.ProjectSchedulerConfig
	err := x.QueryRow(ctx, `SELECT scheduler_enabled,scheduler_max_concurrency FROM agenteam_project.projects WHERE id=$1`, project.String()).Scan(&value.Enabled, &value.MaxConcurrency)
	if cancelled := ctx.Err(); cancelled != nil {
		return c.ProjectSchedulerConfig{}, cancelled
	}
	if err != nil {
		return c.ProjectSchedulerConfig{}, preparationProjectError(err)
	}
	if value.Validate() != nil {
		return c.ProjectSchedulerConfig{}, unavailable(nil)
	}
	return value, nil
}

func schedulerChangedFields(before, after c.ProjectSchedulerConfig) []c.ChangedField {
	var changed []c.ChangedField
	if before.Enabled != after.Enabled {
		changed = append(changed, c.SchedulerEnabledChanged)
	}
	if (before.MaxConcurrency == nil) != (after.MaxConcurrency == nil) || before.MaxConcurrency != nil && after.MaxConcurrency != nil && *before.MaxConcurrency != *after.MaxConcurrency {
		changed = append(changed, c.SchedulerMaxConcurrencyChanged)
	}
	return changed
}

func requireSchedulerPostimage(ctx context.Context, x postgres.SQLExecutor, project c.ProjectID, expected *c.ProjectSchedulerConfig) error {
	if expected == nil {
		return nil
	}
	actual, err := loadSchedulerConfig(ctx, x, project)
	if err != nil {
		return err
	}
	if len(schedulerChangedFields(actual, *expected)) != 0 {
		return fault(f.InvalidState)
	}
	return nil
}

// SchedulerDefaultsInTx reads the real singleton under the original defaults
// lock. This is an internal fact port, not System administrator authorization.
// Project creation holds this lock while its SQL default copies the same row.
func (a *Authority) SchedulerDefaultsInTx(ctx context.Context, tx f.Tx) (c.SystemSchedulerDefaults, error) {
	if a.state() == nil {
		return c.SystemSchedulerDefaults{}, fault(f.DependencyUnbound)
	}
	if ctx == nil || !tx.Valid() {
		return c.SystemSchedulerDefaults{}, invalid()
	}
	if err := ctx.Err(); err != nil {
		return c.SystemSchedulerDefaults{}, err
	}
	x, err := a.state().store.InTx(tx)
	if err == nil {
		err = a.state().store.RequireHeldLocks(ctx, tx, []f.LockRequest{schedulerDefaultsLock(f.Shared)})
	}
	if err != nil {
		return c.SystemSchedulerDefaults{}, preparationProjectError(err)
	}
	var value c.SystemSchedulerDefaults
	err = x.QueryRow(ctx, `SELECT default_project_scheduler_max_concurrency,version FROM agenteam_project.scheduler_defaults WHERE singleton`).Scan(&value.MaxConcurrency, &value.Version)
	if cancelled := ctx.Err(); cancelled != nil {
		return c.SystemSchedulerDefaults{}, cancelled
	}
	if err != nil {
		return c.SystemSchedulerDefaults{}, preparationProjectError(err)
	}
	if value.Version.Validate() != nil || (c.ProjectSchedulerConfig{MaxConcurrency: value.MaxConcurrency}).Validate() != nil {
		return c.SystemSchedulerDefaults{}, unavailable(nil)
	}
	return value, nil
}

// RequireSchedulerProjectInTx supplies current facts only. The Scheduler
// caller owns its private current intent and the complete lock union.
func (a *Authority) RequireSchedulerProjectInTx(ctx context.Context, tx f.Tx, project c.ProjectID) (c.SchedulerProject, error) {
	if a.state() == nil {
		return c.SchedulerProject{}, fault(f.DependencyUnbound)
	}
	if ctx == nil || !tx.Valid() || project.Validate() != nil {
		return c.SchedulerProject{}, invalid()
	}
	if err := ctx.Err(); err != nil {
		return c.SchedulerProject{}, err
	}
	if _, err := a.state().store.InTx(tx); err != nil {
		return c.SchedulerProject{}, preparationProjectError(err)
	}
	if err := a.state().store.RequireHeldLocks(ctx, tx, []f.LockRequest{projectLock(project, f.Shared), schedulerLock(project)}); err != nil {
		return c.SchedulerProject{}, preparationProjectError(err)
	}
	ref, err := a.RequirePreparingProjectInTx(ctx, tx, project)
	if err != nil {
		return c.SchedulerProject{}, err
	}
	x, err := a.state().store.InTx(tx)
	if err != nil {
		return c.SchedulerProject{}, preparationProjectError(err)
	}
	config, err := loadSchedulerConfig(ctx, x, project)
	if err != nil {
		return c.SchedulerProject{}, err
	}
	return c.SchedulerProject{Project: ref, Config: config}, nil
}

// GetSchedulerConfig uses the ordinary current Human Owner read path. The
// corresponding write is UpdateProject with an explicit Scheduler replacement.
func (s *Service) GetSchedulerConfig(ctx context.Context, actor i.Actor, project c.ProjectID) (c.SchedulerProject, error) {
	ctx, done, err := s.begin(ctx)
	if err != nil {
		return c.SchedulerProject{}, err
	}
	defer done()
	if err = human(actor); err != nil {
		return c.SchedulerProject{}, err
	}
	if project.Validate() != nil {
		return c.SchedulerProject{}, invalid()
	}
	cause, err := readCause("scheduler-config")
	if err != nil {
		return c.SchedulerProject{}, err
	}
	var value c.SchedulerProject
	result := s.state().store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := s.state().store.AcquireAll(ctx, tx, []f.LockRequest{userLock(actor.Details().UserID, f.Shared), projectLock(project, f.Shared)}); err != nil {
			return preparationProjectError(err)
		}
		x, err := s.state().deps.Authority.current(ctx, tx, actor, project, f.Shared)
		if err != nil {
			return err
		}
		p, err := ownerProject(ctx, x, actor, project)
		if err != nil {
			return err
		}
		if err = visible(p); err != nil {
			return err
		}
		config, err := loadSchedulerConfig(ctx, x, project)
		if err != nil {
			return err
		}
		value = c.SchedulerProject{Project: p.ref, Config: config}
		return nil
	})
	if err = commitError(result); err != nil {
		return c.SchedulerProject{}, err
	}
	if err = ctx.Err(); err != nil {
		return c.SchedulerProject{}, err
	}
	return value.Clone(), nil
}
