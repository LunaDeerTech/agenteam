package project

import (
	"context"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	c "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

// Every original D06 transaction checks both current canonical progress and
// the live cleanup driver's exact claim. A persisted phase or public cause
// alone is never permission to delete an event.
func (a *LifecycleAuthority) validateOutboxCleanupInTx(ctx context.Context, tx f.Tx, project c.ProjectID, cause c.LifecycleCause) error {
	if cause.Action != c.Delete {
		return fault(f.Forbidden)
	}
	x, err := a.lifecycleExecutor(ctx, tx, project)
	if err != nil {
		return err
	}
	fact, err := a.lifecycleFact(ctx, x, project, cause, c.OutboxParticipant)
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
	if err = outboxCleanupProgress(r); err != nil {
		return err
	}
	return a.requireOutboxCleanupClaimInTx(ctx, tx, cause, project)
}

// This predicate only checks the strict decoder's complete persisted rows.
// Its success is not an owner witness; the transaction gate above also needs
// the original driver's private active context and exact running claim.
func outboxCleanupProgress(r *lifecycleRecord) error {
	if r == nil {
		return unavailable(nil)
	}
	if r.operation.Action != c.Delete || r.operation.State != c.OperationCleaning || r.cleanupStage != "outbox" {
		return fault(f.InvalidState)
	}
	states := make(map[c.ParticipantName]string, len(r.participants))
	for _, row := range r.participants {
		if row.stop != "stopped" {
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
		switch entry.Name {
		case c.OutboxParticipant:
			if state != "required" && state != "pending" {
				return fault(f.InvalidState)
			}
		case c.AuditParticipant:
			if state != "required" {
				return fault(f.InvalidState)
			}
		default:
			if state != "completed" {
				return fault(f.InvalidState)
			}
		}
	}
	return nil
}
