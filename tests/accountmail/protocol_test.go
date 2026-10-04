//go:build integration

package accountmail_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	smtpfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/smtp"
)

func TestAccountMailProtocolFailuresNeverFallbackOrClaimSent(t *testing.T) {
	network := startSMTP(t)
	cases := []struct {
		name                 string
		scenario             smtpfixture.Scenario
		mode, phase, result  string
		auth, mail, messages int
	}{
		{"no_starttls", smtpfixture.Scenario{Mode: "starttls", NoSTARTTLS: true}, "starttls", "failed", "failed", 0, 0, 0},
		{"no_auth", smtpfixture.Scenario{Mode: "none", NoAuth: true}, "none", "failed", "failed", 0, 0, 0},
		{"auth_535", smtpfixture.Scenario{Mode: "none", AuthCode: 535}, "none", "failed", "failed", 1, 0, 0},
		{"mail_451", smtpfixture.Scenario{Mode: "none", MailCode: 451}, "none", "retry_wait", "failed", 1, 1, 0},
		{"mail_550", smtpfixture.Scenario{Mode: "none", MailCode: 550}, "none", "failed", "failed", 1, 1, 0},
		{"lost_acceptance", smtpfixture.Scenario{Mode: "none", CloseStage: "accepted"}, "none", "retry_wait", "unknown", 1, 1, 1},
		{"huge_line", smtpfixture.Scenario{Mode: "none", GreetingBytes: 5000}, "none", "failed", "failed", 0, 0, 0},
		{"too_many_lines", smtpfixture.Scenario{Mode: "none", GreetingLines: 101}, "none", "failed", "failed", 0, 0, 0},
		{"total_header", smtpfixture.Scenario{Mode: "none", GreetingLines: 20, GreetingBytes: 2000}, "none", "failed", "failed", 0, 0, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			endpoint, e := network.Create(ctxFor(t), tc.scenario)
			if e != nil {
				t.Fatal(e)
			}
			f := newAccount(t)
			configure(t, f, endpoint, tc.mode, true)
			m := composeMail(t, f, network, uint16(endpoint.Port))
			job := testJob(t, f)
			err := m.worker.RunJob(ctxFor(t), job)
			if err == nil {
				t.Fatal("expected protocol failure")
			}
			var normal bytes.Buffer
			fmt.Fprintf(&normal, "%v %+v %#v", err, err, err)
			raw, _ := json.Marshal(err)
			normal.Write(raw)
			for _, secret := range []string{"owned fixture password", "recipient@example.test", smtpfixture.Host, "fixture response"} {
				if strings.Contains(normal.String(), secret) {
					t.Fatal("ordinary error leaked transport data")
				}
			}
			status, e := f.service.GetMailJob(ctxFor(t), f.admin, job)
			if e != nil || status.Phase != tc.phase || status.Attempts != 1 {
				t.Fatal("durable failure classification", status, e)
			}
			var outcome string
			var terminal, joined bool
			e = f.store.QueryRow(ctxFor(t), `SELECT result,terminal,io_joined FROM agenteam_account.mail_attempts WHERE job_id=$1`, job.String()).Scan(&outcome, &terminal, &joined)
			if e != nil || outcome != tc.result || !terminal || !joined {
				t.Fatal("attempt terminal", outcome, terminal, joined, e)
			}
			state, e := network.State(ctxFor(t), endpoint.ID)
			if e != nil || state.Auth != tc.auth || state.Mail != tc.mail || state.Messages != tc.messages {
				t.Fatal("actual server counters", state, e)
			}
			content, e := os.ReadFile(f.log)
			if e != nil {
				t.Fatal(e)
			}
			lines := bytes.Count(content, []byte("\n"))
			clear(content)
			if lines != 1 {
				t.Fatal("configured failure fell back to restricted log")
			}
		})
	}
}
func waitSMTPPhase(t *testing.T, network *smtpfixture.Fixture, endpoint smtpfixture.Endpoint, phase string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	for {
		state, e := network.State(ctx, endpoint.ID)
		if e != nil {
			t.Fatal(e)
		}
		if state.Phase == phase {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatal("server phase not observed", phase)
		case <-ticker.C:
		}
	}
}
func TestAccountMailSlowReplyHonorsOriginalParentAndActuallyJoins(t *testing.T) {
	network := startSMTP(t)
	for _, mode := range []string{"cancelled_parent", "read_idle"} {
		t.Run(mode, func(t *testing.T) {
			endpoint, e := network.Create(ctxFor(t), smtpfixture.Scenario{Mode: "none", PauseStage: "greeting"})
			if e != nil {
				t.Fatal(e)
			}
			f := newAccount(t)
			configure(t, f, endpoint, "none", true)
			m := composeMail(t, f, network, uint16(endpoint.Port))
			job := testJob(t, f)
			ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
			defer cancel()
			done := make(chan error, 1)
			go func() { done <- m.worker.RunJob(ctx, job) }()
			waitSMTPPhase(t, network, endpoint, "greeting")
			start := time.Now()
			if mode == "cancelled_parent" {
				cancel()
			}
			var err error
			select {
			case err = <-done:
			case <-time.After(7 * time.Second):
				t.Fatal("SMTP read did not end within fixed idle cap")
			}
			if err == nil || mode == "cancelled_parent" && time.Since(start) > 2*time.Second {
				t.Fatal("short parent lost", time.Since(start), err)
			}
			state, e := network.State(ctxFor(t), endpoint.ID)
			if e != nil || state.Connections != 1 || state.Auth != 0 || state.Mail != 0 {
				t.Fatal(state, e)
			}
			if !m.registry.Joined() {
				t.Fatal("actual socket owner not joined")
			}
			var active int
			e = f.store.QueryRow(ctxFor(t), `SELECT count(*) FROM agenteam_secret.secret_leases WHERE NOT released AND owner_kind='account_delivery_attempt'`).Scan(&active)
			if e != nil || active != 0 {
				t.Fatal("lease retained after actual join", active, e)
			}
			if e = network.Release(ctxFor(t), endpoint.ID); e != nil {
				t.Fatal(e)
			}
		})
	}
}
