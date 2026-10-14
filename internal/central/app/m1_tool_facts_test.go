//go:build integration && linux

package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	wc "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
	rp "github.com/LunaDeerTech/agenteam/internal/runnerprotocol"
	netfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/outbound"
)

// Observe the original Scheduler/Execution owners; the test never advances a
// round or constructs a successful terminal row on their behalf.
func (w *m1Workflow) awaitResult(t *testing.T) {
	t.Helper()
	v := w.app
	ctx, cancel := context.WithTimeout(v.ctx, 40*time.Second)
	defer cancel()
	tick := time.NewTicker(25 * time.Millisecond)
	defer tick.Stop()
	var status, raw string
	var count int
	for {
		var observedCount int
		var observedID, observedStatus string
		err := v.admin.QueryRow(ctx, `SELECT count(*),COALESCE(min(id),''),COALESCE(min(status),'') FROM agenteam_execution.executions WHERE project_id=$1 AND launch_request->'trigger'->>'task_id'=$2`, v.a.String(), w.task.ID.String()).Scan(&observedCount, &observedID, &observedStatus)
		if err != nil {
			w.diagnoseResult(t, count, raw, status)
			if ctx.Err() != nil {
				t.Fatal("M1 original Execution did not finish within the unchanged 40s budget")
			}
		}
		workAppCheck(t, "m1-observe-execution", err)
		count, raw, status = observedCount, observedID, observedStatus
		if count > 1 {
			w.diagnoseResult(t, count, raw, status)
			t.Fatal("M1 Task acquired multiple Executions")
		}
		if count == 1 && ec.Status(status).Terminal() {
			if status != string(ec.Succeeded) {
				w.diagnoseResult(t, count, raw, status)
				t.Fatalf("M1 original Execution terminal status=%s", status)
			}
			w.execution, err = f.ParseID[id.Execution](raw)
			workAppCheck(t, "m1-observed-execution-id", err)
			break
		}
		for _, project := range v.runtime.manager.Observation().Projects {
			if project.ProjectID == v.a && !project.Running && !project.Retained && !project.Stopping && project.Err != nil {
				w.diagnoseResult(t, count, raw, status)
				t.Fatal("M1 original project runner returned with an error before Execution completion")
			}
		}
		select {
		case <-ctx.Done():
			w.diagnoseResult(t, count, raw, status)
			t.Fatalf("M1 original Execution did not finish within budget; status=%s", status)
		case <-tick.C:
		}
	}
	path := taskExecutionReadPath(v.a, w.task.ID.String(), w.execution) + "/runtime-view"
	response := taskExecutionGet(t, v.workAppFixture, v.client, v.cookie, "GET", path, 200)
	workAppCheck(t, "m1-runtime-view-decode", json.Unmarshal(response.body, &w.view))
	workAppCheck(t, "m1-runtime-view-contract", w.view.ValidateProjection())
	if w.view.Execution.ID != w.execution || w.view.Execution.TaskID != w.task.ID.String() || w.view.Execution.AgentID != v.agent || w.view.Execution.Status != ec.Succeeded || w.view.Availability != ec.RuntimeAvailable || w.view.Snapshot == nil || w.view.Snapshot.Schema != ec.DurableFinalTextV2 || len(w.view.Snapshot.Items) != 1 || w.view.Snapshot.Items[0].Data.Content != m1FinalOutput {
		t.Fatal("M1 public final projection differs from the original completed workflow")
	}
	before := w.task.Version
	var err error
	w.task, err = v.planning.taskReader.GetTask(v.ctx, v.actor, v.a, w.task.ID)
	workAppCheck(t, "m1-task-after-execution", err)
	if w.task.State != wc.TaskStateInProgress || w.task.Version != before+1 || w.task.AssigneeAgentID == nil || *w.task.AssigneeAgentID != v.agent {
		t.Fatal("M1 Scheduler claim or retained Task state differs")
	}
	var pending, active, launched int
	workAppCheck(t, "m1-original-occupancy", v.admin.QueryRow(v.ctx, `SELECT
 (SELECT count(*) FROM agenteam_scheduler.dispatches WHERE project_id=$1 AND task_id=$2 AND status='pending'),
 (SELECT count(*) FROM agenteam_execution.executions WHERE project_id=$1 AND agent_id=$3 AND status IN('created','preparing','running','waiting')),
 (SELECT count(*) FROM agenteam_scheduler.dispatches WHERE project_id=$1 AND task_id=$2 AND execution_id=$4 AND status='launched')`, v.a.String(), w.task.ID.String(), v.agent.String(), w.execution.String()).Scan(&pending, &active, &launched))
	if pending != 0 || active != 0 || launched != 1 {
		t.Fatalf("M1 original scheduling facts pending=%d active=%d launched=%d", pending, active, launched)
	}
}

