package runtime

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"slices"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/mount"
	mc "github.com/LunaDeerTech/agenteam/internal/central/mount/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	runnerhttp "github.com/LunaDeerTech/agenteam/internal/central/runner/http"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
	p "github.com/LunaDeerTech/agenteam/internal/runnerprotocol"
)

type RunCommandExecutor struct {
	data func() *runCommandExecutorData
}
type runCommandExecutorData struct {
	authority  *runCommandAuthorityState
	mounts     *mount.CommandResolver
	dispatcher *runnerhttp.CommandDispatcher
	schemas    *RegistrySchemaValidator
}

func NewRunCommandExecutor(authority *RunCommandAuthority, mounts *mount.CommandResolver, dispatcher *runnerhttp.CommandDispatcher, schemas *RegistrySchemaValidator) (*RunCommandExecutor, error) {
	if authority == nil || authority.data == nil || mounts == nil || dispatcher == nil || schemas == nil || schemas.data == nil {
		return nil, fail(f.DependencyUnbound)
	}
	d := authority.data()
	if !sameInstallInstance(d.store, schemas.data().store) {
		return nil, fail(f.InvalidArgument)
	}
	e := &runCommandExecutorData{d, mounts, dispatcher, schemas}
	return &RunCommandExecutor{data: func() *runCommandExecutorData { return e }}, nil
}

// RunCommandOutcome is a durable invocation observation, not a Transcript receipt.
// A completed wire report and command success are distinct. Known error reports
// retain their full status and both streams through the explicit Result method.
type RunCommandOutcome struct{ data func() runCommandObservation }
type runCommandObservation struct {
	record   runCommandOperationRecord
	terminal *runCommandTerminal
	result   p.RunCommandResult
	stdout   []byte
	stderr   []byte
}

func (v RunCommandOutcome) OperationID() tc.OperationID {
	if v.data == nil {
		return tc.OperationID{}
	}
	return v.data().record.ID
}
func (v RunCommandOutcome) AttemptID() tc.AttemptID {
	if v.data == nil {
		return tc.AttemptID{}
	}
	return v.data().record.Attempt
}
func (v RunCommandOutcome) RequestID() p.ID {
	if v.data == nil || v.data().terminal == nil {
		return ""
	}
	return v.data().terminal.Request
}
func (v RunCommandOutcome) Status() (phase, outcome string, attempted bool) {
	if v.data == nil {
		return "", "", false
	}
	o := v.data()
	if o.terminal == nil {
		return o.record.Phase, "", false
	}
	return o.record.Phase, o.terminal.Outcome, o.terminal.Attempted
}
func (v RunCommandOutcome) Code() string {
	if v.data == nil || v.data().terminal == nil {
		return ""
	}
	return v.data().terminal.Code
}

func (v RunCommandOutcome) Result() (p.RunCommandResult, []byte, []byte, bool) {
	if v.data == nil {
		return p.RunCommandResult{}, nil, nil, false
	}
	o := v.data()
	if o.terminal == nil || !o.terminal.ReportAvailable {
		return p.RunCommandResult{}, nil, nil, false
	}
	result := o.result
	if result.CompletionCode != nil {
		code := *result.CompletionCode
		result.CompletionCode = &code
	}
	return result, slices.Clone(o.stdout), slices.Clone(o.stderr), true
}

func runCommandBindingProjection(b tc.ToolCallBinding) runCommandBindingData {
	return runCommandBindingData{b.ProjectID, b.AgentID, b.ExecutionID, b.RoundID, b.SnapshotID, b.InputBindingID, b.LogicalCallID, b.InvocationID, b.CallID, b.ModelVisibleName, b.Spec, b.Input.PayloadID, b.Input.SHA256, b.Input.Bytes, b.CanonicalArguments}
}
func runCommandTargetProjection(t mc.CommandTarget) runCommandTarget {
	return runCommandTarget{t.MountID, t.WorkspaceID, t.RunnerID, t.WorkspaceName, t.AgentVersion, t.MountVersion}
}

