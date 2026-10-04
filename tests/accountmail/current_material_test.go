//go:build integration

package accountmail_test

import (
	"context"
	"encoding/json"
	"net/netip"
	"os"
	"testing"
	"time"

	c "github.com/LunaDeerTech/agenteam/internal/central/account/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/accountmail"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/outbound"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
	smtpfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/smtp"
)

type copiedLink struct {
	kind  c.DeliveryKind
	id    string
	token sc.SecretMaterial
}
type captureLinkPort struct {
	c.DeliveryPort
	ready chan copiedLink
}

func (p *captureLinkPort) PrepareDelivery(ctx context.Context, a c.DeliveryAttempt) (c.DeliveryMaterials, error) {
	m, e := p.DeliveryPort.PrepareDelivery(ctx, a)
	if e != nil {
		return m, e
	}
	e = m.Use(func(f c.DeliveryMaterialFields) error {
		var token sc.SecretMaterial
		e := f.Token.Use(func(b []byte) error { var e error; token, e = sc.NewSecretMaterial(b); return e })
		if e == nil {
			p.ready <- copiedLink{a.Details().Kind, f.LinkID, token}
		}
		return e
	})
	return m, e
}
func bootstrapPassword(t *testing.T, f *fixture) sc.SecretMaterial {
	t.Helper()
	b, e := os.ReadFile(f.log)
	if e != nil {
		t.Fatal(e)
	}
	defer clear(b)
	var row struct {
		Password string `json:"initial_password"`
	}
	if e = json.Unmarshal(b, &row); e != nil {
		t.Fatal(e)
	}
	m, e := sc.NewSecretMaterial([]byte(row.Password))
	row.Password = ""
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(m.Destroy)
	return m
}
func resetMail(t *testing.T, f *fixture) c.JobID {
	t.Helper()
	b, e := f.service.NewAnonymousContext(ctxFor(t))
	if e != nil {
		t.Fatal(e)
	}
	defer b.Cookie.Destroy()
	defer b.CSRF.Destroy()
	q, e := c.NewResetRequest(c.ResetRequestFields{Browser: b.Identity, Key: foundation.IdempotencyKey(id[struct{}](t).String()), Email: "admin@mail.com", ClientIP: netip.MustParseAddr("198.51.100.14")})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = f.service.RequestPasswordReset(ctxFor(t), q); e != nil {
		t.Fatal(e)
	}
	if _, e = f.service.Recover(ctxFor(t)); e != nil {
		t.Fatal(e)
	}
	if _, e = f.service.ReconcileDeliveryIntents(ctxFor(t)); e != nil {
		t.Fatal(e)
	}
	var raw string
	if e = f.store.QueryRow(ctxFor(t), `SELECT job_id::text FROM agenteam_account.delivery_intents WHERE kind='password_reset'`).Scan(&raw); e != nil {
		t.Fatal(e)
	}
	job, e := foundation.ParseID[c.MailJob](raw)
	if e != nil {
		t.Fatal(e)
	}
	return job
}
func TestAccountMailCurrentMaterialChangesStopAuthAndMail(t *testing.T) {
	network := startSMTP(t)
	for _, mutation := range []string{"redeem", "reset_complete", "password_change", "expiry"} {
		for _, stage := range []string{"ehlo", "auth"} {
			t.Run(mutation+"/"+stage, func(t *testing.T) {
				endpoint, e := network.Create(ctxFor(t), smtpfixture.Scenario{Mode: "starttls", PauseStage: stage})
				if e != nil {
					t.Fatal(e)
				}
				f := newAccount(t)
				old := bootstrapPassword(t, f)
				configure(t, f, endpoint, "starttls", true)
				m := composeMail(t, f, network, uint16(endpoint.Port))
				var job c.JobID
				if mutation == "redeem" || mutation == "expiry" {
					job = createMailInvitation(t, f).JobID
				} else {
					job = resetMail(t, f)
				}
				port := &captureLinkPort{DeliveryPort: m.port, ready: make(chan copiedLink, 1)}
				trust, e := outbound.LoadTrustStore(network.CAFile)
				if e != nil {
					t.Fatal(e)
				}
				worker, e := accountmail.New(accountmail.Dependencies{Port: port, Registry: m.registry, Outbound: m.client, Trust: trust, RecoveryLog: f.sink, PublicOrigin: "https://accounts.example.test"})
				if e != nil {
					t.Fatal(e)
				}
				ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
				defer cancel()
				done := make(chan error, 1)
				go func() { done <- worker.RunJob(ctx, job) }()
				waitSMTPPhase(t, network, endpoint, stage)
				var link copiedLink
				select {
				case link = <-port.ready:
				case <-ctx.Done():
					t.Fatal("material not prepared")
				}
				defer link.token.Destroy()
				password, e := sc.NewSecretMaterial([]byte("Changed fixture passphrase 162839!"))
				if e != nil {
					t.Fatal(e)
				}
				defer password.Destroy()
				switch mutation {
				case "password_change":
					q, e := c.NewPasswordChange(c.PasswordChangeFields{Actor: f.admin, Key: foundation.IdempotencyKey(id[struct{}](t).String()), ExpectedVersion: 1, OldPassword: old, Password: password, Confirmation: password})
					if e != nil {
						t.Fatal(e)
					}
					r, e := f.service.ChangePassword(ctxFor(t), q)
					if e != nil {
						t.Fatal(e)
					}
					if e = r.Close(ctxFor(t)); e != nil {
						t.Fatal(e)
					}
					var rows int
					if e = f.store.QueryRow(ctxFor(t), `SELECT count(*) FROM agenteam_account.password_resets WHERE id=$1`, link.id).Scan(&rows); e != nil || rows != 1 {
						t.Fatal("ordinary change retains old reset row", rows, e)
					}
				case "expiry":
					// Owned clock fixture: represent a genuinely expired 24h invitation;
					// the production maintenance entry must invalidate it under EX.
					if _, e = f.store.Exec(ctxFor(t), `UPDATE agenteam_account.invitations SET created_at=q.n-interval '25 hours',expires_at=q.n-interval '1 hour' FROM(SELECT clock_timestamp() n) q WHERE id=$1`, link.id); e != nil {
						t.Fatal(e)
					}
					if _, e = f.service.Recover(ctxFor(t)); e != nil && !faultCode(e, foundation.ResourceBusy) {
						t.Fatal(e)
					}
				default:
					browser, e := f.service.NewAnonymousContext(ctxFor(t))
					if e != nil {
						t.Fatal(e)
					}
					defer browser.Cookie.Destroy()
					defer browser.CSRF.Destroy()
					if mutation == "redeem" {
						iid, _ := foundation.ParseID[c.Invitation](link.id)
						token, e := c.NewInvitationToken(iid, link.token)
						if e != nil {
							t.Fatal(e)
						}
						q, e := c.NewInvitationRedeem(c.RedeemFields{Browser: browser.Identity, Key: foundation.IdempotencyKey(id[struct{}](t).String()), Token: token, Username: "owned-user", DisplayName: "Owned user", Password: password, Confirmation: password})
						if e != nil {
							t.Fatal(e)
						}
						if _, e = f.service.RedeemInvitation(ctxFor(t), q); e != nil {
							t.Fatal(e)
						}
					} else {
						rid, _ := foundation.ParseID[c.PasswordReset](link.id)
						token, e := c.NewResetToken(rid, link.token)
						if e != nil {
							t.Fatal(e)
						}
						q, e := c.NewResetComplete(c.ResetCompleteFields{Browser: browser.Identity, Key: foundation.IdempotencyKey(id[struct{}](t).String()), Token: token, Password: password, Confirmation: password})
						if e != nil {
							t.Fatal(e)
						}
						if _, e = f.service.CompletePasswordReset(ctxFor(t), q); e != nil {
							t.Fatal(e)
						}
					}
				}
				var active int
				if e = f.store.QueryRow(ctxFor(t), `SELECT count(*) FROM agenteam_secret.secret_leases WHERE owner_kind='account_delivery_attempt' AND NOT released`).Scan(&active); e != nil || active != 2 {
					t.Fatal("mutation pretended IO joined", active, e)
				}
				if e = network.Release(ctxFor(t), endpoint.ID); e != nil {
					t.Fatal(e)
				}
				select {
				case e = <-done:
					if e == nil {
						t.Fatal("invalidated token was sent")
					}
				case <-ctx.Done():
					t.Fatal("socket owner not joined")
				}
				state, e := network.State(ctxFor(t), endpoint.ID)
				wantAuth := 0
				if stage == "auth" {
					wantAuth = 1
				}
				if e != nil || state.Auth != wantAuth || state.Mail != 0 || state.Messages != 0 {
					t.Fatal("actual invalidation ordering", state, e)
				}
				if e = f.store.QueryRow(ctxFor(t), `SELECT count(*) FROM agenteam_secret.secret_leases WHERE owner_kind='account_delivery_attempt' AND NOT released`).Scan(&active); e != nil || active != 0 || !m.registry.Joined() {
					t.Fatal("invalid old material was not safely released", active, e)
				}
			})
		}
	}
}

