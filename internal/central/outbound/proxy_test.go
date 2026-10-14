package outbound

import (
	"bufio"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/binary"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/big"
	"net"
	"net/http"
	"net/http/httptrace"
	"net/netip"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

func proxyTestEnvironment(t *testing.T, values map[string]string) {
	t.Helper()
	for _, key := range []string{"HTTP_PROXY", "http_proxy", "HTTPS_PROXY", "https_proxy", "NO_PROXY", "no_proxy", "REQUEST_METHOD"} {
		t.Setenv(key, values[key])
	}
}

func proxyTestOrigin(t *testing.T, raw string) Origin {
	t.Helper()
	origin, err := ParseOrigin(raw)
	if err != nil {
		t.Fatal("test origin:", err)
	}
	return origin
}

func TestEnvironmentProxyRoutesSelection(t *testing.T) {
	const gateway = "http://gateway.example:8080"
	for _, tc := range []struct {
		name, target string
		env          map[string]string
		want         bool
	}{
		{"none", "https://api.example", nil, false},
		{"uppercase_https", "https://api.example", map[string]string{"HTTPS_PROXY": gateway}, true},
		{"lowercase_https", "https://api.example", map[string]string{"https_proxy": gateway}, true},
		{"http_does_not_select_https", "https://api.example", map[string]string{"HTTP_PROXY": gateway}, false},
		{"bare_gateway_authority", "https://api.example", map[string]string{"HTTPS_PROXY": "gateway.example:8080"}, true},
		{"no_proxy_exact", "https://api.example", map[string]string{"HTTPS_PROXY": gateway, "NO_PROXY": "api.example"}, false},
		{"no_proxy_lowercase", "https://api.example", map[string]string{"https_proxy": gateway, "no_proxy": "api.example"}, false},
		{"no_proxy_subdomain", "https://api.example", map[string]string{"HTTPS_PROXY": gateway, "NO_PROXY": ".example"}, false},
		{"no_proxy_exact_port", "https://api.example:8443", map[string]string{"HTTPS_PROXY": gateway, "NO_PROXY": "api.example:8443"}, false},
		{"no_proxy_different_port", "https://api.example:8443", map[string]string{"HTTPS_PROXY": gateway, "NO_PROXY": "api.example:443"}, true},
		{"no_proxy_hostname_boundary", "https://otherapi.example", map[string]string{"HTTPS_PROXY": gateway, "NO_PROXY": "api.example"}, true},
		{"no_proxy_wildcard", "https://api.example", map[string]string{"HTTPS_PROXY": gateway, "NO_PROXY": "*"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			proxyTestEnvironment(t, tc.env)
			origin := proxyTestOrigin(t, tc.target)
			routes, err := NewEnvironmentProxyRoutes([]Origin{origin})
			if err != nil {
				t.Fatal(err)
			}
			got, err := routes.UsesProxy(origin)
			if err != nil || got != tc.want {
				t.Fatalf("selection = %t, %v; want %t", got, err, tc.want)
			}
		})
	}
}

func TestEnvironmentProxyRoutesExactOriginAndSnapshot(t *testing.T) {
	proxyTestEnvironment(t, map[string]string{"HTTPS_PROXY": "http://gateway.example:8080"})
	origin := proxyTestOrigin(t, "https://API.Example./")
	allowed := []Origin{origin}
	routes, err := NewEnvironmentProxyRoutes(allowed)
	if err != nil {
		t.Fatal(err)
	}
	allowed[0] = proxyTestOrigin(t, "https://replacement.example")
	t.Setenv("HTTPS_PROXY", "")
	t.Setenv("NO_PROXY", "*")
	for _, raw := range []string{"https://api.example", "https://api.example:443"} {
		got, err := routes.UsesProxy(proxyTestOrigin(t, raw))
		if err != nil || !got {
			t.Fatalf("captured exact origin lost: %t %v", got, err)
		}
	}
	for _, raw := range []string{"https://sub.api.example", "https://api.example:8443", "https://replacement.example"} {
		got, err := routes.UsesProxy(proxyTestOrigin(t, raw))
		if err == nil || got {
			t.Fatal("unlisted origin selected the captured proxy")
		}
	}
	if got, err := routes.UsesProxy(Origin{}); err == nil || got {
		t.Fatal("invalid origin selected a route")
	}
	// Bypassed targets retain the ordinary direct policy rather than acquiring
	// authority from the deployment's proxy origin list.
	bypassed, err := NewEnvironmentProxyRoutes([]Origin{origin})
	if err != nil {
		t.Fatal(err)
	}
	if got, err := bypassed.UsesProxy(proxyTestOrigin(t, "https://unlisted.example")); err != nil || got {
		t.Fatal("NO_PROXY did not preserve the direct route")
	}
}

func TestEnvironmentProxyRoutesRejectInvalidConfiguration(t *testing.T) {
	origin := proxyTestOrigin(t, "https://api.example")
	for _, raw := range []string{
		"socks5://gateway.example:1080", "http://user:proxy-private-canary@gateway.example",
		"http://gateway.example/private-canary", "http://gateway.example?key=proxy-private-canary",
		"http://gateway.example#proxy-private-canary", "http://gateway.example:",
		"http://gateway.example:0", "http://gateway.example:65536", "http://[broken",
		"http://169.254.169.254", "http://metadata.google.internal",
	} {
		t.Run(strings.ReplaceAll(raw, "/", "_"), func(t *testing.T) {
			proxyTestEnvironment(t, map[string]string{"HTTPS_PROXY": raw})
			_, err := NewEnvironmentProxyRoutes([]Origin{origin})
			if err == nil {
				t.Fatal("invalid deployment proxy accepted")
			}
			proxyTestSafeFormatting(t, err)
		})
	}
	proxyTestEnvironment(t, map[string]string{"HTTP_PROXY": "http://user:proxy-private-canary@gateway.example"})
	if _, err := NewEnvironmentProxyRoutes([]Origin{origin}); err == nil {
		t.Fatal("unused malformed HTTP_PROXY was silently accepted")
	}
	proxyTestEnvironment(t, nil)
	for _, raw := range []string{"http://api.example", "https://127.0.0.1", "https://[::1]", "https://8.8.8.8", "https://localhost", "https://api.localhost", "https://metadata.google.internal"} {
		if _, err := NewEnvironmentProxyRoutes([]Origin{proxyTestOrigin(t, raw)}); err == nil {
			t.Fatal("unsafe proxy target origin accepted")
		}
	}
	for _, origins := range [][]Origin{nil, {Origin{}}, {origin, origin}, make([]Origin, 33)} {
		if _, err := NewEnvironmentProxyRoutes(origins); err == nil {
			t.Fatal("invalid origin list accepted")
		}
	}
}

func TestEnvironmentProxyRoutesHideDeploymentConfiguration(t *testing.T) {
	proxyTestEnvironment(t, map[string]string{
		"HTTPS_PROXY": "https://proxy-private-canary.example:8443/",
		"NO_PROXY":    "private-canary.internal",
	})
	routes, err := NewEnvironmentProxyRoutes([]Origin{proxyTestOrigin(t, "https://target-private-canary.example")})
	if err != nil {
		t.Fatal(err)
	}
	typ := reflect.TypeOf(routes)
	for i := range typ.NumField() {
		if typ.Field(i).PkgPath == "" {
			t.Fatal("proxy route exposes mutable exported state")
		}
	}
	proxyTestSafeFormatting(t, routes)
	proxyTestSafeFormatting(t, struct{ routes ProxyRoutes }{routes})
}

func proxyTestSafeFormatting(t *testing.T, value any) {
	t.Helper()
	var output strings.Builder
	for _, verb := range []string{"%v", "%+v", "%#v", "%s", "%q"} {
		fmt.Fprintf(&output, verb, value)
	}
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal("marshal safe value:", err)
	}
	output.Write(raw)
	slog.New(slog.NewTextHandler(&output, nil)).Info("probe", "value", value)
	slog.New(slog.NewJSONHandler(&output, nil)).Info("probe", "value", value)
	if strings.Contains(output.String(), "private-canary") {
		t.Fatal("proxy configuration or credential escaped implicit formatting")
	}
}

