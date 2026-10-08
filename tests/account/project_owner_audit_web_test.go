//go:build integration

package account_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"
)

type projectOwnerAuditWebResult struct {
	Completed        bool            `json:"completed"`
	Mode             string          `json:"mode"`
	Checks           map[string]bool `json:"checks"`
	BrowserAuditGETs int             `json:"browser_audit_gets"`
	SchemaBodies     int             `json:"schema_bodies"`
	ClientBodies     int             `json:"client_bodies"`
	Layouts          int             `json:"layouts"`
}

var projectOwnerAuditWebChecks = map[string][]string{
	"read":       {"complete_projection", "typed_families", "explicit_pagination", "all_filters", "valid_empty", "cursor_recovery", "same_body_schema", "same_body_client", "zero_mutations"},
	"authority":  {"owner_and_admin_owned", "non_owner_hidden", "both_gets", "life_gates", "detail_missing", "cut_reread", "failure_reread", "list_cancel_join", "detail_cancel_join", "formal_logout", "late_isolated", "cross_project", "cross_domain", "zero_mutations"},
	"navigation": {"dotted_return", "raw_rejection", "settings_current", "default_general", "local_dirty_leave", "existing_draft_guards", "inline_focus", "checking_new_page", "drawer", "reduced_motion", "no_overflow", "no_debug", "zero_mutations"},
}

func projectOwnerAuditWebJSONObject(raw []byte) (map[string]json.RawMessage, error) {
	bad := errors.New("owned result object rejected")
	d := json.NewDecoder(bytes.NewReader(raw))
	token, err := d.Token()
	if err != nil || token != json.Delim('{') {
		return nil, bad
	}
	fields := map[string]json.RawMessage{}
	for d.More() {
		token, err := d.Token()
		key, ok := token.(string)
		if err != nil || !ok || fields[key] != nil {
			return nil, bad
		}
		var value json.RawMessage
		if d.Decode(&value) != nil || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return nil, bad
		}
		fields[key] = value
	}
	if token, err := d.Token(); err != nil || token != json.Delim('}') {
		return nil, bad
	}
	var tail any
	if d.Decode(&tail) != io.EOF {
		return nil, bad
	}
	return fields, nil
}
func decodeProjectOwnerAuditWebResult(raw []byte, mode string) (projectOwnerAuditWebResult, error) {
	var out projectOwnerAuditWebResult
	bad := func() (projectOwnerAuditWebResult, error) {
		return out, errors.New("owned result exact fields or required evidence invalid")
	}
	fields, err := projectOwnerAuditWebJSONObject(raw)
	if err != nil || len(fields) != 7 {
		return bad()
	}
	for _, key := range []string{"completed", "mode", "checks", "browser_audit_gets", "schema_bodies", "client_bodies", "layouts"} {
		if fields[key] == nil {
			return bad()
		}
	}
	checks, err := projectOwnerAuditWebJSONObject(fields["checks"])
	expected, known := projectOwnerAuditWebChecks[mode]
	if err != nil || !known || len(checks) != len(expected) || json.Unmarshal(raw, &out) != nil || !out.Completed || out.Mode != mode {
		return bad()
	}
	for _, key := range expected {
		if checks[key] == nil || !out.Checks[key] {
			return bad()
		}
	}
	if out.BrowserAuditGETs < 1 || out.BrowserAuditGETs > 512 || out.SchemaBodies < 0 || out.SchemaBodies > 512 || out.ClientBodies < 0 || out.ClientBodies > 512 {
		return bad()
	}
	if mode == "read" && (out.SchemaBodies == 0 || out.ClientBodies == 0) {
		return bad()
	}
	if mode == "navigation" && out.Layouts != 8 || mode != "navigation" && out.Layouts != 0 {
		return bad()
	}
	return out, nil
}

func runProjectOwnerAuditWeb(t *testing.T, mode string) {
	t.Helper()
	start := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	// First registered, last observed: every actual root/Store/guard/proxy/
	// child cleanup is inside this same original 120-second budget.
	t.Cleanup(func() {
		cancel()
		if time.Since(start) > 120*time.Second {
			t.Error("Project Audit top including all Cleanup exceeded 120 seconds")
		}
	})
	f := newProjectOwnerAuditWebFixture(t, ctx, mode)
	result := f.browser(ctx)
	f.stopProxy()
	f.mu.Lock()
	unfinished := f.counts["held"] != f.counts["joined"] || f.counts["server_started"] != f.counts["server_finished"] || f.controlPath != "" || f.failSession || f.proxyErrors != 0
	mutations, lookups := f.counts["browser_project_mutations"], f.counts["browser_command_lookups"]
	attempts := f.counts["browser_list_gets"] + f.counts["browser_detail_gets"]
	countFacts := map[string]int{}
	for _, key := range projectOwnerAuditWebCountKeys {
		countFacts[key] = f.counts[key]
	}
	f.mu.Unlock()
	if unfinished {
		t.Fatal("Project Audit controls/proxy callbacks did not reach complete actual retirement")
	}
	if mutations != 0 || lookups != 0 {
		t.Fatal("Audit browser sent Project mutation or command lookup")
	}
	if result.BrowserAuditGETs > attempts {
		t.Fatal("browser completed GET count exceeds real browser attempts")
	}
	if mode == "navigation" {
		directory := os.Getenv("AGENTEAM_AUTH_WEB_IMAGES")
		for _, theme := range []string{"light", "dark"} {
			for _, width := range []string{"390", "768", "1024", "1440"} {
				info, err := os.Stat(filepath.Join(directory, "project-audit-"+theme+"-"+width+".png"))
				if err != nil || !info.Mode().IsRegular() || info.Size() == 0 {
					t.Fatal("actual Project Audit layout image missing")
				}
			}
		}
	}
	// Restricted result facts contain no Cookie/CSRF/producer payload. File
	// presence is not visual acceptance, and server completion is not browser EOF.
	f.safeEvidence("go-facts.json", map[string]any{"mode": mode, "counts": countFacts, "proxy_actual_join": true, "browser_result": result, "lifecycle_facts_only": true})
	t.Logf("real Project Audit UI mode=%s; no-tag app.Run; three formal Project/Secret/Model producers; browser Audit attempts=%d completed=%d; safe same-body checks schema=%d public-client=%d; private Skills and lifecycle facts do not bind production runtime", mode, attempts, result.BrowserAuditGETs, result.SchemaBodies, result.ClientBodies)
}

func TestAccountProjectOwnerAuditWebReadAndFilters(t *testing.T) {
	runProjectOwnerAuditWeb(t, "read")
}
func TestAccountProjectOwnerAuditWebAuthorityAndRecovery(t *testing.T) {
	runProjectOwnerAuditWeb(t, "authority")
}
func TestAccountProjectOwnerAuditWebNavigationAndLayouts(t *testing.T) {
	runProjectOwnerAuditWeb(t, "navigation")
}
