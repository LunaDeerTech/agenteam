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

	eventc "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/work"
	wc "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
)

// One normal browser command creates a planned sibling while the original
// current Sprint remains active. The initialized Project is fixture setup.
func TestSprintCreateWeb(t *testing.T) {
	t.Run("plan-another-sprint-with-existing-current", func(t *testing.T) {
		entered, completed := false, false
		t.Cleanup(func() {
			if !entered || !completed {
				t.Error("Sprint Create original App handoff incomplete")
			}
		})
		a := newAgentCreateFixture(t)
		original := a.p2.base
		projection := *original
		projection.project, projection.projectAuthority = a.p2.project, a.providers.Projects
		_, task := newPreparationTaskInput(t, &projection)
		authority, err := work.NewAuthority(original.tracked, a.providers.Projects)
		agentAppRequire(t, err)
		reader, err := work.NewReader(original.tracked, authority, original.keys)
		agentAppRequire(t, err)
		sprint, err := reader.GetSprint(ctxFor(t), original.ownerBrowser.actor, a.p2.project.ID, task.SprintID)
		agentAppRequire(t, err)
		parent, err := reader.GetMilestone(ctxFor(t), original.ownerBrowser.actor, a.p2.project.ID, sprint.MilestoneID)
		agentAppRequire(t, err)
		project, err := a.p2.creator.GetSchedulerConfig(ctxFor(t), original.ownerBrowser.actor, a.p2.project.ID)
		agentAppRequire(t, err)
		if sprint.State != wc.Planned || sprint.Version != 1 || project.Config.Enabled || project.Project.CurrentSprintID != nil || task.State != wc.TaskStateBacklog || task.AssigneeAgentID != nil {
			t.Fatal("invalid original paused preparation")
		}
		process, err := a.guard.CurrentProcess()
		agentAppRequire(t, err)
		var username string
		agentAppRequire(t, original.raw.QueryRow(ctxFor(t), `SELECT username FROM agenteam_account.users WHERE id=$1`, original.ownerBrowser.actor.Details().UserID).Scan(&username))
		if original.afterOwners != nil {
			t.Fatal("original handoff occupied")
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
			w := newSprintCreateWeb(t, a, sprint, parent, project, task)
			defer w.dispose(t)
			withAgentConfigurationApp(t, a, process.String(), w.origin, func(ctx context.Context, address string) {
				defer w.close(t)
				w.start(t, address)
				w.prepareCurrent(t, ctx)
				w.material(t, username)
				w.browser(t, ctx)
				w.verify(t, ctx)
				w.close(t)
			})
			completed = true
		}
	})
}

const sprintCreateTitle = "Next planned iteration"
const sprintCreateDescription = "Plan the next iteration.\nKeep the current Sprint unchanged."

type sprintCreateIntent struct {
	sprintWebIntent
	request   wc.CreateSprintRequest
	requestID f.ID[f.Request]
}
type sprintCreateIntentKey struct{}
type sprintCreateWeb struct {
	*sprintStartWeb
	parent                                                 wc.Milestone
	generation                                             f.Version
	immutable                                              string
	created                                                wc.StructureMutation
	createIntent                                           sprintCreateIntent
	creates, newReads, parentReads, listReads, emptyBoards int
}

