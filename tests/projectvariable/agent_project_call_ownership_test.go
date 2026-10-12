//go:build integration

package projectvariable_test

import (
	"bytes"
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/.agent-state/project-variables-independent/commitproxy"
	"github.com/LunaDeerTech/agenteam/internal/central/agent"
	ac "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/object"
	objc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

func TestAgentProjectCallOwnership(t *testing.T) {
	if path := os.Getenv(agentOwnershipChildEnv); path != "" {
		agentOwnershipChildMain(t, path)
		return
	}
	if !t.Run("local-original-return-and-terminal-fence", testAgentOwnershipLocal) {
		return
	}
	t.Run("foreign-death-before-original-sql-terminal", testAgentOwnershipForeign)
}

func testAgentOwnershipLocal(t *testing.T) {
	v := newAgentCallOwnershipFixture(t)
	seed, meta, identity := v.request(t)
	p, actor := v.p2.project, v.p2.base.ownerBrowser.actor
	created, err := v.agents.CreateAgent(ctxFor(t), actor, meta, p.ID, seed)
	if err != nil || created.Validate() != nil {
		t.Fatal("real owned seed creation", err)
	}
	process := mustAgentGuardProcess(t, v.guard)
	v.registeredState(t, process, "joined", 1)
	v.installOwnershipBarrier(t)
	// This is an explicit terminal-row fixture for a STILL LIVE caller. It
	// tests the actual late Project SH/running fence and is not foreign proof.
	late := beginAgentProjectCall(t, func(ctx context.Context) error {
		_, err := v.agents.GetAgent(ctx, actor, p.ID, seed.Fields().AgentID)
		return err
	})
	v.registeredState(t, process, "running", 1)
	key, _ := f.ProjectLock(p.ID.String())
	v.p2.base.tx(t, []f.LockRequest{{Key: key, Mode: f.Exclusive}}, func(ctx context.Context, _ f.Tx, x postgres.SQLExecutor) error {
		var callID, instance string
		if err := x.QueryRow(ctx, `SELECT id::text,instance_id::text FROM agenteam_agent.project_calls WHERE project_id=$1 AND process_id=$2 AND lane='service' AND class='read' AND phase='running'`, p.ID.String(), process.String()).Scan(&callID, &instance); err != nil {
			return err
		}
		tag, err := x.Exec(ctx, `UPDATE agenteam_agent.project_calls SET phase='joined',joined_at=GREATEST(clock_timestamp(),registered_at) WHERE id=$1 AND project_id=$2 AND process_id=$3 AND instance_id=$4 AND lane='service' AND class='read' AND phase='running'`, callID, p.ID.String(), process.String(), instance)
		if err == nil && tag.RowsAffected() != 1 {
			return errors.New("terminal fence fixture did not select the exact original call")
		}
		return err
	})
	late.join(t, false)
	requireCode(t, late.err, f.InvalidState)
	v.registeredState(t, process, "running", 0)
	foreign := v.foreignProject(t)
	mutation := beginAgentProjectCall(t, func(ctx context.Context) error {
		_, err := v.agents.CreateAgent(ctx, actor, meta, p.ID, seed)
		return err
	})
	read := beginAgentProjectCall(t, func(ctx context.Context) error {
		_, err := v.agents.GetAgent(ctx, actor, p.ID, seed.Fields().AgentID)
		return err
	})
	directory := beginAgentProjectCall(t, func(ctx context.Context) error {
		_, err := v.directory.GetAgent(ctx, actor, p.ID, seed.Fields().AgentID)
		return err
	})
	other := v.beginForeignRead(t, foreign.ID)
	cause := v.accept(t, pc.Archive)
	stopActor, scope := v.stopThroughDriver(t, cause, []*agentProjectCall{mutation, read, directory, other}, []bool{true, false, false, false})
	report, err := v.pair.ObserveStop(ctxFor(t), stopActor, cause, scope)
	if err != nil || !report.Matches(cause, scope) || report.Details().PendingCalls != 1 || report.Details().RegisteredJoined {
		t.Fatal("live original mutation lost durable ownership", err)
	}
	checkpointEntered, checkpointRelease := make(chan struct{}), newPhaseRelease()
	var once sync.Once
	var physical f.CommitResult
	s := v.p2.base.tracked
	s.mu.Lock()
	s.afterResult = func(c f.TransactionCause, r f.CommitResult) {
		if c.Details().Kind == f.RecoveryCause && c.Details().Owner == "agent-call-return" {
			once.Do(func() { physical = r; close(checkpointEntered); <-checkpointRelease.done })
		}
	}
	s.mu.Unlock()
	t.Cleanup(func() { checkpointRelease.release(); s.mu.Lock(); s.afterResult = nil; s.mu.Unlock() })
	mutation.release.release()
	phaseAwait(t, checkpointEntered)
	if physical.State() != f.Committed {
		t.Fatal("original retirement transaction did not actually commit")
	}
	mutation.requireHeld(t, true)
	report, err = v.pair.ObserveStop(ctxFor(t), stopActor, cause, scope)
	if err != nil || report.Details().RegisteredJoined || report.Details().PendingCalls != 1 {
		t.Fatal("disk joined hid the original checkpoint return", err)
	}
	checkpointRelease.release()
	phaseAwait(t, mutation.done)
	if mutation.err == nil {
		t.Fatal("original stopped mutation unexpectedly succeeded")
	}
	report, err = v.pair.ObserveStop(ctxFor(t), stopActor, cause, scope)
	if err != nil || !report.Details().RegisteredJoined || report.Details().PendingCalls != 0 {
		t.Fatal("actual mutation return did not settle", err)
	}
	for _, call := range []*agentProjectCall{read, directory, other} {
		call.requireHeld(t, false)
		call.join(t, true)
	}
	v.checkFacts(t, seed.Fields().AgentID, meta.IdempotencyKey, identity, [16]int64{1, 1, 0, 0, 2, 1, 1, 1, 0, 0, 1, 1, 1, 1, 1, 0}, true)
	v.requirePhase(t, cause, "stopping", "terminal")
}

func testAgentOwnershipForeign(t *testing.T) {
	v := newAgentCallOwnershipFixture(t)
	seed, createMeta, identity := v.request(t)
	p, actor := v.p2.project, v.p2.base.ownerBrowser.actor
	created, err := v.agents.CreateAgent(ctxFor(t), actor, createMeta, p.ID, seed)
	if err != nil || created.Validate() != nil {
		t.Fatal("actual seed command required for archived replay", err)
	}
	description := seed.Fields().Description
	update, err := ac.NewAgentUpdate(ac.AgentUpdateFields{Description: &description})
	if err != nil {
		t.Fatal(err)
	}
	version := created.Fields().Agent.Fields().Core.Version
	updateMeta := meta(t, id[struct{}](t).String(), &version)
	updated, err := v.agents.UpdateAgent(ctxFor(t), actor, updateMeta, p.ID, seed.Fields().AgentID, update)
	if err != nil || updated.Validate() != nil || updated.Fields().Changed || len(updated.Fields().EventIDs) != 0 || updated.Fields().Agent.Fields().Core.Version != version {
		t.Fatal("actual non-name noop Update prerequisite", err)
	}
	readUpdateReceipt := func() []byte {
		t.Helper()
		var receipt []byte
		if err := v.p2.base.raw.QueryRow(ctxFor(t), `SELECT receipt FROM agenteam_agent.commands WHERE project_id=$1 AND target_id=$2 AND command_name='agent.update' AND idempotency_key=$3 AND state='completed'`, p.ID.String(), seed.Fields().AgentID.String(), string(updateMeta.IdempotencyKey)).Scan(&receipt); err != nil {
			t.Fatal("original completed Update receipt", err)
		}
		return receipt
	}
	originalUpdateReceipt := readUpdateReceipt()
	cause := v.accept(t, pc.Archive)
	stopActor, scope := v.enterStopping(t, cause)
	// A historical completed non-name noop Update uses the Read-priority
	// replay path while archiving and retains Project SH (Create requires EX).
	// Its new call still has service/mutation ownership.
	// Registration commits first; the held COMMIT is the ORIGINAL business
	// prepare/replay Tx, not registration or a metadata retirement checkpoint.
	bucket := v.p2.bucket
	storage, err := agentOwnershipStorage(bucket)
	if err != nil {
		t.Fatal("original installation storage", agentOwnershipSafeCode(err))
	}
	spool := filepath.Join(t.TempDir(), "agent-call-spool")
	proxy, err := commitproxy.New(ctxFor(t), net.JoinHostPort("127.0.0.1", v.p2.base.db.Fixture.Port))
	if err != nil {
		t.Fatal("owned complete COMMIT proxy", err)
	}
	var child *agentOwnershipChild
	var runtime *object.Runtime
	var observer *agent.ProjectCallOwnership
	var observeDone chan struct{}
	var cancelObserve context.CancelFunc
	defer func() {
		if cancelObserve != nil {
			cancelObserve()
		}
		proxy.Release()
		if child != nil {
			joined := child.join(t)
			if !joined {
				t.Log("Agent child diagnostics unavailable: original Wait and reader not both joined")
				return
			}
			child.report(t)
		}
		if observeDone != nil {
			phaseAwait(t, observeDone)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := proxy.Close(ctx); err != nil {
			t.Error("original COMMIT proxy did not join", err)
			return
		}
		if observer != nil && !drainAgentCallPair(t, observer) {
			return // Never release its guard while an original owner remains.
		}
		phaseGuardRetireRuntime(t, runtime)
	}()
	oldProcess := id[objc.Process](t)
	child = startAgentOwnershipChild(t, agentOwnershipInput{Database: v.p2.base.db.Name, Proxy: proxy.Address(), Bucket: bucket, Spool: spool, Process: oldProcess.String(), Project: p.ID.String(), User: actor.Details().UserID, Session: actor.Details().SessionID, Target: seed.Fields().AgentID, Request: update, Meta: updateMeta})
	var writerPID int32
	select {
	case writerPID = <-child.pid:
	case <-child.done:
		t.Fatal("child original replay failed before COMMIT; original Wait and bounded diagnostics follow")
	case <-time.After(15 * time.Second):
		t.Fatal("child actual replay PID missing")
	}
	if err = proxy.Arm(writerPID); err != nil {
		t.Fatal(err)
	}
	if _, err = child.stdin.Write([]byte{1}); err != nil {
		t.Fatal("child original COMMIT arm handshake")
	}
	phaseAwait(t, proxy.Reached())
	phaseAwait(t, child.ready)
	if proxy.WriterPID() != writerPID {
		t.Fatal("proxy substituted original backend")
	}
	v.registeredState(t, oldProcess, "running", 1)
	var class string
	if err = v.p2.base.raw.QueryRow(ctxFor(t), `SELECT class FROM agenteam_agent.project_calls WHERE process_id=$1 AND project_id=$2 AND phase='running'`, oldProcess.String(), p.ID.String()).Scan(&class); err != nil || class != "mutation" {
		t.Fatal("completed replay lost its original mutation class", err)
	}
	newProcess := id[objc.Process](t)
	var guard *object.ProcessGuard
	runtime, _, guard = phaseGuardRuntime(t, v.p2.base.tracked, v.p2.base.audit, storage, spool, newProcess)
	if err = runtime.Initialize(ctxFor(t)); err != nil {
		t.Fatal("real observer guard initialize", err)
	}
	observer, err = v.graph.NewProjectCallOwnership(v.p2.base.keys, &phaseGuardProcesses{guard: guard, process: newProcess}, guard, newProcess)
	if err != nil {
		t.Fatal(err)
	}
	requireCode(t, guard.ConfirmStopped(ctxFor(t), oldProcess), f.ResourceBusy)
	if err = child.cmd.Process.Kill(); err != nil {
		t.Fatal("explicit child SIGKILL", err)
	}
	phaseAwait(t, child.done)
	phaseAwait(t, child.readDone)
	// Child status comes from its one original cmd.Wait.
	if child.cmd.ProcessState == nil {
		t.Fatal("original child Wait missing")
	}
	status, ok := child.cmd.ProcessState.Sys().(syscall.WaitStatus)
	if !ok || !status.Signaled() || status.Signal() != syscall.SIGKILL || child.waitErr == nil {
		t.Fatal("child did not exit through the requested SIGKILL")
	}
	if err = guard.ConfirmStopped(ctxFor(t), oldProcess); err != nil {
		t.Fatal("actual guard did not prove exact waited process", err)
	}
	select {
	case <-proxy.Committed():
		t.Fatal("original upstream COMMIT was forwarded early")
	default:
	}
	key, _ := f.ProjectLock(p.ID.String())
	waiterPID := make(chan int32, 1)
	s := v.p2.base.tracked
	s.mu.Lock()
	s.beforeLocks = func(ctx context.Context, tx f.Tx, locks []f.LockRequest) error {
		if len(locks) == 1 && locks[0].Mode == f.Exclusive && locks[0].Key.AdvisoryKey() == key.AdvisoryKey() {
			x, err := s.InTx(tx)
			if err != nil {
				return err
			}
			var pid int32
			if err = x.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&pid); err != nil {
				return err
			}
			waiterPID <- pid
		}
		return nil
	}
	s.mu.Unlock()
	defer func() { s.mu.Lock(); s.beforeLocks = nil; s.mu.Unlock() }()
	var report agent.ProjectCallOwnershipReport
	var observeErr error
	var observeCtx context.Context
	observeCtx, cancelObserve = context.WithTimeout(context.Background(), 12*time.Second)
	observeDone = make(chan struct{})
	go func() {
		defer close(observeDone)
		report, observeErr = observer.ObserveStop(observeCtx, stopActor, cause, scope)
	}()
	var waiter int32
	select {
	case waiter = <-waiterPID:
	case <-observeDone:
		t.Fatal("observer did not reach original EX terminal cut", observeErr)
	case <-time.After(5 * time.Second):
		t.Fatal("observer actual SQL PID missing")
	}
	agentOwnershipLockWait(t, v.p2.base.raw, key, writerPID, waiter)
	select {
	case <-observeDone:
		t.Fatal("dead process substituted for original SQL terminal", observeErr)
	default:
	}
	v.registeredState(t, oldProcess, "running", 1)
	proxy.Release()
	phaseAwait(t, proxy.Committed())
	phaseAwait(t, proxy.HeldJoined())
	phaseAwait(t, observeDone)
	if observeErr != nil || !report.Matches(cause, scope) || !report.Details().RegisteredJoined || report.Details().PendingCalls != 0 {
		t.Fatal("actual SQL terminal cut failed to retire exact call", observeErr)
	}
	v.registeredState(t, oldProcess, "joined", 1)
	v.checkFacts(t, seed.Fields().AgentID, createMeta.IdempotencyKey, identity, [16]int64{1, 1, 0, 0, 2, 1, 1, 1, 0, 0, 1, 1, 1, 1, 1, 0}, true)
	if !bytes.Equal(originalUpdateReceipt, readUpdateReceipt()) {
		t.Fatal("historical Update replay changed its original receipt")
	}
	v.requirePhase(t, cause, "stopping", "terminal")
	t.Logf("Agent call child actual_wait pid=%d signal=SIGKILL original_backend=%d observer_backend=%d proxy_committed=true proxy_held_joined=true", child.cmd.Process.Pid, writerPID, waiter)
}
