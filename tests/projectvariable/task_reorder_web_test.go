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
	"strings"
	"syscall"
	"testing"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	wc "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
)

// The default App owns the real Work services. All seeds and the competing
// reorder use its Human HTTP routes. The recovery control changes delivery only
// after the original successful response, EOF, Close and bridge return.
func TestTaskReorderWeb(t *testing.T) {
	for _, mode := range []string{"task-reorder-normal", "task-reorder-recovery"} {
		t.Run(mode, func(t *testing.T) {
			entered, completed := false, false
			t.Cleanup(func() {
				if !entered || !completed {
					t.Error("Task reorder default App handoff incomplete")
				}
			})
			a := newAgentCreateFixture(t)
			original := a.p2.base
			project, err := a.p2.creator.GetSchedulerConfig(ctxFor(t), original.ownerBrowser.actor, a.p2.project.ID)
			agentAppRequire(t, err)
			if project.Config.Enabled || project.Project.CurrentSprintID != nil {
				t.Fatal("Task reorder requires the original paused Project")
			}
			process, err := a.guard.CurrentProcess()
			agentAppRequire(t, err)
			var username string
			agentAppRequire(t, original.raw.QueryRow(ctxFor(t), `SELECT username FROM agenteam_account.users WHERE id=$1`, original.ownerBrowser.actor.Details().UserID).Scan(&username))
			if original.afterOwners != nil {
				t.Fatal("original handoff already occupied")
			}
			original.afterOwners = func() {
				entered = true
				if !original.core.Joined() || !a.agents.Joined() || !a.p2.service.Joined() {
					t.Fatal("original setup owners have not returned")
				}
				if _, err := a.guard.CurrentProcess(); err == nil {
					t.Fatal("original setup guard still held")
				}
				var stopped bool
				agentAppRequire(t, original.raw.QueryRow(ctxFor(t), `SELECT state='stopped' AND stopped_at IS NOT NULL FROM agenteam_object.process_claims WHERE process_id=$1`, process.String()).Scan(&stopped))
				if !stopped {
					t.Fatal("original setup process has not stopped")
				}
				w := newTaskReorderWeb(t, a, project, mode)
				defer w.dispose(t)
				withAgentConfigurationApp(t, a, process.String(), w.origin, func(ctx context.Context, address string) {
					defer w.close(t)
					w.startObserved(t, address, w.observe, w.response)
					w.prepare(t, ctx)
					w.material(t, username)
					w.browser(t, ctx)
					w.verify(t, ctx)
					w.close(t)
				})
				completed = true
			}
		})
	}
}

type taskReorderInput struct {
	Expected f.Version      `json:"expected_version"`
	Request  wc.TaskReorder `json:"request"`
}
type taskReorderIntent struct {
	kind                  string
	key                   f.IdempotencyKey
	input                 taskReorderInput
	before                wc.Task
	requestID             f.ID[f.Request]
	bodyDigest, keyDigest [sha256.Size]byte
}
type taskReorderIntentKey struct{}
type taskReorderWeb struct {
	*milestoneCreateWeb
	sprint                        wc.Sprint
	initial, current              wc.Task
	anchor                        wc.TaskID
	rows                          []wc.Task
	writes, committed             []taskReorderIntent
	results                       []wc.TaskMutation
	conflicts, lookups, gets      int
	candidatePages, continuations int
	inflight, unresolved          bool
	initialCounts                 [4]int64
	fixed, other                  string
}

