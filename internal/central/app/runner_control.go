package app

import (
	"context"
	"net/http"
	"sync"

	"github.com/LunaDeerTech/agenteam/internal/central/account"
	"github.com/LunaDeerTech/agenteam/internal/central/audit"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	runnerhttp "github.com/LunaDeerTech/agenteam/internal/central/runner/http"
	"github.com/LunaDeerTech/agenteam/internal/central/runner/service"
)

// The fixed authority is constructed before the single Audit service captures
// it. Neither binding consults a service locator filled later in construction.
func createRunnerAuthority(db database, accounts *account.Authority) (*service.Authority, error) {
	store, ok := db.(service.Store)
	if !ok || runtimeInformationNil(store) || accounts == nil {
		return nil, f.NewFault(f.DependencyUnbound, f.NotStarted)
	}
	return service.NewAuthority(store, accounts)
}

type runnerControlAssembly struct {
	service         *service.Service
	mu              sync.Mutex
	stopped, joined bool
}

func createRunnerControl(authority *service.Authority, auditor *audit.Service) (*runnerControlAssembly, error) {
	if authority == nil || auditor == nil {
		return nil, f.NewFault(f.DependencyUnbound, f.NotStarted)
	}
	commands, e := service.New(authority, auditor)
	if e != nil {
		return nil, e
	}
	return &runnerControlAssembly{service: commands}, nil
}

func (r *runnerControlAssembly) StopAdmission() {
	r.mu.Lock()
	r.stopped = true
	r.mu.Unlock()
	r.service.Stop()
}
func (r *runnerControlAssembly) Drain(ctx context.Context) error {
	r.StopAdmission()
	if e := r.service.Drain(ctx); e != nil {
		return e
	}
	r.mu.Lock()
	r.joined = true
	r.mu.Unlock()
	return nil
}
func (r *runnerControlAssembly) Force(ctx context.Context) error {
	r.StopAdmission()
	if e := r.service.Force(ctx); e != nil {
		return e
	}
	r.mu.Lock()
	r.joined = true
	r.mu.Unlock()
	return nil
}
func (r *runnerControlAssembly) Joined() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.stopped && r.joined
}

func runnerControlHandlers(r *runnerControlAssembly, accounts *account.Service, origin string) (http.Handler, http.Handler, error) {
	if r == nil || r.service == nil {
		return nil, nil, f.NewFault(f.DependencyUnbound, f.NotStarted)
	}
	boundary, e := account.NewHTTPBoundary(accounts, origin)
	if e != nil {
		return nil, nil, e
	}
	admin, e := runnerhttp.NewAdminHandler(r.service, boundary)
	if e != nil {
		return nil, nil, e
	}
	device, e := runnerhttp.NewDeviceHandler(r.service)
	if e != nil {
		return nil, nil, e
	}
	return admin, device, nil
}

func runnerControlRoutes(existing, admin, device http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if runnerhttp.HandlesAdminPath(r.URL.Path) {
			admin.ServeHTTP(w, r)
			return
		}
		if runnerhttp.HandlesDevicePath(r.URL.Path) {
			device.ServeHTTP(w, r)
			return
		}
		existing.ServeHTTP(w, r)
	})
}

var _ accountWork = (*runnerControlAssembly)(nil)
