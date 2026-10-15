//go:build integration && linux

package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
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
	goruntime "runtime"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	ac "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	artifactc "github.com/LunaDeerTech/agenteam/internal/central/artifact/contract"
	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	gc "github.com/LunaDeerTech/agenteam/internal/central/governance/approval/contract"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	kc "github.com/LunaDeerTech/agenteam/internal/central/knowledge/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/meeting"
	c "github.com/LunaDeerTech/agenteam/internal/central/meeting/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
	wc "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
	netfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/outbound"
)

// Account, Project, Agent, original Execution busy ownership and Meeting HTTP
// are the real App. The first Task's original manager-return barrier remains
// held, so this browser proves the queued/busy Meeting UI, not Agent completion.
func TestMeetingM3OwnerWeb(t *testing.T) {
	dist, runtime, evidence, playwright, chromium := os.Getenv("AGENTEAM_MEETING_WEB_DIST"), os.Getenv("AGENTEAM_AUTH_WEB_RUNTIME"), os.Getenv("AGENTEAM_MEETING_WEB_EVIDENCE"), os.Getenv("AGENTEAM_MEETING_WEB_PLAYWRIGHT"), os.Getenv("AGENTEAM_AUTH_WEB_CHROMIUM")
	for _, path := range []string{dist, runtime, evidence, playwright, chromium} {
		if !filepath.IsAbs(path) {
			t.Fatal("exact owned Meeting browser inputs required")
		}
	}
	if len(runtime) > 45 {
		t.Fatal("owned Meeting runtime path too long")
	}
	if info, err := os.Stat(filepath.Join(dist, "index.html")); err != nil || !info.Mode().IsRegular() {
		t.Fatal("frozen Meeting production assets absent")
	}
	private, err := os.MkdirTemp(runtime, "mt-")
	workAppCheck(t, "meeting-web-private", err)
	t.Cleanup(func() { workAppCheck(t, "meeting-web-private-cleanup", os.RemoveAll(private)) })
	evidence = filepath.Join(evidence, t.Name())
	workAppCheck(t, "meeting-web-evidence", os.Mkdir(evidence, 0700))
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	workAppCheck(t, "meeting-web-listener", err)
	t.Cleanup(func() { _ = listener.Close() })
	origin := "http://" + listener.Addr().String()
	v, _, _ := taskExecutionReadFixture(t, origin, nil)
	backend, err := url.Parse(v.address)
	workAppCheck(t, "meeting-web-backend", err)
	transport := &http.Transport{Proxy: nil}
	proxy := httputil.NewSingleHostReverseProxy(backend)
	proxy.Transport = transport
	proxy.ErrorLog = log.New(io.Discard, "", 0)
	var mu sync.Mutex
	mutations, lookups := 0, 0
	dropped := false
	var originalReceipt c.Receipt
	proxy.ModifyResponse = func(response *http.Response) error {
		request := response.Request
		if request.Method != http.MethodPost || !strings.Contains(request.URL.Path, "/meetings") {
			return nil
		}
		mu.Lock()
		defer mu.Unlock()
		if strings.HasSuffix(request.URL.Path, "/lookup") {
			lookups++
			return nil
		}
		mutations++
		if !strings.HasSuffix(request.URL.Path, "/turns") || dropped {
			return nil
		}
		raw, err := io.ReadAll(io.LimitReader(response.Body, 8193))
		closeErr := response.Body.Close()
		if err != nil || closeErr != nil || len(raw) > 8192 || response.StatusCode != 200 || json.Unmarshal(raw, &originalReceipt) != nil {
			return errors.New("owned Meeting send response invalid")
		}
		dropped = true
		// The original complete committed response is observed before dropping only
		// its delivery. Browser recovery must query the same command, never resend.
		return errors.New("owned Meeting response delivery interrupted")
	}
	proxy.ErrorHandler = func(w http.ResponseWriter, _ *http.Request, _ error) {
		http.Error(w, "Owned response unavailable", http.StatusBadGateway)
	}
	server := &http.Server{ReadHeaderTimeout: 2 * time.Second, ErrorLog: log.New(io.Discard, "", 0), Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			proxy.ServeHTTP(w, r)
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
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
	var stopOnce sync.Once
	stop := func() {
		stopOnce.Do(func() {
			ctx, cancel := context.WithTimeout(context.WithoutCancel(v.ctx), 3*time.Second)
			defer cancel()
			if err := server.Shutdown(ctx); err != nil {
				t.Error("Meeting proxy shutdown did not join")
				_ = server.Close()
			}
			if err := <-served; err != nil && !errors.Is(err, http.ErrServerClosed) {
				t.Error("Meeting proxy original Serve failed")
			}
			transport.CloseIdleConnections()
		})
	}
	t.Cleanup(stop)
	session, err := v.request(v.ctx, http.MethodGet, "/api/v1/session", nil, "")
	workAppCheck(t, "meeting-web-owner-session", err)
	var identity struct {
		User struct {
			Username string `json:"username"`
		} `json:"user"`
	}
	if session.status != 200 || json.Unmarshal(session.body, &identity) != nil || identity.User.Username == "" {
		t.Fatal("Meeting Owner route identity missing")
	}
	payload := map[string]any{"origin": origin, "cookie": v.cookie, "project_id": v.a.String(), "agent_id": v.agent.String(), "route": "/" + identity.User.Username + "/runtime-app-a/meetings", "evidence": evidence}
	raw, err := json.Marshal(payload)
	workAppCheck(t, "meeting-web-material", err)
	workAppCheck(t, "meeting-web-material-write", os.WriteFile(filepath.Join(private, "input.json"), raw, 0600))
	clear(raw)
	script := filepath.Join(private, "meeting-web.cjs")
	workAppCheck(t, "meeting-web-script", os.WriteFile(script, []byte(meetingM3BrowserScript), 0600))
	meetingM3RunBrowser(t, v.ctx, private, script, playwright, chromium)
	resultRaw, err := os.ReadFile(filepath.Join(private, "result.json"))
	workAppCheck(t, "meeting-web-result", err)
	var result struct {
		Completed bool   `json:"completed"`
		Meeting   string `json:"meeting"`
	}
	if len(resultRaw) > 4096 || json.Unmarshal(resultRaw, &result) != nil || !result.Completed {
		t.Fatal("Meeting browser did not finish its original scenario")
	}
	meetingID, err := f.ParseID[c.Meeting](result.Meeting)
	workAppCheck(t, "meeting-web-result-id", err)
	response, err := v.request(v.ctx, http.MethodGet, "/api/v1/projects/"+v.a.String()+"/meetings/"+meetingID.String(), nil, "")
	workAppCheck(t, "meeting-web-authoritative-detail", err)
	var detail c.Detail
	if response.status != 200 || json.Unmarshal(response.body, &detail) != nil || detail.Session.ID != meetingID {
		t.Fatal("Meeting authoritative detail differs")
	}
	userMessages := []string{}
	for _, turn := range detail.Turns {
		for _, contribution := range turn.Contributions {
			if contribution.Kind == "user" && contribution.Message != nil {
				var content strings.Builder
				for _, node := range contribution.Message.Content {
					if node.Type == "text" {
						content.WriteString(node.Text)
					}
				}
				userMessages = append(userMessages, content.String())
			}
		}
	}
	mu.Lock()
	actualMutations, actualLookups, actuallyDropped, receipt := mutations, lookups, dropped, originalReceipt
	mu.Unlock()
	if actualMutations != 2 || actualLookups != 1 || !actuallyDropped || receipt.MeetingID != meetingID || len(userMessages) != 2 || userMessages[0] != "Discuss the release checklist." || userMessages[1] != "Keep the migration reversible." {
		t.Fatal("Meeting original commands duplicated or recovery differs")
	}
	stop()
	// The existing fixture stops the original runtime, releases its real held
	// discovery and checks the process guard; do not manufacture completion.
	t.Log("MEETING_M3_OWNER_WEB real_owner_http=true list_start_detail_send_reload=true unknown_lookup_original=true mutation2_lookup1=true task_busy_fixture=true agent_completion=false browser_original_wait=true")
}

func meetingM3RunBrowser(t *testing.T, ctx context.Context, private, script, playwright, chromium string) {
	t.Helper()
	cmd := exec.CommandContext(ctx, "node", script)
	cmd.Dir = private
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = time.Second
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if key != "TMPDIR" && key != "DEBUG" && key != "PWDEBUG" && !strings.HasPrefix(key, "AGENTEAM_") {
			cmd.Env = append(cmd.Env, entry)
		}
	}
	cmd.Env = append(cmd.Env, "TMPDIR="+private, "AGENTEAM_MEETING_WEB_PLAYWRIGHT="+playwright, "AGENTEAM_AUTH_WEB_CHROMIUM="+chromium)
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	workAppCheck(t, "meeting-web-node-start", cmd.Start())
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
	t.Logf("Meeting Node actual_wait pid=%d success=%t", cmd.Process.Pid, err == nil)
	if err != nil || !bytes.Contains(output.Bytes(), []byte("MEETING_BROWSER_COMPLETE")) {
		var result struct {
			Stage        string `json:"stage"`
			Status       int    `json:"status"`
			Code         string `json:"code"`
			Error        string `json:"error"`
			Step         string `json:"step"`
			Line         int    `json:"line"`
			Reads        int    `json:"execution_reads"`
			ReadFailures int    `json:"execution_read_failures"`
			ReadStatus   int    `json:"execution_read_status"`
			PageErrors   int    `json:"page_errors"`
			Visible      string `json:"execution_visible"`
			ReadBusy     bool   `json:"execution_busy"`
			ReadError    bool   `json:"execution_error"`
			Hidden       bool   `json:"document_hidden"`
		}
		if raw, e := os.ReadFile(filepath.Join(private, "failure.json")); e == nil && len(raw) < 1024 {
			_ = json.Unmarshal(raw, &result)
		}
		allowed := map[string]bool{"launch": true, "open": true, "choose": true, "start": true, "start-response": true, "start-navigation": true, "start-detail": true, "draft": true, "send": true, "lookup": true, "reload": true, "layout": true, "close": true, "answer": true, "skip": true, "cancel": true, "archive": true, "approve": true, "reject": true}
		if allowed[result.Stage] {
			t.Logf("Meeting browser failed_stage=%s", result.Stage)
			if result.Status >= 100 && result.Status <= 599 {
				t.Logf("Meeting browser response_status=%d", result.Status)
			}
			safe := regexp.MustCompile(`^[A-Z_]{1,64}$`)
			if safe.MatchString(result.Code) {
				t.Logf("Meeting browser response_code=%s", result.Code)
			}
			if safe.MatchString(result.Error) {
				t.Logf("Meeting browser failure_kind=%s", result.Error)
			}
			if regexp.MustCompile(`^[a-z-]{1,64}$`).MatchString(result.Step) {
				t.Logf("Meeting browser failure_step=%s", result.Step)
			}
			if result.Line > 0 && result.Line < 10000 {
				t.Logf("Meeting browser failure_script_line=%d", result.Line)
			}
			if result.Reads >= 0 && result.Reads < 1000 && result.ReadFailures >= 0 && result.ReadFailures < 1000 && result.PageErrors >= 0 && result.PageErrors < 1000 {
				t.Logf("Meeting browser execution_reads=%d execution_read_failures=%d page_errors=%d busy=%t read_error=%t document_hidden=%t", result.Reads, result.ReadFailures, result.PageErrors, result.ReadBusy, result.ReadError, result.Hidden)
			}
			if result.ReadStatus >= 100 && result.ReadStatus <= 599 {
				t.Logf("Meeting browser execution_read_status=%d", result.ReadStatus)
			}
			if map[string]bool{"absent": true, "created": true, "preparing": true, "running": true, "approval": true, "decision": true, "succeeded": true, "failed": true, "cancelled": true}[result.Visible] {
				t.Logf("Meeting browser execution_visible=%s", result.Visible)
			}
		}
		t.Fatal("Meeting locked browser failed; raw credential-bearing output withheld")
	}
}

