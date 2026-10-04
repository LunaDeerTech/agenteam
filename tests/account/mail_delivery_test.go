//go:build integration

package account_test

import (
	"context"
	"sync"
	"testing"

	"github.com/LunaDeerTech/agenteam/internal/central/account"
	c "github.com/LunaDeerTech/agenteam/internal/central/account/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

// This adapter exercises account's formal port, without claiming SMTP I/O.
// Its test marks join only after every borrowed material has actually returned.
type manualMailRuntime struct {
	mu      sync.Mutex
	process c.ProcessID
	issuer  c.DeliveryIssuer
	active  map[string]c.DeliveryAttempt
	joined  map[string]c.DeliveryCompletion
}

func newManualMail(t *testing.T, f *b02Fixture) (c.DeliveryPort, *manualMailRuntime) {
	t.Helper()
	r := &manualMailRuntime{process: f.process.id, issuer: c.NewDeliveryIssuer(), active: map[string]c.DeliveryAttempt{}, joined: map[string]c.DeliveryCompletion{}}
	p, e := account.NewDeliveryPort(f.service, r)
	if e != nil {
		t.Fatal(e)
	}
	return p, r
}
func (r *manualMailRuntime) CurrentProcess() c.ProcessID { return r.process }
func (r *manualMailRuntime) RequireActive(a c.DeliveryAttempt) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.active[a.Details().AttemptID.String()].Same(a) {
		return nil
	}
	return foundation.NewFault(foundation.InvalidState, foundation.NotStarted)
}
func (r *manualMailRuntime) RequireJoined(a c.DeliveryAttempt, x c.DeliveryCompletion) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if x.Matches(r.issuer, a) && r.joined[a.Details().AttemptID.String()].Matches(r.issuer, a) {
		return nil
	}
	return foundation.NewFault(foundation.Forbidden, foundation.NotStarted)
}
func (r *manualMailRuntime) accept(a c.DeliveryAttempt) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.active[a.Details().AttemptID.String()] = a
}
func (r *manualMailRuntime) finish(t *testing.T, a c.DeliveryAttempt, m c.DeliveryMaterials) c.DeliveryCompletion {
	t.Helper()
	m.Destroy()
	<-m.Done()
	x, e := c.NewDeliveryCompletion(r.issuer, a, c.DeliveryOutcome{Result: c.DeliveryCancelled, Reason: c.ReasonCancelled})
	if e != nil {
		t.Fatal(e)
	}
	r.mu.Lock()
	delete(r.active, a.Details().AttemptID.String())
	r.joined[a.Details().AttemptID.String()] = x
	r.mu.Unlock()
	return x
}

