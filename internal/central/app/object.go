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

// This is app-private ownership, not a domain capability or public guard lease.
type objectAssembly struct {
	process oc.ProcessID
	service *object.Service
	guard   *object.ProcessGuard
	runtime *object.Runtime
}

func (a *objectAssembly) StartMaintenance(ctx context.Context) error {
	return a.runtime.StartMaintenance(ctx)
}
func (a *objectAssembly) Check(ctx context.Context) error           { return a.runtime.Check(ctx) }
func (a *objectAssembly) StopAdmission()                            { a.runtime.StopAdmission() }
func (a *objectAssembly) Drain(ctx context.Context) error           { return a.runtime.Drain(ctx) }
func (a *objectAssembly) Force(ctx context.Context) error           { return a.runtime.Force(ctx) }
func (a *objectAssembly) forceTransports(ctx context.Context) error { return a.service.Force(ctx) }

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
	assembly := &objectAssembly{process: process, service: service, guard: guard, runtime: runtime}
	return assembly, runtime.Initialize(ctx)
}
