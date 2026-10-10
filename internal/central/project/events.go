package project

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	"reflect"

	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	c "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	"github.com/jackc/pgx/v5"
)

func eventHeader(id event.EventID, project c.ProjectID, kind event.StableName, version foundation.Version, at foundation.Instant) (event.Header, error) {
	scope, e := foundation.ParseID[event.Project](project.String())
	if e != nil {
		return event.Header{}, e
	}
	aggregate, e := foundation.ParseID[event.Aggregate](project.String())
	if e != nil {
		return event.Header{}, e
	}
	return event.Header{EventID: id, EventType: kind, SchemaVersion: c.ProjectEventSchemaVersion, OccurredAt: at, Scope: event.Scope{Kind: event.ProjectScope, ProjectID: scope}, AggregateType: c.ProjectAggregate, AggregateID: aggregate, AggregateVersion: &version}, nil
}
func (s *Service) updateEvent(ref c.ProjectRef, changed []c.ChangedField, at foundation.Instant) (event.Event, error) {
	id, e := foundation.NewID[event.EventIdentity]()
	if e != nil {
		return event.Event{}, unavailable(e)
	}
	h, e := eventHeader(id, ref.ID, c.UpdatedEventName, ref.Version, at)
	if e != nil {
		return event.Event{}, e
	}
	return s.state().deps.ProjectEvents.NewUpdated(h, c.UpdatedPayload{ChangedFields: changed})
}
func (s *Service) createdEvent(r *creationRecord, at foundation.Instant) (event.Event, error) {
	id, e := parseID[event.EventIdentity](r.eventID)
	if e != nil {
		return event.Event{}, e
	}
	h, e := eventHeader(id, r.operation.ProjectID, c.CreatedEventName, 1, at)
	if e != nil {
		return event.Event{}, e
	}
	return s.state().deps.ProjectEvents.NewCreated(h, c.CreatedPayload{OwnerUserID: r.owner, CreationID: r.operation.ID})
}
func appendBinding(actor identity.Actor, summary event.Summary) (foundation.Digest, error) {
	if actor.Validate() != nil || summary.Header.Validate() != nil || summary.PayloadDigest.Validate() != nil || summary.Producer != c.ProjectProducer {
		return "", invalid()
	}
	raw, e := json.Marshal(struct {
		Actor   identity.ActorDetails
		Summary event.Summary
	}{actor.Details(), summary})
	if e != nil {
		return "", invalid()
	}
	return digest(raw), nil
}
func canonicalPayload(kind event.StableName, raw []byte) ([]byte, error) {
	switch kind {
	case c.CreatedEventName:
		var p c.CreatedPayload
		if json.Unmarshal(raw, &p) != nil {
			return nil, unavailable(nil)
		}
		raw, e := json.Marshal(p)
		if e != nil {
			return nil, e
		}
		return cursor.CanonicalJSON(raw)
	case c.UpdatedEventName:
		var p c.UpdatedPayload
		if json.Unmarshal(raw, &p) != nil {
			return nil, unavailable(nil)
		}
		raw, e := json.Marshal(p)
		if e != nil {
			return nil, e
		}
		return cursor.CanonicalJSON(raw)
	case c.LifecycleChangedEventName:
		var p c.LifecycleChangedPayload
		if json.Unmarshal(raw, &p) != nil {
			return nil, unavailable(nil)
		}
		raw, e := json.Marshal(p)
		if e != nil {
			return nil, e
		}
		return cursor.CanonicalJSON(raw)
	default:
		return nil, fault(foundation.DependencyUnbound)
	}
}
func exactEvent(summary event.Summary, header, payload []byte) error {
	h, e := event.DecodeHeader(header)
	if e != nil || !reflect.DeepEqual(h, summary.Header) {
		return fault(foundation.Forbidden)
	}
	canonical, e := canonicalPayload(h.EventType, payload)
	if e != nil {
		return e
	}
	if digest(canonical) != summary.PayloadDigest {
		return fault(foundation.Forbidden)
	}
	return nil
}

