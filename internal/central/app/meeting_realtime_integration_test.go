//go:build integration && linux

package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
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

	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	mt "github.com/LunaDeerTech/agenteam/internal/central/meeting/contract"
	rt "github.com/LunaDeerTech/agenteam/internal/central/realtime/contract"
	netfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/outbound"
)

// This uses the actual App/Session/Meeting/Execution readers and WebSocket
// transport. Only the remote Model peer is controlled. No fake Authority,
// injected WebSocket frame, polling substitute, or token delta is involved.
func TestMeetingM3AppRealtimeOwnerWeb(t *testing.T) {
	meetingM3ConfigurationPreflight(t)
	dist, harness, evidenceRoot, chromium := os.Getenv("AGENTEAM_MEETING_WEB_DIST"), os.Getenv("AGENTEAM_MEETING_REALTIME_HARNESS"), os.Getenv("AGENTEAM_MEETING_WEB_EVIDENCE"), os.Getenv("AGENTEAM_AUTH_WEB_CHROMIUM")
	for _, path := range []string{dist, harness, evidenceRoot, chromium} {
		if !filepath.IsAbs(path) {
			t.Fatal("owned Realtime browser paths required")
		}
	}
	for _, path := range []string{filepath.Join(dist, "index.html"), filepath.Join(harness, "meeting-realtime.config.js"), filepath.Join(harness, "e2e", "meeting-realtime.spec.ts"), filepath.Join(harness, "node_modules/@playwright/test/cli.js"), chromium} {
		if info, err := os.Stat(path); err != nil || !info.Mode().IsRegular() {
			t.Fatal("fixed Realtime browser source/assets/executable missing")
		}
	}
	manifest, err := os.ReadFile(filepath.Join(harness, "node_modules/@playwright/test/package.json"))
	workAppCheck(t, "realtime-locked-manifest", err)
	var locked struct{ Version string }
	if json.Unmarshal(manifest, &locked) != nil || locked.Version != "1.56.1" {
		t.Fatal("Realtime browser requires locked Playwright 1.56.1")
	}
	private, err := os.MkdirTemp("/tmp", "agenteam-meeting-rt-")
	workAppCheck(t, "realtime-private", err)
	t.Cleanup(func() { workAppCheck(t, "realtime-private-cleanup", os.RemoveAll(private)) })
	evidence := filepath.Join(evidenceRoot, t.Name())
	workAppCheck(t, "realtime-evidence", os.Mkdir(evidence, 0700))
	w, listener := newM1WorkflowWithOptions(t, nil, false)
	v := w.app
	meetingM3VerifyOwnedDatabase(t, v)
	var tool string
	workAppCheck(t, "realtime-decision-tool", v.admin.QueryRow(v.ctx, `SELECT tool_id FROM agenteam_tool.identities WHERE stable_key='builtin:request-decision'`).Scan(&tool))
	const prompt = "Ask me which calibration to use, then record my answer."
	const question = "Which calibration should this original contribution use?"
	const answer = "Use the smaller calibration."
	const final = "The original contribution used the smaller calibration."
	const title = "Realtime calibration decision"
	args := string(workAppJSON(t, map[string]any{"question": question, "options": []string{"smaller", "larger"}}))
	first := workAppJSON(t, map[string]any{"id": "owned-realtime-decision", "object": "chat.completion", "created": 1, "model": "meeting-collaboration", "choices": []any{map[string]any{"index": 0, "message": map[string]any{"role": "assistant", "content": nil, "tool_calls": []any{map[string]any{"id": "meeting_realtime_decision", "type": "function", "function": map[string]any{"name": "tool_" + strings.ReplaceAll(tool, "-", ""), "arguments": args}}}}, "finish_reason": "tool_calls"}}, "usage": map[string]any{"prompt_tokens": 10, "completion_tokens": 10, "total_tokens": 20}})
	hold := 0
	summary := string(workAppJSON(t, map[string]string{"title": title, "goals": "Choose the original calibration", "decisions": answer, "unresolved": "", "facts": "One original contribution completed"}))
	scenario, err := w.network.Create(v.ctx, netfixture.ScenarioConfig{Mode: "openai_chat_wire", WireSequence: []netfixture.WireScenario{
		{Status: http.StatusOK, Headers: map[string]string{"Content-Type": "application/json"}, Chunks: [][]byte{first}, HoldAfter: &hold},
		meetingM3Reply(t, 2, final), meetingM3Reply(t, 3, summary),
	}})
	workAppCheck(t, "realtime-owned-peer", err)
	defer func() {
		if t.Failed() {
			meetingM3FailureObservation(t, w, scenario)
		}
	}()
	model := meetingM3ConfigureModel(t, v, "https://fixture.test:8443/case/"+scenario)
	agents := meetingM3CreateAgents(t, v, model)
	v.store.releaseDirectory()
	backend, err := url.Parse(v.address)
	workAppCheck(t, "realtime-original-backend", err)
	transport := &http.Transport{Proxy: nil, DisableKeepAlives: true}
	proxy := httputil.NewSingleHostReverseProxy(backend)
	proxy.Transport, proxy.ErrorLog = transport, log.New(io.Discard, "", 0)
	proxy.ErrorHandler = func(out http.ResponseWriter, _ *http.Request, _ error) {
		http.Error(out, "Owned App unavailable", http.StatusBadGateway)
	}
	releasePath := "/__meeting_realtime_release/" + guardID[struct{}](t).String()
	var mu sync.Mutex
	releases := 0
	server := &http.Server{ReadHeaderTimeout: 2 * time.Second, ErrorLog: log.New(io.Discard, "", 0), Handler: http.HandlerFunc(func(out http.ResponseWriter, request *http.Request) {
		if request.URL.Path == releasePath {
			cookie, err := request.Cookie("agenteam_local_session")
			if request.Method != http.MethodPost || err != nil || cookie.Value != v.cookie || request.Header.Get("Origin") != w.origin {
				http.NotFound(out, request)
				return
			}
			mu.Lock()
			defer mu.Unlock()
			ctx, cancel := context.WithTimeout(request.Context(), 2*time.Second)
			defer cancel()
			state, err := w.network.State(ctx, scenario)
			if releases != 0 || err != nil || len(state.Requests) != 1 || state.ActiveHandlers != 1 {
				http.Error(out, "Original response not held", http.StatusConflict)
				return
			}
			if err = w.network.Release(ctx, scenario); err != nil {
				http.Error(out, "Owned peer release failed", http.StatusBadGateway)
				return
			}
			releases++
			out.WriteHeader(http.StatusNoContent)
			return
		}
		if strings.HasPrefix(request.URL.Path, "/api/") {
			proxy.ServeHTTP(out, request) // Preserves the real Origin and WebSocket upgrade.
			return
		}
		if request.Method != http.MethodGet && request.Method != http.MethodHead {
			http.NotFound(out, request)
			return
		}
		if strings.HasPrefix(request.URL.Path, "/assets/") {
			skillWebServeAsset(out, request, dist)
			return
		}
		http.ServeFile(out, request, filepath.Join(dist, "index.html"))
	})}
	served := make(chan error, 1)
	go func() { served <- server.Serve(listener) }()
	var once sync.Once
	stop := func() {
		once.Do(func() {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			if err := server.Shutdown(ctx); err != nil {
				t.Error("Realtime proxy exceeded its original shutdown budget")
				_ = server.Close()
			}
			if err := <-served; err != nil && !errors.Is(err, http.ErrServerClosed) {
				t.Error("Realtime original proxy Serve failed")
			}
			transport.CloseIdleConnections()
		})
	}
	t.Cleanup(stop)
	defer stop()
	response := taskExecutionGet(t, v.workAppFixture, v.client, v.cookie, http.MethodGet, "/api/v1/session", http.StatusOK)
	var identity struct{ User struct{ Username string } }
	workAppCheck(t, "realtime-original-session", json.Unmarshal(response.body, &identity))
	clear(response.body)
	if identity.User.Username == "" {
		t.Fatal("Realtime fixture lost its original Human Session")
	}
	material := workAppJSON(t, map[string]any{"origin": w.origin, "cookie": v.cookie, "project_id": v.a.String(), "route": "/" + identity.User.Username + "/runtime-app-a/meetings", "release_path": releasePath, "prompt": prompt, "answer": answer, "final": final, "title": title})
	workAppCheck(t, "realtime-browser-material", os.WriteFile(filepath.Join(private, "realtime-material.json"), material, 0600))
	clear(material)
	meetingRealtimePlaywright(t, w, harness, private, evidence, chromium, true)
	meetingRealtimePlaywright(t, w, harness, private, evidence, chromium, false)
	var result struct {
		Completed  bool               `json:"completed"`
		Meeting    mt.MeetingID       `json:"meeting"`
		StartKey   string             `json:"start_key"`
		StartBody  json.RawMessage    `json:"start_body"`
		AnswerKey  string             `json:"answer_key"`
		AnswerBody json.RawMessage    `json:"answer_body"`
		Frames     []rt.ServerMessage `json:"frames"`
		Sockets    int                `json:"sockets"`
		Closed     int                `json:"closed"`
	}
	raw, err := os.ReadFile(filepath.Join(private, "realtime-result.json"))
	workAppCheck(t, "realtime-browser-result", err)
	if len(raw) > 8<<20 || json.Unmarshal(raw, &result) != nil || !result.Completed || result.Meeting.Validate() != nil || result.Sockets != 1 || result.Closed != 1 {
		t.Fatal("Realtime browser did not close its exact two-topic journey")
	}
	clear(raw)
	stop()
	mu.Lock()
	released := releases
	mu.Unlock()
	if released != 1 {
		t.Fatal("Realtime peer was not released exactly once")
	}
	var startInput struct {
		Agents  []string        `json:"agents"`
		Mode    string          `json:"mode"`
		Content []mt.InlineNode `json:"content_nodes"`
	}
	workAppCheck(t, "realtime-original-start-input", json.Unmarshal(result.StartBody, &startInput))
	if len(startInput.Agents) != 1 || startInput.Agents[0] != agents[0].String() || startInput.Mode != "sequential" || len(startInput.Content) != 1 || startInput.Content[0].Type != "text" || startInput.Content[0].Text != prompt {
		t.Fatal("browser changed the original single-Agent Start")
	}
	var lookup struct {
		Status  string     `json:"status"`
		Receipt mt.Receipt `json:"receipt"`
	}
	meetingDeletionHTTP(t, v, "realtime-start-lookup", http.MethodPost, "/meetings/start/lookup", result.StartKey, result.StartBody, &lookup)
	if lookup.Status != "committed" || !lookup.Receipt.Replayed || lookup.Receipt.MeetingID != result.Meeting || lookup.Receipt.TurnID == nil {
		t.Fatal("browser Start lost its original receipt")
	}
	receipt := lookup.Receipt
	var detail mt.Detail
	path := "/meetings/" + result.Meeting.String()
	meetingDeletionHTTP(t, v, "realtime-original-detail", http.MethodGet, path, "", nil, &detail)
	if len(detail.Turns) != 1 || len(detail.Turns[0].Contributions) != 2 || detail.Turns[0].Contributions[0].Kind != "user" || detail.Turns[0].Contributions[1].Kind != "agent" || len(detail.Decisions) != 1 || detail.Turns[0].ID != *receipt.TurnID || detail.Turns[0].Status != "completed" || detail.Session.Summary == nil || detail.Session.Summary.Version != 1 {
		t.Fatal("Realtime final durable Meeting differs")
	}
	contribution, decision := detail.Turns[0].Contributions[1], detail.Decisions[0]
	if contribution.ExecutionID == nil || *contribution.ExecutionID != decision.ExecutionID || contribution.Message == nil || len(contribution.Message.Content) != 1 || contribution.Message.Content[0].Text != final || decision.Status != "answered" || decision.Version != 2 || decision.Question != question || decision.Answer == nil || *decision.Answer != answer {
		t.Fatal("Realtime changed the original Decision/Execution/public reply")
	}
	meetingDeletionHTTP(t, v, "realtime-answer-lookup", http.MethodPost, path+"/decisions/lookup", result.AnswerKey, result.AnswerBody, &lookup)
	if lookup.Status != "committed" || !lookup.Receipt.Replayed || lookup.Receipt.MeetingID != result.Meeting {
		t.Fatal("browser answer lost its original command receipt")
	}
	timeline := rt.Topic{Kind: rt.MeetingTimeline, ProjectID: v.a, MeetingID: result.Meeting.String()}
	execution := rt.Topic{Kind: rt.MeetingExecution, ProjectID: v.a, MeetingID: result.Meeting.String(), ContributionID: contribution.ID.String(), Generation: contribution.Generation, Attempt: contribution.Attempt, ExecutionID: *contribution.ExecutionID}
	meetingRealtimeFrames(t, result.Frames, timeline, execution, decision, final)
	var calls, retired, leases, decisions int
	err = v.admin.QueryRow(v.ctx, `SELECT
 (SELECT count(*) FROM agenteam_model.calls WHERE project_id::text=$1::text),
 (SELECT count(*) FROM agenteam_model.calls WHERE project_id::text=$1::text AND retired),
 (SELECT count(*) FROM agenteam_model.calls c JOIN agenteam_secret.secret_leases l ON l.id=c.lease_id WHERE c.project_id::text=$1::text AND l.released),
 (SELECT count(*) FROM agenteam_execution.loop_decision_results WHERE execution_id=$2::text)`, v.a.String(), decision.ExecutionID.String()).Scan(&calls, &retired, &leases, &decisions)
	if err != nil {
		meetingM3SafeError(t, "realtime-durable-counts", err)
	}
	workAppCheck(t, "realtime-durable-counts", err)
	if calls != 3 || retired != 3 || leases != 3 || decisions != 1 {
		t.Fatalf("Realtime original retirement counts calls=%d retired=%d leases=%d decisions=%d", calls, retired, leases, decisions)
	}
	v.runtime.StopAdmission()
	drain, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	err = v.runtime.Drain(drain)
	cancel()
	workAppCheck(t, "realtime-drain", err)
	v.requestStop()
	usageAppWait(t, v.root.done, "realtime-app-wait")
	workAppCheck(t, "realtime-app-return", v.root.err)
	if !v.runtime.Joined() || !v.assembly.Joined() || !v.root.owned.producersJoined() {
		t.Fatal("Realtime original App owners did not join")
	}
	w.joinedPeer(t, scenario, 3, 3)
	meetingDeletionGuardReleased(t, v.workAppFixture)
	t.Log("M3_MEETING_REALTIME real_owner_browser=true real_app_pg=true two_topics_ack_snapshot=true decision_wait_answer_same_execution=true committed_tool_text_timeline=true token_stream=false original_app_browser_peer_joined=true")
}

