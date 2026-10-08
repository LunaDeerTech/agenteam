package audithttp

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	c "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

func projectAuditNativeGate(t *testing.T) {
	t.Helper()
	if os.Getenv("AGENTEAM_PROJECT_AUDIT_NATIVE") != "1" {
		t.Skip("explicit exclusive native resource window required")
	}
}
func projectAuditNativeListener(t *testing.T, handler http.Handler) (string, <-chan nativeAuditResult, *atomic.Int32) {
	t.Helper()
	listener, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	results := make(chan nativeAuditResult, 8)
	var active sync.WaitGroup
	connections := &atomic.Int32{}
	server := &http.Server{ErrorLog: log.New(io.Discard, "", 0), ConnContext: func(ctx context.Context, c net.Conn) context.Context {
		if tcp, ok := c.(*net.TCPConn); ok {
			if e := tcp.SetWriteBuffer(1024); e != nil {
				t.Error("native write buffer", e)
			}
		}
		return ctx
	}, ConnState: func(_ net.Conn, s http.ConnState) {
		if s == http.StateNew {
			connections.Add(1)
		}
	}, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		active.Add(1)
		defer active.Done()
		defer func() {
			v := recover()
			results <- nativeAuditResult{aborted: v == http.ErrAbortHandler}
			if v != nil {
				panic(v)
			}
		}()
		handler.ServeHTTP(w, r)
	})}
	served := make(chan error, 1)
	go func() { served <- server.Serve(listener) }()
	t.Cleanup(func() {
		if e := server.Close(); e != nil {
			t.Error(e)
		}
		_ = listener.Close()
		if e := <-served; !errors.Is(e, http.ErrServerClosed) {
			t.Error("actual Serve wait", e)
		}
		active.Wait()
	})
	return listener.Addr().String(), results, connections
}
func TestProjectAuditHTTPNativeEOFAndKeepAlive(t *testing.T) {
	projectAuditNativeGate(t)
	var eof, closed, requests atomic.Int32
	address, done, connections := projectAuditNativeListener(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		duration := time.Second
		if requests.Add(1) == 1 {
			duration = 120 * time.Millisecond
		}
		ctx, cancel := context.WithTimeout(r.Context(), duration)
		defer cancel()
		r = r.WithContext(ctx)
		r.Body = nativeAuditBody{r.Body, &eof, &closed}
		(&projectAuditHTTP{&projectAuditReaderTest{}, &projectAuditBoundaryTest{}}).ServeHTTP(w, r)
	}))
	conn := nativeAuditDial(t, address)
	reader := bufio.NewReader(conn)
	for i, method := range []string{"GET", "HEAD", "GET"} {
		if i == 1 {
			<-time.After(200 * time.Millisecond)
		}
		if _, e := io.WriteString(conn, method+" "+projectAuditTestPath+" HTTP/1.1\r\nHost: "+address+"\r\nContent-Length: 0\r\n\r\n"); e != nil {
			t.Fatal(e)
		}
		r, e := http.ReadResponse(reader, &http.Request{Method: method})
		if e != nil {
			t.Fatal(e)
		}
		body, re := io.ReadAll(r.Body)
		ce := r.Body.Close()
		if re != nil || ce != nil || r.StatusCode != 200 || r.ContentLength != 31 || method == "GET" && string(body) != `{"items":[],"next_cursor":null}` || method == "HEAD" && len(body) != 0 || nativeAuditTerminal(t, done).aborted {
			t.Fatal("same connection EOF/HEAD/Flush", r.StatusCode, len(body))
		}
	}
	if requests.Load() != 3 || connections.Load() != 1 || eof.Load() != 3 || closed.Load() != 3 {
		t.Fatal("actual keepalive/callback/close count", requests.Load(), connections.Load(), eof.Load(), closed.Load())
	}
}
func TestProjectAuditHTTPNativeSlowBodyBudgets(t *testing.T) {
	projectAuditNativeGate(t)
	for _, tc := range []struct {
		name       string
		parent     time.Duration
		bodyHeader string
	}{{"natural-three-second-Close", time.Minute, "Content-Length: 1\r\n"}, {"earlier-parent-Close", 120 * time.Millisecond, "Content-Length: 1\r\n"}, {"natural-three-second-chunk-Read", time.Minute, "Transfer-Encoding: chunked\r\n"}} {
		t.Run(tc.name, func(t *testing.T) {
			var eof, closed atomic.Int32
			seen := make(chan context.Context, 1)
			service := &projectAuditReaderTest{}
			address, done, _ := projectAuditNativeListener(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				ctx, cancel := context.WithTimeout(r.Context(), tc.parent)
				defer cancel()
				r = r.WithContext(ctx)
				r.Body = nativeAuditBody{r.Body, &eof, &closed}
				b := &projectAuditBoundaryTest{}
				b.auth = func(r *http.Request) (identity.Actor, error) {
					seen <- r.Context()
					return (&projectAuditBoundaryTest{}).RequireHuman(r)
				}
				(&projectAuditHTTP{service, b}).ServeHTTP(w, r)
			}))
			conn := nativeAuditDial(t, address)
			start := time.Now()
			if _, e := io.WriteString(conn, "GET "+projectAuditTestPath+" HTTP/1.1\r\nHost: "+address+"\r\n"+tc.bodyHeader+"\r\n"); e != nil {
				t.Fatal(e)
			}
			var ctx context.Context
			select {
			case ctx = <-seen:
			case <-time.After(time.Second):
				t.Fatal("authorization checkpoint")
			}
			response, e := http.ReadResponse(bufio.NewReader(conn), &http.Request{Method: "GET"})
			if response != nil {
				_, _ = io.Copy(io.Discard, response.Body)
				_ = response.Body.Close()
				if e == nil && response.StatusCode == 200 {
					t.Fatal("slow forbidden body success")
				}
			}
			terminal := nativeAuditTerminal(t, done)
			deadline, _ := ctx.Deadline()
			if !terminal.aborted || time.Now().Before(deadline) || closed.Load() != 1 || service.calls.Load() != 0 || ctx.Err() == nil || time.Since(start) > min(tc.parent, projectAuditHTTPBudget)+time.Second {
				t.Fatal("actual native read/Close budget", ctx.Err(), closed.Load(), service.calls.Load())
			}
		})
	}
}
func TestProjectAuditHTTPNativeWriteAndFlushAbort(t *testing.T) {
	projectAuditNativeGate(t)
	for _, name := range []string{"short-write-GET", "flush-error-GET", "flush-error-HEAD", "blocked-write-GET", "blocked-flush-HEAD"} {
		t.Run(name, func(t *testing.T) {
			var eof, closed, writes, flushes atomic.Int32
			page := foundation.Page[c.SafeRecord]{}
			method := "GET"
			if strings.HasSuffix(name, "HEAD") {
				method = "HEAD"
			}
			if name == "blocked-write-GET" {
				var tc typedCase
				for _, x := range projectAuditTestCases() {
					if x.action == "object.transfer.revoke" {
						tc = x
					}
				}
				for i := 0; i < 200; i++ {
					r := projectAuditTestRecord(t, tc, identity.Human, true)
					r.AuditID, _ = foundation.ParseID[c.Record](fmt.Sprintf("01900000-0000-7000-8000-%012x", 1000-i))
					page.Items = append(page.Items, r)
				}
			}
			service := &projectAuditReaderTest{list: func(context.Context, c.Filter, foundation.PageRequest) (foundation.Page[c.SafeRecord], error) {
				return page, nil
			}}
			address, done, _ := projectAuditNativeListener(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				r.Body = nativeAuditBody{r.Body, &eof, &closed}
				if name == "blocked-flush-HEAD" {
					w.Header().Set("X-Fixture-Padding", strings.Repeat("x", 512<<10))
				}
				wrapped := &projectAuditNativeWriter{ResponseWriter: w, writes: &writes, flushes: &flushes, short: name == "short-write-GET", failFlush: strings.HasPrefix(name, "flush-error")}
				(&projectAuditHTTP{service, &projectAuditBoundaryTest{}}).ServeHTTP(wrapped, r)
			}))
			conn := nativeAuditDial(t, address)
			if tcp, ok := conn.(*net.TCPConn); ok {
				if e := tcp.SetReadBuffer(1024); e != nil {
					t.Fatal(e)
				}
			}
			path := projectAuditTestPath
			if name == "blocked-write-GET" {
				path += "?limit=200"
			}
			if _, e := io.WriteString(conn, method+" "+path+" HTTP/1.1\r\nHost: "+address+"\r\nContent-Length: 0\r\n\r\n"); e != nil {
				t.Fatal(e)
			}
			if strings.HasPrefix(name, "blocked-") {
				start := time.Now()
				terminal := nativeAuditTerminal(t, done)
				if !terminal.aborted || time.Since(start) < 2900*time.Millisecond || closed.Load() != 1 {
					t.Fatal("physical stalled socket timer/close")
				}
				if name == "blocked-write-GET" && writes.Load() == 0 || name == "blocked-flush-HEAD" && (flushes.Load() != 1 || writes.Load() != 0) {
					t.Fatal("wrong physical Write/HEAD Flush phase")
				}
				_ = conn.Close()
				return
			}
			reader := bufio.NewReader(conn)
			response, e := http.ReadResponse(reader, &http.Request{Method: method})
			if response != nil {
				data, re := io.ReadAll(response.Body)
				_ = response.Body.Close()
				if bytes.Contains(data, []byte("problem")) || name == "short-write-GET" && re == nil && e == nil {
					t.Fatal("partial body/second Problem")
				}
				if method == "HEAD" && len(data) != 0 {
					t.Fatal("HEAD body")
				}
			}
			if !nativeAuditTerminal(t, done).aborted || closed.Load() != 1 {
				t.Fatal("actual failure tail")
			}
			var one [1]byte
			if n, e := reader.Read(one[:]); n != 0 || e == nil {
				t.Fatal("failed connection reused")
			}
		})
	}
}

type projectAuditNativeWriter struct {
	http.ResponseWriter
	writes, flushes  *atomic.Int32
	short, failFlush bool
}

func (w *projectAuditNativeWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }
func (w *projectAuditNativeWriter) Write(p []byte) (int, error) {
	w.writes.Add(1)
	if w.short {
		return w.ResponseWriter.Write(p[:1])
	}
	return w.ResponseWriter.Write(p)
}
func (w *projectAuditNativeWriter) FlushError() error {
	w.flushes.Add(1)
	if e := http.NewResponseController(w.ResponseWriter).Flush(); e != nil {
		return e
	}
	if w.failFlush {
		return io.ErrClosedPipe
	}
	return nil
}
