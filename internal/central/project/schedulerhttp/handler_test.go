package schedulerhttp

import (
	"context"
	"errors"
	"io"
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
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/project"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

type testBoundary struct {
	check func(http.ResponseWriter, *http.Request) error
	auth  func(*http.Request) (i.Actor, error)
}

func (b *testBoundary) CheckRequest(w http.ResponseWriter, r *http.Request) error {
	if b.check != nil {
		return b.check(w, r)
	}
	return nil
}
func (b *testBoundary) RequireHuman(r *http.Request) (i.Actor, error) {
	if b.auth != nil {
		return b.auth(r)
	}
	return testActor(), nil
}
func (*testBoundary) WriteProblem(w http.ResponseWriter, r *http.Request, err error) {
	(&account.HTTPBoundary{}).WriteProblem(w, r, err)
}

type testService struct {
	get           func(context.Context, i.Actor, pc.ProjectID) (pc.SchedulerProject, error)
	update        func(context.Context, i.Actor, f.CommandMeta, pc.ProjectID, pc.UpdateProjectRequest) (pc.ProjectRef, error)
	reads, writes atomic.Int32
}

func (s *testService) GetSchedulerConfig(ctx context.Context, a i.Actor, id pc.ProjectID) (pc.SchedulerProject, error) {
	s.reads.Add(1)
	if s.get != nil {
		return s.get(ctx, a, id)
	}
	return pc.SchedulerProject{Project: testProject()}, nil
}
func (s *testService) UpdateProject(ctx context.Context, a i.Actor, m f.CommandMeta, id pc.ProjectID, input pc.UpdateProjectRequest) (pc.ProjectRef, error) {
	s.writes.Add(1)
	if s.update != nil {
		return s.update(ctx, a, m, id, input)
	}
	p := testProject()
	p.Version++
	return p, nil
}

type testWriter struct {
	*httptest.ResponseRecorder
	mu            sync.Mutex
	reads, writes []time.Time
	read          func(time.Time) error
	flush         func() error
	write         func([]byte) (int, error)
}

func (w *testWriter) SetReadDeadline(at time.Time) error {
	w.mu.Lock()
	w.reads = append(w.reads, at)
	w.mu.Unlock()
	if w.read != nil {
		return w.read(at)
	}
	return nil
}
func (w *testWriter) SetWriteDeadline(at time.Time) error {
	w.mu.Lock()
	w.writes = append(w.writes, at)
	w.mu.Unlock()
	return nil
}
func (w *testWriter) FlushError() error {
	if w.flush != nil {
		return w.flush()
	}
	return nil
}
func (w *testWriter) Write(p []byte) (int, error) {
	if w.write != nil {
		return w.write(p)
	}
	return w.ResponseRecorder.Write(p)
}
func writer() *testWriter { return &testWriter{ResponseRecorder: httptest.NewRecorder()} }
func (w *testWriter) cleared(t *testing.T) {
	t.Helper()
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.reads) == 0 || len(w.writes) == 0 || !w.reads[len(w.reads)-1].IsZero() || !w.writes[len(w.writes)-1].IsZero() {
		t.Fatal("native deadlines not cleared after actual tail")
	}
}
func serve(h http.Handler, w http.ResponseWriter, r *http.Request) (panicked any) {
	defer func() { panicked = recover() }()
	httpapi.WithRequestID(nil, h).ServeHTTP(w, r)
	return nil
}
func fixture() (*handler, *testService, *testBoundary) {
	s := &testService{}
	b := &testBoundary{}
	return &handler{s, b}, s, b
}

type testBody struct {
	io.Reader
	close  func() error
	closes atomic.Int32
}

func (b *testBody) Close() error {
	b.closes.Add(1)
	if b.close != nil {
		return b.close()
	}
	return nil
}

func TestSchedulerHTTPConstructorAndResourceOwnership(t *testing.T) {
	for _, v := range []struct {
		s *project.Service
		b *account.HTTPBoundary
	}{{nil, nil}, {&project.Service{}, nil}, {nil, &account.HTTPBoundary{}}} {
		if h, e := NewHTTPHandler(v.s, v.b); e == nil || h != nil {
			t.Fatal("missing binding accepted")
		}
	}
	for path, want := range map[string]bool{testPath: true, projectPrefix + "bad-id/scheduler": true, testPath + "/": false, testPath + "/lookup": false, projectPrefix + "scheduler": false, projectPrefix + "/scheduler": false} {
		if HandlesPath(path) != want {
			t.Fatal(path)
		}
	}
	h, s, _ := fixture()
	w := writer()
	if p := serve(h, w, testRequest("DELETE", testPath, "")); p != nil || w.Code != 405 || w.Header().Get("Allow") != "GET, HEAD, PATCH" || s.reads.Load()+s.writes.Load() != 0 {
		t.Fatal("method ownership", p, w.Code)
	}
}

