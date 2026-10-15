package mcphttp

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

// The real net/http transport owns these deadlines and callbacks. Business
// authority is tested separately through the original App and PostgreSQL.
type credentialWireBody struct {
	io.ReadCloser
	closed *atomic.Int32
	fail   bool
}

func (b credentialWireBody) Close() error {
	b.closed.Add(1)
	err := b.ReadCloser.Close()
	if b.fail {
		return io.ErrClosedPipe
	}
	return err
}

type credentialWireWriter struct {
	http.ResponseWriter
	mode string
}

func (w credentialWireWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }
func (w credentialWireWriter) SetReadDeadline(at time.Time) error {
	return http.NewResponseController(w.ResponseWriter).SetReadDeadline(at)
}
func (w credentialWireWriter) SetWriteDeadline(at time.Time) error {
	return http.NewResponseController(w.ResponseWriter).SetWriteDeadline(at)
}
func (w credentialWireWriter) FlushError() error {
	if w.mode == "flush" {
		return io.ErrClosedPipe
	}
	return http.NewResponseController(w.ResponseWriter).Flush()
}
func (w credentialWireWriter) Write(body []byte) (int, error) {
	if w.mode == "short" && len(body) > 1 {
		return w.ResponseWriter.Write(body[:len(body)-1])
	}
	return w.ResponseWriter.Write(body)
}
func credentialWireTerminal(t *testing.T, returned <-chan bool) bool {
	t.Helper()
	select {
	case aborted := <-returned:
		return aborted
	case <-time.After(2 * time.Second):
		t.Fatal("credential original HTTP handler did not join")
		return false
	}
}
func credentialWireServe(returned chan<- bool, handler func(http.ResponseWriter, *http.Request)) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			cause := recover()
			returned <- cause != nil
			if cause != nil {
				panic(cause)
			}
		}()
		handler(w, r)
	})
}
func TestMCPCredentialServeHTTPRealBodyDeadline(t *testing.T) {
	h, writes := credentialProbe(t)
	returned := make(chan bool, 1)
	var closes atomic.Int32
	server := httptest.NewServer(credentialWireServe(returned, func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 80*time.Millisecond)
		defer cancel()
		r = r.WithContext(ctx)
		r.Body = credentialWireBody{ReadCloser: r.Body, closed: &closes}
		h.ServeHTTP(w, r)
	}))
	defer server.Close()
	conn, err := net.DialTimeout("tcp", server.Listener.Addr().String(), time.Second)
	if err != nil {
		t.Fatal("credential owned listener dial")
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(2 * time.Second))
	_, err = io.WriteString(conn, "POST "+credentialCollectionPath+" HTTP/1.1\r\nHost: owned.test\r\nContent-Type: application/json\r\nIdempotency-Key: owned-body\r\nContent-Length: 100\r\n\r\n{")
	if err != nil {
		t.Fatal("credential owned partial body send")
	}
	data, err := io.ReadAll(conn)
	if err != nil || len(data) != 0 || !credentialWireTerminal(t, returned) || closes.Load() != 1 || writes.calls != 0 {
		t.Fatal("actual body deadline did not abort and join without dispatch")
	}
}
func TestMCPCredentialServeHTTPRealKeepaliveAndAbort(t *testing.T) {
	t.Run("keepalive_get_head_reset", func(t *testing.T) {
		h, writes := credentialProbe(t)
		returned := make(chan bool, 1)
		var closes, connections atomic.Int32
		server := httptest.NewUnstartedServer(credentialWireServe(returned, func(w http.ResponseWriter, r *http.Request) {
			ctx, cancel := context.WithTimeout(r.Context(), 80*time.Millisecond)
			defer cancel()
			r = r.WithContext(ctx)
			r.Body = credentialWireBody{ReadCloser: r.Body, closed: &closes}
			h.ServeHTTP(w, r)
		}))
		server.Config.ConnState = func(_ net.Conn, state http.ConnState) {
			if state == http.StateNew {
				connections.Add(1)
			}
		}
		server.Start()
		defer server.Close()
		client := server.Client()
		client.Timeout = time.Second
		defer client.CloseIdleConnections()
		var length int64
		for _, method := range []string{http.MethodGet, http.MethodHead, http.MethodGet} {
			request, _ := http.NewRequest(method, server.URL+credentialCollectionPath+"/"+credentialID, nil)
			response, err := client.Do(request)
			if err != nil {
				t.Fatal("credential keepalive request")
			}
			body, readErr := io.ReadAll(response.Body)
			closeErr := response.Body.Close()
			if readErr != nil || closeErr != nil || response.StatusCode != 200 || credentialWireTerminal(t, returned) {
				t.Fatal("credential successful wire did not join")
			}
			if method == http.MethodHead {
				if len(body) != 0 || response.ContentLength != length {
					t.Fatal("credential HEAD changed safe representation")
				}
			} else {
				length = int64(len(body))
				if length == 0 || response.ContentLength != length {
					t.Fatal("credential GET framing")
				}
			}
			timer := time.NewTimer(100 * time.Millisecond)
			<-timer.C
		}
		if connections.Load() != 1 || closes.Load() != 3 || writes.calls != 3 {
			t.Fatal("credential deadline not cleared or body ownership lost")
		}
	})
	for _, mode := range []string{"short", "flush", "close"} {
		t.Run(mode, func(t *testing.T) {
			h, writes := credentialProbe(t)
			returned := make(chan bool, 1)
			var closes atomic.Int32
			server := httptest.NewServer(credentialWireServe(returned, func(w http.ResponseWriter, r *http.Request) {
				r.Body = credentialWireBody{ReadCloser: r.Body, closed: &closes, fail: mode == "close"}
				h.ServeHTTP(credentialWireWriter{w, mode}, r)
			}))
			defer server.Close()
			client := server.Client()
			client.Timeout = time.Second
			defer client.CloseIdleConnections()
			response, err := client.Get(server.URL + credentialCollectionPath + "/" + credentialID)
			var data []byte
			if response != nil {
				data, _ = io.ReadAll(response.Body)
				_ = response.Body.Close()
			}
			if !credentialWireTerminal(t, returned) || closes.Load() != 1 || writes.calls != 1 || strings.Contains(string(data), "application/problem+json") {
				t.Fatal("credential wire failure did not abort with one closed owner")
			}
			if err == nil && response != nil && response.ContentLength == int64(len(data)) {
				t.Fatal("failed output appeared complete")
			}
		})
	}
}

