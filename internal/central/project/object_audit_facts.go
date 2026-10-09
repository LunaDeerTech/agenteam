package project

import (
	"context"

	audit "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

// Object's checker owns the private live-Tx witness and exact upload/attempt/
// cleanup facts. Its publish witness is created only after the real resource
// owner and Project gate succeed in that same transaction; metadata containing
// an initiator ID is never used here to reconstruct a Human authorization.
func (a *Authority) checkObjectAuditInTx(ctx context.Context, tx foundation.Tx, entry audit.Entry, key audit.AppendKey) error {
	f, k := entry.Fields(), key.Details()
	actor := f.Actor.Details()
	if f.Action != audit.ObjectUploadComplete && f.Action != audit.ObjectUploadFailed && f.Action != audit.ObjectDelete {
		return fault(foundation.DependencyUnbound)
	}
	if actor.Kind != identity.Service || actor.ServiceName != identity.ObjectService || actor.ProjectID != f.Scope.Details().ProjectID || actor.CauseRef != k.CauseRef || f.Resource.Details().Kind != audit.ObjectResource || f.Associations != (audit.Associations{}) {
		return fault(foundation.Forbidden)
	}
	switch f.Action {
	case audit.ObjectUploadComplete:
		if f.Outcome != audit.Success || k.Ordinal != 0 {
			return fault(foundation.Forbidden)
		}
	case audit.ObjectUploadFailed:
		if f.Outcome != audit.Unknown || k.Ordinal > 1 {
			return fault(foundation.Forbidden)
		}
	case audit.ObjectDelete:
		if f.Outcome != audit.Success || k.Ordinal != 1 {
			return fault(foundation.Forbidden)
		}
	default:
		return fault(foundation.DependencyUnbound)
	}
	project, err := parseID[identity.Project](f.Scope.Details().ProjectID)
	if err != nil {
		return err
	}
	if err = a.state().store.RequireHeldLocks(ctx, tx, []foundation.LockRequest{projectLock(project, foundation.Shared)}); err != nil {
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
		return fault(foundation.NotFound)
	}
	if !p.initialized {
		// Initialization has its own authority; this dispatcher cannot admit
		// its objects just because the actor is registered as ObjectService.
		return fault(foundation.ProjectNotActive)
	}
	if f.Action == audit.ObjectUploadComplete {
		if err = c.CheckOwnerGate(p.ref.Lifecycle, c.Initialized, identity.Mutate); err != nil {
			return err
		}
	}
	provider := a.state().auditFacts[audit.ObjectProducer]
	if nilPort(provider) {
		return fault(foundation.DependencyUnbound)
	}
	// Failure/deletion may converge existing facts after archive/delete begins.
	// Only the real Object checker can prove those facts; this is not a grant
	// of ordinary Owner Converge, a transfer route, or a lifecycle participant.
	return portError(provider.CheckProjectAuditInTx(ctx, tx, entry, key))
}
