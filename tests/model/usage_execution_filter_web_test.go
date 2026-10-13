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
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	wire "github.com/LunaDeerTech/agenteam/internal/central/model/adapter"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	uc "github.com/LunaDeerTech/agenteam/internal/central/usage/contract"
)

const usageExecutionMode = "execution-filter-current-read"

// Preparation uses the existing persisted Runtime Facts, actual owned wire and
// formal Usage Writer. Execution IDs are canonical consumer/input references in
// those Facts, not a claim that a production Execution/Runtime was launched.
// Only durable rows precede the unchanged public default app.Run root.
func TestUsageExecutionFilterWeb(t *testing.T) {
	t.Run(usageExecutionMode, func(t *testing.T) {
		started := time.Now()
		ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
		t.Cleanup(func() { cancel(); projectUsageComplete(t, started) })
		dist, runtime := os.Getenv("AGENTEAM_USAGE_EXECUTION_FILTER_WEB_DIST"), os.Getenv("AGENTEAM_AUTH_WEB_RUNTIME")
		evidence, input := os.Getenv("AGENTEAM_USAGE_EXECUTION_FILTER_WEB_EVIDENCE"), os.Getenv("AGENTEAM_USAGE_EXECUTION_FILTER_WEB_INPUT_HASH")
		digest, err := hex.DecodeString(input)
		if !filepath.IsAbs(dist) || !filepath.IsAbs(runtime) || len(runtime) > 45 || !filepath.IsAbs(evidence) || err != nil || len(digest) != sha256.Size {
			t.Fatal("owned execution-filter browser inputs required")
		}
		info, err := os.Lstat(filepath.Join(dist, "index.html"))
		if err != nil || !info.Mode().IsRegular() {
			t.Fatal("frozen execution-filter assets missing")
		}
		projectUsageWebRequire(t, os.MkdirAll(evidence, 0700))
		private, err := os.MkdirTemp(runtime, "uef-")
		projectUsageWebRequire(t, err)
		t.Cleanup(func() {
			projectUsageWebRequire(t, os.RemoveAll(private))
			if _, err := os.Lstat(private); !errors.Is(err, os.ErrNotExist) {
				t.Error("execution-filter private directory remains")
			}
		})
		v := newProjectUsageHTTPFixture(t)
		other := v.createProject(t, v.owner)
		boundary := usageNow(t, v.usageFixture).Time().UTC().Truncate(24 * time.Hour).Add(-24 * time.Hour)
		known := json.RawMessage(`{"prompt_tokens":7,"completion_tokens":3,"total_tokens":10,"prompt_tokens_details":{"cached_tokens":2,"cache_write_tokens":1},"completion_tokens_details":{"reasoning_tokens":1}}`)
		control := json.RawMessage(`{"prompt_tokens":99,"completion_tokens":1,"total_tokens":100,"prompt_tokens_details":{"cached_tokens":0,"cache_write_tokens":0},"completion_tokens_details":{"reasoning_tokens":0}}`)
		zero := json.RawMessage(`{"prompt_tokens":0,"completion_tokens":0,"total_tokens":0,"prompt_tokens_details":{"cached_tokens":0,"cache_write_tokens":0},"completion_tokens_details":{"reasoning_tokens":0}}`)
		complete := func(c *usageCase, at time.Time) map[string]any {
			if ctx.Err() != nil {
				t.Fatal("execution-filter preparation exhausted original budget")
			}
			// Supply controlled event time before Reserve; the actual wire runs now.
			c.event.Value.StartedAt, err = f.NewInstant(at)
			projectUsageWebRequire(t, err)
			v.reserve(t, c)
			v.authorize(t, c)
			v.startWire(t, c, wire.JSONResponse)
			_, err = c.exchange.Result(ctx)
			projectUsageWebRequire(t, err)
			v.observeWire(t, c)
			v.finishWire(t, c, uc.Succeeded, nil)
			if !c.exchange.Joined() || !c.event.Joined || c.event.Value.Final == nil || c.event.Value.Final.Status != uc.Succeeded {
				t.Fatal("original wire/Writer terminal missing")
			}
			return projectUsageWebDTO(t, c.event.Value)
		}
		scenario := wireJSON(wireReply("execution-filter", "stop", known, nil))
		first := v.newCase(t, &scenario, nil)
		execution := *first.call.Consumer.ExecutionID
		target := []map[string]any{complete(first, boundary.Add(-25*time.Second))}
		for n := 1; n < 51; n++ {
			c := v.nextExecutionCall(t, first)
			if c.call.Consumer.ExecutionID == nil || *c.call.Consumer.ExecutionID != execution || c.call.ID == first.call.ID {
				t.Fatal("same Execution preparation changed canonical consumer/call")
			}
			target = append(target, complete(c, boundary.Add(time.Duration(n-25)*time.Second)))
		}
		extraScenario := wireJSON(wireReply("other-execution", "stop", control, nil))
		extra := v.newCase(t, &extraScenario, nil)
		if extra.call.Consumer.ExecutionID == nil || *extra.call.Consumer.ExecutionID == execution {
			t.Fatal("same-Project different Execution control absent")
		}
		extraRow := complete(extra, boundary.Add(-100*time.Second))
		toolScenario := wireJSON(wireReply("no-execution", "stop", zero, nil))
		tool := mc.Consumer{Kind: mc.ToolConsumer, ProjectID: v.project.ID, Purpose: mc.ApprovalAuto, OperationID: newID[struct{}](t).String()}
		noExecution := v.newCase(t, &toolScenario, &tool)
		nullRow := complete(noExecution, boundary.Add(-200*time.Second))
		foreignAgent, foreignExecution := newID[id.Agent](t), newID[id.Execution](t)
		foreignConsumer := mc.Consumer{Kind: mc.AgentConsumer, ProjectID: other.ID, Purpose: mc.AgentGeneration, AgentID: &foreignAgent, ExecutionID: &foreignExecution}
		foreignCase := v.newCase(t, &extraScenario, &foreignConsumer)
		foreign := complete(foreignCase, boundary.Add(time.Second))
		rows := append(append([]map[string]any{}, target...), extraRow, nullRow)
		for _, list := range [][]map[string]any{target, rows} {
			sort.Slice(list, func(i, j int) bool {
				a, b := list[i]["started_at"].(string), list[j]["started_at"].(string)
				return a > b || a == b && list[i]["id"].(string) > list[j]["id"].(string)
			})
		}
		earlier, later := boundary.Add(-time.Second).Format("2006-01-02"), boundary.Format("2006-01-02")
		expectedTarget := []map[string]any{usageSummaryExpected(earlier, 25, 25, []int64{175, 75, 250, 50, 25, 25}), usageSummaryExpected(later, 26, 26, []int64{182, 78, 260, 52, 26, 26})}
		expectedAll := []map[string]any{usageSummaryExpected(earlier, 27, 27, []int64{274, 76, 350, 50, 25, 25}), usageSummaryExpected(later, 26, 26, []int64{182, 78, 260, 52, 26, 26})}
		before := usageLedgerState(t, v.usageFixture)
		root := startProjectUsageRoot(t, v)
		w := &usageExecutionObserver{projectUsageWebObserver: &projectUsageWebObserver{root: root, path: projectUsagePath(v.project.ID), project: v.project.ID.String(), evidence: evidence, inputHash: input, ids: map[string]bool{}}, execution: execution.String(), rows: rows, target: target, expectedAll: expectedAll, expectedTarget: expectedTarget}
		backend, err := url.Parse("http://" + root.address)
		projectUsageWebRequire(t, err)
		proxy := httputil.NewSingleHostReverseProxy(backend)
		proxy.Transport = root.client.Transport
		proxy.ErrorLog = log.New(io.Discard, "", 0)
		director := proxy.Director
		proxy.Director = func(r *http.Request) {
			director(r)
			r.Host = "system-http.example.test"
			if r.Header.Get("Origin") != "" {
				r.Header.Set("Origin", systemHTTPOrigin)
			}
			r.Header.Set("Accept-Encoding", "identity")
		}
		proxy.ModifyResponse = w.capture
		proxy.ErrorHandler = func(out http.ResponseWriter, _ *http.Request, _ error) {
			_ = w.fail()
			http.Error(out, "owned execution-filter response failed", 502)
		}
		server := httptest.NewUnstartedServer(http.HandlerFunc(func(out http.ResponseWriter, r *http.Request) {
			if r.Host != w.host || r.Method != http.MethodGet || r.ContentLength > 0 || len(r.TransferEncoding) != 0 || r.Header.Get("Transfer-Encoding") != "" {
				_ = w.fail()
				http.Error(out, "undeclared request", 400)
				return
			}
			if strings.HasPrefix(r.URL.Path, "/api/") {
				if !w.allowed(r.URL.Path) {
					_ = w.fail()
					http.Error(out, "undeclared API", 400)
					return
				}
				w.active.Add(1)
				defer w.active.Done()
				proxy.ServeHTTP(out, r.WithContext(context.WithValue(r.Context(), projectUsageWebIngress{}, true)))
				return
			}
			if strings.HasPrefix(r.URL.Path, "/assets/") {
				clean := filepath.Clean(strings.TrimPrefix(r.URL.Path, "/"))
				if !strings.HasPrefix(clean, "assets"+string(filepath.Separator)) {
					http.NotFound(out, r)
					return
				}
				http.ServeFile(out, r, filepath.Join(dist, clean))
				return
			}
			http.ServeFile(out, r, filepath.Join(dist, "index.html"))
		}))
		server.Config.ErrorLog = log.New(io.Discard, "", 0)
		server.Config.ReadHeaderTimeout = 5 * time.Second
		server.Config.BaseContext = func(net.Listener) context.Context { return ctx }
		w.host = server.Listener.Addr().String()
		server.StartTLS()
		var once sync.Once
		closeProxy := func() { once.Do(func() { server.Close(); w.active.Wait(); root.client.CloseIdleConnections() }) }
		t.Cleanup(closeProxy)
		material := map[string]any{"mode": usageExecutionMode, "origin": server.URL, "cookie": v.ownerBrowser.cookie, "input_hash": input, "project_id": v.project.ID, "execution_id": execution, "other_execution_id": extra.call.Consumer.ExecutionID, "other_project_id": other.ID, "foreign": foreign, "items": rows, "target_items": target, "target_summary": expectedTarget, "all_summary": expectedAll, "route": "/" + v.ownerName + "/" + v.project.NormalizedName + "/settings/model-usage"}
		raw := projectUsageWebJSON(t, material)
		projectUsageWebRequire(t, os.WriteFile(filepath.Join(private, "usage-execution-filter-material.json"), raw, 0600))
		clear(raw)
		result := usageExecutionBrowser(t, ctx, private, server.URL, dist, evidence, input, v.ownerBrowser.cookie, v.ownerBrowser.csrf)
		closeProxy()
		w.mu.Lock()
		count, reads, failed := w.count, w.reads, w.failed
		w.mu.Unlock()
		if failed || reads != 13 || result["completed"] != true || result["all_observed"] != true || result["reads"] != float64(13) || result["lists"] != float64(10) || result["summaries"] != float64(3) || result["original_requests"] != float64(count) || result["writes"] != float64(0) {
			t.Fatal("execution filter did not join all original scoped reads")
		}
		if usageLedgerState(t, v.usageFixture) != before {
			t.Fatal("execution filter altered original A/B ledger, receipts or execution summaries")
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
		t.Log("UsageExecutionFilter: public default app.Run; 51 same-Execution + different-Execution/null/B controls; 10 original lists + 3 complete daily summaries; immutable ledger; no external Provider or production Runtime claim")
	})
}

type usageExecutionObserver struct {
	*projectUsageWebObserver
	execution                   string
	rows, target                []map[string]any
	expectedAll, expectedTarget []map[string]any
	reads                       int
}

func (w *usageExecutionObserver) allowed(path string) bool {
	return w.projectUsageWebObserver.allowed(path) || path == w.path+"/summary"
}
func (w *usageExecutionObserver) capture(r *http.Response) error {
	raw, re := io.ReadAll(io.LimitReader(r.Body, (1<<20)+1))
	ce := r.Body.Close()
	if re != nil || ce != nil || len(raw) > 1<<20 || r.StatusCode != 200 || r.ContentLength != int64(len(raw)) || r.Header.Get("Content-Encoding") != "" || r.Header.Get("Cache-Control") != "no-store" {
		return w.fail()
	}
	rid := r.Header.Get("X-Request-ID")
	parsed, err := f.ParseID[f.Request](rid)
	if err != nil || parsed.String() != rid || len(r.Header.Values("X-Request-ID")) != 1 || r.Request.Context().Value(projectUsageWebIngress{}) != true || r.Request.Host != "system-http.example.test" || r.Request.URL.Host != w.root.address || r.Request.URL.Scheme != "http" {
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
	if r.Request.URL.Path == w.path || r.Request.URL.Path == w.path+"/summary" {
		err = w.page(r.Request.URL, raw)
	}
	w.mu.Unlock()
	if err != nil {
		return w.fail()
	}
	proof := map[string]any{"input_hash": w.inputHash, "request_id": rid, "method": r.Request.Method, "url_sha256": fmt.Sprintf("%x", sha256.Sum256([]byte(r.Request.URL.RequestURI()))), "status": r.StatusCode, "content_length": len(raw), "body_sha256": fmt.Sprintf("%x", sha256.Sum256(raw)), "original_eof": true, "original_closed": true, "handler_returned": true, "handler_return_source": "app_http_request_log", "browser_ingress_host_matches": true, "downstream_host_matches": true}
	encoded, err := json.Marshal(proof)
	if err != nil || os.WriteFile(filepath.Join(w.evidence, "usage-execution-filter-"+usageExecutionMode+"-response-"+rid+".json"), encoded, 0600) != nil {
		return w.fail()
	}
	w.mu.Lock()
	w.count++
	w.mu.Unlock()
	r.Body = io.NopCloser(bytes.NewReader(raw))
	return nil
}
func (w *usageExecutionObserver) page(u *url.URL, raw []byte) error {
	bad := errors.New("execution-filter original query/page/aggregate differs")
	index := w.reads
	if index >= 13 {
		return bad
	}
	daily := index == 4 || index == 9 || index == 10
	filtered := index >= 2 && index <= 9
	continuation := index == 1 || index == 3 || index == 6 || index == 12
	if strings.HasSuffix(u.Path, "/summary") != daily {
		return bad
	}
	q, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		return bad
	}
	from, e1 := time.Parse(time.RFC3339Nano, q.Get("from"))
	to, e2 := time.Parse(time.RFC3339Nano, q.Get("to"))
	if e1 != nil || e2 != nil || to.Sub(from) != 30*24*time.Hour || !strings.HasSuffix(q.Get("from"), "Z") || !strings.HasSuffix(q.Get("to"), "Z") || to.After(time.Now().Add(time.Second)) {
		return bad
	}
	if index == 0 || index == 8 {
		if index == 8 {
			old, e := time.Parse(time.RFC3339Nano, w.to)
			if e != nil || !to.After(old) || q.Get("from") <= w.from {
				return bad
			}
		}
		w.from, w.to = q.Get("from"), q.Get("to")
	} else if q.Get("from") != w.from || q.Get("to") != w.to {
		return bad
	}
	wantQuery := url.Values{"from": {w.from}, "to": {w.to}, "limit": {"50"}}
	if filtered {
		wantQuery.Set("execution_id", w.execution)
	}
	if daily {
		wantQuery.Set("limit", "100")
		wantQuery.Set("group_by", "day")
	}
	if continuation {
		if w.cursor == "" {
			return bad
		}
		wantQuery.Set("cursor", w.cursor)
	}
	if !reflect.DeepEqual(q, wantQuery) {
		return bad
	}
	var body map[string]json.RawMessage
	if json.Unmarshal(raw, &body) != nil {
		return bad
	}
	if !daily {
		var page struct {
			Items []map[string]any `json:"items"`
			Next  *string          `json:"next_cursor"`
		}
		if len(body) != 2 || body["items"] == nil || body["next_cursor"] == nil || json.Unmarshal(raw, &page) != nil {
			return bad
		}
		all := w.rows
		if filtered {
			all = w.target
		}
		expected := all[:50]
		if continuation {
			expected = all[50:]
			if page.Next != nil {
				return bad
			}
		} else if page.Next == nil || *page.Next == "" {
			return bad
		}
		if !bytes.Equal(canonicalUsageJSON(page.Items), canonicalUsageJSON(expected)) {
			return bad
		}
		for _, row := range page.Items {
			at, e := time.Parse(time.RFC3339Nano, row["started_at"].(string))
			if e != nil || at.Before(from) || !at.Before(to) {
				return bad
			}
		}
		w.cursor = ""
		if page.Next != nil {
			w.cursor = *page.Next
		}
	} else {
		var page struct {
			Items []map[string]any `json:"items"`
			AsOf  string           `json:"as_of"`
			Next  *string          `json:"next_cursor"`
		}
		if len(body) != 3 || body["items"] == nil || body["as_of"] == nil || string(body["next_cursor"]) != "null" || json.Unmarshal(raw, &page) != nil {
			return bad
		}
		at, e := time.Parse(time.RFC3339Nano, page.AsOf)
		if e != nil || at.Before(to) || at.After(time.Now().Add(time.Second)) {
			return bad
		}
		for _, item := range page.Items {
			s, ok := item["summary"].(map[string]any)
			if !ok || s["as_of"] != page.AsOf {
				return bad
			}
			delete(s, "as_of")
		}
		expected := w.expectedAll
		if filtered {
			expected = w.expectedTarget
		}
		if !reflect.DeepEqual(page.Items, expected) {
			return bad
		}
	}
	w.reads++
	return nil
}
func usageExecutionBrowser(t *testing.T, parent context.Context, private, origin, dist, evidence, inputHash string, secrets ...string) map[string]any {
	t.Helper()
	harness, err := filepath.Abs("../account-captcha-web")
	projectUsageWebRequire(t, err)
	ctx, cancel := context.WithTimeout(parent, 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "node", filepath.Join(harness, "node_modules/@playwright/test/cli.js"), "test", "--config", filepath.Join(harness, "usage-execution-filter.config.js"))
	cmd.Dir = harness
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = time.Second
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		switch key {
		case "TMPDIR", "TMP", "TEMP", "NODE_COMPILE_CACHE", "PWTEST_CACHE_DIR", "XDG_CACHE_HOME":
			continue
		}
		if key != "DEBUG" && key != "PWDEBUG" && key != "PLAYWRIGHT_NO_COPY_PROMPT" && !strings.HasPrefix(key, "AGENTEAM_") {
			cmd.Env = append(cmd.Env, entry)
		}
	}
	for _, v := range []struct{ key, dir string }{{"TMPDIR", "tmp"}, {"TMP", "tmp"}, {"TEMP", "tmp"}, {"NODE_COMPILE_CACHE", "node-cache"}, {"PWTEST_CACHE_DIR", "playwright-cache"}, {"XDG_CACHE_HOME", "xdg-cache"}} {
		path := filepath.Join(private, v.dir)
		projectUsageWebRequire(t, os.MkdirAll(path, 0700))
		cmd.Env = append(cmd.Env, v.key+"="+path)
	}
	cmd.Env = append(cmd.Env, "PLAYWRIGHT_NO_COPY_PROMPT=1", "AGENTEAM_AUTH_WEB_ORIGIN="+origin, "AGENTEAM_AUTH_WEB_PRIVATE="+private, "AGENTEAM_AUTH_WEB_CHROMIUM=/usr/bin/chromium", "AGENTEAM_USAGE_EXECUTION_FILTER_WEB_DIST="+dist, "AGENTEAM_USAGE_EXECUTION_FILTER_WEB_EVIDENCE="+evidence, "AGENTEAM_USAGE_EXECUTION_FILTER_WEB_INPUT_HASH="+inputHash, "AGENTEAM_USAGE_EXECUTION_FILTER_WEB_CASE="+usageExecutionMode)
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	if cmd.Start() != nil {
		t.Fatal("owned execution-filter Node did not start")
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
	t.Logf("UsageExecutionFilter Node actual_wait pid=%d success=%t", cmd.Process.Pid, err == nil)
	safe := output.String()
	for _, secret := range secrets {
		safe = strings.ReplaceAll(safe, secret, "<redacted>")
	}
	t.Log(safe)
	if err != nil || !regexp.MustCompile(`(?m)\b1 passed\b`).Match(output.Bytes()) {
		t.Fatal("execution-filter locked browser did not pass its one case")
	}
	raw, err := os.ReadFile(filepath.Join(private, "usage-execution-filter-result.json"))
	if err != nil || len(raw) > 16<<10 {
		t.Fatal("execution-filter result unavailable")
	}
	var result map[string]any
	if json.Unmarshal(raw, &result) != nil || result["input_hash"] != inputHash {
		t.Fatal("execution-filter result differs from original inputs")
	}
	return result
}
