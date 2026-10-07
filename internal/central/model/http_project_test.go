package model

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/account"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/httpapi"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
)

const projectHTTPTestProject = "01900000-0000-7000-8000-000000000010"
const projectHTTPTestProvider = "01900000-0000-7000-8000-000000000011"
const projectHTTPTestModel = "01900000-0000-7000-8000-000000000012"
const projectHTTPTestBase = "/api/v1/projects/" + projectHTTPTestProject

type projectHTTPTestBoundary struct {
	actor         id.Actor
	check         func(http.ResponseWriter, *http.Request) error
	auth          func(*http.Request) (id.Actor, error)
	problem       func(http.ResponseWriter, *http.Request, error)
	checks, auths atomic.Int32
}

func (b *projectHTTPTestBoundary) CheckRequest(w http.ResponseWriter, r *http.Request) error {
	b.checks.Add(1)
	if b.check != nil {
		return b.check(w, r)
	}
	return nil
}
func (b *projectHTTPTestBoundary) RequireHuman(r *http.Request) (id.Actor, error) {
	b.auths.Add(1)
	if b.auth != nil {
		return b.auth(r)
	}
	return b.actor, nil
}
func (b *projectHTTPTestBoundary) WriteProblem(w http.ResponseWriter, r *http.Request, e error) {
	if b.problem != nil {
		b.problem(w, r, e)
		return
	}
	(&account.HTTPBoundary{}).WriteProblem(w, r, e)
}

type projectHTTPTestReader struct {
	call      func(context.Context, id.Actor, id.ProjectID, ProjectQuery, projectHTTPKind, string) error
	providers ProviderPage
	provider  mc.ProviderView
	models    ModelPage
	model     mc.ModelView
	available AvailableChatModelPage
	calls     atomic.Int32
}

func (s *projectHTTPTestReader) invoke(c context.Context, a id.Actor, p id.ProjectID, q ProjectQuery, k projectHTTPKind, target string) error {
	s.calls.Add(1)
	if s.call != nil {
		return s.call(c, a, p, q, k, target)
	}
	return nil
}
func (s *projectHTTPTestReader) ListProjectProviders(c context.Context, a id.Actor, p id.ProjectID, q ProjectQuery) (ProviderPage, error) {
	return s.providers, s.invoke(c, a, p, q, projectHTTPProviders, "")
}
func (s *projectHTTPTestReader) GetProjectProvider(c context.Context, a id.Actor, p id.ProjectID, k mc.ProviderID) (mc.ProviderView, error) {
	return s.provider, s.invoke(c, a, p, ProjectQuery{}, projectHTTPProvider, k.String())
}
func (s *projectHTTPTestReader) ListProjectModels(c context.Context, a id.Actor, p id.ProjectID, q ProjectQuery) (ModelPage, error) {
	return s.models, s.invoke(c, a, p, q, projectHTTPModels, "")
}
func (s *projectHTTPTestReader) GetProjectModel(c context.Context, a id.Actor, p id.ProjectID, k mc.ModelID) (mc.ModelView, error) {
	return s.model, s.invoke(c, a, p, ProjectQuery{}, projectHTTPModel, k.String())
}
func (s *projectHTTPTestReader) ListAvailableChatModels(c context.Context, a id.Actor, p id.ProjectID, q ProjectQuery) (AvailableChatModelPage, error) {
	return s.available, s.invoke(c, a, p, q, projectHTTPAvailable, "")
}
func projectHTTPTestHandler(t *testing.T) (*projectHTTP, *projectHTTPTestBoundary, *projectHTTPTestReader) {
	t.Helper()
	b := &projectHTTPTestBoundary{actor: testActor(t)}
	s := &projectHTTPTestReader{}
	return &projectHTTP{s, b}, b, s
}
func projectHTTPRequest(method, path string) *http.Request {
	return httptest.NewRequest(method, "http://project.example"+path, nil)
}

