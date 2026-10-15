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
)

// The original nine-reply workflow creates every proposal through a real Agent
// Tool operation. Only the Owner's two review actions move into the browser.
func TestMeetingM3AppProposalInboxWeb(t *testing.T) {
	for _, key := range []string{"AGENTEAM_MEETING_WEB_DIST", "AGENTEAM_MEETING_PROPOSAL_WEB_HARNESS", "AGENTEAM_MEETING_WEB_EVIDENCE", "AGENTEAM_AUTH_WEB_CHROMIUM"} {
		if !filepath.IsAbs(os.Getenv(key)) {
			t.Fatal("owned Proposal browser paths required")
		}
	}
	runMeetingM3ProposalInbox(t, meetingProposalBrowser)
}

func meetingProposalBrowser(t *testing.T, workflow *m1Workflow, listener net.Listener, proposals []meetingProposalBrowserCase) map[string]meetingProposalBrowserReview {
	t.Helper()
	v := workflow.app
	dist, harness := os.Getenv("AGENTEAM_MEETING_WEB_DIST"), os.Getenv("AGENTEAM_MEETING_PROPOSAL_WEB_HARNESS")
	chromium := os.Getenv("AGENTEAM_AUTH_WEB_CHROMIUM")
	for _, path := range []string{filepath.Join(dist, "index.html"), filepath.Join(harness, "meeting-proposal-inbox.config.js"), filepath.Join(harness, "e2e", "meeting-proposal-inbox.spec.ts"), filepath.Join(harness, "node_modules/@playwright/test/cli.js"), chromium} {
		if info, err := os.Stat(path); err != nil || !info.Mode().IsRegular() {
			t.Fatal("frozen Proposal browser source, executable or assets missing")
		}
	}
	manifest, err := os.ReadFile(filepath.Join(harness, "node_modules/@playwright/test/package.json"))
	workAppCheck(t, "proposal-playwright-manifest", err)
	var version struct {
		Version string `json:"version"`
	}
	if json.Unmarshal(manifest, &version) != nil || version.Version != "1.56.1" {
		t.Fatal("Proposal browser requires locked Playwright 1.56.1")
	}
	private, err := os.MkdirTemp("/tmp", "agenteam-mp-web-")
	workAppCheck(t, "proposal-browser-private", err)
	t.Cleanup(func() { workAppCheck(t, "proposal-browser-private-cleanup", os.RemoveAll(private)) })
	evidence := filepath.Join(os.Getenv("AGENTEAM_MEETING_WEB_EVIDENCE"), t.Name())
	workAppCheck(t, "proposal-browser-evidence", os.Mkdir(evidence, 0700))
	backend, err := url.Parse(v.address)
	workAppCheck(t, "proposal-browser-backend", err)
	transport := &http.Transport{Proxy: nil, DisableKeepAlives: true}
	proxy := httputil.NewSingleHostReverseProxy(backend)
	proxy.Transport = transport
	proxy.ErrorLog = log.New(io.Discard, "", 0)
	proxy.ErrorHandler = func(out http.ResponseWriter, _ *http.Request, _ error) {
		http.Error(out, "Owned App unavailable", http.StatusBadGateway)
	}
	server := &http.Server{ReadHeaderTimeout: 2 * time.Second, ErrorLog: log.New(io.Discard, "", 0), Handler: http.HandlerFunc(func(out http.ResponseWriter, request *http.Request) {
		if strings.HasPrefix(request.URL.Path, "/api/") {
			proxy.ServeHTTP(out, request)
			return
		}
		if request.Method != http.MethodGet && request.Method != http.MethodHead {
			http.NotFound(out, request)
			return
		}
		if strings.HasPrefix(request.URL.Path, "/assets/") {
			skillWebServeAsset(out, request, dist)
			return
		}
		http.ServeFile(out, request, filepath.Join(dist, "index.html"))
	})}
	served := make(chan error, 1)
	go func() { served <- server.Serve(listener) }()
	var once sync.Once
	stop := func() {
		once.Do(func() {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			if err := server.Shutdown(ctx); err != nil {
				t.Error("Proposal browser proxy exceeded its original shutdown budget")
				_ = server.Close()
			}
			if err := <-served; err != nil && !errors.Is(err, http.ErrServerClosed) {
				t.Error("Proposal browser proxy original Serve failed")
			}
			transport.CloseIdleConnections()
		})
	}
	t.Cleanup(stop)
	defer stop()
	response := taskExecutionGet(t, v.workAppFixture, v.client, v.cookie, http.MethodGet, "/api/v1/session", http.StatusOK)
	var identity struct {
		User struct {
			Username string `json:"username"`
		} `json:"user"`
	}
	workAppCheck(t, "proposal-browser-current-user", json.Unmarshal(response.body, &identity))
	clear(response.body)
	if identity.User.Username == "" {
		t.Fatal("Proposal browser lost its original Human Session")
	}
	material := workAppJSON(t, map[string]any{"origin": workflow.origin, "cookie": v.cookie, "project_id": v.a.String(), "route": "/" + identity.User.Username + "/runtime-app-a/inbox", "proposals": proposals})
	workAppCheck(t, "proposal-browser-material", os.WriteFile(filepath.Join(private, "proposal-material.json"), material, 0600))
	clear(material)
	meetingProposalPlaywright(t, workflow, harness, private, evidence, chromium, true)
	meetingProposalPlaywright(t, workflow, harness, private, evidence, chromium, false)
	raw, err := os.ReadFile(filepath.Join(private, "proposal-result.json"))
	workAppCheck(t, "proposal-browser-result", err)
	var result struct {
		Completed bool                                    `json:"completed"`
		Reviews   map[string]meetingProposalBrowserReview `json:"reviews"`
	}
	if json.Unmarshal(raw, &result) != nil || !result.Completed || len(result.Reviews) != 2 {
		t.Fatal("Proposal browser did not complete its original two reviews")
	}
	return result.Reviews
}

