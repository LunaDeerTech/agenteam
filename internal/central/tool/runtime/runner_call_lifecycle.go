package runtime

import (
	"context"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
)

type runnerCallOwner struct {
	cancel     context.CancelFunc
	done       <-chan struct{}
	retirement func() error
}

// The exact original selector is immutable from admission, including while
// discovery is running. Reading mutable handoff.binding here would race the
// producer's initial discovery and could cancel a later call in the same E.
func requireRunnerCallOwner(ctx context.Context, b tc.ToolCallBinding) error {
	if ctx == nil {
		return fail(f.ConfirmationStale)
	}
	selected, ok, err := selectedRunnerCall(ctx, b.ExecutionID)
	if err != nil {
		return err
	}
	if !ok || selected.expected == nil || !sameRunnerCallBinding(*selected.expected, b) {
		return fail(f.ConfirmationStale)
	}
	return nil
}

func (e *RunnerExecutor) callOwner(ctx context.Context, b tc.ToolCallBinding) (*runnerCallOwner, error) {
	if _, err := exactRunnerBinding(ctx, b); err != nil {
		return nil, err
	}
	if e == nil {
		return nil, fail(f.DependencyUnbound)
	}
	switch b.RunnerBinding.OperationName {
	case "read-file":
		if e.read == nil {
			return nil, fail(f.DependencyUnbound)
		}
		a := e.read.data().authority
		a.mu.Lock()
		h := a.active[b.ExecutionID]
		a.mu.Unlock()
		if h == nil {
			return nil, nil
		}
		if err := requireRunnerCallOwner(h.ctx, b); err != nil {
			return nil, err
		}
		return &runnerCallOwner{cancel: h.cancel, done: h.done, retirement: func() error {
			h.mu.Lock()
			defer h.mu.Unlock()
			if h.retirement != nil {
				return schemaReadError(h.retirement)
			}
			a.mu.Lock()
			retained := a.active[b.ExecutionID] == h
			a.mu.Unlock()
			if retained {
				return fail(f.ResourceBusy)
			}
			return nil
		}}, nil
	case "write-file":
		if e.write == nil {
			return nil, fail(f.DependencyUnbound)
		}
		a := e.write.data().authority
		a.mu.Lock()
		h := a.active[b.ExecutionID]
		a.mu.Unlock()
		if h == nil {
			return nil, nil
		}
		if err := requireRunnerCallOwner(h.ctx, b); err != nil {
			return nil, err
		}
		return &runnerCallOwner{cancel: h.cancel, done: h.done, retirement: func() error {
			h.mu.Lock()
			defer h.mu.Unlock()
			if h.retirement != nil {
				return schemaReadError(h.retirement)
			}
			a.mu.Lock()
			retained := a.active[b.ExecutionID] == h
			a.mu.Unlock()
			if retained {
				return fail(f.ResourceBusy)
			}
			return nil
		}}, nil
	case "run-command":
		if e.command == nil {
			return nil, fail(f.DependencyUnbound)
		}
		a := e.command.data().authority
		a.mu.Lock()
		h := a.active[b.ExecutionID]
		a.mu.Unlock()
		if h == nil {
			return nil, nil
		}
		if err := requireRunnerCallOwner(h.ctx, b); err != nil {
			return nil, err
		}
		return &runnerCallOwner{cancel: h.cancel, done: h.done, retirement: func() error {
			h.mu.Lock()
			defer h.mu.Unlock()
			if h.retirement != nil {
				return schemaReadError(h.retirement)
			}
			a.mu.Lock()
			retained := a.active[b.ExecutionID] == h
			a.mu.Unlock()
			if retained {
				return fail(f.ResourceBusy)
			}
			return nil
		}}, nil
	}
	return nil, fail(f.DependencyUnbound)
}
func (e *RunnerExecutor) CancelCall(ctx context.Context, b tc.ToolCallBinding) error {
	owner, err := e.callOwner(ctx, b)
	if err != nil {
		return err
	}
	if owner != nil {
		owner.cancel()
	}
	return ctx.Err()
}
func (e *RunnerExecutor) JoinCall(ctx context.Context, b tc.ToolCallBinding) error {
	owner, err := e.callOwner(ctx, b)
	if err != nil {
		return err
	}
	if owner == nil {
		return ctx.Err()
	}
	select {
	case <-owner.done:
	case <-ctx.Done():
		return ctx.Err()
	}
	if err := owner.retirement(); err != nil {
		return err
	}
	return ctx.Err()
}

var _ tc.RunnerCallLifecycle = (*RunnerExecutor)(nil)
