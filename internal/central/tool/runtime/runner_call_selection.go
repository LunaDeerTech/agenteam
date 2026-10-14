package runtime

import (
	"context"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
)

// This private selector travels only between public exact-call wrappers and
// the original executor/reader. It supplies identity, never authority or bytes.
type runnerCallSelectionKey struct{}
type runnerCallSelection struct {
	identity tc.ToolCallIdentity
	expected *tc.ToolCallBinding
}

func selectRunnerCall(ctx context.Context, identity tc.ToolCallIdentity) (context.Context, error) {
	if err := contextError(ctx); err != nil {
		return nil, err
	}
	if identity.Validate() != nil {
		return nil, fail(f.InvalidArgument)
	}
	return context.WithValue(ctx, runnerCallSelectionKey{}, runnerCallSelection{identity: identity}), nil
}
func sameRunnerCallBinding(a, b tc.ToolCallBinding) bool {
	return a.Actor.Equal(b.Actor) && a.Binding == b.Binding && a.RunnerBinding == b.RunnerBinding && readFileBindingProjection(a) == readFileBindingProjection(b)
}
func selectedRunnerCall(ctx context.Context, execution i.ExecutionID) (runnerCallSelection, bool, error) {
	selected, ok := ctx.Value(runnerCallSelectionKey{}).(runnerCallSelection)
	if ok && (selected.identity.Validate() != nil || selected.identity.ExecutionID != execution) {
		return selected, true, fail(f.Forbidden)
	}
	return selected, ok, nil
}
func (s runnerCallSelection) require(b tc.ToolCallBinding) error {
	if !s.identity.Matches(b) || s.expected != nil && !sameRunnerCallBinding(*s.expected, b) {
		return fail(f.ConfirmationStale)
	}
	return nil
}

func discoverSelectedReadFileCall(ctx context.Context, authority tc.ReadFileExecutionAuthority, execution i.ExecutionID) (tc.ReadFileCallPlan, error) {
	selected, exact, err := selectedRunnerCall(ctx, execution)
	if err != nil {
		return nil, err
	}
	if !exact {
		return authority.DiscoverReadFileCall(ctx, execution)
	}
	source, ok := authority.(tc.ReadFileCallDiscovery)
	if !ok || nilPort(source) {
		return nil, fail(f.DependencyUnbound)
	}
	plan, err := source.DiscoverReadFileCallFor(ctx, selected.identity)
	if err != nil {
		return nil, err
	}
	if nilPort(plan) {
		return nil, fail(f.DependencyUnbound)
	}
	if err = selected.require(plan.Binding()); err != nil {
		return nil, err
	}
	return plan, nil
}
func (e *ReadFileExecutor) ExecuteReadFileCall(ctx context.Context, identity tc.ToolCallIdentity) (ReadFileOutcome, error) {
	ctx, err := selectRunnerCall(ctx, identity)
	if err != nil {
		return ReadFileOutcome{}, err
	}
	return e.ExecuteReadFile(ctx, identity.ExecutionID)
}
func (e *ReadFileExecutor) LookupReadFileCall(ctx context.Context, identity tc.ToolCallIdentity) (ReadFileOutcome, bool, error) {
	ctx, err := selectRunnerCall(ctx, identity)
	if err != nil {
		return ReadFileOutcome{}, false, err
	}
	return e.LookupReadFile(ctx, identity.ExecutionID)
}
func (r *ReadFileResultReader) DiscoverReadFileResultFor(ctx context.Context, identity tc.ToolCallIdentity) (tc.ReadFileResultPlan, error) {
	ctx, err := selectRunnerCall(ctx, identity)
	if err != nil {
		return nil, err
	}
	return r.DiscoverReadFileResult(ctx, identity.ExecutionID)
}

func discoverSelectedWriteFileCall(ctx context.Context, authority tc.WriteFileExecutionAuthority, execution i.ExecutionID) (tc.WriteFileCallPlan, error) {
	selected, exact, err := selectedRunnerCall(ctx, execution)
	if err != nil {
		return nil, err
	}
	if !exact {
		return authority.DiscoverWriteFileCall(ctx, execution)
	}
	source, ok := authority.(tc.WriteFileCallDiscovery)
	if !ok || nilPort(source) {
		return nil, fail(f.DependencyUnbound)
	}
	plan, err := source.DiscoverWriteFileCallFor(ctx, selected.identity)
	if err != nil {
		return nil, err
	}
	if nilPort(plan) {
		return nil, fail(f.DependencyUnbound)
	}
	if err = selected.require(plan.Binding()); err != nil {
		return nil, err
	}
	return plan, nil
}
func (e *WriteFileExecutor) ExecuteWriteFileCall(ctx context.Context, identity tc.ToolCallIdentity) (WriteFileOutcome, error) {
	ctx, err := selectRunnerCall(ctx, identity)
	if err != nil {
		return WriteFileOutcome{}, err
	}
	return e.ExecuteWriteFile(ctx, identity.ExecutionID)
}
func (e *WriteFileExecutor) LookupWriteFileCall(ctx context.Context, identity tc.ToolCallIdentity) (WriteFileOutcome, bool, error) {
	ctx, err := selectRunnerCall(ctx, identity)
	if err != nil {
		return WriteFileOutcome{}, false, err
	}
	return e.LookupWriteFile(ctx, identity.ExecutionID)
}
func (r *WriteFileResultReader) DiscoverWriteFileResultFor(ctx context.Context, identity tc.ToolCallIdentity) (tc.WriteFileResultPlan, error) {
	ctx, err := selectRunnerCall(ctx, identity)
	if err != nil {
		return nil, err
	}
	return r.DiscoverWriteFileResult(ctx, identity.ExecutionID)
}

