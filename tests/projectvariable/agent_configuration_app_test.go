//go:build integration

package projectvariable_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	ac "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/app"
	"github.com/LunaDeerTech/agenteam/internal/central/config"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/tool/builtin"
	wc "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
	"github.com/LunaDeerTech/agenteam/internal/platform/logging"
	objectfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/objectstore"
)

// The setup publishes a real P2 Project and Model using the existing providers.
// Every setup owner retires before public app.Run constructs the graph under
// test. No setup service handles any tested Agent HTTP command.
func TestAgentConfigurationApp(t *testing.T) {
	for _, test := range []struct {
		name string
		lost bool
	}{
		{"default-create-get-lookup-directory-and-assign", false},
		{"lost-create-receipt-and-current-owner", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			entered, completed := false, false
			// Register before the original fixture: this check runs after its
			// cleanup-owned handoff, and detects a callback installed on a copy.
			t.Cleanup(func() {
				if !entered || !completed {
					t.Error("public App HTTP handoff did not actually complete")
				}
			})
			a := newAgentCreateFixture(t)
			original := a.p2.base
			process, err := a.guard.CurrentProcess()
			agentAppRequire(t, err)
			var task wc.Task
			if !test.lost {
				projection := *original
				projection.project, projection.projectAuthority = a.p2.project, a.providers.Projects
				_, task = newPreparationTaskInput(t, &projection)
			}
			registration := agentAppRegistration(t, a)
			if original.afterOwners != nil {
				t.Fatal("original fixture already has an owner handoff")
			}
			original.afterOwners = func() {
				entered = true
				if !original.core.Joined() || !a.agents.Joined() || !a.p2.service.Joined() {
					t.Fatal("setup providers have not physically joined")
				}
				if _, err := a.guard.CurrentProcess(); err == nil {
					t.Fatal("setup still holds its ProcessGuard")
				}
				var stopped bool
				agentAppRequire(t, original.raw.QueryRow(ctxFor(t), `SELECT state='stopped' AND stopped_at IS NOT NULL FROM agenteam_object.process_claims WHERE process_id=$1`, process.String()).Scan(&stopped))
				if !stopped {
					t.Fatal("setup process has not retired")
				}
				runAgentConfigurationApp(t, a, task, registration, process.String(), test.lost)
				completed = true
			}
		})
	}
}

func agentAppRequire(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

type agentAppLog struct {
	mu        sync.Mutex
	data      bytes.Buffer
	listening chan string
}

func (l *agentAppLog) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	_, _ = l.data.Write(p)
	var entry struct {
		Event   string `json:"event"`
		Address string `json:"listen_address"`
	}
	if json.Unmarshal(bytes.TrimSpace(p), &entry) == nil && entry.Event == "listening" {
		select {
		case l.listening <- entry.Address:
		default:
		}
	}
	return len(p), nil
}
func (l *agentAppLog) drained() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, line := range bytes.Split(l.data.Bytes(), []byte{'\n'}) {
		var entry struct {
			Event   string `json:"event"`
			Outcome string `json:"outcome"`
		}
		if json.Unmarshal(line, &entry) == nil && entry.Event == "shutdown_complete" && entry.Outcome == "drained" {
			return true
		}
	}
	return false
}

type agentAppProxyResult struct {
	err error
	cut bool
}
type agentAppHTTP struct {
	server     *httptest.Server
	transport  *http.Transport
	done       chan agentAppProxyResult
	mu         sync.Mutex
	createPath string
	lose       bool
	creates    int
	committed  []byte
}