type proxyTestResolver struct {
	calls atomic.Int32
	ips   []netip.Addr
}

func (r *proxyTestResolver) Lookup(context.Context, string) ([]netip.Addr, error) {
	r.calls.Add(1)
	if len(r.ips) == 0 {
		return nil, errors.New("unexpected target DNS lookup")
	}
	return append([]netip.Addr(nil), r.ips...), nil
}

// The loopback socket is the task-owned deployment gateway, not a target IP.
// Gateway discovery, target TLS hostname verification, business writes and
// client lifecycle run through the production proxy implementation and address
// classifier. Target DNS is delegated to the trusted gateway.
type proxyTestGateway struct {
	listener net.Listener
	mu       sync.Mutex
	conns    map[net.Conn]bool
	workers  sync.WaitGroup
	done     chan struct{}
	accepts  atomic.Int32
}

func proxyTestStartGateway(t *testing.T, serve func(net.Conn)) *proxyTestGateway {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	gateway := &proxyTestGateway{listener: listener, conns: map[net.Conn]bool{}, done: make(chan struct{})}
	go func() {
		defer close(gateway.done)
		defer gateway.workers.Wait()
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			gateway.accepts.Add(1)
			gateway.mu.Lock()
			gateway.conns[conn] = true
			gateway.mu.Unlock()
			gateway.workers.Add(1)
			go func() {
				defer gateway.workers.Done()
				defer func() {
					_ = conn.Close()
					gateway.mu.Lock()
					delete(gateway.conns, conn)
					gateway.mu.Unlock()
				}()
				_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
				serve(conn)
			}()
		}
	}()
	t.Cleanup(func() {
		_ = gateway.listener.Close()
		gateway.mu.Lock()
		for conn := range gateway.conns {
			_ = conn.Close()
		}
		gateway.mu.Unlock()
		select {
		case <-gateway.done:
		case <-time.After(2 * time.Second):
			t.Error("owned proxy listener/handlers did not join")
		}
	})
	return gateway
}

