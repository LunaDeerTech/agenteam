//go:build integration && linux

package app

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"reflect"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
	"unicode/utf8"

	ac "github.com/LunaDeerTech/agenteam/internal/central/artifact/contract"
	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/outbound"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
	workdomain "github.com/LunaDeerTech/agenteam/internal/central/work"
	wc "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
	rp "github.com/LunaDeerTech/agenteam/internal/runnerprotocol"
)

const (
	m2LiveOptIn         = "external-model-task-artifact-review"
	m2LiveInput         = "{\"values\":[3,7,11,15,19]}\n"
	m2LivePlan          = "Use only the supplied Runner mount. Read input.json with the read-file tool using its captured mount identity, relative path input.json, offset 0, length 4096 and utf8 encoding. Calculate the values array's count, sum and arithmetic mean yourself. Publish exactly one new application/json Artifact using the create-artifact tool, with a descriptive .json name and a JSON object containing the numeric count, sum and mean. On the first work execution publish those three fields only. If the captured recent Task history contains a Human return comment, follow that latest return comment and publish a new Artifact; never overwrite the original. Do not change Task state. Use only read-file and create-artifact; after their successful results, finish with a short factual text for the Human reviewer. Do not claim publication before the tool succeeds."
	m2LiveReturnComment = "请按本 Task 原始 input.json 重新计算并核验统计，在新的 JSON Artifact 中保留 count、sum、mean，并添加 verified=true。请发布新的 Artifact，保留旧成果；不修改 Task 状态，完成后等待人工再次读取和接受。"
)

type m2LiveTask struct {
	first   id.ExecutionID
	reviews []m2LiveReview
}

type m2LiveReview struct {
	execution id.ExecutionID
	operation string
}

// The model's responses and Artifact bytes are never scripted. The only
// supplied data are the real Task instructions, input file and Human comment.
func TestM2AppLiveArtifactBrowserReview(t *testing.T) {
	selection, optIn := os.Getenv("AGENTEAM_M2_CASE"), os.Getenv("AGENTEAM_M2_LIVE_OPT_IN")
	if selection != "live-task-browser" && optIn == "" {
		t.Skip("explicit M2 live case and opt-in required; no external configuration inspected")
	}
	if selection != "live-task-browser" || optIn != m2LiveOptIn {
		t.Fatal("M2 live requires its exact case and opt-in before external metadata access")
	}
	started := time.Now()
	configuration, err := m1LiveMetadata(os.Getenv("AGENTEAM_M1_CASE"), os.Getenv("AGENTEAM_M1_LIVE_OPT_IN"), os.Getenv("DEV_CHAT_BASE_URL"), os.Getenv("DEV_CHAT_MODEL"))
	workAppCheck(t, "m2-live-metadata", err)
	configuration.deadline = started.Add(m1LiveTotalBudget)
	resolver, err := outbound.SystemResolver()
	workAppCheck(t, "m2-live-system-resolver", err)
	preflight, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	err = configuration.preflight(preflight, resolver)
	cancel()
	workAppCheck(t, "m2-live-route-preflight", err)
	t.Logf("M2_LIVE_ROUTE explicit_origin=true trusted_proxy=%t tls_verification=true", configuration.proxyRoute)
	o := &m2BrowserObservation{paths: map[string]bool{}, tails: map[string]*artifactWebTail{}}
	// Forty eligible visits leave a Human viewing window while allowing the
	// second review through the same normal Scheduler within the total budget.
	// This is a visit count, not a fixed duration or a change to M1's 240 visits.
	w, listener := newM1WorkflowWithReviewWindow(t, nil, true, configuration, 40, o.bind)
	t.Log("M2_LIVE_SCHEDULER scheduler_enabled=true relaunch_skip_count=40 total_budget_seconds=300 project_sent_limit=12")
	defer func() {
		if t.Failed() {
			w.diagnoseLiveResult(t)
			w.m2DiagnoseLiveReviews(t)
		}
	}()
	w.m2Live = &m2LiveTask{}
	w.taskSetup = &m1WorkflowTaskSetup{Title: "读取真实输入并发布可人工审核的统计成果", Plan: m2LivePlan,
		WorkerInstructions: m2LivePlan, WorkerToolKeys: []string{tc.RunnerReadFileKey}, Input: []byte(m2LiveInput),
		ReviewerInstructions: "For task/review, review the submitted result rather than executing the worker plan. The latest Human submission comment in the captured Task history contains the exact artifact_ref JSON object. Call only read-artifact with that exact artifact_ref, offset 0 and limit 8192. Read the actual returned JSON content, then finish with one short factual acknowledgement describing the statistics and whether verified is present. Do not call any other tool, create artifacts, or transfer the Task. The Human alone returns or accepts the Task."}
	w.seed(t)
	w.startRunner(t)
	budget := w.startLiveBudget(t, configuration.deadline)
	w.createModelAndTask(t, configuration.BaseURL)
	w.app.store.releaseDirectory()
	m2AwaitTaskResult(t, w, 1)
	w.m2Live.first = w.execution
	artifact, content := w.m2LiveArtifact(t, false)
	m2OwnerTaskTransfer(t, w, wc.TaskStateInReview, &w.reviewer, m2LiveReviewComment(t, artifact))
	w.m2Browser(t, listener, o, "return-and-accept", artifact, content)
	m2AssertHumanReviewHistory(t, w, true)
	w.m2StopLive(t, budget)
	t.Log("M2_LIVE_ARTIFACT_BROWSER_REVIEW external_model=true captured_task_and_return_comment=true runner_input_read=true original_builtin_artifacts=2 same_task_work_executions=2 scheduler_enabled=true reviewer_consumed_original_artifacts=2 browser_return_and_accept=true immutable_original_read=true task_done_by_human=true autonomous_reviewer=false original_app_runner_wait=true")
}

