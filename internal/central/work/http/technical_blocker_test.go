package workhttp

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
)

// These controlled domain values exercise the existing HTTP read boundary.
// They do not establish Scheduler failure authority or database persistence.
func technicalBlockerFixture() c.TaskBlocker {
	v := testBlocker()
	v.Type, v.Description = c.TaskBlockerTechnical, "Scheduler launch failed."
	v.Metadata, v.CreatedBy = c.TaskBlockerMetadata{}, c.TaskEventActor{}
	cause := testID[c.SchedulerClaim](80).String()
	v.Technical = &c.TaskBlockerTechnicalMetadata{Code: "scheduler_launch_failed", Source: "scheduler_dispatch", ReferenceID: cause}
	v.SchedulerCreatedBy = &c.SchedulerTaskActor{CauseID: cause}
	return v
}

func technicalBlockerPage(t *testing.T, v c.TaskBlocker) ([]byte, []byte) {
	t.Helper()
	h, boundary, p := testHandler()
	p.bl = f.Page[c.TaskBlocker]{Items: []c.TaskBlocker{v}}
	w := newTestWriter()
	r := httptest.NewRequest("GET", testPath("/tasks/"+v.TaskID.String()+"/blockers"), nil)
	if serveTest(h, r, w) || w.Code != 200 || p.calls != 1 || boundary.checks.Load() != 1 || boundary.auths.Load() != 1 {
		t.Fatal("original authenticated read did not publish a complete page", w.Code)
	}
	var page struct {
		Items []json.RawMessage `json:"items"`
	}
	if json.Unmarshal(w.Body.Bytes(), &page) != nil || len(page.Items) != 1 {
		t.Fatal("incomplete Blocker page")
	}
	got, err := c.DecodeTaskBlocker(page.Items[0])
	if err != nil || !reflect.DeepEqual(got, v) {
		t.Fatal("HTTP changed the original typed record", err)
	}
	return append([]byte(nil), w.Body.Bytes()...), page.Items[0]
}

func TestWorkHTTPTechnicalBlockerReadOnly(t *testing.T) {
	v := technicalBlockerFixture()
	_, raw := technicalBlockerPage(t, v)
	wire := wireObject(t, raw)
	metadata, actor := wire["metadata"].(map[string]any), wire["created_by"].(map[string]any)
	if len(wire) != 11 || len(metadata) != 3 || len(actor) != 4 || metadata["reference_id"] != actor["cause_id"] || actor["type"] != "system" || actor["service_name"] != "scheduler" || actor["source"] != "scheduler" {
		t.Fatal("technical read lost its closed wire shape")
	}
	for _, key := range []string{"resolved_at", "resolved_by", "resolution_comment"} {
		if wire[key] != nil {
			t.Fatal("unsupported technical resolution was published")
		}
	}
	for _, edit := range []func(*c.TaskBlocker){
		func(x *c.TaskBlocker) { x.Technical.ReferenceID = testID[c.SchedulerClaim](81).String() },
		func(x *c.TaskBlocker) { x.CreatedBy = testBlocker().CreatedBy },
		func(x *c.TaskBlocker) { x.ProjectID = testID[id.Project](82) },
		func(x *c.TaskBlocker) { x.ResolvedAt = &x.CreatedAt },
		func(x *c.TaskBlocker) { x.Technical.Code = "private-invalid-code" },
	} {
		h, _, p := testHandler()
		bad := v.Clone()
		edit(&bad)
		p.bl.Items = []c.TaskBlocker{bad}
		w := newTestWriter()
		r := httptest.NewRequest("GET", testPath("/tasks/"+v.TaskID.String()+"/blockers"), nil)
		if serveTest(h, r, w) || w.Code != 503 || p.calls != 1 || strings.Contains(w.Body.String(), v.Description) || strings.Contains(w.Body.String(), "private-invalid-code") || strings.Contains(w.Body.String(), v.Technical.ReferenceID) {
			t.Fatal("invalid dependency record was published or leaked", w.Code)
		}
	}
	request := map[string]any{"blocker_id": wire["id"], "type": "technical", "description": wire["description"], "metadata": metadata}
	add, err := json.Marshal(map[string]any{"expected_version": "1", "request": request})
	if err != nil {
		t.Fatal(err)
	}
	h, p := commandFixture()
	w := newTestWriter()
	if serveTest(h, commandRequest("POST", "/tasks/"+v.TaskID.String()+"/blockers", string(add)), w) || w.Code != 503 || p.calls != 0 || !strings.Contains(w.Body.String(), "DEPENDENCY_UNBOUND") {
		t.Fatal("read variant enabled Human Add", w.Code)
	}
	transfer := wireObject(t, []byte(transitionBody(t, false)))
	transfer["request"].(map[string]any)["add_blockers"] = []any{request}
	body, err := json.Marshal(transfer)
	if err != nil {
		t.Fatal(err)
	}
	th, _, old, tp := transitionFixture()
	w = newTestWriter()
	if serveTest(th, commandRequest("POST", transitionPath(false), string(body)), w) || w.Code != 503 || old.calls != 0 || tp.writes != 0 || tp.lookups != 0 || !strings.Contains(w.Body.String(), "DEPENDENCY_UNBOUND") {
		t.Fatal("read variant enabled Human Transfer", w.Code)
	}
}

