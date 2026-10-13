//go:build integration && linux

package projectvariable_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"os/signal"
	"path/filepath"
	"slices"
	"strconv"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/agent"
	ac "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	modelc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/mount"
	mc "github.com/LunaDeerTech/agenteam/internal/central/mount/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	pv "github.com/LunaDeerTech/agenteam/internal/central/projectvariable"
	vc "github.com/LunaDeerTech/agenteam/internal/central/projectvariable/contract"
	runnerhttp "github.com/LunaDeerTech/agenteam/internal/central/runner/http"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
	p "github.com/LunaDeerTech/agenteam/internal/runnerprotocol"
	"golang.org/x/sys/unix"
)

// This exercises the original Runner executable, identity lock, enrollment,
// production selected Handler, Manager and command Owner over real TLS/WSS.
// Only the still-unbound Central Operation producer is the D15 §7 substitute:
// no Model/Tool/Execution business rows or successful Model calls are invented.
func TestRunnerCommandApp(t *testing.T) {
	v := newRunnerReadFileAppFixture(t, runnerReadFileAppBinary(t))
	base := v.setup.agent.p2.base
	current, err := v.setup.agent.agents.GetAgent(ctxFor(t), base.ownerBrowser.actor, v.intent.ProjectID, v.intent.AgentID)
	readFileAdapterOK(t, err, "original command Agent")
	allow, version := ac.ApprovalAllow, current.Fields().Core.Version
	change, err := ac.NewAgentUpdate(ac.AgentUpdateFields{ApprovalPolicy: &allow, ApprovalModelRef: ac.NullableChange[modelc.ModelID]{Present: true}})
	readFileAdapterOK(t, err, "explicit command approval intent")
	updated, err := v.setup.agent.agents.UpdateAgent(ctxFor(t), base.ownerBrowser.actor, meta(t, "command-cli-allow", &version), v.intent.ProjectID, v.intent.AgentID, change)
	readFileAdapterOK(t, err, "formal Owner command approval")
	if updated.Validate() != nil || updated.Fields().Agent.Fields().Core.ApprovalPolicy != ac.ApprovalAllow {
		t.Fatal("formal Owner did not explicitly allow this command fixture")
	}
	_, err = base.raw.Exec(ctxFor(t), `CREATE TABLE runner_command_cli_admissions (
 operation_id text PRIMARY KEY, request_id text NOT NULL UNIQUE,
 project_id text NOT NULL, agent_id text NOT NULL, execution_id text NOT NULL,
 mount_id text NOT NULL, payload bytea NOT NULL)`)
	readFileAdapterOK(t, err, "private command Attempt fixture")
	program, err := os.Executable()
	readFileAdapterOK(t, err, "original integration helper executable")
	program, err = filepath.EvalSymlinks(program)
	readFileAdapterOK(t, err, "original helper executable path")
	workspace := filepath.Join(v.configuration.RootPath, v.intent.ProjectID.String(), v.intent.AgentID.String(), "source")
	arguments := func(mode, marker, code string) p.RunCommandArguments {
		return p.RunCommandArguments{Mode: "argv", Program: program, CWD: ".", Args: []string{"-test.run=^TestRunnerCommandProcessHelper$", "--", runnerCommandHelperMarker, mode, marker, code, "literal ; $(not-a-shell) 雪", ""}}
	}
	var original [32]byte
	if !t.Run("default-enrollment-does-not-start-command", func(t *testing.T) {
		process := runnerReadFileAppLaunch(t, v.binary, v.env(nil, true), []string{"--enroll"}, &v.token)
		runnerDirectoryAppOnline(t, v, process, 1, []string{})
		original = v.identity(t, true)
		call := runnerCommandAppPrepare(t, v, arguments("inspect", "not-started", "0"))
		result, err := call.Dispatch(ctxFor(t))
		requireCode(t, err, f.CapabilityUnsupported)
		if result.Attempted || result.Terminal != nil || len(result.Stdout) != 0 || len(result.Stderr) != 0 {
			t.Fatal("default command capability reached transport")
		}
		if _, err := os.Stat(filepath.Join(workspace, "not-started")); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("unselected command started a process")
		}
		v.stop(t, process)
		t.Log("command_cli_default_rejected=true enrollment_once=true original_wait=true")
	}) {
		return
	}
	selection := "run-command"
	if !t.Run("explicit-argv-report-and-cancelled-child-join", func(t *testing.T) {
		process := runnerReadFileAppLaunch(t, v.binary, v.env(&selection, false), nil, nil)
		runnerDirectoryAppOnline(t, v, process, 2, []string{p.RunCommandOperation})
		if v.identity(t, true) != original {
			t.Fatal("command selection changed the original locked identity")
		}
		args := arguments("inspect", "exited-nonzero", "23")
		call := runnerCommandAppPrepare(t, v, args)
		result, err := call.Dispatch(ctxFor(t))
		readFileAdapterOK(t, err, "actual command invocation report")
		report := runnerCommandAppReport(t, call, result)
		want, err := json.Marshal(runnerCommandInspection{Args: args.Args[6:], Environment: []string{"PATH=/usr/bin:/bin", "LANG=C", "LC_ALL=C"}, Content: string(v.content)})
		readFileAdapterOK(t, err, "independent argv output expectation")
		want = append(want, '\n')
		if report.ExitCode != 23 || report.Signal != 0 || report.Cancelled || report.TimedOut || report.CompletionCode != nil || !report.OutputComplete || !bytes.Equal(result.Stdout, want) || string(result.Stderr) != "command stderr 雪\n" {
			t.Fatal("nonzero exit, literal argv, minimal environment or complete streams changed")
		}
		runnerCommandAppGone(t, runnerCommandAppMarker(t, filepath.Join(workspace, "exited-nonzero")))
		runnerCommandAppCancel(t, v, process, arguments("cancel", "cancel-started", "0"), filepath.Join(workspace, "cancel-started"))
		v.stop(t, process)
		t.Log("command_cli_argv_no_shell=true exit23_report=true complete_streams=true nil_injected_environment=true cancel_actual_child_wait_and_group_absence=true original_cli_drained=true")
	}) {
		return
	}
	t.Run("same-identity-restart-command-and-read", func(t *testing.T) {
		selection = "run-command,read-file"
		process := runnerReadFileAppLaunch(t, v.binary, v.env(&selection, false), nil, nil)
		runnerDirectoryAppOnline(t, v, process, 3, []string{p.ReadFileOperation, p.RunCommandOperation})
		if v.identity(t, true) != original {
			t.Fatal("command restart changed the original stored identity")
		}
		call := runnerCommandAppPrepare(t, v, arguments("inspect", "restarted-command", "0"))
		result, err := call.Dispatch(ctxFor(t))
		readFileAdapterOK(t, err, "restarted command terminal")
		report := runnerCommandAppReport(t, call, result)
		if report.ExitCode != 0 || report.Signal != 0 || report.Cancelled || report.TimedOut || report.CompletionCode != nil || !report.OutputComplete {
			t.Fatal("restarted command did not retain its original successful report")
		}
		runnerCommandAppGone(t, runnerCommandAppMarker(t, filepath.Join(workspace, "restarted-command")))
		v.dispatch(t, true, 64)
		v.stop(t, process)
		t.Log("command_cli_stored_identity_restart=true original_wait=true combined_read=true model_not_invoked=true tools_http_not_tested=true")
	})
}

