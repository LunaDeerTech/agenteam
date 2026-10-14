package runtime

import (
	"context"
	"fmt"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
	"io"
	"log/slog"
	"slices"
)

// RunnerResultReader composes the original result readers. It does not retain
// an executor or dispatch authority and cannot generate a Runner request.
type RunnerResultReader struct {
	read    *ReadFileResultReader
	write   *WriteFileResultReader
	command *RunCommandResultReader
}

func NewRunnerResultReader(read *ReadFileResultReader, write *WriteFileResultReader, command *RunCommandResultReader) (*RunnerResultReader, error) {
	if read == nil && write == nil && command == nil {
		return nil, fail(f.DependencyUnbound)
	}
	var store Store
	check := func(candidate Store) bool {
		if store == nil {
			store = candidate
			return true
		}
		return sameInstallInstance(store, candidate)
	}
	if read != nil && (read.data == nil || !check(read.data().store)) || write != nil && (write.data == nil || !check(write.data().store)) || command != nil && (command.data == nil || !check(command.data().store)) {
		return nil, fail(f.InvalidArgument)
	}
	return &RunnerResultReader{read: read, write: write, command: command}, nil
}

type runnerResultPlan struct {
	owner   *RunnerResultReader
	binding tc.RunnerResultBinding
	locks   []f.LockRequest
	read    tc.ReadFileResultPlan
	write   tc.WriteFileResultPlan
	command tc.RunCommandResultPlan
}

func (p *runnerResultPlan) RequiredLocks() []f.LockRequest {
	if p == nil {
		return nil
	}
	return slices.Clone(p.locks)
}
func (p *runnerResultPlan) Binding() tc.RunnerResultBinding {
	if p == nil {
		return tc.RunnerResultBinding{}
	}
	return p.binding
}
func (r *RunnerResultReader) DiscoverRunnerResult(ctx context.Context, binding tc.ToolCallBinding) (tc.RunnerResultPlan, error) {
	ctx, err := exactRunnerBinding(ctx, binding)
	if err != nil {
		return nil, err
	}
	if r == nil {
		return nil, fail(f.DependencyUnbound)
	}
	result := &runnerResultPlan{owner: r}
	switch binding.RunnerBinding.OperationName {
	case "read-file":
		if r.read == nil {
			return nil, fail(f.DependencyUnbound)
		}
		plan, err := r.read.DiscoverReadFileResult(ctx, binding.ExecutionID)
		if err != nil {
			return nil, err
		}
		if nilPort(plan) {
			return nil, fail(f.DependencyUnbound)
		}
		result.read = plan
		result.binding = tc.RunnerResultBinding(plan.Binding())
		result.locks = plan.RequiredLocks()
	case "write-file":
		if r.write == nil {
			return nil, fail(f.DependencyUnbound)
		}
		plan, err := r.write.DiscoverWriteFileResult(ctx, binding.ExecutionID)
		if err != nil {
			return nil, err
		}
		if nilPort(plan) {
			return nil, fail(f.DependencyUnbound)
		}
		result.write = plan
		result.binding = tc.RunnerResultBinding(plan.Binding())
		result.locks = plan.RequiredLocks()
	case "run-command":
		if r.command == nil {
			return nil, fail(f.DependencyUnbound)
		}
		plan, err := r.command.DiscoverRunCommandResult(ctx, binding.ExecutionID)
		if err != nil {
			return nil, err
		}
		if nilPort(plan) {
			return nil, fail(f.DependencyUnbound)
		}
		result.command = plan
		result.binding = tc.RunnerResultBinding(plan.Binding())
		result.locks = plan.RequiredLocks()
	}
	if result.binding.Validate() != nil || !sameRunnerCallBinding(result.binding.Call, binding) {
		return nil, fail(f.ConfirmationStale)
	}
	return result, nil
}
func sameRunnerResultBinding(a, b tc.RunnerResultBinding) bool {
	return sameRunnerCallBinding(a.Call, b.Call) && a.OperationID == b.OperationID && a.AttemptID == b.AttemptID && a.RequestID == b.RequestID &&
		a.InputDigest == b.InputDigest && a.Fingerprint == b.Fingerprint && a.TerminalDigest == b.TerminalDigest && a.Outcome == b.Outcome &&
		a.Code == b.Code && a.Attempted == b.Attempted && a.ResultDigest == b.ResultDigest && a.ResultBytes == b.ResultBytes
}
func (r *RunnerResultReader) WithRunnerResultInTx(ctx context.Context, tx f.Tx, plan tc.RunnerResultPlan, consume func(context.Context, tc.RunnerResultBinding, mc.ToolResultPart) error) error {
	if err := contextError(ctx); err != nil {
		return err
	}
	p, ok := plan.(*runnerResultPlan)
	if r == nil || !ok || p == nil || p.owner != r || consume == nil || p.binding.Validate() != nil {
		return fail(f.Forbidden)
	}
	calls := 0
	var callbackErr error
	loan := func(ctx context.Context, b tc.RunnerResultBinding, raw []byte) error {
		calls++
		if calls != 1 || b.Validate() != nil || !sameRunnerResultBinding(p.binding, b) || int64(len(raw)) != int64(b.ResultBytes) || digest(raw) != b.ResultDigest {
			callbackErr = fail(f.ConfirmationStale)
			return callbackErr
		}
		part := mc.ToolResultPart{CallID: b.Call.CallID, IsError: b.Outcome != "success", Parts: []mc.ResultPart{{Text: &mc.TextPart{Text: string(raw)}}}}
		callbackErr = consume(ctx, b, part)
		return callbackErr
	}
	var err error
	switch p.binding.Call.RunnerBinding.OperationName {
	case "read-file":
		if r.read == nil || nilPort(p.read) {
			return fail(f.Forbidden)
		}
		err = r.read.WithReadFileResultInTx(ctx, tx, p.read, func(ctx context.Context, b tc.ReadFileResultBinding, raw []byte) error {
			return loan(ctx, tc.RunnerResultBinding(b), raw)
		})
	case "write-file":
		if r.write == nil || nilPort(p.write) {
			return fail(f.Forbidden)
		}
		err = r.write.WithWriteFileResultInTx(ctx, tx, p.write, func(ctx context.Context, b tc.WriteFileResultBinding, raw []byte) error {
			return loan(ctx, tc.RunnerResultBinding(b), raw)
		})
	case "run-command":
		if r.command == nil || nilPort(p.command) {
			return fail(f.Forbidden)
		}
		err = r.command.WithRunCommandResultInTx(ctx, tx, p.command, func(ctx context.Context, b tc.RunCommandResultBinding, raw []byte) error {
			return loan(ctx, tc.RunnerResultBinding(b), raw)
		})
	}
	if err != nil {
		return err
	}
	if callbackErr != nil {
		return callbackErr
	}
	if calls != 1 {
		return fail(f.InvalidState)
	}
	return ctx.Err()
}
func (RunnerResultReader) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "runner_tool_result_reader")
}
func (RunnerResultReader) MarshalJSON() ([]byte, error) {
	return []byte(`"runner_tool_result_reader"`), nil
}
func (RunnerResultReader) LogValue() slog.Value { return slog.StringValue("runner_tool_result_reader") }

func (*runnerResultPlan) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "runner_tool_result_plan")
}
func (*runnerResultPlan) MarshalJSON() ([]byte, error) {
	return []byte(`"runner_tool_result_plan"`), nil
}
func (*runnerResultPlan) LogValue() slog.Value { return slog.StringValue("runner_tool_result_plan") }

var _ tc.RunnerResultAuthority = (*RunnerResultReader)(nil)
