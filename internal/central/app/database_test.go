package app

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"os"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/audit"
	"github.com/LunaDeerTech/agenteam/internal/central/config"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/LunaDeerTech/agenteam/internal/platform/lifecycle"
	"github.com/LunaDeerTech/agenteam/internal/platform/logging"
)

// These collaborators exercise orchestration without Docker. Real database
// assembly, signals and cleanup are separately exercised by integration tests.
type unitDatabase struct {
	check   func(context.Context) (postgres.DatabaseHealth, error)
	drain   func(context.Context) error
	force   func(context.Context) error
	stopped atomic.Bool
}

func unitHealth() postgres.DatabaseHealth {
	instant, _ := foundation.NewInstant(time.Now())
	return postgres.DatabaseHealth{PostgreSQL: true, PGVector: true, Migrations: true, ReadWrite: true, CheckedAt: instant, ServerVersion: "17.8", ExtensionVersion: "0.8.1"}
}
func (d *unitDatabase) Check(ctx context.Context) (postgres.DatabaseHealth, error) {
	if d.check != nil {
		return d.check(ctx)
	}
	return unitHealth(), nil
}
func (d *unitDatabase) StopAdmission() { d.stopped.Store(true) }
func (d *unitDatabase) Drain(ctx context.Context) error {
	d.StopAdmission()
	if d.drain != nil {
		return d.drain(ctx)
	}
	return nil
}
func (d *unitDatabase) ForceClose(ctx context.Context) error {
	d.StopAdmission()
	if d.force != nil {
		return d.force(ctx)
	}
	return nil
}
func unitDependencies(deps dependencies) dependencies {
	if deps.secret == nil {
		deps.secret = func(context.Context, config.Config, database, *audit.Service) (maintenance, error) { return nil, nil }
	}
	if deps.security == nil {
		deps.security = func(context.Context, config.Config, database) (*audit.Service, error) { return nil, nil }
	}
	if deps.open == nil {
		deps.open = func(context.Context, postgres.Config) (database, error) { return &unitDatabase{}, nil }
	}
	if deps.migrate == nil {
		deps.migrate = func(context.Context, postgres.Config) postgres.MigrationState {
			return postgres.MigrationState{Migrated: true, Version: 1}
		}
	}
	return deps
}

func TestDatabaseStartupOrderAndFailureClosesAcquisitions(t *testing.T) {
	for _, failure := range []string{"open", "migrate", "check"} {
		t.Run(failure, func(t *testing.T) {
			var opened, migrated, checked, closed atomic.Bool
			raw := errors.New("private-database-sentinel")
			db := &unitDatabase{check: func(context.Context) (postgres.DatabaseHealth, error) {
				checked.Store(true)
				if !migrated.Load() {
					t.Error("check before migrate")
				}
				return postgres.DatabaseHealth{}, raw
			}, force: func(context.Context) error { closed.Store(true); return nil }}
			deps := dependencies{open: func(context.Context, postgres.Config) (database, error) {
				opened.Store(true)
				if failure == "open" {
					return nil, raw
				}
				return db, nil
			}, migrate: func(context.Context, postgres.Config) postgres.MigrationState {
				if !opened.Load() {
					t.Error("migrate before open")
				}
				migrated.Store(true)
				return postgres.MigrationState{Migrated: failure != "migrate", Version: 1}
			}, listen: func(context.Context, string, string) (net.Listener, error) {
				t.Error("HTTP bound before successful database initialization")
				return nil, raw
			}}
			logger, _ := logging.New(logging.Central, slog.LevelInfo, io.Discard)
			err := run(context.Background(), testConfig(t, "1s"), logger, nil, deps)
			if lifecycle.CodeOf(err) != lifecycle.InitializationFailed || failure != "open" && !closed.Load() {
				t.Fatal("failed startup not cleaned")
			}
			if failure != "check" && checked.Load() {
				t.Fatal("continued after migration/open failure")
			}
		})
	}
}

func TestStartupSecondSignalBoundsUncooperativeAcquisitionAndClosesLateStore(t *testing.T) {
	entered, release, closed := make(chan struct{}), make(chan struct{}), make(chan struct{})
	db := &unitDatabase{force: func(ctx context.Context) error {
		if ctx.Err() == nil {
			t.Error("late store got a reset cleanup budget")
		}
		close(closed)
		return nil
	}}
	deps := unitDependencies(dependencies{open: func(context.Context, postgres.Config) (database, error) { close(entered); <-release; return db, nil }})
	signals := make(chan os.Signal, 2)
	done := make(chan error, 1)
	logs := newEventLog()
	logger, _ := logging.New(logging.Central, slog.LevelInfo, logs)
	go func() { done <- run(context.Background(), testConfig(t, "5s"), logger, signals, deps) }()
	await(t, entered)
	signals <- syscall.SIGTERM
	logs.wait(t, func(e map[string]any) bool { return e["phase"] == "stopping" })
	started := time.Now()
	signals <- syscall.SIGINT
	select {
	case err := <-done:
		if lifecycle.CodeOf(err) != lifecycle.ForcedShutdown {
			t.Fatal("startup force outcome invalid")
		}
	case <-time.After(1500 * time.Millisecond):
		t.Fatal("startup second signal exceeded force budget")
	}
	if time.Since(started) > 1500*time.Millisecond {
		t.Fatal("startup budget reset")
	}
	close(release)
	await(t, closed)
}

func TestHealthSnapshotsExpireRecoverAndRejectLateSuccess(t *testing.T) {
	now := time.Now()
	var offset atomic.Int64
	h := newHealthMonitor(unitHealth(), healthTiming{now: func() time.Time { return now.Add(time.Duration(offset.Load())) }})
	offset.Store(int64(20 * time.Second))
	if _, ok := h.snapshot(); !ok {
		t.Fatal("snapshot expired before boundary")
	}
	offset.Store(int64(20*time.Second + time.Nanosecond))
	if _, ok := h.snapshot(); ok {
		t.Fatal("stale success retained")
	}
	offset.Store(0)
	entered, released := make(chan struct{}, 2), make(chan struct{})
	var calls, active, maximum atomic.Int32
	db := &unitDatabase{check: func(ctx context.Context) (postgres.DatabaseHealth, error) {
		count := active.Add(1)
		if count > maximum.Load() {
			maximum.Store(count)
		}
		defer active.Add(-1)
		call := calls.Add(1)
		if call == 1 {
			entered <- struct{}{}
			<-ctx.Done()
			<-released
			return unitHealth(), nil
		}
		return unitHealth(), nil
	}}
	h.timing.interval = time.Millisecond
	h.timing.timeout = 20 * time.Millisecond
	logs := newEventLog()
	logger, _ := logging.New(logging.Central, slog.LevelInfo, logs)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); h.run(ctx, db, logger) }()
	<-entered
	// The fake waits for the actual deadline before releasing a misleading nil
	// error. The sampler must reject that late success and never overlap checks.
	close(released)
	logs.wait(t, func(e map[string]any) bool { return e["database_phase"] == "unavailable" })
	logs.wait(t, func(e map[string]any) bool { return e["database_phase"] == "healthy" })
	cancel()
	await(t, done)
	if maximum.Load() != 1 {
		t.Fatal("health checks overlapped")
	}
	if _, ok := h.snapshot(); !ok {
		t.Fatal("recovered snapshot missing")
	}
}
