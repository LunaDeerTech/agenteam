package app

import (
	"context"
	"os"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/config"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/LunaDeerTech/agenteam/internal/platform/lifecycle"
	"github.com/LunaDeerTech/agenteam/internal/platform/logging"
)

// Repair accepts only compiled migration identity, never SQL or a filesystem.
// The current transactional production source returns explicit unsupported.
func Repair(ctx context.Context, cfg config.Config, logger *logging.Logger, signals <-chan os.Signal, version int64, checksum foundation.Digest) (postgres.RepairResult, error) {
	if err := cfg.Validate(); err != nil {
		return postgres.RepairResult{}, err
	}
	if logger == nil {
		return postgres.RepairResult{}, lifecycle.NewFailure(lifecycle.InitializationFailed, nil)
	}
	control := lifecycle.New(ctx, signals, cfg.ShutdownTimeout())
	defer control.Close()
	job, cancel := context.WithTimeout(control.StopContext(), cfg.Database().StartupTimeout())
	defer cancel()
	logger.Transition(logging.Starting)
	logger.Database(logging.DatabaseRepairing, "", "", version)
	done := make(chan postgres.RepairResult, 1)
	go func() {
		m, err := postgres.NewMigrator(cfg.Database())
		if err != nil {
			done <- postgres.RepairResult{Fault: asDatabaseError(err)}
			return
		}
		done <- m.Repair(job, version, checksum)
	}()
	select {
	case result := <-done:
		if !control.Stopping() {
			if result.RepairedToPending && job.Err() == nil {
				logger.Database(logging.DatabaseRepaired, "", "", result.Version)
				return result, nil
			}
			databaseFailure(logger, result.Fault)
			logger.Failed(logging.Starting, lifecycle.InitializationFailed)
			return result, lifecycle.NewFailure(lifecycle.InitializationFailed, result.Fault)
		}
		done = nil
	case <-control.StopContext().Done():
	}
	logger.Transition(logging.Stopping)
	cancel()
	drain, finish := control.DrainContext()
	defer finish()
	if done == nil && !control.Forced() {
		logger.ShutdownComplete(false, "")
		return postgres.RepairResult{}, lifecycle.NewFailure(lifecycle.InitializationFailed, context.Canceled)
	}
	var code lifecycle.FailureCode
	select {
	case <-done:
		done = nil
		if !control.Forced() {
			logger.ShutdownComplete(false, "")
			return postgres.RepairResult{}, lifecycle.NewFailure(lifecycle.InitializationFailed, context.Canceled)
		}
		code = lifecycle.ForcedShutdown
	case <-drain.Done():
		code = lifecycle.ShutdownTimeout
	case <-control.ForceDone():
		code = lifecycle.ForcedShutdown
	}
	control.Force()
	forced, stop := context.WithTimeout(context.Background(), time.Second)
	defer stop()
	if done != nil {
		select {
		case <-done:
		case <-forced.Done():
		}
	}
	logger.ShutdownComplete(true, code)
	return postgres.RepairResult{}, lifecycle.NewFailure(code, nil)
}
