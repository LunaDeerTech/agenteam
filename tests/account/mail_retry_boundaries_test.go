//go:build integration

package account_test

import (
	"context"
	"net"
	"net/url"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/account"
	c "github.com/LunaDeerTech/agenteam/internal/central/account/contract"
	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
)

type retryCommitStore struct {
	*postgres.Store
	proxy          *commitProxy
	enabled, fired atomic.Bool
	phase          string
}

func (w *retryCommitStore) WithinTx(ctx context.Context, cause foundation.TransactionCause, fn func(context.Context, foundation.Tx) error) foundation.CommitResult {
	return w.Store.WithinTx(ctx, cause, func(ctx context.Context, tx foundation.Tx) error {
		if e := fn(ctx, tx); e != nil {
			return e
		}
		if !w.enabled.Load() || w.fired.Load() {
			return nil
		}
		x, e := w.InTx(tx)
		if e != nil {
			return e
		}
		var found bool
		if e = x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_account.commands WHERE command_name='mail-retry' AND phase=$1)`, w.phase).Scan(&found); e != nil {
			return e
		}
		if found && w.fired.CompareAndSwap(false, true) {
			w.proxy.armed.Store(true)
		}
		return nil
	})
}
func finishedMailForRetry(t *testing.T, f *b02Fixture) c.MailJobRetry {
	t.Helper()
	job := prepareMailJob(t, f)
	port, runtime := newManualMail(t, f)
	a, e := port.ClaimDelivery(ctxFor(t), job)
	if e != nil {
		t.Fatal(e, safeFailure(e))
	}
	runtime.accept(a)
	// This path has no socket/material work: the formal Runtime proves that
	// exact zero-I/O attempt joined; it does not claim a delivered message.
	done := runtime.finish(t, a, c.DeliveryMaterials{})
	if e = port.FinishDelivery(ctxFor(t), a, done); e != nil {
		t.Fatal(e, safeFailure(e))
	}
	var user, session string
	if e = f.store.QueryRow(ctxFor(t), `SELECT user_id::text,id::text FROM agenteam_account.sessions WHERE revoked_at IS NULL ORDER BY id LIMIT 1`).Scan(&user, &session); e != nil {
		t.Fatal(e)
	}
	u, _ := foundation.ParseID[identity.User](user)
	sid, _ := foundation.ParseID[identity.Session](session)
	actor, e := identity.NewHuman(u, sid)
	if e != nil {
		t.Fatal(e)
	}
	status, e := f.service.GetMailJob(ctxFor(t), actor, job)
	if e != nil {
		t.Fatal(e)
	}
	return c.MailJobRetry{Actor: actor, Key: foundation.IdempotencyKey(id[struct{}](t).String()), JobID: job, ExpectedVersion: status.Version}
}
func retryCounts(t *testing.T, f *b02Fixture, q c.MailJobRetry, accepted int) {
	t.Helper()
	var intents, events, audits, attempts, committed int
	var version int64
	e := f.store.QueryRow(ctxFor(t), `SELECT (SELECT count(*) FROM agenteam_account.delivery_intents),(SELECT count(*) FROM agenteam_outbox.events WHERE event_type='account.delivery-requested'),(SELECT count(*) FROM agenteam_audit.audit_records WHERE action='smtp.delivery.retry'),(SELECT count(*) FROM agenteam_account.mail_attempts),(SELECT count(*) FROM agenteam_account.commands WHERE command_name='mail-retry' AND phase='committed'),version FROM agenteam_account.mail_jobs WHERE id=$1`, q.JobID.String()).Scan(&intents, &events, &audits, &attempts, &committed, &version)
	if e != nil || intents != 1+accepted || events != 1+accepted || audits != accepted || attempts != 1 || committed != accepted || version != int64(q.ExpectedVersion)+int64(accepted) {
		t.Fatal("atomic retry boundary", intents, events, audits, attempts, committed, version, e)
	}
}
func TestAccountMailRetryUnknownKeepsWriterAndCanonicalIdentity(t *testing.T) {
	for _, phase := range []string{"planned", "committed"} {
		for _, commit := range []bool{true, false} {
			name := phase + "/late_commit"
			if !commit {
				name = phase + "/rollback"
			}
			t.Run(name, func(t *testing.T) {
				db, _, _ := database(t)
				proxy := newCommitProxy(t, net.JoinHostPort("127.0.0.1", db.Fixture.Port), commit)
				u, e := url.Parse(db.Fixture.URL(db.Name))
				if e != nil {
					t.Fatal(e)
				}
				u.Host = proxy.listener.Addr().String()
				raw := openStore(t, db.Config(t, map[string]string{"URL": u.String(), "TLS_MODE": "disable"}))
				w := &retryCommitStore{Store: raw, proxy: proxy, phase: phase}
				k, _, _ := keys(t)
				auth, e := account.NewAuthority(w, k)
				if e != nil {
					t.Fatal(e)
				}
				f := assembleB02(t, db, raw, w, auth, nil)
				q := finishedMailForRetry(t, f)
				w.enabled.Store(true)
				ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
				defer cancel()
				type result struct {
					status c.MailJobStatus
					err    error
				}
				done := make(chan result, 1)
				go func() { v, e := f.service.RetryMailJob(ctx, q); done <- result{v, e} }()
				await(t, proxy.reached)
				select {
				case v := <-done:
					assertUnconfirmedState(t, ctx, v.err)
					if v.status.JobID.Validate() == nil {
						t.Fatal("unknown returned new success")
					}
				case <-time.After(4 * time.Second):
					t.Fatal("retry did not return bounded unknown")
				}
				if !w.fired.Load() {
					t.Fatal("COMMIT not intercepted")
				}
				retryCounts(t, f, q, 0)
				// A competing current command cannot bypass the original source/root
				// writer while its outcome is not yet known.
				other := q
				other.Key = foundation.IdempotencyKey(id[struct{}](t).String())
				if _, e = f.service.RetryMailJob(ctxFor(t), other); e == nil {
					t.Fatal("bypassed original writer")
				}
				close(proxy.release)
				await(t, proxy.completed)
				got, e := f.service.RetryMailJob(ctxFor(t), q)
				if e != nil {
					t.Fatal("same-key canonical continuation", safeFailure(e))
				}
				if got.Attempts != 0 || got.Phase != "enqueue_pending" {
					t.Fatal("new cycle falsely attempted", got)
				}
				if again, e := f.service.RetryMailJob(ctxFor(t), q); e != nil || again != got {
					t.Fatal("duplicate retry", e)
				}
				retryCounts(t, f, q, 1)
			})
		}
	}
}

type retryAppendFailure struct {
	oc.Appender
	enabled atomic.Bool
}

func (a *retryAppendFailure) AppendEventInTx(ctx context.Context, tx foundation.Tx, actor identity.Actor, e event.Event, plan oc.AppendPlan) (oc.AppendReceipt, error) {
	if a.enabled.Load() {
		return oc.AppendReceipt{}, foundation.NewFault(foundation.DependencyUnavailable, foundation.NotStarted)
	}
	return a.Appender.AppendEventInTx(ctx, tx, actor, e, plan)
}
func TestAccountMailRetryAuditAndOutboxFailureRollbackWholeCycle(t *testing.T) {
	for _, mode := range []string{"audit", "outbox"} {
		t.Run(mode, func(t *testing.T) {
			db, raw, auth := database(t)
			var wrapper *retryAppendFailure
			f := assembleB02(t, db, raw, raw, auth, func(p oc.Appender) oc.Appender { wrapper = &retryAppendFailure{Appender: p}; return wrapper })
			q := finishedMailForRetry(t, f)
			control := db.Connect(t)
			if mode == "audit" {
				if _, e := control.Exec(ctxFor(t), `ALTER TABLE agenteam_audit.audit_records ADD CONSTRAINT fixture_reject_mail_retry CHECK(action<>'smtp.delivery.retry')`); e != nil {
					t.Fatal(e)
				}
			} else {
				wrapper.enabled.Store(true)
			}
			if _, e := f.service.RetryMailJob(ctxFor(t), q); e == nil {
				t.Fatal("injected actual transaction failure not observed")
			}
			retryCounts(t, f, q, 0)
			if mode == "audit" {
				if _, e := control.Exec(ctxFor(t), `ALTER TABLE agenteam_audit.audit_records DROP CONSTRAINT fixture_reject_mail_retry`); e != nil {
					t.Fatal(e)
				}
			} else {
				wrapper.enabled.Store(false)
			}
			if _, e := f.service.RetryMailJob(ctxFor(t), q); e != nil {
				t.Fatal("same original plan could not resume", safeFailure(e))
			}
			retryCounts(t, f, q, 1)
			if e := f.service.Logout(ctxFor(t), account.LogoutRequest{Actor: q.Actor, Key: foundation.IdempotencyKey(id[struct{}](t).String())}); e != nil {
				t.Fatal(e)
			}
			if _, e := f.service.RetryMailJob(ctxFor(t), q); !hasCode(e, foundation.SessionRevoked) {
				t.Fatal("revoked actor got receipt", safeFailure(e))
			}
		})
	}
}
