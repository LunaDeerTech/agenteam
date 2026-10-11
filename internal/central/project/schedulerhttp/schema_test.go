package schedulerhttp

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	js "github.com/santhosh-tekuri/jsonschema/v6"
)

type noSchemaNetwork struct{}

func (noSchemaNetwork) Load(string) (any, error) {
	return nil, errors.New("only checked local OpenAPI resources are allowed")
}

func TestSchedulerOpenAPIActualWireAndFullInt64(t *testing.T) {
	compiler := js.NewCompiler()
	compiler.DefaultDraft(js.Draft2020)
	compiler.UseLoader(noSchemaNetwork{})
	const base = "https://schema.invalid/"
	var document map[string]any
	for _, name := range []string{"project-scheduler.json", "common.json", "project-owner.json"} {
		raw, err := os.ReadFile(filepath.Join("../../../..", "api/openapi", name))
		if err != nil {
			t.Fatal(err)
		}
		var value any
		if err = json.Unmarshal(raw, &value); err != nil {
			t.Fatal(err)
		}
		if err = compiler.AddResource(base+name, value); err != nil {
			t.Fatal(err)
		}
		if name == "project-scheduler.json" {
			document = value.(map[string]any)
		}
	}
	paths := document["paths"].(map[string]any)
	lookup := paths["/api/v1/projects/{id}/commands/lookup"].(map[string]any)
	if lookup["$ref"] != "./project-owner.json#/paths/~1api~1v1~1projects~1{id}~1commands~1lookup" {
		t.Fatal("old Lookup contract changed")
	}
	compiled := map[string]*js.Schema{}
	for _, name := range []string{"SchedulerConfiguration", "SchedulerUpdateInput", "SchedulerUpdateReceipt"} {
		s, err := compiler.Compile(base + "project-scheduler.json#/components/schemas/" + name)
		if err != nil {
			t.Fatal(name, err)
		}
		compiled[name] = s
	}
	validate := func(name string, raw []byte, want bool) {
		t.Helper()
		var value any
		if err := json.Unmarshal(raw, &value); err != nil {
			t.Fatal(err)
		}
		if got := compiled[name].Validate(value) == nil; got != want {
			t.Fatalf("%s valid=%v want=%v body=%s", name, got, want, raw)
		}
	}
	for _, limit := range []any{nil, "1", "9007199254740993", "9223372036854775807"} {
		input := map[string]any{"expected_version": "1", "scheduler_enabled": false, "scheduler_max_concurrency": limit}
		raw, _ := json.Marshal(input)
		validate("SchedulerUpdateInput", raw, true)
		h, s, _ := fixture()
		s.get = nil
		w := writer()
		if p := serve(h, w, testRequest("PATCH", testPath, string(raw))); p != nil || w.Code != 200 {
			t.Fatal("actual handler output", p, w.Code, w.Body.String())
		}
		validate("SchedulerUpdateReceipt", w.Body.Bytes(), true)
		w = writer()
		if p := serve(h, w, testRequest("GET", testPath, "")); p != nil || w.Code != 200 {
			t.Fatal(p, w.Code)
		}
		validate("SchedulerConfiguration", w.Body.Bytes(), true)
	}
	for _, limit := range []any{0, 1, "0", "01", "-1", "9223372036854775808", true} {
		raw, _ := json.Marshal(map[string]any{"expected_version": "1", "scheduler_enabled": false, "scheduler_max_concurrency": limit})
		validate("SchedulerUpdateInput", raw, false)
	}
	for _, raw := range []string{`{"expected_version":"1","scheduler_enabled":false}`, `{"expected_version":"1","scheduler_enabled":null,"scheduler_max_concurrency":null}`, `{"expected_version":1,"scheduler_enabled":false,"scheduler_max_concurrency":null}`, `{"expected_version":"1","scheduler_enabled":false,"scheduler_max_concurrency":null,"name":"mixed"}`} {
		validate("SchedulerUpdateInput", []byte(raw), false)
	}
	validate("SchedulerUpdateReceipt", []byte(`{"project_id":"01900000-0000-7000-8000-000000000001","version":"2","scheduler_enabled":true}`), false)
}
