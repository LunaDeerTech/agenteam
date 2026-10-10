package app

import (
	"context"
	"net/http"
	"sync"

	"github.com/LunaDeerTech/agenteam/internal/central/account"
	ac "github.com/LunaDeerTech/agenteam/internal/central/account/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/audit"
	"github.com/LunaDeerTech/agenteam/internal/central/config"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/outbox"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/project"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	projecthttp "github.com/LunaDeerTech/agenteam/internal/central/project/http"
)

// Pure construction retains the existing authority, store, actual process
// guard. Creation remains unbound until the full lifecycle manifest and its
// actual stop/guard integration are accepted (D08 §7, D10 §14).
func createProjectUpdate(cfg config.Config, db database, projects *project.Authority, accounts *account.Authority, auditor *audit.Service, journal *outbox.Service, events pc.ProjectEvents, processes accountProcessAuthority) (*project.Service, error) {
	store, ok := db.(project.Store)
	if !ok || runtimeInformationNil(store) || accounts == nil || auditor == nil || journal == nil || processes.guard == nil {
		return nil, foundation.NewFault(foundation.DependencyUnbound, foundation.NotStarted)
	}
	return project.New(store, project.Dependencies{Authority: projects, Activity: accounts, Audit: auditor, Events: journal, ProjectEvents: events, Processes: projectCommandProcess{processes}, Cursors: cfg.CursorKeyring()}, project.DefaultConfig())
}

// The IDs have distinct nominal types but identify the very same root process.
// All stop confirmation still reaches the existing account adapter and guard.
type projectCommandProcess struct{ source accountProcessAuthority }

func (p projectCommandProcess) CurrentProcess() oc.ProcessID {
	value, _ := foundation.ParseID[oc.Process](p.source.CurrentProcess().String())
	return value
}
func (p projectCommandProcess) ConfirmStopped(ctx context.Context, target oc.ProcessID) error {
	value, err := foundation.ParseID[ac.Process](target.String())
	if err != nil {
		return foundation.NewFault(foundation.InvalidArgument, foundation.NotStarted)
	}
	return p.source.ConfirmStopped(ctx, value)
}

type projectCommandWork struct {
	service         *project.Service
	mu              sync.Mutex
	stopped, joined bool
}

func (w *projectCommandWork) StopAdmission() {
	w.service.Stop()
	w.mu.Lock()
	w.stopped = true
	w.mu.Unlock()
}
func (w *projectCommandWork) Drain(ctx context.Context) error {
	w.StopAdmission()
	return w.drain(ctx)
}
func (w *projectCommandWork) drain(ctx context.Context) error {
	err := w.service.Drain(ctx)
	if err == nil {
		w.mu.Lock()
		w.joined = true
		w.mu.Unlock()
	}
	return err
}
func (w *projectCommandWork) Force(ctx context.Context) error {
	w.service.Force()
	w.mu.Lock()
	w.stopped = true
	w.mu.Unlock()
	return w.drain(ctx)
}
func (w *projectCommandWork) Joined() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.stopped && w.joined
}
func projectUpdateHandler(service *project.Service, core *account.Service, origin string) (http.Handler, error) {
	boundary, err := account.NewHTTPBoundary(core, origin)
	if err != nil {
		return nil, err
	}
	return projecthttp.NewUpdateHTTPHandler(service, boundary)
}
func projectUpdateRoutes(existing, updates http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if projecthttp.HandlesUpdateRequest(r.Method, r.URL.Path) {
			updates.ServeHTTP(w, r)
			return
		}
		existing.ServeHTTP(w, r)
	})
}

var _ accountWork = (*projectCommandWork)(nil)
