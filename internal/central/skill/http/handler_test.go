package skillhttp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/account"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/skill"
	sc "github.com/LunaDeerTech/agenteam/internal/central/skill/contract"
)

func testID[T any](n int) f.ID[T] {
	v, err := f.ParseID[T](fmt.Sprintf("01900000-0000-7000-8000-%012x", n))
	if err != nil {
		panic(err)
	}
	return v
}
func testActor() id.Actor      { a, _ := id.NewHuman(testID[id.User](1), testID[id.Session](2)); return a }
func testPath(s string) string { return projectPrefix + testID[id.Project](4).String() + s }
func testMetadata() sc.Metadata {
	return sc.Metadata{ID: testID[pc.Skill](3), ProjectID: testID[id.Project](4), Name: "Add Skills", NormalizedName: "add-skills", Description: "Read <guidance> & 中文\nwith\ttabs", Protected: true, CurrentRevision: 1, Version: math.MaxInt64}
}

type testBoundary struct {
	check, auth   func(*http.Request) error
	checks, auths atomic.Int32
	actor         *id.Actor
	problem       error
}

func (b *testBoundary) CheckRequest(_ http.ResponseWriter, r *http.Request) error {
	b.checks.Add(1)
	if b.check != nil {
		return b.check(r)
	}
	return nil
}
func (b *testBoundary) RequireHuman(r *http.Request) (id.Actor, error) {
	b.auths.Add(1)
	if b.auth != nil {
		if err := b.auth(r); err != nil {
			return id.Actor{}, err
		}
	}
	if b.actor != nil {
		return *b.actor, nil
	}
	return testActor(), nil
}
func (b *testBoundary) WriteProblem(w http.ResponseWriter, r *http.Request, err error) {
	b.problem = err
	(&account.HTTPBoundary{}).WriteProblem(w, r, err)
}

// The private controls isolate transport/projection. They do not certify SQL
// or current Account/Project authorization; those use real services in PG.
type testPorts struct {
	before  func(context.Context)
	err     error
	calls   int
	method  string
	actor   id.Actor
	project id.ProjectID
	target  sc.SkillID
	items   []sc.Metadata
	item    sc.Metadata
}

