package projecthttp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
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
	"github.com/LunaDeerTech/agenteam/internal/central/project"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

var httpListPath = projectsPath
var httpDetailPath = projectPrefix + wireID[id.Project]().String()

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
	get         func(context.Context, id.Actor, pc.ProjectID) (pc.ProjectRef, error)
	list        func(context.Context, id.Actor, pc.ListOwnedProjectsRequest, f.PageRequest) (f.Page[pc.ProjectListItem], error)
	gets, lists atomic.Int32
}

func (s *handlerServices) GetProject(ctx context.Context, a id.Actor, target pc.ProjectID) (pc.ProjectRef, error) {
	s.gets.Add(1)
	if s.get != nil {
		return s.get(ctx, a, target)
	}
	return wireProject(), nil
}
func (s *handlerServices) ListOwnedProjects(ctx context.Context, a id.Actor, request pc.ListOwnedProjectsRequest, page f.PageRequest) (f.Page[pc.ProjectListItem], error) {
	s.lists.Add(1)
	if s.list != nil {
		return s.list(ctx, a, request, page)
	}
	return wirePage(), nil
}
func (s *handlerServices) calls() int32 { return s.gets.Load() + s.lists.Load() }
func handlerFixture() (*projectReadHTTP, *handlerBoundary, *handlerServices) {
	b := &handlerBoundary{}
	s := &handlerServices{}
	return &projectReadHTTP{reader: s, boundary: b}, b, s
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

func TestProjectOwnerReadHTTPPureConstructorAndRoutes(t *testing.T) {
	reader, boundary := &project.Reader{}, &account.HTTPBoundary{}
	for _, v := range []struct {
		r *project.Reader
		b *account.HTTPBoundary
	}{{nil, boundary}, {reader, nil}} {
		if h, e := NewHTTPHandler(v.r, v.b); h != nil || e == nil {
			t.Fatal("nil dependency accepted")
		}
	}
	h, e := NewHTTPHandler(reader, boundary)
	if e != nil || h.(*projectReadHTTP).reader != reader || h.(*projectReadHTTP).boundary != boundary {
		t.Fatal("root instances replaced", e)
	}
	for _, path := range []string{httpListPath, httpDetailPath, projectPrefix + "bad-id"} {
		if !HandlesPath(path) {
			t.Fatal("resource missed", path)
		}
	}
	for _, path := range []string{resolvePath, projectsPath + "/", httpDetailPath + "/", httpDetailPath + "/model-usage", httpDetailPath + "/model-usage/summary", httpDetailPath + "/archive", "/api/v1/session"} {
		if HandlesPath(path) {
			t.Fatal("foreign route captured", path)
		}
	}
}
func TestProjectOwnerReadHTTPPureGETHEADAndCurrentActor(t *testing.T) {
	for _, target := range []string{httpListPath, httpDetailPath} {
		var size string
		for _, method := range []string{"GET", "HEAD"} {
			t.Run(method+target, func(t *testing.T) {
				h, b, s := handlerFixture()
				var observed context.Context
				b.auth = func(r *http.Request) (id.Actor, error) { observed = r.Context(); return wireActor(), nil }
				verify := func(ctx context.Context, a id.Actor) {
					if ctx != observed || !a.Equal(wireActor()) {
						t.Fatal("actor/context detached")
					}
				}
				s.get = func(ctx context.Context, a id.Actor, target pc.ProjectID) (pc.ProjectRef, error) {
					verify(ctx, a)
					return wireProject(), nil
				}
				s.list = func(ctx context.Context, a id.Actor, _ pc.ListOwnedProjectsRequest, _ f.PageRequest) (f.Page[pc.ProjectListItem], error) {
					verify(ctx, a)
					return wirePage(), nil
				}
				w := newHandlerWriter()
				h.ServeHTTP(w, httptest.NewRequest(method, target, nil))
				if w.Code != 200 || s.calls() != 1 || b.auths.Load() != 1 || w.Header().Get("Cache-Control") != "no-store" {
					t.Fatal("read not complete")
				}
				if method == "GET" {
					size = w.Header().Get("Content-Length")
					if size != strconv.Itoa(w.Body.Len()) {
						t.Fatal("length")
					}
				} else if w.Body.Len() != 0 || w.Header().Get("Content-Length") != size {
					t.Fatal("HEAD representation")
				}
				w.assertCleared(t)
			})
		}
	}
}
func TestProjectOwnerReadHTTPPureBoundaryAndUnknown(t *testing.T) {
	for _, target := range []string{httpListPath, httpDetailPath} {
		for _, method := range []string{"GET", "HEAD"} {
			t.Run(method+target, func(t *testing.T) {
				h, _, s := handlerFixture()
				fault := f.NewFault(f.CommitUnknown, f.Unknown)
				fault.CauseID = "private-attempt-canary"
				fault.RetryHint = "lookup"
				s.get = func(context.Context, id.Actor, pc.ProjectID) (pc.ProjectRef, error) { return pc.ProjectRef{}, fault }
				s.list = func(context.Context, id.Actor, pc.ListOwnedProjectsRequest, f.PageRequest) (f.Page[pc.ProjectListItem], error) {
					return f.Page[pc.ProjectListItem]{}, fault
				}
				w := newHandlerWriter()
				var logs bytes.Buffer
				req := httptest.NewRequest(method, target, nil)
				httpapi.Handler(slog.New(slog.NewJSONHandler(&logs, nil)), h).ServeHTTP(w, req)
				if w.Code != 503 || s.calls() != 1 || strings.Contains(w.Body.String(), "private-attempt-canary") || strings.Contains(logs.String(), "private-attempt-canary") || w.Header().Get("Cause-ID") != "" {
					t.Fatal("Unknown boundary")
				}
				if method == "HEAD" {
					if w.Body.Len() != 0 {
						t.Fatal("HEAD Problem body")
					}
				} else {
					var p httpapi.Problem
					if json.Unmarshal(w.Body.Bytes(), &p) != nil || p.Code != f.CommitUnknown || p.CommitState != f.Unknown || p.RetryHint != "lookup" || p.RequestID.Validate() != nil {
						t.Fatal("Unknown projection")
					}
					if bytes.Contains(w.Body.Bytes(), []byte("cause_id")) {
						t.Fatal("expanded public Problem")
					}
				}
			})
		}
	}
	for _, tc := range []struct {
		method, path string
		status       int
	}{{"POST", httpListPath, 405}, {"GET", httpListPath + "?owner=other", 400}, {"GET", httpDetailPath + "?limit=1", 400}, {"GET", projectPrefix + "invalid", 400}} {
		h, _, s := handlerFixture()
		w := newHandlerWriter()
		h.ServeHTTP(w, httptest.NewRequest(tc.method, tc.path, nil))
		if w.Code != tc.status || s.calls() != 0 {
			t.Fatal("invalid request reached reader", tc, w.Code)
		}
		if tc.status == 405 && w.Header().Get("Allow") != "GET, HEAD" {
			t.Fatal("Allow")
		}
	}
	h, b, s := handlerFixture()
	b.auth = func(*http.Request) (id.Actor, error) { return id.Actor{}, f.NewFault(f.SessionRevoked, f.NotStarted) }
	w := newHandlerWriter()
	h.ServeHTTP(w, httptest.NewRequest("GET", httpListPath, nil))
	if s.calls() != 0 || w.Code != 401 {
		t.Fatal("authentication bypass")
	}
}
func TestProjectOwnerReadHTTPPureBodyFramingAndBoundedRead(t *testing.T) {
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
func TestProjectOwnerReadHTTPPureCapabilitiesAndIOFailures(t *testing.T) {
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
				s.list = func(context.Context, id.Actor, pc.ListOwnedProjectsRequest, f.PageRequest) (f.Page[pc.ProjectListItem], error) {
					panic("private-panic")
				}
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
func TestProjectOwnerReadHTTPPurePreauthenticationDeadlineAndCallbackJoin(t *testing.T) {
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
func TestProjectOwnerReadHTTPPureActualIOTails(t *testing.T) {
	for _, phase := range []string{"authentication", "read", "get", "list", "close", "write", "flush"} {
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
			case "get":
				target = httpDetailPath
				s.get = func(context.Context, id.Actor, pc.ProjectID) (pc.ProjectRef, error) {
					block()
					return wireProject(), nil
				}
			case "list":
				s.list = func(context.Context, id.Actor, pc.ListOwnedProjectsRequest, f.PageRequest) (f.Page[pc.ProjectListItem], error) {
					block()
					return wirePage(), nil
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
func TestProjectOwnerReadHTTPPureCloseBeforePublication(t *testing.T) {
	for _, method := range []string{"GET", "HEAD"} {
		for _, problem := range []bool{false, true} {
			t.Run(method+"/problem="+strconv.FormatBool(problem), func(t *testing.T) {
				h, _, service := handlerFixture()
				if problem {
					service.list = func(context.Context, id.Actor, pc.ListOwnedProjectsRequest, f.PageRequest) (f.Page[pc.ProjectListItem], error) {
						return f.Page[pc.ProjectListItem]{}, f.NewFault(f.CommitUnknown, f.Unknown)
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
