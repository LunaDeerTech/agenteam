//go:build integration

package account_test

import (
	c "github.com/LunaDeerTech/agenteam/internal/central/account/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
	"sync"
	"testing"
)

func b02Login(t *testing.T, f *b02Fixture, password sc.SecretMaterial) (identity.Actor, sc.SecretMaterial) {
	t.Helper()
	r, _ := loginRequest(t, f.fixture, password, "admin@mail.com")
	resp, e := f.service.Login(ctxFor(t), r)
	if e != nil {
		t.Fatal(e, safeFailure(e))
	}
	cookie := useCookie(t, resp)
	a, e := f.service.Authenticate(ctxFor(t), cookie)
	if e != nil {
		t.Fatal(e)
	}
	if e = resp.Close(ctxFor(t)); e != nil {
		t.Fatal(e)
	}
	return a, cookie
}
func testPassword(t *testing.T, v string) sc.SecretMaterial {
	t.Helper()
	p, e := sc.NewSecretMaterial([]byte(v))
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(p.Destroy)
	return p
}
func TestAccountPasswordChangeAtomicRevocationAndLostResponse(t *testing.T) {
	f := newB02Account(t)
	old := f.bootstrap(t)
	a, first := b02Login(t, f, old)
	_, second := b02Login(t, f, old)
	newPassword := testPassword(t, "A replacement passphrase 91763 strong!")
	r, e := c.NewPasswordChange(c.PasswordChangeFields{Actor: a, Key: foundation.IdempotencyKey(id[struct{}](t).String()), ExpectedVersion: 1, OldPassword: old, Password: newPassword, Confirmation: newPassword})
	if e != nil {
		t.Fatal(e)
	}
	wrong := r.Fields()
	wrong.OldPassword = testPassword(t, "wrong original phrase 582!")
	bad, _ := c.NewPasswordChange(wrong)
	if _, e = f.service.ChangePassword(ctxFor(t), bad); !hasCode(e, foundation.InvalidArgument) {
		t.Fatal("old password", e, safeFailure(e))
	}
	response, e := f.service.ChangePassword(ctxFor(t), r)
	if e != nil {
		t.Fatal("change", e, safeFailure(e))
	}
	var cookie sc.SecretMaterial
	if e = response.UseCookie(func(b []byte) error { var e error; cookie, e = sc.NewSecretMaterial(b); return e }); e != nil {
		t.Fatal(e)
	}
	defer cookie.Destroy()
	if e = response.Close(ctxFor(t)); e != nil {
		t.Fatal(e)
	}
	for _, v := range []sc.SecretMaterial{first, second} {
		if _, e = f.service.Authenticate(ctxFor(t), v); !hasCode(e, foundation.SessionRevoked) {
			t.Fatal("old cookie", e, safeFailure(e))
		}
	}
	if _, e = f.service.Authenticate(ctxFor(t), cookie); e != nil {
		t.Fatal("new cookie", e)
	}
	if _, e = f.service.ChangePassword(ctxFor(t), r); !hasCode(e, foundation.SessionRevoked) {
		t.Fatal("revoked replay", e)
	}
	var version, pv, seq, live, audits, events, commands int64
	var suggestion bool
	e = f.store.QueryRow(ctxFor(t), `SELECT version,password_version,auth_sequence,initial_password_suggestion,(SELECT count(*) FROM agenteam_account.sessions WHERE revoked_at IS NULL),(SELECT count(*) FROM agenteam_audit.audit_records WHERE action='account.password.change'),(SELECT count(*) FROM agenteam_outbox.events WHERE event_type='account.sessions-revoked'),(SELECT count(*) FROM agenteam_account.commands WHERE command_name='password-change' AND phase='committed') FROM agenteam_account.users`).Scan(&version, &pv, &seq, &suggestion, &live, &audits, &events, &commands)
	if e != nil || version != 2 || pv != 2 || seq != 2 || suggestion || live != 1 || audits != 1 || events != 1 || commands != 1 {
		t.Fatal("atomic facts", version, pv, seq, suggestion, live, audits, events, commands, e)
	}
	b02Login(t, f, newPassword)
	oldRequest, _ := loginRequest(t, f.fixture, old, "admin@mail.com")
	if _, e = f.service.Login(ctxFor(t), oldRequest); !hasCode(e, foundation.Unauthenticated) {
		t.Fatal("old password still valid", e)
	}
}
func TestAccountPasswordResetConsumesTokenWithoutSessionAndSafeReplay(t *testing.T) {
	f := newB02Account(t)
	old := f.bootstrap(t)
	_, cookie := b02Login(t, f, old)
	request := resetRequest(t, f, "admin@mail.com")
	if _, e := f.service.RequestPasswordReset(ctxFor(t), request); e != nil {
		t.Fatal(e)
	}
	if _, e := f.service.Recover(ctxFor(t)); e != nil {
		t.Fatal(e, safeFailure(e))
	}
	var rid string
	if e := f.store.QueryRow(ctxFor(t), `SELECT id::text FROM agenteam_account.password_resets`).Scan(&rid); e != nil {
		t.Fatal(e)
	}
	token := fixtureLinkToken(t, f, c.PasswordResetToken, rid)
	b, e := f.service.NewAnonymousContext(ctxFor(t))
	if e != nil {
		t.Fatal(e)
	}
	defer b.Cookie.Destroy()
	defer b.CSRF.Destroy()
	if view, e := f.service.InspectPasswordReset(ctxFor(t), b.Identity, token); e != nil || !view.Valid {
		t.Fatal("inspect", e)
	}
	password := testPassword(t, "A reset phrase with entropy 73926!")
	r, e := c.NewResetComplete(c.ResetCompleteFields{Browser: b.Identity, Key: foundation.IdempotencyKey(id[struct{}](t).String()), Token: token, Password: password, Confirmation: password})
	if e != nil {
		t.Fatal(e)
	}
	if out, e := f.service.CompletePasswordReset(ctxFor(t), r); e != nil || !out.Completed {
		t.Fatal("complete", e, safeFailure(e))
	}
	if _, e = f.service.CompletePasswordReset(ctxFor(t), r); e != nil {
		t.Fatal("receipt", e)
	}
	changed := r.Fields()
	changed.Password = testPassword(t, "different reset phrase 56218!")
	changed.Confirmation = changed.Password
	different, _ := c.NewResetComplete(changed)
	if _, e = f.service.CompletePasswordReset(ctxFor(t), different); !hasCode(e, foundation.IdempotencyKeyReused) {
		t.Fatal("semantic", e)
	}
	changed = r.Fields()
	changed.Key = foundation.IdempotencyKey(id[struct{}](t).String())
	fresh, _ := c.NewResetComplete(changed)
	if _, e = f.service.CompletePasswordReset(ctxFor(t), fresh); !hasCode(e, foundation.ResourceDeleted) {
		t.Fatal("new key after consumed", e)
	}
	if _, e = f.service.InspectPasswordReset(ctxFor(t), b.Identity, token); !hasCode(e, foundation.ResourceDeleted) {
		t.Fatal("consumed inspect", e)
	}
	if _, e = f.service.Authenticate(ctxFor(t), cookie); !hasCode(e, foundation.SessionRevoked) {
		t.Fatal("old session", e)
	}
	var live, resets, refs, audits, events int
	e = f.store.QueryRow(ctxFor(t), `SELECT (SELECT count(*) FROM agenteam_account.sessions WHERE revoked_at IS NULL),(SELECT count(*) FROM agenteam_account.password_resets),(SELECT count(*) FROM agenteam_secret.secret_references WHERE owner_id=$1),(SELECT count(*) FROM agenteam_audit.audit_records WHERE action='account.password.reset.complete'),(SELECT count(*) FROM agenteam_outbox.events WHERE event_type='account.sessions-revoked')`, rid).Scan(&live, &resets, &refs, &audits, &events)
	if e != nil || live != 0 || resets != 0 || refs != 0 || audits != 1 || events != 1 {
		t.Fatal("atomic facts", live, resets, refs, audits, events, e)
	}
	b02Login(t, f, password)
	if _, e = f.service.Recover(ctxFor(t)); e != nil {
		t.Fatal("cleanup", e, safeFailure(e))
	}
}
func TestAccountPasswordResetDifferentCommandsHaveOneWinner(t *testing.T) {
	f := newB02Account(t)
	f.bootstrap(t)
	request := resetRequest(t, f, "admin@mail.com")
	if _, e := f.service.RequestPasswordReset(ctxFor(t), request); e != nil {
		t.Fatal(e)
	}
	if _, e := f.service.Recover(ctxFor(t)); e != nil {
		t.Fatal(e)
	}
	var rid string
	if e := f.store.QueryRow(ctxFor(t), `SELECT id::text FROM agenteam_account.password_resets`).Scan(&rid); e != nil {
		t.Fatal(e)
	}
	token := fixtureLinkToken(t, f, c.PasswordResetToken, rid)
	b, e := f.service.NewAnonymousContext(ctxFor(t))
	if e != nil {
		t.Fatal(e)
	}
	defer b.Cookie.Destroy()
	defer b.CSRF.Destroy()
	password := testPassword(t, "A concurrent reset phrase 13579!")
	requests := make([]c.ResetComplete, 2)
	for i := range requests {
		requests[i], e = c.NewResetComplete(c.ResetCompleteFields{Browser: b.Identity, Key: foundation.IdempotencyKey(id[struct{}](t).String()), Token: token, Password: password, Confirmation: password})
		if e != nil {
			t.Fatal(e)
		}
	}
	start := make(chan struct{})
	results := make(chan error, 2)
	var wg sync.WaitGroup
	for _, r := range requests {
		wg.Add(1)
		go func() { defer wg.Done(); <-start; _, e := f.service.CompletePasswordReset(ctxFor(t), r); results <- e }()
	}
	close(start)
	wg.Wait()
	close(results)
	wins := 0
	for e := range results {
		if e == nil {
			wins++
		} else if !hasCode(e, foundation.ResourceDeleted) && !hasCode(e, foundation.ResourceBusy) {
			t.Fatal("race", e, safeFailure(e))
		}
	}
	if wins != 1 {
		t.Fatal("winners", wins)
	}
	var pv, events int
	e = f.store.QueryRow(ctxFor(t), `SELECT password_version,(SELECT count(*) FROM agenteam_outbox.events WHERE event_type='account.sessions-revoked') FROM agenteam_account.users`).Scan(&pv, &events)
	if e != nil || pv != 2 || events != 1 {
		t.Fatal("duplicate reset", pv, events, e)
	}
}
