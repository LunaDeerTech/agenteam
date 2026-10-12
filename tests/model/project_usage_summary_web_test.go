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

const usageSummaryMode = "daily-server-aggregate"

func TestProjectUsageSummaryWeb(t *testing.T) {
	t.Run(usageSummaryMode, func(t *testing.T) {
		started := time.Now()
		ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
		t.Cleanup(func() { cancel(); projectUsageComplete(t, started) })
		dist, runtime := os.Getenv("AGENTEAM_PROJECT_USAGE_SUMMARY_WEB_DIST"), os.Getenv("AGENTEAM_AUTH_WEB_RUNTIME")
		evidence, input := os.Getenv("AGENTEAM_PROJECT_USAGE_SUMMARY_WEB_EVIDENCE"), os.Getenv("AGENTEAM_PROJECT_USAGE_SUMMARY_WEB_INPUT_HASH")
		digest, err := hex.DecodeString(input)
		if !filepath.IsAbs(dist) || !filepath.IsAbs(runtime) || len(runtime) > 45 || !filepath.IsAbs(evidence) || err != nil || len(digest) != sha256.Size {
			t.Fatal("owned Usage summary inputs required")
		}
		info, err := os.Lstat(filepath.Join(dist, "index.html"))
		if err != nil || !info.Mode().IsRegular() {
			t.Fatal("Usage summary frozen assets missing")
		}
		projectUsageWebRequire(t, os.MkdirAll(evidence, 0700))
		private, err := os.MkdirTemp(runtime, "pus-")
		projectUsageWebRequire(t, err)
		t.Cleanup(func() { projectUsageWebRequire(t, os.RemoveAll(private)) })
		v := newProjectUsageHTTPFixture(t)
		other, empty := v.createProject(t, v.owner), v.createProject(t, v.owner)
		boundary := usageNow(t, v.usageFixture).Time().UTC().Truncate(24 * time.Hour).Add(-24 * time.Hour)
		rows := map[string][]map[string]any{v.project.ID.String(): {}, other.ID.String(): {}, empty.ID.String(): {}}
		// These are controlled, persisted Runtime Facts times supplied before the
		// first formal Writer call. They are not claims of wire traffic in the past.
		seed := func(project id.ProjectID, at time.Time, usage json.RawMessage) {
			if ctx.Err() != nil {
				t.Fatal("Usage summary preparation exhausted original budget")
			}
			scenario := wireJSON(wireReply("summary", "stop", usage, nil))
			consumer := mc.Consumer{Kind: mc.ToolConsumer, ProjectID: project, Purpose: mc.ApprovalAuto, OperationID: newID[struct{}](t).String()}
			c := v.newCase(t, &scenario, &consumer)
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
				t.Fatal("Usage summary seed lacks actual wire/Writer terminal")
			}
			rows[project.String()] = append(rows[project.String()], projectUsageWebDTO(t, c.event.Value))
		}
		known := json.RawMessage(`{"prompt_tokens":7,"completion_tokens":3,"total_tokens":10,"prompt_tokens_details":{"cached_tokens":2,"cache_write_tokens":1},"completion_tokens_details":{"reasoning_tokens":1}}`)
		for n := 0; n < 52; n++ {
			var usage json.RawMessage
			if n < 25 {
				usage = known
			}
			seed(v.project.ID, boundary.Add(time.Duration(n-26)*time.Second), usage)
		}
		seed(other.ID, boundary.Add(-time.Second), json.RawMessage(`{"prompt_tokens":99,"completion_tokens":1,"total_tokens":100,"prompt_tokens_details":{"cached_tokens":0,"cache_write_tokens":0},"completion_tokens_details":{"reasoning_tokens":0}}`))
		for _, list := range rows {
			sort.Slice(list, func(i, j int) bool { return list[i]["started_at"].(string) > list[j]["started_at"].(string) })
		}
		earlier, later := boundary.Add(-time.Second).Format("2006-01-02"), boundary.Format("2006-01-02")
		expected := map[string][]map[string]any{
			v.project.ID.String(): {usageSummaryExpected(earlier, 26, 25, []int64{175, 75, 250, 50, 25, 25}), usageSummaryExpected(later, 26, 0, nil)},
			other.ID.String():     {usageSummaryExpected(earlier, 1, 1, []int64{99, 1, 100, 0, 0, 0})}, empty.ID.String(): {},
		}
		before := usageLedgerState(t, v.usageFixture)
		root := startProjectUsageRoot(t, v)
		w := &usageSummaryWebObserver{projectUsageWebObserver: &projectUsageWebObserver{root: root, evidence: evidence, inputHash: input, ids: map[string]bool{}}, rows: rows, expected: expected, windows: map[string][2]string{}, lists: map[string]int{}, summaries: map[string]int{}}
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
			http.Error(out, "owned summary response failed", http.StatusBadGateway)
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
		route := func(name string) string { return "/" + v.ownerName + "/" + name + "/settings/model-usage" }
		material := map[string]any{"mode": usageSummaryMode, "origin": server.URL, "cookie": v.ownerBrowser.cookie, "project_id": v.project.ID, "input_hash": input, "projects": []map[string]any{
			{"id": v.project.ID, "route": route(v.project.NormalizedName), "rows": rows[v.project.ID.String()], "expected": expected[v.project.ID.String()]},
			{"id": other.ID, "route": route(other.NormalizedName), "rows": rows[other.ID.String()], "expected": expected[other.ID.String()]},
			{"id": empty.ID, "route": route(empty.NormalizedName), "rows": rows[empty.ID.String()], "expected": expected[empty.ID.String()]},
		}}
		raw := projectUsageWebJSON(t, material)
		projectUsageWebRequire(t, os.WriteFile(filepath.Join(private, "project-usage-summary-material.json"), raw, 0600))
		clear(raw)
		result := usageSummaryBrowser(t, ctx, private, server.URL, dist, evidence, input)
		closeProxy()
		w.mu.Lock()
		count, failed := w.count, w.failed
		for p := range rows {
			if w.lists[p] != 1 || w.summaries[p] != 1 {
				failed = true
			}
		}
		w.mu.Unlock()
		if failed || result["completed"] != true || result["all_observed"] != true || result["original_requests"] != float64(count) || result["lists"] != float64(3) || result["summaries"] != float64(3) {
			t.Fatal("Usage summary original browser reads incomplete")
		}
		if usageLedgerState(t, v.usageFixture) != before {
			t.Fatal("Usage summary reads mutated ledger")
		}
		root.stop(t)
		if !strings.Contains(root.logs.String(), `"outcome":"drained"`) {
			t.Fatal("Usage summary default root not drained")
		}
		for _, secret := range []string{v.ownerBrowser.cookie, v.ownerBrowser.csrf} {
			if strings.Contains(root.logs.String(), secret) {
				t.Fatal("Usage summary logged browser credentials")
			}
		}
		t.Log("Usage summary: formal Writer 52 primary + 1 foreign rows, controlled Facts UTC timestamps, six nullable denominators, real empty Project; original public App/Node return, no production Runtime Facts claim")
	})
}

