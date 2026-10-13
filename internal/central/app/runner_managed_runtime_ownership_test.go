package app

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/LunaDeerTech/agenteam/internal/central/agent"
	"github.com/LunaDeerTech/agenteam/internal/central/config"
	"github.com/LunaDeerTech/agenteam/internal/central/execution"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
)

// This fixture exercises constructor rejection without manufacturing an
// initialized Guard or an original Agent/Execution pair. Positive ownership
// and nonempty Agent configuration are exercised by the real assembly PG test.
func TestRunnerManagedAppRequiresOriginalBindings(t *testing.T) {
	v := newExecutionFactoryAppFixture(t)
	for _, mode := range []string{"missing-store", "foreign-life", "missing-life", "missing-agent", "missing-execution", "missing-objects", "missing-guard", "invalid-process", "uninitialized-pairs"} {
		t.Run(mode, func(t *testing.T) {
			store, life, objects := v.store, v.life, v.objects
			agents, executions := &agent.ProjectCallOwnership{}, &execution.ProjectCallOwnership{}
			switch mode {
			case "missing-store":
				store = nil
			case "foreign-life":
				var err error
				life, err = createProjectLifecycleAuthority(&workOwnedAppStore{})
				if err != nil {
					t.Fatal(err)
				}
			case "missing-life":
				life = nil
			case "missing-agent":
				agents = nil
			case "missing-execution":
				executions = nil
			case "missing-objects":
				objects = nil
			case "missing-guard":
				objects = &objectAssembly{process: v.objects.process}
			case "invalid-process":
				objects = &objectAssembly{guard: v.objects.guard}
			}
			got, err := createOwnedRunnerManagedRuntime(store, life, agents, executions, objects)
			var fault *f.Fault
			if got != nil || !errors.As(err, &fault) || fault.Code != f.DependencyUnbound {
				t.Fatal("unbound original graph published Runner-managed ownership", err)
			}
			v.requireNoSQL(t)
		})
	}
}

func TestRunnerManagedAppDisabledRuntimeStaysUnbound(t *testing.T) {
	b := &executionRuntimeAssembly{constructing: true, agentCalls: &agent.ProjectCallOwnership{}}
	err := constructExecutionRuntime(b, config.Config{}, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, executionRuntimeEventSet{})
	if err == nil || b.constructing || b.runnerManagedCalls != nil || b.runnerManagedCleanup != nil || b.executionCalls != nil || len(b.owners) != 0 || len(b.runs) != 0 {
		t.Fatal("disabled runtime invented a Runner-managed graph", err)
	}
	if err = b.Start(context.Background()); err == nil {
		t.Fatal("unbound runtime admitted execution")
	}
	if err = b.Drain(context.Background()); err != nil || !b.Joined() {
		t.Fatal("disabled construction retained an owner", err)
	}
}

type runnerManagedAppProvider struct {
	stopped atomic.Bool
	forced  atomic.Int32
}

func (p *runnerManagedAppProvider) StopAdmission()              { p.stopped.Store(true) }
func (*runnerManagedAppProvider) Drain(context.Context) error   { return nil }
func (p *runnerManagedAppProvider) Force(context.Context) error { p.forced.Add(1); return nil }
func (p *runnerManagedAppProvider) Joined() bool                { return p.stopped.Load() }

// The controlled original owner below proves the App's reverse dependency
// order and actual-return gate. It is not a Mount/SQL authorization witness.
func TestRunnerManagedAppOriginalControlTail(t *testing.T) {
	for _, late := range []bool{false, true} {
		name := "installed"
		if late {
			name = "late-install"
		}
		t.Run(name, func(t *testing.T) {
			b := &executionRuntimeAssembly{constructing: true}
			if late {
				b.StopAdmission()
			}
			var originalStopped, originalReturned atomic.Bool
			var executionDrains, managerDrains atomic.Int32
			entered, release := make(chan struct{}), make(chan struct{})
			var first, freeOnce sync.Once
			var originalContext context.Context
			free := func() { freeOnce.Do(func() { close(release) }) }
			b.add(executionRuntimeWork{stop: func() {}, drain: func(context.Context) error {
				executionDrains.Add(1)
				return nil
			}, joined: func() bool { return executionDrains.Load() != 0 }})
			b.add(executionRuntimeWork{stop: func() { originalStopped.Store(true) }, drain: func(ctx context.Context) error {
				firstCall := false
				first.Do(func() {
					firstCall = true
					originalContext = ctx
					close(entered)
					<-release
					originalReturned.Store(true)
				})
				if firstCall {
					return ctx.Err()
				}
				if ctx != originalContext {
					t.Error("later Force replaced the original remaining budget")
				}
				return nil // The original owner already actually returned.
			}, joined: func() bool { return originalStopped.Load() && originalReturned.Load() }})
			b.add(executionRuntimeWork{stop: func() {}, drain: func(context.Context) error {
				managerDrains.Add(1)
				return nil
			}, joined: func() bool { return managerDrains.Load() != 0 }})
			if late && !originalStopped.Load() {
				t.Fatal("late Runner control escaped the original Stop")
			}
			b.constructionDone()
			provider := &runnerManagedAppProvider{}
			accounts := &accountAssembly{executions: b, skills: provider}
			root := &resources{stopping: true, accountService: accounts}
			guard := &runtimeGuardProbe{}
			ctx, cancel := context.WithCancel(context.Background())
			done := make(chan error, 1)
			joined := false
			t.Cleanup(func() {
				cancel()
				free()
				if !joined {
					<-done
				}
			})
			go func() { done <- accounts.Force(ctx) }()
			projectCreationAwait(t, entered)
			cancel()
			select {
			case <-done:
				joined = true
				t.Fatal("cancellation replaced the original control return")
			default:
			}
			root.forceObjects(context.Background(), guard)
			if !originalStopped.Load() || b.Joined() || root.producersJoined() || executionDrains.Load() != 0 || managerDrains.Load() != 1 || provider.forced.Load() != 0 || guard.forced != 0 {
				t.Fatal("original Runner control released a dependency before returning")
			}
			free()
			err := <-done
			joined = true
			if !errors.Is(err, context.Canceled) || provider.forced.Load() != 0 || executionDrains.Load() != 0 {
				t.Fatal("original canceled return lost its error or released providers", err)
			}
			if err = accounts.Force(context.Background()); err != nil || !b.Joined() || !root.producersJoined() || executionDrains.Load() != 1 || provider.forced.Load() != 1 {
				t.Fatal("next explicit drain failed after the actual original return", err)
			}
			root.forceObjects(context.Background(), guard)
			if guard.forced != 1 {
				t.Fatal("joined graph did not release its original Guard")
			}
		})
	}
}
