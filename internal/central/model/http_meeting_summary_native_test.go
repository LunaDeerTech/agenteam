package model

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/httpapi"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

func requireSummaryNative(t *testing.T) {
	t.Helper()
	if os.Getenv("AGENTEAM_MEETING_SUMMARY_NATIVE") != "1" {
		t.Skip("requires an explicitly owned native loopback window")
	}
}

type summaryNativeResult struct{ aborted bool }

func summaryNativeListener(t *testing.T, h http.Handler, small bool) (string, <-chan summaryNativeResult, *atomic.Int32) {
	t.Helper()
	listener, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	results := make(chan summaryNativeResult, 16)
	connections := &atomic.Int32{}
	var mu sync.Mutex
	changed := sync.NewCond(&mu)
	active := map[net.Conn]bool{}
	socketErrors := make(chan error, 16)
	server := &http.Server{ErrorLog: log.New(io.Discard, "", 0), Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			p := recover()
			results <- summaryNativeResult{p == http.ErrAbortHandler}
			if p != nil {
				panic(p)
			}
		}()
		httpapi.Handler(nil, h).ServeHTTP(w, r)
	}), ConnState: func(conn net.Conn, state http.ConnState) {
		mu.Lock()
		defer mu.Unlock()
		switch state {
		case http.StateNew:
			connections.Add(1)
			active[conn] = true
			if small {
				if e := conn.(*net.TCPConn).SetWriteBuffer(1024); e != nil {
					socketErrors <- e
				}
			}
		case http.StateClosed, http.StateHijacked:
			delete(active, conn)
			changed.Broadcast()
		}
	}}
	done := make(chan error, 1)
	go func() { done <- server.Serve(listener) }()
	t.Cleanup(func() {
		if e := server.Close(); e != nil {
			t.Error(e)
		}
		_ = listener.Close()
		if e := <-done; !errors.Is(e, http.ErrServerClosed) {
			t.Error("Serve actual terminal", e)
		}
		mu.Lock()
		for len(active) > 0 {
			changed.Wait()
		}
		mu.Unlock()
		close(socketErrors)
		for e := range socketErrors {
			t.Error(e)
		}
	})
	return listener.Addr().String(), results, connections
}
func summaryNativeDial(t *testing.T, address string, small bool) net.Conn {
	t.Helper()
	conn, e := net.DialTimeout("tcp", address, time.Second)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { _ = conn.Close() })
	if e = conn.SetDeadline(time.Now().Add(38 * time.Second)); e != nil {
		t.Fatal(e)
	}
	if small {
		if e = conn.(*net.TCPConn).SetReadBuffer(1024); e != nil {
			t.Fatal(e)
		}
	}
	return conn
}
func summaryNativeTerminal(t *testing.T, results <-chan summaryNativeResult, limit time.Duration) summaryNativeResult {
	t.Helper()
	select {
	case r := <-results:
		return r
	case <-time.After(limit):
		t.Fatal("native handler has not actually returned")
		return summaryNativeResult{}
	}
}

type summaryNativeBody struct {
	io.ReadCloser
	eof, closes *atomic.Int32
	fail        bool
}

func (b summaryNativeBody) Read(p []byte) (int, error) {
	n, e := b.ReadCloser.Read(p)
	if e == io.EOF {
		b.eof.Add(1)
	}
	return n, e
}
func (b summaryNativeBody) Close() error {
	b.closes.Add(1)
	e := b.ReadCloser.Close()
	if e == nil && b.fail {
		return io.ErrClosedPipe
	}
	return e
}

func TestMeetingSummaryHTTPNativeKeepAlive(t *testing.T) {
	requireSummaryNative(t)
	h, _, _ := summaryHTTPFixture(t)
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
		h.serveHTTP(w, r)
	}), false)
	conn := summaryNativeDial(t, address, false)
	reader := bufio.NewReader(conn)
	var length int64
	for index, method := range []string{"GET", "HEAD", "PUT", "GET", "HEAD"} {
		if index == 1 {
			timer := time.NewTimer(200 * time.Millisecond)
			<-timer.C
		}
		body := ""
		if method == "PUT" {
			body = summaryHTTPTestBody
		}
		if _, e := io.WriteString(conn, method+" /api/v1"+meetingSummaryHTTPPath+" HTTP/1.1\r\nHost: "+address+"\r\nContent-Type: application/json\r\nIdempotency-Key: native-original-key\r\nContent-Length: "+strconv.Itoa(len(body))+"\r\n\r\n"+body); e != nil {
			t.Fatal(e)
		}
		response, e := http.ReadResponse(reader, &http.Request{Method: method})
		if e != nil {
			t.Fatal(e)
		}
		data, readErr := io.ReadAll(response.Body)
		closeErr := response.Body.Close()
		if response.StatusCode != 200 || readErr != nil || closeErr != nil || response.Header.Get("X-Request-ID") == "" {
			t.Fatal("incomplete native response")
		}
		if method == "GET" {
			length = int64(len(data))
			if length == 0 || response.ContentLength != length {
				t.Fatal("GET framing")
			}
		} else if method == "HEAD" && (len(data) != 0 || response.ContentLength != length) {
			t.Fatal("HEAD framing")
		}
		if summaryNativeTerminal(t, results, time.Second).aborted {
			t.Fatal("success aborted")
		}
	}
	if connections.Load() != 1 || requests.Load() != 5 || eof.Load() != 5 || closes.Load() != 5 {
		t.Fatal("keepalive EOF/Close ownership", connections.Load(), requests.Load(), eof.Load(), closes.Load())
	}
}

