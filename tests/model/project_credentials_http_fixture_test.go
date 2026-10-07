//go:build integration

package model_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/account"
	"github.com/LunaDeerTech/agenteam/internal/central/audit"
	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/httpapi"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/model"
	"github.com/LunaDeerTech/agenteam/internal/central/project"
	"github.com/LunaDeerTech/agenteam/internal/central/secret"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

// Formal Bootstrap/Invitation/Redeem/Login and Project.Create are inherited.
// Persisted Skills/unused Runtime tables belong only to that accepted fixture;
// this does not bind production Project initialization or Invocation consumers.
type projectCredentialFixture struct {
	*projectUsageHTTPFixture
	writer *secret.Service
	tap    *projectCredentialAuditTap
}
type projectCredentialAuditTap struct {
	next            *audit.Service
	before          func(context.Context, f.Tx, ac.Entry, ac.AppendKey) (ac.Entry, ac.AppendKey)
	calls, rejected atomic.Int32
}

func (a *projectCredentialAuditTap) AppendInTx(ctx context.Context, tx f.Tx, e ac.Entry, k ac.AppendKey) (ac.AppendReceipt, error) {
	targeted := e.Fields().Scope.Details().Kind == id.ProjectScope && k.Details().Producer == ac.SecretProducer
	if targeted {
		a.calls.Add(1)
		if a.before != nil {
			e, k = a.before(ctx, tx, e, k)
		}
	}
	receipt, err := a.next.AppendInTx(ctx, tx, e, k)
	if targeted && err != nil {
		a.rejected.Add(1)
	}
	return receipt, err
}
func newProjectCredentialFixture(t *testing.T) *projectCredentialFixture {
	t.Helper()
	v := newProjectOwnerReadFixture(t)
	u := &projectCredentialFixture{projectUsageHTTPFixture: v}
	u.writer, u.tap = projectCredentialService(t, v, v.tracked)
	u.install(t, u.writer)
	return u
}
func projectCredentialService(t *testing.T, v *projectUsageHTTPFixture, store sharedStore) (*secret.Service, *projectCredentialAuditTap) {
	t.Helper()
	ak, ck, sk := testKeys(t)
	accounts, e := account.NewAuthority(store, ak)
	if e != nil {
		t.Fatal(e)
	}
	checker, e := secret.NewProjectAuditAuthority(store)
	if e != nil {
		t.Fatal(e)
	}
	projects, e := project.NewAuthority(store, project.AuthorityDependencies{Sessions: accounts, Routes: accounts, AuditFacts: map[ac.Producer]ac.ProjectFactAuthority{ac.SecretProducer: checker}})
	if e != nil {
		t.Fatal(e)
	}
	delegate, e := project.NewSecretAuthority(projects)
	if e != nil {
		t.Fatal(e)
	}
	models, e := model.NewAuthority(store, model.Authorizations{Sessions: accounts, System: accounts, Projects: projects})
	if e != nil {
		t.Fatal(e)
	}
	router, e := model.NewSecretUsageRouter(models, accounts)
	if e != nil {
		t.Fatal(e)
	}
	aud, e := audit.New(store, ck, audit.Authorizations{Sessions: accounts, System: accounts, Accounts: accounts, Projects: projects, Models: models})
	if e != nil {
		t.Fatal(e)
	}
	tap := &projectCredentialAuditTap{next: aud}
	s, e := secret.New(store, sk, tap, secret.Authorizations{Sessions: accounts, System: accounts, Projects: delegate, Usage: router, AccountWrites: accounts})
	if e != nil {
		t.Fatal(e)
	}
	if e = s.Initialize(testContext(t)); e != nil {
		t.Fatal(e)
	}
	t.Cleanup(s.StopMaintenance)
	return s, tap
}
func (v *projectCredentialFixture) install(t *testing.T, writes sc.HumanWriteCommands) {
	t.Helper()
	boundary, e := account.NewHTTPBoundary(v.core, systemHTTPOrigin)
	if e != nil {
		t.Fatal(e)
	}
	h, e := model.NewProjectCredentialHTTPHandler(writes, boundary)
	if e != nil {
		t.Fatal(e)
	}
	old := v.handler
	wrapped := httpapi.Handler(slog.New(slog.NewJSONHandler(v.logs, nil)), h)
	v.handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if model.HandlesProjectCredentialHTTPPath(r.URL.Path) {
			wrapped.ServeHTTP(w, r)
		} else {
			old.ServeHTTP(w, r)
		}
	})
}
func (v *projectCredentialFixture) collection() string {
	return projectOwnerReadPath(v.project.ID) + "/model-credentials"
}
func (v *projectCredentialFixture) lookupPath() string {
	return projectOwnerReadPath(v.project.ID) + "/model-credential-commands/lookup"
}
func (v *projectCredentialFixture) request(t *testing.T, b systemHTTPBrowser, method, path, key string, body []byte) systemHTTPResponse {
	t.Helper()
	r := projectUpdateRequest(testContext(t), b, method, path, key, body)
	w := &projectUsageRecorder{ResponseRecorder: httptest.NewRecorder()}
	v.handler.ServeHTTP(w, r)
	return systemHTTPResponse{w.Code, w.Header().Clone(), bytes.Clone(w.Body.Bytes())}
}
func (v *projectCredentialFixture) create(t *testing.T, key, value string) string {
	t.Helper()
	response := v.request(t, v.ownerBrowser, "POST", v.collection(), key, projectUpdateJSON(t, map[string]any{"value": value})).want(t, 200)
	obj := response.object(t)
	if len(obj) != 4 || obj["purpose"] != "model" || obj["version"] != "1" || obj["deleted"] != false {
		t.Fatal("unsafe Create shape")
	}
	return obj["credential_id"].(string)
}
func (v *projectCredentialFixture) lookup(t *testing.T, b systemHTTPBrowser, key string, kind sc.MutationKind, target string, expected f.Version) sc.WriteCommandLookupRequest {
	t.Helper()
	command, e := f.NewCommandIdentity("secret", []string{v.project.ID.String(), b.actor.Details().UserID}, string(kind), f.IdempotencyKey(key))
	if e != nil {
		t.Fatal(e)
	}
	r := sc.WriteCommandLookupRequest{Actor: b.actor, Scope: v.scope, Identity: command, Kind: kind, Purpose: sc.Model, ExpectedVersion: expected}
	if target != "" {
		key, e := f.ParseID[sc.Credential](target)
		if e != nil {
			t.Fatal("invalid target")
		}
		r.Ref, _ = sc.NewCredentialRef(key, v.scope)
	}
	if r.Validate() != nil {
		t.Fatal("bad fixture lookup")
	}
	return r
}
func projectCredentialLookupBody(t *testing.T, kind sc.MutationKind, target string, version f.Version) []byte {
	t.Helper()
	v := map[string]any{"kind": kind}
	if kind != sc.Create {
		v["credential_id"] = target
		v["expected_version"] = version
	}
	return projectUpdateJSON(t, v)
}

