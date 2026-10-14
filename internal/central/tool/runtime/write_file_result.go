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
	p "github.com/LunaDeerTech/agenteam/internal/runnerprotocol"
)

// WriteFileResultReader is a synchronous historical reader, not an executor.
// The consuming Execution owner tracks its original calls and transaction
// returns. No ProcessGuard, active dispatch owner, current permission reader or
// stopped executor is consulted. This reader cannot prepare or send requests.
type WriteFileResultReader struct {
	data func() *writeFileResultReaderData
}
type writeFileResultReaderData struct {
	store      Store
	executions tc.CommittedWriteFileCallAuthority
}

func NewWriteFileResultReader(store Store, executions tc.CommittedWriteFileCallAuthority) (*WriteFileResultReader, error) {
	if nilPort(store) || nilPort(executions) {
		return nil, fail(f.DependencyUnbound)
	}
	d := &writeFileResultReaderData{store: store, executions: executions}
	return &WriteFileResultReader{data: func() *writeFileResultReaderData { return d }}, nil
}

type writeFileResultPlan struct {
	owner   *writeFileResultReaderData
	call    tc.CommittedWriteFileCallPlan
	binding tc.WriteFileResultBinding
	locks   []f.LockRequest
}

func (v *writeFileResultPlan) RequiredLocks() []f.LockRequest {
	if v == nil {
		return nil
	}
	return slices.Clone(v.locks)
}
func (v *writeFileResultPlan) Binding() tc.WriteFileResultBinding {
	if v == nil {
		return tc.WriteFileResultBinding{}
	}
	return v.binding
}

