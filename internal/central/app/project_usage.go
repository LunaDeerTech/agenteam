package app

import (
	"context"
	"net/http"

	"github.com/LunaDeerTech/agenteam/internal/central/account"
	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/config"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/project"
	"github.com/LunaDeerTech/agenteam/internal/central/projectvariable"
	"github.com/LunaDeerTech/agenteam/internal/central/secret"
	"github.com/LunaDeerTech/agenteam/internal/central/usage"
	usagehttp "github.com/LunaDeerTech/agenteam/internal/central/usage/http"
)

// The sole Project authority owns reads, commands and the real Secret Audit
// gate. Its initializer, Runtime facts and lifecycle owner remain unbound.
type projectUsageAssembly struct {
	projects  *project.Authority
	reader    *usage.Service
	variables *projectvariable.Authority
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
	secretStore, ok := db.(secret.Store)
	if !ok || runtimeInformationNil(secretStore) {
		return nil, foundation.NewFault(foundation.DependencyUnbound, foundation.NotStarted)
	}
	secretFacts, err := secret.NewProjectAuditAuthority(secretStore)
	if err != nil {
		return nil, err
	}
	variableFacts, err := createProjectVariableAuthority(db)
	if err != nil {
		return nil, err
	}
	projects, err := project.NewAuthority(projectsStore, project.AuthorityDependencies{Sessions: accounts, Routes: accounts, AuditFacts: map[ac.Producer]ac.ProjectFactAuthority{ac.SecretProducer: secretFacts, ac.ProjectVariableProducer: variableFacts}})
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
	return &projectUsageAssembly{projects: projects, reader: reader, variables: variableFacts}, nil
}

func (a *projectUsageAssembly) handler(core *account.Service, origin string) (http.Handler, error) {
	boundary, err := account.NewHTTPBoundary(core, origin)
	if err != nil {
		return nil, err
	}
	return usagehttp.NewHTTPHandler(a.projects, a.reader, boundary)
}

// Model, its independent Summary singleton and Usage consume the original
// Secret startup context, without another budget or background initializer.
func initializeModelsAndUsage(ctx context.Context, models, summary, usageSchema func(context.Context) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := models(ctx); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := summary(ctx); err != nil {
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