// This is a business reference from the actual worker result, not an object
// storage location or a model response. Both reviewer and Human receive it.
func m2LiveReviewComment(t *testing.T, artifact m2ArtifactItem) string {
	t.Helper()
	return "本次提交成果的精确引用；请用 read-artifact 读取后确认，Task 由 Human 决定：" + string(workAppJSON(t, map[string]any{"artifact_ref": artifact.Reference}))
}

// Keep the normal Scheduler active and wait for the real reviewer to read and
// finish before exposing the Human return/accept action. No slot is cleared.
func (w *m1Workflow) m2AwaitLiveReview(t *testing.T, artifact m2ArtifactItem, content string) {
	t.Helper()
	ctx, cancel := context.WithDeadline(w.app.ctx, w.live.deadline)
	defer cancel()
	known := make([]string, len(w.m2Live.reviews))
	for n, previous := range w.m2Live.reviews {
		known[n] = previous.execution.String()
	}
	if len(known) > 1 {
		t.Fatal("M2 live attempted an extra review")
	}
	tick := time.NewTicker(50 * time.Millisecond)
	defer tick.Stop()
	var raw string
	for {
		var count, succeeded, invalid int
		err := w.app.admin.QueryRow(ctx, `SELECT count(*),count(*) FILTER(WHERE status='succeeded'),
 count(*) FILTER(WHERE agent_id<>$4 OR status IN('failed','cancelled')),
 COALESCE(min(id) FILTER(WHERE NOT(id=ANY($3::text[]))),'')
 FROM agenteam_execution.executions WHERE project_id=$1 AND launch_request->'trigger'->>'task_id'=$2 AND launch_request->>'purpose'='task/review'`, w.app.a.String(), w.task.ID.String(), known, w.reviewer.String()).Scan(&count, &succeeded, &invalid, &raw)
		workAppCheck(t, "m2-live-review-executions", err)
		if count > len(known)+1 || invalid != 0 {
			t.Fatal("M2 live reviewer duplicated, failed or changed its original execution")
		}
		if count == len(known)+1 && succeeded == count && raw != "" {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("M2 live review exceeded its original total deadline")
		case <-tick.C:
		}
	}
	reviewID, err := f.ParseID[id.Execution](raw)
	workAppCheck(t, "m2-live-review-execution-id", err)
	// Only local verification identity changes. Preserve the actual worker
	// execution and its deeply retained public view for subsequent Human reads.
	reviewer := *w
	reviewer.execution = reviewID
	rounds := reviewer.m2LiveExecutionRounds(t, "task/review", 2, m2LiveReviewComment(t, artifact))
	if len(rounds) != 2 {
		t.Fatal("M2 live review did not use exactly one read and one final model round")
	}
	reviewer.checkLiveCalls(t, rounds)
	operation := reviewer.m2LiveTools(t, rounds, artifact, content, true)
	var view ec.TaskRuntimeView
	path := taskExecutionReadPath(w.app.a, w.task.ID.String(), reviewID) + "/runtime-view"
	response := taskExecutionGet(t, w.app.workAppFixture, w.app.client, w.app.cookie, http.MethodGet, path, http.StatusOK)
	workAppCheck(t, "m2-live-review-runtime-view", json.Unmarshal(response.body, &view))
	clear(response.body)
	if view.ValidateProjection() != nil || view.Execution.ID != reviewID || view.Execution.TaskID != w.task.ID.String() || view.Execution.AgentID != w.reviewer || view.Execution.Status != ec.Succeeded || view.Availability != ec.RuntimeAvailable || view.Snapshot == nil || view.Snapshot.Schema != ec.DurableFinalTextV2 || len(view.Snapshot.Items) != 1 || !utf8.ValidString(view.Snapshot.Items[0].Data.Content) || strings.TrimSpace(view.Snapshot.Items[0].Data.Content) == "" {
		t.Fatal("M2 live reviewer lacks its actual completed final acknowledgement")
	}
	current, err := w.app.planning.taskReader.GetTask(ctx, w.app.actor, w.app.a, w.task.ID)
	workAppCheck(t, "m2-live-task-after-review-read", err)
	if !reflect.DeepEqual(current, w.task) {
		t.Fatal("M2 live reviewer changed the Task before Human review")
	}
	var launched, unretired int
	workAppCheck(t, "m2-live-review-dispatch-retirement", w.app.admin.QueryRow(ctx, `SELECT
 (SELECT count(*) FROM agenteam_scheduler.dispatches WHERE project_id=$1 AND task_id=$2 AND execution_id=$3 AND status='launched'),
 (SELECT count(*) FROM agenteam_tool.attempts WHERE execution_id=$3 AND NOT retired)`, w.app.a.String(), w.task.ID.String(), reviewID.String()).Scan(&launched, &unretired))
	if launched != 1 || unretired != 0 {
		t.Fatal("M2 live review lacks its unique Scheduler dispatch or retired Tool Attempt")
	}
	w.m2Live.reviews = append(w.m2Live.reviews, m2LiveReview{reviewID, operation})
}

