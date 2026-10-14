//go:build integration && linux

package app

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
	rp "github.com/LunaDeerTech/agenteam/internal/runnerprotocol"
	netfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/outbound"
	"golang.org/x/sys/unix"
)

// These cases use the same default App/Scheduler/Model/Runner graph as the
// successful workflow. Only the owned model peer chooses different tools.
func TestM1AppToolFailureAndCancellation(t *testing.T) {
	for _, cancellation := range []bool{false, true} {
		name := "missing-file"
		if cancellation {
			name = "running-command-cancel"
		}
		t.Run(name, func(t *testing.T) {
			checkpoint, release := make(chan struct{}), make(chan struct{})
			var selected, released sync.Once
			unblock := func() { released.Do(func() { close(release) }) }
			// Hold only AFTER the original checkpoint COMMIT. The producer's
			// original return continues unchanged after the before image is read.
			observer := func(cause f.TransactionCause, result f.CommitResult) {
				if cause.Details().JobType == "execution-tool-call-checkpoint" && result.State() == f.Committed {
					selected.Do(func() { close(checkpoint); <-release })
				}
			}
			w, _ := newM1WorkflowWithObserver(t, observer)
			t.Cleanup(unblock)
			v := w.app
			w.seed(t)
			w.startRunner(t)
			w.responseSequence = func(t *testing.T) []netfixture.WireScenario {
				key := tc.RunnerReadFileKey
				args := map[string]any{"mount_id": w.mount.String(), "path": "m1-missing.txt", "offset": "0", "length": 4096, "encoding": "utf8"}
				if cancellation {
					key = tc.RunnerRunCommandKey
					workAppCheck(t, "m1-cancel-owned-program", os.WriteFile(filepath.Join(w.workspace, "m1-cancel.py"), []byte(m1CancelProgram), 0600))
					args = map[string]any{"mount_id": w.mount.String(), "mode": "argv", "program": "/usr/bin/python3", "args": []string{"m1-cancel.py"}, "cwd": "."}
				}
				// The fixture requires at least two responses. The second is a
				// sentinel: both cases must prove it was never requested.
				return []netfixture.WireScenario{m1FailureResponse(t, w, key, args), {Status: 200, Headers: map[string]string{"Content-Type": "application/json"}, Chunks: [][]byte{[]byte(`{"id":"unexpected-successor","object":"chat.completion","created":1,"model":"m1-statistics","choices":[{"index":0,"message":{"role":"assistant","content":"unexpected successor"},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":10,"total_tokens":20}}`)}}}
			}
			w.createModelAndTask(t, "https://fixture.test:8443/m1-not-yet-scheduled")
			v.store.releaseDirectory()
			select {
			case <-checkpoint:
			case <-time.After(30 * time.Second):
				w.diagnoseResult(t, 0, "", "checkpoint-not-returned")
				t.Fatal("M1 original tool checkpoint did not commit")
			}
			var rawID string
			workAppCheck(t, "m1-failure-original-execution", v.admin.QueryRow(v.ctx, `SELECT id FROM agenteam_execution.executions WHERE project_id=$1 AND launch_request->'trigger'->>'task_id'=$2`, v.a.String(), w.task.ID.String()).Scan(&rawID))
			var err error
			w.execution, err = f.ParseID[id.Execution](rawID)
			workAppCheck(t, "m1-failure-execution-id", err)
			before := w.failurePending(t)
			w.failureLease(t, false)
			unblock()
			status, reason := ec.Failed, "runtime_failed"
			if cancellation {
				status, reason = ec.Cancelled, "cancelled"
				child, fd := w.awaitCancelChild(t)
				defer unix.Close(fd)
				t.Cleanup(func() { v.runtime.StopAdmission() })
				var running bool
				workAppCheck(t, "m1-command-original-start", v.admin.QueryRow(v.ctx, `SELECT o.phase='running' AND a.phase='running' AND NOT a.retired AND a.backend_request_id<>'' FROM agenteam_tool.operations o JOIN agenteam_tool.attempts a ON a.operation_id=o.operation_id AND a.attempt_id=o.active_attempt_id WHERE o.execution_id=$1`, w.execution.String()).Scan(&running))
				if !running {
					t.Fatal("M1 child marker did not correspond to the original running Attempt")
				}
				w.failureLease(t, false)
				// Existing App bundle Stop cancels the actual original Run
				// contexts. Keep DeviceHandler, Store and Runner alive for join.
				v.runtime.StopAdmission()
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				err := v.runtime.Drain(ctx)
				cancel()
				workAppCheck(t, "m1-cancel-original-runtime-drain", err)
				if !v.runtime.Joined() || !v.runtime.executionCalls.Joined() || !v.runtime.schedulerCalls.Joined() {
					t.Fatal("M1 cancelled original Runtime returned before its owners joined")
				}
				poll := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN}}
				n, err := unix.Poll(poll, 0)
				workAppCheck(t, "m1-child-pidfd-exit", err)
				if n != 1 || poll[0].Revents&unix.POLLIN == 0 || !errors.Is(syscall.Kill(child.PID, 0), syscall.ESRCH) || !errors.Is(syscall.Kill(-child.Group, 0), syscall.ESRCH) {
					t.Fatal("M1 original child or process group survived Runtime join")
				}
			}
			w.awaitFailure(t, status)
			if !reflect.DeepEqual(before, w.failurePending(t)) {
				t.Fatal("M1 terminal changed the original pending response, ToolCall or assistant bytes")
			}
			w.checkFailureTerminal(t, status, reason, cancellation)
			w.failureLease(t, true)
			w.stopRunner(t)
			v.requestStop()
			usageAppWait(t, v.directory.returned, "m1-failure-manager-original-return")
			usageAppWait(t, v.root.done, "m1-failure-original-app-return")
			workAppCheck(t, "m1-failure-original-app-exit", v.root.err)
			if !v.runtime.Joined() || !v.root.owned.producersJoined() {
				t.Fatal("M1 failure App returned before original producer join")
			}
			taskExecutionGuardReleased(t, v.workAppFixture)
			w.checkFailurePeer(t)
			t.Logf("M1_APP_TOOL_FAILURE scenario=%s final_status=%s pending_bytes_preserved=true model_requests=1 tool_operations=1 original_app_runner_join=true", name, status)
		})
	}
}