// The observation context may already have expired. This separate bounded
// read exposes only durable counters, enum states and safe identities, never
// prompts, arguments, request headers, credential material or provider bodies.
func (w *m1Workflow) diagnoseResult(t *testing.T, count int, execution, status string) {
	t.Helper()
	t.Logf("M1 last observation execution_count=%d execution_id=%s status=%s", count, execution, status)
	manager := w.app.runtime.manager.Observation()
	t.Logf("M1 manager observation started=%t stopped=%t projects=%d", manager.Started, manager.Stopped, len(manager.Projects))
	logFault := func(stage string, err error) {
		var fault *f.Fault
		if errors.As(err, &fault) {
			t.Logf("M1 manager %s fault_code=%s outcome=%s", stage, fault.Code.Safe(), fault.CommitState.Safe())
		} else if err != nil {
			t.Logf("M1 manager %s error_type=%T", stage, err)
		}
	}
	logFault("discovery", manager.DiscoveryError)
	for _, project := range manager.Projects {
		if project.ProjectID == w.app.a {
			t.Logf("M1 target project running=%t retained=%t stopping=%t", project.Running, project.Retained, project.Stopping)
			logFault("target-project", project.Err)
		}
	}
	ctx, cancel := context.WithTimeout(w.app.ctx, 2*time.Second)
	defer cancel()
	var facts []byte
	err := w.app.admin.QueryRow(ctx, `WITH selected AS (
 SELECT id,status,version,cancel_requested_at FROM agenteam_execution.executions
 WHERE project_id=$1 AND launch_request->'trigger'->>'task_id'=$2
 ) SELECT jsonb_build_object(
 'task',(SELECT jsonb_build_object('state',state,'version',version) FROM agenteam_work.tasks WHERE project_id::text=$1 AND id::text=$2),
 'dispatch',(SELECT COALESCE(jsonb_agg(jsonb_build_object('status',status,'outcome',launch_outcome,'attempts',attempt_count,'reason',failure_reason)),'[]'::jsonb) FROM agenteam_scheduler.dispatches WHERE project_id=$1 AND task_id=$2),
 'execution',(SELECT COALESCE(jsonb_agg(jsonb_build_object('status',status,'version',version,'cancel_requested',cancel_requested_at IS NOT NULL)),'[]'::jsonb) FROM selected),
 'preparation',(SELECT COALESCE(jsonb_agg(jsonb_build_object('phase',phase,'fence',fence)),'[]'::jsonb) FROM agenteam_execution.preparation_attempts WHERE execution_id IN(SELECT id FROM selected)),
 'preparation_inputs',(SELECT count(*) FROM agenteam_execution.preparation_inputs WHERE execution_id IN(SELECT id FROM selected)),
 'model_resolution',(SELECT COALESCE(jsonb_agg(jsonb_build_object('phase',phase,'plan_version',plan_version)),'[]'::jsonb) FROM agenteam_model.resolution_preparations WHERE owner_kind='execution' AND owner_id::text IN(SELECT id FROM selected)),
 'model_snapshots',(SELECT count(*) FROM agenteam_model.snapshots WHERE id IN(SELECT snapshot_id FROM agenteam_model.resolution_preparations WHERE owner_kind='execution' AND owner_id::text IN(SELECT id FROM selected))),
 'snapshots',(SELECT count(*) FROM agenteam_execution.snapshots WHERE execution_id IN(SELECT id FROM selected)),
 'rounds',(SELECT COALESCE(jsonb_agg(jsonb_build_object('ordinal',round_ordinal,'schema',schema_version,'tool_pending',tool_pending,'status',terminal_status,'reason',terminal_reason) ORDER BY round_ordinal),'[]'::jsonb) FROM agenteam_execution.rounds WHERE execution_id IN(SELECT id FROM selected)),
 'model',(SELECT COALESCE(jsonb_agg(jsonb_build_object('dispatch',dispatch,'status',final_status,'category',error_data->>'category','code',error_data->>'code','usage_source',usage_source)),'[]'::jsonb) FROM agenteam_model.invocations WHERE execution_id::text IN(SELECT id FROM selected)),
 'tool',(SELECT COALESCE(jsonb_agg(jsonb_build_object('phase',phase,'outcome',outcome)),'[]'::jsonb) FROM agenteam_tool.operations WHERE execution_id IN(SELECT id FROM selected)),
 'attempts',(SELECT COALESCE(jsonb_agg(jsonb_build_object('phase',phase,'retired',retired)),'[]'::jsonb) FROM agenteam_tool.attempts WHERE execution_id IN(SELECT id FROM selected)),
 'results',(SELECT count(*) FROM agenteam_execution.loop_tool_results WHERE execution_id IN(SELECT id FROM selected)))`, w.app.a.String(), w.task.ID.String()).Scan(&facts)
	if err != nil {
		t.Logf("M1 safe durable diagnostic unavailable error_type=%T budget_done=%t", err, ctx.Err() != nil)
	} else {
		t.Logf("M1 safe durable diagnostic=%s", facts)
	}
	state, err := w.network.State(ctx, w.scenario)
	if err != nil {
		t.Logf("M1 safe peer diagnostic unavailable error_type=%T budget_done=%t", err, ctx.Err() != nil)
		return
	}
	t.Logf("M1 safe peer diagnostic requests=%d active_handlers=%d completed_handlers=%d", len(state.Requests), state.ActiveHandlers, state.CompletedHandlers)
}

