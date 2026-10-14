//go:build integration && linux

package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"syscall"
	"testing"
	"time"

	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
	"github.com/LunaDeerTech/agenteam/internal/runner/identity"
	netfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/outbound"
	pgfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/postgres"
	"golang.org/x/sys/unix"
)

// These tests drive the actual observer with local clock/count inputs. They
// neither read live configuration nor start a database, App or external HTTP.
func TestM1LiveBudgetObserver(t *testing.T) {
	for _, test := range []struct {
		name, reason string
		counts       []int
		failedRead   bool
		deadline     bool
		stop         bool
	}{
		{name: "deadline", reason: "total_deadline", deadline: true},
		{name: "below_then_exact_threshold", reason: "sent_stop_threshold", counts: []int{11, 12}},
		{name: "inflight_above_threshold", reason: "sent_stop_threshold", counts: []int{13}},
		{name: "count_failure_after_below_threshold", reason: "budget_observation_failed", counts: []int{11, 0}, failedRead: true},
		{name: "normal_stop", stop: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			stop, deadline := make(chan struct{}), make(chan time.Time, 1)
			tick := make(chan time.Time, len(test.counts))
			if test.stop {
				close(stop)
			}
			if test.deadline {
				deadline <- time.Unix(300, 0)
			}
			for range test.counts {
				tick <- time.Unix(1, 0)
			}
			calls := 0
			var contexts []context.Context
			count := func(ctx context.Context) (int, error) {
				contexts = append(contexts, ctx)
				limit, ok := ctx.Deadline()
				if !ok || time.Until(limit) > time.Second || ctx.Err() != nil {
					t.Error("budget count lacks its independent one-second observation bound")
				}
				calls++
				if calls > len(test.counts) {
					return 0, errors.New("unexpected extra count")
				}
				if test.failedRead && calls == len(test.counts) {
					return 0, errors.New("owned count read failure")
				}
				return test.counts[calls-1], nil
			}
			done := make(chan string, 1)
			go func() { done <- m1ObserveLiveBudget(stop, deadline, tick, count) }()
			select {
			case reason := <-done:
				if reason != test.reason || calls != len(test.counts) {
					t.Fatalf("budget reason=%q calls=%d; want reason=%q calls=%d", reason, calls, test.reason, len(test.counts))
				}
				for _, ctx := range contexts {
					if !errors.Is(ctx.Err(), context.Canceled) {
						t.Error("budget count context survived its observation")
					}
				}
			case <-time.After(2 * time.Second):
				if !test.stop {
					close(stop)
				}
				<-done
				t.Fatal("local budget stimulus did not return the actual observer")
			}
		})
	}
}

