package app

import (
	"context"
	"errors"
	"github.com/LunaDeerTech/agenteam/internal/central/account"
	"github.com/LunaDeerTech/agenteam/internal/central/audit"
	"github.com/LunaDeerTech/agenteam/internal/central/config"
	"github.com/LunaDeerTech/agenteam/internal/central/model"
	"github.com/LunaDeerTech/agenteam/internal/central/project"
	"github.com/LunaDeerTech/agenteam/internal/central/secret"
	"time"
)

const SecurityStartupTimeout = 30 * time.Second

func createSecurity(cfg config.Config, db database, authority *account.Authority, models *model.Authority, projects *project.Authority) (*audit.Service, error) {
	store, ok := db.(audit.Store)
	if !ok || authority == nil {
		return nil, errors.New("AUDIT_STORE_UNAVAILABLE")
	}
	if models == nil {
		return nil, errors.New("MODEL_AUTHORITY_UNAVAILABLE")
	}
	if projects == nil {
		return nil, errors.New("PROJECT_AUTHORITY_UNAVAILABLE")
	}
	return audit.New(store, cfg.CursorKeyring(), audit.Authorizations{Accounts: authority, Sessions: authority, System: authority, Models: models, Projects: projects})
}

type maintenance interface {
	RunMaintenance(context.Context) error
	StopMaintenance()
	Status() secret.Status
}

func createSecret(cfg config.Config, db database, auditing *audit.Service, authority *account.Authority, usage *model.SecretUsageRouter, projects *project.Authority) (*secret.Service, error) {
	store, ok := db.(secret.Store)
	if !ok || authority == nil {
		return nil, errors.New("SECRET_STORE_UNAVAILABLE")
	}
	if usage == nil {
		return nil, errors.New("MODEL_USAGE_UNAVAILABLE")
	}
	projectSecrets, err := project.NewSecretAuthority(projects)
	if err != nil {
		return nil, err
	}
	return secret.New(store, cfg.SecretKeyring(), auditing, secret.Authorizations{AccountWrites: authority, Sessions: authority, System: authority, Usage: usage, Projects: projectSecrets})
}
