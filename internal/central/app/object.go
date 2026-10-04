package app

import (
	"context"
	"errors"

	"github.com/LunaDeerTech/agenteam/internal/central/audit"
	"github.com/LunaDeerTech/agenteam/internal/central/config"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/object"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
)

type objectStorage interface {
	StartMaintenance(context.Context) error
	Check(context.Context) error
	StopAdmission()
	Drain(context.Context) error
	Force(context.Context) error
}

func initializeObjects(ctx context.Context, cfg config.Config, db database, auditing *audit.Service) (objectStorage, error) {
	store, ok := db.(object.Store)
	if !ok {
		return nil, errors.New("OBJECT_STORE_UNAVAILABLE")
	}
	settings := cfg.Objects()
	backend, err := object.NewBackend(settings.Storage())
	if err != nil {
		return nil, err
	}
	owned := false
	defer func() {
		if !owned {
			_ = backend.Close()
		}
	}()
	process, err := foundation.NewID[oc.Process]()
	if err != nil {
		return nil, err
	}
	spool, err := object.OpenSpool(settings.SpoolDirectory(), process)
	if err != nil {
		return nil, err
	}
	defer func() {
		if !owned {
			_ = spool.Close()
		}
	}()
	guard, err := object.OpenProcessGuard(spool, process)
	if err != nil {
		return nil, err
	}
	defer func() {
		if !owned {
			_ = guard.Close()
		}
	}()
	// Future identity/Project/D17 providers remain absent. Runtime may initialize
	// only a genuinely empty technical store; business methods still refuse use.
	service, err := object.New(store, backend, spool, auditing, object.Authorizations{Processes: guard})
	if err != nil {
		return nil, err
	}
	transfers, err := object.NewTransferService(service, nil, settings.TransferEndpoint())
	if err != nil {
		return nil, err
	}
	runtime, err := object.NewRuntime(service, guard, transfers)
	if err != nil {
		service.StopAdmission()
		_ = service.Force(ctx)
		return nil, err
	}
	owned = true
	return runtime, runtime.Initialize(ctx)
}
