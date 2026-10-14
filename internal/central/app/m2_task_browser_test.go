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
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	ac "github.com/LunaDeerTech/agenteam/internal/central/artifact/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/audit"
	"github.com/LunaDeerTech/agenteam/internal/central/config"
	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/httpapi"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	wc "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
	netfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/outbound"
)

// The browser owns the accept/return commands. Worker publication and rework
// use the same App, Runner, Tool, Object and Scheduler as the HTTP workflow.
func TestM2AppArtifactBrowserReview(t *testing.T) {
	for _, mode := range []string{"accept", "return-and-accept"} {
		t.Run(mode, func(t *testing.T) {
			o := &m2BrowserObservation{paths: map[string]bool{}, tails: map[string]*artifactWebTail{}}
			w, listener := newM1WorkflowConfigured(t, nil, true, nil, o.bind)
			w.seed(t)
			w.startRunner(t)
			var toolID string
			workAppCheck(t, "m2-browser-builtin", w.app.admin.QueryRow(w.app.ctx, `SELECT tool_id FROM agenteam_tool.identities WHERE stable_key='builtin:create-artifact'`).Scan(&toolID))
			tool, err := f.ParseID[id.Tool](toolID)
			workAppCheck(t, "m2-browser-builtin-id", err)
			w.tools["builtin:create-artifact"] = tool
			content := "{\"count\":4,\"sum\":20,\"mean\":5}\n"
			w.responseSequence = func(t *testing.T) []netfixture.WireScenario {
				return m2ArtifactResponses(t, w, "m2-result.json", content)
			}
			w.createModelAndTask(t, "https://fixture.test:8443/m2-browser-not-yet-scheduled")
			w.app.store.releaseDirectory()
			m2AwaitTaskResult(t, w, 1)
			m2TaskScheduler(t, w, false)
			artifact := m2BrowserArtifact(t, w, "m2-result.json", content)
			m2OwnerTaskTransfer(t, w, wc.TaskStateInReview, &w.reviewer, "已读取真实成果，提交人工审核。")
			firstScenario := w.scenario
			w.m2Browser(t, listener, o, mode, artifact, content)
			returned := mode == "return-and-accept"
			m2AssertHumanReviewHistory(t, w, returned)
			if returned {
				w.joinedPeer(t, firstScenario, 2, 2)
			}
			m2StopApproval(t, w, 2)
			t.Logf("M2_ARTIFACT_BROWSER_REVIEW mode=%s original_http=true actual_artifact=true browser_owner_commands=true durable_done=true external_model=false", mode)
		})
	}
}

func m2BrowserArtifact(t *testing.T, w *m1Workflow, name, content string) m2ArtifactItem {
	t.Helper()
	ref := m2ReadWorkerArtifact(t, w, w.execution, name, content)
	var page struct {
		Items []m2ArtifactItem `json:"items"`
	}
	m2TaskRequest(t, w, http.MethodGet, "/artifacts?"+url.Values{"execution_id": {w.execution.String()}, "name_query": {name}}.Encode(), nil, "", &page)
	if len(page.Items) != 1 || page.Items[0].Reference != ref {
		t.Fatal("browser material lost its original Artifact")
	}
	return page.Items[0]
}

type m2BrowserMaterial struct {
	Mode            string         `json:"mode"`
	Origin          string         `json:"origin"`
	Cookie          string         `json:"cookie"`
	Route           string         `json:"route"`
	Title           string         `json:"title"`
	Project         string         `json:"project_id"`
	Task            string         `json:"task_id"`
	Version         string         `json:"task_version"`
	Worker          string         `json:"worker_id"`
	Reviewer        string         `json:"reviewer_id"`
	Execution       string         `json:"work_execution_id"`
	Operation       string         `json:"source_operation_id"`
	Artifact        ac.ArtifactRef `json:"artifact_ref"`
	Name            string         `json:"name"`
	SHA256          string         `json:"sha256"`
	ExpectedContent string         `json:"expected_content"`
	ReturnComment   string         `json:"return_comment,omitempty"`
	DeadlineUnixMS  int64          `json:"deadline_unix_ms,omitempty"`
}

