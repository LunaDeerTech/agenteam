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

func TestWorkHTTPSprintCompleteStandardSchema(t *testing.T) {
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
	add("SprintCompleteBody", []byte(sprintCompleteBody(false)))
	add("SprintCompleteLookupRequest", []byte(sprintCompleteBody(true)))
	for _, lookup := range []bool{false, true} {
		for _, state := range []c.LookupState{c.LookupCommitted, c.LookupInProgress, c.LookupNotObserved} {
			if !lookup && state != c.LookupCommitted {
				continue
			}
			h, _, _, p := sprintCompletionFixture()
			p.state = state
			w := newTestWriter()
			if serveTest(h, commandRequest("POST", sprintCompletePath(lookup), sprintCompleteBody(lookup)), w) || w.Code != 200 {
				t.Fatal("actual HTTP projection failed", w.Code)
			}
			name := "SprintCompleteMutation"
			if lookup {
				name = "SprintCompleteLookup"
			}
			add(name, w.Body.Bytes())
		}
	}
	bad := wireObject(t, []byte(sprintCompleteBody(false)))
	bad["request"].(map[string]any)["scheduler_enabled"] = true
	samples = append(samples, schemaSample{"SprintCompleteBody", bad, false})
	bad = wireObject(t, []byte(sprintCompleteBody(true)))
	bad["command"] = "work.sprint.start"
	samples = append(samples, schemaSample{"SprintCompleteLookupRequest", bad, false})
	h, _, _, _ := sprintCompletionFixture()
	w := newTestWriter()
	if serveTest(h, commandRequest("POST", sprintCompletePath(false), sprintCompleteBody(false)), w) || w.Code != 200 {
		t.Fatal("Complete schema projection")
	}
	bad = wireObject(t, w.Body.Bytes())
	bad["project"].(map[string]any)["archived_at"] = nil
	samples = append(samples, schemaSample{"SprintCompleteMutation", bad, false})
	bad = wireObject(t, w.Body.Bytes())
	bad["sprint"].(map[string]any)["state"] = "planned"
	samples = append(samples, schemaSample{"SprintCompleteMutation", bad, false})
	h, _, _, port := sprintCompletionFixture()
	port.result.Sprint.Title = strings.Repeat("&", 256)
	port.result.Sprint.Description = strings.Repeat("<", c.MaxDescriptionBytes)
	port.result.Project.Description = strings.Repeat(">", 8192)
	w = newTestWriter()
	if serveTest(h, commandRequest("POST", sprintCompletePath(false), sprintCompleteBody(false)), w) || w.Code != 200 || w.Body.Len() > bodyLimit {
		t.Fatal("maximum legal escaped Complete receipt was not completely published")
	}
	add("SprintCompleteMutation", w.Body.Bytes())
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
print('SPRINT_COMPLETE_STANDARD_SCHEMA_PASS')`, root)
	cmd.Stdin = bytes.NewReader(data)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("standard schema: %v %s", err, out)
	}
	t.Log(strings.TrimSpace(string(out)))
}
