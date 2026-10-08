package audithttp

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

	c "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/httpapi"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

const projectAuditTestPath = "/api/v1/projects/" + projectAuditWireID + "/audit"

type projectAuditBoundaryTest struct{ testBoundary }

func (b *projectAuditBoundaryTest) RequireHuman(r *http.Request) (identity.Actor, error) {
	return b.RequireSystem(r, identity.Read)
}

type projectAuditReaderTest struct {
	calls atomic.Int32
	list  func(context.Context, c.Filter, foundation.PageRequest) (foundation.Page[c.SafeRecord], error)
	get   func(context.Context, c.ID) (c.SafeRecord, error)
}

func (s *projectAuditReaderTest) ListProject(ctx context.Context, _ identity.Actor, p identity.ProjectID, f c.Filter, page foundation.PageRequest) (foundation.Page[c.SafeRecord], error) {
	s.calls.Add(1)
	if p.String() != projectAuditWireID {
		return foundation.Page[c.SafeRecord]{}, errors.New("wrong project")
	}
	if s.list != nil {
		return s.list(ctx, f, page)
	}
	return foundation.Page[c.SafeRecord]{}, nil
}
func (s *projectAuditReaderTest) GetProject(ctx context.Context, _ identity.Actor, _ identity.ProjectID, id c.ID) (c.SafeRecord, error) {
	s.calls.Add(1)
	if s.get != nil {
		return s.get(ctx, id)
	}
	return c.SafeRecord{}, fault(foundation.NotFound)
}
func projectAuditServe(w http.ResponseWriter, r *http.Request, s *projectAuditReaderTest, b *projectAuditBoundaryTest) bool {
	return aborts(func() { httpapi.Handler(nil, &projectAuditHTTP{s, b}).ServeHTTP(w, r) })
}
func TestProjectAuditHTTPPureDispatchAndProjection(t *testing.T) {
	for _, tc := range []struct {
		method, path        string
		status, auth, reads int
	}{{"GET", projectAuditTestPath, 200, 1, 1}, {"HEAD", projectAuditTestPath, 200, 1, 1}, {"POST", projectAuditTestPath, 405, 0, 0}, {"OPTIONS", projectAuditTestPath + "/" + wireID, 405, 0, 0}, {"GET", projectAuditTestPath + "/unknown", 400, 1, 0}, {"GET", projectAuditTestPath + "/" + wireID + "/child", 404, 0, 0}, {"GET", projectAuditTestPath + "?", 400, 1, 0}, {"GET", projectAuditTestPath + "?scope=system", 400, 1, 0}, {"GET", projectAuditTestPath + "?limit=0", 400, 1, 0}, {"GET", projectAuditTestPath + "?limit=01", 400, 1, 0}, {"GET", projectAuditTestPath + "?limit=200&limit=1", 400, 1, 0}, {"GET", projectAuditTestPath + "?limit=200", 200, 1, 1}, {"HEAD", projectAuditTestPath + "?bad=1", 400, 1, 0}, {"GET", projectAuditTestPath + "/" + wireID, 404, 1, 1}} {
		t.Run(tc.method+tc.path, func(t *testing.T) {
			s, b := &projectAuditReaderTest{}, &projectAuditBoundaryTest{}
			w := newBudgetWriter()
			r := httptest.NewRequest(tc.method, tc.path, nil)
			if projectAuditServe(w, r, s, b) || w.Code != tc.status || int(b.calls.Load()) != tc.auth || int(s.calls.Load()) != tc.reads {
				t.Fatal("dispatch/order", w.Code, b.calls.Load(), s.calls.Load())
			}
			if tc.status == 405 && w.Header().Get("Allow") != "GET, HEAD" {
				t.Fatal("Allow")
			}
			if tc.method == "HEAD" && w.Body.Len() != 0 {
				t.Fatal("HEAD body")
			}
			if tc.status == 200 && w.Header().Get("Content-Length") != "31" {
				t.Fatal("GET/HEAD representation length", w.Header())
			}
			if w.flushes != 1 || !w.reads[len(w.reads)-1].IsZero() || !w.writes[len(w.writes)-1].IsZero() {
				t.Fatal("successful response deadlines not cleared")
			}
		})
	}
	for _, name := range []string{"check-before-routing", "authentication-before-query", "unknown-zero-candidate", "invalid-safe-record", "head-detail", "head-problem"} {
		t.Run(name, func(t *testing.T) {
			s, b := &projectAuditReaderTest{}, &projectAuditBoundaryTest{}
			path, method := projectAuditTestPath, "GET"
			status := http.StatusOK
			r := projectAuditTestRecord(t, projectAuditTestCases()[0], identity.Human, false)
			switch name {
			case "check-before-routing":
				b.check = func(http.ResponseWriter, *http.Request) error { return fault(foundation.InvalidArgument) }
				path += "/bogus/extra"
				status = 400
			case "authentication-before-query":
				b.auth = func(*http.Request) (identity.Actor, error) {
					return identity.Actor{}, fault(foundation.Unauthenticated)
				}
				path += "?bogus=1"
				status = 401
			case "unknown-zero-candidate":
				s.list = func(context.Context, c.Filter, foundation.PageRequest) (foundation.Page[c.SafeRecord], error) {
					return foundation.Page[c.SafeRecord]{Items: []c.SafeRecord{r}}, foundation.NewFault(foundation.CommitUnknown, foundation.Unknown).WithCause(errors.New("private-cause-canary"))
				}
				status = 503
			case "invalid-safe-record":
				r.Scope = identity.SystemScope()
				s.list = func(context.Context, c.Filter, foundation.PageRequest) (foundation.Page[c.SafeRecord], error) {
					return foundation.Page[c.SafeRecord]{Items: []c.SafeRecord{r}}, nil
				}
				status = 503
			case "head-detail":
				method = "HEAD"
				path += "/" + r.AuditID.String()
				s.get = func(context.Context, c.ID) (c.SafeRecord, error) { return r, nil }
			case "head-problem":
				method = "HEAD"
				path += "/" + r.AuditID.String()
				status = 404
			}
			w := newBudgetWriter()
			if projectAuditServe(w, httptest.NewRequest(method, path, nil), s, b) || w.Code != status {
				t.Fatal("result", w.Code, status)
			}
			if method == "HEAD" && w.Body.Len() != 0 {
				t.Fatal("HEAD leaked body")
			}
			if status != 200 && strings.Contains(w.Body.String(), "audit_id") {
				t.Fatal("error leaked candidate")
			}
			if name == "unknown-zero-candidate" {
				var v map[string]any
				json.Unmarshal(w.Body.Bytes(), &v)
				if v["code"] != "COMMIT_UNKNOWN" || v["commit_state"] != "unknown" || v["retry_hint"] != "lookup" || bytes.Contains(w.Body.Bytes(), []byte("private-cause-canary")) {
					t.Fatal("Unknown projection")
				}
			}
		})
	}
	if _, err := NewProjectHTTPHandler(nil, nil); err == nil {
		t.Fatal("unbound constructor")
	}
	t.Run("tracked-problem-code", func(t *testing.T) {
		var logs bytes.Buffer
		w := newBudgetWriter()
		s := &projectAuditReaderTest{list: func(context.Context, c.Filter, foundation.PageRequest) (foundation.Page[c.SafeRecord], error) {
			return foundation.Page[c.SafeRecord]{}, foundation.NewFault(foundation.CommitUnknown, foundation.Unknown)
		}}
		h := httpapi.Handler(slog.New(slog.NewJSONHandler(&logs, nil)), &projectAuditHTTP{s, &projectAuditBoundaryTest{}})
		h.ServeHTTP(w, httptest.NewRequest("GET", projectAuditTestPath+"?cursor=private-canary", nil))
		if !strings.Contains(logs.String(), `"code":"COMMIT_UNKNOWN"`) || strings.Count(logs.String(), `"event":"http_request"`) != 1 || strings.Contains(logs.String(), "private-canary") {
			t.Fatal("tracked writer state/log changed", logs.String())
		}
	})
}

