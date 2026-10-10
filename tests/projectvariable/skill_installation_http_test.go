//go:build integration

package projectvariable_test

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/httpapi"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/skill/contract"
	skillhttp "github.com/LunaDeerTech/agenteam/internal/central/skill/http"
)

// This composition uses the two real HTTP adapters, their public dispatch
// predicate, one Skill Service and the real Account boundary. app's private
// router is covered by its existing pure test; this is not an app-root, native
// transport, AgentRun or production Project-initializer acceptance.
func skillInstallationHTTP(t *testing.T) *skillInstallationFixture {
	t.Helper()
	v := newSkillInstallationFixture(t)
	reads, err := skillhttp.NewHTTPHandler(v.service, v.base.boundary)
	if err != nil {
		t.Fatal("real Skill read handler construction")
	}
	management, err := skillhttp.NewManagementHTTPHandler(v.service, v.base.keys, v.base.boundary)
	if err != nil {
		t.Fatal("real Skill management handler construction")
	}
	routes := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if skillhttp.HandlesManagementRequest(r) {
			management.ServeHTTP(w, r)
			return
		}
		reads.ServeHTTP(w, r)
	})
	v.base.handler = httpapi.Handler(slog.New(slog.NewJSONHandler(v.base.logs, nil)), routes)
	return v
}

func skillInstallHTTPBody(t *testing.T, target sc.SkillID) string {
	t.Helper()
	return string(jsonBytes(t, map[string]any{"request": map[string]any{
		"skill_id": target.String(), "mode": "create",
		"source": map[string]any{"kind": "text_files", "files": []any{
			map[string]string{"path": "SKILL.md", "utf8_text": "---\nname: HTTP guide\ndescription: Real Owner HTTP installation\n---\n原始正文。\n"},
		}},
	}}))
}

type skillInstallHTTPReceipt struct {
	SkillID  sc.SkillID `json:"skill_id"`
	Revision f.Revision `json:"revision"`
	Version  f.Version  `json:"version"`
}

type skillInstallHTTPPage struct {
	Items      []sc.Metadata `json:"items"`
	NextCursor string        `json:"next_cursor,omitempty"`
}

func skillInstallHTTPDecode(t *testing.T, response variableHTTPResponse, out any) {
	t.Helper()
	requireHTTP(t, response)
	if response.header.Get("Cache-Control") != "no-store" || response.header.Get("Content-Length") != strconv.Itoa(len(response.body)) {
		t.Fatal("Skill response lost original representation headers")
	}
	decoder := json.NewDecoder(bytes.NewReader(response.body))
	decoder.DisallowUnknownFields()
	if decoder.Decode(out) != nil || decoder.Decode(new(any)) != io.EOF {
		t.Fatal("Skill response was not the closed typed representation")
	}
}

// Read-only postconditions. No Agent, Skill, installation or Object row is
// inserted by this test. Account Logout facts are outside this Project scope.
func skillInstallHTTPFacts(t *testing.T, v *skillInstallationFixture) [8]int64 {
	t.Helper()
	var counts [8]int64
	err := v.base.raw.QueryRow(ctxFor(t), `SELECT
 (SELECT count(*) FROM agenteam_skill.installations WHERE project_id=$1),
 (SELECT count(*) FROM agenteam_skill.installation_attempts WHERE project_id=$1),
 (SELECT count(*) FROM agenteam_skill.skills WHERE project_id=$1),
 (SELECT count(*) FROM agenteam_skill.revisions WHERE project_id=$1),
 (SELECT count(*) FROM agenteam_object.objects WHERE project_id=$1),
 (SELECT count(*) FROM agenteam_object.object_references r JOIN agenteam_object.objects o ON o.id=r.object_id WHERE o.project_id=$1),
 (SELECT count(*) FROM agenteam_audit.audit_records WHERE project_id=$1),
 (SELECT count(*) FROM agenteam_skill.work WHERE project_id=$1)`, v.project.ID.String()).Scan(
		&counts[0], &counts[1], &counts[2], &counts[3], &counts[4], &counts[5], &counts[6], &counts[7])
	if err != nil {
		t.Fatal("Skill HTTP canonical facts query")
	}
	return counts
}

