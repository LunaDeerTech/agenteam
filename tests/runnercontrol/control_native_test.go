//go:build integration

package runnercontrol_test

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"io"
	"log"
	"math/big"
	"net"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/httpapi"
	rc "github.com/LunaDeerTech/agenteam/internal/central/runner/contract"
	runnerhttp "github.com/LunaDeerTech/agenteam/internal/central/runner/http"
	"github.com/LunaDeerTech/agenteam/internal/central/runner/service"
	p "github.com/LunaDeerTech/agenteam/internal/runnerprotocol"
	"github.com/gorilla/websocket"
)

// This fixture owns a real TLS listener and records its actual Serve return,
// every accepted connection (including hijacks), and every handler return.
// HTTP Shutdown alone is not evidence that the Runner WSS owner has joined.
type runnerNativeServer struct {
	service     *service.Service
	server      *http.Server
	listener    net.Listener
	served      chan error
	origin      string
	security    *tls.Config
	transport   *http.Transport
	http        *http.Client
	handlers    sync.WaitGroup
	controls    atomic.Int64
	mu          sync.Mutex
	conns       map[*runnerNativeConn]struct{}
	connections *runnerNativeConnections
}

type runnerNativeListener struct {
	net.Listener
	owner *runnerNativeServer
}

func (l runnerNativeListener) Accept() (net.Conn, error) {
	c, e := l.Listener.Accept()
	if e != nil {
		return nil, e
	}
	return l.owner.nativeConnections().track(c), nil
}

// The response-loss fixture also owns this same accepted-connection map. Keep
// its existing fields and point the close observer at that map, never a copy.
func (v *runnerNativeServer) nativeConnections() *runnerNativeConnections {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.connections == nil {
		v.connections = &runnerNativeConnections{mu: &v.mu, conns: v.conns, changed: make(chan struct{})}
	}
	return v.connections
}

func newRunnerNativeServer(t *testing.T, service *service.Service) *runnerNativeServer {
	t.Helper()
	handler, e := runnerhttp.NewDeviceHandler(service)
	requireServiceOK(t, e, "native device handler")
	public, private, e := ed25519.GenerateKey(rand.Reader)
	requireServiceOK(t, e, "owned TLS key")
	t.Cleanup(func() { clear(private) })
	now := time.Now()
	template := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "owned Runner native fixture"}, NotBefore: now.Add(-time.Minute), NotAfter: now.Add(time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}}
	der, e := x509.CreateCertificate(rand.Reader, template, template, public, private)
	requireServiceOK(t, e, "owned TLS certificate")
	cert, e := x509.ParseCertificate(der)
	requireServiceOK(t, e, "owned TLS certificate parse")
	roots := x509.NewCertPool()
	roots.AddCert(cert)
	listener, e := net.Listen("tcp4", "127.0.0.1:0")
	requireServiceOK(t, e, "owned native listener")
	v := &runnerNativeServer{service: service, listener: listener, served: make(chan error, 1), origin: "https://" + listener.Addr().String(), security: &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: roots}, conns: make(map[*runnerNativeConn]struct{})}
	v.nativeConnections()
	v.transport = &http.Transport{Proxy: nil, TLSClientConfig: v.security.Clone(), DisableCompression: true, TLSHandshakeTimeout: 3 * time.Second, ResponseHeaderTimeout: 3 * time.Second, MaxConnsPerHost: 4}
	v.http = &http.Client{Transport: v.transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	v.server = &http.Server{ConnState: v.connections.observeState, ErrorLog: log.New(io.Discard, "", 0), Handler: httpapi.WithRequestID(nil, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		v.handlers.Add(1)
		defer v.handlers.Done()
		if r.URL.Path == "/api/v1/runner/control" {
			v.controls.Add(1)
			defer v.controls.Add(-1)
		}
		handler.ServeHTTP(w, r)
	}))}
	secure := tls.NewListener(runnerNativeListener{listener, v}, &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: private}}})
	go func() { v.served <- v.server.Serve(secure) }()
	t.Cleanup(func() {
		v.service.Stop()
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		if e := v.service.Drain(ctx); e != nil {
			t.Error("native Runner owner did not join before HTTP cleanup")
		}
		cancel()
		v.transport.CloseIdleConnections()
		shutdown, finishShutdown := context.WithTimeout(context.Background(), 3*time.Second)
		defer finishShutdown()
		if e := v.server.Shutdown(shutdown); e != nil {
			t.Error("native HTTP callbacks did not join before cleanup")
		}
		_ = v.server.Close()
		_ = v.listener.Close()
		select {
		case e := <-v.served:
			if !errors.Is(e, http.ErrServerClosed) {
				t.Error("owned native Serve returned an unexpected error")
			}
		case <-time.After(3 * time.Second):
			t.Error("owned native Serve return is missing")
		}
		if e := v.connections.wait(shutdown); e != nil {
			t.Error("native physical connections did not join within original HTTP shutdown context")
		}
		left, states := v.connections.remainder()
		// Failure cleanup only: it cannot turn a missing owner join into PASS.
		if len(left) != 0 {
			t.Errorf("native owner left %d connections after actual Drain", len(left))
			for _, state := range states {
				t.Errorf("native connection remainder state=%s close_inflight=%d", state.state, state.inflight)
			}
			for _, c := range left {
				_ = c.Close()
			}
		}
		joined := make(chan struct{})
		go func() { v.handlers.Wait(); close(joined) }()
		select {
		case <-joined:
		case <-time.After(3 * time.Second):
			t.Error("owned native handlers have not actually returned")
		}
		if v.controls.Load() != 0 {
			t.Error("a hijacked control handler still owns work")
		}
	})
	return v
}

