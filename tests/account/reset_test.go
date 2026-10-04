//go:build integration

package account_test

import (
	"encoding/json"
	c "github.com/LunaDeerTech/agenteam/internal/central/account/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"net/netip"
	"testing"
)

func resetRequest(t *testing.T, f *b02Fixture, email string) c.ResetRequest {
	t.Helper()
	b, e := f.service.NewAnonymousContext(ctxFor(t))
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(b.Cookie.Destroy)
	t.Cleanup(b.CSRF.Destroy)
	r, e := c.NewResetRequest(c.ResetRequestFields{Browser: b.Identity, Key: foundation.IdempotencyKey(id[struct{}](t).String()), Email: email, ClientIP: netip.MustParseAddr("198.51.100.9")})
	if e != nil {
		t.Fatal(e)
	}
	return r
}
func TestAccountResetPublicQueueAndAsynchronousMaterial(t *testing.T) {
	f := newB02Account(t)
	f.bootstrap(t)
	registered := resetRequest(t, f, "admin@mail.com")
	missing := resetRequest(t, f, "absent@example.com")
	one, e := f.service.RequestPasswordReset(ctxFor(t), registered)
	if e != nil {
		t.Fatal("accept", e, safeFailure(e))
	}
	two, e := f.service.RequestPasswordReset(ctxFor(t), missing)
	if e != nil {
		t.Fatal("absent", e, safeFailure(e))
	}
	a, _ := json.Marshal(one)
	b, _ := json.Marshal(two)
	if string(a) != string(b) || string(a) != `{"accepted":true,"delivery_channel":"backend_log"}` {
		t.Fatal("public difference")
	}
	var queue, attempts, material, intents int
	e = f.store.QueryRow(ctxFor(t), `SELECT (SELECT count(*) FROM agenteam_account.reset_requests WHERE phase='accepted'),(SELECT count(*) FROM agenteam_account.auth_attempts WHERE kind='reset_request' AND user_id IS NULL AND outcome='success'),(SELECT count(*) FROM agenteam_account.password_resets),(SELECT count(*) FROM agenteam_account.delivery_intents)`).Scan(&queue, &attempts, &material, &intents)
	if e != nil || queue != 2 || attempts != 2 || material != 0 || intents != 0 {
		t.Fatal("synchronous difference/material", queue, attempts, material, intents, e)
	}
	again, e := f.service.RequestPasswordReset(ctxFor(t), registered)
	if e != nil || again != one {
		t.Fatal("replay", e)
	}
	st, e := f.service.Recover(ctxFor(t))
	if e != nil {
		t.Fatal("worker", st, e, safeFailure(e))
	}
	var id, ref string
	var seconds int64
	if e = f.store.QueryRow(ctxFor(t), `SELECT id::text,material_ref::text,extract(epoch FROM expires_at-created_at)::bigint FROM agenteam_account.password_resets`).Scan(&id, &ref, &seconds); e != nil || seconds != 1800 {
		t.Fatal("reset material", seconds, e)
	}
	if e = f.store.QueryRow(ctxFor(t), `SELECT (SELECT count(*) FROM agenteam_account.reset_requests WHERE phase='processed'),(SELECT count(*) FROM agenteam_account.delivery_intents)`).Scan(&queue, &intents); e != nil || queue != 2 || intents != 1 {
		t.Fatal("private queue result", queue, intents, e)
	}
	third := resetRequest(t, f, "admin@mail.com")
	if _, e = f.service.RequestPasswordReset(ctxFor(t), third); e != nil {
		t.Fatal(e)
	}
	if _, e = f.service.Recover(ctxFor(t)); e != nil {
		t.Fatal(e, safeFailure(e))
	}
	if e = f.store.QueryRow(ctxFor(t), `SELECT count(*) FROM agenteam_account.delivery_intents`).Scan(&intents); e != nil || intents != 1 {
		t.Fatal("email cooldown", intents, e)
	}
	_ = fixtureLinkToken(t, f, c.PasswordResetToken, id)
}
