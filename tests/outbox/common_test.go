//go:build integration

package outbox_test

import (
	"context"
	"encoding/json"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/outbox"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	pgfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/postgres"
	"github.com/jackc/pgx/v5"
)

func id[K any](t *testing.T) foundation.ID[K] {
	t.Helper()
	v, e := foundation.NewID[K]()
	if e != nil {
		t.Fatal(e)
	}
	return v
}
func ctxFor(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	t.Cleanup(cancel)
	return ctx
}
func cause(t *testing.T) foundation.TransactionCause {
	t.Helper()
	c, e := foundation.NewRecoveryCause("outbox.fixture", id[struct{}](t).String(), "")
	if e != nil {
		t.Fatal(e)
	}
	return c
}
func code(t *testing.T, err error, want foundation.Code) {
	t.Helper()
	var f *foundation.Fault
	if !errors.As(err, &f) || f.Code != want {
		t.Fatalf("want %s got %v", want, err)
	}
}
func state(t *testing.T, result foundation.CommitResult, want foundation.CommitState) {
	t.Helper()
	if result.State() != want {
		t.Fatalf("state %s wanted %s fault=%v", result.State(), want, result.Fault())
	}
}

type payload struct {
	Value string `json:"value"`
}
type fixture struct {
	db        *pgfixture.Database
	store     *postgres.Store
	svc       *outbox.Service
	cat       *event.Catalog
	types     map[uint32]event.EventType[payload]
	actor     identity.Actor
	project   event.ProjectID
	aggregate event.AggregateID
	auth      *authority
}

