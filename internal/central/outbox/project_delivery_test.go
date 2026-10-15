package outbox

import (
	"context"
	"errors"
	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/meeting/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"testing"
	"time"
)

type deliveryGuard struct {
	process oc.ProcessID
	tx      f.Tx
	closed  bool
	checks  int
}

func (g *deliveryGuard) CurrentProcess() oc.ProcessID { return g.process }
func (g *deliveryGuard) ConfirmStopped(context.Context, oc.ProcessID) error {
	return errors.New("current process is live")
}
func (g *deliveryGuard) RequireCurrentProcessInTx(_ context.Context, tx f.Tx, p oc.ProcessID) error {
	g.checks++
	if g.closed || tx != g.tx || p != g.process {
		return errors.New("original guard unavailable")
	}
	return nil
}
func deliveryTestID[T any](t *testing.T) f.ID[T] {
	t.Helper()
	v, e := f.NewID[T]()
	if e != nil {
		t.Fatal(e)
	}
	return v
}
func projectDeliveryFixture(t *testing.T) (*Service, *terminalInspectStore, *deliveryGuard, deliverySnapshot, []f.LockRequest) {
	t.Helper()
	catalog := event.NewCatalog()
	types, err := mc.RegisterEvents(catalog)
	if err != nil {
		t.Fatal(err)
	}
	p := deliveryTestID[event.Project](t)
	m := deliveryTestID[mc.Meeting](t)
	ag, _ := f.ParseID[event.Aggregate](m.String())
	at, _ := f.NewInstant(time.Now())
	version := f.Version(1)
	e, err := types.New(event.Header{EventID: deliveryTestID[event.EventIdentity](t), EventType: mc.ProposedName, SchemaVersion: 1, OccurredAt: at, Scope: event.Scope{Kind: event.ProjectScope, ProjectID: p}, AggregateType: mc.MeetingAggregate, AggregateID: ag, AggregateVersion: &version}, mc.LifecyclePayload{MeetingID: m, Status: "proposed"})
	if err != nil {
		t.Fatal(err)
	}
	store := &terminalInspectStore{tx: f.NewTx()}
	key, _ := f.ProjectLock(p.String())
	locks := []f.LockRequest{{Key: key, Mode: f.Shared}}
	store.locks = locks
	guard := &deliveryGuard{process: deliveryTestID[oc.Process](t), tx: store.tx}
	state := &serviceState{store: store, catalog: catalog, auth: Authorizations{Processes: guard}}
	s := &Service{func() *serviceState { return state }}
	snap := deliverySnapshot{identity: oc.DeliveryIdentity{EventID: e.Header().EventID, DeliveryID: deliveryTestID[oc.Delivery](t), AttemptID: deliveryTestID[oc.Attempt](t), Fence: 1, HandlerID: "humaninbox.meeting_proposal", Scope: e.Header().Scope, Effect: oc.CanonicalConverge}, process: guard.process, phase: oc.Processing, event: e, deadline: time.Now().Add(time.Minute)}
	return s, store, guard, snap, locks
}
func TestProjectDeliveryWitnessRequiresOriginalLiveCallback(t *testing.T) {
	s, store, guard, snap, locks := projectDeliveryFixture(t)
	r, err := deliveryProjectRequest(snap)
	if err != nil {
		t.Fatal(err)
	}
	if RequireProjectDelivery(context.Background(), store.tx, store, r) == nil {
		t.Fatal("bare service actor and delivery IDs granted authority")
	}
	var retained context.Context
	for _, tx := range []f.Tx{{}, store.tx} {
		err = s.withProjectDelivery(context.Background(), tx, snap, locks, func(ctx context.Context, r oc.ProjectRequest) error {
			retained = ctx
			return RequireProjectDelivery(ctx, tx, store, r)
		})
		if err != nil {
			t.Fatal(err)
		}
		if RequireProjectDelivery(retained, tx, store, r) == nil {
			t.Fatal("expired callback granted authority")
		}
	}
	if guard.checks != 1 {
		t.Fatal("discovery granted a transaction, or original guard was skipped")
	}
}
func TestProjectDeliveryWitnessRejectsForeignAndRetiredIdentity(t *testing.T) {
	for _, kind := range []string{"store", "transaction", "closed-transaction", "locks", "process", "guard", "attempt", "fence", "event", "payload", "handler"} {
		t.Run(kind, func(t *testing.T) {
			s, store, guard, snap, locks := projectDeliveryFixture(t)
			err := s.withProjectDelivery(context.Background(), store.tx, snap, locks, func(ctx context.Context, r oc.ProjectRequest) error {
				tx, target := store.tx, any(store)
				d := r.Details()
				switch kind {
				case "store":
					target = &terminalInspectStore{tx: store.tx, locks: locks}
				case "transaction":
					tx = f.NewTx()
				case "closed-transaction":
					store.tx = f.Tx{}
				case "locks":
					store.locks = nil
				case "process":
					guard.process = deliveryTestID[oc.Process](t)
				case "guard":
					guard.closed = true
				case "attempt":
					d.Delivery.AttemptID = deliveryTestID[oc.Attempt](t)
				case "fence":
					d.Delivery.Fence++
				case "event":
					d.Event.Header.EventID = deliveryTestID[event.EventIdentity](t)
					d.Delivery.EventID = d.Event.Header.EventID
				case "payload":
					d.Event.PayloadDigest = oc.DigestBytes([]byte("foreign payload"))
				case "handler":
					d.Delivery.HandlerID = "another.handler"
				}
				cause, _ := d.Delivery.CauseRef()
				scope, _ := i.InProject(d.ProjectID)
				role, _ := i.RegisterService(i.OutboxDelivery)
				d.Actor, _ = role.Actor(cause, scope)
				changed, err := oc.NewProjectRequest(d)
				if err != nil {
					t.Fatal(err)
				}
				return RequireProjectDelivery(ctx, tx, target, changed)
			})
			if err == nil {
				t.Fatal("foreign or retired original call admitted")
			}
		})
	}
}