type eventFact struct {
	creation  *creationRecord
	command   *commandRecord
	lifecycle *lifecycleCommandRecord
	project   c.ProjectID
	identity  foundation.CommandIdentity
	opaque    string
	locks     []foundation.LockRequest
}

func (a *Authority) eventFact(ctx context.Context, x postgres.SQLExecutor, actor identity.Actor, summary event.Summary) (eventFact, error) {
	empty := eventFact{}
	if actor.Validate() != nil || summary.Producer != c.ProjectProducer || summary.Header.Validate() != nil || summary.PayloadDigest.Validate() != nil {
		return empty, fault(foundation.Forbidden)
	}
	h := summary.Header
	if h.Scope.Kind != event.ProjectScope || h.Scope.ProjectID.String() != h.AggregateID.String() || h.AggregateType != c.ProjectAggregate || h.AggregateVersion == nil || h.AggregateSequence != nil || h.SchemaVersion != 1 {
		return empty, fault(foundation.Forbidden)
	}
	project, e := parseID[identity.Project](h.Scope.ProjectID.String())
	if e != nil {
		return empty, e
	}
	fact := eventFact{project: project}
	switch h.EventType {
	case c.CreatedEventName:
		var raw string
		e = x.QueryRow(ctx, `SELECT id::text FROM agenteam_project.creations WHERE project_id=$1 AND event_id=$2`, project.String(), h.EventID.String()).Scan(&raw)
		if errors.Is(e, pgx.ErrNoRows) {
			return empty, fault(foundation.Forbidden)
		}
		if e != nil {
			return empty, unavailable(e)
		}
		id, e := parseID[c.Creation](raw)
		if e != nil {
			return empty, e
		}
		r, e := loadCreation(ctx, x, id)
		if e != nil {
			return empty, e
		}
		if r == nil {
			return empty, fault(foundation.Forbidden)
		}
		d := actor.Details()
		if d.Kind != identity.Service || d.ServiceName != identity.ProjectInitialization || d.ProjectID != project.String() || d.CauseRef != id.String() || *h.AggregateVersion != 1 {
			return empty, fault(foundation.Forbidden)
		}
		if e = exactEvent(summary, r.eventHeader, r.eventPayload); e != nil {
			return empty, e
		}
		var payload c.CreatedPayload
		if json.Unmarshal(r.eventPayload, &payload) != nil || payload.OwnerUserID != r.owner || payload.CreationID != id {
			return empty, fault(foundation.Forbidden)
		}
		fact.creation = r
		fact.identity = r.identity()
		fact.opaque = id.String()
	case c.UpdatedEventName:
		var key foundation.IdempotencyKey
		e = x.QueryRow(ctx, `SELECT key FROM agenteam_project.commands WHERE project_id=$1 AND command_name='update' AND event_id=$2`, project.String(), h.EventID.String()).Scan(&key)
		if errors.Is(e, pgx.ErrNoRows) {
			return empty, fault(foundation.Forbidden)
		}
		if e != nil {
			return empty, unavailable(e)
		}
		r, e := loadCommand(ctx, x, project, c.UpdateCommand, key)
		if e != nil {
			return empty, e
		}
		if r == nil || r.plan == nil || actor.Details().Kind != identity.Human || actor.Details().UserID != r.user || *h.AggregateVersion != r.plan.Project.Version || r.eventID == nil || *r.eventID != h.EventID.String() {
			return empty, fault(foundation.Forbidden)
		}
		if e = exactEvent(summary, r.plan.Header, r.plan.Payload); e != nil {
			return empty, e
		}
		var payload c.UpdatedPayload
		if json.Unmarshal(r.plan.Payload, &payload) != nil || !reflect.DeepEqual(payload.ChangedFields, r.plan.Changed) {
			return empty, fault(foundation.Forbidden)
		}
		fact.command = r
		fact.identity = r.identity()
		fact.opaque = r.id
		fact.locks = append(fact.locks, userLock(actor.Details().UserID, foundation.Exclusive))
		if r.plan.Scheduler != nil {
			fact.locks = append(fact.locks, schedulerLock(project))
		}
	case c.LifecycleChangedEventName:
		fact, e = a.lifecycleEventFact(ctx, x, actor, summary, project)
		if e != nil {
			return empty, e
		}
	default:
		return empty, fault(foundation.DependencyUnbound)
	}
	fact.locks = append(fact.locks, commandLock(fact.identity), projectLock(project, foundation.Exclusive))
	return fact, nil
}
func (a *Authority) DiscoverAppend(ctx context.Context, actor identity.Actor, summary event.Summary) (oc.Dependencies, error) {
	if a.state() == nil {
		return oc.Dependencies{}, fault(foundation.DependencyUnbound)
	}
	fact, e := a.eventFact(ctx, a.state().store, actor, summary)
	if e != nil {
		return oc.Dependencies{}, e
	}
	binding, e := appendBinding(actor, summary)
	if e != nil {
		return oc.Dependencies{}, e
	}
	return oc.NewDependencies(a.state().eventIssuer, binding, fact.locks, []byte(fact.opaque))
}
func (a *Authority) validateEventFact(ctx context.Context, tx foundation.Tx, actor identity.Actor, summary event.Summary, stage oc.Stage, opaque string) error {
	if !stage.Valid() {
		return invalid()
	}
	x, e := a.state().store.InTx(tx)
	if e != nil {
		return unavailable(e)
	}
	fact, e := a.eventFact(ctx, x, actor, summary)
	if e != nil {
		return e
	}
	if fact.opaque != opaque {
		return fault(foundation.ResourceBusy)
	}
	if e = a.state().store.RequireHeldLocks(ctx, tx, fact.locks); e != nil {
		return unavailable(e)
	}
	p, e := loadProject(ctx, x, fact.project)
	if e != nil {
		return e
	}
	if p == nil {
		return fault(foundation.NotFound)
	}
	if fact.creation != nil {
		r := fact.creation
		if e = a.ValidateInitializationInTx(ctx, tx, actor, r.operation.ID, fact.project, r.initializationKey); e != nil {
			return e
		}
		if stage == oc.NewFact && (r.operation.State != c.CreationCompleted || !p.initialized || p.ref.Version != 1) {
			return fault(foundation.InvalidState)
		}
	} else if fact.lifecycle != nil {
		return a.validateLifecycleEvent(ctx, tx, x, actor, p, fact.lifecycle, stage)
	} else {
		r := fact.command
		if _, e = a.RequireOwnerInTx(ctx, tx, actor, fact.project, identity.Mutate); e != nil {
			return e
		}
		if stage == oc.NewFact && (r.state != "planned" || !reflect.DeepEqual(p.ref, r.plan.Project)) {
			return fault(foundation.InvalidState)
		}
		if stage == oc.NewFact {
			if e = requireSchedulerPostimage(ctx, x, fact.project, r.plan.Scheduler); e != nil {
				return e
			}
		}
	}
	return nil
}
func (a *Authority) ValidateAppendInTx(ctx context.Context, tx foundation.Tx, actor identity.Actor, summary event.Summary, deps oc.Dependencies, stage oc.Stage) error {
	if a.state() == nil {
		return fault(foundation.DependencyUnbound)
	}
	binding, e := appendBinding(actor, summary)
	if e != nil || !deps.Matches(a.state().eventIssuer, binding) {
		return fault(foundation.Forbidden)
	}
	if e = a.state().store.RequireHeldLocks(ctx, tx, deps.Locks()); e != nil {
		return unavailable(e)
	}
	return a.validateEventFact(ctx, tx, actor, summary, stage, string(deps.Opaque()))
}