func newTaskReorderWeb(t *testing.T, a *agentCreateFixture, project pc.SchedulerProject, mode string) *taskReorderWeb {
	t.Helper()
	const prefix = "AGENTEAM_TASK_REORDER_WEB"
	dist, runtime, evidence := os.Getenv(prefix+"_DIST"), os.Getenv("AGENTEAM_AUTH_WEB_RUNTIME"), os.Getenv(prefix+"_EVIDENCE")
	if !filepath.IsAbs(dist) || !filepath.IsAbs(runtime) || len(runtime) > 45 || !filepath.IsAbs(evidence) {
		t.Fatal("Task reorder requires exact dist, private runtime and evidence paths")
	}
	if info, err := os.Stat(filepath.Join(dist, "index.html")); err != nil || !info.Mode().IsRegular() {
		t.Fatal("Task reorder production dist missing")
	}
	directory, err := os.MkdirTemp(runtime, "trw-")
	agentAppRequire(t, err)
	agentAppRequire(t, os.Chmod(directory, 0700))
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		_ = os.RemoveAll(directory)
		t.Fatal(err)
	}
	base := &agentConfigurationWeb{a: a, mode: mode, origin: "http://" + listener.Addr().String(), directory: directory, evidence: evidence, inputHash: os.Getenv(prefix + "_INPUT_HASH"), dist: dist, listener: listener, serve: make(chan error, 1), tails: map[string]*reviewWebTail{}}
	return &taskReorderWeb{milestoneCreateWeb: &milestoneCreateWeb{sprintStartWeb: &sprintStartWeb{agentConfigurationWeb: base, project: project.Clone()}}}
}
func (w *taskReorderWeb) path() string {
	return variableHTTPPath(w.project.Project.ID, "/tasks/"+w.initial.ID.String())
}
func (w *taskReorderWeb) prepare(t *testing.T, ctx context.Context) {
	w.prepareEmpty(t, ctx)
	for group, p := range []pc.ProjectID{w.project.Project.ID, w.project.Project.ID, w.a.p2.base.project.ID} {
		m := wc.CreateMilestoneRequest{MilestoneID: id[wc.Milestone](t), Title: fmt.Sprintf("Task reorder milestone %d", group), Description: "Formal HTTP parent"}
		reply := w.request(t, http.MethodPost, variableHTTPPath(p, "/milestones"), jsonBytes(t, map[string]any{"request": m}), f.IdempotencyKey(id[struct{}](t).String()))
		var parent wc.StructureMutation
		if reply.status != 200 || json.Unmarshal(reply.body, &parent) != nil || parent.Validate() != nil || parent.Milestone == nil || parent.Milestone.ID != m.MilestoneID {
			t.Fatalf("Task reorder formal milestone seed failed group=%d status=%d", group, reply.status)
		}
		s := wc.CreateSprintRequest{SprintID: id[pc.Sprint](t), MilestoneID: m.MilestoneID, Title: fmt.Sprintf("Task reorder Sprint %d", group), Description: "Formal HTTP Sprint"}
		reply = w.request(t, http.MethodPost, variableHTTPPath(p, "/sprints"), jsonBytes(t, map[string]any{"request": s}), f.IdempotencyKey(id[struct{}](t).String()))
		var created wc.StructureMutation
		if reply.status != 200 || json.Unmarshal(reply.body, &created) != nil || created.Validate() != nil || created.Sprint == nil || created.Sprint.ID != s.SprintID {
			t.Fatalf("Task reorder formal Sprint seed failed group=%d status=%d", group, reply.status)
		}
		count := 1
		if group == 0 {
			w.sprint = created.Sprint.Clone()
			count = 53 // 52 same-group Tasks plus one other-priority Task.
		}
		for n := 0; n < count; n++ {
			priority := wc.TaskPriorityMedium
			if group == 0 && n == 52 {
				priority = wc.TaskPriorityHigh
			}
			input := wc.TaskCreate{TaskID: id[wc.Task](t), SprintID: s.SprintID, Title: fmt.Sprintf("Reorder task %d-%02d", group, n+1), Description: "Preserve original description", Plan: "Preserve original plan", Type: wc.TaskTypeTask, Priority: priority}
			reply = w.request(t, http.MethodPost, variableHTTPPath(p, "/tasks"), jsonBytes(t, map[string]any{"request": input}), f.IdempotencyKey(id[struct{}](t).String()))
			var task wc.TaskMutation
			if reply.status != 200 || json.Unmarshal(reply.body, &task) != nil || task.Validate() != nil || !task.Changed || task.Task.ID != input.TaskID || task.Task.ProjectID != p || task.Task.SprintID != s.SprintID || task.Task.Priority != priority || task.Task.State != wc.TaskStateBacklog || task.Task.AssigneeAgentID != nil || task.Task.Version != 1 {
				t.Fatalf("Task reorder formal Task seed failed group=%d item=%d status=%d", group, n, reply.status)
			}
			if group == 0 && n < 52 {
				w.rows = append(w.rows, task.Task.Clone())
			}
		}
	}
	// The board's current Sprint is changed only by the original Start endpoint.
	reply := w.request(t, http.MethodPost, variableHTTPPath(w.project.Project.ID, "/sprints/"+w.sprint.ID.String()+"/start"), jsonBytes(t, map[string]any{"expected_version": w.sprint.Version, "request": map[string]any{}}), f.IdempotencyKey(id[struct{}](t).String()))
	var started wc.SprintStartMutation
	if reply.status != 200 || json.Unmarshal(reply.body, &started) != nil || started.Validate() != nil || started.Sprint.ID != w.sprint.ID || started.Sprint.State != wc.Current {
		t.Fatalf("Task reorder formal Sprint Start failed status=%d", reply.status)
	}
	w.sprint, w.project.Project = started.Sprint.Clone(), started.Project.Clone()
	w.initial, w.current, w.anchor = w.rows[0].Clone(), w.rows[0].Clone(), w.rows[51].ID
	var err error
	w.initialCounts, err = w.counts(ctx)
	agentAppRequire(t, err)
	w.fixed, err = w.fixedFacts(ctx)
	agentAppRequire(t, err)
	w.other, err = w.otherFacts(ctx)
	agentAppRequire(t, err)
}
func (w *taskReorderWeb) material(t *testing.T, username string) {
	raw := jsonBytes(t, map[string]any{"mode": w.mode, "origin": w.origin, "cookie": w.a.p2.base.ownerBrowser.cookie, "project_id": w.project.Project.ID, "other_project_id": w.a.p2.base.project.ID, "sprint_id": w.sprint.ID, "task_id": w.initial.ID, "anchor_id": w.anchor, "task": w.initial, "rows": w.rows, "route": "/" + username + "/" + w.project.Project.NormalizedName + "/tasks/" + w.initial.ID.String(), "input_hash": w.inputHash})
	agentAppRequire(t, os.WriteFile(filepath.Join(w.directory, "task-reorder-material.json"), raw, 0600))
	clear(raw)
}

