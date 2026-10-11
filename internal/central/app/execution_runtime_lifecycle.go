package app

import (
	"context"
	"errors"
	"sync"

	"github.com/LunaDeerTech/agenteam/internal/central/execution"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/scheduler"
)

type executionRuntimeWork struct {
	stop   func()
	drain  func(context.Context) error
	joined func() bool
}

type executionRuntimeRun struct {
	done   chan struct{}
	cancel context.CancelFunc
	err    error // published by closing done
}

// Owners are recorded in dependency order. Shutdown cancels admission first,
// then drains in reverse order: Manager/Executor, the original drivers and
// Scheduler calls, Loop, Model, and finally its wire budget. The root retains
// Account/Secret/Usage/Outbox/Store and ProcessGuard until this bundle joins.
type executionRuntimeAssembly struct {
	mu                             sync.Mutex
	constructing, started, stopped bool
	owners                         []executionRuntimeWork
	runs                           []*executionRuntimeRun
	initialize                     func(context.Context) error
	executor                       *execution.AssociatedExecutor
	manager                        *scheduler.ProjectRunners
}

func (b *executionRuntimeAssembly) add(owner executionRuntimeWork) {
	b.mu.Lock()
	b.owners = append(b.owners, owner)
	stopped := b.stopped
	b.mu.Unlock()
	if stopped {
		owner.stop()
	}
}

func (b *executionRuntimeAssembly) constructionDone() {
	b.mu.Lock()
	b.constructing = false
	b.mu.Unlock()
}

func (b *executionRuntimeAssembly) startRun(run func(context.Context) error) *executionRuntimeRun {
	b.mu.Lock()
	if b.stopped {
		b.mu.Unlock()
		return nil
	}
	lifetime, cancel := context.WithCancel(context.Background())
	original := &executionRuntimeRun{done: make(chan struct{}), cancel: cancel}
	b.runs = append(b.runs, original)
	b.mu.Unlock()
	// Lifetime belongs to the installed concrete owners, never the startup
	// deadline, an HTTP request, or a Scheduler visit. Stop calls those owners.
	go func() { original.err = run(lifetime); cancel(); close(original.done) }()
	return original
}

func (b *executionRuntimeAssembly) Start(ctx context.Context) error {
	if ctx == nil {
		return f.NewFault(f.InvalidArgument, f.NotStarted)
	}
	b.mu.Lock()
	if b.constructing || b.started || b.stopped || b.initialize == nil || b.executor == nil || b.manager == nil {
		b.mu.Unlock()
		return f.NewFault(f.DependencyUnavailable, f.NotStarted)
	}
	b.started = true
	b.mu.Unlock()
	if err := b.initialize(ctx); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	executor := b.startRun(b.executor.Run)
	if executor == nil {
		return f.NewFault(f.ShuttingDown, f.NotStarted)
	}
	select {
	case <-b.executor.Ready():
	case <-executor.done:
		return errors.Join(f.NewFault(f.DependencyUnavailable, f.NotStarted), executor.err)
	case <-ctx.Done():
		return ctx.Err()
	}
	b.mu.Lock()
	stopped := b.stopped
	b.mu.Unlock()
	if stopped {
		return f.NewFault(f.ShuttingDown, f.NotStarted)
	}
	if b.startRun(b.manager.Run) == nil {
		return f.NewFault(f.ShuttingDown, f.NotStarted)
	}
	return ctx.Err()
}

func (b *executionRuntimeAssembly) Check(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	b.mu.Lock()
	available := b.started && !b.constructing && !b.stopped
	runs := append([]*executionRuntimeRun(nil), b.runs...)
	b.mu.Unlock()
	if !available || len(runs) != 2 {
		return f.NewFault(f.DependencyUnavailable, f.NotStarted)
	}
	for _, run := range runs {
		select {
		case <-run.done:
			return errors.Join(f.NewFault(f.DependencyUnavailable, f.NotStarted), run.err)
		default:
		}
	}
	return nil
}

func (b *executionRuntimeAssembly) snapshot() ([]executionRuntimeWork, []*executionRuntimeRun, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]executionRuntimeWork(nil), b.owners...), append([]*executionRuntimeRun(nil), b.runs...), b.constructing
}

func (b *executionRuntimeAssembly) StopAdmission() {
	b.mu.Lock()
	b.stopped = true
	b.mu.Unlock()
	owners, runs, _ := b.snapshot()
	for _, run := range runs {
		run.cancel()
	}
	for n := len(owners) - 1; n >= 0; n-- {
		owners[n].stop()
	}
}

func (b *executionRuntimeAssembly) Drain(ctx context.Context) error {
	b.StopAdmission()
	owners, runs, constructing := b.snapshot()
	if constructing {
		return f.NewFault(f.DependencyUnavailable, f.NotStarted)
	}
	for n := len(owners) - 1; n >= 0; n-- {
		if err := owners[n].drain(ctx); err != nil {
			return err
		}
		if !owners[n].joined() {
			return f.NewFault(f.DependencyUnavailable, f.NotStarted)
		}
	}
	for _, run := range runs {
		select {
		case <-run.done:
			// A cancellation is the normal exit of the exact stopped lifetime.
			// Retained Unknown has already prevented its owner from joining.
			if run.err != nil && !errors.Is(run.err, context.Canceled) {
				return run.err
			}
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	if !b.Joined() {
		return f.NewFault(f.DependencyUnavailable, f.NotStarted)
	}
	return nil
}

// Force cancels the same owners; it never bypasses a retained transaction or
// releases a provider beneath an unjoined consumer to satisfy a deadline.
func (b *executionRuntimeAssembly) Force(ctx context.Context) error { return b.Drain(ctx) }

func (b *executionRuntimeAssembly) Joined() bool {
	b.mu.Lock()
	stopped := b.stopped
	b.mu.Unlock()
	owners, runs, constructing := b.snapshot()
	if !stopped || constructing {
		return false
	}
	for _, owner := range owners {
		if !owner.joined() {
			return false
		}
	}
	for _, run := range runs {
		select {
		case <-run.done:
		default:
			return false
		}
	}
	return true
}
