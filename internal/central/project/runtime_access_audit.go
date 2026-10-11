package project

import (
	"context"

	audit "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

// NewRuntimeAccessAuditAuthority binds the actual Model Runtime's audit facts
// after Runtime construction, without replacing or mutating the original
// Project authority. It extends only Project-scoped Model AccessDeny auditing;
// all other authority methods retain that same original Project provider.
//
// facts must verify the original private live Runtime handoff, accepted
// Invocation and process in the supplied same-Store transaction. A Service
// actor, public Entry or non-nil provider is not a grant. Neither this adapter
// nor its fact callback may acquire additional locks or start another Tx.
//
// This constructor does not compose the separate initialization audit wrapper
// or bind a complete app Runtime/Project initializer graph.
func NewRuntimeAccessAuditAuthority(original *Authority, actualRuntime audit.ProjectFactAuthority) (audit.ProjectAuthority, error) {
	if original.state() == nil || nilPort(actualRuntime) {
		return nil, fault(f.DependencyUnbound)
	}
	return &runtimeAccessAuditAuthority{original: original, facts: actualRuntime}, nil
}

type runtimeAccessAuditAuthority struct {
	original *Authority
	facts    audit.ProjectFactAuthority
}

func (a *runtimeAccessAuditAuthority) AuthorizeProject(ctx context.Context, tx f.Tx, actor i.Actor, project i.ProjectID, intent i.AccessIntent) (i.AccessGrant, error) {
	return a.original.AuthorizeProject(ctx, tx, actor, project, intent)
}

func (a *runtimeAccessAuditAuthority) CheckServiceLookup(ctx context.Context, actor i.Actor, scope i.Scope, key audit.AppendKey) error {
	return a.original.CheckServiceLookup(ctx, actor, scope, key)
}

func (a *runtimeAccessAuditAuthority) CheckCleanupInTx(ctx context.Context, tx f.Tx, actor i.Actor, cause audit.LifecycleCause, project i.ProjectID) error {
	return a.original.CheckCleanupInTx(ctx, tx, actor, cause, project)
}

func (a *runtimeAccessAuditAuthority) CheckAppendInTx(ctx context.Context, tx f.Tx, entry audit.Entry, key audit.AppendKey) error {
	if !tx.Valid() || entry.Validate() != nil || key.Validate() != nil {
		return invalid()
	}
	e, k := entry.Fields(), key.Details()
	if k.Producer != audit.AccessProducer || e.Action != audit.AccessDeny {
		return a.original.CheckAppendInTx(ctx, tx, entry, key)
	}
	// Preserve the original domain-dispatch scope/producer checks before using
	// the shared exact Model denial/Project SH/current lifecycle gate.
	if e.Scope.Details().Kind != i.ProjectScope || audit.ProducerFor(e.Action) != k.Producer {
		return fault(f.Forbidden)
	}
	return a.original.checkModelAccessAuditFactsInTx(ctx, tx, entry, key, a.facts)
}

var _ audit.ProjectAuthority = (*runtimeAccessAuditAuthority)(nil)