func (p *testPorts) enter(ctx context.Context, a id.Actor, project id.ProjectID, method string) {
	p.calls++
	p.actor, p.project, p.method = a, project, method
	if p.before != nil {
		p.before(ctx)
	}
}
func (p *testPorts) ListSkills(ctx context.Context, a id.Actor, project id.ProjectID) ([]sc.Metadata, error) {
	p.enter(ctx, a, project, "list")
	return p.items, p.err
}
func (p *testPorts) GetSkill(ctx context.Context, a id.Actor, project id.ProjectID, target sc.SkillID) (sc.Metadata, error) {
	p.enter(ctx, a, project, "get")
	p.target = target
	return p.item, p.err
}
func testHandler() (*handler, *testBoundary, *testPorts) {
	m := testMetadata()
	p := &testPorts{item: m, items: []sc.Metadata{m}}
	b := &testBoundary{}
	return &handler{p, b}, b, p
}
func wireObject(t *testing.T, raw []byte) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	return out
}
func TestSkillOwnerHTTPReadRoutesAndHEAD(t *testing.T) {
	for _, kind := range []string{"list", "get"} {
		t.Run(kind, func(t *testing.T) {
			path := testPath("/skills")
			if kind == "get" {
				path += "/" + testMetadata().ID.String()
			}
			var length int
			for _, method := range []string{"GET", "HEAD"} {
				h, b, p := testHandler()
				w := newTestWriter()
				if serveTest(h, httptest.NewRequest(method, path, nil), w) || w.Code != 200 || p.calls != 1 || p.method != kind || !p.actor.Equal(testActor()) || p.project != testMetadata().ProjectID || b.checks.Load() != 1 || b.auths.Load() != 1 {
					t.Fatal("real dispatch arguments", method, w.Code)
				}
				if kind == "get" && p.target != testMetadata().ID {
					t.Fatal("target lost")
				}
				n, e := strconv.Atoi(w.Header().Get("Content-Length"))
				if e != nil || n <= 0 || w.Header().Get("Content-Type") != "application/json" || w.Header().Get("Cache-Control") != "no-store" {
					t.Fatal("safe headers")
				}
				if method == "GET" {
					length = n
					if w.Body.Len() != n {
						t.Fatal("length")
					}
					dto := wireObject(t, w.Body.Bytes())
					if kind == "list" {
						if len(dto) != 1 {
							t.Fatal("directory fields")
						}
						items, ok := dto["items"].([]any)
						if !ok || len(items) != 1 {
							t.Fatal("bounded directory")
						}
						dto = items[0].(map[string]any)
					}
					if len(dto) != 8 || dto["name"] != "Add Skills" || dto["normalized_name"] != "add-skills" || dto["description"] != testMetadata().Description || dto["protected"] != true || dto["id"] != testMetadata().ID.String() || dto["project_id"] != testMetadata().ProjectID.String() || dto["current_revision"] != "1" || dto["version"] != "9223372036854775807" {
						t.Fatal("explicit eight-field metadata")
					}
				} else if n != length || w.Body.Len() != 0 {
					t.Fatal("HEAD projection differs")
				}
				w.cleared(t)
			}
		})
	}
}
func TestSkillOwnerHTTPStrictRequestBoundary(t *testing.T) {
	for _, tc := range []struct {
		method, suffix, body string
		status               int
	}{
		{"POST", "/skills", "", 405}, {"GET", "/skills/", "", 404}, {"GET", "/skills/not-id", "", 400}, {"GET", "/skills/a/b", "", 404},
		{"GET", "/skills?", "", 400}, {"GET", "/skills?limit=1", "", 400}, {"GET", "/skills?cursor=x&cursor=y", "", 400}, {"GET", "/skills?%FF=x", "", 400}, {"GET", "/skills?raw-secret=private-canary", "", 400}, {"GET", "/skills?;", "", 400}, {"GET", "/skills", "x", 400},
	} {
		t.Run(tc.method+tc.suffix, func(t *testing.T) {
			h, _, p := testHandler()
			w := newTestWriter()
			r := httptest.NewRequest(tc.method, testPath(tc.suffix), strings.NewReader(tc.body))
			if serveTest(h, r, w) || w.Code != tc.status || p.calls != 0 {
				t.Fatal("invalid request reached service", w.Code)
			}
			if strings.Contains(w.Body.String(), "private-canary") || strings.Contains(w.Body.String(), "raw-secret") {
				t.Fatal("request leaked")
			}
			if tc.status == 405 && w.Header().Get("Allow") != "GET, HEAD" {
				t.Fatal("Allow")
			}
		})
	}
	for _, variant := range []string{"transfer", "unknown-length", "hidden-byte", "not-eof"} {
		t.Run(variant, func(t *testing.T) {
			h, _, p := testHandler()
			w := newTestWriter()
			r := httptest.NewRequest("GET", testPath("/skills"), nil)
			body := &testBody{Reader: strings.NewReader("")}
			r.Body = body
			switch variant {
			case "transfer":
				r.TransferEncoding = []string{"chunked"}
			case "unknown-length":
				r.ContentLength = -1
			case "hidden-byte":
				body.Reader = strings.NewReader("x")
			case "not-eof":
				body.Reader = errorReader{}
			}
			if serveTest(h, r, w) || w.Code != 400 || p.calls != 0 || body.closes.Load() != 1 {
				t.Fatal("body was not rejected and closed")
			}
		})
	}
}

type errorReader struct{}

func (errorReader) Read([]byte) (int, error) { return 0, errors.New("private body canary") }

