//go:build integration

package projectvariable_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/app"
	eventc "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/httpapi"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	wc "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
	"github.com/LunaDeerTech/agenteam/internal/platform/logging"
)

// Real default App, original Account owner and formal HTTP seeds. No fault injection.
func TestTaskDependencyWeb(t *testing.T) {
	for _, mode := range []string{"dependency-current-read"} {
		t.Run(mode, func(t *testing.T) {
			entered, completed := false, false
			t.Cleanup(func() {
				if !entered || !completed {
					t.Error("Task Dependency original App handoff incomplete")
				}
			})
			a := newAgentCreateFixture(t)
			original := a.p2.base
			project, err := a.p2.creator.GetSchedulerConfig(ctxFor(t), original.ownerBrowser.actor, a.p2.project.ID)
			agentAppRequire(t, err)
			if project.Config.Enabled || project.Project.CurrentSprintID != nil {
				t.Fatal("original Project must be paused without a current Sprint")
			}
			process, err := a.guard.CurrentProcess()
			agentAppRequire(t, err)
			var username string
			agentAppRequire(t, original.raw.QueryRow(ctxFor(t), `SELECT username FROM agenteam_account.users WHERE id=$1`, original.ownerBrowser.actor.Details().UserID).Scan(&username))
			if original.afterOwners != nil {
				t.Fatal("original handoff occupied")
			}
			original.afterOwners = func() {
				entered = true
				if !original.core.Joined() || !a.agents.Joined() || !a.p2.service.Joined() {
					t.Fatal("original setup owners not joined")
				}
				if _, err := a.guard.CurrentProcess(); err == nil {
					t.Fatal("original guard still held")
				}
				var stopped bool
				agentAppRequire(t, original.raw.QueryRow(ctxFor(t), `SELECT state='stopped' AND stopped_at IS NOT NULL FROM agenteam_object.process_claims WHERE process_id=$1`, process.String()).Scan(&stopped))
				if !stopped {
					t.Fatal("original process not retired")
				}
				w := newTaskDependencyWeb(t, a, project, mode)
				defer w.dispose(t)
				withTaskDependencyApp(t, a, process.String(), w.origin, func(ctx context.Context, address string, logs *agentAppLog) {
					w.logs = logs
					defer w.close(t)
					w.start(t, address)
					w.prepare(t, ctx)
					w.material(t, username)
					w.browser(t, ctx)
					w.verify(t, ctx)
					w.close(t)
				})
				completed = true
			}
		})
	}
}

func newTaskDependencyWeb(t *testing.T, a *agentCreateFixture, project pc.SchedulerProject, mode string) *taskDependencyWeb {
	t.Helper()
	const prefix = "AGENTEAM_TASK_DEPENDENCY_WEB"
	dist, runtime, evidence, inputHash := os.Getenv(prefix+"_DIST"), os.Getenv("AGENTEAM_AUTH_WEB_RUNTIME"), os.Getenv(prefix+"_EVIDENCE"), os.Getenv(prefix+"_INPUT_HASH")
	if !filepath.IsAbs(dist) || !filepath.IsAbs(runtime) || len(runtime) > 45 || !filepath.IsAbs(evidence) || len(inputHash) != 64 || strings.Trim(inputHash, "0123456789abcdef") != "" {
		t.Fatal("frozen Task Dependency inputs required")
	}
	if info, err := os.Stat(filepath.Join(dist, "index.html")); err != nil || !info.Mode().IsRegular() {
		t.Fatal("frozen index missing")
	}
	directory, err := os.MkdirTemp(runtime, "tdw-")
	agentAppRequire(t, err)
	published := false
	defer func() {
		if !published {
			_ = os.RemoveAll(directory)
		}
	}()
	agentAppRequire(t, os.Chmod(directory, 0700))
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	agentAppRequire(t, err)
	base := &agentConfigurationWeb{a: a, mode: mode, origin: "http://" + listener.Addr().String(), directory: directory, evidence: evidence, inputHash: inputHash, dist: dist, listener: listener, serve: make(chan error, 1), tails: map[string]*reviewWebTail{}}
	w := &milestoneCreateWeb{sprintStartWeb: &sprintStartWeb{agentConfigurationWeb: base, project: project.Clone()}}
	published = true
	return &taskDependencyWeb{milestoneCreateWeb: w}
}
func (w *taskDependencyWeb) start(t *testing.T, address string) {
	t.Helper()
	w.upstream = &http.Transport{Proxy: nil, DisableKeepAlives: true}
	w.backend = httptest.NewUnstartedServer(http.HandlerFunc(func(out http.ResponseWriter, r *http.Request) { w.bridge(address, out, r) }))
	w.backend.Config.ErrorLog = log.New(io.Discard, "", 0)
	w.backend.StartTLS()
	w.transport = w.backend.Client().Transport.(*http.Transport).Clone()
	if w.transport.TLSClientConfig.InsecureSkipVerify {
		t.Fatal("owned Task Dependency TLS verification disabled")
	}
	target, err := url.Parse(w.backend.URL)
	agentAppRequire(t, err)
	proxy := httputil.NewSingleHostReverseProxy(target)
	proxy.Transport, proxy.ErrorLog = w.transport, log.New(io.Discard, "", 0)
	proxy.ModifyResponse, proxy.ErrorHandler = w.response, w.proxyError
	static := http.FileServer(http.Dir(w.dist))
	w.front = &http.Server{ReadHeaderTimeout: 2 * time.Second, ErrorLog: log.New(io.Discard, "", 0), Handler: http.HandlerFunc(func(out http.ResponseWriter, r *http.Request) {
		w.frontWG.Add(1)
		defer w.frontWG.Done()
		if strings.HasPrefix(r.URL.Path, "/api/") {
			if err := w.observe(r); err != nil {
				w.fail(err)
				http.Error(out, "owned Task Dependency observation failed", 500)
				return
			}
			proxy.ServeHTTP(out, r)
			return
		}
		if w.dist == "" || r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.NotFound(out, r)
			return
		}
		clean := filepath.Clean("/" + r.URL.Path)
		if info, err := os.Stat(filepath.Join(w.dist, clean)); err == nil && info.Mode().IsRegular() {
			static.ServeHTTP(out, r)
			return
		}
		http.ServeFile(out, r, filepath.Join(w.dist, "index.html"))
	})}
	go func() { w.serve <- w.front.Serve(w.listener) }()
}