// Only accounting is under test here: these are explicitly synthetic rows in
// a new owned database, not Model calls or fabricated workflow evidence. The
// shared query is the one used by the live observer's real connection.
func TestM1LiveBudgetSentCount(t *testing.T) {
	db := pgfixture.NewDatabase(t)
	connection := db.Connect(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := connection.Exec(ctx, `CREATE SCHEMA agenteam_model;
 CREATE TABLE agenteam_model.invocations (
 project_id uuid NOT NULL, dispatch text NOT NULL, execution_purpose text NOT NULL, attempt_index integer NOT NULL
 )`)
	workAppCheck(t, "m1-budget-owned-count-relation", err)
	project, other := guardID[id.Project](t).String(), guardID[id.Project](t).String()
	_, err = connection.Exec(ctx, `INSERT INTO agenteam_model.invocations
 SELECT $1::uuid,'sent','task/work',1 FROM generate_series(1,5)
 UNION ALL SELECT $1::uuid,'sent','task/review',1 FROM generate_series(1,4)
 UNION ALL SELECT $1::uuid,'sent','task/work',2
 UNION ALL SELECT $1::uuid,'sent','task/review',2
 UNION ALL SELECT $1::uuid,'not_sent','task/work',1 FROM generate_series(1,20)
 UNION ALL SELECT $2::uuid,'sent','task/work',1 FROM generate_series(1,20)`, project, other)
	workAppCheck(t, "m1-budget-owned-count-inputs", err)
	count := func(ctx context.Context) (int, error) {
		var sent int
		err := connection.QueryRow(ctx, m1LiveSentQuery, project).Scan(&sent)
		return sent, err
	}
	sent, err := count(ctx)
	workAppCheck(t, "m1-budget-below-threshold-count", err)
	if sent != 11 {
		t.Fatalf("budget count=%d; want work=5 review=4 retries=2, excluding other project and not_sent", sent)
	}
	_, err = connection.Exec(ctx, `INSERT INTO agenteam_model.invocations VALUES ($1::uuid,'sent','task/review',1)`, project)
	workAppCheck(t, "m1-budget-twelfth-accounting-input", err)
	sent, err = count(ctx)
	workAppCheck(t, "m1-budget-threshold-count", err)
	if sent != 12 {
		t.Fatalf("budget count=%d; want work=5 review=5 retries=2", sent)
	}
	tick := make(chan time.Time, 1)
	tick <- time.Now()
	if reason := m1ObserveLiveBudget(make(chan struct{}), nil, tick, count); reason != "sent_stop_threshold" {
		t.Fatalf("owned SQL count did not stop the observer: %q", reason)
	}
	workAppCheck(t, "m1-budget-close-count-connection", connection.Close(ctx))
	tick <- time.Now()
	if reason := m1ObserveLiveBudget(make(chan struct{}), nil, tick, count); reason != "budget_observation_failed" {
		t.Fatalf("closed owned SQL connection did not fail closed: %q", reason)
	}
	t.Log("M1_LIVE_BUDGET_COUNT synthetic_accounting_only=true work=5 review=5 retries=2 threshold=12 other_project_excluded=true not_sent_excluded=true closed_connection_fail_closed=true")
}

// One local deadline drives the same observer/owner used by live runs while
// the original App, Model, Tool and Runner have a real command in flight.
// It proves budget shutdown and incomplete output, not external model work.
func TestM1AppLiveBudgetStop(t *testing.T) {
	w, _ := newM1WorkflowWithOptions(t, nil, false)
	v := w.app
	w.seed(t)
	w.startRunner(t)
	// Register after the Runner so every failure path drains original callers
	// while the Runner and Store they depend on are still alive.
	t.Cleanup(func() {
		if v.runtime.Joined() {
			return
		}
		v.runtime.StopAdmission()
		ctx, cancel := context.WithTimeout(context.Background(), m1LiveJoinBudget)
		err := v.runtime.Drain(ctx)
		cancel()
		if err != nil {
			v.requestStop()
			v.root.cancel()
			t.Error("budget fixture original Runtime cleanup did not drain")
		}
	})
	w.responseSequence = func(t *testing.T) []netfixture.WireScenario {
		workAppCheck(t, "m1-budget-owned-command", os.WriteFile(filepath.Join(w.workspace, "m1-cancel.py"), []byte(m1CancelProgram), 0600))
		return []netfixture.WireScenario{
			m1FailureResponse(t, w, tc.RunnerRunCommandKey, map[string]any{"mount_id": w.mount.String(), "mode": "argv", "program": "/usr/bin/python3", "args": []string{"m1-cancel.py"}, "cwd": "."}),
			m1FailureResponse(t, w, tc.RunnerReadFileKey, map[string]any{"mount_id": w.mount.String(), "path": "input.json", "offset": "0", "length": 4096, "encoding": "utf8"}),
		}
	}
	w.createModelAndTask(t, "https://fixture.test:8443/m1-not-yet-scheduled")
	v.store.releaseDirectory()
	child, fd := w.awaitCancelChild(t)
	defer unix.Close(fd)
	var rawID string
	workAppCheck(t, "m1-budget-original-execution", v.admin.QueryRow(v.ctx, `SELECT id FROM agenteam_execution.executions WHERE project_id=$1 AND launch_request->'trigger'->>'task_id'=$2`, v.a.String(), w.task.ID.String()).Scan(&rawID))
	var err error
	w.execution, err = f.ParseID[id.Execution](rawID)
	workAppCheck(t, "m1-budget-original-execution-id", err)
	before := m1BudgetDispatchFacts(t, w)
	if before != [4]int{1, 1, 1, 1} {
		t.Fatalf("budget precondition executions/dispatches/sent/tools=%v", before)
	}
	pending := w.failurePending(t)
	connection := v.root.db.Connect(t)
	count := func(ctx context.Context) (int, error) {
		var sent int
		err := connection.QueryRow(ctx, m1LiveSentQuery, v.a.String()).Scan(&sent)
		return sent, err
	}
	budget := &m1LiveBudget{stop: make(chan struct{}), done: make(chan struct{})}
	deadline := make(chan time.Time, 1)
	t.Cleanup(func() {
		budget.once.Do(func() { close(budget.stop) })
		<-budget.done
	})
	go w.runLiveBudget(budget, deadline, nil, count)
	deadline <- time.Now()
	select {
	case <-budget.done:
	case <-time.After(m1LiveJoinBudget + time.Second):
		t.Fatal("budget owner did not return within its original drain allowance")
	}
	if budget.reason != "total_deadline" {
		t.Fatalf("budget must report incomplete: reason=%q", budget.reason)
	}
	usageAppWait(t, v.directory.returned, "m1-budget-manager-original-return")
	usageAppWait(t, v.root.done, "m1-budget-original-app-return")
	workAppCheck(t, "m1-budget-original-app-exit", v.root.err)
	if !v.runtime.Joined() || !v.runtime.executionCalls.Joined() || !v.runtime.schedulerCalls.Joined() || !v.planning.Joined() || !v.assembly.Joined() || !v.root.owned.producersJoined() {
		t.Fatal("budget returned before original Runtime/Model/Tool/App owners joined")
	}
	poll := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN}}
	n, err := unix.Poll(poll, 0)
	workAppCheck(t, "m1-budget-child-original-exit", err)
	if n != 1 || poll[0].Revents&unix.POLLIN == 0 || !errors.Is(syscall.Kill(child.PID, 0), syscall.ESRCH) || !errors.Is(syscall.Kill(-child.Group, 0), syscall.ESRCH) {
		t.Fatal("budget stopped without joining original command process/group")
	}
	w.awaitFailure(t, ec.Cancelled)
	w.checkFailureTerminal(t, ec.Cancelled, "cancelled", true)
	if !reflect.DeepEqual(pending, w.failurePending(t)) {
		t.Fatal("budget cancellation changed the original pending tool response")
	}
	if after := m1BudgetDispatchFacts(t, w); after != before {
		t.Fatalf("budget admitted additional execution/dispatch/model/tool work: before=%v after=%v", before, after)
	}
	w.checkFailurePeer(t)
	m1BudgetJoinRunner(t, w)
	taskExecutionGuardReleased(t, v.workAppFixture)
	t.Log("M1_LIVE_BUDGET_STOP incomplete=true reason=total_deadline observer_shared=true original_stop_drain_root_return=true original_model_tool_runner_join=true no_new_dispatch=true external_model=false")
}