func discoverSelectedRunCommandCall(ctx context.Context, authority tc.RunCommandExecutionAuthority, execution i.ExecutionID) (tc.RunCommandCallPlan, error) {
	selected, exact, err := selectedRunnerCall(ctx, execution)
	if err != nil {
		return nil, err
	}
	if !exact {
		return authority.DiscoverRunCommandCall(ctx, execution)
	}
	source, ok := authority.(tc.RunCommandCallDiscovery)
	if !ok || nilPort(source) {
		return nil, fail(f.DependencyUnbound)
	}
	plan, err := source.DiscoverRunCommandCallFor(ctx, selected.identity)
	if err != nil {
		return nil, err
	}
	if nilPort(plan) {
		return nil, fail(f.DependencyUnbound)
	}
	if err = selected.require(plan.Binding()); err != nil {
		return nil, err
	}
	return plan, nil
}
func (e *RunCommandExecutor) ExecuteRunCommandCall(ctx context.Context, identity tc.ToolCallIdentity) (RunCommandOutcome, error) {
	ctx, err := selectRunnerCall(ctx, identity)
	if err != nil {
		return RunCommandOutcome{}, err
	}
	return e.ExecuteRunCommand(ctx, identity.ExecutionID)
}
func (e *RunCommandExecutor) LookupRunCommandCall(ctx context.Context, identity tc.ToolCallIdentity) (RunCommandOutcome, bool, error) {
	ctx, err := selectRunnerCall(ctx, identity)
	if err != nil {
		return RunCommandOutcome{}, false, err
	}
	return e.LookupRunCommand(ctx, identity.ExecutionID)
}
func (r *RunCommandResultReader) DiscoverRunCommandResultFor(ctx context.Context, identity tc.ToolCallIdentity) (tc.RunCommandResultPlan, error) {
	ctx, err := selectRunnerCall(ctx, identity)
	if err != nil {
		return nil, err
	}
	return r.DiscoverRunCommandResult(ctx, identity.ExecutionID)
}

func discoverSelectedCommittedWriteFileCall(ctx context.Context, authority tc.CommittedWriteFileCallAuthority, execution i.ExecutionID) (tc.CommittedWriteFileCallPlan, error) {
	selected, exact, err := selectedRunnerCall(ctx, execution)
	if err != nil {
		return nil, err
	}
	if !exact {
		return authority.DiscoverCommittedWriteFileCall(ctx, execution)
	}
	source, ok := authority.(tc.CommittedWriteFileCallDiscovery)
	if !ok || nilPort(source) {
		return nil, fail(f.DependencyUnbound)
	}
	plan, err := source.DiscoverCommittedWriteFileCallFor(ctx, selected.identity)
	if err != nil {
		return nil, err
	}
	if nilPort(plan) {
		return nil, fail(f.DependencyUnbound)
	}
	if err = selected.require(plan.Binding()); err != nil {
		return nil, err
	}
	return plan, nil
}

func discoverSelectedCommittedRunCommandCall(ctx context.Context, authority tc.CommittedRunCommandCallAuthority, execution i.ExecutionID) (tc.CommittedRunCommandCallPlan, error) {
	selected, exact, err := selectedRunnerCall(ctx, execution)
	if err != nil {
		return nil, err
	}
	if !exact {
		return authority.DiscoverCommittedRunCommandCall(ctx, execution)
	}
	source, ok := authority.(tc.CommittedRunCommandCallDiscovery)
	if !ok || nilPort(source) {
		return nil, fail(f.DependencyUnbound)
	}
	plan, err := source.DiscoverCommittedRunCommandCallFor(ctx, selected.identity)
	if err != nil {
		return nil, err
	}
	if nilPort(plan) {
		return nil, fail(f.DependencyUnbound)
	}
	if err = selected.require(plan.Binding()); err != nil {
		return nil, err
	}
	return plan, nil
}
