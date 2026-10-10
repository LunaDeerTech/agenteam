package commandhttp

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
)

func receive[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case v := <-ch:
		return v
	case <-time.After(time.Second):
		t.Fatal("owned operation did not return")
		var zero T
		return zero
	}
}
func stillHeld[T any](t *testing.T, ch <-chan T) {
	t.Helper()
	select {
	case <-ch:
		t.Fatal("returned before original tail")
	default:
	}
}

func TestTreeCommandsHTTPOriginalBodyAndCallbackJoin(t *testing.T) {
	t.Run("body_close", func(t *testing.T) {
		h, _, _ := fixture()
		r := request("rename", `{"expected_version":"1","title":"original"}`)
		entered, release := make(chan struct{}), make(chan struct{})
		var once sync.Once
		defer once.Do(func() { close(release) })
		body := &heldBody{Reader: strings.NewReader(`{"expected_version":"1","title":"original"}`), close: func() error { close(entered); <-release; return nil }}
		r.Body = body
		w := writer()
		done := make(chan bool, 1)
		go func() { done <- serve(h, r, w) }()
		receive(t, entered)
		stillHeld(t, done)
		if w.Body.Len() != 0 || body.closes.Load() != 1 {
			t.Fatal("published before original body Close")
		}
		once.Do(func() { close(release) })
		if receive(t, done) || w.Code != 200 || !w.cleared() {
			t.Fatal("original Close did not join")
		}
	})
	t.Run("cancel_callback", func(t *testing.T) {
		h, _, _ := fixture()
		r := request("rename", `{"expected_version":"1","title":"original"}`)
		ctx, cancel := context.WithCancel(r.Context())
		defer cancel()
		r = r.WithContext(ctx)
		entered, release := make(chan struct{}), make(chan struct{})
		var once sync.Once
		defer once.Do(func() { close(release) })
		w := writer()
		var callback sync.Once
		w.read = func(at time.Time) error {
			if !at.IsZero() && !at.After(time.Now()) {
				callback.Do(func() { close(entered); <-release })
			}
			return nil
		}
		body := &heldBody{Reader: strings.NewReader(`{"expected_version":"1","title":"original"}`), close: func() error { cancel(); <-entered; return nil }}
		r.Body = body
		done := make(chan bool, 1)
		go func() { done <- serve(h, r, w) }()
		receive(t, entered)
		stillHeld(t, done)
		once.Do(func() { close(release) })
		if !receive(t, done) || w.Body.Len() != 0 || w.cleared() {
			t.Fatal("cancel callback was abandoned or made reusable")
		}
	})
	t.Run("service_actual_tail", func(t *testing.T) {
		h, s, _ := fixture()
		r := request("rename", `{"expected_version":"1","title":"original"}`)
		ctx, cancel := context.WithCancel(r.Context())
		defer cancel()
		r = r.WithContext(ctx)
		entered, release := make(chan struct{}), make(chan struct{})
		var once sync.Once
		defer once.Do(func() { close(release) })
		s.before = func(ctx context.Context) { close(entered); <-ctx.Done(); <-release }
		s.err = f.NewFault(f.CommitUnknown, f.Unknown)
		w := writer()
		done := make(chan bool, 1)
		go func() { done <- serve(h, r, w) }()
		receive(t, entered)
		cancel()
		stillHeld(t, done)
		once.Do(func() { close(release) })
		if !receive(t, done) || w.Body.Len() != 0 || w.cleared() {
			t.Fatal("returned before actual service or published after cancel")
		}
	})
}

func TestTreeCommandsHTTPBoundedIOFailures(t *testing.T) {
	for _, name := range []string{"read_deadline", "write_deadline", "short_write", "write_error", "flush", "body_close", "early_deadline"} {
		t.Run(name, func(t *testing.T) {
			h, s, _ := fixture()
			r := request("rename", `{"expected_version":"1","title":"original"}`)
			w := writer()
			closeCalls := 0
			r.Body = &heldBody{Reader: strings.NewReader(`{"expected_version":"1","title":"original"}`), close: func() error {
				closeCalls++
				if name == "body_close" {
					return errors.New("private-close")
				}
				return nil
			}}
			switch name {
			case "read_deadline":
				w.read = func(time.Time) error { return http.ErrNotSupported }
			case "write_deadline":
				w.write = func(time.Time) error { return http.ErrNotSupported }
			case "short_write":
				w.output = func(b []byte) (int, error) { return len(b) - 1, nil }
			case "write_error":
				w.output = func([]byte) (int, error) { return 0, io.ErrClosedPipe }
			case "flush":
				w.flush = func() error { return io.ErrClosedPipe }
			case "early_deadline":
				ctx, cancel := context.WithDeadline(r.Context(), time.Now().Add(-time.Millisecond))
				defer cancel()
				r = r.WithContext(ctx)
			}
			if !serve(h, r, w) || w.cleared() || closeCalls != 1 {
				t.Fatalf("bad IO state cleared=%v close=%d", w.cleared(), closeCalls)
			}
			if (name == "read_deadline" || name == "write_deadline" || name == "early_deadline") && s.calls != 0 {
				t.Fatal("service entered without budget")
			}
		})
	}
}

func TestTreeCommandsHTTPOriginalTwoSecondBudget(t *testing.T) {
	h, s, b := fixture()
	var first time.Time
	b.check = func(r *http.Request) error {
		var ok bool
		first, ok = r.Context().Deadline()
		if !ok {
			t.Fatal("boundary lacks deadline")
		}
		return nil
	}
	s.before = func(ctx context.Context) {
		deadline, _ := ctx.Deadline()
		if deadline != first {
			t.Fatal("new service budget")
		}
		<-ctx.Done()
	}
	r := request("rename", `{"expected_version":"1","title":"original"}`)
	w := writer()
	start := time.Now()
	if !serve(h, r, w) || time.Since(start) < 1900*time.Millisecond || time.Since(start) > 3*time.Second || w.Body.Len() != 0 || w.cleared() {
		t.Fatal("natural original deadline not enforced")
	}
}
