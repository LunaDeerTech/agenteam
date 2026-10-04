//go:build integration

package account_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/account"
	c "github.com/LunaDeerTech/agenteam/internal/central/account/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
	"github.com/jackc/pgx/v5/pgconn"
)

func assertUnconfirmedState(t *testing.T, ctx context.Context, err error) {
	t.Helper()
	var fault *foundation.Fault
	if !errors.As(err, &fault) || fault == nil || fault.Code != foundation.CommitUnknown || fault.CommitState != foundation.Unknown {
		t.Errorf("original unknown was downgraded: %s", safeFailure(err))
	}
	var pg *pgconn.PgError
	if !errors.As(err, &pg) || pg == nil || pg.Code != "55P03" {
		t.Errorf("confirmation lost its actual lock-timeout cause: %s", safeFailure(err))
	}
	if ctx.Err() != nil {
		t.Error("fixture must exercise confirmation failure while caller is live", ctx.Err())
	}
	wire, e := json.Marshal(err)
	if e != nil {
		t.Fatal(e)
	}
	projection := fmt.Sprintf("%+v %#v", err, struct{ err error }{err}) + string(wire)
	if strings.Contains(projection, "lock timeout") || strings.Contains(projection, "agenteam_account") {
		t.Error("database diagnostic escaped the safe error projection")
	}
}

func TestAccountUnknownConfirmationRetainsState(t *testing.T) {
	queries := map[string]string{
		"planned":           `SELECT count(*) FROM agenteam_account.commands WHERE command_name='login' AND phase='planned'`,
		"login":             `SELECT count(*) FROM agenteam_account.commands WHERE command_name='login' AND phase='committed'`,
		"acquire":           `SELECT count(*) FROM agenteam_account.auth_attempts WHERE kind='response_read' AND phase='active'`,
		"bootstrap-created": `SELECT count(*) FROM agenteam_account.bootstrap WHERE log_state='eligible'`,
		"bootstrap-claimed": `SELECT count(*) FROM agenteam_account.bootstrap WHERE log_state='attempted'`,
		"logout":            `SELECT count(*) FROM agenteam_account.sessions WHERE revoked_at IS NOT NULL`,
	}
	for _, phase := range []string{"planned", "login", "acquire", "bootstrap-created", "bootstrap-claimed", "logout"} {
		for _, commit := range []bool{true, false} {
			name := phase + "/late_commit"
			if !commit {
				name = phase + "/rollback"
			}
			t.Run(name, func(t *testing.T) {
				f, wrapper, proxy := accountProxy(t, phase, commit)
				bootstrap := strings.HasPrefix(phase, "bootstrap-")
				var invoke func(context.Context) error
				if bootstrap {
					invoke = func(ctx context.Context) error { _, e := f.service.Bootstrap(ctx); return e }
				} else {
					request, _ := loginRequest(t, f, f.bootstrap(t), "admin@mail.com")
					if phase == "logout" {
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
						r := account.LogoutRequest{Actor: actor, Key: foundation.IdempotencyKey(id[struct{}](t).String())}
						invoke = func(ctx context.Context) error { return f.service.Logout(ctx, r) }
					} else {
						invoke = func(ctx context.Context) error {
							response, e := f.service.Login(ctx, request)
							called := false
							_ = response.UseCookie(func([]byte) error { called = true; return nil })
							if called || !response.Joined() {
								return errors.New("unconfirmed material escaped")
							}
							return e
						}
					}
				}
				wrapper.enabled.Store(true)
				ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
				defer cancel()
				done := make(chan error, 1)
				go func() { done <- invoke(ctx) }()
				await(t, proxy.reached)
				select {
				case e := <-done:
					assertUnconfirmedState(t, ctx, e)
				case <-time.After(4 * time.Second):
					t.Fatal("operation exceeded existing bounded call")
				}
				if !wrapper.fired.Load() {
					t.Fatal("actual COMMIT was not intercepted")
				}
				if bootstrap {
					info, e := os.Stat(f.log)
					if e != nil || info.Size() != 0 {
						t.Fatal("unconfirmed bootstrap material was printed", e)
					}
				}
				close(proxy.release)
				await(t, proxy.completed)
				var actual int
				want := 0
				if commit {
					want = 1
				}
				if e := f.store.QueryRow(ctxFor(t), queries[phase]).Scan(&actual); e != nil || actual != want {
					t.Fatal("actual original writer outcome differs", actual, want, e)
				}
				for range 2 {
					if _, e := f.service.Recover(ctxFor(t)); e != nil {
						t.Fatal(e, safeFailure(e))
					}
				}
				if bootstrap {
					info, e := os.Stat(f.log)
					if e != nil || info.Size() != 0 {
						t.Fatal("recovery reprinted unknown bootstrap material", e)
					}
				}
			})
		}
	}
}

// Only arm the unchanged real TCP proxy after the actual failed-login callback
// has written all of its facts. Transaction results and timing are not replaced.
type failedLoginCommitStore struct {
	*postgres.Store
	proxy          *commitProxy
	enabled, fired atomic.Bool
}

