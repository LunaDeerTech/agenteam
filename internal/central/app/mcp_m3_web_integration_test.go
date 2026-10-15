//go:build integration && linux

package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
	mcpfixture "github.com/LunaDeerTech/agenteam/tests/mcp/fixture"
)

// The browser uses the original Account/Project/MCP/Secret handlers and the
// official SDK peer. The proxy changes only delivery of one committed receipt.
func TestMCPM3OwnerWeb(t *testing.T) {
	mcpM3ConfigurationPreflight(t)
	dist, runtime, evidence := os.Getenv("AGENTEAM_MCP_WEB_DIST"), os.Getenv("AGENTEAM_AUTH_WEB_RUNTIME"), os.Getenv("AGENTEAM_MCP_WEB_EVIDENCE")
	playwright, chromium, config := os.Getenv("AGENTEAM_MCP_WEB_PLAYWRIGHT"), os.Getenv("AGENTEAM_AUTH_WEB_CHROMIUM"), os.Getenv("AGENTEAM_MCP_WEB_CONFIG")
	for _, path := range []string{dist, runtime, evidence, playwright, chromium, config} {
		if !filepath.IsAbs(path) {
			t.Fatal("exact owned MCP browser inputs required")
		}
	}
	if len(runtime) > 45 {
		t.Fatal("owned MCP browser runtime path too long")
	}
	if info, err := os.Stat(filepath.Join(dist, "index.html")); err != nil || !info.Mode().IsRegular() {
		t.Fatal("frozen MCP production assets absent")
	}
	private, err := os.MkdirTemp(runtime, "mcp-")
	workAppCheck(t, "mcp-web-private", err)
	t.Cleanup(func() { workAppCheck(t, "mcp-web-private-cleanup", os.RemoveAll(private)) })
	evidence = filepath.Join(evidence, t.Name())
	workAppCheck(t, "mcp-web-evidence", os.Mkdir(evidence, 0700))
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	workAppCheck(t, "mcp-web-listener", err)
	t.Cleanup(func() { _ = listener.Close() })
	origin := "http://" + listener.Addr().String()
	peer := mcpfixture.New(t)
	v := newMCPM3AppWithOrigin(t, peer, nil, origin)
	base := "/api/v1/projects/" + v.a.String() + "/mcp"
	backend, err := url.Parse(v.address)
	workAppCheck(t, "mcp-web-backend", err)
	transport := &http.Transport{Proxy: nil}
	proxy := httputil.NewSingleHostReverseProxy(backend)
	proxy.Transport, proxy.ErrorLog = transport, log.New(io.Discard, "", 0)
	var mu sync.Mutex
	writes, lookups := 0, 0
	dropped, credential := false, ""
	proxy.ModifyResponse = func(response *http.Response) error {
		r := response.Request
		if r.Method != http.MethodPost {
			return nil
		}
		mu.Lock()
		defer mu.Unlock()
		if r.URL.Path == base+"/credential-commands/lookup" {
			lookups++
			return nil
		}
		if r.URL.Path != base+"/credentials" {
			return nil
		}
		writes++
		if dropped {
			return nil
		}
		raw, err := io.ReadAll(io.LimitReader(response.Body, 4097))
		closeErr := response.Body.Close()
		defer clear(raw)
		var receipt struct {
			ID      string    `json:"credential_id"`
			Purpose string    `json:"purpose"`
			Version f.Version `json:"version"`
			Deleted bool      `json:"deleted"`
		}
		if err != nil || closeErr != nil || len(raw) > 4096 || response.StatusCode != http.StatusOK || json.Unmarshal(raw, &receipt) != nil || receipt.Purpose != "mcp" || receipt.Version != 1 || receipt.Deleted {
			return errors.New("owned credential response invalid")
		}
		if _, err := f.ParseID[sc.Credential](receipt.ID); err != nil {
			return errors.New("owned credential receipt identity invalid")
		}
		dropped, credential = true, receipt.ID
		return errors.New("owned original response delivery interrupted")
	}
	proxy.ErrorHandler = func(w http.ResponseWriter, _ *http.Request, _ error) {
		http.Error(w, "Owned response unavailable", http.StatusBadGateway)
	}
	server := &http.Server{ReadHeaderTimeout: 2 * time.Second, ErrorLog: log.New(io.Discard, "", 0), Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
	go func() { served <- server.Serve(listener) }()
	var once sync.Once
	stop := func() {
		once.Do(func() {
			ctx, cancel := context.WithTimeout(context.WithoutCancel(v.ctx), 3*time.Second)
			defer cancel()
			if err := server.Shutdown(ctx); err != nil {
				t.Error("MCP browser proxy shutdown did not join")
				_ = server.Close()
			}
			if err := <-served; err != nil && !errors.Is(err, http.ErrServerClosed) {
				t.Error("MCP browser proxy original Serve failed")
			}
			transport.CloseIdleConnections()
		})
	}
	t.Cleanup(stop)
	session, err := v.request(v.ctx, http.MethodGet, "/api/v1/session", nil, "")
	workAppCheck(t, "mcp-web-session", err)
	var identity struct {
		User struct {
			Username string `json:"username"`
		} `json:"user"`
	}
	if session.status != http.StatusOK || json.Unmarshal(session.body, &identity) != nil || identity.User.Username == "" {
		t.Fatal("MCP browser Owner route identity absent")
	}
	material, err := peer.Credential()
	workAppCheck(t, "mcp-web-owned-credential", err)
	err = material.Use(func(value []byte) error {
		payload := map[string]any{"origin": origin, "cookie": v.cookie, "route": "/" + identity.User.Username + "/runtime-app-a/settings/mcp", "endpoint": peer.Endpoint() + "/protected", "material": string(value), "evidence": evidence}
		raw, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		defer clear(raw)
		defer delete(payload, "material")
		return os.WriteFile(filepath.Join(private, "input.json"), raw, 0600)
	})
	material.Destroy()
	workAppCheck(t, "mcp-web-private-input", err)
	mcpM3RunBrowser(t, v.ctx, private, config, playwright, chromium)
	mu.Lock()
	actualWrites, actualLookups, actualDropped, actualCredential := writes, lookups, dropped, credential
	mu.Unlock()
	if actualWrites != 1 || actualLookups != 1 || !actualDropped {
		t.Fatalf("MCP browser credential writes=%d lookups=%d dropped=%t", actualWrites, actualLookups, actualDropped)
	}
	mcpM3CredentialFacts(t, v, actualCredential, 1, 1, 1)
	var configs, connections, leases int
	workAppCheck(t, "mcp-web-current-facts", v.admin.QueryRow(v.ctx, `SELECT (SELECT count(*) FROM agenteam_mcp.configs WHERE project_id=$1),(SELECT count(*) FROM agenteam_mcp.connections WHERE project_id=$1),(SELECT count(*) FROM agenteam_mcp.connection_requests WHERE project_id=$1 AND phase<>'complete')`, v.a.String()).Scan(&configs, &connections, &leases))
	if configs != 0 || connections != 0 || leases != 0 {
		t.Fatal("MCP browser config lifecycle did not persist or original request remained")
	}
	calls, err := peer.CallCount()
	workAppCheck(t, "mcp-web-peer-call-count", err)
	if calls != 0 {
		t.Fatal("MCP Owner browser unexpectedly dispatched an Agent Tool")
	}
	stop()
	v.stopBeforeRelease(t)
	v.store.releaseDirectory()
	usageAppWait(t, v.root.done, "mcp-web-original-app")
	workAppCheck(t, "mcp-web-original-app-return", v.root.err)
	if !v.runtime.Joined() || !v.assembly.Joined() || !v.root.owned.producersJoined() {
		t.Fatal("MCP browser original owners did not join")
	}
	t.Log("MCP_OWNER_WEB actual_app=true credential_write1_lookup1=true actual_controlled_sdk_discovery=true catalogs=true enable_disable_disconnect_delete=true browser_layout_keyboard=true original_owners_joined=true agent_not_run=true")
}

func mcpM3RunBrowser(t *testing.T, ctx context.Context, private, config, playwright, chromium string) {
	t.Helper()
	cmd := exec.CommandContext(ctx, "node", filepath.Join(playwright, "cli.js"), "test", "--config", config)
	cmd.Dir = private
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = time.Second
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if key != "TMPDIR" && key != "DEBUG" && key != "PWDEBUG" && !strings.HasPrefix(key, "AGENTEAM_") {
			cmd.Env = append(cmd.Env, entry)
		}
	}
	cmd.Env = append(cmd.Env, "TMPDIR="+private, "AGENTEAM_MCP_WEB_PLAYWRIGHT="+playwright, "AGENTEAM_MCP_WEB_PRIVATE="+private, "AGENTEAM_AUTH_WEB_CHROMIUM="+chromium)
	var output bytes.Buffer
	cmd.Stdout, cmd.Stderr = &output, &output
	workAppCheck(t, "mcp-web-node-start", cmd.Start())
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
	err := <-done
	joined = true
	t.Logf("MCP browser actual_wait pid=%d success=%t", cmd.Process.Pid, err == nil)
	raw, readErr := os.ReadFile(filepath.Join(private, "complete.json"))
	var complete struct {
		Complete bool `json:"complete"`
	}
	if err != nil || readErr != nil || len(raw) > 1024 || json.Unmarshal(raw, &complete) != nil || !complete.Complete {
		var failure struct{ Stage, Error string }
		if raw, readErr := os.ReadFile(filepath.Join(private, "failure.json")); readErr == nil && len(raw) < 1024 {
			_ = json.Unmarshal(raw, &failure)
		}
		if regexp.MustCompile(`^[a-z-]{1,40}$`).MatchString(failure.Stage) {
			t.Logf("MCP browser failed_stage=%s", failure.Stage)
		}
		if regexp.MustCompile(`^[A-Z_]{1,64}$`).MatchString(failure.Error) {
			t.Logf("MCP browser failure_kind=%s", failure.Error)
		}
		t.Fatal("MCP locked browser failed; private output withheld")
	}
}
