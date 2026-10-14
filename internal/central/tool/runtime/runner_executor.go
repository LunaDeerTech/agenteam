package runtime

import (
	"context"
	"fmt"
	"io"
	"log/slog"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
)

// RunnerExecutor routes a persisted, frozen call to the original executor.
// Registration and a caller binding grant no permission; each executor must
// reacquire its producer's exact private plan and current authorization.
type RunnerExecutor struct {
	read    *ReadFileExecutor
	write   *WriteFileExecutor
	command *RunCommandExecutor
}

func NewRunnerExecutor(read *ReadFileExecutor, write *WriteFileExecutor, command *RunCommandExecutor) (*RunnerExecutor, error) {
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
	if read != nil && (read.data == nil || !check(read.data().authority.store)) ||
		write != nil && (write.data == nil || !check(write.data().authority.store)) ||
		command != nil && (command.data == nil || !check(command.data().authority.store)) {
		return nil, fail(f.InvalidArgument)
	}
	return &RunnerExecutor{read: read, write: write, command: command}, nil
}

// RunnerOutcome reports durable operation metadata only. Protected results are
// consumed separately through RunnerResultAuthority in the Execution's Tx.
type RunnerOutcome = tc.RunnerOutcome

type runnerObservation interface {
	OperationID() tc.OperationID
	AttemptID() tc.AttemptID
	Status() (string, string, bool)
}

func runnerOutcome(v runnerObservation) RunnerOutcome {
	phase, outcome, attempted := v.Status()
	return RunnerOutcome{OperationID: v.OperationID(), AttemptID: v.AttemptID(), Phase: phase, Outcome: outcome, Attempted: attempted}
}

func exactRunnerBinding(ctx context.Context, binding tc.ToolCallBinding) (context.Context, error) {
	if err := contextError(ctx); err != nil {
		return nil, err
	}
	if binding.Validate() != nil {
		return nil, fail(f.InvalidArgument)
	}
	if binding.Binding != (tc.BuiltinBinding{}) || binding.RunnerBinding.Validate() != nil {
		return nil, fail(f.DependencyUnbound)
	}
	return context.WithValue(ctx, runnerCallSelectionKey{}, runnerCallSelection{identity: binding.Identity(), expected: &binding}), nil
}

func (e *RunnerExecutor) Execute(ctx context.Context, binding tc.ToolCallBinding) (RunnerOutcome, error) {
	ctx, err := exactRunnerBinding(ctx, binding)
	if err != nil {
		return RunnerOutcome{}, err
	}
	if e == nil {
		return RunnerOutcome{}, fail(f.DependencyUnbound)
	}
	switch binding.RunnerBinding.OperationName {
	case "read-file":
		if e.read != nil {
			out, err := e.read.ExecuteReadFile(ctx, binding.ExecutionID)
			return runnerOutcome(out), err
		}
	case "write-file":
		if e.write != nil {
			out, err := e.write.ExecuteWriteFile(ctx, binding.ExecutionID)
			return runnerOutcome(out), err
		}
	case "run-command":
		if e.command != nil {
			out, err := e.command.ExecuteRunCommand(ctx, binding.ExecutionID)
			return runnerOutcome(out), err
		}
	}
	return RunnerOutcome{}, fail(f.DependencyUnbound)
}

// Lookup cannot send a request or turn an absent result into permission to
// repeat a prior uncertain operation. It uses the same exact-call selection.
func (e *RunnerExecutor) Lookup(ctx context.Context, binding tc.ToolCallBinding) (RunnerOutcome, bool, error) {
	ctx, err := exactRunnerBinding(ctx, binding)
	if err != nil {
		return RunnerOutcome{}, false, err
	}
	if e == nil {
		return RunnerOutcome{}, false, fail(f.DependencyUnbound)
	}
	switch binding.RunnerBinding.OperationName {
	case "read-file":
		if e.read != nil {
			out, found, err := e.read.LookupReadFile(ctx, binding.ExecutionID)
			return runnerOutcome(out), found, err
		}
	case "write-file":
		if e.write != nil {
			out, found, err := e.write.LookupWriteFile(ctx, binding.ExecutionID)
			return runnerOutcome(out), found, err
		}
	case "run-command":
		if e.command != nil {
			out, found, err := e.command.LookupRunCommand(ctx, binding.ExecutionID)
			return runnerOutcome(out), found, err
		}
	}
	return RunnerOutcome{}, false, fail(f.DependencyUnbound)
}

func (RunnerExecutor) Format(w fmt.State, _ rune)   { _, _ = io.WriteString(w, "runner_tool_executor") }
func (RunnerExecutor) MarshalJSON() ([]byte, error) { return []byte(`"runner_tool_executor"`), nil }
func (RunnerExecutor) LogValue() slog.Value         { return slog.StringValue("runner_tool_executor") }

// These are the original authorities owned by the constituent executors,
// not replacement owners. Stop signals all before Drain awaits their actual
// calls and retained Unknown state; a cancellation signal never implies join.
type runnerLifecycle interface {
	Stop()
	Drain(context.Context) error
	Joined() bool
}

func (e *RunnerExecutor) authorities() []runnerLifecycle {
	if e == nil {
		return nil
	}
	var owners []runnerLifecycle
	if e.read != nil {
		owners = append(owners, &ReadFileAuthority{data: func() *readFileAuthorityState { return e.read.data().authority }})
	}
	if e.write != nil {
		owners = append(owners, &WriteFileAuthority{data: func() *writeFileAuthorityState { return e.write.data().authority }})
	}
	if e.command != nil {
		owners = append(owners, &RunCommandAuthority{data: func() *runCommandAuthorityState { return e.command.data().authority }})
	}
	return owners
}
func (e *RunnerExecutor) Stop() {
	for _, owner := range e.authorities() {
		owner.Stop()
	}
}
func (e *RunnerExecutor) Drain(ctx context.Context) error {
	if err := contextError(ctx); err != nil {
		return err
	}
	owners := e.authorities()
	if len(owners) == 0 {
		return fail(f.DependencyUnbound)
	}
	var first error
	for _, owner := range owners {
		if err := owner.Drain(ctx); err != nil && first == nil {
			first = err
		}
	}
	return first
}
func (e *RunnerExecutor) Joined() bool {
	owners := e.authorities()
	if len(owners) == 0 {
		return false
	}
	for _, owner := range owners {
		if !owner.Joined() {
			return false
		}
	}
	return true
}

var _ tc.RunnerDispatcher = (*RunnerExecutor)(nil)
