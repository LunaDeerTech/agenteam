package model

import (
	"errors"
	"github.com/LunaDeerTech/agenteam/internal/central/httpapi"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type summaryHTTPWriter struct {
	*httptest.ResponseRecorder
	mu                sync.Mutex
	reads, writes     []time.Time
	setRead, setWrite func(time.Time) error
	write             func([]byte) (int, error)
	flush             func() error
	flushes           int
}

func summaryWriter() *summaryHTTPWriter {
	return &summaryHTTPWriter{ResponseRecorder: httptest.NewRecorder()}
}
func (w *summaryHTTPWriter) SetReadDeadline(at time.Time) error {
	w.mu.Lock()
	w.reads = append(w.reads, at)
	w.mu.Unlock()
	if w.setRead != nil {
		return w.setRead(at)
	}
	return nil
}
func (w *summaryHTTPWriter) SetWriteDeadline(at time.Time) error {
	w.mu.Lock()
	w.writes = append(w.writes, at)
	w.mu.Unlock()
	if w.setWrite != nil {
		return w.setWrite(at)
	}
	return nil
}
func (w *summaryHTTPWriter) Write(p []byte) (int, error) {
	if w.write != nil {
		return w.write(p)
	}
	return w.ResponseRecorder.Write(p)
}
func (w *summaryHTTPWriter) FlushError() error {
	w.flushes++
	if w.flush != nil {
		return w.flush()
	}
	return nil
}
func (w *summaryHTTPWriter) cleared(t *testing.T) {
	t.Helper()
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.reads) < 2 || len(w.writes) < 2 || !w.reads[len(w.reads)-1].IsZero() || !w.writes[len(w.writes)-1].IsZero() {
		t.Fatal("native deadline not retired")
	}
}

type summaryHTTPBody struct {
	io.Reader
	close  func() error
	closes atomic.Int32
}

func (b *summaryHTTPBody) Close() error {
	b.closes.Add(1)
	if b.close != nil {
		return b.close()
	}
	return nil
}

type summaryHTTPRead func([]byte) (int, error)

func (fn summaryHTTPRead) Read(p []byte) (int, error) { return fn(p) }
func summaryAborts(fn func()) (aborted bool) {
	defer func() {
		if p := recover(); p != nil {
			if p != http.ErrAbortHandler {
				panic(p)
			}
			aborted = true
		}
	}()
	fn()
	return false
}
func summaryJoined(t *testing.T, c <-chan bool) bool {
	t.Helper()
	select {
	case v := <-c:
		return v
	case <-time.After(time.Second):
		t.Fatal("handler actual return missing")
		return false
	}
}
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
