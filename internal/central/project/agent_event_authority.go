package project

import (
	"context"
	"encoding/json"
	"slices"

	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
)

const agentEventPurpose = "project.agent.append-v1"

// Project proves only its current Owner/lifecycle gate. Agent's separately
// registered producer proves the exact command/payload and private applied
// witness through Outbox's existing CurrentAccess/NewFact producer stages.
func agentEventBinding(request oc.ProjectRequest) (f.Digest, []f.LockRequest, error) {
	if request.Validate() != nil {
		return "", nil, invalid()
	}
	d := request.Details()
	h := d.Event.Header
	if d.Kind != oc.AppendProject || d.Event.Producer != "agent" || h.EventType != "agent.config_changed" || h.AggregateType != "agent.config" || h.SchemaVersion != 1 {
		return "", nil, fault(f.DependencyUnbound)
	}
	if d.Actor.Details().Kind != id.Human || h.AggregateVersion == nil || h.AggregateVersion.Validate() != nil || h.AggregateSequence != nil || h.Scope.Kind != event.ProjectScope || h.Scope.ProjectID.String() != d.ProjectID.String() {
		return "", nil, fault(f.Forbidden)
	}
	raw, err := json.Marshal(struct {
		Purpose string
		Actor   id.ActorDetails
		Summary event.Summary
		Project id.ProjectID
		Stages  [2]oc.Stage
	}{agentEventPurpose, d.Actor.Details(), d.Event, d.ProjectID, [2]oc.Stage{oc.CurrentAccess, oc.NewFact}})
	if err != nil {
		return "", nil, invalid()
	}
	locks := []f.LockRequest{userLock(d.Actor.Details().UserID, f.Shared), projectLock(d.ProjectID, f.Shared)}
	return digest(raw), locks, nil
}
func (a *Authority) discoverAgentEvent(ctx context.Context, request oc.ProjectRequest) (oc.Dependencies, error) {
	if ctx == nil {
		return oc.Dependencies{}, invalid()
	}
	if err := ctx.Err(); err != nil {
		return oc.Dependencies{}, portError(err)
	}
	if request.Details().Stage != oc.CurrentAccess {
		return oc.Dependencies{}, fault(f.Forbidden)
	}
	binding, locks, err := agentEventBinding(request)
	if err != nil {
		return oc.Dependencies{}, err
	}
	return oc.NewDependencies(a.state().projectIssuer, binding, locks, []byte(agentEventPurpose))
}
func (a *Authority) validateAgentEventInTx(ctx context.Context, tx f.Tx, request oc.ProjectRequest, deps oc.Dependencies) error {
	if ctx == nil {
		return invalid()
	}
	if err := ctx.Err(); err != nil {
		return portError(err)
	}
	binding, locks, err := agentEventBinding(request)
	if err != nil {
		return err
	}
	if !deps.Matches(a.state().projectIssuer, binding) || string(deps.Opaque()) != agentEventPurpose || !slices.EqualFunc(deps.Locks(), locks, func(a, b f.LockRequest) bool { return a.Key.Canonical() == b.Key.Canonical() && a.Mode == b.Mode }) {
		return fault(f.Forbidden)
	}
	if _, err = a.state().store.InTx(tx); err != nil {
		return unavailable(err)
	}
	if err = a.state().store.RequireHeldLocks(ctx, tx, locks); err != nil {
		return unavailable(err)
	}
	d := request.Details()
	intent := id.Read
	if d.Stage == oc.NewFact {
		intent = id.Mutate
	}
	_, err = a.RequireOwnerInTx(ctx, tx, d.Actor, d.ProjectID, intent)
	return err
}
