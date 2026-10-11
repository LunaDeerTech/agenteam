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
	c "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
)

func TestWorkHTTPTimelineWireSchema(t *testing.T) {
	python := os.Getenv("AGENTEAM_WORK_HTTP_SCHEMA_PYTHON")
	if python == "" {
		t.Fatal("fixed schema interpreter required")
	}
	items := timelineHTTPEvents(t)
	// Technical resolution is a Human transition source, retaining the original
	// System blocker creation elsewhere; it is not a standalone Human add.
	resolved := items[6].Transition.Clone()
	resolved.ID, resolved.Type = testID[c.TaskEvent](111), c.TaskTransitionBlockerResolved
	resolved.Payload = c.TaskTransitionFactPayload{BlockerResolved: &c.TaskBlockerResolvedPayload{BlockerID: testBlocker().ID, BlockerType: c.TaskBlockerTechnical}}
	items = append(items, c.TaskTimelineEvent{Transition: &resolved})
	var samples []schemaSample
	for _, item := range items {
		raw := timelineHTTPJSON(t, item)
		v := wireObject(t, raw)
		if len(v) != 10 {
			t.Fatal("timeline added a provenance wrapper or lost original fields")
		}
		samples = append(samples, schemaSample{"TaskTimelineEvent", v, true})
		bad := wireObject(t, raw)
		bad["task_version"] = 2
		samples = append(samples, schemaSample{"TaskTimelineEvent", bad, false})
		bad = wireObject(t, raw)
		bad["payload"].(map[string]any)["unknown"] = true
		samples = append(samples, schemaSample{"TaskTimelineEvent", bad, false})
		bad = wireObject(t, raw)
		bad["actor"].(map[string]any)["session_id"] = testActor().Details().SessionID
		samples = append(samples, schemaSample{"TaskTimelineEvent", bad, false})
	}
	large := items[6].Clone()
	large.Transition.Payload.Comment.Body = strings.Repeat("<", c.MaxTaskTransitionCommentBytes)
	raw := timelineHTTPJSON(t, large)
	if len(raw) <= c.MaxTaskEventBytes || len(raw) > c.MaxTaskTransitionCommentEventBytes {
		t.Fatal("large-comment sample did not exercise its original family bound")
	}
	samples = append(samples, schemaSample{"TaskTimelineEvent", wireObject(t, raw), true})
	for _, body := range []string{"", "\t \n", "unsafe\x00body"} {
		bad := wireObject(t, raw)
		bad["payload"].(map[string]any)["body"] = body
		samples = append(samples, schemaSample{"TaskTimelineEvent", bad, false})
	}
	bad := wireObject(t, timelineHTTPJSON(t, items[7]))
	bad["payload"].(map[string]any)["reason_code"] = "unknown"
	samples = append(samples, schemaSample{"TaskTimelineEvent", bad, false})
	bad = wireObject(t, timelineHTTPJSON(t, items[9]))
	bad["payload"].(map[string]any)["blocker_type"] = "waiting_for_human"
	samples = append(samples, schemaSample{"TaskTimelineEvent", bad, false})
	bad = wireObject(t, timelineHTTPJSON(t, items[len(items)-1]))
	bad["payload"].(map[string]any)["resolution_comment"] = "not a standalone resolution"
	samples = append(samples, schemaSample{"TaskTimelineEvent", bad, false})
	h, p := timelineHTTPFixture(items)
	p.page.NextCursor = "original-byte-short-cursor"
	w := newTestWriter()
	if serveTest(h, httptest.NewRequest("GET", timelineHTTPPath()+"?order=asc&limit=200", nil), w) || w.Code != 200 {
		t.Fatal("actual timeline HTTP schema sample failed", w.Code)
	}
	samples = append(samples, schemaSample{"TaskTimelinePage", wireObject(t, w.Body.Bytes()), true})
	for _, value := range []any{map[string]any{"items": nil}, map[string]any{"items": []any{}, "next_cursor": "cursor"}, map[string]any{"items": []any{}, "next_cursor": ""}, map[string]any{"items": []any{}, "unknown": true}} {
		samples = append(samples, schemaSample{"TaskTimelinePage", value, false})
	}
	samples = append(samples, schemaSample{"TaskTimelinePage", f.Page[c.TaskTimelineEvent]{Items: []c.TaskTimelineEvent{}}, true})
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
base='https://work-timeline-schema.invalid/'
registry=Registry()
for name in ['work-timeline.json','work-planning.json','common.json']:
    doc=json.loads((Path(sys.argv[1])/'api/openapi'/name).read_text())
    registry=registry.with_resource(base+name,Resource.from_contents(doc,default_specification=DRAFT202012))
for i,c in enumerate(json.load(sys.stdin)):
    schema={'$ref':base+'work-timeline.json#/components/schemas/'+c['name']}
    errors=list(Draft202012Validator(schema,registry=registry,format_checker=FormatChecker()).iter_errors(c['value']))
    if (not errors)!=c['valid']:
        print('CASE_FAILED',i,c['name'])
        for e in errors[:3]: print('RULE',e.validator,'PATH',list(e.absolute_schema_path))
        sys.exit(1)
print('WORK_TIMELINE_SCHEMA_PASS')`, root)
	cmd.Stdin = bytes.NewReader(data)
	out, err := cmd.CombinedOutput()
	if err != nil || !bytes.Contains(out, []byte("WORK_TIMELINE_SCHEMA_PASS")) {
		t.Fatalf("timeline schema validation: %v %s", err, out)
	}
}
