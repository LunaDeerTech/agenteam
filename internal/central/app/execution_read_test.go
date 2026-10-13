package app

import (
	"context"
	"testing"

	"github.com/LunaDeerTech/agenteam/internal/central/account"
	"github.com/LunaDeerTech/agenteam/internal/central/execution"
)

func TestExecutionReadsRequireOriginalGraph(t *testing.T) {
	store := &workOwnedAppStore{}
	bundle := newWorkOwnedAppBundle(t, store)
	cfg := testConfig(t, "1s")
	accounts, err := account.NewAuthority(store, cfg.AccountKeyring())
	if err != nil {
		t.Fatal(err)
	}
	projects, err := createProjectUsageAuthorityWithLifecycle(store, accounts, bundle.lifecycle)
	if err != nil {
		t.Fatal(err)
	}
	deps, err := createExecutionReadDependencies(cfg, store, projects, bundle.owned, nil)
	if err != nil {
		t.Fatal(err)
	}
	if deps.Tasks.RequireStore(store) != nil {
		t.Fatal("original Work Store not retained")
	}
	if _, err = createExecutionReadDependencies(cfg, store, projects, bundle.legacy, nil); err == nil {
		t.Fatal("unowned Work reader accepted")
	}
	runtimeAuthority, err := execution.NewAuthority(store, projects.projects, nil)
	if err != nil {
		t.Fatal(err)
	}
	enabled, err := createExecutionReadDependencies(cfg, store, projects, bundle.owned, &executionRuntimeAuthorities{execution: runtimeAuthority})
	if err != nil || enabled.Authority != runtimeAuthority {
		t.Fatal("enabled reader replaced Authority", err)
	}
	if _, err = createExecutionReads(store, projects, bundle.objects, deps); err == nil {
		t.Fatal("uninitialized guard accepted")
	}
	if causes, sql := store.snapshot(); len(causes) != 0 || sql != 0 {
		t.Fatal("pure factory executed SQL")
	}
}
func TestExecutionReadAssemblyOriginalExit(t *testing.T) {
	// An absent read owner does not create Runtime or replace its original tail.
	a := &accountAssembly{}
	if a.executionReads != nil || a.executions != nil {
		t.Fatal("unexpected graph")
	}
	r := &executionReadAssembly{}
	if !r.Joined() || r.Drain(context.Background()) != nil {
		t.Fatal("empty partial owner")
	}
	a.executionReads = r
	found := false
	for _, owner := range a.works() {
		if owner == r {
			if found {
				t.Fatal("registered twice")
			}
			found = true
		}
	}
	if !found {
		t.Fatal("read owner absent from Account shutdown")
	}
	var _ accountWork = r
}
