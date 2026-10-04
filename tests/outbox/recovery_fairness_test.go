//go:build integration

package outbox_test

import (
	"context"
	"testing"
	"time"

	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/outbox"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
)

func TestOutboxRuntimeProtectedSixtyFourDoNotStarveTail(t *testing.T) {
	f := newFixture(t)
	h := f.handler("fairness.handler")
	if _, err := f.svc.RegisterHandler(ctxFor(t), h.definition(1)); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 64; i++ {
		e := distinctEvent(t, f, "protected")
		_, result := f.append(t, f.svc, f.store, f.actor, e)
		state(t, result, foundation.Committed)
		d := f.delivery(t, e, string(h.name))
		f.claim(t, d)
		f.sql(t, `UPDATE agenteam_outbox.deliveries SET phase='retry_wait',next_attempt_at=clock_timestamp()-interval '1 hour' WHERE id=$1`, d.String())
		f.sql(t, `UPDATE agenteam_outbox.attempts SET process_id=$2,deadline=started_at+interval '1 microsecond' WHERE delivery_id=$1`, d.String(), id[oc.Process](t).String())
	}
	e := distinctEvent(t, f, "tail")
	_, result := f.append(t, f.svc, f.store, f.actor, e)
	state(t, result, foundation.Committed)
	r := startRuntime(t, f, []oc.HandlerDefinition{h.definition(1)}, outbox.Options{PollInterval: time.Millisecond, ItemTimeout: 20 * time.Millisecond})
	waitOutbox(t, func() bool {
		return f.count(t, `SELECT count(*) FROM agenteam_outbox.processed WHERE event_id=$1`, e.Header().EventID.String()) == 1
	})
	r.StopClaims()
	if err := r.Drain(ctxFor(t)); err != nil {
		t.Fatal(err)
	}
	if h.calls.Load() != 1 || f.count(t, `SELECT count(*) FROM agenteam_outbox.deliveries WHERE phase='retry_wait' AND fence=1`) != 64 || f.count(t, `SELECT count(*) FROM agenteam_outbox.attempts WHERE joined_at IS NULL AND handler_returned_at IS NULL`) != 64 {
		t.Fatal("fair progress stole protected owner/fence or invented return time")
	}
}

type canonicalHandler struct{ base *handler }

