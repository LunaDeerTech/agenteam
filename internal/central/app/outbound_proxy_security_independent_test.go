//go:build integration

package app

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/.agent-state/project-variables-independent/commitproxy"
	"github.com/LunaDeerTech/agenteam/internal/central/account"
	"github.com/LunaDeerTech/agenteam/internal/central/config"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/outbound"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
	"github.com/LunaDeerTech/agenteam/internal/platform/logging"
	"github.com/jackc/pgx/v5/pgconn"
)

const independentProxyPath = "/api/v1/system/outbound-policy/proxy"

type independentProxyBrowser struct {
	app                         *modelRootApp
	address, cookie, csrf, user string
	client                      *http.Client
}

func independentProxyAwait(t *testing.T, ch <-chan struct{}, stage string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(8 * time.Second):
		t.Fatal("independent proxy stage did not join", stage)
	}
}

func independentProxyAddress(t *testing.T, a *modelRootApp) string {
	t.Helper()
	// Observe the actual root event within existing startup phase budgets.
	// The shared five-second test logger can expire while real PG migrates.
	timer := time.NewTimer(a.cfg.Database().StartupTimeout() + SecurityStartupTimeout)
	defer timer.Stop()
	for {
		select {
		case event := <-a.logs.events:
			if event["event"] == "listening" {
				address, ok := event["listen_address"].(string)
				if !ok || address == "" {
					t.Fatal("independent root listening address missing")
				}
				return "http://" + address
			}
		case <-a.done:
			t.Fatal("independent root exited before listening", a.err)
		case <-timer.C:
			t.Fatal("independent root exceeded original startup budgets")
		}
	}
}

func independentProxyLogin(t *testing.T, a *modelRootApp) *independentProxyBrowser {
	t.Helper()
	address := independentProxyAddress(t, a)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	login, err := fixtureAccountLoginResponse(ctx, a.cfg, a.core)
	if err != nil {
		t.Fatal("independent real login failed", err)
	}
	var material sc.SecretMaterial
	if err = login.UseCookie(func(raw []byte) error { var e error; material, e = sc.NewSecretMaterial(raw); return e }); err != nil {
		t.Fatal(err)
	}
	defer material.Destroy()
	if err = login.Close(ctx); err != nil {
		t.Fatal(err)
	}
	actor, err := a.core.Authenticate(ctx, material)
	if err != nil {
		t.Fatal(err)
	}
	session, err := a.core.GetSession(ctx, material)
	if err != nil {
		t.Fatal(err)
	}
	defer session.CSRF.Destroy()
	b := &independentProxyBrowser{app: a, address: address, user: actor.Details().UserID, client: &http.Client{Transport: &http.Transport{Proxy: nil}, Timeout: 8 * time.Second}}
	if err = material.Use(func(raw []byte) error { b.cookie = string(raw); return nil }); err != nil {
		t.Fatal(err)
	}
	if err = session.CSRF.Use(func(raw []byte) error { b.csrf = string(raw); return nil }); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { b.client.CloseIdleConnections(); b.cookie = ""; b.csrf = "" })
	return b
}

func (b *independentProxyBrowser) call(t *testing.T, method, path, key string, body any, mutate func(*http.Request), want int) []byte {
	t.Helper()
	var payload []byte
	var err error
	if body != nil {
		payload, err = json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
	}
	defer clear(payload)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, method, b.address+path, bytes.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	request.Host = "localhost:8080"
	request.Header.Set("Origin", "http://localhost:8080")
	request.AddCookie(&http.Cookie{Name: "agenteam_local_session", Value: b.cookie})
	if method != "GET" {
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("X-CSRF-Token", b.csrf)
		request.Header.Set("Idempotency-Key", key)
	}
	if mutate != nil {
		mutate(request)
	}
	response, err := b.client.Do(request)
	if err != nil {
		t.Fatal("independent real HTTP failed", err)
	}
	raw, readErr := io.ReadAll(io.LimitReader(response.Body, (1<<20)+1))
	closeErr := response.Body.Close()
	if readErr != nil || closeErr != nil || len(raw) > 1<<20 {
		t.Fatal("independent response incomplete")
	}
	if response.StatusCode != want {
		var fault struct {
			Code  string `json:"code"`
			State string `json:"commit_state"`
		}
		_ = json.Unmarshal(raw, &fault)
		t.Fatalf("independent status got=%d want=%d code=%s state=%s", response.StatusCode, want, fault.Code, fault.State)
	}
	if response.Header.Get("Cache-Control") != "no-store" {
		t.Fatal("proxy response missing no-store")
	}
	return raw
}

