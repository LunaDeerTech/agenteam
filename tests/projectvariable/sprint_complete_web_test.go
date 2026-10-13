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
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	wc "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
)

func TestSprintCompleteWeb(t *testing.T) {
	t.Run("complete-explicit-next-and-original-lookup", func(t *testing.T) {
		v := assembleTaskHumanHTTPFixture(t, true)
		a, base := v.domain.agent, v.domain.base
		parent := sprintCompleteGet(t, v, v.domain.task.SprintID)
		first := sprintCompleteCreate(t, v, parent, "Finish this empty Sprint")
		next := sprintCompleteCreate(t, v, parent, "Explicit next empty Sprint")
		first = sprintCompleteStart(t, v, first, "browser-original-start")
		project, err := base.projects.GetSchedulerConfig(ctxFor(t), base.ownerBrowser.actor, base.project.ID)
		agentAppRequire(t, err)
		process, err := a.guard.CurrentProcess()
		agentAppRequire(t, err)
		var username string
		agentAppRequire(t, base.raw.QueryRow(ctxFor(t), `SELECT username FROM agenteam_account.users WHERE id=$1`, base.ownerBrowser.actor.Details().UserID).Scan(&username))
		entered, completed := false, false
		t.Cleanup(func() {
			if !entered || !completed {
				t.Error("original Sprint completion App handoff incomplete")
			}
		})
		if a.p2.base.afterOwners != nil {
			t.Fatal("original fixture handoff occupied")
		}
		a.p2.base.afterOwners = func() {
			entered = true
			if !a.p2.base.core.Joined() || !a.agents.Joined() || !a.p2.service.Joined() {
				t.Fatal("original setup owners have not joined")
			}
			if _, err := a.guard.CurrentProcess(); err == nil {
				t.Fatal("setup ProcessGuard still held")
			}
			var stopped bool
			agentAppRequire(t, base.raw.QueryRow(ctxFor(t), `SELECT state='stopped' AND stopped_at IS NOT NULL FROM agenteam_object.process_claims WHERE process_id=$1`, process.String()).Scan(&stopped))
			if !stopped {
				t.Fatal("original setup process not retired")
			}
			w := newSprintCompleteWeb(t, a, base, first, next, project, username)
			defer w.dispose(t)
			withAgentConfigurationApp(t, a, process.String(), w.origin, func(ctx context.Context, address string) {
				defer w.close(t)
				w.startObserved(t, address, w.observe, w.response)
				w.browser(t, ctx)
				w.close(t)
				w.verify(t, ctx)
			})
			completed = true
		}
	})
}

type sprintCompleteWeb struct {
	*sprintStartWeb
	base                                *variableHTTPFixture
	first, next                         wc.Sprint
	current                             wc.Sprint
	currentProject                      pc.SchedulerProject
	completedFirst, completedNext       wc.SprintCompleteMutation
	startedNext                         wc.SprintStartMutation
	completeIntent                      sprintWebIntent
	writes                              []string
	projectReads, firstReads, nextReads int
	lastSnapshot, tasks                 string
}