func usageSummaryExpected(day string, count, known int64, sums []int64) map[string]any {
	s := map[string]any{"confirmed_invocations": fmt.Sprint(count), "dispatch_unknown": "0", "succeeded": fmt.Sprint(count), "failed": "0", "cancelled": "0", "unknown": "0"}
	for n, name := range []string{"input", "output", "total", "cached_input", "cache_write", "reasoning"} {
		var sum any
		if known > 0 {
			sum = fmt.Sprint(sums[n])
		}
		s[name] = map[string]any{"sum": sum, "known_count": fmt.Sprint(known), "unknown_count": fmt.Sprint(count - known)}
	}
	return map[string]any{"key": map[string]any{"by": "day", "id": nil, "day": day}, "summary": s}
}

type usageSummaryWebObserver struct {
	*projectUsageWebObserver
	rows             map[string][]map[string]any
	expected         map[string][]map[string]any
	windows          map[string][2]string
	lists, summaries map[string]int
}

func (w *usageSummaryWebObserver) allowed(path string) bool {
	if path == "/api/v1/session" || path == "/api/v1/projects" || path == "/api/v1/projects/resolve" {
		return true
	}
	for project := range w.rows {
		base := "/api/v1/projects/" + project
		if path == base || path == base+"/model-usage" || path == base+"/model-usage/summary" {
			return true
		}
	}
	return false
}
func (w *usageSummaryWebObserver) capture(r *http.Response) error {
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
	for project := range w.rows {
		base := "/api/v1/projects/" + project + "/model-usage"
		if r.Request.URL.Path == base || r.Request.URL.Path == base+"/summary" {
			err = w.page(project, r.Request.URL, raw)
		}
	}
	w.mu.Unlock()
	if err != nil {
		return w.fail()
	}
	proof := map[string]any{"input_hash": w.inputHash, "request_id": rid, "method": r.Request.Method, "url_sha256": fmt.Sprintf("%x", sha256.Sum256([]byte(r.Request.URL.RequestURI()))), "status": r.StatusCode, "content_length": len(raw), "body_sha256": fmt.Sprintf("%x", sha256.Sum256(raw)), "original_eof": true, "original_closed": true, "handler_returned": true, "handler_return_source": "app_http_request_log", "browser_ingress_host_matches": true, "downstream_host_matches": true}
	encoded, err := json.Marshal(proof)
	if err != nil || os.WriteFile(filepath.Join(w.evidence, "project-usage-summary-"+usageSummaryMode+"-response-"+rid+".json"), encoded, 0600) != nil {
		return w.fail()
	}
	w.mu.Lock()
	w.count++
	w.mu.Unlock()
	r.Body = io.NopCloser(bytes.NewReader(raw))
	return nil
}
func (w *usageSummaryWebObserver) page(project string, u *url.URL, raw []byte) error {
	bad := errors.New("Usage summary actual scope/window/aggregate differs")
	q, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		return bad
	}
	daily := strings.HasSuffix(u.Path, "/summary")
	for k, values := range q {
		if len(values) != 1 || k != "from" && k != "to" && k != "limit" && (!daily || k != "group_by") {
			return bad
		}
	}
	from, e1 := time.Parse(time.RFC3339Nano, q.Get("from"))
	to, e2 := time.Parse(time.RFC3339Nano, q.Get("to"))
	if e1 != nil || e2 != nil || to.Sub(from) != 30*24*time.Hour || !strings.HasSuffix(q.Get("from"), "Z") || !strings.HasSuffix(q.Get("to"), "Z") || to.After(time.Now().Add(time.Second)) {
		return bad
	}
	window := [2]string{q.Get("from"), q.Get("to")}
	var body map[string]json.RawMessage
	if json.Unmarshal(raw, &body) != nil {
		return bad
	}
	if !daily {
		if q.Get("limit") != "50" || len(q) != 3 || w.lists[project] != 0 || w.summaries[project] != 0 || len(body) != 2 || body["items"] == nil || body["next_cursor"] == nil {
			return bad
		}
		var page struct {
			Items []map[string]any `json:"items"`
			Next  *string          `json:"next_cursor"`
		}
		if json.Unmarshal(raw, &page) != nil {
			return bad
		}
		want := w.rows[project]
		if len(want) > 50 {
			want = want[:50]
			if page.Next == nil || *page.Next == "" {
				return bad
			}
		} else if page.Next != nil {
			return bad
		}
		// Canonical seed DTOs include typed aliases; compare their JSON forms.
		if !bytes.Equal(canonicalUsageJSON(page.Items), canonicalUsageJSON(want)) {
			return bad
		}
		w.windows[project] = window
		w.lists[project]++
		return nil
	}
	if q.Get("limit") != "100" || q.Get("group_by") != "day" || len(q) != 4 || w.lists[project] != 1 || w.summaries[project] != 0 || w.windows[project] != window || len(body) != 3 || body["as_of"] == nil || string(body["next_cursor"]) != "null" {
		return bad
	}
	var page struct {
		Items []map[string]any `json:"items"`
		AsOf  string           `json:"as_of"`
	}
	if json.Unmarshal(raw, &page) != nil {
		return bad
	}
	at, err := time.Parse(time.RFC3339Nano, page.AsOf)
	if err != nil || at.Before(to) || at.After(time.Now().Add(time.Second)) {
		return bad
	}
	for _, item := range page.Items {
		s, ok := item["summary"].(map[string]any)
		if !ok || s["as_of"] != page.AsOf {
			return bad
		}
		delete(s, "as_of")
	}
	if !reflect.DeepEqual(page.Items, w.expected[project]) {
		return bad
	}
	w.summaries[project]++
	return nil
}
func canonicalUsageJSON(value any) []byte {
	raw, _ := json.Marshal(value)
	var decoded any
	if json.Unmarshal(raw, &decoded) != nil {
		return nil
	}
	out, _ := json.Marshal(decoded)
	return out
}

