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

// Setup publishes an initialized Project and planned Sprint through existing
// providers. Only the public runtime-off App handles the tested Start/Lookup.
func TestSprintStartWeb(t *testing.T) {
	for _, mode := range []string{"paused-start-and-current-board", "lost-start-confirmation-only-lookup"} {
		t.Run(mode, func(t *testing.T) {
			entered, completed := false, false
			t.Cleanup(func() {
				if !entered || !completed {
					t.Error("Sprint Start original App handoff did not enter and complete")
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
			project, err := a.p2.creator.GetSchedulerConfig(ctxFor(t), original.ownerBrowser.actor, a.p2.project.ID)
			agentAppRequire(t, err)
			if sprint.State != wc.Planned || sprint.Version != 1 || project.Config.Enabled || project.Project.CurrentSprintID != nil || task.State != wc.TaskStateBacklog || task.AssigneeAgentID != nil {
				t.Fatal("Sprint Start requires real planned/backlog and paused empty-current facts")
			}
			process, err := a.guard.CurrentProcess()
			agentAppRequire(t, err)
			var username string
			agentAppRequire(t, original.raw.QueryRow(ctxFor(t), `SELECT username FROM agenteam_account.users WHERE id=$1`, original.ownerBrowser.actor.Details().UserID).Scan(&username))
			if original.afterOwners != nil {
				t.Fatal("original fixture already has an owner handoff")
			}
			original.afterOwners = func() {
				entered = true
				if !original.core.Joined() || !a.agents.Joined() || !a.p2.service.Joined() {
					t.Fatal("Sprint setup providers have not joined")
				}
				if _, err := a.guard.CurrentProcess(); err == nil {
					t.Fatal("Sprint setup guard remains held")
				}
				var stopped bool
				agentAppRequire(t, original.raw.QueryRow(ctxFor(t), `SELECT state='stopped' AND stopped_at IS NOT NULL FROM agenteam_object.process_claims WHERE process_id=$1`, process.String()).Scan(&stopped))
				if !stopped {
					t.Fatal("Sprint setup process has not retired")
				}
				w := newSprintStartWeb(t, a, mode, username, sprint, project, task)
				defer w.dispose(t)
				withAgentConfigurationApp(t, a, process.String(), w.origin, func(ctx context.Context, address string) {
					defer w.close(t)
					defer func() {
						w.mu.Lock()
						failure := w.failure
						w.mu.Unlock()
						if failure != nil {
							t.Error("original Sprint fixture failed", failure)
						}
					}()
					w.start(t, address)
					w.browser(t, ctx)
					w.close(t)
					w.verify(t, ctx)
				})
				completed = true
			}
		})
	}
}

type sprintWebIntent struct {
	kind                  string
	key                   f.IdempotencyKey
	bodyDigest, keyDigest [sha256.Size]byte
}
type sprintWebIntentKey struct{}
type sprintStartWeb struct {
	*agentConfigurationWeb
	before                                                 wc.Sprint
	project                                                pc.SchedulerProject
	task                                                   wc.Task
	command                                                sprintWebIntent
	result                                                 wc.SprintStartMutation
	starts, lookups, projectReads, sprintReads, boardReads int
	afterCommit                                            string
}

func newSprintStartWeb(t *testing.T, a *agentCreateFixture, mode, username string, sprint wc.Sprint, project pc.SchedulerProject, task wc.Task) *sprintStartWeb {
	t.Helper()
	const prefix = "AGENTEAM_SPRINT_START_WEB"
	dist, runtime := os.Getenv(prefix+"_DIST"), os.Getenv("AGENTEAM_AUTH_WEB_RUNTIME")
	evidence, inputHash := os.Getenv(prefix+"_EVIDENCE"), os.Getenv(prefix+"_INPUT_HASH")
	if !filepath.IsAbs(dist) || !filepath.IsAbs(runtime) || len(runtime) > 45 || !filepath.IsAbs(evidence) || len(inputHash) != 64 || strings.Trim(inputHash, "0123456789abcdef") != "" {
		t.Fatal("Sprint Start requires frozen assets and short owned runtime")
	}
	if info, err := os.Stat(filepath.Join(dist, "index.html")); err != nil || !info.Mode().IsRegular() {
		t.Fatal("frozen Sprint Start index missing")
	}
	directory, err := os.MkdirTemp(runtime, "ssw-")
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
	base := &agentConfigurationWeb{a: a, mode: mode, origin: "http://" + listener.Addr().String(), directory: directory, evidence: evidence, inputHash: inputHash, dist: dist, listener: listener, serve: make(chan error, 1), tails: map[string]*reviewWebTail{}}
	w := &sprintStartWeb{agentConfigurationWeb: base, before: sprint.Clone(), project: project.Clone(), task: task.Clone()}
	raw, err := json.Marshal(map[string]any{"mode": mode, "cookie": a.p2.base.ownerBrowser.cookie, "project_id": project.Project.ID, "sprint_id": sprint.ID, "version": sprint.Version, "project_version": project.Project.Version, "route": "/" + username + "/" + project.Project.NormalizedName + "/tasks/sprints/" + sprint.ID.String(), "title": sprint.Title, "task_id": task.ID, "task_title": task.Title})
	if err == nil {
		err = os.WriteFile(filepath.Join(directory, "sprint-start-material.json"), raw, 0600)
	}
	clear(raw)
	if err != nil {
		_ = listener.Close()
		t.Fatal(err)
	}
	published = true
	return w
}

func (w *sprintStartWeb) start(t *testing.T, address string) {
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

func (w *sprintStartWeb) completedResponse(r *http.Response) ([]byte, error) {
	raw, readErr := io.ReadAll(io.LimitReader(r.Body, (2<<20)+1))
	closeErr := r.Body.Close()
	validLength := r.ContentLength == int64(len(raw))
	if r.Request.Method == http.MethodHead {
		validLength = len(raw) == 0 && r.ContentLength >= 0
	}
	if readErr != nil || closeErr != nil || len(raw) > 2<<20 || !validLength || r.TLS == nil || len(r.TLS.VerifiedChains) == 0 {
		return nil, errors.New("original Sprint Start TLS EOF/Close/length incomplete")
	}
	rid := r.Header.Get("X-Request-ID")
	parsed, err := f.ParseID[f.Request](rid)
	if err != nil || parsed.String() != rid || len(r.Header.Values("X-Request-ID")) != 1 {
		return nil, errors.New("original Sprint Start response identity invalid")
	}
	w.mu.Lock()
	tail := w.tails[rid]
	w.mu.Unlock()
	if tail == nil {
		return nil, errors.New("original Sprint Start handler absent")
	}
	select {
	case <-tail.done:
	case <-r.Request.Context().Done():
		return nil, r.Request.Context().Err()
	}
	w.mu.Lock()
	returned := tail.returned
	w.mu.Unlock()
	if !returned {
		return nil, errors.New("original Sprint Start handler has not returned")
	}
	return raw, nil
}

func (w *sprintStartWeb) browser(t *testing.T, parent context.Context) {
	t.Helper()
	root, err := filepath.Abs("../account-captcha-web")
	agentAppRequire(t, err)
	ctx, cancel := context.WithTimeout(parent, 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "node", filepath.Join(root, "node_modules/@playwright/test/cli.js"), "test", "--config", filepath.Join(root, "sprint-start.config.js"))
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
	const prefix = "AGENTEAM_SPRINT_START_WEB"
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
	t.Logf("Sprint Start Node actual_wait pid=%d success=%t", cmd.Process.Pid, err == nil)
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

func (w *sprintStartWeb) sprintPath() string {
	return variableHTTPPath(w.project.Project.ID, "/sprints/"+w.before.ID.String())
}
func (w *sprintStartWeb) lookupPath() string {
	return variableHTTPPath(w.project.Project.ID, "/sprint-lifecycle-commands/lookup")
}
func (w *sprintStartWeb) observe(r *http.Request) error {
	if r.Method == http.MethodGet || r.Method == http.MethodHead {
		return nil
	}
	if r.Method != http.MethodPost || r.URL.RawQuery != "" {
		return errors.New("Sprint browser attempted a foreign mutation")
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, (16<<10)+1))
	if err != nil || len(raw) > 16<<10 {
		return errors.New("Sprint command exceeded its closed envelope")
	}
	r.Body = io.NopCloser(bytes.NewReader(raw))
	key := f.IdempotencyKey(r.Header.Get("Idempotency-Key"))
	if key.Validate() != nil || len(r.Header.Values("Idempotency-Key")) != 1 {
		return errors.New("Sprint command lost its original key")
	}
	var wire map[string]json.RawMessage
	if json.Unmarshal(raw, &wire) != nil {
		return errors.New("Sprint command was not a JSON object")
	}
	var expected f.Version
	var request map[string]json.RawMessage
	if json.Unmarshal(wire["expected_version"], &expected) != nil || expected != w.before.Version || json.Unmarshal(wire["request"], &request) != nil || request == nil || len(request) != 0 {
		return errors.New("Sprint command changed original version or empty request")
	}
	intent := sprintWebIntent{key: key, bodyDigest: sha256.Sum256(raw), keyDigest: sha256.Sum256([]byte(key))}
	w.mu.Lock()
	defer w.mu.Unlock()
	switch r.URL.Path {
	case w.sprintPath() + "/start":
		if len(wire) != 2 || w.starts != 0 || w.lookups != 0 {
			return errors.New("Sprint browser reissued Start")
		}
		intent.kind = "start"
		w.starts++
		w.command = intent
	case w.lookupPath():
		var command string
		var target wc.SprintID
		if len(wire) != 4 || json.Unmarshal(wire["command"], &command) != nil || command != wc.SprintStartCommandName || json.Unmarshal(wire["target_id"], &target) != nil || target != w.before.ID || w.mode != "lost-start-confirmation-only-lookup" || w.starts != 1 || w.lookups != 0 || w.cut != 1 || w.result.Validate() != nil || key != w.command.key {
			return errors.New("Sprint Lookup did not retain the original committed Start intent")
		}
		intent.kind = "lookup"
		w.lookups++
	default:
		return errors.New("Sprint browser called a foreign write endpoint")
	}
	w.keys = append(w.keys, string(key))
	*r = *r.WithContext(context.WithValue(r.Context(), sprintWebIntentKey{}, intent))
	return nil
}

func (w *sprintStartWeb) response(r *http.Response) error {
	raw, err := w.completedResponse(r)
	if err != nil {
		return err
	}
	intent, write := r.Request.Context().Value(sprintWebIntentKey{}).(sprintWebIntent)
	if write {
		if r.StatusCode != http.StatusOK {
			return fmt.Errorf("original Sprint command returned HTTP %d", r.StatusCode)
		}
		switch intent.kind {
		case "start":
			var receipt wc.SprintStartMutation
			if json.Unmarshal(raw, &receipt) != nil || receipt.Validate() != nil {
				return errors.New("original Sprint receipt invalid")
			}
			if err = w.committedFacts(r.Request.Context(), intent, receipt); err != nil {
				return err
			}
			snapshot, e := w.snapshot(r.Request.Context())
			if e != nil {
				return e
			}
			w.mu.Lock()
			w.result, w.afterCommit = receipt.Clone(), snapshot
			lost := w.mode == "lost-start-confirmation-only-lookup"
			w.mu.Unlock()
			if lost {
				return &reviewWebDrop{r.Header.Clone(), len(raw)}
			}
		case "lookup":
			var found struct {
				Status  wc.LookupState          `json:"status"`
				Receipt *wc.SprintStartMutation `json:"receipt"`
			}
			w.mu.Lock()
			original := w.result.Clone()
			snapshot := w.afterCommit
			w.mu.Unlock()
			if json.Unmarshal(raw, &found) != nil || found.Status != wc.LookupCommitted || found.Receipt == nil || !reflect.DeepEqual(*found.Receipt, original) {
				return errors.New("Sprint Lookup lost its complete original receipt")
			}
			after, e := w.snapshot(r.Request.Context())
			if e != nil {
				return e
			}
			if after != snapshot {
				return errors.New("read-only Sprint Lookup changed committed facts or original Activity")
			}
		default:
			return errors.New("unknown Sprint command proof")
		}
	} else if r.Request.Method == http.MethodGet && r.StatusCode == http.StatusOK {
		if err = w.observeRead(r.Request, raw); err != nil {
			return err
		}
	}
	if r.StatusCode == http.StatusOK && (write || r.Request.Method == http.MethodGet) {
		if err = w.proof(r, raw, intent, write); err != nil {
			return err
		}
	}
	r.Body = io.NopCloser(bytes.NewReader(raw))
	return nil
}

func (w *sprintStartWeb) observeRead(r *http.Request, raw []byte) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.result.Validate() != nil {
		return nil
	}
	switch r.URL.Path {
	case variableHTTPPath(w.project.Project.ID, ""):
		var project pc.ProjectRef
		if json.Unmarshal(raw, &project) != nil || !reflect.DeepEqual(project, w.result.Project) {
			return errors.New("current Project GET differs from the committed Start")
		}
		w.projectReads++
	case w.sprintPath():
		var sprint wc.Sprint
		if json.Unmarshal(raw, &sprint) != nil || !reflect.DeepEqual(sprint, w.result.Sprint) {
			return errors.New("current Sprint GET differs from the committed Start")
		}
		w.sprintReads++
	case variableHTTPPath(w.project.Project.ID, "/tasks"):
		if r.URL.Query().Get("sprint_id") != w.before.ID.String() {
			return errors.New("current board read the wrong Sprint")
		}
		var page f.Page[wc.Task]
		if json.Unmarshal(raw, &page) != nil || len(page.Items) > 1 {
			return errors.New("current board page invalid")
		}
		if r.URL.Query().Get("state") == string(wc.TaskStateBacklog) {
			if len(page.Items) != 1 || !reflect.DeepEqual(page.Items[0], w.task) {
				return errors.New("Start changed or lost the existing backlog Task")
			}
			w.boardReads++
		} else if len(page.Items) != 0 {
			return errors.New("Start moved a Task into another state")
		}
	}
	return nil
}

func (w *sprintStartWeb) proof(r *http.Response, raw []byte, intent sprintWebIntent, write bool) error {
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
	return os.WriteFile(filepath.Join(w.evidence, "sprint-start-"+w.mode+"-response-"+r.Header.Get("X-Request-ID")+".json"), encoded, 0600)
}

func (w *sprintStartWeb) committedFacts(ctx context.Context, intent sprintWebIntent, result wc.SprintStartMutation) error {
	actor := w.a.p2.base.ownerBrowser.actor
	if result.Validate() != nil || result.Sprint.UpdatedAt.Time().Before(w.before.UpdatedAt.Time()) || result.Sprint.UpdatedAt.Time().Before(w.project.Project.UpdatedAt.Time()) {
		return errors.New("Start receipt time or scope invalid")
	}
	expectedSprint := w.before.Clone()
	expectedSprint.State = wc.Current
	expectedSprint.Version++
	expectedSprint.UpdatedAt = result.Sprint.UpdatedAt
	expectedSprint.StartedAt = &expectedSprint.UpdatedAt
	expectedSprint.StartedBy = &wc.ActorHistory{Kind: actor.Details().Kind, UserID: actor.Details().UserID}
	expectedProject := w.project.Project.Clone()
	expectedProject.Version++
	expectedProject.UpdatedAt = result.Sprint.UpdatedAt
	expectedProject.CurrentSprintID = &expectedSprint.ID
	if !reflect.DeepEqual(result.Sprint, expectedSprint) || !reflect.DeepEqual(result.Project, expectedProject) {
		return errors.New("Start changed fields beyond its original Sprint/Project postimage")
	}
	var commandID, eventID, user, digest string
	var raw, planRaw []byte
	var version int64
	err := w.a.p2.base.raw.QueryRow(ctx, `SELECT id::text,event_id::text,actor_user_id::text,semantic_digest,expected_version,receipt,plan FROM agenteam_work.sprint_start_commands WHERE project_id=$1::text::uuid AND sprint_id=$2::text::uuid AND idempotency_key=$3 AND state='completed'`, w.project.Project.ID.String(), w.before.ID.String(), string(intent.key)).Scan(&commandID, &eventID, &user, &digest, &version, &raw, &planRaw)
	if err != nil {
		return err
	}
	var stored wc.SprintStartMutation
	var plan struct {
		Before  wc.Sprint              `json:"before"`
		Project pc.ProjectRef          `json:"project"`
		After   wc.SprintStartMutation `json:"after"`
		Header  eventc.Header          `json:"header"`
		Payload wc.SprintStarted       `json:"payload"`
	}
	rid, err := f.ParseID[f.Request](w.before.ID.String())
	if err != nil {
		return err
	}
	semantic, err := wc.StartSprintDigest(actor, f.CommandMeta{RequestID: rid, IdempotencyKey: intent.key, ExpectedVersion: &w.before.Version}, w.project.Project.ID, w.before.ID)
	if err != nil {
		return err
	}
	if json.Unmarshal(raw, &stored) != nil || !reflect.DeepEqual(stored, result) || json.Unmarshal(planRaw, &plan) != nil || !reflect.DeepEqual(plan.Before, w.before) || !reflect.DeepEqual(plan.Project, w.project.Project) || !reflect.DeepEqual(plan.After, result) || plan.Header.Validate() != nil || plan.Payload.Validate() != nil || plan.Payload.CommandID.String() != commandID || plan.Payload.ActorUserID.String() != user || plan.Payload.MilestoneID != w.before.MilestoneID || plan.Payload.ProjectVersion != result.Project.Version || eventID != result.EventID.String() || plan.Header.EventID != result.EventID || version != int64(w.before.Version) || user != actor.Details().UserID || digest != semantic.String() {
		return errors.New("original Start command/plan/receipt identity differs")
	}
	var facts [6]int64
	err = w.a.p2.base.raw.QueryRow(ctx, `SELECT
 (SELECT count(*) FROM agenteam_work.sprint_start_commands WHERE project_id=$1::text::uuid),
 (SELECT count(*) FROM agenteam_outbox.events WHERE project_id=$1::text::uuid AND aggregate_id=$2::text::uuid AND id=$3::text::uuid AND aggregate_version=$4 AND producer='work' AND event_type='work.sprint_started' AND schema_version=1 AND convert_from(payload,'UTF8')::jsonb->>'command_id'=$5::text AND convert_from(payload,'UTF8')::jsonb->>'actor_user_id'=$6::text AND convert_from(payload,'UTF8')::jsonb->>'milestone_id'=$7::text AND convert_from(payload,'UTF8')::jsonb->>'project_version'=$8::text),
 (SELECT count(*) FROM agenteam_scheduler.dispatches WHERE project_id=$1::text),
 (SELECT count(*) FROM agenteam_execution.executions WHERE project_id=$1::text),
 (SELECT count(*) FROM agenteam_project.projects WHERE id=$1::text::uuid AND version=$9 AND current_sprint_id=$2::text::uuid AND NOT scheduler_enabled AND lifecycle='active' AND updated_at=$10),
 (SELECT count(*) FROM agenteam_work.sprints WHERE project_id=$1::text::uuid AND id=$2::text::uuid AND version=$4 AND started_at=$10 AND completed_at IS NULL AND started_by->>'user_id'=$6::text AND updated_at=$10)`, w.project.Project.ID.String(), w.before.ID.String(), result.EventID.String(), int64(result.Sprint.Version), commandID, user, w.before.MilestoneID.String(), result.Project.Version.String(), int64(result.Project.Version), result.Sprint.UpdatedAt.Time()).Scan(&facts[0], &facts[1], &facts[2], &facts[3], &facts[4], &facts[5])
	if err != nil {
		return err
	}
	if facts != ([6]int64{1, 1, 0, 0, 1, 1}) {
		return errors.New("Start omitted its atomic current/event facts or enabled dispatch")
	}
	var configRaw []byte
	err = w.a.p2.base.raw.QueryRow(ctx, `SELECT jsonb_build_object('scheduler_enabled',scheduler_enabled,'scheduler_max_concurrency',scheduler_max_concurrency) FROM agenteam_project.projects WHERE id=$1::text::uuid`, w.project.Project.ID.String()).Scan(&configRaw)
	if err != nil {
		return err
	}
	var config pc.ProjectSchedulerConfig
	if json.Unmarshal(configRaw, &config) != nil || !reflect.DeepEqual(config, w.project.Config) {
		return errors.New("Start changed the paused Scheduler configuration")
	}
	return nil
}

func (w *sprintStartWeb) snapshot(ctx context.Context) (string, error) {
	var value string
	err := w.a.p2.base.raw.QueryRow(ctx, `SELECT jsonb_build_object(
 'project',(SELECT to_jsonb(p) FROM agenteam_project.projects p WHERE id=$1::text::uuid),
 'sprint',(SELECT to_jsonb(s) FROM agenteam_work.sprints s WHERE project_id=$1::text::uuid AND id=$2::text::uuid),
 'commands',(SELECT coalesce(jsonb_agg(to_jsonb(c) ORDER BY id),'[]'::jsonb) FROM agenteam_work.sprint_start_commands c WHERE project_id=$1::text::uuid),
 'outbox',(SELECT coalesce(jsonb_agg(to_jsonb(e) ORDER BY id),'[]'::jsonb) FROM agenteam_outbox.events e WHERE project_id=$1::text::uuid AND aggregate_id=$2::text::uuid),
 'tasks',(SELECT coalesce(jsonb_agg(to_jsonb(t) ORDER BY id),'[]'::jsonb) FROM agenteam_work.tasks t WHERE project_id=$1::text::uuid),
 'dispatches',(SELECT count(*) FROM agenteam_scheduler.dispatches WHERE project_id=$1::text),
 'executions',(SELECT count(*) FROM agenteam_execution.executions WHERE project_id=$1::text),
 'activity',(SELECT last_activity_at FROM agenteam_account.sessions WHERE id=$3::uuid))::text`, w.project.Project.ID.String(), w.before.ID.String(), w.a.p2.base.ownerBrowser.actor.Details().SessionID).Scan(&value)
	return value, err
}
func (w *sprintStartWeb) verify(t *testing.T, ctx context.Context) {
	t.Helper()
	w.mu.Lock()
	failure := w.failure
	starts, lookups, cut, projectReads, sprintReads, boardReads := w.starts, w.lookups, w.cut, w.projectReads, w.sprintReads, w.boardReads
	result, intent, snapshot := w.result.Clone(), w.command, w.afterCommit
	tails := make([]*reviewWebTail, 0, len(w.tails))
	for _, tail := range w.tails {
		tails = append(tails, tail)
	}
	w.mu.Unlock()
	if failure != nil {
		t.Fatal("original Sprint observer failed", failure)
	}
	wantLookup := 0
	if w.mode == "lost-start-confirmation-only-lookup" {
		wantLookup = 1
	}
	if starts != 1 || lookups != wantLookup || cut != wantLookup || projectReads < 1 || sprintReads < 1 || boardReads < 1 || result.Validate() != nil {
		t.Fatal("Sprint browser did not complete exactly Start/recovery and real current board reads")
	}
	for _, tail := range tails {
		select {
		case <-tail.done:
		default:
			t.Fatal("original Sprint handler not joined")
		}
		if !tail.returned {
			t.Fatal("original Sprint handler did not return")
		}
	}
	agentAppRequire(t, w.committedFacts(ctx, intent, result))
	after, err := w.snapshot(ctx)
	agentAppRequire(t, err)
	if snapshot == "" || after != snapshot {
		t.Fatal("Sprint reads/recovery changed the original committed facts")
	}
}