func openStore(t *testing.T, cfg postgres.Config) *postgres.Store {
	t.Helper()
	s, e := postgres.Open(ctxFor(t), cfg)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if e := s.ForceClose(ctx); e != nil {
			t.Error(e)
		}
	})
	return s
}
func newFixture(t *testing.T) *fixture {
	t.Helper()
	db := pgfixture.NewDatabase(t)
	cfg := db.Config(t, nil)
	m, e := postgres.NewMigrator(cfg)
	if e != nil {
		t.Fatal(e)
	}
	if r := m.Migrate(ctxFor(t)); !r.Migrated {
		t.Fatalf("migration %v", r.Fault)
	}
	s := openStore(t, cfg)
	if _, e = s.Exec(ctxFor(t), `CREATE SCHEMA outbox_fixture; CREATE TABLE outbox_fixture.authority(id uuid PRIMARY KEY,parent_id uuid NOT NULL,visible boolean NOT NULL DEFAULT true,active boolean NOT NULL DEFAULT true); CREATE TABLE outbox_fixture.facts(id uuid PRIMARY KEY,value text NOT NULL,version bigint NOT NULL); CREATE TABLE outbox_fixture.effects(event_id uuid NOT NULL,handler_id text NOT NULL,value text NOT NULL,PRIMARY KEY(event_id,handler_id));`); e != nil {
		t.Fatal(e)
	}
	c := event.NewCatalog()
	types := map[uint32]event.EventType[payload]{}
	for _, version := range []uint32{1, 2} {
		types[version], e = event.DefineEvent(c, event.Definition[payload]{Schema: event.Schema{Producer: "fixture", EventType: "fixture.changed", AggregateType: "fixture", Version: version}, Codec: event.JSONCodec[payload]{}, Validate: func(v payload) error {
			if v.Value == "" {
				return foundation.NewFault(foundation.InvalidArgument, foundation.NotStarted)
			}
			return nil
		}})
		if e != nil {
			t.Fatal(e)
		}
	}
	project := id[event.Project](t)
	aggregate := id[event.Aggregate](t)
	user := id[identity.User](t)
	actor, _ := identity.NewHuman(user, id[identity.Session](t))
	auth := &authority{store: s, issuer: oc.NewPlanIssuer(), process: id[oc.Process](t)}
	if _, e = s.Exec(ctxFor(t), `INSERT INTO outbox_fixture.authority(id,parent_id) VALUES($1,$2)`, project.String(), id[struct{}](t).String()); e != nil {
		t.Fatal(e)
	}
	svc, e := outbox.New(s, c, outbox.Authorizations{Producers: map[event.StableName]oc.ProducerAuthority{"fixture": auth}, Projects: auth, Processes: auth})
	if e != nil {
		t.Fatal(e)
	}
	if e = svc.CheckStorage(ctxFor(t)); e != nil {
		t.Fatal(e)
	}
	return &fixture{db, s, svc, c, types, actor, project, aggregate, auth}
}
func (f *fixture) event(t *testing.T, project bool, version uint32, value string) event.Event {
	t.Helper()
	at, _ := foundation.NewInstant(time.Now())
	v := foundation.Version(1)
	scope := event.Scope{Kind: event.SystemScope}
	if project {
		scope = event.Scope{Kind: event.ProjectScope, ProjectID: f.project}
	}
	e, err := event.NewEvent(f.types[version], event.Header{EventID: id[event.EventIdentity](t), EventType: "fixture.changed", SchemaVersion: version, OccurredAt: at, Scope: scope, AggregateType: "fixture", AggregateID: f.aggregate, AggregateVersion: &v}, payload{value})
	if err != nil {
		t.Fatal(err)
	}
	return e
}
func (f *fixture) append(t *testing.T, svc *outbox.Service, store *postgres.Store, actor identity.Actor, e event.Event) (oc.AppendReceipt, foundation.CommitResult) {
	t.Helper()
	plan, err := svc.PrepareAppend(ctxFor(t), actor, e)
	if err != nil {
		t.Fatal(err)
	}
	var receipt oc.AppendReceipt
	commit := store.WithinTx(ctxFor(t), cause(t), func(ctx context.Context, tx foundation.Tx) error {
		if err := store.AcquireAll(ctx, tx, plan.Locks()); err != nil {
			return err
		}
		var err error
		receipt, err = svc.AppendEventInTx(ctx, tx, actor, e, plan)
		return err
	})
	return receipt, commit
}
func (f *fixture) count(t *testing.T, query string, args ...any) int64 {
	t.Helper()
	var n int64
	if e := f.store.QueryRow(ctxFor(t), query, args...).Scan(&n); e != nil {
		t.Fatal(e)
	}
	return n
}
func (f *fixture) sql(t *testing.T, query string, args ...any) {
	t.Helper()
	if _, e := f.store.Exec(ctxFor(t), query, args...); e != nil {
		t.Fatal(e)
	}
}
func (f *fixture) delivery(t *testing.T, e event.Event, handler string) oc.DeliveryID {
	t.Helper()
	var raw string
	if err := f.store.QueryRow(ctxFor(t), `SELECT id::text FROM agenteam_outbox.deliveries WHERE event_id=$1 AND handler_id=$2`, e.Header().EventID.String(), handler).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	v, err := foundation.ParseID[oc.Delivery](raw)
	if err != nil {
		t.Fatal(err)
	}
	return v
}
func (f *fixture) claim(t *testing.T, d oc.DeliveryID) {
	t.Helper()
	a := id[oc.Attempt](t)
	result := f.store.WithinTx(ctxFor(t), cause(t), func(ctx context.Context, tx foundation.Tx) error {
		// Fixture-only writes actual claim rows; B01 intentionally has no dispatcher.
		x, _ := f.store.InTx(tx)
		if _, err := x.Exec(ctx, `INSERT INTO agenteam_outbox.attempts(id,delivery_id,process_id,fence,redrive_cycle,cycle_number,lifetime_number,deadline) VALUES($1,$2,$3,1,0,1,1,clock_timestamp()+interval '20 seconds')`, a.String(), d.String(), f.auth.process.String()); err != nil {
			return err
		}
		_, err := x.Exec(ctx, `UPDATE agenteam_outbox.deliveries SET phase='processing',current_attempt_id=$2,fence=1,cycle_attempts=1,lifetime_attempts=1 WHERE id=$1`, d.String(), a.String())
		return err
	})
	state(t, result, foundation.Committed)
}