// Keep only closed status/fault fields. Provider payloads, messages, endpoints
// and credentials must never be copied into the failure log.
func (w *m1Workflow) m2DiagnoseLiveReviews(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.WithoutCancel(w.app.ctx), 2*time.Second)
	defer cancel()
	var facts []byte
	err := w.app.admin.QueryRow(ctx, `SELECT COALESCE(jsonb_agg(jsonb_build_object(
 'execution_status',e.status,'dispatch',i.dispatch,'status',i.final_status,
 'category',i.error_data->>'category','code',i.error_data->>'code',
 'retryable',i.error_data->'retryable','partial_output',i.error_data->'partial_output',
 'attempt',i.attempt_index,'round_status',r.terminal_status,'round_reason',r.terminal_reason)),'[]'::jsonb)
 FROM agenteam_execution.executions e LEFT JOIN agenteam_model.invocations i ON i.execution_id=e.id::uuid
 LEFT JOIN agenteam_execution.rounds r ON r.execution_id=e.id AND r.call_id=i.call_id::text
 WHERE e.project_id=$1 AND e.launch_request->'trigger'->>'task_id'=$2 AND e.launch_request->>'purpose'='task/review'`, w.app.a.String(), w.task.ID.String()).Scan(&facts)
	if err != nil {
		t.Logf("M2 live review diagnostic unavailable error_type=%T budget_done=%t", err, ctx.Err() != nil)
		return
	}
	t.Logf("M2 live safe review diagnostic=%s", facts)
}

func (w *m1Workflow) m2AwaitLiveTaskResult(t *testing.T, expected int) {
	t.Helper()
	v := w.app
	ctx, cancel := context.WithDeadline(v.ctx, w.live.deadline)
	defer cancel()
	tick := time.NewTicker(50 * time.Millisecond)
	defer tick.Stop()
	var raw string
	for {
		var count, succeeded, bad int
		err := v.admin.QueryRow(ctx, `SELECT count(*),count(*) FILTER(WHERE status='succeeded'),count(*) FILTER(WHERE status IN('failed','cancelled')),
 COALESCE(min(id) FILTER(WHERE id<>$3),'') FROM agenteam_execution.executions
 WHERE project_id=$1 AND launch_request->'trigger'->>'task_id'=$2 AND launch_request->>'purpose'='task/work'`, v.a.String(), w.task.ID.String(), w.m2Live.first.String()).Scan(&count, &succeeded, &bad, &raw)
		if err != nil || count > expected || bad != 0 {
			w.diagnoseLiveResult(t)
			workAppCheck(t, "m2-live-work-executions", err)
			t.Fatal("M2 live Task duplicated or failed a work Execution")
		}
		if count == expected && succeeded == expected && raw != "" {
			break
		}
		select {
		case <-ctx.Done():
			w.diagnoseLiveResult(t)
			t.Fatal("M2 live Task exceeded its original total deadline")
		case <-tick.C:
		}
	}
	var err error
	w.execution, err = f.ParseID[id.Execution](raw)
	workAppCheck(t, "m2-live-work-execution-id", err)
	path := taskExecutionReadPath(v.a, w.task.ID.String(), w.execution) + "/runtime-view"
	response := taskExecutionGet(t, v.workAppFixture, v.client, v.cookie, http.MethodGet, path, http.StatusOK)
	workAppCheck(t, "m2-live-work-runtime-view", json.Unmarshal(response.body, &w.view))
	clear(response.body)
	if w.view.ValidateProjection() != nil || w.view.Execution.ID != w.execution || w.view.Execution.TaskID != w.task.ID.String() || w.view.Execution.AgentID != v.agent || w.view.Execution.Status != ec.Succeeded || w.view.Availability != ec.RuntimeAvailable || w.view.Snapshot == nil || w.view.Snapshot.Schema != ec.DurableFinalTextV2 || len(w.view.Snapshot.Items) != 1 || !utf8.ValidString(w.view.Snapshot.Items[0].Data.Content) || strings.TrimSpace(w.view.Snapshot.Items[0].Data.Content) == "" {
		t.Fatal("M2 live completed work lacks its original durable final text")
	}
	before := w.task.Version
	m2TaskRequest(t, w, http.MethodGet, "/tasks/"+w.task.ID.String(), nil, "", &w.task)
	if w.task.State != wc.TaskStateInProgress || w.task.Version != before+1 || w.task.AssigneeAgentID == nil || *w.task.AssigneeAgentID != v.agent {
		t.Fatal("M2 live work completed or reassigned the Task without Human review")
	}
	var launched int
	workAppCheck(t, "m2-live-original-dispatch", v.admin.QueryRow(ctx, `SELECT count(*) FROM agenteam_scheduler.dispatches WHERE project_id=$1 AND task_id=$2 AND execution_id=$3 AND status='launched'`, v.a.String(), w.task.ID.String(), w.execution.String()).Scan(&launched))
	if launched != 1 {
		t.Fatal("M2 live work lacks its unique original Scheduler dispatch")
	}
}

// The expected statistics come from independently parsing the file supplied to
// Runner, never from the model response or the Artifact under verification.
func m2LiveStatistics(raw []byte, revised bool) bool {
	var input struct {
		Values []float64 `json:"values"`
	}
	if json.Unmarshal([]byte(m2LiveInput), &input) != nil || len(input.Values) == 0 {
		return false
	}
	var actual map[string]json.RawMessage
	if json.Unmarshal(raw, &actual) != nil {
		return false
	}
	fields := 3
	if revised {
		fields++
	}
	if len(actual) != fields {
		return false
	}
	sum := float64(0)
	for _, value := range input.Values {
		sum += value
	}
	for key, want := range map[string]float64{"count": float64(len(input.Values)), "sum": sum, "mean": sum / float64(len(input.Values))} {
		var got float64
		if json.Unmarshal(actual[key], &got) != nil || got != want {
			return false
		}
	}
	if revised {
		var verified bool
		return json.Unmarshal(actual["verified"], &verified) == nil && verified
	}
	return true
}