func (g *proxyTestGateway) URL() string { return "http://" + g.listener.Addr().String() }

func proxyTestReadConnect(t *testing.T, conn net.Conn) bool {
	t.Helper()
	request, err := http.ReadRequest(bufio.NewReader(conn))
	if err != nil {
		t.Error("owned proxy did not receive a CONNECT request")
		return false
	}
	defer request.Body.Close()
	if request.Method != "CONNECT" || request.RequestURI != "api.example:443" || request.Host != "api.example:443" {
		t.Error("CONNECT did not preserve the exact target hostname and port")
		return false
	}
	for name, values := range request.Header {
		if strings.EqualFold(name, "Authorization") || strings.EqualFold(name, "Proxy-Authorization") || strings.Contains(strings.Join(values, " "), "private-canary") {
			t.Error("business credential appeared in CONNECT")
			return false
		}
	}
	return true
}

func proxyTestCertificate(t *testing.T, host string, extraHosts ...string) (tls.Certificate, TrustStore) {
	t.Helper()
	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	caTemplate := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "owned proxy test root"}, NotBefore: now.Add(-time.Minute), NotAfter: now.Add(time.Hour), BasicConstraintsValid: true, IsCA: true, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageCRLSign}
	caDER, err := x509.CreateCertificate(rand.Reader, caTemplate, caTemplate, &caKey.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	leafKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	leaf := &x509.Certificate{SerialNumber: big.NewInt(2), NotBefore: now.Add(-time.Minute), NotAfter: now.Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	for _, name := range append([]string{host}, extraHosts...) {
		if ip := net.ParseIP(name); ip != nil {
			leaf.IPAddresses = append(leaf.IPAddresses, ip)
		} else {
			leaf.DNSNames = append(leaf.DNSNames, name)
		}
	}
	leafDER, err := x509.CreateCertificate(rand.Reader, leaf, caTemplate, &leafKey.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "root.pem")
	if err := os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER}), 0600); err != nil {
		t.Fatal(err)
	}
	trust, err := LoadTrustStore(path)
	if err != nil {
		t.Fatal(err)
	}
	return tls.Certificate{Certificate: [][]byte{leafDER}, PrivateKey: leafKey}, trust
}

