//go:build integration

package projectvariable_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/.agent-state/project-variables-independent/commitproxy"
	"github.com/LunaDeerTech/agenteam/internal/central/account"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	objc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	pv "github.com/LunaDeerTech/agenteam/internal/central/projectvariable"
	vc "github.com/LunaDeerTech/agenteam/internal/central/projectvariable/contract"
	pgfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/postgres"
)

const variableOwnershipChildEnv = "AGENTEAM_VARIABLE_CALL_OWNERSHIP_CHILD"
const variableOwnershipChildReady = "VARIABLE_CALL_ORIGINAL_UNKNOWN_GUARD_HELD"
const variableOwnershipChildRegistered = "VARIABLE_CALL_REGISTERED_SPOOL_RELEASED"
const variableOwnershipChildPID = "VARIABLE_CALL_COMMIT_PID:"
const variableOwnershipChildDiagnostic = "VARIABLE_CALL_DIAGNOSTIC:"

type variableOwnershipInput struct {
	Database, Proxy, Bucket, Spool, Process, Project, User, Session string
	Request                                                         vc.VariableCreate
	Meta                                                            f.CommandMeta
}
type variableOwnershipChild struct {
	*phaseGuardChild
	registered           chan struct{}
	pid                  chan int32
	diagnosticMu         sync.Mutex
	diagnostics          []string
	scannerCode          string
	diagnosticsTruncated bool
}

// Only closed stage names, stable Fault codes and bounded status tokens leave
// this child. The parent retains no raw errors, stderr, DSNs or private input.
func variableOwnershipSafeCode(err error) string {
	if err == nil {
		return "OK"
	}
	var fault *f.Fault
	if errors.As(err, &fault) {
		return string(fault.Code.Safe())
	}
	if errors.Is(err, context.Canceled) {
		return "CONTEXT_CANCELED"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "DEADLINE_EXCEEDED"
	}
	return "NON_FAULT_ERROR"
}

func variableOwnershipDiagnosticAllowed(stage, code string) bool {
	switch stage {
	case "input", "fixture", "database", "accounts", "identity", "storage", "runtime", "install", "skill", "assembly", "ownership", "replay", "registration-return", "business-callback", "business-return", "original-result", "release-spool", "ready", "cleanup":
	default:
		return false
	}
	switch code {
	case "ENTER", "FAILED", "RETURNED", "OK", "CONTEXT_CANCELED", "DEADLINE_EXCEEDED", "NON_FAULT_ERROR", "not_started", "not_committed", "committed", "unknown":
		return true
	}
	return f.Code(code).Known()
}

func variableOwnershipEmit(stage, code string) {
	if variableOwnershipDiagnosticAllowed(stage, code) {
		fmt.Printf("%s%s:%s\n", variableOwnershipChildDiagnostic, stage, code)
	}
}

func (child *variableOwnershipChild) report(t *testing.T) {
	t.Helper()
	child.diagnosticMu.Lock()
	trace := append([]string(nil), child.diagnostics...)
	scan, truncated := child.scannerCode, child.diagnosticsTruncated
	child.diagnosticMu.Unlock()
	waited, code := false, -1
	select {
	case <-child.done:
		waited = true
		if child.cmd.ProcessState != nil {
			code = child.cmd.ProcessState.ExitCode()
		}
	default:
	}
	t.Logf("Variable child original_wait=%t exit=%d scanner=%s truncated=%t diagnostics=%q", waited, code, scan, truncated, trace)
	if scan == "SCANNER_ERROR" || truncated {
		t.Error("bounded child diagnostic stream did not complete")
	}
}

