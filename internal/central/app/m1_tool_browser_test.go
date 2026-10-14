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
	"reflect"
	"regexp"
	"slices"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	wc "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
)

// browser consumes the same completed App/Runner Task. It introduces no seed,
// alternate API, browser route interception or direct Task mutation.
func (w *m1Workflow) browser(t *testing.T, listener net.Listener) {
	t.Helper()
	v := w.app
	dist, harness, evidenceRoot := os.Getenv("AGENTEAM_M1_WEB_DIST"), os.Getenv("AGENTEAM_M1_WEB_HARNESS"), os.Getenv("AGENTEAM_M1_WEB_EVIDENCE")
	if !filepath.IsAbs(dist) || !filepath.IsAbs(harness) || !filepath.IsAbs(evidenceRoot) {
		t.Fatal("M1 owned browser assets, harness and evidence required")
	}
	for _, path := range []string{filepath.Join(dist, "index.html"), filepath.Join(harness, "m1-task-workflow.config.js"), filepath.Join(harness, "node_modules/@playwright/test/cli.js")} {
		if info, err := os.Stat(path); err != nil || !info.Mode().IsRegular() {
			t.Fatal("M1 required browser source or production asset missing")
		}
	}
	evidence := filepath.Join(evidenceRoot, t.Name())
	workAppCheck(t, "m1-browser-evidence-parent", os.MkdirAll(filepath.Dir(evidence), 0700))
	workAppCheck(t, "m1-browser-fresh-evidence", os.Mkdir(evidence, 0700))
	// Chromium's Unix sockets need a short private path, independent of the Go
	// test's potentially long TempDir. This directory is owned by this child only.
	private, err := os.MkdirTemp("/tmp", "agenteam-m1-web-")
	workAppCheck(t, "m1-browser-private", err)
	t.Cleanup(func() { workAppCheck(t, "m1-browser-private-cleanup", os.RemoveAll(private)) })

	backend, err := url.Parse(v.address)
	workAppCheck(t, "m1-browser-backend", err)
	transport := &http.Transport{Proxy: nil, DisableKeepAlives: true}
	proxy := httputil.NewSingleHostReverseProxy(backend)
	proxy.Transport = transport
	proxy.ErrorLog = log.New(io.Discard, "", 0)
	proxy.ErrorHandler = func(out http.ResponseWriter, _ *http.Request, _ error) {
		http.Error(out, "M1 original App unavailable", http.StatusBadGateway)
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
			ctx, cancel := context.WithTimeout(v.ctx, 2*time.Second)
			defer cancel()
			if err := server.Shutdown(ctx); err != nil {
				t.Error("M1 browser proxy exceeded original shutdown budget")
				_ = server.Close()
			}
			if err := <-served; err != nil && !errors.Is(err, http.ErrServerClosed) {
				t.Error("M1 browser proxy original Serve failed")
			}
			transport.CloseIdleConnections()
		})
	}
	t.Cleanup(stop)

	session := taskExecutionGet(t, v.workAppFixture, v.client, v.cookie, http.MethodGet, "/api/v1/session", http.StatusOK)
	var identity struct {
		User struct {
			Username string `json:"username"`
		} `json:"user"`
	}
	workAppCheck(t, "m1-browser-session-user", json.Unmarshal(session.body, &identity))
	clear(session.body)
	if identity.User.Username == "" {
		t.Fatal("M1 formal Session username missing")
	}
	before, err := v.planning.taskReader.GetTask(v.ctx, v.actor, v.a, w.task.ID)
	workAppCheck(t, "m1-browser-initial-task", err)
	if !reflect.DeepEqual(before, w.task) || before.State != wc.TaskStateInProgress || before.AssigneeAgentID == nil || *before.AssigneeAgentID != v.agent {
		t.Fatal("M1 Task changed before explicit Owner review")
	}
	view := w.view
	if view.Availability != ec.RuntimeAvailable || view.Execution.ID != w.execution || view.Execution.Status != ec.Succeeded || view.Execution.StartedAt == nil || view.Execution.CompletedAt == nil || view.Snapshot == nil || view.Snapshot.Schema != "durable_final_text_v2" || len(view.Snapshot.Items) != 1 {
		t.Fatal("M1 actual final-round projection missing")
	}
	item := view.Snapshot.Items[0]
	if item.Data.Content != m1FinalOutput || item.SourceReference.ID != item.ID {
		t.Fatal("M1 actual final-round content or identity differs")
	}
	payload := map[string]any{
		"mode": "read-write-run-review", "origin": w.origin, "cookie": v.cookie,
		"project_id": v.a.String(), "task_id": before.ID.String(), "agent_id": v.agent.String(), "reviewer_id": w.reviewer.String(),
		"execution_id": w.execution.String(), "final_round_id": item.ID.String(),
		"execution_started_at": view.Execution.StartedAt, "final_round_started_at": item.StartedAt, "execution_completed_at": view.Execution.CompletedAt,
		"route": "/" + identity.User.Username + "/runtime-app-a/tasks/" + before.ID.String(), "title": before.Title, "task_version": before.Version,
		"expected_output": item.Data.Content,
		"artifact_checks": []map[string]string{{"path": "stats.py"}, {"path": "result.json", "expected_content": m1Result}},
	}
	raw, err := json.Marshal(payload)
	workAppCheck(t, "m1-browser-material", err)
	workAppCheck(t, "m1-browser-material-write", os.WriteFile(filepath.Join(private, "m1-task-workflow-material.json"), raw, 0600))
	clear(raw)
	delete(payload, "cookie")
	w.runBrowser(t, private, harness, dist, evidence)

	after, err := v.planning.taskReader.GetTask(v.ctx, v.actor, v.a, before.ID)
	workAppCheck(t, "m1-browser-persisted-review", err)
	if after.State != wc.TaskStateInReview || after.Version != before.Version+1 || after.AssigneeAgentID == nil || *after.AssigneeAgentID != w.reviewer {
		t.Fatal("M1 Owner review did not persist exactly one in_review transition")
	}
	path := taskExecutionReadPath(v.a, before.ID.String(), w.execution) + "/runtime-view"
	actual := taskExecutionGet(t, v.workAppFixture, v.client, v.cookie, http.MethodGet, path, http.StatusOK)
	var reread ec.TaskRuntimeView
	workAppCheck(t, "m1-browser-persisted-output", json.Unmarshal(actual.body, &reread))
	clear(actual.body)
	if !reflect.DeepEqual(reread, view) {
		t.Fatal("M1 review changed the completed Execution projection")
	}
	stop()
	t.Log("M1_BROWSER_WORKFLOW real_http=true output_reads=3 owner_transfer=1 persisted_state=in_review final_projection_unchanged=true proxy_serve_joined=true")
}

