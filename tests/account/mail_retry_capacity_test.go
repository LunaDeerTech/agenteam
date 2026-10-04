//go:build integration

package account_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/LunaDeerTech/agenteam/internal/central/account"
	c "github.com/LunaDeerTech/agenteam/internal/central/account/contract"
	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
)

type retryCapacityFacts struct {
	Pending, Intents, Events, Audits, Committed int
}

func readRetryCapacityFacts(t *testing.T, f *b02Fixture) retryCapacityFacts {
	t.Helper()
	var v retryCapacityFacts
	e := f.store.QueryRow(ctxFor(t), `SELECT
	(SELECT count(*) FROM agenteam_account.delivery_intents i LEFT JOIN agenteam_account.mail_jobs j ON j.intent_id=i.id WHERE j.id IS NULL OR j.phase IN ('pending','claimed','sending','retry_wait','processing','unknown')),
	(SELECT count(*) FROM agenteam_account.delivery_intents),
	(SELECT count(*) FROM agenteam_outbox.events WHERE event_type='account.delivery-requested'),
	(SELECT count(*) FROM agenteam_audit.audit_records WHERE action='smtp.delivery.retry'),
	(SELECT count(*) FROM agenteam_account.commands WHERE command_name='mail-retry' AND phase='committed')`).Scan(&v.Pending, &v.Intents, &v.Events, &v.Audits, &v.Committed)
	if e != nil {
		t.Fatal(e)
	}
	return v
}

func fillRetryCapacity(t *testing.T, f *b02Fixture, template c.JobID, total int) {
	t.Helper()
	before := readRetryCapacityFacts(t, f)
	if before.Pending > total {
		t.Fatal("capacity setup already exceeds target")
	}
	// Only the backlog is bulk fixture data: these exact owned rows represent
	// accepted, not-yet-enqueued intents. They are never submitted to the
	// authorizer, dispatcher or SMTP. Both retry roots below are real commands,
	// real claimed attempts and actual zero-I/O completion through formal ports.
	n, e := f.store.Exec(ctxFor(t), `WITH ids AS MATERIALIZED (
	 SELECT gen_random_uuid() AS id,gen_random_uuid() AS job FROM generate_series(1,$1::int)
	) INSERT INTO agenteam_account.delivery_intents(id,origin_intent_id,job_id,kind,link_id,initiator_id,recipient)
	 SELECT ids.id,ids.id,ids.job,i.kind,i.link_id,i.initiator_id,i.recipient
	 FROM ids CROSS JOIN agenteam_account.delivery_intents i WHERE i.job_id=$2`, total-before.Pending, template.String())
	if e != nil || n.RowsAffected() != int64(total-before.Pending) {
		t.Fatal("owned backlog setup", e)
	}
	if got := readRetryCapacityFacts(t, f); got.Pending != total {
		t.Fatal("wrong initial capacity", got.Pending)
	}
}

func anotherFinishedRetryRoot(t *testing.T, f *b02Fixture, actor identity.Actor) c.MailJobRetry {
	t.Helper()
	created, e := f.service.TestSMTP(ctxFor(t), c.SMTPTest{Actor: actor, Key: foundation.IdempotencyKey(id[struct{}](t).String()), Recipient: "second-capacity@example.test"})
	if e != nil {
		t.Fatal(e, safeFailure(e))
	}
	if _, e = f.service.ReconcileDeliveryIntents(ctxFor(t)); e != nil {
		t.Fatal(e)
	}
	p, r := newManualMail(t, f)
	a, e := p.ClaimDelivery(ctxFor(t), created.JobID)
	if e != nil {
		t.Fatal(e, safeFailure(e))
	}
	r.accept(a)
	if e = p.FinishDelivery(ctxFor(t), a, r.finish(t, a, c.DeliveryMaterials{})); e != nil {
		t.Fatal(e, safeFailure(e))
	}
	status, e := f.service.GetMailJob(ctxFor(t), actor, created.JobID)
	if e != nil {
		t.Fatal(e)
	}
	return c.MailJobRetry{Actor: actor, Key: foundation.IdempotencyKey(id[struct{}](t).String()), JobID: created.JobID, ExpectedVersion: status.Version}
}

func requireCapacityRejection(t *testing.T, status c.MailJobStatus, e error) {
	t.Helper()
	var f *foundation.Fault
	if !errors.As(e, &f) || f.Code != foundation.RateLimited || f.CommitState != foundation.NotCommitted || status.JobID.Validate() == nil {
		t.Fatal("capacity must reject the uncommitted new cycle", safeFailure(e))
	}
}

type retryCapacityBarrier struct {
	oc.Appender
	enabled atomic.Bool
	ready   chan struct{}
	release chan struct{}
}

