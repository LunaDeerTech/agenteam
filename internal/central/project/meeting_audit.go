package project

import (
	"context"

	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

func (a *Authority) checkMeetingAuditInTx(ctx context.Context, tx f.Tx, entry ac.Entry, key ac.AppendKey) error {
	if a == nil || a.state() == nil {
		return fault(f.DependencyUnbound)
	}
	if ctx == nil || !tx.Valid() || entry.Validate() != nil || key.Validate() != nil {
		return invalid()
	}
	e, k := entry.Fields(), key.Details()
	m, err := e.Metadata.MeetingDeletionFields()
	if err != nil || !ac.MeetingAction(e.Action) || k.Producer != ac.MeetingProducer || k.CauseRef != m.DeletionID || e.Action == ac.MeetingDeleteAccepted && k.Ordinal != 0 || e.Action == ac.MeetingDeleteCompleted && k.Ordinal != 1 {
		return fault(f.Forbidden)
	}
	project, err := parseID[i.Project](e.Scope.Details().ProjectID)
	if err != nil {
		return err
	}
	if err = a.state().store.RequireHeldLocks(ctx, tx, []f.LockRequest{projectLock(project, f.Exclusive)}); err != nil {
		return unavailable(err)
	}
	if e.Action == ac.MeetingDeleteAccepted {
		if _, err = a.RequireOwnerInTx(ctx, tx, e.Actor, project, i.Mutate); err != nil {
			return err
		}
	} else {
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
		if !p.initialized || p.ref.OwnerUserID.String() != m.InitiatorID {
			return fault(f.Forbidden)
		}
	}
	provider := a.state().auditFacts[ac.MeetingProducer]
	if nilPort(provider) {
		return fault(f.DependencyUnbound)
	}
	// A valid public Service actor is only a route. The original domain must
	// prove its private live Tx, accepted intent and exact deletion postimage.
	// CheckAppendInTx additionally keeps the original Project Audit phase fence.
	return portError(provider.CheckProjectAuditInTx(ctx, tx, entry, key))
}
