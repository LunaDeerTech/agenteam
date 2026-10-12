//go:build linux

package filesystem

import (
	"context"
	"errors"
	"os"

	"github.com/LunaDeerTech/agenteam/internal/runner/command"
	"golang.org/x/sys/unix"
)

type commandOwner interface {
	Run(context.Context, command.Request, command.Sink) (command.Result, error)
	Stop()
	Drain(context.Context) error
}

// These seams are private to one call. Production always constructs the real
// command owner and closes the actual File exactly once; no mutable registry.
type commandCalls struct {
	newOwner       func(*os.File) (commandOwner, error)
	closeDirectory func(*os.File) error
}

func (r *linuxRoot) runCommand(ctx context.Context, cwd string, request command.Request, sink command.Sink) (command.Result, error) {
	return r.runCommandWith(ctx, cwd, request, sink, commandCalls{
		newOwner: func(directory *os.File) (commandOwner, error) {
			owner, err := command.New(directory)
			if owner == nil {
				return nil, err
			}
			return owner, err
		},
		closeDirectory: func(directory *os.File) error { return directory.Close() },
	})
}

func (r *linuxRoot) runCommandWith(ctx context.Context, cwd string, request command.Request, sink command.Sink, calls commandCalls) (result command.Result, err error) {
	if err = contextError(ctx); err != nil {
		return result, err
	}
	fd, err := r.open(cwd, unix.O_PATH|unix.O_DIRECTORY)
	if err != nil {
		return result, err
	}
	// The name is a fixed diagnostic label, never a recovered host path. The
	// original fd is transferred to File; only File.Close owns its final close.
	directory := os.NewFile(uintptr(fd), "command-cwd")
	defer func() {
		if closeErr := calls.closeDirectory(directory); closeErr != nil {
			err = errors.Join(err, ErrUnavailable)
		}
	}()
	if err = contextError(ctx); err != nil {
		return result, err
	}
	owner, err := calls.newOwner(directory)
	if owner != nil {
		defer func() {
			owner.Stop()
			// Caller cancellation may not detach the owner from its real exit,
			// pipe closes or owned duplicate. No timeout goroutine substitutes.
			err = errors.Join(err, owner.Drain(context.Background()))
		}()
	}
	if err != nil {
		return result, err
	}
	if owner == nil {
		return result, ErrUnavailable
	}
	if err = contextError(ctx); err != nil {
		return result, err
	}
	return owner.Run(ctx, request, sink)
}
