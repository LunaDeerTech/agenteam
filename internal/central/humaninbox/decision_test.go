package humaninbox

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	c "github.com/LunaDeerTech/agenteam/internal/central/humaninbox/contract"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/meeting/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
)

func inboxID[T any](t *testing.T) f.ID[T] {
	t.Helper()
	id, err := f.NewID[T]()
	if err != nil {
		t.Fatal(err)
	}
	return id
}

type decisionReadStore struct {
	Store
	tx     f.Tx
	locks  []f.LockRequest
	checks int
}

func (s *decisionReadStore) InTx(tx f.Tx) (postgres.SQLExecutor, error) {
	if tx != s.tx {
		return nil, fault(f.Forbidden)
	}
	return s, nil
}
func (s *decisionReadStore) RequireHeldLocks(_ context.Context, tx f.Tx, locks []f.LockRequest) error {
	if tx != s.tx || len(locks) != len(s.locks) {
		return fault(f.Forbidden)
	}
	for idx, lock := range locks {
		if lock.Key.Canonical() != s.locks[idx].Key.Canonical() || lock.Mode != s.locks[idx].Mode {
			return fault(f.Forbidden)
		}
	}
	s.checks++
	return nil
}

func TestDecisionInboxSourceWitnessBindsOriginalBorrow(t *testing.T) {
	p, m, d := inboxID[i.Project](t), inboxID[mc.Meeting](t), inboxID[mc.Decision](t)
	tx := f.NewTx()
	store := &decisionReadStore{tx: tx, locks: []f.LockRequest{projectLock(p, f.Shared), meetingLock(m.String()), inboxDecisionLock(d.String())}}
	a := &Authority{store: store}
	check := func(ctx context.Context, tx f.Tx, store any, p i.ProjectID, meeting, decision string, scan bool) error {
		return a.RequireDecisionSourceReadInTx(ctx, tx, store, p, meeting, decision, scan)
	}
	deny := func(err error) {
		t.Helper()
		if err == nil {
			t.Fatal("unissued Decision read accepted")
		}
	}
	deny(check(context.Background(), tx, store, p, m.String(), d.String(), false))
	var retained context.Context
	err := a.withDecisionSource(context.Background(), tx, p, m.String(), d.String(), false, func(ctx context.Context) error {
		retained = ctx
		if err := check(ctx, tx, store, p, m.String(), d.String(), false); err != nil {
			return err
		}
		deny(check(ctx, f.NewTx(), store, p, m.String(), d.String(), false))
		deny(check(ctx, tx, &decisionReadStore{}, p, m.String(), d.String(), false))
		deny(check(ctx, tx, store, inboxID[i.Project](t), m.String(), d.String(), false))
		deny(check(ctx, tx, store, p, inboxID[mc.Meeting](t).String(), d.String(), false))
		deny(check(ctx, tx, store, p, m.String(), inboxID[mc.Decision](t).String(), false))
		deny(check(ctx, tx, store, p, "", "", true))
		deny(a.RequireSourceReadInTx(ctx, tx, store, p, m.String(), false))
		return nil
	})
	if err != nil || store.checks != 1 {
		t.Fatal(err, store.checks)
	}
	deny(check(retained, tx, store, p, m.String(), d.String(), false))
	store.locks = []f.LockRequest{projectLock(p, f.Exclusive)}
	if err = a.withDecisionSource(context.Background(), tx, p, "", "", true, func(ctx context.Context) error {
		return check(ctx, tx, store, p, "", "", true)
	}); err != nil {
		t.Fatal(err)
	}
	if store.checks != 2 {
		t.Fatal("scan did not require Project EX")
	}
}

