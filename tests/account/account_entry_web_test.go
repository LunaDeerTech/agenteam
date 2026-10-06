//go:build integration

package account_test

import (
	"context"
	"testing"
	"time"
)

func (f *entryWebFixture) entryCommandFacts(ctx context.Context, userID, command, action string) {
	f.t.Helper()
	var commands, audits int
	if e := f.db.Connect(f.t).QueryRow(ctx, `SELECT (SELECT count(*) FROM agenteam_account.commands WHERE user_id=$1::uuid AND command_name=$2 AND phase='committed'),(SELECT count(*) FROM agenteam_audit.audit_records WHERE producer='account' AND metadata->>'user_id'=$1::text AND action=$3 AND outcome='success')`, userID, command, action).Scan(&commands, &audits); e != nil || commands != 1 || audits != 1 {
		f.t.Fatalf("exact public command/audit poststate query=%v commands=%d audits=%d", e, commands, audits)
	}
}
func TestAccountPublicEntryWebInvitation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	f := newEntryWebFixture(t, ctx)
	r := f.browserEntry(ctx, "invitation")
	var userID string
	var count int
	if e := f.db.Connect(t).QueryRow(ctx, `SELECT min(id::text),count(*) FROM agenteam_account.users WHERE email=$1 AND username='entry-invited' AND role='user'`, f.invitationEmail).Scan(&userID, &count); e != nil || count != 1 || userID != r.UserID || r.SessionID == "" {
		t.Fatal("exact redeemed user/explicit login facts missing", e)
	}
	f.entryCommandFacts(ctx, r.UserID, "invite-redeem", "account.invite.redeem")
	var resource string
	if e := f.db.Connect(t).QueryRow(ctx, `SELECT resource_id::text FROM agenteam_account.commands WHERE user_id=$1 AND command_name='invite-redeem' AND phase='committed'`, r.UserID).Scan(&resource); e != nil || resource != f.invitationID {
		t.Fatal("redemption receipt does not reference owned invitation", e)
	}
	t.Log("real invitation inspected, duplicate username rejected, redeemed once, and explicitly logged in; exact User/command/Audit verified")
}
func TestAccountPublicEntryWebPasswordReset(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	f := newEntryWebFixture(t, ctx)
	r := f.browserEntry(ctx, "reset")
	if r.UserID != f.member.UserID || len(r.OldSessionIDs) != 2 || r.OldSessionIDs[0] == r.OldSessionIDs[1] || r.SessionID == "" {
		t.Fatal("reset browser did not identify exact owned Sessions")
	}
	f.entryCommandFacts(ctx, r.UserID, "reset-complete", "account.password.reset.complete")
	for _, session := range r.OldSessionIDs {
		var revoked bool
		if e := f.db.Connect(t).QueryRow(ctx, `SELECT revoked_at IS NOT NULL AND revoked_reason='password_reset' FROM agenteam_account.sessions WHERE id=$1 AND user_id=$2`, session, r.UserID).Scan(&revoked); e != nil || !revoked {
			t.Fatal("exact previous Session was not revoked by reset", e)
		}
	}
	var valid bool
	if e := f.db.Connect(t).QueryRow(ctx, `SELECT revoked_at IS NULL FROM agenteam_account.sessions WHERE id=$1 AND user_id=$2`, r.SessionID, r.UserID).Scan(&valid); e != nil || !valid {
		t.Fatal("explicit new-password Session missing", e)
	}
	t.Log("real recovery delivery and reset confirmed once; two prior Sessions revoked, old password refused, explicit new-password login usable")
}
func TestAccountPublicEntryWebIdentityNavigation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	f := newEntryWebFixture(t, ctx)
	r := f.browserEntry(ctx, "identity")
	if !r.IdentityPreserved || r.UserID != f.admin.UserID || r.SessionID == "" {
		t.Fatal("current other identity was not preserved")
	}
	f.entryCommandFacts(ctx, f.member.UserID, "reset-complete", "account.password.reset.complete")
	var live bool
	if e := f.db.Connect(t).QueryRow(ctx, `SELECT revoked_at IS NULL FROM agenteam_account.sessions WHERE id=$1 AND user_id=$2`, r.SessionID, r.UserID).Scan(&live); e != nil || !live {
		t.Fatal("other current Session invalidated by target reset", e)
	}
	t.Log("real forbidden invitation retained A until explicit logout; target B reset preserved A; settings dirty/back remained guarded")
}
func TestAccountPublicEntryWebPrivacyAndProduction(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	f := newEntryWebFixture(t, ctx)
	r := f.browserEntry(ctx, "privacy")
	if !r.GenericResponse || r.Layouts != 24 {
		t.Fatal("privacy or three-page theme/viewport observations incomplete")
	}
	var unknown int
	if e := f.db.Connect(t).QueryRow(ctx, `SELECT count(*) FROM agenteam_account.users WHERE email='public-entry-unknown@example.com'`).Scan(&unknown); e != nil || unknown != 0 {
		t.Fatal("public unknown recovery created a user", e)
	}
	t.Log("registered/unknown email share public receipt; consumed/revoked links rejected; 24 page/theme/viewport pairs, CSS 200 percent, keyboard/reduced motion and production routing verified")
}
