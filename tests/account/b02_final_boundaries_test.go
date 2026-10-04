//go:build integration

package account_test

import (
	"context"
	"errors"
	"testing"
	"time"

	c "github.com/LunaDeerTech/agenteam/internal/central/account/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
)

func TestAccountResetIPQuotaCountsBothExistenceClassesAndReplay(t *testing.T) {
	f := newB02Account(t)
	f.bootstrap(t)
	var original c.ResetRequest
	for i := range 30 {
		email := "absent@example.com"
		if i%2 == 0 {
			email = "admin@mail.com"
		}
		r := resetRequest(t, f, email)
		if i == 0 {
			original = r
		}
		v, e := f.service.RequestPasswordReset(ctxFor(t), r)
		if e != nil || !v.Accepted || v.DeliveryChannel != "backend_log" {
			t.Fatal("uniform request", i, e)
		}
	}
	for _, email := range []string{"admin@mail.com", "absent@example.com"} {
		if _, e := f.service.RequestPasswordReset(ctxFor(t), resetRequest(t, f, email)); !hasCode(e, foundation.RateLimited) {
			t.Fatal("existence changed quota", email, e)
		}
	}
	if _, e := f.service.RequestPasswordReset(ctxFor(t), original); e != nil {
		t.Fatal("quota denied original receipt", e)
	}
	var requests, attempts, material int
	if e := f.store.QueryRow(ctxFor(t), `SELECT (SELECT count(*) FROM agenteam_account.reset_requests),(SELECT count(*) FROM agenteam_account.auth_attempts WHERE kind='reset_request' AND user_id IS NULL),(SELECT count(*) FROM agenteam_account.password_resets)`).Scan(&requests, &attempts, &material); e != nil || requests != 30 || attempts != 30 || material != 0 {
		t.Fatal("quota or synchronous privacy", requests, attempts, material, e)
	}
}

func TestAccountChangedCookieForceWaitsActualUseJoin(t *testing.T) {
	f := newB02Account(t)
	old := f.bootstrap(t)
	actor, _ := b02Login(t, f, old)
	p := testPassword(t, "Actual material join password 573826!")
	r, _ := c.NewPasswordChange(c.PasswordChangeFields{Actor: actor, Key: "force-cookie", ExpectedVersion: 1, OldPassword: old, Password: p, Confirmation: p})
	response, e := f.service.ChangePassword(ctxFor(t), r)
	if e != nil {
		t.Fatal(e)
	}
	entered := make(chan struct{})
	release := make(chan struct{})
	joined := make(chan error, 1)
	go func() {
		joined <- response.UseCookie(func(raw []byte) error {
			if len(raw) == 0 {
				return errors.New("missing cookie")
			}
			close(entered)
			<-release
			return nil
		})
	}()
	await(t, entered)
	released := false
	defer func() {
		if !released {
			close(release)
		}
		_ = response.Close(ctxFor(t))
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	e = f.service.Force(ctx)
	cancel()
	if e == nil || !errors.Is(e, context.DeadlineExceeded) || f.service.Joined() {
		t.Fatal("force pretended callback joined", e)
	}
	exposed := false
	if e = response.UseCookie(func([]byte) error { exposed = true; return nil }); e == nil || exposed {
		t.Fatal("force admitted new material use")
	}
	close(release)
	released = true
	select {
	case e = <-joined:
		if e != nil {
			t.Fatal(e)
		}
	case <-time.After(time.Second):
		t.Fatal("actual use failed to join")
	}
	if e = response.Close(ctxFor(t)); e != nil {
		t.Fatal(e)
	}
	if !f.service.Joined() {
		t.Fatal("joined material still holds process")
	}
}
