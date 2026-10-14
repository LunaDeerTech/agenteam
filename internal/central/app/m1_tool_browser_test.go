//go:build integration && linux

package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
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

	"github.com/LunaDeerTech/agenteam/internal/central/audit"
	"github.com/LunaDeerTech/agenteam/internal/central/config"
	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/httpapi"
	wc "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
)

// browser consumes the same completed App/Runner Task. It introduces no seed,
// alternate API or direct Task mutation. Observation retains original bytes.
func (w *m1Workflow) browser(t *testing.T, listener net.Listener) {
	t.Helper()
	w.browserWithExpectation(t, listener, "controlled")
}

func (w *m1Workflow) browserWithExpectation(t *testing.T, listener net.Listener, expectationMode string) {
	t.Helper()
	if expectationMode != "controlled" && expectationMode != "live" {
		t.Fatal("M1 browser expectation must be explicit controlled or live")
	}
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
	o := w.browserObservation
	if o == nil {
		t.Fatal("M1 original browser handler observation missing")
	}
	o.mu.Lock()
	o.evidence = evidence
	o.task = "/api/v1/projects/" + v.a.String() + "/tasks/" + w.task.ID.String()
	o.agents = "/api/v1/projects/" + v.a.String() + "/agents"
	o.execution = o.task + "/executions/" + w.execution.String()
	o.mu.Unlock()
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
	proxy.ModifyResponse = o.capture
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
	if item.SourceReference.ID != item.ID ||
		(expectationMode == "controlled" && item.Data.Content != m1FinalOutput) ||
		(expectationMode == "live" && strings.TrimSpace(item.Data.Content) == "") {
		t.Fatal("M1 actual final-round content or identity differs")
	}
	// Live artifacts and result semantics are checked independently by the Go
	// facts owner. Browser expectations come only from this original persisted
	// execution, without prescribing the model's program or final wording.
	artifacts := []map[string]string{{"path": "stats.py"}, {"path": "result.json"}}
	if expectationMode == "controlled" {
		artifacts[1]["expected_content"] = m1Result
	}
	payload := map[string]any{
		"mode": "read-write-run-review", "expectation_mode": expectationMode, "origin": w.origin, "cookie": v.cookie,
		"project_id": v.a.String(), "task_id": before.ID.String(), "agent_id": v.agent.String(), "reviewer_id": w.reviewer.String(),
		"execution_id": w.execution.String(), "final_round_id": item.ID.String(),
		"execution_started_at": view.Execution.StartedAt, "final_round_started_at": item.StartedAt, "execution_completed_at": view.Execution.CompletedAt,
		"route": "/" + identity.User.Username + "/runtime-app-a/tasks/" + before.ID.String(), "title": before.Title, "task_version": before.Version,
		"expected_output": item.Data.Content,
		"artifact_checks": artifacts,
	}
	raw, err := json.Marshal(payload)
	workAppCheck(t, "m1-browser-material", err)
	workAppCheck(t, "m1-browser-material-write", os.WriteFile(filepath.Join(private, "m1-task-workflow-material.json"), raw, 0600))
	clear(raw)
	delete(payload, "cookie")
	t.Cleanup(func() {
		if t.Failed() {
			// Registered after the proxy cleanup, so the original App, Runner and
			// owned peer are still alive when durable failure facts are read.
			if expectationMode == "controlled" {
				w.diagnoseResult(t, 1, w.execution.String(), string(w.view.Execution.Status))
			} else {
				w.diagnoseLiveResult(t)
			}
		}
	})
	w.runBrowser(t, private, harness, dist, evidence, expectationMode)
	o.mu.Lock()
	observed := !o.failed && o.accepted >= 11
	for _, tail := range o.tails {
		observed = observed && tail.returned
	}
	o.evidence = ""
	o.mu.Unlock()
	if !observed {
		t.Fatal("M1 original response bytes or handler return not joined")
	}

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
	t.Logf("M1_BROWSER_WORKFLOW expectation_mode=%s real_http=true output_reads=3 owner_transfer=1 persisted_state=in_review final_projection_unchanged=true proxy_serve_joined=true", expectationMode)
}

// This is the same bounded original-body/handler-return proof used by D22.
// Selection starts only for browser-owned public Task reads and one transfer;
// Session credentials and Runner websocket traffic never enter its records.
type m1BrowserObservation struct {
	mu                                      sync.Mutex
	host, evidence, task, agents, execution string
	tails                                   map[string]*artifactWebTail
	failed                                  bool
	accepted                                int
}

func (w *m1Workflow) bindBrowserObservation(d *dependencies) {
	u, _ := url.Parse(w.origin)
	o := &m1BrowserObservation{host: u.Host, tails: map[string]*artifactWebTail{}}
	w.browserObservation = o
	original := d.bind
	d.bind = func(ctx context.Context, cfg config.Config, store database, owned *resources, deps *dependencies) error {
		if err := original(ctx, cfg, store, owned, deps); err != nil {
			return err
		}
		assembly, ok := owned.accounts().(*accountAssembly)
		if !ok {
			return errors.New("M1 original account assembly absent")
		}
		objects := deps.objects
		deps.objects = func(ctx context.Context, cfg config.Config, store database, auditor *audit.Service) (objectStorage, error) {
			actual, err := objects(ctx, cfg, store, auditor)
			if err != nil {
				return actual, err
			}
			assembly.mu.Lock()
			assembly.handler = o.handler(assembly.handler)
			assembly.mu.Unlock()
			return actual, nil
		}
		return nil
	}
}

