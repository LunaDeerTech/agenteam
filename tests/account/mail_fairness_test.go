//go:build integration

package account_test

import (
	"context"
	"testing"

	c "github.com/LunaDeerTech/agenteam/internal/central/account/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

func mailCurrentAdmin(t *testing.T, f *b02Fixture) identity.Actor {
	t.Helper()
	var user, session string
	if e := f.store.QueryRow(ctxFor(t), `SELECT user_id::text,id::text FROM agenteam_account.sessions WHERE revoked_at IS NULL ORDER BY id LIMIT 1`).Scan(&user, &session); e != nil {
		t.Fatal(e)
	}
	u, _ := foundation.ParseID[identity.User](user)
	s, _ := foundation.ParseID[identity.Session](session)
	a, e := identity.NewHuman(u, s)
	if e != nil {
		t.Fatal(e)
	}
	return a
}
func createMailTest(t *testing.T, f *b02Fixture, a identity.Actor) c.JobID {
	t.Helper()
	r, e := f.service.TestSMTP(ctxFor(t), c.SMTPTest{Actor: a, Key: foundation.IdempotencyKey(id[struct{}](t).String()), Recipient: "owned-fairness@example.test"})
	if e != nil {
		t.Fatal(e, safeFailure(e))
	}
	return r.JobID
}

func TestAccountMailFairHundredProtectedCannotStarveJoinedTail(t *testing.T) {
	f := newB02Account(t)
	first := prepareMailJob(t, f)
	admin := mailCurrentAdmin(t, f)
	jobs := []c.JobID{first}
	for range 100 {
		jobs = append(jobs, createMailTest(t, f, admin))
	}
	for range 2 {
		if _, e := f.service.ReconcileDeliveryIntents(ctxFor(t)); e != nil {
			t.Fatal(e)
		}
	}
	p, r := newManualMail(t, f)
	// Two scans exercise the durable pass, while all jobs are due and none is
	// consumed. Every one of the 101 IDs must appear; each batch is at most100.
	seen := map[c.JobID]bool{}
	for range 2 {
		batch, e := p.NextDeliveries(ctxFor(t))
		if e != nil || len(batch) != 100 {
			t.Fatal("due batch", len(batch), e)
		}
		for _, j := range batch {
			seen[j] = true
		}
	}
	if len(seen) != 101 {
		t.Fatal("due tail starved", len(seen))
	}
	var attempts []c.DeliveryAttempt
	for _, j := range jobs {
		a, e := p.ClaimDelivery(ctxFor(t), j)
		if e != nil {
			t.Fatal(e, safeFailure(e))
		}
		r.accept(a)
		attempts = append(attempts, a)
	}
	tail := attempts[len(attempts)-1]
	control := f.db.Connect(t)
	if _, e := control.Exec(ctxFor(t), `ALTER TABLE agenteam_account.mail_attempts ADD CONSTRAINT fixture_hold_final CHECK(NOT terminal)`); e != nil {
		t.Fatal(e)
	}
	if e := p.FinishDelivery(ctxFor(t), tail, r.finish(t, tail, c.DeliveryMaterials{})); e == nil {
		t.Fatal("failed final checkpoint not exercised")
	}
	if _, e := control.Exec(ctxFor(t), `ALTER TABLE agenteam_account.mail_attempts DROP CONSTRAINT fixture_hold_final`); e != nil {
		t.Fatal(e)
	}
	// Restore a common scheduler pass only. All job/attempt/lease identities
	// and the tail's actual joined fence were made by the real public ports.
	if _, e := control.Exec(ctxFor(t), `UPDATE agenteam_account.mail_jobs SET pass=0`); e != nil {
		t.Fatal(e)
	}
	var tailPosition int
	if e := control.QueryRow(ctxFor(t), `SELECT n FROM (SELECT id,row_number() OVER(ORDER BY id) n FROM agenteam_account.mail_jobs) q WHERE id=$1`, tail.Details().JobID.String()).Scan(&tailPosition); e != nil || tailPosition != 101 {
		t.Fatal("tail fixture order", tailPosition, e)
	}
	// A different still-live process has no right to reclaim the first100.
	foreignID := id[c.Process](t)
	foreign := assembleAccount(t, f.store, f.store, f.authority, liveProcess{foreignID})
	recovery, _ := manualMailForService(t, foreign.service, foreignID)
	firstPass, e := recovery.RecoverDeliveries(ctxFor(t))
	if !hasCode(e, foundation.ResourceBusy) || firstPass.Examined != 100 || firstPass.Advanced != 0 {
		t.Fatal("protected first batch", firstPass, safeFailure(e))
	}
	secondPass, e := recovery.RecoverDeliveries(ctxFor(t))
	if !hasCode(e, foundation.ResourceBusy) || secondPass.Examined != 100 || secondPass.Advanced != 1 {
		t.Fatal("independent joined tail", secondPass, safeFailure(e))
	}
	var terminal, joined, protected int
	e = f.store.QueryRow(ctxFor(t), `SELECT count(*) FILTER(WHERE terminal),count(*) FILTER(WHERE io_joined),count(*) FILTER(WHERE NOT io_joined AND NOT terminal) FROM agenteam_account.mail_attempts`).Scan(&terminal, &joined, &protected)
	if e != nil || terminal != 1 || joined != 1 || protected != 100 {
		t.Fatal("false retirement", terminal, joined, protected, e)
	}
}

type mailProofAuthority struct{ id, bad c.ProcessID }

func (p mailProofAuthority) CurrentProcess() c.ProcessID { return p.id }
func (p mailProofAuthority) ConfirmStopped(ctx context.Context, id c.ProcessID) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if id == p.bad {
		return foundation.NewFault(foundation.InternalError, foundation.NotStarted)
	}
	return foundation.NewFault(foundation.ResourceBusy, foundation.NotStarted)
}
func TestAccountMailRecoveryPreservesHardErrorAndAdvancesIndependentWork(t *testing.T) {
	f := newB02Account(t)
	one := prepareMailJob(t, f)
	admin := mailCurrentAdmin(t, f)
	two, three := createMailTest(t, f, admin), createMailTest(t, f, admin)
	if _, e := f.service.ReconcileDeliveryIntents(ctxFor(t)); e != nil {
		t.Fatal(e)
	}
	p, r := newManualMail(t, f)
	a, e := p.ClaimDelivery(ctxFor(t), one)
	if e != nil {
		t.Fatal(e)
	}
	r.accept(a)
	otherID := id[c.Process](t)
	other := assembleAccount(t, f.store, f.store, f.authority, liveProcess{otherID})
	op, or := manualMailForService(t, other.service, otherID)
	b, e := op.ClaimDelivery(ctxFor(t), two)
	if e != nil {
		t.Fatal(e)
	}
	or.accept(b)
	z, e := p.ClaimDelivery(ctxFor(t), three)
	if e != nil {
		t.Fatal(e)
	}
	r.accept(z)
	control := f.db.Connect(t)
	if _, e = control.Exec(ctxFor(t), `ALTER TABLE agenteam_account.mail_attempts ADD CONSTRAINT fixture_hold_final CHECK(NOT terminal)`); e != nil {
		t.Fatal(e)
	}
	if e = p.FinishDelivery(ctxFor(t), z, r.finish(t, z, c.DeliveryMaterials{})); e == nil {
		t.Fatal("final failure absent")
	}
	if _, e = control.Exec(ctxFor(t), `ALTER TABLE agenteam_account.mail_attempts DROP CONSTRAINT fixture_hold_final`); e != nil {
		t.Fatal(e)
	}
	proof := mailProofAuthority{id[c.Process](t), otherID}
	recoverer := assembleAccount(t, f.store, f.store, f.authority, proof)
	rp, _ := manualMailForService(t, recoverer.service, proof.id)
	status, e := rp.RecoverDeliveries(ctxFor(t))
	if !hasCode(e, foundation.InternalError) || status.Examined != 3 || status.Advanced != 1 || status.Pending != 2 {
		t.Fatal("hard proof failure hidden or aborted whole batch", status, safeFailure(e))
	}
	var preserved int
	if e = f.store.QueryRow(ctxFor(t), `SELECT count(*) FROM agenteam_account.mail_attempts WHERE id IN($1,$2) AND NOT io_joined AND NOT terminal`, a.Details().AttemptID.String(), b.Details().AttemptID.String()).Scan(&preserved); e != nil || preserved != 2 {
		t.Fatal("unproved owners retired", preserved, e)
	}
}
