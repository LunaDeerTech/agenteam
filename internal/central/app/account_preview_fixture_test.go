//go:build integration

package app

import (
	"bufio"
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"log"
	"log/slog"
	"math/big"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/http/httputil"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/account"
	c "github.com/LunaDeerTech/agenteam/internal/central/account/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/config"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/outbound"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
	"github.com/LunaDeerTech/agenteam/internal/platform/logging"
	pgfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/postgres"
	"github.com/LunaDeerTech/agenteam/tests/testsupport/previewruntime"
	smtpfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/smtp"
)

// TestAccountPreviewFixture is an explicitly opted-in, bounded development
// composition. Its API is the original production App with DevMode disabled.
// The TLS/static proxy and identity marker exist only in this test executable.
func TestAccountPreviewFixture(t *testing.T) {
	if os.Getenv("AGENTEAM_ACCOUNT_PREVIEW") != "1" {
		t.Skip("explicit account preview opt-in required")
	}
	private := os.Getenv("AGENTEAM_ACCOUNT_PREVIEW_DIRECTORY")
	info, err := os.Lstat(private)
	if !filepath.IsAbs(private) || err != nil || !info.IsDir() || info.Mode().Perm() != 0700 {
		t.Fatal("preview requires an owned 0700 directory")
	}
	actual, err := filepath.EvalSymlinks(private)
	if err != nil || actual != private {
		t.Fatal("preview directory must not contain symlinks")
	}
	if _, err = os.Lstat(filepath.Join(private, "runtime.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("preview directory already contains runtime material")
	}
	nonce, err := pgfixture.RandomHex(16)
	previewCheck(t, "nonce", err)
	port := os.Getenv("AGENTEAM_ACCOUNT_PREVIEW_PORT")
	if port == "" {
		port = "5250"
	}
	outboundPreview := os.Getenv("AGENTEAM_OUTBOUND_PREVIEW") == "1"
	ruleModesPreview := os.Getenv("AGENTEAM_RULE_MODES_PREVIEW") == "1"
	if ruleModesPreview && !outboundPreview {
		t.Fatal("rule modes preview requires owned outbound preview")
	}
	if outboundPreview && port != "5260" {
		t.Fatal("outbound preview requires dedicated owned 5260")
	}
	if port != "5250" && port != "5254" && !(outboundPreview && port == "5260") {
		t.Fatal("preview TLS port must be 5250 or 5254")
	}
	localOrigin := "https://localhost:" + port
	origin := os.Getenv("AGENTEAM_ACCOUNT_PREVIEW_ORIGIN")
	if origin == "" {
		origin = localOrigin
	}
	if origin != localOrigin && origin != "https://mcpc-terra.lunadeer.cn:18443" && origin != "https://154.12.86.233:18443" {
		t.Fatal("preview origin is not an explicitly supported HTTPS endpoint")
	}
	if outboundPreview && origin != localOrigin {
		t.Fatal("outbound preview must stay local")
	}
	originURL, err := url.Parse(origin)
	previewCheck(t, "preview origin", err)
	listener, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", port))
	previewCheck(t, "owned TLS listener", err)
	t.Cleanup(func() { _ = listener.Close() })
	security := previewCertificate(t, private, originURL.Hostname())

	// The explicit public 24h mode also extends every owned fixture certificate
	// through the shared previewruntime gate. Ordinary previews stay bounded to
	// 45m and retain their existing short-lived dependency certificates.
	startedAt := time.Now().UTC()
	deadline := startedAt.Add(previewruntime.Lifetime())
	ctx, cancel := context.WithDeadline(context.Background(), deadline)
	t.Cleanup(cancel)
	watchDone := make(chan struct{})
	go func() {
		defer close(watchDone)
		ticker := time.NewTicker(250 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if _, err := os.Stat(filepath.Join(private, "stop")); err == nil {
					cancel()
					return
				}
			}
		}
	}()
	t.Cleanup(func() { cancel(); <-watchDone })

	setup, setupCancel := context.WithTimeout(ctx, 60*time.Second)
	smtp, err := smtpfixture.Start(setup)
	setupCancel()
	previewCheck(t, "owned SMTP", err)
	t.Cleanup(func() {
		cleanup, stop := context.WithTimeout(context.Background(), 20*time.Second)
		defer stop()
		previewCheck(t, "SMTP cleanup", smtp.Close(cleanup))
	})
	endpoint, err := smtp.Create(ctx, smtpfixture.Scenario{Mode: "starttls", CaptureMessage: true})
	previewCheck(t, "SMTP endpoint", err)
	db := pgfixture.NewDatabase(t)
	cfg := guardConfiguration(t, append(guardEnvironment(t, db),
		"AGENTEAM_CENTRAL_PUBLIC_ORIGIN="+origin,
		"AGENTEAM_CENTRAL_DEV_MODE=false",
		"AGENTEAM_CENTRAL_SHUTDOWN_TIMEOUT=5s",
		"AGENTEAM_CENTRAL_OUTBOUND_CA_FILE="+smtp.CAFile))
	if cfg.DevMode() {
		t.Fatal("preview must enforce production browser boundaries")
	}
	var core *account.Service
	var transport *outboundRuntime
	deps := dependencies{
		smtpResolver:   previewSMTPResolver{netip.MustParseAddr(smtp.PrivateIP)},
		observeAccount: func(v *account.Service) { core = v },
		outboundConstruct: func(ctx context.Context, cfg config.Config, v *outboundRuntime) error {
			transport = v
			if err := v.client.ForceClose(ctx); err != nil {
				return err
			}
			v.resolver = previewSMTPResolver{netip.MustParseAddr(smtp.PrivateIP)}
			var err error
			v.client, err = outbound.NewClient(v.policy, cfg.OutboundTrust(), v.resolver)
			return err
		},
	}
	output := newEventLog()
	logger, err := logging.New(logging.Central, slog.LevelInfo, output)
	previewCheck(t, "logger", err)
	rootCtx, stopRoot := context.WithCancel(ctx)
	rootDone := make(chan struct{})
	var rootResult error
	go func() { rootResult = run(rootCtx, cfg, logger, nil, deps); close(rootDone) }()
	t.Cleanup(func() {
		stopRoot()
		select {
		case <-rootDone:
			previewCheck(t, "original App shutdown", rootResult)
		case <-time.After(7 * time.Second):
			t.Error("original preview App did not join within its shutdown budget")
		}
	})
	backend, err := url.Parse(workRootStartupAddress(t, output, rootDone, func() error { return rootResult }))
	previewCheck(t, "backend address", err)
	if core == nil || transport == nil {
		t.Fatal("production Account and outbound owners missing")
	}
	previewConfigureSMTP(t, ctx, cfg, core, transport, smtp, endpoint)
	credentials := previewBootstrapCredentials(t, cfg.AccountRecoveryLog())
	frontendAddress := "http://127.0.0.1:5245"
	if outboundPreview {
		frontendAddress = "http://127.0.0.1:5246"
	}
	frontend, _ := url.Parse(frontendAddress)
	apiProxy := httputil.NewSingleHostReverseProxy(backend)
	webProxy := httputil.NewSingleHostReverseProxy(frontend)
	proxyTransport := &http.Transport{Proxy: nil}
	t.Cleanup(proxyTransport.CloseIdleConnections)
	for _, proxy := range []*httputil.ReverseProxy{apiProxy, webProxy} {
		proxy.Transport = proxyTransport
		proxy.ErrorLog = log.New(io.Discard, "", 0)
		proxy.ErrorHandler = func(w http.ResponseWriter, _ *http.Request, _ error) {
			http.Error(w, "owned preview upstream unavailable", http.StatusBadGateway)
		}
	}
	server := &http.Server{
		ReadHeaderTimeout: 5 * time.Second,
		TLSConfig:         &tls.Config{Certificates: []tls.Certificate{security.Certificate}, MinVersion: tls.VersionTLS12},
		ErrorLog:          log.New(io.Discard, "", 0),
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Host != originURL.Host {
				http.Error(w, "preview host unavailable", http.StatusMisdirectedRequest)
				return
			}
			if r.URL.Path == "/__fixture__/identity" {
				if r.Method != http.MethodGet {
					w.WriteHeader(http.StatusMethodNotAllowed)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("Cache-Control", "no-store")
				_ = json.NewEncoder(w).Encode(map[string]string{"nonce": nonce})
				return
			}
			if r.URL.Path == "/api/v1" || strings.HasPrefix(r.URL.Path, "/api/v1/") {
				apiProxy.ServeHTTP(w, r)
				return
			}
			webProxy.ServeHTTP(w, r)
		}),
	}
	serverDone := make(chan error, 1)
	go func() { serverDone <- server.ServeTLS(listener, "", "") }()
	t.Cleanup(func() {
		cleanup, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		if err := server.Shutdown(cleanup); err != nil {
			_ = server.Close()
			t.Error("preview TLS shutdown failed")
		}
		if err := <-serverDone; !errors.Is(err, http.ErrServerClosed) {
			t.Error("preview TLS server did not return its closed state")
		}
	})
	previewFirstChain(t, ctx, origin, listener.Addr().String(), security.CA, credentials)
	manifest := map[string]any{
		"origin": origin, "certificate_spki": security.SPKI, "certificate_file": security.CertificateFile,
		"certificate_ca_file": security.CAFile,
		"local_tls_address":   listener.Addr().String(),
		"nonce":               nonce, "credentials": credentials, "recovery_log": cfg.AccountRecoveryLog(),
		"stop_file": filepath.Join(private, "stop"), "started_at": startedAt, "expires_at": deadline.Add(-time.Minute),
		"smtp_control": "http://" + net.JoinHostPort(smtp.PrivateIP, "9000"), "smtp_nonce": smtp.Nonce,
		"smtp_endpoint_id": endpoint.ID, "database_fixture": os.Getenv(pgfixture.Env), "database_name": db.Name,
	}
	if outboundPreview {
		regular := previewOutboundRegular(t, ctx, cfg, core, smtp, endpoint)
		manifest["regular_credentials"] = regular
		scope := "system-outbound-policy-proxy"
		if ruleModesPreview {
			scope = "system-outbound-rule-modes"
		}
		manifest["test_scope"] = scope
		manifest["proxy_gateway"] = previewOutboundGateway(t, ctx, smtp, endpoint)
		sidecar := map[string]string{"test_scope": scope, "runtime_file": filepath.Join(private, "runtime.json"), "nonce": nonce}
		data, e := json.Marshal(sidecar)
		previewCheck(t, "outbound scope encoding", e)
		scopePath := filepath.Join(private, "outbound-owned.json")
		previewCheck(t, "outbound scope", os.WriteFile(scopePath, data, 0600))
		t.Cleanup(func() { _ = os.Remove(scopePath) })
	}
	raw, err := json.Marshal(manifest)
	previewCheck(t, "private manifest encoding", err)
	defer clear(raw)
	manifestFile := filepath.Join(private, "runtime.json")
	t.Cleanup(func() {
		_ = os.Remove(manifestFile)
		_ = os.Remove(manifestFile + ".tmp")
	})
	previewCheck(t, "private manifest", os.WriteFile(manifestFile+".tmp", raw, 0600))
	previewCheck(t, "private manifest publication", os.Rename(manifestFile+".tmp", manifestFile))
	t.Log("preview ready: production bootstrap/login/session/me/logout, anonymous/CSRF rejection, TLS and Secure Cookies verified; controlled SMTP configured")
	select {
	case <-ctx.Done():
	case <-rootDone:
		if ctx.Err() == nil {
			t.Fatal("original preview App exited before the preview ended")
		}
	case err := <-serverDone:
		serverDone <- err
		t.Fatal("preview TLS server exited before the preview ended")
	}
}

