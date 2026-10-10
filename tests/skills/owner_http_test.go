//go:build integration

package skill_test

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/account"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

func skillOwnerHTTPTop(t *testing.T) {
	t.Helper()
	start := time.Now()
	t.Cleanup(func() {
		if time.Since(start) > 90*time.Second {
			t.Error("Skill HTTP top including actual fixture tails exceeded 90s")
		}
	})
}
func (r skillOwnerHTTPResponse) want(t *testing.T, status int) map[string]any {
	t.Helper()
	if r.aborted || r.status != status {
		t.Fatalf("HTTP status=%d aborted=%t, want=%d", r.status, r.aborted, status)
	}
	if r.header.Get("Cache-Control") != "no-store" || r.header.Get("X-Request-ID") == "" || r.header.Get("Content-Length") != strconv.Itoa(len(r.body)) {
		t.Fatal("safe complete response headers")
	}
	var out map[string]any
	if json.Unmarshal(r.body, &out) != nil {
		t.Fatal("invalid complete response JSON")
	}
	return out
}

// Account's legitimate read activity is intentionally outside these business
// facts. Each comparison surrounds reads, never a Login/Logout or fixture write.
func (v *skillOwnerHTTPFixture) facts(t *testing.T) [8]string {
	t.Helper()
	var out [8]string
	for i, table := range []string{"agenteam_skill.initializations", "agenteam_skill.object_attempts", "agenteam_skill.skills", "agenteam_skill.revisions", "agenteam_skill.work", "agenteam_skill.cleanup", "agenteam_audit.audit_records", "agenteam_outbox.events"} {
		query := `SELECT md5(COALESCE(jsonb_agg(to_jsonb(x) ORDER BY to_jsonb(x)::text)::text,'[]')) FROM ` + table + ` x`
		if err := v.store.QueryRow(testContext(t), query).Scan(&out[i]); err != nil {
			t.Fatal("business facts snapshot", err)
		}
	}
	return out
}

type skillOwnerHTTPSchemaCase struct {
	Label  string `json:"label"`
	Schema string `json:"schema"`
	Value  any    `json:"value"`
	Valid  bool   `json:"valid"`
}

func skillOwnerHTTPValidateSchema(t *testing.T, cases []skillOwnerHTTPSchemaCase) {
	t.Helper()
	python := os.Getenv("AGENTEAM_SKILL_HTTP_SCHEMA_PYTHON")
	if python == "" {
		t.Fatal("explicit local Schema interpreter required for actual HTTP response validation")
	}
	raw, err := json.Marshal(map[string]any{"cases": cases})
	if err != nil {
		t.Fatal(err)
	}
	helper, err := filepath.Abs("../../internal/central/skill/http/testdata/schema.py")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, python, helper)
	cmd.Stdin = bytes.NewReader(raw)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("actual response Schema: %v\n%s", err, output)
	}
	t.Log(string(output))
}
func skillOwnerHTTPDetail(t *testing.T, body map[string]any, project id.ProjectID, skill pc.SkillID) {
	t.Helper()
	if len(body) != 8 || body["id"] != skill.String() || body["project_id"] != project.String() || body["name"] != "Add Skills" || body["normalized_name"] != "add-skills" || body["protected"] != true || body["current_revision"] != "1" || body["version"] != "1" {
		t.Fatal("exact published metadata not returned")
	}
	if text, ok := body["description"].(string); !ok || strings.TrimSpace(text) == "" {
		t.Fatal("published builtin description missing")
	}
}

func TestSkillOwnerReadHTTPMetadata(t *testing.T) {
	skillOwnerHTTPTop(t)
	v := newSkillOwnerHTTPFixture(t)
	listed, err := v.service.ListSkills(testContext(t), v.ownerBrowser.actor, v.project)
	if err != nil || len(listed) != 1 {
		t.Fatal("real published Skill prerequisite", err)
	}
	skill := listed[0].ID
	base := skillOwnerHTTPPath(v.project, "")
	before := v.facts(t)
	steps := len(v.seed.objects.steps)
	var cases []skillOwnerHTTPSchemaCase
	t.Run("same_current_directory_and_detail_get_head", func(t *testing.T) {
		for _, suffix := range []string{"", "/" + skill.String()} {
			get := v.request(t, v.ownerBrowser, "GET", base+suffix, "", "")
			body := get.want(t, 200)
			schema := "SkillMetadata"
			if suffix == "" {
				schema = "SkillDirectory"
				items, ok := body["items"].([]any)
				if !ok || len(items) != 1 || len(body) != 1 {
					t.Fatal("bounded directory")
				}
				skillOwnerHTTPDetail(t, items[0].(map[string]any), v.project, skill)
			} else {
				skillOwnerHTTPDetail(t, body, v.project, skill)
			}
			cases = append(cases, skillOwnerHTTPSchemaCase{schema, schema, body, true})
			head := v.request(t, v.ownerBrowser, "HEAD", base+suffix, "", "")
			if head.aborted || head.status != 200 || len(head.body) != 0 || head.header.Get("Content-Length") != get.header.Get("Content-Length") || head.header.Get("Content-Type") != "application/json" || head.header.Get("Cache-Control") != "no-store" {
				t.Fatal("HEAD did not use complete representation")
			}
		}
	})
	t.Run("strict_request_and_real_browser_boundary", func(t *testing.T) {
		for _, tc := range []struct {
			method, suffix, body string
			status               int
		}{{"GET", "?", "", 400}, {"GET", "?limit=1", "", 400}, {"GET", "?q=PRIVATE_QUERY_canary", "", 400}, {"GET", "?%6cimit=1&limit=2", "", 400}, {"GET", "/not-id", "", 400}, {"GET", "/", "", 404}, {"GET", "", "x", 400}, {"POST", "", "", 405}} {
			r := v.request(t, v.ownerBrowser, tc.method, base+tc.suffix, tc.body, "")
			r.want(t, tc.status)
			if bytes.Contains(r.body, []byte("PRIVATE_QUERY_canary")) {
				t.Fatal("query leaked")
			}
			if tc.status == 405 && r.header.Get("Allow") != "GET, HEAD" {
				t.Fatal("Allow")
			}
		}
		v.request(t, skillOwnerHTTPBrowser{}, "POST", base+"?bad", "", "").want(t, 401)
		r := skillOwnerHTTPRequest(testContext(t), v.ownerBrowser, "GET", base, "", "")
		r.Header.Set("Origin", "https://foreign.example.test")
		v.serve(r).want(t, 403)
		missing := base + "/" + testID[pc.Skill](t).String()
		get := v.request(t, v.ownerBrowser, "GET", missing, "", "")
		get.want(t, 404)
		head := v.request(t, v.ownerBrowser, "HEAD", missing, "", "")
		if head.aborted || head.status != 404 || len(head.body) != 0 || head.header.Get("Content-Length") != get.header.Get("Content-Length") || head.header.Get("Content-Type") != "application/problem+json" {
			t.Fatal("HEAD Problem must remain bodyless")
		}
	})
	if v.facts(t) != before || len(v.seed.objects.steps) != steps {
		t.Fatal("metadata HTTP changed business facts or used Object")
	}
	for _, secret := range []string{v.ownerBrowser.cookie, v.ownerBrowser.csrf, "PRIVATE_QUERY_canary"} {
		if strings.Contains(v.logs.text(), secret) {
			t.Fatal("private HTTP material logged")
		}
	}
	skillOwnerHTTPValidateSchema(t, cases)
}

