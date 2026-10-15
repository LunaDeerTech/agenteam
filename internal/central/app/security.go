package app

import (
	"context"
	"errors"
	"github.com/LunaDeerTech/agenteam/internal/central/account"
	"github.com/LunaDeerTech/agenteam/internal/central/audit"
	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/config"
	"github.com/LunaDeerTech/agenteam/internal/central/model"
	"github.com/LunaDeerTech/agenteam/internal/central/project"
	"github.com/LunaDeerTech/agenteam/internal/central/projectvariable"
	runners "github.com/LunaDeerTech/agenteam/internal/central/runner/service"
	"github.com/LunaDeerTech/agenteam/internal/central/secret"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
	"time"
)

const SecurityStartupTimeout = 30 * time.Second

func createSecurity(cfg config.Config, db database, authority *account.Authority, models *model.Authority, projects *project.Authority) (*audit.Service, error) {
	return createSecurityWithRunners(cfg, db, authority, models, projects, nil)
}

func createSecurityWithRunners(cfg config.Config, db database, authority *account.Authority, models *model.Authority, projects ac.ProjectAuthority, runnerAuthority *runners.Authority, mcpAuthority ...ac.MCPAuthority) (*audit.Service, error) {
	store, ok := db.(audit.Store)
	if !ok || authority == nil {
		return nil, errors.New("AUDIT_STORE_UNAVAILABLE")
	}
	if models == nil {
		return nil, errors.New("MODEL_AUTHORITY_UNAVAILABLE")
	}
	if runtimeInformationNil(projects) {
		return nil, errors.New("PROJECT_AUTHORITY_UNAVAILABLE")
	}
	var mcp ac.MCPAuthority
	if len(mcpAuthority) > 1 {
		return nil, errors.New("MCP_AUDIT_UNAVAILABLE")
	}
	if len(mcpAuthority) == 1 {
		mcp = mcpAuthority[0]
	}
	return audit.New(store, cfg.CursorKeyring(), audit.Authorizations{MCP: mcp, Accounts: authority, Sessions: authority, System: authority, Models: models, Projects: projects, Runners: runnerAuthority})
}

type maintenance interface {
	RunMaintenance(context.Context) error
	StopMaintenance()
	Status() secret.Status
}

func createSecret(cfg config.Config, db database, auditing *audit.Service, authority *account.Authority, usage sc.UsageAuthority, projects *project.Authority, writes *projectvariable.SecretWriteAuthority) (*secret.Service, error) {
	store, ok := db.(secret.Store)
	if !ok || authority == nil {
		return nil, errors.New("SECRET_STORE_UNAVAILABLE")
	}
	if runtimeInformationNil(usage) {
		return nil, errors.New("MODEL_USAGE_UNAVAILABLE")
	}
	if writes == nil {
		return nil, errors.New("PROJECT_VARIABLE_AUTHORITY_UNAVAILABLE")
	}
	projectSecrets, err := project.NewSecretAuthority(projects)
	if err != nil {
		return nil, err
	}
	return secret.New(store, cfg.SecretKeyring(), auditing, secret.Authorizations{AccountWrites: authority, Sessions: authority, System: authority, Usage: usage, Projects: projectSecrets, ProjectVariables: writes})
}
