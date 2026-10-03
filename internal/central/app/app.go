// Package app composes Central's real PostgreSQL foundation and diagnostic HTTP
// server. Remaining product dependencies are unbound and ready stays false.
package app

import (
	"context"
	"errors"
	"log"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"os"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/audit"
	"github.com/LunaDeerTech/agenteam/internal/central/config"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/httpapi"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/LunaDeerTech/agenteam/internal/platform/lifecycle"
	"github.com/LunaDeerTech/agenteam/internal/platform/logging"
)

func Run(ctx context.Context, cfg config.Config, logger *logging.Logger, signals <-chan os.Signal) error {
	if logger == nil {
		return lifecycle.NewFailure(lifecycle.InitializationFailed, nil)
	}
	return run(ctx, cfg, logger, signals, dependencies{})
}

type processLogger interface {
	Transition(logging.Phase)
	Listening(netip.AddrPort)
	Failed(logging.Phase, lifecycle.FailureCode)
	ShutdownComplete(bool, lifecycle.FailureCode)
	Database(logging.DatabasePhase, string, string, int64)
	Security(logging.SecurityPhase)
	HTTPLogger() *slog.Logger
	ServerErrorLog() *log.Logger
}

type database interface {
	Check(context.Context) (postgres.DatabaseHealth, error)
	StopAdmission()
	Drain(context.Context) error
	ForceClose(context.Context) error
}

// Only package-local tests can replace assembly. Production always uses Open,
// the embedded migration source and the fixed health sampling intervals.
type dependencies struct {
	listen   func(context.Context, string, string) (net.Listener, error)
	handler  http.Handler
	open     func(context.Context, postgres.Config) (database, error)
	migrate  func(context.Context, postgres.Config) postgres.MigrationState
	health   healthTiming
	security func(context.Context, config.Config, database) (*audit.Service, error)
}

type startupResult struct {
	health          postgres.DatabaseHealth
	sampled         time.Time
	err             error
	code            lifecycle.FailureCode
	securityFailure bool
}

