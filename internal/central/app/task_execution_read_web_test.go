//go:build integration

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
	"regexp"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/audit"
	"github.com/LunaDeerTech/agenteam/internal/central/config"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/httpapi"
)

func TestTaskExecutionReadWeb(t *testing.T) {
	t.Run("history-detail-refresh-and-owner", taskExecutionReadWeb)
}

func taskExecutionReadWeb(t *testing.T) {
	const prefix = "AGENTEAM_TASK_EXECUTION_READ_WEB"
	dist, runtime, evidence := os.Getenv(prefix+"_DIST"), os.Getenv("AGENTEAM_AUTH_WEB_RUNTIME"), os.Getenv(prefix+"_EVIDENCE")
	if !filepath.IsAbs(dist) || !filepath.IsAbs(runtime) || len(runtime) > 45 || !filepath.IsAbs(evidence) {
		t.Fatal("owned Execution browser inputs required")
	}
	if info, err := os.Lstat(filepath.Join(dist, "index.html")); err != nil || !info.Mode().IsRegular() {
		t.Fatal("Execution browser production assets missing")
	}
	evidence = filepath.Join(evidence, t.Name())
	workAppCheck(t, "execution-web-evidence-parent", os.MkdirAll(filepath.Dir(evidence), 0700))
	workAppCheck(t, "execution-web-fresh-evidence", os.Mkdir(evidence, 0700))
	private, err := os.MkdirTemp(runtime, "ew-")
	workAppCheck(t, "execution-web-private", err)
	t.Cleanup(func() { workAppCheck(t, "execution-web-private-cleanup", os.RemoveAll(private)) })
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	workAppCheck(t, "execution-web-listener", err)
	t.Cleanup(func() { _ = listener.Close() })
	origin := "http://" + listener.Addr().String()
	o := &taskExecutionWebObserver{host: listener.Addr().String(), private: private, evidence: evidence, tails: map[string]*artifactWebTail{}}
	t.Cleanup(func() { o.save(t) })
	v, _, task := taskExecutionReadFixture(t, origin, func(d *dependencies, _ *executionSchedulerApp) {
		original := d.bind
		d.bind = func(ctx context.Context, cfg config.Config, store database, owned *resources, deps *dependencies) error {
			if err := original(ctx, cfg, store, owned, deps); err != nil {
				return err
			}
			assembly, ok := owned.accounts().(*accountAssembly)
			if !ok {
				return errors.New("original Execution account assembly absent")
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
	})
	backend, err := url.Parse(v.address)
	workAppCheck(t, "execution-web-backend", err)
	transport := &http.Transport{Proxy: nil, DisableKeepAlives: true}
	proxy := httputil.NewSingleHostReverseProxy(backend)
	proxy.Transport = transport
	proxy.ErrorLog = log.New(io.Discard, "", 0)
	proxy.ModifyResponse = o.capture
	proxy.ErrorHandler = func(w http.ResponseWriter, _ *http.Request, _ error) {
		_ = o.fail("proxy")
		http.Error(w, "owned Execution observation failed", 502)
	}
	server := &http.Server{ReadHeaderTimeout: 2 * time.Second, ErrorLog: log.New(io.Discard, "", 0), Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			proxy.ServeHTTP(w, r)
			return
		}
		if r.Method != "GET" && r.Method != "HEAD" {
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
			_ = os.WriteFile(filepath.Join(private, "release-execution"), []byte("cleanup"), 0600)
			if err := server.Shutdown(v.ctx); err != nil {
				t.Error("Execution proxy exceeded original shutdown budget")
				_ = server.Close()
			}
			if err := <-served; err != nil && !errors.Is(err, http.ErrServerClosed) {
				t.Error("Execution proxy original Serve failed")
			}
			transport.CloseIdleConnections()
		})
	}
	t.Cleanup(stop)
	setup := &knowledgeWebSetup{t: t, ctx: v.ctx, origin: origin, cfg: v.root.cfg, check: func(stage string, err error) { t.Helper(); workAppCheck(t, "execution-web-"+stage, err) }}
	t.Cleanup(setup.close)
	// Original Account invitation/login produces the non-Owner cookie. This
	// account never becomes an Owner of the seeded Task's project.
	foreign, foreignCookie := artifactWebHuman(t, v.workAppFixture, setup, origin)
	if foreign.Details().UserID == v.actor.Details().UserID {
		t.Fatal("nonowner account reused owner identity")
	}
	taskExecutionRecoverForeignInvitation(t, v.workAppFixture, "artifact-owner", "artifact-owner@example.com")
	session := taskExecutionGet(t, v.workAppFixture, v.client, v.cookie, "GET", "/api/v1/session", 200)
	var identity struct {
		User struct {
			Username string `json:"username"`
		} `json:"user"`
	}
	workAppCheck(t, "execution-web-route-user", json.Unmarshal(session.body, &identity))
	clear(session.body)
	if identity.User.Username == "" {
		t.Fatal("formal Session username missing")
	}
	before := taskExecutionFacts(t, v.workAppFixture)
	list := taskExecutionReadPath(v.a, task.ID.String(), v.execution.ID)
	o.mu.Lock()
	o.detailPath = list
	o.active = true
	o.mu.Unlock()
	payload := map[string]any{"mode": "history-detail-refresh-and-owner", "origin": origin, "cookie": v.cookie, "foreign_cookie": foreignCookie, "project_id": v.a.String(), "task_id": task.ID.String(), "agent_id": v.agent.String(), "execution": taskExecutionExpected(v), "route": "/" + identity.User.Username + "/runtime-app-a/tasks/" + task.ID.String(), "title": task.Title, "task_state": task.State, "task_version": task.Version}
	raw, err := json.Marshal(payload)
	workAppCheck(t, "execution-web-private-material", err)
	workAppCheck(t, "execution-web-material-write", os.WriteFile(filepath.Join(private, "task-execution-read-material.json"), raw, 0600))
	clear(raw)
	result := taskExecutionBrowser(t, v.ctx, private, origin, dist, evidence)
	o.mu.Lock()
	failed, accepted, lists, details, denials, held := o.failed, o.accepted, o.lists, o.details, o.denials, o.loadingUsed
	o.mu.Unlock()
	if failed || !held || lists != 3 || details != 5 || denials != 1 || result["completed"] != true || result["execution_requests"] != float64(8) || result["original_requests"] != float64(accepted) {
		t.Fatal("Execution browser actual read/action closure differs")
	}
	taskExecutionReadClosed(t, v.workAppFixture, v.objects.process.String())
	if taskExecutionFacts(t, v.workAppFixture) != before {
		t.Fatal("Execution read browser changed original Task/Execution or invoked work")
	}
	stop()
	taskExecutionJoinEnabled(t, v)
	t.Log("TASK_EXECUTION_READ_WEB original_default_pair=true original_coordinator_handoff=true get8_write0=true independent_detail=true owner_denied_recovered=true original_handler_reader_consumer_joined=true manager_guard_db_joined=true no_model=true no_sql_seed=true")
}

