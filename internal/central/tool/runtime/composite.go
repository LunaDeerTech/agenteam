package runtime

import (
	"context"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
)

// CompositeExecutor delegates to the original owners. A builtin binding is
// never converted into a Runner operation, and an unknown binding cannot
// select a backend by its display name or caller arguments.
type CompositeExecutor struct {
	runners  *RunnerExecutor
	results  *RunnerResultReader
	builtins *BuiltinExecutor
}

func NewCompositeExecutor(runners *RunnerExecutor, results *RunnerResultReader, builtins *BuiltinExecutor) (*CompositeExecutor, error) {
	if runners == nil || results == nil || builtins == nil || builtins.authority == nil {
		return nil, fail(f.DependencyUnbound)
	}
	for _, candidate := range []Store{runnerExecutorStore(runners), runnerResultStore(results)} {
		if nilPort(candidate) || !sameInstallInstance(candidate, builtins.authority.store) {
			return nil, fail(f.InvalidArgument)
		}
	}
	return &CompositeExecutor{runners, results, builtins}, nil
}

func runnerExecutorStore(e *RunnerExecutor) Store {
	if e.read != nil {
		return e.read.data().authority.store
	}
	if e.write != nil {
		return e.write.data().authority.store
	}
	if e.command != nil {
		return e.command.data().authority.store
	}
	return nil
}
func runnerResultStore(r *RunnerResultReader) Store {
	if r.read != nil {
		return r.read.data().store
	}
	if r.write != nil {
		return r.write.data().store
	}
	if r.command != nil {
		return r.command.data().store
	}
	return nil
}

func (e *CompositeExecutor) selectCall(b tc.ToolCallBinding) (tc.RunnerDispatcher, error) {
	if e == nil || b.Validate() != nil {
		return nil, fail(f.InvalidArgument)
	}
	if tc.WorkArtifactBuiltinBinding(b.Binding) {
		return e.builtins, nil
	}
	if b.Binding == (tc.BuiltinBinding{}) && b.RunnerBinding.Validate() == nil {
		return e.runners, nil
	}
	return nil, fail(f.DependencyUnbound)
}
func (e *CompositeExecutor) Execute(ctx context.Context, b tc.ToolCallBinding) (tc.RunnerOutcome, error) {
	p, err := e.selectCall(b)
	if err != nil {
		return tc.RunnerOutcome{}, err
	}
	return p.Execute(ctx, b)
}
func (e *CompositeExecutor) Lookup(ctx context.Context, b tc.ToolCallBinding) (tc.RunnerOutcome, bool, error) {
	p, err := e.selectCall(b)
	if err != nil {
		return tc.RunnerOutcome{}, false, err
	}
	return p.Lookup(ctx, b)
}
func (e *CompositeExecutor) CancelCall(ctx context.Context, b tc.ToolCallBinding) error {
	p, err := e.selectCall(b)
	if err != nil {
		return err
	}
	return p.(tc.RunnerCallLifecycle).CancelCall(ctx, b)
}
func (e *CompositeExecutor) JoinCall(ctx context.Context, b tc.ToolCallBinding) error {
	p, err := e.selectCall(b)
	if err != nil {
		return err
	}
	return p.(tc.RunnerCallLifecycle).JoinCall(ctx, b)
}
func (e *CompositeExecutor) Stop() {
	if e == nil {
		return
	}
	e.runners.Stop()
	e.builtins.Stop()
}
func (e *CompositeExecutor) Drain(ctx context.Context) error {
	if e == nil {
		return fail(f.DependencyUnbound)
	}
	e.Stop()
	first := e.runners.Drain(ctx)
	if err := e.builtins.Drain(ctx); first == nil {
		first = err
	}
	return first
}
func (e *CompositeExecutor) Joined() bool {
	return e != nil && e.runners.Joined() && e.builtins.Joined()
}

type compositeResultPlan struct {
	owner  *CompositeExecutor
	reader tc.RunnerResultAuthority
	plan   tc.RunnerResultPlan
}

func (p *compositeResultPlan) RequiredLocks() []f.LockRequest {
	if p == nil || nilPort(p.plan) {
		return nil
	}
	return p.plan.RequiredLocks()
}
func (p *compositeResultPlan) Binding() tc.RunnerResultBinding {
	if p == nil || nilPort(p.plan) {
		return tc.RunnerResultBinding{}
	}
	return p.plan.Binding()
}
func (e *CompositeExecutor) DiscoverRunnerResult(ctx context.Context, b tc.ToolCallBinding) (tc.RunnerResultPlan, error) {
	if _, err := e.selectCall(b); err != nil {
		return nil, err
	}
	var reader tc.RunnerResultAuthority = e.results
	if tc.WorkArtifactBuiltinBinding(b.Binding) {
		reader = e.builtins
	}
	p, err := reader.DiscoverRunnerResult(ctx, b)
	if err != nil {
		return nil, err
	}
	if nilPort(p) || p.Binding().Validate() != nil || !sameRunnerCallBinding(p.Binding().Call, b) {
		return nil, fail(f.ConfirmationStale)
	}
	return &compositeResultPlan{e, reader, p}, nil
}
func (e *CompositeExecutor) WithRunnerResultInTx(ctx context.Context, tx f.Tx, plan tc.RunnerResultPlan, consume func(context.Context, tc.RunnerResultBinding, mc.ToolResultPart) error) error {
	p, ok := plan.(*compositeResultPlan)
	if e == nil || !ok || p == nil || p.owner != e || nilPort(p.plan) || consume == nil {
		return fail(f.Forbidden)
	}
	return p.reader.WithRunnerResultInTx(ctx, tx, p.plan, consume)
}

var _ tc.RunnerDispatcher = (*CompositeExecutor)(nil)
var _ tc.RunnerResultAuthority = (*CompositeExecutor)(nil)
var _ tc.RunnerCallLifecycle = (*CompositeExecutor)(nil)