func (w *m1Workflow) m2Browser(t *testing.T, listener net.Listener, o *m2BrowserObservation, mode string, artifact m2ArtifactItem, content string) {
	t.Helper()
	dist, evidenceRoot := os.Getenv("AGENTEAM_M2_WEB_DIST"), os.Getenv("AGENTEAM_M2_WEB_EVIDENCE")
	harness := os.Getenv("AGENTEAM_M2_WEB_HARNESS")
	if harness == "" {
		harness = os.Getenv("AGENTEAM_M1_WEB_HARNESS")
	}
	if !filepath.IsAbs(dist) || !filepath.IsAbs(harness) || !filepath.IsAbs(evidenceRoot) {
		t.Fatal("M2 owned production assets, browser harness and evidence required")
	}
	for _, path := range []string{filepath.Join(dist, "index.html"), filepath.Join(harness, "m2-task-review.config.js"), filepath.Join(harness, "node_modules/@playwright/test/cli.js")} {
		if info, err := os.Stat(path); err != nil || !info.Mode().IsRegular() {
			t.Fatal("M2 browser source or production asset missing")
		}
	}
	evidence := filepath.Join(evidenceRoot, t.Name())
	workAppCheck(t, "m2-browser-evidence-parent", os.MkdirAll(filepath.Dir(evidence), 0700))
	workAppCheck(t, "m2-browser-fresh-evidence", os.Mkdir(evidence, 0700))
	private, err := os.MkdirTemp("/tmp", "agenteam-m2-web-")
	workAppCheck(t, "m2-browser-private", err)
	t.Cleanup(func() { workAppCheck(t, "m2-browser-private-cleanup", os.RemoveAll(private)) })
	stop := w.m2BrowserProxy(t, listener, o, dist)
	t.Cleanup(stop)
	var identity struct {
		User struct {
			Username string `json:"username"`
		} `json:"user"`
	}
	session := taskExecutionGet(t, w.app.workAppFixture, w.app.client, w.app.cookie, http.MethodGet, "/api/v1/session", http.StatusOK)
	workAppCheck(t, "m2-browser-session", json.Unmarshal(session.body, &identity))
	clear(session.body)
	if identity.User.Username == "" {
		t.Fatal("M2 formal Session username missing")
	}
	material := m2BrowserMaterial{Mode: mode, Origin: w.origin, Cookie: w.app.cookie,
		Route: "/" + identity.User.Username + "/runtime-app-a/tasks/" + w.task.ID.String(),
		Title: w.task.Title, Project: w.app.a.String(), Task: w.task.ID.String(), Worker: w.app.agent.String(), Reviewer: w.reviewer.String()}
	if w.m2Live != nil {
		material.ReturnComment, material.DeadlineUnixMS = m2LiveReturnComment, w.live.deadline.UnixMilli()
	}
	publish := func(item m2ArtifactItem, body string, next bool) {
		t.Helper()
		if w.m2Live != nil {
			w.m2AwaitLiveReview(t, item, body)
		}
		current, err := w.app.planning.taskReader.GetTask(w.app.ctx, w.app.actor, w.app.a, w.task.ID)
		workAppCheck(t, "m2-browser-current-review", err)
		if !reflect.DeepEqual(current, w.task) || current.State != wc.TaskStateInReview || current.AssigneeAgentID == nil || *current.AssigneeAgentID != w.reviewer || item.Execution != w.execution.String() {
			t.Fatal("M2 browser material is not the actual current review")
		}
		material.Version = strconv.FormatInt(int64(current.Version), 10)
		material.Execution, material.Operation, material.Artifact = item.Execution, item.Operation, item.Reference
		material.Name, material.ExpectedContent = item.Name, body
		material.SHA256 = fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(body)))
		o.allow(w, item.Reference, evidence)
		name := "m2-task-review-material.json"
		if next {
			name = "m2-task-review-next-material.json"
		}
		raw, err := json.Marshal(material)
		workAppCheck(t, "m2-browser-material-json", err)
		temporary := filepath.Join(private, name+".pending")
		workAppCheck(t, "m2-browser-material-private-write", os.WriteFile(temporary, raw, 0600))
		clear(raw)
		workAppCheck(t, "m2-browser-material-publish", os.Rename(temporary, filepath.Join(private, name)))
	}
	// Later work decodes into w.view and JSON reuses its pointer fields. Keep
	// an independent original projection for the final immutability check.
	var firstView ec.TaskRuntimeView
	workAppCheck(t, "m2-browser-preserve-original-execution", json.Unmarshal(workAppJSON(t, w.view), &firstView))
	publish(artifact, content, false)
	var advance func(<-chan struct{})
	if mode == "return-and-accept" {
		revised := "{\"count\":4,\"sum\":20,\"mean\":5,\"verified\":true}\n"
		if w.m2Live == nil {
			w.scenario = m2SetWorkerResponses(t, w, m2ArtifactResponses(t, w, "m2-revised.json", revised))
		}
		advance = func(nodeDone <-chan struct{}) {
			m2AwaitBrowserReturn(t, w, nodeDone)
			if w.m2Live == nil {
				m2TaskScheduler(t, w, true)
			}
			m2AwaitTaskResult(t, w, 2)
			var latest m2ArtifactItem
			if w.m2Live == nil {
				m2TaskScheduler(t, w, false)
				latest = m2BrowserArtifact(t, w, "m2-revised.json", revised)
			} else {
				latest, revised = w.m2LiveArtifact(t, true)
			}
			if latest.Execution == artifact.Execution || latest.Operation == artifact.Operation || latest.Reference.ArtifactID == artifact.Reference.ArtifactID {
				t.Fatal("browser rework did not create a new original execution and Artifact")
			}
			m2ReadArtifactContent(t, w, artifact.Reference, content)
			comment := "已读取新执行的修订成果，重新提交人工审核。"
			if w.m2Live != nil {
				comment = m2LiveReviewComment(t, latest)
			}
			m2OwnerTaskTransfer(t, w, wc.TaskStateInReview, &w.reviewer, comment)
			publish(latest, revised, true)
		}
	}
	w.m2RunBrowser(t, private, harness, dist, evidence, mode, advance)
	material.Cookie = ""
	o.mu.Lock()
	observed := !o.failed && o.accepted >= 10
	for _, tail := range o.tails {
		observed = observed && tail.returned
	}
	o.evidence = ""
	o.mu.Unlock()
	if !observed {
		t.Fatal("M2 original response bytes or handler return were not joined")
	}
	before := w.task
	m2TaskRequest(t, w, http.MethodGet, "/tasks/"+before.ID.String(), nil, "", &w.task)
	if w.task.State != wc.TaskStateDone || w.task.Version != before.Version+1 || w.task.AssigneeAgentID == nil || *w.task.AssigneeAgentID != w.reviewer {
		t.Fatal("browser acceptance did not persist exactly one Owner done transition")
	}
	for _, view := range []ec.TaskRuntimeView{firstView, w.view} {
		response := taskExecutionGet(t, w.app.workAppFixture, w.app.client, w.app.cookie, http.MethodGet, taskExecutionReadPath(w.app.a, w.task.ID.String(), view.Execution.ID)+"/runtime-view", http.StatusOK)
		var actual ec.TaskRuntimeView
		workAppCheck(t, "m2-browser-original-execution", json.Unmarshal(response.body, &actual))
		clear(response.body)
		if !reflect.DeepEqual(actual, view) {
			t.Fatal("browser review changed a completed original Execution projection")
		}
	}
	stop()
}