func run(ctx context.Context, cfg config.Config, logger processLogger, signals <-chan os.Signal, deps dependencies) error {
	if err := cfg.Validate(); err != nil {
		return err
	}
	if logger == nil {
		return lifecycle.NewFailure(lifecycle.InitializationFailed, nil)
	}
	control := lifecycle.New(ctx, signals, cfg.ShutdownTimeout())
	defer control.Close()
	logger.Transition(logging.Starting)
	owned := &resources{}
	if control.Stopping() {
		return stopStartup(logger, control, owned, nil)
	}
	if deps.listen == nil {
		deps.listen = (&net.ListenConfig{}).Listen
	}
	deps.health = deps.health.defaults()
	if deps.open == nil {
		deps.open = func(ctx context.Context, cfg postgres.Config) (database, error) { return postgres.Open(ctx, cfg) }
	}
	if deps.migrate == nil {
		deps.migrate = func(ctx context.Context, cfg postgres.Config) postgres.MigrationState {
			m, err := postgres.NewMigrator(cfg)
			if err != nil {
				return postgres.MigrationState{Fault: asDatabaseError(err)}
			}
			return m.Migrate(ctx)
		}
	}
	if deps.security == nil {
		deps.security = initializeSecurity
	}
	startup, cancelStartup := context.WithCancel(control.StopContext())
	defer cancelStartup()
	initialized := make(chan startupResult, 1)
	go func() { initialized <- initialize(startup, cfg, logger, deps, owned) }()
	var initial startupResult
	select {
	case initial = <-initialized:
		initialized = nil
	case <-control.StopContext().Done():
		cancelStartup()
		return stopStartup(logger, control, owned, initialized)
	}
	cancelStartup()
	if control.Stopping() {
		return stopStartup(logger, control, owned, initialized)
	}
	if initial.err != nil {
		if initial.code == lifecycle.InitializationFailed && !initial.securityFailure {
			databaseFailure(logger, initial.err)
		}
		logger.Failed(logging.Starting, initial.code)
		cleanup, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		cleanupResources(cleanup, owned, nil)
		return lifecycle.NewFailure(initial.code, initial.err)
	}

	monitor := newHealthMonitor(initial.health, deps.health)
	monitor.received = initial.sampled
	healthContext, cancelHealth := context.WithCancel(context.Background())
	defer cancelHealth()
	healthDone := make(chan struct{})
	go func() { defer close(healthDone); monitor.run(healthContext, owned.store(), logger) }()
	if deps.handler == nil {
		deps.handler = diagnosticRouter(monitor, true)
	}
	serving, cancelServing := context.WithCancel(context.WithoutCancel(ctx))
	defer cancelServing()
	gate := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if control.Stopping() {
			httpapi.WriteProblem(w, r, foundation.NewFault(foundation.ShuttingDown, foundation.NotStarted))
			return
		}
		deps.handler.ServeHTTP(w, r)
	})
	server := &http.Server{Handler: httpapi.Handler(logger.HTTPLogger(), gate), BaseContext: func(net.Listener) context.Context { return serving }, ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 1 << 20, ErrorLog: logger.ServerErrorLog()}
	owned.setServer(server)
	listener := owned.listener()
	serveDone := make(chan error, 1)
	go func() { serveDone <- server.Serve(listener) }()
	if address, ok := listener.Addr().(*net.TCPAddr); ok {
		logger.Listening(address.AddrPort())
	}
	logger.Transition(logging.DiagnosticServing)
	var serveFailure error
	select {
	case <-control.StopContext().Done():
	case err := <-serveDone:
		serveDone = nil
		if !control.Stopping() || !errors.Is(err, http.ErrServerClosed) {
			serveFailure = lifecycle.NewFailure(lifecycle.ServeFailed, err)
		}
		control.Stop()
	}
	logger.Transition(logging.Stopping)
	cancelHealth()
	drain, cancelDrain := control.DrainContext()
	defer cancelDrain()
	httpDone := make(chan error, 1)
	go func() { httpDone <- server.Shutdown(drain) }()
	var databaseDone chan error
	httpDrained, databaseDrained := false, false
	for {
		if httpDrained && databaseDone == nil && !databaseDrained {
			databaseDone = make(chan error, 1)
			go func() { store := owned.store(); store.StopAdmission(); databaseDone <- store.Drain(drain) }()
		}
		if httpDrained && databaseDrained && serveDone == nil && healthDone == nil && !control.Forced() {
			cancelServing()
			owned.closeHTTP()
			if serveFailure != nil {
				logger.Failed(logging.Stopped, lifecycle.ServeFailed)
				return serveFailure
			}
			logger.ShutdownComplete(false, "")
			return nil
		}
		var code lifecycle.FailureCode
		select {
		case err := <-serveDone:
			serveDone = nil
			if !errors.Is(err, http.ErrServerClosed) {
				serveFailure = lifecycle.NewFailure(lifecycle.ServeFailed, err)
			}
		case err := <-httpDone:
			httpDone = nil
			if err == nil {
				httpDrained = true
			} else if errors.Is(err, context.DeadlineExceeded) {
				code = lifecycle.ShutdownTimeout
			} else {
				code = lifecycle.ShutdownFailed
			}
		case err := <-databaseDone:
			databaseDone = nil
			if err == nil {
				databaseDrained = true
			} else {
				code = lifecycle.ShutdownTimeout
			}
		case <-healthDone:
			healthDone = nil
		case <-drain.Done():
			code = lifecycle.ShutdownTimeout
		case <-control.ForceDone():
			code = lifecycle.ForcedShutdown
		}
		if code == "" {
			continue
		}
		control.Force()
		cancelDrain()
		forced, cancel := context.WithTimeout(context.Background(), time.Second)
		// Store captures/cancels owned SQL before HTTP cancellation can release a
		// checkout. Every cleanup and join uses this single remaining force budget.
		cleanupResources(forced, owned, cancelServing)
		joinWorkers(forced, serveDone, httpDone, databaseDone, healthDone)
		cancel()
		logger.ShutdownComplete(true, code)
		return lifecycle.NewFailure(code, nil)
	}
}

