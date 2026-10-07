package projecthttp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
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

func wireID[K any]() f.ID[K] {
	v, e := f.ParseID[K]("01900000-0000-7000-8000-000000000001")
	if e != nil {
		panic(e)
	}
	return v
}
func wireInstant() f.Instant {
	v, e := f.ParseInstant("2026-10-07T01:02:03.123456Z")
	if e != nil {
		panic(e)
	}
	return v
}
func wireActor() id.Actor {
	a, e := id.NewHuman(wireID[id.User](), wireID[id.Session]())
	if e != nil {
		panic(e)
	}
	return a
}
func wireProject() pc.ProjectRef {
	return pc.ProjectRef{ID: wireID[id.Project](), OwnerUserID: wireID[id.User](), Name: "Project", NormalizedName: "project", Description: "test", Lifecycle: pc.Active, Version: 1, CreatedAt: wireInstant(), UpdatedAt: wireInstant()}
}
func wirePtr[T any](v T) *T { return &v }
func wirePage() f.Page[pc.ProjectListItem] {
	p := wireProject()
	return f.Page[pc.ProjectListItem]{Items: []pc.ProjectListItem{{ID: p.ID, Name: p.Name, Lifecycle: p.Lifecycle, Version: p.Version, Description: &p.Description}}}
}
func wireMaximumPage() f.Page[pc.ProjectListItem] {
	page := f.Page[pc.ProjectListItem]{Items: make([]pc.ProjectListItem, 100), NextCursor: strings.Repeat("x", 8192)}
	for i := range page.Items {
		p := wirePage().Items[0]
		p.ID, _ = f.ParseID[id.Project](fmt.Sprintf("01900000-0000-7000-8000-%012x", i+1))
		p.Name = strings.Repeat("N", 64)
		p.Version = f.Version(math.MaxInt64)
		p.Lifecycle = pc.Archiving
		p.OperationID = wirePtr(wireID[pc.Operation]())
		p.Description = wirePtr(strings.Repeat("&", 8192))
		page.Items[i] = p
	}
	return page
}
func wireRequest(raw string) *http.Request {
	return &http.Request{Method: "GET", URL: &url.URL{RawQuery: raw}}
}
func wireObject(t *testing.T, raw []byte) map[string]any {
	t.Helper()
	var v map[string]any
	if e := json.Unmarshal(raw, &v); e != nil {
		t.Fatal(e)
	}
	return v
}
func TestProjectOwnerReadWireStrictQuery(t *testing.T) {
	q, p, e := projectQuery(wireRequest(""), true)
	if e != nil || q.Lifecycle != nil || p.Limit != 50 || p.Cursor != "" {
		t.Fatal("defaults changed", e)
	}
	q, p, e = projectQuery(wireRequest("lifecycle=archived%2Cactive&limit=100&cursor=opaque"), true)
	if e != nil || q.Validate() != nil || p.Limit != 100 || p.Cursor != "opaque" {
		t.Fatal("valid set rejected", e)
	}
	for _, raw := range []string{"owner=x", "limit=", "limit=0", "limit=01", "limit=-1", "limit=+1", "limit=101", "limit=1e1", "limit=١", "limit=1&limit=2", "limit=1&%6cimit=2", "lifecycle=active,active", "lifecycle=active, archived", "lifecycle=active,", "lifecycle=ACTIVE", "lifecycle=active&lifecycle=archived", "cursor=", "x", "=x", "&limit=1", "limit=1&", "limit=1;cursor=x", "cursor=%zz", "cursor=%ff", "cursor=%00", strings.Repeat("x", maxQueryBytes+1)} {
		t.Run(raw[:min(len(raw), 50)], func(t *testing.T) {
			if _, _, e := projectQuery(wireRequest(raw), true); e == nil {
				t.Fatal("bad query accepted")
			}
		})
	}
	force := wireRequest("")
	force.URL.ForceQuery = true
	if _, _, e := projectQuery(force, true); e == nil {
		t.Fatal("empty query marker")
	}
	if _, _, e := projectQuery(wireRequest("cursor="+strings.Repeat("x", 8193)), true); e == nil {
		t.Fatal("overlong cursor")
	}
	for _, raw := range []string{"limit=1", "lifecycle=active", "cursor=x"} {
		if _, _, e := projectQuery(wireRequest(raw), false); e == nil {
			t.Fatal("detail query accepted")
		}
	}
}
func TestProjectOwnerReadWireStatesPrecisionAndHiddenDamage(t *testing.T) {
	for _, state := range []pc.Lifecycle{pc.Active, pc.Archiving, pc.Archived, pc.Deleting} {
		t.Run(string(state), func(t *testing.T) {
			page := wirePage()
			item := &page.Items[0]
			item.Lifecycle = state
			item.Version = 9007199254740993
			if state == pc.Archiving || state == pc.Deleting {
				item.OperationID = wirePtr(wireID[pc.Operation]())
			}
			if state == pc.Deleting {
				item.Description = nil
			}
			raw, e := encodeList(context.Background(), pc.ListOwnedProjectsRequest{}, f.PageRequest{Limit: 50}, page)
			if e != nil {
				t.Fatal(e)
			}
			root := wireObject(t, raw)
			o := root["items"].([]any)[0].(map[string]any)
			count := 5
			if state == pc.Archiving {
				count = 6
			}
			if len(root) != 2 || root["next_cursor"] != nil || len(o) != count || o["version"] != "9007199254740993" {
				t.Fatal("conditional fields/precision", o)
			}
			if _, ok := o["description"]; ok == (state == pc.Deleting) {
				t.Fatal("deleting description")
			}
		})
	}
	for _, damage := range []string{"description", "operation", "duplicate", "filter", "page-size", "cursor", "active-operation"} {
		t.Run(damage, func(t *testing.T) {
			page := wirePage()
			request := pc.ListOwnedProjectsRequest{}
			limit := 50
			switch damage {
			case "description":
				page.Items[0].Description = wirePtr("\x00")
			case "operation":
				page.Items[0].Lifecycle = pc.Archiving
			case "duplicate":
				page.Items = append(page.Items, page.Items[0])
			case "filter":
				request.Lifecycle = []pc.Lifecycle{pc.Archived}
			case "page-size":
				limit = 0
			case "cursor":
				page.NextCursor = "next"
			case "active-operation":
				page.Items[0].OperationID = wirePtr(wireID[pc.Operation]())
			}
			if raw, e := encodeList(context.Background(), request, f.PageRequest{Limit: limit}, page); e == nil || raw != nil {
				t.Fatal("bad page published")
			}
		})
	}
	p := wireProject()
	p.Version = f.Version(math.MaxInt64)
	raw, e := encodeProject(context.Background(), wireActor(), p.ID, p)
	if e != nil {
		t.Fatal(e)
	}
	o := wireObject(t, raw)
	if len(o) != 11 || o["version"] != "9223372036854775807" || o["current_sprint_id"] != nil || o["archived_at"] != nil {
		t.Fatal("detail wire")
	}
	for _, damage := range []string{"owner", "id", "normalized", "description", "time", "archived", "deleting", "version", "sprint"} {
		t.Run(damage, func(t *testing.T) {
			p := wireProject()
			switch damage {
			case "owner":
				p.OwnerUserID = id.UserID{}
			case "id":
				p.ID = pc.ProjectID{}
			case "normalized":
				p.NormalizedName = "wrong"
			case "description":
				p.Description = "\x00"
			case "time":
				p.UpdatedAt = f.Instant{}
			case "archived":
				p.Lifecycle = pc.Archived
			case "deleting":
				p.Lifecycle = pc.Deleting
			case "version":
				p.Version = 0
			case "sprint":
				p.CurrentSprintID = wirePtr(pc.SprintID{})
			}
			if raw, e := encodeProject(context.Background(), wireActor(), wireID[id.Project](), p); e == nil || raw != nil {
				t.Fatal("bad detail published")
			}
		})
	}
}
func TestProjectOwnerReadWireMaximumLegalRepresentation(t *testing.T) {
	page := wireMaximumPage()
	raw, e := encodeList(context.Background(), pc.ListOwnedProjectsRequest{}, f.PageRequest{Limit: 100}, page)
	if e != nil {
		t.Fatal(e)
	}
	if len(raw) > maxRepresentationBytes || len(raw) < 4915200 || bytes.Count(raw, []byte(`\u0026`)) != 100*8192 {
		t.Fatal("legal maximum was truncated or escaped differently", len(raw))
	}
	var out struct {
		Items      []projectListDTO `json:"items"`
		NextCursor *string          `json:"next_cursor"`
	}
	if json.Unmarshal(raw, &out) != nil || len(out.Items) != 100 || out.NextCursor == nil || len(*out.NextCursor) != 8192 {
		t.Fatal("maximum result incomplete")
	}
	for _, item := range out.Items {
		if item.Description == nil || *item.Description != strings.Repeat("&", 8192) || item.Version != f.Version(math.MaxInt64) || len(item.Name) != 64 {
			t.Fatal("maximum item truncated")
		}
	}
	t.Logf("MAXIMUM_LEGAL_PROJECT_PAGE bytes=%d limit=%d rows=100 description_bytes_per_row=8192 escaped_ampersands=%d untruncated=true", len(raw), maxRepresentationBytes, bytes.Count(raw, []byte(`\u0026`)))
	if got, e := encodeValue(context.Background(), strings.Repeat("x", maxRepresentationBytes)); e == nil || got != nil {
		t.Fatal("representation overflow accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if got, e := encodeList(ctx, pc.ListOwnedProjectsRequest{}, f.PageRequest{Limit: 100}, page); e == nil || got != nil {
		t.Fatal("cancelled encoding published")
	}
}

func TestProjectOwnerReadWireStandardSchema(t *testing.T) {
	python := os.Getenv("AGENTEAM_PROJECT_READ_SCHEMA_PYTHON")
	if python == "" {
		t.Fatal("fixed standard schema interpreter required")
	}
	type schemaCase struct {
		Name  string `json:"name"`
		Value any    `json:"value"`
		Valid bool   `json:"valid"`
	}
	cases := []schemaCase{}
	raw, e := encodeProject(context.Background(), wireActor(), wireID[id.Project](), wireProject())
	if e != nil {
		t.Fatal(e)
	}
	project := wireObject(t, raw)
	cases = append(cases, schemaCase{"Project", project, true})
	for _, version := range []string{"1", "9007199254740993", "9223372036854775807", "0", "01", "9223372036854775808", "1\n"} {
		v := wireObject(t, raw)
		v["version"] = version
		cases = append(cases, schemaCase{"Project", v, version == "1" || version == "9007199254740993" || version == "9223372036854775807"})
	}
	for _, name := range []string{"A", strings.Repeat("A", 64), "A\n", ".", "..", "界", strings.Repeat("A", 65)} {
		cases = append(cases, schemaCase{"ProjectName", name, name == "A" || name == strings.Repeat("A", 64)})
	}
	for _, state := range []pc.Lifecycle{pc.Active, pc.Archiving, pc.Archived, pc.Deleting} {
		p := wirePage()
		p.Items[0].Lifecycle = state
		if state == pc.Archiving || state == pc.Deleting {
			p.Items[0].OperationID = wirePtr(wireID[pc.Operation]())
		}
		if state == pc.Deleting {
			p.Items[0].Description = nil
		}
		raw, e := encodeList(context.Background(), pc.ListOwnedProjectsRequest{}, f.PageRequest{Limit: 50}, p)
		if e != nil {
			t.Fatal(e)
		}
		v := wireObject(t, raw)
		cases = append(cases, schemaCase{"ProjectPage", v, true})
		bad := wireObject(t, raw)
		bad["unknown"] = true
		cases = append(cases, schemaCase{"ProjectPage", bad, false})
		item := wireObject(t, raw)["items"].([]any)[0].(map[string]any)
		if state == pc.Deleting {
			item["description"] = "private"
		} else {
			delete(item, "description")
		}
		cases = append(cases, schemaCase{"ProjectListItem", item, false})
	}
	data, e := json.Marshal(cases)
	if e != nil {
		t.Fatal(e)
	}
	root, e := filepath.Abs("../../../..")
	if e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, python, "-c", `import json,sys
from pathlib import Path
from jsonschema import Draft202012Validator
from referencing import Registry,Resource
from referencing.jsonschema import DRAFT202012
base='https://project-read-schema.invalid/'
p=Path(sys.argv[1])/'api/openapi'
registry=Registry()
for name in ['project-owner.json','common.json']:
    document=json.loads((p/name).read_text())
    registry=registry.with_resource(base+name,Resource.from_contents(document,default_specification=DRAFT202012))
cases=json.load(sys.stdin)
for i,case in enumerate(cases):
    schema={'$ref':base+'project-owner.json#/components/schemas/'+case['name']}
    valid=not list(Draft202012Validator(schema,registry=registry).iter_errors(case['value']))
    if valid != case['valid']:
        print('SCHEMA_CASE_FAILED',i,case['name']);sys.exit(1)
print('STANDARD_SCHEMA_PASS',len(cases))
`, root)
	cmd.Stdin = bytes.NewReader(data)
	result, e := cmd.CombinedOutput()
	if e != nil {
		t.Fatalf("standard schema: %v %s", e, result)
	}
	t.Log(strings.TrimSpace(string(result)))
}
