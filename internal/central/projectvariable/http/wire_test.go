package projectvariablehttp

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

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/projectvariable/contract"
)

type schemaVector struct {
	Name, Schema string
	Body         json.RawMessage
	Valid        bool
}

func TestVariableHTTPFormalSchemaAndMaximumRepresentations(t *testing.T) {
	vectors := []schemaVector{}
	add := func(name, schema string, value any, limit int) {
		t.Helper()
		raw, e := encodeValue(context.Background(), value, limit)
		if e != nil {
			t.Fatal(name, e)
		}
		vectors = append(vectors, schemaVector{name, schema, raw, true})
	}
	v := testVariable()
	fields := v.Fields()
	fields.Value = strings.Repeat("\x01", c.MaxValueBytes)
	fields.Description = strings.Repeat("\t", c.MaxDescriptionBytes)
	maximum, e := c.NewVariable(fields)
	if e != nil {
		t.Fatal(e)
	}
	add("maximum-value", "Variable", maximum, bodyLimit)
	page := f.Page[c.VariableSummary]{Items: []c.VariableSummary{}}
	// The summary intentionally never reads or serializes value.
	for n := range c.MaxPageLimit {
		fields := maximum.Summary().Fields()
		fields.Name = "NAME_" + strings.Repeat("x", 120)
		fields.ID = testID[id.ProjectVariable](n + 1000)
		item, e := c.NewVariableSummary(fields)
		if e != nil {
			t.Fatal(e)
		}
		page.Items = append(page.Items, item)
	}
	page.NextCursor = strings.Repeat("x", maxCursorBytes)
	add("maximum-summary-page", "VariableSummaryPage", page, listLimit)
	for _, sample := range samples(t) {
		add(string(sample.name), "VariableMutation", sample.receipt, bodyLimit)
		lookup, _ := c.NewVariableCommandLookup(c.LookupCommitted, &sample.receipt)
		add(string(sample.name)+"/lookup", "VariableCommandLookup", lookup, bodyLimit)
		var original any
		if json.Unmarshal([]byte(sample.body), &original) != nil {
			t.Fatal("fixture")
		}
		schema := map[c.CommandName]string{c.CreateCommand: "VariableCreateBody", c.UpdateCommand: "VariableUpdateBody", c.DeleteCommand: "VariableDeleteBody"}[sample.name]
		add(string(sample.name)+"/request", schema, original, bodyLimit)
		var query any
		if json.Unmarshal([]byte(lookupBody(t, sample)), &query) != nil {
			t.Fatal("fixture")
		}
		add(string(sample.name)+"/query", "VariableLookupBody", query, bodyLimit)
	}
	noop, _ := c.NewVariableMutation(c.VariableMutationFields{Command: c.UpdateCommand, Variable: v})
	add("noop", "VariableMutation", noop, bodyLimit)
	for _, status := range []c.LookupStatus{c.LookupNotObserved, c.LookupInProgress} {
		lookup, _ := c.NewVariableCommandLookup(status, nil)
		add(string(status), "VariableCommandLookup", lookup, bodyLimit)
	}
	for _, bad := range []struct{ schema, raw string }{
		{"VariableUpdate", `{}`}, {"VariableUpdate", `{"value":null}`}, {"VariableCreateBody", `{"request":{},"expected_version":"1"}`}, {"VariableDeleteBody", `{"expected_version":"1","request":{}}`}, {"VariableName", `"agenteam_PRIVATE"`}, {"VariableName", `"AGENTEAM"`}, {"VariableValue", `"\u0000"`}, {"VariableDescription", `"\u0001"`}, {"VariableCommandLookup", `{"status":"committed","receipt":null}`},
	} {
		vectors = append(vectors, schemaVector{"negative", bad.schema, []byte(bad.raw), false})
	}
	schemaCheck(t, vectors)
}
func schemaCheck(t *testing.T, vectors []schemaVector) {
	t.Helper()
	python := os.Getenv("AGENTEAM_PROJECT_VARIABLE_SCHEMA_PYTHON")
	if python == "" {
		t.Fatal("fixed local standard schema interpreter required")
	}
	root, e := filepath.Abs(filepath.Join("..", "..", "..", "..", "api", "openapi"))
	if e != nil {
		t.Fatal(e)
	}
	raw, _ := json.Marshal(vectors)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, python, "-c", schemaProgram, root)
	cmd.Stdin = bytes.NewReader(raw)
	output, e := cmd.CombinedOutput()
	if e != nil {
		t.Fatalf("schema: %v %s", e, output)
	}
	t.Log(strings.TrimSpace(string(output)))
}

const schemaProgram = `
import sys,json,pathlib
from jsonschema import Draft202012Validator,FormatChecker
from referencing import Registry,Resource
from referencing.jsonschema import DRAFT202012
root=pathlib.Path(sys.argv[1]); doc=json.loads((root/'project-variables.json').read_bytes()); common=json.loads((root/'common.json').read_bytes())
base=(root/'project-variables.json').as_uri(); registry=Registry().with_resource(base,Resource.from_contents(doc,default_specification=DRAFT202012)).with_resource((root/'common.json').as_uri(),Resource.from_contents(common,default_specification=DRAFT202012))
for schema in doc['components']['schemas'].values():Draft202012Validator.check_schema(schema)
vectors=json.load(sys.stdin)
for v in vectors:
 valid=Draft202012Validator({'$ref':base+'#/components/schemas/'+v['Schema']},registry=registry,format_checker=FormatChecker()).is_valid(v['Body'])
 if valid!=v['Valid']:raise SystemExit('vector disagrees: '+v['Name']+'/'+v['Schema'])
print('standard Draft2020-12 accepted '+str(len(vectors))+' actual wire vectors')
`