const meetingM3BrowserScript = `
const fs = require('node:fs');
const path = require('node:path');
const root = process.env.AGENTEAM_MEETING_WEB_PLAYWRIGHT;
if (require(path.join(root, 'package.json')).version !== '1.56.1') throw new Error('LOCKED_PLAYWRIGHT_REQUIRED');
const { chromium } = require(root);
const input = JSON.parse(fs.readFileSync('input.json', 'utf8'));
let stage = 'launch', diagnosticStatus = 0, diagnosticCode = '';
(async () => {
  let browser, page;
  try {
    browser = await chromium.launch({ executablePath: process.env.AGENTEAM_AUTH_WEB_CHROMIUM, args: ['--no-sandbox'] });
    const context = await browser.newContext({ viewport: { width: 1440, height: 900 }, colorScheme: 'light' });
    await context.addCookies([{ name: 'agenteam_local_session', value: input.cookie, url: input.origin, httpOnly: true, sameSite: 'Strict' }]);
    page = await context.newPage();
    page.setDefaultTimeout(8000);
    stage = 'open';
    await page.goto(input.origin + input.route);
    await page.getByRole('heading', { name: '开始一场会议' }).waitFor();
    stage = 'choose';
    await page.getByRole('button', { name: '编排 Agent', exact: true }).click();
    await page.getByRole('dialog', { name: '编排参会 Agent' }).getByRole('checkbox', { name: /runtime-app-agent/ }).check();
    await page.getByRole('dialog', { name: '编排参会 Agent' }).getByRole('button', { name: '完成', exact: true }).click();
    await page.getByRole('textbox', { name: '会议首条消息', exact: true }).fill('Discuss the release checklist.');
    stage = 'start';
    const created = page.waitForResponse(r => r.request().method() === 'POST' && r.url() === input.origin + '/api/v1/projects/' + input.project_id + '/meetings');
    await page.getByRole('button', { name: '发送', exact: true }).click();
    const response = await created;
    stage = 'start-response'; diagnosticStatus = response.status();
    const receipt = await response.json();
    if (response.status() !== 200) { diagnosticCode = /^[A-Z_]{1,64}$/.test(receipt.code || '') ? receipt.code : ''; throw new Error('START_REJECTED'); }
    stage = 'start-navigation';
    await page.waitForURL(input.origin + input.route + '/' + receipt.meeting_id);
    stage = 'start-detail';
    await page.getByText('Discuss the release checklist.', { exact: true }).waitFor();
    stage = 'draft';
    await page.getByRole('textbox', { name: '发送到会议', exact: true }).fill('Keep the migration reversible.');
    await page.getByRole('button', { name: '新会议', exact: true }).click();
    await page.getByRole('dialog', { name: '保留未提交的输入？' }).getByRole('button', { name: '继续编辑', exact: true }).click();
    if (await page.getByRole('textbox', { name: '发送到会议', exact: true }).inputValue() !== 'Keep the migration reversible.') throw new Error('DRAFT_LOST');
    stage = 'send';
    await page.getByRole('button', { name: '发送', exact: true }).click();
    await page.getByRole('button', { name: '查询原操作', exact: true }).waitFor();
    stage = 'lookup';
    const recovered = page.waitForResponse(r => r.request().method() === 'POST' && r.url().endsWith('/turns/lookup'));
    await page.getByRole('button', { name: '查询原操作', exact: true }).click();
    const lookup = await recovered;
    if (lookup.status() !== 200 || (await lookup.json()).status !== 'committed') throw new Error('LOOKUP_FAILED');
    await page.getByText('Keep the migration reversible.', { exact: true }).waitFor();
    stage = 'reload';
    await page.reload();
    await page.getByText('Discuss the release checklist.', { exact: true }).waitFor();
    await page.getByText('Keep the migration reversible.', { exact: true }).waitFor();
    stage = 'layout';
    await page.screenshot({ path: path.join(input.evidence, 'meeting-desktop-light.png'), fullPage: true });
    await page.emulateMedia({ colorScheme: 'dark', reducedMotion: 'reduce' });
    await page.setViewportSize({ width: 390, height: 844 });
    await page.getByRole('button', { name: '会议列表', exact: true }).click();
    await page.getByRole('dialog', { name: '会议', exact: true }).waitFor();
    await page.keyboard.press('Escape');
    if (!await page.getByRole('button', { name: '会议列表', exact: true }).evaluate(el => el === document.activeElement)) throw new Error('LIST_FOCUS_NOT_RESTORED');
    await page.getByRole('button', { name: '详情', exact: true }).click();
    await page.getByRole('dialog', { name: '会议详情', exact: true }).waitFor();
    await page.keyboard.press('Escape');
    if (!await page.getByRole('button', { name: '详情', exact: true }).evaluate(el => el === document.activeElement)) throw new Error('DETAIL_FOCUS_NOT_RESTORED');
    if (await page.evaluate(() => document.documentElement.scrollWidth > innerWidth)) throw new Error('PAGE_OVERFLOW');
    await page.screenshot({ path: path.join(input.evidence, 'meeting-mobile-dark.png'), fullPage: true });
    stage = 'close';
    await context.close();
    await browser.close(); browser = null;
    fs.writeFileSync('result.json', JSON.stringify({ completed: true, meeting: receipt.meeting_id }), { mode: 0o600 });
    console.log('MEETING_BROWSER_COMPLETE');
  } catch (error) {
    const kind = error && error.name === 'TimeoutError' ? 'TIMEOUT' : /^[A-Z_]{1,64}$/.test(error && error.message || '') ? error.message : 'UNCLASSIFIED';
    fs.writeFileSync('failure.json', JSON.stringify({ stage, status: diagnosticStatus, code: diagnosticCode, error: kind }), { mode: 0o600 });
    if (page) await page.screenshot({ path: path.join(input.evidence, 'meeting-failure.png'), fullPage: true }).catch(() => {});
    if (browser) await browser.close().catch(() => {});
    process.exitCode = 1;
  }
})();
`

