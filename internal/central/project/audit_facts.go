package project

import (
	"context"

	audit "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

// Copy the selection, not the provider's state. This opaque port has no Store
// identity API; its own checker must validate the supplied live transaction.
func copyAuditFacts(source map[audit.Producer]audit.ProjectFactAuthority) (map[audit.Producer]audit.ProjectFactAuthority, error) {
	facts := make(map[audit.Producer]audit.ProjectFactAuthority, len(source))
	for producer, provider := range source {
		switch producer {
		case audit.SecretProducer, audit.ProjectVariableProducer, audit.KnowledgeProducer, audit.ObjectProducer, audit.AccessProducer, audit.AgentProducer:
			if nilPort(provider) {
				return nil, fault(foundation.DependencyUnbound)
			}
			facts[producer] = provider
		default:
			return nil, invalid()
		}
	}
	return facts, nil
}

func (a *Authority) checkDomainAuditInTx(ctx context.Context, tx foundation.Tx, entry audit.Entry, key audit.AppendKey) error {
	f, k := entry.Fields(), key.Details()
	if f.Scope.Details().Kind != identity.ProjectScope || audit.ProducerFor(f.Action) != k.Producer {
		return fault(foundation.Forbidden)
	}
	if k.Producer == audit.ProjectVariableProducer {
		if audit.ProjectSecretVariableAction(f.Action) {
			return a.checkSecretVariableAuditInTx(ctx, tx, entry, key)
		}
		if !audit.ProjectVariableAction(f.Action) || f.Resource.Details().Kind != audit.ProjectVariableResource || f.Outcome != audit.Success || k.Ordinal != 0 || f.Actor.Details().Kind != identity.Human {
			return fault(foundation.Forbidden)
		}
		p, e := parseID[identity.Project](f.Scope.Details().ProjectID)
		if e != nil {
			return e
		}
		if _, e = a.RequireOwnerInTx(ctx, tx, f.Actor, p, identity.Mutate); e != nil {
			return e
		}
		provider := a.state().auditFacts[k.Producer]
		if nilPort(provider) {
			return fault(foundation.DependencyUnbound)
		}
		return portError(provider.CheckProjectAuditInTx(ctx, tx, entry, key))
	}
	if k.Producer == audit.KnowledgeProducer {
		return a.checkKnowledgeAuditInTx(ctx, tx, entry, key)
	}
	if k.Producer == audit.ObjectProducer {
		return a.checkObjectAuditInTx(ctx, tx, entry, key)
	}
	if k.Producer == audit.AccessProducer {
		return a.checkModelAccessAuditInTx(ctx, tx, entry, key)
	}
	if k.Producer == audit.AgentProducer {
		return a.checkAgentAuditInTx(ctx, tx, entry, key)
	}
	if k.Producer != audit.SecretProducer {
		return fault(foundation.DependencyUnbound)
	}
	if f.Resource.Details().Kind != audit.SecretResource || f.Outcome != audit.Success || k.Ordinal != 0 {
		return fault(foundation.Forbidden)
	}
	switch f.Action {
	case audit.SecretCreate, audit.SecretUpdate, audit.SecretDelete, audit.SecretResolve:
	default:
		return fault(foundation.Forbidden)
	}
	project, err := parseID[identity.Project](f.Scope.Details().ProjectID)
	if err != nil {
		return err
	}
	switch actor := f.Actor.Details(); actor.Kind {
	case identity.Human:
		// Audit's existing SecretResolve intent is also Mutate. Do not turn
		// the missing generic Human Usage lock plan into an archived read.
		if _, err = a.RequireOwnerInTx(ctx, tx, f.Actor, project, identity.Mutate); err != nil {
			return err
		}
	case identity.Service:
		if f.Action != audit.SecretResolve || actor.ServiceName != identity.SecretService || actor.ProjectID != project.String() || actor.CauseRef != k.CauseRef {
			return fault(foundation.Forbidden)
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
		initialized := c.InitializationPending
		if p.initialized {
			initialized = c.Initialized
		}
		if err = c.CheckOwnerGate(p.ref.Lifecycle, initialized, identity.Mutate); err != nil {
			return err
		}
	default:
		return fault(foundation.DependencyUnbound)
	}
	provider := a.state().auditFacts[k.Producer]
	if nilPort(provider) {
		return fault(foundation.DependencyUnbound)
	}
	// Preserve the private Secret witness and the exact Entry/Key. Project
	// proves its gate; the checker proves the real mutation or lease/AEAD step.
	return portError(provider.CheckProjectAuditInTx(ctx, tx, entry, key))
}

func (a *Authority) checkKnowledgeAuditInTx(ctx context.Context, tx foundation.Tx, entry audit.Entry, key audit.AppendKey) error {
	f, k := entry.Fields(), key.Details()
	if f.Action != audit.KnowledgeDeleteSubtree || f.Resource.Details().Kind != audit.KnowledgeDocumentResource || f.Actor.Details().Kind != identity.Human || f.Outcome != audit.Success || k.Ordinal != 0 || f.Associations != (audit.Associations{}) {
		return fault(foundation.Forbidden)
	}
	project, err := parseID[identity.Project](f.Scope.Details().ProjectID)
	if err != nil {
		return err
	}
	if _, err = a.RequireOwnerInTx(ctx, tx, f.Actor, project, identity.Mutate); err != nil {
		return err
	}
	provider := a.state().auditFacts[audit.KnowledgeProducer]
	if nilPort(provider) {
		return fault(foundation.DependencyUnbound)
	}
	// The original private witness and live Tx reach the Knowledge checker.
	// The current Owner gate alone never proves the claimed deletion facts.
	return portError(provider.CheckProjectAuditInTx(ctx, tx, entry, key))
}
