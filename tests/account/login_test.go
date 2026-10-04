//go:build integration

package account_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5/pgconn"
	"net/netip"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/account"
	c "github.com/LunaDeerTech/agenteam/internal/central/account/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/audit"
	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/outbox"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/LunaDeerTech/agenteam/internal/central/recoverylog"
	"github.com/LunaDeerTech/agenteam/internal/central/secret"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
	pgfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/postgres"
)

type liveProcess struct{ id c.ProcessID }

func (p liveProcess) CurrentProcess() c.ProcessID { return p.id }
func (p liveProcess) ConfirmStopped(context.Context, c.ProcessID) error {
	return foundation.NewFault(foundation.ResourceBusy, foundation.NotStarted)
}

type fixture struct {
	db        *pgfixture.Database
	service   *account.Service
	store     *postgres.Store
	authority *account.Authority
	secrets   *secret.Service
	log       string
}

func newAccount(t *testing.T) *fixture {
	t.Helper()
	db, store, authority := database(t)
	f := assembleAccount(t, store, store, authority, liveProcess{id[c.Process](t)})
	f.db = db
	return f
}

type accountTestStore interface {
	account.Store
	Acquire(context.Context, foundation.Tx, foundation.LockKey, foundation.LockMode) error
}

func assembleAccount(t *testing.T, raw *postgres.Store, store accountTestStore, authority *account.Authority, process c.ProcessAuthority, transforms ...func(ac.Appender) ac.Appender) *fixture {
	t.Helper()
	_, cursor, master := keys(t)
	aud, e := audit.New(store, cursor, audit.Authorizations{Sessions: authority, System: authority, Accounts: authority})
	if e != nil {
		t.Fatal(e)
	}
	var appender ac.Appender = aud
	for _, transform := range transforms {
		appender = transform(appender)
	}
	secrets, e := secret.New(store, master, appender, secret.Authorizations{Sessions: authority, System: authority, Usage: authority, AccountWrites: authority})
	if e != nil {
		t.Fatal(e)
	}
	if e = secrets.Initialize(ctxFor(t)); e != nil {
		t.Fatal(e)
	}
	dir := t.TempDir()
	if e = os.Chmod(dir, 0700); e != nil {
		t.Fatal(e)
	}
	path := filepath.Join(dir, "recovery.jsonl")
	sink, e := recoverylog.Open(path)
	if e != nil {
		t.Fatal(e)
	}
	catalog := event.NewCatalog()
	revoked, e := c.DefineSessionsRevoked(catalog)
	if e != nil {
		t.Fatal(e)
	}
	events, e := outbox.New(store, catalog, outbox.Authorizations{Producers: map[event.StableName]oc.ProducerAuthority{c.AccountProducer: authority}, Sessions: authority, System: authority, Audit: appender, Cursors: cursor})
	if e != nil {
		t.Fatal(e)
	}
	service, e := account.New(account.Dependencies{Authority: authority, Audit: appender, Secrets: secrets, Events: events, SessionsRevoked: revoked, Processes: process, RecoveryLog: sink})
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if e := service.Force(ctx); e != nil && !service.Joined() {
			t.Error(e)
		}
	})
	return &fixture{service: service, store: raw, authority: authority, secrets: secrets, log: path}
}
func (f *fixture) bootstrap(t *testing.T) sc.SecretMaterial {
	t.Helper()
	got, e := f.service.Bootstrap(ctxFor(t))
	if e != nil || !got.Created || got.LogState != "written" {
		t.Fatalf("bootstrap: %+v %v [%s]", got, e, safeFailure(e))
	}
	b, e := os.ReadFile(f.log)
	if e != nil {
		t.Fatal(e)
	}
	var row struct {
		Password string `json:"initial_password"`
	}
	if e = json.Unmarshal(b, &row); e != nil || len(row.Password) != 24 {
		t.Fatal("private recovery record", e)
	}
	m, e := sc.NewSecretMaterial([]byte(row.Password))
	clear(b)
	row.Password = ""
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(m.Destroy)
	return m
}
func loginRequest(t *testing.T, f *fixture, password sc.SecretMaterial, email string) (c.LoginRequest, account.AnonymousContext) {
	t.Helper()
	anonymous, e := f.service.NewAnonymousContext(ctxFor(t))
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(anonymous.Cookie.Destroy)
	t.Cleanup(anonymous.CSRF.Destroy)
	verified, e := f.service.VerifyAnonymousContext(ctxFor(t), anonymous.Cookie, anonymous.CSRF)
	if e != nil {
		t.Fatal(e)
	}
	r, e := c.NewLoginRequest(c.LoginFields{Browser: verified, Key: foundation.IdempotencyKey(id[struct{}](t).String()), Email: email, Password: password, ClientIP: netip.MustParseAddr("192.0.2.7")})
	if e != nil {
		t.Fatal(e)
	}
	return r, anonymous
}
func useCookie(t *testing.T, r account.LoginResponse) sc.SecretMaterial {
	t.Helper()
	var m sc.SecretMaterial
	if e := r.UseCookie(func(b []byte) error { var e error; m, e = sc.NewSecretMaterial(b); return e }); e != nil {
		t.Fatal(e)
	}
	t.Cleanup(m.Destroy)
	return m
}
func TestAccountBootstrapLoginReplayAndIndependentResponseLeases(t *testing.T) {
	f := newAccount(t)
	password := f.bootstrap(t)
	before, e := os.ReadFile(f.log)
	if e != nil {
		t.Fatal(e)
	}
	again, e := f.service.Bootstrap(ctxFor(t))
	if e != nil || again.Created {
		t.Fatal("bootstrap replay", e)
	}
	after, e := os.ReadFile(f.log)
	if e != nil || string(before) != string(after) {
		t.Fatal("bootstrap reprinted")
	}
	clear(before)
	clear(after)
	r, _ := loginRequest(t, f, password, "ADMIN@mail.com")
	first, e := f.service.Login(ctxFor(t), r)
	if e != nil {
		var phase string
		var planned, response bool
		_ = f.store.QueryRow(ctxFor(t), `SELECT phase,planned_secret_ref IS NOT NULL,response_secret_ref IS NOT NULL FROM agenteam_account.commands LIMIT 1`).Scan(&phase, &planned, &response)
		t.Fatalf("login %v %s phase=%s prepared=%v response=%v", e, safeFailure(e), phase, planned, response)
	}
	defer first.Close(ctxFor(t))
	second, e := f.service.Login(ctxFor(t), r)
	if e != nil {
		t.Fatal("replay", e)
	}
	defer second.Close(ctxFor(t))
	if first.AttemptID() == second.AttemptID() {
		t.Fatal("shared response attempt")
	}
	cookie := useCookie(t, first)
	other := useCookie(t, second)
	var same bool
	if e = cookie.Use(func(a []byte) error {
		return other.Use(func(b []byte) error { same = string(a) == string(b); return nil })
	}); e != nil || !same {
		t.Fatal("replay issued another cookie", e)
	}
	actor, e := f.service.Authenticate(ctxFor(t), cookie)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = f.authority.AuthorizeSystem(ctxFor(t), foundation.Tx{}, actor, "mutate"); e != nil {
		t.Fatal(e)
	}
	view, e := f.service.GetSession(ctxFor(t), cookie)
	if e != nil {
		t.Fatal(e)
	}
	defer view.CSRF.Destroy()
	if _, e = f.service.ValidateSessionCSRF(ctxFor(t), cookie, view.CSRF); e != nil {
		t.Fatal(e)
	}
	var sessions, active, loginAudit int
	if e = f.store.QueryRow(ctxFor(t), `SELECT (SELECT count(*) FROM agenteam_account.sessions),(SELECT count(*) FROM agenteam_secret.secret_leases WHERE owner_kind='account_response' AND NOT released),(SELECT count(*) FROM agenteam_audit.audit_records WHERE action='account.login')`).Scan(&sessions, &active, &loginAudit); e != nil || sessions != 1 || active != 2 || loginAudit != 1 {
		t.Fatalf("facts=%d/%d/%d %v", sessions, active, loginAudit, e)
	}
	if e = first.Close(ctxFor(t)); e != nil {
		t.Fatal(e)
	}
	if e = f.store.QueryRow(ctxFor(t), `SELECT count(*) FROM agenteam_secret.secret_leases WHERE owner_kind='account_response' AND NOT released`).Scan(&active); e != nil || active != 1 {
		t.Fatal("one release touched another", active, e)
	}
	if e = second.UseCookie(func([]byte) error { return nil }); e != nil {
		t.Fatal("other response stopped", e)
	}
	if e = second.Close(ctxFor(t)); e != nil {
		t.Fatal(e)
	}
	changed := r.Fields()
	changed.Password, _ = sc.NewSecretMaterial([]byte("A different long password"))
	defer changed.Password.Destroy()
	different, _ := c.NewLoginRequest(changed)
	if _, e = f.service.Login(ctxFor(t), different); !hasCode(e, foundation.IdempotencyKeyReused) {
		t.Fatal("semantic replay", e)
	}
}
func TestAccountBootstrapConcurrentSingleUserAndOutput(t *testing.T) {
	f := newAccount(t)
	var group sync.WaitGroup
	results := make(chan account.BootstrapStatus, 2)
	errs := make(chan error, 2)
	for range 2 {
		group.Go(func() { v, e := f.service.Bootstrap(ctxFor(t)); results <- v; errs <- e })
	}
	group.Wait()
	close(results)
	close(errs)
	for e := range errs {
		if e != nil {
			t.Fatal(e)
		}
	}
	created := 0
	for v := range results {
		if v.Created {
			created++
		}
	}
	if created != 1 {
		t.Fatal("creators", created)
	}
	var n int
	if e := f.store.QueryRow(ctxFor(t), `SELECT count(*) FROM agenteam_account.users`).Scan(&n); e != nil || n != 1 {
		t.Fatal(n, e)
	}
}

func safeFailure(e error) string {
	var p *postgres.Error
	var sql *pgconn.PgError
	var code, state, constraint string
	if errors.As(e, &p) {
		code = string(p.Code())
		state = p.SQLState()
	}
	if errors.As(e, &sql) {
		constraint = sql.ConstraintName
	}
	chain := ""
	for x := e; x != nil; x = errors.Unwrap(x) {
		var text string
		switch v := x.(type) {
		case *foundation.Fault:
			text = string(v.Code)
		case *secret.Error:
			text = string(v.Code())
		case *postgres.Error:
			text = string(v.Code())
		default:
			text = fmt.Sprintf("%T", x)
		}
		chain += text + "/"
	}
	return fmt.Sprintf("postgres=%s sqlstate=%s constraint=%s chain=%s", code, state, constraint, chain)
}
