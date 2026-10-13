package artifacthttp

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/httpapi"
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

func TestArtifactHTTPNativeFailuresAbort(t *testing.T) {
	for _, mode := range []string{"unsupported", "cycle", "short", "write", "flush", "body-close", "clear", "panic"} {
		t.Run(mode, func(t *testing.T) {
			h, b, p := testHandler()
			w := newTestWriter()
			var sink http.ResponseWriter = w
			body := &testBody{Reader: strings.NewReader("")}
			r := httptest.NewRequest("GET", testPath(true), nil)
			r.Body = body
			switch mode {
			case "unsupported":
				sink = httptest.NewRecorder()
			case "short":
				w.write = func(p []byte) (int, error) { return len(p) - 1, nil }
			case "write":
				w.write = func([]byte) (int, error) { return 0, errors.New("private write canary") }
			case "flush":
				w.flush = func() error { return errors.New("private flush canary") }
			case "body-close":
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
			var aborted bool
			if mode == "cycle" {
				aborted = testAbort(func() {
					httpapi.Handler(nil, http.HandlerFunc(func(outer http.ResponseWriter, r *http.Request) { h.ServeHTTP(&cyclicWriter{outer}, r) })).ServeHTTP(w, r)
				})
			} else {
				aborted = serveTest(h, r, sink)
			}
			if !aborted || body.closes.Load() != 1 {
				t.Fatal("native failure did not abort and close exactly once")
			}
			if (mode == "unsupported" || mode == "cycle") && (b.checks != 0 || b.auths != 0 || len(p.invocations) != 0) {
				t.Fatal("native capability failure reached authority")
			}
			if strings.Contains(w.Body.String(), "private ") || strings.Contains(w.Body.String(), "problem") {
				t.Fatal("replacement Problem after abort")
			}
		})
	}
}

func TestArtifactHTTPActualReturnsHeldAfterCancel(t *testing.T) {
	for _, stage := range []string{"original-service", "request-close"} {
		for _, method := range []string{"GET", "HEAD"} {
			t.Run(stage+"-"+method, func(t *testing.T) {
				h, _, p := testHandler()
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				entered, release, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
				var once sync.Once
				unpark := func() { once.Do(func() { close(release) }) }
				t.Cleanup(func() { cancel(); unpark(); joined(t, done) })
				body := &testBody{Reader: strings.NewReader("")}
				barrier := func() { close(entered); <-release }
				if stage == "original-service" {
					p.before = func(context.Context) { barrier() }
				} else {
					body.close = func() error { barrier(); return nil }
				}
				r := httptest.NewRequest(method, testPath(true), nil).WithContext(ctx)
				r.Body = body
				w := newTestWriter()
				var aborted bool
				go func() { defer close(done); aborted = serveTest(h, r, w) }()
				joined(t, entered)
				cancel()
				select {
				case <-done:
					t.Fatal("HTTP escaped original return")
				default:
				}
				// No write can start while the original service/body call is held.
				if w.Body.Len() != 0 || w.Header().Get("Content-Type") != "" {
					t.Fatal("published before owned return")
				}
				unpark()
				joined(t, done)
				if !aborted || w.Body.Len() != 0 || body.closes.Load() != 1 {
					t.Fatal("cancelled late return published")
				}
				w.cleared(t, false)
			})
		}
	}
}

func TestArtifactHTTPCancellationCallbackJoined(t *testing.T) {
	h, _, p := testHandler()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	callback, closed, release, done := make(chan struct{}), make(chan struct{}), make(chan struct{}), make(chan struct{})
	var once sync.Once
	unpark := func() { once.Do(func() { close(release) }) }
	t.Cleanup(func() { cancel(); unpark(); joined(t, done) })
	var armed atomic.Bool
	w := newTestWriter()
	w.setRead = func(at time.Time) error {
		if !at.IsZero() && ctx.Err() != nil && armed.CompareAndSwap(false, true) {
			close(callback)
			<-release
		}
		return nil
	}
	p.before = func(context.Context) { cancel(); <-callback }
	body := &testBody{Reader: strings.NewReader(""), close: func() error { close(closed); return nil }}
	r := httptest.NewRequest("GET", testPath(true), nil).WithContext(ctx)
	r.Body = body
	var aborted bool
	go func() { defer close(done); aborted = serveTest(h, r, w) }()
	joined(t, closed)
	select {
	case <-done:
		t.Fatal("callback abandoned")
	default:
	}
	unpark()
	joined(t, done)
	if !aborted || w.Body.Len() != 0 || body.closes.Load() != 1 {
		t.Fatal("callback ownership")
	}
	w.cleared(t, false)
}

func TestArtifactHTTPEntryBudgetPreservesEarlierDeadline(t *testing.T) {
	for _, earlier := range []bool{false, true} {
		h, _, p := testHandler()
		ctx := context.Background()
		var cancel context.CancelFunc
		var expected time.Time
		if earlier {
			expected = time.Now().Add(time.Second)
			ctx, cancel = context.WithDeadline(ctx, expected)
			defer cancel()
		}
		start := time.Now()
		p.before = func(ctx context.Context) {
			deadline, ok := ctx.Deadline()
			if !ok || deadline.After(start.Add(2*time.Second+100*time.Millisecond)) {
				t.Fatal("entry budget missing")
			}
			if earlier && !deadline.Equal(expected) {
				t.Fatal("earlier caller deadline replaced")
			}
		}
		w := newTestWriter()
		if serveTest(h, httptest.NewRequest("GET", testPath(true), nil).WithContext(ctx), w) || w.Code != 200 {
			t.Fatal("bounded normal request failed")
		}
	}
}
