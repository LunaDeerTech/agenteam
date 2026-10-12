package project

import (
	"context"

	audit "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	c "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

func auditLifecycleCause(cause audit.LifecycleCause) (c.LifecycleCause, error) {
	if cause.Validate() != nil {
		return c.LifecycleCause{}, invalid()
	}
	d := cause.Details()
	operation, err := f.ParseID[c.Operation](d.OperationID.String())
	if err != nil {
		return c.LifecycleCause{}, invalid()
	}
	return c.LifecycleCause{OperationID: operation, Action: c.LifecycleAction(d.Action), ProjectVersion: d.ProjectVersion}, nil
}

// Both the deleting batch and Audit's read-only Unknown confirmation use this
// same transaction gate. A public cause or a stored phase is not an issuer.
func (a *LifecycleAuthority) validateAuditCleanupInTx(ctx context.Context, tx f.Tx, project c.ProjectID, cause c.LifecycleCause) error {
	if ctx == nil || project.Validate() != nil || cause.Validate() != nil {
		return invalid()
	}
	if cause.Action != c.Delete {
		return fault(f.Forbidden)
	}
	if err := ctx.Err(); err != nil {
		return portError(err)
	}
	x, err := a.lifecycleExecutor(ctx, tx, project)
	if err != nil {
		return err
	}
	if err = a.store.RequireHeldLocks(ctx, tx, []f.LockRequest{projectLock(project, f.Exclusive)}); err != nil {
		return unavailable(err)
	}
	fact, err := a.lifecycleFact(ctx, x, project, cause, c.AuditParticipant)
	if err != nil {
		return err
	}
	if fact.state != c.OperationCleaning {
		return fault(f.InvalidState)
	}
	r, err := loadLifecycleOperation(ctx, x, project, cause.OperationID)
	if err != nil {
		return err
	}
	if err = auditCleanupProgress(r); err != nil {
		return err
	}
	return a.requireAuditCleanupClaimInTx(ctx, tx, cause, project)
}

// This is only the persisted dependency predicate. Permission also requires
// the exact current Project, frozen manifest and live Audit cleanup claim.
func auditCleanupProgress(r *lifecycleRecord) error {
	if r == nil {
		return unavailable(nil)
	}
	if r.operation.Action != c.Delete || r.operation.State != c.OperationCleaning || r.cleanupStage != "audit" {
		return fault(f.InvalidState)
	}
	states := make(map[c.ParticipantName]string, len(r.participants))
	for _, row := range r.participants {
		if row.stop != "stopped" {
			return unavailable(nil)
		}
		if _, duplicate := states[row.name]; duplicate {
			return unavailable(nil)
		}
		states[row.name] = row.cleanup
	}
	entries := r.manifest.Entries()
	if len(states) != len(entries) {
		return unavailable(nil)
	}
	for _, entry := range entries {
		state, exists := states[entry.Name]
		if !exists {
			return unavailable(nil)
		}
		if entry.Name == c.AuditParticipant {
			if state != "required" && state != "pending" {
				return fault(f.InvalidState)
			}
		} else if state != "completed" {
			return fault(f.InvalidState)
		}
	}
	return nil
}
