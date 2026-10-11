//go:build integration

package model_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
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
	"regexp"
	"sort"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	uc "github.com/LunaDeerTech/agenteam/internal/central/usage/contract"
)

const projectUsageWebMode = "paginated-current-read"

// The missing Runtime producer remains the original strict, persisted Facts
// fixture. Each seed uses its real owned wire and the formal Usage Writer. No
// fixture Facts, handler or initializer is injected into public app.Run.
func TestProjectUsageWeb(t *testing.T) {
	t.Run(projectUsageWebMode, projectUsageWeb)
}

func projectUsageWeb(t *testing.T) {
	started := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	t.Cleanup(func() { cancel(); projectUsageComplete(t, started) })
	dist, runtime := os.Getenv("AGENTEAM_PROJECT_USAGE_WEB_DIST"), os.Getenv("AGENTEAM_AUTH_WEB_RUNTIME")
	evidence, inputHash := os.Getenv("AGENTEAM_PROJECT_USAGE_WEB_EVIDENCE"), os.Getenv("AGENTEAM_PROJECT_USAGE_WEB_INPUT_HASH")
	digest, err := hex.DecodeString(inputHash)
	if !filepath.IsAbs(dist) || !filepath.IsAbs(runtime) || len(runtime) > 45 || !filepath.IsAbs(evidence) || err != nil || len(digest) != sha256.Size {
		t.Fatal("owned Usage dist/runtime/evidence/input required")
	}
	info, err := os.Lstat(filepath.Join(dist, "index.html"))
	if err != nil || !info.Mode().IsRegular() {
		t.Fatal("regular frozen Usage assets required")
	}
	evidence = filepath.Join(evidence, t.Name())
	projectUsageWebRequire(t, os.MkdirAll(filepath.Dir(evidence), 0700))
	projectUsageWebRequire(t, os.Mkdir(evidence, 0700))
	private, err := os.MkdirTemp(runtime, "puw-")
	projectUsageWebRequire(t, err)
	t.Cleanup(func() {
		projectUsageWebRequire(t, os.RemoveAll(private))
		if _, e := os.Lstat(private); !errors.Is(e, os.ErrNotExist) {
			t.Error("owned Usage private material remains")
		}
	})
	v := newProjectUsageHTTPFixture(t)
	var rows []map[string]any
	for i := 0; i < 51; i++ {
		if ctx.Err() != nil {
			t.Fatal("Usage preparation exceeded original budget")
		}
		var usage json.RawMessage
		switch i % 4 {
		case 0:
			usage = json.RawMessage(`{"prompt_tokens":9007199254740993,"completion_tokens":0,"total_tokens":9007199254740993}`)
		case 1:
			usage = json.RawMessage(`{"prompt_tokens":0,"completion_tokens":0,"total_tokens":0}`)
		case 2:
			usage = json.RawMessage(`{"prompt_tokens":9223372036854775807,"completion_tokens":0,"total_tokens":9223372036854775807}`)
		}
		seed := v.success(t, usage, nil)
		if seed.exchange == nil || !seed.exchange.Joined() || !seed.event.Joined || seed.event.Value.Final == nil || seed.event.Value.Final.Status != uc.Succeeded {
			t.Fatal("formal seed lacks the original wire/Writer terminal")
		}
		rows = append(rows, projectUsageWebDTO(t, seed.event.Value))
	}
	sort.Slice(rows, func(i, j int) bool {
		a, _ := time.Parse(time.RFC3339Nano, rows[i]["started_at"].(string))
		b, _ := time.Parse(time.RFC3339Nano, rows[j]["started_at"].(string))
		if !a.Equal(b) {
			return a.After(b)
		}
		return rows[i]["id"].(string) > rows[j]["id"].(string)
	})
	before := usageLedgerState(t, v.usageFixture)
	root := startProjectUsageRoot(t, v)
	// Default root setup is the previously accepted public root. It has no
	// observer hook; original middleware logs, rather than an injected handler,
	// supply its request-return observation below.
	w := &projectUsageWebObserver{root: root, path: projectUsagePath(v.project.ID), project: v.project.ID.String(), evidence: evidence, inputHash: inputHash, rows: rows, ids: map[string]bool{}}
	backend, err := url.Parse("http://" + root.address)
	projectUsageWebRequire(t, err)
	proxy := httputil.NewSingleHostReverseProxy(backend)
	proxy.Transport = root.client.Transport
	proxy.ErrorLog = log.New(io.Discard, "", 0)
	proxy.ErrorHandler = func(rw http.ResponseWriter, _ *http.Request, _ error) {
		w.fail()
		http.Error(rw, "Owned Usage request failed", http.StatusBadGateway)
	}
	director := proxy.Director
	proxy.Director = func(r *http.Request) {
		director(r)
		r.Host = "system-http.example.test"
		// Only this owned GET bridge maps the browser's test origin to the
		// already frozen PUBLIC_ORIGIN of startProjectUsageRoot.
		if r.Header.Get("Origin") != "" {
			r.Header.Set("Origin", systemHTTPOrigin)
		}
		r.Header.Set("Accept-Encoding", "identity")
	}
	proxy.ModifyResponse = w.capture
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		if r.Host != w.host || r.Method != http.MethodGet || r.ContentLength > 0 || len(r.TransferEncoding) != 0 || r.Header.Get("Transfer-Encoding") != "" {
			w.fail()
			http.Error(rw, "undeclared browser request", http.StatusBadRequest)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/api/") {
			if !w.allowed(r.URL.Path) {
				w.fail()
				http.Error(rw, "undeclared API", http.StatusBadRequest)
				return
			}
			w.active.Add(1)
			defer w.active.Done()
			proxy.ServeHTTP(rw, r.WithContext(context.WithValue(r.Context(), projectUsageWebIngress{}, true)))
			return
		}
		if strings.HasPrefix(r.URL.Path, "/assets/") {
			clean := filepath.Clean(strings.TrimPrefix(r.URL.Path, "/"))
			if !strings.HasPrefix(clean, "assets"+string(filepath.Separator)) {
				http.NotFound(rw, r)
				return
			}
			http.ServeFile(rw, r, filepath.Join(dist, clean))
			return
		}
		http.ServeFile(rw, r, filepath.Join(dist, "index.html"))
	}))
	server.Config.ErrorLog = log.New(io.Discard, "", 0)
	server.Config.ReadHeaderTimeout = 5 * time.Second
	server.Config.BaseContext = func(net.Listener) context.Context { return ctx }
	w.host = server.Listener.Addr().String()
	server.StartTLS()
	public, err := url.Parse(server.URL)
	projectUsageWebRequire(t, err)
	if public.Host != w.host {
		t.Fatal("owned TLS listener identity changed")
	}
	var closeOnce sync.Once
	closeProxy := func() { closeOnce.Do(func() { server.Close(); w.active.Wait(); root.client.CloseIdleConnections() }) }
	t.Cleanup(closeProxy)
	route := "/" + v.ownerName + "/" + v.project.NormalizedName + "/settings/model-usage"
	material := projectUsageWebJSON(t, map[string]any{"mode": projectUsageWebMode, "origin": server.URL, "cookie": v.ownerBrowser.cookie, "project_id": v.project.ID, "route": route, "items": rows, "input_hash": inputHash})
	projectUsageWebRequire(t, os.WriteFile(filepath.Join(private, "project-usage-material.json"), material, 0600))
	clear(material)
	result := projectUsageWebBrowser(t, ctx, private, server.URL, dist, evidence, inputHash)
	closeProxy()
	w.mu.Lock()
	count, lists, failed := w.count, w.lists, w.failed
	w.mu.Unlock()
	if result["completed"] != true || result["all_observed"] != true || result["lookups"] != float64(0) || result["lists"] != float64(5) || result["original_requests"] != float64(count) || lists != 5 || failed {
		t.Fatal("Usage browser did not complete all five original reads")
	}
	if usageLedgerState(t, v.usageFixture) != before {
		t.Fatal("browser read changed the canonical Usage ledger/receipts/summaries")
	}
	root.stop(t)
	if !strings.Contains(root.logs.String(), `"outcome":"drained"`) {
		t.Fatal("public default root did not actually drain")
	}
	for _, secret := range []string{v.ownerBrowser.cookie, v.ownerBrowser.csrf} {
		if strings.Contains(root.logs.String(), secret) {
			t.Fatal("root logged private browser material")
		}
	}
	t.Log("Usage browser: five current public app.Run reads, formal 51-row Writer preparation, immutable ledger, original Node Wait and root/proxy return; controlled Facts are not a production Runtime producer")
}

