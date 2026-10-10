package project

import (
	"context"
	"encoding/json"
	"slices"

	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

const executionEventPurpose = "project.execution.lifecycle.v1"

// This gate proves Project facts only. The independently registered Execution
// producer must prove its exact private run, original transaction, postimage
// and event budget. A producer name or AgentRun shape is not that proof.
func executionEventBinding(request oc.ProjectRequest) (f.Digest, []f.LockRequest, error) {
	if request.Validate() != nil {
		return "", nil, invalid()
	}
	d := request.Details()
	h := d.Event.Header
	actor := d.Actor.Details()
	if d.Kind != oc.AppendProject || d.Event.Producer != ec.ExecutionProducer || h.AggregateType != ec.ExecutionAggregate || h.SchemaVersion != 1 || !(h.EventType == ec.ExecutionStartedName || h.EventType == ec.ExecutionSucceededName || h.EventType == ec.ExecutionFailedName || h.EventType == ec.ExecutionCancelledName) {
		return "", nil, fault(f.DependencyUnbound)
	}
	if actor.Kind != i.AgentRun || actor.ProjectID != d.ProjectID.String() || actor.ExecutionID != h.AggregateID.String() || h.Scope.Kind != event.ProjectScope || h.Scope.ProjectID.String() != d.ProjectID.String() || h.AggregateVersion == nil || h.AggregateSequence != nil {
		return "", nil, fault(f.Forbidden)
	}
	raw, err := json.Marshal(struct {
		Purpose string
		Actor   i.ActorDetails
		Event   event.Summary
		Project i.ProjectID
	}{executionEventPurpose, actor, d.Event, d.ProjectID})
	if err != nil {
		return "", nil, invalid()
	}
	return digest(raw), []f.LockRequest{projectLock(d.ProjectID, f.Shared)}, nil
}
func (a *Authority) discoverExecutionEvent(request oc.ProjectRequest) (oc.Dependencies, error) {
	if request.Details().Stage != oc.CurrentAccess {
		return oc.Dependencies{}, fault(f.Forbidden)
	}
	binding, locks, err := executionEventBinding(request)
	if err != nil {
		return oc.Dependencies{}, err
	}
	return oc.NewDependencies(a.state().projectIssuer, binding, locks, []byte(executionEventPurpose))
}
func (a *Authority) validateExecutionEventInTx(ctx context.Context, tx f.Tx, request oc.ProjectRequest, deps oc.Dependencies) error {
	binding, locks, err := executionEventBinding(request)
	if err != nil {
		return err
	}
	if !deps.Matches(a.state().projectIssuer, binding) || string(deps.Opaque()) != executionEventPurpose || !slices.EqualFunc(deps.Locks(), locks, func(a, b f.LockRequest) bool { return a.Key.Canonical() == b.Key.Canonical() && a.Mode == b.Mode }) {
		return fault(f.Forbidden)
	}
	x, err := a.state().store.InTx(tx)
	if err != nil {
		return portError(err)
	}
	if err = a.state().store.RequireHeldLocks(ctx, tx, locks); err != nil {
		return portError(err)
	}
	p, err := loadProject(ctx, x, request.Details().ProjectID)
	if err != nil {
		return err
	}
	if p == nil || !p.initialized {
		return fault(f.ProjectNotActive)
	}
	if request.Details().Event.Header.EventType == ec.ExecutionStartedName && p.ref.Lifecycle != c.Active {
		return fault(f.ProjectNotActive)
	}
	// Terminal convergence may run during archive/delete. This gate does not
	// authorize any new call or bypass Outbox's own lifecycle admission gate.
	if p.ref.Lifecycle.Validate() != nil {
		return unavailable(nil)
	}
	return ctx.Err()
}
