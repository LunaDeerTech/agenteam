package knowledgehttp

import (
	"bufio"
	"context"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"fmt"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/httpapi"
	kc "github.com/LunaDeerTech/agenteam/internal/central/knowledge/contract"
	"strings"
)

// These tests use actual loopback TCP and require a separately granted window.
// Their controlled authority/domain ports prove native HTTP ownership only;
// the authenticated default production root has its own process acceptance.
func requireNative(t *testing.T) {
	t.Helper()
	if os.Getenv("AGENTEAM_KNOWLEDGE_HTTP_NATIVE") != "1" {
		t.Skip("requires an explicitly granted Knowledge metadata HTTP native window")
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

func TestKnowledgeHTTPNativeDeadlines(t *testing.T) {
	requireNative(t)
	for _, tc := range []struct {
		name, method, path string
		budget             time.Duration
		parent             bool
	}{
		{"read-natural", "GET", "/knowledge/documents", readBudget, false},
		{"earlier-parent", "GET", "/knowledge/documents", 150 * time.Millisecond, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, _, ports := testHandler()
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
			_, err := io.WriteString(conn, tc.method+" "+testPath(tc.path)+" HTTP/1.1\r\nHost: "+address+"\r\nContent-Type: application/json\r\nIdempotency-Key: original-intent\r\nContent-Length: 100\r\n\r\n{")
			if err != nil {
				t.Fatal(err)
			}
			raw, err := io.ReadAll(conn)
			terminal := nativeTerminal(t, results)
			elapsed := time.Since(started)
			if err != nil || !terminal.aborted || len(raw) != 0 || elapsed < tc.budget*3/4 || elapsed > tc.budget+2*time.Second || closes.Load() != 1 || ports.calls != 0 {
				t.Fatal("actual deadline/EOF/body owner", err, elapsed, len(raw), closes.Load(), ports.calls)
			}
		})
	}
}

func TestKnowledgeHTTPNativeKeepAliveAndClose(t *testing.T) {
	requireNative(t)
	t.Run("cleared-deadline-keeps-real-connection", func(t *testing.T) {
		h, _, ports := testHandler()
		address, results, count := nativeListener(t, h, false)
		conn := nativeDial(t, address)
		reader := bufio.NewReader(conn)
		read := func(method string) {
			t.Helper()
			response, err := http.ReadResponse(reader, &http.Request{Method: method})
			if err != nil {
				t.Fatal(err)
			}
			raw, err := io.ReadAll(response.Body)
			closeErr := response.Body.Close()
			if err != nil || closeErr != nil || response.StatusCode != 200 || method == "GET" && len(raw) == 0 || method == "HEAD" && len(raw) != 0 {
				t.Fatal("complete response", err, closeErr)
			}
			if nativeTerminal(t, results).aborted {
				t.Fatal("success aborted")
			}
		}
		nativeSend(t, conn, address, "GET", "/knowledge/documents", "", false)
		read("GET")
		// Crossing the prior installed deadline is the actual reuse assertion.
		timer := time.NewTimer(readBudget + 150*time.Millisecond)
		defer timer.Stop()
		<-timer.C
		nativeSend(t, conn, address, "HEAD", "/knowledge/documents", "", true)
		read("HEAD")
		if count.Load() != 1 || ports.calls != 2 {
			t.Fatal("did not reuse exact connection")
		}
	})
	t.Run("real-body-close-error-aborts-before-response", func(t *testing.T) {
		h, _, ports := testHandler()
		var closes, eof atomic.Int32
		var callsAtClose atomic.Int32
		address, results, _ := nativeListener(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r.Body = nativeBody{ReadCloser: r.Body, closes: &closes, eof: &eof, beforeClose: func() error {
				callsAtClose.Store(int32(ports.calls))
				return errors.New("private close diagnostic")
			}}
			h.ServeHTTP(w, r)
		}), false)
		conn := nativeDial(t, address)
		nativeSend(t, conn, address, "GET", "/knowledge/documents", "", true)
		raw, err := io.ReadAll(conn)
		terminal := nativeTerminal(t, results)
		// The read and encoding precede Close. A failed original-body Close
		// must suppress publication, not retroactively undo the library read.
		if err != nil || len(raw) != 0 || !terminal.aborted || eof.Load() != 1 || closes.Load() != 1 || callsAtClose.Load() != 1 || ports.calls != 1 {
			t.Fatal("Close did not abort and join", err != nil, len(raw), terminal.aborted, eof.Load(), closes.Load(), callsAtClose.Load(), ports.calls)
		}
	})
}

type nativeWriteObserver struct {
	http.ResponseWriter
	entered chan struct{}
	once    sync.Once
}

func (w *nativeWriteObserver) Unwrap() http.ResponseWriter { return w.ResponseWriter }
func (w *nativeWriteObserver) Write(p []byte) (int, error) {
	w.once.Do(func() { close(w.entered) })
	return w.ResponseWriter.Write(p)
}
func TestKnowledgeHTTPNativeBackpressureAndDisconnect(t *testing.T) {
	requireNative(t)
	t.Run("summary-write-natural-deadline", func(t *testing.T) {
		h, _, ports := testHandler()
		items := make([]kc.DocumentRef, 200)
		for i := range items {
			items[i] = testDocument()
			items[i].ID = testID[kc.Document](100 + i)
			items[i].Title = strings.Repeat("<", kc.MaxTitleRunes-4) + fmt.Sprintf("%04d", i)
		}
		ports.page = f.Page[kc.DocumentRef]{Items: items}
		entered := make(chan struct{})
		address, results, _ := nativeListener(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h.ServeHTTP(&nativeWriteObserver{ResponseWriter: w, entered: entered}, r)
		}), true)
		conn := nativeDial(t, address)
		if e := conn.SetReadBuffer(1024); e != nil {
			t.Fatal(e)
		}
		start := time.Now()
		nativeSend(t, conn, address, "GET", "/knowledge/documents?limit=200", "", true)
		select {
		case <-entered:
		case <-time.After(time.Second):
			t.Fatal("real Write not reached")
		}
		terminal := nativeTerminal(t, results)
		elapsed := time.Since(start)
		if !terminal.aborted || elapsed < readBudget*3/4 || elapsed > readBudget+2*time.Second || ports.calls != 1 {
			t.Fatal("blocked actual Write did not expire", elapsed)
		}
		if e := conn.Close(); e != nil {
			t.Fatal(e)
		}
	})
	t.Run("disconnect-cancels-actual-library-tail", func(t *testing.T) {
		h, _, ports := testHandler()
		entered, cancelled, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
		var once sync.Once
		unpark := func() { once.Do(func() { close(release) }) }
		ports.before = func(ctx context.Context) { close(entered); <-ctx.Done(); close(cancelled); <-release }
		address, results, _ := nativeListener(t, h, false)
		t.Cleanup(unpark)
		conn := nativeDial(t, address)
		nativeSend(t, conn, address, "GET", "/knowledge/documents", "", false)
		joined(t, entered)
		if e := conn.Close(); e != nil {
			t.Fatal(e)
		}
		joined(t, cancelled)
		select {
		case <-results:
			t.Fatal("HTTP released owner before actual library return")
		default:
		}
		unpark()
		if !nativeTerminal(t, results).aborted {
			t.Fatal("disconnect emitted successful response")
		}
	})
}
