package workspace

import (
	"context"
	"errors"

	"github.com/LunaDeerTech/agenteam/internal/runner/command"
)

type commandRoot interface {
	RunCommand(context.Context, string, command.Request, command.Sink) (command.Result, error)
}

// RunCommand uses only this Handle's pinned filesystem owner. It never
// reconstructs a host path from Metadata and does not expose an fd or Root.
// The Manager retains the Handle and original call until all command cleanup
// actually returns, including when Stop or a caller deadline wins the race.
func (h *Handle) RunCommand(ctx context.Context, cwd string, request command.Request, sink command.Sink) (command.Result, error) {
	if h == nil || h.manager == nil {
		return command.Result{}, ErrClosed
	}
	m := h.manager
	op, err := m.begin(ctx, h)
	if err != nil {
		return command.Result{}, err
	}
	var result command.Result
	if runner, ok := h.root.(commandRoot); ok {
		result, err = runner.RunCommand(op.ctx, cwd, request, sink)
	} else {
		err = ErrUnsupported
	}
	m.mu.Lock()
	if cancelled := contextError(op.ctx); cancelled != nil {
		err = errors.Join(err, cancelled)
	}
	m.retireLocked(op)
	m.mu.Unlock()
	return result, err
}
