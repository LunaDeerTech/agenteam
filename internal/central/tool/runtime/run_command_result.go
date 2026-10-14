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
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
)

// RunCommandResultReader is a synchronous historical reader, not an executor.
// The consuming Execution owner tracks its original calls and transaction
// returns. No ProcessGuard, active dispatch owner, current permission reader or
// stopped executor is consulted. This reader cannot prepare or send requests.
type RunCommandResultReader struct {
	data func() *runCommandResultReaderData
}
type runCommandResultReaderData struct {
	store      Store
	executions tc.CommittedRunCommandCallAuthority
}

func NewRunCommandResultReader(store Store, executions tc.CommittedRunCommandCallAuthority) (*RunCommandResultReader, error) {
	if nilPort(store) || nilPort(executions) {
		return nil, fail(f.DependencyUnbound)
	}
	d := &runCommandResultReaderData{store: store, executions: executions}
	return &RunCommandResultReader{data: func() *runCommandResultReaderData { return d }}, nil
}

type runCommandResultPlan struct {
	owner   *runCommandResultReaderData
	call    tc.CommittedRunCommandCallPlan
	binding tc.RunCommandResultBinding
	locks   []f.LockRequest
}

func (v *runCommandResultPlan) RequiredLocks() []f.LockRequest {
	if v == nil {
		return nil
	}
	return slices.Clone(v.locks)
}
func (v *runCommandResultPlan) Binding() tc.RunCommandResultBinding {
	if v == nil {
		return tc.RunCommandResultBinding{}
	}
	return v.binding
}

func runCommandResultTransaction(ctx context.Context, d *runCommandResultReaderData, cause f.TransactionCause, locks []f.LockRequest, consume func(context.Context, f.Tx, postgres.SQLExecutor) error) error {
	// Match Command Lookup: bound each short transaction independently; historical
	// discovery and the complete sequence remain subject to the caller context.
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	completed, calls := false, 0
	result := d.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		calls++
		if calls != 1 {
			return fail(f.InvalidState)
		}
		x, err := d.store.InTx(tx)
		if err != nil {
			return schemaReadError(err)
		}
		if err = d.store.AcquireAll(ctx, tx, locks); err != nil {
			return schemaReadError(err)
		}
		if err = d.store.RequireHeldLocks(ctx, tx, locks); err != nil {
			return schemaReadError(err)
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
		return fail(f.InvalidState)
	}
	if err := commitError(result); err != nil {
		return err
	}
	return ctx.Err()
}

func (r *RunCommandResultReader) DiscoverRunCommandResult(ctx context.Context, execution i.ExecutionID) (tc.RunCommandResultPlan, error) {
	if err := contextError(ctx); err != nil {
		return nil, err
	}
	if r == nil || r.data == nil || execution.Validate() != nil {
		return nil, fail(f.InvalidArgument)
	}
	d := r.data()
	call, err := discoverSelectedCommittedRunCommandCall(ctx, d.executions, execution)
	if err != nil {
		return nil, schemaReadError(err)
	}
	if nilPort(call) {
		return nil, fail(f.DependencyUnbound)
	}
	b := call.Binding()
	if b.Validate() != nil || b.ExecutionID != execution || b.Binding != (tc.BuiltinBinding{}) || b.RunnerBinding != tc.RunCommandRegistration().Binding {
		return nil, fail(f.Forbidden)
	}
	cause, err := inputCause(b)
	if err != nil {
		return nil, err
	}
	locks, err := baseLocks(b, cause, call.RequiredLocks())
	if err != nil {
		return nil, err
	}
	// The first history-validated read discovers only the original Operation
	// identity. Its lock is added before reading any terminal result bytes.
	var original *runCommandOperationRecord
	err = runCommandResultTransaction(ctx, d, cause, locks, func(ctx context.Context, tx f.Tx, x postgres.SQLExecutor) error {
		return borrowCommittedRunCommandArguments(ctx, tx, d.executions, call, b, func(authorized context.Context, _ tc.RunCommandToolArguments) error {
			var err error
			original, err = loadRunCommandOperation(authorized, x, runCommandBindingProjection(b))
			return err
		})
	})
	if err != nil {
		return nil, err
	}
	if original == nil {
		return nil, fail(f.NotFound)
	}
	key, err := f.AggregateLock(f.OperationAggregate, original.ID.String())
	if err != nil {
		return nil, fail(f.InvalidState)
	}
	locks, err = unionLocks(append(locks, f.LockRequest{Key: key, Mode: f.Exclusive}))
	if err != nil {
		return nil, err
	}
	var binding tc.RunCommandResultBinding
	err = runCommandResultTransaction(ctx, d, cause, locks, func(ctx context.Context, tx f.Tx, x postgres.SQLExecutor) error {
		return borrowCommittedRunCommandArguments(ctx, tx, d.executions, call, b, func(authorized context.Context, _ tc.RunCommandToolArguments) error {
			row, err := loadRunCommandOperation(authorized, x, runCommandBindingProjection(b))
			if err != nil {
				return err
			}
			if row == nil || row.ID != original.ID || row.InputDigest != original.InputDigest || row.Fingerprint != original.Fingerprint {
				return fail(f.ConfirmationStale)
			}
			observation, err := loadRunCommandTerminal(authorized, x, *row)
			if err != nil {
				return err
			}
			defer clear(observation.stdout)
			defer clear(observation.stderr)
			binding, err = runCommandResultBinding(b, observation)
			return err
		})
	})
	if err != nil {
		return nil, err
	}
	return &runCommandResultPlan{owner: d, call: call, binding: binding, locks: slices.Clone(locks)}, nil
}