type previewCredential struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

func previewCheck(t *testing.T, stage string, err error) {
	t.Helper()
	if err != nil {
		var fault *foundation.Fault
		if errors.As(err, &fault) {
			t.Fatalf("account preview %s: %s/%s", stage, fault.Code.Safe(), fault.CommitState.Safe())
		}
		t.Fatalf("account preview %s failed", stage)
	}
}

func previewBootstrapCredentials(t *testing.T, path string) previewCredential {
	t.Helper()
	raw, err := os.ReadFile(path)
	previewCheck(t, "bootstrap recovery record", err)
	defer clear(raw)
	scanner := bufio.NewScanner(bytes.NewReader(raw))
	var result previewCredential
	for scanner.Scan() {
		var record struct {
			Purpose, Email string
			Password       string `json:"initial_password"`
		}
		previewCheck(t, "bootstrap record decoding", json.Unmarshal(scanner.Bytes(), &record))
		if record.Purpose == "bootstrap" {
			result = previewCredential{record.Email, record.Password}
		}
	}
	previewCheck(t, "bootstrap record read", scanner.Err())
	if result.Email == "" || result.Password == "" {
		t.Fatal("bootstrap credentials missing from owned recovery log")
	}
	return result
}

type previewSMTPResolver struct{ address netip.Addr }

