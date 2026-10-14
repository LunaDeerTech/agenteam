//go:build integration && linux

package app

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	ac "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	gc "github.com/LunaDeerTech/agenteam/internal/central/governance/approval/contract"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
	netfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/outbound"
)

// This reuses the actual installed App/Owner/Scheduler/schema4 Model/Runner
// graph. The owned provider selects one arbitrary command under Default policy;
// the test observes the original waiting Execution and decides via real HTTP.
func TestM2ApprovalWorkflow(t *testing.T) {
	for _, decision := range []string{"approve", "reject"} {
		t.Run(decision, func(t *testing.T) {
			w, pending := m2StartApproval(t, nil)
			before := w.failurePending(t)
			key := guardID[struct{}](t).String()
			body := gc.Decision{Decision: decision, ExpectedVersion: 1}
			var response struct {
				Receipt gc.Receipt `json:"receipt"`
			}
			m2ApprovalRequest(t, w, pending.ID, "decision", key, body, http.StatusOK, &response)
			receipt := response.Receipt
			want := gc.Approved
			if decision == "reject" {
				want = gc.Rejected
			}
			if receipt.CommandID.Validate() != nil || receipt.Approval.Validate() != nil || receipt.Approval.ID != pending.ID || receipt.Approval.OperationID != pending.OperationID || receipt.Approval.Fingerprint != pending.Fingerprint || receipt.Approval.Status != want || receipt.Approval.ResolvedBy.String() != w.app.actor.Details().UserID {
				t.Fatal("original Owner decision lost its request, operation, fingerprint or author")
			}
			// The same command replays its own receipt. A different command
			// cannot claim this decision even after the Execution has resumed.
			m2ApprovalRequest(t, w, pending.ID, "decision", key, body, http.StatusOK, &response)
			if !reflect.DeepEqual(response.Receipt, receipt) {
				t.Fatal("same command returned a different accepted receipt")
			}
			var lookup gc.Lookup
			m2ApprovalRequest(t, w, pending.ID, "decision/lookup", key, body, http.StatusOK, &lookup)
			if lookup.Status != "committed" || lookup.Receipt == nil || !reflect.DeepEqual(*lookup.Receipt, receipt) {
				t.Fatal("original-intent lookup did not return this command receipt")
			}
			w.awaitResult(t)
			if !reflect.DeepEqual(before, w.failurePending(t)) {
				t.Fatal("approval changed the original Model response, arguments or ToolCall")
			}
			var problem struct {
				Code f.Code `json:"code"`
			}
			m2ApprovalRequest(t, w, pending.ID, "decision", guardID[struct{}](t).String(), body, http.StatusConflict, &problem)
			if problem.Code != f.VersionConflict {
				t.Fatalf("resolved different command code=%s", problem.Code.Safe())
			}
			body.Decision = "approve"
			if decision == "approve" {
				body.Decision = "reject"
			}
			m2ApprovalRequest(t, w, pending.ID, "decision", key, body, http.StatusConflict, &problem)
			if problem.Code != f.IdempotencyKeyReused {
				t.Fatal("changed original decision reused its command key")
			}
			m2CheckApprovalTerminal(t, w, pending, want)
			m2StopApproval(t, w, 2)
			t.Logf("M2_APPROVAL decision=%s same_execution=true same_operation=true original_command_receipt=true actual_app_runner_join=true", decision)
		})
	}
}

func m2StartApproval(t *testing.T, observer func(f.TransactionCause, f.CommitResult)) (*m1Workflow, gc.Request) {
	t.Helper()
	w := m2PrepareApproval(t, observer)
	w.app.store.releaseDirectory()
	return w, m2AwaitApproval(t, w)
}

func m2PrepareApproval(t *testing.T, observer func(f.TransactionCause, f.CommitResult)) *m1Workflow {
	t.Helper()
	w, _ := newM1WorkflowWithObserver(t, observer)
	w.workerApprovalPolicy = ac.ApprovalDefault
	w.seed(t)
	w.startRunner(t)
	w.responseSequence = func(t *testing.T) []netfixture.WireScenario {
		workAppCheck(t, "m2-owned-command", os.WriteFile(filepath.Join(w.workspace, "m2-approval.py"), []byte("from pathlib import Path\nwith Path('m2-effect.txt').open('x') as f:\n    f.write('exactly-once\\n')\nprint('approved command completed')\n"), 0600))
		args := map[string]any{"mount_id": w.mount.String(), "mode": "argv", "program": "/usr/bin/python3", "args": []string{"m2-approval.py"}, "cwd": "."}
		last := map[string]any{"id": "owned-m2-final", "object": "chat.completion", "created": 1, "model": "m1-statistics", "choices": []any{map[string]any{"index": 0, "message": map[string]any{"role": "assistant", "content": m1FinalOutput}, "finish_reason": "stop"}}, "usage": map[string]any{"prompt_tokens": 10, "completion_tokens": 10, "total_tokens": 20}}
		return []netfixture.WireScenario{m1FailureResponse(t, w, tc.RunnerRunCommandKey, args), {Status: 200, Headers: map[string]string{"Content-Type": "application/json"}, Chunks: [][]byte{workAppJSON(t, last)}}}
	}
	w.createModelAndTask(t, "https://fixture.test:8443/m2-not-yet-scheduled")
	return w
}

