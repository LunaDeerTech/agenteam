//go:build integration && linux

package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"testing"
	"time"

	ac "github.com/LunaDeerTech/agenteam/internal/central/artifact/contract"
	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	wc "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
	netfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/outbound"
)

const m2ArtifactFinal = "已发布结果 Artifact，等待人工审核。"

// Only the Model peer is scripted. Artifact publication, Object bytes, Tool
// authority, Scheduler/Execution and Owner HTTP transitions are the real App.
func TestM2AppArtifactHumanReview(t *testing.T) {
	for _, returned := range []bool{false, true} {
		name := "accept"
		if returned {
			name = "return-same-task-and-accept"
		}
		t.Run(name, func(t *testing.T) {
			w, _ := newM1WorkflowWithOptions(t, nil, true)
			w.seed(t)
			w.startRunner(t)
			var raw string
			workAppCheck(t, "m2-artifact-core-registration", w.app.admin.QueryRow(w.app.ctx, `SELECT tool_id FROM agenteam_tool.identities WHERE stable_key='builtin:create-artifact'`).Scan(&raw))
			tool, err := f.ParseID[id.Tool](raw)
			workAppCheck(t, "m2-artifact-core-id", err)
			w.tools["builtin:create-artifact"] = tool
			firstContent := "{\"count\":4,\"sum\":20,\"mean\":5}\n"
			w.responseSequence = func(t *testing.T) []netfixture.WireScenario {
				return m2ArtifactResponses(t, w, "m2-result.json", firstContent)
			}
			w.createModelAndTask(t, "https://fixture.test:8443/m2-artifact-not-yet-scheduled")
			w.app.store.releaseDirectory()
			m2AwaitTaskResult(t, w, 1)
			firstExecution := w.execution
			m2TaskScheduler(t, w, false)
			first := m2ReadWorkerArtifact(t, w, firstExecution, "m2-result.json", firstContent)
			firstScenario := w.scenario
			m2OwnerTaskTransfer(t, w, wc.TaskStateInReview, &w.reviewer, "已读取结果："+artifactOwnerHTTPReadPath(first))
			if returned {
				revised := "{\"count\":4,\"sum\":20,\"mean\":5,\"verified\":true}\n"
				w.scenario = m2SetWorkerResponses(t, w, m2ArtifactResponses(t, w, "m2-revised.json", revised))
				m2OwnerTaskTransfer(t, w, wc.TaskStateTodo, &w.app.agent, "补充 verified=true 的校验结果并重新发布 Artifact。")
				m2TaskScheduler(t, w, true)
				m2AwaitTaskResult(t, w, 2)
				if w.execution == firstExecution {
					t.Fatal("returned Task reused the completed Execution")
				}
				m2TaskScheduler(t, w, false)
				latest := m2ReadWorkerArtifact(t, w, w.execution, "m2-revised.json", revised)
				if latest.ArtifactID == first.ArtifactID {
					t.Fatal("revision overwrote original Artifact")
				}
				// The old immutable Artifact remains readable after the new Execution.
				m2ReadArtifactContent(t, w, first, firstContent)
				m2OwnerTaskTransfer(t, w, wc.TaskStateInReview, &w.reviewer, "已读取修订结果："+artifactOwnerHTTPReadPath(latest))
			}
			m2OwnerTaskTransfer(t, w, wc.TaskStateDone, nil, "已读取并核验真实 Artifact，接受本次结果。")
			m2AssertHumanReviewHistory(t, w, returned)
			if returned {
				w.joinedPeer(t, firstScenario, 2, 2)
			}
			m2StopApproval(t, w, 2)
			t.Logf("M2_ARTIFACT_HUMAN_REVIEW returned=%t actual_builtin_publication=true owner_content_read=true same_task=true scheduler_new_execution=%t task_done_by_owner=true external_model=false", returned, returned)
		})
	}
}

