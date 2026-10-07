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
	"net/http/httptest"
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
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
)

// All identities use accepted Bootstrap/Invitation/Redeem/Login. Model keeps its
// original prepared-checker Audit authority; no Secret AuditFacts is added for
// Model. The inherited fixture isolates only the unbound Skills initializer.
type configurationWriteFixture struct{ *projectCredentialFixture }

func newConfigurationWriteFixture(t *testing.T) *configurationWriteFixture {
	t.Helper()
	v := &configurationWriteFixture{newProjectCredentialFixture(t)}
	v.installConfiguration(t, v.service)
	return v
}
func (v *configurationWriteFixture) installConfiguration(t *testing.T, s *model.Service) {
	t.Helper()
	boundary, e := account.NewHTTPBoundary(v.core, systemHTTPOrigin)
	if e != nil {
		t.Fatal(e)
	}
	read, e := model.NewProjectHTTPHandler(s, boundary)
	if e != nil {
		t.Fatal(e)
	}
	write, e := model.NewProjectConfigurationHTTPHandler(s, boundary)
	if e != nil {
		t.Fatal(e)
	}
	reads := httpapi.Handler(slog.New(slog.NewJSONHandler(v.logs, nil)), read)
	writes := httpapi.Handler(slog.New(slog.NewJSONHandler(v.logs, nil)), write)
	old := v.handler
	v.handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if model.HandlesProjectConfigurationHTTPPath(r.URL.Path) && ((r.Method != "GET" && r.Method != "HEAD") || !model.HandlesProjectHTTPPath(r.URL.Path)) {
			writes.ServeHTTP(w, r)
			return
		}
		if model.HandlesProjectHTTPPath(r.URL.Path) {
			reads.ServeHTTP(w, r)
			return
		}
		old.ServeHTTP(w, r)
	})
}
func (v *configurationWriteFixture) base() string { return projectOwnerReadPath(v.project.ID) + "/" }
func (v *configurationWriteFixture) command(t *testing.T, b systemHTTPBrowser, method, path, key string, body []byte) systemHTTPResponse {
	t.Helper()
	r := projectUpdateRequest(testContext(t), b, method, path, key, body)
	w := &projectUsageRecorder{ResponseRecorder: httptest.NewRecorder()}
	v.handler.ServeHTTP(w, r)
	return systemHTTPResponse{w.Code, w.Header().Clone(), bytes.Clone(w.Body.Bytes())}
}
func configurationProviderBody(ref any) map[string]any {
	return map[string]any{"name": "HTTP Project provider", "protocol": "openai-chat-completions", "base_url": "https://configuration.example/v1", "enabled": true, "credential_ref": ref, "options": map[string]any{}}
}
func configurationModelBody() map[string]any {
	return map[string]any{"name": "HTTP Project chat", "provider_model_id": "configuration-chat", "type": "chat", "enabled": true, "parameters": map[string]any{}, "request_overwrite": map[string]any{}, "header_overwrite": map[string]any{}, "capabilities": map[string]any{"tool_calls": false, "parallel_tool_calls": false, "streaming": false, "reasoning": false, "input_modalities": []string{"text"}, "output_modalities": []string{"text"}, "reasoning_efforts": []string{}, "structured_output_modes": []string{}, "context_length": nil, "max_output": nil}}
}
func configurationWriteReceipt(t *testing.T, r systemHTTPResponse, kind string) mc.CommandReceipt {
	t.Helper()
	r.want(t, 200)
	var out mc.CommandReceipt
	var fields map[string]json.RawMessage
	if json.Unmarshal(r.body, &out) != nil || json.Unmarshal(r.body, &fields) != nil || len(fields) != 4 || out.Validate() != nil || out.Kind != kind || out.AffectedReferences != 0 || len(r.body) > 1024 {
		t.Fatal("invalid safe configuration receipt")
	}
	return out
}
func configurationLookupReceipt(t *testing.T, r systemHTTPResponse, want *mc.CommandReceipt) {
	t.Helper()
	r.want(t, 200)
	var out struct {
		Found   bool               `json:"found"`
		Receipt *mc.CommandReceipt `json:"receipt"`
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(r.body, &out) != nil || json.Unmarshal(r.body, &fields) != nil || len(fields) != 2 || out.Found != (want != nil) || len(r.body) > 1024 {
		t.Fatal("invalid explicit observation")
	}
	if want == nil {
		if out.Receipt != nil {
			t.Fatal("false with receipt")
		}
	} else if out.Receipt == nil || *out.Receipt != *want {
		t.Fatal("historical receipt changed")
	}
}
func configurationCommandIdentity(t *testing.T, b systemHTTPBrowser, p id.ProjectID, kind, key string) f.CommandIdentity {
	t.Helper()
	c, e := f.NewCommandIdentity("model.project", []string{p.String(), b.actor.Details().UserID}, kind, f.IdempotencyKey(key))
	if e != nil {
		t.Fatal(e)
	}
	return c
}
func (v *configurationWriteFixture) snapshot(t *testing.T) []byte {
	t.Helper()
	var out []byte
	// Private exact-state snapshot: never exported/logged or hashed as material.
	// Reference metadata is included; encrypted material is deliberately absent.
	e := v.raw.QueryRow(testContext(t), `SELECT jsonb_build_object(
 'providers',COALESCE((SELECT jsonb_agg(to_jsonb(p) ORDER BY id) FROM agenteam_model.providers p WHERE project_id=$1),'[]'::jsonb),
 'models',COALESCE((SELECT jsonb_agg(to_jsonb(m) ORDER BY m.id) FROM agenteam_model.models m JOIN agenteam_model.providers p ON p.id=m.provider_id WHERE p.scope='project' AND p.project_id=$1),'[]'::jsonb),
 'commands',COALESCE((SELECT jsonb_agg(to_jsonb(c) ORDER BY command_identity) FROM agenteam_model.commands c WHERE project_id=$1),'[]'::jsonb),
 'audits',COALESCE((SELECT jsonb_agg(to_jsonb(a) ORDER BY id) FROM agenteam_audit.audit_records a WHERE project_id=$1 AND producer='model'),'[]'::jsonb),
 'events',COALESCE((SELECT jsonb_agg(to_jsonb(e) ORDER BY id) FROM agenteam_outbox.events e WHERE project_id=$1 AND producer='model'),'[]'::jsonb),
 'refs',COALESCE((SELECT jsonb_agg(to_jsonb(r) ORDER BY credential_id,consumer,owner_id) FROM agenteam_secret.secret_references r),'[]'::jsonb))::text`, v.project.ID.String()).Scan(&out)
	if e != nil {
		t.Fatal("private full state snapshot", e)
	}
	return out
}
func (v *configurationWriteFixture) sameSnapshot(t *testing.T, want []byte) {
	t.Helper()
	if !bytes.Equal(want, v.snapshot(t)) {
		t.Fatal("canonical/view/ref/receipt/Audit/Event changed on rejected command")
	}
}
func configurationWriteTop(t *testing.T) {
	t.Helper()
	started := time.Now()
	t.Cleanup(func() {
		if time.Since(started) > 120*time.Second {
			t.Error("configuration write top including cleanup exceeded 120s")
		}
	})
}
func configurationWriteSchema(t *testing.T, name string, body []byte) {
	t.Helper()
	python := os.Getenv("AGENTEAM_PROJECT_MODEL_CONFIGURATION_SCHEMA_PYTHON")
	if !filepath.IsAbs(python) {
		t.Fatal("absolute fixed schema interpreter required")
	}
	root, e := filepath.Abs("../..")
	if e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, python, "-c", `import json,pathlib,sys
from jsonschema import Draft202012Validator,FormatChecker
from referencing import Registry,Resource
from referencing.jsonschema import DRAFT202012
base=pathlib.Path(sys.argv[1])/'api/openapi';registry=Registry()
for file in ['project-models.json','common.json']:
 p=base/file;registry=registry.with_resource(p.as_uri(),Resource.from_contents(json.loads(p.read_text()),default_specification=DRAFT202012))
file='common.json' if sys.argv[2]=='Problem' else 'project-models.json'
v=Draft202012Validator({'$ref':(base/file).as_uri()+'#/components/schemas/'+sys.argv[2]},registry=registry,format_checker=FormatChecker())
assert not list(v.iter_errors(json.load(sys.stdin)))
print('ACTUAL_CONFIGURATION_BODY_SCHEMA_PASS')`, root, name)
	cmd.Stdin = bytes.NewReader(body)
	raw, e := cmd.CombinedOutput()
	if e != nil {
		t.Fatal("actual safe body schema", e, string(raw))
	}
}
func configurationWriteExport(t *testing.T, name, method, path string, r systemHTTPResponse) {
	t.Helper()
	dir := os.Getenv("AGENTEAM_PROJECT_MODEL_CONFIGURATION_BODY_DIR")
	if dir == "" {
		return
	}
	run, input := os.Getenv("AGENTEAM_PROJECT_MODEL_CONFIGURATION_RUN_ID"), os.Getenv("AGENTEAM_PROJECT_MODEL_CONFIGURATION_INPUT_ID")
	if !filepath.IsAbs(dir) || run == "" || input == "" || r.headers.Get("X-Request-ID") == "" {
		t.Fatal("safe export missing actual provenance")
	}
	if filepath.Base(name) != name {
		t.Fatal("unsafe export label")
	}
	if e := os.MkdirAll(dir, 0700); e != nil {
		t.Fatal(e)
	}
	digest := func(data []byte) string { sum := sha256.Sum256(data); return hex.EncodeToString(sum[:]) }
	schema, e := os.ReadFile("../../api/openapi/project-models.json")
	if e != nil {
		t.Fatal(e)
	}
	meta := map[string]any{"method": method, "path": path, "target": path, "status": r.status, "content_type": r.headers.Get("Content-Type"), "content_length": r.headers.Get("Content-Length"), "response_headers": map[string]string{"X-Request-ID": r.headers.Get("X-Request-ID")}, "body_sha256": digest(r.body), "schema_sha256": digest(schema), "run": run, "input_sha256": input}
	side, e := json.MarshalIndent(meta, "", "  ")
	if e != nil {
		t.Fatal(e)
	}
	for suffix, data := range map[string][]byte{".json": r.body, "-source.json": side} {
		p := filepath.Join(dir, name+suffix)
		file, e := os.OpenFile(p, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if e != nil {
			t.Fatal(e)
		}
		_, we := file.Write(data)
		ce := file.Close()
		if we != nil || ce != nil {
			t.Fatal("safe original export write")
		}
	}
}