func (w *m1Workflow) m2LiveArtifact(t *testing.T, revised bool) (m2ArtifactItem, string) {
	t.Helper()
	var page struct {
		Items []m2ArtifactItem `json:"items"`
	}
	m2TaskRequest(t, w, http.MethodGet, "/artifacts?"+url.Values{"execution_id": {w.execution.String()}}.Encode(), nil, "", &page)
	if len(page.Items) != 1 {
		t.Fatal("M2 live work did not publish exactly one actual Artifact")
	}
	item := page.Items[0]
	if item.Reference.Validate() != nil || item.Reference.ProjectID != w.app.a || item.Execution != w.execution.String() || item.Agent != w.app.agent.String() || item.Operation == "" || !strings.HasSuffix(item.Name, ".json") {
		t.Fatal("M2 live Artifact provenance differs from its original work Execution")
	}
	var read struct {
		Artifact   m2ArtifactItem `json:"artifact"`
		Projection string         `json:"projection"`
		Text       string         `json:"text"`
		Truncated  bool           `json:"truncated"`
	}
	m2TaskRequest(t, w, http.MethodGet, "/artifacts/"+item.Reference.ArtifactID.String()+"/files/"+item.Reference.FileID.String()+"/content", nil, "", &read)
	if !reflect.DeepEqual(read.Artifact, item) || read.Projection != "text" || read.Truncated || !m2LiveStatistics([]byte(read.Text), revised) {
		t.Fatal("M2 live actual Artifact does not match independently parsed input and Human revision")
	}
	rounds := w.m2LiveRounds(t, revised)
	w.checkLiveCalls(t, rounds)
	w.m2LiveTools(t, rounds, item, read.Text, false)
	return item, read.Text
}

func (w *m1Workflow) m2LiveRounds(t *testing.T, revised bool) []m1LiveRound {
	t.Helper()
	comment := ""
	if revised {
		comment = m2LiveReturnComment
	}
	return w.m2LiveExecutionRounds(t, "task/work", 3, comment)
}

func (w *m1Workflow) m2LiveExecutionRounds(t *testing.T, purpose string, minimum int, comment string) []m1LiveRound {
	t.Helper()
	rows, err := w.app.admin.Query(w.app.ctx, `SELECT id,call_id,content,response,tool_pending FROM agenteam_execution.rounds WHERE execution_id=$1 ORDER BY round_ordinal`, w.execution.String())
	workAppCheck(t, "m2-live-rounds", err)
	defer rows.Close()
	var rounds []m1LiveRound
	for rows.Next() {
		var item m1LiveRound
		var roundID, callID string
		var content, response []byte
		workAppCheck(t, "m2-live-round-row", rows.Scan(&roundID, &callID, &content, &response, &item.pending))
		decoded, err := ec.DecodeLoopRound(content)
		workAppCheck(t, "m2-live-round-decode", err)
		item.fields = decoded.Fields()
		workAppCheck(t, "m2-live-response-decode", json.Unmarshal(response, &item.response))
		if item.response.Validate() != nil || item.fields.Round.ExecutionID != w.execution || item.fields.Round.ID.String() != roundID || item.fields.Round.CallID.String() != callID || item.response.CallID != item.fields.Round.CallID || int(item.fields.Ordinal) != len(rounds)+1 {
			t.Fatal("M2 live round lost its original Execution and Model identity")
		}
		if len(rounds) > 0 {
			previous := rounds[len(rounds)-1]
			if !previous.pending || item.fields.Predecessor == nil || *item.fields.Predecessor != previous.fields.Round.ID || item.fields.Round.SnapshotID != previous.fields.Round.SnapshotID || !reflect.DeepEqual(item.fields.Tools, previous.fields.Tools) {
				t.Fatal("M2 live successor changed its original predecessor or captured tools")
			}
		}
		rounds = append(rounds, item)
	}
	workAppCheck(t, "m2-live-round-rows", rows.Err())
	rows.Close()
	if len(rounds) < minimum || rounds[len(rounds)-1].pending || string(rounds[len(rounds)-1].response.FinishReason) != "stop" {
		t.Fatal("M2 live Execution lacks its actual tool rounds and final text")
	}
	var capturedRaw []byte
	workAppCheck(t, "m2-live-captured-snapshot", w.app.admin.QueryRow(w.app.ctx, `SELECT content FROM agenteam_execution.snapshots WHERE execution_id=$1 AND id=$2`, w.execution.String(), rounds[0].fields.Round.SnapshotID.String()).Scan(&capturedRaw))
	snapshot, err := ec.DecodeDirectTextSnapshot(capturedRaw)
	workAppCheck(t, "m2-live-captured-snapshot-decode", err)
	captured, err := workdomain.DecodeTaskContext(snapshot.Fields().Context.Trigger())
	workAppCheck(t, "m2-live-captured-task-decode", err)
	if snapshot.Fields().Context.Input().Fields().Request.ExecutionID != w.execution || captured.Purpose() != purpose || snapshot.Fields().Context.Input().Fields().Request.Launch.Purpose != purpose || !reflect.DeepEqual(captured.Task(), w.task) {
		t.Fatal("M2 live original snapshot differs from the actual Task, purpose and version")
	}
	if comment != "" {
		encoded, err := json.Marshal(comment)
		workAppCheck(t, "m2-live-captured-comment-encoding", err)
		found := false
		version := captured.Task().Version
		if purpose == "task/work" {
			version--
		}
		for _, event := range captured.RecentTaskEvents() {
			var header struct {
				TaskVersion f.Version `json:"task_version"`
			}
			workAppCheck(t, "m2-live-captured-event-version", json.Unmarshal(event, &header))
			found = found || header.TaskVersion == version && bytes.Contains(event, encoded)
		}
		if !found {
			t.Fatal("M2 live Execution omitted the required Human comment and version from captured history")
		}
	}
	var first strings.Builder
	for _, message := range rounds[0].fields.Round.Messages {
		for _, part := range message.Parts {
			if part.Text != nil {
				first.WriteString(part.Text.Text)
				first.WriteByte('\n')
			}
		}
	}
	required := []string{w.task.ID.String(), w.task.Plan}
	if comment != "" {
		required = append(required, comment)
	}
	for _, value := range required {
		encoded, err := json.Marshal(value)
		workAppCheck(t, "m2-live-captured-input-encoding", err)
		if value == "" || (!strings.Contains(first.String(), value) && !strings.Contains(first.String(), string(encoded))) {
			t.Fatal("M2 live original model input omitted captured Task or required Human comment")
		}
	}
	return rounds
}