func newSprintCompleteWeb(t *testing.T, a *agentCreateFixture, base *variableHTTPFixture, first, next wc.Sprint, project pc.SchedulerProject, username string) *sprintCompleteWeb {
	t.Helper()
	const prefix = "AGENTEAM_SPRINT_COMPLETE_WEB"
	dist, runtime, evidence := os.Getenv(prefix+"_DIST"), os.Getenv("AGENTEAM_AUTH_WEB_RUNTIME"), os.Getenv(prefix+"_EVIDENCE")
	if !filepath.IsAbs(dist) || !filepath.IsAbs(runtime) || len(runtime) > 45 || !filepath.IsAbs(evidence) {
		t.Fatal("owned Sprint completion inputs required")
	}
	if info, err := os.Stat(filepath.Join(dist, "index.html")); err != nil || !info.Mode().IsRegular() {
		t.Fatal("production Sprint assets absent")
	}
	directory, err := os.MkdirTemp(runtime, "scw-")
	agentAppRequire(t, err)
	published := false
	defer func() {
		if !published {
			_ = os.RemoveAll(directory)
		}
	}()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	agentAppRequire(t, err)
	owner := &agentConfigurationWeb{a: a, mode: "complete-explicit-next-and-original-lookup", origin: "http://" + listener.Addr().String(), directory: directory, evidence: evidence, dist: dist, listener: listener, serve: make(chan error, 1), tails: map[string]*reviewWebTail{}}
	w := &sprintCompleteWeb{sprintStartWeb: &sprintStartWeb{agentConfigurationWeb: owner}, base: base, first: first.Clone(), next: next.Clone(), current: first.Clone(), currentProject: project.Clone(), tasks: sprintCompleteTaskRows(t, base)}
	route := "/" + username + "/" + project.Project.NormalizedName + "/tasks/sprints/"
	raw, err := json.Marshal(map[string]any{"mode": owner.mode, "origin": owner.origin, "cookie": base.ownerBrowser.cookie, "project_id": base.project.ID, "first": first, "next": next, "project": project.Project, "route": route + first.ID.String(), "next_route": route + next.ID.String()})
	if err == nil {
		err = os.WriteFile(filepath.Join(directory, "sprint-complete-material.json"), raw, 0600)
	}
	clear(raw)
	if err != nil {
		_ = listener.Close()
		t.Fatal(err)
	}
	published = true
	return w
}
func (w *sprintCompleteWeb) observe(r *http.Request) error {
	if r.Method == http.MethodGet || r.Method == http.MethodHead {
		return nil
	}
	if r.Method != http.MethodPost || r.URL.RawQuery != "" {
		return errors.New("foreign Sprint completion mutation")
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, (16<<10)+1))
	closed := r.Body.Close()
	if err != nil || closed != nil || len(raw) > 16<<10 {
		return errors.New("incomplete original Sprint command")
	}
	r.Body = io.NopCloser(bytes.NewReader(raw))
	key := f.IdempotencyKey(r.Header.Get("Idempotency-Key"))
	if key.Validate() != nil || len(r.Header.Values("Idempotency-Key")) != 1 {
		return errors.New("missing original Sprint command key")
	}
	var envelope struct {
		Expected f.Version                  `json:"expected_version"`
		Request  map[string]json.RawMessage `json:"request"`
		Command  string                     `json:"command,omitempty"`
		Target   wc.SprintID                `json:"target_id,omitempty"`
	}
	if agentWebDecode(raw, &envelope) != nil || envelope.Request == nil || len(envelope.Request) != 0 {
		return errors.New("changed original Sprint command envelope")
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	base := variableHTTPPath(w.base.project.ID, "")
	intent := sprintWebIntent{key: key, bodyDigest: sha256.Sum256(raw), keyDigest: sha256.Sum256([]byte(key))}
	switch r.URL.Path {
	case base + "/sprints/" + w.first.ID.String() + "/complete":
		if len(w.writes) != 0 || envelope.Expected != w.first.Version || envelope.Command != "" || envelope.Target != (wc.SprintID{}) {
			return errors.New("first completion original intent changed")
		}
		intent.kind = "complete-first"
	case base + "/sprints/" + w.next.ID.String() + "/start":
		if len(w.writes) != 1 || w.completedFirst.Validate() != nil || w.projectReads < 1 || w.firstReads < 1 || w.nextReads < 1 || envelope.Expected != w.next.Version || envelope.Command != "" || envelope.Target != (wc.SprintID{}) {
			return errors.New("next Sprint started before original completion and independent rereads")
		}
		intent.kind = "start-next"
	case base + "/sprints/" + w.next.ID.String() + "/complete":
		if len(w.writes) != 2 || w.startedNext.Validate() != nil || envelope.Expected != w.startedNext.Sprint.Version || envelope.Command != "" || envelope.Target != (wc.SprintID{}) {
			return errors.New("second completion original intent changed")
		}
		intent.kind = "complete-next"
		w.completeIntent = intent
	case base + "/sprint-lifecycle-commands/lookup":
		if len(w.writes) != 3 || w.cut != 1 || w.completedNext.Validate() != nil || envelope.Expected != w.startedNext.Sprint.Version || envelope.Command != wc.SprintCompleteCommandName || envelope.Target != w.next.ID || key != w.completeIntent.key {
			return errors.New("Lookup did not retain the original lost completion")
		}
		intent.kind = "lookup"
	default:
		return errors.New("unobserved Sprint write endpoint")
	}
	w.writes = append(w.writes, intent.kind)
	w.keys = append(w.keys, string(key))
	*r = *r.WithContext(context.WithValue(r.Context(), sprintWebIntentKey{}, intent))
	return nil
}
func (w *sprintCompleteWeb) response(r *http.Response) error {
	raw, err := w.completedResponse(r)
	if err != nil {
		return err
	}
	intent, write := r.Request.Context().Value(sprintWebIntentKey{}).(sprintWebIntent)
	w.mu.Lock()
	defer w.mu.Unlock()
	if r.StatusCode != 200 {
		return errors.New("original Sprint browser HTTP rejected")
	}
	cut := false
	if write {
		switch intent.kind {
		case "complete-first", "complete-next":
			var receipt wc.SprintCompleteMutation
			if json.Unmarshal(raw, &receipt) != nil {
				return errors.New("invalid complete receipt")
			}
			if err = sprintCompleteCommitted(r.Request.Context(), w.base, w.current, w.currentProject, intent.key, receipt); err != nil {
				return err
			}
			w.current, w.currentProject.Project = receipt.Sprint.Clone(), receipt.Project.Clone()
			w.projectReads, w.firstReads, w.nextReads = 0, 0, 0
			if intent.kind == "complete-first" {
				w.completedFirst = receipt.Clone()
			} else {
				w.completedNext = receipt.Clone()
				cut = true
			}
		case "start-next":
			var receipt wc.SprintStartMutation
			if json.Unmarshal(raw, &receipt) != nil || receipt.Validate() != nil {
				return errors.New("invalid explicit next Start receipt")
			}
			s := w.next.Clone()
			s.State = wc.Current
			s.Version++
			s.UpdatedAt = receipt.Sprint.UpdatedAt
			s.StartedAt = &s.UpdatedAt
			s.StartedBy = &wc.ActorHistory{Kind: w.base.ownerBrowser.actor.Details().Kind, UserID: w.base.ownerBrowser.actor.Details().UserID}
			p := w.currentProject.Project.Clone()
			p.Version++
			p.UpdatedAt = s.UpdatedAt
			p.CurrentSprintID = &s.ID
			if !reflect.DeepEqual(s, receipt.Sprint) || !reflect.DeepEqual(p, receipt.Project) {
				return errors.New("explicit Start changed unrelated fields")
			}
			var same bool
			err = w.base.raw.QueryRow(r.Request.Context(), `SELECT c.receipt=$3::jsonb AND c.state='completed' AND c.xmin=p.xmin AND c.xmin=s.xmin AND c.xmin=e.xmin FROM agenteam_work.sprint_start_commands c JOIN agenteam_project.projects p ON p.id=c.project_id JOIN agenteam_work.sprints s ON s.id=c.sprint_id JOIN agenteam_outbox.events e ON e.id=c.event_id WHERE c.project_id=$1 AND c.idempotency_key=$2`, w.base.project.ID.String(), string(intent.key), raw).Scan(&same)
			if err != nil {
				return err
			}
			if !same {
				return errors.New("explicit Start lost original same-Tx receipt")
			}
			w.startedNext = receipt.Clone()
			w.current, w.currentProject.Project = receipt.Sprint.Clone(), receipt.Project.Clone()
		case "lookup":
			var found struct {
				Status  wc.LookupState             `json:"status"`
				Receipt *wc.SprintCompleteMutation `json:"receipt"`
			}
			if json.Unmarshal(raw, &found) != nil || found.Status != wc.LookupCommitted || found.Receipt == nil || !reflect.DeepEqual(*found.Receipt, w.completedNext) {
				return errors.New("Lookup lost original completed receipt")
			}
			fresh, e := sprintCompleteSnapshotContext(r.Request.Context(), w.base)
			if e != nil {
				return e
			}
			if fresh != w.lastSnapshot {
				return errors.New("Lookup changed original committed facts")
			}
		}
		w.lastSnapshot, err = sprintCompleteSnapshotContext(r.Request.Context(), w.base)
		if err != nil {
			return err
		}
	} else {
		switch r.Request.URL.Path {
		case variableHTTPPath(w.base.project.ID, ""):
			var p pc.ProjectRef
			if json.Unmarshal(raw, &p) != nil || !reflect.DeepEqual(p, w.currentProject.Project) {
				return errors.New("Project reread differs from current original postimage")
			}
			if w.completedFirst.Validate() == nil {
				w.projectReads++
			}
		case variableHTTPPath(w.base.project.ID, "/sprints/"+w.first.ID.String()):
			expected := w.first
			if w.completedFirst.Validate() == nil {
				expected = w.completedFirst.Sprint
			}
			var s wc.Sprint
			if json.Unmarshal(raw, &s) != nil || !reflect.DeepEqual(s, expected) {
				return errors.New("first Sprint reread differs")
			}
			if w.completedFirst.Validate() == nil {
				w.firstReads++
			}
		case variableHTTPPath(w.base.project.ID, "/sprints/"+w.next.ID.String()):
			expected := w.next
			if w.startedNext.Validate() == nil {
				expected = w.startedNext.Sprint
			}
			if w.completedNext.Validate() == nil {
				expected = w.completedNext.Sprint
			}
			var s wc.Sprint
			if json.Unmarshal(raw, &s) != nil || !reflect.DeepEqual(s, expected) {
				return errors.New("next Sprint reread differs")
			}
			w.nextReads++
		}
	}
	record := map[string]any{"request_id": r.Header.Get("X-Request-ID"), "method": r.Request.Method, "url_sha256": fmt.Sprintf("%x", sha256.Sum256([]byte(r.Request.URL.RequestURI()))), "status": r.StatusCode, "content_length": len(raw), "body_sha256": fmt.Sprintf("%x", sha256.Sum256(raw)), "original_eof": true, "original_closed": true, "handler_returned": true, "receipt_cut": cut}
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
	if err = os.WriteFile(filepath.Join(w.evidence, "sprint-complete-response-"+r.Header.Get("X-Request-ID")+".json"), encoded, 0600); err != nil {
		return err
	}
	if cut {
		return &reviewWebDrop{r.Header.Clone(), len(raw)}
	}
	r.Body = io.NopCloser(bytes.NewReader(raw))
	return nil
}
func (w *sprintCompleteWeb) browser(t *testing.T, parent context.Context) {
	t.Helper()
	root, err := filepath.Abs("../account-captcha-web")
	agentAppRequire(t, err)
	ctx, cancel := context.WithTimeout(parent, 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "node", filepath.Join(root, "node_modules/@playwright/test/cli.js"), "test", "--config", filepath.Join(root, "sprint-complete.config.js"))
	cmd.Dir = root
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.WaitDelay = time.Second
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if key != "TMPDIR" && key != "DEBUG" && key != "PWDEBUG" && key != "PLAYWRIGHT_NO_COPY_PROMPT" && !strings.HasPrefix(key, "AGENTEAM_") && !strings.HasPrefix(key, "DEV_CHAT_") {
			cmd.Env = append(cmd.Env, entry)
		}
	}
	cmd.Env = append(cmd.Env, "TMPDIR="+w.directory, "PLAYWRIGHT_NO_COPY_PROMPT=1", "AGENTEAM_AUTH_WEB_ORIGIN="+w.origin, "AGENTEAM_AUTH_WEB_PRIVATE="+w.directory, "AGENTEAM_AUTH_WEB_CHROMIUM=/usr/bin/chromium", "AGENTEAM_SPRINT_COMPLETE_WEB_DIST="+w.dist, "AGENTEAM_SPRINT_COMPLETE_WEB_EVIDENCE="+w.evidence)
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	if err = cmd.Start(); err != nil {
		t.Fatal("owned Sprint browser did not start")
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
	t.Logf("Sprint Complete Node actual_wait pid=%d success=%t", cmd.Process.Pid, err == nil)
	safe := output.String()
	w.mu.Lock()
	tokens := append([]string(nil), w.keys...)
	w.mu.Unlock()
	tokens = append(tokens, w.base.ownerBrowser.cookie, w.base.ownerBrowser.csrf)
	for _, token := range tokens {
		if token != "" {
			safe = strings.ReplaceAll(safe, token, "[redacted]")
		}
	}
	t.Log(safe)
	if err != nil {
		t.Fatal("original Sprint completion browser failed")
	}
}
func (w *sprintCompleteWeb) verify(t *testing.T, ctx context.Context) {
	t.Helper()
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.failure != nil || !reflect.DeepEqual(w.writes, []string{"complete-first", "start-next", "complete-next", "lookup"}) || w.cut != 1 || w.completedNext.Validate() != nil || w.projectReads < 1 || w.nextReads < 1 {
		t.Fatal("browser did not complete the original four commands and rereads", w.failure)
	}
	for _, tail := range w.tails {
		select {
		case <-tail.done:
		default:
			t.Fatal("original Sprint handler has not joined")
		}
		if !tail.returned {
			t.Fatal("original Sprint handler did not return")
		}
	}
	actual, err := sprintCompleteSnapshotContext(ctx, w.base)
	agentAppRequire(t, err)
	if actual != w.lastSnapshot || sprintCompleteTaskRows(t, w.base) != w.tasks {
		t.Fatal("browser completion/recovery changed original member or committed facts")
	}
	raw, err := os.ReadFile(filepath.Join(w.directory, "sprint-complete-result.json"))
	agentAppRequire(t, err)
	var result struct {
		Completed bool `json:"completed"`
		Writes    int  `json:"writes"`
	}
	if json.Unmarshal(raw, &result) != nil || !result.Completed || result.Writes != 4 {
		t.Fatal("browser consumer did not actually retire")
	}
	t.Log("Sprint Complete defaultApp runtime disabled; complete2/start1/originalLookup1; original members unchanged; public App/HTTP/Node actual tails joined")
}
