//go:build integration

package account_test

import (
	"context"
	"testing"
	"time"
)

func (f *personalWebFixture) requirePersonalFacts(ctx context.Context, result personalWebResult) {
	f.t.Helper()
	var version, role string
	var commands, audits int
	e := f.db.Connect(f.t).QueryRow(ctx, `SELECT version::text,role,(SELECT count(*) FROM agenteam_account.commands WHERE user_id=u.id AND command_name IN ('profile-update','avatar-update','password-change') AND phase='committed'),(SELECT count(*) FROM agenteam_audit.audit_records WHERE producer='account' AND resource_id=u.id AND action IN ('account.profile.update','account.avatar.update','account.password.change') AND outcome='success') FROM agenteam_account.users u WHERE id=$1`, result.UserID).Scan(&version, &role, &commands, &audits)
	if e != nil || role != "admin" || version != result.Version || commands != result.Writes || audits != commands || commands == 0 {
		f.t.Fatalf("exact personal facts differ: query=%v versionMatch=%t commands=%d browserWrites=%d audits=%d roleMatch=%t", e, version == result.Version, commands, result.Writes, audits, role == "admin")
	}
	f.t.Logf("owned user=%s version=%s committed personal commands=%d matching success audits=%d", result.UserID, version, commands, audits)
}

func TestAccountPersonalSettingsWebProfileAndAvatar(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	f := newPersonalWebFixture(t, ctx)
	r := f.browserSettings(ctx, "profile")
	f.requirePersonalFacts(ctx, r)
	var exact bool
	var uploads, liveReaders int
	conn := f.db.Connect(t)
	if e := conn.QueryRow(ctx, `SELECT username='settings-admin' AND display_name='' AND avatar_object_id IS NULL AND initial_password_suggestion FROM agenteam_account.users WHERE id=$1`, r.UserID).Scan(&exact); e != nil || !exact || r.Uploads != 3 || r.Rejected != 3 || r.Writes != 8 {
		t.Fatal("profile/avatar browser matrix or persisted current profile incomplete", e)
	}
	if e := conn.QueryRow(ctx, `SELECT count(*) FROM agenteam_account.avatar_changes a JOIN agenteam_account.commands c ON c.id=a.command_id WHERE a.user_id=$1 AND a.new_object_id IS NOT NULL AND c.phase='committed'`, r.UserID).Scan(&uploads); e != nil || uploads != 3 {
		t.Fatal("three actual avatar publications missing", e)
	}
	if e := conn.QueryRow(ctx, `SELECT count(*) FROM agenteam_object.object_leases WHERE owner_kind='reader' AND state='active'`).Scan(&liveReaders); e != nil || liveReaders != 0 {
		t.Fatal("normal completed avatar reads retain active leases", e)
	}
	t.Log("three static formats uploaded/read back, small re-encoded body below 1MiB, three real rejections retained old avatar, exact removal confirmed; normal read closure only")
}

func TestAccountPersonalSettingsWebThemeAndNavigation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	f := newPersonalWebFixture(t, ctx)
	r := f.browserSettings(ctx, "theme")
	f.requirePersonalFacts(ctx, r)
	var exact bool
	if e := f.db.Connect(t).QueryRow(ctx, `SELECT theme='system' AND username='admin' AND initial_password_suggestion FROM agenteam_account.users WHERE id=$1`, r.UserID).Scan(&exact); e != nil || !exact || r.Writes != 3 {
		t.Fatal("theme facts or confirmed writes differ from preview/conflict matrix", e)
	}
	t.Log("preview/cancel were observed against unchanged DB facts; two-window version conflict retained draft; three leaf returns, dirty menu/back/logout and same-Session checking verified")
}

func TestAccountPersonalSettingsWebPasswordRotation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	f := newPersonalWebFixture(t, ctx)
	r := f.browserSettings(ctx, "password")
	f.requirePersonalFacts(ctx, r)
	if len(r.OldSessionIDs) != 2 || r.OldSessionIDs[0] == r.OldSessionIDs[1] || r.SessionID == "" || r.Writes != 2 {
		t.Fatal("password browser did not identify two prior Sessions and the confirmed replacement")
	}
	conn := f.db.Connect(t)
	for _, old := range r.OldSessionIDs {
		var revoked bool
		if e := conn.QueryRow(ctx, `SELECT revoked_at IS NOT NULL AND revoked_reason='password_changed' FROM agenteam_account.sessions WHERE id=$1 AND user_id=$2`, old, r.UserID).Scan(&revoked); e != nil || !revoked || old == r.SessionID {
			t.Fatal("exact original Session not revoked by password change", e)
		}
	}
	var current, suggestion bool
	if e := conn.QueryRow(ctx, `SELECT s.revoked_at IS NULL,u.initial_password_suggestion FROM agenteam_account.sessions s JOIN agenteam_account.users u ON u.id=s.user_id WHERE s.id=$1 AND u.id=$2`, r.SessionID, r.UserID).Scan(&current, &suggestion); e != nil || !current || suggestion {
		t.Fatal("confirmed rotated Session or initial-password fact incorrect", e)
	}
	t.Logf("password change committed once; two original Sessions revoked; replacement=%s usable for subsequent CSRF write; new-password login and old-password refusal verified", r.SessionID)
}

func TestAccountPersonalSettingsWebAuthorityAndProduction(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	f := newPersonalWebFixture(t, ctx)
	r := f.browserSettings(ctx, "authority")
	f.requirePersonalFacts(ctx, r)
	var member, admin bool
	if e := f.db.Connect(t).QueryRow(ctx, `SELECT role='user' AND display_name='Member settings' FROM agenteam_account.users WHERE id=$1`, f.member.UserID).Scan(&member); e != nil || !member || r.MemberID != f.member.UserID || r.Layouts != 12 || r.Writes != 2 {
		t.Fatal("ordinary-user own-profile or layout matrix incomplete", e)
	}
	if e := f.db.Connect(t).QueryRow(ctx, `SELECT role='admin' AND char_length(display_name)=80 FROM agenteam_account.users WHERE id=$1`, r.UserID).Scan(&admin); e != nil || !admin {
		t.Fatal("administrator long-name profile fact missing", e)
	}
	t.Log("real admin/user own-profile authority, CSRF/body rejection, invalidation, twelve theme/viewport pairs, 200%, keyboard/reduced motion and production routing verified; no production SPA hosting claim")
}
