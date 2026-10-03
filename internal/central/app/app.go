// Package app is Central's composition root. D02 serves diagnostics only and
// cannot claim product readiness or accept business commands.
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

	"github.com/LunaDeerTech/agenteam/internal/central/config"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/httpapi"
	"github.com/LunaDeerTech/agenteam/internal/platform/lifecycle"
	"github.com/LunaDeerTech/agenteam/internal/platform/logging"
)

// Run owns every listener, HTTP server and stop watcher it creates. The caller
// registers SIGINT/SIGTERM before calling Run and interprets the returned error.
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
	HTTPLogger() *slog.Logger
	ServerErrorLog() *log.Logger
}

// Package-local dependency injection keeps process test fixtures out of the
// production CLI and router. Production always uses the concrete defaults.
type dependencies struct {
	listen  func(context.Context, string, string) (net.Listener, error)
	handler http.Handler
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
	if control.Stopping() {
		return stoppedDuringStartup(logger, control)
	}
	if deps.handler == nil {
		deps.handler = diagnosticRouter()
	}
	if deps.listen == nil {
		deps.listen = (&net.ListenConfig{}).Listen
	}
	listener, err := deps.listen(control.StopContext(), "tcp", cfg.HTTPAddr())
	if err != nil {
		if control.Stopping() {
			return stoppedDuringStartup(logger, control)
		}
		logger.Failed(logging.Starting, lifecycle.ListenFailed)
		return lifecycle.NewFailure(lifecycle.ListenFailed, err)
	}
	var resources lifecycle.Closers
	resources.Add(listener)
	defer resources.Close()
	if control.Stopping() {
		return stoppedDuringStartup(logger, control)
	}

	// The stop request must never be the HTTP BaseContext: active handlers get
	// their drain budget before serving cancellation, independently of signals.
	serving, cancelServing := context.WithCancel(context.WithoutCancel(ctx))
	defer cancelServing()
	gate := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if control.Stopping() {
			httpapi.WriteProblem(w, r, foundation.NewFault(foundation.ShuttingDown, foundation.NotStarted))
			return
		}
		deps.handler.ServeHTTP(w, r)
	})
	server := &http.Server{
		Handler:           httpapi.Handler(logger.HTTPLogger(), gate),
		BaseContext:       func(net.Listener) context.Context { return serving },
		ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 1 << 20,
		ErrorLog: logger.ServerErrorLog(),
	}
	resources.Add(server)
	serveDone := make(chan error, 1)
	go func() { serveDone <- server.Serve(listener) }()
	if address, ok := listener.Addr().(*net.TCPAddr); ok {
		logger.Listening(address.AddrPort())
	}
	logger.Transition(logging.DiagnosticServing)
	var serveFailure error
	serveExited := false
	select {
	case <-control.StopContext().Done():
	case serveErr := <-serveDone:
		serveExited = true
		if !control.Stopping() || !errors.Is(serveErr, http.ErrServerClosed) {
			serveFailure = lifecycle.NewFailure(lifecycle.ServeFailed, serveErr)
		}
		control.Stop()
	}
	logger.Transition(logging.Stopping)
	drain, cancelDrain := control.DrainContext()
	defer cancelDrain()
	shutdownDone := make(chan error, 1)
	go func() { shutdownDone <- server.Shutdown(drain) }()
	shutdownExited, drained := false, false
	for {
		if drained && serveExited && !control.Forced() {
			cancelServing()
			resources.Close()
			if serveFailure != nil {
				logger.Failed(logging.Stopped, lifecycle.ServeFailed)
				return serveFailure
			}
			logger.ShutdownComplete(false, "")
			return nil
		}
		var forceCode lifecycle.FailureCode
		select {
		case serveErr := <-serveDone:
			serveExited = true
			serveDone = nil
			if !errors.Is(serveErr, http.ErrServerClosed) {
				serveFailure = lifecycle.NewFailure(lifecycle.ServeFailed, serveErr)
			}
		case shutdownErr := <-shutdownDone:
			shutdownExited = true
			shutdownDone = nil
			if shutdownErr == nil {
				drained = true
				continue
			}
			if errors.Is(shutdownErr, context.DeadlineExceeded) {
				forceCode = lifecycle.ShutdownTimeout
			} else {
				forceCode = lifecycle.ShutdownFailed
			}
		case <-drain.Done():
			forceCode = lifecycle.ShutdownTimeout
		case <-control.ForceDone():
			forceCode = lifecycle.ForcedShutdown
		}
		if forceCode == "" {
			continue
		}
		control.Force()
		cancelServing()
		cancelDrain()
		resources.Close()
		// HTTP Close does not wait for handlers that ignore context. Only join
		// our own Serve/Shutdown workers, bounded even if an adapter misbehaves.
		wait := time.NewTimer(time.Second)
		for !serveExited || !shutdownExited {
			select {
			case <-serveDone:
				serveExited = true
				serveDone = nil
			case <-shutdownDone:
				shutdownExited = true
				shutdownDone = nil
			case <-wait.C:
				serveExited = true
				shutdownExited = true
			}
		}
		wait.Stop()
		logger.ShutdownComplete(true, forceCode)
		return lifecycle.NewFailure(forceCode, nil)
	}
}

func stoppedDuringStartup(logger processLogger, control *lifecycle.Controller) error {
	logger.Transition(logging.Stopping)
	if control.Forced() {
		logger.ShutdownComplete(true, lifecycle.ForcedShutdown)
		return lifecycle.NewFailure(lifecycle.ForcedShutdown, nil)
	}
	logger.ShutdownComplete(false, "")
	return nil
}
