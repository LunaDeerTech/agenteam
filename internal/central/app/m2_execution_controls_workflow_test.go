//go:build integration && linux

package app

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"syscall"
	"testing"
	"time"

	ac "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	gc "github.com/LunaDeerTech/agenteam/internal/central/governance/approval/contract"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
	wc "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
	netfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/outbound"
	"github.com/jackc/pgx/v5/pgconn"
	"golang.org/x/sys/unix"
)

// The original App remains running until the public cancellation has stopped
// its actual command child and committed the terminal. Runtime.Stop is only
// cleanup, never the producer of the cancellation under test.
func TestM2ExecutionCancellationWorkflow(t *testing.T) {
	for _, wholeTask := range []bool{false, true} {
		name := "execution"
		if wholeTask {
			name = "task"
		}
		t.Run(name, func(t *testing.T) {
			checkpoint, release := make(chan struct{}), make(chan struct{})
			var selected, released sync.Once
			unblock := func() { released.Do(func() { close(release) }) }
			observer := func(cause f.TransactionCause, result f.CommitResult) {
				if cause.Details().JobType == "execution-tool-call-checkpoint" && result.State() == f.Committed {
					selected.Do(func() { close(checkpoint); <-release })
				}
			}
			w, _ := newM1WorkflowWithObserver(t, observer)
			t.Cleanup(unblock)
			w.workerApprovalPolicy = ac.ApprovalAllow
			w.seed(t)
			w.startRunner(t)
			w.responseSequence = func(t *testing.T) []netfixture.WireScenario {
				workAppCheck(t, "m2-cancel-owned-program", os.WriteFile(filepath.Join(w.workspace, "m1-cancel.py"), []byte(m1CancelProgram), 0600))
				args := map[string]any{"mount_id": w.mount.String(), "mode": "argv", "program": "/usr/bin/python3", "args": []string{"m1-cancel.py"}, "cwd": "."}
				return []netfixture.WireScenario{m1FailureResponse(t, w, tc.RunnerRunCommandKey, args), {Status: http.StatusOK, Headers: map[string]string{"Content-Type": "application/json"}, Chunks: [][]byte{[]byte(`{"id":"unexpected-successor","object":"chat.completion","created":1,"model":"m1-statistics","choices":[{"index":0,"message":{"role":"assistant","content":"unexpected successor"},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":10,"total_tokens":20}}`)}}}
			}
			w.createModelAndTask(t, "https://fixture.test:8443/m2-cancel-not-yet-scheduled")
			w.app.store.releaseDirectory()
			select {
			case <-checkpoint:
			case <-time.After(30 * time.Second):
				w.diagnoseResult(t, 0, "", "m2-cancel-checkpoint")
				t.Fatal("original command checkpoint did not commit")
			}
			var execution string
			workAppCheck(t, "m2-cancel-original-execution", w.app.admin.QueryRow(w.app.ctx, `SELECT id FROM agenteam_execution.executions WHERE project_id=$1 AND launch_request->'trigger'->>'task_id'=$2`, w.app.a.String(), w.task.ID.String()).Scan(&execution))
			var err error
			w.execution, err = f.ParseID[id.Execution](execution)
			workAppCheck(t, "m2-cancel-execution-id", err)
			before := w.failurePending(t)
			w.failureLease(t, false)
			unblock()
			child, fd := w.awaitCancelChild(t)
			defer unix.Close(fd)
			var running bool
			workAppCheck(t, "m2-cancel-original-running-attempt", w.app.admin.QueryRow(w.app.ctx, `SELECT o.phase='running' AND a.phase='running' AND NOT a.retired FROM agenteam_tool.operations o JOIN agenteam_tool.attempts a ON a.operation_id=o.operation_id AND a.attempt_id=o.active_attempt_id WHERE o.execution_id=$1`, execution).Scan(&running))
			if !running {
				t.Fatal("child did not belong to the original running Attempt")
			}
			m2TaskRequest(t, w, http.MethodGet, "/tasks/"+w.task.ID.String(), nil, "", &w.task)
			if w.task.State != wc.TaskStateInProgress {
				t.Fatal("original Task was not running")
			}
			var current struct {
				Execution ec.TaskExecution `json:"execution"`
			}
			path := "/tasks/" + w.task.ID.String() + "/executions/" + execution
			m2TaskRequest(t, w, http.MethodGet, path, nil, "", &current)
			if current.Execution.ID != w.execution || current.Execution.Status != ec.Running || current.Execution.CancelRequestedAt != nil {
				t.Fatal("cancellation did not start from the current running Execution")
			}
			var receipt *ec.CancelReceipt
			if wholeTask {
				m2CancelWholeTask(t, w)
			} else {
				r := m2CancelExecution(t, w, path, current.Execution)
				receipt = &r
			}
			w.awaitFailure(t, ec.Cancelled)
			poll := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN}}
			n, err := unix.Poll(poll, 0)
			workAppCheck(t, "m2-cancel-child-exit", err)
			if n != 1 || poll[0].Revents&unix.POLLIN == 0 || !errors.Is(syscall.Kill(child.PID, 0), syscall.ESRCH) || !errors.Is(syscall.Kill(-child.Group, 0), syscall.ESRCH) {
				t.Fatal("user cancellation published terminal before its original child/group exited")
			}
			if !reflect.DeepEqual(before, w.failurePending(t)) {
				t.Fatal("cancellation changed original Model/Tool call identity or arguments")
			}
			w.checkFailureTerminal(t, ec.Cancelled, "cancelled", true)
			w.failureLease(t, true)
			m2CheckCancelledTask(t, w, wholeTask, receipt)
			w.checkFailurePeer(t)
			m2StopApproval(t, w, 1)
			t.Logf("M2_USER_CANCEL target=%s original_child_joined_before_runtime_stop=true original_operation_preserved=true task_semantics_verified=true", name)
		})
	}
}

