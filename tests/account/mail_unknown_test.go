//go:build integration

package account_test

import (
	"context"
	"errors"
	"net"
	"net/url"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/account"
	c "github.com/LunaDeerTech/agenteam/internal/central/account/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
)

type mailCommitStore struct {
	*postgres.Store
	proxy          *commitProxy
	enabled, fired atomic.Bool
	phase          string
}

func (w *mailCommitStore) WithinTx(ctx context.Context, cause foundation.TransactionCause, fn func(context.Context, foundation.Tx) error) foundation.CommitResult {
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
		query := map[string]string{
			"claim":      `SELECT EXISTS(SELECT 1 FROM agenteam_account.mail_attempts WHERE phase='negotiation')`,
			"acquire":    `SELECT EXISTS(SELECT 1 FROM agenteam_secret.secret_leases WHERE owner_kind='account_delivery_attempt' AND NOT released)`,
			"begin":      `SELECT EXISTS(SELECT 1 FROM agenteam_account.mail_attempts WHERE phase='auth')`,
			"checkpoint": `SELECT EXISTS(SELECT 1 FROM agenteam_account.mail_attempts WHERE phase='data')`,
			"stop":       `SELECT EXISTS(SELECT 1 FROM agenteam_account.mail_attempts WHERE io_joined AND NOT terminal)`,
			"finish":     `SELECT EXISTS(SELECT 1 FROM agenteam_account.mail_attempts WHERE terminal)`,
		}[w.phase]
		var found bool
		if e = x.QueryRow(ctx, query).Scan(&found); e != nil {
			return e
		}
		if found && w.fired.CompareAndSwap(false, true) {
			w.proxy.armed.Store(true)
		}
		return nil
	})
}
func newMailProxy(t *testing.T, phase string, commit bool) (*b02Fixture, *mailCommitStore, *commitProxy) {
	t.Helper()
	db, _, _ := database(t)
	p := newCommitProxy(t, net.JoinHostPort("127.0.0.1", db.Fixture.Port), commit)
	u, e := url.Parse(db.Fixture.URL(db.Name))
	if e != nil {
		t.Fatal(e)
	}
	u.Host = p.listener.Addr().String()
	raw := openStore(t, db.Config(t, map[string]string{"URL": u.String(), "TLS_MODE": "disable"}))
	w := &mailCommitStore{Store: raw, proxy: p, phase: phase}
	key, _, _ := keys(t)
	authority, e := account.NewAuthority(w, key)
	if e != nil {
		t.Fatal(e)
	}
	return assembleB02(t, db, raw, w, authority, nil), w, p
}
func prepareMailJob(t *testing.T, f *b02Fixture) c.JobID {
	t.Helper()
	admin := b02Admin(t, f)
	settings, e := f.service.GetSMTPSettings(ctxFor(t), admin)
	if e != nil {
		t.Fatal(e)
	}
	password := testPassword(t, "owned unknown-boundary SMTP credential")
	req, e := c.NewSMTPUpdate(c.SMTPUpdateFields{Actor: admin, Key: foundation.IdempotencyKey(id[struct{}](t).String()), ExpectedVersion: settings.Version, Configured: true, Host: "mail.example.test", Port: 2525, TLSMode: "none", Username: "fixture", SenderEmail: "sender@example.test", RetryCount: 3, RetryIntervalSeconds: 10, Password: &password})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = f.service.UpdateSMTPSettings(ctxFor(t), req); e != nil {
		t.Fatal(e, safeFailure(e))
	}
	job, e := f.service.TestSMTP(ctxFor(t), c.SMTPTest{Actor: admin, Key: foundation.IdempotencyKey(id[struct{}](t).String()), Recipient: "recipient@example.test"})
	if e != nil {
		t.Fatal(e, safeFailure(e))
	}
	if _, e = f.service.ReconcileDeliveryIntents(ctxFor(t)); e != nil {
		t.Fatal(e)
	}
	return job.JobID
}
func TestAccountMailUnknownTwoTransactionsAndFinalWriters(t *testing.T) {
	for _, phase := range []string{"claim", "acquire", "begin", "checkpoint", "stop", "finish"} {
		for _, commit := range []bool{true, false} {
			name := phase + "/late_commit"
			if !commit {
				name = phase + "/rollback"
			}
			t.Run(name, func(t *testing.T) {
				f, w, proxy := newMailProxy(t, phase, commit)
				job := prepareMailJob(t, f)
				port, runtime := newManualMail(t, f)
				var attempt c.DeliveryAttempt
				var material c.DeliveryMaterials
				var completion c.DeliveryCompletion
				var e error
				if phase != "claim" {
					attempt, e = port.ClaimDelivery(ctxFor(t), job)
					if e != nil {
						t.Fatal(e)
					}
					runtime.accept(attempt)
				}
				if phase != "claim" && phase != "acquire" {
					material, e = port.PrepareDelivery(ctxFor(t), attempt)
					if e != nil {
						t.Fatal(e, safeFailure(e))
					}
				}
				if phase == "checkpoint" {
					permit, e := port.BeginDelivery(ctxFor(t), attempt, c.SMTPMail)
					if e != nil {
						t.Fatal(e)
					}
					permit.Close()
				}
				if phase == "stop" || phase == "finish" {
					completion = runtime.finish(t, attempt, material)
				}
				w.enabled.Store(true)
				ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
				defer cancel()
				done := make(chan error, 1)
				go func() {
					var err error
					switch phase {
					case "claim":
						var a c.DeliveryAttempt
						a, err = port.ClaimDelivery(ctx, job)
						if a.Validate() == nil {
							err = foundation.NewFault(foundation.InternalError, foundation.NotStarted)
						}
					case "acquire":
						var m c.DeliveryMaterials
						m, err = port.PrepareDelivery(ctx, attempt)
						exposed := false
						_ = m.Use(func(c.DeliveryMaterialFields) error { exposed = true; return nil })
						if exposed {
							err = foundation.NewFault(foundation.InternalError, foundation.NotStarted)
						}
						m.Destroy()
					case "begin":
						var p c.SendPermit
						p, err = port.BeginDelivery(ctx, attempt, c.SMTPAuth)
						if _, useErr := p.RunSMTPFirstWrite(func(context.Context) (int, error) { return 1, nil }); useErr == nil {
							err = foundation.NewFault(foundation.InternalError, foundation.NotStarted)
						}
						p.Close()
					case "checkpoint":
						err = port.CheckpointDelivery(ctx, attempt, c.Data)
					default:
						err = port.FinishDelivery(ctx, attempt, completion)
					}
					done <- err
				}()
				await(t, proxy.reached)
				select {
				case e = <-done:
				case <-time.After(4 * time.Second):
					t.Fatal("held writer did not return")
				}
				if phase == "begin" || phase == "checkpoint" {
					var f *foundation.Fault
					if !errors.As(e, &f) || f.Code != foundation.CommitUnknown || f.CommitState != foundation.Unknown || ctx.Err() != nil {
						t.Fatal("checkpoint must preserve unknown without continuing protocol", safeFailure(e))
					}
					// These operations deliberately return Unknown immediately. The worker
					// stops protocol and only its joined Finish waits on the original writer.
					completion = runtime.finish(t, attempt, material)
					if e = port.FinishDelivery(ctxFor(t), attempt, completion); e == nil {
						t.Fatal("joined finish bypassed the held original writer")
					}
				} else {
					assertUnconfirmedState(t, ctx, e)
				}
				if !w.fired.Load() {
					t.Fatal("COMMIT was not actually intercepted")
				}
				if phase == "claim" {
					_, e = port.RecoverDeliveries(ctxFor(t))
					if !hasCode(e, foundation.CommitUnknown) {
						t.Fatal("unhanded recovery lost original unknown", safeFailure(e))
					}
				}
				var terminal int
				if e = f.store.QueryRow(ctxFor(t), `SELECT count(*) FROM agenteam_account.mail_attempts WHERE terminal`).Scan(&terminal); e != nil || terminal != 0 {
					t.Fatal("unconfirmed writer became terminal", terminal, e)
				}
				close(proxy.release)
				await(t, proxy.completed)
				if phase == "claim" {
					for range 2 {
						if _, e = port.RecoverDeliveries(ctxFor(t)); e != nil {
							t.Fatal("unhanded actual join recovery", safeFailure(e))
						}
					}
				} else {
					if phase != "stop" && phase != "finish" && phase != "begin" && phase != "checkpoint" {
						completion = runtime.finish(t, attempt, material)
					}
					if e = port.FinishDelivery(ctxFor(t), attempt, completion); e != nil {
						t.Fatal("release after canonical writer", safeFailure(e))
					}
				}
				var attempts, active, audits int
				e = f.store.QueryRow(ctxFor(t), `SELECT (SELECT count(*) FROM agenteam_account.mail_attempts),(SELECT count(*) FROM agenteam_secret.secret_leases WHERE owner_kind='account_delivery_attempt' AND NOT released),(SELECT count(*) FROM agenteam_audit.audit_records WHERE action='smtp.delivery'),(SELECT count(*) FROM agenteam_account.mail_attempts WHERE terminal)`).Scan(&attempts, &active, &audits, &terminal)
				want := 1
				if phase == "claim" && !commit {
					want = 0
				}
				if e != nil || attempts != want || active != 0 || terminal != want || audits != want {
					t.Fatal("canonical terminal facts", attempts, active, audits, terminal, e)
				}
			})
		}
	}
}