func meetingRealtimeFrames(t *testing.T, frames []rt.ServerMessage, timeline, execution rt.Topic, decision mt.DecisionView, text string) {
	t.Helper()
	if len(frames) == 0 || len(frames) > 256 {
		t.Fatal("Realtime public frame count differs")
	}
	ack := map[rt.SubscriptionID]rt.Topic{}
	invalidations := map[rt.Topic]int{}
	running, waiting, terminal, summary := false, false, false, false
	for _, frame := range frames {
		if frame.Validate() != nil || frame.Topic == nil || *frame.Topic != timeline && *frame.Topic != execution || frame.Type == rt.Resync || frame.Type == rt.Unsubscribed {
			t.Fatal("Realtime actual public frame violates the exact two topics")
		}
		if frame.Type == rt.Subscribed {
			if _, exists := ack[frame.SubscriptionID]; exists {
				t.Fatal("Realtime subscription acknowledged twice")
			}
			ack[frame.SubscriptionID] = *frame.Topic
			continue
		}
		if topic, exists := ack[frame.SubscriptionID]; !exists || topic != *frame.Topic {
			t.Fatal("Realtime data preceded its original acknowledgement")
		}
		if frame.Type == rt.Invalidated {
			invalidations[*frame.Topic]++
		}
		if frame.Type != rt.SnapshotRead {
			continue
		}
		if *frame.Topic == timeline {
			var detail mt.Detail
			workAppCheck(t, "realtime-timeline-snapshot", json.Unmarshal(frame.Snapshot.Content, &detail))
			if detail.Session.ID.String() != timeline.MeetingID || detail.Session.ProjectID != timeline.ProjectID {
				t.Fatal("Realtime timeline changed its original Meeting")
			}
			summary = summary || detail.Session.Summary != nil && detail.Session.Summary.Version == 1
			continue
		}
		var projection ec.RuntimeProjection
		workAppCheck(t, "realtime-execution-snapshot", json.Unmarshal(frame.Snapshot.Content, &projection))
		if projection.Validate() != nil || projection.ExecutionID != execution.ExecutionID || projection.ProjectID != execution.ProjectID {
			t.Fatal("Realtime projection changed its original Execution")
		}
		running = running || projection.Status == ec.Running
		for _, item := range projection.Items {
			if item.Waiting != nil {
				if item.Waiting.Reason != "decision" || item.Waiting.Reference != decision.ID.String() {
					t.Fatal("Realtime waiting projection changed the original Decision")
				}
				waiting = true
			}
		}
		if projection.Status == ec.Succeeded {
			tool, publicText := false, false
			for _, item := range projection.Items {
				if item.Tool != nil && item.Tool.ResultAvailable && !item.Tool.IsError && item.Tool.CallID == "meeting_realtime_decision" {
					var result mt.DecisionResult
					if json.Unmarshal([]byte(item.Tool.ResultText), &result) == nil && result.Validate() == nil && result.DecisionID == decision.ID && result.Version == decision.Version && result.Status == "answered" && result.Answer != nil && decision.Answer != nil && *result.Answer == *decision.Answer {
						tool = true
					}
				}
				publicText = publicText || item.Text != nil && item.Text.Content == text
			}
			terminal = terminal || tool && publicText
		}
	}
	if len(ack) != 2 || invalidations[timeline] == 0 || invalidations[execution] == 0 || !running || !waiting || !terminal || !summary {
		t.Fatalf("Realtime observations ack=%d timeline=%d execution=%d running=%t waiting=%t terminal=%t summary=%t", len(ack), invalidations[timeline], invalidations[execution], running, waiting, terminal, summary)
	}
}

