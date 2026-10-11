package runtime

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"slices"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
	p "github.com/LunaDeerTech/agenteam/internal/runnerprotocol"
)

// ReadFileResultReader is a synchronous historical reader, not an executor.
// The consuming Execution owner tracks its original calls and transaction
// returns. No ProcessGuard, active dispatch owner or stopped executor is reused
// as current authorization, and this reader cannot prepare or send requests.
type ReadFileResultReader struct {
	data func() *readFileResultReaderData
}
type readFileResultReaderData struct {
	store      Store
	executions tc.ReadFileExecutionAuthority
}

func NewReadFileResultReader(store Store, executions tc.ReadFileExecutionAuthority) (*ReadFileResultReader, error) {
	if nilPort(store) || nilPort(executions) {
		return nil, fail(f.DependencyUnbound)
	}
	d := &readFileResultReaderData{store: store, executions: executions}
	return &ReadFileResultReader{data: func() *readFileResultReaderData { return d }}, nil
}

type readFileResultPlan struct {
	owner   *readFileResultReaderData
	call    tc.ReadFileCallPlan
	binding tc.ReadFileResultBinding
	locks   []f.LockRequest
}

func (v *readFileResultPlan) RequiredLocks() []f.LockRequest {
	if v == nil {
		return nil
	}
	return slices.Clone(v.locks)
}
func (v *readFileResultPlan) Binding() tc.ReadFileResultBinding {
	if v == nil {
		return tc.ReadFileResultBinding{}
	}
	return v.binding
}

