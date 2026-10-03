package app

import (
	"context"
	"errors"
	"github.com/LunaDeerTech/agenteam/internal/central/audit"
	"github.com/LunaDeerTech/agenteam/internal/central/config"
	"time"
)

const SecurityStartupTimeout = 30 * time.Second

func initializeSecurity(ctx context.Context, cfg config.Config, db database) (*audit.Service, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	store, ok := db.(audit.Store)
	if !ok {
		return nil, errors.New("AUDIT_STORE_UNAVAILABLE")
	}
	// D07/D08 supply the real authorization adapters in the composition root.
	// Empty ports reject business calls; there is no anonymous Audit HTTP route.
	service, err := audit.New(store, cfg.CursorKeyring(), audit.Authorizations{})
	if err != nil {
		return nil, err
	}
	if err = service.CheckStorage(ctx); err != nil {
		return nil, err
	}
	return service, nil
}
