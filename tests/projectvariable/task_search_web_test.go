//go:build integration

package projectvariable_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	wc "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
)

const taskSearchMode = "server-search-beyond-first-page"

// The original Task services prepare every row. The browser performs only
// reads through the existing real Account/Project/Work HTTP shell; this does
// not claim default Project creation or production SPA hosting.
func TestTaskSearchWeb(t *testing.T) {
	t.Run(taskSearchMode, func(t *testing.T) {
		d := newTaskTransitionFixture(t)
		d.configureProject(t)
		startSchedulerSprint(t, d)
		w := newTaskSearchWeb(t, d)
		w.shell.browser(t)
		w.shell.close(t)
		w.verify(t)
	})
}

type taskSearchWeb struct {
	shell      *taskReviewWeb
	domain     *taskTransitionFixture
	target     wc.Task
	foreign    wc.Task
	first      []wc.TaskID
	terms      []string
	before     []byte
	completed  map[string]map[wc.TaskState]int
	detailRead int
}

func newTaskSearchWeb(t *testing.T, d *taskTransitionFixture) *taskSearchWeb {
	t.Helper()
	w := &taskSearchWeb{domain: d, terms: []string{"TitleNeedle_%", "DescriptionNeedle_%", "PlanNeedle_%"}, completed: make(map[string]map[wc.TaskState]int)}
	create := func(project wc.ProjectID, sprint wc.SprintID, key, title, description, plan string) wc.Task {
		t.Helper()
		result, err := d.tasks.CreateTask(ctxFor(t), d.base.ownerBrowser.actor, meta(t, key, nil), project, wc.TaskCreate{
			TaskID: id[wc.Task](t), SprintID: sprint, Title: title, Description: description, Plan: plan,
			Type: wc.TaskTypeTask, Priority: wc.TaskPriorityMedium,
		})
		firstRoundRequire(t, err)
		if result.Validate() != nil || !result.Changed || result.Task.State != wc.TaskStateBacklog || result.Task.Version != 1 {
			t.Fatal("search setup did not commit an ordinary backlog Task")
		}
		return result.Task.Clone()
	}
	// One original Task plus fifty ordinary writes put the appended target
	// beyond the UI's first fifty. The actual Reader below verifies that fact.
	for n := 0; n < 49; n++ {
		create(d.task.ProjectID, d.task.SprintID, fmt.Sprintf("search-filler-%02d", n), fmt.Sprintf("Search filler %02d", n), "Ordinary Task description", "Ordinary Task plan")
	}
	create(d.task.ProjectID, d.task.SprintID, "search-literal-decoy", "TitleNeedleXY wildcard decoy", "DescriptionNeedleXY", "PlanNeedleXY")
	w.target = create(d.task.ProjectID, d.task.SprintID, "search-target", w.terms[0]+" outside the first page", w.terms[1]+" in the stored description", w.terms[2]+" in the stored plan")
	backlog := wc.TaskStateBacklog
	page, err := d.taskReader.ListTasks(ctxFor(t), d.base.ownerBrowser.actor, d.task.ProjectID,
		wc.TaskFilter{SprintID: &d.task.SprintID, State: &backlog}, f.PageRequest{Limit: 50})
	firstRoundRequire(t, err)
	if len(page.Items) != 50 || page.NextCursor == "" {
		t.Fatal("search fixture must have a real second Task page")
	}
	for _, row := range page.Items {
		if row.ID == w.target.ID {
			t.Fatal("search target unexpectedly appears on the first page")
		}
		w.first = append(w.first, row.ID)
	}
	// Another real project owned by the same Human contains the identical
	// keywords. Search must retain the selected Project/Sprint boundary.
	other, _, _ := d.base.createProject(t, d.base.ownerBrowser.actor, "search-other")
	milestone := wc.CreateMilestoneRequest{MilestoneID: id[wc.Milestone](t), Title: "Other search milestone", Description: "Same Owner, separate Project"}
	_, err = d.structure.CreateMilestone(ctxFor(t), d.base.ownerBrowser.actor, meta(t, "search-other-milestone", nil), other.ID, milestone)
	firstRoundRequire(t, err)
	sprint := wc.CreateSprintRequest{SprintID: id[pc.Sprint](t), MilestoneID: milestone.MilestoneID, Title: "Other search sprint", Description: "Must not leak into the selected Sprint"}
	_, err = d.structure.CreateSprint(ctxFor(t), d.base.ownerBrowser.actor, meta(t, "search-other-sprint", nil), other.ID, sprint)
	firstRoundRequire(t, err)
	w.foreign = create(other.ID, sprint.SprintID, "search-foreign-target", "Other Project "+w.terms[0], w.terms[1], w.terms[2])
	w.before, err = w.facts(ctxFor(t))
	firstRoundRequire(t, err)
	ready := make(chan struct{})
	w.shell = newTaskWebShell(t, d, taskSearchMode, taskWebProfile{
		envPrefix: "AGENTEAM_TASK_SEARCH_WEB", filePrefix: "task-search", configName: "task-search.config.js", logName: "Task Search",
		observe:  func(r *http.Request) error { <-ready; return w.observe(r) },
		response: func(r *http.Response) error { <-ready; return w.response(r) },
		material: func(username string) map[string]any {
			return map[string]any{"mode": taskSearchMode, "cookie": d.base.ownerBrowser.cookie,
				"project_id": d.task.ProjectID, "sprint_id": d.task.SprintID, "target": w.target,
				"foreign_id": w.foreign.ID, "first_ids": w.first, "terms": w.terms,
				"route": "/" + username + "/" + d.base.project.NormalizedName + "/tasks"}
		},
	})
	close(ready)
	return w
}

