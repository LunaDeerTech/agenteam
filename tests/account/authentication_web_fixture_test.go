//go:build integration

package account_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
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

type authenticationWebFixture struct {
	t                          *testing.T
	db                         *pgfixture.Database
	origin, directory, webRoot string
	entry                      httpRecoveryRecord
	log                        *httpFixtureLog
	record                     func(purpose, email, exactID string) httpRecoveryRecord
}

// Only this test server hosts the production dist. The production Central root
// still owns every API response, Cookie, Origin check and CSRF decision.
func newAuthenticationWebFixture(t *testing.T, ctx context.Context) *authenticationWebFixture {
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
		"CURSOR_KEYRING": `{"format":1,"current_kid":"test","keys":[{"kid":"test","key_b64":"AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8="}]}`,
		"SECRET_KEYRING": `{"format":1,"current_version":"1","keys":[{"version":"1","key_b64":"ICEiIyQlJicoKSorLC0uLzAxMjM0NTY3ODk6Ozw9Pj8="}]}`,
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
	proxy.ErrorHandler = func(w http.ResponseWriter, _ *http.Request, _ error) { http.Error(w, "owned API unavailable", 502) }
	root, err := filepath.Abs("../../web/dist")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(filepath.Join(root, "index.html")); err != nil {
		t.Fatal("formal production dist must be built before browser execution")
	}
	assets := http.FileServer(http.Dir(root))
	server := &http.Server{ReadHeaderTimeout: 5 * time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
	f := &authenticationWebFixture{t: t, db: db, origin: origin, directory: directory, webRoot: root, entry: entry, log: output, record: old.record}
	t.Cleanup(func() {
		if output.contains(entry.Password) {
			t.Error("bootstrap material escaped the restricted recovery log")
		}
	})
	var configured bool
	if err := db.Connect(t).QueryRow(ctx, `SELECT configured FROM agenteam_model.platform_selection WHERE singleton`).Scan(&configured); err != nil || configured {
		t.Fatal("real Model Initialize technical singleton missing", err)
	}
	return f
}

// This precondition client uses only real Account HTTP with its own Cookie jar.
// It never supplies a Session, CSRF value or challenge answer to the product.
func (f *authenticationWebFixture) setChallengeThreshold(ctx context.Context) {
	f.t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		f.t.Fatal(err)
	}
	transport := &http.Transport{Proxy: nil}
	defer transport.CloseIdleConnections()
	client := &http.Client{Jar: jar, Transport: transport, Timeout: 20 * time.Second}
	request := func(method, path string, body any, csrf string) map[string]any {
		raw, err := json.Marshal(body)
		if err != nil {
			f.t.Fatal("invalid setup body")
		}
		defer clear(raw)
		var reader io.Reader
		if body != nil {
			reader = bytes.NewReader(raw)
		}
		r, err := http.NewRequestWithContext(ctx, method, f.origin+path, reader)
		if err != nil {
			f.t.Fatal(err)
		}
		r.Header.Set("Origin", f.origin)
		r.Header.Set("Sec-Fetch-Site", "same-origin")
		if body != nil {
			r.Header.Set("Content-Type", "application/json")
			r.Header.Set("Idempotency-Key", id[struct{}](f.t).String())
		}
		if csrf != "" {
			r.Header.Set("X-CSRF-Token", csrf)
		}
		response, err := client.Do(r)
		if err != nil {
			f.t.Fatal("real setup HTTP failed")
		}
		defer response.Body.Close()
		if response.StatusCode != 200 {
			f.t.Fatalf("real setup HTTP status=%d", response.StatusCode)
		}
		var out map[string]any
		if json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&out) != nil {
			f.t.Fatal("invalid real setup response")
		}
		return out
	}
	bootstrap := request("GET", "/api/v1/auth/bootstrap", nil, "")
	request("POST", "/api/v1/sessions/login", map[string]string{"email": f.entry.Email, "password": f.entry.Password}, httpString(f.t, bootstrap, "csrf_token"))
	session := request("GET", "/api/v1/session", nil, "")
	settings := request("GET", "/api/v1/system/account-settings", nil, "")
	request("PUT", "/api/v1/system/account-settings", map[string]any{"version": settings["version"], "session_idle_seconds": settings["session_idle_seconds"], "session_absolute_seconds": settings["session_absolute_seconds"], "password_reset_seconds": settings["password_reset_seconds"], "challenge_after_failures": "1"}, httpString(f.t, session, "csrf_token"))
}

type authenticationWebResult struct {
	UserID      string `json:"user_id"`
	SessionID   string `json:"session_id"`
	ChallengeID string `json:"challenge_id"`
	Verified    bool   `json:"verified"`
	ExactPass   bool   `json:"exact_pass"`
	ExactKey    bool   `json:"exact_key"`
	LoggedOut   bool   `json:"logged_out"`
	Unavailable bool   `json:"unavailable"`
	Layouts     int    `json:"layouts"`
}

