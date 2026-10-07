package model

import (
	"bufio"
	"context"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

func requireProjectModelNative(t *testing.T) {
	t.Helper()
	if os.Getenv("AGENTEAM_PROJECT_MODEL_NATIVE") != "1" {
		t.Skip("requires the explicitly owned Project Model native loopback window")
	}
}
func TestProjectModelHTTPNativeKeepAliveDeadline(t *testing.T) {
	requireProjectModelNative(t)
	h, _, _ := projectHTTPTestHandler(t)
	var requests, eof, closes atomic.Int32
	address, results, connections := summaryNativeListener(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		duration := time.Second
		if requests.Add(1) == 1 {
			duration = 120 * time.Millisecond
		}
		ctx, cancel := context.WithTimeout(r.Context(), duration)
		defer cancel()
		r = r.WithContext(ctx)
		r.Body = summaryNativeBody{r.Body, &eof, &closes, false}
		h.ServeHTTP(w, r)
	}), false)
	conn := summaryNativeDial(t, address, false)
	reader := bufio.NewReader(conn)
	var length int64
	for i, method := range []string{"GET", "HEAD", "GET", "HEAD"} {
		if i == 1 {
			timer := time.NewTimer(200 * time.Millisecond)
			<-timer.C
		}
		if _, e := io.WriteString(conn, method+" "+projectHTTPTestBase+"/models HTTP/1.1\r\nHost: "+address+"\r\n\r\n"); e != nil {
			t.Fatal(e)
		}
		response, e := http.ReadResponse(reader, &http.Request{Method: method})
		if e != nil {
			t.Fatal(e)
		}
		body, re := io.ReadAll(response.Body)
		ce := response.Body.Close()
		if re != nil || ce != nil || response.StatusCode != 200 || response.Header.Get("X-Request-ID") == "" {
			t.Fatal("native response incomplete")
		}
		if method == "GET" {
			length = int64(len(body))
			if length == 0 || response.ContentLength != length {
				t.Fatal("GET framing")
			}
		} else if len(body) != 0 || response.ContentLength != length {
			t.Fatal("HEAD framing")
		}
		if summaryNativeTerminal(t, results, time.Second).aborted {
			t.Fatal("success aborted")
		}
	}
	if connections.Load() != 1 || requests.Load() != 4 || eof.Load() != 4 || closes.Load() != 4 {
		t.Fatal("keepalive/EOF/Close ownership")
	}
}
func TestProjectModelHTTPNativeSlowBodyAndCallback(t *testing.T) {
	requireProjectModelNative(t)
	for _, tc := range []struct {
		method string
		parent time.Duration
	}{{"GET", time.Minute}, {"HEAD", time.Minute}, {"GET", 120 * time.Millisecond}} {
		t.Run(tc.method+"/"+tc.parent.String(), func(t *testing.T) {
			h, b, s := projectHTTPTestHandler(t)
			var eof, closes atomic.Int32
			seen := make(chan context.Context, 1)
			b.auth = func(r *http.Request) (id.Actor, error) { seen <- r.Context(); return b.actor, nil }
			address, results, _ := summaryNativeListener(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				ctx, cancel := context.WithTimeout(r.Context(), tc.parent)
				defer cancel()
				r = r.WithContext(ctx)
				r.Body = summaryNativeBody{r.Body, &eof, &closes, false}
				r.ContentLength = 0
				h.ServeHTTP(w, r)
			}), false)
			conn := summaryNativeDial(t, address, false)
			started := time.Now()
			_, e := io.WriteString(conn, tc.method+" "+projectHTTPTestBase+"/models HTTP/1.1\r\nHost: "+address+"\r\nContent-Length: 1\r\n\r\n")
			if e != nil {
				t.Fatal(e)
			}
			var ctx context.Context
			select {
			case ctx = <-seen:
			case <-time.After(time.Second):
				t.Fatal("auth not reached")
			}
			deadline, ok := ctx.Deadline()
			budget := min(projectHTTPReadBudget, tc.parent)
			if !ok || deadline.Sub(started) > budget+time.Second || deadline.Sub(started) < budget/2 {
				t.Fatal("native preauthorization deadline")
			}
			raw, e := io.ReadAll(conn)
			terminal := summaryNativeTerminal(t, results, time.Second)
			elapsed := time.Since(started)
			if e != nil || !terminal.aborted || ctx.Err() == nil || s.calls.Load() != 0 || closes.Load() != 1 || elapsed < budget*3/4 || elapsed > budget+time.Second || len(raw) != 0 {
				t.Fatal("slow native EOF not bounded/joined", elapsed, e, len(raw))
			}
		})
	}
	t.Run("noncooperative_service_actual_tail", func(t *testing.T) {
		h, _, s := projectHTTPTestHandler(t)
		entered, release := make(chan struct{}), make(chan struct{})
		var once sync.Once
		var owners sync.WaitGroup
		unblock := func() { once.Do(func() { close(release) }) }
		s.call = func(ctx context.Context, _ id.Actor, _ id.ProjectID, _ ProjectQuery, _ projectHTTPKind, _ string) error {
			close(entered)
			<-release
			return ctx.Err()
		}
		address, results, _ := summaryNativeListener(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			owners.Add(1)
			defer owners.Done()
			ctx, cancel := context.WithTimeout(r.Context(), 120*time.Millisecond)
			defer cancel()
			h.ServeHTTP(w, r.WithContext(ctx))
		}), false)
		// Runs before the accepted listener cleanup, which closes and joins all
		// connections/Serve, including any request arriving after this Wait.
		t.Cleanup(func() { unblock(); owners.Wait() })
		conn := summaryNativeDial(t, address, false)
		_, e := io.WriteString(conn, "GET "+projectHTTPTestBase+"/models HTTP/1.1\r\nHost: "+address+"\r\n\r\n")
		if e != nil {
			t.Fatal(e)
		}
		select {
		case <-entered:
		case <-time.After(time.Second):
			t.Fatal("native service did not enter")
		}
		timer := time.NewTimer(200 * time.Millisecond)
		<-timer.C
		select {
		case <-results:
			t.Fatal("cancel substituted for service join")
		default:
		}
		unblock()
		if !summaryNativeTerminal(t, results, time.Second).aborted {
			t.Fatal("late service published")
		}
		raw, e := io.ReadAll(conn)
		if e != nil || len(raw) != 0 {
			t.Fatal("late bytes")
		}
	})
}

