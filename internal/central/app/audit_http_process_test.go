//go:build integration

package app

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

func TestSystemAuditHTTPRootProducerBinding(t *testing.T) {
	started := time.Now()
	a := newModelRootApp(t, "3s", nil)
	address := a.address(t)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	login, err := fixtureAccountLoginResponse(ctx, a.cfg, a.core)
	if err != nil {
		t.Fatal("formal root login", err)
	}
	var material sc.SecretMaterial
	if err = login.UseCookie(func(raw []byte) error { var err error; material, err = sc.NewSecretMaterial(raw); return err }); err != nil {
		t.Fatal(err)
	}
	defer material.Destroy()
	if err = login.Close(ctx); err != nil {
		t.Fatal(err)
	}
	session, err := a.core.GetSession(ctx, material)
	if err != nil {
		t.Fatal(err)
	}
	defer session.CSRF.Destroy()
	var cookie, csrf string
	if err = material.Use(func(raw []byte) error { cookie = string(raw); return nil }); err != nil {
		t.Fatal(err)
	}
	if err = session.CSRF.Use(func(raw []byte) error { csrf = string(raw); return nil }); err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Transport: &http.Transport{Proxy: nil}, Timeout: 5 * time.Second}
	defer client.CloseIdleConnections()
	call := func(method, path string, payload any, status int) map[string]any {
		t.Helper()
		var raw []byte
		var err error
		if payload != nil {
			raw, err = json.Marshal(payload)
			if err != nil {
				t.Fatal(err)
			}
		}
		request, err := http.NewRequestWithContext(ctx, method, address+path, bytes.NewReader(raw))
		if err != nil {
			t.Fatal(err)
		}
		request.Host = "localhost:8080"
		request.Header.Set("Origin", "http://localhost:8080")
		request.AddCookie(&http.Cookie{Name: "agenteam_local_session", Value: cookie})
		if method != "GET" {
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("X-CSRF-Token", csrf)
			request.Header.Set("Idempotency-Key", guardID[struct{}](t).String())
		}
		response, err := client.Do(request)
		if err != nil {
			t.Fatal("root HTTP request", err)
		}
		body, readErr := io.ReadAll(io.LimitReader(response.Body, (1<<20)+1))
		closeErr := response.Body.Close()
		if readErr != nil || closeErr != nil || response.StatusCode != status || len(body) > 1<<20 {
			t.Fatal("root response incomplete/status", response.StatusCode)
		}
		if len(response.Header.Values("X-Request-ID")) != 1 {
			t.Fatal("root request identity duplicated")
		}
		if _, err = foundation.ParseID[foundation.Request](response.Header.Get("X-Request-ID")); err != nil {
			t.Fatal("root request identity malformed")
		}
		if strings.HasPrefix(path, "/api/") && (response.Header.Get("Cache-Control") != "no-store" || response.Header.Get("X-Content-Type-Options") != "nosniff" || response.Header.Get("Referrer-Policy") != "no-referrer") {
			t.Fatal("root shared security headers lost")
		}
		var result map[string]any
		if json.Unmarshal(body, &result) != nil {
			t.Fatal("root JSON malformed")
		}
		return result
	}
	me := call("GET", "/api/v1/me", nil, 200)
	user, ok := me["user"].(map[string]any)
	if !ok {
		t.Fatal("profile prerequisite")
	}
	userID, ok := user["id"].(string)
	if !ok {
		t.Fatal("profile identity prerequisite")
	}
	call("PATCH", "/api/v1/me", map[string]any{"version": user["version"], "display_name": "Audit root private profile"}, 200)
	receipt := call("PUT", "/api/v1/system/outbound-policy", map[string]any{"expected_version": "1", "rules": []any{}}, 200)
	policyAudit, ok := receipt["audit_id"].(string)
	if !ok {
		t.Fatal("formal policy receipt lacks Audit ID")
	}
	var profileAudit string
	if err = a.store.QueryRow(ctx, `SELECT id::text FROM agenteam_audit.audit_records WHERE action='account.profile.update' AND resource_id=$1 ORDER BY created_at DESC,id DESC LIMIT 1`, userID).Scan(&profileAudit); err != nil {
		t.Fatal(err)
	}
	for _, want := range []struct{ id, action, resource string }{{profileAudit, "account.profile.update", "user"}, {policyAudit, "outbound.policy.update", "outbound_policy"}} {
		detail := call("GET", "/api/v1/system/audit/"+want.id, nil, 200)
		if len(detail) != 10 || detail["audit_id"] != want.id || detail["action"] != want.action || detail["scope"] != "system" {
			t.Fatal("root producer not reachable through same bound auditor")
		}
		resource, ok := detail["resource"].(map[string]any)
		if !ok || resource["kind"] != want.resource {
			t.Fatal("root safe resource projection")
		}
		values := url.Values{"action": {want.action}, "limit": {"1"}}
		page := call("GET", "/api/v1/system/audit?"+values.Encode(), nil, 200)
		items, ok := page["items"].([]any)
		if !ok || len(items) != 1 || items[0].(map[string]any)["audit_id"] != want.id {
			t.Fatal("root filtered page omitted formal producer")
		}
	}
	// Existing chain endpoints retain their original handler and ready boundary.
	call("GET", "/api/v1/session", nil, 200)
	call("GET", "/api/v1/system/model-providers", nil, 200)
	current := call("GET", "/api/v1/system/outbound-policy", nil, 200)
	if current["version"] != "2" {
		t.Fatal("Audit binding replaced old policy handler")
	}
	call("GET", "/readyz", nil, 503)
	var profiles, policies int
	if err = a.store.QueryRow(ctx, `SELECT count(*) FILTER(WHERE action='account.profile.update'),count(*) FILTER(WHERE action='outbound.policy.update') FROM agenteam_audit.audit_records`).Scan(&profiles, &policies); err != nil || profiles != 1 || policies != 1 {
		t.Fatal("management GET duplicated producer Audit", err)
	}
	for _, sensitive := range []string{cookie, csrf, profileAudit, policyAudit, "Audit root private profile"} {
		if strings.Contains(a.logs.String(), sensitive) {
			t.Fatal("root ordinary log exposed audit or browser material")
		}
	}
	a.signals <- syscall.SIGTERM
	await(t, a.done)
	if a.err != nil || !a.owned.accounts().Joined() {
		t.Fatal("actual root normal shutdown not joined", a.err)
	}
	if time.Since(started) > 2*time.Minute {
		t.Fatal("root scenario exceeded two-minute budget")
	}
	t.Log("root's existing Account + Outbound producer Audit read by bound new handler; original Model/Account/Outbound and ready503 preserved; normal shutdown joined")
}