// The private substitute owns a complete immutable intent and exact original
// Attempt row. The real current Agent and current environment participants
// execute under its live original-Tx witness, both at Resolve and Claim.
type runnerCommandAppAuthority struct {
	store       *hookStore
	intent      mc.CommandIntent
	payload     []byte
	requestID   p.ID
	digest      f.Digest
	current     ac.ExecutionConfiguration
	environment *pv.CurrentProcessEnvironmentProvider
}
type runnerCommandAppWitnessKey struct{}
type runnerCommandAppWitness struct {
	owner *runnerCommandAppAuthority
	tx    f.Tx
	live  atomic.Bool
	facts *vc.CurrentProcessEnvironmentFacts
}

func (o *runnerCommandAppAuthority) configuration() ac.ExecutionConfigurationRequest {
	in := o.intent
	return ac.ExecutionConfigurationRequest{Actor: in.Actor, ProjectID: in.ProjectID, AgentID: in.AgentID, ExecutionID: in.ExecutionID, Stage: ac.ExecutionConfigurationCurrent}
}
func (o *runnerCommandAppAuthority) environmentRequest() vc.CurrentProcessEnvironmentRequest {
	in := o.intent
	return vc.CurrentProcessEnvironmentRequest{ProjectID: in.ProjectID, AgentID: in.AgentID, ExecutionID: in.ExecutionID, CallBindingDigest: o.digest}
}
func (o *runnerCommandAppAuthority) matches(in mc.CommandIntent) bool {
	w := o.intent
	return in.Validate() == nil && in.Actor.Equal(w.Actor) && in.ProjectID == w.ProjectID && in.AgentID == w.AgentID && in.ExecutionID == w.ExecutionID && in.OperationID == w.OperationID && in.MountID == w.MountID && in.Arguments.Mode == w.Arguments.Mode && in.Arguments.Program == w.Arguments.Program && in.Arguments.CWD == w.Arguments.CWD && slices.Equal(in.Arguments.Args, w.Arguments.Args)
}
func (o *runnerCommandAppAuthority) CommandLocks(in mc.CommandIntent) ([]f.LockRequest, error) {
	if !o.matches(in) {
		return nil, f.NewFault(f.Forbidden, f.NotStarted)
	}
	return o.environmentRequest().RequiredLocks(), nil
}
func (o *runnerCommandAppAuthority) locks() ([]f.LockRequest, error) {
	locks, err := o.CommandLocks(o.intent)
	if err != nil {
		return nil, err
	}
	op, err := f.AggregateLock(f.OperationAggregate, o.intent.OperationID.String())
	if err != nil {
		return nil, err
	}
	locks = append(locks, o.configuration().RequiredLocks()...)
	return oc.NormalizeLocks(append(locks, f.LockRequest{Key: op, Mode: f.Exclusive}))
}
func (o *runnerCommandAppAuthority) held(ctx context.Context, tx f.Tx) error {
	if _, err := o.store.InTx(tx); err != nil {
		return err
	}
	locks, err := o.locks()
	if err != nil {
		return err
	}
	return o.store.RequireHeldLocks(ctx, tx, locks)
}
func (o *runnerCommandAppAuthority) admit(t *testing.T, requestID p.ID) {
	t.Helper()
	if !requestID.Valid() || o.requestID != "" {
		t.Fatal("command admission requires one original RID")
	}
	locks, err := o.locks()
	readFileAdapterOK(t, err, "command fixture lock plan")
	cause, err := f.NewRecoveryCause("runner-command-cli-attempt", o.intent.OperationID.String(), string(requestID))
	readFileAdapterOK(t, err, "command fixture Attempt identity")
	result := o.store.WithinTx(ctxFor(t), cause, func(ctx context.Context, tx f.Tx) error {
		if err := o.store.AcquireAll(ctx, tx, locks); err != nil {
			return err
		}
		x, err := o.store.InTx(tx)
		if err != nil {
			return err
		}
		in := o.intent
		_, err = x.Exec(ctx, `INSERT INTO runner_command_cli_admissions(operation_id,request_id,project_id,agent_id,execution_id,mount_id,payload) VALUES($1,$2,$3,$4,$5,$6,$7)`, in.OperationID.String(), string(requestID), in.ProjectID.String(), in.AgentID.String(), in.ExecutionID.String(), in.MountID.String(), o.payload)
		return err
	})
	if result.State() != f.Committed {
		t.Fatal("original command fixture Attempt did not return known committed", result.State())
	}
	// Neither the callback nor a prepared RID may open this admission.
	o.requestID = requestID
}
func (o *runnerCommandAppAuthority) WithCommandInTx(ctx context.Context, tx f.Tx, in mc.CommandIntent, requestID p.ID, fn func(context.Context) error) error {
	if ctx == nil || fn == nil || !o.matches(in) || requestID != "" && (requestID != o.requestID || !requestID.Valid()) {
		return f.NewFault(f.Forbidden, f.NotStarted)
	}
	if err := o.held(ctx, tx); err != nil {
		return err
	}
	if requestID != "" {
		x, err := o.store.InTx(tx)
		if err != nil {
			return err
		}
		var exists bool
		err = x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM runner_command_cli_admissions WHERE operation_id=$1 AND request_id=$2 AND project_id=$3 AND agent_id=$4 AND execution_id=$5 AND mount_id=$6 AND payload=$7)`, in.OperationID.String(), string(requestID), in.ProjectID.String(), in.AgentID.String(), in.ExecutionID.String(), in.MountID.String(), o.payload).Scan(&exists)
		if err != nil {
			return err
		}
		if !exists {
			return f.NewFault(f.Forbidden, f.NotStarted)
		}
	}
	w := &runnerCommandAppWitness{owner: o, tx: tx}
	w.live.Store(true)
	defer w.live.Store(false)
	authorized := context.WithValue(ctx, runnerCommandAppWitnessKey{}, w)
	current, err := o.current.ReadExecutionConfigurationInTx(authorized, tx, o.configuration())
	if err != nil {
		return err
	}
	fields := current.Fields()
	if fields.Core.ApprovalPolicy != ac.ApprovalAllow || fields.Core.ApprovalModelRef != nil {
		return f.NewFault(f.Forbidden, f.NotStarted)
	}
	w.facts = &vc.CurrentProcessEnvironmentFacts{AgentVersion: fields.Core.Version, AllowedSecretVariableIDs: slices.Clone(fields.AllowedSecretVariableIDs)}
	if err = o.environment.RequireEmptyInTx(authorized, tx, o.environmentRequest()); err != nil {
		return err
	}
	return fn(authorized)
}
func (o *runnerCommandAppAuthority) witness(ctx context.Context, tx f.Tx) (*runnerCommandAppWitness, error) {
	if ctx == nil {
		return nil, f.NewFault(f.Forbidden, f.NotStarted)
	}
	w, ok := ctx.Value(runnerCommandAppWitnessKey{}).(*runnerCommandAppWitness)
	if !ok || w.owner != o || w.tx != tx || !w.live.Load() {
		return nil, f.NewFault(f.Forbidden, f.NotStarted)
	}
	if err := o.held(ctx, tx); err != nil {
		return nil, err
	}
	return w, nil
}
func (o *runnerCommandAppAuthority) RequireExecutionConfigurationInTx(ctx context.Context, tx f.Tx, in ac.ExecutionConfigurationRequest) error {
	if _, err := o.witness(ctx, tx); err != nil {
		return err
	}
	w := o.configuration()
	if in.Validate() != nil || in.Stage != w.Stage || !in.Actor.Equal(w.Actor) || in.ProjectID != w.ProjectID || in.AgentID != w.AgentID || in.ExecutionID != w.ExecutionID {
		return f.NewFault(f.Forbidden, f.NotStarted)
	}
	return nil
}
func (o *runnerCommandAppAuthority) RequireCurrentProcessEnvironmentInTx(ctx context.Context, tx f.Tx, in vc.CurrentProcessEnvironmentRequest) (vc.CurrentProcessEnvironmentFacts, error) {
	w, err := o.witness(ctx, tx)
	if err != nil {
		return vc.CurrentProcessEnvironmentFacts{}, err
	}
	if in != o.environmentRequest() || w.facts == nil || w.facts.Validate() != nil {
		return vc.CurrentProcessEnvironmentFacts{}, f.NewFault(f.Forbidden, f.NotStarted)
	}
	return w.facts.Clone(), nil
}

type runnerCommandAppCall struct {
	*runnerhttp.CommandCall
	operationID p.ID
}

func runnerCommandAppPrepare(t *testing.T, v *runnerReadFileAppFixture, args p.RunCommandArguments) *runnerCommandAppCall {
	t.Helper()
	in := mc.CommandIntent{Actor: v.intent.Actor, ProjectID: v.intent.ProjectID, AgentID: v.intent.AgentID, ExecutionID: v.intent.ExecutionID, OperationID: id[tc.Operation](t), MountID: v.intent.MountID, Arguments: args}
	in.Arguments.Args = slices.Clone(args.Args)
	payload, err := p.EncodeRunCommandPayload(p.RunCommandPayload{WorkspaceName: "source", Arguments: in.Arguments})
	readFileAdapterOK(t, err, "original command payload")
	sum := sha256.Sum256(payload)
	digest, err := f.ParseDigest("sha256:" + hex.EncodeToString(sum[:]))
	readFileAdapterOK(t, err, "private command binding")
	o := &runnerCommandAppAuthority{store: v.setup.agent.p2.base.tracked, intent: in, payload: payload, digest: digest}
	o.current, err = agent.NewExecutionConfiguration(v.setup.agent.providers.Agents, o)
	readFileAdapterOK(t, err, "original current command Agent")
	o.environment, err = pv.NewCurrentProcessEnvironment(o.store, o)
	readFileAdapterOK(t, err, "original current command environment")
	resolver, err := mount.NewCommandResolver(o.store, o.current, o)
	readFileAdapterOK(t, err, "original command Mount resolver")
	dispatcher, err := runnerhttp.NewCommandDispatcher(v.device, resolver)
	readFileAdapterOK(t, err, "original command dispatcher")
	ticket, err := resolver.Resolve(ctxFor(t), in)
	readFileAdapterOK(t, err, "original command ticket")
	call, err := dispatcher.Prepare(ticket)
	readFileAdapterOK(t, err, "original command prepared RID")
	if !call.RequestID().Valid() || v.requests[call.RequestID()] {
		t.Fatal("command reused a prepared RequestID")
	}
	v.requests[call.RequestID()] = true
	o.admit(t, call.RequestID())
	return &runnerCommandAppCall{CommandCall: call, operationID: p.ID(in.OperationID.String())}
}
func runnerCommandAppReport(t *testing.T, call *runnerCommandAppCall, result runnerhttp.CommandDispatchResult) p.RunCommandResult {
	t.Helper()
	terminal := result.Terminal
	if !result.Attempted || terminal == nil || terminal.RequestID != call.RequestID() || terminal.OperationID != call.operationID || terminal.Outcome != p.Success || terminal.Code != nil || terminal.SafeMessage != nil {
		t.Fatal("command did not return its original complete report")
	}
	report, err := p.DecodeRunCommandResult(terminal.Payload)
	if err != nil || report.State != "completed" || !report.ProcessGroupTerminationConfirmed || report.StdoutTruncated || report.StderrTruncated || report.StdoutBytes != p.Decimal(strconv.Itoa(len(result.Stdout))) || report.StderrBytes != p.Decimal(strconv.Itoa(len(result.Stderr))) {
		t.Fatal("command lost original Wait/Drain or declared stream facts")
	}
	reused, err := call.Dispatch(ctxFor(t))
	requireCode(t, err, f.InvalidState)
	if reused.Attempted || reused.Terminal != nil {
		t.Fatal("command reused its original dispatch")
	}
	return report
}

type runnerCommandChild struct {
	PID   int
	Group int
}

func runnerCommandAppMarker(t *testing.T, path string) runnerCommandChild {
	t.Helper()
	raw, err := os.ReadFile(path)
	var child runnerCommandChild
	if err != nil || len(raw) > 256 || json.Unmarshal(raw, &child) != nil || child.PID <= 1 || child.Group != child.PID {
		t.Fatal("original direct child marker is unavailable or unowned")
	}
	return child
}
func runnerCommandAppGone(t *testing.T, child runnerCommandChild) {
	t.Helper()
	if !errors.Is(syscall.Kill(child.PID, 0), syscall.ESRCH) || !errors.Is(syscall.Kill(-child.Group, 0), syscall.ESRCH) {
		t.Fatal("original child or process group remains after command report")
	}
}
func runnerCommandAppCancel(t *testing.T, v *runnerReadFileAppFixture, process *runnerReadFileAppProcess, args p.RunCommandArguments, marker string) {
	t.Helper()
	call := runnerCommandAppPrepare(t, v, args)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	var result runnerhttp.CommandDispatchResult
	var callErr error
	childFD := -1
	var child runnerCommandChild
	t.Cleanup(func() {
		cancel()
		pending := false
		select {
		case <-done:
		default:
			pending = true
		}
		if pending || t.Failed() {
			// Ask the original Runner to cancel and Drain its owners first. A
			// killed parent is never evidence that its separate child group died.
			select {
			case <-process.done:
			default:
				if err := process.command.Process.Signal(syscall.SIGTERM); err != nil && !errors.Is(err, os.ErrProcessDone) {
					t.Error("original Runner cleanup SIGTERM failed")
				}
			}
			select {
			case <-process.done:
			case <-time.After(5 * time.Second):
				t.Error("original Runner cleanup Wait is still owned")
				if childFD < 0 {
					// No pinned child identity: do not kill the only process that
					// can still cancel/reap it. Retain ownership to the test bound.
					<-process.done
				}
			}
		}
		if childFD >= 0 {
			exited, err := runnerCommandChildExited(childFD, 0)
			if err != nil || !exited {
				t.Error("command child remained after original cancellation; force only the pinned child")
				if err := unix.PidfdSendSignal(childFD, unix.SIGKILL, nil, 0); err != nil && !errors.Is(err, unix.ESRCH) {
					t.Error("pinned command child cleanup signal failed")
				}
				exited, err = runnerCommandChildExited(childFD, 5*time.Second)
				if err != nil || !exited {
					t.Error("pinned command child exit remains unconfirmed; retain original Runner owner")
					for !exited {
						exited, err = runnerCommandChildExited(childFD, -1)
						if err != nil || !exited {
							// An invalid descriptor/error cannot become a tight loop
							// or permission to drop ownership. Retry only this same
							// poll after the original failure-cleanup interval; the
							// outer test deadline remains the final failure bound.
							<-time.After(5 * time.Second)
						}
					}
				}
			}
			// Only a confirmed child exit permits last-resort parent cleanup.
			if exited && (pending || t.Failed()) {
				select {
				case <-process.done:
				case <-time.After(5 * time.Second):
					t.Error("Runner did not return after pinned child exit")
					_ = process.command.Process.Kill()
					<-process.done
				}
			}
			if !errors.Is(syscall.Kill(child.PID, 0), syscall.ESRCH) || !errors.Is(syscall.Kill(-child.Group, 0), syscall.ESRCH) {
				t.Error("command child/group absence remains unconfirmed after cleanup")
			}
			if unix.Close(childFD) != nil {
				t.Error("original command child pidfd Close failed")
			}
		}
		if pending || t.Failed() {
			<-process.done
			<-done
		}
	})
	go func() { result, callErr = call.Dispatch(ctx); close(done) }()
	bound := time.NewTimer(3 * time.Second)
	defer bound.Stop()
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	for {
		if _, err := os.Stat(marker); err == nil {
			break
		} else if !errors.Is(err, os.ErrNotExist) {
			t.Fatal("original child readiness could not be observed")
		}
		select {
		case <-done:
			t.Fatal("command returned before its original child readiness")
		case <-bound.C:
			t.Fatal("original child did not reach cancellation point")
		case <-tick.C:
		}
	}
	child = runnerCommandAppMarker(t, marker)
	// The original helper is now blocked awaiting TERM; no cancellation has
	// been issued. Pin that live identity before the first stop request, so a
	// failure cleanup cannot accidentally signal a subsequently reused PID.
	var err error
	childFD, err = unix.PidfdOpen(child.PID, 0)
	readFileAdapterOK(t, err, "original command child identity pin")
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("cancelled command did not join its original terminal and frame")
	}
	readFileAdapterOK(t, callErr, "actual cancelled invocation report")
	report := runnerCommandAppReport(t, call, result)
	// The child writes stderr only AFTER receiving TERM. The bound Control
	// cancellation policy refuses that not-yet-queued tail without poisoning
	// the wire; its complete invocation report must preserve incomplete output.
	if !report.Cancelled || report.TimedOut || report.CompletionCode == nil || *report.CompletionCode != p.CancelledCode || report.ExitCode != 0 || report.Signal != 0 || report.OutputComplete || len(result.Stdout) != 0 || len(result.Stderr) != 0 {
		t.Fatal("cancellation lost original child Wait or changed its unadmitted output tail")
	}
	runnerCommandAppGone(t, child)
}

// Poll observes actual exit. It is not the parent's original Cmd.Wait or a
// descendant-tree proof; those remain separate assertions above.
func runnerCommandChildExited(fd int, bound time.Duration) (bool, error) {
	deadline := time.Now().Add(bound)
	for {
		remaining := time.Until(deadline)
		milliseconds := 0
		if remaining > 0 {
			milliseconds = int((remaining + time.Millisecond - 1) / time.Millisecond)
		}
		if bound < 0 {
			milliseconds = -1
		}
		fds := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN}}
		n, err := unix.Poll(fds, milliseconds)
		if errors.Is(err, unix.EINTR) && (bound < 0 || time.Now().Before(deadline)) {
			continue
		}
		if err != nil {
			return false, err
		}
		return n == 1 && fds[0].Revents&unix.POLLIN != 0, nil
	}
}

const runnerCommandHelperMarker = "owned-runner-command-cli-child"

type runnerCommandInspection struct {
	Args        []string
	Environment []string
	Content     string
}

// Only the explicitly dispatched argv invokes this helper. It needs neither
// fixture secrets nor inherited environment, and never invokes a shell.
func TestRunnerCommandProcessHelper(t *testing.T) {
	index := slices.Index(os.Args, runnerCommandHelperMarker)
	if index < 0 {
		t.Skip("owned command subprocess helper")
	}
	args := os.Args[index+1:]
	if len(args) < 3 || filepath.Base(args[1]) != args[1] {
		os.Exit(90)
	}
	code, err := strconv.Atoi(args[2])
	if err != nil || code < 0 || code > 255 {
		os.Exit(91)
	}
	var stopped chan os.Signal
	if args[0] == "cancel" {
		stopped = make(chan os.Signal, 1)
		signal.Notify(stopped, syscall.SIGTERM)
	}
	marker, err := json.Marshal(runnerCommandChild{PID: os.Getpid(), Group: syscall.Getpgrp()})
	if err != nil {
		os.Exit(92)
	}
	// Atomic publication is the parent's real start barrier, never a sleep.
	if os.WriteFile(args[1]+".tmp", marker, 0600) != nil || os.Rename(args[1]+".tmp", args[1]) != nil {
		os.Exit(94)
	}
	switch args[0] {
	case "inspect":
		content, err := os.ReadFile("notes.txt")
		if err != nil {
			os.Exit(95)
		}
		if json.NewEncoder(os.Stdout).Encode(runnerCommandInspection{Args: args[3:], Environment: os.Environ(), Content: string(content)}) != nil {
			os.Exit(96)
		}
		if _, err := os.Stderr.WriteString("command stderr 雪\n"); err != nil {
			os.Exit(97)
		}
	case "cancel":
		<-stopped
		if _, err := os.Stderr.WriteString("cancelled-after-signal\n"); err != nil {
			os.Exit(98)
		}
	default:
		os.Exit(99)
	}
	// This leaf process has no fixture owners. Use its actual kernel exit so
	// the race test binary's os.Exit bookkeeping delay cannot turn a handled
	// TERM into the command Owner's later KILL. Parent/product race checks and
	// their original Wait/Drain remain enabled and are never bypassed.
	stdoutClose, stderrClose := os.Stdout.Close(), os.Stderr.Close()
	if stdoutClose != nil || stderrClose != nil {
		syscall.Exit(100)
	}
	syscall.Exit(code)
}
