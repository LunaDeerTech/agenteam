package workhttp

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
	c "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
)

// These tests use actual loopback TCP and require a separately granted window.
// Their controlled authority/domain ports prove native HTTP ownership only;
// the authenticated default production root has its own process acceptance.
func requireNative(t *testing.T) {
	t.Helper()
	if os.Getenv("AGENTEAM_WORK_OWNER_HTTP_NATIVE") != "1" {
		t.Skip("requires an explicitly granted Work Owner HTTP native window")
	}
}

type nativeResult struct{ aborted bool }
type nativeBody struct {
	io.ReadCloser
	closes, eof *atomic.Int32
	beforeClose func() error
}

func (b nativeBody) Read(p []byte) (int, error) {
	n, e := b.ReadCloser.Read(p)
	if e == io.EOF {
		b.eof.Add(1)
	}
	return n, e
}
func (b nativeBody) Close() error {
	b.closes.Add(1)
	var injected error
	if b.beforeClose != nil {
		injected = b.beforeClose()
	}
	return errors.Join(injected, b.ReadCloser.Close())
}

func nativeListener(t *testing.T, h http.Handler, small bool) (string, <-chan nativeResult, *atomic.Int32) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	results := make(chan nativeResult, 16)
	count := new(atomic.Int32)
	var mu sync.Mutex
	active := map[net.Conn]bool{}
	changed := make(chan struct{})
	var handlers sync.WaitGroup
	server := &http.Server{ErrorLog: log.New(io.Discard, "", 0), Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handlers.Add(1)
		defer handlers.Done()
		defer func() {
			v := recover()
			results <- nativeResult{v == http.ErrAbortHandler}
			if v != nil {
				panic(v)
			}
		}()
		httpapi.Handler(nil, h).ServeHTTP(w, r)
	}), ConnState: func(conn net.Conn, state http.ConnState) {
		mu.Lock()
		defer mu.Unlock()
		switch state {
		case http.StateNew:
			count.Add(1)
			active[conn] = true
			if small {
				if e := conn.(*net.TCPConn).SetWriteBuffer(1024); e != nil {
					t.Error("owned write buffer", e)
				}
			}
		case http.StateClosed, http.StateHijacked:
			delete(active, conn)
		}
		close(changed)
		changed = make(chan struct{})
	}}
	served := make(chan error, 1)
	go func() { served <- server.Serve(listener) }()
	t.Cleanup(func() {
		if e := server.Close(); e != nil {
			t.Error(e)
		}
		_ = listener.Close()
		select {
		case e := <-served:
			if !errors.Is(e, http.ErrServerClosed) {
				t.Error("Serve terminal", e)
			}
		case <-time.After(3 * time.Second):
			t.Fatal("owned Serve did not join")
		}
		deadline := time.NewTimer(3 * time.Second)
		defer deadline.Stop()
		for {
			mu.Lock()
			empty, next := len(active) == 0, changed
			mu.Unlock()
			if empty {
				break
			}
			select {
			case <-next:
			case <-deadline.C:
				t.Fatal("native connections did not retire")
			}
		}
		// No connection remains able to start another handler before Wait.
		joined := make(chan struct{})
		go func() { handlers.Wait(); close(joined) }()
		select {
		case <-joined:
		case <-time.After(3 * time.Second):
			t.Fatal("native handlers did not actually join")
		}
	})
	return listener.Addr().String(), results, count
}
func nativeDial(t *testing.T, address string) *net.TCPConn {
	t.Helper()
	conn, e := net.DialTimeout("tcp", address, time.Second)
	if e != nil {
		t.Fatal(e)
	}
	tcp := conn.(*net.TCPConn)
	t.Cleanup(func() { _ = tcp.Close() })
	if e = tcp.SetDeadline(time.Now().Add(40 * time.Second)); e != nil {
		t.Fatal(e)
	}
	return tcp
}
func nativeTerminal(t *testing.T, results <-chan nativeResult) nativeResult {
	t.Helper()
	select {
	case v := <-results:
		return v
	case <-time.After(38 * time.Second):
		t.Fatal("native request did not reach a real terminal")
	}
	return nativeResult{}
}
func nativeSend(t *testing.T, w io.Writer, address, method, path, body string, close bool) {
	t.Helper()
	connection := ""
	if close {
		connection = "Connection: close\r\n"
	}
	_, e := io.WriteString(w, method+" "+testPath(path)+" HTTP/1.1\r\nHost: "+address+"\r\nContent-Type: application/json\r\nIdempotency-Key: saved-original-intent\r\n"+connection+"Content-Length: "+strconv.Itoa(len(body))+"\r\n\r\n"+body)
	if e != nil {
		t.Fatal(e)
	}
}