func TestInboxSourceContextClosedPair(t *testing.T) {
	m := inboxID[mc.Meeting](t).String()
	for _, tt := range []struct {
		source, action, raw string
		valid               bool
	}{
		{"meeting", "meeting.review_proposal", `{}`, true},
		{"decision_request", "decision.answer", fmt.Sprintf(`{"meeting_id":%q}`, m), true},
		{"decision_request", "meeting.review_proposal", `{}`, false},
		{"meeting", "decision.answer", `{}`, false},
		{"meeting", "meeting.review_proposal", fmt.Sprintf(`{"meeting_id":%q}`, m), false},
		{"decision_request", "decision.answer", `{}`, false},
		{"decision_request", "decision.answer", `null`, false},
		{"decision_request", "decision.answer", `{"meeting_id":null}`, false},
		{"decision_request", "decision.answer", `{"meeting_id":"https://foreign.example"}`, false},
		{"decision_request", "decision.answer", fmt.Sprintf(`{"meeting_id":%q,"url":"/other"}`, m), false},
	} {
		_, err := decodeSourceContext(tt.source, tt.action, []byte(tt.raw))
		if (err == nil) != tt.valid {
			t.Fatal(tt, err)
		}
	}
}

func TestDecisionInboxProjectorRetainsTypedSourceAndPlan(t *testing.T) {
	catalog := event.NewCatalog()
	decisionTypes, err := mc.RegisterDecisionEvents(catalog)
	if err != nil {
		t.Fatal(err)
	}
	p, m, d := inboxID[i.Project](t), inboxID[mc.Meeting](t), inboxID[mc.Decision](t)
	project, _ := f.ParseID[event.Project](p.String())
	aggregate, _ := f.ParseID[event.Aggregate](d.String())
	at, _ := f.NewInstant(time.Now())
	version := f.Version(1)
	payload := mc.DecisionPayload{DecisionID: d, MeetingID: m, TurnID: inboxID[mc.Turn](t), ContributionID: inboxID[mc.Contribution](t), ParticipantID: inboxID[mc.Participant](t), ExecutionID: inboxID[i.Execution](t), Status: "pending"}
	header := event.Header{EventID: inboxID[event.EventIdentity](t), EventType: mc.DecisionRequestedName, SchemaVersion: 1, OccurredAt: at, Scope: event.Scope{Kind: event.ProjectScope, ProjectID: project}, AggregateType: mc.DecisionAggregate, AggregateID: aggregate, AggregateVersion: &version}
	ev, err := decisionTypes.New(header, payload)
	if err != nil {
		t.Fatal(err)
	}
	store := &decisionReadStore{tx: f.NewTx()}
	a := &Authority{store: store, issuer: oc.NewPlanIssuer()}
	h := &decisionProjector{&Service{authority: a, decisionEvents: decisionTypes}}
	plan, err := h.Prepare(context.Background(), ev)
	if err != nil {
		t.Fatal(err)
	}
	store.locks, err = oc.NormalizeLocks([]f.LockRequest{projectLock(p, f.Shared), meetingLock(m.String()), inboxDecisionLock(d.String())})
	if err != nil {
		t.Fatal(err)
	}
	if err = h.ValidateInTx(context.Background(), store.tx, ev, plan); err != nil {
		t.Fatal(err)
	}
	header.EventID = inboxID[event.EventIdentity](t)
	foreign, err := decisionTypes.New(header, payload)
	if err != nil {
		t.Fatal(err)
	}
	if err = h.ValidateInTx(context.Background(), store.tx, foreign, plan); err == nil {
		t.Fatal("foreign event reused plan")
	}
	other := &decisionProjector{&Service{authority: &Authority{store: store, issuer: oc.NewPlanIssuer()}, decisionEvents: decisionTypes}}
	if err = other.ValidateInTx(context.Background(), store.tx, ev, plan); err == nil {
		t.Fatal("foreign issuer reused plan")
	}
	_, err = (&Service{}).DecisionHandler()
	var failure *f.Fault
	if !errors.As(err, &failure) || failure.Code != f.DependencyUnbound {
		t.Fatal("unbound provider exposed handler", err)
	}
}

var _ c.DecisionSourceReadAuthority = (*Authority)(nil)