func (w *taskReorderWeb) observe(r *http.Request) error {
	if r.Method == http.MethodGet {
		return nil
	}
	lookupPath := variableHTTPPath(w.project.Project.ID, "/task-commands/lookup")
	if r.Method != http.MethodPost || r.URL.RawQuery != "" || r.URL.ForceQuery || (r.URL.Path != w.path()+"/reorder" && r.URL.Path != lookupPath) {
		return errors.New("undeclared Task reorder mutation")
	}
	raw, readErr := io.ReadAll(io.LimitReader(r.Body, 16385))
	closeErr := r.Body.Close()
	key := f.IdempotencyKey(r.Header.Get("Idempotency-Key"))
	if readErr != nil || closeErr != nil || len(raw) > 16384 || key.Validate() != nil || len(r.Header.Values("Idempotency-Key")) != 1 {
		return errors.New("incomplete original Task reorder input")
	}
	r.Body = io.NopCloser(bytes.NewReader(raw))
	in := taskReorderIntent{key: key, bodyDigest: sha256.Sum256(raw), keyDigest: sha256.Sum256([]byte(key))}
	if r.URL.Path == lookupPath {
		var wire struct {
			Command  wc.TaskCommandName `json:"command"`
			Target   wc.TaskID          `json:"target_id"`
			Expected f.Version          `json:"expected_version"`
			Request  wc.TaskReorder     `json:"request"`
		}
		if agentWebDecode(raw, &wire) != nil {
			return errors.New("invalid original reorder Lookup")
		}
		w.mu.Lock()
		defer w.mu.Unlock()
		if w.mode != "task-reorder-recovery" || w.inflight || !w.unresolved || w.cut != 1 || w.lookups != 0 || len(w.writes) != 2 || len(w.results) != 2 {
			return errors.New("Lookup was automatic, repeated, or without an original result")
		}
		original := w.writes[1]
		if key != original.key || wire.Command != wc.TaskCommandReorder || wire.Target != w.initial.ID || wire.Expected != original.input.Expected || !reflect.DeepEqual(wire.Request, original.input.Request) {
			return errors.New("Lookup changed original key, target, version or anchor")
		}
		in.kind, in.input, in.before = "lookup", original.input, original.before
		w.inflight = true
	} else {
		if agentWebDecode(raw, &in.input) != nil || in.input.Request.Validate() != nil {
			return errors.New("invalid original reorder envelope")
		}
		w.mu.Lock()
		index := len(w.writes)
		limit := 3
		if w.mode == "task-reorder-recovery" {
			limit = 2
		}
		valid := !w.inflight && !w.unresolved && index < limit && in.input.Expected == w.current.Version
		for _, old := range w.writes {
			valid = valid && old.key != key
		}
		want := wc.TaskReorder{}
		if index == 0 || w.mode == "task-reorder-recovery" {
			want.BeforeID = &w.anchor
		}
		valid = valid && reflect.DeepEqual(in.input.Request, want)
		if w.mode == "task-reorder-recovery" && index == 1 {
			valid = valid && w.conflicts == 1
		}
		in.kind, in.before = "reorder", w.current.Clone()
		if valid {
			w.inflight = true
			w.writes = append(w.writes, in)
			w.keys = append(w.keys, string(key))
		}
		w.mu.Unlock()
		if !valid {
			return errors.New("reorder repeated or changed original version/anchor")
		}
		if w.mode == "task-reorder-recovery" && index == 0 {
			if err := w.compete(r); err != nil {
				return err
			}
			in.kind = "conflict"
		}
	}
	*r = *r.WithContext(context.WithValue(r.Context(), taskReorderIntentKey{}, in))
	return nil
}

