//go:build integration

package projectvariable_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/account"
	"github.com/LunaDeerTech/agenteam/internal/central/agent"
	agenthttp "github.com/LunaDeerTech/agenteam/internal/central/agent/http"
	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/httpapi"
	"github.com/LunaDeerTech/agenteam/internal/central/outbox"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/project"
	projecthttp "github.com/LunaDeerTech/agenteam/internal/central/project/http"
	"github.com/LunaDeerTech/agenteam/internal/central/usage"
	usagehttp "github.com/LunaDeerTech/agenteam/internal/central/usage/http"
	"github.com/LunaDeerTech/agenteam/internal/central/work"
	wc "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
	workhttp "github.com/LunaDeerTech/agenteam/internal/central/work/http"
)

// Only the browser changes these Tasks. Setup uses the original real completed
// work Execution and, in the recovery case, one normal Human review transfer.
func TestTaskReviewWeb(t *testing.T) {
	for _, mode := range []string{"review-complete", "lost-confirmation-lookup-and-session-revocation"} {
		t.Run(mode, func(t *testing.T) {
			x := newTaskReviewFixture(t)
			if mode != "review-complete" {
				before := x.current(t)
				r, m, lookup := x.intent(t, before, wc.TaskStateInReview, &x.reviewer, "Ready for the browser's review decision.", "web-review-setup")
				g := x.generations(t, before, r.TargetState)
				receipt, err := x.domain.transitions.TransferTask(ctxFor(t), x.domain.base.ownerBrowser.actor, m, before.ProjectID, before.ID, r)
				firstRoundRequire(t, err)
				x.requireCommitted(t, before, r, m, lookup, g, nil, receipt)
			}
			web := newTaskReviewWeb(t, x, mode)
			web.browser(t)
			web.close(t)
			web.verify(t)
			x.requireLineage(t)
		})
	}
}

type reviewWebIntent struct {
	before      wc.Task
	request     wc.TaskTransfer
	key         f.IdempotencyKey
	generations [3]int64
}
type reviewWebIntentKey struct{}
type reviewWebReceipt struct {
	intent  reviewWebIntent
	receipt wc.TaskTransitionMutation
}
type reviewWebTail struct {
	done     chan struct{}
	returned bool
}
type reviewWebDrop struct {
	header http.Header
	length int
}

func (*reviewWebDrop) Error() string { return "owned committed review receipt cut" }

type taskReviewWeb struct {
	x                                            *taskReviewFixture
	mode, origin, directory, evidence, inputHash string
	backend                                      *httptest.Server
	front                                        *http.Server
	serve                                        chan error
	transport                                    *http.Transport
	frontWG, backendWG                           sync.WaitGroup
	mu                                           sync.Mutex
	tails                                        map[string]*reviewWebTail
	receipts                                     []reviewWebReceipt
	keys                                         []string
	failure                                      error
	transfers, lookups, logouts, cut             int
	closed                                       bool
}