func (v *runnerNativeServer) exchange(t *testing.T, path string, raw []byte, status int) []byte {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	r, e := http.NewRequestWithContext(ctx, http.MethodPost, v.origin+path, bytes.NewReader(raw))
	requireServiceOK(t, e, "native request construction")
	r.Header.Set("Content-Type", "application/json")
	response, e := v.http.Do(r)
	requireServiceOK(t, e, "native HTTPS exchange")
	body, readErr := io.ReadAll(io.LimitReader(response.Body, p.MaxDeviceBodyBytes+1))
	closeErr := response.Body.Close()
	if readErr != nil || closeErr != nil || ctx.Err() != nil || len(body) > p.MaxDeviceBodyBytes || response.StatusCode != status || response.ContentLength != int64(len(body)) || response.Header.Get("Cache-Control") != "no-store" {
		clear(body)
		t.Fatalf("native HTTP response rejected: status=%d expected=%d read_error=%t close_error=%t", response.StatusCode, status, readErr != nil, closeErr != nil)
	}
	return body
}

func (v *runnerNativeServer) authentication(t *testing.T, target rc.RunnerID, key ed25519.PrivateKey) p.Authentication {
	t.Helper()
	in, e := p.NewChallengeRequest(p.ID(target.String()))
	requireServiceOK(t, e, "native challenge input")
	raw, e := p.EncodeChallengeRequest(in)
	requireServiceOK(t, e, "native challenge encoding")
	body := v.exchange(t, "/api/v1/runner/challenge", raw, http.StatusOK)
	defer clear(body)
	challenge, e := p.DecodeChallengeResponse(body)
	requireServiceOK(t, e, "native challenge decoding")
	stamp := p.NewUnixSeconds(time.Now().Unix())
	message, e := p.SigningBytes(in.RunnerID(), challenge.Nonce(), stamp)
	requireServiceOK(t, e, "native signature input")
	auth, e := p.NewAuthentication(in.RunnerID(), challenge.Nonce(), stamp, ed25519.Sign(key, message))
	requireServiceOK(t, e, "native signed authentication")
	return auth
}

func (v *runnerNativeServer) dial(t *testing.T, auth p.Authentication, origin bool, want int) *websocket.Conn {
	t.Helper()
	header, e := p.EncodeAuthorization(auth)
	requireServiceOK(t, e, "native authorization encoding")
	headers := http.Header{"Authorization": []string{header}}
	if origin {
		headers.Set("Origin", v.origin)
	}
	dialer := websocket.Dialer{Proxy: nil, TLSClientConfig: v.security.Clone(), HandshakeTimeout: 5 * time.Second, Subprotocols: []string{p.Subprotocol}}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	socket, response, e := dialer.DialContext(ctx, "wss"+strings.TrimPrefix(v.origin, "https")+"/api/v1/runner/control", headers)
	status := 0
	if response != nil {
		status = response.StatusCode
	}
	if want != http.StatusSwitchingProtocols {
		if socket != nil {
			_ = socket.Close()
		}
		if response != nil {
			_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, p.MaxDeviceBodyBytes+1))
			_ = response.Body.Close()
		}
		if socket != nil || e == nil || status != want {
			t.Fatalf("native rejected upgrade mismatch: status=%d expected=%d error_present=%t", status, want, e != nil)
		}
		return nil
	}
	if e != nil || socket == nil || status != want {
		if response != nil {
			_ = response.Body.Close()
		}
		t.Fatalf("native upgrade failed: status=%d error_present=%t", status, e != nil)
	}
	t.Cleanup(func() { _ = socket.Close() })
	if socket.Subprotocol() != p.Subprotocol {
		t.Fatal("native upgrade negotiated an unexpected protocol")
	}
	return socket
}