func (r previewSMTPResolver) Lookup(_ context.Context, host string) ([]netip.Addr, error) {
	if host != smtpfixture.Host {
		return nil, errors.New("preview permits only its owned SMTP hostname")
	}
	return []netip.Addr{r.address}, nil
}

func (r previewSMTPResolver) LookupIPAddr(ctx context.Context, host string) ([]net.IPAddr, error) {
	ips, err := r.Lookup(ctx, host)
	if err != nil {
		return nil, err
	}
	return []net.IPAddr{{IP: net.IP(ips[0].AsSlice())}}, ctx.Err()
}

func previewConfigureSMTP(t *testing.T, ctx context.Context, cfg config.Config, core *account.Service, transport *outboundRuntime, smtp *smtpfixture.Fixture, endpoint smtpfixture.Endpoint) {
	t.Helper()
	actor, err := fixtureAccountActor(ctx, cfg, core)
	previewCheck(t, "SMTP setup real login", err)
	if os.Getenv("AGENTEAM_RULE_MODES_PREVIEW") != "1" {
		ports, err := outbound.SelectedPorts(uint16(endpoint.Port))
		previewCheck(t, "SMTP exact port", err)
		rule, err := outbound.NewRule(smtp.PrivateIP+"/32", ports)
		previewCheck(t, "SMTP exact private rule", err)
		rules, err := outbound.NewRules(rule)
		previewCheck(t, "SMTP rules", err)
		key, err := foundation.NewCommandIdentity("outbound-policy-v2", []string{actor.Details().UserID}, "update", foundation.IdempotencyKey(guardID[struct{}](t).String()))
		previewCheck(t, "SMTP policy identity", err)
		_, err = transport.policy.UpdatePolicy(ctx, actor, outbound.CommandMeta{Identity: key, ExpectedVersion: *transport.policy.Status().Version}, outbound.Allowlist, rules)
		previewCheck(t, "SMTP policy", err)
	}
	settings, err := core.GetSMTPSettings(ctx, actor)
	previewCheck(t, "SMTP current settings", err)
	randomPassword, err := pgfixture.RandomHex(24)
	previewCheck(t, "SMTP credential generation", err)
	password, err := sc.NewSecretMaterial([]byte(randomPassword))
	previewCheck(t, "SMTP credential", err)
	defer password.Destroy()
	update, err := c.NewSMTPUpdate(c.SMTPUpdateFields{Actor: actor, Key: foundation.IdempotencyKey(guardID[struct{}](t).String()), ExpectedVersion: settings.Version, Configured: true, Host: smtpfixture.Host, Port: endpoint.Port, TLSMode: "starttls", Username: "fixture", Password: &password, CredentialAction: "keep", SenderEmail: "sender@example.test", SenderName: "Owned account preview", RetryCount: 0, RetryIntervalSeconds: 60})
	previewCheck(t, "SMTP setup request", err)
	_, err = core.UpdateSMTPSettings(ctx, update)
	previewCheck(t, "SMTP configure", err)
}

