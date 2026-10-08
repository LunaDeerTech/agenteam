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
	"sync"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/account"
	"github.com/LunaDeerTech/agenteam/internal/central/audit"
	audithttp "github.com/LunaDeerTech/agenteam/internal/central/audit/http"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/httpapi"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

// This reuses accepted real Account Bootstrap/Invitation/Redeem/Login and
// Project initialization with an explicitly test-only persisted Skills adapter.
// Account, Project, Secret, Model and Audit keep the same real transaction Store.
type projectAuditFixture struct{ *configurationWriteFixture }

func newProjectAuditFixture(t *testing.T) *projectAuditFixture {
	t.Helper()
	v := &projectAuditFixture{newConfigurationWriteFixture(t)}
	v.installAudit(t, v.aud)
	return v
}
func (v *projectAuditFixture) installAudit(t *testing.T, service *audit.Service) {
	t.Helper()
	boundary, e := account.NewHTTPBoundary(v.core, systemHTTPOrigin)
	if e != nil {
		t.Fatal(e)
	}
	h, e := audithttp.NewProjectHTTPHandler(service, boundary)
	if e != nil {
		t.Fatal(e)
	}
	old := v.handler
	tracked := httpapi.Handler(slog.New(slog.NewJSONHandler(v.logs, nil)), h)
	v.handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if audithttp.HandlesProjectHTTPPath(r.URL.Path) {
			tracked.ServeHTTP(w, r)
		} else {
			old.ServeHTTP(w, r)
		}
	})
}
func projectAuditPath(p id.ProjectID) string { return projectOwnerReadPath(p) + "/audit" }
func (v *projectAuditFixture) read(t *testing.T, b systemHTTPBrowser, method, path string) systemHTTPResponse {
	t.Helper()
	return v.projectUsageHTTPFixture.request(t, b, method, path)
}
func projectAuditReadCause(c f.TransactionCause) bool {
	return c.Kind() == f.RecoveryCause && c.Details().Owner == "audit.project-read"
}

type projectAuditRecord struct {
	AuditID      string          `json:"audit_id"`
	CreatedAt    string          `json:"created_at"`
	Scope        string          `json:"scope"`
	ProjectID    string          `json:"project_id"`
	Actor        json.RawMessage `json:"actor"`
	Action       string          `json:"action"`
	Outcome      string          `json:"outcome"`
	Resource     json.RawMessage `json:"resource"`
	Metadata     json.RawMessage `json:"metadata"`
	Associations json.RawMessage `json:"associations"`
	Summary      string          `json:"summary"`
}
type projectAuditPage struct {
	Items      []projectAuditRecord `json:"items"`
	NextCursor string               `json:"next_cursor"`
}

