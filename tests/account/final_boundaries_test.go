//go:build integration

package account_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	c "github.com/LunaDeerTech/agenteam/internal/central/account/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/audit"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/outbound"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

func TestAccountSessionIssuedLimitsAndTouchRemainCurrent(t *testing.T) {
	for _, expired := range []string{"idle", "absolute"} {
		t.Run(expired, func(t *testing.T) {
			f := newAccount(t)
			password := f.bootstrap(t)
			login, _ := loginRequest(t, f, password, "admin@mail.com")
			r, e := f.service.Login(ctxFor(t), login)
			if e != nil {
				t.Fatal(e)
			}
			cookie := useCookie(t, r)
			if e = r.Close(ctxFor(t)); e != nil {
				t.Fatal(e)
			}
			actor, e := f.service.Authenticate(ctxFor(t), cookie)
			if e != nil {
				t.Fatal(e)
			}
			var absolute, before, after time.Time
			var idle int64
			if e = f.store.QueryRow(ctxFor(t), `SELECT absolute_expires_at,idle_seconds FROM agenteam_account.sessions WHERE id=$1`, r.Session().ID.String()).Scan(&absolute, &idle); e != nil {
				t.Fatal(e)
			}
			if _, e = f.store.Exec(ctxFor(t), `UPDATE agenteam_account.account_settings SET session_idle_seconds=900,session_absolute_seconds=3600`); e != nil {
				t.Fatal(e)
			}
			// Establish a legitimate two-minute-old last activity while leaving the
			// fixed absolute deadline untouched; do not wait minutes in a test.
			if _, e = f.store.Exec(ctxFor(t), `UPDATE agenteam_account.sessions SET issued_at=clock_timestamp()-interval '3 minutes',last_activity_at=clock_timestamp()-interval '2 minutes' WHERE id=$1`, r.Session().ID.String()); e != nil {
				t.Fatal(e)
			}
			if e = f.store.QueryRow(ctxFor(t), `SELECT last_activity_at FROM agenteam_account.sessions WHERE id=$1`, r.Session().ID.String()).Scan(&before); e != nil {
				t.Fatal(e)
			}
			key, _ := foundation.UserLock(actor.Details().UserID)
			touch := func() foundation.CommitResult {
				return f.store.WithinTx(ctxFor(t), cause(t), func(ctx context.Context, tx foundation.Tx) error {
					if e := f.store.AcquireAll(ctx, tx, []foundation.LockRequest{{Key: key, Mode: foundation.Exclusive}}); e != nil {
						return e
					}
					return f.authority.TouchActivityInTx(ctx, tx, actor)
				})
			}
			if result := touch(); result.State() != foundation.Committed {
				t.Fatal(result.Fault())
			}
			var gotAbsolute time.Time
			var gotIdle int64
			if e = f.store.QueryRow(ctxFor(t), `SELECT last_activity_at,absolute_expires_at,idle_seconds FROM agenteam_account.sessions WHERE id=$1`, r.Session().ID.String()).Scan(&after, &gotAbsolute, &gotIdle); e != nil || !after.After(before) || !gotAbsolute.Equal(absolute) || gotIdle != idle {
				t.Fatal("touch changed issued limits", e)
			}
			if result := touch(); result.State() != foundation.Committed {
				t.Fatal(result.Fault())
			}
			if e = f.store.QueryRow(ctxFor(t), `SELECT last_activity_at FROM agenteam_account.sessions WHERE id=$1`, r.Session().ID.String()).Scan(&before); e != nil || !before.Equal(after) {
				t.Fatal("touch not throttled", e)
			}
			sql := `UPDATE agenteam_account.sessions SET issued_at=clock_timestamp()-interval '31 days',last_activity_at=clock_timestamp()-interval '8 days' WHERE id=$1`
			if expired == "absolute" {
				sql = `UPDATE agenteam_account.sessions SET issued_at=clock_timestamp()-interval '31 days',absolute_expires_at=clock_timestamp()-interval '1 second' WHERE id=$1`
			}
			if _, e = f.store.Exec(ctxFor(t), sql, r.Session().ID.String()); e != nil {
				t.Fatal(e)
			}
			if _, e = f.service.Authenticate(ctxFor(t), cookie); !hasCode(e, foundation.Unauthenticated) {
				t.Fatal("expired Session authenticated", expired, e)
			}
			if _, e = f.service.LookupLogin(ctxFor(t), login); !hasCode(e, foundation.Unauthenticated) {
				t.Fatal("expired Session replayed", expired, e)
			}
			if result := touch(); result.State() != foundation.NotCommitted {
				t.Fatal("touch revived Session", expired)
			}
		})
	}
}