type previewTLS struct {
	Certificate                   tls.Certificate
	CA                            *x509.Certificate
	SPKI, CertificateFile, CAFile string
}

func previewCertificate(t *testing.T, directory, hostname string) previewTLS {
	t.Helper()
	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	previewCheck(t, "TLS CA key", err)
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	previewCheck(t, "TLS key", err)
	now := time.Now()
	validity := previewruntime.CertificateValidity(time.Hour)
	caTemplate := &x509.Certificate{SerialNumber: big.NewInt(now.UnixNano()), Subject: pkix.Name{CommonName: "Owned account preview CA"}, NotBefore: now.Add(-time.Minute), NotAfter: now.Add(validity), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageCRLSign}
	caDER, err := x509.CreateCertificate(rand.Reader, caTemplate, caTemplate, &caKey.PublicKey, caKey)
	previewCheck(t, "TLS CA certificate", err)
	ca, err := x509.ParseCertificate(caDER)
	previewCheck(t, "TLS CA parse", err)
	template := &x509.Certificate{SerialNumber: big.NewInt(now.UnixNano() + 1), Subject: pkix.Name{CommonName: "Owned account preview"}, DNSNames: []string{hostname}, IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}, NotBefore: now.Add(-time.Minute), NotAfter: now.Add(validity), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	if address := net.ParseIP(hostname); address != nil {
		template.DNSNames = nil
		template.IPAddresses = []net.IP{address}
	}
	der, err := x509.CreateCertificate(rand.Reader, template, ca, &key.PublicKey, caKey)
	previewCheck(t, "TLS certificate", err)
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	previewCheck(t, "TLS key encoding", err)
	defer clear(keyDER)
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})
	defer clear(keyPEM)
	certificate, err := tls.X509KeyPair(certPEM, keyPEM)
	previewCheck(t, "TLS pair", err)
	parsed, err := x509.ParseCertificate(der)
	previewCheck(t, "TLS parse", err)
	certificate.Leaf = parsed
	path := filepath.Join(directory, "certificate.pem")
	previewCheck(t, "TLS certificate file", os.WriteFile(path, certPEM, 0600))
	caPath := filepath.Join(directory, "certificate-ca.pem")
	previewCheck(t, "TLS CA file", os.WriteFile(caPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER}), 0600))
	sum := sha256.Sum256(parsed.RawSubjectPublicKeyInfo)
	return previewTLS{Certificate: certificate, CA: ca, SPKI: base64.StdEncoding.EncodeToString(sum[:]), CertificateFile: path, CAFile: caPath}
}