// Human actions enter through Chromium and the real HTTP boundary. The original
// Executor owns every Tool wait and continuation; only Provider replies are
// deterministic. A committed answer response is dropped once to prove that
// the visible recovery queries the original command without resending it.
func TestMeetingM3DecisionOwnerWeb(t *testing.T) {
	meetingM3ConfigurationPreflight(t)
	dist, runtime, evidence, playwright, chromium := os.Getenv("AGENTEAM_MEETING_WEB_DIST"), os.Getenv("AGENTEAM_AUTH_WEB_RUNTIME"), os.Getenv("AGENTEAM_MEETING_WEB_EVIDENCE"), os.Getenv("AGENTEAM_MEETING_WEB_PLAYWRIGHT"), os.Getenv("AGENTEAM_AUTH_WEB_CHROMIUM")
	for _, path := range []string{dist, runtime, evidence, playwright, chromium} {
		if !filepath.IsAbs(path) {
			t.Fatal("exact owned Meeting browser inputs required")
		}
	}
	if len(runtime) > 45 {
		t.Fatal("owned Meeting runtime path too long")
	}
	if info, err := os.Stat(filepath.Join(dist, "index.html")); err != nil || !info.Mode().IsRegular() {
		t.Fatal("frozen Meeting assets absent")
	}
	private, err := os.MkdirTemp(runtime, "md-")
	workAppCheck(t, "decision-web-private", err)
	t.Cleanup(func() { workAppCheck(t, "decision-web-private-cleanup", os.RemoveAll(private)) })
	evidence = filepath.Join(evidence, t.Name())
	workAppCheck(t, "decision-web-evidence", os.Mkdir(evidence, 0700))
	w, listener := newM1WorkflowWithOptions(t, nil, false)
	v := w.app
	meetingM3VerifyOwnedDatabase(t, v)
	var tool string
	workAppCheck(t, "decision-web-tool", v.admin.QueryRow(v.ctx, `SELECT tool_id FROM agenteam_tool.identities WHERE stable_key='builtin:request-decision'`).Scan(&tool))
	if _, err := f.ParseID[struct{}](tool); err != nil {
		t.Fatal("invalid original Decision tool identity")
	}
	question := "Which calibration batch should we use?"
	args := string(workAppJSON(t, map[string]any{"question": question, "options": []string{"smaller", "larger"}}))
	initial := workAppJSON(t, map[string]any{"id": "owned-meeting-browser-decision", "object": "chat.completion", "created": 1, "model": "meeting-collaboration", "choices": []any{map[string]any{"index": 0, "message": map[string]any{"role": "assistant", "content": nil, "tool_calls": []any{map[string]any{"id": "meeting_decision_call", "type": "function", "function": map[string]any{"name": "tool_" + strings.ReplaceAll(tool, "-", ""), "arguments": args}}}}, "finish_reason": "tool_calls"}}, "usage": map[string]any{"prompt_tokens": 10, "completion_tokens": 10, "total_tokens": 20}})
	request := netfixture.WireScenario{Status: http.StatusOK, Headers: map[string]string{"Content-Type": "application/json"}, Chunks: [][]byte{initial}}
	first := string(workAppJSON(t, map[string]string{"title": "Calibration decision", "goals": "Select a batch", "decisions": "Owner answered", "unresolved": "", "facts": "First contribution"}))
	second := string(workAppJSON(t, map[string]string{"goals": "Select a batch", "decisions": "Owner answered and skipped", "unresolved": "", "facts": "Two contributions"}))
	scenario, err := w.network.Create(v.ctx, netfixture.ScenarioConfig{Mode: "openai_chat_wire", WireSequence: []netfixture.WireScenario{request, meetingM3Reply(t, 2, "I used the owner's smaller calibration batch."), meetingM3Reply(t, 3, first), request, meetingM3Reply(t, 5, "I continued after the owner skipped the question."), meetingM3Reply(t, 6, second), request}})
	workAppCheck(t, "decision-web-peer", err)
	defer func() {
		if t.Failed() {
			meetingM3FailureObservation(t, w, scenario)
		}
	}()
	model := meetingM3ConfigureModel(t, v, "https://fixture.test:8443/case/"+scenario)
	meetingM3CreateAgents(t, v, model)
	v.store.releaseDirectory()

	backend, err := url.Parse(v.address)
	workAppCheck(t, "decision-web-backend", err)
	transport := &http.Transport{Proxy: nil}
	proxy := httputil.NewSingleHostReverseProxy(backend)
	proxy.Transport, proxy.ErrorLog = transport, log.New(io.Discard, "", 0)
	var mu sync.Mutex
	decisions, lookups, controls := 0, 0, 0
	dropped := false
	proxy.ModifyResponse = func(response *http.Response) error {
		if response.Request.Method != http.MethodPost {
			return nil
		}
		path := response.Request.URL.Path
		mu.Lock()
		defer mu.Unlock()
		if strings.HasSuffix(path, "/decisions/lookup") {
			lookups++
			return nil
		}
		if strings.HasSuffix(path, "/controls") {
			controls++
			return nil
		}
		if !strings.HasSuffix(path, "/decisions") {
			return nil
		}
		decisions++
		if dropped {
			return nil
		}
		raw, err := io.ReadAll(io.LimitReader(response.Body, 8193))
		closeErr := response.Body.Close()
		var receipt c.Receipt
		valid := err == nil && closeErr == nil && len(raw) <= 8192 && response.StatusCode == 200 && json.Unmarshal(raw, &receipt) == nil && receipt.MeetingID.Validate() == nil
		clear(raw)
		if !valid {
			return errors.New("owned Decision response invalid")
		}
		dropped = true
		return errors.New("owned committed Decision response interrupted")
	}
	proxy.ErrorHandler = func(w http.ResponseWriter, _ *http.Request, _ error) {
		http.Error(w, "Owned response unavailable", http.StatusBadGateway)
	}
	server := &http.Server{ReadHeaderTimeout: 2 * time.Second, ErrorLog: log.New(io.Discard, "", 0), Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			proxy.ServeHTTP(w, r)
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
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
	var stopOnce sync.Once
	stop := func() {
		stopOnce.Do(func() {
			ctx, cancel := context.WithTimeout(context.WithoutCancel(v.ctx), 3*time.Second)
			defer cancel()
			if err := server.Shutdown(ctx); err != nil {
				t.Error("Decision proxy did not join")
				_ = server.Close()
			}
			if err := <-served; err != nil && !errors.Is(err, http.ErrServerClosed) {
				t.Error("Decision original Serve failed")
			}
			transport.CloseIdleConnections()
		})
	}
	t.Cleanup(stop)
	session, err := v.request(v.ctx, http.MethodGet, "/api/v1/session", nil, "")
	workAppCheck(t, "decision-web-owner", err)
	var identity struct {
		User struct {
			Username string `json:"username"`
		} `json:"user"`
	}
	if session.status != 200 || json.Unmarshal(session.body, &identity) != nil || identity.User.Username == "" {
		t.Fatal("Decision Owner identity absent")
	}
	payload := map[string]any{"origin": w.origin, "cookie": v.cookie, "project_id": v.a.String(), "route": "/" + identity.User.Username + "/runtime-app-a/meetings", "evidence": evidence}
	raw, err := json.Marshal(payload)
	workAppCheck(t, "decision-web-input", err)
	workAppCheck(t, "decision-web-input-write", os.WriteFile(filepath.Join(private, "input.json"), raw, 0600))
	clear(raw)
	script := filepath.Join(private, "meeting-decision-web.cjs")
	workAppCheck(t, "decision-web-script", os.WriteFile(script, []byte(meetingM3DecisionBrowserScript), 0600))
	meetingM3RunBrowser(t, v.ctx, private, script, playwright, chromium)
	raw, err = os.ReadFile(filepath.Join(private, "result.json"))
	workAppCheck(t, "decision-web-result", err)
	var result struct {
		Completed bool   `json:"completed"`
		Meeting   string `json:"meeting"`
	}
	if len(raw) > 4096 || json.Unmarshal(raw, &result) != nil || !result.Completed {
		t.Fatal("Decision browser did not complete")
	}
	meetingID, err := f.ParseID[c.Meeting](result.Meeting)
	workAppCheck(t, "decision-web-meeting", err)
	detail, err := v.runtime.meetings.GetDetail(v.ctx, v.actor, v.a, meetingID)
	workAppCheck(t, "decision-web-detail", err)
	if detail.Session.Status != "active" || detail.Session.Summary == nil || detail.Session.Summary.Version != 2 || len(detail.Turns) != 3 || len(detail.Decisions) != 3 {
		t.Fatal("Decision browser final meeting state differs")
	}
	for n, status := range []string{"answered", "skipped", "cancelled"} {
		turnStatus := "completed"
		if n == 2 {
			turnStatus = "cancelled"
		}
		if detail.Decisions[n].Status != status || detail.Turns[n].Status != turnStatus {
			t.Fatal("Decision browser final turn or resolution differs")
		}
	}
	if detail.Decisions[0].Answer == nil || *detail.Decisions[0].Answer != "Use the smaller calibration batch." {
		t.Fatal("Decision browser answer differs")
	}
	var executions, succeeded, cancelled, pairs, calls, retired, released int
	err = v.admin.QueryRow(v.ctx, `SELECT (SELECT count(*) FROM agenteam_execution.executions WHERE project_id=$1),(SELECT count(*) FROM agenteam_execution.executions WHERE project_id=$1 AND status='succeeded'),(SELECT count(*) FROM agenteam_execution.executions WHERE project_id=$1 AND status='cancelled'),(SELECT count(*) FROM agenteam_execution.loop_decision_results d JOIN agenteam_meeting.decision_requests m ON m.id=d.decision_id AND m.version=d.decision_version JOIN agenteam_execution.loop_tool_results t ON t.execution_id=d.execution_id AND t.round_id=d.round_id AND t.operation_id=d.operation_id AND t.message_digest=d.message_digest AND t.transcript_sequence=d.transcript_sequence AND t.checkpoint_version=d.checkpoint_version WHERE m.meeting_id=$2),(SELECT count(*) FROM agenteam_model.calls WHERE project_id::text=$1),(SELECT count(*) FROM agenteam_model.calls WHERE project_id::text=$1 AND retired),(SELECT count(*) FROM agenteam_model.calls c JOIN agenteam_secret.secret_leases l ON l.id=c.lease_id WHERE c.project_id::text=$1 AND l.released)`, v.a.String(), meetingID.String()).Scan(&executions, &succeeded, &cancelled, &pairs, &calls, &retired, &released)
	workAppCheck(t, "decision-web-durable-pairs", err)
	if executions != 3 || succeeded != 2 || cancelled != 1 || pairs != 2 || calls != 7 || retired != 7 || released != 7 {
		t.Fatalf("Decision browser durable counts differ e=%d success=%d cancel=%d pairs=%d calls=%d retired=%d released=%d", executions, succeeded, cancelled, pairs, calls, retired, released)
	}
	mu.Lock()
	actualDecisions, actualLookups, actualControls, actuallyDropped := decisions, lookups, controls, dropped
	mu.Unlock()
	if actualDecisions != 2 || actualLookups != 1 || actualControls != 3 || !actuallyDropped {
		t.Fatal("Decision browser original command was duplicated or omitted")
	}
	stop()
	v.runtime.StopAdmission()
	drain, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	err = v.runtime.Drain(drain)
	cancel()
	workAppCheck(t, "decision-web-drain", err)
	v.requestStop()
	usageAppWait(t, v.root.done, "decision-web-original-app-return")
	workAppCheck(t, "decision-web-original-app-exit", v.root.err)
	if !v.runtime.Joined() || !v.assembly.Joined() || !v.root.owned.producersJoined() {
		t.Fatal("Decision browser original owners not joined")
	}
	w.joinedPeer(t, scenario, 7, 7)
	meetingM3GuardReleased(t, v.workAppFixture)
	t.Log("MEETING_M3_DECISION_WEB real_browser_owner_answer_skip_cancel=true original_answer_lookup=true archive_restore=true same_execution_tool_result=true original_app_peer_wait=true external_model=false")
}

const meetingM3DecisionBrowserScript = `
const fs = require('node:fs'), path = require('node:path');
const root = process.env.AGENTEAM_MEETING_WEB_PLAYWRIGHT;
if (require(path.join(root,'package.json')).version !== '1.56.1') throw new Error('LOCKED_PLAYWRIGHT_REQUIRED');
const { chromium, expect } = require(root);
if (process.argv.includes('--list')) { console.log('TestMeetingM3DecisionOwnerWeb: answer / original lookup / skip / cancel / archive / restore'); process.exit(0); }
const input = JSON.parse(fs.readFileSync('input.json','utf8'));
let stage='launch', diagnosticStatus=0, diagnosticCode='';
(async()=>{
 let browser,page;
 try {
  browser=await chromium.launch({executablePath:process.env.AGENTEAM_AUTH_WEB_CHROMIUM,args:['--no-sandbox']});
  const context=await browser.newContext({viewport:{width:1440,height:900},colorScheme:'light'});
  await context.addCookies([{name:'agenteam_local_session',value:input.cookie,url:input.origin,httpOnly:true,sameSite:'Strict'}]);
  page=await context.newPage();page.setDefaultTimeout(8000);
  stage='open';await page.goto(input.origin+input.route);await page.getByRole('heading',{name:'开始一场会议'}).waitFor();
  stage='choose';await page.getByRole('button',{name:'编排 Agent',exact:true}).click();
  await page.getByRole('dialog',{name:'编排参会 Agent'}).getByRole('checkbox',{name:/meeting-agent-1/}).check();
  await page.getByRole('dialog',{name:'编排参会 Agent'}).getByRole('button',{name:'完成',exact:true}).click();
  await page.getByRole('textbox',{name:'会议首条消息',exact:true}).fill('Ask me to select the calibration batch.');
  stage='start';const created=page.waitForResponse(r=>r.request().method()==='POST'&&r.url()===input.origin+'/api/v1/projects/'+input.project_id+'/meetings');
  await page.getByRole('button',{name:'发送',exact:true}).click();const response=await created,receipt=await response.json();diagnosticStatus=response.status();
  if(response.status()!==200){diagnosticCode=receipt.code||'';throw new Error('START_REJECTED')}
  await page.waitForURL(input.origin+input.route+'/'+receipt.meeting_id);
  stage='answer';await page.getByRole('textbox',{name:'你的决定',exact:true}).fill('Use the smaller calibration batch.');
  await page.screenshot({path:path.join(input.evidence,'decision-desktop-light.png'),fullPage:true});
  await page.getByRole('button',{name:'提交回答',exact:true}).click();
  await page.getByRole('button',{name:'查询原操作',exact:true}).waitFor();
  stage='lookup';const looked=page.waitForResponse(r=>r.request().method()==='POST'&&r.url().endsWith('/decisions/lookup'));
  await page.getByRole('button',{name:'查询原操作',exact:true}).click();const lookup=await looked;
  if(lookup.status()!==200||(await lookup.json()).status!=='committed')throw new Error('LOOKUP_FAILED');
  await page.getByText("I used the owner's smaller calibration batch.",{exact:true}).waitFor();
  await page.getByRole('heading',{name:'Calibration decision',exact:true}).waitFor();
  await expect(page.locator('.turn').filter({has:page.getByText('已完成',{exact:true})})).toHaveCount(1);
  stage='skip';await page.getByRole('textbox',{name:'发送到会议',exact:true}).fill('Ask again, and continue if I skip.');
  await page.getByRole('button',{name:'发送',exact:true}).click();await page.getByRole('button',{name:'跳过这个问题',exact:true}).click();
  await page.getByText('I continued after the owner skipped the question.',{exact:true}).waitFor();
  await expect(page.locator('.turn').filter({has:page.getByText('已完成',{exact:true})})).toHaveCount(2);
  stage='cancel';await page.getByRole('textbox',{name:'发送到会议',exact:true}).fill('Ask once more; I will cancel this turn.');
  await page.getByRole('button',{name:'发送',exact:true}).click();await page.getByRole('textbox',{name:'你的决定',exact:true}).waitFor();
  await page.getByRole('button',{name:'取消本轮',exact:true}).click();
  await page.getByRole('dialog',{name:'取消本轮讨论？'}).getByRole('button',{name:'取消本轮',exact:true}).click();
  await page.getByText('问题已随本轮取消',{exact:true}).waitFor();
  await expect(page.locator('.turn').filter({has:page.getByText('已取消',{exact:true})})).toHaveCount(1);
  stage='archive';await page.getByRole('button',{name:'归档',exact:true}).click();await page.getByText('会议已归档。',{exact:true}).waitFor();
  await page.getByRole('button',{name:'恢复会议',exact:true}).click();await expect(page.getByRole('button',{name:'归档',exact:true})).toBeEnabled();
  stage='reload';await page.reload();await page.getByText('问题已随本轮取消',{exact:true}).waitFor();
  await page.getByText('Use the smaller calibration batch.',{exact:true}).waitFor();
  stage='layout';await page.emulateMedia({colorScheme:'dark',reducedMotion:'reduce'});await page.setViewportSize({width:390,height:844});
  await page.getByRole('button',{name:'详情',exact:true}).click();await page.getByRole('dialog',{name:'会议详情',exact:true}).waitFor();await page.keyboard.press('Escape');
  if(!await page.getByRole('button',{name:'详情',exact:true}).evaluate(el=>el===document.activeElement))throw new Error('DETAIL_FOCUS_NOT_RESTORED');
  if(await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth))throw new Error('PAGE_OVERFLOW');
  await page.screenshot({path:path.join(input.evidence,'decision-mobile-dark.png'),fullPage:true});
  stage='close';await context.close();await browser.close();browser=null;
  fs.writeFileSync('result.json',JSON.stringify({completed:true,meeting:receipt.meeting_id}),{mode:0o600});console.log('MEETING_BROWSER_COMPLETE');
 } catch(error) {
  const kind=error&&error.name==='TimeoutError'?'TIMEOUT':/^[A-Z_]{1,64}$/.test(error&&error.message||'')?error.message:'UNCLASSIFIED';
  fs.writeFileSync('failure.json',JSON.stringify({stage,status:diagnosticStatus,code:diagnosticCode,error:kind}),{mode:0o600});
  if(page)await page.screenshot({path:path.join(input.evidence,'decision-failure.png'),fullPage:true}).catch(()=>{});
  if(browser)await browser.close().catch(()=>{});process.exitCode=1;
 }
})();
`

// One actual Meeting uses the original Runner/Tool/Approval graph. Only the
// Provider's eleven replies are controlled: approve A, reject A, then cancel A
// while its original operation awaits approval. B and Summary finish each Turn.
func TestMeetingM3ApprovalOwnerWeb(t *testing.T) {
	browserScript := meetingM3ApprovalBrowserSource(t)
	meetingM3ConfigurationPreflight(t)
	_ = m1RunnerBinary(t) // Fail before acquiring PG if the original binary is absent.
	dist, runtime, evidence, playwright, chromium := os.Getenv("AGENTEAM_MEETING_WEB_DIST"), os.Getenv("AGENTEAM_AUTH_WEB_RUNTIME"), os.Getenv("AGENTEAM_MEETING_WEB_EVIDENCE"), os.Getenv("AGENTEAM_MEETING_WEB_PLAYWRIGHT"), os.Getenv("AGENTEAM_AUTH_WEB_CHROMIUM")
	for _, path := range []string{dist, runtime, evidence, playwright, chromium} {
		if !filepath.IsAbs(path) {
			t.Fatal("exact owned Meeting browser inputs required")
		}
	}
	if len(runtime) > 45 {
		t.Fatal("owned Meeting runtime path too long")
	}
	if info, err := os.Stat(filepath.Join(dist, "index.html")); err != nil || !info.Mode().IsRegular() {
		t.Fatal("frozen Meeting assets absent")
	}
	private, err := os.MkdirTemp(runtime, "ma-")
	workAppCheck(t, "approval-web-private", err)
	t.Cleanup(func() { workAppCheck(t, "approval-web-private-cleanup", os.RemoveAll(private)) })
	evidence = filepath.Join(evidence, t.Name())
	workAppCheck(t, "approval-web-evidence", os.Mkdir(evidence, 0700))
	w, listener := newM1WorkflowWithOptions(t, nil, false)
	v := w.app
	meetingM3VerifyOwnedDatabase(t, v)
	w.seed(t)
	w.startRunner(t)
	model := meetingM3ConfigureModel(t, v, "https://fixture.test:8443/meeting-not-yet-started")
	meetingM3ApprovalAgents(t, w, model)
	args := map[string]any{"mount_id": w.mount.String(), "mode": "argv", "program": "/usr/bin/python3", "args": []string{"meeting-effect.py"}, "cwd": "."}
	request := m1FailureResponse(t, w, tc.RunnerRunCommandKey, args)
	first := string(workAppJSON(t, map[string]string{"title": "Approval collaboration", "goals": "Exercise scoped approval", "decisions": "Owner approved one command", "unresolved": "", "facts": "A and B contributed"}))
	second := string(workAppJSON(t, map[string]string{"goals": "Exercise scoped approval", "decisions": "Owner rejected the second command", "unresolved": "", "facts": "A continued without executing; B contributed"}))
	third := string(workAppJSON(t, map[string]string{"goals": "Exercise scoped approval", "decisions": "Owner cancelled only A", "unresolved": "A did not publish its third response", "facts": "B completed the third turn"}))
	scenario, err := w.network.Create(v.ctx, netfixture.ScenarioConfig{Mode: "openai_chat_wire", WireSequence: []netfixture.WireScenario{
		request, meetingM3Reply(t, 2, "A completed the approved command."), meetingM3Reply(t, 3, "B reviewed the approved contribution."), meetingM3Reply(t, 4, first),
		request, meetingM3Reply(t, 6, "A continued after the rejection without running the command."), meetingM3Reply(t, 7, "B reviewed the rejected contribution."), meetingM3Reply(t, 8, second),
		request, meetingM3Reply(t, 10, "B continued after A was cancelled."), meetingM3Reply(t, 11, third),
	}})
	workAppCheck(t, "approval-web-peer", err)
	defer func() {
		if t.Failed() {
			meetingM3FailureObservation(t, w, scenario)
			meetingM3ApprovalFailure(t, v)
		}
	}()
	meetingM3SetSystemPeer(t, v, scenario)
	v.store.releaseDirectory()

	backend, err := url.Parse(v.address)
	workAppCheck(t, "approval-web-backend", err)
	transport := &http.Transport{Proxy: nil}
	proxy := httputil.NewSingleHostReverseProxy(backend)
	proxy.Transport, proxy.ErrorLog = transport, log.New(io.Discard, "", 0)
	var mu sync.Mutex
	decisions, lookups, controls := 0, 0, 0
	dropped := false
	proxy.ModifyResponse = func(response *http.Response) error {
		if response.Request.Method != http.MethodPost {
			return nil
		}
		path := response.Request.URL.Path
		mu.Lock()
		defer mu.Unlock()
		if strings.HasSuffix(path, "/controls") {
			controls++
			return nil
		}
		if !strings.Contains(path, "/approvals/") {
			return nil
		}
		if strings.HasSuffix(path, "/decision/lookup") {
			lookups++
			return nil
		}
		if !strings.HasSuffix(path, "/decision") {
			return nil
		}
		decisions++
		if dropped {
			return nil
		}
		raw, err := io.ReadAll(io.LimitReader(response.Body, 32769))
		closeErr := response.Body.Close()
		var result struct {
			Receipt gc.Receipt `json:"receipt"`
		}
		valid := err == nil && closeErr == nil && len(raw) <= 32768 && response.StatusCode == http.StatusOK && json.Unmarshal(raw, &result) == nil && result.Receipt.Approval.Validate() == nil && result.Receipt.Approval.Status == gc.Approved
		clear(raw)
		if !valid {
			return errors.New("owned original Approval response invalid")
		}
		dropped = true
		return errors.New("owned committed Approval response delivery interrupted")
	}
	proxy.ErrorHandler = func(w http.ResponseWriter, _ *http.Request, _ error) {
		http.Error(w, "Owned response unavailable", http.StatusBadGateway)
	}
	server := &http.Server{ReadHeaderTimeout: 2 * time.Second, ErrorLog: log.New(io.Discard, "", 0), Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			proxy.ServeHTTP(w, r)
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
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
	var stopOnce sync.Once
	stop := func() {
		stopOnce.Do(func() {
			ctx, cancel := context.WithTimeout(context.WithoutCancel(v.ctx), 3*time.Second)
			defer cancel()
			if err := server.Shutdown(ctx); err != nil {
				t.Error("Approval browser proxy did not join")
				_ = server.Close()
			}
			if err := <-served; err != nil && !errors.Is(err, http.ErrServerClosed) {
				t.Error("Approval browser original Serve failed")
			}
			transport.CloseIdleConnections()
		})
	}
	t.Cleanup(stop)
	session, err := v.request(v.ctx, http.MethodGet, "/api/v1/session", nil, "")
	workAppCheck(t, "approval-web-owner", err)
	var identity struct {
		User struct {
			Username string `json:"username"`
		} `json:"user"`
	}
	if session.status != 200 || json.Unmarshal(session.body, &identity) != nil || identity.User.Username == "" {
		t.Fatal("Approval Owner identity absent")
	}
	raw := workAppJSON(t, map[string]any{"origin": w.origin, "cookie": v.cookie, "project_id": v.a.String(), "route": "/" + identity.User.Username + "/runtime-app-a/meetings", "evidence": evidence})
	workAppCheck(t, "approval-web-input-write", os.WriteFile(filepath.Join(private, "input.json"), raw, 0600))
	clear(raw)
	script := filepath.Join(private, "meeting-approval-web.cjs")
	workAppCheck(t, "approval-web-script", os.WriteFile(script, browserScript, 0600))
	meetingM3RunBrowser(t, v.ctx, private, script, playwright, chromium)
	raw, err = os.ReadFile(filepath.Join(private, "result.json"))
	workAppCheck(t, "approval-web-result", err)
	var result struct {
		Completed bool        `json:"completed"`
		Meeting   c.MeetingID `json:"meeting"`
		Sources   []struct {
			Contribution c.ContributionID `json:"contribution"`
			Execution    id.ExecutionID   `json:"execution"`
			Approval     gc.RequestID     `json:"approval"`
			Operation    tc.OperationID   `json:"operation"`
		} `json:"sources"`
	}
	if len(raw) > 8192 || json.Unmarshal(raw, &result) != nil || !result.Completed || result.Meeting.Validate() != nil || len(result.Sources) != 3 {
		t.Fatal("Approval browser did not complete its exact three turns")
	}
	detail, err := v.runtime.meetings.GetDetail(v.ctx, v.actor, v.a, result.Meeting)
	workAppCheck(t, "approval-web-detail", err)
	if detail.Session.Summary == nil || detail.Session.Summary.Version != 3 || len(detail.Turns) != 3 || len(detail.Decisions) != 0 {
		t.Fatal("Approval final Meeting facts differ")
	}
	for n, source := range result.Sources {
		if source.Operation.Validate() != nil || source.Approval.Validate() != nil {
			t.Fatal("Approval visible source identity absent")
		}
		turn := detail.Turns[n]
		if turn.Status != "completed" || len(turn.Contributions) != 3 {
			t.Fatal("Approval Turn did not finish with both Agents")
		}
		a, b := turn.Contributions[1], turn.Contributions[2]
		if a.ID != source.Contribution || a.ExecutionID == nil || *a.ExecutionID != source.Execution || b.Status != "completed" || b.Message == nil || b.CancelRequested {
			t.Fatal("Approval current source or independent B differs")
		}
		wantA, wantApproval := "completed", gc.Approved
		if n == 1 {
			wantApproval = gc.Rejected
		}
		if n == 2 {
			wantA, wantApproval = "cancelled", gc.Pending
		}
		if a.Status != wantA || a.CancelRequested != (n == 2) || (a.Message == nil) != (n == 2) || (turn.Result == "partial_failure") != (n == 2) {
			t.Fatal("Approval/cancel contribution outcome differs")
		}
		var exact, unattempted bool
		var phase string
		var attempts, tools, approvalResults, approvalVersion, commands, grants int
		err = v.admin.QueryRow(v.ctx, `SELECT r.execution_id=$2 AND r.project_id=$3 AND r.agent_id=$4 AND r.status=$5 AND o.operation_id=r.operation_id AND o.execution_id=r.execution_id AND o.fingerprint=r.fingerprint AND r.operation_id::text=$6,
 (SELECT count(*) FROM agenteam_tool.attempts WHERE operation_id=r.operation_id),
 (SELECT count(*) FROM agenteam_execution.loop_tool_results WHERE execution_id=r.execution_id),
 (SELECT count(*) FROM agenteam_execution.loop_approval_results WHERE execution_id=r.execution_id),o.phase,
 o.phase='created' AND o.version=1 AND o.active_attempt_id IS NULL,r.version,
 (SELECT count(*) FROM agenteam_governance.approval_commands WHERE request_id=r.id),
 (SELECT count(*) FROM agenteam_governance.one_time_approvals WHERE request_id=r.id)
 FROM agenteam_governance.approval_requests r JOIN agenteam_tool.operations o ON o.operation_id=r.operation_id WHERE r.id=$1`, source.Approval.String(), source.Execution.String(), v.a.String(), v.agent.String(), wantApproval, source.Operation.String()).Scan(&exact, &attempts, &tools, &approvalResults, &phase, &unattempted, &approvalVersion, &commands, &grants)
		workAppCheck(t, "approval-web-exact-original-operation", err)
		wantAttempts, wantTools, wantApprovalResults := 0, 0, 0
		if n == 0 {
			wantAttempts, wantTools = 1, 1
		}
		if n == 1 {
			wantApprovalResults = 1
		}
		// Match M2's m2CheckUndispatchedApproval: cancellation before dispatch
		// preserves the original created/v1 reservation and pending approval.
		// The cancelled Execution gate makes that immutable wait non-actionable.
		wantPhase := []string{"succeeded", "failed", "created"}[n]
		if !exact || phase != wantPhase || attempts != wantAttempts || tools != wantTools || approvalResults != wantApprovalResults || n == 2 && (!unattempted || approvalVersion != 1 || commands != 0 || grants != 0) {
			t.Fatalf("Approval original facts differ turn=%d exact=%t phase=%s unattempted=%t attempts=%d tools=%d approvals=%d approval_version=%d commands=%d grants=%d", n, exact, phase, unattempted, attempts, tools, approvalResults, approvalVersion, commands, grants)
		}
	}
	// A pending historical approval remains pending under the original schema;
	// the current cancelled Execution must reject a new human approve command.
	last := result.Sources[2]
	response, err := v.request(v.ctx, http.MethodPost, "/api/v1/projects/"+v.a.String()+"/approvals/"+last.Approval.String()+"/decision", workAppJSON(t, gc.Decision{Decision: "approve", ExpectedVersion: 1}), guardID[struct{}](t).String())
	workAppCheck(t, "approval-web-cancelled-late-approve", err)
	if response.status != http.StatusConflict {
		t.Fatalf("cancelled original Approval accepted a late command status=%d", response.status)
	}
	var executions, succeeded, cancelled, calls, retired, released int
	err = v.admin.QueryRow(v.ctx, `SELECT
 (SELECT count(*) FROM agenteam_execution.executions WHERE project_id=$1),
 (SELECT count(*) FROM agenteam_execution.executions WHERE project_id=$1 AND status='succeeded'),
 (SELECT count(*) FROM agenteam_execution.executions WHERE project_id=$1 AND status='cancelled'),
 (SELECT count(*) FROM agenteam_model.calls WHERE project_id::text=$1),
 (SELECT count(*) FROM agenteam_model.calls WHERE project_id::text=$1 AND retired),
 (SELECT count(*) FROM agenteam_model.calls c JOIN agenteam_secret.secret_leases l ON l.id=c.lease_id WHERE c.project_id::text=$1 AND l.released)`, v.a.String()).Scan(&executions, &succeeded, &cancelled, &calls, &retired, &released)
	workAppCheck(t, "approval-web-durable-counts", err)
	if executions != 6 || succeeded != 5 || cancelled != 1 || calls != 11 || retired != 11 || released != 11 {
		t.Fatalf("Approval counts differ e=%d success=%d cancelled=%d calls=%d retired=%d released=%d", executions, succeeded, cancelled, calls, retired, released)
	}
	effect, err := os.ReadFile(filepath.Join(w.workspace, "meeting-effect.txt"))
	workAppCheck(t, "approval-web-real-runner-effect", err)
	if string(effect) != "approved-once\n" {
		t.Fatal("Runner produced a missing, repeated, rejected or cancelled effect")
	}
	mu.Lock()
	actualDecisions, actualLookups, actualControls, actuallyDropped := decisions, lookups, controls, dropped
	mu.Unlock()
	if actualDecisions != 2 || actualLookups != 1 || actualControls != 1 || !actuallyDropped {
		t.Fatal("Approval browser duplicated or omitted its original commands")
	}
	stop()
	v.runtime.StopAdmission()
	drain, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	err = v.runtime.Drain(drain)
	cancel()
	workAppCheck(t, "approval-web-drain", err)
	w.stopRunner(t)
	v.requestStop()
	usageAppWait(t, v.root.done, "approval-web-original-app-return")
	workAppCheck(t, "approval-web-original-app-exit", v.root.err)
	if !v.runtime.Joined() || !v.assembly.Joined() || !v.root.owned.producersJoined() {
		t.Fatal("Approval browser original owners not joined")
	}
	peer := w.joinedPeer(t, scenario, 11, 11)
	for _, ordinal := range []int{1, 5} {
		var request struct {
			Messages []struct {
				Role       string          `json:"role"`
				ToolCallID string          `json:"tool_call_id"`
				Content    json.RawMessage `json:"content"`
			} `json:"messages"`
		}
		workAppCheck(t, "approval-web-successor-request", json.Unmarshal([]byte(peer.Requests[ordinal].Body), &request))
		found := 0
		for _, message := range request.Messages {
			if message.Role == "tool" && message.ToolCallID == "m1_failure_call" && len(message.Content) > 0 {
				found++
			}
		}
		if found != 1 {
			t.Fatal("Approval outcome did not enter the original successor Model request")
		}
	}
	meetingM3GuardReleased(t, v.workAppFixture)
	t.Log("MEETING_M3_APPROVAL_WEB real_owner_runner_approval=true original_lookup=true rejection_no_attempt=true single_cancel_b_continues=true eleven_model_calls=true original_wait=true external_model=false")
}

// A failed original A is retried with its frozen input, then regenerated from
// current discussion. The real UI must retain history and defer Summary update
// until the next normal Turn. No execution is advanced directly by this test.
func TestMeetingM3ReplacementOwnerWeb(t *testing.T) {
	meetingM3ConfigurationPreflight(t)
	dist, runtime, evidence, playwright, chromium := os.Getenv("AGENTEAM_MEETING_WEB_DIST"), os.Getenv("AGENTEAM_AUTH_WEB_RUNTIME"), os.Getenv("AGENTEAM_MEETING_WEB_EVIDENCE"), os.Getenv("AGENTEAM_MEETING_WEB_PLAYWRIGHT"), os.Getenv("AGENTEAM_AUTH_WEB_CHROMIUM")
	for _, path := range []string{dist, runtime, evidence, playwright, chromium} {
		if !filepath.IsAbs(path) {
			t.Fatal("exact owned replacement browser inputs required")
		}
	}
	if len(runtime) > 45 {
		t.Fatal("owned replacement runtime path too long")
	}
	if info, err := os.Stat(filepath.Join(dist, "index.html")); err != nil || !info.Mode().IsRegular() {
		t.Fatal("frozen replacement assets absent")
	}
	root := os.Getenv("AGENTEAM_FIXTURE_TEST_CWD")
	cwd, err := os.Getwd()
	if err != nil || !filepath.IsAbs(root) || filepath.Clean(root) != root || cwd != root {
		t.Fatal("replacement script requires exact original cwd")
	}
	source := filepath.Join(root, "tests", "meeting", "replacement-owner-web.cjs")
	if info, err := os.Lstat(source); err != nil || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > 65536 {
		t.Fatal("fixed tracked replacement script absent")
	}
	scriptData, err := os.ReadFile(source)
	workAppCheck(t, "replacement-fixed-script", err)
	private, err := os.MkdirTemp(runtime, "mr-")
	workAppCheck(t, "replacement-private", err)
	t.Cleanup(func() { workAppCheck(t, "replacement-private-cleanup", os.RemoveAll(private)) })
	evidence = filepath.Join(evidence, t.Name())
	workAppCheck(t, "replacement-evidence", os.Mkdir(evidence, 0700))
	w, listener := newM1WorkflowWithOptions(t, nil, false)
	v := w.app
	meetingM3VerifyOwnedDatabase(t, v)
	firstSummary := string(workAppJSON(t, map[string]string{"title": "Replacement discussion", "goals": "Original partial discussion", "decisions": "", "unresolved": "A failed", "facts": "B contributed"}))
	secondSummary := string(workAppJSON(t, map[string]string{"goals": "Summary includes the replacement and next turn", "decisions": "", "unresolved": "", "facts": "A and B contributed"}))
	hold := 0
	retried := meetingM3Reply(t, 4, "A retried the original contribution.")
	retried.HoldAfter = &hold
	scenario, err := w.network.Create(v.ctx, netfixture.ScenarioConfig{Mode: "openai_chat_wire", WireSequence: []netfixture.WireScenario{
		{Status: http.StatusBadRequest, Headers: map[string]string{"Content-Type": "application/json"}, Chunks: [][]byte{[]byte(`{"error":{"message":"Owned deterministic rejection","type":"invalid_request_error","code":"invalid_request"}}`)}},
		meetingM3Reply(t, 2, "B kept the original contribution."), meetingM3Reply(t, 3, firstSummary), retried,
		meetingM3Reply(t, 5, "A regenerated with the current discussion."), meetingM3Reply(t, 6, "A completed the next normal turn."), meetingM3Reply(t, 7, "B completed the next normal turn."), meetingM3Reply(t, 8, secondSummary),
	}})
	workAppCheck(t, "replacement-peer", err)
	defer func() {
		if t.Failed() {
			meetingM3FailureObservation(t, w, scenario)
		}
	}()
	model := meetingM3ConfigureModel(t, v, "https://fixture.test:8443/case/"+scenario)
	meetingM3CreateAgents(t, v, model)
	v.store.releaseDirectory()
	backend, err := url.Parse(v.address)
	workAppCheck(t, "replacement-backend", err)
	transport := &http.Transport{Proxy: nil}
	proxy := httputil.NewSingleHostReverseProxy(backend)
	proxy.Transport, proxy.ErrorLog = transport, log.New(io.Discard, "", 0)
	var mu sync.Mutex
	controls, lookups, releases := 0, 0, 0
	dropped := false
	proxy.ModifyResponse = func(response *http.Response) error {
		if response.Request.Method != http.MethodPost {
			return nil
		}
		path := response.Request.URL.Path
		mu.Lock()
		defer mu.Unlock()
		if strings.HasSuffix(path, "/controls/lookup") {
			lookups++
			return nil
		}
		if !strings.HasSuffix(path, "/controls") {
			return nil
		}
		controls++
		if dropped {
			return nil
		}
		raw, e := io.ReadAll(io.LimitReader(response.Body, 8193))
		closed := response.Body.Close()
		var receipt c.Receipt
		valid := e == nil && closed == nil && len(raw) <= 8192 && response.StatusCode == 200 && json.Unmarshal(raw, &receipt) == nil && receipt.MeetingID.Validate() == nil
		clear(raw)
		if !valid {
			return errors.New("owned replacement response invalid")
		}
		dropped = true
		return errors.New("owned committed retry response interrupted")
	}
	proxy.ErrorHandler = func(w http.ResponseWriter, _ *http.Request, _ error) {
		http.Error(w, "Owned response unavailable", http.StatusBadGateway)
	}
	releasePath := "/__meeting_fixture_release/" + guardID[struct{}](t).String()
	server := &http.Server{ReadHeaderTimeout: 2 * time.Second, ErrorLog: log.New(io.Discard, "", 0), Handler: http.HandlerFunc(func(wr http.ResponseWriter, r *http.Request) {
		if r.URL.Path == releasePath {
			cookie, e := r.Cookie("agenteam_local_session")
			if r.Method != http.MethodPost || e != nil || cookie.Value != v.cookie {
				http.NotFound(wr, r)
				return
			}
			mu.Lock()
			defer mu.Unlock()
			ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
			defer cancel()
			state, e := w.network.State(ctx, scenario)
			if releases != 0 || e != nil || len(state.Requests) != 4 || state.ActiveHandlers != 1 || !dropped || lookups != 1 {
				http.Error(wr, "Owned original retry not held", http.StatusConflict)
				return
			}
			if e = w.network.Release(ctx, scenario); e != nil {
				http.Error(wr, "Owned peer release failed", http.StatusBadGateway)
				return
			}
			releases++
			wr.WriteHeader(http.StatusNoContent)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/api/") {
			proxy.ServeHTTP(wr, r)
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.NotFound(wr, r)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/assets/") {
			skillWebServeAsset(wr, r, dist)
			return
		}
		http.ServeFile(wr, r, filepath.Join(dist, "index.html"))
	})}
	served := make(chan error, 1)
	go func() { served <- server.Serve(listener) }()
	var stopOnce sync.Once
	stop := func() {
		stopOnce.Do(func() {
			ctx, cancel := context.WithTimeout(context.WithoutCancel(v.ctx), 3*time.Second)
			defer cancel()
			if e := server.Shutdown(ctx); e != nil {
				t.Error("replacement proxy did not join")
				_ = server.Close()
			}
			if e := <-served; e != nil && !errors.Is(e, http.ErrServerClosed) {
				t.Error("replacement original Serve failed")
			}
			transport.CloseIdleConnections()
		})
	}
	t.Cleanup(stop)
	session, err := v.request(v.ctx, http.MethodGet, "/api/v1/session", nil, "")
	workAppCheck(t, "replacement-owner", err)
	var identity struct {
		User struct {
			Username string `json:"username"`
		} `json:"user"`
	}
	if session.status != 200 || json.Unmarshal(session.body, &identity) != nil || identity.User.Username == "" {
		t.Fatal("replacement Owner absent")
	}
	raw := workAppJSON(t, map[string]any{"origin": w.origin, "cookie": v.cookie, "project_id": v.a.String(), "route": "/" + identity.User.Username + "/runtime-app-a/meetings", "evidence": evidence, "release_path": releasePath})
	workAppCheck(t, "replacement-input", os.WriteFile(filepath.Join(private, "input.json"), raw, 0600))
	clear(raw)
	script := filepath.Join(private, "meeting-replacement-web.cjs")
	workAppCheck(t, "replacement-script", os.WriteFile(script, scriptData, 0600))
	meetingM3RunBrowser(t, v.ctx, private, script, playwright, chromium)
	raw, err = os.ReadFile(filepath.Join(private, "result.json"))
	workAppCheck(t, "replacement-result", err)
	var result struct {
		Completed bool   `json:"completed"`
		Meeting   string `json:"meeting"`
	}
	if len(raw) > 4096 || json.Unmarshal(raw, &result) != nil || !result.Completed {
		t.Fatal("replacement browser incomplete")
	}
	meetingID, err := f.ParseID[c.Meeting](result.Meeting)
	workAppCheck(t, "replacement-meeting", err)
	detail, err := v.runtime.meetings.GetDetail(v.ctx, v.actor, v.a, meetingID)
	workAppCheck(t, "replacement-detail", err)
	if detail.Session.Summary == nil || detail.Session.Summary.Version != 2 || detail.Session.Summary.PendingUpdate || len(detail.Turns) != 2 || len(detail.Turns[0].Contributions) != 3 || detail.Turns[0].Status != "completed" || detail.Turns[1].Status != "completed" {
		t.Fatal("replacement final Meeting differs")
	}
	a, b := detail.Turns[0].Contributions[1], detail.Turns[0].Contributions[2]
	if a.Generation != 2 || a.Attempt != 1 || a.Status != "completed" || a.ExecutionID == nil || a.Message == nil || len(a.History) != 3 || b.Generation != 1 || b.Attempt != 1 || len(b.History) != 1 {
		t.Fatal("replacement lineage or unaffected B differs")
	}
	rows, err := v.admin.Query(v.ctx, `SELECT input_id,execution_id,request_data FROM agenteam_meeting.launch_intents WHERE contribution_id=$1 ORDER BY generation,attempt`, a.ID.String())
	workAppCheck(t, "replacement-original-intents", err)
	var inputs, executions []string
	var requests []ec.LaunchRequest
	for rows.Next() {
		var input, execution string
		var data []byte
		var request ec.LaunchRequest
		if e := rows.Scan(&input, &execution, &data); e != nil {
			rows.Close()
			workAppCheck(t, "replacement-intent-row", e)
		}
		if e := json.Unmarshal(data, &request); e != nil {
			rows.Close()
			workAppCheck(t, "replacement-intent-data", e)
		}
		inputs = append(inputs, input)
		executions = append(executions, execution)
		requests = append(requests, request)
	}
	err = rows.Err()
	rows.Close()
	workAppCheck(t, "replacement-original-intent-tail", err)
	if len(requests) != 3 || inputs[0] != inputs[1] || inputs[1] == inputs[2] || requests[1].Lineage.RetryOf == nil || requests[1].Lineage.RetryOf.String() != executions[0] || requests[2].Lineage.RegenerateOf == nil || requests[2].Lineage.RegenerateOf.String() != executions[1] || a.ExecutionID.String() != executions[2] {
		t.Fatal("retry frozen input or regenerate lineage changed")
	}
	var failed, succeeded, calls, retired, leases int
	err = v.admin.QueryRow(v.ctx, `SELECT (SELECT count(*) FROM agenteam_execution.executions WHERE project_id=$1 AND status='failed'),(SELECT count(*) FROM agenteam_execution.executions WHERE project_id=$1 AND status='succeeded'),count(*),count(*) FILTER(WHERE c.retired),count(*) FILTER(WHERE l.released) FROM agenteam_model.calls c JOIN agenteam_secret.secret_leases l ON l.id=c.lease_id WHERE c.project_id::text=$1`, v.a.String()).Scan(&failed, &succeeded, &calls, &retired, &leases)
	workAppCheck(t, "replacement-original-terminal-facts", err)
	if failed != 1 || succeeded != 5 || calls != 8 || retired != 8 || leases != 8 {
		t.Fatalf("replacement facts failed=%d succeeded=%d calls=%d retired=%d leases=%d", failed, succeeded, calls, retired, leases)
	}
	mu.Lock()
	exact := dropped && controls == 2 && lookups == 1 && releases == 1
	mu.Unlock()
	if !exact {
		t.Fatal("replacement original command or peer release duplicated")
	}
	stop()
	v.runtime.StopAdmission()
	drain, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	err = v.runtime.Drain(drain)
	cancel()
	workAppCheck(t, "replacement-runtime-drain", err)
	v.requestStop()
	usageAppWait(t, v.root.done, "replacement-original-app-wait")
	workAppCheck(t, "replacement-original-app-exit", v.root.err)
	if !v.runtime.Joined() || !v.runtime.meetingRuntime.Joined() || !v.assembly.Joined() || !v.root.owned.producersJoined() {
		t.Fatal("replacement original owners not joined")
	}
	w.joinedPeer(t, scenario, 8, 8)
	meetingM3GuardReleased(t, v.workAppFixture)
	t.Log("MEETING_M3_REPLACEMENT_WEB owner_retry_regenerate=true original_lookup=true frozen_retry_input=true new_generation_context=true historical_replies=true summary_deferred=true original_app_wait=true external_model=false")
}

// Metadata changes use the real Owner HTTP commands and are captured only by
// later executions. Typed references must pass the original Task, Knowledge
// and Artifact Owner identity ports without reading their resource bodies.
func TestMeetingM3MetadataOwnerWeb(t *testing.T) {
	meetingM3ConfigurationPreflight(t)
	dist, runtime, evidence, playwright, chromium := os.Getenv("AGENTEAM_MEETING_WEB_DIST"), os.Getenv("AGENTEAM_AUTH_WEB_RUNTIME"), os.Getenv("AGENTEAM_MEETING_WEB_EVIDENCE"), os.Getenv("AGENTEAM_MEETING_WEB_PLAYWRIGHT"), os.Getenv("AGENTEAM_AUTH_WEB_CHROMIUM")
	for _, path := range []string{dist, runtime, evidence, playwright, chromium} {
		if !filepath.IsAbs(path) {
			t.Fatal("exact owned metadata browser inputs required")
		}
	}
	if len(runtime) > 45 {
		t.Fatal("owned metadata runtime path too long")
	}
	if info, err := os.Stat(filepath.Join(dist, "index.html")); err != nil || !info.Mode().IsRegular() {
		t.Fatal("frozen metadata assets absent")
	}
	root := os.Getenv("AGENTEAM_FIXTURE_TEST_CWD")
	cwd, err := os.Getwd()
	if err != nil || !filepath.IsAbs(root) || filepath.Clean(root) != root || cwd != root {
		t.Fatal("metadata script requires exact original cwd")
	}
	source := filepath.Join(root, "tests", "meeting", "metadata-owner-web.cjs")
	if info, err := os.Lstat(source); err != nil || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > 65536 {
		t.Fatal("fixed tracked metadata script absent")
	}
	scriptData, err := os.ReadFile(source)
	workAppCheck(t, "metadata-fixed-script", err)
	private, err := os.MkdirTemp(runtime, "mm-")
	workAppCheck(t, "metadata-private", err)
	t.Cleanup(func() { workAppCheck(t, "metadata-private-cleanup", os.RemoveAll(private)) })
	evidence = filepath.Join(evidence, t.Name())
	workAppCheck(t, "metadata-evidence", os.Mkdir(evidence, 0700))
	w, listener := newM1WorkflowWithOptions(t, nil, false)
	v := w.app
	meetingM3VerifyOwnedDatabase(t, v)
	summary := func(n int) string {
		value := map[string]string{"goals": "Discuss the current plan", "decisions": "", "unresolved": "", "facts": "Participant and reference changes affect later turns"}
		if n == 1 {
			value["title"] = "Metadata discussion"
		}
		return string(workAppJSON(t, value))
	}
	scenario, err := w.network.Create(v.ctx, netfixture.ScenarioConfig{Mode: "openai_chat_wire", WireSequence: []netfixture.WireScenario{
		meetingM3Reply(t, 1, "A joined the original discussion."), meetingM3Reply(t, 2, "B joined the original discussion."), meetingM3Reply(t, 3, summary(1)),
		meetingM3Reply(t, 4, "A continued with the pinned references."), meetingM3Reply(t, 5, summary(2)),
		meetingM3Reply(t, 6, "B rejoined before A."), meetingM3Reply(t, 7, "A followed the revised order."), meetingM3Reply(t, 8, summary(3)),
	}})
	workAppCheck(t, "metadata-peer", err)
	defer func() {
		if t.Failed() {
			meetingM3FailureObservation(t, w, scenario)
		}
	}()
	model := meetingM3ConfigureModel(t, v, "https://fixture.test:8443/case/"+scenario)
	meetingM3CreateAgents(t, v, model)
	typedReferences := meetingM3MetadataResources(t, v)
	v.store.releaseDirectory()
	backend, err := url.Parse(v.address)
	workAppCheck(t, "metadata-backend", err)
	transport := &http.Transport{Proxy: nil}
	proxy := httputil.NewSingleHostReverseProxy(backend)
	proxy.Transport, proxy.ErrorLog = transport, log.New(io.Discard, "", 0)
	var mu sync.Mutex
	updates, lookups, references, contentReads := 0, 0, 0, 0
	dropped := false
	proxy.ModifyResponse = func(response *http.Response) error {
		if response.Request.Method == http.MethodGet && strings.HasSuffix(response.Request.URL.Path, "/content") {
			mu.Lock()
			contentReads++
			mu.Unlock()
		}
		if response.Request.Method != http.MethodPost {
			return nil
		}
		path := response.Request.URL.Path
		mu.Lock()
		defer mu.Unlock()
		if strings.HasSuffix(path, "/references") {
			references++
			return nil
		}
		if strings.HasSuffix(path, "/participants/lookup") {
			lookups++
			return nil
		}
		if !strings.HasSuffix(path, "/participants") {
			return nil
		}
		updates++
		if dropped {
			return nil
		}
		raw, e := io.ReadAll(io.LimitReader(response.Body, 8193))
		closed := response.Body.Close()
		var receipt c.Receipt
		valid := e == nil && closed == nil && len(raw) <= 8192 && response.StatusCode == 200 && json.Unmarshal(raw, &receipt) == nil && receipt.MeetingID.Validate() == nil
		clear(raw)
		if !valid {
			return errors.New("owned metadata response invalid")
		}
		dropped = true
		return errors.New("owned committed roster response interrupted")
	}
	proxy.ErrorHandler = func(w http.ResponseWriter, _ *http.Request, _ error) {
		http.Error(w, "Owned response unavailable", http.StatusBadGateway)
	}
	server := &http.Server{ReadHeaderTimeout: 2 * time.Second, ErrorLog: log.New(io.Discard, "", 0), Handler: http.HandlerFunc(func(wr http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			proxy.ServeHTTP(wr, r)
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.NotFound(wr, r)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/assets/") {
			skillWebServeAsset(wr, r, dist)
			return
		}
		http.ServeFile(wr, r, filepath.Join(dist, "index.html"))
	})}
	served := make(chan error, 1)
	go func() { served <- server.Serve(listener) }()
	var stopOnce sync.Once
	stop := func() {
		stopOnce.Do(func() {
			ctx, cancel := context.WithTimeout(context.WithoutCancel(v.ctx), 3*time.Second)
			defer cancel()
			if e := server.Shutdown(ctx); e != nil {
				t.Error("metadata proxy did not join")
				_ = server.Close()
			}
			if e := <-served; e != nil && !errors.Is(e, http.ErrServerClosed) {
				t.Error("metadata original Serve failed")
			}
			transport.CloseIdleConnections()
		})
	}
	t.Cleanup(stop)
	session, err := v.request(v.ctx, http.MethodGet, "/api/v1/session", nil, "")
	workAppCheck(t, "metadata-owner", err)
	var identity struct {
		User struct {
			Username string `json:"username"`
		} `json:"user"`
	}
	if session.status != 200 || json.Unmarshal(session.body, &identity) != nil || identity.User.Username == "" {
		t.Fatal("metadata Owner absent")
	}
	raw := workAppJSON(t, map[string]any{"origin": w.origin, "cookie": v.cookie, "project_id": v.a.String(), "route": "/" + identity.User.Username + "/runtime-app-a/meetings", "evidence": evidence})
	workAppCheck(t, "metadata-input", os.WriteFile(filepath.Join(private, "input.json"), raw, 0600))
	clear(raw)
	script := filepath.Join(private, "meeting-metadata-web.cjs")
	workAppCheck(t, "metadata-script", os.WriteFile(script, scriptData, 0600))
	meetingM3RunBrowser(t, v.ctx, private, script, playwright, chromium)
	raw, err = os.ReadFile(filepath.Join(private, "result.json"))
	workAppCheck(t, "metadata-result", err)
	var result struct {
		Completed bool   `json:"completed"`
		Meeting   string `json:"meeting"`
	}
	if len(raw) > 4096 || json.Unmarshal(raw, &result) != nil || !result.Completed {
		t.Fatal("metadata browser incomplete")
	}
	meetingID, err := f.ParseID[c.Meeting](result.Meeting)
	workAppCheck(t, "metadata-meeting", err)
	detail, err := v.runtime.meetings.GetDetail(v.ctx, v.actor, v.a, meetingID)
	workAppCheck(t, "metadata-detail", err)
	if len(detail.Turns) != 3 || detail.Session.Summary == nil || detail.Session.Summary.Version != 3 || len(detail.References) != 0 || len(detail.Participants) != 4 {
		t.Fatal("metadata final Meeting differs")
	}
	for n, length := range []int{3, 2, 3} {
		if len(detail.Turns[n].Contributions) != length || detail.Turns[n].Status != "completed" {
			t.Fatal("metadata Turn participants differ", n)
		}
	}
	a, oldB, newB := detail.Turns[0].Contributions[1].ParticipantID, detail.Turns[0].Contributions[2].ParticipantID, detail.Turns[2].Contributions[1].ParticipantID
	if a != detail.Turns[1].Contributions[1].ParticipantID || a != detail.Turns[2].Contributions[2].ParticipantID || oldB == newB {
		t.Fatal("metadata historical participant identity changed")
	}
	var originalB, rejoinedB c.ParticipantView
	for _, p := range detail.Participants {
		if p.ID == oldB {
			originalB = p
		}
		if p.ID == newB {
			rejoinedB = p
		}
	}
	if originalB.Active || !rejoinedB.Active || originalB.SourceID == "" || originalB.SourceID != rejoinedB.SourceID || rejoinedB.Order != 1 {
		t.Fatal("metadata rejoin did not preserve historical identity")
	}
	for n, turn := range detail.Turns {
		for _, contribution := range turn.Contributions[1:] {
			var data []byte
			err = v.admin.QueryRow(v.ctx, `SELECT i.input_data FROM agenteam_meeting.launch_intents l JOIN agenteam_meeting.inputs i ON i.id=l.input_id WHERE l.contribution_id=$1 AND l.generation=1 AND l.attempt=1`, contribution.ID.String()).Scan(&data)
			workAppCheck(t, "metadata-frozen-input", err)
			var frozen meeting.FrozenContext
			workAppCheck(t, "metadata-frozen-context", json.Unmarshal(data, &frozen))
			workAppCheck(t, "metadata-frozen-context-shape", frozen.Validate())
			active := []c.ParticipantID{}
			for _, p := range frozen.Participants {
				if p.Active && p.Kind == "agent" {
					active = append(active, p.ID)
				}
			}
			if len(active) != len(turn.Contributions)-1 {
				t.Fatal("metadata later roster changed an original context", n)
			}
			for k, participant := range active {
				if participant != turn.Contributions[k+1].ParticipantID {
					t.Fatal("metadata context order differs from captured Turn", n)
				}
			}
			want := []c.Reference{}
			if n > 0 {
				if n == 1 {
					want = append(want, c.Reference{Kind: "link", ID: "https://example.com/design"})
				}
				want = append(want, c.Reference{Kind: "link", ID: "https://example.com/notes"})
				want = append(want, typedReferences...)
			}
			if len(frozen.References) != len(want) {
				t.Fatal("metadata later reference edit changed original input", n)
			}
			for k, ref := range frozen.References {
				if ref.Kind != want[k].Kind || ref.ID != want[k].ID {
					t.Fatal("metadata original reference order differs", n)
				}
			}
			if bytes.Contains(data, []byte("MEETING_REFERENCE_PRIVATE_BODY")) {
				t.Fatal("metadata identity selection injected referenced content")
			}
		}
	}
	var executions, succeeded, calls, retired, leases int
	err = v.admin.QueryRow(v.ctx, `SELECT (SELECT count(*) FROM agenteam_execution.executions WHERE project_id=$1),(SELECT count(*) FROM agenteam_execution.executions WHERE project_id=$1 AND status='succeeded'),count(*),count(*) FILTER(WHERE c.retired),count(*) FILTER(WHERE l.released) FROM agenteam_model.calls c JOIN agenteam_secret.secret_leases l ON l.id=c.lease_id WHERE c.project_id::text=$1`, v.a.String()).Scan(&executions, &succeeded, &calls, &retired, &leases)
	workAppCheck(t, "metadata-original-terminal-facts", err)
	if executions != 5 || succeeded != 5 || calls != 8 || retired != 8 || leases != 8 {
		t.Fatalf("metadata facts executions=%d succeeded=%d calls=%d retired=%d leases=%d", executions, succeeded, calls, retired, leases)
	}
	mu.Lock()
	exact := dropped && updates == 2 && lookups == 1 && references == 3 && contentReads == 0
	mu.Unlock()
	if !exact {
		t.Fatal("metadata original commands duplicated")
	}
	stop()
	v.runtime.StopAdmission()
	drain, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	err = v.runtime.Drain(drain)
	cancel()
	workAppCheck(t, "metadata-runtime-drain", err)
	v.requestStop()
	usageAppWait(t, v.root.done, "metadata-original-app-wait")
	workAppCheck(t, "metadata-original-app-exit", v.root.err)
	if !v.runtime.Joined() || !v.runtime.meetingRuntime.Joined() || !v.assembly.Joined() || !v.root.owned.producersJoined() {
		t.Fatal("metadata original owners not joined")
	}
	w.joinedPeer(t, scenario, 8, 8)
	meetingM3GuardReleased(t, v.workAppFixture)
	t.Log("MEETING_M3_METADATA_WEB owner_roster=true original_lookup=true rejoin_identity=true pinned_links=true typed_owner_references=true stable_artifact_identity=true historical_context=true zero_reference_content_read=true original_app_wait=true external_model=false")
}

// Original installed Human services publish the three resource identities.
// No private-table seed, Agent authority or body read substitutes for them.
func meetingM3MetadataResources(t *testing.T, v *executionSchedulerApp) []c.Reference {
	t.Helper()
	milestone := wc.CreateMilestoneRequest{MilestoneID: guardID[wc.Milestone](t), Title: "Meeting reference milestone"}
	var structure wc.StructureMutation
	v.httpSeed(t, "/milestones", map[string]any{"request": milestone}, &structure)
	if structure.Validate() != nil || structure.Milestone == nil || structure.Milestone.ID != milestone.MilestoneID {
		t.Fatal("metadata milestone receipt differs")
	}
	sprint := wc.CreateSprintRequest{SprintID: guardID[pc.Sprint](t), MilestoneID: milestone.MilestoneID, Title: "Meeting reference sprint"}
	v.httpSeed(t, "/sprints", map[string]any{"request": sprint}, &structure)
	if structure.Validate() != nil || structure.Sprint == nil || structure.Sprint.ID != sprint.SprintID {
		t.Fatal("metadata sprint receipt differs")
	}
	create := wc.TaskCreate{TaskID: guardID[wc.Task](t), SprintID: sprint.SprintID, Title: "Meeting referenced Task", Type: wc.TaskTypeTask, Priority: wc.TaskPriorityMedium, Plan: "MEETING_REFERENCE_PRIVATE_BODY task plan"}
	var created wc.TaskMutation
	v.httpSeed(t, "/tasks", map[string]any{"request": create}, &created)
	if created.Validate() != nil || created.Task.ID != create.TaskID || created.Task.State != wc.TaskStateBacklog || created.Task.AssigneeAgentID != nil {
		t.Fatal("metadata task identity was not an unassigned original Task")
	}
	v.assembly.mu.Lock()
	documents, ok := v.assembly.knowledge.(*knowledgeCallAssembly)
	artifacts := v.assembly.artifacts
	v.assembly.mu.Unlock()
	if !ok || documents == nil || documents.pair == nil || artifacts == nil || artifacts.pair == nil {
		t.Fatal("metadata original resource services absent")
	}
	source, err := kc.NewTextSource(kc.PlainText, "MEETING_REFERENCE_PRIVATE_BODY document text")
	workAppCheck(t, "metadata-document-source", err)
	document, err := documents.pair.Service().CreateDocument(v.ctx, v.actor, executionSchedulerAppMeta(t, nil), kc.CreateRequest{ProjectID: v.a, DocumentID: guardID[kc.Document](t), Title: "Meeting referenced Document"}, source)
	workAppCheck(t, "metadata-original-document", err)
	invocation, err := artifactc.NewInvocation(artifactc.InvocationDetails{Actor: v.actor, ProjectID: v.a, AttemptID: guardID[artifactc.CallAttempt](t)})
	workAppCheck(t, "metadata-original-artifact-invocation", err)
	file, err := artifacts.pair.Service().CreateFromContent(v.ctx, invocation, humanArtifactMeta(t), artifactc.Display{Name: "meeting-referenced-file.txt"}, "text/plain", "MEETING_REFERENCE_PRIVATE_BODY artifact text")
	workAppCheck(t, "metadata-original-artifact", err)
	ref := file.Details().Reference
	if file.Validate() != nil || ref.ProjectID != v.a || ref.ArtifactID.String() == ref.FileID.String() || ref.ArtifactID.Validate() != nil || ref.FileID.Validate() != nil {
		t.Fatal("metadata Artifact did not publish distinct stable and file identities")
	}
	return []c.Reference{{Kind: "task", ID: created.Task.ID.String(), Label: created.Task.Title}, {Kind: "knowledge", ID: document.ID.String(), Label: "Meeting referenced Document"}, {Kind: "file", ID: ref.ArtifactID.String(), Label: file.Details().Name}}
}

func meetingM3ApprovalAgents(t *testing.T, w *m1Workflow, model mc.ModelID) {
	t.Helper()
	v := w.app
	disabled := false
	for n, name := range []string{"meeting-approval-agent", "meeting-continuing-agent"} {
		target := guardID[id.Agent](t)
		fields := ac.AgentCreateFields{AgentID: target, Name: name, Description: "Meeting scoped approval participant", Instructions: "Contribute to the current meeting. Continue after a rejected operation without retrying it.", ModelRef: model, ApprovalPolicy: ac.ApprovalAllow, AllowedToolIDs: []id.ToolID{}, AllowedMountIDs: []id.MountID{}, AllowedSecretVariableIDs: []id.ProjectVariableID{}, AddSkillsEnabled: &disabled, InstallSkillEnabled: &disabled, InjectAgentsMD: false}
		if n == 0 {
			v.agent = target
			fields.ApprovalPolicy = ac.ApprovalDefault
			fields.AllowedToolIDs = []id.ToolID{w.tools[tc.RunnerRunCommandKey]}
			mounts := []ac.RunnerMountDeclaration{{RunnerID: w.runnerID, Workspace: "source"}}
			fields.RunnerMounts, fields.AllowedMountIDs = &mounts, nil
		}
		request, err := ac.NewAgentCreate(fields)
		workAppCheck(t, "approval-web-agent-intent", err)
		created, err := v.assembly.agents.service.CreateAgent(v.ctx, v.actor, executionSchedulerAppMeta(t, nil), v.a, request)
		workAppCheck(t, "approval-web-agent-create", err)
		if n == 0 {
			mounts := created.Fields().RunnerMounts
			if mounts == nil || len(*mounts) != 1 {
				t.Fatal("Approval original mount absent")
			}
			w.mount = (*mounts)[0].MountID
			w.workspace = filepath.Join(w.root, v.a.String(), target.String(), (*mounts)[0].Workspace)
			workAppCheck(t, "approval-web-workspace", os.MkdirAll(w.workspace, 0700))
			workAppCheck(t, "approval-web-owned-program", os.WriteFile(filepath.Join(w.workspace, "meeting-effect.py"), []byte("from pathlib import Path\nwith Path('meeting-effect.txt').open('a') as output:\n    output.write('approved-once\\n')\n"), 0600))
		}
	}
}

func meetingM3SetSystemPeer(t *testing.T, v *executionSchedulerApp, scenario string) {
	t.Helper()
	var page struct {
		Items []struct {
			ID      mc.ProviderID              `json:"id"`
			Version f.Version                  `json:"version"`
			Input   map[string]json.RawMessage `json:"input"`
		} `json:"items"`
	}
	meetingM3SystemCall(t, v, http.MethodGet, "model-providers", nil, &page)
	matched := 0
	for _, provider := range page.Items {
		var name string
		workAppCheck(t, "approval-web-provider-name", json.Unmarshal(provider.Input["name"], &name))
		if name != "Meeting fixture Provider" {
			continue
		}
		matched++
		provider.Input["base_url"] = workAppJSON(t, "https://fixture.test:8443/case/"+scenario)
		meetingM3SystemCall(t, v, http.MethodPut, "model-providers/"+provider.ID.String(), map[string]any{"expected_version": provider.Version, "input": provider.Input}, nil)
	}
	if matched != 1 {
		t.Fatal("Approval original Provider identity differs")
	}
}

// The one tracked script is a test input, read once before allocating fixtures.
// Keeping it outside the binary permits safe diagnostic changes without
// rebuilding an otherwise unchanged App or substituting a generated script.
func meetingM3ApprovalBrowserSource(t *testing.T) []byte {
	t.Helper()
	root := os.Getenv("AGENTEAM_FIXTURE_TEST_CWD")
	cwd, err := os.Getwd()
	if err != nil || !filepath.IsAbs(root) || filepath.Clean(root) != root || root != cwd {
		t.Fatal("Approval script requires the exact original fixture test cwd")
	}
	path := filepath.Join(root, "tests", "meeting", "approval-owner-web.cjs")
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > 65536 {
		t.Fatal("Approval script must be the fixed tracked regular test input")
	}
	data, err := os.ReadFile(path)
	workAppCheck(t, "approval-fixed-script-read", err)
	return data
}

func meetingM3ApprovalFailure(t *testing.T, v *executionSchedulerApp) {
	t.Helper()
	// Report original waiting locations without arguments, SQL or credentials.
	stack := make([]byte, 1<<20)
	stack = stack[:goruntime.Stack(stack, true)]
	for _, block := range strings.Split(string(stack), "\n\n") {
		if !strings.Contains(block, ".changeApprovalWaiting(") {
			continue
		}
		printed := 0
		for _, line := range strings.Split(block, "\n") {
			if line == "" || line[0] == '\t' || strings.HasPrefix(line, "created by ") || strings.HasPrefix(line, "goroutine ") {
				continue
			}
			if end := strings.LastIndex(line, "("); end > 0 && printed < 24 {
				t.Logf("MEETING_APPROVAL_WAIT_FRAME function=%s", line[:end])
				printed++
			}
		}
	}
	clear(stack)
	ctx, cancel := context.WithTimeout(v.ctx, 2*time.Second)
	defer cancel()
	rows, readErr := v.admin.Query(ctx, `SELECT pid,state,COALESCE(wait_event_type,''),COALESCE(wait_event,''),COALESCE(array_length(pg_blocking_pids(pid),1),0) FROM pg_stat_activity WHERE datname=current_database() AND application_name='agenteam' ORDER BY pid LIMIT 32`)
	if readErr == nil {
		for rows.Next() {
			var pid, blockers int
			var state, kind, event string
			if rows.Scan(&pid, &state, &kind, &event, &blockers) == nil {
				t.Logf("MEETING_APPROVAL_DB_WAIT pid=%d state=%s kind=%s event=%s blockers=%d", pid, state, kind, event, blockers)
			}
		}
		rows.Close()
	}
	var waiting, failed, succeeded, cancelled, pending, approved, rejected, attempts, cancelledOperations int
	err := v.admin.QueryRow(ctx, `SELECT
 (SELECT count(*) FROM agenteam_execution.executions WHERE project_id=$1 AND status='waiting'),
 (SELECT count(*) FROM agenteam_execution.executions WHERE project_id=$1 AND status='failed'),
 (SELECT count(*) FROM agenteam_execution.executions WHERE project_id=$1 AND status='succeeded'),
 (SELECT count(*) FROM agenteam_execution.executions WHERE project_id=$1 AND status='cancelled'),
 (SELECT count(*) FROM agenteam_governance.approval_requests WHERE project_id=$1 AND status='pending'),
 (SELECT count(*) FROM agenteam_governance.approval_requests WHERE project_id=$1 AND status='approved'),
 (SELECT count(*) FROM agenteam_governance.approval_requests WHERE project_id=$1 AND status='rejected'),
 (SELECT count(*) FROM agenteam_tool.attempts WHERE project_id=$1),
 (SELECT count(*) FROM agenteam_tool.operations WHERE project_id=$1 AND phase='cancelled')`, v.a.String()).Scan(&waiting, &failed, &succeeded, &cancelled, &pending, &approved, &rejected, &attempts, &cancelledOperations)
	if err != nil {
		meetingM3SafeError(t, "approval-web-failure-observation", err)
		return
	}
	t.Logf("MEETING_APPROVAL_FACTS waiting=%d failed=%d success=%d cancelled=%d pending=%d approved=%d rejected=%d attempts=%d cancelled_operations=%d", waiting, failed, succeeded, cancelled, pending, approved, rejected, attempts, cancelledOperations)
}
