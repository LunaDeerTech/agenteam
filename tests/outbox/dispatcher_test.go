//go:build integration

package outbox_test

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/outbox"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
)

func startRuntime(t *testing.T, f *fixture, defs []oc.HandlerDefinition, options outbox.Options) *outbox.Runtime {
	t.Helper()
	r, err := outbox.NewRuntime(f.svc, defs, options)
	if err != nil {
		t.Fatal(err)
	}
	if err = r.Initialize(ctxFor(t)); err != nil {
		t.Fatal(err)
	}
	if err = r.Start(ctxFor(t)); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := r.Force(ctx); err != nil {
			t.Error(err)
		}
	})
	return r
}
func waitOutbox(t *testing.T, check func() bool) {
	t.Helper()
	ctx := ctxFor(t)
	tick := time.NewTicker(5 * time.Millisecond)
	defer tick.Stop()
	for !check() {
		select {
		case <-ctx.Done():
			t.Fatal("outbox observation timed out")
		case <-tick.C:
		}
	}
}
func distinctEvent(t *testing.T, f *fixture, value string) event.Event {
	t.Helper()
	h := f.event(t, true, 1, value).Header()
	h.AggregateID = id[event.Aggregate](t)
	e, err := event.NewEvent(f.types[1], h, payload{Value: value})
	if err != nil {
		t.Fatal(err)
	}
	return e
}
func TestOutboxRuntimeIndependentHandlersAndAttempts(t *testing.T) {
	f := newFixture(t)
	good, bad := f.handler("runtime.good"), f.handler("runtime.bad")
	bad.mode = "retry"
	r := startRuntime(t, f, []oc.HandlerDefinition{good.definition(1), bad.definition(1)}, outbox.Options{PollInterval: 2 * time.Millisecond, RetryBase: time.Millisecond})
	for i := 0; i < 3; i++ {
		_, commit := f.append(t, f.svc, f.store, f.actor, distinctEvent(t, f, "fact"))
		state(t, commit, foundation.Committed)
	}
	waitOutbox(t, func() bool {
		return f.count(t, `SELECT count(*) FROM agenteam_outbox.deliveries WHERE phase='succeeded'`) == 3 && f.count(t, `SELECT count(*) FROM agenteam_outbox.deliveries WHERE phase='failed'`) == 3
	})
	if good.calls.Load() != 3 || bad.calls.Load() != 24 || f.count(t, `SELECT count(*) FROM outbox_fixture.effects`) != 3 || f.count(t, `SELECT count(*) FROM agenteam_outbox.processed`) != 3 {
		t.Fatal("independent delivery or rollback/marker semantics changed")
	}
	if f.count(t, `SELECT count(*) FROM agenteam_outbox.attempts WHERE checkpoint='failed'`) != 3 || f.count(t, `SELECT count(*) FROM agenteam_outbox.attempts`) != 27 {
		t.Fatal("attempt history or eight-claim cycle lost")
	}
	r.StopClaims()
	if err := r.Drain(ctxFor(t)); err != nil {
		t.Fatal(err)
	}
	if !r.Joined() {
		t.Fatal("Drain without join")
	}
	_, commit := f.append(t, f.svc, f.store, f.actor, distinctEvent(t, f, "late-admitted-producer"))
	state(t, commit, foundation.Committed)
	if f.count(t, `SELECT count(*) FROM agenteam_outbox.deliveries WHERE phase='pending'`) != 2 {
		t.Fatal("StopClaims cut off Append or ran new handler")
	}
}

type concurrencyState struct {
	mu         sync.Mutex
	total, max int
	by, maxBy  map[event.StableName]int
	entered    chan event.StableName
	release    chan struct{}
}
type heldHandler struct {
	base  *handler
	state *concurrencyState
	calls atomic.Int64
}

