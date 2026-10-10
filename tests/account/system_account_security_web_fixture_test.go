//go:build integration

package account_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/app"
	"github.com/LunaDeerTech/agenteam/internal/central/config"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/platform/logging"
	"github.com/LunaDeerTech/agenteam/tests/testsupport/accountenv"
	objectfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/objectstore"
	pgfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/postgres"
)

// This fixture owns only Account settings controls. Its root and helper methods
// consume the accepted same-origin Account/Secret/Model composition.
type accountSecurityWebAttempt struct {
	key        string
	body, csrf [32]byte
}
type accountSecurityWebFixture struct {
	*modelsWebFixture
	singletonID                        string
	attempts                           []accountSecurityWebAttempt
	lostBody, lostCSRF                 [32]byte
	lostSettings                       map[string]any
	sameOriginal                       bool
	secondClient                       *http.Client
	secondCSRF                         string
	oldSessionID, oldIdle, oldAbsolute string
}

func newAccountSecurityWebRoot(t *testing.T, ctx context.Context, own *accountSecurityWebFixture) *authenticationWebFixture {
	t.Helper()
	db := pgfixture.NewDatabase(t)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal("owned frontend listener unavailable")
	}
	t.Cleanup(func() { _ = listener.Close() })
	origin := "http://" + listener.Addr().String()
	inputs := accountenv.New(t)
	objects, err := objectfixture.Environment(ctx, db.Name)
	if err != nil {
		t.Fatal("owned object fixture unavailable")
	}
	values := inputs.Values()
	for _, v := range objects {
		k, value, _ := strings.Cut(v, "=")
		values[k] = value
	}
	for k, v := range map[string]string{
		"DATABASE_URL": db.Fixture.URL(db.Name), "DATABASE_CA_FILE": db.Fixture.CAFile,
		"DATABASE_STARTUP_TIMEOUT": "15s", "HTTP_ADDR": "127.0.0.1:0", "PUBLIC_ORIGIN": origin, "SHUTDOWN_TIMEOUT": "2s",
		"CURSOR_KEYRING":                 `{"format":1,"current_kid":"test","keys":[{"kid":"test","key_b64":"AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8="}]}`,
		"SECRET_KEYRING":                 `{"format":1,"current_version":"1","keys":[{"version":"1","key_b64":"ICEiIyQlJicoKSorLC0uLzAxMjM0NTY3ODk6Ozw9Pj8="}]}`,
		"KNOWLEDGE_CONFIRMATION_KEYRING": `{"format":1,"current_kid":"knowledge","keys":[{"kid":"knowledge","key_b64":"gIGCg4SFhoeIiYqLjI2Oj5CRkpOUlZaXmJmam5ydnp8="}]}`,
	} {
		values[config.Prefix+k] = v
	}
	var environment []string
	for k, v := range values {
		environment = append(environment, k+"="+v)
	}
	cfg, err := config.Load(func(k string) (string, bool) { v, ok := values[k]; return v, ok }, environment)
	if err != nil {
		t.Fatal("owned root configuration invalid")
	}
	output := &httpFixtureLog{listening: make(chan string, 1)}
	logger, err := logging.New(logging.Central, slog.LevelInfo, output)
	if err != nil {
		t.Fatal(err)
	}
	rootCtx, cancel := context.WithCancel(ctx)
	rootDone := make(chan error, 1)
	go func() { rootDone <- app.Run(rootCtx, cfg, logger, nil) }()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-rootDone:
			if err != nil {
				t.Error("full Account/Secret/Model root shutdown failed", err)
			}
		case <-time.After(5 * time.Second):
			t.Error("full root did not join within its original shutdown budget")
		}
	})
	var address string
	select {
	case address = <-output.listening:
	case err := <-rootDone:
		rootDone <- err
		t.Fatal("full root initialization failed", err)
	case <-ctx.Done():
		t.Fatal("shared browser budget ended during root startup")
	}
	backend, err := url.Parse("http://" + address)
	if err != nil {
		t.Fatal(err)
	}
	transport := &http.Transport{Proxy: nil}
	proxy := httputil.NewSingleHostReverseProxy(backend)
	proxy.Transport = transport // NewSingleHostReverseProxy preserves the original Host.
	proxy.ModifyResponse = own.controlResponse
	proxy.ErrorHandler = func(w http.ResponseWriter, _ *http.Request, err error) {
		var lost *modelsResponseLost
		if !errors.As(err, &lost) {
			http.Error(w, "owned API unavailable", 502)
			return
		}
		defer clear(lost.prefix)
		for name, values := range lost.header {
			w.Header()[name] = append([]string(nil), values...)
		}
		w.Header().Del("Transfer-Encoding")
		w.Header().Set("Content-Length", strconv.Itoa(lost.length))
		w.Header().Set("Connection", "close")
		w.WriteHeader(200)
		_, _ = w.Write(lost.prefix)
		_ = http.NewResponseController(w).Flush()
		if hijacker, ok := w.(http.Hijacker); ok {
			if conn, _, e := hijacker.Hijack(); e == nil {
				_ = conn.Close()
			}
		}
	}
	root, err := filepath.Abs("../../web/dist")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(filepath.Join(root, "index.html")); err != nil {
		t.Fatal("formal production dist must be built before browser execution")
	}
	assets := http.FileServer(http.Dir(root))
	server := &http.Server{ReadHeaderTimeout: 5 * time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" && r.URL.Path == "/api/v1/session" && own.failSession.CompareAndSwap(true, false) {
			own.sessionFailures.Add(1)
			http.Error(w, "owned bounded session read unavailable", 503)
			return
		}
		if r.Method == "GET" && own.shouldFailRead(r.URL.Path) {
			own.readFailures.Add(1)
			http.Error(w, "owned exact configuration read unavailable", 503)
			return
		}
		if r.Method == "PUT" && r.URL.Path == "/api/v1/system/account-settings" {
			raw, err := io.ReadAll(io.LimitReader(r.Body, 16385))
			closeErr := r.Body.Close()
			defer clear(raw)
			if err != nil || closeErr != nil || len(raw) > 16384 {
				http.Error(w, "owned write observation unavailable", 400)
				return
			}
			own.mu.Lock()
			own.attempts = append(own.attempts, accountSecurityWebAttempt{key: r.Header.Get("Idempotency-Key"), body: sha256.Sum256(raw), csrf: sha256.Sum256([]byte(r.Header.Get("X-CSRF-Token")))})
			own.mu.Unlock()
			r.Body = io.NopCloser(bytes.NewReader(raw))
		}

		if r.URL.Path == "/api/v1" || strings.HasPrefix(r.URL.Path, "/api/v1/") {
			proxy.ServeHTTP(w, r)
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "method unavailable", 405)
			return
		}
		clean := filepath.Clean("/" + r.URL.Path)
		if info, e := os.Stat(filepath.Join(root, clean)); e == nil && !info.IsDir() {
			assets.ServeHTTP(w, r)
			return
		}
		if strings.HasPrefix(clean, "/assets/") || filepath.Ext(clean) != "" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		http.ServeFile(w, r, filepath.Join(root, "index.html"))
	})}
	serverDone := make(chan error, 1)
	go func() { serverDone <- server.Serve(listener) }()
	t.Cleanup(func() {
		shutdown, stop := context.WithTimeout(context.Background(), 2*time.Second)
		defer stop()
		if err := server.Shutdown(shutdown); err != nil {
			t.Error("frontend server did not drain")
			_ = server.Close()
		}
		if err := <-serverDone; !errors.Is(err, http.ErrServerClosed) {
			t.Error("owned frontend server failed", err)
		}
		transport.CloseIdleConnections()
	})
	runtimeRoot := os.Getenv("AGENTEAM_AUTH_WEB_RUNTIME")
	if !filepath.IsAbs(runtimeRoot) || len(runtimeRoot) > 45 {
		t.Fatal("explicit short task-owned browser runtime is required")
	}
	directory, err := os.MkdirTemp(runtimeRoot, "run-")
	if err != nil {
		t.Fatal("private browser runtime creation failed")
	}
	if err := os.Chmod(directory, 0700); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(directory); err != nil {
			t.Error("private browser runtime cleanup failed")
		}
		if _, err := os.Stat(directory); !errors.Is(err, os.ErrNotExist) {
			t.Error("private browser runtime still exists")
		}
	})
	old := &httpFixture{t: t, db: db, config: cfg, address: address, log: output}
	entry := old.record("bootstrap", "admin@mail.com", "")
	material, err := json.Marshal(map[string]string{"email": entry.Email, "password": entry.Password})
	if err != nil {
		t.Fatal("private bootstrap encoding failed")
	}
	defer clear(material)
	if err := os.WriteFile(filepath.Join(directory, "credentials.json"), material, 0600); err != nil {
		t.Fatal("private bootstrap transfer failed")
	}
	f := &authenticationWebFixture{t: t, db: db, origin: origin, directory: directory, webRoot: root, entry: entry, log: output, record: old.record, recoveryLogPath: cfg.AccountRecoveryLog()}
	t.Cleanup(func() {
		if output.contains(entry.Password) {
			t.Error("bootstrap material escaped the restricted recovery log")
		}
	})
	return f
}

