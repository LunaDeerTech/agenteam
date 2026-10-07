//go:build integration

package model_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/LunaDeerTech/agenteam/internal/central/account"
	"github.com/LunaDeerTech/agenteam/internal/central/audit"
	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	ec "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/httpapi"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/outbox"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/project"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	projecthttp "github.com/LunaDeerTech/agenteam/internal/central/project/http"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// Every identity comes from the accepted formal Bootstrap/Invite/Redeem/Login
// fixture. Only persisted test Skills prepare Project.Create; none binds root.
type projectUpdateFixture struct {
	*projectUsageHTTPFixture
	commands *project.Service
	gates    *projectUpdateGates
	journal  *outbox.Service
}
type projectUpdateGates struct {
	realRejected atomic.Int32
	*project.Authority
	producerCurrent, producerNew, projectCurrent, projectNew, audits atomic.Int32
	reject                                                           string
}

func (g *projectUpdateGates) ValidateAppendInTx(ctx context.Context, tx f.Tx, a id.Actor, s ec.Summary, d oc.Dependencies, stage oc.Stage) error {
	if stage == oc.CurrentAccess {
		g.producerCurrent.Add(1)
	} else {
		g.producerNew.Add(1)
	}
	if g.reject == "producer-summary" {
		s.PayloadDigest = f.Digest("sha256:" + strings.Repeat("2", 64))
	}
	err := g.Authority.ValidateAppendInTx(ctx, tx, a, s, d, stage)
	if err != nil && g.reject == "producer-summary" {
		g.realRejected.Add(1)
	}
	if err == nil && g.reject == "producer" {
		return f.NewFault(f.Forbidden, f.NotStarted)
	}
	return err
}
func (g *projectUpdateGates) ValidateInTx(ctx context.Context, tx f.Tx, r oc.ProjectRequest, d oc.Dependencies) error {
	if r.Details().Stage == oc.CurrentAccess {
		g.projectCurrent.Add(1)
	} else {
		g.projectNew.Add(1)
	}
	if g.reject == "project-summary" {
		fields := r.Details()
		fields.Event.PayloadDigest = f.Digest("sha256:" + strings.Repeat("3", 64))
		var err error
		r, err = oc.NewProjectRequest(fields)
		if err != nil {
			return err
		}
	}
	err := g.Authority.ValidateInTx(ctx, tx, r, d)
	if err != nil && g.reject == "project-summary" {
		g.realRejected.Add(1)
	}
	if err == nil && g.reject == "project" {
		return f.NewFault(f.Forbidden, f.NotStarted)
	}
	return err
}
func (g *projectUpdateGates) CheckAppendInTx(ctx context.Context, tx f.Tx, e ac.Entry, k ac.AppendKey) error {
	g.audits.Add(1)
	if g.reject == "audit-cause" {
		var err error
		k, err = ac.NewAppendKey(ac.ProjectProducer, "sha256:"+strings.Repeat("1", 64), 0)
		if err != nil {
			return err
		}
	}
	if g.reject == "audit-fields" {
		fields := e.Fields()
		metadata, err := fields.Metadata.ProjectFields()
		if err != nil {
			return err
		}
		metadata.ChangedFields = []ac.ProjectChangedField{ac.ProjectDescriptionChanged}
		fields.Metadata, err = ac.ProjectMetadata(ac.ProjectUpdate, metadata)
		if err != nil {
			return err
		}
		e, err = ac.NewEntry(fields)
		if err != nil {
			return err
		}
	}
	err := g.Authority.CheckAppendInTx(ctx, tx, e, k)
	if err != nil && (g.reject == "audit-cause" || g.reject == "audit-fields") {
		g.realRejected.Add(1)
	}
	return err
}
func newProjectUpdateFixture(t *testing.T) *projectUpdateFixture {
	t.Helper()
	v := newProjectOwnerReadFixture(t)
	s, g, j := projectUpdateService(t, v, v.tracked)
	u := &projectUpdateFixture{v, s, g, j}
	u.install(t, s)
	return u
}
func projectUpdateService(t *testing.T, v *projectUsageHTTPFixture, store sharedStore) (*project.Service, *projectUpdateGates, *outbox.Service) {
	t.Helper()
	ak, ck, _ := testKeys(t)
	accounts, e := account.NewAuthority(store, ak)
	if e != nil {
		t.Fatal(e)
	}
	authority, e := project.NewAuthority(store, project.AuthorityDependencies{Sessions: accounts, Routes: accounts})
	if e != nil {
		t.Fatal(e)
	}
	g := &projectUpdateGates{Authority: authority}
	aud, e := audit.New(store, ck, audit.Authorizations{Accounts: accounts, Sessions: accounts, System: accounts, Projects: g})
	if e != nil {
		t.Fatal(e)
	}
	catalog := ec.NewCatalog()
	events, e := pc.RegisterProjectEvents(catalog)
	if e != nil {
		t.Fatal(e)
	}
	process := liveProcess{newID[oc.Process](t)}
	journal, e := outbox.New(store, catalog, outbox.Authorizations{Producers: map[ec.StableName]oc.ProducerAuthority{pc.ProjectProducer: g}, Projects: g, Processes: process, Sessions: accounts, System: accounts, Audit: aud, Cursors: ck})
	if e != nil {
		t.Fatal(e)
	}
	service, e := project.New(store, project.Dependencies{Authority: authority, Activity: accounts, Audit: aud, Events: journal, ProjectEvents: events, Processes: process, Cursors: ck}, project.DefaultConfig())
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		service.Stop()
		ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
		err := service.Drain(ctx)
		cancel()
		if err != nil {
			t.Error("update cleanup timed out; retaining actual ownership", err)
			service.Force()
			if e := service.Drain(context.Background()); e != nil {
				t.Error(e)
			}
		}
	})
	return service, g, journal
}
func (v *projectUpdateFixture) install(t *testing.T, s *project.Service) {
	t.Helper()
	boundary, e := account.NewHTTPBoundary(v.core, systemHTTPOrigin)
	if e != nil {
		t.Fatal(e)
	}
	handler, e := projecthttp.NewUpdateHTTPHandler(s, boundary)
	if e != nil {
		t.Fatal(e)
	}
	old := v.handler
	updates := httpapi.Handler(slog.New(slog.NewJSONHandler(v.logs, nil)), handler)
	v.handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if projecthttp.HandlesUpdateRequest(r.Method, r.URL.Path) {
			updates.ServeHTTP(w, r)
		} else {
			old.ServeHTTP(w, r)
		}
	})
}
func projectUpdateRequest(ctx context.Context, b systemHTTPBrowser, method, path, key string, body []byte) *http.Request {
	r := projectUsageRequest(ctx, b, method, path)
	r.Body = io.NopCloser(bytes.NewReader(body))
	r.ContentLength = int64(len(body))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-CSRF-Token", b.csrf)
	r.Header.Set("Idempotency-Key", key)
	return r
}
func (v *projectUpdateFixture) write(t *testing.T, b systemHTTPBrowser, method, path, key string, body []byte) systemHTTPResponse {
	t.Helper()
	w := &projectUsageRecorder{ResponseRecorder: httptest.NewRecorder()}
	v.handler.ServeHTTP(w, projectUpdateRequest(testContext(t), b, method, path, key, body))
	return systemHTTPResponse{w.Code, w.Header().Clone(), bytes.Clone(w.Body.Bytes())}
}
func projectUpdateJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, e := json.Marshal(v)
	if e != nil {
		t.Fatal(e)
	}
	return b
}
func projectUpdateBody(t *testing.T, version f.Version, name, description *string) []byte {
	v := map[string]any{"expected_version": version}
	if name != nil {
		v["name"] = *name
	}
	if description != nil {
		v["description"] = *description
	}
	return projectUpdateJSON(t, v)
}
func projectUpdateLookupPath(p pc.ProjectID) string {
	return projectOwnerReadPath(p) + "/commands/lookup"
}

