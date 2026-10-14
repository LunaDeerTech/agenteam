package runtime

import (
	"context"
	"fmt"
	"io"
	"log/slog"
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

type ReadFileExecutor struct{ data func() *readFileExecutorData }
type readFileExecutorData struct {
	authority  *readFileAuthorityState
	mounts     *mount.ReadFileResolver
	dispatcher *runnerhttp.ReadFileDispatcher
	schemas    *RegistrySchemaValidator
}

func NewReadFileExecutor(authority *ReadFileAuthority, mounts *mount.ReadFileResolver, dispatcher *runnerhttp.ReadFileDispatcher, schemas *RegistrySchemaValidator) (*ReadFileExecutor, error) {
	if authority == nil || authority.data == nil || mounts == nil || dispatcher == nil || schemas == nil || schemas.data == nil {
		return nil, fail(f.DependencyUnbound)
	}
	d := authority.data()
	if !sameInstallInstance(d.store, schemas.data().store) {
		return nil, fail(f.InvalidArgument)
	}
	e := &readFileExecutorData{d, mounts, dispatcher, schemas}
	return &ReadFileExecutor{data: func() *readFileExecutorData { return e }}, nil
}

// ReadFileOutcome is a durable observation, not a next-round/Transcript receipt.
// File bytes are available only by explicit Result; default formatting is safe.
type ReadFileOutcome struct{ data func() readFileObservation }
type readFileObservation struct {
	record   readFileOperationRecord
	terminal *readFileTerminal
	result   p.ReadFileResult
}

func (v ReadFileOutcome) OperationID() tc.OperationID {
	if v.data == nil {
		return tc.OperationID{}
	}
	return v.data().record.ID
}
func (v ReadFileOutcome) AttemptID() tc.AttemptID {
	if v.data == nil {
		return tc.AttemptID{}
	}
	return v.data().record.Attempt
}
func (v ReadFileOutcome) RequestID() p.ID {
	if v.data == nil || v.data().terminal == nil {
		return ""
	}
	return v.data().terminal.Request
}
func (v ReadFileOutcome) Status() (phase, outcome string, attempted bool) {
	if v.data == nil {
		return "", "", false
	}
	o := v.data()
	if o.terminal == nil {
		return o.record.Phase, "", false
	}
	return o.record.Phase, o.terminal.Outcome, o.terminal.Attempted
}
func (v ReadFileOutcome) Result() (p.ReadFileResult, bool) {
	if v.data == nil {
		return p.ReadFileResult{}, false
	}
	o := v.data()
	if o.terminal == nil || o.terminal.Outcome != "success" {
		return p.ReadFileResult{}, false
	}
	return o.result, true
}

func readFileBindingProjection(b tc.ToolCallBinding) readFileBindingData {
	return readFileBindingData{b.ProjectID, b.AgentID, b.ExecutionID, b.RoundID, b.SnapshotID, b.InputBindingID, b.LogicalCallID, b.InvocationID, b.CallID, b.ModelVisibleName, b.Spec, b.Input.PayloadID, b.Input.SHA256, b.Input.Bytes, b.CanonicalArguments}
}
func readFileTargetProjection(t mc.ReadFileTarget) readFileTarget {
	return readFileTarget{t.MountID, t.WorkspaceID, t.RunnerID, t.WorkspaceName, t.AgentVersion, t.MountVersion}
}

func consumeReadFileInput(ctx context.Context, tx f.Tx, d *readFileAuthorityState, h *readFileHandoff, consume func(context.Context, []byte) error) error {
	calls := 0
	var callbackErr error
	err := d.executions.WithReadFileCallInTx(ctx, tx, h.plan, func(authorized context.Context, b tc.ToolCallBinding, borrowed []byte) (err error) {
		defer func() {
			if err != nil {
				callbackErr = err
			}
		}()
		calls++
		if calls != 1 || authorized == nil || b.Validate() != nil || b.RunnerBinding.Validate() != nil || b.Binding != (tc.BuiltinBinding{}) ||
			!b.Actor.Equal(h.binding.Actor) || b.RunnerBinding != h.binding.RunnerBinding || readFileBindingProjection(b) != readFileBindingProjection(h.binding) ||
			int64(len(borrowed)) != int64(b.Input.Bytes) || digest(borrowed) != b.Input.SHA256 {
			return fail(f.Forbidden)
		}
		arguments, err := tc.DecodeReadFileToolArguments(authorized, borrowed)
		if err != nil {
			return schemaReadError(err)
		}
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
		} else if h.arguments != arguments {
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

func readFileTransaction(ctx context.Context, d *readFileAuthorityState, h *readFileHandoff, consume func(context.Context, f.Tx, postgres.SQLExecutor) error) (f.CommitResult, error) {
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

func discoverReadFile(ctx context.Context, d *readFileExecutorData, execution i.ExecutionID) (*readFileHandoff, error) {
	plan, err := discoverSelectedReadFileCall(ctx, d.authority.executions, execution)
	if err != nil {
		return nil, schemaReadError(err)
	}
	if nilPort(plan) {
		return nil, fail(f.DependencyUnbound)
	}
	b := plan.Binding()
	if b.Validate() != nil || b.RunnerBinding.Validate() != nil || b.Binding != (tc.BuiltinBinding{}) || b.ExecutionID != execution {
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
	return &readFileHandoff{issuer: d.authority, executor: d, execution: execution, plan: plan, binding: b, locks: locks, cause: cause}, nil
}

func bindReadFileOperationLock(h *readFileHandoff, operation tc.OperationID) error {
	key, err := f.AggregateLock(f.OperationAggregate, operation.String())
	if err != nil {
		return fail(f.InvalidState)
	}
	h.locks, err = unionLocks(append(h.locks, f.LockRequest{Key: key, Mode: f.Exclusive}))
	return err
}

// ExecuteReadFile accepts only an Execution identity. Original protected bytes,
// Tool binding and target facts are fetched through their actual owners. There
// is no raw-arguments, caller RID, committed flag or retry input.
func (e *ReadFileExecutor) ExecuteReadFile(ctx context.Context, execution i.ExecutionID) (out ReadFileOutcome, err error) {
	if err = contextError(ctx); err != nil {
		return out, err
	}
	if e == nil || e.data == nil || execution.Validate() != nil {
		return out, fail(f.InvalidArgument)
	}
	d := e.data()
	a := d.authority
	callCtx, cancel := context.WithCancel(ctx)
	h := &readFileHandoff{issuer: a, executor: d, execution: execution, ctx: callCtx, cancel: cancel, done: make(chan struct{})}
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
		}
		h.mu.Unlock()
		a.mu.Lock()
		if retired {
			delete(a.active, execution)
		}
		a.mu.Unlock()
		close(h.done)
	}()
	plan, err := discoverReadFile(callCtx, d, execution)
	if err != nil {
		return out, err
	}
	h.plan, h.binding, h.locks, h.cause = plan.plan, plan.binding, plan.locks, plan.cause
	// Metadata discovery is read-only under original command/Execution locks.
	// The real Operation lock is added before any creation/Attempt transaction.
	var existing *readFileOperationRecord
	_, err = readFileTransaction(callCtx, a, h, func(ctx context.Context, tx f.Tx, x postgres.SQLExecutor) error {
		return consumeReadFileInput(ctx, tx, a, h, func(authorized context.Context, _ []byte) error {
			var err error
			existing, err = loadReadFileOperation(authorized, x, readFileBindingProjection(h.binding))
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
	if err = bindReadFileOperationLock(h, operation); err != nil {
		return out, err
	}
	// From the first possible write, unknown commits retain this exact owner.
	retired = false
	result, err := readFileTransaction(callCtx, a, h, func(ctx context.Context, tx f.Tx, x postgres.SQLExecutor) error {
		return consumeReadFileInput(ctx, tx, a, h, func(authorized context.Context, raw []byte) error {
			old, err := loadReadFileOperation(authorized, x, readFileBindingProjection(h.binding))
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
			if err = d.schemas.ValidateReadFileInputInTx(authorized, tx, h.binding.Spec, raw); err != nil {
				return err
			}
			b := h.binding
			target, err := d.mounts.CurrentTargetInTx(authorized, tx, mc.ReadFileTargetRequest{Actor: b.Actor, ProjectID: b.ProjectID, AgentID: b.AgentID, ExecutionID: b.ExecutionID, MountID: h.arguments.MountID})
			if err != nil {
				return portError(err)
			}
			input := readFileInput{Format: 1, Binding: readFileBindingProjection(b), Operation: b.RunnerBinding.OperationName, Revision: b.RunnerBinding.OperationRevision, ProtocolMajor: int(b.RunnerBinding.ProtocolMajor), Target: readFileTargetProjection(target)}
			encoded, err := encodeReadFileInput(input)
			if err != nil {
				return err
			}
			fingerprint, err := readFileFingerprint(input)
			if err != nil {
				return err
			}
			h.record = readFileOperationRecord{ID: operation, Input: input, InputDigest: digest(encoded), Fingerprint: fingerprint, Phase: "created", Version: 1}
			return insertReadFileOperation(authorized, x, h.record)
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
		out, _, err = lookupReadFile(callCtx, d, execution)
		return out, err
	}
	h.mu.Lock()
	h.admission = "created"
	h.mu.Unlock()
	owned := context.WithValue(callCtx, readFileHandoffKey{}, h)
	ticket, err := d.mounts.Resolve(owned, h.intent())
	h.mu.Lock()
	h.admission = ""
	h.mu.Unlock()
	if err != nil {
		return out, portError(err)
	}
	target, err := ticket.Target()
	if err != nil || readFileTargetProjection(target) != h.record.Input.Target {
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
	result, err = startReadFileAttempt(callCtx, a, h, d.schemas)
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
	terminal, payload := terminalReadFile(h, dispatched, dispatchErr)
	h.mu.Lock()
	h.terminal = &terminal
	h.payload = append([]byte(nil), payload...)
	h.mu.Unlock()
	defer clear(payload)
	finalCtx, finalCancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
	defer finalCancel()
	result, err = finishReadFileAttempt(finalCtx, a, h, d.schemas, &terminal, payload)
	if err != nil {
		h.mu.Lock()
		h.retirement = err
		h.mu.Unlock()
		return out, err
	}
	retired = true
	observation := readFileObservation{record: h.record, terminal: &terminal}
	observation.record.Phase, _, _ = terminal.phases()
	observation.record.Version = 3
	observation.record.Attempt = h.attempt
	if terminal.Result != nil {
		observation.result, err = decodeReadFileStoredResult(*terminal.Result, payload, h.record.ID, h.attempt, h.arguments.Arguments)
		if err != nil {
			return ReadFileOutcome{}, err
		}
	}
	return ReadFileOutcome{data: func() readFileObservation { return observation }}, nil
}

func (ReadFileExecutor) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "tool_read_file_executor")
}
func (ReadFileExecutor) MarshalJSON() ([]byte, error) {
	return []byte(`"tool_read_file_executor"`), nil
}
func (ReadFileExecutor) LogValue() slog.Value { return slog.StringValue("tool_read_file_executor") }
func (ReadFileOutcome) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "tool_read_file_outcome")
}
func (ReadFileOutcome) MarshalJSON() ([]byte, error) { return []byte(`"tool_read_file_outcome"`), nil }
func (ReadFileOutcome) LogValue() slog.Value         { return slog.StringValue("tool_read_file_outcome") }
