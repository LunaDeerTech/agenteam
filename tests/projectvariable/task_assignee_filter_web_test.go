//go:build integration

package projectvariable_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
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

	"github.com/LunaDeerTech/agenteam/internal/central/agent"
	ac "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	wc "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
)

// Current Owner, real Agent create/Task transfer and the accepted Work HTTP
// shell. Browser operations are reads; this does not claim production SPA/App.
func TestTaskAssigneeFilterWeb(t *testing.T) {
	for _, mode := range []string{"assignee-pagination", "assignee-combination-detail"} {
		t.Run(mode, func(t *testing.T) {
			d := newTaskTransitionFixture(t)
			d.configureProject(t)
			startSchedulerSprint(t, d)
			w := newTaskAssigneeFilterWeb(t, d, mode)
			w.browser(t)
			w.shell.close(t)
			w.verify(t)
		})
	}
}

const assigneeFilterTerm = "AssigneeNeedle_%"

type assigneeCursor struct {
	query  string
	offset int
}
type taskAssigneeFilterWeb struct {
	shell                                  *taskReviewWeb
	domain                                 *taskTransitionFixture
	mode                                   string
	target, foreign                        wc.Task
	rows                                   []wc.Task
	agents                                 []ac.DirectoryEntry
	other                                  i.AgentID
	cursors                                map[string]assigneeCursor
	agentCursor                            string
	completed                              map[string]map[wc.TaskState]int
	continuations, agentPages, detailReads int
	before                                 []byte
}