func (w *m1Workflow) runBrowser(t *testing.T, private, harness, dist, evidence string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(w.app.ctx, 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "node", filepath.Join(harness, "node_modules/@playwright/test/cli.js"), "test", "--config", filepath.Join(harness, "m1-task-workflow.config.js"))
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
		workAppCheck(t, "m1-browser-child-cache", os.MkdirAll(path, 0700))
		cmd.Env = append(cmd.Env, name+"="+path)
	}
	cmd.Env = append(cmd.Env, "PLAYWRIGHT_NO_COPY_PROMPT=1", "AGENTEAM_AUTH_WEB_ORIGIN="+w.origin, "AGENTEAM_AUTH_WEB_PRIVATE="+private, "AGENTEAM_AUTH_WEB_CHROMIUM=/usr/bin/chromium", "AGENTEAM_M1_WEB_DIST="+dist, "AGENTEAM_M1_WEB_EVIDENCE="+evidence, "AGENTEAM_M1_WEB_CASE=read-write-run-review")
	var output bytes.Buffer
	cmd.Stdout, cmd.Stderr = &output, &output
	if err := cmd.Start(); err != nil {
		t.Fatal("M1 original browser Node did not start")
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
	err := <-done
	joined = true
	t.Logf("M1 browser Node actual_wait pid=%d success=%t", cmd.Process.Pid, err == nil)
	redacted := output.String()
	for _, secret := range []string{w.app.cookie, w.app.csrf} {
		if secret != "" {
			redacted = strings.ReplaceAll(redacted, secret, "[fixture-session-redacted]")
		}
	}
	workAppCheck(t, "m1-browser-console", os.WriteFile(filepath.Join(evidence, "m1-browser-console.log"), []byte(redacted), 0600))
	if err != nil || !regexp.MustCompile(`(?m)\b1 passed\b`).Match(output.Bytes()) {
		t.Fatal("M1 real browser did not pass its unique case; see private evidence console")
	}
	var result struct {
		Schema      string   `json:"schema"`
		Status      string   `json:"status"`
		Stages      []string `json:"stages"`
		APIWrites   int      `json:"api_writes"`
		OutputReads int      `json:"output_reads"`
	}
	raw, err := os.ReadFile(filepath.Join(evidence, "m1-task-workflow-observation.json"))
	workAppCheck(t, "m1-browser-observation", err)
	if len(raw) > 16<<10 || json.Unmarshal(raw, &result) != nil || result.Schema != "m1_browser_workflow_v1" || result.Status != "passed" || result.APIWrites != 1 || result.OutputReads != 3 || !slices.Equal(result.Stages, []string{
		"initial-task-in-progress", "v2-output-and-file-names-readable-without-task-transition", "explicit-owner-transfer-confirmed-in-review", "reload-preserves-in-review-and-identical-final-output",
	}) {
		t.Fatal("M1 browser completed-body and explicit-review evidence incomplete")
	}
}
