package workhttp

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	c "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
)

func TestWorkHTTPSprintStartStandardSchema(t *testing.T) {
	python := os.Getenv("AGENTEAM_WORK_HTTP_SCHEMA_PYTHON")
	if python == "" {
		t.Fatal("fixed schema interpreter required")
	}
	var samples []schemaSample
	add := func(name string, raw []byte) {
		samples = append(samples, schemaSample{name, wireObject(t, raw), true})
		bad := wireObject(t, raw)
		bad["private_plan"] = "must-be-rejected"
		samples = append(samples, schemaSample{name, bad, false})
	}
	add("SprintStartBody", []byte(sprintStartBody(false)))
	add("SprintStartLookupRequest", []byte(sprintStartBody(true)))
	for _, lookup := range []bool{false, true} {
		for _, state := range []c.LookupState{c.LookupCommitted, c.LookupInProgress, c.LookupNotObserved} {
			if !lookup && state != c.LookupCommitted {
				continue
			}
			h, _, _, p := sprintLifecycleFixture()
			p.state = state
			w := newTestWriter()
			if serveTest(h, commandRequest("POST", sprintStartPath(lookup), sprintStartBody(lookup)), w) || w.Code != 200 {
				t.Fatal("actual HTTP projection failed", w.Code)
			}
			name := "SprintStartMutation"
			if lookup {
				name = "SprintStartLookup"
			}
			add(name, w.Body.Bytes())
		}
	}
	bad := wireObject(t, []byte(sprintStartBody(false)))
	bad["request"].(map[string]any)["scheduler_enabled"] = true
	samples = append(samples, schemaSample{"SprintStartBody", bad, false})
	bad = wireObject(t, []byte(sprintStartBody(true)))
	bad["command"] = "work.sprint.complete"
	samples = append(samples, schemaSample{"SprintStartLookupRequest", bad, false})
	h, _, _, _ := sprintLifecycleFixture()
	w := newTestWriter()
	if serveTest(h, commandRequest("POST", sprintStartPath(false), sprintStartBody(false)), w) || w.Code != 200 {
		t.Fatal("Start schema projection")
	}
	bad = wireObject(t, w.Body.Bytes())
	bad["project"].(map[string]any)["archived_at"] = nil
	samples = append(samples, schemaSample{"SprintStartMutation", bad, false})
	bad = wireObject(t, w.Body.Bytes())
	bad["sprint"].(map[string]any)["state"] = "planned"
	samples = append(samples, schemaSample{"SprintStartMutation", bad, false})
	h, _, _, port := sprintLifecycleFixture()
	port.result.Sprint.Title = strings.Repeat("&", 256)
	port.result.Sprint.Description = strings.Repeat("<", c.MaxDescriptionBytes)
	port.result.Project.Description = strings.Repeat(">", 8192)
	w = newTestWriter()
	if serveTest(h, commandRequest("POST", sprintStartPath(false), sprintStartBody(false)), w) || w.Code != 200 || w.Body.Len() > bodyLimit {
		t.Fatal("maximum legal escaped Start receipt was not completely published")
	}
	add("SprintStartMutation", w.Body.Bytes())
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
print('SPRINT_START_STANDARD_SCHEMA_PASS')`, root)
	cmd.Stdin = bytes.NewReader(data)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("standard schema: %v %s", err, out)
	}
	t.Log(strings.TrimSpace(string(out)))
}
