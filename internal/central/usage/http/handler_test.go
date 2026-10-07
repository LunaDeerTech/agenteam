package usagehttp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
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
	"github.com/LunaDeerTech/agenteam/internal/central/project"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/usage"
	uc "github.com/LunaDeerTech/agenteam/internal/central/usage/contract"
)

var httpListPath = projectPrefix + wireID[id.Project]().String() + "/model-usage"
var httpSummaryPath = httpListPath + "/summary"
var httpResolveURL = resolvePath + "?username=admin&project_name=Project"

type handlerBoundary struct {
	check         func(http.ResponseWriter, *http.Request) error
	auth          func(*http.Request) (id.Actor, error)
	checks, auths atomic.Int32
}

func (b *handlerBoundary) CheckRequest(w http.ResponseWriter, r *http.Request) error {
	b.checks.Add(1)
	if b.check != nil {
		return b.check(w, r)
	}
	return nil
}
func (b *handlerBoundary) RequireHuman(r *http.Request) (id.Actor, error) {
	b.auths.Add(1)
	if b.auth != nil {
		return b.auth(r)
	}
	return wireActor(), nil
}
func (*handlerBoundary) WriteProblem(w http.ResponseWriter, r *http.Request, err error) {
	// Exercise the real safe Account/Problem projector, never copy its mapping.
	(&account.HTTPBoundary{}).WriteProblem(w, r, err)
}

type handlerServices struct {
	resolve                     func(context.Context, id.Actor, string, string) (pc.ProjectRef, error)
	list                        func(context.Context, id.Actor, uc.Query) (uc.Page, error)
	aggregate                   func(context.Context, id.Actor, uc.AggregateQuery) (uc.AggregatePage, error)
	resolves, lists, aggregates atomic.Int32
}

func (s *handlerServices) ResolveProjectPath(ctx context.Context, a id.Actor, user, name string) (pc.ProjectRef, error) {
	s.resolves.Add(1)
	if s.resolve != nil {
		return s.resolve(ctx, a, user, name)
	}
	return wireProject(), nil
}
func (s *handlerServices) List(ctx context.Context, a id.Actor, q uc.Query) (uc.Page, error) {
	s.lists.Add(1)
	if s.list != nil {
		return s.list(ctx, a, q)
	}
	return uc.Page{Items: []uc.Invocation{wireInvocation()}}, nil
}
func (s *handlerServices) Aggregate(ctx context.Context, a id.Actor, q uc.AggregateQuery) (uc.AggregatePage, error) {
	s.aggregates.Add(1)
	if s.aggregate != nil {
		return s.aggregate(ctx, a, q)
	}
	return uc.AggregatePage{Items: []uc.GroupSummary{{Key: wireGroup(q.GroupBy), Summary: wireSummary()}}, AsOf: wireInstant()}, nil
}
func (s *handlerServices) calls() int32 {
	return s.resolves.Load() + s.lists.Load() + s.aggregates.Load()
}
func handlerFixture() (*projectUsageHTTP, *handlerBoundary, *handlerServices) {
	b := &handlerBoundary{}
	s := &handlerServices{}
	return &projectUsageHTTP{projects: s, usage: s, boundary: b}, b, s
}

type handlerWriter struct {
	*httptest.ResponseRecorder
	mu                sync.Mutex
	reads, writes     []time.Time
	setRead, setWrite func(time.Time) error
	write             func([]byte) (int, error)
	flush             func() error
	flushes           int
}

func newHandlerWriter() *handlerWriter {
	return &handlerWriter{ResponseRecorder: httptest.NewRecorder()}
}
func (w *handlerWriter) SetReadDeadline(at time.Time) error {
	w.mu.Lock()
	w.reads = append(w.reads, at)
	w.mu.Unlock()
	if w.setRead != nil {
		return w.setRead(at)
	}
	return nil
}
func (w *handlerWriter) SetWriteDeadline(at time.Time) error {
	w.mu.Lock()
	w.writes = append(w.writes, at)
	w.mu.Unlock()
	if w.setWrite != nil {
		return w.setWrite(at)
	}
	return nil
}
func (w *handlerWriter) Write(p []byte) (int, error) {
	if w.write != nil {
		return w.write(p)
	}
	return w.ResponseRecorder.Write(p)
}
func (w *handlerWriter) FlushError() error {
	w.flushes++
	if w.flush != nil {
		return w.flush()
	}
	return nil
}
func (w *handlerWriter) assertCleared(t *testing.T) {
	t.Helper()
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.reads) == 0 || len(w.writes) == 0 || !w.reads[len(w.reads)-1].IsZero() || !w.writes[len(w.writes)-1].IsZero() {
		t.Fatal("request retained native deadline")
	}
}

type handlerNoFlush struct{ w *handlerWriter }

func (w handlerNoFlush) Header() http.Header                 { return w.w.Header() }
func (w handlerNoFlush) WriteHeader(n int)                   { w.w.WriteHeader(n) }
func (w handlerNoFlush) Write(p []byte) (int, error)         { return w.w.Write(p) }
func (w handlerNoFlush) SetReadDeadline(at time.Time) error  { return w.w.SetReadDeadline(at) }
func (w handlerNoFlush) SetWriteDeadline(at time.Time) error { return w.w.SetWriteDeadline(at) }

