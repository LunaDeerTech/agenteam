//go:build integration

package accountmail_test

import (
	"bytes"
	"context"
	"os"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/account"
	c "github.com/LunaDeerTech/agenteam/internal/central/account/contract"
	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/outbox"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	smtpfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/smtp"
)

// The existing mail fixture predates the HTTP facade. Bind its real Challenge
// authority here without changing that fixture or bypassing facade validation.
func invitationMailFacade(t *testing.T, f *fixture) *account.SystemHTTPFacade {
	t.Helper()
	_, ring, _ := keys(t)
	catalog := event.NewCatalog()
	revoked, e := c.DefineSessionsRevoked(catalog)
	if e != nil {
		t.Fatal(e)
	}
	delivery, e := c.DefineDeliveryRequested(catalog)
	if e != nil {
		t.Fatal(e)
	}
	events, e := outbox.New(f.store, catalog, outbox.Authorizations{Producers: map[event.StableName]oc.ProducerAuthority{c.AccountProducer: f.authority}, Sessions: f.authority, System: f.authority, Audit: f.audit, Cursors: ring})
	if e != nil {
		t.Fatal(e)
	}
	challenges, e := account.NewChallenges(f.authority, f.process.CurrentProcess())
	if e != nil {
		t.Fatal(e)
	}
	service, e := account.New(account.Dependencies{Authority: f.authority, Audit: f.audit, Secrets: f.secrets, Events: events, SessionsRevoked: revoked, DeliveryRequested: delivery, Processes: f.process, RecoveryLog: f.sink, Challenges: challenges})
	if e != nil {
		t.Fatal(e)
	}
	f.service = service
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if e := service.Force(ctx); e != nil && !service.Joined() {
			t.Error(e)
		}
	})
	facade, e := account.NewSystemHTTPFacade(service, ring)
	if e != nil {
		t.Fatal(e)
	}
	return facade
}
func TestAccountMailInvitationDirectoryChannelsAndOutcomes(t *testing.T) {
	network := startSMTP(t)
	f := newAccount(t)
	facade := invitationMailFacade(t, f)
	success, e := network.Create(ctxFor(t), smtpfixture.Scenario{Mode: "none"})
	if e != nil {
		t.Fatal(e)
	}
	m := composeMail(t, f, network, uint16(success.Port))
	create := func(email string) c.InvitationReceipt {
		t.Helper()
		q, e := c.NewInvitationCreate(c.InvitationCreateFields{Actor: f.admin, Key: foundation.IdempotencyKey(id[struct{}](t).String()), Email: email})
		if e != nil {
			t.Fatal(e)
		}
		v, e := f.service.CreateInvitation(ctxFor(t), q)
		if e != nil {
			t.Fatal(e)
		}
		return v
	}
	read := func(inv c.InvitationID, job c.JobID, phase, channel, result string, attempts int64) {
		t.Helper()
		page, e := facade.ListInvitations(ctxFor(t), f.admin, account.HTTPListRequest{Limit: 100})
		if e != nil {
			t.Fatal(e)
		}
		for _, item := range page.Items {
			if item.ID != inv {
				continue
			}
			d := item.LatestDelivery
			if d.Status.JobID != job || d.Status.Phase != phase || int64(d.Status.Attempts) != attempts {
				t.Fatal("latest attempt status", d.Status, phase, attempts)
			}
			if channel == "" {
				if d.Channel != nil {
					t.Fatal("configuration guessed channel")
				}
			} else if d.Channel == nil || *d.Channel != channel {
				t.Fatal("actual channel", d.Channel, channel)
			}
			if result == "" {
				if d.AttemptResult != nil {
					t.Fatal("invented current outcome")
				}
			} else if d.AttemptResult == nil || string(*d.AttemptResult) != result {
				t.Fatal("actual outcome", d.AttemptResult, result)
			}
			return
		}
		t.Fatal("invitation missing")
	}
	enqueue := func() {
		t.Helper()
		if _, e := f.service.ReconcileDeliveryIntents(ctxFor(t)); e != nil {
			t.Fatal(e)
		}
	}
	logged := create("directory-log@example.test")
	read(logged.ID, logged.JobID, "enqueue_pending", "", "", 0)
	enqueue()
	read(logged.ID, logged.JobID, "pending", "", "", 0)
	if e = m.worker.RunJob(ctxFor(t), logged.JobID); e != nil {
		t.Fatal("actual recovery-log delivery", e)
	}
	read(logged.ID, logged.JobID, "sent", "backend_log", "sent", 1)
	raw, e := os.ReadFile(f.log)
	if e != nil {
		t.Fatal(e)
	}
	lines := bytes.Count(raw, []byte("\n"))
	clear(raw)
	if lines != 2 {
		t.Fatal("bootstrap and actual log message", lines)
	}
	configure(t, f, success, "none", false)
	read(logged.ID, logged.JobID, "sent", "backend_log", "sent", 1)
	smtp := create("directory-smtp@example.test")
	read(smtp.ID, smtp.JobID, "enqueue_pending", "", "", 0)
	enqueue()
	read(smtp.ID, smtp.JobID, "pending", "", "", 0)
	if e = m.worker.RunJob(ctxFor(t), smtp.JobID); e != nil {
		t.Fatal(e)
	}
	read(smtp.ID, smtp.JobID, "sent", "smtp", "sent", 1)
	for _, tc := range []struct {
		name                string
		scenario            smtpfixture.Scenario
		mode, phase, result string
	}{
		{"failure", smtpfixture.Scenario{Mode: "none", MailCode: 550}, "none", "failed", "failed"},
		{"unknown", smtpfixture.Scenario{Mode: "none", CloseStage: "accepted"}, "none", "retry_wait", "unknown"},
		{"configuration", smtpfixture.Scenario{Mode: "starttls", NoSTARTTLS: true}, "starttls", "failed", "failed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			endpoint, e := network.Create(ctxFor(t), tc.scenario)
			if e != nil {
				t.Fatal(e)
			}
			configure(t, f, endpoint, tc.mode, false)
			composition := composeMail(t, f, network, uint16(endpoint.Port))
			invite := create("directory-" + tc.name + "@example.test")
			enqueue()
			if e = composition.worker.RunJob(ctxFor(t), invite.JobID); e == nil {
				t.Fatal("controlled failure absent")
			}
			read(invite.ID, invite.JobID, tc.phase, "smtp", tc.result, 1)
			if tc.name == "failure" {
				q := retryRequest(t, f, invite.JobID)
				first, e := f.service.RetryMailJob(ctxFor(t), q)
				if e != nil {
					t.Fatal(e)
				}
				read(invite.ID, first.JobID, "enqueue_pending", "", "", 0)
				again, e := f.service.RetryMailJob(ctxFor(t), q)
				if e != nil || again.JobID != first.JobID {
					t.Fatal("same-key changed accepted retry", e)
				}
				enqueue()
				if e = composition.worker.RunJob(ctxFor(t), first.JobID); e == nil {
					t.Fatal("retry actual failure absent")
				}
				read(invite.ID, first.JobID, "failed", "smtp", "failed", 1)
				second, e := f.service.RetryMailJob(ctxFor(t), retryRequest(t, f, first.JobID))
				if e != nil {
					t.Fatal(e)
				}
				read(invite.ID, second.JobID, "enqueue_pending", "", "", 0)
			}
			if tc.name == "unknown" {
				paused, e := network.Create(ctxFor(t), smtpfixture.Scenario{Mode: "none", PauseStage: "greeting"})
				if e != nil {
					t.Fatal(e)
				}
				configure(t, f, paused, "none", false)
				next := composeMail(t, f, network, uint16(paused.Port))
				if _, e = f.store.Exec(ctxFor(t), `UPDATE agenteam_account.mail_jobs SET next_at=clock_timestamp() WHERE id=$1`, invite.JobID.String()); e != nil {
					t.Fatal(e)
				}
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				done := make(chan error, 1)
				go func() { done <- next.worker.RunJob(ctx, invite.JobID) }()
				defer func() {
					cancel()
					select {
					case <-done:
					case <-time.After(time.Second):
						t.Error("owned worker did not join")
					}
				}()
				waitSMTPPhase(t, network, paused, "greeting")
				page, e := facade.ListInvitations(ctxFor(t), f.admin, account.HTTPListRequest{Limit: 100})
				if e != nil {
					t.Fatal(e)
				}
				found := false
				for _, item := range page.Items {
					if item.ID == invite.ID {
						d := item.LatestDelivery
						found = true
						if d.Status.Attempts != 2 || d.Channel == nil || *d.Channel != "smtp" || d.AttemptResult != nil || d.Status.Phase != "sending" && d.Status.Phase != "claimed" {
							t.Fatal("next claim reused previous attempt outcome", d)
						}
					}
				}
				if !found {
					t.Fatal("next claim row missing")
				}
				if e = network.Release(ctxFor(t), paused.ID); e != nil {
					t.Fatal(e)
				}
				select {
				case e = <-done:
					done <- e
					if e != nil {
						t.Fatal(e)
					}
				case <-ctx.Done():
					t.Fatal("next actual send did not complete")
				}
				read(invite.ID, invite.JobID, "sent", "smtp", "sent", 2)
				var previous string
				if e = f.store.QueryRow(ctxFor(t), `SELECT result FROM agenteam_account.mail_attempts WHERE job_id=$1 ORDER BY fence LIMIT 1`, invite.JobID.String()).Scan(&previous); e != nil || previous != "unknown" {
					t.Fatal("unknown attempt history changed", e)
				}
			}
		})
	}
	raw, e = os.ReadFile(f.log)
	if e != nil {
		t.Fatal(e)
	}
	defer clear(raw)
	if bytes.Count(raw, []byte("\n")) != lines {
		t.Fatal("configured delivery fell back to log")
	}
}