type authority struct {
	store   *postgres.Store
	issuer  oc.PlanIssuer
	process oc.ProcessID
	current atomic.Int64
	newFact atomic.Int64
}

func summaryBinding(actor identity.Actor, s event.Summary) foundation.Digest {
	a, _ := oc.StableActor(actor)
	raw, _ := json.Marshal(struct {
		Actor string
		Event event.Summary
	}{a, s})
	return oc.DigestBytes(raw)
}
func (a *authority) DiscoverAppend(ctx context.Context, actor identity.Actor, s event.Summary) (oc.Dependencies, error) {
	var parent string
	if s.Header.Scope.Kind == event.ProjectScope {
		if e := a.store.QueryRow(ctx, `SELECT parent_id::text FROM outbox_fixture.authority WHERE id=$1`, s.Header.Scope.ProjectID.String()).Scan(&parent); e != nil {
			return oc.Dependencies{}, e
		}
	}
	var locks []foundation.LockRequest
	if parent != "" {
		key, _ := foundation.AggregateLock(foundation.ExecutionAggregate, parent)
		locks = append(locks, foundation.LockRequest{Key: key, Mode: foundation.Shared})
	}
	return oc.NewDependencies(a.issuer, summaryBinding(actor, s), locks, []byte(parent))
}
func (a *authority) ValidateAppendInTx(ctx context.Context, tx foundation.Tx, actor identity.Actor, s event.Summary, d oc.Dependencies, stage oc.Stage) error {
	if !d.Matches(a.issuer, summaryBinding(actor, s)) {
		return foundation.NewFault(foundation.InvalidArgument, foundation.NotStarted)
	}
	if err := a.store.RequireHeldLocks(ctx, tx, d.Locks()); err != nil {
		return err
	}
	if stage == oc.CurrentAccess {
		a.current.Add(1)
	} else {
		a.newFact.Add(1)
	}
	if s.Header.Scope.Kind == event.SystemScope {
		return nil
	}
	x, _ := a.store.InTx(tx)
	var parent string
	var visible, active bool
	err := x.QueryRow(ctx, `SELECT parent_id::text,visible,active FROM outbox_fixture.authority WHERE id=$1`, s.Header.Scope.ProjectID.String()).Scan(&parent, &visible, &active)
	if err != nil {
		return err
	}
	if parent != string(d.Opaque()) {
		return foundation.NewFault(foundation.ResourceBusy, foundation.NotStarted)
	}
	if !visible {
		return foundation.NewFault(foundation.Forbidden, foundation.NotStarted)
	}
	if stage == oc.NewFact && !active {
		return foundation.NewFault(foundation.ProjectNotActive, foundation.NotStarted)
	}
	return nil
}
func projectBinding(r oc.ProjectRequest) foundation.Digest {
	d := r.Details()
	a, _ := oc.StableActor(d.Actor)
	b, _ := json.Marshal(struct {
		Kind     oc.ProjectAction
		Project  string
		Actor    string
		Event    event.Summary
		Delivery oc.DeliveryIdentity
	}{d.Kind, d.ProjectID.String(), a, d.Event, d.Delivery})
	return oc.DigestBytes(b)
}
func (a *authority) Discover(ctx context.Context, r oc.ProjectRequest) (oc.Dependencies, error) {
	return oc.NewDependencies(a.issuer, projectBinding(r), nil, nil)
}
func (a *authority) ValidateInTx(ctx context.Context, tx foundation.Tx, r oc.ProjectRequest, d oc.Dependencies) error {
	if !d.Matches(a.issuer, projectBinding(r)) {
		return foundation.NewFault(foundation.InvalidArgument, foundation.NotStarted)
	}
	x, _ := a.store.InTx(tx)
	var visible, active bool
	if err := x.QueryRow(ctx, `SELECT visible,active FROM outbox_fixture.authority WHERE id=$1`, r.Details().ProjectID.String()).Scan(&visible, &active); err != nil {
		return err
	}
	if !visible {
		return foundation.NewFault(foundation.Forbidden, foundation.NotStarted)
	}
	if r.Details().Stage == oc.NewFact && !active {
		return foundation.NewFault(foundation.ProjectNotActive, foundation.NotStarted)
	}
	return nil
}
func (a *authority) CurrentProcess() oc.ProcessID { return a.process }
func (a *authority) ConfirmStopped(context.Context, oc.ProcessID) error {
	return foundation.NewFault(foundation.ResourceBusy, foundation.NotStarted)
}