func TestWorkHTTPNativeDeadlines(t *testing.T) {
	requireNative(t)
	s := commandSamples()[7]
	for _, tc := range []struct {
		name, method, path string
		budget             time.Duration
		parent             bool
	}{
		{"read-natural", "GET", "/tasks", readBudget, false},
		{"lookup-natural", "POST", s.lookupPath, readBudget, false},
		{"mutation-natural", "PATCH", s.path, mutationBudget, false},
		{"earlier-parent", "PATCH", s.path, 150 * time.Millisecond, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, p := commandFixture()
			s.prepare(p)
			var closes, eof atomic.Int32
			address, results, _ := nativeListener(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if tc.parent {
					ctx, cancel := context.WithTimeout(r.Context(), tc.budget)
					defer cancel()
					r = r.WithContext(ctx)
				}
				r.Body = nativeBody{ReadCloser: r.Body, closes: &closes, eof: &eof}
				h.ServeHTTP(w, r)
			}), false)
			conn := nativeDial(t, address)
			started := time.Now()
			_, err := io.WriteString(conn, tc.method+" "+testPath(tc.path)+" HTTP/1.1\r\nHost: "+address+"\r\nContent-Type: application/json\r\nIdempotency-Key: saved-original-intent\r\nContent-Length: 100\r\n\r\n{")
			if err != nil {
				t.Fatal(err)
			}
			// Read to the real connection EOF; no LimitReader can manufacture it.
			raw, err := io.ReadAll(conn)
			terminal := nativeTerminal(t, results)
			elapsed := time.Since(started)
			if err != nil || !terminal.aborted || len(raw) != 0 || elapsed < tc.budget*3/4 || elapsed > tc.budget+2*time.Second || closes.Load() != 1 || p.calls != 0 {
				t.Fatal("natural deadline/EOF/owned body", err, elapsed, len(raw), closes.Load())
			}
		})
	}
}
func TestWorkHTTPNativeKeepaliveAndEOF(t *testing.T) {
	requireNative(t)
	h, p := commandFixture()
	s := commandSamples()[7]
	s.prepare(p)
	var closes, eof atomic.Int32
	address, results, count := nativeListener(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Body = nativeBody{ReadCloser: r.Body, closes: &closes, eof: &eof}
		h.ServeHTTP(w, r)
	}), false)
	conn := nativeDial(t, address)
	reader := bufio.NewReader(conn)
	first := time.Now()
	for n, tc := range []struct{ method, path, body string }{{"GET", "/tasks", ""}, {"POST", s.lookupPath, sampleBody(t, s, true)}, {s.method, s.path, sampleBody(t, s, false)}} {
		if n == 1 {
			<-time.After(time.Until(first.Add(readBudget + 150*time.Millisecond)))
		}
		nativeSend(t, conn, address, tc.method, tc.path, tc.body, n == 2)
		resp, err := http.ReadResponse(reader, &http.Request{Method: tc.method})
		if err != nil {
			t.Fatal(err)
		}
		raw, readErr := io.ReadAll(resp.Body)
		closeErr := resp.Body.Close()
		terminal := nativeTerminal(t, results)
		if readErr != nil || closeErr != nil || resp.StatusCode != 200 || int64(len(raw)) != resp.ContentLength || resp.Header.Get("X-Request-ID") == "" || terminal.aborted {
			t.Fatal("native representation did not complete", readErr, closeErr)
		}
	}
	if _, err := reader.ReadByte(); err != io.EOF {
		t.Fatal("last response lacked actual underlying EOF", err)
	}
	if count.Load() != 1 || closes.Load() != 3 || eof.Load() != 3 || p.calls != 3 {
		t.Fatal("native keepalive/deadline reset/body ownership", count.Load(), closes.Load(), eof.Load(), p.calls)
	}
}

type nativeFailureWriter struct {
	http.ResponseWriter
	mode string
}

func (w *nativeFailureWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }
func (w *nativeFailureWriter) Write(p []byte) (int, error) {
	switch w.mode {
	case "short":
		return w.ResponseWriter.Write(p[:len(p)-1])
	case "write":
		return 0, io.ErrClosedPipe
	}
	return w.ResponseWriter.Write(p)
}
func (w *nativeFailureWriter) FlushError() error {
	if w.mode == "flush" {
		return io.ErrClosedPipe
	}
	return http.NewResponseController(w.ResponseWriter).Flush()
}
func (w *nativeFailureWriter) SetReadDeadline(at time.Time) error {
	if w.mode == "clear" && at.IsZero() {
		return io.ErrClosedPipe
	}
	return http.NewResponseController(w.ResponseWriter).SetReadDeadline(at)
}