func newTaskAssigneeFilterWeb(t *testing.T, d *taskTransitionFixture, mode string) *taskAssigneeFilterWeb {
	t.Helper()
	w := &taskAssigneeFilterWeb{domain: d, mode: mode, rows: []wc.Task{d.task.Clone()}, cursors: map[string]assigneeCursor{}, completed: map[string]map[wc.TaskState]int{}}
	reader, err := agent.NewDirectoryReader(d.base.tracked, d.agent.providers.Agents, d.base.keys)
	firstRoundRequire(t, err)
	original, err := reader.GetAgent(ctxFor(t), d.base.ownerBrowser.actor, d.base.project.ID, d.agentID)
	firstRoundRequire(t, err)
	reader.Stop()
	firstRoundRequire(t, reader.Drain(ctxFor(t)))
	if !reader.Joined() {
		t.Fatal("original seed directory reader did not join")
	}
	w.agents = append(w.agents, original)
	// Directory order is newest first: the real original Agent lies on page 2.
	for n := 1; n <= 50; n++ {
		request, command, _ := d.agent.request(t)
		fields := request.Fields()
		fields.Name = fmt.Sprintf("filter-agent-%02d", n)
		if n == 50 {
			label := "另一位 Agent"
			fields.DisplayName = &label
			w.other = fields.AgentID
		}
		request, err = ac.NewAgentCreate(fields)
		firstRoundRequire(t, err)
		result, e := d.agent.agents.CreateAgent(ctxFor(t), d.base.ownerBrowser.actor, command, d.base.project.ID, request)
		firstRoundRequire(t, e)
		core := result.Fields().Agent.Fields().Core
		if result.Validate() != nil || core.ID != fields.AgentID || core.Version != 1 {
			t.Fatal("formal Agent creation failed")
		}
		w.agents = append(w.agents, ac.DirectoryEntry{ID: core.ID, ProjectID: core.ProjectID, Name: core.Name, DisplayName: core.DisplayName, TagColor: core.TagColor, Description: core.Description, Version: core.Version, CreatedAt: core.CreatedAt, UpdatedAt: core.UpdatedAt})
	}
	sort.Slice(w.agents, func(a, b int) bool {
		left, right := w.agents[a], w.agents[b]
		if left.CreatedAt != right.CreatedAt {
			return left.CreatedAt.Time().After(right.CreatedAt.Time())
		}
		return left.ID.String() > right.ID.String()
	})
	if w.agents[50].ID != d.agentID {
		t.Fatal("target Agent not beyond first directory page")
	}
	create := func(project wc.ProjectID, sprint wc.SprintID, key, title string, priority wc.TaskPriority, typ wc.TaskType, assignee *i.AgentID) wc.Task {
		t.Helper()
		created, e := d.tasks.CreateTask(ctxFor(t), d.base.ownerBrowser.actor, meta(t, key, nil), project, wc.TaskCreate{TaskID: id[wc.Task](t), SprintID: sprint, Title: title, Description: "Stored " + title, Plan: "Plan " + title, Priority: priority, Type: typ})
		firstRoundRequire(t, e)
		if created.Validate() != nil || !created.Changed || created.Task.State != wc.TaskStateBacklog || created.Task.AssigneeAgentID != nil {
			t.Fatal("formal Task seed failed")
		}
		current := created.Task.Clone()
		if assignee != nil {
			request := wc.TaskTransfer{TargetState: wc.TaskStateTodo, AssigneeAgentID: assignee, AddBlockers: []wc.TaskBlockerCreate{}, ResolveBlockerIDs: []wc.TaskBlockerID{}}
			result, e := d.transitions.TransferTask(ctxFor(t), d.base.ownerBrowser.actor, meta(t, key+"-assign", &current.Version), project, current.ID, request)
			firstRoundRequire(t, e)
			if result.Validate() != nil || result.Task.State != wc.TaskStateTodo || result.Task.Version != 2 || result.Task.AssigneeAgentID == nil || *result.Task.AssigneeAgentID != *assignee {
				t.Fatal("formal Task assignment failed")
			}
			current = result.Task.Clone()
		}
		if project == d.task.ProjectID {
			w.rows = append(w.rows, current)
		}
		return current
	}
	for n := 0; n < 50; n++ {
		create(d.task.ProjectID, d.task.SprintID, fmt.Sprintf("assignee-filler-%02d", n), fmt.Sprintf("Assigned filler %02d", n), wc.TaskPriorityHigh, wc.TaskTypeBug, &d.agentID)
	}
	w.target = create(d.task.ProjectID, d.task.SprintID, "assignee-target", assigneeFilterTerm+" target", wc.TaskPriorityHigh, wc.TaskTypeBug, &d.agentID)
	create(d.task.ProjectID, d.task.SprintID, "assignee-type", assigneeFilterTerm+" other type", wc.TaskPriorityHigh, wc.TaskTypeFeature, &d.agentID)
	create(d.task.ProjectID, d.task.SprintID, "assignee-priority", assigneeFilterTerm+" other priority", wc.TaskPriorityLow, wc.TaskTypeBug, &d.agentID)
	create(d.task.ProjectID, d.task.SprintID, "assignee-other", assigneeFilterTerm+" other Agent", wc.TaskPriorityHigh, wc.TaskTypeBug, &w.other)
	create(d.task.ProjectID, d.task.SprintID, "assignee-null", assigneeFilterTerm+" unassigned", wc.TaskPriorityHigh, wc.TaskTypeBug, nil)
	other, _, _ := d.base.createProject(t, d.base.ownerBrowser.actor, "assignee-other")
	milestone := wc.CreateMilestoneRequest{MilestoneID: id[wc.Milestone](t), Title: "Other Agent filter milestone", Description: "Independent Project"}
	_, err = d.structure.CreateMilestone(ctxFor(t), d.base.ownerBrowser.actor, meta(t, "assignee-other-milestone", nil), other.ID, milestone)
	firstRoundRequire(t, err)
	sprint := wc.CreateSprintRequest{SprintID: id[pc.Sprint](t), MilestoneID: milestone.MilestoneID, Title: "Other filter Sprint", Description: "Not part of the current Sprint"}
	_, err = d.structure.CreateSprint(ctxFor(t), d.base.ownerBrowser.actor, meta(t, "assignee-other-sprint", nil), other.ID, sprint)
	firstRoundRequire(t, err)
	w.foreign = create(other.ID, sprint.SprintID, "assignee-foreign", assigneeFilterTerm+" foreign Project", wc.TaskPriorityHigh, wc.TaskTypeBug, nil)
	ready := make(chan struct{})
	w.shell = newTaskWebShell(t, d, mode, taskWebProfile{
		envPrefix: "AGENTEAM_TASK_ASSIGNEE_FILTER_WEB", filePrefix: "task-assignee-filter", configName: "task-assignee-filter.config.js", logName: "Task Assignee Filter",
		observe: func(r *http.Request) error { <-ready; return w.observe(r) }, response: func(r *http.Response) error { <-ready; return w.response(r) },
		material: func(username string) map[string]any {
			return map[string]any{"mode": mode, "cookie": d.base.ownerBrowser.cookie, "project_id": d.task.ProjectID, "sprint_id": d.task.SprintID, "target": w.target, "foreign_id": w.foreign.ID, "agent_id": d.agentID, "other_agent_id": w.other, "agents": w.agents, "rows": w.rows, "term": assigneeFilterTerm, "input_hash": os.Getenv("AGENTEAM_TASK_ASSIGNEE_FILTER_WEB_INPUT_HASH"), "route": "/" + username + "/" + d.base.project.NormalizedName + "/tasks"}
		},
	})
	// All formal writes and shell construction have returned before the baseline.
	// No background writer is started by this read-only HTTP composition.
	w.before, err = w.facts(ctxFor(t))
	firstRoundRequire(t, err)
	close(ready)
	return w
}
func assigneeQueryKey(q url.Values) string {
	return q.Get("assignee_agent_id") + "|" + q.Get("text") + "|" + q.Get("priority") + "|" + q.Get("type")
}
func (w *taskAssigneeFilterWeb) observe(r *http.Request) error {
	if r.Method != http.MethodGet {
		return errors.New("Agent filter browser attempted mutation/Lookup")
	}
	prefix := "/api/v1/projects/" + w.domain.task.ProjectID.String()
	if r.URL.Path != prefix+"/tasks" && r.URL.Path != prefix+"/agents" {
		return nil
	}
	q := r.URL.Query()
	if q.Get("limit") != "50" {
		return errors.New("filter lost fixed page size")
	}
	if r.URL.Path == prefix+"/agents" {
		for key, values := range q {
			if len(values) != 1 || (key != "limit" && key != "cursor") {
				return errors.New("Agent directory query changed")
			}
		}
		if q.Has("cursor") {
			w.shell.mu.Lock()
			cursor := w.agentCursor
			w.shell.mu.Unlock()
			if cursor == "" || q.Get("cursor") != cursor {
				return errors.New("Agent directory continuation is foreign")
			}
		}
		return nil
	}
	if q.Get("sprint_id") != w.target.SprintID.String() || wc.TaskState(q.Get("state")).Validate() != nil {
		return errors.New("filter lost current Sprint/state")
	}
	for key, values := range q {
		if len(values) != 1 || (key != "sprint_id" && key != "state" && key != "limit" && key != "text" && key != "priority" && key != "type" && key != "cursor" && key != "assignee_agent_id") {
			return errors.New("filter added or duplicated query fields")
		}
	}
	if q.Has("assignee_agent_id") && q.Get("assignee_agent_id") != "null" && q.Get("assignee_agent_id") != w.domain.agentID.String() && q.Get("assignee_agent_id") != w.other.String() {
		return errors.New("filter fabricated Agent identity")
	}
	if q.Has("text") && q.Get("text") != assigneeFilterTerm {
		return errors.New("filter changed literal keyword")
	}
	if q.Has("priority") && q.Get("priority") != "high" && q.Get("priority") != "low" {
		return errors.New("unexpected applied priority")
	}
	if q.Has("type") && q.Get("type") != "bug" && q.Get("type") != "feature" {
		return errors.New("unexpected applied type")
	}
	if q.Has("cursor") {
		w.shell.mu.Lock()
		source, ok := w.cursors[q.Get("cursor")]
		w.shell.mu.Unlock()
		if !ok || source.query != assigneeQueryKey(q)+"|"+q.Get("state") {
			return errors.New("task cursor crossed its original applied filters")
		}
	}
	return nil
}
func (w *taskAssigneeFilterWeb) expected(q url.Values) []wc.Task {
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
func (w *taskAssigneeFilterWeb) response(r *http.Response) error {
	raw, err := w.shell.completedResponse(r)
	if err != nil {
		return err
	}
	prefix := "/api/v1/projects/" + w.target.ProjectID.String()
	q := r.Request.URL.Query()
	if r.Request.URL.Path == prefix+"/tasks/"+w.target.ID.String() {
		var task wc.Task
		if r.StatusCode != 200 || json.Unmarshal(raw, &task) != nil || !reflect.DeepEqual(task, w.target) {
			return errors.New("filtered detail differs from real Task")
		}
		w.shell.mu.Lock()
		w.detailReads++
		w.shell.mu.Unlock()
	}
	if r.Request.URL.Path == prefix+"/agents" {
		var page f.Page[ac.DirectoryEntry]
		if r.StatusCode != 200 || json.Unmarshal(raw, &page) != nil || page.Items == nil {
			return errors.New("real Agent directory response failed")
		}
		from, to := 0, 50
		if q.Has("cursor") {
			from, to = 50, 51
		}
		if !reflect.DeepEqual(page.Items, w.agents[from:to]) || (page.NextCursor != "") != (to == 50) {
			return errors.New("Agent directory lost real publication/order")
		}
		w.shell.mu.Lock()
		if to == 50 {
			w.agentCursor = page.NextCursor
		} else {
			w.agentPages++
		}
		w.shell.mu.Unlock()
		return nil
	}
	if r.Request.URL.Path != prefix+"/tasks" {
		return nil
	}
	var page f.Page[map[string]any]
	if r.StatusCode != 200 || json.Unmarshal(raw, &page) != nil || page.Items == nil {
		return errors.New("actual task page did not complete")
	}
	expected := w.expected(q)
	offset := 0
	if q.Has("cursor") {
		w.shell.mu.Lock()
		offset = w.cursors[q.Get("cursor")].offset
		w.shell.mu.Unlock()
	}
	end := offset + 50
	if end > len(expected) {
		end = len(expected)
	}
	if offset > end {
		return errors.New("cursor outside original expected sequence")
	}
	want := make([]map[string]any, 0, end-offset)
	for _, task := range expected[offset:end] {
		encoded, e := json.Marshal(task)
		if e != nil {
			return e
		}
		var row map[string]any
		if e = json.Unmarshal(encoded, &row); e != nil {
			return e
		}
		delete(row, "description")
		delete(row, "plan")
		want = append(want, row)
	}
	if !reflect.DeepEqual(page.Items, want) || (page.NextCursor != "") != (end < len(expected)) {
		return errors.New("AND/assignee filter differs from independent persisted facts")
	}
	w.shell.mu.Lock()
	defer w.shell.mu.Unlock()
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
func (w *taskAssigneeFilterWeb) facts(ctx context.Context) ([]byte, error) {
	var raw []byte
	err := w.domain.base.raw.QueryRow(ctx, `SELECT jsonb_build_object(
 'projects',(SELECT jsonb_agg(to_jsonb(p) ORDER BY id) FROM agenteam_project.projects p),
 'agents',(SELECT jsonb_agg(to_jsonb(a) ORDER BY id) FROM agenteam_agent.agents a),
 'agent_commands',(SELECT jsonb_agg(to_jsonb(c) ORDER BY project_id,id) FROM agenteam_agent.commands c),
 'tasks',(SELECT jsonb_agg(to_jsonb(t) ORDER BY id) FROM agenteam_work.tasks t),
 'commands',(SELECT jsonb_agg(to_jsonb(c) ORDER BY project_id,id) FROM agenteam_work.task_commands c),
 'transitions',(SELECT jsonb_agg(to_jsonb(c) ORDER BY project_id,id) FROM agenteam_work.task_transition_commands c),
 'events',(SELECT jsonb_agg(to_jsonb(e) ORDER BY project_id,id) FROM agenteam_work.task_events e),
 'generations',(SELECT jsonb_agg(to_jsonb(g) ORDER BY project_id) FROM agenteam_work.task_query_generations g),
 'orders',(SELECT jsonb_agg(to_jsonb(g) ORDER BY project_id,sprint_id,state,priority) FROM agenteam_work.task_order_groups g),
 'outbox',(SELECT jsonb_agg(to_jsonb(e) ORDER BY id) FROM agenteam_outbox.events e),
 'audit',(SELECT jsonb_agg(to_jsonb(a) ORDER BY id) FROM agenteam_audit.audit_records a),
 'dispatches',(SELECT count(*) FROM agenteam_scheduler.dispatches),
 'executions',(SELECT count(*) FROM agenteam_execution.executions))`).Scan(&raw)
	return raw, err
}
func (w *taskAssigneeFilterWeb) verify(t *testing.T) {
	t.Helper()
	w.shell.mu.Lock()
	defer w.shell.mu.Unlock()
	if w.shell.failure != nil || w.shell.cut != 0 || w.shell.transfers != 0 || w.shell.lookups != 0 || w.shell.logouts != 0 {
		t.Fatal("read-only assignee browser observation failed", w.shell.failure)
	}
	queries := []string{"|||"}
	if w.mode == "assignee-pagination" {
		queries = append(queries, w.domain.agentID.String()+"||high|bug", w.other.String()+"||low|feature", "null|||", w.other.String()+"|||")
		if w.continuations != 1 {
			t.Fatal("original applied task cursor not consumed exactly once")
		}
	} else {
		for _, assignee := range []string{w.domain.agentID.String(), w.other.String(), "null"} {
			queries = append(queries, assignee+"|"+assigneeFilterTerm+"|high|bug")
		}
		if w.detailReads < 1 || w.continuations != 0 {
			t.Fatal("real combined detail or fresh page isolation missing")
		}
	}
	if w.agentPages < 1 {
		t.Fatal("real Agent second directory page not consumed")
	}
	for _, key := range queries {
		for _, state := range []wc.TaskState{wc.TaskStateBacklog, wc.TaskStateTodo, wc.TaskStateInProgress, wc.TaskStateInReview, wc.TaskStateBlocked, wc.TaskStateDone, wc.TaskStateCancelled} {
			if w.completed[key][state] == 0 {
				t.Fatal("declared assignee filter did not complete every column", key, state)
			}
		}
	}
	for _, tail := range w.shell.tails {
		if !tail.returned {
			t.Fatal("original assignee HTTP handler did not return")
		}
	}
	after, err := w.facts(ctxFor(t))
	firstRoundRequire(t, err)
	if !bytes.Equal(w.before, after) {
		t.Fatal("read-only assignee filter changed A/B canonical, commands, history or Outbox")
	}
	t.Logf("Task Assignee Filter real mode=%s AgentPages=%d TaskContinuations=%d detail=%d all_project_facts_unchanged=true", w.mode, w.agentPages, w.continuations, w.detailReads)
}

func (owner *taskAssigneeFilterWeb) browser(t *testing.T) {
	w := owner.shell
	t.Helper()
	root, err := filepath.Abs("../account-captcha-web")
	firstRoundRequire(t, err)
	// The browser owns its declared budget; ctxFor is a shorter DB operation budget.
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "node", filepath.Join(root, "node_modules/@playwright/test/cli.js"), "test", "--config", filepath.Join(root, w.profile.configName))
	cmd.Dir = root
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.WaitDelay = time.Second
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
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
	for _, variable := range []struct{ key, directory string }{{"TMPDIR", "tmp"}, {"TMP", "tmp"}, {"TEMP", "tmp"}, {"NODE_COMPILE_CACHE", "node-compile-cache"}, {"PWTEST_CACHE_DIR", "playwright-cache"}, {"XDG_CACHE_HOME", "xdg-cache"}} {
		directory := filepath.Join(w.directory, variable.directory)
		firstRoundRequire(t, os.MkdirAll(directory, 0700))
		cmd.Env = append(cmd.Env, variable.key+"="+directory)
	}
	cmd.Env = append(cmd.Env, "PLAYWRIGHT_NO_COPY_PROMPT=1", "AGENTEAM_AUTH_WEB_ORIGIN="+w.origin, "AGENTEAM_AUTH_WEB_PRIVATE="+w.directory, "AGENTEAM_AUTH_WEB_CHROMIUM=/usr/bin/chromium", w.profile.envPrefix+"_DIST="+os.Getenv(w.profile.envPrefix+"_DIST"), w.profile.envPrefix+"_CASE="+w.mode, w.profile.envPrefix+"_EVIDENCE="+w.evidence, w.profile.envPrefix+"_INPUT_HASH="+w.inputHash)
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	if err := cmd.Start(); err != nil {
		t.Fatal("owned Playwright did not start")
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
	t.Logf("%s Node actual_wait pid=%d success=%t", w.profile.logName, cmd.Process.Pid, err == nil)
	if err != nil {
		ctxErr := ctx.Err()
		exitCode := -1
		if cmd.ProcessState != nil {
			exitCode = cmd.ProcessState.ExitCode()
		}
		t.Logf("%s Node exit deadline=%t cancelled=%t exit_code=%d", w.profile.logName, errors.Is(ctxErr, context.DeadlineExceeded), errors.Is(ctxErr, context.Canceled), exitCode)
	}
	safe := output.String()
	w.mu.Lock()
	tokens := append([]string(nil), w.keys...)
	w.mu.Unlock()
	tokens = append(tokens, w.domain.base.ownerBrowser.cookie, w.domain.base.ownerBrowser.csrf)
	for _, token := range tokens {
		if token != "" {
			safe = strings.ReplaceAll(safe, token, "[redacted]")
		}
	}
	t.Log(safe)
	if err != nil {
		t.Fatal("original Playwright child failed and was waited")
	}
}
