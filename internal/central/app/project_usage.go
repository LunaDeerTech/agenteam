package app

import (
	"context"
	"net/http"

	"github.com/LunaDeerTech/agenteam/internal/central/account"
	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/config"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/knowledge"
	"github.com/LunaDeerTech/agenteam/internal/central/object"
	"github.com/LunaDeerTech/agenteam/internal/central/project"
	"github.com/LunaDeerTech/agenteam/internal/central/projectvariable"
	"github.com/LunaDeerTech/agenteam/internal/central/secret"
	"github.com/LunaDeerTech/agenteam/internal/central/usage"
	uc "github.com/LunaDeerTech/agenteam/internal/central/usage/contract"
	usagehttp "github.com/LunaDeerTech/agenteam/internal/central/usage/http"
)

// The sole Project authority retains every existing fact provider. The Skill
// initialization wrapper is constructed later and belongs only to Audit.
type projectUsageAssembly struct {
	projects    *project.Authority
	reader      *usage.Service
	variables   *projectvariable.Authority
	objectFacts *object.ProjectAuditAuthority
}

func createProjectUsage(cfg config.Config, db database, accounts *account.Authority) (*projectUsageAssembly, error) {
	base, err := createProjectUsageAuthority(db, accounts)
	if err != nil {
		return nil, err
	}
	return bindProjectUsage(cfg, db, accounts, base, nil)
}

// The opt-in graph constructs its original Project authority first, then fixes
// the actual Runtime Invocation provider before constructing Usage. No existing
// Usage authority is rebound and no second Project authority is introduced.
func createProjectUsageAuthority(db database, accounts *account.Authority) (*projectUsageAssembly, error) {
	projectsStore, ok := db.(project.Store)
	if !ok || runtimeInformationNil(projectsStore) || accounts == nil {
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
	knowledgeStore, ok := db.(knowledge.Store)
	if !ok || runtimeInformationNil(knowledgeStore) {
		return nil, foundation.NewFault(foundation.DependencyUnbound, foundation.NotStarted)
	}
	knowledgeFacts, err := knowledge.NewProjectAuditAuthority(knowledgeStore)
	if err != nil {
		return nil, err
	}
	objectStore, ok := db.(object.Store)
	if !ok || runtimeInformationNil(objectStore) {
		return nil, foundation.NewFault(foundation.DependencyUnbound, foundation.NotStarted)
	}
	objectFacts, err := object.NewProjectAuditAuthority(objectStore)
	if err != nil {
		return nil, err
	}
	projects, err := project.NewAuthority(projectsStore, project.AuthorityDependencies{Sessions: accounts, Routes: accounts, AuditFacts: map[ac.Producer]ac.ProjectFactAuthority{ac.SecretProducer: secretFacts, ac.ProjectVariableProducer: variableFacts, ac.KnowledgeProducer: knowledgeFacts, ac.ObjectProducer: objectFacts}})
	if err != nil {
		return nil, err
	}
	return &projectUsageAssembly{projects: projects, variables: variableFacts, objectFacts: objectFacts}, nil
}

func bindProjectUsage(cfg config.Config, db database, accounts *account.Authority, base *projectUsageAssembly, invocations uc.InvocationFacts) (*projectUsageAssembly, error) {
	usageStore, ok := db.(usage.Store)
	if !ok || runtimeInformationNil(usageStore) || accounts == nil || base == nil || base.projects == nil || base.reader != nil {
		return nil, foundation.NewFault(foundation.DependencyUnbound, foundation.NotStarted)
	}
	authority, err := usage.NewAuthority(usageStore, usage.Authorizations{Sessions: accounts, Projects: base.projects, Invocations: invocations})
	if err != nil {
		return nil, err
	}
	reader, err := usage.New(usageStore, authority, usage.Dependencies{Cursors: cfg.CursorKeyring()})
	if err != nil {
		return nil, err
	}
	bound := *base
	bound.reader = reader
	return &bound, nil
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