func (w *taskDependencyWeb) browser(t *testing.T, parent context.Context) {
	t.Helper()
	root, err := filepath.Abs("../account-captcha-web")
	agentAppRequire(t, err)
	ctx, cancel := context.WithTimeout(parent, 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "node", filepath.Join(root, "node_modules/@playwright/test/cli.js"), "test", "--config", filepath.Join(root, "task-dependency.config.js"))
	cmd.Dir = root
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.WaitDelay = time.Second
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		switch key {
		case "TMPDIR", "TMP", "TEMP", "NODE_COMPILE_CACHE", "PWTEST_CACHE_DIR", "XDG_CACHE_HOME":
			continue
		}
		if key != "DEV_CHAT_BASE_URL" && key != "DEV_CHAT_API_KEY" && key != "DEV_CHAT_MODEL" && key != "DEBUG" && key != "PWDEBUG" && key != "PLAYWRIGHT_NO_COPY_PROMPT" && !strings.HasPrefix(key, "AGENTEAM_") {
			cmd.Env = append(cmd.Env, entry)
		}
	}
	// Keep every child temporary/cache path inside the original Dispose owner.
	for _, variable := range []struct{ key, directory string }{
		{"TMPDIR", "tmp"}, {"TMP", "tmp"}, {"TEMP", "tmp"},
		{"NODE_COMPILE_CACHE", "node-compile-cache"},
		{"PWTEST_CACHE_DIR", "playwright-cache"},
		{"XDG_CACHE_HOME", "xdg-cache"},
	} {
		directory := filepath.Join(w.directory, variable.directory)
		agentAppRequire(t, os.MkdirAll(directory, 0700))
		cmd.Env = append(cmd.Env, variable.key+"="+directory)
	}
	const prefix = "AGENTEAM_TASK_DEPENDENCY_WEB"
	cmd.Env = append(cmd.Env, "PLAYWRIGHT_NO_COPY_PROMPT=1", "AGENTEAM_AUTH_WEB_ORIGIN="+w.origin, "AGENTEAM_AUTH_WEB_PRIVATE="+w.directory, "AGENTEAM_AUTH_WEB_CHROMIUM=/usr/bin/chromium", prefix+"_DIST="+w.dist, prefix+"_CASE="+w.mode, prefix+"_EVIDENCE="+w.evidence, prefix+"_INPUT_HASH="+w.inputHash)
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	if err = cmd.Start(); err != nil {
		t.Fatal("owned Task Dependency Playwright did not start")
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
	err = <-done
	joined = true
	t.Logf("Task Dependency Node actual_wait pid=%d success=%t", cmd.Process.Pid, err == nil)
	safe := output.String()
	w.mu.Lock()
	tokens := append([]string(nil), w.keys...)
	w.mu.Unlock()
	tokens = append(tokens, w.a.p2.base.ownerBrowser.cookie, w.a.p2.base.ownerBrowser.csrf)
	for _, token := range tokens {
		if token != "" {
			safe = strings.ReplaceAll(safe, token, "[redacted]")
		}
	}
	t.Log(safe)
	if err != nil {
		t.Fatal("original Task Dependency browser call failed")
	}
}

