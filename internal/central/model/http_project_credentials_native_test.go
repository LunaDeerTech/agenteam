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
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

func requireProjectCredentialNative(t *testing.T) {
	t.Helper()
	if os.Getenv("AGENTEAM_PROJECT_CREDENTIAL_NATIVE") != "1" {
		t.Skip("requires the explicitly owned Project Credential native window")
	}
}
func TestModelProjectCredentialNativeKeepalive(t *testing.T) {
	requireProjectCredentialNative(t)
	h, _, s := credentialTestHandler(t)
	var requests, eof, closes atomic.Int32
	address, results, connections := summaryNativeListener(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		limit := time.Second
		if requests.Add(1) == 1 {
			limit = 120 * time.Millisecond
		}
		ctx, cancel := context.WithTimeout(r.Context(), limit)
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
		if _, e := io.WriteString(conn, method+" "+credentialTestDetail+" HTTP/1.1\r\nHost: "+address+"\r\n\r\n"); e != nil {
			t.Fatal(e)
		}
		response, e := http.ReadResponse(reader, &http.Request{Method: method})
		if e != nil {
			t.Fatal(e)
		}
		data, re := io.ReadAll(response.Body)
		ce := response.Body.Close()
		if re != nil || ce != nil || response.StatusCode != 200 || response.Header.Get("X-Request-ID") == "" {
			t.Fatal("incomplete native safe response")
		}
		if method == "GET" {
			length = int64(len(data))
			if length == 0 || response.ContentLength != length {
				t.Fatal("GET framing")
			}
		} else if len(data) != 0 || response.ContentLength != length {
			t.Fatal("HEAD framing")
		}
		if summaryNativeTerminal(t, results, time.Second).aborted {
			t.Fatal("success aborted")
		}
	}
	if requests.Load() != 4 || connections.Load() != 1 || s.reads.Load() != 4 || eof.Load() != 4 || closes.Load() != 4 {
		t.Fatal("deadline reset or body ownership")
	}
}
func TestModelProjectCredentialNativeBodyDeadline(t *testing.T) {
	requireProjectCredentialNative(t)
	for _, tc := range []struct {
		name, path     string
		parent, budget time.Duration
	}{{"natural_write", credentialTestCollection, time.Minute, projectCredentialWriteBudget}, {"natural_lookup", credentialTestLookup, time.Minute, projectCredentialReadBudget}, {"earlier_parent", credentialTestCollection, 120 * time.Millisecond, 120 * time.Millisecond}} {
		t.Run(tc.name, func(t *testing.T) {
			h, b, s := credentialTestHandler(t)
			var eof, closes atomic.Int32
			seen := make(chan context.Context, 1)
			b.auth = func(r *http.Request) (id.Actor, error) { seen <- r.Context(); return b.actor, nil }
			address, results, _ := summaryNativeListener(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				ctx, cancel := context.WithTimeout(r.Context(), tc.parent)
				defer cancel()
				r = r.WithContext(ctx)
				r.Body = summaryNativeBody{r.Body, &eof, &closes, false}
				h.ServeHTTP(w, r)
			}), false)
			conn := summaryNativeDial(t, address, false)
			start := time.Now()
			_, e := io.WriteString(conn, "POST "+tc.path+" HTTP/1.1\r\nHost: "+address+"\r\nContent-Type: application/json\r\nIdempotency-Key: native-original\r\nContent-Length: 100\r\n\r\n{")
			if e != nil {
				t.Fatal(e)
			}
			var ctx context.Context
			select {
			case ctx = <-seen:
			case <-time.After(time.Second):
				t.Fatal("authentication not reached")
			}
			deadline, ok := ctx.Deadline()
			if !ok || deadline.Sub(start) > tc.budget+time.Second || deadline.Sub(start) < tc.budget/2 {
				t.Fatal("native budget did not precede authentication")
			}
			data, e := io.ReadAll(conn)
			terminal := summaryNativeTerminal(t, results, time.Second)
			elapsed := time.Since(start)
			if e != nil || !terminal.aborted || ctx.Err() == nil || len(data) != 0 || s.writes.Load()+s.lookups.Load()+s.reads.Load() != 0 || closes.Load() != 1 || elapsed < tc.budget*3/4 || elapsed > tc.budget+time.Second {
				t.Fatal("slow input not bounded/closed", elapsed)
			}
		})
	}
}
func TestModelProjectCredentialNativeWriteAndClose(t *testing.T) {
	requireProjectCredentialNative(t)
	for _, mode := range []string{"short", "write_error", "flush_error", "close_error", "panic", "reset_error", "unsupported", "blocked_head_flush", "blocked_get_write"} {
		t.Run(mode, func(t *testing.T) {
			h, b, s := credentialTestHandler(t)
			var eof, closes atomic.Int32
			seen := make(chan *projectHTTPNativeObserved, 1)
			blocked := strings.HasPrefix(mode, "blocked_")
			address, results, _ := summaryNativeListener(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				ctx, cancel := context.WithTimeout(r.Context(), 120*time.Millisecond)
				defer cancel()
				r = r.WithContext(ctx)
				r.Body = summaryNativeBody{r.Body, &eof, &closes, mode == "close_error"}
				writer := &projectHTTPNativeObserved{summaryNativeWriter: &summaryNativeWriter{ResponseWriter: w, mode: mode}}
				seen <- writer
				if blocked {
					w.Header().Set("X-Test-Backpressure", strings.Repeat("x", 4<<20))
				}
				if mode == "unsupported" {
					h.ServeHTTP(summaryNativeUnsupported{writer}, r)
				} else {
					h.ServeHTTP(writer, r)
				}
			}), blocked)
			conn := summaryNativeDial(t, address, blocked)
			method := "GET"
			if mode == "blocked_head_flush" {
				method = "HEAD"
			}
			_, e := io.WriteString(conn, method+" "+credentialTestDetail+" HTTP/1.1\r\nHost: "+address+"\r\n\r\n")
			if e != nil {
				t.Fatal(e)
			}
			writer := <-seen
			terminal := summaryNativeTerminal(t, results, 2*time.Second)
			if !terminal.aborted || closes.Load() != 1 {
				t.Fatal("native terminal/Close missing")
			}
			if mode == "unsupported" && (b.checks.Load()+s.reads.Load() != 0) {
				t.Fatal("unsupported writer reached business")
			}
			if blocked {
				if !writer.timedOut.Load() {
					t.Fatal("backpressure did not observe actual net timeout")
				}
				if mode == "blocked_head_flush" && (writer.flushEntered.Load() == 0 || writer.flushExited.Load() != writer.flushEntered.Load()) {
					t.Fatal("HEAD actual Flush not retired")
				}
				if mode == "blocked_get_write" && (writer.writeEntered.Load() == 0 || writer.writeExited.Load() != writer.writeEntered.Load()) {
					t.Fatal("actual Write not retired")
				}
				if e = conn.(*net.TCPConn).SetReadBuffer(4 << 20); e != nil {
					t.Fatal(e)
				}
			}
			data, e := io.ReadAll(conn)
			if e != nil || strings.Contains(string(data), "application/problem+json") {
				t.Fatal("abort did not close or appended Problem")
			}
		})
	}
	t.Run("service_tail_actual_join", func(t *testing.T) {
		h, _, s := credentialTestHandler(t)
		entered, release := make(chan struct{}), make(chan struct{})
		var once sync.Once
		unblock := func() { once.Do(func() { close(release) }) }
		var owners sync.WaitGroup
		s.metadata = func(ctx context.Context, _ id.Actor, _ sc.CredentialRef) (sc.Metadata, error) {
			close(entered)
			<-release
			return sc.Metadata{}, ctx.Err()
		}
		address, results, _ := summaryNativeListener(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			owners.Add(1)
			defer owners.Done()
			ctx, cancel := context.WithTimeout(r.Context(), 100*time.Millisecond)
			defer cancel()
			h.ServeHTTP(w, r.WithContext(ctx))
		}), false)
		t.Cleanup(func() { unblock(); owners.Wait() })
		conn := summaryNativeDial(t, address, false)
		_, e := io.WriteString(conn, "GET "+credentialTestDetail+" HTTP/1.1\r\nHost: "+address+"\r\n\r\n")
		if e != nil {
			t.Fatal(e)
		}
		<-entered
		timer := time.NewTimer(150 * time.Millisecond)
		<-timer.C
		select {
		case <-results:
			t.Fatal("cancel substituted for service join")
		default:
		}
		unblock()
		if !summaryNativeTerminal(t, results, time.Second).aborted {
			t.Fatal("late result published")
		}
		data, e := io.ReadAll(conn)
		if e != nil || len(data) != 0 {
			t.Fatal("late bytes")
		}
	})
}
