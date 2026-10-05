package app

import (
	"context"
	"errors"
	"sync"

	"github.com/LunaDeerTech/agenteam/internal/central/account"
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

// Partial acquisitions are registered before the next acquisition or any
// Initialize. Only the root's joint producer check may call Force or Drain;
// forceTransports deliberately keeps ProcessGuard held.
type objectAssembly struct {
	mu      sync.Mutex
	process oc.ProcessID
	backend *object.Backend
	spool   *object.Spool
	service *object.Service
	guard   *object.ProcessGuard
	runtime *object.Runtime
	stopped bool
	closed  bool
}

func (a *objectAssembly) StartMaintenance(ctx context.Context) error {
	a.mu.Lock()
	runtime := a.runtime
	a.mu.Unlock()
	if runtime == nil {
		return foundation.NewFault(foundation.DependencyUnbound, foundation.NotStarted)
	}
	return runtime.StartMaintenance(ctx)
}
func (a *objectAssembly) Check(ctx context.Context) error {
	a.mu.Lock()
	runtime := a.runtime
	a.mu.Unlock()
	if runtime == nil {
		return foundation.NewFault(foundation.DependencyUnbound, foundation.NotStarted)
	}
	return runtime.Check(ctx)
}
func (a *objectAssembly) StopAdmission() {
	a.mu.Lock()
	a.stopped = true
	service := a.service
	a.mu.Unlock()
	if service != nil {
		service.StopAdmission()
	}
}
func (a *objectAssembly) Drain(ctx context.Context) error {
	a.StopAdmission()
	a.mu.Lock()
	runtime := a.runtime
	a.mu.Unlock()
	if runtime != nil {
		return runtime.Drain(ctx)
	}
	return a.closePartial(ctx)
}
func (a *objectAssembly) Force(ctx context.Context) error {
	a.StopAdmission()
	a.mu.Lock()
	runtime := a.runtime
	a.mu.Unlock()
	if runtime != nil {
		return runtime.Force(ctx)
	}
	return a.closePartial(ctx)
}
func (a *objectAssembly) forceTransports(ctx context.Context) error {
	a.StopAdmission()
	a.mu.Lock()
	service, backend := a.service, a.backend
	a.mu.Unlock()
	if service != nil {
		return service.Force(ctx)
	}
	if backend != nil {
		return backend.Close()
	}
	return nil
}
func (a *objectAssembly) closePartial(ctx context.Context) error {
	a.mu.Lock()
	if a.closed {
		a.mu.Unlock()
		return nil
	}
	service, guard, spool, backend := a.service, a.guard, a.spool, a.backend
	// No later constructor may attach to resources that are being retired.
	a.closed = true
	a.mu.Unlock()
	var result error
	if service != nil {
		// Even a failed construction uses the real service close before retiring
		// its raw guard or spool. No business operation was admitted on this path.
		if err := service.Force(ctx); err != nil {
			return err
		}
	}
	if guard != nil {
		result = errors.Join(result, guard.Close())
	}
	if spool != nil {
		result = errors.Join(result, spool.Close())
	}
	if backend != nil {
		result = errors.Join(result, backend.Close())
	}
	return result
}
func openObjectAssembly(ctx context.Context, cfg config.Config, owned *resources) (*objectAssembly, error) {
	process, err := foundation.NewID[oc.Process]()
	if err != nil {
		return nil, err
	}
	a := &objectAssembly{process: process}
	if !owned.addObjects(ctx, a) {
		return nil, context.Canceled
	}
	backend, err := object.NewBackend(cfg.Objects().Storage())
	if err != nil {
		return nil, err
	}
	a.mu.Lock()
	if a.closed {
		a.mu.Unlock()
		_ = backend.Close()
		return nil, context.Canceled
	}
	a.backend = backend
	a.mu.Unlock()
	if !owned.objectAcquired(ctx, a) {
		return nil, context.Canceled
	}
	spool, err := object.OpenSpool(cfg.Objects().SpoolDirectory(), process)
	if err != nil {
		return nil, err
	}
	a.mu.Lock()
	if a.closed {
		a.mu.Unlock()
		_ = spool.Close()
		return nil, context.Canceled
	}
	a.spool = spool
	a.mu.Unlock()
	if !owned.objectAcquired(ctx, a) {
		return nil, context.Canceled
	}
	guard, err := object.OpenProcessGuard(spool, process)
	if err != nil {
		return nil, err
	}
	a.mu.Lock()
	if a.closed {
		a.mu.Unlock()
		_ = guard.Close()
		return nil, context.Canceled
	}
	a.guard = guard
	a.mu.Unlock()
	if !owned.objectAcquired(ctx, a) {
		return nil, context.Canceled
	}
	return a, nil
}
func (a *objectAssembly) construct(cfg config.Config, db database, auditing *audit.Service, avatar *account.AvatarAuthority) error {
	store, ok := db.(object.Store)
	if !ok || avatar == nil {
		return foundation.NewFault(foundation.DependencyUnbound, foundation.NotStarted)
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.stopped || a.closed {
		return context.Canceled
	}
	service, err := object.New(store, a.backend, a.spool, auditing, object.Authorizations{Planner: avatar, Resources: avatar, Read: avatar, Gate: avatar, Cleanup: avatar, Leases: avatar, Processes: a.guard})
	if err != nil {
		return err
	}
	a.service = service
	transfers, err := object.NewTransferService(service, nil, cfg.Objects().TransferEndpoint())
	if err != nil {
		return err
	}
	a.runtime, err = object.NewRuntime(service, a.guard, transfers)
	return err
}