type handlerBody struct {
	io.Reader
	close  func() error
	closes atomic.Int32
}

func (b *handlerBody) Close() error {
	b.closes.Add(1)
	if b.close != nil {
		return b.close()
	}
	return nil
}

type handlerReadFunc func([]byte) (int, error)

func (fn handlerReadFunc) Read(p []byte) (int, error) { return fn(p) }
func handlerAborts(run func()) (aborted bool) {
	defer func() {
		if value := recover(); value != nil {
			if value != http.ErrAbortHandler {
				panic(value)
			}
			aborted = true
		}
	}()
	run()
	return false
}
func handlerJoined(t *testing.T, done <-chan bool) bool {
	t.Helper()
	select {
	case result := <-done:
		return result
	case <-time.After(3 * time.Second):
		t.Fatal("handler did not reach actual terminal")
		return false
	}
}

func TestProjectUsageHTTPPureConstructorAndResources(t *testing.T) {
	p, s, b := &project.Authority{}, &usage.Service{}, &account.HTTPBoundary{}
	for _, inputs := range []struct {
		p *project.Authority
		s *usage.Service
		b *account.HTTPBoundary
	}{{nil, s, b}, {p, nil, b}, {p, s, nil}} {
		h, err := NewHTTPHandler(inputs.p, inputs.s, inputs.b)
		if h != nil || err == nil {
			t.Fatal("nil production dependency accepted")
		}
	}
	h, err := NewHTTPHandler(p, s, b)
	if err != nil {
		t.Fatal(err)
	}
	got := h.(*projectUsageHTTP)
	if got.projects != p || got.usage != s || got.boundary != b {
		t.Fatal("constructor replaced supplied root instances")
	}
	for _, path := range []string{resolvePath, httpListPath, httpSummaryPath, projectPrefix + "bad-id/model-usage"} {
		if !HandlesPath(path) {
			t.Fatal("resource not dispatched", path)
		}
	}
	for _, path := range []string{"/", projectPrefix, resolvePath + "/", httpListPath + "/", httpSummaryPath + "/extra", "/api/v1/system/models", projectPrefix + "id", "/api/v1/projects-other/id/model-usage"} {
		if HandlesPath(path) {
			t.Fatal("unrelated resource captured", path)
		}
	}
}
func TestProjectUsageHTTPPureGETHEADAndCurrentActor(t *testing.T) {
	for _, target := range []string{httpResolveURL, httpListPath, httpSummaryPath + "?group_by=day"} {
		var getLength string
		for _, method := range []string{"GET", "HEAD"} {
			t.Run(method+"/"+target, func(t *testing.T) {
				h, b, s := handlerFixture()
				var observed context.Context
				b.auth = func(r *http.Request) (id.Actor, error) { observed = r.Context(); return wireActor(), nil }
				check := func(ctx context.Context, a id.Actor) {
					if ctx != observed || !a.Equal(wireActor()) {
						t.Error("service replaced context or current Human")
					}
				}
				s.resolve = func(ctx context.Context, a id.Actor, user, name string) (pc.ProjectRef, error) {
					check(ctx, a)
					if user != "admin" || name != "Project" {
						t.Error("resolve changed once-decoded current name")
					}
					return wireProject(), nil
				}
				s.list = func(ctx context.Context, a id.Actor, q uc.Query) (uc.Page, error) {
					check(ctx, a)
					if q.Filter.ProjectID != wireID[id.Project]() || q.Limit != 50 {
						t.Error("wrong stable ID query")
					}
					return uc.Page{Items: []uc.Invocation{wireInvocation()}}, nil
				}
				s.aggregate = func(ctx context.Context, a id.Actor, q uc.AggregateQuery) (uc.AggregatePage, error) {
					check(ctx, a)
					if q.GroupBy != uc.ByDay {
						t.Error("group changed")
					}
					return uc.AggregatePage{Items: []uc.GroupSummary{{Key: wireGroup(q.GroupBy), Summary: wireSummary()}}, AsOf: wireInstant()}, nil
				}
				body := &handlerBody{Reader: strings.NewReader("")}
				r := httptest.NewRequest(method, target, nil)
				r.Body = body
				w := newHandlerWriter()
				httpapi.Handler(nil, h).ServeHTTP(w, r)
				if w.Code != 200 || b.checks.Load() != 1 || b.auths.Load() != 1 || s.calls() != 1 || body.closes.Load() != 1 || w.flushes != 1 || w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("X-Request-ID") == "" {
					t.Fatal("incomplete current-Owner read", w.Code)
				}
				if method == "GET" {
					getLength = w.Header().Get("Content-Length")
					if getLength != strconv.Itoa(w.Body.Len()) || w.Body.Len() == 0 {
						t.Fatal("GET length")
					}
				} else if w.Body.Len() != 0 || w.Header().Get("Content-Length") != getLength {
					t.Fatal("HEAD omitted query/encoding or exposed body")
				}
				if observed.Err() == nil {
					t.Fatal("request context not retired")
				}
				w.assertCleared(t)
			})
		}
	}
}
func TestProjectUsageHTTPPureBoundaryMethodAndQueryOrder(t *testing.T) {
	cases := []struct {
		name, method, target  string
		status, auth, service int
		change                func(*handlerBoundary, *http.Request)
	}{
		{"origin-before-path", "POST", "/api//v1", 403, 0, 0, func(b *handlerBoundary, r *http.Request) {
			b.check = func(http.ResponseWriter, *http.Request) error { return f.NewFault(f.OriginDenied, f.NotStarted) }
		}},
		{"unrelated", "GET", "/api/v1/unknown?private=name", 404, 0, 0, nil},
		{"method-resolve", "POST", httpResolveURL, 405, 0, 0, nil}, {"method-list", "OPTIONS", httpListPath, 405, 0, 0, nil}, {"method-summary", "DELETE", httpSummaryPath, 405, 0, 0, nil},
		{"auth-before-query", "GET", httpListPath + "?unknown=private", 401, 1, 0, func(b *handlerBoundary, r *http.Request) {
			b.auth = func(*http.Request) (id.Actor, error) { return id.Actor{}, f.NewFault(f.SessionRevoked, f.NotStarted) }
		}},
		{"bad-id", "GET", projectPrefix + "private-id/model-usage", 400, 1, 0, nil}, {"bad-list-query", "GET", httpListPath + "?group_by=day", 400, 1, 0, nil}, {"missing-group", "GET", httpSummaryPath, 400, 1, 0, nil}, {"bad-resolve", "GET", resolvePath + "?username=admin&project_name=%252e", 400, 1, 0, nil},
		{"non-human", "GET", httpListPath, 401, 1, 0, func(b *handlerBoundary, r *http.Request) {
			b.auth = func(*http.Request) (id.Actor, error) {
				return id.NewAgentRun(wireID[id.Project](), wireID[id.Agent](), wireID[id.Execution]())
			}
		}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			h, b, s := handlerFixture()
			r := httptest.NewRequest(test.method, test.target, nil)
			body := &handlerBody{Reader: strings.NewReader("")}
			r.Body = body
			if test.change != nil {
				test.change(b, r)
			}
			w := newHandlerWriter()
			httpapi.Handler(nil, h).ServeHTTP(w, r)
			if w.Code != test.status || int(b.auths.Load()) != test.auth || int(s.calls()) != test.service || body.closes.Load() != 1 {
				t.Fatal("boundary order", w.Code, b.auths.Load(), s.calls())
			}
			if test.status == 405 && w.Header().Get("Allow") != "GET, HEAD" {
				t.Fatal("Allow contract")
			}
			var p httpapi.Problem
			if json.Unmarshal(w.Body.Bytes(), &p) != nil || p.Instance != "/api/v1" || p.RequestID.String() != w.Header().Get("X-Request-ID") {
				t.Fatal("unsafe Problem")
			}
			w.assertCleared(t)
		})
	}
}
func TestProjectUsageHTTPPureUnknownProjectionAndSafeLogging(t *testing.T) {
	for _, method := range []string{"GET", "HEAD"} {
		for _, target := range []string{httpResolveURL + "&", httpListPath + "?cursor=private-canary", httpSummaryPath + "?group_by=day"} {
			t.Run(method+"/"+target, func(t *testing.T) {
				h, _, s := handlerFixture()
				err := f.NewFault(f.CommitUnknown, f.Unknown).WithCause(errors.New("private-canary"))
				s.resolve = func(context.Context, id.Actor, string, string) (pc.ProjectRef, error) { return wireProject(), err }
				s.list = func(context.Context, id.Actor, uc.Query) (uc.Page, error) {
					return uc.Page{Items: []uc.Invocation{wireInvocation()}}, err
				}
				s.aggregate = func(context.Context, id.Actor, uc.AggregateQuery) (uc.AggregatePage, error) {
					return uc.AggregatePage{Items: []uc.GroupSummary{{Key: wireGroup(uc.ByDay), Summary: wireSummary()}}, AsOf: wireInstant()}, err
				}
				// Resolve has its own current-name query, without Usage cursor keys.
				if strings.HasPrefix(target, resolvePath) {
					target = httpResolveURL
				}
				r := httptest.NewRequest(method, target, nil)
				r.Pattern = "private-canary"
				w := newHandlerWriter()
				var logBytes bytes.Buffer
				logger := slog.New(slog.NewJSONHandler(&logBytes, nil))
				httpapi.Handler(logger, h).ServeHTTP(w, r)
				if w.Code != 503 || s.calls() != 1 || strings.Contains(logBytes.String(), "private-canary") || strings.Contains(w.Body.String(), "private-canary") || strings.Contains(w.Body.String(), "items") {
					t.Fatal("Unknown candidate or unsafe route/cause exposed")
				}
				if method == "HEAD" {
					if w.Body.Len() != 0 || w.Header().Get("Content-Length") == "0" {
						t.Fatal("HEAD Problem body/length")
					}
				} else {
					var p httpapi.Problem
					if json.Unmarshal(w.Body.Bytes(), &p) != nil || p.Code != f.CommitUnknown || p.CommitState != f.Unknown || p.RetryHint != "lookup" || p.Instance != "/api/v1" {
						t.Fatal("Unknown mapping changed")
					}
				}
			})
		}
	}
	for _, method := range []string{"GET", "HEAD"} {
		h, _, s := handlerFixture()
		s.list = func(context.Context, id.Actor, uc.Query) (uc.Page, error) {
			row := wireInvocation()
			row.Fence = 0
			return uc.Page{Items: []uc.Invocation{row}}, nil
		}
		w := newHandlerWriter()
		h.ServeHTTP(w, httptest.NewRequest(method, httpListPath, nil))
		if w.Code != 503 || method == "HEAD" && w.Body.Len() != 0 || strings.Contains(w.Body.String(), "items") {
			t.Fatal("HEAD/full hidden validation bypassed")
		}
	}
}
func TestProjectUsageHTTPPureBodyFramingAndBoundedRead(t *testing.T) {
	for _, mode := range []string{"positive", "unknown", "transfer", "actual", "read_error", "zero_progress"} {
		for _, method := range []string{"GET", "HEAD"} {
			t.Run(mode+"/"+method, func(t *testing.T) {
				h, _, s := handlerFixture()
				reads := 0
				body := &handlerBody{Reader: handlerReadFunc(func(p []byte) (int, error) {
					reads++
					if len(p) > 1 {
						t.Error("defensive read exceeds one byte")
					}
					if mode == "read_error" {
						return 0, io.ErrUnexpectedEOF
					}
					if mode == "zero_progress" {
						return 0, nil
					}
					p[0] = 'x'
					return 1, nil
				})}
				r := httptest.NewRequest(method, httpListPath, nil)
				r.Body = body
				switch mode {
				case "positive":
					r.ContentLength = 1
				case "unknown":
					r.ContentLength = -1
				case "transfer":
					r.TransferEncoding = []string{"chunked"}
				}
				w := newHandlerWriter()
				h.ServeHTTP(w, r)
				if w.Code != 400 || s.calls() != 0 || body.closes.Load() != 1 {
					t.Fatal("forbidden body admitted")
				}
				if (mode == "actual" || mode == "read_error" || mode == "zero_progress") != (reads == 1) {
					t.Fatal("wrong defensive read", reads)
				}
				if method == "HEAD" && w.Body.Len() != 0 {
					t.Fatal("HEAD error body")
				}
			})
		}
	}
}
func TestProjectUsageHTTPPureCapabilitiesAndIOFailures(t *testing.T) {
	for _, mode := range []string{"read_unsupported", "write_unsupported", "no_flush", "short_write", "write_error", "flush_error", "close_error", "clear_read_error", "clear_write_error", "panic"} {
		t.Run(mode, func(t *testing.T) {
			h, _, s := handlerFixture()
			w := newHandlerWriter()
			body := &handlerBody{Reader: strings.NewReader("")}
			r := httptest.NewRequest("GET", httpListPath, nil)
			r.Body = body
			var target http.ResponseWriter = w
			switch mode {
			case "read_unsupported":
				w.setRead = func(time.Time) error { return http.ErrNotSupported }
			case "write_unsupported":
				w.setWrite = func(time.Time) error { return http.ErrNotSupported }
			case "no_flush":
				target = handlerNoFlush{w}
			case "short_write":
				w.write = func(p []byte) (int, error) { return len(p) - 1, nil }
			case "write_error":
				w.write = func([]byte) (int, error) { return 0, io.ErrClosedPipe }
			case "flush_error":
				w.flush = func() error { return io.ErrClosedPipe }
			case "close_error":
				body.close = func() error { return io.ErrClosedPipe }
			case "clear_read_error":
				w.setRead = func(at time.Time) error {
					if at.IsZero() {
						return io.ErrClosedPipe
					}
					return nil
				}
			case "clear_write_error":
				w.setWrite = func(at time.Time) error {
					if at.IsZero() {
						return io.ErrClosedPipe
					}
					return nil
				}
			case "panic":
				s.list = func(context.Context, id.Actor, uc.Query) (uc.Page, error) { panic("private-panic") }
			}
			if !handlerAborts(func() { httpapi.Handler(nil, h).ServeHTTP(target, r) }) || body.closes.Load() != 1 {
				t.Fatal("I/O/capability failure escaped abort/Close")
			}
			if (mode == "read_unsupported" || mode == "write_unsupported") && s.calls() != 0 {
				t.Fatal("unsupported budget reached service")
			}
			if (mode == "close_error" || mode == "panic") && w.Body.Len() != 0 {
				t.Fatal("prepublication failure emitted candidate")
			}
			if strings.Contains(w.Body.String(), "private-panic") || strings.Contains(w.Body.String(), "application/problem") {
				t.Fatal("abort appended Problem")
			}
		})
	}
	body := &handlerBody{Reader: strings.NewReader("")}
	h, _, s := handlerFixture()
	r := httptest.NewRequest("GET", httpListPath, nil)
	r.Body = body
	if !handlerAborts(func() { h.ServeHTTP(httptest.NewRecorder(), r) }) || body.closes.Load() != 1 || s.calls() != 0 {
		t.Fatal("unsupported real controller silently downgraded")
	}
}
func TestProjectUsageHTTPPurePreauthenticationDeadlineAndCallbackJoin(t *testing.T) {
	for _, phase := range []string{"boundary", "authentication"} {
		for _, duration := range []time.Duration{time.Minute, 50 * time.Millisecond} {
			t.Run(phase+"/"+duration.String(), func(t *testing.T) {
				parent, cancel := context.WithTimeout(context.Background(), duration)
				defer cancel()
				parentDeadline, _ := parent.Deadline()
				h, b, s := handlerFixture()
				w := newHandlerWriter()
				body := &handlerBody{Reader: strings.NewReader("")}
				entered, release, joined := make(chan struct{}), make(chan struct{}), make(chan struct{})
				var releaseOnce sync.Once
				var callback atomic.Bool
				w.setRead = func(at time.Time) error {
					if !at.IsZero() && !at.After(time.Now()) && callback.CompareAndSwap(false, true) {
						close(entered)
						<-release
					}
					return nil
				}
				started := time.Now()
				block := func(r *http.Request) error {
					at := time.Now()
					deadline, ok := r.Context().Deadline()
					if !ok || duration < readBudget && !deadline.Equal(parentDeadline) || duration > readBudget && (deadline.Before(started.Add(readBudget)) || deadline.After(at.Add(readBudget)) || !deadline.Before(parentDeadline)) {
						t.Error("deadline restarted/extended before boundary/auth")
					}
					<-r.Context().Done()
					if !errors.Is(r.Context().Err(), context.DeadlineExceeded) || duration > readBudget && parent.Err() != nil {
						t.Error("pure deadline cause/parent changed")
					}
					<-entered
					return r.Context().Err()
				}
				if phase == "boundary" {
					b.check = func(_ http.ResponseWriter, r *http.Request) error { return block(r) }
				} else {
					b.auth = func(r *http.Request) (id.Actor, error) { return id.Actor{}, block(r) }
				}
				r := httptest.NewRequest("GET", httpListPath, nil).WithContext(parent)
				r.Body = body
				done := make(chan bool, 1)
				go func() { defer close(joined); done <- handlerAborts(func() { h.ServeHTTP(w, r) }) }()
				t.Cleanup(func() { cancel(); releaseOnce.Do(func() { close(release) }); <-joined })
				select {
				case <-entered:
				case <-time.After(readBudget + time.Second):
					t.Fatal("natural cancellation callback not entered")
				}
				select {
				case <-done:
					t.Fatal("callback tail returned before release")
				default:
				}
				if s.calls() != 0 {
					t.Fatal("late authentication reached usage")
				}
				releaseOnce.Do(func() { close(release) })
				if !handlerJoined(t, done) {
					t.Fatal("cancelled request returned normally")
				}
				<-joined
				if body.closes.Load() != 1 || w.Body.Len() != 0 || w.flushes != 0 {
					t.Fatal("expired boundary leaked candidate/resources")
				}
				w.assertCleared(t)
			})
		}
	}
}
func TestProjectUsageHTTPPureActualIOTails(t *testing.T) {
	for _, phase := range []string{"authentication", "read", "resolve", "list", "aggregate", "close", "write", "flush"} {
		t.Run(phase, func(t *testing.T) {
			parent, cancel := context.WithCancel(context.Background())
			h, b, s := handlerFixture()
			w := newHandlerWriter()
			body := &handlerBody{Reader: strings.NewReader("")}
			entered, release, joined := make(chan struct{}), make(chan struct{}), make(chan struct{})
			var once sync.Once
			var active atomic.Int32
			block := func() { active.Add(1); defer active.Add(-1); close(entered); <-release }
			target := httpListPath
			switch phase {
			case "authentication":
				b.auth = func(*http.Request) (id.Actor, error) { block(); return wireActor(), nil }
			case "read":
				body.Reader = handlerReadFunc(func([]byte) (int, error) { block(); return 0, io.EOF })
			case "resolve":
				target = httpResolveURL
				s.resolve = func(context.Context, id.Actor, string, string) (pc.ProjectRef, error) {
					block()
					return wireProject(), nil
				}
			case "list":
				s.list = func(context.Context, id.Actor, uc.Query) (uc.Page, error) {
					block()
					return uc.Page{Items: []uc.Invocation{wireInvocation()}}, nil
				}
			case "aggregate":
				target = httpSummaryPath + "?group_by=day"
				s.aggregate = func(context.Context, id.Actor, uc.AggregateQuery) (uc.AggregatePage, error) {
					block()
					return uc.AggregatePage{Items: []uc.GroupSummary{{Key: wireGroup(uc.ByDay), Summary: wireSummary()}}, AsOf: wireInstant()}, nil
				}
			case "close":
				body.close = func() error { block(); return nil }
			case "write":
				w.write = func([]byte) (int, error) { block(); return 0, io.ErrClosedPipe }
			case "flush":
				w.flush = func() error { block(); return io.ErrClosedPipe }
			}
			r := httptest.NewRequest("GET", target, nil).WithContext(parent)
			r.Body = body
			done := make(chan bool, 1)
			go func() { defer close(joined); done <- handlerAborts(func() { h.ServeHTTP(w, r) }) }()
			t.Cleanup(func() { cancel(); once.Do(func() { close(release) }); <-joined })
			select {
			case <-entered:
			case <-time.After(time.Second):
				t.Fatal("synchronous tail not reached")
			}
			cancel()
			if active.Load() != 1 {
				t.Fatal("cancellation pretended tail joined")
			}
			select {
			case <-done:
				t.Fatal("handler returned before actual tail")
			default:
			}
			once.Do(func() { close(release) })
			if !handlerJoined(t, done) {
				t.Fatal("cancelled I/O tail returned normally")
			}
			<-joined
			if active.Load() != 0 || body.closes.Load() != 1 {
				t.Fatal("tail not actually joined")
			}
			if phase != "write" && phase != "flush" && (w.Body.Len() != 0 || w.flushes != 0) {
				t.Fatal("prepublication cancellation leaked candidate")
			}
			if (phase == "authentication" || phase == "read") && s.calls() != 0 {
				t.Fatal("cancelled pre-service tail called service")
			}
			w.assertCleared(t)
		})
	}
}
func TestProjectUsageHTTPPureCloseBeforePublication(t *testing.T) {
	for _, method := range []string{"GET", "HEAD"} {
		for _, problem := range []bool{false, true} {
			t.Run(method+"/problem="+strconv.FormatBool(problem), func(t *testing.T) {
				h, _, service := handlerFixture()
				if problem {
					service.list = func(context.Context, id.Actor, uc.Query) (uc.Page, error) {
						return uc.Page{}, f.NewFault(f.CommitUnknown, f.Unknown)
					}
				}
				w := newHandlerWriter()
				entered, release, joined := make(chan struct{}), make(chan struct{}), make(chan struct{})
				var once sync.Once
				body := &handlerBody{Reader: strings.NewReader(""), close: func() error { close(entered); <-release; return nil }}
				r := httptest.NewRequest(method, httpListPath, nil)
				r.Body = body
				done := make(chan bool, 1)
				go func() { defer close(joined); done <- handlerAborts(func() { h.ServeHTTP(w, r) }) }()
				t.Cleanup(func() { once.Do(func() { close(release) }); <-joined })
				select {
				case <-entered:
				case <-time.After(time.Second):
					t.Fatal("Close not entered")
				}
				// Close is blocked before any writer method, so these reads are synchronized.
				if w.Body.Len() != 0 || w.flushes != 0 {
					t.Fatal("representation published before input Close")
				}
				select {
				case <-done:
					t.Fatal("Close escaped ownership")
				default:
				}
				once.Do(func() { close(release) })
				if handlerJoined(t, done) {
					t.Fatal("normal closed read aborted")
				}
				<-joined
				expected := 200
				if problem {
					expected = 503
				}
				if body.closes.Load() != 1 || w.Code != expected || (method == "GET") != (w.Body.Len() > 0) || w.Header().Get("Content-Length") == "" {
					t.Fatal("complete GET/HEAD publication lost")
				}
				w.assertCleared(t)
			})
		}
	}
}