type exactMailResolver struct {
	host string
	ip   netip.Addr
}

func (r exactMailResolver) Lookup(ctx context.Context, host string) ([]netip.Addr, error) {
	if host != r.host {
		return nil, foundation.NewFault(foundation.InvalidArgument, foundation.NotStarted)
	}
	return []netip.Addr{r.ip}, ctx.Err()
}
func TestAccountMailTLSRejectsUnknownChainAndWrongHost(t *testing.T) {
	network := startSMTP(t)
	for _, mode := range []string{"chain", "hostname"} {
		t.Run(mode, func(t *testing.T) {
			endpoint, e := network.Create(ctxFor(t), smtpfixture.Scenario{Mode: "tls"})
			if e != nil {
				t.Fatal(e)
			}
			f := newAccount(t)
			configure(t, f, endpoint, "tls", true)
			m := composeMail(t, f, network, uint16(endpoint.Port))
			ca, host := network.CAFile, smtpfixture.Host
			if mode == "chain" {
				ca = ""
			} else {
				host = "wrong.mail.example.test"
				cfg, e := f.service.GetSMTPSettings(ctxFor(t), f.admin)
				if e != nil {
					t.Fatal(e)
				}
				q, e := c.NewSMTPUpdate(c.SMTPUpdateFields{Actor: f.admin, Key: foundation.IdempotencyKey(id[struct{}](t).String()), ExpectedVersion: cfg.Version, Configured: true, Host: host, Port: cfg.Port, TLSMode: cfg.TLSMode, Username: cfg.Username, SenderEmail: cfg.SenderEmail, RetryCount: 3, RetryIntervalSeconds: 10})
				if e != nil {
					t.Fatal(e)
				}
				if _, e = f.service.UpdateSMTPSettings(ctxFor(t), q); e != nil {
					t.Fatal(e)
				}
			}
			trust, e := outbound.LoadTrustStore(ca)
			if e != nil {
				t.Fatal(e)
			}
			client, e := outbound.NewClient(m.policy, trust, exactMailResolver{host, netip.MustParseAddr(network.PrivateIP)})
			if e != nil {
				t.Fatal(e)
			}
			defer func() {
				ctx, cancel := context.WithTimeout(context.Background(), time.Second)
				defer cancel()
				_ = client.ForceClose(ctx)
			}()
			worker, e := accountmail.New(accountmail.Dependencies{Port: m.port, Registry: m.registry, Outbound: client, Trust: trust, RecoveryLog: f.sink, PublicOrigin: "https://accounts.example.test"})
			if e != nil {
				t.Fatal(e)
			}
			job := testJob(t, f)
			if e = worker.RunJob(ctxFor(t), job); e == nil {
				t.Fatal("invalid certificate accepted")
			}
			status, e := f.service.GetMailJob(ctxFor(t), f.admin, job)
			if e != nil || status.Phase != "failed" || status.Reason != c.ReasonConfiguration {
				t.Fatal("TLS classification", status, e)
			}
			state, e := network.State(ctxFor(t), endpoint.ID)
			if e != nil || state.Connections != 1 || state.Auth != 0 || state.Mail != 0 {
				t.Fatal("certificate failure leaked credential", state, e)
			}
		})
	}
}
