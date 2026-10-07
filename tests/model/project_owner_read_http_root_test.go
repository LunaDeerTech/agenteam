//go:build integration

package model_test

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/httpapi"
)

func TestModelProjectOwnerReadHTTPRootBinding(t *testing.T) {
	projectOwnerReadTop(t)
	v := newProjectOwnerReadFixture(t)
	root := startProjectUsageRoot(t, v)
	for _, target := range []string{"/api/v1/projects", projectOwnerReadPath(v.project.ID)} {
		get := root.request(t, v.ownerBrowser, "GET", target, nil).want(t, 200)
		head := root.request(t, v.ownerBrowser, "HEAD", target, nil).want(t, 200)
		if len(head.body) != 0 || get.headers.Get("Content-Length") != head.headers.Get("Content-Length") || !bytes.Contains(get.body, []byte(v.project.ID.String())) {
			t.Fatal("default root lost full GET/HEAD representation")
		}
		schema, name := "Project", "root-detail"
		if target == "/api/v1/projects" {
			schema, name = "ProjectPage", "root-list"
		}
		projectOwnerReadSchema(t, schema, get.body)
		projectOwnerReadExport(t, name, target, get)
	}
	for _, target := range []string{projectUsageResolve(v.ownerName, v.project.Name), projectUsagePath(v.project.ID), projectUsagePath(v.project.ID) + "/summary?group_by=day", "/api/v1/session"} {
		root.request(t, v.ownerBrowser, "GET", target, nil).want(t, 200)
	}
	root.request(t, v.adminBrowser, "GET", "/api/v1/system/model-providers", nil).want(t, 200)
	summary := root.request(t, v.adminBrowser, "GET", "/api/v1/system/model-selection/meeting-summary", nil).want(t, 200)
	selection := summary.object(t)
	selectorID, idString := selection["id"].(string)
	_, idError := f.ParseID[struct{}](selectorID)
	_, modelPresent := selection["model"]
	if len(selection) != 3 || !idString || idError != nil || selection["version"] != "1" || !modelPresent || selection["model"] != nil {
		t.Fatal("accepted Summary singleton default changed")
	}
	root.request(t, v.ownerBrowser, "POST", "/api/v1/projects", nil).problem(t, 405, f.MethodNotAllowed)
	root.request(t, v.adminBrowser, "GET", projectOwnerReadPath(v.project.ID), nil).problem(t, 404, f.NotFound)
	ready := root.request(t, v.ownerBrowser, "GET", "/readyz", nil)
	var problem httpapi.Problem
	if ready.status != 503 || json.Unmarshal(ready.body, &problem) != nil || (problem.Code != f.DependencyUnbound && problem.Code != f.DependencyUnavailable) {
		t.Fatal("read root changed readiness")
	}
	if root.connections.Load() != 1 {
		t.Fatal("read/HEAD/old routes did not complete EOF on one connection")
	}
	root.stop(t)
	if !strings.Contains(root.logs.String(), `"outcome":"drained"`) {
		t.Fatal("default root did not join shutdown")
	}
	for _, private := range []string{v.ownerBrowser.cookie, v.ownerBrowser.csrf, v.project.Name} {
		if strings.Contains(root.logs.String(), private) {
			t.Fatal("root log exposed private request data")
		}
	}
	t.Log("public default app.Run; exact new GET/HEAD plus original resolve/Usage/Account/System/S2 routes; no write binding or ready success; actual root shutdown joined")
}