func meetingProposalPlaywright(t *testing.T, workflow *m1Workflow, harness, private, evidence, chromium string, list bool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(workflow.app.ctx, 50*time.Second)
	defer cancel()
	args := []string{filepath.Join(harness, "node_modules/@playwright/test/cli.js"), "test", "--config", filepath.Join(harness, "meeting-proposal-inbox.config.js")}
	if list {
		args = append(args, "--list")
	}
	cmd := exec.CommandContext(ctx, "node", args...)
	cmd.Dir = harness
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.WaitDelay = time.Second
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		switch key {
		case "TMPDIR", "TMP", "TEMP", "NODE_COMPILE_CACHE", "PWTEST_CACHE_DIR", "XDG_CACHE_HOME", "DEBUG", "PWDEBUG", "DEV_CHAT_BASE_URL", "DEV_CHAT_API_KEY", "DEV_CHAT_MODEL", "PLAYWRIGHT_NO_COPY_PROMPT":
			continue
		}
		if !strings.HasPrefix(key, "AGENTEAM_") {
			cmd.Env = append(cmd.Env, entry)
		}
	}
	for _, name := range []string{"TMPDIR", "TMP", "TEMP", "NODE_COMPILE_CACHE", "PWTEST_CACHE_DIR", "XDG_CACHE_HOME"} {
		path := filepath.Join(private, strings.ToLower(name))
		workAppCheck(t, "proposal-browser-child-cache", os.MkdirAll(path, 0700))
		cmd.Env = append(cmd.Env, name+"="+path)
	}
	cmd.Env = append(cmd.Env, "PLAYWRIGHT_NO_COPY_PROMPT=1", "AGENTEAM_AUTH_WEB_ORIGIN="+workflow.origin, "AGENTEAM_AUTH_WEB_PRIVATE="+private, "AGENTEAM_AUTH_WEB_CHROMIUM="+chromium, "AGENTEAM_MEETING_WEB_EVIDENCE="+evidence, "AGENTEAM_MEETING_PROPOSAL_WEB_CASE=review-original-proposals")
	var output bytes.Buffer
	cmd.Stdout, cmd.Stderr = &output, &output
	workAppCheck(t, "proposal-browser-start", cmd.Start())
	err := cmd.Wait()
	// Wait is collected before reclaiming the original child process group.
	_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	cmd.Env = nil
	t.Logf("MEETING_PROPOSAL_BROWSER list=%t actual_wait=true pid=%d success=%t", list, cmd.Process.Pid, err == nil)
	redacted := output.String()
	for _, secret := range []string{workflow.app.cookie, workflow.app.csrf} {
		if secret != "" {
			redacted = strings.ReplaceAll(redacted, secret, "[fixture-session-redacted]")
		}
	}
	name, expected := "proposal-browser-console.log", `(?m)\b1 passed\b`
	if list {
		name, expected = "proposal-browser-list.log", `Total: 1 test in 1 file`
	}
	workAppCheck(t, "proposal-browser-console", os.WriteFile(filepath.Join(evidence, name), []byte(redacted), 0600))
	if err != nil || !regexp.MustCompile(expected).Match(output.Bytes()) {
		t.Fatal("Proposal browser did not pass its exact original case; see private evidence console")
	}
}