func TestAccountResponseWindowEndsWithoutRevivingOrExtendingSession(t *testing.T) {
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
	if _, e = f.service.Recover(ctxFor(t)); e != nil {
		t.Fatal(e)
	}
	// Establish the persisted elapsed-window boundary after its old reader has
	// actually joined. No clock override or shortened production lifetime.
	if _, e = f.store.Exec(ctxFor(t), `UPDATE agenteam_account.commands SET response_expires_at=clock_timestamp()-interval '1 second' WHERE command_name='login'`); e != nil {
		t.Fatal(e)
	}
	if _, e = f.service.LookupLogin(ctxFor(t), r); !hasCode(e, foundation.Unauthenticated) {
		t.Fatal("elapsed response window reopened", e)
	}
	if _, e = f.service.Authenticate(ctxFor(t), cookie); e != nil {
		t.Fatal("response expiry changed Session validity", e)
	}
	if _, e = f.service.Recover(ctxFor(t)); e != nil {
		t.Fatal(e, safeFailure(e))
	}
	var secrets, plans, sessions int
	if e = f.store.QueryRow(ctxFor(t), `SELECT (SELECT count(*) FROM agenteam_secret.secrets),(SELECT count(*) FROM agenteam_account.response_plans),(SELECT count(*) FROM agenteam_account.sessions)`).Scan(&secrets, &plans, &sessions); e != nil || secrets != 0 || plans != 0 || sessions != 1 {
		t.Fatal("expired response did not converge", secrets, plans, sessions, e)
	}
	if _, e = f.service.Login(ctxFor(t), r); !hasCode(e, foundation.Unauthenticated) {
		t.Fatal("historical command reissued cookie", e)
	}
}

func TestAccountRecoveryProtectedHundredPlansCannotStarveJoinedReader(t *testing.T) {
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
	foreign := id[c.Process](t)
	// These are the legitimate pre-acquire plan state: no material or lease has
	// been acquired. The formal process port refuses death proof for their owner.
	for i := 0; i < 100; i++ {
		plan := fmt.Sprintf("01900000-0000-7000-8000-%012x", i+1)
		_, e = f.store.Exec(ctxFor(t), `INSERT INTO agenteam_account.response_plans(id,command_id,browser_id,user_id,session_id,semantic_kid,semantic_mac,password_version,credential_id,scope,purpose,consumer,expires_at,process_id,fence,lease_id) SELECT $1,command_id,browser_id,user_id,session_id,semantic_kid,semantic_mac,password_version,credential_id,scope,purpose,consumer,expires_at,$2,fence,$3 FROM agenteam_account.response_plans WHERE id=$4`, plan, foreign.String(), id[sc.Lease](t).String(), response.AttemptID().String())
		if e != nil {
			t.Fatal(e)
		}
	}
	for round := 0; round < 2; round++ {
		status, e := f.service.Recover(ctxFor(t))
		if e != nil || status.Pending < 1 {
			t.Fatal(status, e)
		}
	}
	var protected, joined int
	if e = f.store.QueryRow(ctxFor(t), `SELECT count(*) FILTER(WHERE process_id=$1),count(*) FILTER(WHERE id=$2) FROM agenteam_account.response_plans`, foreign.String(), response.AttemptID().String()).Scan(&protected, &joined); e != nil || protected != 100 || joined != 0 {
		t.Fatal("fair progress or live protection lost", protected, joined, e)
	}
}

