package meeting

import (
	"context"
	"math"
	"reflect"
	"time"

	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/meeting/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
)

// DecisionEvents borrows the original Meeting producer and Outbox. It does not
// own Decision execution, waiting, retries or an independent delivery loop.
type DecisionEvents struct {
	authority *EventAuthority
	types     c.DecisionEvents
	journal   oc.Appender
}

func NewDecisionEvents(a *EventAuthority, types c.DecisionEvents, journal oc.Appender) (*DecisionEvents, error) {
	if a == nil || nilPort(a.store) || !types.Valid() || nilPort(journal) {
		return nil, fault(f.DependencyUnbound)
	}
	return &DecisionEvents{a, types, journal}, nil
}

type decisionEventKey struct{}
type decisionEventPlan struct {
	owner       *DecisionEvents
	actor       i.Actor
	event       event.Event
	locks       []f.LockRequest
	append      oc.AppendPlan
	tx          f.Tx
	active      bool
	sourceCheck func(context.Context, f.Tx) error
}

func (e *DecisionEvents) prepare(ctx context.Context, actor i.Actor, project i.ProjectID, payload c.DecisionPayload, version f.Version, at time.Time, locks []f.LockRequest) (*decisionEventPlan, error) {
	if e == nil || e.authority == nil || ctx == nil {
		return nil, fault(f.DependencyUnbound)
	}
	name := map[string]event.StableName{"pending": c.DecisionRequestedName, "answered": c.DecisionAnsweredName, "skipped": c.DecisionSkippedName, "cancelled": c.DecisionCancelledName}[payload.Status]
	if actor.Validate() != nil || project.Validate() != nil || payload.Validate() != nil || version.Validate() != nil || name == "" {
		return nil, fault(f.InvalidArgument)
	}
	id, err := f.NewID[event.EventIdentity]()
	if err != nil {
		return nil, portError(err)
	}
	ep, _ := f.ParseID[event.Project](project.String())
	ag, _ := f.ParseID[event.Aggregate](payload.DecisionID.String())
	instant, err := f.NewInstant(at)
	if err != nil {
		return nil, err
	}
	ev, err := e.types.New(event.Header{EventID: id, EventType: name, SchemaVersion: 1, OccurredAt: instant, Scope: event.Scope{Kind: event.ProjectScope, ProjectID: ep}, AggregateType: c.DecisionAggregate, AggregateID: ag, AggregateVersion: &version}, payload)
	if err != nil {
		return nil, err
	}
	locks, err = normalize(append(append([]f.LockRequest{}, locks...), projectLock(project, f.Shared), meetingLock(payload.MeetingID, f.Exclusive), decisionLock(payload.DecisionID)))
	if err != nil {
		return nil, err
	}
	plan := &decisionEventPlan{owner: e, actor: actor, event: ev, locks: locks}
	plan.append, err = e.journal.PrepareAppend(context.WithValue(ctx, decisionEventKey{}, plan), actor, ev)
	if err != nil {
		return nil, portError(err)
	}
	plan.locks, err = normalize(append(plan.locks, plan.append.Locks()...))
	return plan, err
}