func (w *m1Workflow) checkArtifacts(t *testing.T) {
	t.Helper()
	v := w.app
	for name, want := range map[string]string{"stats.py": m1Program, "result.json": m1Result} {
		got, err := os.ReadFile(filepath.Join(w.workspace, name))
		workAppCheck(t, "m1-real-workspace-artifact", err)
		if !bytes.Equal(got, []byte(want)) {
			t.Fatalf("M1 actual artifact differs: %s", name)
		}
	}
	var report, stdout, stderr []byte
	workAppCheck(t, "m1-command-result", v.admin.QueryRow(v.ctx, `SELECT body_data,stdout_data,stderr_data FROM agenteam_tool.command_results WHERE execution_id=$1`, w.execution.String()).Scan(&report, &stdout, &stderr))
	decoded, err := rp.DecodeRunCommandResult(report)
	workAppCheck(t, "m1-command-report", err)
	if decoded.ExitCode != 0 || decoded.Signal != 0 || decoded.Cancelled || decoded.TimedOut || !decoded.OutputComplete || !decoded.ProcessGroupTerminationConfirmed || decoded.StdoutTruncated || decoded.StderrTruncated || decoded.CompletionCode != nil || !bytes.Equal(stdout, []byte(m1Result)) || len(stderr) != 0 {
		t.Fatal("M1 command did not persist the complete expected stdout and successful real exit")
	}
	var operations, invocations, logicalCalls, snapshots, rounds, results, reports int
	var toolOK, modelOK bool
	workAppCheck(t, "m1-tool-facts", v.admin.QueryRow(v.ctx, `SELECT count(*),COALESCE(bool_and(phase='succeeded' AND outcome='success'),false) FROM agenteam_tool.operations WHERE execution_id=$1`, w.execution.String()).Scan(&operations, &toolOK))
	workAppCheck(t, "m1-model-facts", v.admin.QueryRow(v.ctx, `SELECT count(*),count(DISTINCT call_id),count(DISTINCT snapshot_id),COALESCE(bool_and(final_status='succeeded' AND dispatch='sent' AND usage_source='provider' AND input_tokens=10 AND output_tokens=10 AND total_tokens=20),false) FROM agenteam_model.invocations WHERE execution_id=$1::uuid AND project_id=$2::uuid`, w.execution.String(), v.a.String()).Scan(&invocations, &logicalCalls, &snapshots, &modelOK))
	workAppCheck(t, "m1-round-result-facts", v.admin.QueryRow(v.ctx, `SELECT
 (SELECT count(*) FROM agenteam_execution.rounds WHERE execution_id=$1),
 (SELECT count(*) FROM agenteam_execution.loop_tool_results WHERE execution_id=$1),
 (SELECT count(*) FROM agenteam_tool.command_results WHERE execution_id=$1)`, w.execution.String()).Scan(&rounds, &results, &reports))
	if operations != 4 || !toolOK || invocations != 5 || logicalCalls != 5 || snapshots != 1 || !modelOK || rounds != 5 || results != 4 || reports != 1 {
		t.Fatalf("M1 durable counts operations=%d invocations=%d calls=%d snapshots=%d rounds=%d results=%d reports=%d tool_ok=%t model_ok=%t", operations, invocations, logicalCalls, snapshots, rounds, results, reports, toolOK, modelOK)
	}
	var content []byte
	var round string
	var originalTx bool
	workAppCheck(t, "m1-final-round-provenance", v.admin.QueryRow(v.ctx, `SELECT s.content,r.id,
 e.xmin=r.xmin AND e.xmin=s.xmin AND e.status='succeeded' AND r.terminal_status=e.status AND s.terminal_status=e.status
 AND e.version=r.terminal_version AND e.version=s.terminal_version AND e.completed_at=r.finished_at AND e.completed_at=s.completed_at
 AND r.round_ordinal=5 AND r.schema_version=4 AND NOT r.tool_pending AND r.snapshot_id=e.snapshot_id AND s.snapshot_id=e.snapshot_id
 FROM agenteam_execution.executions e JOIN agenteam_execution.runtime_snapshots s ON s.execution_id=e.id
 JOIN agenteam_execution.rounds r ON r.execution_id=e.id AND r.id=s.round_id WHERE e.id=$1`, w.execution.String()).Scan(&content, &round, &originalTx))
	var snapshot ec.RuntimeSnapshot
	workAppCheck(t, "m1-durable-projection", json.Unmarshal(content, &snapshot))
	if !originalTx || round != w.view.Snapshot.Items[0].ID.String() || !reflect.DeepEqual(snapshot, *w.view.Snapshot) {
		t.Fatal("M1 public projection lost original final Round/Execution transaction provenance")
	}
}

