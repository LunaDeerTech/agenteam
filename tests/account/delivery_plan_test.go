//go:build integration

package account_test

import (
	"context"
	"testing"

	c "github.com/LunaDeerTech/agenteam/internal/central/account/contract"
	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
)

func TestAccountDeliveryPlanRejectsChangedSourceMapping(t *testing.T) {
	f := newB02Account(t)
	a := b02Admin(t, f)
	r, _ := c.NewInvitationCreate(c.InvitationCreateFields{Actor: a, Key: "delivery-mapping", Email: "mapping@example.com"})
	v, e := f.service.CreateInvitation(ctxFor(t), r)
	if e != nil {
		t.Fatal(e)
	}
	var header []byte
	var rawID string
	if e = f.store.QueryRow(ctxFor(t), `SELECT e.header,e.id::text FROM agenteam_outbox.events e WHERE event_type='account.delivery-requested'`).Scan(&header, &rawID); e != nil {
		t.Fatal(e)
	}
	h, e := event.DecodeHeader(header)
	if e != nil {
		t.Fatal(e)
	}
	intentID, e := foundation.ParseID[c.DeliveryIntent](rawID)
	if e != nil {
		t.Fatal(e)
	}
	catalog := event.NewCatalog()
	typ, e := c.DefineDeliveryRequested(catalog)
	if e != nil {
		t.Fatal(e)
	}
	evt, e := event.NewEvent(typ, h, c.DeliveryRequested{IntentID: intentID, Kind: c.InvitationDelivery})
	if e != nil {
		t.Fatal(e)
	}
	summary := evt.Summary()
	plan, e := f.authority.DiscoverAppend(ctxFor(t), a, summary)
	if e != nil {
		t.Fatal(e)
	}
	// Controlled mutation of this test's own dependency fact exercises the
	// formal discover/validate boundary. It grants no production bypass.
	if _, e = f.store.Exec(ctxFor(t), `UPDATE agenteam_account.commands SET resource_id=$2 WHERE id=$1`, rawID, id[c.Invitation](t).String()); e != nil {
		t.Fatal(e)
	}
	result := f.store.WithinTx(ctxFor(t), cause(t), func(ctx context.Context, tx foundation.Tx) error {
		if e := f.store.AcquireAll(ctx, tx, plan.Locks()); e != nil {
			return e
		}
		return f.authority.ValidateAppendInTx(ctx, tx, a, summary, plan, oc.CurrentAccess)
	})
	if result.State() != foundation.NotCommitted || !hasCode(result.Fault(), foundation.ResourceBusy) {
		t.Fatal("stale dependency authorized", result.Fault())
	}
	if _, e = f.store.Exec(ctxFor(t), `UPDATE agenteam_account.commands SET resource_id=$2 WHERE id=$1`, rawID, v.ID.String()); e != nil {
		t.Fatal(e)
	}
	if _, e = f.service.CreateInvitation(ctxFor(t), r); e != nil {
		t.Fatal("unchanged authorized receipt", e)
	}
}
