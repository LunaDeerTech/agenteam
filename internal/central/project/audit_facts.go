package project

import (
	"context"
	"encoding/json"

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
		case audit.MemoryProducer, audit.MCPProducer, audit.SecretProducer, audit.ProjectVariableProducer, audit.KnowledgeProducer, audit.ObjectProducer, audit.ArtifactProducer, audit.AccessProducer, audit.AgentProducer, audit.SkillProducer, audit.MeetingProducer:
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
	if k.Producer == audit.MeetingProducer {
		return a.checkMeetingAuditInTx(ctx, tx, entry, key)
	}
	if k.Producer == audit.MemoryProducer || k.Producer == audit.MCPProducer {
		return a.checkM3AuditInTx(ctx, tx, entry, key)
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
	if k.Producer == audit.ArtifactProducer {
		return a.checkArtifactAuditInTx(ctx, tx, entry, key)
	}
	if k.Producer == audit.AccessProducer {
		return a.checkModelAccessAuditInTx(ctx, tx, entry, key)
	}
	if k.Producer == audit.SkillProducer {
		if f.Actor.Details().Kind == identity.AgentRun {
			return a.checkAgentSkillAssignmentAuditInTx(ctx, tx, entry, key)
		}
		return a.checkSkillAssignmentAuditInTx(ctx, tx, entry, key)
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
	case identity.Service, identity.AgentRun:
		if actor.ProjectID != project.String() {
			return fault(foundation.Forbidden)
		}
		if actor.Kind == identity.AgentRun {
			var metadata struct {
				Consumer audit.Consumer `json:"consumer"`
			}
			if f.Action != audit.SecretResolve || json.Unmarshal(f.Metadata.JSON(), &metadata) != nil || metadata.Consumer != audit.MCP ||
				f.Associations.ExecutionID != actor.ExecutionID || f.Associations.OperationID == "" || f.Associations.ToolID == "" || f.Associations.RequestID == "" {
				return fault(foundation.Forbidden)
			}
			// This is only dispatch to Secret's mandatory private resolution
			// proof below. That proof binds the original same-Tx Usage grant,
			// current MCP invocation, retained lease and successful AEAD read.
		} else {
			if actor.ServiceName != identity.SecretService {
				return fault(foundation.Forbidden)
			}
			if f.Action == audit.SecretResolve {
				if actor.CauseRef != k.CauseRef {
					return fault(foundation.Forbidden)
				}
			} else {
				// OAuth's request cause differs from its sealed command key;
				// Secret still verifies the live write proof and postimage.
				if _, err := foundation.ParseID[struct{}](actor.CauseRef); err != nil || foundation.Digest(k.CauseRef).Validate() != nil {
					return fault(foundation.Forbidden)
				}
			}
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
		if actor.Kind == identity.Service && f.Action == audit.SecretDelete && p.initialized {
			// Only Secret's active original OAuth recovery-Delete proof may
			// retire an existing flow credential after this Project closes.
			if matched, cleanupErr := a.checkMCPOAuthCleanupAuditInTx(ctx, tx, entry, key); matched || cleanupErr != nil {
				return cleanupErr
			}
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

// Domain providers must prove the original private mutation witness. The
// independent Project gate rejects a closed Project even for an Agent fact.
func (a *Authority) checkM3AuditInTx(ctx context.Context, tx foundation.Tx, entry audit.Entry, key audit.AppendKey) error {
	e, k := entry.Fields(), key.Details()
	if k.Ordinal != 0 || e.Scope.Details().Kind != identity.ProjectScope || audit.ProducerFor(e.Action) != k.Producer || (k.Producer != audit.MemoryProducer && k.Producer != audit.MCPProducer) {
		return fault(foundation.Forbidden)
	}
	project, err := parseID[identity.Project](e.Scope.Details().ProjectID)
	if err != nil {
		return err
	}
	switch actor := e.Actor.Details(); actor.Kind {
	case identity.Human:
		if _, err = a.RequireOwnerInTx(ctx, tx, e.Actor, project, identity.Mutate); err != nil {
			return err
		}
	case identity.Service:
		if actor.ServiceName != identity.MCPRuntime || e.Action != audit.MCPDiscoveryRefresh || k.Producer != audit.MCPProducer || actor.CauseRef != k.CauseRef {
			return fault(foundation.Forbidden)
		}
		if err = a.checkMCPRuntimeProjectInTx(ctx, tx, e.Actor, project); err != nil {
			return err
		}
	case identity.AgentRun:
		if e.Action != audit.MemoryRetain || actor.ProjectID != project.String() {
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
		return fault(foundation.Forbidden)
	}
	provider := a.state().auditFacts[k.Producer]
	if nilPort(provider) {
		return fault(foundation.DependencyUnbound)
	}
	return portError(provider.CheckProjectAuditInTx(ctx, tx, entry, key))
}

// This branch selects only Skill's live assignment fact authority; an Agent DTO
// or a current project alone can never establish a canonical assignment.
func (a *Authority) checkAgentSkillAssignmentAuditInTx(ctx context.Context, tx foundation.Tx, entry audit.Entry, key audit.AppendKey) error {
	if ctx == nil {
		return invalid()
	}
	e, k := entry.Fields(), key.Details()
	actor := e.Actor.Details()
	if entry.Validate() != nil || key.Validate() != nil || actor.Kind != identity.AgentRun || k.Producer != audit.SkillProducer || !audit.SkillAssignmentAction(e.Action) || e.Scope.Details().Kind != identity.ProjectScope || actor.ProjectID != e.Scope.Details().ProjectID || k.Ordinal != 0 || foundation.Digest(k.CauseRef).Validate() != nil {
		return fault(foundation.Forbidden)
	}
	project, err := parseID[identity.Project](actor.ProjectID)
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
	initialized := c.InitializationPending
	if p.initialized {
		initialized = c.Initialized
	}
	if err = c.CheckOwnerGate(p.ref.Lifecycle, initialized, identity.Mutate); err != nil {
		return err
	}
	facts := a.state().auditFacts[audit.SkillProducer]
	if nilPort(facts) {
		return fault(foundation.DependencyUnbound)
	}
	return portError(facts.CheckProjectAuditInTx(ctx, tx, entry, key))
}