func newSprintCreateWeb(t *testing.T, a *agentCreateFixture, sprint wc.Sprint, parent wc.Milestone, project pc.SchedulerProject, task wc.Task) *sprintCreateWeb {
	t.Helper()
	const prefix = "AGENTEAM_SPRINT_CREATE_WEB"
	dist, runtime, evidence, inputHash := os.Getenv(prefix+"_DIST"), os.Getenv("AGENTEAM_AUTH_WEB_RUNTIME"), os.Getenv(prefix+"_EVIDENCE"), os.Getenv(prefix+"_INPUT_HASH")
	if !filepath.IsAbs(dist) || !filepath.IsAbs(runtime) || len(runtime) > 45 || !filepath.IsAbs(evidence) || len(inputHash) != 64 || strings.Trim(inputHash, "0123456789abcdef") != "" {
		t.Fatal("frozen Sprint Create inputs required")
	}
	if info, err := os.Stat(filepath.Join(dist, "index.html")); err != nil || !info.Mode().IsRegular() {
		t.Fatal("frozen index missing")
	}
	directory, err := os.MkdirTemp(runtime, "scw-")
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
	base := &agentConfigurationWeb{a: a, mode: "plan-another-sprint-with-existing-current", origin: "http://" + listener.Addr().String(), directory: directory, evidence: evidence, inputHash: inputHash, dist: dist, listener: listener, serve: make(chan error, 1), tails: map[string]*reviewWebTail{}}
	w := &sprintCreateWeb{sprintStartWeb: &sprintStartWeb{agentConfigurationWeb: base, before: sprint.Clone(), project: project.Clone(), task: task.Clone()}, parent: parent}
	published = true
	return w
}
func (w *sprintCreateWeb) request(t *testing.T, method, path string, body []byte, key f.IdempotencyKey) variableHTTPResponse {
	return (&agentEditWeb{agentConfigurationWeb: w.agentConfigurationWeb}).request(t, w.a.p2.base.ownerBrowser, method, path, body, key)
}
func (w *sprintCreateWeb) prepareCurrent(t *testing.T, ctx context.Context) {
	const key f.IdempotencyKey = "sprint-create-original-current-start"
	reply := w.request(t, http.MethodPost, w.sprintPath()+"/start", jsonBytes(t, map[string]any{"expected_version": w.before.Version, "request": map[string]any{}}), key)
	var result wc.SprintStartMutation
	if reply.status != 200 || json.Unmarshal(reply.body, &result) != nil || result.Validate() != nil {
		t.Fatal("formal App Start setup failed", reply.status)
	}
	agentAppRequire(t, w.sprintStartWeb.committedFacts(ctx, sprintWebIntent{kind: "start", key: key}, result))
	var generation f.Version
	agentAppRequire(t, w.a.p2.base.raw.QueryRow(ctx, `SELECT order_generation FROM agenteam_work.sprint_order_groups WHERE project_id=$1::text::uuid AND milestone_id=$2::text::uuid`, w.project.Project.ID.String(), w.parent.ID.String()).Scan(&generation))
	immutable, err := w.unchanged(ctx)
	agentAppRequire(t, err)
	// Publish one complete preparation. Every response acquires this same mutex
	// in completedResponse before reading these immutable preparation facts.
	w.mu.Lock()
	w.result, w.generation, w.immutable = result.Clone(), generation, immutable
	w.mu.Unlock()
}
func (w *sprintCreateWeb) material(t *testing.T, username string) {
	raw := jsonBytes(t, map[string]any{"mode": w.mode, "cookie": w.a.p2.base.ownerBrowser.cookie, "project_id": w.project.Project.ID, "milestone_id": w.parent.ID, "milestone_title": w.parent.Title, "current_sprint_id": w.before.ID, "current_sprint_title": w.before.Title, "project_version": w.result.Project.Version, "route": "/" + username + "/" + w.project.Project.NormalizedName + "/tasks", "title": sprintCreateTitle, "description": sprintCreateDescription, "task_title": w.task.Title})
	agentAppRequire(t, os.WriteFile(filepath.Join(w.directory, "sprint-create-material.json"), raw, 0600))
	clear(raw)
}
func (w *sprintCreateWeb) start(t *testing.T, address string) {
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
				http.Error(out, "owned Sprint observation failed", 500)
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

func (w *sprintCreateWeb) browser(t *testing.T, parent context.Context) {
	t.Helper()
	root, err := filepath.Abs("../account-captcha-web")
	agentAppRequire(t, err)
	ctx, cancel := context.WithTimeout(parent, 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "node", filepath.Join(root, "node_modules/@playwright/test/cli.js"), "test", "--config", filepath.Join(root, "sprint-create.config.js"))
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
	const prefix = "AGENTEAM_SPRINT_CREATE_WEB"
	cmd.Env = append(cmd.Env, "TMPDIR="+w.directory, "PLAYWRIGHT_NO_COPY_PROMPT=1", "AGENTEAM_AUTH_WEB_ORIGIN="+w.origin, "AGENTEAM_AUTH_WEB_PRIVATE="+w.directory, "AGENTEAM_AUTH_WEB_CHROMIUM=/usr/bin/chromium", prefix+"_DIST="+w.dist, prefix+"_CASE="+w.mode, prefix+"_EVIDENCE="+w.evidence, prefix+"_INPUT_HASH="+w.inputHash)
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	if err = cmd.Start(); err != nil {
		t.Fatal("owned Sprint Playwright did not start")
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
	t.Logf("Sprint Create Node actual_wait pid=%d success=%t", cmd.Process.Pid, err == nil)
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
		t.Fatal("original Sprint browser call failed")
	}
}

func (w *sprintCreateWeb) observe(r *http.Request) error {
	if r.Method == http.MethodGet || r.Method == http.MethodHead {
		return nil
	}
	if r.Method != http.MethodPost || r.URL.Path != variableHTTPPath(w.project.Project.ID, "/sprints") || r.URL.RawQuery != "" {
		return errors.New("undeclared Sprint Create write")
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, (16<<10)+1))
	closed := r.Body.Close()
	if err != nil || closed != nil || len(raw) > 16<<10 {
		return errors.New("original Sprint Create request not complete")
	}
	r.Body = io.NopCloser(bytes.NewReader(raw))
	var envelope struct {
		Request wc.CreateSprintRequest `json:"request"`
	}
	key := f.IdempotencyKey(r.Header.Get("Idempotency-Key"))
	if key.Validate() != nil || len(r.Header.Values("Idempotency-Key")) != 1 || agentWebDecode(raw, &envelope) != nil || envelope.Request.Validate() != nil {
		return errors.New("invalid original Sprint Create envelope")
	}
	request := envelope.Request
	if request.SprintID == w.before.ID || request.MilestoneID != w.parent.ID || request.Title != sprintCreateTitle || request.Description != sprintCreateDescription {
		return errors.New("browser changed Sprint Create intent")
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.creates != 0 {
		return errors.New("browser repeated Sprint Create")
	}
	in := sprintCreateIntent{sprintWebIntent: sprintWebIntent{kind: "create", key: key, bodyDigest: sha256.Sum256(raw), keyDigest: sha256.Sum256([]byte(key))}, request: request}
	w.creates++
	w.keys = append(w.keys, string(key))
	w.createIntent = in
	*r = *r.WithContext(context.WithValue(r.Context(), sprintCreateIntentKey{}, in))
	return nil
}
func (w *sprintCreateWeb) response(r *http.Response) error {
	raw, err := w.completedResponse(r)
	if err != nil {
		return err
	}
	intent, write := r.Request.Context().Value(sprintCreateIntentKey{}).(sprintCreateIntent)
	if write {
		var result wc.StructureMutation
		if r.StatusCode != 200 || json.Unmarshal(raw, &result) != nil || result.Validate() != nil {
			return errors.New("original Create response invalid")
		}
		intent.requestID, err = f.ParseID[f.Request](r.Header.Get("X-Request-ID"))
		if err != nil {
			return err
		}
		if err = w.committedFacts(r.Request.Context(), intent, result); err != nil {
			return err
		}
		w.mu.Lock()
		w.created = result.Clone()
		w.createIntent = intent
		w.mu.Unlock()
	} else if r.Request.Method == http.MethodGet && r.StatusCode == 200 {
		if err = w.observeRead(r.Request, raw); err != nil {
			return err
		}
	}
	if r.StatusCode == 200 && (write || r.Request.Method == http.MethodGet) {
		if err = w.proof(r, raw, intent.sprintWebIntent, write); err != nil {
			return err
		}
	}
	r.Body = io.NopCloser(bytes.NewReader(raw))
	return nil
}
func (w *sprintCreateWeb) proof(r *http.Response, raw []byte, intent sprintWebIntent, write bool) error {
	record := map[string]any{"input_hash": w.inputHash, "request_id": r.Header.Get("X-Request-ID"), "method": r.Request.Method, "url_sha256": fmt.Sprintf("%x", sha256.Sum256([]byte(r.Request.URL.RequestURI()))), "status": r.StatusCode, "content_length": len(raw), "body_sha256": fmt.Sprintf("%x", sha256.Sum256(raw)), "original_eof": true, "original_closed": true, "handler_returned": true}
	if write {
		record["kind"] = intent.kind
		record["request_body_sha256"] = fmt.Sprintf("%x", intent.bodyDigest)
		record["idempotency_key_sha256"] = fmt.Sprintf("%x", intent.keyDigest)
		record["committed_verified"] = true
	}
	encoded, err := json.Marshal(record)
	if err != nil {
		return err
	}
	if err = os.MkdirAll(w.evidence, 0700); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(w.evidence, "sprint-create-"+w.mode+"-response-"+r.Header.Get("X-Request-ID")+".json"), encoded, 0600)
}