func proxyTestClient(t *testing.T, trust TrustStore, resolver Resolver) (*Client, *policyState) {
	t.Helper()
	origin := proxyTestOrigin(t, "https://api.example")
	routes, err := NewEnvironmentProxyRoutes([]Origin{origin})
	if err != nil {
		t.Fatal(err)
	}
	rules, err := NewRules()
	if err != nil {
		t.Fatal(err)
	}
	state := &policyState{mirror: policy(1, rules), available: true}
	client, err := NewClientWithProxy(&PolicyService{data: func() *policyState { return state }}, trust, resolver, routes)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := client.ForceClose(ctx); err != nil {
			t.Error("proxy client did not join:", err)
		}
	})
	return client, state
}

func proxyTestProfile(t *testing.T, credential bool, limits Limits) Profile {
	t.Helper()
	// This package test supplies only the local call projection. It exercises
	// network/policy behavior and makes no claim about the real Audit adapter.
	options := ProfileOptions{Consumer: ac.Model, Context: CallContext{data: func() callData { return callData{} }}, Limits: limits}
	if credential {
		material, err := sc.NewSecretMaterial([]byte("business-private-canary"))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(material.Destroy)
		header, err := HeaderCredential("Authorization", "Bearer ", material)
		if err != nil {
			t.Fatal(err)
		}
		query, err := QueryCredential("key", material)
		if err != nil {
			t.Fatal(err)
		}
		options.Credentials, err = NewCredentialBinding(proxyTestOrigin(t, "https://api.example"), header, query)
		if err != nil {
			t.Fatal(err)
		}
	}
	profile, err := NewProfile(options)
	if err != nil {
		t.Fatal(err)
	}
	return profile
}

func proxyTestRequest(t *testing.T) *http.Request {
	t.Helper()
	request, err := http.NewRequest("GET", "https://api.example/start", nil)
	if err != nil {
		t.Fatal(err)
	}
	return request
}