func nativeRunnerSend(t *testing.T, socket *websocket.Conn, payload p.Payload) {
	t.Helper()
	id, e := p.NewID()
	requireServiceOK(t, e, "native frame identity")
	message, e := p.NewMessage(p.Header{ProtocolVersion: p.CurrentVersion(), MessageID: id, Timestamp: p.Instant(time.Now().UTC().Format(time.RFC3339Nano))}, payload)
	requireServiceOK(t, e, "native frame")
	raw, e := p.Encode(message)
	requireServiceOK(t, e, "native frame encoding")
	requireServiceOK(t, socket.SetWriteDeadline(time.Now().Add(3*time.Second)), "native write deadline")
	requireServiceOK(t, socket.WriteMessage(websocket.TextMessage, raw), "native frame write")
}

func nativeRunnerReceive(t *testing.T, socket *websocket.Conn) p.Message {
	t.Helper()
	requireServiceOK(t, socket.SetReadDeadline(time.Now().Add(3*time.Second)), "native read deadline")
	kind, raw, e := socket.ReadMessage()
	requireServiceOK(t, e, "native frame read")
	if kind != websocket.TextMessage {
		t.Fatal("native response was not text")
	}
	message, e := p.Decode(raw)
	requireServiceOK(t, e, "native response decoding")
	return message
}

func nativeRunnerHello(t *testing.T, socket *websocket.Conn, target rc.RunnerID) {
	t.Helper()
	nativeRunnerSend(t, socket, p.Hello{RunnerID: p.ID(target.String()), RunnerVersion: "native-test", ProtocolVersion: p.CurrentVersion(), OS: "linux", Arch: "amd64", Headless: true, Capabilities: []string{}, FeatureFlags: []string{}})
	ack, ok := nativeRunnerReceive(t, socket).Payload().(p.HelloAck)
	if !ok || !ack.Accepted || ack.NegotiatedProtocolVersion != p.CurrentVersion() || ack.HeartbeatIntervalMS != 10000 || ack.HeartbeatTimeoutMS != 30000 || len(ack.EnabledFeatures) != 0 {
		t.Fatal("native hello acknowledgment does not match v1.0")
	}
}

func nativeRunnerClosed(t *testing.T, socket *websocket.Conn) {
	t.Helper()
	requireServiceOK(t, socket.SetReadDeadline(time.Now().Add(3*time.Second)), "native retirement deadline")
	_, _, e := socket.ReadMessage()
	var timeout net.Error
	if e == nil || errors.As(e, &timeout) && timeout.Timeout() {
		t.Fatal("native connection retirement was not observed before the bound")
	}
}

func (v *runnerNativeServer) waitControls(t *testing.T, want int64) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for v.controls.Load() != want {
		select {
		case <-ticker.C:
		case <-ctx.Done():
			t.Fatalf("native control callbacks still active: want=%d got=%d", want, v.controls.Load())
		}
	}
}

