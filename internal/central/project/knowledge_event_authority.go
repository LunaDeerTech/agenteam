package project

import (
	"context"
	"encoding/json"
	"slices"

	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
)

const knowledgeEventPurpose = "project.knowledge.append-v1"

// This is only the Project gate. Outbox also invokes the Knowledge producer's
// independent command/current-fact checks in the same transaction.
func knowledgeEventBinding(request oc.ProjectRequest) (foundation.Digest, []foundation.LockRequest, error) {
	if request.Validate() != nil {
		return "", nil, invalid()
	}
	d := request.Details()
	h := d.Event.Header
	if d.Kind != oc.AppendProject || d.Event.Producer != "knowledge" || h.SchemaVersion != 1 || h.AggregateType != "knowledge_document" || (h.EventType != "knowledge.content_changed" && h.EventType != "knowledge.deleted") {
		return "", nil, fault(foundation.DependencyUnbound)
	}
	if d.Actor.Details().Kind != identity.Human || h.AggregateVersion == nil || h.AggregateVersion.Validate() != nil || h.AggregateSequence != nil || h.Scope.Kind != event.ProjectScope || h.Scope.ProjectID.String() != d.ProjectID.String() {
		return "", nil, fault(foundation.Forbidden)
	}
	// A single immutable plan is valid at both stages, without re-discovery.
	// The entire Actor includes the Session; Summary includes the payload digest.
	raw, err := json.Marshal(struct {
		Purpose string
		Actor   identity.ActorDetails
		Summary event.Summary
		Project identity.ProjectID
		Stages  [2]oc.Stage
	}{knowledgeEventPurpose, d.Actor.Details(), d.Event, d.ProjectID, [2]oc.Stage{oc.CurrentAccess, oc.NewFact}})
	if err != nil {
		return "", nil, invalid()
	}
	locks := []foundation.LockRequest{userLock(d.Actor.Details().UserID, foundation.Shared), projectLock(d.ProjectID, foundation.Shared)}
	return digest(raw), locks, nil
}

func (a *Authority) discoverKnowledgeEvent(request oc.ProjectRequest) (oc.Dependencies, error) {
	if request.Details().Stage != oc.CurrentAccess {
		return oc.Dependencies{}, fault(foundation.Forbidden)
	}
	binding, locks, err := knowledgeEventBinding(request)
	if err != nil {
		return oc.Dependencies{}, err
	}
	return oc.NewDependencies(a.state().projectIssuer, binding, locks, []byte(knowledgeEventPurpose))
}

func (a *Authority) validateKnowledgeEventInTx(ctx context.Context, tx foundation.Tx, request oc.ProjectRequest, deps oc.Dependencies) error {
	binding, locks, err := knowledgeEventBinding(request)
	if err != nil {
		return err
	}
	if !deps.Matches(a.state().projectIssuer, binding) || string(deps.Opaque()) != knowledgeEventPurpose || !slices.EqualFunc(deps.Locks(), locks, func(a, b foundation.LockRequest) bool {
		return a.Key.Canonical() == b.Key.Canonical() && a.Mode == b.Mode
	}) {
		return fault(foundation.Forbidden)
	}
	if _, err = a.state().store.InTx(tx); err != nil {
		return unavailable(err)
	}
	if err = a.state().store.RequireHeldLocks(ctx, tx, locks); err != nil {
		return unavailable(err)
	}
	d := request.Details()
	intent := identity.Read
	if d.Stage == oc.NewFact {
		intent = identity.Mutate
	}
	_, err = a.RequireOwnerInTx(ctx, tx, d.Actor, d.ProjectID, intent)
	return err
}