func (w *taskReorderWeb) compete(original *http.Request) error {
	// A separate real Human request moves this same Task to the tail. The
	// browser's already observed version-1 intent is forwarded unchanged.
	key, err := f.NewID[struct{}]()
	if err != nil {
		return err
	}
	w.mu.Lock()
	in := taskReorderIntent{kind: "concurrent", key: f.IdempotencyKey(key.String()), input: taskReorderInput{Expected: w.current.Version}, before: w.current.Clone()}
	w.keys = append(w.keys, string(in.key))
	w.mu.Unlock()
	raw, err := json.Marshal(in.input)
	if err != nil {
		return err
	}
	r, err := http.NewRequestWithContext(original.Context(), http.MethodPost, w.backend.URL+w.path()+"/reorder", bytes.NewReader(raw))
	if err != nil {
		return err
	}
	r.Host = original.Host
	for _, name := range []string{"Cookie", "Origin", "Sec-Fetch-Site", "X-CSRF-Token"} {
		for _, value := range original.Header.Values(name) {
			r.Header.Add(name, value)
		}
	}
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Idempotency-Key", string(in.key))
	client := &http.Client{Transport: w.transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Do(r)
	if err != nil {
		return err
	}
	body, err := w.completedResponse(response)
	if err != nil {
		return err
	}
	var result wc.TaskMutation
	if response.StatusCode != 200 || json.Unmarshal(body, &result) != nil {
		return errors.New("concurrent original Human reorder did not complete")
	}
	in.requestID, err = f.ParseID[f.Request](response.Header.Get("X-Request-ID"))
	if err != nil {
		return err
	}
	return w.acceptMutation(original.Context(), in, result, true)
}

func (w *taskReorderWeb) response(r *http.Response) error {
	raw, err := w.completedResponse(r)
	if err != nil {
		return err
	}
	in, mutation := r.Request.Context().Value(taskReorderIntentKey{}).(taskReorderIntent)
	cut := false
	if mutation {
		in.requestID, err = f.ParseID[f.Request](r.Header.Get("X-Request-ID"))
		if err != nil {
			return err
		}
		switch in.kind {
		case "conflict":
			var problem struct {
				Code        f.Code        `json:"code"`
				CommitState f.CommitState `json:"commit_state"`
			}
			if r.StatusCode != 409 || json.Unmarshal(raw, &problem) != nil || problem.Code != f.TaskVersionConflict || problem.CommitState != f.NotCommitted {
				return errors.New("stale original version was not rejected by Work")
			}
			var count int
			if err = w.a.p2.base.raw.QueryRow(r.Request.Context(), `SELECT count(*) FROM agenteam_work.task_commands WHERE project_id=$1::text::uuid AND command_name='work.task.reorder' AND idempotency_key=$2`, w.project.Project.ID.String(), string(in.key)).Scan(&count); err != nil || count != 0 {
				return errors.New("rejected version created a command")
			}
			w.mu.Lock()
			w.conflicts++
			w.inflight = false
			w.mu.Unlock()
		case "reorder":
			var result wc.TaskMutation
			if r.StatusCode != 200 || json.Unmarshal(raw, &result) != nil {
				return errors.New("original Task reorder did not return a receipt")
			}
			w.mu.Lock()
			changed := w.mode == "task-reorder-recovery" || len(w.writes) < 3
			w.mu.Unlock()
			if err = w.acceptMutation(r.Request.Context(), in, result, changed); err != nil {
				return err
			}
			w.mu.Lock()
			cut = w.mode == "task-reorder-recovery"
			w.inflight, w.unresolved = false, cut
			w.mu.Unlock()
		case "lookup":
			var result wc.TaskCommandLookup
			w.mu.Lock()
			original := w.results[len(w.results)-1].Clone()
			w.mu.Unlock()
			if r.StatusCode != 200 || json.Unmarshal(raw, &result) != nil || result.Validate() != nil || result.Status != wc.LookupCommitted || result.Receipt == nil || !reflect.DeepEqual(*result.Receipt, original) {
				return errors.New("explicit Lookup did not recover the original receipt")
			}
			w.mu.Lock()
			w.lookups++
			w.inflight, w.unresolved = false, false
			w.mu.Unlock()
		default:
			return errors.New("unrecognized browser mutation")
		}
	} else if r.Request.Method == http.MethodGet && r.StatusCode == 200 {
		if err = w.read(r.Request, raw); err != nil {
			return err
		}
	}
	if err = w.proof(r, raw, in, mutation, cut); err != nil {
		return err
	}
	if cut {
		return &reviewWebDrop{r.Header.Clone(), len(raw)}
	}
	r.Body = io.NopCloser(bytes.NewReader(raw))
	return nil
}

func (w *taskReorderWeb) acceptMutation(ctx context.Context, in taskReorderIntent, result wc.TaskMutation, changed bool) error {
	if result.Validate() != nil || result.Changed != changed || in.input.Expected != in.before.Version {
		return errors.New("original reorder receipt lost change/version semantics")
	}
	want := in.before.Clone()
	if changed {
		want.Version++
		want.ManualRank, want.UpdatedAt = result.Task.ManualRank, result.Task.UpdatedAt
		if want.ManualRank == in.before.ManualRank {
			return errors.New("changed reorder retained its old rank")
		}
	}
	if !reflect.DeepEqual(want, result.Task) {
		return errors.New("reorder changed Task content, group or assignment")
	}
	if err := w.commandFacts(ctx, in, result); err != nil {
		return err
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	rows := make([]wc.Task, 0, len(w.rows))
	for _, task := range w.rows {
		if task.ID == w.initial.ID {
			continue
		}
		if in.input.Request.BeforeID != nil && task.ID == *in.input.Request.BeforeID {
			rows = append(rows, result.Task.Clone())
		}
		rows = append(rows, task)
	}
	if in.input.Request.BeforeID == nil {
		rows = append(rows, result.Task.Clone())
	}
	if len(rows) != 52 {
		return errors.New("original anchor missing from independent sequence")
	}
	w.rows, w.current = rows, result.Task.Clone()
	w.committed = append(w.committed, in)
	w.results = append(w.results, result.Clone())
	return nil
}

func (w *taskReorderWeb) commandFacts(ctx context.Context, in taskReorderIntent, result wc.TaskMutation) error {
	actor := w.a.p2.base.ownerBrowser.actor
	digest, err := wc.TaskReorderDigest(actor, f.CommandMeta{RequestID: in.requestID, IdempotencyKey: in.key, ExpectedVersion: &in.input.Expected}, w.project.Project.ID, w.initial.ID, in.input.Request)
	if err != nil {
		return err
	}
	var command string
	var storedRaw, requestRaw []byte
	err = w.a.p2.base.raw.QueryRow(ctx, `SELECT id::text,receipt,request->'reorder' FROM agenteam_work.task_commands WHERE project_id=$1::text::uuid AND command_name='work.task.reorder' AND idempotency_key=$2 AND actor_user_id=$3::text::uuid AND semantic_digest=$4 AND state='completed' AND committed_at IS NOT NULL AND request->>'target_id'=$5::text AND request->>'expected_version'=$6::text AND request->'create'='null'::jsonb AND request->'update'='null'::jsonb`, w.project.Project.ID.String(), string(in.key), actor.Details().UserID, digest.String(), w.initial.ID.String(), in.input.Expected.String()).Scan(&command, &storedRaw, &requestRaw)
	if err != nil {
		return err
	}
	var stored wc.TaskMutation
	var request wc.TaskReorder
	if json.Unmarshal(storedRaw, &stored) != nil || json.Unmarshal(requestRaw, &request) != nil || !reflect.DeepEqual(stored, result) || !reflect.DeepEqual(request, in.input.Request) {
		return errors.New("durable command differs from original full intent/receipt")
	}
	var history, events int
	err = w.a.p2.base.raw.QueryRow(ctx, `SELECT
 (SELECT count(*) FROM agenteam_work.task_events WHERE project_id=$1::text::uuid AND task_id=$2::text::uuid AND operation_id=$3::text::uuid),
 (SELECT count(*) FROM agenteam_outbox.events WHERE project_id=$1::text::uuid AND aggregate_id=$2::text::uuid AND convert_from(payload,'UTF8')::jsonb->>'command_id'=$3::text)`, w.project.Project.ID.String(), w.initial.ID.String(), command).Scan(&history, &events)
	if err != nil {
		return err
	}
	count := 0
	if result.Changed {
		count = 1
	}
	if history != count || events != count {
		return errors.New("reorder/no-op history or Outbox cardinality differs")
	}
	return nil
}

func (w *taskReorderWeb) read(r *http.Request, raw []byte) error {
	if r.URL.Path == w.path() {
		var task wc.Task
		if json.Unmarshal(raw, &task) != nil || task.Validate() != nil || task.ID != w.initial.ID || task.ProjectID != w.project.Project.ID {
			return errors.New("current Task read is invalid")
		}
		w.mu.Lock()
		defer w.mu.Unlock()
		// A concurrent read can legitimately precede the controlled mutation.
		valid := reflect.DeepEqual(task, w.initial)
		for _, result := range w.results {
			valid = valid || reflect.DeepEqual(task, result.Task)
		}
		if !valid {
			return errors.New("Get returned no original Task image")
		}
		w.gets++
	}
	q := r.URL.Query()
	if r.URL.Path == variableHTTPPath(w.project.Project.ID, "/tasks") && q.Get("priority") == "medium" {
		if q.Get("sprint_id") != w.sprint.ID.String() || q.Get("state") != "backlog" || q.Get("limit") != "50" || q.Has("text") || q.Has("type") || q.Has("assignee_agent_id") {
			return errors.New("candidate query inherited unrelated board filters")
		}
		var page f.Page[wc.Task]
		if json.Unmarshal(raw, &page) != nil || page.Items == nil || len(page.Items) > 50 {
			return errors.New("candidate page was not a real bounded Task page")
		}
		for _, task := range page.Items {
			if task.Validate() != nil || task.ProjectID != w.project.Project.ID || task.SprintID != w.sprint.ID || task.State != wc.TaskStateBacklog || task.Priority != wc.TaskPriorityMedium || task.AssigneeAgentID != nil {
				return errors.New("candidate crossed the original Task group")
			}
		}
		w.mu.Lock()
		w.candidatePages++
		if q.Get("cursor") != "" {
			w.continuations++
		}
		w.mu.Unlock()
	}
	return nil
}

func (w *taskReorderWeb) counts(ctx context.Context) (out [4]int64, err error) {
	err = w.a.p2.base.raw.QueryRow(ctx, `SELECT
 (SELECT count(*) FROM agenteam_work.task_commands WHERE project_id=$1::text::uuid AND command_name='work.task.reorder'),
 (SELECT count(*) FROM agenteam_work.task_events WHERE project_id=$1::text::uuid AND task_id=$2::text::uuid),
 (SELECT order_generation FROM agenteam_work.task_order_groups WHERE project_id=$1::text::uuid AND sprint_id=$3::text::uuid AND state='backlog' AND priority='medium'),
 (SELECT query_generation FROM agenteam_work.task_query_generations WHERE project_id=$1::text::uuid)`, w.project.Project.ID.String(), w.initial.ID.String(), w.sprint.ID.String()).Scan(&out[0], &out[1], &out[2], &out[3])
	return
}
func (w *taskReorderWeb) fixedFacts(ctx context.Context) (string, error) {
	var out string
	err := w.a.p2.base.raw.QueryRow(ctx, `SELECT jsonb_build_object(
 'project',(SELECT to_jsonb(p) FROM agenteam_project.projects p WHERE id=$1::text::uuid),
 'milestones',(SELECT jsonb_agg(to_jsonb(m) ORDER BY id) FROM agenteam_work.milestones m WHERE project_id=$1::text::uuid),
 'sprints',(SELECT jsonb_agg(to_jsonb(s) ORDER BY id) FROM agenteam_work.sprints s WHERE project_id=$1::text::uuid),
 'siblings',(SELECT jsonb_agg(to_jsonb(t) ORDER BY id) FROM agenteam_work.tasks t WHERE project_id=$1::text::uuid AND id<>$2::text::uuid),
 'target_fixed',(SELECT to_jsonb(t)-'manual_rank'-'version'-'updated_at' FROM agenteam_work.tasks t WHERE project_id=$1::text::uuid AND id=$2::text::uuid),
 'commands',(SELECT jsonb_agg(to_jsonb(c) ORDER BY id) FROM agenteam_work.task_commands c WHERE project_id=$1::text::uuid AND command_name<>'work.task.reorder'),
 'history',(SELECT jsonb_agg(to_jsonb(h) ORDER BY id) FROM agenteam_work.task_events h WHERE project_id=$1::text::uuid AND NOT EXISTS(SELECT 1 FROM agenteam_work.task_commands c WHERE c.project_id=h.project_id AND c.id=h.operation_id AND c.command_name='work.task.reorder')),
 'structure',(SELECT jsonb_agg(to_jsonb(c) ORDER BY id) FROM agenteam_work.structure_commands c WHERE project_id=$1::text::uuid),
 'starts',(SELECT jsonb_agg(to_jsonb(c) ORDER BY id) FROM agenteam_work.sprint_start_commands c WHERE project_id=$1::text::uuid),
 'other_groups',(SELECT jsonb_agg(to_jsonb(g) ORDER BY sprint_id,state,priority) FROM agenteam_work.task_order_groups g WHERE project_id=$1::text::uuid AND NOT(sprint_id=$3::text::uuid AND state='backlog' AND priority='medium')),
 'events',(SELECT jsonb_agg(to_jsonb(e) ORDER BY id) FROM agenteam_outbox.events e WHERE project_id=$1::text::uuid AND NOT EXISTS(SELECT 1 FROM agenteam_work.task_commands c WHERE c.project_id=e.project_id AND c.event_id=e.id AND c.command_name='work.task.reorder')),
 'audit',(SELECT jsonb_agg(to_jsonb(a) ORDER BY id) FROM agenteam_audit.audit_records a WHERE project_id=$1::text::uuid),
 'executions',(SELECT count(*) FROM agenteam_execution.executions WHERE project_id=$1::text),
 'dispatches',(SELECT count(*) FROM agenteam_scheduler.dispatches WHERE project_id=$1::text))::text`, w.project.Project.ID.String(), w.initial.ID.String(), w.sprint.ID.String()).Scan(&out)
	return out, err
}
func (w *taskReorderWeb) otherFacts(ctx context.Context) (string, error) {
	var out string
	err := w.a.p2.base.raw.QueryRow(ctx, `SELECT jsonb_build_object(
 'project',(SELECT to_jsonb(p) FROM agenteam_project.projects p WHERE id=$1::text::uuid),
 'tasks',(SELECT jsonb_agg(to_jsonb(t) ORDER BY id) FROM agenteam_work.tasks t WHERE project_id=$1::text::uuid),
 'commands',(SELECT jsonb_agg(to_jsonb(c) ORDER BY id) FROM agenteam_work.task_commands c WHERE project_id=$1::text::uuid),
 'history',(SELECT jsonb_agg(to_jsonb(h) ORDER BY id) FROM agenteam_work.task_events h WHERE project_id=$1::text::uuid),
 'groups',(SELECT jsonb_agg(to_jsonb(g) ORDER BY sprint_id,state,priority) FROM agenteam_work.task_order_groups g WHERE project_id=$1::text::uuid),
 'query',(SELECT to_jsonb(g) FROM agenteam_work.task_query_generations g WHERE project_id=$1::text::uuid),
 'events',(SELECT jsonb_agg(to_jsonb(e) ORDER BY id) FROM agenteam_outbox.events e WHERE project_id=$1::text::uuid),
 'audit',(SELECT jsonb_agg(to_jsonb(a) ORDER BY id) FROM agenteam_audit.audit_records a WHERE project_id=$1::text::uuid))::text`, w.a.p2.base.project.ID.String()).Scan(&out)
	return out, err
}

func (w *taskReorderWeb) verify(t *testing.T, ctx context.Context) {
	t.Helper()
	w.mu.Lock()
	failure := w.failure
	writes, results := len(w.writes), append([]wc.TaskMutation(nil), w.results...)
	committed := append([]taskReorderIntent(nil), w.committed...)
	conflicts, lookups, cut, gets := w.conflicts, w.lookups, w.cut, w.gets
	pages, continuations, pending := w.candidatePages, w.continuations, w.inflight || w.unresolved
	current, rows := w.current.Clone(), append([]wc.Task(nil), w.rows...)
	tails := make([]*reviewWebTail, 0, len(w.tails))
	for _, tail := range w.tails {
		tails = append(tails, tail)
	}
	w.mu.Unlock()
	wantWrites, wantResults, wantRecovery := 3, 3, 0
	if w.mode == "task-reorder-recovery" {
		wantWrites, wantResults, wantRecovery = 2, 2, 1
	}
	if failure != nil || pending || writes != wantWrites || len(results) != wantResults || conflicts != wantRecovery || lookups != wantRecovery || cut != wantRecovery || gets < 2 || pages < 2 || continuations < 1 {
		t.Fatalf("Task reorder browser chain incomplete: writes=%d results=%d conflict=%d lookup=%d cut=%d gets=%d pages=%d continuation=%d pending=%t error=%v", writes, len(results), conflicts, lookups, cut, gets, pages, continuations, pending, failure)
	}
	for _, tail := range tails {
		select {
		case <-tail.done:
		default:
			t.Fatal("original browser bridge has not returned")
		}
		if !tail.returned {
			t.Fatal("original browser bridge return missing")
		}
	}
	for index, intent := range committed {
		agentAppRequire(t, w.commandFacts(ctx, intent, results[index]))
	}
	counts, err := w.counts(ctx)
	agentAppRequire(t, err)
	if counts != ([4]int64{w.initialCounts[0] + int64(wantResults), w.initialCounts[1] + 2, w.initialCounts[2] + 2, w.initialCounts[3] + 2}) {
		t.Fatal("reorder/conflict/no-op duplicated command/history or generations")
	}
	fixed, err := w.fixedFacts(ctx)
	agentAppRequire(t, err)
	other, err := w.otherFacts(ctx)
	agentAppRequire(t, err)
	if fixed != w.fixed || other != w.other {
		t.Fatal("Task reorder changed another group, content, lifecycle or Project B")
	}
	// Current public reads, after all browser work, independently confirm the
	// persisted order. Expected identities were moved without computing ranks.
	reply := w.request(t, http.MethodGet, w.path(), nil, "")
	var task wc.Task
	if reply.status != 200 || json.Unmarshal(reply.body, &task) != nil || !reflect.DeepEqual(task, current) {
		t.Fatal("final actual HTTP Get differs from the original receipt")
	}
	var got []wc.Task
	cursor := ""
	for round := 0; round < 2; round++ {
		q := url.Values{"sprint_id": {w.sprint.ID.String()}, "state": {"backlog"}, "priority": {"medium"}, "limit": {"50"}}
		if cursor != "" {
			q.Set("cursor", cursor)
		}
		reply = w.request(t, http.MethodGet, variableHTTPPath(w.project.Project.ID, "/tasks")+"?"+q.Encode(), nil, "")
		var page f.Page[wc.Task]
		if reply.status != 200 || json.Unmarshal(reply.body, &page) != nil || page.Items == nil || (page.NextCursor != "") != (round == 0) {
			t.Fatal("final actual HTTP Task page incomplete")
		}
		got, cursor = append(got, page.Items...), page.NextCursor
	}
	if !reflect.DeepEqual(got, rows) {
		t.Fatal("final real Task order differs from the intended before/tail sequence")
	}
	t.Logf("Task reorder original default App: mode=%s browser_writes=%d committed=%d conflict=%d explicit_lookup=%d changed=2 B_unchanged=true", w.mode, writes, wantResults, conflicts, lookups)
}

func (w *taskReorderWeb) proof(r *http.Response, raw []byte, in taskReorderIntent, mutation, cut bool) error {
	record := map[string]any{"input_hash": w.inputHash, "request_id": r.Header.Get("X-Request-ID"), "method": r.Request.Method, "url_sha256": fmt.Sprintf("%x", sha256.Sum256([]byte(r.Request.URL.RequestURI()))), "status": r.StatusCode, "content_length": len(raw), "body_sha256": fmt.Sprintf("%x", sha256.Sum256(raw)), "original_eof": true, "original_closed": true, "handler_returned": true, "request_host_matches": r.Request.Host == w.listener.Addr().String()}
	if mutation {
		record["kind"], record["input_verified"] = in.kind, true
		record["request_body_sha256"] = fmt.Sprintf("%x", in.bodyDigest)
		record["idempotency_key_sha256"] = fmt.Sprintf("%x", in.keyDigest)
		record["committed_verified"] = in.kind != "conflict"
		record["rejected_verified"], record["receipt_cut"] = in.kind == "conflict", cut
	}
	encoded, err := json.Marshal(record)
	if err != nil {
		return err
	}
	if err = os.MkdirAll(w.evidence, 0700); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(w.evidence, "task-reorder-"+w.mode+"-response-"+r.Header.Get("X-Request-ID")+".json"), encoded, 0600)
}

func (w *taskReorderWeb) browser(t *testing.T, parent context.Context) {
	t.Helper()
	root, err := filepath.Abs("../account-captcha-web")
	agentAppRequire(t, err)
	ctx, cancel := context.WithTimeout(parent, 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "node", filepath.Join(root, "node_modules/@playwright/test/cli.js"), "test", "--config", filepath.Join(root, "task-reorder.config.js"))
	cmd.Dir = root
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.WaitDelay = time.Second
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		switch key {
		case "TMPDIR", "TMP", "TEMP", "NODE_COMPILE_CACHE", "PWTEST_CACHE_DIR", "XDG_CACHE_HOME", "DEBUG", "PWDEBUG", "PLAYWRIGHT_NO_COPY_PROMPT":
			continue
		}
		if !strings.HasPrefix(key, "AGENTEAM_") {
			cmd.Env = append(cmd.Env, entry)
		}
	}
	for _, variable := range []struct{ key, directory string }{{"TMPDIR", "tmp"}, {"TMP", "tmp"}, {"TEMP", "tmp"}, {"NODE_COMPILE_CACHE", "node-cache"}, {"PWTEST_CACHE_DIR", "playwright-cache"}, {"XDG_CACHE_HOME", "xdg-cache"}} {
		directory := filepath.Join(w.directory, variable.directory)
		agentAppRequire(t, os.MkdirAll(directory, 0700))
		cmd.Env = append(cmd.Env, variable.key+"="+directory)
	}
	const prefix = "AGENTEAM_TASK_REORDER_WEB"
	cmd.Env = append(cmd.Env, "PLAYWRIGHT_NO_COPY_PROMPT=1", "AGENTEAM_AUTH_WEB_ORIGIN="+w.origin, "AGENTEAM_AUTH_WEB_PRIVATE="+w.directory, "AGENTEAM_AUTH_WEB_CHROMIUM=/usr/bin/chromium", prefix+"_DIST="+w.dist, prefix+"_CASE="+w.mode, prefix+"_EVIDENCE="+w.evidence, prefix+"_INPUT_HASH="+w.inputHash)
	var output bytes.Buffer
	cmd.Stdout, cmd.Stderr = &output, &output
	if err = cmd.Start(); err != nil {
		t.Fatal("Task reorder browser did not start")
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
	t.Logf("Task reorder Node actual_wait pid=%d success=%t", cmd.Process.Pid, err == nil)
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
		t.Fatal("original Task reorder browser failed")
	}
}
