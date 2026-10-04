//go:build integration

package account_test

import (
	"context"
	"net"
	"net/url"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/account"
	c "github.com/LunaDeerTech/agenteam/internal/central/account/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
)

// Only arm the real TCP proxy. No transaction result or actual database
// callback is replaced; query the callback's own durable candidate facts.
type accountCommitStore struct {
	*postgres.Store
	proxy          *commitProxy
	enabled, fired atomic.Bool
	phase          string
}

func (s *accountCommitStore) WithinTx(ctx context.Context, cause foundation.TransactionCause, fn func(context.Context, foundation.Tx) error) foundation.CommitResult {
	return s.Store.WithinTx(ctx, cause, func(ctx context.Context, tx foundation.Tx) error {
		if e := fn(ctx, tx); e != nil {
			return e
		}
		if !s.enabled.Load() || s.fired.Load() {
			return nil
		}
		x, e := s.InTx(tx)
		if e != nil {
			return e
		}
		var match bool
		switch s.phase {
		case "planned":
			e = x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_account.commands WHERE command_name='login' AND phase='planned')`).Scan(&match)
		case "response-plan":
			e = x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_account.response_plans)`).Scan(&match)
		case "login":
			e = x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_account.commands WHERE command_name='login' AND phase='committed')`).Scan(&match)
		case "acquire":
			e = x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_account.auth_attempts WHERE kind='response_read' AND phase='active')`).Scan(&match)
		case "read":
			e = x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_audit.audit_records WHERE action='secret.resolve')`).Scan(&match)
		case "logout":
			e = x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_account.commands WHERE command_name='logout' AND phase='committed')`).Scan(&match)
		case "bootstrap-created":
			e = x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_account.bootstrap WHERE log_state='eligible')`).Scan(&match)
		case "bootstrap-claimed":
			e = x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_account.bootstrap WHERE log_state='attempted')`).Scan(&match)
		}
		if e != nil {
			return e
		}
		if match && s.fired.CompareAndSwap(false, true) {
			s.proxy.armed.Store(true)
		}
		return nil
	})
}

func TestAccountLogoutUnknownKeepsEventAuditAndRevocationAtomic(t *testing.T) {
	for _, commit := range []bool{true, false} {
		name := "commit"
		if !commit {
			name = "rollback"
		}
		t.Run(name, func(t *testing.T) {
			f, wrapped, p := accountProxy(t, "logout", commit)
			password := f.bootstrap(t)
			request, _ := loginRequest(t, f, password, "admin@mail.com")
			response, e := f.service.Login(ctxFor(t), request)
			if e != nil {
				t.Fatal(e)
			}
			cookie := useCookie(t, response)
			if e = response.Close(ctxFor(t)); e != nil {
				t.Fatal(e)
			}
			actor, e := f.service.Authenticate(ctxFor(t), cookie)
			if e != nil {
				t.Fatal(e)
			}
			logout := account.LogoutRequest{Actor: actor, Key: foundation.IdempotencyKey(id[struct{}](t).String())}
			wrapped.enabled.Store(true)
			done := make(chan error, 1)
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			go func() { done <- f.service.Logout(ctx, logout) }()
			await(t, p.reached)
			e = <-done
			if e == nil {
				t.Fatal("unknown logout reported success")
			}
			var events, revoked int
			if e = f.store.QueryRow(ctxFor(t), `SELECT (SELECT count(*) FROM agenteam_outbox.events),(SELECT count(*) FROM agenteam_account.sessions WHERE revoked_at IS NOT NULL)`).Scan(&events, &revoked); e != nil || events != 0 || revoked != 0 {
				t.Fatal("held facts visible", events, revoked, e)
			}
			close(p.release)
			await(t, p.completed)
			e = f.service.Logout(ctxFor(t), logout)
			if commit {
				if !hasCode(e, foundation.SessionRevoked) {
					t.Fatal(e)
				}
			} else if e != nil {
				t.Fatal(e, safeFailure(e))
			}
			var audits int
			if e = f.store.QueryRow(ctxFor(t), `SELECT (SELECT count(*) FROM agenteam_outbox.events),(SELECT count(*) FROM agenteam_account.sessions WHERE revoked_at IS NOT NULL),(SELECT count(*) FROM agenteam_audit.audit_records WHERE action='account.logout')`).Scan(&events, &revoked, &audits); e != nil || events != 1 || revoked != 1 || audits != 1 {
				t.Fatal("logout facts split/repeated", events, revoked, audits, e)
			}
		})
	}
}