func m2CancelExecution(t *testing.T, w *m1Workflow, path string, before ec.TaskExecution) ec.CancelReceipt {
	t.Helper()
	reason := "  Stop this execution; preserve the Task for review.  "
	body := map[string]any{"expected_version": before.Version, "expected_task_version": w.task.Version, "reason": reason}
	key := guardID[struct{}](t).String()
	var result ec.CancelResult
	m2TaskRequest(t, w, http.MethodPost, path+"/cancel", body, key, &result)
	r := result.Receipt
	if result.Replayed || r.Execution.ID != before.ID || r.Execution.Version != before.Version+1 || r.Execution.Status != before.Status || r.Execution.CancelRequestedAt == nil || r.Task.ID != w.task.ID.String() || r.Task.Version != w.task.Version+1 || r.Task.Validate() != nil {
		t.Fatal("accepted cancellation receipt did not bind original Execution and Task protection")
	}
	var replay ec.CancelResult
	m2TaskRequest(t, w, http.MethodPost, path+"/cancel", body, key, &replay)
	if !replay.Replayed || !reflect.DeepEqual(replay.Receipt, r) {
		t.Fatal("cancellation replay changed the original accepted receipt")
	}
	var lookup ec.CancelLookup
	m2TaskRequest(t, w, http.MethodPost, path+"/cancel/lookup", body, key, &lookup)
	if !lookup.Found || lookup.Receipt == nil || !reflect.DeepEqual(*lookup.Receipt, r) {
		t.Fatal("cancellation lookup did not preserve the original command")
	}
	return r
}