func TestWorkHTTPNativeWriteCloseAndConfirmationTail(t *testing.T) {
	requireNative(t)
	t.Run("natural-slow-write", func(t *testing.T) {
		h, _, p := testHandler()
		p.bl.Items = make([]c.TaskBlocker, 200)
		for n := range p.bl.Items {
			v := testBlocker()
			v.ID = testID[c.TaskBlockerIdentity](n + 1)
			v.Description = strings.Repeat("<", 1024)
			v.ResolvedAt = ptr(testAt())
			v.ResolvedBy = ptr(v.CreatedBy)
			v.ResolutionComment = ptr(strings.Repeat(">", 1024))
			p.bl.Items[n] = v
		}
		address, results, _ := nativeListener(t, h, true)
		conn := nativeDial(t, address)
		if e := conn.SetReadBuffer(1024); e != nil {
			t.Fatal(e)
		}
		started := time.Now()
		nativeSend(t, conn, address, "GET", "/tasks/"+testTask().ID.String()+"/blockers?status=resolved&limit=200", "", true)
		terminal := nativeTerminal(t, results)
		elapsed := time.Since(started)
		if !terminal.aborted || elapsed < readBudget*3/4 || elapsed > readBudget+2*time.Second {
			t.Fatal("slow native write was not bounded", elapsed)
		}
		if e := conn.SetReadBuffer(1 << 20); e != nil {
			t.Fatal(e)
		}
		raw, e := io.ReadAll(conn)
		if e != nil {
			t.Fatal("native abort did not reach actual EOF", e)
		}
		resp, e := http.ReadResponse(bufio.NewReader(bytes.NewReader(raw)), &http.Request{Method: "GET"})
		if e == nil {
			body, be := io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			if be == nil && int64(len(body)) == resp.ContentLength {
				t.Fatal("slow write published a complete success")
			}
		}
	})
	for _, mode := range []string{"short", "write", "flush", "close", "clear"} {
		t.Run(mode, func(t *testing.T) {
			h, p := commandFixture()
			s := commandSamples()[7]
			s.prepare(p)
			var closes, eof atomic.Int32
			address, results, _ := nativeListener(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body := nativeBody{ReadCloser: r.Body, closes: &closes, eof: &eof}
				if mode == "close" {
					body.beforeClose = func() error { return io.ErrClosedPipe }
				}
				r.Body = body
				h.ServeHTTP(&nativeFailureWriter{w, mode}, r)
			}), false)
			conn := nativeDial(t, address)
			nativeSend(t, conn, address, s.method, s.path, sampleBody(t, s, false), true)
			terminal := nativeTerminal(t, results)
			raw, e := io.ReadAll(conn)
			if e != nil || !terminal.aborted || closes.Load() != 1 || bytes.Contains(raw, []byte("application/problem+json")) || mode == "close" && len(raw) != 0 {
				t.Fatal("native abort/EOF/close", e, closes.Load())
			}
		})
	}
	for _, mode := range []string{"body-close", "confirmation-tail"} {
		t.Run(mode, func(t *testing.T) {
			h, p := commandFixture()
			s := commandSamples()[7]
			s.prepare(p)
			var closes, eof atomic.Int32
			entered, release := make(chan struct{}), make(chan struct{})
			var once sync.Once
			unpark := func() { once.Do(func() { close(release) }) }
			t.Cleanup(unpark)
			var cancel context.CancelFunc
			if mode == "confirmation-tail" {
				p.before = func(context.Context) { cancel(); close(entered); <-release }
			}
			address, results, _ := nativeListener(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				ctx, stop := context.WithCancel(r.Context())
				cancel = stop
				defer stop()
				r = r.WithContext(ctx)
				body := nativeBody{ReadCloser: r.Body, closes: &closes, eof: &eof}
				if mode == "body-close" {
					body.beforeClose = func() error { stop(); close(entered); <-release; return nil }
				}
				r.Body = body
				h.ServeHTTP(w, r)
			}), false)
			// Register after the listener so failed assertions release the hold
			// before listener cleanup waits for the real handler.
			t.Cleanup(unpark)
			conn := nativeDial(t, address)
			nativeSend(t, conn, address, s.method, s.path, sampleBody(t, s, false), true)
			joined(t, entered)
			select {
			case <-results:
				t.Fatal("native handler abandoned owned tail")
			default:
			}
			unpark()
			terminal := nativeTerminal(t, results)
			raw, e := io.ReadAll(conn)
			if e != nil || !terminal.aborted || len(raw) != 0 || closes.Load() != 1 {
				t.Fatal("late native publication/real close", e)
			}
		})
	}
}
