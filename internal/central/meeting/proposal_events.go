package meeting

import (
	"context"
	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/meeting/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	"reflect"
	"time"
)

// EventAuthority is composed before Outbox; it has no mutable service binding.
// Only this package's live, same-transaction lifecycle plan can use it.
type EventAuthority struct {
	store    Store
	projects pc.ProjectAuthority
	issuer   oc.PlanIssuer
}

func NewEventAuthority(store Store, projects pc.ProjectAuthority) (*EventAuthority, error) {
	if nilPort(store) || nilPort(projects) {
		return nil, fault(f.DependencyUnbound)
	}
	return &EventAuthority{store, projects, oc.NewPlanIssuer()}, nil
}

type ProposalEvents struct {
	authority *EventAuthority
	types     c.Events
	journal   oc.Appender
}

func NewProposalEvents(a *EventAuthority, types c.Events, journal oc.Appender) (*ProposalEvents, error) {
	if a == nil || !types.Valid() || nilPort(journal) {
		return nil, fault(f.DependencyUnbound)
	}
	return &ProposalEvents{a, types, journal}, nil
}

type lifecycleEventKey struct{}
type lifecycleEventPlan struct {
	owner       *ProposalEvents
	actor       i.Actor
	event       event.Event
	locks       []f.LockRequest
	append      oc.AppendPlan
	tx          f.Tx
	active      bool
	sourceCheck func(context.Context, f.Tx) error
}

