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
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/account"
	"github.com/LunaDeerTech/agenteam/internal/central/audit"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/object"
	objectc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/project"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	objectfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/objectstore"
	pgfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/postgres"
	"github.com/minio/minio-go/v7"
)

const phaseGuardChildEnv = "AGENTEAM_PROJECT_LIFECYCLE_GUARD_CHILD"
const phaseGuardReady = "PROJECT_LIFECYCLE_ACTIVE_GUARD_HELD"

type phaseGuardInput struct {
	Database, Bucket, Spool, Process, Project, Operation string
}

// The adapter only converts typed IDs and records actual guard calls. A Busy
// result is never replaced by age, a canceled context, or a test-owned flag.
type phaseGuardProcesses struct {
	guard                *object.ProcessGuard
	process              objectc.ProcessID
	mu                   sync.Mutex
	calls, busy, stopped int
}

func (p *phaseGuardProcesses) CurrentProcess() oc.ProcessID {
	id, _ := f.ParseID[oc.Process](p.process.String())
	return id
}
func (p *phaseGuardProcesses) ConfirmStopped(ctx context.Context, process oc.ProcessID) error {
	id, err := f.ParseID[objectc.Process](process.String())
	if err != nil {
		return err
	}
	err = p.guard.ConfirmStopped(ctx, id)
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls++
	var fault *f.Fault
	if errors.As(err, &fault) && fault.Code == f.ResourceBusy {
		p.busy++
	}
	if err == nil {
		p.stopped++
	}
	return err
}
func (p *phaseGuardProcesses) counts() (int, int, int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.calls, p.busy, p.stopped
}

func phaseGuardStorage(t *testing.T, bucket string) (object.StorageConfig, string) {
	t.Helper()
	remote, err := objectfixture.Load()
	if err != nil {
		t.Fatal("owned Object fixture unavailable", err)
	}
	prefix := "phase-guard-" + remote.Nonce[:12] + "-"
	if bucket == "" {
		suffix, err := pgfixture.RandomHex(8)
		if err != nil {
			t.Fatal(err)
		}
		bucket = prefix + suffix
		client, transport, err := remote.Client()
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(transport.CloseIdleConnections)
		if err = client.MakeBucket(ctxFor(t), bucket, minio.MakeBucketOptions{Region: "us-east-1"}); err != nil {
			t.Fatal("owned bucket creation failed", err)
		}
	} else if !strings.HasPrefix(bucket, prefix) {
		t.Fatal("child bucket is outside the owned fixture")
	}
	values := map[string]string{"ENDPOINT": remote.Endpoint(), "BUCKET": bucket, "ACCESS_KEY": remote.AccessKey, "SECRET_KEY": remote.SecretKey, "TLS_MODE": "verify-full", "CA_FILE": remote.CAFile}
	storage, err := object.LoadStorageConfig(func(name string) (string, bool) {
		value, ok := values[strings.TrimPrefix(name, object.EnvironmentPrefix)]
		return value, ok
	})
	if err != nil {
		t.Fatal(err)
	}
	return storage, bucket
}

func phaseGuardRuntime(t *testing.T, store object.Store, aud *audit.Service, storage object.StorageConfig, path string, process objectc.ProcessID) (*object.Runtime, *object.Service, *object.ProcessGuard) {
	t.Helper()
	backend, err := object.NewBackend(storage)
	if err != nil {
		t.Fatal(err)
	}
	spool, err := object.OpenSpool(path, process)
	if err != nil {
		t.Fatal(err)
	}
	var guard *object.ProcessGuard
	constructed := false
	defer func() {
		if !constructed {
			if guard != nil {
				_ = guard.Close()
			}
			_ = spool.Close()
		}
	}()
	guard, err = object.OpenProcessGuard(spool, process)
	if err != nil {
		t.Fatal(err)
	}
	objects, err := object.New(store, backend, spool, aud, object.Authorizations{Processes: guard})
	if err != nil {
		t.Fatal(err)
	}
	endpoint, err := object.LoadTransferEndpoint(func(string) (string, bool) { return "", false }, storage)
	if err != nil {
		t.Fatal(err)
	}
	transfers, err := object.NewTransferService(objects, nil, endpoint)
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := object.NewRuntime(objects, guard, transfers)
	if err != nil {
		t.Fatal(err)
	}
	constructed = true
	return runtime, objects, guard
}