func newTaskReviewWeb(t *testing.T, x *taskReviewFixture, mode string) *taskReviewWeb {
	t.Helper()
	dist, runtime := os.Getenv("AGENTEAM_TASK_REVIEW_WEB_DIST"), os.Getenv("AGENTEAM_AUTH_WEB_RUNTIME")
	evidence, inputHash := os.Getenv("AGENTEAM_TASK_REVIEW_WEB_EVIDENCE"), os.Getenv("AGENTEAM_TASK_REVIEW_WEB_INPUT_HASH")
	if !filepath.IsAbs(dist) || !filepath.IsAbs(runtime) || len(runtime) > 45 || !filepath.IsAbs(evidence) || len(inputHash) != 64 || strings.Trim(inputHash, "0123456789abcdef") != "" {
		t.Fatal("explicit frozen dist, short owned runtime, evidence and input hash required")
	}
	if info, err := os.Stat(filepath.Join(dist, "index.html")); err != nil || !info.Mode().IsRegular() {
		t.Fatal("frozen production index missing")
	}
	directory, err := os.MkdirTemp(runtime, "trw-")
	firstRoundRequire(t, err)
	firstRoundRequire(t, os.Chmod(directory, 0700))
	w := &taskReviewWeb{x: x, mode: mode, directory: directory, evidence: evidence, inputHash: inputHash, tails: map[string]*reviewWebTail{}, serve: make(chan error, 1)}
	t.Cleanup(func() {
		w.close(t)
		if err := os.RemoveAll(directory); err != nil {
			t.Error("private browser material cleanup failed")
		}
		if _, err := os.Lstat(directory); !errors.Is(err, os.ErrNotExist) {
			t.Error("private browser material still exists")
		}
	})
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	firstRoundRequire(t, err)
	t.Cleanup(func() { _ = listener.Close() })
	w.origin = "http://" + listener.Addr().String()
	d := x.domain
	boundary, err := account.NewHTTPBoundary(d.base.core, w.origin)
	firstRoundRequire(t, err)
	profiles, err := account.NewProfileService(d.base.core, d.agent.p2.objects)
	firstRoundRequire(t, err)
	profile, err := profiles.GetProfile(ctxFor(t), d.base.ownerBrowser.actor)
	firstRoundRequire(t, err)
	system, err := account.NewSystemHTTPFacade(d.base.core, d.base.keys)
	firstRoundRequire(t, err)
	accounts, err := account.NewHTTPHandler(d.base.core, profiles, account.HTTPOptions{PublicOrigin: w.origin, System: system})
	firstRoundRequire(t, err)
	projectReader, err := project.NewReader(d.base.tracked, d.base.projectAuthority, d.base.keys)
	firstRoundRequire(t, err)
	projects, err := projecthttp.NewHTTPHandler(projectReader, boundary)
	firstRoundRequire(t, err)
	usageAuthority, err := usage.NewAuthority(d.base.tracked, usage.Authorizations{Sessions: d.base.accounts, Projects: d.base.projectAuthority, Invocations: nil})
	firstRoundRequire(t, err)
	usageReader, err := usage.New(d.base.tracked, usageAuthority, usage.Dependencies{Cursors: d.base.keys})
	firstRoundRequire(t, err)
	routes, err := usagehttp.NewHTTPHandler(d.base.projectAuthority, usageReader, boundary)
	firstRoundRequire(t, err)
	directoryReader, err := agent.NewDirectoryReader(d.base.tracked, d.agent.providers.Agents, d.base.keys)
	firstRoundRequire(t, err)
	agents, err := agenthttp.NewDirectoryHTTPHandler(directoryReader, boundary)
	firstRoundRequire(t, err)
	catalog := event.NewCatalog()
	events, err := wc.RegisterTaskBlockerEvents(catalog)
	firstRoundRequire(t, err)
	box, err := outbox.New(d.base.tracked, catalog, outbox.Authorizations{Producers: map[event.StableName]oc.ProducerAuthority{wc.WorkProducer: d.authority}, Sessions: d.base.accounts, System: d.base.accounts, Projects: d.base.projectAuthority, Audit: d.base.audit, Cursors: d.base.keys, Processes: fixtureProcess{id[oc.Process](t)}})
	firstRoundRequire(t, err)
	blockers, err := work.NewBlocker(d.base.tracked, work.BlockerDependencies{Authority: d.authority, Structure: d.structureReader, Events: box, BlockerEvents: events, Activity: d.base.accounts})
	firstRoundRequire(t, err)
	blockerReader, err := work.NewBlockerReader(d.base.tracked, d.authority, d.base.keys)
	firstRoundRequire(t, err)
	workHandler, err := workhttp.NewHTTPHandler(workhttp.Bindings{Structure: d.structure, StructureReader: d.structureReader, Tasks: d.tasks, TaskReader: d.taskReader, Blockers: blockers, BlockerReader: blockerReader, Transitions: d.transitions}, boundary)
	firstRoundRequire(t, err)
	// This cleanup is registered after the private-directory cleanup. Explicit
	// close first joins HTTP handlers; only then may the borrowed domain graph drain.
	t.Cleanup(func() {
		w.close(t)
		blockers.Stop()
		directoryReader.Stop()
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := blockers.Drain(ctx); err != nil {
			t.Error("browser Blocker service did not join", err)
		}
		if err := directoryReader.Drain(ctx); err != nil || !directoryReader.Joined() {
			t.Error("browser Agent directory did not join", err)
		}
	})
	w.backend = httptest.NewUnstartedServer(nil)
	w.backend.Config.ErrorLog = log.New(io.Discard, "", 0)
	w.backend.Config.Handler = httpapi.Handler(nil, http.HandlerFunc(func(out http.ResponseWriter, r *http.Request) {
		w.backendWG.Add(1)
		defer w.backendWG.Done()
		id := httpapi.RequestID(r.Context()).String()
		tail := &reviewWebTail{done: make(chan struct{})}
		w.mu.Lock()
		w.tails[id] = tail
		w.mu.Unlock()
		defer close(tail.done)
		switch {
		case agenthttp.HandlesDirectoryPath(r.URL.Path):
			agents.ServeHTTP(out, r)
		case workhttp.HandlesPath(r.URL.Path):
			workHandler.ServeHTTP(out, r)
		case usagehttp.HandlesPath(r.URL.Path):
			routes.ServeHTTP(out, r)
		case projecthttp.HandlesPath(r.URL.Path):
			projects.ServeHTTP(out, r)
		default:
			accounts.ServeHTTP(out, r)
		}
		w.mu.Lock()
		tail.returned = true
		w.mu.Unlock()
	}))
	w.backend.StartTLS()
	w.transport = w.backend.Client().Transport.(*http.Transport).Clone()
	if w.transport.TLSClientConfig.InsecureSkipVerify {
		t.Fatal("owned TLS verification disabled")
	}
	target, err := url.Parse(w.backend.URL)
	firstRoundRequire(t, err)
	proxy := httputil.NewSingleHostReverseProxy(target)
	proxy.Transport = w.transport
	proxy.ErrorLog = log.New(io.Discard, "", 0)
	proxy.ModifyResponse = w.response
	proxy.ErrorHandler = w.proxyError
	static := http.FileServer(http.Dir(dist))
	w.front = &http.Server{ReadHeaderTimeout: 2 * time.Second, ErrorLog: log.New(io.Discard, "", 0), Handler: http.HandlerFunc(func(out http.ResponseWriter, r *http.Request) {
		w.frontWG.Add(1)
		defer w.frontWG.Done()
		if strings.HasPrefix(r.URL.Path, "/api/") {
			if err := w.observeRequest(r); err != nil {
				w.fail(err)
				http.Error(out, "owned observation failed", 500)
				return
			}
			proxy.ServeHTTP(out, r)
			return
		}
		if r.Method != "GET" && r.Method != "HEAD" {
			http.NotFound(out, r)
			return
		}
		clean := filepath.Clean("/" + r.URL.Path)
		if info, err := os.Stat(filepath.Join(dist, clean)); err == nil && info.Mode().IsRegular() {
			static.ServeHTTP(out, r)
			return
		}
		http.ServeFile(out, r, filepath.Join(dist, "index.html"))
	})}
	go func() { w.serve <- w.front.Serve(listener) }()
	current := x.current(t)
	material := map[string]any{"mode": mode, "cookie": d.base.ownerBrowser.cookie, "project_id": d.base.project.ID, "task_id": current.ID, "sprint_id": current.SprintID, "title": current.Title, "version": current.Version, "worker_id": d.agentID, "reviewer_id": x.reviewer, "route": "/" + profile.User.Username + "/" + d.base.project.NormalizedName + "/tasks/" + current.ID.String()}
	raw, err := json.Marshal(material)
	firstRoundRequire(t, err)
	firstRoundRequire(t, os.WriteFile(filepath.Join(directory, "task-review-material.json"), raw, 0600))
	clear(raw)
	return w
}

