package app

import (
	"context"
	"io"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/LunaDeerTech/agenteam/internal/platform/logging"
)

type b04HealthComponent func(context.Context) error

func (f b04HealthComponent) Check(ctx context.Context) error { return f(ctx) }

func TestB04HealthSamplingWaitsForEachActualReturn(t *testing.T) {
	for slow, name := range []string{"database", "object", "outbox", "account"} {
		t.Run(name, func(t *testing.T) {
			firstEntered, firstExpired, firstReturned := make(chan struct{}), make(chan struct{}), make(chan struct{})
			firstRelease, freshRelease := make(chan struct{}), make(chan struct{})
			freshEntered, freshReturned := make(chan struct{}), make(chan struct{})
			var releaseFirst, releaseFresh sync.Once
			var calls atomic.Int32
			progress := make(chan int32, 128)
			var rounds atomic.Int32
			check := func(index int) b04HealthComponent {
				return func(ctx context.Context) error {
					if index != slow {
						if index == (slow+1)%4 {
							select {
							case progress <- rounds.Add(1):
							default:
							}
						}
						return nil
					}
					switch calls.Add(1) {
					case 1:
						defer close(firstReturned)
						close(firstEntered)
						<-ctx.Done()
						close(firstExpired)
						<-firstRelease
						return nil // A misleading late success must be discarded.
					case 2:
						defer close(freshReturned)
						close(freshEntered)
						<-freshRelease
						return nil
					default:
						return nil
					}
				}
			}
			dbCheck := check(0)
			db := &unitDatabase{check: func(ctx context.Context) (postgres.DatabaseHealth, error) { return unitHealth(), dbCheck(ctx) }}
			h := newHealthMonitor(unitHealth(), healthTiming{interval: time.Millisecond, timeout: 20 * time.Millisecond})
			logger, err := logging.New(logging.Central, slog.LevelInfo, io.Discard)
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			done := make(chan struct{})
			go func() { defer close(done); h.run(ctx, db, logger, check(1), check(2), check(3)) }()
			t.Cleanup(func() {
				cancel()
				releaseFirst.Do(func() { close(firstRelease) })
				releaseFresh.Do(func() { close(freshRelease) })
				await(t, done)
				if calls.Load() >= 1 {
					await(t, firstReturned)
				}
				if calls.Load() >= 2 {
					await(t, freshReturned)
				}
			})
			await(t, firstEntered)
			await(t, firstExpired)
			deadline := time.NewTimer(time.Second)
			defer deadline.Stop()
			for next := int32(0); next < 3; {
				select {
				case next = <-progress:
				case <-deadline.C:
					t.Fatal("an unjoined dependency stopped independent health sampling")
				}
			}
			if calls.Load() != 1 {
				t.Fatal("a timed-out Check was reentered before its actual return")
			}
			available := func() bool {
				switch slow {
				case 0:
					_, ok := h.snapshot()
					return ok
				case 1:
					return h.objectSnapshot()
				case 2:
					return h.outboxSnapshot()
				default:
					return h.accountSnapshot()
				}
			}
			if available() {
				t.Fatal("timed-out unjoined dependency remained healthy")
			}
			releaseFirst.Do(func() { close(firstRelease) })
			await(t, firstReturned)
			await(t, freshEntered)
			if available() {
				t.Fatal("late success was published before the fresh Check returned")
			}
			releaseFresh.Do(func() { close(freshRelease) })
			await(t, freshReturned)
			recovered := time.NewTimer(time.Second)
			defer recovered.Stop()
			tick := time.NewTicker(time.Millisecond)
			defer tick.Stop()
			for !available() {
				select {
				case <-tick.C:
				case <-recovered.C:
					t.Fatal("fresh completed Check did not restore health")
				}
			}
		})
	}
}

func TestB04HealthSamplingStopKeepsActualChecksSeparate(t *testing.T) {
	var entered, expired, returned [4]chan struct{}
	var calls [4]atomic.Int32
	release := make(chan struct{})
	var releaseOnce sync.Once
	for i := range 4 {
		entered[i], expired[i], returned[i] = make(chan struct{}), make(chan struct{}), make(chan struct{})
	}
	check := func(index int) b04HealthComponent {
		return func(ctx context.Context) error {
			if calls[index].Add(1) != 1 {
				return nil
			}
			defer close(returned[index])
			close(entered[index])
			<-ctx.Done()
			close(expired[index])
			<-release
			return nil
		}
	}
	dbCheck := check(0)
	db := &unitDatabase{check: func(ctx context.Context) (postgres.DatabaseHealth, error) { return unitHealth(), dbCheck(ctx) }}
	h := newHealthMonitor(unitHealth(), healthTiming{interval: time.Millisecond, timeout: 20 * time.Millisecond})
	logger, err := logging.New(logging.Central, slog.LevelInfo, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); h.run(ctx, db, logger, check(1), check(2), check(3)) }()
	t.Cleanup(func() {
		cancel()
		releaseOnce.Do(func() { close(release) })
		await(t, done)
		for i := range 4 {
			await(t, returned[i])
		}
	})
	for i := range 4 {
		await(t, entered[i])
	}
	cancel()
	await(t, done)
	for i := range 4 {
		await(t, expired[i])
		if calls[i].Load() != 1 {
			t.Fatal("stopped sampler started an additional Check")
		}
		select {
		case <-returned[i]:
			t.Fatal("context cancellation was confused with actual Check return")
		default:
		}
	}
	releaseOnce.Do(func() { close(release) })
	for i := range 4 {
		await(t, returned[i])
	}
}