func (h *heldHandler) Prepare(ctx context.Context, e event.Event) (oc.HandlerPlan, error) {
	// These fixture effects are independent by (event,handler). Reusing the
	// canonical fixture's aggregate EX would intentionally serialize handlers
	// and could not measure the dispatcher's four independent admissions.
	sum, err := oc.EventDigest(e)
	if err != nil {
		return oc.HandlerPlan{}, err
	}
	k, err := foundation.RecordLock(foundation.OutboxRecordLock, "fixture-effect:"+e.Header().EventID.String()+":"+string(h.base.name))
	if err != nil {
		return oc.HandlerPlan{}, err
	}
	d, err := oc.NewDependencies(h.base.issuer, sum, []foundation.LockRequest{{Key: k, Mode: foundation.Exclusive}}, nil)
	if err != nil {
		return oc.HandlerPlan{}, err
	}
	return oc.NewHandlerPlan(h.base.name, e, d)
}
func (h *heldHandler) ValidateInTx(ctx context.Context, tx foundation.Tx, e event.Event, p oc.HandlerPlan) error {
	return h.base.ValidateInTx(ctx, tx, e, p)
}
func (h *heldHandler) HandleInTx(ctx context.Context, tx foundation.Tx, e event.Event, p oc.HandlerPlan) oc.Result {
	h.calls.Add(1)
	s := h.state
	s.mu.Lock()
	s.total++
	s.by[h.base.name]++
	if s.total > s.max {
		s.max = s.total
	}
	if s.by[h.base.name] > s.maxBy[h.base.name] {
		s.maxBy[h.base.name] = s.by[h.base.name]
	}
	s.mu.Unlock()
	defer func() { s.mu.Lock(); s.total--; s.by[h.base.name]--; s.mu.Unlock() }()
	s.entered <- h.base.name
	<-s.release // Deliberately uncooperative fixture callback; no external I/O.
	return h.base.HandleInTx(ctx, tx, e, p)
}
func TestOutboxRuntimeLimitsAndActualCallbackJoin(t *testing.T) {
	f := newFixture(t)
	s := &concurrencyState{by: map[event.StableName]int{}, maxBy: map[event.StableName]int{}, entered: make(chan event.StableName, 12), release: make(chan struct{})}
	var once sync.Once
	release := func() { once.Do(func() { close(s.release) }) }
	defer release()
	var defs []oc.HandlerDefinition
	for _, name := range []string{"parallel.a", "parallel.b", "parallel.c"} {
		base := f.handler(name)
		h := &heldHandler{base: base, state: s}
		d := base.definition(1)
		d.Handler = h
		defs = append(defs, d)
	}
	r := startRuntime(t, f, defs, outbox.Options{PollInterval: 2 * time.Millisecond})
	// Release must run before the cleanup registered by startRuntime.
	t.Cleanup(release)
	for i := 0; i < 3; i++ {
		_, commit := f.append(t, f.svc, f.store, f.actor, distinctEvent(t, f, "held"))
		state(t, commit, foundation.Committed)
	}
	for i := 0; i < 4; i++ {
		select {
		case <-s.entered:
		case <-ctxFor(t).Done():
			t.Fatal("four independent callback admissions not reached")
		}
	}
	s.mu.Lock()
	if s.total != 4 || s.max != 4 {
		t.Fatal("global callback bound")
	}
	for _, n := range s.maxBy {
		if n > 2 {
			t.Fatal("per handler callback bound")
		}
	}
	s.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if err := r.Force(ctx); err == nil || r.Joined() {
		t.Fatal("cancelled but live callback reported joined")
	}
	if f.count(t, `SELECT count(*) FROM agenteam_outbox.attempts WHERE joined_at IS NOT NULL`) != 0 {
		t.Fatal("callback deadline fabricated DB join")
	}
	release()
	waitOutbox(t, r.Joined)
	if f.count(t, `SELECT count(*) FROM outbox_fixture.effects`) != 0 || f.count(t, `SELECT count(*) FROM agenteam_outbox.processed`) != 0 {
		t.Fatal("cancelled callback committed effects")
	}
	if f.count(t, `SELECT count(*) FROM agenteam_outbox.attempts`) != 4 {
		t.Fatal("new claim admitted after force")
	}
}