// appendInTx is only reached by the original authorized mutation stack after
// its canonical state write. The private callback must revalidate that same
// owner/command; neither an Actor nor an event payload grants this permission.
func (e *DecisionEvents) appendInTx(ctx context.Context, tx f.Tx, plan *decisionEventPlan, sourceCheck func(context.Context, f.Tx) error) error {
	if e == nil || plan == nil || plan.owner != e || plan.active || sourceCheck == nil {
		return fault(f.Forbidden)
	}
	if err := e.authority.store.RequireHeldLocks(ctx, tx, plan.locks); err != nil {
		return portError(err)
	}
	x, err := e.authority.store.InTx(tx)
	if err != nil {
		return portError(err)
	}
	if err = sourceCheck(ctx, tx); err != nil {
		return err
	}
	payload, err := e.types.Decode(plan.event)
	if err != nil {
		return err
	}
	if err = requireDecisionEventPostimage(ctx, x, plan.event, payload); err != nil {
		return err
	}
	h := plan.event.Header()
	_, err = x.Exec(ctx, `INSERT INTO agenteam_meeting.decision_events(event_id,project_id,meeting_id,turn_id,contribution_id,participant_id,execution_id,decision_id,version,event_type,status,occurred_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`, h.EventID.String(), h.Scope.ProjectID.String(), payload.MeetingID.String(), payload.TurnID.String(), payload.ContributionID.String(), payload.ParticipantID.String(), payload.ExecutionID.String(), payload.DecisionID.String(), int64(*h.AggregateVersion), string(h.EventType), payload.Status, h.OccurredAt.Time())
	if err != nil {
		return portError(err)
	}
	plan.tx, plan.active, plan.sourceCheck = tx, true, sourceCheck
	defer func() { plan.active, plan.sourceCheck = false, nil }()
	receipt, err := e.journal.AppendEventInTx(context.WithValue(ctx, decisionEventKey{}, plan), tx, plan.actor, plan.event, plan.append)
	if err != nil {
		return portError(err)
	}
	if receipt.EventID != h.EventID || receipt.Sequence.Validate() != nil {
		return fault(f.InvalidState)
	}
	return nil
}

func requireDecisionEventPostimage(ctx context.Context, x postgres.SQLExecutor, ev event.Event, payload c.DecisionPayload) error {
	h := ev.Header()
	var exact bool
	err := x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_meeting.decision_requests d JOIN agenteam_meeting.sessions s ON s.id=d.meeting_id JOIN agenteam_meeting.contributions c ON c.id=d.contribution_id AND c.meeting_id=s.id JOIN agenteam_meeting.launch_intents l ON l.execution_id=d.execution_id AND l.contribution_id=c.id AND l.meeting_id=s.id WHERE d.id=$1 AND s.project_id=$2 AND s.id=$3 AND c.turn_id=$4 AND c.id=$5 AND c.participant_id=$6 AND d.execution_id=$7 AND d.version=$8 AND d.status=$9 AND COALESCE(d.resolved_at,d.created_at)=$10)`, payload.DecisionID.String(), h.Scope.ProjectID.String(), payload.MeetingID.String(), payload.TurnID.String(), payload.ContributionID.String(), payload.ParticipantID.String(), payload.ExecutionID.String(), int64(*h.AggregateVersion), payload.Status, h.OccurredAt.Time()).Scan(&exact)
	if err != nil {
		return portError(err)
	}
	if !exact {
		return fault(f.ConfirmationStale)
	}
	return nil
}

func (a *EventAuthority) decisionPlan(ctx context.Context, actor i.Actor, summary event.Summary) (*decisionEventPlan, error) {
	if ctx == nil || a == nil {
		return nil, fault(f.Forbidden)
	}
	p, _ := ctx.Value(decisionEventKey{}).(*decisionEventPlan)
	if p == nil || p.owner == nil || p.owner.authority != a || !p.actor.Equal(actor) || !reflect.DeepEqual(p.event.Summary(), summary) {
		return nil, fault(f.Forbidden)
	}
	return p, nil
}

func decisionEventBinding(p *decisionEventPlan) f.Digest {
	raw, _ := canonical(struct {
		Summary event.Summary
		Actor   i.ActorDetails
	}{p.event.Summary(), p.actor.Details()})
	return digest(raw)
}

// The original EventAuthority dispatches to these helpers only when this
// package's private Decision plan is present; Proposal plans remain separate.
func (a *EventAuthority) discoverDecisionAppend(ctx context.Context, actor i.Actor, summary event.Summary) (oc.Dependencies, error) {
	p, err := a.decisionPlan(ctx, actor, summary)
	if err != nil {
		return oc.Dependencies{}, err
	}
	return oc.NewDependencies(a.issuer, decisionEventBinding(p), p.locks, nil)
}

func (a *EventAuthority) validateDecisionAppendInTx(ctx context.Context, tx f.Tx, actor i.Actor, summary event.Summary, deps oc.Dependencies, stage oc.Stage) error {
	p, err := a.decisionPlan(ctx, actor, summary)
	if err != nil {
		return err
	}
	if !stage.Valid() || !p.active || p.tx != tx || p.sourceCheck == nil || !deps.Matches(a.issuer, decisionEventBinding(p)) {
		return fault(f.Forbidden)
	}
	if err = a.store.RequireHeldLocks(ctx, tx, p.locks); err != nil {
		return portError(err)
	}
	x, err := a.store.InTx(tx)
	if err != nil {
		return portError(err)
	}
	if err = p.sourceCheck(ctx, tx); err != nil {
		return err
	}
	payload, err := p.owner.types.Decode(p.event)
	if err != nil {
		return err
	}
	if err = requireDecisionEventPostimage(ctx, x, p.event, payload); err != nil {
		return err
	}
	h := p.event.Header()
	var exact bool
	err = x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_meeting.decision_events WHERE event_id=$1 AND project_id=$2 AND meeting_id=$3 AND turn_id=$4 AND contribution_id=$5 AND participant_id=$6 AND execution_id=$7 AND decision_id=$8 AND version=$9 AND event_type=$10 AND status=$11 AND occurred_at=$12)`, h.EventID.String(), h.Scope.ProjectID.String(), payload.MeetingID.String(), payload.TurnID.String(), payload.ContributionID.String(), payload.ParticipantID.String(), payload.ExecutionID.String(), payload.DecisionID.String(), int64(*h.AggregateVersion), string(h.EventType), payload.Status, h.OccurredAt.Time()).Scan(&exact)
	if err != nil {
		return portError(err)
	}
	if !exact {
		return fault(f.Forbidden)
	}
	return ctx.Err()
}