// Native tests require a separately granted loopback window. Pure selectors and
// -test.list never invoke these helpers, and a default package run skips them.
func requireUsageNative(t *testing.T) {
	t.Helper()
	if os.Getenv("AGENTEAM_USAGE_HTTP_NATIVE") != "1" {
		t.Skip("requires the explicitly granted Project Usage native resource window")
	}
}

type nativeUsageBody struct {
	io.ReadCloser
	eof, closes *atomic.Int32
	closeError  bool
}

func (b nativeUsageBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	if err == io.EOF {
		b.eof.Add(1)
	}
	return n, err
}
func (b nativeUsageBody) Close() error {
	b.closes.Add(1)
	err := b.ReadCloser.Close()
	if b.closeError && err == nil {
		return io.ErrClosedPipe
	}
	return err
}

type nativeUsageResult struct{ aborted bool }

func nativeUsageListener(t *testing.T, handler http.Handler, smallBuffers bool) (string, <-chan nativeUsageResult, *atomic.Int32) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	results := make(chan nativeUsageResult, 16)
	connections := &atomic.Int32{}
	var mu sync.Mutex
	changed := sync.NewCond(&mu)
	active := map[net.Conn]bool{}
	socketErrors := make(chan error, 16)
	server := &http.Server{ErrorLog: log.New(io.Discard, "", 0), Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			v := recover()
			results <- nativeUsageResult{aborted: v == http.ErrAbortHandler}
			if v != nil {
				panic(v)
			}
		}()
		httpapi.Handler(nil, handler).ServeHTTP(w, r)
	}), ConnState: func(conn net.Conn, state http.ConnState) {
		mu.Lock()
		defer mu.Unlock()
		switch state {
		case http.StateNew:
			connections.Add(1)
			active[conn] = true
			if smallBuffers {
				if err := conn.(*net.TCPConn).SetWriteBuffer(1024); err != nil {
					socketErrors <- err
				}
			}
		case http.StateClosed, http.StateHijacked:
			delete(active, conn)
			changed.Broadcast()
		}
	}}
	served := make(chan error, 1)
	go func() { served <- server.Serve(listener) }()
	t.Cleanup(func() {
		if err := server.Close(); err != nil {
			t.Error(err)
		}
		_ = listener.Close()
		if err := <-served; !errors.Is(err, http.ErrServerClosed) {
			t.Error("native Serve did not reach terminal", err)
		}
		mu.Lock()
		for len(active) > 0 {
			changed.Wait()
		}
		mu.Unlock()
		close(socketErrors)
		for err := range socketErrors {
			t.Error("native socket configuration", err)
		}
	})
	return listener.Addr().String(), results, connections
}
func nativeUsageDial(t *testing.T, address string, smallBuffers bool) net.Conn {
	t.Helper()
	conn, err := net.DialTimeout("tcp", address, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	if err = conn.SetDeadline(time.Now().Add(10 * time.Second)); err != nil {
		t.Fatal(err)
	}
	if smallBuffers {
		if err = conn.(*net.TCPConn).SetReadBuffer(1024); err != nil {
			t.Fatal(err)
		}
	}
	return conn
}
func nativeUsageTerminal(t *testing.T, results <-chan nativeUsageResult) nativeUsageResult {
	t.Helper()
	select {
	case result := <-results:
		return result
	case <-time.After(5 * time.Second):
		t.Fatal("native handler did not actually return")
		return nativeUsageResult{}
	}
}

func TestProjectUsageHTTPNativeKeepAlive(t *testing.T) {
	requireUsageNative(t)
	h, _, _ := handlerFixture()
	var eof, closes, requests atomic.Int32
	address, results, connections := nativeUsageListener(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		budget := time.Second
		if requests.Add(1) == 1 {
			budget = 120 * time.Millisecond
		}
		ctx, cancel := context.WithTimeout(r.Context(), budget)
		defer cancel()
		r = r.WithContext(ctx)
		r.Body = nativeUsageBody{ReadCloser: r.Body, eof: &eof, closes: &closes}
		h.ServeHTTP(w, r)
	}), false)
	conn := nativeUsageDial(t, address, false)
	reader := bufio.NewReader(conn)
	lengths := map[string]int64{}
	for index, test := range []struct{ method, target string }{{"GET", httpListPath}, {"HEAD", httpListPath}, {"GET", httpSummaryPath + "?group_by=day"}, {"HEAD", httpSummaryPath + "?group_by=day"}, {"GET", httpResolveURL}, {"HEAD", httpResolveURL}} {
		if index == 1 {
			timer := time.NewTimer(200 * time.Millisecond)
			<-timer.C
		}
		if _, err := io.WriteString(conn, test.method+" "+test.target+" HTTP/1.1\r\nHost: "+address+"\r\nContent-Length: 0\r\n\r\n"); err != nil {
			t.Fatal(err)
		}
		response, err := http.ReadResponse(reader, &http.Request{Method: test.method})
		if err != nil {
			t.Fatal("native keepalive response", err)
		}
		body, readErr := io.ReadAll(response.Body)
		closeErr := response.Body.Close()
		if readErr != nil || closeErr != nil || response.StatusCode != 200 || response.Header.Get("X-Request-ID") == "" {
			t.Fatal("native incomplete read", readErr, closeErr, response.StatusCode)
		}
		if test.method == "GET" {
			lengths[test.target] = int64(len(body))
			if int64(len(body)) != response.ContentLength || len(body) == 0 {
				t.Fatal("native GET representation/EOF")
			}
		} else if len(body) != 0 || response.ContentLength != lengths[test.target] {
			t.Fatal("native HEAD representation/EOF")
		}
		if nativeUsageTerminal(t, results).aborted {
			t.Fatal("completed native request aborted")
		}
	}
	if connections.Load() != 1 || requests.Load() != 6 || eof.Load() != 6 || closes.Load() != 6 {
		t.Fatal("keepalive connection or actual body EOF/Close lost", connections.Load(), requests.Load(), eof.Load(), closes.Load())
	}
}
func TestProjectUsageHTTPNativeSlowBody(t *testing.T) {
	requireUsageNative(t)
	for _, framing := range []string{"declared", "hidden"} {
		for _, parentBudget := range []time.Duration{time.Minute, 120 * time.Millisecond} {
			t.Run(framing+"/"+parentBudget.String(), func(t *testing.T) {
				type observation struct {
					ctx                  context.Context
					parent, before, auth time.Time
				}
				seen := make(chan observation, 1)
				h, b, s := handlerFixture()
				var eof, closes atomic.Int32
				address, results, _ := nativeUsageListener(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					ctx, cancel := context.WithTimeout(r.Context(), parentBudget)
					defer cancel()
					parentDeadline, _ := ctx.Deadline()
					r = r.WithContext(ctx)
					r.Body = nativeUsageBody{ReadCloser: r.Body, eof: &eof, closes: &closes}
					if framing == "hidden" {
						r.ContentLength = 0
					}
					before := time.Now()
					b.auth = func(r *http.Request) (id.Actor, error) {
						seen <- observation{r.Context(), parentDeadline, before, time.Now()}
						return wireActor(), nil
					}
					h.ServeHTTP(w, r)
				}), false)
				conn := nativeUsageDial(t, address, false)
				started := time.Now()
				if _, err := io.WriteString(conn, "GET "+httpListPath+" HTTP/1.1\r\nHost: "+address+"\r\nContent-Length: 1\r\n\r\n"); err != nil {
					t.Fatal(err)
				}
				var observed observation
				select {
				case observed = <-seen:
				case <-time.After(time.Second):
					t.Fatal("native authentication not reached")
				}
				deadline, ok := observed.ctx.Deadline()
				if !ok || parentBudget < readBudget && !deadline.Equal(observed.parent) || parentBudget > readBudget && (deadline.Before(observed.before.Add(readBudget)) || deadline.After(observed.auth.Add(readBudget)) || !deadline.Before(observed.parent)) {
					t.Fatal("native preauthentication deadline clipping")
				}
				response, err := http.ReadResponse(bufio.NewReader(conn), &http.Request{Method: "GET"})
				if err == nil {
					_, _ = io.Copy(io.Discard, response.Body)
					_ = response.Body.Close()
					if response.StatusCode == 200 {
						t.Fatal("slow forbidden body returned success")
					}
				}
				terminal := nativeUsageTerminal(t, results)
				finished := time.Now()
				done := false
				select {
				case <-observed.ctx.Done():
					done = true
				default:
				}
				cause := observed.ctx.Err()
				limit := min(parentBudget, readBudget)
				// Native socket expiry can cancel net/http's parent before the context
				// timer wins. Observe the deadline/Done/elapsed/Close facts, not that race.
				if !terminal.aborted || !done || finished.Before(deadline) || finished.Sub(started) < limit*3/4 || finished.Sub(started) > limit+time.Second || (!errors.Is(cause, context.Canceled) && !errors.Is(cause, context.DeadlineExceeded)) || s.calls() != 0 || closes.Load() != 1 {
					t.Fatal("native slow-body terminal", terminal.aborted, cause, finished.Sub(started), s.calls(), closes.Load())
				}
			})
		}
	}
}

