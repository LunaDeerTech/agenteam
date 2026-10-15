package project

import (
	"context"

	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

// The caller has already held the Project lock and read the current initialized
// Project. This optional selection cannot grant a mutation: the actual Secret
// checker must prove its active same-Tx recovery Delete and canonical postimage.
// A missing witness falls back to the ordinary active-Project gate. A checker
// refusal never falls back, including for an otherwise active Project.
func (a *Authority) checkMCPOAuthCleanupAuditInTx(ctx context.Context, tx f.Tx, entry ac.Entry, key ac.AppendKey) (bool, error) {
	e, k := entry.Fields(), key.Details()
	actor := e.Actor.Details()
	if ctx == nil || entry.Validate() != nil || key.Validate() != nil || e.Action != ac.SecretDelete || e.Outcome != ac.Success ||
		e.Scope.Details().Kind != id.ProjectScope || actor.Kind != id.Service || actor.ServiceName != id.SecretService ||
		actor.ProjectID != e.Scope.Details().ProjectID || e.Resource.Details().Kind != ac.SecretResource ||
		e.Associations != (ac.Associations{}) || k.Producer != ac.SecretProducer || k.Ordinal != 0 {
		return false, fault(f.Forbidden)
	}
	provider, ok := a.state().auditFacts[ac.SecretProducer].(sc.MCPSecretCleanupAuditAuthority)
	if !ok || nilPort(provider) {
		return false, nil
	}
	matched, err := provider.CheckMCPOAuthCleanupAuditInTx(ctx, tx, entry, key)
	return matched, portError(err)
}
