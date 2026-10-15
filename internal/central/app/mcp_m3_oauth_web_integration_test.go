//go:build integration && linux

package app

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"log"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	mcpfixture "github.com/LunaDeerTech/agenteam/tests/mcp/fixture"
)

// The one issuer authorization GET is relayed by Node with the actual fixture
// CA and TLS verification. This does not verify Chromium's independent CA
// trust store. App Begin/Complete/lookup use the real browser/Account boundary.
func TestMCPM3OAuthOwnerWebTLSRelay(t *testing.T) {
	mcpM3ConfigurationPreflight(t)
	dist, runtime, evidence := os.Getenv("AGENTEAM_MCP_WEB_DIST"), os.Getenv("AGENTEAM_AUTH_WEB_RUNTIME"), os.Getenv("AGENTEAM_MCP_WEB_EVIDENCE")
	playwright, chromium, config := os.Getenv("AGENTEAM_MCP_WEB_PLAYWRIGHT"), os.Getenv("AGENTEAM_AUTH_WEB_CHROMIUM"), os.Getenv("AGENTEAM_MCP_WEB_CONFIG")
	for _, path := range []string{dist, runtime, evidence, playwright, chromium, config} {
		if !filepath.IsAbs(path) {
			t.Fatal("exact owned OAuth browser inputs required")
		}
	}
	if len(runtime) > 45 {
		t.Fatal("owned browser runtime path too long")
	}
	if info, err := os.Stat(filepath.Join(dist, "index.html")); err != nil || !info.Mode().IsRegular() {
		t.Fatal("frozen production assets absent")
	}
	private, err := os.MkdirTemp(runtime, "oauth-")
	workAppCheck(t, "oauth-web-private", err)
	t.Cleanup(func() { workAppCheck(t, "oauth-web-private-cleanup", os.RemoveAll(private)) })
	evidence = filepath.Join(evidence, t.Name())
	workAppCheck(t, "oauth-web-evidence", os.Mkdir(evidence, 0700))
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	workAppCheck(t, "oauth-web-listener", err)
	t.Cleanup(func() { _ = listener.Close() })
	origin := "https://" + listener.Addr().String()
	certificate, ca := mcpM3OAuthBrowserCertificate(t)
	chromium = mcpM3OAuthBrowserTrust(t, private, chromium, ca)
	peer := mcpfixture.New(t)
	v := newMCPM3AppWithOrigin(t, peer, nil, origin)
	base := "/api/v1/projects/" + v.a.String() + "/mcp"
	backend, err := url.Parse(v.address)
	workAppCheck(t, "oauth-web-backend", err)
	transport := &http.Transport{Proxy: nil}
	proxy := httputil.NewSingleHostReverseProxy(backend)
	proxy.Transport, proxy.ErrorLog = transport, log.New(io.Discard, "", 0)
	var mu sync.Mutex
	begins, completes, lookups := 0, 0, 0
	dropped := false
	completeKey, lookupKey := "", ""
	proxy.ModifyResponse = func(response *http.Response) error {
		r := response.Request
		if r.Method != http.MethodPost {
			return nil
		}
		mu.Lock()
		defer mu.Unlock()
		switch r.URL.Path {
		case base + "/oauth/begin":
			begins++
			return nil
		case base + "/commands/lookup":
			lookups++
			lookupKey = r.Header.Get("Idempotency-Key")
			return nil
		case "/api/v1/mcp/oauth/complete":
			completes++
			completeKey = r.Header.Get("Idempotency-Key")
		default:
			return nil
		}
		if dropped {
			return nil
		}
		raw, err := io.ReadAll(io.LimitReader(response.Body, 32769))
		closed := response.Body.Close()
		defer clear(raw)
		var receipt struct {
			Connection struct {
				State string `json:"state"`
			} `json:"connection"`
		}
		if err != nil || closed != nil || len(raw) > 32768 || response.StatusCode != http.StatusOK || json.Unmarshal(raw, &receipt) != nil || receipt.Connection.State != "connected" || completeKey == "" || r.Header.Get("Origin") != origin || r.Header.Get("X-CSRF-Token") == "" {
			return errors.New("owned OAuth response invalid")
		}
		dropped = true
		return errors.New("owned committed OAuth response interrupted")
	}
	proxy.ErrorHandler = func(w http.ResponseWriter, _ *http.Request, _ error) {
		http.Error(w, "Owned response unavailable", http.StatusBadGateway)
	}
	server := &http.Server{TLSConfig: &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{certificate}}, ReadHeaderTimeout: 2 * time.Second, ErrorLog: log.New(io.Discard, "", 0), Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			proxy.ServeHTTP(w, r)
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.NotFound(w, r)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/assets/") {
			skillWebServeAsset(w, r, dist)
			return
		}
		http.ServeFile(w, r, filepath.Join(dist, "index.html"))
	})}
	served := make(chan error, 1)
	go func() { served <- server.ServeTLS(listener, "", "") }()
	var once sync.Once
	stop := func() {
		once.Do(func() {
			ctx, cancel := context.WithTimeout(context.WithoutCancel(v.ctx), 3*time.Second)
			defer cancel()
			if err := server.Shutdown(ctx); err != nil {
				t.Error("OAuth browser proxy shutdown did not join")
				_ = server.Close()
			}
			if err := <-served; err != nil && !errors.Is(err, http.ErrServerClosed) {
				t.Error("OAuth browser original Serve failed")
			}
			transport.CloseIdleConnections()
		})
	}
	t.Cleanup(stop)
	// The ordinary App fixture sends its localhost cookie. This HTTPS owner
	// must use Account's production cookie name over the real trusted origin.
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(ca) {
		t.Fatal("OAuth browser owner CA absent")
	}
	ownerTransport := &http.Transport{Proxy: nil, TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: roots}}
	defer ownerTransport.CloseIdleConnections()
	ownerClient := &http.Client{Transport: ownerTransport, Timeout: 8 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	ownerRequest, err := http.NewRequestWithContext(v.ctx, http.MethodGet, origin+"/api/v1/session", nil)
	workAppCheck(t, "oauth-web-session-request", err)
	ownerRequest.Header.Set("Origin", origin)
	ownerRequest.Header.Set("Sec-Fetch-Site", "same-origin")
	ownerRequest.AddCookie(&http.Cookie{Name: "__Host-agenteam_session", Value: v.cookie, Path: "/", Secure: true, HttpOnly: true, SameSite: http.SameSiteStrictMode})
	session, err := ownerClient.Do(ownerRequest)
	workAppCheck(t, "oauth-web-session", err)
	sessionBody, readErr := io.ReadAll(io.LimitReader(session.Body, (1<<20)+1))
	closed := session.Body.Close()
	defer clear(sessionBody)
	workAppCheck(t, "oauth-web-session-read", readErr)
	workAppCheck(t, "oauth-web-session-close", closed)
	var identity struct {
		User struct {
			Username string `json:"username"`
		} `json:"user"`
	}
	decoded := len(sessionBody) <= 1<<20 && json.Unmarshal(sessionBody, &identity) == nil
	if session.StatusCode != http.StatusOK || !decoded || identity.User.Username == "" {
		t.Fatalf("OAuth Owner route identity absent status=%d decoded=%t user_present=%t", session.StatusCode, decoded, identity.User.Username != "")
	}
	payload := map[string]any{"origin": origin, "cookie": v.cookie, "route": "/" + identity.User.Username + "/runtime-app-a/settings/mcp", "endpoint": peer.OAuthEndpoint(), "issuerIP": peer.Address(), "caFile": peer.CAFile(), "evidence": evidence}
	raw, err := json.Marshal(payload)
	workAppCheck(t, "oauth-web-input-encode", err)
	err = os.WriteFile(filepath.Join(private, "input.json"), raw, 0600)
	clear(raw)
	delete(payload, "cookie")
	workAppCheck(t, "oauth-web-input", err)
	mcpM3RunBrowser(t, v.ctx, private, config, playwright, chromium)
	mu.Lock()
	actualBegins, actualCompletes, actualLookups, actualDropped := begins, completes, lookups, dropped
	exactKey := completeKey != "" && completeKey == lookupKey
	mu.Unlock()
	if actualBegins != 1 || actualCompletes != 1 || actualLookups != 1 || !actualDropped || !exactKey {
		t.Fatalf("OAuth browser begin=%d complete=%d lookup=%d dropped=%t exact_key=%t", actualBegins, actualCompletes, actualLookups, actualDropped, exactKey)
	}
	state, err := peer.OAuthState(v.ctx)
	workAppCheck(t, "oauth-web-provider-state", err)
	if state.Registrations != 1 || state.Exchanges != 1 || state.Refreshes != 0 || state.Accepted < 1 {
		t.Fatal("OAuth browser did not use actual DCR/S256/single exchange/SDK")
	}
	var requests, retired, operations, flows, tokens int
	err = v.admin.QueryRow(v.ctx, `SELECT count(*),count(*) FILTER(WHERE phase='complete' AND io_retired AND lease_released AND process_required AND process_id=$2),count(DISTINCT operation) FILTER(WHERE operation IN ('begin','exchange','retire')) FROM agenteam_mcp.oauth_requests WHERE project_id=$1`, v.a.String(), v.objects.process.String()).Scan(&requests, &retired, &operations)
	workAppCheck(t, "oauth-web-original-request-facts", err)
	err = v.admin.QueryRow(v.ctx, `SELECT (SELECT count(*) FROM agenteam_mcp.oauth_flows WHERE project_id=$1 AND phase='complete'),(SELECT count(*) FROM agenteam_mcp.oauth_tokens WHERE project_id=$1)`, v.a.String()).Scan(&flows, &tokens)
	workAppCheck(t, "oauth-web-original-flow-facts", err)
	if requests != 3 || retired != requests || operations != 3 || flows != 1 || tokens != 1 {
		t.Fatal("OAuth browser original durable requests/flow retirement incomplete")
	}
	stop()
	v.stopBeforeRelease(t)
	v.store.releaseDirectory()
	usageAppWait(t, v.root.done, "oauth-web-original-app")
	workAppCheck(t, "oauth-web-original-app-return", v.root.err)
	if !v.runtime.Joined() || !v.assembly.Joined() || !v.root.owned.producersJoined() {
		t.Fatal("OAuth browser original owners did not join")
	}
	t.Log("MCP_OAUTH_WEB actual_app=true same_tab=true clean_callback_csrf_post=true complete1_lookup1=true actual_issuer_tls_relay=true browser_trust_store_not_verified=true official_sdk_discovery=true original_owners_joined=true")
}

