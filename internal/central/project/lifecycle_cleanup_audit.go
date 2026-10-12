package project

import (
	"context"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

// NewLifecycleCleanupDriverWithAudit binds two fixed cleanup participants to
// one original owner. Each Run executes only the canonical current stage; a
// completed Outbox round must return before another Run can clean Audit.
// Completed Audit advances to final, without deleting the Project or binding
// a partial registry as the default lifecycle implementation.
func NewLifecycleCleanupDriverWithAudit(store Store, authority *LifecycleAuthority, processes oc.ProcessAuthority, outbox, audit c.ProjectLifecycleParticipant) (*LifecycleCleanupDriver, error) {
	if nilPort(audit) {
		return nil, fault(f.DependencyUnbound)
	}
	if name, err := cleanupParticipantName(audit); err != nil || name != c.AuditParticipant {
		return nil, fault(f.DependencyUnbound)
	}
	driver, err := NewLifecycleCleanupDriver(store, authority, processes, outbox)
	if err != nil {
		return nil, err
	}
	driver.state.audit = audit
	return driver, nil
}

// Selection is closed over the two implemented phases, not a caller-defined
// registry, stage table or permission to skip incomplete domain cleanup.
func (st *lifecycleCleanupState) cleanupProgress(r *lifecycleRecord) (c.ParticipantName, bool, error) {
	if r == nil {
		return "", false, unavailable(nil)
	}
	if r.operation.State == c.OperationCleaning && r.cleanupStage == "audit" {
		if st.audit != nil {
			return c.AuditParticipant, false, auditCleanupProgress(r)
		}
		// Preserve the original Outbox-only read of its completed checkpoint.
		for _, row := range r.participants {
			if row.stop != "stopped" || row.name != c.AuditParticipant && row.cleanup != "completed" || row.name == c.AuditParticipant && row.cleanup != "required" {
				return "", false, fault(f.InvalidState)
			}
		}
		return c.OutboxParticipant, true, nil
	}
	if st.audit != nil && r.operation.State == c.OperationCleaning && r.cleanupStage == "final" {
		// This only confirms our preceding checkpoint. It cannot run finalizer,
		// release the name, create a receipt or turn the operation completed.
		for _, row := range r.participants {
			if row.stop != "stopped" || row.cleanup != "completed" {
				return "", false, fault(f.InvalidState)
			}
		}
		return c.AuditParticipant, true, nil
	}
	return c.OutboxParticipant, false, outboxCleanupProgress(r)
}

func (st *lifecycleCleanupState) cleanupPort(name c.ParticipantName) (c.ProjectLifecycleParticipant, error) {
	switch name {
	case c.OutboxParticipant:
		return st.participant, nil
	case c.AuditParticipant:
		if !nilPort(st.audit) {
			return st.audit, nil
		}
	}
	return nil, fault(f.DependencyUnbound)
}

func cleanupStages(name c.ParticipantName) (current, next string, err error) {
	switch name {
	case c.OutboxParticipant:
		return "outbox", "audit", nil
	case c.AuditParticipant:
		return "audit", "final", nil
	default:
		return "", "", invalid()
	}
}

func (a *LifecycleAuthority) requireAuditCleanupClaimInTx(ctx context.Context, tx f.Tx, cause c.LifecycleCause, project c.ProjectID) error {
	return a.requireCleanupClaimInTx(ctx, tx, cause, project, c.AuditParticipant)
}

func validateCleanupCheckpoint(participant c.ParticipantName, checkpoint *c.CleanupCheckpoint, cause c.LifecycleCause, project c.ProjectID) error {
	switch participant {
	case c.OutboxParticipant:
		if checkpoint == nil {
			return nil
		}
	case c.AuditParticipant:
		_, err := decodeAuditCleanupCheckpoint(checkpoint, cause, c.ScopeRef{Kind: c.ProjectScope, ProjectID: project})
		return err
	}
	return unavailable(nil)
}
