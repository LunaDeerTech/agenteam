package project

import (
	"context"

	audit "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

// NewRuntimeInitializationAuditAuthority composes the two dedicated audit
// routes around the same original Project authority. initialization must be
// the real Skill/Object initialization fact provider; actualRuntime must be
// the real Model Runtime's accepted Invocation/live handoff fact provider.
// Neither a provider's presence nor this composition establishes those facts.
//
// Each route retains its existing shape, same-Store transaction, held-lock,
// current Project and private-source checks. All other calls use original.
// Construction does not mutate original, acquire resources or bind an app.
func NewRuntimeInitializationAuditAuthority(original *Authority, initialization, actualRuntime audit.ProjectFactAuthority) (audit.ProjectAuthority, error) {
	initializationGate, err := NewInitializationAuditAuthority(original, initialization)
	if err != nil {
		return nil, err
	}
	runtimeGate, err := NewRuntimeAccessAuditAuthority(original, actualRuntime)
	if err != nil {
		return nil, err
	}
	return &runtimeInitializationAuditAuthority{
		original: original, initialization: initializationGate, runtime: runtimeGate,
	}, nil
}

type runtimeInitializationAuditAuthority struct {
	original       *Authority
	initialization audit.ProjectAuthority
	runtime        audit.ProjectAuthority
}

func (a *runtimeInitializationAuditAuthority) AuthorizeProject(ctx context.Context, tx f.Tx, actor i.Actor, project i.ProjectID, intent i.AccessIntent) (i.AccessGrant, error) {
	return a.original.AuthorizeProject(ctx, tx, actor, project, intent)
}

func (a *runtimeInitializationAuditAuthority) CheckServiceLookup(ctx context.Context, actor i.Actor, scope i.Scope, key audit.AppendKey) error {
	return a.original.CheckServiceLookup(ctx, actor, scope, key)
}

func (a *runtimeInitializationAuditAuthority) CheckCleanupInTx(ctx context.Context, tx f.Tx, actor i.Actor, cause audit.LifecycleCause, project i.ProjectID) error {
	return a.original.CheckCleanupInTx(ctx, tx, actor, cause, project)
}

func (a *runtimeInitializationAuditAuthority) CheckAppendInTx(ctx context.Context, tx f.Tx, entry audit.Entry, key audit.AppendKey) error {
	if !tx.Valid() || entry.Validate() != nil || key.Validate() != nil {
		return invalid()
	}
	e, k := entry.Fields(), key.Details()
	// These are routing discriminants, not grants. The selected existing
	// wrapper still validates its complete formal shape and original facts.
	switch {
	case k.Producer == audit.ObjectProducer && (e.Action == audit.ObjectUploadComplete || e.Action == audit.ObjectUploadFailed || e.Action == audit.ObjectDelete):
		return a.initialization.CheckAppendInTx(ctx, tx, entry, key)
	case k.Producer == audit.AccessProducer && e.Action == audit.AccessDeny:
		return a.runtime.CheckAppendInTx(ctx, tx, entry, key)
	default:
		return a.original.CheckAppendInTx(ctx, tx, entry, key)
	}
}

var _ audit.ProjectAuthority = (*runtimeInitializationAuditAuthority)(nil)