func m1FailureResponse(t *testing.T, w *m1Workflow, key string, args any) netfixture.WireScenario {
	t.Helper()
	raw, err := json.Marshal(args)
	workAppCheck(t, "m1-failure-tool-args", err)
	message := map[string]any{"role": "assistant", "content": nil, "tool_calls": []any{map[string]any{"id": "m1_failure_call", "type": "function", "function": map[string]any{"name": "tool_" + strings.ReplaceAll(w.tools[key].String(), "-", ""), "arguments": string(raw)}}}}
	raw, err = json.Marshal(map[string]any{"id": "owned-m1-failure-response", "object": "chat.completion", "created": 1, "model": "m1-statistics", "choices": []any{map[string]any{"index": 0, "message": message, "finish_reason": "tool_calls"}}, "usage": map[string]any{"prompt_tokens": 10, "completion_tokens": 10, "total_tokens": 20}})
	workAppCheck(t, "m1-failure-model-response", err)
	return netfixture.WireScenario{Status: 200, Headers: map[string]string{"Content-Type": "application/json"}, Chunks: [][]byte{raw}}
}

type m1PendingFacts struct {
	Response, Assistant, Original, Canonical, Round                                     []byte
	ResponseDigest, AssistantDigest, OriginalDigest, CanonicalDigest, Call, RoundDigest string
	Through                                                                             int64
	At                                                                                  time.Time
}

func (w *m1Workflow) failurePending(t *testing.T) m1PendingFacts {
	t.Helper()
	var p m1PendingFacts
	workAppCheck(t, "m1-original-pending-bytes", w.app.admin.QueryRow(w.app.ctx, `SELECT r.response,r.response_digest,r.transcript_through,r.checkpoint_at,t.message,t.digest,a.original,a.original_digest,a.canonical,a.canonical_digest,to_jsonb(c)::text,r.content,r.digest
 FROM agenteam_execution.rounds r JOIN agenteam_execution.tool_calls c ON c.execution_id=r.execution_id AND c.round_id=r.id
 JOIN agenteam_execution.tool_call_arguments a ON a.execution_id=c.execution_id AND a.round_id=c.round_id AND a.id=c.payload_id
 JOIN agenteam_execution.transcript_entries t ON t.execution_id=r.execution_id AND t.round_id=r.id AND t.sequence=2
 WHERE r.execution_id=$1 AND r.schema_version=4 AND r.tool_pending AND r.round_ordinal=1 AND t.kind='assistant' AND t.loop_entry`, w.execution.String()).Scan(&p.Response, &p.ResponseDigest, &p.Through, &p.At, &p.Assistant, &p.AssistantDigest, &p.Original, &p.OriginalDigest, &p.Canonical, &p.CanonicalDigest, &p.Call, &p.Round, &p.RoundDigest))
	if p.Through != 2 || ec.TriggerInputDigest(p.Response).String() != p.ResponseDigest || ec.TriggerInputDigest(p.Assistant).String() != p.AssistantDigest || ec.TriggerInputDigest(p.Original).String() != p.OriginalDigest || ec.TriggerInputDigest(p.Canonical).String() != p.CanonicalDigest || ec.TriggerInputDigest(p.Round).String() != p.RoundDigest {
		t.Fatal("M1 original pending digest or transcript sequence differs")
	}
	return p
}