type taskExecutionWebObserver struct {
	mu                                  sync.Mutex
	host, private, evidence, detailPath string
	active, failed, loadingUsed         bool
	stage                               string
	tails                               map[string]*artifactWebTail
	accepted, lists, details, denials   int
}

func (o *taskExecutionWebObserver) fail(stage string) error {
	o.mu.Lock()
	o.failed = true
	if o.stage == "" {
		o.stage = stage
	}
	o.mu.Unlock()
	return errors.New("original Execution web observation failed")
}
func (o *taskExecutionWebObserver) handler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		o.mu.Lock()
		selected := o.active && strings.HasPrefix(r.URL.Path, "/api/")
		o.mu.Unlock()
		if !selected {
			next.ServeHTTP(w, r)
			return
		}
		rid := httpapi.RequestID(r.Context())
		if r.Method != "GET" || r.Host != o.host || rid.Validate() != nil {
			_ = o.fail("request_identity")
			http.Error(w, "owned read rejected", 502)
			return
		}
		tail := &artifactWebTail{done: make(chan struct{}), method: r.Method, uri: r.URL.RequestURI()}
		o.mu.Lock()
		if o.tails[rid.String()] != nil || len(o.tails) >= 64 {
			o.mu.Unlock()
			_ = o.fail("request_bound")
			http.Error(w, "owned read bound", 502)
			return
		}
		o.tails[rid.String()] = tail
		held := r.URL.Path == o.detailPath && !o.loadingUsed
		if held {
			o.loadingUsed = true
		}
		o.mu.Unlock()
		defer close(tail.done)
		if held {
			if os.WriteFile(filepath.Join(o.private, "reached-execution"), []byte("held"), 0600) != nil {
				_ = o.fail("loading_marker")
				return
			}
			timer := time.NewTicker(10 * time.Millisecond)
			defer timer.Stop()
			for {
				_, err := os.Stat(filepath.Join(o.private, "release-execution"))
				if err == nil {
					break
				}
				if !errors.Is(err, os.ErrNotExist) {
					_ = o.fail("loading_release")
					return
				}
				select {
				case <-timer.C:
				case <-r.Context().Done():
					_ = o.fail("loading_cancelled")
					return
				}
			}
		}
		next.ServeHTTP(w, r)
		o.mu.Lock()
		tail.returned = true
		o.mu.Unlock()
	})
}
func (o *taskExecutionWebObserver) capture(response *http.Response) error {
	o.mu.Lock()
	selected := o.active && strings.HasPrefix(response.Request.URL.Path, "/api/")
	o.mu.Unlock()
	if !selected {
		return nil
	}
	raw, readErr := io.ReadAll(io.LimitReader(response.Body, (2<<20)+1))
	closeErr := response.Body.Close()
	if readErr != nil || closeErr != nil || len(raw) > 2<<20 || response.ContentLength != int64(len(raw)) || !json.Valid(raw) || response.Header.Get("Cache-Control") != "no-store" || len(response.Header.Values("X-Request-ID")) != 1 {
		return o.fail("response_integrity")
	}
	rid, err := f.ParseID[f.Request](response.Header.Get("X-Request-ID"))
	if err != nil {
		return o.fail("response_identity")
	}
	o.mu.Lock()
	tail := o.tails[rid.String()]
	o.mu.Unlock()
	if tail == nil || tail.uri != response.Request.URL.RequestURI() || tail.method != response.Request.Method {
		return o.fail("handler_correlation")
	}
	select {
	case <-tail.done:
	case <-response.Request.Context().Done():
		return o.fail("handler_cancelled")
	}
	if !tail.returned {
		return o.fail("handler_not_returned")
	}
	execution := strings.Contains(response.Request.URL.Path, "/executions")
	if response.StatusCode != 200 && (!execution || response.StatusCode != 404) {
		return o.fail("status")
	}
	if response.StatusCode == 404 {
		var problem httpapi.Problem
		if skillWebDecode(raw, &problem) != nil || problem.Code != f.NotFound {
			return o.fail("problem")
		}
	}
	record := map[string]any{"request_id": rid.String(), "method": "GET", "url_sha256": fmt.Sprintf("%x", sha256.Sum256([]byte(tail.uri))), "status": response.StatusCode, "content_length": len(raw), "body_sha256": fmt.Sprintf("%x", sha256.Sum256(raw)), "original_eof": true, "original_closed": true, "handler_returned": true, "request_host_matches": true}
	encoded, err := json.Marshal(record)
	if err != nil {
		return o.fail("evidence_encode")
	}
	if os.WriteFile(filepath.Join(o.evidence, "task-execution-read-response-"+rid.String()+".json"), encoded, 0600) != nil {
		return o.fail("evidence_write")
	}
	o.mu.Lock()
	o.accepted++
	if execution {
		if strings.HasSuffix(response.Request.URL.Path, "/executions") {
			o.lists++
		} else {
			o.details++
		}
		if response.StatusCode == 404 {
			o.denials++
		}
	}
	o.mu.Unlock()
	response.Body = io.NopCloser(bytes.NewReader(raw))
	return nil
}
func (o *taskExecutionWebObserver) save(t *testing.T) {
	t.Helper()
	o.mu.Lock()
	returned := 0
	for _, tail := range o.tails {
		if tail.returned {
			returned++
		}
	}
	raw, err := json.Marshal(map[string]any{"failed": o.failed, "stage": o.stage, "accepted": o.accepted, "handler_started": len(o.tails), "handler_returned": returned, "lists": o.lists, "details": o.details, "denials": o.denials, "loading_held": o.loadingUsed})
	o.mu.Unlock()
	if err != nil || os.WriteFile(filepath.Join(o.evidence, "task-execution-read-driver.json"), raw, 0600) != nil {
		t.Error("Execution driver diagnostic write failed")
	}
}

