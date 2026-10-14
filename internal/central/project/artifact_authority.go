package project

import (
	"context"
	"encoding/json"

	"github.com/LunaDeerTech/agenteam/internal/central/artifact"
	ac "github.com/LunaDeerTech/agenteam/internal/central/artifact/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

// ArtifactAuthority binds current Human ownership and the private cleanup
// family. Ordinary maintenance consumes Artifact's exact, live cancellation
// witness. AgentRun requires an explicitly bound current Runtime authority;
// public provenance and a bare Service label grant nothing.
type ArtifactAuthority struct {
	projects Authority
	runs     ac.AgentRunAuthority
}

func NewArtifactAuthority(projects *Authority, runs ...ac.AgentRunAuthority) (*ArtifactAuthority, error) {
	if projects.state() == nil || len(runs) > 1 || len(runs) == 1 && nilPort(runs[0]) {
		return nil, fault(f.DependencyUnbound)
	}
	a := &ArtifactAuthority{projects: *projects}
	if len(runs) == 1 {
		a.runs = runs[0]
	}
	return a, nil
}

func (a *ArtifactAuthority) Discover(ctx context.Context, subject ac.AccessSubject) (oc.AccessDependencies, error) {
	return a.discoverArtifact(ctx, f.Tx{}, subject)
}

func (a *ArtifactAuthority) discoverArtifact(ctx context.Context, tx f.Tx, subject ac.AccessSubject) (oc.AccessDependencies, error) {
	if a == nil || a.projects.state() == nil {
		return oc.AccessDependencies{}, fault(f.DependencyUnbound)
	}
	if ctx == nil || subject.Validate() != nil {
		return oc.AccessDependencies{}, invalid()
	}
	if err := ctx.Err(); err != nil {
		return oc.AccessDependencies{}, portError(err)
	}
	if subject.Actor.Details().Kind == i.AgentRun {
		return a.discoverArtifactRun(ctx, tx, subject)
	}
	if subject.ExecutionID != "" || subject.OperationID != "" || subject.ToolID != "" || subject.ToolCallID != "" {
		return oc.AccessDependencies{}, fault(f.DependencyUnbound)
	}
	locks := []f.LockRequest{projectLock(subject.ProjectID, f.Shared)}
	var actor any
	if subject.Maintenance {
		// The original maintenance callback checks a Converge service actor.
		// Plan its EX mode before the transaction; never upgrade it late.
		locks[0].Mode = f.Exclusive
	} else {
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
	deps, err := a.discoverArtifact(ctx, tx, subject)
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
	if subject.Actor.Details().Kind == i.AgentRun {
		return a.authorizeArtifactRunInTx(ctx, tx, subject, intent)
	}
	if subject.Actor.Details().Kind == i.Human {
		if intent != i.Read && intent != i.Mutate && intent != i.Converge {
			return i.AccessGrant{}, fault(f.Forbidden)
		}
		if intent == i.Converge {
			// The Artifact domain still checks the original prospective owner and
			// command. Cancellation uses current ownership in active or archived
			// Projects, without granting ordinary mutation on an archive.
			access, err := a.projects.RequireOwnerInTx(ctx, tx, subject.Actor, subject.ProjectID, i.Read)
			if err != nil {
				return i.AccessGrant{}, err
			}
			if state := access.Project().Lifecycle; state != c.Active && state != c.Archived {
				return i.AccessGrant{}, fault(f.ProjectNotActive)
			}
			scope, _ := i.InProject(subject.ProjectID)
			return i.NewAccessGrant(subject.Actor, scope, i.Converge, access.CheckedAt(), access.Project().Version)
		}
		return a.projects.AuthorizeProject(ctx, tx, subject.Actor, subject.ProjectID, intent)
	}
	if intent != i.Lifecycle && intent != i.Converge {
		return i.AccessGrant{}, fault(f.Forbidden)
	}
	w, _ := ctx.Value(lifecycleCleanupScopeKey{}).(*lifecycleCleanupScope)
	if w == nil && subject.Actor.Details().ServiceName == i.ObjectMaintenance && intent == i.Converge {
		return a.authorizeCancelledArtifactInTx(ctx, tx, subject)
	}
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

func (a *ArtifactAuthority) authorizeCancelledArtifactInTx(ctx context.Context, tx f.Tx, subject ac.AccessSubject) (i.AccessGrant, error) {
	store := a.projects.state().store
	if err := artifact.CheckCancelledUploadConvergenceInTx(ctx, tx, store, subject); err != nil {
		return i.AccessGrant{}, portError(err)
	}
	x, err := store.InTx(tx)
	if err != nil {
		return i.AccessGrant{}, unavailable(err)
	}
	if nilPort(x) {
		return i.AccessGrant{}, fault(f.DependencyUnbound)
	}
	p, err := loadProject(ctx, x, subject.ProjectID)
	if err != nil {
		return i.AccessGrant{}, err
	}
	if p == nil {
		return i.AccessGrant{}, fault(f.NotFound)
	}
	initialized := c.InitializationPending
	if p.initialized {
		initialized = c.Initialized
	}
	// Read also permits archiving, but ordinary cancellation convergence does
	// not. Deleting still consumes the exact lifecycle cleanup family above.
	if err = c.CheckOwnerGate(p.ref.Lifecycle, initialized, i.Read); err != nil {
		return i.AccessGrant{}, err
	}
	if p.ref.Lifecycle != c.Active && p.ref.Lifecycle != c.Archived {
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
	return i.NewAccessGrant(subject.Actor, scope, i.Converge, now, p.ref.Version)
}

func (a *ArtifactAuthority) CheckProjectCleanupInTx(ctx context.Context, tx f.Tx, actor i.Actor, cause oc.ProjectCleanupCause) error {
	if a == nil {
		return fault(f.DependencyUnbound)
	}
	check := ObjectCleanupAuthority{projects: a.projects}
	return check.CheckProjectCleanupInTx(ctx, tx, actor, cause)
}

var _ ac.Authority = (*ArtifactAuthority)(nil)