func (w *m1Workflow) m2LiveTools(t *testing.T, rounds []m1LiveRound, artifact m2ArtifactItem, content string, review bool) string {
	t.Helper()
	rows, err := w.app.admin.Query(w.app.ctx, `SELECT c.round_id,c.call_id,c.invocation_id,c.model_visible_name,c.operation_name,a.original,
 o.operation_id,t.message,COALESCE(b.body_data,rf.body_data),
 p.retired AND o.active_attempt_id=p.attempt_id AND o.phase=p.phase AND o.phase='succeeded' AND o.outcome='success'
 AND q.terminal_digest='sha256:'||encode(sha256(o.terminal_data),'hex')
 AND q.message_digest=t.digest AND q.message_digest='sha256:'||encode(sha256(t.message),'hex')
 AND q.result_digest='sha256:'||encode(sha256(convert_to(convert_from(t.message,'UTF8')::jsonb#>>'{parts,0,tool_result,parts,0,text,text}','UTF8')),'hex')
 FROM agenteam_execution.tool_calls c
 JOIN agenteam_execution.tool_call_arguments a ON (a.execution_id,a.round_id,a.id)=(c.execution_id,c.round_id,c.payload_id)
 JOIN agenteam_tool.operations o ON o.execution_id=c.execution_id AND o.invocation_id=c.invocation_id AND o.call_id=c.call_id AND o.logical_call_id=a.logical_call_id AND o.tool_id=c.tool_id AND o.spec_revision=c.spec_revision
 JOIN agenteam_execution.loop_tool_results q ON (q.execution_id,q.round_id,q.operation_id,q.invocation_id,q.call_id)=(c.execution_id,c.round_id,o.operation_id,c.invocation_id,c.call_id)
 JOIN agenteam_tool.attempts p ON (p.attempt_id,p.operation_id,p.execution_id,p.backend_request_id)=(q.attempt_id,q.operation_id,q.execution_id,q.request_id)
 JOIN agenteam_execution.transcript_entries t ON (t.execution_id,t.round_id,t.sequence)=(q.execution_id,q.round_id,q.transcript_sequence)
 LEFT JOIN agenteam_tool.builtin_results b ON (b.execution_id,b.operation_id,b.attempt_id,b.request_id)=(q.execution_id,q.operation_id,q.attempt_id,q.request_id)
 LEFT JOIN agenteam_tool.read_file_results rf ON (rf.execution_id,rf.operation_id,rf.attempt_id)=(q.execution_id,q.operation_id,q.attempt_id)
 WHERE c.execution_id=$1 ORDER BY q.transcript_sequence`, w.execution.String())
	workAppCheck(t, "m2-live-original-tool-pairs", err)
	type pair struct {
		round, call, invocation, name, operation, operationID string
		arguments, message, stored                            []byte
		valid                                                 bool
	}
	var pairs []pair
	for rows.Next() {
		var p pair
		workAppCheck(t, "m2-live-original-tool-pair", rows.Scan(&p.round, &p.call, &p.invocation, &p.name, &p.operation, &p.arguments, &p.operationID, &p.message, &p.stored, &p.valid))
		pairs = append(pairs, p)
	}
	workAppCheck(t, "m2-live-original-tool-pair-rows", rows.Err())
	rows.Close()
	var calls, operations, results int
	workAppCheck(t, "m2-live-tool-counts", w.app.admin.QueryRow(w.app.ctx, `SELECT
 (SELECT count(*) FROM agenteam_execution.tool_calls WHERE execution_id=$1),
 (SELECT count(*) FROM agenteam_tool.operations WHERE execution_id=$1),
 (SELECT count(*) FROM agenteam_execution.loop_tool_results WHERE execution_id=$1)`, w.execution.String()).Scan(&calls, &operations, &results))
	minimum := 2
	if review {
		minimum = 1
	}
	if len(pairs) < minimum || review && len(pairs) != 1 || calls != len(pairs) || operations != len(pairs) || results != len(pairs) || len(rounds) != len(pairs)+1 {
		t.Fatal("M2 live Tool calls, operations, results and successors are not one-to-one")
	}
	readInput, published := false, 0
	readOperation := ""
	for n, p := range pairs {
		round := rounds[n]
		if !p.valid || !round.pending || p.round != round.fields.Round.ID.String() || p.invocation != round.response.InvocationID.String() {
			t.Fatal("M2 live Tool result lost its completed original operation and retired Attempt")
		}
		var original *mc.ToolCallPart
		for _, part := range round.response.Message.Parts {
			if part.ToolCall != nil {
				if original != nil {
					t.Fatal("M2 live parallel Tool calls unsupported")
				}
				original = part.ToolCall
			}
		}
		if original == nil || original.ID != p.call || original.Name != p.name || !m1LiveJSONEqual(original.Arguments, p.arguments) {
			t.Fatal("M2 live Tool arguments differ from original external model output")
		}
		var message mc.Message
		workAppCheck(t, "m2-live-tool-message", json.Unmarshal(p.message, &message))
		if message.Validate() != nil || message.Role != "tool" || len(message.Parts) != 1 || message.Parts[0].ToolResult == nil || message.Parts[0].ToolResult.CallID != p.call || message.Parts[0].ToolResult.IsError || len(message.Parts[0].ToolResult.Parts) != 1 || message.Parts[0].ToolResult.Parts[0].Text == nil {
			t.Fatal("M2 live Tool message lost its exact successful result")
		}
		result := []byte(message.Parts[0].ToolResult.Parts[0].Text.Text)
		if !bytes.Equal(p.stored, result) {
			t.Fatal("M2 live consumed Tool result differs from its original persisted Runner or Builtin response")
		}
		history := append(append([]mc.Message(nil), round.fields.Round.Messages...), round.response.Message, message)
		if !reflect.DeepEqual(history, rounds[n+1].fields.Round.Messages) {
			t.Fatal("M2 live successor did not consume its original Model response and Tool result")
		}
		switch p.operation {
		case "read-file":
			if review {
				t.Fatal("M2 live reviewer used Runner instead of the submitted Artifact")
			}
			var args struct {
				Mount string `json:"mount_id"`
				Path  string `json:"path"`
			}
			workAppCheck(t, "m2-live-read-arguments", json.Unmarshal(p.arguments, &args))
			read, err := rp.DecodeReadFileResult(result)
			workAppCheck(t, "m2-live-read-result", err)
			data := []byte(read.Data)
			if read.Encoding == "base64" {
				data, err = base64.StdEncoding.Strict().DecodeString(read.Data)
				workAppCheck(t, "m2-live-read-bytes", err)
			}
			if args.Mount != w.mount.String() || args.Path != "input.json" || read.Path != args.Path || read.Offset != 0 || !read.EOF || !bytes.Equal(data, []byte(m2LiveInput)) || published != 0 {
				t.Fatal("M2 live input was not actually read from its captured Runner mount before publication")
			}
			readInput = true
		case "artifact.create":
			if review {
				t.Fatal("M2 live reviewer attempted to publish an Artifact")
			}
			var args struct {
				Name      string `json:"name"`
				MediaType string `json:"media_type"`
				Content   string `json:"content"`
			}
			workAppCheck(t, "m2-live-publication-arguments", json.Unmarshal(p.arguments, &args))
			if !readInput || p.operationID != artifact.Operation || args.Name != artifact.Name || args.MediaType != "application/json" || args.Content != content {
				t.Fatal("M2 live Artifact differs from its model-selected content and persisted Builtin result")
			}
			published++
		case "artifact.read":
			if !review {
				t.Fatal("M2 live worker used an unexpected Artifact read")
			}
			var args struct {
				Reference ac.ArtifactRef `json:"artifact_ref"`
				Offset    int64          `json:"offset"`
				Limit     int            `json:"limit"`
			}
			workAppCheck(t, "m2-live-review-read-arguments", json.Unmarshal(p.arguments, &args))
			var read struct {
				Artifact    m2ArtifactItem `json:"artifact"`
				Projection  string         `json:"projection"`
				Text        string         `json:"text"`
				Offset      string         `json:"offset"`
				NextOffset  string         `json:"next_offset"`
				Truncated   bool           `json:"truncated"`
				InvalidUTF8 bool           `json:"invalid_utf8"`
			}
			workAppCheck(t, "m2-live-review-read-result", json.Unmarshal(result, &read))
			if args.Reference != artifact.Reference || args.Offset != 0 || args.Limit != 8192 || p.operationID == artifact.Operation || !reflect.DeepEqual(read.Artifact, artifact) || read.Projection != "text" || read.Text != content || read.Offset != "0" || read.NextOffset != strconv.Itoa(len(content)) || read.Truncated || read.InvalidUTF8 {
				t.Fatal("M2 live reviewer did not consume the exact original worker Artifact, source operation and complete bytes")
			}
			readOperation = p.operationID
		default:
			t.Fatal("M2 live worker used a Tool outside input read and Artifact publication")
		}
	}
	if review {
		if readOperation == "" || published != 0 || readInput {
			t.Fatal("M2 live review lacks its single exact Artifact read")
		}
		t.Logf("M2_LIVE_REVIEW_TOOLS rounds=%d actual_original_artifact_read=true source_operation=true consumed_result=true human_decision=true", len(rounds))
		return readOperation
	}
	if !readInput || published != 1 {
		t.Fatal("M2 live work lacks its actual Runner input read and single Builtin Artifact publication")
	}
	t.Logf("M2_LIVE_WORK_TOOLS rounds=%d actual_input_read=true original_builtin_result=true source_operation=true", len(rounds))
	return ""
}