func m2AwaitBrowserReturn(t *testing.T, w *m1Workflow, nodeDone <-chan struct{}) {
	t.Helper()
	if w.m2Live != nil {
		w.m2AwaitLiveBrowserReturn(t, nodeDone)
		return
	}
	before := w.task
	ctx, cancel := context.WithTimeout(w.app.ctx, 20*time.Second)
	defer cancel()
	tick := time.NewTicker(25 * time.Millisecond)
	defer tick.Stop()
	for {
		current, err := w.app.planning.taskReader.GetTask(ctx, w.app.actor, w.app.a, before.ID)
		workAppCheck(t, "m2-browser-return-current", err)
		if current.State == wc.TaskStateTodo {
			if current.ID != before.ID || current.Version != before.Version+1 || current.AssigneeAgentID == nil || *current.AssigneeAgentID != w.app.agent {
				t.Fatal("browser return changed the original Task or worker")
			}
			w.task = current
			return
		}
		if !reflect.DeepEqual(current, before) {
			t.Fatal("unexpected Task change before browser return")
		}
		select {
		case <-nodeDone:
			t.Fatal("original browser Node exited before its Owner return")
		case <-ctx.Done():
			t.Fatal("browser Owner return did not reach the original Task")
		case <-tick.C:
		}
	}
}

