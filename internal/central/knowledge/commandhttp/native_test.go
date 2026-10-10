//go:build integration

package commandhttp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
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
	"syscall"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/httpapi"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	kc "github.com/LunaDeerTech/agenteam/internal/central/knowledge/contract"
)

// Real net/http + owned loopback sockets, separately scheduled from pure.
// Account/domain ports here are controlled; real authority/SQL have separate
// integration tops. A default-root or authenticated network claim is excluded.
func treeNativeGrant(t *testing.T) {
	t.Helper()
	if os.Getenv("AGENTEAM_KNOWLEDGE_TREE_HTTP_NATIVE") != "1" {
		t.Fatal("requires the separately granted tree-command native window")
	}
}

type treeNativeResult struct{ aborted bool }
type treeNativeBody struct {
	io.ReadCloser
	closed, eof *atomic.Int32
	closeError  error
}

func (b treeNativeBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	if err == io.EOF {
		b.eof.Add(1)
	}
	return n, err
}
func (b treeNativeBody) Close() error {
	err := b.ReadCloser.Close()
	b.closed.Add(1)
	return errors.Join(err, b.closeError)
}

func treeNativeServer(t *testing.T, h http.Handler, small bool) (string, <-chan treeNativeResult, *atomic.Int32) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	out := make(chan treeNativeResult, 16)
	count := new(atomic.Int32)
	var mu sync.Mutex
	connections := map[net.Conn]bool{}
	changed := make(chan struct{})
	var handlers sync.WaitGroup
	server := &http.Server{ErrorLog: log.New(io.Discard, "", 0), Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handlers.Add(1)
		defer handlers.Done()
		defer func() {
			p := recover()
			out <- treeNativeResult{p == http.ErrAbortHandler}
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
			count.Add(1)
			connections[conn] = true
			if small {
				if err := conn.(*net.TCPConn).SetWriteBuffer(1024); err != nil {
					t.Error("owned write buffer", err)
				}
			}
		case http.StateClosed:
			delete(connections, conn)
		case http.StateHijacked:
			t.Error("tree handler must never hijack")
		}
		close(changed)
		changed = make(chan struct{})
	}}
	served := make(chan error, 1)
	go func() { served <- server.Serve(listener) }()
	t.Cleanup(func() {
		if err := server.Close(); err != nil {
			t.Error("owned server Close", err)
		}
		_ = listener.Close()
		select {
		case err := <-served:
			if !errors.Is(err, http.ErrServerClosed) {
				t.Error("actual Serve terminal", err)
			}
		case <-time.After(3 * time.Second):
			t.Fatal("original Serve did not return")
		}
		deadline := time.NewTimer(3 * time.Second)
		defer deadline.Stop()
		for {
			mu.Lock()
			empty, next := len(connections) == 0, changed
			mu.Unlock()
			if empty {
				break
			}
			select {
			case <-next:
			case <-deadline.C:
				t.Fatal("original native connections did not retire")
			}
		}
		// With no accepted connection left, no new handler can enter Add.
		joined := make(chan struct{})
		go func() { handlers.Wait(); close(joined) }()
		select {
		case <-joined:
		case <-time.After(3 * time.Second):
			t.Fatal("original handlers did not join")
		}
	})
	return listener.Addr().String(), out, count
}
func treeNativeDial(t *testing.T, address string) *net.TCPConn {
	t.Helper()
	conn, err := net.DialTimeout("tcp", address, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	tcp := conn.(*net.TCPConn)
	t.Cleanup(func() {
		if err := tcp.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
			t.Error(err)
		}
	})
	if err = tcp.SetDeadline(time.Now().Add(10 * time.Second)); err != nil {
		t.Fatal(err)
	}
	return tcp
}
func treeNativeTerminal(t *testing.T, out <-chan treeNativeResult) treeNativeResult {
	t.Helper()
	select {
	case r := <-out:
		return r
	case <-time.After(5 * time.Second):
		t.Fatal("native original request has no terminal")
	}
	return treeNativeResult{}
}
func treeNativeSend(t *testing.T, w io.Writer, address, method, action, body string, closeConnection bool) {
	t.Helper()
	connection, key := "", "Idempotency-Key: saved-original-intent\r\n"
	if closeConnection {
		connection = "Connection: close\r\n"
	}
	if action == "delete-preview" {
		key = ""
	}
	_, err := fmt.Fprintf(w, "%s %s HTTP/1.1\r\nHost: %s\r\nContent-Type: application/json\r\n%s%sContent-Length: %d\r\n\r\n%s", method, testPath(action), address, key, connection, len(body), body)
	if err != nil {
		t.Fatal("native send", err)
	}
}