func phaseGuardRetireRuntime(t *testing.T, runtime *object.Runtime) {
	t.Helper()
	if runtime == nil {
		return
	}
	runtime.StopAdmission()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := runtime.Drain(ctx); err != nil {
		t.Error("owned Object runtime did not actually drain", err)
		_ = runtime.Force(ctx)
	}
}

type phaseGuardChild struct {
	cmd            *exec.Cmd
	cancel         context.CancelFunc
	stdin          io.WriteCloser
	done, readDone chan struct{}
	ready          chan struct{}
	waitErr        error
}

func startPhaseGuardChild(t *testing.T, input phaseGuardInput) *phaseGuardChild {
	t.Helper()
	data, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "phase-guard-child.json")
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		t.Fatal(err)
	}
	_, writeErr := file.Write(data)
	closeErr := file.Close()
	if writeErr != nil || closeErr != nil {
		t.Fatal("private child input write failed")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestProjectLifecycleStopBatchRealGuard$", "-test.timeout=30s")
	cmd.Env = append(os.Environ(), phaseGuardChildEnv+"="+path)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	reader, writer, err := os.Pipe()
	if err != nil {
		cancel()
		_ = stdin.Close()
		t.Fatal(err)
	}
	cmd.Stdout, cmd.Stderr = writer, io.Discard
	if err = cmd.Start(); err != nil {
		cancel()
		_ = stdin.Close()
		_ = reader.Close()
		_ = writer.Close()
		t.Fatal(err)
	}
	_ = writer.Close()
	child := &phaseGuardChild{cmd: cmd, cancel: cancel, stdin: stdin, done: make(chan struct{}), readDone: make(chan struct{}), ready: make(chan struct{})}
	go func() {
		defer close(child.readDone)
		defer reader.Close()
		scanner := bufio.NewScanner(reader)
		var once sync.Once
		for scanner.Scan() {
			// Child test RUN/PASS/FAIL lines and error text never enter the
			// parent's exact-selector log. Only this closed protocol is read.
			if scanner.Text() == phaseGuardReady {
				once.Do(func() { close(child.ready) })
			}
		}
	}()
	go func() {
		child.waitErr = cmd.Wait()
		close(child.done)
	}()
	return child
}

func (child *phaseGuardChild) join(t *testing.T) bool {
	t.Helper()
	select {
	case <-child.done:
	default:
		_ = child.cmd.Process.Kill()
	}
	child.cancel()
	_ = child.stdin.Close()
	select {
	case <-child.done:
	case <-time.After(3 * time.Second):
		t.Error("original phase child Wait did not return")
		return false
	}
	select {
	case <-child.readDone:
	case <-time.After(time.Second):
		t.Error("original phase child stdout reader did not join")
		return false
	}
	return true
}