func independentProxyBody(version, mode, user, password, action string) map[string]any {
	body := map[string]any{"expected_version": version, "mode": mode, "gateway": map[string]any{"protocol": "http", "host": "10.245.23.17", "port": 3128, "username": user}, "credential_action": action}
	if password != "" {
		body["password"] = password
	}
	return body
}
func independentProxyVersion(t *testing.T, raw []byte, want string) {
	t.Helper()
	var response struct {
		Version string `json:"version"`
	}
	if json.Unmarshal(raw, &response) != nil || response.Version != want {
		t.Fatal("independent version mismatch")
	}
}
func independentProxyNoSecret(t *testing.T, raw []byte, markers ...string) {
	t.Helper()
	for _, marker := range markers {
		if bytes.Contains(raw, []byte(marker)) || bytes.Contains(raw, []byte(hex.EncodeToString([]byte(marker)))) {
			t.Fatal("proxy secret appeared in a public or unencrypted projection")
		}
	}
}

func TestIndependentOutboundProxyHTTPAuthorizationPersistence(t *testing.T) {
	a := newModelRootApp(t, "3s", nil)
	b := independentProxyLogin(t, a)
	b.call(t, "GET", independentProxyPath, "", nil, func(r *http.Request) { r.Header.Del("Cookie") }, 401)
	initial := b.call(t, "GET", independentProxyPath, "", nil, nil, 200)
	independentProxyVersion(t, initial, "1")
	marker := "independent-proxy-private-marker-" + guardID[struct{}](t).String()
	key := guardID[struct{}](t).String()
	body := independentProxyBody("1", "proxy", "owned-user", marker, "keep")
	b.call(t, "PUT", independentProxyPath, key, body, func(r *http.Request) { r.Header.Del("X-CSRF-Token") }, 403)
	independentProxyVersion(t, b.call(t, "GET", independentProxyPath, "", nil, nil, 200), "1")
	receipt := b.call(t, "PUT", independentProxyPath, key, body, nil, 200)
	independentProxyVersion(t, receipt, "2")
	current := b.call(t, "GET", independentProxyPath, "", nil, nil, 200)
	independentProxyNoSecret(t, current, marker)
	var settings outbound.ProxySettings
	if json.Unmarshal(current, &settings) != nil || settings.Gateway == nil || !settings.Gateway.CredentialPresent {
		t.Fatal("persisted gateway projection missing")
	}
	if !bytes.Equal(receipt, b.call(t, "PUT", independentProxyPath, key, body, nil, 200)) {
		t.Fatal("historical proxy receipt changed")
	}
	b.call(t, "PUT", independentProxyPath, guardID[struct{}](t).String(), body, nil, 409)
	changed := independentProxyBody("1", "proxy", "owned-user", marker+"-other", "keep")
	b.call(t, "PUT", independentProxyPath, key, changed, nil, 409)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var ref, configProjection, ciphertextProjection, auditProjection string
	if err := a.store.QueryRow(ctx, `SELECT credential_id::text,gateway::text FROM agenteam_outbound.proxy_settings`).Scan(&ref, &configProjection); err != nil {
		t.Fatal(err)
	}
	if err := a.store.QueryRow(ctx, `SELECT coalesce(string_agg(row_to_json(p)::text,''),'') FROM agenteam_secret.secret_payloads p`).Scan(&ciphertextProjection); err != nil {
		t.Fatal(err)
	}
	if err := a.store.QueryRow(ctx, `SELECT coalesce(string_agg(row_to_json(p)::text,''),'') FROM agenteam_audit.audit_records p`).Scan(&auditProjection); err != nil {
		t.Fatal(err)
	}
	independentProxyNoSecret(t, []byte(configProjection+ciphertextProjection+auditProjection+a.logs.String()), marker)
	var auditCount int
	if err := a.store.QueryRow(ctx, `SELECT count(*) FROM agenteam_audit.audit_records WHERE action='outbound.proxy.update'`).Scan(&auditCount); err != nil || auditCount != 1 {
		t.Fatal("denied/replayed proxy command changed audit count", err)
	}
	keep := independentProxyBody("2", "direct", "owned-user", "", "keep")
	independentProxyVersion(t, b.call(t, "PUT", independentProxyPath, guardID[struct{}](t).String(), keep, nil, 200), "3")
	var retained string
	if err := a.store.QueryRow(ctx, `SELECT credential_id::text FROM agenteam_outbound.proxy_settings`).Scan(&retained); err != nil || retained != ref {
		t.Fatal("blank password or direct mode replaced saved credential", err)
	}
	changedGateway := independentProxyBody("3", "direct", "other-user", "", "keep")
	b.call(t, "PUT", independentProxyPath, guardID[struct{}](t).String(), changedGateway, nil, 400)
	// A second real app startup reads the same task-owned database and keyrings.
	a.cancel()
	independentProxyAwait(t, a.done, "first app stop")
	restarted := independentProxyRestart(t, a)
	restored := independentProxyLogin(t, restarted)
	current = restored.call(t, "GET", independentProxyPath, "", nil, nil, 200)
	independentProxyVersion(t, current, "3")
	independentProxyNoSecret(t, current, marker)
	if json.Unmarshal(current, &settings) != nil || settings.Mode != "direct" || settings.Gateway == nil || !settings.Gateway.CredentialPresent {
		t.Fatal("restart lost saved proxy credential")
	}
	remove := independentProxyBody("3", "direct", "", "", "remove")
	independentProxyVersion(t, restored.call(t, "PUT", independentProxyPath, guardID[struct{}](t).String(), remove, nil, 200), "4")
	// Change only the current authority fact; use the same actual browser session.
	roleCtx, roleCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer roleCancel()
	if _, err := restarted.store.Exec(roleCtx, `UPDATE agenteam_account.users SET role='user' WHERE id=$1`, restored.user); err != nil {
		t.Fatal(err)
	}
	restored.call(t, "GET", independentProxyPath, "", nil, nil, 403)
	restored.call(t, "PUT", independentProxyPath, key, body, nil, 403)
}