// This owned TLS bridge forwards only to the actual App listener. It drains
// each original upstream body before optionally truncating one Create receipt.
// It never supplies an HTTP authority or a synthetic successful response.
func (h *agentAppHTTP) serve(address string, w http.ResponseWriter, r *http.Request) {
	result := agentAppProxyResult{}
	defer func() { h.done <- result }()
	up := r.Clone(r.Context())
	u := *r.URL
	u.Scheme, u.Host = "http", address
	up.URL, up.RequestURI = &u, ""
	response, err := h.transport.RoundTrip(up)
	if err != nil {
		result.err = errors.New("original App RoundTrip failed")
		http.Error(w, "upstream unavailable", 502)
		return
	}
	raw, readErr := io.ReadAll(io.LimitReader(response.Body, (1<<20)+1))
	closeErr := response.Body.Close()
	validLength := response.ContentLength == int64(len(raw))
	if r.Method == http.MethodHead {
		validLength = len(raw) == 0 && response.ContentLength >= 0
	}
	if readErr != nil || closeErr != nil || len(raw) > 1<<20 || !validLength {
		result.err = errors.New("original App response did not complete bounded EOF/Close/length")
		http.Error(w, "upstream incomplete", 502)
		return
	}
	if r.Method == http.MethodPost && r.URL.Path == h.createPath {
		h.mu.Lock()
		h.creates++
		if response.StatusCode == http.StatusOK {
			h.committed = bytes.Clone(raw)
			result.cut = h.lose
			h.lose = false
		}
		h.mu.Unlock()
	}
	for name, values := range response.Header {
		for _, value := range values {
			w.Header().Add(name, value)
		}
	}
	w.WriteHeader(response.StatusCode)
	if r.Method != http.MethodHead {
		if result.cut {
			raw = raw[:len(raw)/2]
		}
		if _, err := w.Write(raw); err != nil {
			result.err = errors.New("owned TLS downstream write failed")
		}
	}
}

func (h *agentAppHTTP) request(t *testing.T, browser variableHTTPBrowser, method, path string, body []byte, key f.IdempotencyKey, cut bool) variableHTTPResponse {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, method, h.server.URL+path, bytes.NewReader(body))
	agentAppRequire(t, err)
	request.AddCookie(&http.Cookie{Name: "__Host-agenteam_session", Value: browser.cookie, Secure: true, HttpOnly: true, Path: "/"})
	request.Header.Set("Origin", h.server.URL)
	request.Header.Set("Sec-Fetch-Site", "same-origin")
	if method != http.MethodGet && method != http.MethodHead {
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("X-CSRF-Token", browser.csrf)
	}
	if key != "" {
		request.Header.Set("Idempotency-Key", string(key))
	}
	client := *h.server.Client()
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	response, err := client.Do(request)
	agentAppRequire(t, err)
	raw, readErr := io.ReadAll(io.LimitReader(response.Body, (1<<20)+1))
	closeErr := response.Body.Close()
	if response.TLS == nil || len(response.TLS.VerifiedChains) == 0 || closeErr != nil || len(raw) > 1<<20 {
		t.Fatal("owned TLS response did not close safely")
	}
	select {
	case observed := <-h.done:
		if observed.err != nil || observed.cut != cut {
			t.Fatal("original proxy handler did not return its exact result", observed.err)
		}
	case <-ctx.Done():
		t.Fatal("original proxy handler did not return")
	}
	if cut {
		if !errors.Is(readErr, io.ErrUnexpectedEOF) || response.StatusCode != http.StatusOK || int64(len(raw)) >= response.ContentLength {
			t.Fatal("receipt-loss boundary was not an actual truncated successful response")
		}
	} else if readErr != nil || method != http.MethodHead && response.ContentLength != int64(len(raw)) {
		t.Fatal("original response lost EOF/Content-Length", readErr)
	}
	if response.Header.Get("Cache-Control") != "no-store" || response.Header.Get("X-Request-ID") == "" {
		t.Fatal("HTTP omitted original security metadata")
	}
	return variableHTTPResponse{status: response.StatusCode, header: response.Header.Clone(), body: raw}
}

func runAgentConfigurationApp(t *testing.T, a *agentCreateFixture, task wc.Task, registration, oldProcess string, lost bool) {
	t.Helper()
	h := &agentAppHTTP{done: make(chan agentAppProxyResult, 1), transport: &http.Transport{Proxy: nil, DisableKeepAlives: true}, lose: lost, createPath: variableHTTPPath(a.p2.project.ID, "/agents")}
	h.server = httptest.NewUnstartedServer(nil)
	h.server.Config.ErrorLog = log.New(io.Discard, "", 0)
	defer h.server.Close()
	defer h.transport.CloseIdleConnections()
	origin := "https://" + h.server.Listener.Addr().String()
	withAgentConfigurationApp(t, a, oldProcess, origin, func(ctx context.Context, address string) {
		// Retire the HTTP bridge before the shared App owner begins shutdown.
		defer func() {
			h.server.Close()
			h.transport.CloseIdleConnections()
		}()
		h.server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { h.serve(address, w, r) })
		h.server.StartTLS()
		if h.server.Client().Transport.(*http.Transport).TLSClientConfig.InsecureSkipVerify {
			t.Fatal("owned TLS verification is disabled")
		}
		if agentAppRegistration(t, a) != registration {
			t.Fatal("App reconciliation rewrote the historical fixed builtin identity")
		}
		// The registration is pre-existing; the new Agent must still traverse
		// this App's current InstallSource/Skill/guard binding.
		agentConfigurationAppCommands(t, ctx, a, task, h, lost)
	})
}