func m2ArtifactResponses(t *testing.T, w *m1Workflow, name, content string) []netfixture.WireScenario {
	t.Helper()
	call := m1FailureResponse(t, w, "builtin:create-artifact", map[string]any{"name": name, "media_type": "application/json", "content": content, "description": "Actual worker result for Human review"})
	final := map[string]any{"id": "owned-m2-artifact-final", "object": "chat.completion", "created": 1, "model": "m1-statistics", "choices": []any{map[string]any{"index": 0, "message": map[string]any{"role": "assistant", "content": m2ArtifactFinal}, "finish_reason": "stop"}}, "usage": map[string]any{"prompt_tokens": 10, "completion_tokens": 10, "total_tokens": 20}}
	return []netfixture.WireScenario{call, {Status: http.StatusOK, Headers: map[string]string{"Content-Type": "application/json"}, Chunks: [][]byte{workAppJSON(t, final)}}}
}

func m2AwaitTaskResult(t *testing.T, w *m1Workflow, expected int) {
	t.Helper()
	if w.m2Live != nil {
		w.m2AwaitLiveTaskResult(t, expected)
		return
	}
	v := w.app
	ctx, cancel := context.WithTimeout(v.ctx, 40*time.Second)
	defer cancel()
	tick := time.NewTicker(25 * time.Millisecond)
	defer tick.Stop()
	var execution string
	for {
		var count, succeeded, terminal int
		workAppCheck(t, "m2-task-executions", v.admin.QueryRow(ctx, `SELECT count(*),count(*) FILTER(WHERE status='succeeded'),count(*) FILTER(WHERE status IN('succeeded','failed','cancelled')),COALESCE(max(id),'') FROM agenteam_execution.executions WHERE project_id=$1 AND launch_request->'trigger'->>'task_id'=$2`, v.a.String(), w.task.ID.String()).Scan(&count, &succeeded, &terminal, &execution))
		if count > expected || terminal > succeeded {
			w.diagnoseResult(t, count, execution, "terminal")
			t.Fatal("Task duplicated or failed its original Execution")
		}
		if count == expected && succeeded == expected {
			break
		}
		select {
		case <-ctx.Done():
			w.diagnoseResult(t, count, execution, "pending")
			t.Fatal("Task result did not arrive within original budget")
		case <-tick.C:
		}
	}
	var err error
	w.execution, err = f.ParseID[id.Execution](execution)
	workAppCheck(t, "m2-task-execution-id", err)
	path := taskExecutionReadPath(v.a, w.task.ID.String(), w.execution) + "/runtime-view"
	response := taskExecutionGet(t, v.workAppFixture, v.client, v.cookie, http.MethodGet, path, http.StatusOK)
	workAppCheck(t, "m2-task-runtime-view", json.Unmarshal(response.body, &w.view))
	if w.view.ValidateProjection() != nil || w.view.Execution.ID != w.execution || w.view.Execution.Status != ec.Succeeded || w.view.Snapshot == nil || w.view.Snapshot.Schema != ec.DurableFinalTextV2 || len(w.view.Snapshot.Items) != 1 || w.view.Snapshot.Items[0].Data.Content != m2ArtifactFinal {
		t.Fatal("original final projection differs from published workflow")
	}
	before := w.task.Version
	m2TaskRequest(t, w, http.MethodGet, "/tasks/"+w.task.ID.String(), nil, "", &w.task)
	if w.task.State != wc.TaskStateInProgress || w.task.Version != before+1 || w.task.AssigneeAgentID == nil || *w.task.AssigneeAgentID != v.agent {
		t.Fatal("Execution success completed or reassigned Task without review")
	}
}