func TestAccountCurrentHumanBindsSecretAndOutboundWithoutLateLocks(t *testing.T) {
	f := newAccount(t)
	password := f.bootstrap(t)
	login, _ := loginRequest(t, f, password, "admin@mail.com")
	r, e := f.service.Login(ctxFor(t), login)
	if e != nil {
		t.Fatal(e)
	}
	cookie := useCookie(t, r)
	if e = r.Close(ctxFor(t)); e != nil {
		t.Fatal(e)
	}
	actor, e := f.service.Authenticate(ctxFor(t), cookie)
	if e != nil {
		t.Fatal(e)
	}
	identityKey, e := foundation.NewCommandIdentity("secret", []string{actor.Details().UserID}, "create", foundation.IdempotencyKey(id[struct{}](t).String()))
	if e != nil {
		t.Fatal(e)
	}
	value, _ := sc.NewSecretMaterial([]byte("owned-credential-no-external-use"))
	defer value.Destroy()
	write := sc.WriteRequest{Actor: actor, Scope: identity.SystemScope(), Identity: identityKey, Kind: sc.Create, Purpose: sc.System, Value: value}
	created, e := f.secrets.ExecuteWrite(ctxFor(t), write)
	if e != nil {
		t.Fatal("current Human Secret locks", e, safeFailure(e))
	}
	_, cursor, _ := keys(t)
	aud, e := audit.New(f.store, cursor, audit.Authorizations{Sessions: f.authority, System: f.authority, Accounts: f.authority})
	if e != nil {
		t.Fatal(e)
	}
	policy, e := outbound.NewPolicyService(f.store, aud, outbound.Authorizations{Sessions: f.authority, System: f.authority})
	if e != nil {
		t.Fatal(e)
	}
	if e = policy.Reload(ctxFor(t)); e != nil {
		t.Fatal(e)
	}
	current, e := policy.GetPolicy(ctxFor(t), actor)
	if e != nil {
		t.Fatal("current Human policy read locks", e)
	}
	pk, _ := foundation.NewCommandIdentity("outbound-policy", []string{actor.Details().UserID}, "update", foundation.IdempotencyKey(id[struct{}](t).String()))
	rules, _ := outbound.NewRules()
	meta := outbound.CommandMeta{Identity: pk, ExpectedVersion: current.Version()}
	if _, e = policy.UpdatePolicy(ctxFor(t), actor, meta, rules); e != nil {
		t.Fatal("current Human policy update locks", e, safeFailure(e))
	}
	// A retained success receipt never replaces current Session authorization.
	userKey, _ := foundation.UserLock(actor.Details().UserID)
	result := f.store.WithinTx(ctxFor(t), cause(t), func(ctx context.Context, tx foundation.Tx) error {
		if e := f.store.AcquireAll(ctx, tx, []foundation.LockRequest{{Key: userKey, Mode: foundation.Exclusive}}); e != nil {
			return e
		}
		x, e := f.store.InTx(tx)
		if e != nil {
			return e
		}
		_, e = x.Exec(ctx, `UPDATE agenteam_account.sessions SET revoked_at=clock_timestamp(),revoked_reason='logout' WHERE id=$1`, actor.Details().SessionID)
		return e
	})
	if result.State() != foundation.Committed {
		t.Fatal(result.Fault())
	}
	if _, e = f.secrets.ExecuteWrite(ctxFor(t), write); e == nil {
		t.Fatal("revoked Human replayed Secret receipt")
	}
	if _, e = policy.GetPolicy(ctxFor(t), actor); e == nil {
		t.Fatal("revoked Human read policy")
	}
	if _, e = policy.UpdatePolicy(ctxFor(t), actor, meta, rules); e == nil {
		t.Fatal("revoked Human replayed policy receipt")
	}
	var count int
	if e = f.store.QueryRow(ctxFor(t), `SELECT count(*) FROM agenteam_secret.secrets WHERE id=$1`, created.Metadata.CredentialRef.Details().ID.String()).Scan(&count); e != nil || count != 1 {
		t.Fatal("revocation altered old fact", count, e)
	}
}
