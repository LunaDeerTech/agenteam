package project

import (
	"context"
	"encoding/json"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

// ObjectCleanupAuthority supplies only whole-Project cleanup planning and its
// original transaction gate. Object still owns the complete object/command
// mapping, one lock union, physical I/O and actual writer returns.
type ObjectCleanupAuthority struct{ projects Authority }

func NewObjectCleanupAuthority(projects *Authority) (*ObjectCleanupAuthority, error) {
	if projects.state() == nil {
		return nil, fault(f.DependencyUnbound)
	}
	if _, err := projects.lifecycleAuthority(); err != nil {
		return nil, err
	}
	return &ObjectCleanupAuthority{projects: *projects}, nil
}

func artifactCleanupActor(actor i.Actor, cause c.LifecycleCause, project c.ProjectID) error {
	if actor.Validate() != nil || cause.Validate() != nil || project.Validate() != nil {
		return invalid()
	}
	d := actor.Details()
	if d.Kind != i.Service || d.ProjectID != project.String() || d.CauseRef != cause.OperationID.String() || (d.ServiceName != i.ProjectLifecycle && d.ServiceName != i.ObjectMaintenance) {
		return fault(f.Forbidden)
	}
	return nil
}

func (a *ObjectCleanupAuthority) Discover(ctx context.Context, request oc.AccessRequest) (oc.AccessDependencies, error) {
	if a == nil || a.projects.state() == nil {
		return oc.AccessDependencies{}, fault(f.DependencyUnbound)
	}
	if ctx == nil || request.Validate() != nil {
		return oc.AccessDependencies{}, invalid()
	}
	if err := ctx.Err(); err != nil {
		return oc.AccessDependencies{}, portError(err)
	}
	d := request.Details()
	if d.Kind != oc.ProjectCleanupAccess {
		return oc.AccessDependencies{}, fault(f.DependencyUnbound)
	}
	cause, err := artifactLifecycleCause(d.ProjectCleanup)
	if err != nil {
		return oc.AccessDependencies{}, err
	}
	project := d.ProjectCleanup.Details().ProjectID
	if err = artifactCleanupActor(d.Actor, cause, project); err != nil {
		return oc.AccessDependencies{}, err
	}
	objects := make([]string, len(d.Objects))
	for n, id := range d.Objects {
		objects[n] = id.String()
	}
	// Explicit fields preserve the actual request. AccessRequest's diagnostic
	// MarshalJSON deliberately omits authority-relevant identities.
	raw, err := json.Marshal([]any{"project-artifact-cleanup-v1", d.Operation, d.Actor.Details(), project.String(), cause.OperationID.String(), cause.ProjectVersion, objects})
	if err != nil {
		return oc.AccessDependencies{}, invalid()
	}
	return oc.NewAccessDependencies(digest(raw), []f.LockRequest{projectLock(project, f.Exclusive)})
}

func (a *ObjectCleanupAuthority) ValidateInTx(ctx context.Context, tx f.Tx, request oc.AccessRequest, expected oc.AccessDependencies) error {
	actual, err := a.Discover(ctx, request)
	if err != nil {
		return err
	}
	if !actual.Equal(expected) {
		return fault(f.Forbidden)
	}
	d := request.Details()
	return a.CheckProjectCleanupInTx(ctx, tx, d.Actor, d.ProjectCleanup)
}

func (a *ObjectCleanupAuthority) CheckProjectCleanupInTx(ctx context.Context, tx f.Tx, actor i.Actor, cause oc.ProjectCleanupCause) error {
	if a == nil || a.projects.state() == nil {
		return fault(f.DependencyUnbound)
	}
	current, err := artifactLifecycleCause(cause)
	if err != nil {
		return err
	}
	project := cause.Details().ProjectID
	if err = artifactCleanupActor(actor, current, project); err != nil {
		return err
	}
	authority, err := a.projects.lifecycleAuthority()
	if err != nil {
		return err
	}
	return authority.validateArtifactCleanupInTx(ctx, tx, project, current)
}

var _ oc.AccessPlanner = (*ObjectCleanupAuthority)(nil)
