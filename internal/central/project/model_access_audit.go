package project

import (
	"context"
	"encoding/json"

	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

// The registered Runtime supplies accepted Invocation/handoff facts. This
// Project gate neither grants a Model invocation nor relaxes an outbound deny.
// D04 already owns Project SH in its original Audit transaction. In particular,
// no call/consumer locks or a second transaction may be added by this callback.
func (a *Authority) checkModelAccessAuditInTx(ctx context.Context, tx f.Tx, entry ac.Entry, key ac.AppendKey) error {
	return a.checkModelAccessAuditFactsInTx(ctx, tx, entry, key, a.state().auditFacts[ac.AccessProducer])
}

// Both the original immutable map and the dedicated Runtime audit adapter use
// the same Project gate. Selecting a provider does not establish its private
// Invocation/handoff facts; those remain the provider's original-Tx proof.
func (a *Authority) checkModelAccessAuditFactsInTx(ctx context.Context, tx f.Tx, entry ac.Entry, key ac.AppendKey, provider ac.ProjectFactAuthority) error {
	e, k := entry.Fields(), key.Details()
	actor := e.Actor.Details()
	var metadata struct {
		Consumer ac.Consumer `json:"consumer"`
	}
	if e.Action != ac.AccessDeny || e.Outcome != ac.Denied || e.Resource.Details().Kind != ac.PolicyResource || k.Ordinal != 0 || actor.Kind != id.Service || actor.ServiceName != id.OutboundService || actor.ProjectID != e.Scope.Details().ProjectID || actor.CauseRef != k.CauseRef || json.Unmarshal(e.Metadata.JSON(), &metadata) != nil || metadata.Consumer != ac.Model {
		return fault(f.Forbidden)
	}
	project, err := parseID[id.Project](actor.ProjectID)
	if err != nil {
		return err
	}
	if err = a.state().store.RequireHeldLocks(ctx, tx, []f.LockRequest{projectLock(project, f.Shared)}); err != nil {
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
	initialization := pc.InitializationPending
	if p.initialized {
		initialization = pc.Initialized
	}
	if err = pc.CheckOwnerGate(p.ref.Lifecycle, initialization, id.Mutate); err != nil {
		return err
	}
	if nilPort(provider) {
		return fault(f.DependencyUnbound)
	}
	return portError(provider.CheckProjectAuditInTx(ctx, tx, entry, key))
}
