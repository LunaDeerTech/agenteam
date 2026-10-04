//go:build integration

package accountmail_test

import (
	"context"
	"testing"
	"time"

	c "github.com/LunaDeerTech/agenteam/internal/central/account/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/accountmail"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	smtpfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/smtp"
)

func setRetryCount(t *testing.T, f *fixture, count int64) {
	t.Helper()
	cfg, e := f.service.GetSMTPSettings(ctxFor(t), f.admin)
	if e != nil {
		t.Fatal(e)
	}
	q, e := c.NewSMTPUpdate(c.SMTPUpdateFields{Actor: f.admin, Key: foundation.IdempotencyKey(id[struct{}](t).String()), ExpectedVersion: cfg.Version, Configured: cfg.Configured, Host: cfg.Host, Port: cfg.Port, TLSMode: cfg.TLSMode, Username: cfg.Username, SenderEmail: cfg.SenderEmail, SenderName: cfg.SenderName, RetryCount: count, RetryIntervalSeconds: 10})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = f.service.UpdateSMTPSettings(ctxFor(t), q); e != nil {
		t.Fatal(e)
	}
}
func TestAccountMailSixAttemptsAndLoweredBudgetUseRealFailures(t *testing.T) {
	network := startSMTP(t)
	endpoint, e := network.Create(ctxFor(t), smtpfixture.Scenario{Mode: "none", MailCode: 451})
	if e != nil {
		t.Fatal(e)
	}
	f := newAccount(t)
	configure(t, f, endpoint, "none", false)
	setRetryCount(t, f, 5)
	m := composeMail(t, f, network, uint16(endpoint.Port))
	job := testJob(t, f)
	for attempt := 1; attempt <= 6; attempt++ {
		if e = m.worker.RunJob(ctxFor(t), job); e == nil {
			t.Fatal("actual 451 became success")
		}
		status, e := f.service.GetMailJob(ctxFor(t), f.admin, job)
		if e != nil || int(status.Attempts) != attempt {
			t.Fatal(status, e)
		}
		want := "retry_wait"
		if attempt == 6 {
			want = "failed"
		}
		if status.Phase != want {
			t.Fatal("wrong budget phase", status)
		}
		if attempt < 6 {
			var delay float64
			e = f.store.QueryRow(ctxFor(t), `SELECT EXTRACT(EPOCH FROM (j.next_at-a.completed_at))::float8 FROM agenteam_account.mail_jobs j JOIN agenteam_account.mail_attempts a ON a.id=j.current_attempt_id WHERE j.id=$1`, job.String()).Scan(&delay)
			base := float64(int64(10) * (int64(1) << uint(attempt-1)))
			if e != nil || delay < base-1 || delay > base*1.1+1 {
				t.Fatal("backoff or jitter", delay, base, e)
			}
			if _, e = f.store.Exec(ctxFor(t), `UPDATE agenteam_account.mail_jobs SET next_at=clock_timestamp() WHERE id=$1`, job.String()); e != nil {
				t.Fatal(e)
			}
		}
	}
	if e = m.worker.RunJob(ctxFor(t), job); e == nil {
		t.Fatal("seventh attempt admitted")
	}
	state, e := network.State(ctxFor(t), endpoint.ID)
	if e != nil || state.Mail != 6 || state.Connections != 6 {
		t.Fatal("wire retry count", state, e)
	}
	other := testJob(t, f)
	if e = m.worker.RunJob(ctxFor(t), other); e == nil {
		t.Fatal("expected original failure")
	}
	setRetryCount(t, f, 0)
	if _, e = f.store.Exec(ctxFor(t), `UPDATE agenteam_account.mail_jobs SET next_at=clock_timestamp() WHERE id=$1`, other.String()); e != nil {
		t.Fatal(e)
	}
	if e = m.worker.RunJob(ctxFor(t), other); e == nil {
		t.Fatal("lowered count reset spent budget")
	}
	state, e = network.State(ctxFor(t), endpoint.ID)
	if e != nil || state.Connections != 7 {
		t.Fatal("lowered budget performed extra dial", state, e)
	}
}
func TestAccountMailRuntimeStopForceUsesActualOwnersAndTwoSlots(t *testing.T) {
	network := startSMTP(t)
	endpoint, e := network.Create(ctxFor(t), smtpfixture.Scenario{Mode: "none", PauseStage: "greeting"})
	if e != nil {
		t.Fatal(e)
	}
	f := newAccount(t)
	configure(t, f, endpoint, "none", true)
	m := composeMail(t, f, network, uint16(endpoint.Port))
	for range 3 {
		_ = testJob(t, f)
	}
	runtime, e := accountmail.NewRuntime(f.service, m.worker)
	if e != nil {
		t.Fatal(e)
	}
	if e = runtime.Start(ctxFor(t)); e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = runtime.Force(ctx)
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	tick := time.NewTicker(5 * time.Millisecond)
	defer tick.Stop()
	for {
		state, e := network.State(ctx, endpoint.ID)
		if e != nil {
			t.Fatal(e)
		}
		if state.Connections == 2 {
			break
		}
		if state.Connections > 2 {
			t.Fatal("third concurrent connection")
		}
		select {
		case <-ctx.Done():
			t.Fatal("two actual workers not reached")
		case <-tick.C:
		}
	}
	runtime.StopAdmission()
	drain, cancelDrain := context.WithTimeout(context.Background(), 30*time.Millisecond)
	if e = runtime.Drain(drain); e == nil || runtime.Joined() {
		t.Fatal("drain invented actual join", e)
	}
	cancelDrain()
	force, stop := context.WithTimeout(context.Background(), time.Second)
	defer stop()
	start := time.Now()
	e = runtime.Force(force)
	if time.Since(start) > 1200*time.Millisecond {
		t.Fatal("shared force budget extended")
	}
	if !runtime.Joined() {
		t.Fatal("actual network workers failed to join", e)
	}
	if e = runtime.Check(context.Background()); e == nil {
		t.Fatal("stopped runtime reports healthy")
	}
	var attempts int
	if e = f.store.QueryRow(ctxFor(t), `SELECT count(*) FROM agenteam_account.mail_attempts`).Scan(&attempts); e != nil || attempts != 2 {
		t.Fatal("queued third job was claimed after stop", attempts, e)
	}
	if e = network.Release(ctxFor(t), endpoint.ID); e != nil {
		t.Fatal(e)
	}
}
