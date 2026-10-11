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

const workEventPurpose = "project.work-structure.append-v1"

// This is the Project lifecycle/configuration gate only. Work's separate
// producer requires its original same-Tx Scheduler proof and actual writer
// postimage for both stages; a registered service or summary cannot mint it.
func schedulerWorkEvent(request oc.ProjectRequest) bool {
	d := request.Details()
	h := d.Event.Header
	return d.Event.Producer == "work" && h.EventType == "work.task_transitioned" && h.AggregateType == "work.task" && (h.SchemaVersion == 2 || h.SchemaVersion == 3 || h.SchemaVersion == 4 || h.SchemaVersion == 5)
}
func schedulerWorkEventBinding(request oc.ProjectRequest) (foundation.Digest, []foundation.LockRequest, error) {
	if request.Validate() != nil {
		return "", nil, invalid()
	}
	d := request.Details()
	h := d.Event.Header
	actor := d.Actor.Details()
	if d.Kind != oc.AppendProject || actor.Kind != identity.Service || actor.ServiceName != identity.Scheduler || actor.ProjectID != d.ProjectID.String() || h.Scope.Kind != event.ProjectScope || h.Scope.ProjectID.String() != d.ProjectID.String() || h.AggregateVersion == nil || *h.AggregateVersion < 2 || h.AggregateSequence != nil {
		return "", nil, fault(foundation.Forbidden)
	}
	if _, err := foundation.ParseID[struct{}](actor.CauseRef); err != nil {
		return "", nil, fault(foundation.Forbidden)
	}
	raw, err := json.Marshal(struct {
		Purpose string
		Actor   identity.ActorDetails
		Summary event.Summary
		Project identity.ProjectID
		Stages  [2]oc.Stage
	}{workEventPurpose, actor, d.Event, d.ProjectID, [2]oc.Stage{oc.CurrentAccess, oc.NewFact}})
	if err != nil {
		return "", nil, invalid()
	}
	schedule, _ := foundation.ProjectScheduleLock(d.ProjectID.String())
	return digest(raw), []foundation.LockRequest{projectLock(d.ProjectID, foundation.Shared), {Key: schedule, Mode: foundation.Exclusive}}, nil
}

// This proves only the Project gate. The Work producer independently proves
// its prepared command and canonical facts in the same physical transaction.
func workEventBinding(request oc.ProjectRequest) (foundation.Digest, []foundation.LockRequest, error) {
	if schedulerWorkEvent(request) {
		return schedulerWorkEventBinding(request)
	}
	if request.Validate() != nil {
		return "", nil, invalid()
	}
	d := request.Details()
	h := d.Event.Header
	if d.Kind != oc.AppendProject || d.Event.Producer != "work" || h.SchemaVersion != 1 || !(h.EventType == "work.milestone_changed" && h.AggregateType == "work.milestone" || h.EventType == "work.sprint_changed" && h.AggregateType == "work.sprint" || h.EventType == "work.sprint_started" && h.AggregateType == "work.sprint" || h.EventType == "work.task_changed" && h.AggregateType == "work.task" || h.EventType == "work.task_blockers_changed" && h.AggregateType == "work.task" || h.EventType == "work.task_transitioned" && h.AggregateType == "work.task") {
		return "", nil, fault(foundation.DependencyUnbound)
	}
	if d.Actor.Details().Kind != identity.Human || h.AggregateVersion == nil || h.AggregateVersion.Validate() != nil || h.AggregateSequence != nil || h.Scope.Kind != event.ProjectScope || h.Scope.ProjectID.String() != d.ProjectID.String() {
		return "", nil, fault(foundation.Forbidden)
	}
	// The same dependencies must authorize both Outbox stages. Binding only
	// CurrentAccess would either reject NewFact or invite an unbound re-plan.
	raw, err := json.Marshal(struct {
		Purpose string
		Actor   identity.ActorDetails
		Summary event.Summary
		Project identity.ProjectID
		Stages  [2]oc.Stage
	}{workEventPurpose, d.Actor.Details(), d.Event, d.ProjectID, [2]oc.Stage{oc.CurrentAccess, oc.NewFact}})
	if err != nil {
		return "", nil, invalid()
	}
	locks := []foundation.LockRequest{userLock(d.Actor.Details().UserID, foundation.Shared), projectLock(d.ProjectID, foundation.Shared)}
	return digest(raw), locks, nil
}
func (a *Authority) discoverWorkEvent(request oc.ProjectRequest) (oc.Dependencies, error) {
	if request.Details().Stage != oc.CurrentAccess {
		return oc.Dependencies{}, fault(foundation.Forbidden)
	}
	binding, locks, err := workEventBinding(request)
	if err != nil {
		return oc.Dependencies{}, err
	}
	return oc.NewDependencies(a.state().projectIssuer, binding, locks, []byte(workEventPurpose))
}
func (a *Authority) validateWorkEventInTx(ctx context.Context, tx foundation.Tx, request oc.ProjectRequest, deps oc.Dependencies) error {
	binding, locks, err := workEventBinding(request)
	if err != nil {
		return err
	}
	if !deps.Matches(a.state().projectIssuer, binding) || string(deps.Opaque()) != workEventPurpose || !slices.EqualFunc(deps.Locks(), locks, func(a, b foundation.LockRequest) bool {
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
	if schedulerWorkEvent(request) {
		p, e := a.RequireSchedulerProjectInTx(ctx, tx, d.ProjectID)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if e != nil {
			return e
		}
		if !p.Config.Enabled || d.Event.Header.SchemaVersion == 2 && p.Project.CurrentSprintID == nil {
			return fault(foundation.InvalidState)
		}
		return nil
	}
	intent := identity.Read
	if d.Stage == oc.NewFact {
		intent = identity.Mutate
	}
	_, err = a.RequireOwnerInTx(ctx, tx, d.Actor, d.ProjectID, intent)
	return err
}