func (s *failedLoginCommitStore) WithinTx(ctx context.Context, cause foundation.TransactionCause, fn func(context.Context, foundation.Tx) error) foundation.CommitResult {
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
		if e = x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_account.commands WHERE command_name='login' AND phase='failed' AND result_code='UNAUTHENTICATED')`).Scan(&match); e != nil {
			return e
		}
		if match && s.fired.CompareAndSwap(false, true) {
			s.proxy.armed.Store(true)
		}
		return nil
	})
}

func TestAccountFailedLoginUnknownKeepsFailureFactsAtomic(t *testing.T) {
	for _, commit := range []bool{true, false} {
		name := "late_commit"
		if !commit {
			name = "rollback"
		}
		t.Run(name, func(t *testing.T) {
			db, _, _ := database(t)
			proxy := newCommitProxy(t, net.JoinHostPort("127.0.0.1", db.Fixture.Port), commit)
			u, e := url.Parse(db.Fixture.URL(db.Name))
			if e != nil {
				t.Fatal(e)
			}
			u.Host = proxy.listener.Addr().String()
			raw := openStore(t, db.Config(t, map[string]string{"URL": u.String(), "TLS_MODE": "disable"}))
			wrapper := &failedLoginCommitStore{Store: raw, proxy: proxy}
			keys, _, _ := keys(t)
			authority, e := account.NewAuthority(wrapper, keys)
			if e != nil {
				t.Fatal(e)
			}
			f := assembleAccount(t, raw, wrapper, authority, liveProcess{id[c.Process](t)})
			f.db = db
			_ = f.bootstrap(t)
			wrong, e := sc.NewSecretMaterial([]byte("owned wrong password for this test only"))
			if e != nil {
				t.Fatal(e)
			}
			defer wrong.Destroy()
			request, _ := loginRequest(t, f, wrong, "admin@mail.com")
			wrapper.enabled.Store(true)
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			type outcome struct {
				response account.LoginResponse
				err      error
			}
			done := make(chan outcome, 1)
			go func() { response, e := f.service.Login(ctx, request); done <- outcome{response, e} }()
			await(t, proxy.reached)
			var got outcome
			select {
			case got = <-done:
			case <-time.After(4 * time.Second):
				t.Fatal("failed login exceeded original bounded call")
			}
			assertUnconfirmedState(t, ctx, got.err)
			called := false
			_ = got.response.UseCookie(func([]byte) error { called = true; return nil })
			if called || !got.response.Joined() || !wrapper.fired.Load() {
				t.Fatal("failed-login actual fault or zero-material boundary missing")
			}
			observe := func(want int) {
				t.Helper()
				var commands, attempts, audits, subjects, ips, sessions, reads, leases int
				e := f.store.QueryRow(ctxFor(t), `SELECT
 (SELECT count(*) FROM agenteam_account.commands WHERE command_name='login' AND phase='failed' AND result_code='UNAUTHENTICATED'),
 (SELECT count(*) FROM agenteam_account.auth_attempts WHERE kind='login' AND outcome='denied' AND phase='completed'),
 (SELECT count(*) FROM agenteam_audit.audit_records WHERE action='account.login' AND outcome='denied'),
 (SELECT coalesce(sum(failures),0)::bigint FROM agenteam_account.auth_failures WHERE kind='subject'),
 (SELECT coalesce(sum(failures),0)::bigint FROM agenteam_account.auth_failures WHERE kind='ip'),
 (SELECT count(*) FROM agenteam_account.sessions),
 (SELECT count(*) FROM agenteam_account.auth_attempts WHERE kind='response_read'),
 (SELECT count(*) FROM agenteam_secret.secret_leases WHERE NOT released)`).Scan(&commands, &attempts, &audits, &subjects, &ips, &sessions, &reads, &leases)
				if e != nil {
					t.Fatal(e)
				}
				if commands != want || attempts != want || audits != want || subjects != want || ips != want || sessions != 0 || reads != 0 || leases != 0 {
					t.Fatal("failure facts split or duplicated", commands, attempts, audits, subjects, ips, sessions, reads, leases, "want", want)
				}
			}
			observe(0)
			close(proxy.release)
			await(t, proxy.completed)
			want := 0
			if commit {
				want = 1
			}
			observe(want)
			for range 2 {
				if _, e = f.service.Recover(ctxFor(t)); e != nil {
					t.Fatal(e, safeFailure(e))
				}
			}
			observe(want)
			_, e = f.service.Login(ctxFor(t), request)
			code := foundation.InvalidState
			if commit {
				code = foundation.Unauthenticated
			}
			if !hasCode(e, code) || hasCode(e, foundation.CommitUnknown) {
				t.Fatal("known historical failure became unknown or revived", e, safeFailure(e))
			}
			observe(want)
			fresh, _ := loginRequest(t, f, wrong, "admin@mail.com")
			_, e = f.service.Login(ctxFor(t), fresh)
			if !hasCode(e, foundation.Unauthenticated) || hasCode(e, foundation.CommitUnknown) {
				t.Fatal("ordinary committed credential failure became unknown", e, safeFailure(e))
			}
			observe(want + 1)
		})
	}
}
