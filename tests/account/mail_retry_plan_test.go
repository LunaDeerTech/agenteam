//go:build integration

package account_test

import (
	"context"
	"testing"

	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
)

type retryPlanCapture struct {
	oc.Appender
	enabled bool
	actor   identity.Actor
	event   event.Event
	plan    oc.AppendPlan
}

func (p *retryPlanCapture) PrepareAppend(ctx context.Context, a identity.Actor, e event.Event) (oc.AppendPlan, error) {
	plan, err := p.Appender.PrepareAppend(ctx, a, e)
	if p.enabled && err == nil {
		p.actor, p.event, p.plan = a, e, plan
	}
	return plan, err
}
func TestAccountMailRetryFormalPlansRequireCompleteLocksAndExactFacts(t *testing.T) {
	db, store, authority := database(t)
	var capture *retryPlanCapture
	f := assembleB02(t, db, store, store, authority, func(next oc.Appender) oc.Appender {
		capture = &retryPlanCapture{Appender: next}
		return capture
	})
	q := finishedMailForRetry(t, f)
	capture.enabled = true
	accepted, e := f.service.RetryMailJob(ctxFor(t), q)
	if e != nil || capture.event.Validate() != nil {
		t.Fatal("real retry plan", e)
	}
	locks := capture.plan.Locks()
	if len(locks) < 5 {
		t.Fatal("root/current authority lock plan missing")
	}
	check := func(held []foundation.LockRequest, fn func(context.Context, foundation.Tx) error) foundation.CommitResult {
		return store.WithinTx(ctxFor(t), cause(t), func(ctx context.Context, tx foundation.Tx) error {
			if e := store.AcquireAll(ctx, tx, held); e != nil {
				return e
			}
			return fn(ctx, tx)
		})
	}
	appendAgain := func(ctx context.Context, tx foundation.Tx) error {
		r, e := f.events.AppendEventInTx(ctx, tx, capture.actor, capture.event, capture.plan)
		if e == nil && r.EventID != capture.event.Header().EventID {
			t.Error("canonical receipt changed")
		}
		return e
	}
	if r := check(locks, appendAgain); r.State() != foundation.Committed {
		t.Fatal("full initial union not composable", r.Fault())
	}
	// Every exact dependency, including low-order command/User/gate locks,
	// must already be held. The appender may neither add nor upgrade one.
	for i := range locks {
		partial := append([]foundation.LockRequest(nil), locks[:i]...)
		partial = append(partial, locks[i+1:]...)
		if r := check(partial, appendAgain); r.State() != foundation.NotCommitted {
			t.Fatal("missing lock accepted", i, r.State())
		}
		if locks[i].Mode == foundation.Exclusive {
			weak := append([]foundation.LockRequest(nil), locks...)
			weak[i].Mode = foundation.Shared
			if r := check(weak, appendAgain); r.State() != foundation.NotCommitted {
				t.Fatal("weak lock upgraded", i, r.State())
			}
		}
	}
	for _, field := range []string{"event", "aggregate", "payload", "producer"} {
		summary := capture.event.Summary()
		switch field {
		case "event":
			summary.Header.EventID = id[event.EventIdentity](t)
		case "aggregate":
			summary.Header.AggregateID = id[event.Aggregate](t)
		case "payload":
			summary.PayloadDigest = foundation.Digest("sha256:0000000000000000000000000000000000000000000000000000000000000000")
		case "producer":
			summary.Producer = "not-account"
		}
		if _, e = authority.DiscoverAppend(ctxFor(t), capture.actor, summary); e == nil {
			t.Fatal("substituted event accepted", field)
		}
	}
	cmdID := capture.event.Header().AggregateID.String()
	validFields := ac.AccountMetadataFields{JobID: q.JobID.String(), InitiatorID: q.Actor.Details().UserID, Version: q.ExpectedVersion, Phase: ac.AccountAccepted}
	for _, change := range []string{"valid", "cause", "ordinal", "initiator", "version", "new_job"} {
		fields := validFields
		job, causeID, ordinal := q.JobID.String(), cmdID, int64(0)
		switch change {
		case "cause":
			causeID = id[struct{}](t).String()
		case "ordinal":
			ordinal = 1
		case "initiator":
			fields.InitiatorID = id[identity.User](t).String()
		case "version":
			fields.Version++
		case "new_job":
			job, fields.JobID = accepted.JobID.String(), accepted.JobID.String()
		}
		metadata, err := ac.AccountMetadata(ac.SMTPDeliveryRetry, fields)
		if err != nil {
			t.Fatal(err)
		}
		resource, _ := ac.NewResource(ac.MailJobResource, job)
		entry, err := ac.NewEntry(ac.EntryFields{Scope: identity.SystemScope(), Actor: q.Actor, Action: ac.SMTPDeliveryRetry, Outcome: ac.Success, Resource: resource, Metadata: metadata})
		if err != nil {
			t.Fatal(err)
		}
		key, err := ac.NewAppendKey(ac.AccountProducer, causeID, ordinal)
		if err != nil {
			t.Fatal(err)
		}
		r := check(locks, func(ctx context.Context, tx foundation.Tx) error {
			return authority.CheckAppendInTx(ctx, tx, entry, key)
		})
		if (r.State() == foundation.Committed) != (change == "valid") {
			t.Fatal("typed Audit did not bind exact retry fact", change, r.State(), r.Fault())
		}
	}
	var root string
	if e = store.QueryRow(ctxFor(t), `SELECT origin_intent_id::text FROM agenteam_account.delivery_intents WHERE id=$1`, cmdID).Scan(&root); e != nil {
		t.Fatal(e)
	}
	// A durable mapping changed after discovery: the old complete lock plan
	// still cannot authorize substituted root provenance. Restore fixture data
	// afterwards, without adding locks or silently rebuilding the old plan.
	if _, e = store.Exec(ctxFor(t), `UPDATE agenteam_account.delivery_intents SET initiator_id=$2 WHERE id=$1`, root, id[identity.User](t).String()); e != nil {
		t.Fatal(e)
	}
	if r := check(locks, appendAgain); r.State() != foundation.NotCommitted {
		t.Fatal("old mapping remained authority", r.State())
	}
	if _, e = store.Exec(ctxFor(t), `UPDATE agenteam_account.delivery_intents SET initiator_id=$2 WHERE id=$1`, root, q.Actor.Details().UserID); e != nil {
		t.Fatal(e)
	}
	if r := check(locks, appendAgain); r.State() != foundation.Committed {
		t.Fatal("restored exact canonical history unavailable", r.Fault())
	}
	retryCounts(t, f, q, 1)
}