// Work and review are separate, formally selected Provider scenarios. Their
// response IDs join the actual Usage ledger to the original durable Round;
// local CallIDs are not invented as outbound HTTP headers.
func (w *m1Workflow) checkPeer(t *testing.T) {
	t.Helper()
	v := w.app
	rows, err := v.admin.Query(v.ctx, `SELECT id,agent_id,launch_request->>'purpose',status FROM agenteam_execution.executions WHERE project_id=$1 AND launch_request->'trigger'->>'task_id'=$2 ORDER BY created_at,id`, v.a.String(), w.task.ID.String())
	workAppCheck(t, "m1-task-execution-scope", err)
	var review, reviewStatus string
	workCount, reviewCount := 0, 0
	for rows.Next() {
		var execution, agent, purpose, status string
		workAppCheck(t, "m1-task-execution-row", rows.Scan(&execution, &agent, &purpose, &status))
		switch purpose {
		case "task/work":
			workCount++
			if execution != w.execution.String() || agent != v.agent.String() || status != string(ec.Succeeded) {
				t.Fatal("M1 Owner window acquired a second or mismatched work Execution")
			}
		case "task/review":
			reviewCount++
			if agent != w.reviewer.String() || !ec.Status(status).Valid() || status == string(ec.Failed) {
				t.Fatal("M1 review Execution lost its original Task/reviewer or failed")
			}
			review, reviewStatus = execution, status
		default:
			t.Fatal("M1 Task acquired an unrelated Execution purpose")
		}
	}
	workAppCheck(t, "m1-task-execution-rows", rows.Err())
	rows.Close()
	if workCount != 1 || reviewCount > 1 {
		t.Fatalf("M1 bounded Owner window work_executions=%d review_executions=%d", workCount, reviewCount)
	}
	workPeer := w.joinedPeer(t, w.scenario, 5, 5)
	workCalls := w.peerCalls(t, w.execution.String(), w.scenario, "m1-statistics", "")
	if len(workCalls) != 5 {
		t.Fatalf("M1 original work invocation count=%d", len(workCalls))
	}
	for n, request := range workPeer.Requests {
		call := workCalls[n]
		if call.status != "succeeded" || call.dispatch != "sent" || call.providerID != fmt.Sprintf("m1-work-round-%d", n+1) {
			t.Fatalf("M1 work Provider response lost original invocation identity at round=%d", n+1)
		}
		checkM1PeerRequest(t, request, call.round, "m1-statistics", w.scenario)
	}
	// Recheck terminal work facts after the entire Owner window, including all
	// actual file effects, four Tools and five Model invocations on that same E.
	w.checkArtifacts(t)
	reviewPeer := w.joinedPeer(t, w.reviewScenario, 0, 2)
	if review == "" {
		if len(reviewPeer.Requests) != 0 {
			t.Fatal("M1 review Provider received a request without a legitimate review Execution")
		}
		return
	}
	reviewCalls := w.peerCalls(t, review, w.reviewScenario, "m1-review", w.reviewModel.String())
	if len(reviewCalls) > 2 || len(reviewPeer.Requests) > len(reviewCalls) {
		t.Fatalf("M1 bounded review invocations=%d requests=%d", len(reviewCalls), len(reviewPeer.Requests))
	}
	for n, call := range reviewCalls {
		if call.status != "succeeded" && call.status != "cancelled" {
			t.Fatalf("M1 review has an unexpected original Model outcome at round=%d status=%s", n+1, call.status)
		}
		if n >= len(reviewPeer.Requests) {
			if call.status != "cancelled" || n != len(reviewCalls)-1 {
				t.Fatal("M1 unobserved review request lacks the original final cancellation")
			}
			continue
		}
		if call.dispatch != "sent" || call.providerID != fmt.Sprintf("m1-review-round-%d", n+1) && !(call.status == "cancelled" && call.providerID == "") {
			t.Fatalf("M1 review Provider response lost original invocation identity at round=%d", n+1)
		}
		checkM1PeerRequest(t, reviewPeer.Requests[n], call.round, "m1-review", w.reviewScenario)
	}
	var tools, results int
	var toolsOK bool
	workAppCheck(t, "m1-review-bounded-tool-facts", v.admin.QueryRow(v.ctx, `SELECT count(*),COALESCE(bool_and((phase='succeeded' AND outcome='success') IS TRUE),false),(SELECT count(*) FROM agenteam_execution.loop_tool_results WHERE execution_id=$1) FROM agenteam_tool.operations WHERE execution_id=$1`, review).Scan(&tools, &toolsOK, &results))
	if tools > 1 || results > 1 {
		t.Fatalf("M1 review exceeded its read-only receipt scope tools=%d results=%d", tools, results)
	}
	if reviewStatus == string(ec.Succeeded) {
		if len(reviewCalls) != 2 || len(reviewPeer.Requests) != 2 || reviewCalls[0].status != "succeeded" || reviewCalls[1].status != "succeeded" || tools != 1 || !toolsOK || results != 1 {
			t.Fatal("M1 successful review receipt lacks its original two rounds and one read result")
		}
		var projection []byte
		workAppCheck(t, "m1-review-receipt-projection", v.admin.QueryRow(v.ctx, `SELECT content FROM agenteam_execution.runtime_snapshots WHERE execution_id=$1 AND terminal_status='succeeded'`, review).Scan(&projection))
		var snapshot ec.RuntimeSnapshot
		workAppCheck(t, "m1-review-receipt-decode", json.Unmarshal(projection, &snapshot))
		if snapshot.Schema != ec.DurableFinalTextV2 || len(snapshot.Items) != 1 || snapshot.Items[0].Data.Content != "评审任务已收到，等待 Owner 后续处理。" {
			t.Fatal("M1 review receipt projection differs; no review decision is implied")
		}
	}
	t.Logf("M1_PEER_ATTRIBUTION work_executions=1 work_requests=5 review_executions=%d review_requests=%d review_status=%s review_decision=false all_connections_joined=true", reviewCount, len(reviewPeer.Requests), reviewStatus)
}