func (w *m1Workflow) m2BrowserProxy(t *testing.T, listener net.Listener, o *m2BrowserObservation, dist string) func() {
	t.Helper()
	backend, err := url.Parse(w.app.address)
	workAppCheck(t, "m2-browser-backend", err)
	transport := &http.Transport{Proxy: nil, DisableKeepAlives: true}
	proxy := httputil.NewSingleHostReverseProxy(backend)
	proxy.Transport, proxy.ModifyResponse = transport, o.capture
	proxy.ErrorLog = log.New(io.Discard, "", 0)
	proxy.ErrorHandler = func(out http.ResponseWriter, _ *http.Request, err error) {
		o.mu.Lock()
		stage, status := o.failureStage, o.failureStatus
		o.mu.Unlock()
		if stage == "" {
			stage = "transport"
		}
		var networkError net.Error
		t.Logf("M2_BROWSER_PROXY_FAILURE stage=%s original_status=%d error_type=%T eof=%t cancelled=%t deadline=%t network_timeout=%t", stage, status, err, errors.Is(err, io.EOF), errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded), errors.As(err, &networkError) && networkError.Timeout())
		http.Error(out, "M2 original App unavailable", http.StatusBadGateway)
	}
	server := &http.Server{ReadHeaderTimeout: 2 * time.Second, ErrorLog: log.New(io.Discard, "", 0), Handler: http.HandlerFunc(func(out http.ResponseWriter, request *http.Request) {
		if strings.HasPrefix(request.URL.Path, "/api/") {
			proxy.ServeHTTP(out, request)
		} else if request.Method != http.MethodGet && request.Method != http.MethodHead {
			http.NotFound(out, request)
		} else if strings.HasPrefix(request.URL.Path, "/assets/") {
			skillWebServeAsset(out, request, dist)
		} else {
			http.ServeFile(out, request, filepath.Join(dist, "index.html"))
		}
	})}
	served := make(chan error, 1)
	go func() { served <- server.Serve(listener) }()
	var once sync.Once
	return func() {
		once.Do(func() {
			ctx, cancel := context.WithTimeout(w.app.ctx, 2*time.Second)
			defer cancel()
			if err := server.Shutdown(ctx); err != nil {
				t.Error("M2 browser proxy exceeded original shutdown budget")
				_ = server.Close()
			}
			if err := <-served; err != nil && !errors.Is(err, http.ErrServerClosed) {
				t.Error("M2 browser proxy original Serve failed")
			}
			transport.CloseIdleConnections()
		})
	}
}

// This observes the original App handler, including both actual Artifact
// identities during rework. Session and Runner websocket traffic pass unchanged.
type m2BrowserObservation struct {
	mu                   sync.Mutex
	host, evidence, task string
	paths                map[string]bool
	tails                map[string]*artifactWebTail
	failed               bool
	accepted             int
	failureStage         string
	failureStatus        int
}