func TestAccountMailSMTPSettingsAndPreparedLeaseCleanup(t *testing.T) {
	f := newB02Account(t)
	admin := b02Admin(t, f)
	settings, e := f.service.GetSMTPSettings(ctxFor(t), admin)
	if e != nil {
		t.Fatal("settings", e, safeFailure(e))
	}
	password, e := sc.NewSecretMaterial([]byte("owned SMTP fixture password"))
	if e != nil {
		t.Fatal(e)
	}
	defer password.Destroy()
	request, e := c.NewSMTPUpdate(c.SMTPUpdateFields{Actor: admin, Key: foundation.IdempotencyKey(id[struct{}](t).String()), ExpectedVersion: settings.Version, Configured: true, Host: "mail.example.test", Port: 2525, TLSMode: "none", Username: "fixture", SenderEmail: "sender@example.test", SenderName: "Owned fixture", RetryCount: 3, RetryIntervalSeconds: 60, Password: &password})
	if e != nil {
		t.Fatal(e)
	}
	receipt, e := f.service.UpdateSMTPSettings(ctxFor(t), request)
	if e != nil {
		t.Fatal("save", e, safeFailure(e))
	}
	if again, e := f.service.UpdateSMTPSettings(ctxFor(t), request); e != nil || again != receipt {
		t.Fatal("stable receipt", e, safeFailure(e))
	}
	settings, e = f.service.GetSMTPSettings(ctxFor(t), admin)
	if e != nil || !settings.CredentialPresent || settings.Version != receipt.Version {
		t.Fatal("projection", e)
	}
	job, e := f.service.TestSMTP(ctxFor(t), c.SMTPTest{Actor: admin, Key: foundation.IdempotencyKey(id[struct{}](t).String()), Recipient: "recipient@example.test"})
	if e != nil || job.Phase != "enqueue_pending" {
		t.Fatal("accepted test", e, safeFailure(e), job.Phase)
	}
	if _, e = f.service.ReconcileDeliveryIntents(ctxFor(t)); e != nil {
		t.Fatal("enqueue", e, safeFailure(e))
	}
	port, runtime := newManualMail(t, f)
	ids, e := port.NextDeliveries(ctxFor(t))
	if e != nil || len(ids) != 1 || ids[0] != job.JobID {
		t.Fatal("scan", e, len(ids))
	}
	a, e := port.ClaimDelivery(ctxFor(t), job.JobID)
	if e != nil {
		t.Fatal("claim", e, safeFailure(e))
	}
	runtime.accept(a)
	m, e := port.PrepareDelivery(ctxFor(t), a)
	if e != nil {
		t.Fatal("prepare", e, safeFailure(e))
	}
	if e = m.Use(func(f c.DeliveryMaterialFields) error {
		if f.Username != "fixture" || f.Recipient != "recipient@example.test" || f.Attempt.Details().Kind != c.TestDelivery {
			return c.Invalid()
		}
		return f.Password.Use(func(b []byte) error {
			if string(b) != "owned SMTP fixture password" {
				return c.Invalid()
			}
			return nil
		})
	}); e != nil {
		t.Fatal("current material", e)
	}
	var leases int
	if e = f.store.QueryRow(ctxFor(t), `SELECT count(*) FROM agenteam_secret.secret_leases WHERE owner_id=$1 AND NOT released`, a.Details().AttemptID.String()).Scan(&leases); e != nil || leases != 1 {
		t.Fatal("live credential lease", leases, e)
	}
	completion := runtime.finish(t, a, m)
	if e = port.FinishDelivery(ctxFor(t), a, completion); e != nil {
		t.Fatal("finish", e, safeFailure(e))
	}
	var terminal, joined bool
	var phase string
	var audits int
	e = f.store.QueryRow(ctxFor(t), `SELECT a.terminal,a.io_joined,j.phase,(SELECT count(*) FROM agenteam_audit.audit_records WHERE action='smtp.delivery') FROM agenteam_account.mail_attempts a JOIN agenteam_account.mail_jobs j ON j.id=a.job_id WHERE a.id=$1`, a.Details().AttemptID.String()).Scan(&terminal, &joined, &phase, &audits)
	if e != nil || !terminal || !joined || phase != "cancelled" || audits != 1 {
		t.Fatal("atomic final", e, terminal, joined, phase, audits)
	}
	if e = port.FinishDelivery(ctxFor(t), a, completion); e != nil {
		t.Fatal("final replay", e)
	}
	if e = f.store.QueryRow(ctxFor(t), `SELECT count(*) FROM agenteam_secret.secret_leases WHERE owner_id=$1 AND NOT released`, a.Details().AttemptID.String()).Scan(&leases); e != nil || leases != 0 {
		t.Fatal("released", leases, e)
	}
}
func TestAccountMailNeverAcquiredCanCloseWithoutCreatingLease(t *testing.T) {
	f := newB02Account(t)
	admin := b02Admin(t, f)
	request, _ := c.NewInvitationCreate(c.InvitationCreateFields{Actor: admin, Key: foundation.IdempotencyKey(id[struct{}](t).String()), Email: "never-acquired@example.test"})
	invite, e := f.service.CreateInvitation(ctxFor(t), request)
	if e != nil {
		t.Fatal(e, safeFailure(e))
	}
	if _, e = f.service.ReconcileDeliveryIntents(ctxFor(t)); e != nil {
		t.Fatal(e)
	}
	p, r := newManualMail(t, f)
	a, e := p.ClaimDelivery(ctxFor(t), invite.JobID)
	if e != nil {
		t.Fatal(e, safeFailure(e))
	}
	r.accept(a)
	completion := r.finish(t, a, c.DeliveryMaterials{})
	if e = p.FinishDelivery(ctxFor(t), a, completion); e != nil {
		t.Fatal("zero lease convergence", e, safeFailure(e))
	}
	var leases int
	var joined, terminal bool
	e = f.store.QueryRow(ctxFor(t), `SELECT io_joined,terminal,(SELECT count(*) FROM agenteam_secret.secret_leases WHERE owner_id=$1) FROM agenteam_account.mail_attempts WHERE id=$1`, a.Details().AttemptID.String()).Scan(&joined, &terminal, &leases)
	if e != nil || !joined || !terminal || leases != 0 {
		t.Fatal("no invented lease", e, joined, terminal, leases)
	}
	if _, e = p.PrepareDelivery(context.Background(), a); e == nil {
		t.Fatal("late prepare reopened")
	}
}