// The private NSS database is visible only inside this browser's mount
// namespace. HOME, the parent's trust database and Chromium's TLS verifier are
// unchanged. No server key is written to disk or given to the browser.
func mcpM3OAuthBrowserCertificate(t *testing.T) (tls.Certificate, []byte) {
	t.Helper()
	// Match the real controlled SDK fixture and Chromium's X.509 algorithms.
	// Chromium 151's certificate verifier does not accept Ed25519 signatures.
	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	workAppCheck(t, "oauth-web-ca-key", err)
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	workAppCheck(t, "oauth-web-server-key", err)
	t.Cleanup(func() { caKey.D.SetInt64(0); key.D.SetInt64(0) })
	now := time.Now()
	ca := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "Owned MCP browser root"}, NotBefore: now.Add(-time.Minute), NotAfter: now.Add(time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageCRLSign}
	caDER, err := x509.CreateCertificate(rand.Reader, ca, ca, &caKey.PublicKey, caKey)
	workAppCheck(t, "oauth-web-ca", err)
	leaf := &x509.Certificate{SerialNumber: big.NewInt(2), Subject: pkix.Name{CommonName: "Owned MCP browser endpoint"}, NotBefore: now.Add(-time.Minute), NotAfter: now.Add(time.Hour), BasicConstraintsValid: true, KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}}
	der, err := x509.CreateCertificate(rand.Reader, leaf, ca, &key.PublicKey, caKey)
	workAppCheck(t, "oauth-web-server-certificate", err)
	return tls.Certificate{Certificate: [][]byte{der, caDER}, PrivateKey: key}, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER})
}

