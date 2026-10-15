package humaninbox

import (
	"context"
	"encoding/json"
	"reflect"

	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	c "github.com/LunaDeerTech/agenteam/internal/central/humaninbox/contract"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/meeting/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
)

const decisionHandler event.StableName = "humaninbox.meeting_decision"

// NewServiceWithDecisions fixes both source providers at construction. Their
// distinct private read witnesses cannot be exchanged to gain source access.
func NewServiceWithDecisions(a *Authority, proposals c.SourceProvider, lifecycle mc.Events, decisions c.DecisionSourceProvider, events mc.DecisionEvents) (*Service, error) {
	s, err := NewService(a, proposals, lifecycle)
	if err != nil {
		return nil, err
	}
	if nilPort(decisions) || !events.Valid() {
		return nil, fault(f.DependencyUnbound)
	}
	s.decisions, s.decisionEvents = decisions, events
	return s, nil
}

type projectionKind uint8

const (
	proposalProjection projectionKind = iota + 1
	decisionProjection
)

func (k projectionKind) pair() (string, string) {
	switch k {
	case proposalProjection:
		return "meeting", "meeting.review_proposal"
	case decisionProjection:
		return "decision_request", "decision.answer"
	default:
		return "", ""
	}
}

func (k projectionKind) validContext(v c.SourceContext) bool {
	if k == proposalProjection {
		return v.MeetingID == ""
	}
	if k != decisionProjection {
		return false
	}
	_, err := f.ParseID[mc.Meeting](v.MeetingID)
	return err == nil
}

// Decode the closed pair and exact object shape together. A context is a
// navigation hint, never an arbitrary metadata/URL or an authorization token.
func decodeSourceContext(source, action string, raw []byte) (c.SourceContext, error) {
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil || fields == nil {
		return c.SourceContext{}, fault(f.InvalidState)
	}
	if source == "meeting" && action == "meeting.review_proposal" && len(fields) == 0 {
		return c.SourceContext{}, nil
	}
	var v c.SourceContext
	if source != "decision_request" || action != "decision.answer" || len(fields) != 1 || json.Unmarshal(fields["meeting_id"], &v.MeetingID) != nil || !decisionProjection.validContext(v) {
		return c.SourceContext{}, fault(f.InvalidState)
	}
	return v, nil
}

func inboxDecisionLock(id string) f.LockRequest {
	k, _ := f.AggregateLock(f.DecisionAggregate, id)
	return f.LockRequest{Key: k, Mode: f.Shared}
}

type decisionSourceReadKey struct{}
type decisionSourceReadWitness struct {
	owner        *Authority
	tx           f.Tx
	project      i.ProjectID
	meeting      string
	decision     string
	scan, active bool
}

func (a *Authority) withDecisionSource(ctx context.Context, tx f.Tx, p i.ProjectID, meeting, decision string, scan bool, fn func(context.Context) error) error {
	w := &decisionSourceReadWitness{a, tx, p, meeting, decision, scan, true}
	defer func() { w.active = false }()
	return fn(context.WithValue(ctx, decisionSourceReadKey{}, w))
}

func (a *Authority) RequireDecisionSourceReadInTx(ctx context.Context, tx f.Tx, store any, p i.ProjectID, meeting, decision string, scan bool) error {
	if a == nil || ctx == nil || store == nil || !reflect.TypeOf(store).Comparable() || a.store != store {
		return fault(f.DependencyUnbound)
	}
	w, _ := ctx.Value(decisionSourceReadKey{}).(*decisionSourceReadWitness)
	if w == nil || !w.active || w.owner != a || w.tx != tx || w.project != p || w.meeting != meeting || w.decision != decision || w.scan != scan {
		return fault(f.Forbidden)
	}
	if !tx.Valid() || p.Validate() != nil {
		return fault(f.Forbidden)
	}
	locks := []f.LockRequest{projectLock(p, f.Exclusive)}
	if scan {
		if meeting != "" || decision != "" {
			return fault(f.Forbidden)
		}
	} else {
		if _, err := f.ParseID[mc.Meeting](meeting); err != nil {
			return fault(f.Forbidden)
		}
		if _, err := f.ParseID[mc.Decision](decision); err != nil {
			return fault(f.Forbidden)
		}
		locks = []f.LockRequest{projectLock(p, f.Shared), meetingLock(meeting), inboxDecisionLock(decision)}
	}
	if err := a.store.RequireHeldLocks(ctx, tx, locks); err != nil {
		return portError(err)
	}
	_, err := a.store.InTx(tx)
	return portError(err)
}