// Only the selector/input/closed protocol differ from the existing real guard
// child. Its single original cmd.Wait and pipe reader are retained by join.
func startVariableOwnershipChild(t *testing.T, input variableOwnershipInput) *variableOwnershipChild {
	t.Helper()
	raw, err := json.Marshal(input)
	if err != nil {
		t.Fatal("Variables fixture step failed", variableOwnershipSafeCode(err))
	}
	path := filepath.Join(t.TempDir(), "variable-call-child.json")
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		t.Fatal("Variables fixture step failed", variableOwnershipSafeCode(err))
	}
	_, writeErr := file.Write(raw)
	closeErr := file.Close()
	if writeErr != nil || closeErr != nil {
		t.Fatal("private Variable child input write")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestVariableProjectCallOwnership$", "-test.timeout=40s")
	cmd.Env = append(os.Environ(), variableOwnershipChildEnv+"="+path)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		t.Fatal("Variables fixture step failed", variableOwnershipSafeCode(err))
	}
	reader, writer, err := os.Pipe()
	if err != nil {
		cancel()
		_ = stdin.Close()
		t.Fatal("Variables fixture step failed", variableOwnershipSafeCode(err))
	}
	cmd.Stdout, cmd.Stderr = writer, writer
	if err = cmd.Start(); err != nil {
		cancel()
		_ = stdin.Close()
		_ = reader.Close()
		_ = writer.Close()
		t.Fatal("Variables fixture step failed", variableOwnershipSafeCode(err))
	}
	_ = writer.Close()
	child := &variableOwnershipChild{phaseGuardChild: &phaseGuardChild{cmd: cmd, cancel: cancel, stdin: stdin, done: make(chan struct{}), readDone: make(chan struct{}), ready: make(chan struct{})}, registered: make(chan struct{}), pid: make(chan int32, 1), scannerCode: "PENDING"}
	go func() {
		defer close(child.readDone)
		defer reader.Close()
		scanner := bufio.NewScanner(reader)
		scanner.Buffer(make([]byte, 1024), 64*1024)
		var readyOnce, pidOnce, registeredOnce sync.Once
		for scanner.Scan() {
			line := scanner.Text()
			if strings.HasPrefix(line, variableOwnershipChildDiagnostic) {
				entry := strings.TrimPrefix(line, variableOwnershipChildDiagnostic)
				stage, code, ok := strings.Cut(entry, ":")
				if ok && variableOwnershipDiagnosticAllowed(stage, code) {
					child.diagnosticMu.Lock()
					if len(child.diagnostics) < 32 {
						child.diagnostics = append(child.diagnostics, entry)
					} else {
						child.diagnosticsTruncated = true
					}
					child.diagnosticMu.Unlock()
				}
			}
			if line == variableOwnershipChildRegistered {
				registeredOnce.Do(func() { close(child.registered) })
			}
			if line == variableOwnershipChildReady {
				readyOnce.Do(func() { close(child.ready) })
			}
			if strings.HasPrefix(line, variableOwnershipChildPID) {
				n, err := strconv.ParseInt(strings.TrimPrefix(line, variableOwnershipChildPID), 10, 32)
				if err == nil && n > 0 {
					pidOnce.Do(func() { child.pid <- int32(n) })
				}
			}
		}
		child.diagnosticMu.Lock()
		child.scannerCode = "EOF"
		if scanner.Err() != nil {
			child.scannerCode = "SCANNER_ERROR"
		}
		child.diagnosticMu.Unlock()
	}()
	go func() { child.waitErr = cmd.Wait(); close(child.done) }()
	return child
}

