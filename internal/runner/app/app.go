// Package app owns the Runner process, its original shutdown budget and the
// device client's actual return. Closing a socket is not a joined process.
package app

import (
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/platform/lifecycle"
	"github.com/LunaDeerTech/agenteam/internal/platform/logging"
	"github.com/LunaDeerTech/agenteam/internal/runner/config"
	"github.com/LunaDeerTech/agenteam/internal/runner/control"
	"github.com/LunaDeerTech/agenteam/internal/runner/identity"
	p "github.com/LunaDeerTech/agenteam/internal/runnerprotocol"
)

type clientOwner interface {
	Run(context.Context) error
	Stop()
	Drain(context.Context) error
	Force(context.Context) error
}
type clientFactory func(control.Options) (clientOwner, error)

func nativeClient(options control.Options) (clientOwner, error) { return control.New(options) }

func Run(ctx context.Context, cfg config.Config, logger *logging.Logger, signals <-chan os.Signal) error {
	return run(ctx, cfg, logger, signals, nil, nativeClient)
}

// RunEnrollment is the same process owner with one bounded stdin token source.
// The source is consumed only after obtaining the real private identity lock.
func RunEnrollment(ctx context.Context, cfg config.Config, logger *logging.Logger, signals <-chan os.Signal, input io.ReadCloser) error {
	if input == nil {
		return lifecycle.NewFailure(lifecycle.InitializationFailed, nil)
	}
	return run(ctx, cfg, logger, signals, input, nativeClient)
}
func run(ctx context.Context, cfg config.Config, logger *logging.Logger, signals <-chan os.Signal, input io.ReadCloser, construct clientFactory) error {
	if err := cfg.Validate(); err != nil {
		return err
	}
	if logger == nil || construct == nil {
		return lifecycle.NewFailure(lifecycle.InitializationFailed, nil)
	}
	coordinator := lifecycle.New(ctx, signals, cfg.ShutdownTimeout())
	defer coordinator.Close()
	logger.Transition(logging.Starting)
	if coordinator.Stopping() {
		logger.Transition(logging.Stopping)
		logger.ShutdownComplete(false, "")
		return nil
	}
	if err := cfg.OfflineCheck(input != nil); err != nil {
		logger.Failed(logging.Starting, lifecycle.InitializationFailed)
		return err
	}
	roots, err := cfg.Roots()
	if err != nil {
		logger.Failed(logging.Starting, lifecycle.InitializationFailed)
		return err
	}
	options := control.Options{IdentityFile: cfg.IdentityFile(), RunnerVersion: "development", Roots: roots, Observe: func(_ context.Context, state control.ConnectionState) {
		logger.RunnerConnection(logging.RunnerConnectionState(state))
	}}
	if device, configured := cfg.Device(); configured {
		options.Configuration = &device
	}
	if input != nil {
		options.EnrollmentSource = func(ctx context.Context) (p.EnrollmentToken, error) { return readEnrollmentToken(ctx, input) }
	}
	client, err := construct(options)
	if err != nil {
		logger.Failed(logging.Starting, lifecycle.InitializationFailed)
		return lifecycle.NewFailure(lifecycle.InitializationFailed, err)
	}
	done := make(chan error, 1)
	go func() { done <- client.Run(coordinator.StopContext()) }()
	var runErr error
	returned := false
	select {
	case runErr = <-done:
		returned = true
		coordinator.Stop()
	case <-coordinator.StopContext().Done():
	}
	client.Stop()
	logger.Transition(logging.Stopping)
	drain, cancelDrain := coordinator.DrainContext()
	defer cancelDrain()
	if !returned {
		select {
		case runErr = <-done:
			returned = true
		case <-drain.Done():
		case <-coordinator.ForceDone():
		}
	}
	if !returned || coordinator.Forced() {
		coordinator.Force()
		cancelDrain()
		force, cancelForce := context.WithTimeout(context.Background(), time.Second)
		defer cancelForce()
		forceErr := client.Force(force)
		if forceErr == nil && !returned {
			select {
			case runErr = <-done:
				returned = true
			case <-force.Done():
				forceErr = force.Err()
			}
		}
		code := lifecycle.ForcedShutdown
		if forceErr != nil || !returned {
			code = lifecycle.ShutdownTimeout
		}
		logger.ShutdownComplete(true, code)
		return lifecycle.NewFailure(code, forceErr)
	}
	if err = client.Drain(drain); err != nil {
		logger.ShutdownComplete(true, lifecycle.ShutdownFailed)
		return lifecycle.NewFailure(lifecycle.ShutdownFailed, err)
	}
	if runErr != nil && !errors.Is(runErr, context.Canceled) {
		logger.Failed(logging.Stopping, lifecycle.ServeFailed)
		return lifecycle.NewFailure(lifecycle.ServeFailed, runErr)
	}
	logger.ShutdownComplete(false, "")
	return nil
}

// Only the actual read's completion permits this owner to return. A stop closes
// the native stdin pipe but still waits for its Read to finish; the outer root
// owns a timeout and must report unjoined if a supplied reader ignores Close.
func readEnrollmentToken(ctx context.Context, input io.ReadCloser) (p.EnrollmentToken, error) {
	if err := ctx.Err(); err != nil {
		return p.EnrollmentToken{}, err
	}
	type result struct {
		raw []byte
		err error
	}
	done := make(chan result, 1)
	go func() { raw, err := io.ReadAll(io.LimitReader(input, 129)); done <- result{raw, err} }()
	var got result
	select {
	case got = <-done:
	case <-ctx.Done():
		_ = input.Close()
		got = <-done
		clear(got.raw)
		return p.EnrollmentToken{}, ctx.Err()
	}
	closeErr := input.Close()
	defer clear(got.raw)
	if got.err != nil || closeErr != nil || len(got.raw) > 128 || ctx.Err() != nil {
		return p.EnrollmentToken{}, identity.ErrInvalid
	}
	text := string(got.raw)
	if strings.HasSuffix(text, "\n") {
		text = strings.TrimSuffix(text, "\n")
	}
	token, err := p.ParseEnrollmentToken(text)
	if err != nil {
		return p.EnrollmentToken{}, identity.ErrInvalid
	}
	return token, nil
}