func TestMeetingSummaryHTTPNativeSlowBody(t *testing.T) {
	requireSummaryNative(t)
	for _, tc := range []struct {
		method string
		parent time.Duration
	}{{"GET", time.Minute}, {"PUT", time.Minute}, {"HEAD", 120 * time.Millisecond}, {"PUT", 120 * time.Millisecond}} {
		t.Run(tc.method+"/"+tc.parent.String(), func(t *testing.T) {
			h, b, s := summaryHTTPFixture(t)
			var eof, closes atomic.Int32
			seen := make(chan context.Context, 1)
			var before, parentDeadline time.Time
			b.auth = func(r *http.Request, intent id.AccessIntent) (id.Actor, error) {
				seen <- r.Context()
				return b.actor, nil
			}
			address, results, _ := summaryNativeListener(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				ctx, cancel := context.WithTimeout(r.Context(), tc.parent)
				defer cancel()
				parentDeadline, _ = ctx.Deadline()
				r = r.WithContext(ctx)
				r.Body = summaryNativeBody{r.Body, &eof, &closes, false}
				if tc.method != "PUT" {
					r.ContentLength = 0
				}
				before = time.Now()
				h.serveHTTP(w, r)
			}), false)
			conn := summaryNativeDial(t, address, false)
			started := time.Now()
			if _, e := io.WriteString(conn, tc.method+" /api/v1"+meetingSummaryHTTPPath+" HTTP/1.1\r\nHost: "+address+"\r\nContent-Type: application/json\r\nIdempotency-Key: original\r\nContent-Length: 1\r\n\r\n"); e != nil {
				t.Fatal(e)
			}
			var ctx context.Context
			select {
			case ctx = <-seen:
			case <-time.After(time.Second):
				t.Fatal("authorization not reached")
			}
			budget := meetingSummaryHTTPReadBudget
			if tc.method == "PUT" {
				budget = meetingSummaryHTTPWriteBudget
			}
			deadline, ok := ctx.Deadline()
			if !ok || tc.parent < budget && !deadline.Equal(parentDeadline) || tc.parent > budget && (deadline.Before(before.Add(budget)) || !deadline.Before(parentDeadline)) {
				t.Fatal("native preauthorization deadline")
			}
			// The client does not send the declared byte. Actual server Read, Close,
			// callback and handler must all return, including the natural 30-second PUT.
			data, e := io.ReadAll(conn)
			if e != nil {
				t.Fatal("native abort did not close connection", e)
			}
			terminal := summaryNativeTerminal(t, results, time.Second)
			elapsed := time.Since(started)
			if !terminal.aborted || ctx.Err() == nil || s.gets.Load() != 0 || s.updates.Load() != 0 || closes.Load() != 1 || elapsed < min(tc.parent, budget)*3/4 || elapsed > min(tc.parent, budget)+time.Second || len(data) != 0 {
				t.Fatal("slow body terminal", elapsed, terminal, closes.Load(), len(data))
			}
		})
	}
}

// Intentionally withhold Unwrap and deadline support over a real connection.
// Unsupported capability must abort before authentication or service dispatch.
type summaryNativeUnsupported struct{ writer http.ResponseWriter }

func (w summaryNativeUnsupported) Header() http.Header            { return w.writer.Header() }
func (w summaryNativeUnsupported) WriteHeader(code int)           { w.writer.WriteHeader(code) }
func (w summaryNativeUnsupported) Write(data []byte) (int, error) { return w.writer.Write(data) }
func (w summaryNativeUnsupported) FlushError() error {
	return http.NewResponseController(w.writer).Flush()
}

type summaryNativeWriter struct {
	http.ResponseWriter
	mode      string
	timedOut  atomic.Bool
	mu        sync.Mutex
	deadlines []time.Time
}