func TestSkillInstallationOwnerHTTP(t *testing.T) {
	v := skillInstallationHTTP(t)
	owner := v.base.ownerBrowser
	collection := variableHTTPPath(v.project.ID, "/skills")
	target := id[pc.Skill](t)
	body := skillInstallHTTPBody(t, target)
	key := f.IdempotencyKey(id[struct{}](t).String())
	if !t.Run("install-lookup-catalog-and-read", func(t *testing.T) {
		builtin, err := v.service.ListSkills(ctxFor(t), owner.actor, v.project.ID)
		if err != nil || len(builtin) != 1 || !builtin[0].Protected {
			t.Fatal("real published builtin prerequisite")
		}
		var original skillInstallHTTPPage
		skillInstallHTTPDecode(t, v.base.request(t, owner, "GET", collection, "", ""), &original)
		if len(original.Items) != 1 || original.Items[0] != builtin[0] || original.NextCursor != "" {
			t.Fatal("original builtin-only HTTP directory")
		}
		var installed skillInstallHTTPReceipt
		skillInstallHTTPDecode(t, v.base.request(t, owner, "POST", collection, body, key), &installed)
		if installed.SkillID != target || installed.Revision != 1 || installed.Version != 1 {
			t.Fatal("Human POST did not return the original public installation receipt")
		}
		current, err := v.service.GetSkill(ctxFor(t), owner.actor, v.project.ID, target)
		if err != nil || current.Validate() != nil || current.ID != target || current.ProjectID != v.project.ID || current.Protected || current.Name != "HTTP guide" || current.Description != "Real Owner HTTP installation" || current.CurrentRevision != 1 || current.Version != 1 {
			t.Fatal("HTTP installation did not publish the exact canonical metadata")
		}
		var objectID string
		if err = v.base.raw.QueryRow(ctxFor(t), `SELECT object_id::text FROM agenteam_skill.installations WHERE project_id=$1 AND skill_id=$2 AND command_key=$3 AND phase='published'`, v.project.ID.String(), target.String(), string(key)).Scan(&objectID); err != nil {
			t.Fatal("HTTP command has no original published Object binding")
		}
		var attempts, revisions, references, uploads, liveWork, leases int
		err = v.base.raw.QueryRow(ctxFor(t), `SELECT
 (SELECT count(*) FROM agenteam_skill.installation_attempts WHERE project_id=$1 AND skill_id=$2),
 (SELECT count(*) FROM agenteam_skill.revisions WHERE project_id=$1 AND skill_id=$2 AND installation_id IS NOT NULL),
 (SELECT count(*) FROM agenteam_object.object_references WHERE object_id=$3),
 (SELECT count(*) FROM agenteam_audit.audit_records WHERE project_id=$1 AND action='object.upload_complete' AND resource_id=$3),
 (SELECT count(*) FROM agenteam_skill.work WHERE project_id=$1 AND phase<>'joined'),
 (SELECT count(*) FROM agenteam_object.object_leases WHERE object_id=$3 AND state='active')`, v.project.ID.String(), target.String(), objectID).Scan(&attempts, &revisions, &references, &uploads, &liveWork, &leases)
		if err != nil || attempts != 1 || revisions != 1 || references != 1 || uploads != 1 || liveWork != 0 || leases != 0 {
			t.Fatal("HTTP installation original attempt/publication/Object/Audit tails")
		}
		before := skillInstallHTTPFacts(t, v)
		var lookup skillInstallHTTPReceipt
		skillInstallHTTPDecode(t, v.base.request(t, owner, "POST", collection+"/commands/lookup", body, key), &lookup)
		if lookup != installed {
			t.Fatal("same-key Lookup changed the original receipt")
		}
		var first, second skillInstallHTTPPage
		skillInstallHTTPDecode(t, v.base.request(t, owner, "GET", collection+"/catalog?limit=1", "", ""), &first)
		if len(first.Items) != 1 || first.NextCursor == "" {
			t.Fatal("catalog did not provide one bounded page and its continuation")
		}
		skillInstallHTTPDecode(t, v.base.request(t, owner, "GET", collection+"/catalog?limit=1&cursor="+url.QueryEscape(first.NextCursor), "", ""), &second)
		if len(second.Items) != 1 || second.NextCursor != "" || first.Items[0].ID.String() >= second.Items[0].ID.String() {
			t.Fatal("catalog did not finish its exact ordered two-item view")
		}
		want := map[sc.SkillID]sc.Metadata{builtin[0].ID: builtin[0], current.ID: current}
		for _, got := range []sc.Metadata{first.Items[0], second.Items[0]} {
			if got != want[got.ID] {
				t.Fatal("catalog metadata differs from the original canonical records")
			}
		}
		oldGET := v.base.request(t, owner, "GET", collection, "", "")
		var old skillInstallHTTPPage
		skillInstallHTTPDecode(t, oldGET, &old)
		if len(old.Items) != 1 || old.Items[0] != builtin[0] || old.NextCursor != "" {
			t.Fatal("ordinary installation widened the original builtin directory")
		}
		detailPath := collection + "/" + target.String()
		detailGET := v.base.request(t, owner, "GET", detailPath, "", "")
		var detail sc.Metadata
		skillInstallHTTPDecode(t, detailGET, &detail)
		if detail != current {
			t.Fatal("old detail adapter did not read the same ordinary publication")
		}
		for _, entry := range []struct {
			path string
			get  variableHTTPResponse
		}{{collection, oldGET}, {detailPath, detailGET}} {
			head := v.base.request(t, owner, "HEAD", entry.path, "", "")
			if head.aborted || head.status != http.StatusOK || len(head.body) != 0 || head.header.Get("Content-Length") != strconv.Itoa(len(entry.get.body)) || head.header.Get("Cache-Control") != "no-store" {
				t.Fatal("old GET/HEAD representation compatibility")
			}
		}
		if before != skillInstallHTTPFacts(t, v) {
			t.Fatal("Lookup or read-only HTTP routes added installation facts")
		}
	}) {
		return
	}
	t.Run("current-owner-and-csrf", func(t *testing.T) {
		before := skillInstallHTTPFacts(t, v)
		newTarget := id[pc.Skill](t)
		newBody := skillInstallHTTPBody(t, newTarget)
		missingCSRF := owner
		missingCSRF.csrf = ""
		requireProblem(t, v.base.request(t, missingCSRF, "POST", collection, newBody, "csrf-denied"), f.CSRFFailed)
		// The real Project authority conceals non-owned Projects as NotFound.
		requireProblem(t, v.base.request(t, v.base.otherBrowser, "POST", collection, newBody, "owner-denied"), f.NotFound)
		requireProblem(t, v.base.request(t, v.base.otherBrowser, "GET", collection+"/catalog?limit=1", "", ""), f.NotFound)
		if before != skillInstallHTTPFacts(t, v) {
			t.Fatal("Owner or CSRF refusal changed canonical installation facts")
		}
		browser := v.base.login(t, owner.email)
		requireHTTP(t, v.base.request(t, browser, "GET", collection+"/catalog?limit=1", "", ""))
		request := variableHTTPRequest(ctxFor(t), browser, "POST", collection, newBody, "revoked-after-auth")
		reached := false
		request.Body = &authReadBarrier{ReadCloser: request.Body, before: func() {
			reached = true
			v.base.revoke(t, browser) // Real Logout after RequireHuman, before domain admission.
		}}
		response := v.base.serve(request) // Original handler returns synchronously.
		problem := requireProblem(t, response, f.SessionRevoked)
		decoder := json.NewDecoder(bytes.NewReader(response.body))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&problem) != nil || decoder.Decode(new(any)) != io.EOF || len(problem.FieldErrors) != 0 || problem.RetryHint != "" {
			t.Fatal("revocation Problem contains fields outside its safe contract")
		}
		cleared := false
		for _, cookie := range (&http.Response{Header: response.header}).Cookies() {
			if cookie.Name == "__Host-agenteam_session" && cookie.MaxAge < 0 && cookie.Value == "" {
				cleared = true
			}
		}
		if !reached || response.status != http.StatusUnauthorized || !cleared || problem.Type != "urn:agenteam:problem:session-revoked" || problem.Title != "Session revoked" || problem.Detail != "Sign in again." || problem.Instance != collection || before != skillInstallHTTPFacts(t, v) {
			t.Fatal("post-authentication revocation did not refuse with safe empty metadata")
		}
		var targets int
		if err := v.base.raw.QueryRow(ctxFor(t), `SELECT (SELECT count(*) FROM agenteam_skill.installations WHERE project_id=$1 AND skill_id=$2)+(SELECT count(*) FROM agenteam_skill.skills WHERE project_id=$1 AND id=$2)`, v.project.ID.String(), newTarget.String()).Scan(&targets); err != nil || targets != 0 {
			t.Fatal("refused HTTP intent created canonical target facts")
		}
	})
}
