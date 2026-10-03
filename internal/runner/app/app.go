// Package app owns the Runner process lifecycle. D02 has no listener, device
// identity, protocol connection, workspace, managed process or execution loop.
package app

import (
	"context"
	"os"

	"github.com/LunaDeerTech/agenteam/internal/platform/lifecycle"
	"github.com/LunaDeerTech/agenteam/internal/platform/logging"
	"github.com/LunaDeerTech/agenteam/internal/runner/config"
)

func Run(ctx context.Context, cfg config.Config, logger *logging.Logger, signals <-chan os.Signal) error {
	if err := cfg.Validate(); err != nil {
		return err
	}
	if logger == nil {
		return lifecycle.NewFailure(lifecycle.InitializationFailed, nil)
	}
	control := lifecycle.New(ctx, signals, cfg.ShutdownTimeout())
	defer control.Close()
	logger.Transition(logging.Starting)
	if !control.Stopping() {
		logger.Transition(logging.Unconnected)
		<-control.StopContext().Done()
	}
	logger.Transition(logging.Stopping)
	// Future Runner owners must stop their work using this cancelled context.
	// There is currently no RPC, child process or channel to pretend to drain.
	if control.Forced() {
		logger.ShutdownComplete(true, lifecycle.ForcedShutdown)
		return lifecycle.NewFailure(lifecycle.ForcedShutdown, nil)
	}
	logger.ShutdownComplete(false, "")
	return nil
}
