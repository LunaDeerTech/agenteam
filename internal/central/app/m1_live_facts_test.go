//go:build integration && linux

package app

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
	wc "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
	rp "github.com/LunaDeerTech/agenteam/internal/runnerprotocol"
)

// This observer uses the live fixture's total budget. Its owner, rather than
// this polling loop, stops admission, drains and joins the original runtime.
func (w *m1Workflow) awaitLiveResult(t *testing.T) {
	t.Helper()
	v := w.app
	tick := time.NewTicker(50 * time.Millisecond)
	defer tick.Stop()
	for {
		var count int
		var raw, status string
		err := v.admin.QueryRow(v.ctx, `SELECT count(*),COALESCE(min(id),''),COALESCE(min(status),'') FROM agenteam_execution.executions WHERE project_id=$1 AND launch_request->'trigger'->>'task_id'=$2`, v.a.String(), w.task.ID.String()).Scan(&count, &raw, &status)
		workAppCheck(t, "m1-live-observe-execution", err)
		if count > 1 {
			t.Fatal("M1 live Task acquired multiple Executions before the Owner action")
		}
		if count == 1 && ec.Status(status).Terminal() {
			if status != string(ec.Succeeded) {
				t.Fatalf("M1 live original work Execution terminal status=%s", status)
			}
			w.execution, err = f.ParseID[id.Execution](raw)
			workAppCheck(t, "m1-live-execution-id", err)
			break
		}
		for _, project := range v.runtime.manager.Observation().Projects {
			if project.ProjectID == v.a && !project.Running && !project.Retained && !project.Stopping && project.Err != nil {
				t.Fatal("M1 live original project runner returned an error before completion")
			}
		}
		select {
		case <-v.ctx.Done():
			t.Fatalf("M1 live work did not finish within its original total budget; status=%s", status)
		case <-tick.C:
		}
	}
	path := taskExecutionReadPath(v.a, w.task.ID.String(), w.execution) + "/runtime-view"
	response := taskExecutionGet(t, v.workAppFixture, v.client, v.cookie, "GET", path, 200)
	workAppCheck(t, "m1-live-runtime-view", json.Unmarshal(response.body, &w.view))
	workAppCheck(t, "m1-live-runtime-view-contract", w.view.ValidateProjection())
	if w.view.Execution.ID != w.execution || w.view.Execution.TaskID != w.task.ID.String() || w.view.Execution.AgentID != v.agent || w.view.Execution.Status != ec.Succeeded || w.view.Availability != ec.RuntimeAvailable || w.view.Snapshot == nil || w.view.Snapshot.Schema != ec.DurableFinalTextV2 || len(w.view.Snapshot.Items) != 1 {
		t.Fatal("M1 live public projection lost the original completed workflow")
	}
	content := w.view.Snapshot.Items[0].Data.Content
	if !utf8.ValidString(content) || strings.TrimSpace(content) == "" {
		t.Fatal("M1 live final text is empty or invalid UTF-8")
	}
	before := w.task.Version
	var err error
	w.task, err = v.planning.taskReader.GetTask(v.ctx, v.actor, v.a, w.task.ID)
	workAppCheck(t, "m1-live-task-after-work", err)
	if w.task.State != wc.TaskStateInProgress || w.task.Version != before+1 || w.task.AssigneeAgentID == nil || *w.task.AssigneeAgentID != v.agent {
		t.Fatal("M1 live Scheduler claim or retained Task state differs")
	}
	var pending, active, launched int
	workAppCheck(t, "m1-live-original-occupancy", v.admin.QueryRow(v.ctx, `SELECT
 (SELECT count(*) FROM agenteam_scheduler.dispatches WHERE project_id=$1 AND task_id=$2 AND status='pending'),
 (SELECT count(*) FROM agenteam_execution.executions WHERE project_id=$1 AND agent_id=$3 AND status IN('created','preparing','running','waiting')),
 (SELECT count(*) FROM agenteam_scheduler.dispatches WHERE project_id=$1 AND task_id=$2 AND execution_id=$4 AND status='launched')`, v.a.String(), w.task.ID.String(), v.agent.String(), w.execution.String()).Scan(&pending, &active, &launched))
	if pending != 0 || active != 0 || launched != 1 {
		t.Fatalf("M1 live scheduling facts pending=%d active=%d launched=%d", pending, active, launched)
	}
}