func projectUsageWebRequire(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		// Deliberately omit private paths, URLs and credential-bearing errors.
		t.Fatal("owned Usage fixture operation failed")
	}
}

func projectUsageWebJSON(t *testing.T, value any) []byte {
	t.Helper()
	raw, err := json.Marshal(value)
	projectUsageWebRequire(t, err)
	return raw
}

// The independent expected DTO comes from the seed's original canonical event,
// not from the HTTP representation under test. Internal execution/fence/input
// and Secret authority material never enter the browser's material file.
func projectUsageWebDTO(t *testing.T, row uc.Invocation) map[string]any {
	t.Helper()
	nullable := func(s string) any {
		if s == "" {
			return nil
		}
		return s
	}
	value := map[string]any{
		"id": row.ID, "call_id": row.CallID, "attempt_index": row.AttemptIndex, "project_id": row.Consumer.ProjectID,
		"consumer_kind": row.Consumer.Kind, "purpose": row.Consumer.Purpose, "agent_id": row.Consumer.AgentID, "execution_id": row.Consumer.ExecutionID, "meeting_id": nullable(row.Consumer.MeetingID),
		"provider_id": row.Identity.ProviderID, "model_id": row.Identity.ModelID, "provider_name": row.Identity.ProviderName, "model_name": row.Identity.ModelName, "provider_model_id": row.Identity.ProviderModelID, "protocol": row.Identity.Protocol, "model_type": row.Identity.ModelType,
		"live_provider_id": row.LiveProviderID, "live_model_id": row.LiveModelID, "dispatch": row.Dispatch, "started_at": row.StartedAt, "dispatched_at": row.DispatchedAt,
		"final":               map[string]any{"status": row.Final.Status, "finished_at": row.Final.FinishedAt, "error_category": nil},
		"usage":               map[string]any{"input_tokens": row.Usage.InputTokens, "output_tokens": row.Usage.OutputTokens, "total_tokens": row.Usage.TotalTokens, "cached_input_tokens": row.Usage.CachedInputTokens, "cache_write_tokens": row.Usage.CacheWriteTokens, "reasoning_tokens": row.Usage.ReasoningTokens, "source": row.Usage.Source},
		"provider_request_id": nullable(row.ProviderRequestID),
	}
	var result map[string]any
	projectUsageWebRequire(t, json.Unmarshal(projectUsageWebJSON(t, value), &result))
	if len(result) != 24 || len(result["usage"].(map[string]any)) != 7 || len(result["final"].(map[string]any)) != 3 {
		t.Fatal("safe Usage seed DTO lost its closed shape")
	}
	return result
}

