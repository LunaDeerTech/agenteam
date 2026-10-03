// Package logging emits explicitly projected process diagnostics. It does not
// accept application objects, raw errors, credentials, or arbitrary headers.
package logging

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"log"
	"log/slog"
	"net/netip"

	"github.com/LunaDeerTech/agenteam/internal/platform/lifecycle"
)

type Service string

const (
	Central Service = "central"
	Runner  Service = "runner"
)

type Phase string

const (
	Starting          Phase = "starting"
	DiagnosticServing Phase = "diagnostic_serving"
	Unconnected       Phase = "unconnected"
	Stopping          Phase = "stopping"
	Stopped           Phase = "stopped"
)

func (p Phase) safe() Phase {
	switch p {
	case Starting, DiagnosticServing, Unconnected, Stopping, Stopped:
		return p
	}
	return Starting
}

type Logger struct{ logger *slog.Logger }

func New(service Service, level slog.Level, output io.Writer) (*Logger, error) {
	return newLogger(service, level, output, rand.Reader)
}
func newLogger(service Service, level slog.Level, output io.Writer, entropy io.Reader) (*Logger, error) {
	if service != Central && service != Runner || output == nil {
		return nil, errors.New("LOG_INITIALIZATION_FAILED")
	}
	var id [16]byte
	if _, err := io.ReadFull(entropy, id[:]); err != nil {
		return nil, errors.New("LOG_INITIALIZATION_FAILED")
	}
	handler := slog.NewJSONHandler(output, &slog.HandlerOptions{Level: level, ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
		if len(groups) == 0 && a.Key == slog.TimeKey {
			a.Value = slog.TimeValue(a.Value.Time().UTC())
		}
		return a
	}})
	logger := slog.New(handler).With("service", string(service), "run_id", hex.EncodeToString(id[:]), "ready", false)
	if service == Runner {
		logger = logger.With("connected", false, "authenticated", false)
	}
	return &Logger{logger: logger}, nil
}

func (l *Logger) Transition(phase Phase) {
	phase = phase.safe()
	if phase == Unconnected {
		l.logger.Info("lifecycle", "event", "lifecycle", "phase", phase, "runner_protocol", "unbound")
		return
	}
	l.logger.Info("lifecycle", "event", "lifecycle", "phase", phase)
}
func (l *Logger) Listening(address netip.AddrPort) {
	l.logger.Info("listening", "event", "listening", "phase", DiagnosticServing, "listen_address", address.String())
}
func (l *Logger) Failed(phase Phase, code lifecycle.FailureCode) {
	l.logger.Error("process_failed", "event", "process_failed", "phase", phase.safe(), "code", code.Safe())
}
func (l *Logger) ShutdownComplete(forced bool, code lifecycle.FailureCode) {
	if forced {
		l.logger.Error("shutdown_complete", "event", "shutdown_complete", "phase", Stopped, "outcome", "forced", "code", code.Safe())
		return
	}
	l.logger.Info("shutdown_complete", "event", "shutdown_complete", "phase", Stopped, "outcome", "drained")
}
func (l *Logger) CLIRejected() {
	l.logger.Error("cli_rejected", "event", "cli_rejected", "code", "INVALID_ARGUMENT")
}
func (l *Logger) InvalidConfig(field, reason string) {
	switch field {
	case "AGENTEAM_CENTRAL_*", "AGENTEAM_CENTRAL_LOG_LEVEL", "AGENTEAM_CENTRAL_SHUTDOWN_TIMEOUT", "AGENTEAM_CENTRAL_HTTP_ADDR", "AGENTEAM_CENTRAL_PUBLIC_ORIGIN", "AGENTEAM_CENTRAL_CURSOR_KEYRING", "AGENTEAM_RUNNER_*", "AGENTEAM_RUNNER_LOG_LEVEL", "AGENTEAM_RUNNER_SHUTDOWN_TIMEOUT",
		"PG*", "AGENTEAM_CENTRAL_DATABASE_*", "AGENTEAM_CENTRAL_DATABASE_URL", "AGENTEAM_CENTRAL_DATABASE_TLS_MODE", "AGENTEAM_CENTRAL_DATABASE_CA_FILE", "AGENTEAM_CENTRAL_DATABASE_MAX_CONNS", "AGENTEAM_CENTRAL_DATABASE_CONNECT_TIMEOUT", "AGENTEAM_CENTRAL_DATABASE_STARTUP_TIMEOUT", "AGENTEAM_CENTRAL_DATABASE_LOCK_TIMEOUT":
	default:
		field = "unknown_field"
	}
	switch reason {
	case "invalid", "unsupported", "unreadable":
	default:
		reason = "invalid"
	}
	l.logger.Error("configuration_rejected", "event", "configuration_rejected", "code", "INVALID_CONFIGURATION", "field", field, "reason", reason)
}

// HTTPLogger is restricted to the audited httpapi boundary, which supplies only
// its explicit request ID, method, route, status, duration, byte count and code.
func (l *Logger) HTTPLogger() *slog.Logger { return l.logger }

// ServerErrorLog discards net/http's free-form text; it may contain a request
// value or stack. The process still records the occurrence with a stable code.
func (l *Logger) ServerErrorLog() *log.Logger { return log.New(serverErrorWriter{l}, "", 0) }

type serverErrorWriter struct{ logger *Logger }

func (w serverErrorWriter) Write(b []byte) (int, error) {
	w.logger.logger.Error("http_server_error", "event", "http_server_error", "code", "HTTP_SERVER_ERROR")
	return len(b), nil
}
