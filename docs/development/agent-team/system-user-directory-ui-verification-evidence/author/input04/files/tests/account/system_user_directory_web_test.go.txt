//go:build integration

package account_test

import (
	"context"
	"reflect"
	"testing"
	"time"
)

func TestAccountSystemUserDirectoryWebReadAndPagination(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	f := newSystemDirectoryWebFixture(t, ctx)
	r := f.browserDirectory(ctx, "read")
	if !r.Canonical || r.Pages != 4 || r.Rows != len(f.expected) || !reflect.DeepEqual(f.expected, f.databaseUsers(ctx)) {
		t.Fatal("directory wire/display facts, canonical timestamps or four fresh observations differ")
	}
	t.Log("real bootstrap/invitation identities plus 28 explicitly inserted read-only samples: first/next/previous/refresh verified against canonical database fields; no snapshot claim")
}

func TestAccountSystemUserDirectoryWebAuthorityAndIdentity(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	f := newSystemDirectoryWebFixture(t, ctx)
	r := f.browserDirectory(ctx, "authority")
	if r.Denied != 2 || !r.Revoked || !r.Demoted || !r.Switched || f.revokedSession == "" || f.demotedSession == "" || f.revokedSession == f.demotedSession {
		t.Fatal("directory current authority and explicit identity-switch matrix incomplete")
	}
	var exact bool
	if err := f.db.Connect(t).QueryRow(ctx, `SELECT u.role='user' AND s.revoked_at IS NOT NULL AND s.revoked_reason='administrative' FROM agenteam_account.users u JOIN agenteam_account.sessions s ON s.user_id=u.id WHERE u.id=$1 AND s.id=$2`, f.admin.UserID, f.revokedSession).Scan(&exact); err != nil || !exact {
		t.Fatal("owned role and exact revoked Session facts differ", err)
	}
	t.Log("normal direct link did not issue directory GET; owned administrative Session revocation and role demotion produced actual backend 401/403; explicit account switch retired old data")
}

func TestAccountSystemUserDirectoryWebNavigationAndLayouts(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	f := newSystemDirectoryWebFixture(t, ctx)
	r := f.browserDirectory(ctx, "navigation")
	if !r.Navigation || r.Layouts != 8 {
		t.Fatal("directory navigation and light/dark four-width matrix incomplete")
	}
	t.Log("real administrator entry/login return, personal draft continue/discard, top logout, light/dark x 1440/1024/834/390, long names/email, keyboard/focus, reduced motion and no page overflow; production dist fixture only, no native zoom or deployment claim")
}
