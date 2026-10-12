package app

import (
	"context"
	"net/http"
	"sync"

	"github.com/LunaDeerTech/agenteam/internal/central/account"
	"github.com/LunaDeerTech/agenteam/internal/central/agent"
	agenthttp "github.com/LunaDeerTech/agenteam/internal/central/agent/http"
	"github.com/LunaDeerTech/agenteam/internal/central/audit"
	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/model"
	"github.com/LunaDeerTech/agenteam/internal/central/mount"
	"github.com/LunaDeerTech/agenteam/internal/central/outbox"
	"github.com/LunaDeerTech/agenteam/internal/central/project"
	"github.com/LunaDeerTech/agenteam/internal/central/projectvariable"
	runnerservice "github.com/LunaDeerTech/agenteam/internal/central/runner/service"
	"github.com/LunaDeerTech/agenteam/internal/central/skill"
	"github.com/LunaDeerTech/agenteam/internal/central/tool/builtin"
	"github.com/LunaDeerTech/agenteam/internal/central/tool/registry"
	toolruntime "github.com/LunaDeerTech/agenteam/internal/central/tool/runtime"
)

func createAgentConfigurationAuthority(db database, projects *project.Authority) (*agent.Authority, error) {
	store, ok := db.(agent.Store)
	if !ok || runtimeInformationNil(store) || projects == nil {
		return nil, f.NewFault(f.DependencyUnbound, f.NotStarted)
	}
	return agent.NewAuthority(store, projects)
}

// InstallAuthority is a real, fixed Skill consumer even when the optional
// Execution Runtime is disabled. It admits no Tool call without its original
// InstallExecutor's private witness; constructing it starts no executor.
type agentInstallWork struct{ authority *toolruntime.InstallAuthority }

func createAgentInstall(db database, objects *objectAssembly) (*agentInstallWork, error) {
	store, ok := db.(toolruntime.Store)
	if !ok || runtimeInformationNil(store) || objects == nil || objects.guard == nil {
		return nil, f.NewFault(f.DependencyUnbound, f.NotStarted)
	}
	a, err := toolruntime.NewInstallAuthority(store, objects.guard, objects.process)
	if err != nil {
		return nil, err
	}
	return &agentInstallWork{a}, nil
}
func (w *agentInstallWork) StopAdmission() { w.authority.Stop() }
func (w *agentInstallWork) Drain(ctx context.Context) error {
	w.StopAdmission()
	return w.authority.Drain(ctx)
}
func (w *agentInstallWork) Force(ctx context.Context) error { return w.Drain(ctx) }
func (w *agentInstallWork) Joined() bool                    { return w.authority.Joined() }

type agentConfigurationAssembly struct {
	store     agent.Store
	service   *agent.Service
	ownership *agent.ProjectCallOwnership
	registry  *registry.Registry
	mu        sync.Mutex
	stopped   bool
	initDone  chan struct{}
	initStop  context.CancelFunc
}

// Keep the existing configuration-only constructor independent of call
// ownership. The default App uses the explicit owned constructor below.
func createAgentConfiguration(db database, projects *project.Authority, accounts *account.Authority,
	agents *agent.Authority, agentEvents *agent.EventAuthority, models *model.Authority,
	skills *skill.Service, skillAuthority *skill.Authority, installer *agentInstallWork,
	auditor *audit.Service, journal *outbox.Service, cursorKeys ...cursor.Keyring) (*agentConfigurationAssembly, error) {
	providers, err := createAgentConfigurationProviders(db, projects, accounts, agents, agentEvents, models, skills, skillAuthority, installer, auditor, journal, cursorKeys...)
	if err != nil {
		return nil, err
	}
	service, err := agent.New(providers.store, providers.dependencies)
	if err != nil {
		return nil, err
	}
	return &agentConfigurationAssembly{store: providers.store, service: service, registry: providers.registry}, nil
}

