package projecthttp

import (
	"bytes"
	"context"
	"encoding/json"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/httpapi"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func updateDecode(t *testing.T, raw string) (m f.CommandMeta, p pc.UpdateProjectRequest, e error) {
	t.Helper()
	httpapi.WithRequestID(nil, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { m, p, e = decodeUpdate(w, r, "original-key") })).ServeHTTP(httptest.NewRecorder(), updateRequest("PATCH", httpDetailPath, raw))
	return
}
func TestProjectOwnerUpdateWireStrictPresence(t *testing.T) {
	for _, raw := range []string{`{"expected_version":"1","name":"Project"}`, `{"expected_version":"9223372036854775807","description":""}`, `{"expected_version":"9007199254740993","name":"Project","description":"test"}`} {
		m, p, e := updateDecode(t, raw)
		if e != nil || m.Validate() != nil || p.Validate() != nil {
			t.Fatal(raw, e)
		}
	}
	for _, raw := range []string{`{}`, `{"description":"test"}`, `{"expected_version":"1"}`, `{"expected_version":null,"description":""}`, `{"expected_version":1,"description":""}`, `{"expected_version":"01","description":""}`, `{"expected_version":"0","description":""}`, `{"expected_version":"9223372036854775808","description":""}`, `{"expected_version":"1","name":null}`, `{"expected_version":"1","description":null}`, `{"expected_version":"1","name":""}`, `{"expected_version":"1","Name":"Project"}`, `{"expected_version":"1","name":"A","name":"B"}`, `{"expected_version":"1","description":"","owner":"x"}`, `{"expected_version":"1","description":""} {}`, `{"expected_version":"1","description":[]}`, `{"expected_version":"1","description":{}}`} {
		if _, _, e := updateDecode(t, raw); e == nil {
			t.Fatal("bad strict input", raw)
		}
	}
	m, p, e := updateDecode(t, `{"expected_version":"1","description":""}`)
	if e != nil || p.Name != nil || p.Description == nil || *p.Description != "" || *m.ExpectedVersion != 1 {
		t.Fatal("absence versus empty")
	}
}
func TestProjectOwnerUpdateWireProjectionAndMaximum(t *testing.T) {
	p := wireProject()
	p.Name = strings.Repeat("N", 64)
	p.NormalizedName = strings.ToLower(p.Name)
	p.Description = strings.Repeat("&", 8192)
	p.Version = f.Version(math.MaxInt64)
	input, _ := json.Marshal(map[string]any{"expected_version": "9223372036854775807", "name": p.Name, "description": p.Description})
	m, r, e := updateDecode(t, string(input))
	if e != nil || len(input) > updateBodyLimit || len(input) < 49152 {
		t.Fatal("max request", len(input), e)
	}
	raw, e := encodeUpdate(context.Background(), wireActor(), p.ID, m, r, p)
	if e != nil || len(raw) > updateBodyLimit || bytes.Count(raw, []byte(`\u0026`)) != 8192 {
		t.Fatal("max response", len(raw), e)
	}
	lookup, e := encodeUpdateLookup(context.Background(), wireActor(), p.ID, pc.CommandLookupResult{State: pc.LookupCommitted, Result: &pc.CommandResult{Command: pc.UpdateCommand, Project: &p}})
	if e != nil || len(lookup) > updateBodyLimit || bytes.Count(lookup, []byte(`\u0026`)) != 8192 {
		t.Fatal("max lookup", e)
	}
	for _, b := range [][]byte{raw, lookup} {
		v := wireObject(t, b)
		if v["result"] != nil {
			v = v["result"].(map[string]any)["project"].(map[string]any)
		}
		if len(v) != 11 || v["description"] != p.Description || v["version"] != "9223372036854775807" {
			t.Fatal("truncation/unsafe field")
		}
	}
	t.Logf("MAXIMUM_UPDATE request=%d project=%d lookup=%d limit=%d description_bytes=8192 roundtrip=true", len(input), len(raw), len(lookup), updateBodyLimit)
	m, r, _ = updateDecode(t, updateTestBody)
	for _, change := range []func(*pc.ProjectRef){func(v *pc.ProjectRef) { v.ID = pc.ProjectID{} }, func(v *pc.ProjectRef) { v.OwnerUserID = id.UserID{} }, func(v *pc.ProjectRef) { v.Version = 3 }, func(v *pc.ProjectRef) { v.Description = "other" }, func(v *pc.ProjectRef) { v.Lifecycle = pc.Archiving }} {
		bad := wireProject()
		change(&bad)
		if b, e := encodeUpdate(context.Background(), wireActor(), wireProject().ID, m, r, bad); e == nil || b != nil {
			t.Fatal("bad receipt accepted")
		}
	}
	for _, state := range []pc.LookupState{pc.LookupInProgress, pc.LookupNotObserved} {
		b, e := encodeUpdateLookup(context.Background(), wireActor(), wireProject().ID, pc.CommandLookupResult{State: state})
		if e != nil || len(wireObject(t, b)) != 1 {
			t.Fatal("invented receipt")
		}
	}
	good := wireProject()
	for _, v := range []pc.CommandLookupResult{{State: pc.LookupCommitted}, {State: pc.LookupInProgress, Result: &pc.CommandResult{Command: pc.UpdateCommand, Project: &good}}, {State: pc.LookupCommitted, Result: &pc.CommandResult{Command: pc.RestoreCommand, Project: &good}}} {
		if b, e := encodeUpdateLookup(context.Background(), wireActor(), good.ID, v); e == nil || b != nil {
			t.Fatal("bad union")
		}
	}
}
func TestProjectOwnerUpdateWireStandardSchema(t *testing.T) {
	python := os.Getenv("AGENTEAM_PROJECT_READ_SCHEMA_PYTHON")
	if python == "" {
		t.Fatal("fixed schema interpreter required")
	}
	type sample struct {
		Name  string `json:"name"`
		Value any    `json:"value"`
		Valid bool   `json:"valid"`
	}
	cases := []sample{}
	for _, raw := range []string{updateTestBody, `{"expected_version":"1","name":"A"}`, `{"expected_version":"1"}`, `{"expected_version":"1","description":null}`, `{"expected_version":"1","description":"","extra":1}`, `{"expected_version":"0","name":"A"}`, `{"expected_version":"9223372036854775807","name":"A"}`, `{"expected_version":"9223372036854775808","name":"A"}`} {
		_, _, err := updateDecode(t, raw)
		cases = append(cases, sample{"UpdateInput", wireObject(t, []byte(raw)), err == nil})
	}
	for _, command := range []string{"update", "create", "archive", "restore", "delete", "retry-lifecycle", "UPDATE"} {
		cases = append(cases, sample{"UpdateLookupInput", map[string]any{"command": command}, command == "update"})
	}
	p := wireProject()
	raw, _ := encodeUpdateLookup(context.Background(), wireActor(), p.ID, pc.CommandLookupResult{State: pc.LookupCommitted, Result: &pc.CommandResult{Command: pc.UpdateCommand, Project: &p}})
	cases = append(cases, sample{"UpdateLookupResult", wireObject(t, raw), true})
	for _, state := range []string{"in_progress", "not_observed", "committed", "unknown"} {
		cases = append(cases, sample{"UpdateLookupResult", map[string]any{"state": state}, state == "in_progress" || state == "not_observed"}, sample{"UpdateLookupResult", map[string]any{"state": state, "result": nil}, false})
	}
	data, _ := json.Marshal(cases)
	root, _ := filepath.Abs("../../../..")
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, python, "-c", `import json,sys
from pathlib import Path
from jsonschema import Draft202012Validator,FormatChecker
from referencing import Registry,Resource
from referencing.jsonschema import DRAFT202012
base='https://project-update-schema.invalid/'
registry=Registry()
for name in ['project-owner.json','common.json']:
    doc=json.loads((Path(sys.argv[1])/'api/openapi'/name).read_text())
    registry=registry.with_resource(base+name,Resource.from_contents(doc,default_specification=DRAFT202012))
for i,c in enumerate(json.load(sys.stdin)):
    schema={'$ref':base+'project-owner.json#/components/schemas/'+c['name']}
    ok=not list(Draft202012Validator(schema,registry=registry,format_checker=FormatChecker()).iter_errors(c['value']))
    if ok!=c['valid']: print('CASE_FAILED',i,c['name']);sys.exit(1)
print('UPDATE_STANDARD_SCHEMA_PASS')`, root)
	cmd.Stdin = bytes.NewReader(data)
	out, e := cmd.CombinedOutput()
	if e != nil {
		t.Fatalf("schema: %v %s", e, out)
	}
	t.Log(strings.TrimSpace(string(out)))
}
