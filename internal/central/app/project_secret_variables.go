package app

import (
	"context"
	"net/http"
	"sync"

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
)

// The immutable write authority precedes D04 construction. It shares the
// original Project authority and Store; the existing facts authority remains
// the sole projectvariable Audit/Outbox producer for both variable types.
func createProjectSecretWriteAuthority(db database, projects *project.Authority) (*projectvariable.SecretWriteAuthority, error) {
	store, ok := db.(projectvariable.Store)
	if !ok || runtimeInformationNil(store) || projects == nil {
		return nil, foundation.NewFault(foundation.DependencyUnbound, foundation.NotStarted)
	}
	return projectvariable.NewSecretWriteAuthority(store, projects)
}

type projectSecretVariablesAssembly struct {
	service         *projectvariable.SecretService
	calls           workCommandCalls
	mu              sync.Mutex
	stopped, joined bool
}

func createProjectSecretVariables(cfg config.Config, db database, authority *projectvariable.Authority, writes *projectvariable.SecretWriteAuthority, secrets *secret.Service, projects *project.Authority, accounts *account.Authority, auditor *audit.Service, journal *outbox.Service, events vc.SecretVariableEvents) (*projectSecretVariablesAssembly, error) {
	store, ok := db.(projectvariable.Store)
	if !ok || runtimeInformationNil(store) || authority == nil || writes == nil || secrets == nil || projects == nil || accounts == nil || auditor == nil || journal == nil {
		return nil, foundation.NewFault(foundation.DependencyUnbound, foundation.NotStarted)
	}
	service, err := projectvariable.NewSecret(store, projectvariable.SecretDependencies{
		Authority: authority, Writes: writes, Secrets: secrets, Projects: projects,
		Events: journal, VariableEvents: events, Audit: auditor, Activity: accounts, Cursors: cfg.CursorKeyring(),
	})
	if err != nil {
		return nil, err
	}
	return &projectSecretVariablesAssembly{service: service, calls: service}, nil
}

func (b *projectSecretVariablesAssembly) StopAdmission() {
	b.mu.Lock()
	b.stopped = true
	b.mu.Unlock()
	b.calls.Stop()
}

func (b *projectSecretVariablesAssembly) Drain(ctx context.Context) error {
	b.StopAdmission()
	if err := b.calls.Drain(ctx); err != nil {
		return err
	}
	b.mu.Lock()
	b.joined = true
	b.mu.Unlock()
	return nil
}

func (b *projectSecretVariablesAssembly) Force(ctx context.Context) error { return b.Drain(ctx) }

func (b *projectSecretVariablesAssembly) Joined() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.stopped && b.joined
}

func projectSecretVariablesHandler(b *projectSecretVariablesAssembly, core *account.Service, origin string) (http.Handler, error) {
	if b == nil || b.service == nil {
		return nil, foundation.NewFault(foundation.DependencyUnbound, foundation.NotStarted)
	}
	boundary, err := account.NewHTTPBoundary(core, origin)
	if err != nil {
		return nil, err
	}
	return variablehttp.NewSecretHTTPHandler(b.service, boundary)
}

func projectSecretVariablesRoutes(existing, variables http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if variablehttp.HandlesSecretPath(r.URL.Path) {
			variables.ServeHTTP(w, r)
			return
		}
		existing.ServeHTTP(w, r)
	})
}

var _ accountWork = (*projectSecretVariablesAssembly)(nil)
