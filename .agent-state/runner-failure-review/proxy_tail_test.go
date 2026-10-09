// Independent fixed-Go dependency control for Runner C's handler counters.
// This uses no listener, socket, process, or database. Run from the Work tree:
// GOTOOLCHAIN=local GOENV=off GOWORK=off GOPROXY=off GOSUMDB=off GOTELEMETRY=off \
// GOCACHE=/workspace/agenteam-work-ui/output/ai/work-owner-planning-ui/implementation/gocache \
// /workspace/toolchains/go1.27.1/bin/go test -race -count=1 -timeout=10s -v \
// .agent-state/runner-failure-review/proxy_tail_test.go
package failure_test

import (
	"bufio"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type memoryConn struct {
	closed, readReturned chan struct{}
	once                 sync.Once
}

func (c *memoryConn) Read([]byte) (int, error) {
	<-c.closed
	close(c.readReturned)
	return 0, net.ErrClosed
}
func (*memoryConn) Write(p []byte) (int, error)      { return len(p), nil }
func (c *memoryConn) Close() error                   { c.once.Do(func() { close(c.closed) }); return nil }
func (*memoryConn) LocalAddr() net.Addr              { return nil }
func (*memoryConn) RemoteAddr() net.Addr             { return nil }
func (*memoryConn) SetDeadline(time.Time) error      { return nil }
func (*memoryConn) SetReadDeadline(time.Time) error  { return nil }
func (*memoryConn) SetWriteDeadline(time.Time) error { return nil }

type heldBackend struct {
	closeEntered, closeRelease, closeReturned chan struct{}
}

func (*heldBackend) Read([]byte) (int, error) {
	return 0, errors.New("controlled upstream read failure")
}
func (*heldBackend) Write(p []byte) (int, error) { return len(p), nil }
func (b *heldBackend) Close() error {
	close(b.closeEntered)
	<-b.closeRelease
	close(b.closeReturned)
	return nil
}

type upgradeWriter struct {
	conn   *memoryConn
	header http.Header
}

func (w *upgradeWriter) Header() http.Header       { return w.header }
func (*upgradeWriter) WriteHeader(int)             {}
func (*upgradeWriter) Write(p []byte) (int, error) { return len(p), nil }
func (w *upgradeWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return w.conn, bufio.NewReadWriter(bufio.NewReader(w.conn), bufio.NewWriter(w.conn)), nil
}

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func await(t *testing.T, ch <-chan struct{}, label string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(time.Second):
		t.Fatal(label)
	}
}

// A successful control demonstrates a missing fixture witness, not a failure
// in the standard library: ReverseProxy does not promise to join backend Close.
func TestOriginalReverseProxyHandlerReturnDoesNotJoinUpgradeBody(t *testing.T) {
	back := &heldBackend{make(chan struct{}), make(chan struct{}), make(chan struct{})}
	client := &memoryConn{closed: make(chan struct{}), readReturned: make(chan struct{})}
	var release sync.Once
	defer release.Do(func() { close(back.closeRelease) })
	defer client.Close()
	target, _ := url.Parse("http://owned.invalid")
	proxy := &httputil.ReverseProxy{
		Rewrite: func(r *httputil.ProxyRequest) { r.SetURL(target) },
		Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: 101, Header: http.Header{"Connection": {"Upgrade"}, "Upgrade": {"websocket"}}, Body: back, Request: r}, nil
		}),
		ErrorLog: log.New(io.Discard, "", 0),
	}
	request, _ := http.NewRequest("GET", "http://owned.invalid/api/v1/runner/control", nil)
	request.Header.Set("Connection", "Upgrade")
	request.Header.Set("Upgrade", "websocket")
	var requests, wss atomic.Int32
	handlerDone := make(chan struct{})
	go func() {
		defer close(handlerDone)
		requests.Add(1)
		defer requests.Add(-1)
		wss.Add(1)
		defer wss.Add(-1)
		proxy.ServeHTTP(&upgradeWriter{client, make(http.Header)}, request)
	}()
	await(t, handlerDone, "original ReverseProxy handler did not return")
	await(t, back.closeEntered, "original async backend Close did not enter")
	await(t, client.readReturned, "original downstream reader did not return")
	if requests.Load() != 0 || wss.Load() != 0 {
		t.Fatal("fixture's original counters not zero")
	}
	select {
	case <-back.closeReturned:
		t.Fatal("controlled backend Close was not actually held")
	default:
	}
	t.Log("original request/control counters are zero while the original upgrade body Close has not returned")
	release.Do(func() { close(back.closeRelease) })
	await(t, back.closeReturned, "original backend Close did not actually return after release")
}
