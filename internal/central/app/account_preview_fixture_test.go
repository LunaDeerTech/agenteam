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
	"strings"
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
	const origin = "https://localhost:5250"
	listener, err := net.Listen("tcp", "127.0.0.1:5250")
	previewCheck(t, "owned TLS listener", err)
	t.Cleanup(func() { _ = listener.Close() })
	certificate, spki, certFile := previewCertificate(t, private)

	// PG certificates are deliberately short lived. End this owned round before
	// their one-hour expiry; a subsequent preview creates fresh resources.
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Minute)
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
	frontend, _ := url.Parse("http://127.0.0.1:5245")
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
		TLSConfig:         &tls.Config{Certificates: []tls.Certificate{certificate}, MinVersion: tls.VersionTLS12},
		ErrorLog:          log.New(io.Discard, "", 0),
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Host != "localhost:5250" {
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
	previewFirstChain(t, ctx, origin, certificate, credentials)
	manifest := map[string]any{
		"origin": origin, "certificate_spki": spki, "certificate_file": certFile,
		"nonce": nonce, "credentials": credentials, "recovery_log": cfg.AccountRecoveryLog(),
		"stop_file": filepath.Join(private, "stop"), "expires_at": time.Now().Add(44 * time.Minute).UTC(),
		"smtp_control": "http://" + net.JoinHostPort(smtp.PrivateIP, "9000"), "smtp_nonce": smtp.Nonce,
		"smtp_endpoint_id": endpoint.ID, "database_fixture": os.Getenv(pgfixture.Env), "database_name": db.Name,
	}
	raw, err := json.Marshal(manifest)
	previewCheck(t, "private manifest encoding", err)
	defer clear(raw)
	previewCheck(t, "private manifest", os.WriteFile(filepath.Join(private, "runtime.json"), raw, 0600))
	t.Cleanup(func() { _ = os.Remove(filepath.Join(private, "runtime.json")) })
	t.Log("preview ready: production bootstrap/login/session/me, TLS and Secure Cookies verified; controlled SMTP configured")
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

func previewConfigureSMTP(t *testing.T, ctx context.Context, cfg config.Config, core *account.Service, transport *outboundRuntime, smtp *smtpfixture.Fixture, endpoint smtpfixture.Endpoint) {
	t.Helper()
	actor, err := fixtureAccountActor(ctx, cfg, core)
	previewCheck(t, "SMTP setup real login", err)
	ports, err := outbound.SelectedPorts(uint16(endpoint.Port))
	previewCheck(t, "SMTP exact port", err)
	rule, err := outbound.NewRule(smtp.PrivateIP+"/32", ports, false)
	previewCheck(t, "SMTP exact private rule", err)
	rules, err := outbound.NewRules(rule)
	previewCheck(t, "SMTP rules", err)
	key, err := foundation.NewCommandIdentity("outbound-policy", []string{actor.Details().UserID}, "update", foundation.IdempotencyKey(guardID[struct{}](t).String()))
	previewCheck(t, "SMTP policy identity", err)
	_, err = transport.policy.UpdatePolicy(ctx, actor, outbound.CommandMeta{Identity: key, ExpectedVersion: *transport.policy.Status().Version}, rules)
	previewCheck(t, "SMTP policy", err)
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

func previewCertificate(t *testing.T, directory string) (tls.Certificate, string, string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	previewCheck(t, "TLS key", err)
	now := time.Now()
	template := &x509.Certificate{SerialNumber: big.NewInt(now.UnixNano()), Subject: pkix.Name{CommonName: "Owned account preview"}, DNSNames: []string{"localhost"}, IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}, NotBefore: now.Add(-time.Minute), NotAfter: now.Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
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
	sum := sha256.Sum256(parsed.RawSubjectPublicKeyInfo)
	return certificate, base64.StdEncoding.EncodeToString(sum[:]), path
}

func previewFirstChain(t *testing.T, ctx context.Context, origin string, certificate tls.Certificate, credential previewCredential) {
	t.Helper()
	trust := x509.NewCertPool()
	trust.AddCert(certificate.Leaf)
	transport := &http.Transport{Proxy: nil, TLSClientConfig: &tls.Config{RootCAs: trust, MinVersion: tls.VersionTLS12}}
	defer transport.CloseIdleConnections()
	jar, err := cookiejar.New(nil)
	previewCheck(t, "Cookie jar", err)
	client := &http.Client{Transport: transport, Jar: jar, Timeout: 20 * time.Second}
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
		}
		if want == http.StatusNoContent {
			return nil
		}
		var result map[string]any
		previewCheck(t, "HTTP response decoding", json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&result))
		return result
	}
	bootstrap := request(http.MethodGet, "/api/v1/auth/bootstrap", nil, "", http.StatusOK)
	csrf, ok := bootstrap["csrf_token"].(string)
	if !ok || csrf == "" {
		t.Fatal("anonymous CSRF missing")
	}
	request(http.MethodPost, "/api/v1/sessions/login", credential, csrf, http.StatusOK)
	session := request(http.MethodGet, "/api/v1/session", nil, "", http.StatusOK)
	request(http.MethodGet, "/api/v1/me", nil, "", http.StatusOK)
	csrf, ok = session["csrf_token"].(string)
	if !ok || csrf == "" {
		t.Fatal("session CSRF missing")
	}
	request(http.MethodPost, "/api/v1/sessions/logout", struct{}{}, csrf, http.StatusNoContent)
}