func TestProxyConnectTLSAndSameOriginCredentials(t *testing.T) {
	certificate, trust := proxyTestCertificate(t, "api.example")
	var business, handshakes atomic.Int32
	gateway := proxyTestStartGateway(t, func(conn net.Conn) {
		if !proxyTestReadConnect(t, conn) {
			return
		}
		if _, err := io.WriteString(conn, "HTTP/1.1 200 Connection Established\r\n\r\n"); err != nil {
			return
		}
		target := tls.Server(conn, &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{certificate}, GetConfigForClient: func(hello *tls.ClientHelloInfo) (*tls.Config, error) {
			if hello.ServerName != "api.example" {
				t.Error("TLS did not verify the original target hostname")
			}
			handshakes.Add(1)
			return nil, nil
		}})
		defer target.Close()
		request, err := http.ReadRequest(bufio.NewReader(target))
		if err != nil {
			t.Error("target did not receive business HTTP after TLS")
			return
		}
		defer request.Body.Close()
		business.Add(1)
		if request.Host != "api.example:443" || request.Header.Get("Authorization") != "Bearer business-private-canary" || request.URL.Query().Get("key") != "business-private-canary" {
			t.Error("same-origin business credential or hostname was lost")
		}
		switch request.URL.Path {
		case "/start":
			_, _ = io.WriteString(target, "HTTP/1.1 302 Found\r\nLocation: /finish\r\nContent-Length: 0\r\n\r\n")
		case "/finish":
			_, _ = io.WriteString(target, "HTTP/1.1 200 OK\r\nContent-Length: 2\r\nConnection: close\r\n\r\nok")
		default:
			t.Error("unexpected target path")
		}
	})
	// The locked httpproxy version uses lowercase precedence. Both endpoints
	// belong to this test so a precedence defect cannot touch existing services.
	unselected := proxyTestStartGateway(t, func(net.Conn) { t.Error("uppercase proxy overrode lowercase selection") })
	proxyTestEnvironment(t, map[string]string{"HTTPS_PROXY": unselected.URL(), "https_proxy": gateway.URL()})
	resolver := &proxyTestResolver{}
	client, _ := proxyTestClient(t, trust, resolver)
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	response, err := client.Do(ctx, proxyTestRequest(t), proxyTestProfile(t, true, Limits{}))
	if err != nil {
		t.Fatal("owned proxy request failed:", err)
	}
	defer response.Close()
	body, err := io.ReadAll(response.Body())
	if err != nil || response.StatusCode() != http.StatusOK || string(body) != "ok" {
		t.Fatal("tunneled response was not delivered")
	}
	decision := response.Decision()
	if !decision.Sent || decision.RedirectCount != 1 || string(decision.AddressClass) != "trusted_proxy" || decision.Origin != "https://api.example:443" {
		t.Fatalf("wrong proxy decision: %+v", decision)
	}
	if business.Load() != 2 || handshakes.Load() != 2 || resolver.calls.Load() != 0 {
		t.Fatal("proxy requests escaped the tunnel, target DNS was called, or redirect was lost")
	}
}

func TestProxyTLSFailuresSendNoBusinessRequest(t *testing.T) {
	for _, kind := range []string{"wrong_hostname", "untrusted_ca"} {
		t.Run(kind, func(t *testing.T) {
			host := "api.example"
			if kind == "wrong_hostname" {
				host = "other.example"
			}
			certificate, trust := proxyTestCertificate(t, host)
			if kind == "untrusted_ca" {
				var err error
				trust, err = LoadTrustStore("")
				if err != nil {
					t.Fatal(err)
				}
			}
			var business atomic.Int32
			done := make(chan struct{}, 1)
			gateway := proxyTestStartGateway(t, func(conn net.Conn) {
				defer func() { done <- struct{}{} }()
				if !proxyTestReadConnect(t, conn) {
					return
				}
				_, _ = io.WriteString(conn, "HTTP/1.1 200 Connection Established\r\n\r\n")
				target := tls.Server(conn, &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{certificate}})
				defer target.Close()
				if request, err := http.ReadRequest(bufio.NewReader(target)); err == nil {
					business.Add(1)
					request.Body.Close()
				}
			})
			proxyTestEnvironment(t, map[string]string{"HTTPS_PROXY": gateway.URL()})
			resolver := &proxyTestResolver{}
			client, _ := proxyTestClient(t, trust, resolver)
			ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
			defer cancel()
			_, err := client.Do(ctx, proxyTestRequest(t), proxyTestProfile(t, true, Limits{}))
			if err == nil || SafeNetworkError(err).Decision().Sent || SafeNetworkError(err).Decision().Reason != ac.TLSFailed {
				t.Fatal("unverified target TLS sent business bytes or was not rejected")
			}
			proxyTestSafeFormatting(t, err)
			select {
			case <-done:
			case <-ctx.Done():
				t.Fatal("failed target TLS did not close the gateway socket")
			}
			if business.Load() != 0 || resolver.calls.Load() != 0 || gateway.accepts.Load() != 1 {
				t.Fatal("TLS failure leaked business HTTP or retried outside its tunnel")
			}
		})
	}
}

