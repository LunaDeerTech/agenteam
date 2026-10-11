package agenthttp

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

	c "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

func TestAgentDirectoryHTTPStandardSchema(t *testing.T) {
	python := os.Getenv("AGENTEAM_AGENT_DIRECTORY_SCHEMA_PYTHON")
	if python == "" {
		t.Fatal("fixed local standard schema interpreter required")
	}
	type vector struct {
		Name, Schema string
		Body         json.RawMessage
		Valid        bool
	}
	vectors := []vector{}
	add := func(name, schema string, value any) {
		t.Helper()
		raw, err := encodeDirectory(context.Background(), value, 13<<20)
		if err != nil {
			t.Fatal(err)
		}
		vectors = append(vectors, vector{name, schema, raw, true})
	}
	v := directorySample()
	v.Description = strings.Repeat("<", c.MaxAgentDescriptionBytes)
	display := strings.Repeat("名", 256)
	v.DisplayName = &display
	add("safe maximum", "AgentDirectoryEntry", v)
	page := f.Page[c.DirectoryEntry]{Items: []c.DirectoryEntry{}}
	for n := range f.MaxPageLimit {
		copy := v.Clone()
		copy.ID = directoryID[i.Agent](1000 + f.MaxPageLimit - n)
		page.Items = append(page.Items, copy)
	}
	page.NextCursor = strings.Repeat("x", 8192)
	if validateDirectoryPage(page, v.ProjectID, f.PageRequest{Limit: 200}) != nil {
		t.Fatal("valid maximum page rejected")
	}
	add("bounded full page", "AgentDirectoryPage", page)
	add("empty page", "AgentDirectoryPage", f.Page[c.DirectoryEntry]{})
	original, _ := json.Marshal(directorySample())
	for _, bad := range []string{
		strings.Replace(string(original), `"version":"2"`, `"version":2`, 1),
		strings.Replace(string(original), `"version":"2"`, `"version":"0"`, 1),
		strings.Replace(string(original), `"name":`, `"instructions":"private","name":`, 1),
		strings.Replace(string(original), `"name":`, `"busy":false,"name":`, 1),
		strings.Replace(string(original), `"display_name":null,`, "", 1),
	} {
		vectors = append(vectors, vector{"closed projection", "AgentDirectoryEntry", []byte(bad), false})
	}
	vectors = append(vectors, vector{"null list", "AgentDirectoryPage", []byte(`{"items":null}`), false})
	root, err := filepath.Abs(filepath.Join("..", "..", "..", "..", "api", "openapi"))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(vectors)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, python, "-c", directorySchemaProgram, root)
	cmd.Stdin = bytes.NewReader(raw)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("standard schema: %v %s", err, output)
	}
}

const directorySchemaProgram = `
import sys,json,pathlib
from jsonschema import Draft202012Validator,FormatChecker
from referencing import Registry,Resource
from referencing.jsonschema import DRAFT202012
root=pathlib.Path(sys.argv[1]);doc=json.loads((root/'agent-directory.json').read_bytes());common=json.loads((root/'common.json').read_bytes())
base=(root/'agent-directory.json').as_uri();registry=Registry().with_resource(base,Resource.from_contents(doc,default_specification=DRAFT202012)).with_resource((root/'common.json').as_uri(),Resource.from_contents(common,default_specification=DRAFT202012))
for schema in doc['components']['schemas'].values():Draft202012Validator.check_schema(schema)
for v in json.load(sys.stdin):
 valid=Draft202012Validator({'$ref':base+'#/components/schemas/'+v['Schema']},registry=registry,format_checker=FormatChecker()).is_valid(v['Body'])
 if valid!=v['Valid']:raise SystemExit('directory schema vector disagrees: '+v['Name'])
`
