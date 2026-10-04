//go:build integration

package account_test

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	c "github.com/LunaDeerTech/agenteam/internal/central/account/contract"
	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
)

type resetPrepareBarrier struct {
	oc.Appender
	armed, fired     atomic.Bool
	arrived, release chan struct{}
}

func (b *resetPrepareBarrier) PrepareAppend(ctx context.Context, a identity.Actor, e event.Event) (oc.AppendPlan, error) {
	p, err := b.Appender.PrepareAppend(ctx, a, e)
	if err != nil {
		return p, err
	}
	if b.armed.Load() && a.Details().ServiceName == identity.AccountAuth && b.fired.CompareAndSwap(false, true) {
		close(b.arrived)
		select {
		case <-b.release:
		case <-ctx.Done():
			return oc.AppendPlan{}, ctx.Err()
		}
	}
	return p, nil
}
func TestAccountResetPreparedBeforePasswordChangeCannotPublishOldVersion(t *testing.T) {
	db, raw, authority := database(t)
	var barrier *resetPrepareBarrier
	f := assembleB02(t, db, raw, raw, authority, func(p oc.Appender) oc.Appender {
		barrier = &resetPrepareBarrier{Appender: p, arrived: make(chan struct{}), release: make(chan struct{})}
		return barrier
	})
	old := f.bootstrap(t)
	admin, _ := b02Login(t, f, old)
	request := resetRequest(t, f, "admin@mail.com")
	if _, e := f.service.RequestPasswordReset(ctxFor(t), request); e != nil {
		t.Fatal(e)
	}
	if _, e := f.service.Recover(ctxFor(t)); e != nil {
		t.Fatal(e)
	}
	var reset string
	if e := f.store.QueryRow(ctxFor(t), `SELECT id::text FROM agenteam_account.password_resets`).Scan(&reset); e != nil {
		t.Fatal(e)
	}
	token := fixtureLinkToken(t, f, c.PasswordResetToken, reset)
	loser := testPassword(t, "Prepared stale reset phrase 162738!")
	resetRequest, e := c.NewResetComplete(c.ResetCompleteFields{Browser: request.Fields().Browser, Key: "stale-prepared-reset", Token: token, Password: loser, Confirmation: loser})
	if e != nil {
		t.Fatal(e)
	}
	barrier.armed.Store(true)
	done := make(chan error, 1)
	go func() { _, e := f.service.CompletePasswordReset(ctxFor(t), resetRequest); done <- e }()
	await(t, barrier.arrived)
	released := false
	defer func() {
		if !released {
			close(barrier.release)
		}
	}()
	winner := testPassword(t, "Actual current password phrase 918273!")
	change, _ := c.NewPasswordChange(c.PasswordChangeFields{Actor: admin, Key: "winning-password-change", ExpectedVersion: 1, OldPassword: old, Password: winner, Confirmation: winner})
	response, e := f.service.ChangePassword(ctxFor(t), change)
	if e != nil {
		t.Fatal(e, safeFailure(e))
	}
	if e = response.Close(ctxFor(t)); e != nil {
		t.Fatal(e)
	}
	close(barrier.release)
	released = true
	select {
	case e = <-done:
		if !hasCode(e, foundation.ResourceDeleted) {
			t.Fatal("stale reset published", e, safeFailure(e))
		}
	case <-time.After(4 * time.Second):
		t.Fatal("stale reset did not join")
	}
	var changes, resets, events int
	if e = f.store.QueryRow(ctxFor(t), `SELECT (SELECT count(*) FROM agenteam_account.commands WHERE command_name='password-change' AND phase='committed'),(SELECT count(*) FROM agenteam_account.commands WHERE command_name='reset-complete' AND phase='committed'),(SELECT count(*) FROM agenteam_outbox.events WHERE event_type='account.sessions-revoked')`).Scan(&changes, &resets, &events); e != nil || changes != 1 || resets != 0 || events != 1 {
		t.Fatal("stale side effects", changes, resets, events, e)
	}
	if _, e = f.service.Recover(ctxFor(t)); e != nil {
		t.Fatal(e, safeFailure(e))
	}
	b02Login(t, f, winner)
}

func TestAccountInvitationConcurrentUniquenessAndRevocation(t *testing.T) {
	for _, mode := range []string{"same_link", "same_username", "revoke"} {
		t.Run(mode, func(t *testing.T) {
			f := newB02Account(t)
			admin := b02Admin(t, f)
			create := func(email string) c.InvitationReceipt {
				r, _ := c.NewInvitationCreate(c.InvitationCreateFields{Actor: admin, Key: foundation.IdempotencyKey(id[struct{}](t).String()), Email: email})
				v, e := f.service.CreateInvitation(ctxFor(t), r)
				if e != nil {
					t.Fatal(e)
				}
				return v
			}
			first := create("compete-one@example.com")
			second := first
			if mode == "same_username" {
				second = create("compete-two@example.com")
			}
			browser, e := f.service.NewAnonymousContext(ctxFor(t))
			if e != nil {
				t.Fatal(e)
			}
			defer browser.Cookie.Destroy()
			defer browser.CSRF.Destroy()
			password := testPassword(t, "Concurrent invitation password phrase 64917!")
			requests := make([]c.InvitationRedeem, 2)
			for i, inv := range []c.InvitationReceipt{first, second} {
				token := fixtureLinkToken(t, f, c.InvitationToken, inv.ID.String())
				requests[i], e = c.NewInvitationRedeem(c.RedeemFields{Browser: browser.Identity, Key: foundation.IdempotencyKey(id[struct{}](t).String()), Token: token, Username: "same-unique-name", DisplayName: "同一用户", Password: password, Confirmation: password})
				if e != nil {
					t.Fatal(e)
				}
			}
			start := make(chan struct{})
			results := make(chan error, 2)
			var wg sync.WaitGroup
			for i, r := range requests {
				wg.Go(func() {
					<-start
					if mode == "revoke" && i == 1 {
						results <- f.service.RevokeInvitation(ctxFor(t), c.InvitationRevoke{Actor: admin, Key: "competing-revoke", ID: first.ID, ExpectedVersion: 1})
						return
					}
					_, e := f.service.RedeemInvitation(ctxFor(t), r)
					results <- e
				})
			}
			close(start)
			wg.Wait()
			close(results)
			wins := 0
			for e := range results {
				if e == nil {
					wins++
				} else if !hasCode(e, foundation.ResourceDeleted) && !hasCode(e, foundation.InvalidArgument) && !hasCode(e, foundation.ResourceBusy) {
					t.Fatal("unexpected race", e, safeFailure(e))
				}
			}
			if wins != 1 {
				t.Fatal("race winners", wins)
			}
			var users, redeemed, revoked int
			if e = f.store.QueryRow(ctxFor(t), `SELECT (SELECT count(*) FROM agenteam_account.users WHERE username='same-unique-name'),(SELECT count(*) FROM agenteam_audit.audit_records WHERE action='account.invite.redeem'),(SELECT count(*) FROM agenteam_audit.audit_records WHERE action='account.invite.revoke')`).Scan(&users, &redeemed, &revoked); e != nil || redeemed+revoked != 1 || users != redeemed {
				t.Fatal("race split state", users, redeemed, revoked, e)
			}
			if _, e = f.service.Recover(ctxFor(t)); e != nil {
				t.Fatal(e, safeFailure(e))
			}
		})
	}
}