type nativeUsageFailureWriter struct {
	http.ResponseWriter
	mode      string
	timedOut  atomic.Bool
	written   atomic.Int64
	mu        sync.Mutex
	deadlines []time.Time
}

func (w *nativeUsageFailureWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }
func (w *nativeUsageFailureWriter) observe(err error) {
	var timeout net.Error
	if errors.As(err, &timeout) && timeout.Timeout() {
		w.timedOut.Store(true)
	}
}
func (w *nativeUsageFailureWriter) Write(p []byte) (int, error) {
	if w.mode == "write_error" {
		return 0, io.ErrClosedPipe
	}
	if w.mode == "short_write" {
		p = p[:1]
	}
	n, err := w.ResponseWriter.Write(p)
	w.written.Add(int64(n))
	w.observe(err)
	return n, err
}
func (w *nativeUsageFailureWriter) FlushError() error {
	err := http.NewResponseController(w.ResponseWriter).Flush()
	w.observe(err)
	if err == nil && w.mode == "flush_error" {
		return io.ErrClosedPipe
	}
	return err
}
func (w *nativeUsageFailureWriter) SetWriteDeadline(at time.Time) error {
	w.mu.Lock()
	w.deadlines = append(w.deadlines, at)
	w.mu.Unlock()
	return http.NewResponseController(w.ResponseWriter).SetWriteDeadline(at)
}
func TestProjectUsageHTTPNativeWriteAndClose(t *testing.T) {
	requireUsageNative(t)
	for _, mode := range []string{"short_write", "write_error", "flush_error", "close_error", "native_write_deadline"} {
		t.Run(mode, func(t *testing.T) {
			h, _, s := handlerFixture()
			var eof, closes atomic.Int32
			observed := make(chan *nativeUsageFailureWriter, 1)
			target := httpListPath
			expected := 0
			if mode == "native_write_deadline" {
				page := uc.Page{Items: make([]uc.Invocation, 100)}
				for i := range page.Items {
					page.Items[i] = wireMaximumInvocation()
				}
				s.list = func(context.Context, id.Actor, uc.Query) (uc.Page, error) { return page, nil }
				encoded, err := encodeList(context.Background(), wireQuery(), page)
				if err != nil {
					t.Fatal(err)
				}
				expected = len(encoded)
				target += "?limit=100"
			}
			address, results, _ := nativeUsageListener(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				r.Body = nativeUsageBody{ReadCloser: r.Body, eof: &eof, closes: &closes, closeError: mode == "close_error"}
				writer := &nativeUsageFailureWriter{ResponseWriter: w, mode: mode}
				observed <- writer
				h.ServeHTTP(writer, r)
			}), mode == "native_write_deadline")
			conn := nativeUsageDial(t, address, mode == "native_write_deadline")
			started := time.Now()
			if _, err := io.WriteString(conn, "GET "+target+" HTTP/1.1\r\nHost: "+address+"\r\nContent-Length: 0\r\n\r\n"); err != nil {
				t.Fatal(err)
			}
			var writer *nativeUsageFailureWriter
			select {
			case writer = <-observed:
			case <-time.After(time.Second):
				t.Fatal("native writer not reached")
			}
			// For the real deadline case the peer intentionally does not read until
			// the bounded write has actually returned and the handler has aborted.
			terminal := nativeUsageTerminal(t, results)
			if !terminal.aborted || closes.Load() != 1 {
				t.Fatal("native failure missed abort/Close")
			}
			if mode == "native_write_deadline" {
				writer.mu.Lock()
				first, last := writer.deadlines[0], writer.deadlines[len(writer.deadlines)-1]
				writer.mu.Unlock()
				if !writer.timedOut.Load() || time.Now().Before(first) || time.Since(started) < readBudget*3/4 || writer.written.Load() >= int64(expected) || !last.IsZero() {
					t.Fatal("native write was not deadline-bounded/joined", writer.timedOut.Load(), writer.written.Load(), expected)
				}
			}
			data, err := io.ReadAll(io.LimitReader(conn, maxRepresentationBytes+4096))
			if err != nil {
				t.Fatal("aborted native connection did not reach EOF", err)
			}
			if bytes.Contains(data, []byte("application/problem+json")) || mode == "close_error" && len(data) != 0 {
				t.Fatal("failure appended Problem/published before Close")
			}
			var one [1]byte
			if n, err := conn.Read(one[:]); n != 0 || !errors.Is(err, io.EOF) {
				t.Fatal("aborted keepalive survived", n, err)
			}
		})
	}
}