func TestProxyHTTPSGatewayTLS(t *testing.T) {
	for _, mode := range []string{"trusted", "wrong_gateway_hostname", "untrusted_gateway_ca"} {
		t.Run(mode, func(t *testing.T) {
			gatewaySAN := "127.0.0.1"
			if mode == "wrong_gateway_hostname" {
				gatewaySAN = "other.example"
			}
			certificate, trust := proxyTestCertificate(t, "api.example", gatewaySAN)
			if mode == "untrusted_gateway_ca" {
				var err error
				trust, err = LoadTrustStore("")
				if err != nil {
					t.Fatal(err)
				}
			}
			var connects, business atomic.Int32
			done := make(chan struct{})
			gateway := proxyTestStartGateway(t, func(conn net.Conn) {
				defer close(done)
				outer := tls.Server(conn, &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{certificate}})
				defer outer.Close()
				if err := outer.Handshake(); err != nil {
					if mode == "trusted" {
						t.Error("trusted gateway TLS handshake failed")
					}
					return
				}
				if !proxyTestReadConnect(t, outer) {
					return
				}
				connects.Add(1)
				_, _ = io.WriteString(outer, "HTTP/1.1 200 Connection Established\r\n\r\n")
				target := tls.Server(outer, &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{certificate}})
				defer target.Close()
				request, err := http.ReadRequest(bufio.NewReader(target))
				if err != nil {
					t.Error("nested target TLS request did not arrive")
					return
				}
				defer request.Body.Close()
				business.Add(1)
				if request.Header.Get("Authorization") != "Bearer business-private-canary" {
					t.Error("nested TLS lost the business credential")
				}
				_, _ = io.WriteString(target, "HTTP/1.1 200 OK\r\nContent-Length: 2\r\nConnection: close\r\n\r\nok")
			})
			proxyTestEnvironment(t, map[string]string{"HTTPS_PROXY": "https://" + gateway.listener.Addr().String()})
			resolver := &proxyTestResolver{}
			client, _ := proxyTestClient(t, trust, resolver)
			ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
			defer cancel()
			response, err := client.Do(ctx, proxyTestRequest(t), proxyTestProfile(t, true, Limits{}))
			if mode == "trusted" {
				if err != nil {
					t.Fatal("HTTPS gateway request failed:", err)
				}
				body, readErr := io.ReadAll(response.Body())
				response.Close()
				if readErr != nil || string(body) != "ok" || !response.Decision().Sent {
					t.Fatal("nested TLS response lost body or send state")
				}
			} else if err == nil || SafeNetworkError(err).Decision().Reason != ac.TLSFailed || SafeNetworkError(err).Decision().Sent {
				t.Fatal("unverified gateway TLS was not refused before business send")
			}
			select {
			case <-done:
			case <-ctx.Done():
				t.Fatal("HTTPS gateway connection did not join")
			}
			want := int32(0)
			if mode == "trusted" {
				want = 1
			}
			if connects.Load() != want || business.Load() != want || resolver.calls.Load() != 0 {
				t.Fatal("HTTPS gateway TLS boundary leaked CONNECT/business bytes or bypassed its route")
			}
		})
	}
}

func TestProxyConnectFailureAndResponseBudget(t *testing.T) {
	for _, tc := range []struct{ name, wire string }{
		{"non_200", "HTTP/1.1 407 Proxy Authentication Required\r\nContent-Length: 0\r\n\r\n"},
		{"malformed", "private-canary is not an HTTP response\r\n\r\n"},
		{"oversized_headers", "HTTP/1.1 200 Connection Established\r\nX-Long: " + strings.Repeat("x", 512) + "\r\n\r\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, trust := proxyTestCertificate(t, "api.example")
			trailing := make(chan int64, 1)
			gateway := proxyTestStartGateway(t, func(conn net.Conn) {
				if !proxyTestReadConnect(t, conn) {
					trailing <- -1
					return
				}
				_, _ = io.WriteString(conn, tc.wire)
				n, _ := io.Copy(io.Discard, conn)
				trailing <- n
			})
			proxyTestEnvironment(t, map[string]string{"HTTPS_PROXY": gateway.URL()})
			resolver := &proxyTestResolver{}
			client, _ := proxyTestClient(t, trust, resolver)
			ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
			defer cancel()
			_, err := client.Do(ctx, proxyTestRequest(t), proxyTestProfile(t, true, Limits{ResponseHeaderBytes: 256}))
			if err == nil || SafeNetworkError(err).Decision().Sent {
				t.Fatal("failed CONNECT accepted or marked as a business send")
			}
			proxyTestSafeFormatting(t, err)
			select {
			case n := <-trailing:
				if n != 0 {
					t.Fatal("client sent additional bytes after CONNECT rejection")
				}
			case <-ctx.Done():
				t.Fatal("rejected CONNECT did not close its socket")
			}
			if resolver.calls.Load() != 0 || gateway.accepts.Load() != 1 {
				t.Fatal("failed CONNECT fell back or retried")
			}
		})
	}
}

