package approval

import (
	"context"
	"encoding/json"
	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	c "github.com/LunaDeerTech/agenteam/internal/central/governance/approval/contract"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"reflect"
)

type eventPlan struct {
	owner   *Service
	actor   i.Actor
	record  *record
	event   event.Event
	locks   []f.LockRequest
	append  oc.AppendPlan
	applied f.Tx
	active  bool
}
type eventKey struct{}

func (s *Service) prepareEvent(ctx context.Context, actor i.Actor, r *record, locks []f.LockRequest) (*eventPlan, error) {
	id, err := f.NewID[event.EventIdentity]()
	if err != nil {
		return nil, portError(err)
	}
	name := c.RequestedName
	at := r.value.CreatedAt
	if r.value.Status != c.Pending {
		name = c.ResolvedName
		at = *r.value.ResolvedAt
		r.resolvedEvent = id.String()
	} else {
		r.requestedEvent = id.String()
	}
	project, _ := f.ParseID[event.Project](r.value.ProjectID.String())
	aggregate, _ := f.ParseID[event.Aggregate](r.value.ID.String())
	version := r.value.Version
	ev, err := s.deps.EventTypes.New(event.Header{EventID: id, EventType: name, SchemaVersion: 1, OccurredAt: at, Scope: event.Scope{Kind: event.ProjectScope, ProjectID: project}, AggregateType: c.Aggregate, AggregateID: aggregate, AggregateVersion: &version}, c.EventPayload{RequestID: r.value.ID, ExecutionID: r.value.ExecutionID, OperationID: r.value.OperationID, Status: r.value.Status})
	if err != nil {
		return nil, portError(err)
	}
	plan := &eventPlan{owner: s, actor: actor, record: r, event: ev, locks: locks}
	proof := context.WithValue(ctx, eventKey{}, plan)
	plan.append, err = s.deps.Events.PrepareAppend(proof, actor, ev)
	if err != nil {
		return nil, portError(err)
	}
	plan.locks, err = oc.NormalizeLocks(append(locks, plan.append.Locks()...))
	return plan, err
}
func (s *Service) appendEvent(ctx context.Context, tx f.Tx, p *eventPlan) error {
	p.applied, p.active = tx, true
	defer func() { p.active = false }()
	proof := context.WithValue(ctx, eventKey{}, p)
	receipt, err := s.deps.Events.AppendEventInTx(proof, tx, p.actor, p.event, p.append)
	if err != nil {
		return portError(err)
	}
	if receipt.EventID != p.event.Header().EventID || receipt.Sequence.Validate() != nil {
		return fault(f.InvalidState)
	}
	return nil
}
func (a *Authority) eventPlan(ctx context.Context, actor i.Actor, summary event.Summary) (*eventPlan, error) {
	p, ok := ctx.Value(eventKey{}).(*eventPlan)
	if !ok || p == nil || p.owner == nil || p.owner.deps.Authority != a || p.owner.store != a.store || !p.actor.Equal(actor) || !reflect.DeepEqual(p.event.Summary(), summary) {
		return nil, fault(f.Forbidden)
	}
	return p, nil
}
func eventBinding(p *eventPlan) f.Digest {
	raw, _ := json.Marshal(struct {
		Summary event.Summary
		User    string
		Request c.Request
	}{p.event.Summary(), p.actor.Details().UserID, p.record.value})
	return oc.DigestBytes(raw)
}
func (a *Authority) DiscoverAppend(ctx context.Context, actor i.Actor, summary event.Summary) (oc.Dependencies, error) {
	if ctx == nil {
		return oc.Dependencies{}, fault(f.InvalidArgument)
	}
	p, err := a.eventPlan(ctx, actor, summary)
	if err != nil {
		return oc.Dependencies{}, err
	}
	return oc.NewDependencies(a.issuer, eventBinding(p), p.locks, nil)
}
func (a *Authority) ValidateAppendInTx(ctx context.Context, tx f.Tx, actor i.Actor, summary event.Summary, deps oc.Dependencies, stage oc.Stage) error {
	if ctx == nil || !stage.Valid() {
		return fault(f.InvalidArgument)
	}
	p, err := a.eventPlan(ctx, actor, summary)
	if err != nil {
		return err
	}
	if !p.active || p.applied != tx || !deps.Matches(a.issuer, eventBinding(p)) {
		return fault(f.Forbidden)
	}
	if err = a.store.RequireHeldLocks(ctx, tx, p.locks); err != nil {
		return portError(err)
	}
	x, err := a.store.InTx(tx)
	if err != nil {
		return portError(err)
	}
	current, err := loadByID(ctx, x, p.record.value.ID)
	if err != nil {
		return err
	}
	if current == nil || !reflect.DeepEqual(current.value, p.record.value) || current.requestedEvent != p.record.requestedEvent || current.resolvedEvent != p.record.resolvedEvent {
		return fault(f.Forbidden)
	}
	if actor.Details().Kind == i.Human {
		if err = p.owner.owner(ctx, tx, actor, p.record.value.ProjectID, i.Mutate); err != nil {
			return err
		}
	}
	return ctx.Err()
}

var _ oc.ProducerAuthority = (*Authority)(nil)