func (w *m1Workflow) failureLease(t *testing.T, released bool) {
	t.Helper()
	var count int
	var actual bool
	workAppCheck(t, "m1-shared-execution-lease", w.app.admin.QueryRow(w.app.ctx, `SELECT count(*),COALESCE(bool_and(released),false) FROM agenteam_secret.secret_leases WHERE owner_kind='execution' AND owner_id=$1::uuid AND consumer='model'`, w.execution.String()).Scan(&count, &actual))
	if count != 1 || actual != released {
		t.Fatalf("M1 shared Model lease count=%d released=%t expected_released=%t", count, actual, released)
	}
}

func (w *m1Workflow) awaitFailure(t *testing.T, expected ec.Status) {
	t.Helper()
	ctx, cancel := context.WithTimeout(w.app.ctx, 10*time.Second)
	defer cancel()
	tick := time.NewTicker(20 * time.Millisecond)
	defer tick.Stop()
	for {
		var status string
		err := w.app.admin.QueryRow(ctx, `SELECT status FROM agenteam_execution.executions WHERE id=$1`, w.execution.String()).Scan(&status)
		if err != nil || ec.Status(status).Terminal() {
			if err != nil || status != string(expected) {
				w.diagnoseResult(t, 1, w.execution.String(), status)
				t.Fatalf("M1 failure terminal status=%s expected=%s query_failed=%t", status, expected, err != nil)
			}
			return
		}
		select {
		case <-ctx.Done():
			w.diagnoseResult(t, 1, w.execution.String(), status)
			t.Fatal("M1 failure did not acquire its real terminal")
		case <-tick.C:
		}
	}
}

func (w *m1Workflow) checkFailureTerminal(t *testing.T, status ec.Status, reason string, cancelled bool) {
	t.Helper()
	v := w.app
	var sameTx bool
	var content []byte
	var gotReason string
	workAppCheck(t, "m1-failure-terminal-transaction", v.admin.QueryRow(v.ctx, `SELECT e.xmin=r.xmin AND e.xmin=s.xmin AND e.status=r.terminal_status AND e.status=s.terminal_status
 AND e.version=r.terminal_version AND e.version=s.terminal_version AND e.completed_at=r.finished_at AND e.completed_at=s.completed_at
 AND r.transcript_through=2 AND r.tool_pending AND r.snapshot_id=e.snapshot_id AND s.snapshot_id=e.snapshot_id,
 s.content,r.terminal_reason FROM agenteam_execution.executions e JOIN agenteam_execution.rounds r ON r.execution_id=e.id
 JOIN agenteam_execution.runtime_snapshots s ON s.execution_id=e.id AND s.round_id=r.id WHERE e.id=$1`, w.execution.String()).Scan(&sameTx, &content, &gotReason))
	var snapshot ec.RuntimeSnapshot
	workAppCheck(t, "m1-failure-snapshot", json.Unmarshal(content, &snapshot))
	if !sameTx || gotReason != reason || snapshot.Schema != ec.DurableFinalTextV2 || len(snapshot.Items) != 0 {
		t.Fatal("M1 failure terminal and empty v2 snapshot lack same-transaction provenance")
	}
	var operations, attempts, models, rounds, entries, results, reads, reports int
	var phase, outcome string
	var retired bool
	workAppCheck(t, "m1-failure-original-counts", v.admin.QueryRow(v.ctx, `SELECT
 (SELECT count(*) FROM agenteam_tool.operations WHERE execution_id=$1::text),
 (SELECT count(*) FROM agenteam_tool.attempts WHERE execution_id=$1::text),
 (SELECT count(*) FROM agenteam_model.invocations WHERE execution_id=$1::uuid),
 (SELECT count(*) FROM agenteam_execution.rounds WHERE execution_id=$1::text),
 (SELECT count(*) FROM agenteam_execution.transcript_entries WHERE execution_id=$1::text),
 (SELECT count(*) FROM agenteam_execution.loop_tool_results WHERE execution_id=$1::text),
 (SELECT count(*) FROM agenteam_tool.read_file_results WHERE execution_id=$1::text),
 (SELECT count(*) FROM agenteam_tool.command_results WHERE execution_id=$1::text)`, w.execution.String()).Scan(&operations, &attempts, &models, &rounds, &entries, &results, &reads, &reports))
	workAppCheck(t, "m1-failure-original-operation-attempt", v.admin.QueryRow(v.ctx, `SELECT o.phase,o.outcome,a.retired AND a.completed_at IS NOT NULL AND a.outcome_data=o.terminal_data AND a.phase=o.phase FROM agenteam_tool.operations o JOIN agenteam_tool.attempts a ON a.operation_id=o.operation_id AND a.attempt_id=o.active_attempt_id WHERE o.execution_id=$1`, w.execution.String()).Scan(&phase, &outcome, &retired))
	wantReports, wantOutcome := 0, "error"
	if cancelled {
		wantReports, wantOutcome = 1, "cancelled"
	}
	if operations != 1 || attempts != 1 || models != 1 || rounds != 1 || entries != 2 || results != 0 || reads != 0 || reports != wantReports || phase != string(status) || outcome != wantOutcome || !retired {
		t.Fatalf("M1 failure counts operations=%d attempts=%d models=%d rounds=%d entries=%d results=%d reads=%d reports=%d phase=%s outcome=%s retired=%t", operations, attempts, models, rounds, entries, results, reads, reports, phase, outcome, retired)
	}
	if cancelled {
		var report, stdout, stderr []byte
		var exact bool
		workAppCheck(t, "m1-cancel-original-command-report", v.admin.QueryRow(v.ctx, `SELECT r.body_data,r.stdout_data,r.stderr_data,r.request_id=a.backend_request_id AND r.attempt_id=a.attempt_id AND r.operation_id=o.operation_id AND o.xmin=a.xmin AND o.xmin=r.xmin AND convert_from(o.terminal_data,'UTF8')::jsonb->>'report_available'='true'
 FROM agenteam_tool.command_results r JOIN agenteam_tool.attempts a ON a.attempt_id=r.attempt_id JOIN agenteam_tool.operations o ON o.operation_id=r.operation_id WHERE r.execution_id=$1`, w.execution.String()).Scan(&report, &stdout, &stderr, &exact))
		decoded, err := rp.DecodeRunCommandResult(report)
		workAppCheck(t, "m1-cancel-command-report-contract", err)
		if !exact || decoded.State != "completed" || !decoded.Cancelled || decoded.TimedOut || !decoded.ProcessGroupTerminationConfirmed || decoded.CompletionCode == nil || *decoded.CompletionCode != rp.CancelledCode || len(stdout) != 0 || len(stderr) != 0 {
			t.Fatal("M1 cancellation lost the exact original process/pipe/handler completion report")
		}
	}
}