func (w *taskReviewWeb) fail(err error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.failure == nil {
		w.failure = err
	}
}

func (w *taskReviewWeb) observeRequest(r *http.Request) error {
	if r.Method != "POST" {
		return nil
	}
	key := f.IdempotencyKey(r.Header.Get("Idempotency-Key"))
	w.mu.Lock()
	w.keys = append(w.keys, string(key))
	w.mu.Unlock()
	taskPath := "/api/v1/projects/" + w.x.domain.base.project.ID.String() + "/tasks/" + w.x.domain.task.ID.String() + "/transfer"
	lookupPath := "/api/v1/projects/" + w.x.domain.base.project.ID.String() + "/task-transition-commands/lookup"
	if r.URL.Path == "/api/v1/sessions/logout" {
		w.mu.Lock()
		w.logouts++
		w.mu.Unlock()
		return nil
	}
	if r.URL.Path != taskPath && r.URL.Path != lookupPath {
		return errors.New("unexpected browser mutation")
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, (512<<10)+1))
	closeErr := r.Body.Close()
	if err != nil || closeErr != nil || len(raw) > 512<<10 || key.Validate() != nil {
		return errors.New("original browser intent incomplete")
	}
	r.Body = io.NopCloser(bytes.NewReader(raw))
	var envelope struct {
		Expected f.Version                    `json:"expected_version"`
		Request  json.RawMessage              `json:"request"`
		Command  wc.TaskTransitionCommandName `json:"command,omitempty"`
		Target   wc.TaskID                    `json:"target_id,omitempty"`
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&envelope); err != nil {
		return errors.New("original intent envelope malformed")
	}
	request, err := wc.DecodeTaskTransfer(envelope.Request)
	if err != nil {
		return err
	}
	if r.URL.Path == lookupPath {
		w.mu.Lock()
		defer w.mu.Unlock()
		w.lookups++
		if len(w.receipts) != 1 || key != w.receipts[0].intent.key || envelope.Expected != w.receipts[0].intent.before.Version || envelope.Target != w.receipts[0].intent.before.ID || string(envelope.Command) != "work.task.transfer" || !reflect.DeepEqual(request, w.receipts[0].intent.request) {
			return errors.New("Lookup changed original browser intent")
		}
		return nil
	}
	d := w.x.domain
	before, err := d.taskReader.GetTask(r.Context(), d.base.ownerBrowser.actor, d.base.project.ID, d.task.ID)
	if err != nil || before.Version != envelope.Expected {
		return errors.New("browser lost original current version")
	}
	var g [3]int64
	err = d.base.raw.QueryRow(r.Context(), `SELECT
 (SELECT query_generation FROM agenteam_work.task_query_generations WHERE project_id=$1::text::uuid),
 coalesce((SELECT order_generation FROM agenteam_work.task_order_groups WHERE project_id=$1::text::uuid AND sprint_id=$2::text::uuid AND state=$3 AND priority=$5),1),
 coalesce((SELECT order_generation FROM agenteam_work.task_order_groups WHERE project_id=$1::text::uuid AND sprint_id=$2::text::uuid AND state=$4 AND priority=$5),1)`, before.ProjectID.String(), before.SprintID.String(), string(before.State), string(request.TargetState), string(before.Priority)).Scan(&g[0], &g[1], &g[2])
	if err != nil {
		return err
	}
	w.mu.Lock()
	w.transfers++
	count := w.transfers
	w.mu.Unlock()
	if w.mode != "review-complete" && count != 1 {
		return errors.New("uncertain browser intent was retransmitted")
	}
	intent := reviewWebIntent{before, request, key, g}
	*r = *r.WithContext(context.WithValue(r.Context(), reviewWebIntentKey{}, intent))
	return nil
}