func readFileResultTransaction(ctx context.Context, d *readFileResultReaderData, cause f.TransactionCause, locks []f.LockRequest, consume func(context.Context, f.Tx, postgres.SQLExecutor) error) error {
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

func (r *ReadFileResultReader) DiscoverReadFileResult(ctx context.Context, execution i.ExecutionID) (tc.ReadFileResultPlan, error) {
	if err := contextError(ctx); err != nil {
		return nil, err
	}
	if r == nil || r.data == nil || execution.Validate() != nil {
		return nil, fail(f.InvalidArgument)
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	d := r.data()
	call, err := d.executions.DiscoverReadFileCall(ctx, execution)
	if err != nil {
		return nil, schemaReadError(err)
	}
	if nilPort(call) {
		return nil, fail(f.DependencyUnbound)
	}
	b := call.Binding()
	if b.Validate() != nil || b.ExecutionID != execution || b.Binding != (tc.BuiltinBinding{}) || b.RunnerBinding != tc.ReadFileRegistration().Binding {
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
	// The first current-authorized read discovers only the original Operation
	// identity. Its lock is added before reading any terminal result bytes.
	h := &readFileHandoff{binding: b, plan: call, locks: locks}
	loan := &readFileAuthorityState{executions: d.executions}
	var original *readFileOperationRecord
	err = readFileResultTransaction(ctx, d, cause, locks, func(ctx context.Context, tx f.Tx, x postgres.SQLExecutor) error {
		return consumeReadFileInput(ctx, tx, loan, h, func(authorized context.Context, _ []byte) error {
			var err error
			original, err = loadReadFileOperation(authorized, x, readFileBindingProjection(b))
			return err
		})
	})
	if err != nil {
		return nil, err
	}
	if original == nil {
		return nil, fail(f.NotFound)
	}
	if err = bindReadFileOperationLock(h, original.ID); err != nil {
		return nil, err
	}
	var binding tc.ReadFileResultBinding
	err = readFileResultTransaction(ctx, d, cause, h.locks, func(ctx context.Context, tx f.Tx, x postgres.SQLExecutor) error {
		return consumeReadFileInput(ctx, tx, loan, h, func(authorized context.Context, _ []byte) error {
			row, err := loadReadFileOperation(authorized, x, readFileBindingProjection(b))
			if err != nil {
				return err
			}
			if row == nil || row.ID != original.ID || row.InputDigest != original.InputDigest {
				return fail(f.ConfirmationStale)
			}
			observation, err := loadReadFileTerminal(authorized, x, *row, h.arguments.Arguments)
			if err != nil {
				return err
			}
			binding, err = readFileResultBinding(b, observation)
			return err
		})
	})
	if err != nil {
		return nil, err
	}
	return &readFileResultPlan{owner: d, call: call, binding: binding, locks: slices.Clone(h.locks)}, nil
}

func (r *ReadFileResultReader) WithReadFileResultInTx(ctx context.Context, tx f.Tx, plan tc.ReadFileResultPlan, consume func(context.Context, tc.ReadFileResultBinding, []byte) error) error {
	if err := contextError(ctx); err != nil {
		return err
	}
	if r == nil || r.data == nil || consume == nil {
		return fail(f.InvalidArgument)
	}
	d := r.data()
	v, ok := plan.(*readFileResultPlan)
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
	h := &readFileHandoff{binding: v.binding.Call, plan: v.call}
	// Reuse only the original protected-input verification helper. This local
	// loan has no process/admission/active state and cannot authorize dispatch.
	loan := &readFileAuthorityState{executions: d.executions}
	return consumeReadFileInput(ctx, tx, loan, h, func(authorized context.Context, _ []byte) error {
		row, err := loadReadFileOperation(authorized, x, readFileBindingProjection(v.binding.Call))
		if err != nil {
			return err
		}
		if row == nil {
			return fail(f.ConfirmationStale)
		}
		observation, err := loadReadFileTerminal(authorized, x, *row, h.arguments.Arguments)
		if err != nil {
			return err
		}
		binding, err := readFileResultBinding(v.binding.Call, observation)
		if err != nil {
			return err
		}
		if !sameReadFileResultBinding(binding, v.binding) {
			return fail(f.ConfirmationStale)
		}
		raw, err := p.EncodeReadFileResult(observation.result)
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

func readFileResultBinding(call tc.ToolCallBinding, observed readFileObservation) (tc.ReadFileResultBinding, error) {
	v := observed.terminal
	if observed.record.Phase != "succeeded" || v == nil || v.Outcome != "success" || v.Result == nil || v.validate(observed.record) != nil {
		return tc.ReadFileResultBinding{}, fail(f.InvalidState)
	}
	raw, err := encode(v)
	if err != nil {
		return tc.ReadFileResultBinding{}, err
	}
	b := tc.ReadFileResultBinding{Call: call, OperationID: observed.record.ID, AttemptID: observed.record.Attempt, RequestID: v.Request,
		InputDigest: observed.record.InputDigest, Fingerprint: observed.record.Fingerprint, TerminalDigest: digest(raw), Outcome: v.Outcome,
		Code: v.Code, Attempted: v.Attempted, ResultDigest: v.Result.SHA256, ResultBytes: v.Result.Bytes}
	if b.Validate() != nil {
		return tc.ReadFileResultBinding{}, fail(f.InvalidState)
	}
	return b, nil
}

func sameReadFileResultBinding(a, b tc.ReadFileResultBinding) bool {
	return a.Call.Actor.Equal(b.Call.Actor) && a.Call.Binding == b.Call.Binding && a.Call.RunnerBinding == b.Call.RunnerBinding &&
		readFileBindingProjection(a.Call) == readFileBindingProjection(b.Call) && a.OperationID == b.OperationID && a.AttemptID == b.AttemptID &&
		a.RequestID == b.RequestID && a.InputDigest == b.InputDigest && a.Fingerprint == b.Fingerprint && a.TerminalDigest == b.TerminalDigest &&
		a.Outcome == b.Outcome && a.Code == b.Code && a.Attempted == b.Attempted && a.ResultDigest == b.ResultDigest && a.ResultBytes == b.ResultBytes
}

func (ReadFileResultReader) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "tool_read_file_result_reader")
}
func (ReadFileResultReader) LogValue() slog.Value {
	return slog.StringValue("tool_read_file_result_reader")
}
func (ReadFileResultReader) MarshalJSON() ([]byte, error) {
	return []byte(`"tool_read_file_result_reader"`), nil
}
func (*readFileResultPlan) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "tool_read_file_result_plan")
}
func (*readFileResultPlan) LogValue() slog.Value {
	return slog.StringValue("tool_read_file_result_plan")
}
func (*readFileResultPlan) MarshalJSON() ([]byte, error) {
	return []byte(`"tool_read_file_result_plan"`), nil
}

var _ tc.ReadFileResultAuthority = (*ReadFileResultReader)(nil)
