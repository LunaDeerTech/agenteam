package model

import (
	"bufio"
	"context"
	"io"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

func requireProjectConfigurationNative(t *testing.T) {
	t.Helper()
	if os.Getenv("AGENTEAM_PROJECT_MODEL_CONFIGURATION_NATIVE") != "1" {
		t.Skip("requires the explicitly owned Project Model Configuration native window")
	}
}
func TestProjectModelConfigurationNativeBodyDeadline(t *testing.T) {
	requireProjectConfigurationNative(t)
	for _, tc := range []struct {
		name, path     string
		parent, budget time.Duration
	}{{"natural_write", configurationTestModels, time.Minute, configurationWriteBudget}, {"natural_lookup", configurationTestLookup, time.Minute, configurationLookupBudget}, {"earlier_parent", configurationTestModels, 120 * time.Millisecond, 120 * time.Millisecond}} {
		t.Run(tc.name, func(t *testing.T) {
			h, b, s := configurationTestHandler(t)
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
			if e != nil || !terminal.aborted || ctx.Err() == nil || len(data) != 0 || s.writes.Load()+s.lookups.Load() != 0 || closes.Load() != 1 || elapsed < tc.budget*3/4 || elapsed > tc.budget+time.Second {
				t.Fatal("slow input not bounded/closed", elapsed)
			}
		})
	}
}

// The HEAD error path may publish its first 405 header before Flush times out.
// Count publication calls separately from the existing actual Write/Flush owner.
type configurationNativeHeaderObserved struct {
	*projectHTTPNativeObserved
	headerCalls, headerStatus atomic.Int32
}

func (w *configurationNativeHeaderObserved) WriteHeader(status int) {
	w.headerCalls.Add(1)
	w.headerStatus.Store(int32(status))
	w.projectHTTPNativeObserved.WriteHeader(status)
}

func TestProjectModelConfigurationNativeWriteAndClose(t *testing.T) {
	requireProjectConfigurationNative(t)
	for _, mode := range []string{"short", "write_error", "flush_error", "close_error", "panic", "reset_error", "unsupported", "blocked_head_flush", "blocked_get_write"} {
		t.Run(mode, func(t *testing.T) {
			h, b, s := configurationTestHandler(t)
			var eof, closes atomic.Int32
			seen := make(chan *configurationNativeHeaderObserved, 1)
			blocked := strings.HasPrefix(mode, "blocked_")
			address, results, _ := summaryNativeListener(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				ctx, cancel := context.WithTimeout(r.Context(), time.Minute)
				defer cancel()
				r = r.WithContext(ctx)
				r.Body = summaryNativeBody{r.Body, &eof, &closes, mode == "close_error"}
				writer := &configurationNativeHeaderObserved{projectHTTPNativeObserved: &projectHTTPNativeObserved{summaryNativeWriter: &summaryNativeWriter{ResponseWriter: w, mode: mode}}}
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
			method := "POST"
			if mode == "blocked_head_flush" {
				method = "HEAD"
			}
			_, e := io.WriteString(conn, configurationNativeRequest(method, configurationTestLookup, address, `{"command":"model.delete"}`))
			if e != nil {
				t.Fatal(e)
			}
			var writer *configurationNativeHeaderObserved
			select {
			case writer = <-seen:
			case <-time.After(time.Second):
				t.Fatal("actual writer owner missing")
			}
			terminal := summaryNativeTerminal(t, results, 3*time.Second)
			if !terminal.aborted || closes.Load() != 1 {
				t.Fatal("native terminal/Close missing")
			}
			if mode == "unsupported" && (b.checks.Load()+s.writes.Load()+s.lookups.Load() != 0) {
				t.Fatal("unsupported writer reached business")
			}
			if blocked {
				if !writer.timedOut.Load() {
					t.Fatal("backpressure did not observe actual net timeout")
				}
				if mode == "blocked_head_flush" && (writer.headerCalls.Load() != 1 || writer.headerStatus.Load() != http.StatusMethodNotAllowed || writer.writeEntered.Load() != 0 || writer.writeExited.Load() != 0 || writer.writeAsked.Load() != 0 || writer.writeReturned.Load() != 0 || writer.flushEntered.Load() != 1 || writer.flushExited.Load() != 1) {
					t.Fatal("HEAD actual Flush not retired")
				}
				if mode == "blocked_get_write" && (writer.writeEntered.Load() == 0 || writer.writeExited.Load() != writer.writeEntered.Load() || writer.writeAsked.Load() <= 0 || writer.writeReturned.Load() < 0 || writer.writeReturned.Load() > writer.writeAsked.Load()) {
					t.Fatal("actual Write not retired")
				}
				t.Logf("actual_write_entered=%d actual_write_exited=%d actual_write_asked=%d actual_write_returned=%d actual_flush_entered=%d actual_flush_exited=%d net_timeout=%t", writer.writeEntered.Load(), writer.writeExited.Load(), writer.writeAsked.Load(), writer.writeReturned.Load(), writer.flushEntered.Load(), writer.flushExited.Load(), writer.timedOut.Load())
				if e = conn.(*net.TCPConn).SetReadBuffer(4 << 20); e != nil {
					t.Fatal(e)
				}
			}
			data, e := io.ReadAll(conn)
			if mode == "blocked_head_flush" {
				text := string(data)
				separator := strings.Index(text, "\r\n\r\n")
				bodyBytes := -1 // No complete header was received.
				if separator >= 0 {
					bodyBytes = len(data) - separator - 4
				}
				t.Logf("actual_header_calls=%d actual_header_status=%d read_error_present=%t received_bytes=%d header_complete=%t body_bytes=%d", writer.headerCalls.Load(), writer.headerStatus.Load(), e != nil, len(data), separator >= 0, bodyBytes)
				if e != nil {
					t.Fatal("HEAD client read did not retire at EOF")
				}
				const statusLine = "HTTP/1.1 405 Method Not Allowed\r\n"
				if !strings.HasPrefix(text, statusLine) && !strings.HasPrefix(statusLine, text) {
					t.Fatal("HEAD response was not the original 405 header")
				}
				if separator >= 0 && bodyBytes != 0 {
					t.Fatal("HEAD published bytes after its header")
				}
			} else if e != nil || strings.Contains(string(data), "application/problem+json") {
				t.Fatal("abort did not close or appended Problem")
			}
		})
	}
	t.Run("service_tail_actual_join", func(t *testing.T) {
		h, _, s := configurationTestHandler(t)
		entered, release := make(chan struct{}), make(chan struct{})
		var once sync.Once
		unblock := func() { once.Do(func() { close(release) }) }
		var owners sync.WaitGroup
		s.lookup = func(ctx context.Context, _ LookupCommandRequest) (CommandLookup, error) {
			close(entered)
			<-release
			return CommandLookup{}, ctx.Err()
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
		_, e := io.WriteString(conn, configurationNativeRequest("POST", configurationTestLookup, address, `{"command":"model.delete"}`))
		if e != nil {
			t.Fatal(e)
		}
		configurationTestSignal(t, entered)
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

func configurationNativeRequest(method, path, address, body string) string {
	return method + " " + path + " HTTP/1.1\r\nHost: " + address + "\r\nContent-Type: application/json\r\nIdempotency-Key: native-original\r\nContent-Length: " + strconv.Itoa(len(body)) + "\r\n\r\n" + body
}
func TestProjectModelConfigurationNativeKeepalive(t *testing.T) {
	requireProjectConfigurationNative(t)
	h, b, s := configurationTestHandler(t)
	var eof, closes, requests atomic.Int32
	b.auth = func(r *http.Request) (id.Actor, error) {
		d, ok := r.Context().Deadline()
		want := configurationWriteBudget
		if strings.HasSuffix(r.URL.Path, "lookup") {
			want = configurationLookupBudget
		}
		if !ok || time.Until(d) > want || time.Until(d) < want-time.Second {
			return b.actor, context.DeadlineExceeded
		}
		return b.actor, nil
	}
	address, results, connections := summaryNativeListener(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		r.Body = summaryNativeBody{r.Body, &eof, &closes, false}
		h.ServeHTTP(w, r)
	}), false)
	conn := summaryNativeDial(t, address, false)
	reader := bufio.NewReader(conn)
	for _, tc := range []configurationTestCase{configurationTestCases()[6], configurationTestCases()[0], configurationTestCases()[6]} {
		if _, e := io.WriteString(conn, configurationNativeRequest(tc.method, tc.path, address, tc.body)); e != nil {
			t.Fatal(e)
		}
		response, e := http.ReadResponse(reader, &http.Request{Method: tc.method})
		if e != nil {
			t.Fatal(e)
		}
		data, re := io.ReadAll(response.Body)
		ce := response.Body.Close()
		if re != nil || ce != nil || response.StatusCode != 200 || response.ContentLength != int64(len(data)) || len(data) == 0 || response.Header.Get("X-Request-ID") == "" {
			t.Fatal("complete native receipt")
		}
		if summaryNativeTerminal(t, results, time.Second).aborted {
			t.Fatal("success aborted")
		}
		if tc.kind == "lookup" && requests.Load() == 1 {
			timer := time.NewTimer(2100 * time.Millisecond)
			<-timer.C
		}
	}
	if requests.Load() != 3 || connections.Load() != 1 || s.writes.Load() != 1 || s.lookups.Load() != 2 || closes.Load() != 3 || eof.Load() != 3 {
		t.Fatal("write/lookup reset or ownership")
	}
}