var projectUpdateLookupBody = []byte(`{"command":"update"}`)

func projectUpdatePtr[T any](v T) *T { return &v }

type projectUpdateFacts struct {
	Project, Activity         string
	Completed, Audits, Events int64
}

func projectUpdateSnapshot(t *testing.T, v *projectUsageHTTPFixture) projectUpdateFacts {
	t.Helper()
	var s projectUpdateFacts
	e := v.raw.QueryRow(testContext(t), `SELECT (SELECT row_to_json(p)::text FROM agenteam_project.projects p WHERE id=$1),(SELECT last_activity_at::text FROM agenteam_account.sessions WHERE id=$2),(SELECT count(*) FROM agenteam_project.commands WHERE project_id=$1 AND state='completed' AND command_name='update'),(SELECT count(*) FROM agenteam_audit.audit_records WHERE project_id=$1 AND action='project.update'),(SELECT count(*) FROM agenteam_outbox.events WHERE project_id=$1 AND event_type='project.updated')`, v.project.ID.String(), v.ownerBrowser.actor.Details().SessionID).Scan(&s.Project, &s.Activity, &s.Completed, &s.Audits, &s.Events)
	if e != nil {
		t.Fatal(e)
	}
	return s
}
func projectUpdateExport(t *testing.T, name, method, path string, response systemHTTPResponse) {
	t.Helper()
	dir := os.Getenv("AGENTEAM_PROJECT_UPDATE_BODY_DIR")
	if dir == "" {
		return
	}
	if !filepath.IsAbs(dir) {
		t.Fatal("absolute body directory required")
	}
	if e := os.MkdirAll(dir, 0700); e != nil {
		t.Fatal(e)
	}
	sum := sha256.Sum256(response.body)
	schema, e := os.ReadFile("../../api/openapi/project-owner.json")
	if e != nil {
		t.Fatal(e)
	}
	sh := sha256.Sum256(schema)
	meta := map[string]any{"method": method, "path": path, "status": response.status, "content_type": response.headers.Get("Content-Type"), "content_length": response.headers.Get("Content-Length"), "body_sha256": hex.EncodeToString(sum[:]), "schema_sha256": hex.EncodeToString(sh[:]), "run": os.Getenv("AGENTEAM_PROJECT_UPDATE_RUN"), "candidate": os.Getenv("AGENTEAM_PROJECT_UPDATE_CANDIDATE")}
	for file, data := range map[string][]byte{name + ".json": response.body, name + "-source.json": projectUpdateJSON(t, meta)} {
		if e := os.WriteFile(filepath.Join(dir, file), data, 0600); e != nil {
			t.Fatal(e)
		}
	}
}