func (e *ProposalEvents) prepare(ctx context.Context, a i.Actor, p i.ProjectID, m c.MeetingID, v f.Version, name event.StableName, status string, at time.Time, locks []f.LockRequest) (*lifecycleEventPlan, error) {
	if e == nil {
		return nil, fault(f.DependencyUnbound)
	}
	id, err := f.NewID[event.EventIdentity]()
	if err != nil {
		return nil, portError(err)
	}
	ep, _ := f.ParseID[event.Project](p.String())
	ag, _ := f.ParseID[event.Aggregate](m.String())
	instant, err := f.NewInstant(at)
	if err != nil {
		return nil, err
	}
	ev, err := e.types.New(event.Header{EventID: id, EventType: name, SchemaVersion: 1, OccurredAt: instant, Scope: event.Scope{Kind: event.ProjectScope, ProjectID: ep}, AggregateType: c.MeetingAggregate, AggregateID: ag, AggregateVersion: &v}, c.LifecyclePayload{MeetingID: m, Status: status})
	if err != nil {
		return nil, err
	}
	plan := &lifecycleEventPlan{owner: e, actor: a, event: ev, locks: locks}
	plan.append, err = e.journal.PrepareAppend(context.WithValue(ctx, lifecycleEventKey{}, plan), a, ev)
	if err != nil {
		return nil, portError(err)
	}
	plan.locks, err = normalize(append(locks, plan.append.Locks()...))
	return plan, err
}
func (e *ProposalEvents) appendInTx(ctx context.Context, tx f.Tx, p *lifecycleEventPlan) error {
	if e == nil || p == nil || p.owner != e || p.active {
		return fault(f.Forbidden)
	}
	x, err := e.authority.store.InTx(tx)
	if err != nil {
		return portError(err)
	}
	payload, err := e.types.Decode(p.event)
	if err != nil {
		return err
	}
	h := p.event.Header()
	if _, err = x.Exec(ctx, `INSERT INTO agenteam_meeting.lifecycle_events(event_id,project_id,meeting_id,event_type,version,status,occurred_at) VALUES($1,$2,$3,$4,$5,$6,$7)`, h.EventID.String(), h.Scope.ProjectID.String(), payload.MeetingID.String(), string(h.EventType), int64(*h.AggregateVersion), payload.Status, h.OccurredAt.Time()); err != nil {
		return portError(err)
	}
	p.tx, p.active = tx, true
	defer func() { p.active = false }()
	receipt, err := e.journal.AppendEventInTx(context.WithValue(ctx, lifecycleEventKey{}, p), tx, p.actor, p.event, p.append)
	if err != nil {
		return portError(err)
	}
	if receipt.EventID != h.EventID || receipt.Sequence.Validate() != nil {
		return fault(f.InvalidState)
	}
	return nil
}
func (a *EventAuthority) plan(ctx context.Context, actor i.Actor, summary event.Summary) (*lifecycleEventPlan, error) {
	p, _ := ctx.Value(lifecycleEventKey{}).(*lifecycleEventPlan)
	if p == nil || p.owner == nil || p.owner.authority != a || !p.actor.Equal(actor) || !reflect.DeepEqual(p.event.Summary(), summary) {
		return nil, fault(f.Forbidden)
	}
	return p, nil
}
func lifecycleBinding(p *lifecycleEventPlan) f.Digest {
	raw, _ := canonical(struct {
		Summary event.Summary
		Actor   i.ActorDetails
	}{p.event.Summary(), p.actor.Details()})
	return digest(raw)
}
func (a *EventAuthority) DiscoverAppend(ctx context.Context, actor i.Actor, summary event.Summary) (oc.Dependencies, error) {
	if ctx == nil {
		return oc.Dependencies{}, fault(f.InvalidArgument)
	}
	if _, ok := ctx.Value(decisionEventKey{}).(*decisionEventPlan); ok {
		return a.discoverDecisionAppend(ctx, actor, summary)
	}
	p, err := a.plan(ctx, actor, summary)
	if err != nil {
		return oc.Dependencies{}, err
	}
	return oc.NewDependencies(a.issuer, lifecycleBinding(p), p.locks, nil)
}
func (a *EventAuthority) ValidateAppendInTx(ctx context.Context, tx f.Tx, actor i.Actor, summary event.Summary, deps oc.Dependencies, stage oc.Stage) error {
	if ctx == nil || !stage.Valid() {
		return fault(f.InvalidArgument)
	}
	if _, ok := ctx.Value(decisionEventKey{}).(*decisionEventPlan); ok {
		return a.validateDecisionAppendInTx(ctx, tx, actor, summary, deps, stage)
	}
	p, err := a.plan(ctx, actor, summary)
	if err != nil {
		return err
	}
	if !p.active || p.tx != tx || !deps.Matches(a.issuer, lifecycleBinding(p)) {
		return fault(f.Forbidden)
	}
	if err = a.store.RequireHeldLocks(ctx, tx, p.locks); err != nil {
		return portError(err)
	}
	x, err := a.store.InTx(tx)
	if err != nil {
		return portError(err)
	}
	payload, err := p.owner.types.Decode(p.event)
	if err != nil {
		return err
	}
	h := p.event.Header()
	var exact bool
	err = x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_meeting.lifecycle_events WHERE event_id=$1 AND project_id=$2 AND meeting_id=$3 AND event_type=$4 AND version=$5 AND status=$6 AND occurred_at=$7)`, h.EventID.String(), h.Scope.ProjectID.String(), payload.MeetingID.String(), string(h.EventType), int64(*h.AggregateVersion), payload.Status, h.OccurredAt.Time()).Scan(&exact)
	if err != nil {
		return portError(err)
	}
	if !exact {
		return fault(f.Forbidden)
	}
	project, _ := f.ParseID[i.Project](h.Scope.ProjectID.String())
	if payload.Status != "deleted" {
		session, err := loadSession(ctx, x, project, payload.MeetingID)
		if err != nil {
			return err
		}
		if session.Status != payload.Status || session.Version != *h.AggregateVersion {
			return fault(f.Forbidden)
		}
	} else {
		var exists bool
		if err = x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_meeting.sessions WHERE id=$1)`, payload.MeetingID.String()).Scan(&exists); err != nil {
			return portError(err)
		}
		if exists {
			return fault(f.Forbidden)
		}
	}
	if actor.Details().Kind == i.Human {
		grant, err := a.projects.RequireOwnerInTx(ctx, tx, actor, project, i.Mutate)
		if err != nil {
			return portError(err)
		}
		if !grant.Matches(actor, project) {
			return fault(f.Forbidden)
		}
	} else if actor.Details().Kind == i.Service && actor.Details().ServiceName == i.MeetingRuntime && h.EventType == c.DeletedName && p.sourceCheck != nil {
		if err = p.sourceCheck(ctx, tx); err != nil {
			return err
		}
	} else if actor.Details().Kind == i.AgentRun && h.EventType == c.ProposedName && p.sourceCheck != nil {
		if err = p.sourceCheck(ctx, tx); err != nil {
			return err
		}
	} else {
		return fault(f.Forbidden)
	}
	return ctx.Err()
}

var _ oc.ProducerAuthority = (*EventAuthority)(nil)
