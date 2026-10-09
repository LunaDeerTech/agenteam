package workhttp

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
)

type schemaSample struct {
	Name  string `json:"name"`
	Value any    `json:"value"`
	Valid bool   `json:"valid"`
}

func wireObject(t *testing.T, raw []byte) map[string]any {
	t.Helper()
	var v map[string]any
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatal(err)
	}
	return v
}

func TestWorkHTTPMaximumWireAndStandardSchema(t *testing.T) {
	python := os.Getenv("AGENTEAM_WORK_HTTP_SCHEMA_PYTHON")
	if python == "" {
		t.Fatal("fixed schema interpreter required")
	}
	var samples []schemaSample
	add := func(name string, raw []byte) {
		v := wireObject(t, raw)
		samples = append(samples, schemaSample{name, v, true})
		bad := wireObject(t, raw)
		bad["unknown"] = true
		samples = append(samples, schemaSample{name, bad, false})
	}
	for _, kind := range []resource{milestones, sprints, tasks, blockers} {
		h, _, p := testHandler()
		p.m.Title = strings.Repeat("&", 256)
		p.m.Description = strings.Repeat("<", c.MaxDescriptionBytes)
		p.s.Title = p.m.Title
		p.s.Description = p.m.Description
		p.t.Title = p.m.Title
		p.t.Description = p.m.Description
		p.t.Plan = strings.Repeat(">", c.MaxTaskPlanTextBytes)
		p.b.Description = strings.Repeat("<", c.MaxTaskBlockerDescriptionBytes)
		p.b.ResolvedAt = ptr(testAt())
		p.b.ResolvedBy = ptr(p.b.CreatedBy)
		p.b.ResolutionComment = ptr(strings.Repeat(">", c.MaxTaskBlockerResolutionCommentBytes))
		if p.m.Validate() != nil || p.s.Validate() != nil || p.t.Validate() != nil || p.b.Validate() != nil {
			t.Fatal("maximum fixture invalid")
		}
		var path, pageName, detailName string
		var detailPath string
		switch kind {
		case milestones:
			path = "/milestones?limit=200"
			pageName = "MilestoneSummaryPage"
			detailName = "Milestone"
			detailPath = "/milestones/" + p.m.ID.String()
			p.ml.Items = make([]c.Milestone, 200)
			p.ml.NextCursor = strings.Repeat("c", 8192)
			for n := range p.ml.Items {
				v := p.m
				v.ID = testID[c.Milestone](n + 1)
				p.ml.Items[n] = v
			}
		case sprints:
			path = "/sprints?limit=200&milestone_id=" + p.s.MilestoneID.String()
			pageName = "SprintSummaryPage"
			detailName = "Sprint"
			detailPath = "/sprints/" + p.s.ID.String()
			p.sl.Items = make([]c.Sprint, 200)
			p.sl.NextCursor = strings.Repeat("c", 8192)
			for n := range p.sl.Items {
				v := p.s
				v.ID = testID[pc.Sprint](n + 1)
				p.sl.Items[n] = v
			}
		case tasks:
			path = "/tasks?limit=200"
			pageName = "TaskSummaryPage"
			detailName = "Task"
			detailPath = "/tasks/" + p.t.ID.String()
			p.tl.Items = make([]c.Task, 200)
			p.tl.NextCursor = strings.Repeat("c", 8192)
			for n := range p.tl.Items {
				v := p.t
				v.ID = testID[c.Task](n + 1)
				p.tl.Items[n] = v
			}
		case blockers:
			path = "/tasks/" + p.t.ID.String() + "/blockers?limit=200&status=resolved"
			pageName = "TaskBlockerPage"
			p.bl.Items = make([]c.TaskBlocker, 200)
			p.bl.NextCursor = strings.Repeat("c", 8192)
			for n := range p.bl.Items {
				v := p.b
				v.ID = testID[c.TaskBlockerIdentity](n + 1)
				p.bl.Items[n] = v
			}
		}
		w := newTestWriter()
		if serveTest(h, httptest.NewRequest("GET", testPath(path), nil), w) || w.Code != 200 || w.Body.Len() > listLimit {
			t.Fatal("maximum page did not encode", pageName, w.Code)
		}
		var decoded struct {
			Items []json.RawMessage `json:"items"`
			Next  string            `json:"next_cursor"`
		}
		if json.Unmarshal(w.Body.Bytes(), &decoded) != nil || len(decoded.Items) != 200 || len(decoded.Next) != 8192 {
			t.Fatal("maximum page roundtrip")
		}
		for _, item := range decoded.Items {
			if len(item) > 16<<10 {
				t.Fatal("per-item bound")
			}
		}
		add(pageName, w.Body.Bytes())
		if detailName != "" {
			w = newTestWriter()
			if serveTest(h, httptest.NewRequest("GET", testPath(detailPath), nil), w) || w.Code != 200 || w.Body.Len() > bodyLimit {
				t.Fatal("maximum detail", detailName, w.Code)
			}
			add(detailName, w.Body.Bytes())
		}
	}
	// Feed the actual command/Lookup HTTP output and the saved pre-send intent,
	// not a second hand-written result DTO, to the published schema.
	for _, s := range commandSamples() {
		for _, lookup := range []bool{false, true} {
			h, p := commandFixture()
			s.prepare(p)
			method, path, name := s.method, s.path, s.schema
			if lookup {
				method, path, name = "POST", s.lookupPath, s.lookupSchema
			}
			body := sampleBody(t, s, lookup)
			add(name, []byte(body))
			w := newTestWriter()
			if serveTest(h, commandRequest(method, path, body), w) || w.Code != 200 {
				t.Fatal("schema sample output")
			}
			result := "StructureMutation"
			if s.lookupSchema == "TaskLookupRequest" {
				result = "TaskMutation"
			}
			if s.lookupSchema == "BlockerLookupRequest" {
				result = "BlockerMutation"
			}
			if lookup {
				result = strings.TrimSuffix(s.lookupSchema, "Request")
			}
			add(result, w.Body.Bytes())
			if lookup {
				for _, state := range []c.LookupState{c.LookupInProgress, c.LookupNotObserved} {
					p.lookupState = state
					w = newTestWriter()
					if serveTest(h, commandRequest(method, path, body), w) || w.Code != 200 {
						t.Fatal("lookup null schema sample")
					}
					add(result, w.Body.Bytes())
					v := wireObject(t, w.Body.Bytes())
					delete(v, "receipt")
					delete(v, "result")
					samples = append(samples, schemaSample{result, v, false})
				}
			}
		}
	}
	// Maximal escaping still reaches the original strict DTO and service once.
	s := commandSamples()[6]
	request := s.request.(c.TaskCreate)
	request.Title = strings.Repeat("&", 256)
	request.Description = strings.Repeat("<", c.MaxDescriptionBytes)
	request.Plan = strings.Repeat(">", c.MaxTaskPlanTextBytes)
	s.request = request
	h, p := commandFixture()
	s.prepare(p)
	p.taskResult.Task.Title = request.Title
	p.taskResult.Task.Description = request.Description
	p.taskResult.Task.Plan = request.Plan
	w := newTestWriter()
	body := sampleBody(t, s, false)
	if len(body) > bodyLimit || serveTest(h, commandRequest(s.method, s.path, body), w) || w.Code != 200 || p.calls != 1 {
		t.Fatal("maximum escaped input was rejected", w.Code)
	}
	add("TaskCreateBody", []byte(body))
	add("TaskMutation", w.Body.Bytes())
	for _, name := range []string{"MilestoneSummaryPage", "SprintSummaryPage", "TaskSummaryPage", "TaskBlockerPage"} {
		samples = append(samples, schemaSample{name, map[string]any{"items": []any{}}, true}, schemaSample{name, map[string]any{"items": nil}, false}, schemaSample{name, map[string]any{"items": []any{}, "next_cursor": ""}, false})
	}
	// Nullable presence and all closed enums are checked by standard validation.
	v := wireObject(t, []byte(sampleBody(t, commandSamples()[10], false)))
	v["expected_version"] = 1
	samples = append(samples, schemaSample{"BlockerResolveBody", v, false})
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
        for e in errors[:3]: print('RULE',e.validator,'PATH',list(e.absolute_schema_path))
        sys.exit(1)
print('WORK_STANDARD_SCHEMA_PASS')`, root)
	cmd.Stdin = bytes.NewReader(data)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("standard schema: %v %s", err, out)
	}
	t.Log(strings.TrimSpace(string(out)))
}

func TestWorkHTTPEmptyPageAndNullableSummary(t *testing.T) {
	h, _, p := testHandler()
	p.tl = f.Page[c.Task]{}
	w := newTestWriter()
	if serveTest(h, httptest.NewRequest("GET", testPath("/tasks"), nil), w) || w.Body.String() != `{"items":[]}` {
		t.Fatal("empty page wire")
	}
	h, _, p = testHandler()
	p.tl.Items[0].AssigneeAgentID = ptr(testID[id.Agent](50))
	p.tl.Items[0].State = c.TaskStateInProgress
	w = newTestWriter()
	if serveTest(h, httptest.NewRequest("GET", testPath("/tasks"), nil), w) || w.Code != 200 {
		t.Fatal("assigned list projection")
	}
}