func (r *RunCommandResultReader) WithRunCommandResultInTx(ctx context.Context, tx f.Tx, plan tc.RunCommandResultPlan, consume func(context.Context, tc.RunCommandResultBinding, []byte) error) error {
	if err := contextError(ctx); err != nil {
		return err
	}
	if r == nil || r.data == nil || consume == nil {
		return fail(f.InvalidArgument)
	}
	d := r.data()
	v, ok := plan.(*runCommandResultPlan)
	if !ok || v == nil || v.owner != d || v.binding.Validate() != nil {
		return fail(f.Forbidden)
	}
	x, err := d.store.InTx(tx)
	if err != nil {
		return schemaReadError(err)
	}
	if err = d.store.RequireHeldLocks(ctx, tx, v.locks); err != nil {
		return schemaReadError(err)
	}
	return borrowCommittedRunCommandArguments(ctx, tx, d.executions, v.call, v.binding.Call, func(authorized context.Context, _ tc.RunCommandToolArguments) error {
		row, err := loadRunCommandOperation(authorized, x, runCommandBindingProjection(v.binding.Call))
		if err != nil {
			return err
		}
		if row == nil {
			return fail(f.ConfirmationStale)
		}
		observation, err := loadRunCommandTerminal(authorized, x, *row)
		if err != nil {
			return err
		}
		defer clear(observation.stdout)
		defer clear(observation.stderr)
		binding, err := runCommandResultBinding(v.binding.Call, observation)
		if err != nil {
			return err
		}
		if !sameRunCommandResultBinding(binding, v.binding) {
			return fail(f.ConfirmationStale)
		}
		raw, err := tc.EncodeRunCommandResultBundle(tc.RunCommandResultBundle{Report: observation.result, Stdout: observation.stdout, Stderr: observation.stderr})
		if err != nil {
			return fail(f.InvalidState)
		}
		defer clear(raw)
		if digest(raw) != binding.ResultDigest || f.Progress(len(raw)) != binding.ResultBytes {
			return fail(f.InvalidState)
		}
		return consume(authorized, binding, raw)
	})
}

