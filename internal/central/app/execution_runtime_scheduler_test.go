package app

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/LunaDeerTech/agenteam/internal/central/account"
	"github.com/LunaDeerTech/agenteam/internal/central/execution"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/object"
	objc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/scheduler"
)

func TestOwnedSchedulerRuntimeRequiresOriginalInitializedGuard(t *testing.T) {
	store := &workOwnedAppStore{}
	cfg := testConfig(t, "1s")
	accounts, err := account.NewAuthority(store, cfg.AccountKeyring())
	if err != nil {
		t.Fatal(err)
	}
	life, err := createProjectLifecycleAuthority(store)
	if err != nil {
		t.Fatal(err)
	}
	projects, err := createProjectUsageAuthorityWithLifecycle(store, accounts, life)
	if err != nil {
		t.Fatal(err)
	}
	foreign, err := createProjectUsageAuthorityWithLifecycle(store, accounts, life)
	if err != nil {
		t.Fatal(err)
	}
	pending, err := scheduler.NewPendingAuthority(store)
	if err != nil {
		t.Fatal(err)
	}
	process := updateRootID[objc.Process]()
	spool, err := object.OpenSpool(filepath.Join(t.TempDir(), "spool"), process)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := spool.Close(); err != nil {
			t.Error(err)
		}
	})
	guard, err := object.OpenProcessGuard(spool, process)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := guard.Close(); err != nil {
			t.Error(err)
		}
	})
	objects := &objectAssembly{process: process, guard: guard}
	_, original := guard.CurrentProcess()
	var originalFault *f.Fault
	if !errors.As(original, &originalFault) || originalFault.Code != f.DependencyUnavailable {
		t.Fatal("opening the original flock fabricated an initialized guard", original)
	}
	for _, sameProject := range []bool{true, false} {
		name := "opened-guard-is-not-initialized"
		if !sameProject {
			name = "different-original-project"
		}
		t.Run(name, func(t *testing.T) {
			originalProject := projects.projects
			want := originalFault.Code
			if !sameProject {
				originalProject = foreign.projects
				want = f.DependencyUnbound
			}
			// Remaining owner ports are intentionally absent. This asserts the
			// actual original guard / Project boundary before any pair is made,
			// not a successful Scheduler graph or a fabricated live transaction.
			deps := scheduler.ProjectCallDependencies{Coordinator: scheduler.CoordinatorDependencies{Projects: originalProject}}
			got, err := createOwnedSchedulerRuntime(store, pending, deps, life, projects.projects, objects, &execution.AssociatedExecutor{}, scheduler.ProjectRunnersOptions{})
			var fault *f.Fault
			if got != nil || !errors.As(err, &fault) || fault.Code != want {
				t.Fatal("factory bypassed the original binding boundary", err)
			}
			if causes, sql := store.snapshot(); len(causes) != 0 || sql != 0 {
				t.Fatal("rejected construction performed transaction work")
			}
		})
	}
}

// Only the first Stop return is controlled. Subsequent Stop calls from the
// actual account.install path stay live; sync.Once around a blocked callback
// would incorrectly serialize and hide the installation interval under test.
type schedulerRuntimeAppStop struct {
	first            atomic.Bool
	entered, release chan struct{}
	mu               sync.Mutex
	stopped, joined  bool
	forced           []context.Context
}

func (s *schedulerRuntimeAppStop) StopAdmission() {
	s.mu.Lock()
	s.stopped = true
	s.mu.Unlock()
	if s.first.CompareAndSwap(false, true) {
		close(s.entered)
		<-s.release
	}
}
func (s *schedulerRuntimeAppStop) Drain(context.Context) error {
	s.mu.Lock()
	s.joined = s.stopped
	s.mu.Unlock()
	return nil
}
func (s *schedulerRuntimeAppStop) Force(ctx context.Context) error {
	s.mu.Lock()
	s.forced = append(s.forced, ctx)
	s.mu.Unlock()
	return s.Drain(ctx)
}
func (s *schedulerRuntimeAppStop) Joined() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.stopped && s.joined
}

func TestOwnedRuntimeLateInstallBeforeOriginalForceEnumeration(t *testing.T) {
	prior := &schedulerRuntimeAppStop{entered: make(chan struct{}), release: make(chan struct{})}
	provider := &runtimeExternalProbe{}
	root := &accountAssembly{planning: prior, projects: provider}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var once sync.Once
	release := func() { once.Do(func() { close(prior.release) }) }
	t.Cleanup(release)
	originalForce := variableAppAsync(t, release, func() error { return root.Force(ctx) })
	variableAppReceive(t, prior.entered)
	// The original Force has already captured its old fields and is inside
	// StopAdmission. Install uses the real late-install cancellation and Force
	// path while this new assembly's actual construction tail is still open.
	late := &executionRuntimeAssembly{constructing: true}
	t.Cleanup(late.constructionDone)
	if root.install(context.Background(), func() { root.executions = late }) {
		t.Fatal("late Runtime escaped stopped root admission")
	}
	if late.Joined() || provider.forced != 0 {
		t.Fatal("late constructor tail was retired during installation")
	}
	release()
	if err := variableAppReceive(t, originalForce); err == nil || root.Joined() || late.Joined() || provider.forced != 0 {
		t.Fatal("original Force used its stale Runtime snapshot and retired providers", err)
	}
	prior.mu.Lock()
	forced := append([]context.Context(nil), prior.forced...)
	prior.mu.Unlock()
	if len(forced) != 0 || root.forced != ctx {
		t.Fatal("held construction allowed provider Force or replaced its context")
	}
	resources := &resources{stopping: true, accountService: root}
	guard := &runtimeGuardProbe{}
	resources.forceObjects(ctx, guard)
	if guard.forced != 0 {
		t.Fatal("partial late graph released the original ProcessGuard")
	}
	// This is the original failed-install branch's construction return. Only
	// that physical tail permits the later explicit Force to reach providers.
	late.constructionDone()
	other, cancelOther := context.WithCancel(context.Background())
	defer cancelOther()
	if err := root.Force(other); err != nil || !root.Joined() || !late.Joined() || provider.forced != 1 {
		t.Fatal("returned empty construction did not release its original root", err)
	}
	prior.mu.Lock()
	forced = append([]context.Context(nil), prior.forced...)
	prior.mu.Unlock()
	if len(forced) != 1 || forced[0] != ctx || root.forced != ctx {
		t.Fatal("later explicit Force renewed the original shutdown context")
	}
	resources.forceObjects(other, guard)
	if guard.forced != 1 {
		t.Fatal("actual constructor/provider returns did not release guard")
	}
}
