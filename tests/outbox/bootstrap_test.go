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

// This is a real test consumer, not a future production projection. Its own
// generation and dirty queue demonstrate the registration/consumer boundary.
type bootstrapHandler struct{ *handler }

func (h bootstrapHandler) HandleInTx(ctx context.Context, tx foundation.Tx, e event.Event, _ oc.HandlerPlan) oc.Result {
	h.calls.Add(1)
	x, err := h.store.InTx(tx)
	if err != nil {
		return oc.Retry(oc.Unavailable)
	}
	var ready bool
	if err = x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM outbox_fixture.bootstrap_generation WHERE generation=9)`).Scan(&ready); err != nil || !ready {
		return oc.Retry(oc.Unavailable)
	}
	if _, err = x.Exec(ctx, `INSERT INTO outbox_fixture.bootstrap_dirty(generation,id) VALUES(9,$1) ON CONFLICT DO NOTHING`, e.Header().AggregateID.String()); err != nil {
		return oc.Retry(oc.Unavailable)
	}
	return oc.Ack(oc.DigestBytes([]byte("fixture generation 9 durable dirty")))
}

func TestOutboxBootstrapGenerationDirtyAndDeletionDuringScan(t *testing.T) {
	f := newFixture(t)
	f.sql(t, `CREATE TABLE outbox_fixture.bootstrap_source(id uuid PRIMARY KEY,version bigint NOT NULL,value text NOT NULL);
 CREATE TABLE outbox_fixture.bootstrap_generation(generation bigint PRIMARY KEY,phase text NOT NULL);
 CREATE TABLE outbox_fixture.bootstrap_dirty(generation bigint REFERENCES outbox_fixture.bootstrap_generation(generation),id uuid,PRIMARY KEY(generation,id));
 CREATE TABLE outbox_fixture.bootstrap_projection(generation bigint,id uuid,version bigint,value text,PRIMARY KEY(generation,id))`)
	f.sql(t, `INSERT INTO outbox_fixture.bootstrap_source VALUES($1,42,'scan version 42')`, f.aggregate.String())
	old := f.event(t, true, 1, "historical body is not bootstrap data")
	_, result := f.append(t, f.svc, f.store, f.actor, old)
	state(t, result, foundation.Committed)
	h := bootstrapHandler{f.handler("bootstrap.handler")}
	definition := h.definition(1)
	definition.Handler, definition.Ordering = h, oc.CanonicalReconcile
	registration, err := f.svc.RegisterHandler(ctxFor(t), definition)
	if err != nil || len(registration.Subscriptions) != 1 || !registration.Subscriptions[0].New {
		t.Fatal("late registration", err)
	}
	if f.count(t, `SELECT count(*) FROM agenteam_outbox.deliveries`) != 0 {
		t.Fatal("late registration replayed historical payload")
	}
	first := f.event(t, true, 1, "dirty before generation")
	_, result = f.append(t, f.svc, f.store, f.actor, first)
	state(t, result, foundation.Committed)
	r := startRuntime(t, f, []oc.HandlerDefinition{definition}, outbox.Options{PollInterval: time.Millisecond, RetryBase: 100 * time.Millisecond})
	waitOutbox(t, func() bool {
		return f.count(t, `SELECT count(*) FROM agenteam_outbox.deliveries WHERE phase='retry_wait'`) == 1
	})
	if f.count(t, `SELECT count(*) FROM agenteam_outbox.processed`) != 0 || f.count(t, `SELECT count(*) FROM outbox_fixture.bootstrap_dirty`) != 0 {
		t.Fatal("missing generation acknowledged and lost notification")
	}
	// The scan captures a baseline, then the canonical source changes/deletes
	// before that baseline is written. New events remain durable dirty IDs.
	var oldVersion int64
	var oldValue string
	if err = f.store.QueryRow(ctxFor(t), `SELECT version,value FROM outbox_fixture.bootstrap_source WHERE id=$1`, f.aggregate.String()).Scan(&oldVersion, &oldValue); err != nil {
		t.Fatal(err)
	}
	f.sql(t, `INSERT INTO outbox_fixture.bootstrap_generation VALUES(9,'scanning')`)
	waitOutbox(t, func() bool { return f.count(t, `SELECT count(*) FROM agenteam_outbox.processed`) == 1 })
	for _, deleted := range []bool{false, true} {
		e := f.event(t, true, 1, "old payload never reconstructs canonical")
		plan, err := f.svc.PrepareAppend(ctxFor(t), f.actor, e)
		if err != nil {
			t.Fatal(err)
		}
		key, _ := foundation.AggregateLock(foundation.ExecutionAggregate, f.aggregate.String())
		result = f.store.WithinTx(ctxFor(t), cause(t), func(ctx context.Context, tx foundation.Tx) error {
			locks := append(plan.Locks(), foundation.LockRequest{Key: key, Mode: foundation.Exclusive})
			if err := f.store.AcquireAll(ctx, tx, locks); err != nil {
				return err
			}
			x, _ := f.store.InTx(tx)
			query := `UPDATE outbox_fixture.bootstrap_source SET version=43,value='current 43' WHERE id=$1`
			if deleted {
				query = `DELETE FROM outbox_fixture.bootstrap_source WHERE id=$1`
			}
			if _, err := x.Exec(ctx, query, f.aggregate.String()); err != nil {
				return err
			}
			_, err := f.svc.AppendEventInTx(ctx, tx, f.actor, e, plan)
			return err
		})
		state(t, result, foundation.Committed)
	}
	waitOutbox(t, func() bool { return f.count(t, `SELECT count(*) FROM agenteam_outbox.processed`) == 3 })
	f.sql(t, `INSERT INTO outbox_fixture.bootstrap_projection VALUES(9,$1,$2,$3)`, f.aggregate.String(), oldVersion, oldValue)
	if f.count(t, `SELECT count(*) FROM outbox_fixture.bootstrap_dirty WHERE generation=9`) != 1 {
		t.Fatal("source deletion was not a durable dirty ID")
	}
	key, _ := foundation.AggregateLock(foundation.ExecutionAggregate, f.aggregate.String())
	result = f.store.WithinTx(ctxFor(t), cause(t), func(ctx context.Context, tx foundation.Tx) error {
		if err := f.store.AcquireAll(ctx, tx, []foundation.LockRequest{{Key: key, Mode: foundation.Exclusive}}); err != nil {
			return err
		}
		x, _ := f.store.InTx(tx)
		_, err := x.Exec(ctx, `DELETE FROM outbox_fixture.bootstrap_projection p USING outbox_fixture.bootstrap_dirty d WHERE p.generation=d.generation AND p.id=d.id AND NOT EXISTS(SELECT 1 FROM outbox_fixture.bootstrap_source s WHERE s.id=d.id);
 DELETE FROM outbox_fixture.bootstrap_dirty WHERE generation=9;
 UPDATE outbox_fixture.bootstrap_generation SET phase='active' WHERE generation=9`)
		return err
	})
	state(t, result, foundation.Committed)
	if f.count(t, `SELECT count(*) FROM outbox_fixture.bootstrap_projection`) != 0 || f.count(t, `SELECT count(*) FROM outbox_fixture.bootstrap_generation WHERE phase='active'`) != 1 {
		t.Fatal("stale scan resurrected deleted canonical body")
	}
	r.StopClaims()
	if err := r.Drain(ctxFor(t)); err != nil {
		t.Fatal(err)
	}
}
