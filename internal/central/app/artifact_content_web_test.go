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

	ac "github.com/LunaDeerTech/agenteam/internal/central/artifact/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/audit"
	"github.com/LunaDeerTech/agenteam/internal/central/config"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/httpapi"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
)

func TestArtifactContentWeb(t *testing.T) {
	t.Run("create-and-recover-original", artifactContentWeb)
}

func artifactContentWeb(t *testing.T) {
	const prefix = "AGENTEAM_ARTIFACT_CONTENT_WEB"
	dist, runtime, evidence := os.Getenv(prefix+"_DIST"), os.Getenv("AGENTEAM_AUTH_WEB_RUNTIME"), os.Getenv(prefix+"_EVIDENCE")
	if !filepath.IsAbs(dist) || !filepath.IsAbs(runtime) || len(runtime) > 45 || !filepath.IsAbs(evidence) {
		t.Fatal("owned Artifact content browser paths required")
	}
	if info, err := os.Lstat(filepath.Join(dist, "index.html")); err != nil || !info.Mode().IsRegular() {
		t.Fatal("Artifact content actual build missing")
	}
	evidence = filepath.Join(evidence, t.Name())
	workAppCheck(t, "artifact-content-evidence-parent", os.MkdirAll(filepath.Dir(evidence), 0700))
	workAppCheck(t, "artifact-content-fresh-evidence", os.Mkdir(evidence, 0700))
	private, err := os.MkdirTemp(runtime, "acw-")
	workAppCheck(t, "artifact-content-private", err)
	t.Cleanup(func() { workAppCheck(t, "artifact-content-private-remove", os.RemoveAll(private)) })
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	workAppCheck(t, "artifact-content-listener", err)
	t.Cleanup(func() { _ = listener.Close() })
	origin := "http://" + listener.Addr().String()
	o := &artifactContentObserver{host: listener.Addr().String(), private: private, evidence: evidence, tails: map[string]*artifactWebTail{}}
	t.Cleanup(func() { o.save(t) })
	v := newWorkAppFixtureWith(t, func(root *modelRootApp, d *dependencies) {
		root.cfg = guardConfiguration(t, append(guardEnvironment(t, root.db), "AGENTEAM_CENTRAL_PUBLIC_ORIGIN="+origin, "AGENTEAM_CENTRAL_SHUTDOWN_TIMEOUT=5s"))
		open := d.open
		d.open = func(ctx context.Context, cfg postgres.Config) (database, error) {
			original, err := open(ctx, cfg)
			store, ok := original.(*workAppStore)
			if err != nil || !ok {
				return original, err
			}
			o.store = &humanArtifactAppStore{workAppStore: store}
			return o.store, nil
		}
		bind := d.bind
		d.bind = func(ctx context.Context, cfg config.Config, store database, owned *resources, deps *dependencies) error {
			if err := bind(ctx, cfg, store, owned, deps); err != nil {
				return err
			}
			assembly, ok := owned.accounts().(*accountAssembly)
			if !ok {
				return errors.New("original Artifact content accounts absent")
			}
			objects := deps.objects
			deps.objects = func(ctx context.Context, cfg config.Config, store database, auditor *audit.Service) (objectStorage, error) {
				original, err := objects(ctx, cfg, store, auditor)
				if err != nil {
					return original, err
				}
				assembly.mu.Lock()
				assembly.handler = o.handler(assembly.handler)
				assembly.mu.Unlock()
				return original, nil
			}
			return nil
		}
	})
	if o.store == nil {
		t.Fatal("original content Store missing")
	}
	t.Cleanup(o.store.releaseAll)
	v.assembly.mu.Lock()
	installed := v.assembly.artifacts
	v.assembly.mu.Unlock()
	if installed == nil || installed.pair == nil {
		t.Fatal("original content Artifact pair absent")
	}
	service := installed.pair.Service()
	workAppCheck(t, "artifact-content-original-binding", service.RequireProjectStopBinding(v.objects.service))
	backend, err := url.Parse(v.address)
	workAppCheck(t, "artifact-content-backend", err)
	transport := &http.Transport{Proxy: nil, DisableKeepAlives: true}
	proxy := httputil.NewSingleHostReverseProxy(backend)
	proxy.Transport, proxy.ErrorLog = transport, log.New(io.Discard, "", 0)
	proxy.ModifyResponse = o.capture
	proxy.ErrorHandler = func(w http.ResponseWriter, _ *http.Request, _ error) {
		o.fail()
		http.Error(w, "owned Artifact content observation failed", 502)
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
			o.store.releaseAll()
			if err := server.Shutdown(v.ctx); err != nil {
				t.Error("content proxy original shutdown exceeded budget")
				_ = server.Close()
			}
			if err := <-served; err != nil && !errors.Is(err, http.ErrServerClosed) {
				t.Error("content proxy original Serve failed")
			}
			transport.CloseIdleConnections()
		})
	}
	t.Cleanup(stop)
	check := func(stage string, err error) { t.Helper(); workAppCheck(t, "artifact-content-web-"+stage, err) }
	setup := &knowledgeWebSetup{t: t, ctx: v.ctx, origin: origin, cfg: v.root.cfg, check: check}
	t.Cleanup(setup.close)
	owner, cookie := artifactWebHuman(t, v, setup, origin)
	project := artifactWebProject(t, v, owner, "artifact-content")
	other := artifactWebProject(t, v, owner, "artifact-content-other")
	artifactWebCreate(t, v, service, owner, other, "other-project.txt", "text/plain", "Unchanged independent project.\n")
	beforeOther := artifactWebFixedFacts(t, v, other)
	inputs := []artifactContentIntent{
		{"Browser 世界🙂.md", "Original Markdown content", "text/markdown", "  # 世界🙂\n<script>window.contentExecuted=true</script>\n"},
		{"Published original.txt", "", "text/plain", "Original COMMIT completed; the ACK was unknown.\n"},
		{"Unobserved original.txt", "", "text/plain", "Explicit retry preserves this original request.\n"},
		{"Cancelled original.txt", "", "text/plain", "Prospective verified bytes will be retired.\n"},
	}
	payload := map[string]any{"mode": "create-and-recover-original", "origin": origin, "cookie": cookie, "project_id": project.String(), "other_project_id": other.String(), "user_id": owner.Details().UserID, "route": "/artifact-owner/artifact-content/artifacts", "inputs": inputs}
	raw, err := json.Marshal(payload)
	check("private-material", err)
	check("private-material-write", os.WriteFile(filepath.Join(private, "artifact-content-material.json"), raw, 0600))
	clear(raw)
	o.mu.Lock()
	o.project = project
	o.active = true
	o.mu.Unlock()
	result := artifactContentBrowser(t, v.ctx, private, origin, dist, evidence)
	o.mu.Lock()
	failed, count, creates, lookups, cancels, lists, reads := o.failed, o.count, o.creates, o.lookups, o.cancels, o.lists, o.reads
	keys := append([]f.IdempotencyKey(nil), o.keys...)
	hooks := append([]*usageAppHook(nil), o.hooks...)
	reject := o.reject
	firstObserved, firstHeldRelease := o.firstObserved, o.firstHeldRelease
	o.mu.Unlock()
	if failed || count != 16 || creates != 5 || lookups != 3 || cancels != 1 || lists != 4 || reads != 3 || len(keys) != 5 || keys[2] != keys[3] || result["completed"] != true || result["original_requests"] != float64(count) {
		t.Fatal("content browser actual request/action closure differs")
	}
	if len(hooks) != 3 {
		t.Fatal("content original Store controls missing")
	}
	if firstObserved == nil || !firstHeldRelease {
		t.Fatal("normal original Store was not held across the UI loading check")
	}
	usageAppWait(t, firstObserved.returned, "content normal original Store return")
	if firstObserved.result.State() != f.Committed || firstObserved.masked.State() != f.Committed || firstObserved.after <= firstObserved.before {
		t.Fatal("normal creation Store was not real COMMIT")
	}
	artifactContentObservedACK(t, hooks[1], project, keys[1], false)
	artifactContentObservedACK(t, hooks[2], project, keys[2], true)
	if reject == nil {
		t.Fatal("content prospective publication refusal missing")
	}
	o.store.mu.Lock()
	used, state := reject.used, reject.result.State()
	o.store.mu.Unlock()
	if !used || state != f.NotCommitted {
		t.Fatal("content final publication was not refused before BEGIN")
	}
	human := &humanArtifactApp{workAppFixture: v, tracked: o.store, pair: installed.pair, service: service}
	artifactContentCancelledFacts(t, human, project, keys[4])
	for index, key := range []f.IdempotencyKey{keys[0], keys[1], keys[2]} {
		inv, err := ac.NewInvocation(ac.InvocationDetails{Actor: owner, ProjectID: project, AttemptID: guardID[ac.CallAttempt](t)})
		check("canonical-invocation", err)
		input := inputs[index]
		found, err := service.LookupCreation(v.ctx, inv, f.CommandMeta{RequestID: guardID[f.Request](t), IdempotencyKey: key}, ac.Display{Name: input.Name, Description: input.Description}, input.Media, input.Content)
		check("original-canonical-lookup", err)
		if found.State != ac.CreationCommitted || found.Artifact == nil {
			t.Fatal("browser receipt has no original committed command")
		}
	}
	var artifacts, converges, active int
	check("final-business-facts", v.admin.QueryRow(v.ctx, `SELECT (SELECT count(*) FROM agenteam_artifact.artifacts WHERE project_id=$1::uuid),(SELECT count(*) FROM agenteam_artifact.project_work WHERE project_id=$1::uuid AND kind='converge'),(SELECT count(*) FROM agenteam_object.object_leases WHERE process_id=$2::uuid AND state='active')`, project.String(), v.objects.process.String()).Scan(&artifacts, &converges, &active))
	if artifacts != 3 || converges != 1 || active != 0 || artifactWebFixedFacts(t, v, other) != beforeOther {
		t.Fatal("content browser duplicate creation/cancel or leaked Object/foreign change")
	}
	var createsAudit, listsAudit, readsAudit, invalidAudit int64
	check("current-human-audit", v.admin.QueryRow(v.ctx, `SELECT
 count(*) FILTER(WHERE action='artifact.create'),count(*) FILTER(WHERE action='artifact.list'),count(*) FILTER(WHERE action='artifact.read'),
 count(*) FILTER(WHERE actor_kind<>'human' OR user_id<>$2::uuid OR session_id<>$3::uuid OR outcome<>'success'
 OR execution_id IS NOT NULL OR operation_id IS NOT NULL OR tool_id IS NOT NULL OR tool_call_id IS NOT NULL
 OR metadata ?| ARRAY['name','description','content','text'])
 FROM agenteam_audit.audit_records WHERE project_id=$1::uuid AND producer='artifact'`, project.String(), owner.Details().UserID, owner.Details().SessionID).Scan(&createsAudit, &listsAudit, &readsAudit, &invalidAudit))
	if createsAudit != 3 || listsAudit != 4 || readsAudit != 3 || invalidAudit != 0 {
		t.Fatal("content browser real Human audit missing, duplicated or misattributed")
	}
	stop()
	v.joined(t)
	if !installed.pair.Joined() {
		t.Fatal("content original Artifact owner did not join")
	}
	t.Log("ARTIFACT_CONTENT_WEB actual_create_lookup_retry_cancel=true original_post9_get7=true local_typed_ack_only=true not_observed_no_cancel=true pending_object_retired=true current_owner=true original_node_handler_proxy_root_guard_joined=true runtime_disabled=true")
}

