//go:build integration

package accountmail_test

import (
	"errors"
	"sync"
	"testing"

	c "github.com/LunaDeerTech/agenteam/internal/central/account/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	smtpfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/smtp"
)

func faultCode(e error, code foundation.Code) bool {
	var f *foundation.Fault
	return errors.As(e, &f) && f.Code == code
}
func retryRequest(t *testing.T, f *fixture, job c.JobID) c.MailJobRetry {
	t.Helper()
	status, e := f.service.GetMailJob(ctxFor(t), f.admin, job)
	if e != nil {
		t.Fatal(e)
	}
	return c.MailJobRetry{Actor: f.admin, Key: foundation.IdempotencyKey(id[struct{}](t).String()), JobID: job, ExpectedVersion: status.Version}
}
func TestAccountMailManualRetryAtomicRootAndSingleAcceptedCycle(t *testing.T) {
	network := startSMTP(t)
	endpoint, e := network.Create(ctxFor(t), smtpfixture.Scenario{Mode: "none", MailCode: 550})
	if e != nil {
		t.Fatal(e)
	}
	f := newAccount(t)
	configure(t, f, endpoint, "none", false)
	m := composeMail(t, f, network, uint16(endpoint.Port))
	job := testJob(t, f)
	if e = m.worker.RunJob(ctxFor(t), job); e == nil {
		t.Fatal("actual terminal failure absent")
	}
	q := retryRequest(t, f, job)
	got, e := f.service.RetryMailJob(ctxFor(t), q)
	if e != nil || got.Phase != "enqueue_pending" || got.JobID == job || got.Attempts != 0 {
		t.Fatal("retry accepted", got, e)
	}
	again, e := f.service.RetryMailJob(ctxFor(t), q)
	if e != nil || again != got {
		t.Fatal("receipt", again, e)
	}
	var intents, events, audits, attempts int
	var version int64
	e = f.store.QueryRow(ctxFor(t), `SELECT (SELECT count(*) FROM agenteam_account.delivery_intents),(SELECT count(*) FROM agenteam_outbox.events WHERE event_type='account.delivery-requested'),(SELECT count(*) FROM agenteam_audit.audit_records WHERE action='smtp.delivery.retry'),(SELECT count(*) FROM agenteam_account.mail_attempts),version FROM agenteam_account.mail_jobs WHERE id=$1`, job.String()).Scan(&intents, &events, &audits, &attempts, &version)
	if e != nil || intents != 2 || events != 2 || audits != 1 || attempts != 1 || version != int64(q.ExpectedVersion)+1 {
		t.Fatal("atomic retry facts", intents, events, audits, attempts, version, e)
	}
	other := retryRequest(t, f, job)
	if _, e = f.service.RetryMailJob(ctxFor(t), other); !faultCode(e, foundation.ResourceBusy) {
		t.Fatal("unenqueued cycle not protected", e)
	}
	altered := q
	altered.ExpectedVersion++
	if _, e = f.service.RetryMailJob(ctxFor(t), altered); !faultCode(e, foundation.IdempotencyKeyReused) {
		t.Fatal("semantic digest", e)
	}
	if _, e = f.service.ReconcileDeliveryIntents(ctxFor(t)); e != nil {
		t.Fatal(e)
	}
	if e = m.worker.RunJob(ctxFor(t), got.JobID); e == nil {
		t.Fatal("second real terminal failure absent")
	}
	newRequest := retryRequest(t, f, got.JobID)
	third, e := f.service.RetryMailJob(ctxFor(t), newRequest)
	if e != nil {
		t.Fatal("retry of retry", e)
	}
	var roots, distinct int
	e = f.store.QueryRow(ctxFor(t), `SELECT count(*),count(DISTINCT origin_intent_id) FROM agenteam_account.delivery_intents`).Scan(&roots, &distinct)
	if e != nil || roots != 3 || distinct != 1 || third.Attempts != 0 {
		t.Fatal("one-hop root", roots, distinct, e)
	}
	if _, e = f.service.RetryMailJob(ctxFor(t), q); e != nil {
		t.Fatal("historical receipt while newer cycle exists", e)
	}
	state, e := network.State(ctxFor(t), endpoint.ID)
	if e != nil || state.Mail != 2 {
		t.Fatal("retry request itself emitted", state, e)
	}
}
func TestAccountMailConcurrentRetryVersionOnlyOneNewCycle(t *testing.T) {
	network := startSMTP(t)
	endpoint, e := network.Create(ctxFor(t), smtpfixture.Scenario{Mode: "none", MailCode: 550})
	if e != nil {
		t.Fatal(e)
	}
	f := newAccount(t)
	configure(t, f, endpoint, "none", false)
	m := composeMail(t, f, network, uint16(endpoint.Port))
	job := testJob(t, f)
	if e = m.worker.RunJob(ctxFor(t), job); e == nil {
		t.Fatal("actual terminal failure absent")
	}
	a := retryRequest(t, f, job)
	b := a
	b.Key = foundation.IdempotencyKey(id[struct{}](t).String())
	start := make(chan struct{})
	results := make(chan error, 2)
	var wg sync.WaitGroup
	for _, q := range []c.MailJobRetry{a, b} {
		wg.Add(1)
		go func() { defer wg.Done(); <-start; _, e := f.service.RetryMailJob(ctxFor(t), q); results <- e }()
	}
	close(start)
	wg.Wait()
	close(results)
	ok, conflict := 0, 0
	for e := range results {
		if e == nil {
			ok++
		} else if faultCode(e, foundation.VersionConflict) {
			conflict++
		} else {
			t.Fatal("unexpected concurrent result", e)
		}
	}
	if ok != 1 || conflict != 1 {
		t.Fatal("competing cycles", ok, conflict)
	}
	var n int
	if e = f.store.QueryRow(ctxFor(t), `SELECT count(*) FROM agenteam_account.delivery_intents`).Scan(&n); e != nil || n != 2 {
		t.Fatal(n, e)
	}
}