func (w *taskReviewWeb) response(r *http.Response) error {
	// Read the original verified-TLS response to EOF and close it once. Do not
	// replace business results; the single declared fault cuts only their delivery.
	raw, err := io.ReadAll(io.LimitReader(r.Body, (2<<20)+1))
	closeErr := r.Body.Close()
	if err != nil || closeErr != nil || len(raw) > 2<<20 || r.ContentLength != int64(len(raw)) || r.TLS == nil || len(r.TLS.VerifiedChains) == 0 {
		return errors.New("original TLS body/length/close not completed")
	}
	id := r.Header.Get("X-Request-ID")
	w.mu.Lock()
	tail := w.tails[id]
	w.mu.Unlock()
	if tail == nil {
		return errors.New("original HTTP handler identity absent")
	}
	select {
	case <-tail.done:
	case <-r.Request.Context().Done():
		return r.Request.Context().Err()
	}
	r.Body = io.NopCloser(bytes.NewReader(raw))
	intent, ok := r.Request.Context().Value(reviewWebIntentKey{}).(reviewWebIntent)
	if !ok {
		if strings.HasSuffix(r.Request.URL.Path, "/task-transition-commands/lookup") {
			found, err := wc.DecodeTaskTransitionLookup(raw)
			w.mu.Lock()
			matches := err == nil && r.StatusCode == 200 && found.Status == wc.LookupCommitted && found.Receipt != nil && len(w.receipts) == 1 && reflect.DeepEqual(*found.Receipt, w.receipts[0].receipt)
			w.mu.Unlock()
			if !matches {
				return errors.New("Lookup did not return the complete original committed receipt")
			}
		}
		if r.Request.URL.Path == "/api/v1/sessions/logout" && (r.StatusCode != http.StatusNoContent || len(raw) != 0) {
			return errors.New("original Account logout did not complete")
		}
		return nil
	}
	if r.StatusCode != 200 {
		return errors.New("browser Transfer did not commit")
	}
	requestID, err := f.ParseID[f.Request](id)
	if err != nil {
		return err
	}
	meta := f.CommandMeta{RequestID: requestID, IdempotencyKey: intent.key, ExpectedVersion: &intent.before.Version}
	digest, err := wc.TaskTransferDigest(w.x.domain.base.ownerBrowser.actor, meta, intent.before.ProjectID, intent.before.ID, intent.request)
	if err != nil {
		return err
	}
	lookup := wc.TaskTransitionLookupRequest{ProjectID: intent.before.ProjectID, Command: wc.TaskTransitionTransfer, IdempotencyKey: intent.key, SemanticDigest: digest}
	receipt, err := wc.DecodeTaskTransitionMutation(raw)
	if err != nil {
		return err
	}
	stored, err := w.x.committedFacts(r.Request.Context(), w.x.domain.base.raw, intent.before, intent.request, meta, lookup, intent.generations, nil)
	if err != nil || !reflect.DeepEqual(stored, receipt) {
		return errors.New("browser receipt disagrees with physical Task/history/rank/Outbox")
	}
	w.mu.Lock()
	w.receipts = append(w.receipts, reviewWebReceipt{intent, receipt})
	w.mu.Unlock()
	if w.mode != "review-complete" {
		return &reviewWebDrop{r.Header.Clone(), len(raw)}
	}
	return nil
}

