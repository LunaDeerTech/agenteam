package runtime

import (
	"context"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
)

func (e *BuiltinExecutor) Stop() {
	if e == nil {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	e.stopped = true
	for _, call := range e.calls {
		call.cancel()
	}
	for call := range e.lookups {
		call.cancel()
	}
}
func (e *BuiltinExecutor) Joined() bool {
	if e == nil || e.authority == nil {
		return false
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.stopped && len(e.calls) == 0 && len(e.lookups) == 0
}
func (e *BuiltinExecutor) Drain(ctx context.Context) error {
	if e == nil || e.authority == nil {
		return fail(f.DependencyUnbound)
	}
	if ctx == nil {
		return fail(f.InvalidArgument)
	}
draining:
	for {
		e.mu.Lock()
		if len(e.calls) == 0 && len(e.lookups) == 0 {
			e.mu.Unlock()
			return nil
		}
		for _, call := range e.calls {
			select {
			case <-call.done:
				if call.retirement != nil {
					if call.backendPending {
						binding := call.binding
						e.mu.Unlock()
						if err := e.JoinCall(ctx, binding); err != nil {
							return err
						}
						continue draining
					}
					err := call.retirement
					e.mu.Unlock()
					return err
				}
			default:
			}
		}
		changed := e.changed
		e.mu.Unlock()
		select {
		case <-changed:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}
func (e *BuiltinExecutor) CancelCall(ctx context.Context, b tc.ToolCallBinding) error {
	if e == nil || b.Validate() != nil {
		return fail(f.InvalidArgument)
	}
	if err := contextError(ctx); err != nil {
		return err
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if call := e.calls[b.Identity()]; call != nil {
		if !sameRunnerCallBinding(call.binding, b) {
			return fail(f.Forbidden)
		}
		call.cancel()
	}
	return nil
}
func (e *BuiltinExecutor) JoinCall(ctx context.Context, b tc.ToolCallBinding) error {
	if e == nil || b.Validate() != nil {
		return fail(f.InvalidArgument)
	}
	if err := contextError(ctx); err != nil {
		return err
	}
	e.mu.Lock()
	call := e.calls[b.Identity()]
	e.mu.Unlock()
	if call == nil {
		return nil
	}
	if !sameRunnerCallBinding(call.binding, b) {
		return fail(f.Forbidden)
	}
	select {
	case <-call.done:
		e.mu.Lock()
		pending, err := call.backendPending, call.retirement
		observed := &builtinCall{binding: call.binding, plan: call.plan, locks: append([]f.LockRequest(nil), call.locks...), cause: call.cause, record: call.record, lookup: true}
		e.mu.Unlock()
		if !pending {
			return err
		}
		defer func() { clear(observed.arguments); clear(observed.body) }()
		if err = e.resolveReturned(ctx, observed); err != nil {
			return err
		}
		e.retireObserved(observed)
		e.mu.Lock()
		err = call.retirement
		e.mu.Unlock()
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

var _ tc.RunnerDispatcher = (*BuiltinExecutor)(nil)
var _ tc.RunnerCallLifecycle = (*BuiltinExecutor)(nil)
