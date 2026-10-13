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

const taskCancelComment = "明确取消此任务。\n保留 <script>globalThis.__cancel_executed = true</script> 原文。"
const taskCancelBlocker = "等待人工确认；取消任务不等于解决此阻塞。"

func TestTaskCancelWeb(t *testing.T) {
	t.Run("explicit-cancel-preserves-relations-and-completes-sprint", func(t *testing.T) {
		entered, completed := false, false
		t.Cleanup(func() {
			if !entered || !completed {
				t.Error("Task Cancel public App handoff incomplete")
			}
		})
		v := assembleTaskHumanHTTPFixture(t, true)
		a, base := v.domain.agent, v.domain.base
		body, _, key := v.intent(t)
		assigned := v.requireTransition(t, v.request(t, base.ownerBrowser, "POST", v.transferPath(), body, key)).Task
		if assigned.AssigneeAgentID == nil || assigned.State != wc.TaskStateTodo {
			t.Fatal("formal assigned Todo seed missing")
		}
		created, err := v.domain.tasks.CreateTask(ctxFor(t), base.ownerBrowser.actor, meta(t, "cancel-second-task", nil), base.project.ID, wc.TaskCreate{TaskID: id[wc.Task](t), SprintID: assigned.SprintID, Title: "Cancel original task with unresolved blocker", Type: wc.TaskTypeTask, Priority: wc.TaskPriorityMedium})
		agentAppRequire(t, err)
		add := wc.TaskBlockerCreate{BlockerID: id[wc.TaskBlockerIdentity](t), Type: wc.TaskBlockerWaitingForHuman, Description: taskCancelBlocker, Metadata: wc.TaskBlockerMetadata{WaitingForHuman: &wc.TaskBlockerWaitingForHumanMetadata{}}}
		agentAppRequire(t, add.Validate())
		response := v.request(t, base.ownerBrowser, "POST", variableHTTPPath(base.project.ID, "/tasks/"+created.Task.ID.String()+"/blockers"), string(jsonBytes(t, map[string]any{"expected_version": created.Task.Version, "request": add})), "cancel-original-blocker")
		var blocked wc.TaskBlockerMutation
		if response.status != 200 || json.Unmarshal(response.body, &blocked) != nil || blocked.Validate() != nil || blocked.Blocker.ResolvedAt != nil || blocked.Task.State != wc.TaskStateBacklog || blocked.Task.AssigneeAgentID != nil {
			t.Fatal("formal backlog blocker seed failed")
		}
		sprint := sprintCompleteStart(t, v, sprintCompleteGet(t, v, assigned.SprintID), "cancel-current-sprint")
		project, err := base.projects.GetSchedulerConfig(ctxFor(t), base.ownerBrowser.actor, base.project.ID)
		agentAppRequire(t, err)
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
				t.Fatal("original setup ProcessGuard still held")
			}
			var stopped bool
			agentAppRequire(t, base.raw.QueryRow(ctxFor(t), `SELECT state='stopped' AND stopped_at IS NOT NULL FROM agenteam_object.process_claims WHERE process_id=$1`, process.String()).Scan(&stopped))
			if !stopped {
				t.Fatal("original setup process not retired")
			}
			w := newTaskCancelWeb(t, a, base, [2]wc.Task{assigned, blocked.Task}, blocked.Blocker, sprint, project, username)
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

type taskCancelIntent struct {
	kind                  string
	index                 int
	key                   f.IdempotencyKey
	requestID             f.ID[f.Request]
	bodyDigest, keyDigest [32]byte
	request               wc.TaskTransfer
}
type taskCancelIntentKey struct{}
type taskCancelWeb struct {
	*sprintStartWeb
	base                                                                          *variableHTTPFixture
	initial, current                                                              [2]wc.Task
	blocker                                                                       wc.TaskBlocker
	sprint                                                                        wc.Sprint
	project                                                                       pc.SchedulerProject
	receipts                                                                      [2]wc.TaskTransitionMutation
	history                                                                       [2][]wc.TaskTransitionEvent
	completion                                                                    wc.SprintCompleteMutation
	writes                                                                        []string
	blockersBefore, afterComplete                                                 string
	beforeCompletionTasks                                                         json.RawMessage
	taskReads, timelineReads, blockerReads, boardReads, projectReads, sprintReads int
}

func newTaskCancelWeb(t *testing.T, a *agentCreateFixture, base *variableHTTPFixture, tasks [2]wc.Task, blocker wc.TaskBlocker, sprint wc.Sprint, project pc.SchedulerProject, username string) *taskCancelWeb {
	t.Helper()
	const prefix = "AGENTEAM_TASK_CANCEL_WEB"
	dist, runtime, evidence := os.Getenv(prefix+"_DIST"), os.Getenv("AGENTEAM_AUTH_WEB_RUNTIME"), os.Getenv(prefix+"_EVIDENCE")
	if !filepath.IsAbs(dist) || !filepath.IsAbs(runtime) || len(runtime) > 45 || !filepath.IsAbs(evidence) {
		t.Fatal("owned Task Cancel paths required")
	}
	if info, err := os.Stat(filepath.Join(dist, "index.html")); err != nil || !info.Mode().IsRegular() {
		t.Fatal("Task Cancel production assets missing")
	}
	dir, err := os.MkdirTemp(runtime, "tcw-")
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
	owner := &agentConfigurationWeb{a: a, mode: "explicit-cancel-preserves-relations-and-completes-sprint", origin: "http://" + listener.Addr().String(), directory: dir, evidence: evidence, dist: dist, listener: listener, serve: make(chan error, 1), tails: map[string]*reviewWebTail{}}
	w := &taskCancelWeb{sprintStartWeb: &sprintStartWeb{agentConfigurationWeb: owner}, base: base, initial: tasks, current: tasks, blocker: blocker.Clone(), sprint: sprint.Clone(), project: project.Clone()}
	w.blockersBefore, err = w.blockerFacts(ctxFor(t))
	agentAppRequire(t, err)
	route := "/" + username + "/" + project.Project.NormalizedName + "/tasks/"
	raw, err := json.Marshal(map[string]any{"mode": owner.mode, "origin": owner.origin, "cookie": base.ownerBrowser.cookie, "project_id": base.project.ID, "tasks": tasks, "blocker": blocker, "sprint": sprint, "project": project.Project, "comment": taskCancelComment, "route": route + tasks[0].ID.String(), "sprint_route": route + "sprints/" + sprint.ID.String()})
	if err == nil {
		err = os.WriteFile(filepath.Join(dir, "task-cancel-material.json"), raw, 0600)
	}
	clear(raw)
	if err != nil {
		t.Fatal(err)
	}
	published = true
	return w
}
func (w *taskCancelWeb) observe(r *http.Request) error {
	if r.Method == http.MethodGet || r.Method == http.MethodHead {
		return nil
	}
	if r.Method != http.MethodPost || r.URL.RawQuery != "" {
		return errors.New("undeclared Task Cancel mutation")
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, (16<<10)+1))
	closed := r.Body.Close()
	if err != nil || closed != nil || len(raw) > 16<<10 {
		return errors.New("original cancellation request incomplete")
	}
	r.Body = io.NopCloser(bytes.NewReader(raw))
	key := f.IdempotencyKey(r.Header.Get("Idempotency-Key"))
	if key.Validate() != nil || len(r.Header.Values("Idempotency-Key")) != 1 {
		return errors.New("original cancellation key missing")
	}
	var wire struct {
		Expected f.Version       `json:"expected_version"`
		Request  json.RawMessage `json:"request"`
	}
	if agentWebDecode(raw, &wire) != nil {
		return errors.New("invalid cancellation envelope")
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	in := taskCancelIntent{key: key, index: -1, bodyDigest: sha256.Sum256(raw), keyDigest: sha256.Sum256([]byte(key))}
	if r.URL.Path == variableHTTPPath(w.base.project.ID, "/sprints/"+w.sprint.ID.String()+"/complete") {
		var empty map[string]json.RawMessage
		if len(w.writes) != 2 || w.receipts[0].Validate() != nil || w.receipts[1].Validate() != nil || wire.Expected != w.sprint.Version || json.Unmarshal(wire.Request, &empty) != nil || empty == nil || len(empty) != 0 || w.taskReads < 2 || w.timelineReads < 2 || w.blockerReads < 1 || w.boardReads < 1 {
			return errors.New("Sprint completed before cancellation and actual rereads")
		}
		if err = w.base.raw.QueryRow(r.Context(), `SELECT coalesce(jsonb_agg(to_jsonb(t) ORDER BY id),'[]'::jsonb)::text FROM agenteam_work.tasks t WHERE project_id=$1`, w.base.project.ID.String()).Scan(&w.beforeCompletionTasks); err != nil {
			return err
		}
		in.kind = "complete"
	} else {
		for i, t := range w.initial {
			if r.URL.Path == variableHTTPPath(w.base.project.ID, "/tasks/"+t.ID.String()+"/transfer") {
				in.index = i
			}
		}
		if in.index < 0 || len(w.writes) != in.index || wire.Expected != w.initial[in.index].Version {
			return errors.New("cancellation lost original Task/version/order")
		}
		var request struct {
			Target  string  `json:"target_state"`
			Comment *string `json:"comment,omitempty"`
		}
		if agentWebDecode(wire.Request, &request) != nil || request.Target != "cancelled" || in.index == 0 && (request.Comment == nil || *request.Comment != taskCancelComment) || in.index == 1 && request.Comment != nil {
			return errors.New("cancellation added assignment/blocker operations or changed optional comment")
		}
		in.request, err = wc.DecodeTaskTransfer(wire.Request)
		if err != nil {
			return err
		}
		in.kind = fmt.Sprintf("cancel-%d", in.index)
	}
	for _, old := range w.keys {
		if old == string(key) {
			return errors.New("distinct cancellation command reused key")
		}
	}
	w.writes = append(w.writes, in.kind)
	w.keys = append(w.keys, string(key))
	*r = *r.WithContext(context.WithValue(r.Context(), taskCancelIntentKey{}, in))
	return nil
}
func (w *taskCancelWeb) response(r *http.Response) error {
	raw, err := w.completedResponse(r)
	if err != nil {
		return err
	}
	if r.StatusCode != 200 {
		return fmt.Errorf("original Task Cancel HTTP returned %d", r.StatusCode)
	}
	in, write := r.Request.Context().Value(taskCancelIntentKey{}).(taskCancelIntent)
	w.mu.Lock()
	defer w.mu.Unlock()
	if write {
		if in.kind == "complete" {
			if json.Unmarshal(raw, &w.completion) != nil {
				return errors.New("Sprint completion receipt invalid")
			}
			if err = sprintCompleteCommitted(r.Request.Context(), w.base, w.sprint, w.project, in.key, w.completion); err != nil {
				return err
			}
			w.afterComplete, err = sprintCompleteSnapshotContext(r.Request.Context(), w.base)
			if err != nil {
				return err
			}
			var after struct {
				Tasks json.RawMessage `json:"tasks"`
			}
			var beforeTasks, afterTasks bytes.Buffer
			if json.Unmarshal([]byte(w.afterComplete), &after) != nil || json.Compact(&beforeTasks, w.beforeCompletionTasks) != nil || json.Compact(&afterTasks, after.Tasks) != nil || !bytes.Equal(beforeTasks.Bytes(), afterTasks.Bytes()) {
				return errors.New("Sprint completion changed original terminal Task rows")
			}
		} else {
			in.requestID, err = f.ParseID[f.Request](r.Header.Get("X-Request-ID"))
			if err != nil {
				return err
			}
			receipt, e := wc.DecodeTaskTransitionMutation(raw)
			if e != nil {
				return e
			}
			if err = w.committed(r.Request.Context(), in, receipt); err != nil {
				return err
			}
			w.receipts[in.index], w.current[in.index] = receipt.Clone(), receipt.Task.Clone()
		}
	} else if err = w.read(r.Request, raw); err != nil {
		return err
	}
	record := map[string]any{"request_id": r.Header.Get("X-Request-ID"), "method": r.Request.Method, "url_sha256": fmt.Sprintf("%x", sha256.Sum256([]byte(r.Request.URL.RequestURI()))), "status": 200, "content_length": len(raw), "body_sha256": fmt.Sprintf("%x", sha256.Sum256(raw)), "original_eof": true, "original_closed": true, "handler_returned": true}
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
	if err = os.WriteFile(filepath.Join(w.evidence, "task-cancel-response-"+r.Header.Get("X-Request-ID")+".json"), encoded, 0600); err != nil {
		return err
	}
	r.Body = io.NopCloser(bytes.NewReader(raw))
	return nil
}
func (w *taskCancelWeb) committed(ctx context.Context, in taskCancelIntent, result wc.TaskTransitionMutation) error {
	before := w.initial[in.index]
	expected := before.Clone()
	expected.State = wc.TaskStateCancelled
	expected.Version++
	expected.UpdatedAt = result.Task.UpdatedAt
	expected.ManualRank = result.Task.ManualRank
	history := 1
	if in.request.Comment != nil {
		history++
	}
	if result.Validate() != nil || !reflect.DeepEqual(result.Task, expected) || len(result.TaskEventIDs) != history || len(result.EventIDs) != 1 {
		return errors.New("Cancel changed original Task/assignee/metadata or history cardinality")
	}
	semantic, err := wc.TaskTransferDigest(w.base.ownerBrowser.actor, f.CommandMeta{RequestID: in.requestID, IdempotencyKey: in.key, ExpectedVersion: &before.Version}, before.ProjectID, before.ID, in.request)
	if err != nil {
		return err
	}
	var raw, requestRaw, payload []byte
	var exact bool
	var command string
	err = w.base.raw.QueryRow(ctx, `SELECT c.receipt,c.request->'request',e.payload,c.id::text,
 c.actor_user_id=$4::text::uuid AND c.expected_version=$5 AND c.semantic_digest=$6 AND c.state='completed'
 AND t.state='cancelled' AND t.version=$7 AND t.assignee_agent_id IS NOT DISTINCT FROM $8::text::uuid AND t.sprint_id=$9::uuid AND t.milestone_id=$10::uuid AND t.updated_at=$11
 AND c.xmin=t.xmin AND c.xmin=e.xmin AND c.committed_at=$11
 AND e.producer='work' AND e.event_type='work.task_transitioned' AND e.aggregate_type=$13 AND e.schema_version=1 AND e.project_id=c.project_id AND e.aggregate_id=t.id AND e.aggregate_version=t.version
 AND (SELECT count(*) FROM agenteam_work.task_events h WHERE h.transition_operation_id=c.id)=$12
 AND NOT EXISTS(SELECT 1 FROM agenteam_work.task_events h WHERE h.transition_operation_id=c.id AND (h.xmin<>c.xmin OR h.task_version<>t.version OR h.created_at<>t.updated_at OR h.project_id<>c.project_id OR h.task_id<>c.task_id OR h.correlation_id<>c.id OR h.actor<>jsonb_build_object('type','human','user_id',$4::text,'source','task_domain')))
 AND NOT EXISTS(SELECT 1 FROM agenteam_execution.executions WHERE project_id=$1::text)
 AND NOT EXISTS(SELECT 1 FROM agenteam_scheduler.dispatches WHERE project_id=$1::text)
 FROM agenteam_work.task_transition_commands c JOIN agenteam_work.tasks t ON t.id=c.task_id JOIN agenteam_outbox.events e ON e.id=c.event_id
 WHERE c.project_id=$1::text::uuid AND c.task_id=$2::uuid AND c.idempotency_key=$3`, before.ProjectID.String(), before.ID.String(), string(in.key), w.base.ownerBrowser.actor.Details().UserID, int64(before.Version), semantic.String(), int64(result.Task.Version), taskCancelAgent(before), before.SprintID.String(), before.MilestoneID.String(), result.Task.UpdatedAt.Time(), history, string(wc.TaskAggregate)).Scan(&raw, &requestRaw, &payload, &command, &exact)
	if err != nil {
		return err
	}
	stored, err := wc.DecodeTaskTransitionMutation(raw)
	if err != nil {
		return err
	}
	storedRequest, err := wc.DecodeTaskTransfer(requestRaw)
	if err != nil {
		return err
	}
	event, err := wc.DecodeTaskTransitioned(payload)
	if err != nil {
		return err
	}
	if !exact || !reflect.DeepEqual(stored, result) || !reflect.DeepEqual(storedRequest, in.request) || event.CommandID.String() != command || event.Actor.UserID.String() != w.base.ownerBrowser.actor.Details().UserID || event.MilestoneID != before.MilestoneID || event.SprintID != before.SprintID || event.FromState != before.State || event.ToState != wc.TaskStateCancelled || event.AssigneeChange != nil || !reflect.DeepEqual(event.TaskEventIDs, result.TaskEventIDs) {
		return errors.New("Cancel lost original full intent/receipt/event or same transaction")
	}
	for i, id := range result.TaskEventIDs {
		h := wc.TaskTransitionEvent{ID: id, ProjectID: before.ProjectID, TaskID: before.ID, TaskVersion: result.Task.Version, Actor: event.Actor, OperationID: event.CommandID, CorrelationID: event.CommandID, CreatedAt: result.Task.UpdatedAt}
		if i == 0 {
			h.Type = wc.TaskTransitionStateChanged
			h.Payload.StateChanged = &wc.TaskStateChangedPayload{FromState: before.State, ToState: wc.TaskStateCancelled}
		} else {
			h.Type = wc.TaskTransitionComment
			h.Payload.Comment = &wc.TaskCommentPayload{Body: *in.request.Comment}
		}
		if h.Validate() != nil {
			return errors.New("expected cancellation history invalid")
		}
		w.history[in.index] = append(w.history[in.index], h)
	}
	blockers, err := w.blockerFacts(ctx)
	if err != nil {
		return err
	}
	if blockers != w.blockersBefore {
		return errors.New("Cancel resolved or changed original blockers")
	}
	return nil
}
func taskCancelAgent(t wc.Task) any {
	if t.AssigneeAgentID == nil {
		return nil
	}
	return t.AssigneeAgentID.String()
}
func (w *taskCancelWeb) blockerFacts(ctx context.Context) (string, error) {
	var raw string
	err := w.base.raw.QueryRow(ctx, `SELECT coalesce(jsonb_agg(to_jsonb(b) ORDER BY id),'[]'::jsonb)::text FROM agenteam_work.task_blockers b WHERE project_id=$1`, w.base.project.ID.String()).Scan(&raw)
	return raw, err
}
func (w *taskCancelWeb) read(r *http.Request, raw []byte) error {
	base := variableHTTPPath(w.base.project.ID, "")
	for i, task := range w.current {
		path := base + "/tasks/" + task.ID.String()
		if r.URL.Path == path {
			var got wc.Task
			if json.Unmarshal(raw, &got) != nil || !reflect.DeepEqual(got, task) {
				return errors.New("Task reread differs from original cancellation")
			}
			if w.receipts[i].Validate() == nil {
				w.taskReads++
			}
			return nil
		}
		if r.URL.Path == path+"/events" && w.receipts[i].Validate() == nil {
			var page struct {
				Items []json.RawMessage `json:"items"`
			}
			if json.Unmarshal(raw, &page) != nil {
				return errors.New("Timeline original page invalid")
			}
			for _, want := range w.history[i] {
				found := 0
				for _, item := range page.Items {
					var identity struct {
						ID wc.TaskEventID `json:"id"`
					}
					if json.Unmarshal(item, &identity) != nil {
						return errors.New("Timeline identity invalid")
					}
					if identity.ID == want.ID {
						var got wc.TaskTransitionEvent
						if json.Unmarshal(item, &got) != nil || !reflect.DeepEqual(got, want) {
							return errors.New("Timeline cancellation full history differs")
						}
						found++
					}
				}
				if found != 1 {
					return errors.New("Timeline omitted or duplicated cancellation history")
				}
			}
			w.timelineReads++
			return nil
		}
		if r.URL.Path == path+"/blockers" && i == 1 && w.receipts[1].Validate() == nil {
			var page f.Page[wc.TaskBlocker]
			if json.Unmarshal(raw, &page) != nil || len(page.Items) != 1 || !reflect.DeepEqual(page.Items[0], w.blocker) {
				return errors.New("Blocker reread differs after cancellation")
			}
			w.blockerReads++
			return nil
		}
	}
	if r.URL.Path == base+"/tasks" && r.URL.Query().Get("state") == "cancelled" && w.receipts[1].Validate() == nil {
		var page struct {
			Items []struct {
				ID      wc.TaskID    `json:"id"`
				State   wc.TaskState `json:"state"`
				Version f.Version    `json:"version"`
			} `json:"items"`
		}
		if json.Unmarshal(raw, &page) != nil || len(page.Items) != 2 {
			return errors.New("cancelled column lost members")
		}
		seen := map[wc.TaskID]bool{}
		for _, item := range page.Items {
			valid := false
			for _, task := range w.current {
				if item.ID == task.ID && item.State == task.State && item.Version == task.Version {
					valid = true
				}
			}
			if !valid || seen[item.ID] {
				return errors.New("cancelled column postimage changed")
			}
			seen[item.ID] = true
		}
		w.boardReads++
	}
	if w.completion.Validate() == nil {
		if r.URL.Path == base {
			var got pc.ProjectRef
			if json.Unmarshal(raw, &got) != nil || !reflect.DeepEqual(got, w.completion.Project) {
				return errors.New("completion Project reread differs")
			}
			w.projectReads++
		}
		if r.URL.Path == base+"/sprints/"+w.sprint.ID.String() {
			var got wc.Sprint
			if json.Unmarshal(raw, &got) != nil || !reflect.DeepEqual(got, w.completion.Sprint) {
				return errors.New("completion Sprint reread differs")
			}
			w.sprintReads++
		}
	}
	return nil
}
func (w *taskCancelWeb) browser(t *testing.T, parent context.Context) {
	t.Helper()
	root, err := filepath.Abs("../account-captcha-web")
	agentAppRequire(t, err)
	ctx, cancel := context.WithTimeout(parent, 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "node", filepath.Join(root, "node_modules/@playwright/test/cli.js"), "test", "--config", filepath.Join(root, "task-cancel.config.js"))
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
	cmd.Env = append(cmd.Env, "TMPDIR="+w.directory, "PLAYWRIGHT_NO_COPY_PROMPT=1", "AGENTEAM_AUTH_WEB_ORIGIN="+w.origin, "AGENTEAM_AUTH_WEB_PRIVATE="+w.directory, "AGENTEAM_AUTH_WEB_CHROMIUM=/usr/bin/chromium", "AGENTEAM_TASK_CANCEL_WEB_DIST="+w.dist, "AGENTEAM_TASK_CANCEL_WEB_EVIDENCE="+w.evidence)
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	if err = cmd.Start(); err != nil {
		t.Fatal("Task Cancel Node did not start")
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
	t.Logf("Task Cancel Node actual_wait pid=%d success=%t", cmd.Process.Pid, err == nil)
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
		t.Fatal("original Task Cancel browser failed")
	}
}
func (w *taskCancelWeb) verify(t *testing.T, ctx context.Context) {
	t.Helper()
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.failure != nil || !reflect.DeepEqual(w.writes, []string{"cancel-0", "cancel-1", "complete"}) || w.completion.Validate() != nil || w.projectReads < 1 || w.sprintReads < 1 {
		t.Fatal("Task Cancel original browser chain incomplete", w.failure)
	}
	for _, tail := range w.tails {
		select {
		case <-tail.done:
		default:
			t.Fatal("Task Cancel original handler not joined")
		}
		if !tail.returned {
			t.Fatal("Task Cancel original handler did not return")
		}
	}
	current, err := sprintCompleteSnapshotContext(ctx, w.base)
	agentAppRequire(t, err)
	blockers, err := w.blockerFacts(ctx)
	agentAppRequire(t, err)
	if current != w.afterComplete || blockers != w.blockersBefore {
		t.Fatal("final reads changed committed business or blockers")
	}
	raw, err := os.ReadFile(filepath.Join(w.directory, "task-cancel-result.json"))
	agentAppRequire(t, err)
	var result struct {
		Completed bool `json:"completed"`
		Writes    int  `json:"writes"`
	}
	if json.Unmarshal(raw, &result) != nil || !result.Completed || result.Writes != 3 {
		t.Fatal("Task Cancel original browser consumers not retired")
	}
	t.Log("Task Cancel defaultApp runtime disabled; explicit cancel2/SprintComplete1; original assignee and unresolved blocker preserved; no Model/Execution cancellation; all original tails joined")
}
