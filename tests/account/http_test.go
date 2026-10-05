//go:build integration

package account_test

import (
	"bytes"
	"net/http"
	"strings"
	"testing"
)

func TestAccountHTTPBoundaryAndProfile(t *testing.T) {
	f := newHTTPFixture(t)
	guest := f.browser()
	unauthenticated := guest.request("GET", "/api/v1/me", nil, "", "")
	unauthenticated.problem(t, 401, "UNAUTHENTICATED")
	f.assertTrace(unauthenticated, "/api/v1/me")
	bootstrap := guest.bootstrap()
	for _, c := range (&http.Response{Header: bootstrap.headers}).Cookies() {
		if c.Name != "agenteam_local_browser" || !c.HttpOnly || c.Secure || c.Path != "/" || c.Domain != "" || c.SameSite != http.SameSiteLaxMode {
			t.Fatal("local anonymous cookie lost its fixed browser attributes")
		}
	}
	if len(bootstrap.headers.Values("Set-Cookie")) != 1 {
		t.Fatal("bootstrap must issue exactly one browser cookie")
	}
	for _, change := range []func(*http.Request){
		func(r *http.Request) { r.Header.Set("Origin", "https://foreign.invalid") },
		func(r *http.Request) { r.Header.Del("Origin") },
		func(r *http.Request) { r.Header.Set("Sec-Fetch-Site", "cross-site") },
		func(r *http.Request) {
			r.Host = "foreign.invalid"
			r.Header.Set("X-Forwarded-Host", "localhost:8080")
			r.Header.Set("X-Forwarded-Proto", "http")
		},
	} {
		guest.raw("POST", "/api/v1/sessions/login", []byte(`{invalid`), change).problem(t, 403, "ORIGIN_DENIED")
	}
	guest.request("POST", "/api/v1/sessions/login", map[string]any{}, id[struct{}](t).String(), "").problem(t, 403, "CSRF_FAILED")
	admin := f.admin()
	initial := admin.profile()
	user := httpObject(t, initial, "user")
	version := httpString(t, user, "version")
	if user["role"] != "admin" || user["initial_password_suggestion"] != true || initial["avatar"] != nil {
		t.Fatal("bootstrap current user projection is wrong")
	}
	admin.raw("GET", "/api/v1/me", nil, func(r *http.Request) {
		r.Header.Set("X-Forwarded-Host", "foreign.invalid")
		r.Header.Set("X-Forwarded-Proto", "https")
	}).want(t, 200)
	for _, data := range []string{
		`{"version":"` + version + `","display_name":"a","display_name":"b"}`,
		`{"version":"` + version + `","display_name":null}`,
		`{"version":"` + version + `","email":"other@example.com"}`,
		`{"version":1,"display_name":"a"}`,
	} {
		admin.raw("PATCH", "/api/v1/me", []byte(data), func(r *http.Request) {
			r.Header.Set("X-CSRF-Token", admin.csrf)
			r.Header.Set("Idempotency-Key", id[struct{}](t).String())
		}).problem(t, 400, "INVALID_ARGUMENT")
	}
	patch := map[string]any{"version": version, "username": "http-admin", "display_name": "HTTP Administrator"}
	admin.request("PATCH", "/api/v1/me", patch, id[struct{}](t).String(), guest.anonymousCSRF).problem(t, 403, "CSRF_FAILED")
	admin.request("PATCH", "/api/v1/me", patch, "", admin.csrf).problem(t, 400, "INVALID_ARGUMENT")
	key := id[struct{}](t).String()
	applied := admin.request("PATCH", "/api/v1/me", patch, key, admin.csrf).want(t, 200)
	replay := admin.request("PATCH", "/api/v1/me", patch, key, admin.csrf).want(t, 200)
	if !bytes.Equal(applied.data, replay.data) {
		t.Fatal("profile same-command replay changed the receipt")
	}
	patch["display_name"] = "different semantics"
	admin.request("PATCH", "/api/v1/me", patch, key, admin.csrf).problem(t, 409, "IDEMPOTENCY_KEY_REUSED")
	admin.request("PATCH", "/api/v1/me", patch, id[struct{}](t).String(), admin.csrf).problem(t, 409, "VERSION_CONFLICT")
	prefs := admin.request("GET", "/api/v1/me/preferences", nil, "", "").want(t, 200).object(t)
	newPrefs := admin.request("PUT", "/api/v1/me/preferences", map[string]any{"version": prefs["version"], "theme": "dark"}, id[struct{}](t).String(), admin.csrf).want(t, 200).object(t)
	if newPrefs["theme"] != "dark" || newPrefs["version"] == prefs["version"] {
		t.Fatal("preferences were not persisted with a new user version")
	}
	current := httpObject(t, admin.profile(), "user")
	if current["username"] != "http-admin" || current["theme"] != "dark" || current["initial_password_suggestion"] != true {
		t.Fatal("profile save changed unrelated bootstrap/password state")
	}
	admin.request("GET", "/api/v1/me?email=private-marker", nil, "", "").problem(t, 400, "INVALID_ARGUMENT")
	unknown := admin.request("GET", "/api/v1/private-capability-marker", nil, "", "")
	unknown.problem(t, 404, "NOT_FOUND")
	if strings.Contains(string(unknown.data), "private-capability-marker") {
		t.Fatal("Problem reflected an unrecognized capability-like path")
	}
	if f.log.contains("private-capability-marker") {
		t.Fatal("ordinary request logging reflected an unrecognized path")
	}
}
