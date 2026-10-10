package knowledgehttp

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/account"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/httpapi"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	kc "github.com/LunaDeerTech/agenteam/internal/central/knowledge/contract"
)

func testActor() id.Actor      { a, _ := id.NewHuman(testID[id.User](1), testID[id.Session](2)); return a }
func testPath(s string) string { return projectPrefix + testID[id.Project](4).String() + s }

type testBoundary struct {
	check, auth   func(*http.Request) error
	checks, auths atomic.Int32
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
	return testActor(), nil
}
func (b *testBoundary) WriteProblem(w http.ResponseWriter, r *http.Request, err error) {
	b.problem = err
	(&account.HTTPBoundary{}).WriteProblem(w, r, err)
}

// Explicit private HTTP controls never certify Account, current Owner or SQL.
type testPorts struct {
	before    func(context.Context)
	err       error
	calls     int
	method    string
	actor     id.Actor
	project   id.ProjectID
	target    kc.DocumentID
	parent    *kc.DocumentID
	filter    kc.ListFilter
	query     f.PageRequest
	head      kc.DocumentHead
	page      f.Page[kc.DocumentRef]
	ancestors []kc.DocumentRef
	hits      f.Page[kc.TitleHit]
}

func (p *testPorts) enter(ctx context.Context, actor id.Actor, project id.ProjectID, method string) {
	p.calls++
	p.actor, p.project, p.method = actor, project, method
	if p.before != nil {
		p.before(ctx)
	}
}
func (p *testPorts) GetDocument(ctx context.Context, a id.Actor, project id.ProjectID, target kc.DocumentID) (kc.DocumentHead, error) {
	p.enter(ctx, a, project, "get")
	p.target = target
	return p.head, p.err
}
func (p *testPorts) ListDocuments(ctx context.Context, a id.Actor, project id.ProjectID, filter kc.ListFilter, q f.PageRequest) (f.Page[kc.DocumentRef], error) {
	p.enter(ctx, a, project, "list")
	p.filter, p.query = filter, q
	return p.page, p.err
}
func (p *testPorts) ListChildren(ctx context.Context, a id.Actor, project id.ProjectID, parent *kc.DocumentID, filter kc.ListFilter, q f.PageRequest) (f.Page[kc.DocumentRef], error) {
	p.enter(ctx, a, project, "children")
	p.parent, p.filter, p.query = parent, filter, q
	return p.page, p.err
}
func (p *testPorts) ReadAncestors(ctx context.Context, a id.Actor, project id.ProjectID, target kc.DocumentID) ([]kc.DocumentRef, error) {
	p.enter(ctx, a, project, "ancestors")
	p.target = target
	return p.ancestors, p.err
}
func (p *testPorts) SearchTitles(ctx context.Context, a id.Actor, project id.ProjectID, title string, q f.PageRequest) (f.Page[kc.TitleHit], error) {
	p.enter(ctx, a, project, "search")
	p.filter, p.query = kc.ListFilter{TitleQuery: title}, q
	return p.hits, p.err
}
func testHandler() (*handler, *testBoundary, *testPorts) {
	d := testDocument()
	p := &testPorts{head: kc.DocumentHead{Active: &d}, page: f.Page[kc.DocumentRef]{Items: []kc.DocumentRef{d}}, ancestors: []kc.DocumentRef{}, hits: f.Page[kc.TitleHit]{Items: []kc.TitleHit{{Document: d, Ancestors: []kc.DocumentRef{}}}}}
	b := &testBoundary{}
	return &handler{p, b}, b, p
}

