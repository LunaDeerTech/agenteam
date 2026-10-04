//go:build integration

package account_test

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"testing"

	"github.com/LunaDeerTech/agenteam/internal/central/account"
	c "github.com/LunaDeerTech/agenteam/internal/central/account/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/object"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

func TestAccountMissingChallengeProviderAndFailureReceiptPrivacy(t *testing.T) {
	f := newAccount(t)
	_ = f.bootstrap(t)
	if _, e := f.store.Exec(ctxFor(t), `UPDATE agenteam_account.account_settings SET challenge_after_failures=1`); e != nil {
		t.Fatal(e)
	}
	bad, e := sc.NewSecretMaterial([]byte("intentionally-wrong-private-password"))
	if e != nil {
		t.Fatal(e)
	}
	defer bad.Destroy()
	r, _ := loginRequest(t, f, bad, "nonexistent-private@example.com")
	for range 2 {
		if _, e = f.service.Login(ctxFor(t), r); !hasCode(e, foundation.Unauthenticated) {
			t.Fatal("incorrect same-command result", e, safeFailure(e))
		}
	}
	var attempts, audits, sessions int
	var user *string
	if e = f.store.QueryRow(ctxFor(t), `SELECT (SELECT count(*) FROM agenteam_account.auth_attempts WHERE kind='login'),(SELECT count(*) FROM agenteam_audit.audit_records WHERE action='account.login'),(SELECT count(*) FROM agenteam_account.sessions),(SELECT user_id::text FROM agenteam_account.auth_attempts WHERE kind='login' LIMIT 1)`).Scan(&attempts, &audits, &sessions, &user); e != nil {
		t.Fatal(e)
	}
	if attempts != 1 || audits != 1 || sessions != 0 || user != nil {
		t.Fatal("failed replay mutated/forged User", attempts, audits, sessions, user != nil)
	}
	fields := r.Fields()
	fields.Key = foundation.IdempotencyKey(id[struct{}](t).String())
	next, e := c.NewLoginRequest(fields)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = f.service.Login(ctxFor(t), next); !hasCode(e, foundation.ChallengeRequired) {
		t.Fatal("unbound challenge bypass", e, safeFailure(e))
	}
	var count int
	if e = f.store.QueryRow(ctxFor(t), `SELECT count(*) FROM agenteam_account.commands`).Scan(&count); e != nil || count != 1 {
		t.Fatal("challenge refusal created a login", count, e)
	}
	var leaked bool
	if e = f.store.QueryRow(ctxFor(t), `SELECT EXISTS(SELECT 1 FROM agenteam_audit.audit_records WHERE metadata::text LIKE '%nonexistent-private%' OR metadata::text LIKE '%intentionally-wrong%')`).Scan(&leaked); e != nil || leaked {
		t.Fatal("private auth input reached Audit")
	}
	if _, e = f.store.Exec(ctxFor(t), `UPDATE agenteam_account.auth_failures SET window_start=clock_timestamp()-interval '2 days',expires_at=clock_timestamp()-interval '1 second' WHERE kind='subject'`); e != nil {
		t.Fatal(e)
	}
	if _, e = f.service.Recover(ctxFor(t)); e != nil {
		t.Fatal(e)
	}
	var subjects, ips int
	if e = f.store.QueryRow(ctxFor(t), `SELECT count(*) FILTER(WHERE kind='subject'),count(*) FILTER(WHERE kind='ip') FROM agenteam_account.auth_failures`).Scan(&subjects, &ips); e != nil || subjects != 0 || ips != 1 {
		t.Fatal("counter expiry removed current IP or kept expired subject", subjects, ips, e)
	}
}
func TestAccountCurrentRegularHumanAuditDoesNotGrantSystem(t *testing.T) {
	f := newAccount(t)
	password := f.bootstrap(t)
	r, _ := loginRequest(t, f, password, "admin@mail.com")
	response, e := f.service.Login(ctxFor(t), r)
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
	if _, e = f.store.Exec(ctxFor(t), `UPDATE agenteam_account.users SET role='user',version=version+1 WHERE id=$1`, actor.Details().UserID); e != nil {
		t.Fatal(e)
	}
	if _, e = f.authority.AuthorizeSystem(ctxFor(t), foundation.Tx{}, actor, identity.Mutate); !hasCode(e, foundation.Forbidden) {
		t.Fatal("ordinary user got System", e)
	}
	if e = f.service.Logout(ctxFor(t), account.LogoutRequest{Actor: actor, Key: foundation.IdempotencyKey(id[struct{}](t).String())}); e != nil {
		t.Fatal("ordinary self Audit denied", e, safeFailure(e))
	}
}
func accountRing(t *testing.T, current, entries string) account.Keyring {
	t.Helper()
	_, cursor, master := keys(t)
	d, e := object.LoadDownloadKeyring(fmt.Sprintf(`{"format":1,"current_kid":"d","keys":[{"kid":"d","key_b64":%q}]}`, base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{3}, 32))), cursor, master)
	if e != nil {
		t.Fatal(e)
	}
	k, e := account.LoadKeyring(fmt.Sprintf(`{"format":1,"current_kid":%q,"keys":[%s]}`, current, entries), cursor, master, d)
	if e != nil {
		t.Fatal(e)
	}
	return k
}
func TestAccountKeyRegistryRequiresOldReferencedKeysAndRejectsReuse(t *testing.T) {
	f := newAccount(t)
	password := f.bootstrap(t)
	r, _ := loginRequest(t, f, password, "admin@mail.com")
	response, e := f.service.Login(ctxFor(t), r)
	if e != nil {
		t.Fatal(e)
	}
	if e = response.Close(ctxFor(t)); e != nil {
		t.Fatal(e)
	}
	entry := func(k string, b byte) string {
		return fmt.Sprintf(`{"kid":%q,"key_b64":%q}`, k, base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{b}, 32)))
	}
	for _, v := range []struct {
		name, current, entries string
		fail                   bool
	}{
		{"rotate", "b", entry("a", 4) + "," + entry("b", 9), false},
		{"remove_referenced", "b", entry("b", 9), true},
		{"reuse_kid", "a", entry("a", 8) + "," + entry("b", 9), true},
	} {
		t.Run(v.name, func(t *testing.T) {
			a, e := account.NewAuthority(f.store, accountRing(t, v.current, v.entries))
			if e != nil {
				t.Fatal(e)
			}
			e = a.Initialize(ctxFor(t))
			if (e != nil) != v.fail {
				t.Fatal("registry result", e)
			}
		})
	}
}