func independentProxyRestart(t *testing.T, old *modelRootApp) *modelRootApp {
	t.Helper()
	a := &modelRootApp{db: old.db, cfg: old.cfg, store: &modelRootStore{}, logs: newEventLog(), signals: make(chan os.Signal, 4), done: make(chan struct{})}
	ctx, cancel := context.WithCancel(context.Background())
	a.cancel = cancel
	deps := dependencies{
		open: func(ctx context.Context, cfg postgres.Config) (database, error) {
			store, err := postgres.Open(ctx, cfg)
			if err != nil {
				return nil, err
			}
			a.store.Store = store
			return a.store, nil
		},
		bind: func(ctx context.Context, cfg config.Config, db database, owned *resources, d *dependencies) error {
			a.owned, a.store.owned = owned, owned
			return bindAccounts(ctx, cfg, db, owned, d)
		},
		observeAccount: func(core *account.Service) { a.core = core },
		listen: func(ctx context.Context, network, address string) (net.Listener, error) {
			return new(net.ListenConfig).Listen(ctx, network, address)
		},
	}
	logger, err := logging.New(logging.Central, slog.LevelInfo, a.logs)
	if err != nil {
		t.Fatal(err)
	}
	go func() { a.err = run(ctx, a.cfg, logger, a.signals, deps); close(a.done) }()
	t.Cleanup(func() { cancel(); independentProxyAwait(t, a.done, "restarted app stop") })
	return a
}

