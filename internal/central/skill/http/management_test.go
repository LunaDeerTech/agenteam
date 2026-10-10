package skillhttp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/skill"
	sc "github.com/LunaDeerTech/agenteam/internal/central/skill/contract"
)

type managementPorts struct {
	installs, lookups, lists int
	actor                    id.Actor
	project                  id.ProjectID
	meta                     f.CommandMeta
	input                    skill.InstallRequest
	query                    skill.OwnerCatalogQuery
	ctx                      context.Context
	receipt                  skill.InstallReceipt
	page                     skill.OwnerCatalogPage
	err                      error
}

func (p *managementPorts) Install(ctx context.Context, actor id.Actor, meta f.CommandMeta, project id.ProjectID, input skill.InstallRequest) (skill.InstallReceipt, error) {
	p.installs++
	p.ctx, p.actor, p.meta, p.project, p.input = ctx, actor, meta, project, input
	return p.receipt, p.err
}
func (p *managementPorts) LookupInstall(ctx context.Context, actor id.Actor, project id.ProjectID, key f.IdempotencyKey, input skill.InstallRequest) (skill.InstallReceipt, error) {
	p.lookups++
	p.ctx, p.actor, p.project, p.input = ctx, actor, project, input
	p.meta.IdempotencyKey = key
	return p.receipt, p.err
}
func (p *managementPorts) List(ctx context.Context, actor id.Actor, project id.ProjectID, q skill.OwnerCatalogQuery) (skill.OwnerCatalogPage, error) {
	p.lists++
	p.ctx, p.actor, p.project, p.query = ctx, actor, project, q
	return p.page, p.err
}
func managementFixture() (*managementHandler, *testBoundary, *managementPorts) {
	p := &managementPorts{receipt: skill.InstallReceipt{
		InstallationID: testID[skill.Installation](40), SkillID: testMetadata().ID, ProjectID: testMetadata().ProjectID,
		RevisionID: testID[sc.Revision](41), Revision: 1, Version: 1, ObjectID: testID[oc.StoredObject](42), PackageSHA256: f.Digest("sha256:" + strings.Repeat("a", 64)),
	}, page: skill.OwnerCatalogPage{Items: []sc.Metadata{testMetadata()}}}
	b := &testBoundary{}
	return &managementHandler{p, p, b}, b, p
}
func installationBody(t *testing.T) string {
	t.Helper()
	text := "---\nname: ordinary-test\ndescription: Owner supplied package\n---\nUse the actual package.\n"
	raw, err := json.Marshal(struct {
		Request installRequestDTO `json:"request"`
	}{installRequestDTO{testMetadata().ID, "create", installSourceDTO{"text_files", []installFileDTO{{"SKILL.md", &text}}}}})
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}
func installationRequest(path, body string) *http.Request {
	r := httptest.NewRequest("POST", testPath(path), strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Idempotency-Key", "skill-install-original")
	return r
}

func TestSkillOwnerInstallHTTPOriginalServiceAndLookup(t *testing.T) {
	for _, lookup := range []bool{false, true} {
		name := "install"
		path := "/skills"
		if lookup {
			name = "lookup"
			path += "/commands/lookup"
		}
		t.Run(name, func(t *testing.T) {
			h, b, p := managementFixture()
			w := newTestWriter()
			r := installationRequest(path, installationBody(t))
			start := time.Now()
			if serveTest(h, r, w) || w.Code != 200 {
				t.Fatal("original service dispatch failed", w.Code)
			}
			if p.installs+p.lookups != 1 || p.lists != 0 || lookup && p.lookups != 1 || !lookup && p.installs != 1 {
				t.Fatal("lookup resubmitted or route lost")
			}
			if !p.actor.Equal(testActor()) || p.project != testMetadata().ProjectID || p.meta.IdempotencyKey != "skill-install-original" || p.input.Validate() != nil || p.input.SkillID() != testMetadata().ID || p.meta.ExpectedVersion != nil {
				t.Fatal("original Human/project/intent lost")
			}
			if !lookup && p.meta.RequestID.Validate() != nil {
				t.Fatal("real middleware RequestID lost")
			}
			deadline, ok := p.ctx.Deadline()
			if !ok || deadline.After(start.Add(installBudget+time.Second)) || deadline.Before(start.Add(installBudget-time.Second)) {
				t.Fatal("write total deadline changed")
			}
			if p.ctx.Err() == nil || b.checks.Load() != 1 || b.auths.Load() != 1 {
				t.Fatal("request boundary or original context retirement lost")
			}
			dto := wireObject(t, w.Body.Bytes())
			if len(dto) != 3 || dto["skill_id"] != testMetadata().ID.String() || dto["revision"] != "1" || dto["version"] != "1" {
				t.Fatal("receipt exposed non-public fields")
			}
			w.cleared(t)
		})
	}
}

func TestSkillOwnerInstallHTTPRejectsInvalidIntentsBeforeService(t *testing.T) {
	valid := installationBody(t)
	for _, tc := range []struct {
		name, body string
		alter      func(*http.Request)
		status     int
	}{
		{"duplicate", strings.Replace(valid, `"mode":"create"`, `"mode":"create","mode":"create"`, 1), nil, 400},
		{"actor", strings.TrimSuffix(valid, "}") + `,"actor":"private-canary"}`, nil, 400},
		{"nested-unknown", strings.Replace(valid, `"kind":"text_files"`, `"kind":"text_files","url":"https://private.invalid"`, 1), nil, 400},
		{"missing-text", strings.Replace(valid, `"utf8_text":`, `"other":`, 1), nil, 400},
		{"update", strings.Replace(valid, `"mode":"create"`, `"mode":"update"`, 1), nil, 400},
		{"null-request", `{"request":null}`, nil, 400},
		{"duplicate-key", valid, func(r *http.Request) { r.Header.Add("Idempotency-Key", "other") }, 400},
		{"encoding", valid, func(r *http.Request) { r.Header.Set("Content-Encoding", "") }, 415},
		{"query", valid, func(r *http.Request) { r.URL.RawQuery = "ignored=1" }, 400},
		{"too-large", strings.Repeat(" ", installBodyLimit) + valid, nil, 413},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, _, p := managementFixture()
			w := newTestWriter()
			r := installationRequest("/skills", tc.body)
			if tc.alter != nil {
				tc.alter(r)
			}
			if serveTest(h, r, w) || w.Code != tc.status || p.installs+p.lookups+p.lists != 0 {
				t.Fatal("invalid request reached provider", w.Code)
			}
			if strings.Contains(w.Body.String(), "private-canary") || strings.Contains(w.Body.String(), "private.invalid") {
				t.Fatal("input echoed")
			}
			w.cleared(t)
		})
	}
}

