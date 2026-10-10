package projectvariablehttp

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

	c "github.com/LunaDeerTech/agenteam/internal/central/projectvariable/contract"
)

func TestSecretHTTPOwnedCallAndCancellationJoin(t *testing.T) {
	h, _, p := secretTestHandler(t)
	p.result = secretTestReceipt(t, c.SecretCreateCommand, true, 1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	entered, release, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var once sync.Once
	unpark := func() { once.Do(func() { close(release) }) }
	t.Cleanup(func() { cancel(); unpark(); joined(t, done) })
	p.before = func(context.Context) {
		cancel()
		close(entered)
		<-release
		if err := p.create.UseValue(func([]byte) error { return nil }); err != nil {
			t.Error("material destroyed before actual library return")
		}
	}
	w := newTestWriter()
	var aborted bool
	go func() {
		defer close(done)
		aborted = serveTest(h, commandRequest("POST", "/secret-variables", secretCreateBody("owned-material")).WithContext(ctx), w)
	}()
	joined(t, entered)
	select {
	case <-done:
		t.Fatal("abandoned original library call")
	default:
	}
	unpark()
	joined(t, done)
	if !aborted || w.Body.Len() != 0 || p.create.UseValue(func([]byte) error { return nil }) == nil {
		t.Fatal("late publication or retained material")
	}

	h, _, p = secretTestHandler(t)
	ctx, cancel = context.WithCancel(context.Background())
	defer cancel()
	callback, closed, callbackRelease, callbackDone := make(chan struct{}), make(chan struct{}), make(chan struct{}), make(chan struct{})
	var callbackOnce sync.Once
	releaseCallback := func() { callbackOnce.Do(func() { close(callbackRelease) }) }
	t.Cleanup(func() { cancel(); releaseCallback(); joined(t, callbackDone) })
	var armed, returned, earlyClear atomic.Bool
	w = newTestWriter()
	w.setRead = func(at time.Time) error {
		if !at.IsZero() && ctx.Err() != nil && armed.CompareAndSwap(false, true) {
			close(callback)
			<-callbackRelease
			returned.Store(true)
		}
		if at.IsZero() && !returned.Load() {
			earlyClear.Store(true)
		}
		return nil
	}
	p.before = func(context.Context) { cancel(); <-callback }
	body := &testBody{Reader: strings.NewReader(""), close: func() error { close(closed); return nil }}
	r := httptest.NewRequest("GET", testPath("/secret-variables"), nil).WithContext(ctx)
	r.Body = body
	go func() { defer close(callbackDone); aborted = serveTest(h, r, w) }()
	joined(t, closed)
	select {
	case <-callbackDone:
		t.Fatal("callback was not joined")
	default:
	}
	releaseCallback()
	joined(t, callbackDone)
	if !aborted || earlyClear.Load() || w.Body.Len() != 0 || body.closes.Load() != 1 {
		t.Fatal("callback ownership")
	}
}

func TestSecretHTTPIOFailuresAndBudgets(t *testing.T) {
	for _, mode := range []string{"unsupported", "short-write", "flush", "close"} {
		h, b, p := secretTestHandler(t)
		w := newTestWriter()
		var sink http.ResponseWriter = w
		body := &testBody{Reader: strings.NewReader("")}
		r := httptest.NewRequest("GET", testPath("/secret-variables"), nil)
		r.Body = body
		switch mode {
		case "unsupported":
			sink = httptest.NewRecorder()
		case "short-write":
			w.write = func(raw []byte) (int, error) { return len(raw) - 1, nil }
		case "flush":
			w.flush = func() error { return errors.New("private-io-canary") }
		case "close":
			body.close = func() error { return errors.New("private-io-canary") }
		}
		if !serveTest(h, r, sink) || body.closes.Load() != 1 || strings.Contains(w.Body.String(), "private-io-canary") {
			t.Fatal("I/O ownership", mode)
		}
		if mode == "unsupported" && (b.checks.Load() != 0 || p.calls != 0) {
			t.Fatal("capability check came after authority")
		}
	}
	for _, tc := range []struct {
		method, path, body string
		budget             time.Duration
	}{{"GET", "/secret-variables", "", readBudget}, {"POST", "/secret-variables/commands/lookup", secretLookupBody(c.SecretCreateCommand), readBudget}, {"POST", "/secret-variables", secretCreateBody("owned-material"), mutationBudget}} {
		h, b, p := secretTestHandler(t)
		p.result = secretTestReceipt(t, c.SecretCreateCommand, true, 1)
		p.lookup, _ = c.NewSecretVariableCommandLookup(c.SecretLookupNotObserved, nil)
		start := time.Now()
		b.check = func(r *http.Request) error {
			deadline, ok := r.Context().Deadline()
			if !ok || deadline.Sub(start) < tc.budget-time.Second || deadline.Sub(start) > tc.budget+time.Second {
				t.Fatal("budget did not start before authority")
			}
			return nil
		}
		w := newTestWriter()
		r := commandRequest(tc.method, tc.path, tc.body)
		if tc.method == "GET" {
			r.Body = http.NoBody
			r.ContentLength = 0
		}
		if serveTest(h, r, w) || w.Code != 200 {
			t.Fatal("budget fixture", w.Code)
		}
	}
}
