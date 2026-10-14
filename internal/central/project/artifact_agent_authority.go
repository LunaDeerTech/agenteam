package project

import (
	"context"
	"encoding/json"

	ac "github.com/LunaDeerTech/agenteam/internal/central/artifact/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

// RequireAgentRunBindingInTx is code/store binding only. Actual calls still
// require the Runtime's original private witness and current facts above.
func (a *ArtifactAuthority) RequireAgentRunBindingInTx(ctx context.Context, tx f.Tx, store any) error {
	actual, ok := store.(Store)
	if a == nil || a.projects.state() == nil || nilPort(a.runs) || !ok || !sameStore(a.projects.state().store, actual) {
		return fault(f.DependencyUnbound)
	}
	if ctx == nil || !tx.Valid() {
		return invalid()
	}
	if _, err := actual.InTx(tx); err != nil {
		return portError(err)
	}
	return portError(ctx.Err())
}

func (a *ArtifactAuthority) discoverArtifactRun(ctx context.Context, tx f.Tx, subject ac.AccessSubject) (oc.AccessDependencies, error) {
	if nilPort(a.runs) {
		return oc.AccessDependencies{}, fault(f.DependencyUnbound)
	}
	var deps oc.AccessDependencies
	var err error
	if tx.Valid() {
		deps, err = a.runs.DiscoverArtifactRunInTx(ctx, tx, subject)
	} else {
		deps, err = a.runs.DiscoverArtifactRun(ctx, subject)
	}
	if err != nil {
		return oc.AccessDependencies{}, portError(err)
	}
	if deps.Validate() != nil {
		return oc.AccessDependencies{}, fault(f.DependencyUnavailable)
	}
	raw, err := json.Marshal([]any{"artifact-agent-parent-v1", subject.Actor.Details(), subject.ProjectID.String(), subject.ExecutionID, subject.OperationID, subject.ToolID, subject.ToolCallID, deps.Mapping().String()})
	if err != nil {
		return oc.AccessDependencies{}, invalid()
	}
	locks := append(deps.Locks(), projectLock(subject.ProjectID, f.Shared))
	return oc.NewAccessDependencies(digest(raw), locks)
}

func (a *ArtifactAuthority) authorizeArtifactRunInTx(ctx context.Context, tx f.Tx, subject ac.AccessSubject, intent i.AccessIntent) (i.AccessGrant, error) {
	if intent != i.Read && intent != i.Mutate {
		return i.AccessGrant{}, fault(f.Forbidden)
	}
	if nilPort(a.runs) {
		return i.AccessGrant{}, fault(f.DependencyUnbound)
	}
	if err := a.runs.RequireArtifactRunInTx(ctx, tx, subject, intent); err != nil {
		return i.AccessGrant{}, portError(err)
	}
	x, err := a.projects.state().store.InTx(tx)
	if err != nil {
		return i.AccessGrant{}, unavailable(err)
	}
	p, err := loadProject(ctx, x, subject.ProjectID)
	if err != nil {
		return i.AccessGrant{}, err
	}
	if p == nil {
		return i.AccessGrant{}, fault(f.NotFound)
	}
	if p.ref.Lifecycle != c.Active || !p.initialized {
		return i.AccessGrant{}, fault(f.ProjectNotActive)
	}
	now, err := dbNow(ctx, x)
	if err != nil {
		return i.AccessGrant{}, err
	}
	if err = ctx.Err(); err != nil {
		return i.AccessGrant{}, portError(err)
	}
	scope, _ := i.InProject(subject.ProjectID)
	return i.NewAccessGrant(subject.Actor, scope, intent, now, p.ref.Version)
}
