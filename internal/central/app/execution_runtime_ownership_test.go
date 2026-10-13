package app

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/LunaDeerTech/agenteam/internal/central/account"
	"github.com/LunaDeerTech/agenteam/internal/central/audit"
	"github.com/LunaDeerTech/agenteam/internal/central/execution"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/model"
	"github.com/LunaDeerTech/agenteam/internal/central/object"
	objc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/project"
	pv "github.com/LunaDeerTech/agenteam/internal/central/projectvariable"
	"github.com/LunaDeerTech/agenteam/internal/central/secret"
)

type executionFactoryAppFixture struct {
	store     *workOwnedAppStore
	life      *project.LifecycleAuthority
	deps      execution.ProjectCallDependencies
	variables *pv.Authority
	secrets   *secret.Service
	objects   *objectAssembly
}

// These are the real, pure capability constructors. This fixture deliberately
// does not initialize the Object guard or fabricate a successful transaction.
// The real App test owns positive pair construction and first-Launch provenance.
func newExecutionFactoryAppFixture(t *testing.T) executionFactoryAppFixture {
	t.Helper()
	v := executionFactoryAppFixture{store: &workOwnedAppStore{}}
	cfg := testConfig(t, "1s")
	accounts, err := account.NewAuthority(v.store, cfg.AccountKeyring())
	if err != nil {
		t.Fatal(err)
	}
	v.life, err = createProjectLifecycleAuthority(v.store)
	if err != nil {
		t.Fatal(err)
	}
	projects, err := createProjectUsageAuthorityWithLifecycle(v.store, accounts, v.life)
	if err != nil {
		t.Fatal(err)
	}
	v.variables = projects.variables
	v.objects = &objectAssembly{process: updateRootID[objc.Process](), guard: &object.ProcessGuard{}}
	authorities, err := createExecutionRuntimeAuthorities(v.store, projects.projects, v.objects.guard)
	if err != nil {
		t.Fatal(err)
	}
	v.deps.Service.Authority = authorities.execution
	writes, err := createProjectSecretWriteAuthority(v.store, projects.projects)
	if err != nil {
		t.Fatal(err)
	}
	models, err := model.NewAuthority(v.store, model.Authorizations{Sessions: accounts, System: accounts, Projects: projects.projects})
	if err != nil {
		t.Fatal(err)
	}
	usage, err := model.NewSecretUsageRouter(models, accounts)
	if err != nil {
		t.Fatal(err)
	}
	auditor, err := audit.New(v.store, cfg.CursorKeyring(), audit.Authorizations{Accounts: accounts, Sessions: accounts, System: accounts, Projects: projects.projects})
	if err != nil {
		t.Fatal(err)
	}
	v.secrets, err = createSecret(cfg, v.store, auditor, accounts, usage, projects.projects, writes)
	if err != nil {
		t.Fatal(err)
	}
	v.requireNoSQL(t)
	return v
}

func (v executionFactoryAppFixture) requireNoSQL(t *testing.T) {
	t.Helper()
	if causes, sql := v.store.snapshot(); len(causes) != 0 || sql != 0 {
		t.Fatal("factory performed transaction work", len(causes), sql)
	}
}

func TestOwnedExecutionRuntimeRequiredBindings(t *testing.T) {
	v := newExecutionFactoryAppFixture(t)
	foreignLife, err := createProjectLifecycleAuthority(&workOwnedAppStore{})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name   string
		change func(*executionFactoryAppFixture)
	}{
		{"missing-store", func(v *executionFactoryAppFixture) { v.store = nil }},
		{"missing-life", func(v *executionFactoryAppFixture) { v.life = nil }},
		{"foreign-life-store", func(v *executionFactoryAppFixture) { v.life = foreignLife }},
		{"missing-execution-authority", func(v *executionFactoryAppFixture) { v.deps.Service.Authority = nil }},
		{"missing-variables", func(v *executionFactoryAppFixture) { v.variables = nil }},
		{"missing-secret", func(v *executionFactoryAppFixture) { v.secrets = nil }},
		{"missing-objects", func(v *executionFactoryAppFixture) { v.objects = nil }},
		{"missing-guard", func(v *executionFactoryAppFixture) { v.objects = &objectAssembly{process: v.objects.process} }},
		{"invalid-process", func(v *executionFactoryAppFixture) { v.objects = &objectAssembly{guard: v.objects.guard} }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			candidate := v
			tc.change(&candidate)
			got, err := createOwnedExecutionRuntime(candidate.store, candidate.deps, candidate.life, candidate.variables, candidate.secrets, candidate.objects)
			var fault *f.Fault
			if got != nil || !errors.As(err, &fault) || fault.Code != f.DependencyUnbound {
				t.Fatal("invalid original binding published an Execution graph", err)
			}
			v.requireNoSQL(t)
		})
	}
}

func TestOwnedExecutionRuntimeRequiresInitializedGuard(t *testing.T) {
	v := newExecutionFactoryAppFixture(t)
	spool, err := object.OpenSpool(filepath.Join(t.TempDir(), "spool"), v.objects.process)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := spool.Close(); err != nil {
			t.Error(err)
		}
	})
	guard, err := object.OpenProcessGuard(spool, v.objects.process)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := guard.Close(); err != nil {
			t.Error(err)
		}
	})
	v.objects = &objectAssembly{process: v.objects.process, guard: guard}
	// Opening the real flock is not the original runtime's successful bind.
	// Its DependencyUnavailable must survive the factory unchanged; absent
	// Service/driver ports would instead return DependencyUnbound if reached.
	_, original := guard.CurrentProcess()
	var originalFault *f.Fault
	if !errors.As(original, &originalFault) || originalFault.Code != f.DependencyUnavailable {
		t.Fatal("uninitialized original guard was unexpectedly bound", original)
	}
	got, err := createOwnedExecutionRuntime(v.store, v.deps, v.life, v.variables, v.secrets, v.objects)
	var fault *f.Fault
	if got != nil || !errors.As(err, &fault) || fault.Code != originalFault.Code || fault.CommitState != originalFault.CommitState {
		t.Fatal("factory bypassed or replaced original guard rejection", err)
	}
	v.requireNoSQL(t)
}