func meetingRealtimePlaywright(t *testing.T, w *m1Workflow, harness, private, evidence, chromium string, list bool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(w.app.ctx, 50*time.Second)
	defer cancel()
	args := []string{filepath.Join(harness, "node_modules/@playwright/test/cli.js"), "test", "--config", filepath.Join(harness, "meeting-realtime.config.js")}
	if list {
		args = append(args, "--list")
	}
	cmd := exec.CommandContext(ctx, "node", args...)
	cmd.Dir, cmd.SysProcAttr, cmd.WaitDelay = harness, &syscall.SysProcAttr{Setpgid: true}, time.Second
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
		workAppCheck(t, "realtime-child-cache", os.MkdirAll(path, 0700))
		cmd.Env = append(cmd.Env, name+"="+path)
	}
	cmd.Env = append(cmd.Env, "PLAYWRIGHT_NO_COPY_PROMPT=1", "AGENTEAM_AUTH_WEB_ORIGIN="+w.origin, "AGENTEAM_AUTH_WEB_PRIVATE="+private, "AGENTEAM_AUTH_WEB_CHROMIUM="+chromium, "AGENTEAM_MEETING_WEB_EVIDENCE="+evidence, "AGENTEAM_MEETING_REALTIME_CASE=original-decision")
	var output bytes.Buffer
	cmd.Stdout, cmd.Stderr = &output, &output
	workAppCheck(t, "realtime-browser-start", cmd.Start())
	err := cmd.Wait()
	_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	cmd.Env = nil
	t.Logf("MEETING_REALTIME_BROWSER list=%t actual_wait=true pid=%d success=%t", list, cmd.Process.Pid, err == nil)
	redacted := output.String()
	for _, secret := range []string{w.app.cookie, w.app.csrf} {
		if secret != "" {
			redacted = strings.ReplaceAll(redacted, secret, "[fixture-session-redacted]")
		}
	}
	name, expected := "realtime-browser-console.log", `(?m)\b1 passed\b`
	if list {
		name, expected = "realtime-browser-list.log", `Total: 1 test in 1 file`
	}
	workAppCheck(t, "realtime-browser-console", os.WriteFile(filepath.Join(evidence, name), []byte(redacted), 0600))
	if err != nil || !regexp.MustCompile(expected).Match(output.Bytes()) {
		t.Fatal("Realtime browser did not pass its original case; see private evidence console")
	}
}
