//go:build integration && linux

package app

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/config"
	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	gc "github.com/LunaDeerTech/agenteam/internal/central/governance/approval/contract"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	objectc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/LunaDeerTech/agenteam/internal/platform/lifecycle"
	"github.com/LunaDeerTech/agenteam/internal/platform/logging"
	"github.com/LunaDeerTech/agenteam/internal/runner/identity"
	p "github.com/LunaDeerTech/agenteam/internal/runnerprotocol"
	objectfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/objectstore"
	netfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/outbound"
	pgfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/postgres"
	"github.com/jackc/pgx/v5/pgconn"
)

// Every business row comes from the installed App. The parent owns all fixture
// resources; each child owns its actual default App, ProcessGuard and workers.
// SIGKILL plus the original Wait proves old borrowers cannot resume. No normal
// shutdown is used to pretend that a cancelled Execution remained waiting.
func TestM2AppExecutionColdRecovery(t *testing.T) {
	for _, running := range []bool{false, true} {
		name := "waiting-resumes-original-operation"
		if running {
			name = "running-without-result-never-redispatches"
		}
		t.Run(name, func(t *testing.T) {
			w := m2PrepareApproval(t, nil)
			var executions int
			workAppCheck(t, "m2-cold-provisioning-zero-executions", w.app.admin.QueryRow(w.app.ctx, `SELECT count(*) FROM agenteam_execution.executions WHERE project_id=$1`, w.app.a.String()).Scan(&executions))
			if executions != 0 {
				t.Fatal("provisioning App escaped the original directory return barrier")
			}
			taskExecutionJoinEnabled(t, w.app)
			first := m2StartRecoveryChild(t, w, running)
			pending := m2AwaitApproval(t, w)
			before := w.failurePending(t)
			w.failureLease(t, false)
			var snapshotProcess string
			workAppCheck(t, "m2-cold-original-snapshot-process", w.app.admin.QueryRow(w.app.ctx, `SELECT process_id FROM agenteam_execution.snapshots WHERE execution_id=$1`, w.execution.String()).Scan(&snapshotProcess))
			if snapshotProcess != first.process {
				t.Fatal("original checkpoint was not produced by the actual child ProcessGuard")
			}
			if running {
				m2RecoveryApprove(t, w, pending)
				m2RecoveryEvent(t, first, "m2_approved_return_held", nil)
				m2CheckColdBlocked(t, w, pending)
			}
			connectionBeforeKill := m2RecoveryRunnerConnections(&w.runner.stderr)
			m2KillRecoveryChild(t, w, first)
			var connectionAfterExit m2RecoveryRunnerConnection
			if !running {
				// A later disconnected event orders all old-session events
				// before the reconnect baseline. A historical connected event
				// cannot satisfy the new App's readiness barrier.
				connectionAfterExit = m2AwaitRecoveryRunner(t, w, nil, connectionBeforeKill, false)
			}
			second := m2StartRecoveryChild(t, w, false)
			if first.process == second.process {
				t.Fatal("restart reused the old ProcessGuard identity")
			}
			m2RecoveryEvent(t, second, "m2_cold_checkpoint", func(e map[string]any) bool { return e["committed"] == true })
			if running {
				// Four completed original Tool transactions establish that the
				// restored loop reached its read path more than once. With the
				// real approved Runner available, an Execute would create an
				// Attempt/effect; absence cannot authorize that Execute.
				m2RecoveryEvent(t, second, "m2_cold_tool_read", func(e map[string]any) bool { n, _ := e["count"].(float64); return n >= 4 })
				m2CheckColdBlocked(t, w, pending)
				if !reflect.DeepEqual(before, w.failurePending(t)) {
					t.Fatal("blocked recovery rewrote original checkpoint bytes")
				}
				w.failureLease(t, false)
				// This deliberately unresolved owner cannot report graceful
				// retirement. End its actual process and collect its real Wait.
				m2KillRecoveryChild(t, w, second)
				w.joinedPeer(t, w.scenario, 1, 1)
			} else {
				m2AwaitRecoveryRunner(t, w, second, connectionAfterExit, true)
				m2CheckUndispatchedApproval(t, w, pending, ec.Waiting)
				m2RecoveryApprove(t, w, pending)
				m2AwaitRecoveredTerminal(t, w, second)
				m2CheckApprovalTerminal(t, w, pending, gc.Approved)
				if !reflect.DeepEqual(before, w.failurePending(t)) {
					t.Fatal("cold recovery rewrote original Model/ToolCall checkpoint")
				}
				w.failureLease(t, true)
				m2StopRecoveryChild(t, w, second)
				w.joinedPeer(t, w.scenario, 2, 2)
			}
			m2StopRecoveryRunner(t, w)
			t.Logf("M2_COLD_RECOVERY scenario=%s actual_old_sigkill_wait=true new_process_guard=true same_database_execution_operation=true controlled_model_http=true", name)
		})
	}
}