// With Scheduler enabled, the Human's original todo receipt can already be
// followed by a claim when Go observes it. Read that exact committed receipt,
// never synthesize todo or clear the active reviewer to avoid the competition.
func (w *m1Workflow) m2AwaitLiveBrowserReturn(t *testing.T, nodeDone <-chan struct{}) {
	t.Helper()
	before := w.task
	ctx, cancel := context.WithDeadline(w.app.ctx, w.live.deadline)
	defer cancel()
	tick := time.NewTicker(25 * time.Millisecond)
	defer tick.Stop()
	for {
		var count int
		var raw []byte
		workAppCheck(t, "m2-live-browser-return-receipt", w.app.admin.QueryRow(ctx, `SELECT count(*),COALESCE(jsonb_agg(receipt)->0,'null'::jsonb) FROM agenteam_work.task_transition_commands
 WHERE project_id=$1 AND task_id=$2 AND actor_user_id=$3 AND actor_agent_id IS NULL AND actor_execution_id IS NULL
 AND expected_version=$4 AND state='completed' AND request#>>'{request,target_state}'='todo' AND request#>>'{request,comment}'=$5`, w.app.a.String(), before.ID.String(), w.app.actor.Details().UserID, int64(before.Version), m2LiveReturnComment).Scan(&count, &raw))
		if count > 1 {
			t.Fatal("M2 live browser duplicated its Human return command")
		}
		if count == 1 {
			receipt, err := wc.DecodeTaskTransitionMutation(raw)
			workAppCheck(t, "m2-live-browser-return-original-receipt", err)
			if receipt.Task.ID != before.ID || receipt.Task.Version != before.Version+1 || receipt.Task.State != wc.TaskStateTodo || receipt.Task.AssigneeAgentID == nil || *receipt.Task.AssigneeAgentID != w.app.agent {
				t.Fatal("M2 live browser return receipt changed Task identity, version or worker")
			}
			w.task = receipt.Task
			return
		}
		select {
		case <-nodeDone:
			t.Fatal("M2 live browser exited before its original Human return")
		case <-ctx.Done():
			t.Fatal("M2 live Human return exceeded its original total deadline")
		case <-tick.C:
		}
	}
}