// A second same-process Advance observes the original live owner before
// cancellation. Only a real old-process exit precedes the new cold App.
func TestM2ExecutionWaitingCancellationRecovery(t *testing.T) {
	w := m2PrepareApproval(t, nil)
	taskExecutionJoinEnabled(t, w.app)
	first := m2StartRecoveryChildMode(t, w, "observe-owner")
	pending := m2AwaitApproval(t, w)
	before := w.failurePending(t)
	m2RecoveryEvent(t, first, "m2_same_owner_advance_return", nil)
	m2RecoveryGuardHeld(t, w, first.process)
	m2CheckUndispatchedApproval(t, w, pending, ec.Waiting)
	if !reflect.DeepEqual(before, w.failurePending(t)) {
		t.Fatal("same-process repeated Advance rewrote the original checkpoint")
	}
	m2TaskRequest(t, w, http.MethodGet, "/tasks/"+w.task.ID.String(), nil, "", &w.task)
	path := "/tasks/" + w.task.ID.String() + "/executions/" + w.execution.String()
	var current struct {
		Execution ec.TaskExecution `json:"execution"`
	}
	m2TaskRequest(t, w, http.MethodGet, path, nil, "", &current)
	if current.Execution.Status != ec.Waiting || current.Execution.WaitingReference == nil || *current.Execution.WaitingReference != pending.ID.String() {
		t.Fatal("waiting cancellation lost the original approval")
	}
	receipt := m2CancelExecution(t, w, path, current.Execution)
	w.awaitFailure(t, ec.Cancelled)
	m2CheckUndispatchedApproval(t, w, pending, ec.Cancelled)
	m2CheckCancelledTask(t, w, false, &receipt)
	w.failureLease(t, true)
	m2KillRecoveryChild(t, w, first)
	second := m2StartRecoveryChild(t, w, false)
	if second.process == first.process {
		t.Fatal("cancelled restart reused a prior process identity")
	}
	m2RecoveryEvent(t, second, "m2_cancelled_observed", nil)
	m2CheckUndispatchedApproval(t, w, pending, ec.Cancelled)
	m2CheckCancelledTask(t, w, false, &receipt)
	var problem struct {
		Code f.Code `json:"code"`
	}
	m2ApprovalRequest(t, w, pending.ID, "decision", guardID[struct{}](t).String(), gc.Decision{Decision: "approve", ExpectedVersion: pending.Version}, http.StatusConflict, &problem)
	if problem.Code != f.ConfirmationStale {
		t.Fatal("late approval did not reject the cancelled original Execution")
	}
	m2CheckUndispatchedApproval(t, w, pending, ec.Cancelled)
	if !reflect.DeepEqual(before, w.failurePending(t)) {
		t.Fatal("cancelled restart changed original Model/Tool checkpoint bytes")
	}
	m2StopRecoveryChild(t, w, second)
	w.joinedPeer(t, w.scenario, 1, 1)
	m2StopRecoveryRunner(t, w)
	t.Log("M2_WAITING_CANCEL same_process_original_owner_reused=true old_guard_held=true zero_attempts_effects=true cancelled_restart_no_resurrection=true late_approve_rejected=true")
}

func m2CheckUndispatchedApproval(t *testing.T, w *m1Workflow, pending gc.Request, status ec.Status) {
	t.Helper()
	var exact bool
	var operations, attempts, models, commands, grants, results int
	workAppCheck(t, "m2-undispatched-original-facts", w.app.admin.QueryRow(w.app.ctx, `SELECT
 e.status=$2 AND ((e.status='waiting' AND e.waiting_reference=r.id AND e.cancel_requested_at IS NULL) OR (e.status='cancelled' AND e.waiting_reference IS NULL AND e.waiting_reason IS NULL AND e.waiting_at IS NULL AND e.cancel_requested_at IS NOT NULL)),
 (SELECT count(*) FROM agenteam_tool.operations WHERE execution_id=e.id),
 (SELECT count(*) FROM agenteam_tool.attempts WHERE execution_id=e.id),
 (SELECT count(*) FROM agenteam_model.invocations WHERE execution_id=e.id::uuid),
 (SELECT count(*) FROM agenteam_governance.approval_commands WHERE request_id=r.id),
 (SELECT count(*) FROM agenteam_governance.one_time_approvals WHERE request_id=r.id),
 (SELECT count(*) FROM agenteam_tool.command_results WHERE execution_id=e.id)
 FROM agenteam_execution.executions e JOIN agenteam_governance.approval_requests r ON r.execution_id=e.id JOIN agenteam_tool.operations o ON o.operation_id=r.operation_id
 WHERE e.id=$1 AND r.id=$3 AND r.status='pending' AND r.version=1 AND o.operation_id=$4 AND o.fingerprint=$5 AND o.phase='created' AND o.version=1 AND o.active_attempt_id IS NULL`, w.execution.String(), string(status), pending.ID.String(), pending.OperationID.String(), string(pending.Fingerprint)).Scan(&exact, &operations, &attempts, &models, &commands, &grants, &results))
	if !exact || operations != 1 || attempts != 0 || models != 1 || commands != 0 || grants != 0 || results != 0 {
		t.Fatal("pending/cancelled approval changed its original operation or dispatched a side effect")
	}
	if _, err := os.Stat(filepath.Join(w.workspace, "m2-effect.txt")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("undispatched approval produced a filesystem effect")
	}
}