func phaseGuardChildMain(t *testing.T, path string) {
	t.Helper()
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 || info.Size() > 4096 {
		t.Fatal("invalid private phase child input")
	}
	rawInput, err := os.ReadFile(path)
	if err != nil {
		t.Fatal("private phase child input unavailable")
	}
	var input phaseGuardInput
	decoder := json.NewDecoder(bytes.NewReader(rawInput))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&input) != nil || decoder.Decode(new(any)) != io.EOF || !filepath.IsAbs(input.Spool) {
		t.Fatal("invalid phase child input shape")
	}
	remote, err := pgfixture.Load()
	if err != nil {
		t.Fatal(err)
	}
	db := &pgfixture.Database{Fixture: remote, Name: input.Database}
	store := openStore(t, db.Config(t, nil))
	ak, ck := keys(t)
	accounts, err := account.NewAuthority(store, ak)
	if err != nil {
		t.Fatal(err)
	}
	if err = accounts.Initialize(ctxFor(t)); err != nil {
		t.Fatal(err)
	}
	lifecycle, err := project.NewLifecycleAuthority(store, lifecycleManifest(t), nil)
	if err != nil {
		t.Fatal(err)
	}
	projects, err := project.NewAuthority(store, project.AuthorityDependencies{Sessions: accounts, Routes: accounts, Lifecycle: lifecycle})
	if err != nil {
		t.Fatal(err)
	}
	aud, err := audit.New(store, ck, audit.Authorizations{Sessions: accounts, System: accounts, Accounts: accounts, Projects: projects})
	if err != nil {
		t.Fatal(err)
	}
	process, err := f.ParseID[objectc.Process](input.Process)
	if err != nil {
		t.Fatal(err)
	}
	target, err := f.ParseID[i.Project](input.Project)
	if err != nil {
		t.Fatal(err)
	}
	operation, err := f.ParseID[pc.Operation](input.Operation)
	if err != nil {
		t.Fatal(err)
	}
	storage, _ := phaseGuardStorage(t, input.Bucket)
	runtime, objects, guard := phaseGuardRuntime(t, store, aud, storage, input.Spool, process)
	var driver *project.LifecycleStopDriver
	release := newPhaseRelease()
	done := make(chan struct{})
	started := false
	defer func() {
		release.release()
		if driver != nil {
			driver.Stop()
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			err := driver.Drain(ctx)
			cancel()
			if err != nil {
				t.Error("child original driver did not drain", err)
				return // A live local caller never authorizes guard release.
			}
		}
		if started {
			await(t, done)
		}
		phaseGuardRetireRuntime(t, runtime)
	}()
	if err = runtime.Initialize(ctxFor(t)); err != nil {
		t.Fatal(err)
	}
	entered := make(chan struct{})
	driver, err = project.NewLifecycleStopDriver(store, lifecycle, &phaseGuardProcesses{guard: guard, process: process}, func(ctx context.Context, _ i.Actor, actual pc.LifecycleCause, scope pc.ScopeRef) error {
		if actual.OperationID != operation || scope.ProjectID != target {
			return errors.New("child original cause mismatch")
		}
		close(entered)
		<-release.done // Original 2s cancellation is not a returned callback.
		return ctx.Err()
	})
	if err != nil {
		t.Fatal(err)
	}
	var runErr error
	started = true
	go func() { defer close(done); runErr = driver.Run(context.Background(), target, operation) }()
	select {
	case <-entered:
	case <-done:
		t.Fatal("child original driver returned before durable claim", runErr)
	case <-time.After(10 * time.Second):
		t.Fatal("child claim barrier missing")
	}
	// This releases only the spool directory lock. The separately held guard
	// remains live until actual process death; no runtime finish runs here.
	objects.StopAdmission()
	if err = objects.Drain(ctxFor(t)); err != nil {
		t.Fatal(err)
	}
	fmt.Println(phaseGuardReady)
	_, _ = bufio.NewReader(os.Stdin).ReadByte()
}

type phaseGuardClaim struct {
	process, attempt, phase string
	fence                   int64
}

func readPhaseGuardClaim(t *testing.T, v *variableLifecycleFixture, operation pc.OperationID) phaseGuardClaim {
	t.Helper()
	var claim phaseGuardClaim
	if err := v.raw.QueryRow(ctxFor(t), `SELECT process_id::text,attempt_id::text,phase,fence FROM agenteam_project.work_claims WHERE work_kind='lifecycle' AND work_id=$1`, operation.String()).Scan(&claim.process, &claim.attempt, &claim.phase, &claim.fence); err != nil {
		t.Fatal("original lifecycle claim unavailable", err)
	}
	return claim
}