func TestSkillOwnerReadHTTPCurrentAuthority(t *testing.T) {
	skillOwnerHTTPTop(t)
	v := newSkillOwnerHTTPFixture(t)
	list, err := v.service.ListSkills(testContext(t), v.ownerBrowser.actor, v.project)
	if err != nil || len(list) != 1 {
		t.Fatal("published read", err)
	}
	skill := list[0].ID
	base := skillOwnerHTTPPath(v.project, "")
	readBoth := func(t *testing.T, browser skillOwnerHTTPBrowser, status int) {
		t.Helper()
		before := v.facts(t)
		for _, suffix := range []string{"", "/" + skill.String()} {
			v.request(t, browser, "GET", base+suffix, "", "").want(t, status)
		}
		if v.facts(t) != before {
			t.Fatal("authorization read changed facts")
		}
	}
	t.Run("foreign_owner_and_admin_no_bypass", func(t *testing.T) {
		readBoth(t, v.otherBrowser, 404)
		readBoth(t, v.adminBrowser, 404)
		v.request(t, v.ownerBrowser, "GET", skillOwnerHTTPPath(testID[id.Project](t), ""), "", "").want(t, 404)
	})
	t.Run("project_gate_and_missing_publication_are_distinct", func(t *testing.T) {
		unready := v.newCase(t, true, false)
		r := v.request(t, v.ownerBrowser, "GET", skillOwnerHTTPPath(unready.request.ProjectID, ""), "", "").want(t, 409)
		if r["code"] != string(f.ProjectNotActive) {
			t.Fatal("uninitialized Project gate lost")
		}
		missing := v.newCase(t, false, true)
		r = v.request(t, v.ownerBrowser, "GET", skillOwnerHTTPPath(missing.request.ProjectID, ""), "", "").want(t, 409)
		if r["code"] != string(f.InvalidState) || r["items"] != nil {
			t.Fatal("missing P2 publication became an empty directory")
		}
	})
	t.Run("real_new_session_and_logout", func(t *testing.T) {
		newSession := v.login(t, v.ownerBrowser.email)
		readBoth(t, newSession, 200)
		if err := v.core.Logout(testContext(t), account.LogoutRequest{Actor: newSession.actor, Key: f.IdempotencyKey(testID[struct{}](t).String())}); err != nil {
			t.Fatal("real Logout", err)
		}
		readBoth(t, newSession, 401)
		readBoth(t, v.ownerBrowser, 200)
	})
	t.Run("current_owner_mapping_rechecked", func(t *testing.T) {
		other := v.otherBrowser.actor.Details().UserID
		readerMutation(t, v.skillPG, v.seed, `UPDATE agenteam_project.projects SET owner_user_id=$2,version=version+1,updated_at=statement_timestamp() WHERE id=$1`, v.project.String(), other)
		readBoth(t, v.ownerBrowser, 404)
		readBoth(t, v.otherBrowser, 200)
		readerMutation(t, v.skillPG, v.seed, `UPDATE agenteam_project.projects SET owner_user_id=$2,version=version+1,updated_at=statement_timestamp() WHERE id=$1`, v.project.String(), v.ownerBrowser.actor.Details().UserID)
		readBoth(t, v.ownerBrowser, 200)
	})
	t.Run("archived_read_then_deleting_gate", func(t *testing.T) {
		readerMutation(t, v.skillPG, v.seed, `UPDATE agenteam_project.projects SET lifecycle='archived',archived_at=statement_timestamp(),updated_at=statement_timestamp(),version=version+1 WHERE id=$1`, v.project.String())
		readBoth(t, v.ownerBrowser, 200)
		seedReaderDeletingProject(t, v.skillPG, v.seed)
		readBoth(t, v.ownerBrowser, 409)
	})
}