func previewFirstChain(t *testing.T, ctx context.Context, origin, localAddress string, ca *x509.Certificate, credential previewCredential) {
	t.Helper()
	trust := x509.NewCertPool()
	trust.AddCert(ca)
	originURL, err := url.Parse(origin)
	previewCheck(t, "first-chain origin", err)
	// The public-origin candidate remains entirely local until its separate
	// public proxy is enabled. URL authority, SNI, certificate verification and
	// the Secure Cookie jar still use the exact configured browser origin.
	transport := &http.Transport{Proxy: nil, TLSClientConfig: &tls.Config{RootCAs: trust, MinVersion: tls.VersionTLS12}, DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
		if address != originURL.Host {
			return nil, errors.New("preview first chain refused a different upstream")
		}
		return (&net.Dialer{}).DialContext(ctx, network, localAddress)
	}}
	defer transport.CloseIdleConnections()
	jar, err := cookiejar.New(nil)
	previewCheck(t, "Cookie jar", err)
	client := &http.Client{Transport: transport, Jar: jar, Timeout: 20 * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
	var sawBrowserCookie, sawSessionCookie bool
	request := func(method, path string, body any, csrf string, want int) map[string]any {
		var reader io.Reader
		if body != nil {
			raw, err := json.Marshal(body)
			previewCheck(t, "request encoding", err)
			defer clear(raw)
			reader = bytes.NewReader(raw)
		}
		r, err := http.NewRequestWithContext(ctx, method, origin+path, reader)
		previewCheck(t, "request construction", err)
		r.Header.Set("Origin", origin)
		r.Header.Set("Sec-Fetch-Site", "same-origin")
		if body != nil {
			r.Header.Set("Content-Type", "application/json")
		}
		if csrf != "" {
			r.Header.Set("X-CSRF-Token", csrf)
		}
		if method != http.MethodGet {
			r.Header.Set("Idempotency-Key", guardID[struct{}](t).String())
		}
		response, err := client.Do(r)
		previewCheck(t, "TLS HTTP "+path, err)
		defer response.Body.Close()
		if response.StatusCode != want {
			t.Fatalf("preview %s status=%d want=%d", path, response.StatusCode, want)
		}
		for _, cookie := range response.Cookies() {
			if strings.HasPrefix(cookie.Name, "__Host-") && (!cookie.Secure || !cookie.HttpOnly || cookie.Path != "/" || cookie.Domain != "") {
				t.Fatal("production Cookie attributes missing")
			}
			if cookie.Value != "" {
				sawBrowserCookie = sawBrowserCookie || cookie.Name == "__Host-agenteam_browser"
				sawSessionCookie = sawSessionCookie || cookie.Name == "__Host-agenteam_session"
			}
		}
		if want == http.StatusNoContent {
			return nil
		}
		var result map[string]any
		previewCheck(t, "HTTP response decoding", json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&result))
		return result
	}
	request(http.MethodGet, "/api/v1/session", nil, "", http.StatusUnauthorized)
	bootstrap := request(http.MethodGet, "/api/v1/auth/bootstrap", nil, "", http.StatusOK)
	csrf, ok := bootstrap["csrf_token"].(string)
	if !ok || csrf == "" {
		t.Fatal("anonymous CSRF missing")
	}
	request(http.MethodPost, "/api/v1/sessions/login", credential, "", http.StatusForbidden)
	request(http.MethodPost, "/api/v1/sessions/login", credential, csrf, http.StatusOK)
	session := request(http.MethodGet, "/api/v1/session", nil, "", http.StatusOK)
	request(http.MethodGet, "/api/v1/me", nil, "", http.StatusOK)
	request(http.MethodPatch, "/api/v1/me", map[string]string{"display_name": "Rejected preview change"}, "", http.StatusForbidden)
	if !sawBrowserCookie || !sawSessionCookie {
		t.Fatal("production browser and session Cookies were not both issued")
	}
	csrf, ok = session["csrf_token"].(string)
	if !ok || csrf == "" {
		t.Fatal("session CSRF missing")
	}
	request(http.MethodPost, "/api/v1/sessions/logout", struct{}{}, csrf, http.StatusNoContent)
	request(http.MethodGet, "/api/v1/session", nil, "", http.StatusUnauthorized)
}