func TestProjectModelHTTPPureConstruction(t *testing.T) {
	store := &noIOStore{}
	auth := pureAuthority(t, store)
	core, err := New(store, auth, testDependencies(t))
	if err != nil {
		t.Fatal(err)
	}
	for _, x := range []*Service{nil, {}, core} {
		if h, e := NewProjectHTTPHandler(x, &account.HTTPBoundary{}); e == nil || h != nil {
			t.Fatal("unbound Project authority accepted")
		}
	}
	auth.state().auth.Projects = projectGrantChannel(nil)
	if h, e := NewProjectHTTPHandler(core, nil); e == nil || h != nil {
		t.Fatal("nil Account boundary accepted")
	}
	// A non-nil channel has a real method that panics if construction does I/O.
	auth.state().auth.Projects = make(projectGrantChannel)
	if h, e := NewProjectHTTPHandler(core, &account.HTTPBoundary{}); e != nil || h == nil {
		t.Fatal("pure bound constructor", e)
	}
	foreign := &noIOStore{}
	other, _ := New(foreign, pureAuthority(t, foreign), testDependencies(t))
	other.state().authority = auth
	if h, e := NewProjectHTTPHandler(other, &account.HTTPBoundary{}); e == nil || h != nil {
		t.Fatal("foreign Store authority accepted")
	}
}
func TestProjectModelHTTPPureStrictDispatch(t *testing.T) {
	for _, tc := range []struct {
		name, method, path string
		change             func(*http.Request)
		status, calls      int
	}{
		{"providers", "GET", projectHTTPTestBase + "/model-providers", nil, 200, 1},
		{"models_head", "HEAD", projectHTTPTestBase + "/models", nil, 200, 1},
		{"directory", "GET", projectHTTPTestBase + "/available-chat-models?limit=1&cursor=opaque", nil, 200, 1},
		{"bad_project", "GET", "/api/v1/projects/bad/models", nil, 400, 0},
		{"bad_provider", "GET", projectHTTPTestBase + "/model-providers/bad", nil, 400, 0},
		{"bad_model", "GET", projectHTTPTestBase + "/models/bad", nil, 400, 0},
		{"unknown", "GET", projectHTTPTestBase + "/models/a/b", nil, 404, 0},
		{"method", "PUT", projectHTTPTestBase + "/models", nil, 405, 0},
		{"duplicate_decoded", "GET", projectHTTPTestBase + "/models?limit=1&%6cimit=2", nil, 400, 0},
		{"provider_filter", "GET", projectHTTPTestBase + "/models?provider_id=x", nil, 400, 0},
		{"detail_query", "HEAD", projectHTTPTestBase + "/models/" + projectHTTPTestModel + "?", nil, 400, 0},
		{"length", "GET", projectHTTPTestBase + "/models", func(r *http.Request) { r.ContentLength = 1 }, 400, 0},
		{"unknown_length", "GET", projectHTTPTestBase + "/models", func(r *http.Request) { r.ContentLength = -1 }, 400, 0},
		{"chunked", "GET", projectHTTPTestBase + "/models", func(r *http.Request) { r.TransferEncoding = []string{"chunked"} }, 400, 0},
		{"hidden_body", "GET", projectHTTPTestBase + "/models", func(r *http.Request) { r.Body = io.NopCloser(strings.NewReader("x")) }, 400, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, b, s := projectHTTPTestHandler(t)
			r := projectHTTPRequest(tc.method, tc.path)
			if tc.change != nil {
				tc.change(r)
			}
			w := summaryWriter()
			h.ServeHTTP(w, r)
			if w.Code != tc.status || int(s.calls.Load()) != tc.calls || b.checks.Load() != 1 {
				t.Fatalf("status=%d calls=%d checks=%d", w.Code, s.calls.Load(), b.checks.Load())
			}
			if tc.method == "HEAD" && w.Body.Len() != 0 {
				t.Fatal("HEAD body")
			}
			if tc.status == 405 && (w.Header().Get("Allow") != "GET, HEAD" || b.auths.Load() != 0) {
				t.Fatal("method boundary")
			}
			if w.flushes != 1 {
				t.Fatal("Flush missing")
			}
			w.cleared(t)
		})
	}
}
func TestProjectModelHTTPPureBoundaryAndOriginalError(t *testing.T) {
	for _, phase := range []string{"check", "auth", "service"} {
		t.Run(phase, func(t *testing.T) {
			h, b, s := projectHTTPTestHandler(t)
			original := f.NewFault(f.CommitUnknown, f.Unknown)
			var got error
			b.problem = func(w http.ResponseWriter, r *http.Request, e error) { got = e; httpapi.WriteProblem(w, r, e) }
			if phase == "check" {
				b.check = func(w http.ResponseWriter, r *http.Request) error {
					if d, ok := r.Context().Deadline(); !ok || time.Until(d) > projectHTTPReadBudget {
						t.Error("budget not before boundary")
					}
					return original
				}
			} else if phase == "auth" {
				b.auth = func(*http.Request) (id.Actor, error) { return id.Actor{}, original }
			} else {
				s.call = func(context.Context, id.Actor, id.ProjectID, ProjectQuery, projectHTTPKind, string) error {
					return original
				}
				s.available.Items = make([]AvailableChatModel, 101)
			}
			w := summaryWriter()
			h.ServeHTTP(w, projectHTTPRequest("GET", projectHTTPTestBase+"/available-chat-models"))
			if got != original || w.Code != 503 || !strings.Contains(w.Body.String(), `"commit_state":"unknown"`) {
				t.Fatal("original error replaced", got, w.Code)
			}
			if phase == "check" && b.auths.Load() != 0 {
				t.Fatal("auth after failed CheckRequest")
			}
			if phase != "service" && s.calls.Load() != 0 {
				t.Fatal("query after boundary error")
			}
		})
	}
}
func TestProjectModelHTTPPureMiddlewareProblemOwnership(t *testing.T) {
	for _, committed := range []bool{false, true} {
		t.Run(map[bool]string{false: "problem", true: "already_committed"}[committed], func(t *testing.T) {
			h, b, _ := projectHTTPTestHandler(t)
			b.check = func(w http.ResponseWriter, r *http.Request) error {
				if committed {
					w.WriteHeader(202)
					_, _ = w.Write([]byte("prefix"))
				}
				return fault(f.Forbidden)
			}
			var logs bytes.Buffer
			w := summaryWriter()
			aborted := summaryAborts(func() {
				httpapi.Handler(slog.New(slog.NewJSONHandler(&logs, nil)), h).ServeHTTP(w, projectHTTPRequest("GET", projectHTTPTestBase+"/models?cursor=sensitive_cursor"))
			})
			if committed {
				if !aborted || w.Body.String() != "prefix" || w.Code != 202 {
					t.Fatal("committed response got second Problem")
				}
			} else {
				if aborted || w.Code != 403 || !strings.Contains(logs.String(), `"code":"FORBIDDEN"`) || !strings.Contains(w.Body.String(), w.Header().Get("X-Request-ID")) {
					t.Fatal("middleware lost code or RequestID", logs.String())
				}
			}
			if strings.Contains(logs.String(), "sensitive_cursor") || strings.Contains(logs.String(), projectHTTPTestProject) {
				t.Fatal("unsafe raw target logged")
			}
			w.cleared(t)
		})
	}
}
func TestProjectModelHTTPPureDeadlineSetterPanics(t *testing.T) {
	for _, phase := range []string{"start_read", "start_write", "cancel_read", "cancel_write", "finish_abort_read", "finish_abort_write", "reset_read", "reset_write"} {
		t.Run(phase, func(t *testing.T) {
			h, _, s := projectHTTPTestHandler(t)
			w := summaryWriter()
			body := &summaryHTTPBody{Reader: strings.NewReader("")}
			r := projectHTTPRequest("GET", projectHTTPTestBase+"/models")
			r.Body = body
			ctx, cancel := context.WithCancel(r.Context())
			defer cancel()
			r = r.WithContext(ctx)
			var reads, writes atomic.Int32
			entered := make(chan struct{})
			release := make(chan struct{})
			var once atomic.Bool
			setter := func(which string, counter *atomic.Int32) func(time.Time) error {
				return func(at time.Time) error {
					n := counter.Add(1)
					if phase == "start_"+which && n == 1 {
						panic("private setter")
					}
					if phase == "cancel_"+which && n == 2 && once.CompareAndSwap(false, true) {
						close(entered)
						<-release
						panic("private callback")
					}
					if phase == "finish_abort_"+which && n == 2 {
						panic("private finish")
					}
					if phase == "reset_"+which && at.IsZero() {
						panic("private reset")
					}
					return nil
				}
			}
			w.setRead = setter("read", &reads)
			w.setWrite = setter("write", &writes)
			if strings.HasPrefix(phase, "cancel_") {
				s.call = func(c context.Context, _ id.Actor, _ id.ProjectID, _ ProjectQuery, _ projectHTTPKind, _ string) error {
					cancel()
					select {
					case <-entered:
					case <-release:
					}
					return c.Err()
				}
			} else if strings.HasPrefix(phase, "finish_abort_") {
				s.call = func(context.Context, id.Actor, id.ProjectID, ProjectQuery, projectHTTPKind, string) error {
					panic("service")
				}
			}
			done, unblock := projectHTTPAsync(t, cancel, release, func() { h.ServeHTTP(w, r) })
			if strings.HasPrefix(phase, "cancel_") {
				select {
				case <-entered:
				case <-time.After(time.Second):
					t.Fatal("callback did not enter")
				}
				select {
				case <-done:
					t.Fatal("returned before callback actual join")
				case <-time.After(10 * time.Millisecond):
				}
				unblock()
			}
			if !summaryJoined(t, done) || body.closes.Load() != 1 {
				t.Fatal("panic escaped abort/Close")
			}
			if !strings.HasPrefix(phase, "reset_") {
				w.cleared(t)
			}
		})
	}
}
func TestProjectModelHTTPPureActualTail(t *testing.T) {
	for _, phase := range []string{"body", "service", "close", "write", "flush"} {
		t.Run(phase, func(t *testing.T) {
			h, _, s := projectHTTPTestHandler(t)
			w := summaryWriter()
			r := projectHTTPRequest("GET", projectHTTPTestBase+"/models")
			ctx, cancel := context.WithCancel(r.Context())
			defer cancel()
			r = r.WithContext(ctx)
			entered, release := make(chan struct{}), make(chan struct{})
			block := func() { close(entered); <-release }
			body := &summaryHTTPBody{Reader: strings.NewReader("")}
			r.Body = body
			switch phase {
			case "body":
				body.Reader = summaryHTTPRead(func([]byte) (int, error) { block(); return 0, io.EOF })
			case "service":
				s.call = func(context.Context, id.Actor, id.ProjectID, ProjectQuery, projectHTTPKind, string) error {
					block()
					return nil
				}
			case "close":
				body.close = func() error { block(); return nil }
			case "write":
				w.write = func(p []byte) (int, error) { block(); return len(p), nil }
			case "flush":
				w.flush = func() error { block(); return nil }
			}
			done, unblock := projectHTTPAsync(t, cancel, release, func() { h.ServeHTTP(w, r) })
			select {
			case <-entered:
			case <-time.After(time.Second):
				t.Fatal("tail missing")
			}
			cancel()
			select {
			case <-done:
				t.Fatal("context cancellation substituted for join")
			case <-time.After(10 * time.Millisecond):
			}
			unblock()
			if !summaryJoined(t, done) || body.closes.Load() != 1 {
				t.Fatal("tail not aborted/joined")
			}
			w.cleared(t)
		})
	}
	for _, phase := range []string{"close_error", "close_panic", "write_short", "write_error", "write_panic", "flush_error", "flush_panic"} {
		t.Run(phase, func(t *testing.T) {
			h, _, _ := projectHTTPTestHandler(t)
			w := summaryWriter()
			r := projectHTTPRequest("GET", projectHTTPTestBase+"/models")
			body := &summaryHTTPBody{Reader: strings.NewReader("")}
			r.Body = body
			switch phase {
			case "close_error":
				body.close = func() error { return errors.New("private") }
			case "close_panic":
				body.close = func() error { panic("private") }
			case "write_short":
				w.write = func([]byte) (int, error) { return 0, nil }
			case "write_error":
				w.write = func([]byte) (int, error) { return 0, errors.New("private") }
			case "write_panic":
				w.write = func([]byte) (int, error) { panic("private") }
			case "flush_error":
				w.flush = func() error { return errors.New("private") }
			case "flush_panic":
				w.flush = func() error { panic("private") }
			}
			if !summaryAborts(func() { h.ServeHTTP(w, r) }) || body.closes.Load() != 1 {
				t.Fatal("tail failure not abort+Close")
			}
			w.cleared(t)
		})
	}
}

// Register ownership before launch. Fatal on an entry/negative assertion still
// releases every private seam and waits for the actual handler/callback tail.
func projectHTTPAsync(t *testing.T, cancel context.CancelFunc, release chan struct{}, fn func()) (<-chan bool, func()) {
	t.Helper()
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	done, joined := make(chan bool, 1), make(chan struct{})
	t.Cleanup(func() { unblock(); cancel(); <-joined })
	go func() { defer close(joined); done <- summaryAborts(fn) }()
	return done, unblock
}
