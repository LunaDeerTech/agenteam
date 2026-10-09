package projectvariablehttp

import (
	"context"
	"fmt"
	"github.com/LunaDeerTech/agenteam/internal/central/account"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/httpapi"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/projectvariable/contract"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func testID[T any](n int) f.ID[T] {
	v, e := f.ParseID[T](fmt.Sprintf("01900000-0000-7000-8000-%012x", n))
	if e != nil {
		panic(e)
	}
	return v
}
func testAt() f.Instant {
	v, e := f.ParseInstant("2026-10-09T01:02:03.123456Z")
	if e != nil {
		panic(e)
	}
	return v
}
func testActor() id.Actor { a, _ := id.NewHuman(testID[id.User](1), testID[id.Session](2)); return a }
func testVariable() c.Variable {
	v, e := c.NewVariable(c.VariableFields{ID: testID[id.ProjectVariable](3), ProjectID: testID[id.Project](4), Type: "variable", Name: "NAME", Description: "description-canary", Value: "value-canary", Version: 1, CreatedAt: testAt(), UpdatedAt: testAt()})
	if e != nil {
		panic(e)
	}
	return v
}
func testPath(s string) string { return projectPrefix + testID[id.Project](4).String() + s }

type testBoundary struct {
	check, auth   func(*http.Request) error
	checks, auths atomic.Int32
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
		if e := b.auth(r); e != nil {
			return id.Actor{}, e
		}
	}
	return testActor(), nil
}
func (*testBoundary) WriteProblem(w http.ResponseWriter, r *http.Request, e error) {
	(&account.HTTPBoundary{}).WriteProblem(w, r, e)
}

// Private transport controls do not replace real authentication, SQL or root acceptance.
type testPorts struct {
	c.Commands
	calls      int
	before     func(context.Context)
	err        error
	value      c.Variable
	page       f.Page[c.VariableSummary]
	result     c.VariableMutation
	lookup     c.VariableCommandLookup
	gotQuery   f.PageRequest
	gotMeta    f.CommandMeta
	gotLookup  c.VariableCommandLookupRequest
	gotRequest any
	gotTarget  c.VariableID
}

func (p *testPorts) enter(ctx context.Context) {
	p.calls++
	if p.before != nil {
		p.before(ctx)
	}
}
func (p *testPorts) GetVariable(ctx context.Context, _ id.Actor, _ c.ProjectID, target c.VariableID) (c.Variable, error) {
	p.enter(ctx)
	p.gotTarget = target
	return p.value, p.err
}
func (p *testPorts) ListVariables(ctx context.Context, _ id.Actor, _ c.ProjectID, q f.PageRequest) (f.Page[c.VariableSummary], error) {
	p.enter(ctx)
	p.gotQuery = q
	return p.page, p.err
}
func (p *testPorts) CreateVariable(ctx context.Context, _ id.Actor, m f.CommandMeta, _ c.ProjectID, q c.VariableCreate) (c.VariableMutation, error) {
	p.enter(ctx)
	p.gotMeta = m
	p.gotRequest = q
	p.gotTarget = q.Fields().ID
	return p.result, p.err
}
func (p *testPorts) UpdateVariable(ctx context.Context, _ id.Actor, m f.CommandMeta, _ c.ProjectID, target c.VariableID, q c.VariableUpdate) (c.VariableMutation, error) {
	p.enter(ctx)
	p.gotMeta = m
	p.gotRequest = q
	p.gotTarget = target
	return p.result, p.err
}
func (p *testPorts) DeleteVariable(ctx context.Context, _ id.Actor, m f.CommandMeta, _ c.ProjectID, target c.VariableID) (c.VariableMutation, error) {
	p.enter(ctx)
	p.gotMeta = m
	p.gotTarget = target
	return p.result, p.err
}
func (p *testPorts) LookupVariableCommand(ctx context.Context, _ id.Actor, q c.VariableCommandLookupRequest) (c.VariableCommandLookup, error) {
	p.enter(ctx)
	p.gotLookup = q
	return p.lookup, p.err
}
func testHandler() (*handler, *testBoundary, *testPorts) {
	b := &testBoundary{}
	v := testVariable()
	p := &testPorts{value: v, page: f.Page[c.VariableSummary]{Items: []c.VariableSummary{v.Summary()}}}
	return &handler{p, b}, b, p
}
func commandRequest(method, path, body string) *http.Request {
	r := httptest.NewRequest(method, testPath(path), strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Idempotency-Key", "original-key")
	return r
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