type m1CancelChild struct{ PID, Group int }

const m1CancelProgram = `import json, os, signal
from pathlib import Path
def stop(signum, frame):
    raise SystemExit(0)
signal.signal(signal.SIGTERM, stop)
Path("m1-child.tmp").write_text(json.dumps({"PID":os.getpid(),"Group":os.getpgrp()}))
os.replace("m1-child.tmp", "m1-child.json")
while True:
    signal.pause()
`

func (w *m1Workflow) awaitCancelChild(t *testing.T) (m1CancelChild, int) {
	t.Helper()
	ctx, cancel := context.WithTimeout(w.app.ctx, 10*time.Second)
	defer cancel()
	tick := time.NewTicker(20 * time.Millisecond)
	defer tick.Stop()
	for {
		raw, err := os.ReadFile(filepath.Join(w.workspace, "m1-child.json"))
		if err == nil {
			var child m1CancelChild
			if len(raw) > 256 || json.Unmarshal(raw, &child) != nil || child.PID <= 1 || child.Group != child.PID {
				t.Fatal("M1 command marker is not its owned direct child")
			}
			fd, err := unix.PidfdOpen(child.PID, 0)
			workAppCheck(t, "m1-command-original-pidfd", err)
			return child, fd
		}
		if !errors.Is(err, os.ErrNotExist) {
			workAppCheck(t, "m1-command-marker-read", err)
		}
		select {
		case <-ctx.Done():
			w.diagnoseResult(t, 1, w.execution.String(), "running-command-marker")
			t.Fatal("M1 original command did not publish its start marker")
		case <-tick.C:
		}
	}
}

func (w *m1Workflow) checkFailurePeer(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithTimeout(w.app.ctx, 3*time.Second)
	defer cancel()
	state, err := w.network.State(ctx, w.scenario)
	workAppCheck(t, "m1-failure-original-peer", err)
	if len(state.Requests) != 1 || state.ActiveHandlers != 0 || state.CompletedHandlers != 1 || !state.Closed[state.Requests[0].Connection] {
		t.Fatal("M1 failure repeated Model invocation or left its original HTTP owner open")
	}
}