func TestSkillOwnerInstallHTTPCurrentAuthorityAndUnknown(t *testing.T) {
	for _, name := range []string{"csrf", "session", "domain-revoked", "unknown", "wrong-receipt", "short-context"} {
		t.Run(name, func(t *testing.T) {
			h, b, p := managementFixture()
			w := newTestWriter()
			r := installationRequest("/skills/commands/lookup", installationBody(t))
			want := 401
			calls := 0
			switch name {
			case "csrf":
				b.check = func(*http.Request) error { return f.NewFault(f.Forbidden, f.NotStarted) }
				want = 403
			case "session":
				b.auth = func(*http.Request) error { return f.NewFault(f.SessionRevoked, f.NotStarted) }
			case "domain-revoked":
				p.err = f.NewFault(f.SessionRevoked, f.NotStarted)
				calls = 1
			case "unknown":
				p.err = f.NewFault(f.CommitUnknown, f.Unknown)
				want = 503
				calls = 1
			case "wrong-receipt":
				p.receipt.ProjectID = testID[id.Project](999)
				want = 503
				calls = 1
			case "short-context":
				ctx, cancel := context.WithTimeout(r.Context(), time.Second)
				defer cancel()
				r = r.WithContext(ctx)
				want = 200
				calls = 1
			}
			if serveTest(h, r, w) || w.Code != want || p.installs != 0 || p.lookups != calls {
				t.Fatal("authorization/outcome changed", w.Code)
			}
			if name == "unknown" || name == "wrong-receipt" {
				if wireObject(t, w.Body.Bytes())["commit_state"] != "unknown" {
					t.Fatal("physical uncertainty lost")
				}
			}
			if name == "short-context" {
				got, _ := p.ctx.Deadline()
				want, _ := r.Context().Deadline()
				if got != want {
					t.Fatal("earlier context replaced")
				}
			}
			w.cleared(t)
		})
	}
}

func TestSkillOwnerCatalogHTTPBoundedPageAndQueries(t *testing.T) {
	for _, method := range []string{"GET", "HEAD"} {
		h, _, p := managementFixture()
		w := newTestWriter()
		r := httptest.NewRequest(method, testPath("/skills/catalog?limit=1&cursor=original"), nil)
		p.page.NextCursor = "next"
		if serveTest(h, r, w) || w.Code != 200 || p.lists != 1 || p.query.Limit != 1 || p.query.Cursor != "original" {
			t.Fatal("page dispatch failed")
		}
		if method == "HEAD" && w.Body.Len() != 0 {
			t.Fatal("HEAD emitted body")
		}
		if method == "GET" {
			dto := wireObject(t, w.Body.Bytes())
			if len(dto) != 2 || dto["next_cursor"] != "next" {
				t.Fatal("page shape")
			}
		}
		w.cleared(t)
	}
	for _, suffix := range []string{"?", "?limit=0", "?limit=101", "?limit=01", "?limit=1&limit=2", "?cursor=", "?unknown=1", "?limit=1;cursor=x"} {
		h, _, p := managementFixture()
		w := newTestWriter()
		if serveTest(h, httptest.NewRequest("GET", testPath("/skills/catalog"+suffix), nil), w) || w.Code != 400 || p.lists != 0 {
			t.Fatal("unbounded/ambiguous query accepted", suffix)
		}
	}
}