type handler struct {
	store  *postgres.Store
	name   event.StableName
	issuer oc.PlanIssuer
	calls  atomic.Int64
	mode   string
	types  map[uint32]event.EventType[payload]
}

func (f *fixture) handler(name string) *handler {
	return &handler{store: f.store, name: event.StableName(name), issuer: oc.NewPlanIssuer(), types: f.types}
}
func (h *handler) definition(versions ...uint32) oc.HandlerDefinition {
	return oc.HandlerDefinition{ID: h.name, Subscriptions: []oc.Subscription{{EventType: "fixture.changed", Versions: versions}}, Effect: oc.CanonicalConverge, Ordering: oc.VersionGuarded, Handler: h}
}
func (h *handler) Prepare(ctx context.Context, e event.Event) (oc.HandlerPlan, error) {
	sum, _ := oc.EventDigest(e)
	key, _ := foundation.AggregateLock(foundation.ExecutionAggregate, e.Header().AggregateID.String())
	d, err := oc.NewDependencies(h.issuer, sum, []foundation.LockRequest{{Key: key, Mode: foundation.Exclusive}}, nil)
	if err != nil {
		return oc.HandlerPlan{}, err
	}
	return oc.NewHandlerPlan(h.name, e, d)
}
func (h *handler) ValidateInTx(ctx context.Context, tx foundation.Tx, e event.Event, p oc.HandlerPlan) error {
	sum, _ := oc.EventDigest(e)
	if !p.Dependencies().Matches(h.issuer, sum) {
		return foundation.NewFault(foundation.InvalidArgument, foundation.NotStarted)
	}
	return h.store.RequireHeldLocks(ctx, tx, p.Locks())
}
func (h *handler) HandleInTx(ctx context.Context, tx foundation.Tx, e event.Event, p oc.HandlerPlan) oc.Result {
	h.calls.Add(1)
	v, err := event.DecodeEvent(h.types[e.Header().SchemaVersion], e)
	if err != nil {
		return oc.Reject(oc.InvalidEvent)
	}
	x, err := h.store.InTx(tx)
	if err != nil {
		return oc.Retry(oc.Unavailable)
	}
	if _, err = x.Exec(ctx, `INSERT INTO outbox_fixture.effects(event_id,handler_id,value) VALUES($1,$2,$3)`, e.Header().EventID.String(), string(h.name), v.Value); err != nil {
		return oc.Retry(oc.Unavailable)
	}
	switch h.mode {
	case "retry":
		return oc.Retry(oc.HandlerRetry)
	case "reject":
		return oc.Reject(oc.SourceTerminal)
	case "panic":
		panic("private-handler-panic")
	}
	return oc.Ack(oc.DigestBytes([]byte(v.Value)))
}
func waitDB(t *testing.T, conn *pgx.Conn, query string, args ...any) {
	t.Helper()
	ctx := ctxFor(t)
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	for {
		var ready bool
		if err := conn.QueryRow(ctx, query, args...).Scan(&ready); err != nil {
			t.Fatal("database barrier query failed")
		}
		if ready {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatal("database barrier timed out")
		case <-ticker.C:
		}
	}
}