// The oracle is confined to the verifier. Neither these values nor a program
// implementation is sent to the live model by this file.
func m1LiveStatistics(raw []byte) bool {
	var value map[string]float64
	return json.Unmarshal(raw, &value) == nil && len(value) == 3 && value["count"] == 4 && value["sum"] == 20 && value["mean"] == 5
}

// Diagnostics expose enums and counts only. In particular, no network fixture,
// prompt, provider body, endpoint, argument bytes or Secret is inspected here.
func (w *m1Workflow) diagnoseLiveResult(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.WithoutCancel(w.app.ctx), 2*time.Second)
	defer cancel()
	var facts []byte
	err := w.app.admin.QueryRow(ctx, `WITH selected AS (SELECT id,status,launch_request->>'purpose' AS purpose FROM agenteam_execution.executions WHERE project_id=$1 AND launch_request->'trigger'->>'task_id'=$2)
 SELECT jsonb_build_object(
 'executions',(SELECT COALESCE(jsonb_agg(jsonb_build_object('status',status,'purpose',purpose)),'[]'::jsonb) FROM selected),
 'model',(SELECT COALESCE(jsonb_agg(jsonb_build_object('dispatch',dispatch,'status',final_status,'category',error_data->>'category','code',error_data->>'code','usage_source',usage_source)),'[]'::jsonb) FROM agenteam_model.invocations WHERE execution_id::text IN(SELECT id FROM selected)),
 'tools',(SELECT COALESCE(jsonb_agg(jsonb_build_object('phase',phase,'outcome',outcome)),'[]'::jsonb) FROM agenteam_tool.operations WHERE execution_id IN(SELECT id FROM selected)),
 'rounds',(SELECT count(*) FROM agenteam_execution.rounds WHERE execution_id IN(SELECT id FROM selected)),
 'results',(SELECT count(*) FROM agenteam_execution.loop_tool_results WHERE execution_id IN(SELECT id FROM selected)))`, w.app.a.String(), w.task.ID.String()).Scan(&facts)
	if err != nil {
		t.Logf("M1 live safe diagnostic unavailable error_type=%T budget_done=%t", err, ctx.Err() != nil)
		return
	}
	t.Logf("M1 live safe durable diagnostic=%s", facts)
}

type m1LiveRound struct {
	fields   ec.LoopRoundFields
	response mc.ModelResponse
	pending  bool
}

func (w *m1Workflow) checkLiveArtifacts(t *testing.T) {
	t.Helper()
	v := w.app
	input := m1LiveArtifact(t, filepath.Join(w.workspace, "input.json"), 64<<10)
	if !bytes.Equal(input, []byte("{\"values\":[2,4,6,8]}\n")) {
		t.Fatal("M1 live work changed its original input")
	}
	program := m1LiveArtifact(t, filepath.Join(w.workspace, "stats.py"), 1<<20)
	if !utf8.Valid(program) || len(bytes.TrimSpace(program)) == 0 {
		t.Fatal("M1 live generated program is empty or invalid UTF-8")
	}
	result := m1LiveArtifact(t, filepath.Join(w.workspace, "result.json"), 64<<10)
	if !m1LiveStatistics(result) {
		t.Fatal("M1 live result.json does not contain the correct count, sum and mean")
	}
	rounds := w.liveRounds(t)
	w.checkLiveCalls(t, rounds)
	w.checkLiveTools(t, rounds, program)
	var content []byte
	var round string
	var originalTx bool
	workAppCheck(t, "m1-live-final-transaction", v.admin.QueryRow(v.ctx, `SELECT s.content,r.id,
 e.xmin=r.xmin AND e.xmin=s.xmin AND e.status='succeeded' AND r.terminal_status=e.status AND s.terminal_status=e.status
 AND e.version=r.terminal_version AND e.version=s.terminal_version AND e.completed_at=r.finished_at AND e.completed_at=s.completed_at
 AND r.schema_version=4 AND NOT r.tool_pending AND r.snapshot_id=e.snapshot_id AND s.snapshot_id=e.snapshot_id
 FROM agenteam_execution.executions e JOIN agenteam_execution.runtime_snapshots s ON s.execution_id=e.id
 JOIN agenteam_execution.rounds r ON r.execution_id=e.id AND r.id=s.round_id WHERE e.id=$1`, w.execution.String()).Scan(&content, &round, &originalTx))
	var snapshot ec.RuntimeSnapshot
	workAppCheck(t, "m1-live-durable-projection", json.Unmarshal(content, &snapshot))
	if !originalTx || round != rounds[len(rounds)-1].fields.Round.ID.String() || round != w.view.Snapshot.Items[0].ID.String() || !reflect.DeepEqual(snapshot, *w.view.Snapshot) {
		t.Fatal("M1 live projection lost original final Round/Execution transaction provenance")
	}
}