func TestM2AppExecutionCompletedToolColdRecovery(t *testing.T) {
	w := m2PrepareApproval(t, nil)
	taskExecutionJoinEnabled(t, w.app)
	first := m2StartRecoveryChildMode(t, w, "hold-result")
	pending := m2AwaitApproval(t, w)
	before := w.failurePending(t)
	m2RecoveryApprove(t, w, pending)
	m2RecoveryEvent(t, first, "m2_completed_result_return_held", nil)
	var exact bool
	var attempts, reports, models, consumptions int
	var original string
	workAppCheck(t, "m2-cold-completed-original-result", w.app.admin.QueryRow(w.app.ctx, `SELECT
 e.status='running' AND e.cancel_requested_at IS NULL AND o.operation_id=$2 AND o.fingerprint=$3 AND o.phase='succeeded' AND a.phase='succeeded' AND a.retired AND a.process_id=$4,
 (SELECT count(*) FROM agenteam_tool.attempts WHERE execution_id=e.id),
 (SELECT count(*) FROM agenteam_tool.command_results WHERE execution_id=e.id),
 (SELECT count(*) FROM agenteam_model.invocations WHERE execution_id=e.id::uuid),
 (SELECT count(*) FROM agenteam_execution.loop_tool_results WHERE execution_id=e.id),
 jsonb_build_array(to_jsonb(o),to_jsonb(a),to_jsonb(r))::text
 FROM agenteam_execution.executions e JOIN agenteam_tool.operations o ON o.execution_id=e.id JOIN agenteam_tool.attempts a ON a.attempt_id=o.active_attempt_id JOIN agenteam_tool.command_results r ON r.operation_id=o.operation_id AND r.attempt_id=a.attempt_id WHERE e.id=$1`, w.execution.String(), pending.OperationID.String(), string(pending.Fingerprint), first.process).Scan(&exact, &attempts, &reports, &models, &consumptions, &original))
	if !exact || attempts != 1 || reports != 1 || models != 1 || consumptions != 0 {
		t.Fatal("crash barrier was not after the original Tool result and before its successor")
	}
	effect, err := os.ReadFile(filepath.Join(w.workspace, "m2-effect.txt"))
	workAppCheck(t, "m2-cold-original-effect", err)
	if string(effect) != "exactly-once\n" {
		t.Fatal("original command did not leave the intended exclusive-create effect")
	}
	w.failureLease(t, false)
	m2KillRecoveryChild(t, w, first)
	second := m2StartRecoveryChild(t, w, false)
	if second.process == first.process {
		t.Fatal("completed-result restart reused the original ProcessGuard")
	}
	m2RecoveryEvent(t, second, "m2_cold_checkpoint", func(e map[string]any) bool { return e["committed"] == true })
	m2RecoveryEvent(t, second, "m2_cold_tool_read", nil)
	m2AwaitRecoveredTerminal(t, w, second)
	m2CheckApprovalTerminal(t, w, pending, gc.Approved)
	var recovered string
	workAppCheck(t, "m2-cold-immutable-original-result", w.app.admin.QueryRow(w.app.ctx, `SELECT jsonb_build_array(to_jsonb(o),to_jsonb(a),to_jsonb(r))::text FROM agenteam_tool.operations o JOIN agenteam_tool.attempts a ON a.attempt_id=o.active_attempt_id JOIN agenteam_tool.command_results r ON r.operation_id=o.operation_id AND r.attempt_id=a.attempt_id WHERE o.operation_id=$1`, pending.OperationID.String()).Scan(&recovered))
	if original != recovered || !reflect.DeepEqual(before, w.failurePending(t)) {
		t.Fatal("cold lookup replaced the committed operation, Attempt, result or Model checkpoint")
	}
	w.failureLease(t, true)
	m2StopRecoveryChild(t, w, second)
	w.joinedPeer(t, w.scenario, 2, 2)
	m2StopRecoveryRunner(t, w)
	t.Log("M2_COMPLETED_TOOL_COLD actual_sigkill_wait=true original_completed_result_lookup=true one_attempt_effect=true original_operation_bytes_preserved=true successor_once=true")
}

type m2RecoveryChild struct {
	*fixtureProcess
	process    string
	pending    []map[string]any
	waitLogged sync.Once
}

func m2StartRecoveryChild(t *testing.T, w *m1Workflow, holdApproved bool) *m2RecoveryChild {
	t.Helper()
	mode := "observe"
	if holdApproved {
		mode = "hold-approved"
	}
	return m2StartRecoveryChildMode(t, w, mode)
}

