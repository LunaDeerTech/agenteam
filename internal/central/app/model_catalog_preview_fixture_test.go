//go:build integration

package app

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"io"
	"log"
	"log/slog"
	"net"
	"net/http"
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
	"github.com/LunaDeerTech/agenteam/internal/central/config"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/outbound"
	"github.com/LunaDeerTech/agenteam/internal/platform/logging"
	pgfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/postgres"
	smtpfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/smtp"
	"github.com/jackc/pgx/v5/pgconn"
)

// This fixture uses the production App/PG/Secret and browser boundary. Only
// DNS for its two owned targets and their remote protocol servers are controlled.
// It cannot route to public providers or reuse a deployment database.
func TestModelCatalogPreviewFixture(t *testing.T) {
	if os.Getenv("AGENTEAM_MODEL_CATALOG_PREVIEW") != "1" {
		t.Skip("explicit owned model catalog preview required")
	}
	private := os.Getenv("AGENTEAM_MODEL_CATALOG_DIRECTORY")
	info, err := os.Lstat(private)
	if !filepath.IsAbs(private) || err != nil || !info.IsDir() || info.Mode().Perm() != 0700 {
		t.Fatal("owned model preview requires a private directory")
	}
	actual, err := filepath.EvalSymlinks(private)
	if err != nil || actual != private {
		t.Fatal("owned model directory must not use symlinks")
	}
	if _, err = os.Lstat(filepath.Join(private, "runtime.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("owned model directory already contains runtime material")
	}
	nonce, err := pgfixture.RandomHex(16)
	previewCheck(t, "model nonce", err)
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
				if _, e := os.Stat(filepath.Join(private, "stop")); e == nil {
					cancel()
					return
				}
			}
		}
	}()
	t.Cleanup(func() { cancel(); <-watchDone })

	setup, stopSetup := context.WithTimeout(ctx, 60*time.Second)
	smtp, err := smtpfixture.Start(setup)
	stopSetup()
	previewCheck(t, "model owned SMTP", err)
	t.Cleanup(func() {
		cleanup, stop := context.WithTimeout(context.Background(), 20*time.Second)
		defer stop()
		previewCheck(t, "model SMTP cleanup", smtp.Close(cleanup))
	})
	sink, err := smtp.Create(ctx, smtpfixture.Scenario{Mode: "starttls", CaptureMessage: true})
	previewCheck(t, "model SMTP sink", err)
	providerDirectory := filepath.Join(private, "provider")
	previewCheck(t, "model provider directory", os.Mkdir(providerDirectory, 0700))
	providerTLS := previewCertificate(t, providerDirectory, "models.fixture.test")
	provider := startModelCatalogProvider(t, providerTLS)
	trust, err := os.ReadFile(providerTLS.CAFile)
	previewCheck(t, "model CA", err)
	smtpTrust, err := os.ReadFile(smtp.CAFile)
	previewCheck(t, "SMTP CA", err)
	trustFile := filepath.Join(private, "outbound-ca.pem")
	previewCheck(t, "owned outbound trust", os.WriteFile(trustFile, append(trust, smtpTrust...), 0600))
	security := previewCertificate(t, private, "localhost")
	const origin = "https://localhost:5283"
	listener, err := net.Listen("tcp", "127.0.0.1:5283")
	previewCheck(t, "model owned TLS listener", err)
	t.Cleanup(func() { _ = listener.Close() })
	db := pgfixture.NewDatabase(t)
	cfg := guardConfiguration(t, append(guardEnvironment(t, db),
		"AGENTEAM_CENTRAL_PUBLIC_ORIGIN="+origin,
		"AGENTEAM_CENTRAL_HTTP_ADDR=127.0.0.1:5282",
		"AGENTEAM_CENTRAL_DEV_MODE=false",
		"AGENTEAM_CENTRAL_SHUTDOWN_TIMEOUT=5s",
		"AGENTEAM_CENTRAL_OUTBOUND_CA_FILE="+trustFile))
	var core *account.Service
	var transport *outboundRuntime
	deps := dependencies{
		smtpResolver:   previewSMTPResolver{netip.MustParseAddr(smtp.PrivateIP)},
		observeAccount: func(v *account.Service) { core = v },
		outboundConstruct: func(ctx context.Context, cfg config.Config, v *outboundRuntime) error {
			transport = v
			if e := v.client.ForceClose(ctx); e != nil {
				return e
			}
			v.resolver = modelCatalogResolver{smtpAddress: netip.MustParseAddr(smtp.PrivateIP)}
			var e error
			v.client, e = outbound.NewClient(v.policy, cfg.OutboundTrust(), v.resolver)
			return e
		},
	}
	output := newEventLog()
	logger, err := logging.New(logging.Central, slog.LevelInfo, output)
	previewCheck(t, "model logger", err)
	rootCtx, stopRoot := context.WithCancel(ctx)
	rootDone := make(chan struct{})
	var rootResult error
	go func() { rootResult = run(rootCtx, cfg, logger, nil, deps); close(rootDone) }()
	t.Cleanup(func() {
		stopRoot()
		select {
		case <-rootDone:
			if rootResult != nil {
				modelCatalogStartupFailure(t, private, output, rootResult, db)
			}
			previewCheck(t, "model App joined", rootResult)
		case <-time.After(7 * time.Second):
			t.Error("owned model App did not join within its shutdown budget")
		}
	})
	backend, err := url.Parse(workRootStartupAddress(t, output, rootDone, func() error { return rootResult }))
	previewCheck(t, "model App address", err)
	if core == nil || transport == nil || backend.Host != "127.0.0.1:5282" || cfg.DevMode() {
		t.Fatal("owned production model composition missing")
	}
	previewConfigureSMTP(t, ctx, cfg, core, transport, smtp, sink)
	regular := previewOutboundRegular(t, ctx, cfg, core, smtp, sink)
	actor, err := fixtureAccountActor(ctx, cfg, core)
	previewCheck(t, "model policy actor", err)
	ports, err := outbound.SelectedPorts(uint16(provider.port))
	previewCheck(t, "model exact provider port", err)
	rule, err := outbound.NewRule("127.0.0.1/32", ports)
	previewCheck(t, "model owned target rule", err)
	rules, err := outbound.NewRules(rule)
	previewCheck(t, "model rule set", err)
	key, err := foundation.NewCommandIdentity("outbound-policy-v2", []string{actor.Details().UserID}, "update", foundation.IdempotencyKey(guardID[struct{}](t).String()))
	previewCheck(t, "model policy command", err)
	_, err = transport.policy.UpdatePolicy(ctx, actor, outbound.CommandMeta{Identity: key, ExpectedVersion: *transport.policy.Status().Version}, outbound.Allowlist, rules)
	previewCheck(t, "model target allowlist", err)
	credentials := previewBootstrapCredentials(t, cfg.AccountRecoveryLog())
	frontend, _ := url.Parse("http://127.0.0.1:5281")
	apiProxy, webProxy := httputil.NewSingleHostReverseProxy(backend), httputil.NewSingleHostReverseProxy(frontend)
	proxyTransport := &http.Transport{Proxy: nil}
	t.Cleanup(proxyTransport.CloseIdleConnections)
	for _, proxy := range []*httputil.ReverseProxy{apiProxy, webProxy} {
		proxy.Transport = proxyTransport
		proxy.ErrorLog = log.New(io.Discard, "", 0)
		proxy.ErrorHandler = func(w http.ResponseWriter, _ *http.Request, _ error) {
			http.Error(w, "owned upstream unavailable", 502)
		}
	}
	server := &http.Server{ReadHeaderTimeout: 5 * time.Second, TLSConfig: &tls.Config{Certificates: []tls.Certificate{security.Certificate}, MinVersion: tls.VersionTLS12}, ErrorLog: log.New(io.Discard, "", 0)}
	server.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Host != "localhost:5283" {
			w.WriteHeader(http.StatusMisdirectedRequest)
			return
		}
		if r.URL.Path == "/__fixture__/identity" && r.Method == http.MethodGet {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Cache-Control", "no-store")
			_ = json.NewEncoder(w).Encode(map[string]string{"nonce": nonce})
			return
		}
		if r.URL.Path == "/__fixture__/provider" {
			if r.Method != http.MethodGet || r.Header.Get("X-Fixture-Nonce") != nonce {
				w.WriteHeader(http.StatusForbidden)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(provider.observations())
			return
		}
		if r.URL.Path == "/api/v1" || strings.HasPrefix(r.URL.Path, "/api/v1/") {
			apiProxy.ServeHTTP(w, r)
			return
		}
		webProxy.ServeHTTP(w, r)
	})
	serverDone := make(chan error, 1)
	go func() { serverDone <- server.ServeTLS(listener, "", "") }()
	t.Cleanup(func() {
		cleanup, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		if e := server.Shutdown(cleanup); e != nil {
			_ = server.Close()
			t.Error("model TLS shutdown failed")
		}
		if e := <-serverDone; !errors.Is(e, http.ErrServerClosed) {
			t.Error("model TLS server did not join")
		}
	})
	previewFirstChain(t, ctx, origin, listener.Addr().String(), security.CA, credentials)
	manifest := map[string]any{
		"test_scope": "system-model-catalog", "nonce": nonce, "origin": origin,
		"certificate_spki": security.SPKI, "certificate_file": security.CertificateFile, "certificate_ca_file": security.CAFile,
		"local_tls_address": listener.Addr().String(), "credentials": credentials, "regular_credentials": regular,
		"expires_at": time.Now().UTC().Add(40 * time.Minute), "stop_file": filepath.Join(private, "stop"),
		"provider_base_url": "https://models.fixture.test:" + strconv.Itoa(provider.port) + "/v1", "provider_credential": provider.key,
		"database_fixture": os.Getenv(pgfixture.Env), "database_name": db.Name,
		"owned_resources": map[string]any{
			"postgres_descriptor": os.Getenv(pgfixture.Env),
			"object_descriptor":   os.Getenv("AGENTEAM_OBJECT_FIXTURE"),
			"outbound_descriptor": os.Getenv("AGENTEAM_OUTBOUND_FIXTURE"),
			"smtp_container_id":   smtp.ContainerID, "smtp_network_id": smtp.NetworkID,
			"smtp_nonce": smtp.Nonce, "smtp_directory": filepath.Dir(smtp.CAFile),
		},
	}
	for name, value := range map[string]any{
		"runtime.json":     manifest,
		"model-owned.json": map[string]string{"test_scope": "system-model-catalog", "nonce": nonce, "runtime_file": filepath.Join(private, "runtime.json")},
	} {
		raw, e := json.Marshal(value)
		previewCheck(t, "model manifest encoding", e)
		file := filepath.Join(private, name)
		previewCheck(t, "model manifest publication", os.WriteFile(file, raw, 0600))
		clear(raw)
		t.Cleanup(func() { _ = os.Remove(file) })
	}
	t.Log("owned model preview ready: real App/bootstrap/PG/Secret, strict TLS, ordinary account and controlled provider")
	select {
	case <-ctx.Done():
	case <-rootDone:
		t.Fatal("model App exited before owned preview ended")
	case e := <-serverDone:
		serverDone <- e
		t.Fatal("model TLS exited before owned preview ended")
	}
}