// Construction fixes both real Agent instances before either is published.
// The original guard may still be unbound: its live Store/Tx and process are
// proved by each actual registration/checkpoint, never by this constructor.
func createOwnedAgentConfiguration(db database, projects *project.Authority, accounts *account.Authority,
	agents *agent.Authority, agentEvents *agent.EventAuthority, models *model.Authority,
	skills *skill.Service, skillAuthority *skill.Authority, installer *agentInstallWork,
	auditor *audit.Service, journal *outbox.Service, objects *objectAssembly, keys cursor.Keyring) (*agentConfigurationAssembly, error) {
	if objects == nil || objects.guard == nil || objects.process.Validate() != nil {
		return nil, f.NewFault(f.DependencyUnbound, f.NotStarted)
	}
	providers, err := createAgentConfigurationProviders(db, projects, accounts, agents, agentEvents, models, skills, skillAuthority, installer, auditor, journal, keys)
	if err != nil {
		return nil, err
	}
	ownership, err := agent.NewProjectCallOwnership(providers.store, providers.dependencies, keys, objects.guard, objects.process)
	if err != nil {
		return nil, err
	}
	return &agentConfigurationAssembly{store: providers.store, service: ownership.Service(), ownership: ownership, registry: providers.registry}, nil
}

type agentConfigurationProviders struct {
	store        agent.Store
	registry     *registry.Registry
	dependencies agent.Dependencies
}

// Both constructors use the same fixed real providers. The Skill Authority
// and Service are also shared by Object, Owner reads and Execution capture.
func createAgentConfigurationProviders(db database, projects *project.Authority, accounts *account.Authority,
	agents *agent.Authority, agentEvents *agent.EventAuthority, models *model.Authority,
	skills *skill.Service, skillAuthority *skill.Authority, installer *agentInstallWork,
	auditor *audit.Service, journal *outbox.Service, cursorKeys ...cursor.Keyring) (*agentConfigurationProviders, error) {
	store, ok := db.(agent.Store)
	if !ok || runtimeInformationNil(store) || projects == nil || accounts == nil || agents == nil || agentEvents == nil || models == nil || skills == nil || skillAuthority == nil || installer == nil || installer.authority == nil || auditor == nil || journal == nil {
		return nil, f.NewFault(f.DependencyUnbound, f.NotStarted)
	}
	source, err := toolruntime.NewInstallSource(installer.authority, skills)
	if err != nil {
		return nil, err
	}
	registered, err := registry.New(store, registry.Options{
		Sources:        map[string]registry.BuiltinSource{builtin.SkillInstallStableKey: source},
		Authorizations: registry.Authorizations{Sessions: accounts, Projects: projects},
	})
	if err != nil {
		return nil, err
	}
	tools, err := registry.NewConfiguration(registered, agents, builtin.SkillInstallStableKey)
	if err != nil {
		return nil, err
	}
	selections, err := model.NewAgentConfiguration(store, models)
	if err != nil {
		return nil, err
	}
	modelOwner, err := agent.NewModelOwnerAuthority(agents)
	if err != nil {
		return nil, err
	}
	modelRefs, err := model.NewAgentReferences(store, selections, modelOwner)
	if err != nil {
		return nil, err
	}
	secrets, err := projectvariable.NewSecretDirectory(store, projects)
	if err != nil {
		return nil, err
	}
	secretRefs, err := projectvariable.NewSecretReferences(store, secrets, agents)
	if err != nil {
		return nil, err
	}
	runners, err := runnerservice.NewConfigurationDirectory(store, projects)
	if err != nil {
		return nil, err
	}
	var keys cursor.Keyring
	if len(cursorKeys) > 1 {
		return nil, f.NewFault(f.InvalidArgument, f.NotStarted)
	}
	if len(cursorKeys) == 1 {
		keys = cursorKeys[0]
		if keys.Validate() != nil {
			return nil, f.NewFault(f.InvalidArgument, f.NotStarted)
		}
	}
	mounts, err := mount.NewRunnerMountConfiguration(store, projects, agents, runners)
	if err != nil {
		return nil, err
	}
	initial, err := skill.NewAgentInitializer(skillAuthority, agents)
	if err != nil {
		return nil, err
	}
	dependencies := agent.Dependencies{
		RunnerCandidates: runners, RunnerCursorKeys: keys, Authority: agents, EventAuthority: agentEvents, Events: journal, Audit: auditor, Activity: accounts,
		Models: modelRefs, Tools: tools, ToolReferences: tools, Mounts: mounts,
		Secrets: secrets, SecretReferences: secretRefs, Skills: initial,
	}
	return &agentConfigurationProviders{store: store, registry: registered, dependencies: dependencies}, nil
}

