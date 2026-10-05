//go:build integration

package account_test

import (
	"bytes"
	"net/url"
	"testing"
)

func TestAccountHTTPAdministratorPagesSettingsAndCurrentAuthority(t *testing.T) {
	f := newHTTPFixture(t)
	admin := f.admin()
	member := f.invite(admin, "http-member@example.com", "http-member")
	f.invite(admin, "http-member-two@example.com", "http-member-two")
	for _, path := range []string{"users", "invitations", "account-settings", "smtp", "mail-jobs"} {
		member.request("GET", "/api/v1/system/"+path, nil, "", "").problem(t, 403, "FORBIDDEN")
	}
	first := admin.request("GET", "/api/v1/system/users?limit=1", nil, "", "").want(t, 200).object(t)
	cursor := httpString(t, first, "next_cursor")
	if len(httpItems(t, first)) != 1 {
		t.Fatal("first user page ignored its limit")
	}
	second := admin.request("GET", "/api/v1/system/users?limit=100&cursor="+url.QueryEscape(cursor), nil, "", "").want(t, 200).object(t)
	if len(httpItems(t, second)) != 2 {
		t.Fatal("user page did not resume with the independently changed limit")
	}
	firstID := httpItems(t, first)[0].(map[string]any)["id"]
	for _, item := range httpItems(t, second) {
		if item.(map[string]any)["id"] == firstID {
			t.Fatal("pagination duplicated the boundary row")
		}
	}
	admin.request("GET", "/api/v1/system/invitations?cursor="+url.QueryEscape(cursor), nil, "", "").problem(t, 400, "CURSOR_INVALID")
	admin.request("GET", "/api/v1/system/users?limit=01", nil, "", "").problem(t, 400, "INVALID_ARGUMENT")
	admin.request("GET", "/api/v1/system/users?cursor="+url.QueryEscape(cursor+"x"), nil, "", "").problem(t, 400, "CURSOR_INVALID")
	settings := admin.request("GET", "/api/v1/system/account-settings", nil, "", "").want(t, 200).object(t)
	put := map[string]any{"version": settings["version"], "session_idle_seconds": "3600", "session_absolute_seconds": "7200", "password_reset_seconds": "900", "challenge_after_failures": "4"}
	settingsKey := id[struct{}](t).String()
	applied := admin.request("PUT", "/api/v1/system/account-settings", put, settingsKey, admin.csrf).want(t, 200)
	if !bytes.Equal(applied.data, admin.request("PUT", "/api/v1/system/account-settings", put, settingsKey, admin.csrf).want(t, 200).data) {
		t.Fatal("settings replay changed the persisted receipt")
	}
	if applied.object(t)["lifetime_changes_apply_to"] != "newly_issued_sessions_and_tokens" {
		t.Fatal("settings omitted its lifetime-change scope")
	}
	admin.request("PUT", "/api/v1/system/account-settings", put, id[struct{}](t).String(), admin.csrf).problem(t, 409, "VERSION_CONFLICT")
	// The session already issued before this save must retain its own lifetime.
	conn := f.db.Connect(t)
	var idle int64
	if err := conn.QueryRow(ctxFor(t), `SELECT idle_seconds FROM agenteam_account.sessions WHERE user_id=(SELECT id FROM agenteam_account.users WHERE email='admin@mail.com') AND revoked_at IS NULL`).Scan(&idle); err != nil || idle != 604800 {
		t.Fatal("settings retroactively changed an issued session", err)
	}
	smtp := admin.request("GET", "/api/v1/system/smtp", nil, "", "").want(t, 200).object(t)
	if smtp["configured"] != false || smtp["credential_present"] != false {
		t.Fatal("fresh SMTP settings are not empty")
	}
	const password = "HTTP-SMTP-private-sentinel-82!"
	configure := map[string]any{"version": smtp["version"], "host": "smtp.fixture.invalid.test", "port": 587, "encryption": "starttls", "username": "test-user", "sender_email": "sender@example.com", "sender_name": "HTTP test", "credential_action": "replace", "password": password, "auto_retry_count": "2", "retry_interval_seconds": "120"}
	admin.request("PUT", "/api/v1/system/smtp", configure, id[struct{}](t).String(), admin.csrf).problem(t, 400, "INVALID_ARGUMENT")
	configure["credential_action"] = "keep" // A nonempty password performs replacement.
	configureKey := id[struct{}](t).String()
	configured := admin.request("PUT", "/api/v1/system/smtp", configure, configureKey, admin.csrf).want(t, 200).object(t)
	view := httpObject(t, configured, "settings")
	if view["configured"] != true || view["credential_present"] != true {
		t.Fatal("SMTP configuration did not install its real credential reference")
	}
	admin.request("POST", "/api/v1/system/smtp/unconfigure", map[string]any{"version": view["version"]}, id[struct{}](t).String(), admin.csrf).problem(t, 400, "INVALID_ARGUMENT")
	removed := admin.request("POST", "/api/v1/system/smtp/unconfigure", map[string]any{"version": view["version"], "auto_retry_count": "1", "retry_interval_seconds": "300"}, id[struct{}](t).String(), admin.csrf).want(t, 200).object(t)
	removedView := httpObject(t, removed, "settings")
	if removedView["configured"] != false || removedView["credential_present"] != false || removedView["auto_retry_count"] != "1" || removedView["retry_interval_seconds"] != "300" {
		t.Fatal("unconfigure lost its explicitly chosen retry policy or kept credentials")
	}
	oldReplay := admin.request("PUT", "/api/v1/system/smtp", configure, configureKey, admin.csrf).want(t, 200)
	oldBody := oldReplay.object(t)
	if oldBody["applied_version"] != configured["applied_version"] || httpObject(t, oldBody, "settings")["version"] != removedView["version"] || httpObject(t, oldBody, "settings")["configured"] != false {
		t.Fatal("SMTP replay confused the original receipt with current settings")
	}
	if bytes.Contains(oldReplay.data, []byte(password)) || f.log.contains(password) {
		t.Fatal("SMTP credential leaked out of its input/Secret boundary")
	}
	var hasReference bool
	if err := conn.QueryRow(ctxFor(t), `SELECT password_ref IS NOT NULL FROM agenteam_account.smtp_settings WHERE singleton`).Scan(&hasReference); err != nil || hasReference {
		t.Fatal("unconfigure did not cut the real SMTP credential reference", err)
	}
	// A committed role change is an external test fact. The following requests
	// must consult current authority instead of the session or cursor snapshot.
	if _, err := conn.Exec(ctxFor(t), `UPDATE agenteam_account.users SET role=CASE email WHEN 'admin@mail.com' THEN 'user' ELSE 'admin' END,version=version+1 WHERE email IN ('admin@mail.com','http-member@example.com')`); err != nil {
		t.Fatal(err)
	}
	admin.request("GET", "/api/v1/system/users?cursor="+url.QueryEscape(cursor), nil, "", "").problem(t, 403, "FORBIDDEN")
	admin.request("PUT", "/api/v1/system/account-settings", put, settingsKey, admin.csrf).problem(t, 403, "FORBIDDEN")
	admin.request("PUT", "/api/v1/system/smtp", configure, configureKey, admin.csrf).problem(t, 403, "FORBIDDEN")
	member.request("GET", "/api/v1/system/users", nil, "", "").want(t, 200)
}