// Both native HTTP and browser exercises use the same public App owner. The
// callback may only attach HTTP observation; it never supplies domain services.
func withAgentConfigurationApp(t *testing.T, a *agentCreateFixture, oldProcess, origin string, exercise func(context.Context, string)) {
	t.Helper()
	directory, err := os.MkdirTemp("", "agent-app-")
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
	exercise(ctx, address)
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
	t.Log("Agent Configuration App original public Run and TLS handlers joined; runtime disabled; original create count=1")
}

func agentConfigurationAppCommands(t *testing.T, ctx context.Context, a *agentCreateFixture, task wc.Task, h *agentAppHTTP, lost bool) {
	t.Helper()
	request, _, _ := a.request(t)
	wire := map[string]json.RawMessage{}
	agentAppRequire(t, json.Unmarshal(jsonBytes(t, request), &wire))
	delete(wire, "add_skills_enabled")
	delete(wire, "install_skill_enabled")
	rawRequest := jsonBytes(t, wire)
	request, err := ac.DecodeAgentCreate(rawRequest)
	agentAppRequire(t, err)
	key := f.IdempotencyKey("app-agent-create")
	body := jsonBytes(t, map[string]any{"request": json.RawMessage(rawRequest)})
	owner := a.p2.base.ownerBrowser
	created := h.request(t, owner, http.MethodPost, h.createPath, body, key, lost)
	if created.status != http.StatusOK {
		t.Fatal("actual App Agent create failed", created.status)
	}
	h.mu.Lock()
	original := bytes.Clone(h.committed)
	h.mu.Unlock()
	receipt, err := ac.DecodeAgentMutation(original)
	agentAppRequire(t, err)
	if !lost && !bytes.Equal(original, created.body) {
		t.Fatal("normal original receipt changed in transport")
	}
	fields := receipt.Fields().Agent.Fields()
	target := request.Fields().AgentID
	if fields.Core.ID != target || fields.Core.Version != 1 || !slices.Equal(fields.AllowedToolIDs, []i.ToolID{a.tool.ToolID}) {
		t.Fatal("default-enabled create omitted its real install Tool")
	}
	command, err := ac.AgentCommandIdentity(a.p2.project.ID, ac.CreateAgentCommand, key)
	agentAppRequire(t, err)
	want := [16]int64{1, 1, 0, 0, 2, 1, 1, 1, 0, 0, 1, 1, 1, 1, 1, 0}
	facts, err := agentCreateFacts(ctx, a.p2.base.raw, a.p2.project.ID, target, key)
	agentAppRequire(t, err)
	if facts != want {
		t.Fatal("App Agent creation omitted or duplicated atomic domain facts", facts)
	}
	agentAppRequire(t, agentCreateRelations(ctx, a.p2.base.raw, a.p2.project.ID, target, key, a.modelID, a.tool.ToolID, command))
	lookupBody := jsonBytes(t, map[string]any{"command": ac.CreateAgentCommand, "request": json.RawMessage(rawRequest)})
	lookupPath := variableHTTPPath(a.p2.project.ID, "/agent-commands/lookup")
	found := h.request(t, owner, http.MethodPost, lookupPath, lookupBody, key, false)
	lookup, err := ac.DecodeAgentCommandLookup(found.body)
	agentAppRequire(t, err)
	if found.status != 200 || lookup.Status() != ac.AgentLookupCommitted || lookup.Receipt() == nil || !bytes.Equal(jsonBytes(t, *lookup.Receipt()), original) {
		t.Fatal("original key Lookup did not recover the exact committed receipt")
	}
	configurationPath := h.createPath + "/" + target.String() + "/configuration"
	get := h.request(t, owner, http.MethodGet, configurationPath, nil, "", false)
	var configuration ac.AgentConfig
	agentAppRequire(t, json.Unmarshal(get.body, &configuration))
	var fullFields map[string]json.RawMessage
	agentAppRequire(t, json.Unmarshal(get.body, &fullFields))
	if get.status != 200 || len(fullFields) != 20 || !bytes.Equal(jsonBytes(t, configuration), jsonBytes(t, receipt.Fields().Agent)) {
		t.Fatal("full configuration Get lost its typed original fields")
	}
	head := h.request(t, owner, http.MethodHead, configurationPath, nil, "", false)
	if head.status != 200 || len(head.body) != 0 || head.header.Get("Content-Length") != strconv.Itoa(len(get.body)) {
		t.Fatal("configuration HEAD differs from GET metadata")
	}
	directoryReply := h.request(t, owner, http.MethodGet, h.createPath+"/"+target.String(), nil, "", false)
	var entry ac.DirectoryEntry
	var safeFields map[string]json.RawMessage
	agentAppRequire(t, json.Unmarshal(directoryReply.body, &entry))
	agentAppRequire(t, json.Unmarshal(directoryReply.body, &safeFields))
	if directoryReply.status != 200 || entry.Validate() != nil || len(safeFields) != 9 || entry.ID != target || entry.Name != fields.Core.Name || bytes.Contains(directoryReply.body, []byte(fields.Core.Instructions)) || bytes.Contains(directoryReply.body, []byte(a.modelID.String())) {
		t.Fatal("safe Directory was replaced by full configuration")
	}
	page := h.request(t, owner, http.MethodGet, h.createPath, nil, "", false)
	var listed f.Page[ac.DirectoryEntry]
	agentAppRequire(t, json.Unmarshal(page.body, &listed))
	if page.status != 200 || len(listed.Items) != 1 || listed.Items[0].ID != target {
		t.Fatal("collection GET no longer dispatches to safe Directory")
	}
	if lost {
		for _, forbidden := range []struct {
			method, path string
			body         []byte
			key          f.IdempotencyKey
		}{
			{http.MethodGet, configurationPath, nil, ""}, {http.MethodPost, lookupPath, lookupBody, key},
		} {
			reply := h.request(t, a.p2.base.otherBrowser, forbidden.method, forbidden.path, forbidden.body, forbidden.key, false)
			agentAppProblem(t, reply, http.StatusNotFound, f.NotFound)
		}
		logout := h.request(t, owner, http.MethodPost, "/api/v1/sessions/logout", []byte(`{}`), "app-agent-logout", false)
		if logout.status != http.StatusNoContent || len(logout.body) != 0 {
			t.Fatal("original Session did not logout", logout.status)
		}
		agentAppProblem(t, h.request(t, owner, http.MethodPost, lookupPath, lookupBody, key, false), http.StatusUnauthorized, f.SessionRevoked)
	} else {
		transfer := wc.TaskTransfer{TargetState: wc.TaskStateTodo, AssigneeAgentID: &target, AddBlockers: []wc.TaskBlockerCreate{}, ResolveBlockerIDs: []wc.TaskBlockerID{}}
		transferPath := variableHTTPPath(a.p2.project.ID, "/tasks/"+task.ID.String()+"/transfer")
		reply := h.request(t, owner, http.MethodPost, transferPath, jsonBytes(t, map[string]any{"expected_version": task.Version, "request": transfer}), "app-new-agent-task", false)
		changed, err := wc.DecodeTaskTransitionMutation(reply.body)
		agentAppRequire(t, err)
		if reply.status != 200 || changed.Validate() != nil || changed.Task.State != wc.TaskStateTodo || changed.Task.AssigneeAgentID == nil || *changed.Task.AssigneeAgentID != target || changed.Task.Version != task.Version+1 {
			t.Fatal("new App Agent could not be assigned through original Work HTTP")
		}
		var state, assigned string
		var version int64
		agentAppRequire(t, a.p2.base.raw.QueryRow(ctx, `SELECT state,assignee_agent_id::text,version FROM agenteam_work.tasks WHERE project_id=$1 AND id=$2`, a.p2.project.ID.String(), task.ID.String()).Scan(&state, &assigned, &version))
		if state != "todo" || assigned != target.String() || version != int64(changed.Task.Version) {
			t.Fatal("Work response was not durably assigned")
		}
		// Reuse only the existing read-only fact observer, not its retired
		// service graph. It binds both history entries and the single Outbox
		// event to this HTTP command, version and current Human Owner.
		projection := *a.p2.base
		projection.project = a.p2.project
		observer := &taskTransitionFixture{base: &projection, task: task, agentID: target}
		stored, err := observer.committedFacts(ctx, a.p2.base.raw, "app-new-agent-task")
		agentAppRequire(t, err)
		if !bytes.Equal(jsonBytes(t, stored), jsonBytes(t, changed)) {
			t.Fatal("Work HTTP receipt differs from original history and Outbox")
		}
	}
	after, err := agentCreateFacts(ctx, a.p2.base.raw, a.p2.project.ID, target, key)
	agentAppRequire(t, err)
	if after != facts {
		t.Fatal("configuration reads/Lookup or forbidden session duplicated Agent facts")
	}
	h.mu.Lock()
	creates := h.creates
	h.mu.Unlock()
	if creates != 1 {
		t.Fatal("confirmation loss was retried as another Create")
	}
}