func (f *authenticationWebFixture) browser(ctx context.Context, name string) authenticationWebResult {
	f.t.Helper()
	root, err := filepath.Abs("../account-captcha-web")
	if err != nil {
		f.t.Fatal(err)
	}
	cmd := exec.CommandContext(ctx, "node", filepath.Join(root, "node_modules/@playwright/test/cli.js"), "test", "--config", filepath.Join(root, "authentication.config.js"))
	cmd.Dir = root
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = time.Second
	for _, v := range os.Environ() {
		if !strings.HasPrefix(v, "TMPDIR=") && !strings.HasPrefix(v, "AGENTEAM_AUTH_WEB_") && !strings.HasPrefix(v, "PLAYWRIGHT_NO_COPY_PROMPT=") && !strings.HasPrefix(v, "DEBUG=") && !strings.HasPrefix(v, "PWDEBUG=") {
			cmd.Env = append(cmd.Env, v)
		}
	}
	cmd.Env = append(cmd.Env, "TMPDIR="+f.directory, "AGENTEAM_AUTH_WEB_ORIGIN="+f.origin, "AGENTEAM_AUTH_WEB_CASE="+name, "AGENTEAM_AUTH_WEB_PRIVATE="+f.directory, "AGENTEAM_AUTH_WEB_CHROMIUM=/usr/bin/chromium", "PLAYWRIGHT_NO_COPY_PROMPT=1")
	if images := os.Getenv("AGENTEAM_AUTH_WEB_IMAGES"); images != "" {
		if !filepath.IsAbs(images) {
			f.t.Fatal("task-owned layout image path must be absolute")
		}
		cmd.Env = append(cmd.Env, "AGENTEAM_AUTH_WEB_IMAGES="+images)
	}
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	if err = cmd.Start(); err != nil {
		f.t.Fatal("locked browser runner could not start")
	}
	defer func() { _ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); cmd.Env = nil }()
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	var runErr error
	finished := false
	expired := false
	for !finished {
		select {
		case runErr = <-done:
			finished = true
		case <-ticker.C:
			if name == "expiry" && !expired {
				raw, e := os.ReadFile(filepath.Join(f.directory, "expire.json"))
				if errors.Is(e, os.ErrNotExist) {
					continue
				}
				if e != nil {
					f.t.Error("private expiry IPC failed")
					_ = cmd.Cancel()
					continue
				}
				var request struct {
					SessionID string `json:"session_id"`
				}
				e = json.Unmarshal(raw, &request)
				clear(raw)
				if _, valid := foundation.ParseID[struct{}](request.SessionID); e != nil || valid != nil {
					f.t.Error("expiry IPC contained no confirmed Session ID")
					_ = cmd.Cancel()
					continue
				}
				conn := f.db.Connect(f.t)
				var issued, activity, absolute time.Time
				if e = conn.QueryRow(ctx, `SELECT issued_at,last_activity_at,absolute_expires_at FROM agenteam_account.sessions WHERE id=$1 AND user_id=$2 AND revoked_at IS NULL`, request.SessionID, f.entry.ID).Scan(&issued, &activity, &absolute); e != nil {
					f.t.Error("expiry target did not match confirmed owned user/session")
					_ = cmd.Cancel()
					continue
				}
				tag, e := conn.Exec(ctx, `UPDATE agenteam_account.sessions SET issued_at=clock_timestamp()-interval '2 days',last_activity_at=clock_timestamp()-interval '1 day',absolute_expires_at=clock_timestamp()-interval '1 hour' WHERE id=$1 AND user_id=$2 AND issued_at=$3 AND last_activity_at=$4 AND absolute_expires_at=$5 AND revoked_at IS NULL`, request.SessionID, f.entry.ID, issued, activity, absolute)
				if e != nil || tag.RowsAffected() != 1 {
					f.t.Error("exact owned expiry update failed")
					_ = cmd.Cancel()
					continue
				}
				f.t.Logf("expiry exact Session=%s prior issued=%s activity=%s absolute=%s; one owned row moved into past", request.SessionID, issued.UTC().Format(time.RFC3339Nano), activity.UTC().Format(time.RFC3339Nano), absolute.UTC().Format(time.RFC3339Nano))
				if e = os.WriteFile(filepath.Join(f.directory, "expire-ack.json"), []byte(`{"expired":true}`), 0600); e != nil {
					f.t.Error("private expiry acknowledgement failed")
					_ = cmd.Cancel()
				}
				expired = true
			}
		}
	}
	// The runner receives sensitive material only from the restricted file. Its
	// assertions report booleans/statuses; this final filter also protects fills.
	safe := strings.ReplaceAll(output.String(), f.entry.Password, "[redacted]")
	f.t.Log(safe)
	if runErr != nil {
		f.t.Fatalf("actual production browser failed: %v", runErr)
	}
	raw, err := os.ReadFile(filepath.Join(f.directory, "result.json"))
	if err != nil {
		f.t.Fatal("browser safe result missing")
	}
	defer clear(raw)
	var result authenticationWebResult
	if json.Unmarshal(raw, &result) != nil {
		f.t.Fatal("browser safe result invalid")
	}
	return result
}