func initialize(ctx context.Context, cfg config.Config, logger processLogger, deps dependencies, owned *resources) startupResult {
	parent := ctx
	ctx, cancelDatabase := context.WithTimeout(parent, cfg.Database().StartupTimeout())
	defer cancelDatabase()
	failed := func(err error) startupResult { return startupResult{err: err, code: lifecycle.InitializationFailed} }
	logger.Database(logging.DatabaseConnecting, "", "", 0)
	store, err := deps.open(ctx, cfg.Database())
	if err != nil {
		return failed(err)
	}
	if !owned.addStore(store) {
		return failed(context.Canceled)
	}
	if err := ctx.Err(); err != nil {
		return failed(err)
	}
	logger.Database(logging.DatabaseMigrating, "", "", 0)
	migration := deps.migrate(ctx, cfg.Database())
	if !migration.Migrated {
		if migration.Fault == nil {
			return failed(errors.New("MIGRATION_FAILED"))
		}
		return failed(migration.Fault)
	}
	if err := ctx.Err(); err != nil {
		return failed(err)
	}
	logger.Database(logging.DatabaseMigrated, "", "", migration.Version)
	logger.Database(logging.DatabaseChecking, "", "", 0)
	health, err := store.Check(ctx)
	if err != nil {
		return failed(err)
	}
	if !healthy(health) {
		return failed(errors.New("DATABASE_HEALTH_FAILED"))
	}
	sampled := deps.health.now()
	if err := ctx.Err(); err != nil {
		return failed(err)
	}
	cancelDatabase()
	ctx, cancelSecurity := context.WithTimeout(parent, SecurityStartupTimeout)
	defer cancelSecurity()
	logger.Security(logging.CursorInitializing)
	logger.Security(logging.AuditInitializing)
	auditService, err := deps.security(ctx, cfg, store)
	if err == nil {
		err = ctx.Err()
	}
	if err != nil {
		logger.Security(logging.SecurityFailed)
		return startupResult{err: err, code: lifecycle.InitializationFailed, securityFailure: true}
	}
	owned.setAudit(auditService)
	logger.Security(logging.SecurityInitialized)
	listener, err := deps.listen(ctx, "tcp", cfg.HTTPAddr())
	if listener != nil {
		owned.addListener(listener)
	}
	if err != nil {
		return startupResult{err: err, code: lifecycle.ListenFailed}
	}
	if err := ctx.Err(); err != nil {
		return failed(err)
	}
	logger.Database(logging.DatabaseHealthy, "", "", migration.Version)
	return startupResult{health: health, sampled: sampled}
}

func stopStartup(logger processLogger, control *lifecycle.Controller, owned *resources, initialized <-chan startupResult) error {
	logger.Transition(logging.Stopping)
	drain, cancel := control.DrainContext()
	defer cancel()
	var drained chan error
	for {
		if initialized == nil && drained == nil {
			owned.closeHTTP()
			drained = make(chan error, 1)
			go func() {
				if store := owned.store(); store != nil {
					store.StopAdmission()
					drained <- store.Drain(drain)
				} else {
					drained <- nil
				}
			}()
		}
		var code lifecycle.FailureCode
		select {
		case <-initialized:
			initialized = nil
		case err := <-drained:
			if err == nil && !control.Forced() {
				logger.ShutdownComplete(false, "")
				return nil
			}
			code = lifecycle.ShutdownTimeout
		case <-drain.Done():
			code = lifecycle.ShutdownTimeout
		case <-control.ForceDone():
			code = lifecycle.ForcedShutdown
		}
		if code == "" {
			continue
		}
		control.Force()
		cancel()
		forced, finish := context.WithTimeout(context.Background(), time.Second)
		cleanupResources(forced, owned, nil)
		if initialized != nil {
			select {
			case <-initialized:
			case <-forced.Done():
			}
		}
		joinWorkers(forced, nil, nil, drained, nil)
		finish()
		logger.ShutdownComplete(true, code)
		return lifecycle.NewFailure(code, nil)
	}
}