// A second, ordinary account is created through the same invitation/delivery/
// redemption protocols as the UI. Only this task's SMTP sink sees its token.
func previewOutboundRegular(t *testing.T, ctx context.Context, cfg config.Config, core *account.Service, smtp *smtpfixture.Fixture, endpoint smtpfixture.Endpoint) previewCredential {
	t.Helper()
	actor, e := fixtureAccountActor(ctx, cfg, core)
	previewCheck(t, "ordinary seed login", e)
	email := "outbound-reader@example.test"
	input, e := c.NewInvitationCreate(c.InvitationCreateFields{Actor: actor, Key: foundation.IdempotencyKey(guardID[struct{}](t).String()), Email: email})
	previewCheck(t, "ordinary seed invitation", e)
	_, e = core.CreateInvitation(ctx, input)
	previewCheck(t, "ordinary invitation command", e)
	var link string
	delivery, stop := context.WithTimeout(ctx, 20*time.Second)
	defer stop()
	for {
		e = smtp.UseLastMessage(delivery, endpoint.ID, func(raw []byte) error {
			for _, word := range strings.Fields(string(raw)) {
				if strings.Contains(word, "/invite#") {
					u, e := url.Parse(word)
					if e == nil {
						link = u.Fragment
					}
				}
			}
			if link == "" {
				return errors.New("owned invitation link missing")
			}
			return nil
		})
		if e == nil {
			break
		}
		if !errors.Is(e, smtpfixture.ErrMessageUnavailable) {
			previewCheck(t, "ordinary delivered link", e)
		}
		select {
		case <-delivery.Done():
			t.Fatal("ordinary invitation delivery deadline")
		case <-time.After(100 * time.Millisecond):
		}
	}
	parts := strings.Split(link, ".")
	link = ""
	if len(parts) != 2 {
		t.Fatal("owned invitation link format")
	}
	id, e := foundation.ParseID[c.Invitation](parts[0])
	previewCheck(t, "ordinary invitation id", e)
	tokenMaterial, e := sc.NewSecretMaterial([]byte(parts[1]))
	parts = nil
	previewCheck(t, "ordinary invitation material", e)
	defer tokenMaterial.Destroy()
	token, e := c.NewInvitationToken(id, tokenMaterial)
	previewCheck(t, "ordinary invitation token", e)
	password, e := pgfixture.RandomHex(24)
	previewCheck(t, "ordinary password", e)
	material, e := sc.NewSecretMaterial([]byte(password))
	previewCheck(t, "ordinary password material", e)
	defer material.Destroy()
	anonymous, e := core.NewAnonymousContext(ctx)
	previewCheck(t, "ordinary anonymous context", e)
	defer anonymous.Cookie.Destroy()
	defer anonymous.CSRF.Destroy()
	browser, e := core.VerifyAnonymousContext(ctx, anonymous.Cookie, anonymous.CSRF)
	previewCheck(t, "ordinary browser", e)
	redeem, e := c.NewInvitationRedeem(c.RedeemFields{Browser: browser, Key: foundation.IdempotencyKey(guardID[struct{}](t).String()), Token: token, Username: "outbound-reader", DisplayName: "Outbound reader", Password: material, Confirmation: material})
	previewCheck(t, "ordinary redeem request", e)
	_, e = core.RedeemInvitation(ctx, redeem)
	previewCheck(t, "ordinary redeem", e)
	return previewCredential{Email: email, Password: password}
}

