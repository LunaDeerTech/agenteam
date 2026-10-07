package projecthttp

import (
	"context"
	"errors"
	"github.com/LunaDeerTech/agenteam/internal/central/account"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/httpapi"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/project"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type updateServices struct {
	update           func(context.Context, id.Actor, f.CommandMeta, pc.ProjectID, pc.UpdateProjectRequest) (pc.ProjectRef, error)
	lookup           func(context.Context, id.Actor, pc.CommandLookupRequest) (pc.CommandLookupResult, error)
	updates, lookups atomic.Int32
}

func (s *updateServices) UpdateProject(c context.Context, a id.Actor, m f.CommandMeta, p pc.ProjectID, r pc.UpdateProjectRequest) (pc.ProjectRef, error) {
	s.updates.Add(1)
	if s.update != nil {
		return s.update(c, a, m, p, r)
	}
	return wireProject(), nil
}
func (s *updateServices) LookupCommand(c context.Context, a id.Actor, r pc.CommandLookupRequest) (pc.CommandLookupResult, error) {
	s.lookups.Add(1)
	if s.lookup != nil {
		return s.lookup(c, a, r)
	}
	p := wireProject()
	return pc.CommandLookupResult{State: pc.LookupCommitted, Result: &pc.CommandResult{Command: pc.UpdateCommand, Project: &p}}, nil
}
func updateFixture() (*projectUpdateHTTP, *handlerBoundary, *updateServices) {
	b := &handlerBoundary{}
	s := &updateServices{}
	return &projectUpdateHTTP{s, b}, b, s
}
func updateRequest(method, path, body string) *http.Request {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Idempotency-Key", "original-key")
	return r
}
func updateServe(h http.Handler, w http.ResponseWriter, r *http.Request) bool {
	return handlerAborts(func() { httpapi.WithRequestID(nil, h).ServeHTTP(w, r) })
}

const updateTestBody = `{"expected_version":"1","description":"test"}`

type updateUnwrapper struct {
	http.ResponseWriter
	next http.ResponseWriter
}

func (w *updateUnwrapper) Unwrap() http.ResponseWriter { return w.next }

type updateFlusher struct{ handlerNoFlush }

func (w updateFlusher) Flush() { _ = w.w.FlushError() }

type updateObservedWriter struct {
	http.ResponseWriter
	headers *int
}

func (w updateObservedWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }
func (w updateObservedWriter) WriteHeader(code int) {
	*w.headers++
	w.ResponseWriter.WriteHeader(code)
}

type updateReadUnwrapper struct {
	*updateUnwrapper
	called *int
}

func (w updateReadUnwrapper) SetReadDeadline(time.Time) error { *w.called++; return nil }

type updateWriteUnwrapper struct {
	*updateUnwrapper
	called *int
}

func (w updateWriteUnwrapper) SetWriteDeadline(time.Time) error { *w.called++; return nil }

type updateBothFlush struct {
	*handlerWriter
	flusher *int
}

func (w updateBothFlush) Flush() { *w.flusher++ }

type updatePanicUnwrapper struct{ handlerNoFlush }

func (w updatePanicUnwrapper) Unwrap() http.ResponseWriter { panic("private unwrap") }

func TestProjectOwnerUpdatePureIOCapabilitiesBeforeService(t *testing.T) {
	for _, lookup := range []bool{false, true} {
		for _, mode := range []string{"missing", "cycle", "cycle-with-deadlines", "nil-unwrap", "panic-unwrap", "FlushError", "Flusher", "wrapped"} {
			t.Run(map[bool]string{false: "PATCH", true: "lookup"}[lookup]+"/"+mode, func(t *testing.T) {
				h, b, s := updateFixture()
				method, path, body := "PATCH", httpDetailPath, updateTestBody
				if lookup {
					method, path, body = "POST", path+updateLookupSuffix, `{"command":"update"}`
				}
				base := newHandlerWriter()
				writes, headers, auth := 0, 0, 0
				base.write = func(p []byte) (int, error) { writes++; return base.ResponseRecorder.Write(p) }
				base.flush = func() error {
					if s.updates.Load()+s.lookups.Load() != 1 || writes != 1 {
						t.Fatal("preflight performed a flush")
					}
					return nil
				}
				b.auth = func(*http.Request) (id.Actor, error) { auth++; return wireActor(), nil }
				var writer http.ResponseWriter = base
				allowed := mode == "FlushError" || mode == "Flusher" || mode == "wrapped"
				switch mode {
				case "missing":
					writer = handlerNoFlush{base}
				case "cycle", "cycle-with-deadlines":
					cycle := &updateUnwrapper{ResponseWriter: base}
					cycle.next = cycle
					writer = cycle
					if mode == "cycle-with-deadlines" {
						writer = &updateDeadlineUnwrapper{handlerNoFlush{base}, cycle}
					}
				case "nil-unwrap":
					writer = &updateUnwrapper{ResponseWriter: base}
				case "panic-unwrap":
					writer = updatePanicUnwrapper{handlerNoFlush{base}}
				case "Flusher":
					writer = updateFlusher{handlerNoFlush{base}}
				case "wrapped":
					writer = &updateUnwrapper{ResponseWriter: base, next: &updateUnwrapper{ResponseWriter: base, next: base}}
				}
				writer = updateObservedWriter{writer, &headers}
				r := updateRequest(method, path, body)
				stream := &handlerBody{Reader: strings.NewReader(body)}
				r.Body = stream
				// Allocate the formal request identity before injecting a malformed
				// writer into this handler. The shared middleware's own Unwrap walk
				// is outside this handler capability/Close test.
				aborted := handlerAborts(func() {
					httpapi.WithRequestID(nil, http.HandlerFunc(func(_ http.ResponseWriter, tagged *http.Request) {
						h.ServeHTTP(writer, tagged)
					})).ServeHTTP(base, r)
				})
				if stream.closes.Load() != 1 {
					t.Fatal("original body not closed exactly once")
				}
				if allowed {
					if aborted || s.updates.Load()+s.lookups.Load() != 1 || auth != 1 || writes != 1 || headers != 1 || base.flushes != 1 {
						t.Fatal("supported capability changed execution")
					}
					base.assertCleared(t)
				} else if !aborted || s.updates.Load()+s.lookups.Load() != 0 || auth != 0 || writes != 0 || headers != 0 || base.Body.Len() != 0 || base.flushes != 0 || base.Header().Get("Content-Type") != "" {
					t.Fatal("unsupported capability reached auth/service/publication")
				}
				if mode == "panic-unwrap" || mode == "cycle-with-deadlines" || mode == "missing" {
					base.assertCleared(t)
				}
			})
		}
	}
	t.Run("per-capability-receiver-and-priority", func(t *testing.T) {
		base := newHandlerWriter()
		reads, writes, flusher := 0, 0, 0
		lower := updateBothFlush{base, &flusher}
		middle := updateWriteUnwrapper{&updateUnwrapper{ResponseWriter: base, next: lower}, &writes}
		upper := updateReadUnwrapper{&updateUnwrapper{ResponseWriter: base, next: middle}, &reads}
		io := &updateIOWriter{ResponseWriter: upper}
		ready := io.prepare()
		if !ready || reads+writes+flusher+base.flushes != 0 || len(base.reads)+len(base.writes) != 0 {
			t.Fatal("preflight performed I/O")
		}
		if io.SetReadDeadline(time.Now()) != nil || io.SetWriteDeadline(time.Now()) != nil || io.FlushError() != nil || reads != 1 || writes != 1 || flusher != 0 || base.flushes != 1 || len(base.reads)+len(base.writes) != 0 {
			t.Fatal("capability layer or FlushError priority changed")
		}
	})
}

type updateDeadlineUnwrapper struct {
	handlerNoFlush
	next http.ResponseWriter
}

func (w *updateDeadlineUnwrapper) Unwrap() http.ResponseWriter { return w.next }

func TestProjectOwnerUpdatePureRoutesAndCalls(t *testing.T) {
	for _, v := range []struct {
		s *project.Service
		b *account.HTTPBoundary
	}{{nil, &account.HTTPBoundary{}}, {&project.Service{}, nil}} {
		if h, e := NewUpdateHTTPHandler(v.s, v.b); e == nil || h != nil {
			t.Fatal("missing provider")
		}
	}
	for _, tc := range []struct {
		method, path string
		capture      bool
	}{{"PATCH", httpDetailPath, true}, {"DELETE", httpDetailPath, true}, {"GET", httpDetailPath, false}, {"HEAD", httpDetailPath, false}, {"POST", httpDetailPath + updateLookupSuffix, true}, {"GET", httpDetailPath + updateLookupSuffix, true}, {"PATCH", resolvePath, false}, {"POST", projectsPath, false}, {"POST", httpDetailPath + "/archive", false}, {"PATCH", httpDetailPath + "/", false}} {
		if HandlesUpdateRequest(tc.method, tc.path) != tc.capture {
			t.Fatal(tc)
		}
	}
	for _, lookup := range []bool{false, true} {
		t.Run(map[bool]string{false: "PATCH", true: "lookup"}[lookup], func(t *testing.T) {
			h, b, s := updateFixture()
			method, path, body := "PATCH", httpDetailPath, updateTestBody
			if lookup {
				method, path, body = "POST", path+updateLookupSuffix, `{"command":"update"}`
			}
			var auth context.Context
			b.auth = func(r *http.Request) (id.Actor, error) { auth = r.Context(); return wireActor(), nil }
			verify := func(ctx context.Context, a id.Actor) {
				if ctx != auth || !a.Equal(wireActor()) {
					t.Fatal("actor/context replaced")
				}
			}
			s.update = func(ctx context.Context, a id.Actor, m f.CommandMeta, p pc.ProjectID, r pc.UpdateProjectRequest) (pc.ProjectRef, error) {
				verify(ctx, a)
				if m.Validate() != nil || m.RequestID != httpapi.RequestID(ctx) || m.IdempotencyKey != "original-key" || *m.ExpectedVersion != 1 || p != wireProject().ID || r.Name != nil || r.Description == nil {
					t.Fatal("meta/presence changed")
				}
				return wireProject(), nil
			}
			s.lookup = func(ctx context.Context, a id.Actor, r pc.CommandLookupRequest) (pc.CommandLookupResult, error) {
				verify(ctx, a)
				if r.Command != pc.UpdateCommand || r.Key != "original-key" || r.ProjectID != wireProject().ID {
					t.Fatal("lookup widened")
				}
				return pc.CommandLookupResult{State: pc.LookupNotObserved}, nil
			}
			w := newHandlerWriter()
			if updateServe(h, w, updateRequest(method, path, body)) || w.Code != 200 || s.updates.Load()+s.lookups.Load() != 1 || w.Header().Get("Content-Length") == "" {
				t.Fatal("call/result")
			}
			w.assertCleared(t)
		})
	}
	for _, tc := range []struct{ method, path, allow string }{{"DELETE", httpDetailPath, "GET, HEAD, PATCH"}, {"HEAD", httpDetailPath + updateLookupSuffix, "POST"}} {
		h, _, s := updateFixture()
		w := newHandlerWriter()
		if updateServe(h, w, updateRequest(tc.method, tc.path, "{}")) || w.Code != 405 || w.Header().Get("Allow") != tc.allow || s.updates.Load()+s.lookups.Load() != 0 || tc.method == "HEAD" && w.Body.Len() != 0 {
			t.Fatal("method boundary")
		}
	}
}
func TestProjectOwnerUpdatePureRejectBeforeService(t *testing.T) {
	for _, tc := range []struct {
		name, method, path, body string
		change                   func(*http.Request)
	}{
		{"null", "PATCH", httpDetailPath, `{"expected_version":"1","description":null}`, nil},
		{"empty", "PATCH", httpDetailPath, `{"expected_version":"1"}`, nil},
		{"query", "PATCH", httpDetailPath + "?x=1", updateTestBody, nil},
		{"force-query", "PATCH", httpDetailPath + "?", updateTestBody, nil},
		{"id", "PATCH", projectPrefix + "bad", updateTestBody, nil},
		{"foreign-command", "POST", httpDetailPath + updateLookupSuffix, `{"command":"delete"}`, nil},
		{"missing-key", "PATCH", httpDetailPath, updateTestBody, func(r *http.Request) { r.Header.Del("Idempotency-Key") }},
		{"duplicate-key", "PATCH", httpDetailPath, updateTestBody, func(r *http.Request) { r.Header.Add("Idempotency-Key", "other") }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, _, s := updateFixture()
			r := updateRequest(tc.method, tc.path, tc.body)
			if tc.change != nil {
				tc.change(r)
			}
			w := newHandlerWriter()
			if updateServe(h, w, r) || w.Code != 400 || s.updates.Load()+s.lookups.Load() != 0 {
				t.Fatal("strict input reached service", w.Code, w.Body.String())
			}
		})
	}
	h, b, s := updateFixture()
	b.auth = func(*http.Request) (id.Actor, error) { return id.Actor{}, f.NewFault(f.SessionRevoked, f.NotStarted) }
	w := newHandlerWriter()
	if updateServe(h, w, updateRequest("PATCH", httpDetailPath, updateTestBody)) || w.Code != 401 || s.updates.Load() != 0 {
		t.Fatal("authority")
	}
	h, _, s = updateFixture()
	w = newHandlerWriter()
	if !handlerAborts(func() { h.ServeHTTP(w, updateRequest("PATCH", httpDetailPath, updateTestBody)) }) && w.Code != 400 {
		t.Fatal("missing request identity")
	}
	if s.updates.Load() != 0 {
		t.Fatal("missing RequestID reached service")
	}
}
func TestProjectOwnerUpdatePureUnknownAndOwnedTail(t *testing.T) {
	h, _, s := updateFixture()
	cause := f.NewFault(f.CommitUnknown, f.Unknown)
	cause.CauseID = wireID[f.TransactionAttempt]().String()
	cause.RetryHint = "lookup"
	s.update = func(context.Context, id.Actor, f.CommandMeta, pc.ProjectID, pc.UpdateProjectRequest) (pc.ProjectRef, error) {
		return pc.ProjectRef{}, cause
	}
	w := newHandlerWriter()
	if updateServe(h, w, updateRequest("PATCH", httpDetailPath, updateTestBody)) || w.Code != 503 || !strings.Contains(w.Body.String(), `"commit_state":"unknown"`) || strings.Contains(w.Body.String(), "cause_id") {
		t.Fatal("Unknown projection", w.Body.String())
	}
	h, _, s = updateFixture()
	entered, release, done := make(chan struct{}), make(chan struct{}), make(chan bool, 1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.update = func(ctx context.Context, _ id.Actor, _ f.CommandMeta, _ pc.ProjectID, _ pc.UpdateProjectRequest) (pc.ProjectRef, error) {
		close(entered)
		<-ctx.Done()
		<-release
		return wireProject(), nil
	}
	w = newHandlerWriter()
	go func() {
		done <- updateServe(h, w, updateRequest("PATCH", httpDetailPath, updateTestBody).WithContext(ctx))
	}()
	<-entered
	cancel()
	select {
	case <-done:
		t.Fatal("abandoned service tail")
	case <-time.After(25 * time.Millisecond):
	}
	if w.Body.Len() != 0 {
		t.Fatal("early publish")
	}
	close(release)
	if !handlerJoined(t, done) || w.Body.Len() != 0 || s.updates.Load() != 1 {
		t.Fatal("late success/second invocation")
	}
	w.assertCleared(t)
}
func TestProjectOwnerUpdatePureNaturalBudgets(t *testing.T) {
	for _, lookup := range []bool{false, true} {
		t.Run(map[bool]string{false: "natural30s", true: "natural2s"}[lookup], func(t *testing.T) {
			h, b, s := updateFixture()
			limit := updateBudget
			method, path, body := "PATCH", httpDetailPath, updateTestBody
			if lookup {
				limit = readBudget
				method, path, body = "POST", path+updateLookupSuffix, `{"command":"update"}`
			}
			started := time.Now()
			b.auth = func(r *http.Request) (id.Actor, error) {
				deadline, ok := r.Context().Deadline()
				if !ok || deadline.Before(started.Add(limit)) || deadline.After(time.Now().Add(limit)) {
					t.Fatal("deadline not before authentication")
				}
				<-r.Context().Done()
				return id.Actor{}, r.Context().Err()
			}
			w := newHandlerWriter()
			if !updateServe(h, w, updateRequest(method, path, body)) || time.Since(started) < limit || w.Body.Len() != 0 || s.updates.Load()+s.lookups.Load() != 0 {
				t.Fatal("natural publication deadline")
			}
			w.assertCleared(t)
		})
	}
}
func TestProjectOwnerUpdatePureActualIO(t *testing.T) {
	for _, mode := range []string{"short", "write", "flush", "close", "unsupported", "panic", "clear"} {
		t.Run(mode, func(t *testing.T) {
			h, _, s := updateFixture()
			w := newHandlerWriter()
			r := updateRequest("PATCH", httpDetailPath, updateTestBody)
			b := &handlerBody{Reader: strings.NewReader(updateTestBody)}
			r.Body = b
			switch mode {
			case "short":
				w.write = func(p []byte) (int, error) { return len(p) - 1, nil }
			case "write":
				w.write = func([]byte) (int, error) { return 0, io.ErrClosedPipe }
			case "flush":
				w.flush = func() error { return io.ErrClosedPipe }
			case "close":
				b.close = func() error { return io.ErrClosedPipe }
			case "unsupported":
				w.setRead = func(time.Time) error { return http.ErrNotSupported }
			case "panic":
				s.update = func(context.Context, id.Actor, f.CommandMeta, pc.ProjectID, pc.UpdateProjectRequest) (pc.ProjectRef, error) {
					panic("private")
				}
			case "clear":
				w.setWrite = func(at time.Time) error {
					if at.IsZero() {
						return errors.New("clear")
					}
					return nil
				}
			}
			if !updateServe(h, w, r) || b.closes.Load() != 1 || strings.Contains(w.Body.String(), "application/problem") {
				t.Fatal("actual IO terminal", mode, b.closes.Load())
			}
			if mode == "unsupported" && s.updates.Load() != 0 {
				t.Fatal("unsupported deadline called service")
			}
		})
	}
}