func m2CancelWholeTask(t *testing.T, w *m1Workflow) {
	t.Helper()
	before := w.task
	reason := "Cancel this entire Task."
	request := wc.TaskTransfer{TargetState: wc.TaskStateCancelled, Comment: &reason, AddBlockers: []wc.TaskBlockerCreate{}, ResolveBlockerIDs: []wc.TaskBlockerID{}}
	body := map[string]any{"expected_version": before.Version, "request": request}
	key := guardID[struct{}](t).String()
	var result wc.TaskTransitionMutation
	path := "/tasks/" + before.ID.String() + "/transfer"
	m2TaskRequest(t, w, http.MethodPost, path, body, key, &result)
	if result.Validate() != nil || result.Task.ID != before.ID || result.Task.Version != before.Version+1 || result.Task.State != wc.TaskStateCancelled {
		t.Fatal("whole Task cancellation receipt differs from original intent")
	}
	var replay wc.TaskTransitionMutation
	m2TaskRequest(t, w, http.MethodPost, path, body, key, &replay)
	if !reflect.DeepEqual(replay, result) {
		t.Fatal("whole Task cancellation changed its original receipt")
	}
	var lookup wc.TaskTransitionLookup
	body["command"], body["target_id"] = wc.TaskTransitionTransfer, before.ID
	m2TaskRequest(t, w, http.MethodPost, "/task-transition-commands/lookup", body, key, &lookup)
	if lookup.Status != wc.LookupCommitted || lookup.Receipt == nil || !reflect.DeepEqual(*lookup.Receipt, result) {
		t.Fatal("whole Task cancellation lookup lost its original receipt")
	}
}

func m2CheckCancelledTask(t *testing.T, w *m1Workflow, wholeTask bool, receipt *ec.CancelReceipt) {
	t.Helper()
	var task wc.Task
	m2TaskRequest(t, w, http.MethodGet, "/tasks/"+w.task.ID.String(), nil, "", &task)
	want := wc.TaskStateBlocked
	if wholeTask {
		want = wc.TaskStateCancelled
	}
	if task.State != want || task.Version != w.task.Version+1 {
		t.Fatal("Execution cancellation and whole Task cancellation lost their distinct states")
	}
	var blockers f.Page[wc.TaskBlocker]
	m2TaskRequest(t, w, http.MethodGet, "/tasks/"+w.task.ID.String()+"/blockers?limit=10", nil, "", &blockers)
	if wholeTask {
		if len(blockers.Items) != 0 {
			t.Fatal("whole Task cancellation created an Execution protection blocker")
		}
	} else {
		if receipt == nil || len(blockers.Items) != 1 {
			t.Fatal("Execution cancellation did not protect the original Task exactly once")
		}
		b := blockers.Items[0]
		if b.Validate() != nil || b.ID.String() != receipt.Task.BlockerID || b.Type != wc.TaskBlockerUserCancelledExecution || b.ResolvedAt != nil || b.ExecutionCancellation == nil || b.ExecutionCancellation.ExecutionID != w.execution || b.ExecutionCancellation.CancelledBy.String() != w.app.actor.Details().UserID || b.ExecutionCancellation.Reason == nil || *b.ExecutionCancellation.Reason != "  Stop this execution; preserve the Task for review.  " {
			t.Fatal("Task blocker lost exact cancellation provenance")
		}
		var atomic bool
		workAppCheck(t, "m2-cancel-protection-transaction", w.app.admin.QueryRow(w.app.ctx, `SELECT c.xmin=t.xmin AND c.xmin=b.xmin FROM agenteam_execution.cancel_commands c JOIN agenteam_work.tasks t ON t.id::text=c.task_id JOIN agenteam_work.task_blockers b ON b.id::text=$2 AND b.task_id=t.id WHERE c.execution_id=$1`, w.execution.String(), receipt.Task.BlockerID).Scan(&atomic))
		if !atomic {
			t.Fatal("cancellation receipt and Task protection were not committed together")
		}
	}
	var executions, commands int
	var requested bool
	workAppCheck(t, "m2-cancel-no-resurrection", w.app.admin.QueryRow(w.app.ctx, `SELECT (SELECT count(*) FROM agenteam_execution.executions WHERE project_id=$1 AND launch_request->'trigger'->>'task_id'=$2),(SELECT count(*) FROM agenteam_execution.cancel_commands WHERE execution_id=$3),cancel_requested_at IS NOT NULL FROM agenteam_execution.executions WHERE id=$3`, w.app.a.String(), w.task.ID.String(), w.execution.String()).Scan(&executions, &commands, &requested))
	wantCommands := 1
	if wholeTask {
		wantCommands = 0
	}
	if executions != 1 || commands != wantCommands || !requested {
		t.Fatal("cancellation duplicated execution/command or lacked a durable request")
	}
}

