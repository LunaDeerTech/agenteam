package project

import (
	"context"

	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

func (a *Authority) checkAgentAuditInTx(ctx context.Context, tx f.Tx, entry ac.Entry, key ac.AppendKey) error {
	if ctx == nil {
		return invalid()
	}
	if err := ctx.Err(); err != nil {
		return portError(err)
	}
	e, k := entry.Fields(), key.Details()
	if !ac.AgentAction(e.Action) || e.Resource.Details().Kind != ac.AgentResource || e.Actor.Details().Kind != id.Human || e.Scope.Details().Kind != id.ProjectScope || e.Outcome != ac.Success || k.Producer != ac.AgentProducer || k.Ordinal != 0 || f.Digest(k.CauseRef).Validate() != nil || e.Associations != (ac.Associations{}) {
		return fault(f.Forbidden)
	}
	project, err := parseID[id.Project](e.Scope.Details().ProjectID)
	if err != nil {
		return err
	}
	// Read hides existence behind current ownership; Mutate then checks that the
	// project is active. Neither establishes the Agent's canonical mutation.
	if _, err = a.RequireOwnerInTx(ctx, tx, e.Actor, project, id.Read); err != nil {
		return err
	}
	if _, err = a.RequireOwnerInTx(ctx, tx, e.Actor, project, id.Mutate); err != nil {
		return err
	}
	provider := a.state().auditFacts[ac.AgentProducer]
	if nilPort(provider) {
		return fault(f.DependencyUnbound)
	}
	// Preserve the caller's original private witness context and exact Entry/Key.
	// The Agent provider validates its own Store/Tx and complete command locks.
	return portError(provider.CheckProjectAuditInTx(ctx, tx, entry, key))
}