func (w *m1Workflow) m2StopLive(t *testing.T, budget *m1LiveBudget) {
	t.Helper()
	v := w.app
	v.runtime.StopAdmission()
	ctx, cancel := context.WithTimeout(context.Background(), m1LiveJoinBudget)
	err := v.runtime.Drain(ctx)
	cancel()
	workAppCheck(t, "m2-live-original-runtime-drain", err)
	budget.finish(t)
	w.m2LiveFinalFacts(t)
	w.stopRunner(t)
	v.requestStop()
	usageAppWait(t, v.directory.returned, "m2-live-manager-original-return")
	usageAppWait(t, v.root.done, "m2-live-original-app-return")
	workAppCheck(t, "m2-live-original-app-exit", v.root.err)
	if !v.runtime.Joined() || !v.runtime.executionCalls.Joined() || !v.runtime.schedulerCalls.Joined() || !v.planning.Joined() || !v.assembly.Joined() || !v.root.owned.producersJoined() {
		t.Fatal("M2 live App returned before all original owners joined")
	}
	w.m2AwaitLiveRetirement(t)
}

// Called only after the original App and every owner have actually joined.
// Local socket Close is complete; PostgreSQL may still be removing the remote
// backend from pg_stat_activity. Observe the same strict final facts without
// changing any product state or terminating a connection.
func (w *m1Workflow) m2AwaitLiveRetirement(t *testing.T) {
	t.Helper()
	v := w.app
	workAppCheck(t, "m2-live-original-guard-release", syscall.Flock(int(v.claim.Fd()), syscall.LOCK_EX|syscall.LOCK_NB))
	workAppCheck(t, "m2-live-guard-observer-unlock", syscall.Flock(int(v.claim.Fd()), syscall.LOCK_UN))
	ctx, cancel := context.WithTimeout(v.ctx, 2*time.Second)
	defer cancel()
	started := time.Now()
	tick := time.NewTicker(20 * time.Millisecond)
	defer tick.Stop()
	observations := 0
	var stopped bool
	var connections int
	for {
		err := v.admin.QueryRow(ctx, `SELECT
 (SELECT state='stopped' AND stopped_at IS NOT NULL FROM agenteam_object.process_claims WHERE process_id=$1::uuid),
 (SELECT count(*) FROM pg_stat_activity WHERE datname=current_database() AND application_name='agenteam')`, v.objects.process.String()).Scan(&stopped, &connections)
		if err != nil && ctx.Err() != nil {
			t.Fatalf("M2 live original retirement observation deadline stopped=%t app_connections=%d observations=%d", stopped, connections, observations)
		}
		workAppCheck(t, "m2-live-original-exit-facts", err)
		observations++
		if observations == 1 {
			t.Logf("M2_LIVE_RETIREMENT_INITIAL exact_process=true same_database=true stopped=%t app_connections=%d original_guard_reacquired=true", stopped, connections)
		}
		if stopped && connections == 0 {
			t.Logf("M2_LIVE_RETIREMENT_COMPLETE stopped=true app_connections=0 observations=%d elapsed_ms=%d original_owners_joined=true", observations, time.Since(started).Milliseconds())
			return
		}
		select {
		case <-ctx.Done():
			t.Fatalf("M2 live original retirement deadline stopped=%t app_connections=%d observations=%d", stopped, connections, observations)
		case <-tick.C:
		}
	}
}

