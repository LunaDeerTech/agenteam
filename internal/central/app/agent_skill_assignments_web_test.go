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
	"reflect"
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
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/skill/contract"
)

// The browser configures an existing Agent and package through the unchanged
// default App. Runtime is disabled; the real ingress observes no active slot.
func TestAgentSkillAssignmentsWeb(t *testing.T) {
	started := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	t.Cleanup(func() {
		cancel()
		if time.Since(started) > 120*time.Second {
			t.Error("Agent Skills browser exceeded original 120s including cleanup")
		}
	})
	const prefix = "AGENTEAM_AGENT_SKILLS_WEB"
	dist, runtime, evidence := os.Getenv(prefix+"_DIST"), os.Getenv("AGENTEAM_AUTH_WEB_RUNTIME"), os.Getenv(prefix+"_EVIDENCE")
	inputHash := os.Getenv(prefix + "_INPUT_HASH")
	if !filepath.IsAbs(dist) || !filepath.IsAbs(runtime) || len(runtime) > 45 || !filepath.IsAbs(evidence) || len(inputHash) != 64 || strings.Trim(inputHash, "0123456789abcdef") != "" {
		t.Fatal("owned Agent Skills browser inputs required")
	}
	if info, err := os.Lstat(filepath.Join(dist, "index.html")); err != nil || !info.Mode().IsRegular() {
		t.Fatal("frozen Agent Skills assets missing")
	}
	evidence = filepath.Join(evidence, t.Name())
	workAppCheck(t, "skills-web-evidence", os.Mkdir(evidence, 0700))
	private, err := os.MkdirTemp(runtime, "asw-")
	workAppCheck(t, "skills-web-private", err)
	t.Cleanup(func() { workAppCheck(t, "skills-web-private-remove", os.RemoveAll(private)) })
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	workAppCheck(t, "skills-web-listener", err)
	t.Cleanup(func() { _ = listener.Close() })
	origin := "http://" + listener.Addr().String()
	o := &agentSkillsWebObserver{host: listener.Addr().String(), evidence: evidence, inputHash: inputHash, tails: map[string]*agentSkillsWebTail{}}
	t.Cleanup(func() { o.save(t) })
	v := newWorkAppFixtureWith(t, func(root *modelRootApp, d *dependencies) {
		root.cfg = guardConfiguration(t, append(guardEnvironment(t, root.db), "AGENTEAM_CENTRAL_PUBLIC_ORIGIN="+origin, "AGENTEAM_CENTRAL_SHUTDOWN_TIMEOUT=5s"))
		bind := d.bind
		d.bind = func(c context.Context, cfg config.Config, store database, owned *resources, deps *dependencies) error {
			if err := bind(c, cfg, store, owned, deps); err != nil {
				return err
			}
			assembly, ok := owned.accounts().(*accountAssembly)
			if !ok {
				return errors.New("original Agent Skills assembly absent")
			}
			objects := deps.objects
			deps.objects = func(c context.Context, cfg config.Config, store database, auditor *audit.Service) (objectStorage, error) {
				actual, err := objects(c, cfg, store, auditor)
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
	stopDeadline := context.AfterFunc(ctx, v.root.cancel)
	defer stopDeadline()
	agent := skillAssignmentAppAgent(t, v)
	base := "/api/v1/projects/" + v.a.String()
	assignmentPath := base + "/agents/" + agent.String() + "/skills"
	var initial skillAssignmentHTTPPage
	skillAssignmentAppCall(t, v, "GET", assignmentPath, nil, "", 200, &initial)
	if initial.AgentVersion != 1 || initial.Sequence != 1 || len(initial.Items) != 1 || !initial.Items[0].Enabled {
		t.Fatal("formal initial grant absent")
	}
	ordinary := guardID[pc.Skill](t)
	skillAssignmentAppCall(t, v, "POST", base+"/skills", map[string]any{"request": map[string]any{"skill_id": ordinary, "mode": "create", "source": map[string]any{"kind": "text_files", "files": []map[string]string{{"path": "SKILL.md", "utf8_text": "---\nname: assignment-browser\ndescription: Explicit browser assignment\n---\nUse existing permissions only.\n"}}}}}, guardID[struct{}](t).String(), 200, nil)
	selection := skillAssignmentAppSelection(t, v, agent)
	var session struct {
		User struct {
			Username string `json:"username"`
		} `json:"user"`
	}
	skillAssignmentAppCall(t, v, "GET", "/api/v1/session", nil, "", 200, &session)
	if session.User.Username == "" {
		t.Fatal("formal route username absent")
	}
	backend, err := url.Parse(v.address)
	workAppCheck(t, "skills-web-backend", err)
	transport := &http.Transport{Proxy: nil, DisableKeepAlives: true}
	proxy := httputil.NewSingleHostReverseProxy(backend)
	proxy.Transport = transport
	proxy.ErrorLog = log.New(io.Discard, "", 0)
	proxy.ModifyResponse = o.capture
	proxy.ErrorHandler = func(w http.ResponseWriter, _ *http.Request, _ error) {
		_ = o.fail("proxy")
		http.Error(w, "owned Agent Skills observation failed", 502)
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
			if err := server.Shutdown(ctx); err != nil {
				t.Error("Agent Skills proxy exceeded original budget")
				_ = server.Close()
			}
			if err := <-served; err != nil && !errors.Is(err, http.ErrServerClosed) {
				t.Error("Agent Skills original Serve failed")
			}
			transport.CloseIdleConnections()
		})
	}
	t.Cleanup(stop)
	payload := map[string]any{"mode": "assign-disable-enable-remove", "cookie": v.cookie, "owner_id": v.actor.Details().UserID, "project_id": v.a.String(), "agent_id": agent.String(), "skill_id": ordinary.String(), "builtin": initial.Items[0], "route": "/" + session.User.Username + "/work-app-a/settings/agents", "input_hash": inputHash}
	raw := workAppJSON(t, payload)
	workAppCheck(t, "skills-web-material", os.WriteFile(filepath.Join(private, "agent-skills-material.json"), raw, 0600))
	clear(raw)
	o.mu.Lock()
	o.path = assignmentPath
	o.active = true
	o.mu.Unlock()
	result := agentSkillsWebBrowser(t, ctx, private, origin, dist, evidence, inputHash)
	o.mu.Lock()
	failed, accepted := o.failed, o.accepted
	receipts := append([]sc.AssignmentMutation(nil), o.receipts...)
	o.active = false
	o.mu.Unlock()
	if failed || len(receipts) != 4 || result["completed"] != true || result["original_requests"] != float64(accepted) || result["writes"] != float64(4) {
		t.Fatal("Agent Skills browser actual closure differs")
	}
	if receipts[0].Assignment == nil || receipts[1].Assignment == nil || receipts[2].Assignment == nil || receipts[3].Assignment != nil {
		t.Fatal("Agent Skills receipt shape differs")
	}
	for n, r := range receipts {
		if r.Validate() != nil || !r.Changed || r.ProjectID != v.a || r.AgentID != agent || r.AgentVersion != f.Version(n+2) || r.AssignmentSequence != f.Version(n+2) {
			t.Fatal("Agent Skills actual transition differs")
		}
		if n < 3 && (r.Assignment.SkillID != ordinary || r.Assignment.Enabled != (n != 1)) {
			t.Fatal("Agent Skills enabled semantics differ")
		}
		if n == 0 || n == 2 {
			if r.Runtime == nil || r.Runtime.Queued || r.Runtime.ExecutionID != nil {
				t.Fatal("Agent Skills no-active ingress differs")
			}
		} else if r.Runtime != nil {
			t.Fatal("disabled/removed assignment queued ingress")
		}
	}
	if receipts[0].Assignment.ID == receipts[1].Assignment.ID || receipts[1].Assignment.ID == receipts[2].Assignment.ID {
		t.Fatal("Agent Skills reused retired grant identity")
	}
	var final skillAssignmentHTTPPage
	skillAssignmentAppCall(t, v, "GET", assignmentPath, nil, "", 200, &final)
	if final.AgentVersion != 5 || final.Sequence != 5 || len(final.Items) != 1 || !reflect.DeepEqual(final.Items[0], initial.Items[0]) || final.NextCursor != "" {
		t.Fatal("browser writes lost original builtin grant")
	}
	skillAssignmentAppAtomic(t, v, receipts[3])
	if skillAssignmentAppSelection(t, v, agent) != selection {
		t.Fatal("browser changed unrelated Agent configuration")
	}
	var exact bool
	workAppCheck(t, "skills-web-durable-counts", v.admin.QueryRow(v.ctx, `SELECT
 (SELECT count(*)=4 AND bool_and(state='completed') FROM agenteam_skill.assignment_commands WHERE agent_id::text=$1)
 AND (SELECT count(*)=4 FROM agenteam_skill.assignment_changes WHERE agent_id::text=$1)
 AND (SELECT count(*)=2 AND bool_and(outcome='no_active_execution' AND execution_id IS NULL) FROM agenteam_execution.skill_assignment_ingress WHERE agent_id::text=$1)
 AND (SELECT count(*)=4 FROM agenteam_outbox.events WHERE producer='skill' AND aggregate_id::text=$1)
 AND (SELECT count(*)=4 FROM agenteam_audit.audit_records WHERE producer='skill' AND resource_id::text=$1)
 AND NOT EXISTS(SELECT 1 FROM agenteam_skill.assignment_pending_refs)
 AND (SELECT count(*)=2 FROM agenteam_skill.skills WHERE project_id::text=$2 AND serving=true)`, agent.String(), v.a.String()).Scan(&exact))
	if !exact {
		t.Fatal("browser duplicated changes or removed packages")
	}
	stop()
	v.joined(t)
	t.Log("AGENT_SKILLS_WEB default_app=true writes4_lookup0=true explicit_assign_disable_enable_remove=true public_receipt_identity=true current_reads=true initial_grant_preserved=true packages_preserved=true no_execution_model_tools=true original_handler_reader_consumer_wait_joined=true")
}

type agentSkillsWebTail struct {
	done        chan struct{}
	returned    bool
	method, uri string
}
type agentSkillsWebObserver struct {
	mu                                     sync.Mutex
	host, evidence, inputHash, path, stage string
	active, failed                         bool
	tails                                  map[string]*agentSkillsWebTail
	accepted                               int
	receipts                               []sc.AssignmentMutation
}

func (o *agentSkillsWebObserver) fail(stage string) error {
	o.mu.Lock()
	o.failed = true
	if o.stage == "" {
		o.stage = stage
	}
	o.mu.Unlock()
	return errors.New("original Agent Skills observation failed")
}
func (o *agentSkillsWebObserver) handler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		o.mu.Lock()
		active, path := o.active, o.path
		o.mu.Unlock()
		if !active || !strings.HasPrefix(r.URL.Path, "/api/") {
			next.ServeHTTP(w, r)
			return
		}
		rid := httpapi.RequestID(r.Context())
		if r.Host != o.host || rid.Validate() != nil || (r.Method != "GET" && (r.Method != "POST" || (r.URL.Path != path+"/assign" && r.URL.Path != path+"/remove"))) {
			_ = o.fail("request_scope")
			http.Error(w, "owned request rejected", 502)
			return
		}
		tail := &agentSkillsWebTail{done: make(chan struct{}), method: r.Method, uri: r.URL.RequestURI()}
		o.mu.Lock()
		if o.tails[rid.String()] != nil || len(o.tails) >= 64 {
			o.mu.Unlock()
			_ = o.fail("request_bound")
			http.Error(w, "owned request bound", 502)
			return
		}
		o.tails[rid.String()] = tail
		o.mu.Unlock()
		defer close(tail.done)
		next.ServeHTTP(w, r)
		o.mu.Lock()
		tail.returned = true
		o.mu.Unlock()
	})
}
func (o *agentSkillsWebObserver) capture(response *http.Response) error {
	o.mu.Lock()
	active := o.active
	o.mu.Unlock()
	if !active || !strings.HasPrefix(response.Request.URL.Path, "/api/") {
		return nil
	}
	raw, readErr := io.ReadAll(io.LimitReader(response.Body, (2<<20)+1))
	closeErr := response.Body.Close()
	if readErr != nil || closeErr != nil || len(raw) > 2<<20 || response.ContentLength != int64(len(raw)) || !json.Valid(raw) || response.StatusCode != 200 || response.Header.Get("Cache-Control") != "no-store" || len(response.Header.Values("X-Request-ID")) != 1 {
		return o.fail("response_integrity")
	}
	rid, err := f.ParseID[f.Request](response.Header.Get("X-Request-ID"))
	if err != nil {
		return o.fail("response_identity")
	}
	o.mu.Lock()
	tail := o.tails[rid.String()]
	o.mu.Unlock()
	if tail == nil || tail.method != response.Request.Method || tail.uri != response.Request.URL.RequestURI() {
		return o.fail("handler_correlation")
	}
	select {
	case <-tail.done:
	case <-response.Request.Context().Done():
		return o.fail("handler_cancelled")
	}
	if !tail.returned {
		return o.fail("handler_return")
	}
	if tail.method == "POST" {
		var receipt sc.AssignmentMutation
		if len(raw) > 128<<10 || skillWebDecode(raw, &receipt) != nil || receipt.Validate() != nil {
			return o.fail("original_mutation")
		}
		o.mu.Lock()
		o.receipts = append(o.receipts, receipt)
		o.mu.Unlock()
	}
	record := map[string]any{"input_hash": o.inputHash, "request_id": rid.String(), "method": tail.method, "url_sha256": fmt.Sprintf("%x", sha256.Sum256([]byte(tail.uri))), "status": response.StatusCode, "content_length": len(raw), "body_sha256": fmt.Sprintf("%x", sha256.Sum256(raw)), "original_eof": true, "original_closed": true, "handler_returned": true, "request_host_matches": true}

	// Only fixture-owned Agent/Skill public projections may enter evidence.
	// Session responses contain private CSRF material and remain hash-only.
	agentBase := strings.TrimSuffix(o.path, "/skills")
	projectBase := strings.Split(agentBase, "/agents/")[0]
	path := response.Request.URL.Path
	if strings.HasPrefix(path, o.path) || path == agentBase+"/configuration" || path == agentBase+"/runner-mounts" || path == projectBase+"/agents" || path == projectBase+"/skills/catalog" {
		record["value"] = json.RawMessage(raw)
	}
	encoded, err := json.Marshal(record)
	if err != nil {
		return o.fail("evidence_encode")
	}
	if os.WriteFile(filepath.Join(o.evidence, "agent-skills-response-"+rid.String()+".json"), encoded, 0600) != nil {
		return o.fail("evidence_write")
	}
	o.mu.Lock()
	o.accepted++
	o.mu.Unlock()
	response.Body = io.NopCloser(bytes.NewReader(raw))
	return nil
}
func (o *agentSkillsWebObserver) save(t *testing.T) {
	t.Helper()
	o.mu.Lock()
	returned := 0
	for _, tail := range o.tails {
		if tail.returned {
			returned++
		}
	}
	raw, err := json.Marshal(map[string]any{"failed": o.failed, "stage": o.stage, "accepted": o.accepted, "handler_started": len(o.tails), "handler_returned": returned, "writes": len(o.receipts)})
	o.mu.Unlock()
	if err != nil || os.WriteFile(filepath.Join(o.evidence, "agent-skills-driver.json"), raw, 0600) != nil {
		t.Error("Agent Skills diagnostic write failed")
	}
}
func agentSkillsWebBrowser(t *testing.T, parent context.Context, private, origin, dist, evidence, inputHash string) map[string]any {
	t.Helper()
	harness, err := filepath.Abs("../../../tests/account-captcha-web")
	workAppCheck(t, "skills-web-harness", err)
	ctx, cancel := context.WithTimeout(parent, 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "node", filepath.Join(harness, "node_modules/@playwright/test/cli.js"), "test", "--config", filepath.Join(harness, "agent-skills.config.js"))
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
		workAppCheck(t, "skills-web-child-cache", os.MkdirAll(path, 0700))
		cmd.Env = append(cmd.Env, name+"="+path)
	}
	cmd.Env = append(cmd.Env, "PLAYWRIGHT_NO_COPY_PROMPT=1", "AGENTEAM_AUTH_WEB_ORIGIN="+origin, "AGENTEAM_AUTH_WEB_PRIVATE="+private, "AGENTEAM_AUTH_WEB_CHROMIUM=/usr/bin/chromium", "AGENTEAM_AGENT_SKILLS_WEB_DIST="+dist, "AGENTEAM_AGENT_SKILLS_WEB_EVIDENCE="+evidence, "AGENTEAM_AGENT_SKILLS_WEB_INPUT_HASH="+inputHash, "AGENTEAM_AGENT_SKILLS_WEB_CASE=assign-disable-enable-remove")
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	if err := cmd.Start(); err != nil {
		t.Fatal("Agent Skills original Node did not start")
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
	t.Logf("AgentSkills Node actual_wait pid=%d success=%t", cmd.Process.Pid, err == nil)
	if err != nil || !regexp.MustCompile(`(?m)\b1 passed\b`).Match(output.Bytes()) {
		t.Fatal("Agent Skills real browser did not pass its one case")
	}
	raw, err := os.ReadFile(filepath.Join(private, "agent-skills-result.json"))
	workAppCheck(t, "skills-web-result", err)
	var result map[string]any
	if len(raw) > 16<<10 || json.Unmarshal(raw, &result) != nil || result["input_hash"] != inputHash {
		t.Fatal("Agent Skills browser result invalid")
	}
	return result
}
