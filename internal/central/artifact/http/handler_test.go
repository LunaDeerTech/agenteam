package artifacthttp

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
	"github.com/LunaDeerTech/agenteam/internal/central/artifact"
	ac "github.com/LunaDeerTech/agenteam/internal/central/artifact/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/httpapi"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
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
func testReference() ac.ArtifactRef {
	return ac.ArtifactRef{ProjectID: testID[id.Project](3), ArtifactID: testID[ac.ArtifactEntity](4), FileID: testID[ac.File](5)}
}
func testPath(read bool) string {
	r := testReference()
	path := projectPrefix + r.ProjectID.String() + "/artifacts"
	if read {
		path += "/" + r.ArtifactID.String() + "/files/" + r.FileID.String() + "/content"
	}
	return path
}
func testMetadata() ac.Metadata {
	at, _ := f.ParseInstant("2026-10-13T01:02:03Z")
	scope, _ := id.InProject(testReference().ProjectID)
	digest, _ := f.ParseDigest("sha256:" + strings.Repeat("a", 64))
	m, err := ac.NewMetadata(ac.MetadataDetails{Reference: testReference(), Kind: ac.Generated, Name: "bounded 文.txt", Description: "safe metadata", Object: oc.ObjectMeta{ID: testID[oc.StoredObject](999), Scope: scope, MediaType: "text/plain", ByteSize: 4, SHA256: digest, State: oc.Available, Version: 2, CreatedAt: at}, CreatedByUserID: testID[id.User](1).String(), CreatedAt: at, Version: 3})
	if err != nil {
		panic(err)
	}
	return m
}
func testRead() ac.ReadResult {
	r, err := ac.NewReadResult(ac.ReadDetails{Artifact: testMetadata(), Projection: ac.TextProjection, Text: "文a", NextOffset: 4})
	if err != nil {
		panic(err)
	}
	return r
}

// Local boundary/service controls test this adapter, not current Owner SQL or
// typed Object Close; those are exercised by the real default-App integration.
type testBoundary struct {
	check, auth   error
	checks, auths int
	problem       error
}

func (b *testBoundary) CheckRequest(http.ResponseWriter, *http.Request) error {
	b.checks++
	return b.check
}
func (b *testBoundary) RequireHuman(*http.Request) (id.Actor, error) {
	b.auths++
	return testActor(), b.auth
}
func (b *testBoundary) WriteProblem(w http.ResponseWriter, r *http.Request, err error) {
	b.problem = err
	(&account.HTTPBoundary{}).WriteProblem(w, r, err)
}

type testReader struct {
	before      func(context.Context)
	err         error
	result      ac.ReadResult
	invocations []ac.Invocation
	filter      ac.ListFilter
	page        f.PageRequest
	reference   ac.ArtifactRef
	preview     ac.PreviewRequest
}

func (p *testReader) ListArtifacts(ctx context.Context, v ac.Invocation, filter ac.ListFilter, page f.PageRequest) (artifact.Page, error) {
	p.invocations = append(p.invocations, v)
	p.filter, p.page = filter, page
	if p.before != nil {
		p.before(ctx)
	}
	return artifact.Page{}, p.err
}
func (p *testReader) ReadArtifact(ctx context.Context, v ac.Invocation, ref ac.ArtifactRef, preview ac.PreviewRequest) (ac.ReadResult, error) {
	p.invocations = append(p.invocations, v)
	p.reference, p.preview = ref, preview
	if p.before != nil {
		p.before(ctx)
	}
	return p.result, p.err
}
func testHandler() (*handler, *testBoundary, *testReader) {
	b, p := &testBoundary{}, &testReader{result: testRead()}
	return &handler{p, b}, b, p
}

