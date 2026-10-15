package project

import (
	"context"
	"encoding/json"
	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/meeting/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/outbox"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	"slices"
)

const meetingEventPurpose = "project.meeting.lifecycle.v1"

func meetingEventBinding(request oc.ProjectRequest) (f.Digest, []f.LockRequest, error) {
	if request.Validate() != nil {
		return "", nil, invalid()
	}
	d := request.Details()
	h := d.Event.Header
	a := d.Actor.Details()
	if d.Kind != oc.AppendProject || d.Event.Producer != mc.MeetingProducer || h.SchemaVersion != 1 {
		return "", nil, fault(f.DependencyUnbound)
	}
	if h.Scope.Kind != event.ProjectScope || h.Scope.ProjectID.String() != d.ProjectID.String() || h.AggregateSequence != nil || h.AggregateVersion == nil || h.AggregateVersion.Validate() != nil {
		return "", nil, fault(f.Forbidden)
	}
	locks := []f.LockRequest{projectLock(d.ProjectID, f.Shared)}
	if meetingDecisionEvent(h.EventType) {
		if h.AggregateType != mc.DecisionAggregate {
			return "", nil, fault(f.Forbidden)
		}
	} else if h.AggregateType != mc.MeetingAggregate {
		return "", nil, fault(f.Forbidden)
	}
	switch h.EventType {
	case mc.ProposedName, mc.DecisionRequestedName:
		if *h.AggregateVersion != 1 || a.Kind != i.AgentRun || a.ProjectID != d.ProjectID.String() {
			return "", nil, fault(f.Forbidden)
		}
	case mc.ActivatedName, mc.RestoredName, mc.ArchivedName, mc.DecisionAnsweredName, mc.DecisionSkippedName:
		if a.Kind != i.Human {
			return "", nil, fault(f.Forbidden)
		}
		locks = append(locks, userLock(a.UserID, f.Shared))
	case mc.DecisionCancelledName:
		if a.Kind == i.Human {
			locks = append(locks, userLock(a.UserID, f.Shared))
		} else if a.ProjectID != d.ProjectID.String() || a.Kind != i.AgentRun && (a.Kind != i.Service || a.ServiceName != i.MeetingRuntime) {
			return "", nil, fault(f.Forbidden)
		}
	case mc.DeletedName:
		if a.Kind == i.Human {
			locks = append(locks, userLock(a.UserID, f.Shared))
		} else if a.Kind != i.Service || a.ServiceName != i.MeetingRuntime || a.ProjectID != d.ProjectID.String() {
			return "", nil, fault(f.Forbidden)
		}
	default:
		return "", nil, fault(f.DependencyUnbound)
	}
	raw, err := json.Marshal(struct {
		Purpose string
		Actor   i.ActorDetails
		Summary event.Summary
		Project i.ProjectID
	}{meetingEventPurpose, a, d.Event, d.ProjectID})
	if err != nil {
		return "", nil, invalid()
	}
	locks, err = oc.NormalizeLocks(locks)
	return digest(raw), locks, err
}

const meetingDeliveryPurpose = "project.meeting.inbox-delivery.v1"