func TestAccountBootstrapUnknownDoesNotReprint(t *testing.T) {
	for _, phase := range []string{"bootstrap-created", "bootstrap-claimed"} {
		for _, commit := range []bool{true, false} {
			name := phase + "/commit"
			if !commit {
				name = phase + "/rollback"
			}
			t.Run(name, func(t *testing.T) {
				f, wrapper, p := accountProxy(t, phase, commit)
				wrapper.enabled.Store(true)
				done := make(chan error, 1)
				ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
				defer cancel()
				go func() { _, e := f.service.Bootstrap(ctx); done <- e }()
				await(t, p.reached)
				if e := <-done; e == nil {
					t.Fatal("unconfirmed bootstrap reported success")
				}
				info, e := os.Stat(f.log)
				if e != nil || info.Size() != 0 {
					t.Fatal("password emitted before confirmation")
				}
				close(p.release)
				await(t, p.completed)
				if _, e = f.service.Recover(ctxFor(t)); e != nil {
					t.Fatal(e, safeFailure(e))
				}
				info, e = os.Stat(f.log)
				if e != nil || info.Size() != 0 {
					t.Fatal("old bootstrap password reprinted")
				}
				var users int
				if e = f.store.QueryRow(ctxFor(t), `SELECT count(*) FROM agenteam_account.users`).Scan(&users); e != nil {
					t.Fatal(e)
				}
				want := 1
				if phase == "bootstrap-created" && !commit {
					want = 0
				}
				if users != want {
					t.Fatal("unknown bootstrap duplicated or lost user", users)
				}
			})
		}
	}
}
func accountProxy(t *testing.T, phase string, commit bool) (*fixture, *accountCommitStore, *commitProxy) {
	t.Helper()
	db, _, _ := database(t)
	p := newCommitProxy(t, net.JoinHostPort("127.0.0.1", db.Fixture.Port), commit)
	u, _ := url.Parse(db.Fixture.URL(db.Name))
	u.Host = p.listener.Addr().String()
	raw := openStore(t, db.Config(t, map[string]string{"URL": u.String(), "TLS_MODE": "disable"}))
	wrapped := &accountCommitStore{Store: raw, proxy: p, phase: phase}
	keys, _, _ := keys(t)
	a, e := account.NewAuthority(wrapped, keys)
	if e != nil {
		t.Fatal(e)
	}
	f := assembleAccount(t, raw, wrapped, a, liveProcess{id[c.Process](t)})
	f.db = db
	return f, wrapped, p
}
func await(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(4 * time.Second):
		t.Fatal("real commit barrier timeout")
	}
}
func TestAccountUnknownNeverExposesUnconfirmedLoginMaterial(t *testing.T) {
	for _, phase := range []string{"planned", "login", "response-plan", "acquire", "read"} {
		for _, commit := range []bool{true, false} {
			name := phase + "/commit"
			if !commit {
				name = phase + "/rollback"
			}
			t.Run(name, func(t *testing.T) {
				f, wrapper, p := accountProxy(t, phase, commit)
				password := f.bootstrap(t)
				r, _ := loginRequest(t, f, password, "admin@mail.com")
				wrapper.enabled.Store(true)
				type outcome struct {
					response account.LoginResponse
					err      error
				}
				done := make(chan outcome, 1)
				ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
				defer cancel()
				go func() { v, e := f.service.Login(ctx, r); done <- outcome{v, e} }()
				await(t, p.reached)
				result := <-done
				if result.err == nil || !result.response.Joined() {
					t.Fatal("unconfirmed response escaped", result.err)
				}
				if !wrapper.fired.Load() {
					t.Fatal("fault was not armed")
				}
				var active, sessions int
				if e := f.store.QueryRow(ctxFor(t), `SELECT (SELECT count(*) FROM agenteam_account.sessions),(SELECT count(*) FROM agenteam_secret.secret_leases WHERE NOT released)`).Scan(&sessions, &active); e != nil {
					t.Fatal(e)
				}
				if (phase == "planned" || phase == "login") && sessions != 0 || (phase == "response-plan" || phase == "acquire") && active != 0 {
					t.Fatal("held transaction visible", sessions, active)
				}
				close(p.release)
				await(t, p.completed)
				// The still-serving sink worker intentionally makes Service.Joined
				// false. Observe this attempt's durable lease convergence instead.
				recoveryCtx, recoveryCancel := context.WithTimeout(context.Background(), 3*time.Second)
				defer recoveryCancel()
				tick := time.NewTicker(5 * time.Millisecond)
				defer tick.Stop()
				for {
					if _, e := f.service.Recover(recoveryCtx); e != nil {
						t.Fatal(e, safeFailure(e))
					}
					if e := f.store.QueryRow(recoveryCtx, `SELECT count(*) FROM agenteam_secret.secret_leases WHERE NOT released`).Scan(&active); e != nil {
						t.Fatal(e)
					}
					if active == 0 {
						break
					}
					select {
					case <-tick.C:
					case <-recoveryCtx.Done():
						t.Fatal("orphaned unknown read lease", active)
					}
				}
				fresh, e := f.service.Login(ctxFor(t), r)
				if phase == "login" && !commit || phase == "planned" && commit {
					if !hasCode(e, foundation.InvalidState) {
						t.Fatal("rolled-back planned login was revived", e)
					}
				} else {
					if e != nil {
						t.Fatal(e, safeFailure(e))
					}
					if e = fresh.Close(ctxFor(t)); e != nil {
						t.Fatal(e)
					}
				}
				if e = f.store.QueryRow(ctxFor(t), `SELECT count(*) FROM agenteam_account.sessions`).Scan(&sessions); e != nil {
					t.Fatal(e)
				}
				want := 1
				if phase == "login" && !commit || phase == "planned" && commit {
					want = 0
				}
				if sessions != want {
					t.Fatal("duplicate or missing session", sessions, want)
				}
			})
		}
	}
}