func TestWorkHTTPTechnicalBlockerStandardSchema(t *testing.T) {
	python := os.Getenv("AGENTEAM_WORK_HTTP_SCHEMA_PYTHON")
	if python == "" {
		t.Fatal("fixed schema interpreter required")
	}
	page, raw := technicalBlockerPage(t, technicalBlockerFixture())
	samples := []schemaSample{{"TaskBlockerPage", wireObject(t, page), true}, {"TaskBlocker", wireObject(t, raw), true}}
	_, human := technicalBlockerPage(t, testBlocker())
	samples = append(samples, schemaSample{"TaskBlocker", wireObject(t, human), true})
	for _, edit := range []func(map[string]any){
		func(v map[string]any) { v["metadata"].(map[string]any)["code"] = "other" },
		func(v map[string]any) { v["metadata"].(map[string]any)["source"] = "other" },
		func(v map[string]any) { v["metadata"].(map[string]any)["extra"] = true },
		func(v map[string]any) { v["created_by"].(map[string]any)["service_name"] = "other" },
		func(v map[string]any) { v["created_by"].(map[string]any)["user_id"] = testActor().Details().UserID },
		func(v map[string]any) { v["resolved_at"] = v["created_at"] },
		func(v map[string]any) { v["resolution_comment"] = "unsupported" },
		func(v map[string]any) { v["private_plan"] = true },
	} {
		bad := wireObject(t, raw)
		edit(bad)
		samples = append(samples, schemaSample{"TaskBlocker", bad, false})
	}
	// The read-only actor and metadata must not widen either Human write DTO.
	v := wireObject(t, raw)
	request := map[string]any{"blocker_id": v["id"], "type": v["type"], "description": v["description"], "metadata": v["metadata"]}
	samples = append(samples, schemaSample{"BlockerAddRequest", request, false}, schemaSample{"TaskEventActor", v["created_by"], false})
	transfer := wireObject(t, []byte(transitionBody(t, false)))
	transfer["request"].(map[string]any)["add_blockers"] = []any{request}
	samples = append(samples, schemaSample{"TaskTransferBody", transfer, false})
	data, err := json.Marshal(samples)
	if err != nil {
		t.Fatal(err)
	}
	root, err := filepath.Abs("../../../..")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, python, "-c", `import json,sys
from pathlib import Path
from jsonschema import Draft202012Validator,FormatChecker
from referencing import Registry,Resource
from referencing.jsonschema import DRAFT202012
base='https://work-planning-schema.invalid/'
registry=Registry()
for name in ['work-planning.json','common.json']:
    doc=json.loads((Path(sys.argv[1])/'api/openapi'/name).read_text())
    registry=registry.with_resource(base+name,Resource.from_contents(doc,default_specification=DRAFT202012))
for i,c in enumerate(json.load(sys.stdin)):
    schema={'$ref':base+'work-planning.json#/components/schemas/'+c['name']}
    errors=list(Draft202012Validator(schema,registry=registry,format_checker=FormatChecker()).iter_errors(c['value']))
    if (not errors)!=c['valid']:
        print('CASE_FAILED',i,c['name'])
        sys.exit(1)
print('TECHNICAL_BLOCKER_STANDARD_SCHEMA_PASS')`, root)
	cmd.Stdin = bytes.NewReader(data)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("standard schema: %v %s", err, out)
	}
	t.Log(strings.TrimSpace(string(out)))
}
