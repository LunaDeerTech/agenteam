package executionhttp

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/account"
	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/httpapi"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

func testID[T any](n int) f.ID[T] {
	v, err := f.ParseID[T](fmt.Sprintf("01900000-0000-7000-8000-%012x", n))
	if err != nil {
		panic(err)
	}
	return v
}
func testActor() i.Actor { v, _ := i.NewHuman(testID[i.User](1), testID[i.Session](2)); return v }
func testPath() string {
	return projectPrefix + testID[i.Project](3).String() + "/tasks/" + testID[struct{}](4).String() + "/executions"
}
func testExecution() ec.TaskExecution {
	at, _ := f.ParseInstant("2026-10-13T01:02:03.000001Z")
	return ec.TaskExecution{ID: testID[i.Execution](5), ProjectID: testID[i.Project](3), TaskID: testID[struct{}](4).String(), AgentID: testID[i.Agent](6), Purpose: "task/work", Status: ec.Created, Version: 1, CreatedAt: at}
}

type testBoundary struct {
	deny          error
	checks, auths int
	problem       error
}

func (b *testBoundary) CheckRequest(http.ResponseWriter, *http.Request) error {
	b.checks++
	return b.deny
}
func (b *testBoundary) RequireHuman(*http.Request) (i.Actor, error) {
	b.auths++
	return testActor(), nil
}
func (b *testBoundary) WriteProblem(w http.ResponseWriter, r *http.Request, err error) {
	b.problem = err
	(&account.HTTPBoundary{}).WriteProblem(w, r, err)
}

type testReader struct {
	runtime ec.TaskRuntimeView
	calls   int
	err     error
	page    f.PageRequest
	before  func(context.Context)
	get     ec.TaskExecution
	list    ec.TaskExecutionPage
}

func (p *testReader) GetRuntimeView(ctx context.Context, _ i.Actor, _ i.ProjectID, _ string, _ i.ExecutionID) (ec.TaskRuntimeView, error) {
	p.calls++
	if p.before != nil {
		p.before(ctx)
	}
	return p.runtime, p.err
}

func (p *testReader) List(ctx context.Context, _ i.Actor, _ i.ProjectID, _ string, page f.PageRequest) (ec.TaskExecutionPage, error) {
	p.calls++
	p.page = page
	if p.before != nil {
		p.before(ctx)
	}
	return p.list, p.err
}
func (p *testReader) Get(ctx context.Context, _ i.Actor, _ i.ProjectID, _ string, _ i.ExecutionID) (ec.TaskExecution, error) {
	p.calls++
	if p.before != nil {
		p.before(ctx)
	}
	return p.get, p.err
}

type testWriter struct{ *httptest.ResponseRecorder }