func variableOwnershipChildMain(t *testing.T, path string) {
	t.Helper()
	stage := "input"
	setStage := func(s string) { stage = s; variableOwnershipEmit(stage, "ENTER") }
	fail := func(err error) {
		variableOwnershipEmit(stage, variableOwnershipSafeCode(err))
		t.Fatal("Variables child failed; closed diagnostic emitted")
	}
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 || info.Size() > 65536 {
		t.Fatal("invalid private Variables child input")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		fail(err)
	}
	var input variableOwnershipInput
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&input) != nil || decoder.Decode(new(any)) != io.EOF || !filepath.IsAbs(input.Spool) || input.Request.Validate() != nil || input.Meta.Validate() != nil {
		t.Fatal("invalid Variables child shape")
	}
	host, _, err := net.SplitHostPort(input.Proxy)
	if err != nil || host != "127.0.0.1" {
		t.Fatal("child proxy is not owned loopback")
	}
	setStage("fixture")
	remote, err := pgfixture.Load()
	if err != nil {
		fail(err)
	}
	db := &pgfixture.Database{Fixture: remote, Name: input.Database}
	u, err := url.Parse(remote.URL(db.Name))
	if err != nil {
		fail(err)
	}
	u.Host = input.Proxy
	setStage("database")
	raw := openStore(t, db.Config(t, map[string]string{"URL": u.String(), "TLS_MODE": "disable", "CA_FILE": ""}))
	store := &hookStore{fixtureStore: raw}
	ak, keys := keys(t)
	accounts, err := account.NewAuthority(store, ak)
	if err != nil {
		fail(err)
	}
	if err = accounts.Initialize(ctxFor(t)); err != nil {
		fail(err)
	}
	process, err := f.ParseID[objc.Process](input.Process)
	if err != nil {
		fail(err)
	}
	projectID, err := f.ParseID[i.Project](input.Project)
	if err != nil {
		fail(err)
	}
	user, err := f.ParseID[i.User](input.User)
	if err != nil {
		fail(err)
	}
	session, err := f.ParseID[i.Session](input.Session)
	if err != nil {
		fail(err)
	}
	actor, err := i.NewHuman(user, session)
	if err != nil {
		fail(err)
	}
	storage, err := agentOwnershipStorage(input.Bucket)
	if err != nil {
		fail(err)
	}
	setStage("runtime")
	owned, err := newAgentOwnershipRuntime(store, accounts, keys, storage, input.Spool, process)
	var pair *pv.ProjectCallOwnership
	var deps variableOwnershipDependencies
	defer func() {
		variableOwnershipDrain(t, pair)
		if deps.d04 != nil {
			deps.d04.StopMaintenance()
		}
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := owned.drain(ctx); err != nil {
			owned.retain()
			t.Error("Variables child original graph did not join", variableOwnershipSafeCode(err))
			select {}
		}
	}()
	if err != nil {
		fail(err)
	}
	if err = owned.runtime.Initialize(ctxFor(t)); err != nil {
		fail(err)
	}
	manifest, err := agentOwnershipManifest()
	if err != nil {
		fail(err)
	}
	deps, err = variableOwnershipAssemble(store, accounts, keys, manifest, process)
	if err != nil {
		fail(err)
	}
	if err = deps.d04.Initialize(ctxFor(t)); err != nil {
		fail(err)
	}
	pair, err = pv.NewProjectCallOwnership(store, deps.ordinary, deps.secrets, owned.guard, process)
	if err != nil {
		fail(err)
	}
	command, err := vc.VariableCommandIdentity(projectID, vc.CreateCommand, input.Meta.IdempotencyKey)
	if err != nil {
		fail(err)
	}
	registered, original := variableOwnershipNewBarrier(), variableOwnershipNewBarrier()
	done := make(chan struct{})
	var callErr error
	defer func() { registered.release.release(); original.release.release(); variableOwnershipWait(t, done) }()
	inputReader := bufio.NewReader(os.Stdin)
	store.mu.Lock()
	store.after = func(ctx context.Context, tx f.Tx, cause f.TransactionCause) error {
		if cause.Kind() != f.CommandsCause || cause.Details().Primary.Canonical() != command.Canonical() {
			return nil
		}
		x, err := store.InTx(tx)
		if err != nil {
			return err
		}
		key, _ := f.ProjectLock(projectID.String())
		if err = store.RequireHeldLocks(ctx, tx, []f.LockRequest{{Key: key, Mode: f.Exclusive}}); err != nil {
			return err
		}
		var completed bool
		if err = x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_projectvariable.commands WHERE project_id=$1 AND command_name=$2 AND idempotency_key=$3 AND state='completed')`, projectID.String(), string(vc.CreateCommand), string(input.Meta.IdempotencyKey)).Scan(&completed); err != nil {
			return err
		}
		if !completed {
			return errors.New("Variables child did not take original completed replay")
		}
		var pid int32
		if err = x.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&pid); err != nil {
			return err
		}
		fmt.Printf("%s%d\n", variableOwnershipChildPID, pid)
		if _, err = inputReader.ReadByte(); err != nil {
			return errors.New("Variables original COMMIT arm handshake missing")
		}
		return nil
	}
	store.afterResult = func(cause f.TransactionCause, result f.CommitResult) {
		if cause.Details().Owner == "projectvariable-call-register" {
			registered.hold(nil, 0, result)
		}
		if cause.Kind() == f.CommandsCause && cause.Details().Primary.Canonical() == command.Canonical() {
			original.hold(nil, 0, result)
		}
	}
	store.mu.Unlock()
	setStage("replay")
	go func() {
		defer close(done)
		_, callErr = pair.Service().CreateVariable(ctxFor(t), actor, input.Meta, projectID, input.Request)
	}()
	select {
	case <-registered.entered:
	case <-done:
		fail(callErr)
	case <-time.After(12 * time.Second):
		t.Fatal("child original registration return missing")
	}
	if registered.physical.State() != f.Committed {
		t.Fatal("child registration did not actually commit")
	}
	// Variables borrows only the still-bound guard/Store. Release the original
	// idle Object spool BEFORE business, allowing the observer to initialize on
	// the exact same spool inode. This is NOT Runtime.Drain or guard release.
	setStage("release-spool")
	owned.objects.StopAdmission()
	if err = owned.objects.Drain(ctxFor(t)); err != nil {
		fail(err)
	}
	if current, err := owned.guard.CurrentProcess(); err != nil || current != process {
		t.Fatal("child guard lost before original business")
	}
	fmt.Println(variableOwnershipChildRegistered)
	if _, err = inputReader.ReadByte(); err != nil {
		fail(err)
	}
	registered.release.release()
	select {
	case <-original.entered:
	case <-done:
		fail(callErr)
	case <-time.After(12 * time.Second):
		t.Fatal("child original business result missing")
	}
	if original.physical.State() != f.Unknown || original.physical.AttemptID().Validate() != nil {
		t.Fatal("child original COMMIT loss is not actual Unknown")
	}
	setStage("ready")
	fmt.Println(variableOwnershipChildReady)
	_, _ = inputReader.ReadByte() // Parent kills and Waits; original call/guard remain owned.
}

func variableOwnershipLockWait(t *testing.T, raw *postgres.Store, key f.LockKey, holder, waiter int32) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	tick := time.NewTicker(5 * time.Millisecond)
	defer tick.Stop()
	bits := uint64(key.AdvisoryKey())
	for {
		var blocked bool
		err := raw.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_locks w JOIN pg_locks h ON h.locktype=w.locktype AND h.database=w.database AND h.classid=w.classid AND h.objid=w.objid AND h.objsubid=w.objsubid JOIN pg_stat_activity a ON a.pid=w.pid WHERE a.datname=current_database() AND w.locktype='advisory' AND w.classid::bigint=$1 AND w.objid::bigint=$2 AND w.objsubid=1 AND w.pid=$3 AND NOT w.granted AND w.mode='ExclusiveLock' AND h.pid=$4 AND h.granted AND h.mode='ExclusiveLock' AND $4::int=ANY(pg_blocking_pids(w.pid)))`, int64(bits>>32), int64(bits&0xffffffff), waiter, holder).Scan(&blocked)
		if err != nil {
			t.Fatal("Variables exact original EX lock observation", variableOwnershipSafeCode(err))
		}
		if blocked {
			return
		}
		select {
		case <-tick.C:
		case <-ctx.Done():
			t.Fatal("Variables original EX did not block observer EX")
		}
	}
}

