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

func TestWorkHTTPTaskTransitionStandardSchema(t *testing.T) {
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
	add("TaskTransferBody", []byte(transitionBody(t, false)))
	add("TaskTransitionLookupRequest", []byte(transitionBody(t, true)))
	for _, request := range reviewTransitionRequests() {
		for _, lookup := range []bool{false, true} {
			body := reviewTransitionBody(t, request, lookup)
			input, output := "TaskTransferBody", "TaskTransitionMutation"
			if lookup {
				input, output = "TaskTransitionLookupRequest", "TaskTransitionLookup"
			}
			add(input, []byte(body))
			h, _, _, p := transitionFixture()
			p.result.Task.State = request.TargetState
			if request.AssigneeAgentID != nil {
				p.result.Task.AssigneeAgentID = ptr(*request.AssigneeAgentID)
			}
			w := newTestWriter()
			if serveTest(h, commandRequest("POST", transitionPath(lookup), body), w) || w.Code != 200 {
				t.Fatal("actual review HTTP projection failed", lookup, w.Code)
			}
			add(output, w.Body.Bytes())
			for _, field := range []string{"assignee_agent_id", "comment", "reviewer_agent_id"} {
				bad := wireObject(t, []byte(body))
				var value any
				if field == "reviewer_agent_id" {
					// Use a valid ID so rejection proves the field is unknown.
					value = p.result.Task.AssigneeAgentID.String()
				}
				bad["request"].(map[string]any)[field] = value
				samples = append(samples, schemaSample{input, bad, false})
			}
		}
	}
	for _, lookup := range []bool{false, true} {
		for _, state := range []c.LookupState{c.LookupCommitted, c.LookupInProgress, c.LookupNotObserved} {
			if !lookup && state != c.LookupCommitted {
				continue
			}
			h, _, _, p := transitionFixture()
			p.status = state
			w := newTestWriter()
			if serveTest(h, commandRequest("POST", transitionPath(lookup), transitionBody(t, lookup)), w) || w.Code != 200 {
				t.Fatal("actual HTTP projection failed", w.Code)
			}
			name := "TaskTransitionMutation"
			if lookup {
				name = "TaskTransitionLookup"
			}
			add(name, w.Body.Bytes())
		}
	}
	bad := wireObject(t, []byte(transitionBody(t, false)))
	bad["request"].(map[string]any)["assignee_agent_id"] = nil
	samples = append(samples, schemaSample{"TaskTransferBody", bad, false})
	bad = wireObject(t, []byte(transitionBody(t, true)))
	bad["command"] = "work.task.update"
	samples = append(samples, schemaSample{"TaskTransitionLookupRequest", bad, false})
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
print('TASK_TRANSITION_STANDARD_SCHEMA_PASS')`, root)
	cmd.Stdin = bytes.NewReader(data)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("standard schema: %v %s", err, out)
	}
	t.Log(strings.TrimSpace(string(out)))
}