func m2AwaitApproval(t *testing.T, w *m1Workflow) gc.Request {
	t.Helper()
	ctx, cancel := context.WithTimeout(w.app.ctx, 30*time.Second)
	defer cancel()
	tick := time.NewTicker(25 * time.Millisecond)
	defer tick.Stop()
	var execution, status, reference string
	var count int
	for {
		err := w.app.admin.QueryRow(ctx, `SELECT count(*),COALESCE(min(id),''),COALESCE(min(status),''),COALESCE(min(waiting_reference),'') FROM agenteam_execution.executions WHERE project_id=$1 AND launch_request->'trigger'->>'task_id'=$2`, w.app.a.String(), w.task.ID.String()).Scan(&count, &execution, &status, &reference)
		if err != nil {
			w.diagnoseResult(t, count, execution, status)
		}
		workAppCheck(t, "m2-observe-original-wait", err)
		if count > 1 || count == 1 && ec.Status(status).Terminal() {
			w.diagnoseResult(t, count, execution, status)
			t.Fatal("original Execution terminated or duplicated before its approval wait")
		}
		if count == 1 && status == string(ec.Waiting) && reference != "" {
			break
		}
		for _, project := range w.app.runtime.manager.Observation().Projects {
			if project.ProjectID == w.app.a && !project.Running && !project.Retained && !project.Stopping && project.Err != nil {
				w.diagnoseResult(t, count, execution, status)
				t.Fatal("original project runner returned an error before approval waiting")
			}
		}
		select {
		case <-ctx.Done():
			w.diagnoseResult(t, count, execution, status)
			t.Fatal("original command did not persist approval waiting")
		case <-tick.C:
		}
	}
	var err error
	w.execution, err = f.ParseID[id.Execution](execution)
	workAppCheck(t, "m2-original-execution-id", err)
	request, err := f.ParseID[gc.RequestIdentity](reference)
	workAppCheck(t, "m2-original-approval-id", err)
	var got struct {
		Approval gc.Request `json:"approval"`
	}
	m2ApprovalRequest(t, w, request, "", "", nil, http.StatusOK, &got)
	if got.Approval.Validate() != nil || got.Approval.Status != gc.Pending || got.Approval.Version != 1 || got.Approval.ID != request || got.Approval.ExecutionID != w.execution || got.Approval.ToolName != "run-command" {
		t.Fatal("public waiting request differs from the original Execution")
	}
	var operations, attempts int
	var untouched bool
	workAppCheck(t, "m2-waiting-zero-attempt", w.app.admin.QueryRow(ctx, `SELECT (SELECT count(*) FROM agenteam_tool.operations WHERE execution_id=$1), (SELECT count(*) FROM agenteam_tool.attempts WHERE execution_id=$1), o.phase='created' AND o.version=1 AND o.active_attempt_id IS NULL AND o.fingerprint=$3 AND e.waiting_reason='approval' AND e.waiting_at IS NOT NULL AND e.waiting_reference=$4 FROM agenteam_tool.operations o JOIN agenteam_execution.executions e ON e.id=o.execution_id WHERE o.operation_id=$2 AND o.execution_id=$1`, execution, got.Approval.OperationID.String(), string(got.Approval.Fingerprint), reference).Scan(&operations, &attempts, &untouched))
	if operations != 1 || attempts != 0 || !untouched {
		t.Fatal("waiting did not preserve exactly one unattempted original operation")
	}
	if _, err := os.Stat(filepath.Join(w.workspace, "m2-effect.txt")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("command produced an effect before Owner approval")
	}
	return got.Approval
}

func m2ApprovalRequest(t *testing.T, w *m1Workflow, request gc.RequestID, suffix, key string, body any, status int, out any) {
	t.Helper()
	method, path := http.MethodGet, "/api/v1/projects/"+w.app.a.String()+"/approvals/"+request.String()
	var raw []byte
	if suffix != "" {
		method, path, raw = http.MethodPost, path+"/"+suffix, workAppJSON(t, body)
	}
	response, err := w.app.request(w.app.ctx, method, path, raw, key)
	workAppCheck(t, "m2-approval-http-return", err)
	if response.status != status {
		var problem struct {
			Code f.Code `json:"code"`
		}
		_ = json.Unmarshal(response.body, &problem)
		t.Fatalf("M2 Approval HTTP status=%d expected=%d code=%s", response.status, status, problem.Code.Safe())
	}
	if out != nil {
		workAppCheck(t, "m2-approval-http-json", json.Unmarshal(response.body, out))
	}
}

