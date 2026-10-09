//go:build integration

package runnercontrol_test

import (
	"context"
	"crypto/ed25519"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	rc "github.com/LunaDeerTech/agenteam/internal/central/runner/contract"
	"github.com/LunaDeerTech/agenteam/internal/runner/control"
	"github.com/LunaDeerTech/agenteam/internal/runner/identity"
	p "github.com/LunaDeerTech/agenteam/internal/runnerprotocol"
	"github.com/gorilla/websocket"
)

var errNativeRecoveryCut = errors.New("owned native response cut")

// The proxy forwards the original requests to the actual TLS handler. Its
// only fault is closing one response's real frontend connection: it supplies
// no successful enrollment, authentication, frame or persistent fact itself.
type nativeRecoveryProxy struct {
	server                                                           *httptest.Server
	roots                                                            *x509.CertPool
	handlers                                                         atomic.Int64
	mu                                                               sync.Mutex
	enrolls, challenges, controls, cuts                              int
	backendComplete, frontendClosed, pendingEnroll, pendingChallenge bool
	failed                                                           bool
	public                                                           [32]byte
}

func newNativeRecoveryProxy(t *testing.T, backend *runnerNativeServer, cutPath, identityPath string) *nativeRecoveryProxy {
	t.Helper()
	upstream, err := url.Parse(backend.origin)
	requireServiceOK(t, err, "recovery original TLS address")
	v := &nativeRecoveryProxy{}
	transport := backend.transport.Clone()
	proxy := httputil.NewSingleHostReverseProxy(upstream)
	proxy.Transport, proxy.ErrorLog = transport, log.New(io.Discard, "", 0)
	proxy.ModifyResponse = func(response *http.Response) error {
		if response.Request.URL.Path != cutPath {
			return nil
		}
		v.mu.Lock()
		defer v.mu.Unlock()
		if v.cuts != 0 {
			v.failed = true
			return errNativeRecoveryCut
		}
		v.cuts++
		if cutPath == "/api/v1/runner/enroll" {
			raw, readErr := io.ReadAll(io.LimitReader(response.Body, p.MaxDeviceBodyBytes+1))
			closeErr := response.Body.Close()
			response.Body = http.NoBody // ReverseProxy must not close the original twice.
			defer clear(raw)
			known, decodeErr := p.DecodeEnrollmentResponse(raw)
			v.backendComplete = response.StatusCode == http.StatusOK && readErr == nil && closeErr == nil && int64(len(raw)) == response.ContentLength && decodeErr == nil && known.PublicKeyFingerprint() == p.PublicKeyFingerprint(v.public)
		} else {
			// An upstream 101 is a real committed reservation. Drop its frontend
			// handshake rather than claiming that a backend Upgrade failed.
			closeErr := response.Body.Close()
			v.backendComplete = response.StatusCode == http.StatusSwitchingProtocols && closeErr == nil
			response.Body = http.NoBody
		}
		if !v.backendComplete {
			v.failed = true
		}
		return errNativeRecoveryCut
	}
	proxy.ErrorHandler = func(w http.ResponseWriter, _ *http.Request, err error) {
		if !errors.Is(err, errNativeRecoveryCut) {
			v.mu.Lock()
			v.failed = true
			v.mu.Unlock()
			http.Error(w, "unavailable", http.StatusBadGateway)
			return
		}
		conn, _, hijackErr := http.NewResponseController(w).Hijack()
		closed := false
		if hijackErr == nil {
			closed = conn.Close() == nil
		}
		v.mu.Lock()
		v.frontendClosed = closed
		v.failed = v.failed || !closed
		v.mu.Unlock()
	}
	v.server = httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		v.handlers.Add(1)
		defer v.handlers.Add(-1)
		v.mu.Lock()
		switch request.URL.Path {
		case "/api/v1/runner/enroll":
			v.enrolls++
		case "/api/v1/runner/challenge":
			v.challenges++
		case "/api/v1/runner/control":
			v.controls++
		}
		if identityPath != "" && (request.URL.Path == "/api/v1/runner/enroll" || request.URL.Path == "/api/v1/runner/challenge") {
			stored, readErr := identity.ReadOnly(identityPath)
			public, keyErr := stored.PublicKey()
			pending := readErr == nil && keyErr == nil && stored.State() == identity.Pending
			if request.URL.Path == "/api/v1/runner/enroll" {
				v.pendingEnroll, v.public = pending, public
			} else {
				v.pendingChallenge = pending && public == v.public
			}
		}
		v.mu.Unlock()
		proxy.ServeHTTP(w, request)
	}))
	// Keep actual hijacked connections in the same native connection tracker
	// used by the integration listener; httptest's HTTP map omits hijacks.
	owner := &runnerNativeServer{conns: make(map[*runnerNativeConn]struct{})}
	v.server.Listener = runnerNativeListener{v.server.Listener, owner}
	v.server.Config.ErrorLog = log.New(io.Discard, "", 0)
	v.server.StartTLS()
	v.roots = x509.NewCertPool()
	v.roots.AddCert(v.server.Certificate())
	t.Cleanup(func() {
		_ = v.server.Listener.Close()
		v.waitIdle(t)
		transport.CloseIdleConnections()
		owner.mu.Lock()
		connections := make([]*runnerNativeConn, 0, len(owner.conns))
		for conn := range owner.conns {
			connections = append(connections, conn)
		}
		owner.mu.Unlock()
		for _, conn := range connections {
			_ = conn.Close()
		}
		v.server.Close() // joins the original test HTTP Serve owner.
		owner.mu.Lock()
		remaining := len(owner.conns)
		owner.mu.Unlock()
		if remaining != 0 || v.handlers.Load() != 0 {
			t.Error("native recovery proxy retained an original connection or handler")
		}
	})
	return v
}

