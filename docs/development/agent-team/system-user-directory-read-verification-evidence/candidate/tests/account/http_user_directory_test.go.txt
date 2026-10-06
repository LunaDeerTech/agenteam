//go:build integration

package account_test

import (
	"net/url"
	"reflect"
	"strconv"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
)

func TestAccountHTTPSystemUserDirectory(t *testing.T) {
	f := newHTTPFixture(t)
	bootstrap := f.record("bootstrap", "admin@mail.com", "")
	admin := f.browser()
	login := admin.login(bootstrap.Email, bootstrap.Password, id[struct{}](t).String())
	directoryHTTPUserShape(t, httpObject(t, login.object(t), "user"), false)
	member := f.invite(admin, "directory-one@example.com", "directory-one")
	f.invite(admin, "directory-two@example.com", "directory-two")
	for _, browser := range []*httpBrowser{admin, member} {
		session := browser.request("GET", "/api/v1/session", nil, "", "").want(t, 200).object(t)
		directoryHTTPUserShape(t, httpObject(t, session, "user"), false)
		directoryHTTPUserShape(t, httpObject(t, browser.profile(), "user"), false)
	}

	order, created := directoryHTTPDatabaseUsers(t, f)
	if len(order) != 3 {
		t.Fatal("directory fixture did not create bootstrap and two redeemed users")
	}
	full := admin.request("GET", "/api/v1/system/users", nil, "", "").want(t, 200)
	directoryHTTPFullPage(t, full.object(t), order, created)
	head := admin.request("HEAD", "/api/v1/system/users", nil, "", "").want(t, 200)
	if len(head.data) != 0 || head.headers.Get("Content-Length") != strconv.Itoa(len(full.data)) || head.headers.Get("Content-Type") != full.headers.Get("Content-Type") {
		t.Fatal("HEAD did not execute the directory representation without a body")
	}

	profile := httpObject(t, member.profile(), "user")
	memberID := httpString(t, profile, "id")
	changed := member.request("PATCH", "/api/v1/me", map[string]any{
		"version": profile["version"], "username": "directory-renamed", "display_name": "Directory renamed",
	}, id[struct{}](t).String(), member.csrf).want(t, 200).object(t)
	directoryHTTPUserShape(t, httpObject(t, changed, "user"), false)
	afterOrder, afterCreated := directoryHTTPDatabaseUsers(t, f)
	if !reflect.DeepEqual(created, afterCreated) || !reflect.DeepEqual(order, afterOrder) {
		t.Fatal("profile update changed persisted registration time or directory order")
	}
	updated := admin.request("GET", "/api/v1/system/users?limit=100", nil, "", "").want(t, 200).object(t)
	directoryHTTPFullPage(t, updated, order, created)
	found := false
	for _, raw := range httpItems(t, updated) {
		item := raw.(map[string]any)
		if item["id"] == memberID {
			found = true
			if item["username"] != "directory-renamed" || item["display_name"] != "Directory renamed" || item["version"] == profile["version"] {
				t.Fatal("directory did not combine current profile fields with original registration time")
			}
		}
	}
	if !found {
		t.Fatal("updated member disappeared from the directory")
	}

	// Only this owned database's two redeemed users share an adjusted timestamp.
	// The real creation timestamps were compared before this ordering fixture.
	tie := time.Date(2026, 10, 5, 1, 2, 3, 456789000, time.UTC)
	conn := f.db.Connect(t)
	if tag, err := conn.Exec(ctxFor(t), `UPDATE agenteam_account.users SET created_at=$1 WHERE email IN ('directory-one@example.com','directory-two@example.com')`, tie); err != nil || tag.RowsAffected() != 2 {
		t.Fatal("could not set owned equal-time directory fixture", err)
	}
	order, created = directoryHTTPDatabaseUsers(t, f)
	var paged []string
	seen := map[string]bool{}
	cursor := ""
	for pageNumber := 0; ; pageNumber++ {
		if pageNumber >= len(order)+1 {
			t.Fatal("directory cursor did not terminate")
		}
		path := "/api/v1/system/users?limit=1"
		if cursor != "" {
			path += "&cursor=" + url.QueryEscape(cursor)
		}
		pageResult := admin.request("GET", path, nil, "", "").want(t, 200)
		directoryHTTPHeadMatches(t, admin, path, pageResult)
		page := pageResult.object(t)
		items := httpItems(t, page)
		if len(items) != 1 {
			t.Fatal("one-item directory page ignored its limit or lost an item")
		}
		item := items[0].(map[string]any)
		directoryHTTPUserShape(t, item, true)
		userID := httpString(t, item, "id")
		if seen[userID] || item["created_at"] != created[userID] {
			t.Fatal("directory pagination duplicated an item or changed its registration time")
		}
		seen[userID] = true
		paged = append(paged, userID)
		next, hasNext := page["next_cursor"]
		if !hasNext {
			break
		}
		cursor, _ = next.(string)
		if cursor == "" || len(page) != 2 {
			t.Fatal("directory cursor was empty or page contained extra fields")
		}
		if pageNumber == 0 {
			remainder := admin.request("GET", "/api/v1/system/users?limit=100&cursor="+url.QueryEscape(cursor), nil, "", "").want(t, 200).object(t)
			directoryHTTPFullPage(t, remainder, order[1:], created)
		}
	}
	if !reflect.DeepEqual(paged, order) {
		t.Fatal("directory pagination lost created_at DESC,id DESC ordering or omitted users")
	}

	guest := f.browser()
	for _, denied := range []struct {
		browser *httpBrowser
		status  int
		code    string
	}{{member, 403, "FORBIDDEN"}, {guest, 401, "UNAUTHENTICATED"}} {
		denied.browser.request("GET", "/api/v1/system/users", nil, "", "").problem(t, denied.status, denied.code)
		if result := denied.browser.request("HEAD", "/api/v1/system/users", nil, "", "").want(t, denied.status); len(result.data) != 0 {
			t.Fatal("denied HEAD exposed a response body")
		}
	}
	for _, email := range []string{"directory-pending-one@example.com", "directory-pending-two@example.com"} {
		admin.request("POST", "/api/v1/system/invitations", map[string]any{"email": email}, id[struct{}](t).String(), admin.csrf).want(t, 201)
	}
	invitations := admin.request("GET", "/api/v1/system/invitations?limit=1", nil, "", "").want(t, 200).object(t)
	invitationCursor := httpString(t, invitations, "next_cursor")
	for _, tc := range []struct{ query, code string }{
		{"limit=01", "INVALID_ARGUMENT"},
		{"cursor=invalid", "CURSOR_INVALID"},
		{"cursor=" + url.QueryEscape(cursor+"x"), "CURSOR_INVALID"},
		{"cursor=" + url.QueryEscape(invitationCursor), "CURSOR_INVALID"},
	} {
		path := "/api/v1/system/users?" + tc.query
		result := admin.request("GET", path, nil, "", "")
		result.problem(t, 400, tc.code)
		directoryHTTPHeadMatches(t, admin, path, result)
	}
	// PostgreSQL supports this year, while the account Instant wire type does
	// not. Only this owned fixture row is changed, then restored after both reads.
	if tag, err := conn.Exec(ctxFor(t), `UPDATE agenteam_account.users SET created_at='10000-01-01 00:00:00+00'::timestamptz WHERE id=$1`, memberID); err != nil || tag.RowsAffected() != 1 {
		t.Fatal("could not set owned invalid-time directory fixture", err)
	}
	failed := admin.request("GET", "/api/v1/system/users", nil, "", "")
	failed.problem(t, 503, "DEPENDENCY_UNAVAILABLE")
	for _, key := range []string{"items", "next_cursor", "created_at", "user"} {
		if _, ok := failed.object(t)[key]; ok {
			t.Fatal("failed directory read published candidate data", key)
		}
	}
	directoryHTTPHeadMatches(t, admin, "/api/v1/system/users", failed)
	if tag, err := conn.Exec(ctxFor(t), `UPDATE agenteam_account.users SET created_at=$2 WHERE id=$1`, memberID, tie); err != nil || tag.RowsAffected() != 1 {
		t.Fatal("could not restore owned invalid-time directory fixture", err)
	}
	revoked := admin.cookie("agenteam_local_session")
	admin.request("POST", "/api/v1/sessions/logout", map[string]any{}, id[struct{}](t).String(), admin.csrf).want(t, 204)
	admin.setCookie("agenteam_local_session", revoked)
	admin.request("GET", "/api/v1/system/users", nil, "", "").problem(t, 401, "SESSION_REVOKED")
	admin.setCookie("agenteam_local_session", revoked)
	if result := admin.request("HEAD", "/api/v1/system/users", nil, "", "").want(t, 401); len(result.data) != 0 {
		t.Fatal("revoked HEAD exposed a response body")
	}
	if f.log.contains(bootstrap.Password) || f.log.contains(revoked) {
		t.Fatal("directory requests exposed account material in ordinary logs")
	}
}

