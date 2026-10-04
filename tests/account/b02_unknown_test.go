//go:build integration

package account_test

import (
	"context"
	"errors"
	"github.com/LunaDeerTech/agenteam/internal/central/account"
	c "github.com/LunaDeerTech/agenteam/internal/central/account/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"net"
	"net/url"
	"sync/atomic"
	"testing"
	"time"
)

type b02CommitStore struct {
	*postgres.Store
	proxy          *commitProxy
	enabled, fired atomic.Bool
	name, phase    string
}

func (w *b02CommitStore) WithinTx(ctx context.Context, cause foundation.TransactionCause, fn func(context.Context, foundation.Tx) error) foundation.CommitResult {
	return w.Store.WithinTx(ctx, cause, func(ctx context.Context, tx foundation.Tx) error {
		if e := fn(ctx, tx); e != nil {
			return e
		}
		if !w.enabled.Load() || w.fired.Load() {
			return nil
		}
		x, e := w.InTx(tx)
		if e != nil {
			return e
		}
		var exists bool
		if e = x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_account.commands WHERE command_name=$1 AND phase=$2)`, w.name, w.phase).Scan(&exists); e != nil {
			return e
		}
		if exists && w.fired.CompareAndSwap(false, true) {
			w.proxy.armed.Store(true)
		}
		return nil
	})
}
func newB02Proxy(t *testing.T, name, phase string, commit bool) (*b02Fixture, *b02CommitStore, *commitProxy) {
	t.Helper()
	db, _, _ := database(t)
	p := newCommitProxy(t, net.JoinHostPort("127.0.0.1", db.Fixture.Port), commit)
	u, e := url.Parse(db.Fixture.URL(db.Name))
	if e != nil {
		t.Fatal(e)
	}
	u.Host = p.listener.Addr().String()
	raw := openStore(t, db.Config(t, map[string]string{"URL": u.String(), "TLS_MODE": "disable"}))
	w := &b02CommitStore{Store: raw, proxy: p, name: name, phase: phase}
	k, _, _ := keys(t)
	a, e := account.NewAuthority(w, k)
	if e != nil {
		t.Fatal(e)
	}
	return assembleB02(t, db, raw, w, a, nil), w, p
}
func TestAccountB02MutationUnknownKeepsOriginalStateAndFacts(t *testing.T) {
	for _, name := range []string{"invite-create", "reset-complete", "password-change"} {
		for _, commit := range []bool{true, false} {
			suffix := "late_commit"
			if !commit {
				suffix = "rollback"
			}
			t.Run(name+"/"+suffix, func(t *testing.T) {
				f, w, p := newB02Proxy(t, name, "committed", commit)
				old := f.bootstrap(t)
				actor, _ := b02Login(t, f, old)
				newPassword := testPassword(t, "A new actual unknown-boundary phrase 75319!")
				var invoke func(context.Context) error
				var retry func(context.Context) error
				switch name {
				case "invite-create":
					r, _ := c.NewInvitationCreate(c.InvitationCreateFields{Actor: actor, Key: foundation.IdempotencyKey(id[struct{}](t).String()), Email: "unknown-invite@example.com"})
					invoke = func(ctx context.Context) error { _, e := f.service.CreateInvitation(ctx, r); return e }
					retry = invoke
				case "reset-complete":
					request := resetRequest(t, f, "admin@mail.com")
					if _, e := f.service.RequestPasswordReset(ctxFor(t), request); e != nil {
						t.Fatal(e)
					}
					if _, e := f.service.Recover(ctxFor(t)); e != nil {
						t.Fatal(e)
					}
					var id string
					if e := f.store.QueryRow(ctxFor(t), `SELECT id::text FROM agenteam_account.password_resets`).Scan(&id); e != nil {
						t.Fatal(e)
					}
					token := fixtureLinkToken(t, f, c.PasswordResetToken, id)
					r, e := c.NewResetComplete(c.ResetCompleteFields{Browser: request.Fields().Browser, Key: foundation.IdempotencyKey("complete-unknown"), Token: token, Password: newPassword, Confirmation: newPassword})
					if e != nil {
						t.Fatal(e)
					}
					invoke = func(ctx context.Context) error { _, e := f.service.CompletePasswordReset(ctx, r); return e }
					retry = invoke
				case "password-change":
					r, _ := c.NewPasswordChange(c.PasswordChangeFields{Actor: actor, Key: foundation.IdempotencyKey("change-unknown"), ExpectedVersion: 1, OldPassword: old, Password: newPassword, Confirmation: newPassword})
					invoke = func(ctx context.Context) error {
						response, e := f.service.ChangePassword(ctx, r)
						exposed := false
						_ = response.UseCookie(func([]byte) error { exposed = true; return nil })
						if e == nil {
							_ = response.Close(ctxFor(t))
						}
						if exposed {
							return errors.New("unconfirmed password response exposed")
						}
						return e
					}
					retry = func(ctx context.Context) error {
						response, e := f.service.ChangePassword(ctx, r)
						if e == nil {
							e = response.Close(ctxFor(t))
						}
						return e
					}
				}
				w.enabled.Store(true)
				ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
				defer cancel()
				done := make(chan error, 1)
				go func() { done <- invoke(ctx) }()
				await(t, p.reached)
				select {
				case e := <-done:
					assertUnconfirmedState(t, ctx, e)
				case <-time.After(4 * time.Second):
					t.Fatal("bounded mutation did not return")
				}
				if !w.fired.Load() {
					t.Fatal("COMMIT not intercepted")
				}
				var visible int
				if e := f.store.QueryRow(ctxFor(t), `SELECT count(*) FROM agenteam_account.commands WHERE command_name=$1 AND phase='committed'`, name).Scan(&visible); e != nil || visible != 0 {
					t.Fatal("uncommitted business visible", visible, e)
				}
				close(p.release)
				await(t, p.completed)
				var actual int
				if e := f.store.QueryRow(ctxFor(t), `SELECT count(*) FROM agenteam_account.commands WHERE command_name=$1 AND phase='committed'`, name).Scan(&actual); e != nil {
					t.Fatal(e)
				}
				want := 0
				if commit {
					want = 1
				}
				if actual != want {
					t.Fatal("original terminal fact", actual, want)
				}
				e := retry(ctxFor(t))
				if name == "password-change" && commit {
					if !hasCode(e, foundation.SessionRevoked) {
						t.Fatal("old session replay", e)
					}
				} else if e != nil {
					t.Fatal("safe retry", e, safeFailure(e))
				}
				var commands, audits, events int
				action := map[string]string{"invite-create": "account.invite.create", "reset-complete": "account.password.reset.complete", "password-change": "account.password.change"}[name]
				eventType := "account.sessions-revoked"
				if name == "invite-create" {
					eventType = "account.delivery-requested"
				}
				if e = f.store.QueryRow(ctxFor(t), `SELECT (SELECT count(*) FROM agenteam_account.commands WHERE command_name=$1 AND phase='committed'),(SELECT count(*) FROM agenteam_audit.audit_records WHERE action=$2),(SELECT count(*) FROM agenteam_outbox.events WHERE event_type=$3)`, name, action, eventType).Scan(&commands, &audits, &events); e != nil || commands != 1 || audits != 1 || events != 1 {
					t.Fatal("split or duplicate effects", commands, audits, events, e)
				}
				if name != "invite-create" {
					b02Login(t, f, newPassword)
				}
				if _, e = f.service.Recover(ctxFor(t)); e != nil {
					t.Fatal("post-terminal recovery", e, safeFailure(e))
				}
			})
		}
	}
}

func TestAccountB02PlannedUnknownRecoveryWaitsOriginalWriter(t *testing.T) {
	for _, commit := range []bool{true, false} {
		name := "late_commit"
		if !commit {
			name = "rollback"
		}
		t.Run(name, func(t *testing.T) {
			f, w, p := newB02Proxy(t, "invite-create", "planned", commit)
			a := b02Admin(t, f)
			r, _ := c.NewInvitationCreate(c.InvitationCreateFields{Actor: a, Key: "unknown-planning", Email: "planned-unknown@example.com"})
			w.enabled.Store(true)
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			done := make(chan error, 1)
			go func() { _, e := f.service.CreateInvitation(ctx, r); done <- e }()
			await(t, p.reached)
			select {
			case e := <-done:
				assertUnconfirmedState(t, ctx, e)
			case <-time.After(4 * time.Second):
				t.Fatal("planning did not return")
			}
			// Request has actually joined, but the original COMMIT still owns the
			// command writer. Absence is not permission to discard local provenance.
			check, stop := context.WithTimeout(context.Background(), 200*time.Millisecond)
			_, e := f.service.Recover(check)
			stop()
			if e == nil || !errors.Is(e, context.DeadlineExceeded) {
				t.Fatal("unknown writer not serialized", e, safeFailure(e))
			}
			close(p.release)
			await(t, p.completed)
			for range 2 {
				if _, e = f.service.Recover(ctxFor(t)); e != nil {
					t.Fatal("joined planning recovery", e, safeFailure(e))
				}
			}
			var planned, cancelled, links int
			if e = f.store.QueryRow(ctxFor(t), `SELECT count(*) FILTER(WHERE phase='planned'),count(*) FILTER(WHERE phase='cancelled'),(SELECT count(*) FROM agenteam_account.invitations) FROM agenteam_account.commands WHERE command_name='invite-create'`).Scan(&planned, &cancelled, &links); e != nil || planned != 0 || links != 0 {
				t.Fatal("orphan planning", planned, cancelled, links, e)
			}
			if commit {
				if cancelled != 1 {
					t.Fatal("late plan not retired")
				}
				if _, e = f.service.CreateInvitation(ctxFor(t), r); !hasCode(e, foundation.InvalidState) {
					t.Fatal("retired command revived", e)
				}
			} else {
				if cancelled != 0 {
					t.Fatal("rolled back plan fabricated")
				}
				if _, e = f.service.CreateInvitation(ctxFor(t), r); e != nil {
					t.Fatal("confirmed absent command could not retry", e, safeFailure(e))
				}
			}
		})
	}
}