func writeFileResultTransaction(ctx context.Context, d *writeFileResultReaderData, cause f.TransactionCause, locks []f.LockRequest, consume func(context.Context, f.Tx, postgres.SQLExecutor) error) error {
	// Match Write Lookup: bound each short transaction independently; historical
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

func (r *WriteFileResultReader) DiscoverWriteFileResult(ctx context.Context, execution i.ExecutionID) (tc.WriteFileResultPlan, error) {
	if err := contextError(ctx); err != nil {
		return nil, err
	}
	if r == nil || r.data == nil || execution.Validate() != nil {
		return nil, fail(f.InvalidArgument)
	}
	d := r.data()
	call, err := discoverSelectedCommittedWriteFileCall(ctx, d.executions, execution)
	if err != nil {
		return nil, schemaReadError(err)
	}
	if nilPort(call) {
		return nil, fail(f.DependencyUnbound)
	}
	b := call.Binding()
	if b.Validate() != nil || b.ExecutionID != execution || b.Binding != (tc.BuiltinBinding{}) || b.RunnerBinding != tc.WriteFileRegistration().Binding {
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
	var original *writeFileOperationRecord
	err = writeFileResultTransaction(ctx, d, cause, locks, func(ctx context.Context, tx f.Tx, x postgres.SQLExecutor) error {
		return borrowCommittedWriteFileArguments(ctx, tx, d.executions, call, b, func(authorized context.Context, _ tc.WriteFileToolArguments) error {
			var err error
			original, err = loadWriteFileOperation(authorized, x, writeFileBindingProjection(b))
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
	var binding tc.WriteFileResultBinding
	err = writeFileResultTransaction(ctx, d, cause, locks, func(ctx context.Context, tx f.Tx, x postgres.SQLExecutor) error {
		return borrowCommittedWriteFileArguments(ctx, tx, d.executions, call, b, func(authorized context.Context, arguments tc.WriteFileToolArguments) error {
			row, err := loadWriteFileOperation(authorized, x, writeFileBindingProjection(b))
			if err != nil {
				return err
			}
			if row == nil || row.ID != original.ID || row.InputDigest != original.InputDigest || row.Fingerprint != original.Fingerprint {
				return fail(f.ConfirmationStale)
			}
			observation, err := loadWriteFileTerminal(authorized, x, *row, arguments.Arguments)
			if err != nil {
				return err
			}
			binding, err = writeFileResultBinding(b, observation)
			return err
		})
	})
	if err != nil {
		return nil, err
	}
	return &writeFileResultPlan{owner: d, call: call, binding: binding, locks: slices.Clone(locks)}, nil
}

func (r *WriteFileResultReader) WithWriteFileResultInTx(ctx context.Context, tx f.Tx, plan tc.WriteFileResultPlan, consume func(context.Context, tc.WriteFileResultBinding, []byte) error) error {
	if err := contextError(ctx); err != nil {
		return err
	}
	if r == nil || r.data == nil || consume == nil {
		return fail(f.InvalidArgument)
	}
	d := r.data()
	v, ok := plan.(*writeFileResultPlan)
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
	return borrowCommittedWriteFileArguments(ctx, tx, d.executions, v.call, v.binding.Call, func(authorized context.Context, arguments tc.WriteFileToolArguments) error {
		row, err := loadWriteFileOperation(authorized, x, writeFileBindingProjection(v.binding.Call))
		if err != nil {
			return err
		}
		if row == nil {
			return fail(f.ConfirmationStale)
		}
		observation, err := loadWriteFileTerminal(authorized, x, *row, arguments.Arguments)
		if err != nil {
			return err
		}
		binding, err := writeFileResultBinding(v.binding.Call, observation)
		if err != nil {
			return err
		}
		if !sameWriteFileResultBinding(binding, v.binding) {
			return fail(f.ConfirmationStale)
		}
		raw, err := p.EncodeWriteFileResult(observation.result)
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
// clears its decoded file data/canonical copy before the original port returns.
func borrowCommittedWriteFileArguments(ctx context.Context, tx f.Tx, executions tc.CommittedWriteFileCallAuthority, plan tc.CommittedWriteFileCallPlan, expected tc.ToolCallBinding, consume func(context.Context, tc.WriteFileToolArguments) error) error {
	calls := 0
	var callbackErr error
	err := executions.WithCommittedWriteFileCallInTx(ctx, tx, plan, func(authorized context.Context, b tc.ToolCallBinding, raw []byte) (err error) {
		defer func() {
			if err != nil {
				callbackErr = err
			}
		}()
		calls++
		if calls != 1 || authorized == nil || b.Validate() != nil || b.RunnerBinding != tc.WriteFileRegistration().Binding || b.Binding != (tc.BuiltinBinding{}) || !sameCommittedWriteFileBinding(b, expected) || int64(len(raw)) != int64(b.Input.Bytes) || digest(raw) != b.Input.SHA256 {
			return fail(f.Forbidden)
		}
		if err = ctx.Err(); err != nil {
			return err
		}
		arguments, err := tc.DecodeWriteFileToolArguments(authorized, raw)
		if err != nil {
			return schemaReadError(err)
		}
		defer clear(arguments.Arguments.Data)
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

func sameCommittedWriteFileBinding(a, b tc.ToolCallBinding) bool {
	return a.Actor.Equal(b.Actor) && a.Binding == b.Binding && a.RunnerBinding == b.RunnerBinding && writeFileBindingProjection(a) == writeFileBindingProjection(b)
}

func writeFileResultBinding(call tc.ToolCallBinding, observed writeFileObservation) (tc.WriteFileResultBinding, error) {
	v := observed.terminal
	if observed.record.Phase != "succeeded" || v == nil || v.Outcome != "success" || v.Result == nil || v.validate(observed.record) != nil {
		return tc.WriteFileResultBinding{}, fail(f.InvalidState)
	}
	raw, err := encode(v)
	if err != nil {
		return tc.WriteFileResultBinding{}, err
	}
	b := tc.WriteFileResultBinding{Call: call, OperationID: observed.record.ID, AttemptID: observed.record.Attempt, RequestID: v.Request,
		InputDigest: observed.record.InputDigest, Fingerprint: observed.record.Fingerprint, TerminalDigest: digest(raw), Outcome: v.Outcome,
		Code: v.Code, Attempted: v.Attempted, ResultDigest: v.Result.SHA256, ResultBytes: v.Result.Bytes}
	if b.Validate() != nil {
		return tc.WriteFileResultBinding{}, fail(f.InvalidState)
	}
	return b, nil
}

func sameWriteFileResultBinding(a, b tc.WriteFileResultBinding) bool {
	return a.Call.Actor.Equal(b.Call.Actor) && a.Call.Binding == b.Call.Binding && a.Call.RunnerBinding == b.Call.RunnerBinding &&
		writeFileBindingProjection(a.Call) == writeFileBindingProjection(b.Call) && a.OperationID == b.OperationID && a.AttemptID == b.AttemptID &&
		a.RequestID == b.RequestID && a.InputDigest == b.InputDigest && a.Fingerprint == b.Fingerprint && a.TerminalDigest == b.TerminalDigest &&
		a.Outcome == b.Outcome && a.Code == b.Code && a.Attempted == b.Attempted && a.ResultDigest == b.ResultDigest && a.ResultBytes == b.ResultBytes
}

func (WriteFileResultReader) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "tool_write_file_result_reader")
}
func (WriteFileResultReader) LogValue() slog.Value {
	return slog.StringValue("tool_write_file_result_reader")
}
func (WriteFileResultReader) MarshalJSON() ([]byte, error) {
	return []byte(`"tool_write_file_result_reader"`), nil
}
func (*writeFileResultPlan) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "tool_write_file_result_plan")
}
func (*writeFileResultPlan) LogValue() slog.Value {
	return slog.StringValue("tool_write_file_result_plan")
}
func (*writeFileResultPlan) MarshalJSON() ([]byte, error) {
	return []byte(`"tool_write_file_result_plan"`), nil
}

var _ tc.WriteFileResultAuthority = (*WriteFileResultReader)(nil)
