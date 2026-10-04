//go:build integration

package account_test

import (
	"context"
	c "github.com/LunaDeerTech/agenteam/internal/central/account/contract"
	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/outbox"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"sync/atomic"
	"testing"
	"time"
)

type enqueueRollback struct {
	oc.Handler
	calls   atomic.Int64
	reached chan struct{}
	release chan struct{}
}

func (h *enqueueRollback) HandleInTx(ctx context.Context, tx foundation.Tx, e event.Event, p oc.HandlerPlan) oc.Result {
	result := h.Handler.HandleInTx(ctx, tx, e, p)
	if h.calls.Add(1) == 1 {
		close(h.reached)
		select {
		case <-h.release:
		case <-ctx.Done():
		}
		return oc.Retry(oc.Unavailable)
	}
	return result
}
func TestAccountDeliveryHandlerRealOutboxTransactionRollbackAndCanonical(t *testing.T) {
	f := newB02Account(t)
	admin := b02Admin(t, f)
	// A pre-registration event is not retroactively delivered. Canonical scan
	// must independently converge this accepted intent to its one job.
	create := func(email string) c.InvitationReceipt {
		r, _ := c.NewInvitationCreate(c.InvitationCreateFields{Actor: admin, Key: foundation.IdempotencyKey(id[struct{}](t).String()), Email: email})
		v, e := f.service.CreateInvitation(ctxFor(t), r)
		if e != nil {
			t.Fatal(e, safeFailure(e))
		}
		return v
	}
	before := create("before-handler@example.com")
	def, e := f.service.MailHandler()
	if e != nil {
		t.Fatal(e)
	}
	h := &enqueueRollback{Handler: def.Handler, reached: make(chan struct{}), release: make(chan struct{})}
	def.Handler = h
	runtime, e := outbox.NewRuntime(f.events, []oc.HandlerDefinition{def}, outbox.Options{PollInterval: 5 * time.Millisecond, RetryBase: time.Millisecond})
	if e != nil {
		t.Fatal(e)
	}
	if e = runtime.Initialize(ctxFor(t)); e != nil {
		t.Fatal(e, safeFailure(e))
	}
	after := create("after-handler@example.com")
	worker, cancel := context.WithCancel(context.Background())
	defer cancel()
	released := false
	defer func() {
		if !released {
			close(h.release)
		}
		ctx, c := context.WithTimeout(context.Background(), time.Second)
		defer c()
		if e := runtime.Force(ctx); e != nil && !runtime.Joined() {
			t.Error(e)
		}
	}()
	if e = runtime.Start(worker); e != nil {
		t.Fatal(e)
	}
	select {
	case <-h.reached:
	case <-time.After(4 * time.Second):
		t.Fatal("real enqueue callback not reached")
	}
	var jobs, markers int
	if e = f.store.QueryRow(ctxFor(t), `SELECT (SELECT count(*) FROM agenteam_account.mail_jobs),(SELECT count(*) FROM agenteam_outbox.processed WHERE handler_id='account.mail-enqueue')`).Scan(&jobs, &markers); e != nil || jobs != 0 || markers != 0 {
		t.Fatal("uncommitted callback visible", jobs, markers, e)
	}
	close(h.release)
	released = true
	ctx, cancelWait := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelWait()
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	for {
		if e = f.store.QueryRow(ctx, `SELECT (SELECT count(*) FROM agenteam_account.mail_jobs WHERE id=$1),(SELECT count(*) FROM agenteam_outbox.processed WHERE handler_id='account.mail-enqueue')`, after.JobID.String()).Scan(&jobs, &markers); e != nil {
			t.Fatal(e)
		}
		if jobs == 1 && markers == 1 {
			break
		}
		select {
		case <-ticker.C:
		case <-ctx.Done():
			t.Fatal("enqueue retry did not atomically commit", jobs, markers, h.calls.Load())
		}
	}
	if h.calls.Load() != 2 {
		t.Fatal("unexpected callback attempts", h.calls.Load())
	}
	runtime.StopClaims()
	if e = runtime.Drain(ctxFor(t)); e != nil {
		t.Fatal(e)
	}
	if st, e := f.service.ReconcileDeliveryIntents(ctxFor(t)); e != nil || st.Advanced != 1 {
		t.Fatal("canonical pre-registration", st, e, safeFailure(e))
	}
	if st, e := f.service.ReconcileDeliveryIntents(ctxFor(t)); e != nil || st.Examined != 0 {
		t.Fatal("canonical replay", st, e)
	}
	var pending int
	if e = f.store.QueryRow(ctxFor(t), `SELECT count(*) FROM agenteam_account.mail_jobs WHERE id IN ($1,$2) AND phase='pending' AND attempts=0`, before.JobID.String(), after.JobID.String()).Scan(&pending); e != nil || pending != 2 {
		t.Fatal("enqueue claimed delivery", pending, e)
	}
}
func TestAccountInvitationExplicitResendVersionAndHistoricalReceipt(t *testing.T) {
	f := newB02Account(t)
	a := b02Admin(t, f)
	create, _ := c.NewInvitationCreate(c.InvitationCreateFields{Actor: a, Key: foundation.IdempotencyKey(id[struct{}](t).String()), Email: "resend-explicit@example.com"})
	first, e := f.service.CreateInvitation(ctxFor(t), create)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = f.store.Exec(ctxFor(t), `UPDATE agenteam_account.invitations SET last_delivery_at=clock_timestamp()-interval '61 seconds' WHERE id=$1`, first.ID.String()); e != nil {
		t.Fatal(e)
	}
	r := c.InvitationResend{Actor: a, Key: foundation.IdempotencyKey(id[struct{}](t).String()), ID: first.ID, ExpectedVersion: 2}
	if _, e = f.service.ResendInvitation(ctxFor(t), r); !hasCode(e, foundation.VersionConflict) {
		t.Fatal("version", e)
	}
	r.ExpectedVersion = 1
	sent, e := f.service.ResendInvitation(ctxFor(t), r)
	if e != nil || sent.ID != first.ID || sent.JobID == first.JobID {
		t.Fatal("resend", e, safeFailure(e))
	}
	if e = f.service.RevokeInvitation(ctxFor(t), c.InvitationRevoke{Actor: a, Key: foundation.IdempotencyKey(id[struct{}](t).String()), ID: first.ID, ExpectedVersion: 1}); e != nil {
		t.Fatal(e)
	}
	if got, e := f.service.ResendInvitation(ctxFor(t), r); e != nil || got != sent {
		t.Fatal("safe receipt after removal", e)
	}
	r.Key = foundation.IdempotencyKey(id[struct{}](t).String())
	if _, e = f.service.ResendInvitation(ctxFor(t), r); !hasCode(e, foundation.ResourceDeleted) {
		t.Fatal("late new resend", e)
	}
}
