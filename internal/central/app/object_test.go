package app

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/audit"
	"github.com/LunaDeerTech/agenteam/internal/central/config"
	"github.com/LunaDeerTech/agenteam/internal/platform/logging"
)

// This test isolates the composition deadline; the integration suite exercises
// the real object Runtime, sockets, ProcessGuard and command signals.
func TestObjectsReceiveUnrenewedSecurityDeadlineBeforeListener(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	var securityDeadline time.Time
	var objectEntered, listened atomic.Bool
	deps := unitDependencies(dependencies{
		security: func(ctx context.Context, _ config.Config, _ database) (*audit.Service, error) {
			securityDeadline, _ = ctx.Deadline()
			return nil, nil
		},
		objects: func(ctx context.Context, _ config.Config, _ database, _ *audit.Service) (objectStorage, error) {
			objectEntered.Store(true)
			deadline, ok := ctx.Deadline()
			if !ok || !deadline.Equal(securityDeadline) {
				t.Error("object phase renewed startup budget")
			}
			<-ctx.Done()
			return nil, ctx.Err()
		},
		listen: func(context.Context, string, string) (net.Listener, error) {
			listened.Store(true)
			return nil, errors.New("unexpected listener")
		},
	})
	logger, _ := logging.New(logging.Central, slog.LevelInfo, io.Discard)
	start := time.Now()
	_ = run(ctx, testConfig(t, "1s"), logger, nil, deps)
	if !objectEntered.Load() || listened.Load() || time.Since(start) > time.Second {
		t.Fatal("object deadline did not fence listener/startup")
	}
}

type lateObjectResources struct {
	stopped bool
	force   context.Context
}

func (*lateObjectResources) StartMaintenance(context.Context) error { return nil }
func (*lateObjectResources) Check(context.Context) error            { return nil }
func (o *lateObjectResources) StopAdmission()                       { o.stopped = true }
func (*lateObjectResources) Drain(context.Context) error            { return nil }
func (o *lateObjectResources) Force(ctx context.Context) error      { o.force = ctx; return ctx.Err() }

func TestLateObjectsAreRejectedAndForcedWithOriginalExpiredBudget(t *testing.T) {
	deadline := time.Now().Add(-time.Second)
	ctx, cancel := context.WithDeadline(context.Background(), deadline)
	defer cancel()
	owned := &resources{forced: ctx, stopping: true}
	late := &lateObjectResources{}
	if owned.addObjects(context.Background(), late) || !late.stopped || late.force == nil {
		t.Fatal("late object resource escaped cleanup")
	}
	actual, ok := late.force.Deadline()
	if !ok || !actual.Equal(deadline) || late.force.Err() == nil || owned.objects() != nil {
		t.Fatal("late object cleanup renewed/accepted resource")
	}
}

func TestCancelledObjectsRemainOwnedByOriginalShutdownGroup(t *testing.T) {
	for _, stopping := range []bool{false, true} {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		owned := &resources{stopping: stopping}
		late := &lateObjectResources{}
		if owned.addObjects(ctx, late) || !late.stopped || late.force != nil || owned.objects() != late {
			t.Fatal("cancelled startup resource escaped shutdown ownership")
		}
		forced, finish := context.WithTimeout(context.Background(), time.Second)
		cleanupResources(forced, owned, nil)
		if late.force != forced {
			t.Fatal("shutdown lost or renewed the cleanup-only resource budget")
		}
		finish()
	}
}

func TestObjectHealthAgeDoesNotRefreshDatabaseSample(t *testing.T) {
	now := time.Now()
	monitor := newHealthMonitor(unitHealth(), healthTiming{now: func() time.Time { return now }})
	monitor.objectAvailable = true
	monitor.objectReceived = now
	now = now.Add(19 * time.Second)
	if !monitor.objectSnapshot() {
		t.Fatal("fresh object sample rejected")
	}
	monitor.objectReceived = now // A real new object result does not refresh DB.
	now = now.Add(2 * time.Second)
	if _, ok := monitor.snapshot(); ok {
		t.Fatal("object sample refreshed stale DB")
	}
	if !monitor.objectSnapshot() {
		t.Fatal("new object sample expired too soon")
	}
	now = now.Add(19 * time.Second)
	if monitor.objectSnapshot() {
		t.Fatal("20s object freshness not enforced")
	}
}
