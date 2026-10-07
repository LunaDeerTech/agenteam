package projecthttp

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/httpapi"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// Native tests require a separately granted loopback window. Pure selectors and
// -test.list never invoke these helpers, and a default package run skips them.
func requireOwnerReadNative(t *testing.T) {
	t.Helper()
	if os.Getenv("AGENTEAM_PROJECT_OWNER_READ_NATIVE") != "1" {
		t.Skip("requires the explicitly granted Project Owner read native resource window")
	}
}

type nativeReadBody struct {
	io.ReadCloser
	eof, closes *atomic.Int32
	closeError  bool
}

func (b nativeReadBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	if err == io.EOF {
		b.eof.Add(1)
	}
	return n, err
}
func (b nativeReadBody) Close() error {
	b.closes.Add(1)
	err := b.ReadCloser.Close()
	if b.closeError && err == nil {
		return io.ErrClosedPipe
	}
	return err
}

type nativeReadResult struct{ aborted bool }

func nativeReadListener(t *testing.T, handler http.Handler, smallBuffers bool) (string, <-chan nativeReadResult, *atomic.Int32) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	results := make(chan nativeReadResult, 16)
	connections := &atomic.Int32{}
	var mu sync.Mutex
	changed := sync.NewCond(&mu)
	active := map[net.Conn]bool{}
	socketErrors := make(chan error, 16)
	server := &http.Server{ErrorLog: log.New(io.Discard, "", 0), Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			v := recover()
			results <- nativeReadResult{aborted: v == http.ErrAbortHandler}
			if v != nil {
				panic(v)
			}
		}()
		httpapi.Handler(nil, handler).ServeHTTP(w, r)
	}), ConnState: func(conn net.Conn, state http.ConnState) {
		mu.Lock()
		defer mu.Unlock()
		switch state {
		case http.StateNew:
			connections.Add(1)
			active[conn] = true
			if smallBuffers {
				if err := conn.(*net.TCPConn).SetWriteBuffer(1024); err != nil {
					socketErrors <- err
				}
			}
		case http.StateClosed, http.StateHijacked:
			delete(active, conn)
			changed.Broadcast()
		}
	}}
	served := make(chan error, 1)
	go func() { served <- server.Serve(listener) }()
	t.Cleanup(func() {
		if err := server.Close(); err != nil {
			t.Error(err)
		}
		_ = listener.Close()
		if err := <-served; !errors.Is(err, http.ErrServerClosed) {
			t.Error("native Serve did not reach terminal", err)
		}
		mu.Lock()
		for len(active) > 0 {
			changed.Wait()
		}
		mu.Unlock()
		close(socketErrors)
		for err := range socketErrors {
			t.Error("native socket configuration", err)
		}
	})
	return listener.Addr().String(), results, connections
}
func nativeReadDial(t *testing.T, address string, smallBuffers bool) net.Conn {
	t.Helper()
	conn, err := net.DialTimeout("tcp", address, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	if err = conn.SetDeadline(time.Now().Add(10 * time.Second)); err != nil {
		t.Fatal(err)
	}
	if smallBuffers {
		if err = conn.(*net.TCPConn).SetReadBuffer(1024); err != nil {
			t.Fatal(err)
		}
	}
	return conn
}
func nativeReadTerminal(t *testing.T, results <-chan nativeReadResult) nativeReadResult {
	t.Helper()
	select {
	case result := <-results:
		return result
	case <-time.After(5 * time.Second):
		t.Fatal("native handler did not actually return")
		return nativeReadResult{}
	}
}

func TestProjectOwnerReadHTTPNativeKeepAlive(t *testing.T) {
	requireOwnerReadNative(t)
	h, _, _ := handlerFixture()
	var eof, closes, requests atomic.Int32
	address, results, connections := nativeReadListener(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		budget := time.Second
		if requests.Add(1) == 1 {
			budget = 120 * time.Millisecond
		}
		ctx, cancel := context.WithTimeout(r.Context(), budget)
		defer cancel()
		r = r.WithContext(ctx)
		r.Body = nativeReadBody{ReadCloser: r.Body, eof: &eof, closes: &closes}
		h.ServeHTTP(w, r)
	}), false)
	conn := nativeReadDial(t, address, false)
	reader := bufio.NewReader(conn)
	lengths := map[string]int64{}
	for index, test := range []struct{ method, target string }{{"GET", httpListPath}, {"HEAD", httpListPath}, {"GET", httpDetailPath}, {"HEAD", httpDetailPath}} {
		if index == 1 {
			timer := time.NewTimer(200 * time.Millisecond)
			<-timer.C
		}
		if _, err := io.WriteString(conn, test.method+" "+test.target+" HTTP/1.1\r\nHost: "+address+"\r\nContent-Length: 0\r\n\r\n"); err != nil {
			t.Fatal(err)
		}
		response, err := http.ReadResponse(reader, &http.Request{Method: test.method})
		if err != nil {
			t.Fatal("native keepalive response", err)
		}
		body, readErr := io.ReadAll(response.Body)
		closeErr := response.Body.Close()
		if readErr != nil || closeErr != nil || response.StatusCode != 200 || response.Header.Get("X-Request-ID") == "" {
			t.Fatal("native incomplete read", readErr, closeErr, response.StatusCode)
		}
		if test.method == "GET" {
			lengths[test.target] = int64(len(body))
			if int64(len(body)) != response.ContentLength || len(body) == 0 {
				t.Fatal("native GET representation/EOF")
			}
		} else if len(body) != 0 || response.ContentLength != lengths[test.target] {
			t.Fatal("native HEAD representation/EOF")
		}
		if nativeReadTerminal(t, results).aborted {
			t.Fatal("completed native request aborted")
		}
	}
	if connections.Load() != 1 || requests.Load() != 4 || eof.Load() != 4 || closes.Load() != 4 {
		t.Fatal("keepalive connection or actual body EOF/Close lost", connections.Load(), requests.Load(), eof.Load(), closes.Load())
	}
}
func TestProjectOwnerReadHTTPNativeSlowBody(t *testing.T) {
	requireOwnerReadNative(t)
	for _, framing := range []string{"declared", "hidden"} {
		for _, parentBudget := range []time.Duration{time.Minute, 120 * time.Millisecond} {
			t.Run(framing+"/"+parentBudget.String(), func(t *testing.T) {
				type observation struct {
					ctx                  context.Context
					parent, before, auth time.Time
				}
				seen := make(chan observation, 1)
				h, b, s := handlerFixture()
				var eof, closes atomic.Int32
				address, results, _ := nativeReadListener(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					ctx, cancel := context.WithTimeout(r.Context(), parentBudget)
					defer cancel()
					parentDeadline, _ := ctx.Deadline()
					r = r.WithContext(ctx)
					r.Body = nativeReadBody{ReadCloser: r.Body, eof: &eof, closes: &closes}
					if framing == "hidden" {
						r.ContentLength = 0
					}
					before := time.Now()
					b.auth = func(r *http.Request) (id.Actor, error) {
						seen <- observation{r.Context(), parentDeadline, before, time.Now()}
						return wireActor(), nil
					}
					h.ServeHTTP(w, r)
				}), false)
				conn := nativeReadDial(t, address, false)
				started := time.Now()
				if _, err := io.WriteString(conn, "GET "+httpListPath+" HTTP/1.1\r\nHost: "+address+"\r\nContent-Length: 1\r\n\r\n"); err != nil {
					t.Fatal(err)
				}
				var observed observation
				select {
				case observed = <-seen:
				case <-time.After(time.Second):
					t.Fatal("native authentication not reached")
				}
				deadline, ok := observed.ctx.Deadline()
				if !ok || parentBudget < readBudget && !deadline.Equal(observed.parent) || parentBudget > readBudget && (deadline.Before(observed.before.Add(readBudget)) || deadline.After(observed.auth.Add(readBudget)) || !deadline.Before(observed.parent)) {
					t.Fatal("native preauthentication deadline clipping")
				}
				response, err := http.ReadResponse(bufio.NewReader(conn), &http.Request{Method: "GET"})
				if err == nil {
					_, _ = io.Copy(io.Discard, response.Body)
					_ = response.Body.Close()
					if response.StatusCode == 200 {
						t.Fatal("slow forbidden body returned success")
					}
				}
				terminal := nativeReadTerminal(t, results)
				finished := time.Now()
				done := false
				select {
				case <-observed.ctx.Done():
					done = true
				default:
				}
				cause := observed.ctx.Err()
				limit := min(parentBudget, readBudget)
				// Native socket expiry can cancel net/http's parent before the context
				// timer wins. Observe the deadline/Done/elapsed/Close facts, not that race.
				if !terminal.aborted || !done || finished.Before(deadline) || finished.Sub(started) < limit*3/4 || finished.Sub(started) > limit+time.Second || (!errors.Is(cause, context.Canceled) && !errors.Is(cause, context.DeadlineExceeded)) || s.calls() != 0 || closes.Load() != 1 {
					t.Fatal("native slow-body terminal", terminal.aborted, cause, finished.Sub(started), s.calls(), closes.Load())
				}
			})
		}
	}
}