func usageSummaryBrowser(t *testing.T, parent context.Context, private, origin, dist, evidence, input string) map[string]any {
	t.Helper()
	harness, err := filepath.Abs("../account-captcha-web")
	projectUsageWebRequire(t, err)
	ctx, cancel := context.WithTimeout(parent, 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "node", filepath.Join(harness, "node_modules/@playwright/test/cli.js"), "test", "--config", filepath.Join(harness, "project-usage-summary.config.js"))
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
	cmd.Env = append(cmd.Env, "TMPDIR="+private, "PLAYWRIGHT_NO_COPY_PROMPT=1", "AGENTEAM_AUTH_WEB_ORIGIN="+origin, "AGENTEAM_AUTH_WEB_PRIVATE="+private, "AGENTEAM_AUTH_WEB_CHROMIUM=/usr/bin/chromium", "AGENTEAM_PROJECT_USAGE_SUMMARY_WEB_DIST="+dist, "AGENTEAM_PROJECT_USAGE_SUMMARY_WEB_EVIDENCE="+evidence, "AGENTEAM_PROJECT_USAGE_SUMMARY_WEB_INPUT_HASH="+input, "AGENTEAM_PROJECT_USAGE_SUMMARY_WEB_CASE="+usageSummaryMode)
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	if cmd.Start() != nil {
		t.Fatal("Usage summary Node did not start")
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
	t.Logf("ProjectUsageSummary Node actual_wait pid=%d success=%t", cmd.Process.Pid, err == nil)
	if err != nil {
		t.Fatal("Usage summary original browser failed")
	}
	raw, err := os.ReadFile(filepath.Join(private, "project-usage-summary-result.json"))
	if err != nil || len(raw) > 16<<10 {
		t.Fatal("Usage summary result missing")
	}
	defer clear(raw)
	var result map[string]any
	if json.Unmarshal(raw, &result) != nil || result["input_hash"] != input {
		t.Fatal("Usage summary result identity differs")
	}
	return result
}