type projectCredentialFacts struct{ Canonical, Receipts, Audits, Events int64 }

func (v *projectCredentialFixture) facts(t *testing.T) projectCredentialFacts {
	t.Helper()
	var out projectCredentialFacts
	e := v.raw.QueryRow(testContext(t), `SELECT (SELECT count(*) FROM agenteam_secret.secrets WHERE project_id=$1),(SELECT count(*) FROM agenteam_secret.secret_command_receipts WHERE project_id=$1),(SELECT count(*) FROM agenteam_audit.audit_records WHERE project_id=$1 AND producer='secret'),(SELECT count(*) FROM agenteam_outbox.events WHERE project_id=$1)`, v.project.ID.String()).Scan(&out.Canonical, &out.Receipts, &out.Audits, &out.Events)
	if e != nil {
		t.Fatal(e)
	}
	return out
}
func (v *projectCredentialFixture) unchanged(t *testing.T, want projectCredentialFacts) {
	t.Helper()
	if got := v.facts(t); got != want {
		t.Fatal("canonical/receipt/Audit/Event changed", got, want)
	}
}
func projectCredentialTop(t *testing.T) {
	t.Helper()
	start := time.Now()
	t.Cleanup(func() {
		if time.Since(start) > 2*time.Minute {
			t.Error("Project Credential top including cleanup exceeded 120s")
		}
	})
}
func projectCredentialSchema(t *testing.T, name string, body []byte) {
	t.Helper()
	python := os.Getenv("AGENTEAM_PROJECT_CREDENTIAL_SCHEMA_PYTHON")
	if python == "" {
		t.Fatal("fixed schema interpreter required")
	}
	root, e := filepath.Abs("../..")
	if e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, python, "-c", `import json,pathlib,sys
from jsonschema import Draft202012Validator,FormatChecker
from referencing import Registry,Resource
from referencing.jsonschema import DRAFT202012
base=pathlib.Path(sys.argv[1])/'api/openapi';registry=Registry()
for file in ['project-model-credentials.json','common.json']:
 p=base/file;registry=registry.with_resource(p.as_uri(),Resource.from_contents(json.loads(p.read_text()),default_specification=DRAFT202012))
v=json.load(sys.stdin);s={'$ref':(base/'project-model-credentials.json').as_uri()+'#/components/schemas/'+sys.argv[2]}
if list(Draft202012Validator(s,registry=registry,format_checker=FormatChecker()).iter_errors(v)):print('SAFE_BODY_SCHEMA_FAIL');sys.exit(1)
print('SAFE_BODY_SCHEMA_PASS')`, root, name)
	cmd.Stdin = bytes.NewReader(body)
	out, e := cmd.CombinedOutput()
	if e != nil {
		t.Fatal("safe response schema failed", e, string(out))
	}
}
func projectCredentialExport(t *testing.T, name, method, path string, response systemHTTPResponse) {
	t.Helper()
	dir := os.Getenv("AGENTEAM_PROJECT_CREDENTIAL_BODY_DIR")
	if dir == "" {
		return
	}
	if !filepath.IsAbs(dir) {
		t.Fatal("absolute evidence directory required")
	}
	if e := os.MkdirAll(dir, 0700); e != nil {
		t.Fatal(e)
	}
	digest := sha256.Sum256(response.body)
	schema, e := os.ReadFile("../../api/openapi/project-model-credentials.json")
	if e != nil {
		t.Fatal(e)
	}
	schemaHash := sha256.Sum256(schema)
	metadata := map[string]any{"method": method, "path": path, "status": response.status, "content_type": response.headers.Get("Content-Type"), "content_length": response.headers.Get("Content-Length"), "body_sha256": hex.EncodeToString(digest[:]), "schema_sha256": hex.EncodeToString(schemaHash[:]), "run": os.Getenv("AGENTEAM_PROJECT_CREDENTIAL_RUN"), "candidate": os.Getenv("AGENTEAM_PROJECT_CREDENTIAL_CANDIDATE")}
	for file, data := range map[string][]byte{name + ".json": response.body, name + "-source.json": projectUpdateJSON(t, metadata)} {
		if e = os.WriteFile(filepath.Join(dir, file), data, 0600); e != nil {
			t.Fatal(e)
		}
	}
}
