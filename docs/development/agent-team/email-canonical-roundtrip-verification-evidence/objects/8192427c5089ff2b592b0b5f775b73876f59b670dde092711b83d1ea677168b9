//go:build integration

package accountmail_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"testing"
	"time"

	c "github.com/LunaDeerTech/agenteam/internal/central/account/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
	smtpfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/smtp"
)

func TestAccountMailEmailCanonicalWireRoundTrip(t *testing.T) {
	network := startSMTP(t)
	cases := []struct{ name, sender, recipient, senderWire, recipientWire string }{
		{"ipv6", "sender@[ipv6:2001:db8::7]", "recipient@[ipv6:2001:db8::8]", "sender@[IPv6:2001:db8::7]", "recipient@[IPv6:2001:db8::8]"},
		{"prefixed-ipv4", "sender@[ipv6:192.0.2.7]", "recipient@[ipv6:192.0.2.8]", "sender@[IPv6:192.0.2.7]", "recipient@[IPv6:192.0.2.8]"},
		{"mapped", "sender@[::ffff:192.0.2.7]", "recipient@[0:0:0:0:0:ffff:c000:208]", "sender@[::ffff:192.0.2.7]", "recipient@[0:0:0:0:0:ffff:c000:208]"},
		{"ordinary", "sender@example.test", "recipient@example.test", "sender@example.test", "recipient@example.test"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()
			endpoint, err := network.Create(ctx, smtpfixture.Scenario{Mode: "none"})
			if err != nil {
				t.Fatal("SMTP scenario", err)
			}
			f := newAccount(t)
			settings, err := f.service.GetSMTPSettings(ctx, f.admin)
			if err != nil {
				t.Fatal(err)
			}
			password, err := sc.NewSecretMaterial([]byte("owned email-closure SMTP credential"))
			if err != nil {
				t.Fatal(err)
			}
			defer password.Destroy()
			request, err := c.NewSMTPUpdate(c.SMTPUpdateFields{Actor: f.admin, Key: foundation.IdempotencyKey(id[struct{}](t).String()), ExpectedVersion: settings.Version, Configured: true, Host: smtpfixture.Host, Port: endpoint.Port, TLSMode: "none", Username: "fixture", Password: &password, SenderEmail: tc.sender, RetryCount: 0, RetryIntervalSeconds: 10})
			if err != nil {
				t.Fatal(err)
			}
			if _, err = f.service.UpdateSMTPSettings(ctx, request); err != nil {
				t.Fatal("canonical sender save", err)
			}
			m := composeMail(t, f, network, uint16(endpoint.Port))
			accepted, err := f.service.TestSMTP(ctx, c.SMTPTest{Actor: f.admin, Key: foundation.IdempotencyKey(id[struct{}](t).String()), Recipient: tc.recipient})
			if err != nil {
				t.Fatal("canonical test intent", err)
			}
			if _, err = f.service.ReconcileDeliveryIntents(ctx); err != nil {
				t.Fatal(err)
			}
			if err = m.worker.RunJob(ctx, accepted.JobID); err != nil {
				t.Fatal("actual SMTP send", err)
			}
			status, err := f.service.GetMailJob(ctx, f.admin, accepted.JobID)
			if err != nil || status.Phase != "sent" || status.Attempts != 1 {
				t.Fatal("sent fact", err)
			}
			// Await the fixture's actual connection close, not only its 250 reply.
			closed, cancelClose := context.WithTimeout(ctx, 2*time.Second)
			defer cancelClose()
			tick := time.NewTicker(10 * time.Millisecond)
			defer tick.Stop()
			var state smtpfixture.State
			for {
				state, err = network.State(closed, endpoint.ID)
				if err != nil {
					t.Fatal("SMTP observation", err)
				}
				if state.Closed == 1 {
					break
				}
				select {
				case <-tick.C:
				case <-closed.Done():
					t.Fatal("SMTP connection did not actually close")
				}
			}
			wantMessage := "From: <" + tc.senderWire + ">\r\nTo: <" + tc.recipientWire + ">\r\nMessage-ID: <" + accepted.JobID.String() + "@accounts.example.test>\r\nSubject: Agenteam account\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=utf-8\r\nContent-Transfer-Encoding: 8bit\r\n\r\nAgenteam SMTP test.\r\n"
			digest := func(value string) string { h := sha256.Sum256([]byte(value)); return hex.EncodeToString(h[:]) }
			mailMatches := state.MailFromSHA == digest("MAIL FROM:<"+tc.senderWire+">\r\n")
			rcptMatches := state.RCPTToSHA == digest("RCPT TO:<"+tc.recipientWire+">\r\n")
			bodyMatches := state.MessageSHA == digest(wantMessage)
			if !mailMatches || !rcptMatches || !bodyMatches || state.MessageBytes != len(wantMessage) {
				t.Fatalf("SMTP explicit wire matches: mail=%t rcpt=%t body=%t", mailMatches, rcptMatches, bodyMatches)
			}
			if state.Connections != 1 || state.Auth != 1 || state.TLS != 0 || state.Mail != 1 || state.RCPT != 1 || state.Data != 1 || state.Messages != 1 {
				t.Fatal("SMTP protocol counts changed")
			}
			var terminal, joined bool
			var leases, active, audits int
			var sender, recipient string
			err = f.store.QueryRow(ctx, `SELECT a.terminal,a.io_joined,(SELECT count(*) FROM agenteam_secret.secret_leases WHERE owner_id=a.id),(SELECT count(*) FROM agenteam_secret.secret_leases WHERE owner_id=a.id AND NOT released),(SELECT count(*) FROM agenteam_audit.audit_records WHERE action='smtp.delivery' AND cause_ref=a.id::text),(SELECT from_address FROM agenteam_account.smtp_settings WHERE singleton),i.recipient FROM agenteam_account.mail_jobs j JOIN agenteam_account.mail_attempts a ON a.id=j.current_attempt_id JOIN agenteam_account.delivery_intents i ON i.id=j.intent_id WHERE j.id=$1`, accepted.JobID.String()).Scan(&terminal, &joined, &leases, &active, &audits, &sender, &recipient)
			if err != nil || !terminal || !joined || leases != 1 || active != 0 || audits != 1 || !m.registry.Joined() || sender != tc.sender || recipient != tc.recipient {
				t.Fatal("canonical persistence, terminal or actual material join", err)
			}
			t.Log("explicit MAIL/RCPT/message matches=true; one sent message; terminal/io_joined/lease release/registry joined=true")
		})
	}
}