type decisionProjector struct{ service *Service }

func (s *Service) DecisionHandler() (oc.HandlerDefinition, error) {
	if s == nil || s.authority == nil || nilPort(s.decisions) || !s.decisionEvents.Valid() {
		return oc.HandlerDefinition{}, fault(f.DependencyUnbound)
	}
	subscriptions := []oc.Subscription{}
	for _, name := range []event.StableName{mc.DecisionRequestedName, mc.DecisionAnsweredName, mc.DecisionSkippedName, mc.DecisionCancelledName} {
		subscriptions = append(subscriptions, oc.Subscription{EventType: name, Versions: []uint32{1}})
	}
	return oc.HandlerDefinition{ID: decisionHandler, Subscriptions: subscriptions, Effect: oc.CanonicalConverge, Ordering: oc.CanonicalReconcile, Handler: &decisionProjector{s}}, nil
}

func (h *decisionProjector) source(e event.Event) (i.ProjectID, mc.DecisionPayload, error) {
	payload, err := h.service.decisionEvents.Decode(e)
	if err != nil {
		return i.ProjectID{}, mc.DecisionPayload{}, err
	}
	p, err := f.ParseID[i.Project](e.Header().Scope.ProjectID.String())
	return p, payload, err
}

func (h *decisionProjector) Prepare(ctx context.Context, e event.Event) (oc.HandlerPlan, error) {
	p, v, err := h.source(e)
	if err != nil {
		return oc.HandlerPlan{}, err
	}
	binding, err := oc.EventDigest(e)
	if err != nil {
		return oc.HandlerPlan{}, err
	}
	deps, err := oc.NewDependencies(h.service.authority.issuer, binding, []f.LockRequest{projectLock(p, f.Shared), meetingLock(v.MeetingID.String()), inboxDecisionLock(v.DecisionID.String())}, nil)
	if err != nil {
		return oc.HandlerPlan{}, err
	}
	return oc.NewHandlerPlan(decisionHandler, e, deps)
}

func (h *decisionProjector) ValidateInTx(ctx context.Context, tx f.Tx, e event.Event, plan oc.HandlerPlan) error {
	binding, err := oc.EventDigest(e)
	if err != nil || !plan.Matches(decisionHandler, e) || !plan.Dependencies().Matches(h.service.authority.issuer, binding) {
		return fault(f.Forbidden)
	}
	if _, _, err = h.source(e); err != nil {
		return err
	}
	if _, err = h.service.authority.store.InTx(tx); err != nil {
		return portError(err)
	}
	return portError(h.service.authority.store.RequireHeldLocks(ctx, tx, plan.Locks()))
}

func (h *decisionProjector) HandleInTx(ctx context.Context, tx f.Tx, e event.Event, plan oc.HandlerPlan) oc.Result {
	if err := h.ValidateInTx(ctx, tx, e, plan); err != nil {
		return oc.Retry(oc.AuthorizationChanged)
	}
	p, v, err := h.source(e)
	if err != nil {
		return oc.Reject(oc.AuthorizationChanged)
	}
	var source c.Source
	err = h.service.authority.withDecisionSource(ctx, tx, p, v.MeetingID.String(), v.DecisionID.String(), false, func(ctx context.Context) error {
		var err error
		source, err = h.service.decisions.ReadDecisionSourceInTx(ctx, tx, p, v.MeetingID.String(), v.DecisionID.String())
		return err
	})
	if err != nil {
		return oc.Retry(oc.Unavailable)
	}
	if source.ProjectID != p || source.ID != v.DecisionID.String() || source.Context.MeetingID != v.MeetingID.String() || source.Version.Validate() != nil {
		return oc.Retry(oc.AuthorizationChanged)
	}
	x, err := h.service.authority.store.InTx(tx)
	if err != nil {
		return oc.Retry(oc.Unavailable)
	}
	if source.Relevant {
		if err = upsertProjection(ctx, x, source, decisionProjection); err != nil {
			return oc.Retry(oc.Unavailable)
		}
	}
	raw, _ := json.Marshal(struct {
		ID      string
		Context c.SourceContext
		Version f.Version
		Open    bool
	}{source.ID, source.Context, source.Version, source.Open})
	return oc.Ack(oc.DigestBytes(raw))
}

var _ c.DecisionSourceReadAuthority = (*Authority)(nil)