func (w *summaryNativeWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }
func (w *summaryNativeWriter) observe(e error) {
	var timeout net.Error
	if errors.As(e, &timeout) && timeout.Timeout() {
		w.timedOut.Store(true)
	}
}
func (w *summaryNativeWriter) Write(p []byte) (int, error) {
	if w.mode == "panic" {
		panic("private native adapter")
	}
	if w.mode == "write_error" {
		return 0, io.ErrClosedPipe
	}
	if w.mode == "short" {
		p = p[:1]
	}
	n, e := w.ResponseWriter.Write(p)
	w.observe(e)
	return n, e
}
func (w *summaryNativeWriter) FlushError() error {
	e := http.NewResponseController(w.ResponseWriter).Flush()
	w.observe(e)
	if e == nil && w.mode == "flush_error" {
		return io.ErrClosedPipe
	}
	return e
}
func (w *summaryNativeWriter) SetWriteDeadline(at time.Time) error {
	w.mu.Lock()
	w.deadlines = append(w.deadlines, at)
	w.mu.Unlock()
	e := http.NewResponseController(w.ResponseWriter).SetWriteDeadline(at)
	if e == nil && at.IsZero() && w.mode == "reset_error" {
		return io.ErrClosedPipe
	}
	return e
}
func TestMeetingSummaryHTTPNativeWriteAndClose(t *testing.T) {
	requireSummaryNative(t)
	for _, mode := range []string{"short", "write_error", "flush_error", "close_error", "panic", "reset_error", "unsupported", "blocked_head_flush"} {
		t.Run(mode, func(t *testing.T) {
			h, boundary, service := summaryHTTPFixture(t)
			var eof, closes atomic.Int32
			seen := make(chan *summaryNativeWriter, 1)
			address, results, _ := summaryNativeListener(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				ctx, cancel := context.WithTimeout(r.Context(), 120*time.Millisecond)
				defer cancel()
				r = r.WithContext(ctx)
				r.Body = summaryNativeBody{r.Body, &eof, &closes, mode == "close_error"}
				writer := &summaryNativeWriter{ResponseWriter: w, mode: mode}
				seen <- writer
				// A test-owned large header forces genuine socket backpressure for the
				// otherwise tiny HEAD representation. This is not a product DTO size proof.
				if mode == "blocked_head_flush" {
					w.Header().Set("X-Test-Backpressure", strings.Repeat("x", 4<<20))
				}
				if mode == "unsupported" {
					h.serveHTTP(summaryNativeUnsupported{writer}, r)
				} else {
					h.serveHTTP(writer, r)
				}
			}), mode == "blocked_head_flush")
			conn := summaryNativeDial(t, address, mode == "blocked_head_flush")
			method := "GET"
			if mode == "blocked_head_flush" {
				method = "HEAD"
			}
			if _, e := io.WriteString(conn, method+" /api/v1"+meetingSummaryHTTPPath+" HTTP/1.1\r\nHost: "+address+"\r\n\r\n"); e != nil {
				t.Fatal(e)
			}
			writer := <-seen
			terminal := summaryNativeTerminal(t, results, time.Second)
			if !terminal.aborted || closes.Load() != 1 {
				t.Fatal("missing abort/actual Close")
			}
			if mode == "unsupported" && (boundary.calls.Load() != 0 || service.gets.Load() != 0) {
				t.Fatal("native unsupported writer reached authorization/service")
			}
			if mode == "blocked_head_flush" {
				writer.mu.Lock()
				first, last := writer.deadlines[0], writer.deadlines[len(writer.deadlines)-1]
				writer.mu.Unlock()
				if !writer.timedOut.Load() || time.Now().Before(first) || !last.IsZero() {
					t.Fatal("native HEAD Flush not deadline-bounded/joined")
				}
			}
			// Widen the client receive buffer after the backpressure proof so buffered
			// native bytes drain promptly; the server handler has already actually ended.
			if mode == "blocked_head_flush" {
				if e := conn.(*net.TCPConn).SetReadBuffer(4 << 20); e != nil {
					t.Fatal(e)
				}
			}
			data, e := io.ReadAll(io.LimitReader(conn, 5<<20))
			if e != nil {
				t.Fatal("abort EOF", e)
			}
			if bytes.Contains(data, []byte("application/problem+json")) || mode == "close_error" && len(data) != 0 {
				t.Fatal("unbounded second Problem/publication before Close")
			}
			var one [1]byte
			if n, e := conn.Read(one[:]); n != 0 || e != io.EOF {
				t.Fatal("aborted keepalive survived", n, e)
			}
		})
	}
}