// A partial or mismatched approval-consumption row must not become a tool
// result. Exercise the real deferred PostgreSQL guard, then finish the real
// pending approval through the unchanged Owner/Execution path.
func TestM2ApprovalRejectsUnpairedResult(t *testing.T) {
	w, pending := m2StartApproval(t, nil)
	tx, err := w.app.admin.Begin(w.app.ctx)
	workAppCheck(t, "m2-rejection-pair-tx", err)
	defer tx.Rollback(w.app.ctx)
	tag, err := tx.Exec(w.app.ctx, `INSERT INTO agenteam_execution.loop_approval_results(execution_id,round_id,approval_id,operation_id,fingerprint,call_id,message_digest,transcript_sequence,checkpoint_version,created_at)
 SELECT e.id,r.id,$2,a.operation_id,a.fingerprint,a.tool_call_id,'sha256:'||repeat('0',64),r.transcript_through+1,e.version+1,clock_timestamp()
 FROM agenteam_execution.executions e JOIN agenteam_execution.rounds r ON r.execution_id=e.id JOIN agenteam_governance.approval_requests a ON a.execution_id=e.id AND a.round_id=r.id WHERE e.id=$1 AND a.id=$3 AND e.status='waiting' AND a.status='pending'`, w.execution.String(), guardID[gc.RequestIdentity](t).String(), pending.ID.String())
	workAppCheck(t, "m2-rejection-unpaired-row", err)
	if tag.RowsAffected() != 1 {
		t.Fatal("negative case did not reach the original pending round")
	}
	err = tx.Commit(w.app.ctx)
	var pg *pgconn.PgError
	if !errors.As(err, &pg) || pg.Code != "23514" || pg.Message != "invalid loop approval result pair" {
		t.Fatal("unpaired rejection did not fail at the deferred approval provenance guard")
	}
	var count int
	workAppCheck(t, "m2-rejection-negative-rollback", w.app.admin.QueryRow(w.app.ctx, `SELECT count(*) FROM agenteam_execution.loop_approval_results WHERE execution_id=$1`, w.execution.String()).Scan(&count))
	if count != 0 {
		t.Fatal("failed rejection provenance transaction left a consumed result")
	}
	var response struct {
		Receipt gc.Receipt `json:"receipt"`
	}
	m2ApprovalRequest(t, w, pending.ID, "decision", guardID[struct{}](t).String(), gc.Decision{Decision: "reject", ExpectedVersion: 1}, http.StatusOK, &response)
	w.awaitResult(t)
	m2CheckApprovalTerminal(t, w, pending, gc.Rejected)
	m2StopApproval(t, w, 2)
	t.Log("M2_APPROVAL_PROVENANCE unpaired_request_rejected_by_pg=true rollback_empty=true original_rejection_continued=true")
}
