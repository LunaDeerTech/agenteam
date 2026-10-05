package project

import (
	"context"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	obj "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

func (a *LifecycleAuthority) DiscoverProjectStop(_ context.Context, request obj.ProjectStopRequest) (obj.AccessDependencies, error) {
	if !a.bound() {
		return obj.AccessDependencies{}, fault(foundation.DependencyUnbound)
	}
	binding, err := obj.ProjectStopBinding(request)
	if err != nil {
		return obj.AccessDependencies{}, invalid()
	}
	return obj.NewAccessDependencies(binding, []foundation.LockRequest{projectLock(request.Details().Cause.Details().ProjectID, foundation.Shared)})
}

func (a *LifecycleAuthority) ValidateProjectStopInTx(ctx context.Context, tx foundation.Tx, request obj.ProjectStopRequest, dependencies obj.AccessDependencies) (obj.ProjectStopAuthorization, error) {
	var grant obj.ProjectStopAuthorization
	expected, err := a.DiscoverProjectStop(ctx, request)
	if err != nil {
		return grant, err
	}
	if !expected.Equal(dependencies) {
		return grant, fault(foundation.Forbidden)
	}
	d := request.Details()
	cause := d.Cause.Details()
	op, err := foundation.ParseID[c.Operation](cause.OperationID.String())
	if err != nil {
		return grant, invalid()
	}
	x, err := a.lifecycleExecutor(ctx, tx, cause.ProjectID)
	if err != nil {
		return grant, err
	}
	fact, err := a.lifecycleFact(ctx, x, cause.ProjectID, c.LifecycleCause{OperationID: op, Action: c.LifecycleAction(cause.Action), ProjectVersion: cause.ProjectVersion}, c.ArtifactObjectParticipant)
	if err != nil {
		return grant, err
	}
	if err = fact.authorize(d.Step == obj.InspectProjectStopStep); err != nil {
		return grant, err
	}
	mode := obj.ContinueProjectStop
	if fact.state != c.OperationStopping {
		mode = obj.ReadProjectStop
	}
	return obj.NewProjectStopAuthorization(tx, request, dependencies, mode)
}

var _ obj.ProjectStopAuthority = (*LifecycleAuthority)(nil)