func (w *taskSearchWeb) observe(r *http.Request) error {
	if r.Method != http.MethodGet {
		return errors.New("search browser attempted a mutation")
	}
	if r.URL.Path != "/api/v1/projects/"+w.target.ProjectID.String()+"/tasks" {
		return nil
	}
	q := r.URL.Query()
	if q.Get("sprint_id") != w.target.SprintID.String() || q.Get("limit") != "50" || q.Has("cursor") || wc.TaskState(q.Get("state")).Validate() != nil {
		return errors.New("search lost its exact Sprint, state or first-page budget")
	}
	for key, values := range q {
		if len(values) != 1 || (key != "sprint_id" && key != "state" && key != "limit" && key != "text") {
			return errors.New("search supplied an unexpected or duplicate query field")
		}
	}
	text := q.Get("text")
	if q.Has("text") && text != w.terms[0] && text != w.terms[1] && text != w.terms[2] && text != strings.ToLower(w.terms[0]) {
		return errors.New("search changed the literal keyword")
	}
	return nil
}

func (w *taskSearchWeb) response(r *http.Response) error {
	raw, err := w.shell.completedResponse(r)
	if err != nil {
		return err
	}
	prefix := "/api/v1/projects/" + w.target.ProjectID.String() + "/tasks"
	if r.Request.URL.Path == prefix+"/"+w.target.ID.String() {
		var task wc.Task
		if r.StatusCode != http.StatusOK || json.Unmarshal(raw, &task) != nil || !reflect.DeepEqual(task, w.target) {
			return errors.New("search detail differs from the original committed Task")
		}
		w.shell.mu.Lock()
		w.detailRead++
		w.shell.mu.Unlock()
	}
	if r.Request.URL.Path != prefix {
		return nil
	}
	var page struct {
		Items []struct {
			ID        wc.TaskID    `json:"id"`
			ProjectID wc.ProjectID `json:"project_id"`
			SprintID  wc.SprintID  `json:"sprint_id"`
			State     wc.TaskState `json:"state"`
		} `json:"items"`
		Next string `json:"next_cursor"`
	}
	if r.StatusCode != http.StatusOK || json.Unmarshal(raw, &page) != nil || page.Items == nil {
		return errors.New("original search HTTP page did not complete")
	}
	q := r.Request.URL.Query()
	text, state := q.Get("text"), wc.TaskState(q.Get("state"))
	ids := make([]wc.TaskID, 0, len(page.Items))
	for _, row := range page.Items {
		if row.ProjectID != w.target.ProjectID || row.SprintID != w.target.SprintID || row.State != state || row.ID == w.foreign.ID {
			return errors.New("search crossed the original Project or Sprint")
		}
		ids = append(ids, row.ID)
	}
	want := []wc.TaskID{}
	if state == wc.TaskStateBacklog {
		if text == "" {
			want = w.first
		} else if text != strings.ToLower(w.terms[0]) {
			want = []wc.TaskID{w.target.ID}
		}
	}
	if !reflect.DeepEqual(ids, want) || ((page.Next != "") != (state == wc.TaskStateBacklog && text == "")) {
		return errors.New("server search did not return the independent expected page")
	}
	w.shell.mu.Lock()
	if w.completed[text] == nil {
		w.completed[text] = make(map[wc.TaskState]int)
	}
	w.completed[text][state]++
	w.shell.mu.Unlock()
	return nil
}

func (w *taskSearchWeb) facts(ctx context.Context) ([]byte, error) {
	var raw []byte
	err := w.domain.base.raw.QueryRow(ctx, `SELECT jsonb_build_object(
 'tasks',(SELECT jsonb_agg(to_jsonb(t) ORDER BY id) FROM agenteam_work.tasks t),
 'commands',(SELECT jsonb_agg(to_jsonb(c) ORDER BY project_id,id) FROM agenteam_work.task_commands c),
 'events',(SELECT jsonb_agg(to_jsonb(e) ORDER BY project_id,id) FROM agenteam_work.task_events e),
 'generations',(SELECT jsonb_agg(to_jsonb(g) ORDER BY project_id) FROM agenteam_work.task_query_generations g))`).Scan(&raw)
	return raw, err
}

func (w *taskSearchWeb) verify(t *testing.T) {
	t.Helper()
	w.shell.mu.Lock()
	defer w.shell.mu.Unlock()
	if w.shell.failure != nil {
		t.Fatal("original search HTTP observation failed", w.shell.failure)
	}
	if w.detailRead == 0 {
		t.Fatal("browser did not read the actual matching Task detail")
	}
	for _, term := range append(append([]string{""}, w.terms...), strings.ToLower(w.terms[0])) {
		for _, state := range []wc.TaskState{wc.TaskStateBacklog, wc.TaskStateTodo, wc.TaskStateInProgress, wc.TaskStateInReview, wc.TaskStateBlocked, wc.TaskStateDone, wc.TaskStateCancelled} {
			if w.completed[term][state] == 0 {
				t.Fatal("browser did not complete every server-filtered column")
			}
		}
	}
	for _, tail := range w.shell.tails {
		if !tail.returned {
			t.Fatal("original search HTTP handler did not return")
		}
	}
	after, err := w.facts(ctxFor(t))
	firstRoundRequire(t, err)
	if string(w.before) != string(after) {
		t.Fatal("read-only browser search changed Task, command, event or generation facts")
	}
}