// Startup failures must retain the first dependency boundary without persisting
// the logger's arbitrary fields, credentials, connection strings or SQL text.
func modelCatalogStartupFailure(t *testing.T, private string, output *eventLog, failure error, db *pgfixture.Database) {
	t.Helper()
	record := map[string]any{}
	var fault *foundation.Fault
	if errors.As(failure, &fault) {
		record["code"], record["commit_state"] = fault.Code.Safe(), fault.CommitState.Safe()
	}
	var pgError *pgconn.PgError
	if errors.As(failure, &pgError) {
		record["sqlstate"] = pgError.Code
	}
	var events []map[string]any
	for _, line := range strings.Split(output.String(), "\n") {
		var event map[string]any
		if json.Unmarshal([]byte(line), &event) != nil {
			continue
		}
		projected := map[string]any{}
		for _, key := range []string{"event", "stage", "phase", "component", "code", "error_code", "state"} {
			if value, ok := event[key].(string); ok && len(value) <= 96 && !strings.ContainsAny(value, " \t\r\n:@/?=\\") {
				projected[key] = value
			}
		}
		if len(projected) != 0 {
			events = append(events, projected)
		}
	}
	record["events"] = events
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	conn, err := db.Fixture.Connect(ctx, db.Name)
	if err == nil {
		defer conn.Close(ctx)
		var table bool
		var schema int64
		err = conn.QueryRow(ctx, `SELECT to_regclass('agenteam_model.management_operations') IS NOT NULL,
 COALESCE((SELECT max(version_id) FROM agenteam_meta.goose_db_version WHERE is_applied),0)`).Scan(&table, &schema)
		if err == nil {
			record["management_table_exists"], record["schema"] = table, schema
		}
	}
	record["schema_observation_ok"] = err == nil
	if raw, err := json.Marshal(record); err == nil {
		if os.WriteFile(filepath.Join(private, "startup-failure-safe.json"), raw, 0600) != nil {
			t.Error("owned safe startup diagnostic publication failed")
		}
		t.Logf("owned model startup safe diagnostic: %s", raw)
	}
}