func m2TaskRequest(t *testing.T, w *m1Workflow, method, path string, body any, key string, out any) {
	t.Helper()
	var raw []byte
	if body != nil {
		raw = workAppJSON(t, body)
	}
	response, err := w.app.request(w.app.ctx, method, "/api/v1/projects/"+w.app.a.String()+path, raw, key)
	workAppCheck(t, "m2-owner-http", err)
	if response.status != http.StatusOK {
		var problem struct {
			Code string `json:"code"`
		}
		_ = json.Unmarshal(response.body, &problem)
		t.Fatalf("M2 Owner HTTP method=%s status=%d code=%s", method, response.status, problem.Code)
	}
	if out != nil {
		workAppCheck(t, "m2-owner-http-json", json.Unmarshal(response.body, out))
	}
}
func m2TaskScheduler(t *testing.T, w *m1Workflow, enabled bool) {
	t.Helper()
	v := w.app
	before, err := v.projects.service.GetSchedulerConfig(v.ctx, v.actor, v.a)
	workAppCheck(t, "m2-owner-scheduler-read", err)
	config := before.Config
	config.Enabled = enabled
	_, err = v.projects.service.UpdateProject(v.ctx, v.actor, executionSchedulerAppMeta(t, &before.Project.Version), v.a, pc.UpdateProjectRequest{Scheduler: &config})
	workAppCheck(t, "m2-owner-scheduler-update", err)
}
func m2OwnerTaskTransfer(t *testing.T, w *m1Workflow, target wc.TaskState, agent *id.AgentID, comment string) {
	t.Helper()
	before := w.task
	request := wc.TaskTransfer{TargetState: target, AssigneeAgentID: agent, Comment: &comment, AddBlockers: []wc.TaskBlockerCreate{}, ResolveBlockerIDs: []wc.TaskBlockerID{}}
	body := map[string]any{"expected_version": strconv.FormatInt(int64(before.Version), 10), "request": request}
	key := guardID[struct{}](t).String()
	var result wc.TaskTransitionMutation
	path := "/tasks/" + before.ID.String() + "/transfer"
	m2TaskRequest(t, w, http.MethodPost, path, body, key, &result)
	if result.Validate() != nil || result.Task.ID != before.ID || result.Task.Version != before.Version+1 || result.Task.State != target {
		t.Fatal("Owner transition receipt differs from original intent")
	}
	var replay wc.TaskTransitionMutation
	m2TaskRequest(t, w, http.MethodPost, path, body, key, &replay)
	if string(workAppJSON(t, replay)) != string(workAppJSON(t, result)) {
		t.Fatal("same-key Owner review replay changed receipt")
	}
	w.task = result.Task
}

type m2ArtifactItem struct {
	Reference ac.ArtifactRef `json:"artifact_ref"`
	Name      string         `json:"name"`
	Execution string         `json:"source_execution_id"`
	Operation string         `json:"source_operation_id"`
	Agent     string         `json:"created_by_agent_id"`
}