func mcpM3OAuthBrowserTrust(t *testing.T, private, chromium string, ca []byte) string {
	t.Helper()
	for _, path := range []string{private, chromium} {
		if !filepath.IsAbs(path) {
			t.Fatal("absolute owned browser trust paths required")
		}
	}
	certutil, err := exec.LookPath("certutil")
	workAppCheck(t, "oauth-web-certutil", err)
	bwrap, err := exec.LookPath("bwrap")
	workAppCheck(t, "oauth-web-bwrap", err)
	home, err := os.UserHomeDir()
	workAppCheck(t, "oauth-web-home-observer", err)
	nss := filepath.Join(private, "trust", "pki", "nssdb")
	workAppCheck(t, "oauth-web-private-nss", os.MkdirAll(nss, 0700))
	caFile := filepath.Join(private, "browser-root.pem")
	workAppCheck(t, "oauth-web-public-ca", os.WriteFile(caFile, ca, 0600))
	for _, args := range [][]string{{"-N", "--empty-password", "-d", "sql:" + nss}, {"-A", "-d", "sql:" + nss, "-n", "owned-mcp-app", "-t", "C,,", "-i", caFile}} {
		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
		command := exec.CommandContext(ctx, certutil, args...)
		command.Dir = private
		err := command.Run()
		cancel()
		workAppCheck(t, "oauth-web-private-certutil-wait", err)
	}
	args := []string{bwrap, "--unshare-user", "--unshare-pid", "--die-with-parent", "--ro-bind", "/", "/", "--dev", "/dev", "--proc", "/proc", "--bind", private, private, "--setenv", "XDG_DATA_HOME", filepath.Join(private, "trust"), "--setenv", "XDG_CACHE_HOME", filepath.Join(private, "cache"), "--setenv", "XDG_CONFIG_HOME", filepath.Join(private, "config")}
	legacy := filepath.Join(home, ".pki", "nssdb")
	visibleNSS := nss
	if info, err := os.Stat(legacy); err == nil && info.IsDir() {
		// Chromium 151 prefers an existing legacy database over XDG_DATA_HOME.
		// This bind exists only in the child namespace; it never writes legacy.
		args = append(args, "--bind", nss, legacy)
		visibleNSS = legacy
	} else if err != nil && !os.IsNotExist(err) {
		workAppCheck(t, "oauth-web-legacy-nss-observer", err)
	}
	// Query the public certificate through the exact child namespace mapping.
	// The command neither reads private keys nor writes the parent's NSS store.
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	checkArgs := append(append([]string{}, args[1:]...), certutil, "-L", "-d", "sql:"+visibleNSS, "-n", "owned-mcp-app", "-a")
	visibleCA, err := exec.CommandContext(ctx, args[0], checkArgs...).Output()
	cancel()
	workAppCheck(t, "oauth-web-visible-ca-wait", err)
	visibleBlock, rest := pem.Decode(visibleCA)
	originalBlock, _ := pem.Decode(ca)
	if visibleBlock == nil || originalBlock == nil || visibleBlock.Type != "CERTIFICATE" || len(bytes.TrimSpace(rest)) != 0 || !bytes.Equal(visibleBlock.Bytes, originalBlock.Bytes) {
		t.Fatal("owned browser namespace did not expose the original CA")
	}
	t.Log("MCP_BROWSER_TRUST private_ca_visible_in_namespace=true signature=ecdsa_p256_sha256")
	args = append(args, chromium)
	for i := range args {
		args[i] = "'" + strings.ReplaceAll(args[i], "'", "'\"'\"'") + "'"
	}
	wrapper := filepath.Join(private, "owned-chromium")
	workAppCheck(t, "oauth-web-browser-wrapper", os.WriteFile(wrapper, []byte("#!/bin/sh\nexec "+strings.Join(args, " ")+" \"$@\"\n"), 0700))
	return wrapper
}