// This test-only outer wrapper observes the actual Write/Flush call boundaries;
// the accepted inner observer still recognizes only net.Error.Timeout from real
// I/O. Handler entry alone cannot establish which publication phase timed out.
type projectHTTPNativeObserved struct {
	*summaryNativeWriter
	writeEntered, writeExited, flushEntered, flushExited atomic.Int32
	writeAsked, writeReturned                            atomic.Int64
}

func (w *projectHTTPNativeObserved) Write(p []byte) (int, error) {
	w.writeEntered.Add(1)
	w.writeAsked.Add(int64(len(p)))
	defer w.writeExited.Add(1)
	n, err := w.summaryNativeWriter.Write(p)
	w.writeReturned.Add(int64(n))
	return n, err
}
func (w *projectHTTPNativeObserved) FlushError() error {
	w.flushEntered.Add(1)
	defer w.flushExited.Add(1)
	return w.summaryNativeWriter.FlushError()
}

func TestProjectModelHTTPNativeWriteFlushCloseAndAbort(t *testing.T) {
	requireProjectModelNative(t)
	for _, mode := range []string{"short", "write_error", "flush_error", "close_error", "panic", "reset_error", "unsupported", "blocked_head_flush", "blocked_get_write"} {
		t.Run(mode, func(t *testing.T) {
			h, b, s := projectHTTPTestHandler(t)
			if mode == "blocked_get_write" {
				_, _, _, av := projectHTTPValues(t)
				av.Capabilities.Reasoning = true
				av.Capabilities.ReasoningEfforts = make([]string, 100000)
				for i := range av.Capabilities.ReasoningEfforts {
					av.Capabilities.ReasoningEfforts[i] = projectNativeToken(i)
				}
				s.available = AvailableChatModelPage{Items: []AvailableChatModel{av}}
			}
			var eof, closes atomic.Int32
			seen := make(chan *projectHTTPNativeObserved, 1)
			blocked := strings.HasPrefix(mode, "blocked_")
			address, results, _ := summaryNativeListener(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				parentBudget := 120 * time.Millisecond
				if mode == "blocked_get_write" {
					// Keep the legal large DTO and the product's natural 2s.
					// The old artificial 120ms parent did not prove Write entry.
					parentBudget = time.Minute
				}
				ctx, cancel := context.WithTimeout(r.Context(), parentBudget)
				defer cancel()
				r = r.WithContext(ctx)
				r.Body = summaryNativeBody{r.Body, &eof, &closes, mode == "close_error"}
				writer := &projectHTTPNativeObserved{summaryNativeWriter: &summaryNativeWriter{ResponseWriter: w, mode: mode}}
				seen <- writer
				if mode == "blocked_head_flush" {
					w.Header().Set("X-Test-Backpressure", strings.Repeat("x", 4<<20))
				}
				if mode == "unsupported" {
					h.ServeHTTP(summaryNativeUnsupported{writer}, r)
				} else {
					h.ServeHTTP(writer, r)
				}
			}), blocked)
			conn := summaryNativeDial(t, address, blocked)
			method, path := "GET", projectHTTPTestBase+"/models"
			if mode == "blocked_head_flush" {
				method = "HEAD"
			}
			if mode == "blocked_get_write" {
				path = projectHTTPTestBase + "/available-chat-models"
			}
			_, e := io.WriteString(conn, method+" "+path+" HTTP/1.1\r\nHost: "+address+"\r\n\r\n")
			if e != nil {
				t.Fatal(e)
			}
			var writer *projectHTTPNativeObserved
			select {
			case writer = <-seen:
			case <-time.After(time.Second):
				t.Fatal("native writer did not enter")
			}
			terminal := summaryNativeTerminal(t, results, 3*time.Second)
			if !terminal.aborted || closes.Load() != 1 {
				t.Fatal("native terminal/Close missing")
			}
			if mode == "unsupported" && (b.checks.Load() != 0 || s.calls.Load() != 0) {
				t.Fatal("unsupported reached auth/service")
			}
			if blocked {
				t.Logf("actual I/O write_entered=%d write_exited=%d requested_bytes=%d returned_bytes=%d flush_entered=%d flush_exited=%d net_timeout=%t", writer.writeEntered.Load(), writer.writeExited.Load(), writer.writeAsked.Load(), writer.writeReturned.Load(), writer.flushEntered.Load(), writer.flushExited.Load(), writer.timedOut.Load())
				if mode == "blocked_get_write" && (writer.writeEntered.Load() == 0 || writer.writeEntered.Load() != writer.writeExited.Load() || writer.writeAsked.Load() <= writer.writeReturned.Load()) {
					t.Fatal("real blocked Write entry/partial return not observed")
				}
				if mode == "blocked_head_flush" && (writer.flushEntered.Load() == 0 || writer.flushEntered.Load() != writer.flushExited.Load()) {
					t.Fatal("real blocked HEAD Flush entry/exit not observed")
				}
				writer.mu.Lock()
				first, last := writer.deadlines[0], writer.deadlines[len(writer.deadlines)-1]
				writer.mu.Unlock()
				if !writer.timedOut.Load() || time.Now().Before(first) || !last.IsZero() {
					t.Fatal("socket backpressure not actual bounded I/O")
				}
				if e = conn.(*net.TCPConn).SetReadBuffer(4 << 20); e != nil {
					t.Fatal(e)
				}
			}
			raw, e := io.ReadAll(conn)
			if e != nil {
				t.Fatal("socket did not terminate", e)
			}
			if strings.Contains(string(raw), "application/problem+json") {
				t.Fatal("abort appended Problem")
			}
		})
	}
}
func projectNativeToken(n int) string {
	const digits = "0123456789abcdef"
	var v [9]byte
	v[0] = 'r'
	for i := 8; i > 0; i-- {
		v[i] = digits[n&15]
		n >>= 4
	}
	return string(v[:])
}