func (w *m1Workflow) joinedPeer(t *testing.T, scenario string, minimum, maximum int) netfixture.State {
	t.Helper()
	if scenario == "" {
		t.Fatal("M1 independent Provider scenario absent")
	}
	ctx, cancel := context.WithTimeout(w.app.ctx, 5*time.Second)
	defer cancel()
	tick := time.NewTicker(20 * time.Millisecond)
	defer tick.Stop()
	for {
		state, err := w.network.State(ctx, scenario)
		workAppCheck(t, "m1-owned-peer-state", err)
		if len(state.Requests) > maximum {
			t.Fatalf("M1 Provider exceeded explicit request bound actual=%d maximum=%d", len(state.Requests), maximum)
		}
		closed := state.ActiveHandlers == 0 && state.CompletedHandlers == len(state.Requests)
		for _, request := range state.Requests {
			closed = closed && state.Closed[request.Connection]
		}
		for connection := range state.Connections {
			closed = closed && state.Closed[connection]
		}
		if closed {
			if len(state.Requests) < minimum {
				t.Fatalf("M1 Provider request count=%d minimum=%d", len(state.Requests), minimum)
			}
			return state
		}
		select {
		case <-ctx.Done():
			t.Fatalf("M1 Provider transport did not join: requests=%d active=%d completed=%d", len(state.Requests), state.ActiveHandlers, state.CompletedHandlers)
		case <-tick.C:
		}
	}
}