func sprintCreateEqual(a, b any) bool {
	x, e := json.Marshal(a)
	if e != nil {
		return false
	}
	y, e := json.Marshal(b)
	if e != nil {
		return false
	}
	var left, right any
	return json.Unmarshal(x, &left) == nil && json.Unmarshal(y, &right) == nil && reflect.DeepEqual(left, right)
}
func sprintCreateSummary(v any, omit ...string) (map[string]any, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	var fields map[string]any
	if err = json.Unmarshal(raw, &fields); err != nil {
		return nil, err
	}
	for _, key := range omit {
		delete(fields, key)
	}
	return fields, nil
}
func (w *sprintCreateWeb) observeRead(r *http.Request, raw []byte) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	p := w.project.Project.ID
	switch r.URL.Path {
	case variableHTTPPath(p, ""):
		var project pc.ProjectRef
		if json.Unmarshal(raw, &project) != nil || !reflect.DeepEqual(project, w.result.Project) {
			return errors.New("Create changed actual Project current/version")
		}
		w.projectReads++
	case w.sprintPath():
		var sprint wc.Sprint
		if json.Unmarshal(raw, &sprint) != nil || !reflect.DeepEqual(sprint, w.result.Sprint) {
			return errors.New("Create changed original current Sprint")
		}
	case variableHTTPPath(p, "/milestones/"+w.parent.ID.String()):
		var parent wc.Milestone
		if json.Unmarshal(raw, &parent) != nil || !reflect.DeepEqual(parent, w.parent) {
			return errors.New("Create changed parent Milestone")
		}
		if w.created.Validate() == nil {
			w.parentReads++
		}
	case variableHTTPPath(p, "/sprints"):
		if r.URL.Query().Get("milestone_id") != w.parent.ID.String() {
			return errors.New("Sprint list used another parent")
		}
		var page f.Page[map[string]any]
		if json.Unmarshal(raw, &page) != nil || page.Items == nil || page.NextCursor != "" {
			return errors.New("invalid original Sprint list")
		}
		expected := []map[string]any{}
		for _, value := range []wc.Sprint{w.result.Sprint} {
			v, err := sprintCreateSummary(value, "description", "started_at", "started_by", "completed_at", "completed_by")
			if err != nil {
				return err
			}
			expected = append(expected, v)
		}
		if w.created.Validate() == nil {
			v, err := sprintCreateSummary(*w.created.Sprint, "description", "started_at", "started_by", "completed_at", "completed_by")
			if err != nil {
				return err
			}
			expected = append(expected, v)
			w.listReads++
		}
		if !reflect.DeepEqual(page.Items, expected) {
			return errors.New("Sprint list lost original ordered sibling summaries")
		}
	case variableHTTPPath(p, "/milestones"):
		var page f.Page[map[string]any]
		expected, err := sprintCreateSummary(w.parent, "description")
		if err != nil || json.Unmarshal(raw, &page) != nil || page.NextCursor != "" || !reflect.DeepEqual(page.Items, []map[string]any{expected}) {
			return errors.New("original Milestone list changed")
		}
	case variableHTTPPath(p, "/tasks"):
		var page f.Page[map[string]any]
		if json.Unmarshal(raw, &page) != nil || page.Items == nil || page.NextCursor != "" {
			return errors.New("invalid original Task summary page")
		}
		sprint := r.URL.Query().Get("sprint_id")
		if w.created.Validate() == nil && sprint == w.created.Sprint.ID.String() {
			if len(page.Items) != 0 {
				return errors.New("new planned Sprint has unexpected Task")
			}
			w.emptyBoards++
		} else if sprint == w.before.ID.String() {
			expected := []map[string]any{}
			if r.URL.Query().Get("state") == string(wc.TaskStateBacklog) {
				v, err := sprintCreateSummary(w.task, "description", "plan")
				if err != nil {
					return err
				}
				expected = append(expected, v)
			}
			if !reflect.DeepEqual(page.Items, expected) {
				return errors.New("Create changed original current Task board")
			}
		} else {
			return errors.New("board read another Sprint")
		}
	default:
		if w.created.Validate() == nil && r.URL.Path == variableHTTPPath(p, "/sprints/"+w.created.Sprint.ID.String()) {
			var sprint wc.Sprint
			if json.Unmarshal(raw, &sprint) != nil || !reflect.DeepEqual(sprint, *w.created.Sprint) {
				return errors.New("new Sprint Get differs from original Create receipt")
			}
			w.newReads++
		}
	}
	return nil
}
func (w *sprintCreateWeb) unchanged(ctx context.Context) (string, error) {
	var snapshot string
	err := w.a.p2.base.raw.QueryRow(ctx, `SELECT jsonb_build_object(
 'project',(SELECT to_jsonb(p) FROM agenteam_project.projects p WHERE id=$1::text::uuid),
 'parent',(SELECT to_jsonb(m) FROM agenteam_work.milestones m WHERE project_id=$1::text::uuid AND id=$2::text::uuid),
 'current',(SELECT to_jsonb(s) FROM agenteam_work.sprints s WHERE project_id=$1::text::uuid AND id=$3::text::uuid),
 'tasks',(SELECT jsonb_agg(to_jsonb(t) ORDER BY id) FROM agenteam_work.tasks t WHERE project_id=$1::text::uuid),
 'starts',(SELECT jsonb_agg(to_jsonb(c) ORDER BY id) FROM agenteam_work.sprint_start_commands c WHERE project_id=$1::text::uuid),
 'dispatches',(SELECT count(*) FROM agenteam_scheduler.dispatches WHERE project_id=$1::text),
 'executions',(SELECT count(*) FROM agenteam_execution.executions WHERE project_id=$1::text))::text`, w.project.Project.ID.String(), w.parent.ID.String(), w.before.ID.String()).Scan(&snapshot)
	return snapshot, err
}
func (w *sprintCreateWeb) committedFacts(ctx context.Context, in sprintCreateIntent, result wc.StructureMutation) error {
	if result.Validate() != nil || result.Command != wc.SprintCreate || !result.Changed || result.Milestone != nil || result.Sprint == nil || result.EventID == nil {
		return errors.New("Create lost its original complete Structure receipt")
	}
	v := result.Sprint
	expected := wc.Sprint{ID: in.request.SprintID, ProjectID: w.project.Project.ID, MilestoneID: w.parent.ID, Title: in.request.Title, Description: in.request.Description, ManualRank: v.ManualRank, Version: 1, CreatedAt: v.CreatedAt, UpdatedAt: v.CreatedAt, State: wc.Planned}
	if !reflect.DeepEqual(*v, expected) || v.ManualRank <= w.result.Sprint.ManualRank || v.CreatedAt.Time().Before(w.result.Sprint.UpdatedAt.Time()) {
		return errors.New("Create did not append a new planned v1 sibling")
	}
	actor := w.a.p2.base.ownerBrowser.actor
	semantic, err := wc.CreateSprintDigest(actor, f.CommandMeta{RequestID: in.requestID, IdempotencyKey: in.key}, w.project.Project.ID, in.request)
	if err != nil {
		return err
	}
	var commandID, eventID, user, digest string
	var requestRaw, receiptRaw, planRaw []byte
	var revision int64
	err = w.a.p2.base.raw.QueryRow(ctx, `SELECT id::text,event_id::text,actor_user_id::text,semantic_digest,request,receipt,plan,plan_revision FROM agenteam_work.structure_commands WHERE project_id=$1::text::uuid AND command_name='work.sprint.create' AND idempotency_key=$2 AND state='completed' AND committed_at IS NOT NULL`, w.project.Project.ID.String(), string(in.key)).Scan(&commandID, &eventID, &user, &digest, &requestRaw, &receiptRaw, &planRaw, &revision)
	if err != nil {
		return err
	}
	var input map[string]any
	var stored wc.StructureMutation
	var plan struct {
		BeforeMilestone *wc.Milestone        `json:"before_milestone"`
		BeforeSprint    *wc.Sprint           `json:"before_sprint"`
		After           wc.StructureMutation `json:"after"`
		GroupBefore     *f.Version           `json:"group_before"`
		Header          eventc.Header        `json:"header"`
		Payload         wc.SprintChanged     `json:"payload"`
	}
	expectedInput := map[string]any{"command": wc.SprintCreate, "project_id": w.project.Project.ID, "target_id": v.ID.String(), "actor_user_id": actor.Details().UserID, "expected_version": nil, "create_milestone": nil, "create_sprint": in.request, "update": nil, "reorder_milestone": nil, "reorder_sprint": nil}
	if json.Unmarshal(requestRaw, &input) != nil || !sprintCreateEqual(input, expectedInput) || json.Unmarshal(receiptRaw, &stored) != nil || !reflect.DeepEqual(stored, result) || json.Unmarshal(planRaw, &plan) != nil || plan.BeforeMilestone != nil || plan.BeforeSprint != nil || plan.GroupBefore == nil || *plan.GroupBefore != w.generation || !reflect.DeepEqual(plan.After, result) || revision < 1 || digest != semantic.String() || user != actor.Details().UserID || eventID != result.EventID.String() {
		return errors.New("Create command/plan differs from original identity and full intent")
	}
	h, p := plan.Header, plan.Payload
	if h.Validate() != nil || p.Validate() != nil || h.EventID != *result.EventID || h.EventType != wc.SprintChangedName || h.AggregateType != wc.SprintAggregate || h.AggregateID.String() != v.ID.String() || h.AggregateVersion == nil || *h.AggregateVersion != 1 || h.AggregateSequence != nil || h.SchemaVersion != wc.WorkSchemaVersion || h.Scope.Kind != eventc.ProjectScope || h.Scope.ProjectID.String() != w.project.Project.ID.String() || h.OccurredAt != v.CreatedAt || p.CommandID.String() != commandID || p.ActorUserID.String() != user || p.MilestoneID != w.parent.ID || p.Change != wc.CreatedChange || !slices.Equal(p.ChangedFields, []wc.ChangedField{wc.DescriptionChanged, wc.RankChanged, wc.TitleChanged}) || p.Position == nil || p.Position.OrderGeneration != w.generation+1 || p.Position.PreviousID == nil || *p.Position.PreviousID != w.before.ID || p.Position.NextID != nil {
		return errors.New("Create event lost exact new sibling position/actor/version")
	}
	var headerRaw, payloadRaw []byte
	var eventColumns bool
	err = w.a.p2.base.raw.QueryRow(ctx, `SELECT header,payload,event_type='work.sprint_changed' AND schema_version=1 AND scope='project' AND aggregate_type='work.sprint' AND aggregate_version=1 AND aggregate_sequence IS NULL AND occurred_at=$4 FROM agenteam_outbox.events WHERE project_id=$1::text::uuid AND aggregate_id=$2::text::uuid AND id=$3::text::uuid AND producer='work'`, w.project.Project.ID.String(), v.ID.String(), eventID, v.CreatedAt.Time()).Scan(&headerRaw, &payloadRaw, &eventColumns)
	if err != nil {
		return err
	}
	actualHeader, err := eventc.DecodeHeader(headerRaw)
	var actualPayload wc.SprintChanged
	if err != nil || !eventColumns || json.Unmarshal(payloadRaw, &actualPayload) != nil || !reflect.DeepEqual(actualHeader, h) || !reflect.DeepEqual(actualPayload, p) {
		return errors.New("Create Outbox differs from original command plan")
	}
	var canonical bool
	err = w.a.p2.base.raw.QueryRow(ctx, `SELECT milestone_id=$3::text::uuid AND title=$4 AND description=$5 AND manual_rank=$6 AND version=1 AND created_at=$7 AND updated_at=$7 AND started_at IS NULL AND started_by IS NULL AND completed_at IS NULL AND completed_by IS NULL FROM agenteam_work.sprints WHERE project_id=$1::text::uuid AND id=$2::text::uuid`, w.project.Project.ID.String(), v.ID.String(), w.parent.ID.String(), v.Title, v.Description, v.ManualRank, v.CreatedAt.Time()).Scan(&canonical)
	if err != nil {
		return err
	}
	if !canonical {
		return errors.New("new canonical Sprint differs from receipt")
	}
	var facts [6]int64
	err = w.a.p2.base.raw.QueryRow(ctx, `SELECT
 (SELECT count(*) FROM agenteam_work.structure_commands WHERE project_id=$1::text::uuid AND command_name='work.sprint.create' AND request->>'target_id'=$2::text),
 (SELECT count(*) FROM agenteam_outbox.events WHERE project_id=$1::text::uuid AND aggregate_id=$2::text::uuid AND producer='work'),
 (SELECT order_generation FROM agenteam_work.sprint_order_groups WHERE project_id=$1::text::uuid AND milestone_id=$3::text::uuid),
 (SELECT count(*) FROM agenteam_work.sprint_start_commands WHERE project_id=$1::text::uuid),
 (SELECT count(*) FROM agenteam_scheduler.dispatches WHERE project_id=$1::text),
 (SELECT count(*) FROM agenteam_execution.executions WHERE project_id=$1::text)`, w.project.Project.ID.String(), v.ID.String(), w.parent.ID.String()).Scan(&facts[0], &facts[1], &facts[2], &facts[3], &facts[4], &facts[5])
	if err != nil {
		return err
	}
	if facts != ([6]int64{1, 1, int64(w.generation) + 1, 1, 0, 0}) {
		return errors.New("Create duplicated command/event or started dispatch")
	}
	snapshot, err := w.unchanged(ctx)
	if err != nil {
		return err
	}
	if snapshot != w.immutable {
		return errors.New("Create changed existing parent/current/paused/task facts")
	}
	return nil
}
func (w *sprintCreateWeb) verify(t *testing.T, ctx context.Context) {
	w.mu.Lock()
	failure := w.failure
	creates, newReads, parentReads, listReads, boards := w.creates, w.newReads, w.parentReads, w.listReads, w.emptyBoards
	in, result := w.createIntent, w.created.Clone()
	w.mu.Unlock()
	if failure != nil || creates != 1 || newReads < 1 || parentReads < 1 || listReads < 1 || boards < 7 || w.cut != 0 {
		t.Fatal("Sprint Create original browser facts incomplete", failure)
	}
	agentAppRequire(t, w.committedFacts(ctx, in, result))
	body := jsonBytes(t, map[string]any{"command": wc.SprintCreate, "request": in.request})
	reply := w.request(t, http.MethodPost, variableHTTPPath(w.project.Project.ID, "/structure-commands/lookup"), body, in.key)
	var found wc.CommandLookup
	if reply.status != 200 || json.Unmarshal(reply.body, &found) != nil || found.Validate() != nil || found.State != wc.LookupCommitted || found.Result == nil || !reflect.DeepEqual(*found.Result, result) {
		t.Fatal("original-key Sprint Create Lookup lost its original full result")
	}
	agentAppRequire(t, w.committedFacts(ctx, in, result))
}