func m1LiveArtifact(t *testing.T, path string, maximum int64) []byte {
	t.Helper()
	before, err := os.Lstat(path)
	workAppCheck(t, "m1-live-artifact-lstat", err)
	if !before.Mode().IsRegular() || before.Size() < 1 || before.Size() > maximum {
		t.Fatal("M1 live artifact is not a regular file within its observation limit")
	}
	file, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	workAppCheck(t, "m1-live-artifact-open", err)
	defer file.Close()
	opened, err := file.Stat()
	workAppCheck(t, "m1-live-artifact-stat", err)
	if !opened.Mode().IsRegular() || !os.SameFile(before, opened) {
		t.Fatal("M1 live artifact changed identity during observation")
	}
	raw, err := io.ReadAll(io.LimitReader(file, maximum+1))
	workAppCheck(t, "m1-live-artifact-read", err)
	if int64(len(raw)) > maximum || int64(len(raw)) != opened.Size() {
		t.Fatal("M1 live artifact changed size or exceeded its observation limit")
	}
	return raw
}

func (w *m1Workflow) liveRounds(t *testing.T) []m1LiveRound {
	t.Helper()
	v := w.app
	rows, err := v.admin.Query(v.ctx, `SELECT id,call_id,content,response,tool_pending FROM agenteam_execution.rounds WHERE execution_id=$1 ORDER BY round_ordinal`, w.execution.String())
	workAppCheck(t, "m1-live-rounds", err)
	defer rows.Close()
	var rounds []m1LiveRound
	for rows.Next() {
		var item m1LiveRound
		var roundID, callID string
		var content, response []byte
		workAppCheck(t, "m1-live-round-row", rows.Scan(&roundID, &callID, &content, &response, &item.pending))
		decoded, err := ec.DecodeLoopRound(content)
		workAppCheck(t, "m1-live-round-decode", err)
		item.fields = decoded.Fields()
		workAppCheck(t, "m1-live-response-decode", json.Unmarshal(response, &item.response))
		workAppCheck(t, "m1-live-response-contract", item.response.Validate())
		if item.fields.Round.ExecutionID != w.execution || item.fields.Round.ID.String() != roundID || item.fields.Round.CallID.String() != callID || item.response.CallID != item.fields.Round.CallID || int(item.fields.Ordinal) != len(rounds)+1 {
			t.Fatal("M1 live round lost its original Execution, CallID or order")
		}
		if len(rounds) > 0 {
			previous := rounds[len(rounds)-1]
			if !previous.pending || item.fields.Predecessor == nil || *item.fields.Predecessor != previous.fields.Round.ID || item.fields.Round.SnapshotID != previous.fields.Round.SnapshotID || !reflect.DeepEqual(item.fields.Tools, previous.fields.Tools) {
				t.Fatal("M1 live successor lost its predecessor or frozen tools and snapshot")
			}
		}
		rounds = append(rounds, item)
	}
	workAppCheck(t, "m1-live-round-rows", rows.Err())
	rows.Close()
	if len(rounds) < 3 || rounds[len(rounds)-1].pending || string(rounds[len(rounds)-1].response.FinishReason) != "stop" {
		t.Fatal("M1 live workflow lacks multiple tool rounds and a final text round")
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
	for _, required := range []string{w.task.Plan, "input.json", "stats.py", "result.json"} {
		encoded, err := json.Marshal(required)
		workAppCheck(t, "m1-live-task-goal-encoding", err)
		if required == "" || (!strings.Contains(first.String(), required) && !strings.Contains(first.String(), string(encoded))) {
			t.Fatal("M1 live first model input omitted the Task goal or file instructions")
		}
	}
	return rounds
}

// Each actual attempt, including a retried failure, must retain the original
// round and Model snapshot. A successful round has one final successful attempt;
// earlier retryable failures remain visible in the reported counts.
func (w *m1Workflow) checkLiveCalls(t *testing.T, rounds []m1LiveRound) {
	t.Helper()
	v := w.app
	rows, err := v.admin.Query(v.ctx, `SELECT i.id::text,i.call_id::text,i.attempt_index,i.dispatch,COALESCE(i.final_status,''),i.usage_source,
 i.input_data#>>'{value,round_id}',i.identity_data#>>'{value,provider_model_id}',s.snapshot_data->>'Endpoint',
 COALESCE((i.error_data->>'retryable')::boolean,false),COALESCE((i.error_data->>'partial_output')::boolean,false),
	 i.input_tokens,i.output_tokens,i.total_tokens,i.provider_request_id
 FROM agenteam_model.invocations i JOIN agenteam_model.snapshots s ON s.id=i.snapshot_id AND s.project_id=i.project_id
 WHERE i.execution_id=$1::uuid AND i.project_id=$2::uuid ORDER BY i.call_id,i.attempt_index`, w.execution.String(), v.a.String())
	workAppCheck(t, "m1-live-model-attempts", err)
	defer rows.Close()
	byCall := make(map[string]m1LiveRound, len(rounds))
	for _, round := range rounds {
		byCall[round.fields.Round.CallID.String()] = round
	}
	attempts := make(map[string]int)
	succeeded := make(map[string]bool)
	sent, retries := 0, 0
	for rows.Next() {
		var invocation, call, dispatch, status, usage, roundID, model, endpoint, providerRequest string
		var attempt int
		var retryable, partial bool
		var input, output, total *int64
		workAppCheck(t, "m1-live-model-attempt", rows.Scan(&invocation, &call, &attempt, &dispatch, &status, &usage, &roundID, &model, &endpoint, &retryable, &partial, &input, &output, &total, &providerRequest))
		round, ok := byCall[call]
		if !ok || succeeded[call] || attempt != attempts[call]+1 || round.fields.Round.ID.String() != roundID || model != w.live.Model || endpoint != w.live.BaseURL {
			t.Fatal("M1 live Model attempt lost its exact round, retry order or configured provider")
		}
		attempts[call]++
		if dispatch == "sent" {
			sent++
		}
		if attempt > 1 {
			retries++
		}
		switch status {
		case "succeeded":
			u := round.response.Usage
			if dispatch != "sent" || usage != "provider" || input == nil || output == nil || total == nil || *input < 0 || *output < 0 || *total < 0 || round.response.InvocationID.String() != invocation || providerRequest != round.response.ProviderRequestID || u.Source != mc.ProviderUsage || u.InputTokens == nil || u.OutputTokens == nil || u.TotalTokens == nil || int64(*u.InputTokens) != *input || int64(*u.OutputTokens) != *output || int64(*u.TotalTokens) != *total {
				t.Fatal("M1 live successful Model attempt lacks actual provider Usage or original response identity")
			}
			succeeded[call] = true
		case "failed":
			if !retryable || partial || (dispatch != "sent" && dispatch != "not_sent") {
				t.Fatal("M1 live work contains a failed non-retryable or ambiguous Model attempt")
			}
		default:
			t.Fatal("M1 live work contains an unfinished, cancelled or unknown Model attempt")
		}
	}
	workAppCheck(t, "m1-live-model-attempt-rows", rows.Err())
	if len(attempts) != len(rounds) || len(succeeded) != len(rounds) {
		t.Fatal("M1 live round lacks its final original successful Model invocation")
	}
	t.Logf("M1_LIVE_WORK_FACTS rounds=%d sent_attempts=%d retry_attempts=%d provider_usage=true", len(rounds), sent, retries)
}

func (w *m1Workflow) checkLiveTools(t *testing.T, rounds []m1LiveRound, program []byte) {
	t.Helper()
	v := w.app
	rows, err := v.admin.Query(v.ctx, `SELECT c.round_id,c.call_id,c.invocation_id,c.model_visible_name,c.operation_name,a.original,
 o.operation_id,q.attempt_id,t.message,o.outcome,convert_from(o.terminal_data,'UTF8')::jsonb->>'Outcome',convert_from(o.terminal_data,'UTF8')::jsonb->>'Code',
 p.retired AND o.active_attempt_id=p.attempt_id AND o.phase=p.phase AND ((o.phase='succeeded' AND o.outcome='success')
 OR (c.operation_name='run-command' AND ((o.phase='failed' AND o.outcome='error') OR (o.phase='cancelled' AND o.outcome='cancelled'))))
 AND q.terminal_digest='sha256:'||encode(sha256(o.terminal_data),'hex')
 AND q.message_digest=t.digest AND q.message_digest='sha256:'||encode(sha256(t.message),'hex')
	 AND q.result_digest='sha256:'||encode(sha256(convert_to(convert_from(t.message,'UTF8')::jsonb#>>'{parts,0,tool_result,parts,0,text,text}','UTF8')),'hex')
 FROM agenteam_execution.tool_calls c
 JOIN agenteam_execution.tool_call_arguments a ON (a.execution_id,a.round_id,a.id)=(c.execution_id,c.round_id,c.payload_id)
 JOIN agenteam_tool.operations o ON o.execution_id=c.execution_id AND o.invocation_id=c.invocation_id AND o.call_id=c.call_id AND o.logical_call_id=a.logical_call_id AND o.tool_id=c.tool_id AND o.spec_revision=c.spec_revision
 JOIN agenteam_execution.loop_tool_results q ON (q.execution_id,q.round_id,q.operation_id,q.invocation_id,q.call_id)=(c.execution_id,c.round_id,o.operation_id,c.invocation_id,c.call_id)
 JOIN agenteam_tool.attempts p ON (p.attempt_id,p.operation_id,p.execution_id,p.backend_request_id)=(q.attempt_id,q.operation_id,q.execution_id,q.request_id)
 JOIN agenteam_execution.transcript_entries t ON (t.execution_id,t.round_id,t.sequence)=(q.execution_id,q.round_id,q.transcript_sequence)
 WHERE c.execution_id=$1 ORDER BY q.transcript_sequence`, w.execution.String())
	workAppCheck(t, "m1-live-tool-pairs", err)
	type pair struct {
		round, call, invocation, name, operation, operationID string
		attemptID                                             string
		outcome, terminalOutcome, terminalCode                string
		arguments, message                                    []byte
		valid                                                 bool
	}
	var pairs []pair
	for rows.Next() {
		var p pair
		workAppCheck(t, "m1-live-tool-pair", rows.Scan(&p.round, &p.call, &p.invocation, &p.name, &p.operation, &p.arguments, &p.operationID, &p.attemptID, &p.message, &p.outcome, &p.terminalOutcome, &p.terminalCode, &p.valid))
		pairs = append(pairs, p)
	}
	workAppCheck(t, "m1-live-tool-pair-rows", rows.Err())
	rows.Close()
	var calls, operations, results int
	workAppCheck(t, "m1-live-tool-counts", v.admin.QueryRow(v.ctx, `SELECT
 (SELECT count(*) FROM agenteam_execution.tool_calls WHERE execution_id=$1),
 (SELECT count(*) FROM agenteam_tool.operations WHERE execution_id=$1),
 (SELECT count(*) FROM agenteam_execution.loop_tool_results WHERE execution_id=$1)`, w.execution.String()).Scan(&calls, &operations, &results))
	if len(pairs) < 2 || calls != len(pairs) || operations != len(pairs) || results != len(pairs) || len(rounds) != len(pairs)+1 {
		t.Fatal("M1 live Tool calls, operations, consumed results and successor rounds are not one-to-one")
	}
	readInput, wroteProgram, ranProgram, readResult := false, false, false, false
	knownCommandFailures := 0
	for n, p := range pairs {
		round := rounds[n]
		if !p.valid || !round.pending || p.round != round.fields.Round.ID.String() || p.invocation != round.response.InvocationID.String() {
			t.Fatal("M1 live Tool result lost its original round, known operation or retired attempt")
		}
		var original *mc.ToolCallPart
		for _, part := range round.response.Message.Parts {
			if part.ToolCall != nil {
				if original != nil {
					t.Fatal("M1 live round has unsupported parallel Tool calls")
				}
				original = part.ToolCall
			}
		}
		if original == nil || original.ID != p.call || original.Name != p.name || !m1LiveJSONEqual(original.Arguments, p.arguments) {
			t.Fatal("M1 live Tool arguments differ from the original model response")
		}
		var message mc.Message
		workAppCheck(t, "m1-live-consumed-result", json.Unmarshal(p.message, &message))
		workAppCheck(t, "m1-live-consumed-result-contract", message.Validate())
		if message.Role != "tool" || len(message.Parts) != 1 || message.Parts[0].ToolResult == nil || message.Parts[0].ToolResult.CallID != p.call || message.Parts[0].ToolResult.IsError != (p.outcome != "success") || len(message.Parts[0].ToolResult.Parts) != 1 || message.Parts[0].ToolResult.Parts[0].Text == nil {
			t.Fatal("M1 live consumed result does not belong to its original Tool call and outcome")
		}
		resultBytes := []byte(message.Parts[0].ToolResult.Parts[0].Text.Text)
		wantHistory := append(append([]mc.Message(nil), round.fields.Round.Messages...), round.response.Message, message)
		if !reflect.DeepEqual(wantHistory, rounds[n+1].fields.Round.Messages) {
			t.Fatal("M1 live successor input differs from the actual prior model response and Tool result")
		}
		var arguments struct {
			MountID    string   `json:"mount_id"`
			Path       string   `json:"path"`
			DataBase64 string   `json:"data_base64"`
			Mode       string   `json:"mode"`
			Program    string   `json:"program"`
			Args       []string `json:"args"`
			CWD        string   `json:"cwd"`
		}
		workAppCheck(t, "m1-live-tool-arguments", json.Unmarshal(p.arguments, &arguments))
		if arguments.MountID != w.mount.String() {
			t.Fatal("M1 live work Tool escaped its actual captured workspace mount")
		}
		switch p.operation {
		case "read-file":
			read, err := rp.DecodeReadFileResult(resultBytes)
			workAppCheck(t, "m1-live-read-result", err)
			data := []byte(read.Data)
			if read.Encoding == "base64" {
				data, err = base64.StdEncoding.Strict().DecodeString(read.Data)
				workAppCheck(t, "m1-live-read-bytes", err)
			}
			complete := read.Path == arguments.Path && read.Offset == 0 && read.EOF
			readInput = readInput || complete && arguments.Path == "input.json" && m1LiveJSONEqual(data, []byte(`{"values":[2,4,6,8]}`))
			readResult = readResult || ranProgram && complete && arguments.Path == "result.json" && m1LiveStatistics(data)
		case "write-file":
			written, err := rp.DecodeWriteFileResult(resultBytes)
			workAppCheck(t, "m1-live-write-result", err)
			if arguments.Path == "stats.py" {
				data, err := base64.StdEncoding.Strict().DecodeString(arguments.DataBase64)
				workAppCheck(t, "m1-live-written-program", err)
				wroteProgram = bytes.Equal(data, program) && written.Path == "stats.py" && written.BytesWritten == len(program) && written.State == "committed" && written.CompletionCode == nil
				// A new program must earn its own real successful run and read.
				ranProgram, readResult = false, false
			}
		case "run-command":
			if arguments.Mode != "argv" || arguments.Program != "/usr/bin/python3" || len(arguments.Args) != 1 || arguments.Args[0] != "stats.py" || arguments.CWD != "." {
				t.Fatal("M1 live command did not directly execute the generated stats.py in its workspace")
			}
			var report, stdout, stderr []byte
			workAppCheck(t, "m1-live-command-result", v.admin.QueryRow(v.ctx, `SELECT body_data,stdout_data,stderr_data FROM agenteam_tool.command_results WHERE execution_id=$1 AND operation_id=$2 AND attempt_id=$3`, w.execution.String(), p.operationID, p.attemptID).Scan(&report, &stdout, &stderr))
			decoded, err := rp.DecodeRunCommandResult(report)
			workAppCheck(t, "m1-live-command-report", err)
			bundle, err := tc.DecodeRunCommandResultBundle(v.ctx, resultBytes)
			workAppCheck(t, "m1-live-command-consumed-result", err)
			if !reflect.DeepEqual(bundle.Report, decoded) || !bytes.Equal(bundle.Stdout, stdout) || !bytes.Equal(bundle.Stderr, stderr) {
				t.Fatal("M1 live command's consumed result differs from its original report and output streams")
			}
			classified, code := tc.ClassifyRunCommandResult(decoded)
			if !tc.ValidRunCommandResultClassification(classified, code) || classified != p.terminalOutcome || code != p.terminalCode || (classified == "success") != (p.outcome == "success") || (classified == "cancelled") != (p.outcome == "cancelled") {
				t.Fatal("M1 live command report differs from its original known terminal classification")
			}
			if classified != "success" {
				knownCommandFailures++
			}
			// Known failures remain real error results in the next model input;
			// they do not earn the final artifact-success assertion.
			ranProgram = wroteProgram && classified == "success" && decoded.ExitCode == 0 && decoded.Signal == 0 && !decoded.Cancelled && !decoded.TimedOut && decoded.OutputComplete && decoded.ProcessGroupTerminationConfirmed && !decoded.StdoutTruncated && !decoded.StderrTruncated && decoded.CompletionCode == nil && m1LiveStatistics(stdout) && len(stderr) == 0
			readResult = false
		default:
			t.Fatal("M1 live workflow used a Tool outside its read/write/run scope")
		}
	}
	if !readInput || !wroteProgram || !ranProgram || !readResult {
		t.Fatal("M1 live workflow lacks an actual input read, final program write, successful final run or subsequent result read")
	}
	t.Logf("M1_LIVE_TOOL_FACTS tool_rounds=%d known_command_failures=%d final_program_run_succeeded=true", len(pairs), knownCommandFailures)
}

func m1LiveJSONEqual(a, b []byte) bool {
	x, xe := cursor.CanonicalJSON(a)
	y, ye := cursor.CanonicalJSON(b)
	return xe == nil && ye == nil && bytes.Equal(x, y)
}

// Called after the real runtime Drain while Store remains available. Review
// receipt generation is separately attributed; it never implies approval.
func (w *m1Workflow) checkLiveFinalFacts(t *testing.T) {
	t.Helper()
	v := w.app
	rows, err := v.admin.Query(v.ctx, `SELECT id,agent_id,launch_request->>'purpose',status FROM agenteam_execution.executions WHERE project_id=$1 AND launch_request->'trigger'->>'task_id'=$2 ORDER BY created_at,id`, v.a.String(), w.task.ID.String())
	workAppCheck(t, "m1-live-final-task-scope", err)
	work, review := 0, 0
	for rows.Next() {
		var execution, agent, purpose, status string
		workAppCheck(t, "m1-live-final-execution", rows.Scan(&execution, &agent, &purpose, &status))
		switch purpose {
		case "task/work":
			work++
			if execution != w.execution.String() || agent != v.agent.String() || status != string(ec.Succeeded) {
				t.Fatal("M1 live Owner window acquired a second or mismatched work Execution")
			}
		case "task/review":
			review++
			if agent != w.reviewer.String() || (status != string(ec.Succeeded) && status != string(ec.Cancelled)) {
				t.Fatal("M1 live review Execution has a mismatched reviewer or failed to join normally")
			}
		default:
			t.Fatal("M1 live Task acquired an unrelated Execution purpose")
		}
	}
	workAppCheck(t, "m1-live-final-execution-rows", rows.Err())
	rows.Close()
	if work != 1 || review > 1 {
		t.Fatalf("M1 live bounded Owner window work_executions=%d review_executions=%d", work, review)
	}
	var sentWork, sentReview, retries, failed, cancelled, unknown, active int
	workAppCheck(t, "m1-live-all-attempts", v.admin.QueryRow(v.ctx, `SELECT
 count(*) FILTER(WHERE i.dispatch='sent' AND e.launch_request->>'purpose'='task/work'),
 count(*) FILTER(WHERE i.dispatch='sent' AND e.launch_request->>'purpose'='task/review'),
 count(*) FILTER(WHERE i.attempt_index>1),count(*) FILTER(WHERE i.final_status='failed'),
 count(*) FILTER(WHERE i.final_status='cancelled'),count(*) FILTER(WHERE i.final_status='unknown' OR i.dispatch='unknown'),
 count(*) FILTER(WHERE i.final_status IS NULL)
 FROM agenteam_model.invocations i JOIN agenteam_execution.executions e ON e.id=i.execution_id::text
 WHERE e.project_id=$1 AND e.launch_request->'trigger'->>'task_id'=$2`, v.a.String(), w.task.ID.String()).Scan(&sentWork, &sentReview, &retries, &failed, &cancelled, &unknown, &active))
	if unknown != 0 || active != 0 {
		t.Fatalf("M1 live Model owners left unknown=%d active=%d attempts after Drain", unknown, active)
	}
	if sentWork+sentReview >= m1LiveSentThreshold {
		t.Fatalf("M1 live reached its soft sent-attempt stop threshold after Drain: work=%d review=%d threshold=%d", sentWork, sentReview, m1LiveSentThreshold)
	}
	var invalidReview int
	workAppCheck(t, "m1-live-review-model-attribution", v.admin.QueryRow(v.ctx, `SELECT count(*) FROM agenteam_model.invocations i
 JOIN agenteam_execution.executions e ON e.id=i.execution_id::text
 LEFT JOIN agenteam_model.snapshots s ON s.id=i.snapshot_id AND s.project_id=i.project_id
 LEFT JOIN agenteam_execution.rounds r ON r.execution_id=e.id AND r.call_id=i.call_id::text AND r.id=i.input_data#>>'{value,round_id}'
 WHERE e.project_id=$1 AND e.launch_request->'trigger'->>'task_id'=$2 AND e.launch_request->>'purpose'='task/review'
 AND (i.project_id::text=$1 AND i.agent_id::text=$3 AND i.historical_model_id::text=$4
 AND i.identity_data#>>'{value,provider_model_id}'=$5 AND s.snapshot_data->>'Endpoint'=$6
 AND r.id IS NOT NULL AND i.purpose='agent_generation' AND i.consumer_kind='agent'
 AND (i.final_status<>'succeeded' OR (i.dispatch='sent' AND i.usage_source='provider'))
 AND (i.final_status<>'failed' OR ((i.error_data->>'retryable')::boolean AND NOT (i.error_data->>'partial_output')::boolean))) IS NOT TRUE`, v.a.String(), w.task.ID.String(), w.reviewer.String(), w.reviewModel.String(), w.live.Model, w.live.BaseURL).Scan(&invalidReview))
	if invalidReview != 0 {
		t.Fatalf("M1 live review has mismatched Model/snapshot/round attribution or non-retryable failure count=%d", invalidReview)
	}
	w.checkLiveArtifacts(t)
	t.Logf("M1_LIVE_FINAL_FACTS work_executions=%d review_executions=%d sent_work=%d sent_review=%d sent_total=%d retry_attempts=%d failed_attempts=%d cancelled_attempts=%d review_decision=false", work, review, sentWork, sentReview, sentWork+sentReview, retries, failed, cancelled)
}