// This gateway is owned by the fixture and accepts CONNECT only to its own SMTP
// sink. It is not an external relay, and no runtime configuration selects it
// until a test explicitly saves that gateway through the administrator API.
func previewOutboundGateway(t *testing.T, ctx context.Context, smtp *smtpfixture.Fixture, endpoint smtpfixture.Endpoint) map[string]any {
	t.Helper()
	addresses, e := net.InterfaceAddrs()
	previewCheck(t, "proxy private interface", e)
	var host string
	for _, address := range addresses {
		prefix, e := netip.ParsePrefix(address.String())
		if e == nil && prefix.Addr().Is4() && outbound.Classify(prefix.Addr()) == outbound.Private {
			host = prefix.Addr().String()
			break
		}
	}
	if host == "" {
		t.Fatal("owned private gateway interface unavailable")
	}
	listener, e := net.Listen("tcp", net.JoinHostPort(host, "0"))
	previewCheck(t, "owned gateway listen", e)
	allowed := net.JoinHostPort(smtp.PrivateIP, strconv.Itoa(endpoint.Port))
	server := &http.Server{ReadHeaderTimeout: 2 * time.Second, ErrorLog: log.New(io.Discard, "", 0)}
	var mu sync.Mutex
	var calls sync.WaitGroup
	stopped := false
	active := make(map[net.Conn]bool)
	server.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		if stopped {
			mu.Unlock()
			return
		}
		calls.Add(1)
		mu.Unlock()
		defer calls.Done()
		if r.Method != http.MethodConnect || r.Host != allowed {
			http.Error(w, "owned target required", http.StatusForbidden)
			return
		}
		upstream, e := (&net.Dialer{Timeout: 2 * time.Second}).DialContext(r.Context(), "tcp", allowed)
		if e != nil {
			http.Error(w, "owned target unavailable", http.StatusBadGateway)
			return
		}
		downstream, rw, e := w.(http.Hijacker).Hijack()
		if e != nil {
			_ = upstream.Close()
			return
		}
		defer downstream.Close()
		defer upstream.Close()
		mu.Lock()
		if stopped {
			mu.Unlock()
			return
		}
		active[downstream], active[upstream] = true, true
		mu.Unlock()
		defer func() { mu.Lock(); delete(active, downstream); delete(active, upstream); mu.Unlock() }()
		_ = downstream.SetDeadline(time.Now().Add(20 * time.Second))
		_ = upstream.SetDeadline(time.Now().Add(20 * time.Second))
		if _, e = rw.WriteString("HTTP/1.1 200 Connection Established\r\n\r\n"); e != nil {
			return
		}
		if rw.Flush() != nil {
			return
		}
		joined := make(chan struct{})
		go func() { _, _ = io.Copy(upstream, rw); _ = upstream.Close(); close(joined) }()
		_, _ = io.Copy(downstream, upstream)
		_ = downstream.Close()
		<-joined
	})
	done := make(chan error, 1)
	go func() { done <- server.Serve(listener) }()
	t.Cleanup(func() {
		mu.Lock()
		stopped = true
		for conn := range active {
			_ = conn.Close()
		}
		mu.Unlock()
		_ = server.Close()
		calls.Wait()
		if e := <-done; !errors.Is(e, http.ErrServerClosed) {
			t.Error("owned gateway shutdown failed")
		}
	})
	port := listener.Addr().(*net.TCPAddr).Port
	return map[string]any{"protocol": "http", "host": host, "port": port}
}