func consumeRunCommandInput(ctx context.Context, tx f.Tx, d *runCommandAuthorityState, h *runCommandHandoff, consume func(context.Context, []byte) error) error {
	calls := 0
	var callbackErr error
	err := d.executions.WithRunCommandCallInTx(ctx, tx, h.plan, func(authorized context.Context, b tc.ToolCallBinding, borrowed []byte) (err error) {
		defer func() {
			if err != nil {
				callbackErr = err
			}
		}()
		calls++
		if calls != 1 || authorized == nil || b.Validate() != nil || b.RunnerBinding.Validate() != nil || b.RunnerBinding.OperationName != p.RunCommandOperation || b.RunnerBinding.OperationRevision != string(p.RunCommandRevision) || b.Binding != (tc.BuiltinBinding{}) ||
			!b.Actor.Equal(h.binding.Actor) || b.RunnerBinding != h.binding.RunnerBinding || runCommandBindingProjection(b) != runCommandBindingProjection(h.binding) ||
			int64(len(borrowed)) != int64(b.Input.Bytes) || digest(borrowed) != b.Input.SHA256 {
			return fail(f.Forbidden)
		}
		arguments, err := tc.DecodeRunCommandToolArguments(authorized, borrowed)
		if err != nil {
			return schemaReadError(err)
		}
		defer clear(arguments.Arguments.Args)
		canonical, err := cursor.CanonicalJSON(borrowed)
		if err != nil {
			return fail(f.InvalidState)
		}
		defer clear(canonical)
		if digest(canonical) != b.CanonicalArguments {
			return fail(f.ConfirmationStale)
		}
		if h.arguments.MountID == (i.MountID{}) {
			h.arguments = arguments
			h.arguments.Arguments.Args = slices.Clone(arguments.Arguments.Args)
		} else if h.arguments.MountID != arguments.MountID || !sameRunCommandArguments(h.arguments.Arguments, arguments.Arguments) {
			return fail(f.ConfirmationStale)
		}
		return consume(authorized, borrowed)
	})
	if err != nil {
		return schemaReadError(err)
	}
	if calls != 1 {
		return fail(f.Forbidden)
	}
	if callbackErr != nil {
		return callbackErr
	}
	return ctx.Err()
}

func runCommandTransaction(ctx context.Context, d *runCommandAuthorityState, h *runCommandHandoff, consume func(context.Context, f.Tx, postgres.SQLExecutor) error) (f.CommitResult, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	completed, calls := false, 0
	result := d.store.WithinTx(ctx, h.cause, func(ctx context.Context, tx f.Tx) error {
		calls++
		if calls != 1 {
			return fail(f.InvalidState)
		}
		x, err := d.store.InTx(tx)
		if err != nil {
			return portError(err)
		}
		if err = d.store.AcquireAll(ctx, tx, h.locks); err != nil {
			return portError(err)
		}
		if err = d.store.RequireHeldLocks(ctx, tx, h.locks); err != nil {
			return portError(err)
		}
		if err = d.requireProcess(); err != nil {
			return err
		}
		if err = consume(ctx, tx, x); err != nil {
			return err
		}
		if err = ctx.Err(); err != nil {
			return err
		}
		completed = true
		return nil
	})
	if result.State() == f.Committed && (!completed || calls != 1) {
		return result, fail(f.InvalidState)
	}
	return result, commitError(result)
}

func discoverRunCommand(ctx context.Context, d *runCommandExecutorData, execution i.ExecutionID) (*runCommandHandoff, error) {
	plan, err := d.authority.executions.DiscoverRunCommandCall(ctx, execution)
	if err != nil {
		return nil, schemaReadError(err)
	}
	if nilPort(plan) {
		return nil, fail(f.DependencyUnbound)
	}
	b := plan.Binding()
	if b.Validate() != nil || b.RunnerBinding.Validate() != nil || b.RunnerBinding.OperationName != p.RunCommandOperation || b.RunnerBinding.OperationRevision != string(p.RunCommandRevision) || b.Binding != (tc.BuiltinBinding{}) || b.ExecutionID != execution {
		return nil, fail(f.Forbidden)
	}
	cause, err := inputCause(b)
	if err != nil {
		return nil, err
	}
	locks, err := baseLocks(b, cause, plan.RequiredLocks())
	if err != nil {
		return nil, err
	}
	return &runCommandHandoff{issuer: d.authority, executor: d, execution: execution, plan: plan, binding: b, locks: locks, cause: cause}, nil
}

func bindRunCommandOperationLock(h *runCommandHandoff, operation tc.OperationID) error {
	key, err := f.AggregateLock(f.OperationAggregate, operation.String())
	if err != nil {
		return fail(f.InvalidState)
	}
	h.locks, err = unionLocks(append(h.locks, f.LockRequest{Key: key, Mode: f.Exclusive}))
	return err
}

