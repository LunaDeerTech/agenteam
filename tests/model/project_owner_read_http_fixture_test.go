//go:build integration

package model_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/account"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/httpapi"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/project"
	projecthttp "github.com/LunaDeerTech/agenteam/internal/central/project/http"
)

func newProjectOwnerReadFixture(t *testing.T) *projectUsageHTTPFixture {
	t.Helper()
	// This accepted helper uses Bootstrap/Invitation/Redeem/Login for every
	// identity and Project.Create plus a persisted, explicitly test-only Skill.
	// Its unused Runtime/Resolution test tables are not production dependencies.
	v := newProjectUsageHTTPFixture(t)
	_, keys, _ := testKeys(t)
	reader, e := project.NewReader(v.tracked, v.projects, keys)
	if e != nil {
		t.Fatal(e)
	}
	installProjectOwnerRead(t, v, reader)
	return v
}
func installProjectOwnerRead(t *testing.T, v *projectUsageHTTPFixture, reader *project.Reader) {
	t.Helper()
	boundary, e := account.NewHTTPBoundary(v.core, systemHTTPOrigin)
	if e != nil {
		t.Fatal(e)
	}
	h, e := projecthttp.NewHTTPHandler(reader, boundary)
	if e != nil {
		t.Fatal(e)
	}
	old := v.handler
	reads := httpapi.Handler(slog.New(slog.NewJSONHandler(v.logs, nil)), h)
	v.handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if projecthttp.HandlesPath(r.URL.Path) {
			reads.ServeHTTP(w, r)
		} else {
			old.ServeHTTP(w, r)
		}
	})
}
func projectOwnerReadPath(project id.ProjectID) string { return "/api/v1/projects/" + project.String() }
func projectOwnerReadCause(c f.TransactionCause) bool {
	return c.Kind() == f.RecoveryCause && (c.Details().Owner == "project.get" || c.Details().Owner == "project.list")
}
func projectOwnerReadTop(t *testing.T) {
	t.Helper()
	started := time.Now()
	// Register first so this observes all subsequently registered fixture cleanup.
	t.Cleanup(func() {
		if time.Since(started) > 2*time.Minute {
			t.Error("Project Owner read top including Cleanup exceeded 120s")
		}
	})
}
func projectOwnerReadSchema(t *testing.T, name string, body []byte) {
	t.Helper()
	python := os.Getenv("AGENTEAM_PROJECT_READ_SCHEMA_PYTHON")
	if python == "" {
		t.Fatal("fixed schema interpreter required")
	}
	root, e := filepath.Abs("../..")
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
base='https://project-owner-real.invalid/'
registry=Registry()
for name in ['project-owner.json','common.json']:
    doc=json.loads((Path(sys.argv[1])/'api/openapi'/name).read_text())
    registry=registry.with_resource(base+name,Resource.from_contents(doc,default_specification=DRAFT202012))
value=json.load(sys.stdin)
schema={'$ref':base+'project-owner.json#/components/schemas/'+sys.argv[2]}
if list(Draft202012Validator(schema,registry=registry).iter_errors(value)):
    print('REAL_BODY_SCHEMA_FAILED');sys.exit(1)
print('REAL_BODY_SCHEMA_PASS')
`, root, name)
	cmd.Stdin = bytes.NewReader(body)
	raw, e := cmd.CombinedOutput()
	if e != nil {
		t.Fatalf("actual response schema: %v %s", e, raw)
	}
}
func projectOwnerReadExport(t *testing.T, name, path string, response systemHTTPResponse) {
	t.Helper()
	dir := os.Getenv("AGENTEAM_PROJECT_READ_BODY_DIR")
	if dir == "" {
		return
	}
	if !filepath.IsAbs(dir) {
		t.Fatal("body export directory must be absolute")
	}
	if e := os.MkdirAll(dir, 0700); e != nil {
		t.Fatal(e)
	}
	// Only the synthetic Project fixture's public read body is exported. Cookies,
	// recovery material and descriptor/URL credentials never enter this record.
	bodyHash := sha256.Sum256(response.body)
	schema, e := os.ReadFile("../../api/openapi/project-owner.json")
	if e != nil {
		t.Fatal(e)
	}
	schemaHash := sha256.Sum256(schema)
	metadata := map[string]any{"method": "GET", "path": path, "status": response.status, "content_type": response.headers.Get("Content-Type"), "content_length": response.headers.Get("Content-Length"), "body_sha256": hex.EncodeToString(bodyHash[:]), "schema_sha256": hex.EncodeToString(schemaHash[:]), "run": os.Getenv("AGENTEAM_PROJECT_READ_RUN"), "candidate": os.Getenv("AGENTEAM_PROJECT_READ_CANDIDATE")}
	raw, e := json.MarshalIndent(metadata, "", "  ")
	if e != nil {
		t.Fatal(e)
	}
	for file, data := range map[string][]byte{name + ".json": response.body, name + "-source.json": raw} {
		if e := os.WriteFile(filepath.Join(dir, file), data, 0600); e != nil {
			t.Fatal(e)
		}
	}
}