func m2StartRecoveryChildMode(t *testing.T, w *m1Workflow, mode string) *m2RecoveryChild {
	t.Helper()
	executable, err := os.Executable()
	workAppCheck(t, "m2-cold-test-executable", err)
	address, err := url.Parse(w.app.address)
	workAppCheck(t, "m2-cold-original-listener", err)
	env := slices.Clone(w.environment)
	for n, value := range env {
		if strings.HasPrefix(value, "AGENTEAM_CENTRAL_HTTP_ADDR=") {
			env[n] = "AGENTEAM_CENTRAL_HTTP_ADDR=" + address.Host
		}
	}
	p := &m2RecoveryChild{fixtureProcess: &fixtureProcess{cmd: exec.Command(executable, "-test.run=^TestM2ExecutionRecoveryChild$", "-test.timeout=80s"), stdout: newEventLog(), stderr: newEventLog(), done: make(chan struct{})}}
	p.cmd.Dir = t.TempDir()
	p.cmd.Env = append(env, "PATH="+os.Getenv("PATH"), pgfixture.Env+"="+os.Getenv(pgfixture.Env), objectfixture.Env+"="+os.Getenv(objectfixture.Env), netfixture.Env+"="+os.Getenv(netfixture.Env), "AGENTEAM_M2_RECOVERY_CHILD="+mode, "AGENTEAM_M2_RECOVERY_DATABASE="+w.app.root.db.Name)
	p.cmd.Stdout, p.cmd.Stderr = p.stdout, p.stderr
	workAppCheck(t, "m2-cold-child-start", p.cmd.Start())
	t.Logf("M2_RECOVERY_CHILD_START mode=%s pid=%d", mode, p.cmd.Process.Pid)
	go func() { p.err = p.cmd.Wait(); close(p.done) }()
	t.Cleanup(func() {
		defer m2RecoveryChildWaitLog(t, p, "cleanup")
		select {
		case <-p.done:
		default:
			err := p.cmd.Process.Kill()
			t.Logf("M2_RECOVERY_CHILD_CLEANUP pid=%d kill_failed=%t", p.cmd.Process.Pid, err != nil)
			usageAppWait(t, p.done, "m2-cold-owned-child-cleanup-wait")
		}
	})
	ready := m2RecoveryEvent(t, p, "m2_recovery_ready", nil)
	p.process, _ = ready["process_id"].(string)
	_, err = f.ParseID[objectc.Process](p.process)
	workAppCheck(t, "m2-cold-actual-child-process", err)
	return p
}

// These event waits monitor the original child exit and never include private
// environment/configuration or arbitrary child output in a failing assertion.
func m2RecoveryEvent(t *testing.T, p *m2RecoveryChild, name string, match func(map[string]any) bool) map[string]any {
	t.Helper()
	accept := func(event map[string]any) bool {
		if event["event"] == "m2_recovery_failure" {
			m2RecoveryLogFailure(t, event)
		}
		if event["event"] == "m2_recovery_observer_failed" {
			t.Fatal("M2 cold recovery observer query failed")
		}
		return event["event"] == name && (match == nil || match(event))
	}
	for n, event := range p.pending {
		if accept(event) {
			p.pending = append(p.pending[:n], p.pending[n+1:]...)
			return event
		}
	}
	timer := time.NewTimer(15 * time.Second)
	defer timer.Stop()
	for {
		select {
		case event := <-p.stdout.events:
			if accept(event) {
				return event
			}
			p.pending = append(p.pending, event)
		case <-p.done:
			m2RecoveryChildWaitLog(t, p, "event")
			// Cmd.Wait has joined stdout's writer. Its final event can still
			// be buffered when both the event and done channels are ready.
			for {
				select {
				case event := <-p.stdout.events:
					if accept(event) {
						return event
					}
					p.pending = append(p.pending, event)
				default:
					t.Fatalf("M2 child returned before stage=%s failed=%t", name, p.err != nil)
				}
			}
		case <-timer.C:
			t.Fatalf("M2 child did not reach stage=%s", name)
		}
	}
}

func m2KillRecoveryChild(t *testing.T, w *m1Workflow, p *m2RecoveryChild) {
	t.Helper()
	workAppCheck(t, "m2-cold-original-sigkill", p.cmd.Process.Kill())
	usageAppWait(t, p.done, "m2-cold-original-sigkill-wait")
	m2RecoveryChildWaitLog(t, p, "sigkill")
	var exit *exec.ExitError
	if !errors.As(p.err, &exit) {
		t.Fatal("M2 crash did not return an actual child exit")
	}
	status, ok := exit.Sys().(syscall.WaitStatus)
	if !ok || !status.Signaled() || status.Signal() != syscall.SIGKILL {
		t.Fatal("M2 original child did not actually die from SIGKILL")
	}
	m2RecoveryGuardReleased(t, w, p.process)
}

func m2StopRecoveryChild(t *testing.T, w *m1Workflow, p *m2RecoveryChild) {
	t.Helper()
	workAppCheck(t, "m2-cold-success-root-stop", p.cmd.Process.Signal(syscall.SIGTERM))
	returned := m2RecoveryEvent(t, p, "m2_recovery_returned", nil)
	usageAppWait(t, p.done, "m2-cold-success-root-wait")
	m2RecoveryChildWaitLog(t, p, "stop")
	if p.err != nil || returned["joined"] != true {
		t.Fatal("recovered successful App did not join its actual owners")
	}
	m2RecoveryGuardReleased(t, w, p.process)
}