type m1PeerCall struct {
	status, dispatch, providerID string
	round                        ec.LoopRoundFields
}

func (w *m1Workflow) peerCalls(t *testing.T, execution, scenario, providerModel, model string) []m1PeerCall {
	t.Helper()
	v := w.app
	var count int
	workAppCheck(t, "m1-provider-invocation-count", v.admin.QueryRow(v.ctx, `SELECT count(*) FROM agenteam_model.invocations WHERE execution_id::text=$1`, execution).Scan(&count))
	rows, err := v.admin.Query(v.ctx, `SELECT i.call_id::text,i.attempt_index,i.dispatch,COALESCE(i.final_status,''),i.provider_request_id,i.historical_model_id::text,
 i.identity_data#>>'{value,provider_model_id}',s.snapshot_data->>'Endpoint',r.content
 FROM agenteam_model.invocations i JOIN agenteam_execution.rounds r ON r.execution_id=i.execution_id::text AND r.call_id=i.call_id::text AND r.id=i.input_data#>>'{value,round_id}'
 JOIN agenteam_model.snapshots s ON s.id=i.snapshot_id
 WHERE i.execution_id::text=$1 AND i.project_id::text=$2 ORDER BY r.round_ordinal,i.attempt_index`, execution, v.a.String())
	workAppCheck(t, "m1-provider-original-rounds", err)
	defer rows.Close()
	var calls []m1PeerCall
	for rows.Next() {
		var call m1PeerCall
		var callID, modelID, upstreamModel, endpoint string
		var attempt int
		var content []byte
		workAppCheck(t, "m1-provider-original-round-row", rows.Scan(&callID, &attempt, &call.dispatch, &call.status, &call.providerID, &modelID, &upstreamModel, &endpoint, &content))
		round, err := ec.DecodeLoopRound(content)
		workAppCheck(t, "m1-provider-original-round-decode", err)
		call.round = round.Fields()
		if attempt != 1 || call.round.Round.ExecutionID.String() != execution || call.round.Round.CallID.String() != callID || int(call.round.Ordinal) != len(calls)+1 || upstreamModel != providerModel || endpoint != "https://fixture.test:8443/case/"+scenario || model != "" && modelID != model {
			t.Fatal("M1 Provider request no longer belongs to its exact original Execution/round/Model")
		}
		if len(calls) > 0 && !reflect.DeepEqual(calls[0].round.Tools, call.round.Tools) {
			t.Fatal("M1 durable successor changed the frozen complete tool set")
		}
		calls = append(calls, call)
	}
	workAppCheck(t, "m1-provider-original-round-rows", rows.Err())
	rows.Close()
	if len(calls) != count {
		t.Fatal("M1 invocation lacks an exact original round and snapshot binding")
	}
	if len(calls) > 0 {
		var names []string
		workAppCheck(t, "m1-provider-frozen-tools", v.admin.QueryRow(v.ctx, `SELECT array_agg(model_visible_name ORDER BY model_visible_name) FROM agenteam_tool.execution_references WHERE execution_id=$1`, execution).Scan(&names))
		seen := make(map[string]bool, len(names))
		for _, name := range names {
			seen[name] = true
		}
		if len(names) != len(calls[0].round.Tools) {
			t.Fatal("M1 Provider dropped a captured Core/ordinary tool")
		}
		for _, tool := range calls[0].round.Tools {
			if !seen[tool.Name] {
				t.Fatal("M1 Provider introduced an uncaptured tool")
			}
		}
	}
	return calls
}