func testVariableOwnershipForeign(t *testing.T) {
	v := newVariableOwnershipFixture(t)
	p, actor := v.p2.project, v.p2.base.ownerBrowser.actor
	in := createInput(t, "FOREIGN_REPLAY", "nonempty-original")
	m := meta(t, "foreign-original-create", nil)
	variableOwnershipRun(t, v, p.ID, "ordinary", "mutation", func(ctx context.Context) error {
		_, err := v.pair.Service().CreateVariable(ctx, actor, m, p.ID, in)
		return err
	})
	cause, scope, stopActor := v.phase(t, p, pc.Archive)
	before := variableOwnershipSnapshot(t, v.p2.base.raw, p.ID, true)
	storage, err := agentOwnershipStorage(v.p2.bucket)
	if err != nil {
		t.Fatal("Variables exact owned storage", variableOwnershipSafeCode(err))
	}
	spool := filepath.Join(t.TempDir(), "variable-call-spool")
	proxy, err := commitproxy.New(ctxFor(t), net.JoinHostPort("127.0.0.1", v.p2.base.db.Fixture.Port))
	if err != nil {
		t.Fatal("Variables fixture step failed", variableOwnershipSafeCode(err))
	}
	var child *variableOwnershipChild
	var owned *agentOwnershipRuntime
	var observer *pv.ProjectCallOwnership
	var observeDone chan struct{}
	var cancelObserve context.CancelFunc
	capture := variableOwnershipNewBarrier()
	defer func() {
		capture.release.release()
		proxy.Release()
		if cancelObserve != nil {
			cancelObserve()
		}
		if child != nil {
			if !child.join(t) {
				t.Error("Variables child original Wait/reader unresolved")
				select {}
			}
			child.report(t)
		}
		if observeDone != nil {
			variableOwnershipWait(t, observeDone)
		}
		variableOwnershipDrain(t, observer)
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := proxy.Close(ctx); err != nil {
			t.Error("Variables original proxy did not join")
			select {}
		}
		if err := owned.drain(ctx); err != nil {
			owned.retain()
			t.Error("Variables observer graph did not join", variableOwnershipSafeCode(err))
			select {}
		}
	}()
	oldProcess := id[objc.Process](t)
	child = startVariableOwnershipChild(t, variableOwnershipInput{Database: v.p2.base.db.Name, Proxy: proxy.Address(), Bucket: v.p2.bucket, Spool: spool, Process: oldProcess.String(), Project: p.ID.String(), User: actor.Details().UserID, Session: actor.Details().SessionID, Request: in, Meta: m})
	select {
	case <-child.registered:
	case <-child.done:
		t.Fatal("Variables child exited before registration")
	case <-time.After(15 * time.Second):
		t.Fatal("Variables child registration/spool handoff missing")
	}
	var callID string
	if err = v.p2.base.raw.QueryRow(ctxFor(t), `SELECT id::text FROM agenteam_projectvariable.project_calls WHERE project_id=$1 AND process_id=$2 AND lane='ordinary' AND class='mutation' AND phase='running'`, p.ID.String(), oldProcess.String()).Scan(&callID); err != nil {
		t.Fatal("Variables actual child registration", variableOwnershipSafeCode(err))
	}
	newProcess := id[objc.Process](t)
	owned, err = newAgentOwnershipRuntime(v.p2.base.tracked, v.p2.base.accounts, v.p2.base.keys, storage, spool, newProcess)
	if err != nil {
		t.Fatal("Variables observer full graph", variableOwnershipSafeCode(err))
	}
	if err = owned.runtime.Initialize(ctxFor(t)); err != nil {
		t.Fatal("Variables observer actual runtime initialize", variableOwnershipSafeCode(err))
	}
	observer, err = pv.NewProjectCallOwnership(v.p2.base.tracked, v.deps.ordinary, v.deps.secrets, owned.guard, newProcess)
	if err != nil {
		t.Fatal("Variables fixture step failed", variableOwnershipSafeCode(err))
	}
	requireCode(t, owned.guard.ConfirmStopped(ctxFor(t), oldProcess), f.ResourceBusy)
	key, _ := f.ProjectLock(p.ID.String())
	waiterPID := make(chan int32, 1)
	s := v.p2.base.tracked
	s.mu.Lock()
	oldAfter, oldBefore := s.afterResult, s.beforeLocks
	s.afterResult = func(c f.TransactionCause, r f.CommitResult) {
		if oldAfter != nil {
			oldAfter(c, r)
		}
		if c.Details().Owner == "projectvariable-call-observe" && c.Details().RecoveryRunID == cause.OperationID.String() {
			capture.hold(nil, 0, r)
		}
	}
	s.beforeLocks = func(ctx context.Context, tx f.Tx, locks []f.LockRequest) error {
		if oldBefore != nil {
			if err := oldBefore(ctx, tx, locks); err != nil {
				return err
			}
		}
		if ctx.Value(variableOwnershipObserveKey{}) == capture && len(locks) == 1 && locks[0].Mode == f.Exclusive && locks[0].Key.AdvisoryKey() == key.AdvisoryKey() {
			x, err := s.InTx(tx)
			if err != nil {
				return err
			}
			var pid int32
			if err = x.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&pid); err != nil {
				return err
			}
			select {
			case waiterPID <- pid:
			default:
			}
		}
		return nil
	}
	s.mu.Unlock()
	defer func() { s.mu.Lock(); s.afterResult, s.beforeLocks = oldAfter, oldBefore; s.mu.Unlock() }()
	var report pv.ProjectBusinessStopReport
	var observeErr error
	observeCtx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	observeCtx = context.WithValue(observeCtx, variableOwnershipObserveKey{}, capture)
	cancelObserve = cancel
	observeDone = make(chan struct{})
	go func() {
		defer close(observeDone)
		report, observeErr = observer.ObserveStop(observeCtx, stopActor, cause, scope)
	}()
	select {
	case <-capture.entered:
	case <-observeDone:
		t.Fatal("Variables capture returned early", variableOwnershipSafeCode(observeErr))
	case <-time.After(3 * time.Second):
		t.Fatal("Variables capture original return barrier missing")
	}
	if capture.physical.State() != f.Committed {
		t.Fatal("Variables capture was not physically committed")
	}
	// Capture's SH is now physically released, but its original return has
	// not reached the provider. Only now may the child acquire its original EX.
	if _, err = child.stdin.Write([]byte{1}); err != nil {
		t.Fatal("Variables child business handoff")
	}
	var writerPID int32
	select {
	case writerPID = <-child.pid:
	case <-child.done:
		t.Fatal("Variables original business callback failed")
	case <-time.After(3 * time.Second):
		t.Fatal("Variables original writer PID missing")
	}
	if err = proxy.Arm(writerPID); err != nil {
		t.Fatal("Variables fixture step failed", variableOwnershipSafeCode(err))
	}
	if _, err = child.stdin.Write([]byte{1}); err != nil {
		t.Fatal("Variables original COMMIT arm acknowledgement")
	}
	phaseAwait(t, proxy.Reached())
	phaseAwait(t, child.ready)
	if proxy.WriterPID() != writerPID {
		t.Fatal("Variables proxy changed original backend")
	}
	if err = child.cmd.Process.Kill(); err != nil {
		t.Fatal("Variables explicit child SIGKILL")
	}
	variableOwnershipWait(t, child.done)
	variableOwnershipWait(t, child.readDone)
	if child.cmd.ProcessState == nil {
		t.Fatal("Variables original child Wait missing")
	}
	status, ok := child.cmd.ProcessState.Sys().(syscall.WaitStatus)
	if !ok || !status.Signaled() || status.Signal() != syscall.SIGKILL || child.waitErr == nil {
		t.Fatal("Variables child did not exit through original SIGKILL/Wait")
	}
	if err = owned.guard.ConfirmStopped(ctxFor(t), oldProcess); err != nil {
		t.Fatal("Variables actual guard death proof", variableOwnershipSafeCode(err))
	}
	capture.release.release()
	var waiter int32
	select {
	case waiter = <-waiterPID:
	case <-observeDone:
		t.Fatal("Variables observer missed final EX", variableOwnershipSafeCode(observeErr))
	case <-time.After(3 * time.Second):
		t.Fatal("Variables final EX PID missing")
	}
	variableOwnershipLockWait(t, v.p2.base.raw, key, writerPID, waiter)
	select {
	case <-observeDone:
		t.Fatal("Variables process death replaced upstream SQL terminal")
	default:
	}
	variableOwnershipRow(t, v.p2.base.raw, callID, p.ID.String(), oldProcess.String(), "ordinary", "mutation", "running")
	proxy.Release()
	phaseAwait(t, proxy.Committed())
	phaseAwait(t, proxy.HeldJoined())
	variableOwnershipWait(t, observeDone)
	if observeErr != nil || !report.Matches(cause, scope) || !report.Details().RegisteredProjectBusinessJoined || report.Details().PendingCalls != 0 || report.Details().More {
		t.Fatal("Variables actual final EX did not close exact call", variableOwnershipSafeCode(observeErr))
	}
	variableOwnershipRow(t, v.p2.base.raw, callID, p.ID.String(), oldProcess.String(), "ordinary", "mutation", "business_closed")
	if variableOwnershipSnapshot(t, v.p2.base.raw, p.ID, true) != before {
		t.Fatal("Variables foreign replay changed original business history")
	}
	t.Logf("variable_project_call_ownership foreign_actual_wait=true signal=SIGKILL original_backend=%d observer_backend=%d original_project_lock=EX proxy_committed=true proxy_held_joined=true project_business_only=true", writerPID, waiter)
}

