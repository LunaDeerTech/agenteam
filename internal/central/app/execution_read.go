package app

import (
	"context"
	"net/http"

	"github.com/LunaDeerTech/agenteam/internal/central/account"
	"github.com/LunaDeerTech/agenteam/internal/central/config"
	"github.com/LunaDeerTech/agenteam/internal/central/execution"
	executionhttp "github.com/LunaDeerTech/agenteam/internal/central/execution/http"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/project"
	"github.com/LunaDeerTech/agenteam/internal/central/work"
)

type executionReadAssembly struct {
	pair *execution.ProjectReadOwnership
}

func createExecutionReadDependencies(cfg config.Config, db database, projects *projectUsageAssembly, planning *workPlanningAssembly, runtime *executionRuntimeAuthorities) (execution.TaskReadDependencies, error) {
	store, ok := db.(execution.Store)
	if !ok || runtimeInformationNil(store) || projects == nil || projects.projects == nil || planning == nil || planning.ownership == nil {
		return execution.TaskReadDependencies{}, f.NewFault(f.DependencyUnbound, f.NotStarted)
	}
	var authority *execution.Authority
	if runtime != nil {
		authority = runtime.execution
	}
	if authority == nil {
		var err error
		authority, err = execution.NewAuthority(store, projects.projects, nil)
		if err != nil {
			return execution.TaskReadDependencies{}, err
		}
	}
	tasks, err := work.NewExecutionTaskReader(planning.taskReader)
	if err != nil {
		return execution.TaskReadDependencies{}, err
	}
	return execution.TaskReadDependencies{Authority: authority, Tasks: tasks, Cursors: cfg.CursorKeyring()}, nil
}
func createExecutionReads(db database, projects *projectUsageAssembly, objects *objectAssembly, deps execution.TaskReadDependencies) (*executionReadAssembly, error) {
	store, ok := db.(project.Store)
	if !ok || runtimeInformationNil(store) || projects == nil || projects.lifecycle == nil || objects == nil {
		return nil, f.NewFault(f.DependencyUnbound, f.NotStarted)
	}
	stop, err := project.NewExecutionProjectStopAuthority(projects.lifecycle, store)
	if err != nil {
		return nil, err
	}
	pair, err := execution.NewProjectReadOwnership(store, deps, objects.guard, objects.process, stop)
	if err != nil {
		return nil, err
	}
	return &executionReadAssembly{pair}, nil
}
func (a *executionReadAssembly) StopAdmission() {
	if a != nil && a.pair != nil {
		a.pair.Stop()
	}
}
func (a *executionReadAssembly) Drain(ctx context.Context) error {
	if a == nil || a.pair == nil {
		return nil
	}
	return a.pair.Drain(ctx)
}
func (a *executionReadAssembly) Force(ctx context.Context) error { return a.Drain(ctx) }
func (a *executionReadAssembly) Joined() bool                    { return a == nil || a.pair == nil || a.pair.Joined() }
func executionReadHandler(reader *execution.TaskExecutionReader, core *account.Service, origin string) (http.Handler, error) {
	boundary, err := account.NewHTTPBoundary(core, origin)
	if err != nil {
		return nil, err
	}
	return executionhttp.NewHTTPHandler(reader, boundary)
}
func executionReadRoutes(existing, reads http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if executionhttp.HandlesPath(r.URL.Path) {
			reads.ServeHTTP(w, r)
			return
		}
		existing.ServeHTTP(w, r)
	})
}

var _ accountWork = (*executionReadAssembly)(nil)