// Author integration only. This is real PG plus native TLS/WebSocket, and must
// run exclusively under a fresh resource grant. It is never a pure test.
func TestRunnerControlNativeGeneration(t *testing.T) {
	v := newRunnerServiceFixture(t)
	first := newRunnerNativeServer(t, v.runner)
	for _, boundary := range []string{"untrusted-ca", "wrong-hostname"} {
		t.Run(boundary, func(t *testing.T) {
			security := first.security.Clone()
			if boundary == "untrusted-ca" {
				security.RootCAs = x509.NewCertPool()
			} else {
				security.ServerName = "wrong.invalid"
			}
			transport := &http.Transport{Proxy: nil, TLSClientConfig: security, TLSHandshakeTimeout: 3 * time.Second}
			defer transport.CloseIdleConnections()
			client := &http.Client{Transport: transport}
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			r, e := http.NewRequestWithContext(ctx, http.MethodPost, first.origin+"/api/v1/runner/challenge", strings.NewReader(`{}`))
			requireServiceOK(t, e, "TLS rejection request")
			response, e := client.Do(r)
			if response != nil {
				_ = response.Body.Close()
			}
			var certificate *tls.CertificateVerificationError
			if response != nil || !errors.As(e, &certificate) {
				t.Fatal("native TLS did not reject the exact certificate boundary")
			}
		})
	}
	_, _, created := v.create(t, "native-generation")
	target := created.Receipt.Runner.ID
	public, private, e := ed25519.GenerateKey(rand.Reader)
	requireServiceOK(t, e, "native device key")
	t.Cleanup(func() { clear(private) })
	enrollment, e := p.NewEnrollmentRequest(p.ID(target.String()), created.Material.Token, [32]byte(public), created.Receipt.Runner.RootPath, "linux", "amd64")
	requireServiceOK(t, e, "native enrollment input")
	raw, e := p.EncodeEnrollmentRequest(enrollment)
	requireServiceOK(t, e, "native enrollment encoding")
	defer clear(raw)
	body := first.exchange(t, "/api/v1/runner/enroll", raw, http.StatusOK)
	registered, e := p.DecodeEnrollmentResponse(body)
	clear(body)
	requireServiceOK(t, e, "native known enrollment response")
	if registered.RunnerID() != p.ID(target.String()) || registered.Version() != "2" || registered.CredentialGeneration() != "1" || registered.PublicKeyFingerprint() != p.PublicKeyFingerprint([32]byte(public)) {
		t.Fatal("native enrollment did not preserve the original identity")
	}
	clear(first.exchange(t, "/api/v1/runner/enroll", raw, http.StatusUnauthorized))
	auth := first.authentication(t, target, private)
	first.dial(t, auth, true, http.StatusUnauthorized)
	one := first.dial(t, auth, false, http.StatusSwitchingProtocols)
	v.snapshot(t, target, rc.Offline)
	nativeRunnerHello(t, one, target)
	v.snapshot(t, target, rc.Online)
	first.dial(t, auth, false, http.StatusUnauthorized)
	nativeRunnerSend(t, one, p.Heartbeat{Sequence: "1", RunnerTime: p.Instant(time.Now().UTC().Format(time.RFC3339Nano))})
	if ack, ok := nativeRunnerReceive(t, one).Payload().(p.HeartbeatAck); !ok || ack.Sequence != "1" {
		t.Fatal("native heartbeat did not receive the same sequence")
	}

	// A genuinely different Service owner and listener win the next durable
	// generation. The old socket and its late DB cleanup must not retire it.
	otherService := v.newRunner(t)
	second := newRunnerNativeServer(t, otherService)
	two := second.dial(t, second.authentication(t, target, private), false, http.StatusSwitchingProtocols)
	v.snapshot(t, target, rc.Offline)
	nativeRunnerHello(t, two, target)
	nativeRunnerClosed(t, one)
	first.waitControls(t, 0)
	current := v.snapshot(t, target, rc.Online)
	if current.Version != 2 || current.CredentialGeneration != 1 {
		t.Fatal("control generations changed the business credential version")
	}
	nativeRunnerSend(t, two, p.Heartbeat{Sequence: "1", RunnerTime: p.Instant(time.Now().UTC().Format(time.RFC3339Nano))})
	if ack, ok := nativeRunnerReceive(t, two).Payload().(p.HeartbeatAck); !ok || ack.Sequence != "1" {
		t.Fatal("successor heartbeat did not remain usable after old retirement")
	}
	revoke, e := rc.NewRevoke(target, rc.CredentialRequest{ExpectedVersion: 2})
	requireServiceOK(t, e, "native revoke intent")
	changed, e := v.runner.Execute(migrationContext(t), v.actor, serviceKey(t), revoke)
	requireServiceOK(t, e, "native current credential revoke")
	if !changed.Receipt.Changed || changed.Receipt.Runner.Version != 3 || changed.Receipt.Runner.CredentialGeneration != 2 {
		t.Fatal("native revoke did not advance the actual credential")
	}
	nativeRunnerClosed(t, two)
	second.waitControls(t, 0)
	v.snapshot(t, target, rc.Offline)
}