func checkM1PeerRequest(t *testing.T, request netfixture.Request, round ec.LoopRoundFields, model, scenario string) {
	t.Helper()
	var body struct {
		Model    string `json:"model"`
		Stream   bool   `json:"stream"`
		Parallel *bool  `json:"parallel_tool_calls"`
		Choice   string `json:"tool_choice"`
		Tools    []struct {
			Type     string `json:"type"`
			Function struct {
				Name, Description string
				Parameters        json.RawMessage
				Strict            *bool
			} `json:"function"`
		} `json:"tools"`
		Messages []struct {
			Role       string          `json:"role"`
			Content    json.RawMessage `json:"content"`
			ToolCallID string          `json:"tool_call_id"`
			ToolCalls  []struct {
				ID, Type string
				Function struct{ Name, Arguments string }
			} `json:"tool_calls"`
		} `json:"messages"`
	}
	workAppCheck(t, "m1-owned-peer-request", json.Unmarshal([]byte(request.Body), &body))
	if request.Method != "POST" || request.Path != "/case/"+scenario+"/chat/completions" || body.Model != model || body.Stream || body.Parallel == nil || *body.Parallel || body.Choice != round.Choice.Kind || len(body.Tools) != len(round.Tools) || len(body.Messages) != len(round.Round.Messages) {
		t.Fatal("M1 Provider request differs from its exact frozen round")
	}
	jsonEqual := func(a, b []byte) bool {
		x, xe := cursor.CanonicalJSON(a)
		y, ye := cursor.CanonicalJSON(b)
		return xe == nil && ye == nil && bytes.Equal(x, y)
	}
	for n, tool := range round.Tools {
		got := body.Tools[n]
		if got.Type != "function" || got.Function.Name != tool.Name || got.Function.Description != tool.Description || got.Function.Strict == nil || *got.Function.Strict || !jsonEqual(got.Function.Parameters, tool.InputSchema) {
			t.Fatal("M1 Provider function definition differs from the complete frozen tool set")
		}
	}
	for n, message := range round.Round.Messages {
		got := body.Messages[n]
		if got.Role != message.Role {
			t.Fatal("M1 Provider history role differs from its original round")
		}
		var texts []string
		var calls []mc.ToolCallPart
		var result *mc.ToolResultPart
		for _, part := range message.Parts {
			if part.Text != nil {
				texts = append(texts, part.Text.Text)
			} else if part.ToolCall != nil {
				calls = append(calls, *part.ToolCall)
			} else if part.ToolResult != nil && result == nil {
				result = part.ToolResult
			} else {
				t.Fatal("M1 original round contains an unexpected history part")
			}
		}
		if result != nil {
			var parts []struct{ Type, Text string }
			workAppCheck(t, "m1-provider-tool-result", json.Unmarshal(got.Content, &parts))
			if got.ToolCallID != result.CallID || len(got.ToolCalls) != 0 || len(parts) != len(result.Parts) {
				t.Fatal("M1 Provider lost the exact predecessor ToolResult pairing")
			}
			for p, part := range result.Parts {
				if part.Text == nil || parts[p].Type != "text" || parts[p].Text != part.Text.Text {
					t.Fatal("M1 Provider tool result differs from the durable original result")
				}
			}
			continue
		}
		var content *string
		workAppCheck(t, "m1-provider-message-content", json.Unmarshal(got.Content, &content))
		if len(texts) > 1 || len(texts) == 0 && content != nil || len(texts) == 1 && (content == nil || *content != texts[0]) || got.ToolCallID != "" || len(got.ToolCalls) != len(calls) {
			t.Fatal("M1 Provider history text or assistant ToolCall count differs")
		}
		for p, call := range calls {
			actual := got.ToolCalls[p]
			if actual.ID != call.ID || actual.Type != "function" || actual.Function.Name != call.Name || !jsonEqual([]byte(actual.Function.Arguments), call.Arguments) {
				t.Fatal("M1 Provider assistant ToolCall differs from the durable predecessor")
			}
		}
	}
}
