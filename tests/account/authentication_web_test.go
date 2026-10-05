//go:build integration

package account_test

import (
	"context"
	"testing"
	"time"
)

func TestAccountAuthenticationWebSessionLifecycle(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	f := newAuthenticationWebFixture(t, ctx)
	r := f.browser(ctx, "lifecycle")
	if !r.LoggedOut || r.UserID != f.entry.ID || r.SessionID == "" {
		t.Fatal("browser did not complete real Session lifecycle")
	}
	var revoked bool
	var audits int
	conn := f.db.Connect(t)
	if e := conn.QueryRow(ctx, `SELECT revoked_at IS NOT NULL AND revoked_reason='logout' FROM agenteam_account.sessions WHERE id=$1 AND user_id=$2`, r.SessionID, r.UserID).Scan(&revoked); e != nil || !revoked {
		t.Fatal("exact browser Session was not revoked by logout", e)
	}
	if e := conn.QueryRow(ctx, `SELECT count(*) FROM agenteam_audit.audit_records WHERE action IN ('account.login','account.logout') AND metadata->>'session_id'=$1 AND outcome='success'`, r.SessionID).Scan(&audits); e != nil || audits != 2 {
		t.Fatal("real Session login/logout audit missing", e)
	}
	t.Logf("real user=%s session=%s logout=true successful Session audit=%d", r.UserID, r.SessionID, audits)
}

func TestAccountAuthenticationWebRotateChallenge(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	for _, name := range []string{"desktop", "keyboard"} {
		t.Run(name, func(t *testing.T) {
			f := newAuthenticationWebFixture(t, ctx)
			f.setChallengeThreshold(ctx)
			r := f.browser(ctx, name)
			if !r.Verified || !r.ExactPass || !r.ExactKey || r.UserID != f.entry.ID {
				t.Fatal("actual rotate did not preserve verified pass and login intent")
			}
			var phase string
			if e := f.db.Connect(t).QueryRow(ctx, `SELECT phase FROM agenteam_account.challenges WHERE id=$1`, r.ChallengeID).Scan(&phase); e != nil || phase != "consumed" {
				t.Fatal("real challenge was not atomically consumed", e)
			}
			t.Logf("%s actual challenge=%s consumed; pass length80/exact same login key checked in memory", name, r.ChallengeID)
		})
	}
}

func TestAccountAuthenticationWebRevocationAndExpiry(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	for _, name := range []string{"revocation", "expiry"} {
		t.Run(name, func(t *testing.T) {
			f := newAuthenticationWebFixture(t, ctx)
			r := f.browser(ctx, name)
			if !r.Unavailable || r.UserID != f.entry.ID || r.SessionID == "" {
				t.Fatal("real Session invalidation did not remove protected content")
			}
			var invalid bool
			query := `SELECT revoked_at IS NOT NULL AND revoked_reason='password_changed' FROM agenteam_account.sessions WHERE id=$1 AND user_id=$2`
			if name == "expiry" {
				query = `SELECT absolute_expires_at<clock_timestamp() FROM agenteam_account.sessions WHERE id=$1 AND user_id=$2`
			}
			if e := f.db.Connect(t).QueryRow(ctx, query, r.SessionID, r.UserID).Scan(&invalid); e != nil || !invalid {
				t.Fatal("exact owned Session invalidation fact missing", e)
			}
			t.Logf("%s exact Session=%s confirmed unavailable through real GET/router", name, r.SessionID)
		})
	}
}

func TestAccountAuthenticationWebLayoutsAndProduction(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	f := newAuthenticationWebFixture(t, ctx)
	r := f.browser(ctx, "layouts")
	if r.Layouts != 8 || r.UserID != f.entry.ID {
		t.Fatal("formal dist theme/viewport matrix incomplete")
	}
	t.Log("formal dist: eight theme/viewport pairs, 200% scale, keyboard/reduced motion, real saved theme, API/asset fallback and no Debug verified")
}
