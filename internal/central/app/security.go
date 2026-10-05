package app

import (
	"context"
	"errors"
	"github.com/LunaDeerTech/agenteam/internal/central/account"
	"github.com/LunaDeerTech/agenteam/internal/central/audit"
	"github.com/LunaDeerTech/agenteam/internal/central/config"
	"github.com/LunaDeerTech/agenteam/internal/central/model"
	"github.com/LunaDeerTech/agenteam/internal/central/secret"
	"time"
)

const SecurityStartupTimeout = 30 * time.Second

func createSecurity(cfg config.Config, db database, authority *account.Authority, models *model.Authority) (*audit.Service, error) {
	store, ok := db.(audit.Store)
	if !ok || authority == nil {
		return nil, errors.New("AUDIT_STORE_UNAVAILABLE")
	}
	if models == nil {
		return nil, errors.New("MODEL_AUTHORITY_UNAVAILABLE")
	}
	return audit.New(store, cfg.CursorKeyring(), audit.Authorizations{Accounts: authority, Sessions: authority, System: authority, Models: models})
}

type maintenance interface {
	RunMaintenance(context.Context) error
	StopMaintenance()
	Status() secret.Status
}

func createSecret(cfg config.Config, db database, auditing *audit.Service, authority *account.Authority, usage *model.SecretUsageRouter) (*secret.Service, error) {
	store, ok := db.(secret.Store)
	if !ok || authority == nil {
		return nil, errors.New("SECRET_STORE_UNAVAILABLE")
	}
	if usage == nil {
		return nil, errors.New("MODEL_USAGE_UNAVAILABLE")
	}
	return secret.New(store, cfg.SecretKeyring(), auditing, secret.Authorizations{AccountWrites: authority, Sessions: authority, System: authority, Usage: usage})
}