func TestProjectLifecycleStopBatchRealGuard(t *testing.T) {
	if path := os.Getenv(phaseGuardChildEnv); path != "" {
		phaseGuardChildMain(t, path)
		return
	}
	v := newVariableLifecycleFixture(t)
	firstProject := v.project
	laterProject, _, _ := v.createProject(t, v.ownerBrowser.actor, "guard-later")
	first := phaseAccepted(t, v, firstProject)
	later := phaseAccepted(t, v, laterProject)
	if later.OperationID.String() <= first.OperationID.String() {
		t.Fatal("later accepted fixture did not follow the first operation")
	}
	storage, bucket := phaseGuardStorage(t, "")
	spool := filepath.Join(t.TempDir(), "spool")
	oldProcess := id[objectc.Process](t)
	child := startPhaseGuardChild(t, phaseGuardInput{Database: v.db.Name, Bucket: bucket, Spool: spool, Process: oldProcess.String(), Project: firstProject.ID.String(), Operation: first.OperationID.String()})
	var runtime *object.Runtime
	var recovery *project.LifecycleStopRecovery
	defer func() {
		// Child Wait/pipe join comes first on every parent failure path.
		if !child.join(t) {
			return
		}
		if recovery != nil {
			recovery.Stop()
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			err := recovery.Drain(ctx)
			cancel()
			if err != nil {
				t.Error("original recovery batch did not drain", err)
				return
			}
		}
		phaseGuardRetireRuntime(t, runtime)
	}()
	select {
	case <-child.ready:
	case <-child.done:
		t.Fatal("child exited before the fixed live-guard barrier")
	case <-time.After(15 * time.Second):
		t.Fatal("child live-guard barrier missing")
	}
	original := readPhaseGuardClaim(t, v, first.OperationID)
	if original.process != oldProcess.String() || original.phase != "running" || original.fence != 1 {
		t.Fatal("old live process lacks its original running claim")
	}
	phaseState(t, v, first, "stopping", "running", 1)
	newProcess := id[objectc.Process](t)
	var guard *object.ProcessGuard
	runtime, _, guard = phaseGuardRuntime(t, v.tracked, v.audit, storage, spool, newProcess)
	if err := runtime.Initialize(ctxFor(t)); err != nil {
		t.Fatal(err)
	}
	processes := &phaseGuardProcesses{guard: guard, process: newProcess}
	var visited []pc.OperationID
	recovery, err := project.NewLifecycleStopRecovery(v.tracked, v.lifecycle, processes, func(ctx context.Context, actor i.Actor, cause pc.LifecycleCause, scope pc.ScopeRef) error {
		expected := first
		expectedProject := firstProject.ID
		if cause.OperationID == later.OperationID {
			expected, expectedProject = later, laterProject.ID
		}
		if cause != expected || scope.Kind != pc.ProjectScope || scope.ProjectID != expectedProject {
			return errors.New("batch substituted its original cause")
		}
		report, err := v.stopper.InspectStop(ctx, actor, cause, scope)
		if err != nil {
			return err
		}
		if !report.Matches(cause, scope) || !report.Details().LocalJoined || report.Details().PendingCalls != 0 {
			return errors.New("local observation did not join")
		}
		visited = append(visited, cause.OperationID)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	page, err := recovery.RunBatch(ctxFor(t), nil, 1)
	if err != nil || page.Visited != 1 || page.Pending != 1 || page.Next == nil || *page.Next != first.OperationID || len(visited) != 0 {
		t.Fatal("live first claim was taken over or lost continuation", err)
	}
	if calls, busy, stopped := processes.counts(); calls != 1 || busy != 1 || stopped != 0 {
		t.Fatal("batch did not delegate live death proof to the original guard")
	}
	if readPhaseGuardClaim(t, v, first.OperationID) != original {
		t.Fatal("live child claim changed before actual death")
	}
	page, err = recovery.RunBatch(ctxFor(t), page.Next, 1)
	if err != nil || page.Visited != 1 || page.Pending != 1 || page.Next != nil || len(visited) != 1 || visited[0] != later.OperationID {
		t.Fatal("busy prefix starved the next bounded item", err)
	}
	phaseState(t, v, later, "stopping", "terminal", 1)
	phaseState(t, v, first, "stopping", "running", 1)
	if !child.join(t) {
		t.Fatal("old child did not actually exit")
	}
	var exit *exec.ExitError
	if !errors.As(child.waitErr, &exit) {
		t.Fatal("old child did not return its original signal exit")
	}
	status, ok := exit.Sys().(syscall.WaitStatus)
	if !ok || !status.Signaled() || status.Signal() != syscall.SIGKILL {
		t.Fatal("old child exit was not the requested SIGKILL")
	}
	t.Logf("ProjectStopBatch child actual_wait pid=%d signal=SIGKILL stdout_joined=true", child.cmd.Process.Pid)
	// Start a new pass at nil. Only this original ConfirmStopped success plus
	// the driver's locked exact-claim recheck permits a new fenced attempt.
	page, err = recovery.RunBatch(ctxFor(t), nil, 1)
	if err != nil || page.Visited != 1 || page.Pending != 1 || page.Next == nil || *page.Next != first.OperationID || len(visited) != 2 || visited[1] != first.OperationID {
		t.Fatal("actual dead claim did not recover in the next bounded pass", err)
	}
	if calls, busy, stopped := processes.counts(); calls != 2 || busy != 1 || stopped != 1 {
		t.Fatal("actual death did not pass the original guard exactly once")
	}
	recovered := readPhaseGuardClaim(t, v, first.OperationID)
	if recovered.process != newProcess.String() || recovered.attempt == original.attempt || recovered.phase != "terminal" || recovered.fence != 2 {
		t.Fatal("recovery failed to fence the exact old attempt")
	}
	phaseState(t, v, first, "stopping", "terminal", 2)
	phaseState(t, v, later, "stopping", "terminal", 1)
	// These checks deliberately keep every participant required. Local return,
	// actual process death and claim terminal are not whole-domain completion.
}