func taskExecutionBrowser(t *testing.T, parent context.Context, private, origin, dist, evidence string) map[string]any {
	t.Helper()
	harness := os.Getenv("AGENTEAM_TASK_EXECUTION_READ_WEB_HARNESS")
	if !filepath.IsAbs(harness) {
		t.Fatal("owned Execution browser harness required")
	}
	ctx, cancel := context.WithTimeout(parent, 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "node", filepath.Join(harness, "node_modules/@playwright/test/cli.js"), "test", "--config", filepath.Join(harness, "task-execution-read.config.js"))
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
		workAppCheck(t, "execution-web-child-cache", os.MkdirAll(path, 0700))
		cmd.Env = append(cmd.Env, name+"="+path)
	}
	cmd.Env = append(cmd.Env, "PLAYWRIGHT_NO_COPY_PROMPT=1", "AGENTEAM_AUTH_WEB_ORIGIN="+origin, "AGENTEAM_AUTH_WEB_PRIVATE="+private, "AGENTEAM_AUTH_WEB_CHROMIUM=/usr/bin/chromium", "AGENTEAM_TASK_EXECUTION_READ_WEB_DIST="+dist, "AGENTEAM_TASK_EXECUTION_READ_WEB_EVIDENCE="+evidence, "AGENTEAM_TASK_EXECUTION_READ_WEB_CASE=history-detail-refresh-and-owner")
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	if err := cmd.Start(); err != nil {
		t.Fatal("Execution original Node did not start")
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
	t.Logf("TaskExecution Node actual_wait pid=%d success=%t", cmd.Process.Pid, err == nil)
	if err != nil || !regexp.MustCompile(`(?m)\b1 passed\b`).Match(output.Bytes()) {
		t.Fatal("Execution real browser did not pass its one case")
	}
	raw, err := os.ReadFile(filepath.Join(private, "task-execution-read-result.json"))
	workAppCheck(t, "execution-web-result", err)
	var result map[string]any
	if len(raw) > 16<<10 || json.Unmarshal(raw, &result) != nil {
		t.Fatal("Execution browser result invalid")
	}
	return result
}