// A fresh real Store is assembled through the existing complete-frame proxy
// BEFORE its Runtime/guard and all Variables/D04 authorities. No same-DSN
// authority or fake current-process witness is substituted after construction.
func variableOwnershipConfirm(t *testing.T, parent *variableOwnershipFixture, lane string) {
	t.Helper()
	p := parent.project(t, "variable-confirm-"+lane)
	proxy, err := commitproxy.New(ctxFor(t), net.JoinHostPort("127.0.0.1", parent.p2.base.db.Fixture.Port))
	if err != nil {
		t.Fatal("Variables fixture step failed", variableOwnershipSafeCode(err))
	}
	var owned *agentOwnershipRuntime
	var pair *pv.ProjectCallOwnership
	var deps variableOwnershipDependencies
	var call *variableOwnershipCall
	confirmation := variableOwnershipNewBarrier()
	defer func() {
		proxy.Release()
		confirmation.release.release()
		if call != nil {
			call.cancel()
			variableOwnershipWait(t, call.done)
		}
		variableOwnershipDrain(t, pair)
		if deps.d04 != nil {
			deps.d04.StopMaintenance()
		}
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := owned.drain(ctx); err != nil {
			owned.retain()
			t.Error("Variables confirmation original graph remains live", variableOwnershipSafeCode(err))
			select {}
		}
		if err := proxy.Close(ctx); err != nil {
			t.Error("Variables confirmation proxy remains live")
			select {}
		}
	}()
	u, err := url.Parse(parent.p2.base.db.Fixture.URL(parent.p2.base.db.Name))
	if err != nil {
		t.Fatal("owned confirmation URL")
	}
	u.Host = proxy.Address()
	raw := openStore(t, parent.p2.base.db.Config(t, map[string]string{"URL": u.String(), "TLS_MODE": "disable", "CA_FILE": ""}))
	store := &hookStore{fixtureStore: raw}
	ak, keys := keys(t)
	accounts, err := account.NewAuthority(store, ak)
	if err != nil {
		t.Fatal("Variables fixture step failed", variableOwnershipSafeCode(err))
	}
	if err = accounts.Initialize(ctxFor(t)); err != nil {
		t.Fatal("Variables fixture step failed", variableOwnershipSafeCode(err))
	}
	storage, err := agentOwnershipStorage(parent.p2.bucket)
	if err != nil {
		t.Fatal("original confirmation storage", variableOwnershipSafeCode(err))
	}
	process := id[objc.Process](t)
	owned, err = newAgentOwnershipRuntime(store, accounts, keys, storage, filepath.Join(t.TempDir(), "confirmation-spool"), process)
	if err != nil {
		t.Fatal("confirmation full graph", variableOwnershipSafeCode(err))
	}
	if err = owned.runtime.Initialize(ctxFor(t)); err != nil {
		t.Fatal("confirmation actual Runtime.Initialize", variableOwnershipSafeCode(err))
	}
	deps, err = variableOwnershipAssemble(store, accounts, keys, parent.p2.manifest, process)
	if err != nil {
		t.Fatal("Variables fixture step failed", variableOwnershipSafeCode(err))
	}
	if err = deps.d04.Initialize(ctxFor(t)); err != nil {
		t.Fatal("confirmation actual D04 initialize", variableOwnershipSafeCode(err))
	}
	pair, err = pv.NewProjectCallOwnership(store, deps.ordinary, deps.secrets, owned.guard, process)
	if err != nil {
		t.Fatal("Variables fixture step failed", variableOwnershipSafeCode(err))
	}
	base := *parent.p2.base
	base.raw, base.tracked, base.accounts, base.keys = raw, store, accounts, keys
	p2 := *parent.p2
	p2.base = &base
	remote := &variableOwnershipFixture{p2: &p2, deps: deps, guard: owned.guard, process: process, pair: pair}
	remote.installHooks(t)
	plain, private := createInput(t, "CONFIRM_PLAIN", "nonempty"), secretCreateInput(t, "CONFIRM_SECRET", []byte("nonempty-private-confirmation"))
	m := meta(t, "confirmation-"+lane, nil)
	var identity f.CommandIdentity
	if lane == "ordinary" {
		identity, err = vc.VariableCommandIdentity(p.ID, vc.CreateCommand, m.IdempotencyKey)
	} else {
		identity, err = vc.SecretVariableCommandIdentity(p.ID, vc.SecretCreateCommand, m.IdempotencyKey)
	}
	if err != nil {
		t.Fatal("Variables fixture step failed", variableOwnershipSafeCode(err))
	}
	var armed, unknown bool
	var mu sync.Mutex
	var original f.CommitResult
	originalCall := &variableOwnershipCall{}
	store.mu.Lock()
	oldAfter, oldResult, oldLocks := store.after, store.afterResult, store.beforeLocks
	store.after = func(ctx context.Context, tx f.Tx, cause f.TransactionCause) error {
		if oldAfter != nil {
			if err := oldAfter(ctx, tx, cause); err != nil {
				return err
			}
		}
		if cause.Kind() != f.CommandsCause || cause.Details().Primary.Canonical() != identity.Canonical() {
			return nil
		}
		mu.Lock()
		already := armed
		mu.Unlock()
		if already {
			return nil
		}
		x, err := store.InTx(tx)
		if err != nil {
			return err
		}
		var complete bool
		query := `SELECT EXISTS(SELECT 1 FROM agenteam_projectvariable.commands c JOIN agenteam_projectvariable.history h ON h.operation_id=c.id AND h.event_id=c.event_id JOIN agenteam_audit.audit_records a ON a.id=(c.receipt->>'audit_id')::uuid JOIN agenteam_outbox.events e ON e.id=c.event_id WHERE c.project_id=$1 AND c.command_name=$2 AND c.idempotency_key=$3 AND c.state='completed')`
		if lane == "secret" {
			query = `SELECT EXISTS(SELECT 1 FROM agenteam_projectvariable.secret_commands c JOIN agenteam_projectvariable.secret_history h ON h.operation_id=c.id AND h.event_id=c.event_id JOIN agenteam_secret.project_variable_receipts r ON r.id=c.d04_receipt_id AND r.project_id=c.project_id AND r.variable_id=c.target_id JOIN agenteam_audit.audit_records a ON a.id=c.audit_id JOIN agenteam_outbox.events e ON e.id=c.event_id WHERE c.project_id=$1 AND c.command_name=$2 AND c.idempotency_key=$3)`
		}
		if err = x.QueryRow(ctx, query, p.ID.String(), identity.Command(), string(identity.Key())).Scan(&complete); err != nil {
			return err
		}
		if !complete {
			return nil
		}
		var pid int32
		if err = x.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&pid); err != nil {
			return err
		}
		if err = proxy.Arm(pid); err != nil {
			return err
		}
		mu.Lock()
		armed = true
		mu.Unlock()
		return nil
	}
	store.afterResult = func(cause f.TransactionCause, result f.CommitResult) {
		if oldResult != nil {
			oldResult(cause, result)
		}
		if cause.Kind() == f.CommandsCause && cause.Details().Primary.Canonical() == identity.Canonical() && result.State() == f.Unknown {
			mu.Lock()
			original, unknown = result, true
			mu.Unlock()
		}
	}
	store.beforeLocks = func(ctx context.Context, tx f.Tx, locks []f.LockRequest) error {
		if oldLocks != nil {
			if err := oldLocks(ctx, tx, locks); err != nil {
				return err
			}
		}
		mu.Lock()
		selected := unknown && ctx.Value(variableOwnershipCallKey{}) == originalCall
		mu.Unlock()
		if selected && len(locks) > 1 {
			x, err := store.InTx(tx)
			if err != nil {
				return err
			}
			var pid int32
			if err = x.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&pid); err != nil {
				return err
			}
			confirmation.hold(ctx, pid, f.CommitResult{})
		}
		return nil
	}
	store.mu.Unlock()
	call = variableOwnershipStart(t, originalCall, func(ctx context.Context) error {
		if lane == "ordinary" {
			_, err := pair.Service().CreateVariable(ctx, parent.p2.base.ownerBrowser.actor, m, p.ID, plain)
			return err
		}
		_, err := pair.SecretService().CreateSecretVariable(ctx, parent.p2.base.ownerBrowser.actor, m, p.ID, private)
		return err
	})
	call.await(t, confirmation)
	if original.State() != f.Unknown || original.AttemptID().Validate() != nil || confirmation.pid == proxy.WriterPID() {
		t.Fatal("original business Unknown/independent confirmation Tx missing")
	}
	variableOwnershipRow(t, parent.p2.base.raw, call.callID, p.ID.String(), process.String(), lane, "mutation", "running")
	pair.Stop()
	call.held(t, confirmation, true)
	if pair.Joined() {
		t.Fatal("Stop forgot original confirmation")
	}
	if current, err := owned.guard.CurrentProcess(); err != nil || current != process {
		t.Fatal("original guard released under confirmation")
	}
	proxy.Release()
	phaseAwait(t, proxy.Committed())
	phaseAwait(t, proxy.HeldJoined())
	before := variableOwnershipSnapshot(t, parent.p2.base.raw, p.ID, false)
	confirmation.release.release()
	variableOwnershipWait(t, call.done)
	var fault *f.Fault
	if !errors.As(call.err, &fault) || fault.Code != f.CommitUnknown || fault.CommitState != f.Unknown || fault.CauseID != original.AttemptID().String() {
		t.Fatal("canceled confirmation rewrote original Unknown")
	}
	variableOwnershipDrain(t, pair)
	variableOwnershipRow(t, parent.p2.base.raw, call.callID, p.ID.String(), process.String(), lane, "mutation", "business_closed")
	var count int
	if err = parent.p2.base.raw.QueryRow(ctxFor(t), `SELECT count(*) FROM agenteam_projectvariable.project_calls WHERE project_id=$1 AND process_id=$2`, p.ID.String(), process.String()).Scan(&count); err != nil || count != 1 {
		t.Fatal("confirmation registered a second call")
	}
	if variableOwnershipSnapshot(t, parent.p2.base.raw, p.ID, false) != before {
		t.Fatal("confirmation/checkpoint repeated business")
	}
}
