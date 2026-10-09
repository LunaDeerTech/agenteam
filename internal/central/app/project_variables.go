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
	mu              sync.Mutex
	stopped, joined bool
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
	b.mu.Lock()
	b.stopped = true
	b.mu.Unlock()
	b.calls.Stop()
}
func (b *projectVariablesAssembly) Drain(ctx context.Context) error {
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