func m2CheckApprovalTerminal(t *testing.T, w *m1Workflow, original gc.Request, status string) {
	t.Helper()
	v := w.app
	var operations, attempts, grants, denials, commands, results, approvalResults, models, reports int
	var exact, sameTx, rejectionExact bool
	var phase string
	workAppCheck(t, "m2-terminal-original-operation", v.admin.QueryRow(v.ctx, `SELECT
 (SELECT count(*) FROM agenteam_tool.operations WHERE execution_id=$1),
 (SELECT count(*) FROM agenteam_tool.attempts WHERE execution_id=$1),
 (SELECT count(*) FROM agenteam_governance.one_time_approvals WHERE request_id=$2),
 (SELECT count(*) FROM agenteam_tool.approval_denials WHERE request_id=$2),
 (SELECT count(*) FROM agenteam_governance.approval_commands WHERE request_id=$2),
 (SELECT count(*) FROM agenteam_execution.loop_tool_results WHERE execution_id=$1),
 (SELECT count(*) FROM agenteam_execution.loop_approval_results WHERE execution_id=$1),
 (SELECT count(*) FROM agenteam_model.invocations WHERE execution_id=$1::uuid),
 (SELECT count(*) FROM agenteam_tool.command_results WHERE execution_id=$1),
 o.operation_id=$3 AND o.fingerprint=$4 AND r.operation_id=o.operation_id AND r.status=$5 AND e.status='succeeded' AND e.waiting_reference IS NULL AND e.waiting_reason IS NULL AND e.waiting_at IS NULL,
 r.xmin=c.xmin AND r.xmin=ev.xmin, o.phase,
 EXISTS(SELECT 1 FROM agenteam_execution.loop_approval_results ar JOIN agenteam_tool.approval_denials d ON d.request_id=ar.approval_id AND d.operation_id=ar.operation_id AND d.fingerprint=ar.fingerprint WHERE ar.execution_id=e.id AND ar.approval_id=r.id AND ar.operation_id=o.operation_id AND ar.fingerprint=o.fingerprint)
 FROM agenteam_governance.approval_requests r JOIN agenteam_tool.operations o ON o.operation_id=r.operation_id JOIN agenteam_execution.executions e ON e.id=r.execution_id JOIN agenteam_governance.approval_commands c ON c.request_id=r.id JOIN agenteam_outbox.events ev ON ev.id=r.resolved_event_id::uuid WHERE r.id=$2 AND r.execution_id=$1`, w.execution.String(), original.ID.String(), original.OperationID.String(), string(original.Fingerprint), status).Scan(&operations, &attempts, &grants, &denials, &commands, &results, &approvalResults, &models, &reports, &exact, &sameTx, &phase, &rejectionExact))
	wantAttempts, wantGrants, wantDenials, wantReports, wantResults, wantApprovalResults, wantPhase := 1, 1, 0, 1, 1, 0, "succeeded"
	if status == gc.Rejected {
		wantAttempts, wantGrants, wantDenials, wantReports, wantResults, wantApprovalResults, wantPhase = 0, 0, 1, 0, 0, 1, "failed"
	}
	if operations != 1 || attempts != wantAttempts || grants != wantGrants || denials != wantDenials || reports != wantReports || phase != wantPhase || commands != 1 || results != wantResults || approvalResults != wantApprovalResults || rejectionExact != (status == gc.Rejected) || models != 2 || !exact || !sameTx {
		t.Fatalf("M2 terminal ops=%d attempts=%d grants=%d denials=%d commands=%d tool_results=%d approval_results=%d models=%d reports=%d phase=%s exact=%t same_tx=%t rejection_exact=%t", operations, attempts, grants, denials, commands, results, approvalResults, models, reports, phase, exact, sameTx, rejectionExact)
	}
	bytes, err := os.ReadFile(filepath.Join(w.workspace, "m2-effect.txt"))
	if status == gc.Approved {
		workAppCheck(t, "m2-approved-actual-effect", err)
		if string(bytes) != "exactly-once\n" {
			t.Fatal("approved original command effect differs")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		t.Fatal("rejected command produced a filesystem effect")
	}
}

func m2StopApproval(t *testing.T, w *m1Workflow, requests int) {
	t.Helper()
	v := w.app
	v.runtime.StopAdmission()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	err := v.runtime.Drain(ctx)
	cancel()
	workAppCheck(t, "m2-original-runtime-drain", err)
	w.stopRunner(t)
	v.requestStop()
	usageAppWait(t, v.directory.returned, "m2-manager-original-return")
	usageAppWait(t, v.root.done, "m2-original-app-return")
	workAppCheck(t, "m2-original-app-exit", v.root.err)
	if !v.runtime.Joined() || !v.runtime.executionCalls.Joined() || !v.runtime.schedulerCalls.Joined() || !v.root.owned.producersJoined() {
		t.Fatal("M2 original App owners did not join")
	}
	taskExecutionGuardReleased(t, v.workAppFixture)
	w.joinedPeer(t, w.scenario, requests, requests)
}
