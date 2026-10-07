//go:build integration

package model_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

type projectUsageSchemaCase struct {
	Name  string          `json:"name"`
	Value json.RawMessage `json:"value"`
}

func projectUsageCheckSchemas(t *testing.T, cases []projectUsageSchemaCase) {
	t.Helper()
	python := os.Getenv("AGENTEAM_USAGE_SCHEMA_PYTHON")
	if python == "" {
		t.Fatal("fixed standard schema interpreter required")
	}
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(cases)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, python, "-c", `import json,sys
from pathlib import Path
from jsonschema import Draft202012Validator
from referencing import Registry,Resource
from referencing.jsonschema import DRAFT202012
base='https://usage-schema.invalid/'
p=Path(sys.argv[1])/'api/openapi'
registry=Registry()
for name in ['project-usage.json','common.json']:
    document=json.loads((p/name).read_text())
    registry=registry.with_resource(base+name,Resource.from_contents(document,default_specification=DRAFT202012))
cases=json.load(sys.stdin)
for case in cases:
    schema={'$ref':base+'project-usage.json#/components/schemas/'+case['name']}
    if list(Draft202012Validator(schema,registry=registry).iter_errors(case['value'])):
        print('SCHEMA_VALIDATION_FAILED');sys.exit(1)
print('STANDARD_SCHEMA_PASS',len(cases))
`, root)
	cmd.Stdin = bytes.NewReader(data)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatal("actual HTTP DTO standard schema validation failed", err)
	}
	if !strings.HasPrefix(string(output), "STANDARD_SCHEMA_PASS ") {
		t.Fatal("schema validator did not finish")
	}
	t.Log(strings.TrimSpace(string(output)))
}