func (w *taskReviewWeb) proxyError(out http.ResponseWriter, r *http.Request, err error) {
	var drop *reviewWebDrop
	if !errors.As(err, &drop) {
		w.fail(err)
		http.Error(out, "owned proxy failed", 502)
		return
	}
	for key, values := range drop.header {
		out.Header()[key] = append([]string(nil), values...)
	}
	out.Header().Set("Content-Length", fmt.Sprint(drop.length))
	out.Header().Set("Connection", "close")
	out.WriteHeader(200)
	n, writeErr := io.WriteString(out, "{")
	flushErr := http.NewResponseController(out).Flush()
	conn, _, hijackErr := http.NewResponseController(out).Hijack()
	var closeErr error
	if conn != nil {
		closeErr = conn.Close()
	}
	if n != 1 || writeErr != nil || flushErr != nil || hijackErr != nil || closeErr != nil {
		w.fail(errors.New("original committed response cut failed"))
		return
	}
	w.mu.Lock()
	w.cut++
	w.mu.Unlock()
}

func (w *taskReviewWeb) close(t *testing.T) {
	t.Helper()
	if w.closed {
		return
	}
	w.closed = true
	if w.front != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		err := w.front.Shutdown(ctx)
		cancel()
		if err != nil {
			_ = w.front.Close()
			t.Error("front HTTP shutdown required forced close", err)
		}
		if serveErr := <-w.serve; !errors.Is(serveErr, http.ErrServerClosed) {
			t.Error("original front Serve returned unexpectedly", serveErr)
		}
		w.frontWG.Wait()
	}
	if w.transport != nil {
		w.transport.CloseIdleConnections()
	}
	if w.backend != nil {
		w.backend.Close()
		w.backendWG.Wait()
	}
}

