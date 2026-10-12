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
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	eventc "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	vc "github.com/LunaDeerTech/agenteam/internal/central/projectvariable/contract"
)

// Real initialized P2 preparation retires before the public runtime-off App.
// The browser creates, changes and logically deletes an ordinary Variable; no Secret or runtime
// capability, production Project initialization, or Owner transfer is claimed.
func TestProjectVariablesWeb(t *testing.T) {
	t.Run("create-update-clear-and-delete", func(t *testing.T) {
		entered, completed := false, false
		t.Cleanup(func() {
			if !entered || !completed {
				t.Error("Project Variables original App handoff incomplete")
			}
		})
		a := newAgentCreateFixture(t)
		original := a.p2.base
		process, err := a.guard.CurrentProcess()
		agentAppRequire(t, err)
		var username string
		agentAppRequire(t, original.raw.QueryRow(ctxFor(t), `SELECT username FROM agenteam_account.users WHERE id=$1`, original.ownerBrowser.actor.Details().UserID).Scan(&username))
		if original.afterOwners != nil {
			t.Fatal("original owner handoff occupied")
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
			w := newProjectVariablesWeb(t, a)
			defer w.dispose(t)
			withAgentConfigurationApp(t, a, process.String(), w.origin, func(ctx context.Context, address string) {
				defer w.close(t)
				w.start(t, address)
				w.prepareEmpty(t, ctx)
				w.material(t, username)
				w.browser(t, ctx)
				w.verify(t, ctx)
				w.close(t)
			})
			completed = true
		}
	})
}

const variableWebName = "BROWSER_ORDINARY"
const variableWebDescription = "Ordinary project setting."
const variableWebValue = "public-fixture-initial\nKeep the complete value."
const variableWebNextDescription = "Updated ordinary project setting."
const variableWebNextValue = "public-fixture-updated\nSecond complete line."

type projectVariableWebIntent struct {
	kind                  string
	key                   f.IdempotencyKey
	index                 int
	create                vc.VariableCreate
	update                vc.VariableUpdate
	before                vc.Variable
	target                vc.VariableID
	requestID             f.ID[f.Request]
	bodyDigest, keyDigest [32]byte
}
type projectVariableWebIntentKey struct{}
type projectVariablesWeb struct {
	*agentEditWeb
	immutable      string
	intents        []projectVariableWebIntent
	receipts       []vc.VariableMutation
	details, lists [4]int
}