// This bounded no-database gate proves the actual browser trust boundary
// before provisioning the OAuth App. An unrelated CA must remain rejected.
func TestMCPM3OAuthBrowserTrustIsolation(t *testing.T) {
	runtime, playwright, chromium, config := os.Getenv("AGENTEAM_AUTH_WEB_RUNTIME"), os.Getenv("AGENTEAM_MCP_WEB_PLAYWRIGHT"), os.Getenv("AGENTEAM_AUTH_WEB_CHROMIUM"), os.Getenv("AGENTEAM_MCP_WEB_CONFIG")
	for _, path := range []string{runtime, playwright, chromium, config} {
		if !filepath.IsAbs(path) {
			t.Fatal("exact owned browser trust inputs required")
		}
	}
	if len(runtime) > 45 {
		t.Fatal("owned browser runtime path too long")
	}
	private, err := os.MkdirTemp(runtime, "trust-")
	workAppCheck(t, "oauth-trust-private", err)
	t.Cleanup(func() { workAppCheck(t, "oauth-trust-private-cleanup", os.RemoveAll(private)) })
	trusted, ca := mcpM3OAuthBrowserCertificate(t)
	untrusted, _ := mcpM3OAuthBrowserCertificate(t)
	chromium = mcpM3OAuthBrowserTrust(t, private, chromium, ca)
	var accepted, rejected atomic.Int64
	serve := func(cert tls.Certificate, count *atomic.Int64) *httptest.Server {
		server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/trust-check" {
				http.NotFound(w, r)
				return
			}
			count.Add(1)
			_, _ = io.WriteString(w, "owned-browser-tls-ok")
		}))
		server.TLS = &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{cert}}
		server.Config.ErrorLog = log.New(io.Discard, "", 0)
		server.StartTLS()
		t.Cleanup(server.Close)
		return server
	}
	first, second := serve(trusted, &accepted), serve(untrusted, &rejected)
	raw, err := json.Marshal(map[string]string{"trusted": first.URL + "/trust-check", "untrusted": second.URL + "/trust-check"})
	workAppCheck(t, "oauth-trust-input", err)
	workAppCheck(t, "oauth-trust-input-file", os.WriteFile(filepath.Join(private, "input.json"), raw, 0600))
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	mcpM3RunBrowser(t, ctx, private, filepath.Join(filepath.Dir(config), "trust.config.cjs"), playwright, chromium)
	if accepted.Load() != 1 || rejected.Load() != 0 {
		t.Fatal("owned browser TLS boundary not observed")
	}
	t.Log("MCP_BROWSER_TRUST actual_chromium_tls=true owned_ca_accepted=true unrelated_ca_rejected=true private_nss_namespace=true original_browser_wait=true")
}
