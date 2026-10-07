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
	"github.com/LunaDeerTech/agenteam/internal/central/model"
)

// Reuse accepted Bootstrap/Invitation/Redeem/Login identities, formal Project
// Create and persisted private Skills confirmation from project_configuration.
// No SQL identity grant, producer replacement, Resolver or Provider call is
// introduced here. The old fixture's unused runtime tables are not root ports.
func newProjectConfigurationHTTP(t *testing.T) *projectUsageHTTPFixture {
	t.Helper()
	v := newProjectUsageHTTPFixture(t)
	installProjectConfigurationHTTP(t, v, v.service)
	return v
}
func installProjectConfigurationHTTP(t *testing.T, v *projectUsageHTTPFixture, core *model.Service) {
	t.Helper()
	boundary, e := account.NewHTTPBoundary(v.core, systemHTTPOrigin)
	if e != nil {
		t.Fatal(e)
	}
	h, e := model.NewProjectHTTPHandler(core, boundary)
	if e != nil {
		t.Fatal(e)
	}
	old := v.handler
	next := httpapi.Handler(slog.New(slog.NewJSONHandler(v.logs, nil)), h)
	v.handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if model.HandlesProjectHTTPPath(r.URL.Path) {
			next.ServeHTTP(w, r)
		} else {
			old.ServeHTTP(w, r)
		}
	})
}
func projectConfigurationHTTPPath(p id.ProjectID, suffix string) string {
	return "/api/v1/projects/" + p.String() + "/" + suffix
}
func projectConfigurationHTTPCause(c f.TransactionCause) bool {
	return c.Kind() == f.RecoveryCause && c.Details().Owner == "model.query"
}
func projectConfigurationHTTPTop(t *testing.T) {
	t.Helper()
	started := time.Now()
	t.Cleanup(func() {
		if time.Since(started) > 120*time.Second {
			t.Error("Project configuration HTTP top plus all Cleanup exceeded 120s")
		}
	})
}
func projectConfigurationHTTPPage(t *testing.T, r systemHTTPResponse) ([]map[string]json.RawMessage, string) {
	t.Helper()
	r.want(t, 200)
	var page struct {
		Items []map[string]json.RawMessage `json:"items"`
		Next  *string                      `json:"next_cursor"`
	}
	if e := json.Unmarshal(r.body, &page); e != nil || page.Items == nil {
		t.Fatal("invalid complete page", e)
	}
	if page.Next == nil {
		return page.Items, ""
	}
	return page.Items, *page.Next
}
func projectConfigurationHTTPID(t *testing.T, row map[string]json.RawMessage) string {
	t.Helper()
	var value string
	if json.Unmarshal(row["id"], &value) != nil {
		t.Fatal("invalid ID wire")
	}
	return value
}
func projectConfigurationHTTPSchema(t *testing.T, name string, body []byte) {
	t.Helper()
	python := os.Getenv("AGENTEAM_PROJECT_MODEL_SCHEMA_PYTHON")
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
from jsonschema import Draft202012Validator,FormatChecker
from referencing import Registry,Resource
from referencing.jsonschema import DRAFT202012
base='https://project-model-real.invalid/'
r=Registry()
for n in ['common.json','project-models.json']:
 d=json.loads((Path(sys.argv[1])/'api/openapi'/n).read_text());r=r.with_resource(base+n,Resource.from_contents(d,default_specification=DRAFT202012))
s={'$ref':base+'project-models.json#/components/schemas/'+sys.argv[2]}
assert not list(Draft202012Validator(s,registry=r,format_checker=FormatChecker()).iter_errors(json.load(sys.stdin)))
print('REAL_BODY_SCHEMA_PASS')
`, root, name)
	cmd.Stdin = bytes.NewReader(body)
	raw, e := cmd.CombinedOutput()
	if e != nil {
		t.Fatalf("actual schema %v %s", e, raw)
	}
}
func projectConfigurationHTTPExport(t *testing.T, name, method, target string, r systemHTTPResponse) {
	t.Helper()
	dir := os.Getenv("AGENTEAM_PROJECT_MODEL_BODY_DIR")
	if dir == "" {
		return
	}
	if !filepath.IsAbs(dir) {
		t.Fatal("export path must be absolute")
	}
	if r.headers.Get("X-Request-ID") == "" {
		t.Fatal("actual response RequestID missing")
	}
	if e := os.MkdirAll(dir, 0700); e != nil {
		t.Fatal(e)
	}
	digest := func(b []byte) string { x := sha256.Sum256(b); return hex.EncodeToString(x[:]) }
	schema, e := os.ReadFile("../../api/openapi/project-models.json")
	if e != nil {
		t.Fatal(e)
	}
	meta := map[string]any{"method": method, "target": target, "status": r.status, "content_type": r.headers.Get("Content-Type"), "content_length": r.headers.Get("Content-Length"), "response_headers": map[string]string{"X-Request-ID": r.headers.Get("X-Request-ID")}, "body_sha256": digest(r.body), "schema_sha256": digest(schema), "run": os.Getenv("AGENTEAM_PROJECT_MODEL_RUN"), "input_sha256": os.Getenv("AGENTEAM_PROJECT_MODEL_INPUT_SHA256")}
	raw, e := json.MarshalIndent(meta, "", "  ")
	if e != nil {
		t.Fatal(e)
	}
	for file, data := range map[string][]byte{name + ".json": r.body, name + "-source.json": raw} {
		if e = os.WriteFile(filepath.Join(dir, file), data, 0600); e != nil {
			t.Fatal(e)
		}
	}
}
