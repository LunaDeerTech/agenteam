//go:build integration

package account_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"image"
	_ "image/png"
	"strings"
	"sync"
	"testing"

	c "github.com/LunaDeerTech/agenteam/internal/central/account/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

// Solve only the public puzzle pixels, independently from GoCaptcha's private
// answer and geometry validator. No test-only answer API exists in production.
func solveChallenge(t *testing.T, v c.Challenge) int {
	t.Helper()
	decode := func(raw string) image.Image {
		if i := strings.Index(raw, ","); i >= 0 {
			raw = raw[i+1:]
		}
		b, e := base64.StdEncoding.DecodeString(raw)
		if e != nil {
			t.Fatal(e)
		}
		im, _, e := image.Decode(bytes.NewReader(b))
		if e != nil {
			t.Fatal(e)
		}
		return im
	}
	result, err := solvePublicRotation(decode(v.Master), decode(v.Thumb))
	if err != nil {
		t.Fatal("public puzzle geometry", err)
	}
	return result.Angle
}
func TestAccountChallengeRealGenerationConsumptionAndBinding(t *testing.T) {
	f := newB02Account(t)
	password := f.bootstrap(t)
	if _, e := f.store.Exec(ctxFor(t), `UPDATE agenteam_account.account_settings SET challenge_after_failures=1`); e != nil {
		t.Fatal(e)
	}
	bad, _ := sc.NewSecretMaterial([]byte("not-the-actual-password-value"))
	defer bad.Destroy()
	fail, anon := loginRequest(t, f.fixture, bad, "admin@mail.com")
	if _, e := f.service.Login(ctxFor(t), fail); !hasCode(e, foundation.Unauthenticated) {
		t.Fatal(e)
	}
	fields := fail.Fields()
	fields.Key = foundation.IdempotencyKey(id[struct{}](t).String())
	fields.Password = password
	r, _ := c.NewLoginRequest(fields)
	if _, e := f.service.Login(ctxFor(t), r); !hasCode(e, foundation.ChallengeRequired) {
		t.Fatal("unproved challenge admitted hash", e, safeFailure(e))
	}
	cr, _ := c.NewChallengeRequest(c.ChallengeFields{Browser: anon.Identity, Email: "admin@mail.com", LoginKey: fields.Key})
	v, e := f.service.CreateChallenge(ctxFor(t), cr)
	if e != nil {
		t.Fatal(e, safeFailure(e))
	}
	answer := solveChallenge(t, v)
	pass, e := f.service.VerifyChallenge(ctxFor(t), cr, v.ID, answer)
	if e != nil {
		t.Fatal("actual rendered puzzle did not verify", answer, e, safeFailure(e))
	}
	defer pass.Destroy()
	if _, e = f.service.VerifyChallenge(ctxFor(t), cr, v.ID, answer); !hasCode(e, foundation.ChallengeInvalid) {
		t.Fatal("verify replay", e)
	}
	// Every proof is bound to its original browser, email and planned key.
	for _, kind := range []string{"email", "key", "browser"} {
		g := fields
		g.ChallengePass = pass
		switch kind {
		case "email":
			g.Email = "not-registered@example.com"
		case "key":
			g.Key = foundation.IdempotencyKey(id[struct{}](t).String())
		case "browser":
			a, e := f.service.NewAnonymousContext(ctxFor(t))
			if e != nil {
				t.Fatal(e)
			}
			defer a.Cookie.Destroy()
			defer a.CSRF.Destroy()
			g.Browser = a.Identity
		}
		// Direct transaction uses the actual formal gate, without depending on an
		// unrelated address's failure threshold.
		rr, _ := c.NewLoginRequest(g)
		lock, _ := foundation.SystemConfigLock("account-challenges")
		result := f.store.WithinTx(ctxFor(t), cause(t), func(ctx context.Context, tx foundation.Tx) error {
			if e := f.store.AcquireAll(ctx, tx, []foundation.LockRequest{{Key: lock, Mode: foundation.Exclusive}}); e != nil {
				return e
			}
			return f.challenges.CheckLoginInTx(ctx, tx, rr)
		})
		if result.State() == foundation.Committed || !hasCode(result.Fault(), foundation.ChallengeInvalid) {
			t.Fatal(kind, "proof mismatch", result.Fault())
		}
	}
	fields.ChallengePass = pass
	r, _ = c.NewLoginRequest(fields)
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for range 2 {
		wg.Go(func() {
			resp, e := f.service.Login(ctxFor(t), r)
			if e == nil {
				e = resp.Close(ctxFor(t))
			}
			errs <- e
		})
	}
	wg.Wait()
	close(errs)
	successes := 0
	for e := range errs {
		if e == nil {
			successes++
		} else if !hasCode(e, foundation.ResourceBusy) {
			t.Fatal("concurrent login", e, safeFailure(e))
		}
	}
	if successes == 0 {
		t.Fatal("no successful challenge login")
	}
	var sessions, consumed, subjects int
	if e = f.store.QueryRow(ctxFor(t), `SELECT (SELECT count(*) FROM agenteam_account.sessions),(SELECT count(*) FROM agenteam_account.challenges WHERE phase='consumed'),(SELECT count(*) FROM agenteam_account.auth_failures WHERE kind='subject')`).Scan(&sessions, &consumed, &subjects); e != nil || sessions != 1 || consumed != 1 || subjects != 0 {
		t.Fatal("not one atomic consumption", sessions, consumed, subjects, e)
	}
}