func projectAuditDecodePage(t *testing.T, r systemHTTPResponse, p id.ProjectID) projectAuditPage {
	t.Helper()
	r.want(t, 200)
	var page projectAuditPage
	var fields map[string]json.RawMessage
	if json.Unmarshal(r.body, &page) != nil || json.Unmarshal(r.body, &fields) != nil || len(fields) != 2 || page.Items == nil {
		t.Fatal("unsafe/non-explicit Audit page")
	}
	for _, item := range page.Items {
		if item.ProjectID != p.String() || item.Scope != "project" || item.AuditID == "" || item.Summary == "" {
			t.Fatal("foreign/incomplete Audit projection")
		}
	}
	return page
}
func projectAuditSchema(t *testing.T, name string, body []byte) {
	t.Helper()
	python := os.Getenv("AGENTEAM_PROJECT_AUDIT_SCHEMA_PYTHON")
	if python == "" {
		t.Fatal("fixed Audit schema interpreter required")
	}
	root, e := filepath.Abs("../../api/openapi")
	if e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithTimeout(testContext(t), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, python, "-c", `import json,sys
from pathlib import Path
from jsonschema import Draft202012Validator,FormatChecker
from referencing import Registry,Resource
from referencing.jsonschema import DRAFT202012
base='https://audit-real.invalid/'; registry=Registry()
for name in ['project-audit.json','common.json']:
 doc=json.loads((Path(sys.argv[1])/name).read_bytes());registry=registry.with_resource(base+name,Resource.from_contents(doc,default_specification=DRAFT202012))
value=json.load(sys.stdin)
validator=Draft202012Validator({'$ref':base+'project-audit.json#/components/schemas/'+sys.argv[2]},registry=registry,format_checker=FormatChecker())
if not validator.is_valid(value):raise SystemExit('REAL_AUDIT_BODY_SCHEMA_FAILED')
print('REAL_AUDIT_BODY_SCHEMA_PASS')`, root, name)
	cmd.Stdin = bytes.NewReader(body)
	out, e := cmd.CombinedOutput()
	if e != nil {
		t.Fatalf("actual Audit schema %v %s", e, out)
	}
}
func projectAuditExport(t *testing.T, name, method, path string, p id.ProjectID, r systemHTTPResponse) {
	t.Helper()
	dir := os.Getenv("AGENTEAM_PROJECT_AUDIT_BODY_DIR")
	if dir == "" {
		return
	}
	if !filepath.IsAbs(dir) {
		t.Fatal("absolute body directory required")
	}
	if e := os.MkdirAll(dir, 0700); e != nil {
		t.Fatal(e)
	}
	schema, e := os.ReadFile("../../api/openapi/project-audit.json")
	if e != nil {
		t.Fatal(e)
	}
	h := sha256.Sum256(r.body)
	sh := sha256.Sum256(schema)
	meta := map[string]any{"method": method, "path": path, "status": r.status, "content_type": r.headers.Get("Content-Type"), "content_length": r.headers.Get("Content-Length"), "request_id": r.headers.Get("X-Request-ID"), "project_id": p.String(), "body_sha256": hex.EncodeToString(h[:]), "schema_sha256": hex.EncodeToString(sh[:]), "source_run": os.Getenv("AGENTEAM_PROJECT_AUDIT_RUN"), "producer_test": t.Name(), "candidate": os.Getenv("AGENTEAM_PROJECT_AUDIT_CANDIDATE"), "input": os.Getenv("AGENTEAM_PROJECT_AUDIT_INPUT")}
	source, e := json.MarshalIndent(meta, "", "  ")
	if e != nil {
		t.Fatal(e)
	}
	for file, data := range map[string][]byte{name + ".json": r.body, name + "-source.json": source} {
		if e := os.WriteFile(filepath.Join(dir, file), data, 0600); e != nil {
			t.Fatal(e)
		}
	}
}

// Cleanup is installed before any wait: even a failed checkpoint releases and
// actually waits for the holder's original real transaction, never a substitute.
func projectAuditHold(t *testing.T, v *projectAuditFixture, key f.LockKey) (release func(), wait func()) {
	t.Helper()
	ctx, cancel := context.WithCancel(testContext(t))
	entered := make(chan struct{})
	done := make(chan struct{})
	gate := make(chan struct{})
	var once sync.Once
	var result f.CommitResult
	release = func() { once.Do(func() { close(gate) }) }
	wait = func() {
		<-done
		if result.State() != f.Committed {
			t.Error("Audit test holder did not commit", result.Fault())
		}
	}
	t.Cleanup(func() { release(); cancel(); <-done })
	go func() {
		defer close(done)
		cause, _ := f.NewRecoveryCause("audit-http-holder", newID[struct{}](t).String(), "")
		result = v.raw.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
			if e := v.raw.AcquireAll(ctx, tx, []f.LockRequest{{Key: key, Mode: f.Exclusive}}); e != nil {
				return e
			}
			close(entered)
			select {
			case <-gate:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		})
	}()
	select {
	case <-entered:
	case <-done:
		t.Fatal("holder exited before acquisition")
	case <-time.After(5 * time.Second):
		t.Fatal("holder checkpoint timed out")
	}
	return
}