type decisionEventSource struct {
	payload c.DecisionPayload
	version f.Version
}

// The filter is private and fixed by the original action, never SQL supplied
// by a caller. Empty typed identities mean the whole original Meeting.
type decisionEventScope struct {
	project      i.ProjectID
	meeting      c.MeetingID
	turn         c.TurnID
	contribution c.ContributionID
	execution    i.ExecutionID
	decision     c.DecisionID
}

func pendingDecisionEvents(ctx context.Context, x postgres.SQLExecutor, scope decisionEventScope) ([]decisionEventSource, error) {
	rows, err := x.Query(ctx, `SELECT d.id,c.turn_id,d.contribution_id,c.participant_id,d.execution_id,d.version FROM agenteam_meeting.decision_requests d JOIN agenteam_meeting.sessions s ON s.id=d.meeting_id JOIN agenteam_meeting.contributions c ON c.id=d.contribution_id AND c.meeting_id=s.id WHERE s.project_id=$1 AND s.id=$2 AND d.status='pending' AND ($3::text='' OR c.turn_id=$3) AND ($4::text='' OR c.id=$4) AND ($5::text='' OR d.execution_id=$5) AND ($6::text='' OR d.id=$6) ORDER BY d.id`, scope.project.String(), scope.meeting.String(), optionalDecisionEventID(scope.turn), optionalDecisionEventID(scope.contribution), optionalDecisionEventID(scope.execution), optionalDecisionEventID(scope.decision))
	if err != nil {
		return nil, portError(err)
	}
	defer rows.Close()
	out := []decisionEventSource{}
	for rows.Next() {
		var v decisionEventSource
		var d, t, contribution, participant, execution string
		if err = rows.Scan(&d, &t, &contribution, &participant, &execution, &v.version); err != nil {
			return nil, portError(err)
		}
		v.payload.DecisionID, _ = f.ParseID[c.Decision](d)
		v.payload.MeetingID = scope.meeting
		v.payload.TurnID, _ = f.ParseID[c.Turn](t)
		v.payload.ContributionID, _ = f.ParseID[c.Contribution](contribution)
		v.payload.ParticipantID, _ = f.ParseID[c.Participant](participant)
		v.payload.ExecutionID, _ = f.ParseID[i.Execution](execution)
		v.payload.Status = "pending"
		if v.payload.Validate() != nil || v.version.Validate() != nil || v.version == f.Version(math.MaxInt64) {
			return nil, fault(f.InvalidState)
		}
		out = append(out, v)
	}
	return out, portError(rows.Err())
}

