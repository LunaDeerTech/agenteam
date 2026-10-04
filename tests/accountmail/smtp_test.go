//go:build integration

package accountmail_test

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	c "github.com/LunaDeerTech/agenteam/internal/central/account/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	smtpfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/smtp"
)

func TestAccountMailSMTPThreeModesUseActualControlledSockets(t *testing.T) {
	network := startSMTP(t)
	for _, mode := range []string{"none", "starttls", "tls"} {
		t.Run(mode, func(t *testing.T) {
			endpoint, e := network.Create(ctxFor(t), smtpfixture.Scenario{Mode: mode})
			if e != nil {
				t.Fatal(e)
			}
			f := newAccount(t)
			configure(t, f, endpoint, mode, true)
			m := composeMail(t, f, network, uint16(endpoint.Port))
			job := testJob(t, f)
			if e = m.worker.RunJob(ctxFor(t), job); e != nil {
				t.Fatal("real delivery", e)
			}
			status, e := f.service.GetMailJob(ctxFor(t), f.admin, job)
			if e != nil || status.Phase != "sent" || status.Attempts != 1 {
				t.Fatal("committed outcome", status, e)
			}
			state, e := network.State(ctxFor(t), endpoint.ID)
			if e != nil {
				t.Fatal(e)
			}
			tls := 0
			if mode != "none" {
				tls = 1
			}
			if state.Connections != 1 || state.TLS != tls || state.Auth != 1 || state.Mail != 1 || state.RCPT != 1 || state.Data != 1 || state.Messages != 1 || state.MessageID != "<"+job.String()+"@accounts.example.test>" {
				t.Fatal("actual protocol counters", state)
			}
			var terminal, joined bool
			var audits, leases, active int
			e = f.store.QueryRow(ctxFor(t), `SELECT a.terminal,a.io_joined,(SELECT count(*) FROM agenteam_audit.audit_records WHERE action='smtp.delivery'),(SELECT count(*) FROM agenteam_secret.secret_leases WHERE owner_id=a.id),(SELECT count(*) FROM agenteam_secret.secret_leases WHERE owner_id=a.id AND NOT released) FROM agenteam_account.mail_attempts a WHERE a.job_id=$1`, job.String()).Scan(&terminal, &joined, &audits, &leases, &active)
			if e != nil || !terminal || !joined || audits != 1 || leases != 1 || active != 0 || !m.registry.Joined() {
				t.Fatal("actual join and atomic cleanup", e, terminal, joined, audits, leases, active)
			}
		})
	}
}

func TestAccountMailLogOnlyUnconfiguredAndUsesRealTicket(t *testing.T) {
	network := startSMTP(t)
	endpoint, e := network.Create(ctxFor(t), smtpfixture.Scenario{Mode: "none"})
	if e != nil {
		t.Fatal(e)
	}
	f := newAccount(t)
	m := composeMail(t, f, network, uint16(endpoint.Port))
	req, e := c.NewInvitationCreate(c.InvitationCreateFields{Actor: f.admin, Key: foundation.IdempotencyKey(id[struct{}](t).String()), Email: "invited@example.test"})
	if e != nil {
		t.Fatal(e)
	}
	invite, e := f.service.CreateInvitation(ctxFor(t), req)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = f.service.ReconcileDeliveryIntents(ctxFor(t)); e != nil {
		t.Fatal(e)
	}
	if e = m.worker.RunJob(ctxFor(t), invite.JobID); e != nil {
		t.Fatal("real log delivery", e)
	}
	raw, e := os.ReadFile(f.log)
	if e != nil {
		t.Fatal(e)
	}
	defer clear(raw)
	lines := bytes.Split(bytes.TrimSpace(raw), []byte("\n"))
	if len(lines) != 2 {
		t.Fatal("one bootstrap and one delivery line")
	}
	var row struct{ Purpose, URL, Email string }
	if e = json.Unmarshal(lines[1], &row); e != nil || row.Email != "invited@example.test" || row.URL == "" {
		t.Fatal("restricted record shape", e)
	}
	row.URL = ""
	status, e := f.service.GetMailJob(ctxFor(t), f.admin, invite.JobID)
	if e != nil || status.Phase != "sent" {
		t.Fatal(status, e)
	}
	state, e := network.State(ctxFor(t), endpoint.ID)
	if e != nil || state.Connections != 0 {
		t.Fatal("log used network", e, state)
	}
	if !m.registry.Joined() || f.sink.Joined() {
		t.Fatal("single ticket join confused with whole sink")
	}
	f.sink.StopAdmission()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if e = f.sink.Drain(ctx); e != nil || !f.sink.Joined() {
		t.Fatal("whole sink join", e)
	}
}
