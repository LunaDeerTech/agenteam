package app

import (
	"context"
	"net/http"

	"github.com/LunaDeerTech/agenteam/internal/central/account"
	"github.com/LunaDeerTech/agenteam/internal/central/config"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/project"
	"github.com/LunaDeerTech/agenteam/internal/central/usage"
	usagehttp "github.com/LunaDeerTech/agenteam/internal/central/usage/http"
)

// These are read capabilities, with no Project initializer, Runtime facts or
// lifecycle owner. All constructors retain the existing Store and Account authority.
type projectUsageAssembly struct {
	projects *project.Authority
	reader   *usage.Service
}

func createProjectUsage(cfg config.Config, db database, accounts *account.Authority) (*projectUsageAssembly, error) {
	projectsStore, ok := db.(project.Store)
	if !ok || runtimeInformationNil(projectsStore) || accounts == nil {
		return nil, foundation.NewFault(foundation.DependencyUnbound, foundation.NotStarted)
	}
	usageStore, ok := db.(usage.Store)
	if !ok || runtimeInformationNil(usageStore) {
		return nil, foundation.NewFault(foundation.DependencyUnbound, foundation.NotStarted)
	}
	projects, err := project.NewAuthority(projectsStore, project.AuthorityDependencies{Sessions: accounts, Routes: accounts})
	if err != nil {
		return nil, err
	}
	authority, err := usage.NewAuthority(usageStore, usage.Authorizations{Sessions: accounts, Projects: projects, Invocations: nil})
	if err != nil {
		return nil, err
	}
	reader, err := usage.New(usageStore, authority, usage.Dependencies{Cursors: cfg.CursorKeyring()})
	if err != nil {
		return nil, err
	}
	return &projectUsageAssembly{projects: projects, reader: reader}, nil
}

func (a *projectUsageAssembly) handler(core *account.Service, origin string) (http.Handler, error) {
	boundary, err := account.NewHTTPBoundary(core, origin)
	if err != nil {
		return nil, err
	}
	return usagehttp.NewHTTPHandler(a.projects, a.reader, boundary)
}

// Both schema checks consume the original Secret/Model startup context. No
// child budget, default rows, table creation or background work is introduced.
func initializeModelsAndUsage(ctx context.Context, models, usageSchema func(context.Context) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := models(ctx); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return usageSchema(ctx)
}

func projectUsageRoutes(existing, reads http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if usagehttp.HandlesPath(r.URL.Path) {
			reads.ServeHTTP(w, r)
			return
		}
		existing.ServeHTTP(w, r)
	})
}
