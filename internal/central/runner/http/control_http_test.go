package runnerhttp

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/httpapi"
	"github.com/LunaDeerTech/agenteam/internal/central/runner/service"
	p "github.com/LunaDeerTech/agenteam/internal/runnerprotocol"
)

func pureControlUpgradeRequest(t *testing.T) *http.Request {
	t.Helper()
	runner, _ := p.NewID()
	nonce, _ := p.NewNonce()
	auth, e := p.NewAuthentication(runner, nonce, p.NewUnixSeconds(time.Now().Unix()), make([]byte, 64))
	if e != nil {
		t.Fatal(e)
	}
	header, _ := p.EncodeAuthorization(auth)
	r := httptest.NewRequest("GET", controlPath, nil)
	r.Header = http.Header{
		"Connection": {"keep-alive, Upgrade"}, "Upgrade": {"websocket"}, "Sec-Websocket-Version": {"13"},
		"Sec-Websocket-Protocol": {p.Subprotocol}, "Sec-Websocket-Key": {"MDEyMzQ1Njc4OWFiY2RlZg=="}, "Authorization": {header},
	}
	var request *http.Request
	httpapi.WithRequestID(nil, http.HandlerFunc(func(_ http.ResponseWriter, original *http.Request) { request = original })).ServeHTTP(httptest.NewRecorder(), r)
	return request
}

func TestRunnerControlHTTPPureStrictUpgradeIdentity(t *testing.T) {
	original := pureControlUpgradeRequest(t)
	if auth, e := controlAuthentication(original); e != nil || !auth.Valid() {
		t.Fatal("canonical signed shape rejected before real signature authority", e)
	}
	for _, mode := range []string{"cookie", "origin", "csrf", "authorization-duplicate", "protocol-list", "protocol-duplicate", "extensions", "empty-extensions", "query", "force-query", "raw-path", "wrong-path", "method", "http2", "key-alias", "version", "body", "unknown-body", "encoding", "connection-duplicate"} {
		t.Run(mode, func(t *testing.T) {
			r := pureControlUpgradeRequest(t)
			switch mode {
			case "cookie":
				r.Header["cookie"] = []string{""}
			case "origin":
				r.Header["Origin"] = []string{"https://central.example"}
			case "csrf":
				r.Header["X-CSRF-Token"] = []string{""}
			case "authorization-duplicate":
				r.Header["authorization"] = []string{r.Header.Get("Authorization")}
			case "protocol-list":
				r.Header.Set("Sec-WebSocket-Protocol", p.Subprotocol+", other")
			case "protocol-duplicate":
				r.Header["sec-websocket-protocol"] = []string{p.Subprotocol}
			case "extensions":
				r.Header.Set("Sec-WebSocket-Extensions", "permessage-deflate")
			case "empty-extensions":
				r.Header["sec-websocket-extensions"] = []string{""}
			case "query":
				r.URL.RawQuery = "nonce=canary"
			case "force-query":
				r.URL.ForceQuery = true
			case "raw-path":
				r.URL.RawPath = "/api/v1/runner/%63ontrol"
			case "wrong-path":
				r.URL.Path += "/"
			case "method":
				r.Method = "POST"
			case "http2":
				r.ProtoMajor = 2
			case "key-alias":
				r.Header.Set("Sec-WebSocket-Key", "MDEyMzQ1Njc4OWFiY2RlZg")
			case "version":
				r.Header.Add("Sec-WebSocket-Version", "13")
			case "body":
				r.ContentLength = 1
				r.Body = io.NopCloser(strings.NewReader("x"))
			case "unknown-body":
				r.Body = io.NopCloser(strings.NewReader("x"))
			case "encoding":
				r.Header["Content-Encoding"] = []string{""}
			case "connection-duplicate":
				r.Header.Set("Connection", "Upgrade, upgrade")
			}
			if _, e := controlAuthentication(r); e == nil {
				t.Fatal("ambiguous/browser/unsupported handshake accepted")
			}
		})
	}
}

// This is a syscall-free deadline recorder, not net.Pipe or a listener.
type deadlineOnlyConn struct{ read, write time.Time }

func (*deadlineOnlyConn) Read([]byte) (int, error)             { return 0, io.EOF }
func (*deadlineOnlyConn) Write(b []byte) (int, error)          { return len(b), nil }
func (*deadlineOnlyConn) Close() error                         { return nil }
func (*deadlineOnlyConn) LocalAddr() net.Addr                  { return nil }
func (*deadlineOnlyConn) RemoteAddr() net.Addr                 { return nil }
func (c *deadlineOnlyConn) SetDeadline(d time.Time) error      { c.read, c.write = d, d; return nil }
func (c *deadlineOnlyConn) SetReadDeadline(d time.Time) error  { c.read = d; return nil }
func (c *deadlineOnlyConn) SetWriteDeadline(d time.Time) error { c.write = d; return nil }

func TestRunnerControlHTTPPureHandshakeDeadlineHandoff(t *testing.T) {
	deadline := time.Now().Add(time.Second)
	ctx, cancel := context.WithDeadline(context.Background(), deadline)
	defer cancel()
	native := &deadlineOnlyConn{}
	conn := &handshakeConn{Conn: native, ctx: ctx, deadline: deadline}
	if e := conn.SetDeadline(time.Time{}); e != nil || !native.read.Equal(deadline) || !native.write.Equal(deadline) {
		t.Fatal("gorilla zero deadline escaped original handshake")
	}
	earlier := deadline.Add(-500 * time.Millisecond)
	_ = conn.SetWriteDeadline(earlier)
	if !native.write.Equal(earlier) {
		t.Fatal("earlier writer budget widened")
	}
	cancel()
	_ = conn.SetDeadline(time.Time{})
	if native.read.After(time.Now()) || conn.handoff() == nil {
		t.Fatal("cancel raced deadline reset into a live connection")
	}
	ctx2, cancel2 := context.WithDeadline(context.Background(), deadline)
	defer cancel2()
	conn = &handshakeConn{Conn: native, ctx: ctx2, deadline: deadline}
	if e := conn.handoff(); e != nil || !native.read.IsZero() || !native.write.IsZero() {
		t.Fatal("successful handoff retained finite HTTP budget", e)
	}
	cancel2()
	next := time.Now().Add(5 * time.Second)
	_ = conn.SetWriteDeadline(next)
	if !native.write.Equal(next) {
		t.Fatal("retired handshake context still owns session deadlines")
	}
}

func TestRunnerControlHTTPPureRegistryOldCleanup(t *testing.T) {
	id, _ := p.NewID()
	oldSocket, nextSocket := newPureSessionSocket(), newPureSessionSocket()
	old := newControlSession(newControlWire(oldSocket, pureControlGate), &pureSessionAuthority{}, service.Connection{}, id)
	next := newControlSession(newControlWire(nextSocket, pureControlGate), &pureSessionAuthority{}, service.Connection{}, id)
	registry := &controlRegistry{sessions: make(map[p.ID]*controlSession)}
	registry.install(old)
	registry.install(next)
	select {
	case <-oldSocket.closed:
	default:
		t.Fatal("replacement did not close old owned socket")
	}
	registry.remove(old)
	if registry.sessions[id] != next {
		t.Fatal("old retirement removed successor")
	}
	registry.remove(next)
	if len(registry.sessions) != 0 {
		t.Fatal("exact owner did not retire")
	}
	next.wire.stop()
}
