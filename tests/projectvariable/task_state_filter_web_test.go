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
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"syscall"
	"testing"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	wc "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
)

// All Task seed writes use original Owner services/HTTP. After their owners
// retire, this read-only scope uses the production SPA and genuine defaultApp.
// Fifty-one backlog Tasks are the minimum required to exercise a real cursor.
const taskStateFilterTerm = "StateNeedle_%"

func TestTaskStateFilterWeb(t *testing.T) {
	t.Run("default-app-state-pagination", func(t *testing.T) {
		entered, completed := false, false
		t.Cleanup(func() {
			if !entered || !completed {
				t.Error("original state filter App handoff incomplete")
			}
		})
		v := assembleTaskHumanHTTPFixture(t, true)
		a, base := v.domain.agent, v.domain.base
		title, priority, typ := taskStateFilterTerm+" assigned target", wc.TaskPriorityHigh, wc.TaskTypeBug
		update, err := v.domain.tasks.UpdateTask(ctxFor(t), base.ownerBrowser.actor, meta(t, "state-filter-target-fields", &v.domain.task.Version), base.project.ID, v.domain.task.ID, wc.TaskFieldsUpdate{Title: &title, Priority: &priority, Type: &typ})
		if err != nil {
			t.Fatalf("formal state-filter target update failed (%s)", blockerManagementSafeFailure(err))
		}
		if update.Validate() != nil || !update.Changed {
			t.Fatal("formal target update failed")
		}
		v.domain.task = update.Task.Clone()
		body, _, key := v.intent(t)
		reply := v.request(t, base.ownerBrowser, "POST", v.transferPath(), body, key)
		receipt, err := wc.DecodeTaskTransitionMutation(reply.body)
		if reply.status != 200 || err != nil || receipt.Validate() != nil || receipt.Task.ID != v.domain.task.ID || receipt.Task.ProjectID != base.project.ID || receipt.Task.Version != v.domain.task.Version+1 || receipt.Task.State != wc.TaskStateTodo || receipt.Task.AssigneeAgentID == nil || *receipt.Task.AssigneeAgentID != v.domain.agentID {
			t.Fatal("formal updated target Ready receipt invalid")
		}
		expected := v.domain.task.Clone()
		expected.Version++
		expected.State, expected.AssigneeAgentID = wc.TaskStateTodo, &v.domain.agentID
		// Ready may establish a new rank in the destination group.
		expected.ManualRank, expected.UpdatedAt = receipt.Task.ManualRank, receipt.Task.UpdatedAt
		if !reflect.DeepEqual(receipt.Task, expected) {
			t.Fatal("formal Ready changed Task parent or retained business fields")
		}
		assigned := receipt.Task.Clone()
		rows := []wc.Task{assigned}
		for n := 0; n < 51; n++ {
			result, e := v.domain.tasks.CreateTask(ctxFor(t), base.ownerBrowser.actor, meta(t, fmt.Sprintf("state-filter-backlog-%02d", n), nil), base.project.ID, wc.TaskCreate{TaskID: id[wc.Task](t), SprintID: assigned.SprintID, Title: fmt.Sprintf("%s backlog %02d", taskStateFilterTerm, n), Type: typ, Priority: priority})
			if e != nil {
				t.Fatalf("formal backlog seed index=%d failed (%s)", n, blockerManagementSafeFailure(e))
			}
			if result.Validate() != nil || !result.Changed || result.Task.State != wc.TaskStateBacklog || result.Task.AssigneeAgentID != nil {
				t.Fatal("formal backlog seed failed")
			}
			rows = append(rows, result.Task.Clone())
		}
		sprintCompleteStart(t, v, sprintCompleteGet(t, v, assigned.SprintID), "state-filter-current")
		process, err := a.guard.CurrentProcess()
		agentAppRequire(t, err)
		var username string
		agentAppRequire(t, base.raw.QueryRow(ctxFor(t), `SELECT username FROM agenteam_account.users WHERE id=$1`, base.ownerBrowser.actor.Details().UserID).Scan(&username))
		if a.p2.base.afterOwners != nil {
			t.Fatal("original fixture handoff occupied")
		}
		a.p2.base.afterOwners = func() {
			entered = true
			if !a.p2.base.core.Joined() || !a.agents.Joined() || !a.p2.service.Joined() {
				t.Fatal("original setup owners not joined")
			}
			if _, err := a.guard.CurrentProcess(); err == nil {
				t.Fatal("original setup ProcessGuard held")
			}
			var stopped bool
			agentAppRequire(t, base.raw.QueryRow(ctxFor(t), `SELECT state='stopped' AND stopped_at IS NOT NULL FROM agenteam_object.process_claims WHERE process_id=$1`, process.String()).Scan(&stopped))
			if !stopped {
				t.Fatal("original setup process not retired")
			}
			w := newTaskStateFilterWeb(t, a, base, assigned, rows, username)
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

type taskStateFilterWeb struct {
	*sprintStartWeb
	base                       *variableHTTPFixture
	initial                    wc.Task
	rows                       []wc.Task
	cursors                    map[string]assigneeCursor
	completed                  map[string]map[wc.TaskState]int
	continuations, detailReads int
	before                     string
	responseFailure            map[string]any
	browserStarted             time.Time
	browserReturned            bool
}

func newTaskStateFilterWeb(t *testing.T, a *agentCreateFixture, base *variableHTTPFixture, task wc.Task, rows []wc.Task, username string) *taskStateFilterWeb {
	t.Helper()
	const prefix = "AGENTEAM_TASK_STATE_FILTER_WEB"
	dist, runtime, evidence := os.Getenv(prefix+"_DIST"), os.Getenv("AGENTEAM_AUTH_WEB_RUNTIME"), os.Getenv(prefix+"_EVIDENCE")
	if !filepath.IsAbs(dist) || !filepath.IsAbs(runtime) || len(runtime) > 45 || !filepath.IsAbs(evidence) {
		t.Fatal("owned Task State Filter paths required")
	}
	if info, err := os.Stat(filepath.Join(dist, "index.html")); err != nil || !info.Mode().IsRegular() {
		t.Fatal("Task State Filter production assets missing")
	}
	dir, err := os.MkdirTemp(runtime, "tsf-")
	agentAppRequire(t, err)
	published := false
	defer func() {
		if !published {
			_ = os.RemoveAll(dir)
		}
	}()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	agentAppRequire(t, err)
	defer func() {
		if !published {
			_ = listener.Close()
		}
	}()
	owner := &agentConfigurationWeb{a: a, mode: "default-app-state-pagination", origin: "http://" + listener.Addr().String(), directory: dir, evidence: evidence, dist: dist, listener: listener, serve: make(chan error, 1), tails: map[string]*reviewWebTail{}}
	w := &taskStateFilterWeb{sprintStartWeb: &sprintStartWeb{agentConfigurationWeb: owner}, base: base, initial: task.Clone(), rows: rows, cursors: map[string]assigneeCursor{}, completed: map[string]map[wc.TaskState]int{}}
	w.before, err = w.facts(ctxFor(t))
	agentAppRequire(t, err)
	route := "/" + username + "/" + base.project.NormalizedName + "/tasks"
	raw, err := json.Marshal(map[string]any{"mode": owner.mode, "origin": owner.origin, "cookie": base.ownerBrowser.cookie, "project_id": task.ProjectID, "task": task, "rows": rows, "term": taskStateFilterTerm, "route": route})
	if err == nil {
		err = os.WriteFile(filepath.Join(dir, "task-state-filter-material.json"), raw, 0600)
	}
	clear(raw)
	agentAppRequire(t, err)
	published = true
	return w
}
func (w *taskStateFilterWeb) path() string {
	return variableHTTPPath(w.initial.ProjectID, "/tasks/"+w.initial.ID.String())
}
func (w *taskStateFilterWeb) observe(r *http.Request) error {
	if r.Method != http.MethodGet {
		return errors.New("state filter attempted mutation/Lookup")
	}
	if r.URL.Path != variableHTTPPath(w.initial.ProjectID, "/tasks") {
		return nil
	}
	q := r.URL.Query()
	if q.Get("limit") != "50" || q.Get("sprint_id") != w.initial.SprintID.String() || wc.TaskState(q.Get("state")).Validate() != nil {
		return errors.New("state filter lost Sprint/state/page size")
	}
	for key, values := range q {
		if len(values) != 1 || (key != "limit" && key != "sprint_id" && key != "state" && key != "text" && key != "priority" && key != "type" && key != "assignee_agent_id" && key != "cursor") {
			return errors.New("state filter changed query shape")
		}
	}
	combination := assigneeQueryKey(q)
	if combination != "|||" {
		expected := "null|" + taskStateFilterTerm + "|high|bug"
		if q.Get("state") == "todo" {
			expected = w.initial.AssigneeAgentID.String() + "|" + taskStateFilterTerm + "|high|bug"
		} else if q.Get("state") != "backlog" {
			return errors.New("excluded column requested under applied state")
		}
		if combination != expected {
			return errors.New("state filter lost exact AND combination")
		}
	}
	if q.Has("cursor") {
		w.mu.Lock()
		source, ok := w.cursors[q.Get("cursor")]
		w.mu.Unlock()
		if !ok || source.query != combination+"|"+q.Get("state") {
			return errors.New("state cursor crossed original applied query")
		}
	}
	return nil
}
func (w *taskStateFilterWeb) response(r *http.Response) (err error) {
	stage := "complete-response"
	defer func() {
		if err == nil {
			return
		}
		observed := time.Now()
		record := map[string]any{"observed_at_unix_ms": observed.UnixMilli(), "stage": stage, "error": blockerManagementSafeFailure(err), "context": blockerManagementSafeFailure(r.Request.Context().Err()), "status": r.StatusCode, "request_kind": w.failureRequestKind(r.Request)}
		w.mu.Lock()
		record["elapsed_node_ms"] = observed.Sub(w.browserStarted).Milliseconds()
		record["before_node_return"] = !w.browserReturned
		record["original_failure_already_set"] = w.failure != nil
		if w.responseFailure == nil {
			w.responseFailure = record
		}
		w.mu.Unlock()
	}()
	raw, err := w.completedResponse(r)
	if err != nil {
		return err
	}
	stage = "http-status"
	if r.StatusCode != 200 {
		return fmt.Errorf("original State Filter HTTP returned %d", r.StatusCode)
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	record := map[string]any{"request_id": r.Header.Get("X-Request-ID"), "method": r.Request.Method, "url_sha256": fmt.Sprintf("%x", sha256.Sum256([]byte(r.Request.URL.RequestURI()))), "status": 200, "content_length": len(raw), "body_sha256": fmt.Sprintf("%x", sha256.Sum256(raw)), "original_eof": true, "original_closed": true, "handler_returned": true}
	stage = "read-facts"
	if err = w.read(r.Request, raw); err != nil {
		return err
	}
	stage = "evidence-write"
	encoded, err := json.Marshal(record)
	if err != nil {
		return err
	}
	if err = os.MkdirAll(w.evidence, 0700); err != nil {
		return err
	}
	if err = os.WriteFile(filepath.Join(w.evidence, "task-state-filter-response-"+r.Header.Get("X-Request-ID")+".json"), encoded, 0600); err != nil {
		return err
	}
	r.Body = io.NopCloser(bytes.NewReader(raw))
	return nil
}
func (w *taskStateFilterWeb) expected(q url.Values) []wc.Task {
	rows := make([]wc.Task, 0)
	for _, task := range w.rows {
		if string(task.State) != q.Get("state") || q.Has("priority") && string(task.Priority) != q.Get("priority") || q.Has("type") && string(task.Type) != q.Get("type") {
			continue
		}
		if q.Has("assignee_agent_id") {
			assignee := q.Get("assignee_agent_id")
			if assignee == "null" {
				if task.AssigneeAgentID != nil {
					continue
				}
			} else if task.AssigneeAgentID == nil || task.AssigneeAgentID.String() != assignee {
				continue
			}
		}
		if q.Has("text") && !strings.Contains(task.Title, q.Get("text")) && !strings.Contains(task.Description, q.Get("text")) && !strings.Contains(task.Plan, q.Get("text")) {
			continue
		}
		rows = append(rows, task)
	}
	priority := map[wc.TaskPriority]int{wc.TaskPriorityCritical: 0, wc.TaskPriorityHigh: 1, wc.TaskPriorityMedium: 2, wc.TaskPriorityLow: 3}
	sort.Slice(rows, func(a, b int) bool {
		left, right := rows[a], rows[b]
		if left.Priority != right.Priority {
			return priority[left.Priority] < priority[right.Priority]
		}
		if left.ManualRank != right.ManualRank {
			return left.ManualRank < right.ManualRank
		}
		return left.ID.String() < right.ID.String()
	})
	return rows
}
func (w *taskStateFilterWeb) read(r *http.Request, raw []byte) error {
	if r.URL.Path == w.path() {
		var task wc.Task
		if json.Unmarshal(raw, &task) != nil || !reflect.DeepEqual(task, w.initial) {
			return errors.New("state-filter detail differs from persisted target")
		}
		w.detailReads++
	}
	if r.URL.Path != variableHTTPPath(w.initial.ProjectID, "/tasks") {
		return nil
	}
	q := r.URL.Query()
	var page f.Page[map[string]any]
	if json.Unmarshal(raw, &page) != nil || page.Items == nil {
		return errors.New("state-filter original page invalid")
	}
	expected := w.expected(q)
	offset := 0
	if q.Has("cursor") {
		offset = w.cursors[q.Get("cursor")].offset
	}
	end := offset + 50
	if end > len(expected) {
		end = len(expected)
	}
	if offset > end {
		return errors.New("cursor outside original sequence")
	}
	want := make([]map[string]any, 0, end-offset)
	for _, task := range expected[offset:end] {
		encoded, err := json.Marshal(task)
		if err != nil {
			return err
		}
		var item map[string]any
		if err = json.Unmarshal(encoded, &item); err != nil {
			return err
		}
		delete(item, "description")
		delete(item, "plan")
		want = append(want, item)
	}
	if !reflect.DeepEqual(page.Items, want) || (page.NextCursor != "") != (end < len(expected)) {
		return errors.New("state-filter page differs from independent persisted facts")
	}
	key := assigneeQueryKey(q)
	state := wc.TaskState(q.Get("state"))
	if page.NextCursor != "" {
		w.cursors[page.NextCursor] = assigneeCursor{query: key + "|" + string(state), offset: end}
	}
	if q.Has("cursor") {
		w.continuations++
	} else {
		if w.completed[key] == nil {
			w.completed[key] = map[wc.TaskState]int{}
		}
		w.completed[key][state]++
	}
	return nil
}
func (w *taskStateFilterWeb) facts(ctx context.Context) (string, error) {
	var value string
	err := w.base.raw.QueryRow(ctx, `SELECT jsonb_build_object(
 'tasks',(SELECT jsonb_agg(to_jsonb(t) ORDER BY id) FROM agenteam_work.tasks t WHERE project_id=$1::text::uuid),
 'commands',(SELECT jsonb_agg(to_jsonb(t) ORDER BY id) FROM agenteam_work.task_commands t WHERE project_id=$1::text::uuid),
 'transitions',(SELECT jsonb_agg(to_jsonb(t) ORDER BY id) FROM agenteam_work.task_transition_commands t WHERE project_id=$1::text::uuid),
 'events',(SELECT jsonb_agg(to_jsonb(t) ORDER BY id) FROM agenteam_work.task_events t WHERE project_id=$1::text::uuid),
 'orders',(SELECT jsonb_agg(to_jsonb(t) ORDER BY sprint_id,state,priority) FROM agenteam_work.task_order_groups t WHERE project_id=$1::text::uuid),
 'generations',(SELECT to_jsonb(t) FROM agenteam_work.task_query_generations t WHERE project_id=$1::text::uuid),
 'outbox',(SELECT jsonb_agg(to_jsonb(t) ORDER BY id) FROM agenteam_outbox.events t WHERE project_id=$1::text::uuid),
 'dispatches',(SELECT count(*) FROM agenteam_scheduler.dispatches WHERE project_id=$1::text),
 'executions',(SELECT count(*) FROM agenteam_execution.executions WHERE project_id=$1::text))::text`, w.initial.ProjectID.String()).Scan(&value)
	return value, err
}
func (w *taskStateFilterWeb) browser(t *testing.T, parent context.Context) {
	t.Helper()
	root, err := filepath.Abs("../account-captcha-web")
	agentAppRequire(t, err)
	ctx, cancel := context.WithTimeout(parent, 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "node", filepath.Join(root, "node_modules/@playwright/test/cli.js"), "test", "--config", filepath.Join(root, "task-state-filter.config.js"))
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
	cmd.Env = append(cmd.Env, "TMPDIR="+w.directory, "PLAYWRIGHT_NO_COPY_PROMPT=1", "AGENTEAM_AUTH_WEB_ORIGIN="+w.origin, "AGENTEAM_AUTH_WEB_PRIVATE="+w.directory, "AGENTEAM_AUTH_WEB_CHROMIUM=/usr/bin/chromium", "AGENTEAM_TASK_STATE_FILTER_WEB_DIST="+w.dist, "AGENTEAM_TASK_STATE_FILTER_WEB_EVIDENCE="+w.evidence)
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	w.mu.Lock()
	w.browserStarted = time.Now()
	w.mu.Unlock()
	if err = cmd.Start(); err != nil {
		t.Fatal("Task State Filter Node did not start")
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
	w.mu.Lock()
	w.browserReturned = true
	w.mu.Unlock()
	t.Logf("Task State Filter Node actual_wait pid=%d success=%t", cmd.Process.Pid, err == nil)
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
		w.saveFailure(t, ctx, err)
		t.Fatal("original Task State Filter browser failed")
	}
}

func (w *taskStateFilterWeb) verify(t *testing.T, ctx context.Context) {
	t.Helper()
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.failure != nil || w.continuations != 1 || w.detailReads != 1 {
		t.Fatal("state filter browser chain incomplete", w.failure)
	}
	for _, state := range []wc.TaskState{wc.TaskStateBacklog, wc.TaskStateTodo, wc.TaskStateInProgress, wc.TaskStateInReview, wc.TaskStateBlocked, wc.TaskStateDone, wc.TaskStateCancelled} {
		if w.completed["|||"][state] != 2 {
			t.Fatal("initial/cleared state reads incomplete", state)
		}
	}
	if w.completed["null|"+taskStateFilterTerm+"|high|bug"][wc.TaskStateBacklog] != 1 || w.completed[w.initial.AssigneeAgentID.String()+"|"+taskStateFilterTerm+"|high|bug"][wc.TaskStateTodo] != 3 {
		t.Fatal("applied combination or detail return reads incomplete")
	}
	for _, tail := range w.tails {
		select {
		case <-tail.done:
		default:
			t.Fatal("state filter handler tail open")
		}
		if !tail.returned {
			t.Fatal("state filter original handler did not return")
		}
	}
	after, err := w.facts(ctx)
	agentAppRequire(t, err)
	if after != w.before {
		t.Fatal("read-only state filter changed durable Work facts")
	}
	raw, err := os.ReadFile(filepath.Join(w.directory, "task-state-filter-result.json"))
	agentAppRequire(t, err)
	var result struct {
		Completed bool `json:"completed"`
		Writes    int  `json:"writes"`
	}
	if json.Unmarshal(raw, &result) != nil || !result.Completed || result.Writes != 0 {
		t.Fatal("original state filter consumers not retired")
	}
	t.Log("Task State Filter defaultApp runtime disabled; real backlog 50+1 cursor, draft binding, Todo AND filter, detail/back and clear; durable Work facts unchanged; all original tails joined")
}
func (w *taskStateFilterWeb) saveFailure(t *testing.T, ctx context.Context, nodeErr error) {
	t.Helper()
	w.mu.Lock()
	returned, open := 0, 0
	for _, tail := range w.tails {
		if tail.returned {
			returned++
		} else {
			open++
		}
	}
	record := map[string]any{"observed_at_unix_ms": time.Now().UnixMilli(), "node_started_at_unix_ms": w.browserStarted.UnixMilli(), "node_returned": w.browserReturned, "node_error": blockerManagementSafeFailure(nodeErr), "browser_context": blockerManagementSafeFailure(ctx.Err()), "original_failure": blockerManagementSafeFailure(w.failure), "response_failure": w.responseFailure, "handler_returned": returned, "handler_open": open, "continuations": w.continuations, "detail_reads": w.detailReads}
	w.mu.Unlock()
	raw, err := json.MarshalIndent(record, "", "  ")
	if err == nil {
		err = os.MkdirAll(w.evidence, 0700)
	}
	if err == nil {
		err = os.WriteFile(filepath.Join(w.evidence, "task-state-filter-go-failure.json"), raw, 0600)
	}
	if err != nil {
		t.Error("Blocker safe failure evidence could not be preserved")
	}
	t.Logf("Task State Filter safe failure: %s", raw)
}

func (w *taskStateFilterWeb) failureRequestKind(r *http.Request) string {
	if r.URL.Path == variableHTTPPath(w.initial.ProjectID, "/tasks") {
		switch r.URL.Query().Get("state") {
		case "backlog", "todo", "in_progress", "in_review", "blocked", "done", "cancelled":
			return "task-list-" + r.URL.Query().Get("state")
		}
		return "task-list-other"
	}
	if r.URL.Path == w.path() {
		return "task-detail"
	}
	if r.URL.Path == w.path()+"/blockers" {
		return "task-blockers"
	}
	if r.URL.Path == w.path()+"/events" {
		return "task-timeline"
	}
	return "other"
}