func m2RecoveryGuardReleased(t *testing.T, w *m1Workflow, process string) {
	t.Helper()
	claim, err := os.OpenFile(filepath.Join(w.app.root.cfg.Objects().SpoolDirectory()+".processes", process+".claim"), os.O_RDWR, 0)
	workAppCheck(t, "m2-cold-original-guard-file", err)
	defer claim.Close()
	workAppCheck(t, "m2-cold-original-guard-unlocked", syscall.Flock(int(claim.Fd()), syscall.LOCK_EX|syscall.LOCK_NB))
	workAppCheck(t, "m2-cold-guard-observation-unlock", syscall.Flock(int(claim.Fd()), syscall.LOCK_UN))
}

func m2RecoveryGuardHeld(t *testing.T, w *m1Workflow, process string) {
	t.Helper()
	claim, err := os.OpenFile(filepath.Join(w.app.root.cfg.Objects().SpoolDirectory()+".processes", process+".claim"), os.O_RDWR, 0)
	workAppCheck(t, "m2-live-original-guard-file", err)
	defer claim.Close()
	err = syscall.Flock(int(claim.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if err == nil {
		_ = syscall.Flock(int(claim.Fd()), syscall.LOCK_UN)
		t.Fatal("competing recovery observed a released original ProcessGuard")
	}
	if !errors.Is(err, syscall.EWOULDBLOCK) {
		t.Fatal("original guard check failed without proving a live lock")
	}
}

func m2RecoveryApprove(t *testing.T, w *m1Workflow, pending gc.Request) {
	t.Helper()
	var response struct {
		Receipt gc.Receipt `json:"receipt"`
	}
	m2ApprovalRequest(t, w, pending.ID, "decision", guardID[struct{}](t).String(), gc.Decision{Decision: "approve", ExpectedVersion: pending.Version}, http.StatusOK, &response)
	if response.Receipt.Approval.ID != pending.ID || response.Receipt.Approval.OperationID != pending.OperationID || response.Receipt.Approval.Fingerprint != pending.Fingerprint || response.Receipt.Approval.Status != gc.Approved {
		t.Fatal("cold decision replaced the original request/operation")
	}
}

func m2CheckColdBlocked(t *testing.T, w *m1Workflow, original gc.Request) {
	t.Helper()
	var exact bool
	var attempts, invocations, results, operations int
	workAppCheck(t, "m2-cold-no-replay-facts", w.app.admin.QueryRow(w.app.ctx, `SELECT e.status='running' AND e.cancel_requested_at IS NULL AND e.waiting_reference IS NULL AND o.operation_id=$2 AND o.fingerprint=$3 AND o.phase='created' AND o.active_attempt_id IS NULL,
 (SELECT count(*) FROM agenteam_tool.attempts WHERE execution_id=e.id),
 (SELECT count(*) FROM agenteam_model.invocations WHERE execution_id=e.id::uuid),
 (SELECT count(*) FROM agenteam_tool.command_results WHERE execution_id=e.id),
 (SELECT count(*) FROM agenteam_tool.operations WHERE execution_id=e.id)
 FROM agenteam_execution.executions e JOIN agenteam_tool.operations o ON o.execution_id=e.id WHERE e.id=$1`, w.execution.String(), original.OperationID.String(), string(original.Fingerprint)).Scan(&exact, &attempts, &invocations, &results, &operations))
	if !exact || attempts != 0 || invocations != 1 || results != 0 || operations != 1 {
		t.Fatal("unproved running operation was dispatched or replaced")
	}
	if _, err := os.Stat(filepath.Join(w.workspace, "m2-effect.txt")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("unproved running operation produced a filesystem effect")
	}
}

func m2AwaitRecoveredTerminal(t *testing.T, w *m1Workflow, p *m2RecoveryChild) {
	t.Helper()
	ctx, cancel := context.WithTimeout(w.app.ctx, 30*time.Second)
	defer cancel()
	tick := time.NewTicker(25 * time.Millisecond)
	defer tick.Stop()
	for {
		var status string
		workAppCheck(t, "m2-cold-original-execution-result", w.app.admin.QueryRow(ctx, `SELECT status FROM agenteam_execution.executions WHERE id=$1`, w.execution.String()).Scan(&status))
		if ec.Status(status).Terminal() {
			if status != string(ec.Succeeded) {
				m2RecoveryTerminalDiagnostic(t, w)
				w.diagnoseResult(t, 1, w.execution.String(), status)
				t.Fatal("cold original execution did not succeed")
			}
			return
		}
		select {
		case <-ctx.Done():
			w.diagnoseResult(t, 1, w.execution.String(), status)
			t.Fatal("cold original execution did not finish")
		case <-p.done:
			m2RecoveryChildWaitLog(t, p, "terminal")
			t.Fatal("recovering App returned before original terminal")
		case <-tick.C:
		}
	}
}

func m2StopRecoveryRunner(t *testing.T, w *m1Workflow) {
	t.Helper()
	v := w.runner
	workAppCheck(t, "m2-cold-runner-stop", v.cmd.Process.Signal(syscall.SIGTERM))
	usageAppWait(t, v.done, "m2-cold-original-runner-wait")
	if v.err != nil || !v.stderr.event("shutdown_complete", "outcome", "drained") {
		t.Fatal("cold workflow Runner did not join its original process")
	}
	v.proxy.stop(t)
	file, err := identity.Open(v.identityPath)
	workAppCheck(t, "m2-cold-runner-identity-unlocked", err)
	workAppCheck(t, "m2-cold-runner-identity-close", file.Close())
}

// Only codes with a closed vocabulary leave the child. In particular neither
// an error's text nor a PG detail, URI, configuration value or stderr is logged.
func m2RecoveryFailure(stage string, err error) {
	event := map[string]any{"event": "m2_recovery_failure", "stage": stage,
		"cancelled": errors.Is(err, context.Canceled), "deadline": errors.Is(err, context.DeadlineExceeded)}
	var fault *f.Fault
	if errors.As(err, &fault) {
		event["code"], event["commit_state"] = fault.Code.Safe(), fault.CommitState.Safe()
	}
	var failure *lifecycle.Failure
	if errors.As(err, &failure) {
		event["lifecycle"] = failure.Code()
	}
	var pg *pgconn.PgError
	if errors.As(err, &pg) && m2RecoverySQLState(pg.Code) {
		event["sqlstate"] = pg.Code
	}
	var errno syscall.Errno
	if errors.As(err, &errno) {
		event["errno"] = uint64(errno)
	}
	_ = json.NewEncoder(os.Stdout).Encode(event)
}

func m2RecoverySQLState(value string) bool {
	return len(value) == 5 && strings.IndexFunc(value, func(r rune) bool {
		return !(r >= '0' && r <= '9' || r >= 'A' && r <= 'Z')
	}) == -1
}

func m2RecoveryLogFailure(t *testing.T, event map[string]any) {
	t.Helper()
	stage, _ := event["stage"].(string)
	switch stage {
	case "database-fixture", "database-config", "network-fixture", "network-address", "config", "logger", "store-open", "bind", "startup-return", "startup-timeout", "guard", "root-return", "cancel-observer", "result-owner", "result-observer", "approved-observer", "same-owner-query", "same-owner-tuple", "same-owner-call", "same-owner-proof":
	default:
		stage = "unrecognized"
	}
	code, _ := event["code"].(string)
	state, _ := event["commit_state"].(string)
	life, _ := event["lifecycle"].(string)
	sqlstate, _ := event["sqlstate"].(string)
	if !m2RecoverySQLState(sqlstate) {
		sqlstate = ""
	}
	errno, _ := event["errno"].(float64)
	cancelled, _ := event["cancelled"].(bool)
	deadline, _ := event["deadline"].(bool)
	t.Logf("M2_RECOVERY_FAILURE stage=%s fault_present=%t code=%s state=%s lifecycle_present=%t lifecycle=%s sqlstate=%s errno=%d cancelled=%t deadline=%t", stage, code != "", f.Code(code).Safe(), f.CommitState(state).Safe(), life != "", lifecycle.FailureCode(life).Safe(), sqlstate, uint64(errno), cancelled, deadline)
}

func m2RecoveryCheck(t *testing.T, stage string, err error) {
	t.Helper()
	if err != nil {
		m2RecoveryFailure(stage, err)
		t.Fatalf("M2 recovery child failed stage=%s", stage)
	}
}

func m2RecoveryChildWaitLog(t *testing.T, child *m2RecoveryChild, source string) {
	t.Helper()
	select {
	case <-child.done:
		child.waitLogged.Do(func() {
			exit, signaled, signal := -1, false, 0
			if state := child.cmd.ProcessState; state != nil {
				exit = state.ExitCode()
				if status, ok := state.Sys().(syscall.WaitStatus); ok && status.Signaled() {
					signaled, signal = true, int(status.Signal())
				}
			}
			t.Logf("M2_RECOVERY_CHILD_WAIT pid=%d source=%s actual_wait=true exit=%d signaled=%t signal=%d failed=%t", child.cmd.Process.Pid, source, exit, signaled, signal, child.err != nil)
		})
	default:
		t.Logf("M2_RECOVERY_CHILD_WAIT pid=%d source=%s actual_wait=false cleanup_failed=true", child.cmd.Process.Pid, source)
	}
}

func m2RecoveryTerminalDiagnostic(t *testing.T, w *m1Workflow) {
	t.Helper()
	var raw []byte
	err := w.app.admin.QueryRow(w.app.ctx, `SELECT terminal_data FROM agenteam_tool.operations WHERE execution_id=$1`, w.execution.String()).Scan(&raw)
	if err != nil {
		t.Log("M2_RECOVERY_TOOL_DIAGNOSTIC query_failed=true")
		return
	}
	defer clear(raw)
	var terminal struct {
		Code            string
		Attempted       bool
		ReportAvailable bool `json:"report_available"`
	}
	if json.Unmarshal(raw, &terminal) != nil {
		t.Log("M2_RECOVERY_TOOL_DIAGNOSTIC decode_failed=true")
		return
	}
	code := terminal.Code
	switch code {
	case "", "not_dispatched", "command_exit_nonzero", "command_signaled", "output_incomplete", "backend_contract_violation":
	default:
		if p.Code(code).SafeMessage() == "" {
			code = "unrecognized"
		}
	}
	t.Logf("M2_RECOVERY_TOOL_DIAGNOSTIC code=%s attempted=%t report_available=%t", code, terminal.Attempted, terminal.ReportAvailable)
	connection := m2RecoveryRunnerConnections(&w.runner.stderr)
	t.Logf("M2_RECOVERY_RUNNER_DIAGNOSTIC last_state=%s authenticated_connections=%d overflow=%t", connection.state, connection.connected, connection.overflow)
}

type m2RecoveryRunnerConnection struct {
	state                   string
	connected, disconnected int
	overflow, authenticated bool
}

func m2RecoveryRunnerConnections(output *m1RunnerOutput) m2RecoveryRunnerConnection {
	raw, overflow := output.snapshot()
	defer clear(raw)
	observation := m2RecoveryRunnerConnection{state: "absent", overflow: overflow}
	if !overflow {
		for _, line := range strings.Split(string(raw), "\n") {
			var event map[string]any
			if json.Unmarshal([]byte(line), &event) != nil || event["event"] != "runner_connection" {
				continue
			}
			switch event["state"] {
			case "connecting", "connected", "disconnected":
				observation.state = event["state"].(string)
			default:
				observation.state = "unrecognized"
			}
			if event["state"] == "connected" && event["connected"] == true && event["authenticated"] == true && event["ready"] == false {
				observation.authenticated = true
				observation.connected++
			} else {
				observation.authenticated = false
			}
			if event["state"] == "disconnected" && event["connected"] == false && event["authenticated"] == false {
				observation.disconnected++
			}
		}
	}
	return observation
}

func m2AwaitRecoveryRunner(t *testing.T, w *m1Workflow, child *m2RecoveryChild, before m2RecoveryRunnerConnection, connected bool) m2RecoveryRunnerConnection {
	t.Helper()
	// Runner's real reconnect backoff caps at 30 seconds. This remains inside
	// the existing 80-second child budget and does not retry a Tool operation.
	ctx, cancel := context.WithTimeout(w.app.ctx, 35*time.Second)
	defer cancel()
	tick := time.NewTicker(25 * time.Millisecond)
	defer tick.Stop()
	var childDone <-chan struct{}
	if child != nil {
		childDone = child.done
	}
	for {
		current := m2RecoveryRunnerConnections(&w.runner.stderr)
		if current.overflow {
			t.Fatal("M2 Runner connection observer overflowed")
		}
		if !connected && current.state == "disconnected" && current.disconnected > before.disconnected || connected && current.state == "connected" && current.authenticated && current.connected > before.connected {
			t.Logf("M2_RECOVERY_RUNNER_BARRIER connected=%t new_connection=%t new_disconnect=%t", connected, current.connected > before.connected, current.disconnected > before.disconnected)
			return current
		}
		select {
		case <-ctx.Done():
			t.Fatalf("M2 Runner new connection barrier timed out connected=%t last_state=%s authenticated_connections=%d disconnects=%d", connected, current.state, current.connected, current.disconnected)
		case <-w.runner.done:
			t.Fatal("original Runner returned before its new connection barrier")
		case <-childDone:
			m2RecoveryChildWaitLog(t, child, "connection")
			t.Fatal("new App returned before original Runner reconnected")
		case <-tick.C:
		}
	}
}

// This is the actual installed executor and the original durable Scheduler
// association. No private driver witness or alternate executor is fabricated.
func m2RecoveryObserveSameOwner(t *testing.T, owned *resources, store *postgres.Store) {
	t.Helper()
	accounts, ok := owned.accounts().(*accountAssembly)
	if !ok || accounts == nil || accounts.executions == nil || accounts.executions.executor == nil {
		m2RecoveryCheck(t, "same-owner-proof", f.NewFault(f.DependencyUnbound, f.NotStarted))
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	tick := time.NewTicker(25 * time.Millisecond)
	defer tick.Stop()
	var project, execution, agent, dispatch, key, digest string
	for {
		var ready bool
		err := store.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_execution.executions WHERE status='waiting' AND cancel_requested_at IS NULL)`).Scan(&ready)
		m2RecoveryCheck(t, "same-owner-query", err)
		if ready {
			break
		}
		select {
		case <-ctx.Done():
			m2RecoveryCheck(t, "same-owner-query", ctx.Err())
		case <-tick.C:
		}
	}
	err := store.QueryRow(ctx, `SELECT d.project_id,d.execution_id,d.agent_id,d.id,d.idempotency_key,d.launch_digest FROM agenteam_scheduler.dispatches d JOIN agenteam_execution.executions e ON e.id=d.execution_id WHERE d.status='launched' AND e.status='waiting' AND e.cancel_requested_at IS NULL`).Scan(&project, &execution, &agent, &dispatch, &key, &digest)
	m2RecoveryCheck(t, "same-owner-query", err)
	pid, err := f.ParseID[i.Project](project)
	m2RecoveryCheck(t, "same-owner-tuple", err)
	eid, err := f.ParseID[i.Execution](execution)
	m2RecoveryCheck(t, "same-owner-tuple", err)
	aid, err := f.ParseID[i.Agent](agent)
	m2RecoveryCheck(t, "same-owner-tuple", err)
	const owners = `SELECT count(*),coalesce(jsonb_agg(jsonb_build_array(id,lane,process_id) ORDER BY id)::text,'[]') FROM agenteam_execution.project_calls WHERE project_id=$1 AND lane IN ('associated','direct') AND phase='running'`
	var before, after string
	var beforeCount, afterCount int
	m2RecoveryCheck(t, "same-owner-query", store.QueryRow(ctx, owners, project).Scan(&beforeCount, &before))
	observation, err := accounts.executions.executor.Advance(ctx, pid, ec.AssociatedDispatch{ExecutionID: eid, AgentID: aid, DispatchID: dispatch, Key: f.IdempotencyKey(key), Digest: f.Digest(digest)})
	m2RecoveryCheck(t, "same-owner-call", err)
	m2RecoveryCheck(t, "same-owner-query", store.QueryRow(ctx, owners, project).Scan(&afterCount, &after))
	if observation.ProjectID != pid || observation.ExecutionID != eid || !observation.Active || observation.Joined || beforeCount != 2 || afterCount != beforeCount || before != after {
		m2RecoveryCheck(t, "same-owner-proof", f.NewFault(f.InvalidState, f.NotStarted))
	}
	_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"event": "m2_same_owner_advance_return", "active": true, "joined": false, "same_project_call_ids": true})
}

type m2RecoveryStore struct {
	*postgres.Store
	mode         string
	holdApproved bool
	held, cold   atomic.Bool
	reads        atomic.Int64
}

func (s *m2RecoveryStore) WithinTx(ctx context.Context, cause f.TransactionCause, fn func(context.Context, f.Tx) error) f.CommitResult {
	result := s.Store.WithinTx(ctx, cause, fn)
	d := cause.Details()
	if d.Kind == f.RecoveryCause && d.Owner == "execution.loop.cold-checkpoint" {
		if result.State() == f.Committed {
			s.cold.Store(true)
		}
		_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"event": "m2_cold_checkpoint", "committed": result.State() == f.Committed})
	}
	if result.State() != f.Committed {
		return result
	}
	// Observe actual original callbacks, not just a process-ready marker. Each
	// observation is after the Store's real CommitResult and never changes it.
	if d.Kind == f.RecoveryCause && d.Owner == "execution.associated-read" {
		var cancelled bool
		err := s.Store.QueryRow(ctx, `SELECT status='cancelled' AND cancel_requested_at IS NOT NULL FROM agenteam_execution.executions WHERE id=$1`, d.RecoveryRunID).Scan(&cancelled)
		if err != nil {
			m2RecoveryFailure("cancel-observer", err)
			_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"event": "m2_recovery_observer_failed"})
		} else if cancelled {
			_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"event": "m2_cancelled_observed"})
		}
	}
	if s.cold.Load() && d.Kind == f.CommandsCause && d.Primary.Namespace() == "tool.operation" && d.Primary.Command() == "prepare" {
		_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"event": "m2_cold_tool_read", "count": s.reads.Add(1)})
	}
	if s.mode == "hold-result" && d.Kind == f.CommandsCause && d.Primary.Namespace() == "tool.operation" && d.Primary.Command() == "prepare" {
		owners := d.Primary.OwnerIDs()
		var complete bool
		if len(owners) != 1 {
			m2RecoveryFailure("result-owner", f.NewFault(f.InvalidState, f.NotStarted))
			_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"event": "m2_recovery_observer_failed"})
			return result
		}
		err := s.Store.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_tool.operations o JOIN agenteam_tool.attempts a ON a.attempt_id=o.active_attempt_id JOIN agenteam_tool.command_results r ON r.operation_id=o.operation_id AND r.attempt_id=a.attempt_id WHERE o.execution_id=$1 AND o.phase='succeeded' AND a.retired)`, owners[0]).Scan(&complete)
		if err != nil {
			m2RecoveryFailure("result-observer", err)
			_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"event": "m2_recovery_observer_failed"})
			return result
		}
		if complete && s.held.CompareAndSwap(false, true) {
			_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"event": "m2_completed_result_return_held"})
			select {} // Released only by the parent's actual SIGKILL and Wait.
		}
	}
	if s.holdApproved && d.Kind == f.JobCause && d.JobType == "execution-approval-wait" {
		var running bool
		err := s.Store.QueryRow(ctx, `SELECT status='running' AND waiting_reference IS NULL AND cancel_requested_at IS NULL FROM agenteam_execution.executions WHERE id=$1`, d.JobID).Scan(&running)
		if err != nil {
			m2RecoveryFailure("approved-observer", err)
			_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"event": "m2_recovery_observer_failed"})
			return result
		}
		if running && s.held.CompareAndSwap(false, true) {
			_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"event": "m2_approved_return_held"})
			// The exact successful transition committed, but its original
			// return has not authorized the caller to dispatch. Only the
			// parent's real process kill releases this fault boundary.
			select {}
		}
	}
	return result
}

