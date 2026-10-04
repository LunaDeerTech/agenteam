//go:build integration

package account_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/account"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/jackc/pgx/v5/pgconn"
)

// The actual response-plan COMMIT remains held past the confirmation's database
// lock timeout. Neither the original Store outcome nor the proxy is replaced.
// Prepare the successful login before the 3s fault window: this test targets a
// response-plan replay, not the first password verification or Secret write.
func TestAccountResponsePlanUnknownKeepsCauseAndConvergesAfterWriter(t *testing.T) {
	for _, commit := range []bool{true, false} {
		name := "late_commit"
		if !commit {
			name = "rollback"
		}
		t.Run(name, func(t *testing.T) {
			f, wrapper, proxy := accountProxy(t, "response-plan", commit)
			password := f.bootstrap(t)
			request, _ := loginRequest(t, f, password, "admin@mail.com")
			// Use the same original request while faults remain disabled. Close
			// and Recover must actually retire its independent response read;
			// clearing tables or inventing a committed command would hide bugs.
			prepareCtx := ctxFor(t)
			if wrapper.enabled.Load() || wrapper.fired.Load() {
				t.Fatal("response-plan fault armed during preparation")
			}
			prepared, prepareErr := f.service.Login(prepareCtx, request)
			if prepareErr != nil {
				t.Fatal("prepare successful login", safeFailure(prepareErr))
			}
			preparedCookie := useCookie(t, prepared)
			if prepareErr = prepared.Close(prepareCtx); prepareErr != nil || !prepared.Joined() {
				t.Fatal("prepare response did not join", safeFailure(prepareErr))
			}
			if _, prepareErr = f.service.Recover(prepareCtx); prepareErr != nil {
				t.Fatal("prepare response recovery", safeFailure(prepareErr))
			}
			_, prepareErr = f.service.Authenticate(prepareCtx, preparedCookie)
			preparedCookie.Destroy()
			if prepareErr != nil {
				t.Fatal("prepared Session is not current", safeFailure(prepareErr))
			}
			var preparedPlans, preparedAttempts, preparedLeases, committedCommands, preparedSessions int
			prepareErr = f.store.QueryRow(prepareCtx, `SELECT
			 (SELECT count(*) FROM agenteam_account.response_plans),
			 (SELECT count(*) FROM agenteam_account.auth_attempts WHERE kind='response_read'),
			 (SELECT count(*) FROM agenteam_secret.secret_leases WHERE NOT released),
			 (SELECT count(*) FROM agenteam_account.commands WHERE namespace='account.login' AND owner_id=$1 AND command_key=$2 AND command_name='login' AND phase='committed' AND user_id=$3 AND session_id=$4),
			 (SELECT count(*) FROM agenteam_account.sessions)`, request.Fields().Browser.ID().String(), string(request.Fields().Key), prepared.User().ID.String(), prepared.Session().ID.String()).Scan(&preparedPlans, &preparedAttempts, &preparedLeases, &committedCommands, &preparedSessions)
			if prepareErr != nil || preparedPlans != 0 || preparedAttempts != 0 || preparedLeases != 0 || committedCommands != 1 || preparedSessions != 1 {
				t.Fatal("prepare login facts did not converge", preparedPlans, preparedAttempts, preparedLeases, committedCommands, preparedSessions, safeFailure(prepareErr))
			}
			wrapper.enabled.Store(true)
			type outcome struct {
				response account.LoginResponse
				err      error
			}
			done := make(chan outcome, 1)
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			go func() { response, err := f.service.Login(ctx, request); done <- outcome{response, err} }()
			await(t, proxy.reached)
			var out outcome
			select {
			case out = <-done:
			case <-time.After(4 * time.Second):
				t.Fatal("held login exceeded original budget")
			}
			var fault *foundation.Fault
			var pg *pgconn.PgError
			if !errors.As(out.err, &fault) || fault == nil || fault.Code != foundation.CommitUnknown || fault.CommitState != foundation.Unknown {
				t.Fatal("confirmation failure lost original unknown", out.err, safeFailure(out.err))
			}
			if !errors.As(out.err, &pg) || pg == nil || pg.Code != "55P03" {
				t.Fatal("confirmation lock cause lost", safeFailure(out.err))
			}
			if ctx.Err() != nil {
				t.Fatal("fixture did not exercise live-caller database lock timeout", ctx.Err())
			}
			if !wrapper.fired.Load() || !out.response.Joined() {
				t.Fatal("unconfirmed response escaped")
			}
			called := false
			_ = out.response.UseCookie(func([]byte) error { called = true; return nil })
			if called {
				t.Fatal("unknown cookie exposed")
			}
			var plans, attempts, leases int
			observe := func() {
				t.Helper()
				e := f.store.QueryRow(ctxFor(t), `SELECT (SELECT count(*) FROM agenteam_account.response_plans),(SELECT count(*) FROM agenteam_account.auth_attempts WHERE kind='response_read'),(SELECT count(*) FROM agenteam_secret.secret_leases WHERE NOT released)`).Scan(&plans, &attempts, &leases)
				if e != nil {
					t.Fatal(e)
				}
			}
			observe()
			if plans != 0 || attempts != 0 || leases != 0 {
				t.Fatal("held original writer became visible", plans, attempts, leases)
			}
			// Error projection may expose only the safe code/state; diagnostics
			// reach the server's cause through explicit errors.As above.
			projection := fmt.Sprintf("%+v %#v", out.err, struct{ err error }{out.err})
			wire, e := json.Marshal(out.err)
			if e != nil {
				t.Fatal(e)
			}
			if strings.Contains(projection+string(wire), "lock timeout") || strings.Contains(projection+string(wire), "agenteam_account") {
				t.Fatal("raw database diagnostic exposed")
			}
			close(proxy.release)
			await(t, proxy.completed)
			observe()
			want := 0
			if commit {
				want = 1
			}
			if plans != want || attempts != 0 || leases != 0 {
				t.Fatal("actual original outcome mismatch", plans, attempts, leases)
			}
			for range 2 {
				if _, e = f.service.Recover(ctxFor(t)); e != nil {
					t.Fatal(e, safeFailure(e))
				}
			}
			observe()
			if plans != 0 || attempts != 0 || leases != 0 {
				t.Fatal("same-process joined plan did not converge", plans, attempts, leases)
			}
			// Convergence did not alter the original successful login. A fresh
			// legal replay obtains a distinct independent read and one Session.
			fresh, e := f.service.Login(ctxFor(t), request)
			if e != nil {
				t.Fatal(e, safeFailure(e))
			}
			if e = fresh.Close(ctxFor(t)); e != nil {
				t.Fatal(e)
			}
			if _, e = f.service.Recover(ctxFor(t)); e != nil {
				t.Fatal(e)
			}
			observe()
			if plans != 0 || attempts != 0 || leases != 0 {
				t.Fatal("later normal read did not retire", plans, attempts, leases)
			}
			var sessions int
			if e = f.store.QueryRow(ctxFor(t), `SELECT count(*) FROM agenteam_account.sessions`).Scan(&sessions); e != nil || sessions != 1 {
				t.Fatal("recovery created another Session", sessions, e)
			}
		})
	}
}