func TestSchedulerHTTPCurrentReadHeadAndOriginalWrite(t *testing.T) {
	h, s, b := fixture()
	b.auth = func(r *http.Request) (i.Actor, error) {
		if r.Pattern != projectPrefix+"{project_id}/scheduler" {
			t.Error("unsafe pattern", r.Pattern)
		}
		deadline, ok := r.Context().Deadline()
		if !ok || time.Until(deadline) <= 0 || time.Until(deadline) > writeBudget {
			t.Error("budget not installed before authentication")
		}
		return testActor(), nil
	}
	var getLength string
	for _, method := range []string{"GET", "HEAD"} {
		w := writer()
		r := testRequest(method, testPath, "")
		if p := serve(h, w, r); p != nil || w.Code != 200 {
			t.Fatal(method, p, w.Code, w.Body.String())
		}
		if method == "GET" {
			getLength = w.Header().Get("Content-Length")
			if len(object(t, w.Body.Bytes())) != 4 {
				t.Fatal("read shape")
			}
		} else if w.Body.Len() != 0 || w.Header().Get("Content-Length") != getLength {
			t.Fatal("HEAD differed")
		}
		w.cleared(t)
	}
	s.update = func(ctx context.Context, a i.Actor, m f.CommandMeta, id pc.ProjectID, input pc.UpdateProjectRequest) (pc.ProjectRef, error) {
		if a.Details() != testActor().Details() || id != testID[i.Project]() || m.IdempotencyKey != "scheduler-original-key" || m.RequestID.Validate() != nil || *m.ExpectedVersion != 1 || input.Scheduler == nil || input.Scheduler.Enabled || input.Scheduler.MaxConcurrency != nil || input.Name != nil || input.Description != nil {
			t.Fatal("original intent altered")
		}
		p := testProject()
		p.Version = 2
		return p, nil
	}
	w := writer()
	if p := serve(h, w, testRequest("PATCH", testPath, validInput)); p != nil || w.Code != 200 || len(object(t, w.Body.Bytes())) != 2 {
		t.Fatal("update acknowledgement", p, w.Code, w.Body.String())
	}
	if s.reads.Load() != 2 || s.writes.Load() != 1 {
		t.Fatal("write refreshed or repeated", s.reads.Load(), s.writes.Load())
	}
	w.cleared(t)
}

func TestSchedulerHTTPRefusesInvalidTransportBeforeDomain(t *testing.T) {
	for name, mutate := range map[string]func(*http.Request){
		"query": func(r *http.Request) { r.URL.RawQuery = "x=1" }, "bare query": func(r *http.Request) { r.URL.ForceQuery = true }, "invalid id": func(r *http.Request) { r.URL.Path = projectPrefix + "bad/scheduler" }, "duplicate key": func(r *http.Request) { r.Header.Add("Idempotency-Key", "other") }, "missing key": func(r *http.Request) { r.Header.Del("Idempotency-Key") }, "wrong media": func(r *http.Request) { r.Header.Set("Content-Type", "text/plain") }, "encoding": func(r *http.Request) { r.Header.Set("Content-Encoding", "gzip") }, "oversize": func(r *http.Request) {
			r.Body = io.NopCloser(strings.NewReader(strings.Repeat(" ", representationLimit) + validInput))
		}, "read body": func(r *http.Request) { r.Method = "GET" },
	} {
		t.Run(name, func(t *testing.T) {
			h, s, _ := fixture()
			w := writer()
			r := testRequest("PATCH", testPath, validInput)
			mutate(r)
			if p := serve(h, w, r); p != nil || w.Code < 400 || s.reads.Load()+s.writes.Load() != 0 {
				t.Fatal("invalid request reached domain", p, w.Code)
			}
			w.cleared(t)
		})
	}
}