type testWriter struct {
	*httptest.ResponseRecorder
	mu            sync.Mutex
	reads, writes []time.Time
	setRead       func(time.Time) error
	write         func([]byte) (int, error)
	flush         func() error
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
func (w *testWriter) cleared(t *testing.T, expected bool) {
	t.Helper()
	w.mu.Lock()
	defer w.mu.Unlock()
	for _, values := range [][]time.Time{w.reads, w.writes} {
		if expected && (len(values) == 0 || !values[len(values)-1].IsZero()) {
			t.Fatal("normal response retained deadline")
		}
		if !expected {
			for _, at := range values {
				if at.IsZero() {
					t.Fatal("failed response became reusable")
				}
			}
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

func TestArtifactHTTPReadGETHEADAndAttempts(t *testing.T) {
	h, _, p := testHandler()
	var get []byte
	for _, method := range []string{"GET", "HEAD"} {
		w := newTestWriter()
		r := httptest.NewRequest(method, testPath(true), nil)
		r.Header.Set("X-Request-ID", testID[f.Request](90).String())
		if serveTest(h, r, w) || w.Code != 200 {
			t.Fatal("read failed", w.Code)
		}
		if method == "GET" {
			get = append([]byte(nil), w.Body.Bytes()...)
		} else if w.Body.Len() != 0 {
			t.Fatal("HEAD body")
		}
		if w.Header().Get("Content-Length") != strconv.Itoa(len(get)) || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("read headers")
		}
		w.cleared(t, true)
	}
	if len(p.invocations) != 2 || p.reference != testReference() || p.preview != (ac.PreviewRequest{Limit: 8192}) {
		t.Fatal("original reference/request changed")
	}
	a, b := p.invocations[0].Details(), p.invocations[1].Details()
	if a.AttemptID == b.AttemptID || a.AttemptID.String() == testID[f.Request](90).String() || a.Actor.Details().SessionID != testActor().Details().SessionID || a.ProjectID != testReference().ProjectID || a.ExecutionID != "" || a.OperationID != "" || a.ToolID != "" || a.ToolCallID != "" {
		t.Fatal("untrusted or reused invocation identity")
	}
	var body map[string]json.RawMessage
	if json.Unmarshal(get, &body) != nil || len(body) != 6 || strings.Contains(string(get), testID[oc.StoredObject](999).String()) || strings.Contains(string(get), "object_id") {
		t.Fatal("unsafe projection")
	}
}

func TestArtifactHTTPListAndStrictIngress(t *testing.T) {
	if _, err := NewHTTPHandler(nil, &account.HTTPBoundary{}); err == nil {
		t.Fatal("nil service bound")
	}
	for _, read := range []bool{false, true} {
		if !HandlesPath(testPath(read)) || HandlesPath(testPath(read)+"/") {
			t.Fatal("route recognition")
		}
	}
	h, _, p := testHandler()
	w := newTestWriter()
	if serveTest(h, httptest.NewRequest("GET", testPath(false)+"?kind=generated&name_query=%25_", nil), w) || w.Code != 200 || p.page.Limit != 50 || p.filter.NameQuery != "%_" || w.Body.String() != `{"items":[]}` {
		t.Fatal("list adapter")
	}
	for _, mode := range []string{"method", "body", "unannounced-body", "range", "if-range", "encoding", "duplicate", "path", "file", "trace", "route"} {
		t.Run(mode, func(t *testing.T) {
			h, _, p := testHandler()
			r := httptest.NewRequest("GET", testPath(true), nil)
			want := 400
			switch mode {
			case "method":
				r.Method = "POST"
				want = 405
			case "body":
				r = httptest.NewRequest("GET", testPath(true), strings.NewReader("x"))
			case "unannounced-body":
				r.Body = io.NopCloser(strings.NewReader("x"))
			case "range":
				r.Header.Set("Range", "bytes=0-1")
			case "if-range":
				r.Header.Set("If-Range", "opaque")
			case "encoding":
				r.Header.Set("Content-Encoding", "identity")
			case "duplicate":
				r.URL.RawQuery = "limit=1&%6cimit=2"
			case "path":
				r.URL.Path = strings.Replace(r.URL.Path, testReference().ProjectID.String(), "bad", 1)
			case "file":
				r.URL.Path = strings.Replace(r.URL.Path, testReference().FileID.String(), "bad", 1)
			case "trace": // Calling without the formal outer middleware supplies no trusted trace.
			case "route":
				r.URL.Path += "/"
				want = 404
			}
			w := newTestWriter()
			var aborted bool
			if mode == "trace" {
				aborted = testAbort(func() { h.ServeHTTP(w, r) })
			} else {
				aborted = serveTest(h, r, w)
			}
			if aborted || w.Code != want || len(p.invocations) != 0 {
				t.Fatal("invalid input reached service", w.Code)
			}
			if want == 405 && w.Header().Get("Allow") != "GET, HEAD" {
				t.Fatal("missing Allow")
			}
		})
	}
}

func TestArtifactHTTPAuthorityAndOriginalFaults(t *testing.T) {
	for _, stage := range []string{"boundary", "session", "service", "projection"} {
		for _, method := range []string{"GET", "HEAD"} {
			t.Run(stage+"-"+method, func(t *testing.T) {
				h, b, p := testHandler()
				cause := errors.New("private-cause-canary")
				original := f.NewFault(f.CommitUnknown, f.Unknown).WithCause(cause)
				want := 503
				switch stage {
				case "boundary":
					b.check = f.NewFault(f.OriginDenied, f.NotStarted)
					want = 403
				case "session":
					b.auth = f.NewFault(f.SessionRevoked, f.NotStarted)
					want = 401
				case "service":
					p.err = original
				case "projection":
					p.result = ac.ReadResult{}
				}
				w := newTestWriter()
				if serveTest(h, httptest.NewRequest(method, testPath(true)+"?limit=1", nil), w) || w.Code != want {
					t.Fatal("fault projection", w.Code)
				}
				if (stage == "boundary" || stage == "session") && len(p.invocations) != 0 {
					t.Fatal("authority denial reached service")
				}
				if stage == "service" && !errors.Is(b.problem, cause) {
					t.Fatal("original cause lost")
				}
				if stage == "projection" {
					var fault *f.Fault
					if !errors.As(b.problem, &fault) || fault.CommitState != f.Committed {
						t.Fatal("successful read Audit outcome lost")
					}
				}
				if method == "HEAD" && w.Body.Len() != 0 {
					t.Fatal("HEAD Problem body")
				}
				if strings.Contains(w.Body.String(), "private-cause-canary") || strings.Contains(w.Body.String(), "?limit") {
					t.Fatal("private diagnostic leak")
				}
			})
		}
	}
}
