//go:build integration

package account_test

import (
	"context"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/account"
	c "github.com/LunaDeerTech/agenteam/internal/central/account/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
)

func TestAccountLogoutAuditEventAndCurrentRevocationAtomic(t *testing.T) {
	f := newAccount(t)
	password := f.bootstrap(t)
	request, _ := loginRequest(t, f, password, "admin@mail.com")
	r, e := f.service.Login(ctxFor(t), request)
	if e != nil {
		t.Fatal(e, safeFailure(e))
	}
	cookie := useCookie(t, r)
	if e = r.Close(ctxFor(t)); e != nil {
		t.Fatal(e)
	}
	actor, e := f.service.Authenticate(ctxFor(t), cookie)
	if e != nil {
		t.Fatal(e)
	}
	logout := account.LogoutRequest{Actor: actor, Key: foundation.IdempotencyKey(id[struct{}](t).String())}
	if e = f.service.Logout(ctxFor(t), logout); e != nil {
		t.Fatal(e, safeFailure(e))
	}
	if _, e = f.service.Authenticate(ctxFor(t), cookie); !hasCode(e, foundation.SessionRevoked) {
		t.Fatal("cookie current after logout", e)
	}
	if e = f.service.Logout(ctxFor(t), logout); !hasCode(e, foundation.SessionRevoked) {
		t.Fatal("revoked logout replay", e)
	}
	if _, e = f.service.LookupLogin(ctxFor(t), request); !hasCode(e, foundation.SessionRevoked) {
		t.Fatal("login replay revived session", e)
	}
	var audits, events, seq, deliveries int64
	e = f.store.QueryRow(ctxFor(t), `SELECT (SELECT count(*) FROM agenteam_audit.audit_records WHERE action='account.logout'),(SELECT count(*) FROM agenteam_outbox.events WHERE event_type='account.sessions-revoked'),(SELECT auth_sequence FROM agenteam_account.users WHERE id=$1),(SELECT count(*) FROM agenteam_outbox.deliveries)`, actor.Details().UserID).Scan(&audits, &events, &seq, &deliveries)
	if e != nil || audits != 1 || events != 1 || seq != 2 || deliveries != 0 {
		t.Fatalf("atomic facts %d/%d/%d/%d %v", audits, events, seq, deliveries, e)
	}
	var scope string
	var version, sequence int64
	e = f.store.QueryRow(ctxFor(t), `SELECT scope,aggregate_version,aggregate_sequence FROM agenteam_outbox.events WHERE event_type='account.sessions-revoked'`).Scan(&scope, &version, &sequence)
	if e != nil || scope != "system" || version != seq || sequence != seq {
		t.Fatal("auth event", e)
	}
}
func TestAccountResponseCloseWaitsActualUseAndKeepsOtherLease(t *testing.T) {
	f := newAccount(t)
	password := f.bootstrap(t)
	request, _ := loginRequest(t, f, password, "admin@mail.com")
	first, e := f.service.Login(ctxFor(t), request)
	if e != nil {
		t.Fatal(e, safeFailure(e))
	}
	second, e := f.service.Login(ctxFor(t), request)
	if e != nil {
		t.Fatal(e)
	}
	defer second.Close(ctxFor(t))
	entered, release, joined := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	go func() { joined <- first.UseCookie(func([]byte) error { close(entered); <-release; return nil }) }()
	select {
	case <-entered:
	case <-ctxFor(t).Done():
		t.Fatal("Use not active")
	}
	short, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	e = first.Close(short)
	cancel()
	if e == nil || first.Joined() {
		t.Fatal("cancel claimed join")
	}
	var active int
	e = f.store.QueryRow(ctxFor(t), `SELECT count(*) FROM agenteam_secret.secret_leases WHERE owner_kind='account_response' AND NOT released`).Scan(&active)
	if e != nil || active != 2 {
		t.Fatal("active use lease released", active, e)
	}
	close(release)
	if e = <-joined; e != nil {
		t.Fatal(e)
	}
	if e = first.Close(ctxFor(t)); e != nil {
		t.Fatal(e)
	}
	if e = second.UseCookie(func([]byte) error { return nil }); e != nil {
		t.Fatal(e)
	}
	e = f.store.QueryRow(ctxFor(t), `SELECT count(*) FROM agenteam_secret.secret_leases WHERE owner_kind='account_response' AND NOT released`).Scan(&active)
	if e != nil || active != 1 {
		t.Fatal("other lease affected", e)
	}
}
func TestAccountReadWindowCurrentPasswordAndActivity(t *testing.T) {
	f := newAccount(t)
	password := f.bootstrap(t)
	request, _ := loginRequest(t, f, password, "admin@mail.com")
	r, e := f.service.Login(ctxFor(t), request)
	if e != nil {
		t.Fatal(e, safeFailure(e))
	}
	cookie := useCookie(t, r)
	if e = r.Close(ctxFor(t)); e != nil {
		t.Fatal(e)
	}
	var before, after time.Time
	if e = f.store.QueryRow(ctxFor(t), `SELECT last_activity_at FROM agenteam_account.sessions WHERE id=$1`, r.Session().ID.String()).Scan(&before); e != nil {
		t.Fatal(e)
	}
	for range 3 {
		v, e := f.service.GetSession(ctxFor(t), cookie)
		if e != nil {
			t.Fatal(e)
		}
		v.CSRF.Destroy()
	}
	if e = f.store.QueryRow(ctxFor(t), `SELECT last_activity_at FROM agenteam_account.sessions WHERE id=$1`, r.Session().ID.String()).Scan(&after); e != nil || !before.Equal(after) {
		t.Fatal("poll touched activity", e)
	}
	if _, e = f.store.Exec(ctxFor(t), `UPDATE agenteam_account.users SET password_version=password_version+1 WHERE id=$1`, r.User().ID.String()); e != nil {
		t.Fatal(e)
	}
	if _, e = f.service.LookupLogin(ctxFor(t), request); !hasCode(e, foundation.Unauthenticated) {
		t.Fatal("old password cookie replay", e)
	}
	var plans int
	if e = f.store.QueryRow(ctxFor(t), `SELECT count(*) FROM agenteam_account.auth_attempts WHERE kind='response_read'`).Scan(&plans); e != nil || plans != 1 {
		t.Fatal("invalid current state opened lease", plans, e)
	}
	_ = c.OneSession
}
