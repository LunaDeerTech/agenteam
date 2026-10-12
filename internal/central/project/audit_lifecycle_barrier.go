package project

import (
	"context"
	"slices"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	c "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

// This is a denial fence, not cleanup authorization. The caller's successful
// original fact check and Project lock remain required. Neither a completed
// producer nor a failed cleanup round may reopen Project Audit after its stage.
func (a *Authority) checkAuditLifecycleBarrierInTx(ctx context.Context, tx f.Tx, project c.ProjectID) error {
	if ctx == nil {
		return invalid()
	}
	if err := ctx.Err(); err != nil {
		return portError(err)
	}
	if a.state() == nil {
		return fault(f.DependencyUnbound)
	}
	if err := a.state().store.RequireHeldLocks(ctx, tx, []f.LockRequest{projectLock(project, f.Shared)}); err != nil {
		return unavailable(err)
	}
	x, err := a.state().store.InTx(tx)
	if err != nil {
		return unavailable(err)
	}
	p, err := loadProject(ctx, x, project)
	if err != nil {
		return err
	}
	if p == nil {
		return fault(f.NotFound)
	}
	if p.ref.ID != project {
		return unavailable(nil)
	}
	if p.ref.Lifecycle != c.Deleting {
		return portError(ctx.Err())
	}
	if !p.initialized || p.operation == nil {
		return unavailable(nil)
	}
	// Read the current pointer's minimal phase projection under the same lock.
	// This does not grant participant progress or replace the full manifest
	// decoder used by the cleanup driver and domain authorities.
	var current auditLifecycleStage
	var version int64
	err = x.QueryRow(ctx, `SELECT project_id::text,owner_user_id::text,action,project_version,state,COALESCE(resume_phase,''),COALESCE(cleanup_stage,'') FROM agenteam_project.lifecycle_operations WHERE id=$1 AND project_id=$2`, p.operation.String(), project.String()).Scan(&current.project, &current.owner, &current.action, &version, &current.state, &current.resume, &current.stage)
	if err != nil {
		return unavailable(err)
	}
	if err = ctx.Err(); err != nil {
		return portError(err)
	}
	current.version = f.Version(version)
	return current.check(p)
}

type auditLifecycleStage struct {
	project string
	owner   string
	action  c.LifecycleAction
	version f.Version
	state   c.OperationState
	resume  c.OperationPhase
	stage   string
}

func (s auditLifecycleStage) check(p *projectRecord) error {
	if p == nil || p.operation == nil || p.ref.Lifecycle != c.Deleting || !p.initialized || s.project != p.ref.ID.String() || s.owner != p.ref.OwnerUserID.String() || s.action != c.Delete || s.version != p.ref.Version || s.version.Validate() != nil {
		return unavailable(nil)
	}
	switch s.state {
	case c.OperationAccepted, c.OperationStopping:
		if s.resume != "" || s.stage != "" {
			return unavailable(nil)
		}
		return nil
	case c.OperationCleaning:
		if s.resume != "" {
			return unavailable(nil)
		}
	case c.OperationFailed:
		if s.resume == c.StopPhase && s.stage == "" {
			return nil
		}
		if s.resume != c.CleanupPhase {
			return unavailable(nil)
		}
	default:
		return unavailable(nil)
	}
	if !slices.Contains([]string{"domains", "outbox", "audit", "final"}, s.stage) {
		return unavailable(nil)
	}
	if s.stage == "audit" || s.stage == "final" {
		return fault(f.InvalidState)
	}
	return nil
}