func TestProxyCancellationClosesConnectAndTLSAndDrains(t *testing.T) {
	for _, stage := range []string{"connect", "tls", "force_close_connect", "force_close_tls"} {
		t.Run(stage, func(t *testing.T) {
			_, trust := proxyTestCertificate(t, "api.example")
			ready, closed := make(chan struct{}), make(chan struct{})
			gateway := proxyTestStartGateway(t, func(conn net.Conn) {
				defer close(closed)
				if !proxyTestReadConnect(t, conn) {
					return
				}
				if strings.HasSuffix(stage, "tls") {
					_, _ = io.WriteString(conn, "HTTP/1.1 200 Connection Established\r\n\r\n")
					var header [5]byte
					if _, err := io.ReadFull(conn, header[:]); err != nil {
						return
					}
					if header[0] != 22 {
						t.Error("expected TLS handshake after CONNECT")
						return
					}
					if _, err := io.CopyN(io.Discard, conn, int64(binary.BigEndian.Uint16(header[3:]))); err != nil {
						return
					}
				}
				close(ready)
				_, _ = io.Copy(io.Discard, conn)
			})
			proxyTestEnvironment(t, map[string]string{"HTTPS_PROXY": gateway.URL()})
			resolver := &proxyTestResolver{}
			client, _ := proxyTestClient(t, trust, resolver)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			result := make(chan error, 1)
			request, profile := proxyTestRequest(t), proxyTestProfile(t, true, Limits{})
			go func() { _, err := client.Do(ctx, request, profile); result <- err }()
			select {
			case <-ready:
			case <-time.After(2 * time.Second):
				t.Fatal("request did not reach the cancellation barrier")
			}
			if strings.HasPrefix(stage, "force_close_") {
				forceCtx, stop := context.WithTimeout(context.Background(), time.Second)
				err := client.ForceClose(forceCtx)
				stop()
				if err != nil {
					t.Fatal("ForceClose did not join the proxy operation:", err)
				}
			} else {
				cancel()
			}
			select {
			case err := <-result:
				if err == nil || SafeNetworkError(err).Decision().Sent || SafeNetworkError(err).Decision().Reason != ac.Cancelled {
					t.Fatal("cancellation was not reported as zero business bytes")
				}
			case <-time.After(time.Second):
				t.Fatal("cancellation did not release the caller")
			}
			select {
			case <-closed:
			case <-time.After(time.Second):
				t.Fatal("cancellation did not close the owned gateway connection")
			}
			client.StopAdmission()
			drainCtx, stop := context.WithTimeout(context.Background(), time.Second)
			defer stop()
			if err := client.Drain(drainCtx); err != nil {
				t.Fatal("cancelled proxy operation did not drain:", err)
			}
			if resolver.calls.Load() != 0 {
				t.Fatal("cancellation retried target DNS")
			}
		})
	}
}

