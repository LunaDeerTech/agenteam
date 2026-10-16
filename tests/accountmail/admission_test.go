//go:build integration

package accountmail_test

import (
	"context"
	"testing"
	"time"

	c "github.com/LunaDeerTech/agenteam/internal/central/account/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
	smtpfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/smtp"
)

func unconfigure(t *testing.T, f *fixture) {
	t.Helper()
	cfg, e := f.service.GetSMTPSettings(ctxFor(t), f.admin)
	if e != nil {
		t.Fatal(e)
	}
	r, e := c.NewSMTPUpdate(c.SMTPUpdateFields{Actor: f.admin, Key: foundation.IdempotencyKey(id[struct{}](t).String()), ExpectedVersion: cfg.Version, Configured: false, CredentialAction: "remove", RetryCount: int64(cfg.RetryCount), RetryIntervalSeconds: int64(cfg.RetryIntervalSeconds)})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = f.service.UpdateSMTPSettings(ctxFor(t), r); e != nil {
		t.Fatal("committed config invalidation", e)
	}
}

// An accepted AUTH does not authorize a later MAIL after configured credentials
// change. The old credential remains leased until the actual socket owner joins.
func TestAccountMailConfiguredCredentialReplacementBeforeMail(t *testing.T) {
	network := startSMTP(t)
	endpoint, err := network.Create(ctxFor(t), smtpfixture.Scenario{Mode: "starttls", PauseStage: "auth"})
	if err != nil {
		t.Fatal(err)
	}
	f := newAccount(t)
	configure(t, f, endpoint, "starttls", true)
	m := composeMail(t, f, network)
	invite := createMailInvitation(t, f)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- m.worker.RunJob(ctx, invite.JobID) }()
	waitSMTPPhase(t, network, endpoint, "auth")
	cfg, err := f.service.GetSMTPSettings(ctxFor(t), f.admin)
	if err != nil {
		t.Fatal(err)
	}
	password, err := sc.NewSecretMaterial([]byte("owned replacement credential"))
	if err != nil {
		t.Fatal(err)
	}
	defer password.Destroy()
	update, err := c.NewSMTPUpdate(c.SMTPUpdateFields{
		Actor: f.admin, Key: foundation.IdempotencyKey(id[struct{}](t).String()),
		ExpectedVersion: cfg.Version, Configured: true, Host: cfg.Host, Port: cfg.Port,
		TLSMode: cfg.TLSMode, Username: cfg.Username, Password: &password,
		SenderEmail: cfg.SenderEmail, SenderName: cfg.SenderName,
		RetryCount: int64(cfg.RetryCount), RetryIntervalSeconds: int64(cfg.RetryIntervalSeconds),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.service.UpdateSMTPSettings(ctxFor(t), update); err != nil {
		t.Fatal("committed credential replacement", err)
	}
	current, err := f.service.GetSMTPSettings(ctxFor(t), f.admin)
	if err != nil || !current.Configured || current.Version == cfg.Version || !current.CredentialPresent {
		t.Fatal("configured replacement did not advance version", err)
	}
	var active int
	err = f.store.QueryRow(ctxFor(t), `SELECT count(*) FROM agenteam_secret.secret_leases WHERE owner_kind='account_delivery_attempt' AND NOT released`).Scan(&active)
	if err != nil || active != 2 {
		t.Fatal("replacement retired an active token or credential lease", active, err)
	}
	if err = network.Release(ctxFor(t), endpoint.ID); err != nil {
		t.Fatal(err)
	}
	select {
	case err = <-done:
		if err == nil {
			t.Fatal("old attempt sent after configured credential replacement")
		}
	case <-ctx.Done():
		t.Fatal("actual SMTP owner did not join")
	}
	state, err := network.State(ctxFor(t), endpoint.ID)
	if err != nil || state.Auth != 1 || state.Mail != 0 || state.Messages != 0 {
		t.Fatal("stale attempt crossed the MAIL socket gate", state, err)
	}
	err = f.store.QueryRow(ctxFor(t), `SELECT count(*) FROM agenteam_secret.secret_leases WHERE owner_kind='account_delivery_attempt' AND NOT released`).Scan(&active)
	if err != nil || active != 0 || !m.registry.Joined() {
		t.Fatal("replacement did not retain leases through actual join", active, err)
	}
}
func createMailInvitation(t *testing.T, f *fixture) c.InvitationReceipt {
	t.Helper()
	r, e := c.NewInvitationCreate(c.InvitationCreateFields{Actor: f.admin, Key: foundation.IdempotencyKey(id[struct{}](t).String()), Email: "invite@example.test"})
	if e != nil {
		t.Fatal(e)
	}
	v, e := f.service.CreateInvitation(ctxFor(t), r)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = f.service.ReconcileDeliveryIntents(ctxFor(t)); e != nil {
		t.Fatal(e)
	}
	return v
}
func TestAccountMailActualAuthAndMailSeparateAdmission(t *testing.T) {
	network := startSMTP(t)
	for _, mutation := range []string{"unconfigure", "revoke"} {
		for _, stage := range []string{"ehlo", "auth", "mail"} {
			t.Run(mutation+"/"+stage, func(t *testing.T) {
				endpoint, e := network.Create(ctxFor(t), smtpfixture.Scenario{Mode: "starttls", PauseStage: stage})
				if e != nil {
					t.Fatal(e)
				}
				// EHLO pauses before STARTTLS; other stages exercise the upgraded TLS wire.
				f := newAccount(t)
				configure(t, f, endpoint, "starttls", true)
				m := composeMail(t, f, network)
				invite := createMailInvitation(t, f)
				ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
				defer cancel()
				done := make(chan error, 1)
				go func() { done <- m.worker.RunJob(ctx, invite.JobID) }()
				waitSMTPPhase(t, network, endpoint, stage)
				if mutation == "unconfigure" {
					unconfigure(t, f)
				} else {
					e = f.service.RevokeInvitation(ctxFor(t), c.InvitationRevoke{Actor: f.admin, Key: foundation.IdempotencyKey(id[struct{}](t).String()), ID: invite.ID, ExpectedVersion: invite.Version})
					if e != nil {
						t.Fatal("committed token invalidation", e)
					}
				}
				var active int
				e = f.store.QueryRow(ctxFor(t), `SELECT count(*) FROM agenteam_secret.secret_leases WHERE owner_kind='account_delivery_attempt' AND NOT released`).Scan(&active)
				if e != nil || active != 2 {
					t.Fatal("invalidating mutation retired in-flight leases", active, e)
				}
				if e = network.Release(ctxFor(t), endpoint.ID); e != nil {
					t.Fatal(e)
				}
				select {
				case e = <-done:
				case <-ctx.Done():
					t.Fatal("real SMTP owner failed to join")
				}
				if stage == "mail" && e != nil || stage != "mail" && e == nil {
					t.Fatal("admission outcome", e)
				}
				state, err := network.State(ctxFor(t), endpoint.ID)
				if err != nil {
					t.Fatal(err)
				}
				wantAuth, wantMail, wantMessages := 0, 0, 0
				if stage != "ehlo" {
					wantAuth = 1
				}
				if stage == "mail" {
					wantMail = 1
					wantMessages = 1
				}
				if state.Auth != wantAuth || state.Mail != wantMail || state.Messages != wantMessages {
					t.Fatal("actual first-write ordering", state)
				}
				e = f.store.QueryRow(ctxFor(t), `SELECT count(*) FROM agenteam_secret.secret_leases WHERE owner_kind='account_delivery_attempt' AND NOT released`).Scan(&active)
				if e != nil || active != 0 || !m.registry.Joined() {
					t.Fatal("actual join did not release original leases", active, e)
				}
				if mutation == "revoke" {
					var live int
					e = f.store.QueryRow(ctxFor(t), `SELECT count(*) FROM agenteam_account.invitations WHERE id=$1`, invite.ID.String()).Scan(&live)
					if e != nil || live != 0 {
						t.Fatal("in-flight delivery revived invalid token", live, e)
					}
				}
			})
		}
	}
}