func (o *m2BrowserObservation) bind(w *m1Workflow, d *dependencies) {
	u, _ := url.Parse(w.origin)
	o.host = u.Host
	original := d.bind
	d.bind = func(ctx context.Context, cfg config.Config, store database, owned *resources, deps *dependencies) error {
		if err := original(ctx, cfg, store, owned, deps); err != nil {
			return err
		}
		assembly, ok := owned.accounts().(*accountAssembly)
		if !ok {
			return errors.New("M2 original account assembly absent")
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
func (o *m2BrowserObservation) allow(w *m1Workflow, ref ac.ArtifactRef, evidence string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	project := "/api/v1/projects/" + w.app.a.String()
	o.evidence, o.task = evidence, project+"/tasks/"+w.task.ID.String()
	execution := o.task + "/executions/" + w.execution.String()
	for _, path := range []string{o.task, o.task + "/executions", execution, execution + "/runtime-view", project + "/agents", project + "/artifacts", project + "/artifacts/" + ref.ArtifactID.String() + "/files/" + ref.FileID.String() + "/content"} {
		o.paths[path] = true
	}
}
func (o *m2BrowserObservation) selected(method, path string) bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.evidence != "" && ((method == http.MethodPost && path == o.task+"/transfer") || (method == http.MethodGet && o.paths[path]))
}
func (o *m2BrowserObservation) fail(stage string, status int) error {
	o.mu.Lock()
	o.failed = true
	if o.failureStage == "" {
		o.failureStage, o.failureStatus = stage, status
	}
	o.mu.Unlock()
	return errors.New("M2 original response observation failed")
}
func (o *m2BrowserObservation) handler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(out http.ResponseWriter, request *http.Request) {
		if !o.selected(request.Method, request.URL.Path) {
			next.ServeHTTP(out, request)
			return
		}
		rid := httpapi.RequestID(request.Context())
		if request.Host != o.host || rid.Validate() != nil {
			_ = o.fail("request_identity", 0)
			http.Error(out, "M2 original request identity unavailable", http.StatusBadGateway)
			return
		}
		tail := &artifactWebTail{done: make(chan struct{}), method: request.Method, uri: request.URL.RequestURI()}
		o.mu.Lock()
		if o.tails[rid.String()] != nil || len(o.tails) >= 128 {
			o.mu.Unlock()
			_ = o.fail("observation_bound", 0)
			http.Error(out, "M2 observation bound", http.StatusBadGateway)
			return
		}
		o.tails[rid.String()] = tail
		o.mu.Unlock()
		completed := false
		defer func() {
			if !completed {
				_ = o.fail("original_handler_aborted", 0)
			}
			close(tail.done)
		}()
		next.ServeHTTP(out, request)
		o.mu.Lock()
		tail.returned = true
		o.mu.Unlock()
		completed = true
	})
}
func (o *m2BrowserObservation) capture(response *http.Response) error {
	if !o.selected(response.Request.Method, response.Request.URL.Path) {
		return nil
	}
	raw, readErr := io.ReadAll(io.LimitReader(response.Body, (2<<20)+1))
	closeErr := response.Body.Close()
	checks := []struct {
		stage string
		valid bool
	}{
		{"body_read", readErr == nil}, {"body_close", closeErr == nil},
		{"body_bound", len(raw) <= 2<<20}, {"content_length", response.ContentLength == int64(len(raw))},
		{"json", json.Valid(raw)}, {"cache_control", response.Header.Get("Cache-Control") == "no-store"},
		{"request_id_count", len(response.Header.Values("X-Request-ID")) == 1},
	}
	for _, check := range checks {
		if !check.valid {
			return o.fail(check.stage, response.StatusCode)
		}
	}
	rid, err := f.ParseID[f.Request](response.Header.Get("X-Request-ID"))
	if err != nil {
		return o.fail("request_id_parse", response.StatusCode)
	}
	o.mu.Lock()
	tail, evidence := o.tails[rid.String()], o.evidence
	o.mu.Unlock()
	if tail == nil || tail.method != response.Request.Method || tail.uri != response.Request.URL.RequestURI() {
		return o.fail("original_tail_identity", response.StatusCode)
	}
	select {
	case <-tail.done:
	case <-response.Request.Context().Done():
		return o.fail("original_tail_cancelled", response.StatusCode)
	}
	if !tail.returned {
		return o.fail("original_tail_return", response.StatusCode)
	}
	record := map[string]any{"request_id": rid.String(), "method": tail.method, "url_sha256": fmt.Sprintf("%x", sha256.Sum256([]byte(tail.uri))), "status": response.StatusCode, "content_length": len(raw), "body_sha256": fmt.Sprintf("%x", sha256.Sum256(raw)), "original_eof": true, "original_closed": true, "handler_returned": true, "request_host_matches": true, "value": json.RawMessage(raw)}
	encoded, err := json.Marshal(record)
	if err != nil || os.WriteFile(filepath.Join(evidence, "m2-response-"+rid.String()+".json"), encoded, 0600) != nil {
		return o.fail("evidence_write", response.StatusCode)
	}
	o.mu.Lock()
	o.accepted++
	o.mu.Unlock()
	response.Body = io.NopCloser(bytes.NewReader(raw))
	return nil
}