func TestSkillOwnerHTTPBoundInstancesAndAuthentication(t *testing.T) {
	service, boundary := &skill.Service{}, &account.HTTPBoundary{}
	for _, tc := range []struct {
		s *skill.Service
		b *account.HTTPBoundary
	}{{nil, boundary}, {service, nil}} {
		if _, e := NewHTTPHandler(tc.s, tc.b); e == nil {
			t.Fatal("nil dependency accepted")
		}
	}
	got, e := NewHTTPHandler(service, boundary)
	if e != nil || got.(*handler).reader != service || got.(*handler).boundary != boundary {
		t.Fatal("real instances not retained")
	}
	for _, stage := range []string{"check", "human", "invalid-actor"} {
		t.Run(stage, func(t *testing.T) {
			h, b, p := testHandler()
			deny := func(*http.Request) error { return f.NewFault(f.Unauthenticated, f.NotStarted) }
			switch stage {
			case "check":
				b.check = deny
			case "human":
				b.auth = deny
			default:
				bad := id.Actor{}
				b.actor = &bad
			}
			w := newTestWriter()
			if serveTest(h, httptest.NewRequest("POST", testPath("/skills?bad"), nil), w) || w.Code != 401 || p.calls != 0 {
				t.Fatal("authentication must precede method/input")
			}
		})
	}
}
func TestSkillOwnerHTTPFaultAndHEADProblem(t *testing.T) {
	for _, code := range []f.Code{f.SessionRevoked, f.NotFound, f.InvalidState, f.ProjectNotActive, f.DependencyUnbound, f.DependencyUnavailable, f.CommitUnknown} {
		t.Run(string(code), func(t *testing.T) {
			fault := f.NewFault(code, f.NotStarted).WithCause(errors.New("private SQL canary"))
			if code == f.CommitUnknown {
				fault = f.NewFault(code, f.Unknown)
				fault.CauseID = testID[f.TransactionAttempt](41).String()
			}
			var length string
			for _, method := range []string{"GET", "HEAD"} {
				h, b, p := testHandler()
				p.err = fault
				w := newTestWriter()
				if serveTest(h, httptest.NewRequest(method, testPath("/skills"), nil), w) || b.problem != fault || w.Code < 400 {
					t.Fatal("original fault lost")
				}
				if w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("Content-Type") != "application/problem+json" {
					t.Fatal("Problem headers")
				}
				if method == "HEAD" {
					if w.Body.Len() != 0 || w.Header().Get("Content-Length") != length {
						t.Fatal("HEAD Problem body/length")
					}
					continue
				}
				length = w.Header().Get("Content-Length")
				body := wireObject(t, w.Body.Bytes())
				if body["code"] != string(code) || body["instance"] != "/api/v1" || strings.Contains(w.Body.String(), "private SQL") || strings.Contains(w.Body.String(), "items") || body["cause_id"] != nil {
					t.Fatal("unsafe Problem")
				}
				if code == f.CommitUnknown && (body["commit_state"] != "unknown" || strings.Contains(w.Body.String(), fault.CauseID)) {
					t.Fatal("Unknown lost or private cause exposed")
				}
			}
		})
	}
}
func TestSkillOwnerHTTPBudgetPropagation(t *testing.T) {
	for _, earlier := range []bool{false, true} {
		t.Run(strconv.FormatBool(earlier), func(t *testing.T) {
			h, _, p := testHandler()
			w := newTestWriter()
			ctx := context.Background()
			cancel := func() {}
			var want time.Time
			if earlier {
				want = time.Now().Add(time.Second)
				ctx, cancel = context.WithDeadline(ctx, want)
			}
			defer cancel()
			start := time.Now()
			p.before = func(ctx context.Context) {
				end, ok := ctx.Deadline()
				if !ok || earlier && !end.Equal(want) || !earlier && (end.Before(start.Add(readBudget)) || end.After(time.Now().Add(readBudget))) {
					t.Fatal("original request budget not propagated")
				}
			}
			if serveTest(h, httptest.NewRequest("GET", testPath("/skills"), nil).WithContext(ctx), w) || w.Code != 200 {
				t.Fatal("budget control")
			}
		})
	}
}
