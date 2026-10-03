package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/LunaDeerTech/agenteam/internal/central/app"
	"github.com/LunaDeerTech/agenteam/internal/central/config"
	"github.com/LunaDeerTech/agenteam/internal/platform/logging"
)

func main() {
	signals := make(chan os.Signal, 2)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)
	code := execute(os.Args[1:], os.LookupEnv, os.Environ(), os.Stdout, os.Stderr, signals)
	signal.Stop(signals)
	os.Exit(code)
}

func execute(args []string, lookup config.LookupEnv, env []string, stdout, stderr io.Writer, signals <-chan os.Signal) int {
	action := "run"
	if len(args) == 1 {
		switch args[0] {
		case "--help":
			action = "help"
		case "--version":
			action = "version"
		case "--check-config":
			action = "check"
		default:
			action = "invalid"
		}
	} else if len(args) > 1 {
		action = "invalid"
	}
	if action == "help" {
		_, _ = fmt.Fprintln(stdout, "Usage: agenteam [--help | --version | --check-config]\nD02 process foundation only; product ready=false.")
		return 0
	}
	if action == "version" {
		_, _ = fmt.Fprintln(stdout, "agenteam development (D02)")
		return 0
	}
	if action == "invalid" {
		logger, err := logging.New(logging.Central, slog.LevelInfo, stderr)
		if err != nil {
			return loggingFailure(stderr)
		}
		logger.CLIRejected()
		return 2
	}
	cfg, err := config.Load(lookup, env)
	if err != nil {
		logger, logErr := logging.New(logging.Central, slog.LevelInfo, stderr)
		if logErr != nil {
			return loggingFailure(stderr)
		}
		if issue, ok := err.(*config.Error); ok {
			logger.InvalidConfig(issue.Field(), issue.Reason())
		} else {
			logger.InvalidConfig("unknown_field", "invalid")
		}
		return 2
	}
	logger, err := logging.New(logging.Central, cfg.LogLevel(), stderr)
	if err != nil {
		return loggingFailure(stderr)
	}
	if action == "check" {
		result := struct {
			Scope string `json:"scope"`
			Valid bool   `json:"valid"`
			Ready bool   `json:"ready"`
		}{Scope: "d02", Valid: true}
		if err := json.NewEncoder(stdout).Encode(result); err != nil {
			return 1
		}
		return 0
	}
	if err := app.Run(context.Background(), cfg, logger, signals); err != nil {
		return 1
	}
	return 0
}

func loggingFailure(stderr io.Writer) int {
	// Random identity failure cannot yield a normal identity-bearing log record.
	// Emit only a fixed bootstrap diagnostic, never the underlying error.
	_, _ = fmt.Fprintln(stderr, `{"event":"logging_failed","service":"central","code":"LOG_INITIALIZATION_FAILED"}`)
	return 1
}
