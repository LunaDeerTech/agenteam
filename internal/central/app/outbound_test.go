package app

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/audit"
	"github.com/LunaDeerTech/agenteam/internal/central/config"
	"github.com/LunaDeerTech/agenteam/internal/central/outbound"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/LunaDeerTech/agenteam/internal/platform/lifecycle"
	"github.com/LunaDeerTech/agenteam/internal/platform/logging"
)

type unitEgress struct {
	stopped atomic.Bool
	drain   func(context.Context) error
	force   func(context.Context) error
}

func (e *unitEgress) StopAdmission() { e.stopped.Store(true) }
func (e *unitEgress) Status() outbound.PolicyStatus {
	return outbound.PolicyStatus{Available: !e.stopped.Load()}
}
func (e *unitEgress) Drain(ctx context.Context) error {
	if e.drain != nil {
		return e.drain(ctx)
	}
	return nil
}
func (e *unitEgress) ForceClose(ctx context.Context) error {
	e.StopAdmission()
	if e.force != nil {
		return e.force(ctx)
	}
	return nil
}

func TestOutboundHTTPMaintenanceDrainAllPrecedeDatabase(t *testing.T) {
	httpEntered, httpRelease := make(chan struct{}), make(chan struct{})
	workerEntered, workerRelease := make(chan struct{}), make(chan struct{})
	outboundEntered, outboundRelease := make(chan context.Context, 1), make(chan struct{})
	db := &unitDatabase{}
	worker := &unitMaintenance{run: func(context.Context) error { close(workerEntered); <-workerRelease; return nil }}
	transport := &unitEgress{drain: func(ctx context.Context) error {
		outboundEntered <- ctx
		select {
		case <-outboundRelease:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}}
	var onceHTTP, onceWorker, onceOutbound sync.Once
	finishHTTP := func() { onceHTTP.Do(func() { close(httpRelease) }) }
	defer finishHTTP()
	finishWorker := func() { onceWorker.Do(func() { close(workerRelease) }) }
	defer finishWorker()
	finishOutbound := func() { onceOutbound.Do(func() { close(outboundRelease) }) }
	defer finishOutbound()
	var deadline time.Time
	db.drain = func(ctx context.Context) error {
		got, _ := ctx.Deadline()
		if !got.Equal(deadline) {
			t.Error("DB drain received new budget")
		}
		return nil
	}
	app := startApp(t, "2s", dependencies{open: func(context.Context, postgres.Config) (database, error) { return db, nil }, secret: func(context.Context, config.Config, database, *audit.Service) (maintenance, error) {
		return worker, nil
	}, outbound: func(context.Context, config.Config, database, *audit.Service) (egress, error) { return transport, nil }, handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { close(httpEntered); <-httpRelease; w.WriteHeader(200) })})
	await(t, workerEntered)
	client := &http.Client{Timeout: 3 * time.Second}
	defer client.CloseIdleConnections()
	response := asyncGet(client, app.url)
	await(t, httpEntered)
	app.signals <- syscall.SIGTERM
	select {
	case ctx := <-outboundEntered:
		deadline, _ = ctx.Deadline()
	case <-time.After(time.Second):
		t.Fatal("outbound drain not started")
	}
	if !transport.stopped.Load() || db.stopped.Load() {
		t.Fatal("admission ordering")
	}
	finishHTTP()
	if got := receiveResponse(t, response); got.err != nil || got.status != 200 {
		t.Fatal(got.err)
	}
	if db.stopped.Load() {
		t.Fatal("DB stopped before maintenance/outbound")
	}
	finishWorker()
	if db.stopped.Load() {
		t.Fatal("DB stopped before outbound")
	}
	finishOutbound()
	await(t, app.done)
	if app.err != nil || !db.stopped.Load() {
		t.Fatal(app.err)
	}
}

func TestOutboundAndDatabaseForceShareOneBudget(t *testing.T) {
	entered := make(chan context.Context, 2)
	block := func(ctx context.Context) error { entered <- ctx; <-ctx.Done(); return ctx.Err() }
	db := &unitDatabase{force: block}
	transport := &unitEgress{drain: func(ctx context.Context) error { <-ctx.Done(); return ctx.Err() }, force: block}
	app := startApp(t, "5s", dependencies{open: func(context.Context, postgres.Config) (database, error) { return db, nil }, outbound: func(context.Context, config.Config, database, *audit.Service) (egress, error) { return transport, nil }})
	start := time.Now()
	app.signals <- syscall.SIGTERM
	app.log.wait(t, func(e map[string]any) bool { return e["phase"] == "stopping" })
	app.signals <- syscall.SIGINT
	first, second := <-entered, <-entered
	d1, _ := first.Deadline()
	d2, _ := second.Deadline()
	if !d1.Equal(d2) {
		t.Fatal("force budget reset")
	}
	await(t, app.done)
	if time.Since(start) > 1500*time.Millisecond || lifecycle.CodeOf(app.err) != lifecycle.ForcedShutdown {
		t.Fatal("force time/outcome", app.err)
	}
}

func TestOutboundStartupFailureAndLateResourceDisposed(t *testing.T) {
	for _, mode := range []string{"failure", "first", "second_late"} {
		t.Run(mode, func(t *testing.T) {
			entered := make(chan context.Context, 1)
			release := make(chan struct{})
			defer close(release)
			lateClosed := make(chan context.Context, 1)
			transport := &unitEgress{force: func(ctx context.Context) error { lateClosed <- ctx; return nil }}
			deps := unitDependencies(dependencies{outbound: func(ctx context.Context, _ config.Config, _ database, _ *audit.Service) (egress, error) {
				deadline, _ := ctx.Deadline()
				if left := time.Until(deadline); left > SecurityStartupTimeout || left < 29*time.Second {
					t.Error("outbound not in shared security budget")
				}
				entered <- ctx
				switch mode {
				case "failure":
					return nil, errors.New("outbound-private-sentinel")
				case "first":
					<-ctx.Done()
					return transport, nil
				default:
					<-release
					return transport, nil
				}
			}, listen: func(context.Context, string, string) (net.Listener, error) {
				t.Error("outbound startup failure bound HTTP")
				return nil, errors.New("unexpected")
			}})
			log := newEventLog()
			logger, _ := logging.New(logging.Central, slog.LevelInfo, io.MultiWriter(log, io.Discard))
			signals := make(chan os.Signal, 2)
			done := make(chan error, 1)
			go func() { done <- run(context.Background(), testConfig(t, "5s"), logger, signals, deps) }()
			ctx := <-entered
			if mode == "failure" {
				if err := <-done; err == nil {
					t.Fatal("failure ignored")
				}
				return
			}
			signals <- syscall.SIGTERM
			<-ctx.Done()
			if mode == "second_late" {
				signals <- syscall.SIGINT
				if err := <-done; lifecycle.CodeOf(err) != lifecycle.ForcedShutdown {
					t.Fatal(err)
				}
				release <- struct{}{}
			}
			select {
			case closed := <-lateClosed:
				if closed.Err() == nil {
					t.Fatal("late resource given renewed budget")
				}
			case <-time.After(2 * time.Second):
				t.Fatal("late outbound leaked")
			}
			if !transport.stopped.Load() {
				t.Fatal("late outbound accepted requests")
			}
			if mode == "first" {
				if err := <-done; err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}
