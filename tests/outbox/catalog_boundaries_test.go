//go:build integration

package outbox_test

import (
	"context"
	"fmt"
	"testing"

	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/outbox"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
)

func TestOutboxCatalogLimitsAndPerTypeExtensionBoundary(t *testing.T) {
	f := newFixture(t)
	for n := 0; n < 128; n++ {
		h := f.handler(fmt.Sprintf("fixture.h%03d", n))
		if _, err := f.svc.RegisterHandler(ctxFor(t), h.definition(1)); err != nil {
			t.Fatal("bounded handler registration", n, err)
		}
	}
	if _, err := f.svc.RegisterHandler(ctxFor(t), f.handler("fixture.overflow").definition(1)); err == nil {
		t.Fatal("129 handlers admitted")
	} else {
		code(t, err, foundation.InvalidState)
	}
	e := f.event(t, false, 2, "fanout")
	receipt, result := f.append(t, f.svc, f.store, f.actor, e)
	state(t, result, foundation.Committed)
	if f.count(t, `SELECT count(*) FROM agenteam_outbox.deliveries`) != 128 {
		t.Fatal("fanout cap excluded unsupported schema or omitted handler")
	}
	h := f.handler("fixture.h000")
	def := h.definition(1, 2)
	def.Subscriptions = append(def.Subscriptions, oc.Subscription{EventType: "fixture.other", Versions: []uint32{1}})
	reg, err := f.svc.RegisterHandler(ctxFor(t), def)
	if err != nil || len(reg.Subscriptions) != 2 {
		t.Fatal("new type registration", err)
	}
	for _, s := range reg.Subscriptions {
		switch s.EventType {
		case "fixture.changed":
			if s.New || s.AcceptedAfter != 0 {
				t.Fatal("old type boundary replaced")
			}
		case "fixture.other":
			if !s.New || s.AcceptedAfter != foundation.Progress(receipt.Sequence) {
				t.Fatal("new type boundary not current")
			}
		default:
			t.Fatal("unknown subscription")
		}
	}
	if _, err = f.svc.RegisterHandler(ctxFor(t), h.definition(1, 2)); err == nil {
		t.Fatal("type removed without explicit migration")
	}
	if f.count(t, `SELECT count(*) FROM agenteam_outbox.subscriptions WHERE handler_id='fixture.h000'`) != 2 {
		t.Fatal("rejected declaration partially committed")
	}
	if err = f.svc.CheckStorage(ctxFor(t)); err != nil {
		t.Fatal(err)
	}
	f.sql(t, `ALTER SEQUENCE agenteam_outbox.outbox_sequence CACHE 2`)
	if err = f.svc.CheckStorage(ctxFor(t)); err == nil {
		t.Fatal("unsafe preallocated sequence cache accepted")
	}
}
func TestOutboxUnboundProvidersAndForeignPlansFailClosed(t *testing.T) {
	f := newFixture(t)
	e := f.event(t, true, 1, "authority")
	svc, err := outbox.New(f.store, f.cat, outbox.Authorizations{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = svc.PrepareAppend(ctxFor(t), f.actor, e); err == nil {
		t.Fatal("unbound producer accepted")
	} else {
		code(t, err, foundation.DependencyUnbound)
	}
	svc, err = outbox.New(f.store, f.cat, outbox.Authorizations{Producers: map[event.StableName]oc.ProducerAuthority{"fixture": f.auth}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = svc.PrepareAppend(ctxFor(t), f.actor, e); err == nil {
		t.Fatal("unbound Project accepted")
	} else {
		code(t, err, foundation.DependencyUnbound)
	}
	plan, err := f.svc.PrepareAppend(ctxFor(t), f.actor, e)
	if err != nil {
		t.Fatal(err)
	}
	// A capability from another actual Service cannot replace this issuer even
	// with the same Store, catalog, Actor, event and complete lock projection.
	svc, err = outbox.New(f.store, f.cat, outbox.Authorizations{Producers: map[event.StableName]oc.ProducerAuthority{"fixture": f.auth}, Projects: f.auth})
	if err != nil {
		t.Fatal(err)
	}
	result := f.store.WithinTx(ctxFor(t), cause(t), func(ctx context.Context, tx foundation.Tx) error {
		if err := f.store.AcquireAll(ctx, tx, plan.Locks()); err != nil {
			return err
		}
		_, err := svc.AppendEventInTx(ctx, tx, f.actor, e, plan)
		return err
	})
	state(t, result, foundation.NotCommitted)
	code(t, result.Fault(), foundation.InvalidArgument)
	if f.count(t, `SELECT count(*) FROM agenteam_outbox.events`) != 0 {
		t.Fatal("foreign plan wrote event")
	}
}

func TestOutboxStorageCheckRejectsMissingMarkerRelation(t *testing.T) {
	f := newFixture(t)
	f.sql(t, `DROP TABLE agenteam_outbox.processed`)
	if err := f.svc.CheckStorage(ctxFor(t)); err == nil {
		t.Fatal("missing marker storage reported available")
	}
}
