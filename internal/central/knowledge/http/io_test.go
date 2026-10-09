package knowledgehttp

import (
	"context"
	"errors"
	"github.com/LunaDeerTech/agenteam/internal/central/httpapi"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func joined(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(3 * time.Second):
		t.Fatal("owned operation did not join")
	}
}

type cyclicWriter struct{ http.ResponseWriter }

func (w *cyclicWriter) Unwrap() http.ResponseWriter { return w }

func TestKnowledgeHTTPNativeCapabilityFailureAndIOAbort(t *testing.T) {
	for _, mode := range []string{"unsupported", "cycle", "short", "write", "flush", "close", "clear", "panic"} {
		t.Run(mode, func(t *testing.T) {
			h, b, p := testHandler()
			w := newTestWriter()
			var sink http.ResponseWriter = w
			body := &testBody{Reader: strings.NewReader("")}
			r := httptest.NewRequest("GET", testPath("/knowledge/documents"), nil)
			r.Body = body
			switch mode {
			case "unsupported":
				sink = httptest.NewRecorder()
			case "cycle":
				// Install the server-owned bad adapter after the real outer
				// middleware, at the Work handler's declared boundary.
			case "short":
				w.write = func(p []byte) (int, error) { return len(p) - 1, nil }
			case "write":
				w.write = func([]byte) (int, error) { return 0, errors.New("private write canary") }
			case "flush":
				w.flush = func() error { return errors.New("private flush canary") }
			case "close":
				body.close = func() error { return errors.New("private close canary") }
			case "clear":
				w.setRead = func(at time.Time) error {
					if at.IsZero() {
						return errors.New("private clear canary")
					}
					return nil
				}
			case "panic":
				p.before = func(context.Context) { panic("private panic canary") }
			}
			aborted := false
			if mode == "cycle" {
				aborted = testAbort(func() {
					httpapi.Handler(nil, http.HandlerFunc(func(outer http.ResponseWriter, request *http.Request) { h.ServeHTTP(&cyclicWriter{outer}, request) })).ServeHTTP(w, r)
				})
			} else {
				aborted = serveTest(h, r, sink)
			}
			if !aborted || body.closes.Load() != 1 {
				t.Fatal("I/O failure did not abort with single body ownership")
			}
			if (mode == "unsupported" || mode == "cycle") && (b.checks.Load() != 0 || b.auths.Load() != 0 || p.calls != 0) {
				t.Fatal("capability resolution occurred after authority")
			}
			if strings.Contains(w.Body.String(), "private ") || strings.Contains(w.Body.String(), "problem") {
				t.Fatal("abort wrote replacement Problem")
			}
		})
	}
}
func TestKnowledgeHTTPCancellationCallbackActuallyJoined(t *testing.T) {
	h, _, p := testHandler()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	callback, bodyClosed, release, done := make(chan struct{}), make(chan struct{}), make(chan struct{}), make(chan struct{})
	var once sync.Once
	unpark := func() { once.Do(func() { close(release) }) }
	t.Cleanup(func() { cancel(); unpark(); joined(t, done) })
	var armed, returned, clearedBeforeJoin atomic.Bool
	w := newTestWriter()
	w.setRead = func(at time.Time) error {
		if !at.IsZero() && ctx.Err() != nil && armed.CompareAndSwap(false, true) {
			close(callback)
			<-release
			returned.Store(true)
		}
		if at.IsZero() && !returned.Load() {
			clearedBeforeJoin.Store(true)
		}
		return nil
	}
	p.before = func(context.Context) { cancel(); <-callback }
	body := &testBody{Reader: strings.NewReader(""), close: func() error { close(bodyClosed); return nil }}
	r := httptest.NewRequest("GET", testPath("/knowledge/documents"), nil).WithContext(ctx)
	r.Body = body
	var aborted bool
	go func() { defer close(done); aborted = serveTest(h, r, w) }()
	joined(t, bodyClosed)
	select {
	case <-done:
		t.Fatal("request returned before its active cancellation callback")
	default:
	}
	unpark()
	joined(t, done)
	if !aborted || clearedBeforeJoin.Load() || body.closes.Load() != 1 || w.Body.Len() != 0 {
		t.Fatal("callback/body/output ownership")
	}
	w.cleared(t)
}

func TestKnowledgeHTTPActualCommandTailPreventsReturnAndLatePublication(t *testing.T) {
	h, _, p := testHandler()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	entered, release, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var once sync.Once
	unpark := func() { once.Do(func() { close(release) }) }
	t.Cleanup(func() { cancel(); unpark(); joined(t, done) })
	p.before = func(context.Context) { cancel(); close(entered); <-release }
	w := newTestWriter()
	r := httptest.NewRequest("GET", testPath("/knowledge/documents"), nil).WithContext(ctx)
	var aborted bool
	go func() { defer close(done); aborted = serveTest(h, r, w) }()
	joined(t, entered)
	select {
	case <-done:
		t.Fatal("HTTP abandoned actual library tail")
	default:
	}
	unpark()
	joined(t, done)
	if !aborted || w.Body.Len() != 0 {
		t.Fatal("late publication")
	}
}
