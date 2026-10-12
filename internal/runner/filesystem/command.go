package filesystem

import (
	"context"
	"errors"
	"slices"

	"github.com/LunaDeerTech/agenteam/internal/runner/command"
)

type nativeCommandRoot interface {
	runCommand(context.Context, string, command.Request, command.Sink) (command.Result, error)
}

// RunCommand pins a contained directory for this entire original call. Neither
// a Root, path to the host workspace, process owner nor descriptor is lent to
// the caller. Cancellation requests termination; admission is retained until
// the actual process, streams, owner and temporary directory have joined.
// This local primitive supplies a cwd, not a process sandbox or authorization.
func (r *Root) RunCommand(ctx context.Context, cwd string, request command.Request, sink command.Sink) (command.Result, error) {
	if !validPath(cwd, true) {
		return command.Result{}, ErrInvalid
	}
	if err := request.Validate(); err != nil {
		return command.Result{}, err
	}
	op, err := r.begin(ctx)
	if err != nil {
		return command.Result{}, err
	}
	var result command.Result
	if runner, ok := r.fs.(nativeCommandRoot); ok {
		request.Args = slices.Clone(request.Args)
		result, err = runner.runCommand(op.ctx, cwd, request, sink)
	} else {
		err = ErrUnsupported
	}
	// As with Write, a cancellation race must not erase actual completion facts.
	r.mu.Lock()
	if cancelled := contextError(op.ctx); cancelled != nil {
		err = errors.Join(err, cancelled)
	}
	delete(r.active, op)
	if r.stopped && len(r.active) == 0 {
		close(r.quiet)
	}
	r.mu.Unlock()
	op.cancel()
	return result, err
}