func directoryHTTPHeadMatches(t *testing.T, browser *httpBrowser, path string, get httpResult) {
	t.Helper()
	head := browser.request("HEAD", path, nil, "", "").want(t, get.status)
	if len(head.data) != 0 || head.headers.Get("Content-Type") != get.headers.Get("Content-Type") || head.headers.Get("Content-Length") != strconv.Itoa(len(get.data)) {
		t.Fatal("HEAD query did not preserve GET status/metadata with no response body")
	}
}

func directoryHTTPUserShape(t *testing.T, user map[string]any, system bool) {
	t.Helper()
	fields := []string{"id", "email", "username", "display_name", "role", "theme", "version", "initial_password_suggestion"}
	if system {
		fields = append(fields, "created_at")
		value := httpString(t, user, "created_at")
		at, err := foundation.ParseInstant(value)
		if err != nil || at.String() != value {
			t.Fatal("directory registration time is not canonical UTC microseconds", err)
		}
	}
	if len(user) != len(fields) {
		t.Fatalf("User fields=%d want=%d", len(user), len(fields))
	}
	for _, field := range fields {
		if value, ok := user[field]; !ok || value == nil {
			t.Fatalf("User field %s is missing or null", field)
		}
	}
	if _, err := foundation.ParseVersion(httpString(t, user, "version")); err != nil {
		t.Fatal("directory version is not a decimal string", err)
	}
}