func (w *testWriter) SetReadDeadline(time.Time) error  { return nil }
func (w *testWriter) SetWriteDeadline(time.Time) error { return nil }
func (w *testWriter) FlushError() error                { return nil }
func runHTTP(h http.Handler, r *http.Request, w http.ResponseWriter) (aborted bool) {
	defer func() {
		if err := recover(); err != nil {
			if err != http.ErrAbortHandler {
				panic(err)
			}
			aborted = true
		}
	}()
	httpapi.Handler(nil, h).ServeHTTP(w, r)
	return false
}
func testHandler() (*handler, *testReader, *testBoundary) {
	p := &testReader{get: testExecution(), list: ec.TaskExecutionPage{Items: []ec.TaskExecution{testExecution()}}}
	b := &testBoundary{}
	return &handler{p, b}, p, b
}
func TestTaskExecutionHTTPListAndGet(t *testing.T) {
	for _, detail := range []bool{false, true} {
		for _, method := range []string{"GET", "HEAD"} {
			t.Run(fmt.Sprint(detail, "-", method), func(t *testing.T) {
				h, p, b := testHandler()
				path := testPath()
				if detail {
					path += "/" + testExecution().ID.String()
				}
				r := httptest.NewRequest(method, path, nil)
				w := &testWriter{httptest.NewRecorder()}
				if runHTTP(h, r, w) || w.Code != 200 || p.calls != 1 || b.checks != 1 || b.auths != 1 {
					t.Fatal(w.Code, b.problem)
				}
				if w.Header().Get("Cache-Control") != "no-store" {
					t.Fatal("cacheable")
				}
				if method == "HEAD" && w.Body.Len() != 0 {
					t.Fatal("HEAD body")
				}
				if method == "GET" && !detail && (!strings.Contains(w.Body.String(), `"next_cursor":null`) || !strings.Contains(w.Body.String(), `"reason":null`)) {
					t.Fatal("missing nullable wire")
				}
			})
		}
	}
}
func TestTaskExecutionHTTPRejectsInput(t *testing.T) {
	cases := []struct{ path, method, body string }{{"?limit=0", "GET", ""}, {"?limit=101", "GET", ""}, {"?limit=01", "GET", ""}, {"?limit=1&limit=2", "GET", ""}, {"?limit=1;cursor=x", "GET", ""}, {"?agent_id=x", "GET", ""}, {"?cursor=%00", "GET", ""}, {"?cursor=" + strings.Repeat("a", 8193), "GET", ""}, {"?", "GET", ""}, {"", "POST", ""}, {"", "GET", "x"}, {"/" + testExecution().ID.String() + "?limit=1", "GET", ""}}
	for n, c := range cases {
		t.Run(fmt.Sprint(n), func(t *testing.T) {
			h, p, b := testHandler()
			r := httptest.NewRequest(c.method, testPath()+c.path, strings.NewReader(c.body))
			w := &testWriter{httptest.NewRecorder()}
			if runHTTP(h, r, w) || b.problem == nil || p.calls != 0 {
				t.Fatal("invalid input reached service", b.problem, p.calls)
			}
		})
	}
	t.Run("boundary", func(t *testing.T) {
		h, p, b := testHandler()
		b.deny = f.NewFault(f.Forbidden, f.NotStarted)
		r := httptest.NewRequest("GET", testPath(), nil)
		w := &testWriter{httptest.NewRecorder()}
		if runHTTP(h, r, w) || p.calls != 0 || b.auths != 0 || w.Code != 403 {
			t.Fatal("boundary bypass")
		}
	})
}
func TestTaskExecutionHTTPRejectsOutput(t *testing.T) {
	mutations := []func(*testReader){func(p *testReader) { p.list.Items[0].ProjectID = testID[i.Project](91) }, func(p *testReader) { p.list.Items[0].TaskID = testID[struct{}](92).String() }, func(p *testReader) { p.list.Items[0].Purpose = "meeting/response" }, func(p *testReader) { p.list.Items = append(p.list.Items, p.list.Items[0]) }, func(p *testReader) { v := "private error"; p.list.Items[0].Reason = &v }, func(p *testReader) { p.list.Items[0].Status = ec.Succeeded }, func(p *testReader) { p.list.Items = nil }, func(p *testReader) { v := "x"; p.list.NextCursor = &v }}
	for n, mutate := range mutations {
		t.Run(fmt.Sprint(n), func(t *testing.T) {
			h, p, b := testHandler()
			mutate(p)
			w := &testWriter{httptest.NewRecorder()}
			if runHTTP(h, httptest.NewRequest("GET", testPath(), nil), w) || b.problem == nil || strings.Contains(w.Body.String(), `"items"`) {
				t.Fatal("unsafe output disclosed")
			}
		})
	}
}
func TestTaskExecutionHTTPOriginalReturn(t *testing.T) {
	h, p, _ := testHandler()
	entered, release, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var exited atomic.Bool
	p.before = func(ctx context.Context) {
		deadline, ok := ctx.Deadline()
		if !ok || time.Until(deadline) > requestBudget {
			panic("missing request budget")
		}
		close(entered)
		<-ctx.Done()
		<-release
		exited.Store(true)
	}
	ctx, cancel := context.WithCancel(context.Background())
	r := httptest.NewRequest("GET", testPath(), nil).WithContext(ctx)
	go func() {
		defer close(done)
		if !runHTTP(h, r, &testWriter{httptest.NewRecorder()}) {
			t.Error("cancelled response not aborted")
		}
	}()
	<-entered
	cancel()
	select {
	case <-done:
		t.Fatal("handler returned before original service")
	default:
	}
	close(release)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("handler did not return")
	}
	if !exited.Load() {
		t.Fatal("lost original return")
	}
}