func TestAccountHTTPInvitationAndMailAdministration(t *testing.T) {
	f := newHTTPFixture(t)
	admin := f.admin()
	create := map[string]any{"email": "pending-http@example.com"}
	key := id[struct{}](t).String()
	created := admin.request("POST", "/api/v1/system/invitations", create, key, admin.csrf).want(t, 201)
	if !bytes.Equal(created.data, admin.request("POST", "/api/v1/system/invitations", create, key, admin.csrf).want(t, 201).data) {
		t.Fatal("invitation creation did not replay its stable receipt")
	}
	receipt := created.object(t)
	invitation := httpString(t, receipt, "id")
	job := httpString(t, receipt, "job_id")
	entry := f.record("invitation", "pending-http@example.com", invitation)
	u, err := url.Parse(entry.URL)
	if err != nil {
		t.Fatal("invalid owned invitation URL")
	}
	admin.waitMail(job, "sent")
	page := admin.request("GET", "/api/v1/system/invitations?limit=1", nil, "", "").want(t, 200).object(t)
	if len(httpItems(t, page)) != 1 || httpItems(t, page)[0].(map[string]any)["id"] != invitation {
		t.Fatal("pending invitation list lost its committed item")
	}
	jobs := admin.request("GET", "/api/v1/system/mail-jobs?limit=1", nil, "", "").want(t, 200).object(t)
	if len(httpItems(t, jobs)) != 1 {
		t.Fatal("durable mail list is empty after delivery")
	}
	admin.request("POST", "/api/v1/system/invitations/"+invitation+"/resend", map[string]any{"version": receipt["version"]}, id[struct{}](t).String(), admin.csrf).problem(t, 429, "RATE_LIMITED")
	// Set only this owned record's delivery clock, preserving link and expiry.
	conn := f.db.Connect(t)
	if _, err := conn.Exec(ctxFor(t), `UPDATE agenteam_account.invitations SET last_delivery_at=clock_timestamp()-interval '61 seconds' WHERE id=$1`, invitation); err != nil {
		t.Fatal(err)
	}
	resend := admin.request("POST", "/api/v1/system/invitations/"+invitation+"/resend", map[string]any{"version": receipt["version"]}, id[struct{}](t).String(), admin.csrf).want(t, 202).object(t)
	admin.waitMail(httpString(t, resend, "job_id"), "sent")
	guest := f.browser()
	guest.bootstrap()
	// Resending a still-valid invitation preserves its original link/lifetime.
	inspected := guest.request("POST", "/api/v1/invitations/inspect", map[string]any{"token": u.Fragment}, "", guest.anonymousCSRF).want(t, 200).object(t)
	if inspected["expires_at"] != httpItems(t, page)[0].(map[string]any)["expires_at"] {
		t.Fatal("resend extended the invitation lifetime")
	}
	// A completed cycle can be retried while the same origin remains live.
	sent := admin.request("GET", "/api/v1/system/mail-jobs/"+job, nil, "", "").want(t, 200).object(t)
	retryBody := map[string]any{"version": sent["version"]}
	retryKey := id[struct{}](t).String()
	retried := admin.request("POST", "/api/v1/system/mail-jobs/"+job+"/retry", retryBody, retryKey, admin.csrf).want(t, 202).object(t)
	retriedJob := httpString(t, retried, "job_id")
	if retriedJob == job {
		t.Fatal("manual retry reused the completed job cycle")
	}
	admin.waitMail(retriedJob, "sent")
	replayed := admin.request("POST", "/api/v1/system/mail-jobs/"+job+"/retry", retryBody, retryKey, admin.csrf).want(t, 202).object(t)
	if httpString(t, replayed, "job_id") != retriedJob {
		t.Fatal("manual retry replay created a second delivery cycle")
	}
	var terminal, joined bool
	var phase string
	var cycles int
	if err := conn.QueryRow(ctxFor(t), `SELECT j.phase,a.terminal,a.io_joined,(SELECT count(*) FROM agenteam_account.delivery_intents WHERE id IN (SELECT id FROM agenteam_account.commands WHERE namespace='account.mail-retry' AND command_key=$2)) FROM agenteam_account.mail_jobs j JOIN agenteam_account.mail_attempts a ON a.id=j.current_attempt_id WHERE j.id=$1`, retriedJob, retryKey).Scan(&phase, &terminal, &joined, &cycles); err != nil || phase != "sent" || !terminal || !joined || cycles != 1 {
		t.Fatal("manual retry did not commit one actually joined delivery cycle", err)
	}
	admin.request("POST", "/api/v1/system/invitations/"+invitation+"/revoke", map[string]any{"version": resend["version"]}, id[struct{}](t).String(), admin.csrf).want(t, 204)
	if len(httpItems(t, admin.request("GET", "/api/v1/system/invitations", nil, "", "").want(t, 200).object(t))) != 0 {
		t.Fatal("revoked invitation remains in the pending list")
	}
	guest.request("POST", "/api/v1/invitations/inspect", map[string]any{"token": u.Fragment}, "", guest.anonymousCSRF).problem(t, 410, "RESOURCE_DELETED")
	sent = admin.request("GET", "/api/v1/system/mail-jobs/"+job, nil, "", "").want(t, 200).object(t)
	admin.request("POST", "/api/v1/system/mail-jobs/"+job+"/retry", map[string]any{"version": sent["version"]}, id[struct{}](t).String(), admin.csrf).problem(t, 410, "RESOURCE_DELETED")
	admin.request("GET", "/api/v1/system/mail-jobs/"+id[struct{}](t).String(), nil, "", "").problem(t, 404, "NOT_FOUND")
	admin.request("POST", "/api/v1/system/smtp/test", map[string]any{"recipient": "receiver@example.com"}, id[struct{}](t).String(), admin.csrf).problem(t, 409, "INVALID_STATE")
	if f.log.contains(u.Fragment) {
		t.Fatal("mail administration leaked a link token to ordinary logs")
	}
}