type projectUsageWebIngress struct{}
type projectUsageWebObserver struct {
	mu                                       sync.Mutex
	active                                   sync.WaitGroup
	root                                     *projectUsageRoot
	host, path, project, evidence, inputHash string
	rows                                     []map[string]any
	ids                                      map[string]bool
	count, lists                             int
	failed                                   bool
	from, to, cursor                         string
}

func (w *projectUsageWebObserver) fail() error {
	w.mu.Lock()
	w.failed = true
	w.mu.Unlock()
	return errors.New("original Usage read proof incomplete")
}

func (w *projectUsageWebObserver) allowed(path string) bool {
	return path == w.path || path == "/api/v1/session" || path == "/api/v1/projects" || path == "/api/v1/projects/resolve" || path == "/api/v1/projects/"+w.project
}

// httpapi.WithRequestID emits this original log only after its next.ServeHTTP
// returns. This observes that business handler return, not the outer middleware
// stack or App-internal Host (which the production log intentionally omits).
func (w *projectUsageWebObserver) returned(ctx context.Context, rid, method string, status, length int) error {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	tick := time.NewTicker(5 * time.Millisecond)
	defer tick.Stop()
	for {
		matches := 0
		for _, line := range strings.Split(w.root.logs.String(), "\n") {
			var row struct {
				Event  string `json:"event"`
				ID     string `json:"request_id"`
				Method string `json:"method"`
				Status int    `json:"status"`
				Bytes  int    `json:"bytes"`
			}
			if json.Unmarshal([]byte(line), &row) != nil || row.Event != "http_request" || row.ID != rid {
				continue
			}
			if row.Method != method || row.Status != status || row.Bytes != length {
				return errors.New("App request-return fields differ")
			}
			matches++
		}
		if matches == 1 {
			return nil
		}
		if matches > 1 {
			return errors.New("App request identity repeated")
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-tick.C:
		}
	}
}