func newAccountSecurityWebFixture(t *testing.T, ctx context.Context, mode string) *accountSecurityWebFixture {
	t.Helper()
	f := &accountSecurityWebFixture{modelsWebFixture: &modelsWebFixture{mode: mode, ids: map[string]string{}}}
	f.authenticationWebFixture = newAccountSecurityWebRoot(t, ctx, f)
	f.setup = &personalWebFixture{authenticationWebFixture: f.authenticationWebFixture}
	f.admin = personalWebCredential{Email: f.entry.Email, Password: f.entry.Password, UserID: f.entry.ID}
	f.adminClient, f.csrf, _ = f.loginClient(ctx)
	initial := f.request(ctx, "GET", "/api/v1/system/account-settings", nil, 200)
	f.singletonID = httpString(t, initial, "id")
	f.secrets = []string{f.admin.Password, f.csrf}
	if mode == "authority" {
		f.member = f.setup.inviteMember(ctx, f.adminClient, f.csrf, "security-member@example.com", "security-member")
		f.secrets = append(f.secrets, f.member.Password)
	}
	f.private("account-security-material.json", map[string]any{"admin": f.admin, "member": f.member, "initial": initial})
	t.Cleanup(func() {
		for _, value := range f.secrets {
			if value != "" && f.log.contains(value) {
				t.Error("Account security private material escaped restricted transfer")
			}
		}
		f.secrets = nil
		f.csrf, f.secondCSRF, f.admin.Password, f.member.Password = "", "", "", ""
		f.mu.Lock()
		f.lostKey = ""
		f.attempts = nil
		f.lostSettings = nil
		f.mu.Unlock()
	})
	return f
}
func (f *accountSecurityWebFixture) loginClient(ctx context.Context) (*http.Client, string, string) {
	client := f.setup.setupClient()
	bootstrap := f.setup.setupRequest(ctx, client, "GET", "/api/v1/auth/bootstrap", nil, "", false, 200)
	f.setup.setupRequest(ctx, client, "POST", "/api/v1/sessions/login", map[string]string{"email": f.admin.Email, "password": f.admin.Password}, httpString(f.t, bootstrap, "csrf_token"), true, 200)
	view := f.setup.setupRequest(ctx, client, "GET", "/api/v1/session", nil, "", false, 200)
	return client, httpString(f.t, view, "csrf_token"), httpString(f.t, httpObject(f.t, view, "session"), "id")
}
func (f *accountSecurityWebFixture) shouldFailRead(path string) bool {
	return path == "/api/v1/system/account-settings" && f.failRead.CompareAndSwap(true, false)
}
func (f *accountSecurityWebFixture) controlResponse(response *http.Response) error {
	r := response.Request
	if r == nil || r.Method != "PUT" || r.URL.Path != "/api/v1/system/account-settings" || response.StatusCode != 200 {
		return nil
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	key := r.Header.Get("Idempotency-Key")
	var attempt accountSecurityWebAttempt
	for n := len(f.attempts) - 1; n >= 0; n-- {
		if f.attempts[n].key == key {
			attempt = f.attempts[n]
			break
		}
	}
	if f.lostKey != "" && key == f.lostKey {
		f.replayed++
		f.sameOriginal = f.sameOriginal && attempt.body == f.lostBody && attempt.csrf == f.lostCSRF
	}
	if !f.dropNext {
		return nil
	}
	f.dropNext = false
	raw, err := io.ReadAll(io.LimitReader(response.Body, 600001))
	closeErr := response.Body.Close()
	defer clear(raw)
	var settings map[string]any
	if err != nil || closeErr != nil || len(raw) > 600000 || len(raw) < 2 || json.Unmarshal(raw, &settings) != nil || len(settings) != 7 || settings["id"] != f.singletonID || settings["lifetime_changes_apply_to"] != "newly_issued_sessions_and_tokens" {
		return errors.New("owned response control requires complete formal Account settings")
	}
	f.lostKey, f.lostBody, f.lostCSRF, f.lostSettings = key, attempt.body, attempt.csrf, settings
	f.dropped++
	return &modelsResponseLost{header: response.Header.Clone(), prefix: append([]byte(nil), raw[:len(raw)/2]...), length: len(raw)}
}
func (f *accountSecurityWebFixture) securityFacts(ctx context.Context) map[string]any {
	conn, closeConnection := invitationsWebConnect(f.t, ctx, f.db)
	defer closeConnection()
	var version, idle, absolute, reset, challenge string
	if err := conn.QueryRow(ctx, `SELECT version::text,session_idle_seconds::text,session_absolute_seconds::text,password_reset_seconds::text,challenge_after_failures::text FROM agenteam_account.account_settings WHERE singleton AND id=$1`, f.singletonID).Scan(&version, &idle, &absolute, &reset, &challenge); err != nil {
		f.t.Fatal("owned settings snapshot unavailable", err)
	}
	f.mu.Lock()
	key := f.lostKey
	f.mu.Unlock()
	identityDigest := ""
	if key != "" {
		identity, err := foundation.NewCommandIdentity("account.settings", []string{f.admin.UserID}, "settings-update", foundation.IdempotencyKey(key))
		if err != nil {
			f.t.Fatal("owned Account settings command identity invalid")
		}
		digest := sha256.Sum256([]byte(identity.Canonical()))
		identityDigest = "sha256:" + hex.EncodeToString(digest[:])
	}
	var commands, audits, originalCommands, originalAudits, invocations int
	if err := conn.QueryRow(ctx, `SELECT
 (SELECT count(*) FROM agenteam_account.commands WHERE namespace='account.settings' AND command_name='settings-update' AND resource_id=$1 AND owner_id=$2 AND phase='committed'),
 (SELECT count(*) FROM agenteam_audit.audit_records WHERE scope='system' AND producer='account' AND action='account.settings.update' AND resource_kind='account_settings' AND resource_id=$1 AND user_id=$2 AND outcome='success' AND ordinal=0),
 (SELECT count(*) FROM agenteam_account.commands WHERE namespace='account.settings' AND command_name='settings-update' AND resource_id=$1 AND owner_id=$2 AND identity_digest=$3 AND phase='committed'),
 (SELECT count(*) FROM agenteam_audit.audit_records WHERE scope='system' AND producer='account' AND action='account.settings.update' AND resource_kind='account_settings' AND resource_id=$1 AND user_id=$2 AND outcome='success' AND ordinal=0 AND cause_ref IN (SELECT id::text FROM agenteam_account.commands WHERE namespace='account.settings' AND command_name='settings-update' AND resource_id=$1 AND owner_id=$2 AND identity_digest=$3 AND phase='committed')),
 (SELECT count(*) FROM agenteam_model.invocations)`, f.singletonID, f.admin.UserID, identityDigest).Scan(&commands, &audits, &originalCommands, &originalAudits, &invocations); err != nil {
		f.t.Fatal("owned settings command/audit facts unavailable", err)
	}
	return map[string]any{"settings": map[string]any{"id": f.singletonID, "version": version, "session_idle_seconds": idle, "session_absolute_seconds": absolute, "password_reset_seconds": reset, "challenge_after_failures": challenge, "lifetime_changes_apply_to": "newly_issued_sessions_and_tokens"}, "commands": commands, "audits": audits, "original_commands": originalCommands, "original_audits": originalAudits, "invocations": invocations}
}

type accountSecurityWebIPC struct {
	Sequence  int    `json:"sequence"`
	Action    string `json:"action"`
	UserID    string `json:"user_id"`
	SessionID string `json:"session_id"`
}

func (f *accountSecurityWebFixture) securityIPC(ctx context.Context, r accountSecurityWebIPC) map[string]any {
	out := map[string]any{"ok": true, "sequence": r.Sequence}
	switch r.Action {
	case "facts":
		out["facts"] = f.securityFacts(ctx)
	case "compete":
		if f.secondClient == nil {
			f.secondClient, f.secondCSRF, _ = f.loginClient(ctx)
			f.secrets = append(f.secrets, f.secondCSRF)
		}
		observed := f.setup.setupRequest(ctx, f.secondClient, "GET", "/api/v1/system/account-settings", nil, "", false, 200)
		body := map[string]any{"version": observed["version"], "session_idle_seconds": "3600", "session_absolute_seconds": "14400", "password_reset_seconds": "3600", "challenge_after_failures": "6"}
		out["settings"] = f.setup.setupRequest(ctx, f.secondClient, "PUT", "/api/v1/system/account-settings", body, f.secondCSRF, true, 200)
	case "capture-session":
		if r.UserID != f.admin.UserID {
			f.t.Fatal("owned Session user mismatch")
		}
		if _, err := foundation.ParseID[struct{}](r.SessionID); err != nil {
			f.t.Fatal("owned Session ID invalid")
		}
		conn, closeConnection := invitationsWebConnect(f.t, ctx, f.db)
		defer closeConnection()
		if err := conn.QueryRow(ctx, `SELECT idle_seconds::text,absolute_expires_at::text FROM agenteam_account.sessions WHERE id=$1 AND user_id=$2 AND revoked_at IS NULL`, r.SessionID, r.UserID).Scan(&f.oldIdle, &f.oldAbsolute); err != nil {
			f.t.Fatal("owned live Session snapshot unavailable", err)
		}
		f.oldSessionID = r.SessionID
		out["captured"] = true
	case "lifetime-facts":
		if f.oldSessionID == "" {
			f.t.Fatal("original Session capture required")
		}
		_, csrf, newID := f.loginClient(ctx)
		f.secrets = append(f.secrets, csrf)
		conn, closeConnection := invitationsWebConnect(f.t, ctx, f.db)
		defer closeConnection()
		var oldIdle, oldAbsolute string
		if err := conn.QueryRow(ctx, `SELECT idle_seconds::text,absolute_expires_at::text FROM agenteam_account.sessions WHERE id=$1 AND user_id=$2 AND revoked_at IS NULL`, f.oldSessionID, f.admin.UserID).Scan(&oldIdle, &oldAbsolute); err != nil {
			f.t.Fatal("old owned Session disappeared", err)
		}
		var matches bool
		if err := conn.QueryRow(ctx, `SELECT s.idle_seconds=a.session_idle_seconds AND s.absolute_expires_at-s.issued_at=a.session_absolute_seconds*interval '1 second' FROM agenteam_account.sessions s CROSS JOIN agenteam_account.account_settings a WHERE s.id=$1 AND s.user_id=$2 AND s.revoked_at IS NULL AND a.singleton AND a.id=$3`, newID, f.admin.UserID, f.singletonID).Scan(&matches); err != nil {
			f.t.Fatal("new owned Session lifetimes unavailable", err)
		}
		out["old_unchanged"], out["new_matches"], out["different_session"] = oldIdle == f.oldIdle && oldAbsolute == f.oldAbsolute, matches, newID != f.oldSessionID
	case "arm-drop":
		f.mu.Lock()
		f.dropNext = true
		f.lostKey = ""
		f.lostSettings = nil
		f.dropped = 0
		f.replayed = 0
		f.sameOriginal = true
		f.mu.Unlock()
		out["armed"] = true
	case "drop-facts":
		f.mu.Lock()
		out["dropped"], out["replayed"], out["same_original"], out["settings"], out["armed"] = f.dropped, f.replayed, f.sameOriginal, f.lostSettings, f.dropNext
		f.mu.Unlock()
	case "arm-get-fail":
		f.failRead.Store(true)
		out["armed"] = true
	case "arm-session-fail":
		f.failSession.Store(true)
		out["armed"] = true
	case "failure-facts":
		out["read_failures"], out["session_failures"] = f.readFailures.Load(), f.sessionFailures.Load()
	case "attempt-facts":
		f.mu.Lock()
		keys := map[string]bool{}
		for _, attempt := range f.attempts {
			keys[attempt.key] = true
		}
		out["attempts"], out["distinct_keys"] = len(f.attempts), len(keys)
		f.mu.Unlock()
	case "demote", "promote", "revoke-session":
		if f.mode != "authority" || r.UserID != f.admin.UserID {
			f.t.Fatal("exact Account security authority target rejected")
		}
		if _, err := foundation.ParseID[struct{}](r.SessionID); err != nil {
			f.t.Fatal("exact Account security Session invalid")
		}
		conn, closeConnection := invitationsWebConnect(f.t, ctx, f.db)
		defer closeConnection()
		var exists bool
		if err := conn.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_account.sessions WHERE id=$1 AND user_id=$2 AND revoked_at IS NULL)`, r.SessionID, r.UserID).Scan(&exists); err != nil || !exists {
			f.t.Fatal("exact active Account security Session absent", err)
		}
		if r.Action == "revoke-session" {
			tag, err := conn.Exec(ctx, `UPDATE agenteam_account.sessions SET revoked_at=clock_timestamp(),revoked_reason='administrative' WHERE id=$1 AND user_id=$2 AND revoked_at IS NULL`, r.SessionID, r.UserID)
			if err != nil || tag.RowsAffected() != 1 {
				f.t.Fatal("exact Session revoke failed", err)
			}
		} else {
			role, previous := "user", "admin"
			if r.Action == "promote" {
				role, previous = "admin", "user"
			}
			tag, err := conn.Exec(ctx, `UPDATE agenteam_account.users SET role=$1,version=version+1,updated_at=clock_timestamp() WHERE id=$2 AND role=$3`, role, r.UserID, previous)
			if err != nil || tag.RowsAffected() != 1 {
				f.t.Fatal("exact owned role transition failed", err)
			}
		}
	default:
		f.t.Fatal("unknown Account security IPC action")
	}
	return out
}

func (f *accountSecurityWebFixture) browserAccountSecurity(ctx context.Context) map[string]any {
	f.t.Helper()
	root, e := filepath.Abs("../account-captcha-web")
	if e != nil {
		f.t.Fatal(e)
	}
	cmd := exec.CommandContext(ctx, "node", filepath.Join(root, "node_modules/@playwright/test/cli.js"), "test", "--config", filepath.Join(root, "system-account-security.config.js"))
	cmd.Dir = root
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = time.Second
	for _, value := range os.Environ() {
		if !strings.HasPrefix(value, "TMPDIR=") && !strings.HasPrefix(value, "AGENTEAM_AUTH_WEB_") && !strings.HasPrefix(value, "AGENTEAM_ACCOUNT_SECURITY_WEB_") && !strings.HasPrefix(value, "PLAYWRIGHT_NO_COPY_PROMPT=") && !strings.HasPrefix(value, "DEBUG=") && !strings.HasPrefix(value, "PWDEBUG=") {
			cmd.Env = append(cmd.Env, value)
		}
	}
	cmd.Env = append(cmd.Env, "TMPDIR="+f.directory, "AGENTEAM_AUTH_WEB_ORIGIN="+f.origin, "AGENTEAM_ACCOUNT_SECURITY_WEB_CASE="+f.mode, "AGENTEAM_AUTH_WEB_PRIVATE="+f.directory, "AGENTEAM_AUTH_WEB_CHROMIUM=/usr/bin/chromium", "PLAYWRIGHT_NO_COPY_PROMPT=1")
	if images := os.Getenv("AGENTEAM_AUTH_WEB_IMAGES"); images != "" {
		if !filepath.IsAbs(images) {
			f.t.Fatal("Account security image path must be absolute")
		}
		cmd.Env = append(cmd.Env, "AGENTEAM_AUTH_WEB_IMAGES="+images)
	}
	var output bytes.Buffer
	cmd.Stdout, cmd.Stderr = &output, &output
	if e = cmd.Start(); e != nil {
		f.t.Fatal("locked Account security browser runner could not start")
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	joined := false
	defer func() {
		if !joined {
			_ = cmd.Cancel()
			<-done
		}
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		cmd.Env = nil
	}()
	tick := time.NewTicker(20 * time.Millisecond)
	defer tick.Stop()
	sequence := 0
	var runErr error
wait:
	for {
		select {
		case runErr = <-done:
			joined = true
			break wait
		case <-tick.C:
			raw, err := os.ReadFile(filepath.Join(f.directory, "account-security-ipc.json"))
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			var request accountSecurityWebIPC
			decode := json.Unmarshal(raw, &request)
			clear(raw)
			if err != nil || decode != nil || request.Sequence < 1 || request.Sequence > 128 {
				f.t.Error("private Account security IPC rejected")
				_ = cmd.Cancel()
				continue
			}
			if request.Sequence <= sequence {
				continue
			}
			if request.Sequence != sequence+1 {
				f.t.Error("private Account security IPC sequence invalid")
				_ = cmd.Cancel()
				continue
			}
			reply := f.securityIPC(ctx, request)
			sequence = request.Sequence
			f.private("account-security-ack-"+strconv.Itoa(sequence)+".json", reply)
		}
	}
	safe := output.String()
	for _, secret := range f.secrets {
		if secret != "" {
			safe = strings.ReplaceAll(safe, secret, "[redacted]")
		}
	}
	f.mu.Lock()
	if f.lostKey != "" {
		safe = strings.ReplaceAll(safe, f.lostKey, "[redacted]")
	}
	for _, attempt := range f.attempts {
		if attempt.key != "" {
			safe = strings.ReplaceAll(safe, attempt.key, "[redacted]")
		}
	}
	f.mu.Unlock()
	f.t.Log(safe)
	if runErr != nil {
		f.t.Fatalf("actual Account security production browser failed: %v", runErr)
	}
	raw, e := os.ReadFile(filepath.Join(f.directory, "account-security-result.json"))
	if e != nil {
		f.t.Fatal("safe Account security browser result missing")
	}
	defer clear(raw)
	var result map[string]any
	if json.Unmarshal(raw, &result) != nil || result["completed"] != true {
		f.t.Fatal("safe Account security browser result invalid")
	}
	return result
}