func (w *taskReviewWeb) browser(t *testing.T) {
	t.Helper()
	root, err := filepath.Abs("../account-captcha-web")
	firstRoundRequire(t, err)
	ctx, cancel := context.WithTimeout(ctxFor(t), 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "node", filepath.Join(root, "node_modules/@playwright/test/cli.js"), "test", "--config", filepath.Join(root, "task-review.config.js"))
	cmd.Dir = root
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.WaitDelay = time.Second
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if key != "TMPDIR" && key != "DEBUG" && key != "PWDEBUG" && key != "PLAYWRIGHT_NO_COPY_PROMPT" && !strings.HasPrefix(key, "AGENTEAM_") {
			cmd.Env = append(cmd.Env, entry)
		}
	}
	cmd.Env = append(cmd.Env, "TMPDIR="+w.directory, "PLAYWRIGHT_NO_COPY_PROMPT=1", "AGENTEAM_AUTH_WEB_ORIGIN="+w.origin, "AGENTEAM_AUTH_WEB_PRIVATE="+w.directory, "AGENTEAM_AUTH_WEB_CHROMIUM=/usr/bin/chromium", "AGENTEAM_TASK_REVIEW_WEB_CASE="+w.mode, "AGENTEAM_TASK_REVIEW_WEB_EVIDENCE="+w.evidence, "AGENTEAM_TASK_REVIEW_WEB_INPUT_HASH="+w.inputHash)
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	if err := cmd.Start(); err != nil {
		t.Fatal("owned Playwright did not start")
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
	t.Logf("Task Review Node actual_wait pid=%d success=%t", cmd.Process.Pid, err == nil)
	safe := output.String()
	w.mu.Lock()
	tokens := append([]string(nil), w.keys...)
	w.mu.Unlock()
	tokens = append(tokens, w.x.domain.base.ownerBrowser.cookie, w.x.domain.base.ownerBrowser.csrf)
	for _, token := range tokens {
		if token != "" {
			safe = strings.ReplaceAll(safe, token, "[redacted]")
		}
	}
	t.Log(safe)
	if err != nil {
		t.Fatal("original Playwright child failed and was waited")
	}
}

func (w *taskReviewWeb) verify(t *testing.T) {
	t.Helper()
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.failure != nil {
		t.Fatal("owned HTTP observation failed", w.failure)
	}
	want := 2
	if w.mode != "review-complete" {
		want = 1
	}
	if w.transfers != want || len(w.receipts) != want {
		t.Fatal("browser transition count differs from real receipts")
	}
	for _, tail := range w.tails {
		if !tail.returned {
			t.Fatal("original HTTP handler did not return")
		}
	}
	last := w.receipts[len(w.receipts)-1]
	if w.mode == "review-complete" {
		if w.receipts[0].intent.request.TargetState != wc.TaskStateInReview || w.receipts[0].receipt.Task.AssigneeAgentID == nil || *w.receipts[0].receipt.Task.AssigneeAgentID != w.x.reviewer || last.receipt.Task.State != wc.TaskStateDone || last.intent.request.AssigneeAgentID != nil || w.lookups != 0 || w.cut != 0 {
			t.Fatal("browser review/completion changed intended semantics")
		}
	} else if last.receipt.Task.State != wc.TaskStateTodo || last.receipt.Task.AssigneeAgentID == nil || *last.receipt.Task.AssigneeAgentID != w.x.domain.agentID || w.lookups != 1 || w.logouts != 1 || w.cut != 1 {
		t.Fatal("browser recovery did not Lookup exactly its original rework and revoke Session")
	}
	// Persisted current Task remains the verified last receipt even after logout.
	var state, assignee string
	var version int64
	err := w.x.domain.base.raw.QueryRow(ctxFor(t), `SELECT state,assignee_agent_id::text,version FROM agenteam_work.tasks WHERE project_id=$1::text::uuid AND id=$2::text::uuid`, last.receipt.Task.ProjectID.String(), last.receipt.Task.ID.String()).Scan(&state, &assignee, &version)
	if err != nil || state != string(last.receipt.Task.State) || assignee != last.receipt.Task.AssigneeAgentID.String() || version != int64(last.receipt.Task.Version) {
		t.Fatal("final browser Task differs from persisted original receipt", err)
	}
}