func (w *projectUsageWebObserver) capture(r *http.Response) error {
	raw, readErr := io.ReadAll(io.LimitReader(r.Body, (1<<20)+1))
	closeErr := r.Body.Close()
	if readErr != nil || closeErr != nil || len(raw) > 1<<20 || r.StatusCode != 200 || r.ContentLength != int64(len(raw)) || r.Header.Get("Content-Encoding") != "" || r.Header.Get("Cache-Control") != "no-store" {
		return w.fail()
	}
	rid := r.Header.Get("X-Request-ID")
	id, err := f.ParseID[f.Request](rid)
	if err != nil || id.String() != rid || len(r.Header.Values("X-Request-ID")) != 1 || r.Request.Context().Value(projectUsageWebIngress{}) != true || r.Request.Host != "system-http.example.test" || r.Request.URL.Host != w.root.address || r.Request.URL.Scheme != "http" {
		return w.fail()
	}
	if w.returned(r.Request.Context(), rid, r.Request.Method, r.StatusCode, len(raw)) != nil {
		return w.fail()
	}
	w.mu.Lock()
	if w.ids[rid] {
		w.mu.Unlock()
		return w.fail()
	}
	w.ids[rid] = true
	if r.Request.URL.Path == w.path {
		err = w.page(r.Request.URL, raw)
	}
	w.mu.Unlock()
	if err != nil {
		return w.fail()
	}
	proof := map[string]any{"input_hash": w.inputHash, "request_id": rid, "method": r.Request.Method, "url_sha256": fmt.Sprintf("%x", sha256.Sum256([]byte(r.Request.URL.RequestURI()))), "status": r.StatusCode, "content_length": len(raw), "body_sha256": fmt.Sprintf("%x", sha256.Sum256(raw)), "original_eof": true, "original_closed": true, "handler_returned": true, "handler_return_source": "app_http_request_log", "browser_ingress_host_matches": true, "downstream_host_matches": true}
	encoded, err := json.Marshal(proof)
	if err != nil || os.WriteFile(filepath.Join(w.evidence, "project-usage-"+projectUsageWebMode+"-response-"+rid+".json"), encoded, 0600) != nil {
		return w.fail()
	}
	w.mu.Lock()
	w.count++
	w.mu.Unlock()
	r.Body = io.NopCloser(bytes.NewReader(raw))
	return nil
}