func TestModelProjectUsageHTTPProjectionAndPath(t *testing.T) {
	started := time.Now()
	defer projectUsageComplete(t, started)
	v := newProjectUsageHTTPFixture(t)
	large := v.success(t, json.RawMessage(`{"prompt_tokens":9007199254740993,"completion_tokens":0,"total_tokens":9007199254740993}`), nil)
	v.success(t, json.RawMessage(`{"prompt_tokens":0,"completion_tokens":0,"total_tokens":0}`), nil)
	v.success(t, nil, nil)
	path := projectUsagePath(v.project.ID)
	list := v.request(t, v.ownerBrowser, "GET", path).want(t, 200)
	object := list.object(t)
	items, ok := object["items"].([]any)
	if !ok || len(items) != 3 || len(object) != 2 || object["next_cursor"] != nil {
		t.Fatal("nonempty complete list envelope")
	}
	known, zero, unknown := false, false, false
	for _, item := range items {
		row := item.(map[string]any)
		usage := row["usage"].(map[string]any)
		if row["project_id"] != v.project.ID.String() || row["attempt_index"] != "1" || row["meeting_id"] != nil {
			t.Fatal("stable projection association/precision")
		}
		switch usage["input_tokens"] {
		case "9007199254740993":
			known = true
		case "0":
			zero = true
		case nil:
			unknown = true
		default:
			t.Fatal("usage integer changed")
		}
		for _, hidden := range []string{"process_id", "fence", "snapshot_id", "input", "profile", "adapter_revision", "endpoint", "error", "operation_id"} {
			if _, present := row[hidden]; present {
				t.Fatal("private Invocation field escaped")
			}
		}
	}
	if !known || !zero || !unknown || !bytes.Contains(list.body, []byte(large.event.Identity.Attempt.InvocationID.String())) {
		t.Fatal("known/zero/unknown facts collapsed")
	}
	checks := []projectUsageSchemaCase{{"List", list.body}}
	resolve := v.request(t, v.ownerBrowser, "GET", projectUsageResolve(v.ownerName, v.project.Name)).want(t, 200)
	if resolve.object(t)["id"] != v.project.ID.String() {
		t.Fatal("current names resolved wrong identity")
	}
	checks = append(checks, projectUsageSchemaCase{"Project", resolve.body})
	for _, by := range []string{"consumer", "agent", "model", "provider", "execution", "meeting", "purpose", "day"} {
		aggregate := v.request(t, v.ownerBrowser, "GET", path+"/summary?group_by="+by).want(t, 200)
		groups := aggregate.object(t)["items"].([]any)
		if len(groups) == 0 {
			t.Fatal("nonempty aggregate became empty")
		}
		for _, group := range groups {
			row := group.(map[string]any)
			key := row["key"].(map[string]any)
			if key["by"] != by {
				t.Fatal("group dimension changed")
			}
			if by == "meeting" && key["id"] != nil {
				t.Fatal("absent Meeting was invented")
			}
		}
		checks = append(checks, projectUsageSchemaCase{"Aggregate", aggregate.body})
	}
	for _, resource := range []string{projectUsageResolve(v.ownerName, v.project.Name), path, path + "/summary?group_by=day"} {
		get := v.request(t, v.ownerBrowser, "GET", resource).want(t, 200)
		head := v.request(t, v.ownerBrowser, "HEAD", resource).want(t, 200)
		if len(head.body) != 0 || head.headers.Get("Content-Length") != get.headers.Get("Content-Length") {
			t.Fatal("HEAD omitted full representation check")
		}
	}
	first := v.request(t, v.ownerBrowser, "GET", path+"?limit=1").want(t, 200).object(t)
	cursor, ok := first["next_cursor"].(string)
	if !ok || cursor == "" {
		t.Fatal("formal paging cursor missing")
	}
	oldName := v.project.Name
	projectUsageRename(t, v, "renamed-"+newID[struct{}](t).String()[24:])
	v.request(t, v.ownerBrowser, "GET", projectUsageResolve(v.ownerName, oldName)).problem(t, 404, f.NotFound)
	current := v.request(t, v.ownerBrowser, "GET", projectUsageResolve(v.ownerName, v.project.Name)).want(t, 200)
	if current.object(t)["id"] != v.project.ID.String() {
		t.Fatal("rename replaced stable project identity")
	}
	v.request(t, v.ownerBrowser, "GET", path+"?limit=2&cursor="+url.QueryEscape(cursor)).want(t, 200)
	reusedID := newID[id.Project](t)
	reused, err := v.projectService.CreateProject(testContext(t), v.owner, f.CommandMeta{RequestID: newID[f.Request](t), IdempotencyKey: f.IdempotencyKey(newID[struct{}](t).String())}, pc.CreateProjectRequest{ProjectID: reusedID, Name: oldName})
	if err != nil || reused.State != pc.CreationReady || reused.Project == nil {
		t.Fatal("formal name reuse creation", err)
	}
	if got := v.request(t, v.ownerBrowser, "GET", projectUsageResolve(v.ownerName, oldName)).want(t, 200).object(t)["id"]; got != reusedID.String() || got == v.project.ID.String() {
		t.Fatal("name reuse restored historical identity")
	}
	v.request(t, v.ownerBrowser, "GET", projectUsagePath(reusedID)+"?cursor="+url.QueryEscape(cursor)).problem(t, 400, f.CursorInvalid)
	empty := v.request(t, v.ownerBrowser, "GET", projectUsagePath(reusedID)).want(t, 200)
	if string(empty.body) != `{"items":[],"next_cursor":null}` {
		t.Fatal("empty formal project has invented Usage")
	}
	checks = append(checks, projectUsageSchemaCase{"List", empty.body})
	// Account rename uses the public root's real Profile service, which already
	// owns the formal Avatar/Object collaborators. No replacement Profile port.
	root := startProjectUsageRoot(t, v)
	profile := root.request(t, v.ownerBrowser, "GET", "/api/v1/me", nil).want(t, 200).object(t)["user"].(map[string]any)
	oldUser := v.ownerName
	v.ownerName = "renamed-" + newID[struct{}](t).String()[24:]
	root.request(t, v.ownerBrowser, "PATCH", "/api/v1/me", map[string]any{"version": profile["version"], "username": v.ownerName}).want(t, 200)
	v.request(t, v.ownerBrowser, "GET", projectUsageResolve(oldUser, v.project.Name)).problem(t, 404, f.NotFound)
	v.request(t, v.ownerBrowser, "GET", projectUsageResolve(v.ownerName, v.project.Name)).want(t, 200)
	v.request(t, v.ownerBrowser, "GET", path+"?limit=2&cursor="+url.QueryEscape(cursor)).want(t, 200)
	root.stop(t)
	for _, bad := range []string{"?limit=01", "?limit=0", "?limit=101", "?status=unknown&status=failed", "?actor=admin", "?cursor=", "?limit=1;status=failed", "?from=2026-01-01T00%3A00%3A00Z"} {
		v.request(t, v.ownerBrowser, "GET", path+bad).problem(t, 400, f.InvalidArgument)
	}
	projectUsageCheckSchemas(t, checks)
	if strings.Contains(v.logs.text(), cursor) || strings.Contains(v.logs.text(), v.ownerBrowser.cookie) {
		t.Fatal("private browser/cursor leaked to HTTP log")
	}
	t.Log("formal owner projects and three joined wire results passed all eight aggregates, standard DTO schemas, HEAD, rename/name reuse and stable-ID cursor boundaries")
}
