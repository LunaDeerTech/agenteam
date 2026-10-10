// Package config validates the Runner's process and fixed D15 device settings.
package config

import (
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/runner/identity"
	p "github.com/LunaDeerTech/agenteam/internal/runnerprotocol"
)

const Prefix = "AGENTEAM_RUNNER_"

type LookupEnv func(string) (string, bool)
type Config struct {
	logLevel        slog.Level
	shutdownTimeout time.Duration
	identityFile    string
	caFile          string
	device          identity.Configuration
	hasDevice       bool
}

func (c Config) LogLevel() slog.Level                   { return c.logLevel }
func (c Config) ShutdownTimeout() time.Duration         { return c.shutdownTimeout }
func (c Config) IdentityFile() string                   { return c.identityFile }
func (c Config) Device() (identity.Configuration, bool) { return c.device, c.hasDevice }
func (Config) Format(w fmt.State, _ rune)               { _, _ = io.WriteString(w, "runner_configuration") }
func (Config) MarshalJSON() ([]byte, error)             { return []byte(`"runner_configuration"`), nil }
func (Config) LogValue() slog.Value                     { return slog.StringValue("runner_configuration") }

type Error struct{ field, reason string }

func (e *Error) Error() string   { return e.field + ": " + e.reason }
func (e *Error) Field() string   { return e.field }
func (e *Error) Reason() string  { return e.reason }
func invalid(field string) error { return &Error{field: Prefix + field, reason: "invalid"} }

// Load reads only the closed environment settings and performs no filesystem or
// network I/O. OfflineCheck separately verifies files without creating an owner.
func Load(lookup LookupEnv, env []string) (Config, error) {
	for _, entry := range env {
		key, _, _ := strings.Cut(entry, "=")
		if !strings.HasPrefix(key, Prefix) {
			continue
		}
		switch strings.TrimPrefix(key, Prefix) {
		case "LOG_LEVEL", "SHUTDOWN_TIMEOUT", "IDENTITY_FILE", "CENTRAL_URL", "ID", "ROOT_PATH", "CA_FILE":
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
	c.identityFile = value("IDENTITY_FILE", "")
	if pathErr := privatePath("IDENTITY_FILE", c.identityFile); pathErr != nil {
		return Config{}, pathErr
	}
	if ca, present := lookup(Prefix + "CA_FILE"); present {
		if pathErr := privatePath("CA_FILE", ca); pathErr != nil {
			return Config{}, pathErr
		}
		c.caFile = ca
	}
	central, hasCentral := lookup(Prefix + "CENTRAL_URL")
	id, hasID := lookup(Prefix + "ID")
	root, hasRoot := lookup(Prefix + "ROOT_PATH")
	if hasCentral || hasID || hasRoot {
		if !hasCentral || !hasID || !hasRoot {
			return Config{}, invalid("*")
		}
		c.device = identity.Configuration{CentralURL: central, RunnerID: p.ID(id), RootPath: root}
		c.hasDevice = true
		if c.device.Validate() != nil {
			return Config{}, invalid("*")
		}
	}
	return c, c.Validate()
}

func (c Config) Validate() error {
	if c.logLevel != slog.LevelDebug && c.logLevel != slog.LevelInfo && c.logLevel != slog.LevelWarn && c.logLevel != slog.LevelError {
		return invalid("LOG_LEVEL")
	}
	if c.shutdownTimeout < 100*time.Millisecond || c.shutdownTimeout > 5*time.Minute {
		return invalid("SHUTDOWN_TIMEOUT")
	}
	if err := privatePath("IDENTITY_FILE", c.identityFile); err != nil {
		return err
	}
	if c.caFile != "" {
		if err := privatePath("CA_FILE", c.caFile); err != nil {
			return err
		}
	}
	if c.hasDevice && c.device.Validate() != nil {
		return invalid("*")
	}
	return nil
}

func privatePath(field, value string) error {
	if value == "" || len(value) > 4096 || !filepath.IsAbs(value) || filepath.Clean(value) != value || strings.ContainsAny(value, "\x00\\") {
		return invalid(field)
	}
	return nil
}

// OfflineCheck never locks or writes credentials and never authenticates. An
// enrollment may prepare an absent file only after its real parent passed the
// identity owner's permission checks and the full new configuration is valid.
func (c Config) OfflineCheck(enrolling bool) error {
	if err := c.Validate(); err != nil {
		return err
	}
	id, err := identity.ReadOnly(c.identityFile)
	if err != nil {
		if !enrolling || !c.hasDevice || !errors.Is(err, os.ErrNotExist) {
			return invalid("IDENTITY_FILE")
		}
	} else if c.hasDevice && id.Configuration() != c.device {
		return invalid("*")
	}
	_, err = c.Roots()
	return err
}

// Roots appends explicit deployment roots to the platform trust store. It never
// switches off hostname verification or changes the default trust implicitly.
func (c Config) Roots() (*x509.CertPool, error) {
	if c.caFile == "" {
		return nil, nil
	}
	file, err := os.OpenFile(c.caFile, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, invalid("CA_FILE")
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > 1<<20 {
		return nil, invalid("CA_FILE")
	}
	content, err := io.ReadAll(io.LimitReader(file, (1<<20)+1))
	if err != nil || len(content) > 1<<20 {
		return nil, invalid("CA_FILE")
	}
	roots, err := x509.SystemCertPool()
	if err != nil {
		return nil, invalid("CA_FILE")
	}
	if !roots.AppendCertsFromPEM(content) {
		return nil, invalid("CA_FILE")
	}
	return roots, nil
}