func withTaskDependencyApp(t *testing.T, a *agentCreateFixture, oldProcess, origin string, exercise func(context.Context, string, *agentAppLog)) {
	t.Helper()
	directory, err := os.MkdirTemp("", "dependency-app-")
	agentAppRequire(t, err)
	defer func() { agentAppRequire(t, os.RemoveAll(directory)) }()
	cfg := agentConfigurationAppConfig(t, a, directory, origin)
	if _, enabled := cfg.ExecutionRuntime(); enabled {
		t.Fatal("Agent configuration test enabled Runtime")
	}
	logs := &agentAppLog{listening: make(chan string, 1)}
	logger, err := logging.New(logging.Central, slog.LevelInfo, logs)
	agentAppRequire(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	signals := make(chan os.Signal, 1)
	done := make(chan struct{})
	var runErr error
	go func() { runErr = app.Run(ctx, cfg, logger, signals); close(done) }()
	joined := false
	defer func() {
		if !joined {
			cancel()
			select {
			case <-done:
			case <-time.After(15 * time.Second):
				t.Error("original App call did not return; retaining dependencies")
				<-done
			}
		}
	}()
	var address string
	select {
	case address = <-logs.listening:
	case <-done:
		t.Fatal("real App initialization failed", runErr)
	case <-ctx.Done():
		t.Fatal("real App did not publish its listener")
	}
	exercise(ctx, address, logs)
	signals <- syscall.SIGTERM
	select {
	case <-done:
		joined = true
	case <-ctx.Done():
		t.Fatal("App did not join original shutdown")
	}
	agentAppRequire(t, runErr)
	if !logs.drained() {
		t.Fatal("App had no original graceful shutdown record")
	}
	var active, executionCount, newStopped int64
	agentAppRequire(t, a.p2.base.raw.QueryRow(ctx, `SELECT
 (SELECT count(*) FROM agenteam_object.process_claims WHERE state<>'stopped'),
 (SELECT count(*) FROM agenteam_execution.executions),
 (SELECT count(*) FROM agenteam_object.process_claims WHERE process_id<>$1 AND state='stopped' AND stopped_at IS NOT NULL)`, oldProcess).Scan(&active, &executionCount, &newStopped))
	if active != 0 || executionCount != 0 || newStopped != 1 {
		t.Fatal("runtime-off App omitted its own guard retirement or launched Execution")
	}
	t.Log("Task Dependency App original public Run joined; runtime disabled; no Execution")
}

const dependencyDescription = "等待关联任务完成。\n保留 原始依赖说明。"
const dependencyCycleDescription = "尝试反向依赖。\n拒绝后保留草稿。"
const dependencyResolution = "原 Owner 明确解除依赖。\n保留历史。"

type taskDependencyIntent struct {
	humanBlockerIntent
	target         wc.TaskID
	rejectedBefore string
}
type taskDependencyIntentKey struct{}
type taskDependencyWeb struct {
	*milestoneCreateWeb
	logs                                                                                *agentAppLog
	initial, current, related, foreign                                                  wc.Task
	parents                                                                             []wc.Milestone
	sprints                                                                             []wc.Sprint
	rows                                                                                []wc.Task
	blocker                                                                             *wc.TaskBlocker
	writes                                                                              []taskDependencyIntent
	receipts                                                                            []wc.TaskBlockerMutation
	queryGeneration                                                                     int64
	fixed                                                                               []byte
	cursors                                                                             map[string]bool
	taskReads, blockerReads, timelineReads, candidatePages, candidateSecond, rejections int
}

func (w *taskDependencyWeb) path(task wc.TaskID) string {
	return variableHTTPPath(w.project.Project.ID, "/tasks/"+task.String())
}
func (w *taskDependencyWeb) prepare(t *testing.T, ctx context.Context) {
	w.prepareEmpty(t, ctx)
	other := w.a.p2.base.project.ID
	if other == w.project.Project.ID {
		t.Fatal("independent Q Project absent")
	}
	for group, project := range []pc.ProjectID{w.project.Project.ID, other} {
		parentInput := wc.CreateMilestoneRequest{MilestoneID: id[wc.Milestone](t), Title: fmt.Sprintf("Dependency milestone %d", group+1), Description: "Original planning parent"}
		reply := w.request(t, http.MethodPost, variableHTTPPath(project, "/milestones"), jsonBytes(t, map[string]any{"request": parentInput}), f.IdempotencyKey(id[struct{}](t).String()))
		var parent wc.StructureMutation
		if reply.status != 200 || json.Unmarshal(reply.body, &parent) != nil || parent.Validate() != nil || parent.Milestone == nil || parent.Milestone.ID != parentInput.MilestoneID {
			t.Fatalf("dependency seed stage=milestone group=%d status=%d", group, reply.status)
		}
		w.parents = append(w.parents, *parent.Milestone)
		count := 2
		if group == 1 {
			count = 1
		}
		for n := 0; n < count; n++ {
			in := wc.CreateSprintRequest{SprintID: id[pc.Sprint](t), MilestoneID: parentInput.MilestoneID, Title: fmt.Sprintf("Dependency Sprint %d-%d", group+1, n+1), Description: "Original cross-Sprint placement"}
			reply = w.request(t, http.MethodPost, variableHTTPPath(project, "/sprints"), jsonBytes(t, map[string]any{"request": in}), f.IdempotencyKey(id[struct{}](t).String()))
			var out wc.StructureMutation
			if reply.status != 200 || json.Unmarshal(reply.body, &out) != nil || out.Validate() != nil || out.Sprint == nil || out.Sprint.ID != in.SprintID || out.Sprint.State != wc.Planned {
				t.Fatalf("dependency seed stage=sprint group=%d item=%d status=%d", group, n, reply.status)
			}
			w.sprints = append(w.sprints, out.Sprint.Clone())
		}
	}
	// Actual task ordering starts with Sprint ID, independently of Sprint rank.
	if w.sprints[0].ID.String() > w.sprints[1].ID.String() {
		w.sprints[0], w.sprints[1] = w.sprints[1], w.sprints[0]
	}
	for group, sprint := range w.sprints {
		count := 1
		if group == 0 {
			count = 52
		}
		for n := 0; n < count; n++ {
			title := fmt.Sprintf("Dependency candidate %02d", n+1)
			if group == 0 && n == 0 {
				title = "Source task A"
			}
			if group == 1 {
				title = "Related task B"
			}
			if group == 2 {
				title = "Foreign project task Q"
			}
			in := wc.TaskCreate{TaskID: id[wc.Task](t), SprintID: sprint.ID, Title: title, Description: "Original task content", Type: wc.TaskTypeTask, Priority: wc.TaskPriorityMedium, Plan: "Preserve original plan"}
			reply := w.request(t, http.MethodPost, variableHTTPPath(sprint.ProjectID, "/tasks"), jsonBytes(t, map[string]any{"request": in}), f.IdempotencyKey(id[struct{}](t).String()))
			var out wc.TaskMutation
			if reply.status != 200 || json.Unmarshal(reply.body, &out) != nil || out.Validate() != nil || !out.Changed || out.Task.ID != in.TaskID || out.Task.State != wc.TaskStateBacklog || out.Task.AssigneeAgentID != nil || out.Task.Version != 1 {
				t.Fatalf("dependency seed stage=task group=%d item=%d status=%d", group, n, reply.status)
			}
			if group < 2 {
				w.rows = append(w.rows, out.Task.Clone())
			}
			if group == 0 && n == 0 {
				w.initial, w.current = out.Task.Clone(), out.Task.Clone()
			}
			if group == 1 {
				w.related = out.Task.Clone()
			}
			if group == 2 {
				w.foreign = out.Task.Clone()
			}
		}
	}
	sort.Slice(w.rows, func(a, b int) bool {
		x, y := w.rows[a], w.rows[b]
		if x.SprintID != y.SprintID {
			return x.SprintID.String() < y.SprintID.String()
		}
		if x.State != y.State {
			return x.State.Order() < y.State.Order()
		}
		if x.Priority != y.Priority {
			return x.Priority.Order() < y.Priority.Order()
		}
		if x.ManualRank != y.ManualRank {
			return x.ManualRank < y.ManualRank
		}
		return x.ID.String() < y.ID.String()
	})
	if len(w.rows) != 53 || w.rows[52].ID != w.related.ID || w.initial.SprintID == w.related.SprintID {
		t.Fatal("cross-Sprint second-page target absent")
	}
	target := w.sprints[0]
	reply := w.request(t, http.MethodPost, variableHTTPPath(w.project.Project.ID, "/sprints/"+target.ID.String()+"/start"), jsonBytes(t, map[string]any{"expected_version": target.Version, "request": map[string]any{}}), f.IdempotencyKey(id[struct{}](t).String()))
	var started wc.SprintStartMutation
	if reply.status != 200 || json.Unmarshal(reply.body, &started) != nil || started.Validate() != nil || started.Sprint.ID != target.ID {
		t.Fatalf("dependency seed stage=start status=%d", reply.status)
	}
	w.sprints[0] = started.Sprint.Clone()
	w.project.Project = started.Project.Clone()
	w.cursors = map[string]bool{}
	agentAppRequire(t, w.a.p2.base.raw.QueryRow(ctx, `SELECT query_generation FROM agenteam_work.task_query_generations WHERE project_id=$1::text::uuid`, w.initial.ProjectID.String()).Scan(&w.queryGeneration))
	var err error
	w.fixed, err = w.fixedFacts(ctx)
	agentAppRequire(t, err)
}
func (w *taskDependencyWeb) material(t *testing.T, username string) {
	raw := jsonBytes(t, map[string]any{"mode": w.mode, "origin": w.origin, "cookie": w.a.p2.base.ownerBrowser.cookie, "project_id": w.initial.ProjectID, "other_project_id": w.foreign.ProjectID, "initial": w.initial, "related": w.related, "foreign": w.foreign, "rows": w.rows, "sprints": w.sprints[:2], "milestone": w.parents[0], "route": "/" + username + "/" + w.project.Project.NormalizedName + "/tasks/" + w.initial.ID.String(), "description": dependencyDescription, "cycle_description": dependencyCycleDescription, "resolution": dependencyResolution, "input_hash": w.inputHash})
	agentAppRequire(t, os.WriteFile(filepath.Join(w.directory, "task-dependency-material.json"), raw, 0600))
	clear(raw)
}
func (w *taskDependencyWeb) returned(ctx context.Context, rid, method string, status, length int) error {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	tick := time.NewTicker(5 * time.Millisecond)
	defer tick.Stop()
	for {
		w.logs.mu.Lock()
		raw := bytes.Clone(w.logs.data.Bytes())
		w.logs.mu.Unlock()
		matches := 0
		for _, line := range bytes.Split(raw, []byte{'\n'}) {
			var row struct {
				Event  string `json:"event"`
				ID     string `json:"request_id"`
				Method string `json:"method"`
				Status int    `json:"status"`
				Bytes  int    `json:"bytes"`
			}
			if json.Unmarshal(line, &row) != nil || row.Event != "http_request" || row.ID != rid {
				continue
			}
			if row.Method != method || row.Status != status || row.Bytes != length {
				return errors.New("original App handler return differs")
			}
			matches++
		}
		clear(raw)
		if matches == 1 {
			return nil
		}
		if matches > 1 {
			return errors.New("original App handler identity repeated")
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-tick.C:
		}
	}
}
func (w *taskDependencyWeb) observe(r *http.Request) error {
	if r.Method == http.MethodGet {
		return nil
	}
	if r.Method != http.MethodPost || r.URL.RawQuery != "" || r.URL.ForceQuery {
		return errors.New("undeclared dependency mutation")
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, 16385))
	closeErr := r.Body.Close()
	key := f.IdempotencyKey(r.Header.Get("Idempotency-Key"))
	if err != nil || closeErr != nil || len(raw) > 16384 || key.Validate() != nil || len(r.Header.Values("Idempotency-Key")) != 1 {
		return errors.New("dependency original request incomplete")
	}
	r.Body = io.NopCloser(bytes.NewReader(raw))
	w.mu.Lock()
	defer w.mu.Unlock()
	index := len(w.writes)
	if index >= 3 || index != len(w.receipts)+w.rejections {
		return errors.New("dependency request repeated or unconfirmed")
	}
	for _, old := range w.writes {
		if old.key == key {
			return errors.New("dependency key reused")
		}
	}
	in := taskDependencyIntent{humanBlockerIntent: humanBlockerIntent{key: key, before: w.current.Clone(), requestDigest: sha256.Sum256(raw), keyDigest: sha256.Sum256([]byte(key))}, target: w.initial.ID}
	if index < 2 {
		var wire struct {
			Expected f.Version            `json:"expected_version"`
			Request  wc.TaskBlockerCreate `json:"request"`
		}
		target, related, description := w.initial.ID, w.related.ID, dependencyDescription
		if index == 1 {
			target, related, description = w.related.ID, w.initial.ID, dependencyCycleDescription
			in.before = w.related.Clone()
		}
		if agentWebDecode(raw, &wire) != nil || wire.Request.Validate() != nil || wire.Expected != in.before.Version || r.URL.Path != w.path(target)+"/blockers" || wire.Request.Type != wc.TaskBlockerRelyOn || wire.Request.Description != description || wire.Request.Metadata.RelyOn == nil || wire.Request.Metadata.RelyOn.RelatedTaskID != related {
			return errors.New("dependency Add changed original target/version/input")
		}
		var exists bool
		if err = w.a.p2.base.raw.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM agenteam_work.task_blockers WHERE id=$1::text::uuid)`, wire.Request.BlockerID.String()).Scan(&exists); err != nil {
			return err
		}
		if exists {
			return errors.New("dependency reused existing blocker identity")
		}
		in.kind, in.expected, in.add, in.target = "add", wire.Expected, &wire.Request, target
		if index == 1 {
			in.kind = "cycle"
			in.rejectedBefore, err = w.mutableFacts(r.Context())
			if err != nil {
				return err
			}
		}
	} else {
		var wire struct {
			Expected f.Version             `json:"expected_version"`
			Request  wc.TaskBlockerResolve `json:"request"`
		}
		if agentWebDecode(raw, &wire) != nil || wire.Request.Validate() != nil || wire.Expected != w.current.Version || r.URL.Path != w.path(w.initial.ID)+"/blockers/resolve" || w.blocker == nil || wire.Request.BlockerID != w.blocker.ID || wire.Request.ResolutionComment == nil || *wire.Request.ResolutionComment != dependencyResolution {
			return errors.New("dependency Resolve changed original identity/version/comment")
		}
		before := w.blocker.Clone()
		in.kind, in.expected, in.resolve, in.blockerBefore = "resolve-blocker", wire.Expected, &wire.Request, &before
	}
	w.writes = append(w.writes, in)
	w.keys = append(w.keys, string(key))
	*r = *r.WithContext(context.WithValue(r.Context(), taskDependencyIntentKey{}, in))
	return nil
}
func (w *taskDependencyWeb) response(r *http.Response) error {
	raw, err := w.completedResponse(r)
	if err != nil {
		return err
	}
	rid := r.Header.Get("X-Request-ID")
	if err = w.returned(r.Request.Context(), rid, r.Request.Method, r.StatusCode, len(raw)); err != nil {
		return err
	}
	in, write := r.Request.Context().Value(taskDependencyIntentKey{}).(taskDependencyIntent)
	if write {
		in.requestID, err = f.ParseID[f.Request](rid)
		if err != nil {
			return err
		}
		if in.kind == "cycle" {
			var problem httpapi.Problem
			after, e := w.mutableFacts(r.Request.Context())
			if e != nil {
				return e
			}
			if r.StatusCode != 409 || json.Unmarshal(raw, &problem) != nil || problem.Code != f.TaskDependencyCycle || problem.CommitState != f.NotCommitted || problem.RequestID != in.requestID || problem.Instance != r.Request.URL.Path || after != in.rejectedBefore {
				return errors.New("real cycle rejection committed facts or lost original problem")
			}
			w.mu.Lock()
			w.writes[1] = in
			w.rejections++
			w.mu.Unlock()
		} else {
			var result wc.TaskBlockerMutation
			if r.StatusCode != 200 || json.Unmarshal(raw, &result) != nil {
				return errors.New("dependency original receipt failed")
			}
			w.mu.Lock()
			index := len(w.receipts)
			w.mu.Unlock()
			if err = w.commandFacts(r.Request.Context(), in.humanBlockerIntent, result, index, true); err != nil {
				return err
			}
			w.mu.Lock()
			w.writes[len(w.writes)-1] = in
			w.receipts = append(w.receipts, result.Clone())
			w.current = result.Task.Clone()
			b := result.Blocker.Clone()
			w.blocker = &b
			w.cursors = map[string]bool{}
			w.mu.Unlock()
		}
	} else {
		if r.StatusCode != 200 {
			return fmt.Errorf("dependency read failed status=%d", r.StatusCode)
		}
		if err = w.observeRead(r.Request, raw); err != nil {
			return err
		}
	}
	proof := map[string]any{"input_hash": w.inputHash, "request_id": rid, "method": r.Request.Method, "url_sha256": fmt.Sprintf("%x", sha256.Sum256([]byte(r.Request.URL.RequestURI()))), "status": r.StatusCode, "content_length": len(raw), "body_sha256": fmt.Sprintf("%x", sha256.Sum256(raw)), "original_eof": true, "original_closed": true, "handler_returned": true, "handler_return_source": "app_http_request_log"}
	if write {
		proof["kind"] = in.kind
		proof["request_body_sha256"] = fmt.Sprintf("%x", in.requestDigest)
		proof["idempotency_key_sha256"] = fmt.Sprintf("%x", in.keyDigest)
		proof["committed_verified"] = in.kind != "cycle"
		proof["zero_commit_verified"] = in.kind == "cycle"
	}
	encoded, err := json.Marshal(proof)
	if err != nil {
		return err
	}
	if err = os.MkdirAll(w.evidence, 0700); err != nil {
		return err
	}
	if err = os.WriteFile(filepath.Join(w.evidence, "task-dependency-"+w.mode+"-response-"+rid+".json"), encoded, 0600); err != nil {
		return err
	}
	r.Body = io.NopCloser(bytes.NewReader(raw))
	return nil
}
func (w *taskDependencyWeb) commandFacts(ctx context.Context, in humanBlockerIntent, result wc.TaskBlockerMutation, index int, canonical bool) error {
	if result.Validate() != nil || in.expected != in.before.Version {
		return errors.New("invalid original blocker receipt/version")
	}
	expected := in.before.Clone()
	expected.Version++
	expected.UpdatedAt = result.Task.UpdatedAt
	if expected.UpdatedAt.Time().Before(in.before.UpdatedAt.Time()) || !reflect.DeepEqual(expected, result.Task) {
		return errors.New("blocker write changed Task content/state/assignee/rank")
	}
	user, err := f.ParseID[i.User](w.a.p2.base.ownerBrowser.actor.Details().UserID)
	if err != nil {
		return err
	}
	actor := wc.TaskEventActor{Type: i.Human, UserID: user, Source: "task_domain"}
	var expectedBlocker wc.TaskBlocker
	command := wc.TaskBlockerCommandAdd
	if in.add != nil {
		expectedBlocker = wc.TaskBlocker{ID: in.add.BlockerID, ProjectID: w.initial.ProjectID, TaskID: w.initial.ID, Type: wc.TaskBlockerRelyOn, Description: in.add.Description, Metadata: in.add.Metadata.Clone(), CreatedAt: expected.UpdatedAt, CreatedBy: actor}
	} else {
		if in.blockerBefore == nil || in.resolve == nil {
			return errors.New("Resolve missing original blocker")
		}
		expectedBlocker = in.blockerBefore.Clone()
		at := expected.UpdatedAt
		expectedBlocker.ResolvedAt = &at
		expectedBlocker.ResolvedBy = &actor
		expectedBlocker.ResolutionComment = in.resolve.Clone().ResolutionComment
		command = wc.TaskBlockerCommandResolve
	}
	if !reflect.DeepEqual(expectedBlocker, result.Blocker) {
		return errors.New("blocker receipt lost original identity/content/actors/resolution")
	}
	meta := f.CommandMeta{RequestID: in.requestID, IdempotencyKey: in.key, ExpectedVersion: &in.expected}
	var digest f.Digest
	if in.add != nil {
		digest, err = wc.TaskBlockerAddDigest(w.a.p2.base.ownerBrowser.actor, meta, w.initial.ProjectID, w.initial.ID, *in.add)
	} else {
		digest, err = wc.TaskBlockerResolveDigest(w.a.p2.base.ownerBrowser.actor, meta, w.initial.ProjectID, w.initial.ID, *in.resolve)
	}
	if err != nil {
		return err
	}
	var commandID, historyID, eventID, userID, semantic string
	var requestRaw, receiptRaw, planRaw []byte
	var revision int64
	err = w.a.p2.base.raw.QueryRow(ctx, `SELECT id::text,task_event_id::text,event_id::text,actor_user_id::text,semantic_digest,request,receipt,plan,plan_revision FROM agenteam_work.task_blocker_commands WHERE project_id=$1::text::uuid AND command_name=$2 AND idempotency_key=$3 AND state='completed' AND committed_at IS NOT NULL`, w.initial.ProjectID.String(), string(command), string(in.key)).Scan(&commandID, &historyID, &eventID, &userID, &semantic, &requestRaw, &receiptRaw, &planRaw, &revision)
	if err != nil {
		return err
	}
	var request, wantRequest map[string]any
	var receipt wc.TaskBlockerMutation
	want := map[string]any{"command": command, "project_id": w.initial.ProjectID, "task_id": w.initial.ID, "actor_user_id": user, "expected_version": in.expected, "add": in.add, "resolve": in.resolve}
	wantRaw, err := json.Marshal(want)
	if err != nil {
		return err
	}
	if revision < 1 || userID != user.String() || semantic != digest.String() || historyID != result.TaskEventID.String() || eventID != result.EventIDs[0].String() || json.Unmarshal(requestRaw, &request) != nil || json.Unmarshal(wantRaw, &wantRequest) != nil || !reflect.DeepEqual(request, wantRequest) || json.Unmarshal(receiptRaw, &receipt) != nil || !reflect.DeepEqual(receipt, result) {
		return errors.New("original blocker command identity/request/receipt differs")
	}
	var plan struct {
		Before        wc.Task                `json:"before"`
		BlockerBefore *wc.TaskBlocker        `json:"blocker_before"`
		After         wc.TaskBlockerMutation `json:"after"`
		Placement     struct {
			Milestone wc.MilestoneID `json:"milestone_id"`
			Sprint    wc.SprintID    `json:"sprint_id"`
			State     wc.SprintState `json:"state"`
		} `json:"placement"`
		Generation int64                  `json:"query_generation"`
		Event      wc.TaskBlockerEvent    `json:"task_event"`
		Header     eventc.Header          `json:"header"`
		Payload    wc.TaskBlockersChanged `json:"payload"`
	}
	if json.Unmarshal(planRaw, &plan) != nil || !reflect.DeepEqual(plan.Before, in.before) || !reflect.DeepEqual(plan.BlockerBefore, in.blockerBefore) || !reflect.DeepEqual(plan.After, result) || plan.Generation != w.queryGeneration+int64(index) || plan.Placement.Milestone != w.initial.MilestoneID || plan.Placement.Sprint != w.initial.SprintID || plan.Placement.State != wc.Current {
		return errors.New("blocker plan lost original before/after/placement/generation")
	}
	op, err := f.ParseID[wc.TaskBlockerCommand](commandID)
	if err != nil {
		return err
	}
	eventType, change := wc.TaskBlockerEventAdded, wc.TaskBlockerAddedChange
	payload := wc.TaskBlockerEventPayload{Added: &wc.TaskBlockerAddedPayload{BlockerID: result.Blocker.ID, BlockerType: wc.TaskBlockerRelyOn}}
	if in.resolve != nil {
		eventType, change = wc.TaskBlockerEventResolved, wc.TaskBlockerResolvedChange
		payload = wc.TaskBlockerEventPayload{Resolved: &wc.TaskBlockerResolvedPayload{BlockerID: result.Blocker.ID, BlockerType: wc.TaskBlockerRelyOn, ResolutionComment: in.resolve.ResolutionComment}}
	}
	expectedHistory := wc.TaskBlockerEvent{ID: result.TaskEventID, ProjectID: w.initial.ProjectID, TaskID: w.initial.ID, TaskVersion: expected.Version, Type: eventType, Actor: actor, OperationID: op, CorrelationID: op, Payload: payload, CreatedAt: expected.UpdatedAt}
	expectedPayload := wc.TaskBlockersChanged{OperationID: op, ActorUserID: user, TaskEventID: result.TaskEventID, BlockerID: result.Blocker.ID, Change: change}
	h := plan.Header
	if plan.Event.Validate() != nil || !reflect.DeepEqual(plan.Event, expectedHistory) || !reflect.DeepEqual(plan.Payload, expectedPayload) || h.Validate() != nil || h.EventID != result.EventIDs[0] || h.EventType != wc.TaskBlockersChangedName || h.AggregateType != wc.TaskAggregate || h.AggregateID.String() != w.initial.ID.String() || h.AggregateVersion == nil || *h.AggregateVersion != expected.Version || h.AggregateSequence != nil || h.Scope.Kind != eventc.ProjectScope || h.Scope.ProjectID.String() != w.initial.ProjectID.String() || h.SchemaVersion != wc.TaskBlockerSchemaVersion || h.OccurredAt != expected.UpdatedAt {
		return errors.New("blocker plan history/Outbox differs from independently expected facts")
	}
	var historyRaw, headerRaw, payloadRaw []byte
	var historyColumns, eventColumns bool
	err = w.a.p2.base.raw.QueryRow(ctx, `SELECT operation_id IS NULL AND transition_operation_id IS NULL AND claim_operation_id IS NULL AND compensation_operation_id IS NULL AND failure_operation_id IS NULL,
 jsonb_build_object('id',id,'project_id',project_id,'task_id',task_id,'task_version',task_version::text,'type',type,'actor',actor,'operation_id',blocker_operation_id,'correlation_id',correlation_id,'payload',payload,'created_at',to_char(created_at AT TIME ZONE 'UTC','YYYY-MM-DD"T"HH24:MI:SS.US"Z"')) FROM agenteam_work.task_events WHERE id=$1::text::uuid AND project_id=$2::text::uuid AND task_id=$3::text::uuid AND blocker_operation_id=$4::text::uuid`, historyID, w.initial.ProjectID.String(), w.initial.ID.String(), commandID).Scan(&historyColumns, &historyRaw)
	if err != nil {
		return err
	}
	var storedHistory wc.TaskBlockerEvent
	if !historyColumns || json.Unmarshal(historyRaw, &storedHistory) != nil || !reflect.DeepEqual(storedHistory, expectedHistory) {
		return errors.New("canonical Task history differs from original blocker plan")
	}
	err = w.a.p2.base.raw.QueryRow(ctx, `SELECT header,payload,event_type='work.task_blockers_changed' AND schema_version=1 AND scope='project' AND aggregate_type='work.task' AND aggregate_version=$4 AND aggregate_sequence IS NULL AND occurred_at=$5 FROM agenteam_outbox.events WHERE id=$1::text::uuid AND project_id=$2::text::uuid AND aggregate_id=$3::text::uuid AND producer='work'`, eventID, w.initial.ProjectID.String(), w.initial.ID.String(), int64(expected.Version), expected.UpdatedAt.Time()).Scan(&headerRaw, &payloadRaw, &eventColumns)
	if err != nil {
		return err
	}
	actualHeader, err := eventc.DecodeHeader(headerRaw)
	var actualPayload wc.TaskBlockersChanged
	if err != nil || !eventColumns || json.Unmarshal(payloadRaw, &actualPayload) != nil || !reflect.DeepEqual(actualHeader, h) || !reflect.DeepEqual(actualPayload, expectedPayload) {
		return errors.New("canonical blocker Outbox differs from original plan")
	}
	if canonical {
		current, err := w.currentTask(ctx, w.initial.ID)
		if err != nil {
			return err
		}
		if !reflect.DeepEqual(current, expected) {
			return errors.New("canonical Task differs from blocker receipt")
		}
		var blockerRaw []byte
		var operations bool
		var generation int64
		err = w.a.p2.base.raw.QueryRow(ctx, `SELECT jsonb_build_object('id',id,'project_id',project_id,'task_id',task_id,'type',type,'description',description,'metadata',metadata,'created_at',to_char(created_at AT TIME ZONE 'UTC','YYYY-MM-DD"T"HH24:MI:SS.US"Z"'),'created_by',created_by,'resolved_at',CASE WHEN resolved_at IS NULL THEN NULL ELSE to_char(resolved_at AT TIME ZONE 'UTC','YYYY-MM-DD"T"HH24:MI:SS.US"Z"') END,'resolved_by',resolved_by,'resolution_comment',resolution_comment),failure_operation_id IS NULL AND resolved_transition_operation_id IS NULL AND created_operation_id=(SELECT id FROM agenteam_work.task_blocker_commands WHERE project_id=$1::text::uuid AND command_name='work.task.blocker.add' AND receipt->'blocker'->>'id'=$3::text) AND CASE WHEN $4 THEN resolved_operation_id=$5::text::uuid ELSE resolved_operation_id IS NULL END FROM agenteam_work.task_blockers WHERE project_id=$1::text::uuid AND task_id=$2::text::uuid AND id=$3::text::uuid`, w.initial.ProjectID.String(), w.initial.ID.String(), result.Blocker.ID.String(), in.resolve != nil, commandID).Scan(&blockerRaw, &operations)
		if err != nil {
			return err
		}
		var actual wc.TaskBlocker
		if !operations || json.Unmarshal(blockerRaw, &actual) != nil || !reflect.DeepEqual(actual, expectedBlocker) {
			return errors.New("canonical blocker differs from full original receipt/operation ownership")
		}
		err = w.a.p2.base.raw.QueryRow(ctx, `SELECT query_generation FROM agenteam_work.task_query_generations WHERE project_id=$1::text::uuid`, w.initial.ProjectID.String()).Scan(&generation)
		if err != nil {
			return err
		}
		if generation != w.queryGeneration+int64(index)+1 {
			return errors.New("blocker query generation did not advance exactly once")
		}
	}
	return nil
}
func (w *taskDependencyWeb) fixedFacts(ctx context.Context) ([]byte, error) {
	var raw []byte
	err := w.a.p2.base.raw.QueryRow(ctx, `SELECT jsonb_build_object(
 'projects',(SELECT jsonb_agg(to_jsonb(p) ORDER BY id) FROM agenteam_project.projects p),
 'agents',(SELECT jsonb_agg(to_jsonb(a) ORDER BY id) FROM agenteam_agent.agents a),
 'agent_commands',(SELECT jsonb_agg(to_jsonb(c) ORDER BY project_id,id) FROM agenteam_agent.commands c),
 'milestones',(SELECT jsonb_agg(to_jsonb(m) ORDER BY id) FROM agenteam_work.milestones m),
 'sprints',(SELECT jsonb_agg(to_jsonb(s) ORDER BY id) FROM agenteam_work.sprints s),
 'structure_commands',(SELECT jsonb_agg(to_jsonb(c) ORDER BY project_id,id) FROM agenteam_work.structure_commands c),
 'starts',(SELECT jsonb_agg(to_jsonb(c) ORDER BY project_id,id) FROM agenteam_work.sprint_start_commands c),
 'milestone_order',(SELECT jsonb_agg(to_jsonb(g) ORDER BY project_id) FROM agenteam_work.milestone_order_groups g),
 'sprint_order',(SELECT jsonb_agg(to_jsonb(g) ORDER BY project_id,milestone_id) FROM agenteam_work.sprint_order_groups g),
 'target_fixed',(SELECT to_jsonb(t)-'version'-'updated_at' FROM agenteam_work.tasks t WHERE project_id=$1::text::uuid AND id=$2::text::uuid),
 'other_tasks',(SELECT jsonb_agg(to_jsonb(t) ORDER BY id) FROM agenteam_work.tasks t WHERE id<>$2::text::uuid),
 'commands',(SELECT jsonb_agg(to_jsonb(c) ORDER BY project_id,id) FROM agenteam_work.task_commands c),
 'transitions',(SELECT jsonb_agg(to_jsonb(c) ORDER BY project_id,id) FROM agenteam_work.task_transition_commands c),
 'other_blocker_commands',(SELECT jsonb_agg(to_jsonb(c) ORDER BY project_id,id) FROM agenteam_work.task_blocker_commands c WHERE project_id<>$1::text::uuid OR request->>'task_id'<>$2::text),
 'other_blockers',(SELECT jsonb_agg(to_jsonb(b) ORDER BY id) FROM agenteam_work.task_blockers b WHERE task_id<>$2::text::uuid),
 'old_events',(SELECT jsonb_agg(to_jsonb(e) ORDER BY project_id,id) FROM agenteam_work.task_events e WHERE task_id<>$2::text::uuid OR blocker_operation_id IS NULL),
 'other_generations',(SELECT jsonb_agg(to_jsonb(g) ORDER BY project_id) FROM agenteam_work.task_query_generations g WHERE project_id<>$1::text::uuid),
 'orders',(SELECT jsonb_agg(to_jsonb(g) ORDER BY project_id,sprint_id,state,priority) FROM agenteam_work.task_order_groups g),
 'old_outbox',(SELECT jsonb_agg(to_jsonb(e) ORDER BY id) FROM agenteam_outbox.events e WHERE aggregate_id<>$2::text::uuid OR event_type<>'work.task_blockers_changed'),
 'audit',(SELECT jsonb_agg(to_jsonb(a) ORDER BY id) FROM agenteam_audit.audit_records a),
 'dispatches',(SELECT jsonb_agg(to_jsonb(d) ORDER BY id) FROM agenteam_scheduler.dispatches d),
 'executions',(SELECT jsonb_agg(to_jsonb(e) ORDER BY id) FROM agenteam_execution.executions e))`, w.initial.ProjectID.String(), w.initial.ID.String()).Scan(&raw)
	return raw, err
}
func (w *taskDependencyWeb) currentTask(ctx context.Context, task wc.TaskID) (wc.Task, error) {
	var raw []byte
	var out wc.Task
	err := w.a.p2.base.raw.QueryRow(ctx, `SELECT (to_jsonb(t)-'version'-'created_at'-'updated_at')||jsonb_build_object('version',version::text,'created_at',to_char(created_at AT TIME ZONE 'UTC','YYYY-MM-DD"T"HH24:MI:SS.US"Z"'),'updated_at',to_char(updated_at AT TIME ZONE 'UTC','YYYY-MM-DD"T"HH24:MI:SS.US"Z"')) FROM agenteam_work.tasks t WHERE project_id=$1::text::uuid AND id=$2::text::uuid`, w.initial.ProjectID.String(), task.String()).Scan(&raw)
	if err != nil {
		return out, err
	}
	err = json.Unmarshal(raw, &out)
	if err == nil {
		err = out.Validate()
	}
	return out, err
}
func (w *taskDependencyWeb) mutableFacts(ctx context.Context) (string, error) {
	var value string
	err := w.a.p2.base.raw.QueryRow(ctx, `SELECT jsonb_build_object(
 'tasks',(SELECT jsonb_agg(to_jsonb(t) ORDER BY id) FROM agenteam_work.tasks t),
 'blockers',(SELECT jsonb_agg(to_jsonb(b) ORDER BY id) FROM agenteam_work.task_blockers b),
 'commands',(SELECT jsonb_agg(to_jsonb(c) ORDER BY project_id,id) FROM agenteam_work.task_blocker_commands c),
 'events',(SELECT jsonb_agg(to_jsonb(e) ORDER BY project_id,id) FROM agenteam_work.task_events e),
 'generations',(SELECT jsonb_agg(to_jsonb(g) ORDER BY project_id) FROM agenteam_work.task_query_generations g),
 'outbox',(SELECT jsonb_agg(to_jsonb(e) ORDER BY id) FROM agenteam_outbox.events e),
 'audit',(SELECT jsonb_agg(to_jsonb(a) ORDER BY id) FROM agenteam_audit.audit_records a))::text`).Scan(&value)
	return value, err
}
func (w *taskDependencyWeb) observeRead(r *http.Request, raw []byte) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	for _, task := range []wc.Task{w.current, w.related} {
		switch r.URL.Path {
		case w.path(task.ID):
			var got wc.Task
			if len(r.URL.Query()) != 0 || json.Unmarshal(raw, &got) != nil || !reflect.DeepEqual(got, task) {
				return errors.New("current Task read differs from exact facts")
			}
			w.taskReads++
			return nil
		case w.path(task.ID) + "/blockers":
			q := r.URL.Query()
			var page f.Page[wc.TaskBlocker]
			want := []wc.TaskBlocker{}
			if task.ID == w.initial.ID && w.blocker != nil {
				want = append(want, w.blocker.Clone())
			}
			if len(q) != 2 || q.Get("limit") != "50" || q.Get("status") != "all" || json.Unmarshal(raw, &page) != nil || page.NextCursor != "" || !reflect.DeepEqual(page.Items, want) {
				return errors.New("current blockers lost complete same-task history")
			}
			w.blockerReads++
			return nil
		case w.path(task.ID) + "/events":
			if err := w.timelineRead(r, raw, task.ID); err != nil {
				return err
			}
			w.timelineReads++
			return nil
		}
	}
	if r.URL.Path == variableHTTPPath(w.initial.ProjectID, "/tasks") {
		q := r.URL.Query()
		var page f.Page[map[string]any]
		if json.Unmarshal(raw, &page) != nil || page.Items == nil || q.Get("limit") != "50" {
			return errors.New("Task page invalid")
		}
		candidate := q.Get("sprint_id") == "" && q.Get("state") == ""
		rows := []wc.Task{}
		for _, task := range w.rows {
			if task.ID == w.initial.ID {
				task = w.current.Clone()
			}
			if !candidate && (task.SprintID.String() != q.Get("sprint_id") || string(task.State) != q.Get("state")) {
				continue
			}
			if q.Get("text") != "" && !strings.Contains(strings.ToLower(task.Title), strings.ToLower(q.Get("text"))) {
				continue
			}
			rows = append(rows, task)
		}
		offset := 0
		if cursor := q.Get("cursor"); cursor != "" {
			if !candidate || !w.cursors[cursor] {
				return errors.New("candidate changed original cursor")
			}
			offset = 50
			w.candidateSecond++
		}
		if candidate {
			for key, values := range q {
				if len(values) != 1 || (key != "limit" && key != "cursor" && key != "text") {
					return errors.New("candidate restricted project-wide query")
				}
			}
			if q.Get("text") != "" && q.Get("text") != w.initial.Title {
				return errors.New("candidate changed explicit search")
			}
			w.candidatePages++
		} else if len(q) != 3 {
			return errors.New("board query changed original scope")
		}
		if offset > len(rows) {
			return errors.New("candidate offset outside original ordered rows")
		}
		end := offset + 50
		if end > len(rows) {
			end = len(rows)
		}
		want := make([]map[string]any, 0, end-offset)
		for _, task := range rows[offset:end] {
			item, e := sprintCreateSummary(task, "description", "plan")
			if e != nil {
				return e
			}
			want = append(want, item)
		}
		if !reflect.DeepEqual(page.Items, want) || (page.NextCursor != "") != (end < len(rows)) {
			return errors.New("Task page differs from complete independent server order")
		}
		if candidate && page.NextCursor != "" {
			w.cursors[page.NextCursor] = true
		}
	}
	return nil
}
func (w *taskDependencyWeb) timelineRead(r *http.Request, raw []byte, target wc.TaskID) error {
	q := r.URL.Query()
	var page f.Page[wc.TaskTimelineEvent]
	count := 1
	if target == w.initial.ID {
		count += len(w.receipts)
	}
	if len(q) != 2 || q.Get("limit") != "50" || q.Get("order") != "desc" || json.Unmarshal(raw, &page) != nil || page.Items == nil || page.NextCursor != "" || len(page.Items) != count {
		return errors.New("current timeline lost exact complete history")
	}
	rows, err := w.a.p2.base.raw.Query(r.Context(), `SELECT operation_id::text,blocker_operation_id::text,transition_operation_id IS NULL AND claim_operation_id IS NULL AND compensation_operation_id IS NULL AND failure_operation_id IS NULL,
 jsonb_build_object('id',id,'project_id',project_id,'task_id',task_id,'task_version',task_version::text,'type',type,'actor',actor,'operation_id',coalesce(operation_id,blocker_operation_id),'correlation_id',correlation_id,'payload',payload,'created_at',to_char(created_at AT TIME ZONE 'UTC','YYYY-MM-DD"T"HH24:MI:SS.US"Z"')) FROM agenteam_work.task_events WHERE project_id=$1::text::uuid AND task_id=$2::text::uuid ORDER BY created_at DESC,id DESC`, w.initial.ProjectID.String(), target.String())
	if err != nil {
		return err
	}
	defer rows.Close()
	expected := []wc.TaskTimelineEvent{}
	for rows.Next() {
		var planning, blocker *string
		var source bool
		var raw []byte
		if err = rows.Scan(&planning, &blocker, &source, &raw); err != nil {
			return err
		}
		var event wc.TaskTimelineEvent
		if !source {
			return errors.New("unexpected timeline operation family")
		}
		if planning != nil && blocker == nil {
			var value wc.TaskEvent
			if err = json.Unmarshal(raw, &value); err != nil {
				return err
			}
			event.Planning = &value
		} else if blocker != nil && planning == nil {
			var value wc.TaskBlockerEvent
			if err = json.Unmarshal(raw, &value); err != nil {
				return err
			}
			event.Blocker = &value
		} else {
			return errors.New("ambiguous timeline operation family")
		}
		if event.Validate() != nil {
			return errors.New("invalid stored timeline record")
		}
		expected = append(expected, event)
	}
	if err = rows.Err(); err != nil {
		return err
	}
	a, err := json.Marshal(page.Items)
	if err != nil {
		return err
	}
	b, err := json.Marshal(expected)
	if err != nil {
		return err
	}
	if !bytes.Equal(a, b) {
		return errors.New("HTTP timeline differs from independent full persisted facts")
	}
	return nil
}
func (w *taskDependencyWeb) verify(t *testing.T, ctx context.Context) {
	w.mu.Lock()
	failure := w.failure
	writes := append([]taskDependencyIntent(nil), w.writes...)
	receipts := append([]wc.TaskBlockerMutation(nil), w.receipts...)
	gets, blockers, timelines, pages, second, rejected := w.taskReads, w.blockerReads, w.timelineReads, w.candidatePages, w.candidateSecond, w.rejections
	tails := make([]*reviewWebTail, 0, len(w.tails))
	for _, tail := range w.tails {
		tails = append(tails, tail)
	}
	w.mu.Unlock()
	if failure != nil || w.cut != 0 || len(writes) != 3 || len(receipts) != 2 || rejected != 1 || gets < 4 || blockers < 4 || timelines < 4 || pages < 3 || second < 1 {
		t.Fatal("dependency original browser chain incomplete", failure, gets, blockers, timelines, pages, second, rejected)
	}
	for _, tail := range tails {
		select {
		case <-tail.done:
		default:
			t.Fatal("dependency original HTTP handler remains")
		}
		if !tail.returned {
			t.Fatal("dependency original handler did not return")
		}
	}
	agentAppRequire(t, w.commandFacts(ctx, writes[0].humanBlockerIntent, receipts[0], 0, false))
	agentAppRequire(t, w.commandFacts(ctx, writes[2].humanBlockerIntent, receipts[1], 1, true))
	var facts [6]int64
	agentAppRequire(t, w.a.p2.base.raw.QueryRow(ctx, `SELECT
 (SELECT count(*) FROM agenteam_work.task_blocker_commands WHERE project_id=$1::text::uuid),
 (SELECT count(*) FROM agenteam_work.task_blockers WHERE project_id=$1::text::uuid),
 (SELECT count(*) FROM agenteam_work.task_blockers WHERE project_id=$1::text::uuid AND resolved_at IS NOT NULL),
 (SELECT count(*) FROM agenteam_work.task_events WHERE project_id=$1::text::uuid AND blocker_operation_id IS NOT NULL),
 (SELECT count(*) FROM agenteam_outbox.events WHERE project_id=$1::text::uuid AND event_type='work.task_blockers_changed'),
 (SELECT count(*) FROM agenteam_work.task_transition_commands WHERE project_id=$1::text::uuid)`, w.initial.ProjectID.String()).Scan(&facts[0], &facts[1], &facts[2], &facts[3], &facts[4], &facts[5]))
	if facts != ([6]int64{2, 1, 1, 2, 2, 0}) {
		t.Fatal("dependency cycle/cancellation/read changed ledger or sent Transfer")
	}
	after, err := w.fixedFacts(ctx)
	agentAppRequire(t, err)
	if !bytes.Equal(after, w.fixed) {
		t.Fatal("dependency changed original ordering/content/Project/Agent/foreign Q facts")
	}
	raw, err := os.ReadFile(filepath.Join(w.directory, "task-dependency-result.json"))
	agentAppRequire(t, err)
	var result map[string]any
	agentAppRequire(t, json.Unmarshal(raw, &result))
	if result["completed"] != true || result["all_observed"] != true || result["retired"] != true || result["writes"] != float64(3) || result["committed"] != float64(2) || result["rejections"] != float64(1) {
		t.Fatal("dependency original browser result incomplete")
	}
	t.Log("TaskDependency: default App; 53 ordered same-P candidates, real cross-Sprint page 2; original 3 POST requests, 2 committed writes, 1 TASK_DEPENDENCY_CYCLE/409 with full zero-commit ledger; A versions 1,2,3; B/Q unchanged; runtime disabled")
}