func (v *nativeRecoveryProxy) waitIdle(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for v.handlers.Load() != 0 {
		select {
		case <-ctx.Done():
			t.Error("native recovery proxy's original handler has not returned")
			return
		case <-ticker.C:
		}
	}
}

// One finite identity/recovery group: original nonce ownership across a lost
// native upgrade, signed clock rejection, and a lost enrollment response. It
// does not claim an ambiguous database COMMIT was actually produced.
func TestRunnerControlNativeIdentityRecovery(t *testing.T) {
	t.Run("lost client upgrade consumes nonce and invalid clocks do not", func(t *testing.T) {
		v := newRunnerServiceFixture(t)
		server := newRunnerNativeServer(t, v.runner)
		_, _, created := v.create(t, "native upgrade recovery")
		target := created.Receipt.Runner.ID
		key, _ := v.enroll(t, created)
		auth := server.authentication(t, target, key)
		proxy := newNativeRecoveryProxy(t, server, "/api/v1/runner/control", "")
		header, err := p.EncodeAuthorization(auth)
		requireServiceOK(t, err, "original recovery authorization")
		dialer := websocket.Dialer{Proxy: nil, TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: proxy.roots}, HandshakeTimeout: 3 * time.Second, Subprotocols: []string{p.Subprotocol}}
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		socket, response, err := dialer.DialContext(ctx, "wss"+strings.TrimPrefix(proxy.server.URL, "https")+"/api/v1/runner/control", http.Header{"Authorization": {header}})
		cancel()
		if socket != nil {
			_ = socket.Close()
		}
		if response != nil {
			_ = response.Body.Close()
		}
		if err == nil || socket != nil || response != nil {
			t.Fatal("lost native frontend handshake was incorrectly accepted")
		}
		proxy.waitIdle(t)
		server.waitControls(t, 0)
		proxy.mu.Lock()
		cut := proxy.cuts == 1 && proxy.controls == 1 && proxy.backendComplete && proxy.frontendClosed && !proxy.failed
		proxy.mu.Unlock()
		if !cut {
			t.Fatal("original committed upgrade was not actually cut and retired")
		}
		hash, err := auth.Nonce().Digest()
		requireServiceOK(t, err, "original nonce digest")
		var consumed bool
		err = v.store.QueryRow(migrationContext(t), `SELECT consumed_at IS NOT NULL FROM agenteam_runner.challenges WHERE nonce_hash=$1 AND runner_id=$2`, hash[:], target.String()).Scan(&consumed)
		requireServiceOK(t, err, "lost upgrade durable nonce")
		if !consumed {
			t.Fatal("lost handshake restored the original nonce")
		}
		server.dial(t, auth, false, http.StatusUnauthorized)
		fresh := server.authentication(t, target, key)
		for _, offset := range []int64{-60, 60} {
			stamp := p.NewUnixSeconds(time.Now().Unix() + offset)
			raw, err := p.SigningBytes(fresh.RunnerID(), fresh.Nonce(), stamp)
			requireServiceOK(t, err, "actual out-of-window signature input")
			invalid, err := p.NewAuthentication(fresh.RunnerID(), fresh.Nonce(), stamp, ed25519.Sign(key, raw))
			requireServiceOK(t, err, "actual signed clock rejection")
			server.dial(t, invalid, false, http.StatusUnauthorized)
		}
		successor := server.dial(t, fresh, false, http.StatusSwitchingProtocols)
		nativeRunnerHello(t, successor, target)
		v.snapshot(t, target, rc.Online)
		requireServiceOK(t, successor.Close(), "original successful successor close")
		server.waitControls(t, 0)
		// Expire the original challenge naturally, then sign with current DB
		// time so a stale timestamp cannot substitute for the expiry rejection.
		expiring := server.authentication(t, target, key)
		expiringHash, err := expiring.Nonce().Digest()
		requireServiceOK(t, err, "expiring original nonce digest")
		var expires time.Time
		err = v.store.QueryRow(migrationContext(t), `SELECT expires_at FROM agenteam_runner.challenges WHERE nonce_hash=$1 AND runner_id=$2`, expiringHash[:], target.String()).Scan(&expires)
		requireServiceOK(t, err, "original challenge deadline")
		timer := time.NewTimer(time.Until(expires.Add(100 * time.Millisecond)))
		defer timer.Stop()
		<-timer.C
		var now time.Time
		var expired bool
		err = v.store.QueryRow(migrationContext(t), `SELECT clock_timestamp(),clock_timestamp()>=expires_at AND consumed_at IS NULL FROM agenteam_runner.challenges WHERE nonce_hash=$1 AND runner_id=$2`, expiringHash[:], target.String()).Scan(&now, &expired)
		requireServiceOK(t, err, "actual database expiry of original unused nonce")
		if !expired {
			t.Fatal("original nonce expiry was not observed in the database")
		}
		stamp := p.NewUnixSeconds(now.Unix())
		raw, err := p.SigningBytes(expiring.RunnerID(), expiring.Nonce(), stamp)
		requireServiceOK(t, err, "current-time expired nonce signature input")
		expiredAuth, err := p.NewAuthentication(expiring.RunnerID(), expiring.Nonce(), stamp, ed25519.Sign(key, raw))
		requireServiceOK(t, err, "current-time expired nonce authentication")
		server.dial(t, expiredAuth, false, http.StatusUnauthorized)
		var generation, current int
		err = v.store.QueryRow(migrationContext(t), `SELECT connection_generation,(SELECT count(*) FROM agenteam_runner.connections WHERE runner_id=$1) FROM agenteam_runner.runners WHERE id=$1`, target.String()).Scan(&generation, &current)
		requireServiceOK(t, err, "recovery generation and retirement")
		if generation != 2 || current != 0 {
			t.Fatal("clock rejection consumed a reservation or old cleanup retained a connection")
		}
	})
	if t.Failed() {
		return
	}
	t.Run("lost enrollment response recovers with the original pending key", func(t *testing.T) {
		v := newRunnerServiceFixture(t)
		server := newRunnerNativeServer(t, v.runner)
		_, _, created := v.create(t, "native pending key recovery")
		target, path := created.Receipt.Runner.ID, nativeIdentityPath(t)
		proxy := newNativeRecoveryProxy(t, server, "/api/v1/runner/enroll", path)
		configuration := identity.Configuration{CentralURL: proxy.server.URL, RunnerID: p.ID(target.String()), RootPath: created.Receipt.Runner.RootPath}
		states := make(chan control.ConnectionState, 32)
		client := startNativeClient(t, control.Options{IdentityFile: path, Configuration: &configuration, EnrollmentToken: created.Material.Token, RunnerVersion: "native-recovery", Roots: proxy.roots, Observe: func(ctx context.Context, state control.ConnectionState) {
			select {
			case states <- state:
			case <-ctx.Done():
			}
		}}, nil)
		client.connected(t, states)
		stored := nativeStoredIdentity(t, path, configuration)
		public, err := stored.PublicKey()
		requireServiceOK(t, err, "recovered actual public key")
		proxy.mu.Lock()
		recovered := proxy.enrolls == 1 && proxy.challenges == 1 && proxy.controls == 1 && proxy.cuts == 1 && proxy.backendComplete && proxy.frontendClosed && proxy.pendingEnroll && proxy.pendingChallenge && proxy.public == public && !proxy.failed
		proxy.mu.Unlock()
		if !recovered {
			t.Fatal("lost enrollment did not recover through the one original pending key and fresh challenge")
		}
		snapshot := v.snapshot(t, target, rc.Online)
		if snapshot.PublicKeyFingerprint == nil || string(*snapshot.PublicKeyFingerprint) != p.PublicKeyFingerprint(public) {
			t.Fatal("recovered public identity does not match the original enrollment")
		}
		v.nativeIdentityFacts(t, target)
		requireNativeIdentityLocked(t, path)
		client.stop(t)
		proxy.waitIdle(t)
		server.waitControls(t, 0)
		v.snapshot(t, target, rc.Offline)
		file, err := identity.Open(path)
		requireServiceOK(t, err, "recovered identity released after actual Run")
		requireServiceOK(t, file.Close(), "recovered identity observer close")
	})
}