// Called after the original Object.Initialize has bound the ProcessGuard and
// before HTTP/Project runners are admitted. Reconciliation uses the real
// Source's same-Tx binding checks, not a predeclared Active registration.
func (a *agentConfigurationAssembly) Initialize(ctx context.Context) error {
	if a == nil || a.service == nil || a.registry == nil || runtimeInformationNil(a.store) {
		return f.NewFault(f.DependencyUnbound, f.NotStarted)
	}
	if ctx == nil {
		return f.NewFault(f.InvalidArgument, f.NotStarted)
	}
	a.mu.Lock()
	if a.stopped || a.initDone != nil {
		a.mu.Unlock()
		return f.NewFault(f.ShuttingDown, f.NotStarted)
	}
	ctx, a.initStop = context.WithCancel(ctx)
	a.initDone = make(chan struct{})
	done, cancel := a.initDone, a.initStop
	a.mu.Unlock()
	defer func() { cancel(); close(done) }()
	run, err := f.NewID[f.Request]()
	if err != nil {
		return err
	}
	cause, err := f.NewRecoveryCause("app.agent-configuration", run.String(), "")
	if err != nil {
		return err
	}
	result := a.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := a.store.AcquireAll(ctx, tx, []f.LockRequest{registry.RegistryLock(f.Exclusive)}); err != nil {
			return err
		}
		_, err := a.registry.ReconcileBuiltinInTx(ctx, tx, builtin.SkillInstallStableKey)
		return err
	})
	switch result.State() {
	case f.Committed:
		return nil
	case f.NotCommitted:
		return result.Fault()
	default:
		fault := f.NewFault(f.CommitUnknown, f.Unknown)
		fault.CauseID = result.AttemptID().String()
		return fault
	}
}

func (a *agentConfigurationAssembly) StopAdmission() {
	a.mu.Lock()
	a.stopped = true
	cancel := a.initStop
	a.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if a.ownership != nil {
		a.ownership.Stop()
		return
	}
	a.service.Stop()
}
func (a *agentConfigurationAssembly) Drain(ctx context.Context) error {
	a.StopAdmission()
	a.mu.Lock()
	done := a.initDone
	a.mu.Unlock()
	if done != nil {
		select {
		case <-done:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	if a.ownership != nil {
		return a.ownership.Drain(ctx)
	}
	return a.service.Drain(ctx)
}
func (a *agentConfigurationAssembly) Force(ctx context.Context) error { return a.Drain(ctx) }
func (a *agentConfigurationAssembly) Joined() bool {
	a.mu.Lock()
	done, stopped := a.initDone, a.stopped
	a.mu.Unlock()
	if !stopped {
		return false
	}
	if done != nil {
		select {
		case <-done:
		default:
			return false
		}
	}
	if a.ownership != nil {
		return a.ownership.Joined()
	}
	return a.service.Joined()
}

func agentConfigurationHandler(a *agentConfigurationAssembly, core *account.Service, origin string) (http.Handler, error) {
	if a == nil || a.service == nil {
		return nil, f.NewFault(f.DependencyUnbound, f.NotStarted)
	}
	boundary, err := account.NewHTTPBoundary(core, origin)
	if err != nil {
		return nil, err
	}
	commands, err := agenthttp.NewConfigurationHTTPHandler(a.service, boundary)
	if err != nil {
		return nil, err
	}
	reads, err := agenthttp.NewRunnerMountHTTPHandler(a.service, boundary)
	if err != nil {
		return nil, err
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r != nil && r.URL != nil && agenthttp.HandlesRunnerMountPath(r.URL.Path) {
			reads.ServeHTTP(w, r)
			return
		}
		commands.ServeHTTP(w, r)
	}), nil
}
func agentConfigurationRoutes(existing, commands http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if agenthttp.HandlesConfigurationRequest(r) || r != nil && r.URL != nil && agenthttp.HandlesRunnerMountPath(r.URL.Path) {
			commands.ServeHTTP(w, r)
			return
		}
		existing.ServeHTTP(w, r)
	})
}

var _ accountWork = (*agentInstallWork)(nil)
var _ accountWork = (*agentConfigurationAssembly)(nil)