func (b *retryCapacityBarrier) PrepareAppend(ctx context.Context, actor identity.Actor, value event.Event) (oc.AppendPlan, error) {
	plan, e := b.Appender.PrepareAppend(ctx, actor, value)
	if e != nil || !b.enabled.Load() {
		return plan, e
	}
	b.ready <- struct{}{}
	select {
	case <-b.release:
		return plan, nil
	case <-ctx.Done():
		return oc.AppendPlan{}, ctx.Err()
	}
}

func TestAccountMailRetryGlobalCapacityAndHistoricalReceipt(t *testing.T) {
	t.Run("full_rejects_new_but_replays_current_history", func(t *testing.T) {
		f := newB02Account(t)
		first := finishedMailForRetry(t, f)
		second := anotherFinishedRetryRoot(t, f, first.Actor)
		accepted, e := f.service.RetryMailJob(ctxFor(t), first)
		if e != nil {
			t.Fatal(e, safeFailure(e))
		}
		fillRetryCapacity(t, f, second.JobID, 10000)
		before := readRetryCapacityFacts(t, f)
		got, e := f.service.RetryMailJob(ctxFor(t), second)
		requireCapacityRejection(t, got, e)
		if after := readRetryCapacityFacts(t, f); after != before {
			t.Fatal("rejected retry wrote intent/event/Audit/receipt", before, after)
		}
		status, e := f.service.GetMailJob(ctxFor(t), second.Actor, second.JobID)
		if e != nil || status.Version != second.ExpectedVersion {
			t.Fatal("rejected retry changed source version", e)
		}
		if replay, e := f.service.RetryMailJob(ctxFor(t), first); e != nil || replay != accepted {
			t.Fatal("capacity incorrectly hid canonical receipt", safeFailure(e))
		}
		if after := readRetryCapacityFacts(t, f); after != before {
			t.Fatal("history created a second cycle", before, after)
		}
		if e = f.service.Logout(ctxFor(t), account.LogoutRequest{Actor: first.Actor, Key: foundation.IdempotencyKey(id[struct{}](t).String())}); e != nil {
			t.Fatal(e)
		}
		if _, e = f.service.RetryMailJob(ctxFor(t), first); !hasCode(e, foundation.SessionRevoked) {
			t.Fatal("capacity/history bypassed current Session", safeFailure(e))
		}
	})
	t.Run("different_roots_compete_for_last_slot", func(t *testing.T) {
		db, store, authority := database(t)
		var barrier *retryCapacityBarrier
		f := assembleB02(t, db, store, store, authority, func(p oc.Appender) oc.Appender {
			barrier = &retryCapacityBarrier{Appender: p, ready: make(chan struct{}, 2), release: make(chan struct{})}
			return barrier
		})
		first := finishedMailForRetry(t, f)
		second := anotherFinishedRetryRoot(t, f, first.Actor)
		fillRetryCapacity(t, f, first.JobID, 9999)
		before := readRetryCapacityFacts(t, f)
		barrier.enabled.Store(true)
		type result struct {
			request c.MailJobRetry
			status  c.MailJobStatus
			err     error
		}
		results := make(chan result, 2)
		ctx := ctxFor(t)
		for _, q := range []c.MailJobRetry{first, second} {
			go func() {
				v, e := f.service.RetryMailJob(ctx, q)
				results <- result{q, v, e}
			}()
		}
		// Both different roots have committed their plan and passed the real
		// appender's discovery before either can start the final writer Tx.
		for range 2 {
			select {
			case <-barrier.ready:
			case <-ctx.Done():
				close(barrier.release)
				t.Fatal("both formal plans did not reach the barrier", ctx.Err())
			}
		}
		close(barrier.release)
		barrier.enabled.Store(false)
		successes, limited := 0, 0
		for range 2 {
			var v result
			select {
			case v = <-results:
			case <-ctx.Done():
				t.Fatal("concurrent retries did not join", ctx.Err())
			}
			status, e := f.service.GetMailJob(ctxFor(t), v.request.Actor, v.request.JobID)
			if e != nil {
				t.Fatal(e)
			}
			wantVersion := v.request.ExpectedVersion
			if v.err == nil {
				successes++
				wantVersion++
				if replay, e := f.service.RetryMailJob(ctxFor(t), v.request); e != nil || replay != v.status {
					t.Fatal("last-slot winner cannot replay at capacity", safeFailure(e))
				}
			} else {
				limited++
				requireCapacityRejection(t, v.status, v.err)
			}
			if status.Version != wantVersion {
				t.Fatal("wrong source version after capacity competition", status.Version, wantVersion)
			}
		}
		if successes != 1 || limited != 1 {
			t.Fatal("global capacity was not serialized", successes, limited)
		}
		want := retryCapacityFacts{Pending: 10000, Intents: before.Intents + 1, Events: before.Events + 1, Audits: before.Audits + 1, Committed: before.Committed + 1}
		if got := readRetryCapacityFacts(t, f); got != want {
			t.Fatal("one remaining slot did not create exactly one atomic cycle", got, want)
		}
	})
}
