//go:build integration

package account_test

import (
	"context"
	"errors"
	"net/netip"
	"testing"

	"github.com/LunaDeerTech/agenteam/internal/central/account"
	c "github.com/LunaDeerTech/agenteam/internal/central/account/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
)

func TestAccountChallengeQuotaRollbackAndRestart(t *testing.T) {
	f := newB02Account(t)
	b, e := f.service.NewAnonymousContext(ctxFor(t))
	if e != nil {
		t.Fatal(e)
	}
	defer b.Cookie.Destroy()
	defer b.CSRF.Destroy()
	r, _ := c.NewChallengeRequest(c.ChallengeFields{Browser: b.Identity, Email: "unknown@example.com", LoginKey: "planned-login"})
	v, e := f.service.CreateChallenge(ctxFor(t), r)
	if e != nil {
		t.Fatal(e, safeFailure(e))
	}
	pass, e := f.service.VerifyChallenge(ctxFor(t), r, v.ID, solveChallenge(t, v))
	if e != nil {
		t.Fatal(e)
	}
	defer pass.Destroy()
	for range 2 {
		if _, e = f.service.CreateChallenge(ctxFor(t), r); e != nil {
			t.Fatal(e)
		}
	}
	full := func() {
		t.Helper()
		if _, e := f.service.CreateChallenge(ctxFor(t), r); !hasCode(e, foundation.RateLimited) {
			t.Fatal("active proofs were evicted", e)
		}
	}
	full()
	password := testPassword(t, "A challenge-only request password 27391!")
	login, e := c.NewLoginRequest(c.LoginFields{Browser: b.Identity, Key: "planned-login", Email: "unknown@example.com", Password: password, ChallengePass: pass, ClientIP: netip.MustParseAddr("198.51.100.26")})
	if e != nil {
		t.Fatal(e)
	}
	lock, _ := foundation.SystemConfigLock("account-challenges")
	check := func(provider c.ChallengeAuthority, rollback bool) foundation.CommitResult {
		return f.store.WithinTx(ctxFor(t), cause(t), func(ctx context.Context, tx foundation.Tx) error {
			if e := f.store.AcquireAll(ctx, tx, []foundation.LockRequest{{Key: lock, Mode: foundation.Exclusive}}); e != nil {
				return e
			}
			if e := provider.CheckLoginInTx(ctx, tx, login); e != nil {
				return e
			}
			if rollback {
				return errors.New("fixture rollback after actual proof consumption")
			}
			return nil
		})
	}
	if result := check(f.challenges, true); result.State() != foundation.NotCommitted {
		t.Fatal("rollback", result.Fault())
	}
	full()
	// A newly constructed provider for a different real process identity has no
	// old answer memory. This is a restart-state test, not a claim of OS SIGKILL.
	restarted, e := account.NewChallenges(f.authority, id[c.Process](t))
	if e != nil {
		t.Fatal(e)
	}
	if result := check(restarted, false); !hasCode(result.Fault(), foundation.ChallengeInvalid) {
		t.Fatal("old process proof accepted", result.Fault())
	}
	if result := check(f.challenges, false); result.State() != foundation.Committed {
		t.Fatal("proof lost by rollback", result.Fault())
	}
	newChallenge, e := f.service.CreateChallenge(ctxFor(t), r)
	if e != nil {
		t.Fatal("committed consumption did not free quota", e, safeFailure(e))
	}
	if _, e = f.service.VerifyChallenge(ctxFor(t), r, newChallenge.ID, 361); !hasCode(e, foundation.ChallengeInvalid) {
		t.Fatal("invalid angle", e)
	}
	if _, e = f.service.VerifyChallenge(ctxFor(t), r, newChallenge.ID, solveChallenge(t, newChallenge)); !hasCode(e, foundation.ChallengeInvalid) {
		t.Fatal("failed challenge reused", e)
	}
	if _, e = f.service.CreateChallenge(ctxFor(t), r); e != nil {
		t.Fatal("failed challenge leaked quota", e)
	}
}

func TestAccountChallengeGlobalDatabaseQuota(t *testing.T) {
	f := newB02Account(t)
	b, e := f.service.NewAnonymousContext(ctxFor(t))
	if e != nil {
		t.Fatal(e)
	}
	defer b.Cookie.Destroy()
	defer b.CSRF.Destroy()
	r, _ := c.NewChallengeRequest(c.ChallengeFields{Browser: b.Identity, Email: "quota@example.com", LoginKey: "quota-key"})
	v, e := f.service.CreateChallenge(ctxFor(t), r)
	if e != nil {
		t.Fatal(e)
	}
	// Only capacity state is seeded, using the exact valid process/keyring row.
	// Puzzle generation itself is covered by the genuine challenge and browser tests.
	_, e = f.store.Exec(ctxFor(t), `INSERT INTO agenteam_account.challenges(id,process_id,browser_id,purpose,subject_kid,subject_digest,login_key,phase,expires_at) SELECT gen_random_uuid(),process_id,gen_random_uuid(),purpose,subject_kid,subject_digest,login_key,'challenge',expires_at FROM agenteam_account.challenges CROSS JOIN generate_series(1,1023) WHERE id=$1`, v.ID.String())
	if e != nil {
		t.Fatal(e)
	}
	if _, e = f.service.CreateChallenge(ctxFor(t), r); !hasCode(e, foundation.RateLimited) {
		t.Fatal("database global capacity ignored", e)
	}
	var count int
	if e = f.store.QueryRow(ctxFor(t), `SELECT count(*) FROM agenteam_account.challenges`).Scan(&count); e != nil || count != 1024 {
		t.Fatal("quota changed accepted challenges", count, e)
	}
	if _, e = f.store.Exec(ctxFor(t), `UPDATE agenteam_account.challenges SET phase='failed' WHERE id=(SELECT id FROM agenteam_account.challenges WHERE id<>$1 ORDER BY id LIMIT 1)`, v.ID.String()); e != nil {
		t.Fatal(e)
	}
	if _, e = f.service.CreateChallenge(ctxFor(t), r); e != nil {
		t.Fatal("released global slot unavailable", e)
	}
}