func (w *m1Workflow) m2RunBrowser(t *testing.T, private, harness, dist, evidence, mode string, advance func(<-chan struct{})) {
	t.Helper()
	deadline := time.Now().Add(60 * time.Second)
	if w.m2Live != nil {
		deadline = w.live.deadline
	}
	ctx, cancel := context.WithDeadline(w.app.ctx, deadline)
	defer cancel()
	cmd := exec.CommandContext(ctx, "node", filepath.Join(harness, "node_modules/@playwright/test/cli.js"), "test", "--config", filepath.Join(harness, "m2-task-review.config.js"))
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
		workAppCheck(t, "m2-browser-child-cache", os.MkdirAll(path, 0700))
		cmd.Env = append(cmd.Env, name+"="+path)
	}
	cmd.Env = append(cmd.Env, "PLAYWRIGHT_NO_COPY_PROMPT=1", "AGENTEAM_AUTH_WEB_ORIGIN="+w.origin, "AGENTEAM_AUTH_WEB_PRIVATE="+private, "AGENTEAM_AUTH_WEB_CHROMIUM=/usr/bin/chromium", "AGENTEAM_M2_WEB_DIST="+dist, "AGENTEAM_M2_WEB_EVIDENCE="+evidence, "AGENTEAM_M2_WEB_CASE="+mode)
	var output bytes.Buffer
	cmd.Stdout, cmd.Stderr = &output, &output
	if err := cmd.Start(); err != nil {
		t.Fatal("M2 original browser Node did not start")
	}
	done := make(chan struct{})
	var waitErr error
	go func() { waitErr = cmd.Wait(); close(done) }()
	joined := false
	defer func() {
		if !joined {
			_ = cmd.Cancel()
			<-done
		}
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		cmd.Env = nil
		redacted := output.String()
		for _, secret := range []string{w.app.cookie, w.app.csrf} {
			if secret != "" {
				redacted = strings.ReplaceAll(redacted, secret, "[fixture-session-redacted]")
			}
		}
		workAppCheck(t, "m2-browser-console", os.WriteFile(filepath.Join(evidence, "m2-browser-console.log"), []byte(redacted), 0600))
		t.Logf("M2 browser Node actual_wait pid=%d success=%t", cmd.Process.Pid, waitErr == nil)
	}()
	if advance != nil {
		advance(done)
	}
	<-done
	joined = true
	if waitErr != nil || !regexp.MustCompile(`(?m)\b1 passed\b`).Match(output.Bytes()) {
		t.Fatal("M2 real browser did not pass its unique case; see private evidence console")
	}
	var result struct {
		Schema        string   `json:"schema"`
		Status        string   `json:"status"`
		Mode          string   `json:"mode"`
		Stages        []string `json:"stages"`
		APIWrites     int      `json:"api_writes"`
		ArtifactReads int      `json:"artifact_reads"`
	}
	stages := []string{"initial-real-task-in-review", "original-work-artifact-provenance-and-content-readable"}
	writes, reads := 1, 2
	if mode == "return-and-accept" {
		stages = append(stages, "explicit-owner-return-to-same-task-and-worker", "same-task-new-execution-and-artifact-after-real-rework")
		writes, reads = 2, 3
	}
	stages = append(stages, "explicit-owner-accept-confirmed-done", "reload-preserves-done-and-original-worker-artifact")
	raw, err := os.ReadFile(filepath.Join(evidence, "m2-task-review-observation.json"))
	workAppCheck(t, "m2-browser-observation", err)
	if len(raw) > 16<<10 || json.Unmarshal(raw, &result) != nil || result.Schema != "m2_browser_review_v1" || result.Status != "passed" || result.Mode != mode || result.APIWrites != writes || result.ArtifactReads != reads || !slices.Equal(result.Stages, stages) {
		t.Fatal("M2 actual Artifact read and Owner command evidence incomplete")
	}
}
