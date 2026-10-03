package httpapi

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httptrace"
	"net/textproto"
	"strings"
	"sync"
	"testing"
	"time"
)

func awaitSignal(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(5 * time.Second):
		t.Fatal("HTTP test barrier timed out")
	}
}

func TestRealHTTPStreamingAndResponseController(t *testing.T) {
	release := make(chan struct{})
	var once sync.Once
	releaseHandler := func() { once.Do(func() { close(release) }) }
	flushed := make(chan struct{})
	handlerDone := make(chan struct{})
	server := httptest.NewServer(Handler(nil, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer close(handlerDone)
		if _, ok := w.(http.Flusher); !ok {
			t.Error("native HTTP Flusher lost")
			return
		}
		if _, ok := w.(http.Hijacker); !ok {
			t.Error("native HTTP/1 Hijacker lost")
			return
		}
		controller := http.NewResponseController(w)
		if err := controller.SetWriteDeadline(time.Now().Add(5 * time.Second)); err != nil {
			t.Error("Unwrap write deadline", err)
			return
		}
		if _, err := io.WriteString(w, "first\n"); err != nil {
			t.Error(err)
			return
		}
		if err := controller.Flush(); err != nil {
			t.Error(err)
			return
		}
		close(flushed)
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
		_, _ = io.WriteString(w, "second\n")
	})))
	defer server.Close()
	defer releaseHandler()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	r, _ := http.NewRequestWithContext(ctx, http.MethodGet, server.URL, nil)
	response, err := server.Client().Do(r)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	reader := bufio.NewReader(response.Body)
	first, err := reader.ReadString('\n')
	if err != nil || first != "first\n" {
		t.Fatalf("stream first chunk: %q %v", first, err)
	}
	awaitSignal(t, flushed)
	select {
	case <-handlerDone:
		t.Fatal("stream buffered until handler finished")
	default:
	}
	releaseHandler()
	rest, err := io.ReadAll(reader)
	if err != nil || string(rest) != "second\n" {
		t.Fatalf("stream rest: %q %v", rest, err)
	}
	awaitSignal(t, handlerDone)
}

func TestRealHTTPPanicAfterPartialResponseAborts(t *testing.T) {
	const secret = "panic-SENTINEL-secret"
	var logs, serverLogs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	server := httptest.NewUnstartedServer(Handler(logger, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "partial\n")
		if err := http.NewResponseController(w).Flush(); err != nil {
			t.Error(err)
			return
		}
		panic(errors.New(secret))
	})))
	server.Config.ErrorLog = log.New(&serverLogs, "", 0)
	server.Start()
	response, err := server.Client().Get(server.URL)
	if err != nil {
		server.Close()
		t.Fatal(err)
	}
	body, readErr := io.ReadAll(response.Body)
	_ = response.Body.Close()
	server.Close()
	if response.StatusCode != 200 || string(body) != "partial\n" || !errors.Is(readErr, io.ErrUnexpectedEOF) {
		t.Fatalf("partial response was not aborted: status=%d body=%q err=%v", response.StatusCode, body, readErr)
	}
	if strings.Contains(logs.String(), secret) || strings.Contains(serverLogs.String(), secret) || bytes.Contains(body, []byte(secret)) {
		t.Fatal("panic detail leaked")
	}
	if !strings.Contains(logs.String(), `"bytes":8`) || !strings.Contains(logs.String(), `"status":200`) || !strings.Contains(logs.String(), `"code":"INTERNAL_ERROR"`) {
		t.Fatal("partial-response access record missing")
	}
	if serverLogs.Len() != 0 {
		t.Fatalf("net/http emitted a raw panic: %s", serverLogs.String())
	}
}

func TestRealHTTPInformationalResponseCanBecomeProblem(t *testing.T) {
	server := httptest.NewServer(Handler(nil, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusEarlyHints)
		panic("not a committed final response")
	})))
	defer server.Close()
	interim := make(chan int, 1)
	trace := &httptrace.ClientTrace{Got1xxResponse: func(status int, _ textproto.MIMEHeader) error { interim <- status; return nil }}
	r, _ := http.NewRequestWithContext(httptrace.WithClientTrace(context.Background(), trace), http.MethodGet, server.URL, nil)
	response, err := server.Client().Do(r)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil || response.StatusCode != 500 || !bytes.Contains(body, []byte(`"commit_state":"unknown"`)) {
		t.Fatalf("final Problem: %d %s %v", response.StatusCode, body, err)
	}
	select {
	case status := <-interim:
		if status != 103 {
			t.Fatal(status)
		}
	default:
		t.Fatal("missing real 103 response")
	}
}

func TestRealHTTPHijack(t *testing.T) {
	server := httptest.NewServer(Handler(nil, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, rw, err := http.NewResponseController(w).Hijack()
		if err != nil {
			t.Error(err)
			return
		}
		defer conn.Close()
		_, err = fmt.Fprintf(rw, "HTTP/1.1 200 OK\r\nContent-Length: 8\r\nX-Request-ID: %s\r\n\r\nhijacked", RequestID(r.Context()).String())
		if err == nil {
			err = rw.Flush()
		}
		if err != nil {
			t.Error(err)
		}
	})))
	defer server.Close()
	response, err := server.Client().Get(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil || string(body) != "hijacked" || response.Header.Get("X-Request-ID") == "" {
		t.Fatalf("hijack lost: %s %v", body, err)
	}
}

func TestRealHTTPPanicClosesHijackedConnection(t *testing.T) {
	returned := make(chan struct{})
	server := httptest.NewServer(Handler(nil, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer close(returned)
		_, rw, err := http.NewResponseController(w).Hijack()
		if err != nil {
			t.Error(err)
			return
		}
		_, _ = rw.WriteString("prefix\n")
		_ = rw.Flush()
		panic("hijack-SENTINEL-secret")
	})))
	defer server.Close()
	conn, err := net.DialTimeout("tcp", server.Listener.Addr().String(), 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	_, err = io.WriteString(conn, "GET / HTTP/1.1\r\nHost: localhost\r\n\r\n")
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(conn)
	if err != nil || string(body) != "prefix\n" {
		t.Fatalf("hijacked panic left connection open or appended output: %q %v", body, err)
	}
	awaitSignal(t, returned)
}

func TestRealHTTPClientCancellationReachesHandler(t *testing.T) {
	started, cancelled := make(chan struct{}), make(chan struct{})
	server := httptest.NewServer(Handler(nil, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-r.Context().Done()
		if !errors.Is(r.Context().Err(), context.Canceled) {
			t.Error("handler cancellation reason")
		}
		close(cancelled)
	})))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	r, _ := http.NewRequestWithContext(ctx, http.MethodGet, server.URL, nil)
	done := make(chan error, 1)
	go func() {
		response, err := server.Client().Do(r)
		if response != nil {
			_ = response.Body.Close()
		}
		done <- err
	}()
	awaitSignal(t, started)
	cancel()
	awaitSignal(t, cancelled)
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("client request not cancelled: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("client cancellation timed out")
	}
}