func TestTreeCommandsHTTPNativeReadDeadlines(t *testing.T) {
	treeNativeGrant(t)
	for _, early := range []bool{false, true} {
		name := "original_two_seconds"
		if early {
			name = "earlier_parent"
		}
		t.Run(name, func(t *testing.T) {
			h, service, _ := fixture()
			var closed, eof atomic.Int32
			budget := requestBudget
			if early {
				budget = 150 * time.Millisecond
			}
			address, out, _ := treeNativeServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if early {
					ctx, cancel := context.WithTimeout(r.Context(), budget)
					defer cancel()
					r = r.WithContext(ctx)
				}
				r.Body = treeNativeBody{ReadCloser: r.Body, closed: &closed, eof: &eof}
				h.ServeHTTP(w, r)
			}), false)
			conn := treeNativeDial(t, address)
			start := time.Now()
			if _, err := fmt.Fprintf(conn, "POST %s HTTP/1.1\r\nHost: %s\r\nContent-Type: application/json\r\nIdempotency-Key: held-body\r\nContent-Length: 1000\r\n\r\n{", testPath("rename"), address); err != nil {
				t.Fatal(err)
			}
			result := treeNativeTerminal(t, out)
			elapsed := time.Since(start)
			if !result.aborted || service.calls != 0 || closed.Load() != 1 || eof.Load() != 0 || elapsed < budget*3/4 || elapsed > budget+800*time.Millisecond {
				t.Fatal("original native body deadline/Close boundary")
			}
			var b [1]byte
			if n, err := conn.Read(b[:]); n != 0 || !(errors.Is(err, io.EOF) || errors.Is(err, syscall.ECONNRESET)) {
				t.Fatal("failed body unexpectedly published/reused connection")
			}
		})
	}
}

func TestTreeCommandsHTTPNativeKeepAliveAndClose(t *testing.T) {
	treeNativeGrant(t)
	t.Run("normal_deadlines_cleared_for_original_connection", func(t *testing.T) {
		h, service, _ := fixture()
		address, out, count := treeNativeServer(t, h, false)
		conn := treeNativeDial(t, address)
		reader := bufio.NewReader(conn)
		for i := 0; i < 2; i++ {
			treeNativeSend(t, conn, address, "POST", "rename", `{"expected_version":"1","title":"original"}`, false)
			response, err := http.ReadResponse(reader, &http.Request{Method: "POST"})
			if err != nil {
				t.Fatal(err)
			}
			raw, readErr := io.ReadAll(response.Body)
			closeErr := response.Body.Close()
			if response.StatusCode != 200 || readErr != nil || closeErr != nil || int64(len(raw)) != response.ContentLength || treeNativeTerminal(t, out).aborted {
				t.Fatal("native complete original response")
			}
			if i == 0 {
				timer := time.NewTimer(requestBudget + 100*time.Millisecond)
				<-timer.C
			}
		}
		if count.Load() != 1 || service.calls != 2 {
			t.Fatal("keepalive opened another connection")
		}
	})
	t.Run("original_body_close_error_no_response", func(t *testing.T) {
		h, service, _ := fixture()
		var closed, eof atomic.Int32
		address, out, _ := treeNativeServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r.Body = treeNativeBody{ReadCloser: r.Body, closed: &closed, eof: &eof, closeError: errors.New("native owned close fault")}
			h.ServeHTTP(w, r)
		}), false)
		conn := treeNativeDial(t, address)
		treeNativeSend(t, conn, address, "POST", "rename", `{"expected_version":"1","title":"original"}`, false)
		result := treeNativeTerminal(t, out)
		if !result.aborted || service.calls != 1 || closed.Load() != 1 || eof.Load() != 1 {
			t.Fatal("original service success/Close failure boundary")
		}
		var b [1]byte
		if n, err := conn.Read(b[:]); n != 0 || !(errors.Is(err, io.EOF) || errors.Is(err, syscall.ECONNRESET)) {
			t.Fatal("Close failure published a response")
		}
	})
}