func meetingDeliveryBinding(request oc.ProjectRequest) (f.Digest, []f.LockRequest, error) {
	if request.Validate() != nil {
		return "", nil, invalid()
	}
	d := request.Details()
	h := d.Event.Header
	if d.Kind != oc.DeliverProject || d.Event.Producer != mc.MeetingProducer || d.Delivery.Effect != oc.CanonicalConverge || h.SchemaVersion != 1 || h.AggregateVersion == nil || h.AggregateSequence != nil {
		return "", nil, fault(f.Forbidden)
	}
	if meetingDecisionEvent(h.EventType) {
		if d.Delivery.HandlerID != "humaninbox.meeting_decision" || h.AggregateType != mc.DecisionAggregate {
			return "", nil, fault(f.Forbidden)
		}
	} else {
		if d.Delivery.HandlerID != "humaninbox.meeting_proposal" || h.AggregateType != mc.MeetingAggregate {
			return "", nil, fault(f.Forbidden)
		}
		switch h.EventType {
		case mc.ProposedName, mc.ActivatedName, mc.RestoredName, mc.ArchivedName, mc.DeletedName:
		default:
			return "", nil, fault(f.DependencyUnbound)
		}
	}
	raw, err := json.Marshal(struct {
		Purpose  string
		Actor    i.ActorDetails
		Summary  event.Summary
		Delivery oc.DeliveryIdentity
	}{meetingDeliveryPurpose, d.Actor.Details(), d.Event, d.Delivery})
	if err != nil {
		return "", nil, invalid()
	}
	return digest(raw), []f.LockRequest{projectLock(d.ProjectID, f.Shared)}, nil
}
func (a *Authority) discoverMeetingDelivery(ctx context.Context, request oc.ProjectRequest) (oc.Dependencies, error) {
	if err := outbox.RequireProjectDelivery(ctx, f.Tx{}, a.state().store, request); err != nil {
		return oc.Dependencies{}, err
	}
	binding, locks, err := meetingDeliveryBinding(request)
	if err != nil {
		return oc.Dependencies{}, err
	}
	return oc.NewDependencies(a.state().projectIssuer, binding, locks, []byte(meetingDeliveryPurpose))
}
func (a *Authority) validateMeetingDeliveryInTx(ctx context.Context, tx f.Tx, request oc.ProjectRequest, deps oc.Dependencies) error {
	if err := outbox.RequireProjectDelivery(ctx, tx, a.state().store, request); err != nil {
		return err
	}
	binding, locks, err := meetingDeliveryBinding(request)
	if err != nil {
		return err
	}
	if !deps.Matches(a.state().projectIssuer, binding) || string(deps.Opaque()) != meetingDeliveryPurpose || !slices.EqualFunc(deps.Locks(), locks, func(a, b f.LockRequest) bool { return a.Key.Canonical() == b.Key.Canonical() && a.Mode == b.Mode }) {
		return fault(f.Forbidden)
	}
	return a.validateMeetingDeliveryProjectInTx(ctx, tx, request.Details().ProjectID, locks)
}
func (a *Authority) validateMeetingDeliveryProjectInTx(ctx context.Context, tx f.Tx, project i.ProjectID, locks []f.LockRequest) error {
	if err := a.state().store.RequireHeldLocks(ctx, tx, locks); err != nil {
		return portError(err)
	}
	x, err := a.state().store.InTx(tx)
	if err != nil {
		return portError(err)
	}
	p, err := loadProject(ctx, x, project)
	if err != nil {
		return err
	}
	// Canonical Inbox convergence may complete while a Project archives; it
	// cannot start new work and stops when deletion begins.
	if p == nil || !p.initialized || p.ref.Lifecycle == pc.Deleting {
		return fault(f.ProjectNotActive)
	}
	return ctx.Err()
}
func (a *Authority) discoverMeetingEvent(request oc.ProjectRequest) (oc.Dependencies, error) {
	if request.Details().Stage != oc.CurrentAccess {
		return oc.Dependencies{}, fault(f.Forbidden)
	}
	binding, locks, err := meetingEventBinding(request)
	if err != nil {
		return oc.Dependencies{}, err
	}
	return oc.NewDependencies(a.state().projectIssuer, binding, locks, []byte(meetingEventPurpose))
}
func (a *Authority) validateMeetingEventInTx(ctx context.Context, tx f.Tx, request oc.ProjectRequest, deps oc.Dependencies) error {
	binding, locks, err := meetingEventBinding(request)
	if err != nil {
		return err
	}
	if !deps.Matches(a.state().projectIssuer, binding) || string(deps.Opaque()) != meetingEventPurpose || !slices.EqualFunc(deps.Locks(), locks, func(a, b f.LockRequest) bool { return a.Key.Canonical() == b.Key.Canonical() && a.Mode == b.Mode }) {
		return fault(f.Forbidden)
	}
	x, err := a.state().store.InTx(tx)
	if err != nil {
		return portError(err)
	}
	if err = a.state().store.RequireHeldLocks(ctx, tx, locks); err != nil {
		return portError(err)
	}
	d := request.Details()
	if d.Actor.Details().Kind == i.Human {
		intent := i.Read
		if d.Stage == oc.NewFact {
			intent = i.Mutate
		}
		_, err = a.RequireOwnerInTx(ctx, tx, d.Actor, d.ProjectID, intent)
		return err
	}
	p, err := loadProject(ctx, x, d.ProjectID)
	if err != nil {
		return err
	}
	if p == nil || !p.initialized {
		return fault(f.ProjectNotActive)
	}
	if d.Event.Header.EventType != mc.DeletedName && d.Event.Header.EventType != mc.DecisionCancelledName && p.ref.Lifecycle != pc.Active {
		return fault(f.ProjectNotActive)
	}
	// The separately registered Meeting producer proves its private original
	// tool/deletion witness, exact same Tx and canonical postimage. This gate
	// alone grants no source mutation and never reads Meeting private tables.
	return ctx.Err()
}

func meetingDecisionEvent(name event.StableName) bool {
	switch name {
	case mc.DecisionRequestedName, mc.DecisionAnsweredName, mc.DecisionSkippedName, mc.DecisionCancelledName:
		return true
	default:
		return false
	}
}