type credentialWireHeldService struct {
	sc.HumanWriteCommands
	entered chan struct{}
	release chan struct{}
}

func (s *credentialWireHeldService) Metadata(ctx context.Context, _ id.Actor, _ sc.CredentialRef) (sc.Metadata, error) {
	close(s.entered)
	<-s.release
	return sc.Metadata{}, ctx.Err()
}
func TestMCPCredentialServeHTTPWaitsForOriginalServiceTail(t *testing.T) {
	h, _ := credentialProbe(t)
	service := &credentialWireHeldService{entered: make(chan struct{}), release: make(chan struct{})}
	h.writes = service
	var once sync.Once
	release := func() { once.Do(func() { close(service.release) }) }
	defer release()
	returned := make(chan bool, 1)
	cancelled := make(chan struct{})
	var closes atomic.Int32
	server := httptest.NewServer(credentialWireServe(returned, func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithCancel(r.Context())
		defer cancel()
		r = r.WithContext(ctx)
		r.Body = credentialWireBody{ReadCloser: r.Body, closed: &closes}
		stop := context.AfterFunc(ctx, func() { close(cancelled) })
		defer stop()
		go func() {
			select {
			case <-service.entered:
				cancel()
			case <-ctx.Done():
			}
		}()
		h.ServeHTTP(w, r)
	}))
	defer func() { release(); server.Close() }()
	client := server.Client()
	client.Timeout = 2 * time.Second
	defer client.CloseIdleConnections()
	requestDone := make(chan struct{})
	go func() {
		defer close(requestDone)
		response, _ := client.Get(server.URL + credentialCollectionPath + "/" + credentialID)
		if response != nil {
			_, _ = io.Copy(io.Discard, response.Body)
			_ = response.Body.Close()
		}
	}()
	select {
	case <-cancelled:
	case <-time.After(time.Second):
		t.Fatal("credential cancellation not reached")
	}
	select {
	case <-returned:
		t.Fatal("cancellation replaced original service join")
	default:
	}
	release()
	if !credentialWireTerminal(t, returned) || closes.Load() != 1 {
		t.Fatal("credential service tail did not abort and close")
	}
	select {
	case <-requestDone:
	case <-time.After(time.Second):
		t.Fatal("credential client did not join")
	}
}