// Called with mu held, after full original EOF/Close and the App's own return
// observation. No cursor or page is published from an incomplete response.
func (w *projectUsageWebObserver) page(u *url.URL, raw []byte) error {
	bad := errors.New("Usage current page/window differs from original facts")
	q, err := url.ParseQuery(u.RawQuery)
	if err != nil || q.Get("limit") != "50" || len(q["limit"]) != 1 || len(q["from"]) != 1 || len(q["to"]) != 1 {
		return bad
	}
	for k, values := range q {
		if len(values) != 1 || k != "limit" && k != "from" && k != "to" && k != "cursor" {
			return bad
		}
	}
	from, e1 := time.Parse(time.RFC3339Nano, q.Get("from"))
	to, e2 := time.Parse(time.RFC3339Nano, q.Get("to"))
	if e1 != nil || e2 != nil || to.Sub(from) != 30*24*time.Hour || !strings.HasSuffix(q.Get("from"), "Z") || !strings.HasSuffix(q.Get("to"), "Z") || to.After(time.Now().Add(time.Second)) {
		return bad
	}
	var page struct {
		Items []map[string]any `json:"items"`
		Next  *string          `json:"next_cursor"`
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &page) != nil || json.Unmarshal(raw, &fields) != nil || len(fields) != 2 || fields["items"] == nil || fields["next_cursor"] == nil {
		return bad
	}
	index := w.lists
	if index > 4 {
		return bad
	}
	continuation := index == 1 || index == 3
	if index == 0 || index == 4 {
		if _, present := q["cursor"]; present {
			return bad
		}
		if index == 4 {
			old, e := time.Parse(time.RFC3339Nano, w.to)
			if e != nil || !to.After(old) {
				return bad
			}
		}
		w.from, w.to = q.Get("from"), q.Get("to")
	} else if q.Get("from") != w.from || q.Get("to") != w.to {
		return bad
	}
	want := w.rows[:50]
	if continuation {
		if w.cursor == "" || q.Get("cursor") != w.cursor || len(q["cursor"]) != 1 || page.Next != nil {
			return bad
		}
		want = w.rows[50:]
	} else {
		if _, present := q["cursor"]; present {
			return bad
		}
		if page.Next == nil || *page.Next == "" {
			return bad
		}
	}
	if !reflect.DeepEqual(page.Items, want) {
		return bad
	}
	for _, item := range page.Items {
		at, e := time.Parse(time.RFC3339Nano, item["started_at"].(string))
		if e != nil || at.Before(from) || !at.Before(to) {
			return bad
		}
	}
	w.cursor = ""
	if page.Next != nil {
		w.cursor = *page.Next
	}
	w.lists++
	return nil
}

func projectUsageWebBrowser(t *testing.T, parent context.Context, private, origin, dist, evidence, inputHash string) map[string]any {
	t.Helper()
	harness, err := filepath.Abs("../account-captcha-web")
	projectUsageWebRequire(t, err)
	ctx, cancel := context.WithTimeout(parent, 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "node", filepath.Join(harness, "node_modules/@playwright/test/cli.js"), "test", "--config", filepath.Join(harness, "project-usage.config.js"))
	cmd.Dir = harness
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = time.Second
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if key != "TMPDIR" && key != "DEBUG" && key != "PWDEBUG" && key != "PLAYWRIGHT_NO_COPY_PROMPT" && !strings.HasPrefix(key, "AGENTEAM_") {
			cmd.Env = append(cmd.Env, entry)
		}
	}
	cmd.Env = append(cmd.Env, "TMPDIR="+private, "PLAYWRIGHT_NO_COPY_PROMPT=1", "AGENTEAM_AUTH_WEB_ORIGIN="+origin, "AGENTEAM_AUTH_WEB_PRIVATE="+private, "AGENTEAM_AUTH_WEB_CHROMIUM=/usr/bin/chromium", "AGENTEAM_PROJECT_USAGE_WEB_DIST="+dist, "AGENTEAM_PROJECT_USAGE_WEB_EVIDENCE="+evidence, "AGENTEAM_PROJECT_USAGE_WEB_INPUT_HASH="+inputHash, "AGENTEAM_PROJECT_USAGE_WEB_CASE="+projectUsageWebMode)
	var output bytes.Buffer
	cmd.Stdout, cmd.Stderr = &output, &output
	if cmd.Start() != nil {
		t.Fatal("owned Usage Node did not start")
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
	t.Logf("ProjectUsage Node actual_wait pid=%d success=%t", cmd.Process.Pid, err == nil)
	if err != nil || !regexp.MustCompile(`(?m)\b1 passed\b`).Match(output.Bytes()) {
		t.Fatal("Usage locked browser did not pass its one case")
	}
	raw, err := os.ReadFile(filepath.Join(private, "project-usage-result.json"))
	if err != nil || len(raw) > 16<<10 {
		t.Fatal("Usage browser result unavailable")
	}
	var result map[string]any
	if json.Unmarshal(raw, &result) != nil || result["input_hash"] != inputHash {
		t.Fatal("Usage result not bound to original inputs")
	}
	return result
}