// Discover/ValidateInTx bind typed Project appends and explicitly configured
// lifecycle facts, plus the Model, Work, ProjectVariable and Knowledge gates.
// Other delivery remains unbound; each producer proves its own facts.
func (a *Authority) Discover(ctx context.Context, request oc.ProjectRequest) (oc.Dependencies, error) {
	if a.state() == nil {
		return oc.Dependencies{}, fault(foundation.DependencyUnbound)
	}
	if request.Validate() != nil {
		return oc.Dependencies{}, invalid()
	}
	d := request.Details()
	if d.Kind == oc.LifecycleProject {
		facts, err := a.lifecycleAuthority()
		if err != nil {
			return oc.Dependencies{}, err
		}
		return facts.Discover(ctx, request)
	}
	if d.Kind == oc.AppendProject && d.Event.Producer == "model" {
		return a.discoverModelEvent(request)
	}
	if d.Kind == oc.AppendProject && d.Event.Producer == "work" {
		return a.discoverWorkEvent(request)
	}
	if d.Kind == oc.AppendProject && d.Event.Producer == "projectvariable" {
		return a.discoverProjectVariableEvent(request)
	}
	if d.Kind == oc.AppendProject && d.Event.Producer == "knowledge" {
		return a.discoverKnowledgeEvent(request)
	}
	if d.Kind == oc.AppendProject && d.Event.Producer == "agent" {
		return a.discoverAgentEvent(ctx, request)
	}
	if d.Kind == oc.AppendProject && d.Event.Producer == "execution" {
		return a.discoverExecutionEvent(request)
	}
	if d.Kind != oc.AppendProject || d.Event.Producer != c.ProjectProducer {
		return oc.Dependencies{}, fault(foundation.DependencyUnbound)
	}
	fact, e := a.eventFact(ctx, a.state().store, d.Actor, d.Event)
	if e != nil {
		return oc.Dependencies{}, e
	}
	binding, e := appendBinding(d.Actor, d.Event)
	if e != nil {
		return oc.Dependencies{}, e
	}
	return oc.NewDependencies(a.state().projectIssuer, binding, fact.locks, []byte(fact.opaque))
}
func (a *Authority) ValidateInTx(ctx context.Context, tx foundation.Tx, request oc.ProjectRequest, deps oc.Dependencies) error {
	if a.state() == nil {
		return fault(foundation.DependencyUnbound)
	}
	if request.Validate() != nil {
		return invalid()
	}
	d := request.Details()
	if d.Kind == oc.LifecycleProject {
		facts, err := a.lifecycleAuthority()
		if err != nil {
			return err
		}
		return facts.ValidateInTx(ctx, tx, request, deps)
	}
	if d.Kind == oc.AppendProject && d.Event.Producer == "model" {
		return a.validateModelEventInTx(ctx, tx, request, deps)
	}
	if d.Kind == oc.AppendProject && d.Event.Producer == "work" {
		return a.validateWorkEventInTx(ctx, tx, request, deps)
	}
	if d.Kind == oc.AppendProject && d.Event.Producer == "projectvariable" {
		return a.validateProjectVariableEventInTx(ctx, tx, request, deps)
	}
	if d.Kind == oc.AppendProject && d.Event.Producer == "knowledge" {
		return a.validateKnowledgeEventInTx(ctx, tx, request, deps)
	}
	if d.Kind == oc.AppendProject && d.Event.Producer == "agent" {
		return a.validateAgentEventInTx(ctx, tx, request, deps)
	}
	if d.Kind == oc.AppendProject && d.Event.Producer == "execution" {
		return a.validateExecutionEventInTx(ctx, tx, request, deps)
	}
	if d.Kind != oc.AppendProject || d.Event.Producer != c.ProjectProducer {
		return fault(foundation.DependencyUnbound)
	}
	binding, e := appendBinding(d.Actor, d.Event)
	if e != nil || !deps.Matches(a.state().projectIssuer, binding) {
		return fault(foundation.Forbidden)
	}
	if e = a.state().store.RequireHeldLocks(ctx, tx, deps.Locks()); e != nil {
		return unavailable(e)
	}
	return a.validateEventFact(ctx, tx, d.Actor, d.Event, d.Stage, string(deps.Opaque()))
}

var _ oc.ProducerAuthority = (*Authority)(nil)
var _ oc.ProjectAuthority = (*Authority)(nil)