func m2ReadWorkerArtifact(t *testing.T, w *m1Workflow, execution id.ExecutionID, name, content string) ac.ArtifactRef {
	t.Helper()
	var page struct {
		Items []m2ArtifactItem `json:"items"`
	}
	m2TaskRequest(t, w, http.MethodGet, "/artifacts?"+url.Values{"execution_id": {execution.String()}, "name_query": {name}}.Encode(), nil, "", &page)
	if len(page.Items) != 1 {
		t.Fatal("worker did not publish exactly one readable Artifact")
	}
	item := page.Items[0]
	if item.Reference.Validate() != nil || item.Reference.ProjectID != w.app.a || item.Name != name || item.Execution != execution.String() || item.Agent != w.app.agent.String() || item.Operation == "" {
		t.Fatal("Artifact source differs from original worker operation")
	}
	m2ReadArtifactContent(t, w, item.Reference, content)
	var matches bool
	workAppCheck(t, "m2-artifact-original-operation", w.app.admin.QueryRow(w.app.ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_tool.operations o JOIN agenteam_tool.identities t ON t.tool_id=o.tool_id WHERE o.operation_id=$1 AND o.execution_id=$2 AND t.stable_key='builtin:create-artifact' AND o.phase='succeeded')`, item.Operation, execution.String()).Scan(&matches))
	if !matches {
		t.Fatal("Artifact lacks its actual completed Builtin operation")
	}
	return item.Reference
}
func m2ReadArtifactContent(t *testing.T, w *m1Workflow, ref ac.ArtifactRef, content string) {
	t.Helper()
	var read struct {
		Artifact   m2ArtifactItem `json:"artifact"`
		Projection string         `json:"projection"`
		Text       string         `json:"text"`
		Truncated  bool           `json:"truncated"`
	}
	path := "/artifacts/" + ref.ArtifactID.String() + "/files/" + ref.FileID.String() + "/content"
	m2TaskRequest(t, w, http.MethodGet, path, nil, "", &read)
	if read.Artifact.Reference != ref || read.Projection != "text" || read.Text != content || read.Truncated {
		t.Fatal("Owner did not read the actual stored Artifact bytes")
	}
}

func m2SetWorkerResponses(t *testing.T, w *m1Workflow, sequence []netfixture.WireScenario) string {
	t.Helper()
	scenario, err := w.network.Create(w.app.ctx, netfixture.ScenarioConfig{Mode: "openai_chat_wire", WireSequence: sequence})
	workAppCheck(t, "m2-revision-model-peer", err)
	var page struct {
		Items []struct {
			ID      mc.ProviderID              `json:"id"`
			Version f.Version                  `json:"version"`
			Input   map[string]json.RawMessage `json:"input"`
		} `json:"items"`
	}
	m2TaskRequest(t, w, http.MethodGet, "/model-providers", nil, "", &page)
	matched := false
	for _, provider := range page.Items {
		var name string
		workAppCheck(t, "m2-provider-name", json.Unmarshal(provider.Input["name"], &name))
		if name != "M1 owned Provider" {
			continue
		}
		if matched {
			t.Fatal("worker Provider name is ambiguous")
		}
		matched = true
		provider.Input["base_url"] = workAppJSON(t, "https://fixture.test:8443/case/"+scenario)
		m2TaskRequest(t, w, http.MethodPut, "/model-providers/"+provider.ID.String(), map[string]any{"expected_version": provider.Version, "input": provider.Input}, guardID[struct{}](t).String(), nil)
	}
	if !matched {
		t.Fatal("original worker Provider was not visible to Owner")
	}
	return scenario
}
func m2AssertHumanReviewHistory(t *testing.T, w *m1Workflow, returned bool) {
	t.Helper()
	expected := 1
	if returned {
		expected = 2
	}
	var tasks, executions, artifacts, reviewed, done, backToTodo, alien int
	workAppCheck(t, "m2-review-durable-facts", w.app.admin.QueryRow(w.app.ctx, `SELECT
 (SELECT count(*) FROM agenteam_work.tasks WHERE project_id=$1 AND id=$2 AND state='done'),
 (SELECT count(*) FROM agenteam_execution.executions WHERE project_id=$1::text AND launch_request->'trigger'->>'task_id'=$2::text AND status='succeeded' AND (NOT $4::boolean OR launch_request->>'purpose'='task/work')),
 (SELECT count(*) FROM agenteam_artifact.artifacts WHERE project_id=$1::text::uuid),
 (SELECT count(*) FROM agenteam_work.task_events WHERE project_id=$1 AND task_id=$2 AND type='state_changed' AND payload->>'to_state'='in_review'),
 (SELECT count(*) FROM agenteam_work.task_events WHERE project_id=$1 AND task_id=$2 AND type='state_changed' AND payload->>'to_state'='done'),
 (SELECT count(*) FROM agenteam_work.task_events WHERE project_id=$1 AND task_id=$2 AND type='state_changed' AND payload->>'from_state'='in_review' AND payload->>'to_state'='todo'),
 (SELECT count(*) FROM agenteam_work.task_events WHERE project_id=$1 AND task_id=$2 AND type='state_changed' AND payload->>'from_state' IN('in_progress','in_review') AND (actor->>'type'<>'human' OR actor->>'user_id'<>$3))`, w.app.a.String(), w.task.ID.String(), w.app.actor.Details().UserID, w.m2Live != nil).Scan(&tasks, &executions, &artifacts, &reviewed, &done, &backToTodo, &alien))
	wantReturn := 0
	if returned {
		wantReturn = 1
	}
	if tasks != 1 || executions != expected || artifacts != expected || reviewed != expected || done != 1 || backToTodo != wantReturn || alien != 0 {
		t.Fatal(fmt.Sprintf("review facts task=%d executions=%d artifacts=%d reviewed=%d done=%d returned=%d alien=%d", tasks, executions, artifacts, reviewed, done, backToTodo, alien))
	}
}
