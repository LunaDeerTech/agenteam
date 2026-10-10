package project

import (
	"context"
	"encoding/json"
	"slices"

	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
)

const secretVariableEventPurpose = "project.secret-variable.append-v1"

// Secret has its own exact action/event branches. The ordinary predicates and
// all other domains keep their prior authority requirements unchanged.
func (a *Authority) checkSecretVariableAuditInTx(ctx context.Context, tx f.Tx, entry ac.Entry, key ac.AppendKey) error {
	if entry.Validate() != nil || key.Validate() != nil {
		return invalid()
	}
	v, k := entry.Fields(), key.Details()
	if !ac.ProjectSecretVariableAction(v.Action) || k.Producer != ac.ProjectVariableProducer || k.Ordinal != 0 || v.Resource.Details().Kind != ac.ProjectVariableResource || v.Outcome != ac.Success || v.Actor.Details().Kind != i.Human || v.Scope.Details().Kind != i.ProjectScope || v.Associations != (ac.Associations{}) {
		return fault(f.Forbidden)
	}
	project, err := parseID[i.Project](v.Scope.Details().ProjectID)
	if err != nil {
		return err
	}
	if _, err = a.RequireOwnerInTx(ctx, tx, v.Actor, project, i.Mutate); err != nil {
		return err
	}
	provider := a.state().auditFacts[k.Producer]
	if nilPort(provider) {
		return fault(f.DependencyUnbound)
	}
	// Preserve the original context/Tx/entry/key. The producer must prove the
	// actual same-transaction D04 result and D10 canonical/history mutation.
	return portError(provider.CheckProjectAuditInTx(ctx, tx, entry, key))
}
func secretVariableEventBinding(request oc.ProjectRequest) (f.Digest, []f.LockRequest, error) {
	if request.Validate() != nil {
		return "", nil, invalid()
	}
	d := request.Details()
	h := d.Event.Header
	if d.Kind != oc.AppendProject || d.Event.Producer != "projectvariable" || h.SchemaVersion != 1 || h.EventType != "project.secret_variable_changed" || h.AggregateType != "project.variable" {
		return "", nil, fault(f.DependencyUnbound)
	}
	if d.Actor.Details().Kind != i.Human || h.AggregateVersion == nil || h.AggregateVersion.Validate() != nil || h.AggregateSequence != nil || h.Scope.Kind != event.ProjectScope || h.Scope.ProjectID.String() != d.ProjectID.String() {
		return "", nil, fault(f.Forbidden)
	}
	raw, err := json.Marshal(struct {
		Purpose string
		Actor   i.ActorDetails
		Summary event.Summary
		Project i.ProjectID
		Stages  [2]oc.Stage
	}{secretVariableEventPurpose, d.Actor.Details(), d.Event, d.ProjectID, [2]oc.Stage{oc.CurrentAccess, oc.NewFact}})
	if err != nil {
		return "", nil, invalid()
	}
	locks := []f.LockRequest{userLock(d.Actor.Details().UserID, f.Shared), projectLock(d.ProjectID, f.Shared)}
	return digest(raw), locks, nil
}
func (a *Authority) discoverSecretVariableEvent(request oc.ProjectRequest) (oc.Dependencies, error) {
	if request.Details().Stage != oc.CurrentAccess {
		return oc.Dependencies{}, fault(f.Forbidden)
	}
	binding, locks, err := secretVariableEventBinding(request)
	if err != nil {
		return oc.Dependencies{}, err
	}
	return oc.NewDependencies(a.state().projectIssuer, binding, locks, []byte(secretVariableEventPurpose))
}
func (a *Authority) validateSecretVariableEventInTx(ctx context.Context, tx f.Tx, request oc.ProjectRequest, deps oc.Dependencies) error {
	binding, locks, err := secretVariableEventBinding(request)
	if err != nil {
		return err
	}
	if !deps.Matches(a.state().projectIssuer, binding) || string(deps.Opaque()) != secretVariableEventPurpose || !slices.EqualFunc(deps.Locks(), locks, func(a, b f.LockRequest) bool { return a.Key.Canonical() == b.Key.Canonical() && a.Mode == b.Mode }) {
		return fault(f.Forbidden)
	}
	if _, err = a.state().store.InTx(tx); err != nil {
		return unavailable(err)
	}
	if err = a.state().store.RequireHeldLocks(ctx, tx, locks); err != nil {
		return unavailable(err)
	}
	d := request.Details()
	intent := i.Read
	if d.Stage == oc.NewFact {
		intent = i.Mutate
	}
	_, err = a.RequireOwnerInTx(ctx, tx, d.Actor, d.ProjectID, intent)
	return err
}
