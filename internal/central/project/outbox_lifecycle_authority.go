package project

import (
	"context"
	"slices"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

func outboxLifecycleCause(cause oc.LifecycleCause) (c.LifecycleCause, error) {
	if cause.Validate() != nil {
		return c.LifecycleCause{}, invalid()
	}
	d := cause.Details()
	id, err := foundation.ParseID[c.Operation](d.OperationID.String())
	return c.LifecycleCause{OperationID: id, Action: c.LifecycleAction(d.Action), ProjectVersion: d.ProjectVersion}, err
}

func (a *LifecycleAuthority) Discover(_ context.Context, request oc.ProjectRequest) (oc.Dependencies, error) {
	if !a.bound() {
		return oc.Dependencies{}, fault(foundation.DependencyUnbound)
	}
	if request.Validate() != nil {
		return oc.Dependencies{}, invalid()
	}
	d := request.Details()
	if d.Kind != oc.LifecycleProject {
		return oc.Dependencies{}, fault(foundation.DependencyUnbound)
	}
	cause, err := outboxLifecycleCause(d.Lifecycle)
	if err != nil {
		return oc.Dependencies{}, err
	}
	project, err := lifecycleActorProject(d.Actor, cause)
	if err != nil {
		return oc.Dependencies{}, err
	}
	if project != d.ProjectID {
		return oc.Dependencies{}, fault(foundation.Forbidden)
	}
	binding, err := oc.LifecycleBinding(request)
	if err != nil {
		return oc.Dependencies{}, invalid()
	}
	return oc.NewDependencies(a.issuer, binding, []foundation.LockRequest{projectLock(project, foundation.Shared)}, nil)
}

func (a *LifecycleAuthority) ValidateInTx(ctx context.Context, tx foundation.Tx, request oc.ProjectRequest, dependencies oc.Dependencies) error {
	expected, err := a.Discover(ctx, request)
	if err != nil {
		return err
	}
	if !dependencies.Matches(a.issuer, expected.Binding()) || !slices.EqualFunc(dependencies.Locks(), expected.Locks(), func(a, b foundation.LockRequest) bool {
		return a.Key.Canonical() == b.Key.Canonical() && a.Mode == b.Mode
	}) || len(dependencies.Opaque()) != 0 {
		return fault(foundation.Forbidden)
	}
	d := request.Details()
	if d.LifecycleStep == oc.LifecycleCleanup {
		return fault(foundation.DependencyUnbound)
	}
	x, err := a.lifecycleExecutor(ctx, tx, d.ProjectID)
	if err != nil {
		return err
	}
	cause, _ := outboxLifecycleCause(d.Lifecycle)
	fact, err := a.lifecycleFact(ctx, x, d.ProjectID, cause, c.OutboxParticipant)
	if err != nil {
		return err
	}
	return fact.authorize(d.LifecycleStep == oc.LifecycleInspect)
}

func (a *LifecycleAuthority) ResolveLifecycleActor(ctx context.Context, cause oc.LifecycleCause) (identity.Actor, error) {
	if !a.bound() {
		return identity.Actor{}, fault(foundation.DependencyUnbound)
	}
	current, err := outboxLifecycleCause(cause)
	if err != nil {
		return identity.Actor{}, err
	}
	project := cause.Details().ProjectID
	txCause, err := readCause("lifecycle-actor")
	if err != nil {
		return identity.Actor{}, err
	}
	result := a.store.WithinTx(ctx, txCause, func(ctx context.Context, tx foundation.Tx) error {
		if err := a.store.AcquireAll(ctx, tx, []foundation.LockRequest{projectLock(project, foundation.Shared)}); err != nil {
			return unavailable(err)
		}
		x, err := a.lifecycleExecutor(ctx, tx, project)
		if err != nil {
			return err
		}
		_, err = a.lifecycleFact(ctx, x, project, current, "")
		return err
	})
	if err := commitError(result); err != nil {
		return identity.Actor{}, err
	}
	registration, _ := identity.RegisterService(identity.ProjectLifecycle)
	scope, _ := identity.InProject(project)
	return registration.Actor(current.OperationID.String(), scope)
}

func (a *Authority) lifecycleAuthority() (*LifecycleAuthority, error) {
	if a.state() != nil {
		if facts, ok := a.state().lifecycle.(*LifecycleAuthority); ok && facts.bound() && sameStore(a.state().store, facts.store) {
			return facts, nil
		}
	}
	return nil, fault(foundation.DependencyUnbound)
}

func (a *Authority) ResolveLifecycleActor(ctx context.Context, cause oc.LifecycleCause) (identity.Actor, error) {
	facts, err := a.lifecycleAuthority()
	if err != nil {
		return identity.Actor{}, err
	}
	return facts.ResolveLifecycleActor(ctx, cause)
}

var _ oc.ProjectAuthority = (*LifecycleAuthority)(nil)
var _ oc.LifecycleActorResolver = (*LifecycleAuthority)(nil)
var _ oc.LifecycleActorResolver = (*Authority)(nil)