func (o *m1BrowserObservation) selected(method, path string) bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.evidence == "" {
		return false
	}
	return (method == http.MethodPost && path == o.task+"/transfer") ||
		(method == http.MethodGet && (path == o.task || path == o.agents || path == o.task+"/executions" || path == o.execution || path == o.execution+"/runtime-view"))
}

func (o *m1BrowserObservation) fail() error {
	o.mu.Lock()
	o.failed = true
	o.mu.Unlock()
	return errors.New("M1 original response observation failed")
}

func (o *m1BrowserObservation) handler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(out http.ResponseWriter, request *http.Request) {
		if !o.selected(request.Method, request.URL.Path) {
			next.ServeHTTP(out, request)
			return
		}
		rid := httpapi.RequestID(request.Context())
		if request.Host != o.host || rid.Validate() != nil {
			_ = o.fail()
			http.Error(out, "M1 request identity unavailable", 502)
			return
		}
		tail := &artifactWebTail{done: make(chan struct{}), method: request.Method, uri: request.URL.RequestURI()}
		o.mu.Lock()
		if o.tails[rid.String()] != nil || len(o.tails) >= 64 {
			o.mu.Unlock()
			_ = o.fail()
			http.Error(out, "M1 observation bound", 502)
			return
		}
		o.tails[rid.String()] = tail
		o.mu.Unlock()
		defer close(tail.done)
		next.ServeHTTP(out, request)
		o.mu.Lock()
		tail.returned = true
		o.mu.Unlock()
	})
}

func (o *m1BrowserObservation) capture(response *http.Response) error {
	if !o.selected(response.Request.Method, response.Request.URL.Path) {
		return nil
	}
	raw, readErr := io.ReadAll(io.LimitReader(response.Body, (2<<20)+1))
	closeErr := response.Body.Close()
	if readErr != nil || closeErr != nil || len(raw) > 2<<20 || response.ContentLength != int64(len(raw)) || !json.Valid(raw) || response.Header.Get("Cache-Control") != "no-store" || len(response.Header.Values("X-Request-ID")) != 1 {
		return o.fail()
	}
	rid, err := f.ParseID[f.Request](response.Header.Get("X-Request-ID"))
	if err != nil {
		return o.fail()
	}
	o.mu.Lock()
	tail := o.tails[rid.String()]
	evidence := o.evidence
	o.mu.Unlock()
	if tail == nil || tail.method != response.Request.Method || tail.uri != response.Request.URL.RequestURI() {
		return o.fail()
	}
	select {
	case <-tail.done:
	case <-response.Request.Context().Done():
		return o.fail()
	}
	if !tail.returned {
		return o.fail()
	}
	record := map[string]any{"request_id": rid.String(), "method": tail.method, "url_sha256": fmt.Sprintf("%x", sha256.Sum256([]byte(tail.uri))), "status": response.StatusCode, "content_length": len(raw), "body_sha256": fmt.Sprintf("%x", sha256.Sum256(raw)), "original_eof": true, "original_closed": true, "handler_returned": true, "request_host_matches": true, "value": json.RawMessage(raw)}
	encoded, err := json.Marshal(record)
	if err != nil || os.WriteFile(filepath.Join(evidence, "m1-response-"+rid.String()+".json"), encoded, 0600) != nil {
		return o.fail()
	}
	o.mu.Lock()
	o.accepted++
	o.mu.Unlock()
	response.Body = io.NopCloser(bytes.NewReader(raw))
	return nil
}

func (w *m1Workflow) runBrowser(t *testing.T, private, harness, dist, evidence, expectationMode string) {
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
		Schema          string   `json:"schema"`
		Status          string   `json:"status"`
		ExpectationMode string   `json:"expectation_mode"`
		Stages          []string `json:"stages"`
		APIWrites       int      `json:"api_writes"`
		OutputReads     int      `json:"output_reads"`
	}
	outputStage := "v2-output-and-file-names-readable-without-task-transition"
	if expectationMode == "live" {
		outputStage = "v2-output-readable-without-task-transition"
	}
	raw, err := os.ReadFile(filepath.Join(evidence, "m1-task-workflow-observation.json"))
	workAppCheck(t, "m1-browser-observation", err)
	if len(raw) > 16<<10 || json.Unmarshal(raw, &result) != nil || result.Schema != "m1_browser_workflow_v1" || result.Status != "passed" || result.ExpectationMode != expectationMode || result.APIWrites != 1 || result.OutputReads != 3 || !slices.Equal(result.Stages, []string{
		"initial-task-in-progress", outputStage, "explicit-owner-transfer-confirmed-in-review", "reload-preserves-in-review-and-identical-final-output",
	}) {
		t.Fatal("M1 browser completed-body and explicit-review evidence incomplete")
	}
}