type projectAuditIOTest struct {
	*budgetWriter
	read, write func(time.Time) error
	flush       func() error
}

func (w *projectAuditIOTest) SetReadDeadline(at time.Time) error {
	if w.read != nil {
		return w.read(at)
	}
	return w.budgetWriter.SetReadDeadline(at)
}
func (w *projectAuditIOTest) SetWriteDeadline(at time.Time) error {
	if w.write != nil {
		return w.write(at)
	}
	return w.budgetWriter.SetWriteDeadline(at)
}
func (w *projectAuditIOTest) FlushError() error {
	if w.flush != nil {
		return w.flush()
	}
	return w.budgetWriter.FlushError()
}

type projectAuditWrap struct {
	http.ResponseWriter
	next func() http.ResponseWriter
}

func (w projectAuditWrap) Unwrap() http.ResponseWriter { return w.next() }

type projectAuditNoFlush struct {
	header      http.Header
	read, write int
}

func (w *projectAuditNoFlush) Header() http.Header              { return w.header }
func (*projectAuditNoFlush) WriteHeader(int)                    {}
func (*projectAuditNoFlush) Write(p []byte) (int, error)        { return len(p), nil }
func (w *projectAuditNoFlush) SetReadDeadline(time.Time) error  { w.read++; return nil }
func (w *projectAuditNoFlush) SetWriteDeadline(time.Time) error { w.write++; return nil }
func TestProjectAuditHTTPPureActualTail(t *testing.T) {
	for _, name := range []string{"nonempty-body", "false-zero-content-length", "missing-flush", "unwrap-cycle", "unwrap-panic", "read-setter-panic", "write-setter-panic", "close-panic", "short-write", "flush-panic", "flush-error", "committed-no-second-problem"} {
		t.Run(name, func(t *testing.T) {
			s, b := &projectAuditReaderTest{}, &projectAuditBoundaryTest{}
			under := newBudgetWriter()
			body := &closeBody{Reader: strings.NewReader("")}
			var w http.ResponseWriter = under
			r := httptest.NewRequest("GET", projectAuditTestPath, nil)
			r.Body = body
			abort := true
			status := 0
			switch name {
			case "nonempty-body":
				body.Reader = strings.NewReader("x")
				r.ContentLength = 1
				abort = false
				status = 400
			case "false-zero-content-length":
				body.Reader = strings.NewReader("x")
				abort = false
				status = 400
			case "missing-flush":
				w = &projectAuditNoFlush{header: make(http.Header)}
			case "unwrap-cycle":
				var cycle *projectAuditWrap
				cycle = &projectAuditWrap{ResponseWriter: under, next: func() http.ResponseWriter { return cycle }}
				w = cycle
			case "unwrap-panic":
				w = projectAuditWrap{under, func() http.ResponseWriter { panic("private-canary") }}
			case "read-setter-panic":
				w = &projectAuditIOTest{budgetWriter: under, read: func(time.Time) error { panic("private-canary") }}
			case "write-setter-panic":
				w = &projectAuditIOTest{budgetWriter: under, write: func(time.Time) error { panic("private-canary") }}
			case "close-panic":
				body.close = func() error { panic("private-canary") }
			case "short-write":
				under.short = true
			case "flush-panic":
				w = &projectAuditIOTest{budgetWriter: under, flush: func() error { panic("private-canary") }}
			case "flush-error":
				under.flushErr = errors.New("flush-error")
			case "committed-no-second-problem":
				b.check = func(w http.ResponseWriter, _ *http.Request) error {
					w.WriteHeader(202)
					return fault(foundation.InvalidArgument)
				}
			}
			// Malformed unwrap graphs are injected AFTER formal RequestID assignment;
			// this probes this handler's ownership, not legacy middleware's cycle handling.
			got := aborts(func() {
				httpapi.WithRequestID(nil, http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) { (&projectAuditHTTP{s, b}).ServeHTTP(w, request) })).ServeHTTP(under, r)
			})
			if name == "committed-no-second-problem" {
				under = newBudgetWriter()
				body = &closeBody{Reader: strings.NewReader("")}
				r.Body = body
				got = aborts(func() { httpapi.Handler(nil, &projectAuditHTTP{s, b}).ServeHTTP(under, r) })
			}
			if got != abort || body.closed.Load() != 1 {
				t.Fatal("abort/close ownership", got, body.closed.Load())
			}
			if status != 0 && under.Code != status {
				t.Fatal("body error", under.Code)
			}
			if strings.Contains(under.Body.String(), "private-canary") {
				t.Fatal("panic exposed")
			}
			if name == "missing-flush" || strings.HasPrefix(name, "unwrap-") || strings.Contains(name, "setter-panic") {
				if s.calls.Load() != 0 || b.calls.Load() != 0 {
					t.Fatal("work before capabilities")
				}
			}
			if name == "committed-no-second-problem" && (under.Code != 202 || under.Body.Len() != 0) {
				t.Fatal("second problem after commit")
			}
		})
	}
	for _, phase := range []string{"body-close", "cancel-read-setter", "cancel-write-setter"} {
		t.Run(phase, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			entered, release, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
			var once sync.Once
			var result bool
			under := newBudgetWriter()
			w := &projectAuditIOTest{budgetWriter: under}
			body := &closeBody{Reader: strings.NewReader("")}
			s := &projectAuditReaderTest{}
			b := &projectAuditBoundaryTest{}
			if phase == "body-close" {
				body.close = func() error { close(entered); <-release; return nil }
			} else {
				f := func(at time.Time) error {
					if !at.IsZero() && time.Until(at) < time.Second {
						once.Do(func() { close(entered) })
						<-release
						panic("callback-panic-canary")
					}
					return nil
				}
				if phase == "cancel-read-setter" {
					w.read = f
				} else {
					w.write = f
				}
				b.auth = func(r *http.Request) (identity.Actor, error) {
					cancel()
					<-entered
					return identity.Actor{}, r.Context().Err()
				}
			}
			r := httptest.NewRequest("GET", projectAuditTestPath, nil).WithContext(ctx)
			r.Body = body
			var releaseOnce sync.Once
			go func() { defer close(done); result = projectAuditServe(w, r, s, b) }()
			t.Cleanup(func() { cancel(); releaseOnce.Do(func() { close(release) }); <-done })
			select {
			case <-entered:
			case <-time.After(time.Second):
				t.Fatal("owned checkpoint")
			}
			cancel()
			select {
			case <-done:
				t.Fatal("returned before actual owned join")
			default:
			}
			releaseOnce.Do(func() { close(release) })
			<-done
			if !result || body.closed.Load() != 1 || under.Body.Len() != 0 {
				t.Fatal("cancelled tail publication/close")
			}
		})
	}
	t.Run("natural-three-seconds-and-earlier-parent", func(t *testing.T) {
		for _, budget := range []time.Duration{10 * time.Second, 30 * time.Millisecond} {
			ctx, cancel := context.WithTimeout(context.Background(), budget)
			body := &closeBody{Reader: strings.NewReader("")}
			s := &projectAuditReaderTest{}
			b := &projectAuditBoundaryTest{}
			var startDeadline time.Time
			b.auth = func(r *http.Request) (identity.Actor, error) {
				startDeadline, _ = r.Context().Deadline()
				<-r.Context().Done()
				return identity.Actor{}, r.Context().Err()
			}
			w := newBudgetWriter()
			r := httptest.NewRequest("GET", projectAuditTestPath, nil).WithContext(ctx)
			r.Body = body
			start := time.Now()
			aborted := projectAuditServe(w, r, s, b)
			elapsed := time.Since(start)
			cancel()
			if !aborted || s.calls.Load() != 0 || body.closed.Load() != 1 || w.Body.Len() != 0 || elapsed > 4*time.Second || startDeadline.After(start.Add(projectAuditHTTPBudget+10*time.Millisecond)) {
				t.Fatal("original preauth budget", elapsed)
			}
			if budget > 3*time.Second && elapsed < 2900*time.Millisecond {
				t.Fatal("natural timer not reached")
			}
		}
	})
	t.Run("problem-head-length-is-not-body", func(t *testing.T) {
		w := newBudgetWriter()
		r := httptest.NewRequest("HEAD", projectAuditTestPath, nil)
		s := &projectAuditReaderTest{list: func(context.Context, c.Filter, foundation.PageRequest) (foundation.Page[c.SafeRecord], error) {
			return foundation.Page[c.SafeRecord]{}, fault(foundation.InvalidState)
		}}
		if projectAuditServe(w, r, s, &projectAuditBoundaryTest{}) || w.Code != 409 || w.Body.Len() != 0 {
			t.Fatal("HEAD problem behavior")
		}
		if v := w.Header().Get("Content-Length"); v != "" {
			if n, e := strconv.Atoi(v); e != nil || n < 0 {
				t.Fatal("invalid length")
			}
		}
	})
}

var _ io.ReadCloser = (*closeBody)(nil)