func TestKnowledgeHTTPReadRoutesAndHEAD(t *testing.T) {
	d := testDocument()
	for _, tc := range []struct{ path, method string }{
		{"/knowledge/documents", "list"}, {"/knowledge/documents/" + d.ID.String(), "get"}, {"/knowledge/documents/children?parent_document_id=null", "children"}, {"/knowledge/documents/" + d.ID.String() + "/ancestors", "ancestors"}, {"/knowledge/documents/search-titles?title_query=literal", "search"},
	} {
		t.Run(tc.method, func(t *testing.T) {
			var length int
			for _, method := range []string{"GET", "HEAD"} {
				h, b, p := testHandler()
				w := newTestWriter()
				r := httptest.NewRequest(method, testPath(tc.path), nil)
				if serveTest(h, r, w) || w.Code != 200 || b.checks.Load() != 1 || b.auths.Load() != 1 || p.calls != 1 || p.method != tc.method || !p.actor.Equal(testActor()) || p.project != d.ProjectID {
					t.Fatal("read dispatch", method, w.Code, p.method, w.Body.String())
				}
				if w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("Content-Type") != "application/json" {
					t.Fatal("safe response headers")
				}
				n, err := strconv.Atoi(w.Header().Get("Content-Length"))
				if err != nil || n <= 0 {
					t.Fatal("complete representation length")
				}
				if method == "GET" {
					length = n
					if w.Body.Len() != n {
						t.Fatal("body length")
					}
				} else if n != length || w.Body.Len() != 0 {
					t.Fatal("HEAD did not perform same projection")
				}
				if strings.Contains(w.Body.String(), d.ObjectID.String()) || strings.Contains(w.Body.String(), "object_id") {
					t.Fatal("hidden Object escaped")
				}
				w.cleared(t)
			}
		})
	}
}
func TestKnowledgeHTTPRejectsBeforeDomainAndPreservesFault(t *testing.T) {
	for _, tc := range []struct {
		method, path, body string
		status             int
	}{
		{"POST", "/knowledge/documents", "", 405}, {"GET", "/knowledge/documents/", "", 404}, {"GET", "/knowledge/documents?raw-secret=private-canary", "", 400}, {"GET", "/knowledge/documents/not-an-id", "", 400}, {"GET", "/knowledge/documents/children", "", 400}, {"GET", "/knowledge/documents", "x", 400},
	} {
		h, _, p := testHandler()
		w := newTestWriter()
		r := httptest.NewRequest(tc.method, testPath(tc.path), strings.NewReader(tc.body))
		if serveTest(h, r, w) || w.Code != tc.status || p.calls != 0 {
			t.Fatal("invalid input dispatch", tc, w.Code)
		}
		if strings.Contains(w.Body.String(), "private-canary") || strings.Contains(w.Body.String(), "raw-secret") {
			t.Fatal("input leaked in Problem")
		}
		if tc.status == 405 && w.Header().Get("Allow") != "GET, HEAD" {
			t.Fatal("method allow")
		}
	}
	for _, code := range []f.Code{f.SessionRevoked, f.NotFound, f.ProjectNotActive, f.DependencyUnbound, f.VersionConflict, f.CommitUnknown} {
		h, b, p := testHandler()
		fault := f.NewFault(code, f.NotStarted).WithCause(errors.New("private SQL canary"))
		if code == f.CommitUnknown {
			fault = f.NewFault(code, f.Unknown)
			fault.CauseID = testID[f.TransactionAttempt](41).String()
		}
		p.err = fault
		w := newTestWriter()
		r := httptest.NewRequest("GET", testPath("/knowledge/documents"), nil)
		if serveTest(h, r, w) {
			t.Fatal("known fault aborted")
		}
		body := wireObject(t, w.Body.Bytes())
		if body["code"] != string(code) || strings.Contains(w.Body.String(), "private SQL") || strings.Contains(w.Body.String(), "items") {
			t.Fatal("fault or candidate leaked", code, w.Body.String())
		}
		if b.problem != fault {
			t.Fatal("original domain Fault was replaced before the Account boundary")
		}
		if code == f.CommitUnknown && (body["commit_state"] != "unknown" || body["cause_id"] != nil || strings.Contains(w.Body.String(), fault.CauseID)) {
			t.Fatal("Unknown identity lost", w.Body.String())
		}
	}
}
func TestKnowledgeHTTPBoundInstancesAndAuthentication(t *testing.T) {
	if _, err := NewHTTPHandler(nil, &account.HTTPBoundary{}); err == nil {
		t.Fatal("nil domain bound")
	}
	for _, stage := range []string{"boundary", "human"} {
		h, b, p := testHandler()
		deny := func(*http.Request) error { return f.NewFault(f.Unauthenticated, f.NotStarted) }
		if stage == "boundary" {
			b.check = deny
		} else {
			b.auth = deny
		}
		w := newTestWriter()
		if serveTest(h, httptest.NewRequest("GET", testPath("/knowledge/documents"), nil), w) || w.Code != 401 || p.calls != 0 {
			t.Fatal("auth bypass", stage)
		}
	}
}

type testWriter struct {
	*httptest.ResponseRecorder
	mu                sync.Mutex
	reads, writes     []time.Time
	setRead, setWrite func(time.Time) error
	write             func([]byte) (int, error)
	flush             func() error
}

func newTestWriter() *testWriter { return &testWriter{ResponseRecorder: httptest.NewRecorder()} }
func (w *testWriter) SetReadDeadline(at time.Time) error {
	w.mu.Lock()
	w.reads = append(w.reads, at)
	w.mu.Unlock()
	if w.setRead != nil {
		return w.setRead(at)
	}
	return nil
}
func (w *testWriter) SetWriteDeadline(at time.Time) error {
	w.mu.Lock()
	w.writes = append(w.writes, at)
	w.mu.Unlock()
	if w.setWrite != nil {
		return w.setWrite(at)
	}
	return nil
}
func (w *testWriter) Write(p []byte) (int, error) {
	if w.write != nil {
		return w.write(p)
	}
	return w.ResponseRecorder.Write(p)
}
func (w *testWriter) FlushError() error {
	if w.flush != nil {
		return w.flush()
	}
	return nil
}
func (w *testWriter) cleared(t *testing.T) {
	t.Helper()
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.reads) == 0 || len(w.writes) == 0 || !w.reads[len(w.reads)-1].IsZero() || !w.writes[len(w.writes)-1].IsZero() {
		t.Fatal("native deadlines retained")
	}
}

type testBody struct {
	io.Reader
	closes atomic.Int32
	close  func() error
}

func (b *testBody) Close() error {
	b.closes.Add(1)
	if b.close != nil {
		return b.close()
	}
	return nil
}
func testAbort(run func()) (aborted bool) {
	defer func() {
		if v := recover(); v != nil {
			if v != http.ErrAbortHandler {
				panic(v)
			}
			aborted = true
		}
	}()
	run()
	return false
}
func serveTest(h http.Handler, r *http.Request, w http.ResponseWriter) bool {
	return testAbort(func() { httpapi.Handler(nil, h).ServeHTTP(w, r) })
}
