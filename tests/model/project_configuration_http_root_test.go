//go:build integration

package model_test

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/httpapi"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

func TestModelProjectConfigurationHTTPDefaultRoot(t *testing.T) {
	projectConfigurationHTTPTop(t)
	v := newProjectConfigurationHTTP(t)
	provider := v.projectProvider(t, nil)
	m := v.projectModel(t, provider)
	// Only remove unused nullable technical singletons, never initialize Summary
	// or supply a replacement root. Public app.Run must restore their identities.
	v.sql(t, `DELETE FROM agenteam_model.platform_selection; DELETE FROM agenteam_model.meeting_summary_selection`)
	// A missing original table must fail default startup before listener publish.
	// The accepted helper runs public app.Run(nil hooks), owns cancel/actual wait
	// and retains the existing root cleanup contracts, including their limits.
	v.sql(t, `ALTER TABLE agenteam_model.meeting_summary_selection RENAME TO unavailable_project_http_summary`)
	restore := func() {
		v.sql(t, `ALTER TABLE agenteam_model.unavailable_project_http_summary RENAME TO meeting_summary_selection`)
	}
	restored := false
	t.Cleanup(func() {
		if !restored {
			restore()
		}
	})
	startMeetingSummarySettingsRoot(t, &systemHTTPFixture{fixture: v.fixture}, true)
	restore()
	restored = true
	root := startProjectUsageRoot(t, v)
	base := projectConfigurationHTTPPath(v.project.ID, "")
	for _, tc := range []struct{ suffix, schema, name string }{{"model-providers", "ProviderPage", "root-providers"}, {"model-providers/" + provider.ID.String(), "Provider", "root-provider"}, {"models", "ModelPage", "root-models"}, {"models/" + m.ID.String(), "Model", "root-model"}, {"available-chat-models", "AvailableChatModelPage", "root-available"}} {
		path := base + tc.suffix
		r := root.request(t, v.ownerBrowser, "GET", path, nil).want(t, 200)
		head := root.request(t, v.ownerBrowser, "HEAD", path, nil).want(t, 200)
		if len(head.body) != 0 || head.headers.Get("Content-Length") != r.headers.Get("Content-Length") {
			t.Fatal("root lost complete HEAD/GET")
		}
		projectConfigurationHTTPSchema(t, tc.schema, r.body)
		projectConfigurationHTTPExport(t, tc.name, "GET", path, r)
		root.request(t, v.adminBrowser, "GET", path, nil).problem(t, 404, f.NotFound)
	}
	for _, path := range []string{projectOwnerReadPath(v.project.ID), "/api/v1/projects", projectUsageResolve(v.ownerName, v.project.Name), projectUsagePath(v.project.ID), projectUsagePath(v.project.ID) + "/summary?group_by=day", "/api/v1/session"} {
		root.request(t, v.ownerBrowser, "GET", path, nil).want(t, 200)
	}
	root.request(t, v.adminBrowser, "GET", "/api/v1/system/model-providers", nil).want(t, 200)
	summary := root.request(t, v.adminBrowser, "GET", meetingSummarySettingsPath, nil).want(t, 200)
	obj := summary.object(t)
	if len(obj) != 3 || obj["model"] != nil || obj["version"] != "1" {
		t.Fatal("default Summary changed")
	}
	path := projectOwnerReadPath(v.project.ID)
	description := "root-model-http-read-compatibility"
	patch := projectUpdateRootRequest(t, root, v.ownerBrowser, "PATCH", path, "root-model-compat-key", projectUpdateBody(t, 1, nil, &description)).want(t, 200)
	if patch.object(t)["version"] != "2" {
		t.Fatal("accepted Update no longer bound")
	}
	lookup := projectUpdateRootRequest(t, root, v.ownerBrowser, "POST", projectUpdateLookupPath(v.project.ID), "root-model-compat-key", projectUpdateLookupBody).want(t, 200)
	var observed struct {
		State  pc.LookupState `json:"state"`
		Result struct {
			Command pc.CommandName  `json:"command"`
			Project json.RawMessage `json:"project"`
		} `json:"result"`
	}
	if json.Unmarshal(lookup.body, &observed) != nil || observed.State != pc.LookupCommitted || observed.Result.Command != pc.UpdateCommand || !bytes.Equal(observed.Result.Project, patch.body) {
		t.Fatal("Update lookup lost exact committed command/project receipt")
	}
	var historical map[string]any
	if json.Unmarshal(observed.Result.Project, &historical) != nil || historical["id"] != v.project.ID.String() || historical["version"] != "2" {
		t.Fatal("Update lookup changed original Project identity/version")
	}
	root.request(t, v.ownerBrowser, "POST", base+"models", nil).problem(t, 405, f.MethodNotAllowed)
	ready := root.request(t, v.ownerBrowser, "GET", "/readyz", nil)
	var problem httpapi.Problem
	if ready.status != 503 || json.Unmarshal(ready.body, &problem) != nil || (problem.Code != f.DependencyUnbound && problem.Code != f.DependencyUnavailable) {
		t.Fatal("default readiness changed")
	}
	if root.connections.Load() != 1 {
		t.Fatal("HTTP read/HEAD/old routes did not retire on one connection")
	}
	root.stop(t)
	if !strings.Contains(root.logs.String(), `"outcome":"drained"`) {
		t.Fatal("default root did not actually drain")
	}
	again := startProjectUsageRoot(t, v)
	after := again.request(t, v.adminBrowser, "GET", meetingSummarySettingsPath, nil).want(t, 200)
	if !bytes.Equal(after.body, summary.body) {
		t.Fatal("restart reset Summary singleton")
	}
	again.request(t, v.ownerBrowser, "GET", base+"models/"+m.ID.String(), nil).want(t, 200)
	again.request(t, v.ownerBrowser, "GET", path, nil).want(t, 200)
	again.stop(t)
	for _, r := range []*projectUsageRoot{root, again} {
		for _, private := range []string{v.ownerBrowser.cookie, v.ownerBrowser.csrf, description, "root-model-compat-key"} {
			if strings.Contains(r.logs.String(), private) {
				t.Fatal("root logs leaked sensitive request material")
			}
		}
	}
	t.Log("default app.Run: original init failure publishes no listener; fresh Model/Summary singleton restored and restart stable; sole Project authority binds all five resources; existing Update/lookup/Ownerread/Usage/System retained; actual root drain; production Resolution/Invocations remain unbound")
}
