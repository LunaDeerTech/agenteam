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

// ArtifactAuthority binds current Human ownership and the private cleanup
// family. Agent/Execution provenance and ordinary maintenance convergence are
// deliberately unbound; an ObjectMaintenance label alone cannot authorize it.
type ArtifactAuthority struct{ projects Authority }

func NewArtifactAuthority(projects *Authority) (*ArtifactAuthority, error) {
	if projects.state() == nil {
		return nil, fault(f.DependencyUnbound)
	}
	return &ArtifactAuthority{projects: *projects}, nil
}

func (a *ArtifactAuthority) Discover(ctx context.Context, subject ac.AccessSubject) (oc.AccessDependencies, error) {
	if a == nil || a.projects.state() == nil {
		return oc.AccessDependencies{}, fault(f.DependencyUnbound)
	}
	if ctx == nil || subject.Validate() != nil {
		return oc.AccessDependencies{}, invalid()
	}
	if err := ctx.Err(); err != nil {
		return oc.AccessDependencies{}, portError(err)
	}
	if subject.ExecutionID != "" || subject.OperationID != "" || subject.ToolID != "" || subject.ToolCallID != "" {
		return oc.AccessDependencies{}, fault(f.DependencyUnbound)
	}
	locks := []f.LockRequest{projectLock(subject.ProjectID, f.Shared)}
	var actor any
	if !subject.Maintenance {
		d := subject.Actor.Details()
		actor = d
		switch d.Kind {
		case i.Human:
			locks = append(locks, userLock(d.UserID, f.Shared))
		case i.Service:
			if d.ServiceName != i.ProjectLifecycle && d.ServiceName != i.ObjectMaintenance || d.ProjectID != subject.ProjectID.String() {
				return oc.AccessDependencies{}, fault(f.Forbidden)
			}
			locks[0].Mode = f.Exclusive
		default:
			return oc.AccessDependencies{}, fault(f.DependencyUnbound)
		}
	}
	raw, err := json.Marshal([]any{"artifact-project-parent-v1", subject.ProjectID.String(), subject.Maintenance, actor})
	if err != nil {
		return oc.AccessDependencies{}, invalid()
	}
	return oc.NewAccessDependencies(digest(raw), locks)
}

func (a *ArtifactAuthority) DiscoverInTx(ctx context.Context, tx f.Tx, subject ac.AccessSubject) (oc.AccessDependencies, error) {
	deps, err := a.Discover(ctx, subject)
	if err != nil {
		return oc.AccessDependencies{}, err
	}
	store := a.projects.state().store
	x, err := store.InTx(tx)
	if err != nil {
		return oc.AccessDependencies{}, unavailable(err)
	}
	if nilPort(x) {
		return oc.AccessDependencies{}, fault(f.DependencyUnbound)
	}
	if err = store.RequireHeldLocks(ctx, tx, deps.Locks()); err != nil {
		return oc.AccessDependencies{}, unavailable(err)
	}
	return deps, nil
}

func (a *ArtifactAuthority) AuthorizeInTx(ctx context.Context, tx f.Tx, subject ac.AccessSubject, intent i.AccessIntent) (i.AccessGrant, error) {
	if _, err := a.DiscoverInTx(ctx, tx, subject); err != nil {
		return i.AccessGrant{}, err
	}
	if subject.Maintenance {
		return i.AccessGrant{}, invalid()
	}
	if subject.Actor.Details().Kind == i.Human {
		if intent != i.Read && intent != i.Mutate && intent != i.Converge {
			return i.AccessGrant{}, fault(f.Forbidden)
		}
		return a.projects.AuthorizeProject(ctx, tx, subject.Actor, subject.ProjectID, intent)
	}
	if intent != i.Lifecycle && intent != i.Converge {
		return i.AccessGrant{}, fault(f.Forbidden)
	}
	w, _ := ctx.Value(lifecycleCleanupScopeKey{}).(*lifecycleCleanupScope)
	if w == nil || w.round.participant != c.ArtifactObjectParticipant || w.round.claim.project != subject.ProjectID {
		return i.AccessGrant{}, fault(f.Forbidden)
	}
	current := w.round.cause
	if err := artifactCleanupActor(subject.Actor, current, subject.ProjectID); err != nil {
		return i.AccessGrant{}, err
	}
	authority, err := a.projects.lifecycleAuthority()
	if err != nil {
		return i.AccessGrant{}, err
	}
	if err = authority.validateArtifactCleanupInTx(ctx, tx, subject.ProjectID, current); err != nil {
		return i.AccessGrant{}, err
	}
	x, err := a.projects.state().store.InTx(tx)
	if err != nil {
		return i.AccessGrant{}, unavailable(err)
	}
	now, err := dbNow(ctx, x)
	if err != nil {
		return i.AccessGrant{}, err
	}
	if err = ctx.Err(); err != nil {
		return i.AccessGrant{}, portError(err)
	}
	scope, _ := i.InProject(subject.ProjectID)
	return i.NewAccessGrant(subject.Actor, scope, intent, now, current.ProjectVersion)
}

func (a *ArtifactAuthority) CheckProjectCleanupInTx(ctx context.Context, tx f.Tx, actor i.Actor, cause oc.ProjectCleanupCause) error {
	if a == nil {
		return fault(f.DependencyUnbound)
	}
	check := ObjectCleanupAuthority{projects: a.projects}
	return check.CheckProjectCleanupInTx(ctx, tx, actor, cause)
}

var _ ac.Authority = (*ArtifactAuthority)(nil)