// This is a historical protected-argument loan, not the dispatch handoff helper.
// Both scopes own their copies: Execution clears its raw loan, and this reader
// clears its decoded argv slice/canonical copy before the original port returns.
func borrowCommittedRunCommandArguments(ctx context.Context, tx f.Tx, executions tc.CommittedRunCommandCallAuthority, plan tc.CommittedRunCommandCallPlan, expected tc.ToolCallBinding, consume func(context.Context, tc.RunCommandToolArguments) error) error {
	calls := 0
	var callbackErr error
	err := executions.WithCommittedRunCommandCallInTx(ctx, tx, plan, func(authorized context.Context, b tc.ToolCallBinding, raw []byte) (err error) {
		defer func() {
			if err != nil {
				callbackErr = err
			}
		}()
		calls++
		if calls != 1 || authorized == nil || b.Validate() != nil || b.RunnerBinding != tc.RunCommandRegistration().Binding || b.Binding != (tc.BuiltinBinding{}) || !sameCommittedRunCommandBinding(b, expected) || int64(len(raw)) != int64(b.Input.Bytes) || digest(raw) != b.Input.SHA256 {
			return fail(f.Forbidden)
		}
		if err = ctx.Err(); err != nil {
			return err
		}
		arguments, err := tc.DecodeRunCommandToolArguments(authorized, raw)
		if err != nil {
			return schemaReadError(err)
		}
		defer clear(arguments.Arguments.Args)
		canonical, err := cursor.CanonicalJSON(raw)
		if err != nil {
			return fail(f.InvalidState)
		}
		defer clear(canonical)
		if digest(canonical) != b.CanonicalArguments {
			return fail(f.ConfirmationStale)
		}
		if err = consume(authorized, arguments); err != nil {
			return err
		}
		if err = authorized.Err(); err != nil {
			return err
		}
		return ctx.Err()
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

func sameCommittedRunCommandBinding(a, b tc.ToolCallBinding) bool {
	return a.Actor.Equal(b.Actor) && a.Binding == b.Binding && a.RunnerBinding == b.RunnerBinding && runCommandBindingProjection(a) == runCommandBindingProjection(b)
}

func runCommandResultBinding(call tc.ToolCallBinding, observed runCommandObservation) (tc.RunCommandResultBinding, error) {
	v := observed.terminal
	if v == nil || !v.ReportAvailable || v.Result == nil || v.validate(observed.record) != nil || !tc.ValidRunCommandResultClassification(v.Outcome, v.Code) {
		return tc.RunCommandResultBinding{}, fail(f.InvalidState)
	}
	raw, err := encode(v)
	if err != nil {
		return tc.RunCommandResultBinding{}, err
	}
	bundle, err := tc.EncodeRunCommandResultBundle(tc.RunCommandResultBundle{Report: observed.result, Stdout: observed.stdout, Stderr: observed.stderr})
	if err != nil {
		return tc.RunCommandResultBinding{}, fail(f.InvalidState)
	}
	defer clear(bundle)
	classified, code := tc.ClassifyRunCommandResult(observed.result)
	if classified != v.Outcome || code != v.Code {
		return tc.RunCommandResultBinding{}, fail(f.InvalidState)
	}
	b := tc.RunCommandResultBinding{Call: call, OperationID: observed.record.ID, AttemptID: observed.record.Attempt, RequestID: v.Request,
		InputDigest: observed.record.InputDigest, Fingerprint: observed.record.Fingerprint, TerminalDigest: digest(raw), Outcome: v.Outcome,
		Code: v.Code, Attempted: v.Attempted, ResultDigest: digest(bundle), ResultBytes: f.Progress(len(bundle))}
	if b.Validate() != nil {
		return tc.RunCommandResultBinding{}, fail(f.InvalidState)
	}
	return b, nil
}

func sameRunCommandResultBinding(a, b tc.RunCommandResultBinding) bool {
	return a.Call.Actor.Equal(b.Call.Actor) && a.Call.Binding == b.Call.Binding && a.Call.RunnerBinding == b.Call.RunnerBinding &&
		runCommandBindingProjection(a.Call) == runCommandBindingProjection(b.Call) && a.OperationID == b.OperationID && a.AttemptID == b.AttemptID &&
		a.RequestID == b.RequestID && a.InputDigest == b.InputDigest && a.Fingerprint == b.Fingerprint && a.TerminalDigest == b.TerminalDigest &&
		a.Outcome == b.Outcome && a.Code == b.Code && a.Attempted == b.Attempted && a.ResultDigest == b.ResultDigest && a.ResultBytes == b.ResultBytes
}

func (RunCommandResultReader) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "tool_run_command_result_reader")
}
func (RunCommandResultReader) LogValue() slog.Value {
	return slog.StringValue("tool_run_command_result_reader")
}
func (RunCommandResultReader) MarshalJSON() ([]byte, error) {
	return []byte(`"tool_run_command_result_reader"`), nil
}
func (*runCommandResultPlan) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "tool_run_command_result_plan")
}
func (*runCommandResultPlan) LogValue() slog.Value {
	return slog.StringValue("tool_run_command_result_plan")
}
func (*runCommandResultPlan) MarshalJSON() ([]byte, error) {
	return []byte(`"tool_run_command_result_plan"`), nil
}

var _ tc.RunCommandResultAuthority = (*RunCommandResultReader)(nil)