func (h canonicalHandler) Prepare(ctx context.Context, e event.Event) (oc.HandlerPlan, error) {
	return h.base.Prepare(ctx, e)
}
func (h canonicalHandler) ValidateInTx(ctx context.Context, tx foundation.Tx, e event.Event, p oc.HandlerPlan) error {
	return h.base.ValidateInTx(ctx, tx, e, p)
}
func (h canonicalHandler) HandleInTx(ctx context.Context, tx foundation.Tx, e event.Event, _ oc.HandlerPlan) oc.Result {
	h.base.calls.Add(1)
	x, err := h.base.store.InTx(tx)
	if err != nil {
		return oc.Retry(oc.Unavailable)
	}
	var generation int64
	var value string
	var deleted bool
	if err = x.QueryRow(ctx, `SELECT generation,value,deleted FROM outbox_fixture.canonical_source WHERE id=$1`, e.Header().AggregateID.String()).Scan(&generation, &value, &deleted); err != nil {
		return oc.Reject(oc.SourceTerminal)
	}
	// Reconcile from a locked current source. A payload (including an old scan
	// baseline) is only a dirty notification and never reconstructs deleted data.
	if deleted {
		_, err = x.Exec(ctx, `DELETE FROM outbox_fixture.canonical_projection WHERE id=$1 AND generation<=$2`, e.Header().AggregateID.String(), generation)
	} else {
		_, err = x.Exec(ctx, `INSERT INTO outbox_fixture.canonical_projection(id,generation,value) VALUES($1,$2,$3) ON CONFLICT(id) DO UPDATE SET generation=excluded.generation,value=excluded.value WHERE outbox_fixture.canonical_projection.generation<=excluded.generation`, e.Header().AggregateID.String(), generation, value)
	}
	if err != nil {
		return oc.Retry(oc.Unavailable)
	}
	raw := []byte(value)
	if deleted {
		raw = []byte("canonical tombstone")
	}
	return oc.Ack(oc.DigestBytes(raw))
}
func TestOutboxRuntimeCanonicalVersionSiblingsAndLateBootstrapDirty(t *testing.T) {
	f := newFixture(t)
	f.sql(t, `CREATE TABLE outbox_fixture.canonical_source(id uuid PRIMARY KEY,generation bigint NOT NULL,value text NOT NULL,deleted boolean NOT NULL);CREATE TABLE outbox_fixture.canonical_projection(id uuid PRIMARY KEY,generation bigint NOT NULL,value text NOT NULL)`)
	f.sql(t, `INSERT INTO outbox_fixture.canonical_source VALUES($1,43,'current 43',false)`, f.aggregate.String())
	base := f.handler("canonical.handler")
	definition := base.definition(1)
	definition.Ordering = oc.CanonicalReconcile
	definition.Handler = canonicalHandler{base}
	if _, err := f.svc.RegisterHandler(ctxFor(t), definition); err != nil {
		t.Fatal(err)
	}
	// A late scan took this old baseline before the current generation changed.
	// Its own conditional write must not overwrite the concurrent newer result.
	events := make([]event.Event, 0, 3)
	for _, v := range []foundation.Version{43, 42, 43} {
		header := f.event(t, true, 1, "old payload must not win").Header()
		header.AggregateVersion = &v
		e, err := event.NewEvent(f.types[1], header, payload{Value: "historical private payload"})
		if err != nil {
			t.Fatal(err)
		}
		events = append(events, e)
		_, result := f.append(t, f.svc, f.store, f.actor, e)
		state(t, result, foundation.Committed)
	}
	r := startRuntime(t, f, []oc.HandlerDefinition{definition}, outbox.Options{PollInterval: time.Millisecond})
	waitOutbox(t, func() bool { return f.count(t, `SELECT count(*) FROM agenteam_outbox.processed`) == 3 })
	f.sql(t, `INSERT INTO outbox_fixture.canonical_projection(id,generation,value) VALUES($1,42,'late baseline') ON CONFLICT(id) DO UPDATE SET generation=excluded.generation,value=excluded.value WHERE outbox_fixture.canonical_projection.generation<=excluded.generation`, f.aggregate.String())
	if f.count(t, `SELECT count(*) FROM outbox_fixture.canonical_projection WHERE generation=43 AND value='current 43'`) != 1 {
		t.Fatal("v42/scan/sibling overwrote current canonical value")
	}
	// A missing sequence is legal because this handler re-reads current source;
	// the explicit generation proof remains mandatory for Ack/marker.
	f.sql(t, `UPDATE outbox_fixture.canonical_source SET generation=44,deleted=true WHERE id=$1`, f.aggregate.String())
	last := f.event(t, true, 1, "must never resurrect deleted body")
	_, result := f.append(t, f.svc, f.store, f.actor, last)
	state(t, result, foundation.Committed)
	waitOutbox(t, func() bool { return f.count(t, `SELECT count(*) FROM agenteam_outbox.processed`) == 4 })
	if f.count(t, `SELECT count(*) FROM outbox_fixture.canonical_projection`) != 0 {
		t.Fatal("dirty delete replay resurrected old payload")
	}
	// Same Event replay creates neither a delivery nor a second callback.
	_, result = f.append(t, f.svc, f.store, f.actor, events[0])
	state(t, result, foundation.Committed)
	r.StopClaims()
	if err := r.Drain(ctxFor(t)); err != nil {
		t.Fatal(err)
	}
	if base.calls.Load() != 4 {
		t.Fatal("event replay invoked canonical effect again")
	}
}

