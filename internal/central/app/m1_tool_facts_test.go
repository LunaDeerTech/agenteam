//go:build integration && linux

package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	wc "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
	rp "github.com/LunaDeerTech/agenteam/internal/runnerprotocol"
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

func (w *m1Workflow) checkPeer(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithTimeout(w.app.ctx, 5*time.Second)
	defer cancel()
	tick := time.NewTicker(20 * time.Millisecond)
	defer tick.Stop()
	for {
		state, err := w.network.State(ctx, w.scenario)
		workAppCheck(t, "m1-owned-peer-state", err)
		closed := state.ActiveHandlers == 0 && state.CompletedHandlers == 5
		for _, request := range state.Requests {
			closed = closed && state.Closed[request.Connection]
		}
		if closed {
			if len(state.Requests) != 5 {
				t.Fatalf("M1 Provider request count=%d", len(state.Requests))
			}
			var first []json.RawMessage
			for n, request := range state.Requests {
				var body struct {
					Model    string            `json:"model"`
					Stream   bool              `json:"stream"`
					Parallel *bool             `json:"parallel_tool_calls"`
					Choice   string            `json:"tool_choice"`
					Tools    []json.RawMessage `json:"tools"`
					Messages []json.RawMessage `json:"messages"`
				}
				workAppCheck(t, "m1-owned-peer-request", json.Unmarshal([]byte(request.Body), &body))
				choice := "auto"
				if n == 0 {
					choice = "required"
					first = body.Tools
				}
				if request.Method != "POST" || body.Model != "m1-statistics" || body.Stream || body.Parallel == nil || *body.Parallel || body.Choice != choice || len(body.Tools) < 3 || !reflect.DeepEqual(body.Tools, first) || len(body.Messages) != 2+2*n {
					t.Fatalf("M1 Provider request shape or frozen tools/history differs at round=%d", n+1)
				}
			}
			return
		}
		select {
		case <-ctx.Done():
			t.Fatalf("M1 Provider transport did not join: requests=%d active=%d completed=%d", len(state.Requests), state.ActiveHandlers, state.CompletedHandlers)
		case <-tick.C:
		}
	}
}