func TestIndependentOutboundProxyCrashLeaseRemoval(t *testing.T) {
	a := newModelRootApp(t, "3s", nil)
	b := independentProxyLogin(t, a)
	body := independentProxyBody("1", "proxy", "owned-user", "independent-orphan-private-marker", "keep")
	b.call(t, "PUT", independentProxyPath, guardID[struct{}](t).String(), body, nil, 200)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	// Reconstruct the committed acquisition state, bound to the original real
	// ProcessGuard. This is durable-state injection, not a killed process.
	objects, ok := a.owned.objects().(*objectAssembly)
	if !ok || objects.guard == nil {
		t.Fatal("independent original process guard missing")
	}
	process, err := objects.guard.CurrentProcess()
	if err != nil {
		t.Fatal(err)
	}
	lease, owner := guardID[struct{}](t).String(), guardID[struct{}](t).String()
	var operations bool
	if err := a.store.QueryRow(ctx, `SELECT to_regclass('agenteam_outbound.proxy_credential_operations') IS NOT NULL`).Scan(&operations); err != nil {
		t.Fatal(err)
	}
	if operations {
		_, err = a.store.Exec(ctx, `WITH acquired AS (INSERT INTO agenteam_secret.secret_leases(id,credential_id,scope,project_id,owner_kind,owner_id,consumer) SELECT $1,credential_id,'system',NULL,'outbound_proxy',$2,'system' FROM agenteam_outbound.proxy_settings RETURNING id,credential_id) INSERT INTO agenteam_outbound.proxy_credential_operations(operation_id,process_id,lease_id,credential_id,proxy_version) SELECT $2,$3,acquired.id,acquired.credential_id,p.version FROM acquired CROSS JOIN agenteam_outbound.proxy_settings p`, lease, owner, process.String())
	} else {
		// a37 predates the operation table. Its legitimate acquisition persists
		// this lease alone; the owner and lease IDs lived only in memory.
		_, err = a.store.Exec(ctx, `INSERT INTO agenteam_secret.secret_leases(id,credential_id,scope,project_id,owner_kind,owner_id,consumer) SELECT $1,credential_id,'system',NULL,'outbound_proxy',$2,'system' FROM agenteam_outbound.proxy_settings`, lease, owner)
	}
	if err != nil {
		t.Fatal(err)
	}
	// Match the real Object lifecycle boundary: its service joins and releases
	// the spool, while its separate ProcessGuard still holds the exact flock.
	// The second root must enter recovery, not merely fail to open the spool.
	objects.service.StopAdmission()
	drainCtx, drainCancel := context.WithTimeout(context.Background(), 10*time.Second)
	err = objects.service.Drain(drainCtx)
	drainCancel()
	if err != nil {
		t.Fatal("independent old Object service did not drain", err)
	}
	restarted := independentProxyRestart(t, a)
	restored := independentProxyLogin(t, restarted)
	newObjects, ok := restarted.owned.objects().(*objectAssembly)
	if !ok || newObjects.guard == nil {
		t.Fatal("independent restarted process guard missing")
	}
	probeCtx, probeCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer probeCancel()
	err = newObjects.guard.ConfirmStopped(probeCtx, process)
	var fault *f.Fault
	if !errors.As(err, &fault) || fault.Code != f.ResourceBusy {
		t.Fatal("original exact ProcessGuard was not still held", err)
	}
	runtime, ok := restarted.owned.outbound().(*outboundRuntime)
	if !ok {
		t.Fatal("restarted real outbound runtime missing")
	}
	// The interface keeps the same test input compilable on the old baseline,
	// which had no recovery API or durable operation table.
	recovery, hasRecovery := any(runtime.policy).(interface {
		RecoverProxyCredentials(context.Context) error
	})
	if operations && !hasRecovery {
		t.Fatal("durable operations have no actual recovery entry point")
	}
	if hasRecovery {
		if err := recovery.RecoverProxyCredentials(probeCtx); err != nil {
			t.Fatal("live foreign process recovery failed", err)
		}
	}
	var released bool
	if err := restarted.store.QueryRow(probeCtx, `SELECT released FROM agenteam_secret.secret_leases WHERE id=$1`, lease).Scan(&released); err != nil || released {
		t.Fatal("actual recovery reclaimed a live foreign lease", err)
	}
	if operations {
		var retained bool
		if err := restarted.store.QueryRow(probeCtx, `SELECT EXISTS(SELECT 1 FROM agenteam_outbound.proxy_credential_operations WHERE operation_id=$1 AND process_id=$2)`, owner, process.String()).Scan(&retained); err != nil || !retained {
			t.Fatal("actual recovery discarded a live foreign operation", err)
		}
	}
	a.cancel()
	independentProxyAwait(t, a.done, "original guard actual stop")
	stoppedCtx, stoppedCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer stoppedCancel()
	if err := newObjects.guard.ConfirmStopped(stoppedCtx, process); err != nil {
		t.Fatal("joined original ProcessGuard was not provably stopped", err)
	}
	if hasRecovery {
		if err := recovery.RecoverProxyCredentials(stoppedCtx); err != nil {
			t.Fatal("stopped foreign process recovery failed", err)
		}
		if err := restarted.store.QueryRow(stoppedCtx, `SELECT released FROM agenteam_secret.secret_leases WHERE id=$1`, lease).Scan(&released); err != nil || !released {
			t.Fatal("stopped process lease was not released", err)
		}
		var remaining int
		if err := restarted.store.QueryRow(stoppedCtx, `SELECT count(*) FROM agenteam_outbound.proxy_credential_operations WHERE operation_id=$1`, owner).Scan(&remaining); err != nil || remaining != 0 {
			t.Fatal("stopped process operation remained", err)
		}
	}
	removal := independentProxyBody("2", "direct", "", "", "remove")
	independentProxyVersion(t, restored.call(t, "PUT", independentProxyPath, guardID[struct{}](t).String(), removal, nil, 200), "3")

}