func (w *m1Workflow) m2LiveFinalFacts(t *testing.T) {
	t.Helper()
	v := w.app
	cfg, err := v.projects.service.GetSchedulerConfig(v.ctx, v.actor, v.a)
	workAppCheck(t, "m2-live-scheduler-still-enabled", err)
	if !cfg.Config.Enabled {
		t.Fatal("M2 live review disabled the Scheduler")
	}
	rows, err := v.admin.Query(v.ctx, `SELECT id,agent_id,launch_request->>'purpose',status FROM agenteam_execution.executions WHERE project_id=$1 AND launch_request->'trigger'->>'task_id'=$2`, v.a.String(), w.task.ID.String())
	workAppCheck(t, "m2-live-final-executions", err)
	defer rows.Close()
	work, review := 0, 0
	for rows.Next() {
		var execution, agent, purpose, status string
		workAppCheck(t, "m2-live-final-execution", rows.Scan(&execution, &agent, &purpose, &status))
		switch purpose {
		case "task/work":
			work++
			if (execution != w.m2Live.first.String() && execution != w.execution.String()) || agent != v.agent.String() || status != string(ec.Succeeded) {
				t.Fatal("M2 live original work Execution changed or was duplicated")
			}
		case "task/review":
			review++
			known := false
			for _, original := range w.m2Live.reviews {
				known = known || original.execution.String() == execution
			}
			if !known || agent != w.reviewer.String() || (status != string(ec.Succeeded) && status != string(ec.Cancelled)) {
				t.Fatal("M2 live review Execution has a mismatched reviewer or unexpected terminal status")
			}
		default:
			t.Fatal("M2 live Task acquired an unrelated Execution purpose")
		}
	}
	workAppCheck(t, "m2-live-final-execution-rows", rows.Err())
	rows.Close()
	if work != 2 || review != 2 || len(w.m2Live.reviews) != 2 {
		t.Fatal("M2 live did not retain exactly two original work Executions and two actual Artifact-reading review Executions")
	}
	var sentWork, sentReview, unknown, active, unfinishedTools, pending int
	workAppCheck(t, "m2-live-all-owners-terminal", v.admin.QueryRow(v.ctx, `SELECT
 (SELECT count(*) FROM agenteam_model.invocations i JOIN agenteam_execution.executions e ON e.id=i.execution_id::text WHERE i.project_id=$1::uuid AND i.dispatch='sent' AND e.launch_request->>'purpose'='task/work'),
 (SELECT count(*) FROM agenteam_model.invocations i JOIN agenteam_execution.executions e ON e.id=i.execution_id::text WHERE i.project_id=$1::uuid AND i.dispatch='sent' AND e.launch_request->>'purpose'='task/review'),
 (SELECT count(*) FROM agenteam_model.invocations WHERE project_id=$1::uuid AND (final_status='unknown' OR dispatch='unknown')),
 (SELECT count(*) FROM agenteam_model.invocations WHERE project_id=$1::uuid AND final_status IS NULL),
 (SELECT count(*) FROM agenteam_tool.operations WHERE project_id=$1::text AND (phase NOT IN('succeeded','failed','cancelled') OR outcome='unknown')),
 (SELECT count(*) FROM agenteam_scheduler.dispatches WHERE project_id=$1::text AND task_id=$2 AND status='pending')`, v.a.String(), w.task.ID.String()).Scan(&sentWork, &sentReview, &unknown, &active, &unfinishedTools, &pending))
	if unknown != 0 || active != 0 || unfinishedTools != 0 || pending != 0 || sentWork+sentReview >= m1LiveSentThreshold {
		t.Fatal("M2 live retained unknown or active owners, pending dispatches, or reached the project soft sent budget")
	}
	var invalidReview int
	workAppCheck(t, "m2-live-review-attribution", v.admin.QueryRow(v.ctx, `SELECT count(*) FROM agenteam_model.invocations i
 JOIN agenteam_execution.executions e ON e.id=i.execution_id::text
 LEFT JOIN agenteam_model.snapshots s ON s.id=i.snapshot_id AND s.project_id=i.project_id
 LEFT JOIN agenteam_execution.rounds r ON r.execution_id=e.id AND r.call_id=i.call_id::text AND r.id=i.input_data#>>'{value,round_id}'
 WHERE e.project_id=$1 AND e.launch_request->'trigger'->>'task_id'=$2 AND e.launch_request->>'purpose'='task/review'
 AND (i.project_id::text=$1 AND i.agent_id::text=$3 AND i.historical_model_id::text=$4
 AND i.identity_data#>>'{value,provider_model_id}'=$5 AND s.snapshot_data->>'Endpoint'=$6
 AND r.id IS NOT NULL AND i.purpose='agent_generation' AND i.consumer_kind='agent'
 AND (i.final_status<>'succeeded' OR (i.dispatch='sent' AND i.usage_source='provider'))
 AND (i.final_status<>'failed' OR ((i.error_data->>'retryable')::boolean AND NOT (i.error_data->>'partial_output')::boolean))) IS NOT TRUE`, v.a.String(), w.task.ID.String(), w.reviewer.String(), w.reviewModel.String(), w.live.Model, w.live.BaseURL).Scan(&invalidReview))
	var reviewTools, invalidReviewTools, unretired int
	workAppCheck(t, "m2-live-review-and-attempt-owners", v.admin.QueryRow(v.ctx, `SELECT
 (SELECT count(*) FROM agenteam_tool.operations o JOIN agenteam_execution.executions e ON e.id=o.execution_id WHERE e.project_id=$1 AND e.launch_request->'trigger'->>'task_id'=$2 AND e.launch_request->>'purpose'='task/review'),
 (SELECT count(*) FROM agenteam_tool.operations o JOIN agenteam_execution.executions e ON e.id=o.execution_id WHERE e.project_id=$1 AND e.launch_request->'trigger'->>'task_id'=$2 AND e.launch_request->>'purpose'='task/review'
 AND (o.operation_kind<>'artifact.read' OR o.phase<>'succeeded' OR o.outcome<>'success' OR NOT((o.operation_id=$3 AND o.execution_id=$5) OR (o.operation_id=$4 AND o.execution_id=$6)))),
 (SELECT count(*) FROM agenteam_tool.attempts WHERE project_id=$1 AND NOT retired)`, v.a.String(), w.task.ID.String(), w.m2Live.reviews[0].operation, w.m2Live.reviews[1].operation, w.m2Live.reviews[0].execution.String(), w.m2Live.reviews[1].execution.String()).Scan(&reviewTools, &invalidReviewTools, &unretired))
	if reviewTools != 2 || invalidReviewTools != 0 || unretired != 0 {
		t.Fatal("M2 live reviewer lacks its two exact Artifact read Operations or a Tool Attempt owner remained unretired")
	}
	if invalidReview != 0 {
		t.Fatal("M2 live review model attempt lost its original provider/snapshot/round attribution")
	}
	t.Logf("M2_LIVE_FINAL_FACTS work_executions=%d review_executions=%d sent_work=%d sent_review=%d sent_total=%d unknown=0 active=0 autonomous_reviewer=false", work, review, sentWork, sentReview, sentWork+sentReview)
}