type artifactContentObserver struct {
	mu                                             sync.Mutex
	host, private, evidence                        string
	project                                        id.ProjectID
	store                                          *humanArtifactAppStore
	active, failed                                 bool
	tails                                          map[string]*artifactWebTail
	count, creates, lookups, cancels, lists, reads int
	keys                                           []f.IdempotencyKey
	hooks                                          []*usageAppHook
	reject                                         *humanArtifactPublishRejection
	firstObserved                                  *usageAppObservation
	firstHeldRelease                               bool
}

func (o *artifactContentObserver) fail() error {
	o.mu.Lock()
	o.failed = true
	o.mu.Unlock()
	return errors.New("content original observation failed")
}
func (o *artifactContentObserver) save(t *testing.T) {
	o.mu.Lock()
	returned := 0
	for _, tail := range o.tails {
		if tail.returned {
			returned++
		}
	}
	raw, err := json.Marshal(map[string]any{"failed": o.failed, "accepted": o.count, "handler_started": len(o.tails), "handler_returned": returned, "creates": o.creates, "lookups": o.lookups, "cancels": o.cancels, "lists": o.lists, "reads": o.reads, "first_store_observed": o.firstObserved != nil, "first_store_held_until_ui_release": o.firstHeldRelease})
	o.mu.Unlock()
	if err != nil || os.WriteFile(filepath.Join(o.evidence, "artifact-content-driver.json"), raw, 0600) != nil {
		t.Error("content safe diagnostic write failed")
	}
}
func (o *artifactContentObserver) handler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		o.mu.Lock()
		selected := o.active && strings.Contains(r.URL.Path, "/artifacts")
		o.mu.Unlock()
		if !selected {
			next.ServeHTTP(w, r)
			return
		}
		rid := httpapi.RequestID(r.Context())
		if r.Host != o.host || rid.Validate() != nil || (r.Method != "GET" && r.Method != "POST") {
			_ = o.fail()
			http.Error(w, "content request rejected", 502)
			return
		}
		tail := &artifactWebTail{done: make(chan struct{}), method: r.Method, uri: r.URL.RequestURI()}
		o.mu.Lock()
		if o.tails[rid.String()] != nil || len(o.tails) >= 32 {
			o.mu.Unlock()
			_ = o.fail()
			http.Error(w, "content duplicate request", 502)
			return
		}
		o.tails[rid.String()] = tail
		o.mu.Unlock()
		defer close(tail.done)
		var releaseDone chan struct{}
		var releaseCancel context.CancelFunc
		if r.Method == "POST" && r.URL.Path == artifactContentCommandPath(o.project, "create") {
			key, err := f.ParseIdempotencyKey(r.Header.Get("Idempotency-Key"))
			if err != nil {
				_ = o.fail()
				http.Error(w, "content identity rejected", 502)
				return
			}
			o.mu.Lock()
			o.keys = append(o.keys, key)
			ordinal := len(o.keys)
			o.mu.Unlock()
			var hook *usageAppHook
			switch ordinal {
			case 1:
				hook = humanArtifactPublishHook(o.project, key, false, false)
			case 2:
				hook = humanArtifactPublishHook(o.project, key, true, false)
			case 3:
				hook = artifactContentMissingACK(o.project, key)
			case 4:
				o.mu.Lock()
				same := key == o.keys[2]
				o.mu.Unlock()
				if !same {
					_ = o.fail()
					http.Error(w, "content retry identity changed", 502)
					return
				}
			case 5:
				reject := &humanArtifactPublishRejection{project: o.project, key: key, err: f.NewFault(f.DependencyUnavailable, f.NotCommitted).WithCause(errors.New("controlled original final publication refusal"))}
				o.store.mu.Lock()
				o.store.reject = reject
				o.store.mu.Unlock()
				o.mu.Lock()
				o.reject = reject
				o.mu.Unlock()
			default:
				_ = o.fail()
				http.Error(w, "unexpected content create", 502)
				return
			}
			if hook != nil {
				o.store.arm(hook)
				o.mu.Lock()
				o.hooks = append(o.hooks, hook)
				o.mu.Unlock()
				if ordinal != 1 {
					hook.open()
				} else {
					releaseDone = make(chan struct{})
					var releaseContext context.Context
					releaseContext, releaseCancel = context.WithCancel(r.Context())
					defer func() {
						releaseCancel()
						<-releaseDone
					}()
					go func() {
						defer close(releaseDone)
						defer hook.open()
						// This is the sole consumer of the original COMMIT witness.
						// The browser cannot release an as-yet-unreached barrier.
						var observed usageAppObservation
						select {
						case observed = <-hook.observed:
						case <-releaseContext.Done():
							_ = o.fail()
							return
						case <-hook.release:
							_ = o.fail()
							return
						}
						if observed.result.State() != f.Committed || observed.masked.State() != f.Committed || observed.after <= observed.before || !humanArtifactCommand(observed.cause, o.project, key) {
							_ = o.fail()
							return
						}
						select {
						case <-observed.returned:
							_ = o.fail()
							return
						case <-hook.release:
							_ = o.fail()
							return
						default:
						}
						o.mu.Lock()
						o.firstObserved = &observed
						o.mu.Unlock()
						if err := os.WriteFile(filepath.Join(o.private, "reached-content"), []byte("held"), 0600); err != nil {
							_ = o.fail()
							return
						}
						tick := time.NewTicker(10 * time.Millisecond)
						defer tick.Stop()
						for {
							if _, err := os.Stat(filepath.Join(o.private, "release-content")); err == nil {
								select {
								case <-observed.returned:
									_ = o.fail()
									return
								case <-hook.release:
									_ = o.fail()
									return
								default:
								}
								o.mu.Lock()
								o.firstHeldRelease = true
								o.mu.Unlock()
								return
							} else if !errors.Is(err, os.ErrNotExist) {
								_ = o.fail()
								return
							}
							select {
							case <-tick.C:
							case <-releaseContext.Done():
								_ = o.fail()
								return
							case <-hook.release:
								_ = o.fail()
								return
							}
						}
					}()
				}
			}
		}
		next.ServeHTTP(w, r)
		if releaseDone != nil {
			releaseCancel()
			<-releaseDone
		}
		o.mu.Lock()
		tail.returned = true
		o.mu.Unlock()
	})
}
func (o *artifactContentObserver) capture(response *http.Response) error {
	o.mu.Lock()
	selected := o.active && strings.Contains(response.Request.URL.Path, "/artifacts")
	o.mu.Unlock()
	if !selected {
		return nil
	}
	raw, readErr := io.ReadAll(io.LimitReader(response.Body, (1<<20)+1))
	closeErr := response.Body.Close()
	if readErr != nil || closeErr != nil || len(raw) > 1<<20 || response.ContentLength != int64(len(raw)) || !json.Valid(raw) || response.Header.Get("Cache-Control") != "no-store" || len(response.Header.Values("X-Request-ID")) != 1 {
		return o.fail()
	}
	rid, err := f.ParseID[f.Request](response.Header.Get("X-Request-ID"))
	if err != nil {
		return o.fail()
	}
	o.mu.Lock()
	tail := o.tails[rid.String()]
	o.mu.Unlock()
	if tail == nil || tail.uri != response.Request.URL.RequestURI() || tail.method != response.Request.Method {
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
	if response.StatusCode != 200 {
		var problem httpapi.Problem
		if skillWebDecode(raw, &problem) != nil || response.StatusCode != 503 || !((problem.Code == f.CommitUnknown && problem.CommitState == f.Unknown && problem.RetryHint == "lookup") || (problem.Code == f.DependencyUnavailable && problem.CommitState == f.NotCommitted)) {
			return o.fail()
		}
	}
	record := map[string]any{"request_id": rid.String(), "method": tail.method, "url_sha256": fmt.Sprintf("%x", sha256.Sum256([]byte(tail.uri))), "status": response.StatusCode, "content_length": len(raw), "body_sha256": fmt.Sprintf("%x", sha256.Sum256(raw)), "original_eof": true, "original_closed": true, "handler_returned": true, "request_host_matches": true}
	encoded, err := json.Marshal(record)
	if err != nil {
		return o.fail()
	}
	if err = os.WriteFile(filepath.Join(o.evidence, "artifact-content-response-"+rid.String()+".json"), encoded, 0600); err != nil {
		return o.fail()
	}
	o.mu.Lock()
	o.count++
	switch {
	case strings.HasSuffix(response.Request.URL.Path, "/create-content"):
		o.creates++
	case strings.HasSuffix(response.Request.URL.Path, "/lookup"):
		o.lookups++
	case strings.HasSuffix(response.Request.URL.Path, "/cancel"):
		o.cancels++
	case strings.HasSuffix(response.Request.URL.Path, "/content"):
		o.reads++
	default:
		o.lists++
	}
	o.mu.Unlock()
	response.Body = io.NopCloser(bytes.NewReader(raw))
	return nil
}

func artifactContentBrowser(t *testing.T, parent context.Context, private, origin, dist, evidence string) map[string]any {
	t.Helper()
	harness := os.Getenv("AGENTEAM_ARTIFACT_CONTENT_WEB_HARNESS")
	if !filepath.IsAbs(harness) {
		t.Fatal("owned content harness required")
	}
	ctx, cancel := context.WithTimeout(parent, 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "node", filepath.Join(harness, "node_modules/@playwright/test/cli.js"), "test", "--config", filepath.Join(harness, "artifact-content.config.js"))
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
		workAppCheck(t, "content-child-cache", os.MkdirAll(path, 0700))
		cmd.Env = append(cmd.Env, name+"="+path)
	}
	cmd.Env = append(cmd.Env, "PLAYWRIGHT_NO_COPY_PROMPT=1", "AGENTEAM_AUTH_WEB_ORIGIN="+origin, "AGENTEAM_AUTH_WEB_PRIVATE="+private, "AGENTEAM_AUTH_WEB_CHROMIUM=/usr/bin/chromium", "AGENTEAM_ARTIFACT_CONTENT_WEB_DIST="+dist, "AGENTEAM_ARTIFACT_CONTENT_WEB_EVIDENCE="+evidence, "AGENTEAM_ARTIFACT_CONTENT_WEB_CASE=create-and-recover-original")
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	if err := cmd.Start(); err != nil {
		t.Fatal("content Node original Start failed")
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
	t.Logf("Artifact content Node actual_wait pid=%d success=%t", cmd.Process.Pid, err == nil)
	if err != nil || !regexp.MustCompile(`(?m)\b1 passed\b`).Match(output.Bytes()) {
		t.Fatal("content real browser did not pass its one case")
	}
	raw, err := os.ReadFile(filepath.Join(private, "artifact-content-result.json"))
	workAppCheck(t, "content-browser-result", err)
	var result map[string]any
	if len(raw) > 16<<10 || json.Unmarshal(raw, &result) != nil {
		t.Fatal("content browser result invalid")
	}
	return result
}
