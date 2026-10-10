package contenthttp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
)

func testID[T any](n int) f.ID[T] {
	v, err := f.ParseID[T](fmt.Sprintf("01900000-0000-7000-8000-%012x", n))
	if err != nil {
		panic(err)
	}
	return v
}
func testActor() id.Actor { a, _ := id.NewHuman(testID[id.User](1), testID[id.Session](2)); return a }
func testPath() string {
	return projectPrefix + testID[id.Project](4).String() + "/knowledge/documents/" + testID[kc.Document](3).String() + "/content"
}
func testContent() kc.DocumentContent {
	creator, _ := kc.NewCreatorRef(kc.CreatorDetails{Kind: id.Human, UserID: testID[id.User](1)})
	at, _ := f.ParseInstant("2026-10-10T01:02:03Z")
	doc := kc.DocumentRef{ID: testID[kc.Document](3), ProjectID: testID[id.Project](4), Title: "current metadata", ContentVersion: 2, SourceKind: kc.Text, MediaType: kc.Markdown, ObjectID: testID[oc.StoredObject](99), Status: kc.Active, IndexingStatus: kc.IndexPending, CreatedBy: creator, CreatedAt: at, UpdatedAt: at}
	return kc.DocumentContent{Document: doc, Text: &kc.TextContent{Text: "文a", NextByteOffset: 4}}
}

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

// Local service control; actual current Owner/SQL/typed Object Close need the
// separate domain and integration tests, not this test double.
type testPorts struct {
	before   func(context.Context)
	err      error
	content  kc.DocumentContent
	calls    int
	actor    id.Actor
	project  id.ProjectID
	document kc.DocumentID
	request  kc.ReadRequest
}

func (p *testPorts) ReadDocument(ctx context.Context, a id.Actor, project id.ProjectID, document kc.DocumentID, request kc.ReadRequest) (kc.DocumentContent, error) {
	p.calls++
	p.actor, p.project, p.document, p.request = a, project, document, request
	if p.before != nil {
		p.before(ctx)
	}
	return p.content, p.err
}
func testHandler() (*handler, *testBoundary, *testPorts) {
	b, p := &testBoundary{}, &testPorts{content: testContent()}
	return &handler{p, b}, b, p
}

func TestContentHTTPGETAndHEADSameConsumption(t *testing.T) {
	var get []byte
	for _, method := range []string{"GET", "HEAD"} {
		h, _, p := testHandler()
		w := newTestWriter()
		if serveTest(h, httptest.NewRequest(method, testPath(), nil), w) || w.Code != 200 || p.calls != 1 || p.request != kc.DefaultReadRequest() || p.document != testID[kc.Document](3) || p.project != testID[id.Project](4) || p.actor.Details().SessionID != testActor().Details().SessionID {
			t.Fatal("GET/HEAD did not consume the original authorized request")
		}
		if method == "GET" {
			get = append([]byte(nil), w.Body.Bytes()...)
		} else if w.Body.Len() != 0 {
			t.Fatal("HEAD exposed body")
		}
		if w.Header().Get("Content-Length") != strconv.Itoa(len(get)) || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("representation headers changed")
		}
		w.cleared(t)
	}
	var body map[string]json.RawMessage
	if json.Unmarshal(get, &body) != nil || len(body) != 2 || strings.Contains(string(get), testID[oc.StoredObject](99).String()) || strings.Contains(string(get), "object_id") {
		t.Fatal("unsafe content representation")
	}
}

func TestContentHTTPBoundariesAndOriginalFaults(t *testing.T) {
	if _, err := NewHTTPHandler(nil, &account.HTTPBoundary{}); err == nil {
		t.Fatal("nil service bound")
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
		if serveTest(h, httptest.NewRequest("POST", testPath()+"?wrong=1", nil), w) || w.Code != 401 || p.calls != 0 {
			t.Fatal("syntax/method bypassed authentication")
		}
	}
	for _, method := range []string{"GET", "HEAD"} {
		for _, code := range []f.Code{f.ResourceDeleted, f.NotFound, f.SessionRevoked, f.ProjectNotActive, f.CommitUnknown, f.DependencyUnavailable} {
			h, b, p := testHandler()
			fault := f.NewFault(code, f.NotStarted).WithCause(errors.New("private cause canary"))
			if code == f.CommitUnknown {
				fault = f.NewFault(code, f.Unknown)
			}
			p.err = fault
			w := newTestWriter()
			if serveTest(h, httptest.NewRequest(method, testPath(), nil), w) || b.problem != fault || p.calls != 1 {
				t.Fatal("original service error replaced")
			}
			if code == f.ResourceDeleted && w.Code != 410 || code == f.NotFound && w.Code != 404 {
				t.Fatal("deleted/absence classification lost")
			}
			if method == "HEAD" && w.Body.Len() != 0 || strings.Contains(w.Body.String(), "private cause") || strings.Contains(w.Body.String(), "current metadata") {
				t.Fatal("error emitted a body or content candidate")
			}
		}
	}
	for _, kind := range []string{"post", "body", "range", "encoding", "query", "target", "route"} {
		h, _, p := testHandler()
		r := httptest.NewRequest("GET", testPath(), nil)
		want := 400
		switch kind {
		case "post":
			r.Method = "POST"
			want = 405
		case "body":
			r.Body = io.NopCloser(strings.NewReader("x"))
		case "range":
			r.Header.Set("Range", "bytes=0-2")
		case "encoding":
			r.Header.Set("Content-Encoding", "identity")
		case "query":
			r.URL.RawQuery = "byte_offset=0&byte_offset=1"
		case "target":
			r.URL.Path = strings.Replace(r.URL.Path, testID[kc.Document](3).String(), "not-id", 1)
		case "route":
			r.URL.Path += "/"
			want = 404
		}
		w := newTestWriter()
		if serveTest(h, r, w) || w.Code != want || p.calls != 0 {
			t.Fatal("invalid read reached service", kind, w.Code)
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
		t.Fatal("normal completed response retained deadlines")
	}
}
func (w *testWriter) notCleared(t *testing.T) {
	t.Helper()
	w.mu.Lock()
	defer w.mu.Unlock()
	for _, ts := range append(append([]time.Time(nil), w.reads...), w.writes...) {
		if ts.IsZero() {
			t.Fatal("failed response made connection reusable")
		}
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