func m1BudgetDispatchFacts(t *testing.T, w *m1Workflow) [4]int {
	t.Helper()
	var facts [4]int
	workAppCheck(t, "m1-budget-original-dispatch-facts", w.app.admin.QueryRow(w.app.ctx, `SELECT
 (SELECT count(*) FROM agenteam_execution.executions WHERE project_id=$1::text),
 (SELECT count(*) FROM agenteam_scheduler.dispatches WHERE project_id=$1::text),
 (SELECT count(*) FROM agenteam_model.invocations WHERE project_id=$1::uuid AND dispatch='sent'),
 (SELECT count(*) FROM agenteam_tool.operations WHERE project_id=$1::text)`, w.app.a.String()).Scan(&facts[0], &facts[1], &facts[2], &facts[3]))
	return facts
}

// The watchdog has already closed the App Store, so this observes the original
// Runner Wait and identity lock without asking a retired service for status.
func m1BudgetJoinRunner(t *testing.T, w *m1Workflow) {
	t.Helper()
	runner := w.runner
	workAppCheck(t, "m1-budget-runner-sigterm", runner.cmd.Process.Signal(syscall.SIGTERM))
	select {
	case <-runner.done:
	case <-time.After(5 * time.Second):
		t.Fatal("budget original Runner Wait exceeded its shutdown bound")
	}
	status, ok := runner.cmd.ProcessState.Sys().(syscall.WaitStatus)
	if !ok || !status.Exited() || status.ExitStatus() != 0 || runner.err != nil || !runner.stderr.event("shutdown_complete", "outcome", "drained") {
		t.Fatal("budget original Runner did not exit through drained shutdown")
	}
	t.Logf("m1_budget_runner_original_wait pid=%d exit=%d", runner.cmd.Process.Pid, status.ExitStatus())
	runner.proxy.stop(t)
	file, err := identity.Open(runner.identityPath)
	workAppCheck(t, "m1-budget-runner-identity-lock-released", err)
	workAppCheck(t, "m1-budget-runner-identity-close", file.Close())
}