type independentProxyCommitStore struct {
	*modelRootStore
	proxy *commitproxy.Proxy
	armed atomic.Bool
}

func (s *independentProxyCommitStore) InTx(tx f.Tx) (postgres.SQLExecutor, error) {
	x, err := s.modelRootStore.InTx(tx)
	if err != nil {
		return nil, err
	}
	return independentProxyCommitSQL{x, s}, nil
}

type independentProxyCommitSQL struct {
	postgres.SQLExecutor
	owner *independentProxyCommitStore
}

func (x independentProxyCommitSQL) Exec(ctx context.Context, query string, args ...any) (pgconn.CommandTag, error) {
	if strings.HasPrefix(query, "UPDATE agenteam_outbound.proxy_settings SET") && x.owner.armed.CompareAndSwap(true, false) {
		var pid int32
		if err := x.SQLExecutor.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&pid); err != nil {
			return pgconn.CommandTag{}, err
		}
		if err := x.owner.proxy.Arm(pid); err != nil {
			return pgconn.CommandTag{}, err
		}
	}
	return x.SQLExecutor.Exec(ctx, query, args...)
}

func TestIndependentOutboundProxyUnknownReceiptRecovery(t *testing.T) {
	t.Run("original_replay", func(t *testing.T) { independentProxyUnknownReceiptRecovery(t, false) })
	t.Run("other_resource_save", func(t *testing.T) { independentProxyUnknownReceiptRecovery(t, true) })
}

func independentProxyUnknownReceiptRecovery(t *testing.T, otherSave bool) {
	var proxy *commitproxy.Proxy
	var interception *independentProxyCommitStore
	a := newModelRootApp(t, "3s", func(a *modelRootApp, d *dependencies) {
		var err error
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		proxy, err = commitproxy.New(ctx, net.JoinHostPort("127.0.0.1", a.db.Fixture.Port))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			proxy.Release()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := proxy.Close(ctx); err != nil {
				t.Error("independent commit proxy did not join")
			}
		})
		u, err := url.Parse(a.db.Fixture.URL(a.db.Name))
		if err != nil {
			t.Fatal(err)
		}
		u.Host = proxy.Address()
		cfg := a.db.Config(t, map[string]string{"URL": u.String(), "TLS_MODE": "disable", "CA_FILE": ""})
		interception = &independentProxyCommitStore{modelRootStore: a.store, proxy: proxy}
		d.open = func(ctx context.Context, _ postgres.Config) (database, error) {
			store, err := postgres.Open(ctx, cfg)
			if err != nil {
				return nil, err
			}
			a.store.Store = store
			return interception, nil
		}
	})
	b := independentProxyLogin(t, a)
	runtime, ok := a.owned.outbound().(*outboundRuntime)
	if !ok {
		t.Fatal("real root outbound missing")
	}
	key := guardID[struct{}](t).String()
	body := map[string]any{"expected_version": "1", "mode": "direct", "gateway": nil, "credential_action": "remove"}
	interception.armed.Store(true)
	response := b.call(t, "PUT", independentProxyPath, key, body, nil, 503)
	var failure struct {
		State string `json:"commit_state"`
	}
	if json.Unmarshal(response, &failure) != nil || failure.State != "unknown" {
		t.Fatal("real lost COMMIT was not unknown")
	}
	independentProxyAwait(t, proxy.Reached(), "complete original COMMIT")
	if runtime.policy.Status().Available {
		t.Fatal("unknown proxy commit left outbound available")
	}
	if otherSave {
		rules := map[string]any{"expected_version": "1", "rules": []any{}}
		b.call(t, "PUT", "/api/v1/system/outbound-policy", guardID[struct{}](t).String(), rules, nil, 200)
		if runtime.policy.Status().Available {
			t.Error("other resource save restored unknown proxy mirror")
		}
	}

	proxy.Release()
	independentProxyAwait(t, proxy.Committed(), "original durable COMMIT")
	independentProxyAwait(t, proxy.HeldJoined(), "original writer join")
	receipt := b.call(t, "PUT", independentProxyPath, key, body, nil, 200)
	independentProxyVersion(t, receipt, "2")
	status := runtime.policy.Status()
	if !status.Available {
		t.Fatal("confirmed original command left outbound unavailable")
	}
	if otherSave && (status.Version == nil || *status.Version != 2) {
		t.Fatal("recovery used a stale rules snapshot")
	}
	independentProxyVersion(t, b.call(t, "GET", independentProxyPath, "", nil, nil, 200), "2")
}