type nativeReadFailureWriter struct {
	http.ResponseWriter
	mode      string
	timedOut  atomic.Bool
	written   atomic.Int64
	mu        sync.Mutex
	deadlines []time.Time
}

func (w *nativeReadFailureWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }
func (w *nativeReadFailureWriter) observe(err error) {
	var timeout net.Error
	if errors.As(err, &timeout) && timeout.Timeout() {
		w.timedOut.Store(true)
	}
}
func (w *nativeReadFailureWriter) Write(p []byte) (int, error) {
	if w.mode == "write_error" {
		return 0, io.ErrClosedPipe
	}
	if w.mode == "short_write" {
		p = p[:1]
	}
	n, err := w.ResponseWriter.Write(p)
	w.written.Add(int64(n))
	w.observe(err)
	return n, err
}
func (w *nativeReadFailureWriter) FlushError() error {
	err := http.NewResponseController(w.ResponseWriter).Flush()
	w.observe(err)
	if err == nil && w.mode == "flush_error" {
		return io.ErrClosedPipe
	}
	return err
}
func (w *nativeReadFailureWriter) SetWriteDeadline(at time.Time) error {
	w.mu.Lock()
	w.deadlines = append(w.deadlines, at)
	w.mu.Unlock()
	return http.NewResponseController(w.ResponseWriter).SetWriteDeadline(at)
}
func TestProjectOwnerReadHTTPNativeWriteAndClose(t *testing.T) {
	requireOwnerReadNative(t)
	for _, mode := range []string{"short_write", "write_error", "flush_error", "close_error", "native_write_deadline"} {
		t.Run(mode, func(t *testing.T) {
			h, _, s := handlerFixture()
			var eof, closes atomic.Int32
			observed := make(chan *nativeReadFailureWriter, 1)
			target := httpListPath
			expected := 0
			if mode == "native_write_deadline" {
				page := wireMaximumPage()
				s.list = func(context.Context, id.Actor, pc.ListOwnedProjectsRequest, f.PageRequest) (f.Page[pc.ProjectListItem], error) {
					return page, nil
				}
				encoded, err := encodeList(context.Background(), pc.ListOwnedProjectsRequest{}, f.PageRequest{Limit: 100}, page)
				if err != nil {
					t.Fatal(err)
				}
				expected = len(encoded)
				target += "?limit=100"
			}
			address, results, _ := nativeReadListener(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				r.Body = nativeReadBody{ReadCloser: r.Body, eof: &eof, closes: &closes, closeError: mode == "close_error"}
				writer := &nativeReadFailureWriter{ResponseWriter: w, mode: mode}
				observed <- writer
				h.ServeHTTP(writer, r)
			}), mode == "native_write_deadline")
			conn := nativeReadDial(t, address, mode == "native_write_deadline")
			started := time.Now()
			if _, err := io.WriteString(conn, "GET "+target+" HTTP/1.1\r\nHost: "+address+"\r\nContent-Length: 0\r\n\r\n"); err != nil {
				t.Fatal(err)
			}
			var writer *nativeReadFailureWriter
			select {
			case writer = <-observed:
			case <-time.After(time.Second):
				t.Fatal("native writer not reached")
			}
			// For the real deadline case the peer intentionally does not read until
			// the bounded write has actually returned and the handler has aborted.
			terminal := nativeReadTerminal(t, results)
			if !terminal.aborted || closes.Load() != 1 {
				t.Fatal("native failure missed abort/Close")
			}
			if mode == "native_write_deadline" {
				writer.mu.Lock()
				first, last := writer.deadlines[0], writer.deadlines[len(writer.deadlines)-1]
				writer.mu.Unlock()
				if !writer.timedOut.Load() || time.Now().Before(first) || time.Since(started) < readBudget*3/4 || writer.written.Load() >= int64(expected) || !last.IsZero() {
					t.Fatal("native write was not deadline-bounded/joined", writer.timedOut.Load(), writer.written.Load(), expected)
				}
			}
			data, err := io.ReadAll(io.LimitReader(conn, maxRepresentationBytes+4096))
			if err != nil {
				t.Fatal("aborted native connection did not reach EOF", err)
			}
			if bytes.Contains(data, []byte("application/problem+json")) || mode == "close_error" && len(data) != 0 {
				t.Fatal("failure appended Problem/published before Close")
			}
			var one [1]byte
			if n, err := conn.Read(one[:]); n != 0 || !errors.Is(err, io.EOF) {
				t.Fatal("aborted keepalive survived", n, err)
			}
		})
	}
}