type treeNativeWriter struct {
	http.ResponseWriter
	entered  chan<- struct{}
	returned chan<- error
}

func (w treeNativeWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }
func (w treeNativeWriter) Write(p []byte) (int, error) {
	w.entered <- struct{}{}
	n, err := w.ResponseWriter.Write(p)
	w.returned <- err
	return n, err
}

func TestTreeCommandsHTTPNativeWriteAndDisconnect(t *testing.T) {
	treeNativeGrant(t)
	t.Run("original_write_backpressure_deadline", func(t *testing.T) {
		h, service, _ := fixture()
		root := testDocument()
		nodes := make([]kc.DocumentRef, 2000)
		scope := make([]kc.ScopeNode, len(nodes))
		for i := range nodes {
			d := root
			if i != 0 {
				d.ID = testID[kc.Document](1000 + i)
				p := root.ID
				d.ParentDocumentID = &p
			}
			d.Title = strings.Repeat("x", 512)
			nodes[i] = d
			scope[i] = kc.ScopeNode{ID: d.ID, ProjectID: d.ProjectID, ParentID: d.ParentDocumentID, ContentVersion: d.ContentVersion, Status: d.Status}
		}
		digest := must(kc.SubtreeDigest(root.ProjectID, root.ID, scope))
		keys := must(kc.LoadConfirmationKeys(fmt.Sprintf(`{"format":1,"current_kid":"native","keys":[{"kid":"native","key_b64":"%s"}]}`, base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{6}, 32)))))
		token := must(keys.Sign(kc.DeleteConfirmationClaims{UserID: testID[id.User](1), ProjectID: root.ProjectID, RootID: root.ID, ScopeDigest: digest, ExpiresAt: testAt()}))
		service.preview = kc.DeletePreview{Root: root.ID, Nodes: nodes, ScopeDigest: digest, Confirmation: token, ExpiresAt: testAt()}
		entered, returned := make(chan struct{}, 1), make(chan error, 1)
		address, out, _ := treeNativeServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { h.ServeHTTP(treeNativeWriter{w, entered, returned}, r) }), true)
		conn := treeNativeDial(t, address)
		if err := conn.SetReadBuffer(1024); err != nil {
			t.Fatal(err)
		}
		start := time.Now()
		treeNativeSend(t, conn, address, "POST", "delete-preview", `{}`, false)
		select {
		case <-entered:
		case <-time.After(time.Second):
			t.Fatal("original Write not reached")
		}
		result := treeNativeTerminal(t, out)
		if !result.aborted || <-returned == nil || time.Since(start) > requestBudget+800*time.Millisecond {
			t.Fatal("native original write deadline/return")
		}
	})
	t.Run("disconnect_cancels_original_call_before_handler_tail", func(t *testing.T) {
		h, service, _ := fixture()
		entered, returned := make(chan struct{}), make(chan struct{})
		service.before = func(ctx context.Context) { close(entered); <-ctx.Done(); close(returned) }
		address, out, _ := treeNativeServer(t, h, false)
		conn := treeNativeDial(t, address)
		treeNativeSend(t, conn, address, "POST", "rename", `{"expected_version":"1","title":"original"}`, false)
		select {
		case <-entered:
		case <-time.After(time.Second):
			t.Fatal("original domain call not entered")
		}
		if err := conn.Close(); err != nil {
			t.Fatal(err)
		}
		result := treeNativeTerminal(t, out)
		select {
		case <-returned:
		default:
			t.Fatal("original call abandoned")
		}
		if !result.aborted || service.calls != 1 {
			t.Fatal("disconnect did not terminate original call")
		}
	})
}