// ExecuteRunCommand accepts only an Execution identity. Original protected bytes,
// Tool binding and target facts are fetched through their actual owners. There
// is no raw-arguments, caller RID, committed flag or retry input.
func (e *RunCommandExecutor) ExecuteRunCommand(ctx context.Context, execution i.ExecutionID) (out RunCommandOutcome, err error) {
	if err = contextError(ctx); err != nil {
		return out, err
	}
	if e == nil || e.data == nil || execution.Validate() != nil {
		return out, fail(f.InvalidArgument)
	}
	d := e.data()
	a := d.authority
	callCtx, cancel := context.WithCancel(ctx)
	h := &runCommandHandoff{issuer: a, executor: d, execution: execution, ctx: callCtx, cancel: cancel, done: make(chan struct{})}
	a.mu.Lock()
	if a.stopped || a.active[execution] != nil {
		stopped := a.stopped
		a.mu.Unlock()
		cancel()
		if stopped {
			return out, fail(f.ShuttingDown)
		}
		return out, fail(f.ResourceBusy)
	}
	a.active[execution] = h
	a.mu.Unlock()
	retired := true
	defer func() {
		cancel()
		h.mu.Lock()
		h.admission = ""
		if !retired && h.retirement == nil {
			h.retirement = fail(f.DependencyUnavailable)
		}
		if retired {
			clear(h.payload)
			h.payload = nil
			clear(h.stdout)
			clear(h.stderr)
			h.stdout, h.stderr = nil, nil
			clearRunCommandArguments(h)
		}
		h.mu.Unlock()
		a.mu.Lock()
		if retired {
			delete(a.active, execution)
		}
		a.mu.Unlock()
		close(h.done)
	}()
	plan, err := discoverRunCommand(callCtx, d, execution)
	if err != nil {
		return out, err
	}
	h.plan, h.binding, h.locks, h.cause = plan.plan, plan.binding, plan.locks, plan.cause
	// Metadata discovery is read-only under original command/Execution locks.
	// The real Operation lock is added before any creation/Attempt transaction.
	var existing *runCommandOperationRecord
	_, err = runCommandTransaction(callCtx, a, h, func(ctx context.Context, tx f.Tx, x postgres.SQLExecutor) error {
		return consumeRunCommandInput(ctx, tx, a, h, func(authorized context.Context, _ []byte) error {
			var err error
			existing, err = loadRunCommandOperation(authorized, x, runCommandBindingProjection(h.binding))
			return err
		})
	})
	if err != nil {
		return out, err
	}
	operation, err := f.NewID[tc.Operation]()
	if err != nil {
		return out, portError(err)
	}
	if existing != nil {
		operation = existing.ID
	}
	if err = bindRunCommandOperationLock(h, operation); err != nil {
		return out, err
	}
	// From the first possible write, unknown commits retain this exact owner.
	retired = false
	result, err := runCommandTransaction(callCtx, a, h, func(ctx context.Context, tx f.Tx, x postgres.SQLExecutor) error {
		return consumeRunCommandInput(ctx, tx, a, h, func(authorized context.Context, raw []byte) error {
			old, err := loadRunCommandOperation(authorized, x, runCommandBindingProjection(h.binding))
			if err != nil {
				return err
			}
			if old != nil {
				if old.ID != operation {
					return fail(f.ResourceBusy)
				}
				h.record = *old
				return nil
			}
			if existing != nil {
				return fail(f.ConfirmationStale)
			}
			if err = a.executions.RequireRunCommandAdmissionInTx(authorized, tx, h.plan); err != nil {
				return schemaReadError(err)
			}
			if err = d.schemas.ValidateRunCommandInputInTx(authorized, tx, h.binding.Spec, raw); err != nil {
				return err
			}
			b := h.binding
			target, err := d.mounts.CurrentTargetInTx(authorized, tx, mc.CommandTargetRequest{Actor: b.Actor, ProjectID: b.ProjectID, AgentID: b.AgentID, ExecutionID: b.ExecutionID, MountID: h.arguments.MountID})
			if err != nil {
				return portError(err)
			}
			input := runCommandInput{Format: 1, Binding: runCommandBindingProjection(b), Operation: b.RunnerBinding.OperationName, Revision: b.RunnerBinding.OperationRevision, ProtocolMajor: int(b.RunnerBinding.ProtocolMajor), Target: runCommandTargetProjection(target)}
			encoded, err := encodeRunCommandInput(input)
			if err != nil {
				return err
			}
			fingerprint, err := runCommandFingerprint(input)
			if err != nil {
				return err
			}
			h.record = runCommandOperationRecord{ID: operation, Input: input, InputDigest: digest(encoded), Fingerprint: fingerprint, Phase: "created", Version: 1}
			return insertRunCommandOperation(authorized, x, h.record)
		})
	})
	if err != nil {
		retired = result.State() == f.NotCommitted
		h.retirement = err
		return out, err
	}
	retired = true
	if h.record.Phase != "created" {
		if h.record.Phase == "running" {
			return out, f.NewFault(f.CommitUnknown, f.Unknown)
		}
		out, _, err = lookupRunCommand(callCtx, d, execution)
		return out, err
	}
	h.mu.Lock()
	h.admission = "created"
	h.mu.Unlock()
	owned := context.WithValue(callCtx, runCommandHandoffKey{}, h)
	ticket, err := d.mounts.Resolve(owned, h.intent())
	h.mu.Lock()
	h.admission = ""
	h.mu.Unlock()
	if err != nil {
		return out, portError(err)
	}
	target, err := ticket.Target()
	if err != nil || runCommandTargetProjection(target) != h.record.Input.Target {
		return out, fail(f.ConfirmationStale)
	}
	call, err := d.dispatcher.Prepare(ticket)
	if err != nil {
		return out, portError(err)
	}
	h.request = call.RequestID()
	if !h.request.Valid() {
		return out, fail(f.InvalidState)
	}
	h.attempt, err = f.NewID[tc.Attempt]()
	if err != nil {
		return out, portError(err)
	}
	retired = false
	result, err = startRunCommandAttempt(callCtx, a, h, d.schemas)
	if err != nil {
		retired = result.State() == f.NotCommitted
		h.retirement = err
		return out, err
	}
	h.mu.Lock()
	h.admission = "running"
	h.mu.Unlock()
	dispatched, dispatchErr := call.Dispatch(owned)
	h.mu.Lock()
	h.admission = ""
	h.mu.Unlock()
	// Actual dispatcher return joins the original frame and terminal/connection.
	// A context signal or queued Cancel is never treated as this event.
	terminal, payload, stdout, stderr := terminalRunCommand(h, dispatched, dispatchErr)
	h.mu.Lock()
	h.terminal = &terminal
	h.payload = append([]byte(nil), payload...)
	h.stdout, h.stderr = slices.Clone(stdout), slices.Clone(stderr)
	h.mu.Unlock()
	defer clear(payload)
	defer clear(stdout)
	defer clear(stderr)
	defer clear(dispatched.Stdout)
	defer clear(dispatched.Stderr)
	finalCtx, finalCancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
	defer finalCancel()
	result, err = finishRunCommandAttempt(finalCtx, a, h, d.schemas, &terminal, payload, stdout, stderr)
	h.mu.Lock()
	h.terminal = &terminal
	h.mu.Unlock()
	if err != nil {
		h.mu.Lock()
		h.retirement = err
		h.mu.Unlock()
		return out, err
	}
	retired = true
	observation := runCommandObservation{record: h.record, terminal: &terminal}
	observation.record.Phase, _, _ = terminal.phases()
	observation.record.Version = 3
	observation.record.Attempt = h.attempt
	if terminal.Result != nil {
		observation.result, err = decodeRunCommandStoredResult(*terminal.Result, payload, stdout, stderr, h.record.ID, h.attempt, h.request)
		if err != nil {
			return RunCommandOutcome{}, err
		}
		observation.stdout, observation.stderr = slices.Clone(stdout), slices.Clone(stderr)
	}
	return RunCommandOutcome{data: func() runCommandObservation { return observation }}, nil
}

func (RunCommandExecutor) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "tool_run_command_executor")
}
func (RunCommandExecutor) MarshalJSON() ([]byte, error) {
	return []byte(`"tool_run_command_executor"`), nil
}
func (RunCommandExecutor) LogValue() slog.Value { return slog.StringValue("tool_run_command_executor") }
func (RunCommandOutcome) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "tool_run_command_outcome")
}
func (RunCommandOutcome) MarshalJSON() ([]byte, error) {
	return []byte(`"tool_run_command_outcome"`), nil
}
func (RunCommandOutcome) LogValue() slog.Value { return slog.StringValue("tool_run_command_outcome") }
