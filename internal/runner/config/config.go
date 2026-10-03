// Package config owns only the Runner's D02 process configuration. Runner device
// identity, trust material, workspaces and connection configuration belong to D15.
package config

import (
	"log/slog"
	"strings"
	"time"
)

const Prefix = "AGENTEAM_RUNNER_"

type LookupEnv func(string) (string, bool)
type Config struct {
	logLevel        slog.Level
	shutdownTimeout time.Duration
}

func (c Config) LogLevel() slog.Level           { return c.logLevel }
func (c Config) ShutdownTimeout() time.Duration { return c.shutdownTimeout }

type Error struct{ field, reason string }

func (e *Error) Error() string   { return e.field + ": " + e.reason }
func (e *Error) Field() string   { return e.field }
func (e *Error) Reason() string  { return e.reason }
func invalid(field string) error { return &Error{field: Prefix + field, reason: "invalid"} }

// Load inspects names to reject unsupported Runner keys, and reads values only
// through the supplied lookup for the two supported process settings.
func Load(lookup LookupEnv, env []string) (Config, error) {
	for _, entry := range env {
		key, _, _ := strings.Cut(entry, "=")
		if !strings.HasPrefix(key, Prefix) {
			continue
		}
		switch strings.TrimPrefix(key, Prefix) {
		case "LOG_LEVEL", "SHUTDOWN_TIMEOUT":
		default:
			return Config{}, &Error{field: Prefix + "*", reason: "unsupported"}
		}
	}
	if lookup == nil {
		return Config{}, &Error{field: Prefix + "*", reason: "unreadable"}
	}
	value := func(key, fallback string) string {
		if v, ok := lookup(Prefix + key); ok {
			return v
		}
		return fallback
	}
	var c Config
	switch value("LOG_LEVEL", "info") {
	case "debug":
		c.logLevel = slog.LevelDebug
	case "info":
		c.logLevel = slog.LevelInfo
	case "warn":
		c.logLevel = slog.LevelWarn
	case "error":
		c.logLevel = slog.LevelError
	default:
		return Config{}, invalid("LOG_LEVEL")
	}
	var err error
	c.shutdownTimeout, err = time.ParseDuration(value("SHUTDOWN_TIMEOUT", "10s"))
	if err != nil || c.shutdownTimeout < 100*time.Millisecond || c.shutdownTimeout > 5*time.Minute {
		return Config{}, invalid("SHUTDOWN_TIMEOUT")
	}
	return c, nil
}

func (c Config) Validate() error {
	if c.logLevel != slog.LevelDebug && c.logLevel != slog.LevelInfo && c.logLevel != slog.LevelWarn && c.logLevel != slog.LevelError {
		return invalid("LOG_LEVEL")
	}
	if c.shutdownTimeout < 100*time.Millisecond || c.shutdownTimeout > 5*time.Minute {
		return invalid("SHUTDOWN_TIMEOUT")
	}
	return nil
}
