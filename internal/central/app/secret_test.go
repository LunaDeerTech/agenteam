package app

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/audit"
	"github.com/LunaDeerTech/agenteam/internal/central/config"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/LunaDeerTech/agenteam/internal/central/secret"
)

type unitMaintenance struct {
	run    func(context.Context) error
	stop   func()
	status func() secret.Status
}

func (s *unitMaintenance) RunMaintenance(ctx context.Context) error { return s.run(ctx) }
func (s *unitMaintenance) StopMaintenance() {
	if s.stop != nil {
		s.stop()
	}
}
func (s *unitMaintenance) Status() secret.Status {
	if s.status != nil {
		return s.status()
	}
	return secret.Status{Available: true, Rotation: "running"}
}
func TestSecretWorkerAndExistingHTTPDrainBeforeDatabase(t *testing.T) {
	db := &unitDatabase{}
	workerEntered, workerStopped, workerRelease := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var once sync.Once
	worker := &unitMaintenance{run: func(ctx context.Context) error {
		close(workerEntered)
		select {
		case <-workerRelease:
			if ctx.Err() != nil || db.stopped.Load() {
				t.Error("current batch lost serving context or database before drain")
			}
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}, stop: func() { once.Do(func() { close(workerStopped) }) }}
	httpEntered, httpRelease := make(chan struct{}), make(chan struct{})
	app := startApp(t, "1s", dependencies{open: func(context.Context, postgres.Config) (database, error) { return db, nil }, secret: func(context.Context, config.Config, database, *audit.Service) (maintenance, error) {
		return worker, nil
	}, handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(httpEntered)
		<-httpRelease
		if r.Context().Err() != nil || db.stopped.Load() {
			t.Error("existing HTTP cancelled before its transaction can finish")
		}
		w.WriteHeader(204)
	})})
	await(t, workerEntered)
	client := &http.Client{Timeout: time.Second}
	defer client.CloseIdleConnections()
	response := asyncGet(client, app.url)
	await(t, httpEntered)
	app.signals <- syscall.SIGTERM
	await(t, workerStopped)
	close(httpRelease)
	got := receiveResponse(t, response)
	if got.err != nil || got.status != 204 {
		t.Fatal("existing HTTP failed", got.err)
	}
	if db.stopped.Load() {
		t.Fatal("database admission stopped with current batch running")
	}
	close(workerRelease)
	await(t, app.done)
	if app.err != nil || !db.stopped.Load() {
		t.Fatal("drain did not finish", app.err)
	}
}
func TestSecretWorkerForceAndJoinUseSingleTotalBudget(t *testing.T) {
	for _, second := range []bool{false, true} {
		t.Run(map[bool]string{false: "timeout", true: "second_signal"}[second], func(t *testing.T) {
			entered, stopped, release, workerDone := make(chan struct{}), make(chan struct{}), make(chan struct{}), make(chan struct{})
			defer func() { close(release); await(t, workerDone) }()
			var once sync.Once
			var forceCalls atomic.Int32
			worker := &unitMaintenance{run: func(ctx context.Context) error { close(entered); defer close(workerDone); <-release; return ctx.Err() }, stop: func() { once.Do(func() { close(stopped) }) }}
			db := &unitDatabase{force: func(ctx context.Context) error {
				forceCalls.Add(1)
				deadline, ok := ctx.Deadline()
				if !ok || time.Until(deadline) > time.Second {
					t.Error("database force reset outer budget")
				}
				<-ctx.Done()
				return ctx.Err()
			}}
			timeout := "100ms"
			if second {
				timeout = "5s"
			}
			app := startApp(t, timeout, dependencies{open: func(context.Context, postgres.Config) (database, error) { return db, nil }, secret: func(context.Context, config.Config, database, *audit.Service) (maintenance, error) {
				return worker, nil
			}})
			await(t, entered)
			start := time.Now()
			app.signals <- syscall.SIGTERM
			await(t, stopped)
			if second {
				app.signals <- syscall.SIGINT
			}
			await(t, app.done)
			if app.err == nil || forceCalls.Load() != 1 || time.Since(start) > 1500*time.Millisecond {
				t.Fatal("force/worker join expanded shared budget")
			}
		})
	}
}
func TestSecretLateStartupAcquisitionCannotStartWorkerAfterStop(t *testing.T) {
	o := &resources{}
	o.stopMaintenance()
	var stopped, ran atomic.Bool
	s := &unitMaintenance{run: func(context.Context) error { ran.Store(true); return nil }, stop: func() { stopped.Store(true) }}
	if o.startMaintenance(context.Background(), s) || !stopped.Load() || ran.Load() || o.workerDone() != nil {
		t.Fatal("late maintenance resurrected stopped process")
	}
}
func TestSecretWorkerFailureDoesNotClaimReadiness(t *testing.T) {
	var failed atomic.Bool
	worker := &unitMaintenance{run: func(context.Context) error { failed.Store(true); return errors.New("private-worker-error") }, status: func() secret.Status { return secret.Status{Available: !failed.Load(), Rotation: "failed"} }}
	app := startApp(t, "1s", dependencies{secret: func(context.Context, config.Config, database, *audit.Service) (maintenance, error) {
		return worker, nil
	}})
	app.log.wait(t, func(e map[string]any) bool { return e["event"] == "security" && e["phase"] == "secret_unavailable" })
	response := receiveResponse(t, asyncGet(&http.Client{Timeout: time.Second}, app.url+"/readyz"))
	if response.err != nil || response.status != 503 {
		t.Fatal("failed Secret reported ready")
	}
	app.signals <- syscall.SIGTERM
	await(t, app.done)
}