func TestSchedulerHTTPPreservesBoundaryAndDomainRefusal(t *testing.T) {
	for _, method := range []string{"GET", "HEAD", "PATCH"} {
		for _, code := range []f.Code{f.Unauthenticated, f.SessionRevoked, f.Forbidden, f.NotFound, f.VersionConflict, f.CommitUnknown} {
			t.Run(method+string(code), func(t *testing.T) {
				h, s, b := fixture()
				failure := f.NewFault(code, f.NotStarted)
				if code == f.CommitUnknown {
					failure = f.NewFault(code, f.Unknown)
				}
				if code == f.Unauthenticated || code == f.SessionRevoked {
					b.auth = func(*http.Request) (i.Actor, error) { return i.Actor{}, failure }
				} else {
					s.get = func(context.Context, i.Actor, pc.ProjectID) (pc.SchedulerProject, error) {
						return pc.SchedulerProject{}, failure
					}
					s.update = func(context.Context, i.Actor, f.CommandMeta, pc.ProjectID, pc.UpdateProjectRequest) (pc.ProjectRef, error) {
						return pc.ProjectRef{}, failure
					}
				}
				body := ""
				if method == "PATCH" {
					body = validInput
				}
				w := writer()
				if p := serve(h, w, testRequest(method, testPath, body)); p != nil || w.Code < 400 {
					t.Fatal("error became success", p, w.Code)
				}
				if method == "HEAD" {
					if w.Body.Len() != 0 {
						t.Fatal("HEAD Problem body")
					}
				} else if object(t, w.Body.Bytes())["code"] != string(code) {
					t.Fatal("original problem lost", w.Body.String())
				}
				if (code == f.Unauthenticated || code == f.SessionRevoked) && s.reads.Load()+s.writes.Load() != 0 {
					t.Fatal("auth refusal still called domain")
				}
				w.cleared(t)
			})
		}
	}
	h, s, b := fixture()
	b.check = func(http.ResponseWriter, *http.Request) error { return f.NewFault(f.CSRFFailed, f.NotStarted) }
	w := writer()
	if p := serve(h, w, testRequest("PATCH", testPath, validInput)); p != nil || s.writes.Load() != 0 || w.Code != 403 {
		t.Fatal("browser boundary bypassed", p, w.Code)
	}
}

func TestSchedulerHTTPJoinsOriginalCallCloseAndCancellationCallback(t *testing.T) {
	h, s, _ := fixture()
	entered, releaseCall := make(chan struct{}), make(chan struct{})
	closeEntered, releaseClose := make(chan struct{}), make(chan struct{})
	callbackEntered, releaseCallback := make(chan struct{}), make(chan struct{})
	var callbackOnce sync.Once
	s.get = func(ctx context.Context, _ i.Actor, _ pc.ProjectID) (pc.SchedulerProject, error) {
		close(entered)
		<-ctx.Done()
		<-releaseCall
		return pc.SchedulerProject{}, ctx.Err()
	}
	w := writer()
	w.read = func(at time.Time) error {
		if !at.IsZero() && time.Until(at) <= 0 {
			callbackOnce.Do(func() { close(callbackEntered); <-releaseCallback })
		}
		return nil
	}
	b := &testBody{Reader: strings.NewReader(""), close: func() error { close(closeEntered); <-releaseClose; return nil }}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r := testRequest("GET", testPath, "").WithContext(ctx)
	r.Body = b
	done := make(chan any, 1)
	go func() { done <- serve(h, w, r) }()
	await(t, entered)
	cancel()
	await(t, callbackEntered)
	select {
	case <-done:
		t.Fatal("returned before original call")
	default:
	}
	close(releaseCall)
	close(releaseCallback)
	await(t, closeEntered)
	select {
	case <-done:
		t.Fatal("returned before body close")
	default:
	}
	close(releaseClose)
	select {
	case p := <-done:
		if p != http.ErrAbortHandler {
			t.Fatal("cancellation did not abort", p)
		}
	case <-time.After(time.Second):
		t.Fatal("original request tail did not join")
	}
	if b.closes.Load() != 1 || w.Body.Len() != 0 {
		t.Fatal("duplicate close/late publish")
	}
	w.cleared(t)
}
func await(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(time.Second):
		t.Fatal("control barrier not reached")
	}
}

func TestSchedulerHTTPRejectsShortWriteFlushAndEarlierDeadline(t *testing.T) {
	for _, failure := range []string{"short write", "flush", "close"} {
		t.Run(failure, func(t *testing.T) {
			h, _, _ := fixture()
			w := writer()
			r := testRequest("GET", testPath, "")
			switch failure {
			case "short write":
				w.write = func(p []byte) (int, error) { return len(p) - 1, nil }
			case "flush":
				w.flush = func() error { return errors.New("flush") }
			case "close":
				r.Body = &testBody{Reader: strings.NewReader(""), close: func() error { return errors.New("close") }}
			}
			if p := serve(h, w, r); p != http.ErrAbortHandler {
				t.Fatal("I/O failure swallowed", p)
			}
			w.cleared(t)
		})
	}
	h, s, b := fixture()
	deadline := time.Now().Add(500 * time.Millisecond)
	ctx, cancel := context.WithDeadline(context.Background(), deadline)
	defer cancel()
	b.auth = func(r *http.Request) (i.Actor, error) {
		at, ok := r.Context().Deadline()
		if !ok || !at.Equal(deadline) {
			t.Fatal("parent deadline extended")
		}
		return testActor(), nil
	}
	w := writer()
	if p := serve(h, w, testRequest("PATCH", testPath, validInput).WithContext(ctx)); p != nil || s.writes.Load() != 1 {
		t.Fatal(p)
	}
}
