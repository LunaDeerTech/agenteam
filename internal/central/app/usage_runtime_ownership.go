package app

import (
	"context"

	"github.com/LunaDeerTech/agenteam/internal/central/account"
	"github.com/LunaDeerTech/agenteam/internal/central/config"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/project"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/usage"
	uc "github.com/LunaDeerTech/agenteam/internal/central/usage/contract"
)

// The same service is installed in HTTP and the original Model Runtime. The
// latter borrows its private whole-call owner; it never receives another Usage
// instance or a replacement InvocationFacts authority.
type usageCallAssembly struct {
	pair    *usage.ProjectCallOwnership
	cleanup pc.ProjectLifecycleParticipant
}

func bindOwnedProjectUsage(cfg config.Config, db database, accounts *account.Authority, base *projectUsageAssembly, invocations uc.InvocationFacts, objects *objectAssembly) (*projectUsageAssembly, error) {
	store, ok := db.(usage.Store)
	if !ok || runtimeInformationNil(store) || accounts == nil || base == nil || base.projects == nil || base.reader != nil || base.usageCalls != nil || base.lifecycle == nil || objects == nil || objects.guard == nil || objects.process.Validate() != nil {
		return nil, f.NewFault(f.DependencyUnbound, f.NotStarted)
	}
	stop, err := project.NewUsageStopAuthority(base.lifecycle, store)
	if err != nil {
		return nil, err
	}
	cleanup, err := project.NewUsageProjectCleanupAuthority(base.lifecycle, store)
	if err != nil {
		return nil, err
	}
	authority, err := usage.NewAuthority(store, usage.Authorizations{Sessions: accounts, Projects: base.projects, Invocations: invocations})
	if err != nil {
		return nil, err
	}
	// Like Work, Usage fixes the actual guard/expected process here and verifies
	// its binding on first admission. Object.Initialize has not yet run; this
	// constructor must not initialize the guard or perform transaction work.
	pair, err := usage.NewProjectCallOwnership(store, authority, usage.Dependencies{Cursors: cfg.CursorKeyring()}, objects.guard, objects.process, stop)
	if err != nil {
		return nil, err
	}
	bound := *base
	bound.reader = pair.Service()
	bound.usageCalls = &usageCallAssembly{pair: pair}
	bound.usageCalls.cleanup, err = usage.NewProjectCleanupParticipant(pair, cleanup)
	// Even a final binding failure returns the original pair. Account installs
	// it before inspecting err, so Stop/Force cannot lose a partial constructor.
	return &bound, err
}

func (a *usageCallAssembly) StopAdmission() {
	if a != nil && a.pair != nil {
		a.pair.Stop()
	}
}

func (a *usageCallAssembly) Drain(ctx context.Context) error {
	if a == nil || a.pair == nil {
		return nil
	}
	a.StopAdmission()
	if err := a.pair.Drain(ctx); err != nil {
		return err
	}
	if !a.pair.Joined() {
		return f.NewFault(f.DependencyUnavailable, f.NotStarted)
	}
	return nil
}

func (a *usageCallAssembly) Force(ctx context.Context) error { return a.Drain(ctx) }
func (a *usageCallAssembly) Joined() bool {
	return a == nil || a.pair == nil || a.pair.Joined()
}

var _ accountWork = (*usageCallAssembly)(nil)