func newProjectVariablesWeb(t *testing.T, a *agentCreateFixture) *projectVariablesWeb {
	t.Helper()
	const prefix = "AGENTEAM_PROJECT_VARIABLES_WEB"
	dist, runtime, evidence, inputHash := os.Getenv(prefix+"_DIST"), os.Getenv("AGENTEAM_AUTH_WEB_RUNTIME"), os.Getenv(prefix+"_EVIDENCE"), os.Getenv(prefix+"_INPUT_HASH")
	if !filepath.IsAbs(dist) || !filepath.IsAbs(runtime) || len(runtime) > 45 || !filepath.IsAbs(evidence) || len(inputHash) != 64 || strings.Trim(inputHash, "0123456789abcdef") != "" {
		t.Fatal("frozen Variables assets/runtime/input required")
	}
	if info, err := os.Stat(filepath.Join(dist, "index.html")); err != nil || !info.Mode().IsRegular() {
		t.Fatal("frozen Variables index missing")
	}
	directory, err := os.MkdirTemp(runtime, "pvw-")
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
	base := &agentConfigurationWeb{a: a, mode: "create-update-clear-and-delete", origin: "http://" + listener.Addr().String(), directory: directory, evidence: evidence, inputHash: inputHash, dist: dist, listener: listener, serve: make(chan error, 1), tails: map[string]*reviewWebTail{}}
	published = true
	return &projectVariablesWeb{agentEditWeb: &agentEditWeb{agentConfigurationWeb: base}}
}
func (w *projectVariablesWeb) material(t *testing.T, username string) {
	raw := jsonBytes(t, map[string]any{"mode": w.mode, "cookie": w.a.p2.base.ownerBrowser.cookie, "project_id": w.a.p2.project.ID, "route": "/" + username + "/" + w.a.p2.project.NormalizedName + "/settings/variables", "draft": map[string]string{"name": variableWebName, "description": variableWebDescription, "value": variableWebValue}, "changed_description": variableWebNextDescription, "changed_value": variableWebNextValue})
	agentAppRequire(t, os.WriteFile(filepath.Join(w.directory, "project-variables-material.json"), raw, 0600))
	clear(raw)
}
func (w *projectVariablesWeb) prepareEmpty(t *testing.T, ctx context.Context) {
	var empty bool
	agentAppRequire(t, w.a.p2.base.raw.QueryRow(ctx, `SELECT
 NOT EXISTS(SELECT 1 FROM agenteam_projectvariable.variables WHERE project_id=$1::text::uuid)
 AND NOT EXISTS(SELECT 1 FROM agenteam_projectvariable.commands WHERE project_id=$1::text::uuid)
 AND NOT EXISTS(SELECT 1 FROM agenteam_projectvariable.project_generations WHERE project_id=$1::text::uuid)
 AND NOT EXISTS(SELECT 1 FROM agenteam_scheduler.dispatches WHERE project_id=$1::text)
 AND NOT EXISTS(SELECT 1 FROM agenteam_execution.executions WHERE project_id=$1::text)`, w.a.p2.project.ID.String()).Scan(&empty))
	if !empty {
		t.Fatal("Variables preparation already has business facts")
	}
	snapshot, err := w.unchanged(ctx)
	agentAppRequire(t, err)
	w.mu.Lock()
	w.immutable = snapshot
	w.mu.Unlock()
}
func (w *projectVariablesWeb) start(t *testing.T, address string) {
	t.Helper()
	w.upstream = &http.Transport{Proxy: nil, DisableKeepAlives: true}
	w.backend = httptest.NewUnstartedServer(http.HandlerFunc(func(out http.ResponseWriter, r *http.Request) { w.bridge(address, out, r) }))
	w.backend.Config.ErrorLog = log.New(io.Discard, "", 0)
	w.backend.StartTLS()
	w.transport = w.backend.Client().Transport.(*http.Transport).Clone()
	if w.transport.TLSClientConfig.InsecureSkipVerify {
		t.Fatal("owned Agent Edit TLS verification disabled")
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
				http.Error(out, "owned Variables observation failed", 500)
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

func (w *projectVariablesWeb) browser(t *testing.T, parent context.Context) {
	t.Helper()
	root, err := filepath.Abs("../account-captcha-web")
	agentAppRequire(t, err)
	ctx, cancel := context.WithTimeout(parent, 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "node", filepath.Join(root, "node_modules/@playwright/test/cli.js"), "test", "--config", filepath.Join(root, "project-variables.config.js"))
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
	const prefix = "AGENTEAM_PROJECT_VARIABLES_WEB"
	cmd.Env = append(cmd.Env, "TMPDIR="+w.directory, "PLAYWRIGHT_NO_COPY_PROMPT=1", "AGENTEAM_AUTH_WEB_ORIGIN="+w.origin, "AGENTEAM_AUTH_WEB_PRIVATE="+w.directory, "AGENTEAM_AUTH_WEB_CHROMIUM=/usr/bin/chromium", prefix+"_DIST="+w.dist, prefix+"_CASE="+w.mode, prefix+"_EVIDENCE="+w.evidence, prefix+"_INPUT_HASH="+w.inputHash)
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	if err = cmd.Start(); err != nil {
		t.Fatal("owned Variables Playwright did not start")
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
	t.Logf("Project Variables Node actual_wait pid=%d success=%t", cmd.Process.Pid, err == nil)
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
		t.Fatal("original Variables browser call failed")
	}
}

func (w *projectVariablesWeb) observe(r *http.Request) error {
	if r.Method == http.MethodGet || r.Method == http.MethodHead {
		return nil
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, vc.MaxRequestBytes+1))
	closed := r.Body.Close()
	if err != nil || closed != nil || len(raw) > vc.MaxRequestBytes {
		return errors.New("original Variables request incomplete")
	}
	r.Body = io.NopCloser(bytes.NewReader(raw))
	key := f.IdempotencyKey(r.Header.Get("Idempotency-Key"))
	if key.Validate() != nil || len(r.Header.Values("Idempotency-Key")) != 1 || r.URL.RawQuery != "" {
		return errors.New("original Variables key/query invalid")
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	index := len(w.intents)
	if index >= 4 || index != len(w.receipts) {
		return errors.New("Variables repeated or overlapped a command")
	}
	for _, old := range w.intents {
		if old.key == key {
			return errors.New("Variables reused another command key")
		}
	}
	in := projectVariableWebIntent{key: key, index: index, bodyDigest: sha256.Sum256(raw), keyDigest: sha256.Sum256([]byte(key))}
	if index == 0 {
		var body struct {
			Request vc.VariableCreate `json:"request"`
		}
		if r.Method != http.MethodPost || r.URL.Path != variableHTTPPath(w.a.p2.project.ID, "/variables") || agentWebDecode(raw, &body) != nil || body.Request.Validate() != nil {
			return errors.New("original Variable Create invalid")
		}
		v := body.Request.Fields()
		if v.Name != variableWebName || v.Description != variableWebDescription || v.Value != variableWebValue {
			return errors.New("Variable Create changed original draft")
		}
		in.kind, in.create, in.target = "create", body.Request, v.ID
	} else if index == 3 {
		var body struct {
			Expected f.Version `json:"expected_version"`
		}
		before := w.receipts[2].Fields().Variable
		if r.Method != http.MethodDelete || r.URL.Path != variableHTTPPath(w.a.p2.project.ID, "/variables/"+before.Fields().ID.String()) || agentWebDecode(raw, &body) != nil || body.Expected != 3 {
			return errors.New("Variable Delete changed original target/version")
		}
		if w.details[2] == 0 || w.lists[2] == 0 {
			return errors.New("Variable Delete preceded original current Get/List")
		}
		in.kind, in.before, in.target = "delete", before.Clone(), before.Fields().ID
	} else {
		var body struct {
			Expected f.Version         `json:"expected_version"`
			Request  vc.VariableUpdate `json:"request"`
		}
		before := w.receipts[index-1].Fields().Variable
		if r.Method != http.MethodPatch || r.URL.Path != variableHTTPPath(w.a.p2.project.ID, "/variables/"+before.Fields().ID.String()) || agentWebDecode(raw, &body) != nil || body.Request.Validate() != nil || body.Expected != f.Version(index) {
			return errors.New("Variable Update changed original target/version")
		}
		fields := body.Request.Fields()
		if fields.Name != nil || fields.Value == nil || index == 1 && (fields.Description == nil || *fields.Description != variableWebNextDescription || *fields.Value != variableWebNextValue) || index == 2 && (fields.Description != nil || *fields.Value != "") {
			return errors.New("Variable Update lost touched-field presence or explicit empty value")
		}
		if w.details[index-1] == 0 || w.lists[index-1] == 0 {
			return errors.New("next Variable write preceded original current Get/List")
		}
		in.kind, in.update, in.before, in.target = "update", body.Request, before.Clone(), before.Fields().ID
	}
	w.intents = append(w.intents, in)
	w.keys = append(w.keys, string(key))
	*r = *r.WithContext(context.WithValue(r.Context(), projectVariableWebIntentKey{}, in))
	return nil
}
func (w *projectVariablesWeb) response(r *http.Response) error {
	raw, err := w.completedResponse(r)
	if err != nil {
		return err
	}
	in, write := r.Request.Context().Value(projectVariableWebIntentKey{}).(projectVariableWebIntent)
	if write {
		var result vc.VariableMutation
		if r.StatusCode != 200 || json.Unmarshal(raw, &result) != nil || result.Validate() != nil {
			return errors.New("original Variable receipt invalid")
		}
		in.requestID, err = f.ParseID[f.Request](r.Header.Get("X-Request-ID"))
		if err != nil {
			return err
		}
		if err = w.commandFacts(r.Request.Context(), in, result); err != nil {
			return err
		}
		if err = w.currentFacts(r.Request.Context(), result, in.index+1); err != nil {
			return err
		}
		w.mu.Lock()
		w.intents[in.index] = in
		w.receipts = append(w.receipts, result.Clone())
		w.mu.Unlock()
	} else if r.Request.Method == http.MethodGet && r.StatusCode == 200 {
		if err = w.observeRead(r.Request, raw); err != nil {
			return err
		}
	}
	if r.StatusCode == 200 && (write || r.Request.Method == http.MethodGet) {
		if err = w.proof(r, raw, in, write); err != nil {
			return err
		}
	}
	r.Body = io.NopCloser(bytes.NewReader(raw))
	return nil
}
func (w *projectVariablesWeb) proof(r *http.Response, raw []byte, in projectVariableWebIntent, write bool) error {
	// completedResponse proves the original verified TLS bridge response and
	// the owned bridge handler return. Final App Join is checked separately.
	record := map[string]any{"input_hash": w.inputHash, "request_id": r.Header.Get("X-Request-ID"), "method": r.Request.Method, "url_sha256": fmt.Sprintf("%x", sha256.Sum256([]byte(r.Request.URL.RequestURI()))), "status": r.StatusCode, "content_length": len(raw), "body_sha256": fmt.Sprintf("%x", sha256.Sum256(raw)), "original_eof": true, "original_closed": true, "handler_returned": true}
	if write {
		record["kind"] = in.kind
		record["request_body_sha256"] = fmt.Sprintf("%x", in.bodyDigest)
		record["idempotency_key_sha256"] = fmt.Sprintf("%x", in.keyDigest)
		record["committed_verified"] = true
	}
	encoded, err := json.Marshal(record)
	if err != nil {
		return err
	}
	if err = os.MkdirAll(w.evidence, 0700); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(w.evidence, "project-variables-"+w.mode+"-response-"+r.Header.Get("X-Request-ID")+".json"), encoded, 0600)
}
func (w *projectVariablesWeb) observeRead(r *http.Request, raw []byte) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	prefix := variableHTTPPath(w.a.p2.project.ID, "/variables")
	count := len(w.receipts)
	if r.URL.Path == prefix {
		if r.URL.Query().Get("limit") != "50" || len(r.URL.Query()) != 1 {
			return errors.New("Variables directory changed its actual page query")
		}
		var page f.Page[vc.VariableSummary]
		if json.Unmarshal(raw, &page) != nil || page.Items == nil || page.NextCursor != "" {
			return errors.New("Variables directory not a complete safe page")
		}
		if count == 0 {
			if len(page.Items) != 0 {
				return errors.New("Variables directory was not initially empty")
			}
			return nil
		}
		if count == 4 {
			if len(page.Items) != 0 {
				return errors.New("deleted Variable remained in current directory")
			}
			w.lists[3]++
			return nil
		}
		expected := w.receipts[count-1].Fields().Variable.Summary()
		if len(page.Items) != 1 || !bytes.Equal(jsonAgentWeb(page.Items[0]), jsonAgentWeb(expected)) {
			return errors.New("Variable summary lost exact metadata or exposed value")
		}
		w.lists[count-1]++
	} else if strings.HasPrefix(r.URL.Path, prefix+"/") {
		if count == 0 || count == 4 || r.URL.Path != prefix+"/"+w.receipts[count-1].Fields().Variable.Fields().ID.String() || r.URL.RawQuery != "" {
			return errors.New("Variable detail read a foreign identity")
		}
		var detail vc.Variable
		if json.Unmarshal(raw, &detail) != nil || !bytes.Equal(jsonAgentWeb(detail), jsonAgentWeb(w.receipts[count-1].Fields().Variable)) {
			return errors.New("Variable detail differs from current complete receipt")
		}
		w.details[count-1]++
	}
	return nil
}
func (w *projectVariablesWeb) unchanged(ctx context.Context) (string, error) {
	var snapshot string
	err := w.a.p2.base.raw.QueryRow(ctx, `SELECT jsonb_build_object(
 'project',(SELECT to_jsonb(p) FROM agenteam_project.projects p WHERE id=$1::text::uuid),
 'milestones',(SELECT jsonb_agg(to_jsonb(m) ORDER BY id) FROM agenteam_work.milestones m WHERE project_id=$1::text::uuid),
 'sprints',(SELECT jsonb_agg(to_jsonb(s) ORDER BY id) FROM agenteam_work.sprints s WHERE project_id=$1::text::uuid),
 'tasks',(SELECT jsonb_agg(to_jsonb(t) ORDER BY id) FROM agenteam_work.tasks t WHERE project_id=$1::text::uuid),
 'secret_commands',(SELECT jsonb_agg(to_jsonb(s) ORDER BY id) FROM agenteam_projectvariable.secret_commands s WHERE project_id=$1::text::uuid),
 'dispatches',(SELECT count(*) FROM agenteam_scheduler.dispatches WHERE project_id=$1::text),
 'executions',(SELECT count(*) FROM agenteam_execution.executions WHERE project_id=$1::text))::text`, w.a.p2.project.ID.String()).Scan(&snapshot)
	return snapshot, err
}
func (w *projectVariablesWeb) commandFacts(ctx context.Context, in projectVariableWebIntent, result vc.VariableMutation) error {
	m := result.Fields()
	v := m.Variable.Fields()
	deleting := in.kind == "delete"
	if deleting {
		if m.Deleted == nil || m.Deleted.ID != in.target || m.Deleted.ProjectID != w.a.p2.project.ID || m.Deleted.Version != 4 {
			return errors.New("Variable tombstone changed identity/version")
		}
		v = in.before.Fields()
		v.Version = m.Deleted.Version
		v.UpdatedAt = m.Deleted.DeletedAt
		v.Value = ""
		v.Description = ""
	}
	postimage, err := vc.NewVariable(v)
	if err != nil {
		return err
	}
	command, request, fields, change := vc.UpdateCommand, any(in.update), []string{"value"}, vc.Updated
	expected := in.before.Fields()
	expected.Version = f.Version(in.index + 1)
	expected.UpdatedAt = v.UpdatedAt
	var before *vc.Variable
	if in.index == 0 {
		command, request, fields, change = vc.CreateCommand, in.create, []string{"created"}, vc.Created
		expected = vc.VariableFields{ID: in.target, ProjectID: w.a.p2.project.ID, Type: vc.VariableType, Name: variableWebName, Description: variableWebDescription, Value: variableWebValue, Version: 1, CreatedAt: v.CreatedAt, UpdatedAt: v.CreatedAt}
	} else {
		b := in.before.Clone()
		before = &b
		expected.Value = ""
		if in.index == 1 {
			expected.Description, expected.Value = variableWebNextDescription, variableWebNextValue
			fields = []string{"description", "value"}
		}
		if deleting {
			command, request, fields, change = vc.DeleteCommand, nil, []string{"deleted"}, vc.Deleted
			expected.Description = ""
		}
	}
	if result.Validate() != nil || m.Command != command || !m.Changed || (m.Deleted != nil) != deleting || m.EventID == nil || m.AuditID == nil || !reflect.DeepEqual(v, expected) {
		return errors.New("Variable receipt lost original full postimage")
	}
	meta := f.CommandMeta{RequestID: in.requestID, IdempotencyKey: in.key}
	if in.index > 0 {
		version := f.Version(in.index)
		meta.ExpectedVersion = &version
	}
	actor := w.a.p2.base.ownerBrowser.actor
	digest, err := vc.VariableCommandDigest(actor, meta, w.a.p2.project.ID, in.target, command, request)
	if err != nil {
		return err
	}
	identity, err := vc.VariableCommandIdentity(w.a.p2.project.ID, command, in.key)
	if err != nil {
		return err
	}
	cause, err := cursor.Digest([]byte(identity.Canonical()))
	if err != nil {
		return err
	}
	var operation, event, user, semantic string
	var requestRaw, planRaw, receiptRaw []byte
	var revision int64
	err = w.a.p2.base.raw.QueryRow(ctx, `SELECT id::text,event_id::text,actor_user_id::text,semantic_digest,request,plan,receipt,plan_revision FROM agenteam_projectvariable.commands WHERE project_id=$1::text::uuid AND target_id=$2::text::uuid AND command_name=$3 AND idempotency_key=$4 AND state='completed' AND committed_at IS NOT NULL`, w.a.p2.project.ID.String(), in.target.String(), string(command), string(in.key)).Scan(&operation, &event, &user, &semantic, &requestRaw, &planRaw, &receiptRaw, &revision)
	if err != nil {
		return err
	}
	input := map[string]any{"project_id": w.a.p2.project.ID, "actor_user_id": actor.Details().UserID, "command": command, "target_id": in.target}
	if in.index == 0 {
		input["create"] = in.create
	} else {
		input["expected_version"] = f.Version(in.index)
		if !deleting {
			input["update"] = in.update
		}
	}
	var actualInput map[string]any
	var stored vc.VariableMutation
	var plan struct {
		Before    *vc.Variable   `json:"before"`
		After     vc.Variable    `json:"after"`
		Deleted   bool           `json:"deleted"`
		Fields    []string       `json:"changed_fields"`
		HistoryID vc.OperationID `json:"history_id"`
	}
	if json.Unmarshal(requestRaw, &actualInput) != nil || !projectVariableWebEqual(actualInput, input) || json.Unmarshal(receiptRaw, &stored) != nil || !bytes.Equal(jsonAgentWeb(stored), jsonAgentWeb(result)) || json.Unmarshal(planRaw, &plan) != nil || !bytes.Equal(jsonAgentWeb(plan.Before), jsonAgentWeb(before)) || !bytes.Equal(jsonAgentWeb(plan.After), jsonAgentWeb(postimage)) || plan.Deleted != deleting || !slices.Equal(plan.Fields, fields) || plan.HistoryID.Validate() != nil || revision < 1 || semantic != digest.String() || user != actor.Details().UserID || event != m.EventID.String() {
		return errors.New("Variable original command/plan/receipt mismatch")
	}
	var history bool
	err = w.a.p2.base.raw.QueryRow(ctx, `SELECT id=$4::text::uuid AND operation_id=$3::text::uuid AND version=$5 AND kind=$6 AND changed_fields=$7::jsonb AND actor_user_id=$8::text::uuid AND occurred_at=$9 AND event_id=$10::text::uuid FROM agenteam_projectvariable.history WHERE project_id=$1::text::uuid AND variable_id=$2::text::uuid AND version=$5`, w.a.p2.project.ID.String(), in.target.String(), operation, plan.HistoryID.String(), int64(v.Version), string(change), string(jsonAgentWeb(fields)), user, v.UpdatedAt.Time(), event).Scan(&history)
	if err != nil {
		return err
	}
	if !history {
		return errors.New("Variable history differs from original plan")
	}
	var auditRaw, headerRaw, payloadRaw []byte
	var auditColumns, eventColumns bool
	err = w.a.p2.base.raw.QueryRow(ctx, `SELECT metadata,scope='project' AND actor_kind='human' AND user_id=$4::text::uuid AND session_id=$5::text::uuid AND action=$6 AND outcome='success' AND resource_kind='project_variable' AND cause_ref=$7 AND ordinal=0 FROM agenteam_audit.audit_records WHERE id=$3::text::uuid AND project_id=$1::text::uuid AND resource_id=$2::text::uuid AND producer='projectvariable'`, w.a.p2.project.ID.String(), in.target.String(), m.AuditID.String(), user, actor.Details().SessionID, string(command), cause.String()).Scan(&auditRaw, &auditColumns)
	if err != nil {
		return err
	}
	var audit map[string]any
	if !auditColumns || json.Unmarshal(auditRaw, &audit) != nil || !projectVariableWebEqual(audit, map[string]any{"variable_id": in.target, "version": v.Version, "changed_fields": fields}) {
		return errors.New("Variable Audit lost original identity or leaked user material")
	}
	err = w.a.p2.base.raw.QueryRow(ctx, `SELECT header,payload,event_type='project.variable_changed' AND schema_version=1 AND scope='project' AND aggregate_type='project.variable' AND aggregate_version=$4 AND aggregate_sequence IS NULL AND occurred_at=$5 FROM agenteam_outbox.events WHERE project_id=$1::text::uuid AND aggregate_id=$2::text::uuid AND id=$3::text::uuid AND producer='projectvariable'`, w.a.p2.project.ID.String(), in.target.String(), event, int64(v.Version), v.UpdatedAt.Time()).Scan(&headerRaw, &payloadRaw, &eventColumns)
	if err != nil {
		return err
	}
	header, err := eventc.DecodeHeader(headerRaw)
	var payload vc.VariableChanged
	if err != nil || !eventColumns || json.Unmarshal(payloadRaw, &payload) != nil || payload.Validate() != nil || header.EventID != *m.EventID || header.Scope.ProjectID.String() != w.a.p2.project.ID.String() || header.AggregateID.String() != in.target.String() || header.AggregateVersion == nil || *header.AggregateVersion != v.Version || payload.VariableID != in.target || payload.OperationID.String() != operation || payload.Change != change || !slices.Equal(payload.ChangedFields, fields) {
		return errors.New("Variable Outbox lost original command/version/fields")
	}
	return nil
}
func (w *projectVariablesWeb) currentFacts(ctx context.Context, result vc.VariableMutation, count int) error {
	m := result.Fields()
	v := m.Variable.Fields()
	if m.Deleted != nil {
		w.mu.Lock()
		v = w.intents[count-1].before.Fields()
		w.mu.Unlock()
		v.Version = m.Deleted.Version
		v.UpdatedAt = m.Deleted.DeletedAt
		v.Value = ""
		v.Description = ""
	}
	var deletedAt any
	if m.Deleted != nil {
		deletedAt = m.Deleted.DeletedAt.Time()
	}
	var canonical bool
	err := w.a.p2.base.raw.QueryRow(ctx, `SELECT type='variable' AND name=$3 AND description=$4 AND value=$5 AND version=$6 AND created_at=$7 AND updated_at=$8 AND deleted_at IS NOT DISTINCT FROM $9::timestamptz FROM agenteam_projectvariable.variables WHERE project_id=$1::text::uuid AND id=$2::text::uuid`, w.a.p2.project.ID.String(), v.ID.String(), v.Name, v.Description, v.Value, int64(v.Version), v.CreatedAt.Time(), v.UpdatedAt.Time(), deletedAt).Scan(&canonical)
	if err != nil {
		return err
	}
	if !canonical {
		return errors.New("Variable canonical current row differs from complete receipt")
	}
	var facts [6]int64
	err = w.a.p2.base.raw.QueryRow(ctx, `SELECT
 (SELECT count(*) FROM agenteam_projectvariable.variables WHERE project_id=$1::text::uuid),
 (SELECT count(*) FROM agenteam_projectvariable.commands WHERE project_id=$1::text::uuid),
 (SELECT count(*) FROM agenteam_projectvariable.history WHERE project_id=$1::text::uuid),
 (SELECT count(*) FROM agenteam_audit.audit_records WHERE project_id=$1::text::uuid AND producer='projectvariable'),
 (SELECT count(*) FROM agenteam_outbox.events WHERE project_id=$1::text::uuid AND producer='projectvariable'),
 (SELECT query_generation FROM agenteam_projectvariable.project_generations WHERE project_id=$1::text::uuid)`, w.a.p2.project.ID.String()).Scan(&facts[0], &facts[1], &facts[2], &facts[3], &facts[4], &facts[5])
	if err != nil {
		return err
	}
	n := int64(count)
	if facts != ([6]int64{1, n, n, n, n, n + 1}) {
		return errors.New("Variable commit duplicated or omitted atomic facts")
	}
	snapshot, err := w.unchanged(ctx)
	if err != nil {
		return err
	}
	w.mu.Lock()
	immutable := w.immutable
	w.mu.Unlock()
	if snapshot != immutable {
		return errors.New("Variable writes changed Project/Work/Secret/runtime facts")
	}
	return nil
}
func (w *projectVariablesWeb) verify(t *testing.T, ctx context.Context) {
	w.mu.Lock()
	failure := w.failure
	intents := slices.Clone(w.intents)
	receipts := slices.Clone(w.receipts)
	details, lists := w.details, w.lists
	w.mu.Unlock()
	if failure != nil || len(intents) != 4 || len(receipts) != 4 || w.cut != 0 {
		t.Fatal("Variables browser original commands incomplete", failure)
	}
	for n, in := range intents {
		if n < 3 && details[n] < 1 || n == 3 && details[n] != 0 || lists[n] < 1 {
			t.Fatal("Variables original Get/List absent for committed version")
		}
		agentAppRequire(t, w.commandFacts(ctx, in, receipts[n]))
		command := vc.CreateCommand
		body := map[string]any{"command": command, "request": in.create}
		if n > 0 {
			command = vc.UpdateCommand
			body = map[string]any{"command": command, "target_id": in.target, "expected_version": f.Version(n), "request": in.update}
		}
		if n == 3 {
			body = map[string]any{"command": vc.DeleteCommand, "target_id": in.target, "expected_version": f.Version(3)}
		}
		reply := w.request(t, w.a.p2.base.ownerBrowser, http.MethodPost, variableHTTPPath(w.a.p2.project.ID, "/variables/commands/lookup"), jsonBytes(t, body), in.key)
		var found vc.VariableCommandLookup
		if reply.status != 200 || json.Unmarshal(reply.body, &found) != nil || found.Validate() != nil || found.Status() != vc.LookupCommitted || found.Receipt() == nil || !bytes.Equal(jsonAgentWeb(*found.Receipt()), jsonAgentWeb(receipts[n])) {
			t.Fatal("Variable original-key Lookup lost historical receipt", reply.status)
		}
		agentAppRequire(t, w.commandFacts(ctx, in, receipts[n]))
		agentAppRequire(t, w.currentFacts(ctx, receipts[3], 4))
	}
	reply := w.request(t, w.a.p2.base.ownerBrowser, http.MethodGet, variableHTTPPath(w.a.p2.project.ID, "/variables/"+intents[3].target.String()), nil, "")
	if reply.status != http.StatusNotFound {
		t.Fatal("deleted Variable remained publicly readable", reply.status)
	}
	agentAppRequire(t, w.currentFacts(ctx, receipts[3], 4))
	t.Log("PROJECT_VARIABLE_DELETE confirmed_tombstone=true original_lookup=true unique_delete=true current_get_not_found=true")
}

func projectVariableWebEqual(a, b any) bool {
	var left, right any
	ar, ae := json.Marshal(a)
	br, be := json.Marshal(b)
	return ae == nil && be == nil && json.Unmarshal(ar, &left) == nil && json.Unmarshal(br, &right) == nil && reflect.DeepEqual(left, right)
}