func agentAppProblem(t *testing.T, response variableHTTPResponse, status int, code f.Code) {
	t.Helper()
	var problem struct {
		Code f.Code `json:"code"`
	}
	if response.status != status || json.Unmarshal(response.body, &problem) != nil || problem.Code != code {
		t.Fatal("current authority boundary returned an unexpected problem", response.status)
	}
}

func agentAppRegistration(t *testing.T, a *agentCreateFixture) string {
	t.Helper()
	var raw string
	agentAppRequire(t, a.p2.base.raw.QueryRow(ctxFor(t), `SELECT jsonb_build_object('identity',to_jsonb(i),'registration',to_jsonb(r),'definition',encode(s.definition,'hex'))::text FROM agenteam_tool.identities i JOIN agenteam_tool.registrations r USING(tool_id) JOIN agenteam_tool.spec_revisions s ON(s.tool_id,s.spec_revision)=(r.tool_id,r.spec_revision) WHERE i.stable_key=$1`, builtin.SkillInstallStableKey).Scan(&raw))
	return raw
}

func agentConfigurationAppConfig(t *testing.T, a *agentCreateFixture, directory, origin string) config.Config {
	t.Helper()
	remote, err := objectfixture.Load()
	agentAppRequire(t, err)
	recovery := filepath.Join(directory, "account-recovery.log")
	agentAppRequire(t, os.WriteFile(recovery, nil, 0600))
	key := func(k string, b byte) string {
		return fmt.Sprintf(`{"format":1,"current_kid":%q,"keys":[{"kid":%q,"key_b64":%q}]}`, k, k, base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{b}, 32)))
	}
	values := map[string]string{
		"HTTP_ADDR": "127.0.0.1:0", "PUBLIC_ORIGIN": origin, "SHUTDOWN_TIMEOUT": "10s",
		"DATABASE_URL": a.p2.base.db.Fixture.URL(a.p2.base.db.Name), "DATABASE_CA_FILE": a.p2.base.db.Fixture.CAFile, "DATABASE_TLS_MODE": "verify-full",
		"CURSOR_KEYRING": key("c", 1), "ACCOUNT_KEYRING": key("a", 4), "SECRET_KEYRING": fmt.Sprintf(`{"format":1,"current_version":"1","keys":[{"version":"1","key_b64":%q}]}`, base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{2}, 32))),
		"OBJECT_DOWNLOAD_KEYRING": key("d", 3), "KNOWLEDGE_CONFIRMATION_KEYRING": key("k", 5), "ACCOUNT_RECOVERY_LOG": recovery,
		"OBJECT_ENDPOINT": remote.Endpoint(), "OBJECT_BUCKET": a.p2.bucket, "OBJECT_ACCESS_KEY": remote.AccessKey, "OBJECT_SECRET_KEY": remote.SecretKey, "OBJECT_TLS_MODE": "verify-full", "OBJECT_CA_FILE": remote.CAFile, "OBJECT_SPOOL_DIR": filepath.Join(directory, "spool"),
	}
	cfg, err := config.Load(func(name string) (string, bool) {
		v, ok := values[strings.TrimPrefix(name, config.Prefix)]
		return v, ok
	}, nil)
	agentAppRequire(t, err)
	return cfg
}
