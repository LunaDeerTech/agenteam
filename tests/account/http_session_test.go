//go:build integration

package account_test

import (
	"bytes"
	"encoding/base64"
	"image"
	"net/url"
	"strings"
	"testing"
)

func TestAccountHTTPSessionReplayPasswordChangeResetAndLogout(t *testing.T) {
	f := newHTTPFixture(t)
	bootstrap := f.record("bootstrap", "admin@mail.com", "")
	admin := f.browser()
	key := id[struct{}](t).String()
	first := admin.login(bootstrap.Email, bootstrap.Password, key).object(t)
	oldCookie := admin.cookie("agenteam_local_session")
	replayed := admin.request("POST", "/api/v1/sessions/login", map[string]any{"email": bootstrap.Email, "password": bootstrap.Password}, key, admin.anonymousCSRF).want(t, 200).object(t)
	if oldCookie == "" || oldCookie != admin.cookie("agenteam_local_session") || httpObject(t, first, "session")["id"] != httpObject(t, replayed, "session")["id"] {
		t.Fatal("login replay issued a different session or cookie")
	}
	other := f.browser()
	other.login(bootstrap.Email, bootstrap.Password, id[struct{}](t).String())
	user := httpObject(t, admin.profile(), "user")
	const changed = "HTTP-Changed-Password-82!"
	change := map[string]any{"version": user["version"], "current_password": bootstrap.Password, "new_password": changed, "confirmation": changed}
	admin.request("POST", "/api/v1/me/change-password", change, id[struct{}](t).String(), admin.csrf).want(t, 200)
	if admin.cookie("agenteam_local_session") == oldCookie {
		t.Fatal("password change failed to rotate the current session")
	}
	other.request("GET", "/api/v1/session", nil, "", "").problem(t, 401, "SESSION_REVOKED")
	if other.cookie("agenteam_local_session") != "" {
		t.Fatal("revoked session cookie was not cleared")
	}
	if httpObject(t, admin.profile(), "user")["initial_password_suggestion"] != false {
		t.Fatal("password change retained the bootstrap suggestion")
	}
	resetBrowser := f.browser()
	resetBrowser.bootstrap()
	known := resetBrowser.request("POST", "/api/v1/password-resets/request", map[string]any{"email": bootstrap.Email}, id[struct{}](t).String(), resetBrowser.anonymousCSRF).want(t, 202)
	unknown := resetBrowser.request("POST", "/api/v1/password-resets/request", map[string]any{"email": "absent-http@example.com"}, id[struct{}](t).String(), resetBrowser.anonymousCSRF).want(t, 202)
	if !bytes.Equal(known.data, unknown.data) {
		t.Fatal("public reset response enumerated account existence")
	}
	entry := f.record("password_reset", bootstrap.Email, "")
	u, err := url.Parse(entry.URL)
	if err != nil || u.Scheme+"://"+u.Host != httpOrigin || u.Path != "/reset-password" || u.Fragment == "" {
		t.Fatal("reset URL did not use the configured origin and fragment")
	}
	resetBrowser.request("POST", "/api/v1/password-resets/inspect", map[string]any{"token": u.Fragment}, "", resetBrowser.anonymousCSRF).want(t, 200)
	const resetPassword = "HTTP-Reset-Password-93!"
	complete := map[string]any{"token": u.Fragment, "new_password": resetPassword, "confirmation": resetPassword}
	completeKey := id[struct{}](t).String()
	resetBrowser.request("POST", "/api/v1/password-resets/complete", complete, completeKey, resetBrowser.anonymousCSRF).want(t, 204)
	resetBrowser.request("POST", "/api/v1/password-resets/complete", complete, completeKey, resetBrowser.anonymousCSRF).want(t, 204)
	admin.request("GET", "/api/v1/session", nil, "", "").problem(t, 401, "SESSION_REVOKED")
	resetBrowser.request("POST", "/api/v1/password-resets/inspect", map[string]any{"token": u.Fragment}, "", resetBrowser.anonymousCSRF).problem(t, 410, "RESOURCE_DELETED")
	resetBrowser.login(bootstrap.Email, resetPassword, id[struct{}](t).String())
	currentCookie := resetBrowser.cookie("agenteam_local_session")
	logout := resetBrowser.request("POST", "/api/v1/sessions/logout", map[string]any{}, id[struct{}](t).String(), resetBrowser.csrf).want(t, 204)
	if len(logout.data) != 0 || resetBrowser.cookie("agenteam_local_session") != "" {
		t.Fatal("logout did not clear its cookie with an empty response")
	}
	resetBrowser.setCookie("agenteam_local_session", currentCookie)
	resetBrowser.request("GET", "/api/v1/me", nil, "", "").problem(t, 401, "SESSION_REVOKED")
	for _, secret := range []string{bootstrap.Password, changed, resetPassword, u.Fragment, oldCookie, currentCookie} {
		if f.log.contains(secret) {
			t.Fatal("ordinary HTTP logs contain password, token or session material")
		}
	}
}

func TestAccountHTTPPublicChallengeRoundTrip(t *testing.T) {
	f := newHTTPFixture(t)
	entry := f.record("bootstrap", "admin@mail.com", "")
	browser := f.browser()
	browser.bootstrap()
	loginKey := id[struct{}](t).String()
	challenge := browser.request("POST", "/api/v1/auth/challenges", map[string]any{"mode": "rotate", "email": entry.Email, "login_key": loginKey}, "", browser.anonymousCSRF).want(t, 201).object(t)
	decode := func(field string) image.Image {
		raw := httpString(t, challenge, field)
		if _, encoded, ok := strings.Cut(raw, ","); ok {
			raw = encoded
		}
		data, err := base64.StdEncoding.DecodeString(raw)
		if err != nil {
			t.Fatal("challenge public image is not valid base64")
		}
		im, _, err := image.Decode(bytes.NewReader(data))
		if err != nil {
			t.Fatal("challenge public image is not decodable")
		}
		return im
	}
	rotation, err := solvePublicRotation(decode("master"), decode("thumb"))
	if err != nil {
		t.Fatal(err)
	}
	proof := map[string]any{"email": entry.Email, "login_key": loginKey, "challenge_id": challenge["id"], "proof": map[string]any{"angle": rotation.Angle}}
	verified := browser.request("POST", "/api/v1/auth/challenges/verify", proof, "", browser.anonymousCSRF).want(t, 200).object(t)
	pass := httpString(t, verified, "pass")
	browser.request("POST", "/api/v1/sessions/login", map[string]any{"email": entry.Email, "password": entry.Password, "challenge_pass": pass}, loginKey, browser.anonymousCSRF).want(t, 200)
	browser.request("GET", "/api/v1/session", nil, "", "").want(t, 200)
	if f.log.contains(pass) || f.log.contains(entry.Password) {
		t.Fatal("challenge/login material leaked to ordinary logs")
	}
}
