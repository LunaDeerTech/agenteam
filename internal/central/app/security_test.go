package app

import (
	"context"
	"errors"
	"github.com/LunaDeerTech/agenteam/internal/central/audit"
	"github.com/LunaDeerTech/agenteam/internal/central/config"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/LunaDeerTech/agenteam/internal/platform/logging"
	"io"
	"log/slog"
	"net"
	"os"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

func TestSecurityInitializationSeparateBudgetAndBeforeListener(t *testing.T) {
	cfg := testConfig(t, "1s")
	var checked atomic.Bool
	var listened atomic.Bool
	log := newEventLog()
	logger, _ := logging.New(logging.Central, slog.LevelInfo, log)
	db := &unitDatabase{check: func(ctx context.Context) (postgres.DatabaseHealth, error) {
		deadline, _ := ctx.Deadline()
		if time.Until(deadline) > cfg.Database().StartupTimeout() {
			t.Error("DB budget expanded")
		}
		checked.Store(true)
		return unitHealth(), nil
	}}
	deps := unitDependencies(dependencies{open: func(context.Context, postgres.Config) (database, error) { return db, nil }, listen: func(context.Context, string, string) (net.Listener, error) {
		listened.Store(true)
		return nil, errors.New("unexpected listen")
	}, security: func(ctx context.Context, c config.Config, d database) (*audit.Service, error) {
		if !checked.Load() {
			t.Error("security before DB check")
		}
		deadline, ok := ctx.Deadline()
		remaining := time.Until(deadline)
		if !ok || remaining < 29*time.Second || remaining > SecurityStartupTimeout {
			t.Error("security does not own its fixed separate budget")
		}
		return nil, errors.New("security-private-canary")
	}})
	if err := run(context.Background(), cfg, logger, nil, deps); err == nil || listened.Load() {
		t.Fatal("security failure reached listener")
	}
	if strings.Contains(log.String(), "security-private-canary") || !strings.Contains(log.String(), `"phase":"failed"`) {
		t.Fatal("security failure was unsafe or not recorded")
	}
}
func TestSecurityInitializationFirstAndSecondSignal(t *testing.T) {
	for _, second := range []bool{false, true} {
		t.Run(map[bool]string{false: "cancel", true: "force"}[second], func(t *testing.T) {
			entered, release := make(chan context.Context, 1), make(chan struct{})
			defer close(release)
			deps := unitDependencies(dependencies{security: func(ctx context.Context, _ config.Config, _ database) (*audit.Service, error) {
				entered <- ctx
				if second {
					<-release
				} else {
					<-ctx.Done()
				}
				return nil, ctx.Err()
			}, listen: func(context.Context, string, string) (net.Listener, error) {
				t.Error("startup signal reached listener")
				return nil, errors.New("unexpected")
			}})
			signals := make(chan os.Signal, 2)
			logger, _ := logging.New(logging.Central, slog.LevelInfo, io.Discard)
			done := make(chan error, 1)
			go func() { done <- run(context.Background(), testConfig(t, "5s"), logger, signals, deps) }()
			var startup context.Context
			select {
			case startup = <-entered:
			case <-time.After(time.Second):
				t.Fatal("security not entered")
			}
			signals <- syscall.SIGTERM
			select {
			case <-startup.Done():
			case <-time.After(time.Second):
				t.Fatal("first signal did not cancel security")
			}
			if second {
				signals <- syscall.SIGINT
			}
			select {
			case err := <-done:
				if (err != nil) != second {
					t.Fatal("startup shutdown outcome wrong")
				}
			case <-time.After(1500 * time.Millisecond):
				t.Fatal("second signal exceeded single force budget")
			}
		})
	}
}