func TestM2ExecutionRecoveryChild(t *testing.T) {
	mode := os.Getenv("AGENTEAM_M2_RECOVERY_CHILD")
	if mode == "" {
		return
	}
	if mode != "observe" && mode != "hold-approved" && mode != "hold-result" && mode != "observe-owner" {
		t.Fatal("unknown owned recovery child mode")
	}
	fixture, err := pgfixture.Load()
	m2RecoveryCheck(t, "database-fixture", err)
	_, err = fixture.Config(os.Getenv("AGENTEAM_M2_RECOVERY_DATABASE"), nil)
	m2RecoveryCheck(t, "database-config", err)
	network, err := netfixture.Load()
	m2RecoveryCheck(t, "network-fixture", err)
	address, err := netip.ParseAddr(network.PrivateIP)
	m2RecoveryCheck(t, "network-address", err)
	env := os.Environ()
	values := make(map[string]string, len(env))
	for _, entry := range env {
		key, value, _ := strings.Cut(entry, "=")
		values[key] = value
	}
	cfg, err := config.Load(func(key string) (string, bool) { value, ok := values[key]; return value, ok }, env)
	m2RecoveryCheck(t, "config", err)
	signals := make(chan os.Signal, 4)
	signal.Notify(signals, syscall.SIGTERM, syscall.SIGINT)
	defer signal.Stop(signals)
	var owned *resources
	store := &m2RecoveryStore{mode: mode, holdApproved: mode == "hold-approved"}
	d := dependencies{
		open: func(ctx context.Context, cfg postgres.Config) (database, error) {
			var err error
			store.Store, err = postgres.Open(ctx, cfg)
			if err != nil {
				m2RecoveryFailure("store-open", err)
			}
			return store, err
		},
		bind: func(ctx context.Context, cfg config.Config, db database, actual *resources, deps *dependencies) error {
			owned = actual
			err := bindAccounts(ctx, cfg, db, actual, deps)
			if err != nil {
				m2RecoveryFailure("bind", err)
			}
			return err
		},
		outboundConstruct: func(_ context.Context, _ config.Config, actual *outboundRuntime) error {
			actual.resolver = appFixtureResolver{address}
			return nil
		},
	}
	logs := newEventLog()
	logger, err := logging.New(logging.Central, slog.LevelInfo, io.MultiWriter(os.Stderr, logs))
	m2RecoveryCheck(t, "logger", err)
	done := make(chan struct{})
	var runErr error
	go func() { runErr = run(context.Background(), cfg, logger, signals, d); close(done) }()
	startup := time.NewTimer(40 * time.Second)
	defer startup.Stop()
listening:
	for {
		select {
		case event := <-logs.events:
			if event["event"] == "listening" {
				break listening
			}
		case <-done:
			m2RecoveryFailure("startup-return", runErr)
			t.Fatal("M2 child returned before listening")
		case <-startup.C:
			m2RecoveryCheck(t, "startup-timeout", context.DeadlineExceeded)
		}
	}
	objects, ok := owned.objects().(*objectAssembly)
	if !ok || objects == nil {
		m2RecoveryCheck(t, "guard", f.NewFault(f.DependencyUnbound, f.NotStarted))
	}
	_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"event": "m2_recovery_ready", "process_id": objects.process.String()})
	if mode == "observe-owner" {
		m2RecoveryObserveSameOwner(t, owned, store.Store)
	}
	<-done
	m2RecoveryCheck(t, "root-return", runErr)
	joined := owned.producersJoined()
	_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"event": "m2_recovery_returned", "joined": joined})
	if !joined {
		t.Fatal("cold child returned before its original producers")
	}
}