func TestProxyFirstBusinessWriteRechecksCurrentPolicy(t *testing.T) {
	certificate, trust := proxyTestCertificate(t, "api.example")
	var business atomic.Int32
	done := make(chan struct{})
	gateway := proxyTestStartGateway(t, func(conn net.Conn) {
		defer close(done)
		if !proxyTestReadConnect(t, conn) {
			return
		}
		_, _ = io.WriteString(conn, "HTTP/1.1 200 Connection Established\r\n\r\n")
		target := tls.Server(conn, &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{certificate}})
		defer target.Close()
		if request, err := http.ReadRequest(bufio.NewReader(target)); err == nil {
			business.Add(1)
			request.Body.Close()
		}
	})
	proxyTestEnvironment(t, map[string]string{"HTTPS_PROXY": gateway.URL()})
	resolver := &proxyTestResolver{}
	client, state := proxyTestClient(t, trust, resolver)
	var observed atomic.Bool
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	ctx = httptrace.WithClientTrace(ctx, &httptrace.ClientTrace{GotConn: func(httptrace.GotConnInfo) {
		// This callback runs after tunnel/TLS completion and before the real
		// firstWriter acquires its current-policy send permit.
		state.mu.Lock()
		state.available = false
		state.mu.Unlock()
		observed.Store(true)
	}})
	_, err := client.Do(ctx, proxyTestRequest(t), proxyTestProfile(t, true, Limits{}))
	if !observed.Load() || err == nil || SafeNetworkError(err).Decision().Reason != ac.PolicyUnavailable || SafeNetworkError(err).Decision().Sent {
		t.Fatal("proxy handshake bypassed the current policy at first business write")
	}
	select {
	case <-done:
	case <-ctx.Done():
		t.Fatal("denied first write did not close the tunnel")
	}
	if business.Load() != 0 || resolver.calls.Load() != 0 {
		t.Fatal("revoked policy leaked a business request or fell back to target DNS")
	}
}

func TestProxySentRequestDoesNotRetryAfterResponseDisconnect(t *testing.T) {
	certificate, trust := proxyTestCertificate(t, "api.example")
	var business atomic.Int32
	gateway := proxyTestStartGateway(t, func(conn net.Conn) {
		if !proxyTestReadConnect(t, conn) {
			return
		}
		_, _ = io.WriteString(conn, "HTTP/1.1 200 Connection Established\r\n\r\n")
		target := tls.Server(conn, &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{certificate}})
		defer target.Close()
		if request, err := http.ReadRequest(bufio.NewReader(target)); err == nil {
			business.Add(1)
			request.Body.Close()
		}
		// Close after observing the whole request, before any response headers.
	})
	proxyTestEnvironment(t, map[string]string{"HTTPS_PROXY": gateway.URL()})
	resolver := &proxyTestResolver{}
	client, _ := proxyTestClient(t, trust, resolver)
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	_, err := client.Do(ctx, proxyTestRequest(t), proxyTestProfile(t, true, Limits{}))
	if err == nil || !SafeNetworkError(err).Decision().Sent {
		t.Fatal("response disconnect lost the real business-send fact")
	}
	if business.Load() != 1 || gateway.accepts.Load() != 1 || resolver.calls.Load() != 0 {
		t.Fatal("already-sent proxy request was retried")
	}
}

func TestProxyNoProxyRetainsDirectAddressPolicy(t *testing.T) {
	_, trust := proxyTestCertificate(t, "api.example")
	gateway := proxyTestStartGateway(t, func(net.Conn) { t.Error("NO_PROXY opened a proxy socket") })
	proxyTestEnvironment(t, map[string]string{"HTTPS_PROXY": gateway.URL(), "NO_PROXY": "api.example"})
	resolver := &proxyTestResolver{ips: []netip.Addr{netip.MustParseAddr("127.0.0.1")}}
	client, _ := proxyTestClient(t, trust, resolver)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, err := client.Do(ctx, proxyTestRequest(t), proxyTestProfile(t, false, Limits{}))
	if err == nil || SafeNetworkError(err).Decision().Reason != ac.AddressForbidden || SafeNetworkError(err).Decision().Sent {
		t.Fatal("NO_PROXY bypassed the direct loopback denial")
	}
	if resolver.calls.Load() != 1 || gateway.accepts.Load() != 0 {
		t.Fatal("NO_PROXY did not take the direct DNS/policy path")
	}
}
