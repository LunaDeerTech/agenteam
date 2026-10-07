//go:build integration

package model_test

import (
	"bytes"
	"net/url"
	"sort"
	"testing"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

func projectOwnerPage(t *testing.T, response systemHTTPResponse) ([]any, string) {
	t.Helper()
	o := response.want(t, 200).object(t)
	items, ok := o["items"].([]any)
	if !ok || len(o) != 2 {
		t.Fatal("closed Page shape")
	}
	next := ""
	if o["next_cursor"] != nil {
		var ok bool
		next, ok = o["next_cursor"].(string)
		if !ok || next == "" {
			t.Fatal("cursor shape")
		}
	}
	return items, next
}
func TestModelProjectOwnerReadHTTPProjectionAndPaging(t *testing.T) {
	projectOwnerReadTop(t)
	v := newProjectOwnerReadFixture(t)
	original := v.project
	refs := []pc.ProjectRef{original, v.createProject(t, v.owner), v.createProject(t, v.owner)}
	v.createProject(t, v.otherBrowser.actor)
	// A tie in canonical creation time is a deliberate ordering fixture; Owner,
	// identity, accepted creation and initialized Skill facts remain untouched.
	tied := time.Now().UTC().Add(-time.Hour).Truncate(time.Microsecond)
	for _, p := range refs {
		if _, e := v.raw.Exec(testContext(t), `UPDATE agenteam_project.projects SET created_at=$2 WHERE id=$1`, p.ID.String(), tied); e != nil {
			t.Fatal(e)
		}
	}
	sort.Slice(refs, func(i, j int) bool { return refs[i].ID.String() > refs[j].ID.String() })
	first := v.request(t, v.ownerBrowser, "GET", "/api/v1/projects?limit=1").want(t, 200)
	items, next := projectOwnerPage(t, first)
	if len(items) != 1 || items[0].(map[string]any)["id"] != refs[0].ID.String() || next == "" {
		t.Fatal("descending tie-break/first page")
	}
	renewed := v.login(t, v.ownerBrowser.email)
	second := v.request(t, renewed, "GET", "/api/v1/projects?limit=2&cursor="+url.QueryEscape(next))
	items, tail := projectOwnerPage(t, second)
	if len(items) != 2 || tail != "" || items[0].(map[string]any)["id"] != refs[1].ID.String() || items[1].(map[string]any)["id"] != refs[2].ID.String() {
		t.Fatal("Session/limit changed cursor identity")
	}
	v.request(t, v.otherBrowser, "GET", "/api/v1/projects?cursor="+url.QueryEscape(next)).problem(t, 400, f.CursorInvalid)
	v.request(t, v.ownerBrowser, "GET", "/api/v1/projects?lifecycle=active&cursor="+url.QueryEscape(next)).problem(t, 400, f.CursorInvalid)
	v.request(t, v.ownerBrowser, "GET", "/api/v1/projects?cursor="+url.QueryEscape(next+"x")).problem(t, 400, f.CursorInvalid)
	for _, target := range []string{"/api/v1/projects", projectOwnerReadPath(original.ID)} {
		get := v.request(t, v.ownerBrowser, "GET", target).want(t, 200)
		head := v.request(t, v.ownerBrowser, "HEAD", target).want(t, 200)
		if len(head.body) != 0 || head.headers.Get("Content-Length") != get.headers.Get("Content-Length") {
			t.Fatal("HEAD did not encode full GET")
		}
		schema := "Project"
		if target == "/api/v1/projects" {
			schema = "ProjectPage"
		}
		projectOwnerReadSchema(t, schema, get.body)
	}
	oldPath := projectUsageResolve(v.ownerName, original.Name)
	projectUsageRename(t, v, "renamed-owner-read")
	v.request(t, v.ownerBrowser, "GET", oldPath).problem(t, 404, f.NotFound)
	resolved := v.request(t, v.ownerBrowser, "GET", projectUsageResolve(v.ownerName, v.project.Name)).want(t, 200)
	detail := v.request(t, v.ownerBrowser, "GET", projectOwnerReadPath(original.ID)).want(t, 200)
	if resolved.object(t)["id"] != detail.object(t)["id"] || detail.object(t)["name"] != v.project.Name {
		t.Fatal("stable ID diverged after formal rename")
	}
	// Pending is produced by real creation accepting the explicit test Skill's
	// pending result, not by making a completed row look initialized.
	v.skills.setMode("pending")
	pendingID := newID[id.Project](t)
	result, e := v.projectService.CreateProject(testContext(t), v.owner, f.CommandMeta{RequestID: newID[f.Request](t), IdempotencyKey: f.IdempotencyKey(pendingID.String())}, pc.CreateProjectRequest{ProjectID: pendingID, Name: "pending-" + pendingID.String()[24:]})
	v.skills.setMode("")
	if e != nil || result.Project != nil || result.State == pc.CreationReady {
		t.Fatal("pending fixture acceptance", e)
	}
	v.request(t, v.ownerBrowser, "GET", projectOwnerReadPath(pendingID)).problem(t, 409, f.ProjectNotActive)
	if body := v.request(t, v.ownerBrowser, "GET", "/api/v1/projects").want(t, 200).body; bytes.Contains(body, []byte(pendingID.String())) {
		t.Fatal("uninitialized row listed")
	}
	for _, state := range []pc.Lifecycle{pc.Archiving, pc.Archived, pc.Deleting} {
		t.Run(string(state), func(t *testing.T) {
			p := v.createProject(t, v.owner)
			v.project = p
			// gate uses formal archive/delete acceptance. Archived completion is an
			// explicit canonical SQL fixture, never evidence of participant runtime stop.
			v.gate(t, state)
			got := v.request(t, v.ownerBrowser, "GET", "/api/v1/projects?lifecycle="+string(state)).want(t, 200)
			rows, _ := projectOwnerPage(t, got)
			if len(rows) != 1 {
				t.Fatal("state filter")
			}
			item := rows[0].(map[string]any)
			_, description := item["description"]
			_, operation := item["operation_id"]
			if description == (state == pc.Deleting) || operation != (state == pc.Archiving || state == pc.Deleting) {
				t.Fatal("state conditional projection")
			}
			projectOwnerReadSchema(t, "ProjectPage", got.body)
			if state == pc.Deleting {
				v.request(t, v.ownerBrowser, "GET", projectOwnerReadPath(p.ID)).problem(t, 409, f.ProjectNotActive)
			} else {
				ref := v.request(t, v.ownerBrowser, "GET", projectOwnerReadPath(p.ID)).want(t, 200)
				projectOwnerReadSchema(t, "Project", ref.body)
			}
		})
	}
	// Default all states and an explicit set are cursor-equivalent; ordering of
	// the set does not change its digest. The initialized pending row stays absent.
	_, allCursor := projectOwnerPage(t, v.request(t, v.ownerBrowser, "GET", "/api/v1/projects?limit=1"))
	v.request(t, v.ownerBrowser, "GET", "/api/v1/projects?lifecycle=deleting,archived,active,archiving&cursor="+url.QueryEscape(allCursor)).want(t, 200)
	t.Log("formal identity/Create/rename; pending Skill result; formal lifecycle acceptance plus explicitly seeded archived terminal; Owner/filter/tie-break/current-Session paging and exact schema passed")
}