func directoryHTTPDatabaseUsers(t *testing.T, f *httpFixture) ([]string, map[string]string) {
	t.Helper()
	rows, err := f.db.Connect(t).Query(ctxFor(t), `SELECT id::text,created_at FROM agenteam_account.users ORDER BY created_at DESC,id DESC`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var order []string
	created := map[string]string{}
	for rows.Next() {
		var userID string
		var at time.Time
		if err := rows.Scan(&userID, &at); err != nil {
			t.Fatal(err)
		}
		canonical, err := foundation.NewInstant(at)
		if err != nil {
			t.Fatal(err)
		}
		order = append(order, userID)
		created[userID] = canonical.String()
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return order, created
}

func directoryHTTPFullPage(t *testing.T, page map[string]any, order []string, created map[string]string) {
	t.Helper()
	items := httpItems(t, page)
	if len(items) != len(order) || len(page) != 1 {
		t.Fatal("complete directory page has an incorrect item count or unexpected cursor")
	}
	for i, raw := range items {
		item, ok := raw.(map[string]any)
		if !ok {
			t.Fatal("directory item is not an object")
		}
		directoryHTTPUserShape(t, item, true)
		if item["id"] != order[i] || item["created_at"] != created[order[i]] {
			t.Fatal("directory does not match persisted registration time and ordering")
		}
	}
}
