package artifact

import (
	"context"

	ac "github.com/LunaDeerTech/agenteam/internal/central/artifact/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
)

// OwnerFileReferences lends only published Artifact identity validation to the
// caller's existing transaction. The caller owns the SQL lifetime and full
// lock union. No Artifact read session, Object access or content is produced.
type OwnerFileReferences struct{ original *Service }

type fileReferenceLockStore interface {
	Store
	RequireHeldLocks(context.Context, f.Tx, []f.LockRequest) error
}

func NewOwnerFileReferences(original *Service) (*OwnerFileReferences, error) {
	if original == nil || original.data == nil {
		return nil, failure(f.DependencyUnbound, nil)
	}
	s := original.state()
	if s == nil || s.ownership == nil || s.owners == nil || s.owners.bound() != nil || !sameProjectAuditStore(s.store, s.owners.state().store) {
		return nil, failure(f.DependencyUnbound, nil)
	}
	if _, ok := s.store.(fileReferenceLockStore); !ok {
		return nil, failure(f.DependencyUnbound, nil)
	}
	return &OwnerFileReferences{original}, nil
}

func (r *OwnerFileReferences) RequireStore(value any) error {
	actual, ok := value.(Store)
	if r == nil || r.original == nil || r.original.data == nil || !ok || !sameProjectAuditStore(r.original.state().store, actual) {
		return failure(f.DependencyUnbound, nil)
	}
	return nil
}

func (r *OwnerFileReferences) input(ctx context.Context, actor i.Actor, project i.ProjectID, artifact ac.ArtifactID) error {
	if r == nil || r.original == nil || r.original.data == nil {
		return failure(f.DependencyUnbound, nil)
	}
	if ctx == nil || project.Validate() != nil || artifact.Validate() != nil {
		return invalid()
	}
	if actor.Validate() != nil || actor.Details().Kind != i.Human {
		return failure(f.Forbidden, nil)
	}
	return portOrNil(ctx.Err())
}

func requirePublishedReference(ctx context.Context, x postgres.SQLExecutor, project i.ProjectID, artifact ac.ArtifactID) error {
	var found bool
	if err := x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_artifact.artifacts WHERE project_id=$1 AND id=$2)`, project.String(), artifact.String()).Scan(&found); err != nil {
		return unavailable(err)
	}
	if !found {
		return failure(f.NotFound, nil)
	}
	return nil
}

func fileReferenceLocks(actor i.Actor, project i.ProjectID, artifact ac.ArtifactID, dependency []f.LockRequest) ([]f.LockRequest, error) {
	userKey, _ := f.UserLock(actor.Details().UserID)
	projectKey, _ := f.ProjectLock(project.String())
	locks := []f.LockRequest{{Key: userKey, Mode: f.Shared}, {Key: projectKey, Mode: f.Shared}, artifactLock(artifact.String())}
	return oc.NormalizeLocks(append(locks, dependency...))
}

// D24's file reference is the stable ArtifactID, not a FileID or ObjectID.
// Discovery returns locks only; current authorization and published identity
// are checked again in the caller's mutation transaction.
func (r *OwnerFileReferences) FileReferenceLocks(ctx context.Context, actor i.Actor, project i.ProjectID, artifact ac.ArtifactID) ([]f.LockRequest, error) {
	if err := r.input(ctx, actor, project, artifact); err != nil {
		return nil, err
	}
	s := r.original.state()
	deps, err := s.owners.state().authority.Discover(ctx, subject(actor, project))
	if err != nil {
		return nil, portError(err)
	}
	if deps.Validate() != nil {
		return nil, unavailable(nil)
	}
	if err = requirePublishedReference(ctx, s.store, project, artifact); err != nil {
		return nil, err
	}
	return fileReferenceLocks(actor, project, artifact, deps.Locks())
}

func (r *OwnerFileReferences) RequireFileReferenceInTx(ctx context.Context, tx f.Tx, actor i.Actor, project i.ProjectID, artifact ac.ArtifactID) error {
	if err := r.input(ctx, actor, project, artifact); err != nil {
		return err
	}
	s := r.original.state()
	x, err := s.store.InTx(tx)
	if err != nil {
		return portError(err)
	}
	deps, err := s.owners.state().authority.DiscoverInTx(ctx, tx, subject(actor, project))
	if err != nil {
		return portError(err)
	}
	if deps.Validate() != nil {
		return unavailable(nil)
	}
	locks, err := fileReferenceLocks(actor, project, artifact, deps.Locks())
	if err != nil {
		return err
	}
	if err = s.store.(fileReferenceLockStore).RequireHeldLocks(ctx, tx, locks); err != nil {
		return portError(err)
	}
	if err = s.owners.authorize(ctx, tx, subject(actor, project), i.Read); err != nil {
		return err
	}
	return requirePublishedReference(ctx, x, project, artifact)
}
