package app

import (
	"context"
	"github.com/LunaDeerTech/agenteam/internal/central/account"
	"github.com/LunaDeerTech/agenteam/internal/central/audit"
	"github.com/LunaDeerTech/agenteam/internal/central/config"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/outbox"
	"github.com/LunaDeerTech/agenteam/internal/central/project"
	"github.com/LunaDeerTech/agenteam/internal/central/projectvariable"
	vc "github.com/LunaDeerTech/agenteam/internal/central/projectvariable/contract"
	variablehttp "github.com/LunaDeerTech/agenteam/internal/central/projectvariable/http"
	"github.com/LunaDeerTech/agenteam/internal/central/secret"
	"net/http"
	"sync"
)

// Constructed inside the one Project assembly before its Audit map is frozen.
// The resulting exact pointer is also selected by the root Outbox registry.
func createProjectVariableAuthority(db database) (*projectvariable.Authority, error) {
	store, ok := db.(projectvariable.Store)
	if !ok || runtimeInformationNil(store) {
		return nil, foundation.NewFault(foundation.DependencyUnbound, foundation.NotStarted)
	}
	return projectvariable.NewAuthority(store)
}

type projectVariablesAssembly struct {
	service         *projectvariable.Service
	calls           workCommandCalls
	owner           *projectVariableCallAssembly
	mu              sync.Mutex
	stopped, joined bool
}

// The default App publishes both fixed views with one lifecycle owner. The
// original guard may be unbound here; admission proves its original live Tx.
type projectVariableCallAssembly struct {
	pair            *projectvariable.ProjectCallOwnership
	variables       *projectVariablesAssembly
	secretVariables *projectSecretVariablesAssembly
	mu              sync.Mutex
	stopped         bool
}

func createOwnedProjectVariables(cfg config.Config, db database, authority *projectvariable.Authority, writes *projectvariable.SecretWriteAuthority, secrets *secret.Service, projects *project.Authority, accounts *account.Authority, auditor *audit.Service, journal *outbox.Service, ordinaryEvents vc.VariableEvents, secretEvents vc.SecretVariableEvents, objects *objectAssembly) (*projectVariableCallAssembly, error) {
	store, ok := db.(projectvariable.Store)
	if !ok || runtimeInformationNil(store) || authority == nil || writes == nil || secrets == nil || projects == nil || accounts == nil || auditor == nil || journal == nil || objects == nil || objects.guard == nil || objects.process.Validate() != nil {
		return nil, foundation.NewFault(foundation.DependencyUnbound, foundation.NotStarted)
	}
	pair, err := projectvariable.NewProjectCallOwnership(store,
		projectvariable.Dependencies{
			Authority: authority, Projects: projects, Events: journal, VariableEvents: ordinaryEvents,
			Audit: auditor, Activity: accounts, Cursors: cfg.CursorKeyring(),
		},
		projectvariable.SecretDependencies{
			Authority: authority, Writes: writes, Secrets: secrets, Projects: projects,
			Events: journal, VariableEvents: secretEvents, Audit: auditor, Activity: accounts, Cursors: cfg.CursorKeyring(),
		}, objects.guard, objects.process)
	if err != nil {
		return nil, err
	}
	owner := &projectVariableCallAssembly{pair: pair}
	owner.variables = &projectVariablesAssembly{service: pair.Service(), calls: pair.Service(), owner: owner}
	owner.secretVariables = &projectSecretVariablesAssembly{service: pair.SecretService(), calls: pair.SecretService(), owner: owner}
	return owner, nil
}

func (b *projectVariableCallAssembly) StopAdmission() {
	b.pair.Stop()
	b.mu.Lock()
	b.stopped = true
	b.mu.Unlock()
}

func (b *projectVariableCallAssembly) Drain(ctx context.Context) error {
	b.StopAdmission()
	if err := b.pair.Drain(ctx); err != nil {
		return err
	}
	if !b.Joined() {
		return foundation.NewFault(foundation.DependencyUnavailable, foundation.NotStarted)
	}
	return nil
}

func (b *projectVariableCallAssembly) Force(ctx context.Context) error { return b.Drain(ctx) }

func (b *projectVariableCallAssembly) Joined() bool {
	b.mu.Lock()
	stopped := b.stopped
	b.mu.Unlock()
	return stopped && b.pair.Joined()
}

func createProjectVariables(cfg config.Config, db database, authority *projectvariable.Authority, projects *project.Authority, accounts *account.Authority, auditor *audit.Service, journal *outbox.Service, events vc.VariableEvents) (*projectVariablesAssembly, error) {
	store, ok := db.(projectvariable.Store)
	if !ok || runtimeInformationNil(store) || authority == nil || projects == nil || accounts == nil || auditor == nil || journal == nil {
		return nil, foundation.NewFault(foundation.DependencyUnbound, foundation.NotStarted)
	}
	service, e := projectvariable.New(store, projectvariable.Dependencies{Authority: authority, Projects: projects, Events: journal, VariableEvents: events, Audit: auditor, Activity: accounts, Cursors: cfg.CursorKeyring()})
	if e != nil {
		return nil, e
	}
	return &projectVariablesAssembly{service: service, calls: service}, nil
}
func (b *projectVariablesAssembly) StopAdmission() {
	if b.owner != nil {
		b.owner.StopAdmission()
		return
	}
	b.mu.Lock()
	b.stopped = true
	b.mu.Unlock()
	b.calls.Stop()
}
func (b *projectVariablesAssembly) Drain(ctx context.Context) error {
	if b.owner != nil {
		return b.owner.Drain(ctx)
	}
	b.StopAdmission()
	if e := b.calls.Drain(ctx); e != nil {
		return e
	}
	b.mu.Lock()
	b.joined = true
	b.mu.Unlock()
	return nil
}
func (b *projectVariablesAssembly) Force(ctx context.Context) error { return b.Drain(ctx) }
func (b *projectVariablesAssembly) Joined() bool {
	if b.owner != nil {
		return b.owner.Joined()
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.stopped && b.joined
}
func projectVariablesHandler(b *projectVariablesAssembly, core *account.Service, origin string) (http.Handler, error) {
	if b == nil || b.service == nil {
		return nil, foundation.NewFault(foundation.DependencyUnbound, foundation.NotStarted)
	}
	boundary, e := account.NewHTTPBoundary(core, origin)
	if e != nil {
		return nil, e
	}
	return variablehttp.NewHTTPHandler(b.service, boundary)
}
func projectVariablesRoutes(existing, variables http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if variablehttp.HandlesPath(r.URL.Path) {
			variables.ServeHTTP(w, r)
			return
		}
		existing.ServeHTTP(w, r)
	})
}

var _ accountWork = (*projectVariablesAssembly)(nil)
var _ accountWork = (*projectVariableCallAssembly)(nil)