type unavailableDeathProof struct{ current oc.ProcessID }

func (p unavailableDeathProof) CurrentProcess() oc.ProcessID { return p.current }
func (unavailableDeathProof) ConfirmStopped(context.Context, oc.ProcessID) error {
	return foundation.NewFault(foundation.DependencyUnbound, foundation.NotStarted)
}
func TestOutboxRuntimeUnknownDeathProofIsProtectedButHardDBFailureIsNotSwallowed(t *testing.T) {
	f := newFixture(t)
	h := f.handler("recover.safety")
	if _, err := f.svc.RegisterHandler(ctxFor(t), h.definition(1)); err != nil {
		t.Fatal(err)
	}
	var deliveries []oc.DeliveryID
	for i := 0; i < 3; i++ {
		e := distinctEvent(t, f, "recovery fixture")
		_, result := f.append(t, f.svc, f.store, f.actor, e)
		state(t, result, foundation.Committed)
		d := f.delivery(t, e, string(h.name))
		f.claim(t, d)
		deliveries = append(deliveries, d)
	}
	plan, err := f.svc.PrepareDelivery(ctxFor(t), deliveries[2])
	if err != nil {
		t.Fatal(err)
	}
	result, _ := f.svc.ApplyDelivery(ctxFor(t), plan)
	state(t, result, foundation.Committed)
	f.sql(t, `UPDATE agenteam_outbox.deliveries SET phase='processing' WHERE id=$1`, deliveries[2].String())
	current := id[oc.Process](t)
	svc, err := outbox.New(f.store, f.cat, outbox.Authorizations{Producers: map[event.StableName]oc.ProducerAuthority{"fixture": f.auth}, Projects: f.auth, Processes: unavailableDeathProof{current}})
	if err != nil {
		t.Fatal(err)
	}
	// The first and last Process proof cannot be bound here. The second item
	// encounters a genuine PostgreSQL write failure; later marker work must run.
	f.sql(t, `CREATE FUNCTION outbox_fixture.fail_recovery() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF OLD.id='`+deliveries[1].String()+`'::uuid THEN RAISE EXCEPTION 'private-recovery-failure'; END IF; RETURN NEW; END $$; CREATE TRIGGER fail_recovery BEFORE UPDATE ON agenteam_outbox.deliveries FOR EACH ROW EXECUTE FUNCTION outbox_fixture.fail_recovery()`)
	r, err := outbox.NewRuntime(svc, []oc.HandlerDefinition{h.definition(1)}, outbox.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = r.Force(ctx)
	})
	if err = r.Initialize(ctxFor(t)); err == nil {
		t.Fatal("hard recovery failure folded into protected success")
	}
	if f.count(t, `SELECT count(*) FROM agenteam_outbox.deliveries WHERE id=$1 AND phase='succeeded'`, deliveries[2].String()) != 1 || f.count(t, `SELECT count(*) FROM agenteam_outbox.deliveries WHERE id=$1 AND phase='processing' AND fence=1`, deliveries[0].String()) != 1 {
		t.Fatal("recovery stopped at earlier error or stole unknown owner")
	}
	f.sql(t, `DROP TRIGGER fail_recovery ON agenteam_outbox.deliveries;DROP FUNCTION outbox_fixture.fail_recovery()`)
	if err = r.Initialize(ctxFor(t)); err != nil {
		t.Fatal("safe unbound death proof prevented initialization", err)
	}
	if err = r.Start(ctxFor(t)); err != nil {
		t.Fatal(err)
	}
	if err = r.Check(ctxFor(t)); err != nil {
		t.Fatal(err)
	}
	r.StopClaims()
	if err = r.Drain(ctxFor(t)); err != nil {
		t.Fatal(err)
	}
	if f.count(t, `SELECT count(*) FROM agenteam_outbox.attempts`) != 3 {
		t.Fatal("unbound death proof invented another claim")
	}
}