// Only the private scope's zero value omits a filter. ID.String formats even
// that value as a UUID, whereas this query uses an empty string for omission.
// Required Project/Meeting identities never pass through this conversion.
func optionalDecisionEventID[K any](value f.ID[K]) string {
	if value == (f.ID[K]{}) {
		return ""
	}
	return value.String()
}

type decisionCancellation struct {
	scope  decisionEventScope
	before []decisionEventSource
	plans  []*decisionEventPlan
	locks  []f.LockRequest
	at     time.Time
}

func prepareDecisionCancellation(ctx context.Context, store Store, events *DecisionEvents, actor i.Actor, scope decisionEventScope, locks []f.LockRequest) (*decisionCancellation, error) {
	before, err := pendingDecisionEvents(ctx, store, scope)
	if err != nil {
		return nil, err
	}
	v := &decisionCancellation{scope: scope, before: before, locks: locks, at: now()}
	for _, current := range before {
		if events == nil || events.authority == nil || !sameStore(store, events.authority.store) {
			return nil, fault(f.DependencyUnbound)
		}
		payload := current.payload
		payload.Status = "cancelled"
		plan, err := events.prepare(ctx, actor, scope.project, payload, current.version+1, v.at, locks)
		if err != nil {
			return nil, err
		}
		v.plans = append(v.plans, plan)
		v.locks = append(v.locks, plan.locks...)
	}
	v.locks, err = normalize(v.locks)
	return v, err
}

// Re-read the complete pre-discovered set under Meeting EX before changing
// anything. A concurrent new Decision requires fresh discovery, never adding
// a missing lock after acquisition or cancelling without its canonical event.
func (v *decisionCancellation) applyInTx(ctx context.Context, tx f.Tx, store Store, check func(context.Context, f.Tx) error) error {
	if v == nil || check == nil {
		return fault(f.Forbidden)
	}
	if err := store.RequireHeldLocks(ctx, tx, v.locks); err != nil {
		return portError(err)
	}
	x, err := store.InTx(tx)
	if err != nil {
		return portError(err)
	}
	current, err := pendingDecisionEvents(ctx, x, v.scope)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(v.before, current) {
		return fault(f.ConfirmationStale)
	}
	for idx, original := range current {
		tag, err := x.Exec(ctx, `UPDATE agenteam_meeting.decision_requests SET status='cancelled',version=version+1,resolved_at=$3 WHERE id=$1 AND version=$2 AND status='pending'`, original.payload.DecisionID.String(), int64(original.version), v.at)
		if err != nil {
			return portError(err)
		}
		if tag.RowsAffected() != 1 {
			return fault(f.ConfirmationStale)
		}
		plan := v.plans[idx]
		if err = plan.owner.appendInTx(ctx, tx, plan, check); err != nil {
			return err
		}
	}
	return nil
}

func decisionPayloadFromIntent(id c.DecisionID, intent intentRecord, status string) (c.DecisionPayload, error) {
	var out c.DecisionPayload
	if intent.execution == nil {
		return out, fault(f.InvalidState)
	}
	out.DecisionID, out.ExecutionID, out.Status = id, *intent.execution, status
	out.MeetingID, _ = f.ParseID[c.Meeting](intent.request.Trigger.MeetingID)
	out.TurnID, _ = f.ParseID[c.Turn](intent.request.Trigger.TurnID)
	out.ContributionID, _ = f.ParseID[c.Contribution](intent.request.Trigger.ContributionID)
	out.ParticipantID, _ = f.ParseID[c.Participant](intent.request.Trigger.ParticipantID)
	if out.Validate() != nil {
		return c.DecisionPayload{}, fault(f.InvalidState)
	}
	return out, nil
}

func (s *DecisionService) requireEventCallInTx(ctx context.Context, tx f.Tx, v decisionCall) error {
	w, _ := ctx.Value(decisionWitnessKey{}).(*decisionWitness)
	if w == nil || !w.active || w.tx != tx || w.authority != s.authority || !sameDecisionCall(w.call, v.call) || w.decision != v.id || w.meeting.String() != v.intent.request.Trigger.MeetingID || (w.phase != "enter" && w.phase != "cancel") {
		return fault(f.Forbidden)
	}
	return s.authority.RequireMeetingDecisionBindingInTx(ctx, tx, s.store)
}
