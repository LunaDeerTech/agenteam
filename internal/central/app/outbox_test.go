package app

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/audit"
	"github.com/LunaDeerTech/agenteam/internal/central/config"
	"github.com/LunaDeerTech/agenteam/internal/platform/logging"
)

type unitOutbox struct {
	stopped    atomic.Bool
	joined     atomic.Bool
	initialize func(context.Context) error
	drain      func(context.Context) error
	force      func(context.Context) error
	check      func(context.Context) error
}

func (o *unitOutbox) Initialize(ctx context.Context) error {
	if o.initialize != nil {
		return o.initialize(ctx)
	}
	return nil
}
func (o *unitOutbox) Start(context.Context) error { return nil }
func (o *unitOutbox) Check(ctx context.Context) error {
	if o.check != nil {
		return o.check(ctx)
	}
	return nil
}
func (o *unitOutbox) StopClaims() { o.stopped.Store(true) }
func (o *unitOutbox) Drain(ctx context.Context) error {
	if o.drain != nil {
		return o.drain(ctx)
	}
	o.joined.Store(true)
	return nil
}
func (o *unitOutbox) Force(ctx context.Context) error {
	o.StopClaims()
	if o.force != nil {
		return o.force(ctx)
	}
	o.joined.Store(true)
	return nil
}
func (o *unitOutbox) Joined() bool { return o.joined.Load() }

type guardedUnitObjects struct {
	outbox    *unitOutbox
	stopped   atomic.Bool
	released  atomic.Bool
	transport atomic.Bool
	seen      context.Context
}

func (o *guardedUnitObjects) StartMaintenance(context.Context) error { return nil }
func (o *guardedUnitObjects) Check(context.Context) error            { return nil }
func (o *guardedUnitObjects) StopAdmission()                         { o.stopped.Store(true) }
func (o *guardedUnitObjects) Drain(context.Context) error {
	if !o.outbox.Joined() {
		return errors.New("guard released before callback join")
	}
	o.released.Store(true)
	return nil
}
func (o *guardedUnitObjects) Force(ctx context.Context) error {
	o.seen = ctx
	o.released.Store(true)
	return ctx.Err()
}
func (o *guardedUnitObjects) forceTransports(ctx context.Context) error {
	o.seen = ctx
	o.transport.Store(true)
	return ctx.Err()
}
func TestOutboxForcePreservesGuardUntilActualJoinAndClosesDBLast(t *testing.T) {
	for _, joined := range []bool{false, true} {
		name := "still_running"
		if joined {
			name = "joined"
		}
		t.Run(name, func(t *testing.T) {
			deliveries := &unitOutbox{}
			objects := &guardedUnitObjects{outbox: deliveries}
			var forceContext context.Context
			deliveries.force = func(ctx context.Context) error {
				forceContext = ctx
				if joined {
					deliveries.joined.Store(true)
					return nil
				}
				<-ctx.Done()
				return ctx.Err()
			}
			var dbCalled bool
			db := &unitDatabase{force: func(ctx context.Context) error {
				dbCalled = true
				if !deliveries.stopped.Load() || !objects.stopped.Load() {
					t.Error("admission was not stopped")
				}
				if !objects.transport.Load() && !objects.released.Load() {
					t.Error("DB overtook object cleanup initiation")
				}
				a, _ := forceContext.Deadline()
				b, _ := ctx.Deadline()
				if !a.Equal(b) {
					t.Error("DB reset force budget")
				}
				return nil
			}}
			resources := &resources{db: db, objectService: objects, outboxService: deliveries}
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
			defer cancel()
			cleanupResources(ctx, resources, nil)
			if !dbCalled || objects.released.Load() != joined || objects.transport.Load() == joined {
				t.Fatal("force falsely released a live process guard")
			}
			a, _ := ctx.Deadline()
			b, _ := objects.seen.Deadline()
			if !a.Equal(b) {
				t.Fatal("object force reset deadline")
			}
		})
	}
}
func TestOutboxOwnerRegisteredBeforeInitializationAndLateForce(t *testing.T) {
	entered := make(chan struct{})
	finish := make(chan struct{})
	var once sync.Once
	deliveries := &unitOutbox{initialize: func(ctx context.Context) error { once.Do(func() { close(entered) }); <-finish; return ctx.Err() }}
	owned := &resources{}
	ctx, cancel := context.WithCancel(context.Background())
	if !owned.addOutbox(ctx, deliveries) || owned.outbox() != deliveries {
		t.Fatal("outbox not owned before initialize")
	}
	done := make(chan struct{})
	go func() { _ = deliveries.Initialize(ctx); close(done) }()
	<-entered
	cancel()
	owned.stopMaintenance()
	owned.stopOutbox()
	if !deliveries.stopped.Load() {
		t.Fatal("initializing owner missed first stop")
	}
	close(finish)
	<-done
	expired, end := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer end()
	owned.forced = expired
	late := &unitOutbox{}
	var got context.Context
	late.force = func(ctx context.Context) error { got = ctx; return ctx.Err() }
	if owned.addOutbox(context.Background(), late) || !late.stopped.Load() || got == nil || got.Err() == nil || owned.outbox() != deliveries {
		t.Fatal("late outbox escaped original cleanup ownership")
	}
	a, _ := got.Deadline()
	b, _ := expired.Deadline()
	if !a.Equal(b) {
		t.Fatal("late owner renewed force budget")
	}
}
func TestOutboxSharesStartupBudgetAndFailurePreventsListener(t *testing.T) {
	entered := make(chan context.Context, 1)
	deliveries := &unitOutbox{initialize: func(ctx context.Context) error { entered <- ctx; return errors.New("private-outbox-sentinel") }}
	listened := false
	deps := unitDependencies(dependencies{outbox: func(config.Config, database, *audit.Service, objectStorage) (outboxStorage, error) {
		return deliveries, nil
	}})
	// initialize consumes the same earlier security deadline; this test calls its
	// package-local assembly, while process tests cover the real CLI composition.
	deps.health = deps.health.defaults()
	deps.listen = func(context.Context, string, string) (net.Listener, error) {
		listened = true
		return nil, errors.New("unexpected listen")
	}
	cfg := testConfig(t, "1s")
	parent, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	owned := &resources{}
	logger, _ := logging.New(logging.Central, slog.LevelInfo, io.Discard)
	result := initialize(parent, cfg, logger, deps, owned)
	sampled := <-entered
	deadline, _ := sampled.Deadline()
	expected, _ := parent.Deadline()
	if !deadline.Equal(expected) || !result.securityFailure || result.err == nil || listened || owned.outbox() != deliveries {
		t.Fatal("outbox initialization renewed budget or bound HTTP")
	}
	cleanup, cancelCleanup := context.WithTimeout(context.Background(), time.Second)
	defer cancelCleanup()
	cleanupResources(cleanup, owned, nil)
}
func TestOutboxHealthFreshnessIsIndependent(t *testing.T) {
	now := time.Now()
	monitor := newHealthMonitor(unitHealth(), healthTiming{now: func() time.Time { return now }})
	monitor.outboxAvailable = true
	monitor.outboxReceived = now
	monitor.objectAvailable = true
	monitor.objectReceived = now
	now = now.Add(21 * time.Second)
	monitor.objectReceived = now
	if monitor.outboxSnapshot() {
		t.Fatal("object freshness refreshed outbox")
	}
	monitor.outboxReceived = now
	if !monitor.outboxSnapshot() {
		t.Fatal("fresh successful outbox rejected")
	}
	monitor.outboxAvailable = false
	if monitor.outboxSnapshot() {
		t.Fatal("failed outbox remained available")
	}
}