type modelCatalogResolver struct{ smtpAddress netip.Addr }

func (r modelCatalogResolver) Lookup(_ context.Context, host string) ([]netip.Addr, error) {
	switch host {
	case "models.fixture.test":
		return []netip.Addr{netip.MustParseAddr("127.0.0.1")}, nil
	case smtpfixture.Host:
		return []netip.Addr{r.smtpAddress}, nil
	default:
		return nil, errors.New("owned model fixture refuses other DNS targets")
	}
}

type modelCatalogProvider struct {
	mu                                               sync.Mutex
	port                                             int
	key                                              string
	requests, authorized, discovery, chat, embedding int
}

func (p *modelCatalogProvider) observations() map[string]int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return map[string]int{"requests": p.requests, "authorized": p.authorized, "discovery": p.discovery, "chat": p.chat, "embedding": p.embedding}
}
func startModelCatalogProvider(t *testing.T, security previewTLS) *modelCatalogProvider {
	t.Helper()
	key, err := pgfixture.RandomHex(24)
	previewCheck(t, "model provider credential", err)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	previewCheck(t, "model provider listener", err)
	p := &modelCatalogProvider{port: listener.Addr().(*net.TCPAddr).Port, key: key}
	server := &http.Server{ReadHeaderTimeout: 5 * time.Second, TLSConfig: &tls.Config{Certificates: []tls.Certificate{security.Certificate}, MinVersion: tls.VersionTLS12}, ErrorLog: log.New(io.Discard, "", 0)}
	server.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p.mu.Lock()
		p.requests++
		authorized := r.Header.Get("Authorization") == "Bearer "+p.key
		if authorized {
			p.authorized++
		}
		p.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if !authorized {
			w.WriteHeader(401)
			_, _ = io.WriteString(w, `{"error":{"message":"owned credential rejected"}}`)
			return
		}
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/models":
			p.mu.Lock()
			p.discovery++
			p.mu.Unlock()
			_, _ = io.WriteString(w, `{"object":"list","data":[{"id":"owned-chat","object":"model"},{"id":"owned-embedding","object":"model"}]}`)
		case r.Method == http.MethodPost && r.URL.Path == "/v1/chat/completions":
			var input struct {
				Model string `json:"model"`
			}
			if json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&input) != nil {
				w.WriteHeader(400)
				return
			}
			p.mu.Lock()
			p.chat++
			p.mu.Unlock()
			if input.Model == "owned-rejected" {
				w.WriteHeader(429)
				_, _ = io.WriteString(w, `{"error":{"message":"owned bounded rejection"}}`)
				return
			}
			_, _ = io.WriteString(w, `{"id":"owned-completion","object":"chat.completion","model":"owned-chat","choices":[{"index":0,"message":{"role":"assistant","content":"owned model response"},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":4,"total_tokens":7}}`)
		case r.Method == http.MethodPost && r.URL.Path == "/v1/embeddings":
			var input struct {
				Model          string `json:"model"`
				EncodingFormat string `json:"encoding_format"`
			}
			if json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&input) != nil || input.EncodingFormat != "float" {
				w.WriteHeader(400)
				return
			}
			p.mu.Lock()
			p.embedding++
			p.mu.Unlock()
			_, _ = io.WriteString(w, `{"object":"list","model":"owned-embedding","data":[{"object":"embedding","index":0,"embedding":[0.25,-0.5,0.75]}],"usage":{"prompt_tokens":3,"total_tokens":3}}`)
		default:
			w.WriteHeader(404)
			_, _ = io.WriteString(w, `{"error":{"message":"owned route unavailable"}}`)
		}
	})
	done := make(chan error, 1)
	go func() { done <- server.ServeTLS(listener, "", "") }()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if e := server.Shutdown(ctx); e != nil {
			_ = server.Close()
			t.Error("model provider shutdown failed")
		}
		if e := <-done; !errors.Is(e, http.ErrServerClosed) {
			t.Error("model provider server did not join")
		}
	})
	return p
}
